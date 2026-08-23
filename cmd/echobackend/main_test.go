package main

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
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
