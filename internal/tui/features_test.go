package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// --- Keys view: delete key confirmation -----------------------------------

func newModelsTestModel(t *testing.T) *ModelsModel {
	t.Helper()
	theme := DefaultTheme()
	km, err := NewKeyManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewKeyManager() error = %v", err)
	}
	return NewModelsModel(theme, NewKeyMap(), km, NewDefaultModelFetcher())
}

func TestDeleteKey_RequiresConfirmation(t *testing.T) {
	m := newModelsTestModel(t)
	if err := m.KeyManager.AddKey("openai", "sk-test-1234", "", ""); err != nil {
		t.Fatalf("AddKey() error = %v", err)
	}
	m.RefreshProviders()
	m.SetSize(100, 30)
	m.Ready = true

	// Press 'd' on the selected provider (openai is first).
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = updated

	if !m.IsConfirmingDelete {
		t.Fatal("expected delete confirmation to be shown after 'd'")
	}
	if m.DeleteTargetProvider != "openai" {
		t.Errorf("DeleteTargetProvider = %q, want openai", m.DeleteTargetProvider)
	}
	// Key must still exist until confirmed.
	if _, ok := m.KeyManager.GetKey("openai"); !ok {
		t.Fatal("key was removed without confirmation")
	}

	// Escape cancels.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated
	if m.IsConfirmingDelete {
		t.Error("expected confirmation to be cancelled by esc")
	}
	if _, ok := m.KeyManager.GetKey("openai"); !ok {
		t.Error("key should still exist after cancelling")
	}

	// Enter confirms the deletion.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = updated
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated
	if _, ok := m.KeyManager.GetKey("openai"); ok {
		t.Error("key should be removed after confirming with enter")
	}
	if !strings.Contains(m.StatusMessage, "Removed key for OpenAI") {
		t.Errorf("StatusMessage = %q, want removal confirmation", m.StatusMessage)
	}
}

func TestDeleteKey_NoKeyShowsFeedback(t *testing.T) {
	m := newModelsTestModel(t)
	m.RefreshProviders()
	m.SetSize(100, 30)
	m.Ready = true

	// Select a provider without a key (no keys exist at all).
	idx := -1
	for i, p := range m.Providers {
		if !p.HasKey {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("expected at least one keyless provider")
	}
	m.SelectedProvider = idx

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = updated
	if m.IsConfirmingDelete {
		t.Error("should not ask to delete when no key is stored")
	}
	if !strings.Contains(m.Error, "No API key stored") {
		t.Errorf("Error = %q, want feedback about missing key", m.Error)
	}
}

func TestDeleteKeyOverlay_Renders(t *testing.T) {
	m := newModelsTestModel(t)
	_ = m.KeyManager.AddKey("anthropic", "sk-ant-test", "", "")
	m.RefreshProviders()
	m.SetSize(100, 30)
	m.Ready = true
	m.IsConfirmingDelete = true
	m.DeleteTargetProvider = "anthropic"

	view := m.View()
	if !strings.Contains(view, "Delete API Key") {
		t.Errorf("overlay missing title, got: %s", view)
	}
	if !strings.Contains(view, "Anthropic") {
		t.Errorf("overlay missing provider name, got: %s", view)
	}
}

// --- Chat: thinking window -------------------------------------------------

func TestThinking_AccumulatesAndAttachesToMessage(t *testing.T) {
	theme := DefaultTheme()
	m := NewChatModel(theme, NewKeyMap())

	m.StartStreaming()
	m.AppendThinkingChunk("Let me consider ")
	m.AppendThinkingChunk("the options.")
	m.AppendStreamChunk("The answer is 4.")
	m.EndStreaming()

	if len(m.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(m.Messages))
	}
	msg := m.Messages[0]
	if msg.Content != "The answer is 4." {
		t.Errorf("Content = %q, want %q", msg.Content, "The answer is 4.")
	}
	if msg.Thinking != "Let me consider the options." {
		t.Errorf("Thinking = %q, want accumulated thinking text", msg.Thinking)
	}
	if m.StreamingThinking != "" {
		t.Errorf("StreamingThinking not reset: %q", m.StreamingThinking)
	}
}

