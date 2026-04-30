// Package gemini implements the Google Gemini API provider for Orchestra.
//
// It supports Generate and Stream operations, function calling with
// Gemini's FunctionCall/FunctionResponse, multi-modal inputs (text,
// image, video, audio), and safety settings.
//
// Register with the Orchestra provider registry:
//
//	registry.Register("gemini", gemini.Factory, cfg)
package gemini

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
	defaultBaseURL      = "https://generativelanguage.googleapis.com/v1beta"
	providerName        = "gemini"
	defaultModel        = "gemini-2.0-flash"
	generateContentPath = "/models/%s:generateContent"
	streamContentPath   = "/models/%s:streamGenerateContent?alt=sse"
	modelsListPath      = "/models"
	sseDataPrefix       = "data: "
	httpTimeout         = 10 * time.Minute
	maxIdleConns        = 100
	maxIdleConnsPerHost = 100
	idleConnTimeout     = 90 * time.Second
)

// ---------------------------------------------------------------------------
// Gemini API Types — Request
// ---------------------------------------------------------------------------

// gemRequest is the request body for the Gemini generateContent API.
type gemRequest struct {
	Contents          []gemContent       `json:"contents"`
	Tools             []gemTool          `json:"tools,omitempty"`
	SystemInstruction *gemContent        `json:"systemInstruction,omitempty"`
	GenerationConfig  *gemGenConfig      `json:"generationConfig,omitempty"`
	SafetySettings    []gemSafetySetting `json:"safetySettings,omitempty"`
}

// gemContent represents a single content message in Gemini format.
type gemContent struct {
	Role  string    `json:"role,omitempty"`
	Parts []gemPart `json:"parts"`
}

// gemPart represents a single part within a content message.
// Only one of the fields should be set.
type gemPart struct {
	// Text content
	Text string `json:"text,omitempty"`

	// Inline data (images, video, audio)
	InlineData *gemInlineData `json:"inlineData,omitempty"`

	// File URI reference
	FileData *gemFileData `json:"fileData,omitempty"`

	// Function call (in model responses)
	FunctionCall *gemFunctionCall `json:"functionCall,omitempty"`

	// Function response (in user messages after tool execution)
	FunctionResponse *gemFunctionResponse `json:"functionResponse,omitempty"`
}

// gemInlineData represents inline binary data.
type gemInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"` // base64 encoded
}

// gemFileData represents a file reference by URI.
type gemFileData struct {
	MimeType string `json:"mimeType,omitempty"`
	FileURI  string `json:"fileUri"`
}

// gemFunctionCall represents a function call from the model.
type gemFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

// gemFunctionResponse represents a function response to send back.
type gemFunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

// gemGenConfig configures generation parameters.
type gemGenConfig struct {
	Temperature      *float64       `json:"temperature,omitempty"`
	TopP             *float64       `json:"topP,omitempty"`
	TopK             *int           `json:"topK,omitempty"`
	MaxOutputTokens  *int           `json:"maxOutputTokens,omitempty"`
	StopSequences    []string       `json:"stopSequences,omitempty"`
	Seed             *int64         `json:"seed,omitempty"`
	ResponseMimeType string         `json:"responseMimeType,omitempty"`
	ResponseSchema   map[string]any `json:"responseSchema,omitempty"`
	CandidateCount   *int           `json:"candidateCount,omitempty"`
	PresencePenalty  *float64       `json:"presencePenalty,omitempty"`
	FrequencyPenalty *float64       `json:"frequencyPenalty,omitempty"`
}

// gemTool describes a set of function declarations.
type gemTool struct {
	FunctionDeclarations []gemFunctionDecl `json:"functionDeclarations"`
}

// gemFunctionDecl describes a function that the model can call.
type gemFunctionDecl struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// gemSafetySetting configures content safety filtering.
type gemSafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

// ---------------------------------------------------------------------------
// Gemini API Types — Response
// ---------------------------------------------------------------------------

// gemResponse is the response from the generateContent API.
type gemResponse struct {
	Candidates     []gemCandidate     `json:"candidates"`
	UsageMetadata  *gemUsageMetadata  `json:"usageMetadata,omitempty"`
	ModelVersion   string             `json:"modelVersion,omitempty"`
	PromptFeedback *gemPromptFeedback `json:"promptFeedback,omitempty"`
}

