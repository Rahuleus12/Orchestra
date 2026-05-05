package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ViewType identifies the type of content in a chat message.
type ViewType int

const (
	// ViewUser represents a user message.
	ViewUser ViewType = iota

	// ViewAssistant represents an assistant message.
	ViewAssistant

	// ViewSystem represents a system message.
	ViewSystem

	// ViewTool represents a tool call/result.
	ViewTool

	// ViewError represents an error message.
	ViewError
)

// ChatMessage represents a rendered message in the chat view.
type ChatMessage struct {
	// Type is the message type.
	Type ViewType

	// Content is the message content.
	Content string

	// Timestamp is when the message was created.
	Timestamp time.Time

	// TokenUsage contains token usage info (for assistant messages).
	TokenUsage *TokenUsageInfo

	// ToolName is the name of the tool called (for tool messages).
	ToolName string

	// ToolDuration is how long the tool call took.
	ToolDuration time.Duration

	// Expanded indicates if tool details are shown.
	Expanded bool
}

// ChatModel is the Bubble Tea model for the chat view.
type ChatModel struct {
	// Theme holds the styling configuration.
	Theme *Theme

	// KeyMap holds the keybindings.
	KeyMap *KeyMap

	// Messages is the chat history.
	Messages []ChatMessage

	// Input is the text input component.
	Input textarea.Model

	// Spinner is the loading spinner.
	Spinner spinner.Model

	// IsGenerating indicates if the agent is currently generating.
	IsGenerating bool

	// IsStreaming indicates if we're receiving streamed content.
	IsStreaming bool

	// StreamingContent accumulates streamed content.
	StreamingContent string

	// Width is the terminal width.
	Width int

	// Height is the terminal height.
	Height int

	// ShowToolDetails indicates if tool call details are expanded by default.
	ShowToolDetails bool

	// OnSubmit is called when the user submits a message.
	OnSubmit func(content string) tea.Cmd

	// OnCommand is called when the user enters a slash command.
	OnCommand func(cmd Command) tea.Cmd

	// OnCompact is called when the user triggers compaction.
	OnCompact func() tea.Cmd

	// OnSave is called when the user wants to save the conversation.
	OnSave func() tea.Cmd

	// ScrollOffset is the scroll position in the message list.
	ScrollOffset int

	// Ready indicates if the model is fully initialized.
	Ready bool

	// MarkdownRenderer renders markdown content.
	markdownRenderer *MarkdownRenderer
}

// NewChatModel creates a new ChatModel.
func NewChatModel(theme *Theme, keyMap *KeyMap) *ChatModel {
	ta := textarea.New()
	ta.Placeholder = "Type a message... (/help for commands)"
	ta.Focus()
	ta.CharLimit = 10000
	ta.SetWidth(80)
	ta.SetHeight(3)

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = theme.Styles.Spinner

	return &ChatModel{
		Theme:            theme,
		KeyMap:           keyMap,
		Input:            ta,
		Spinner:          s,
		Messages:         []ChatMessage{},
		ShowToolDetails:  false,
		Width:            80,
		Height:           24,
		ScrollOffset:     0,
	}
}

// Init initializes the chat model.
func (m *ChatModel) Init() tea.Cmd {
	return tea.Batch(
		m.Spinner.Tick,
		textarea.Blink,
		m.initMarkdownRenderer(),
	)
}

func (m *ChatModel) initMarkdownRenderer() tea.Cmd {
	return func() tea.Msg {
		// Calculate available width for content (account for padding and borders)
		contentWidth := m.Width - 6
		if contentWidth < 40 {
			contentWidth = 40
		}
		renderer, err := NewMarkdownRenderer(contentWidth)
		if err != nil {
			return fmt.Errorf("failed to create markdown renderer: %w", err)
		}
		return markdownRendererReadyMsg{renderer}
	}
}

type markdownRendererReadyMsg struct {
	renderer *MarkdownRenderer
}

// AddMessage adds a message to the chat history.
func (m *ChatModel) AddMessage(msgType ViewType, content string) {
	m.Messages = append(m.Messages, ChatMessage{
		Type:      msgType,
		Content:   content,
		Timestamp: time.Now(),
	})
	m.ScrollToBottom()
}

// AddMessageWithUsage adds a message with token usage information.
func (m *ChatModel) AddMessageWithUsage(msgType ViewType, content string, usage *TokenUsageInfo) {
	m.Messages = append(m.Messages, ChatMessage{
		Type:       msgType,
		Content:    content,
		Timestamp:  time.Now(),
		TokenUsage: usage,
	})
	m.ScrollToBottom()
}

