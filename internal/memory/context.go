package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/user/orchestra/internal/message"
)

// ContextWindow represents the context window constraints for a model.
type ContextWindow struct {
	// MaxTokens is the maximum number of tokens the model can process.
	MaxTokens int

	// ModelName is the name of the model (for logging/reference).
	ModelName string

	// SafeMargin is the percentage of tokens to keep as a safety margin.
	// For example, 0.1 means keep 10% as a safety buffer.
	// Defaults to 0.05 (5%).
	SafeMargin float64

	// SystemPromptTokens is the estimated number of tokens reserved for system prompts.
	SystemPromptTokens int
}

// DefaultContextWindow returns a reasonable default context window.
func DefaultContextWindow() *ContextWindow {
	return &ContextWindow{
		MaxTokens:          4096,
		ModelName:          "default",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	}
}

// GetAvailableTokens returns the number of tokens available for messages,
// accounting for the safety margin and system prompt reservation.
func (cw *ContextWindow) GetAvailableTokens() int {
	safeMargin := int(float64(cw.MaxTokens) * cw.SafeMargin)
	available := cw.MaxTokens - safeMargin - cw.SystemPromptTokens
	if available < 0 {
		return 0
	}
	return available
}

// GetEffectiveLimit returns the effective token limit considering the safety margin.
func (cw *ContextWindow) GetEffectiveLimit() int {
	safeMargin := int(float64(cw.MaxTokens) * cw.SafeMargin)
	limit := cw.MaxTokens - safeMargin
	if limit < 0 {
		return 0
	}
	return limit
}

// TruncationStrategy defines how messages should be truncated.
type TruncationStrategy string

const (
	// TruncateOldest removes the oldest messages first.
	TruncateOldest TruncationStrategy = "oldest"

	// TruncateOldestPreservingSystem removes oldest messages but keeps system messages.
	TruncateOldestPreservingSystem TruncationStrategy = "oldest_preserve_system"

	// TruncateMiddle removes messages from the middle, keeping beginning and end.
	TruncateMiddle TruncationStrategy = "middle"

	// TruncateSmallest removes messages with the smallest token count first.
	TruncateSmallest TruncationStrategy = "smallest"

	// TruncateRecency keeps the most recent messages only.
	TruncateRecency TruncationStrategy = "recency"
)

// ContextManager handles context window management for conversations.
type ContextManager struct {
	mu               sync.RWMutex
	contextWindow    *ContextWindow
	tokenizer        Tokenizer
	strategy         TruncationStrategy
	warningThreshold float64 // Percentage (0.0-1.0) to emit warnings
	onWarning        func(warning *ContextWarning)
}

// ContextWarning represents a warning about context window usage.
type ContextWarning struct {
	// Level is the severity level (warn, critical).
	Level string

	// Usage is the current token usage as a percentage (0.0-1.0).
	Usage float64

	// CurrentTokens is the current number of tokens.
	CurrentTokens int

	// MaxTokens is the maximum allowed tokens.
	MaxTokens int

	// Message is a human-readable warning message.
	Message string
}

// NewContextManager creates a new ContextManager with the specified configuration.
func NewContextManager(contextWindow *ContextWindow, tokenizer Tokenizer) *ContextManager {
	if contextWindow == nil {
		contextWindow = DefaultContextWindow()
	}

	return &ContextManager{
		contextWindow:    contextWindow,
		tokenizer:        tokenizer,
		strategy:         TruncateOldestPreservingSystem,
		warningThreshold: 0.9, // Warn at 90% usage
		onWarning:        nil,
	}
}

// SetTruncationStrategy sets the strategy for truncating messages.
func (cm *ContextManager) SetTruncationStrategy(strategy TruncationStrategy) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.strategy = strategy
}

// GetTruncationStrategy returns the current truncation strategy.
func (cm *ContextManager) GetTruncationStrategy() TruncationStrategy {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.strategy
}

// SetWarningThreshold sets the usage percentage (0.0-1.0) at which to emit warnings.
func (cm *ContextManager) SetWarningThreshold(threshold float64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if threshold < 0 {
		threshold = 0
	}
	if threshold > 1 {
		threshold = 1
	}
	cm.warningThreshold = threshold
}

// SetWarningCallback sets a callback function to handle context warnings.
func (cm *ContextManager) SetWarningCallback(callback func(warning *ContextWarning)) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.onWarning = callback
}

// TruncateMessages truncates a slice of messages to fit within the context window
// using the configured truncation strategy.
func (cm *ContextManager) TruncateMessages(ctx context.Context, msgs []message.Message) []message.Message {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.truncateMessagesInternal(ctx, msgs)
}

