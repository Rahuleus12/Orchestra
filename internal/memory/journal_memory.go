package memory

import (
	"context"

	"github.com/user/orchestra/internal/message"
)

// JournalMemory wraps SessionJournal to satisfy the Memory interface.
// This allows SessionJournal to be used as a drop-in replacement for any
// existing memory strategy while adding SHA-based lookup and chain resolution.
type JournalMemory struct {
	*SessionJournal
}

// NewJournalMemory creates a new JournalMemory with the given options.
func NewJournalMemory(opts ...SessionJournalOption) *JournalMemory {
	return &JournalMemory{
		SessionJournal: NewSessionJournal(opts...),
	}
}

// Ensure JournalMemory implements the Memory interface.
var _ Memory = (*JournalMemory)(nil)

// Add stores a message in memory using the journal's Append method.
func (m *JournalMemory) Add(ctx context.Context, msg message.Message) error {
	_, err := m.SessionJournal.Append(ctx, msg)
	return err
}

// GetRelevant retrieves messages relevant to the given input.
// For JournalMemory, this returns the most recent messages.
func (m *JournalMemory) GetRelevant(ctx context.Context, input string, opts GetOptions) ([]message.Message, error) {
	// For now, return recent messages
	// In the future, this could use semantic search if combined with RAG
	if opts.Limit > 0 {
		return m.SessionJournal.Recent(ctx, opts.Limit)
	}
	return m.SessionJournal.All(), nil
}

// GetAll retrieves all messages, respecting the GetOptions constraints.
func (m *JournalMemory) GetAll(ctx context.Context, opts GetOptions) ([]message.Message, error) {
	all := m.SessionJournal.All()

	// Apply token limit if specified
	if opts.MaxTokens > 0 && opts.Tokenizer != nil {
		all = TruncateToTokenLimit(all, opts.MaxTokens, opts.Tokenizer)
	}

	// Apply message limit if specified
	if opts.Limit > 0 && len(all) > opts.Limit {
		start := len(all) - opts.Limit
		all = all[start:]
	}

	return all, nil
}

// Clear removes all messages from the journal.
func (m *JournalMemory) Clear(ctx context.Context) error {
	// Create a new empty journal
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ordered = make([]string, 0)
	m.store = make(map[string]message.Message)
	m.head = ""
	m.compacted = make(map[string]*message.CompactionInfo)

	return nil
}

// Size returns the current number of messages.
func (m *JournalMemory) Size(ctx context.Context) int {
	return m.SessionJournal.Size()
}

// ---------------------------------------------------------------------------
// Context Helpers
// ---------------------------------------------------------------------------

// journalContextKey is the context key for the journal.
type journalContextKey struct{}

// JournalFromContext extracts a SessionJournal from context.
// Returns nil if no journal is found in context.
func JournalFromContext(ctx context.Context) *SessionJournal {
	journal, ok := ctx.Value(journalContextKey{}).(*SessionJournal)
	if !ok {
		return nil
	}
	return journal
}

// ContextWithJournal adds a SessionJournal to context.
func ContextWithJournal(ctx context.Context, journal *SessionJournal) context.Context {
	return context.WithValue(ctx, journalContextKey{}, journal)
}

// JournalMemoryFromContext extracts a JournalMemory from context.
// Returns nil if no JournalMemory is found in context.
func JournalMemoryFromContext(ctx context.Context) *JournalMemory {
	mem, ok := ctx.Value(journalContextKey{}).(*JournalMemory)
	if !ok {
		return nil
	}
	return mem
}

// ContextWithJournalMemory adds a JournalMemory to context.
func ContextWithJournalMemory(ctx context.Context, mem *JournalMemory) context.Context {
	return context.WithValue(ctx, journalContextKey{}, mem)
}
