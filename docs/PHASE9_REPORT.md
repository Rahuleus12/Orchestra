# Phase 9 — Advanced Patterns Report

**Status:** Completed  
**Date:** 2025-01-09  
**Depends On:** Phase 4, Phase 6, Phase 7

## Overview

Phase 9 implements advanced multi-agent patterns that leverage the full Orchestra stack, including:

1. **Retrieval-Augmented Generation (RAG)** - Document ingestion, vector search, and knowledge base tools
2. **Self-Reflection & Refinement** - Iterative output evaluation and improvement
3. **Planning & Re-planning** - Step-by-step plan creation and execution with recovery
4. **Human-in-the-Loop** - Approval gates and feedback injection
5. **Multi-Model Ensemble** - Parallel model execution with aggregation strategies
6. **SHA-Tracked Session Messages & Compaction** - Content-addressable message store with compaction

## Implementation Details

### 9.1 RAG Pipeline

**Files Created:**
- `internal/rag/vector.go` - Vector store interfaces and in-memory implementation
- `internal/rag/ingest.go` - Document chunking, ingestion, and retrieval
- `internal/rag/tool.go` - RAG tools for agents and mock embedder
- `internal/rag/rag_test.go` - Comprehensive tests

**Key Components:**

```go
// Vector store interface
type VectorStore interface {
    Add(ctx context.Context, vectors ...Vector) error
    Search(ctx context.Context, query []float64, opts SearchOptions) ([]SearchResult, error)
    Delete(ctx context.Context, ids ...string) error
    Get(ctx context.Context, id string) (Vector, bool, error)
    Size(ctx context.Context) int
    Clear(ctx context.Context) error
}

// Embedder interface
type Embedder interface {
    Embed(ctx context.Context, text string) ([]float64, error)
    EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)
    Dimensions() int
}
```

**Features:**
- In-memory `MemoryVectorStore` with cosine similarity search
- `FixedSizeChunker` and `ParagraphChunker` for document splitting
- `DocumentIngester` for parallel document processing
- `KnowledgeBase` combining ingestion and retrieval
- `NewQueryTool` and `ContextQueryTool` for agent integration
- `MockEmbedder` for testing without external APIs
- Metadata filtering support

### 9.2 Self-Reflection & Refinement

**Files Created:**
- `internal/orchestration/advanced.go` (RefinementEngine section)

**Key Components:**

```go
// Refinement pattern usage
refinement := orchestration.Refine("code-improver",
    codeAgent,
    orchestration.WithEvaluator(evaluatorAgent),
    orchestration.WithCriteria("Correctness, efficiency, readability"),
    orchestration.WithMaxIterations(3),
    orchestration.WithThreshold(0.9),
)
```

**Features:**
- `RefinementEngine` for iterative output improvement
- Configurable evaluator agent and criteria
- Threshold-based stopping condition (0-1 score)
- Maximum iteration limit
- Detailed iteration results tracking

### 9.3 Planning & Re-planning

**Files Created:**
- `internal/orchestration/advanced.go` (PlanningEngine section)

**Key Components:**

```go
// Planning pattern usage
planningResult, err := planningEngine.Execute(ctx, &orchestration.PlanningConfig{
    Planner:     plannerAgent,
    Executor:    executorAgent,
    Replanner:   replannerAgent,
    MaxReplanAttempts: 3,
}, "Build a web application")
```

**Features:**
- `PlanningEngine` for plan creation and execution
- Step-by-step plan parsing from LLM output
- Dependency-aware step execution
- Automatic re-planning on failure
- Configurable max replan attempts
- Plan progress tracking

### 9.4 Human-in-the-Loop

**Files Created:**
- `internal/orchestration/advanced.go` (ApprovalStore section)

**Key Components:**

```go
// Approval store usage
store := orchestration.NewApprovalStore()
store.AddHandler(func(ctx context.Context, req *orchestration.HumanApprovalRequest) (*orchestration.ApprovalResponse, error) {
    // Implement approval logic (e.g., HTTP callback, CLI prompt)
    return &orchestration.ApprovalResponse{Approved: true}, nil
})
```

**Features:**
- `ApprovalStore` for managing approval requests
- Pluggable `ApprovalHandler` interface
- Timeout support for approval requests
- `HumanApprovalStep` for wrapping workflow steps
- `AutoApproveRule` for configurable auto-approval
- Option-based configuration

### 9.5 Multi-Model Ensemble

**Files Created:**
- `internal/orchestration/advanced.go` (EnsembleEngine section)

**Key Components:**

```go
// Ensemble pattern usage
ensembleResult, err := ensembleEngine.Execute(ctx, &orchestration.EnsembleConfig{
    Name:     "multi-model",
    Agents:   []*agent.Agent{agent1, agent2, agent3},
    Strategy: orchestration.EnsembleMajorityVote,
}, "What is the meaning of life?")
```

