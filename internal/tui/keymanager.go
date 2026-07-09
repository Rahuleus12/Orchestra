package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ProviderKey holds an API key and its associated provider information.
type ProviderKey struct {
	// Provider is the provider name (e.g., "openai", "anthropic", "openrouter").
	Provider string `json:"provider"`

	// APIKey is the API key value.
	APIKey string `json:"api_key"`

	// BaseURL is an optional custom base URL for the provider.
	BaseURL string `json:"base_url,omitempty"`

	// OrganizationID is an optional organization identifier.
	OrganizationID string `json:"organization_id,omitempty"`

	// AddedAt tracks when the key was added.
	AddedAt string `json:"added_at,omitempty"`

	// IsValid indicates if the key has been validated.
	IsValid *bool `json:"is_valid,omitempty"`

	// LastValidatedAt tracks when the key was last validated.
	LastValidatedAt string `json:"last_validated_at,omitempty"`
}

// KeyManager manages API keys for providers. Keys are persisted to disk
// in the Orchestra config directory.
type KeyManager struct {
	mu   sync.RWMutex
	keys map[string]*ProviderKey // provider name -> key
	dir  string                  // storage directory
}

// NewKeyManager creates a new KeyManager. Keys are loaded from the given
// directory. If the directory does not exist, it is created.
//
// In addition to keys persisted to disk, keys are also discovered from the
// standard provider environment variables (OPENAI_API_KEY, ANTHROPIC_API_KEY,
// etc.) for any provider that does not already have a stored key. Env-var
// keys are persisted so subsequent loads don't depend on the environment.
func NewKeyManager(dir string) (*KeyManager, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create key directory: %w", err)
	}

	km := &KeyManager{
		keys: make(map[string]*ProviderKey),
		dir:  dir,
	}

	if err := km.load(); err != nil {
		// Non-fatal: start with empty keys
	}

	// Pick up any provider keys exposed via environment variables that aren't
	// already stored on disk. This keeps the TUI in sync with the documented
	// way of configuring providers (see the `orchestra` help text).
	km.loadFromEnv()

	return km, nil
}

// AddKey adds or updates an API key for a provider.
func (km *KeyManager) AddKey(provider, apiKey, baseURL, orgID string) error {
	km.mu.Lock()
	defer km.mu.Unlock()

	provider = normalizeProvider(provider)

	km.keys[provider] = &ProviderKey{
		Provider:       provider,
		APIKey:         apiKey,
		BaseURL:        baseURL,
		OrganizationID: orgID,
		AddedAt:        currentTimeString(),
	}

	return km.save()
}

// RemoveKey removes the API key for a provider.
func (km *KeyManager) RemoveKey(provider string) error {
	km.mu.Lock()
	defer km.mu.Unlock()

	provider = normalizeProvider(provider)
	delete(km.keys, provider)

	return km.save()
}

// GetKey returns the API key for a provider.
func (km *KeyManager) GetKey(provider string) (*ProviderKey, bool) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	provider = normalizeProvider(provider)
	key, ok := km.keys[provider]
	return key, ok
}

// ListKeys returns all stored provider keys.
func (km *KeyManager) ListKeys() []*ProviderKey {
	km.mu.RLock()
	defer km.mu.RUnlock()

	keys := make([]*ProviderKey, 0, len(km.keys))
	for _, key := range km.keys {
		keys = append(keys, key)
	}
	return keys
}

// HasKey returns true if a key is configured for the given provider.
func (km *KeyManager) HasKey(provider string) bool {
	km.mu.RLock()
	defer km.mu.RUnlock()

	provider = normalizeProvider(provider)
	_, ok := km.keys[provider]
	return ok
}

// Mask masks an API key for display, showing only the last 4 characters.
func MaskKey(key string) string {
	if len(key) <= 4 {
		return "****"
	}
	return strings.Repeat("*", len(key)-4) + key[len(key)-4:]
}

