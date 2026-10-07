package llamacpp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/user/orchestra/internal/config"
	"github.com/user/orchestra/internal/message"
	"github.com/user/orchestra/internal/provider"
)

func TestNewProvider_Defaults(t *testing.T) {
	p, err := NewProvider(config.ProviderConfig{})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	if p.Name() != "llamacpp" {
		t.Errorf("Name() = %q, want %q", p.Name(), "llamacpp")
	}
	if p.apiBaseURL != "http://localhost:8080/v1" {
		t.Errorf("apiBaseURL = %q, want %q", p.apiBaseURL, "http://localhost:8080/v1")
	}
}

func TestNewProvider_BaseURLVariants(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"http://h:8080", "http://h:8080/v1"},
		{"http://h:8080/", "http://h:8080/v1"},
		{"http://h:8080/v1", "http://h:8080/v1"},
		{"http://h:8080/v1/", "http://h:8080/v1"},
	}
	for _, c := range cases {
		p, err := NewProvider(config.ProviderConfig{BaseURL: c.in})
		if err != nil {
			t.Fatalf("NewProvider(%q) error = %v", c.in, err)
		}
		if p.apiBaseURL != c.want {
			t.Errorf("NewProvider(%q) apiBaseURL = %q, want %q", c.in, p.apiBaseURL, c.want)
		}
		if p.baseURL != strings.TrimRight(c.in, "/") {
			t.Errorf("NewProvider(%q) baseURL = %q, want %q", c.in, p.baseURL, strings.TrimRight(c.in, "/"))
		}
	}
}

func TestNewProvider_InvalidBaseURL(t *testing.T) {
	if _, err := NewProvider(config.ProviderConfig{BaseURL: "not a url"}); err == nil {
		t.Error("expected error for invalid base_url, got nil")
	}
}

func TestModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "TestModel-7B-Q4_K_M", "object": "model", "owned_by": "llamacpp"},
			},
		})
	}))
	defer srv.Close()

	p, err := NewProvider(config.ProviderConfig{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error = %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("Models() returned %d models, want 1", len(models))
	}
	if models[0].ID != "TestModel-7B-Q4_K_M" {
		t.Errorf("models[0].ID = %q, want %q", models[0].ID, "TestModel-7B-Q4_K_M")
	}
	if models[0].Capabilities.Streaming != true {
		t.Error("expected Streaming capability")
	}
}

func TestIsAvailable(t *testing.T) {
	t.Run("health endpoint", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"ok"}`))
				return
			}
			http.NotFound(w, r)
		}))
		defer srv.Close()

		p, err := NewProvider(config.ProviderConfig{BaseURL: srv.URL})
		if err != nil {
			t.Fatalf("NewProvider() error = %v", err)
		}
		if err := p.IsAvailable(context.Background()); err != nil {
			t.Errorf("IsAvailable() error = %v, want nil", err)
		}
	})

	t.Run("fallback to models endpoint", func(t *testing.T) {
		// Older llama-server builds have no /health route.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/models" {
				_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
				return
			}
			http.NotFound(w, r)
		}))
		defer srv.Close()

		p, err := NewProvider(config.ProviderConfig{BaseURL: srv.URL})
		if err != nil {
			t.Fatalf("NewProvider() error = %v", err)
		}
		if err := p.IsAvailable(context.Background()); err != nil {
			t.Errorf("IsAvailable() error = %v, want nil", err)
		}
	})

	t.Run("server down", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		url := srv.URL
		srv.Close()

		p, err := NewProvider(config.ProviderConfig{BaseURL: url})
		if err != nil {
			t.Fatalf("NewProvider() error = %v", err)
		}
		if err := p.IsAvailable(context.Background()); err == nil {
			t.Error("IsAvailable() error = nil, want error for unreachable server")
		}
	})
}

func TestGenerate_OpenAICompatibleChatCompletions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// llama-server serves whatever model is loaded; the request must
		// still carry a non-empty model field.
		if m, _ := req["model"].(string); m == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-1", "object": "chat.completion", "created": 1, "model": "TestModel",
			"choices": []map[string]any{
				{"index": 0, "message": map[string]any{"role": "assistant", "content": "hello from llama.cpp"}, "finish_reason": "stop"},
			},
			"usage": map[string]any{"prompt_tokens": 2, "completion_tokens": 5, "total_tokens": 7},
		})
	}))
	defer srv.Close()

	p, err := NewProvider(config.ProviderConfig{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	res, err := p.Generate(context.Background(), provider.GenerateRequest{
		Messages: []message.Message{message.UserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got := res.Text(); got != "hello from llama.cpp" {
		t.Errorf("Generate() text = %q, want %q", got, "hello from llama.cpp")
	}
}
