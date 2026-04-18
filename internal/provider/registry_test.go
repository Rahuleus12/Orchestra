package provider

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/user/orchestra/internal/message"
)

// mockProvider is a minimal Provider implementation for registry tests.
type mockProvider struct {
	name    string
	models  []ModelInfo
	caps    ModelCapabilities
}

func (m *mockProvider) Name() string                                         { return m.name }
func (m *mockProvider) Models(_ context.Context) ([]ModelInfo, error)        { return m.models, nil }
func (m *mockProvider) Generate(_ context.Context, _ GenerateRequest) (*GenerateResult, error) {
	return &GenerateResult{
		ID:       "test",
		Message:  message.AssistantMessage("mock"),
		Model:    "test-model",
	}, nil
}
func (m *mockProvider) Stream(_ context.Context, _ GenerateRequest) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent)
	close(ch)
	return ch, nil
}
func (m *mockProvider) Capabilities(model string) ModelCapabilities { return m.caps }

// --- NewRegistry ---

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()

	if r == nil {
		t.Fatal("NewRegistry() returned nil")
	}
	if len(r.ListProviders()) != 0 {
		t.Error("new registry should have no providers")
	}
	if len(r.ListAliases()) != 0 {
		t.Error("new registry should have no aliases")
	}
}

// --- Register ---

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()

	factory := func(config ProviderConfig) (Provider, error) {
		return &mockProvider{name: "test-provider"}, nil
	}

	err := r.Register("test", factory, ProviderConfig{DefaultModel: "model-a"})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if !r.IsRegistered("test") {
		t.Error("IsRegistered(\"test\") = false, want true")
	}
}

func TestRegistry_Register_Duplicate(t *testing.T) {
	r := NewRegistry()

	factory := func(config ProviderConfig) (Provider, error) {
		return &mockProvider{name: "test"}, nil
	}

	err := r.Register("test", factory, ProviderConfig{})
	if err != nil {
		t.Fatalf("first Register() error = %v", err)
	}

	err = r.Register("test", factory, ProviderConfig{})
	if err == nil {
		t.Fatal("duplicate Register() should return error")
	}
}

func TestRegistry_Register_NameNormalization(t *testing.T) {
	r := NewRegistry()

	factory := func(config ProviderConfig) (Provider, error) {
		return &mockProvider{name: "Test"}, nil
	}

	// Register with uppercase
	err := r.Register("OpenAI", factory, ProviderConfig{})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Should be accessible via lowercase
	if !r.IsRegistered("openai") {
		t.Error("IsRegistered(\"openai\") = false after registering \"OpenAI\"")
	}

	// Duplicate with different case should fail
	err = r.Register("openai", factory, ProviderConfig{})
	if err == nil {
		t.Fatal("duplicate Register() with normalized name should return error")
	}
}

func TestRegistry_MustRegister(t *testing.T) {
	r := NewRegistry()

	factory := func(config ProviderConfig) (Provider, error) {
		return &mockProvider{name: "test"}, nil
	}

	// Should not panic
	r.MustRegister("test", factory, ProviderConfig{})
}

func TestRegistry_MustRegister_PanicsOnDuplicate(t *testing.T) {
	r := NewRegistry()

	factory := func(config ProviderConfig) (Provider, error) {
		return &mockProvider{name: "test"}, nil
	}

	r.MustRegister("test", factory, ProviderConfig{})

	defer func() {
		if r := recover(); r == nil {
			t.Error("MustRegister on duplicate should panic")
		}
	}()

	r.MustRegister("test", factory, ProviderConfig{})
}

// --- RegisterProvider ---

func TestRegistry_RegisterProvider(t *testing.T) {
	r := NewRegistry()

	p := &mockProvider{name: "direct"}
	err := r.RegisterProvider("direct", p)
	if err != nil {
		t.Fatalf("RegisterProvider() error = %v", err)
	}

	if !r.IsRegistered("direct") {
		t.Error("IsRegistered(\"direct\") = false, want true")
	}

	got, err := r.Get("direct")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Name() != "direct" {
		t.Errorf("Get().Name() = %q, want %q", got.Name(), "direct")
	}
}

