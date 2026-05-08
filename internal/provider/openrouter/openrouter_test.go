package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/user/orchestra/internal/config"
	"github.com/user/orchestra/internal/message"
	"github.com/user/orchestra/internal/provider"
)

// ---------------------------------------------------------------------------
// Test Helpers
// ---------------------------------------------------------------------------

// testConfig returns a minimal ProviderConfig for testing.
func testConfig(apiKey string) config.ProviderConfig {
	return config.ProviderConfig{
		APIKey:       apiKey,
		BaseURL:      "https://openrouter.ai/api/v1",
		DefaultModel: "openai/gpt-4o",
	}
}

// newTestProvider creates a provider with a custom HTTP server for testing.
func newTestProvider(handler http.Handler) (*Provider, *httptest.Server) {
	server := httptest.NewServer(handler)

	cfg := config.ProviderConfig{
		APIKey:       "test-key",
		DefaultModel: "openai/gpt-4o",
		Extra: map[string]any{
			"app_name": "test",
			"site_url": "https://test.example.com",
		},
	}

	p, err := NewProvider(cfg)
	if err != nil {
		panic(fmt.Sprintf("failed to create test provider: %v", err))
	}

	// Override base URL and HTTP client to use test server
	p.baseURL = server.URL
	p.httpClient = server.Client()

	return p, server
}

// ---------------------------------------------------------------------------
// Provider Construction Tests
// ---------------------------------------------------------------------------

func TestNewProvider_RequiresAPIKey(t *testing.T) {
	_, err := NewProvider(config.ProviderConfig{})
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
	if !strings.Contains(err.Error(), "api_key") {
		t.Errorf("error should mention api_key, got: %v", err)
	}
}

