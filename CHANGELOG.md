# Changelog

All notable changes to Orchestra will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Phase 10 Production Readiness implementation
- Comprehensive architecture documentation (`docs/ARCHITECTURE.md`)
- Contributing guidelines (`docs/CONTRIBUTING.md`)
- Example code files for major features
- Shared `internal/provider/httpx` package for HTTP client/transport construction
  across all providers (proxy support, HTTP/2, streaming-safe timeouts,
  connection pooling)
- `MemoryCacheStore.Len()` for cache-size monitoring
- Workflow validation in the HTTP API (cycles now return 400, not 500)
- Debug logging for malformed SSE events in Anthropic/Cohere providers (set log
  level to debug to inspect dropped stream chunks)
- Benchmarks for the caching Generate path and brute-force vector search
- `SessionStore` is now safe for concurrent use (mutex-guarded map and
  active-session pointer)

### Fixed
- **Agent memory storage**: stored the system prompt instead of the user's
  input in memory, polluting recall and creating a feedback loop
- **Cache key collisions**: the caching middleware omitted tool-result content
  and content blocks (images/files) from the cache key, returning stale
  responses when only tool results differed
- **Circuit breaker**: counted client-initiated cancellation
  (`context.Canceled`/`DeadlineExceeded`) as provider failures, allowing a
  single cancelling client to trip the breaker for everyone
- **Retry**: transport-level failures (connection refused, DNS, TLS — wrapped as
  `ProviderError` with `StatusCode: 0`) were never retried; now treated as
  transient
- **Workflow cancellation status**: a cancelled workflow was reported as
  `StatusFailed` instead of `StatusCancelled` because the engine checked the
  caller's parent context rather than the workflow context
- **Bus handler panics**: a panicking subscriber crashed the entire process;
  now recovered, logged, and counted
- **Bus subscription cancellation**: `Unsubscribe()`/`Close()` could not
  interrupt in-flight handlers because the cancellable context was discarded
- **Mailbox lost wakeups**: with multiple blocked receivers and a burst of
  messages, a receiver could stall even though messages were queued
  (capacity-1 signal channel dropped wakeups)
- **Tool execution panics**: a panicking tool crashed the agent's execution
  loop; now recovered into an error tool result
- **Memory cache leak**: expired entries were never evicted from
  `MemoryCacheStore`, causing unbounded growth in long-running services
- **Memory backing-array leaks**: `BufferMemory`/`SlidingWindowMemory` reslicing
  retained dropped messages (including large `FileData` payloads) in the
  backing array
- **Workflow ID collisions and non-random jitter**: derived from wall-clock
  nanoseconds; now use `crypto/rand` and `math/rand/v2`
- **Instrumentation nil-deref**: `WrapGenerate` panicked if a provider returned
  `(nil, nil)`
- **Non-atomic session writes**: TUI session saves could corrupt files on
  crash; now write-then-rename atomically
- **Vector store O(n²) Add**: `containsOrdered` scanned the ordered slice per
  insert; now O(1) via the vectors map

