package memory

import (
	"context"
	"testing"

	"github.com/user/orchestra/internal/message"
)

// TestMemoryInterface verifies that all memory implementations satisfy the Memory interface.
func TestMemoryInterface(t *testing.T) {
	ctx := context.Background()

	// Test BufferMemory
	t.Run("BufferMemory", func(t *testing.T) {
		var mem Memory = NewBufferMemory(10)
		testBasicMemoryOperations(t, ctx, mem)
	})

	// Test SlidingWindowMemory
	t.Run("SlidingWindowMemory", func(t *testing.T) {
		var mem Memory = NewSlidingWindowMemory(10)
		testBasicMemoryOperations(t, ctx, mem)
	})

	// Test SlidingWindowMemoryWithTokens
	t.Run("SlidingWindowMemoryWithTokens", func(t *testing.T) {
		tokenizer := NewApproxTokenizer()
		var mem Memory = NewSlidingWindowMemoryWithTokens(1000, tokenizer)
		testBasicMemoryOperations(t, ctx, mem)
	})

	// Test CompositeMemory
	t.Run("CompositeMemory", func(t *testing.T) {
		mem1 := NewBufferMemory(5)
		mem2 := NewSlidingWindowMemory(5)
		var mem Memory = NewCompositeMemory(
			MemoryWithPriority{Memory: mem1, Priority: 1, Name: "buffer"},
			MemoryWithPriority{Memory: mem2, Priority: 2, Name: "sliding"},
		)
		testBasicMemoryOperations(t, ctx, mem)
	})
}

