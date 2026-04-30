// Package openai implements the OpenAI Chat Completions API provider for Orchestra.
//
// It supports Generate and Stream operations, tool/function calling, vision
// (image inputs via URL and base64), JSON mode, structured outputs, and the
// full range of OpenAI models including GPT-4o and reasoning models.
//
// Register with the Orchestra provider registry:
//
//	registry.Register("openai", openai.Factory, cfg)
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/user/orchestra/internal/config"
	"github.com/user/orchestra/internal/message"
	"github.com/user/orchestra/internal/provider"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	defaultBaseURL      = "https://api.openai.com/v1"
	providerName        = "openai"
	chatCompletionsPath = "/chat/completions"
	modelsPath          = "/models"
	defaultModel        = "gpt-4o"
	sseDataPrefix       = "data: "
	sseDoneMarker       = "[DONE]"
	httpTimeout         = 10 * time.Minute
	maxIdleConns        = 100
	maxIdleConnsPerHost = 100
	idleConnTimeout     = 90 * time.Second
)

// ---------------------------------------------------------------------------
// OpenAI API Types
// ---------------------------------------------------------------------------

// oaiChatRequest is the request body for the Chat Completions API.
type oaiChatRequest struct {
	Model               string             `json:"model"`
	Messages            []oaiMessage       `json:"messages"`
	Temperature         *float64           `json:"temperature,omitempty"`
	TopP                *float64           `json:"top_p,omitempty"`
	MaxTokens           *int               `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int               `json:"max_completion_tokens,omitempty"`
	Stop                []string           `json:"stop,omitempty"`
	Seed                *int64             `json:"seed,omitempty"`
	ResponseFormat      *oaiResponseFormat `json:"response_format,omitempty"`
	Tools               []oaiTool          `json:"tools,omitempty"`
	Stream              bool               `json:"stream,omitempty"`
	StreamOptions       *oaiStreamOptions  `json:"stream_options,omitempty"`
	FrequencyPenalty    *float64           `json:"frequency_penalty,omitempty"`
	PresencePenalty     *float64           `json:"presence_penalty,omitempty"`
	N                   int                `json:"n,omitempty"`
}

// oaiStreamOptions enables usage reporting in streaming responses.
type oaiStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// oaiResponseFormat specifies the response format for the API.
type oaiResponseFormat struct {
	Type       string         `json:"type"`
	JSONSchema *oaiJSONSchema `json:"json_schema,omitempty"`
}

// oaiJSONSchema describes a JSON schema for structured outputs.
type oaiJSONSchema struct {
	Name   string         `json:"name"`
	Schema map[string]any `json:"schema"`
	Strict bool           `json:"strict"`
}

// oaiMessage represents a message in the OpenAI Chat format.
// Content uses json.RawMessage so it can be either a string or an
// array of content parts.
type oaiMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCalls  []oaiToolCall   `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

// oaiContentPart represents a single content part in a multi-part message.
type oaiContentPart struct {
	Type     string       `json:"type"`
	Text     string       `json:"text,omitempty"`
	ImageURL *oaiImageURL `json:"image_url,omitempty"`
}

// oaiImageURL represents an image URL or data URI.
type oaiImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// oaiTool represents a tool definition in the request.
type oaiTool struct {
	Type     string     `json:"type"`
	Function oaiFuncDef `json:"function"`
}

// oaiFuncDef describes a function that can be called by the model.
type oaiFuncDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// oaiToolCall represents a tool call in a message.
type oaiToolCall struct {
	ID       string      `json:"id"`
	Type     string      `json:"type"`
	Function oaiFunction `json:"function"`
}

// oaiFunction holds the function name and arguments for a tool call.
type oaiFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// oaiChatResponse is the response from the Chat Completions API.
type oaiChatResponse struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Created int64       `json:"created"`
	Model   string      `json:"model"`
	Choices []oaiChoice `json:"choices"`
	Usage   oaiUsage    `json:"usage,omitempty"`
}

// oaiChoice is a single completion choice.
type oaiChoice struct {
	Index        int        `json:"index"`
	Message      oaiMessage `json:"message"`
	FinishReason *string    `json:"finish_reason"`
}

// oaiUsage reports token consumption.
type oaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// oaiErrorResponse wraps an API error response.
type oaiErrorResponse struct {
	Error oaiErrorDetail `json:"error"`
}