func TestNewProvider_Defaults(t *testing.T) {
	p, err := NewProvider(testConfig("test-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Name() != "openrouter" {
		t.Errorf("Name() = %q, want %q", p.Name(), "openrouter")
	}
	if p.defaultModel != "openai/gpt-4o" {
		t.Errorf("defaultModel = %q, want %q", p.defaultModel, "openai/gpt-4o")
	}
	if p.baseURL != "https://openrouter.ai/api/v1" {
		t.Errorf("baseURL = %q, want %q", p.baseURL, "https://openrouter.ai/api/v1")
	}
}

func TestNewProvider_CustomBaseURL(t *testing.T) {
	cfg := testConfig("test-key")
	cfg.BaseURL = "https://custom.api.com/v1/"
	p, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.baseURL != "https://custom.api.com/v1" {
		t.Errorf("baseURL = %q, want trailing slash trimmed", p.baseURL)
	}
}

func TestNewProvider_ExtraConfig(t *testing.T) {
	cfg := testConfig("test-key")
	cfg.Extra = map[string]any{
		"app_name":        "my-app",
		"site_url":        "https://myapp.com",
		"model_cache_ttl": "10m",
		"cost_budget": map[string]any{
			"max_cost_per_request": 0.50,
			"max_cost_per_session": 10.0,
		},
		"routing": map[string]any{
			"provider_order":  []any{"anthropic", "openai"},
			"fallback":        true,
			"data_collection": "deny",
		},
	}

	p, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.appName != "my-app" {
		t.Errorf("appName = %q, want %q", p.appName, "my-app")
	}
	if p.siteURL != "https://myapp.com" {
		t.Errorf("siteURL = %q, want %q", p.siteURL, "https://myapp.com")
	}
	if p.cache.ttl != 10*time.Minute {
		t.Errorf("cache TTL = %v, want %v", p.cache.ttl, 10*time.Minute)
	}
	if p.budget.MaxCostPerRequest != 0.50 {
		t.Errorf("budget MaxCostPerRequest = %v, want 0.50", p.budget.MaxCostPerRequest)
	}
	if p.budget.MaxCostPerSession != 10.0 {
		t.Errorf("budget MaxCostPerSession = %v, want 10.0", p.budget.MaxCostPerSession)
	}
	if !p.routing.Enabled {
		t.Error("routing should be enabled")
	}
	if !p.routing.Fallback {
		t.Error("routing Fallback should be true")
	}
	if p.routing.DataCollection != "deny" {
		t.Errorf("routing DataCollection = %q, want %q", p.routing.DataCollection, "deny")
	}
}

func TestFactory(t *testing.T) {
	p, err := Factory(testConfig("test-key"))
	if err != nil {
		t.Fatalf("Factory returned error: %v", err)
	}
	if p.Name() != "openrouter" {
		t.Errorf("Name() = %q, want %q", p.Name(), "openrouter")
	}
}

// ---------------------------------------------------------------------------
// Headers Tests
// ---------------------------------------------------------------------------

func TestSetHeaders(t *testing.T) {
	p, err := NewProvider(config.ProviderConfig{
		APIKey: "sk-test-123",
		Extra: map[string]any{
			"app_name": "test-app",
			"site_url": "https://test.example.com",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	p.setHeaders(req)

	// Check Authorization header
	auth := req.Header.Get("Authorization")
	if auth != "Bearer sk-test-123" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer sk-test-123")
	}

	// Check Content-Type
	ct := req.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	// Check OpenRouter-specific headers
	referer := req.Header.Get("HTTP-Referer")
	if referer != "https://test.example.com" {
		t.Errorf("HTTP-Referer = %q, want %q", referer, "https://test.example.com")
	}

	title := req.Header.Get("X-Title")
	if title != "test-app" {
		t.Errorf("X-Title = %q, want %q", title, "test-app")
	}
}

// ---------------------------------------------------------------------------
// Model Catalog Tests
// ---------------------------------------------------------------------------

func TestModelCache_BasicFlow(t *testing.T) {
	cache := newModelCache(5 * time.Minute)

	if cache == nil {
		t.Fatal("newModelCache returned nil")
	}
	if cache.ttl != 5*time.Minute {
		t.Errorf("TTL = %v, want %v", cache.ttl, 5*time.Minute)
	}
}

func TestModelCache_LookupCapabilities_EmptyCache(t *testing.T) {
	cache := newModelCache(5 * time.Minute)

	_, ok := cache.LookupCapabilities("nonexistent")
	if ok {
		t.Error("expected false for empty cache lookup")
	}
}

func TestModelCache_LookupPricing_EmptyCache(t *testing.T) {
	cache := newModelCache(5 * time.Minute)

	p := cache.LookupPricing("nonexistent")
	if p != nil {
		t.Error("expected nil for empty cache pricing lookup")
	}
}

func TestConvertModel(t *testing.T) {
	m := orModel{
		ID:            "openai/gpt-4o",
		Name:          "OpenAI: GPT-4o",
		Description:   "Most advanced model",
		ContextLength: 128000,
		Pricing: orModelPricing{
			Prompt:     "0.000005",
			Completion: "0.000015",
		},
		TopProvider: orProviderInfo{
			MaxCompletionTokens: 16384,
		},
		Architecture: orArchitecture{
			Modality:         "text->text",
			InputModalities:  []string{"text", "image"},
			OutputModalities: []string{"text"},
			Tokenizer:        "o200k_base",
		},
		Moderated: false,
		Created:   1700000000,
	}

	info := convertModel(m)

	if info.ID != "openai/gpt-4o" {
		t.Errorf("ID = %q, want %q", info.ID, "openai/gpt-4o")
	}
	if info.Name != "OpenAI: GPT-4o" {
		t.Errorf("Name = %q, want %q", info.Name, "OpenAI: GPT-4o")
	}
	if info.Capabilities.Streaming != true {
		t.Error("Streaming should be true")
	}
	if info.Capabilities.Vision != true {
		t.Error("Vision should be true (model has image input modality)")
	}
	if info.Capabilities.ContextWindow != 128000 {
		t.Errorf("ContextWindow = %d, want 128000", info.Capabilities.ContextWindow)
	}
	if info.Capabilities.MaxTokens != 16384 {
		t.Errorf("MaxTokens = %d, want 16384", info.Capabilities.MaxTokens)
	}
}

func TestInferToolSupport(t *testing.T) {
	tests := []struct {
		name  string
		model orModel
		want  bool
	}{
		{
			name: "chat model supports tools",
			model: orModel{
				ID:           "openai/gpt-4o",
				Architecture: orArchitecture{Modality: "text->text"},
			},
			want: true,
		},
		{
			name: "image model does not support tools",
			model: orModel{
				ID:           "openai/dall-e-3",
				Architecture: orArchitecture{Modality: "text->image"},
			},
			want: false,
		},
		{
			name: "embedding model does not support tools",
			model: orModel{
				ID:           "openai/text-embedding-3-large",
				Architecture: orArchitecture{Modality: "text->text"},
			},
			want: false,
		},
		{
			name: "whisper model does not support tools",
			model: orModel{
				ID:           "openai/whisper",
				Architecture: orArchitecture{Modality: "audio->text"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferToolSupport(tt.model)
			if got != tt.want {
				t.Errorf("inferToolSupport() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasModality(t *testing.T) {
	tests := []struct {
		name     string
		model    orModel
		modality string
		want     bool
	}{
		{
			name: "has image modality in list",
			model: orModel{
				Architecture: orArchitecture{
					InputModalities: []string{"text", "image"},
				},
			},
			modality: "image",
			want:     true,
		},
		{
			name: "no image modality",
			model: orModel{
				Architecture: orArchitecture{
					InputModalities: []string{"text"},
				},
			},
			modality: "image",
			want:     false,
		},
		{
			name: "image in modality string",
			model: orModel{
				Architecture: orArchitecture{
					Modality: "text+image->text",
				},
			},
			modality: "image",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasModality(tt.model, tt.modality)
			if got != tt.want {
				t.Errorf("hasModality() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseModelPricing(t *testing.T) {
	tests := []struct {
		name    string
		pricing orModelPricing
		want    *ModelPricing
	}{
		{
			name: "valid pricing",
			pricing: orModelPricing{
				Prompt:     "0.000005",
				Completion: "0.000015",
			},
			want: &ModelPricing{
				PromptCost:     0.000005,
				CompletionCost: 0.000015,
			},
		},
		{
			name:    "empty pricing",
			pricing: orModelPricing{},
			want:    nil,
		},
		{
			name: "zero pricing values",
			pricing: orModelPricing{
				Prompt:     "0",
				Completion: "0",
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseModelPricing(tt.pricing)
			if tt.want == nil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
			} else {
				if got == nil {
					t.Fatalf("expected non-nil result")
				}
				if got.PromptCost != tt.want.PromptCost {
					t.Errorf("PromptCost = %v, want %v", got.PromptCost, tt.want.PromptCost)
				}
				if got.CompletionCost != tt.want.CompletionCost {
					t.Errorf("CompletionCost = %v, want %v", got.CompletionCost, tt.want.CompletionCost)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Models API Integration Test (with mock server)
// ---------------------------------------------------------------------------

func TestModels_MockServer(t *testing.T) {
	modelResponse := orModelResponse{
		Data: []orModel{
			{
				ID:            "openai/gpt-4o",
				Name:          "OpenAI: GPT-4o",
				ContextLength: 128000,
				Pricing: orModelPricing{
					Prompt:     "0.000005",
					Completion: "0.000015",
				},
				TopProvider: orProviderInfo{MaxCompletionTokens: 16384},
				Architecture: orArchitecture{
					Modality:         "text->text",
					InputModalities:  []string{"text", "image"},
					OutputModalities: []string{"text"},
				},
			},
			{
				ID:            "anthropic/claude-sonnet-4-20250514",
				Name:          "Anthropic: Claude Sonnet 4",
				ContextLength: 200000,
				Pricing: orModelPricing{
					Prompt:     "0.000003",
					Completion: "0.000015",
				},
				TopProvider: orProviderInfo{MaxCompletionTokens: 8192},
				Architecture: orArchitecture{
					Modality:         "text->text",
					InputModalities:  []string{"text", "image"},
					OutputModalities: []string{"text"},
				},
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(modelResponse)
			return
		}
		http.NotFound(w, r)
	})

	p, server := newTestProvider(handler)
	defer server.Close()

	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() returned error: %v", err)
	}

	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}

	if models[0].ID != "openai/gpt-4o" {
		t.Errorf("models[0].ID = %q, want %q", models[0].ID, "openai/gpt-4o")
	}
	if models[0].Capabilities.ContextWindow != 128000 {
		t.Errorf("models[0] ContextWindow = %d, want 128000", models[0].Capabilities.ContextWindow)
	}
	if !models[0].Capabilities.Vision {
		t.Error("models[0] should have Vision=true")
	}
}

func TestModels_Caching(t *testing.T) {
	callCount := 0
	modelResponse := orModelResponse{
		Data: []orModel{
			{ID: "openai/gpt-4o", Name: "GPT-4o", ContextLength: 128000},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(modelResponse)
	})

	p, server := newTestProvider(handler)
	defer server.Close()

	// First call should hit the server
	_, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 API call, got %d", callCount)
	}

	// Second call within TTL should use cache
	_, err = p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 API call (cached), got %d", callCount)
	}
}

// ---------------------------------------------------------------------------
// Generate Tests (with mock server)
// ---------------------------------------------------------------------------

func TestGenerate_MockServer(t *testing.T) {
	chatResponse := orChatResponse{
		ID:      "chatcmpl-test123",
		Model:   "openai/gpt-4o",
		Created: 1700000000,
		Choices: []orChoice{
			{
				Index: 0,
				Message: orMessage{
					Role:    "assistant",
					Content: jsonString("Hello! How can I help you?"),
				},
				FinishReason: strPtr("stop"),
			},
		},
		Usage: orUsage{
			PromptTokens:     10,
			CompletionTokens: 8,
			TotalTokens:      18,
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing Authorization header")
		}
		if r.Header.Get("X-Title") != "test" {
			t.Errorf("missing X-Title header")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chatResponse)
	})

	p, server := newTestProvider(handler)
	defer server.Close()

	result, err := p.Generate(context.Background(), provider.GenerateRequest{
		Model: "openai/gpt-4o",
		Messages: []message.Message{
			{
				Role:    message.RoleUser,
				Content: []message.ContentBlock{message.TextContentBlock("Hello")},
			},
		},
	})
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	if result.Text() != "Hello! How can I help you?" {
		t.Errorf("Text() = %q, want %q", result.Text(), "Hello! How can I help you?")
	}
	if result.FinishReason != provider.FinishReasonStop {
		t.Errorf("FinishReason = %q, want %q", result.FinishReason, provider.FinishReasonStop)
	}
	if result.Usage.TotalTokens != 18 {
		t.Errorf("Usage.TotalTokens = %d, want 18", result.Usage.TotalTokens)
	}
}

func TestGenerate_RequiresMessages(t *testing.T) {
	p, err := NewProvider(testConfig("test-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = p.Generate(context.Background(), provider.GenerateRequest{
		Model:    "openai/gpt-4o",
		Messages: []message.Message{},
	})
	if err == nil {
		t.Fatal("expected error for empty messages")
	}
}

func TestGenerate_APIError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(orErrorResponse{
			Error: orErrorDetail{
				Message: "Invalid API key",
				Type:    "authentication_error",
				Code:    "invalid_api_key",
			},
		})
	})

	p, server := newTestProvider(handler)
	defer server.Close()

	_, err := p.Generate(context.Background(), provider.GenerateRequest{
		Model: "openai/gpt-4o",
		Messages: []message.Message{
			{
				Role:    message.RoleUser,
				Content: []message.ContentBlock{message.TextContentBlock("Hello")},
			},
		},
	})
	if err == nil {
		t.Fatal("expected error for API error response")
	}

	// Check it's a ProviderError via type assertion
	perr, ok := err.(*provider.ProviderError)
	if !ok {
		t.Fatalf("expected ProviderError, got %T: %v", err, err)
	}
	if perr.Code != "invalid_api_key" {
		t.Errorf("Code = %q, want %q", perr.Code, "invalid_api_key")
	}
	if perr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want %d", perr.StatusCode, http.StatusUnauthorized)
	}
}

func TestGenerate_ToolCalls(t *testing.T) {
	chatResponse := orChatResponse{
		ID:    "chatcmpl-tool123",
		Model: "openai/gpt-4o",
		Choices: []orChoice{
			{
				Index: 0,
				Message: orMessage{
					Role: "assistant",
					ToolCalls: []orToolCall{
						{
							ID:   "call_abc123",
							Type: "function",
							Function: orFunction{
								Name:      "get_weather",
								Arguments: `{"city":"New York"}`,
							},
						},
					},
				},
				FinishReason: strPtr("tool_calls"),
			},
		},
		Usage: orUsage{PromptTokens: 20, CompletionTokens: 15, TotalTokens: 35},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chatResponse)
	})

	p, server := newTestProvider(handler)
	defer server.Close()

	result, err := p.Generate(context.Background(), provider.GenerateRequest{
		Model: "openai/gpt-4o",
		Messages: []message.Message{
			{Role: message.RoleUser, Content: []message.ContentBlock{message.TextContentBlock("What's the weather?")}},
		},
		Tools: []provider.ToolDefinition{
			provider.FunctionTool("get_weather", "Get weather", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string"},
				},
			}),
		},
	})
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	if !result.IsToolCall() {
		t.Error("expected tool call result")
	}
	if result.FinishReason != provider.FinishReasonToolCall {
		t.Errorf("FinishReason = %q, want %q", result.FinishReason, provider.FinishReasonToolCall)
	}
	if len(result.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.Message.ToolCalls))
	}
	tc := result.Message.ToolCalls[0]
	if tc.Function.Name != "get_weather" {
		t.Errorf("tool name = %q, want %q", tc.Function.Name, "get_weather")
	}
}

// ---------------------------------------------------------------------------
// Stream Tests (with mock server)
// ---------------------------------------------------------------------------

func TestStream_MockServer(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer does not support flushing")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")

		// Send some SSE chunks
		chunks := []string{
			`{"id":"chatcmpl-1","model":"openai/gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-1","model":"openai/gpt-4o","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-1","model":"openai/gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`,
		}

		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		}

		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	})

	p, server := newTestProvider(handler)
	defer server.Close()

	ch, err := p.Stream(context.Background(), provider.GenerateRequest{
		Model: "openai/gpt-4o",
		Messages: []message.Message{
			{Role: message.RoleUser, Content: []message.ContentBlock{message.TextContentBlock("Hi")}},
		},
	})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	var text string
	var gotDone bool
	var gotStart bool

	for evt := range ch {
		switch evt.Type {
		case provider.StreamEventStart:
			gotStart = true
		case provider.StreamEventChunk:
			text += evt.Chunk
		case provider.StreamEventDone:
			gotDone = true
			if evt.Usage != nil && evt.Usage.TotalTokens != 8 {
				t.Errorf("usage TotalTokens = %d, want 8", evt.Usage.TotalTokens)
			}
		case provider.StreamEventError:
			t.Fatalf("stream error: %v", evt.Error)
		}
	}

	if !gotStart {
		t.Error("missing start event")
	}
	if !gotDone {
		t.Error("missing done event")
	}
	if text != "Hello world" {
		t.Errorf("streamed text = %q, want %q", text, "Hello world")
	}
}

