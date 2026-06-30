package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/user/orchestra/internal/config"
	"github.com/user/orchestra/internal/message"
	"github.com/user/orchestra/internal/provider"
)

// ---------------------------------------------------------------------------
// Mock Provider for testing
// ---------------------------------------------------------------------------

type mockProvider struct {
	name   string
	models []provider.ModelInfo
	result *provider.GenerateResult
	stream []provider.StreamEvent
	err    error
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.models, nil
}

func (m *mockProvider) Generate(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResult, error) {
	if m.err != nil {
		return nil, provider.NewProviderError(m.name, req.Model, m.err)
	}
	if m.result != nil {
		return m.result, nil
	}
	return &provider.GenerateResult{
		ID:      "test-gen-id",
		Model:   req.Model,
		Message: message.AssistantMessage("Hello from " + m.name),
		Usage: provider.TokenUsage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
		FinishReason: provider.FinishReasonStop,
		CreatedAt:    time.Now(),
	}, nil
}

func (m *mockProvider) Stream(ctx context.Context, req provider.GenerateRequest) (<-chan provider.StreamEvent, error) {
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan provider.StreamEvent, len(m.stream)+1)
	go func() {
		for _, evt := range m.stream {
			ch <- evt
		}
		ch <- provider.StreamEvent{
			Type: provider.StreamEventDone,
		}
		close(ch)
	}()
	return ch, nil
}

