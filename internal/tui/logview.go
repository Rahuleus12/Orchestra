package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// LogLevel represents the severity of a log entry.
type LogLevel int

const (
	// LogLevelDebug is for debug messages.
	LogLevelDebug LogLevel = iota

	// LogLevelInfo is for informational messages.
	LogLevelInfo

	// LogLevelWarn is for warning messages.
	LogLevelWarn

	// LogLevelError is for error messages.
	LogLevelError
)

// LogEntry represents a single log entry.
type LogEntry struct {
	// Timestamp is when the log entry was created.
	Timestamp time.Time

	// Level is the severity level.
	Level LogLevel

	// Message is the log message.
	Message string

	// Source is the source of the log (e.g., "agent", "provider").
	Source string

	// Metadata contains additional structured data.
	Metadata map[string]string
}

// LogModel is the Bubble Tea model for the log/trace view.
type LogModel struct {
	// Theme holds the styling configuration.
	Theme *Theme

	// KeyMap holds the keybindings.
	KeyMap *KeyMap

	// Entries is the list of log entries.
	Entries []LogEntry

	// Filter is the current log level filter.
	Filter LogLevel

	// SearchFilter filters entries by source.
	SearchFilter string

	// ScrollOffset is the scroll position.
	ScrollOffset int

	// AutoScroll indicates if the view auto-scrolls to new entries.
	AutoScroll bool

	// Width is the terminal width.
	Width int

	// Height is the terminal height.
	Height int

	// Ready indicates if the model is fully initialized.
	Ready bool
}

// NewLogModel creates a new LogModel.
func NewLogModel(theme *Theme, keyMap *KeyMap) *LogModel {
	return &LogModel{
		Theme:      theme,
		KeyMap:     keyMap,
		Entries:    []LogEntry{},
		Filter:     LogLevelDebug,
		AutoScroll: true,
		Width:      80,
		Height:     24,
	}
}

// Init initializes the log model.
func (m *LogModel) Init() tea.Cmd {
	return nil
}

// AddEntry adds a new log entry.
func (m *LogModel) AddEntry(level LogLevel, source, message string) {
	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
		Source:    source,
	}
	m.Entries = append(m.Entries, entry)

	// Limit the number of entries
	if len(m.Entries) > 10000 {
		m.Entries = m.Entries[len(m.Entries)-5000:]
	}
}

// AddEntryWithMetadata adds a new log entry with metadata.
func (m *LogModel) AddEntryWithMetadata(level LogLevel, source, message string, metadata map[string]string) {
	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
		Source:    source,
		Metadata:  metadata,
	}
	m.Entries = append(m.Entries, entry)
}

// Clear removes all log entries.
func (m *LogModel) Clear() {
	m.Entries = []LogEntry{}
	m.ScrollOffset = 0
}

// SetFilter sets the minimum log level to display.
func (m *LogModel) SetFilter(level LogLevel) {
	m.Filter = level
}

// SetSearchFilter filters entries by source.
func (m *LogModel) SetSearchFilter(filter string) {
	m.SearchFilter = filter
}

// SetSize updates the model dimensions.
func (m *LogModel) SetSize(width, height int) {
	m.Width = width
	m.Height = height
}

// Update handles messages.
func (m *LogModel) Update(msg tea.Msg) (*LogModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		m.Ready = true
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.KeyMap.Chat.ScrollUp):
			m.ScrollOffset++
			m.AutoScroll = false
		case key.Matches(msg, m.KeyMap.Chat.ScrollDown):
			if m.ScrollOffset > 0 {
				m.ScrollOffset--
			}
			if m.ScrollOffset == 0 {
				m.AutoScroll = true
			}
		case key.Matches(msg, m.KeyMap.Chat.ScrollTop):
			m.ScrollOffset = len(m.filteredEntries())
			m.AutoScroll = false
		case key.Matches(msg, m.KeyMap.Chat.ScrollBottom):
			m.ScrollOffset = 0
			m.AutoScroll = true
		}
	}

	return m, nil
}