func TestRegistry_RegisterProvider_Duplicate(t *testing.T) {
	r := NewRegistry()

	p := &mockProvider{name: "test"}
	_ = r.RegisterProvider("test", p)

	err := r.RegisterProvider("test", p)
	if err == nil {
		t.Fatal("duplicate RegisterProvider() should return error")
	}
}

func TestRegistry_RegisterProvider_ConflictsWithFactory(t *testing.T) {
	r := NewRegistry()

	factory := func(config ProviderConfig) (Provider, error) {
		return &mockProvider{name: "test"}, nil
	}
	_ = r.Register("test", factory, ProviderConfig{})

	p := &mockProvider{name: "test"}
	err := r.RegisterProvider("test", p)
	if err == nil {
		t.Fatal("RegisterProvider() conflicting with factory should return error")
	}
}

func TestRegistry_MustRegisterProvider(t *testing.T) {
	r := NewRegistry()

	p := &mockProvider{name: "test"}
	r.MustRegisterProvider("test", p)

	defer func() {
		if r := recover(); r == nil {
			t.Error("MustRegisterProvider on duplicate should panic")
		}
	}()

	r.MustRegisterProvider("test", p)
}

// --- Get (Lazy Initialization) ---

func TestRegistry_Get_LazyInit(t *testing.T) {
	r := NewRegistry()

	initCalled := false
	factory := func(config ProviderConfig) (Provider, error) {
		initCalled = true
		return &mockProvider{name: "lazy"}, nil
	}

	_ = r.Register("lazy", factory, ProviderConfig{DefaultModel: "model-x"})

	if initCalled {
		t.Fatal("factory should not be called during Register")
	}

	p, err := r.Get("lazy")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if !initCalled {
		t.Fatal("factory should have been called on first Get")
	}

	if p.Name() != "lazy" {
		t.Errorf("Get().Name() = %q, want %q", p.Name(), "lazy")
	}
}

func TestRegistry_Get_ReturnsSameInstance(t *testing.T) {
	r := NewRegistry()

	callCount := 0
	factory := func(config ProviderConfig) (Provider, error) {
		callCount++
		return &mockProvider{name: "singleton"}, nil
	}

	_ = r.Register("singleton", factory, ProviderConfig{})

	p1, _ := r.Get("singleton")
	p2, _ := r.Get("singleton")

	if callCount != 1 {
		t.Errorf("factory called %d times, want 1", callCount)
	}

	if p1 != p2 {
		t.Error("Get() should return the same instance")
	}
}

func TestRegistry_Get_NotRegistered(t *testing.T) {
	r := NewRegistry()

	_, err := r.Get("nonexistent")
	if err == nil {
		t.Fatal("Get() on unregistered provider should return error")
	}
}

func TestRegistry_Get_FactoryError(t *testing.T) {
	r := NewRegistry()

	factory := func(config ProviderConfig) (Provider, error) {
		return nil, errors.New("init failed")
	}

	_ = r.Register("broken", factory, ProviderConfig{})

	_, err := r.Get("broken")
	if err == nil {
		t.Fatal("Get() with failing factory should return error")
	}
	if !errors.Is(err, ErrProviderNotFound) {
		// Should contain "failed to create provider"
	}
}

func TestRegistry_Get_ConfigPassedToFactory(t *testing.T) {
	r := NewRegistry()

	var receivedConfig ProviderConfig
	factory := func(config ProviderConfig) (Provider, error) {
		receivedConfig = config
		return &mockProvider{name: "test"}, nil
	}

	originalConfig := ProviderConfig{
		APIKey:       "test-key-123",
		BaseURL:      "https://api.example.com",
		DefaultModel: "gpt-4",
	}

	_ = r.Register("test", factory, originalConfig)

	_, _ = r.Get("test")

	if receivedConfig.APIKey != "test-key-123" {
		t.Errorf("config.APIKey = %q, want %q", receivedConfig.APIKey, "test-key-123")
	}
	if receivedConfig.BaseURL != "https://api.example.com" {
		t.Errorf("config.BaseURL = %q, want %q", receivedConfig.BaseURL, "https://api.example.com")
	}
	if receivedConfig.DefaultModel != "gpt-4" {
		t.Errorf("config.DefaultModel = %q, want %q", receivedConfig.DefaultModel, "gpt-4")
	}
}

