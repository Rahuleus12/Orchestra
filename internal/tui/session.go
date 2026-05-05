package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SessionModel is the Bubble Tea model for the session browser view.
type SessionModel struct {
	// Theme holds the styling configuration.
	Theme *Theme

	// KeyMap holds the keybindings.
	KeyMap *KeyMap

	// Store is the session store.
	Store *SessionStore

	// Sessions is the displayed list of sessions.
	Sessions []*Session

	// SelectedIndex is the index of the currently selected session.
	SelectedIndex int

	// SearchInput is the search text input.
	SearchInput textinput.Model

	// IsSearching indicates if the user is in search mode.
	IsSearching bool

	// Width is the terminal width.
	Width int

	// Height is the terminal height.
	Height int

	// Ready indicates if the model is fully initialized.
	Ready bool

	// OnOpen is called when the user opens a session.
	OnOpen func(sessionID string) tea.Cmd

	// OnDelete is called when the user deletes a session.
	OnDelete func(sessionID string) tea.Cmd

	// OnNewSession is called when the user creates a new session.
	OnNewSession func() tea.Cmd

	// OnExport is called when the user exports a session.
	OnExport func(sessionID string, format string) tea.Cmd
}

// NewSessionModel creates a new SessionModel.
func NewSessionModel(theme *Theme, keyMap *KeyMap, store *SessionStore) *SessionModel {
	si := textinput.New()
	si.Placeholder = "Search sessions..."
	si.CharLimit = 100

	return &SessionModel{
		Theme:        theme,
		KeyMap:       keyMap,
		Store:        store,
		Sessions:     store.ListSessions(),
		SelectedIndex: -1,
		SearchInput:  si,
		IsSearching:  false,
		Width:        80,
		Height:       24,
	}
}

// Init initializes the session model.
func (m *SessionModel) Init() tea.Cmd {
	return textinput.Blink
}

// Refresh reloads the session list from the store.
func (m *SessionModel) Refresh() {
	m.Sessions = m.Store.ListSessions()
	if m.IsSearching {
		m.Sessions = m.Store.SearchSessions(m.SearchInput.Value())
	}
	if m.SelectedIndex >= len(m.Sessions) {
		m.SelectedIndex = len(m.Sessions) - 1
	}
}

// SetSize updates the model dimensions.
func (m *SessionModel) SetSize(width, height int) {
	m.Width = width
	m.Height = height
	m.SearchInput.Width = width - 4
}

// SelectNext selects the next session.
func (m *SessionModel) SelectNext() {
	if m.SelectedIndex < len(m.Sessions)-1 {
		m.SelectedIndex++
	}
}

// SelectPrev selects the previous session.
func (m *SessionModel) SelectPrev() {
	if m.SelectedIndex > 0 {
		m.SelectedIndex--
	}
}

// GetSelectedSession returns the currently selected session.
func (m *SessionModel) GetSelectedSession() *Session {
	if m.SelectedIndex >= 0 && m.SelectedIndex < len(m.Sessions) {
		return m.Sessions[m.SelectedIndex]
	}
	return nil
}

// Update handles messages.
func (m *SessionModel) Update(msg tea.Msg) (*SessionModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		m.Ready = true
		return m, nil

	case tea.KeyMsg:
		if m.IsSearching {
			return m.handleSearchKeys(msg)
		}

		switch {
		case key.Matches(msg, m.KeyMap.Session.Search):
			m.IsSearching = true
			m.SearchInput.Focus()
			return m, nil

		case key.Matches(msg, m.KeyMap.Session.Open):
			session := m.GetSelectedSession()
			if session != nil && m.OnOpen != nil {
				return m, m.OnOpen(session.ID)
			}

		case key.Matches(msg, m.KeyMap.Session.Delete):
			session := m.GetSelectedSession()
			if session != nil && m.OnDelete != nil {
				return m, m.OnDelete(session.ID)
			}

		case key.Matches(msg, m.KeyMap.Session.Export):
			session := m.GetSelectedSession()
			if session != nil && m.OnExport != nil {
				return m, m.OnExport(session.ID, "markdown")
			}

		case key.Matches(msg, m.KeyMap.Session.NewSession):
			if m.OnNewSession != nil {
				return m, m.OnNewSession()
			}

		case key.Matches(msg, m.KeyMap.Session.SelectNext):
			m.SelectNext()

		case key.Matches(msg, m.KeyMap.Session.SelectPrev):
			m.SelectPrev()
		}
	}

	// Update search input
	if m.IsSearching {
		var cmd tea.Cmd
		m.SearchInput, cmd = m.SearchInput.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *SessionModel) handleSearchKeys(msg tea.KeyMsg) (*SessionModel, tea.Cmd) {
	switch {
	case key.Matches(msg, m.KeyMap.Help.Close):
		m.IsSearching = false
		m.SearchInput.Blur()
		m.Refresh()
		return m, nil
	}

	// Let the text input handle other keys
	var cmd tea.Cmd
	m.SearchInput, cmd = m.SearchInput.Update(msg)

	// Filter sessions based on search
	m.Sessions = m.Store.SearchSessions(m.SearchInput.Value())

	return m, cmd
}

