package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// newRuntimeTestModel builds a minimal AppModel wired for chat runtime tests
// without touching the user's home directory.
func newRuntimeTestModel(t *testing.T) *AppModel {
	t.Helper()
	theme := DefaultTheme()
	km := &KeyManager{keys: make(map[string]*ProviderKey), dir: t.TempDir()}
	store, err := NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSessionStore() error = %v", err)
	}
	m := &AppModel{
		Theme:           theme,
		KeyMap:          NewKeyMap(),
		CommandRegistry: NewCommandRegistry(),
		SessionStore:    store,
		Chat:            NewChatModel(theme, NewKeyMap()),
		Logs:            NewLogModel(theme, NewKeyMap()),
		Models:          NewModelsModel(theme, NewKeyMap(), km, NewDefaultModelFetcher()),
		KeyManager:      km,
		ActiveView:      ViewChat,
	}
	m.setupCommandHandlers()
	m.setupChatCallbacks()
	return m
}

// sseChunk builds one OpenAI-style streaming chunk.
func sseChunk(model, content, finish string) string {
	delta := map[string]any{}
	if content != "" {
		delta["content"] = content
	}
	chunk := map[string]any{
		"id": "chatcmpl-test", "object": "chat.completion.chunk", "created": 1, "model": model,
		"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}},
	}
	b, _ := json.Marshal(chunk)
	return "data: " + string(b) + "\n\n"
}

// driveStream pumps the tea.Cmd chain through AppModel.Update the same way
// the bubbletea runtime would, until the stream finishes.
func driveStream(t *testing.T, m *AppModel, first tea.Cmd) {
	t.Helper()
	cur := first()
	for i := 0; i < 100; i++ {
		switch evt := cur.(type) {
		case chatChunkMsg:
			_, next := m.Update(evt)
			cur = next()
		case chatThinkingMsg:
			_, next := m.Update(evt)
			cur = next()
		case chatToolTurnMsg:
			_, next := m.Update(evt)
			cur = next()
		case chatToolResultsMsg:
			_, next := m.Update(evt)
			cur = next()
		case chatStreamDoneMsg:
			_, _ = m.Update(evt)
			return
		case chatErrorMsg:
			t.Fatalf("unexpected stream error: %v", evt.err)
		default:
			t.Fatalf("unexpected message type %T", cur)
		}
	}
	t.Fatal("stream did not finish within 100 messages")
}

