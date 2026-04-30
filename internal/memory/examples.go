// Package memory provides examples demonstrating how to use the various memory strategies
// and utilities for managing conversation context in the Orchestra framework.
//
// These examples show practical usage patterns for different memory implementations
// and how to integrate them with agents.
package memory

import (
	"context"
	"fmt"
	"log"

	"github.com/user/orchestra/internal/message"
)

// Example 1: Basic BufferMemory
// BufferMemory is the simplest memory strategy - it stores messages in memory
// with an optional size limit. Use this when you need a simple FIFO buffer.
func ExampleBufferMemory() {
	ctx := context.Background()

	// Create a buffer memory that stores up to 100 messages
	mem := NewBufferMemory(100)

	// Add messages to memory
	_ = mem.Add(ctx, message.UserMessage("Hello, how can I help you?"))
	_ = mem.Add(ctx, message.AssistantMessage("I need help with programming."))
	_ = mem.Add(ctx, message.UserMessage("What programming language?"))
	_ = mem.Add(ctx, message.AssistantMessage("Go, please."))

	// Retrieve all messages
	msgs, err := mem.GetAll(ctx, DefaultGetOptions())
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Total messages: %d\n", len(msgs))
	// Output: Total messages: 4

	// Retrieve with a limit
	msgs, err = mem.GetAll(ctx, WithLimit(2))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Last 2 messages: %d\n", len(msgs))
	// Output: Last 2 messages: 2
}

// Example 2: SlidingWindowMemory with Message Limit
// SlidingWindowMemory automatically removes the oldest messages when the limit is exceeded.
// This is perfect for keeping only the most recent conversation history.
func ExampleSlidingWindowMemory() {
	ctx := context.Background()

	// Create a sliding window that keeps only the last 5 messages
	mem := NewSlidingWindowMemory(5)

	// Add 10 messages
	for i := 0; i < 10; i++ {
		_ = mem.Add(ctx, message.UserMessage(fmt.Sprintf("Message %d", i)))
	}

	// Only the last 5 messages should remain
	fmt.Printf("Messages in window: %d\n", mem.Size(ctx))
	// Output: Messages in window: 5

	// Retrieve the messages
	msgs, _ := mem.GetAll(ctx, DefaultGetOptions())
	for i, msg := range msgs {
		fmt.Printf("%d: %s\n", i, msg.Text())
	}
	// Output:
	// 0: Message 5
	// 1: Message 6
	// 2: Message 7
	// 3: Message 8
	// 4: Message 9
}

// Example 3: SlidingWindowMemory with Token Limit
// You can also limit based on token count, which is more accurate for LLM context windows.
func ExampleSlidingWindowMemoryWithTokens() {
	ctx := context.Background()

	// Create a tokenizer for OpenAI models
	tokenizer := NewOpenAITokenizer()

	// Create a sliding window limited to 1000 tokens
	mem := NewSlidingWindowMemoryWithTokens(1000, tokenizer)

	// Add messages
	for i := 0; i < 20; i++ {
		msg := message.UserMessage("This is a test message that uses some tokens")
		_ = mem.Add(ctx, msg)
	}

	// Check current token count
	currentTokens := mem.GetCurrentTokenCount(ctx)
	fmt.Printf("Current tokens: %d\n", currentTokens)
	// Output: Current tokens: [some value <= 1000]

	// Check if memory is full
	fmt.Printf("Is full: %v\n", mem.IsFull(ctx))
	// Output: Is full: true or false

	// Add more messages - oldest will be removed to stay under 1000 tokens
	_ = mem.Add(ctx, message.UserMessage("Another message"))
}

