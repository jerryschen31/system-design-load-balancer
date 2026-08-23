package main

import (
	"io"
	"log"
	"net/http/httptest"
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