// oaiErrorDetail contains details about an API error.
type oaiErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param"`
	Code    string `json:"code"`
}

// --- Streaming types ---

// oaiStreamChunk is a single chunk in a streaming response.
type oaiStreamChunk struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Created int64             `json:"created"`
	Model   string            `json:"model"`
	Choices []oaiStreamChoice `json:"choices"`
	Usage   *oaiUsage         `json:"usage,omitempty"`
}

// oaiStreamChoice is a single choice in a stream chunk.
type oaiStreamChoice struct {
	Index        int            `json:"index"`
	Delta        oaiStreamDelta `json:"delta"`
	FinishReason *string        `json:"finish_reason"`
}

// oaiStreamDelta holds incremental content in a stream chunk.
type oaiStreamDelta struct {
	Role      string             `json:"role,omitempty"`
	Content   string             `json:"content,omitempty"`
	ToolCalls []oaiToolCallDelta `json:"tool_calls,omitempty"`
}

// oaiToolCallDelta represents an incremental tool call update in a stream.
type oaiToolCallDelta struct {
	Index    int               `json:"index"`
	ID       string            `json:"id,omitempty"`
	Type     string            `json:"type,omitempty"`
	Function *oaiFunctionDelta `json:"function,omitempty"`
}

// oaiFunctionDelta holds incremental function data in a stream.
type oaiFunctionDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ---------------------------------------------------------------------------
// Known Models
// ---------------------------------------------------------------------------

// knownModels is the static list of well-known OpenAI models.
var knownModels = []provider.ModelInfo{
	{
		ID:          "gpt-4o",
		Name:        "GPT-4o",
		Description: "Most advanced multimodal model with vision, audio, and tool calling",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: true,
			JSONMode: true, Seed: true, MaxTokens: 16384, ContextWindow: 128000,
		},
	},
	{
		ID:          "gpt-4o-mini",
		Name:        "GPT-4o Mini",
		Description: "Fast, affordable multimodal model",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 16384, ContextWindow: 128000,
		},
	},
	{
		ID:          "gpt-4-turbo",
		Name:        "GPT-4 Turbo",
		Description: "GPT-4 with improved speed and 128k context",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 4096, ContextWindow: 128000,
		},
	},
	{
		ID:          "gpt-4",
		Name:        "GPT-4",
		Description: "GPT-4 base model with 8k context",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 8192,
		},
	},
	{
		ID:          "gpt-4-32k",
		Name:        "GPT-4 32K",
		Description: "GPT-4 with 32k context window",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 4096, ContextWindow: 32768,
		},
	},
	{
		ID:          "gpt-3.5-turbo",
		Name:        "GPT-3.5 Turbo",
		Description: "Fast, cost-effective model with 16k context",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 4096, ContextWindow: 16385,
		},
	},
	{
		ID:          "o1",
		Name:        "o1",
		Description: "Reasoning model with deep analysis capabilities",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 100000, ContextWindow: 200000,
		},
	},
	{
		ID:          "o1-mini",
		Name:        "o1 Mini",
		Description: "Smaller reasoning model for faster responses",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 65536, ContextWindow: 128000,
		},
	},
	{
		ID:          "o1-pro",
		Name:        "o1 Pro",
		Description: "Premium reasoning model with highest capability",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 100000, ContextWindow: 200000,
		},
	},
	{
		ID:          "o3-mini",
		Name:        "o3 Mini",
		Description: "Next-generation small reasoning model",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 100000, ContextWindow: 200000,
		},
	},
}

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

// Provider implements the provider.Provider interface for OpenAI.
// It communicates with the OpenAI Chat Completions API and supports
// both Generate and Stream operations.
//
// A Provider is safe for concurrent use by multiple goroutines.
type Provider struct {
	apiKey       string
	baseURL      string
	orgID        string
	defaultModel string
	httpClient   *http.Client
}