// Example 4: Using SemanticMemory
// SemanticMemory uses vector embeddings to retrieve messages based on semantic similarity.
// This allows you to find messages that are conceptually related to a query.
func ExampleSemanticMemory() {
	ctx := context.Background()

	// Create a mock embedding provider (use a real one in production)
	provider := NewMockEmbeddingProvider(384)

	// Create semantic memory
	mem := NewSemanticMemory(provider, 100, 5, 0.5) // max 100 msgs, return top 5, min score 0.5

	// Add messages
	_ = mem.Add(ctx, message.UserMessage("I love programming in Go"))
	_ = mem.Add(ctx, message.UserMessage("Go has excellent concurrency support"))
	_ = mem.Add(ctx, message.UserMessage("The weather is nice today"))
	_ = mem.Add(ctx, message.UserMessage("Golang is great for building APIs"))
	_ = mem.Add(ctx, message.UserMessage("I enjoy hiking in the mountains"))

	// Query for programming-related content
	query := "What do you know about Go programming?"
	msgs, err := mem.GetRelevant(ctx, query, DefaultGetOptions())
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Found %d relevant messages\n", len(msgs))
	for i, msg := range msgs {
		fmt.Printf("%d. %s\n", i+1, msg.Text())
	}
	// Output will show messages about Go programming, not weather or hiking

	// Get similarity scores for debugging
	scores, _ := mem.GetSimilarityScores(ctx, query)
	for _, score := range scores {
		fmt.Printf("  Score %f: %s\n", score.Score, score.Message.Text())
	}
}

// Example 5: CompositeMemory - Combining Multiple Strategies
// CompositeMemory allows you to combine different memory strategies for optimal results.
// Common pattern: Use a sliding window for recent messages + semantic search for context.
func ExampleCompositeMemory() {
	ctx := context.Background()

	// Create individual memories
	recentMem := NewSlidingWindowMemory(10) // Keep last 10 messages
	provider := NewMockEmbeddingProvider(384)
	semanticMem := NewSemanticMemory(provider, 50, 5, 0.6) // Semantic search

	// Combine them into a composite memory
	// Higher priority = results appear first
	mem := NewCompositeMemory(
		MemoryWithPriority{
			Memory:   recentMem,
			Priority: 2, // Recent messages have higher priority
			Name:     "recent",
		},
		MemoryWithPriority{
			Memory:   semanticMem,
			Priority: 1, // Semantic results are secondary
			Name:     "semantic",
		},
	)

	// Add messages - they go to all underlying memories
	_ = mem.Add(ctx, message.UserMessage("My name is Alice"))
	_ = mem.Add(ctx, message.UserMessage("I work as a software engineer"))
	_ = mem.Add(ctx, message.UserMessage("I specialize in backend development"))
	_ = mem.Add(ctx, message.UserMessage("Hello!"))
	_ = mem.Add(ctx, message.UserMessage("What can you tell me about Alice?"))

	// Query combines recent messages and semantic matches
	query := "What does Alice do?"
	msgs, _ := mem.GetRelevant(ctx, query, DefaultGetOptions())

	fmt.Printf("Found %d messages\n", len(msgs))
	for i, msg := range msgs {
		fmt.Printf("%d. %s\n", i+1, msg.Text())
	}
	// Results will prioritize recent messages, then include semantically relevant ones
}

// Example 6: Context Window Management
// ContextManager helps you stay within model context limits automatically.
func ExampleContextManager() {
	ctx := context.Background()

	// Get context window for a specific model
	cw := GetModelContextWindow("gpt-4")
	fmt.Printf("Model: %s, Max tokens: %d\n", cw.ModelName, cw.MaxTokens)
	// Output: Model: gpt-4, Max tokens: 8192

	// Create a context manager with a tokenizer
	tokenizer := NewOpenAITokenizer()
	cm := NewContextManager(cw, tokenizer)

	// Set up warnings when approaching limits
	cm.SetWarningThreshold(0.9) // Warn at 90%
	cm.SetWarningCallback(func(warning *ContextWarning) {
		fmt.Printf("WARNING [%s]: %s\n", warning.Level, warning.Message)
	})

	// Create a long conversation
	msgs := make([]message.Message, 100)
	for i := range msgs {
		msgs[i] = message.UserMessage(fmt.Sprintf("This is message number %d in a long conversation", i))
	}

	// Truncate to fit in context window
	truncated := cm.TruncateMessages(ctx, msgs)
	fmt.Printf("Original: %d messages, Truncated: %d messages\n", len(msgs), len(truncated))

	// Check usage percentage
	usage := cm.GetUsagePercentage(truncated)
	fmt.Printf("Context usage: %.1f%%\n", usage*100)
}

