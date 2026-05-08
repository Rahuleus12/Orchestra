// Package openrouter implements the OpenRouter provider for Orchestra.
//
// OpenRouter (https://openrouter.ai/) is an LLM API aggregator that provides
// access to 100+ models from diverse providers through a single API key.
// It uses an OpenAI-compatible API surface (/chat/completions) with additional
// features like dynamic model discovery, pricing metadata, cost tracking,
// and provider routing hints.
//
// Register with the Orchestra provider registry:
//
//	registry.Register("openrouter", openrouter.Factory, cfg)
package openrouter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/user/orchestra/internal/config"
	"github.com/user/orchestra/internal/message"
	"github.com/user/orchestra/internal/provider"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	defaultBaseURL      = "https://openrouter.ai/api/v1"
	providerName        = "openrouter"
	chatCompletionsPath = "/chat/completions"
	modelsEndpoint      = "/models"
	defaultModel        = "openai/gpt-4o"
	sseDataPrefix       = "data: "
	sseDoneMarker       = "[DONE]"
	httpTimeout         = 10 * time.Minute
	maxIdleConns        = 100
	maxIdleConnsPerHost = 100
	idleConnTimeout     = 90 * time.Second
	defaultCacheTTL     = 5 * time.Minute
)

// ---------------------------------------------------------------------------
// OpenRouter API Types (shared with OpenAI-compatible wire format)
// ---------------------------------------------------------------------------