// --- Alias ---

func TestRegistry_Alias(t *testing.T) {
	r := NewRegistry()

	err := r.Alias("gpt4", "openai::gpt-4-turbo")
	if err != nil {
		t.Fatalf("Alias() error = %v", err)
	}

	aliases := r.ListAliases()
	if aliases["gpt4"] != "openai::gpt-4-turbo" {
		t.Errorf("alias gpt4 = %q, want %q", aliases["gpt4"], "openai::gpt-4-turbo")
	}
}

func TestRegistry_Alias_Duplicate(t *testing.T) {
	r := NewRegistry()

	_ = r.Alias("gpt4", "openai::gpt-4-turbo")
	err := r.Alias("gpt4", "openai::gpt-4o")
	if err == nil {
		t.Fatal("duplicate Alias() should return error")
	}
}

func TestRegistry_Alias_NameNormalization(t *testing.T) {
	r := NewRegistry()

	err := r.Alias("GPT4", "openai::gpt-4-turbo")
	if err != nil {
		t.Fatalf("Alias() error = %v", err)
	}

	aliases := r.ListAliases()
	if _, ok := aliases["gpt4"]; !ok {
		t.Error("alias should be normalized to lowercase \"gpt4\"")
	}
}

func TestRegistry_MustAlias(t *testing.T) {
	r := NewRegistry()
	r.MustAlias("gpt4", "openai::gpt-4-turbo")

	defer func() {
		if r := recover(); r == nil {
			t.Error("MustAlias on duplicate should panic")
		}
	}()

	r.MustAlias("gpt4", "openai::gpt-4o")
}

func TestRegistry_SetAlias(t *testing.T) {
	r := NewRegistry()

	r.SetAlias("gpt4", "openai::gpt-4-turbo")
	r.SetAlias("gpt4", "openai::gpt-4o")

	aliases := r.ListAliases()
	if aliases["gpt4"] != "openai::gpt-4o" {
		t.Errorf("SetAlias overwrite: got %q, want %q", aliases["gpt4"], "openai::gpt-4o")
	}
}

// --- Resolve ---

func TestRegistry_Resolve_ProviderModelFormat(t *testing.T) {
	r := NewRegistry()

	factory := func(config ProviderConfig) (Provider, error) {
		return &mockProvider{
			name:   "openai",
			models: []ModelInfo{{ID: "gpt-4-turbo"}},
		}, nil
	}
	_ = r.Register("openai", factory, ProviderConfig{})

	p, model, err := r.Resolve("openai::gpt-4-turbo")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("Resolve() provider = %q, want %q", p.Name(), "openai")
	}
	if model != "gpt-4-turbo" {
		t.Errorf("Resolve() model = %q, want %q", model, "gpt-4-turbo")
	}
}

func TestRegistry_Resolve_ViaAlias(t *testing.T) {
	r := NewRegistry()

	factory := func(config ProviderConfig) (Provider, error) {
		return &mockProvider{name: "openai"}, nil
	}
	_ = r.Register("openai", factory, ProviderConfig{})

	_ = r.Alias("gpt4", "openai::gpt-4-turbo")

	p, model, err := r.Resolve("gpt4")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("Resolve() provider = %q, want %q", p.Name(), "openai")
	}
	if model != "gpt-4-turbo" {
		t.Errorf("Resolve() model = %q, want %q", model, "gpt-4-turbo")
	}
}

func TestRegistry_Resolve_ViaDefaultModel(t *testing.T) {
	r := NewRegistry()

	factory := func(config ProviderConfig) (Provider, error) {
		return &mockProvider{name: "openai"}, nil
	}
	_ = r.Register("openai", factory, ProviderConfig{
		DefaultModel: "gpt-4-turbo",
	})

	p, model, err := r.Resolve("gpt-4-turbo")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("Resolve() provider = %q, want %q", p.Name(), "openai")
	}
	if model != "gpt-4-turbo" {
		t.Errorf("Resolve() model = %q, want %q", model, "gpt-4-turbo")
	}
}

