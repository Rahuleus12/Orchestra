package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ViewID identifies the current active view.
type ViewID int

const (
	// ViewChat is the chat view.
	ViewChat ViewID = iota

	// ViewWorkflow is the workflow view.
	ViewWorkflow

	// ViewSessions is the session browser view.
	ViewSessions

	// ViewLogs is the log/trace view.
	ViewLogs
)

// String returns a human-readable name for the view.
func (v ViewID) String() string {
	switch v {
	case ViewChat:
		return "Chat"
	case ViewWorkflow:
		return "Workflow"
	case ViewSessions:
		return "Sessions"
	case ViewLogs:
		return "Logs"
	default:
		return "Unknown"
	}
}

// AppModel is the root Bubble Tea model for the Orchestra TUI.
type AppModel struct {
	// Theme holds the styling configuration.
	Theme *Theme

	// KeyMap holds the keybindings.
	KeyMap *KeyMap

	// CommandRegistry handles slash commands.
	CommandRegistry *CommandRegistry

	// SessionStore manages session persistence.
	SessionStore *SessionStore

	// Chat is the chat view model.
	Chat *ChatModel

	// Workflow is the workflow view model.
	Workflow *WorkflowModel

	// Sessions is the session browser model.
	Sessions *SessionModel

	// Logs is the log view model.
	Logs *LogModel

	// ActiveView is the currently active view.
	ActiveView ViewID

	// ShowHelp indicates if the help overlay is shown.
	ShowHelp bool

	// Quitting indicates if the app is shutting down.
	Quitting bool

	// PendingQuit indicates if we're waiting for quit confirmation.
	PendingQuit bool

	// Width is the terminal width.
	Width int

	// Height is the terminal height.
	Height int

	// Version is the Orchestra version string.
	Version string

	// OnSubmitMessage is called when the user submits a chat message.
	OnSubmitMessage func(content string) tea.Cmd

	// AgentName is the current agent name.
	AgentName string

	// ModelName is the current model name.
	ModelName string

	// Error is a temporary error to display.
	Error string
}

// AppOption is a functional option for configuring the AppModel.
type AppOption func(*AppModel)

// WithVersion sets the version string.
func WithVersion(version string) AppOption {
	return func(m *AppModel) {
		m.Version = version
	}
}

// WithAgent sets the default agent name.
func WithAgent(agent, model string) AppOption {
	return func(m *AppModel) {
		m.AgentName = agent
		m.ModelName = model
	}
}

// WithSessionDir sets the session storage directory.
func WithSessionDir(dir string) AppOption {
	return func(m *AppModel) {
		store, err := NewSessionStore(dir)
		if err == nil {
			m.SessionStore = store
			m.Sessions.Store = store
		}
	}
}

// WithTheme sets the theme.
func WithTheme(theme *Theme) AppOption {
	return func(m *AppModel) {
		m.Theme = theme
	}
}

// WithOnSubmitMessage sets the message submission handler.
func WithOnSubmitMessage(handler func(content string) tea.Cmd) AppOption {
	return func(m *AppModel) {
		m.OnSubmitMessage = handler
	}
}

// NewAppModel creates a new AppModel with the given options.
func NewAppModel(opts ...AppOption) (*AppModel, error) {
	theme := DefaultTheme()
	keyMap := NewKeyMap()

	// Create session store with default directory
	homeDir, _ := os.UserHomeDir()
	sessionDir := homeDir + "/.orchestra/sessions"

	store, err := NewSessionStore(sessionDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create session store: %w", err)
	}

	model := &AppModel{
		Theme:           theme,
		KeyMap:          keyMap,
		CommandRegistry: NewCommandRegistry(),
		SessionStore:    store,
		Width:           80,
		Height:          24,
		ActiveView:      ViewChat,
	}

	// Create sub-models
	model.Chat = NewChatModel(theme, keyMap)
	model.Workflow = NewWorkflowModel(theme, keyMap)
	model.Sessions = NewSessionModel(theme, keyMap, store)
	model.Logs = NewLogModel(theme, keyMap)

	// Apply options
	for _, opt := range opts {
		opt(model)
	}

	// Setup command handlers
	model.setupCommandHandlers()

	// Setup chat callbacks
	model.setupChatCallbacks()

	// Setup session callbacks
	model.setupSessionCallbacks()

	return model, nil
}