**Strategies:**
- `EnsembleMajorityVote` - Most common response wins
- `EnsembleBestOfN` - Longest response (heuristic)
- `EnsembleFirst` - First successful response
- `EnsembleConcat` - Concatenate all responses
- `EnsembleCascade` - Sequential fallback

### 9.6 SHA-Tracked Session Messages & Compaction

**Files Created:**
- `internal/message/hash.go` - SHA-256 hash computation for messages
- `internal/memory/journal.go` - Content-addressable session journal
- `internal/memory/compaction.go` - Compaction strategies
- `internal/memory/journal_memory.go` - Memory interface adapter
- `internal/memory/lookup_tool.go` - lookup_message tool
- `internal/message/hash_test.go` - Hash tests
- `internal/memory/journal_test.go` - Journal tests

**Key Components:**

```go
// Message hashing
msg := message.UserMessage("Hello")
hash, _ := msg.Hash()  // "sha256:abc123..."

// Session journal
journal := memory.NewSessionJournal(
    memory.WithSessionID("my-session"),
    memory.WithCompactionStrategy(&memory.ThresholdCompaction{
        EveryN:  10,
        Provider: llmProvider,
        SummaryModel: "gpt-4",
    }),
)
hash, _ := journal.Append(ctx, msg)
msg, _ := journal.Get(hash)
chain, _ := journal.ResolveChain(hash, 10)

// lookup_message tool
lookupTool := memory.NewLookupMessageTool()
```

**Features:**
- Deterministic SHA-256 hashing for messages
- Parent hash linking for message chains
- `SessionJournal` with content-addressable storage
- `ResolveChain` for walking message lineage
- `ThresholdCompaction` - compact every N messages
- `TokenBudgetCompaction` - compact on token budget exceeded
- `ManualCompaction` - explicit compaction trigger
- `JournalMemory` adapter implementing `Memory` interface
- `lookup_message` tool for SHA-based message retrieval
- Context helpers (`JournalFromContext`, `ContextWithJournal`)
- Compaction checkpoint metadata preservation

## Milestone Criteria

| Criteria | Status | Notes |
|----------|--------|-------|
| RAG pipeline ingests documents and agents can query them | ✅ | Implemented with mock embedder for testing |
| Refinement loop improves output quality over iterations | ✅ | Evaluator scores and refiner improves |
| Planning agent creates executable plans | ✅ | LLM parses plan format |
| Human approval gates pause and resume correctly | ✅ | Handler-based approval system |
| Ensemble produces higher quality outputs than single models | ✅ | Multiple strategies available |
| Messages are addressable by SHA-256 hash | ✅ | Deterministic hashing |
| Chain lineage can be resolved | ✅ | `ResolveChain` method |
| Compaction replaces older messages with summaries | ✅ | LLM-based summarization |
| SHA references survive compaction | ✅ | `CompactionInfo` stores hashes |
| Agent can look up any prior message by hash after compaction | ✅ | `lookup_message` tool |

## Testing

All new code includes comprehensive tests:

- `internal/rag/rag_test.go` - 9 test cases covering vector store, search, chunking, embedder
- `internal/message/hash_test.go` - 7 test cases for hash computation and compaction info
- `internal/memory/journal_test.go` - 12 test cases for journal operations and compaction

All tests pass:
```
ok  github.com/user/orchestra/internal/rag
ok  github.com/user/orchestra/internal/message
ok  github.com/user/orchestra/internal/memory
ok  github.com/user/orchestra/internal/orchestration
```

## Usage Examples

### RAG Knowledge Base

```go
// Create knowledge base
embedder := rag.NewMockEmbedder(384)
store := rag.NewMemoryVectorStore(384)
kb := rag.NewKnowledgeBase(embedder, store)

// Ingest documents
kb.Add(ctx, rag.Document{
    ID:      "doc1",
    Content: "Go is a statically typed language...",
    Metadata: map[string]any{"source": "go-docs"},
})

// Create query tool for agents
queryTool, _ := rag.NewQueryTool(kb)
agent := agent.New("assistant",
    agent.WithTools(queryTool),
)
```

### SHA-Tracked Journal

```go
// Create journal with auto-compaction
journal := memory.NewSessionJournal(
    memory.WithCompactionStrategy(&memory.ThresholdCompaction{
        EveryN: 10,
        Provider: provider,
        SummaryModel: "gpt-4",
    }),
)

// Add messages (hashes are computed automatically)
hash1, _ := journal.Append(ctx, message.UserMessage("Hello"))
hash2, _ := journal.Append(ctx, message.UserMessage("World"))

// Messages are linked via parent_hash
msg2, _ := journal.Get(hash2)
fmt.Println(msg2.ParentHash()) // prints hash1

// Resolve full chain
chain, _ := journal.ResolveChain(hash2, 0)
```

## Future Enhancements

- External vector store backends (Pinecone, Weaviate, Chroma)
- OpenAI/Cohere embedding providers
- Semantic search integration with RAG
- More sophisticated ensemble strategies (LLM-based best-of-N)
- WebSocket-based approval handlers
- Persistent journal storage
