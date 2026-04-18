// Package provider defines the core abstraction layer for LLM providers.
//
// The Provider interface is the primary contract that all LLM backends
// (OpenAI, Anthropic, Google Gemini, Ollama, etc.) must implement.
// It provides a uniform API for generating completions and streaming
// responses across heterogeneous model providers.
package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/user/orchestra/internal/message"
)

// FinishReason indicates why the model stopped generating tokens.
type FinishReason string

const (
	// FinishReasonStop indicates the model generated a natural stop point
	// (end of text, or a stop sequence was hit).
	FinishReasonStop FinishReason = "stop"

	// FinishReasonLength indicates generation stopped because the maximum
	// number of tokens was reached.
	FinishReasonLength FinishReason = "length"

	// FinishReasonToolCall indicates the model requested one or more tool
	// executions before continuing.
	FinishReasonToolCall FinishReason = "tool_call"

	// FinishReasonContentFilter indicates generation was stopped because the
	// output was flagged by the provider's content safety filter.
	FinishReasonContentFilter FinishReason = "content_filter"

	// FinishReasonError indicates generation stopped due to an error.
	FinishReasonError FinishReason = "error"
)

// String returns the string representation of the FinishReason.
func (r FinishReason) String() string {
	return string(r)
}

// IsTerminal returns true if this finish reason indicates the conversation
// turn has completed (as opposed to needing a tool call response).
func (r FinishReason) IsTerminal() bool {
	switch r {
	case FinishReasonStop, FinishReasonLength, FinishReasonContentFilter, FinishReasonError:
		return true
	default:
		return false
	}
}

// TokenUsage tracks token consumption for a single request.
type TokenUsage struct {
	// PromptTokens is the number of tokens in the input messages.
	PromptTokens int `json:"prompt_tokens" yaml:"prompt_tokens"`

	// CompletionTokens is the number of tokens in the generated response.
	CompletionTokens int `json:"completion_tokens" yaml:"completion_tokens"`

	// TotalTokens is the sum of PromptTokens and CompletionTokens.
	TotalTokens int `json:"total_tokens" yaml:"total_tokens"`
}

// Add returns a new TokenUsage that is the sum of this and another TokenUsage.
func (u TokenUsage) Add(other TokenUsage) TokenUsage {
	return TokenUsage{
		PromptTokens:     u.PromptTokens + other.PromptTokens,
		CompletionTokens: u.CompletionTokens + other.CompletionTokens,
		TotalTokens:      u.TotalTokens + other.TotalTokens,
	}
}

// ModelCapabilities describes the features a specific model supports.
// Different models within the same provider may have different capabilities.
type ModelCapabilities struct {
	// Streaming indicates whether the model supports streaming responses.
	Streaming bool `json:"streaming" yaml:"streaming"`

	// ToolCalling indicates whether the model supports function/tool calling.
	ToolCalling bool `json:"tool_calling" yaml:"tool_calling"`

	// Vision indicates whether the model can process image inputs.
	Vision bool `json:"vision" yaml:"vision"`

	// Audio indicates whether the model can process audio inputs.
	Audio bool `json:"audio" yaml:"audio"`

	// JSONMode indicates whether the model supports JSON response formatting.
	JSONMode bool `json:"json_mode" yaml:"json_mode"`

	// Seed indicates whether the model supports deterministic generation
	// via a seed parameter.
	Seed bool `json:"seed" yaml:"seed"`

	// MaxTokens is the maximum number of output tokens the model can generate.
	MaxTokens int `json:"max_tokens" yaml:"max_tokens"`

	// ContextWindow is the maximum total number of tokens (input + output)
	// the model can process in a single request.
	ContextWindow int `json:"context_window" yaml:"context_window"`
}