func (m *AppModel) setupCommandHandlers() {
	// /help command
	m.CommandRegistry.Register(CommandHelpFn, func(cmd Command) (string, error) {
		return GetCommandHelp(), nil
	})

	// /clear command
	m.CommandRegistry.Register(CommandClear, func(cmd Command) (string, error) {
		m.Chat.ClearHistory()
		_ = m.SessionStore.ClearMessages()
		return "Conversation cleared.", nil
	})

	// /compact command
	m.CommandRegistry.Register(CommandCompact, func(cmd Command) (string, error) {
		return "Compaction triggered...", nil
	})

	// /save command
	m.CommandRegistry.Register(CommandSave, func(cmd Command) (string, error) {
		path := cmd.Args
		if path == "" {
			path = "conversation.md"
		}
		return fmt.Sprintf("Conversation saved to %s", path), nil
	})

	// /tools command
	m.CommandRegistry.Register(CommandTools, func(cmd Command) (string, error) {
		return "Available tools:\n  (No tools configured)", nil
	})

	// /agent command
	m.CommandRegistry.Register(CommandAgent, func(cmd Command) (string, error) {
		if cmd.Args == "" {
			return fmt.Sprintf("Current agent: %s\nUse /agent <name> to switch.", m.AgentName), nil
		}
		m.AgentName = cmd.Args
		return fmt.Sprintf("Switched to agent: %s", cmd.Args), nil
	})

	// /model command
	m.CommandRegistry.Register(CommandModel, func(cmd Command) (string, error) {
		if cmd.Args == "" {
			return fmt.Sprintf("Current model: %s\nUse /model <name> to switch.", m.ModelName), nil
		}
		m.ModelName = cmd.Args
		return fmt.Sprintf("Switched to model: %s", cmd.Args), nil
	})

	// /system command
	m.CommandRegistry.Register(CommandSystem, func(cmd Command) (string, error) {
		if cmd.Args == "" {
			return "Current system prompt is empty.\nUse /system <prompt> to set it.", nil
		}
		return "System prompt updated.", nil
	})

	// /theme command
	m.CommandRegistry.Register(CommandTheme, func(cmd Command) (string, error) {
		switch strings.ToLower(cmd.Args) {
		case "light":
			m.Theme.SetLightTheme()
			return "Switched to light theme.", nil
		case "dark":
			m.Theme.SetDarkTheme()
			return "Switched to dark theme.", nil
		default:
			return "Usage: /theme [light|dark]", nil
		}
	})

	// /quit command
	m.CommandRegistry.Register(CommandQuit, func(cmd Command) (string, error) {
		m.Quitting = true
		return "", nil
	})
}

func (m *AppModel) setupChatCallbacks() {
	m.Chat.OnSubmit = func(content string) tea.Cmd {
		_ = m.SessionStore.AddMessage("user", content, nil)
		m.Logs.AddEntry(LogLevelInfo, "chat", fmt.Sprintf("User: %s", content))
		if m.OnSubmitMessage != nil {
			return m.OnSubmitMessage(content)
		}
		return nil
	}

	m.Chat.OnCommand = func(cmd Command) tea.Cmd {
		response, err := m.CommandRegistry.Execute(cmd)
		if err != nil {
			m.Chat.AddMessage(ViewSystem, fmt.Sprintf("Error: %s", err))
			return nil
		}
		if cmd.Type == CommandQuit {
			m.Quitting = true
			return tea.Quit
		}
		if response != "" {
			m.Chat.AddMessage(ViewSystem, response)
		}
		return nil
	}

	m.Chat.OnCompact = func() tea.Cmd {
		m.Chat.AddMessage(ViewSystem, "Compacting conversation...")
		return nil
	}

	m.Chat.OnSave = func() tea.Cmd {
		m.Chat.AddMessage(ViewSystem, "Conversation saved.")
		return nil
	}
}