// truncateMessagesInternal performs the actual truncation without acquiring locks.
// This is used by both TruncateMessages and FitMessages to avoid deadlock.
func (cm *ContextManager) truncateMessagesInternal(ctx context.Context, msgs []message.Message) []message.Message {
	if cm.tokenizer == nil || len(msgs) == 0 {
		return msgs
	}

	// Check current token usage
	currentTokens := CountTokensInMessages(msgs, cm.tokenizer)
	effectiveLimit := cm.contextWindow.GetEffectiveLimit()

	// Emit warning if approaching limit
	cm.checkAndEmitWarning(currentTokens, effectiveLimit)

	// If we're within limits, no truncation needed
	if currentTokens <= effectiveLimit {
		return msgs
	}

	// Apply truncation strategy
	switch cm.strategy {
	case TruncateOldest:
		return cm.truncateOldest(msgs, effectiveLimit)
	case TruncateOldestPreservingSystem:
		return cm.truncateOldestPreservingSystem(msgs, effectiveLimit)
	case TruncateMiddle:
		return cm.truncateMiddle(msgs, effectiveLimit)
	case TruncateSmallest:
		return cm.truncateSmallest(msgs, effectiveLimit)
	case TruncateRecency:
		return cm.truncateRecency(msgs, effectiveLimit)
	default:
		return cm.truncateOldestPreservingSystem(msgs, effectiveLimit)
	}
}

// checkAndEmitWarning checks if we should emit a warning based on current usage.
func (cm *ContextManager) checkAndEmitWarning(currentTokens, maxTokens int) {
	if cm.onWarning == nil {
		return
	}

	if maxTokens == 0 {
		return
	}

	usage := float64(currentTokens) / float64(maxTokens)

	var warning *ContextWarning
	switch {
	case usage >= 1.0:
		warning = &ContextWarning{
			Level:         "critical",
			Usage:         usage,
			CurrentTokens: currentTokens,
			MaxTokens:     maxTokens,
			Message:       fmt.Sprintf("Context window exceeded: %d/%d tokens (%.1f%%) for model %s", currentTokens, maxTokens, usage*100, cm.contextWindow.ModelName),
		}
	case usage >= cm.warningThreshold:
		warning = &ContextWarning{
			Level:         "warn",
			Usage:         usage,
			CurrentTokens: currentTokens,
			MaxTokens:     maxTokens,
			Message:       fmt.Sprintf("Approaching context window limit: %d/%d tokens (%.1f%%) for model %s", currentTokens, maxTokens, usage*100, cm.contextWindow.ModelName),
		}
	}

	if warning != nil {
		cm.onWarning(warning)
	}
}

// truncateOldest removes the oldest messages until within the token limit.
func (cm *ContextManager) truncateOldest(msgs []message.Message, maxTokens int) []message.Message {
	return TruncateToTokenLimit(msgs, maxTokens, cm.tokenizer)
}

// truncateOldestPreservingSystem removes oldest messages but keeps all system messages.
func (cm *ContextManager) truncateOldestPreservingSystem(msgs []message.Message, maxTokens int) []message.Message {
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
	systemTokens := CountTokensInMessages(systemMsgs, cm.tokenizer)

	// If system messages exceed the limit, truncate them too
	if systemTokens >= maxTokens {
		return TruncateToTokenLimit(systemMsgs, maxTokens, cm.tokenizer)
	}

	// Add system messages and fit as many other messages as possible
	result := make([]message.Message, len(systemMsgs))
	copy(result, systemMsgs)
	remaining := maxTokens - systemTokens

	// Add messages from the end (most recent first)
	for i := len(otherMsgs) - 1; i >= 0; i-- {
		tokens := cm.tokenizer.CountTokensInMessage(otherMsgs[i])
		if tokens <= remaining {
			result = append([]message.Message{otherMsgs[i]}, result...)
			remaining -= tokens
		}
	}

	return result
}

// truncateMiddle removes messages from the middle, keeping beginning and end.
func (cm *ContextManager) truncateMiddle(msgs []message.Message, maxTokens int) []message.Message {
	if len(msgs) == 0 {
		return msgs
	}

	// Keep system messages and first/last few non-system messages
	var systemMsgs []message.Message
	var otherMsgs []message.Message

	for _, msg := range msgs {
		if msg.Role == message.RoleSystem {
			systemMsgs = append(systemMsgs, msg)
		} else {
			otherMsgs = append(otherMsgs, msg)
		}
	}

	// Start with system messages
	result := make([]message.Message, len(systemMsgs))
	copy(result, systemMsgs)

	currentTokens := CountTokensInMessages(result, cm.tokenizer)

	// Add messages from both ends until we reach the limit
	left := 0
	right := len(otherMsgs) - 1

	for left <= right {
		// Try to add from the left
		if left <= right {
			leftTokens := cm.tokenizer.CountTokensInMessage(otherMsgs[left])
			if currentTokens+leftTokens <= maxTokens {
				result = append(result, otherMsgs[left])
				currentTokens += leftTokens
				left++
			}
		}

		// Try to add from the right
		if left <= right {
			rightTokens := cm.tokenizer.CountTokensInMessage(otherMsgs[right])
			if currentTokens+rightTokens <= maxTokens {
				result = append(result, otherMsgs[right])
				currentTokens += rightTokens
				right--
			}
		}

		// If we can't add from either side, stop
		if currentTokens+cm.tokenizer.CountTokensInMessage(otherMsgs[left]) > maxTokens &&
			currentTokens+cm.tokenizer.CountTokensInMessage(otherMsgs[right]) > maxTokens {
			break
		}
	}

	return result
}

