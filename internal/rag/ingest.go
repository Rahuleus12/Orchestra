package rag

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
)

// ChunkingStrategy defines how documents are split into chunks.
type ChunkingStrategy interface {
	// Chunk splits text into smaller pieces.
	Chunk(text string) []string
}

// ChunkingOptions configures the chunking process.
type ChunkingOptions struct {
	// ChunkSize is the maximum number of characters per chunk.
	// Default is 1000.
	ChunkSize int

	// ChunkOverlap is the number of characters to overlap between chunks.
	// Default is 200.
	ChunkOverlap int

	// Separator is the string used to split text (before applying size limits).
	// Default is "\n\n" (paragraph separator).
	Separator string
}

// DefaultChunkingOptions returns ChunkingOptions with sensible defaults.
func DefaultChunkingOptions() ChunkingOptions {
	return ChunkingOptions{
		ChunkSize:    1000,
		ChunkOverlap: 200,
		Separator:    "\n\n",
	}
}

// ---------------------------------------------------------------------------
// Chunking Strategies
// ---------------------------------------------------------------------------

// FixedSizeChunker splits text into fixed-size chunks with optional overlap.
type FixedSizeChunker struct {
	ChunkSize    int
	ChunkOverlap int
}

// NewFixedSizeChunker creates a chunker with the specified size and overlap.
func NewFixedSizeChunker(chunkSize, chunkOverlap int) *FixedSizeChunker {
	if chunkSize <= 0 {
		chunkSize = 1000
	}
	if chunkOverlap < 0 {
		chunkOverlap = 0
	}
	if chunkOverlap >= chunkSize {
		chunkOverlap = chunkSize / 2
	}

	return &FixedSizeChunker{
		ChunkSize:    chunkSize,
		ChunkOverlap: chunkOverlap,
	}
}

// Chunk splits text into fixed-size chunks with overlap.
func (c *FixedSizeChunker) Chunk(text string) []string {
	if len(text) == 0 {
		return nil
	}

	if len(text) <= c.ChunkSize {
		return []string{text}
	}

	var chunks []string
	start := 0

	for start < len(text) {
		end := start + c.ChunkSize
		if end > len(text) {
			end = len(text)
		}

		chunks = append(chunks, text[start:end])
		start += c.ChunkSize - c.ChunkOverlap

		// Avoid infinite loop if overlap is too large
		if start >= end && end < len(text) {
			start = end
		}
	}

	return chunks
}

// ParagraphChunker splits text by paragraphs, then merges small paragraphs
// into chunks that fit within the specified size.
type ParagraphChunker struct {
	MaxChunkSize int
	Separator    string
}

// NewParagraphChunker creates a paragraph-based chunker.
func NewParagraphChunker(maxChunkSize int) *ParagraphChunker {
	if maxChunkSize <= 0 {
		maxChunkSize = 1000
	}
	return &ParagraphChunker{
		MaxChunkSize: maxChunkSize,
		Separator:    "\n\n",
	}
}

// Chunk splits text by paragraphs, merging small paragraphs together.
func (c *ParagraphChunker) Chunk(text string) []string {
	if len(text) == 0 {
		return nil
	}

	// Split by separator
	paragraphs := strings.Split(text, c.Separator)

	var chunks []string
	var currentChunk strings.Builder

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		// If adding this paragraph would exceed the limit and we have content,
		// save the current chunk and start a new one.
		if currentChunk.Len() > 0 && currentChunk.Len()+len(c.Separator)+len(para) > c.MaxChunkSize {
			chunks = append(chunks, currentChunk.String())
			currentChunk.Reset()
		}

		// Handle paragraphs that exceed the limit on their own
		if len(para) > c.MaxChunkSize {
			// Save any current content first
			if currentChunk.Len() > 0 {
				chunks = append(chunks, currentChunk.String())
				currentChunk.Reset()
			}

			// Use fixed-size chunking for this large paragraph
			fixedChunker := NewFixedSizeChunker(c.MaxChunkSize, 0)
			subChunks := fixedChunker.Chunk(para)
			chunks = append(chunks, subChunks...)
			continue
		}

		// Add paragraph to current chunk
		if currentChunk.Len() > 0 {
			currentChunk.WriteString(c.Separator)
		}
		currentChunk.WriteString(para)
	}

	// Don't forget the last chunk
	if currentChunk.Len() > 0 {
		chunks = append(chunks, currentChunk.String())
	}

	return chunks
}