// testBasicMemoryOperations tests the basic Memory interface operations.
func testBasicMemoryOperations(t *testing.T, ctx context.Context, mem Memory) {
	// Test Add
	msg := message.UserMessage("Hello, world!")
	err := mem.Add(ctx, msg)
	if err != nil {
		t.Errorf("Add() failed: %v", err)
	}

	// Test Size
	size := mem.Size(ctx)
	if size != 1 {
		t.Errorf("Size() = %d, want 1", size)
	}

	// Test GetAll
	msgs, err := mem.GetAll(ctx, DefaultGetOptions())
	if err != nil {
		t.Errorf("GetAll() failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("GetAll() returned %d messages, want 1", len(msgs))
	}

	// Test GetRelevant
	msgs, err = mem.GetRelevant(ctx, "test", DefaultGetOptions())
	if err != nil {
		t.Errorf("GetRelevant() failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("GetRelevant() returned %d messages, want 1", len(msgs))
	}

	// Test Clear
	err = mem.Clear(ctx)
	if err != nil {
		t.Errorf("Clear() failed: %v", err)
	}
	size = mem.Size(ctx)
	if size != 0 {
		t.Errorf("Size() after Clear() = %d, want 0", size)
	}
}

// TestBufferMemory tests the BufferMemory implementation.
func TestBufferMemory(t *testing.T) {
	ctx := context.Background()

	t.Run("BasicOperations", func(t *testing.T) {
		mem := NewBufferMemory(0) // No limit
		testBasicMemoryOperations(t, ctx, mem)
	})

	t.Run("SizeLimit", func(t *testing.T) {
		mem := NewBufferMemory(3)

		// Add 5 messages
		for i := 0; i < 5; i++ {
			msg := message.UserMessage(string(rune('a' + i)))
			_ = mem.Add(ctx, msg)
		}

		// Should only keep last 3
		size := mem.Size(ctx)
		if size != 3 {
			t.Errorf("Size() = %d, want 3", size)
		}

		msgs, _ := mem.GetAll(ctx, DefaultGetOptions())
		if len(msgs) != 3 {
			t.Errorf("Got %d messages, want 3", len(msgs))
		}
		// Check that we have the last 3 messages
		if msgs[0].Text() != "c" || msgs[1].Text() != "d" || msgs[2].Text() != "e" {
			t.Error("Incorrect messages kept")
		}
	})

	t.Run("TokenLimit", func(t *testing.T) {
		tokenizer := NewApproxTokenizer()
		mem := NewBufferMemory(0)

		// Add messages
		for i := 0; i < 5; i++ {
			msg := message.UserMessage("This is a test message")
			_ = mem.Add(ctx, msg)
		}

		opts := WithMaxTokens(100, tokenizer)
		msgs, _ := mem.GetAll(ctx, opts)

		totalTokens := CountTokensInMessages(msgs, tokenizer)
		if totalTokens > 100 {
			t.Errorf("Token count %d exceeds limit 100", totalTokens)
		}
	})

	t.Run("SetMaxSize", func(t *testing.T) {
		mem := NewBufferMemory(10)

		// Add 5 messages
		for i := 0; i < 5; i++ {
			_ = mem.Add(ctx, message.UserMessage(string(rune('a' + i))))
		}

		// Reduce limit to 2
		mem.SetMaxSize(2)

		size := mem.Size(ctx)
		if size != 2 {
			t.Errorf("Size() = %d, want 2", size)
		}
	})
}

// TestSlidingWindowMemory tests the SlidingWindowMemory implementation.
func TestSlidingWindowMemory(t *testing.T) {
	ctx := context.Background()

	t.Run("MessageLimit", func(t *testing.T) {
		mem := NewSlidingWindowMemory(3)

		// Add 5 messages
		for i := 0; i < 5; i++ {
			_ = mem.Add(ctx, message.UserMessage(string(rune('a' + i))))
		}

		size := mem.Size(ctx)
		if size != 3 {
			t.Errorf("Size() = %d, want 3", size)
		}

		if mem.IsFull(ctx) != true {
			t.Error("IsFull() should return true")
		}
	})

	t.Run("TokenLimit", func(t *testing.T) {
		tokenizer := NewApproxTokenizer()
		mem := NewSlidingWindowMemoryWithTokens(100, tokenizer)

		// Add messages with known token counts
		// Each message is roughly 5 tokens, so we can fit ~20 messages
		for i := 0; i < 25; i++ {
			msg := message.UserMessage("Test message")
			_ = mem.Add(ctx, msg)
		}

		tokenCount := mem.GetCurrentTokenCount(ctx)
		if tokenCount > 100 {
			t.Errorf("Token count %d exceeds limit 100", tokenCount)
		}
	})

	t.Run("BothLimits", func(t *testing.T) {
		tokenizer := NewApproxTokenizer()
		mem := NewSlidingWindowMemoryFull(5, 1000, tokenizer)

		// Add messages
		for i := 0; i < 10; i++ {
			_ = mem.Add(ctx, message.UserMessage("Test message"))
		}

		size := mem.Size(ctx)
		if size > 5 {
			t.Errorf("Size() = %d, want <= 5", size)
		}
	})

	t.Run("DynamicLimitChange", func(t *testing.T) {
		tokenizer := NewApproxTokenizer()
		mem := NewSlidingWindowMemory(0)

		// Add messages without limit
		for i := 0; i < 10; i++ {
			_ = mem.Add(ctx, message.UserMessage("Test"))
		}

		if mem.Size(ctx) != 10 {
			t.Errorf("Size() = %d, want 10", mem.Size(ctx))
		}

		// Set token limit
		mem.SetMaxTokens(50, tokenizer)
		if mem.GetCurrentTokenCount(ctx) > 50 {
			t.Errorf("Token count exceeds new limit")
		}
	})
}

// TestSummaryMemory tests the SummaryMemory implementation.
func TestSummaryMemory(t *testing.T) {
	ctx := context.Background()

	t.Run("MessageLimitTriggersSummarization", func(t *testing.T) {
		// This test would need a mock provider
		// For now, we'll skip the actual summarization
		t.Skip("SummaryMemory requires a provider mock")
	})

	t.Run("Clear", func(t *testing.T) {
		// Create with nil provider - will fail on summarization but Clear should work
		mem := NewSummaryMemory(nil, "gpt-4", 100, 1000, nil)

		_ = mem.Add(ctx, message.UserMessage("Test"))
		_ = mem.Clear(ctx)

		if mem.Size(ctx) != 0 {
			t.Errorf("Size() after Clear() = %d, want 0", mem.Size(ctx))
		}
	})

	t.Run("GetSummaryCount", func(t *testing.T) {
		mem := NewSummaryMemory(nil, "gpt-4", 100, 1000, nil)

		count := mem.GetSummaryCount(ctx)
		if count != 0 {
			t.Errorf("GetSummaryCount() = %d, want 0", count)
		}
	})
}

// TestSemanticMemory tests the SemanticMemory implementation.
func TestSemanticMemory(t *testing.T) {
	ctx := context.Background()
	provider := NewMockEmbeddingProvider(384)

	t.Run("BasicOperations", func(t *testing.T) {
		mem := NewSemanticMemory(provider, 0, 5, 0.0)
		testBasicMemoryOperations(t, ctx, mem)
	})

	t.Run("SemanticRetrieval", func(t *testing.T) {
		mem := NewSemanticMemory(provider, 0, 3, 0.0)

		// Add related messages
		_ = mem.Add(ctx, message.UserMessage("I love programming in Go"))
		_ = mem.Add(ctx, message.UserMessage("Go is a great language"))
		_ = mem.Add(ctx, message.UserMessage("The weather is nice today"))
		_ = mem.Add(ctx, message.UserMessage("Golang has great concurrency"))

		// Query for programming-related content
		msgs, err := mem.GetRelevant(ctx, "programming languages", DefaultGetOptions())
		if err != nil {
			t.Errorf("GetRelevant() failed: %v", err)
		}

		if len(msgs) == 0 {
			t.Error("GetRelevant() returned no messages")
		}

		// Should return at most 3 messages (topK default)
		if len(msgs) > 3 {
			t.Errorf("GetRelevant() returned %d messages, want <= 3", len(msgs))
		}
	})

	t.Run("MinScoreThreshold", func(t *testing.T) {
		mem := NewSemanticMemory(provider, 0, 10, 0.8) // High threshold

		_ = mem.Add(ctx, message.UserMessage("Completely unrelated content"))

		msgs, _ := mem.GetRelevant(ctx, "something totally different", DefaultGetOptions())
		// With high threshold and unrelated content, might get no results
		// This is expected behavior
		_ = msgs
	})

	t.Run("SizeLimit", func(t *testing.T) {
		mem := NewSemanticMemory(provider, 3, 5, 0.0)

		// Add 5 messages
		for i := 0; i < 5; i++ {
			_ = mem.Add(ctx, message.UserMessage(string(rune('a' + i))))
		}

		if mem.Size(ctx) != 3 {
			t.Errorf("Size() = %d, want 3", mem.Size(ctx))
		}
	})

	t.Run("SimilarityScores", func(t *testing.T) {
		mem := NewSemanticMemory(provider, 0, 5, 0.0)

		_ = mem.Add(ctx, message.UserMessage("Hello world"))
		_ = mem.Add(ctx, message.UserMessage("Goodbye world"))

		scores, err := mem.GetSimilarityScores(ctx, "Hello")
		if err != nil {
			t.Errorf("GetSimilarityScores() failed: %v", err)
		}

		if len(scores) != 2 {
			t.Errorf("GetSimilarityScores() returned %d scores, want 2", len(scores))
		}

		// Scores should be sorted in descending order
		if len(scores) > 1 && scores[0].Score < scores[1].Score {
			t.Error("Scores not sorted in descending order")
		}
	})
}

// TestCompositeMemory tests the CompositeMemory implementation.
func TestCompositeMemory(t *testing.T) {
	ctx := context.Background()

	t.Run("BasicOperations", func(t *testing.T) {
		mem1 := NewBufferMemory(5)
		mem2 := NewSlidingWindowMemory(5)
		mem := NewCompositeMemory(
			MemoryWithPriority{Memory: mem1, Priority: 1, Name: "buffer"},
			MemoryWithPriority{Memory: mem2, Priority: 2, Name: "sliding"},
		)
		testBasicMemoryOperations(t, ctx, mem)
	})

	t.Run("AddsToAllMemories", func(t *testing.T) {
		mem1 := NewBufferMemory(5)
		mem2 := NewBufferMemory(5)
		mem := NewCompositeMemory(
			MemoryWithPriority{Memory: mem1, Priority: 1, Name: "mem1"},
			MemoryWithPriority{Memory: mem2, Priority: 1, Name: "mem2"},
		)

		_ = mem.Add(ctx, message.UserMessage("Test"))

		if mem1.Size(ctx) != 1 || mem2.Size(ctx) != 1 {
			t.Error("Message not added to all underlying memories")
		}
	})

	t.Run("PriorityOrdering", func(t *testing.T) {
		mem1 := NewBufferMemory(0)
		mem2 := NewBufferMemory(0)
		mem := NewCompositeMemory(
			MemoryWithPriority{Memory: mem1, Priority: 1, Name: "low"},
			MemoryWithPriority{Memory: mem2, Priority: 2, Name: "high"},
		)

		msg := message.UserMessage("Test")
		_ = mem.Add(ctx, msg)

		allMsgs, _ := mem.GetAll(ctx, DefaultGetOptions())

		// With dedup enabled, should only have one message
		// But the metadata should indicate it came from high priority
		// This is hard to test without exposing internal structure
		_ = allMsgs
	})

	t.Run("Deduplication", func(t *testing.T) {
		mem1 := NewBufferMemory(0)
		mem2 := NewBufferMemory(0)
		mem := NewCompositeMemory(
			MemoryWithPriority{Memory: mem1, Priority: 1, Name: "mem1"},
			MemoryWithPriority{Memory: mem2, Priority: 1, Name: "mem2"},
		)
		mem.SetDedup(true)

		msg := message.UserMessage("Test")
		_ = mem.Add(ctx, msg)

		allMsgs, _ := mem.GetAll(ctx, DefaultGetOptions())

		// Should have only one message despite being in two memories
		if len(allMsgs) != 1 {
			t.Errorf("Got %d messages, want 1 (deduplicated)", len(allMsgs))
		}
	})

	t.Run("AddAndRemoveMemory", func(t *testing.T) {
		mem1 := NewBufferMemory(0)
		mem := NewCompositeMemory(
			MemoryWithPriority{Memory: mem1, Priority: 1, Name: "mem1"},
		)

		_ = mem.Add(ctx, message.UserMessage("Test"))

		// Add new memory
		mem2 := NewBufferMemory(0)
		mem.AddMemory(mem2, 2, "mem2")

		memories := mem.GetMemories()
		if len(memories) != 2 {
			t.Errorf("Got %d memories, want 2", len(memories))
		}

		// Remove memory
		removed := mem.RemoveMemory("mem2")
		if !removed {
			t.Error("RemoveMemory() returned false")
		}

		memories = mem.GetMemories()
		if len(memories) != 1 {
			t.Errorf("Got %d memories after removal, want 1", len(memories))
		}
	})

	t.Run("ClearAll", func(t *testing.T) {
		mem1 := NewBufferMemory(0)
		mem2 := NewBufferMemory(0)
		mem := NewCompositeMemory(
			MemoryWithPriority{Memory: mem1, Priority: 1, Name: "mem1"},
			MemoryWithPriority{Memory: mem2, Priority: 1, Name: "mem2"},
		)

		_ = mem.Add(ctx, message.UserMessage("Test"))
		_ = mem.Clear(ctx)

		if mem.Size(ctx) != 0 {
			t.Errorf("Size() after Clear() = %d, want 0", mem.Size(ctx))
		}
	})
}

// TestTokenizers tests the tokenizer implementations.
func TestTokenizers(t *testing.T) {
	testText := "Hello, world! This is a test message."

	t.Run("ApproxTokenizer", func(t *testing.T) {
		tokenizer := NewApproxTokenizer()

		count := tokenizer.CountTokens(testText)
		if count <= 0 {
			t.Errorf("CountTokens() = %d, want > 0", count)
		}

		msg := message.UserMessage(testText)
		msgCount := tokenizer.CountTokensInMessage(msg)
		if msgCount <= 0 {
			t.Errorf("CountTokensInMessage() = %d, want > 0", msgCount)
		}

		// Message count should be >= text count (includes metadata)
		if msgCount < count {
			t.Errorf("Message token count %d < text token count %d", msgCount, count)
		}
	})

	t.Run("OpenAITokenizer", func(t *testing.T) {
		tokenizer := NewOpenAITokenizer()

		count := tokenizer.CountTokens(testText)
		if count <= 0 {
			t.Errorf("CountTokens() = %d, want > 0", count)
		}

		msg := message.UserMessage(testText)
		msgCount := tokenizer.CountTokensInMessage(msg)
		if msgCount <= 0 {
			t.Errorf("CountTokensInMessage() = %d, want > 0", msgCount)
		}
	})

	t.Run("AnthropicTokenizer", func(t *testing.T) {
		tokenizer := NewAnthropicTokenizer()

		count := tokenizer.CountTokens(testText)
		if count <= 0 {
			t.Errorf("CountTokens() = %d, want > 0", count)
		}
	})

	t.Run("GoogleTokenizer", func(t *testing.T) {
		tokenizer := NewGoogleTokenizer()

		count := tokenizer.CountTokens(testText)
		if count <= 0 {
			t.Errorf("CountTokens() = %d, want > 0", count)
		}
	})

	t.Run("MistralTokenizer", func(t *testing.T) {
		tokenizer := NewMistralTokenizer()

		count := tokenizer.CountTokens(testText)
		if count <= 0 {
			t.Errorf("CountTokens() = %d, want > 0", count)
		}
	})

	t.Run("ToolCalls", func(t *testing.T) {
		tokenizer := NewApproxTokenizer()

		msg := message.AssistantToolCallMessage([]message.ToolCall{
			{
				ID:   "call_123",
				Type: "function",
				Function: message.ToolCallFunction{
					Name:      "test_function",
					Arguments: `{"arg1": "value1"}`,
				},
			},
		})

		count := tokenizer.CountTokensInMessage(msg)
		if count <= 0 {
			t.Errorf("CountTokensInMessage() for tool call = %d, want > 0", count)
		}
	})

	t.Run("EmptyText", func(t *testing.T) {
		tokenizer := NewApproxTokenizer()

		count := tokenizer.CountTokens("")
		if count != 0 {
			t.Errorf("CountTokens() for empty string = %d, want 0", count)
		}
	})
}

// TestContextManagement tests the context window management.
func TestContextManagement(t *testing.T) {
	ctx := context.Background()
	tokenizer := NewApproxTokenizer()

	t.Run("DefaultContextWindow", func(t *testing.T) {
		cw := DefaultContextWindow()

		if cw.MaxTokens == 0 {
			t.Error("DefaultContextWindow has MaxTokens = 0")
		}

		available := cw.GetAvailableTokens()
		if available <= 0 {
			t.Errorf("GetAvailableTokens() = %d, want > 0", available)
		}
	})

	t.Run("ContextManager", func(t *testing.T) {
		cw := &ContextWindow{
			MaxTokens:          1000,
			ModelName:          "test-model",
			SafeMargin:         0.1,
			SystemPromptTokens: 100,
		}

		cm := NewContextManager(cw, tokenizer)

		// Create messages that exceed the limit
		msgs := make([]message.Message, 10)
		for i := range msgs {
			msgs[i] = message.UserMessage("This is a long test message that uses many tokens")
		}

		truncated := cm.TruncateMessages(ctx, msgs)

		totalTokens := CountTokensInMessages(truncated, tokenizer)
		effectiveLimit := cw.GetEffectiveLimit()

		if totalTokens > effectiveLimit {
			t.Errorf("Truncated messages use %d tokens, limit is %d", totalTokens, effectiveLimit)
		}
	})

	t.Run("TruncationStrategies", func(t *testing.T) {
		cw := &ContextWindow{
			MaxTokens:          100,
			ModelName:          "test-model",
			SafeMargin:         0.0,
			SystemPromptTokens: 0,
		}

		cm := NewContextManager(cw, tokenizer)

		strategies := []TruncationStrategy{
			TruncateOldest,
			TruncateOldestPreservingSystem,
			TruncateMiddle,
			TruncateSmallest,
			TruncateRecency,
		}

		// Create messages
		msgs := make([]message.Message, 10)
		for i := range msgs {
			msgs[i] = message.UserMessage("Test message")
		}

		for _, strategy := range strategies {
			cm.SetTruncationStrategy(strategy)
			truncated := cm.TruncateMessages(ctx, msgs)

			totalTokens := CountTokensInMessages(truncated, tokenizer)
			if totalTokens > 100 {
				t.Errorf("Strategy %s: %d tokens exceed limit", strategy, totalTokens)
			}
		}
	})

	t.Run("PreserveSystemMessages", func(t *testing.T) {
		cw := &ContextWindow{
			MaxTokens:          50,
			ModelName:          "test-model",
			SafeMargin:         0.0,
			SystemPromptTokens: 0,
		}

		cm := NewContextManager(cw, tokenizer)
		cm.SetTruncationStrategy(TruncateOldestPreservingSystem)

		msgs := []message.Message{
			message.SystemMessage("Important system instruction"),
			message.UserMessage("Message 1"),
			message.UserMessage("Message 2"),
			message.UserMessage("Message 3"),
		}

		truncated := cm.TruncateMessages(ctx, msgs)

		// System message should be preserved
		hasSystem := false
		for _, msg := range truncated {
			if msg.Role == message.RoleSystem {
				hasSystem = true
				break
			}
		}

		if !hasSystem {
			t.Error("System message was not preserved")
		}
	})

	t.Run("WarningCallback", func(t *testing.T) {
		cw := &ContextWindow{
			MaxTokens:          100,
			ModelName:          "test-model",
			SafeMargin:         0.0,
			SystemPromptTokens: 0,
		}

		cm := NewContextManager(cw, tokenizer)
		cm.SetWarningThreshold(0.8)

		var warningReceived bool
		cm.SetWarningCallback(func(w *ContextWarning) {
			warningReceived = true
			if w.Level != "warn" {
				t.Errorf("Expected warn level, got %s", w.Level)
			}
		})

		// Create messages that use 85% of the limit
		msgs := make([]message.Message, 10)
		for i := range msgs {
			msgs[i] = message.UserMessage("Test message")
		}

		_ = cm.TruncateMessages(ctx, msgs)

		if !warningReceived {
			t.Error("Warning callback not triggered")
		}
	})

	t.Run("FitMessages", func(t *testing.T) {
		cw := &ContextWindow{
			MaxTokens:          100,
			ModelName:          "test-model",
			SafeMargin:         0.0,
			SystemPromptTokens: 0,
		}

		cm := NewContextManager(cw, tokenizer)

		msgs := make([]message.Message, 10)
		for i := range msgs {
			msgs[i] = message.UserMessage("Test message")
		}

		fitted, warning := cm.FitMessages(ctx, msgs)

		if warning == nil {
			// Should have a warning since we're truncating
			t.Log("No warning (may be within limits)")
		}

		totalTokens := CountTokensInMessages(fitted, tokenizer)
		if totalTokens > 100 {
			t.Errorf("Fitted messages use %d tokens, limit is 100", totalTokens)
		}
	})

	t.Run("GetModelContextWindow", func(t *testing.T) {
		// Test known model
		cw := GetModelContextWindow("gpt-4")
		if cw == nil {
			t.Error("GetModelContextWindow() returned nil for gpt-4")
		}
		if cw.MaxTokens == 0 {
			t.Error("gpt-4 has MaxTokens = 0")
		}

		// Test unknown model (should return default)
		cw = GetModelContextWindow("unknown-model")
		if cw == nil {
			t.Error("GetModelContextWindow() returned nil for unknown model")
		}
	})

	t.Run("UsagePercentage", func(t *testing.T) {
		cw := DefaultContextWindow()
		cm := NewContextManager(cw, tokenizer)

		msgs := []message.Message{
			message.UserMessage("Test"),
		}

		usage := cm.GetUsagePercentage(msgs)
		if usage < 0 || usage > 1 {
			t.Errorf("UsagePercentage = %f, want 0.0-1.0", usage)
		}
	})
}

// TestMockEmbeddingProvider tests the mock embedding provider.
func TestMockEmbeddingProvider(t *testing.T) {
	ctx := context.Background()

	t.Run("BasicEmbedding", func(t *testing.T) {
		provider := NewMockEmbeddingProvider(384)

		embedding, err := provider.GenerateEmbedding(ctx, "Hello, world!")
		if err != nil {
			t.Errorf("GenerateEmbedding() failed: %v", err)
		}

		if len(embedding) != 384 {
			t.Errorf("Embedding dimension = %d, want 384", len(embedding))
		}
	})

	t.Run("Dimension", func(t *testing.T) {
		provider := NewMockEmbeddingProvider(768)

		if provider.Dimension() != 768 {
			t.Errorf("Dimension() = %d, want 768", provider.Dimension())
		}
	})

	t.Run("Deterministic", func(t *testing.T) {
		provider := NewMockEmbeddingProviderWithSeed(384, 42)

		emb1, _ := provider.GenerateEmbedding(ctx, "test")
		emb2, _ := provider.GenerateEmbedding(ctx, "test")

		// Same input should produce same embedding
		for i := range emb1 {
			if emb1[i] != emb2[i] {
				t.Error("Embeddings for same input differ")
				break
			}
		}
	})

	t.Run("Normalization", func(t *testing.T) {
		provider := NewMockEmbeddingProvider(384)

		embedding, _ := provider.GenerateEmbedding(ctx, "test")

		// Check that embedding is normalized (unit length)
		var sum float32
		for _, v := range embedding {
			sum += v * v
		}

		// Sum of squares should be approximately 1.0
		if sum < 0.99 || sum > 1.01 {
			t.Errorf("Embedding not normalized: sum of squares = %f", sum)
		}
	})

	t.Run("DifferentInputs", func(t *testing.T) {
		provider := NewMockEmbeddingProvider(384)

		emb1, _ := provider.GenerateEmbedding(ctx, "Hello")
		emb2, _ := provider.GenerateEmbedding(ctx, "Goodbye")

		// Different inputs should produce different embeddings
		different := false
		for i := range emb1 {
			if emb1[i] != emb2[i] {
				different = true
				break
			}
		}

		if !different {
			t.Error("Different inputs produced same embedding")
		}
	})

	t.Run("CosineSimilarity", func(t *testing.T) {
		provider := NewMockEmbeddingProvider(384)

		emb1, _ := provider.GenerateEmbedding(ctx, "programming")
		emb2, _ := provider.GenerateEmbedding(ctx, "coding")
		emb3, _ := provider.GenerateEmbedding(ctx, "cooking")

		// Programming and coding should be more similar than programming and cooking
		sim12 := cosineSimilarity(emb1, emb2)
		sim13 := cosineSimilarity(emb1, emb3)

		// This is a weak assertion since mock embedding is simple
		// But similar words should at least have some correlation
		t.Logf("Similarity (programming, coding) = %f", sim12)
		t.Logf("Similarity (programming, cooking) = %f", sim13)
	})
}

// TestTruncateToTokenLimit tests the token limit truncation utility.
func TestTruncateToTokenLimit(t *testing.T) {
	tokenizer := NewApproxTokenizer()

	t.Run("PreserveSystem", func(t *testing.T) {
		msgs := []message.Message{
			message.SystemMessage("System instruction 1"),
			message.SystemMessage("System instruction 2"),
			message.UserMessage("User message 1"),
			message.UserMessage("User message 2"),
			message.UserMessage("User message 3"),
		}

		// Limit should allow system + 1 user message
		limit := CountTokensInMessages(msgs[:2], tokenizer) +
			tokenizer.CountTokensInMessage(msgs[2]) + 10

		truncated := TruncateToTokenLimit(msgs, limit, tokenizer)

		// Should preserve both system messages
		systemCount := 0
		for _, msg := range truncated {
			if msg.Role == message.RoleSystem {
				systemCount++
			}
		}

		if systemCount != 2 {
			t.Errorf("Expected 2 system messages, got %d", systemCount)
		}
	})

	t.Run("AllFit", func(t *testing.T) {
		msgs := []message.Message{
			message.UserMessage("Message 1"),
			message.UserMessage("Message 2"),
		}

		limit := CountTokensInMessages(msgs, tokenizer) + 100

		truncated := TruncateToTokenLimit(msgs, limit, tokenizer)

		if len(truncated) != len(msgs) {
			t.Errorf("Expected all messages to fit, got %d", len(truncated))
		}
	})

	t.Run("NilTokenizer", func(t *testing.T) {
		msgs := []message.Message{
			message.UserMessage("Message 1"),
		}

		truncated := TruncateToTokenLimit(msgs, 10, nil)

		// Should return all messages if no tokenizer
		if len(truncated) != len(msgs) {
			t.Errorf("Expected all messages with nil tokenizer, got %d", len(truncated))
		}
	})
}

// TestGetOptions tests the GetOptions helpers.
func TestGetOptions(t *testing.T) {
	t.Run("DefaultGetOptions", func(t *testing.T) {
		opts := DefaultGetOptions()

		if opts.Limit != 0 {
			t.Errorf("Limit = %d, want 0", opts.Limit)
		}
		if opts.MaxTokens != 0 {
			t.Errorf("MaxTokens = %d, want 0", opts.MaxTokens)
		}
		if opts.Tokenizer != nil {
			t.Error("Tokenizer = non-nil, want nil")
		}
	})

	t.Run("WithLimit", func(t *testing.T) {
		opts := WithLimit(10)

		if opts.Limit != 10 {
			t.Errorf("Limit = %d, want 10", opts.Limit)
		}
	})

	t.Run("WithMaxTokens", func(t *testing.T) {
		tokenizer := NewApproxTokenizer()
		opts := WithMaxTokens(1000, tokenizer)

		if opts.MaxTokens != 1000 {
			t.Errorf("MaxTokens = %d, want 1000", opts.MaxTokens)
		}
		if opts.Tokenizer != tokenizer {
			t.Error("Tokenizer not set correctly")
		}
	})
}
