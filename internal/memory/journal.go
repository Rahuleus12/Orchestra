package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/user/orchestra/internal/message"
)

// SessionJournal is a content-addressable, append-only log of session messages.
// Every message is indexed by its SHA-256 hash, enabling stable references
// across truncation, compaction, and distributed hand-offs.
type SessionJournal struct {
	mu        sync.RWMutex
	ordered   []string                   // ordered SHA hashes (append-only)
	store     map[string]message.Message // sha → Message
	head      string                     // SHA of the latest message
	sessionID string                     // unique session identifier

	// Compaction
	compactor CompactionStrategy
	compacted map[string]*message.CompactionInfo // checkpoint SHA → info

	// Options
	logger interface{} // *slog.Logger (avoid import cycle)
}

// SessionJournalOption configures a SessionJournal.
type SessionJournalOption func(*SessionJournal)

// WithSessionID sets the session identifier.
func WithSessionID(id string) SessionJournalOption {
	return func(j *SessionJournal) {
		j.sessionID = id
	}
}

// WithCompactionStrategy sets the compaction strategy.
func WithCompactionStrategy(strategy CompactionStrategy) SessionJournalOption {
	return func(j *SessionJournal) {
		j.compactor = strategy
	}
}

// NewSessionJournal creates a new session journal.
func NewSessionJournal(opts ...SessionJournalOption) *SessionJournal {
	j := &SessionJournal{
		ordered:   make([]string, 0),
		store:     make(map[string]message.Message),
		compacted: make(map[string]*message.CompactionInfo),
		sessionID: fmt.Sprintf("session-%d", time.Now().UnixNano()),
	}

	for _, opt := range opts {
		opt(j)
	}

	return j
}

// SessionID returns the session identifier.
func (j *SessionJournal) SessionID() string {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.sessionID
}

// Append adds a message, computes its hash, and links it to the previous head.
// Returns the SHA-256 hash of the appended message.
func (j *SessionJournal) Append(ctx context.Context, msg message.Message) (string, error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	// Compute hash
	hash, err := msg.Hash()
	if err != nil {
		return "", fmt.Errorf("failed to hash message: %w", err)
	}

	// Store hash in metadata
	if msg.Metadata == nil {
		msg.Metadata = make(map[string]any)
	}
	msg.Metadata["sha"] = hash

	// Link to previous head (if any)
	if j.head != "" {
		msg.SetParentHash(j.head)
		// Recompute hash with parent hash included
		hash, err = msg.Hash()
		if err != nil {
			return "", fmt.Errorf("failed to hash message with parent: %w", err)
		}
		msg.Metadata["sha"] = hash
	}

	// Store the message
	j.store[hash] = msg
	j.ordered = append(j.ordered, hash)
	j.head = hash

	// Check if compaction should be triggered
	if j.compactor != nil && j.compactor.ShouldCompact(j) {
		j.mu.Unlock() // Release lock for compaction
		err := j.compactor.Compact(ctx, j)
		j.mu.Lock() // Reacquire lock
		if err != nil {
			// Log error but don't fail the append
			_ = err
		}
	}

	return hash, nil
}

// Get retrieves a message by its SHA hash (works even for compacted messages).
func (j *SessionJournal) Get(sha string) (message.Message, bool) {
	j.mu.RLock()
	defer j.mu.RUnlock()

	msg, ok := j.store[sha]
	return msg, ok
}

// GetByIndex retrieves a message by its position in the ordered list.
// Returns an error if the index is out of bounds.
func (j *SessionJournal) GetByIndex(index int) (message.Message, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()

	if index < 0 || index >= len(j.ordered) {
		return message.Message{}, fmt.Errorf("index %d out of bounds (0-%d)", index, len(j.ordered)-1)
	}

	sha := j.ordered[index]
	return j.store[sha], nil
}

// Head returns the SHA of the most recently appended message.
func (j *SessionJournal) Head() string {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.head
}

// OrderedHashes returns all SHA hashes in order.
func (j *SessionJournal) OrderedHashes() []string {
	j.mu.RLock()
	defer j.mu.RUnlock()

	hashes := make([]string, len(j.ordered))
	copy(hashes, j.ordered)
	return hashes
}

// ResolveChain walks backwards from a given hash, returning the full lineage.
// maxDepth limits how far back to walk (0 = unlimited).
func (j *SessionJournal) ResolveChain(fromSHA string, maxDepth int) ([]message.Message, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()

	var chain []message.Message
	currentSHA := fromSHA
	depth := 0

	for currentSHA != "" {
		if maxDepth > 0 && depth >= maxDepth {
			break
		}

		msg, ok := j.store[currentSHA]
		if !ok {
			// Check if this hash was compacted into a checkpoint
			currentSHA = j.findCompactedHash(currentSHA)
			if currentSHA == "" {
				break
			}
			continue
		}

		chain = append(chain, msg)
		currentSHA = msg.ParentHash()
		depth++
	}

	return chain, nil
}

// findCompactedHash finds the checkpoint that contains a compacted hash.
func (j *SessionJournal) findCompactedHash(sha string) string {
	for checkpointSHA, info := range j.compacted {
		for _, compactedHash := range info.CompactedHashes {
			if compactedHash == sha {
				return checkpointSHA
			}
		}
	}
	return ""
}