// truncateSmallest removes messages with the smallest token count first.
func (cm *ContextManager) truncateSmallest(msgs []message.Message, maxTokens int) []message.Message {
	if len(msgs) == 0 {
		return msgs
	}

	// Separate system messages (always keep)
	var systemMsgs []message.Message
	var otherMsgs []message.Message

	for _, msg := range msgs {
		if msg.Role == message.RoleSystem {
			systemMsgs = append(systemMsgs, msg)
		} else {
			otherMsgs = append(otherMsgs, msg)
		}
	}

	// Start with system messages
	result := make([]message.Message, len(systemMsgs))
	copy(result, systemMsgs)

	currentTokens := CountTokensInMessages(result, cm.tokenizer)

	// Sort other messages by token count (ascending)
	sortedMsgs := make([]message.Message, len(otherMsgs))
	copy(sortedMsgs, otherMsgs)

	// Simple bubble sort by token count
	for i := 0; i < len(sortedMsgs)-1; i++ {
		for j := i + 1; j < len(sortedMsgs); j++ {
			if cm.tokenizer.CountTokensInMessage(sortedMsgs[i]) > cm.tokenizer.CountTokensInMessage(sortedMsgs[j]) {
				sortedMsgs[i], sortedMsgs[j] = sortedMsgs[j], sortedMsgs[i]
			}
		}
	}

	// Add smallest messages first
	for _, msg := range sortedMsgs {
		tokens := cm.tokenizer.CountTokensInMessage(msg)
		if currentTokens+tokens <= maxTokens {
			result = append(result, msg)
			currentTokens += tokens
		}
	}

	return result
}

// truncateRecency keeps only the most recent messages, removing all older ones.
func (cm *ContextManager) truncateRecency(msgs []message.Message, maxTokens int) []message.Message {
	if len(msgs) == 0 {
		return msgs
	}

	// Start from the end and work backwards
	result := make([]message.Message, 0)
	currentTokens := 0

	for i := len(msgs) - 1; i >= 0; i-- {
		msg := msgs[i]
		tokens := cm.tokenizer.CountTokensInMessage(msg)

		// Always include system messages if possible
		if msg.Role == message.RoleSystem {
			if currentTokens+tokens <= maxTokens {
				result = append([]message.Message{msg}, result...)
				currentTokens += tokens
			}
		} else {
			if currentTokens+tokens <= maxTokens {
				result = append([]message.Message{msg}, result...)
				currentTokens += tokens
			}
		}
	}

	return result
}

// GetTokenCount returns the total token count for the given messages.
func (cm *ContextManager) GetTokenCount(msgs []message.Message) int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.tokenizer == nil {
		return 0
	}
	return CountTokensInMessages(msgs, cm.tokenizer)
}

// GetUsagePercentage returns the current usage as a percentage (0.0-1.0).
func (cm *ContextManager) GetUsagePercentage(msgs []message.Message) float64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.tokenizer == nil {
		return 0
	}

	currentTokens := CountTokensInMessages(msgs, cm.tokenizer)
	maxTokens := cm.contextWindow.GetEffectiveLimit()

	if maxTokens == 0 {
		return 1.0
	}

	return float64(currentTokens) / float64(maxTokens)
}

// GetContextWindow returns the configured context window.
func (cm *ContextManager) GetContextWindow() *ContextWindow {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.contextWindow
}

// SetContextWindow updates the context window configuration.
func (cm *ContextManager) SetContextWindow(cw *ContextWindow) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.contextWindow = cw
}