// ---------------------------------------------------------------------------
// Document Ingestion Pipeline
// ---------------------------------------------------------------------------

// IngestionResult holds the result of ingesting a document.
type IngestionResult struct {
	// DocumentID is the ID of the ingested document.
	DocumentID string

	// ChunkCount is the number of chunks created.
	ChunkCount int

	// ChunkIDs are the IDs of the created chunks.
	ChunkIDs []string

	// Error holds any error that occurred during ingestion.
	Error error
}

// DocumentIngester handles the ingestion of documents into the RAG system.
type DocumentIngester struct {
	mu              sync.Mutex
	embedder        Embedder
	store           VectorStore
	chunker         ChunkingStrategy
	idGenerator     func() string
}

// IngesterOption configures a DocumentIngester.
type IngesterOption func(*DocumentIngester)

// WithChunker sets the chunking strategy.
func WithChunker(chunker ChunkingStrategy) IngesterOption {
	return func(d *DocumentIngester) {
		d.chunker = chunker
	}
}

// WithIDGenerator sets the ID generator for documents and chunks.
func WithIDGenerator(gen func() string) IngesterOption {
	return func(d *DocumentIngester) {
		d.idGenerator = gen
	}
}

// NewDocumentIngester creates a new document ingester.
func NewDocumentIngester(embedder Embedder, store VectorStore, opts ...IngesterOption) *DocumentIngester {
	d := &DocumentIngester{
		embedder:    embedder,
		store:       store,
		chunker:     NewParagraphChunker(1000),
		idGenerator: generateID,
	}

	for _, opt := range opts {
		opt(d)
	}

	return d
}

// Ingest processes a document and adds it to the vector store.
func (d *DocumentIngester) Ingest(ctx context.Context, doc Document) (*IngestionResult, error) {
	// Generate document ID if not provided
	if doc.ID == "" {
		doc.ID = d.idGenerator()
	}

	// Chunk the document
	chunks := d.chunker.Chunk(doc.Content)
	if len(chunks) == 0 {
		return &IngestionResult{
			DocumentID: doc.ID,
			ChunkCount: 0,
		}, nil
	}

	// Generate embeddings for all chunks
	embeddings, err := d.embedder.EmbedBatch(ctx, chunks)
	if err != nil {
		return &IngestionResult{
			DocumentID: doc.ID,
			Error:      fmt.Errorf("failed to generate embeddings: %w", err),
		}, err
	}

	// Create vectors for each chunk
	vectors := make([]Vector, 0, len(chunks))
	chunkIDs := make([]string, 0, len(chunks))

	for i, chunk := range chunks {
		if i >= len(embeddings) {
			break
		}

		chunkID := fmt.Sprintf("%s-chunk-%d", doc.ID, i)
		chunkIDs = append(chunkIDs, chunkID)

		// Create metadata for the chunk
		metadata := make(map[string]any)
		for k, v := range doc.Metadata {
			metadata[k] = v
		}
		metadata["document_id"] = doc.ID
		metadata["chunk_index"] = i
		metadata["content"] = chunk

		vectors = append(vectors, Vector{
			ID:       chunkID,
			Values:   embeddings[i],
			Metadata: metadata,
		})
	}

	// Store vectors
	if err := d.store.Add(ctx, vectors...); err != nil {
		return &IngestionResult{
			DocumentID: doc.ID,
			Error:      fmt.Errorf("failed to store vectors: %w", err),
		}, err
	}

	return &IngestionResult{
		DocumentID: doc.ID,
		ChunkCount: len(chunks),
		ChunkIDs:   chunkIDs,
	}, nil
}

// IngestBatch processes multiple documents concurrently.
func (di *DocumentIngester) IngestBatch(ctx context.Context, docs []Document) []*IngestionResult {
	results := make([]*IngestionResult, len(docs))
	var wg sync.WaitGroup

	for i, doc := range docs {
		wg.Add(1)
		go func(idx int, d Document) {
			defer wg.Done()
			result, _ := di.Ingest(ctx, d)
			results[idx] = result
		}(i, doc)
	}

	wg.Wait()
	return results
}

