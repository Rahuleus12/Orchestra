// Package anthropic implements the Anthropic Messages API provider for Orchestra.
//
// It supports Generate and Stream operations, tool use with Anthropic's
// tool_use/tool_result content blocks, vision (image inputs via base64),
// prompt caching, extended thinking, and the full range of Claude models.
//
// Register with the Orchestra provider registry:
//
//	registry.Register("anthropic", anthropic.Factory, cfg)
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
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
	defaultBaseURL    = "https://api.anthropic.com/v1"
	providerName      = "anthropic"
	messagesPath      = "/messages"
	defaultModel      = "claude-sonnet-4-20250514"
	apiVersionHeader  = "2023-06-01"
	sseDataPrefix     = "data: "
	sseEventPrefix    = "event: "
	httpTimeout       = 10 * time.Minute
	maxIdleConns      = 100
	maxIdleConnsPerHost = 100
	idleConnTimeout   = 90 * time.Second
)

// ---------------------------------------------------------------------------
// Anthropic API Types
// ---------------------------------------------------------------------------

// antRequest is the request body for the Anthropic Messages API.
type antRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	System    json.RawMessage `json:"system,omitempty"`
	Messages  []antMessage    `json:"messages"`
	Tools     []antTool       `json:"tools,omitempty"`
	Stream    bool            `json:"stream,omitempty"`

	// Optional generation parameters
	Temperature *float64  `json:"temperature,omitempty"`
	TopP        *float64  `json:"top_p,omitempty"`
	TopK        *int      `json:"top_k,omitempty"`
	StopSequences []string `json:"stop_sequences,omitempty"`

	// Metadata
	Metadata *antMetadata `json:"metadata,omitempty"`
}

// antMetadata holds request metadata.
type antMetadata struct {
	UserID string `json:"user_id,omitempty"`
}

// antMessage represents a message in the Anthropic format.
type antMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// antTextContent is a text content block.
type antTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// antImageContent is an image content block.
type antImageContent struct {
	Type  string        `json:"type"`
	Source *antImageSrc `json:"source"`
}

// antImageSrc is the image source. Supports both "base64" and "url" types.
type antImageSrc struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

// antToolUseContent is a tool_use content block in an assistant message.
type antToolUseContent struct {
	Type  string      `json:"type"`
	ID    string      `json:"id"`
	Name  string      `json:"name"`
	Input interface{} `json:"input"`
}

// antToolResultContent is a tool_result content block in a user message.
type antToolResultContent struct {
	Type      string      `json:"type"`
	ToolUseID string      `json:"tool_use_id"`
	Content   interface{} `json:"content"`
	IsError   bool        `json:"is_error,omitempty"`
}

// antThinkingContent is a thinking content block.
type antThinkingContent struct {
	Type      string `json:"type"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature,omitempty"`
}

// antTool represents a tool definition in the request.
type antTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
	CacheControl *antCacheControl `json:"cache_control,omitempty"`
}

// antCacheControl is the cache control directive.
type antCacheControl struct {
	Type string `json:"type"`
}

// antSystemBlock is a system prompt content block.
type antSystemBlock struct {
	Type         string           `json:"type"`
	Text         string           `json:"text"`
	CacheControl *antCacheControl `json:"cache_control,omitempty"`
}

// antResponse is the response from the Messages API.
type antResponse struct {
	ID           string           `json:"id"`
	Type         string           `json:"type"`
	Role         string           `json:"role"`
	Content      []antContentBlock `json:"content"`
	Model        string           `json:"model"`
	StopReason   *string          `json:"stop_reason,omitempty"`
	StopSequence *string          `json:"stop_sequence,omitempty"`
	Usage        antUsage         `json:"usage"`
}

// antContentBlock is a generic content block in the response.
type antContentBlock struct {
	Type  string      `json:"type"`
	Text  string      `json:"text,omitempty"`
	ID    string      `json:"id,omitempty"`
	Name  string      `json:"name,omitempty"`
	Input interface{} `json:"input,omitempty"`

	// For thinking blocks
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
}

