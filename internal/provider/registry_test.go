package provider

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/user/orchestra/internal/config"
	"github.com/user/orchestra/internal/message"
)

// mockProvider is a minimal Provider implementation for registry tests.
type mockProvider struct {
	name   string
	models []ModelInfo
	caps   ModelCapabilities
}

func (m *mockProvider) Name() string                                  { return m.name }
func (m *mockProvider) Models(_ context.Context) ([]ModelInfo, error) { return m.models, nil }
func (m *mockProvider) Generate(_ context.Context, _ GenerateRequest) (*GenerateResult, error) {
	return &GenerateResult{
		ID:      "test",
		Message: message.AssistantMessage("mock"),
		Model:   "test-model",
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

	factory := func(cfg config.ProviderConfig) (Provider, error) {
		return &mockProvider{name: "test-provider"}, nil
	}

	err := r.Register("test", factory, config.ProviderConfig{DefaultModel: "model-a"})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if !r.IsRegistered("test") {
		t.Error("IsRegistered() should return true after Register()")
	}
}

func TestRegistry_Register_Duplicate(t *testing.T) {
	r := NewRegistry()

	factory := func(cfg config.ProviderConfig) (Provider, error) {
		return &mockProvider{name: "test"}, nil
	}

	err := r.Register("test", factory, config.ProviderConfig{})
	if err != nil {
		t.Fatalf("first Register() error = %v", err)
	}

	err = r.Register("test", factory, config.ProviderConfig{})
	if err == nil {
		t.Fatal("duplicate Register() should return error")
	}
}

func TestRegistry_Register_NameNormalization(t *testing.T) {
	r := NewRegistry()

	factory := func(cfg config.ProviderConfig) (Provider, error) {
		return &mockProvider{name: "Test"}, nil
	}

	// Register with uppercase
	err := r.Register("OpenAI", factory, config.ProviderConfig{})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Get with lowercase should work
	p, err := r.Get("openai")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if p.Name() != "Test" {
		t.Errorf("Get() name = %q, want %q", p.Name(), "Test")
	}

	// Duplicate with different case should fail
	err = r.Register("openai", factory, config.ProviderConfig{})
	if err == nil {
		t.Fatal("duplicate Register() with normalized name should return error")
	}
}

func TestRegistry_MustRegister(t *testing.T) {
	r := NewRegistry()

	factory := func(cfg config.ProviderConfig) (Provider, error) {
		return &mockProvider{name: "test"}, nil
	}

	// Should not panic
	r.MustRegister("test", factory, config.ProviderConfig{})
}

func TestRegistry_MustRegister_PanicsOnDuplicate(t *testing.T) {
	r := NewRegistry()

	factory := func(cfg config.ProviderConfig) (Provider, error) {
		return &mockProvider{name: "test"}, nil
	}

	r.MustRegister("test", factory, config.ProviderConfig{})

	defer func() {
		if r := recover(); r == nil {
			t.Error("MustRegister on duplicate should panic")
		}
	}()

	r.MustRegister("test", factory, config.ProviderConfig{})
}

// --- RegisterProvider ---

func TestRegistry_RegisterProvider(t *testing.T) {
	r := NewRegistry()

	p := &mockProvider{name: "direct"}
	err := r.RegisterProvider("direct", p)
	if err != nil {
		t.Fatalf("RegisterProvider() error = %v", err)
	}

	got, err := r.Get("direct")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != p {
		t.Error("Get() should return the same instance")
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

	factory := func(cfg config.ProviderConfig) (Provider, error) {
		return &mockProvider{name: "test"}, nil
	}
	_ = r.Register("test", factory, config.ProviderConfig{})

	p := &mockProvider{name: "test"}
	err := r.RegisterProvider("test", p)
	if err == nil {
		t.Fatal("RegisterProvider() conflicting with factory should return error")
	}
}

func TestRegistry_MustRegisterProvider(t *testing.T) {
	r := NewRegistry()

	p := &mockProvider{name: "test"}
	// Should not panic
	r.MustRegisterProvider("test", p)
}

// --- Get (lazy initialization) ---

func TestRegistry_Get_LazyInit(t *testing.T) {
	r := NewRegistry()

	initCalled := false
	factory := func(cfg config.ProviderConfig) (Provider, error) {
		initCalled = true
		return &mockProvider{name: "lazy"}, nil
	}

	_ = r.Register("lazy", factory, config.ProviderConfig{DefaultModel: "model-x"})

	if initCalled {
		t.Fatal("factory should not be called during Register()")
	}

	p, err := r.Get("lazy")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !initCalled {
		t.Fatal("factory should have been called during Get()")
	}
	if p.Name() != "lazy" {
		t.Errorf("Get() name = %q, want %q", p.Name(), "lazy")
	}
}

func TestRegistry_Get_ReturnsSameInstance(t *testing.T) {
	r := NewRegistry()

	callCount := 0
	factory := func(cfg config.ProviderConfig) (Provider, error) {
		callCount++
		return &mockProvider{name: "singleton"}, nil
	}

	_ = r.Register("singleton", factory, config.ProviderConfig{})

	p1, _ := r.Get("singleton")
	p2, _ := r.Get("singleton")

	if p1 != p2 {
		t.Error("Get() should return the same instance (singleton)")
	}
	if callCount != 1 {
		t.Errorf("factory called %d times, want 1", callCount)
	}
}

func TestRegistry_Get_NotRegistered(t *testing.T) {
	r := NewRegistry()

	_, err := r.Get("nonexistent")
	if err == nil {
		t.Fatal("Get() for unregistered provider should return error")
	}
}

func TestRegistry_Get_FactoryError(t *testing.T) {
	r := NewRegistry()

	factory := func(cfg config.ProviderConfig) (Provider, error) {
		return nil, errors.New("init failed")
	}

	_ = r.Register("broken", factory, config.ProviderConfig{})

	_, err := r.Get("broken")
	if err == nil {
		t.Fatal("Get() with failing factory should return error")
	}
}

func TestRegistry_Get_ConfigPassedToFactory(t *testing.T) {
	r := NewRegistry()

	var receivedConfig config.ProviderConfig
	factory := func(cfg config.ProviderConfig) (Provider, error) {
		receivedConfig = cfg
		return &mockProvider{name: "test"}, nil
	}

	originalConfig := config.ProviderConfig{
		APIKey:       "test-key-123",
		BaseURL:      "https://api.example.com",
		DefaultModel: "gpt-4",
	}

	_ = r.Register("test", factory, originalConfig)

	_, err := r.Get("test")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if receivedConfig.APIKey != originalConfig.APIKey {
		t.Errorf("received APIKey = %q, want %q", receivedConfig.APIKey, originalConfig.APIKey)
	}
	if receivedConfig.BaseURL != originalConfig.BaseURL {
		t.Errorf("received BaseURL = %q, want %q", receivedConfig.BaseURL, originalConfig.BaseURL)
	}
	if receivedConfig.DefaultModel != originalConfig.DefaultModel {
		t.Errorf("received DefaultModel = %q, want %q", receivedConfig.DefaultModel, originalConfig.DefaultModel)
	}
}

// --- Alias ---

func TestRegistry_Alias(t *testing.T) {
	r := NewRegistry()

	factory := func(cfg config.ProviderConfig) (Provider, error) {
		return &mockProvider{name: "openai"}, nil
	}
	_ = r.Register("openai", factory, config.ProviderConfig{})

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

	_ = r.Alias("GPT4", "openai::gpt-4-turbo")

	aliases := r.ListAliases()
	if _, ok := aliases["gpt4"]; !ok {
		t.Error("alias should be normalized to lowercase")
	}
}

func TestRegistry_MustAlias(t *testing.T) {
	r := NewRegistry()

	// Should not panic
	r.MustAlias("gpt4", "openai::gpt-4-turbo")
}

func TestRegistry_SetAlias(t *testing.T) {
	r := NewRegistry()

	// First set
	r.SetAlias("gpt4", "openai::gpt-4-turbo")

	// Overwrite should work without error
	r.SetAlias("gpt4", "openai::gpt-4o")

	aliases := r.ListAliases()
	if aliases["gpt4"] != "openai::gpt-4o" {
		t.Errorf("alias gpt4 = %q, want %q", aliases["gpt4"], "openai::gpt-4o")
	}
}

// --- Resolve ---

func TestRegistry_Resolve_ProviderModelFormat(t *testing.T) {
	r := NewRegistry()

	factory := func(cfg config.ProviderConfig) (Provider, error) {
		return &mockProvider{
			name:   "openai",
			models: []ModelInfo{{ID: "gpt-4-turbo"}},
		}, nil
	}
	_ = r.Register("openai", factory, config.ProviderConfig{})

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

	factory := func(cfg config.ProviderConfig) (Provider, error) {
		return &mockProvider{name: "openai"}, nil
	}
	_ = r.Register("openai", factory, config.ProviderConfig{})

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

	factory := func(cfg config.ProviderConfig) (Provider, error) {
		return &mockProvider{name: "openai"}, nil
	}
	_ = r.Register("openai", factory, config.ProviderConfig{
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
		t.Fatal("Resolve() with empty ref should return error")
	}
}

func TestRegistry_Resolve_WhitespaceRef(t *testing.T) {
	r := NewRegistry()

	_, _, err := r.Resolve("   ")
	if err == nil {
		t.Fatal("Resolve() with whitespace ref should return error")
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

	factory := func(cfg config.ProviderConfig) (Provider, error) {
		return &mockProvider{name: "openai"}, nil
	}
	_ = r.Register("openai", factory, config.ProviderConfig{DefaultModel: "gpt-4"})

	_, _, err := r.Resolve("nonexistent-model")
	if err == nil {
		t.Fatal("Resolve() with unknown bare model should return error")
	}
}

func TestRegistry_Resolve_AliasChain(t *testing.T) {
	r := NewRegistry()

	factory := func(cfg config.ProviderConfig) (Provider, error) {
		return &mockProvider{name: "anthropic"}, nil
	}
	_ = r.Register("anthropic", factory, config.ProviderConfig{})

	_ = r.Alias("claude", "anthropic::claude-sonnet-4-20250514")

	p, model, err := r.Resolve("claude")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if p.Name() != "anthropic" {
		t.Errorf("Resolve() provider = %q, want %q", p.Name(), "anthropic")
	}
	if model != "claude-sonnet-4-20250514" {
		t.Errorf("Resolve() model = %q, want %q", model, "claude-sonnet-4-20250514")
	}
}

func TestRegistry_Resolve_CaseInsensitiveAlias(t *testing.T) {
	r := NewRegistry()

	factory := func(cfg config.ProviderConfig) (Provider, error) {
		return &mockProvider{name: "openai"}, nil
	}
	_ = r.Register("openai", factory, config.ProviderConfig{})
	_ = r.Alias("GPT4", "openai::gpt-4-turbo")

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

// --- ListProviders ---

func TestRegistry_ListProviders(t *testing.T) {
	r := NewRegistry()

	factory := func(cfg config.ProviderConfig) (Provider, error) {
		return &mockProvider{name: "test"}, nil
	}

	_ = r.Register("openai", factory, config.ProviderConfig{})
	_ = r.Register("anthropic", factory, config.ProviderConfig{})
	_ = r.RegisterProvider("direct", &mockProvider{name: "direct"})

	providers := r.ListProviders()
	if len(providers) != 3 {
		t.Fatalf("ListProviders() returned %d providers, want 3", len(providers))
	}

	seen := make(map[string]bool)
	for _, name := range providers {
		seen[name] = true
	}
	if !seen["openai"] || !seen["anthropic"] || !seen["direct"] {
		t.Error("ListProviders() missing expected providers")
	}
}

func TestRegistry_ListProviders_Empty(t *testing.T) {
	r := NewRegistry()

	providers := r.ListProviders()
	if len(providers) != 0 {
		t.Errorf("ListProviders() on empty registry returned %d, want 0", len(providers))
	}
}

func TestRegistry_ListProviders_NoDuplicates(t *testing.T) {
	r := NewRegistry()

	// Register, get (lazy init), then list
	factory := func(cfg config.ProviderConfig) (Provider, error) {
		return &mockProvider{name: "test"}, nil
	}
	_ = r.Register("openai", factory, config.ProviderConfig{})
	_, _ = r.Get("openai")

	providers := r.ListProviders()
	count := 0
	for _, name := range providers {
		if name == "openai" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("openai appears %d times in ListProviders(), want 1", count)
	}
}

// --- ListAliases ---

func TestRegistry_ListAliases(t *testing.T) {
	r := NewRegistry()

	_ = r.Alias("gpt4", "openai::gpt-4-turbo")
	_ = r.Alias("claude", "anthropic::claude-sonnet-4-20250514")

	aliases := r.ListAliases()
	if len(aliases) != 2 {
		t.Fatalf("ListAliases() returned %d aliases, want 2", len(aliases))
	}
	if aliases["gpt4"] != "openai::gpt-4-turbo" {
		t.Errorf("alias gpt4 = %q, want %q", aliases["gpt4"], "openai::gpt-4-turbo")
	}
	if aliases["claude"] != "anthropic::claude-sonnet-4-20250514" {
		t.Errorf("alias claude = %q, want %q", aliases["claude"], "anthropic::claude-sonnet-4-20250514")
	}
}

func TestRegistry_ListAliases_Empty(t *testing.T) {
	r := NewRegistry()

	aliases := r.ListAliases()
	if len(aliases) != 0 {
		t.Errorf("ListAliases() on empty registry returned %d, want 0", len(aliases))
	}
}

func TestRegistry_ListAliases_Copy(t *testing.T) {
	r := NewRegistry()

	_ = r.Alias("gpt4", "openai::gpt-4-turbo")

	// Modifying the returned map should not affect the registry
	aliases := r.ListAliases()
	aliases["gpt4"] = "modified"

	original := r.ListAliases()
	if original["gpt4"] != "openai::gpt-4-turbo" {
		t.Error("ListAliases() should return a copy, not the internal map")
	}
}

// --- IsRegistered ---

func TestRegistry_IsRegistered(t *testing.T) {
	r := NewRegistry()

	factory := func(cfg config.ProviderConfig) (Provider, error) {
		return &mockProvider{name: "test"}, nil
	}

	if r.IsRegistered("openai") {
		t.Error("IsRegistered() should return false for unregistered provider")
	}

	_ = r.Register("openai", factory, config.ProviderConfig{})

	if !r.IsRegistered("openai") {
		t.Error("IsRegistered() should return true after Register()")
	}
}

func TestRegistry_IsRegistered_DirectProvider(t *testing.T) {
	r := NewRegistry()

	p := &mockProvider{name: "test"}
	_ = r.RegisterProvider("direct", p)

	if !r.IsRegistered("direct") {
		t.Error("IsRegistered() should return true for directly registered provider")
	}
}

// --- Clear ---

func TestRegistry_Clear(t *testing.T) {
	r := NewRegistry()

	factory := func(cfg config.ProviderConfig) (Provider, error) {
		return &mockProvider{name: "test"}, nil
	}

	_ = r.Register("openai", factory, config.ProviderConfig{})
	_ = r.RegisterProvider("direct", &mockProvider{name: "direct"})
	_ = r.Alias("gpt4", "openai::gpt-4-turbo")

	// Instantiate the factory-registered provider
	_, _ = r.Get("openai")

	r.Clear()

	if len(r.ListProviders()) != 0 {
		t.Error("Clear() should remove all providers")
	}
	if len(r.ListAliases()) != 0 {
		t.Error("Clear() should remove all aliases")
	}
	if r.IsRegistered("openai") {
		t.Error("Clear() should remove factory registrations")
	}
	if r.IsRegistered("direct") {
		t.Error("Clear() should remove direct registrations")
	}
}

// --- Concurrent access ---

func TestRegistry_ConcurrentRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	const numGoroutines = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	// Concurrent registers
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			name := fmt.Sprintf("provider-%d", id)
			factory := func(cfg config.ProviderConfig) (Provider, error) {
				return &mockProvider{name: name}, nil
			}
			_ = r.Register(name, factory, config.ProviderConfig{})
		}(i)
	}

	// Concurrent gets (some will fail, some will succeed)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			name := fmt.Sprintf("provider-%d", id)
			_, _ = r.Get(name)
		}(i)
	}

	wg.Wait()

	providers := r.ListProviders()
	if len(providers) != numGoroutines {
		t.Errorf("expected %d providers, got %d", numGoroutines, len(providers))
	}
}

