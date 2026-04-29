// Package memory provides token counting utilities for different model families.
package memory

import (
	"strings"
	"unicode"

	"github.com/user/orchestra/internal/message"
)

// ModelFamily represents different LLM model families with their tokenization characteristics.
type ModelFamily string

const (
	// ModelFamilyOpenAI represents OpenAI models (GPT-3, GPT-4, etc.)
	ModelFamilyOpenAI ModelFamily = "openai"
	// ModelFamilyAnthropic represents Anthropic models (Claude)
	ModelFamilyAnthropic ModelFamily = "anthropic"
	// ModelFamilyGoogle represents Google models (Gemini, PaLM)
	ModelFamilyGoogle ModelFamily = "google"
	// ModelFamilyMistral represents Mistral models
	ModelFamilyMistral ModelFamily = "mistral"
	// ModelFamilyGeneric represents models where we use approximation
	ModelFamilyGeneric ModelFamily = "generic"
)

// NewTokenizer creates a tokenizer for the specified model family.
// Returns an approximation-based tokenizer by default.
func NewTokenizer(family ModelFamily) Tokenizer {
	switch family {
	case ModelFamilyOpenAI:
		return NewOpenAITokenizer()
	case ModelFamilyAnthropic:
		return NewAnthropicTokenizer()
	case ModelFamilyGoogle:
		return NewGoogleTokenizer()
	case ModelFamilyMistral:
		return NewMistralTokenizer()
	default:
		return NewApproxTokenizer()
	}
}

// ApproxTokenizer provides a simple approximation-based tokenizer
// for models without exact tokenization support.
type ApproxTokenizer struct {
	// charsPerToken is the approximate number of characters per token.
	// This varies by language but 4 is a reasonable average for English.
	charsPerToken float64
}

// NewApproxTokenizer creates a new approximation tokenizer.
func NewApproxTokenizer() *ApproxTokenizer {
	return &ApproxTokenizer{
		charsPerToken: 4.0,
	}
}

// CountTokens returns an approximate token count based on character length.
func (t *ApproxTokenizer) CountTokens(text string) int {
	if text == "" {
		return 0
	}
	// Simple approximation: divide character count by average chars per token
	// Add 1 for any remainder to avoid undercounting
	count := int(float64(len(text)) / t.charsPerToken)
	if count == 0 && len(text) > 0 {
		return 1
	}
	return count
}

// CountTokensInMessage returns an approximate token count for a message.
// This includes role metadata and content.
func (t *ApproxTokenizer) CountTokensInMessage(msg message.Message) int {
	// Base tokens for message formatting (role, separators, etc.)
	baseTokens := 4

	// Count tokens in role
	baseTokens += t.CountTokens(string(msg.Role))

	// Count tokens in name if present
	if msg.Name != "" {
		baseTokens += t.CountTokens(msg.Name)
	}

	// Count tokens in content blocks
	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			baseTokens += t.CountTokens(block.Text)
		case "image":
			// Images are typically counted as a fixed number of tokens
			// This is a simplified estimate
			baseTokens += 85
		case "file":
			// Files are more complex - approximate based on size
			baseTokens += len(block.FileData)/100 + 10
		}
	}

	// Count tokens in tool calls if present
	for _, call := range msg.ToolCalls {
		baseTokens += t.CountTokens(call.Type)
		baseTokens += t.CountTokens(call.Function.Name)
		baseTokens += t.CountTokens(call.Function.Arguments)
	}

	// Count tokens in tool result if present
	if msg.ToolResult != nil {
		baseTokens += t.CountTokens(msg.ToolResult.ToolCallID)
		baseTokens += t.CountTokens(msg.ToolResult.Content)
	}

	return baseTokens
}

// OpenAITokenizer provides tokenization for OpenAI models using cl100k_base encoding.
// This uses tiktoken-compatible tokenization.
type OpenAITokenizer struct {
	// encoding represents the tiktoken encoding
	// In a full implementation, this would use a tiktoken Go library
	// For now, we use a more sophisticated approximation
}

// NewOpenAITokenizer creates a new OpenAI tokenizer.
func NewOpenAITokenizer() *OpenAITokenizer {
	return &OpenAITokenizer{}
}

// CountTokens returns the token count using cl100k_base encoding approximation.
func (t *OpenAITokenizer) CountTokens(text string) int {
	if text == "" {
		return 0
	}

	// This is an approximation of cl100k_base encoding
	// For production, integrate a proper tiktoken Go implementation
	// See: github.com/pkoukk/tiktoken-go or similar

	// Use word-based approximation with special handling
	words := strings.Fields(text)
	tokens := 0

	for _, word := range words {
		// Most words are 1-2 tokens
		tokens++

		// Common subword patterns that add tokens
		if strings.Contains(word, "ing") || strings.Contains(word, "tion") ||
			strings.Contains(word, "ness") || strings.Contains(word, "ment") {
			tokens++
		}

		// Numbers and special characters often split
		for _, r := range word {
			if unicode.IsDigit(r) || unicode.IsSymbol(r) || unicode.IsPunct(r) {
				tokens++
			}
		}
	}

	// Account for whitespace and formatting
	tokens += len(words) / 4

	return tokens
}

