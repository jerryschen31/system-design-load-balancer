package proxy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

// TestStress_SlowBackendHasNoTimeout demonstrates a real weakness of this
// phase: the proxy places no time limit on waiting for the backend. If the
// client hadn't set its own timeout, this request would have hung for the
// full backend delay -- and with enough concurrent hung requests, the load
// balancer's own resources (goroutines, file descriptors) would be tied up
// indefinitely. See notes/phase-1-single-backend-proxy.md for the fix plan.
func TestStress_SlowBackendHasNoTimeout(t *testing.T) {
	const backendDelay = 300 * time.Millisecond
	const clientTimeout = 50 * time.Millisecond

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(backendDelay)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	lb := newProxyServer(t, backend.URL)
	defer lb.Close()

	client := &http.Client{Timeout: clientTimeout}
	start := time.Now()
	_, err := client.Get(lb.URL + "/")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected the client's own timeout to fire, since the proxy enforces no timeout of its own")
	}
	if elapsed >= backendDelay {
		t.Errorf("client waited %s (>= full backend delay of %s); expected the client's timeout (~%s) to cut it off first, proving the proxy itself never would have", elapsed, backendDelay, clientTimeout)
	}
	t.Logf("KNOWN WEAKNESS: proxy has no timeout on backend responses (aborted after %s only because the client protected itself; backend needed %s). A hung backend can hold load balancer resources indefinitely.", elapsed, backendDelay)
}

// TestStress_ConcurrentRequestsHandledConcurrently fires a burst of
// concurrent requests at a backend that responds slowly, and checks that
// the proxy handles them in parallel rather than one at a time.
func TestStress_ConcurrentRequestsHandledConcurrently(t *testing.T) {
	const numRequests = 50
	const perRequestDelay = 100 * time.Millisecond

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(perRequestDelay)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	lb := newProxyServer(t, backend.URL)
	defer lb.Close()

	var wg sync.WaitGroup
	errCh := make(chan error, numRequests)
	start := time.Now()
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(lb.URL + "/")
			if err != nil {
				errCh <- err
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errCh <- fmt.Errorf("got status %d", resp.StatusCode)
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)
	close(errCh)

	for err := range errCh {
		t.Errorf("request in burst failed: %v", err)
	}

	// Handled serially this would take numRequests*perRequestDelay; handled
	// concurrently it should take roughly one perRequestDelay plus overhead.
	serialTime := time.Duration(numRequests) * perRequestDelay
	if elapsed >= serialTime/2 {
		t.Errorf("burst of %d requests took %s, expected well under the serial time of %s -- requests may not be handled concurrently", numRequests, elapsed, serialTime)
	}
}

// TestStress_BurstAgainstUnreachableBackend checks that the load balancer
// itself stays healthy and keeps responding cleanly (502s, not hangs,
// crashes, or resource exhaustion) when hit with concurrent requests while
// its only backend is down.
func TestStress_BurstAgainstUnreachableBackend(t *testing.T) {
	const numRequests = 50

	unreachable, err := url.Parse("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("parse unreachable URL: %v", err)
	}
	lb := httptest.NewServer(New(unreachable, newTestLogger()))
	defer lb.Close()

	var wg sync.WaitGroup
	statusCh := make(chan int, numRequests)
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(lb.URL + "/")
			if err != nil {
				statusCh <- -1
				return
			}
			defer resp.Body.Close()
			statusCh <- resp.StatusCode
		}()
	}
	wg.Wait()
	close(statusCh)

	for status := range statusCh {
		if status != http.StatusBadGateway {
			t.Errorf("got status %d, want %d for every request in the burst against a down backend", status, http.StatusBadGateway)
		}
	}
}