// Recent returns the most recent N messages (post-compaction view).
// If n is 0 or negative, returns all messages.
func (j *SessionJournal) Recent(ctx context.Context, n int) ([]message.Message, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()

	if len(j.ordered) == 0 {
		return nil, nil
	}

	// If n <= 0, return all messages
	if n <= 0 {
		return j.All(), nil
	}

	start := len(j.ordered) - n
	if start < 0 {
		start = 0
	}

	msgs := make([]message.Message, 0, n)
	for i := start; i < len(j.ordered); i++ {
		sha := j.ordered[i]
		msg, ok := j.store[sha]
		if ok {
			msgs = append(msgs, msg)
		}
	}

	return msgs, nil
}

// All returns all messages in order (including compaction checkpoints).
func (j *SessionJournal) All() []message.Message {
	j.mu.RLock()
	defer j.mu.RUnlock()

	msgs := make([]message.Message, 0, len(j.ordered))
	for _, sha := range j.ordered {
		if msg, ok := j.store[sha]; ok {
			msgs = append(msgs, msg)
		}
	}
	return msgs
}

// Compact triggers compaction of older messages according to the strategy.
func (j *SessionJournal) Compact(ctx context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.compactor == nil {
		return fmt.Errorf("no compaction strategy configured")
	}

	return j.compactor.Compact(ctx, j)
}

// Size returns the total number of messages (including checkpoints).
func (j *SessionJournal) Size() int {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return len(j.ordered)
}

// StoreSize returns the number of messages in the underlying store
// (includes both current and compacted messages).
func (j *SessionJournal) StoreSize() int {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return len(j.store)
}

// CompactionCount returns the number of compaction checkpoints.
func (j *SessionJournal) CompactionCount() int {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return len(j.compacted)
}

// SetCompacted marks a checkpoint as containing compacted messages.
// This is called by compaction strategies.
func (j *SessionJournal) SetCompacted(checkpointSHA string, info *message.CompactionInfo) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.compacted[checkpointSHA] = info
}

// GetCompactionInfo returns compaction info for a checkpoint.
func (j *SessionJournal) GetCompactionInfo(checkpointSHA string) (*message.CompactionInfo, bool) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	info, ok := j.compacted[checkpointSHA]
	return info, ok
}

// ForCompaction returns messages available for compaction (non-checkpoint messages).
// This is used by compaction strategies.
func (j *SessionJournal) ForCompaction() []message.Message {
	j.mu.RLock()
	defer j.mu.RUnlock()

	var msgs []message.Message
	for _, sha := range j.ordered {
		msg := j.store[sha]
		if !msg.IsCompactionCheckpoint() {
			msgs = append(msgs, msg)
		}
	}
	return msgs
}

// ReplaceForCompaction replaces messages with a compaction checkpoint.
// This is called by compaction strategies.
func (j *SessionJournal) ReplaceForCompaction(ctx context.Context, compactedHashes []string, checkpoint message.Message) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	// Verify all hashes exist
	for _, sha := range compactedHashes {
		if _, ok := j.store[sha]; !ok {
			return fmt.Errorf("compact failed: message %s not found", sha)
		}
	}

	// Compute checkpoint hash
	checkpointHash, err := checkpoint.Hash()
	if err != nil {
		return fmt.Errorf("failed to hash checkpoint: %w", err)
	}
	if checkpoint.Metadata == nil {
		checkpoint.Metadata = make(map[string]any)
	}
	checkpoint.Metadata["sha"] = checkpointHash

	// Link to parent of first compacted message
	if len(compactedHashes) > 0 {
		firstMsg := j.store[compactedHashes[0]]
		parentHash := firstMsg.ParentHash()
		if parentHash != "" {
			checkpoint.SetParentHash(parentHash)
			// Recompute hash
			checkpointHash, _ = checkpoint.Hash()
			checkpoint.Metadata["sha"] = checkpointHash
		}
	}

	// Remove compacted messages from ordered list
	compactSet := make(map[string]struct{}, len(compactedHashes))
	for _, sha := range compactedHashes {
		compactSet[sha] = struct{}{}
	}

	newOrdered := make([]string, 0, len(j.ordered)-len(compactedHashes)+1)
	inserted := false

	for _, sha := range j.ordered {
		if _, isCompacted := compactSet[sha]; isCompacted {
			// Find the position of the first compacted message
			if !inserted {
				// Insert checkpoint here
				newOrdered = append(newOrdered, checkpointHash)
				inserted = true
			}
			// Don't include the compacted message
			continue
		}
		newOrdered = append(newOrdered, sha)
	}

	// If no compaction happened (edge case), just append
	if !inserted {
		newOrdered = append(newOrdered, checkpointHash)
	}

	j.ordered = newOrdered
	j.store[checkpointHash] = checkpoint

	// If the last compacted message was the head, update head
	if len(compactedHashes) > 0 {
		lastCompacted := compactedHashes[len(compactedHashes)-1]
		if j.head == lastCompacted {
			j.head = checkpointHash
		}
	}

	return nil
}
