package proxy

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jerryschen31/system-design-load-balancer/internal/balancer"
	"github.com/jerryschen31/system-design-load-balancer/internal/targetgroup"
)

func newTestLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}

// singleBackend wraps one backend URL in a round-robin balancer that has
// nothing to choose between -- the tests in this file exercise proxy
// mechanics (headers, error handling, streaming), not balancer selection,
// so a single-backend balancer keeps them focused on that.
func singleBackend(t *testing.T, backendURL string) balancer.Balancer {
	t.Helper()
	target, err := url.Parse(backendURL)
	if err != nil {
		t.Fatalf("parse backend URL: %v", err)
	}
	return balancer.NewRoundRobin(targetgroup.New([]*url.URL{target}))
}

// newProxyServer builds a proxy with no backend timeout (0 = disabled) --
// the tests using it aren't exercising timeout behavior, so they keep the
// original "wait indefinitely" semantics. newProxyServerWithTimeout below
// is for the tests that specifically are.
func newProxyServer(t *testing.T, backendURL string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(New(singleBackend(t, backendURL), 0, newTestLogger()))
}

func newProxyServerWithTimeout(t *testing.T, backendURL string, backendTimeout time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(New(singleBackend(t, backendURL), backendTimeout, newTestLogger()))
}

func TestForwardsRequestToBackend(t *testing.T) {
	var gotMethod, gotPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("backend step: received %s %s", r.Method, r.URL.Path)
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusTeapot)
		w.Write([]byte("hello from backend"))
	}))
	defer backend.Close()

	lb := newProxyServer(t, backend.URL)
	defer lb.Close()
	t.Logf("input: client sends POST /widgets to load balancer %s; backend is %s", lb.URL, backend.URL)

	resp, err := http.Post(lb.URL+"/widgets", "text/plain", nil)
	if err != nil {
		t.Fatalf("request through proxy failed: %v", err)
	}
	defer resp.Body.Close()

	if gotMethod != http.MethodPost {
		t.Errorf("backend saw method %q, want POST", gotMethod)
	}
	if gotPath != "/widgets" {
		t.Errorf("backend saw path %q, want /widgets", gotPath)
	}
	if resp.StatusCode != http.StatusTeapot {
		t.Errorf("client got status %d, want %d", resp.StatusCode, http.StatusTeapot)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello from backend" {
		t.Errorf("client got body %q, want %q", body, "hello from backend")
	}
	t.Logf("output: client got status %d and body %q", resp.StatusCode, string(body))
}

func TestClientCannotSpoofForwardedFor(t *testing.T) {
	var gotXFF string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("backend step: saw X-Forwarded-For=%q", r.Header.Get("X-Forwarded-For"))
		gotXFF = r.Header.Get("X-Forwarded-For")
	}))
	defer backend.Close()

	lb := newProxyServer(t, backend.URL)
	defer lb.Close()

	req, _ := http.NewRequest(http.MethodGet, lb.URL+"/", nil)
	// A malicious or misconfigured client claims to be a different IP.
	req.Header.Set("X-Forwarded-For", "6.6.6.6")
	t.Logf("input: client sends X-Forwarded-For=%q to %s", req.Header.Get("X-Forwarded-For"), lb.URL)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request through proxy failed: %v", err)
	}
	resp.Body.Close()

	if gotXFF == "6.6.6.6" {
		t.Errorf("backend saw spoofed X-Forwarded-For %q; the proxy should overwrite it with the real client address", gotXFF)
	}
	if gotXFF == "" {
		t.Errorf("backend saw empty X-Forwarded-For; the proxy should set it to the real client address")
	}
	t.Logf("output: backend received rewritten X-Forwarded-For=%q", gotXFF)
}