func TestThinking_ToggleHidesWindow(t *testing.T) {
	theme := DefaultTheme()
	km := NewKeyMap()
	m := NewChatModel(theme, km)
	m.SetSize(100, 30)
	m.Ready = true
	m.Messages = []ChatMessage{
		{Type: ViewAssistant, Content: "Answer", Thinking: "secret reasoning"},
	}

	// Default: thinking shown.
	if !m.ShowThinking {
		t.Fatal("ShowThinking should default to true")
	}
	if view := m.View(); !strings.Contains(view, "secret reasoning") {
		t.Errorf("expected thinking content in view, got: %s", view)
	}

	// ctrl+p hides it.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = updated
	if m.ShowThinking {
		t.Error("ShowThinking should be false after ctrl+p")
	}
	view := m.View()
	if strings.Contains(view, "secret reasoning") {
		t.Error("thinking content should be hidden after toggle")
	}
	if !strings.Contains(view, "ctrl+p to show") {
		t.Errorf("expected collapsed hint in view, got: %s", view)
	}

	// ctrl+p shows it again.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = updated
	if !m.ShowThinking {
		t.Error("ShowThinking should be true after second ctrl+p")
	}
}

func TestThinking_LiveWindowWhileStreaming(t *testing.T) {
	theme := DefaultTheme()
	m := NewChatModel(theme, NewKeyMap())
	m.SetSize(100, 30)
	m.Ready = true

	m.StartStreaming()
	m.AppendThinkingChunk("working it out...")
	view := m.View()
	if !strings.Contains(view, "working it out...") {
		t.Errorf("live thinking window missing from view, got: %s", view)
	}
	if !strings.Contains(view, "Thinking...") {
		t.Errorf("live thinking window should show an active title, got: %s", view)
	}
}

// --- Chat runtime: thinking events from a provider stream -------------------

// sseReasoningChunk builds one OpenAI-style streaming chunk with reasoning.
func sseReasoningChunk(model, reasoning, content string) string {
	delta := map[string]any{}
	if reasoning != "" {
		delta["reasoning"] = reasoning
	}
	if content != "" {
		delta["content"] = content
	}
	chunk := map[string]any{
		"id": "chatcmpl-test", "object": "chat.completion.chunk", "created": 1, "model": model,
		"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": ""}},
	}
	b, _ := json.Marshal(chunk)
	return "data: " + string(b) + "\n\n"
}

func TestStreamThinking_ReasoningShownInWindow(t *testing.T) {
	events := []string{
		sseReasoningChunk("test-model", "Thinking hard ", ""),
		sseReasoningChunk("test-model", "about it.", ""),
		sseReasoningChunk("test-model", "", "Answer: 42"),
		sseChunk("test-model", "", "stop"),
		"data: [DONE]\n\n",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, evt := range events {
			fmt.Fprint(w, evt)
		}
	}))
	defer srv.Close()

	t.Setenv("LLAMACPP_BASE_URL", srv.URL)

	m := newRuntimeTestModel(t)
	m.ProviderName = "llamacpp"
	m.ModelName = "test-model"

	m.Chat.AddMessage(ViewUser, "hi")
	cmd := m.Chat.OnSubmit("hi")
	if cmd == nil {
		t.Fatal("OnSubmit returned nil cmd")
	}
	driveStream(t, m, cmd)

	if len(m.Chat.Messages) == 0 {
		t.Fatal("expected messages")
	}
	last := m.Chat.Messages[len(m.Chat.Messages)-1]
	if last.Type != ViewAssistant {
		t.Fatalf("expected assistant message, got %v", last.Type)
	}
	if last.Content != "Answer: 42" {
		t.Errorf("Content = %q, want %q", last.Content, "Answer: 42")
	}
	if last.Thinking != "Thinking hard about it." {
		t.Errorf("Thinking = %q, want accumulated reasoning", last.Thinking)
	}
}

