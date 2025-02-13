package main

import (
	"failsafe/utils"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/circuitbreaker"
	"github.com/failsafe-go/failsafe-go/failsafehttp"
	"github.com/failsafe-go/failsafe-go/hedgepolicy"
	"github.com/failsafe-go/failsafe-go/retrypolicy"
	"github.com/failsafe-go/failsafe-go/timeout"
)

func TestHttpWithRetryPolicy(t *testing.T) {
	// Test retry policy
	retryPolicy := failsafehttp.RetryPolicyBuilder().
		WithMaxAttempts(5).
		WithBackoff(time.Millisecond*100, time.Second*1).
		OnRetryScheduled(func(e failsafe.ExecutionScheduledEvent[*http.Response]) {
			fmt.Println("Ping retry", e.Attempts(), "after delay of", e.Delay)
		}).Build()

	// Use the RetryPolicy with a failsafe RoundTripper
	t.Run("with failsafe round tripper", func(t *testing.T) {
		// Setup a test http server that returns 429 on the first two requests with a 1 second Retry-After header
		server := utils.FlakyServer(2, 429, time.Second)
		defer server.Close()
		roundTripper := failsafehttp.NewRoundTripper(nil, retryPolicy)
		client := &http.Client{Transport: roundTripper}

		fmt.Println("Sending ping")
		req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
		resp, err := client.Do(req)

		utils.ReadAndPrintResponse(resp, err)
	})

	// Use the RetryPolicy with an HTTP client via a failsafe execution
	t.Run("with failsafe execution", func(t *testing.T) {
		// Setup a test http server that returns 429 on the first two requests with a 1 second Retry-After header
		server := utils.FlakyServer(2, 429, time.Second)
		defer server.Close()

		fmt.Println("Sending ping")
		resp, err := failsafe.GetWithExecution(func(exec failsafe.Execution[*http.Response]) (*http.Response, error) {
			// Include the execution context in the request, so that cancellations are propagated
			req, _ := http.NewRequestWithContext(exec.Context(), http.MethodGet, server.URL, nil)
			client := &http.Client{}
			return client.Do(req)
		}, retryPolicy)

		utils.ReadAndPrintResponse(resp, err)
	})

	// Use the RetryPolicy with a failsafehttp.Request
	t.Run("with failsafehttp.Request", func(t *testing.T) {
		// Setup a test http server that returns 429 on the first two requests with a 1 second Retry-After header
		server := utils.FlakyServer(2, 429, time.Second)
		defer server.Close()
		client := &http.Client{}

		fmt.Println("Sending ping")
		req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
		failsafeReq := failsafehttp.NewRequest(req, client, retryPolicy)
		resp, err := failsafeReq.Do()

		utils.ReadAndPrintResponse(resp, err)
	})
}

// This test demonstrates how to use a RetryPolicy with custom response handling HTTP using a failsafe RoundTripper.
func TestHttpWithCustomRetryPolicy(t *testing.T) {
	// Setup a test http server that returns 500 on the first two requests
	server := utils.FlakyServer(2, 500, 0)
	defer server.Close()

	// Create a RetryPolicy that only handles 500 responses, with backoff delays between retries
	retryPolicy := retrypolicy.Builder[*http.Response]().
		HandleIf(func(response *http.Response, _ error) bool {
			return response != nil && response.StatusCode == 500
		}).
		WithBackoff(time.Second, 10*time.Second).
		OnRetryScheduled(func(e failsafe.ExecutionScheduledEvent[*http.Response]) {
			fmt.Println("Ping retry", e.Attempts(), "after delay of", e.Delay)
		}).Build()

	// Use the RetryPolicy with a failsafe RoundTripper
	roundTripper := failsafehttp.NewRoundTripper(nil, retryPolicy)
	client := &http.Client{Transport: roundTripper}

	fmt.Println("Sending ping")
	resp, err := client.Get(server.URL)

	utils.ReadAndPrintResponse(resp, err)
}

