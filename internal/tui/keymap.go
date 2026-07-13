package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all the keybindings used in the TUI.
type KeyMap struct {
	// Global keys work in all views.
	Global GlobalKeyMap

	// Chat keys work in the chat view.
	Chat ChatKeyMap

	// Workflow keys work in the workflow view.
	Workflow WorkflowKeyMap

	// Session keys work in the session browser view.
	Session SessionKeyMap

	// Help keys control the help overlay.
	Help HelpKeyMap

	// Models keys work in the API keys & models view.
	Models ModelsKeyMap
}

// GlobalKeyMap defines keybindings that work in all views.
type GlobalKeyMap struct {
	// Quit exits the TUI.
	Quit key.Binding

	// Help toggles the help overlay.
	Help key.Binding

	// ToggleTheme switches between light and dark themes.
	ToggleTheme key.Binding

	// NextView cycles to the next view (Tab).
	NextView key.Binding

	// PrevView cycles to the previous view (Shift+Tab).
	PrevView key.Binding

	// BackToChat returns to the chat view from any other view (Esc).
	BackToChat key.Binding

	// SwitchView cycles through available views (legacy ctrl+tab).
	SwitchView key.Binding

	// Chat switches to the chat view.
	Chat key.Binding

	// Workflow switches to the workflow view.
	Workflow key.Binding

	// Sessions switches to the session browser view.
	Sessions key.Binding

	// Logs switches to the log view.
	Logs key.Binding

	// Models switches to the API keys & models view.
	Models key.Binding
}

// ChatKeyMap defines keybindings for the chat view.
type ChatKeyMap struct {
	// Send submits the current input.
	Send key.Binding

	// NewLine inserts a new line in the input.
	NewLine key.Binding

	// ClearInput clears the input field.
	ClearInput key.Binding

	// ClearHistory clears the conversation history.
	ClearHistory key.Binding

	// EditMessage enters edit mode for a previous message.
	EditMessage key.Binding

	// Compact triggers conversation compaction.
	Compact key.Binding

	// Save exports the conversation.
	Save key.Binding

	// ScrollUp scrolls the chat view up.
	ScrollUp key.Binding

	// ScrollDown scrolls the chat view down.
	ScrollDown key.Binding

	// ScrollTop scrolls to the top of the chat.
	ScrollTop key.Binding

	// ScrollBottom scrolls to the bottom of the chat.
	ScrollBottom key.Binding

	// ToggleToolDetails expands/collapses tool call details.
	ToggleToolDetails key.Binding

	// CopyMessage copies the selected message to clipboard.
	CopyMessage key.Binding
}

// WorkflowKeyMap defines keybindings for the workflow view.
type WorkflowKeyMap struct {
	// Start starts or resumes a workflow.
	Start key.Binding

	// Pause pauses a running workflow.
	Pause key.Binding

	// Cancel cancels a running workflow.
	Cancel key.Binding

	// StepDetail shows details for the selected step.
	StepDetail key.Binding

	// SelectNext selects the next step in the DAG.
	SelectNext key.Binding

	// SelectPrev selects the previous step in the DAG.
	SelectPrev key.Binding
}

// SessionKeyMap defines keybindings for the session browser view.
type SessionKeyMap struct {
	// Open opens the selected session.
	Open key.Binding

	// Delete deletes the selected session.
	Delete key.Binding

	// Search filters sessions by keyword.
	Search key.Binding

	// Export exports the selected session.
	Export key.Binding

	// SelectNext selects the next session in the list.
	SelectNext key.Binding

	// SelectPrev selects the previous session in the list.
	SelectPrev key.Binding

	// NewSession creates a new chat session.
	NewSession key.Binding
}

// HelpKeyMap defines keybindings for the help overlay.
type HelpKeyMap struct {
	// Close closes the help overlay.
	Close key.Binding

	// ScrollUp scrolls the help content up.
	ScrollUp key.Binding

	// ScrollDown scrolls the help content down.
	ScrollDown key.Binding
}

// ModelsKeyMap defines keybindings for the API keys & models view.
type ModelsKeyMap struct {
	// SelectNext selects the next item (provider or model).
	SelectNext key.Binding

	// SelectPrev selects the previous item.
	SelectPrev key.Binding

	// Search opens the filter input.
	Search key.Binding

	// AddKey opens the add-key dialog.
	AddKey key.Binding

	// RemoveKey removes the key for the selected provider.
	RemoveKey key.Binding

	// CheckModels fetches models for the selected provider.
	CheckModels key.Binding

	// UseModel switches to the selected model.
	UseModel key.Binding
}

