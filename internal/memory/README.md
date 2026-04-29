# Memory Package

The `memory` package provides flexible memory management for agents in the Orchestra framework, supporting various strategies for storing, retrieving, and managing conversation context with LLMs.

## Overview

Managing conversation context is critical for building effective AI agents. This package provides:

- **Multiple Memory Strategies**: Choose from simple buffers, sliding windows, semantic search, or composite approaches
- **Token-Aware Operations**: Accurate token counting for different model families (OpenAI, Anthropic, Google, Mistral)
- **Context Window Management**: Automatic truncation to stay within model limits
- **Extensible Design**: Easy to implement custom memory strategies and embedders

## Features

### Memory Implementations

#### BufferMemory
Simple in-memory buffer with optional size limit. Messages are stored in FIFO order.

**Best for:**
- Simple applications with short conversations
- Testing and prototyping
- When you just need a message cache

```go
mem := NewBufferMemory(100) // Store up to 100 messages
```

#### SlidingWindowMemory
Maintains a fixed-size window of the most recent messages. Supports both message count and token count limits.

**Best for:**
- Long-running conversations
- When only recent context matters
- Strict memory constraints

```go
// Limit by message count
mem := NewSlidingWindowMemory(50)

// Limit by token count
tokenizer := NewOpenAITokenizer()
mem := NewSlidingWindowMemoryWithTokens(4000, tokenizer)

// Both limits
mem := NewSlidingWindowMemoryFull(50, 4000, tokenizer)
```

#### SummaryMemory
Summarizes older messages using an LLM to compress context while preserving key information.

**Best for:**
- Long conversations with important historical context
- When you need to remember decisions and outcomes
- Limited context windows but need long-term memory

```go
// Requires a provider for summarization
mem := NewSummaryMemory(provider, "gpt-4", 100, 4000, tokenizer)
```

#### SemanticMemory
Uses vector embeddings to retrieve messages based on semantic similarity to a query.

**Best for:**
- Retrieval-augmented generation (RAG)
- Finding relevant historical context
- Building knowledge-based agents

```go
provider := NewMockEmbeddingProvider(384)
mem := NewSemanticMemory(provider, 1000, 5, 0.7)
// Returns top 5 messages with similarity score >= 0.7
```

#### CompositeMemory
Combines multiple memory strategies with priority ordering. Results are merged and optionally deduplicated.

**Best for:**
- Complex applications requiring multiple memory types
- Combining recent context with semantic search
- Flexible memory architectures

```go
mem := NewCompositeMemory(
    MemoryWithPriority{Memory: recentMem, Priority: 2, Name: "recent"},
    MemoryWithPriority{Memory: semanticMem, Priority: 1, Name: "semantic"},
)
```

### Tokenizers

Accurate token counting for different model families:

- **OpenAI**: cl100k_base encoding (GPT-3.5, GPT-4, etc.)
- **Anthropic**: Claude tokenizer
- **Google**: Gemini/PaLM tokenizer
- **Mistral**: Mistral tokenizer
- **Generic**: Approximation-based fallback

```go
tokenizer := NewTokenizer(ModelFamilyOpenAI)
count := tokenizer.CountTokens("Hello, world!")
```

### Context Window Management

Automatic context truncation with configurable strategies:

```go
cw := GetModelContextWindow("gpt-4")
cm := NewContextManager(cw, tokenizer)

// Set truncation strategy
cm.SetTruncationStrategy(TruncateOldestPreservingSystem)

// Truncate messages to fit in context window
truncated := cm.TruncateMessages(ctx, messages)
```

**Truncation Strategies:**
- `TruncateOldest`: Remove oldest messages first
- `TruncateOldestPreservingSystem`: Remove oldest but keep system messages
- `TruncateMiddle`: Remove from middle, keep beginning and end
- `TruncateSmallest`: Remove messages with fewest tokens first
- `TruncateRecency`: Keep only most recent messages

## API Reference

### Memory Interface

```go
type Memory interface {
    Add(ctx context.Context, msg message.Message) error
    GetRelevant(ctx context.Context, input string, opts GetOptions) ([]message.Message, error)
    GetAll(ctx context.Context, opts GetOptions) ([]message.Message, error)
    Clear(ctx context.Context) error
    Size(ctx context.Context) int
}
```

### GetOptions

```go
type GetOptions struct {
    Limit      int        // Maximum number of messages (0 = no limit)
    MaxTokens  int        // Maximum tokens (0 = no limit)
    Tokenizer  Tokenizer  // For token counting
}

// Helper functions
func DefaultGetOptions() GetOptions
func WithLimit(limit int) GetOptions
func WithMaxTokens(maxTokens int, tokenizer Tokenizer) GetOptions
```