// --- Topbar shortcut keys ---------------------------------------------------

func TestTabBar_ShowsShortcutKeys(t *testing.T) {
	theme := DefaultTheme()
	km := &KeyManager{keys: make(map[string]*ProviderKey), dir: t.TempDir()}
	m := &AppModel{
		Theme:      theme,
		KeyMap:     NewKeyMap(),
		Chat:       NewChatModel(theme, NewKeyMap()),
		Logs:       NewLogModel(theme, NewKeyMap()),
		Models:     NewModelsModel(theme, NewKeyMap(), km, NewDefaultModelFetcher()),
		Width:      120,
		Height:     40,
		ActiveView: ViewChat,
	}

	view := m.renderTabBar()
	// Shortcut hints must come straight from the keymap so the topbar can
	// never drift from the actual bindings.
	for _, b := range []key.Binding{m.KeyMap.Global.Chat, m.KeyMap.Global.Workflow, m.KeyMap.Global.Sessions, m.KeyMap.Global.Logs, m.KeyMap.Global.Models} {
		want := b.Help().Key
		if !strings.Contains(view, want) {
			t.Errorf("tab bar missing shortcut %q, got: %s", want, view)
		}
	}
	for _, want := range []string{"Chat", "Workflow", "Sessions", "Logs", "Keys"} {
		if !strings.Contains(view, want) {
			t.Errorf("tab bar missing %q, got: %s", want, view)
		}
	}
}

// --- Stuck-loading fixes ---------------------------------------------------

// newAppTestModel builds an AppModel with all five views wired, for testing
// app-level message routing.
func newAppTestModel(t *testing.T) *AppModel {
	t.Helper()
	theme := DefaultTheme()
	km := &KeyManager{keys: make(map[string]*ProviderKey), dir: t.TempDir()}
	store, err := NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSessionStore() error = %v", err)
	}
	m := &AppModel{
		Theme:        theme,
		KeyMap:       NewKeyMap(),
		Chat:         NewChatModel(theme, NewKeyMap()),
		Workflow:     NewWorkflowModel(theme, NewKeyMap()),
		Sessions:     NewSessionModel(theme, NewKeyMap(), store),
		Logs:         NewLogModel(theme, NewKeyMap()),
		Models:       NewModelsModel(theme, NewKeyMap(), km, NewDefaultModelFetcher()),
		SessionStore: store,
		KeyManager:   km,
		Width:        120,
		Height:       40,
		ActiveView:   ViewChat,
	}
	return m
}

// TestWindowSizeMsg_ReadiesAllViews verifies that a window resize marks every
// view ready — previously only the active view became ready, so switching
// tabs showed "Loading..." forever.
func TestWindowSizeMsg_ReadiesAllViews(t *testing.T) {
	m := newAppTestModel(t)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(*AppModel)

	for _, tc := range []struct {
		name  string
		ready bool
	}{
		{"Chat", m.Chat.Ready},
		{"Workflow", m.Workflow.Ready},
		{"Sessions", m.Sessions.Ready},
		{"Logs", m.Logs.Ready},
		{"Models", m.Models.Ready},
	} {
		if !tc.ready {
			t.Errorf("%s view should be Ready after WindowSizeMsg", tc.name)
		}
	}

	// Every view must render content, not the "Loading..." placeholder.
	for _, view := range []struct {
		name string
		out  string
	}{
		{"Workflow", m.Workflow.View()},
		{"Sessions", m.Sessions.View()},
		{"Logs", m.Logs.View()},
		{"Models", m.Models.View()},
	} {
		if strings.Contains(view.out, "Loading...") {
			t.Errorf("%s view stuck on Loading...", view.name)
		}
	}
}