func (m *mockProvider) Capabilities(model string) provider.ModelCapabilities {
	return provider.ModelCapabilities{
		Streaming:   true,
		ToolCalling: true,
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestServer(t *testing.T, mp *mockProvider, cfg *config.Config) *Server {
	t.Helper()

	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	registry := provider.NewRegistry()
	err := registry.RegisterProvider(mp.name, mp)
	if err != nil {
		t.Fatalf("failed to register mock provider: %v", err)
	}

	serverCfg := DefaultServerConfig()
	serverCfg.APIKeys = []string{} // No auth in tests

	return New(serverCfg, registry, cfg, nil)
}

func makeRequest(t *testing.T, srv *Server, method, path string, body string) *http.Response {
	t.Helper()

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req := httptest.NewRequest(method, path, bodyReader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	return w.Result()
}

func parseResponse(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to parse JSON response: %v\nbody: %s", err, string(data))
	}
	return result
}

// ---------------------------------------------------------------------------
// Health Tests
// ---------------------------------------------------------------------------

func TestHandleHealth(t *testing.T) {
	mp := &mockProvider{name: "test", models: []provider.ModelInfo{}}
	srv := newTestServer(t, mp, nil)

	resp := makeRequest(t, srv, http.MethodGet, "/v1/health", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body := parseResponse(t, resp)
	if body["status"] != "ok" {
		t.Errorf("expected status ok, got %v", body["status"])
	}
	if body["timestamp"] == nil {
		t.Error("expected timestamp to be set")
	}
}

// ---------------------------------------------------------------------------
// Info Tests
// ---------------------------------------------------------------------------

func TestHandleInfo(t *testing.T) {
	mp := &mockProvider{name: "test", models: []provider.ModelInfo{}}
	srv := newTestServer(t, mp, nil)

	resp := makeRequest(t, srv, http.MethodGet, "/v1/info", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body := parseResponse(t, resp)
	if body["name"] != "Orchestra" {
		t.Errorf("expected name Orchestra, got %v", body["name"])
	}
}

// ---------------------------------------------------------------------------
// Provider Tests
// ---------------------------------------------------------------------------

func TestHandleListProviders(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		models: []provider.ModelInfo{
			{ID: "model-1", Name: "Model 1"},
			{ID: "model-2", Name: "Model 2"},
		},
	}
	srv := newTestServer(t, mp, nil)

	resp := makeRequest(t, srv, http.MethodGet, "/v1/providers", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body := parseResponse(t, resp)
	providers, ok := body["providers"].([]any)
	if !ok {
		t.Fatalf("expected providers to be an array, got %T", body["providers"])
	}
	if len(providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(providers))
	}
}

func TestHandleGetProvider(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		models: []provider.ModelInfo{
			{ID: "model-1", Name: "Model 1"},
		},
	}
	srv := newTestServer(t, mp, nil)

	resp := makeRequest(t, srv, http.MethodGet, "/v1/providers/test", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body := parseResponse(t, resp)
	if body["name"] != "test" {
		t.Errorf("expected name test, got %v", body["name"])
	}
}

func TestHandleGetProviderNotFound(t *testing.T) {
	mp := &mockProvider{name: "test", models: []provider.ModelInfo{}}
	srv := newTestServer(t, mp, nil)

	resp := makeRequest(t, srv, http.MethodGet, "/v1/providers/nonexistent", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Models Tests
// ---------------------------------------------------------------------------

func TestHandleListModels(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		models: []provider.ModelInfo{
			{ID: "model-1", Name: "Model 1"},
			{ID: "model-2", Name: "Model 2"},
		},
	}
	srv := newTestServer(t, mp, nil)

	resp := makeRequest(t, srv, http.MethodGet, "/v1/models", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body := parseResponse(t, resp)
	if body["count"].(float64) != 2 {
		t.Errorf("expected 2 models, got %v", body["count"])
	}
}

func TestHandleListModelsFilteredByProvider(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		models: []provider.ModelInfo{
			{ID: "model-1", Name: "Model 1"},
		},
	}
	srv := newTestServer(t, mp, nil)

	resp := makeRequest(t, srv, http.MethodGet, "/v1/models?provider=test", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body := parseResponse(t, resp)
	if body["count"].(float64) != 1 {
		t.Errorf("expected 1 model, got %v", body["count"])
	}
}

func TestHandleListModelsProviderNotFound(t *testing.T) {
	mp := &mockProvider{name: "test", models: []provider.ModelInfo{}}
	srv := newTestServer(t, mp, nil)

	resp := makeRequest(t, srv, http.MethodGet, "/v1/models?provider=nonexistent", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Generate Tests
// ---------------------------------------------------------------------------

func TestHandleGenerate(t *testing.T) {
	mp := &mockProvider{name: "test"}
	cfg := config.DefaultConfig()
	cfg.DefaultProvider = "test"
	cfg.DefaultModel = "model-1"

	srv := newTestServer(t, mp, cfg)

	body := `{
		"model": "test::model-1",
		"messages": [
			{"role": "user", "content": "Hello"}
		]
	}`

	resp := makeRequest(t, srv, http.MethodPost, "/v1/generate", body)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Errorf("expected status 200, got %d; body: %s", resp.StatusCode, string(b))
	}

	result := parseResponse(t, resp)
	if result["id"] == nil {
		t.Error("expected id in response")
	}
}

func TestHandleGenerateEmptyMessages(t *testing.T) {
	mp := &mockProvider{name: "test"}
	srv := newTestServer(t, mp, nil)

	body := `{"model": "test-model", "messages": []}`
	resp := makeRequest(t, srv, http.MethodPost, "/v1/generate", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestHandleGenerateInvalidJSON(t *testing.T) {
	mp := &mockProvider{name: "test"}
	srv := newTestServer(t, mp, nil)

	resp := makeRequest(t, srv, http.MethodPost, "/v1/generate", "not json")
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestHandleGenerateWithModelRef(t *testing.T) {
	mp := &mockProvider{name: "test"}
	srv := newTestServer(t, mp, nil)

	body := `{
		"model": "test::model-1",
		"messages": [
			{"role": "user", "content": "Hello"}
		],
		"options": {
			"temperature": 0.7,
			"max_tokens": 100
		}
	}`

	resp := makeRequest(t, srv, http.MethodPost, "/v1/generate", body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Agent Tests
// ---------------------------------------------------------------------------

func TestHandleCreateAgent(t *testing.T) {
	mp := &mockProvider{name: "test"}
	cfg := config.DefaultConfig()
	cfg.DefaultProvider = "test"
	cfg.DefaultModel = "model-1"

	srv := newTestServer(t, mp, cfg)

	body := `{
		"name": "test-agent",
		"model": "test::model-1",
		"system_prompt": "You are a helpful assistant.",
		"max_turns": 5
	}`

	resp := makeRequest(t, srv, http.MethodPost, "/v1/agents", body)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Errorf("expected status 201, got %d; body: %s", resp.StatusCode, string(b))
	}

	result := parseResponse(t, resp)
	if result["agent_id"] == nil {
		t.Error("expected agent_id in response")
	}
}

func TestHandleCreateAgentNoName(t *testing.T) {
	mp := &mockProvider{name: "test"}
	srv := newTestServer(t, mp, nil)

	body := `{"model": "test::model-1"}`
	resp := makeRequest(t, srv, http.MethodPost, "/v1/agents", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestHandleListAgents(t *testing.T) {
	mp := &mockProvider{name: "test"}
	cfg := config.DefaultConfig()
	cfg.DefaultProvider = "test"
	cfg.DefaultModel = "model-1"

	srv := newTestServer(t, mp, cfg)

	// Create an agent first
	body := `{
		"name": "test-agent",
		"model": "test::model-1"
	}`
	makeRequest(t, srv, http.MethodPost, "/v1/agents", body)

	// List agents
	resp := makeRequest(t, srv, http.MethodGet, "/v1/agents", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	result := parseResponse(t, resp)
	if result["count"].(float64) < 1 {
		t.Errorf("expected at least 1 agent, got %v", result["count"])
	}
}

// ---------------------------------------------------------------------------
// Auth Tests
// ---------------------------------------------------------------------------

func TestAuthMiddlewareNoKeys(t *testing.T) {
	mp := &mockProvider{name: "test", models: []provider.ModelInfo{}}
	srv := newTestServer(t, mp, nil)

	resp := makeRequest(t, srv, http.MethodGet, "/v1/info", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 without auth, got %d", resp.StatusCode)
	}
}

func TestAuthMiddlewareWithKeys(t *testing.T) {
	mp := &mockProvider{name: "test", models: []provider.ModelInfo{}}
	cfg := config.DefaultConfig()

	registry := provider.NewRegistry()
	registry.RegisterProvider("test", mp)

	serverCfg := DefaultServerConfig()
	serverCfg.APIKeys = []string{"test-key-123"}

	srv := New(serverCfg, registry, cfg, nil)

	// Request without auth should fail
	req := httptest.NewRequest(http.MethodGet, "/v1/info", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401 without auth, got %d", resp.StatusCode)
	}

	// Request with valid API key should succeed
	req = httptest.NewRequest(http.MethodGet, "/v1/info", nil)
	req.Header.Set("X-API-Key", "test-key-123")
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	resp = w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 with valid key, got %d", resp.StatusCode)
	}
}

func TestAuthMiddlewareBearerToken(t *testing.T) {
	mp := &mockProvider{name: "test", models: []provider.ModelInfo{}}
	cfg := config.DefaultConfig()

	registry := provider.NewRegistry()
	registry.RegisterProvider("test", mp)

	serverCfg := DefaultServerConfig()
	serverCfg.APIKeys = []string{"bearer-token-abc"}

	srv := New(serverCfg, registry, cfg, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/info", nil)
	req.Header.Set("Authorization", "Bearer bearer-token-abc")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 with valid bearer token, got %d", resp.StatusCode)
	}
}

func TestAuthHealthEndpointAlwaysOpen(t *testing.T) {
	mp := &mockProvider{name: "test", models: []provider.ModelInfo{}}
	cfg := config.DefaultConfig()

	registry := provider.NewRegistry()
	registry.RegisterProvider("test", mp)

	serverCfg := DefaultServerConfig()
	serverCfg.APIKeys = []string{"secret-key"}

	srv := New(serverCfg, registry, cfg, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected health endpoint to be accessible without auth, got %d", resp.StatusCode)
	}
}

func TestAuthInvalidKey(t *testing.T) {
	mp := &mockProvider{name: "test", models: []provider.ModelInfo{}}
	cfg := config.DefaultConfig()

	registry := provider.NewRegistry()
	registry.RegisterProvider("test", mp)

	serverCfg := DefaultServerConfig()
	serverCfg.APIKeys = []string{"correct-key"}

	srv := New(serverCfg, registry, cfg, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/info", nil)
	req.Header.Set("X-API-Key", "wrong-key")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401 with wrong key, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// CORS Tests
// ---------------------------------------------------------------------------

func TestCORSMiddleware(t *testing.T) {
	mp := &mockProvider{name: "test", models: []provider.ModelInfo{}}
	cfg := config.DefaultConfig()

	registry := provider.NewRegistry()
	registry.RegisterProvider("test", mp)

	serverCfg := DefaultServerConfig()
	serverCfg.CORSAllowedOrigins = []string{"https://example.com"}

	srv := New(serverCfg, registry, cfg, nil)

	req := httptest.NewRequest(http.MethodOptions, "/v1/health", nil)
	req.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204 for preflight, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Errorf("expected CORS origin header, got %q", resp.Header.Get("Access-Control-Allow-Origin"))
	}
}

// ---------------------------------------------------------------------------
// Request ID Tests
// ---------------------------------------------------------------------------

func TestRequestIDMiddleware(t *testing.T) {
	mp := &mockProvider{name: "test", models: []provider.ModelInfo{}}
	srv := newTestServer(t, mp, nil)

	resp := makeRequest(t, srv, http.MethodGet, "/v1/health", "")
	reqID := resp.Header.Get("X-Request-ID")
	if reqID == "" {
		t.Error("expected X-Request-ID header to be set")
	}
}

// ---------------------------------------------------------------------------
// ServerConfig Tests
// ---------------------------------------------------------------------------

func TestDefaultServerConfig(t *testing.T) {
	cfg := DefaultServerConfig()
	if cfg.Addr != ":8080" {
		t.Errorf("expected addr :8080, got %q", cfg.Addr)
	}
	if cfg.ReadTimeout != 30*time.Second {
		t.Errorf("expected 30s read timeout, got %v", cfg.ReadTimeout)
	}
	if cfg.WriteTimeout != 120*time.Second {
		t.Errorf("expected 120s write timeout, got %v", cfg.WriteTimeout)
	}
	if cfg.ShutdownTimeout != 30*time.Second {
		t.Errorf("expected 30s shutdown timeout, got %v", cfg.ShutdownTimeout)
	}
}

// ---------------------------------------------------------------------------
// Generate Options Tests
// ---------------------------------------------------------------------------

func TestBuildGenerateOptions_Nil(t *testing.T) {
	opts := buildGenerateOptions(nil)
	if opts.Temperature != nil {
		t.Error("expected nil temperature")
	}
}

func TestBuildGenerateOptions_WithValues(t *testing.T) {
	temp := 0.7
	topP := 0.9
	maxTokens := 100
	seed := int64(42)

	opts := buildGenerateOptions(&generateOptionsRequest{
		Temperature:    &temp,
		TopP:           &topP,
		MaxTokens:      &maxTokens,
		Seed:           &seed,
		StopSequences:  []string{"stop"},
		ResponseFormat: "json",
	})

	if opts.Temperature == nil || *opts.Temperature != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", opts.Temperature)
	}
	if opts.MaxTokens == nil || *opts.MaxTokens != 100 {
		t.Errorf("expected max_tokens 100, got %v", opts.MaxTokens)
	}
}

// ---------------------------------------------------------------------------
// Convert Messages Tests
// ---------------------------------------------------------------------------

func TestConvertMessages(t *testing.T) {
	msgs := []messageRequest{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
	}

	result := convertMessages(msgs)
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}

	if result[0].Role != message.RoleSystem {
		t.Errorf("expected role system, got %s", result[0].Role)
	}
	if result[1].Role != message.RoleUser {
		t.Errorf("expected role user, got %s", result[1].Role)
	}
	if result[2].Role != message.RoleAssistant {
		t.Errorf("expected role assistant, got %s", result[2].Role)
	}

	if result[1].Text() != "Hello" {
		t.Errorf("expected content 'Hello', got %q", result[1].Text())
	}
}

func TestConvertMessagesWithToolResult(t *testing.T) {
	msgs := []messageRequest{
		{
			Role:    "tool",
			Content: "tool output",
			ToolResult: &toolResultRequest{
				ToolCallID: "call-123",
				Content:    "result data",
			},
		},
	}

	result := convertMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].Role != message.RoleTool {
		t.Errorf("expected role tool, got %s", result[0].Role)
	}
	if result[0].ToolResult == nil {
		t.Fatal("expected tool result to be set")
	}
	if result[0].ToolResult.ToolCallID != "call-123" {
		t.Errorf("expected tool call ID call-123, got %q", result[0].ToolResult.ToolCallID)
	}
}

// ---------------------------------------------------------------------------
// Extract API Key Tests
// ---------------------------------------------------------------------------

func TestExtractAPIKey(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*http.Request)
		expected string
	}{
		{
			name:     "no key",
			setup:    func(r *http.Request) {},
			expected: "",
		},
		{
			name: "X-API-Key header",
			setup: func(r *http.Request) {
				r.Header.Set("X-API-Key", "my-key")
			},
			expected: "my-key",
		},
		{
			name: "Bearer token",
			setup: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer my-token")
			},
			expected: "my-token",
		},
		{
			name: "X-API-Key takes priority",
			setup: func(r *http.Request) {
				r.Header.Set("X-API-Key", "key-1")
				r.Header.Set("Authorization", "Bearer key-2")
			},
			expected: "key-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			tt.setup(req)
			got := extractAPIKey(req)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Web UI Tests
// ---------------------------------------------------------------------------

func TestHandleUI(t *testing.T) {
	mp := &mockProvider{name: "test", models: []provider.ModelInfo{}}
	srv := newTestServer(t, mp, nil)

	resp := makeRequest(t, srv, http.MethodGet, "/", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 for /, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html content type, got %q", ct)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	if !strings.Contains(string(data), "Orchestra") {
		t.Error("expected HTML to contain 'Orchestra'")
	}
	if !strings.Contains(string(data), "agentList") {
		t.Error("expected HTML to contain 'agentList' element")
	}
}

func TestHandleUIAltPath(t *testing.T) {
	mp := &mockProvider{name: "test", models: []provider.ModelInfo{}}
	srv := newTestServer(t, mp, nil)

	resp := makeRequest(t, srv, http.MethodGet, "/ui", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 for /ui, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Integration: Generate with provider error
// ---------------------------------------------------------------------------

func TestHandleGenerateProviderError(t *testing.T) {
	mp := &mockProvider{
		name: "test",
		err:  provider.NewProviderErrorWithCode("test", "model-1", "rate_limit", 429, context.DeadlineExceeded),
	}
	srv := newTestServer(t, mp, nil)

	body := `{
		"model": "test::model-1",
		"messages": [{"role": "user", "content": "Hello"}]
	}`

	resp := makeRequest(t, srv, http.MethodPost, "/v1/generate", body)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// CORS
// ---------------------------------------------------------------------------

// makeRequestWithOrigin issues a request with the given Origin header against
// the server's handler chain and returns the recorder so headers can be
// inspected.
func makeRequestWithOrigin(t *testing.T, srv *Server, method, path, origin string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

func TestCORS_WildcardReturnsLiteralStar(t *testing.T) {
	srv := newTestServer(t, &mockProvider{name: "test"}, nil)
	// DefaultServerConfig already sets CORSAllowedOrigins = ["*"].

	w := makeRequestWithOrigin(t, srv, http.MethodGet, "/v1/health", "https://evil.example.com")

	got := w.Header().Get("Access-Control-Allow-Origin")
	if got != "*" {
		t.Errorf("wildcard config must return literal %q, not reflected origin; got %q", "*", got)
	}
}

func TestCORS_ExplicitAllowlistReflectsOrigin(t *testing.T) {
	srv := newTestServer(t, &mockProvider{name: "test"}, nil)
	srv.cfg.CORSAllowedOrigins = []string{"https://app.example.com"}

	w := makeRequestWithOrigin(t, srv, http.MethodGet, "/v1/health", "https://app.example.com")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("expected reflected allow-listed origin, got %q", got)
	}
}

func TestCORS_DisallowedOriginGetsNoHeader(t *testing.T) {
	srv := newTestServer(t, &mockProvider{name: "test"}, nil)
	srv.cfg.CORSAllowedOrigins = []string{"https://app.example.com"}

	w := makeRequestWithOrigin(t, srv, http.MethodGet, "/v1/health", "https://evil.example.com")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("disallowed origin must not receive an Allow-Origin header; got %q", got)
	}
}

func TestCORS_PreflightReturnsNoContent(t *testing.T) {
	srv := newTestServer(t, &mockProvider{name: "test"}, nil)

	w := makeRequestWithOrigin(t, srv, http.MethodOptions, "/v1/generate", "https://app.example.com")
	if w.Code != http.StatusNoContent {
		t.Errorf("preflight (OPTIONS) should return 204, got %d", w.Code)
	}
}
