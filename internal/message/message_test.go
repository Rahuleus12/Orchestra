package message

import (
	"strings"
	"testing"
)

// --- Role Tests ---

func TestRole_IsValid(t *testing.T) {
	tests := []struct {
		name string
		role Role
		want bool
	}{
		{"system role", RoleSystem, true},
		{"user role", RoleUser, true},
		{"assistant role", RoleAssistant, true},
		{"tool role", RoleTool, true},
		{"function role", RoleFunction, true},
		{"empty role", Role(""), false},
		{"unknown role", Role("unknown"), false},
		{"capitalized system", Role("System"), false},
		{"capitalized user", Role("User"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.role.IsValid(); got != tt.want {
				t.Errorf("Role(%q).IsValid() = %v, want %v", tt.role, got, tt.want)
			}
		})
	}
}

func TestRole_String(t *testing.T) {
	tests := []struct {
		name string
		role Role
		want string
	}{
		{"system", RoleSystem, "system"},
		{"user", RoleUser, "user"},
		{"assistant", RoleAssistant, "assistant"},
		{"tool", RoleTool, "tool"},
		{"function", RoleFunction, "function"},
		{"custom", Role("custom"), "custom"},
		{"empty", Role(""), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.role.String(); got != tt.want {
				t.Errorf("Role(%q).String() = %q, want %q", tt.role, got, tt.want)
			}
		})
	}
}

// --- ContentBlock Tests ---

func TestTextContentBlock(t *testing.T) {
	text := "Hello, world!"
	block := TextContentBlock(text)

	if block.Type != "text" {
		t.Errorf("TextContentBlock().Type = %q, want %q", block.Type, "text")
	}
	if block.Text != text {
		t.Errorf("TextContentBlock().Text = %q, want %q", block.Text, text)
	}
	if block.ImageURL != "" {
		t.Errorf("TextContentBlock().ImageURL = %q, want empty", block.ImageURL)
	}
	if block.FileData != nil {
		t.Errorf("TextContentBlock().FileData = %v, want nil", block.FileData)
	}
	if block.MimeType != "" {
		t.Errorf("TextContentBlock().MimeType = %q, want empty", block.MimeType)
	}
}

func TestImageContentBlock(t *testing.T) {
	url := "https://example.com/image.png"
	mime := "image/png"
	block := ImageContentBlock(url, mime)

	if block.Type != "image" {
		t.Errorf("ImageContentBlock().Type = %q, want %q", block.Type, "image")
	}
	if block.ImageURL != url {
		t.Errorf("ImageContentBlock().ImageURL = %q, want %q", block.ImageURL, url)
	}
	if block.MimeType != mime {
		t.Errorf("ImageContentBlock().MimeType = %q, want %q", block.MimeType, mime)
	}
	if block.Text != "" {
		t.Errorf("ImageContentBlock().Text = %q, want empty", block.Text)
	}
}

func TestFileContentBlock(t *testing.T) {
	data := []byte{0x89, 0x50, 0x4E, 0x47}
	mime := "image/png"
	block := FileContentBlock(data, mime)

	if block.Type != "file" {
		t.Errorf("FileContentBlock().Type = %q, want %q", block.Type, "file")
	}
	if string(block.FileData) != string(data) {
		t.Errorf("FileContentBlock().FileData = %v, want %v", block.FileData, data)
	}
	if block.MimeType != mime {
		t.Errorf("FileContentBlock().MimeType = %q, want %q", block.MimeType, mime)
	}
	if block.Text != "" {
		t.Errorf("FileContentBlock().Text = %q, want empty", block.Text)
	}
}

// --- Message Constructor Tests ---