func TestStream_RequiresMessages(t *testing.T) {
	p, err := NewProvider(testConfig("test-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = p.Stream(context.Background(), provider.GenerateRequest{
		Model:    "openai/gpt-4o",
		Messages: []message.Message{},
	})
	if err == nil {
		t.Fatal("expected error for empty messages")
	}
}

// ---------------------------------------------------------------------------
// Capabilities Tests
// ---------------------------------------------------------------------------

func TestCapabilities_KnownModel(t *testing.T) {
	p, err := NewProvider(testConfig("test-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	caps := p.Capabilities("openai/gpt-4o")
	if !caps.Streaming {
		t.Error("gpt-4o should support streaming")
	}
	if !caps.ToolCalling {
		t.Error("gpt-4o should support tool calling")
	}
	if !caps.Vision {
		t.Error("gpt-4o should support vision (heuristic)")
	}
}

func TestCapabilities_UnknownModel(t *testing.T) {
	p, err := NewProvider(testConfig("test-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should not panic for unknown models
	caps := p.Capabilities("some-unknown-model-xyz")
	if caps.MaxTokens < 0 {
		t.Errorf("MaxTokens should be >= 0, got %d", caps.MaxTokens)
	}
}

// ---------------------------------------------------------------------------
// Cost Tracking Tests
// ---------------------------------------------------------------------------

func TestCostTracker_BasicFlow(t *testing.T) {
	tracker := NewCostTracker()

	tracker.Record("openai/gpt-4o", provider.TokenUsage{
		PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150,
	}, 0.00125)

	tracker.Record("anthropic/claude-sonnet-4-20250514", provider.TokenUsage{
		PromptTokens: 200, CompletionTokens: 100, TotalTokens: 300,
	}, 0.00210)

	report := tracker.Report()
	if report.RequestCount != 2 {
		t.Errorf("RequestCount = %d, want 2", report.RequestCount)
	}
	if report.TotalCost < 0.00334 || report.TotalCost > 0.00336 {
		t.Errorf("TotalCost = %f, want ~0.003350", report.TotalCost)
	}
	if report.TotalTokens.PromptTokens != 300 {
		t.Errorf("TotalTokens.PromptTokens = %d, want 300", report.TotalTokens.PromptTokens)
	}
	if len(report.ByModel) != 2 {
		t.Errorf("ByModel count = %d, want 2", len(report.ByModel))
	}
}

func TestCostTracker_Reset(t *testing.T) {
	tracker := NewCostTracker()
	tracker.Record("test", provider.TokenUsage{}, 0.01)

	tracker.Reset()
	report := tracker.Report()
	if report.RequestCount != 0 {
		t.Errorf("RequestCount after reset = %d, want 0", report.RequestCount)
	}
}

func TestCostTracker_Concurrent(t *testing.T) {
	tracker := NewCostTracker()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tracker.Record("test-model", provider.TokenUsage{
				PromptTokens: 10, CompletionTokens: 10, TotalTokens: 20,
			}, 0.001)
		}()
	}

	wg.Wait()

	report := tracker.Report()
	if report.RequestCount != 100 {
		t.Errorf("RequestCount = %d, want 100", report.RequestCount)
	}
}

func TestCostFormatting(t *testing.T) {
	tests := []struct {
		cost float64
		want string
	}{
		{0.0000001, "$0.000000"},
		{0.0001, "$0.0001"},
		{0.005, "$0.0050"},
		{0.50, "$0.5000"},
		{5.00, "$5.00"},
	}

	for _, tt := range tests {
		got := FormatCost(tt.cost)
		if got != tt.want {
			t.Errorf("FormatCost(%f) = %q, want %q", tt.cost, got, tt.want)
		}
	}
}

func TestFormatCostReport(t *testing.T) {
	report := CostReport{
		TotalCost:    0.05,
		RequestCount: 3,
		TotalTokens:  provider.TokenUsage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
		ByModel: map[string]ModelCostSummary{
			"openai/gpt-4o": {CostUSD: 0.05, RequestCount: 3, PromptTokens: 100, CompletionTokens: 50},
		},
	}

	s := FormatCostReport(report)
	if !strings.Contains(s, "$0.05") {
		t.Errorf("cost report should contain cost, got: %s", s)
	}
	if !strings.Contains(s, "openai/gpt-4o") {
		t.Errorf("cost report should contain model name, got: %s", s)
	}
}

func TestFormatCostReport_Empty(t *testing.T) {
	report := CostReport{}
	s := FormatCostReport(report)
	if !strings.Contains(s, "No costs") {
		t.Errorf("empty report should say no costs, got: %s", s)
	}
}

// ---------------------------------------------------------------------------
// Budget Enforcement Tests
// ---------------------------------------------------------------------------

func TestBudgetEnforcement_SessionExceeded(t *testing.T) {
	cfg := testConfig("test-key")
	cfg.Extra = map[string]any{
		"cost_budget": map[string]any{
			"max_cost_per_session": 0.001, // Very small budget
		},
	}

	p, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Pre-record some cost to exceed budget
	p.costTracker.Record("test-model", provider.TokenUsage{}, 0.01)

	// A request should be blocked
	err = p.budgetCheck("test-model", 0)
	if err == nil {
		t.Error("expected budget exceeded error")
	}
	if !strings.Contains(err.Error(), "session budget") {
		t.Errorf("error should mention session budget, got: %v", err)
	}
}

func TestBudgetEnforcement_NoBudget(t *testing.T) {
	p, err := NewProvider(testConfig("test-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No budget configured — should always pass
	err = p.budgetCheck("any-model", 1000000)
	if err != nil {
		t.Errorf("unexpected error with no budget: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Routing Tests
// ---------------------------------------------------------------------------

func TestBuildProviderPrefs_Disabled(t *testing.T) {
	p, err := NewProvider(testConfig("test-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	prefs := p.buildProviderPrefs()
	if prefs != nil {
		t.Error("expected nil prefs when routing is disabled")
	}
}

func TestBuildProviderPrefs_Enabled(t *testing.T) {
	cfg := testConfig("test-key")
	cfg.Extra = map[string]any{
		"routing": map[string]any{
			"provider_order":  []any{"anthropic"},
			"fallback":        true,
			"data_collection": "deny",
		},
	}

	p, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	prefs := p.buildProviderPrefs()
	if prefs == nil {
		t.Fatal("expected non-nil prefs")
	}
	if len(prefs.Order) != 1 || prefs.Order[0] != "anthropic" {
		t.Errorf("Order = %v, want [anthropic]", prefs.Order)
	}
	if prefs.AllowFallbacks == nil || !*prefs.AllowFallbacks {
		t.Error("AllowFallbacks should be true")
	}
	if prefs.DataCollection != "deny" {
		t.Errorf("DataCollection = %q, want %q", prefs.DataCollection, "deny")
	}
}

func TestParseRoutingConfig(t *testing.T) {
	tests := []struct {
		name  string
		extra map[string]any
		want  RoutingConfig
	}{
		{
			name:  "no routing config",
			extra: map[string]any{},
			want:  RoutingConfig{},
		},
		{
			name: "full routing config",
			extra: map[string]any{
				"routing": map[string]any{
					"provider_order":  []any{"anthropic", "openai"},
					"fallback":        true,
					"data_collection": "deny",
				},
			},
			want: RoutingConfig{
				Enabled:        true,
				ProviderOrder:  []string{"anthropic", "openai"},
				Fallback:       true,
				DataCollection: "deny",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRoutingConfig(tt.extra)
			if got.Enabled != tt.want.Enabled {
				t.Errorf("Enabled = %v, want %v", got.Enabled, tt.want.Enabled)
			}
			if got.Fallback != tt.want.Fallback {
				t.Errorf("Fallback = %v, want %v", got.Fallback, tt.want.Fallback)
			}
			if got.DataCollection != tt.want.DataCollection {
				t.Errorf("DataCollection = %q, want %q", got.DataCollection, tt.want.DataCollection)
			}
			if len(got.ProviderOrder) != len(tt.want.ProviderOrder) {
				t.Errorf("ProviderOrder len = %d, want %d", len(got.ProviderOrder), len(tt.want.ProviderOrder))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Message Conversion Tests
// ---------------------------------------------------------------------------

func TestConvertMessages(t *testing.T) {
	msgs := []message.Message{
		{
			Role:    message.RoleSystem,
			Content: []message.ContentBlock{message.TextContentBlock("You are helpful")},
		},
		{
			Role:    message.RoleUser,
			Content: []message.ContentBlock{message.TextContentBlock("Hello")},
		},
		{
			Role: message.RoleAssistant,
			ToolCalls: []message.ToolCall{
				{
					ID:   "call_123",
					Type: "function",
					Function: message.ToolCallFunction{
						Name:      "search",
						Arguments: `{"q":"test"}`,
					},
				},
			},
		},
		{
			Role: message.RoleTool,
			ToolResult: &message.ToolResult{
				ToolCallID: "call_123",
				Content:    "result data",
			},
		},
	}

	result := convertMessages(msgs)

	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}

	if result[0].Role != "system" {
		t.Errorf("msg[0] Role = %q, want %q", result[0].Role, "system")
	}
	if result[1].Role != "user" {
		t.Errorf("msg[1] Role = %q, want %q", result[1].Role, "user")
	}
	if result[2].Role != "assistant" {
		t.Errorf("msg[2] Role = %q, want %q", result[2].Role, "assistant")
	}
	if len(result[2].ToolCalls) != 1 {
		t.Errorf("msg[2] ToolCalls len = %d, want 1", len(result[2].ToolCalls))
	}
	if result[3].ToolCallID != "call_123" {
		t.Errorf("msg[3] ToolCallID = %q, want %q", result[3].ToolCallID, "call_123")
	}
}

// ---------------------------------------------------------------------------
// Helper Functions
// ---------------------------------------------------------------------------

func strPtr(s string) *string {
	return &s
}