// NewProvider creates a new OpenAI provider from the given configuration.
func NewProvider(cfg config.ProviderConfig) (*Provider, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		return nil, fmt.Errorf("openai: api_key is required")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	// Trim trailing slash
	baseURL = strings.TrimRight(baseURL, "/")

	dm := cfg.DefaultModel
	if dm == "" {
		dm = defaultModel
	}

	p := &Provider{
		apiKey:       apiKey,
		baseURL:      baseURL,
		orgID:        cfg.OrganizationID,
		defaultModel: dm,
		httpClient: &http.Client{
			Timeout: httpTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        maxIdleConns,
				MaxIdleConnsPerHost: maxIdleConnsPerHost,
				IdleConnTimeout:     idleConnTimeout,
			},
		},
	}

	return p, nil
}

// Factory is a provider.ProviderFactory that creates a new OpenAI provider.
// Use this with the provider registry:
//
//	registry.Register("openai", openai.Factory, cfg)
var Factory provider.ProviderFactory = func(cfg config.ProviderConfig) (provider.Provider, error) {
	return NewProvider(cfg)
}

// Compile-time interface check.
var _ provider.Provider = (*Provider)(nil)

// ---------------------------------------------------------------------------
// Interface Methods
// ---------------------------------------------------------------------------

// Name returns the provider identifier "openai".
func (p *Provider) Name() string {
	return providerName
}

// Models returns the list of well-known OpenAI models.
func (p *Provider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	result := make([]provider.ModelInfo, len(knownModels))
	copy(result, knownModels)
	return result, nil
}

// Generate sends a non-streaming completion request to the OpenAI API.
func (p *Provider) Generate(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResult, error) {
	model := p.resolveModel(req.Model)
	if len(req.Messages) == 0 {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("at least one message is required"))
	}

	oaiReq := p.buildRequest(model, req, false)

	bodyBytes, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to marshal request: %w", err))
	}

	url := p.baseURL + chatCompletionsPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to create request: %w", err))
	}
	p.setHeaders(httpReq)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("request failed: %w", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, p.handleAPIError(resp, model)
	}

	var oaiResp oaiChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to decode response: %w", err))
	}

	result, err := p.convertResponse(&oaiResp)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model, err)
	}

	return result, nil
}

// Stream sends a streaming completion request to the OpenAI API and returns
// a channel of StreamEvent values.
func (p *Provider) Stream(ctx context.Context, req provider.GenerateRequest) (<-chan provider.StreamEvent, error) {
	model := p.resolveModel(req.Model)
	if len(req.Messages) == 0 {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("at least one message is required"))
	}

	oaiReq := p.buildRequest(model, req, true)

	bodyBytes, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to marshal request: %w", err))
	}

	url := p.baseURL + chatCompletionsPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to create request: %w", err))
	}
	p.setHeaders(httpReq)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("request failed: %w", err))
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, p.handleAPIError(resp, model)
	}

	ch := make(chan provider.StreamEvent, 64)

	go p.streamEvents(ctx, resp, ch, model)

	return ch, nil
}

// Capabilities returns the feature set for a specific model.
func (p *Provider) Capabilities(model string) provider.ModelCapabilities {
	resolved := p.resolveModel(model)

	// Look up from known models
	for _, m := range knownModels {
		if m.ID == resolved {
			return m.Capabilities
		}
	}

	// Match by prefix for model variants (e.g., "gpt-4o-2024-05-13")
	lower := strings.ToLower(resolved)
	switch {
	case strings.Contains(lower, "gpt-4o"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: true,
			JSONMode: true, Seed: true, MaxTokens: 16384, ContextWindow: 128000,
		}
	case strings.Contains(lower, "gpt-4-turbo"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 4096, ContextWindow: 128000,
		}
	case strings.HasPrefix(lower, "gpt-4-32k"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 4096, ContextWindow: 32768,
		}
	case strings.HasPrefix(lower, "gpt-4"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 8192,
		}
	case strings.HasPrefix(lower, "gpt-3.5"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 4096, ContextWindow: 16385,
		}
	case strings.HasPrefix(lower, "o1") || strings.HasPrefix(lower, "o3"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 100000, ContextWindow: 200000,
		}
	}

	// Sensible default for unknown models
	return provider.ModelCapabilities{
		Streaming: true, ToolCalling: true, Vision: false, Audio: false,
		JSONMode: true, Seed: true, MaxTokens: 4096, ContextWindow: 8192,
	}
}

// ---------------------------------------------------------------------------
// Request Building
// ---------------------------------------------------------------------------

// resolveModel returns the model to use, falling back to the default.
func (p *Provider) resolveModel(model string) string {
	if model != "" {
		return model
	}
	return p.defaultModel
}

