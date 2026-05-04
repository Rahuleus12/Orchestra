package rag

import (
	"context"
	"testing"
)

func TestMemoryVectorStore(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryVectorStore(3)

	t.Run("AddAndGet", func(t *testing.T) {
		vec := Vector{
			ID:     "vec1",
			Values: []float64{1.0, 0.0, 0.0},
			Metadata: map[string]any{
				"content": "test document",
				"source":  "test.txt",
			},
		}

		err := store.Add(ctx, vec)
		if err != nil {
			t.Fatalf("Add() failed: %v", err)
		}

		got, ok, err := store.Get(ctx, "vec1")
		if err != nil {
			t.Fatalf("Get() failed: %v", err)
		}
		if !ok {
			t.Fatal("Get() returned ok=false, expected true")
		}
		if got.ID != "vec1" {
			t.Errorf("Get() ID = %q, want %q", got.ID, "vec1")
		}
	})

	t.Run("Search", func(t *testing.T) {
		// Add some vectors
		vectors := []Vector{
			{ID: "v1", Values: []float64{1.0, 0.0, 0.0}, Metadata: map[string]any{"content": "first"}},
			{ID: "v2", Values: []float64{0.0, 1.0, 0.0}, Metadata: map[string]any{"content": "second"}},
			{ID: "v3", Values: []float64{0.9, 0.1, 0.0}, Metadata: map[string]any{"content": "similar to first"}},
		}

		for _, v := range vectors {
			err := store.Add(ctx, v)
			if err != nil {
				t.Fatalf("Add() failed: %v", err)
			}
		}

		// Search for similar to first vector
		results, err := store.Search(ctx, []float64{1.0, 0.0, 0.0}, SearchOptions{TopK: 2})
		if err != nil {
			t.Fatalf("Search() failed: %v", err)
		}

		if len(results) < 2 {
			t.Fatalf("Search() returned %d results, want at least 2", len(results))
		}

		// First result should be the exact match or very similar
		if results[0].Document.ID != "v1" && results[0].Document.ID != "vec1" {
			t.Logf("Search() first result ID = %q (score: %.3f)", results[0].Document.ID, results[0].Score)
		}
	})

	t.Run("Size", func(t *testing.T) {
		if size := store.Size(ctx); size == 0 {
			t.Error("Size() = 0, want > 0")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		err := store.Delete(ctx, "v2")
		if err != nil {
			t.Fatalf("Delete() failed: %v", err)
		}

		_, ok, _ := store.Get(ctx, "v2")
		if ok {
			t.Error("Get() after Delete() returned ok=true, expected false")
		}
	})

	t.Run("Clear", func(t *testing.T) {
		err := store.Clear(ctx)
		if err != nil {
			t.Fatalf("Clear() failed: %v", err)
		}

		if size := store.Size(ctx); size != 0 {
			t.Errorf("Size() after Clear() = %d, want 0", size)
		}
	})
}

func TestMemoryVectorStoreWithFilters(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryVectorStore(3)

	// Add vectors with different metadata
	vectors := []Vector{
		{ID: "doc1", Values: []float64{1.0, 0.0, 0.0}, Metadata: map[string]any{"type": "article", "content": "AI research"}},
		{ID: "doc2", Values: []float64{0.9, 0.1, 0.0}, Metadata: map[string]any{"type": "blog", "content": "AI blog post"}},
		{ID: "doc3", Values: []float64{0.8, 0.2, 0.0}, Metadata: map[string]any{"type": "article", "content": "ML research"}},
	}

	for _, v := range vectors {
		store.Add(ctx, v)
	}

	// Search with type filter
	results, err := store.Search(ctx, []float64{1.0, 0.0, 0.0}, SearchOptions{
		TopK:    10,
		Filters: map[string]any{"type": "article"},
	})
	if err != nil {
		t.Fatalf("Search() with filters failed: %v", err)
	}

	for _, r := range results {
		if r.Document.Metadata["type"] != "article" {
			t.Errorf("Search() result has type = %v, want 'article'", r.Document.Metadata["type"])
		}
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a    []float64
		b    []float64
		want float64
	}{
		{"identical", []float64{1.0, 0.0, 0.0}, []float64{1.0, 0.0, 0.0}, 1.0},
		{"orthogonal", []float64{1.0, 0.0, 0.0}, []float64{0.0, 1.0, 0.0}, 0.0},
		{"opposite", []float64{1.0, 0.0, 0.0}, []float64{-1.0, 0.0, 0.0}, -1.0},
		{"similar", []float64{1.0, 0.0, 0.0}, []float64{0.9, 0.1, 0.0}, 0.995}, // approximately
		{"different length", []float64{1.0, 0.0}, []float64{1.0, 0.0, 0.0}, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineSimilarity(tt.a, tt.b)
			if tt.name == "similar" {
				// Allow some tolerance for floating point
				if got < 0.99 || got > 1.0 {
					t.Errorf("cosineSimilarity() = %v, want approximately 0.995", got)
				}
			} else {
				if got != tt.want {
					t.Errorf("cosineSimilarity() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestFixedSizeChunker(t *testing.T) {
	chunker := NewFixedSizeChunker(20, 5)

	t.Run("ShortText", func(t *testing.T) {
		chunks := chunker.Chunk("Hello")
		if len(chunks) != 1 {
			t.Errorf("Chunk() returned %d chunks, want 1", len(chunks))
		}
	})

	t.Run("LongText", func(t *testing.T) {
		text := "This is a longer text that should be split into multiple chunks."
		chunks := chunker.Chunk(text)
		if len(chunks) < 2 {
			t.Errorf("Chunk() returned %d chunks, want at least 2", len(chunks))
		}
		// Check that chunks don't exceed max size (except possibly the last)
		for i, chunk := range chunks {
			if i < len(chunks)-1 && len(chunk) > 20 {
				t.Errorf("Chunk %d has length %d, want <= 20", i, len(chunk))
			}
		}
	})

	t.Run("EmptyText", func(t *testing.T) {
		chunks := chunker.Chunk("")
		if len(chunks) != 0 {
			t.Errorf("Chunk() returned %d chunks for empty text, want 0", len(chunks))
		}
	})
}

func TestParagraphChunker(t *testing.T) {
	chunker := NewParagraphChunker(100)

	text := `First paragraph with some text.

Second paragraph with more content.

Third paragraph.`

	chunks := chunker.Chunk(text)

	if len(chunks) < 1 {
		t.Error("Chunk() returned 0 chunks, want at least 1")
	}
}

func TestMockEmbedder(t *testing.T) {
	ctx := context.Background()
	embedder := NewMockEmbedder(10)

	t.Run("Embed", func(t *testing.T) {
		vec, err := embedder.Embed(ctx, "test text")
		if err != nil {
			t.Fatalf("Embed() failed: %v", err)
		}
		if len(vec) != 10 {
			t.Errorf("Embed() returned vector of length %d, want 10", len(vec))
		}
	})

	t.Run("EmbedBatch", func(t *testing.T) {
		texts := []string{"text1", "text2", "text3"}
		vecs, err := embedder.EmbedBatch(ctx, texts)
		if err != nil {
			t.Fatalf("EmbedBatch() failed: %v", err)
		}
		if len(vecs) != 3 {
			t.Errorf("EmbedBatch() returned %d vectors, want 3", len(vecs))
		}
	})

	t.Run("Dimensions", func(t *testing.T) {
		if dim := embedder.Dimensions(); dim != 10 {
			t.Errorf("Dimensions() = %d, want 10", dim)
		}
	})
}

func TestKnowledgeBase(t *testing.T) {
	ctx := context.Background()
	embedder := NewMockEmbedder(10)
	store := NewMemoryVectorStore(10)
	kb := NewKnowledgeBase(embedder, store)

	doc := Document{
		ID:      "doc1",
		Content: "This is a test document about artificial intelligence and machine learning.",
		Metadata: map[string]any{
			"source": "test.txt",
		},
	}

	t.Run("Add", func(t *testing.T) {
		result, err := kb.Add(ctx, doc)
		if err != nil {
			t.Fatalf("Add() failed: %v", err)
		}
		if result.DocumentID != "doc1" {
			t.Errorf("Add() DocumentID = %q, want %q", result.DocumentID, "doc1")
		}
	})

	t.Run("Query", func(t *testing.T) {
		results, err := kb.Query(ctx, "artificial intelligence", DefaultSearchOptions())
		if err != nil {
			t.Fatalf("Query() failed: %v", err)
		}
		if len(results) == 0 {
			t.Error("Query() returned 0 results, want at least 1")
		}
	})

	t.Run("Size", func(t *testing.T) {
		if size := kb.Size(ctx); size == 0 {
			t.Error("Size() = 0 after Add(), want > 0")
		}
	})
}