func TestRegistry_Resolve_EmptyRef(t *testing.T) {
	r := NewRegistry()

	_, _, err := r.Resolve("")
	if err == nil {
		t.Fatal("Resolve(\"\") should return error")
	}
}

func TestRegistry_Resolve_WhitespaceRef(t *testing.T) {
	r := NewRegistry()

	_, _, err := r.Resolve("  ")
	if err == nil {
		t.Fatal("Resolve(\"  \") should return error")
	}
}

func TestRegistry_Resolve_UnknownProvider(t *testing.T) {
	r := NewRegistry()

	_, _, err := r.Resolve("unknown::model")
	if err == nil {
		t.Fatal("Resolve() with unknown provider should return error")
	}
}

func TestRegistry_Resolve_UnknownModel(t *testing.T) {
	r := NewRegistry()

	factory := func(config ProviderConfig) (Provider, error) {
		return &mockProvider{name: "openai"}, nil
	}
	_ = r.Register("openai", factory, ProviderConfig{DefaultModel: "gpt-4"})

	_, _, err := r.Resolve("nonexistent-model")
	if err == nil {
		t.Fatal("Resolve() with unknown bare model should return error")
	}
}

func TestRegistry_Resolve_AliasChain(t *testing.T) {
	r := NewRegistry()

	factory := func(config ProviderConfig) (Provider, error) {
		return &mockProvider{name: "anthropic"}, nil
	}
	_ = r.Register("anthropic", factory, ProviderConfig{})

	_ = r.Alias("claude", "anthropic::claude-sonnet-4-20250514")

	p, model, err := r.Resolve("claude")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if p.Name() != "anthropic" {
		t.Errorf("provider = %q, want %q", p.Name(), "anthropic")
	}
	if model != "claude-sonnet-4-20250514" {
		t.Errorf("model = %q, want %q", model, "claude-sonnet-4-20250514")
	}
}

func TestRegistry_Resolve_CaseInsensitiveAlias(t *testing.T) {
	r := NewRegistry()

	factory := func(config ProviderConfig) (Provider, error) {
		return &mockProvider{name: "openai"}, nil
	}
	_ = r.Register("openai", factory, ProviderConfig{})
	_ = r.Alias("GPT4", "openai::gpt-4-turbo")

	p, model, err := r.Resolve("gpt4")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("provider = %q, want %q", p.Name(), "openai")
	}
	if model != "gpt-4-turbo" {
		t.Errorf("model = %q, want %q", model, "gpt-4-turbo")
	}
}

// --- ListProviders ---

func TestRegistry_ListProviders(t *testing.T) {
	r := NewRegistry()

	factory := func(config ProviderConfig) (Provider, error) {
		return &mockProvider{name: "test"}, nil
	}

	_ = r.Register("openai", factory, ProviderConfig{})
	_ = r.Register("anthropic", factory, ProviderConfig{})
	_ = r.RegisterProvider("direct", &mockProvider{name: "direct"})

	providers := r.ListProviders()
	if len(providers) != 3 {
		t.Fatalf("ListProviders() returned %d, want 3", len(providers))
	}

	// Check all are present (order doesn't matter)
	seen := make(map[string]bool)
	for _, name := range providers {
		seen[name] = true
	}
	for _, expected := range []string{"openai", "anthropic", "direct"} {
		if !seen[expected] {
			t.Errorf("ListProviders() missing %q", expected)
		}
	}
}

func TestRegistry_ListProviders_Empty(t *testing.T) {
	r := NewRegistry()
	providers := r.ListProviders()
	if providers == nil || len(providers) != 0 {
		t.Error("empty registry should return empty slice")
	}
}