### Tokenizer Interface

```go
type Tokenizer interface {
    CountTokens(text string) int
    CountTokensInMessage(msg message.Message) int
}
```

### Utility Functions

```go
// Count tokens in multiple messages
func CountTokensInMessages(msgs []message.Message, tokenizer Tokenizer) int

// Truncate messages to fit token limit (preserves system messages)
func TruncateToTokenLimit(msgs []message.Message, maxTokens int, tokenizer Tokenizer) []message.Message
```

## Usage Examples

### Basic Usage

```go
ctx := context.Background()

// Create a sliding window memory
mem := NewSlidingWindowMemory(20)

// Add messages
mem.Add(ctx, message.UserMessage("Hello!"))
mem.Add(ctx, message.AssistantMessage("Hi there!"))

// Retrieve all messages
msgs, _ := mem.GetAll(ctx, DefaultGetOptions())

// Retrieve with limit
msgs, _ = mem.GetAll(ctx, WithLimit(10))
```

### Token-Aware Retrieval

```go
tokenizer := NewOpenAITokenizer()
mem := NewBufferMemory(0) // Unlimited message count

// Add many messages
for i := 0; i < 100; i++ {
    mem.Add(ctx, message.UserMessage(fmt.Sprintf("Message %d", i)))
}

// Retrieve within token limit
opts := WithMaxTokens(2000, tokenizer)
msgs, _ := mem.GetAll(ctx, opts)

fmt.Printf("Retrieved %d messages within token limit\n", len(msgs))
```

### Semantic Search

```go
provider := NewMockEmbeddingProvider(384)
mem := NewSemanticMemory(provider, 100, 5, 0.6)

// Add knowledge
mem.Add(ctx, message.UserMessage("I love programming in Go"))
mem.Add(ctx, message.UserMessage("Go has great concurrency"))

// Query for relevant context
query := "What do you know about Go?"
msgs, _ := mem.GetRelevant(ctx, query, DefaultGetOptions())

// Get similarity scores
scores, _ := mem.GetSimilarityScores(ctx, query)
for _, score := range scores {
    fmt.Printf("Score: %.2f - %s\n", score.Score, score.Message.Text())
}
```

### Composite Memory

```go
// Combine recent messages with semantic search
recent := NewSlidingWindowMemory(10)
semantic := NewSemanticMemory(provider, 100, 5, 0.7)

mem := NewCompositeMemory(
    MemoryWithPriority{Memory: recent, Priority: 2, Name: "recent"},
    MemoryWithPriority{Memory: semantic, Priority: 1, Name: "semantic"},
)

// Messages go to all underlying memories
mem.Add(ctx, message.UserMessage("Important information"))

// Retrieve combines results from both
msgs, _ := mem.GetRelevant(ctx, "query", DefaultGetOptions())
```

### Context Window Management

```go
// Get context window for a model
cw := GetModelContextWindow("gpt-4o")

// Create context manager
tokenizer := NewOpenAITokenizer()
cm := NewContextManager(cw, tokenizer)

// Set up warnings
cm.SetWarningThreshold(0.9)
cm.SetWarningCallback(func(w *ContextWarning) {
    log.Printf("Context warning [%s]: %s", w.Level, w.Message)
})

// Truncate messages to fit
truncated := cm.TruncateMessages(ctx, longConversation)

// Check usage
usage := cm.GetUsagePercentage(truncated)
fmt.Printf("Context usage: %.1f%%\n", usage*100)
```

## Model Context Windows

Pre-configured context windows for common models:

| Model | Max Tokens |
|-------|------------|
| gpt-4o | 128000 |
| gpt-4o-mini | 128000 |
| o1 | 200000 |
| o1-mini | 128000 |
| o3-mini | 200000 |
| gpt-4-turbo | 128000 |
| gpt-4 | 8192 |
| gpt-4-32k | 32768 |
| gpt-3.5-turbo | 16385 |
| claude-sonnet-4-20250514 | 200000 |
| claude-opus-4-20250514 | 200000 |
| claude-3-5-sonnet-20241022 | 200000 |
| claude-3-5-haiku-20241022 | 200000 |
| claude-3-opus | 200000 |
| claude-3-sonnet | 200000 |
| claude-3-haiku | 200000 |

```go
// Get context window for a known model
cw := GetModelContextWindow("gpt-4o")

// Or create custom
cw := &ContextWindow{
    MaxTokens:          100000,
    ModelName:          "custom-model",
    SafeMargin:         0.05,
    SystemPromptTokens: 1000,
}
```

