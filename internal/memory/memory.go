// Package memory provides flexible memory management for agents,
// supporting various strategies for storing and retrieving conversation context.
package memory

import (
	"context"

	"github.com/user/orchestra/internal/message"
)

// Memory defines the interface for different memory strategies.
// Implementations can use various approaches like simple buffers,
// sliding windows, summarization, semantic search, or composite strategies.
type Memory interface {
	// Add stores a message in memory.
	Add(ctx context.Context, msg message.Message) error

	// GetRelevant retrieves messages relevant to the given input.
	// The relevance algorithm is implementation-dependent (e.g., recency,
	// semantic similarity, custom scoring).
	GetRelevant(ctx context.Context, input string, opts GetOptions) ([]message.Message, error)

	// GetAll retrieves all messages (up to limit and respecting token constraints).
	GetAll(ctx context.Context, opts GetOptions) ([]message.Message, error)

	// Clear removes all messages.
	Clear(ctx context.Context) error

	// Size returns the current number of messages.
	Size(ctx context.Context) int
}

// GetOptions specifies constraints for retrieving messages from memory.
type GetOptions struct {
	// Limit is the maximum number of messages to return.
	// If 0, no limit is applied.
	Limit int

	// MaxTokens is the maximum number of tokens to return.
	// If 0, no token limit is applied. If a Tokenizer is not set,
	// this option is ignored.
	MaxTokens int

	// Tokenizer is used to count tokens for the MaxTokens constraint.
	// If nil, token counting is skipped.
	Tokenizer Tokenizer
}

// Tokenizer defines the interface for counting tokens in text.
// Different models use different tokenization algorithms.
type Tokenizer interface {
	// CountTokens returns the number of tokens in the given text.
	CountTokens(text string) int

	// CountTokensInMessage returns the number of tokens in a message.
	// This typically includes the role, content, and any special formatting.
	CountTokensInMessage(msg message.Message) int
}

// DefaultGetOptions returns a GetOptions with no limits.
func DefaultGetOptions() GetOptions {
	return GetOptions{
		Limit:     0,
		MaxTokens: 0,
		Tokenizer: nil,
	}
}

// WithLimit returns a new GetOptions with the specified limit.
func WithLimit(limit int) GetOptions {
	return GetOptions{
		Limit: limit,
	}
}

// WithMaxTokens returns a new GetOptions with the specified token limit.
func WithMaxTokens(maxTokens int, tokenizer Tokenizer) GetOptions {
	return GetOptions{
		MaxTokens: maxTokens,
		Tokenizer: tokenizer,
	}
}
