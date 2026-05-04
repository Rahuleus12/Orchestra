# Orchestra Architecture

This document provides a detailed technical overview of Orchestra's architecture, design decisions, and internal components.

## Table of Contents

- [Overview](#overview)
- [Design Principles](#design-principles)
- [System Architecture](#system-architecture)
- [Core Components](#core-components)
  - [Message System](#message-system)
  - [Provider Interface](#provider-interface)
  - [Provider Registry](#provider-registry)
  - [Agent Runtime](#agent-runtime)
  - [Orchestration Engine](#orchestration-engine)
  - [Tool System](#tool-system)
  - [Memory System](#memory-system)
  - [Message Bus](#message-bus)
  - [Middleware System](#middleware-system)
- [Configuration](#configuration)
- [Observability](#observability)
- [Error Handling](#error-handling)
- [Concurrency Model](#concurrency-model)
- [Extension Points](#extension-points)

---

## Overview

Orchestra is a multi-agent AI orchestration engine written in Go. It provides:

- **Unified Provider Interface**: Abstract away differences between LLM providers (OpenAI, Anthropic, Gemini, Ollama, Mistral, Cohere)
- **Agent Runtime**: Self-contained agents with tools, memory, and middleware
- **Orchestration Engine**: DAG-based workflow execution with parallel processing
- **Communication**: Message bus for inter-agent communication
- **Observability**: Built-in tracing, metrics, and structured logging

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              CLI / Library API                              │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                        Orchestration Engine                         │    │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────────────────┐   │    │
│  │  │  DAG    │  │ Builder │  │ Patterns│  │  Workflow Context   │   │    │
│  │  └─────────┘  └─────────┘  └─────────┘  └─────────────────────┘   │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                    │                                         │
│  ┌─────────────────────────────────┼─────────────────────────────────────┐  │
│  │                          Agent Runtime                               │  │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐  │  │
│  │  │ Execution│  │  Tool   │  │ Memory  │  │Template │  │ Result  │  │  │
│  │  │  Loop   │  │ Registry│  │ Manager │  │ Engine  │  │ Handler │  │  │
│  │  └─────────┘  └─────────┘  └─────────┘  └─────────┘  └─────────┘  │  │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                    │                                         │
│  ┌─────────────────────────────────┼─────────────────────────────────────┐  │
│  │                        Provider Layer                                │  │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐  │  │
│  │  │ OpenAI  │  │Anthropic│  │ Gemini  │  │ Ollama  │  │ Mistral │  │  │
│  │  └─────────┘  └─────────┘  └─────────┘  └─────────┘  └─────────┘  │  │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                    Cross-Cutting Concerns                           │    │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────────┐   │    │
│  │  │  Config  │  │  Logger  │  │  Tracer  │  │    Middleware     │   │    │
│  │  └──────────┘  └──────────┘  └──────────┘  └──────────────────┘   │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Design Principles

### 1. Interface-Based Abstraction

All major components are defined by interfaces, enabling easy mocking and extension:

```go
type Provider interface {
    Name() string
    Models() []ModelInfo
    Generate(ctx context.Context, req GenerateRequest) (*GenerateResult, error)
    Stream(ctx context.Context, req GenerateRequest) (<-chan StreamEvent, error)
    Capabilities() ModelCapabilities
}
```

### 2. Functional Options Pattern

Configuration uses the functional options pattern for flexible, type-safe configuration:

```go
agent, _ := agent.New("assistant",
    agent.WithProvider(openaiProvider, "gpt-4o"),
    agent.WithSystemPrompt("You are helpful."),
    agent.WithMaxTurns(10),
    agent.WithMemory(memory.NewBufferMemory()),
)
```

### 3. Minimal Dependencies

Orchestra uses only the Go standard library plus `gopkg.in/yaml.v3` for configuration. This ensures:

- Fast compilation
- Small binary sizes
- Easy dependency management
- No supply chain concerns

### 4. Composition Over Inheritance

Components are composed rather than inherited. Middleware wraps providers, memory strategies can be combined, and tools can be chained.

### 5. Zero-Value Safety

Types are designed to work correctly with their zero values where possible, reducing initialization boilerplate.

---

## System Architecture

### Layer Organization

```
┌─────────────────────────────────────────────────────────────────┐
│                         cmd/orchestra                            │  CLI Entry Point
├─────────────────────────────────────────────────────────────────┤
│                         pkg/orchestra                            │  Public API
├─────────────────────────────────────────────────────────────────┤
│  internal/                                                       │
│  ├── agent/         Agent runtime and execution                  │
│  ├── provider/      Provider interfaces and implementations      │
│  ├── orchestration/ Workflow engine and patterns                 │
│  ├── tool/          Tool system and registry                     │
│  ├── memory/        Memory strategies and journal                │
│  ├── bus/           Inter-agent message bus                      │
│  ├── middleware/     Provider and tool middleware                 │
│  ├── message/       Core message types                           │
│  ├── config/        Configuration management                     │
│  ├── observability/ Tracing and metrics                          │
│  ├── rag/           Retrieval-augmented generation                │
│  └── testutil/      Test utilities                               │
└─────────────────────────────────────────────────────────────────┘
```

---

## Core Components

### Message System

The message system (`internal/message`) provides the fundamental data structures for all LLM interactions.

#### Key Types

| Type | Purpose |
|------|---------|
| `Role` | Message sender (system, user, assistant, tool) |
| `ContentBlock` | Multi-modal content (text, image, file) |
| `Message` | Complete message with metadata |
| `Conversation` | Ordered sequence of messages |
| `ToolCall` | Function invocation request |
| `ToolResult` | Function execution result |

#### Message Flow

```
┌──────────┐     ┌──────────┐     ┌──────────┐     ┌──────────┐
│  User    │────>│ Provider │────>│  Agent   │────>│  Memory  │
│  Input   │     │ Response │     │ Process  │     │  Store   │
└──────────┘     └──────────┘     └──────────┘     └──────────┘
                                        │
                                        v
                                 ┌──────────┐
                                 │   Tool   │
                                 │ Execute  │
                                 └──────────┘
```

#### SHA-Tracked Messages

Messages include a hash-based tracking system for compaction and auditability:

```go
type Message struct {
    // ... fields ...
    hash       string    // SHA-256 of message content
    parentHash string    // Hash of previous message
    createdAt  time.Time
}
```

---

### Provider Interface

The provider system (`internal/provider`) abstracts LLM provider differences.

#### Interface Definition

```go
type Provider interface {
    // Name returns the provider identifier (e.g., "openai", "anthropic")
    Name() string

    // Models returns available models with capabilities
    Models() []ModelInfo

    // Generate creates a completion from messages
    Generate(ctx context.Context, req GenerateRequest) (*GenerateResult, error)

    // Stream creates a streaming completion
    Stream(ctx context.Context, req GenerateRequest) (<-chan StreamEvent, error)

    // Capabilities returns provider-level capabilities
    Capabilities() ModelCapabilities
}
```

#### Provider Implementations

| Provider | Package | Models |
|----------|---------|--------|
| OpenAI | `internal/provider/openai` | GPT-4o, GPT-4-turbo, GPT-3.5-turbo |
| Anthropic | `internal/provider/anthropic` | Claude 3.5 Sonnet, Claude 3 Opus |
| Google Gemini | `internal/provider/gemini` | Gemini Pro, Gemini Ultra |
| Ollama | `internal/provider/ollama` | Any locally-hosted model |
| Mistral | `internal/provider/mistral` | Mistral Large, Mistral Medium |
| Cohere | `internal/provider/cohere` | Command R+, Command |
| Mock | `internal/provider/mock` | Deterministic testing |

#### Request/Response Lifecycle

```
┌────────────────────────────────────────────────────────────────┐
│                      Generate Request                           │
├────────────────────────────────────────────────────────────────┤
│  Model: "gpt-4o"                                               │
│  Messages: [System, User]                                       │
│  Tools: [search, calculator]                                    │
│  Options: {temperature: 0.7, max_tokens: 1000}                  │
└────────────────────────────────────────────────────────────────┘
                                │
                                v
┌────────────────────────────────────────────────────────────────┐
│                      Middleware Chain                           │
│  ┌────────┐  ┌────────┐  ┌────────┐  ┌────────┐  ┌────────┐   │
│  │ Logging│->│  Retry │->│ Rate   │->│ Circuit│->│ Cache  │   │
│  │        │  │        │  │ Limit  │  │ Breaker│  │        │   │
│  └────────┘  └────────┘  └────────┘  └────────┘  └────────┘   │
└────────────────────────────────────────────────────────────────┘
                                │
                                v
┌────────────────────────────────────────────────────────────────┐
│                      Generate Result                           │
├────────────────────────────────────────────────────────────────┤
│  ID: "chatcmpl-abc123"                                         │
│  Message: {role: assistant, content: "..."}                    │
│  Usage: {prompt: 100, completion: 50, total: 150}              │
│  FinishReason: "stop"                                          │
└────────────────────────────────────────────────────────────────┘
```

---

### Provider Registry

The registry provides centralized provider management:

```go
// Global registry
reg := provider.GlobalRegistry()

// Register providers
reg.Register("openai", func(cfg config.ProviderConfig) (Provider, error) {
    return openai.New(cfg)
})

// Resolve by model reference
p, model := reg.Resolve("openai::gpt-4o")

// Direct lookup
p := reg.Get("openai")
```

#### Alias Resolution

Model references support aliases for convenience:

```
openai::gpt-4o     -> Provider: openai, Model: gpt-4o
claude             -> Provider: anthropic, Model: claude-3.5-sonnet
local::llama3      -> Provider: ollama, Model: llama3
```

---

### Agent Runtime

The agent system (`internal/agent`) manages autonomous LLM interactions.

#### Agent Lifecycle

```
┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐
│ Created  │───>│ Started  │───>│ Running  │───>│Finished  │───>│ Returned │
│          │    │          │    │          │    │          │    │          │
│ New()    │    │ Run()    │    │ Loop     │    │          │    │ Result   │
└──────────┘    └──────────┘    └──────────┘    └──────────┘    └──────────┘
```

#### Execution Loop

The agent execution loop handles the iterative process of LLM calls and tool execution:

```go
for turn := 0; turn < a.maxTurns; turn++ {
    // 1. Build messages from conversation + memory
    messages := a.buildMessages(input)
    
    // 2. Generate LLM response
    result, err := a.provider.Generate(ctx, req)
    
    // 3. Check for tool calls
    if result.IsToolCall() {
        // 4. Execute tools
        for _, toolCall := range result.Message.ToolCalls {
            toolResult := a.tools.Execute(ctx, toolCall)
            // 5. Feed result back to conversation
            a.addToolResult(toolCall, toolResult)
        }
        continue  // Next turn
    }
    
    // 6. No tool calls - done
    return result
}
```

#### Event Streaming

Agents support streaming for real-time output:

```go
events, _ := agent.Stream(ctx, "Tell me a story")

for event := range events {
    switch event.Type {
    case EventGenerateChunk:
        fmt.Print(event.Chunk)
    case EventToolCall:
        fmt.Printf("Calling tool: %s\n", event.ToolCall.Function.Name)
    case EventToolResult:
        fmt.Printf("Result: %s\n", event.Result)
    case EventUsage:
        fmt.Printf("Tokens: %d\n", event.Usage.TotalTokens)
    }
}
```

---

### Orchestration Engine

The orchestration system (`internal/orchestration`) enables complex multi-agent workflows.

#### DAG-Based Workflows

Workflows are represented as directed acyclic graphs (DAGs):

```
    ┌─────────┐
    │  Start  │
    └────┬────┘
         │
    ┌────┴────┐
    │ Step A  │ (parallel with B)
    └────┬────┘
         │
    ┌────┴────┐
    │ Step B  │ (parallel with A)
    └────┬────┘
         │
    ┌────┴────┐
    │  Merge  │
    └────┬────┘
         │
    ┌────┴────┐
    │ Step C  │ (depends on A and B)
    └─────────┘
```

#### Workflow Builder

The fluent API enables declarative workflow construction:

```go
workflow := orchestration.NewBuilder("pipeline").
    AddStep("extract", extractAgent).
    AddStep("translate", translateAgent).
    AddStep("summarize", summarizeAgent).
    AddEdge("extract", "translate").
    AddEdge("translate", "summarize").
    Build()
```

#### Orchestration Patterns

| Pattern | Description | Use Case |
|---------|-------------|----------|
| **Sequential** | Linear step execution | Data pipelines |
| **Parallel Fan-Out/In** | Concurrent execution with merge | Batch processing |
| **Router** | Conditional branching based on input | Request classification |
| **Debate** | Multiple agents discuss and judge | Decision making |
| **Hierarchical** | Manager delegates to workers | Task decomposition |

#### Parallel Execution

The engine executes independent steps concurrently using goroutines:

```go
func (e *Engine) executeLevel(steps []string) []*StepResult {
    var wg sync.WaitGroup
    results := make([]*StepResult, len(steps))
    
    for i, stepID := range steps {
        wg.Add(1)
        go func(idx int, id string) {
            defer wg.Done()
            results[idx] = e.executeStep(id)
        }(i, stepID)
    }
    
    wg.Wait()
    return results
}
```

---

### Tool System

The tool system (`internal/tool`) enables agents to interact with external systems.

#### Tool Interface

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() json.RawMessage
    Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error)
}
```

#### Tool Registry

Tools are organized in a registry with namespace support:

```go
registry := tool.NewRegistry()

// Register tools
registry.MustRegister("search", searchTool)
registry.MustRegisterInNamespace("weather", "current", weatherTool)

// Execute tools
result, err := registry.Execute(ctx, "search", input)

// List all tools
tools := registry.Definitions()
```

#### Built-in Tools

| Tool | Namespace | Description |
|------|-----------|-------------|
| Lookup Message | `memory` | Retrieve messages from journal |
| (Extensible) | - | Custom tools via Tool interface |

#### Agent-as-Tool

Agents can be wrapped as tools for delegation:

```go
adapter := tool.NewAgentToolAdapter(researchAgent)
registry.MustRegister("research", adapter)

// Now other agents can call the research agent as a tool
```

---

### Memory System

The memory system (`internal/memory`) provides conversation persistence and context management.

#### Memory Interface

```go
type Memory interface {
    Add(ctx context.Context, msg Message) error
    GetRelevant(ctx context.Context, query string, limit int) ([]Message, error)
    GetAll(ctx context.Context) ([]Message, error)
    Clear(ctx context.Context) error
    Size() int
}
```

#### Memory Strategies

| Strategy | Description | Best For |
|----------|-------------|----------|
| **BufferMemory** | Stores all messages | Short conversations |
| **SlidingWindowMemory** | Keeps last N messages | Limited context |
| **SummaryMemory** | Summarizes old messages | Long conversations |
| **SemanticMemory** | Embedding-based retrieval | RAG applications |
| **CompositeMemory** | Combines strategies | Complex scenarios |
| **JournalMemory** | SHA-tracked session journal | Audit trails |

#### Session Journal

The journal provides immutable, hash-linked message storage:

```
┌─────────┐    ┌─────────┐    ┌─────────┐    ┌─────────┐
│ Message │───>│ Message │───>│ Message │───>│ Message │
│   #1    │    │   #2    │    │   #3    │    │   #4    │
│ SHA: A  │    │ SHA: B  │    │ SHA: C  │    │ SHA: D  │
│Parent:- │    │Parent:A │    │Parent:B │    │Parent:C │
└─────────┘    └─────────┘    └─────────┘    └─────────┘
```

#### Compaction Strategies

| Strategy | Trigger | Method |
|----------|---------|--------|
| **ThresholdCompaction** | Every N messages | LLM-generated summary |
| **TokenBudgetCompaction** | Token limit reached | Keep recent + summarize old |

---

### Message Bus

The message bus (`internal/bus`) enables inter-agent communication.

#### Bus Interface

```go
type Bus interface {
    Publish(ctx context.Context, msg BusMessage) error
    Subscribe(topic string, handler Handler) (Subscription, error)
    Unsubscribe(sub Subscription) error
    Close() error
}
```

#### Message Structure

```go
type BusMessage struct {
    ID        string            // Unique message ID
    Topic     string            // Routing topic
    FromAgent string            // Sender agent ID
    ToAgent   string            // Recipient agent ID (optional)
    Payload   []byte            // Message content
    Timestamp time.Time         // Send time
    Metadata  map[string]string // Additional metadata
}
```

#### Communication Patterns

| Pattern | Implementation | Use Case |
|---------|---------------|----------|
| **Direct** | Mailbox with agent ID | Point-to-point |
| **Broadcast** | Topic subscription | Announcements |
| **Request-Reply** | Correlation ID | RPC-style |
| **Pub-Sub** | Topic filtering | Event streaming |

---

### Middleware System

Middleware provides cross-cutting concerns for providers and tools.

#### Provider Middleware

```go
type ProviderMiddleware func(Provider) Provider
```

| Middleware | Purpose |
|------------|---------|
| `WithRetry` | Automatic retry with exponential backoff |
| `WithRateLimit` | Request rate limiting |
| `WithLogging` | Request/response logging |
| `WithCaching` | Response caching |
| `WithCircuitBreaker` | Failure protection |

#### Tool Middleware

```go
type ToolMiddleware func(Tool) Tool
```

| Middleware | Purpose |
|------------|---------|
| `WithToolLogging` | Execution logging |
| `WithToolMetrics` | Performance metrics |

---

## Configuration

Configuration is managed through YAML files with environment variable substitution:

```yaml
providers:
  openai:
    api_key: ${OPENAI_API_KEY}
    base_url: https://api.openai.com/v1
    default_model: gpt-4o
    rate_limit:
      requests_per_minute: 60
    retry:
      max_attempts: 3
      initial_backoff: 1s
```

### Configuration Hierarchy

1. Default values in code
2. Config file (`orchestra.yaml`)
3. Environment variables (`ORCHESTRA_*`)
4. Command-line flags

---

## Observability

### Structured Logging

Using Go's `log/slog` with context-aware logging:

```go
logger := slog.Default()
logger.Info("Agent started",
    "agent_id", agent.ID(),
    "model", agent.Model(),
    "turn", currentTurn,
)
```

### Distributed Tracing

Integration with OpenTelemetry for request tracing:

```go
tracer := otel.Tracer("orchestra")
ctx, span := tracer.Start(ctx, "agent.run")
defer span.End()
```

### Metrics

Prometheus-compatible metrics:

```go
meter := otel.Meter("orchestra")
counter := meter.Int64Counter("agent.runs.total")
counter.Add(ctx, 1)
```

---

## Error Handling

### Error Types

| Type | Purpose |
|------|---------|
| `ProviderError` | Provider-specific errors with code and status |
| `MaxTurnsError` | Agent exceeded turn limit |
| `WorkflowError` | Workflow execution failures |

### Error Wrapping

All errors are wrapped with context:

```go
return nil, fmt.Errorf("agent %q execution failed: %w", name, err)
```

### Recovery

Agents and workflows implement graceful degradation:

- Partial results available on `MaxTurnsError`
- Workflow results include all completed steps
- Circuit breakers prevent cascade failures

---

## Concurrency Model

### Goroutine Usage

| Component | Concurrency Pattern |
|-----------|---------------------|
| Agent | Sequential (single conversation) |
| Workflow Levels | Parallel (`sync.WaitGroup`) |
| Message Bus | Async (channels) |
| Providers | Thread-safe (connection pooling) |

### Thread Safety

- All components are safe for concurrent reads
- Providers must handle concurrent requests internally
- Memory implementations use mutex protection
- Tool registries use sync.RWMutex

---

## Extension Points

### Custom Providers

Implement the `Provider` interface:

```go
type MyProvider struct{}

func (p *MyProvider) Name() string { return "myprovider" }
func (p *MyProvider) Generate(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResult, error) {
    // Implementation
}
// ... other methods
```

### Custom Tools

Implement the `Tool` interface:

```go
type MyTool struct{}

func (t *MyTool) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
    // Implementation
}
```

### Custom Memory

Implement the `Memory` interface:

```go
type MyMemory struct{}

func (m *MyMemory) Add(ctx context.Context, msg message.Message) error {
    // Implementation
}
```

### Custom Middleware

Create middleware functions:

```go
func WithMyMiddleware() middleware.ProviderMiddleware {
    return func(next provider.Provider) provider.Provider {
        return &myMiddleware{next: next}
    }
}
```

---

## Performance Considerations

### Optimization Points

1. **Connection Pooling**: HTTP/2 connections for providers
2. **Response Caching**: Configurable TTL-based caching
3. **Parallel Execution**: Concurrent step processing in workflows
4. **Lazy Initialization**: On-demand provider setup

### Benchmarking

Critical paths are benchmarked:

```bash
make bench
```

### Memory Efficiency

- Object pooling for frequent allocations
- Stream processing for large responses
- Compaction for conversation history

---

## Security Considerations

### API Key Management

- Keys loaded from environment variables
- Never logged or exposed
- Config file uses variable substitution

### Input Validation

- JSON schema validation for tool inputs
- Content sanitization for user messages
- Rate limiting for abuse prevention

### Dependency Security

- Minimal external dependencies
- Regular `govulncheck` scanning
- `gosec` security analysis in CI