// ---------------------------------------------------------------------------
// RAG Retriever
// ---------------------------------------------------------------------------

// Retriever provides retrieval capabilities for the RAG system.
type Retriever struct {
	embedder Embedder
	store    VectorStore
}

// NewRetriever creates a new RAG retriever.
func NewRetriever(embedder Embedder, store VectorStore) *Retriever {
	return &Retriever{
		embedder: embedder,
		store:    store,
	}
}

// Retrieve finds documents relevant to the query.
func (r *Retriever) Retrieve(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
	// Generate embedding for the query
	queryEmbedding, err := r.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}

	// Search the vector store
	return r.store.Search(ctx, queryEmbedding, opts)
}

// RetrieveWithContext retrieves documents and formats them as context for an LLM.
func (r *Retriever) RetrieveWithContext(ctx context.Context, query string, opts SearchOptions) (string, error) {
	results, err := r.Retrieve(ctx, query, opts)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "No relevant documents found.", nil
	}

	var sb strings.Builder
	sb.WriteString("Relevant documents:\n\n")

	for i, result := range results {
		sb.WriteString(fmt.Sprintf("--- Document %d (similarity: %.2f) ---\n", i+1, result.Score))
		sb.WriteString(result.Document.Content)
		sb.WriteString("\n\n")
	}

	return sb.String(), nil
}

// ---------------------------------------------------------------------------
// RAG Knowledge Base
// ---------------------------------------------------------------------------

// KnowledgeBase combines ingestion and retrieval into a single convenient API.
type KnowledgeBase struct {
	ingester  *DocumentIngester
	retriever *Retriever
	store     VectorStore
	embedder  Embedder
}

// KnowledgeBaseOption configures a KnowledgeBase.
type KnowledgeBaseOption func(*KnowledgeBase)

// WithChunkingOptions sets the chunking options for the knowledge base.
func WithChunkingOptions(opts ChunkingOptions) KnowledgeBaseOption {
	return func(kb *KnowledgeBase) {
		chunker := NewParagraphChunker(opts.ChunkSize)
		kb.ingester = NewDocumentIngester(kb.embedder, kb.store, WithChunker(chunker))
	}
}

// NewKnowledgeBase creates a new RAG knowledge base.
func NewKnowledgeBase(embedder Embedder, store VectorStore, opts ...KnowledgeBaseOption) *KnowledgeBase {
	kb := &KnowledgeBase{
		embedder: embedder,
		store:    store,
	}

	kb.ingester = NewDocumentIngester(embedder, store)
	kb.retriever = NewRetriever(embedder, store)

	for _, opt := range opts {
		opt(kb)
	}

	return kb
}

// Add adds a document to the knowledge base.
func (kb *KnowledgeBase) Add(ctx context.Context, doc Document) (*IngestionResult, error) {
	return kb.ingester.Ingest(ctx, doc)
}

// Query searches the knowledge base for relevant documents.
func (kb *KnowledgeBase) Query(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
	return kb.retriever.Retrieve(ctx, query, opts)
}

// QueryWithContext searches and returns formatted context for an LLM.
func (kb *KnowledgeBase) QueryWithContext(ctx context.Context, query string, opts SearchOptions) (string, error) {
	return kb.retriever.RetrieveWithContext(ctx, query, opts)
}

// Store returns the underlying vector store.
func (kb *KnowledgeBase) Store() VectorStore {
	return kb.store
}

// Embedder returns the underlying embedder.
func (kb *KnowledgeBase) Embedder() Embedder {
	return kb.embedder
}

// Size returns the number of documents in the knowledge base.
func (kb *KnowledgeBase) Size(ctx context.Context) int {
	return kb.store.Size(ctx)
}

// Clear removes all documents from the knowledge base.
func (kb *KnowledgeBase) Clear(ctx context.Context) error {
	return kb.store.Clear(ctx)
}

// generateID generates a unique identifier using SHA-256 hash.
func generateID() string {
	b := make([]byte, 16)
	// Use crypto/rand for unique IDs
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:])[:16]
}
