# Contributing to Orchestra

Thank you for your interest in contributing to Orchestra! This document provides guidelines for contributing to the project.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Environment](#development-environment)
- [Development Workflow](#development-workflow)
- [Code Style](#code-style)
- [Writing Tests](#writing-tests)
- [Writing Documentation](#writing-documentation)
- [Pull Request Process](#pull-request-process)
- [Release Process](#release-process)
- [Architecture Guidelines](#architecture-guidelines)

---

## Code of Conduct

Be respectful, constructive, and inclusive. We follow the [Go Community Code of Conduct](https://go.dev/conduct).

---

## Getting Started

### Prerequisites

- **Go 1.25+** - Required for building and running tests
- **Make** (optional) - For build automation
- **Docker** (optional) - For containerized development
- **golangci-lint** - For linting (auto-installed by Makefile)

### Fork and Clone

```bash
# Fork the repository on GitHub, then:
git clone https://github.com/YOUR_USERNAME/orchestra.git
cd orchestra

# Add upstream remote
git remote add upstream https://github.com/user/orchestra.git
```

### Install Dependencies

```bash
go mod download
```

### Build

```bash
make build
# or
go build -o bin/orchestra ./cmd/orchestra
```

---

## Development Environment

### Project Structure

```
orchestra/
├── cmd/orchestra/      # CLI entry point
├── internal/           # Private packages
│   ├── agent/          # Agent runtime
│   ├── bus/            # Message bus
│   ├── config/         # Configuration
│   ├── memory/         # Memory strategies
│   ├── message/        # Core types
│   ├── middleware/      # Provider middleware
│   ├── observability/  # Tracing/metrics
│   ├── orchestration/  # Workflow engine
│   ├── provider/       # Provider interfaces
│   ├── rag/            # RAG support
│   └── tool/           # Tool system
├── pkg/orchestra/      # Public API
├── docs/               # Documentation
├── configs/            # Default configs
└── scripts/            # Build scripts
```

### IDE Setup

#### VS Code

Recommended extensions:
- Go (golang.go)
- Error Lens (usernamehw.errorlens)
- Go Test Explorer (leoluz.gotestexplorer)

#### GoLand

Import the project as a Go module. The IDE should automatically detect the project structure.

### Environment Variables

For local development with real providers:

```bash
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."
```

---

## Development Workflow

### Branch Naming

Use descriptive branch names:

```
feature/add-cohere-provider
fix/memory-leak-in-bus
docs/update-api-reference
refactor/simplify-tool-registry
```

### Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add Cohere provider support
fix: resolve race condition in message bus
docs: update architecture documentation
refactor: simplify tool registry interface
test: add integration tests for debate pattern
chore: update dependencies
```

### Keeping Up with Upstream

```bash
git fetch upstream
git checkout main
git merge upstream/main
```

---

## Code Style

Orchestra follows Go's standard style with additional conventions enforced by `golangci-lint`.

### Formatting

We use `gofmt` with strict settings:

```bash
make fmt      # Format all files
make fmt-check  # Verify formatting (CI)
```

### Linting

```bash
make lint      # Run all linters
make lint-fix  # Auto-fix where possible
```

### Naming Conventions

| Element | Convention | Example |
|---------|------------|---------|
| Packages | lowercase, single word | `agent`, `provider` |
| Interfaces | noun or adjective | `Provider`, `Memory` |
| Interface implementations | short, descriptive | `bufferMemory` |
| Functions | camelCase | `newAgent`, `executeStep` |
| Constants | camelCase (exported) | `MaxTurns`, `defaultModel` |
| Errors | `Err` prefix | `ErrProviderNotFound` |

### Documentation

All exported types and functions must have GoDoc comments:

```go
// Agent is the primary abstraction for interacting with LLM providers.
// An agent owns a provider, a system prompt template, a set of tools,
// optional memory, and middleware.
type Agent struct {
    // ...
}

// WithProvider sets the agent's LLM provider and model.
// If model is empty, the agent uses "default".
func WithProvider(p provider.Provider, model string) Option {
    // ...
}
```

### Error Handling

- Always handle errors explicitly
- Use `fmt.Errorf` with `%w` for wrapping
- Return early on errors to reduce nesting
- Never ignore errors with `_`

```go
// Good
result, err := agent.Run(ctx, input)
if err != nil {
    return nil, fmt.Errorf("agent execution failed: %w", err)
}

// Bad
result, _ := agent.Run(ctx, input)
```

### Context Usage

- Always accept `context.Context` as the first parameter
- Pass context through all call chains
- Check for cancellation in long-running operations

```go
func (a *Agent) Run(ctx context.Context, input string) (*AgentResult, error) {
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }
    // ...
}
```

---

## Writing Tests

### Test Organization

- Unit tests: Same package, `_test.go` suffix
- Integration tests: `//go:build integration` tag
- Examples: `examples_test.go` files

### Test Naming

```go
func TestAgent_Run_Success(t *testing.T) { ... }
func TestAgent_Run_MaxTurnsExceeded(t *testing.T) { ... }
func TestAgent_Run_ContextCancelled(t *testing.T) { ... }
```

### Test Structure

Use table-driven tests where appropriate:

```go
func TestAdd(t *testing.T) {
    tests := []struct {
        name     string
        a, b     int
        expected int
    }{
        {"positive", 1, 2, 3},
        {"negative", -1, -2, -3},
        {"zero", 0, 0, 0},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := Add(tt.a, tt.b)
            if result != tt.expected {
                t.Errorf("Add(%d, %d) = %d, want %d", tt.a, tt.b, result, tt.expected)
            }
        })
    }
}
```

### Mocking

Use the mock provider for testing agents and workflows:

```go
func TestAgent_WithMockProvider(t *testing.T) {
    mockProvider := mock.New()
    mockProvider.SetResponse("Hello, world!")

    agent, err := agent.New("test",
        agent.WithProvider(mockProvider, "test"),
    )
    // ...
}
```

### Running Tests

```bash
make test          # Run all tests
make test-verbose  # Verbose output
make cover         # Coverage report
make cover-terminal  # Coverage in terminal
```

### Test Tags

```bash
# Unit tests only
go test ./...

# Integration tests
go test -tags=integration ./...

# Benchmarks
go test -bench=. ./...
```

### Coverage

We aim for 80%+ coverage. Check coverage:

```bash
make cover-terminal

# Or with HTML report
make cover
open coverage.html
```

---

## Writing Documentation

### GoDoc

All exported symbols must have documentation:

```go
// New creates a new agent with the given name and options.
//
// The name is used for logging and debugging. It must be non-empty.
//
// Returns an error if required options are missing or invalid.
func New(name string, opts ...Option) (*Agent, error) {
    // ...
}
```

### Examples

Add examples in `examples_test.go`:

```go
func ExampleNew() {
    agent, err := New("assistant",
        WithProvider(mock.New(), "test"),
        WithSystemPrompt("You are helpful."),
    )
    if err != nil {
        log.Fatal(err)
    }
    _ = agent // Output:
}
```

### README Updates

Update the README when:
- Adding new features
- Changing public API
- Updating dependencies
- Adding new examples

### Documentation Files

Documentation lives in `docs/`:
- `ARCHITECTURE.md` - Technical architecture
- `CONTRIBUTING.md` - This file
- `PLAN.md` - Project roadmap
- `PHASE*.md` - Phase reports

---

## Pull Request Process

### Before Submitting

1. [ ] Run all tests: `make test`
2. [ ] Run linter: `make lint`
3. [ ] Check formatting: `make fmt-check`
4. [ ] Update documentation if needed
5. [ ] Add tests for new functionality
6. [ ] Squash related commits

### PR Description Template

```markdown
## Summary
Brief description of changes.

## Changes
- Change 1
- Change 2

## Testing
- [ ] Unit tests added/updated
- [ ] Integration tests (if applicable)
- [ ] Manual testing performed

## Documentation
- [ ] GoDoc updated
- [ ] README updated (if needed)
- [ ] Examples added (if applicable)
```

### Review Process

1. Automated CI must pass
2. At least one maintainer approval required
3. Address all review comments
4. Squash and merge when approved

### CI Checks

Pull requests are checked by:
- Go formatting (`gofmt`, `gofumpt`)
- Go vet (standard + shadow)
- golangci-lint (100+ linters)
- Unit tests with race detection
- Integration tests
- Security scanning (govulncheck, gosec)
- Coverage threshold (80%)

---

## Release Process

Releases follow [Semantic Versioning](https://semver.org/).

### Version Numbers

- **MAJOR**: Breaking changes
- **MINOR**: New features (backward compatible)
- **PATCH**: Bug fixes (backward compatible)

### Release Steps

1. Update `CHANGELOG.md`
2. Create release branch: `release/vX.Y.Z`
3. Update version in code
4. Push tag: `vX.Y.Z`
5. CI creates release with binaries

### Changelog Format

```markdown
## [0.2.0] - 2024-01-15

### Added
- New feature description

### Changed
- Change description

### Fixed
- Bug fix description

### Breaking
- Breaking change description
```

---

## Architecture Guidelines

### Interface Design

- Keep interfaces small (prefer multiple small interfaces)
- Accept interfaces, return concrete types
- Design for composition

```go
// Good: Small, focused interfaces
type Reader interface {
    Read(ctx context.Context, id string) ([]byte, error)
}

type Writer interface {
    Write(ctx context.Context, id string, data []byte) error
}

// Bad: Large interface
type Store interface {
    Read(ctx context.Context, id string) ([]byte, error)
    Write(ctx context.Context, id string, data []byte) error
    Delete(ctx context.Context, id string) error
    List(ctx context.Context) ([]string, error)
    // ... many more methods
}
```

### Error Design

- Define package-level sentinel errors
- Use custom error types for rich error information
- Wrap errors with context

```go
var (
    ErrProviderNotFound = errors.New("provider not found")
    ErrModelNotFound    = errors.New("model not found")
    ErrMaxTurns         = errors.New("max turns exceeded")
)

type ProviderError struct {
    Provider  string
    Model     string
    Code      string
    Err       error
}

func (e *ProviderError) Error() string {
    return fmt.Sprintf("provider %q model %q: %s", e.Provider, e.Model, e.Err)
}

func (e *ProviderError) Unwrap() error {
    return e.Err
}
```

### Concurrency

- Document thread-safety requirements
- Use `sync.RWMutex` for read-heavy access
- Prefer channels for communication
- Use `context.Context` for cancellation

```go
type SafeCounter struct {
    mu    sync.RWMutex
    count int
}

func (c *SafeCounter) Increment() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.count++
}

func (c *SafeCounter) Value() int {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.count
}
```

### Configuration

- Use functional options for configuration
- Provide sensible defaults
- Validate configuration early

```go
type Option func(*Agent) error

func WithMaxTurns(n int) Option {
    return func(a *Agent) error {
        if n <= 0 {
            return fmt.Errorf("max turns must be positive, got %d", n)
        }
        a.maxTurns = n
        return nil
    }
}
```

---

## Getting Help

- Open a GitHub issue for bugs or feature requests
- Use Discussions for questions
- Check existing issues before creating new ones
- Join our community chat (if available)

Thank you for contributing to Orchestra!
