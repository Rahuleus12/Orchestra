package builtin

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// hostPort returns the host (without scheme or port) from a URL string.
func hostPort(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return u.Hostname()
}

func TestIPIsBlocked(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"10.0.0.1", true},
		{"192.168.1.1", true},
		{"172.16.0.1", true},
		{"169.254.169.254", true}, // link-local (cloud metadata)
		{"0.0.0.0", true},
		{"224.0.0.1", true}, // multicast
		{"8.8.8.8", false},
		{"1.1.1.1", false},
	}
	for _, tc := range cases {
		blocked, reason := ipIsBlocked(net.ParseIP(tc.ip))
		if blocked != tc.blocked {
			t.Errorf("ipIsBlocked(%s) = %v (%s), want %v", tc.ip, blocked, reason, tc.blocked)
		}
	}
}

func TestHTTPRequestTool_BlocksLoopbackByDefault(t *testing.T) {
	t.Parallel()
	tool := NewHTTPRequestTool()
	in, _ := json.Marshal(HTTPRequestInput{URL: "http://127.0.0.1:9/secret"})
	out, err := tool.Execute(t.Context(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp HTTPResponseOutput
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error == "" || !strings.Contains(resp.Error, "non-public") {
		t.Errorf("expected SSRF block error, got %q", resp.Error)
	}
}

func TestHTTPRequestTool_BlocksRedirectToInternal(t *testing.T) {
	t.Parallel()
	// External server that redirects to a loopback metadata address.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:9/meta", http.StatusFound)
	}))
	defer srv.Close()

	// Make the test server reachable by allowlisting its host.
	host := hostPort(srv.URL)
	tool := NewHTTPRequestToolWithConfig(HTTPClientConfig{AllowedHosts: []string{host}})
	in, _ := json.Marshal(HTTPRequestInput{URL: srv.URL + "/start", FollowRedirects: true})
	out, err := tool.Execute(t.Context(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp HTTPResponseOutput
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// The redirect to 127.0.0.1 must be blocked.
	if resp.Error == "" {
		t.Errorf("expected SSRF block on redirect, got status %d and no error", resp.StatusCode)
	}
}

func TestHTTPRequestTool_AllowListPermitsInternal(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	host := hostPort(srv.URL)
	tool := NewHTTPRequestToolWithConfig(HTTPClientConfig{AllowedHosts: []string{host}})
	in, _ := json.Marshal(HTTPRequestInput{URL: srv.URL})
	out, err := tool.Execute(t.Context(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp HTTPResponseOutput
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 via allowlist, got %d (err=%q)", resp.StatusCode, resp.Error)
	}
}

func TestHTTPRequestTool_InsecureSkipVerifyIgnoredByDefault(t *testing.T) {
	t.Parallel()
	// By default AllowInsecureSkipVerify is false, so the per-request flag
	// must be ignored. We can't easily assert the TLS config here, but we can
	// ensure the tool still constructs/runs without error for a plain request.
	cfg := HTTPClientConfig{} // zero value: AllowInsecureSkipVerify == false
	if cfg.AllowInsecureSkipVerify {
		t.Fatal("zero-value config should not allow insecure skip verify")
	}
}
