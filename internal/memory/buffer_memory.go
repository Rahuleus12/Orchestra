package memory

import (
	"context"
	"sync"

	"github.com/user/orchestra/internal/message"
)

// BufferMemory is a simple in-memory buffer that stores messages.
// It can optionally limit the number of messages stored.
type BufferMemory struct {
	mu       sync.RWMutex
	messages []message.Message
	maxSize  int // 0 means no limit
}

// NewBufferMemory creates a new BufferMemory with an optional size limit.
// If maxSize is 0 or negative, there is no limit on the number of messages.
func NewBufferMemory(maxSize int) *BufferMemory {
	return &BufferMemory{
		messages: make([]message.Message, 0),
		maxSize:  maxSize,
	}
}

// Add stores a message in memory.
// If the buffer has a size limit, it will remove the oldest messages
// when adding a new message would exceed the limit.
func (m *BufferMemory) Add(ctx context.Context, msg message.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages = append(m.messages, msg)

	// Enforce size limit by removing oldest messages
	if m.maxSize > 0 && len(m.messages) > m.maxSize {
		// Remove the oldest message(s)
		excess := len(m.messages) - m.maxSize
		m.messages = m.messages[excess:]
	}

	return nil
}

// GetRelevant retrieves messages relevant to the given input.
// For BufferMemory, relevance is based on recency - it returns the most
// recent messages up to the specified limit and token constraints.
func (m *BufferMemory) GetRelevant(ctx context.Context, input string, opts GetOptions) ([]message.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.getMessages(opts), nil
}

// GetAll retrieves all messages stored in memory, respecting the options.
func (m *BufferMemory) GetAll(ctx context.Context, opts GetOptions) ([]message.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.getMessages(opts), nil
}

// getMessages is a helper that returns messages respecting the GetOptions.
func (m *BufferMemory) getMessages(opts GetOptions) []message.Message {
	// Start with all messages
	msgs := make([]message.Message, len(m.messages))
	copy(msgs, m.messages)

	// Apply token limit if specified
	if opts.MaxTokens > 0 && opts.Tokenizer != nil {
		msgs = TruncateToTokenLimit(msgs, opts.MaxTokens, opts.Tokenizer)
	}

	// Apply message limit if specified
	if opts.Limit > 0 && len(msgs) > opts.Limit {
		// Keep the most recent messages
		start := len(msgs) - opts.Limit
		msgs = msgs[start:]
	}

	return msgs
}

// Clear removes all messages from memory.
func (m *BufferMemory) Clear(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages = make([]message.Message, 0)
	return nil
}

// Size returns the current number of messages.
func (m *BufferMemory) Size(ctx context.Context) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.messages)
}

// GetMaxSize returns the configured maximum size limit.
// Returns 0 if there is no limit.
func (m *BufferMemory) GetMaxSize() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.maxSize
}

// SetMaxSize updates the maximum size limit.
// If the new limit is smaller than the current number of messages,
// the oldest messages will be removed to fit the new limit.
func (m *BufferMemory) SetMaxSize(maxSize int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.maxSize = maxSize

	// Enforce new size limit
	if m.maxSize > 0 && len(m.messages) > m.maxSize {
		excess := len(m.messages) - m.maxSize
		m.messages = m.messages[excess:]
	}
}