func TestRegistry_ListProviders_NoDuplicates(t *testing.T) {
	r := NewRegistry()

	// Register, get (lazy init), then list
	factory := func(config ProviderConfig) (Provider, error) {
		return &mockProvider{name: "test"}, nil
	}
	_ = r.Register("openai", factory, ProviderConfig{})
	_, _ = r.Get("openai")

	providers := r.ListProviders()
	count := 0
	for _, name := range providers {
		if name == "openai" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("openai appeared %d times, want 1", count)
	}
}

// --- ListAliases ---

func TestRegistry_ListAliases(t *testing.T) {
	r := NewRegistry()

	_ = r.Alias("gpt4", "openai::gpt-4-turbo")
	_ = r.Alias("claude", "anthropic::claude-sonnet-4-20250514")

	aliases := r.ListAliases()
	if len(aliases) != 2 {
		t.Fatalf("ListAliases() returned %d, want 2", len(aliases))
	}
	if aliases["gpt4"] != "openai::gpt-4-turbo" {
		t.Errorf("gpt4 alias = %q, want %q", aliases["gpt4"], "openai::gpt-4-turbo")
	}
	if aliases["claude"] != "anthropic::claude-sonnet-4-20250514" {
		t.Errorf("claude alias = %q, want %q", aliases["claude"], "anthropic::claude-sonnet-4-20250514")
	}
}

func TestRegistry_ListAliases_Empty(t *testing.T) {
	r := NewRegistry()
	aliases := r.ListAliases()
	if len(aliases) != 0 {
		t.Error("empty registry should return empty alias map")
	}
}

func TestRegistry_ListAliases_Copy(t *testing.T) {
	r := NewRegistry()
	_ = r.Alias("gpt4", "openai::gpt-4-turbo")

	aliases := r.ListAliases()
	aliases["gpt4"] = "modified"

	original := r.ListAliases()
	if original["gpt4"] != "openai::gpt-4-turbo" {
		t.Error("modifying ListAliases() result should not affect registry")
	}
}

// --- IsRegistered ---

func TestRegistry_IsRegistered(t *testing.T) {
	r := NewRegistry()

	if r.IsRegistered("openai") {
		t.Error("unregistered provider should return false")
	}

	factory := func(config ProviderConfig) (Provider, error) {
		return &mockProvider{name: "openai"}, nil
	}
	_ = r.Register("openai", factory, ProviderConfig{})

	if !r.IsRegistered("openai") {
		t.Error("registered provider should return true")
	}
	if !r.IsRegistered("OpenAI") {
		t.Error("IsRegistered should be case-insensitive")
	}
}

func TestRegistry_IsRegistered_DirectProvider(t *testing.T) {
	r := NewRegistry()

	_ = r.RegisterProvider("direct", &mockProvider{name: "direct"})

	if !r.IsRegistered("direct") {
		t.Error("directly registered provider should return true")
	}
}

// --- Clear ---

func TestRegistry_Clear(t *testing.T) {
	r := NewRegistry()

	factory := func(config ProviderConfig) (Provider, error) {
		return &mockProvider{name: "test"}, nil
	}

	_ = r.Register("openai", factory, ProviderConfig{})
	_ = r.RegisterProvider("direct", &mockProvider{name: "direct"})
	_ = r.Alias("gpt4", "openai::gpt-4-turbo")
	_, _ = r.Get("openai") // trigger lazy init

	r.Clear()

	if r.IsRegistered("openai") {
		t.Error("after Clear, openai should not be registered")
	}
	if r.IsRegistered("direct") {
		t.Error("after Clear, direct should not be registered")
	}
	if len(r.ListAliases()) != 0 {
		t.Error("after Clear, aliases should be empty")
	}
	if len(r.ListProviders()) != 0 {
		t.Error("after Clear, providers should be empty")
	}
}

// --- Concurrent Access ---

