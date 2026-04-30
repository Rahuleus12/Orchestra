package memory

import (
	"context"
	"hash/fnv"
	"math"
	"unicode"
)

// MockEmbeddingProvider is a simple embedding provider for testing.
// It generates deterministic embeddings based on text hashing, without
// requiring an external API or model.
type MockEmbeddingProvider struct {
	dimension int
	seed      uint32
}

// NewMockEmbeddingProvider creates a new mock embedding provider.
// The dimension specifies the size of the embedding vectors.
// Common values are 384 (small), 768 (medium), or 1536 (large).
func NewMockEmbeddingProvider(dimension int) *MockEmbeddingProvider {
	return &MockEmbeddingProvider{
		dimension: dimension,
		seed:      42, // Default seed for reproducibility
	}
}

// NewMockEmbeddingProviderWithSeed creates a new mock embedding provider
// with a specific seed for deterministic behavior.
func NewMockEmbeddingProviderWithSeed(dimension int, seed uint32) *MockEmbeddingProvider {
	return &MockEmbeddingProvider{
		dimension: dimension,
		seed:      seed,
	}
}

// GenerateEmbedding creates an embedding vector for the given text.
// The embedding is generated deterministically using a hash-based approach.
// Similar texts will produce similar embeddings (to a limited degree).
func (m *MockEmbeddingProvider) GenerateEmbedding(ctx context.Context, text string) (Embedding, error) {
	embedding := make(Embedding, m.dimension)

	// Use FNV hash for base values
	h := fnv.New32a()
	h.Write([]byte(text))
	hash := h.Sum32()

	// Generate embedding values using the hash and text characteristics
	for i := 0; i < m.dimension; i++ {
		// Combine hash with position to create varied values
		combined := uint64(hash)*uint64(i+1) + uint64(m.seed)

		// Create a pseudo-random float in range [-1, 1]
		// Use sine to create smooth variations
		value := math.Sin(float64(combined) * 0.01)

		// Add some influence from text content
		if i < len(text) {
			r := rune(text[i])
			// Normalize rune to roughly [-0.5, 0.5]
			charInfluence := float64(r%256)/512.0 - 0.5
			value = (value + charInfluence) / 2
		}

		embedding[i] = float32(value)
	}

	// Normalize the embedding to unit length
	m.normalize(embedding)

	return embedding, nil
}

// normalize normalizes an embedding vector to unit length.
func (m *MockEmbeddingProvider) normalize(embedding Embedding) {
	var norm float32
	for _, v := range embedding {
		norm += v * v
	}
	norm = float32(math.Sqrt(float64(norm)))

	if norm > 0 {
		for i := range embedding {
			embedding[i] /= norm
		}
	}
}

// Dimension returns the dimension of the embedding vectors produced by this provider.
func (m *MockEmbeddingProvider) Dimension() int {
	return m.dimension
}

// GetSeed returns the seed used for embedding generation.
func (m *MockEmbeddingProvider) GetSeed() uint32 {
	return m.seed
}

// SetSeed updates the seed for embedding generation.
// This will affect all future embeddings generated.
func (m *MockEmbeddingProvider) SetSeed(seed uint32) {
	m.seed = seed
}

// DeterministicTextEmbedding is a helper function that creates a more
// semantically-aware embedding by considering word-level features.
// This is still a mock but provides better similarity for related words.
func (m *MockEmbeddingProvider) DeterministicTextEmbedding(ctx context.Context, text string) (Embedding, error) {
	embedding := make(Embedding, m.dimension)

	// Analyze text characteristics
	wordCount := countWords(text)
	avgWordLength := calculateAvgWordLength(text)
	letterCount := countLetters(text)

	// Create base hash
	h := fnv.New32a()
	h.Write([]byte(text))
	baseHash := h.Sum32()

	for i := 0; i < m.dimension; i++ {
		// Base value from hash and position
		positionFactor := float64(i) / float64(m.dimension)
		value := math.Sin(float64(baseHash+uint32(i)) * 0.01)

		// Add text characteristics influence
		value += float64(wordCount%10) * 0.01 * math.Cos(positionFactor*math.Pi)
		value += float64(avgWordLength) * 0.05 * math.Sin(positionFactor*2*math.Pi)
		value += float64(letterCount%20) * 0.02 * math.Cos(positionFactor*3*math.Pi)

		// Add character n-gram influence
		if i < len(text)-1 {
			ngram := text[i:min(i+3, len(text))]
			ngramHash := simpleHash(ngram)
			value += float64(ngramHash%100) * 0.01
		}

		embedding[i] = float32(value)
	}

	// Normalize
	m.normalize(embedding)

	return embedding, nil
}

// countWords returns the number of words in the text.
func countWords(text string) int {
	count := 0
	inWord := false
	for _, r := range text {
		if unicode.IsSpace(r) {
			if inWord {
				count++
				inWord = false
			}
		} else {
			inWord = true
		}
	}
	if inWord {
		count++
	}
	return count
}

// calculateAvgWordLength returns the average word length.
func calculateAvgWordLength(text string) float64 {
	words := 0
	totalLen := 0
	currentLen := 0

	for _, r := range text {
		if unicode.IsSpace(r) {
			if currentLen > 0 {
				words++
				totalLen += currentLen
				currentLen = 0
			}
		} else if unicode.IsLetter(r) {
			currentLen++
		}
	}

	if currentLen > 0 {
		words++
		totalLen += currentLen
	}

	if words == 0 {
		return 0
	}
	return float64(totalLen) / float64(words)
}

// countLetters returns the number of letters in the text.
func countLetters(text string) int {
	count := 0
	for _, r := range text {
		if unicode.IsLetter(r) {
			count++
		}
	}
	return count
}

// simpleHash creates a simple hash from a string.
func simpleHash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

// min returns the minimum of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// PredefinedMockProviders provides common mock embedding providers for testing.
var PredefinedMockProviders = struct {
	Small  *MockEmbeddingProvider // 384 dimensions
	Medium *MockEmbeddingProvider // 768 dimensions
	Large  *MockEmbeddingProvider // 1536 dimensions
}{
	Small:  NewMockEmbeddingProvider(384),
	Medium: NewMockEmbeddingProvider(768),
	Large:  NewMockEmbeddingProvider(1536),
}
