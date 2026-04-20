// Package cohere implements the Cohere Chat API (v2) provider for Orchestra.
//
// It supports Generate and Stream operations, tool use with Cohere's
// native tool calling, connectors, citations, and the full range of
// Cohere models.
//
// Register with the Orchestra provider registry:
//
//	registry.Register("cohere", cohere.Factory, cfg)
package cohere

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
	defaultBaseURL      = "https://api.cohere.com/v2"
	providerName        = "cohere"
	chatPath            = "/chat"
	defaultModel        = "command-r-plus"
	httpTimeout         = 10 * time.Minute
	maxIdleConns        = 100
	maxIdleConnsPerHost = 100
	idleConnTimeout     = 90 * time.Second
)

// ---------------------------------------------------------------------------
// Cohere API Types — Request
// ---------------------------------------------------------------------------

// cohRequest is the request body for the Cohere v2 Chat API.
type cohRequest struct {
	Model          string          `json:"model"`
	Messages       []cohMessage    `json:"messages"`
	Streaming      bool            `json:"stream,omitempty"`
	Temperature    *float64        `json:"temperature,omitempty"`
	TopP           *float64        `json:"top_p,omitempty"`
	TopK           *int            `json:"top_k,omitempty"`
	MaxTokens      *int            `json:"max_tokens,omitempty"`
	StopSequences  []string        `json:"stop_sequences,omitempty"`
	Seed           *int64          `json:"seed,omitempty"`
	FrequencyPenalty *float64      `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64      `json:"presence_penalty,omitempty"`
	Tools          []cohTool       `json:"tools,omitempty"`
	ToolResults    []cohToolResult `json:"tool_results,omitempty"`
	ResponseFormat *cohRespFormat  `json:"response_format,omitempty"`
	SafetyMode     string          `json:"safety_mode,omitempty"`
	Connectors     []interface{}   `json:"connectors,omitempty"`
}

// cohRespFormat specifies the response format.
type cohRespFormat struct {
	Type string `json:"type"`
}

// cohMessage represents a message in the Cohere v2 chat format.
// Content uses json.RawMessage so it can be either a string or an
// array of content parts.
type cohMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"`
	ToolCalls  []cohToolCall   `json:"tool_calls,omitempty"`
	ToolPlan   *string         `json:"tool_plan,omitempty"`
}

// cohContentPart represents a single content part in a multi-part message.
type cohContentPart struct {
	Type     string       `json:"type"`
	Text     string       `json:"text,omitempty"`
	ImageURL *cohImgURL   `json:"image_url,omitempty"`
}

// cohImgURL represents an image URL.
type cohImgURL struct {
	URL string `json:"url"`
}

// cohTool represents a tool definition in the request.
type cohTool struct {
	Type     string      `json:"type"`
	Function cohFuncDef  `json:"function"`
}

// cohFuncDef describes a function the model can call.
type cohFuncDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// cohToolCall represents a tool call in an assistant message.
type cohToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function cohFunction  `json:"function"`
}

// cohFunction holds the function name and arguments.
type cohFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// cohToolResult represents a tool result to send back to the model.
type cohToolResult struct {
	Call     cohToolCall `json:"call"`
	Outputs  []map[string]interface{} `json:"outputs"`
}

// ---------------------------------------------------------------------------
// Cohere API Types — Response
// ---------------------------------------------------------------------------

// cohResponse is the response from the Chat API.
type cohResponse struct {
	ID           string         `json:"id"`
	Model        string         `json:"model"`
	Message      cohMessage     `json:"message"`
	FinishReason *string        `json:"finish_reason,omitempty"`
	Usage        cohUsage       `json:"usage,omitempty"`
	Meta         *cohMeta       `json:"meta,omitempty"`
	Citations    []cohCitation  `json:"citations,omitempty"`
}

// cohUsage reports token consumption.
type cohUsage struct {
	BilledUnits cohBilledUnits `json:"billed_units,omitempty"`
	Tokens      cohTokens      `json:"tokens,omitempty"`
}

// cohBilledUnits reports billed token counts.
type cohBilledUnits struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
}

