package tui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestThemeCreation(t *testing.T) {
	theme := DefaultTheme()
	if theme == nil {
		t.Fatal("DefaultTheme returned nil")
	}
	if theme.Styles == nil {
		t.Fatal("Theme styles not initialized")
	}
}

func TestThemeToggle(t *testing.T) {
	theme := DefaultTheme()
	initial := theme.Colors.Primary

	theme.SetLightTheme()
	if theme.Colors.Primary == initial {
		t.Error("Primary color did not change after SetLightTheme")
	}

	theme.SetDarkTheme()
	if theme.Colors.Primary != initial {
		t.Error("Primary color did not reset after SetDarkTheme")
	}
}

func TestKeyMapCreation(t *testing.T) {
	km := NewKeyMap()
	if km == nil {
		t.Fatal("NewKeyMap returned nil")
	}
	if len(km.Enabled()) == 0 {
		t.Error("No enabled keys")
	}
	if len(km.ShortHelp()) == 0 {
		t.Error("No short help keys")
	}
	if len(km.FullHelp()) == 0 {
		t.Error("No full help keys")
	}
}

func TestCommandParsing(t *testing.T) {
	reg := NewCommandRegistry()

	tests := []struct {
		input   string
		isCmd   bool
		cmdType CommandType
		args    string
	}{
		{"/help", true, CommandHelpFn, ""},
		{"/agent gpt-4", true, CommandAgent, "gpt-4"},
		{"/model claude", true, CommandModel, "claude"},
		{"/clear", true, CommandClear, ""},
		{"/save output.md", true, CommandSave, "output.md"},
		{"/theme dark", true, CommandTheme, "dark"},
		{"/unknown", true, CommandType("unknown"), ""},
		{"regular text", false, "", ""},
		{"  /help  ", true, CommandHelpFn, ""},
		{"/h", true, CommandHelpFn, ""}, // Alias
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			cmd := reg.Parse(tt.input)
			if tt.isCmd {
				if cmd == nil {
					t.Fatalf("expected command, got nil")
				}
				if cmd.Type != tt.cmdType {
					t.Errorf("expected type %v, got %v", tt.cmdType, cmd.Type)
				}
				if cmd.Args != tt.args {
					t.Errorf("expected args %q, got %q", tt.args, cmd.Args)
				}
			} else {
				if cmd != nil {
					t.Errorf("expected nil, got command")
				}
			}
		})
	}
}

func TestCommandExecution(t *testing.T) {
	reg := NewCommandRegistry()

	helpCalled := false
	reg.Register(CommandHelpFn, func(cmd Command) (string, error) {
		helpCalled = true
		return "help text", nil
	})

	cmd := reg.Parse("/help")
	if cmd == nil {
		t.Fatal("failed to parse /help")
	}

	result, err := reg.Execute(*cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !helpCalled {
		t.Error("help handler not called")
	}
	if result != "help text" {
		t.Errorf("expected 'help text', got %q", result)
	}
}

func TestSessionStore(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSessionStore(dir)
	if err != nil {
		t.Fatalf("failed to create session store: %v", err)
	}

	// Create a session
	session := store.CreateSession("Test Session", "test-agent", "gpt-4")
	if session == nil {
		t.Fatal("CreateSession returned nil")
	}
	if session.Title != "Test Session" {
		t.Errorf("expected title 'Test Session', got %q", session.Title)
	}

	// Add a message
	err = store.AddMessage("user", "Hello world", nil)
	if err != nil {
		t.Fatalf("failed to add message: %v", err)
	}

	// Verify message was added
	active, ok := store.GetActiveSession()
	if !ok {
		t.Fatal("no active session")
	}
	if len(active.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(active.Messages))
	}

	// List sessions
	sessions := store.ListSessions()
	if len(sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(sessions))
	}

	// Search sessions
	results := store.SearchSessions("Test")
	if len(results) != 1 {
		t.Errorf("expected 1 search result, got %d", len(results))
	}

	results = store.SearchSessions("nonexistent")
	if len(results) != 0 {
		t.Errorf("expected 0 search results, got %d", len(results))
	}

	// Clear messages
	err = store.ClearMessages()
	if err != nil {
		t.Fatalf("failed to clear messages: %v", err)
	}
	active, ok = store.GetActiveSession()
	if !ok || len(active.Messages) != 0 {
		t.Error("messages not cleared")
	}

	// Delete session
	err = store.DeleteSession(session.ID)
	if err != nil {
		t.Fatalf("failed to delete session: %v", err)
	}
	sessions = store.ListSessions()
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions after delete, got %d", len(sessions))
	}
}

