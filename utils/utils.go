package utils

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"time"
)

func ReadAndPrintResponse(response *http.Response, err error) {
	if err != nil {
		fmt.Println("Received", err)
		return
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		fmt.Println("Error: " + err.Error())
		return
	}
	if len(body) > 0 {
		fmt.Println("Received", string(body))
	} else {
		fmt.Println("No body received")
	}
}

func FlakyServer(failTimes int, responseCode int, retryAfterDelay time.Duration) *httptest.Server {
	failures := atomic.Int32{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		fmt.Println("Received request", string(body))
		if failures.Add(1) <= int32(failTimes) {
			if retryAfterDelay > 0 {
				w.Header().Add("Retry-After", strconv.Itoa(int(retryAfterDelay.Seconds())))
			}
			fmt.Println("Replying with", responseCode)
			w.WriteHeader(responseCode)
		} else {
			fmt.Fprintf(w, "pong")
		}
	}))
}

func SlowServer(delay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		timer := time.After(delay)
		select {
		// request.Context() will be done as soon as the first successful response is handled by the client
		case <-request.Context().Done():
		case <-timer:
			fmt.Fprintf(w, "pong")
		}
	}))
}