// NewKeyMap creates a new KeyMap with default bindings.
func NewKeyMap() *KeyMap {
	return &KeyMap{
		Global: GlobalKeyMap{
			Quit: key.NewBinding(
				key.WithKeys("ctrl+c"),
				key.WithHelp("ctrl+c", "quit"),
			),
			Help: key.NewBinding(
				key.WithKeys("?"),
				key.WithHelp("?", "help"),
			),
			ToggleTheme: key.NewBinding(
				key.WithKeys("ctrl+t"),
				key.WithHelp("ctrl+t", "toggle theme"),
			),
			NextView: key.NewBinding(
				key.WithKeys("tab"),
				key.WithHelp("tab", "next view"),
			),
			PrevView: key.NewBinding(
				key.WithKeys("shift+tab"),
				key.WithHelp("shift+tab", "prev view"),
			),
			BackToChat: key.NewBinding(
				key.WithKeys("esc"),
				key.WithHelp("esc", "back to chat"),
			),
			SwitchView: key.NewBinding(
				key.WithKeys("ctrl+tab"),
				key.WithHelp("ctrl+tab", "switch view"),
			),
			Chat: key.NewBinding(
				key.WithKeys("ctrl+1"),
				key.WithHelp("ctrl+1", "chat"),
			),
			Workflow: key.NewBinding(
				key.WithKeys("ctrl+2"),
				key.WithHelp("ctrl+2", "workflow"),
			),
			Sessions: key.NewBinding(
				key.WithKeys("ctrl+3"),
				key.WithHelp("ctrl+3", "sessions"),
			),
			Logs: key.NewBinding(
				key.WithKeys("ctrl+4"),
				key.WithHelp("ctrl+4", "logs"),
			),
			Models: key.NewBinding(
				key.WithKeys("ctrl+5"),
				key.WithHelp("ctrl+5", "keys"),
			),
		},
		Chat: ChatKeyMap{
			Send: key.NewBinding(
				key.WithKeys("enter"),
				key.WithHelp("enter", "send"),
			),
			NewLine: key.NewBinding(
				key.WithKeys("ctrl+enter"),
				key.WithHelp("ctrl+enter", "new line"),
			),
			ClearInput: key.NewBinding(
				key.WithKeys("ctrl+u"),
				key.WithHelp("ctrl+u", "clear input"),
			),
			ClearHistory: key.NewBinding(
				key.WithKeys("ctrl+l"),
				key.WithHelp("ctrl+l", "clear history"),
			),
			EditMessage: key.NewBinding(
				key.WithKeys("e"),
				key.WithHelp("e", "edit message"),
			),
			Compact: key.NewBinding(
				key.WithKeys("ctrl+m"),
				key.WithHelp("ctrl+m", "compact"),
			),
			Save: key.NewBinding(
				key.WithKeys("ctrl+s"),
				key.WithHelp("ctrl+s", "save"),
			),
			ScrollUp: key.NewBinding(
				key.WithKeys("up", "pgup"),
				key.WithHelp("↑/pgup", "scroll up"),
			),
			ScrollDown: key.NewBinding(
				key.WithKeys("down", "pgdown"),
				key.WithHelp("↓/pgdown", "scroll down"),
			),
			ScrollTop: key.NewBinding(
				key.WithKeys("home", "g"),
				key.WithHelp("home/g", "scroll top"),
			),
			ScrollBottom: key.NewBinding(
				key.WithKeys("end", "G"),
				key.WithHelp("end/G", "scroll bottom"),
			),
			ToggleToolDetails: key.NewBinding(
				key.WithKeys("ctrl+d"),
				key.WithHelp("ctrl+d", "toggle tool details"),
			),
			CopyMessage: key.NewBinding(
				key.WithKeys("ctrl+y"),
				key.WithHelp("ctrl+y", "copy message"),
			),
		},
		Workflow: WorkflowKeyMap{
			Start: key.NewBinding(
				key.WithKeys("s", "enter"),
				key.WithHelp("s/enter", "start"),
			),
			Pause: key.NewBinding(
				key.WithKeys("p"),
				key.WithHelp("p", "pause"),
			),
			Cancel: key.NewBinding(
				key.WithKeys("ctrl+x"),
				key.WithHelp("ctrl+x", "cancel"),
			),
			StepDetail: key.NewBinding(
				key.WithKeys("d", "enter"),
				key.WithHelp("d/enter", "step detail"),
			),
			SelectNext: key.NewBinding(
				key.WithKeys("down", "j"),
				key.WithHelp("↓/j", "next step"),
			),
			SelectPrev: key.NewBinding(
				key.WithKeys("up", "k"),
				key.WithHelp("↑/k", "prev step"),
			),
		},
		Session: SessionKeyMap{
			Open: key.NewBinding(
				key.WithKeys("enter", "o"),
				key.WithHelp("enter/o", "open"),
			),
			Delete: key.NewBinding(
				key.WithKeys("d", "delete"),
				key.WithHelp("d", "delete"),
			),
			Search: key.NewBinding(
				key.WithKeys("/"),
				key.WithHelp("/", "search"),
			),
			Export: key.NewBinding(
				key.WithKeys("e"),
				key.WithHelp("e", "export"),
			),
			SelectNext: key.NewBinding(
				key.WithKeys("down", "j"),
				key.WithHelp("↓/j", "next"),
			),
			SelectPrev: key.NewBinding(
				key.WithKeys("up", "k"),
				key.WithHelp("↑/k", "prev"),
			),
			NewSession: key.NewBinding(
				key.WithKeys("n"),
				key.WithHelp("n", "new session"),
			),
		},
		Help: HelpKeyMap{
			Close: key.NewBinding(
				key.WithKeys("esc", "?"),
				key.WithHelp("esc/?", "close"),
			),
			ScrollUp: key.NewBinding(
				key.WithKeys("up", "pgup"),
				key.WithHelp("↑/pgup", "scroll up"),
			),
			ScrollDown: key.NewBinding(
				key.WithKeys("down", "pgdown"),
				key.WithHelp("↓/pgdown", "scroll down"),
			),
		},
		Models: ModelsKeyMap{
			SelectNext: key.NewBinding(
				key.WithKeys("down", "j"),
				key.WithHelp("↓/j", "next"),
			),
			SelectPrev: key.NewBinding(
				key.WithKeys("up", "k"),
				key.WithHelp("↑/k", "prev"),
			),
			Search: key.NewBinding(
				key.WithKeys("/"),
				key.WithHelp("/", "filter"),
			),
			AddKey: key.NewBinding(
				key.WithKeys("a"),
				key.WithHelp("a", "add key"),
			),
			RemoveKey: key.NewBinding(
				key.WithKeys("d"),
				key.WithHelp("d", "remove key"),
			),
			CheckModels: key.NewBinding(
				key.WithKeys("enter", "r"),
				key.WithHelp("enter/r", "fetch models"),
			),
			UseModel: key.NewBinding(
				key.WithKeys("u"),
				key.WithHelp("u", "use model"),
			),
		},
	}
}

