// Package provider defines the core abstraction layer for LLM providers.
package provider

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Registry manages the creation and lookup of Provider instances.
// It supports lazy initialization via provider factories and model
// reference resolution through aliases.
//
// A Registry is safe for concurrent use by multiple goroutines.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]ProviderFactory
	providers map[string]Provider
	configs   map[string]ProviderConfig
	aliases   map[string]string
}

// NewRegistry creates a new empty provider registry.
func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[string]ProviderFactory),
		providers: make(map[string]Provider),
		configs:   make(map[string]ProviderConfig),
		aliases:   make(map[string]string),
	}
}

// Register registers a provider factory with the given name and configuration.
// The provider is not instantiated until it is first requested via Get,
// enabling lazy initialization.
//
// If a factory is already registered under the given name, Register returns
// an error. Use MustRegister in tests or initialization code where a panic
// is acceptable on duplicate registration.
func (r *Registry) Register(name string, factory ProviderFactory, config ProviderConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name = normalizeName(name)

	if _, exists := r.factories[name]; exists {
		return fmt.Errorf("provider %q already registered", name)
	}

	r.factories[name] = factory
	r.configs[name] = config

	return nil
}

// MustRegister is like Register but panics if a provider with the given
// name is already registered. This is intended for use in init() functions
// and test setups where a duplicate registration indicates a programming error.
func (r *Registry) MustRegister(name string, factory ProviderFactory, config ProviderConfig) {
	if err := r.Register(name, factory, config); err != nil {
		panic(err)
	}
}

// RegisterProvider registers an already-instantiated Provider directly,
// bypassing the lazy factory mechanism. This is useful for providers
// that require custom initialization or for injecting mock providers
// in tests.
//
// If a provider is already registered or a factory exists under the given
// name, RegisterProvider returns an error.
func (r *Registry) RegisterProvider(name string, p Provider) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name = normalizeName(name)

	if _, exists := r.providers[name]; exists {
		return fmt.Errorf("provider %q already registered", name)
	}
	if _, exists := r.factories[name]; exists {
		return fmt.Errorf("factory for provider %q already registered", name)
	}

	r.providers[name] = p

	return nil
}

// MustRegisterProvider is like RegisterProvider but panics on error.
func (r *Registry) MustRegisterProvider(name string, p Provider) {
	if err := r.RegisterProvider(name, p); err != nil {
		panic(err)
	}
}

// Get returns the Provider registered under the given name. If the provider
// has not yet been instantiated, its factory is invoked with the stored
// configuration to create it (lazy initialization).
//
// Returns an error if no factory or provider is registered under the name,
// or if factory invocation fails.
func (r *Registry) Get(name string) (Provider, error) {
	name = normalizeName(name)

	// Fast path: check if already instantiated (read lock)
	r.mu.RLock()
	p, exists := r.providers[name]
	if exists {
		r.mu.RUnlock()
		return p, nil
	}
	r.mu.RUnlock()

	// Slow path: instantiate via factory (write lock)
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock
	if p, exists = r.providers[name]; exists {
		return p, nil
	}

	factory, exists := r.factories[name]
	if !exists {
		return nil, fmt.Errorf("provider %q not registered", name)
	}

	config, exists := r.configs[name]
	if !exists {
		config = ProviderConfig{}
	}

	p, err := factory(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider %q: %w", name, err)
	}

	r.providers[name] = p

	return p, nil
}

// Alias registers a short alias for a model reference. The alias maps to
// a fully qualified "provider::model" string.
//
// Example:
//
//	reg.Alias("gpt4", "openai::gpt-4-turbo")
//	reg.Alias("claude", "anthropic::claude-sonnet-4-20250514")
//
// If the alias already exists, Alias returns an error.
func (r *Registry) Alias(shortName, modelRef string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	shortName = normalizeName(shortName)

	if _, exists := r.aliases[shortName]; exists {
		return fmt.Errorf("alias %q already registered", shortName)
	}

	r.aliases[shortName] = modelRef

	return nil
}

// MustAlias is like Alias but panics on error.
func (r *Registry) MustAlias(shortName, modelRef string) {
	if err := r.Alias(shortName, modelRef); err != nil {
		panic(err)
	}
}

// SetAlias registers or overwrites a short alias for a model reference.
// Unlike Alias, this does not return an error if the alias already exists.
func (r *Registry) SetAlias(shortName, modelRef string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.aliases[normalizeName(shortName)] = modelRef
}