// setHeaders sets the required HTTP headers on the request.
func (p *Provider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	if p.orgID != "" {
		req.Header.Set("OpenAI-Organization", p.orgID)
	}
}

// buildRequest constructs an OpenAI API request from an Orchestra request.
func (p *Provider) buildRequest(model string, req provider.GenerateRequest, stream bool) oaiChatRequest {
	oaiReq := oaiChatRequest{
		Model:    model,
		Messages: convertMessages(req.Messages, model),
		Stream:   stream,
		N:        1,
	}

	if stream {
		oaiReq.StreamOptions = &oaiStreamOptions{IncludeUsage: true}
	}

	// Tools
	if len(req.Tools) > 0 {
		oaiReq.Tools = convertTools(req.Tools)
	}

	// Options
	opts := req.Options
	if opts.Temperature != nil {
		oaiReq.Temperature = opts.Temperature
	}
	if opts.TopP != nil {
		oaiReq.TopP = opts.TopP
	}
	if opts.MaxTokens != nil {
		if isCompletionTokensModel(model) {
			oaiReq.MaxCompletionTokens = opts.MaxTokens
		} else {
			oaiReq.MaxTokens = opts.MaxTokens
		}
	}
	if len(opts.StopSequences) > 0 {
		oaiReq.Stop = opts.StopSequences
	}
	if opts.Seed != nil {
		oaiReq.Seed = opts.Seed
	}
	if opts.ResponseFormat != nil {
		oaiReq.ResponseFormat = convertResponseFormat(opts.ResponseFormat)
	}

	// Extra provider-specific options
	if opts.Extra != nil {
		if v, ok := asFloat64(opts.Extra["frequency_penalty"]); ok {
			oaiReq.FrequencyPenalty = &v
		}
		if v, ok := asFloat64(opts.Extra["presence_penalty"]); ok {
			oaiReq.PresencePenalty = &v
		}
		// Allow max_completion_tokens override via Extra for any model
		if v, ok := asInt(opts.Extra["max_completion_tokens"]); ok {
			oaiReq.MaxCompletionTokens = &v
		}
	}

	return oaiReq
}

// ---------------------------------------------------------------------------
// Streaming
// ---------------------------------------------------------------------------

// streamEvents reads SSE events from the response body and sends them on ch.
func (p *Provider) streamEvents(ctx context.Context, resp *http.Response, ch chan<- provider.StreamEvent, model string) {
	defer close(ch)
	defer resp.Body.Close()

	// Send start event
	if !sendEvent(ctx, ch, provider.StreamEvent{Type: provider.StreamEventStart}) {
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	// Increase scanner buffer for large chunks
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// Tool call accumulator for assembling incremental tool calls
	tcAccum := newToolCallAccumulator()
	var usage *provider.TokenUsage

	for scanner.Scan() {
		// Check context cancellation
		if ctx.Err() != nil {
			sendEvent(ctx, ch, provider.StreamEvent{
				Type:  provider.StreamEventError,
				Error: ctx.Err(),
			})
			return
		}

		line := scanner.Text()

		// Skip empty lines and SSE comments
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// Parse SSE data lines
		if !strings.HasPrefix(line, sseDataPrefix) {
			continue
		}

		data := strings.TrimPrefix(line, sseDataPrefix)

		// Check for stream end
		if data == sseDoneMarker {
			break
		}

		var chunk oaiStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			sendEvent(ctx, ch, provider.StreamEvent{
				Type:  provider.StreamEventError,
				Error: fmt.Errorf("failed to parse stream chunk: %w", err),
			})
			return
		}

		// Process choices
		for _, choice := range chunk.Choices {
			// Content chunks
			if choice.Delta.Content != "" {
				if !sendEvent(ctx, ch, provider.StreamEvent{
					Type:  provider.StreamEventChunk,
					Chunk: choice.Delta.Content,
				}) {
					return
				}
			}

			// Tool call deltas
			for _, tcDelta := range choice.Delta.ToolCalls {
				tcAccum.add(tcDelta)
			}

			// Finish reason
			if choice.FinishReason != nil {
				fr := *choice.FinishReason
				if fr == "tool_calls" {
					// Emit accumulated tool calls
					for _, tc := range tcAccum.collect() {
						if !sendEvent(ctx, ch, provider.StreamEvent{
							Type:     provider.StreamEventToolCall,
							ToolCall: &tc,
						}) {
							return
						}
					}
				}
			}
		}

		// Capture usage if present
		if chunk.Usage != nil {
			usage = &provider.TokenUsage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
		}
	}

	if err := scanner.Err(); err != nil {
		sendEvent(ctx, ch, provider.StreamEvent{
			Type:  provider.StreamEventError,
			Error: fmt.Errorf("stream read error: %w", err),
		})
		return
	}

	// Send done event with usage
	sendEvent(ctx, ch, provider.StreamEvent{
		Type:  provider.StreamEventDone,
		Usage: usage,
	})
}