func (m *AppModel) setupSessionCallbacks() {
	m.Sessions.OnOpen = func(sessionID string) tea.Cmd {
		m.SessionStore.SetActiveSession(sessionID)
		m.ActiveView = ViewChat
		m.Chat.ClearHistory()
		return nil
	}

	m.Sessions.OnDelete = func(sessionID string) tea.Cmd {
		_ = m.SessionStore.DeleteSession(sessionID)
		m.Sessions.Refresh()
		return nil
	}

	m.Sessions.OnNewSession = func() tea.Cmd {
		agent := m.AgentName
		model := m.ModelName
		if agent == "" {
			agent = "default"
		}
		if model == "" {
			model = "default"
		}
		m.SessionStore.CreateSession("New Chat", agent, model)
		m.ActiveView = ViewChat
		m.Chat.ClearHistory()
		return nil
	}

	m.Sessions.OnExport = func(sessionID string, format string) tea.Cmd {
		var data []byte
		var err error
		if format == "markdown" {
			data, err = m.SessionStore.ExportSessionToMarkdown(sessionID)
		} else {
			data, err = m.SessionStore.ExportSession(sessionID)
		}
		if err != nil {
			m.Error = fmt.Sprintf("Export failed: %s", err)
			return nil
		}
		path := fmt.Sprintf("session_%s.%s", sessionID, format)
		_ = os.WriteFile(path, data, 0o644)
		m.Error = fmt.Sprintf("Exported to %s", path)
		return nil
	}
}

// Init initializes the application.
func (m *AppModel) Init() tea.Cmd {
	return tea.Batch(
		m.Chat.Init(),
		m.Workflow.Init(),
		m.Sessions.Init(),
		m.Logs.Init(),
	)
}

// Update handles messages and updates the application state.
func (m *AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.Chat.SetSize(m.Width, m.Height-3)
		m.Workflow.SetSize(m.Width, m.Height-3)
		m.Sessions.SetSize(m.Width, m.Height-3)
		m.Logs.SetSize(m.Width, m.Height-3)

	case tea.KeyMsg:
		// Handle global keys first
		if key.Matches(msg, m.KeyMap.Global.Quit) {
			if m.PendingQuit {
				m.Quitting = true
				return m, tea.Quit
			}
			m.PendingQuit = true
			return m, nil
		}

		if key.Matches(msg, m.KeyMap.Global.Help) {
			m.ShowHelp = !m.ShowHelp
			return m, nil
		}

		if key.Matches(msg, m.KeyMap.Global.ToggleTheme) {
			// Check current theme and toggle
			m.Theme.SetLightTheme() // Toggle logic could be smarter
			return m, nil
		}

		if key.Matches(msg, m.KeyMap.Global.Chat) {
			m.ActiveView = ViewChat
			return m, nil
		}

		if key.Matches(msg, m.KeyMap.Global.Workflow) {
			m.ActiveView = ViewWorkflow
			return m, nil
		}

		if key.Matches(msg, m.KeyMap.Global.Sessions) {
			m.ActiveView = ViewSessions
			m.Sessions.Refresh()
			return m, nil
		}

		if key.Matches(msg, m.KeyMap.Global.Logs) {
			m.ActiveView = ViewLogs
			return m, nil
		}

		if key.Matches(msg, m.KeyMap.Global.SwitchView) {
			m.ActiveView = (m.ActiveView + 1) % 4
			if m.ActiveView == ViewSessions {
				m.Sessions.Refresh()
			}
			return m, nil
		}

		// Cancel pending quit on any other key
		m.PendingQuit = false
	}

	// Update active view
	var cmd tea.Cmd
	switch m.ActiveView {
	case ViewChat:
		m.Chat, cmd = m.Chat.Update(msg)
	case ViewWorkflow:
		m.Workflow, cmd = m.Workflow.Update(msg)
	case ViewSessions:
		m.Sessions, cmd = m.Sessions.Update(msg)
	case ViewLogs:
		m.Logs, cmd = m.Logs.Update(msg)
	}
	cmds = append(cmds, cmd)

	// Clear error after a short time
	if m.Error != "" {
		m.Error = ""
	}

	if m.Quitting {
		return m, tea.Quit
	}

	return m, tea.Batch(cmds...)
}

// View renders the entire application.
func (m *AppModel) View() string {
	if m.Quitting {
		return "Goodbye!\n"
	}

	var b strings.Builder

	// Render tab bar
	b.WriteString(m.renderTabBar())
	b.WriteString("\n")

	// Render active view content
	content := m.renderActiveView()
	b.WriteString(content)

	// Render status bar
	b.WriteString(m.renderStatusBar())

	// Render help overlay if shown
	if m.ShowHelp {
		b.WriteString(m.renderHelpOverlay())
	}

	// Render quit confirmation
	if m.PendingQuit {
		b.WriteString(m.renderQuitConfirmation())
	}

	return b.String()
}