func TestRegistry_ConcurrentRegisterAndGet(t *testing.T) {
	r := NewRegistry()

	const numGoroutines = 50

	// Phase 1: Concurrent registrations
	var regWg sync.WaitGroup
	regWg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer regWg.Done()
			name := fmt.Sprintf("provider-%d", idx)
			factory := func(config ProviderConfig) (Provider, error) {
				return &mockProvider{name: name}, nil
			}
			_ = r.Register(name, factory, ProviderConfig{})
		}(i)
	}

	// Wait for all registrations to complete before starting gets
	regWg.Wait()

	// Phase 2: Concurrent gets
	var getWg sync.WaitGroup
	getWg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer getWg.Done()
			name := fmt.Sprintf("provider-%d", idx)
			p, err := r.Get(name)
			if err != nil {
				t.Errorf("Get(%q) error = %v", name, err)
				return
			}
			if p.Name() != name {
				t.Errorf("Get(%q).Name() = %q, want %q", name, p.Name(), name)
			}
		}(i)
	}

	getWg.Wait()
}

func TestRegistry_ConcurrentGet_SameProvider(t *testing.T) {
	r := NewRegistry()

	callCount := 0
	var mu sync.Mutex
	factory := func(config ProviderConfig) (Provider, error) {
		mu.Lock()
		callCount++
		mu.Unlock()
		time.Sleep(10 * time.Millisecond) // slow init
		return &mockProvider{name: "slow"}, nil
	}

	_ = r.Register("slow", factory, ProviderConfig{})

	const numGoroutines = 20
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	results := make([]Provider, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			p, err := r.Get("slow")
			if err != nil {
				t.Errorf("Get() error = %v", err)
				return
			}
			results[idx] = p
		}(i)
	}

	wg.Wait()

	// Factory should be called exactly once
	if callCount != 1 {
		t.Errorf("factory called %d times, want 1", callCount)
	}

	// All goroutines should get the same instance
	for i := 1; i < numGoroutines; i++ {
		if results[i] != results[0] {
			t.Errorf("results[%d] != results[0]: concurrent gets returned different instances", i)
		}
	}
}

func TestRegistry_ConcurrentAliasAndResolve(t *testing.T) {
	r := NewRegistry()

	factory := func(config ProviderConfig) (Provider, error) {
		return &mockProvider{name: "openai"}, nil
	}
	_ = r.Register("openai", factory, ProviderConfig{})

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				alias := fmt.Sprintf("alias-%d", idx)
				r.SetAlias(alias, "openai::gpt-4")
				_, _, _ = r.Resolve(alias)
			} else {
				_, _, _ = r.Resolve("openai::gpt-4")
			}
		}(i)
	}

	wg.Wait()
}

// --- parseProviderModel ---

func TestParseProviderModel(t *testing.T) {
	tests := []struct {
		input   string
		pProvider string
		pModel   string
		ok      bool
	}{
		{"openai::gpt-4", "openai", "gpt-4", true},
		{"anthropic::claude-sonnet-4-20250514", "anthropic", "claude-sonnet-4-20250514", true},
		{"openai::", "openai", "", false},
		{"::model", "", "model", false},
		{"::", "", "", false},
		{"no-separator", "", "", false},
		{"openai::gpt-4::extra", "openai", "gpt-4::extra", true}, // SplitN(2)
		{" openai :: gpt-4 ", "openai", "gpt-4", true},           // trimmed
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			provider, model, ok := parseProviderModel(tt.input)
			if ok != tt.ok {
				t.Errorf("parseProviderModel(%q) ok = %v, want %v", tt.input, ok, tt.ok)
			}
			if provider != tt.pProvider {
				t.Errorf("provider = %q, want %q", provider, tt.pProvider)
			}
			if model != tt.pModel {
				t.Errorf("model = %q, want %q", model, tt.pModel)
			}
		})
	}
}