// gemCandidate is a single completion candidate.
type gemCandidate struct {
	Content       gemContent        `json:"content"`
	FinishReason  string            `json:"finishReason,omitempty"`
	Index         int               `json:"index,omitempty"`
	SafetyRatings []gemSafetyRating `json:"safetyRatings,omitempty"`
}

// gemSafetyRating contains safety assessment for content.
type gemSafetyRating struct {
	Category    string `json:"category"`
	Probability string `json:"probability"`
}

// gemUsageMetadata reports token consumption.
type gemUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// gemPromptFeedback contains feedback about the prompt.
type gemPromptFeedback struct {
	BlockReason   string            `json:"blockReason,omitempty"`
	SafetyRatings []gemSafetyRating `json:"safetyRatings,omitempty"`
}

// gemErrorResponse wraps an API error response.
type gemErrorResponse struct {
	Error *gemErrorDetail `json:"error,omitempty"`
}

// gemErrorDetail contains details about an API error.
type gemErrorDetail struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// ---------------------------------------------------------------------------
// Known Models
// ---------------------------------------------------------------------------

// knownModels is the static list of well-known Gemini models.
var knownModels = []provider.ModelInfo{
	{
		ID:          "gemini-2.5-pro-preview-06-05",
		Name:        "Gemini 2.5 Pro",
		Description: "Most capable Gemini model with deep thinking",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: true,
			JSONMode: true, Seed: false, MaxTokens: 65536, ContextWindow: 1048576,
		},
	},
	{
		ID:          "gemini-2.5-flash-preview-05-20",
		Name:        "Gemini 2.5 Flash",
		Description: "Fast and versatile Gemini model with thinking",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: true,
			JSONMode: true, Seed: false, MaxTokens: 65536, ContextWindow: 1048576,
		},
	},
	{
		ID:          "gemini-2.0-flash",
		Name:        "Gemini 2.0 Flash",
		Description: "Fast and efficient Gemini 2.0 model",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: true,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 1048576,
		},
	},
	{
		ID:          "gemini-2.0-flash-lite",
		Name:        "Gemini 2.0 Flash Lite",
		Description: "Cost-efficient Gemini 2.0 model",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 1048576,
		},
	},
	{
		ID:          "gemini-1.5-pro",
		Name:        "Gemini 1.5 Pro",
		Description: "Best quality Gemini 1.5 model with long context",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: true,
			JSONMode: true, Seed: false, MaxTokens: 8192, ContextWindow: 2097152,
		},
	},
	{
		ID:          "gemini-1.5-flash",
		Name:        "Gemini 1.5 Flash",
		Description: "Fast and versatile Gemini 1.5 model",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: true,
			JSONMode: true, Seed: false, MaxTokens: 8192, ContextWindow: 1048576,
		},
	},
	{
		ID:          "gemini-1.5-flash-8b",
		Name:        "Gemini 1.5 Flash 8B",
		Description: "Small and efficient Gemini model",
		Capabilities: provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: false, Seed: false, MaxTokens: 4096, ContextWindow: 1048576,
		},
	},
}

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

// Provider implements the provider.Provider interface for Google Gemini.
// It communicates with the Gemini generativelanguage API and supports
// both Generate and Stream operations.
//
// A Provider is safe for concurrent use by multiple goroutines.
type Provider struct {
	apiKey       string
	baseURL      string
	defaultModel string
	httpClient   *http.Client
}

// NewProvider creates a new Gemini provider from the given configuration.
func NewProvider(cfg config.ProviderConfig) (*Provider, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		return nil, fmt.Errorf("gemini: api_key is required")
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

// Factory is a provider.ProviderFactory that creates a new Gemini provider.
// Use this with the provider registry:
//
//	registry.Register("gemini", gemini.Factory, cfg)
var Factory provider.ProviderFactory = func(cfg config.ProviderConfig) (provider.Provider, error) {
	return NewProvider(cfg)
}

// Compile-time interface check.
var _ provider.Provider = (*Provider)(nil)

// ---------------------------------------------------------------------------
// Interface Methods
// ---------------------------------------------------------------------------

// Name returns the provider identifier "gemini".
func (p *Provider) Name() string {
	return providerName
}

// Models returns the list of well-known Gemini models.
func (p *Provider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	result := make([]provider.ModelInfo, len(knownModels))
	copy(result, knownModels)
	return result, nil
}

// Generate sends a non-streaming completion request to the Gemini API.
func (p *Provider) Generate(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResult, error) {
	model := p.resolveModel(req.Model)
	if len(req.Messages) == 0 {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("at least one message is required"))
	}

	gemReq, err := p.buildRequest(req)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to build request: %w", err))
	}

	bodyBytes, err := json.Marshal(gemReq)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to marshal request: %w", err))
	}

	path := fmt.Sprintf(generateContentPath, model)
	url := p.baseURL + path + "?key=" + p.apiKey

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to create request: %w", err))
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("request failed: %w", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, p.handleAPIError(resp, model)
	}

	var gemResp gemResponse
	if err := json.NewDecoder(resp.Body).Decode(&gemResp); err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to decode response: %w", err))
	}

	// Check if prompt was blocked
	if gemResp.PromptFeedback != nil && gemResp.PromptFeedback.BlockReason != "" {
		return nil, provider.NewProviderErrorWithCode(providerName, model, "content_blocked", resp.StatusCode,
			fmt.Errorf("prompt blocked: %s", gemResp.PromptFeedback.BlockReason))
	}

	result, err := p.convertResponse(&gemResp, model)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model, err)
	}

	return result, nil
}

