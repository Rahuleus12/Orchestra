// Package httpx provides shared HTTP client and transport construction helpers
// for LLM provider implementations.
//
// All Orchestra providers talk to upstream HTTP APIs and share the same
// requirements: connection pooling, support for HTTP(S) proxies (via the
// standard proxy environment variables), bounded idle timeouts, and a sane
// response-header timeout. Centralizing this here keeps the providers
// consistent and prevents drift — historically a transport fix landed in one
// provider but not the others.
//
// The package also distinguishes between the client used for ordinary,
// bounded-duration requests and the client used for long-lived streaming
// (SSE/NDJSON) responses. Go's http.Client.Timeout covers the entire request
// including reading the response body, so a single client with a 10-minute
// timeout would abort any stream that runs longer than 10 minutes. Streaming
// therefore uses a client with no overall timeout and relies on the request
// context plus the transport's ResponseHeaderTimeout for protection.
package httpx

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"time"
)

// Defaults applied to transports returned by NewTransport. They mirror the
// values every Orchestra provider used independently before this package
// existed, and are tuned for an LLM workload with relatively few hosts and
// long-lived keep-alive connections.
const (
	// DefaultMaxIdleConns is the global cap on idle connections across all hosts.
	DefaultMaxIdleConns = 100
	// DefaultMaxIdleConnsPerHost caps idle connections per host.
	DefaultMaxIdleConnsPerHost = 100
	// DefaultIdleConnTimeout is how long an idle connection lives before being closed.
	DefaultIdleConnTimeout = 90 * time.Second
	// DefaultResponseHeaderTimeout bounds how long we wait for the server to
	// send response headers after the request (and optionally request body) have
	// been written. This protects against silently hanging connections for
	// streaming requests that have no overall client timeout.
	DefaultResponseHeaderTimeout = 2 * time.Minute
	// DefaultTLSHandshakeTimeout bounds the TLS handshake.
	DefaultTLSHandshakeTimeout = 10 * time.Second
	// DefaultExpectContinueTimeout bounds waiting for an HTTP 100-continue
	// response before sending the request body.
	DefaultExpectContinueTimeout = 1 * time.Second
	// DefaultDialTimeout bounds the dial phase of a connection.
	DefaultDialTimeout = 30 * time.Second
)

// DefaultRequestTimeout is the overall timeout applied to non-streaming
// requests. Streaming requests intentionally have no overall timeout.
const DefaultRequestTimeout = 10 * time.Minute

// NewTransport returns an *http.Transport configured for LLM provider use:
// connection pooling, HTTP(S) proxy support via the standard proxy environment
// variables (HTTP_PROXY/HTTPS_PROXY/NO_PROXY), HTTP/2 where available, bounded
// idle/handshake timeouts, and a response-header timeout.
//
// The returned transport is safe to share across multiple http.Clients and
// goroutines.
func NewTransport() *http.Transport {
	return &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		MaxIdleConns:        DefaultMaxIdleConns,
		MaxIdleConnsPerHost: DefaultMaxIdleConnsPerHost,
		IdleConnTimeout:     DefaultIdleConnTimeout,
		DialContext: (&net.Dialer{
			Timeout:   DefaultDialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		ResponseHeaderTimeout: DefaultResponseHeaderTimeout,
		TLSHandshakeTimeout:   DefaultTLSHandshakeTimeout,
		ExpectContinueTimeout: DefaultExpectContinueTimeout,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
}

// NewClient returns an http.Client for non-streaming requests with an overall
// timeout. The timeout covers the entire request including reading the response
// body, so callers should NOT use this client for SSE/NDJSON streams that may
// legitimately run longer than the timeout.
//
// If transport is nil, a fresh transport from NewTransport is used. Pass a
// shared transport when you also construct a streaming client so both clients
// reuse the same connection pool.
func NewClient(transport *http.Transport, timeout time.Duration) *http.Client {
	if transport == nil {
		transport = NewTransport()
	}
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}
}

// NewStreamingClient returns an http.Client for long-lived streaming responses
// (SSE/NDJSON). It has no overall timeout, because http.Client.Timeout covers
// the entire request including reading the body and would otherwise kill any
// stream that runs longer than the timeout. Instead, protection comes from:
//   - the request context (caller cancellation/deadline), and
//   - the transport's ResponseHeaderTimeout (guards the connection phase).
//
// If transport is nil, a fresh transport from NewTransport is used.
func NewStreamingClient(transport *http.Transport) *http.Client {
	if transport == nil {
		transport = NewTransport()
	}
	return &http.Client{
		Transport: transport,
		// No overall Timeout — rely on context + ResponseHeaderTimeout.
	}
}

// ValidateBaseURL reports whether u is an http(s) URL with a non-empty host.
// Providers can use this to reject malformed base URLs early instead of
// producing confusing downstream errors.
func ValidateBaseURL(u string) bool {
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return parsed.Host != ""
}
