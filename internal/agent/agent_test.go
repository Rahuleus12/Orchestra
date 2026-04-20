package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/user/orchestra/internal/message"
	"github.com/user/orchestra/internal/middleware"
	"github.com/user/orchestra/internal/provider"
	"github.com/user/orchestra/internal/provider/mock"
)

// ---------------------------------------------------------------------------
// Test Helpers
// ---------------------------------------------------------------------------

// newTestAgent creates a minimal agent for testing with sensible defaults.
func newTestAgent(t *testing.T, opts ...Option) *Agent {
	t.Helper()
	mp := mock.NewProvider("test")
	allOpts := append([]Option{WithProvider(mp, "mock-model")}, opts...)
	a, err := New("test-agent", allOpts...)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	return a
}

// newTestAgentWithMock creates an agent and returns both the agent and the mock provider.
func newTestAgentWithMock(t *testing.T, opts ...Option) (*Agent, *mock.Provider) {
	t.Helper()
	mp := mock.NewProvider("test")
	allOpts := append([]Option{WithProvider(mp, "mock-model")}, opts...)
	a, err := New("test-agent", allOpts...)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	return a, mp
}

// echoTool is a simple tool that echoes back its arguments.
type echoTool struct{}

func (echoTool) Name() string                                          { return "echo" }
func (echoTool) Description() string                                   { return "Echoes back the input arguments" }
func (echoTool) Parameters() map[string]any                            { return nil }
func (echoTool) Execute(_ context.Context, args string) (string, error) { return args, nil }

// errorTool always returns an error when executed.
type errorTool struct{}

func (errorTool) Name() string        { return "fail" }
func (errorTool) Description() string { return "Always fails" }
func (errorTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"message": map[string]any{"type": "string"},
		},
	}
}
func (errorTool) Execute(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("tool execution failed")
}

// slowTool takes a long time to execute, useful for cancellation tests.
type slowTool struct {
	duration time.Duration
}