## Best Practices

### 1. Choose the Right Memory Strategy

- **Short conversations**: Use `BufferMemory` or `SlidingWindowMemory`
- **Need historical context**: Use `SummaryMemory` or `SemanticMemory`
- **Complex requirements**: Use `CompositeMemory` to combine strategies

### 2. Always Use Tokenizers for Token Limits

```go
// Good: Use tokenizer for accurate token counting
tokenizer := NewOpenAITokenizer()
mem := NewSlidingWindowMemoryWithTokens(4000, tokenizer)

// Bad: Only limit by message count
mem := NewSlidingWindowMemory(100) // May exceed token limit
```

### 3. Preserve System Messages

```go
// Use a strategy that preserves system messages
cm.SetTruncationStrategy(TruncateOldestPreservingSystem)

// Or use TruncateToTokenLimit which preserves system messages by default
truncated := TruncateToTokenLimit(msgs, maxTokens, tokenizer)
```

### 4. Monitor Context Usage

```go
// Set up warnings
cm.SetWarningThreshold(0.8) // Warn at 80%
cm.SetWarningCallback(func(w *ContextWarning) {
    log.Printf("Warning: %s", w.Message)
})

// Check usage before sending
usage := cm.GetUsagePercentage(messages)
if usage > 0.9 {
    log.Printf("Approaching context limit: %.1f%%", usage*100)
}
```

### 5. Use Appropriate Tokenizers

```go
// Use the correct tokenizer for your model
switch modelProvider {
case "openai":
    tokenizer = NewOpenAITokenizer()
case "anthropic":
    tokenizer = NewAnthropicTokenizer()
case "google":
    tokenizer = NewGoogleTokenizer()
default:
    tokenizer = NewApproxTokenizer() // Fallback
}
```

### 6. Handle Errors Gracefully

```go
msgs, err := mem.GetRelevant(ctx, query, opts)
if err != nil {
    // Log error but continue with partial results
    log.Printf("Semantic search failed: %v", err)
    // Fall back to GetAll
    msgs, err = mem.GetAll(ctx, opts)
}
```

### 7. Clear Memory When Needed

```go
// Clear between sessions
mem.Clear(ctx)

// Or implement session-based memory
func startSession() Memory {
    return NewSlidingWindowMemory(50)
}
```

## Testing

The package includes comprehensive tests. Run them with:

```bash
go test ./internal/memory/...
```

### Mock Embedding Provider

For testing without external dependencies, use the mock embedding provider:

```go
provider := NewMockEmbeddingProvider(384)
mem := NewSemanticMemory(provider, 100, 5, 0.7)
```

## Extending the Package

### Custom Memory Implementation

```go
type MyCustomMemory struct {
    // Your fields
}

func (m *MyCustomMemory) Add(ctx context.Context, msg message.Message) error {
    // Your implementation
}

func (m *MyCustomMemory) GetRelevant(ctx context.Context, input string, opts GetOptions) ([]message.Message, error) {
    // Your implementation
}

// ... implement other Memory interface methods
```

### Custom Embedding Provider

```go
type MyEmbeddingProvider struct {
    // Your fields
}

func (p *MyEmbeddingProvider) GenerateEmbedding(ctx context.Context, text string) (Embedding, error) {
    // Your implementation
}

func (p *MyEmbeddingProvider) Dimension() int {
    return 768 // or your embedding dimension
}
```

### Custom Tokenizer

```go
type MyTokenizer struct {
    // Your fields
}

func (t *MyTokenizer) CountTokens(text string) int {
    // Your implementation
}

func (t *MyTokenizer) CountTokensInMessage(msg message.Message) int {
    // Your implementation
}
```

## Performance Considerations

- **BufferMemory** and **SlidingWindowMemory** are O(1) for add/get operations
- **SemanticMemory** requires embedding generation (O(1) per message, but with API latency)
- **SummaryMemory** requires LLM calls for summarization (use sparingly)
- **CompositeMemory** aggregates operations from all underlying memories

For high-performance scenarios:
- Use `BufferMemory` or `SlidingWindowMemory` when possible
- Consider caching embeddings in `SemanticMemory`
- Batch operations when adding many messages

## Persistence

The current implementations are in-memory. To add persistence, implement the `Memory` interface with a database backend. See `examples.go` for a conceptual example.

## License

Part of the Orchestra project. See project LICENSE for details.

## Contributing

Contributions are welcome! Please ensure:
- All tests pass
- New features include tests
- Documentation is updated
- Code follows project conventions