// ModelInfo describes a model available from a provider.
type ModelInfo struct {
	// ID is the model identifier used in API calls (e.g., "gpt-4-turbo").
	ID string `json:"id" yaml:"id"`

	// Name is a human-readable display name (e.g., "GPT-4 Turbo").
	Name string `json:"name" yaml:"name"`

	// Description is an optional longer description of the model.
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Capabilities lists the features this model supports.
	Capabilities ModelCapabilities `json:"capabilities" yaml:"capabilities"`

	// Deprecated indicates whether this model version is deprecated.
	Deprecated bool `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`

	// Metadata contains provider-specific model metadata.
	Metadata map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// ResponseFormat specifies how the model should format its output.
type ResponseFormat struct {
	// Type is the format type. Common values are "text" and "json_object".
	Type string `json:"type" yaml:"type"`

	// JSONSchema is an optional JSON Schema for structured outputs.
	// Only used when Type is "json_schema".
	JSONSchema map[string]any `json:"json_schema,omitempty" yaml:"json_schema,omitempty"`
}

// TextResponseFormat creates a ResponseFormat for plain text output.
func TextResponseFormat() *ResponseFormat {
	return &ResponseFormat{Type: "text"}
}

// JSONResponseFormat creates a ResponseFormat for JSON object output.
func JSONResponseFormat() *ResponseFormat {
	return &ResponseFormat{Type: "json_object"}
}

// JSONSchemaResponseFormat creates a ResponseFormat for structured JSON output
// conforming to the given JSON Schema.
func JSONSchemaResponseFormat(schema map[string]any) *ResponseFormat {
	return &ResponseFormat{
		Type:       "json_schema",
		JSONSchema: schema,
	}
}

// StreamEventType identifies the kind of event in a response stream.
type StreamEventType string

const (
	// StreamEventStart is emitted once when streaming begins.
	StreamEventStart StreamEventType = "start"

	// StreamEventChunk is emitted for each token or text fragment.
	StreamEventChunk StreamEventType = "chunk"

	// StreamEventToolCall is emitted when the model requests a tool call
	// during streaming. May be emitted incrementally (partial tool calls)
	// depending on the provider.
	StreamEventToolCall StreamEventType = "tool_call"

	// StreamEventDone is emitted once when streaming completes successfully.
	StreamEventDone StreamEventType = "done"

	// StreamEventError is emitted when an error occurs during streaming.
	StreamEventError StreamEventType = "error"
)

// StreamEvent represents a single event in a streaming response.
// A stream produces a sequence of events over a channel.
type StreamEvent struct {
	// Type identifies the kind of event.
	Type StreamEventType `json:"type" yaml:"type"`

	// Chunk contains the text content for StreamEventChunk events.
	Chunk string `json:"chunk,omitempty" yaml:"chunk,omitempty"`

	// ToolCall contains the tool call data for StreamEventToolCall events.
	ToolCall *message.ToolCall `json:"tool_call,omitempty" yaml:"tool_call,omitempty"`

	// Usage contains token usage information. Populated on
	// StreamEventDone events when the provider reports it.
	Usage *TokenUsage `json:"usage,omitempty" yaml:"usage,omitempty"`

	// Error contains the error for StreamEventError events.
	Error error `json:"-" yaml:"-"`
}

// GenerateOptions configures how a model generates its response.
// All pointer fields are optional — nil means "use the provider default".
type GenerateOptions struct {
	// Temperature controls randomness. Higher values (e.g., 0.8) produce
	// more random outputs; lower values (e.g., 0.2) are more deterministic.
	Temperature *float64 `json:"temperature,omitempty" yaml:"temperature,omitempty"`

	// TopP controls diversity via nucleus sampling. 0.5 means half of
	// likely-weighted options are considered.
	TopP *float64 `json:"top_p,omitempty" yaml:"top_p,omitempty"`

	// MaxTokens sets the maximum number of tokens in the generated response.
	MaxTokens *int `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`

	// StopSequences is a list of strings that will cause generation to stop
	// when encountered in the output.
	StopSequences []string `json:"stop_sequences,omitempty" yaml:"stop_sequences,omitempty"`

	// Seed enables deterministic sampling. Set the same seed and Temperature=0
	// for reproducible outputs (when the model supports it).
	Seed *int64 `json:"seed,omitempty" yaml:"seed,omitempty"`

	// ResponseFormat constrains the output format (e.g., JSON mode).
	ResponseFormat *ResponseFormat `json:"response_format,omitempty" yaml:"response_format,omitempty"`

	// Extra holds provider-specific options that don't map to the
	// standard fields above. Keys are provider-specific.
	Extra map[string]any `json:"extra,omitempty" yaml:"extra,omitempty"`
}

// GenerateRequest represents a request to generate a completion from a model.
type GenerateRequest struct {
	// Model is the model identifier to use (e.g., "gpt-4-turbo").
	Model string `json:"model" yaml:"model"`

	// Messages is the ordered conversation to send to the model.
	Messages []message.Message `json:"messages" yaml:"messages"`

	// Tools is the list of tools the model may invoke during generation.
	// This field is optional — omit it if no tools are available.
	Tools []ToolDefinition `json:"tools,omitempty" yaml:"tools,omitempty"`

	// Options configures the generation parameters.
	Options GenerateOptions `json:"options" yaml:"options"`

	// Metadata contains arbitrary data associated with this request.
	// It is not sent to the provider; it is used for internal tracking.
	Metadata map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// ToolDefinition defines a tool that the model can invoke.
type ToolDefinition struct {
	// Type is the tool type, typically "function".
	Type string `json:"type" yaml:"type"`

	// Function describes the function the model can call.
	Function FunctionDef `json:"function" yaml:"function"`
}

// FunctionDef describes a function that can be called by the model.
type FunctionDef struct {
	// Name is the function identifier.
	Name string `json:"name" yaml:"name"`

	// Description explains what the function does, helping the model decide
	// when to call it.
	Description string `json:"description" yaml:"description"`

	// Parameters is a JSON Schema object describing the function's input
	// parameters.
	Parameters map[string]any `json:"parameters" yaml:"parameters"`
}

// FunctionTool creates a ToolDefinition for a function-type tool.
func FunctionTool(name, description string, parameters map[string]any) ToolDefinition {
	return ToolDefinition{
		Type: "function",
		Function: FunctionDef{
			Name:        name,
			Description: description,
			Parameters:  parameters,
		},
	}
}

// GenerateResult holds the result of a single completion generation.
type GenerateResult struct {
	// ID is the unique identifier for this completion (provider-assigned).
	ID string `json:"id" yaml:"id"`

	// Message is the generated response message from the model.
	Message message.Message `json:"message" yaml:"message"`

	// Usage reports token consumption for this request.
	Usage TokenUsage `json:"usage" yaml:"usage"`

	// FinishReason indicates why the model stopped generating.
	FinishReason FinishReason `json:"finish_reason" yaml:"finish_reason"`

	// Model is the actual model ID used (may differ from request if
	// an alias or default was resolved).
	Model string `json:"model" yaml:"model"`

	// CreatedAt is the timestamp when this result was generated.
	CreatedAt time.Time `json:"created_at" yaml:"created_at"`

	// Metadata contains provider-specific response metadata.
	Metadata map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// IsToolCall returns true if the result contains tool call requests.
func (r *GenerateResult) IsToolCall() bool {
	return r.FinishReason == FinishReasonToolCall || r.Message.IsToolCall()
}

// Text returns the text content of the generated message.
func (r *GenerateResult) Text() string {
	return r.Message.Text()
}

// ProviderConfig holds the configuration needed to create a Provider instance.
// Each provider implementation defines its own config structure; this provides
// the common base.
type ProviderConfig struct {
	// APIKey is the authentication key for the provider's API.
	APIKey string `json:"api_key,omitempty" yaml:"api_key,omitempty"`

	// BaseURL is the provider's API endpoint. If empty, the provider default is used.
	BaseURL string `json:"base_url,omitempty" yaml:"base_url,omitempty"`

	// DefaultModel is the model used when none is specified in a request.
	DefaultModel string `json:"default_model" yaml:"default_model"`

	// RateLimit specifies rate limiting configuration.
	RateLimit RateLimitConfig `json:"rate_limit,omitempty" yaml:"rate_limit,omitempty"`

	// Retry specifies retry configuration for transient failures.
	Retry RetryConfig `json:"retry,omitempty" yaml:"retry,omitempty"`

	// Extra contains provider-specific configuration options.
	Extra map[string]any `json:"extra,omitempty" yaml:"extra,omitempty"`
}

// RateLimitConfig configures rate limiting behavior for a provider.
type RateLimitConfig struct {
	// RequestsPerMinute is the maximum number of API requests per minute.
	RequestsPerMinute int `json:"requests_per_minute,omitempty" yaml:"requests_per_minute,omitempty"`

	// TokensPerMinute is the maximum number of tokens (prompt + completion) per minute.
	TokensPerMinute int `json:"tokens_per_minute,omitempty" yaml:"tokens_per_minute,omitempty"`
}

// RetryConfig configures retry behavior for transient failures.
type RetryConfig struct {
	// MaxAttempts is the maximum number of retry attempts (0 = no retries).
	MaxAttempts int `json:"max_attempts,omitempty" yaml:"max_attempts,omitempty"`

	// InitialBackoff is the duration to wait before the first retry.
	InitialBackoff time.Duration `json:"initial_backoff,omitempty" yaml:"initial_backoff,omitempty"`

	// MaxBackoff is the maximum backoff duration between retries.
	MaxBackoff time.Duration `json:"max_backoff,omitempty" yaml:"max_backoff,omitempty"`

	// BackoffMultiplier is the factor by which backoff increases after each retry.
	// A value of 2.0 means each retry waits twice as long as the previous.
	BackoffMultiplier float64 `json:"backoff_multiplier,omitempty" yaml:"backoff_multiplier,omitempty"`
}

// ProviderFactory is a function that creates a new Provider instance
// from the given configuration.
type ProviderFactory func(config ProviderConfig) (Provider, error)

// Provider is the core interface that all LLM backends must implement.
// It provides a uniform API for generating completions and streaming
// responses across different model providers.
//
// Implementations must be safe for concurrent use. A single Provider
// instance may be shared across multiple goroutines and agents.
type Provider interface {
	// Name returns the provider identifier (e.g., "openai", "anthropic").
	// This is used for logging, metrics, and registry lookups.
	Name() string

	// Models returns the list of model IDs available for this provider.
	// The context can be used for cancellation.
	Models(ctx context.Context) ([]ModelInfo, error)

	// Generate sends a conversation and returns a single completion.
	// The request specifies the model, messages, tools, and generation options.
	Generate(ctx context.Context, req GenerateRequest) (*GenerateResult, error)

	// Stream sends a conversation and returns a channel of streaming events.
	// The caller should read events from the channel until it is closed.
	// If an error occurs during setup, it is returned immediately.
	// Errors during streaming are sent as StreamEventError events.
	Stream(ctx context.Context, req GenerateRequest) (<-chan StreamEvent, error)

	// Capabilities returns the feature flags for a specific model.
	Capabilities(model string) ModelCapabilities
}

// ProviderError wraps an error from a provider with additional context.
type ProviderError struct {
	// Provider is the name of the provider that produced the error.
	Provider string

	// Model is the model being used when the error occurred.
	Model string

	// Code is an optional provider-specific error code.
	Code string

	// StatusCode is the HTTP status code (if applicable).
	StatusCode int

	// Err is the underlying error.
	Err error
}

// Error implements the error interface.
func (e *ProviderError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("provider %s: model %s: [%s] %v", e.Provider, e.Model, e.Code, e.Err)
	}
	return fmt.Sprintf("provider %s: model %s: %v", e.Provider, e.Model, e.Err)
}

// Unwrap returns the underlying error for error chain inspection.
func (e *ProviderError) Unwrap() error {
	return e.Err
}

// ErrProviderNotFound is returned when a requested provider is not registered
// in the registry.
var ErrProviderNotFound = fmt.Errorf("provider not found")

// NewProviderError creates a new ProviderError.
func NewProviderError(provider, model string, err error) *ProviderError {
	return &ProviderError{
		Provider: provider,
		Model:    model,
		Err:      err,
	}
}

// NewProviderErrorWithCode creates a new ProviderError with a specific code.
func NewProviderErrorWithCode(provider, model, code string, statusCode int, err error) *ProviderError {
	return &ProviderError{
		Provider:   provider,
		Model:      model,
		Code:       code,
		StatusCode: statusCode,
		Err:        err,
	}
}