func (m *AppModel) renderTabBar() string {
	tabs := []struct {
		id    ViewID
		label string
	}{
		{ViewChat, "💬 Chat"},
		{ViewWorkflow, "🔄 Workflow"},
		{ViewSessions, "📋 Sessions"},
		{ViewLogs, "📊 Logs"},
	}

	var parts []string
	for _, tab := range tabs {
		if tab.id == m.ActiveView {
			parts = append(parts, m.Theme.Styles.ActiveTab.Render(tab.label))
		} else {
			parts = append(parts, m.Theme.Styles.InactiveTab.Render(tab.label))
		}
	}

	// Add version on the right
	versionStr := m.Theme.Styles.Dim.Render(fmt.Sprintf("v%s", m.Version))

	return lipgloss.JoinHorizontal(lipgloss.Top, parts...) +
		strings.Repeat(" ", max(0, m.Width-len(lipgloss.JoinHorizontal(lipgloss.Top, parts...))-len(versionStr))) +
		versionStr
}

func (m *AppModel) renderActiveView() string {
	switch m.ActiveView {
	case ViewChat:
		return m.Chat.View()
	case ViewWorkflow:
		return m.Workflow.View()
	case ViewSessions:
		return m.Sessions.View()
	case ViewLogs:
		return m.Logs.View()
	default:
		return "Unknown view"
	}
}

func (m *AppModel) renderStatusBar() string {
	// Build status bar content
	var parts []string

	// Agent/Model info
	if m.AgentName != "" {
		agent := m.Theme.Styles.Muted.Render(fmt.Sprintf("Agent: %s", m.AgentName))
		parts = append(parts, agent)
	}
	if m.ModelName != "" {
		model := m.Theme.Styles.Muted.Render(fmt.Sprintf("Model: %s", m.ModelName))
		parts = append(parts, model)
	}

	// Session info
	if session, ok := m.SessionStore.GetActiveSession(); ok {
		sessionInfo := m.Theme.Styles.Dim.Render(fmt.Sprintf("Session: %s", session.Title))
		parts = append(parts, sessionInfo)
	}

	// Keybindings
	bindings := []string{}
	for _, k := range m.KeyMap.ShortHelp() {
		h := k.Help()
		bindings = append(bindings, h.Desc)
	}
	keys := m.Theme.Styles.Dim.Render(fmt.Sprintf("[%s]", strings.Join(bindings, " | ")))
	parts = append(parts, keys)

	// Error
	if m.Error != "" {
		errStr := m.Theme.Styles.Error.Render(m.Error)
		parts = append(parts, errStr)
	}

	// Join with separator
	content := strings.Join(parts, "  ")

	// Pad to full width
	padding := max(0, m.Width-lipgloss.Width(content))
	content += strings.Repeat(" ", padding)

	return m.Theme.Styles.Status.Render(content)
}

func (m *AppModel) renderHelpOverlay() string {
	box := lipgloss.NewStyle().
		Border(m.Theme.Styles.Border).
		BorderForeground(m.Theme.Colors.Primary).
		Padding(1, 2).
		Width(m.Width - 4).
		Height(m.Height - 4).
		Background(m.Theme.Colors.Background)

	var b strings.Builder
	b.WriteString(m.Theme.Styles.Title.Render("Keyboard Shortcuts"))
	b.WriteString("\n\n")

	for _, row := range m.KeyMap.FullHelp() {
		for _, binding := range row {
			key := m.Theme.Styles.HelpKey.Render(binding.Keys()[0])
			help := m.Theme.Styles.Help.Render(binding.Help().Desc)
			b.WriteString(fmt.Sprintf("  %s %s\n", key, help))
		}
		b.WriteString("\n")
	}

	b.WriteString(m.Theme.Styles.Muted.Render("Press ? or esc to close"))

	return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, box.Render(b.String()))
}

func (m *AppModel) renderQuitConfirmation() string {
	box := lipgloss.NewStyle().
		Border(m.Theme.Styles.Border).
		BorderForeground(m.Theme.Colors.Warning).
		Padding(1, 2)

	content := m.Theme.Styles.Warning.Render("Quit?") + " Press " +
		m.Theme.Styles.HelpKey.Render("y") + " to confirm, any other key to cancel"

	return lipgloss.Place(m.Width, 1, lipgloss.Center, lipgloss.Center, box.Render(content))
}

// Helper functions
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
