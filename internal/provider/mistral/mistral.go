// Package mistral implements the Mistral AI Chat API provider for Orchestra.
//
// It supports Generate and Stream operations, function calling,
// JSON mode, safe prompt, and the full range of Mistral models.
//
// Register with the Orchestra provider registry:
//
//	registry.Register("mistral", mistral.Factory, cfg)
package mistral

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	defaultBaseURL      = "https://api.mistral.ai/v1"
	providerName        = "mistral"
	chatCompletionsPath = "/chat/completions"
	modelsPath          = "/models"
	defaultModel        = "mistral-large-latest"
	sseDataPrefix       = "data: "
	sseDoneMarker       = "[DONE]"
	httpTimeout         = 10 * time.Minute
	maxIdleConns        = 100
	maxIdleConnsPerHost = 100
	idleConnTimeout     = 90 * time.Second
)

// ---------------------------------------------------------------------------
// Mistral API Types — Request
// ---------------------------------------------------------------------------

// mistRequest is the request body for the Mistral Chat Completions API.
type mistRequest struct {
	Model          string           `json:"model"`
	Messages       []mistMessage    `json:"messages"`
	Temperature    *float64         `json:"temperature,omitempty"`
	TopP           *float64         `json:"top_p,omitempty"`
	MaxTokens      *int             `json:"max_tokens,omitempty"`
	Stop           []string         `json:"stop,omitempty"`
	Seed           *int64           `json:"seed,omitempty"`
	ResponseFormat *mistRespFormat  `json:"response_format,omitempty"`
	Tools          []mistTool       `json:"tools,omitempty"`
	ToolChoice     interface{}      `json:"tool_choice,omitempty"`
	Stream         bool             `json:"stream,omitempty"`
	RandomSeed     *int64           `json:"random_seed,omitempty"`
	SafePrompt     bool             `json:"safe_prompt,omitempty"`
	N              int              `json:"n,omitempty"`
	PresencePenalty  *float64       `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64       `json:"frequency_penalty,omitempty"`
}

// mistRespFormat specifies the response format.
type mistRespFormat struct {
	Type string `json:"type"`
}

// mistMessage represents a message in the Mistral chat format.
type mistMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCalls  []mistToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

// mistContentPart represents a single content part in a multi-part message.
type mistContentPart struct {
	Type     string       `json:"type"`
	Text     string       `json:"text,omitempty"`
	ImageURL *mistImgURL  `json:"image_url,omitempty"`
}

// mistImgURL represents an image URL.
type mistImgURL struct {
	URL string `json:"url"`
}

// mistTool represents a tool definition in the request.
type mistTool struct {
	Type     string      `json:"type"`
	Function mistFuncDef `json:"function"`
}

// mistFuncDef describes a function the model can call.
type mistFuncDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// mistToolCall represents a tool call in an assistant message.
type mistToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function mistFunction `json:"function"`
}

// mistFunction holds the function name and arguments.
type mistFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ---------------------------------------------------------------------------
// Mistral API Types — Response
// ---------------------------------------------------------------------------

// mistResponse is the response from the Chat Completions API.
type mistResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []mistChoice `json:"choices"`
	Usage   mistUsage    `json:"usage,omitempty"`
}

// mistChoice is a single completion choice.
type mistChoice struct {
	Index        int         `json:"index"`
	Message      mistMessage `json:"message"`
	FinishReason *string     `json:"finish_reason"`
}

// mistUsage reports token consumption.
type mistUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// mistErrorResponse wraps an API error response.
type mistErrorResponse struct {
	Error mistErrorDetail `json:"error"`
}

// mistErrorDetail contains details about an API error.
type mistErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

// --- Streaming types ---

// mistStreamChunk is a single chunk in a streaming response.
type mistStreamChunk struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []mistStreamChoice `json:"choices"`
	Usage   *mistUsage         `json:"usage,omitempty"`
}

// mistStreamChoice is a single choice in a stream chunk.
type mistStreamChoice struct {
	Index        int             `json:"index"`
	Delta        mistStreamDelta `json:"delta"`
	FinishReason *string         `json:"finish_reason"`
}

// mistStreamDelta holds incremental content in a stream chunk.
type mistStreamDelta struct {
	Role      string             `json:"role,omitempty"`
	Content   string             `json:"content,omitempty"`
	ToolCalls []mistToolCallDelta `json:"tool_calls,omitempty"`
}

// mistToolCallDelta represents an incremental tool call update in a stream.
type mistToolCallDelta struct {
	Index    int                `json:"index"`
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function *mistFunctionDelta `json:"function,omitempty"`
}

// mistFunctionDelta holds incremental function data in a stream.
type mistFunctionDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ---------------------------------------------------------------------------
// Known Models
// ---------------------------------------------------------------------------

// knownModels is the static list of well-known Mistral models.
var knownModels = []provider.ModelInfo{
	{
		ID:          "mistral-large-latest",
		Name:        "Mistral Large",
		Description: "Most capable Mistral model for complex tasks",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 128000,
		},
	},
	{
		ID:          "mistral-medium-latest",
		Name:        "Mistral Medium",
		Description: "Balanced Mistral model for most tasks",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 32000,
		},
	},
	{
		ID:          "mistral-small-latest",
		Name:        "Mistral Small",
		Description: "Fast and affordable Mistral model",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 32000,
		},
	},
	{
		ID:          "open-mistral-nemo",
		Name:        "Mistral Nemo",
		Description: "Open-weight model with 128k context",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 128000,
		},
	},
	{
		ID:          "open-mistral-7b",
		Name:        "Mistral 7B",
		Description: "Open-weight 7B parameter model",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: false, Vision: false, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 32000,
		},
	},
	{
		ID:          "open-mixtral-8x7b",
		Name:        "Mixtral 8x7B",
		Description: "Open-weight mixture of experts model",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 32000,
		},
	},
	{
		ID:          "open-mixtral-8x22b",
		Name:        "Mixtral 8x22B",
		Description: "Large open-weight mixture of experts model",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 64000,
		},
	},
	{
		ID:          "codestral-latest",
		Name:        "Codestral",
		Description: "Code-focused Mistral model",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: false, Vision: false, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 32000,
		},
	},
	{
		ID:          "pixtral-large-latest",
		Name:        "Pixtral Large",
		Description: "Multimodal Mistral model with vision",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 128000,
		},
	},
	{
		ID:          "mistral-embed",
		Name:        "Mistral Embed",
		Description: "Embedding model (not for generation)",
		Capabilities: provider.ModelCapabilities{
			Streaming: false, ToolCalling: false, Vision: false, Audio: false,
			JSONMode: false, Seed: false, MaxTokens: 0, ContextWindow: 8192,
		},
		Deprecated: true,
	},
}

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

// Provider implements the provider.Provider interface for Mistral AI.
// It communicates with the Mistral Chat API and supports both Generate
// and Stream operations.
//
// A Provider is safe for concurrent use by multiple goroutines.
type Provider struct {
	apiKey       string
	baseURL      string
	defaultModel string
	httpClient   *http.Client
}

// NewProvider creates a new Mistral provider from the given configuration.
func NewProvider(cfg config.ProviderConfig) (*Provider, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		return nil, fmt.Errorf("mistral: api_key is required")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	dm := cfg.DefaultModel
	if dm == "" {
		dm = defaultModel
	}

	return &Provider{
		apiKey:       apiKey,
		baseURL:      baseURL,
		defaultModel: dm,
		httpClient: &http.Client{
			Timeout: httpTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        maxIdleConns,
				MaxIdleConnsPerHost: maxIdleConnsPerHost,
				IdleConnTimeout:     idleConnTimeout,
			},
		},
	}, nil
}

// Factory is a provider.ProviderFactory that creates a new Mistral provider.
// Use this with the provider registry:
//
//	registry.Register("mistral", mistral.Factory, cfg)
var Factory provider.ProviderFactory = func(cfg config.ProviderConfig) (provider.Provider, error) {
	return NewProvider(cfg)
}

// Compile-time interface check.
var _ provider.Provider = (*Provider)(nil)

// ---------------------------------------------------------------------------
// Interface Methods
// ---------------------------------------------------------------------------

// Name returns the provider identifier "mistral".
func (p *Provider) Name() string {
	return providerName
}

// Models returns the list of well-known Mistral models.
func (p *Provider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	result := make([]provider.ModelInfo, len(knownModels))
	copy(result, knownModels)
	return result, nil
}

// Generate sends a non-streaming completion request to the Mistral API.
func (p *Provider) Generate(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResult, error) {
	model := p.resolveModel(req.Model)
	if len(req.Messages) == 0 {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("at least one message is required"))
	}

	mistReq := p.buildRequest(model, req, false)

	bodyBytes, err := json.Marshal(mistReq)
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

	var mistResp mistResponse
	if err := json.NewDecoder(resp.Body).Decode(&mistResp); err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to decode response: %w", err))
	}

	result, err := p.convertResponse(&mistResp)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model, err)
	}

	return result, nil
}

// Stream sends a streaming completion request to the Mistral API and returns
// a channel of StreamEvent values. Mistral uses SSE streaming, similar to OpenAI.
func (p *Provider) Stream(ctx context.Context, req provider.GenerateRequest) (<-chan provider.StreamEvent, error) {
	model := p.resolveModel(req.Model)
	if len(req.Messages) == 0 {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("at least one message is required"))
	}

	mistReq := p.buildRequest(model, req, true)

	bodyBytes, err := json.Marshal(mistReq)
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

	// Match by prefix for model variants
	lower := strings.ToLower(resolved)
	switch {
	case strings.Contains(lower, "pixtral"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 128000,
		}
	case strings.Contains(lower, "large"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 128000,
		}
	case strings.Contains(lower, "medium"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 32000,
		}
	case strings.Contains(lower, "small"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 32000,
		}
	case strings.Contains(lower, "nemo"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 128000,
		}
	case strings.Contains(lower, "codestral"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: false, Vision: false, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 32000,
		}
	case strings.Contains(lower, "mixtral-8x22b"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 64000,
		}
	case strings.Contains(lower, "mixtral"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 32000,
		}
	case strings.Contains(lower, "mistral-7b") || strings.Contains(lower, "open-mistral-7b"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: false, Vision: false, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 32000,
		}
	}

	// Sensible default for unknown models
	return provider.ModelCapabilities{
		Streaming: true, ToolCalling: true, Vision: false, Audio: false,
		JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 32000,
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
}

// buildRequest constructs a Mistral API request from an Orchestra request.
func (p *Provider) buildRequest(model string, req provider.GenerateRequest, stream bool) mistRequest {
	mistReq := mistRequest{
		Model:    model,
		Messages: convertMessages(req.Messages),
		Stream:   stream,
		N:        1,
	}

	// Tools
	if len(req.Tools) > 0 {
		mistReq.Tools = convertTools(req.Tools)
	}

	// Options
	opts := req.Options
	if opts.Temperature != nil {
		mistReq.Temperature = opts.Temperature
	}
	if opts.TopP != nil {
		mistReq.TopP = opts.TopP
	}
	if opts.MaxTokens != nil {
		mistReq.MaxTokens = opts.MaxTokens
	}
	if len(opts.StopSequences) > 0 {
		mistReq.Stop = opts.StopSequences
	}
	if opts.Seed != nil {
		mistReq.RandomSeed = opts.Seed
	}

	// Response format
	if opts.ResponseFormat != nil {
		switch opts.ResponseFormat.Type {
		case "json_object":
			mistReq.ResponseFormat = &mistRespFormat{Type: "json_object"}
		case "text":
			mistReq.ResponseFormat = &mistRespFormat{Type: "text"}
		}
	}

	// Extra provider-specific options
	if opts.Extra != nil {
		if v, ok := asBool(opts.Extra["safe_prompt"]); ok && v {
			mistReq.SafePrompt = true
		}
		if v, ok := asFloat64(opts.Extra["presence_penalty"]); ok {
			mistReq.PresencePenalty = &v
		}
		if v, ok := asFloat64(opts.Extra["frequency_penalty"]); ok {
			mistReq.FrequencyPenalty = &v
		}
		if v, ok := asString(opts.Extra["tool_choice"]); ok {
			mistReq.ToolChoice = v
		}
		// "any" or "none" or "auto" for tool_choice
		if v, ok := opts.Extra["tool_choice"]; ok {
			mistReq.ToolChoice = v
		}
	}

	return mistReq
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

		var chunk mistStreamChunk
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
func (a *toolCallAccumulator) add(delta mistToolCallDelta) {
	idx := delta.Index
	tc, exists := a.calls[idx]
	if !exists {
		tc = &message.ToolCall{
			Type: "function",
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
// Conversion: Orchestra → Mistral
// ---------------------------------------------------------------------------

// convertMessages converts Orchestra messages to the Mistral message format.
func convertMessages(msgs []message.Message) []mistMessage {
	result := make([]mistMessage, 0, len(msgs))
	for _, msg := range msgs {
		mistMsg := mistMessage{
			Role: convertRole(msg.Role),
			Name: msg.Name,
		}

		switch msg.Role {
		case message.RoleTool:
			// Tool result message
			if msg.ToolResult != nil {
				mistMsg.ToolCallID = msg.ToolResult.ToolCallID
				mistMsg.Content = jsonString(msg.ToolResult.Content)
			}

		case message.RoleAssistant:
			if len(msg.ToolCalls) > 0 {
				mistMsg.ToolCalls = convertToolCallsOut(msg.ToolCalls)
				text := msg.Text()
				if text != "" {
					mistMsg.Content = jsonString(text)
				}
			} else {
				mistMsg.Content = convertContentBlocks(msg.Content)
			}

		case message.RoleFunction:
			// Legacy function role — map to tool role for compatibility
			mistMsg.Role = "tool"
			if msg.ToolResult != nil {
				mistMsg.ToolCallID = msg.ToolResult.ToolCallID
				mistMsg.Content = jsonString(msg.ToolResult.Content)
			}

		default:
			// System and User messages
			mistMsg.Content = convertContentBlocks(msg.Content)
		}

		result = append(result, mistMsg)
	}
	return result
}

// convertRole maps an Orchestra role to a Mistral role string.
func convertRole(role message.Role) string {
	return string(role)
}

// convertContentBlocks converts ContentBlocks to a json.RawMessage.
func convertContentBlocks(blocks []message.ContentBlock) json.RawMessage {
	if len(blocks) == 0 {
		return nil
	}

	// Single text block → plain string content
	if len(blocks) == 1 && blocks[0].Type == "text" && blocks[0].ImageURL == "" {
		return jsonString(blocks[0].Text)
	}

	// Multiple blocks → array of content parts
	parts := make([]mistContentPart, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if block.Text != "" {
				parts = append(parts, mistContentPart{
					Type: "text",
					Text: block.Text,
				})
			}
		case "image":
			if block.ImageURL != "" {
				parts = append(parts, mistContentPart{
					Type:     "image_url",
					ImageURL: &mistImgURL{URL: block.ImageURL},
				})
			}
		}
	}

	if len(parts) == 0 {
		return nil
	}

	// Single text part → plain string
	if len(parts) == 1 && parts[0].Type == "text" {
		return jsonString(parts[0].Text)
	}

	return jsonRaw(parts)
}

// convertTools converts Orchestra tool definitions to Mistral tool format.
func convertTools(tools []provider.ToolDefinition) []mistTool {
	result := make([]mistTool, 0, len(tools))
	for _, tool := range tools {
		params := tool.Function.Parameters
		if params == nil {
			params = map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}
		}
		result = append(result, mistTool{
			Type: "function",
			Function: mistFuncDef{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  params,
			},
		})
	}
	return result
}

// convertToolCallsOut converts Orchestra tool calls to Mistral tool calls.
func convertToolCallsOut(tcs []message.ToolCall) []mistToolCall {
	result := make([]mistToolCall, 0, len(tcs))
	for _, tc := range tcs {
		result = append(result, mistToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: mistFunction{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}
	return result
}

// ---------------------------------------------------------------------------
// Conversion: Mistral → Orchestra
// ---------------------------------------------------------------------------

// convertResponse converts a Mistral chat response to a GenerateResult.
func (p *Provider) convertResponse(resp *mistResponse) (*provider.GenerateResult, error) {
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := resp.Choices[0]
	msg := convertMistMessage(choice.Message)

	finishReason := provider.FinishReasonStop
	if choice.FinishReason != nil {
		finishReason = mapFinishReason(*choice.FinishReason)
	}

	usage := provider.TokenUsage{}
	if resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0 {
		usage = provider.TokenUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
	}

	result := &provider.GenerateResult{
		ID:           resp.ID,
		Message:      msg,
		Usage:        usage,
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

// convertMistMessage converts a Mistral response message to an Orchestra Message.
func convertMistMessage(mistMsg mistMessage) message.Message {
	msg := message.Message{
		Role: message.Role(mistMsg.Role),
	}

	// Parse content
	text := extractText(mistMsg.Content)
	if text != "" {
		msg.Content = []message.ContentBlock{message.TextContentBlock(text)}
	}

	// Convert tool calls
	if len(mistMsg.ToolCalls) > 0 {
		msg.ToolCalls = make([]message.ToolCall, len(mistMsg.ToolCalls))
		for i, tc := range mistMsg.ToolCalls {
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return provider.NewProviderErrorWithCode(providerName, model, "http_error", resp.StatusCode,
			fmt.Errorf("HTTP %d: failed to read error body: %w", resp.StatusCode, err))
	}

	var errResp mistErrorResponse
	if err := json.Unmarshal(body, &errResp); err != nil {
		return provider.NewProviderErrorWithCode(providerName, model, "http_error", resp.StatusCode,
			fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body)))
	}

	code := errResp.Error.Code
	if code == "" {
		code = errResp.Error.Type
	}
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

// ---------------------------------------------------------------------------
// Mapping Helpers
// ---------------------------------------------------------------------------

// mapFinishReason maps a Mistral finish reason string to a FinishReason.
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
	case "model_length":
		return provider.FinishReasonLength
	default:
		return provider.FinishReasonStop
	}
}

// extractText extracts a string value from a json.RawMessage.
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

// asBool extracts a bool from an any value.
func asBool(v any) (bool, bool) {
	b, ok := v.(bool)
	return b, ok
}

// asString extracts a string from an any value.
func asString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}
