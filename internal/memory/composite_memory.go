package memory

import (
	"context"
	"sort"
	"sync"

	"github.com/user/orchestra/internal/message"
)

// MemoryWithPriority holds a memory implementation with its priority.
// Higher priority memories are consulted first and their results are given more weight.
type MemoryWithPriority struct {
	Memory   Memory
	Priority int
	Name     string // Optional name for identification
}

// CompositeMemory combines multiple memory strategies into a single unified memory interface.
// This allows, for example, combining a sliding window for recent messages with
// semantic search for relevant historical messages.
type CompositeMemory struct {
	mu       sync.RWMutex
	memories []MemoryWithPriority
	dedup    bool // Whether to deduplicate messages by content
}

// NewCompositeMemory creates a new CompositeMemory with the specified memories.
// Memories are consulted in order of priority (highest first).
//
// Example:
//
//	composite := NewCompositeMemory(
//	    MemoryWithPriority{Memory: NewSlidingWindowMemory(10), Priority: 2, Name: "recent"},
//	    MemoryWithPriority{Memory: NewSemanticMemory(embedProvider, 100, 5, 0.7), Priority: 1, Name: "semantic"},
//	)
func NewCompositeMemory(memories ...MemoryWithPriority) *CompositeMemory {
	// Sort memories by priority (highest first)
	sortedMemories := make([]MemoryWithPriority, len(memories))
	copy(sortedMemories, memories)
	sort.Slice(sortedMemories, func(i, j int) bool {
		return sortedMemories[i].Priority > sortedMemories[j].Priority
	})

	return &CompositeMemory{
		memories: sortedMemories,
		dedup:    true,
	}
}