// Stream sends a streaming completion request to the Gemini API and returns
// a channel of StreamEvent values.
func (p *Provider) Stream(ctx context.Context, req provider.GenerateRequest) (<-chan provider.StreamEvent, error) {
	model := p.resolveModel(req.Model)
	if len(req.Messages) == 0 {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("at least one message is required"))
	}

	gemReq, err := p.buildRequest(req)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to build request: %w", err))
	}

	bodyBytes, err := json.Marshal(gemReq)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to marshal request: %w", err))
	}

	path := fmt.Sprintf(streamContentPath, model)
	url := p.baseURL + path + "&key=" + p.apiKey

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to create request: %w", err))
	}
	httpReq.Header.Set("Content-Type", "application/json")

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
	case strings.Contains(lower, "2.5-pro"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: true,
			JSONMode: true, Seed: false, MaxTokens: 65536, ContextWindow: 1048576,
		}
	case strings.Contains(lower, "2.5-flash"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: true,
			JSONMode: true, Seed: false, MaxTokens: 65536, ContextWindow: 1048576,
		}
	case strings.Contains(lower, "2.0-flash-lite"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: false,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 1048576,
		}
	case strings.Contains(lower, "2.0-flash"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: true,
			JSONMode: true, Seed: true, MaxTokens: 8192, ContextWindow: 1048576,
		}
	case strings.Contains(lower, "1.5-pro"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: true,
			JSONMode: true, Seed: false, MaxTokens: 8192, ContextWindow: 2097152,
		}
	case strings.Contains(lower, "1.5-flash-8b"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: false, Seed: false, MaxTokens: 4096, ContextWindow: 1048576,
		}
	case strings.Contains(lower, "1.5-flash"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: true, Audio: true,
			JSONMode: true, Seed: false, MaxTokens: 8192, ContextWindow: 1048576,
		}
	case strings.Contains(lower, "1.0-pro"):
		return provider.ModelCapabilities{
			Streaming: true, ToolCalling: true, Vision: false, Audio: false,
			JSONMode: false, Seed: false, MaxTokens: 4096, ContextWindow: 32768,
		}
	}

	// Sensible default for unknown Gemini models
	return provider.ModelCapabilities{
		Streaming: true, ToolCalling: true, Vision: true, Audio: false,
		JSONMode: true, Seed: false, MaxTokens: 8192, ContextWindow: 1048576,
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

// buildRequest constructs a Gemini API request from an Orchestra request.
func (p *Provider) buildRequest(req provider.GenerateRequest) (*gemRequest, error) {
	systemContent, contents, err := convertMessages(req.Messages)
	if err != nil {
		return nil, err
	}

	gemReq := &gemRequest{
		Contents: contents,
	}

	// System instruction
	if systemContent != nil {
		gemReq.SystemInstruction = systemContent
	}

	// Tools
	if len(req.Tools) > 0 {
		gemReq.Tools = convertTools(req.Tools)
	}

	// Generation config
	opts := req.Options
	genConfig := &gemGenConfig{}
	hasConfig := false

	if opts.Temperature != nil {
		genConfig.Temperature = opts.Temperature
		hasConfig = true
	}
	if opts.TopP != nil {
		genConfig.TopP = opts.TopP
		hasConfig = true
	}
	if opts.MaxTokens != nil {
		genConfig.MaxOutputTokens = opts.MaxTokens
		hasConfig = true
	}
	if len(opts.StopSequences) > 0 {
		genConfig.StopSequences = opts.StopSequences
		hasConfig = true
	}
	if opts.Seed != nil {
		genConfig.Seed = opts.Seed
		hasConfig = true
	}

	// Response format
	if opts.ResponseFormat != nil {
		switch opts.ResponseFormat.Type {
		case "json_object":
			genConfig.ResponseMimeType = "application/json"
			hasConfig = true
		case "json_schema":
			genConfig.ResponseMimeType = "application/json"
			if opts.ResponseFormat.JSONSchema != nil {
				// Gemini expects the schema directly
				if schema, ok := opts.ResponseFormat.JSONSchema["schema"].(map[string]any); ok {
					genConfig.ResponseSchema = schema
				} else {
					genConfig.ResponseSchema = opts.ResponseFormat.JSONSchema
				}
			}
			hasConfig = true
		}
	}

	// Extra provider-specific options
	if opts.Extra != nil {
		if v, ok := asInt(opts.Extra["top_k"]); ok {
			genConfig.TopK = &v
			hasConfig = true
		}
		if v, ok := asFloat64(opts.Extra["presence_penalty"]); ok {
			genConfig.PresencePenalty = &v
			hasConfig = true
		}
		if v, ok := asFloat64(opts.Extra["frequency_penalty"]); ok {
			genConfig.FrequencyPenalty = &v
			hasConfig = true
		}
	}

	if hasConfig {
		gemReq.GenerationConfig = genConfig
	}

	return gemReq, nil
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

	var usage *provider.TokenUsage

	for scanner.Scan() {
		if ctx.Err() != nil {
			sendEvent(ctx, ch, provider.StreamEvent{
				Type:  provider.StreamEventError,
				Error: ctx.Err(),
			})
			return
		}

		line := scanner.Text()

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// Parse SSE data lines
		if !strings.HasPrefix(line, sseDataPrefix) {
			continue
		}

		data := strings.TrimPrefix(line, sseDataPrefix)
		if data == "" {
			continue
		}

		// Gemini streaming sends each chunk as a full gemResponse JSON
		var gemResp gemResponse
		if err := json.Unmarshal([]byte(data), &gemResp); err != nil {
			sendEvent(ctx, ch, provider.StreamEvent{
				Type:  provider.StreamEventError,
				Error: fmt.Errorf("failed to parse stream chunk: %w", err),
			})
			return
		}

		// Check for prompt block
		if gemResp.PromptFeedback != nil && gemResp.PromptFeedback.BlockReason != "" {
			sendEvent(ctx, ch, provider.StreamEvent{
				Type:  provider.StreamEventError,
				Error: fmt.Errorf("prompt blocked: %s", gemResp.PromptFeedback.BlockReason),
			})
			return
		}

		// Process candidates
		for _, candidate := range gemResp.Candidates {
			for _, part := range candidate.Content.Parts {
				// Text chunks
				if part.Text != "" {
					if !sendEvent(ctx, ch, provider.StreamEvent{
						Type:  provider.StreamEventChunk,
						Chunk: part.Text,
					}) {
						return
					}
				}

				// Function calls — emit as tool_call events
				if part.FunctionCall != nil {
					args := "{}"
					if part.FunctionCall.Args != nil {
						if b, err := json.Marshal(part.FunctionCall.Args); err == nil {
							args = string(b)
						}
					}
					tc := message.ToolCall{
						ID:   fmt.Sprintf("gemini_%s_%d", part.FunctionCall.Name, time.Now().UnixNano()),
						Type: "function",
						Function: message.ToolCallFunction{
							Name:      part.FunctionCall.Name,
							Arguments: args,
						},
					}
					if !sendEvent(ctx, ch, provider.StreamEvent{
						Type:     provider.StreamEventToolCall,
						ToolCall: &tc,
					}) {
						return
					}
				}
			}
		}

		// Capture usage metadata
		if gemResp.UsageMetadata != nil {
			usage = &provider.TokenUsage{
				PromptTokens:     gemResp.UsageMetadata.PromptTokenCount,
				CompletionTokens: gemResp.UsageMetadata.CandidatesTokenCount,
				TotalTokens:      gemResp.UsageMetadata.TotalTokenCount,
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
// Conversion: Orchestra → Gemini
// ---------------------------------------------------------------------------

// convertMessages converts Orchestra messages to Gemini format.
// Returns the system instruction content (if any) and the message contents.
func convertMessages(msgs []message.Message) (*gemContent, []gemContent, error) {
	var systemParts []gemPart
	var contents []gemContent

	for _, msg := range msgs {
		switch msg.Role {
		case message.RoleSystem:
			// System messages become systemInstruction in Gemini
			text := msg.Text()
			if text != "" {
				systemParts = append(systemParts, gemPart{Text: text})
			}
			// Also convert any non-text content blocks
			for _, block := range msg.Content {
				if block.Type != "text" {
					if part, ok := contentBlockToPart(block); ok {
						systemParts = append(systemParts, part)
					}
				}
			}

		case message.RoleUser:
			parts := convertContentBlocks(msg.Content)
			if len(parts) > 0 {
				contents = append(contents, gemContent{
					Role:  "user",
					Parts: parts,
				})
			}

		case message.RoleAssistant:
			parts := convertAssistantParts(msg)
			if len(parts) > 0 {
				contents = append(contents, gemContent{
					Role:  "model",
					Parts: parts,
				})
			}

		case message.RoleTool, message.RoleFunction:
			// Tool results become user messages with functionResponse parts
			if msg.ToolResult != nil {
				var response map[string]any
				if msg.ToolResult.Content != "" {
					// Try to parse as JSON
					if err := json.Unmarshal([]byte(msg.ToolResult.Content), &response); err != nil {
						// Not JSON, wrap as plain text
						response = map[string]any{"result": msg.ToolResult.Content}
					}
				}
				if response == nil {
					response = map[string]any{}
				}
				if msg.ToolResult.IsError {
					response["error"] = true
				}
				contents = append(contents, gemContent{
					Role: "user",
					Parts: []gemPart{
						{
							FunctionResponse: &gemFunctionResponse{
								Name:     msg.Name,
								Response: response,
							},
						},
					},
				})
			}
		}
	}

	var systemContent *gemContent
	if len(systemParts) > 0 {
		systemContent = &gemContent{
			Parts: systemParts,
		}
	}

	return systemContent, contents, nil
}

// convertContentBlocks converts Orchestra ContentBlocks to Gemini Parts.
func convertContentBlocks(blocks []message.ContentBlock) []gemPart {
	var parts []gemPart
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if block.Text != "" {
				parts = append(parts, gemPart{Text: block.Text})
			}
		case "image":
			if len(block.FileData) > 0 {
				mime := block.MimeType
				if mime == "" {
					mime = "image/png"
				}
				parts = append(parts, gemPart{
					InlineData: &gemInlineData{
						MimeType: mime,
						Data:     base64.StdEncoding.EncodeToString(block.FileData),
					},
				})
			} else if block.ImageURL != "" {
				if strings.HasPrefix(block.ImageURL, "data:") {
					mime, data := parseDataURI(block.ImageURL)
					parts = append(parts, gemPart{
						InlineData: &gemInlineData{
							MimeType: mime,
							Data:     data,
						},
					})
				} else {
					mime := block.MimeType
					if mime == "" {
						mime = "image/png"
					}
					parts = append(parts, gemPart{
						FileData: &gemFileData{
							MimeType: mime,
							FileURI:  block.ImageURL,
						},
					})
				}
			}
		case "file":
			if len(block.FileData) > 0 {
				mime := block.MimeType
				if mime == "" {
					mime = "application/octet-stream"
				}
				parts = append(parts, gemPart{
					InlineData: &gemInlineData{
						MimeType: mime,
						Data:     base64.StdEncoding.EncodeToString(block.FileData),
					},
				})
			}
		}
	}
	return parts
}

// contentBlockToPart converts a single ContentBlock to a Gemini Part.
func contentBlockToPart(block message.ContentBlock) (gemPart, bool) {
	switch block.Type {
	case "text":
		if block.Text != "" {
			return gemPart{Text: block.Text}, true
		}
	case "image":
		if len(block.FileData) > 0 {
			mime := block.MimeType
			if mime == "" {
				mime = "image/png"
			}
			return gemPart{
				InlineData: &gemInlineData{
					MimeType: mime,
					Data:     base64.StdEncoding.EncodeToString(block.FileData),
				},
			}, true
		}
	}
	return gemPart{}, false
}

// convertAssistantParts converts an assistant message to Gemini model content parts.
func convertAssistantParts(msg message.Message) []gemPart {
	var parts []gemPart

	// Text content
	text := msg.Text()
	if text != "" {
		parts = append(parts, gemPart{Text: text})
	}

	// Tool calls become functionCall parts
	for _, tc := range msg.ToolCalls {
		var args map[string]any
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = map[string]any{}
			}
		}
		if args == nil {
			args = map[string]any{}
		}
		parts = append(parts, gemPart{
			FunctionCall: &gemFunctionCall{
				Name: tc.Function.Name,
				Args: args,
			},
		})
	}

	// Ensure at least one part
	if len(parts) == 0 {
		parts = append(parts, gemPart{Text: ""})
	}

	return parts
}

// convertTools converts Orchestra tool definitions to Gemini tool format.
func convertTools(tools []provider.ToolDefinition) []gemTool {
	declarations := make([]gemFunctionDecl, 0, len(tools))
	for _, tool := range tools {
		params := tool.Function.Parameters
		if params == nil {
			params = map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}
		}
		declarations = append(declarations, gemFunctionDecl{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			Parameters:  params,
		})
	}
	return []gemTool{{FunctionDeclarations: declarations}}
}

