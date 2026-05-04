// Package rag provides Retrieval-Augmented Generation capabilities,
// including document ingestion, embedding, and vector search.
package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"sync"
)

// Vector represents an embedding vector with associated metadata.
type Vector struct {
	// ID is a unique identifier for this vector.
	ID string `json:"id"`

	// Values are the floating-point embedding values.
	Values []float64 `json:"values"`

	// Metadata contains arbitrary key-value pairs associated with this vector.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Document represents a chunk of text that has been embedded.
type Document struct {
	// ID is a unique identifier for this document.
	ID string `json:"id"`

	// Content is the text content of the document.
	Content string `json:"content"`

	// Metadata contains arbitrary key-value pairs (source, page, etc.).
	Metadata map[string]any `json:"metadata,omitempty"`

	// Embedding is the vector representation of the document.
	Embedding []float64 `json:"embedding,omitempty"`
}

// SearchOptions specifies constraints for vector search.
type SearchOptions struct {
	// TopK is the maximum number of results to return.
	// Default is 5.
	TopK int

	// MinSimilarity is the minimum cosine similarity threshold.
	// Results below this threshold are filtered out.
	// Range: -1.0 to 1.0, default is 0.0.
	MinSimilarity float64

	// Filters restricts results to documents matching all specified metadata.
	// Only documents whose metadata contains all key-value pairs are returned.
	Filters map[string]any
}

// SearchResult represents a single search result.
type SearchResult struct {
	// Document is the matched document.
	Document Document

	// Score is the similarity score (higher is more similar).
	Score float64
}

// Embedder is the interface for generating embeddings from text.
type Embedder interface {
	// Embed generates an embedding vector for the given text.
	Embed(ctx context.Context, text string) ([]float64, error)

	// EmbedBatch generates embeddings for multiple texts in a single call.
	EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)

	// Dimensions returns the dimensionality of the embedding vectors.
	Dimensions() int
}

// VectorStore is the interface for storing and searching vectors.
type VectorStore interface {
	// Add stores one or more vectors in the store.
	Add(ctx context.Context, vectors ...Vector) error

	// Search finds the most similar vectors to the query vector.
	Search(ctx context.Context, query []float64, opts SearchOptions) ([]SearchResult, error)

	// Delete removes vectors by their IDs.
	Delete(ctx context.Context, ids ...string) error

	// Get retrieves a vector by its ID.
	Get(ctx context.Context, id string) (Vector, bool, error)

	// Size returns the number of vectors in the store.
	Size(ctx context.Context) int

	// Clear removes all vectors from the store.
	Clear(ctx context.Context) error
}

// DefaultSearchOptions returns SearchOptions with sensible defaults.
func DefaultSearchOptions() SearchOptions {
	return SearchOptions{
		TopK:          5,
		MinSimilarity: 0.0,
		Filters:       nil,
	}
}

// ---------------------------------------------------------------------------
// In-Memory Vector Store
// ---------------------------------------------------------------------------

// MemoryVectorStore is an in-memory implementation of VectorStore.
// It is suitable for development, testing, and small datasets.
type MemoryVectorStore struct {
	mu      sync.RWMutex
	vectors map[string]Vector
	ordered []string // preserves insertion order
	dimSize int      // expected vector dimensionality
}

// NewMemoryVectorStore creates a new in-memory vector store.
// dimSize is the expected dimensionality of vectors; if 0, any dimension is accepted.
func NewMemoryVectorStore(dimSize int) *MemoryVectorStore {
	return &MemoryVectorStore{
		vectors: make(map[string]Vector),
		ordered: make([]string, 0),
		dimSize: dimSize,
	}
}

// Add stores vectors in the memory store.
func (s *MemoryVectorStore) Add(ctx context.Context, vectors ...Vector) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, v := range vectors {
		if s.dimSize > 0 && len(v.Values) != s.dimSize {
			return fmt.Errorf("vector %q has dimension %d, expected %d", v.ID, len(v.Values), s.dimSize)
		}
		s.vectors[v.ID] = v
		// Track insertion order
		if !s.containsOrdered(v.ID) {
			s.ordered = append(s.ordered, v.ID)
		}
	}

	return nil
}

// containsOrdered checks if an ID is already in the ordered list.
func (s *MemoryVectorStore) containsOrdered(id string) bool {
	for _, existing := range s.ordered {
		if existing == id {
			return true
		}
	}
	return false
}

// Search finds the most similar vectors using cosine similarity.
func (s *MemoryVectorStore) Search(ctx context.Context, query []float64, opts SearchOptions) ([]SearchResult, error) {
	if opts.TopK <= 0 {
		opts.TopK = DefaultSearchOptions().TopK
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]SearchResult, 0, len(s.vectors))

	for _, v := range s.vectors {
		// Apply metadata filters
		if !matchesFilters(v.Metadata, opts.Filters) {
			continue
		}

		score := cosineSimilarity(query, v.Values)

		// Apply minimum similarity threshold
		if score < opts.MinSimilarity {
			continue
		}

		results = append(results, SearchResult{
			Document: Document{
				ID:        v.ID,
				Content:   fmt.Sprintf("%v", v.Metadata["content"]),
				Metadata:  v.Metadata,
				Embedding: v.Values,
			},
			Score: score,
		})
	}

	// Sort by score (highest first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Limit to TopK
	if len(results) > opts.TopK {
		results = results[:opts.TopK]
	}

	return results, nil
}

// Delete removes vectors by their IDs.
func (s *MemoryVectorStore) Delete(ctx context.Context, ids ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	idSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
		delete(s.vectors, id)
	}

	// Rebuild ordered list without deleted IDs
	filtered := make([]string, 0, len(s.ordered))
	for _, id := range s.ordered {
		if _, exists := idSet[id]; !exists {
			filtered = append(filtered, id)
		}
	}
	s.ordered = filtered

	return nil
}

// Get retrieves a vector by its ID.
func (s *MemoryVectorStore) Get(ctx context.Context, id string) (Vector, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.vectors[id]
	return v, ok, nil
}

// Size returns the number of vectors in the store.
func (s *MemoryVectorStore) Size(ctx context.Context) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.vectors)
}

// Clear removes all vectors from the store.
func (s *MemoryVectorStore) Clear(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.vectors = make(map[string]Vector)
	s.ordered = make([]string, 0)
	return nil
}

// cosineSimilarity computes the cosine similarity between two vectors.
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// matchesFilters checks if metadata matches all specified filter criteria.
func matchesFilters(metadata, filters map[string]any) bool {
	if len(filters) == 0 {
		return true
	}

	for key, value := range filters {
		metaValue, exists := metadata[key]
		if !exists {
			return false
		}
		if !equalValues(metaValue, value) {
			return false
		}
	}

	return true
}

// equalValues checks if two values are equal, supporting basic type comparisons.
func equalValues(a, b any) bool {
	// Handle string comparisons
	aStr, aIsStr := a.(string)
	bStr, bIsStr := b.(string)
	if aIsStr && bIsStr {
		return aStr == bStr
	}

	// Handle JSON marshaling for complex types
	aJSON, _ := json.Marshal(a)
	bJSON, _ := json.Marshal(b)
	return string(aJSON) == string(bJSON)
}
