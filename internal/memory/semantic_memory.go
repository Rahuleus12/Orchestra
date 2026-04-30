package memory

import (
	"context"
	"math"
	"sort"
	"sync"

	"github.com/user/orchestra/internal/message"
)

// Embedding represents a vector representation of text.
type Embedding []float32

// EmbeddingProvider defines the interface for generating text embeddings.
// Implementations can use various embedding models (OpenAI, Cohere, etc.).
type EmbeddingProvider interface {
	// GenerateEmbedding creates an embedding vector for the given text.
	GenerateEmbedding(ctx context.Context, text string) (Embedding, error)

	// Dimension returns the dimension of the embedding vectors produced by this provider.
	Dimension() int
}

// semanticMessage stores a message along with its embedding.
type semanticMessage struct {
	message   message.Message
	embedding Embedding
	timestamp int64
}

// SimilarityScore represents a message with its similarity score to a query.
type SimilarityScore struct {
	Message message.Message
	Score   float32
}

// SemanticMemory stores messages and retrieves them based on semantic similarity
// using vector embeddings. This enables retrieval of messages that are
// conceptually related to a query, not just recency-based.
type SemanticMemory struct {
	mu                sync.RWMutex
	messages          []semanticMessage
	embeddingProvider EmbeddingProvider
	maxSize           int     // Maximum number of messages to store (0 = unlimited)
	topK              int     // Number of top results to return (default: 5)
	minScore          float32 // Minimum similarity score threshold (0.0 - 1.0)
	includeContent    bool    // Whether to include content in embedding generation
}

// NewSemanticMemory creates a new SemanticMemory with the specified embedding provider.
//
// Parameters:
//   - provider: The embedding provider to use for generating embeddings
//   - maxSize: Maximum number of messages to store (0 = unlimited)
//   - topK: Number of most similar messages to return (default: 5)
//   - minScore: Minimum similarity score (0.0 - 1.0, default: 0.0)
func NewSemanticMemory(provider EmbeddingProvider, maxSize, topK int, minScore float32) *SemanticMemory {
	if topK <= 0 {
		topK = 5
	}
	if minScore < 0 {
		minScore = 0
	}
	if minScore > 1 {
		minScore = 1
	}

	return &SemanticMemory{
		messages:          make([]semanticMessage, 0),
		embeddingProvider: provider,
		maxSize:           maxSize,
		topK:              topK,
		minScore:          minScore,
		includeContent:    true,
	}
}

// Add stores a message in memory along with its embedding.
func (m *SemanticMemory) Add(ctx context.Context, msg message.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate embedding for the message
	embedding, err := m.generateEmbedding(ctx, msg)
	if err != nil {
		return err
	}

	// Store the message with its embedding
	semanticMsg := semanticMessage{
		message:   msg.Clone(),
		embedding: embedding,
		timestamp: getCurrentTimestamp(),
	}

	m.messages = append(m.messages, semanticMsg)

	// Enforce size limit
	if m.maxSize > 0 && len(m.messages) > m.maxSize {
		// Remove the oldest message
		m.messages = m.messages[1:]
	}

	return nil
}

// generateEmbedding creates an embedding for a message.
// It combines the role and content (if enabled) into a single text string.
func (m *SemanticMemory) generateEmbedding(ctx context.Context, msg message.Message) (Embedding, error) {
	var text string

	// Include role in the embedding for better semantic understanding
	text += string(msg.Role) + ": "

	// Include content if enabled
	if m.includeContent {
		text += msg.Text()
	}

	// Include tool call information if present
	if msg.IsToolCall() && len(msg.ToolCalls) > 0 {
		text += " [tool_calls: "
		for _, call := range msg.ToolCalls {
			text += call.Function.Name + " "
		}
		text += "]"
	}

	// Generate the embedding
	embedding, err := m.embeddingProvider.GenerateEmbedding(ctx, text)
	if err != nil {
		return nil, err
	}

	return embedding, nil
}

