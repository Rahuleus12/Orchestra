# Orchestra — Multi-Agent AI Orchestration Engine

[![Go Reference](https://pkg.go.dev/badge/github.com/user/orchestra.svg)](https://pkg.go.dev/github.com/user/orchestra)

[![Go Report Card](https://goreportcard.com/badge/github.com/user/orchestra)](https://goreportcard.com/report/github.com/user/orchestra)

Orchestra is a Go-based framework for orchestrating multiple AI agents that use different LLM providers (OpenAI, Anthropic, Google Gemini, Ollama, Mistral, Cohere, etc.) and models. It provides a unified abstraction layer over heterogeneous LLM backends and enables complex multi-agent workflows including sequential pipelines, parallel fan-out/fan-in, dynamic routing, debate loops, and hierarchical delegation.

## Features

- **Provider-agnostic** — Uniform Go interface across all LLM providers and models
- **Composable agents** — Agents that can be combined into arbitrarily complex workflows
- **First-class Go idioms** — Interfaces, context propagation, error wrapping, goroutine-based concurrency
- **Extensible** — Easy to add new providers, tools, memory backends, and orchestration patterns
- **Observable** — Built-in tracing, metrics, and structured logging from day one
- **Production-grade** — Connection pooling, rate limiting, retry logic, graceful shutdown
- **Testable** — Mock providers, deterministic testing helpers, and integration test suites
- **Streaming-native** — Full support for streaming tokens from all providers

## Quick Start

### Installation

```bash
go get github.com/user/orchestra
```

### Basic Usage

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/user/orchestra/internal/config"
    "github.com/user/orchestra/internal/message"
    "github.com/user/orchestra/internal/provider"
    "github.com/user/orchestra/internal/provider/mock"
)

func main() {
    ctx := context.Background()

    // Register a provider
    mp := mock.NewProvider("openai")
    provider.GlobalRegistry.MustRegisterProvider("openai", mp)

    // Generate a completion
    result, err := mp.Generate(ctx, provider.GenerateRequest{
        Model: "mock-model",
        Messages: []message.Message{
            message.SystemMessage("You are a helpful assistant."),
            message.UserMessage("What is 2 + 2?"),
        },
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(result.Text())
}
```

### Configuration

Load configuration from YAML with environment variable interpolation:

```go
cfg, err := config.LoadFromFile("configs/orchestra.yaml")
if err != nil {
    log.Fatal(err)
}

// Or from environment variables only:
cfg := config.LoadFromEnv()

// Or merge multiple sources:
cfg := config.Merge(fileConfig, envConfig)
```

### Provider Registry

Resolve model references across providers:

```go
registry := provider.NewRegistry()

// Register provider factories (lazy initialization)
registry.MustRegister("openai", openaiFactory, config.ProviderConfig{
    DefaultModel: "gpt-4-turbo",
})

// Register aliases
registry.MustAlias("gpt4", "openai::gpt-4-turbo")
registry.MustAlias("claude", "anthropic::claude-sonnet-4-20250514")

// Resolve by provider::model, alias, or bare model name
p, model, err := registry.Resolve("gpt4")     // alias → openai::gpt-4-turbo
p, model, err := registry.Resolve("openai::gpt-4o")  // explicit
p, model, err := registry.Resolve("gpt-4-turbo")     // default model match
```

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                      Application                         │
│                                                          │
│  ┌────────────┐  ┌──────────────────────────────────┐   │
│  │  Workflow   │  │         Orchestration Engine      │   │
│  │  Builder    │  │  (DAG executor, scheduler, router)│   │
│  └──────┬─────┘  └──────────┬───────────────────────┘   │
│         │                   │                            │
│  ┌──────┴───────────────────┴────────────────────┐      │
│  │              Agent Runtime                      │      │
│  │  (lifecycle, prompt assembly, tool dispatch)   │      │
│  └──────────────────┬────────────────────────────┘      │
│                     │                                    │
│  ┌──────────────────┴────────────────────────────┐      │
│  │             Provider Layer                      │      │
│  │  ┌──────┐ ┌─────────┐ ┌───────┐ ┌──────────┐  │      │
│  │  │OpenAI│ │Anthropic│ │Gemini │ │ Ollama   │  │      │
│  │  └──────┘ └─────────┘ └───────┘ └──────────┘  │      │
│  │  ┌──────┐ ┌─────────┐ ┌─────────────────────┐ │      │
│  │  │Mistral│ │Cohere  │ │ Custom / HTTP       │ │      │
│  │  └──────┘ └─────────┘ └─────────────────────┘ │      │
│  └─────────────────────────────────────────────────┘      │
│                                                          │
│  ┌───────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │  Tool System   │  │    Memory     │  │   Message     │  │
│  │  (functions)   │  │   (context)   │  │     Bus       │  │
│  └───────────────┘  └──────────────┘  └──────────────┘  │
│                                                          │
│  ┌───────────────────────────────────────────────────┐  │
│  │            Observability (trace, metrics, logs)    │  │
│  └───────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

## Project Structure

```
Orchestra/
├── cmd/
│   └── orchestra/                # CLI and server entry point
│       └── main.go
├── docs/
│   ├── PLAN.md                   # Full project plan and phase breakdown
│   ├── ARCHITECTURE.md           # Detailed architecture docs
│   └── ADR/                      # Architecture Decision Records
├── internal/
│   ├── message/                  # Message types and conversation history
│   │   ├── message.go            # Message, ContentBlock, Conversation types
│   │   ├── role.go               # Role type and constants
│   │   └── message_test.go       # Comprehensive table-driven tests
│   ├── provider/                 # Provider interface and registry
│   │   ├── provider.go           # Provider interface, GenerateRequest/Result, StreamEvent
│   │   ├── registry.go           # Thread-safe registry with factories and aliases
│   │   ├── registry_test.go      # Registry tests
│   │   ├── mock/                 # Mock provider for testing
│   │   │   ├── provider.go       # Configurable mock with call tracking
│   │   │   └── provider_test.go  # Mock provider tests
│   │   ├── openai/               # (Phase 2) OpenAI provider
│   │   ├── anthropic/            # (Phase 2) Anthropic provider
│   │   ├── gemini/               # (Phase 2) Google Gemini provider
│   │   └── ollama/               # (Phase 2) Ollama provider
│   ├── agent/                    # (Phase 3) Agent runtime and lifecycle
│   ├── orchestration/            # (Phase 4) Workflow engine and patterns
│   ├── tool/                     # (Phase 6) Tool/function calling system
│   ├── memory/                   # (Phase 7) Context and memory management
│   ├── bus/                      # (Phase 5) Inter-agent message bus
│   ├── prompt/                   # (Phase 3) Prompt templates and assembly
│   ├── middleware/                # (Phase 2) Cross-cutting concerns
│   ├── config/                   # Configuration management
│   │   ├── config.go             # Config types and validation
│   │   ├── loader.go             # YAML loading with env var interpolation
│   │   └── config_test.go        # Config and loader tests
│   └── observe/                  # (Phase 8) Observability
├── pkg/
│   └── orchestra/                # Public API surface (re-exports)
├── configs/
│   └── orchestra.yaml            # Default configuration
├── scripts/
│   ├── build.sh
│   ├── test.sh
│   └── lint.sh
├── go.mod
├── Makefile
├── Dockerfile
├── docker-compose.yaml
├── .golangci.yml
└── README.md
```

## Core Concepts

### Messages

Messages are the fundamental unit of communication with LLM providers.

```go
// Simple text messages
sys := message.SystemMessage("You are a helpful assistant.")
user := message.UserMessage("What is 2 + 2?")
assistant := message.AssistantMessage("The answer is 4.")

// Multi-modal content
msg := message.Message{
    Role: message.RoleUser,
    Content: []message.ContentBlock{
        message.TextContentBlock("Describe this image:"),
        message.ImageContentBlock("https://example.com/photo.jpg", "image/jpeg"),
    },
}

// Tool calls
toolCallMsg := message.AssistantToolCallMessage([]message.ToolCall{
    {
        ID:   "call_001",
        Type: "function",
        Function: message.ToolCallFunction{
            Name:      "get_weather",
            Arguments: `{"city": "London"}`,
        },
    },
})

// Tool results
toolResultMsg := message.ToolResultMessage("call_001", `{"temp": 15, "condition": "rainy"}`, false)
```

### Conversations

Conversations manage ordered sequences of messages with utilities for filtering, truncation, and formatting.

```go
conv := message.NewConversation()
conv.Add(
    message.SystemMessage("You are a math tutor."),
    message.UserMessage("What is 2 + 2?"),
    message.AssistantMessage("2 + 2 = 4."),
)

// Filter by role
userMessages := conv.FilterByRole(message.RoleUser)

// Truncate while preserving system messages
recent := conv.TruncatePreservingSystem(10)

// Format for display
fmt.Println(conv.Format())
```

### Provider Interface

All LLM backends implement the `Provider` interface:

```go
type Provider interface {
    Name() string
    Models(ctx context.Context) ([]ModelInfo, error)
    Generate(ctx context.Context, req GenerateRequest) (*GenerateResult, error)
    Stream(ctx context.Context, req GenerateRequest) (<-chan StreamEvent, error)
    Capabilities(model string) ModelCapabilities
}
```

### Provider Registry

The registry manages provider lifecycle with lazy initialization and model resolution:

```go
registry := provider.NewRegistry()

// Register with lazy factory
registry.MustRegister("openai", func(cfg ProviderConfig) (Provider, error) {
    return openai.NewProvider(cfg)
}, ProviderConfig{
    APIKey:       "sk-...",
    DefaultModel: "gpt-4-turbo",
})

// Resolve model references
p, modelID, err := registry.Resolve("openai::gpt-4-turbo")
```

### Configuration

Configuration supports YAML files, environment variables, and programmatic setup:

```yaml
# configs/orchestra.yaml
providers:
  openai:
    api_key: ${OPENAI_API_KEY}
    base_url: https://api.openai.com/v1
    default_model: gpt-4-turbo
    rate_limit:
      requests_per_minute: 60
      tokens_per_minute: 150000
    retry:
      max_attempts: 3
      initial_backoff: 1s

  anthropic:
    api_key: ${ANTHROPIC_API_KEY}
    default_model: claude-sonnet-4-20250514

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

Environment variable overrides use the `ORCHESTRA_` prefix:

```bash
export ORCHESTRA_PROVIDER_OPENAI_API_KEY=sk-your-key
export ORCHESTRA_PROVIDER_OPENAI_DEFAULT_MODEL=gpt-4o
export ORCHESTRA_LOGGING_LEVEL=debug
export ORCHESTRA_TRACING_ENABLED=true
```

### Mock Provider

The mock provider is designed for testing:

```go
mp := mock.NewProvider("test")

// Configure responses
mp.AddResponse(mock.MockResponse{
    Message:      message.AssistantMessage("Hello!"),
    FinishReason: provider.FinishReasonStop,
    Usage:        provider.TokenUsage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7},
})

// Configure streaming
mp.AddStreamChunks([]mock.StreamChunk{
    {Type: provider.StreamEventStart},
    {Type: provider.StreamEventChunk, Chunk: "Hello "},
    {Type: provider.StreamEventChunk, Chunk: "world!"},
    {Type: provider.StreamEventDone, Usage: &provider.TokenUsage{TotalTokens: 7}},
})

// Use it
result, err := mp.Generate(ctx, provider.GenerateRequest{
    Model:    "mock-model",
    Messages: []message.Message{message.UserMessage("hi")},
})

// Inspect calls
calls := mp.GenerateCalls()
lastReq, _ := mp.LastGenerateCall()
```

## Development

### Prerequisites

- Go 1.24+
- Make (optional, for build commands)
- Docker (optional, for containerized builds)
- golangci-lint (optional, for linting)

### Building

```bash
# Build the binary
make build

# Or directly with Go
go build -o bin/orchestra ./cmd/orchestra
```

### Testing

```bash
# Run all tests
make test

# Run with verbose output
make test-verbose

# Run with coverage
make cover

# Run specific package tests
go test ./internal/message/... -v
go test ./internal/provider/... -v
go test ./internal/config/... -v
```

### Linting

```bash
# Run all linters
make lint

# Auto-fix issues
make lint-fix

# Check formatting
make fmt-check
```

### Docker

```bash
# Build and run with Docker Compose
docker compose up orchestra

# Build the image manually
docker build -t orchestra .

# Run with environment variables
docker run -e OPENAI_API_KEY=sk-... orchestra
```

### CI

The GitHub Actions pipeline runs on every push and pull request:

1. **Format check** — Verifies `gofmt` compliance
2. **Go vet** — Static analysis
3. **Lint** — golangci-lint with strict rules
4. **Test** — Cross-platform (Linux, Windows, macOS) with race detector
5. **Build** — Multi-platform binaries (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64)
6. **Coverage** — Coverage report with threshold check
7. **Docker** — Verify Dockerfile builds correctly

## Roadmap

| Phase | Description | Status |
|-------|-------------|--------|
| 1 | Foundation & Core Abstractions | ✅ Complete |
| 2 | Provider Integrations (OpenAI, Anthropic, Gemini, Ollama, Mistral, Cohere) | ✅ Complete |
| 3 | Agent Runtime & Lifecycle | 🔲 Planned |
| 4 | Orchestration Engine (DAG-based workflows) | 🔲 Planned |
| 5 | Inter-Agent Communication (Message Bus) | 🔲 Planned |
| 6 | Tool System & Function Calling | 🔲 Planned |
| 7 | Memory & Context Management | 🔲 Planned |
| 8 | Observability & Operations | 🔲 Planned |
| 9 | Advanced Patterns (RAG, Self-Reflection, HITL) | 🔲 Planned |
| 10 | Production Readiness | 🔲 Planned |

See [docs/PLAN.md](docs/PLAN.md) for the detailed project plan.

## Design Principles

1. **Interface-driven design** — Every major component is defined as a Go interface
2. **Context propagation** — `context.Context` flows through every call for cancellation, timeouts, and tracing
3. **Immutable message types** — Messages, configurations, and workflow definitions are value types
4. **Concurrency-safe** — All shared state uses proper synchronization; agents are independently schedulable
5. **Fail-fast with graceful degradation** — Errors are surfaced immediately; partial results are recoverable

## License

This project is currently in development. License information will be added before public release.