// sendEvent sends an event on the channel, respecting context cancellation.
// Returns false if the event could not be sent (context cancelled).
func sendEvent(ctx context.Context, ch chan<- provider.StreamEvent, evt provider.StreamEvent) bool {
	select {
	case ch <- evt:
		return true
	case <-ctx.Done():
		return false
	}
}

// ---------------------------------------------------------------------------
// Tool Call Accumulator
// ---------------------------------------------------------------------------

// toolCallAccumulator assembles incremental tool call deltas from a stream.
type toolCallAccumulator struct {
	calls map[int]*message.ToolCall
}

func newToolCallAccumulator() *toolCallAccumulator {
	return &toolCallAccumulator{
		calls: make(map[int]*message.ToolCall),
	}
}

// add incorporates a tool call delta into the accumulator.
func (a *toolCallAccumulator) add(delta oaiToolCallDelta) {
	idx := delta.Index
	tc, exists := a.calls[idx]
	if !exists {
		tc = &message.ToolCall{
			Type:     "function",
			Function: message.ToolCallFunction{},
		}
		a.calls[idx] = tc
	}

	if delta.ID != "" {
		tc.ID = delta.ID
	}
	if delta.Type != "" {
		tc.Type = delta.Type
	}
	if delta.Function != nil {
		if delta.Function.Name != "" {
			tc.Function.Name += delta.Function.Name
		}
		tc.Function.Arguments += delta.Function.Arguments
	}
}

