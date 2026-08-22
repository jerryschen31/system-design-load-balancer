package main

import "testing"

func TestParseBackendURL(t *testing.T) {
	t.Log("input: backend URL http://localhost:9000")
	target, err := parseBackendURL("http://localhost:9000")
	if err != nil {
		t.Fatalf("parseBackendURL returned unexpected error: %v", err)
	}
	t.Logf("output: scheme=%q host=%q", target.Scheme, target.Host)
}

func TestParseBackendURLRejectsMissingHost(t *testing.T) {
	t.Log("input: backend URL localhost:9000")
	_, err := parseBackendURL("localhost:9000")
	if err == nil {
		t.Fatal("expected parseBackendURL to reject a backend URL without an HTTP(S) host")
	}
	t.Logf("output: %v", err)
}

func TestParseBackendURLRejectsUnsupportedScheme(t *testing.T) {
	t.Log("input: backend URL tcp://localhost:9000")
	_, err := parseBackendURL("tcp://localhost:9000")
	if err == nil {
		t.Fatal("expected parseBackendURL to reject a non-HTTP backend URL")
	}
	t.Logf("output: %v", err)
}