func TestSessionExport(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSessionStore(dir)
	if err != nil {
		t.Fatalf("failed to create session store: %v", err)
	}

	session := store.CreateSession("Export Test", "agent", "model")
	_ = store.AddMessage("user", "Export Test", nil)
	_ = store.AddMessage("assistant", "Hi there!", &TokenUsageInfo{
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
	})

	// Export as JSON
	jsonData, err := store.ExportSession(session.ID)
	if err != nil {
		t.Fatalf("failed to export JSON: %v", err)
	}
	if len(jsonData) == 0 {
		t.Error("JSON export is empty")
	}

	// Export as Markdown
	mdData, err := store.ExportSessionToMarkdown(session.ID)
	if err != nil {
		t.Fatalf("failed to export Markdown: %v", err)
	}
	if len(mdData) == 0 {
		t.Error("Markdown export is empty")
	}
	if !containsString(string(mdData), "# Export Test") {
		t.Errorf("Markdown export missing title, got: %s", string(mdData)[:min(100, len(mdData))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestChatModel(t *testing.T) {
	theme := DefaultTheme()
	km := NewKeyMap()
	model := NewChatModel(theme, km)

	// Test Init
	cmd := model.Init()
	if cmd == nil {
		t.Error("Init returned nil cmd")
	}

	// Test AddMessage
	model.AddMessage(ViewUser, "Hello")
	if len(model.Messages) != 1 {
		t.Error("Message not added")
	}

	// Test ClearHistory
	model.ClearHistory()
	if len(model.Messages) != 0 {
		t.Error("History not cleared")
	}

	// Test streaming
	model.StartStreaming()
	if !model.IsStreaming {
		t.Error("IsStreaming not set")
	}
	model.AppendStreamChunk("Hello ")
	model.AppendStreamChunk("World")
	if model.StreamingContent != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", model.StreamingContent)
	}
	model.EndStreaming()
	if model.IsStreaming {
		t.Error("IsStreaming still set after EndStreaming")
	}
}

func TestWorkflowModel(t *testing.T) {
	theme := DefaultTheme()
	km := NewKeyMap()
	model := NewWorkflowModel(theme, km)

	steps := []WorkflowStep{
		{ID: "step1", Name: "Step 1", Agent: "agent1", Status: StepPending},
		{ID: "step2", Name: "Step 2", Agent: "agent2", Status: StepPending, Dependencies: []string{"step1"}},
	}
	model.SetSteps(steps)

	if len(model.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(model.Steps))
	}

	// Test step update
	model.UpdateStep("step1", StepRunning, "")
	if model.Steps[0].Status != StepRunning {
		t.Error("step status not updated")
	}

	model.UpdateStep("step1", StepDone, "output")
	if model.Steps[0].Status != StepDone {
		t.Error("step status not updated to done")
	}
}

func TestLogModel(t *testing.T) {
	theme := DefaultTheme()
	km := NewKeyMap()
	model := NewLogModel(theme, km)

	// Test AddEntry
	model.AddEntry(LogLevelInfo, "test", "Test message")
	if len(model.Entries) != 1 {
		t.Error("Entry not added")
	}

	model.AddEntry(LogLevelError, "error", "Error occurred")
	if len(model.Entries) != 2 {
		t.Error("Second entry not added")
	}

	// Test filter
	model.SetFilter(LogLevelError)
	if len(model.filteredEntries()) != 1 {
		t.Errorf("expected 1 filtered entry, got %d", len(model.filteredEntries()))
	}

	// Test clear
	model.Clear()
	if len(model.Entries) != 0 {
		t.Error("Entries not cleared")
	}
}

func TestMarkdownRenderer(t *testing.T) {
	renderer, err := NewMarkdownRenderer(80)
	if err != nil {
		t.Fatalf("failed to create renderer: %v", err)
	}

	// Test basic rendering
	output, err := renderer.Render("**bold** text")
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if len(output) == 0 {
		t.Error("render output is empty")
	}

	// Test inline rendering
	inline := renderer.RenderInline("# Title\nContent")
	if containsString(inline, "\n") {
		t.Error("inline render should not contain newlines")
	}
}

func TestAppModel(t *testing.T) {
	model, err := NewAppModel()
	if err != nil {
		t.Fatalf("failed to create app model: %v", err)
	}

	// Test Init
	cmd := model.Init()
	if cmd == nil {
		t.Error("Init returned nil cmd")
	}

	// Test view switching
	if model.ActiveView != ViewChat {
		t.Error("Default view should be Chat")
	}

	model.ActiveView = ViewWorkflow
	if model.ActiveView != ViewWorkflow {
		t.Error("View not switched")
	}

	// Test View method (should not panic)
	view := model.View()
	if len(view) == 0 {
		t.Error("View returned empty string")
	}
}

func TestWindowResize(t *testing.T) {
	theme := DefaultTheme()
	km := NewKeyMap()
	model := NewChatModel(theme, km)

	msg := tea.WindowSizeMsg{Width: 120, Height: 40}
	_, _ = model.Update(msg)

	if model.Width != 120 {
		t.Errorf("expected width 120, got %d", model.Width)
	}
	if model.Height != 40 {
		t.Errorf("expected height 40, got %d", model.Height)
	}
}

// Helper function
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && containsSubstring(s, substr)
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Benchmark tests
func BenchmarkSessionStoreCreate(b *testing.B) {
	dir := b.TempDir()
	store, _ := NewSessionStore(dir)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.CreateSession(
			fmt.Sprintf("Session %d", i),
			"agent",
			"model",
		)
	}
}

func BenchmarkSessionStoreAddMessage(b *testing.B) {
	dir := b.TempDir()
	store, _ := NewSessionStore(dir)
	store.CreateSession("Test", "agent", "model")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = store.AddMessage("user", fmt.Sprintf("Message %d", i), nil)
	}
}

func BenchmarkMarkdownRender(b *testing.B) {
	renderer, _ := NewMarkdownRenderer(80)
	content := "# Title\n\n**Bold** and *italic* text.\n\n```\ncode block\n```\n"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = renderer.Render(content)
	}
}
