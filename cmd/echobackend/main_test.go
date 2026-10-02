package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHandlerReportsOwnAddress(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	handler := newHandler(logger, ":9001")

	req := httptest.NewRequest("GET", "/widgets", nil)
	rec := httptest.NewRecorder()
	t.Logf("input: GET /widgets against a handler configured with addr=:9001")

	handler(rec, req)

	body := rec.Body.String()
	want := "echobackend :9001 handled GET /widgets\n"
	t.Logf("output: response body = %q", body)
	if body != want {
		t.Errorf("got body %q, want %q", body, want)
	}
}

func TestHealthHandlerReflectsState(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	var healthy atomic.Bool
	healthy.Store(true)
	handler := newHealthHandler(logger, &healthy)
	t.Log("input: GET /health while healthy=true, then again after flipping to healthy=false")

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest("GET", "/health", nil))
	t.Logf("step: healthy=true -> status %d", rec.Code)
	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	healthy.Store(false)
	rec = httptest.NewRecorder()
	handler(rec, httptest.NewRequest("GET", "/health", nil))
	t.Logf("output: healthy=false -> status %d", rec.Code)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

// TestHealthHandlerLogsRequest confirms the fix for the review comment
// flagging newHealthHandler's logger parameter as unused: a real logger
// (writing to a buffer instead of io.Discard) must actually see a line for
// each /health request, matching the other two handlers' behavior.
func TestHealthHandlerLogsRequest(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	var healthy atomic.Bool
	healthy.Store(true)
	handler := newHealthHandler(logger, &healthy)
	t.Log("input: GET /health against a handler with a real (non-discard) logger")

	handler(httptest.NewRecorder(), httptest.NewRequest("GET", "/health", nil))

	got := buf.String()
	t.Logf("output: logged %q", got)
	if !strings.Contains(got, "GET") || !strings.Contains(got, "/health") {
		t.Fatalf("log output %q does not mention the request method/path", got)
	}
}

func TestToggleHandlerFlipsHealthAndRejectsGet(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	var healthy atomic.Bool
	healthy.Store(true)
	handler := newToggleHandler(logger, &healthy)
	t.Log("input: POST /health/toggle twice, then GET /health/toggle once")

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest("POST", "/health/toggle", nil))
	t.Logf("step: after 1st POST, healthy=%v (body %q)", healthy.Load(), rec.Body.String())
	if healthy.Load() {
		t.Error("got healthy=true after one toggle from true, want false")
	}

	rec = httptest.NewRecorder()
	handler(rec, httptest.NewRequest("POST", "/health/toggle", nil))
	t.Logf("step: after 2nd POST, healthy=%v", healthy.Load())
	if !healthy.Load() {
		t.Error("got healthy=false after two toggles from true, want true")
	}

	rec = httptest.NewRecorder()
	handler(rec, httptest.NewRequest("GET", "/health/toggle", nil))
	t.Logf("output: GET /health/toggle -> status %d", rec.Code)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("got status %d, want %d -- GET must not be able to change state", rec.Code, http.StatusMethodNotAllowed)
	}
}

// TestParseSleepSeconds exercises the default/cap/error logic directly,
// without going through the handler -- so the cap (maxSleepSeconds) can be
// verified without an automated test ever actually waiting that long.
func TestParseSleepSeconds(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    int
		wantErr bool
	}{
		{name: "empty uses default", raw: "", want: defaultSleepSeconds},
		{name: "explicit value", raw: "5", want: 5},
		{name: "zero is valid", raw: "0", want: 0},
		{name: "over cap is clamped", raw: "9999", want: maxSleepSeconds},
		{name: "negative is an error", raw: "-1", wantErr: true},
		{name: "non-numeric is an error", raw: "soon", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("input: seconds=%q", tt.raw)
			got, err := parseSleepSeconds(tt.raw)
			t.Logf("output: got=%d err=%v", got, err)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("got err=nil, want an error for seconds=%q", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("got unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSleepHandlerRespondsAfterElapsed(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	handler := newSleepHandler(logger)
	t.Log("input: GET /sleep?seconds=0 -- should return 200 essentially immediately")

	req := httptest.NewRequest("GET", "/sleep?seconds=0", nil)
	rec := httptest.NewRecorder()
	start := time.Now()
	handler(rec, req)
	elapsed := time.Since(start)

	t.Logf("output: status=%d body=%q elapsed=%v", rec.Code, rec.Body.String(), elapsed)
	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}
	if elapsed > time.Second {
		t.Errorf("handler took %v to return for seconds=0, want near-instant", elapsed)
	}
}

func TestSleepHandlerRejectsInvalidSeconds(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	handler := newSleepHandler(logger)
	t.Log("input: GET /sleep?seconds=nope")

	req := httptest.NewRequest("GET", "/sleep?seconds=nope", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	t.Logf("output: status=%d body=%q", rec.Code, rec.Body.String())
	if rec.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// TestSleepHandlerCancelledByContext proves the handler actually races
// r.Context().Done() against its own sleep, instead of always sleeping to
// completion regardless of whether the caller has given up -- the exact
// mechanism phase 4a's backend-response timeout (and an ordinary client
// disconnect) both depend on. The request context here is cancelled before
// the handler is even called, and seconds is set high (60s) specifically so
// this test fails loudly via t.Fatal, rather than hanging for a minute, if
// the handler were to ignore cancellation and sleep the full duration
// anyway.
func TestSleepHandlerCancelledByContext(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	handler := newSleepHandler(logger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("GET", "/sleep?seconds=60", nil).WithContext(ctx)
	t.Log("input: GET /sleep?seconds=60 with an already-cancelled context")

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler(rec, req)
		close(done)
	}()

	select {
	case <-done:
		t.Logf("output: handler returned promptly on the cancellation path; body=%q (empty means it never reached the normal-completion branch)", rec.Body.String())
		if rec.Body.Len() != 0 {
			t.Errorf("got body %q, want empty -- the cancellation branch must not write a response", rec.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return within 2s of an already-cancelled context -- it is not honoring r.Context().Done()")
	}
}
