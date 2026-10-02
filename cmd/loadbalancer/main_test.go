package main

import "testing"

func TestParseBackendURL(t *testing.T) {
	t.Log("input: backend URL http://localhost:9000")
	target, err := parseBackendURL("http://localhost:9000")
	if err != nil {
		t.Fatalf("parseBackendURL returned unexpected error: %v", err)
	}
	if target.Scheme != "http" {
		t.Fatalf("got scheme %q, want %q", target.Scheme, "http")
	}
	if target.Host != "localhost:9000" {
		t.Fatalf("got host %q, want %q", target.Host, "localhost:9000")
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

func TestParseBackendURLsPreservesOrder(t *testing.T) {
	raw := []string{"http://localhost:9001", "http://localhost:9002", "http://localhost:9003"}
	t.Logf("input: repeated -backend flags in order %v", raw)

	targets, err := parseBackendURLs(raw)
	if err != nil {
		t.Fatalf("parseBackendURLs returned unexpected error: %v", err)
	}
	if len(targets) != len(raw) {
		t.Fatalf("got %d targets, want %d", len(targets), len(raw))
	}
	for i, target := range targets {
		if target.String() != raw[i] {
			t.Errorf("target %d: got %s, want %s (order must match flag order for round-robin)", i, target, raw[i])
		}
	}
	t.Logf("output: %v", targets)
}

func TestParseBackendURLsRejectsEmpty(t *testing.T) {
	t.Log("input: no -backend flags given")
	_, err := parseBackendURLs(nil)
	if err == nil {
		t.Fatal("expected parseBackendURLs to reject an empty backend list")
	}
	t.Logf("output: %v", err)
}

func TestParseBackendURLsRejectsAnyInvalidEntry(t *testing.T) {
	raw := []string{"http://localhost:9001", "not-a-valid-backend"}
	t.Logf("input: %v (second entry has no scheme/host)", raw)

	_, err := parseBackendURLs(raw)
	if err == nil {
		t.Fatal("expected parseBackendURLs to reject the list when any single entry is invalid")
	}
	t.Logf("output: %v", err)
}