// CountTokensInMessage returns the token count for a message using OpenAI's format.
func (t *OpenAITokenizer) CountTokensInMessage(msg message.Message) int {
	// OpenAI format includes specific tokens for message structure
	tokens := 3 // Start of message, role, end of role

	// Add role tokens
	tokens += t.CountTokens(string(msg.Role))

	// Add name tokens if present
	if msg.Name != "" {
		tokens += 1 // Name key
		tokens += t.CountTokens(msg.Name)
	}

	// Add content tokens
	tokens += 1 // Content key
	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			tokens += t.CountTokens(block.Text)
		case "image":
			// OpenAI charges 85 tokens for low-res images
			// High-res images use a different formula
			tokens += 85
		case "file":
			// File tokens depend on size and type
			tokens += len(block.FileData)/100 + 10
		}
	}

	// Add tool calls
	if len(msg.ToolCalls) > 0 {
		tokens += 2 // Tool calls array start/end
		for _, call := range msg.ToolCalls {
			tokens += 5 // Basic tool call structure
			tokens += t.CountTokens(call.Type)
			tokens += t.CountTokens(call.Function.Name)
			tokens += t.CountTokens(call.Function.Arguments)
		}
	}

	// Add tool result
	if msg.ToolResult != nil {
		tokens += 4 // Tool result structure
		tokens += t.CountTokens(msg.ToolResult.ToolCallID)
		tokens += t.CountTokens(msg.ToolResult.Content)
	}

	return tokens
}

// AnthropicTokenizer provides tokenization for Anthropic Claude models.
type AnthropicTokenizer struct {
	// Similar to OpenAI but with different characteristics
}

// NewAnthropicTokenizer creates a new Anthropic tokenizer.
func NewAnthropicTokenizer() *AnthropicTokenizer {
	return &AnthropicTokenizer{}
}

// CountTokens returns an approximation for Claude's tokenization.
func (t *AnthropicTokenizer) CountTokens(text string) int {
	if text == "" {
		return 0
	}

	// Claude uses a different tokenizer than GPT
	// This is an approximation
	words := strings.Fields(text)
	tokens := 0

	for _, word := range words {
		// Claude tends to have slightly different subword boundaries
		tokens++

		// Claude handles certain affixes differently
		if strings.HasSuffix(word, "ly") || strings.HasSuffix(word, "ed") {
			tokens++
		}

		// Special character handling
		for _, r := range word {
			if unicode.IsSymbol(r) || (!unicode.IsLetter(r) && !unicode.IsDigit(r)) {
				tokens++
			}
		}
	}

	return tokens + len(words)/5
}

// CountTokensInMessage returns the token count for a message using Anthropic's format.
func (t *AnthropicTokenizer) CountTokensInMessage(msg message.Message) int {
	tokens := 4 // Anthropic message overhead

	tokens += t.CountTokens(string(msg.Role))

	if msg.Name != "" {
		tokens += t.CountTokens(msg.Name)
	}

	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			tokens += t.CountTokens(block.Text)
		case "image":
			tokens += 100 // Claude's estimate for images
		case "file":
			tokens += len(block.FileData)/100 + 10
		}
	}

	// Tool handling for Claude
	for _, call := range msg.ToolCalls {
		tokens += t.CountTokens(call.Function.Name)
		tokens += t.CountTokens(call.Function.Arguments)
	}

	if msg.ToolResult != nil {
		tokens += t.CountTokens(msg.ToolResult.ToolCallID)
		tokens += t.CountTokens(msg.ToolResult.Content)
	}

	return tokens
}

// GoogleTokenizer provides tokenization for Google models (Gemini, PaLM).
type GoogleTokenizer struct{}

// NewGoogleTokenizer creates a new Google tokenizer.
func NewGoogleTokenizer() *GoogleTokenizer {
	return &GoogleTokenizer{}
}

// CountTokens returns an approximation for Google's tokenization.
func (t *GoogleTokenizer) CountTokens(text string) int {
	if text == "" {
		return 0
	}

	// Google's tokenization is similar to SentencePiece
	words := strings.Fields(text)
	tokens := 0

	for _, word := range words {
		tokens++

		// Google models handle compound words and subwords differently
		if strings.Contains(word, "-") || strings.Contains(word, "_") {
			tokens++
		}

		// Count special characters
		for _, r := range word {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsSpace(r) {
				tokens++
			}
		}
	}

	return tokens
}