// Example 7: Context Manager with Different Truncation Strategies
// You can choose how messages are truncated when limits are exceeded.
func ExampleTruncationStrategies() {
	ctx := context.Background()

	cw := &ContextWindow{
		MaxTokens:          100,
		ModelName:          "test-model",
		SafeMargin:         0.05,
		SystemPromptTokens: 20,
	}

	tokenizer := NewApproxTokenizer()
	cm := NewContextManager(cw, tokenizer)

	// Create messages including a system prompt
	msgs := []message.Message{
		message.SystemMessage("You are a helpful assistant"),
		message.UserMessage("Message 1"),
		message.UserMessage("Message 2"),
		message.UserMessage("Message 3"),
		message.UserMessage("Message 4"),
	}

	// Strategy 1: Remove oldest messages
	cm.SetTruncationStrategy(TruncateOldest)
	result1 := cm.TruncateMessages(ctx, msgs)
	fmt.Printf("Oldest-first: %d messages\n", len(result1))

	// Strategy 2: Remove oldest but keep system messages
	cm.SetTruncationStrategy(TruncateOldestPreservingSystem)
	result2 := cm.TruncateMessages(ctx, msgs)
	fmt.Printf("Preserve system: %d messages\n", len(result2))

	// Strategy 3: Remove from the middle
	cm.SetTruncationStrategy(TruncateMiddle)
	result3 := cm.TruncateMessages(ctx, msgs)
	fmt.Printf("Truncate middle: %d messages\n", len(result3))

	// Strategy 4: Remove smallest messages first
	cm.SetTruncationStrategy(TruncateSmallest)
	result4 := cm.TruncateMessages(ctx, msgs)
	fmt.Printf("Truncate smallest: %d messages\n", len(result4))

	// Strategy 5: Keep only most recent (recency)
	cm.SetTruncationStrategy(TruncateRecency)
	result5 := cm.TruncateMessages(ctx, msgs)
	fmt.Printf("Recency-only: %d messages\n", len(result5))
}

// Example 8: Using Tokenizers
// Different model families use different tokenization. Use the appropriate tokenizer.
func ExampleTokenizers() {
	text := "Hello, world! This is a test of token counting."

	// OpenAI tokenizer (cl100k_base encoding approximation)
	openaiTokenizer := NewOpenAITokenizer()
	openaiTokens := openaiTokenizer.CountTokens(text)
	fmt.Printf("OpenAI tokens: %d\n", openaiTokens)

	// Anthropic tokenizer (Claude)
	anthropicTokenizer := NewAnthropicTokenizer()
	anthropicTokens := anthropicTokenizer.CountTokens(text)
	fmt.Printf("Anthropic tokens: %d\n", anthropicTokens)

	// Generic approximation (when exact tokenizer isn't available)
	genericTokenizer := NewApproxTokenizer()
	genericTokens := genericTokenizer.CountTokens(text)
	fmt.Printf("Generic tokens: %d\n", genericTokens)

	// Count tokens in a full message (includes metadata)
	msg := message.UserMessage(text)
	msgTokens := openaiTokenizer.CountTokensInMessage(msg)
	fmt.Printf("Message tokens (with metadata): %d\n", msgTokens)
}

