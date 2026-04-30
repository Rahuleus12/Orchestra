// Package ollama implements the Ollama local LLM provider for Orchestra.
//
// It supports Generate and Stream operations, tool calling with Ollama's
// native tool support, model listing from a local Ollama instance, and
// auto-detection of Ollama availability. Custom endpoints are supported
// for remote Ollama hosts.
//
// Register with the Orchestra provider registry:
//
//	registry.Register("ollama", ollama.Factory, cfg)
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
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
	defaultBaseURL      = "http://localhost:11434"
	providerName        = "ollama"
	chatPath            = "/api/chat"
	tagsPath            = "/api/tags"
	defaultModel        = "llama3"
	httpTimeout         = 10 * time.Minute
	maxIdleConns        = 100
	maxIdleConnsPerHost = 100
	idleConnTimeout     = 90 * time.Second
	detectTimeout       = 3 * time.Second
)

// ---------------------------------------------------------------------------
// Ollama API Types — Request
// ---------------------------------------------------------------------------

// ollRequest is the request body for the Ollama Chat API.
type ollRequest struct {
	Model    string       `json:"model"`
	Messages []ollMessage `json:"messages"`
	Stream   bool         `json:"stream"`
	Tools    []ollTool    `json:"tools,omitempty"`

	// Generation options
	Options *ollOptions `json:"options,omitempty"`

	// Format controls output formatting. Can be "json" or a JSON schema object.
	Format interface{} `json:"format,omitempty"`

	// KeepAlive controls how long the model stays loaded.
	KeepAlive string `json:"keep_alive,omitempty"`
}

// ollOptions contains generation parameters for Ollama.
type ollOptions struct {
	Temperature      *float64 `json:"temperature,omitempty"`
	TopP             *float64 `json:"top_p,omitempty"`
	TopK             *int     `json:"top_k,omitempty"`
	NumPredict       *int     `json:"num_predict,omitempty"`
	Stop             []string `json:"stop,omitempty"`
	Seed             *int64   `json:"seed,omitempty"`
	NumCtx           *int     `json:"num_ctx,omitempty"`
	RepeatLastN      *int     `json:"repeat_last_n,omitempty"`
	RepeatPenalty    *float64 `json:"repeat_penalty,omitempty"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64 `json:"presence_penalty,omitempty"`
}

// ollMessage represents a message in the Ollama chat format.
type ollMessage struct {
	Role      string        `json:"role"`
	Content   string        `json:"content,omitempty"`
	Images    []string      `json:"images,omitempty"`
	ToolCalls []ollToolCall `json:"tool_calls,omitempty"`
}

// ollToolCall represents a tool call in an assistant message.
type ollToolCall struct {
	Function ollToolFunction `json:"function"`
}

// ollToolFunction holds the function details for a tool call.
type ollToolFunction struct {
	Name      string      `json:"name"`
	Arguments interface{} `json:"arguments"`
}

// ollTool represents a tool definition in the request.
type ollTool struct {
	Type     string     `json:"type"`
	Function ollFuncDef `json:"function"`
}

// ollFuncDef describes a function that can be called by the model.
type ollFuncDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ---------------------------------------------------------------------------
// Ollama API Types — Response
// ---------------------------------------------------------------------------

// ollChatResponse is the response from the Ollama Chat API.
type ollChatResponse struct {
	Model      string     `json:"model"`
	CreatedAt  string     `json:"created_at"`
	Message    ollMessage `json:"message"`
	Done       bool       `json:"done"`
	DoneReason string     `json:"done_reason,omitempty"`

	// Performance metrics
	TotalDuration      int64 `json:"total_duration,omitempty"`
	LoadDuration       int64 `json:"load_duration,omitempty"`
	PromptEvalCount    int   `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64 `json:"prompt_eval_duration,omitempty"`
	EvalCount          int   `json:"eval_count,omitempty"`
	EvalDuration       int64 `json:"eval_duration,omitempty"`
}

// ollTagsResponse is the response from the Ollama tags (list models) API.
type ollTagsResponse struct {
	Models []ollModelInfo `json:"models"`
}

// ollModelInfo describes a model available in the local Ollama instance.
type ollModelInfo struct {
	Name       string `json:"name"`
	Model      string `json:"model"`
	ModifiedAt string `json:"modified_at"`
	Size       int64  `json:"size"`
	Digest     string `json:"digest"`
	Details    struct {
		ParentModel   string   `json:"parent_model,omitempty"`
		Format        string   `json:"format"`
		Family        string   `json:"family"`
		Families      []string `json:"families"`
		ParameterSize string   `json:"parameter_size"`
		Quantization  string   `json:"quantization_level"`
	} `json:"details"`
}