// antUsage reports token consumption.
type antUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`

	// Cache-related fields
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// antErrorResponse wraps an API error response.
type antErrorResponse struct {
	Type  string        `json:"type"`
	Error antErrorDetail `json:"error"`
}

// antErrorDetail contains details about an API error.
type antErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// --- Streaming types ---

// antStreamEvent is a generic streaming event wrapper.
type antStreamEvent struct {
	Type         string          `json:"type"`
	Message      *antResponse    `json:"message,omitempty"`
	Index        int             `json:"index,omitempty"`
	ContentBlock *antContentBlock `json:"content_block,omitempty"`
	Delta        json.RawMessage `json:"delta,omitempty"`
	Usage        *antUsage       `json:"usage,omitempty"`
}

// antTextDelta is a text content delta.
type antTextDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// antToolUseDelta is a tool use input delta.
type antToolUseDelta struct {
	Type          string `json:"type"`
	PartialJSON   string `json:"partial_json"`
}

// antThinkingDelta is a thinking content delta.
type antThinkingDelta struct {
	Type     string `json:"type"`
	Thinking string `json:"thinking"`
}

// antMessageDelta is the message delta event payload.
type antMessageDeltaBody struct {
	Delta       antMessageDeltaDelta `json:"delta"`
	Usage       *antUsage            `json:"usage,omitempty"`
}

// antMessageDeltaDelta holds the delta fields.
type antMessageDeltaDelta struct {
	StopReason   *string `json:"stop_reason,omitempty"`
	StopSequence *string `json:"stop_sequence,omitempty"`
}

// ---------------------------------------------------------------------------
// Known Models
// ---------------------------------------------------------------------------

// knownModels is the static list of well-known Anthropic models.
var knownModels = []provider.ModelInfo{
	{
		ID:          "claude-sonnet-4-20250514",
		Name:        "Claude Sonnet 4",
		Description: "Latest Claude Sonnet with enhanced capabilities",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 16384, ContextWindow: 200000,
		},
	},
	{
		ID:          "claude-opus-4-20250514",
		Name:        "Claude Opus 4",
		Description: "Most capable Claude model for complex tasks",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 16384, ContextWindow: 200000,
		},
	},
	{
		ID:          "claude-3-5-sonnet-20241022",
		Name:        "Claude 3.5 Sonnet",
		Description: "Claude 3.5 Sonnet with improved coding and analysis",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 8192, ContextWindow: 200000,
		},
	},
	{
		ID:          "claude-3-5-haiku-20241022",
		Name:        "Claude 3.5 Haiku",
		Description: "Fast and affordable Claude model",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 8192, ContextWindow: 200000,
		},
	},
	{
		ID:          "claude-3-opus-20240229",
		Name:        "Claude 3 Opus",
		Description: "Most powerful Claude 3 model for complex tasks",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 200000,
		},
	},
	{
		ID:          "claude-3-sonnet-20240229",
		Name:        "Claude 3 Sonnet",
		Description: "Balanced Claude 3 model for most tasks",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 200000,
		},
	},
	{
		ID:          "claude-3-haiku-20240307",
		Name:        "Claude 3 Haiku",
		Description: "Fastest and most compact Claude 3 model",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 200000,
		},
	},
}

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

// Provider implements the provider.Provider interface for Anthropic.
// It communicates with the Anthropic Messages API and supports
// both Generate and Stream operations.
//
// A Provider is safe for concurrent use by multiple goroutines.
type Provider struct {
	apiKey       string
	baseURL      string
	defaultModel string
	httpClient   *http.Client
}

// NewProvider creates a new Anthropic provider from the given configuration.
func NewProvider(cfg config.ProviderConfig) (*Provider, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		return nil, fmt.Errorf("anthropic: api_key is required")
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

// Factory is a provider.ProviderFactory that creates a new Anthropic provider.
// Use this with the provider registry:
//
//	registry.Register("anthropic", anthropic.Factory, cfg)
var Factory provider.ProviderFactory = func(cfg config.ProviderConfig) (provider.Provider, error) {
	return NewProvider(cfg)
}

// Compile-time interface check.
var _ provider.Provider = (*Provider)(nil)

// ---------------------------------------------------------------------------
// Interface Methods
// ---------------------------------------------------------------------------

// Name returns the provider identifier "anthropic".
func (p *Provider) Name() string {
	return providerName
}

// Models returns the list of well-known Anthropic models.
func (p *Provider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	result := make([]provider.ModelInfo, len(knownModels))
	copy(result, knownModels)
	return result, nil
}

// Generate sends a non-streaming completion request to the Anthropic Messages API.
func (p *Provider) Generate(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResult, error) {
	model := p.resolveModel(req.Model)
	if len(req.Messages) == 0 {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("at least one message is required"))
	}

	antReq, err := p.buildRequest(model, req)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to build request: %w", err))
	}

	bodyBytes, err := json.Marshal(antReq)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to marshal request: %w", err))
	}

	url := p.baseURL + messagesPath
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

	var antResp antResponse
	if err := json.NewDecoder(resp.Body).Decode(&antResp); err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to decode response: %w", err))
	}

	result, err := p.convertResponse(&antResp)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model, err)
	}

	return result, nil
}

// Stream sends a streaming completion request to the Anthropic API and returns
// a channel of StreamEvent values.
func (p *Provider) Stream(ctx context.Context, req provider.GenerateRequest) (<-chan provider.StreamEvent, error) {
	model := p.resolveModel(req.Model)
	if len(req.Messages) == 0 {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("at least one message is required"))
	}

	antReq, err := p.buildRequest(model, req)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to build request: %w", err))
	}
	antReq.Stream = true

	bodyBytes, err := json.Marshal(antReq)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to marshal request: %w", err))
	}

	url := p.baseURL + messagesPath
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
	case strings.Contains(lower, "opus"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 200000,
		}
	case strings.Contains(lower, "sonnet"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 8192, ContextWindow: 200000,
		}
	case strings.Contains(lower, "haiku"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 8192, ContextWindow: 200000,
		}
	}

	// Sensible default for unknown models
	return provider.ModelCapabilities{
		Streaming: true, ToolCalling: true, Vision: true, Audio: false,
		JSONMode: true, Seed: false, MaxTokens: 8192, ContextWindow: 200000,
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
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", apiVersionHeader)
}

// buildRequest constructs an Anthropic API request from an Orchestra request.
func (p *Provider) buildRequest(model string, req provider.GenerateRequest) (*antRequest, error) {
	systemJSON, antMsgs, err := convertMessages(req.Messages)
	if err != nil {
		return nil, err
	}

	antReq := &antRequest{
		Model:     model,
		MaxTokens: 4096, // Default max tokens
		System:    systemJSON,
		Messages:  antMsgs,
	}

	// Tools
	if len(req.Tools) > 0 {
		antReq.Tools = convertTools(req.Tools)
	}

	// Options
	opts := req.Options
	if opts.Temperature != nil {
		antReq.Temperature = opts.Temperature
	}
	if opts.TopP != nil {
		antReq.TopP = opts.TopP
	}
	if opts.MaxTokens != nil {
		antReq.MaxTokens = *opts.MaxTokens
	}
	if len(opts.StopSequences) > 0 {
		antReq.StopSequences = opts.StopSequences
	}

	// Extra provider-specific options
	if opts.Extra != nil {
		if v, ok := asInt(opts.Extra["top_k"]); ok {
			antReq.TopK = &v
		}
		if v, ok := asString(opts.Extra["user_id"]); ok {
			antReq.Metadata = &antMetadata{UserID: v}
		}
		// Allow max_tokens override even when Options.MaxTokens is nil
		if v, ok := asInt(opts.Extra["max_tokens"]); ok && opts.MaxTokens == nil {
			antReq.MaxTokens = v
		}
	}

	return antReq, nil
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

	var (
		currentEventType string
		toolUseAccum     = newToolUseAccumulator()
		usage            *provider.TokenUsage
	)

	for scanner.Scan() {
		if ctx.Err() != nil {
			sendEvent(ctx, ch, provider.StreamEvent{
				Type:  provider.StreamEventError,
				Error: ctx.Err(),
			})
			return
		}

		line := scanner.Text()

		// Skip empty lines
		if line == "" {
			continue
		}

		// Parse event type
		if strings.HasPrefix(line, sseEventPrefix) {
			currentEventType = strings.TrimPrefix(line, sseEventPrefix)
			continue
		}

		// Parse data lines
		if !strings.HasPrefix(line, sseDataPrefix) {
			continue
		}

		data := strings.TrimPrefix(line, sseDataPrefix)
		if data == "" {
			continue
		}

		// Handle each event type
		switch currentEventType {
		case "message_start":
			// Contains the initial message object
			var evt antStreamEvent
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				sendEvent(ctx, ch, provider.StreamEvent{
					Type:  provider.StreamEventError,
					Error: fmt.Errorf("failed to parse message_start: %w", err),
				})
				return
			}
			if evt.Message != nil && evt.Message.Usage.InputTokens > 0 {
				usage = &provider.TokenUsage{
					PromptTokens: evt.Message.Usage.InputTokens,
				}
			}

		case "content_block_start":
			var evt antStreamEvent
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				if evt.ContentBlock != nil && evt.ContentBlock.Type == "tool_use" {
					toolUseAccum.startBlock(evt.Index, evt.ContentBlock.ID, evt.ContentBlock.Name)
				}
			}

		case "content_block_delta":
			var raw map[string]json.RawMessage
			if err := json.Unmarshal([]byte(data), &raw); err != nil {
				continue
			}

			deltaBytes, ok := raw["delta"]
			if !ok {
				continue
			}

			// Peek at delta type
			var deltaType struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(deltaBytes, &deltaType); err != nil {
				continue
			}

			switch deltaType.Type {
			case "text_delta":
				var td antTextDelta
				if err := json.Unmarshal(deltaBytes, &td); err == nil && td.Text != "" {
					if !sendEvent(ctx, ch, provider.StreamEvent{
						Type:  provider.StreamEventChunk,
						Chunk: td.Text,
					}) {
						return
					}
				}

			case "input_json_delta":
				var td antToolUseDelta
				if err := json.Unmarshal(deltaBytes, &td); err == nil {
					idx := 0
					if rawIdx, ok := raw["index"]; ok {
						json.Unmarshal(rawIdx, &idx)
					}
					toolUseAccum.appendJSON(idx, td.PartialJSON)
				}

			case "thinking_delta":
				// Extended thinking delta — not emitted as text chunks
				// but tracked internally if needed
			}

		case "content_block_stop":
			// A content block has finished — tool use blocks are complete

		case "message_delta":
			var evt antMessageDeltaBody
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				if evt.Delta.StopReason != nil {
					fr := mapStopReason(*evt.Delta.StopReason)
					if fr == provider.FinishReasonToolCall {
						// Emit accumulated tool calls
						for _, tc := range toolUseAccum.collect() {
							if !sendEvent(ctx, ch, provider.StreamEvent{
								Type:     provider.StreamEventToolCall,
								ToolCall: &tc,
							}) {
								return
							}
						}
					}
				}
				if evt.Usage != nil {
					if usage == nil {
						usage = &provider.TokenUsage{}
					}
					usage.CompletionTokens = evt.Usage.OutputTokens
					usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
				}
			}

		case "message_stop":
			// Stream is complete

		case "ping":
			// Keepalive, ignore

		case "error":
			var errResp antErrorResponse
			if err := json.Unmarshal([]byte(data), &errResp); err == nil {
				sendEvent(ctx, ch, provider.StreamEvent{
					Type:  provider.StreamEventError,
					Error: fmt.Errorf("%s: %s", errResp.Error.Type, errResp.Error.Message),
				})
				return
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
// Tool Use Accumulator (streaming)
// ---------------------------------------------------------------------------

// toolUseAccumulator assembles incremental tool use input from streaming events.
type toolUseAccumulator struct {
	blocks map[int]*toolUseBlock
}

type toolUseBlock struct {
	id   string
	name string
	json strings.Builder
}

func newToolUseAccumulator() *toolUseAccumulator {
	return &toolUseAccumulator{
		blocks: make(map[int]*toolUseBlock),
	}
}

func (a *toolUseAccumulator) startBlock(index int, id, name string) {
	a.blocks[index] = &toolUseBlock{id: id, name: name}
}

func (a *toolUseAccumulator) appendJSON(index int, partial string) {
	if b, ok := a.blocks[index]; ok {
		b.json.WriteString(partial)
	}
}

func (a *toolUseAccumulator) collect() []message.ToolCall {
	if len(a.blocks) == 0 {
		return nil
	}
	result := make([]message.ToolCall, 0, len(a.blocks))
	for i := 0; i < len(a.blocks); i++ {
		if b, ok := a.blocks[i]; ok {
			result = append(result, message.ToolCall{
				ID:   b.id,
				Type: "function",
				Function: message.ToolCallFunction{
					Name:      b.name,
					Arguments: b.json.String(),
				},
			})
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Conversion: Orchestra → Anthropic
// ---------------------------------------------------------------------------

// convertMessages converts Orchestra messages to Anthropic format.
// Returns the system prompt as json.RawMessage (if any), and the messages array.
// Anthropic requires system messages to be a top-level field, not in the messages array.
func convertMessages(msgs []message.Message) (json.RawMessage, []antMessage, error) {
	var systemParts []antSystemBlock
	var antMsgs []antMessage

	for _, msg := range msgs {
		switch msg.Role {
		case message.RoleSystem:
			// Extract system message content
			text := msg.Text()
			if text != "" {
				systemParts = append(systemParts, antSystemBlock{
					Type: "text",
					Text: text,
				})
			}

		case message.RoleUser:
			content, err := convertUserContent(msg)
			if err != nil {
				return nil, nil, err
			}
			antMsgs = append(antMsgs, antMessage{
				Role:    "user",
				Content: content,
			})

		case message.RoleAssistant:
			content, err := convertAssistantContent(msg)
			if err != nil {
				return nil, nil, err
			}
			antMsgs = append(antMsgs, antMessage{
				Role:    "assistant",
				Content: content,
			})

		case message.RoleTool, message.RoleFunction:
			// Tool results must be in user messages in Anthropic's API.
			// We wrap them as a user message with tool_result content blocks.
			if msg.ToolResult != nil {
				content, err := json.Marshal([]antToolResultContent{
					{
						Type:      "tool_result",
						ToolUseID: msg.ToolResult.ToolCallID,
						Content:   msg.ToolResult.Content,
						IsError:   msg.ToolResult.IsError,
					},
				})
				if err != nil {
					return nil, nil, fmt.Errorf("failed to marshal tool result: %w", err)
				}
				antMsgs = append(antMsgs, antMessage{
					Role:    "user",
					Content: content,
				})
			}
		}
	}

	var systemJSON json.RawMessage
	if len(systemParts) > 0 {
		if len(systemParts) == 1 {
			// Single system message can be a plain string
			systemJSON, _ = json.Marshal(systemParts[0].Text)
		} else {
			systemJSON, _ = json.Marshal(systemParts)
		}
	}

	return systemJSON, antMsgs, nil
}

// convertUserContent converts a user message's content to Anthropic format.
func convertUserContent(msg message.Message) (json.RawMessage, error) {
	if len(msg.Content) == 0 && len(msg.ToolCalls) == 0 {
		return json.Marshal("")
	}

	var parts []interface{}
	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				parts = append(parts, antTextContent{Type: "text", Text: block.Text})
			}
		case "image":
			// Anthropic supports base64 images and URLs
			if len(block.FileData) > 0 {
				mime := block.MimeType
				if mime == "" {
					mime = "image/png"
				}
				parts = append(parts, antImageContent{
					Type: "image",
					Source: &antImageSrc{
						Type:      "base64",
						MediaType: mime,
						Data:      base64.StdEncoding.EncodeToString(block.FileData),
					},
				})
			} else if block.ImageURL != "" {
				// Check if it's a base64 data URI
				if strings.HasPrefix(block.ImageURL, "data:") {
					mime, data := parseDataURI(block.ImageURL)
					parts = append(parts, antImageContent{
						Type: "image",
						Source: &antImageSrc{
							Type:      "base64",
							MediaType: mime,
							Data:      data,
						},
					})
				} else {
					// URL-based image — uses unified antImageSrc with URL field
					parts = append(parts, antImageContent{
						Type: "image",
						Source: &antImageSrc{
							Type: "url",
							URL:  block.ImageURL,
						},
					})
				}
			}
		case "file":
			// Anthropic doesn't have a generic file type, but images work
			if len(block.FileData) > 0 && strings.HasPrefix(block.MimeType, "image/") {
				mime := block.MimeType
				if mime == "" {
					mime = "image/png"
				}
				parts = append(parts, antImageContent{
					Type: "image",
					Source: &antImageSrc{
						Type:      "base64",
						MediaType: mime,
						Data:      base64.StdEncoding.EncodeToString(block.FileData),
					},
				})
			}
		}
	}

	if len(parts) == 0 {
		return json.Marshal("")
	}

	// Single text block → plain string
	if len(parts) == 1 {
		if tc, ok := parts[0].(antTextContent); ok {
			return json.Marshal(tc.Text)
		}
	}

	return json.Marshal(parts)
}

// convertAssistantContent converts an assistant message's content to Anthropic format.
func convertAssistantContent(msg message.Message) (json.RawMessage, error) {
	var parts []interface{}

	// Add text content
	text := msg.Text()
	if text != "" {
		parts = append(parts, antTextContent{Type: "text", Text: text})
	}

	// Add tool use content blocks
	for _, tc := range msg.ToolCalls {
		var input interface{}
		if tc.Function.Arguments != "" {
			// Try to parse as JSON; if it fails, pass as-is
			var parsed interface{}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &parsed); err == nil {
				input = parsed
			} else {
				input = map[string]interface{}{}
			}
		} else {
			input = map[string]interface{}{}
		}

		parts = append(parts, antToolUseContent{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}

	if len(parts) == 0 {
		return json.Marshal("")
	}

	// Single text block → plain string
	if len(parts) == 1 {
		if tc, ok := parts[0].(antTextContent); ok {
			return json.Marshal(tc.Text)
		}
	}

	return json.Marshal(parts)
}

// convertTools converts Orchestra tool definitions to Anthropic tool format.
func convertTools(tools []provider.ToolDefinition) []antTool {
	result := make([]antTool, 0, len(tools))
	for _, tool := range tools {
		params := tool.Function.Parameters
		if params == nil {
			params = map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}
		}
		result = append(result, antTool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: params,
		})
	}
	return result
}

// ---------------------------------------------------------------------------
// Conversion: Anthropic → Orchestra
// ---------------------------------------------------------------------------

// convertResponse converts an Anthropic Messages response to a GenerateResult.
func (p *Provider) convertResponse(resp *antResponse) (*provider.GenerateResult, error) {
	msg := convertAntContent(resp.Content)

	finishReason := provider.FinishReasonStop
	if resp.StopReason != nil {
		finishReason = mapStopReason(*resp.StopReason)
	}

	usage := provider.TokenUsage{
		PromptTokens:     resp.Usage.InputTokens,
		CompletionTokens: resp.Usage.OutputTokens,
		TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
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

	// Add cache-related usage metadata
	if resp.Usage.CacheCreationInputTokens > 0 || resp.Usage.CacheReadInputTokens > 0 {
		result.Metadata["cache_creation_input_tokens"] = resp.Usage.CacheCreationInputTokens
		result.Metadata["cache_read_input_tokens"] = resp.Usage.CacheReadInputTokens
	}

	if resp.StopSequence != nil {
		result.Metadata["stop_sequence"] = *resp.StopSequence
	}

	return result, nil
}

// convertAntContent converts Anthropic response content blocks to an Orchestra Message.
func convertAntContent(blocks []antContentBlock) message.Message {
	msg := message.Message{
		Role: message.RoleAssistant,
	}

	var textParts []string
	var toolCalls []message.ToolCall

	for _, block := range blocks {
		switch block.Type {
		case "text":
			if block.Text != "" {
				textParts = append(textParts, block.Text)
			}
		case "tool_use":
			args := "{}"
			if block.Input != nil {
				if b, err := json.Marshal(block.Input); err == nil {
					args = string(b)
				}
			}
			toolCalls = append(toolCalls, message.ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: message.ToolCallFunction{
					Name:      block.Name,
					Arguments: args,
				},
			})
		case "thinking":
			// Store thinking content in metadata if needed
			// For now, we skip it in the main message
		}
	}

	if len(textParts) > 0 {
		msg.Content = []message.ContentBlock{
			message.TextContentBlock(strings.Join(textParts, "")),
		}
	}

	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
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

	var errResp antErrorResponse
	if err := json.Unmarshal(body, &errResp); err != nil {
		return provider.NewProviderErrorWithCode(providerName, model, "http_error", resp.StatusCode,
			fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body)))
	}

	code := errResp.Error.Type
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

// mapStopReason maps an Anthropic stop reason to a FinishReason.
func mapStopReason(reason string) provider.FinishReason {
	switch reason {
	case "end_turn":
		return provider.FinishReasonStop
	case "max_tokens":
		return provider.FinishReasonLength
	case "stop_sequence":
		return provider.FinishReasonStop
	case "tool_use":
		return provider.FinishReasonToolCall
	default:
		return provider.FinishReasonStop
	}
}

// parseDataURI parses a data URI into its MIME type and base64 data.
func parseDataURI(dataURI string) (mimeType, data string) {
	// Format: data:<mime>;base64,<data>
	if !strings.HasPrefix(dataURI, "data:") {
		return "", dataURI
	}
	rest := dataURI[5:]
	idx := strings.Index(rest, ",")
	if idx < 0 {
		return "", rest
	}
	meta := rest[:idx]
	data = rest[idx+1:]

	// Extract MIME type
	parts := strings.SplitN(meta, ";", 2)
	mimeType = parts[0]
	if mimeType == "" {
		mimeType = "image/png"
	}

	return mimeType, data
}

// ---------------------------------------------------------------------------
// Type Helpers
// ---------------------------------------------------------------------------

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

// asString extracts a string from an any value.
func asString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}
