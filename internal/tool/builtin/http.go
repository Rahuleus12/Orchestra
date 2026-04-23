// Package builtin provides built-in tools for the Orchestra tool system.
// These tools cover common operations that agents frequently need:
// HTTP requests, file operations, shell commands, code search, etc.
package builtin

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// HTTP Request Tool
// ---------------------------------------------------------------------------

// HTTPRequestInput defines the input for the http_request tool.
type HTTPRequestInput struct {
	// URL is the request URL. Required.
	URL string `json:"url" description:"The URL to send the request to"`

	// Method is the HTTP method. Defaults to GET.
	Method string `json:"method,omitempty" description:"HTTP method (GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS)" default:"GET" enum:"GET,POST,PUT,DELETE,PATCH,HEAD,OPTIONS"`

	// Headers is a map of HTTP headers to include in the request.
	Headers map[string]string `json:"headers,omitempty" description:"HTTP headers to include in the request"`

	// Body is the request body for POST/PUT/PATCH requests.
	Body string `json:"body,omitempty" description:"Request body for POST/PUT/PATCH requests"`

	// ContentType sets the Content-Type header. Defaults to application/json for methods with body.
	ContentType string `json:"content_type,omitempty" description:"Content-Type header value" default:"application/json"`

	// TimeoutSeconds is the request timeout in seconds. Defaults to 30.
	TimeoutSeconds int `json:"timeout_seconds,omitempty" description:"Request timeout in seconds" default:"30" min:"1" max:"300"`

	// FollowRedirects controls whether to follow HTTP redirects. Defaults to true.
	FollowRedirects bool `json:"follow_redirects,omitempty" description:"Whether to follow HTTP redirects" default:"true"`

	// InsecureSkipVerify disables TLS certificate verification. Use with caution.
	InsecureSkipVerify bool `json:"insecure_skip_verify,omitempty" description:"Skip TLS certificate verification (insecure)" default:"false"`

	// MaxResponseBytes limits the response body size. Defaults to 1MB.
	MaxResponseBytes int `json:"max_response_bytes,omitempty" description:"Maximum response body size in bytes" default:"1048576" min:"1024" max:"104857600"`
}

// HTTPResponseOutput defines the output of the http_request tool.
type HTTPResponseOutput struct {
	// StatusCode is the HTTP status code.
	StatusCode int `json:"status_code"`

	// StatusText is the HTTP status text (e.g., "OK", "Not Found").
	StatusText string `json:"status_text"`

	// Headers contains the response headers.
	Headers map[string]string `json:"headers,omitempty"`

	// Body contains the response body (truncated if it exceeded MaxResponseBytes).
	Body string `json:"body"`

	// BodyTruncated indicates if the response body was truncated.
	BodyTruncated bool `json:"body_truncated,omitempty"`

	// Error contains an error message if the request failed.
	Error string `json:"error,omitempty"`

	// RequestURL is the final URL after any redirects.
	RequestURL string `json:"request_url,omitempty"`
}

// NewHTTPRequestTool creates the http_request built-in tool.
//
// This tool allows agents to make HTTP requests to external APIs and services.
// It supports all common HTTP methods, custom headers, request bodies, and
// configurable timeouts.
//
// Security considerations:
//   - By default, TLS certificate verification is enabled
//   - Response body size is limited to prevent memory exhaustion
//   - Timeouts prevent hanging on unresponsive servers
//   - Redirects can be disabled to prevent SSRF amplification
func NewHTTPRequestTool() HTTPRequestTool {
	return HTTPRequestTool{}
}

// HTTPRequestTool implements the http_request built-in tool.
// It is safe for concurrent use.
type HTTPRequestTool struct{}

// Name returns the tool's identifier.
func (t HTTPRequestTool) Name() string { return "http_request" }

// Description returns the tool's description for the LLM.
func (t HTTPRequestTool) Description() string {
	return `Make an HTTP request to a URL and return the response.

Supports GET, POST, PUT, DELETE, PATCH, HEAD, and OPTIONS methods.
Use this tool to interact with web APIs, fetch web pages, or send data to endpoints.

Common use cases:
- Fetch data from REST APIs (GET)
- Submit forms or JSON data (POST)
- Update resources (PUT/PATCH)
- Delete resources (DELETE)
- Check URL accessibility (HEAD)

For POST/PUT/PATCH requests, set the body field with your request payload.
The Content-Type header defaults to application/json but can be overridden.

The response body is limited to prevent memory issues; set max_response_bytes
if you need larger responses.`
}