// TestModelsView_EscClosesDialogNotSwitchView verifies that Esc inside a
// keys-view dialog cancels the dialog instead of jumping to chat and leaving
// it stuck open.
func TestModelsView_EscClosesDialogNotSwitchView(t *testing.T) {
	m := newAppTestModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(*AppModel)
	_ = m.KeyManager.AddKey("openai", "sk-test", "", "")
	m.Models.RefreshProviders()
	m.ActiveView = ViewModels

	// Open the delete dialog.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = updated.(*AppModel)
	if !m.Models.IsConfirmingDelete {
		t.Fatal("expected delete dialog")
	}

	// Esc must close the dialog, not switch to chat.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(*AppModel)
	if m.ActiveView != ViewModels {
		t.Errorf("ActiveView = %v, want ViewModels (Esc was hijacked)", m.ActiveView)
	}
	if m.Models.IsConfirmingDelete {
		t.Error("delete dialog should be closed after Esc")
	}

	// Same for the add-key dialog.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = updated.(*AppModel)
	if !m.Models.IsAddingKey {
		t.Fatal("expected add-key dialog")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(*AppModel)
	if m.ActiveView != ViewModels {
		t.Errorf("ActiveView = %v, want ViewModels", m.ActiveView)
	}
	if m.Models.IsAddingKey {
		t.Error("add-key dialog should be closed after Esc")
	}
}

// TestModelsFetchResult_RoutedWhenInactive verifies that a fetch result that
// arrives while the user is in another view still clears the loading state —
// previously it was dropped and the keys view stayed in "Fetching models...".
func TestModelsFetchResult_RoutedWhenInactive(t *testing.T) {
	m := newAppTestModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(*AppModel)
	m.ActiveView = ViewChat
	m.Models.IsLoading = true

	updated, _ = m.Update(modelsFetchResultMsg{
		Provider: "openai",
		Models:   []ModelEntry{{ID: "gpt-test", Provider: "openai"}},
	})
	m = updated.(*AppModel)

	if m.Models.IsLoading {
		t.Error("IsLoading should be false after the fetch result")
	}
	if len(m.Models.Models) != 1 || m.Models.Models[0].ID != "gpt-test" {
		t.Errorf("models not stored: %+v", m.Models.Models)
	}
}

// TestSessionsView_EscClosesSearch verifies Esc closes the sessions search
// input instead of switching views with the input stuck focused.
func TestSessionsView_EscClosesSearch(t *testing.T) {
	m := newAppTestModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(*AppModel)
	m.ActiveView = ViewSessions

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(*AppModel)
	if !m.Sessions.IsSearching {
		t.Fatal("expected sessions search mode")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(*AppModel)
	if m.ActiveView != ViewSessions {
		t.Errorf("ActiveView = %v, want ViewSessions", m.ActiveView)
	}
	if m.Sessions.IsSearching {
		t.Error("search mode should be closed after Esc")
	}
}

// TestAltDigitSwitchesViews verifies the alt+digit view shortcuts, which are
// the universally deliverable chords (ctrl+digit does not reach the app in
// most terminals — it arrives as a legacy C0 control code instead).
func TestAltDigitSwitchesViews(t *testing.T) {
	theme := DefaultTheme()
	km := &KeyManager{keys: make(map[string]*ProviderKey), dir: t.TempDir()}
	m := &AppModel{
		Theme:      theme,
		KeyMap:     NewKeyMap(),
		Chat:       NewChatModel(theme, NewKeyMap()),
		Logs:       NewLogModel(theme, NewKeyMap()),
		Models:     NewModelsModel(theme, NewKeyMap(), km, NewDefaultModelFetcher()),
		Width:      120,
		Height:     40,
		ActiveView: ViewChat,
	}

	switches := []struct {
		digit rune
		want  ViewID
	}{
		{'2', ViewWorkflow},
		{'5', ViewModels},
		{'1', ViewChat},
	}
	for _, s := range switches {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{s.digit}, Alt: true}
		if _, cmd := m.Update(msg); cmd != nil {
			_ = cmd()
		}
		if m.ActiveView != s.want {
			t.Errorf("alt+%c: ActiveView = %v, want %v", s.digit, m.ActiveView, s.want)
		}
	}
}
