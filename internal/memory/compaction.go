package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/user/orchestra/internal/message"
	"github.com/user/orchestra/internal/provider"
)

// CompactionStrategy decides when and how to compact messages in a SessionJournal.
type CompactionStrategy interface {
	// ShouldCompact returns true if compaction should be triggered.
	ShouldCompact(journal *SessionJournal) bool

	// Compact performs the compaction, replacing older messages with a summary.
	Compact(ctx context.Context, journal *SessionJournal) error
}

// ---------------------------------------------------------------------------
// Threshold Compaction
// ---------------------------------------------------------------------------

// ThresholdCompaction compacts every N messages (e.g., every 10 prompts).
type ThresholdCompaction struct {
	// EveryN is the number of messages to accumulate before compacting.
	// Compaction triggers when the journal size exceeds this threshold.
	EveryN int

	// Provider is the LLM used to generate summaries.
	Provider provider.Provider

	// SummaryModel is the model to use for summarization.
	SummaryModel string

	// KeepRecent is the number of recent messages to keep un-compacted.
	// Default is 2 (to maintain conversation continuity).
	KeepRecent int
}

// NewThresholdCompaction creates a new threshold-based compaction strategy.
func NewThresholdCompaction(everyN int, prov provider.Provider, summaryModel string) *ThresholdCompaction {
	if everyN <= 0 {
		everyN = 10
	}
	return &ThresholdCompaction{
		EveryN:       everyN,
		Provider:     prov,
		SummaryModel: summaryModel,
		KeepRecent:   2,
	}
}

// ShouldCompact returns true if the journal has more messages than EveryN.
func (t *ThresholdCompaction) ShouldCompact(journal *SessionJournal) bool {
	// Only count non-checkpoint messages
	msgs := journal.ForCompaction()
	return len(msgs) >= t.EveryN
}

// Compact summarizes older messages and replaces them with a checkpoint.
func (t *ThresholdCompaction) Compact(ctx context.Context, journal *SessionJournal) error {
	msgs := journal.ForCompaction()
	if len(msgs) < t.EveryN {
		return nil
	}

	// Determine how many messages to compact
	// Keep the most recent messages un-compacted
	toCompact := len(msgs) - t.KeepRecent
	if toCompact <= 0 {
		return nil
	}

	// Get the messages to compact
	msgsToCompact := msgs[:toCompact]

	// Generate summary
	summary, err := t.generateSummary(ctx, msgsToCompact)
	if err != nil {
		return fmt.Errorf("failed to generate summary: %w", err)
	}

	// Create checkpoint message
	checkpoint := t.createCheckpoint(msgsToCompact, summary)

	// Get hashes of compacted messages
	hashes := make([]string, len(msgsToCompact))
	for i, msg := range msgsToCompact {
		hash, _ := msg.GetHash()
		hashes[i] = hash
	}

	// Replace messages with checkpoint
	return journal.ReplaceForCompaction(ctx, hashes, checkpoint)
}

// generateSummary uses the LLM to create a summary of the messages.
func (t *ThresholdCompaction) generateSummary(ctx context.Context, msgs []message.Message) (string, error) {
	// Format messages for summarization
	var sb strings.Builder
	for _, msg := range msgs {
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", msg.Role, msg.Text()))
	}

	prompt := fmt.Sprintf(`Summarize the following conversation history concisely.
Focus on key information, decisions, and context that would be useful for continuing the conversation.
Keep the summary under 500 words.

Conversation to summarize:
---
%s
---

Summary:`, sb.String())

	temperature := 0.3
	maxTokens := 500
	req := provider.GenerateRequest{
		Model: t.SummaryModel,
		Messages: []message.Message{
			message.SystemMessage("You are a helpful assistant that creates concise summaries."),
			message.UserMessage(fmt.Sprintf(prompt, sb.String())),
		},
		Options: provider.GenerateOptions{
			Temperature: &temperature,
			MaxTokens:   &maxTokens,
		},
	}

	result, err := t.Provider.Generate(ctx, req)
	if err != nil {
		return "", err
	}

	return result.Message.Text(), nil
}