// ProviderDisplayName returns a user-friendly name for a provider.
func ProviderDisplayName(provider string) string {
	switch strings.ToLower(provider) {
	case "openai":
		return "OpenAI"
	case "anthropic":
		return "Anthropic"
	case "openrouter":
		return "OpenRouter"
	case "gemini", "google":
		return "Google Gemini"
	case "mistral":
		return "Mistral"
	case "ollama":
		return "Ollama"
	case "cohere":
		return "Cohere"
	default:
		return strings.Title(provider)
	}
}

// KnownProviders returns the list of providers that Orchestra supports.
func KnownProviders() []string {
	return []string{
		"openai",
		"anthropic",
		"openrouter",
		"gemini",
		"mistral",
		"ollama",
		"cohere",
	}
}

// providerEnvVars maps each provider name to the environment variables that
// may hold its API key (first non-empty match wins), and an optional env var
// for a custom base URL. Ollama is omitted because it needs no key.
var providerEnvVars = []struct {
	Provider   string
	KeyVars    []string
	BaseURLVar string
}{
	{"openai", []string{"OPENAI_API_KEY"}, "OPENAI_BASE_URL"},
	{"anthropic", []string{"ANTHROPIC_API_KEY"}, "ANTHROPIC_BASE_URL"},
	{"openrouter", []string{"OPENROUTER_API_KEY"}, "OPENROUTER_BASE_URL"},
	{"gemini", []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"}, "GEMINI_BASE_URL"},
	{"mistral", []string{"MISTRAL_API_KEY"}, "MISTRAL_BASE_URL"},
	{"cohere", []string{"COHERE_API_KEY"}, "COHERE_BASE_URL"},
}

// loadFromEnv populates keys from environment variables for any provider that
// does not already have a key stored on disk. Discovered keys are persisted so
// later loads are stable.
func (km *KeyManager) loadFromEnv() {
	km.mu.Lock()
	defer km.mu.Unlock()

	var changed bool
	for _, p := range providerEnvVars {
		if _, exists := km.keys[p.Provider]; exists {
			continue
		}
		var apiKey string
		for _, envVar := range p.KeyVars {
			if v := strings.TrimSpace(os.Getenv(envVar)); v != "" {
				apiKey = v
				break
			}
		}
		if apiKey == "" {
			continue
		}
		km.keys[p.Provider] = &ProviderKey{
			Provider: p.Provider,
			APIKey:   apiKey,
			BaseURL:  strings.TrimSpace(os.Getenv(p.BaseURLVar)),
			AddedAt:  currentTimeString(),
		}
		changed = true
	}

	if changed {
		// Best-effort persist; a failure here is non-fatal (the key is still
		// available for this session in memory).
		_ = km.saveLocked()
	}
}

// load reads keys from disk.
func (km *KeyManager) load() error {
	path := km.filePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read keys file: %w", err)
	}

	var keys []*ProviderKey
	if err := json.Unmarshal(data, &keys); err != nil {
		return fmt.Errorf("failed to parse keys file: %w", err)
	}

	for _, key := range keys {
		km.keys[normalizeProvider(key.Provider)] = key
	}

	return nil
}

// save persists keys to disk.
func (km *KeyManager) save() error {
	km.mu.Lock()
	defer km.mu.Unlock()
	return km.saveLocked()
}

// saveLocked persists keys to disk; the caller must hold km.mu.
func (km *KeyManager) saveLocked() error {
	keys := make([]*ProviderKey, 0, len(km.keys))
	for _, key := range km.keys {
		keys = append(keys, key)
	}

	data, err := json.MarshalIndent(keys, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal keys: %w", err)
	}

	path := km.filePath()
	return os.WriteFile(path, data, 0o600) // read/write for owner only
}

// filePath returns the path to the keys file.
func (km *KeyManager) filePath() string {
	return filepath.Join(km.dir, "api_keys.json")
}

func normalizeProvider(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func currentTimeString() string {
	return fmt.Sprintf("%d", time.Now().Unix())
}