func TestHopByHopHeaderNotForwarded(t *testing.T) {
	var gotConnectionHeader string
	sawKeepAliveHeader := false
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("backend step: Connection=%q Keep-Alive present=%t", r.Header.Get("Connection"), r.Header.Get("Keep-Alive") != "")
		gotConnectionHeader = r.Header.Get("Connection")
		_, sawKeepAliveHeader = r.Header["Keep-Alive"]
	}))
	defer backend.Close()

	lb := newProxyServer(t, backend.URL)
	defer lb.Close()

	req, _ := http.NewRequest(http.MethodGet, lb.URL+"/", nil)
	// Connection lists Keep-Alive as a hop-by-hop header that should be
	// stripped before forwarding, not passed through to the backend.
	req.Header.Set("Connection", "Keep-Alive")
	req.Header.Set("Keep-Alive", "timeout=5")
	t.Logf("input: client sends Connection=%q and Keep-Alive=%q", req.Header.Get("Connection"), req.Header.Get("Keep-Alive"))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request through proxy failed: %v", err)
	}
	resp.Body.Close()

	if gotConnectionHeader != "" {
		t.Errorf("backend saw Connection header %q, want it stripped", gotConnectionHeader)
	}
	if sawKeepAliveHeader {
		t.Errorf("backend saw Keep-Alive header, want it stripped as a hop-by-hop header")
	}
	t.Logf("output: backend saw Connection=%q Keep-Alive present=%t", gotConnectionHeader, sawKeepAliveHeader)
}