// ---------------------------------------------------------------------------
// Conversion: Gemini → Orchestra
// ---------------------------------------------------------------------------

// convertResponse converts a Gemini response to a GenerateResult.
func (p *Provider) convertResponse(resp *gemResponse, model string) (*provider.GenerateResult, error) {
	if len(resp.Candidates) == 0 {
		return nil, fmt.Errorf("no candidates in response")
	}

	candidate := resp.Candidates[0]
	msg := convertGemContent(candidate.Content)

	finishReason := mapFinishReason(candidate.FinishReason)

	usage := provider.TokenUsage{}
	if resp.UsageMetadata != nil {
		usage = provider.TokenUsage{
			PromptTokens:     resp.UsageMetadata.PromptTokenCount,
			CompletionTokens: resp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      resp.UsageMetadata.TotalTokenCount,
		}
	}

	result := &provider.GenerateResult{
		ID:           fmt.Sprintf("gemini-%d", time.Now().UnixMilli()),
		Message:      msg,
		Usage:        usage,
		FinishReason: finishReason,
		Model:        model,
		CreatedAt:    time.Now(),
		Metadata:     make(map[string]any),
	}

	if resp.ModelVersion != "" {
		result.Metadata["model_version"] = resp.ModelVersion
	}

	// Include safety ratings if present
	if len(candidate.SafetyRatings) > 0 {
		result.Metadata["safety_ratings"] = candidate.SafetyRatings
	}

	return result, nil
}