// View renders the session browser view.
func (m *SessionModel) View() string {
	if !m.Ready {
		return "Loading..."
	}

	var b strings.Builder

	// Render header
	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")

	// Render search bar
	b.WriteString(m.renderSearchBar())
	b.WriteString("\n")

	// Render session list
	b.WriteString(m.renderSessionList())

	// Render footer with key hints
	b.WriteString("\n")
	b.WriteString(m.renderFooter())

	return b.String()
}

func (m *SessionModel) renderHeader() string {
	title := m.Theme.Styles.Title.Render("Sessions")
	count := m.Theme.Styles.Muted.Render(fmt.Sprintf("(%d sessions)", len(m.Sessions)))
	return lipgloss.JoinHorizontal(lipgloss.Top, title, " ", count)
}

func (m *SessionModel) renderSearchBar() string {
	prompt := m.Theme.Styles.HelpKey.Render("/")
	if m.IsSearching {
		return lipgloss.JoinHorizontal(lipgloss.Top, prompt, m.SearchInput.View())
	}
	hint := m.Theme.Styles.Help.Render("Press / to search")
	return lipgloss.JoinHorizontal(lipgloss.Top, prompt, hint)
}

func (m *SessionModel) renderSessionList() string {
	if len(m.Sessions) == 0 {
		if m.IsSearching {
			return m.Theme.Styles.Muted.Render("  No sessions match your search.\n")
		}
		return m.Theme.Styles.Muted.Render("  No sessions yet. Press n to create one.\n")
	}

	// Calculate visible area
	visibleHeight := m.Height - 10
	if visibleHeight < 5 {
		visibleHeight = 5
	}

	// Calculate visible range
	start := 0
	if m.SelectedIndex >= visibleHeight {
		start = m.SelectedIndex - visibleHeight + 1
	}
	end := start + visibleHeight
	if end > len(m.Sessions) {
		end = len(m.Sessions)
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		session := m.Sessions[i]
		selected := i == m.SelectedIndex

		// Highlight active session
		activeMark := ""
		if _, ok := m.Store.GetActiveSession(); ok {
			if active, _ := m.Store.GetActiveSession(); active != nil && active.ID == session.ID {
				activeMark = "● "
			}
		}

		line := m.renderSessionItem(session, selected, activeMark)
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

func (m *SessionModel) renderSessionItem(session *Session, selected bool, activeMark string) string {
	var style lipgloss.Style
	if selected {
		style = m.Theme.Styles.ListSelected
	} else {
		style = m.Theme.Styles.ListNormal
	}

	// Format title
	title := session.Title
	if len(title) > 40 {
		title = title[:37] + "..."
	}

	// Format metadata
	meta := fmt.Sprintf("%s | %s", session.AgentName, session.Model)
	if len(meta) > 20 {
		meta = meta[:17] + "..."
	}

	// Format date
	date := session.UpdatedAt.Format("Jan 2, 15:04")

	// Message count
	msgCount := fmt.Sprintf("%d msgs", len(session.Messages))

	// Build the line
	parts := []string{
		activeMark,
		title,
		"  ",
		m.Theme.Styles.Dim.Render(meta),
		"  ",
		m.Theme.Styles.Muted.Render(msgCount),
		"  ",
		m.Theme.Styles.Dim.Render(date),
	}

	line := strings.Join(parts, "")
	return style.Render(line)
}

func (m *SessionModel) renderFooter() string {
	keys := []struct {
		key string
		help string
	}{
		{"n", "new"},
		{"enter", "open"},
		{"d", "delete"},
		{"e", "export"},
		{"/", "search"},
	}

	var parts []string
	for _, k := range keys {
		keyStr := m.Theme.Styles.HelpKey.Render(k.key)
		helpStr := m.Theme.Styles.Help.Render(k.help)
		parts = append(parts, keyStr+helpStr)
	}

	return strings.Join(parts, "  ")
}
