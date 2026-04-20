package provider

import (
	"errors"
	"testing"

	"github.com/user/orchestra/internal/message"
)

// ---------------------------------------------------------------------------
// FinishReason
// ---------------------------------------------------------------------------

func TestFinishReason_String(t *testing.T) {
	tests := []struct {
		reason FinishReason
		want   string
	}{
		{FinishReasonStop, "stop"},
		{FinishReasonLength, "length"},
		{FinishReasonToolCall, "tool_call"},
		{FinishReasonContentFilter, "content_filter"},
		{FinishReasonError, "error"},
		{FinishReason("custom"), "custom"},
		{FinishReason(""), ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.reason), func(t *testing.T) {
			if got := tt.reason.String(); got != tt.want {
				t.Errorf("FinishReason(%q).String() = %q, want %q", tt.reason, got, tt.want)
			}
		})
	}
}

func TestFinishReason_IsTerminal(t *testing.T) {
	tests := []struct {
		reason FinishReason
		want   bool
	}{
		{FinishReasonStop, true},
		{FinishReasonLength, true},
		{FinishReasonContentFilter, true},
		{FinishReasonError, true},
		{FinishReasonToolCall, false},
		{FinishReason("custom"), false},
		{FinishReason(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.reason), func(t *testing.T) {
			if got := tt.reason.IsTerminal(); got != tt.want {
				t.Errorf("FinishReason(%q).IsTerminal() = %v, want %v", tt.reason, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TokenUsage
// ---------------------------------------------------------------------------

func TestTokenUsage_Add(t *testing.T) {
	tests := []struct {
		name    string
		a, b    TokenUsage
		want    TokenUsage
	}{
		{
			name: "both_nonzero",
			a:    TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			b:    TokenUsage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30},
			want: TokenUsage{PromptTokens: 30, CompletionTokens: 15, TotalTokens: 45},
		},
		{
			name: "zero_plus_nonzero",
			a:    TokenUsage{},
			b:    TokenUsage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
			want: TokenUsage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
		},
		{
			name: "nonzero_plus_zero",
			a:    TokenUsage{PromptTokens: 7, CompletionTokens: 2, TotalTokens: 9},
			b:    TokenUsage{},
			want: TokenUsage{PromptTokens: 7, CompletionTokens: 2, TotalTokens: 9},
		},
		{
			name: "both_zero",
			a:    TokenUsage{},
			b:    TokenUsage{},
			want: TokenUsage{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.a.Add(tt.b)
			if got != tt.want {
				t.Errorf("TokenUsage.Add() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestTokenUsage_Add_DoesNotMutate(t *testing.T) {
	a := TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}
	b := TokenUsage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30}

	_ = a.Add(b)

	// Original should be unchanged
	if a.PromptTokens != 10 || a.CompletionTokens != 5 || a.TotalTokens != 15 {
		t.Errorf("Add() mutated the receiver: %+v", a)
	}
}

// ---------------------------------------------------------------------------
// ResponseFormat constructors
// ---------------------------------------------------------------------------

func TestTextResponseFormat(t *testing.T) {
	rf := TextResponseFormat()
	if rf == nil {
		t.Fatal("TextResponseFormat() returned nil")
	}
	if rf.Type != "text" {
		t.Errorf("Type = %q, want %q", rf.Type, "text")
	}
	if rf.JSONSchema != nil {
		t.Errorf("JSONSchema should be nil, got %v", rf.JSONSchema)
	}
}

func TestJSONResponseFormat(t *testing.T) {
	rf := JSONResponseFormat()
	if rf == nil {
		t.Fatal("JSONResponseFormat() returned nil")
	}
	if rf.Type != "json_object" {
		t.Errorf("Type = %q, want %q", rf.Type, "json_object")
	}
}

func TestJSONSchemaResponseFormat(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}
	rf := JSONSchemaResponseFormat(schema)
	if rf == nil {
		t.Fatal("JSONSchemaResponseFormat() returned nil")
	}
	if rf.Type != "json_schema" {
		t.Errorf("Type = %q, want %q", rf.Type, "json_schema")
	}
	if rf.JSONSchema == nil {
		t.Fatal("JSONSchema should not be nil")
	}
	if rf.JSONSchema["type"] != "object" {
		t.Errorf("JSONSchema['type'] = %v, want 'object'", rf.JSONSchema["type"])
	}
}

func TestJSONSchemaResponseFormat_EmptySchema(t *testing.T) {
	rf := JSONSchemaResponseFormat(nil)
	if rf.Type != "json_schema" {
		t.Errorf("Type = %q, want %q", rf.Type, "json_schema")
	}
	// nil schema is acceptable
}

// ---------------------------------------------------------------------------
// ToolDefinition / FunctionTool
// ---------------------------------------------------------------------------

func TestFunctionTool(t *testing.T) {
	params := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
		},
	}

	td := FunctionTool("search", "Search the web", params)

	if td.Type != "function" {
		t.Errorf("Type = %q, want %q", td.Type, "function")
	}
	if td.Function.Name != "search" {
		t.Errorf("Function.Name = %q, want %q", td.Function.Name, "search")
	}
	if td.Function.Description != "Search the web" {
		t.Errorf("Function.Description = %q, want %q", td.Function.Description, "Search the web")
	}
	if td.Function.Parameters["type"] != "object" {
		t.Errorf("Function.Parameters['type'] = %v, want 'object'", td.Function.Parameters["type"])
	}
}

func TestFunctionTool_NilParameters(t *testing.T) {
	td := FunctionTool("noop", "Does nothing", nil)
	if td.Function.Parameters != nil {
		t.Errorf("Expected nil Parameters, got %v", td.Function.Parameters)
	}
}

func TestFunctionTool_EmptyFields(t *testing.T) {
	td := FunctionTool("", "", map[string]any{})
	if td.Type != "function" {
		t.Errorf("Type = %q, want %q", td.Type, "function")
	}
	if td.Function.Name != "" {
		t.Errorf("Function.Name = %q, want empty", td.Function.Name)
	}
}

// ---------------------------------------------------------------------------
// GenerateResult helpers
// ---------------------------------------------------------------------------

func TestGenerateResult_IsToolCall(t *testing.T) {
	tests := []struct {
		name   string
		result GenerateResult
		want   bool
	}{
		{
			name: "finish_reason_tool_call",
			result: GenerateResult{
				FinishReason: FinishReasonToolCall,
			},
			want: true,
		},
		{
			name: "message_has_tool_calls",
			result: GenerateResult{
				FinishReason: FinishReasonStop,
				Message:      message.AssistantToolCallMessage([]message.ToolCall{{ID: "1"}}),
			},
			want: true,
		},
		{
			name: "both_tool_call",
			result: GenerateResult{
				FinishReason: FinishReasonToolCall,
				Message:      message.AssistantToolCallMessage([]message.ToolCall{{ID: "1"}}),
			},
			want: true,
		},
		{
			name: "stop_reason_no_tools",
			result: GenerateResult{
				FinishReason: FinishReasonStop,
				Message:      message.AssistantMessage("done"),
			},
			want: false,
		},
		{
			name: "length_reason_no_tools",
			result: GenerateResult{
				FinishReason: FinishReasonLength,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.IsToolCall(); got != tt.want {
				t.Errorf("IsToolCall() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerateResult_Text(t *testing.T) {
	tests := []struct {
		name   string
		result GenerateResult
		want   string
	}{
		{
			name: "simple_text",
			result: GenerateResult{
				Message: message.AssistantMessage("Hello, world!"),
			},
			want: "Hello, world!",
		},
		{
			name: "empty_message",
			result: GenerateResult{
				Message: message.AssistantMessage(""),
			},
			want: "",
		},
		{
			name: "multi_content",
			result: GenerateResult{
				Message: message.Message{
					Role: message.RoleAssistant,
					Content: []message.ContentBlock{
						message.TextContentBlock("part1 "),
						message.TextContentBlock("part2"),
					},
				},
			},
			want: "part1 part2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.Text(); got != tt.want {
				t.Errorf("Text() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ProviderError
// ---------------------------------------------------------------------------

func TestNewProviderError(t *testing.T) {
	inner := errors.New("connection refused")
	pe := NewProviderError("openai", "gpt-4", inner)

	if pe.Provider != "openai" {
		t.Errorf("Provider = %q, want %q", pe.Provider, "openai")
	}
	if pe.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", pe.Model, "gpt-4")
	}
	if pe.Code != "" {
		t.Errorf("Code = %q, want empty", pe.Code)
	}
	if pe.StatusCode != 0 {
		t.Errorf("StatusCode = %d, want 0", pe.StatusCode)
	}
	if pe.Err != inner {
		t.Errorf("Err = %v, want %v", pe.Err, inner)
	}
}

func TestNewProviderErrorWithCode(t *testing.T) {
	inner := errors.New("rate limited")
	pe := NewProviderErrorWithCode("anthropic", "claude-3", "rate_limit", 429, inner)

	if pe.Provider != "anthropic" {
		t.Errorf("Provider = %q, want %q", pe.Provider, "anthropic")
	}
	if pe.Model != "claude-3" {
		t.Errorf("Model = %q, want %q", pe.Model, "claude-3")
	}
	if pe.Code != "rate_limit" {
		t.Errorf("Code = %q, want %q", pe.Code, "rate_limit")
	}
	if pe.StatusCode != 429 {
		t.Errorf("StatusCode = %d, want 429", pe.StatusCode)
	}
}

func TestProviderError_Error_WithCode(t *testing.T) {
	pe := &ProviderError{
		Provider: "openai",
		Model:    "gpt-4",
		Code:     "context_length_exceeded",
		Err:      errors.New("max tokens"),
	}
	got := pe.Error()
	want := "provider openai: model gpt-4: [context_length_exceeded] max tokens"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestProviderError_Error_WithoutCode(t *testing.T) {
	pe := &ProviderError{
		Provider: "ollama",
		Model:    "llama3",
		Err:      errors.New("connection refused"),
	}
	got := pe.Error()
	want := "provider ollama: model llama3: connection refused"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestProviderError_Unwrap(t *testing.T) {
	inner := errors.New("inner error")
	pe := NewProviderError("test", "model", inner)

	unwrapped := pe.Unwrap()
	if unwrapped != inner {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, inner)
	}
}

func TestProviderError_Unwrap_Nil(t *testing.T) {
	pe := &ProviderError{Provider: "test", Model: "model"}
	unwrapped := pe.Unwrap()
	if unwrapped != nil {
		t.Errorf("Unwrap() = %v, want nil", unwrapped)
	}
}

func TestProviderError_ErrorsIs(t *testing.T) {
	inner := errors.New("target")
	pe := NewProviderError("p", "m", inner)

	if !errors.Is(pe, inner) {
		t.Error("errors.Is should match the inner error via Unwrap()")
	}
}

// ---------------------------------------------------------------------------
// GenerateOptions Functional Options
// ---------------------------------------------------------------------------

func TestNewGenerateOptions(t *testing.T) {
	opts := NewGenerateOptions(
		WithTemperature(0.7),
		WithMaxTokens(1024),
	)

	if opts.Temperature == nil || *opts.Temperature != 0.7 {
		t.Errorf("Temperature = %v, want 0.7", opts.Temperature)
	}
	if opts.MaxTokens == nil || *opts.MaxTokens != 1024 {
		t.Errorf("MaxTokens = %v, want 1024", opts.MaxTokens)
	}
}

func TestNewGenerateOptions_Empty(t *testing.T) {
	opts := NewGenerateOptions()
	if opts.Temperature != nil {
		t.Errorf("Temperature = %v, want nil", opts.Temperature)
	}
	if opts.MaxTokens != nil {
		t.Errorf("MaxTokens = %v, want nil", opts.MaxTokens)
	}
	if opts.TopP != nil {
		t.Errorf("TopP = %v, want nil", opts.TopP)
	}
}

func TestWithTemperature(t *testing.T) {
	opts := GenerateOptions{}
	WithTemperature(0.5)(&opts)
	if opts.Temperature == nil || *opts.Temperature != 0.5 {
		t.Errorf("Temperature = %v, want 0.5", opts.Temperature)
	}
}

func TestWithTemperature_Zero(t *testing.T) {
	opts := GenerateOptions{}
	WithTemperature(0.0)(&opts)
	if opts.Temperature == nil {
		t.Fatal("Temperature should not be nil after WithTemperature(0)")
	}
	if *opts.Temperature != 0.0 {
		t.Errorf("Temperature = %v, want 0", *opts.Temperature)
	}
}

func TestWithTopP(t *testing.T) {
	opts := GenerateOptions{}
	WithTopP(0.9)(&opts)
	if opts.TopP == nil || *opts.TopP != 0.9 {
		t.Errorf("TopP = %v, want 0.9", opts.TopP)
	}
}

func TestWithMaxTokens(t *testing.T) {
	opts := GenerateOptions{}
	WithMaxTokens(2048)(&opts)
	if opts.MaxTokens == nil || *opts.MaxTokens != 2048 {
		t.Errorf("MaxTokens = %v, want 2048", opts.MaxTokens)
	}
}

func TestWithMaxTokens_Zero(t *testing.T) {
	opts := GenerateOptions{}
	WithMaxTokens(0)(&opts)
	if opts.MaxTokens == nil {
		t.Fatal("MaxTokens should not be nil after WithMaxTokens(0)")
	}
}

func TestWithStopSequences(t *testing.T) {
	opts := GenerateOptions{}
	WithStopSequences("\n", "END", "---")(&opts)
	if len(opts.StopSequences) != 3 {
		t.Fatalf("StopSequences len = %d, want 3", len(opts.StopSequences))
	}
	if opts.StopSequences[0] != "\n" || opts.StopSequences[1] != "END" || opts.StopSequences[2] != "---" {
		t.Errorf("StopSequences = %v, want [\\n END ---]", opts.StopSequences)
	}
}

func TestWithStopSequences_Empty(t *testing.T) {
	opts := GenerateOptions{}
	WithStopSequences()(&opts)
	if opts.StopSequences != nil {
		t.Errorf("StopSequences = %v, want nil", opts.StopSequences)
	}
}

func TestWithSeed(t *testing.T) {
	opts := GenerateOptions{}
	WithSeed(42)(&opts)
	if opts.Seed == nil || *opts.Seed != 42 {
		t.Errorf("Seed = %v, want 42", opts.Seed)
	}
}

func TestWithResponseFormat(t *testing.T) {
	rf := JSONResponseFormat()
	opts := GenerateOptions{}
	WithResponseFormat(rf)(&opts)
	if opts.ResponseFormat == nil || opts.ResponseFormat.Type != "json_object" {
		t.Errorf("ResponseFormat = %v, want json_object", opts.ResponseFormat)
	}
}

func TestWithResponseFormat_Nil(t *testing.T) {
	opts := GenerateOptions{
		ResponseFormat: TextResponseFormat(),
	}
	WithResponseFormat(nil)(&opts)
	if opts.ResponseFormat != nil {
		t.Errorf("ResponseFormat should be nil after WithResponseFormat(nil)")
	}
}

func TestWithJSONMode(t *testing.T) {
	opts := GenerateOptions{}
	WithJSONMode()(&opts)
	if opts.ResponseFormat == nil || opts.ResponseFormat.Type != "json_object" {
		t.Errorf("ResponseFormat = %v, want json_object", opts.ResponseFormat)
	}
}

func TestWithTextMode(t *testing.T) {
	opts := GenerateOptions{}
	WithTextMode()(&opts)
	if opts.ResponseFormat == nil || opts.ResponseFormat.Type != "text" {
		t.Errorf("ResponseFormat = %v, want text", opts.ResponseFormat)
	}
}

func TestWithJSONSchema(t *testing.T) {
	schema := map[string]any{"type": "object"}
	opts := GenerateOptions{}
	WithJSONSchema(schema)(&opts)
	if opts.ResponseFormat == nil || opts.ResponseFormat.Type != "json_schema" {
		t.Errorf("ResponseFormat = %v, want json_schema", opts.ResponseFormat)
	}
}

func TestWithExtra(t *testing.T) {
	opts := GenerateOptions{}
	WithExtra("custom_key", "custom_value")(&opts)
	if opts.Extra == nil {
		t.Fatal("Extra should not be nil")
	}
	if opts.Extra["custom_key"] != "custom_value" {
		t.Errorf("Extra['custom_key'] = %v, want 'custom_value'", opts.Extra["custom_key"])
	}
}

func TestWithExtra_MultipleKeys(t *testing.T) {
	opts := GenerateOptions{}
	WithExtra("key1", "val1")(&opts)
	WithExtra("key2", 42)(&opts)

	if opts.Extra["key1"] != "val1" {
		t.Errorf("Extra['key1'] = %v, want 'val1'", opts.Extra["key1"])
	}
	if opts.Extra["key2"] != 42 {
		t.Errorf("Extra['key2'] = %v, want 42", opts.Extra["key2"])
	}
}

func TestWithExtra_NilValue(t *testing.T) {
	opts := GenerateOptions{}
	WithExtra("null_key", nil)(&opts)
	if _, ok := opts.Extra["null_key"]; !ok {
		t.Error("Extra should contain 'null_key'")
	}
}

func TestGenerateOptions_Apply(t *testing.T) {
	opts := &GenerateOptions{}
	result := opts.Apply(
		WithTemperature(0.8),
		WithMaxTokens(512),
	)

	if result != opts {
		t.Error("Apply() should return the same pointer for chaining")
	}
	if opts.Temperature == nil || *opts.Temperature != 0.8 {
		t.Errorf("Temperature = %v, want 0.8", opts.Temperature)
	}
	if opts.MaxTokens == nil || *opts.MaxTokens != 512 {
		t.Errorf("MaxTokens = %v, want 512", opts.MaxTokens)
	}
}

func TestGenerateOptions_Apply_Empty(t *testing.T) {
	opts := &GenerateOptions{Temperature: floatPtr(0.5)}
	opts.Apply()
	if *opts.Temperature != 0.5 {
		t.Error("Apply() with no options should not modify existing fields")
	}
}

func TestGenerateOptions_Apply_Chaining(t *testing.T) {
	opts := &GenerateOptions{}
	opts.
		Apply(WithTemperature(0.3)).
		Apply(WithMaxTokens(256), WithTopP(0.95))

	if *opts.Temperature != 0.3 {
		t.Errorf("Temperature = %v, want 0.3", *opts.Temperature)
	}
	if *opts.MaxTokens != 256 {
		t.Errorf("MaxTokens = %v, want 256", *opts.MaxTokens)
	}
	if *opts.TopP != 0.95 {
		t.Errorf("TopP = %v, want 0.95", *opts.TopP)
	}
}

func TestNewGenerateOptions_AllOptions(t *testing.T) {
	opts := NewGenerateOptions(
		WithTemperature(0.7),
		WithTopP(0.9),
		WithMaxTokens(4096),
		WithStopSequences("STOP"),
		WithSeed(123),
		WithJSONMode(),
		WithExtra("top_k", 50),
	)

	if *opts.Temperature != 0.7 {
		t.Errorf("Temperature = %v, want 0.7", *opts.Temperature)
	}
	if *opts.TopP != 0.9 {
		t.Errorf("TopP = %v, want 0.9", *opts.TopP)
	}
	if *opts.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %v, want 4096", *opts.MaxTokens)
	}
	if len(opts.StopSequences) != 1 || opts.StopSequences[0] != "STOP" {
		t.Errorf("StopSequences = %v, want [STOP]", opts.StopSequences)
	}
	if *opts.Seed != 123 {
		t.Errorf("Seed = %v, want 123", *opts.Seed)
	}
	if opts.ResponseFormat == nil || opts.ResponseFormat.Type != "json_object" {
		t.Errorf("ResponseFormat = %v, want json_object", opts.ResponseFormat)
	}
	if opts.Extra["top_k"] != 50 {
		t.Errorf("Extra['top_k'] = %v, want 50", opts.Extra["top_k"])
	}
}

func TestNewGenerateOptions_Overrides(t *testing.T) {
	// Later options should override earlier ones
	opts := NewGenerateOptions(
		WithTemperature(0.1),
		WithTemperature(0.9),
	)
	if *opts.Temperature != 0.9 {
		t.Errorf("Temperature = %v, want 0.9 (last option wins)", *opts.Temperature)
	}
}

// ---------------------------------------------------------------------------
// ErrProviderNotFound sentinel
// ---------------------------------------------------------------------------

func TestErrProviderNotFound(t *testing.T) {
	if ErrProviderNotFound == nil {
		t.Fatal("ErrProviderNotFound should not be nil")
	}
	if ErrProviderNotFound.Error() != "provider not found" {
		t.Errorf("ErrProviderNotFound.Error() = %q, want %q", ErrProviderNotFound.Error(), "provider not found")
	}
}

// ---------------------------------------------------------------------------
// StreamEventType constants
// ---------------------------------------------------------------------------

func TestStreamEventType_Values(t *testing.T) {
	tests := []struct {
		eventType StreamEventType
		want      string
	}{
		{StreamEventStart, "start"},
		{StreamEventChunk, "chunk"},
		{StreamEventToolCall, "tool_call"},
		{StreamEventDone, "done"},
		{StreamEventError, "error"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if string(tt.eventType) != tt.want {
				t.Errorf("StreamEventType = %q, want %q", string(tt.eventType), tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// FinishReason constants
// ---------------------------------------------------------------------------

func TestFinishReason_Values(t *testing.T) {
	if string(FinishReasonStop) != "stop" {
		t.Errorf("FinishReasonStop = %q, want %q", FinishReasonStop, "stop")
	}
	if string(FinishReasonLength) != "length" {
		t.Errorf("FinishReasonLength = %q, want %q", FinishReasonLength, "length")
	}
	if string(FinishReasonToolCall) != "tool_call" {
		t.Errorf("FinishReasonToolCall = %q, want %q", FinishReasonToolCall, "tool_call")
	}
	if string(FinishReasonContentFilter) != "content_filter" {
		t.Errorf("FinishReasonContentFilter = %q, want %q", FinishReasonContentFilter, "content_filter")
	}
	if string(FinishReasonError) != "error" {
		t.Errorf("FinishReasonError = %q, want %q", FinishReasonError, "error")
	}
}

// ---------------------------------------------------------------------------
// ModelCapabilities zero value
// ---------------------------------------------------------------------------

func TestModelCapabilities_ZeroValue(t *testing.T) {
	caps := ModelCapabilities{}
	if caps.Streaming != false {
		t.Error("zero-value Streaming should be false")
	}
	if caps.ToolCalling != false {
		t.Error("zero-value ToolCalling should be false")
	}
	if caps.MaxTokens != 0 {
		t.Error("zero-value MaxTokens should be 0")
	}
	if caps.ContextWindow != 0 {
		t.Error("zero-value ContextWindow should be 0")
	}
}

// ---------------------------------------------------------------------------
// ModelInfo
// ---------------------------------------------------------------------------

func TestModelInfo_Fields(t *testing.T) {
	m := ModelInfo{
		ID:          "gpt-4-turbo",
		Name:        "GPT-4 Turbo",
		Description: "Most capable model",
		Capabilities: ModelCapabilities{
			Streaming:  true,
			Vision:     true,
			MaxTokens:  4096,
		},
		Deprecated: false,
		Metadata:   map[string]any{"tier": "premium"},
	}

	if m.ID != "gpt-4-turbo" {
		t.Errorf("ID = %q, want %q", m.ID, "gpt-4-turbo")
	}
	if m.Name != "GPT-4 Turbo" {
		t.Errorf("Name = %q, want %q", m.Name, "GPT-4 Turbo")
	}
	if !m.Capabilities.Streaming {
		t.Error("Capabilities.Streaming should be true")
	}
	if m.Metadata["tier"] != "premium" {
		t.Errorf("Metadata['tier'] = %v, want 'premium'", m.Metadata["tier"])
	}
}

// ---------------------------------------------------------------------------
// GenerateRequest
// ---------------------------------------------------------------------------

func TestGenerateRequest_Fields(t *testing.T) {
	req := GenerateRequest{
		Model:    "gpt-4",
		Messages: []message.Message{message.UserMessage("hello")},
		Tools:    []ToolDefinition{FunctionTool("test", "desc", nil)},
		Options:  NewGenerateOptions(WithTemperature(0.5)),
		Metadata: map[string]any{"trace": "abc"},
	}

	if req.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", req.Model, "gpt-4")
	}
	if len(req.Messages) != 1 {
		t.Fatalf("Messages len = %d, want 1", len(req.Messages))
	}
	if req.Messages[0].Text() != "hello" {
		t.Errorf("Messages[0].Text() = %q, want %q", req.Messages[0].Text(), "hello")
	}
	if len(req.Tools) != 1 {
		t.Fatalf("Tools len = %d, want 1", len(req.Tools))
	}
	if req.Options.Temperature == nil || *req.Options.Temperature != 0.5 {
		t.Errorf("Options.Temperature = %v, want 0.5", req.Options.Temperature)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func floatPtr(f float64) *float64 { return &f }