// GetRelevant retrieves messages semantically relevant to the given input.
// It returns up to topK messages with similarity scores >= minScore.
func (m *SemanticMemory) GetRelevant(ctx context.Context, input string, opts GetOptions) ([]message.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.messages) == 0 {
		return []message.Message{}, nil
	}

	// Generate embedding for the input query
	queryEmbedding, err := m.embeddingProvider.GenerateEmbedding(ctx, input)
	if err != nil {
		return nil, err
	}

	// Calculate similarity scores for all messages
	scores := make([]SimilarityScore, 0, len(m.messages))
	for _, semMsg := range m.messages {
		score := cosineSimilarity(queryEmbedding, semMsg.embedding)
		if score >= m.minScore {
			scores = append(scores, SimilarityScore{
				Message: semMsg.message,
				Score:   score,
			})
		}
	}

	// Sort by similarity score (descending)
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score > scores[j].Score
	})

	// Take top K results
	topK := m.topK
	if opts.Limit > 0 && opts.Limit < topK {
		topK = opts.Limit
	}

	if len(scores) > topK {
		scores = scores[:topK]
	}

	// Extract messages
	result := make([]message.Message, len(scores))
	for i, score := range scores {
		result[i] = score.Message
	}

	// Apply token limit if specified
	if opts.MaxTokens > 0 && opts.Tokenizer != nil {
		result = TruncateToTokenLimit(result, opts.MaxTokens, opts.Tokenizer)
	}

	return result, nil
}

// GetAll retrieves all messages in memory, respecting the options.
// Messages are returned in chronological order (oldest first).
func (m *SemanticMemory) GetAll(ctx context.Context, opts GetOptions) ([]message.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Extract all messages
	msgs := make([]message.Message, len(m.messages))
	for i, semMsg := range m.messages {
		msgs[i] = semMsg.message
	}

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

	return msgs, nil
}

// Clear removes all messages from memory.
func (m *SemanticMemory) Clear(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages = make([]semanticMessage, 0)
	return nil
}

// Size returns the current number of messages.
func (m *SemanticMemory) Size(ctx context.Context) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.messages)
}

// GetTopK returns the configured number of top results to return.
func (m *SemanticMemory) GetTopK() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.topK
}

// SetTopK updates the number of top results to return.
func (m *SemanticMemory) SetTopK(topK int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if topK <= 0 {
		topK = 5
	}
	m.topK = topK
}

// GetMinScore returns the minimum similarity score threshold.
func (m *SemanticMemory) GetMinScore() float32 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.minScore
}

// SetMinScore updates the minimum similarity score threshold (0.0 - 1.0).
func (m *SemanticMemory) SetMinScore(score float32) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	m.minScore = score
}

// SetIncludeContent sets whether to include message content in embedding generation.
func (m *SemanticMemory) SetIncludeContent(include bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.includeContent = include
}

// cosineSimilarity calculates the cosine similarity between two embedding vectors.
// Returns a value between -1.0 and 1.0, where 1.0 means identical direction.
func cosineSimilarity(a, b Embedding) float32 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct float32
	var normA float32
	var normB float32

	for i := 0; i < len(a); i++ {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}

// GetSimilarityScores retrieves messages with their similarity scores to the input.
// This is useful for debugging or when you need to know the relevance scores.
func (m *SemanticMemory) GetSimilarityScores(ctx context.Context, input string) ([]SimilarityScore, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.messages) == 0 {
		return []SimilarityScore{}, nil
	}

	// Generate embedding for the input query
	queryEmbedding, err := m.embeddingProvider.GenerateEmbedding(ctx, input)
	if err != nil {
		return nil, err
	}

	// Calculate similarity scores for all messages
	scores := make([]SimilarityScore, 0, len(m.messages))
	for _, semMsg := range m.messages {
		score := cosineSimilarity(queryEmbedding, semMsg.embedding)
		scores = append(scores, SimilarityScore{
			Message: semMsg.message,
			Score:   score,
		})
	}

	// Sort by similarity score (descending)
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score > scores[j].Score
	})

	return scores, nil
}

// Reindex regenerates embeddings for all stored messages.
// This is useful if the embedding provider or configuration has changed.
func (m *SemanticMemory) Reindex(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.messages {
		embedding, err := m.generateEmbedding(ctx, m.messages[i].message)
		if err != nil {
			return err
		}
		m.messages[i].embedding = embedding
	}

	return nil
}

// GetEmbeddingProvider returns the current embedding provider.
func (m *SemanticMemory) GetEmbeddingProvider() EmbeddingProvider {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.embeddingProvider
}

// SetEmbeddingProvider updates the embedding provider.
// You may want to call Reindex() after changing the provider.
func (m *SemanticMemory) SetEmbeddingProvider(provider EmbeddingProvider) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.embeddingProvider = provider
}

// getCurrentTimestamp returns the current Unix timestamp in nanoseconds.
// This is used for tracking message order.
func getCurrentTimestamp() int64 {
	// In a real implementation, use time.Now().UnixNano()
	// For now, return a simple incrementing value based on message count
	return 0
}