func (s *slowTool) Name() string           { return "slow" }
func (s *slowTool) Description() string    { return "Takes a long time" }
func (s *slowTool) Parameters() map[string]any { return nil }
func (s *slowTool) Execute(ctx context.Context, _ string) (string, error) {
	select {
	case <-time.After(s.duration):
		return "done", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// countingTool counts how many times it's called.
type countingTool struct {
	count atomic.Int64
}

func (c *countingTool) Name() string           { return "counter" }
func (c *countingTool) Description() string    { return "Counts calls" }
func (c *countingTool) Parameters() map[string]any { return nil }
func (c *countingTool) Execute(_ context.Context, _ string) (string, error) {
	n := c.count.Add(1)
	return fmt.Sprintf("call #%d", n), nil
}
func (c *countingTool) Count() int64 { return c.count.Load() }

// ---------------------------------------------------------------------------
// Agent Creation Tests
// ---------------------------------------------------------------------------

func TestNew_BasicCreation(t *testing.T) {
	mp := mock.NewProvider("test")
	a, err := New("my-agent", WithProvider(mp, "gpt-4"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Name() != "my-agent" {
		t.Errorf("expected name 'my-agent', got %q", a.Name())
	}
	if a.ID() == "" {
		t.Error("expected non-empty ID")
	}
	if a.Provider() != mp {
		t.Error("provider mismatch")
	}
	if a.Model() != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got %q", a.Model())
	}
	if a.MaxTurns() != defaultMaxTurns {
		t.Errorf("expected default max turns %d, got %d", defaultMaxTurns, a.MaxTurns())
	}
}

func TestNew_EmptyName(t *testing.T) {
	mp := mock.NewProvider("test")
	_, err := New("", WithProvider(mp, "model"))
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNew_NilProvider(t *testing.T) {
	_, err := New("test", WithProvider(nil, "model"))
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
	if !strings.Contains(err.Error(), "provider must not be nil") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNew_MissingProvider(t *testing.T) {
	_, err := New("test")
	if err == nil {
		t.Fatal("expected error when no provider is configured")
	}
	if !strings.Contains(err.Error(), "provider is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNew_WithSystemPrompt(t *testing.T) {
	a := newTestAgent(t, WithSystemPrompt("You are {{.Role}}."))
	tmpl := a.SystemTemplate()
	if tmpl == nil {
		t.Fatal("expected system template to be set")
	}
	if tmpl.Name() != "system" {
		t.Errorf("expected template name 'system', got %q", tmpl.Name())
	}
}

func TestNew_WithSystemPrompt_InvalidTemplate(t *testing.T) {
	mp := mock.NewProvider("test")
	_, err := New("test", WithProvider(mp, "model"), WithSystemPrompt("{{.Unclosed"))
	if err == nil {
		t.Fatal("expected error for invalid template")
	}
	if !strings.Contains(err.Error(), "parse system prompt template") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNew_WithMaxTurns(t *testing.T) {
	a := newTestAgent(t, WithMaxTurns(5))
	if a.MaxTurns() != 5 {
		t.Errorf("expected max turns 5, got %d", a.MaxTurns())
	}
}

func TestNew_WithMaxTurns_Invalid(t *testing.T) {
	mp := mock.NewProvider("test")
	_, err := New("test", WithProvider(mp, "model"), WithMaxTurns(0))
	if err == nil {
		t.Fatal("expected error for max turns <= 0")
	}
	if !strings.Contains(err.Error(), "max turns must be positive") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNew_WithTools(t *testing.T) {
	a := newTestAgent(t, WithTools(&echoTool{}))
	if !a.HasTools() {
		t.Error("expected agent to have tools")
	}
	if a.ToolCount() != 1 {
		t.Errorf("expected 1 tool, got %d", a.ToolCount())
	}
}

func TestNew_WithTools_Multiple(t *testing.T) {
	a := newTestAgent(t, WithTools(&echoTool{}, &errorTool{}))
	if a.ToolCount() != 2 {
		t.Errorf("expected 2 tools, got %d", a.ToolCount())
	}
}

func TestNew_WithTools_DuplicateName(t *testing.T) {
	mp := mock.NewProvider("test")
	_, err := New("test",
		WithProvider(mp, "model"),
		WithTools(&echoTool{}, &echoTool{}),
	)
	if err == nil {
		t.Fatal("expected error for duplicate tool name")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNew_WithGenerateOptions(t *testing.T) {
	a := newTestAgent(t,
		WithGenerateOptions(provider.WithTemperature(0.7)),
		WithGenerateOptions(provider.WithMaxTokens(100)),
	)
	if a == nil {
		t.Fatal("expected agent to be created")
	}
}

func TestNew_WithMiddleware(t *testing.T) {
	logger := middleware.WithLogging(nil)
	a := newTestAgent(t, WithMiddleware(logger))
	if a == nil {
		t.Fatal("expected agent to be created")
	}
}

func TestNew_WithModel_DefaultModel(t *testing.T) {
	mp := mock.NewProvider("test")
	a, err := New("test", WithProvider(mp, ""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Model() != "default" {
		t.Errorf("expected 'default' model, got %q", a.Model())
	}
}

func TestNew_WithNilLogger(t *testing.T) {
	mp := mock.NewProvider("test")
	_, err := New("test", WithProvider(mp, "model"), WithLogger(nil))
	if err == nil {
		t.Fatal("expected error for nil logger")
	}
}

func TestNew_OptionsAppliedInOrder(t *testing.T) {
	var order []string
	mp := mock.NewProvider("test")

	captureOpt := func(name string) Option {
		return func(a *Agent) error {
			order = append(order, name)
			return nil
		}
	}

	_, err := New("test",
		WithProvider(mp, "model"),
		captureOpt("first"),
		captureOpt("second"),
		captureOpt("third"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 3 || order[0] != "first" || order[1] != "second" || order[2] != "third" {
		t.Errorf("options not applied in order: %v", order)
	}
}

// ---------------------------------------------------------------------------
// Agent Run Tests
// ---------------------------------------------------------------------------

func TestRun_SingleTurn(t *testing.T) {
	a, mp := newTestAgentWithMock(t)
	mp.SetDefaultResponse(mock.MockResponse{
		Message:      message.AssistantMessage("Hello!"),
		Usage:        provider.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		FinishReason: provider.FinishReasonStop,
	})

	result, err := a.Run(context.Background(), "Hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.FinalText() != "Hello!" {
		t.Errorf("expected 'Hello!', got %q", result.FinalText())
	}
	if result.Turns != 1 {
		t.Errorf("expected 1 turn, got %d", result.Turns)
	}
	if result.Usage.TotalTokens != 15 {
		t.Errorf("expected 15 total tokens, got %d", result.Usage.TotalTokens)
	}
	if result.Duration == 0 {
		t.Error("expected non-zero duration")
	}
	if result.HasToolCalls() {
		t.Error("expected no tool calls")
	}
	if result.Conversation == nil {
		t.Error("expected non-nil conversation")
	}
	if result.Conversation.Len() != 2 { // user + assistant
		t.Errorf("expected 2 messages, got %d", result.Conversation.Len())
	}
}

func TestRun_WithSystemPrompt(t *testing.T) {
	a, mp := newTestAgentWithMock(t,
		WithSystemPrompt("You are a {{.Role}}."),
		WithSystemData(map[string]any{"Role": "helper"}),
	)
	mp.SetDefaultResponse(mock.MockResponse{
		Message:      message.AssistantMessage("I'm a helper!"),
		Usage:        provider.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		FinishReason: provider.FinishReasonStop,
	})

	_, err := a.Run(context.Background(), "What are you?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := mp.GenerateCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 generate call, got %d", len(calls))
	}
	msgs := calls[0].Messages
	if len(msgs) < 1 {
		t.Fatal("expected at least 1 message")
	}
	if msgs[0].Role != message.RoleSystem {
		t.Errorf("expected first message role to be 'system', got %q", msgs[0].Role)
	}
	if msgs[0].Text() != "You are a helper." {
		t.Errorf("expected rendered system prompt 'You are a helper.', got %q", msgs[0].Text())
	}
}

func TestRun_ToolCallLoop(t *testing.T) {
	echo := &echoTool{}
	a, mp := newTestAgentWithMock(t, WithTools(echo), WithMaxTurns(5))

	mp.AddResponse(mock.MockResponse{
		Message: message.AssistantToolCallMessage([]message.ToolCall{
			{
				ID:   "call-1",
				Type: "function",
				Function: message.ToolCallFunction{
					Name:      "echo",
					Arguments: `{"message":"hello"}`,
				},
			},
		}),
		Usage:        provider.TokenUsage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30},
		FinishReason: provider.FinishReasonToolCall,
	})
	mp.AddResponse(mock.MockResponse{
		Message:      message.AssistantMessage("Echo said: hello"),
		Usage:        provider.TokenUsage{PromptTokens: 30, CompletionTokens: 10, TotalTokens: 40},
		FinishReason: provider.FinishReasonStop,
	})

	result, err := a.Run(context.Background(), "Call echo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Turns != 2 {
		t.Errorf("expected 2 turns, got %d", result.Turns)
	}
	if !result.HasToolCalls() {
		t.Error("expected tool calls")
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	tc := result.ToolCalls[0]
	if tc.Call.Function.Name != "echo" {
		t.Errorf("expected tool 'echo', got %q", tc.Call.Function.Name)
	}
	if tc.Result.Content != `{"message":"hello"}` {
		t.Errorf("unexpected tool result: %q", tc.Result.Content)
	}
	if tc.Result.IsError {
		t.Error("expected no tool error")
	}
	if tc.Turn != 0 {
		t.Errorf("expected turn 0, got %d", tc.Turn)
	}

	expectedTotal := 30 + 40 // 70
	if result.Usage.TotalTokens != expectedTotal {
		t.Errorf("expected %d total tokens, got %d", expectedTotal, result.Usage.TotalTokens)
	}

	if result.FinalText() != "Echo said: hello" {
		t.Errorf("unexpected final text: %q", result.FinalText())
	}
}

func TestRun_MultipleToolCallsInOneTurn(t *testing.T) {
	counter := &countingTool{}
	a, mp := newTestAgentWithMock(t, WithTools(counter), WithMaxTurns(5))

	mp.AddResponse(mock.MockResponse{
		Message: message.AssistantToolCallMessage([]message.ToolCall{
			{ID: "call-1", Type: "function", Function: message.ToolCallFunction{Name: "counter", Arguments: "{}"}},
			{ID: "call-2", Type: "function", Function: message.ToolCallFunction{Name: "counter", Arguments: "{}"}},
		}),
		Usage:        provider.TokenUsage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30},
		FinishReason: provider.FinishReasonToolCall,
	})
	mp.AddResponse(mock.MockResponse{
		Message:      message.AssistantMessage("Counted twice!"),
		Usage:        provider.TokenUsage{PromptTokens: 30, CompletionTokens: 10, TotalTokens: 40},
		FinishReason: provider.FinishReasonStop,
	})

	result, err := a.Run(context.Background(), "Count twice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(result.ToolCalls))
	}
	if counter.Count() != 2 {
		t.Errorf("expected counter to be called 2 times, got %d", counter.Count())
	}
}

func TestRun_ToolNotFound(t *testing.T) {
	a, mp := newTestAgentWithMock(t, WithMaxTurns(5)) // no tools registered

	mp.AddResponse(mock.MockResponse{
		Message: message.AssistantToolCallMessage([]message.ToolCall{
			{ID: "call-1", Type: "function", Function: message.ToolCallFunction{Name: "nonexistent", Arguments: "{}"}},
		}),
		Usage:        provider.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		FinishReason: provider.FinishReasonToolCall,
	})
	mp.AddResponse(mock.MockResponse{
		Message:      message.AssistantMessage("Tool not found, sorry."),
		Usage:        provider.TokenUsage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30},
		FinishReason: provider.FinishReasonStop,
	})

	result, err := a.Run(context.Background(), "Use nonexistent tool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if !result.ToolCalls[0].Result.IsError {
		t.Error("expected tool result to be an error")
	}
	if !strings.Contains(result.ToolCalls[0].Result.Content, "not found") {
		t.Errorf("unexpected error content: %q", result.ToolCalls[0].Result.Content)
	}
	if result.ToolCalls[0].Error == nil {
		t.Error("expected non-nil tool error")
	}
}

func TestRun_ToolExecutionError(t *testing.T) {
	a, mp := newTestAgentWithMock(t, WithTools(&errorTool{}), WithMaxTurns(5))

	mp.AddResponse(mock.MockResponse{
		Message: message.AssistantToolCallMessage([]message.ToolCall{
			{ID: "call-1", Type: "function", Function: message.ToolCallFunction{Name: "fail", Arguments: "{}"}},
		}),
		Usage:        provider.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		FinishReason: provider.FinishReasonToolCall,
	})
	mp.AddResponse(mock.MockResponse{
		Message:      message.AssistantMessage("The tool failed."),
		Usage:        provider.TokenUsage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30},
		FinishReason: provider.FinishReasonStop,
	})

	result, err := a.Run(context.Background(), "Use fail tool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if !result.ToolCalls[0].Result.IsError {
		t.Error("expected tool result to be an error")
	}
	if result.ToolCalls[0].Error == nil {
		t.Error("expected tool execution error")
	}
}

func TestRun_MaxTurnsExceeded(t *testing.T) {
	a, mp := newTestAgentWithMock(t, WithMaxTurns(3))

	// Every response is a tool call — this will loop until max turns
	mp.SetDefaultResponse(mock.MockResponse{
		Message: message.AssistantToolCallMessage([]message.ToolCall{
			{ID: "call-1", Type: "function", Function: message.ToolCallFunction{Name: "echo", Arguments: "{}"}},
		}),
		Usage:        provider.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		FinishReason: provider.FinishReasonToolCall,
	})

	// Register a tool so the agent tries to execute it
	registry := NewToolRegistry()
	registry.Register(&echoTool{})
	a.SetTools(registry)

	_, err := a.Run(context.Background(), "Loop forever")
	if err == nil {
		t.Fatal("expected max turns error")
	}

	var maxTurnsErr *MaxTurnsError
	if !errors.As(err, &maxTurnsErr) {
		t.Fatalf("expected MaxTurnsError, got %T: %v", err, err)
	}
	if maxTurnsErr.MaxTurns != 3 {
		t.Errorf("expected max turns 3, got %d", maxTurnsErr.MaxTurns)
	}
	if maxTurnsErr.Partial == nil {
		t.Error("expected partial result in MaxTurnsError")
	}
	if maxTurnsErr.Partial.Turns != 3 {
		t.Errorf("expected 3 partial turns, got %d", maxTurnsErr.Partial.Turns)
	}
}

func TestRun_ContextCancellation(t *testing.T) {
	a, mp := newTestAgentWithMock(t)
	mp.SetDefaultResponse(mock.MockResponse{
		Message:      message.AssistantMessage("response"),
		Usage:        provider.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		FinishReason: provider.FinishReasonStop,
		Delay:        5 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := a.Run(ctx, "Hello")
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestRun_ContextAlreadyCancelled(t *testing.T) {
	a, mp := newTestAgentWithMock(t)
	mp.SetDefaultResponse(mock.MockResponse{
		Message:      message.AssistantMessage("response"),
		Usage:        provider.TokenUsage{},
		FinishReason: provider.FinishReasonStop,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := a.Run(ctx, "Hello")
	if err == nil {
		t.Fatal("expected error for already cancelled context")
	}
}

func TestRun_ProviderError(t *testing.T) {
	a, mp := newTestAgentWithMock(t)
	mp.SetDefaultResponse(mock.MockResponse{
		Error: fmt.Errorf("provider API error"),
	})

	_, err := a.Run(context.Background(), "Hello")
	if err == nil {
		t.Fatal("expected provider error")
	}
	if !strings.Contains(err.Error(), "provider API error") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// RunConversation Tests
// ---------------------------------------------------------------------------

func TestRunConversation_SingleTurn(t *testing.T) {
	a, mp := newTestAgentWithMock(t)
	mp.SetDefaultResponse(mock.MockResponse{
		Message:      message.AssistantMessage("Hi there!"),
		Usage:        provider.TokenUsage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
		FinishReason: provider.FinishReasonStop,
	})

	msgs := []message.Message{
		message.SystemMessage("You are helpful."),
		message.UserMessage("Hello"),
	}

	result, err := a.RunConversation(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.FinalText() != "Hi there!" {
		t.Errorf("expected 'Hi there!', got %q", result.FinalText())
	}
	if result.Turns != 1 {
		t.Errorf("expected 1 turn, got %d", result.Turns)
	}
}

func TestRunConversation_DoesNotPrependSystem(t *testing.T) {
	a, mp := newTestAgentWithMock(t)
	mp.SetDefaultResponse(mock.MockResponse{
		Message:      message.AssistantMessage("ok"),
		Usage:        provider.TokenUsage{},
		FinishReason: provider.FinishReasonStop,
	})

	msgs := []message.Message{message.UserMessage("Hello")}

	_, err := a.RunConversation(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := mp.GenerateCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if len(calls[0].Messages) != 1 {
		t.Errorf("expected 1 message in request, got %d", len(calls[0].Messages))
	}
}

func TestRunConversation_WithToolCalls(t *testing.T) {
	counter := &countingTool{}
	a, mp := newTestAgentWithMock(t, WithTools(counter), WithMaxTurns(5))

	mp.AddResponse(mock.MockResponse{
		Message: message.AssistantToolCallMessage([]message.ToolCall{
			{ID: "c1", Type: "function", Function: message.ToolCallFunction{Name: "counter", Arguments: "{}"}},
		}),
		Usage:        provider.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		FinishReason: provider.FinishReasonToolCall,
	})
	mp.AddResponse(mock.MockResponse{
		Message:      message.AssistantMessage("Done!"),
		Usage:        provider.TokenUsage{PromptTokens: 15, CompletionTokens: 5, TotalTokens: 20},
		FinishReason: provider.FinishReasonStop,
	})

	msgs := []message.Message{message.UserMessage("Count")}
	result, err := a.RunConversation(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Turns != 2 {
		t.Errorf("expected 2 turns, got %d", result.Turns)
	}
	if len(result.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
}

// ---------------------------------------------------------------------------
// Stream Tests
// ---------------------------------------------------------------------------

func TestStream_SingleTurn(t *testing.T) {
	a, mp := newTestAgentWithMock(t)
	mp.SetDefaultStreamChunks([]mock.StreamChunk{
		{Type: provider.StreamEventStart},
		{Type: provider.StreamEventChunk, Chunk: "Hello "},
		{Type: provider.StreamEventChunk, Chunk: "world!"},
		{Type: provider.StreamEventDone, Usage: &provider.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}},
	})

	events, err := a.Stream(context.Background(), "Hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var collectedText string
	var gotDone bool
	var usage *provider.TokenUsage

	for evt := range events {
		switch evt.Type {
		case EventGenerateChunk:
			collectedText += evt.Chunk
		case EventDone:
			gotDone = true
			usage = evt.Usage
		case EventError:
			t.Fatalf("unexpected error event: %v", evt.Error)
		}
	}

	if !gotDone {
		t.Error("expected EventDone")
	}
	if collectedText != "Hello world!" {
		t.Errorf("expected 'Hello world!', got %q", collectedText)
	}
	if usage == nil || usage.TotalTokens != 15 {
		t.Errorf("unexpected usage: %v", usage)
	}
}

func TestStream_WithToolCalls(t *testing.T) {
	counter := &countingTool{}
	a, mp := newTestAgentWithMock(t, WithTools(counter), WithMaxTurns(5))

	mp.AddStreamChunks([]mock.StreamChunk{
		{Type: provider.StreamEventStart},
		{Type: provider.StreamEventToolCall, ToolCall: &message.ToolCall{
			ID: "c1", Type: "function",
			Function: message.ToolCallFunction{Name: "counter", Arguments: "{}"},
		}},
		{Type: provider.StreamEventDone, Usage: &provider.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}},
	})
	mp.AddStreamChunks([]mock.StreamChunk{
		{Type: provider.StreamEventStart},
		{Type: provider.StreamEventChunk, Chunk: "Counted!"},
		{Type: provider.StreamEventDone, Usage: &provider.TokenUsage{PromptTokens: 15, CompletionTokens: 5, TotalTokens: 20}},
	})

	events, err := a.Stream(context.Background(), "Count")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var eventTypes []AgentEventType
	for evt := range events {
		eventTypes = append(eventTypes, evt.Type)
		if evt.Type == EventError {
			t.Fatalf("unexpected error: %v", evt.Error)
		}
	}

	expectedSequence := []AgentEventType{
		EventGenerateStart,
		EventGenerateDone,
		EventToolCallStart,
		EventToolCallEnd,
		EventGenerateStart,
		EventGenerateDone,
		EventDone,
	}
	if len(eventTypes) != len(expectedSequence) {
		t.Fatalf("expected %d events, got %d: %v", len(expectedSequence), len(eventTypes), eventTypes)
	}
	for i, expected := range expectedSequence {
		if eventTypes[i] != expected {
			t.Errorf("event %d: expected %q, got %q", i, expected, eventTypes[i])
		}
	}
}

func TestStream_ContextCancellation(t *testing.T) {
	a, mp := newTestAgentWithMock(t)
	mp.SetDefaultStreamChunks([]mock.StreamChunk{
		{Type: provider.StreamEventStart},
		{Type: provider.StreamEventChunk, Chunk: "Hello"},
	})
	mp.SetStreamDelay(5 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	events, err := a.Stream(ctx, "Hi")
	if err != nil {
		t.Fatalf("unexpected setup error: %v", err)
	}

	var gotError bool
	for evt := range events {
		if evt.Type == EventError {
			gotError = true
		}
	}
	if !gotError {
		t.Error("expected error event due to context cancellation")
	}
}

func TestStream_ProviderStreamError(t *testing.T) {
	a, mp := newTestAgentWithMock(t)
	mp.SetDefaultStreamChunks([]mock.StreamChunk{
		{Type: provider.StreamEventStart},
		{Type: provider.StreamEventError, Error: fmt.Errorf("stream error")},
	})

	events, err := a.Stream(context.Background(), "Hi")
	if err != nil {
		t.Fatalf("unexpected setup error: %v", err)
	}

	var gotError bool
	for evt := range events {
		if evt.Type == EventError {
			gotError = true
		}
	}
	if !gotError {
		t.Error("expected error event")
	}
}

func TestStream_MaxTurnsExceeded(t *testing.T) {
	counter := &countingTool{}
	a, mp := newTestAgentWithMock(t, WithTools(counter), WithMaxTurns(2))

	mp.SetDefaultStreamChunks([]mock.StreamChunk{
		{Type: provider.StreamEventStart},
		{Type: provider.StreamEventToolCall, ToolCall: &message.ToolCall{
			ID: "c1", Type: "function",
			Function: message.ToolCallFunction{Name: "counter", Arguments: "{}"},
		}},
		{Type: provider.StreamEventDone, Usage: &provider.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}},
	})

	events, err := a.Stream(context.Background(), "Loop")
	if err != nil {
		t.Fatalf("unexpected setup error: %v", err)
	}

	var gotError bool
	var errorMsg string
	for evt := range events {
		if evt.Type == EventError {
			gotError = true
			if evt.Error != nil {
				errorMsg = evt.Error.Error()
			}
		}
	}
	if !gotError {
		t.Error("expected error event due to max turns")
	}
	if !strings.Contains(errorMsg, "max turns") {
		t.Errorf("expected max turns error, got: %s", errorMsg)
	}
}

// ---------------------------------------------------------------------------
// Clone Tests
// ---------------------------------------------------------------------------

func TestClone_Basic(t *testing.T) {
	a, mp := newTestAgentWithMock(t,
		WithSystemPrompt("You are helpful."),
		WithTools(&echoTool{}),
		WithMaxTurns(10),
	)

	clone := a.Clone("clone-1")
	if clone.Name() != "clone-1" {
		t.Errorf("expected name 'clone-1', got %q", clone.Name())
	}
	if clone.ID() == a.ID() {
		t.Error("clone should have a different ID")
	}
	if clone.Provider() != mp {
		t.Error("clone should share the same provider")
	}
	if clone.Model() != a.Model() {
		t.Error("clone should have the same model")
	}
	if clone.MaxTurns() != a.MaxTurns() {
		t.Error("clone should have the same max turns")
	}
}

func TestClone_EmptyName(t *testing.T) {
	a := newTestAgent(t)
	clone := a.Clone("")
	if clone.Name() != "test-agent-clone" {
		t.Errorf("expected default clone name 'test-agent-clone', got %q", clone.Name())
	}
}

func TestClone_IndependentExecution(t *testing.T) {
	a, mp := newTestAgentWithMock(t)
	mp.SetDefaultResponse(mock.MockResponse{
		Message:      message.AssistantMessage("response"),
		Usage:        provider.TokenUsage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
		FinishReason: provider.FinishReasonStop,
	})

	clone := a.Clone("clone")

	var wg sync.WaitGroup
	wg.Add(2)

	var aErr, cErr error
	go func() {
		defer wg.Done()
		_, aErr = a.Run(context.Background(), "msg1")
	}()
	go func() {
		defer wg.Done()
		_, cErr = clone.Run(context.Background(), "msg2")
	}()
	wg.Wait()

	if aErr != nil {
		t.Errorf("agent error: %v", aErr)
	}
	if cErr != nil {
		t.Errorf("clone error: %v", cErr)
	}

	if mp.CallCount() != 2 {
		t.Errorf("expected 2 total calls, got %d", mp.CallCount())
	}
}

// ---------------------------------------------------------------------------
// Accessor / Setter Tests
// ---------------------------------------------------------------------------

func TestAgent_SetModel(t *testing.T) {
	a := newTestAgent(t)
	a.SetModel("new-model")
	if a.Model() != "new-model" {
		t.Errorf("expected 'new-model', got %q", a.Model())
	}
}

func TestAgent_SetMaxTurns(t *testing.T) {
	a := newTestAgent(t)
	a.SetMaxTurns(42)
	if a.MaxTurns() != 42 {
		t.Errorf("expected 42, got %d", a.MaxTurns())
	}
}

func TestAgent_SetMaxTurns_Invalid(t *testing.T) {
	a := newTestAgent(t)
	original := a.MaxTurns()
	a.SetMaxTurns(0)
	if a.MaxTurns() != original {
		t.Errorf("max turns should not change for invalid value")
	}
	a.SetMaxTurns(-1)
	if a.MaxTurns() != original {
		t.Errorf("max turns should not change for negative value")
	}
}

func TestAgent_SetSystemData(t *testing.T) {
	a := newTestAgent(t, WithSystemPrompt("Role: {{.Role}}"))
	a.SetSystemData(map[string]any{"Role": "assistant"})
}

func TestAgent_HasTools_NoTools(t *testing.T) {
	a := newTestAgent(t)
	if a.HasTools() {
		t.Error("expected no tools")
	}
	if a.ToolCount() != 0 {
		t.Errorf("expected 0 tools, got %d", a.ToolCount())
	}
}

// ---------------------------------------------------------------------------
// Prompt Template Tests
// ---------------------------------------------------------------------------

func TestNewTemplate_Basic(t *testing.T) {
	tmpl, err := NewTemplate("test", "Hello, {{.Name}}!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tmpl.Name() != "test" {
		t.Errorf("expected name 'test', got %q", tmpl.Name())
	}

	result, err := tmpl.Execute(map[string]any{"Name": "World"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello, World!" {
		t.Errorf("expected 'Hello, World!', got %q", result)
	}
}

func TestNewTemplate_InvalidSyntax(t *testing.T) {
	_, err := NewTemplate("bad", "{{.Unclosed")
	if err == nil {
		t.Fatal("expected error for invalid template syntax")
	}
}

func TestNewTemplate_EmptyName(t *testing.T) {
	tmpl, err := NewTemplate("", "Hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tmpl.Name() != "unnamed" {
		t.Errorf("expected 'unnamed', got %q", tmpl.Name())
	}
}

func TestMustTemplate_Panics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic")
		}
	}()
	MustTemplate("bad", "{{.Unclosed")
}

func TestMustTemplate_Success(t *testing.T) {
	tmpl := MustTemplate("ok", "Hello {{.Name}}")
	result := tmpl.MustExecute(map[string]any{"Name": "World"})
	if result != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", result)
	}
}

func TestTemplate_ExecuteMap(t *testing.T) {
	tmpl := MustTemplate("test", "Value: {{.val}}")
	result, err := tmpl.ExecuteMap(map[string]any{"val": 42})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Value: 42" {
		t.Errorf("expected 'Value: 42', got %q", result)
	}
}

func TestTemplate_Clone(t *testing.T) {
	tmpl := MustTemplate("base", "Hello {{.Name}}")
	cloned, err := tmpl.Clone()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cloned.Name() != tmpl.Name() {
		t.Error("clone should have same name")
	}
	result, err := cloned.Execute(map[string]any{"Name": "Clone"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello Clone" {
		t.Errorf("expected 'Hello Clone', got %q", result)
	}
}

func TestTemplate_Source(t *testing.T) {
	src := "Hello {{.Name}}"
	tmpl := MustTemplate("test", src)
	if tmpl.Source() != src {
		t.Errorf("expected source %q, got %q", src, tmpl.Source())
	}
}

func TestTemplate_AddBlock(t *testing.T) {
	tmpl := MustTemplate("base", `{{template "greeting" .}}`)
	err := tmpl.AddBlock("greeting", "Hello, {{.Name}}!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, err := tmpl.Execute(map[string]any{"Name": "World"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello, World!" {
		t.Errorf("expected 'Hello, World!', got %q", result)
	}
}

func TestTemplate_BuiltinFunc_JSON(t *testing.T) {
	tmpl := MustTemplate("test", `{{json .}}`)
	result, err := tmpl.Execute(map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"key"`) || !strings.Contains(result, `"value"`) {
		t.Errorf("unexpected json output: %q", result)
	}
}

func TestTemplate_BuiltinFunc_JSONPretty(t *testing.T) {
	tmpl := MustTemplate("test", `{{json_pretty .}}`)
	result, err := tmpl.Execute(map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "\n") {
		t.Errorf("expected pretty JSON with newlines, got: %q", result)
	}
}

func TestTemplate_BuiltinFunc_YAML(t *testing.T) {
	tmpl := MustTemplate("test", `{{yaml .}}`)
	result, err := tmpl.Execute(map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "key: value") {
		t.Errorf("unexpected yaml output: %q", result)
	}
}

func TestTemplate_BuiltinFunc_Indent(t *testing.T) {
	tmpl := MustTemplate("test", `{{indent 4 .Text}}`)
	result, err := tmpl.Execute(map[string]any{"Text": "line1\nline2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "    line1\n    line2" {
		t.Errorf("unexpected indent output: %q", result)
	}
}

func TestTemplate_BuiltinFunc_Default(t *testing.T) {
	tmpl := MustTemplate("test", `{{default "fallback" .Value}}`)

	result1, _ := tmpl.Execute(map[string]any{"Value": ""})
	if result1 != "fallback" {
		t.Errorf("expected 'fallback', got %q", result1)
	}

	result2, _ := tmpl.Execute(map[string]any{"Value": "actual"})
	if result2 != "actual" {
		t.Errorf("expected 'actual', got %q", result2)
	}
}

func TestTemplate_BuiltinFunc_Coalesce(t *testing.T) {
	tmpl := MustTemplate("test", `{{coalesce .A .B .C}}`)

	result, _ := tmpl.Execute(map[string]any{"A": "", "B": "", "C": "third"})
	if result != "third" {
		t.Errorf("expected 'third', got %q", result)
	}

	result2, _ := tmpl.Execute(map[string]any{"A": "first", "B": "", "C": "third"})
	if result2 != "first" {
		t.Errorf("expected 'first', got %q", result2)
	}
}

func TestTemplate_BuiltinFunc_Truncate(t *testing.T) {
	tmpl := MustTemplate("test", `{{truncate 8 .Text}}`)

	result, _ := tmpl.Execute(map[string]any{"Text": "Hello, World!"})
	if result != "Hello..." {
		t.Errorf("expected 'Hello...', got %q", result)
	}

	result2, _ := tmpl.Execute(map[string]any{"Text": "Short"})
	if result2 != "Short" {
		t.Errorf("expected 'Short', got %q", result2)
	}
}

func TestTemplate_BuiltinFunc_UpperLowerTitle(t *testing.T) {
	tmpl := MustTemplate("test", `{{upper .Text}} {{lower .Text}} {{title .Text}}`)
	result, _ := tmpl.Execute(map[string]any{"Text": "hello world"})
	if result != "HELLO WORLD hello world Hello World" {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestTemplate_BuiltinFunc_Trim(t *testing.T) {
	tmpl := MustTemplate("test", `{{trim .Text}}`)
	result, _ := tmpl.Execute(map[string]any{"Text": "  hello  "})
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestTemplate_BuiltinFunc_Contains(t *testing.T) {
	tmpl := MustTemplate("test", `{{contains "world" .Text}}`)
	result, _ := tmpl.Execute(map[string]any{"Text": "hello world"})
	if result != "true" {
		t.Errorf("expected 'true', got %q", result)
	}
}

func TestTemplate_BuiltinFunc_Replace(t *testing.T) {
	tmpl := MustTemplate("test", `{{replace "world" "Go" .Text}}`)
	result, _ := tmpl.Execute(map[string]any{"Text": "hello world"})
	if result != "hello Go" {
		t.Errorf("expected 'hello Go', got %q", result)
	}
}

func TestTemplate_BuiltinFunc_Join(t *testing.T) {
	tmpl := MustTemplate("test", `{{join ", " .Items}}`)
	result, _ := tmpl.Execute(map[string]any{"Items": []string{"a", "b", "c"}})
	if result != "a, b, c" {
		t.Errorf("expected 'a, b, c', got %q", result)
	}
}

func TestTemplate_BuiltinFunc_Split(t *testing.T) {
	tmpl := MustTemplate("test", `{{len (split "," .Text)}}`)
	result, _ := tmpl.Execute(map[string]any{"Text": "a,b,c"})
	if result != "3" {
		t.Errorf("expected '3', got %q", result)
	}
}

func TestTemplate_BuiltinFunc_Wrap(t *testing.T) {
	tmpl := MustTemplate("test", `{{wrap 10 .Text}}`)
	result, _ := tmpl.Execute(map[string]any{"Text": "This is a long line"})
	if len(strings.Split(result, "\n")) < 2 {
		t.Errorf("expected wrapped text with newlines, got: %q", result)
	}
}

func TestTemplate_ConditionalBlocks(t *testing.T) {
	tmpl := MustTemplate("cond", `{{if .Show}}visible{{else}}hidden{{end}}`)

	r1, _ := tmpl.Execute(map[string]any{"Show": true})
	if r1 != "visible" {
		t.Errorf("expected 'visible', got %q", r1)
	}

	r2, _ := tmpl.Execute(map[string]any{"Show": false})
	if r2 != "hidden" {
		t.Errorf("expected 'hidden', got %q", r2)
	}
}

func TestTemplate_Loops(t *testing.T) {
	tmpl := MustTemplate("loop", `{{range .Items}}- {{.}}
{{end}}`)
	result, _ := tmpl.Execute(map[string]any{"Items": []string{"a", "b", "c"}})
	if !strings.Contains(result, "- a") || !strings.Contains(result, "- b") {
		t.Errorf("unexpected loop output: %q", result)
	}
}

// ---------------------------------------------------------------------------
// Template Registry Tests
// ---------------------------------------------------------------------------

func TestTemplateRegistry_Basic(t *testing.T) {
	reg := NewTemplateRegistry()
	tmpl := MustTemplate("greeting", "Hello {{.Name}}!")

	if err := reg.Register(tmpl); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := reg.Get("greeting")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name() != "greeting" {
		t.Errorf("expected 'greeting', got %q", got.Name())
	}
}

func TestTemplateRegistry_Duplicate(t *testing.T) {
	reg := NewTemplateRegistry()
	reg.Register(MustTemplate("dup", "first"))
	err := reg.Register(MustTemplate("dup", "second"))
	if err == nil {
		t.Error("expected error for duplicate registration")
	}
}

func TestTemplateRegistry_NotFound(t *testing.T) {
	reg := NewTemplateRegistry()
	_, err := reg.Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent template")
	}
}

func TestTemplateRegistry_MustGet_Panics(t *testing.T) {
	reg := NewTemplateRegistry()
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic")
		}
	}()
	reg.MustGet("nonexistent")
}

func TestTemplateRegistry_LazyLoading(t *testing.T) {
	reg := NewTemplateRegistry()
	loaded := false
	err := reg.RegisterLazy("lazy", func() (*Template, error) {
		loaded = true
		return NewTemplate("lazy", "Lazy: {{.Val}}")
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if loaded {
		t.Error("template should not be loaded yet")
	}

	tmpl, err := reg.Get("lazy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !loaded {
		t.Error("template should now be loaded")
	}

	result, err := tmpl.Execute(map[string]any{"Val": "yes"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Lazy: yes" {
		t.Errorf("expected 'Lazy: yes', got %q", result)
	}

	// Second Get should not call factory again
	loaded = false
	_, _ = reg.Get("lazy")
}

func TestTemplateRegistry_List(t *testing.T) {
	reg := NewTemplateRegistry()
	reg.Register(MustTemplate("a", "a"))
	reg.Register(MustTemplate("b", "b"))
	reg.RegisterLazy("c", func() (*Template, error) { return NewTemplate("c", "c") })

	names := reg.List()
	if len(names) != 3 {
		t.Errorf("expected 3 names, got %d: %v", len(names), names)
	}
}

func TestTemplateRegistry_LoadFromFS(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "greeting.tmpl"), []byte("Hello, {{.Name}}!"), 0o644)
	os.WriteFile(filepath.Join(dir, "farewell.tmpl"), []byte("Goodbye, {{.Name}}!"), 0o644)
	os.WriteFile(filepath.Join(dir, "notatemplate.json"), []byte("{}"), 0o644)

	reg := NewTemplateRegistry()
	err := reg.LoadFromFS(os.DirFS(dir), ".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := reg.List()
	if len(names) != 2 {
		t.Errorf("expected 2 templates, got %d: %v", len(names), names)
	}

	tmpl, err := reg.Get("greeting")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, err := tmpl.Execute(map[string]any{"Name": "World"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello, World!" {
		t.Errorf("expected 'Hello, World!', got %q", result)
	}
}

func TestTemplateRegistry_LoadFromFS_NestedDirs(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "prompts", "agents")
	os.MkdirAll(subDir, 0o755)
	os.WriteFile(filepath.Join(subDir, "helper.tmpl"), []byte("Help: {{.Task}}"), 0o644)

	reg := NewTemplateRegistry()
	err := reg.LoadFromFS(os.DirFS(dir), "prompts")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tmpl, err := reg.Get("helper")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, err := tmpl.Execute(map[string]any{"Task": "write code"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Help: write code" {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestLoadTemplateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.tmpl")
	os.WriteFile(path, []byte("Hello, {{.Name}}!"), 0o644)

	tmpl, err := LoadTemplateFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tmpl.Name() != "test" {
		t.Errorf("expected name 'test', got %q", tmpl.Name())
	}

	result, err := tmpl.Execute(map[string]any{"Name": "World"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello, World!" {
		t.Errorf("expected 'Hello, World!', got %q", result)
	}
}

func TestLoadTemplateFile_NotFound(t *testing.T) {
	_, err := LoadTemplateFile("/nonexistent/path.tmpl")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadTemplateFS(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "prompt.tmpl"), []byte("Say {{.Word}}"), 0o644)

	tmpl, err := LoadTemplateFS(os.DirFS(dir), "prompt.tmpl")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tmpl.Name() != "prompt" {
		t.Errorf("expected name 'prompt', got %q", tmpl.Name())
	}
}

// ---------------------------------------------------------------------------
// Tool Tests
// ---------------------------------------------------------------------------

func TestToolFunc_Basic(t *testing.T) {
	tool := NewToolFunc("add", "Adds numbers", func(_ context.Context, args string) (string, error) {
		var input struct{ A, B int }
		json.Unmarshal([]byte(args), &input)
		return fmt.Sprintf("%d", input.A+input.B), nil
	})

	if tool.Name() != "add" {
		t.Errorf("expected 'add', got %q", tool.Name())
	}
	if tool.Description() != "Adds numbers" {
		t.Errorf("unexpected description: %q", tool.Description())
	}

	result, err := tool.Execute(context.Background(), `{"A": 3, "B": 4}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "7" {
		t.Errorf("expected '7', got %q", result)
	}
}

func TestToolFuncWithSchema(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
		},
	}
	tool := NewToolFuncWithSchema("search", "Search docs", schema, func(_ context.Context, args string) (string, error) {
		return "found: " + args, nil
	})
	if tool.Parameters() == nil {
		t.Error("expected non-nil parameters")
	}
}

func TestToolRegistry_Basic(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&echoTool{})

	if reg.Size() != 1 {
		t.Errorf("expected size 1, got %d", reg.Size())
	}
	if !reg.Has("echo") {
		t.Error("expected 'echo' to be registered")
	}
	if reg.Has("nonexistent") {
		t.Error("expected 'nonexistent' to not be registered")
	}

	tool, err := reg.Get("echo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool.Name() != "echo" {
		t.Errorf("expected 'echo', got %q", tool.Name())
	}
}

func TestToolRegistry_Duplicate(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&echoTool{})
	err := reg.Register(&echoTool{})
	if err == nil {
		t.Error("expected error for duplicate tool")
	}
}

func TestToolRegistry_MustRegister(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic")
		}
	}()
	reg := NewToolRegistry()
	reg.Register(&echoTool{})
	reg.MustRegister(&echoTool{})
}

func TestToolRegistry_NotFound(t *testing.T) {
	reg := NewToolRegistry()
	_, err := reg.Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent tool")
	}
}

func TestToolRegistry_List(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&echoTool{})
	reg.Register(&errorTool{})

	tools := reg.List()
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
}

func TestToolRegistry_Names(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&echoTool{})
	reg.Register(&errorTool{})

	names := reg.Names()
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %d", len(names))
	}
}

func TestToolRegistry_Definitions(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&errorTool{})

	defs := reg.Definitions()
	if len(defs) != 1 {
		t.Fatalf("expected 1 definition, got %d", len(defs))
	}
	if defs[0].Type != "function" {
		t.Errorf("expected type 'function', got %q", defs[0].Type)
	}
	if defs[0].Function.Name != "fail" {
		t.Errorf("expected function name 'fail', got %q", defs[0].Function.Name)
	}
}

func TestToolRegistry_Clear(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&echoTool{})
	reg.Clear()
	if reg.Size() != 0 {
		t.Errorf("expected 0 after clear, got %d", reg.Size())
	}
}

func TestToolRegistry_ExecuteTool(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&echoTool{})

	result, err := reg.ExecuteTool(context.Background(), "echo", `{"msg":"hi"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != `{"msg":"hi"}` {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestToolRegistry_ExecuteTool_NotFound(t *testing.T) {
	reg := NewToolRegistry()
	_, err := reg.ExecuteTool(context.Background(), "nonexistent", "{}")
	if err == nil {
		t.Error("expected error")
	}
}

func TestToolDefinition(t *testing.T) {
	def := ToolDefinition(&errorTool{})
	if def.Type != "function" {
		t.Errorf("expected 'function', got %q", def.Type)
	}
	if def.Function.Name != "fail" {
		t.Errorf("expected 'fail', got %q", def.Function.Name)
	}
	if def.Function.Description != "Always fails" {
		t.Errorf("unexpected description: %q", def.Function.Description)
	}
}

func TestParseArguments(t *testing.T) {
	type Input struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	result, err := ParseArguments[Input](`{"name":"Alice","age":30}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Name != "Alice" || result.Age != 30 {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestParseArguments_InvalidJSON(t *testing.T) {
	_, err := ParseArguments[map[string]any](`{invalid json}`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// Tool Execution in Agent Context
// ---------------------------------------------------------------------------

func TestAgent_MultiTurnToolLoop(t *testing.T) {
	counter := &countingTool{}
	a, mp := newTestAgentWithMock(t, WithTools(counter), WithMaxTurns(10))

	mp.AddResponse(mock.MockResponse{
		Message: message.AssistantToolCallMessage([]message.ToolCall{
			{ID: "c1", Type: "function", Function: message.ToolCallFunction{Name: "counter", Arguments: "{}"}},
		}),
		Usage:        provider.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		FinishReason: provider.FinishReasonToolCall,
	})
	mp.AddResponse(mock.MockResponse{
		Message: message.AssistantToolCallMessage([]message.ToolCall{
			{ID: "c2", Type: "function", Function: message.ToolCallFunction{Name: "counter", Arguments: "{}"}},
		}),
		Usage:        provider.TokenUsage{PromptTokens: 20, CompletionTokens: 5, TotalTokens: 25},
		FinishReason: provider.FinishReasonToolCall,
	})
	mp.AddResponse(mock.MockResponse{
		Message:      message.AssistantMessage("Called counter 2 times!"),
		Usage:        provider.TokenUsage{PromptTokens: 30, CompletionTokens: 10, TotalTokens: 40},
		FinishReason: provider.FinishReasonStop,
	})

	result, err := a.Run(context.Background(), "Count twice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Turns != 3 {
		t.Errorf("expected 3 turns, got %d", result.Turns)
	}
	if len(result.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(result.ToolCalls))
	}
	if counter.Count() != 2 {
		t.Errorf("expected 2 counter calls, got %d", counter.Count())
	}
	if result.Usage.TotalTokens != 15+25+40 {
		t.Errorf("expected %d total tokens, got %d", 15+25+40, result.Usage.TotalTokens)
	}

	turn0Calls := result.ToolCallsByTurn(0)
	turn1Calls := result.ToolCallsByTurn(1)
	if len(turn0Calls) != 1 {
		t.Errorf("expected 1 tool call in turn 0, got %d", len(turn0Calls))
	}
	if len(turn1Calls) != 1 {
		t.Errorf("expected 1 tool call in turn 1, got %d", len(turn1Calls))
	}
}

func TestAgent_RunIncludesToolResultsInConversation(t *testing.T) {
	a, mp := newTestAgentWithMock(t, WithTools(&echoTool{}), WithMaxTurns(5))

	mp.AddResponse(mock.MockResponse{
		Message: message.AssistantToolCallMessage([]message.ToolCall{
			{ID: "c1", Type: "function", Function: message.ToolCallFunction{Name: "echo", Arguments: `"hello"`}},
		}),
		Usage:        provider.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		FinishReason: provider.FinishReasonToolCall,
	})
	mp.AddResponse(mock.MockResponse{
		Message:      message.AssistantMessage("Done"),
		Usage:        provider.TokenUsage{PromptTokens: 20, CompletionTokens: 5, TotalTokens: 25},
		FinishReason: provider.FinishReasonStop,
	})

	result, err := a.Run(context.Background(), "Test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := result.Conversation.Messages
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
	if msgs[0].Role != message.RoleUser {
		t.Errorf("expected user message at index 0, got %q", msgs[0].Role)
	}
	if msgs[1].Role != message.RoleAssistant || !msgs[1].IsToolCall() {
		t.Errorf("expected assistant tool call at index 1")
	}
	if msgs[2].Role != message.RoleTool {
		t.Errorf("expected tool result at index 2, got %q", msgs[2].Role)
	}
	if msgs[3].Role != message.RoleAssistant {
		t.Errorf("expected assistant message at index 3, got %q", msgs[3].Role)
	}
}

// ---------------------------------------------------------------------------
// Memory Integration Tests
// ---------------------------------------------------------------------------

type simpleMemory struct {
	mu   sync.RWMutex
	msgs []message.Message
}

func newSimpleMemory() *simpleMemory {
	return &simpleMemory{}
}

func (m *simpleMemory) Add(_ context.Context, msg message.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = append(m.msgs, msg)
	return nil
}

func (m *simpleMemory) GetRelevant(_ context.Context, _ string, limit int) ([]message.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if limit > len(m.msgs) {
		limit = len(m.msgs)
	}
	result := make([]message.Message, limit)
	copy(result, m.msgs[:limit])
	return result, nil
}

func (m *simpleMemory) GetAll(_ context.Context) ([]message.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]message.Message{}, m.msgs...), nil
}

func (m *simpleMemory) Clear(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = nil
	return nil
}

func (m *simpleMemory) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.msgs)
}

func TestAgent_WithMemory(t *testing.T) {
	mem := newSimpleMemory()
	a, mp := newTestAgentWithMock(t, WithMemory(mem))
	mp.SetDefaultResponse(mock.MockResponse{
		Message:      message.AssistantMessage("I'll remember that."),
		Usage:        provider.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		FinishReason: provider.FinishReasonStop,
	})

	_, err := a.Run(context.Background(), "Remember this")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mem.Size() != 2 {
		t.Errorf("expected 2 messages in memory, got %d", mem.Size())
	}
}

func TestAgent_WithMemory_ContextIncludedInNextRun(t *testing.T) {
	mem := newSimpleMemory()
	a, mp := newTestAgentWithMock(t, WithMemory(mem))

	mp.SetDefaultResponse(mock.MockResponse{
		Message:      message.AssistantMessage("Got it."),
		Usage:        provider.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		FinishReason: provider.FinishReasonStop,
	})

	a.Run(context.Background(), "First message")
	a.Run(context.Background(), "Second message")

	calls := mp.GenerateCalls()
	lastCall := calls[len(calls)-1]

	hasMemoryContent := false
	for _, msg := range lastCall.Messages {
		if msg.Text() == "First message" {
			hasMemoryContent = true
		}
	}
	if !hasMemoryContent {
		t.Error("expected memory content to be included in second run's messages")
	}
}

// ---------------------------------------------------------------------------
// Middleware Integration Tests
// ---------------------------------------------------------------------------

func TestAgent_WithMiddleware(t *testing.T) {
	a, mp := newTestAgentWithMock(t,
		WithMiddleware(middleware.WithRetry(3, middleware.ExponentialBackoff{Initial: 1 * time.Millisecond})),
	)
	mp.SetDefaultResponse(mock.MockResponse{
		Message:      message.AssistantMessage("ok"),
		Usage:        provider.TokenUsage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
		FinishReason: provider.FinishReasonStop,
	})

	result, err := a.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FinalText() != "ok" {
		t.Errorf("expected 'ok', got %q", result.FinalText())
	}
}

// ---------------------------------------------------------------------------
// Edge Case Tests
// ---------------------------------------------------------------------------

func TestAgent_RunWithNoSystemPrompt(t *testing.T) {
	a, mp := newTestAgentWithMock(t)
	mp.SetDefaultResponse(mock.MockResponse{
		Message:      message.AssistantMessage("No system prompt."),
		Usage:        provider.TokenUsage{},
		FinishReason: provider.FinishReasonStop,
	})

	_, err := a.Run(context.Background(), "Hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := mp.GenerateCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	for _, msg := range calls[0].Messages {
		if msg.Role == message.RoleSystem {
			t.Error("expected no system message")
		}
	}
}

func TestAgent_MaxTurns1_NoToolLoop(t *testing.T) {
	a, mp := newTestAgentWithMock(t, WithTools(&echoTool{}), WithMaxTurns(1))
	mp.SetDefaultResponse(mock.MockResponse{
		Message: message.AssistantToolCallMessage([]message.ToolCall{
			{ID: "c1", Type: "function", Function: message.ToolCallFunction{Name: "echo", Arguments: `"hi"`}},
		}),
		Usage:        provider.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		FinishReason: provider.FinishReasonToolCall,
	})

	_, err := a.Run(context.Background(), "Call tool")
	if err == nil {
		t.Fatal("expected max turns error")
	}

	var maxErr *MaxTurnsError
	if !errors.As(err, &maxErr) {
		t.Fatalf("expected MaxTurnsError, got %T", err)
	}
}

func TestAgent_ConcurrentRuns(t *testing.T) {
	a, mp := newTestAgentWithMock(t)
	mp.SetDefaultResponse(mock.MockResponse{
		Message:      message.AssistantMessage("ok"),
		Usage:        provider.TokenUsage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
		FinishReason: provider.FinishReasonStop,
	})

	var wg sync.WaitGroup
	errCh := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := a.Run(context.Background(), fmt.Sprintf("msg-%d", i))
			errCh <- err
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}

	if mp.CallCount() != 10 {
		t.Errorf("expected 10 calls, got %d", mp.CallCount())
	}
}

// ---------------------------------------------------------------------------
// Events Type Tests
// ---------------------------------------------------------------------------

func TestAgentResult_HasToolCalls(t *testing.T) {
	r := &AgentResult{ToolCalls: nil}
	if r.HasToolCalls() {
		t.Error("expected no tool calls")
	}
	r.ToolCalls = append(r.ToolCalls, ToolExecution{Turn: 0})
	if !r.HasToolCalls() {
		t.Error("expected tool calls")
	}
}

func TestAgentResult_ToolCallsByTurn(t *testing.T) {
	r := &AgentResult{
		ToolCalls: []ToolExecution{
			{Turn: 0, Call: message.ToolCall{ID: "c1"}},
			{Turn: 0, Call: message.ToolCall{ID: "c2"}},
			{Turn: 1, Call: message.ToolCall{ID: "c3"}},
		},
	}

	turn0 := r.ToolCallsByTurn(0)
	if len(turn0) != 2 {
		t.Errorf("expected 2 tool calls in turn 0, got %d", len(turn0))
	}
	turn1 := r.ToolCallsByTurn(1)
	if len(turn1) != 1 {
		t.Errorf("expected 1 tool call in turn 1, got %d", len(turn1))
	}
	turn5 := r.ToolCallsByTurn(5)
	if len(turn5) != 0 {
		t.Errorf("expected 0 tool calls in turn 5, got %d", len(turn5))
	}
}

func TestMaxTurnsError(t *testing.T) {
	err := &MaxTurnsError{
		AgentName: "test",
		MaxTurns:  5,
		Partial:   &AgentResult{Turns: 5},
	}

	if !strings.Contains(err.Error(), "test") {
		t.Errorf("expected error to contain agent name")
	}
	if !strings.Contains(err.Error(), "5") {
		t.Errorf("expected error to contain max turns")
	}
	if err.PartialResult() == nil {
		t.Error("expected non-nil partial result")
	}
}

// ---------------------------------------------------------------------------
// Template Helper Tests
// ---------------------------------------------------------------------------

func TestTemplateNameFromPath(t *testing.T) {
	tests := []struct {
		path, expected string
	}{
		{"greeting.tmpl", "greeting"},
		{"templates/system.prompt", "system"},
		{"/abs/path/to/my_template.txt", "my_template"},
		{"no_extension", "no_extension"},
		{`C:\Users\test\prompt.gotmpl`, "prompt"},
	}

	for _, tt := range tests {
		result := templateNameFromPath(tt.path)
		if result != tt.expected {
			t.Errorf("templateNameFromPath(%q) = %q, want %q", tt.path, result, tt.expected)
		}
	}
}

func TestIsTemplateFile(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"test.tmpl", true},
		{"test.prompt", true},
		{"test.txt", true},
		{"test.gotmpl", true},
		{"test.json", false},
		{"test.yaml", false},
		{"tmpl.go", false},
	}

	for _, tt := range tests {
		result := isTemplateFile(tt.name)
		if result != tt.expected {
			t.Errorf("isTemplateFile(%q) = %v, want %v", tt.name, result, tt.expected)
		}
	}
}

func TestTemplate_String(t *testing.T) {
	tmpl := MustTemplate("test", "Hello {{.Name}}")
	s := tmpl.String()
	if !strings.Contains(s, "test") || !strings.Contains(s, "13 bytes") {
		t.Errorf("unexpected string: %s", s)
	}
}

// ---------------------------------------------------------------------------
// Boundary Tests
// ---------------------------------------------------------------------------

func TestAgent_LargeConversation(t *testing.T) {
	a, mp := newTestAgentWithMock(t)
	mp.SetDefaultResponse(mock.MockResponse{
		Message:      message.AssistantMessage("Got it."),
		Usage:        provider.TokenUsage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110},
		FinishReason: provider.FinishReasonStop,
	})

	var msgs []message.Message
	for i := 0; i < 100; i++ {
		msgs = append(msgs, message.UserMessage(fmt.Sprintf("Message %d", i)))
		msgs = append(msgs, message.AssistantMessage(fmt.Sprintf("Reply %d", i)))
	}
	msgs = append(msgs, message.UserMessage("Final question"))

	result, err := a.RunConversation(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Turns != 1 {
		t.Errorf("expected 1 turn, got %d", result.Turns)
	}
}

func TestAgent_EmptyInput(t *testing.T) {
	a, mp := newTestAgentWithMock(t)
	mp.SetDefaultResponse(mock.MockResponse{
		Message:      message.AssistantMessage("Empty input received."),
		Usage:        provider.TokenUsage{},
		FinishReason: provider.FinishReasonStop,
	})

	result, err := a.Run(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FinalText() != "Empty input received." {
		t.Errorf("unexpected result: %q", result.FinalText())
	}
}

func TestToolExecution_Duration(t *testing.T) {
	a, mp := newTestAgentWithMock(t, WithTools(&echoTool{}), WithMaxTurns(5))

	mp.AddResponse(mock.MockResponse{
		Message: message.AssistantToolCallMessage([]message.ToolCall{
			{ID: "c1", Type: "function", Function: message.ToolCallFunction{Name: "echo", Arguments: `"test"`}},
		}),
		Usage:        provider.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		FinishReason: provider.FinishReasonToolCall,
	})
	mp.AddResponse(mock.MockResponse{
		Message:      message.AssistantMessage("Done"),
		Usage:        provider.TokenUsage{},
		FinishReason: provider.FinishReasonStop,
	})

	result, err := a.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call")
	}
	if result.ToolCalls[0].Duration < 0 {
		t.Error("expected non-negative tool duration")
	}
}