// CountTokensInMessage returns the token count for a message using Google's format.
func (t *GoogleTokenizer) CountTokensInMessage(msg message.Message) int {
	tokens := 3 // Base message structure

	tokens += t.CountTokens(string(msg.Role))

	if msg.Name != "" {
		tokens += t.CountTokens(msg.Name)
	}

	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			tokens += t.CountTokens(block.Text)
		case "image":
			tokens += 258 // Google's fixed cost for images
		case "file":
			tokens += len(block.FileData)/100 + 10
		}
	}

	// Tool handling
	for _, call := range msg.ToolCalls {
		tokens += t.CountTokens(call.Function.Name)
		tokens += t.CountTokens(call.Function.Arguments)
	}

	if msg.ToolResult != nil {
		tokens += t.CountTokens(msg.ToolResult.ToolCallID)
		tokens += t.CountTokens(msg.ToolResult.Content)
	}

	return tokens
}

// MistralTokenizer provides tokenization for Mistral models.
type MistralTokenizer struct{}

// NewMistralTokenizer creates a new Mistral tokenizer.
func NewMistralTokenizer() *MistralTokenizer {
	return &MistralTokenizer{}
}

// CountTokens returns an approximation for Mistral's tokenization.
func (t *MistralTokenizer) CountTokens(text string) int {
	if text == "" {
		return 0
	}

	// Mistral uses a tokenizer similar to Llama 2
	words := strings.Fields(text)
	tokens := 0

	for _, word := range words {
		tokens++

		// Common patterns
		if strings.Contains(word, "ing") || strings.Contains(word, "tion") {
			tokens++
		}
	}

	return tokens
}

// CountTokensInMessage returns the token count for a message using Mistral's format.
func (t *MistralTokenizer) CountTokensInMessage(msg message.Message) int {
	tokens := 3 // Base structure

	tokens += t.CountTokens(string(msg.Role))

	if msg.Name != "" {
		tokens += t.CountTokens(msg.Name)
	}

	for _, block := range msg.Content {
		if block.Type == "text" {
			tokens += t.CountTokens(block.Text)
		}
	}

	// Tool handling
	for _, call := range msg.ToolCalls {
		tokens += t.CountTokens(call.Function.Name)
		tokens += t.CountTokens(call.Function.Arguments)
	}

	if msg.ToolResult != nil {
		tokens += t.CountTokens(msg.ToolResult.ToolCallID)
		tokens += t.CountTokens(msg.ToolResult.Content)
	}

	return tokens
}

// CountTokensInMessages is a utility function to count total tokens in a slice of messages.
func CountTokensInMessages(msgs []message.Message, tokenizer Tokenizer) int {
	if tokenizer == nil {
		return 0
	}

	total := 0
	for _, msg := range msgs {
		total += tokenizer.CountTokensInMessage(msg)
	}
	return total
}

// TruncateToTokenLimit truncates a message slice to fit within the token limit.
// It preserves system messages and truncates from the beginning (oldest first).
// Returns a new slice with messages that fit within the limit.
func TruncateToTokenLimit(msgs []message.Message, maxTokens int, tokenizer Tokenizer) []message.Message {
	if tokenizer == nil || maxTokens <= 0 {
		return msgs
	}

	// Separate system messages from others
	var systemMsgs []message.Message
	var otherMsgs []message.Message

	for _, msg := range msgs {
		if msg.Role == message.RoleSystem {
			systemMsgs = append(systemMsgs, msg)
		} else {
			otherMsgs = append(otherMsgs, msg)
		}
	}

	// Count tokens in system messages
	systemTokens := CountTokensInMessages(systemMsgs, tokenizer)

	// If system messages already exceed the limit, truncate them too
	if systemTokens >= maxTokens {
		// Keep only the most recent system messages
		result := make([]message.Message, 0)
		remaining := maxTokens

		for i := len(systemMsgs) - 1; i >= 0; i-- {
			tokens := tokenizer.CountTokensInMessage(systemMsgs[i])
			if tokens <= remaining {
				result = append([]message.Message{systemMsgs[i]}, result...)
				remaining -= tokens
			} else {
				break
			}
		}
		return result
	}

	// Add system messages and fit as many other messages as possible
	result := make([]message.Message, len(systemMsgs))
	copy(result, systemMsgs)
	remaining := maxTokens - systemTokens

	// Add messages from the end (most recent first)
	for i := len(otherMsgs) - 1; i >= 0; i-- {
		tokens := tokenizer.CountTokensInMessage(otherMsgs[i])
		if tokens <= remaining {
			result = append([]message.Message{otherMsgs[i]}, result...)
			remaining -= tokens
		}
	}

	return result
}