func TestRegistry_ConcurrentGet_SameProvider(t *testing.T) {
	r := NewRegistry()

	callCount := 0
	factory := func(cfg config.ProviderConfig) (Provider, error) {
		callCount++
		time.Sleep(1 * time.Millisecond) // simulate slow init
		return &mockProvider{name: "shared"}, nil
	}
	_ = r.Register("shared", factory, config.ProviderConfig{})

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	instances := make(chan Provider, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			p, err := r.Get("shared")
			if err != nil {
				t.Errorf("Get() error = %v", err)
				return
			}
			instances <- p
		}()
	}

	wg.Wait()
	close(instances)

	var first Provider
	for p := range instances {
		if first == nil {
			first = p
		}
		if p != first {
			t.Error("concurrent Get() should return the same instance")
			break
		}
	}

	if callCount != 1 {
		t.Errorf("factory called %d times, want 1", callCount)
	}
}

func TestRegistry_ConcurrentAliasAndResolve(t *testing.T) {
	r := NewRegistry()

	factory := func(cfg config.ProviderConfig) (Provider, error) {
		return &mockProvider{name: "openai"}, nil
	}
	_ = r.Register("openai", factory, config.ProviderConfig{})

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			if id%2 == 0 {
				r.SetAlias(fmt.Sprintf("alias-%d", id), fmt.Sprintf("openai::model-%d", id))
			} else {
				_, _, _ = r.Resolve(fmt.Sprintf("alias-%d", id-1))
			}
		}(i)
	}

	wg.Wait()
}

