package mock

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/user/orchestra/internal/message"
	"github.com/user/orchestra/internal/provider"
)

// --- Interface Compliance ---

func TestMockProvider_ImplementsInterface(t *testing.T) {
	// This test ensures the mock provider satisfies the Provider interface.
	// If it doesn't compile, the interface contract is broken.
	var p provider.Provider = NewProvider("test")
	_ = p
}

func TestMockProvider_Name(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		want     string
	}{
		{"simple name", "openai", "openai"},
		{"empty name", "", ""},
		{"name with spaces", "my provider", "my provider"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProvider(tt.provider)
			if got := p.Name(); got != tt.want {
				t.Errorf("Name() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- Models ---

func TestMockProvider_Models(t *testing.T) {
	p := NewProvider("test")

	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error = %v", err)
	}

	if len(models) != 2 {
		t.Fatalf("Models() returned %d models, want 2", len(models))
	}

	if models[0].ID != "mock-model" {
		t.Errorf("Models()[0].ID = %q, want %q", models[0].ID, "mock-model")
	}
	if models[1].ID != "mock-model-vision" {
		t.Errorf("Models()[1].ID = %q, want %q", models[1].ID, "mock-model-vision")
	}
}

func TestMockProvider_Models_Custom(t *testing.T) {
	p := NewProvider("test")
	p.SetModels([]ModelConfig{
		{
			ID:   "custom-model-a",
			Name: "Custom Model A",
			Capabilities: provider.ModelCapabilities{
				Streaming:   true,
				ToolCalling: false,
				MaxTokens:   2048,
			},
		},
	})

	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error = %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("Models() returned %d models, want 1", len(models))
	}
	if models[0].ID != "custom-model-a" {
		t.Errorf("Models()[0].ID = %q, want %q", models[0].ID, "custom-model-a")
	}
	if models[0].Capabilities.MaxTokens != 2048 {
		t.Errorf("MaxTokens = %d, want 2048", models[0].Capabilities.MaxTokens)
	}
}

func TestMockProvider_AddModel(t *testing.T) {
	p := NewProvider("test")
	p.AddModel(ModelConfig{
		ID:   "new-model",
		Name: "New Model",
		Capabilities: provider.ModelCapabilities{
			Streaming: true,
			Vision:    true,
			MaxTokens: 16384,
		},
	})

	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error = %v", err)
	}

	if len(models) != 3 {
		t.Fatalf("Models() returned %d models, want 3", len(models))
	}

	found := false
	for _, m := range models {
		if m.ID == "new-model" {
			found = true
			if !m.Capabilities.Vision {
				t.Error("new-model Vision = false, want true")
			}
		}
	}
	if !found {
		t.Error("new-model not found in models list")
	}
}

// --- Generate ---

func TestMockProvider_Generate_DefaultResponse(t *testing.T) {
	p := NewProvider("test")

	req := provider.GenerateRequest{
		Model:    "mock-model",
		Messages: []message.Message{message.UserMessage("hello")},
	}

	result, err := p.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if result.ID == "" {
		t.Error("Generate() result.ID is empty")
	}
	if result.Message.Text() != "mock response" {
		t.Errorf("Generate() message = %q, want %q", result.Message.Text(), "mock response")
	}
	if result.FinishReason != provider.FinishReasonStop {
		t.Errorf("Generate() finish reason = %q, want %q", result.FinishReason, provider.FinishReasonStop)
	}
	if result.Model != "mock-model" {
		t.Errorf("Generate() model = %q, want %q", result.Model, "mock-model")
	}
	if result.Usage.PromptTokens != 10 {
		t.Errorf("Generate() usage.PromptTokens = %d, want 10", result.Usage.PromptTokens)
	}
	if result.Usage.CompletionTokens != 5 {
		t.Errorf("Generate() usage.CompletionTokens = %d, want 5", result.Usage.CompletionTokens)
	}
	if result.Usage.TotalTokens != 15 {
		t.Errorf("Generate() usage.TotalTokens = %d, want 15", result.Usage.TotalTokens)
	}
	if result.CreatedAt.IsZero() {
		t.Error("Generate() result.CreatedAt is zero")
	}
}

func TestMockProvider_Generate_DefaultModel(t *testing.T) {
	p := NewProvider("test")

	// Empty model should default to "mock-model"
	req := provider.GenerateRequest{
		Model:    "",
		Messages: []message.Message{message.UserMessage("hello")},
	}

	result, err := p.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if result.Model != "mock-model" {
		t.Errorf("Generate() model = %q, want %q", result.Model, "mock-model")
	}
}

func TestMockProvider_Generate_CustomResponse(t *testing.T) {
	p := NewProvider("test")

	p.AddResponse(MockResponse{
		Message:      message.AssistantMessage("custom response 1"),
		FinishReason: provider.FinishReasonToolCall,
		Usage: provider.TokenUsage{
			PromptTokens:     20,
			CompletionTokens: 10,
			TotalTokens:      30,
		},
	})

	p.AddResponse(MockResponse{
		Message:      message.AssistantMessage("custom response 2"),
		FinishReason: provider.FinishReasonStop,
		Usage: provider.TokenUsage{
			PromptTokens:     5,
			CompletionTokens: 3,
			TotalTokens:      8,
		},
	})

	req := provider.GenerateRequest{
		Model:    "mock-model",
		Messages: []message.Message{message.UserMessage("hello")},
	}

	// First call gets the first queued response
	result1, err := p.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Generate() #1 error = %v", err)
	}
	if result1.Message.Text() != "custom response 1" {
		t.Errorf("Generate() #1 message = %q, want %q", result1.Message.Text(), "custom response 1")
	}
	if result1.FinishReason != provider.FinishReasonToolCall {
		t.Errorf("Generate() #1 finish reason = %q, want %q", result1.FinishReason, provider.FinishReasonToolCall)
	}
	if result1.Usage.TotalTokens != 30 {
		t.Errorf("Generate() #1 total tokens = %d, want 30", result1.Usage.TotalTokens)
	}

	// Second call gets the second queued response
	result2, err := p.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Generate() #2 error = %v", err)
	}
	if result2.Message.Text() != "custom response 2" {
		t.Errorf("Generate() #2 message = %q, want %q", result2.Message.Text(), "custom response 2")
	}

	// Third call falls back to the default
	result3, err := p.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Generate() #3 error = %v", err)
	}
	if result3.Message.Text() != "mock response" {
		t.Errorf("Generate() #3 message = %q, want %q (default)", result3.Message.Text(), "mock response")
	}
}