// Parameters returns the JSON Schema for the tool's input.
func (t HTTPRequestTool) Parameters() json.RawMessage {
	// Use the schema generator from the parent package via manual schema
	// to avoid import cycle. This schema matches HTTPRequestInput.
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {
				"type": "string",
				"description": "The URL to send the request to",
				"format": "uri"
			},
			"method": {
				"type": "string",
				"description": "HTTP method (GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS)",
				"default": "GET",
				"enum": ["GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"]
			},
			"headers": {
				"type": "object",
				"description": "HTTP headers to include in the request",
				"additionalProperties": {"type": "string"}
			},
			"body": {
				"type": "string",
				"description": "Request body for POST/PUT/PATCH requests"
			},
			"content_type": {
				"type": "string",
				"description": "Content-Type header value",
				"default": "application/json"
			},
			"timeout_seconds": {
				"type": "integer",
				"description": "Request timeout in seconds",
				"default": 30,
				"minimum": 1,
				"maximum": 300
			},
			"follow_redirects": {
				"type": "boolean",
				"description": "Whether to follow HTTP redirects",
				"default": true
			},
			"insecure_skip_verify": {
				"type": "boolean",
				"description": "Skip TLS certificate verification (insecure)",
				"default": false
			},
			"max_response_bytes": {
				"type": "integer",
				"description": "Maximum response body size in bytes",
				"default": 1048576,
				"minimum": 1024,
				"maximum": 104857600
			}
		},
		"required": ["url"]
	}`)
}

// Execute performs the HTTP request.
func (t HTTPRequestTool) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req HTTPRequestInput
	if err := json.Unmarshal(input, &req); err != nil {
		return marshalError(fmt.Errorf("parse input: %w", err))
	}

	// Validate URL
	if req.URL == "" {
		return marshalError(fmt.Errorf("url is required"))
	}
	parsedURL, err := url.Parse(req.URL)
	if err != nil {
		return marshalError(fmt.Errorf("invalid URL %q: %w", req.URL, err))
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return marshalError(fmt.Errorf("URL must use http or https scheme, got %q", parsedURL.Scheme))
	}

	// Apply defaults
	if req.Method == "" {
		req.Method = "GET"
	}
	req.Method = strings.ToUpper(req.Method)
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = 30
	}
	if req.MaxResponseBytes <= 0 {
		req.MaxResponseBytes = 1048576 // 1MB
	}

	// Build the HTTP request
	var bodyReader io.Reader
	if req.Body != "" {
		bodyReader = strings.NewReader(req.Body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, bodyReader)
	if err != nil {
		return marshalError(fmt.Errorf("create request: %w", err))
	}

	// Set headers
	if req.ContentType != "" && hasBody(req.Method) {
		httpReq.Header.Set("Content-Type", req.ContentType)
	} else if hasBody(req.Method) && req.ContentType == "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	for key, value := range req.Headers {
		// Don't override Content-Type if already set
		if strings.EqualFold(key, "Content-Type") && httpReq.Header.Get("Content-Type") != "" {
			continue
		}
		httpReq.Header.Set(key, value)
	}

	// Build the HTTP client
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	client := &http.Client{
		Timeout: timeout,
	}
	if !req.FollowRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	if req.InsecureSkipVerify {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}
	}

	// Execute the request
	resp, err := client.Do(httpReq)
	if err != nil {
		// Check for context cancellation
		if ctx.Err() != nil {
			return marshalError(fmt.Errorf("request cancelled: %w", ctx.Err()))
		}
		return marshalError(fmt.Errorf("request failed: %w", err))
	}
	defer resp.Body.Close()

	// Read response body with size limit
	limitedReader := io.LimitReader(resp.Body, int64(req.MaxResponseBytes)+1)
	bodyBytes, err := io.ReadAll(limitedReader)
	if err != nil {
		return marshalError(fmt.Errorf("read response body: %w", err))
	}

	// Build output
	output := HTTPResponseOutput{
		StatusCode:     resp.StatusCode,
		StatusText:     resp.Status,
		RequestURL:     resp.Request.URL.String(),
		Headers:        extractHeaders(resp.Header),
		Body:           string(bodyBytes),
		BodyTruncated:  len(bodyBytes) > req.MaxResponseBytes,
	}

	if output.BodyTruncated {
		output.Body = output.Body[:req.MaxResponseBytes]
	}

	return json.Marshal(output)
}

// hasBody returns true if the HTTP method typically includes a request body.
func hasBody(method string) bool {
	switch method {
	case "POST", "PUT", "PATCH":
		return true
	default:
		return false
	}
}

// extractHeaders converts http.Header to a simple map.
// Multiple values for the same header are joined with commas.
func extractHeaders(h http.Header) map[string]string {
	result := make(map[string]string, len(h))
	for key, values := range h {
		if len(values) == 0 {
			continue
		}
		// Join multiple values with comma (per RFC 7230)
		result[key] = strings.Join(values, ", ")
	}
	return result
}

// marshalError creates a JSON error response.
func marshalError(err error) (json.RawMessage, error) {
	output := HTTPResponseOutput{
		Error: err.Error(),
	}
	return json.Marshal(output)
}