func TestStartChatGeneration_StreamsFromLlamaCpp(t *testing.T) {
	events := []string{
		sseChunk("test-model", "Hello", ""),
		sseChunk("test-model", " from", ""),
		sseChunk("test-model", " llama.cpp!", ""),
		`data: {"id":"chatcmpl-test","object":"chat.completion.chunk","created":1,"model":"test-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":4,"total_tokens":9}}` + "\n\n",
		"data: [DONE]\n\n",
	}

	var sawPath string
	var sawModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		sawModel = req.Model
		if len(req.Messages) == 0 || req.Messages[0].Role != "system" {
			t.Errorf("expected system message first, got %+v", req.Messages)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, evt := range events {
			fmt.Fprint(w, evt)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	t.Setenv("LLAMACPP_BASE_URL", srv.URL)

	m := newRuntimeTestModel(t)
	m.ProviderName = "llamacpp"
	m.ModelName = "test-model"
	m.SystemPrompt = "You are terse."

	// Simulate the user submitting a message (ChatModel does this on Enter).
	m.Chat.AddMessage(ViewUser, "hi")
	cmd := m.Chat.OnSubmit("hi")
	if cmd == nil {
		t.Fatal("OnSubmit returned nil cmd, expected a streaming generation")
	}
	if !m.Chat.IsStreaming {
		t.Error("expected chat to be in streaming state")
	}
	driveStream(t, m, cmd)

	if sawPath != "/v1/chat/completions" {
		t.Errorf("request path = %q, want /v1/chat/completions (a call must reach the model server)", sawPath)
	}
	if sawModel != "test-model" {
		t.Errorf("request model = %q, want %q", sawModel, "test-model")
	}

	// The streamed reply must have been added to the chat and session.
	if len(m.Chat.Messages) == 0 || m.Chat.Messages[len(m.Chat.Messages)-1].Type != ViewAssistant {
		t.Fatalf("expected final assistant message, got %+v", m.Chat.Messages)
	}
	last := m.Chat.Messages[len(m.Chat.Messages)-1]
	if last.Content != "Hello from llama.cpp!" {
		t.Errorf("assistant content = %q, want %q", last.Content, "Hello from llama.cpp!")
	}
	if last.TokenUsage == nil || last.TokenUsage.TotalTokens != 9 {
		t.Errorf("token usage not recorded: %+v", last.TokenUsage)
	}
	if m.Chat.IsStreaming || m.Chat.IsGenerating {
		t.Error("streaming/generating flags should be cleared after completion")
	}
}

func TestStartChatGeneration_NoSelectionNoServers(t *testing.T) {
	// Point the llama.cpp probe at a closed port so detection fails fast.
	t.Setenv("LLAMACPP_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("OLLAMA_BASE_URL", "http://127.0.0.1:1")

	m := newRuntimeTestModel(t)
	m.Chat.AddMessage(ViewUser, "hi")

	cmd := m.Chat.OnSubmit("hi")
	if cmd != nil {
		t.Fatal("expected nil cmd when no provider is available")
	}
	if m.Chat.IsGenerating || m.Chat.IsStreaming {
		t.Error("generating flags should be cleared on failure")
	}
	if len(m.Chat.Messages) == 0 || m.Chat.Messages[len(m.Chat.Messages)-1].Type != ViewError {
		t.Fatalf("expected an error message in chat, got %+v", m.Chat.Messages)
	}
	errMsg := m.Chat.Messages[len(m.Chat.Messages)-1].Content
	if !strings.Contains(errMsg, "no model selected") {
		t.Errorf("error message = %q, want guidance about selecting a model", errMsg)
	}
}

func TestStartChatGeneration_MissingAPIKey(t *testing.T) {
	m := newRuntimeTestModel(t)
	m.ProviderName = "openai"
	m.ModelName = "gpt-4o"
	m.Chat.AddMessage(ViewUser, "hi")

	cmd := m.Chat.OnSubmit("hi")
	if cmd != nil {
		t.Fatal("expected nil cmd when API key is missing")
	}
	last := m.Chat.Messages[len(m.Chat.Messages)-1]
	if last.Type != ViewError || !strings.Contains(last.Content, "no API key for OpenAI") {
		t.Errorf("expected missing-key error, got %+v", last)
	}
}

func TestSystemPromptCommand(t *testing.T) {
	m := newRuntimeTestModel(t)

	cmd := m.CommandRegistry.Parse("/system You are a pirate.")
	if cmd == nil {
		t.Fatal("Parse(/system ...) = nil")
	}
	if _, err := m.CommandRegistry.Execute(*cmd); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if m.SystemPrompt != "You are a pirate." {
		t.Errorf("SystemPrompt = %q, want %q", m.SystemPrompt, "You are a pirate.")
	}

	// Empty args now shows the current prompt.
	cmd = m.CommandRegistry.Parse("/system")
	if cmd == nil {
		t.Fatal("Parse(/system) = nil")
	}
	resp, err := m.CommandRegistry.Execute(*cmd)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(resp, "You are a pirate.") {
		t.Errorf("Execute(/system) = %q, want current prompt echoed", resp)
	}
}

func TestBuildChatProvider_KeylessProviders(t *testing.T) {
	// llama.cpp needs no key; base URL comes from the env var.
	t.Setenv("LLAMACPP_BASE_URL", "http://127.0.0.1:8080")
	p, err := buildChatProvider("llamacpp", nil)
	if err != nil {
		t.Fatalf("buildChatProvider(llamacpp) error = %v", err)
	}
	if p.Name() != "llamacpp" {
		t.Errorf("Name() = %q, want llamacpp", p.Name())
	}

	// Unknown provider errors.
	if _, err := buildChatProvider("doesnotexist", nil); err == nil {
		t.Error("expected error for unknown provider")
	}
}

// ensure tea import stays used.
var _ = context.Background