func TestTextMessage(t *testing.T) {
	msg := TextMessage(RoleUser, "hello")

	if msg.Role != RoleUser {
		t.Errorf("TextMessage role = %v, want %v", msg.Role, RoleUser)
	}
	if len(msg.Content) != 1 {
		t.Fatalf("TextMessage content blocks = %d, want 1", len(msg.Content))
	}
	if msg.Content[0].Type != "text" {
		t.Errorf("TextMessage content type = %q, want %q", msg.Content[0].Type, "text")
	}
	if msg.Content[0].Text != "hello" {
		t.Errorf("TextMessage content text = %q, want %q", msg.Content[0].Text, "hello")
	}
}

func TestSystemMessage(t *testing.T) {
	msg := SystemMessage("you are a helpful assistant")

	if msg.Role != RoleSystem {
		t.Errorf("SystemMessage role = %v, want %v", msg.Role, RoleSystem)
	}
	if msg.Text() != "you are a helpful assistant" {
		t.Errorf("SystemMessage text = %q, want %q", msg.Text(), "you are a helpful assistant")
	}
}

func TestUserMessage(t *testing.T) {
	msg := UserMessage("what is 2+2?")

	if msg.Role != RoleUser {
		t.Errorf("UserMessage role = %v, want %v", msg.Role, RoleUser)
	}
	if msg.Text() != "what is 2+2?" {
		t.Errorf("UserMessage text = %q, want %q", msg.Text(), "what is 2+2?")
	}
}

func TestAssistantMessage(t *testing.T) {
	msg := AssistantMessage("the answer is 4")

	if msg.Role != RoleAssistant {
		t.Errorf("AssistantMessage role = %v, want %v", msg.Role, RoleAssistant)
	}
	if msg.Text() != "the answer is 4" {
		t.Errorf("AssistantMessage text = %q, want %q", msg.Text(), "the answer is 4")
	}
}

func TestToolResultMessage(t *testing.T) {
	msg := ToolResultMessage("call_123", `{"result": 42}`, false)

	if msg.Role != RoleTool {
		t.Errorf("ToolResultMessage role = %v, want %v", msg.Role, RoleTool)
	}
	if msg.ToolResult == nil {
		t.Fatal("ToolResultMessage tool result is nil")
	}
	if msg.ToolResult.ToolCallID != "call_123" {
		t.Errorf("ToolResultMessage tool call ID = %q, want %q", msg.ToolResult.ToolCallID, "call_123")
	}
	if msg.ToolResult.Content != `{"result": 42}` {
		t.Errorf("ToolResultMessage content = %q, want %q", msg.ToolResult.Content, `{"result": 42}`)
	}
	if msg.ToolResult.IsError {
		t.Errorf("ToolResultMessage isError = true, want false")
	}
}

func TestToolResultMessage_WithError(t *testing.T) {
	msg := ToolResultMessage("call_456", "tool execution failed", true)

	if !msg.ToolResult.IsError {
		t.Errorf("ToolResultMessage isError = false, want true")
	}
}

func TestAssistantToolCallMessage(t *testing.T) {
	toolCalls := []ToolCall{
		{
			ID:   "call_001",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "get_weather",
				Arguments: `{"city": "London"}`,
			},
		},
		{
			ID:   "call_002",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "get_time",
				Arguments: `{"timezone": "UTC"}`,
			},
		},
	}

	msg := AssistantToolCallMessage(toolCalls)

	if msg.Role != RoleAssistant {
		t.Errorf("AssistantToolCallMessage role = %v, want %v", msg.Role, RoleAssistant)
	}
	if len(msg.ToolCalls) != 2 {
		t.Fatalf("AssistantToolCallMessage tool calls = %d, want 2", len(msg.ToolCalls))
	}
	if msg.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("ToolCalls[0] name = %q, want %q", msg.ToolCalls[0].Function.Name, "get_weather")
	}
	if msg.ToolCalls[1].Function.Name != "get_time" {
		t.Errorf("ToolCalls[1] name = %q, want %q", msg.ToolCalls[1].Function.Name, "get_time")
	}
}

// --- Message Method Tests ---