// collect returns the accumulated tool calls in index order.
func (a *toolCallAccumulator) collect() []message.ToolCall {
	if len(a.calls) == 0 {
		return nil
	}
	result := make([]message.ToolCall, 0, len(a.calls))
	for i := 0; i < len(a.calls); i++ {
		if tc, ok := a.calls[i]; ok {
			result = append(result, *tc)
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Conversion: Orchestra → OpenAI
// ---------------------------------------------------------------------------

// convertMessages converts Orchestra messages to the OpenAI message format.
func convertMessages(msgs []message.Message, model string) []oaiMessage {
	result := make([]oaiMessage, 0, len(msgs))
	for _, msg := range msgs {
		oaiMsg := oaiMessage{
			Role: convertRole(msg.Role, model),
			Name: msg.Name,
		}

		switch msg.Role {
		case message.RoleTool:
			// Tool result message
			if msg.ToolResult != nil {
				oaiMsg.ToolCallID = msg.ToolResult.ToolCallID
				if msg.ToolResult.IsError {
					oaiMsg.Content = jsonString(msg.ToolResult.Content)
				} else {
					oaiMsg.Content = jsonString(msg.ToolResult.Content)
				}
			}

		case message.RoleAssistant:
			if len(msg.ToolCalls) > 0 {
				oaiMsg.ToolCalls = convertToolCallsOut(msg.ToolCalls)
				text := msg.Text()
				if text != "" {
					oaiMsg.Content = jsonString(text)
				}
			} else {
				oaiMsg.Content = convertContentBlocks(msg.Content)
			}

		case message.RoleFunction:
			// Legacy function role — map to tool role for compatibility
			oaiMsg.Role = "tool"
			if msg.ToolResult != nil {
				oaiMsg.ToolCallID = msg.ToolResult.ToolCallID
				oaiMsg.Content = jsonString(msg.ToolResult.Content)
			}

		default:
			// System and User messages
			oaiMsg.Content = convertContentBlocks(msg.Content)
		}

		result = append(result, oaiMsg)
	}
	return result
}

// convertRole maps an Orchestra role to an OpenAI role string.
func convertRole(role message.Role, model string) string {
	// Standard mapping
	return string(role)
}

// convertContentBlocks converts ContentBlocks to a json.RawMessage.
// Returns nil if there are no blocks.
// Returns a plain string for a single text block.
// Returns an array of content parts for multi-part content.
func convertContentBlocks(blocks []message.ContentBlock) json.RawMessage {
	if len(blocks) == 0 {
		return nil
	}

	// Single text block → plain string content
	if len(blocks) == 1 && blocks[0].Type == "text" && blocks[0].ImageURL == "" && len(blocks[0].FileData) == 0 {
		return jsonString(blocks[0].Text)
	}

	// Multiple blocks or image/file blocks → array of content parts
	parts := make([]oaiContentPart, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case "text":
			parts = append(parts, oaiContentPart{
				Type: "text",
				Text: block.Text,
			})
		case "image":
			url := block.ImageURL
			if url == "" && len(block.FileData) > 0 {
				// Encode file data as base64 data URI
				mime := block.MimeType
				if mime == "" {
					mime = "image/png"
				}
				url = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(block.FileData)
			}
			if url != "" {
				parts = append(parts, oaiContentPart{
					Type:     "image_url",
					ImageURL: &oaiImageURL{URL: url},
				})
			}
		case "file":
			// OpenAI doesn't support generic file blocks directly.
			// If the file is an image, encode it as an image content part.
			if len(block.FileData) > 0 && strings.HasPrefix(block.MimeType, "image/") {
				url := "data:" + block.MimeType + ";base64," + base64.StdEncoding.EncodeToString(block.FileData)
				parts = append(parts, oaiContentPart{
					Type:     "image_url",
					ImageURL: &oaiImageURL{URL: url},
				})
			}
		}
	}

	if len(parts) == 0 {
		return nil
	}

	// Optimization: single text part → plain string
	if len(parts) == 1 && parts[0].Type == "text" {
		return jsonString(parts[0].Text)
	}

	return jsonRaw(parts)
}

// convertTools converts Orchestra tool definitions to OpenAI tool format.
func convertTools(tools []provider.ToolDefinition) []oaiTool {
	result := make([]oaiTool, 0, len(tools))
	for _, tool := range tools {
		result = append(result, oaiTool{
			Type: tool.Type,
			Function: oaiFuncDef{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  tool.Function.Parameters,
			},
		})
	}
	return result
}

// convertToolCallsOut converts Orchestra tool calls to OpenAI tool calls.
func convertToolCallsOut(tcs []message.ToolCall) []oaiToolCall {
	result := make([]oaiToolCall, 0, len(tcs))
	for _, tc := range tcs {
		result = append(result, oaiToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: oaiFunction{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}
	return result
}

// convertResponseFormat converts an Orchestra ResponseFormat to OpenAI format.
func convertResponseFormat(rf *provider.ResponseFormat) *oaiResponseFormat {
	if rf == nil {
		return nil
	}

	result := &oaiResponseFormat{
		Type: rf.Type,
	}

	if rf.Type == "json_schema" && rf.JSONSchema != nil {
		name := "response"
		if n, ok := rf.JSONSchema["name"].(string); ok && n != "" {
			name = n
		}
		schema := rf.JSONSchema
		if s, ok := rf.JSONSchema["schema"].(map[string]any); ok {
			schema = s
		}
		result.JSONSchema = &oaiJSONSchema{
			Name:   name,
			Schema: schema,
			Strict: true,
		}
	}

	return result
}

// ---------------------------------------------------------------------------
// Conversion: OpenAI → Orchestra
// ---------------------------------------------------------------------------

// convertResponse converts an OpenAI chat response to a GenerateResult.
func (p *Provider) convertResponse(resp *oaiChatResponse) (*provider.GenerateResult, error) {
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := resp.Choices[0]
	msg := convertOAIMessage(choice.Message)

	finishReason := provider.FinishReasonStop
	if choice.FinishReason != nil {
		finishReason = mapFinishReason(*choice.FinishReason)
	}

	result := &provider.GenerateResult{
		ID:           resp.ID,
		Message:      msg,
		Usage:        mapUsage(resp.Usage),
		FinishReason: finishReason,
		Model:        resp.Model,
		CreatedAt:    time.Now(),
		Metadata:     make(map[string]any),
	}

	if resp.Created > 0 {
		result.Metadata["created"] = resp.Created
	}

	return result, nil
}

// convertOAIMessage converts an OpenAI response message to an Orchestra Message.
func convertOAIMessage(oaiMsg oaiMessage) message.Message {
	msg := message.Message{
		Role: message.Role(oaiMsg.Role),
	}

	// Parse content
	text := extractText(oaiMsg.Content)
	if text != "" {
		msg.Content = []message.ContentBlock{message.TextContentBlock(text)}
	}

	// Convert tool calls
	if len(oaiMsg.ToolCalls) > 0 {
		msg.ToolCalls = make([]message.ToolCall, len(oaiMsg.ToolCalls))
		for i, tc := range oaiMsg.ToolCalls {
			msg.ToolCalls[i] = message.ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: message.ToolCallFunction{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			}
		}
	}

	return msg
}

// ---------------------------------------------------------------------------
// Error Handling
// ---------------------------------------------------------------------------

// handleAPIError parses an error HTTP response and returns a ProviderError.
func (p *Provider) handleAPIError(resp *http.Response, model string) error {
	body, err := readResponseBody(resp)
	if err != nil {
		return provider.NewProviderErrorWithCode(providerName, model, "http_error", resp.StatusCode,
			fmt.Errorf("HTTP %d: failed to read error body: %w", resp.StatusCode, err))
	}

	var errResp oaiErrorResponse
	if err := json.Unmarshal(body, &errResp); err != nil {
		return provider.NewProviderErrorWithCode(providerName, model, "http_error", resp.StatusCode,
			fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body)))
	}

	code := errResp.Error.Code
	if code == "" {
		code = fmt.Sprintf("http_%d", resp.StatusCode)
	}

	errMsg := errResp.Error.Message
	if errMsg == "" {
		errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	return provider.NewProviderErrorWithCode(providerName, model, code, resp.StatusCode,
		fmt.Errorf("%s", errMsg))
}

// readResponseBody reads and returns the response body as bytes.
func readResponseBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return readAll(resp.Body, 1024*1024) // Max 1MB error body
}

// readAll reads from r with a size limit.
func readAll(r interface{ Read([]byte) (int, error) }, maxSize int) ([]byte, error) {
	buf := make([]byte, 0, 512)
	tmp := make([]byte, 256)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			if len(buf) > maxSize {
				return buf, nil
			}
		}
		if err != nil {
			return buf, nil //nolint: nilerr // Return what we have
		}
	}
}

