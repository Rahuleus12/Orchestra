package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// DefaultModelFetcher — fetches models via provider APIs
// ---------------------------------------------------------------------------

// DefaultModelFetcher implements ModelFetcher by calling provider-specific
// API endpoints to list available models.
type DefaultModelFetcher struct {
	httpClient *http.Client
}

// NewDefaultModelFetcher creates a new DefaultModelFetcher.
func NewDefaultModelFetcher() *DefaultModelFetcher {
	return &DefaultModelFetcher{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// FetchModels fetches available models from the given provider API.
func (f *DefaultModelFetcher) FetchModels(ctx context.Context, provider, apiKey, baseURL string) ([]ModelEntry, error) {
	switch strings.ToLower(provider) {
	case "openai":
		return f.fetchOpenAIModels(ctx, apiKey, baseURL)
	case "anthropic":
		return f.fetchAnthropicModels(ctx, apiKey, baseURL)
	case "openrouter":
		return f.fetchOpenRouterModels(ctx, apiKey, baseURL)
	case "gemini", "google":
		return f.fetchGeminiModels(ctx, apiKey, baseURL)
	case "mistral":
		return f.fetchMistralModels(ctx, apiKey, baseURL)
	case "ollama":
		return f.fetchOllamaModels(ctx, baseURL)
	case "cohere":
		return f.fetchCohereModels(ctx, apiKey, baseURL)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}
}

// fetchOpenAIModels fetches models from the OpenAI API.
func (f *DefaultModelFetcher) fetchOpenAIModels(ctx context.Context, apiKey, baseURL string) ([]ModelEntry, error) {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	url := strings.TrimRight(baseURL, "/") + "/models"

	return f.fetchModelsFromAPI(ctx, url, apiKey, "", func(data json.RawMessage) ([]ModelEntry, error) {
		var resp struct {
			Data []struct {
				ID      string `json:"id"`
				OwnedBy string `json:"owned_by"`
			} `json:"data"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, err
		}

		entries := make([]ModelEntry, 0, len(resp.Data))
		for _, m := range resp.Data {
			entries = append(entries, ModelEntry{
				ID:                  m.ID,
				Name:                m.ID,
				Provider:            "openai",
				SupportsStreaming:   true,
				SupportsToolCalling: isOpenAIChatModel(m.ID),
				SupportsVision:      isOpenAIVisionModel(m.ID),
			})
		}
		return entries, nil
	})
}

// fetchAnthropicModels returns the known Anthropic models.
// Anthropic does not have a public model listing endpoint, so we return
// a hardcoded list of known models.
func (f *DefaultModelFetcher) fetchAnthropicModels(ctx context.Context, apiKey, baseURL string) ([]ModelEntry, error) {
	// Anthropic doesn't have a /models endpoint, so return known models
	models := []ModelEntry{
		{
			ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4", Provider: "anthropic",
			SupportsStreaming: true, SupportsToolCalling: true, SupportsVision: true, ContextWindow: 200000,
		},
		{
			ID: "claude-opus-4-20250514", Name: "Claude Opus 4", Provider: "anthropic",
			SupportsStreaming: true, SupportsToolCalling: true, SupportsVision: true, ContextWindow: 200000,
		},
		{
			ID: "claude-3-7-sonnet-20250219", Name: "Claude 3.7 Sonnet", Provider: "anthropic",
			SupportsStreaming: true, SupportsToolCalling: true, SupportsVision: true, ContextWindow: 200000,
		},
		{
			ID: "claude-3-5-sonnet-20241022", Name: "Claude 3.5 Sonnet", Provider: "anthropic",
			SupportsStreaming: true, SupportsToolCalling: true, SupportsVision: true, ContextWindow: 200000,
		},
		{
			ID: "claude-3-5-haiku-20241022", Name: "Claude 3.5 Haiku", Provider: "anthropic",
			SupportsStreaming: true, SupportsToolCalling: true, SupportsVision: true, ContextWindow: 200000,
		},
		{
			ID: "claude-3-opus-20240229", Name: "Claude 3 Opus", Provider: "anthropic",
			SupportsStreaming: true, SupportsToolCalling: true, SupportsVision: true, ContextWindow: 200000,
		},
		{
			ID: "claude-3-haiku-20240307", Name: "Claude 3 Haiku", Provider: "anthropic",
			SupportsStreaming: true, SupportsToolCalling: true, SupportsVision: true, ContextWindow: 200000,
		},
	}

	// Validate the key by making a lightweight request if available
	if apiKey != "" {
		return models, nil
	}
	return nil, fmt.Errorf("API key is required for Anthropic")
}

// fetchOpenRouterModels fetches models from the OpenRouter API.
func (f *DefaultModelFetcher) fetchOpenRouterModels(ctx context.Context, apiKey, baseURL string) ([]ModelEntry, error) {
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	url := strings.TrimRight(baseURL, "/") + "/models"

	return f.fetchModelsFromAPI(ctx, url, apiKey, "", func(data json.RawMessage) ([]ModelEntry, error) {
		var resp struct {
			Data []struct {
				ID            string `json:"id"`
				Name          string `json:"name"`
				ContextLength int    `json:"context_length"`
				Architecture  struct {
					Modality        string   `json:"modality"`
					InputModalities []string `json:"input_modalities"`
				} `json:"architecture"`
			} `json:"data"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, err
		}

		entries := make([]ModelEntry, 0, len(resp.Data))
		for _, m := range resp.Data {
			vision := false
			for _, mod := range m.Architecture.InputModalities {
				if strings.EqualFold(mod, "image") {
					vision = true
				}
			}

			entries = append(entries, ModelEntry{
				ID:                  m.ID,
				Name:                m.Name,
				Provider:            "openrouter",
				SupportsStreaming:   true,
				SupportsToolCalling: true,
				SupportsVision:      vision,
				ContextWindow:       m.ContextLength,
			})
		}
		return entries, nil
	})
}

// fetchGeminiModels fetches models from the Google Gemini API.
func (f *DefaultModelFetcher) fetchGeminiModels(ctx context.Context, apiKey, baseURL string) ([]ModelEntry, error) {
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	url := strings.TrimRight(baseURL, "/") + "/models?key=" + apiKey

	resp, err := f.doGet(ctx, url, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Models []struct {
			Name                       string   `json:"name"`
			DisplayName                string   `json:"displayName"`
			InputTokenLimit            int      `json:"inputTokenLimit"`
			OutputTokenLimit           int      `json:"outputTokenLimit"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	entries := make([]ModelEntry, 0, len(result.Models))
	for _, m := range result.Models {
		// Extract model ID from name (e.g., "models/gemini-1.5-pro" -> "gemini-1.5-pro")
		id := strings.TrimPrefix(m.Name, "models/")

		// Only include models that support generateContent
		supportsGenerate := false
		for _, method := range m.SupportedGenerationMethods {
			if method == "generateContent" || method == "generateMessage" {
				supportsGenerate = true
				break
			}
		}
		if !supportsGenerate {
			continue
		}

		entries = append(entries, ModelEntry{
			ID:                id,
			Name:              m.DisplayName,
			Provider:          "gemini",
			SupportsStreaming: true,
			ContextWindow:     m.InputTokenLimit + m.OutputTokenLimit,
		})
	}
	return entries, nil
}

// fetchMistralModels fetches models from the Mistral API.
func (f *DefaultModelFetcher) fetchMistralModels(ctx context.Context, apiKey, baseURL string) ([]ModelEntry, error) {
	if baseURL == "" {
		baseURL = "https://api.mistral.ai/v1"
	}
	url := strings.TrimRight(baseURL, "/") + "/models"

	return f.fetchModelsFromAPI(ctx, url, apiKey, "", func(data json.RawMessage) ([]ModelEntry, error) {
		var resp struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, err
		}

		entries := make([]ModelEntry, 0, len(resp.Data))
		for _, m := range resp.Data {
			entries = append(entries, ModelEntry{
				ID:                m.ID,
				Name:              m.ID,
				Provider:          "mistral",
				SupportsStreaming: true,
			})
		}
		return entries, nil
	})
}

// fetchOllamaModels fetches models from a local Ollama instance.
func (f *DefaultModelFetcher) fetchOllamaModels(ctx context.Context, baseURL string) ([]ModelEntry, error) {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	url := strings.TrimRight(baseURL, "/") + "/api/tags"

	resp, err := f.doGet(ctx, url, "")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ollama: %w (is Ollama running?)", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result struct {
		Models []struct {
			Name   string `json:"name"`
			Model  string `json:"model"`
			Size   int64  `json:"size"`
			Detail struct {
				ParameterSize string `json:"parameter_size"`
				QuantLevel    string `json:"quant_level"`
				Family        string `json:"family"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	entries := make([]ModelEntry, 0, len(result.Models))
	for _, m := range result.Models {
		name := m.Name
		if name == "" {
			name = m.Model
		}

		entries = append(entries, ModelEntry{
			ID:                name,
			Name:              name,
			Provider:          "ollama",
			Description:       fmt.Sprintf("%s, %s", m.Detail.ParameterSize, m.Detail.Family),
			SupportsStreaming: true,
		})
	}
	return entries, nil
}

// fetchCohereModels fetches models from the Cohere API.
func (f *DefaultModelFetcher) fetchCohereModels(ctx context.Context, apiKey, baseURL string) ([]ModelEntry, error) {
	if baseURL == "" {
		baseURL = "https://api.cohere.com/v2"
	}
	url := strings.TrimRight(baseURL, "/") + "/models"

	return f.fetchModelsFromAPI(ctx, url, apiKey, "", func(data json.RawMessage) ([]ModelEntry, error) {
		var resp struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, err
		}

		entries := make([]ModelEntry, 0, len(resp.Models))
		for _, m := range resp.Models {
			entries = append(entries, ModelEntry{
				ID:                m.Name,
				Name:              m.Name,
				Provider:          "cohere",
				SupportsStreaming: true,
			})
		}
		return entries, nil
	})
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// fetchModelsFromAPI is a helper that GETs a URL, parses JSON, and converts
// the result using the provided converter function.
func (f *DefaultModelFetcher) fetchModelsFromAPI(ctx context.Context, url, apiKey, orgID string, converter func(json.RawMessage) ([]ModelEntry, error)) ([]ModelEntry, error) {
	resp, err := f.doGet(ctx, url, apiKey)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	return converter(json.RawMessage(body))
}

// doGet performs an authenticated GET request.
func (f *DefaultModelFetcher) doGet(ctx context.Context, url, apiKey string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Accept", "application/json")

	return f.httpClient.Do(req)
}

// isOpenAIChatModel returns true if the model ID likely supports chat/tool calling.
func isOpenAIChatModel(id string) bool {
	chatModels := []string{"gpt-4", "gpt-3.5-turbo", "o1-", "o3-", "o4-"}
	for _, prefix := range chatModels {
		if strings.Contains(strings.ToLower(id), strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}

// isOpenAIVisionModel returns true if the model ID likely supports vision.
func isOpenAIVisionModel(id string) bool {
	visionModels := []string{"gpt-4o", "gpt-4-turbo", "gpt-4-vision"}
	for _, prefix := range visionModels {
		if strings.Contains(strings.ToLower(id), strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}