func TestMessage_Text(t *testing.T) {
	tests := []struct {
		name    string
		message Message
		want    string
	}{
		{
			name:    "single text block",
			message: UserMessage("hello world"),
			want:    "hello world",
		},
		{
			name: "multiple text blocks concatenated",
			message: Message{
				Role: RoleUser,
				Content: []ContentBlock{
					TextContentBlock("hello "),
					TextContentBlock("world"),
				},
			},
			want: "hello world",
		},
		{
			name: "image block skipped",
			message: Message{
				Role: RoleUser,
				Content: []ContentBlock{
					TextContentBlock("describe this: "),
					ImageContentBlock("https://example.com/img.png", "image/png"),
				},
			},
			want: "describe this: ",
		},
		{
			name: "no content blocks",
			message: Message{
				Role:    RoleAssistant,
				Content: []ContentBlock{},
			},
			want: "",
		},
		{
			name: "nil content blocks",
			message: Message{
				Role:    RoleAssistant,
				Content: nil,
			},
			want: "",
		},
		{
			name: "only image block",
			message: Message{
				Role: RoleUser,
				Content: []ContentBlock{
					ImageContentBlock("https://example.com/img.png", "image/png"),
				},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.message.Text(); got != tt.want {
				t.Errorf("Message.Text() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMessage_IsToolCall(t *testing.T) {
	tests := []struct {
		name    string
		message Message
		want    bool
	}{
		{
			name:    "text message without tool calls",
			message: AssistantMessage("hello"),
			want:    false,
		},
		{
			name: "message with tool calls",
			message: AssistantToolCallMessage([]ToolCall{
				{ID: "call_1", Type: "function", Function: ToolCallFunction{Name: "foo", Arguments: "{}"}},
			}),
			want: true,
		},
		{
			name:    "empty tool calls slice",
			message: Message{Role: RoleAssistant, ToolCalls: []ToolCall{}},
			want:    false,
		},
		{
			name:    "nil tool calls",
			message: Message{Role: RoleAssistant},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.message.IsToolCall(); got != tt.want {
				t.Errorf("Message.IsToolCall() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMessage_IsToolResult(t *testing.T) {
	tests := []struct {
		name    string
		message Message
		want    bool
	}{
		{
			name:    "regular message",
			message: UserMessage("hello"),
			want:    false,
		},
		{
			name:    "tool result message",
			message: ToolResultMessage("call_1", "result", false),
			want:    true,
		},
		{
			name:    "assistant message no tool result",
			message: AssistantMessage("response"),
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.message.IsToolResult(); got != tt.want {
				t.Errorf("Message.IsToolResult() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMessage_Clone(t *testing.T) {
	original := Message{
		Role: RoleUser,
		Content: []ContentBlock{
			TextContentBlock("hello"),
			FileContentBlock([]byte{1, 2, 3}, "application/octet-stream"),
		},
		Name: "test_user",
		ToolCalls: []ToolCall{
			{ID: "call_1", Type: "function", Function: ToolCallFunction{Name: "search", Arguments: `{"q": "test"}`}},
		},
		ToolResult: &ToolResult{
			ToolCallID: "call_1",
			Content:    "found",
			IsError:    false,
		},
		Metadata: map[string]any{
			"key": "value",
		},
	}

	cloned := original.Clone()

	// Verify equality of values
	if cloned.Role != original.Role {
		t.Errorf("Clone().Role = %v, want %v", cloned.Role, original.Role)
	}
	if cloned.Name != original.Name {
		t.Errorf("Clone().Name = %q, want %q", cloned.Name, original.Name)
	}
	if cloned.Text() != original.Text() {
		t.Errorf("Clone().Text() = %q, want %q", cloned.Text(), original.Text())
	}
	if len(cloned.Content) != len(original.Content) {
		t.Errorf("Clone() content length = %d, want %d", len(cloned.Content), len(original.Content))
	}
	if len(cloned.ToolCalls) != len(original.ToolCalls) {
		t.Errorf("Clone() tool calls length = %d, want %d", len(cloned.ToolCalls), len(original.ToolCalls))
	}
	if cloned.ToolResult == nil {
		t.Fatal("Clone().ToolResult is nil")
	}
	if cloned.ToolResult.Content != original.ToolResult.Content {
		t.Errorf("Clone().ToolResult.Content = %q, want %q", cloned.ToolResult.Content, original.ToolResult.Content)
	}

	// Verify deep copy — mutations to clone shouldn't affect original
	cloned.Content[0] = TextContentBlock("modified")
	if original.Content[0].Text != "hello" {
		t.Error("modifying clone content affected original")
	}

	cloned.Metadata["new_key"] = "new_value"
	if _, ok := original.Metadata["new_key"]; ok {
		t.Error("modifying clone metadata affected original")
	}

	cloned.ToolResult.Content = "changed"
	if original.ToolResult.Content != "found" {
		t.Error("modifying clone tool result affected original")
	}

	// Verify FileData deep copy
	cloned.Content[1].FileData[0] = 0xFF
	if original.Content[1].FileData[0] == 0xFF {
		t.Error("modifying clone file data affected original")
	}
}

func TestMessage_Clone_EmptyMessage(t *testing.T) {
	original := Message{Role: RoleUser}
	cloned := original.Clone()

	if cloned.Role != original.Role {
		t.Errorf("Clone().Role = %v, want %v", cloned.Role, original.Role)
	}
	if len(cloned.Content) != 0 {
		t.Errorf("Clone() content length = %d, want 0", len(cloned.Content))
	}
	if len(cloned.ToolCalls) != 0 {
		t.Errorf("Clone() tool calls length = %d, want 0", len(cloned.ToolCalls))
	}
	if cloned.ToolResult != nil {
		t.Error("Clone().ToolResult should be nil")
	}
}

// --- Conversation Tests ---

func TestNewConversation(t *testing.T) {
	conv := NewConversation()

	if conv.ID == "" {
		t.Error("NewConversation().ID is empty")
	}
	if len(conv.Messages) != 0 {
		t.Errorf("NewConversation() messages length = %d, want 0", len(conv.Messages))
	}
	if conv.Metadata == nil {
		t.Error("NewConversation() metadata is nil")
	}
}

func TestNewConversationWithID(t *testing.T) {
	id := "test-conv-123"
	conv := NewConversationWithID(id)

	if conv.ID != id {
		t.Errorf("NewConversationWithID().ID = %q, want %q", conv.ID, id)
	}
}

func TestConversation_Add(t *testing.T) {
	conv := NewConversation()

	conv.Add(UserMessage("hello"))
	if conv.Len() != 1 {
		t.Errorf("after Add(1), Len() = %d, want 1", conv.Len())
	}

	conv.Add(AssistantMessage("hi"), UserMessage("how are you?"))
	if conv.Len() != 3 {
		t.Errorf("after Add(2 more), Len() = %d, want 3", conv.Len())
	}
}

func TestConversation_Last(t *testing.T) {
	t.Run("non-empty conversation", func(t *testing.T) {
		conv := NewConversation()
		conv.Add(SystemMessage("sys"), UserMessage("hello"), AssistantMessage("hi"))

		last, err := conv.Last()
		if err != nil {
			t.Fatalf("Last() error = %v", err)
		}
		if last.Role != RoleAssistant {
			t.Errorf("Last().Role = %v, want %v", last.Role, RoleAssistant)
		}
		if last.Text() != "hi" {
			t.Errorf("Last().Text() = %q, want %q", last.Text(), "hi")
		}
	})

	t.Run("empty conversation", func(t *testing.T) {
		conv := NewConversation()
		_, err := conv.Last()
		if err == nil {
			t.Error("Last() on empty conversation should return error")
		}
	})
}

func TestConversation_Len(t *testing.T) {
	conv := NewConversation()

	if conv.Len() != 0 {
		t.Errorf("empty conversation Len() = %d, want 0", conv.Len())
	}

	conv.Add(UserMessage("a"))
	if conv.Len() != 1 {
		t.Errorf("Len() = %d, want 1", conv.Len())
	}
}

func TestConversation_IsEmpty(t *testing.T) {
	conv := NewConversation()
	if !conv.IsEmpty() {
		t.Error("new conversation IsEmpty() = false, want true")
	}

	conv.Add(UserMessage("hello"))
	if conv.IsEmpty() {
		t.Error("after Add, IsEmpty() = true, want false")
	}
}

func TestConversation_Filter(t *testing.T) {
	conv := NewConversation()
	conv.Add(
		SystemMessage("system"),
		UserMessage("hello"),
		AssistantMessage("hi"),
		UserMessage("how are you?"),
		AssistantMessage("good"),
	)

	filtered := conv.Filter(func(msg Message) bool {
		return msg.Role == RoleUser
	})

	if filtered.ID != conv.ID {
		t.Errorf("Filter().ID = %q, want %q", filtered.ID, conv.ID)
	}
	if filtered.Len() != 2 {
		t.Fatalf("Filter(by user).Len() = %d, want 2", filtered.Len())
	}
	if filtered.Messages[0].Text() != "hello" {
		t.Errorf("Filter()[0] text = %q, want %q", filtered.Messages[0].Text(), "hello")
	}
	if filtered.Messages[1].Text() != "how are you?" {
		t.Errorf("Filter()[1] text = %q, want %q", filtered.Messages[1].Text(), "how are you?")
	}

	// Original should be unchanged
	if conv.Len() != 5 {
		t.Errorf("original Len() = %d, want 5", conv.Len())
	}
}

func TestConversation_FilterByRole(t *testing.T) {
	conv := NewConversation()
	conv.Add(
		SystemMessage("system"),
		UserMessage("hello"),
		AssistantMessage("hi"),
		UserMessage("how are you?"),
	)

	tests := []struct {
		name     string
		role     Role
		wantLen  int
		wantText string
	}{
		{"system messages", RoleSystem, 1, "system"},
		{"user messages", RoleUser, 2, "hello"},
		{"assistant messages", RoleAssistant, 1, "hi"},
		{"tool messages", RoleTool, 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := conv.FilterByRole(tt.role)
			if filtered.Len() != tt.wantLen {
				t.Errorf("FilterByRole(%q).Len() = %d, want %d", tt.role, filtered.Len(), tt.wantLen)
			}
			if tt.wantLen > 0 && filtered.Messages[0].Text() != tt.wantText {
				t.Errorf("FilterByRole(%q)[0].Text() = %q, want %q", tt.role, filtered.Messages[0].Text(), tt.wantText)
			}
		})
	}
}

func TestConversation_Truncate(t *testing.T) {
	conv := NewConversation()
	conv.Add(
		SystemMessage("s1"),
		UserMessage("u1"),
		AssistantMessage("a1"),
		UserMessage("u2"),
		AssistantMessage("a2"),
	)

	tests := []struct {
		name    string
		max     int
		wantLen int
		wantFirst string
		wantLast  string
	}{
		{
			name:       "truncate to 3",
			max:        3,
			wantLen:    3,
			wantFirst:  "a1",
			wantLast:   "a2",
		},
		{
			name:       "truncate to 1",
			max:        1,
			wantLen:    1,
			wantFirst:  "a2",
			wantLast:   "a2",
		},
		{
			name:       "no truncation needed",
			max:        10,
			wantLen:    5,
			wantFirst:  "s1",
			wantLast:   "a2",
		},
		{
			name:       "truncate to exact size",
			max:        5,
			wantLen:    5,
			wantFirst:  "s1",
			wantLast:   "a2",
		},
		{
			name:       "truncate to 0",
			max:        0,
			wantLen:    0,
			wantFirst:  "",
			wantLast:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := conv.Truncate(tt.max)
			if result.Len() != tt.wantLen {
				t.Errorf("Truncate(%d).Len() = %d, want %d", tt.max, result.Len(), tt.wantLen)
			}
			if tt.wantLen > 0 {
				if result.Messages[0].Text() != tt.wantFirst {
					t.Errorf("Truncate(%d)[0] = %q, want %q", tt.max, result.Messages[0].Text(), tt.wantFirst)
				}
				if result.Messages[result.Len()-1].Text() != tt.wantLast {
					t.Errorf("Truncate(%d)[-1] = %q, want %q", tt.max, result.Messages[result.Len()-1].Text(), tt.wantLast)
				}
			}
			// Original should be unchanged
			if conv.Len() != 5 {
				t.Errorf("original Len() changed to %d, want 5", conv.Len())
			}
		})
	}
}

func TestConversation_TruncatePreservingSystem(t *testing.T) {
	conv := NewConversation()
	conv.Add(
		SystemMessage("system instructions"),
		UserMessage("u1"),
		AssistantMessage("a1"),
		UserMessage("u2"),
		AssistantMessage("a2"),
	)

	tests := []struct {
		name           string
		max            int
		wantLen        int
		wantFirstText  string
		wantLastText   string
	}{
		{
			name:          "truncate to 3 preserves system",
			max:           3,
			wantLen:       3,
			wantFirstText: "system instructions",
			wantLastText:  "a2",
		},
		{
			name:          "truncate to 2 preserves system plus one",
			max:           2,
			wantLen:       2,
			wantFirstText: "system instructions",
			wantLastText:  "a2",
		},
		{
			name:          "truncate to 1 keeps only system",
			max:           1,
			wantLen:       1,
			wantFirstText: "system instructions",
			wantLastText:  "system instructions",
		},
		{
			name:          "no truncation needed",
			max:           10,
			wantLen:       5,
			wantFirstText: "system instructions",
			wantLastText:  "a2",
		},
		{
			name:          "truncate to 0",
			max:           0,
			wantLen:       0,
			wantFirstText: "",
			wantLastText:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := conv.TruncatePreservingSystem(tt.max)
			if result.Len() != tt.wantLen {
				t.Errorf("TruncatePreservingSystem(%d).Len() = %d, want %d", tt.max, result.Len(), tt.wantLen)
			}
			if tt.wantLen > 0 {
				if result.Messages[0].Text() != tt.wantFirstText {
					t.Errorf("first message = %q, want %q", result.Messages[0].Text(), tt.wantFirstText)
				}
				if result.Messages[result.Len()-1].Text() != tt.wantLastText {
					t.Errorf("last message = %q, want %q", result.Messages[result.Len()-1].Text(), tt.wantLastText)
				}
			}
		})
	}
}

func TestConversation_TruncatePreservingSystem_MultipleSystemMessages(t *testing.T) {
	conv := NewConversation()
	conv.Add(
		SystemMessage("primary instructions"),
		SystemMessage("secondary instructions"),
		UserMessage("u1"),
		AssistantMessage("a1"),
		UserMessage("u2"),
	)

	// Truncate to 3 — both system messages + most recent non-system
	result := conv.TruncatePreservingSystem(3)
	if result.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", result.Len())
	}
	if result.Messages[0].Text() != "primary instructions" {
		t.Errorf("first = %q, want %q", result.Messages[0].Text(), "primary instructions")
	}
	if result.Messages[1].Text() != "secondary instructions" {
		t.Errorf("second = %q, want %q", result.Messages[1].Text(), "secondary instructions")
	}
	if result.Messages[2].Text() != "u2" {
		t.Errorf("third = %q, want %q", result.Messages[2].Text(), "u2")
	}
}

func TestConversation_TruncatePreservingSystem_NoSystemMessages(t *testing.T) {
	conv := NewConversation()
	conv.Add(
		UserMessage("u1"),
		AssistantMessage("a1"),
		UserMessage("u2"),
		AssistantMessage("a2"),
	)

	result := conv.TruncatePreservingSystem(2)
	if result.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", result.Len())
	}
	if result.Messages[0].Text() != "u2" {
		t.Errorf("first = %q, want %q", result.Messages[0].Text(), "u2")
	}
	if result.Messages[1].Text() != "a2" {
		t.Errorf("second = %q, want %q", result.Messages[1].Text(), "a2")
	}
}

func TestConversation_Clone(t *testing.T) {
	conv := NewConversation()
	conv.Add(
		SystemMessage("sys"),
		UserMessage("hello"),
	)
	conv.SetMetadata("key", "value")

	cloned := conv.Clone()

	if cloned.ID != conv.ID {
		t.Errorf("Clone().ID = %q, want %q", cloned.ID, conv.ID)
	}
	if cloned.Len() != conv.Len() {
		t.Errorf("Clone().Len() = %d, want %d", cloned.Len(), conv.Len())
	}
	if cloned.Messages[0].Text() != "sys" {
		t.Errorf("Clone()[0].Text() = %q, want %q", cloned.Messages[0].Text(), "sys")
	}

	// Verify deep copy
	cloned.Add(AssistantMessage("hi"))
	if conv.Len() != 2 {
		t.Error("adding to clone affected original")
	}

	cloned.Messages[0] = UserMessage("modified")
	if conv.Messages[0].Text() != "sys" {
		t.Error("modifying clone messages affected original")
	}
}

func TestConversation_Clone_Empty(t *testing.T) {
	conv := NewConversation()
	cloned := conv.Clone()

	if cloned.ID != conv.ID {
		t.Errorf("Clone().ID = %q, want %q", cloned.ID, conv.ID)
	}
	if cloned.Len() != 0 {
		t.Errorf("Clone().Len() = %d, want 0", cloned.Len())
	}
	if cloned.Metadata == nil {
		t.Error("Clone().Metadata is nil")
	}
}

func TestConversation_Format(t *testing.T) {
	conv := NewConversation()
	conv.Add(
		SystemMessage("you are helpful"),
		UserMessage("hello"),
		AssistantMessage("hi there"),
	)

	formatted := conv.Format()

	if !strings.Contains(formatted, "[system] you are helpful") {
		t.Errorf("Format() missing system message, got: %q", formatted)
	}
	if !strings.Contains(formatted, "[user] hello") {
		t.Errorf("Format() missing user message, got: %q", formatted)
	}
	if !strings.Contains(formatted, "[assistant] hi there") {
		t.Errorf("Format() missing assistant message, got: %q", formatted)
	}
}

func TestConversation_Format_Empty(t *testing.T) {
	conv := NewConversation()
	formatted := conv.Format()
	if formatted != "" {
		t.Errorf("Format() on empty = %q, want empty string", formatted)
	}
}

func TestConversation_Metadata(t *testing.T) {
	conv := NewConversation()

	// Set and get metadata
	conv.SetMetadata("model", "gpt-4")
	conv.SetMetadata("temperature", 0.7)

	val, ok := conv.GetMetadata("model")
	if !ok {
		t.Error("GetMetadata(\"model\") not found")
	}
	if val != "gpt-4" {
		t.Errorf("GetMetadata(\"model\") = %v, want %q", val, "gpt-4")
	}

	val, ok = conv.GetMetadata("temperature")
	if !ok {
		t.Error("GetMetadata(\"temperature\") not found")
	}
	if val != 0.7 {
		t.Errorf("GetMetadata(\"temperature\") = %v, want 0.7", val)
	}

	// Non-existent key
	_, ok = conv.GetMetadata("nonexistent")
	if ok {
		t.Error("GetMetadata(\"nonexistent\") should return false")
	}
}

func TestConversation_Metadata_NilInit(t *testing.T) {
	conv := &Conversation{
		ID:       "test",
		Messages: []Message{},
	}

	// Metadata is nil — SetMetadata should initialize it
	conv.SetMetadata("key", "value")

	if conv.Metadata == nil {
		t.Error("SetMetadata should initialize nil metadata map")
	}
	if conv.Metadata["key"] != "value" {
		t.Errorf("Metadata[\"key\"] = %v, want %q", conv.Metadata["key"], "value")
	}
}

// --- Integration-style tests ---

func TestConversation_FullLifecycle(t *testing.T) {
	conv := NewConversation()

	// Add system message
	conv.Add(SystemMessage("You are a helpful math tutor."))

	// User asks question
	conv.Add(UserMessage("What is 2 + 2?"))

	// Assistant responds
	conv.Add(AssistantMessage("2 + 2 = 4."))

	// Verify state
	if conv.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", conv.Len())
	}

	last, err := conv.Last()
	if err != nil {
		t.Fatalf("Last() error = %v", err)
	}
	if last.Role != RoleAssistant {
		t.Errorf("Last().Role = %v, want %v", last.Role, RoleAssistant)
	}

	// User follows up
	conv.Add(UserMessage("And 3 + 3?"))

	// Assistant calls a tool
	conv.Add(AssistantToolCallMessage([]ToolCall{
		{
			ID:   "call_calc",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "calculator",
				Arguments: `{"expression": "3 + 3"}`,
			},
		},
	}))

	// Tool returns result
	conv.Add(ToolResultMessage("call_calc", "6", false))

	// Assistant gives final answer
	conv.Add(AssistantMessage("3 + 3 = 6."))

	// Verify full conversation
	if conv.Len() != 7 {
		t.Errorf("Len() = %d, want 7", conv.Len())
	}

	// Filter to only user messages
	userOnly := conv.FilterByRole(RoleUser)
	if userOnly.Len() != 2 {
		t.Errorf("user messages = %d, want 2", userOnly.Len())
	}

	// Truncate while preserving system message
	truncated := conv.TruncatePreservingSystem(4)
	if truncated.Len() != 4 {
		t.Errorf("truncated len = %d, want 4", truncated.Len())
	}
	if truncated.Messages[0].Role != RoleSystem {
		t.Errorf("truncated first message role = %v, want %v", truncated.Messages[0].Role, RoleSystem)
	}

	// Format
	formatted := conv.Format()
	if !strings.Contains(formatted, "[system]") {
		t.Error("Format() missing [system]")
	}
	if !strings.Contains(formatted, "6.") {
		t.Error("Format() missing final answer")
	}
}

func TestToolCallMessage_HasEmptyContent(t *testing.T) {
	msg := AssistantToolCallMessage([]ToolCall{
		{ID: "call_1", Type: "function", Function: ToolCallFunction{Name: "foo", Arguments: "{}"}},
	})

	// Tool call messages may have no text content
	if msg.Text() != "" {
		t.Errorf("tool call message Text() = %q, want empty", msg.Text())
	}
	if !msg.IsToolCall() {
		t.Error("tool call message IsToolCall() = false, want true")
	}
}

func TestMessage_WithMetadata(t *testing.T) {
	msg := Message{
		Role:    RoleUser,
		Content: []ContentBlock{TextContentBlock("hello")},
		Metadata: map[string]any{
			"source":  "api",
			"version": 2,
		},
	}

	if msg.Metadata["source"] != "api" {
		t.Errorf("Metadata[source] = %v, want %q", msg.Metadata["source"], "api")
	}
	if msg.Metadata["version"] != 2 {
		t.Errorf("Metadata[version] = %v, want 2", msg.Metadata["version"])
	}
}

func TestConversation_MultipleContentBlocks(t *testing.T) {
	msg := Message{
		Role: RoleUser,
		Content: []ContentBlock{
			TextContentBlock("What's in this image?"),
			ImageContentBlock("https://example.com/photo.jpg", "image/jpeg"),
			TextContentBlock("Please describe it in detail."),
		},
	}

	text := msg.Text()
	if text != "What's in this image?Please describe it in detail." {
		t.Errorf("Text() = %q, want concatenated text blocks", text)
	}
}