// Example 9: Integrating Memory with an Agent
// This shows how to use memory with an agent (conceptual example).
func ExampleAgentIntegration() {
	ctx := context.Background()

	// Create a memory strategy
	mem := NewSlidingWindowMemory(20)

	// In a real scenario, you would pass this to an agent:
	// agent := agent.New(
	//     agent.WithMemory(mem),
	//     // ... other options
	// )

	// Simulate adding messages from conversation
	_ = mem.Add(ctx, message.UserMessage("My name is Bob"))
	_ = mem.Add(ctx, message.AssistantMessage("Hello Bob! How can I help you?"))
	_ = mem.Add(ctx, message.UserMessage("I need help with a project"))

	// Retrieve context for the next turn
	opts := WithLimit(10) // Get last 10 messages
	contextMsgs, err := mem.GetAll(ctx, opts)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Context for next turn: %d messages\n", len(contextMsgs))

	// In practice, you would pass these messages to the LLM provider
	// along with the new user message
}

// Example 10: Memory Persistence (Conceptual)
// While the current implementations are in-memory, the interface supports
// persistence. Here's how you might implement a persistent memory.
/*
func ExamplePersistentMemory() {
    type PersistentMemory struct {
        db *sql.DB
        mem *BufferMemory // Cache for fast access
    }

    func (pm *PersistentMemory) Add(ctx context.Context, msg message.Message) error {
        // Add to cache
        if err := pm.mem.Add(ctx, msg); err != nil {
            return err
        }

        // Persist to database
        _, err := pm.db.ExecContext(ctx,
            "INSERT INTO messages (role, content, created_at) VALUES (?, ?, ?)",
            msg.Role, msg.Text(), time.Now())
        return err
    }

    // ... implement other Memory interface methods
}
*/

// Example 11: Memory with Token-Aware Retrieval
// Combine memory strategies with token limits for optimal context management.
func ExampleTokenAwareRetrieval() {
	ctx := context.Background()

	// Create a memory with a large buffer
	mem := NewBufferMemory(0) // Unlimited message count

	// Add many messages
	for i := 0; i < 50; i++ {
		_ = mem.Add(ctx, message.UserMessage(fmt.Sprintf("Message %d with some content", i)))
	}

	// Retrieve with a token limit
	tokenizer := NewOpenAITokenizer()
	opts := WithMaxTokens(2000, tokenizer)

	msgs, err := mem.GetAll(ctx, opts)
	if err != nil {
		log.Fatal(err)
	}

	totalTokens := CountTokensInMessages(msgs, tokenizer)
	fmt.Printf("Retrieved %d messages within 2000 token limit\n", len(msgs))
	fmt.Printf("Total tokens: %d\n", totalTokens)
	// Output will show messages truncated to fit within 2000 tokens
}

// Example 12: Dynamic Memory Configuration
// You can dynamically adjust memory limits based on model requirements.
func ExampleDynamicConfiguration() {
	ctx := context.Background()

	tokenizer := NewOpenAITokenizer()

	// Start with a small limit
	mem := NewSlidingWindowMemoryWithTokens(500, tokenizer)

	// Add messages
	for i := 0; i < 10; i++ {
		_ = mem.Add(ctx, message.UserMessage("Test message"))
	}

	fmt.Printf("Initial: %d messages\n", mem.Size(ctx))

	// Dynamically increase the limit (e.g., switching to a larger model)
	mem.SetMaxTokens(2000, tokenizer)

	// Or change to message-based limit
	mem.SetMaxMessages(50)

	fmt.Printf("After adjustment: %d messages\n", mem.Size(ctx))
}

// Example 13: Memory Statistics and Monitoring
// Monitor memory usage and make informed decisions about context management.
func ExampleMemoryMonitoring() {
	ctx := context.Background()

	tokenizer := NewOpenAITokenizer()
	mem := NewSlidingWindowMemoryWithTokens(1000, tokenizer)

	// Add messages
	for i := 0; i < 20; i++ {
		_ = mem.Add(ctx, message.UserMessage("Sample message content"))
	}

	// Gather statistics
	msgCount := mem.Size(ctx)
	tokenCount := mem.GetCurrentTokenCount(ctx)
	isFull := mem.IsFull(ctx)

	fmt.Printf("Message count: %d\n", msgCount)
	fmt.Printf("Token count: %d\n", tokenCount)
	fmt.Printf("Is at capacity: %v\n", isFull)
	fmt.Printf("Average tokens per message: %.1f\n",
		float64(tokenCount)/float64(msgCount))
}