// AddToolMessage adds a tool call message.
func (m *ChatModel) AddToolMessage(name, input, output string, duration time.Duration, isError bool) {
	var content string
	if isError {
		content = fmt.Sprintf("Error: %s", output)
	} else {
		content = output
	}

	m.Messages = append(m.Messages, ChatMessage{
		Type:         ViewTool,
		Content:      content,
		Timestamp:    time.Now(),
		ToolName:     name,
		ToolDuration: duration,
		Expanded:     m.ShowToolDetails,
	})
	m.ScrollToBottom()
}

// StartStreaming prepares the model to receive streaming content.
func (m *ChatModel) StartStreaming() {
	m.IsStreaming = true
	m.StreamingContent = ""
}

// AppendStreamChunk appends a chunk of streamed content.
func (m *ChatModel) AppendStreamChunk(chunk string) {
	m.StreamingContent += chunk
}

// EndStreaming finalizes the streaming content and adds it as a message.
func (m *ChatModel) EndStreaming() {
	m.IsStreaming = false
	if m.StreamingContent != "" {
		m.AddMessage(ViewAssistant, m.StreamingContent)
	}
	m.StreamingContent = ""
}

// ClearHistory removes all messages from the chat history.
func (m *ChatModel) ClearHistory() {
	m.Messages = []ChatMessage{}
	m.ScrollOffset = 0
}

// SetGenerating sets the generating state.
func (m *ChatModel) SetGenerating(generating bool) {
	m.IsGenerating = generating
}

// SetSize updates the model dimensions.
func (m *ChatModel) SetSize(width, height int) {
	m.Width = width
	m.Height = height
	m.Input.SetWidth(width - 4)
	m.Input.SetHeight(3)

	if m.markdownRenderer != nil {
		contentWidth := width - 6
		if contentWidth < 40 {
			contentWidth = 40
		}
		_ = m.markdownRenderer.SetWidth(contentWidth)
	}
}

// ScrollToBottom scrolls to the most recent messages.
func (m *ChatModel) ScrollToBottom() {
	m.ScrollOffset = 0
}

// ScrollUp moves the scroll position up.
func (m *ChatModel) ScrollUp() {
	m.ScrollOffset++
}

// ScrollDown moves the scroll position down.
func (m *ChatModel) ScrollDown() {
	if m.ScrollOffset > 0 {
		m.ScrollOffset--
	}
}