// --- parseProviderModel ---

func TestParseProviderModel(t *testing.T) {
	tests := []struct {
		input    string
		pProvider string
		pModel   string
		ok       bool
	}{
		{"openai::gpt-4", "openai", "gpt-4", true},
		{"anthropic::claude-sonnet-4-20250514", "anthropic", "claude-sonnet-4-20250514", true},
		{"gpt-4", "", "", false},
		{"::model", "", "model", false},
		{"provider::", "provider", "", false},
		{"a::b::c", "a", "b::c", true},
		{" provider :: model ", "provider", "model", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			p, m, ok := parseProviderModel(tt.input)
			if ok != tt.ok {
				t.Errorf("parseProviderModel(%q) ok = %v, want %v", tt.input, ok, tt.ok)
			}
			if p != tt.pProvider {
				t.Errorf("parseProviderModel(%q) provider = %q, want %q", tt.input, p, tt.pProvider)
			}
			if m != tt.pModel {
				t.Errorf("parseProviderModel(%q) model = %q, want %q", tt.input, m, tt.pModel)
			}
		})
	}
}

// --- normalizeName ---

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"OpenAI", "openai"},
		{"  OpenAI  ", "openai"},
		{"ANTHROPIC", "anthropic"},
		{"ollama", "ollama"},
		{"  ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeName(tt.input)
			if got != tt.want {
				t.Errorf("normalizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- Global Registry ---

func TestGlobalRegistry(t *testing.T) {
	// Save and restore
	defer GlobalRegistry.Clear()

	factory := func(cfg config.ProviderConfig) (Provider, error) {
		return &mockProvider{name: "global-test"}, nil
	}

	err := Register("global-test", factory, config.ProviderConfig{})
	if err != nil {
		t.Fatalf("global Register() error = %v", err)
	}

	p, err := Get("global-test")
	if err != nil {
		t.Fatalf("global Get() error = %v", err)
	}
	if p.Name() != "global-test" {
		t.Errorf("global Get() name = %q, want %q", p.Name(), "global-test")
	}
}

func TestGlobalRegistry_MustRegisterAndAlias(t *testing.T) {
	defer GlobalRegistry.Clear()

	factory := func(cfg config.ProviderConfig) (Provider, error) {
		return &mockProvider{name: "openai"}, nil
	}

	MustRegister("openai", factory, config.ProviderConfig{DefaultModel: "gpt-4-turbo"})
	MustAlias("gpt4", "openai::gpt-4-turbo")

	p, model, err := Resolve("gpt4")
	if err != nil {
		t.Fatalf("global Resolve() error = %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("global Resolve() provider = %q, want %q", p.Name(), "openai")
	}
	if model != "gpt-4-turbo" {
		t.Errorf("global Resolve() model = %q, want %q", model, "gpt-4-turbo")
	}
}

// --- Full workflow ---

func TestRegistry_FullWorkflow(t *testing.T) {
	r := NewRegistry()

	// Register providers
	openaiFactory := func(cfg config.ProviderConfig) (Provider, error) {
		return &mockProvider{
			name:   "openai",
			models: []ModelInfo{{ID: "gpt-4-turbo"}, {ID: "gpt-4o"}},
		}, nil
	}
	anthropicFactory := func(cfg config.ProviderConfig) (Provider, error) {
		return &mockProvider{
			name:   "anthropic",
			models: []ModelInfo{{ID: "claude-sonnet-4-20250514"}},
		}, nil
	}

	_ = r.Register("openai", openaiFactory, config.ProviderConfig{
		DefaultModel: "gpt-4-turbo",
	})
	_ = r.Register("anthropic", anthropicFactory, config.ProviderConfig{
		DefaultModel: "claude-sonnet-4-20250514",
	})

	// Set aliases
	_ = r.Alias("gpt4", "openai::gpt-4-turbo")
	_ = r.Alias("claude", "anthropic::claude-sonnet-4-20250514")

	// List
	providers := r.ListProviders()
	if len(providers) != 2 {
		t.Fatalf("ListProviders() returned %d, want 2", len(providers))
	}

	// Resolve via alias
	p, model, err := r.Resolve("gpt4")
	if err != nil {
		t.Fatalf("Resolve(gpt4) error = %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("Resolve(gpt4) provider = %q, want %q", p.Name(), "openai")
	}
	if model != "gpt-4-turbo" {
		t.Errorf("Resolve(gpt4) model = %q, want %q", model, "gpt-4-turbo")
	}

	// Resolve via default model
	ref := func() (Provider, string, error) {
		wantProvider := "anthropic"
		wantModel := "claude-sonnet-4-20250514"
		p, m, err := r.Resolve("claude-sonnet-4-20250514")
		if err != nil {
			return nil, "", err
		}
		if p.Name() != wantProvider {
			t.Errorf("Resolve() provider = %q, want %q", p.Name(), wantProvider)
		}
		if m != wantModel {
			t.Errorf("Resolve() model = %q, want %q", m, wantModel)
		}
		return p, m, nil
	}
	if _, _, err := ref(); err != nil {
		t.Fatalf("Resolve(default model) error = %v", err)
	}

	// Resolve via provider::model
	p, model, err = r.Resolve("openai::gpt-4o")
	if err != nil {
		t.Fatalf("Resolve(openai::gpt-4o) error = %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("Resolve(openai::gpt-4o) provider = %q, want %q", p.Name(), "openai")
	}
	if model != "gpt-4o" {
		t.Errorf("Resolve(openai::gpt-4o) model = %q, want %q", model, "gpt-4o")
	}

	// Clear
	r.Clear()
	if len(r.ListProviders()) != 0 {
		t.Error("Clear() should remove all providers")
	}
}
