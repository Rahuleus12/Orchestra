package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/user/orchestra/internal/message"
	"github.com/user/orchestra/internal/provider"
)

// SummaryMemory maintains a conversation history by summarizing older messages
// when the memory exceeds configured limits. It uses an LLM to generate concise
// summaries of message groups, preserving key information while reducing token usage.
type SummaryMemory struct {
	mu                sync.RWMutex
	messages          []message.Message       // Current (unsummarized) messages
	summaries         []message.Message       // Summary messages (typically system messages)
	provider          provider.Provider       // LLM provider for generating summaries
	summaryModel      string                  // Model to use for summarization
	maxMessages       int                     // Max messages before summarizing
	maxTokens         int                     // Max tokens before summarizing
	tokenizer         Tokenizer               // For counting tokens
	summaryThreshold  int                     // Messages to summarize at once
	summaryPrompt     string                  // Custom prompt for summarization
	humanMessagePrefix string                 // Prefix for human messages in summary
	aiMessagePrefix   string                 // Prefix for AI messages in summary
}

// NewSummaryMemory creates a new SummaryMemory with the specified provider and limits.
// The provider is used to generate summaries of older messages.
//
// Parameters:
//   - prov: The LLM provider to use for generating summaries
//   - summaryModel: The model to use for summarization (e.g., "gpt-4", "claude-3")
//   - maxMessages: Maximum number of unsummarized messages to keep (0 = no limit)
//   - maxTokens: Maximum total tokens before summarizing (0 = no limit)
//   - tokenizer: Tokenizer for counting tokens (required if maxTokens > 0)
func NewSummaryMemory(prov provider.Provider, summaryModel string, maxMessages, maxTokens int, tokenizer Tokenizer) *SummaryMemory {
	return &SummaryMemory{
		messages:          make([]message.Message, 0),
		summaries:         make([]message.Message, 0),
		provider:          prov,
		summaryModel:      summaryModel,
		maxMessages:       maxMessages,
		maxTokens:         maxTokens,
		tokenizer:         tokenizer,
		summaryThreshold:  10, // Default: summarize messages in groups of 10
		summaryPrompt:     "Summarize the following conversation concisely, capturing the main points, decisions, and context. Keep the summary brief and focused on information that would be useful for future conversation turns.",
		humanMessagePrefix: "Human: ",
		aiMessagePrefix:   "Assistant: ",
	}
}

// Add stores a message in memory, triggering summarization if limits are exceeded.
func (m *SummaryMemory) Add(ctx context.Context, msg message.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages = append(m.messages, msg)

	// Check if we need to summarize
	if m.shouldSummarize(ctx) {
		return m.summarizeOldestMessages(ctx)
	}

	return nil
}

// shouldSummarize checks if the memory has exceeded its configured limits.
func (m *SummaryMemory) shouldSummarize(ctx context.Context) bool {
	// Check message count limit
	if m.maxMessages > 0 && len(m.messages) > m.maxMessages {
		return true
	}

	// Check token count limit
	if m.maxTokens > 0 && m.tokenizer != nil {
		totalTokens := CountTokensInMessages(m.messages, m.tokenizer)
		if totalTokens > m.maxTokens {
			return true
		}
	}

	return false
}

// summarizeOldestMessages summarizes the oldest messages in the buffer.
func (m *SummaryMemory) summarizeOldestMessages(ctx context.Context) error {
	if len(m.messages) == 0 {
		return nil
	}

	// Determine how many messages to summarize
	// We want to keep the most recent messages (typically half the limit)
	messagesToSummarize := len(m.messages) / 2
	if messagesToSummarize < m.summaryThreshold {
		messagesToSummarize = m.summaryThreshold
	}
	if messagesToSummarize > len(m.messages) {
		messagesToSummarize = len(m.messages)
	}

	// Extract the oldest messages
	oldest := m.messages[:messagesToSummarize]
	remaining := m.messages[messagesToSummarize:]

	// Generate summary text
	summaryText, err := m.generateSummary(ctx, oldest)
	if err != nil {
		// If summarization fails, we still need to truncate to prevent unlimited growth
		// We'll truncate instead of summarizing
		m.messages = remaining
		return fmt.Errorf("failed to generate summary: %w", err)
	}

	// Create a summary message (as a system message)
	summaryMsg := message.SystemMessage(fmt.Sprintf("[Previous conversation summary]\n%s", summaryText))

	// Add to summaries
	m.summaries = append(m.summaries, summaryMsg)

	// Replace messages with remaining
	m.messages = remaining

	return nil
}