// --- normalizeName ---

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"OpenAI", "openai"},
		{"  OpenAI  ", "openai"},
		{"GPT-4", "gpt-4"},
		{"", ""},
		{"already_lower", "already_lower"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := normalizeName(tt.input); got != tt.want {
				t.Errorf("normalizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- Global Registry ---

func TestGlobalRegistry(t *testing.T) {
	// Save and restore the global registry
	original := GlobalRegistry
	defer func() { GlobalRegistry = original }()

	GlobalRegistry = NewRegistry()

	factory := func(config ProviderConfig) (Provider, error) {
		return &mockProvider{name: "global-test"}, nil
	}

	err := Register("global-test", factory, ProviderConfig{})
	if err != nil {
		t.Fatalf("global Register() error = %v", err)
	}

	p, err := Get("global-test")
	if err != nil {
		t.Fatalf("global Get() error = %v", err)
	}
	if p.Name() != "global-test" {
		t.Errorf("global Get().Name() = %q, want %q", p.Name(), "global-test")
	}
}

func TestGlobalRegistry_MustRegisterAndAlias(t *testing.T) {
	original := GlobalRegistry
	defer func() { GlobalRegistry = original }()

	GlobalRegistry = NewRegistry()

	factory := func(config ProviderConfig) (Provider, error) {
		return &mockProvider{name: "openai"}, nil
	}

	MustRegister("openai", factory, ProviderConfig{})
	MustAlias("gpt4", "openai::gpt-4-turbo")

	p, model, err := Resolve("gpt4")
	if err != nil {
		t.Fatalf("global Resolve() error = %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("provider = %q, want %q", p.Name(), "openai")
	}
	if model != "gpt-4-turbo" {
		t.Errorf("model = %q, want %q", model, "gpt-4-turbo")
	}
}

// --- Integration: Full Workflow ---

func TestRegistry_FullWorkflow(t *testing.T) {
	r := NewRegistry()

	// Register multiple providers
	openaiFactory := func(config ProviderConfig) (Provider, error) {
		return &mockProvider{
			name:   "openai",
			models: []ModelInfo{{ID: "gpt-4-turbo"}, {ID: "gpt-4o"}},
		}, nil
	}
	anthropicFactory := func(config ProviderConfig) (Provider, error) {
		return &mockProvider{
			name:   "anthropic",
			models: []ModelInfo{{ID: "claude-sonnet-4-20250514"}},
		}, nil
	}

	_ = r.Register("openai", openaiFactory, ProviderConfig{DefaultModel: "gpt-4-turbo"})
	_ = r.Register("anthropic", anthropicFactory, ProviderConfig{DefaultModel: "claude-sonnet-4-20250514"})

	// Register aliases
	_ = r.Alias("gpt4", "openai::gpt-4-turbo")
	_ = r.Alias("claude", "anthropic::claude-sonnet-4-20250514")
	_ = r.Alias("smart", "openai::gpt-4o")

	// List all
	providers := r.ListProviders()
	if len(providers) != 2 {
		t.Fatalf("ListProviders() = %d, want 2", len(providers))
	}

	aliases := r.ListAliases()
	if len(aliases) != 3 {
		t.Fatalf("ListAliases() = %d, want 3", len(aliases))
	}

	// Resolve via different methods
	tests := []struct {
		ref          string
		wantProvider string
		wantModel    string
	}{
		{"openai::gpt-4-turbo", "openai", "gpt-4-turbo"},
		{"anthropic::claude-sonnet-4-20250514", "anthropic", "claude-sonnet-4-20250514"},
		{"gpt4", "openai", "gpt-4-turbo"},
		{"claude", "anthropic", "claude-sonnet-4-20250514"},
		{"smart", "openai", "gpt-4o"},
		{"gpt-4-turbo", "openai", "gpt-4-turbo"},       // bare model matches default
		{"claude-sonnet-4-20250514", "anthropic", "claude-sonnet-4-20250514"}, // bare model matches default
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			p, model, err := r.Resolve(tt.ref)
			if err != nil {
				t.Fatalf("Resolve(%q) error = %v", tt.ref, err)
			}
			if p.Name() != tt.wantProvider {
				t.Errorf("provider = %q, want %q", p.Name(), tt.wantProvider)
			}
			if model != tt.wantModel {
				t.Errorf("model = %q, want %q", model, tt.wantModel)
			}
		})
	}

	// Use provider directly via Get
	p, err := r.Get("openai")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	result, err := p.Generate(context.Background(), GenerateRequest{
		Model:    "gpt-4-turbo",
		Messages: []message.Message{message.UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if result.Message.Text() != "mock" {
		t.Errorf("Generate() text = %q, want %q", result.Message.Text(), "mock")
	}
}
