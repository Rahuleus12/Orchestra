// Package llamacpp implements the llama.cpp local LLM provider for Orchestra.
//
// It talks to llama.cpp's llama-server, which exposes an OpenAI-compatible
// HTTP API (GET /v1/models, POST /v1/chat/completions) plus a lightweight
// GET /health endpoint. Request handling is delegated to the OpenAI provider
// (chat completions, streaming, JSON mode); this package adds llama.cpp's
// live model discovery and availability detection on top.
//
// By default the provider detects a llama-server on localhost:8080 (the
// llama-server default port). A custom endpoint can be set via cfg.BaseURL
// (e.g. "http://myhost:8080"); a trailing "/v1" is appended automatically
// when missing. No API key is required — llama-server ignores the
// Authorization header, but a placeholder is supplied because the OpenAI
// client treats an empty key as a configuration error.
//
// Register with the Orchestra provider registry:
//
//	registry.Register("llamacpp", llamacpp.Factory, cfg)
package llamacpp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/user/orchestra/internal/config"
	"github.com/user/orchestra/internal/provider"
	"github.com/user/orchestra/internal/provider/httpx"
	"github.com/user/orchestra/internal/provider/openai"
)

const (
	// defaultBaseURL is the llama-server default listen address. Kept as the
	// root URL (no /v1) so /health probing matches llama.cpp's own docs.
	defaultBaseURL = "http://localhost:8080"
	providerName   = "llamacpp"
	apiPrefix      = "/v1"
	healthPath     = "/health"
	// defaultModel is sent as the "model" field when a request doesn't
	// specify one. llama-server serves whichever model is loaded and
	// ignores this value.
	defaultModel = "local"
	// placeholderAPIKey satisfies the OpenAI client's non-empty-key
	// requirement; llama-server does not validate credentials.
	placeholderAPIKey = "llamacpp-local"
	detectTimeout     = 2 * time.Second
)

// Provider implements the provider.Provider interface for llama.cpp.
//
// Chat completions (Generate/Stream) and capability lookup are inherited
// from the OpenAI provider, pointed at the llama-server endpoint. This
// struct adds the llama.cpp provider identity and local-server discovery.
//
// A Provider is safe for concurrent use by multiple goroutines.
type Provider struct {
	*openai.Provider

	// baseURL is the server root, e.g. "http://localhost:8080".
	baseURL string
	// apiBaseURL is baseURL with the "/v1" API prefix, e.g.
	// "http://localhost:8080/v1".
	apiBaseURL string
	// httpClient is used for model listing and availability probes.
	httpClient *http.Client
}

// NewProvider creates a new llama.cpp provider from the given configuration.
// Unlike cloud providers, llama.cpp does not require an API key.
func NewProvider(cfg config.ProviderConfig) (*Provider, error) {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if !httpx.ValidateBaseURL(baseURL) {
		return nil, fmt.Errorf("llamacpp: invalid base_url %q", cfg.BaseURL)
	}

	// Normalize to the OpenAI-compatible API root so the embedded OpenAI
	// client can append its own /chat/completions path.
	apiBaseURL := baseURL
	if !strings.HasSuffix(apiBaseURL, apiPrefix) {
		apiBaseURL += apiPrefix
	}

	if cfg.APIKey == "" {
		cfg.APIKey = placeholderAPIKey
	}
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = defaultModel
	}
	cfg.BaseURL = apiBaseURL

	inner, err := openai.NewProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("llamacpp: %w", err)
	}

	return &Provider{
		Provider:   inner,
		baseURL:    baseURL,
		apiBaseURL: apiBaseURL,
		httpClient: httpx.NewClient(httpx.NewTransport(), httpx.DefaultRequestTimeout),
	}, nil
}

// Factory is a provider.ProviderFactory that creates a new llama.cpp provider.
// Use this with the provider registry:
//
//	registry.Register("llamacpp", llamacpp.Factory, cfg)
var Factory provider.ProviderFactory = func(cfg config.ProviderConfig) (provider.Provider, error) {
	return NewProvider(cfg)
}

// Compile-time interface check.
var _ provider.Provider = (*Provider)(nil)

// Name returns the provider identifier "llamacpp".
func (p *Provider) Name() string {
	return providerName
}

// Models returns the models served by the llama.cpp server, discovered live
// from its OpenAI-compatible /v1/models endpoint. llama-server typically
// reports the model currently loaded into memory.
func (p *Provider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.apiBaseURL+"/models", nil)
	if err != nil {
		return nil, provider.NewProviderError(providerName, "",
			fmt.Errorf("failed to create request: %w", err))
	}
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, provider.NewProviderError(providerName, "",
			fmt.Errorf("failed to connect to llama.cpp at %s: %w (is llama-server running?)", p.baseURL, err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, provider.NewProviderError(providerName, "",
			fmt.Errorf("failed to read models response: %w", err))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, provider.NewProviderError(providerName, "",
			fmt.Errorf("models endpoint returned %d: %s", resp.StatusCode, truncate(string(body), 300)))
	}

	var listResp struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &listResp); err != nil {
		return nil, provider.NewProviderError(providerName, "",
			fmt.Errorf("failed to decode models response: %w", err))
	}

	result := make([]provider.ModelInfo, 0, len(listResp.Data))
	for _, m := range listResp.Data {
		if m.ID == "" {
			continue
		}
		result = append(result, provider.ModelInfo{
			ID:          m.ID,
			Name:        m.ID,
			Description: "Model served by llama.cpp (llama-server)",
			Capabilities: provider.ModelCapabilities{
				Streaming:   true,
				ToolCalling: true,
				JSONMode:    true,
			},
			Metadata: map[string]any{
				"endpoint": p.baseURL,
				"owned_by": m.OwnedBy,
			},
		})
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Availability Detection
// ---------------------------------------------------------------------------

// IsAvailable checks if a llama.cpp server is reachable at the configured
// endpoint. It probes GET /health first; on older llama-server builds that
// lack the endpoint, it falls back to GET /v1/models. Returns nil if the
// server is up, or an error describing why it was not detected.
func (p *Provider) IsAvailable(ctx context.Context) error {
	detectCtx, cancel := context.WithTimeout(ctx, detectTimeout)
	defer cancel()

	if err := p.probe(detectCtx, p.baseURL+healthPath); err == nil {
		return nil
	}
	if err := p.probe(detectCtx, p.apiBaseURL+"/models"); err == nil {
		return nil
	}
	return fmt.Errorf("llamacpp: no llama.cpp server detected at %s (is llama-server running?)", p.baseURL)
}

func (p *Provider) probe(ctx context.Context, url string) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
