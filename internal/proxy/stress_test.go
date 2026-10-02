package proxy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestStress_DisabledTimeoutWaitsIndefinitely confirms the documented
// opt-out (backendTimeout <= 0) still behaves exactly like phase 1-3's
// original proxy: no timeout of its own, so only the client's own timeout
// (if any) cuts a slow request off. This used to be this suite's
// KNOWN WEAKNESS -- phase 4a's actual fix, and the test proving it, is
// TestBackendTimeoutReturns502 in proxy_test.go.
func TestStress_DisabledTimeoutWaitsIndefinitely(t *testing.T) {
	const backendDelay = 300 * time.Millisecond
	const clientTimeout = 50 * time.Millisecond
	t.Logf("input: backendTimeout=0 (disabled); backend sleeps for %s; client timeout is %s", backendDelay, clientTimeout)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Log("backend step: request reached backend; backend is now sleeping")
		time.Sleep(backendDelay)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	lb := newProxyServer(t, backend.URL) // backendTimeout=0
	defer lb.Close()

	client := &http.Client{Timeout: clientTimeout}
	start := time.Now()
	_, err := client.Get(lb.URL + "/")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected the client's own timeout to fire, since backendTimeout=0 disables the proxy's own timeout")
	}
	if elapsed >= backendDelay {
		t.Errorf("client waited %s (>= full backend delay of %s); expected the client's timeout (~%s) to cut it off first, proving the proxy itself never would have with backendTimeout=0", elapsed, backendDelay, clientTimeout)
	}
	t.Logf("output: client returned after %s with error %v -- opt-out confirmed working", elapsed, err)
}

// TestStress_ConcurrentRequestsHandledConcurrently fires a burst of
// concurrent requests at a backend that responds slowly, and checks that
// the proxy handles them in parallel rather than one at a time.
func TestStress_ConcurrentRequestsHandledConcurrently(t *testing.T) {
	const numRequests = 50
	const perRequestDelay = 100 * time.Millisecond
	t.Logf("input: %d concurrent requests; backend delay per request is %s", numRequests, perRequestDelay)

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
	t.Logf("output: burst finished in %s (serial time would have been %s)", elapsed, serialTime)
}

// TestStress_BurstAgainstUnreachableBackend checks that the load balancer
// itself stays healthy and keeps responding cleanly (502s, not hangs,
// crashes, or resource exhaustion) when hit with concurrent requests while
// its only backend is down.
func TestStress_BurstAgainstUnreachableBackend(t *testing.T) {
	const numRequests = 50
	t.Logf("input: %d concurrent requests against a load balancer whose only backend is down", numRequests)

	lb := httptest.NewServer(New(singleBackend(t, "http://127.0.0.1:1"), 0, newTestLogger()))
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
	t.Logf("output: every observed response was %d", http.StatusBadGateway)
}