// Enabled returns all keybindings that should be shown in the help.
func (k *KeyMap) Enabled() []key.Binding {
	return []key.Binding{
		k.Global.Quit,
		k.Global.Help,
		k.Global.NextView,
		k.Global.PrevView,
		k.Global.BackToChat,
		k.Global.ToggleTheme,
		k.Global.Chat,
		k.Global.Workflow,
		k.Global.Sessions,
		k.Global.Logs,
		k.Global.Models,
	}
}

// ShortHelp returns keybindings for the short help (status bar).
func (k *KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Global.NextView,
		k.Global.PrevView,
		k.Global.BackToChat,
		k.Global.Help,
		k.Global.Quit,
	}
}

// FullHelp returns all keybindings for the full help overlay.
func (k *KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		// Navigation
		{k.Global.NextView, k.Global.PrevView, k.Global.BackToChat, k.Global.SwitchView},
		{k.Global.Chat, k.Global.Workflow, k.Global.Sessions, k.Global.Logs, k.Global.Models},
		{k.Global.Quit, k.Global.Help, k.Global.ToggleTheme},
		// Chat
		{k.Chat.Send, k.Chat.NewLine, k.Chat.ClearInput, k.Chat.ClearHistory},
		{k.Chat.ScrollUp, k.Chat.ScrollDown, k.Chat.ScrollTop, k.Chat.ScrollBottom},
		{k.Chat.EditMessage, k.Chat.Compact, k.Chat.Save, k.Chat.CopyMessage, k.Chat.ToggleToolDetails},
		// Workflow
		{k.Workflow.Start, k.Workflow.Pause, k.Workflow.Cancel, k.Workflow.StepDetail},
		{k.Workflow.SelectNext, k.Workflow.SelectPrev},
		// Sessions
		{k.Session.Open, k.Session.Delete, k.Session.Search, k.Session.Export, k.Session.NewSession},
		{k.Session.SelectNext, k.Session.SelectPrev},
		// Models
		{k.Models.SelectNext, k.Models.SelectPrev, k.Models.Search, k.Models.CheckModels},
		{k.Models.AddKey, k.Models.RemoveKey, k.Models.UseModel},
	}
}