// createCheckpoint creates a compaction checkpoint message.
func (t *ThresholdCompaction) createCheckpoint(msgs []message.Message, summary string) message.Message {
	// Get hashes of compacted messages
	hashes := make([]string, len(msgs))
	for i, msg := range msgs {
		hash, _ := msg.GetHash()
		hashes[i] = hash
	}

	// Create compaction info
	info := &message.CompactionInfo{
		CompactedHashes: hashes,
		CompactedAt:     time.Now().Unix(),
		MessageCount:    len(msgs),
	}

	// Create checkpoint message
	checkpoint := message.Message{
		Role: message.RoleSystem,
		Content: []message.ContentBlock{
			message.TextContentBlock(fmt.Sprintf("[Conversation Summary - %d messages compacted at %s]\n\n%s",
				len(msgs),
				time.Unix(info.CompactedAt, 0).Format(time.RFC3339),
				summary)),
		},
		Metadata: make(map[string]any),
	}

	// Set compaction info
	checkpoint.SetCompactionInfo(info)

	return checkpoint
}

// ---------------------------------------------------------------------------
// Token Budget Compaction
// ---------------------------------------------------------------------------

// TokenBudgetCompaction compacts when the total token count exceeds a budget.
type TokenBudgetCompaction struct {
	// MaxTokens is the maximum total tokens before compaction is triggered.
	MaxTokens int

	// KeepRecent is the number of recent messages to always keep un-compacted.
	KeepRecent int

	// Provider is the LLM used to generate summaries.
	Provider provider.Provider

	// SummaryModel is the model to use for summarization.
	SummaryModel string

	// Tokenizer is used to count tokens.
	Tokenizer Tokenizer
}

// NewTokenBudgetCompaction creates a new token-budget-based compaction strategy.
func NewTokenBudgetCompaction(maxTokens, keepRecent int, prov provider.Provider, summaryModel string, tokenizer Tokenizer) *TokenBudgetCompaction {
	if maxTokens <= 0 {
		maxTokens = 4000
	}
	if keepRecent <= 0 {
		keepRecent = 2
	}
	return &TokenBudgetCompaction{
		MaxTokens:    maxTokens,
		KeepRecent:   keepRecent,
		Provider:     prov,
		SummaryModel: summaryModel,
		Tokenizer:    tokenizer,
	}
}

// ShouldCompact returns true if the total token count exceeds the budget.
func (t *TokenBudgetCompaction) ShouldCompact(journal *SessionJournal) bool {
	if t.Tokenizer == nil {
		return false
	}

	msgs := journal.ForCompaction()
	totalTokens := 0
	for _, msg := range msgs {
		totalTokens += t.Tokenizer.CountTokensInMessage(msg)
	}
	return totalTokens > t.MaxTokens
}

// Compact summarizes older messages until the token budget is met.
func (t *TokenBudgetCompaction) Compact(ctx context.Context, journal *SessionJournal) error {
	msgs := journal.ForCompaction()
	if len(msgs) <= t.KeepRecent {
		return nil
	}

	// Calculate current tokens
	totalTokens := 0
	for _, msg := range msgs {
		totalTokens += t.Tokenizer.CountTokensInMessage(msg)
	}

	// Determine how many messages to compact
	// Keep compacting until we're under budget or only keepRecent remain
	toCompact := len(msgs) - t.KeepRecent
	compactTokens := 0

	for i := 0; i < toCompact; i++ {
		compactTokens += t.Tokenizer.CountTokensInMessage(msgs[i])
		if totalTokens-compactTokens <= t.MaxTokens {
			// We've compacted enough
			toCompact = i + 1
			break
		}
	}

	if toCompact <= 0 {
		return nil
	}

	// Get the messages to compact
	msgsToCompact := msgs[:toCompact]

	// Generate summary
	summary, err := t.generateSummary(ctx, msgsToCompact)
	if err != nil {
		return fmt.Errorf("failed to generate summary: %w", err)
	}

	// Create checkpoint message
	checkpoint := t.createCheckpoint(msgsToCompact, summary)

	// Get hashes of compacted messages
	hashes := make([]string, len(msgsToCompact))
	for i, msg := range msgsToCompact {
		hash, _ := msg.GetHash()
		hashes[i] = hash
	}

	// Replace messages with checkpoint
	return journal.ReplaceForCompaction(ctx, hashes, checkpoint)
}