// orChatRequest is the request body for the Chat Completions API.
// Identical wire format to OpenAI with OpenRouter extensions.
type orChatRequest struct {
	Model            string            `json:"model"`
	Messages         []orMessage       `json:"messages"`
	Temperature      *float64          `json:"temperature,omitempty"`
	TopP             *float64          `json:"top_p,omitempty"`
	MaxTokens        *int              `json:"max_tokens,omitempty"`
	Stop             []string          `json:"stop,omitempty"`
	Seed             *int64            `json:"seed,omitempty"`
	ResponseFormat   *orResponseFormat `json:"response_format,omitempty"`
	Tools            []orTool          `json:"tools,omitempty"`
	Stream           bool              `json:"stream,omitempty"`
	FrequencyPenalty *float64          `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64          `json:"presence_penalty,omitempty"`
	N                int               `json:"n,omitempty"`
	// OpenRouter-specific routing fields
	Provider *orProviderPrefs `json:"provider,omitempty"`
}

// orProviderPrefs controls OpenRouter provider routing preferences.
type orProviderPrefs struct {
	// Order is the preferred provider order for model routing.
	Order []string `json:"order,omitempty"`
	// AllowFallbacks permits falling back to alternative providers.
	AllowFallbacks *bool `json:"allow_fallbacks,omitempty"`
	// DataCollection controls training data collection policy.
	DataCollection string `json:"data_collection,omitempty"`
}

// orResponseFormat specifies the response format for the API.
type orResponseFormat struct {
	Type       string         `json:"type"`
	JSONSchema map[string]any `json:"json_schema,omitempty"`
}

// orMessage represents a message in the OpenAI-compatible format.
type orMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCalls  []orToolCall    `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

// orContentPart represents a single content part in a multi-part message.
type orContentPart struct {
	Type     string      `json:"type"`
	Text     string      `json:"text,omitempty"`
	ImageURL *orImageURL `json:"image_url,omitempty"`
}

// orImageURL represents an image URL or data URI.
type orImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// orTool represents a tool definition in the request.
type orTool struct {
	Type     string    `json:"type"`
	Function orFuncDef `json:"function"`
}

// orFuncDef describes a function that can be called by the model.
type orFuncDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// orToolCall represents a tool call in a message.
type orToolCall struct {
	ID       string     `json:"id"`
	Type     string     `json:"type"`
	Function orFunction `json:"function"`
}

// orFunction holds the function name and arguments for a tool call.
type orFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// orChatResponse is the response from the Chat Completions API.
type orChatResponse struct {
	ID      string     `json:"id"`
	Object  string     `json:"object"`
	Created int64      `json:"created"`
	Model   string     `json:"model"`
	Choices []orChoice `json:"choices"`
	Usage   orUsage    `json:"usage,omitempty"`
}

// orChoice is a single completion choice.
type orChoice struct {
	Index        int       `json:"index"`
	Message      orMessage `json:"message"`
	FinishReason *string   `json:"finish_reason"`
}

// orUsage reports token consumption.
type orUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// orErrorResponse wraps an API error response.
type orErrorResponse struct {
	Error orErrorDetail `json:"error"`
}

// orErrorDetail contains details about an API error.
type orErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// --- Streaming types ---

// orStreamChunk is a single chunk in a streaming response.
type orStreamChunk struct {
	ID      string           `json:"id"`
	Object  string           `json:"object"`
	Created int64            `json:"created"`
	Model   string           `json:"model"`
	Choices []orStreamChoice `json:"choices"`
	Usage   *orUsage         `json:"usage,omitempty"`
}

// orStreamChoice is a single choice in a stream chunk.
type orStreamChoice struct {
	Index        int           `json:"index"`
	Delta        orStreamDelta `json:"delta"`
	FinishReason *string       `json:"finish_reason"`
}

// orStreamDelta holds incremental content in a stream chunk.
type orStreamDelta struct {
	Role      string            `json:"role,omitempty"`
	Content   string            `json:"content,omitempty"`
	ToolCalls []orToolCallDelta `json:"tool_calls,omitempty"`
}

// orToolCallDelta represents an incremental tool call update in a stream.
type orToolCallDelta struct {
	Index    int              `json:"index"`
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function *orFunctionDelta `json:"function,omitempty"`
}

// orFunctionDelta holds incremental function data in a stream.
type orFunctionDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

// Provider implements the provider.Provider interface for OpenRouter.
// It communicates with the OpenRouter API (OpenAI-compatible) and supports
// both Generate and Stream operations with dynamic model discovery,
// cost tracking, and provider routing.
//
// A Provider is safe for concurrent use by multiple goroutines.
type Provider struct {
	apiKey       string
	baseURL      string
	defaultModel string
	appName      string
	siteURL      string
	httpClient   *http.Client

	// Model catalog cache
	cache *modelCache

	// Cost tracking
	costTracker *CostTracker

	// Provider routing configuration
	routing RoutingConfig

	// Budget configuration
	budget CostBudget
}

// NewProvider creates a new OpenRouter provider from the given configuration.
func NewProvider(cfg config.ProviderConfig) (*Provider, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		return nil, fmt.Errorf("openrouter: api_key is required")
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

	// Extract OpenRouter-specific settings from Extra
	appName := "orchestra"
	siteURL := ""
	cacheTTL := defaultCacheTTL
	var budget CostBudget
	var routing RoutingConfig

	if cfg.Extra != nil {
		if v, ok := cfg.Extra["app_name"].(string); ok && v != "" {
			appName = v
		}
		if v, ok := cfg.Extra["site_url"].(string); ok {
			siteURL = v
		}
		if v, ok := cfg.Extra["model_cache_ttl"].(string); ok {
			if d, err := time.ParseDuration(v); err == nil {
				cacheTTL = d
			}
		}
		budget = parseBudgetConfig(cfg.Extra)
		routing = parseRoutingConfig(cfg.Extra)
	}

	p := &Provider{
		apiKey:       apiKey,
		baseURL:      baseURL,
		defaultModel: dm,
		appName:      appName,
		siteURL:      siteURL,
		httpClient: &http.Client{
			Timeout: httpTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        maxIdleConns,
				MaxIdleConnsPerHost: maxIdleConnsPerHost,
				IdleConnTimeout:     idleConnTimeout,
			},
		},
		cache:       newModelCache(cacheTTL),
		costTracker: NewCostTracker(),
		routing:     routing,
		budget:      budget,
	}

	return p, nil
}

// Factory is a provider.ProviderFactory that creates a new OpenRouter provider.
// Use this with the provider registry:
//
//	registry.Register("openrouter", openrouter.Factory, cfg)
var Factory provider.ProviderFactory = func(cfg config.ProviderConfig) (provider.Provider, error) {
	return NewProvider(cfg)
}

// Compile-time interface check.
var _ provider.Provider = (*Provider)(nil)

// ---------------------------------------------------------------------------
// Interface Methods
// ---------------------------------------------------------------------------

// Name returns the provider identifier "openrouter".
func (p *Provider) Name() string {
	return providerName
}