// Add stores a message in all underlying memories.
// If any memory fails, the error from the first failure is returned,
// but the message is still added to other memories.
func (m *CompositeMemory) Add(ctx context.Context, msg message.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for _, mem := range m.memories {
		if err := mem.Memory.Add(ctx, msg); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// GetRelevant retrieves messages relevant to the given input from all memories.
// Results are combined, optionally deduplicated, and returned respecting the GetOptions.
// Messages from higher-priority memories appear earlier in the result.
func (m *CompositeMemory) GetRelevant(ctx context.Context, input string, opts GetOptions) ([]message.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Collect messages from all memories
	allMsgs := make([]messageWithSource, 0)
	for _, mem := range m.memories {
		msgs, err := mem.Memory.GetRelevant(ctx, input, opts)
		if err != nil {
			// Log the error but continue with other memories
			continue
		}

		for _, msg := range msgs {
			allMsgs = append(allMsgs, messageWithSource{
				Message:  msg,
				Priority: mem.Priority,
				Source:   mem.Name,
			})
		}
	}

	// Deduplicate if enabled
	if m.dedup {
		allMsgs = m.deduplicateMessages(allMsgs)
	}

	// Sort by priority (highest first), then by order within each memory
	sort.Slice(allMsgs, func(i, j int) bool {
		if allMsgs[i].Priority != allMsgs[j].Priority {
			return allMsgs[i].Priority > allMsgs[j].Priority
		}
		return allMsgs[i].Index < allMsgs[j].Index
	})

	// Extract messages
	result := make([]message.Message, len(allMsgs))
	for i, mws := range allMsgs {
		result[i] = mws.Message
	}

	// Apply token limit if specified
	if opts.MaxTokens > 0 && opts.Tokenizer != nil {
		result = TruncateToTokenLimit(result, opts.MaxTokens, opts.Tokenizer)
	}

	// Apply message limit if specified
	if opts.Limit > 0 && len(result) > opts.Limit {
		result = result[:opts.Limit]
	}

	return result, nil
}

// GetAll retrieves all messages from all memories, respecting the options.
// Results are combined from highest-priority memory first, then from lower-priority memories.
func (m *CompositeMemory) GetAll(ctx context.Context, opts GetOptions) ([]message.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Collect messages from all memories
	allMsgs := make([]messageWithSource, 0)
	for _, mem := range m.memories {
		msgs, err := mem.Memory.GetAll(ctx, opts)
		if err != nil {
			continue
		}

		for _, msg := range msgs {
			allMsgs = append(allMsgs, messageWithSource{
				Message:  msg,
				Priority: mem.Priority,
				Source:   mem.Name,
			})
		}
	}

	// Deduplicate if enabled
	if m.dedup {
		allMsgs = m.deduplicateMessages(allMsgs)
	}

	// Sort by priority
	sort.Slice(allMsgs, func(i, j int) bool {
		if allMsgs[i].Priority != allMsgs[j].Priority {
			return allMsgs[i].Priority > allMsgs[j].Priority
		}
		return allMsgs[i].Index < allMsgs[j].Index
	})

	// Extract messages
	result := make([]message.Message, len(allMsgs))
	for i, mws := range allMsgs {
		result[i] = mws.Message
	}

	// Apply token limit if specified
	if opts.MaxTokens > 0 && opts.Tokenizer != nil {
		result = TruncateToTokenLimit(result, opts.MaxTokens, opts.Tokenizer)
	}

	// Apply message limit if specified
	if opts.Limit > 0 && len(result) > opts.Limit {
		result = result[:opts.Limit]
	}

	return result, nil
}

// Clear removes all messages from all underlying memories.
// If any memory fails, the error from the first failure is returned.
func (m *CompositeMemory) Clear(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for _, mem := range m.memories {
		if err := mem.Memory.Clear(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Size returns the total number of unique messages across all memories.
func (m *CompositeMemory) Size(ctx context.Context) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.dedup {
		// Get all messages to deduplicate
		allMsgs := make([]messageWithSource, 0)
		for _, mem := range m.memories {
			msgs, err := mem.Memory.GetAll(ctx, DefaultGetOptions())
			if err != nil {
				continue
			}
			for _, msg := range msgs {
				allMsgs = append(allMsgs, messageWithSource{
					Message:  msg,
					Priority: mem.Priority,
					Source:   mem.Name,
				})
			}
		}
		return len(m.deduplicateMessages(allMsgs))
	}

	// Without dedup, sum all sizes
	total := 0
	for _, mem := range m.memories {
		total += mem.Memory.Size(ctx)
	}
	return total
}

// AddMemory adds a new memory to the composite.
// If a memory with the same name already exists, it will be replaced.
func (m *CompositeMemory) AddMemory(memory Memory, priority int, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove existing memory with the same name
	newMemories := make([]MemoryWithPriority, 0, len(m.memories))
	for _, mem := range m.memories {
		if mem.Name != name {
			newMemories = append(newMemories, mem)
		}
	}
	newMemories = append(newMemories, MemoryWithPriority{
		Memory:   memory,
		Priority: priority,
		Name:     name,
	})

	// Sort by priority
	sort.Slice(newMemories, func(i, j int) bool {
		return newMemories[i].Priority > newMemories[j].Priority
	})

	m.memories = newMemories
}

// RemoveMemory removes a memory by name.
// Returns true if the memory was found and removed.
func (m *CompositeMemory) RemoveMemory(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, mem := range m.memories {
		if mem.Name == name {
			m.memories = append(m.memories[:i], m.memories[i+1:]...)
			return true
		}
	}
	return false
}

// GetMemories returns all underlying memories.
func (m *CompositeMemory) GetMemories() []MemoryWithPriority {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]MemoryWithPriority, len(m.memories))
	copy(result, m.memories)
	return result
}

// SetDedup sets whether to deduplicate messages across memories.
func (m *CompositeMemory) SetDedup(dedup bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.dedup = dedup
}

// GetDedup returns whether deduplication is enabled.
func (m *CompositeMemory) GetDedup() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.dedup
}

// messageWithSource tracks a message along with its source memory and priority.
type messageWithSource struct {
	Message  message.Message
	Priority int
	Source   string
	Index    int // Position in the source memory
}

// messageKey generates a unique key for a message for deduplication.
// The key is based on role and text content.
func messageKey(msg message.Message) string {
	return string(msg.Role) + "|||" + msg.Text()
}

// deduplicateMessages removes duplicate messages based on content.
// Keeps the message from the highest-priority source.
func (m *CompositeMemory) deduplicateMessages(msgs []messageWithSource) []messageWithSource {
	seen := make(map[string]int) // message key -> index in msgs
	result := make([]messageWithSource, 0)

	for i, mws := range msgs {
		mws.Index = i
		key := messageKey(mws.Message)

		if idx, exists := seen[key]; !exists {
			seen[key] = len(result)
			result = append(result, mws)
		} else {
			// Keep the message from higher-priority source
			if mws.Priority > result[idx].Priority {
				result[idx] = mws
			}
		}
	}

	return result
}