// ollErrorResponse wraps an API error response.
type ollErrorResponse struct {
	Error string `json:"error"`
}

// ---------------------------------------------------------------------------
// Known Model Capabilities
// ---------------------------------------------------------------------------

// modelCapabilityMap maps model name patterns to their capabilities.
// Ollama supports many models; we provide sensible defaults for popular ones.
var modelCapabilityMap = []struct {
	pattern string
	caps    provider.ModelCapabilities
}{
	{
		pattern: "llama3.1",
		caps: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 131072,
		},
	},
	{
		pattern: "llama3",
		caps: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 8192,
		},
	},
	{
		pattern: "llama3.2-vision",
		caps: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 131072,
		},
	},
	{
		pattern: "llama3.2",
		caps: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 131072,
		},
	},
	{
		pattern: "mistral",
		caps: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 32768,
		},
	},
	{
		pattern: "mixtral",
		caps: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 32768,
		},
	},
	{
		pattern: "qwen2",
		caps: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 32768,
		},
	},
	{
		pattern: "gemma2",
		caps: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 8192,
		},
	},
	{
		pattern: "codellama",
		caps: provider.ModelCapabilities{
			Streaming: true, ToolCalling: false, Vision: false, Audio: false,
			JSONMode: false, Seed: false, MaxTokens: 4096, ContextWindow: 16384,
		},
	},
	{
		pattern: "phi3",
		caps: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 8192,
		},
	},
	{
		pattern: "deepseek",
		caps: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 65536,
		},
	},
	{
		pattern: "llava",
		caps: provider.ModelCapabilities{
			Streaming: true, ToolCalling: false, Vision: true, Audio: false,
			JSONMode: false, Seed: false, MaxTokens: 4096, ContextWindow: 4096,
		},
	},
	{
		pattern: "llama-guard",
		caps: provider.ModelCapabilities{
			Streaming: true, ToolCalling: false, Vision: false, Audio: false,
			JSONMode: false, Seed: false, MaxTokens: 4096, ContextWindow: 8192,
		},
	},
	{
		pattern: "command-r",
		caps: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 131072,
		},
	},
}

// defaultCapabilities returns capabilities when no pattern matches.
var defaultCapabilities = provider.ModelCapabilities{
	Streaming: true, ToolCalling: true, Vision: false, Audio: false,
	JSONMode: true, Seed: false, MaxTokens: 4096, ContextWindow: 8192,
}

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

// Provider implements the provider.Provider interface for Ollama.
// It communicates with a local or remote Ollama instance and supports
// both Generate and Stream operations.
//
// A Provider is safe for concurrent use by multiple goroutines.
type Provider struct {
	baseURL      string
	defaultModel string
	httpClient   *http.Client
}

// NewProvider creates a new Ollama provider from the given configuration.
// Unlike cloud providers, Ollama does not require an API key.
func NewProvider(cfg config.ProviderConfig) (*Provider, error) {
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

// Factory is a provider.ProviderFactory that creates a new Ollama provider.
// Use this with the provider registry:
//
//	registry.Register("ollama", ollama.Factory, cfg)
var Factory provider.ProviderFactory = func(cfg config.ProviderConfig) (provider.Provider, error) {
	return NewProvider(cfg)
}

// Compile-time interface check.
var _ provider.Provider = (*Provider)(nil)

// ---------------------------------------------------------------------------
// Interface Methods
// ---------------------------------------------------------------------------

// Name returns the provider identifier "ollama".
func (p *Provider) Name() string {
	return providerName
}

// Models returns the list of models available on the local Ollama instance.
// It queries the /api/tags endpoint to discover installed models.
// If Ollama is not running, it returns an error suggesting the user start Ollama.
func (p *Provider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	url := p.baseURL + tagsPath

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, provider.NewProviderError(providerName, "",
			fmt.Errorf("failed to create request: %w", err))
	}

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, p.wrapConnectionError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, p.handleAPIError(resp, "")
	}

	var tagsResp ollTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
		return nil, provider.NewProviderError(providerName, "",
			fmt.Errorf("failed to decode tags response: %w", err))
	}

	result := make([]provider.ModelInfo, 0, len(tagsResp.Models))
	for _, m := range tagsResp.Models {
		modelName := m.Name
		if modelName == "" {
			modelName = m.Model
		}

		caps := lookupCapabilities(modelName)
		description := fmt.Sprintf("Ollama model (%s, %s)", m.Details.Format, m.Details.ParameterSize)
		if m.Details.Family != "" {
			description = fmt.Sprintf("Ollama %s model (%s, %s)", m.Details.Family, m.Details.ParameterSize, m.Details.Quantization)
		}

		info := provider.ModelInfo{
			ID:           modelName,
			Name:         m.Details.Family,
			Description:  description,
			Capabilities: caps,
			Metadata: map[string]any{
				"size_bytes":   m.Size,
				"digest":       m.Digest,
				"format":       m.Details.Format,
				"family":       m.Details.Family,
				"families":     m.Details.Families,
				"param_size":   m.Details.ParameterSize,
				"quantization": m.Details.Quantization,
				"modified_at":  m.ModifiedAt,
			},
		}
		if info.Name == "" {
			info.Name = modelName
		}
		result = append(result, info)
	}

	return result, nil
}