// convertGemContent converts Gemini content blocks to an Orchestra Message.
func convertGemContent(content gemContent) message.Message {
	msg := message.Message{
		Role: message.RoleAssistant,
	}

	var textParts []string
	var toolCalls []message.ToolCall

	for _, part := range content.Parts {
		if part.Text != "" {
			textParts = append(textParts, part.Text)
		}
		if part.FunctionCall != nil {
			args := "{}"
			if part.FunctionCall.Args != nil {
				if b, err := json.Marshal(part.FunctionCall.Args); err == nil {
					args = string(b)
				}
			}
			toolCalls = append(toolCalls, message.ToolCall{
				ID:   fmt.Sprintf("gemini_%s_%d", part.FunctionCall.Name, time.Now().UnixNano()),
				Type: "function",
				Function: message.ToolCallFunction{
					Name:      part.FunctionCall.Name,
					Arguments: args,
				},
			})
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

	var errResp gemErrorResponse
	if err := json.Unmarshal(body, &errResp); err != nil || errResp.Error == nil {
		return provider.NewProviderErrorWithCode(providerName, model, "http_error", resp.StatusCode,
			fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body)))
	}

	code := fmt.Sprintf("http_%d", errResp.Error.Code)
	if errResp.Error.Status != "" {
		code = errResp.Error.Status
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

// mapFinishReason maps a Gemini finish reason to a FinishReason.
func mapFinishReason(reason string) provider.FinishReason {
	switch reason {
	case "STOP":
		return provider.FinishReasonStop
	case "MAX_TOKENS":
		return provider.FinishReasonLength
	case "SAFETY":
		return provider.FinishReasonContentFilter
	case "RECITATION":
		return provider.FinishReasonContentFilter
	case "BLOCKLIST":
		return provider.FinishReasonContentFilter
	case "PROHIBITED_CONTENT":
		return provider.FinishReasonContentFilter
	case "SPII":
		return provider.FinishReasonContentFilter
	case "MALFORMED_FUNCTION_CALL":
		return provider.FinishReasonError
	case "OTHER":
		return provider.FinishReasonStop
	default:
		return provider.FinishReasonStop
	}
}

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
