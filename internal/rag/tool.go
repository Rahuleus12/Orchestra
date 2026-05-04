package rag

import (
	"context"
	"fmt"

	"github.com/user/orchestra/internal/tool"
)

// QueryInput is the input for the RAG query tool.
type QueryInput struct {
	// Query is the search query.
	Query string `json:"query" description:"The search query to find relevant documents"`

	// TopK is the maximum number of results to return (default: 5).
	TopK int `json:"top_k,omitempty" description:"Maximum number of results to return"`

	// MinSimilarity is the minimum similarity threshold (default: 0.0).
	MinSimilarity float64 `json:"min_similarity,omitempty" description:"Minimum similarity threshold (0.0-1.0)"`
}

// QueryOutput is the output of the RAG query tool.
type QueryOutput struct {
	// Results is the list of search results.
	Results []QueryResultItem `json:"results"`

	// Context is the formatted context for LLM consumption.
	Context string `json:"context"`
}

// QueryResultItem represents a single query result.
type QueryResultItem struct {
	// Content is the document content.
	Content string `json:"content"`

	// Score is the similarity score.
	Score float64 `json:"score"`

	// Source is the document source (if available).
	Source string `json:"source,omitempty"`
}

// NewQueryTool creates a tool that allows agents to query a knowledge base.
func NewQueryTool(kb *KnowledgeBase, opts ...tool.BuilderOption) (tool.Tool, error) {
	return tool.New("rag_query",
		append([]tool.BuilderOption{
			tool.WithDescription("Search the knowledge base for relevant documents. Returns documents that match the query along with their similarity scores."),
			tool.WithInputSchema[QueryInput](),
			tool.WithHandler[QueryInput, QueryOutput](func(ctx context.Context, input QueryInput) (QueryOutput, error) {
				// Set defaults
				if input.TopK <= 0 {
					input.TopK = 5
				}

				// Create search options
				opts := SearchOptions{
					TopK:          input.TopK,
					MinSimilarity: input.MinSimilarity,
				}

				// Query the knowledge base
				results, err := kb.Query(ctx, input.Query, opts)
				if err != nil {
					return QueryOutput{}, fmt.Errorf("query failed: %w", err)
				}

				// Convert results
				items := make([]QueryResultItem, 0, len(results))
				for _, r := range results {
					source := ""
					if src, ok := r.Document.Metadata["source"].(string); ok {
						source = src
					}

					items = append(items, QueryResultItem{
						Content: r.Document.Content,
						Score:   r.Score,
						Source:  source,
					})
				}

				// Get formatted context
				context, err := kb.QueryWithContext(ctx, input.Query, opts)
				if err != nil {
					context = fmt.Sprintf("Error formatting context: %v", err)
				}

				return QueryOutput{
					Results: items,
					Context: context,
				}, nil
			}),
		}, opts...)...)
}

// MustQueryTool is like NewQueryTool but panics on error.
func MustQueryTool(kb *KnowledgeBase, opts ...tool.BuilderOption) tool.Tool {
	t, err := NewQueryTool(kb, opts...)
	if err != nil {
		panic(fmt.Sprintf("rag.MustQueryTool: %v", err))
	}
	return t
}

// ---------------------------------------------------------------------------
// Context-Aware Query Tool
// ---------------------------------------------------------------------------

// ContextQueryTool creates a tool that returns RAG context formatted for
// direct injection into an LLM prompt.
func ContextQueryTool(kb *KnowledgeBase, opts ...tool.BuilderOption) (tool.Tool, error) {
	return tool.New("rag_context",
		append([]tool.BuilderOption{
			tool.WithDescription("Retrieve relevant context from the knowledge base. Returns formatted text suitable for inclusion in prompts."),
			tool.WithInputSchema[QueryInput](),
			tool.WithStringHandler[QueryInput](func(ctx context.Context, input QueryInput) (string, error) {
				if input.TopK <= 0 {
					input.TopK = 5
				}

				opts := SearchOptions{
					TopK:          input.TopK,
					MinSimilarity: input.MinSimilarity,
				}

				return kb.QueryWithContext(ctx, input.Query, opts)
			}),
		}, opts...)...)
}

// MustContextQueryTool is like ContextQueryTool but panics on error.
func MustContextQueryTool(kb *KnowledgeBase, opts ...tool.BuilderOption) tool.Tool {
	t, err := ContextQueryTool(kb, opts...)
	if err != nil {
		panic(fmt.Sprintf("rag.MustContextQueryTool: %v", err))
	}
	return t
}

// ---------------------------------------------------------------------------
// Knowledge Base Context Helper
// ---------------------------------------------------------------------------

// KnowledgeBaseFromContext extracts a KnowledgeBase from context.
// This is useful for tools that need access to the knowledge base.
func KnowledgeBaseFromContext(ctx context.Context) (*KnowledgeBase, bool) {
	kb, ok := ctx.Value(knowledgeBaseContextKey{}).(*KnowledgeBase)
	return kb, ok
}

// ContextWithKnowledgeBase adds a KnowledgeBase to context.
func ContextWithKnowledgeBase(ctx context.Context, kb *KnowledgeBase) context.Context {
	return context.WithValue(ctx, knowledgeBaseContextKey{}, kb)
}

type knowledgeBaseContextKey struct{}

// ---------------------------------------------------------------------------
// Mock Embedder for Testing
// ---------------------------------------------------------------------------

// MockEmbedder is a simple embedder that generates deterministic vectors.
// It is useful for testing and development without requiring an embedding API.
type MockEmbedder struct {
	dim int
}

// NewMockEmbedder creates a new mock embedder with the specified dimensions.
func NewMockEmbedder(dimensions int) *MockEmbedder {
	return &MockEmbedder{
		dim: dimensions,
	}
}

// Embed generates a deterministic embedding based on the input text.
func (e *MockEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	vec := make([]float64, e.dim)
	for i := 0; i < e.dim; i++ {
		// Create a simple hash-based embedding
		hash := 0
		for _, c := range text {
			hash = hash*31 + int(c) + i
		}
		// Normalize to [0, 1]
		vec[i] = float64((hash%1000)-500) / 500.0
	}
	return vec, nil
}

// EmbedBatch generates embeddings for multiple texts.
func (e *MockEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	embeddings := make([][]float64, len(texts))
	for i, text := range texts {
		var err error
		embeddings[i], err = e.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
	}
	return embeddings, nil
}

// Dimensions returns the dimensionality of the embedding vectors.
func (e *MockEmbedder) Dimensions() int {
	return e.dim
}

// Compile-time interface checks
var _ Embedder = (*MockEmbedder)(nil)
var _ VectorStore = (*MemoryVectorStore)(nil)
var _ ChunkingStrategy = (*FixedSizeChunker)(nil)
var _ ChunkingStrategy = (*ParagraphChunker)(nil)
