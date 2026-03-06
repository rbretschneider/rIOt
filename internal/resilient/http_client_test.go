package resilient

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResilientHTTPClientDirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	client := NewResilientHTTPClient(nil, nil)
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestResilientHTTPClientFallback(t *testing.T) {
	// This test verifies that when a hostname can't be resolved,
	// the client falls back to cached IPs (if available).
	resolver := NewResilientResolver("")

	client := NewResilientHTTPClient(resolver, nil)

	// Request to a non-existent host with no cache — should fail
	_, err := client.Get("http://this-host-does-not-exist.invalid/test")
	if err == nil {
		t.Fatal("expected error for non-existent host with no cache")
	}
}

func TestResilientHTTPClientSetTransport(t *testing.T) {
	client := NewResilientHTTPClient(nil, nil)

	// Custom transport that always returns 418
	client.SetTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTeapot,
			Body:       http.NoBody,
		}, nil
	}))

	resp, err := client.Get("http://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusTeapot {
		t.Fatalf("expected 418, got %d", resp.StatusCode)
	}
}

// roundTripFunc is an adapter for using functions as http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