// cohTokens reports actual token counts.
type cohTokens struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
}

// cohMeta contains metadata about the response.
type cohMeta struct {
	APIVersion struct {
		Version string `json:"version"`
	} `json:"api_version"`
}

// cohCitation represents a citation in the response.
type cohCitation struct {
	Start       int      `json:"start"`
	End         int      `json:"end"`
	Text        string   `json:"text"`
	DocumentIDs []string `json:"document_ids"`
	Type        string   `json:"type,omitempty"`
}

// cohErrorResponse wraps an API error response.
type cohErrorResponse struct {
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
	Error      string `json:"error"`
}

// --- Streaming types ---

// cohStreamEvent is the envelope for a streaming SSE event.
type cohStreamEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"-"`
}

// cohStreamContentDelta is a text delta event.
type cohStreamContentDelta struct {
	Type string `json:"type"`
	Delta struct {
		MessageType string `json:"message_type"`
		Content     string `json:"content"`
	} `json:"delta"`
}

// cohStreamToolCallDelta is a tool call event during streaming.
type cohStreamToolCallDelta struct {
	Type string `json:"type"`
	Delta struct {
		MessageType string `json:"message_type"`
		ToolPlan    string `json:"tool_plan,omitempty"`
		ToolCalls   []cohToolCall `json:"tool_calls,omitempty"`
	} `json:"delta"`
}

// cohStreamMessageEnd is the final event with usage.
type cohStreamMessageEnd struct {
	Type    string         `json:"type"`
	Delta   cohEndDelta    `json:"delta"`
	Usage   *cohUsage      `json:"usage,omitempty"`
}

// cohEndDelta holds the finish reason at stream end.
type cohEndDelta struct {
	FinishReason *string `json:"finish_reason,omitempty"`
	MessageType  string  `json:"message_type"`
}

// cohStreamMessageStart is the start event.
type cohStreamMessageStart struct {
	Type  string `json:"type"`
	Delta struct {
		MessageType string `json:"message_type"`
	} `json:"delta"`
}

// ---------------------------------------------------------------------------
// Known Models
// ---------------------------------------------------------------------------

// knownModels is the static list of well-known Cohere models.
var knownModels = []provider.ModelInfo{
	{
		ID:          "command-r-plus",
		Name:        "Command R+",
		Description: "Most capable Cohere model for complex tasks",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 128000,
		},
	},
	{
		ID:          "command-r-plus-08-2024",
		Name:        "Command R+ (August 2024)",
		Description: "Command R+ snapshot from August 2024",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 128000,
		},
	},
	{
		ID:          "command-r",
		Name:        "Command R",
		Description: "Efficient model for RAG and tool use",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 128000,
		},
	},
	{
		ID:          "command-r-08-2024",
		Name:        "Command R (August 2024)",
		Description: "Command R snapshot from August 2024",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 128000,
		},
	},
	{
		ID:          "command",
		Name:        "Command",
		Description: "Legacy Command model",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: false, Vision: false, Audio: false,
			JSONMode: false, Seed: false, MaxTokens: 4096, ContextWindow: 4096,
		},
		Deprecated: true,
	},
	{
		ID:          "command-light",
		Name:        "Command Light",
		Description: "Lightweight Command model for fast responses",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: false, Vision: false, Audio: false,
			JSONMode: false, Seed: false, MaxTokens: 4096, ContextWindow: 4096,
		},
		Deprecated: true,
	},
	{
		ID:          "c4ai-aya-expanse-8b",
		Name:        "Aya Expanse 8B",
		Description: "Multilingual model supporting 23 languages",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 8192,
		},
	},
	{
		ID:          "c4ai-aya-expanse-32b",
		Name:        "Aya Expanse 32B",
		Description: "Large multilingual model supporting 23 languages",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 128000,
		},
	},
	{
		ID:          "embed-v4",
		Name:        "Embed v4",
		Description: "Embedding model (not for generation)",
		Capabilities: provider.ModelCapabilities{
			Streaming: false, ToolCalling: false, Vision: false, Audio: false,
			JSONMode: false, Seed: false, MaxTokens: 0, ContextWindow: 128000,
		},
		Deprecated: true,
	},
	{
		ID:          "rerank-v3",
		Name:        "Rerank v3",
		Description: "Reranking model (not for generation)",
		Capabilities: provider.ModelCapabilities{
			Streaming: false, ToolCalling: false, Vision: false, Audio: false,
			JSONMode: false, Seed: false, MaxTokens: 0, ContextWindow: 4096,
		},
		Deprecated: true,
	},
}

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

// Provider implements the provider.Provider interface for Cohere.
// It communicates with the Cohere v2 Chat API and supports both Generate
// and Stream operations.
//
// A Provider is safe for concurrent use by multiple goroutines.
type Provider struct {
	apiKey       string
	baseURL      string
	defaultModel string
	httpClient   *http.Client
}

// NewProvider creates a new Cohere provider from the given configuration.
func NewProvider(cfg config.ProviderConfig) (*Provider, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		return nil, fmt.Errorf("cohere: api_key is required")
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

// Factory is a provider.ProviderFactory that creates a new Cohere provider.
// Use this with the provider registry:
//
//	registry.Register("cohere", cohere.Factory, cfg)
var Factory provider.ProviderFactory = func(cfg config.ProviderConfig) (provider.Provider, error) {
	return NewProvider(cfg)
}

// Compile-time interface check.
var _ provider.Provider = (*Provider)(nil)

// ---------------------------------------------------------------------------
// Interface Methods
// ---------------------------------------------------------------------------

// Name returns the provider identifier "cohere".
func (p *Provider) Name() string {
	return providerName
}

// Models returns the list of well-known Cohere models.
func (p *Provider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	result := make([]provider.ModelInfo, len(knownModels))
	copy(result, knownModels)
	return result, nil
}

// Generate sends a non-streaming completion request to the Cohere API.
func (p *Provider) Generate(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResult, error) {
	model := p.resolveModel(req.Model)
	if len(req.Messages) == 0 {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("at least one message is required"))
	}

	cohReq, err := p.buildRequest(model, req, false)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to build request: %w", err))
	}

	bodyBytes, err := json.Marshal(cohReq)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to marshal request: %w", err))
	}

	url := p.baseURL + chatPath
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

	var cohResp cohResponse
	if err := json.NewDecoder(resp.Body).Decode(&cohResp); err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to decode response: %w", err))
	}

	result, err := p.convertResponse(&cohResp)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model, err)
	}

	return result, nil
}

// Stream sends a streaming completion request to the Cohere API and returns
// a channel of StreamEvent values. Cohere uses SSE streaming with typed events.
func (p *Provider) Stream(ctx context.Context, req provider.GenerateRequest) (<-chan provider.StreamEvent, error) {
	model := p.resolveModel(req.Model)
	if len(req.Messages) == 0 {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("at least one message is required"))
	}

	cohReq, err := p.buildRequest(model, req, true)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to build request: %w", err))
	}

	bodyBytes, err := json.Marshal(cohReq)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to marshal request: %w", err))
	}

	url := p.baseURL + chatPath
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
	case strings.Contains(lower, "command-r-plus"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 128000,
		}
	case strings.Contains(lower, "command-r"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 128000,
		}
	case strings.Contains(lower, "aya-expanse-32b"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 128000,
		}
	case strings.Contains(lower, "aya-expanse"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 8192,
		}
	case strings.HasPrefix(lower, "command"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: false, Vision: false, Audio: false,
			JSONMode: false, Seed: false, MaxTokens: 4096, ContextWindow: 4096,
		}
	}

	// Sensible default for unknown models
	return provider.ModelCapabilities{
		Streaming: true, ToolCalling: true, Vision: false, Audio: false,
		JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 128000,
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

// buildRequest constructs a Cohere v2 API request from an Orchestra request.
func (p *Provider) buildRequest(model string, req provider.GenerateRequest, stream bool) (*cohRequest, error) {
	cohMsgs, toolResults, err := convertMessages(req.Messages)
	if err != nil {
		return nil, err
	}

	cohReq := &cohRequest{
		Model:     model,
		Messages:  cohMsgs,
		Streaming: stream,
	}

	// Tool results (from prior tool calls)
	if len(toolResults) > 0 {
		cohReq.ToolResults = toolResults
	}

	// Tools
	if len(req.Tools) > 0 {
		cohReq.Tools = convertTools(req.Tools)
	}

	// Options
	opts := req.Options
	if opts.Temperature != nil {
		cohReq.Temperature = opts.Temperature
	}
	if opts.TopP != nil {
		cohReq.TopP = opts.TopP
	}
	if opts.MaxTokens != nil {
		cohReq.MaxTokens = opts.MaxTokens
	}
	if len(opts.StopSequences) > 0 {
		cohReq.StopSequences = opts.StopSequences
	}
	if opts.Seed != nil {
		cohReq.Seed = opts.Seed
	}

	// Response format
	if opts.ResponseFormat != nil {
		switch opts.ResponseFormat.Type {
		case "json_object":
			cohReq.ResponseFormat = &cohRespFormat{Type: "json_object"}
		case "text":
			cohReq.ResponseFormat = &cohRespFormat{Type: "text"}
		}
	}

	// Extra provider-specific options
	if opts.Extra != nil {
		if v, ok := asFloat64(opts.Extra["frequency_penalty"]); ok {
			cohReq.FrequencyPenalty = &v
		}
		if v, ok := asFloat64(opts.Extra["presence_penalty"]); ok {
			cohReq.PresencePenalty = &v
		}
		if v, ok := asInt(opts.Extra["top_k"]); ok {
			cohReq.TopK = &v
		}
		if v, ok := asString(opts.Extra["safety_mode"]); ok {
			cohReq.SafetyMode = v
		}
		// Connectors support — allows web search, etc.
		if v, ok := opts.Extra["connectors"]; ok {
			if conns, ok := v.([]interface{}); ok {
				cohReq.Connectors = conns
			}
		}
	}

	return cohReq, nil
}

// ---------------------------------------------------------------------------
// Streaming
// ---------------------------------------------------------------------------

// streamEvents reads SSE events from the response body and sends them on ch.
// Cohere v2 uses typed SSE events with the format:
//
//	event: <type>
//	data: <json>
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
		usage            *provider.TokenUsage
	)

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

		// Skip empty lines
		if line == "" {
			continue
		}

		// Parse event type line
		if strings.HasPrefix(line, "event: ") {
			currentEventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		// Parse data lines
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "" {
			continue
		}

		// Handle each event type
		switch currentEventType {
		case "content-delta":
			var evt cohStreamContentDelta
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				if evt.Delta.Content != "" {
					if !sendEvent(ctx, ch, provider.StreamEvent{
						Type:  provider.StreamEventChunk,
						Chunk: evt.Delta.Content,
					}) {
						return
					}
				}
			}

		case "tool-plan":
			// Tool plan is the model "thinking" about which tools to use.
			// We don't emit this as chunks, but it could be tracked.
			var evt cohStreamToolCallDelta
			_ = json.Unmarshal([]byte(data), &evt)

		case "tool-call-delta":
			// Tool call delta — incremental tool call arguments
			var evt cohStreamToolCallDelta
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				for _, tc := range evt.Delta.ToolCalls {
					toolCall := message.ToolCall{
						ID:   tc.ID,
						Type: tc.Type,
						Function: message.ToolCallFunction{
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						},
					}
					if !sendEvent(ctx, ch, provider.StreamEvent{
						Type:     provider.StreamEventToolCall,
						ToolCall: &toolCall,
					}) {
						return
					}
				}
			}

		case "message-end":
			var evt cohStreamMessageEnd
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				if evt.Usage != nil {
					inputTokens := 0
					outputTokens := 0
					if evt.Usage.Tokens.InputTokens > 0 {
						inputTokens = evt.Usage.Tokens.InputTokens
						outputTokens = evt.Usage.Tokens.OutputTokens
					} else {
						inputTokens = evt.Usage.BilledUnits.InputTokens
						outputTokens = evt.Usage.BilledUnits.OutputTokens
					}
					usage = &provider.TokenUsage{
						PromptTokens:     inputTokens,
						CompletionTokens: outputTokens,
						TotalTokens:      inputTokens + outputTokens,
					}
				}
			}

		case "error":
			var errResp cohErrorResponse
			if err := json.Unmarshal([]byte(data), &errResp); err == nil {
				errMsg := errResp.Error
				if errMsg == "" {
					errMsg = errResp.Body
				}
				sendEvent(ctx, ch, provider.StreamEvent{
					Type:  provider.StreamEventError,
					Error: fmt.Errorf("%s", errMsg),
				})
				return
			}

		case "message-start":
			// Stream started — informational only
		case "content-start":
			// Content started — informational only
		case "content-end":
			// Content ended — informational only
		case "tool-start":
			// Tool call started — informational only
		case "tool-end":
			// Tool call ended — informational only
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
// Conversion: Orchestra → Cohere
// ---------------------------------------------------------------------------

// convertMessages converts Orchestra messages to Cohere v2 format.
// Returns the messages array, any tool results that need to be sent
// separately, and any error.
func convertMessages(msgs []message.Message) ([]cohMessage, []cohToolResult, error) {
	var cohMsgs []cohMessage
	var toolResults []cohToolResult

	for _, msg := range msgs {
		switch msg.Role {
		case message.RoleSystem:
			cohMsgs = append(cohMsgs, cohMessage{
				Role:    "system",
				Content: convertContentBlocks(msg.Content),
			})

		case message.RoleUser:
			cohMsgs = append(cohMsgs, cohMessage{
				Role:    "user",
				Content: convertContentBlocks(msg.Content),
			})

		case message.RoleAssistant:
			cohMsg := cohMessage{
				Role: "assistant",
			}

			// Set content
			text := msg.Text()
			if text != "" {
				cohMsg.Content = jsonString(text)
			}

			// Set tool calls
			if len(msg.ToolCalls) > 0 {
				cohMsg.ToolCalls = convertToolCallsOut(msg.ToolCalls)
			}

			cohMsgs = append(cohMsgs, cohMsg)

		case message.RoleTool, message.RoleFunction:
			// Cohere v2 uses tool_results at the top level alongside messages.
			// We need to find the corresponding tool call to create the result.
			if msg.ToolResult != nil {
				// Extract the function name from the tool call ID if available
				// Cohere needs the full call object
				toolResults = append(toolResults, cohToolResult{
					Call: cohToolCall{
						ID:   msg.ToolResult.ToolCallID,
						Type: "function",
					},
					Outputs: []map[string]interface{}{
						{
							"result": msg.ToolResult.Content,
						},
					},
				})
			}
		}
	}

	return cohMsgs, toolResults, nil
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
	parts := make([]cohContentPart, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if block.Text != "" {
				parts = append(parts, cohContentPart{
					Type: "text",
					Text: block.Text,
				})
			}
		case "image":
			if block.ImageURL != "" {
				parts = append(parts, cohContentPart{
					Type:     "image_url",
					ImageURL: &cohImgURL{URL: block.ImageURL},
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

// convertTools converts Orchestra tool definitions to Cohere tool format.
func convertTools(tools []provider.ToolDefinition) []cohTool {
	result := make([]cohTool, 0, len(tools))
	for _, tool := range tools {
		params := tool.Function.Parameters
		if params == nil {
			params = map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}
		}
		result = append(result, cohTool{
			Type: "function",
			Function: cohFuncDef{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  params,
			},
		})
	}
	return result
}

// convertToolCallsOut converts Orchestra tool calls to Cohere tool calls.
func convertToolCallsOut(tcs []message.ToolCall) []cohToolCall {
	result := make([]cohToolCall, 0, len(tcs))
	for _, tc := range tcs {
		result = append(result, cohToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: cohFunction{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}
	return result
}

// ---------------------------------------------------------------------------
// Conversion: Cohere → Orchestra
// ---------------------------------------------------------------------------

// convertResponse converts a Cohere response to a GenerateResult.
func (p *Provider) convertResponse(resp *cohResponse) (*provider.GenerateResult, error) {
	msg := convertCohMessage(resp.Message)

	finishReason := provider.FinishReasonStop
	if resp.FinishReason != nil {
		finishReason = mapFinishReason(*resp.FinishReason)
	}

	// Check for tool calls in the message
	if len(resp.Message.ToolCalls) > 0 {
		finishReason = provider.FinishReasonToolCall
	}

	// Build usage — prefer actual tokens over billed units
	inputTokens := resp.Usage.Tokens.InputTokens
	outputTokens := resp.Usage.Tokens.OutputTokens
	if inputTokens == 0 && outputTokens == 0 {
		inputTokens = resp.Usage.BilledUnits.InputTokens
		outputTokens = resp.Usage.BilledUnits.OutputTokens
	}
	usage := provider.TokenUsage{
		PromptTokens:     inputTokens,
		CompletionTokens: outputTokens,
		TotalTokens:      inputTokens + outputTokens,
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

	// Include citations if present
	if len(resp.Citations) > 0 {
		result.Metadata["citations"] = resp.Citations
	}

	// Include API version
	if resp.Meta != nil && resp.Meta.APIVersion.Version != "" {
		result.Metadata["api_version"] = resp.Meta.APIVersion.Version
	}

	return result, nil
}

// convertCohMessage converts a Cohere response message to an Orchestra Message.
func convertCohMessage(cohMsg cohMessage) message.Message {
	msg := message.Message{
		Role: message.Role(cohMsg.Role),
	}

	// If role is empty, default to assistant
	if msg.Role == "" {
		msg.Role = message.RoleAssistant
	}

	// Parse content
	text := extractText(cohMsg.Content)
	if text != "" {
		msg.Content = []message.ContentBlock{message.TextContentBlock(text)}
	}

	// Convert tool calls
	if len(cohMsg.ToolCalls) > 0 {
		msg.ToolCalls = make([]message.ToolCall, len(cohMsg.ToolCalls))
		for i, tc := range cohMsg.ToolCalls {
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

	// Include tool plan if present
	if cohMsg.ToolPlan != nil {
		if msg.Metadata == nil {
			msg.Metadata = make(map[string]any)
		}
		msg.Metadata["tool_plan"] = *cohMsg.ToolPlan
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

	var errResp cohErrorResponse
	if jsonErr := json.Unmarshal(body, &errResp); jsonErr != nil || errResp.Error == "" {
		return provider.NewProviderErrorWithCode(providerName, model, "http_error", resp.StatusCode,
			fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body)))
	}

	code := fmt.Sprintf("http_%d", resp.StatusCode)
	if errResp.StatusCode > 0 {
		code = fmt.Sprintf("http_%d", errResp.StatusCode)
	}

	errMsg := errResp.Error
	if errMsg == "" {
		errMsg = errResp.Body
	}
	if errMsg == "" {
		errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	return provider.NewProviderErrorWithCode(providerName, model, code, resp.StatusCode,
		fmt.Errorf("%s", errMsg))
}

// ---------------------------------------------------------------------------
// Mapping Helpers
// ---------------------------------------------------------------------------

// mapFinishReason maps a Cohere finish reason to a FinishReason.
func mapFinishReason(reason string) provider.FinishReason {
	switch reason {
	case "COMPLETE":
		return provider.FinishReasonStop
	case "MAX_TOKENS":
		return provider.FinishReasonLength
	case "STOP_SEQUENCE":
		return provider.FinishReasonStop
	case "TOOL_CALL":
		return provider.FinishReasonToolCall
	case "ERROR":
		return provider.FinishReasonError
	case "ERROR_TOXIC":
		return provider.FinishReasonContentFilter
	case "ERROR_LIMIT":
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

// ---------------------------------------------------------------------------
// Type Helpers
// ---------------------------------------------------------------------------

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

// asString extracts a string from an any value.
func asString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}