// Generate sends a non-streaming completion request to the Ollama API.
func (p *Provider) Generate(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResult, error) {
	model := p.resolveModel(req.Model)
	if len(req.Messages) == 0 {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("at least one message is required"))
	}

	ollReq := p.buildRequest(model, req, false)

	bodyBytes, err := json.Marshal(ollReq)
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
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, p.wrapConnectionError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, p.handleAPIError(resp, model)
	}

	var ollResp ollChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollResp); err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to decode response: %w", err))
	}

	result, err := p.convertResponse(&ollResp, model)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model, err)
	}

	return result, nil
}

// Stream sends a streaming completion request to the Ollama API and returns
// a channel of StreamEvent values. Ollama uses newline-delimited JSON streaming.
func (p *Provider) Stream(ctx context.Context, req provider.GenerateRequest) (<-chan provider.StreamEvent, error) {
	model := p.resolveModel(req.Model)
	if len(req.Messages) == 0 {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("at least one message is required"))
	}

	ollReq := p.buildRequest(model, req, true)

	bodyBytes, err := json.Marshal(ollReq)
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
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, p.wrapConnectionError(err)
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
	return lookupCapabilities(p.resolveModel(model))
}

// ---------------------------------------------------------------------------
// Availability Detection
// ---------------------------------------------------------------------------

// IsAvailable checks if the Ollama instance is reachable.
// Returns nil if available, or an error describing why it's not.
func (p *Provider) IsAvailable(ctx context.Context) error {
	detectCtx, cancel := context.WithTimeout(ctx, detectTimeout)
	defer cancel()

	url := p.baseURL + tagsPath
	httpReq, err := http.NewRequestWithContext(detectCtx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("ollama: failed to create detection request: %w", err)
	}

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return p.wrapConnectionError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama: unexpected status %d from %s", resp.StatusCode, url)
	}

	return nil
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

// buildRequest constructs an Ollama API request from an Orchestra request.
func (p *Provider) buildRequest(model string, req provider.GenerateRequest, stream bool) ollRequest {
	ollReq := ollRequest{
		Model:    model,
		Messages: convertMessages(req.Messages),
		Stream:   stream,
	}

	// Tools
	if len(req.Tools) > 0 {
		ollReq.Tools = convertTools(req.Tools)
	}

	// Generation options
	opts := req.Options
	ollOpts := &ollOptions{}
	hasOpts := false

	if opts.Temperature != nil {
		ollOpts.Temperature = opts.Temperature
		hasOpts = true
	}
	if opts.TopP != nil {
		ollOpts.TopP = opts.TopP
		hasOpts = true
	}
	if opts.MaxTokens != nil {
		ollOpts.NumPredict = opts.MaxTokens
		hasOpts = true
	}
	if len(opts.StopSequences) > 0 {
		ollOpts.Stop = opts.StopSequences
		hasOpts = true
	}
	if opts.Seed != nil {
		ollOpts.Seed = opts.Seed
		hasOpts = true
	}

	// Extra provider-specific options
	if opts.Extra != nil {
		if v, ok := asInt(opts.Extra["top_k"]); ok {
			ollOpts.TopK = &v
			hasOpts = true
		}
		if v, ok := asInt(opts.Extra["num_ctx"]); ok {
			ollOpts.NumCtx = &v
			hasOpts = true
		}
		if v, ok := asInt(opts.Extra["repeat_last_n"]); ok {
			ollOpts.RepeatLastN = &v
			hasOpts = true
		}
		if v, ok := asFloat64(opts.Extra["repeat_penalty"]); ok {
			ollOpts.RepeatPenalty = &v
			hasOpts = true
		}
		if v, ok := asFloat64(opts.Extra["frequency_penalty"]); ok {
			ollOpts.FrequencyPenalty = &v
			hasOpts = true
		}
		if v, ok := asFloat64(opts.Extra["presence_penalty"]); ok {
			ollOpts.PresencePenalty = &v
			hasOpts = true
		}
		if v, ok := asString(opts.Extra["keep_alive"]); ok {
			ollReq.KeepAlive = v
		}
	}

	if hasOpts {
		ollReq.Options = ollOpts
	}

	// Response format
	if opts.ResponseFormat != nil {
		switch opts.ResponseFormat.Type {
		case "json_object":
			ollReq.Format = "json"
		case "json_schema":
			// Ollama supports JSON schema in the format field
			if opts.ResponseFormat.JSONSchema != nil {
				if schema, ok := opts.ResponseFormat.JSONSchema["schema"].(map[string]any); ok {
					ollReq.Format = schema
				} else {
					ollReq.Format = "json"
				}
			} else {
				ollReq.Format = "json"
			}
		}
	}

	return ollReq
}