// View renders the log view.
func (m *LogModel) View() string {
	if !m.Ready {
		return "Loading..."
	}

	var b strings.Builder

	// Render header
	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")

	// Render log entries
	b.WriteString(m.renderEntries())

	// Render footer
	b.WriteString("\n")
	b.WriteString(m.renderFooter())

	return b.String()
}

func (m *LogModel) renderHeader() string {
	title := m.Theme.Styles.Title.Render("Logs")
	count := m.Theme.Styles.Muted.Render(fmt.Sprintf("(%d entries)", len(m.filteredEntries())))
	return lipgloss.JoinHorizontal(lipgloss.Top, title, " ", count)
}

func (m *LogModel) renderEntries() string {
	entries := m.filteredEntries()
	if len(entries) == 0 {
		return m.Theme.Styles.Muted.Render("  No log entries.\n")
	}

	// Calculate visible area
	visibleHeight := m.Height - 8
	if visibleHeight < 5 {
		visibleHeight = 5
	}

	// Get visible entries
	visibleEntries := m.getVisibleEntries(entries, visibleHeight)

	var b strings.Builder
	for _, entry := range visibleEntries {
		b.WriteString(m.renderEntry(entry))
		b.WriteString("\n")
	}

	return b.String()
}

func (m *LogModel) filteredEntries() []LogEntry {
	var result []LogEntry
	for _, entry := range m.Entries {
		if entry.Level < m.Filter {
			continue
		}
		if m.SearchFilter != "" && !strings.Contains(strings.ToLower(entry.Source), strings.ToLower(m.SearchFilter)) {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func (m *LogModel) getVisibleEntries(entries []LogEntry, maxHeight int) []LogEntry {
	if m.AutoScroll {
		// Show the most recent entries
		if len(entries) <= maxHeight {
			return entries
		}
		return entries[len(entries)-maxHeight:]
	}

	// Show from scroll offset
	if m.ScrollOffset >= len(entries) {
		return entries
	}
	start := m.ScrollOffset
	end := start + maxHeight
	if end > len(entries) {
		end = len(entries)
	}
	return entries[start:end]
}

func (m *LogModel) renderEntry(entry LogEntry) string {
	// Timestamp
	timeStr := m.Theme.Styles.Dim.Render(entry.Timestamp.Format("15:04:05.000"))

	// Level
	levelStr := m.renderLevel(entry.Level)

	// Source
	sourceStr := m.Theme.Styles.Muted.Render(fmt.Sprintf("[%s]", entry.Source))

	// Message
	var messageStyle lipgloss.Style
	switch entry.Level {
	case LogLevelError:
		messageStyle = m.Theme.Styles.Error
	case LogLevelWarn:
		messageStyle = m.Theme.Styles.Warning
	default:
		messageStyle = m.Theme.Styles.ListNormal
	}

	message := entry.Message
	if len(message) > m.Width-40 {
		message = message[:m.Width-43] + "..."
	}
	messageStr := messageStyle.Render(message)

	return fmt.Sprintf("  %s %s %s %s", timeStr, levelStr, sourceStr, messageStr)
}

func (m *LogModel) renderLevel(level LogLevel) string {
	switch level {
	case LogLevelDebug:
		return m.Theme.Styles.Dim.Render("DEBUG")
	case LogLevelInfo:
		return m.Theme.Styles.Success.Render("INFO ")
	case LogLevelWarn:
		return m.Theme.Styles.Warning.Render("WARN ")
	case LogLevelError:
		return m.Theme.Styles.Error.Render("ERROR")
	default:
		return "?????"
	}
}

func (m *LogModel) renderFooter() string {
	keys := []struct {
		key string
		help string
	}{
		{"↑/↓", "scroll"},
		{"g/G", "top/bottom"},
	}

	var parts []string
	for _, k := range keys {
		keyStr := m.Theme.Styles.HelpKey.Render(k.key)
		helpStr := m.Theme.Styles.Help.Render(k.help)
		parts = append(parts, keyStr+helpStr)
	}

	// Add filter indicator
	filterStr := m.Theme.Styles.Muted.Render(fmt.Sprintf("  Filter: %s", m.logLevelName(m.Filter)))
	parts = append(parts, filterStr)

	return strings.Join(parts, "  ")
}

func (m *LogModel) logLevelName(level LogLevel) string {
	switch level {
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}