// Update handles messages.
func (m *ChatModel) Update(msg tea.Msg) (*ChatModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		m.Ready = true
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.Spinner, cmd = m.Spinner.Update(msg)
		return m, cmd

	case markdownRendererReadyMsg:
		m.markdownRenderer = msg.renderer
		return m, nil

	case tea.KeyMsg:
		// Handle textarea keys
		if m.Input.Focused() {
			switch {
			case key.Matches(msg, m.KeyMap.Chat.Send):
				content := strings.TrimSpace(m.Input.Value())
				if content == "" {
					return m, nil
				}

				// Check for slash commands
				if IsCommand(content) {
					m.Input.SetValue("")
					if m.OnCommand != nil {
						reg := NewCommandRegistry()
						cmd := reg.Parse(content)
						if cmd != nil && m.OnCommand != nil {
							return m, m.OnCommand(*cmd)
						}
					}
					return m, nil
				}

				// Add user message
				m.AddMessage(ViewUser, content)
				m.Input.SetValue("")

				if m.OnSubmit != nil {
					cmds = append(cmds, m.OnSubmit(content))
				}
				return m, tea.Batch(cmds...)

			case key.Matches(msg, m.KeyMap.Chat.NewLine):
				m.Input.InsertString("\n")
				return m, nil

			case key.Matches(msg, m.KeyMap.Chat.ClearInput):
				m.Input.SetValue("")
				return m, nil

			case key.Matches(msg, m.KeyMap.Chat.ClearHistory):
				m.ClearHistory()
				return m, nil

			case key.Matches(msg, m.KeyMap.Chat.Compact):
				if m.OnCompact != nil {
					return m, m.OnCompact()
				}
				return m, nil

			case key.Matches(msg, m.KeyMap.Chat.Save):
				if m.OnSave != nil {
					return m, m.OnSave()
				}
				return m, nil

			case key.Matches(msg, m.KeyMap.Chat.ScrollUp):
				m.ScrollUp()
				return m, nil

			case key.Matches(msg, m.KeyMap.Chat.ScrollDown):
				m.ScrollDown()
				return m, nil

			case key.Matches(msg, m.KeyMap.Chat.ToggleToolDetails):
				m.ShowToolDetails = !m.ShowToolDetails
				return m, nil
			}
		}

	default:
		// Update sub-components
		var cmd tea.Cmd
		m.Input, cmd = m.Input.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// View renders the chat view.
func (m *ChatModel) View() string {
	if !m.Ready {
		return "Loading..."
	}

	var b strings.Builder

	// Render header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Render messages
	b.WriteString(m.renderMessages())

	// Render streaming content if active
	if m.IsStreaming && m.StreamingContent != "" {
		b.WriteString(m.renderAssistantMessage(m.StreamingContent))
	}

	// Render generating indicator
	if m.IsGenerating && !m.IsStreaming {
		b.WriteString(m.renderGenerating())
	}

	// Render input area
	b.WriteString("\n")
	b.WriteString(m.renderInput())

	return b.String()
}

func (m *ChatModel) renderHeader() string {
	return m.Theme.Styles.Title.Render("Chat")
}

func (m *ChatModel) renderMessages() string {
	if len(m.Messages) == 0 {
		return m.Theme.Styles.Muted.Render("\n  No messages yet. Type a message to begin.\n")
	}

	// Calculate visible area
	visibleHeight := m.Height - 10 // Account for header, input, status
	messages := m.getVisibleMessages(visibleHeight)

	var b strings.Builder
	for _, msg := range messages {
		b.WriteString(m.renderMessage(msg))
	}
	return b.String()
}

func (m *ChatModel) getVisibleMessages(maxHeight int) []ChatMessage {
	if m.ScrollOffset == 0 {
		// Show the most recent messages that fit
		messages := m.Messages
		var result []ChatMessage
		height := 0
		for i := len(messages) - 1; i >= 0; i-- {
			msgHeight := m.messageHeight(messages[i])
			if height+msgHeight > maxHeight && len(result) > 0 {
				break
			}
			result = append([]ChatMessage{messages[i]}, result...)
			height += msgHeight
		}
		return result
	}

	// Show messages starting from scroll offset
	if m.ScrollOffset >= len(m.Messages) {
		return m.Messages
	}
	return m.Messages[m.ScrollOffset:]
}

func (m *ChatModel) messageHeight(msg ChatMessage) int {
	// Rough estimation of message height
	lines := len(msg.Content)/m.Width + 1
	if lines < 2 {
		lines = 2
	}
	if msg.Type == ViewTool {
		lines += 2
	}
	return lines + 1 // Add padding
}

func (m *ChatModel) renderMessage(msg ChatMessage) string {
	switch msg.Type {
	case ViewUser:
		return m.renderUserMessage(msg.Content)
	case ViewAssistant:
		return m.renderAssistantMessage(msg.Content)
	case ViewSystem:
		return m.renderSystemMessage(msg.Content)
	case ViewTool:
		return m.renderToolMessage(msg)
	case ViewError:
		return m.renderErrorMessage(msg.Content)
	default:
		return msg.Content
	}
}

func (m *ChatModel) renderUserMessage(content string) string {
	label := m.Theme.Styles.UserMsg.Render("You:")
	var renderedContent string
	if m.markdownRenderer != nil {
		renderedContent, _ = m.markdownRenderer.Render(content)
	} else {
		renderedContent = content
	}
	return lipgloss.JoinVertical(lipgloss.Left, label, renderedContent)
}

func (m *ChatModel) renderAssistantMessage(content string) string {
	label := m.Theme.Styles.AssistantMsg.Render("Assistant:")
	var renderedContent string
	if m.markdownRenderer != nil {
		renderedContent, _ = m.markdownRenderer.Render(content)
	} else {
		renderedContent = content
	}
	return lipgloss.JoinVertical(lipgloss.Left, label, renderedContent)
}

func (m *ChatModel) renderSystemMessage(content string) string {
	label := m.Theme.Styles.SystemMsg.Render("System:")
	return lipgloss.JoinVertical(lipgloss.Left, label, content)
}

func (m *ChatModel) renderToolMessage(msg ChatMessage) string {
	indicator := m.Theme.Styles.ToolCallMsg.Render("⚡")
	title := fmt.Sprintf("%s Tool: %s (%.1fs)", indicator, msg.ToolName, msg.ToolDuration.Seconds())
	var details string
	if msg.Expanded {
		details = m.Theme.Styles.Dim.Render(msg.Content)
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, details)
}

func (m *ChatModel) renderErrorMessage(content string) string {
	return m.Theme.Styles.Error.Render(fmt.Sprintf("Error: %s", content))
}

func (m *ChatModel) renderGenerating() string {
	s := m.Spinner.View()
	return m.Theme.Styles.Muted.Render(s + " Generating...")
}

func (m *ChatModel) renderInput() string {
	return m.Input.View()
}
