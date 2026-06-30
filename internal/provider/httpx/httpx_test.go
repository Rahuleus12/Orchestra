package httpx

import (
	"net/http"
	"testing"
	"time"
)

func TestNewTransport_HasProxySupport(t *testing.T) {
	tr := NewTransport()
	if tr.Proxy == nil {
		t.Fatal("expected transport to honor HTTP(S)_PROXY via http.ProxyFromEnvironment, got nil Proxy")
	}
	if !tr.ForceAttemptHTTP2 {
		t.Error("expected ForceAttemptHTTP2=true for modern connection reuse")
	}
	if tr.ResponseHeaderTimeout <= 0 {
		t.Error("expected a positive ResponseHeaderTimeout to guard streaming requests")
	}
	if tr.IdleConnTimeout <= 0 {
		t.Error("expected a positive IdleConnTimeout")
	}
}

func TestNewClient_AppliesOverallTimeout(t *testing.T) {
	tr := NewTransport()
	c := NewClient(tr, 5*time.Second)
	if c.Timeout != 5*time.Second {
		t.Errorf("expected client timeout 5s, got %v", c.Timeout)
	}
	if c.Transport == nil {
		t.Error("expected transport to be set")
	}
}

func TestNewClient_DefaultTimeoutWhenZero(t *testing.T) {
	c := NewClient(nil, 0)
	if c.Timeout != DefaultRequestTimeout {
		t.Errorf("expected default timeout %v when 0 passed, got %v", DefaultRequestTimeout, c.Timeout)
	}
}

func TestNewStreamingClient_HasNoOverallTimeout(t *testing.T) {
	// A non-zero http.Client.Timeout would kill any SSE/NDJSON stream that
	// runs longer than the timeout, because Timeout covers reading the body.
	c := NewStreamingClient(nil)
	if c.Timeout != 0 {
		t.Errorf("streaming client must have no overall timeout, got %v", c.Timeout)
	}
	if c.Transport == nil {
		t.Error("expected transport to be set")
	}
}

func TestNewStreamingClient_SharesTransport(t *testing.T) {
	tr := NewTransport()
	a := NewClient(tr, time.Minute)
	b := NewStreamingClient(tr)
	if a.Transport != http.RoundTripper(tr) || b.Transport != http.RoundTripper(tr) {
		t.Error("both clients should reuse the same transport for connection pooling")
	}
}

func TestValidateBaseURL(t *testing.T) {
	cases := map[string]bool{
		"https://api.openai.com": true,
		"http://localhost:11434": true,
		"":                       false,
		"ftp://example.com":      false,
		"https://":               false,
		"not a url":              false,
	}
	for in, want := range cases {
		if got := ValidateBaseURL(in); got != want {
			t.Errorf("ValidateBaseURL(%q) = %v, want %v", in, got, want)
		}
	}
}
