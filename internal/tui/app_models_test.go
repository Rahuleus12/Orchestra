package tui

import "testing"

// TestOnSelectModel_SetsActiveModel verifies that the models view's
// "use model" action sets the active chat model (previously the
// OnSelectModel callback was never wired up, so pressing 'u' was a no-op).
func TestOnSelectModel_SetsActiveModel(t *testing.T) {
	theme := DefaultTheme()
	km := &KeyManager{keys: make(map[string]*ProviderKey), dir: t.TempDir()}
	m := &AppModel{
		Theme:  theme,
		KeyMap: NewKeyMap(),
		Chat:   NewChatModel(theme, NewKeyMap()),
		Logs:   NewLogModel(theme, NewKeyMap()),
		Models: NewModelsModel(theme, NewKeyMap(), km, NewDefaultModelFetcher()),
	}
	m.setupModelsCallbacks()

	if m.Models.OnSelectModel == nil {
		t.Fatal("OnSelectModel not wired")
	}

	cmd := m.Models.OnSelectModel("openrouter", "test/free-model")
	if cmd != nil {
		t.Error("expected nil cmd from OnSelectModel")
	}
	if m.ProviderName != "openrouter" {
		t.Errorf("ProviderName = %q, want %q", m.ProviderName, "openrouter")
	}
	if m.ModelName != "test/free-model" {
		t.Errorf("ModelName = %q, want %q", m.ModelName, "test/free-model")
	}
	if m.Models.StatusMessage == "" {
		t.Error("expected a status message confirming the selection")
	}
}

// TestProviderNeedsKeyForModels_LlamaCpp verifies llama.cpp is treated as a
// keyless provider for model listing.
func TestProviderNeedsKeyForModels_LlamaCpp(t *testing.T) {
	for _, name := range []string{"llamacpp", "llama.cpp"} {
		if providerNeedsKeyForModels(name) {
			t.Errorf("providerNeedsKeyForModels(%q) = true, want false", name)
		}
	}
}