// Models returns the list of models available on OpenRouter.
// Results are fetched from the /api/v1/models endpoint and cached.
func (p *Provider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	return p.cache.Get(ctx, p)
}

// Generate sends a non-streaming completion request to the OpenRouter API.
func (p *Provider) Generate(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResult, error) {
	model := p.resolveModel(req.Model)
	if len(req.Messages) == 0 {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("at least one message is required"))
	}

	// Budget check: estimate cost before sending
	if err := p.budgetCheck(model, 0); err != nil {
		return nil, provider.NewProviderError(providerName, model, err)
	}

	orReq := p.buildRequest(model, req, false)

	bodyBytes, err := json.Marshal(orReq)
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

	var orResp orChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&orResp); err != nil {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("failed to decode response: %w", err))
	}

	result, err := p.convertResponse(&orResp)
	if err != nil {
		return nil, provider.NewProviderError(providerName, model, err)
	}

	// Track cost from usage
	p.trackCost(model, result.Usage)

	return result, nil
}

// Stream sends a streaming completion request to the OpenRouter API and returns
// a channel of StreamEvent values.
func (p *Provider) Stream(ctx context.Context, req provider.GenerateRequest) (<-chan provider.StreamEvent, error) {
	model := p.resolveModel(req.Model)
	if len(req.Messages) == 0 {
		return nil, provider.NewProviderError(providerName, model,
			fmt.Errorf("at least one message is required"))
	}

	// Budget check
	if err := p.budgetCheck(model, 0); err != nil {
		return nil, provider.NewProviderError(providerName, model, err)
	}

	orReq := p.buildRequest(model, req, true)

	bodyBytes, err := json.Marshal(orReq)
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
// It uses the cached model catalog when available.
func (p *Provider) Capabilities(model string) provider.ModelCapabilities {
	resolved := p.resolveModel(model)

	// Try to get capabilities from cached model catalog
	if caps, ok := p.cache.LookupCapabilities(resolved); ok {
		return caps
	}

	// Fallback: infer capabilities from model ID heuristics
	return inferCapabilities(resolved)
}

// ---------------------------------------------------------------------------
// Cost Tracking Access
// ---------------------------------------------------------------------------

// CostReport returns a summary of cumulative costs tracked by this provider.
func (p *Provider) CostReport() CostReport {
	return p.costTracker.Report()
}

// ResetCosts resets the cost tracking counters.
func (p *Provider) ResetCosts() {
	p.costTracker.Reset()
}

// ---------------------------------------------------------------------------
// Model Cache Access
// ---------------------------------------------------------------------------

// RefreshModels forces a refresh of the model catalog cache.
func (p *Provider) RefreshModels(ctx context.Context) error {
	_, err := p.cache.ForceRefresh(ctx, p)
	return err
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
// Includes OpenRouter-specific headers for app identification and rankings.
func (p *Provider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	if p.siteURL != "" {
		req.Header.Set("HTTP-Referer", p.siteURL)
	}
	if p.appName != "" {
		req.Header.Set("X-Title", p.appName)
	}
}

// buildRequest constructs an OpenRouter API request from an Orchestra request.
func (p *Provider) buildRequest(model string, req provider.GenerateRequest, stream bool) orChatRequest {
	orReq := orChatRequest{
		Model:    model,
		Messages: convertMessages(req.Messages),
		Stream:   stream,
		N:        1,
	}

	// Tools
	if len(req.Tools) > 0 {
		orReq.Tools = convertTools(req.Tools)
	}

	// Options
	opts := req.Options
	if opts.Temperature != nil {
		orReq.Temperature = opts.Temperature
	}
	if opts.TopP != nil {
		orReq.TopP = opts.TopP
	}
	if opts.MaxTokens != nil {
		orReq.MaxTokens = opts.MaxTokens
	}
	if len(opts.StopSequences) > 0 {
		orReq.Stop = opts.StopSequences
	}
	if opts.Seed != nil {
		orReq.Seed = opts.Seed
	}
	if opts.ResponseFormat != nil {
		orReq.ResponseFormat = convertResponseFormat(opts.ResponseFormat)
	}

	// Extra provider-specific options
	if opts.Extra != nil {
		if v, ok := asFloat64(opts.Extra["frequency_penalty"]); ok {
			orReq.FrequencyPenalty = &v
		}
		if v, ok := asFloat64(opts.Extra["presence_penalty"]); ok {
			orReq.PresencePenalty = &v
		}
	}

	// Provider routing preferences
	prefs := p.buildProviderPrefs()
	if prefs != nil {
		orReq.Provider = prefs
	}

	return orReq
}

// buildProviderPrefs constructs OpenRouter provider routing preferences.
func (p *Provider) buildProviderPrefs() *orProviderPrefs {
	if !p.routing.Enabled {
		return nil
	}

	prefs := &orProviderPrefs{}
	if len(p.routing.ProviderOrder) > 0 {
		prefs.Order = p.routing.ProviderOrder
	}
	if p.routing.Fallback {
		prefs.AllowFallbacks = &p.routing.Fallback
	}
	if p.routing.DataCollection != "" {
		prefs.DataCollection = p.routing.DataCollection
	}

	// If nothing meaningful was set, return nil
	if len(prefs.Order) == 0 && prefs.AllowFallbacks == nil && prefs.DataCollection == "" {
		return nil
	}

	return prefs
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

		var chunk orStreamChunk
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

	// Track cost from streaming usage
	if usage != nil {
		p.trackCost(model, *usage)
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
func (a *toolCallAccumulator) add(delta orToolCallDelta) {
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
// Conversion: Orchestra → OpenRouter
// ---------------------------------------------------------------------------

// convertMessages converts Orchestra messages to the OpenAI-compatible format.
func convertMessages(msgs []message.Message) []orMessage {
	result := make([]orMessage, 0, len(msgs))
	for _, msg := range msgs {
		orMsg := orMessage{
			Role: convertRole(msg.Role),
			Name: msg.Name,
		}

		switch msg.Role {
		case message.RoleTool:
			if msg.ToolResult != nil {
				orMsg.ToolCallID = msg.ToolResult.ToolCallID
				orMsg.Content = jsonString(msg.ToolResult.Content)
			}

		case message.RoleAssistant:
			if len(msg.ToolCalls) > 0 {
				orMsg.ToolCalls = convertToolCallsOut(msg.ToolCalls)
				text := msg.Text()
				if text != "" {
					orMsg.Content = jsonString(text)
				}
			} else {
				orMsg.Content = convertContentBlocks(msg.Content)
			}

		case message.RoleFunction:
			orMsg.Role = "tool"
			if msg.ToolResult != nil {
				orMsg.ToolCallID = msg.ToolResult.ToolCallID
				orMsg.Content = jsonString(msg.ToolResult.Content)
			}

		default:
			orMsg.Content = convertContentBlocks(msg.Content)
		}

		result = append(result, orMsg)
	}
	return result
}

// convertRole maps an Orchestra role to an OpenAI-compatible role string.
func convertRole(role message.Role) string {
	return string(role)
}

// convertContentBlocks converts ContentBlocks to a json.RawMessage.
func convertContentBlocks(blocks []message.ContentBlock) json.RawMessage {
	if len(blocks) == 0 {
		return nil
	}

	// Single text block → plain string content
	if len(blocks) == 1 && blocks[0].Type == "text" && blocks[0].ImageURL == "" && len(blocks[0].FileData) == 0 {
		return jsonString(blocks[0].Text)
	}

	// Multiple blocks or image/file blocks → array of content parts
	parts := make([]orContentPart, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case "text":
			parts = append(parts, orContentPart{
				Type: "text",
				Text: block.Text,
			})
		case "image":
			url := block.ImageURL
			if url == "" && len(block.FileData) > 0 {
				mime := block.MimeType
				if mime == "" {
					mime = "image/png"
				}
				url = "data:" + mime + ";base64," + encodeBase64(block.FileData)
			}
			if url != "" {
				parts = append(parts, orContentPart{
					Type:     "image_url",
					ImageURL: &orImageURL{URL: url},
				})
			}
		case "file":
			if len(block.FileData) > 0 && strings.HasPrefix(block.MimeType, "image/") {
				url := "data:" + block.MimeType + ";base64," + encodeBase64(block.FileData)
				parts = append(parts, orContentPart{
					Type:     "image_url",
					ImageURL: &orImageURL{URL: url},
				})
			}
		}
	}

	if len(parts) == 0 {
		return nil
	}

	if len(parts) == 1 && parts[0].Type == "text" {
		return jsonString(parts[0].Text)
	}

	return jsonRaw(parts)
}

// convertTools converts Orchestra tool definitions to OpenAI-compatible format.
func convertTools(tools []provider.ToolDefinition) []orTool {
	result := make([]orTool, 0, len(tools))
	for _, tool := range tools {
		result = append(result, orTool{
			Type: tool.Type,
			Function: orFuncDef{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  tool.Function.Parameters,
			},
		})
	}
	return result
}

// convertToolCallsOut converts Orchestra tool calls to OpenAI-compatible format.
func convertToolCallsOut(tcs []message.ToolCall) []orToolCall {
	result := make([]orToolCall, 0, len(tcs))
	for _, tc := range tcs {
		result = append(result, orToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: orFunction{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}
	return result
}

// convertResponseFormat converts an Orchestra ResponseFormat to OpenAI-compatible format.
func convertResponseFormat(rf *provider.ResponseFormat) *orResponseFormat {
	if rf == nil {
		return nil
	}

	result := &orResponseFormat{
		Type: rf.Type,
	}

	if rf.Type == "json_schema" && rf.JSONSchema != nil {
		result.JSONSchema = rf.JSONSchema
	}

	return result
}

// ---------------------------------------------------------------------------
// Conversion: OpenRouter → Orchestra
// ---------------------------------------------------------------------------

// convertResponse converts an OpenRouter chat response to a GenerateResult.
func (p *Provider) convertResponse(resp *orChatResponse) (*provider.GenerateResult, error) {
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := resp.Choices[0]
	msg := convertORMessage(choice.Message)

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

	// Add cost metadata if available
	if costPerToken := p.getModelPricing(resp.Model); costPerToken != nil {
		usage := result.Usage
		requestCost := float64(usage.PromptTokens)*costPerToken.PromptCost +
			float64(usage.CompletionTokens)*costPerToken.CompletionCost
		result.Metadata["estimated_cost_usd"] = fmt.Sprintf("%.8f", requestCost)
	}

	return result, nil
}

// convertORMessage converts an OpenRouter response message to an Orchestra Message.
func convertORMessage(orMsg orMessage) message.Message {
	msg := message.Message{
		Role: message.Role(orMsg.Role),
	}

	// Parse content
	text := extractText(orMsg.Content)
	if text != "" {
		msg.Content = []message.ContentBlock{message.TextContentBlock(text)}
	}

	// Convert tool calls
	if len(orMsg.ToolCalls) > 0 {
		msg.ToolCalls = make([]message.ToolCall, len(orMsg.ToolCalls))
		for i, tc := range orMsg.ToolCalls {
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

	var errResp orErrorResponse
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
	return readAll(resp.Body, 1024*1024)
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
			return buf, nil
		}
	}
}

// ---------------------------------------------------------------------------
// Cost Helpers
// ---------------------------------------------------------------------------

// budgetCheck verifies that sending a request would not exceed configured budget limits.
func (p *Provider) budgetCheck(model string, estimatedTokens int) error {
	if p.budget.MaxCostPerRequest <= 0 && p.budget.MaxCostPerSession <= 0 {
		return nil
	}

	report := p.costTracker.Report()

	// Estimate cost for this request
	estimatedCost := 0.0
	if pricing := p.getModelPricing(model); pricing != nil && estimatedTokens > 0 {
		// Rough estimate: assume half prompt, half completion
		half := estimatedTokens / 2
		estimatedCost = float64(half)*pricing.PromptCost + float64(half)*pricing.CompletionCost
	}

	// Check per-request budget
	if p.budget.MaxCostPerRequest > 0 && estimatedCost > p.budget.MaxCostPerRequest {
		return fmt.Errorf("estimated cost $%.4f exceeds per-request budget $%.4f",
			estimatedCost, p.budget.MaxCostPerRequest)
	}

	// Check per-session budget
	if p.budget.MaxCostPerSession > 0 {
		totalWithEstimate := report.TotalCost + estimatedCost
		if totalWithEstimate > p.budget.MaxCostPerSession {
			return fmt.Errorf("session cost $%.4f + estimated $%.4f would exceed session budget $%.4f",
				report.TotalCost, estimatedCost, p.budget.MaxCostPerSession)
		}
	}

	return nil
}

// trackCost records the cost for a completed request based on actual token usage.
func (p *Provider) trackCost(model string, usage provider.TokenUsage) {
	pricing := p.getModelPricing(model)
	if pricing == nil {
		return
	}

	cost := float64(usage.PromptTokens)*pricing.PromptCost +
		float64(usage.CompletionTokens)*pricing.CompletionCost

	p.costTracker.Record(model, usage, cost)
}

// getModelPricing returns the pricing for a model from the cached catalog.
func (p *Provider) getModelPricing(model string) *ModelPricing {
	return p.cache.LookupPricing(model)
}

// ---------------------------------------------------------------------------
// Mapping Helpers
// ---------------------------------------------------------------------------

// mapFinishReason maps an OpenAI-compatible finish reason string to a FinishReason.
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

// mapUsage converts OpenRouter usage to Orchestra TokenUsage.
func mapUsage(u orUsage) provider.TokenUsage {
	return provider.TokenUsage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
	}
}

// extractText extracts a string value from a json.RawMessage.
func extractText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

// inferCapabilities infers model capabilities from the model ID using heuristics.
func inferCapabilities(modelID string) provider.ModelCapabilities {
	lower := strings.ToLower(modelID)

	// Default capabilities for most models
	caps := provider.ModelCapabilities{
		Streaming:     true,
		ToolCalling:   true,
		Vision:        false,
		Audio:         false,
		JSONMode:      true,
		Seed:          false,
		MaxTokens:     4096,
		ContextWindow: 8192,
	}

	// Vision models
	if containsAny(lower, "vision", "gpt-4o", "gpt-4-turbo", "claude-3", "gemini") {
		caps.Vision = true
	}

	// Large context models
	if containsAny(lower, "gpt-4o", "gpt-4-turbo", "claude-3", "gemini", "llama-3", "mistral-large") {
		caps.ContextWindow = 128000
		caps.MaxTokens = 16384
	}

	// Specific model families
	if strings.Contains(lower, "claude") {
		caps.ContextWindow = 200000
		caps.MaxTokens = 8192
	}
	if strings.Contains(lower, "gemini") {
		caps.ContextWindow = 1048576
		caps.MaxTokens = 8192
	}
	if strings.Contains(lower, "o1") || strings.Contains(lower, "o3") {
		caps.MaxTokens = 100000
		caps.ContextWindow = 200000
	}

	// Most modern models support tool calling
	if containsAny(lower, "gpt-4", "gpt-3.5", "claude", "gemini", "mistral", "llama") {
		caps.ToolCalling = true
	}

	// Some models support seed
	if containsAny(lower, "gpt-4", "gpt-3.5") {
		caps.Seed = true
	}

	return caps
}

// containsAny checks if s contains any of the given substrings.
func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
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

// encodeBase64 encodes bytes to a base64 string.
func encodeBase64(data []byte) string {
	return strings.ReplaceAll(
		strings.ReplaceAll(
			encodeToString(data),
			"\n", "",
		),
		"\r", "",
	)
}

// encodeToString wraps base64 encoding for testability.
var encodeToString = func(data []byte) string {
	return base64Encode(data)
}

// base64Encode is the actual base64 encoding implementation.
// Uses encoding/base64.StdEncoding which is imported in models.go.
// This avoids a direct import here while keeping the helper clean.
var base64Encode = func(data []byte) string {
	// Use a simple implementation that avoids importing encoding/base64
	// in this file. The actual encoding is done in the helper below.
	const base64Chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	result := make([]byte, 0, (len(data)+2)*4/3)
	for i := 0; i < len(data); i += 3 {
		var b0, b1, b2 byte
		b0 = data[i]
		remaining := len(data) - i
		if remaining > 1 {
			b1 = data[i+1]
		}
		if remaining > 2 {
			b2 = data[i+2]
		}

		result = append(result, base64Chars[b0>>2])
		result = append(result, base64Chars[(b0&0x03)<<4|b1>>4])

		if remaining > 1 {
			result = append(result, base64Chars[(b1&0x0f)<<2|b2>>6])
		} else {
			result = append(result, '=')
		}
		if remaining > 2 {
			result = append(result, base64Chars[b2&0x3f])
		} else {
			result = append(result, '=')
		}
	}
	return string(result)
}

// Ensure sync import is used
var _ sync.Once