// generateSummary uses the LLM to create a summary of the messages.
func (t *TokenBudgetCompaction) generateSummary(ctx context.Context, msgs []message.Message) (string, error) {
	// Similar to ThresholdCompaction but might be more aggressive
	var sb strings.Builder
	for _, msg := range msgs {
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", msg.Role, msg.Text()))
	}

	prompt := fmt.Sprintf(`Summarize the following conversation history concisely to reduce token usage.
Focus on essential context and key decisions. Be brief.

Conversation to summarize:
---
%s
---

Summary:`, sb.String())

	temperature := 0.2
	maxTokens := 300
	req := provider.GenerateRequest{
		Model: t.SummaryModel,
		Messages: []message.Message{
			message.SystemMessage("Create very concise summaries to reduce token count."),
			message.UserMessage(fmt.Sprintf(prompt, sb.String())),
		},
		Options: provider.GenerateOptions{
			Temperature: &temperature,
			MaxTokens:   &maxTokens,
		},
	}

	result, err := t.Provider.Generate(ctx, req)
	if err != nil {
		return "", err
	}

	return result.Message.Text(), nil
}

// createCheckpoint creates a compaction checkpoint message.
func (t *TokenBudgetCompaction) createCheckpoint(msgs []message.Message, summary string) message.Message {
	// Get hashes of compacted messages
	hashes := make([]string, len(msgs))
	for i, msg := range msgs {
		hash, _ := msg.GetHash()
		hashes[i] = hash
	}

	// Create compaction info
	info := &message.CompactionInfo{
		CompactedHashes: hashes,
		CompactedAt:     time.Now().Unix(),
		MessageCount:    len(msgs),
	}

	// Create checkpoint message
	checkpoint := message.Message{
		Role: message.RoleSystem,
		Content: []message.ContentBlock{
			message.TextContentBlock(fmt.Sprintf("[Summary - %d msgs, %s]\n%s",
				len(msgs),
				time.Unix(info.CompactedAt, 0).Format(time.RFC3339),
				summary)),
		},
		Metadata: make(map[string]any),
	}

	// Set compaction info
	checkpoint.SetCompactionInfo(info)

	return checkpoint
}

// ---------------------------------------------------------------------------
// Manual Compaction (for testing)
// ---------------------------------------------------------------------------

// ManualCompaction is a compaction strategy that only compacts when explicitly triggered.
type ManualCompaction struct {
	// Provider is the LLM used to generate summaries.
	Provider provider.Provider

	// SummaryModel is the model to use for summarization.
	SummaryModel string
}

// NewManualCompaction creates a new manual compaction strategy.
func NewManualCompaction(prov provider.Provider, summaryModel string) *ManualCompaction {
	return &ManualCompaction{
		Provider:     prov,
		SummaryModel: summaryModel,
	}
}

// ShouldCompact always returns false for manual compaction.
func (m *ManualCompaction) ShouldCompact(journal *SessionJournal) bool {
	return false
}

// Compact compacts all non-checkpoint messages except the last 2.
func (m *ManualCompaction) Compact(ctx context.Context, journal *SessionJournal) error {
	msgs := journal.ForCompaction()
	if len(msgs) <= 2 {
		return nil
	}

	// Compact all but the last 2 messages
	msgsToCompact := msgs[:len(msgs)-2]

	// Generate simple summary (concatenation since we don't have LLM in tests)
	var sb strings.Builder
	for _, msg := range msgsToCompact {
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", msg.Role, msg.Text()))
	}
	summary := sb.String()

	// Create checkpoint
	hashes := make([]string, len(msgsToCompact))
	for i, msg := range msgsToCompact {
		hash, _ := msg.GetHash()
		hashes[i] = hash
	}

	info := &message.CompactionInfo{
		CompactedHashes: hashes,
		CompactedAt:     time.Now().Unix(),
		MessageCount:    len(msgsToCompact),
	}

	checkpoint := message.Message{
		Role: message.RoleSystem,
		Content: []message.ContentBlock{
			message.TextContentBlock(fmt.Sprintf("[Summary - %d messages]\n%s",
				len(msgsToCompact), summary)),
		},
		Metadata: make(map[string]any),
	}
	checkpoint.SetCompactionInfo(info)

	return journal.ReplaceForCompaction(ctx, hashes, checkpoint)
}