// generateSummary uses the LLM to create a summary of the given messages.
func (m *SummaryMemory) generateSummary(ctx context.Context, msgs []message.Message) (string, error) {
	// Build conversation text for summarization
	var convText strings.Builder
	for _, msg := range msgs {
		switch msg.Role {
		case message.RoleSystem:
			// Include system messages as context
			if msg.Text() != "" {
				convText.WriteString(fmt.Sprintf("[System: %s]\n", msg.Text()))
			}
		case message.RoleUser:
			convText.WriteString(m.humanMessagePrefix)
			convText.WriteString(msg.Text())
			convText.WriteString("\n")
		case message.RoleAssistant:
			convText.WriteString(m.aiMessagePrefix)
			convText.WriteString(msg.Text())
			convText.WriteString("\n")
		case message.RoleTool:
			// Include tool results
			convText.WriteString(fmt.Sprintf("[Tool result: %s]\n", msg.Text()))
		}
	}

	// Build the summary prompt
	prompt := fmt.Sprintf("%s\n\nConversation:\n%s", m.summaryPrompt, convText.String())

	// Create a request to the provider
	temperature := 0.3 // Lower temperature for more consistent summaries
	maxTokens := 500   // Limit summary length
	req := provider.GenerateRequest{
		Model: m.summaryModel,
		Messages: []message.Message{
			message.SystemMessage(prompt),
		},
		Options: provider.GenerateOptions{
			Temperature: &temperature,
			MaxTokens:   &maxTokens,
		},
	}

	// Generate the summary
	result, err := m.provider.Generate(ctx, req)
	if err != nil {
		return "", err
	}

	// Extract the summary text
	if len(result.Message.Content) > 0 {
		return result.Message.Text(), nil
	}

	return "", fmt.Errorf("no summary generated")
}

// GetRelevant retrieves messages relevant to the given input.
// For SummaryMemory, this returns all summaries plus the current messages,
// respecting the GetOptions constraints.
func (m *SummaryMemory) GetRelevant(ctx context.Context, input string, opts GetOptions) ([]message.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.getAllMessages(opts), nil
}

// GetAll retrieves all messages (summaries + current messages), respecting the options.
func (m *SummaryMemory) GetAll(ctx context.Context, opts GetOptions) ([]message.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.getAllMessages(opts), nil
}

// getAllMessages is a helper that combines summaries and current messages.
func (m *SummaryMemory) getAllMessages(opts GetOptions) []message.Message {
	// Combine summaries and messages
	allMsgs := make([]message.Message, 0, len(m.summaries)+len(m.messages))
	allMsgs = append(allMsgs, m.summaries...)
	allMsgs = append(allMsgs, m.messages...)

	// Apply token limit if specified
	if opts.MaxTokens > 0 && opts.Tokenizer != nil {
		allMsgs = TruncateToTokenLimit(allMsgs, opts.MaxTokens, opts.Tokenizer)
	}

	// Apply message limit if specified
	if opts.Limit > 0 && len(allMsgs) > opts.Limit {
		// Keep the most recent messages (which are at the end)
		start := len(allMsgs) - opts.Limit
		allMsgs = allMsgs[start:]
	}

	return allMsgs
}

// Clear removes all messages and summaries.
func (m *SummaryMemory) Clear(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages = make([]message.Message, 0)
	m.summaries = make([]message.Message, 0)
	return nil
}

// Size returns the total number of messages (summaries + current messages).
func (m *SummaryMemory) Size(ctx context.Context) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.summaries) + len(m.messages)
}

// GetSummaryCount returns the number of summary messages.
func (m *SummaryMemory) GetSummaryCount(ctx context.Context) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.summaries)
}

// GetMessageCount returns the number of unsummarized messages.
func (m *SummaryMemory) GetMessageCount(ctx context.Context) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.messages)
}

// GetCurrentTokenCount returns the total tokens in all messages (summaries + current).
// Returns 0 if no tokenizer is configured.
func (m *SummaryMemory) GetCurrentTokenCount(ctx context.Context) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.tokenizer == nil {
		return 0
	}

	allMsgs := append(m.summaries, m.messages...)
	return CountTokensInMessages(allMsgs, m.tokenizer)
}

// SetSummaryPrompt sets a custom prompt for summarization.
func (m *SummaryMemory) SetSummaryPrompt(prompt string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.summaryPrompt = prompt
}

// SetSummaryThreshold sets the number of messages to summarize at once.
func (m *SummaryMemory) SetSummaryThreshold(threshold int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.summaryThreshold = threshold
}

// SetMessagePrefixes sets the prefixes used when formatting messages for summarization.
func (m *SummaryMemory) SetMessagePrefixes(humanPrefix, aiPrefix string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.humanMessagePrefix = humanPrefix
	m.aiMessagePrefix = aiPrefix
}

// TriggerSummarization forces a summarization of the oldest messages,
// even if limits haven't been exceeded. This is useful for testing or
// manual memory management.
func (m *SummaryMemory) TriggerSummarization(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.summarizeOldestMessages(ctx)
}

// GetProvider returns the LLM provider used for summarization.
func (m *SummaryMemory) GetProvider() provider.Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.provider
}

// SetProvider updates the LLM provider used for summarization.
func (m *SummaryMemory) SetProvider(prov provider.Provider) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.provider = prov
}
