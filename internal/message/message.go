package message

import (
	"crypto/rand"
	"fmt"
	"time"
)

// ContentBlock represents a single piece of content within a message.
// A message may contain multiple content blocks of different types
// (text, image, file) to support multi-modal interactions.
type ContentBlock struct {
	// Type identifies the kind of content. Valid values are "text", "image", "file".
	Type string `json:"type" yaml:"type"`

	// Text contains the textual content when Type is "text".
	Text string `json:"text,omitempty" yaml:"text,omitempty"`

	// ImageURL contains a URL to an image when Type is "image".
	ImageURL string `json:"image_url,omitempty" yaml:"image_url,omitempty"`

	// FileData contains raw file bytes when Type is "file".
	FileData []byte `json:"file_data,omitempty" yaml:"file_data,omitempty"`

	// MimeType specifies the MIME type of the content (e.g., "image/png", "application/pdf").
	MimeType string `json:"mime_type,omitempty" yaml:"mime_type,omitempty"`
}

// TextContentBlock creates a new ContentBlock of type "text" with the given string.
func TextContentBlock(text string) ContentBlock {
	return ContentBlock{
		Type: "text",
		Text: text,
	}
}

// ImageContentBlock creates a new ContentBlock of type "image" with the given URL.
func ImageContentBlock(imageURL, mimeType string) ContentBlock {
	return ContentBlock{
		Type:     "image",
		ImageURL: imageURL,
		MimeType: mimeType,
	}
}

// FileContentBlock creates a new ContentBlock of type "file" with the given data.
func FileContentBlock(data []byte, mimeType string) ContentBlock {
	return ContentBlock{
		Type:     "file",
		FileData: data,
		MimeType: mimeType,
	}
}

// ToolCallFunction represents the function details within a tool call.
type ToolCallFunction struct {
	// Name is the identifier of the function to invoke.
	Name string `json:"name" yaml:"name"`

	// Arguments is the JSON-encoded arguments for the function call.
	Arguments string `json:"arguments" yaml:"arguments"`
}

// ToolCall represents a single tool/function call requested by the model.
type ToolCall struct {
	// ID is a unique identifier for this tool call.
	ID string `json:"id" yaml:"id"`

	// Type is the type of tool call, typically "function".
	Type string `json:"type" yaml:"type"`

	// Function contains the function name and arguments.
	Function ToolCallFunction `json:"function" yaml:"function"`
}

// ToolResult holds the result of executing a tool call.
type ToolResult struct {
	// ToolCallID links this result back to the originating ToolCall.ID.
	ToolCallID string `json:"tool_call_id" yaml:"tool_call_id"`

	// Content is the output of the tool execution.
	Content string `json:"content" yaml:"content"`

	// IsError indicates whether the tool execution resulted in an error.
	IsError bool `json:"is_error,omitempty" yaml:"is_error,omitempty"`
}