// ---------------------------------------------------------------------------
// Mapping Helpers
// ---------------------------------------------------------------------------

// mapFinishReason maps an OpenAI finish reason string to a FinishReason.
func mapFinishReason(reason string) provider.FinishReason {
	switch reason {
	case "stop":
		return provider.FinishReasonStop
	case "length":
		return provider.FinishReasonLength
	case "tool_calls":
		return provider.FinishReasonToolCall
	case "content_filter":
		return provider.FinishReasonContentFilter
	default:
		return provider.FinishReasonStop
	}
}

// mapUsage converts OpenAI usage to Orchestra TokenUsage.
func mapUsage(u oaiUsage) provider.TokenUsage {
	return provider.TokenUsage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
	}
}

// extractText extracts a string value from a json.RawMessage.
// Returns empty string if the raw message is nil, "null", or not a string.
func extractText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// isCompletionTokensModel returns true if the model uses max_completion_tokens
// instead of max_tokens.
func isCompletionTokensModel(model string) bool {
	lower := strings.ToLower(model)
	return strings.HasPrefix(lower, "o1") || strings.HasPrefix(lower, "o3")
}

// ---------------------------------------------------------------------------
// JSON Helpers
// ---------------------------------------------------------------------------

// jsonString creates a json.RawMessage from a string value.
func jsonString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

// jsonRaw creates a json.RawMessage by marshaling any value.
func jsonRaw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// asFloat64 extracts a float64 from an any value.
func asFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	default:
		return 0, false
	}
}

// asInt extracts an int from an any value.
func asInt(v any) (int, bool) {
	switch val := v.(type) {
	case int:
		return val, true
	case int64:
		return int(val), true
	case float64:
		return int(val), true
	default:
		return 0, false
	}
}