// Resolve resolves a model reference string to a Provider and model ID.
//
// The reference can be in one of these formats:
//   - "provider::model"  — directly specifies provider and model
//   - "model"            — resolved against all providers' default models
//   - "alias"            — resolved via registered aliases to "provider::model"
//
// Returns the resolved Provider, the model ID, or an error if resolution fails.
func (r *Registry) Resolve(modelRef string) (Provider, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	modelRef = strings.TrimSpace(modelRef)
	if modelRef == "" {
		return nil, "", fmt.Errorf("empty model reference")
	}

	// Step 1: Check if it's a registered alias
	if resolved, ok := r.aliases[normalizeName(modelRef)]; ok {
		modelRef = resolved
	}

	// Step 2: Check for "provider::model" format
	if providerName, modelID, ok := parseProviderModel(modelRef); ok {
		// Look up factory without triggering lazy init inside the lock
		if _, exists := r.factories[providerName]; !exists {
			if _, exists := r.providers[providerName]; !exists {
				return nil, "", fmt.Errorf("provider %q not registered (resolving %q)", providerName, modelRef)
			}
		}

		// Release read lock before calling Get (which may need a write lock)
		r.mu.RUnlock()
		p, err := r.Get(providerName)
		r.mu.RLock() // re-acquire for deferred unlock
		if err != nil {
			return nil, "", err
		}
		return p, modelID, nil
	}

	// Step 3: Bare model name — try to find a provider whose default model matches
	for name, config := range r.configs {
		if config.DefaultModel == modelRef {
			r.mu.RUnlock()
			p, err := r.Get(name)
			r.mu.RLock()
			if err != nil {
				return nil, "", err
			}
			return p, modelRef, nil
		}
	}

	// Step 4: Check already-instantiated providers
	for name, p := range r.providers {
		// Try to match against the provider's known models
		models, err := p.Models(context.Background())
		if err != nil {
			continue
		}
		for _, m := range models {
			if m.ID == modelRef {
				return p, modelRef, nil
			}
		}
		_ = name
	}

	return nil, "", fmt.Errorf("cannot resolve model reference %q: no matching provider or alias found", modelRef)
}

// ListProviders returns the names of all registered providers (both
// factory-registered and directly registered).
func (r *Registry) ListProviders() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	names := make([]string, 0)

	for name := range r.factories {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	for name := range r.providers {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}

	return names
}

// ListAliases returns all registered aliases and their resolved references.
func (r *Registry) ListAliases() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]string, len(r.aliases))
	for k, v := range r.aliases {
		result[k] = v
	}
	return result
}

// IsRegistered returns true if a provider or factory exists under the given name.
func (r *Registry) IsRegistered(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	name = normalizeName(name)

	_, hasFactory := r.factories[name]
	_, hasProvider := r.providers[name]

	return hasFactory || hasProvider
}

// Clear removes all registered providers, factories, and aliases.
// This is primarily useful in tests.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.factories = make(map[string]ProviderFactory)
	r.providers = make(map[string]Provider)
	r.configs = make(map[string]ProviderConfig)
	r.aliases = make(map[string]string)
}

// parseProviderModel parses a "provider::model" string into its components.
// Returns the parsed provider and model along with false if either is empty.
// Returns ("", "", false) if the string doesn't contain the separator.
func parseProviderModel(ref string) (string, string, bool) {
	parts := strings.SplitN(ref, "::", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	provider := strings.TrimSpace(parts[0])
	model := strings.TrimSpace(parts[1])
	return provider, model, provider != "" && model != ""
}

// normalizeName lowercases and trims a provider or alias name.
func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// GlobalRegistry is the default global provider registry.
// It is safe for concurrent use and can be accessed directly
// for convenience in simple use cases.
var GlobalRegistry = NewRegistry()

// Register registers a provider factory in the global registry.
// See Registry.Register for details.
func Register(name string, factory ProviderFactory, config ProviderConfig) error {
	return GlobalRegistry.Register(name, factory, config)
}

// MustRegister registers a provider factory in the global registry,
// panicking on error.
func MustRegister(name string, factory ProviderFactory, config ProviderConfig) {
	GlobalRegistry.MustRegister(name, factory, config)
}

// Get retrieves a provider from the global registry.
func Get(name string) (Provider, error) {
	return GlobalRegistry.Get(name)
}

// Resolve resolves a model reference using the global registry.
func Resolve(modelRef string) (Provider, string, error) {
	return GlobalRegistry.Resolve(modelRef)
}

// Alias registers an alias in the global registry.
func Alias(shortName, modelRef string) error {
	return GlobalRegistry.Alias(shortName, modelRef)
}

// MustAlias registers an alias in the global registry, panicking on error.
func MustAlias(shortName, modelRef string) {
	GlobalRegistry.MustAlias(shortName, modelRef)
}