### Changed
- All 7 providers use a dedicated streaming HTTP client with no overall
  timeout (the previous single client's `Timeout` would kill long SSE streams);
  the custom transports now honor `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY`
- SSE handlers in the HTTP server now stop promptly on client disconnect and
  clear the server-level `WriteTimeout` so legitimate long streams aren't killed
- CORS wildcard (`*`) now returns the literal `*` instead of reflecting the
  request origin (prevents credentialed cross-origin requests by default)
- Server logs a prominent warning at startup when authentication is disabled
- Memory errors in the agent layer are now logged rather than silently dropped
- `MemoryVectorStore.Search` respects context cancellation and pre-allocates
  based on `TopK`
- Retry middleware now explicitly detects `net.Error` (timeouts/temporary)
  and `io.EOF` for generic (non-`ProviderError`) errors rather than blindly
  retrying all generic errors

## [0.9.0] - 2024-12-15

### Added
- **Phase 9: Advanced Patterns**
  - Retrieval-Augmented Generation (RAG) support
  - Self-reflection and refinement agent pattern
  - Planning and re-planning agent capabilities
  - Human-in-the-loop workflow support
  - Multi-model ensemble for consensus decisions
  - SHA-tracked session messages with immutable journal
  - Message compaction strategies (threshold, token budget)
  - Session journal with hash-linked message chain
  - JournalMemory adapter for agent integration
  - LookupMessageTool for message retrieval

### Changed
- Enhanced message system with hash-based tracking
- Improved memory system with compaction support

## [0.8.0] - 2024-12-01

### Added
- **Phase 8: Observability & Operations**
  - Structured logging with `log/slog`
  - OpenTelemetry tracing integration
  - Prometheus metrics exporter
  - Health check endpoints
  - Custom slog handler attributes
  - Correlation ID middleware

### Changed
- Enhanced error reporting with context
- Improved logging throughout all packages

## [0.7.0] - 2024-11-15

### Added
- **Phase 7: Memory & Context Management**
  - Memory interface with multiple strategies
  - BufferMemory for simple conversations
  - SlidingWindowMemory for limited context
  - SummaryMemory for long conversations
  - SemanticMemory with embedding support
  - CompositeMemory for combining strategies
  - Token counting and context window management
  - Mock embedding provider for testing

### Changed
- Agent memory integration via functional options

## [0.6.0] - 2024-11-01

### Added
- **Phase 6: Tool System & Function Calling**
  - Tool interface and registry
  - Namespace-based tool organization
  - Tool execution with JSON schema validation
  - Agent-as-Tool adapter pattern
  - ToolFunc for simple function tools
  - Tool middleware (logging, metrics)
  - Builder pattern for complex tools
  - Search and execution utilities

### Changed
- Provider interface with tool calling support

## [0.5.0] - 2024-10-15

### Added
- **Phase 5: Inter-Agent Communication**
  - Message bus interface and implementation
  - Agent mailbox for direct messaging
  - Broadcast patterns for announcements
  - Request-reply pattern with correlation
  - Pub-sub topic-based routing

### Changed
- Enhanced agent communication capabilities

## [0.4.0] - 2024-10-01

### Added
- **Phase 4: Orchestration Engine**
  - DAG-based workflow definition
  - Workflow builder with fluent API
  - Execution engine with parallel step processing
  - Sequential pipeline pattern
  - Parallel fan-out/fan-in pattern
  - Router pattern for conditional branching
  - Debate pattern for multi-agent discussion
  - Hierarchical delegation pattern
  - Workflow context and state management
  - Retry policies with exponential backoff

### Changed
- Enhanced agent integration with orchestration

## [0.3.0] - 2024-09-15

### Added
- **Phase 3: Agent Runtime & Lifecycle**
  - Agent definition with functional options
  - Agent execution loop with tool calling
  - Agent streaming for real-time output
  - AgentResult with usage and duration tracking
  - AgentEvent types for event-driven architectures
  - Prompt template system with variable substitution
  - Agent cloning for parallel execution

### Changed
- Improved tool integration with agents

## [0.2.0] - 2024-09-01

### Added
- **Phase 2: Provider Integrations**
  - OpenAI provider (GPT-4o, GPT-4-turbo, GPT-3.5-turbo)
  - Anthropic provider (Claude 3.5 Sonnet, Claude 3 Opus)
  - Google Gemini provider
  - Ollama provider for local models
  - Mistral provider
  - Cohere provider
  - Provider middleware layer:
    - Retry with exponential backoff
    - Rate limiting
    - Request/response logging
    - Response caching
    - Circuit breaker

### Changed
- Enhanced provider interface with streaming
- Added model capabilities metadata

## [0.1.0] - 2024-08-15

### Added
- **Phase 1: Foundation & Core Abstractions**
  - Project bootstrapping with Go module
  - CLI entry point with cobra
  - Core message types (Message, ContentBlock, Conversation)
  - Provider interface definition
  - GenerateRequest and GenerateResult types
  - StreamEvent types for streaming responses
  - GenerateOptions with functional options
  - Provider registry with alias resolution
  - Configuration system with YAML support
  - Mock provider for testing
  - Makefile with comprehensive targets
  - golangci-lint configuration
  - CI/CD pipelines (GitHub Actions)
  - Docker support (Dockerfile, docker-compose)

---

## Version History

| Version | Date | Phase | Status |
|---------|------|-------|--------|
| 0.1.0 | 2024-08-15 | Foundation | ✅ Complete |
| 0.2.0 | 2024-09-01 | Provider Integrations | ✅ Complete |
| 0.3.0 | 2024-09-15 | Agent Runtime | ✅ Complete |
| 0.4.0 | 2024-10-01 | Orchestration Engine | ✅ Complete |
| 0.5.0 | 2024-10-15 | Inter-Agent Communication | ✅ Complete |
| 0.6.0 | 2024-11-01 | Tool System | ✅ Complete |
| 0.7.0 | 2024-11-15 | Memory & Context | ✅ Complete |
| 0.8.0 | 2024-12-01 | Observability | ✅ Complete |
| 0.9.0 | 2024-12-15 | Advanced Patterns | ✅ Complete |
| 1.0.0 | TBD | Production Readiness | 🚧 In Progress |

---

## Upgrade Guide

### 0.8.x → 0.9.x

- Memory strategies now support compaction
- Use `JournalMemory` for hash-tracked sessions
- Import `internal/memory` for new types

### 0.7.x → 0.8.x

- Observability is now enabled by default
- Add tracing configuration to your config file
- Use `observability` package for custom metrics

### 0.6.x → 0.7.x

- Memory is now required for persistent conversations
- Use `memory.NewBufferMemory()` for simple cases
- Agent constructor now accepts memory options

### 0.5.x → 0.6.x

- Tools use namespace-based registration
- Update tool registration to use `MustRegister`
- Tool definitions now require JSON schema

### 0.4.x → 0.5.x

- Message bus is now available for inter-agent communication
- Use `bus.New()` to create a bus instance
- Agents can subscribe to topics

### 0.3.x → 0.4.x

- Workflows now use DAG-based execution
- Update workflow definitions to use `NewBuilder`
- Parallel execution is now automatic for independent steps

### 0.2.x → 0.3.x

- Agent interface has changed
- Use functional options for agent creation
- Streaming is now supported via `Stream()` method

### 0.1.x → 0.2.x

- Provider interface now requires `Stream()` method
- Update custom providers to implement streaming
- Middleware chain is now required for providers
