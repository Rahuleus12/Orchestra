package memory

import (
	"context"
	"sync"

	"github.com/user/orchestra/internal/message"
)

// SlidingWindowMemory maintains a fixed-size window of the most recent messages.
// It can be configured with either a maximum number of messages or a maximum
// token count, automatically evicting older messages when limits are exceeded.
type SlidingWindowMemory struct {
	mu          sync.RWMutex
	messages    []message.Message
	maxMessages int // Maximum number of messages (0 = no limit)
	maxTokens   int // Maximum total tokens (0 = no limit)
	tokenizer   Tokenizer
}

// NewSlidingWindowMemory creates a new SlidingWindowMemory with message count limit.
// If maxMessages is 0 or negative, there is no limit on message count.
func NewSlidingWindowMemory(maxMessages int) *SlidingWindowMemory {
	return &SlidingWindowMemory{
		messages:    make([]message.Message, 0),
		maxMessages: maxMessages,
		maxTokens:   0,
		tokenizer:   nil,
	}
}

// NewSlidingWindowMemoryWithTokens creates a new SlidingWindowMemory with token count limit.
// Messages will be removed from the beginning to keep total tokens below maxTokens.
// If tokenizer is nil, token counting will be disabled and only maxMessages will apply.
func NewSlidingWindowMemoryWithTokens(maxTokens int, tokenizer Tokenizer) *SlidingWindowMemory {
	return &SlidingWindowMemory{
		messages:    make([]message.Message, 0),
		maxMessages: 0,
		maxTokens:   maxTokens,
		tokenizer:   tokenizer,
	}
}

// NewSlidingWindowMemoryFull creates a new SlidingWindowMemory with both message and token limits.
// Both limits will be enforced; messages will be truncated to satisfy the more restrictive constraint.
func NewSlidingWindowMemoryFull(maxMessages, maxTokens int, tokenizer Tokenizer) *SlidingWindowMemory {
	return &SlidingWindowMemory{
		messages:    make([]message.Message, 0),
		maxMessages: maxMessages,
		maxTokens:   maxTokens,
		tokenizer:   tokenizer,
	}
}

// Add stores a message in memory, enforcing the sliding window constraints.
// Older messages are automatically removed to maintain the window size.
func (m *SlidingWindowMemory) Add(ctx context.Context, msg message.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages = append(m.messages, msg)

	// Enforce constraints
	m.enforceConstraints()

	return nil
}

// enforceConstraints ensures the memory stays within its configured limits.
// It removes messages from the beginning (oldest first) until constraints are satisfied.
func (m *SlidingWindowMemory) enforceConstraints() {
	// Enforce message count limit
	if m.maxMessages > 0 {
		for len(m.messages) > m.maxMessages {
			m.messages = m.messages[1:]
		}
	}

	// Enforce token count limit
	if m.maxTokens > 0 && m.tokenizer != nil {
		for {
			totalTokens := CountTokensInMessages(m.messages, m.tokenizer)
			if totalTokens <= m.maxTokens || len(m.messages) == 0 {
				break
			}
			// Remove the oldest message
			m.messages = m.messages[1:]
		}
	}
}

// GetRelevant retrieves messages relevant to the given input.
// For SlidingWindowMemory, relevance is based on recency - it returns all
// messages currently in the sliding window (up to the specified additional limits).
func (m *SlidingWindowMemory) GetRelevant(ctx context.Context, input string, opts GetOptions) ([]message.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.getMessages(opts), nil
}

// GetAll retrieves all messages currently in the sliding window, respecting the options.
func (m *SlidingWindowMemory) GetAll(ctx context.Context, opts GetOptions) ([]message.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.getMessages(opts), nil
}

// getMessages is a helper that returns messages respecting the GetOptions.
func (m *SlidingWindowMemory) getMessages(opts GetOptions) []message.Message {
	// Copy all messages
	msgs := make([]message.Message, len(m.messages))
	copy(msgs, m.messages)

	// Apply additional token limit if specified in options
	if opts.MaxTokens > 0 && opts.Tokenizer != nil {
		msgs = TruncateToTokenLimit(msgs, opts.MaxTokens, opts.Tokenizer)
	}

	// Apply additional message limit if specified in options
	if opts.Limit > 0 && len(msgs) > opts.Limit {
		// Keep the most recent messages
		start := len(msgs) - opts.Limit
		msgs = msgs[start:]
	}

	return msgs
}

// Clear removes all messages from memory.
func (m *SlidingWindowMemory) Clear(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages = make([]message.Message, 0)
	return nil
}

// Size returns the current number of messages in the window.
func (m *SlidingWindowMemory) Size(ctx context.Context) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.messages)
}

// GetMaxMessages returns the configured maximum message limit.
// Returns 0 if there is no limit.
func (m *SlidingWindowMemory) GetMaxMessages() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.maxMessages
}

// GetMaxTokens returns the configured maximum token limit.
// Returns 0 if there is no limit.
func (m *SlidingWindowMemory) GetMaxTokens() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.maxTokens
}

// GetTokenizer returns the configured tokenizer.
func (m *SlidingWindowMemory) GetTokenizer() Tokenizer {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.tokenizer
}

// SetMaxMessages updates the maximum message limit.
// If the new limit is smaller than the current number of messages,
// the oldest messages will be removed to fit the new limit.
func (m *SlidingWindowMemory) SetMaxMessages(maxMessages int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.maxMessages = maxMessages
	m.enforceConstraints()
}

// SetMaxTokens updates the maximum token limit.
// Messages will be removed to satisfy the new token limit.
// If tokenizer is nil, token counting will be disabled.
func (m *SlidingWindowMemory) SetMaxTokens(maxTokens int, tokenizer Tokenizer) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.maxTokens = maxTokens
	m.tokenizer = tokenizer
	m.enforceConstraints()
}

// GetCurrentTokenCount returns the total number of tokens in all messages.
// Returns 0 if no tokenizer is configured.
func (m *SlidingWindowMemory) GetCurrentTokenCount(ctx context.Context) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.tokenizer == nil {
		return 0
	}
	return CountTokensInMessages(m.messages, m.tokenizer)
}

// IsFull returns true if the memory is at or exceeds its configured limits.
func (m *SlidingWindowMemory) IsFull(ctx context.Context) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check message count limit
	if m.maxMessages > 0 && len(m.messages) >= m.maxMessages {
		return true
	}

	// Check token count limit
	if m.maxTokens > 0 && m.tokenizer != nil {
		totalTokens := CountTokensInMessages(m.messages, m.tokenizer)
		if totalTokens >= m.maxTokens {
			return true
		}
	}

	return false
}