func TestMockProvider_Generate_Error(t *testing.T) {
	p := NewProvider("test")

	expectedErr := errors.New("api error: rate limit exceeded")
	p.AddResponse(MockResponse{
		Error: expectedErr,
	})

	req := provider.GenerateRequest{
		Model:    "mock-model",
		Messages: []message.Message{message.UserMessage("hello")},
	}

	result, err := p.Generate(context.Background(), req)
	if err == nil {
		t.Fatal("Generate() should return error")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("Generate() error = %v, want %v", err, expectedErr)
	}
	if result != nil {
		t.Error("Generate() result should be nil on error")
	}
}

func TestMockProvider_Generate_ContextCancellation(t *testing.T) {
	p := NewProvider("test")
	p.AddResponse(MockResponse{
		Delay: 5 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req := provider.GenerateRequest{
		Model:    "mock-model",
		Messages: []message.Message{message.UserMessage("hello")},
	}

	_, err := p.Generate(ctx, req)
	if err == nil {
		t.Fatal("Generate() should return error on context cancellation")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Generate() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestMockProvider_Generate_ToolCallResponse(t *testing.T) {
	p := NewProvider("test")

	toolCalls := []message.ToolCall{
		{
			ID:   "call_001",
			Type: "function",
			Function: message.ToolCallFunction{
				Name:      "get_weather",
				Arguments: `{"city": "London"}`,
			},
		},
	}

	p.AddResponse(MockResponse{
		Message:      message.AssistantToolCallMessage(toolCalls),
		FinishReason: provider.FinishReasonToolCall,
		Usage: provider.TokenUsage{
			PromptTokens:     15,
			CompletionTokens: 8,
			TotalTokens:      23,
		},
	})

	req := provider.GenerateRequest{
		Model:    "mock-model",
		Messages: []message.Message{message.UserMessage("What's the weather in London?")},
	}

	result, err := p.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if !result.IsToolCall() {
		t.Error("Generate() result should be a tool call")
	}
	if !result.Message.IsToolCall() {
		t.Error("Generate() message should report as tool call")
	}
	if len(result.Message.ToolCalls) != 1 {
		t.Fatalf("Generate() tool calls = %d, want 1", len(result.Message.ToolCalls))
	}
	if result.Message.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("Tool call name = %q, want %q", result.Message.ToolCalls[0].Function.Name, "get_weather")
	}
}

func TestMockProvider_SetDefaultResponse(t *testing.T) {
	p := NewProvider("test")

	p.SetDefaultResponse(MockResponse{
		Message:      message.AssistantMessage("new default"),
		FinishReason: provider.FinishReasonLength,
		Usage: provider.TokenUsage{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
		},
	})

	result, err := p.Generate(context.Background(), provider.GenerateRequest{
		Model:    "mock-model",
		Messages: []message.Message{message.UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if result.Message.Text() != "new default" {
		t.Errorf("Generate() message = %q, want %q", result.Message.Text(), "new default")
	}
	if result.FinishReason != provider.FinishReasonLength {
		t.Errorf("Generate() finish reason = %q, want %q", result.FinishReason, provider.FinishReasonLength)
	}
}

// --- Stream ---

func TestMockProvider_Stream_DefaultChunks(t *testing.T) {
	p := NewProvider("test")

	req := provider.GenerateRequest{
		Model:    "mock-model",
		Messages: []message.Message{message.UserMessage("hello")},
	}

	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var events []provider.StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Default: start + 3 chunks + done = 5 events
	if len(events) != 5 {
		t.Fatalf("Stream() emitted %d events, want 5", len(events))
	}

	if events[0].Type != provider.StreamEventStart {
		t.Errorf("events[0].Type = %q, want %q", events[0].Type, provider.StreamEventStart)
	}

	// Collect text from chunks
	var text string
	for i := 1; i <= 3; i++ {
		if events[i].Type != provider.StreamEventChunk {
			t.Errorf("events[%d].Type = %q, want %q", i, events[i].Type, provider.StreamEventChunk)
		}
		text += events[i].Chunk
	}

	if text != "mock streaming response" {
		t.Errorf("streamed text = %q, want %q", text, "mock streaming response")
	}

	if events[4].Type != provider.StreamEventDone {
		t.Errorf("events[4].Type = %q, want %q", events[4].Type, provider.StreamEventDone)
	}
	if events[4].Usage == nil {
		t.Error("done event should have usage")
	} else if events[4].Usage.TotalTokens != 15 {
		t.Errorf("done event usage.TotalTokens = %d, want 15", events[4].Usage.TotalTokens)
	}
}

func TestMockProvider_Stream_CustomChunks(t *testing.T) {
	p := NewProvider("test")

	p.AddStreamChunks([]StreamChunk{
		{Type: provider.StreamEventStart},
		{Type: provider.StreamEventChunk, Chunk: "Hello "},
		{Type: provider.StreamEventChunk, Chunk: "world!"},
		{Type: provider.StreamEventDone, Usage: &provider.TokenUsage{
			PromptTokens:     5,
			CompletionTokens: 2,
			TotalTokens:      7,
		}},
	})

	ch, err := p.Stream(context.Background(), provider.GenerateRequest{
		Model:    "mock-model",
		Messages: []message.Message{message.UserMessage("greet me")},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var text string
	for evt := range ch {
		if evt.Type == provider.StreamEventChunk {
			text += evt.Chunk
		}
	}

	if text != "Hello world!" {
		t.Errorf("streamed text = %q, want %q", text, "Hello world!")
	}

	// Second call should fall back to default
	ch2, err := p.Stream(context.Background(), provider.GenerateRequest{
		Model:    "mock-model",
		Messages: []message.Message{message.UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("Stream() #2 error = %v", err)
	}

	var text2 string
	for evt := range ch2 {
		if evt.Type == provider.StreamEventChunk {
			text2 += evt.Chunk
		}
	}

	if text2 != "mock streaming response" {
		t.Errorf("second stream text = %q, want %q (default)", text2, "mock streaming response")
	}
}

func TestMockProvider_Stream_WithToolCalls(t *testing.T) {
	p := NewProvider("test")

	p.AddStreamChunks([]StreamChunk{
		{Type: provider.StreamEventStart},
		{Type: provider.StreamEventChunk, Chunk: "Let me check that for you."},
		{Type: provider.StreamEventToolCall, ToolCall: &message.ToolCall{
			ID:   "call_search",
			Type: "function",
			Function: message.ToolCallFunction{
				Name:      "search",
				Arguments: `{"query": "test"}`,
			},
		}},
		{Type: provider.StreamEventDone, Usage: &provider.TokenUsage{
			PromptTokens:     10,
			CompletionTokens: 15,
			TotalTokens:      25,
		}},
	})

	ch, err := p.Stream(context.Background(), provider.GenerateRequest{
		Model:    "mock-model",
		Messages: []message.Message{message.UserMessage("search for test")},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var toolCalls []*message.ToolCall
	for evt := range ch {
		if evt.Type == provider.StreamEventToolCall && evt.ToolCall != nil {
			toolCalls = append(toolCalls, evt.ToolCall)
		}
	}

	if len(toolCalls) != 1 {
		t.Fatalf("stream tool calls = %d, want 1", len(toolCalls))
	}
	if toolCalls[0].Function.Name != "search" {
		t.Errorf("tool call name = %q, want %q", toolCalls[0].Function.Name, "search")
	}
}

func TestMockProvider_Stream_WithDelay(t *testing.T) {
	p := NewProvider("test")
	p.SetStreamDelay(10 * time.Millisecond)

	start := time.Now()
	ch, err := p.Stream(context.Background(), provider.GenerateRequest{
		Model:    "mock-model",
		Messages: []message.Message{message.UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	// Drain the channel
	for range ch {
	}

	elapsed := time.Since(start)
	// 5 events * 10ms delay = ~50ms minimum
	if elapsed < 40*time.Millisecond {
		t.Errorf("stream completed in %v, expected at least ~50ms with delay", elapsed)
	}
}

func TestMockProvider_Stream_ContextCancellation(t *testing.T) {
	p := NewProvider("test")
	p.SetStreamDelay(100 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	ch, err := p.Stream(ctx, provider.GenerateRequest{
		Model:    "mock-model",
		Messages: []message.Message{message.UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	// Cancel after reading the first event
	evt := <-ch
	if evt.Type != provider.StreamEventStart {
		t.Errorf("first event type = %q, want %q", evt.Type, provider.StreamEventStart)
	}

	cancel()

	// Read remaining events — should eventually get an error or the channel should close
	var gotError bool
	for evt := range ch {
		if evt.Type == provider.StreamEventError {
			gotError = true
			if evt.Error == nil {
				t.Error("error event should have non-nil Error field")
			}
		}
	}

	if !gotError {
		t.Error("expected an error event from cancelled context")
	}
}

func TestMockProvider_Stream_ErrorEvent(t *testing.T) {
	p := NewProvider("test")

	p.AddStreamChunks([]StreamChunk{
		{Type: provider.StreamEventStart},
		{Type: provider.StreamEventChunk, Chunk: "partial "},
		{Type: provider.StreamEventError, Error: errors.New("stream interrupted")},
		{Type: provider.StreamEventChunk, Chunk: "should not see this"},
	})

	ch, err := p.Stream(context.Background(), provider.GenerateRequest{
		Model:    "mock-model",
		Messages: []message.Message{message.UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var events []provider.StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Should get start, chunk, error — but NOT the fourth chunk (error stops processing)
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	if events[2].Type != provider.StreamEventError {
		t.Errorf("events[2].Type = %q, want %q", events[2].Type, provider.StreamEventError)
	}
	if events[2].Error == nil || events[2].Error.Error() != "stream interrupted" {
		t.Errorf("events[2].Error = %v, want 'stream interrupted'", events[2].Error)
	}
}

// --- Capabilities ---

func TestMockProvider_Capabilities_KnownModel(t *testing.T) {
	p := NewProvider("test")

	caps := p.Capabilities("mock-model")
	if !caps.Streaming {
		t.Error("mock-model Streaming = false, want true")
	}
	if !caps.ToolCalling {
		t.Error("mock-model ToolCalling = false, want true")
	}
	if caps.Vision {
		t.Error("mock-model Vision = true, want false")
	}
	if caps.MaxTokens != 4096 {
		t.Errorf("mock-model MaxTokens = %d, want 4096", caps.MaxTokens)
	}
}

func TestMockProvider_Capabilities_VisionModel(t *testing.T) {
	p := NewProvider("test")

	caps := p.Capabilities("mock-model-vision")
	if !caps.Vision {
		t.Error("mock-model-vision Vision = false, want true")
	}
	if caps.MaxTokens != 8192 {
		t.Errorf("mock-model-vision MaxTokens = %d, want 8192", caps.MaxTokens)
	}
}

func TestMockProvider_Capabilities_UnknownModel(t *testing.T) {
	p := NewProvider("test")

	caps := p.Capabilities("unknown-model")
	// Should return default capabilities
	if caps.Streaming != true {
		t.Error("unknown model default Streaming should be true")
	}
	if caps.MaxTokens != 4096 {
		t.Errorf("unknown model default MaxTokens = %d, want 4096", caps.MaxTokens)
	}
}

// --- Call Tracking ---

func TestMockProvider_GenerateCalls(t *testing.T) {
	p := NewProvider("test")

	msgs1 := []message.Message{message.UserMessage("first")}
	msgs2 := []message.Message{message.UserMessage("second")}

	_, _ = p.Generate(context.Background(), provider.GenerateRequest{Model: "m1", Messages: msgs1})
	_, _ = p.Generate(context.Background(), provider.GenerateRequest{Model: "m2", Messages: msgs2})

	calls := p.GenerateCalls()
	if len(calls) != 2 {
		t.Fatalf("GenerateCalls() = %d calls, want 2", len(calls))
	}
	if calls[0].Model != "m1" {
		t.Errorf("calls[0].Model = %q, want %q", calls[0].Model, "m1")
	}
	if calls[1].Model != "m2" {
		t.Errorf("calls[1].Model = %q, want %q", calls[1].Model, "m2")
	}
	if calls[0].Messages[0].Text() != "first" {
		t.Errorf("calls[0].Messages[0] = %q, want %q", calls[0].Messages[0].Text(), "first")
	}
}

func TestMockProvider_StreamCalls(t *testing.T) {
	p := NewProvider("test")

	msgs := []message.Message{message.UserMessage("stream me")}

	ch, _ := p.Stream(context.Background(), provider.GenerateRequest{Model: "m1", Messages: msgs})
	for range ch {
	} // drain

	calls := p.StreamCalls()
	if len(calls) != 1 {
		t.Fatalf("StreamCalls() = %d calls, want 1", len(calls))
	}
	if calls[0].Model != "m1" {
		t.Errorf("calls[0].Model = %q, want %q", calls[0].Model, "m1")
	}
}

func TestMockProvider_CallCount(t *testing.T) {
	p := NewProvider("test")

	if p.CallCount() != 0 {
		t.Errorf("initial CallCount() = %d, want 0", p.CallCount())
	}

	_, _ = p.Generate(context.Background(), provider.GenerateRequest{
		Model: "mock-model", Messages: []message.Message{message.UserMessage("a")},
	})
	if p.CallCount() != 1 {
		t.Errorf("after 1 Generate, CallCount() = %d, want 1", p.CallCount())
	}

	ch, _ := p.Stream(context.Background(), provider.GenerateRequest{
		Model: "mock-model", Messages: []message.Message{message.UserMessage("b")},
	})
	for range ch {
	}
	if p.CallCount() != 2 {
		t.Errorf("after 1 Generate + 1 Stream, CallCount() = %d, want 2", p.CallCount())
	}
}

func TestMockProvider_LastGenerateCall(t *testing.T) {
	p := NewProvider("test")

	_, err := p.LastGenerateCall()
	if err == nil {
		t.Error("LastGenerateCall() on empty should return error")
	}

	_, _ = p.Generate(context.Background(), provider.GenerateRequest{
		Model: "first", Messages: []message.Message{message.UserMessage("1")},
	})
	_, _ = p.Generate(context.Background(), provider.GenerateRequest{
		Model: "second", Messages: []message.Message{message.UserMessage("2")},
	})

	last, err := p.LastGenerateCall()
	if err != nil {
		t.Fatalf("LastGenerateCall() error = %v", err)
	}
	if last.Model != "second" {
		t.Errorf("LastGenerateCall().Model = %q, want %q", last.Model, "second")
	}
}

func TestMockProvider_LastStreamCall(t *testing.T) {
	p := NewProvider("test")

	_, err := p.LastStreamCall()
	if err == nil {
		t.Error("LastStreamCall() on empty should return error")
	}

	ch, _ := p.Stream(context.Background(), provider.GenerateRequest{
		Model: "stream-model", Messages: []message.Message{message.UserMessage("stream")},
	})
	for range ch {
	}

	last, err := p.LastStreamCall()
	if err != nil {
		t.Fatalf("LastStreamCall() error = %v", err)
	}
	if last.Model != "stream-model" {
		t.Errorf("LastStreamCall().Model = %q, want %q", last.Model, "stream-model")
	}
}

func TestMockProvider_GenerateCallCount(t *testing.T) {
	p := NewProvider("test")

	if p.GenerateCallCount() != 0 {
		t.Errorf("initial GenerateCallCount() = %d, want 0", p.GenerateCallCount())
	}

	_, _ = p.Generate(context.Background(), provider.GenerateRequest{
		Model: "m", Messages: []message.Message{message.UserMessage("a")},
	})
	_, _ = p.Generate(context.Background(), provider.GenerateRequest{
		Model: "m", Messages: []message.Message{message.UserMessage("b")},
	})

	if p.GenerateCallCount() != 2 {
		t.Errorf("GenerateCallCount() = %d, want 2", p.GenerateCallCount())
	}
}

func TestMockProvider_StreamCallCount(t *testing.T) {
	p := NewProvider("test")

	if p.StreamCallCount() != 0 {
		t.Errorf("initial StreamCallCount() = %d, want 0", p.StreamCallCount())
	}

	ch, _ := p.Stream(context.Background(), provider.GenerateRequest{
		Model: "m", Messages: []message.Message{message.UserMessage("a")},
	})
	for range ch {
	}

	if p.StreamCallCount() != 1 {
		t.Errorf("StreamCallCount() = %d, want 1", p.StreamCallCount())
	}
}

// --- Reset ---

func TestMockProvider_Reset(t *testing.T) {
	p := NewProvider("test")

	// Make some calls
	_, _ = p.Generate(context.Background(), provider.GenerateRequest{
		Model: "m", Messages: []message.Message{message.UserMessage("a")},
	})
	ch, _ := p.Stream(context.Background(), provider.GenerateRequest{
		Model: "m", Messages: []message.Message{message.UserMessage("b")},
	})
	for range ch {
	}

	// Queue some responses
	p.AddResponse(MockResponse{
		Message: message.AssistantMessage("queued"),
	})

	// Reset
	p.Reset()

	if p.CallCount() != 0 {
		t.Errorf("after Reset, CallCount() = %d, want 0", p.CallCount())
	}
	if p.GenerateCallCount() != 0 {
		t.Errorf("after Reset, GenerateCallCount() = %d, want 0", p.GenerateCallCount())
	}
	if p.StreamCallCount() != 0 {
		t.Errorf("after Reset, StreamCallCount() = %d, want 0", p.StreamCallCount())
	}
	if len(p.GenerateCalls()) != 0 {
		t.Errorf("after Reset, GenerateCalls() = %d, want 0", len(p.GenerateCalls()))
	}
	if len(p.StreamCalls()) != 0 {
		t.Errorf("after Reset, StreamCalls() = %d, want 0", len(p.StreamCalls()))
	}

	// After reset, should use default response (not the queued one)
	result, err := p.Generate(context.Background(), provider.GenerateRequest{
		Model: "mock-model", Messages: []message.Message{message.UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("Generate() after reset error = %v", err)
	}
	if result.Message.Text() != "mock response" {
		t.Errorf("after Reset, Generate() message = %q, want default %q", result.Message.Text(), "mock response")
	}
}

// --- Concurrent Access ---

func TestMockProvider_ConcurrentGenerate(t *testing.T) {
	p := NewProvider("test")
	p.SetDefaultResponse(MockResponse{
		Message:      message.AssistantMessage("concurrent response"),
		FinishReason: provider.FinishReasonStop,
		Usage: provider.TokenUsage{
			PromptTokens:     1,
			CompletionTokens: 1,
			TotalTokens:      2,
		},
	})

	const numGoroutines = 50
	results := make(chan *provider.GenerateResult, numGoroutines)
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			result, err := p.Generate(context.Background(), provider.GenerateRequest{
				Model:    "mock-model",
				Messages: []message.Message{message.UserMessage(fmt.Sprintf("msg %d", idx))},
			})
			if err != nil {
				errors <- err
				return
			}
			results <- result
		}(i)
	}

	for i := 0; i < numGoroutines; i++ {
		select {
		case result := <-results:
			if result.Message.Text() != "concurrent response" {
				t.Errorf("concurrent result text = %q, want %q", result.Message.Text(), "concurrent response")
			}
		case err := <-errors:
			t.Errorf("concurrent Generate() error = %v", err)
		}
	}

	if p.GenerateCallCount() != numGoroutines {
		t.Errorf("after %d concurrent calls, GenerateCallCount() = %d", numGoroutines, p.GenerateCallCount())
	}
}

func TestMockProvider_ConcurrentStream(t *testing.T) {
	p := NewProvider("test")

	const numGoroutines = 20
	done := make(chan bool, numGoroutines)
	errs := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			ch, err := p.Stream(context.Background(), provider.GenerateRequest{
				Model:    "mock-model",
				Messages: []message.Message{message.UserMessage(fmt.Sprintf("stream %d", idx))},
			})
			if err != nil {
				errs <- err
				return
			}
			for range ch {
			}
			done <- true
		}(i)
	}

	for i := 0; i < numGoroutines; i++ {
		select {
		case <-done:
			// ok
		case err := <-errs:
			t.Errorf("concurrent Stream() error = %v", err)
		}
	}

	if p.StreamCallCount() != numGoroutines {
		t.Errorf("after %d concurrent streams, StreamCallCount() = %d", numGoroutines, p.StreamCallCount())
	}
}

// --- Full Lifecycle Test ---

func TestMockProvider_FullLifecycle(t *testing.T) {
	p := NewProvider("lifecycle-test")

	// Verify initial state
	if p.Name() != "lifecycle-test" {
		t.Errorf("Name() = %q, want %q", p.Name(), "lifecycle-test")
	}

	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error = %v", err)
	}
	if len(models) == 0 {
		t.Error("default models list is empty")
	}

	// Configure a custom response
	p.AddResponse(MockResponse{
		Message:      message.AssistantMessage("I can help with that!"),
		FinishReason: provider.FinishReasonStop,
		Usage: provider.TokenUsage{
			PromptTokens:     12,
			CompletionTokens: 7,
			TotalTokens:      19,
		},
	})

	// Configure a custom stream
	p.AddStreamChunks([]StreamChunk{
		{Type: provider.StreamEventStart},
		{Type: provider.StreamEventChunk, Chunk: "Think"},
		{Type: provider.StreamEventChunk, Chunk: "ing..."},
		{Type: provider.StreamEventDone, Usage: &provider.TokenUsage{
			PromptTokens:     8,
			CompletionTokens: 2,
			TotalTokens:      10,
		}},
	})

	// Generate
	genResult, err := p.Generate(context.Background(), provider.GenerateRequest{
		Model:    "mock-model",
		Messages: []message.Message{message.UserMessage("help me")},
		Tools: []provider.ToolDefinition{
			provider.FunctionTool("helper", "A helper function", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
				},
			}),
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if genResult.Message.Text() != "I can help with that!" {
		t.Errorf("Generate() text = %q, want %q", genResult.Message.Text(), "I can help with that!")
	}

	// Stream
	ch, err := p.Stream(context.Background(), provider.GenerateRequest{
		Model:    "mock-model",
		Messages: []message.Message{message.UserMessage("think")},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	var streamText string
	for evt := range ch {
		if evt.Type == provider.StreamEventChunk {
			streamText += evt.Chunk
		}
	}
	if streamText != "Thinking..." {
		t.Errorf("Stream() text = %q, want %q", streamText, "Thinking...")
	}

	// Verify call tracking
	if p.CallCount() != 2 {
		t.Errorf("CallCount() = %d, want 2", p.CallCount())
	}

	lastGen, _ := p.LastGenerateCall()
	if len(lastGen.Tools) != 1 {
		t.Errorf("last generate call had %d tools, want 1", len(lastGen.Tools))
	}

	// Reset and verify clean state
	p.Reset()
	if p.CallCount() != 0 {
		t.Errorf("after Reset, CallCount() = %d, want 0", p.CallCount())
	}
}
