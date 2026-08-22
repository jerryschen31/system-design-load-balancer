package proxy

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func newTestLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}

func newProxyServer(t *testing.T, backendURL string) *httptest.Server {
	t.Helper()
	target, err := url.Parse(backendURL)
	if err != nil {
		t.Fatalf("parse backend URL: %v", err)
	}
	return httptest.NewServer(New(target, newTestLogger()))
}

func TestForwardsRequestToBackend(t *testing.T) {
	var gotMethod, gotPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusTeapot)
		w.Write([]byte("hello from backend"))
	}))
	defer backend.Close()

	lb := newProxyServer(t, backend.URL)
	defer lb.Close()

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
}

func TestClientCannotSpoofForwardedFor(t *testing.T) {
	var gotXFF string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotXFF = r.Header.Get("X-Forwarded-For")
	}))
	defer backend.Close()

	lb := newProxyServer(t, backend.URL)
	defer lb.Close()

	req, _ := http.NewRequest(http.MethodGet, lb.URL+"/", nil)
	// A malicious or misconfigured client claims to be a different IP.
	req.Header.Set("X-Forwarded-For", "6.6.6.6")

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
}

func TestHopByHopHeaderNotForwarded(t *testing.T) {
	var gotConnectionHeader string
	sawKeepAliveHeader := false
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
}

func TestBackendDownReturns502(t *testing.T) {
	// A backend URL that nothing is listening on: connection refused.
	unreachable, err := url.Parse("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("parse unreachable URL: %v", err)
	}
	lb := httptest.NewServer(New(unreachable, newTestLogger()))
	defer lb.Close()

	resp, err := http.Get(lb.URL + "/")
	if err != nil {
		t.Fatalf("request through proxy failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("got status %d, want %d (Bad Gateway) when backend is unreachable", resp.StatusCode, http.StatusBadGateway)
	}
}
