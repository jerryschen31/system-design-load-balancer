package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoggingAllowsNilLogger(t *testing.T) {
	t.Log("input: middleware.Logging(nil) wrapping a handler that returns 204")
	handler := Logging(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Log("step: inner handler runs and writes HTTP 204")
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("got status %d, want %d", rec.Code, http.StatusNoContent)
	}
	t.Logf("output: request completed with status %d and no panic", rec.Code)
}