// Message represents a single message within a conversation.
// Messages are the fundamental unit of communication with LLM providers.
type Message struct {
	// Role identifies who produced this message.
	Role Role `json:"role" yaml:"role"`

	// Content holds the content blocks of the message.
	// For simple text messages, this will contain a single text ContentBlock.
	Content []ContentBlock `json:"content" yaml:"content"`

	// Name is an optional participant name (used for some providers).
	Name string `json:"name,omitempty" yaml:"name,omitempty"`

	// ToolCalls contains tool/function calls requested by the assistant.
	// Only populated when Role is RoleAssistant.
	ToolCalls []ToolCall `json:"tool_calls,omitempty" yaml:"tool_calls,omitempty"`

	// ToolResult holds the result of a tool execution.
	// Only populated when Role is RoleTool.
	ToolResult *ToolResult `json:"tool_result,omitempty" yaml:"tool_result,omitempty"`

	// Metadata contains arbitrary extra data associated with this message.
	Metadata map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// TextMessage creates a new Message with a single text content block.
func TextMessage(role Role, text string) Message {
	return Message{
		Role:    role,
		Content: []ContentBlock{TextContentBlock(text)},
	}
}

// SystemMessage creates a new system message with the given text.
func SystemMessage(text string) Message {
	return TextMessage(RoleSystem, text)
}

// UserMessage creates a new user message with the given text.
func UserMessage(text string) Message {
	return TextMessage(RoleUser, text)
}

// AssistantMessage creates a new assistant message with the given text.
func AssistantMessage(text string) Message {
	return TextMessage(RoleAssistant, text)
}

// ToolResultMessage creates a new tool result message.
func ToolResultMessage(toolCallID, content string, isError bool) Message {
	return Message{
		Role: RoleTool,
		Content: []ContentBlock{
			TextContentBlock(content),
		},
		ToolResult: &ToolResult{
			ToolCallID: toolCallID,
			Content:    content,
			IsError:    isError,
		},
	}
}

// AssistantToolCallMessage creates a new assistant message containing tool calls.
func AssistantToolCallMessage(toolCalls []ToolCall) Message {
	return Message{
		Role:      RoleAssistant,
		ToolCalls: toolCalls,
	}
}

// Text returns the concatenated text content of all text ContentBlocks.
// Returns empty string if there are no text blocks.
func (m Message) Text() string {
	var result string
	for _, block := range m.Content {
		if block.Type == "text" {
			result += block.Text
		}
	}
	return result
}

// IsToolCall returns true if this message contains tool call requests.
func (m Message) IsToolCall() bool {
	return len(m.ToolCalls) > 0
}

// IsToolResult returns true if this message contains a tool result.
func (m Message) IsToolResult() bool {
	return m.ToolResult != nil
}

// Clone returns a deep copy of the message.
func (m Message) Clone() Message {
	cp := m
	if len(m.Content) > 0 {
		cp.Content = make([]ContentBlock, len(m.Content))
		copy(cp.Content, m.Content)
		for i, block := range cp.Content {
			if len(block.FileData) > 0 {
				cp.Content[i].FileData = make([]byte, len(block.FileData))
				copy(cp.Content[i].FileData, block.FileData)
			}
		}
	}
	if len(m.ToolCalls) > 0 {
		cp.ToolCalls = make([]ToolCall, len(m.ToolCalls))
		copy(cp.ToolCalls, m.ToolCalls)
	}
	if m.ToolResult != nil {
		cp.ToolResult = &ToolResult{}
		*cp.ToolResult = *m.ToolResult
	}
	if len(m.Metadata) > 0 {
		cp.Metadata = make(map[string]any, len(m.Metadata))
		for k, v := range m.Metadata {
			cp.Metadata[k] = v
		}
	}
	return cp
}

// Conversation represents an ordered sequence of messages exchanged
// within a single interaction session with an LLM provider.
type Conversation struct {
	// ID is a unique identifier for this conversation.
	ID string `json:"id" yaml:"id"`

	// Messages is the ordered list of messages in the conversation.
	Messages []Message `json:"messages" yaml:"messages"`

	// Metadata contains arbitrary data associated with this conversation.
	Metadata map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// NewConversation creates a new Conversation with a randomly generated ID.
func NewConversation() *Conversation {
	return &Conversation{
		ID:       generateID(),
		Messages: []Message{},
		Metadata: make(map[string]any),
	}
}

// NewConversationWithID creates a new Conversation with the specified ID.
func NewConversationWithID(id string) *Conversation {
	return &Conversation{
		ID:       id,
		Messages: []Message{},
		Metadata: make(map[string]any),
	}
}

// Add appends one or more messages to the conversation.
func (c *Conversation) Add(msgs ...Message) {
	c.Messages = append(c.Messages, msgs...)
}

// Last returns the most recently added message, or an error if the conversation is empty.
func (c *Conversation) Last() (Message, error) {
	if len(c.Messages) == 0 {
		return Message{}, fmt.Errorf("conversation is empty")
	}
	return c.Messages[len(c.Messages)-1], nil
}

// Len returns the number of messages in the conversation.
func (c *Conversation) Len() int {
	return len(c.Messages)
}

// IsEmpty returns true if the conversation contains no messages.
func (c *Conversation) IsEmpty() bool {
	return len(c.Messages) == 0
}

// Filter returns a new Conversation containing only messages that match the predicate.
// The conversation ID is preserved.
func (c *Conversation) Filter(predicate func(Message) bool) *Conversation {
	filtered := &Conversation{
		ID:       c.ID,
		Metadata: c.Metadata,
	}
	for _, msg := range c.Messages {
		if predicate(msg) {
			filtered.Messages = append(filtered.Messages, msg)
		}
	}
	return filtered
}

// FilterByRole returns a new Conversation containing only messages with the given role.
func (c *Conversation) FilterByRole(role Role) *Conversation {
	return c.Filter(func(msg Message) bool {
		return msg.Role == role
	})
}

// Truncate returns a new Conversation with at most maxMessages, keeping the
// most recent messages. The conversation ID is preserved.
// If maxMessages is 0 or negative, an empty conversation is returned.
func (c *Conversation) Truncate(maxMessages int) *Conversation {
	if maxMessages <= 0 {
		return &Conversation{
			ID:       c.ID,
			Metadata: c.Metadata,
		}
	}
	if len(c.Messages) <= maxMessages {
		return c.Clone()
	}

	truncated := &Conversation{
		ID:       c.ID,
		Metadata: c.Metadata,
		Messages: make([]Message, maxMessages),
	}

	start := len(c.Messages) - maxMessages
	copy(truncated.Messages, c.Messages[start:])
	return truncated
}

// TruncatePreservingSystem returns a new Conversation with at most maxMessages.
// System messages are always preserved at the beginning of the conversation.
// If maxMessages is 0, an empty conversation is returned.
func (c *Conversation) TruncatePreservingSystem(maxMessages int) *Conversation {
	if maxMessages <= 0 {
		return &Conversation{
			ID:       c.ID,
			Metadata: c.Metadata,
		}
	}
	if len(c.Messages) <= maxMessages {
		return c.Clone()
	}

	// Collect all system messages
	var systemMsgs []Message
	var otherMsgs []Message
	for _, msg := range c.Messages {
		if msg.Role == RoleSystem {
			systemMsgs = append(systemMsgs, msg)
		} else {
			otherMsgs = append(otherMsgs, msg)
		}
	}

	// If we can't fit even the system messages, truncate system messages too
	if len(systemMsgs) >= maxMessages {
		result := &Conversation{
			ID:       c.ID,
			Metadata: c.Metadata,
		}
		// Keep the last maxMessages system messages
		start := len(systemMsgs) - maxMessages
		result.Messages = make([]Message, maxMessages)
		copy(result.Messages, systemMsgs[start:])
		return result
	}

	// Calculate how many non-system messages we can keep
	remaining := maxMessages - len(systemMsgs)
	if remaining > len(otherMsgs) {
		remaining = len(otherMsgs)
	}

	// Keep all system messages + the most recent non-system messages
	result := &Conversation{
		ID:       c.ID,
		Metadata: c.Metadata,
	}
	result.Messages = append(result.Messages, systemMsgs...)
	if remaining > 0 {
		result.Messages = append(result.Messages, otherMsgs[len(otherMsgs)-remaining:]...)
	}
	return result
}

// Clone returns a deep copy of the conversation.
func (c *Conversation) Clone() *Conversation {
	cp := &Conversation{
		ID:       c.ID,
		Messages: make([]Message, len(c.Messages)),
	}
	for i, msg := range c.Messages {
		cp.Messages[i] = msg.Clone()
	}
	if len(c.Metadata) > 0 {
		cp.Metadata = make(map[string]any, len(c.Metadata))
		for k, v := range c.Metadata {
			cp.Metadata[k] = v
		}
	} else {
		cp.Metadata = make(map[string]any)
	}
	return cp
}

// Format returns a human-readable string representation of the conversation,
// with each message prefixed by its role.
func (c *Conversation) Format() string {
	var result string
	for _, msg := range c.Messages {
		result += fmt.Sprintf("[%s] %s\n", msg.Role, msg.Text())
	}
	return result
}

// SetMetadata sets a key-value pair in the conversation metadata.
func (c *Conversation) SetMetadata(key string, value any) {
	if c.Metadata == nil {
		c.Metadata = make(map[string]any)
	}
	c.Metadata[key] = value
}

// GetMetadata retrieves a value from the conversation metadata.
func (c *Conversation) GetMetadata(key string) (any, bool) {
	val, ok := c.Metadata[key]
	return val, ok
}

// generateID creates a new random identifier for conversations and results.
func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x-%d", b[0:2], b[2:4], b[4:6], b[6:8], b[8:10], time.Now().UnixMilli()%10000)
}