func TestBackendDownReturns502(t *testing.T) {
	// A backend URL that nothing is listening on: connection refused.
	const unreachableURL = "http://127.0.0.1:1"
	lb := httptest.NewServer(New(singleBackend(t, unreachableURL), 0, newTestLogger()))
	defer lb.Close()
	t.Logf("input: backend %s is down; client sends GET / through load balancer %s", unreachableURL, lb.URL)

	resp, err := http.Get(lb.URL + "/")
	if err != nil {
		t.Fatalf("request through proxy failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("got status %d, want %d (Bad Gateway) when backend is unreachable", resp.StatusCode, http.StatusBadGateway)
	}
	t.Logf("output: client got status %d", resp.StatusCode)
}

func TestNewAllowsNilLogger(t *testing.T) {
	const unreachableURL = "http://127.0.0.1:1"
	lb := httptest.NewServer(New(singleBackend(t, unreachableURL), 0, nil))
	defer lb.Close()
	t.Logf("input: proxy.New(%s, 0, nil)", unreachableURL)

	resp, err := http.Get(lb.URL + "/")
	if err != nil {
		t.Fatalf("request through proxy failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
	t.Logf("output: client got status %d and the proxy did not panic", resp.StatusCode)
}

// TestNoHealthyBackendsReturns502 exercises the path added for health
// checks: a balancer that has a backend but has marked it unhealthy must
// behave, from the client's perspective, exactly like a backend that's
// simply down -- a clean 502, not a hang or a panic.
func TestNoHealthyBackendsReturns502(t *testing.T) {
	target, err := url.Parse("http://127.0.0.1:1") // never actually dialed
	if err != nil {
		t.Fatalf("parse backend URL: %v", err)
	}
	group := targetgroup.New([]*url.URL{target})
	rr := balancer.NewRoundRobin(group)
	group.SetHealthy(target, false)
	t.Logf("input: balancer's only backend (%s) marked unhealthy", target)

	lb := httptest.NewServer(New(rr, 0, newTestLogger()))
	defer lb.Close()

	resp, err := http.Get(lb.URL)
	if err != nil {
		t.Fatalf("request to load balancer failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	t.Logf("output: client got status %d, body %q", resp.StatusCode, string(body))
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
	// Distinct from a generic backend-down 502: the balancer itself
	// refused to pick a target, so the client should get a specific,
	// honest message rather than an empty body indistinguishable from
	// any other failure.
	const wantBody = "no healthy backends available\n"
	if string(body) != wantBody {
		t.Fatalf("got body %q, want %q", string(body), wantBody)
	}
}

func TestNewPanicsOnNilBalancer(t *testing.T) {
	t.Log("input: proxy.New(nil, 0, nil)")

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected New to panic on a nil Balancer, so the failure surfaces at construction instead of on the first request")
		}
		t.Logf("output: New panicked as expected: %v", r)
	}()

	New(nil, 0, nil)
}

// TestBackendTimeoutReturns502 proves the proxy's own backendTimeout cuts a
// slow backend off, without depending on the client having set any timeout
// of its own -- http.DefaultClient here has none. This is the fix for the
// weakness TestStress_SlowBackendHasNoTimeout (stress_test.go) documented
// back in phase 1.
func TestBackendTimeoutReturns502(t *testing.T) {
	const backendDelay = 500 * time.Millisecond
	const backendTimeout = 50 * time.Millisecond
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(backendDelay)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	lb := newProxyServerWithTimeout(t, backend.URL, backendTimeout)
	defer lb.Close()
	t.Logf("input: backend sleeps %s, proxy backendTimeout is %s, client has no timeout of its own", backendDelay, backendTimeout)

	start := time.Now()
	resp, err := http.Get(lb.URL + "/")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("request through proxy failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	t.Logf("output: status=%d body=%q elapsed=%s", resp.StatusCode, string(body), elapsed)
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
	const wantBody = "backend request timed out\n"
	if string(body) != wantBody {
		t.Errorf("got body %q, want %q", string(body), wantBody)
	}
	if elapsed >= backendDelay {
		t.Errorf("client waited %s (>= full backend delay %s); the proxy's own timeout (%s) should have cut it off first", elapsed, backendDelay, backendTimeout)
	}
}

// TestBackendTimeoutAllowsSlowStreamedBodyWithinBudget guards against the
// specific bug cancelOnCloseBody exists to prevent: if the round trip's
// context were cancelled the instant RoundTrip returned (right after
// headers), rather than deferred to resp.Body.Close(), this test's
// streamed, multi-chunk body -- which takes some real time to fully
// arrive, but well within backendTimeout -- would be truncated even though
// nothing actually timed out.
func TestBackendTimeoutAllowsSlowStreamedBodyWithinBudget(t *testing.T) {
	const chunkDelay = 20 * time.Millisecond
	const numChunks = 5
	const backendTimeout = 2 * time.Second
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("httptest ResponseWriter does not support flushing")
		}
		w.WriteHeader(http.StatusOK)
		for i := 0; i < numChunks; i++ {
			fmt.Fprintf(w, "chunk-%d ", i)
			flusher.Flush()
			time.Sleep(chunkDelay)
		}
	}))
	defer backend.Close()

	lb := newProxyServerWithTimeout(t, backend.URL, backendTimeout)
	defer lb.Close()
	t.Logf("input: backend streams %d chunks, %s apart, total ~%s; backendTimeout is %s (well above that)", numChunks, chunkDelay, time.Duration(numChunks)*chunkDelay, backendTimeout)

	resp, err := http.Get(lb.URL + "/")
	if err != nil {
		t.Fatalf("request through proxy failed: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body failed: %v -- this is the exact failure mode of cancelling too early", err)
	}

	want := "chunk-0 chunk-1 chunk-2 chunk-3 chunk-4 "
	t.Logf("output: status=%d body=%q", resp.StatusCode, string(body))
	if string(body) != want {
		t.Errorf("got body %q, want %q -- body was truncated, which means the round trip's context was cancelled before the body finished streaming", string(body), want)
	}
}

// TestRewriteLogsRoutingDecision confirms the fix for the review comment
// asking which backend a request was routed to be logged: a real logger
// (not io.Discard) must see a line naming the selected backend.
func TestRewriteLogsRoutingDecision(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	lb := httptest.NewServer(New(singleBackend(t, backend.URL), 0, logger))
	defer lb.Close()
	t.Logf("input: GET / through the load balancer, backend is %s", backend.URL)

	resp, err := http.Get(lb.URL + "/")
	if err != nil {
		t.Fatalf("request through proxy failed: %v", err)
	}
	defer resp.Body.Close()

	got := buf.String()
	t.Logf("output: logged %q", got)
	if !strings.Contains(got, "GET") || !strings.Contains(got, backend.URL) {
		t.Fatalf("log output %q does not mention the routing decision (method + selected backend)", got)
	}
}