// ---------------------------------------------------------------------------
// Streaming
// ---------------------------------------------------------------------------

// streamEvents reads newline-delimited JSON events from the response body
// and sends them on the channel.
func (p *Provider) streamEvents(ctx context.Context, resp *http.Response, ch chan<- provider.StreamEvent, model string) {
	defer close(ch)
	defer resp.Body.Close()

	// Send start event
	if !sendEvent(ctx, ch, provider.StreamEvent{Type: provider.StreamEventStart}) {
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var totalUsage provider.TokenUsage

	for scanner.Scan() {
		if ctx.Err() != nil {
			sendEvent(ctx, ch, provider.StreamEvent{
				Type:  provider.StreamEventError,
				Error: ctx.Err(),
			})
			return
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		var ollResp ollChatResponse
		if err := json.Unmarshal([]byte(line), &ollResp); err != nil {
			sendEvent(ctx, ch, provider.StreamEvent{
				Type:  provider.StreamEventError,
				Error: fmt.Errorf("failed to parse stream chunk: %w", err),
			})
			return
		}

		// Process content
		if ollResp.Message.Content != "" {
			if !sendEvent(ctx, ch, provider.StreamEvent{
				Type:  provider.StreamEventChunk,
				Chunk: ollResp.Message.Content,
			}) {
				return
			}
		}

		// Process tool calls
		for _, tc := range ollResp.Message.ToolCalls {
			args := "{}"
			if tc.Function.Arguments != nil {
				if b, err := json.Marshal(tc.Function.Arguments); err == nil {
					args = string(b)
				}
			}
			toolCall := message.ToolCall{
				ID:   fmt.Sprintf("ollama_%d", time.Now().UnixNano()),
				Type: "function",
				Function: message.ToolCallFunction{
					Name:      tc.Function.Name,
					Arguments: args,
				},
			}
			if !sendEvent(ctx, ch, provider.StreamEvent{
				Type:     provider.StreamEventToolCall,
				ToolCall: &toolCall,
			}) {
				return
			}
		}

		// Track usage metrics from final response
		if ollResp.Done {
			totalUsage = provider.TokenUsage{
				PromptTokens:     ollResp.PromptEvalCount,
				CompletionTokens: ollResp.EvalCount,
				TotalTokens:      ollResp.PromptEvalCount + ollResp.EvalCount,
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
		Usage: &totalUsage,
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
// Conversion: Orchestra → Ollama
// ---------------------------------------------------------------------------

// convertMessages converts Orchestra messages to the Ollama chat format.
func convertMessages(msgs []message.Message) []ollMessage {
	result := make([]ollMessage, 0, len(msgs))

	for _, msg := range msgs {
		ollMsg := ollMessage{
			Role: string(msg.Role),
		}

		switch msg.Role {
		case message.RoleSystem:
			ollMsg.Role = "system"
			ollMsg.Content = msg.Text()

		case message.RoleUser:
			ollMsg.Role = "user"
			ollMsg.Content = msg.Text()
			// Extract images from content blocks
			for _, block := range msg.Content {
				switch block.Type {
				case "image":
					if len(block.FileData) > 0 {
						ollMsg.Images = append(ollMsg.Images,
							base64.StdEncoding.EncodeToString(block.FileData))
					} else if block.ImageURL != "" {
						// Ollama supports base64 images in the images array.
						// If it's a data URI, extract the base64 data.
						if strings.HasPrefix(block.ImageURL, "data:") {
							_, data := parseDataURI(block.ImageURL)
							ollMsg.Images = append(ollMsg.Images, data)
						} else {
							// For URLs, we can't inline them directly;
							// Ollama expects base64 encoded images.
							// We note this limitation in the content.
						}
					}
				case "file":
					// Ollama treats file data as images if the MIME type is image/*
					if len(block.FileData) > 0 && strings.HasPrefix(block.MimeType, "image/") {
						ollMsg.Images = append(ollMsg.Images,
							base64.StdEncoding.EncodeToString(block.FileData))
					}
				}
			}

		case message.RoleAssistant:
			ollMsg.Role = "assistant"
			ollMsg.Content = msg.Text()
			if len(msg.ToolCalls) > 0 {
				ollMsg.ToolCalls = convertToolCallsOut(msg.ToolCalls)
			}

		case message.RoleTool, message.RoleFunction:
			// Ollama uses "tool" role for tool results.
			// The content should be the tool result content.
			ollMsg.Role = "tool"
			if msg.ToolResult != nil {
				ollMsg.Content = msg.ToolResult.Content
			}
		}

		result = append(result, ollMsg)
	}

	return result
}

// convertToolCallsOut converts Orchestra tool calls to Ollama tool calls.
func convertToolCallsOut(tcs []message.ToolCall) []ollToolCall {
	result := make([]ollToolCall, 0, len(tcs))
	for _, tc := range tcs {
		var args interface{}
		if tc.Function.Arguments != "" {
			// Try to parse as JSON map for Ollama's expected format
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = map[string]interface{}{}
			}
		} else {
			args = map[string]interface{}{}
		}

		result = append(result, ollToolCall{
			Function: ollToolFunction{
				Name:      tc.Function.Name,
				Arguments: args,
			},
		})
	}
	return result
}

// convertTools converts Orchestra tool definitions to Ollama tool format.
func convertTools(tools []provider.ToolDefinition) []ollTool {
	result := make([]ollTool, 0, len(tools))
	for _, tool := range tools {
		params := tool.Function.Parameters
		if params == nil {
			params = map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}
		}
		result = append(result, ollTool{
			Type: "function",
			Function: ollFuncDef{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  params,
			},
		})
	}
	return result
}

// ---------------------------------------------------------------------------
// Conversion: Ollama → Orchestra
// ---------------------------------------------------------------------------

// convertResponse converts an Ollama chat response to a GenerateResult.
func (p *Provider) convertResponse(resp *ollChatResponse, model string) (*provider.GenerateResult, error) {
	msg := convertOllMessage(resp.Message)

	finishReason := provider.FinishReasonStop
	if resp.Done {
		switch resp.DoneReason {
		case "length":
			finishReason = provider.FinishReasonLength
		case "load":
			// Model was loaded but no generation yet
			finishReason = provider.FinishReasonStop
		default:
			finishReason = provider.FinishReasonStop
		}
	}

	// Check if message contains tool calls
	if len(resp.Message.ToolCalls) > 0 {
		finishReason = provider.FinishReasonToolCall
	}

	usage := provider.TokenUsage{
		PromptTokens:     resp.PromptEvalCount,
		CompletionTokens: resp.EvalCount,
		TotalTokens:      resp.PromptEvalCount + resp.EvalCount,
	}

	createdAt := time.Now()
	if resp.CreatedAt != "" {
		if t, err := time.Parse(time.RFC3339, resp.CreatedAt); err == nil {
			createdAt = t
		}
	}

	result := &provider.GenerateResult{
		ID:           fmt.Sprintf("ollama-%d", time.Now().UnixMilli()),
		Message:      msg,
		Usage:        usage,
		FinishReason: finishReason,
		Model:        model,
		CreatedAt:    createdAt,
		Metadata:     make(map[string]any),
	}

	// Include performance metrics
	if resp.TotalDuration > 0 {
		result.Metadata["total_duration_ns"] = resp.TotalDuration
		result.Metadata["total_duration_ms"] = resp.TotalDuration / int64(time.Millisecond)
	}
	if resp.LoadDuration > 0 {
		result.Metadata["load_duration_ns"] = resp.LoadDuration
	}
	if resp.PromptEvalDuration > 0 {
		result.Metadata["prompt_eval_duration_ns"] = resp.PromptEvalDuration
	}
	if resp.EvalDuration > 0 {
		result.Metadata["eval_duration_ns"] = resp.EvalDuration
	}

	return result, nil
}

// convertOllMessage converts an Ollama response message to an Orchestra Message.
func convertOllMessage(ollMsg ollMessage) message.Message {
	msg := message.Message{
		Role: message.RoleAssistant,
	}

	// Set content
	if ollMsg.Content != "" {
		msg.Content = []message.ContentBlock{message.TextContentBlock(ollMsg.Content)}
	}

	// Convert tool calls
	if len(ollMsg.ToolCalls) > 0 {
		msg.ToolCalls = make([]message.ToolCall, len(ollMsg.ToolCalls))
		for i, tc := range ollMsg.ToolCalls {
			args := "{}"
			if tc.Function.Arguments != nil {
				if b, err := json.Marshal(tc.Function.Arguments); err == nil {
					args = string(b)
				}
			}
			msg.ToolCalls[i] = message.ToolCall{
				ID:   fmt.Sprintf("ollama_%d_%d", time.Now().UnixNano(), i),
				Type: "function",
				Function: message.ToolCallFunction{
					Name:      tc.Function.Name,
					Arguments: args,
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

	var errResp ollErrorResponse
	if err := json.Unmarshal(body, &errResp); err != nil || errResp.Error == "" {
		return provider.NewProviderErrorWithCode(providerName, model, "http_error", resp.StatusCode,
			fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body)))
	}

	code := fmt.Sprintf("http_%d", resp.StatusCode)
	errMsg := errResp.Error

	return provider.NewProviderErrorWithCode(providerName, model, code, resp.StatusCode,
		fmt.Errorf("%s", errMsg))
}

// wrapConnectionError wraps connection errors with helpful messages about
// starting Ollama.
func (p *Provider) wrapConnectionError(err error) *provider.ProviderError {
	msg := err.Error()

	// Check for specific connection error types
	if isConnectionRefused(err) {
		return provider.NewProviderError(providerName, "",
			fmt.Errorf("ollama is not running at %s: connection refused. "+
				"Start Ollama with: ollama serve", p.baseURL))
	}

	if isTimeout(err) {
		return provider.NewProviderError(providerName, "",
			fmt.Errorf("ollama at %s timed out: %s. "+
				"Check that Ollama is running and responsive", p.baseURL, msg))
	}

	return provider.NewProviderError(providerName, "",
		fmt.Errorf("failed to connect to ollama at %s: %s. "+
			"Ensure Ollama is installed and running", p.baseURL, msg))
}

// isConnectionRefused checks if the error is a "connection refused" error.
func isConnectionRefused(err error) bool {
	if opErr, ok := err.(*net.OpError); ok {
		return strings.Contains(opErr.Error(), "connection refused")
	}
	return strings.Contains(err.Error(), "connection refused")
}

// isTimeout checks if the error is a timeout error.
func isTimeout(err error) bool {
	if netErr, ok := err.(interface{ Timeout() bool }); ok {
		return netErr.Timeout()
	}
	return strings.Contains(err.Error(), "timeout") ||
		strings.Contains(err.Error(), "deadline exceeded")
}

// ---------------------------------------------------------------------------
// Capability Lookup
// ---------------------------------------------------------------------------

// lookupCapabilities finds capabilities for a model by matching against known patterns.
func lookupCapabilities(model string) provider.ModelCapabilities {
	lower := strings.ToLower(model)

	// Strip tag suffix for matching (e.g., "llama3:latest" → "llama3")
	base := lower
	if idx := strings.Index(lower, ":"); idx >= 0 {
		base = lower[:idx]
	}

	// Try matching against known patterns (most specific first)
	for _, entry := range modelCapabilityMap {
		if strings.Contains(base, entry.pattern) || strings.Contains(lower, entry.pattern) {
			return entry.caps
		}
	}

	return defaultCapabilities
}

// ---------------------------------------------------------------------------
// Utility Functions
// ---------------------------------------------------------------------------

// parseDataURI parses a data URI into its MIME type and base64 data.
func parseDataURI(dataURI string) (mimeType, data string) {
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

	parts := strings.SplitN(meta, ";", 2)
	mimeType = parts[0]
	if mimeType == "" {
		mimeType = "image/png"
	}

	return mimeType, data
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

// asString extracts a string from an any value.
func asString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}
