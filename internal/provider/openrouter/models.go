package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/user/orchestra/internal/provider"
)

// ---------------------------------------------------------------------------
// OpenRouter Model Catalog Types
// ---------------------------------------------------------------------------

// orModelResponse represents the response from GET /api/v1/models.
type orModelResponse struct {
	Data []orModel `json:"data"`
}

// orModel represents a single model in the OpenRouter catalog.
type orModel struct {
	ID               string         `json:"id"`   // e.g., "openai/gpt-4o"
	Name             string         `json:"name"` // e.g., "OpenAI: GPT-4o"
	Description      string         `json:"description,omitempty"`
	ContextLength    int            `json:"context_length"`
	Pricing          orModelPricing `json:"pricing"`
	TopProvider      orProviderInfo `json:"top_provider,omitempty"`
	PerRequestLimits *orLimits      `json:"per_request_limits,omitempty"`
	Architecture     orArchitecture `json:"architecture,omitempty"`
	Moderated        bool           `json:"moderated"`
	Created          int64          `json:"created"`
}

// orModelPricing holds per-token pricing for a model.
type orModelPricing struct {
	Prompt     string `json:"prompt"`     // cost per token (USD string, e.g., "0.000005")
	Completion string `json:"completion"` // cost per token (USD string)
	Request    string `json:"request"`    // cost per request (USD string)
	Image      string `json:"image"`      // cost per image (USD string)
}

// orProviderInfo holds top provider metadata.
type orProviderInfo struct {
	MaxCompletionTokens int  `json:"max_completion_tokens,omitempty"`
	IsModerated         bool `json:"is_moderated"`
}

// orArchitecture holds model architecture details.
type orArchitecture struct {
	Modality         string   `json:"modality"`                    // e.g., "text->text"
	InputModalities  []string `json:"input_modalities,omitempty"`  // e.g., ["text", "image"]
	OutputModalities []string `json:"output_modalities,omitempty"` // e.g., ["text"]
	Tokenizer        string   `json:"tokenizer"`                   // e.g., "o200k_base"
}

// orLimits holds per-request token limits.
type orLimits struct {
	MaxTokens int `json:"max_tokens,omitempty"`
}

// ---------------------------------------------------------------------------
// ModelPricing — parsed pricing for a model
// ---------------------------------------------------------------------------

// ModelPricing holds parsed float pricing for a model.
type ModelPricing struct {
	// PromptCost is the cost per prompt token in USD.
	PromptCost float64
	// CompletionCost is the cost per completion token in USD.
	CompletionCost float64
	// RequestCost is the cost per request in USD.
	RequestCost float64
	// ImageCost is the cost per image in USD.
	ImageCost float64
}

// ---------------------------------------------------------------------------
// modelCache — TTL-based cache for the OpenRouter model catalog
// ---------------------------------------------------------------------------

// modelCache caches the OpenRouter model catalog with TTL-based expiration.
// It is safe for concurrent use.
type modelCache struct {
	mu      sync.RWMutex
	models  []provider.ModelInfo
	pricing map[string]*ModelPricing // model ID → parsed pricing
	expiry  time.Time
	ttl     time.Duration
}

// newModelCache creates a new model cache with the given TTL.
func newModelCache(ttl time.Duration) *modelCache {
	return &modelCache{
		ttl:     ttl,
		pricing: make(map[string]*ModelPricing),
	}
}

// Get returns the cached model list, fetching from the API if the cache
// is expired or empty.
func (c *modelCache) Get(ctx context.Context, p *Provider) ([]provider.ModelInfo, error) {
	c.mu.RLock()
	if c.models != nil && time.Now().Before(c.expiry) {
		result := make([]provider.ModelInfo, len(c.models))
		copy(result, c.models)
		c.mu.RUnlock()
		return result, nil
	}
	c.mu.RUnlock()

	return c.ForceRefresh(ctx, p)
}

// ForceRefresh fetches the model catalog from the OpenRouter API,
// bypassing the cache.
func (c *modelCache) ForceRefresh(ctx context.Context, p *Provider) ([]provider.ModelInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	models, err := fetchModels(ctx, p)
	if err != nil {
		// Return stale cache if available
		if c.models != nil {
			result := make([]provider.ModelInfo, len(c.models))
			copy(result, c.models)
			return result, nil
		}
		return nil, err
	}

	c.models = models
	c.expiry = time.Now().Add(c.ttl)

	result := make([]provider.ModelInfo, len(c.models))
	copy(result, c.models)
	return result, nil
}

// LookupCapabilities returns capabilities for a model from the cached catalog.
// Returns false if the model is not found or the cache is empty.
func (c *modelCache) LookupCapabilities(modelID string) (provider.ModelCapabilities, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, m := range c.models {
		if m.ID == modelID {
			return m.Capabilities, true
		}
	}
	return provider.ModelCapabilities{}, false
}

// LookupPricing returns pricing for a model from the cached catalog.
func (c *modelCache) LookupPricing(modelID string) *ModelPricing {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.pricing[modelID]
}