// FitMessages ensures messages fit within the context window by truncating
// if necessary. This is a convenience method that combines token counting
// and truncation.
func (cm *ContextManager) FitMessages(ctx context.Context, msgs []message.Message) ([]message.Message, *ContextWarning) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.tokenizer == nil {
		return msgs, nil
	}

	currentTokens := CountTokensInMessages(msgs, cm.tokenizer)
	effectiveLimit := cm.contextWindow.GetEffectiveLimit()

	var warning *ContextWarning
	if effectiveLimit > 0 {
		usage := float64(currentTokens) / float64(effectiveLimit)
		if usage >= 1.0 {
			warning = &ContextWarning{
				Level:         "critical",
				Usage:         usage,
				CurrentTokens: currentTokens,
				MaxTokens:     effectiveLimit,
				Message:       fmt.Sprintf("Context exceeded: %d/%d tokens (%.1f%%)", currentTokens, effectiveLimit, usage*100),
			}
		} else if usage >= cm.warningThreshold {
			warning = &ContextWarning{
				Level:         "warn",
				Usage:         usage,
				CurrentTokens: currentTokens,
				MaxTokens:     effectiveLimit,
				Message:       fmt.Sprintf("Approaching limit: %d/%d tokens (%.1f%%)", currentTokens, effectiveLimit, usage*100),
			}
		}
	}

	// Truncate if necessary (call internal method to avoid deadlock)
	result := cm.truncateMessagesInternal(ctx, msgs)
	return result, warning
}

// ModelContextWindows provides context window information for common models.
var ModelContextWindows = map[string]*ContextWindow{
	// --- OpenAI GPT-4o family (recommended) ---
	"gpt-4o": {
		MaxTokens:          128000,
		ModelName:          "gpt-4o",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	},
	"gpt-4o-mini": {
		MaxTokens:          128000,
		ModelName:          "gpt-4o-mini",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	},
	// --- OpenAI reasoning models ---
	"o1": {
		MaxTokens:          200000,
		ModelName:          "o1",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	},
	"o1-mini": {
		MaxTokens:          128000,
		ModelName:          "o1-mini",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	},
	"o1-pro": {
		MaxTokens:          200000,
		ModelName:          "o1-pro",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	},
	"o3-mini": {
		MaxTokens:          200000,
		ModelName:          "o3-mini",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	},
	// --- OpenAI GPT-4 family (legacy) ---
	"gpt-4-turbo": {
		MaxTokens:          128000,
		ModelName:          "gpt-4-turbo",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	},
	"gpt-4": {
		MaxTokens:          8192,
		ModelName:          "gpt-4",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	},
	"gpt-4-32k": {
		MaxTokens:          32768,
		ModelName:          "gpt-4-32k",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	},
	"gpt-3.5-turbo": {
		MaxTokens:          16385,
		ModelName:          "gpt-3.5-turbo",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	},
	// --- Anthropic Claude 4 family (recommended) ---
	"claude-sonnet-4-20250514": {
		MaxTokens:          200000,
		ModelName:          "claude-sonnet-4-20250514",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	},
	"claude-opus-4-20250514": {
		MaxTokens:          200000,
		ModelName:          "claude-opus-4-20250514",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	},
	// --- Anthropic Claude 3.5 family ---
	"claude-3-5-sonnet-20241022": {
		MaxTokens:          200000,
		ModelName:          "claude-3-5-sonnet-20241022",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	},
	"claude-3-5-haiku-20241022": {
		MaxTokens:          200000,
		ModelName:          "claude-3-5-haiku-20241022",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	},
	// --- Anthropic Claude 3 family (legacy) ---
	"claude-3-opus-20240229": {
		MaxTokens:          200000,
		ModelName:          "claude-3-opus-20240229",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	},
	"claude-3-sonnet-20240229": {
		MaxTokens:          200000,
		ModelName:          "claude-3-sonnet-20240229",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	},
	"claude-3-haiku-20240307": {
		MaxTokens:          200000,
		ModelName:          "claude-3-haiku-20240307",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	},
	// --- Short aliases for convenience ---
	"claude-4-sonnet": {
		MaxTokens:          200000,
		ModelName:          "claude-sonnet-4-20250514",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	},
	"claude-4-opus": {
		MaxTokens:          200000,
		ModelName:          "claude-opus-4-20250514",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	},
	"claude-3-opus": {
		MaxTokens:          200000,
		ModelName:          "claude-3-opus-20240229",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	},
	"claude-3-sonnet": {
		MaxTokens:          200000,
		ModelName:          "claude-3-sonnet-20240229",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	},
	"claude-3-haiku": {
		MaxTokens:          200000,
		ModelName:          "claude-3-haiku-20240307",
		SafeMargin:         0.05,
		SystemPromptTokens: 500,
	},
}

// GetModelContextWindow returns the context window for a known model,
// or a default if the model is not recognized.
func GetModelContextWindow(modelName string) *ContextWindow {
	if cw, ok := ModelContextWindows[modelName]; ok {
		return cw
	}
	return DefaultContextWindow()
}
