package proxy

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jerryschen31/system-design-load-balancer/internal/balancer"
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
	return balancer.NewRoundRobin([]*url.URL{target})
}

func newProxyServer(t *testing.T, backendURL string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(New(singleBackend(t, backendURL), newTestLogger()))
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
	lb := httptest.NewServer(New(singleBackend(t, unreachableURL), newTestLogger()))
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
	lb := httptest.NewServer(New(singleBackend(t, unreachableURL), nil))
	defer lb.Close()
	t.Logf("input: proxy.New(%s, nil)", unreachableURL)

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
	rr := balancer.NewRoundRobin([]*url.URL{target})
	rr.SetHealthy(target, false)
	t.Logf("input: balancer's only backend (%s) marked unhealthy", target)

	lb := httptest.NewServer(New(rr, newTestLogger()))
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
	t.Log("input: proxy.New(nil, nil)")

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected New to panic on a nil Balancer, so the failure surfaces at construction instead of on the first request")
		}
		t.Logf("output: New panicked as expected: %v", r)
	}()

	New(nil, nil)
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
	lb := httptest.NewServer(New(singleBackend(t, backend.URL), logger))
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