// fetchModels fetches the model catalog from the OpenRouter /api/v1/models endpoint.
// Caller must hold the cache write lock.
func fetchModels(ctx context.Context, p *Provider) ([]provider.ModelInfo, error) {
	url := p.baseURL + modelsEndpoint

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create models request: %w", err)
	}

	// Set auth headers (no Content-Type needed for GET)
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch models: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var orResp orModelResponse
	if err := json.NewDecoder(resp.Body).Decode(&orResp); err != nil {
		return nil, fmt.Errorf("failed to decode models response: %w", err)
	}

	// Convert to Orchestra ModelInfo and build pricing lookup
	models := make([]provider.ModelInfo, 0, len(orResp.Data))
	pricing := make(map[string]*ModelPricing, len(orResp.Data))

	for _, m := range orResp.Data {
		info := convertModel(m)
		models = append(models, info)

		// Parse and cache pricing
		if p := parseModelPricing(m.Pricing); p != nil {
			pricing[m.ID] = p
		}
	}

	// Update pricing cache
	p.cache.pricing = pricing

	return models, nil
}

// convertModel maps an OpenRouter model to an Orchestra ModelInfo.
func convertModel(m orModel) provider.ModelInfo {
	caps := provider.ModelCapabilities{
		Streaming:     true, // OpenRouter streams all models
		ToolCalling:   inferToolSupport(m),
		Vision:        hasModality(m, "image"),
		Audio:         hasModality(m, "audio"),
		JSONMode:      true, // Most models support JSON
		Seed:          false,
		MaxTokens:     m.TopProvider.MaxCompletionTokens,
		ContextWindow: m.ContextLength,
	}

	// If MaxTokens is 0, try per-request limits
	if caps.MaxTokens == 0 && m.PerRequestLimits != nil {
		caps.MaxTokens = m.PerRequestLimits.MaxTokens
	}

	metadata := map[string]any{
		"pricing_prompt":     m.Pricing.Prompt,
		"pricing_completion": m.Pricing.Completion,
		"pricing_request":    m.Pricing.Request,
		"pricing_image":      m.Pricing.Image,
		"modality":           m.Architecture.Modality,
		"moderated":          m.Moderated,
		"created":            m.Created,
	}

	if len(m.Architecture.InputModalities) > 0 {
		metadata["input_modalities"] = m.Architecture.InputModalities
	}
	if len(m.Architecture.OutputModalities) > 0 {
		metadata["output_modalities"] = m.Architecture.OutputModalities
	}
	if m.Architecture.Tokenizer != "" {
		metadata["tokenizer"] = m.Architecture.Tokenizer
	}

	return provider.ModelInfo{
		ID:           m.ID,
		Name:         m.Name,
		Description:  m.Description,
		Capabilities: caps,
		Metadata:     metadata,
	}
}

// inferToolSupport uses heuristics to determine if a model supports tool calling.
func inferToolSupport(m orModel) bool {
	id := strings.ToLower(m.ID)
	name := strings.ToLower(m.Name)

	// Models known to NOT support tool calling
	noToolPatterns := []string{
		"dall-e", "stable-diffusion", "midjourney",
		"whisper", "tts", "embedding",
	}
	for _, pattern := range noToolPatterns {
		if strings.Contains(id, pattern) || strings.Contains(name, pattern) {
			return false
		}
	}

	// Check architecture modality — only text->text supports tools typically
	modality := strings.ToLower(m.Architecture.Modality)
	if modality != "" && !strings.Contains(modality, "text->text") {
		return false
	}

	// Most chat/instruction models support tool calling via OpenRouter
	return true
}

// hasModality checks if the model supports a given input modality.
func hasModality(m orModel, modality string) bool {
	for _, im := range m.Architecture.InputModalities {
		if strings.EqualFold(im, modality) {
			return true
		}
	}
	// Also check modality string as a fallback
	modalityStr := strings.ToLower(m.Architecture.Modality)
	if modality == "image" && strings.Contains(modalityStr, "image") {
		return true
	}
	if modality == "audio" && strings.Contains(modalityStr, "audio") {
		return true
	}
	return false
}

// parseModelPricing parses OpenRouter pricing strings into float64 values.
func parseModelPricing(p orModelPricing) *ModelPricing {
	promptCost := parseFloat(p.Prompt)
	completionCost := parseFloat(p.Completion)
	requestCost := parseFloat(p.Request)
	imageCost := parseFloat(p.Image)

	// Only return pricing if at least one field is non-zero
	if promptCost == 0 && completionCost == 0 && requestCost == 0 && imageCost == 0 {
		return nil
	}

	return &ModelPricing{
		PromptCost:     promptCost,
		CompletionCost: completionCost,
		RequestCost:    requestCost,
		ImageCost:      imageCost,
	}
}

// parseFloat parses a string to float64, returning 0 on error.
func parseFloat(s string) float64 {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}