// Example 14: Using CompositeMemory Without Deduplication
// Sometimes you want to see duplicates from different memory sources.
func ExampleCompositeWithoutDedup() {
	ctx := context.Background()

	mem1 := NewBufferMemory(0)
	mem2 := NewBufferMemory(0)

	mem := NewCompositeMemory(
		MemoryWithPriority{Memory: mem1, Priority: 1, Name: "mem1"},
		MemoryWithPriority{Memory: mem2, Priority: 2, Name: "mem2"},
	)

	// Disable deduplication
	mem.SetDedup(false)

	// Add a message - it goes to both memories
	msg := message.UserMessage("Test message")
	_ = mem.Add(ctx, msg)

	// Retrieve - will see duplicates
	msgs, _ := mem.GetAll(ctx, DefaultGetOptions())
	fmt.Printf("Messages (with duplicates): %d\n", len(msgs))
	// Output: Messages (with duplicates): 2

	// Enable deduplication
	mem.SetDedup(true)

	msgs, _ = mem.GetAll(ctx, DefaultGetOptions())
	fmt.Printf("Messages (deduplicated): %d\n", len(msgs))
	// Output: Messages (deduplicated): 1
}

// Example 15: Practical Pattern - Conversation Session
// A complete example showing a typical conversation session with memory.
func ExampleConversationSession() {
	ctx := context.Background()

	// Set up memory and context management
	tokenizer := NewOpenAITokenizer()
	mem := NewSlidingWindowMemory(50)

	cw := GetModelContextWindow("gpt-4o")
	cm := NewContextManager(cw, tokenizer)

	// Simulate a conversation session
	conversation := []struct {
		role message.Role
		text string
	}{
		{message.RoleSystem, "You are a helpful coding assistant."},
		{message.RoleUser, "How do I reverse a string in Go?"},
		{message.RoleAssistant, "You can use the strings package..."},
		{message.RoleUser, "Can you show me an example?"},
		{message.RoleAssistant, "Sure! Here's an example..."},
		{message.RoleUser, "What about reversing a slice?"},
	}

	// Add messages to memory
	for _, turn := range conversation {
		_ = mem.Add(ctx, message.TextMessage(turn.role, turn.text))
	}

	// Get context for the next response
	allMsgs, _ := mem.GetAll(ctx, DefaultGetOptions())

	// Ensure context fits in the model's window
	contextMsgs := cm.TruncateMessages(ctx, allMsgs)

	// Calculate usage
	usage := cm.GetUsagePercentage(contextMsgs)
	fmt.Printf("Context messages: %d\n", len(contextMsgs))
	fmt.Printf("Token usage: %.1f%%\n", usage*100)

	// In a real agent, you would:
	// 1. Combine contextMsgs with the new user query
	// 2. Send to the LLM provider
	// 3. Get the response
	// 4. Add the response to memory
}

// Example 16: Memory with Custom Options
// Use GetOptions for fine-grained control over message retrieval.
func ExampleCustomGetOptions() {
	ctx := context.Background()

	mem := NewBufferMemory(0)

	// Add messages with different content lengths
	_ = mem.Add(ctx, message.UserMessage("Short"))
	_ = mem.Add(ctx, message.UserMessage("This is a much longer message that uses more tokens"))
	_ = mem.Add(ctx, message.UserMessage("Another message"))

	// Retrieve with both message and token limits
	tokenizer := NewApproxTokenizer()
	opts := GetOptions{
		Limit:     10,  // At most 10 messages
		MaxTokens: 100, // At most 100 tokens
		Tokenizer: tokenizer,
	}

	msgs, err := mem.GetAll(ctx, opts)
	if err != nil {
		log.Fatal(err)
	}

	actualTokens := CountTokensInMessages(msgs, tokenizer)
	fmt.Printf("Retrieved %d messages\n", len(msgs))
	fmt.Printf("Total tokens: %d (limit: 100)\n", actualTokens)
}