// This test demonstrates how to use a CircuitBreaker with HTTP via a RoundTripper.
func TestHttpWithCircuitBreaker(t *testing.T) {
	// Setup a test http server that returns 429 on the first request with a 1 second Retry-After header
	server := utils.FlakyServer(1, 429, time.Second)
	defer server.Close()

	// Create a CircuitBreaker that handles 429 responses and uses a half-open delay based on the Retry-After header
	circuitBreaker := circuitbreaker.Builder[*http.Response]().
		HandleIf(func(response *http.Response, err error) bool {
			return response != nil && response.StatusCode == 429
		}).
		WithDelayFunc(failsafehttp.DelayFunc).
		OnStateChanged(func(event circuitbreaker.StateChangedEvent) {
			fmt.Println("circuit breaker state changed", event)
		}).
		Build()

	// Use the RetryPolicy with a failsafe RoundTripper
	roundTripper := failsafehttp.NewRoundTripper(nil, circuitBreaker)
	client := &http.Client{Transport: roundTripper}

	sendPing := func() {
		fmt.Println("Sending ping")
		resp, err := client.Get(server.URL)
		utils.ReadAndPrintResponse(resp, err)
	}

	sendPing()                                  // Should fail with 429, breaker opens
	sendPing()                                  // Should fail with circuitbreaker.ErrOpen
	time.Sleep(circuitBreaker.RemainingDelay()) // Wait for circuit breaker's delay, provided by the Retry-After header
	sendPing()                                  // Should succeed, breaker closes
}

// This test demonstrates how to use a HedgePolicy with HTTP using two different approaches:
//
//   - a failsafe http.RoundTripper
//   - a failsafe execution
//
// Each test will send a request and the HedgePolicy will delay for 1 second before sending up to 2 hedges. The server
// will delay 5 seconds before responding to any of the requests. After the first successul response is received by the
// client, the context for any outstanding requests will be canceled.

func TestHttpWithHedgePolicy(t *testing.T) {
	// Setup a test http server that takes 5 seconds to respond
	server := utils.SlowServer(5 * time.Second)
	defer server.Close()

	// Create a HedgePolicy that sends up to 2 hedges after a 1 second delay each
	hedgePolicy := hedgepolicy.BuilderWithDelay[*http.Response](time.Second).
		WithMaxHedges(2).
		OnHedge(func(f failsafe.ExecutionEvent[*http.Response]) {
			fmt.Println("Sending hedged ping")
		}).
		Build()

	// Use the HedgePolicy with a failsafe RoundTripper
	t.Run("with failsafe round tripper", func(t *testing.T) {
		roundTripper := failsafehttp.NewRoundTripper(nil, hedgePolicy)
		client := &http.Client{Transport: roundTripper}

		fmt.Println("Sending ping")
		resp, err := client.Get(server.URL)

		utils.ReadAndPrintResponse(resp, err)
	})

	// Use the HedgePolicy with an HTTP client via a failsafe execution
	t.Run("with failsafe execution", func(t *testing.T) {
		fmt.Println("Sending ping")
		resp, err := failsafe.GetWithExecution(func(exec failsafe.Execution[*http.Response]) (*http.Response, error) {
			// Include the execution context in the request, so that cancellations are propagated
			req, _ := http.NewRequestWithContext(exec.Context(), http.MethodGet, server.URL, nil)
			client := &http.Client{}
			return client.Do(req)
		}, hedgePolicy)

		utils.ReadAndPrintResponse(resp, err)
	})
}

// This test demonstrates how to use a Timeout with HTTP via a RoundTripper.
func TestHttpWithTimeout(t *testing.T) {
	// Setup a test http server that takes 5 seconds to respond
	server := utils.SlowServer(5 * time.Second)
	defer server.Close()

	// Create a Timeout for 1 second
	timeOut := timeout.With[*http.Response](time.Second)

	// Use the Timeout with a failsafe RoundTripper
	roundTripper := failsafehttp.NewRoundTripper(nil, timeOut)
	client := &http.Client{Transport: roundTripper}

	fmt.Println("Sending ping")
	resp, err := client.Get(server.URL)
	utils.ReadAndPrintResponse(resp, err)
}
