# Orchestra — CLI Coding Tool & Multi-Agent AI Orchestration Engine

## Project Plan

**Version:** 1.0.0-draft
**Date:** 2026-04-18
**Status:** Planning

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Goals & Non-Goals](#2-goals--non-goals)
3. [Architecture Overview](#3-architecture-overview)
4. [Project Structure](#4-project-structure)
5. [Phase 1 — Foundation & Core Abstractions](#5-phase-1--foundation--core-abstractions)
6. [Phase 2 — Provider Integrations](#6-phase-2--provider-integrations)
7. [Phase 3 — Agent Runtime & Lifecycle](#7-phase-3--agent-runtime--lifecycle)
8. [Phase 4 — Orchestration Engine](#8-phase-4--orchestration-engine)
9. [Phase 5 — Inter-Agent Communication](#9-phase-5--inter-agent-communication)
10. [Phase 6 — Tool System & Function Calling](#10-phase-6--tool-system--function-calling)
11. [Phase 7 — Memory & Context Management](#11-phase-7--memory--context-management)
12. [Phase 8 — Observability & Operations](#12-phase-8--observability--operations)
13. [Phase 9 — Advanced Patterns](#13-phase-9--advanced-patterns)
14. [Phase 10 — Production Readiness](#14-phase-10--production-readiness)
15. [Technical Decision Records](#15-technical-decision-records)
16. [Risk Assessment](#16-risk-assessment)
17. [Success Metrics](#17-success-metrics)

---

## 1. Executive Summary

Orchestra is a Go-based **CLI coding tool** that assists developers with code generation, refactoring, debugging, and exploration — powered by multiple AI providers (OpenAI, Anthropic, Google Gemini, Ollama, Mistral, Cohere, etc.) and models. Beyond single-turn coding assistance, Orchestra also supports **multi-agent workflows**, enabling complex orchestration patterns including sequential pipelines, parallel fan-out/fan-in, dynamic routing, debate loops, and hierarchical delegation where multiple specialized agents collaborate on a task.

The system is designed as a CLI-first tool with a library-first core, making it both a powerful developer companion on the command line and an embeddable framework in existing Go applications. An optional standalone server mode allows deployment as a microservice.

---

## 2. Goals & Non-Goals

### Goals

- **CLI coding assistant**: First-class command-line interface for code generation, refactoring, debugging, and codebase exploration.
- **Multi-agent workflows**: Multiple specialized agents that can collaborate on complex tasks via sequential pipelines, parallel fan-out/fan-in, dynamic routing, and hierarchical delegation.
- **Provider-agnostic**: Uniform Go interface across all LLM providers and models.
- **Composable agents**: Agents that can be combined into arbitrarily complex workflows.
- **First-class Go idioms**: Interfaces, context propagation, error wrapping, goroutine-based concurrency.
- **Extensible**: Easy to add new providers, tools, memory backends, and orchestration patterns.
- **Observable**: Built-in tracing, metrics, and structured logging from day one.
- **Production-grade**: Connection pooling, rate limiting, retry logic, graceful shutdown.
- **Testable**: Mock providers, deterministic testing helpers, and integration test suites.
- **Streaming-native**: Full support for streaming tokens from all providers.

### Non-Goals

- Building our own LLM or foundation model.
- Supporting non-Go client languages (a REST/gRPC gateway may come later).
- A visual drag-and-drop workflow UI (future consideration, not in scope).
- Multi-tenant SaaS platform features (auth, billing, RBAC).

---

## 3. Architecture Overview

### High-Level Component Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                         CLI Layer                            │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐  │
│  │ orchestra    │  │ orchestra    │  │  orchestra       │  │
│  │ chat (REPL)  │  │ run (one-shot│  │  pipeline (multi │  │
│  │              │  │  coding task)│  │  -agent workflow) │  │
│  └──────┬───────┘  └──────┬───────┘  └────────┬─────────┘  │
└─────────┼─────────────────┼───────────────────┼────────────┘
          │                 │                   │
┌─────────┴─────────────────┴───────────────────┴────────────┐
│                      Application / Library                  │
│                                                             │
│  ┌────────────┐  ┌──────────────────────────────────┐      │
│  │  Workflow   │  │         Orchestration Engine      │      │
│  │  Builder    │  │  (DAG executor, scheduler, router)│      │
│  └──────┬─────┘  └──────────┬───────────────────────┘      │
│         │                   │                               │
│  ┌──────┴───────────────────┴────────────────────┐         │
│  │              Agent Runtime                      │         │
│  │  (lifecycle, prompt assembly, tool dispatch)   │         │
│  └──────────────────┬────────────────────────────┘         │
│                     │                                       │
│  ┌──────────────────┴────────────────────────────┐         │
│  │             Provider Layer                      │         │
│  │  ┌──────┐ ┌─────────┐ ┌───────┐ ┌──────────┐  │         │
│  │  │OpenAI│ │Anthropic│ │Gemini │ │ Ollama   │  │         │
│  │  └──────┘ └─────────┘ └───────┘ └──────────┘  │         │
│  │  ┌──────┐ ┌─────────┐ ┌─────────────────────┐ │         │
│  │  │Mistral│ │Cohere  │ │ Custom / HTTP       │ │         │
│  │  └──────┘ └─────────┘ └─────────────────────┘ │         │
│  └─────────────────────────────────────────────────┘         │
│                                                             │
│  ┌───────────────┐  ┌──────────────┐  ┌──────────────┐     │
│  │  Tool System   │  │    Memory     │  │   Message     │     │
│  │  (functions)   │  │   (context)   │  │     Bus       │     │
│  └───────────────┘  └──────────────┘  └──────────────┘     │
│                                                             │
│  ┌───────────────────────────────────────────────────┐     │
│  │            Observability (trace, metrics, logs)    │     │
│  └───────────────────────────────────────────────────┘     │
└─────────────────────────────────────────────────────────────┘
```

### Key Design Principles

1. **Interface-driven design** — Every major component is defined as a Go interface.
2. **Context propagation** — `context.Context` flows through every call for cancellation, timeouts, and tracing.
3. **Immutable message types** — Messages, configurations, and workflow definitions are value types.
4. **Concurrency-safe** — All shared state uses proper synchronization; agents are independently schedulable.
5. **Fail-fast with graceful degradation** — Errors are surfaced immediately; partial results are recoverable.

---

## 4. Project Structure

```
Orchestra/
├── cmd/
│   └── orchestra/                # CLI and server entry point
│       └── main.go
├── docs/
│   ├── PLAN.md                   # This file
│   ├── ARCHITECTURE.md           # Detailed architecture docs
│   ├── ADR/                      # Architecture Decision Records
│   │   ├── 001-provider-interface.md
│   │   ├── 002-streaming-design.md
│   │   └── 003-workflow-dsl.md
│   └── examples/                 # Usage examples
│       ├── simple-agent/
│       ├── multi-provider/
│       ├── debate-pattern/
│       └── hierarchical/
├── internal/
│   ├── agent/                    # Agent runtime and lifecycle
│   │   ├── agent.go
│   │   ├── options.go
│   │   └── agent_test.go
│   ├── message/                  # Message types and conversation history
│   │   ├── message.go
│   │   ├── role.go
│   │   └── message_test.go
│   ├── provider/                 # Provider interface and registry
│   │   ├── provider.go
│   │   ├── options.go
│   │   ├── registry.go
│   │   ├── streaming.go
│   │   ├── openai/
│   │   │   ├── provider.go
│   │   │   ├── streaming.go
│   │   │   ├── options.go
│   │   │   └── provider_test.go
│   │   ├── anthropic/
│   │   │   ├── provider.go
│   │   │   ├── streaming.go
│   │   │   ├── options.go
│   │   │   └── provider_test.go
│   │   ├── gemini/
│   │   │   ├── provider.go
│   │   │   ├── streaming.go
│   │   │   ├── options.go
│   │   │   └── provider_test.go
│   │   ├── ollama/
│   │   │   ├── provider.go
│   │   │   ├── streaming.go
│   │   │   ├── options.go
│   │   │   └── provider_test.go
│   │   ├── mistral/
│   │   │   ├── provider.go
│   │   │   ├── streaming.go
│   │   │   ├── options.go
│   │   │   └── provider_test.go
│   │   ├── cohere/
│   │   │   ├── provider.go
│   │   │   ├── streaming.go
│   │   │   ├── options.go
│   │   │   └── provider_test.go
│   │   └── mock/
│   │       ├── provider.go
│   │       └── provider_test.go
│   ├── orchestration/            # Workflow engine and patterns
│   │   ├── engine.go
│   │   ├── dag.go
│   │   ├── sequential.go
│   │   ├── parallel.go
│   │   ├── router.go
│   │   ├── loop.go
│   │   ├── debate.go
│   │   ├── hierarchical.go
│   │   └── engine_test.go
│   ├── tool/                     # Tool/function calling system
│   │   ├── tool.go
│   │   ├── registry.go
│   │   ├── executor.go
│   │   ├── builtin/
│   │   │   ├── http.go
│   │   │   ├── calculator.go
│   │   │   ├── code_interpreter.go
│   │   │   └── filesystem.go
│   │   └── tool_test.go
│   ├── memory/                   # Context and memory management
│   │   ├── memory.go
│   │   ├── buffer.go
│   │   ├── window.go
│   │   ├── summary.go
│   │   ├── semantic.go
│   │   └── memory_test.go
│   ├── bus/                      # Inter-agent message bus
│   │   ├── bus.go
│   │   ├── channel.go
│   │   ├── subscription.go
│   │   └── bus_test.go
│   ├── prompt/                   # Prompt templates and assembly
│   │   ├── template.go
│   │   ├── renderer.go
│   │   ├── funcs.go
│   │   └── prompt_test.go
│   ├── middleware/                # Cross-cutting concerns
│   │   ├── retry.go
│   │   ├── ratelimit.go
│   │   ├── logging.go
│   │   ├── tracing.go
│   │   ├── caching.go
│   │   └── middleware_test.go
│   ├── config/                   # Configuration management
│   │   ├── config.go
│   │   ├── loader.go
│   │   └── config_test.go
│   └── observe/                  # Observability
│       ├── trace.go
│       ├── metrics.go
│       ├── logger.go
│       └── observe_test.go
├── pkg/                          # Public API surface
│   └── orchestra/
│       └── orchestra.go          # Re-exports for public consumption
├── api/
│   └── v1/
│       ├── orchestra.proto       # gRPC service definition
│       └── openapi.yaml          # REST API spec
├── configs/
│   ├── orchestra.yaml            # Default configuration
│   └── providers.yaml            # Provider-specific defaults
├── scripts/
│   ├── build.sh
│   ├── test.sh
│   └── lint.sh
├── go.mod
├── go.sum
├── Makefile
├── .golangci.yml
└── README.md
```

---

## 5. Phase 1 — Foundation & Core Abstractions

**Status:** ✅ Core Complete — CLI coding commands remaining

### Objectives

Establish the project skeleton, core interfaces, CLI entry point, and type system that all subsequent phases build upon. The foundation (types, interfaces, registry, config) is complete; remaining work adds CLI coding subcommands (`chat`, `run`, `pipeline`).

### Tasks

#### 1.1 Project Bootstrapping
- [x] Initialize Go module (`github.com/user/orchestra`)
- [x] Set up `Makefile` with build, test, lint, and fmt targets
- [x] Configure `.golangci.yml` with strict linting rules
- [x] Set up CI pipeline (GitHub Actions: lint, test, build on push/PR)
- [x] Add `README.md` with project overview and quickstart

#### 1.1b CLI Entry Point
- [x] Scaffold `cmd/orchestra/main.go` as the CLI entry point (stdlib-based, no framework)
- [x] Implement root command with `version`, `help`, `serve`, `healthcheck` subcommands
- [x] Implement global flags (`--config`, `--help`, `--version`) and build-time ldflags
- [ ] Implement `orchestra chat` subcommand for interactive coding sessions (REPL-style)
- [ ] Implement `orchestra run` subcommand for one-shot code generation / refactoring tasks
- [ ] Implement `orchestra pipeline` subcommand for running multi-agent workflow definitions from YAML
- [ ] Add `--model` flag for quick model override on any command
- [ ] Write CLI smoke tests

#### 1.2 Core Message Types
- [x] Define `Role` type (`System`, `User`, `Assistant`, `Tool`, `Function`)
- [x] Define `Message` struct with content, role, name, tool calls, tool results, metadata
- [x] Define `ContentBlock` for multi-modal content (text, image, file)
- [x] Define `Conversation` type as an ordered sequence of messages
- [x] Implement conversation utilities: truncation, filtering, formatting
- [x] Write comprehensive table-driven tests for all message types

```go
// internal/message/role.go
type Role string

const (
    RoleSystem    Role = "system"
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
    RoleTool      Role = "tool"
    RoleFunction  Role = "function"
)

// internal/message/message.go
type Message struct {
    Role       Role
    Content    []ContentBlock
    Name       string
    ToolCalls  []ToolCall
    ToolResult *ToolResult
    Metadata   map[string]any
}

type ContentBlock struct {
    Type     string // "text", "image", "file"
    Text     string
    ImageURL string
    FileData []byte
    MimeType string
}

type Conversation struct {
    ID       string
    Messages []Message
    Metadata map[string]any
}
```

#### 1.3 Provider Interface
- [x] Define the core `Provider` interface
- [x] Define `ProviderConfig` for provider-level configuration
- [x] Define `GenerateOptions` (temperature, max tokens, stop sequences, etc.)
- [x] Define `GenerateResult` (content, usage, finish reason, model info)
- [x] Define streaming interfaces (`StreamEvent`, `StreamChunk`, `StreamReader`)
- [x] Define model capability flags (streaming, tools, vision, etc.)
- [x] Write mock provider for testing

```go
// internal/provider/provider.go
type Provider interface {
    // Name returns the provider identifier (e.g., "openai", "anthropic").
    Name() string

    // Models returns the list of available model IDs for this provider.
    Models(ctx context.Context) ([]ModelInfo, error)

    // Generate sends a conversation and returns a single completion.
    Generate(ctx context.Context, req GenerateRequest) (*GenerateResult, error)

    // Stream sends a conversation and returns a stream of chunks.
    Stream(ctx context.Context, req GenerateRequest) (<-chan StreamEvent, error)

    // Capabilities returns what features this provider supports.
    Capabilities(model string) ModelCapabilities
}

type GenerateRequest struct {
    Model       string
    Messages    []message.Message
    Tools       []tool.Tool
    Options     GenerateOptions
    Metadata    map[string]any
}

type GenerateResult struct {
    ID          string
    Message     message.Message
    Usage       TokenUsage
    FinishReason FinishReason
    Model       string
    Metadata    map[string]any
}

type StreamEvent struct {
    Type    StreamEventType // Start, Chunk, ToolCall, Done, Error
    Chunk   string
    ToolCall *message.ToolCall
    Usage   *TokenUsage
    Error   error
}

type GenerateOptions struct {
    Temperature   *float64
    TopP          *float64
    MaxTokens     *int
    StopSequences []string
    Seed          *int64
    ResponseFormat *ResponseFormat
    // Provider-specific overrides
    Extra map[string]any
}
```

#### 1.4 Provider Registry
- [x] Implement a global and scoped provider registry
- [x] Support lazy initialization with provider factories
- [x] Support provider aliases (e.g., `gpt4` → `openai::gpt-4-turbo`)
- [x] Thread-safe registration and lookup

```go
// internal/provider/registry.go
type Registry struct {
    mu         sync.RWMutex
    factories  map[string]ProviderFactory
    providers  map[string]Provider
    aliases    map[string]string
}

func (r *Registry) Register(name string, factory ProviderFactory) error
func (r *Registry) Get(name string) (Provider, error)
func (r *Registry) Resolve(modelRef string) (Provider, string, error)
```

#### 1.5 Configuration System
- [x] Define hierarchical YAML-based configuration schema
- [x] Support environment variable overrides (`ORCHESTRA_PROVIDER_OPENAI_API_KEY`)
- [x] Support programmatic configuration via Go options pattern
- [x] Implement config validation with clear error messages

```yaml
# configs/orchestra.yaml
providers:
  openai:
    api_key: ${OPENAI_API_KEY}
    base_url: https://api.openai.com/v1
    default_model: gpt-4o
    rate_limit:
      requests_per_minute: 60
      tokens_per_minute: 150000
    retry:
      max_attempts: 3
      initial_backoff: 1s

  anthropic:
    api_key: ${ANTHROPIC_API_KEY}
    default_model: claude-sonnet-4-20250514
    rate_limit:
      requests_per_minute: 60

  ollama:
    base_url: http://localhost:11434
    default_model: llama3

logging:
  level: info
  format: json

observability:
  tracing:
    enabled: true
    endpoint: http://localhost:4318
  metrics:
    enabled: true
    endpoint: http://localhost:9090/metrics
```

### Deliverables

- [x] Working Go module with all core types and interfaces
- [x] Mock provider that passes all interface compliance tests
- [x] Provider registry with factory pattern
- [x] Configuration loading and validation
- [x] CI pipeline running on every PR
- [x] 90%+ test coverage on core types
- [ ] CLI binary with `chat`, `run`, and `pipeline` coding subcommands

### Milestone Criteria

- All interfaces compile and are documented with GoDoc ✅
- Mock provider demonstrates the full generate and stream lifecycle ✅
- Configuration loads from YAML and environment variables ✅
- `make test` passes cleanly ✅
- CLI `chat`, `run`, and `pipeline` subcommands accept input and produce output (pending provider integration in Phase 2)

---

## 6. Phase 2 — Provider Integrations

**Status:** ✅ COMPLETE
**Depends On:** Phase 1
**Completion Date:** 2026-04-20
**Report:** `docs/PHASE2_REPORT.md`

### Objectives

Implement concrete provider integrations for the six primary LLM backends, each conforming to the `Provider` interface.

### Tasks

#### 2.1 OpenAI Provider ✅
- [x] Implement `Generate` using OpenAI Chat Completions API
- [x] Implement `Stream` using SSE streaming
- [x] Map OpenAI message format ↔ Orchestra `Message` type
- [x] Handle function/tool calling (parallel tool calls)
- [x] Support vision (image inputs via URL and base64)
- [x] Support JSON mode and structured outputs
- [x] Support model-specific features (GPT-4o, GPT-4-Turbo, o1/o3 reasoning models)
- [x] Handle OpenAI-specific error codes and rate limit headers
- [x] Configure connection pooling for the HTTP client

#### 2.2 Anthropic Provider ✅
- [x] Implement `Generate` using Anthropic Messages API
- [x] Implement `Stream` using SSE streaming
- [x] Map Anthropic content blocks ↔ Orchestra `ContentBlock`
- [x] Handle tool use with Anthropic's `tool_use` / `tool_result` blocks
- [x] Support Anthropic-specific features (caching, extended thinking)
- [x] Handle prompt caching headers for cost optimization
- [x] Map Anthropic stop reasons to Orchestra `FinishReason`

#### 2.3 Google Gemini Provider ✅
- [x] Implement `Generate` using Gemini API (REST)
- [x] Implement `Stream` using Gemini streaming endpoint
- [x] Map Gemini `Content`/`Part` ↔ Orchestra `Message`/`ContentBlock`
- [x] Handle function calling with Gemini's `FunctionCall`/`FunctionResponse`
- [x] Support multi-modal inputs (text, image, video, audio)
- [x] Handle Gemini safety settings and block reasons

#### 2.4 Ollama Provider ✅
- [x] Implement `Generate` using Ollama REST API
- [x] Implement `Stream` using Ollama streaming
- [x] Support model listing from local Ollama instance
- [x] Handle tool calling (Ollama's native tool support)
- [x] Auto-detect Ollama availability and provide meaningful errors
- [x] Support custom Ollama endpoints (remote hosts)

#### 2.5 Mistral Provider ✅
- [x] Implement `Generate` using Mistral Chat API
- [x] Implement `Stream` using Mistral streaming
- [x] Handle Mistral function calling
- [x] Support Mistral-specific features (JSON mode, safe prompt)

#### 2.6 Cohere Provider ✅
- [x] Implement `Generate` using Cohere Chat API (v2)
- [x] Implement `Stream` using Cohere streaming
- [x] Handle Cohere tool use
- [x] Support Cohere-specific features (connectors, citations)

#### 2.7 Provider Middleware Layer ✅
- [x] Implement retry middleware with exponential backoff + jitter
- [x] Implement rate limiter (token bucket per provider)
- [x] Implement logging middleware (request/response structured logs)
- [x] Implement caching middleware (optional, keyed by request hash)
- [x] Implement circuit breaker for provider failure protection
- [x] Implement request/response middleware chain

```go
// internal/middleware/middleware.go
type ProviderMiddleware func(Provider) Provider

func WithRetry(maxAttempts int, backoff BackoffStrategy) ProviderMiddleware
func WithRateLimit(rpm int, tpm int) ProviderMiddleware
func WithLogging(logger *slog.Logger) ProviderMiddleware
func WithCaching(store CacheStore, ttl time.Duration) ProviderMiddleware
func WithCircuitBreaker(threshold int, resetTimeout time.Duration) ProviderMiddleware
```

### Deliverables

- [x] Six provider implementations, each passing a shared interface compliance test suite
- [x] Per-provider integration tests (skipped without API keys, runnable in CI with secrets)
- [x] Middleware chain for cross-cutting concerns
- [x] Token usage normalization across all providers
- [x] Error normalization across all providers

### Milestone Criteria ✅

- [x] All providers pass the shared `ProviderContract` test suite
- [x] Streaming works end-to-end for all providers
- [x] Tool calling works for providers that support it
- [x] Rate limiting and retry logic verified with simulated failures
- [x] Provider can be swapped with zero code changes beyond configuration

---

## 7. Phase 3 — Agent Runtime & Lifecycle

**Status:** ✅ Complete
**Depends On:** Phase 1, Phase 2

### Objectives

Build the Agent abstraction — the primary building block that users interact with. An agent owns a provider, a system prompt, a set of tools, and memory.

### Tasks

#### 3.1 Agent Definition ✅
- [x] Define `Agent` struct with configuration and runtime state
- [x] Define functional options for agent creation
- [x] Implement agent lifecycle: create → run → stop
- [x] Support agent cloning for parallel execution

```go
// internal/agent/agent.go
type Agent struct {
    id          string
    name        string
    provider    provider.Provider
    model       string
    system      *prompt.Template
    tools       []tool.Tool
    memory      memory.Memory
    maxTurns    int
    middleware  []middleware.ProviderMiddleware
    logger      *slog.Logger
}

func New(name string, opts ...Option) (*Agent, error)
func (a *Agent) Run(ctx context.Context, input string) (*AgentResult, error)
func (a *Agent) RunConversation(ctx context.Context, messages []message.Message) (*AgentResult, error)
func (a *Agent) Stream(ctx context.Context, input string) (<-chan AgentEvent, error)
func (a *Agent) Clone(name string) *Agent

type Option func(*Agent) error

func WithProvider(p provider.Provider, model string) Option
func WithModel(modelRef string) Option // "openai::gpt-4o"
func WithSystemPrompt(tmpl string) Option
func WithSystemPromptFile(path string) Option
func WithTools(tools ...tool.Tool) Option
func WithMemory(m memory.Memory) Option
func WithMaxTurns(n int) Option
func WithMiddleware(m ...middleware.ProviderMiddleware) Option
```

#### 3.2 Agent Execution Loop ✅
- [x] Implement the generate → tool call → feed result → generate loop
- [x] Track conversation turns within a single `Run` call
- [x] Handle `maxTurns` to prevent infinite tool loops
- [x] Emit events at each stage for observability
- [x] Support graceful cancellation via context

```go
// Execution loop pseudocode:
// 1. Assemble prompt (system + memory + input)
// 2. Call provider.Generate or provider.Stream
// 3. If response has tool calls:
//    a. Execute each tool (parallel if independent)
//    b. Append tool results to conversation
//    c. Go to step 2
// 4. Return final response
```

#### 3.3 Agent Result & Events ✅
- [x] Define `AgentResult` with full execution trace
- [x] Define `AgentEvent` for streaming and observability
- [x] Track token usage across the entire execution loop
- [x] Capture tool execution details in result

```go
type AgentResult struct {
    Output      message.Message
    Conversation message.Conversation
    ToolCalls   []ToolExecution
    Usage       TokenUsage // aggregate across all turns
    Duration    time.Duration
    Turns       int
    Metadata    map[string]any
}

type ToolExecution struct {
    Turn     int
    Call     message.ToolCall
    Result   message.ToolResult
    Duration time.Duration
    Error    error
}

type AgentEvent struct {
    Type    AgentEventType // Thinking, GenerateStart, GenerateChunk, ToolCallStart, ToolCallEnd, Done, Error
    Chunk   string
    ToolCall *message.ToolCall
    Result  *ToolExecution
    Usage   *TokenUsage
    Error   error
}
```

#### 3.4 Prompt Template System ✅
- [x] Define a Go template-based prompt system
- [x] Support variable injection (e.g., `{{.Task}}`, `{{.Context}}`)
- [x] Support conditional blocks and loops in prompts
- [x] Include built-in template functions (json, yaml, indent, etc.)
- [x] Support prompt loading from embedded filesystem

### Deliverables

- [x] Agent type with full lifecycle management
- [x] Execution loop with tool calling support
- [x] Streaming agent events
- [x] Prompt template system
- [x] Agent cloning for parallel use

### Milestone Criteria ✅

- ✅ Agent can execute a single-turn conversation
- ✅ Agent can execute a multi-turn tool-calling loop
- ✅ Agent streaming delivers chunks in real-time
- ✅ Agent can be configured entirely via functional options
- ✅ All agent operations respect context cancellation

---

## 8. Phase 4 — Orchestration Engine

**Status:** Not Started
**Depends On:** Phase 3

### Objectives

Build the orchestration layer that composes multiple agents into workflows. This is the core value proposition of Orchestra.

### Tasks

#### 4.1 Workflow Definition & DAG
- [ ] Define `Workflow` as a directed acyclic graph of `Step` nodes
- [ ] Each step wraps an agent with input/output mapping
- [ ] Define edges with conditional routing
- [ ] Support workflow input/output schemas (optional, for validation)
- [ ] Compile workflow to verify no cycles and all dependencies are resolvable

```go
// internal/orchestration/dag.go
type Workflow struct {
    id    string
    name  string
    steps map[string]*Step
    edges []Edge
    input InputMapping
    output OutputMapping
}

type Step struct {
    ID          string
    Agent       *agent.Agent
    InputMap    InputMapping   // How to map workflow data → agent input
    OutputMap   OutputMapping  // How to map agent output → workflow data
    Condition   Condition      // Optional: only execute if true
    RetryPolicy *RetryPolicy
    Timeout     time.Duration
}

type Edge struct {
    From string
    To   string
    Condition Condition // Optional: conditional routing
}

type InputMapping func(ctx WorkflowContext) (string, error)
type OutputMapping func(result *agent.AgentResult, ctx WorkflowContext) error
type Condition func(ctx WorkflowContext) bool
```

#### 4.2 Workflow Builder (Fluent API)
- [ ] Create a builder pattern for composing workflows
- [ ] Support sequential, parallel, conditional, and loop patterns
- [ ] Validate workflow at build time

```go
// internal/orchestration/builder.go
workflow := orchestration.NewWorkflow("research-and-summarize").
    AddStep("researcher", researcherAgent,
        orchestration.WithInput(func(ctx WorkflowContext) (string, error) {
            return fmt.Sprintf("Research: %s", ctx.Get("topic")), nil
        }),
    ).
    AddStep("writer", writerAgent,
        orchestration.WithInput(func(ctx WorkflowContext) (string, error) {
            return fmt.Sprintf("Write about: %s\nBased on:\n%s",
                ctx.Get("topic"), ctx.GetStepOutput("researcher")), nil
        }),
        orchestration.DependsOn("researcher"),
    ).
    AddStep("reviewer", reviewerAgent,
        orchestration.WithInput(func(ctx WorkflowContext) (string, error) {
            return fmt.Sprintf("Review this:\n%s", ctx.GetStepOutput("writer")), nil
        }),
        orchestration.DependsOn("writer"),
    ).
    Build()
```

#### 4.3 Execution Engine
- [ ] Implement DAG executor with topological sort
- [ ] Run independent steps in parallel using goroutines
- [ ] Propagate workflow context between steps
- [ ] Handle step failures (fail-fast, continue-on-error, fallback)
- [ ] Implement workflow-level cancellation and timeout

```go
// internal/orchestration/engine.go
type Engine struct {
    registry *provider.Registry
    logger   *slog.Logger
    tracer   trace.Tracer
    meter    metric.Meter
}

func (e *Engine) Execute(ctx context.Context, workflow *Workflow, input map[string]any) (*WorkflowResult, error)
func (e *Engine) Stream(ctx context.Context, workflow *Workflow, input map[string]any) (<-chan WorkflowEvent, error)

type WorkflowResult struct {
    ID        string
    Status    WorkflowStatus // Completed, Failed, Cancelled
    Steps     map[string]*StepResult
    Output    map[string]any
    Usage     TokenUsage // aggregate
    Duration  time.Duration
    Error     error
}
```

#### 4.4 Orchestration Patterns

**Sequential Pipeline**
- [ ] Chain agents in a linear sequence
- [ ] Pass output of one agent as input to the next
- [ ] Stop on first error or continue with partial results

```go
pipeline := orchestration.Sequential("pipeline",
    researcherAgent,
    analyzerAgent,
    writerAgent,
    reviewerAgent,
)
```

**Parallel Fan-Out / Fan-In**
- [ ] Run multiple agents concurrently on the same or partitioned input
- [ ] Collect results with configurable aggregation (concat, merge, vote, best-of-N)
- [ ] Support scatter (partition input) and gather (aggregate outputs)

```go
parallel := orchestration.Parallel("multi-perspective",
    orchestration.FanOut(
        optimisticAgent,
        pessimisticAgent,
        neutralAgent,
    ),
    orchestration.FanIn(func(results []*agent.AgentResult) (string, error) {
        // Aggregate all perspectives into a synthesized view
    }),
)
```

**Router / Dynamic Dispatch**
- [ ] Route input to different agents based on content analysis
- [ ] Support LLM-based routing (use a fast/cheap model to classify)
- [ ] Support rule-based routing (regex, keyword, schema match)

```go
router := orchestration.Router("task-router",
    orchestration.Route(conditions.ContainsCode, codeAgent),
    orchestration.Route(conditions.IsCreative, creativeAgent),
    orchestration.Route(conditions.IsMath, mathAgent),
    orchestration.Default(generalAgent),
)
```

**Debate / Multi-Round**
- [ ] Two or more agents debate a topic for N rounds
- [ ] Judge agent evaluates and picks the best response
- [ ] Configurable number of rounds and early stopping

```go
debate := orchestration.Debate("code-review",
    orchestration.WithDebaters(proAgent, conAgent),
    orchestration.WithJudge(judgeAgent),
    orchestration.WithRounds(3),
    orchestration.WithTopic("code quality assessment"),
)
```

**Hierarchical Delegation**
- [ ] Manager agent decomposes tasks and delegates to worker agents
- [ ] Manager synthesizes worker results
- [ ] Support multi-level hierarchies (manager → sub-managers → workers)

```go
hierarchy := orchestration.Hierarchical("project-manager",
    orchestration.WithManager(managerAgent),
    orchestration.WithWorkers(map[string]*agent.Agent{
        "research": researchAgent,
        "coding":   codeAgent,
        "testing":  testAgent,
    }),
    orchestration.WithMaxDelegations(5),
)
```

### Deliverables

- [ ] DAG-based workflow engine with parallel execution
- [ ] Fluent workflow builder API
- [ ] Five orchestration patterns: sequential, parallel, router, debate, hierarchical
- [ ] Workflow-level streaming events
- [ ] Failure handling strategies

### Milestone Criteria

- Sequential pipeline runs agents in order, passing data between them
- Parallel execution uses goroutines and correctly aggregates results
- Router dispatches to the correct agent based on input
- Debate pattern completes N rounds with judge evaluation
- Hierarchical delegation decomposes and reassembles tasks
- All patterns respect context cancellation and timeouts

---

## 9. Phase 5 — Inter-Agent Communication

**Status:** Completed ✅
**Depends On:** Phase 3

### Objectives

Enable agents to communicate with each other outside of rigid workflow structures via a publish-subscribe message bus.

### Tasks

#### 5.1 Message Bus ✅
- [x] Implement in-memory pub/sub message bus
- [x] Support topic-based subscriptions (wildcards)
- [x] Support direct agent-to-agent messaging
- [x] Thread-safe with buffered channels for backpressure
- [ ] Optional: Redis or NATS backend for distributed deployments

```go
// internal/bus/bus.go
type Bus interface {
    Publish(ctx context.Context, topic string, msg BusMessage) error
    Subscribe(topic string, handler Handler) (Subscription, error)
    Unsubscribe(sub Subscription) error
    Close() error
}

type BusMessage struct {
    ID        string
    Topic     string
    FromAgent string
    ToAgent   string // empty for broadcast
    Payload   any
    Timestamp time.Time
    Metadata  map[string]any
}

type Handler func(ctx context.Context, msg BusMessage) error
```

#### 5.2 Agent Mailbox ✅
- [x] Each agent gets an optional mailbox for direct messages
- [x] Mailbox is a buffered queue with configurable size
- [x] Agent can poll or block on incoming messages
- [ ] Messages can be injected into the agent's conversation context

#### 5.3 Broadcast Patterns ✅
- [x] Implement request-broadcast: one agent asks, many answer
- [x] Implement consensus: agents vote, majority wins
- [x] Implement auction: agents bid, best offer wins

### Deliverables

- [x] In-memory pub/sub message bus
- [x] Agent mailbox system
- [x] Three broadcast communication patterns
- [x] Request/response pattern with timeout
- [x] Multicast pattern for targeted delivery

### Milestone Criteria ✅

- [x] Agents can publish and subscribe to topics
- [x] Direct agent-to-agent messaging works
- [x] Broadcast with aggregation collects responses from multiple agents
- [x] Bus handles backpressure without deadlocking

---

## 10. Phase 6 — Tool System & Function Calling

**Status:** Not Started
**Depends On:** Phase 1, Phase 2

### Objectives

Build the tool/function calling system that agents use to interact with the outside world.

### Tasks

#### 6.1 Tool Interface & Registry
- [ ] Define `Tool` interface with JSON Schema generation
- [ ] Define `ToolRegistry` for managing available tools
- [ ] Support tool namespacing to avoid collisions
- [ ] Validate tool definitions at registration time

```go
// internal/tool/tool.go
type Tool interface {
    // Name returns the tool identifier (e.g., "web_search").
    Name() string

    // Description returns a human-readable description for the LLM.
    Description() string

    // Parameters returns the JSON Schema for the tool's input.
    Parameters() json.RawMessage

    // Execute runs the tool with the given input and returns the output.
    Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error)
}

type ToolDefinition struct {
    Type        string          `json:"type"` // "function"
    Function    FunctionDef     `json:"function"`
}

type FunctionDef struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Parameters  json.RawMessage `json:"parameters"`
}
```

#### 6.2 Tool Execution
- [ ] Implement synchronous tool execution with timeout
- [ ] Implement parallel tool execution (for independent tool calls)
- [ ] Sandboxed execution with resource limits
- [ ] Tool execution logging and metrics

#### 6.3 Built-in Tools

**General-Purpose Tools**
- [ ] `http_request` — HTTP GET/POST/PUT/DELETE with configurable headers
- [ ] `calculator` — Mathematical expression evaluation
- [ ] `code_interpreter` — Execute code in a sandboxed environment
- [ ] `file_read` / `file_write` — Filesystem operations (configurable root)
- [ ] `web_search` — Web search via configurable backend (SerpAPI, Brave, etc.)
- [ ] `json_transform` — JSON manipulation (jq-like)
- [ ] `sql_query` — Execute SQL queries (read-only, configurable)

**Coding-Specific Tools (CLI Mode)**
- [ ] `file_edit` — Apply targeted edits to files (search-and-replace blocks, line-range replacement, patch-style diffs)
- [ ] `code_search` — Search codebase by text (grep), symbol name, or AST pattern; return matching file paths, line numbers, and context
- [ ] `shell_exec` — Execute shell commands in a sandboxed working directory with configurable allowlists and timeouts
- [ ] `git_operations` — Common Git operations (`diff`, `log`, `blame`, `status`, `apply`) for understanding and modifying codebases
- [ ] `list_directory` — Recursively list files and directories with ignore-file support (`.gitignore`, `.orchestaignore`)
- [ ] `diagnostics` — Run linters, type-checkers, and test runners; parse and return structured error output

#### 6.4 Tool Helper Utilities
- [ ] Go struct → JSON Schema generator for easy tool definition
- [ ] Tool builder for declarative tool creation
- [ ] Tool input/output validation

```go
// Declarative tool creation example
searchTool := tool.New("web_search",
    tool.WithDescription("Search the web for information"),
    tool.WithInputSchema[SearchInput](),
    tool.WithHandler(func(ctx context.Context, input SearchInput) (SearchOutput, error) {
        // implementation
    }),
)

type SearchInput struct {
    Query string `json:"query" description:"The search query"`
    Count int    `json:"count" description:"Number of results" default:"5"`
}
```

### Deliverables

- [ ] Tool interface, registry, and execution engine
- [ ] Thirteen built-in tools (7 general-purpose + 6 coding-specific)
- [ ] Tool builder with schema generation from Go types
- [ ] Parallel tool execution

### Milestone Criteria

- Agent can call a tool and receive results in the execution loop
- Multiple tool calls in a single turn execute in parallel
- Tool schemas are correctly generated from Go structs
- Built-in tools pass integration tests
- Coding-specific tools (`file_edit`, `code_search`, `shell_exec`, `git_operations`) work end-to-end in CLI mode — an agent can read, search, edit, and verify code changes
</newtext>


---

## 11. Phase 7 — Memory & Context Management

**Status:** ✅ Complete
**Depends On:** Phase 3

### Objectives

Build a flexible memory system that manages conversation context and long-term knowledge for agents.

### Tasks

#### 7.1 Memory Interface
- [x] Define `Memory` interface for different memory strategies
- [x] Support adding messages and retrieving relevant context
- [x] Support memory expiration and compaction

```go
// internal/memory/memory.go
type Memory interface {
    // Add stores a message in memory.
    Add(ctx context.Context, msg message.Message) error

    // GetRelevant retrieves messages relevant to the given input.
    GetRelevant(ctx context.Context, input string, opts GetOptions) ([]message.Message, error)

    // GetAll retrieves all messages (up to limit).
    GetAll(ctx context.Context, opts GetOptions) ([]message.Message, error)

    // Clear removes all messages.
    Clear(ctx context.Context) error

    // Size returns the current number of messages.
    Size(ctx context.Context) int
}

type GetOptions struct {
    Limit      int
    MaxTokens  int
    Tokenizer  Tokenizer
}
```

#### 7.2 Memory Strategies
- [x] **BufferMemory** — Simple in-memory buffer with optional size limit
- [x] **SlidingWindowMemory** — Keeps the last N messages or tokens
- [x] **SummaryMemory** — Older messages are summarized by an LLM to compress context
- [x] **SemanticMemory** — Vector-based retrieval using embeddings for relevant context
- [x] **CompositeMemory** — Combines multiple strategies (e.g., recent messages + semantic search)

#### 7.3 Token Counting
- [x] Implement tokenizer interface for token counting
- [x] Integrate `tiktoken` for OpenAI models (via CGo or WASM)
- [x] Provide approximation fallback for models without exact tokenizers
- [x] Track token usage per message for accurate context window management

#### 7.4 Context Window Management
- [x] Auto-truncate conversation to fit within model context window
- [x] Prioritize system prompt + recent messages
- [x] Support custom truncation strategies
- [x] Warn when approaching context limits

### Deliverables

- [x] Memory interface with five implementations
- [x] Token counting for major model families
- [x] Context window auto-management
- [x] Memory persistence hooks (for future DB backends)

### Milestone Criteria

- Agent maintains conversation context across multiple `Run` calls
- Sliding window correctly evicts old messages
- Summary memory compresses history without losing key information
- Context window management prevents token limit errors

---

## 12. Phase 8 — Observability & Operations

**Status:** Not Started
**Depends On:** Phase 3, Phase 4

### Objectives

Instrument Orchestra with comprehensive observability — structured logging, distributed tracing, and metrics — so operators can understand, debug, and optimize multi-agent workflows.

### Tasks

#### 8.1 Structured Logging
- [ ] Use `log/slog` throughout the codebase
- [ ] Log agent lifecycle events (start, generate, tool call, complete)
- [ ] Log workflow execution events (step start, step complete, workflow complete)
- [ ] Support configurable log levels per component
- [ ] Redact sensitive data (API keys, personal content) from logs

#### 8.2 Distributed Tracing
- [ ] Integrate OpenTelemetry tracing (`go.opentelemetry.io/otel`)
- [ ] Create spans for: provider calls, tool executions, workflow steps
- [ ] Propagate trace context across agent boundaries
- [ ] Attribute spans with model, provider, token usage, latency

```go
// Span naming convention:
// "orchestra.agent.{name}.generate"
// "orchestra.agent.{name}.tool.{tool_name}"
// "orchestra.workflow.{name}.step.{step_id}"
// "orchestra.provider.{provider}.generate"
```

#### 8.3 Metrics
- [ ] Integrate OpenTelemetry metrics
- [ ] Track request count, latency histogram, error rate per provider
- [ ] Track token usage (prompt tokens, completion tokens) per model
- [ ] Track tool execution count and latency
- [ ] Track active agents and workflows gauge
- [ ] Expose Prometheus-compatible metrics endpoint

```go
// Metrics naming convention:
// orchestra_provider_requests_total{provider, model, status}
// orchestra_provider_latency_seconds{provider, model}
// orchestra_tokens_total{provider, model, type="prompt|completion"}
// orchestra_tool_executions_total{tool, status}
// orchestra_tool_latency_seconds{tool}
// orchestra_active_agents{workflow}
```

#### 8.4 Dashboard & Monitoring
- [ ] Provide Grafana dashboard JSON template
- [ ] Define alerting rules for common issues (high error rate, token budget exceeded)
- [ ] Health check endpoint for server mode

### Deliverables

- [ ] Structured logging throughout
- [ ] OpenTelemetry tracing integration
- [ ] OpenTelemetry metrics with Prometheus endpoint
- [ ] Grafana dashboard template
- [ ] Example alerting rules

### Milestone Criteria

- Every provider call creates a trace span with model and token attributes
- Token usage metrics are accurate and queryable
- Grafana dashboard shows real-time agent and workflow status
- Logs correlate with traces via trace ID

---

## 9. Phase 9 — Advanced Patterns

**Status:** Not Started
**Depends On:** Phase 4, Phase 6, Phase 7

### Objectives

Implement advanced multi-agent patterns that leverage the full Orchestra stack.

### Tasks

#### 9.1 Retrieval-Augmented Generation (RAG)
- [ ] Document ingestion pipeline (chunking, embedding, storage)
- [ ] Vector store interface with in-memory and external backends
- [ ] RAG tool that agents can use to query knowledge bases
- [ ] Embedding provider interface (OpenAI, Cohere, local)

#### 9.2 Self-Reflection & Refinement
- [ ] Agent evaluates its own output against criteria
- [ ] Iterative refinement loop (generate → evaluate → refine)
- [ ] Configurable quality thresholds and max iterations

```go
refinement := orchestration.Refine("code-improver",
    codeAgent,
    orchestration.WithEvaluator(evaluatorAgent),
    orchestration.WithCriteria("Correctness, efficiency, readability"),
    orchestration.WithMaxIterations(3),
    orchestration.WithThreshold(0.9),
)
```

#### 9.3 Planning & Re-planning
- [ ] Planner agent creates a step-by-step plan
- [ ] Executor agents carry out each step
- [ ] Re-planner evaluates progress and adjusts plan
- [ ] Support for partial plan execution and recovery

#### 9.4 Human-in-the-Loop
- [ ] Interrupt agent execution for human approval
- [ ] Support approval gates in workflows (before critical steps)
- [ ] Human feedback injection into agent context
- [ ] Configurable auto-approval rules

```go
workflow := orchestration.NewWorkflow("content-creation").
    AddStep("draft", draftAgent).
    AddStep("human-review", orchestration.HumanApproval(
        "Review the draft and provide feedback",
        orchestration.WithTimeout(30*time.Minute),
        orchestration.WithCallbackEndpoint("/api/approvals/{id}"),
    )).
    AddStep("final", finalAgent, orchestration.DependsOn("human-review")).
    Build()
```

#### 9.5 Multi-Model Ensemble
- [ ] Run the same prompt through multiple models
- [ ] Aggregate responses (majority vote, best-of-N, cascading)
- [ ] Cost-quality tradeoff configuration

### Deliverables

- [ ] RAG pipeline with vector store
- [ ] Self-refinement orchestration pattern
- [ ] Planning/re-planning pattern
- [ ] Human-in-the-loop integration
- [ ] Multi-model ensemble pattern

### Milestone Criteria

- RAG pipeline ingests documents and agents can query them
- Refinement loop improves output quality over iterations
- Planning agent creates executable plans
- Human approval gates pause and resume correctly
- Ensemble produces higher quality outputs than single models

---

## 14. Phase 10 — Production Readiness

**Status:** Not Started
**Depends On:** All previous phases

### Objectives

Harden Orchestra for production use with comprehensive testing, documentation, deployment tooling, and a stable public API.

### Tasks

#### 10.1 Comprehensive Testing
- [ ] Achieve 80%+ test coverage across all packages
- [ ] Write end-to-end integration tests for all orchestration patterns
- [ ] Write provider contract tests (shared test suite for all providers)
- [ ] Write concurrency tests for parallel execution and message bus
- [ ] Write failure injection tests (network errors, timeouts, rate limits)
- [ ] Set up property-based testing for core types (using `rapid` or `gopter`)
- [ ] Benchmark critical paths (provider call overhead, DAG scheduling)

#### 10.2 Documentation
- [ ] Write `README.md` with quickstart, installation, and overview
- [ ] Write `docs/ARCHITECTURE.md` with detailed architecture description
- [ ] Write GoDoc for all public types and functions
- [ ] Write examples in `docs/examples/` for each major feature:
  - Single agent with one provider
  - Multi-provider agent switching
  - Sequential pipeline
  - Parallel fan-out/fan-in
  - Router pattern
  - Debate pattern
  - Hierarchical delegation
  - Custom tool creation
  - Memory management
  - Observability setup
- [ ] Write `docs/CONTRIBUTING.md` with development guidelines
- [ ] Record Architecture Decision Records (ADRs) for key decisions

#### 10.3 API Stability
- [ ] Audit all exported types for naming consistency
- [ ] Ensure backward compatibility guarantees
- [ ] Use `go vet`, `staticcheck`, and `golangci-lint` with zero warnings
- [ ] Create a `CHANGELOG.md` and establish versioning convention

#### 10.4 Server Mode (Optional)
- [ ] Implement REST API using `net/http` (no framework dependency)
- [ ] Implement gRPC API using protocol buffers
- [ ] Authentication middleware (API key, bearer token)
- [ ] Configuration hot-reload
- [ ] Graceful shutdown with in-flight request draining

#### 10.5 Distribution & Deployment
- [ ] Build and release static binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64 via GoReleaser or equivalent
- [ ] Publish binaries to GitHub Releases with checksums and signatures
- [ ] Homebrew formula for macOS/Linux installation
- [ ] `go install` support for latest development builds
- [ ] (Optional) Multi-stage Dockerfile for server-mode deployment
- [ ] (Optional) Docker Compose for local development with observability stack
- [ ] (Optional) Kubernetes manifests (Deployment, Service, ConfigMap) and Helm chart

#### 10.6 Performance Optimization
- [ ] Profile and optimize hot paths
- [ ] Implement connection pooling for all HTTP-based providers
- [ ] Add optional response caching with configurable TTL
- [ ] Benchmark and tune goroutine pool sizes

### Deliverables

- [ ] 80%+ test coverage
- [ ] Complete documentation and examples
- [ ] Zero lint warnings
- [ ] Static binaries for all platforms with automated release pipeline
- [ ] Homebrew formula and `go install` working
- [ ] (Optional) Docker image < 50MB for server mode
- [ ] (Optional) Kubernetes deployment manifests and Helm chart
- [ ] REST and gRPC API definitions

### Milestone Criteria

- All tests pass consistently (no flaky tests)
- Documentation covers every public API
- Static binaries build and run on all target platforms
- `go install` and Homebrew install work end-to-end
- (Optional) Docker image builds and runs correctly for server mode
- Performance benchmarks show acceptable latency overhead (< 5ms per agent call beyond provider time)

---

## 15. Technical Decision Records

### TDR-001: Interface-Based Provider Abstraction

**Decision:** Define a `Provider` interface with `Generate` and `Stream` methods, with provider-specific configuration passed via options.

**Rationale:** Each LLM provider has unique features, but the core interaction (send messages, get completion) is universal. An interface allows swapping providers without changing agent or workflow code. Streaming is a first-class concern because most providers support it and it is critical for user experience.

**Alternatives Considered:**
- Provider-specific types with adapters: More type-safe but much more boilerplate.
- Generic provider using OpenAI-compatible endpoints: Many providers now offer OpenAI-compatible APIs, but not all features map cleanly.

### TDR-002: DAG-Based Workflow Engine

**Decision:** Use a directed acyclic graph (DAG) to represent workflows, with steps as nodes and data dependencies as edges.

**Rationale:** A DAG naturally represents data flow between agents and enables automatic parallel execution of independent steps. It is more expressive than a simple linear pipeline and more maintainable than ad-hoc goroutine orchestration.

**Alternatives Considered:**
- Actor model: More flexible but significantly more complex.
- Linear pipeline only: Simple but insufficient for real-world workflows.
- YAML/JSON workflow definitions: Good for configuration but not as the primary definition mechanism in Go.

### TDR-003: Functional Options Pattern

**Decision:** Use the functional options pattern for configuring agents, providers, and workflows.

**Rationale:** This is the idiomatic Go approach for optional configuration. It allows backward-compatible addition of new options, clear defaults, and validation at construction time.

### TDR-004: Go Standard Library + Minimal Dependencies

**Decision:** Minimize external dependencies. Use `net/http` for HTTP, `log/slog` for logging, `encoding/json` for JSON. Only add well-maintained dependencies when they provide significant value (OpenTelemetry, protobuf).

**Rationale:** Reduces supply chain risk, avoids version conflicts, and keeps the project maintainable. Users can bring their own HTTP transports, loggers, etc.

### TDR-005: CLI-First, Library-Native, Server Optional

**Decision:** Orchestra is primarily a CLI coding tool and a Go library. The CLI (`cmd/orchestra`) is the primary user-facing interface for code generation, refactoring, debugging, and multi-agent workflows. All core logic lives in importable Go packages so users can also embed it in their own applications. The server mode (REST/gRPC) is an optional layer on top.

**Rationale:** Developers interact with coding tools on the command line — a CLI-first approach meets users where they work. Keeping the core as importable Go packages ensures the same functionality is available to users who want to embed multi-agent orchestration directly into their Go applications. A server mode adds operational overhead that not all users need, so it remains optional.

---

## 16. Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Provider API changes break integrations | Medium | High | Pin API versions, comprehensive integration tests, monitor provider changelogs |
| Provider rate limits cause workflow failures | High | Medium | Rate limiting middleware, exponential backoff, circuit breakers, queue-based retries |
| Context window exceeded in long workflows | Medium | High | Token counting, context window management, automatic summarization |
| Complex DAG deadlocks during execution | Low | High | DAG validation at compile time, cycle detection, execution timeouts |
| High latency from sequential LLM calls | High | Medium | Parallel execution, caching, model selection (fast models for routing) |
| Cost overruns from unbounded token usage | Medium | High | Token budgets per workflow/agent, cost estimation before execution, alerts |
| Concurrent access to shared agent state | Medium | High | Immutable message types, copy-on-write conversation, proper mutex usage |
| Streaming error handling complexity | Medium | Medium | Clear error types, reconnection logic, partial result preservation |

---

## 17. Success Metrics

### Functional Metrics
- [ ] Support 6+ LLM providers with unified interface
- [ ] Support 5+ orchestration patterns
- [ ] Support streaming for all providers that offer it
- [ ] Support tool/function calling for providers that offer it
- [ ] Support multi-modal inputs (text + images) for capable models

### Quality Metrics
- [ ] 80%+ test coverage
- [ ] Zero lint warnings
- [ ] All public APIs have GoDoc
- [ ] All examples run without modification
- [ ] No data races detected by `-race` flag

### Performance Metrics
- [ ] < 5ms overhead per agent call (beyond provider latency)
- [ ] < 1ms overhead for DAG scheduling (beyond step execution)
- [ ] < 30MB static binary size (per platform)
- [ ] Support 100+ concurrent agent executions

### Developer Experience Metrics
- [ ] `orchestra chat` starts an interactive session in < 2 seconds
- [ ] `orchestra run` produces output for a one-shot task in < 5 seconds (excluding provider latency)
- [ ] `orchestra pipeline` executes a multi-agent workflow from a YAML definition with zero Go code
- [ ] New user can create a working agent in < 10 lines of Go code
- [ ] New user can create a working pipeline in < 30 lines of Go code
- [ ] Adding a new provider requires implementing one interface
- [ ] Adding a new tool requires implementing one interface

---

## Appendix A: Quick Start Example (Target API)

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/user/orchestra/pkg/orchestra"
)

func main() {
    ctx := context.Background()

    // Create agents using different providers and models
    researcher := orchestra.NewAgent("researcher",
        orchestra.WithModel("openai::gpt-4o"),
        orchestra.WithSystemPrompt("You are a thorough research assistant."),
        orchestra.WithTools(orchestra.WebSearch()),
    )

    writer := orchestra.NewAgent("writer",
        orchestra.WithModel("anthropic::claude-sonnet-4-20250514"),
        orchestra.WithSystemPrompt("You are a skilled technical writer."),
    )

    reviewer := orchestra.NewAgent("reviewer",
        orchestra.WithModel("openai::gpt-4o"),
        orchestra.WithSystemPrompt("You are a critical editor."),
    )

    // Compose into a workflow
    workflow := orchestra.Pipeline("research-write-review",
        researcher, writer, reviewer,
    )

    // Execute
    result, err := workflow.Run(ctx, "Write a technical blog post about WebAssembly")
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(result.Output)
    fmt.Printf("Tokens used: %d (cost: $%.4f)\n",
        result.Usage.TotalTokens,
        result.EstimatedCost(),
    )
}
```

---

## Appendix B: Phase Dependency Graph

```
Phase 1: Foundation
    │
    ├── Phase 2: Providers ──────────────────────┐
    │                                            │
    ├── Phase 6: Tools ──────────┐               │
    │                            │               │
    ├── Phase 7: Memory ─────────┼───┐           │
    │                            │   │           │
    └── Phase 3: Agent Runtime ──┼───┼───────────┘
                                 │   │
                    Phase 4: Orchestration Engine
                                 │
                    Phase 5: Inter-Agent Communication
                                 │
                    Phase 8: Observability
                                 │
                    Phase 9: Advanced Patterns
                                 │
                    Phase 10: Production Readiness
```

---

*This plan is a living document. It should be updated as requirements evolve and technical decisions are made.*
