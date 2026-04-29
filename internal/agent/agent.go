// Package agent implements the Agent abstraction — the primary building block
// that users interact with in the Orchestra multi-agent orchestration engine.
//
// An agent owns a provider, a system prompt template, a set of tools,
// optional memory, and middleware. It manages the full execution lifecycle:
// generating LLM responses, executing tool calls, feeding results back,
// and producing a final response.
//
// # Quick Start
//
//	agent, err := agent.New("assistant",
//	    agent.WithProvider(openaiProvider, "gpt-4o"),
//	    agent.WithSystemPrompt("You are a helpful assistant."),
//	    agent.WithMaxTurns(10),
//	)
//	if err != nil { ... }
//
//	result, err := agent.Run(ctx, "Hello, world!")
//	fmt.Println(result.Output.Text())
//
// # Streaming
//
//	events, err := agent.Stream(ctx, "Tell me a story")
//	for evt := range events {
//	    if evt.Type == agent.EventGenerateChunk {
//	        fmt.Print(evt.Chunk)
//	    }
//	}
package agent

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/user/orchestra/internal/message"
	"github.com/user/orchestra/internal/middleware"
	"github.com/user/orchestra/internal/provider"
)

// Memory is a minimal interface for agent memory. Implementations provide
// persistent storage and retrieval of messages across agent runs.
// The full memory system with multiple strategies (conversation buffer,
// summary, sliding window, vector) will be implemented in Phase 7.
//
// Implementations must be safe for concurrent use.
type Memory interface {
	// Add stores a message in memory.
	Add(ctx context.Context, msg message.Message) error

	// GetRelevant retrieves messages relevant to the given query,
	// up to the specified limit. Relevance is implementation-defined
	// (e.g., recency, semantic similarity).
	GetRelevant(ctx context.Context, query string, limit int) ([]message.Message, error)

	// GetAll retrieves all messages stored in memory.
	GetAll(ctx context.Context) ([]message.Message, error)

	// Clear removes all messages from memory.
	Clear(ctx context.Context) error

	// Size returns the number of messages stored in memory.
	Size() int
}

const (
	// defaultMaxTurns is the default maximum number of provider calls
	// an agent will make in a single execution before returning an error.
	defaultMaxTurns = 25

	// defaultModel is the model identifier used when none is specified.
	defaultModel = "default"
)

// MaxTurnsError is returned when an agent execution exceeds the configured
// maximum number of turns. The Partial field contains all data collected
// before the limit was reached.
type MaxTurnsError struct {
	// AgentName is the name of the agent that hit the limit.
	AgentName string

	// MaxTurns is the configured maximum.
	MaxTurns int

	// Partial contains the partial execution result collected so far.
	Partial *AgentResult
}

// Error implements the error interface.
func (e *MaxTurnsError) Error() string {
	return fmt.Sprintf("agent %q exceeded max turns (%d)", e.AgentName, e.MaxTurns)
}

// PartialResult returns the partial execution result collected before the
// max turns limit was reached. Callers can use this to extract partial data
// for graceful degradation.
func (e *MaxTurnsError) PartialResult() *AgentResult {
	return e.Partial
}

// Agent is the primary abstraction for interacting with LLM providers.
// An agent owns a provider, a system prompt template, a set of tools,
// optional memory, and middleware. It manages the full execution lifecycle:
// generating responses, executing tool calls, and feeding results back.
//
// Agents are designed to be created once and reused for multiple executions.
// Use Clone to create independent copies for parallel execution.
//
// An Agent is safe for concurrent use as long as the underlying Provider
// and Memory implementations are also safe for concurrent use. The Agent
// struct itself does not hold mutable per-execution state.
type Agent struct {
	id         string
	name       string
	provider   provider.Provider
	model      string
	system     *Template
	systemData any
	tools      *ToolRegistry
	memory     Memory
	maxTurns   int
	middleware []middleware.ProviderMiddleware
	genOptions []provider.GenerateOption
	logger     *slog.Logger
}

// Option is a functional option for configuring an Agent. Options are applied
// in order during agent creation via New.
type Option func(*Agent) error

// WithProvider sets the agent's LLM provider and model. This is required
// for all agents. If model is empty, the agent uses "default".
func WithProvider(p provider.Provider, model string) Option {
	return func(a *Agent) error {
		if p == nil {
			return fmt.Errorf("provider must not be nil")
		}
		a.provider = p
		if model != "" {
			a.model = model
		}
		return nil
	}
}

// WithModel resolves a model reference string (e.g., "openai::gpt-4o")
// using the global provider registry and sets the resolved provider and model.
// This is a convenience option that combines provider lookup and model selection.
func WithModel(modelRef string) Option {
	return func(a *Agent) error {
		p, model, err := provider.Resolve(modelRef)
		if err != nil {
			return fmt.Errorf("resolve model %q: %w", modelRef, err)
		}
		a.provider = p
		a.model = model
		return nil
	}
}

// WithSystemPrompt sets the agent's system prompt from a template string.
// The string is parsed as a Go text/template with built-in functions
// (see Template documentation for available functions).
func WithSystemPrompt(tmpl string) Option {
	return func(a *Agent) error {
		t, err := NewTemplate("system", tmpl)
		if err != nil {
			return fmt.Errorf("parse system prompt template: %w", err)
		}
		a.system = t
		return nil
	}
}

// WithSystemPromptFile loads the agent's system prompt template from a file.
// The file content is parsed as a Go text/template.
func WithSystemPromptFile(path string) Option {
	return func(a *Agent) error {
		t, err := LoadTemplateFile(path)
		if err != nil {
			return fmt.Errorf("load system prompt from %q: %w", path, err)
		}
		a.system = t
		return nil
	}
}

// WithSystemData sets the data passed to the system prompt template when
// rendering. Use this when your system prompt template contains variables
// (e.g., {{.Role}}, {{.Context}}). The data can be any Go value — typically
// a struct or a map[string]any.
func WithSystemData(data any) Option {
	return func(a *Agent) error {
		a.systemData = data
		return nil
	}
}

// WithTools adds one or more tools to the agent's tool registry. If a tool
// registry does not yet exist, one is created. Tools are made available to
// the LLM for function calling during execution.
func WithTools(tools ...Tool) Option {
	return func(a *Agent) error {
		if a.tools == nil {
			a.tools = NewToolRegistry()
		}
		for _, t := range tools {
			if err := a.tools.Register(t); err != nil {
				return fmt.Errorf("register tool %q: %w", t.Name(), err)
			}
		}
		return nil
	}
}

// WithToolRegistry sets a pre-built tool registry on the agent.
// This replaces any previously registered tools.
func WithToolRegistry(registry *ToolRegistry) Option {
	return func(a *Agent) error {
		a.tools = registry
		return nil
	}
}

// WithMemory sets the agent's memory implementation. Memory provides
// persistent context across agent runs, enabling the agent to recall
// previous interactions.
func WithMemory(m Memory) Option {
	return func(a *Agent) error {
		a.memory = m
		return nil
	}
}

// WithMaxTurns sets the maximum number of provider calls the agent will
// make in a single execution. This prevents infinite tool-calling loops.
// The default is 25. Set to 1 for single-turn (no tool calling) execution.
func WithMaxTurns(n int) Option {
	return func(a *Agent) error {
		if n <= 0 {
			return fmt.Errorf("max turns must be positive, got %d", n)
		}
		a.maxTurns = n
		return nil
	}
}

// WithMiddleware adds provider middleware to the agent. Middleware is applied
// to the provider before each execution, enabling cross-cutting concerns
// like retry, rate limiting, logging, caching, and circuit breaking.
//
// Multiple WithMiddleware calls accumulate; all middleware is applied in order.
func WithMiddleware(m ...middleware.ProviderMiddleware) Option {
	return func(a *Agent) error {
		a.middleware = append(a.middleware, m...)
		return nil
	}
}

// WithGenerateOptions sets default generation options for all provider calls
// made by this agent. These configure parameters like temperature, max tokens,
// stop sequences, etc.
//
// Multiple WithGenerateOptions calls accumulate.
func WithGenerateOptions(opts ...provider.GenerateOption) Option {
	return func(a *Agent) error {
		a.genOptions = append(a.genOptions, opts...)
		return nil
	}
}

// WithLogger sets the agent's structured logger. If not set, slog.Default()
// is used.
func WithLogger(logger *slog.Logger) Option {
	return func(a *Agent) error {
		if logger == nil {
			return fmt.Errorf("logger must not be nil")
		}
		a.logger = logger
		return nil
	}
}

// New creates a new Agent with the given name and configuration options.
// At minimum, a provider must be specified via WithProvider or WithModel.
//
// Name must be non-empty and is used for identification in logs, metrics,
// and error messages.
//
// Returns an error if required options are missing or if any option returns
// an error during application. Options are applied in order; if one fails,
// subsequent options are not applied.
func New(name string, opts ...Option) (*Agent, error) {
	if name == "" {
		return nil, fmt.Errorf("agent name is required")
	}

	a := &Agent{
		id:       generateAgentID(),
		name:     name,
		model:    defaultModel,
		maxTurns: defaultMaxTurns,
		logger:   slog.Default(),
	}

	for _, opt := range opts {
		if err := opt(a); err != nil {
			return nil, fmt.Errorf("agent %q: %w", name, err)
		}
	}

	if a.provider == nil {
		return nil, fmt.Errorf("agent %q: provider is required (use WithProvider or WithModel)", name)
	}

	return a, nil
}

// Run executes the agent with a simple text input. It assembles the full
// conversation context — system prompt, memory, and user input — then runs
// the execution loop (generate → tool call → generate) until a final
// response is produced or the maximum number of turns is reached.
//
// The context can be used for cancellation and timeouts. If the context is
// cancelled during a provider call, the error is returned immediately.
//
// Returns an AgentResult with the final output, full conversation trace,
// tool execution details, aggregate token usage, and timing information.
func (a *Agent) Run(ctx context.Context, input string) (*AgentResult, error) {
	msgs, err := a.buildMessages(ctx, input)
	if err != nil {
		return nil, err
	}
	return a.RunConversation(ctx, msgs)
}

// RunConversation executes the agent with a pre-built conversation. The
// provided messages are used as-is; the agent's system prompt and memory
// are NOT automatically prepended. Use this for:
//   - Multi-turn conversations where you control the full context
//   - Continuing a previous conversation
//   - When you've already assembled messages via buildMessages
//
// The execution loop continues until the model produces a response without
// tool calls, the maximum number of turns is reached, or the context is
// cancelled.
func (a *Agent) RunConversation(ctx context.Context, messages []message.Message) (*AgentResult, error) {
	start := time.Now()

	conv := message.NewConversation()
	conv.Add(messages...)

	result := &AgentResult{
		Conversation: conv,
		ToolCalls:    []ToolExecution{},
	}

	wrappedProvider := a.applyMiddleware(a.provider)

	for turn := 0; turn < a.maxTurns; turn++ {
		// Check for context cancellation before each turn
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Build the generation request
		req := provider.GenerateRequest{
			Model:    a.model,
			Messages: conv.Messages,
			Options:  provider.NewGenerateOptions(a.genOptions...),
		}
		if a.tools != nil && a.tools.Size() > 0 {
			req.Tools = a.tools.Definitions()
		}

		a.logger.Debug("agent generating",
			slog.String("agent", a.name),
			slog.Int("turn", turn),
			slog.String("model", a.model),
			slog.Int("messages", len(conv.Messages)),
		)

		// Call the provider
		genResult, err := wrappedProvider.Generate(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("agent %q turn %d: generate: %w", a.name, turn, err)
		}

		// Track usage
		result.Usage = result.Usage.Add(genResult.Usage)
		result.Turns = turn + 1

		// Add the assistant message to the conversation
		conv.Add(genResult.Message)

		// Check for tool calls
		if genResult.IsToolCall() && len(genResult.Message.ToolCalls) > 0 {
			a.logger.Debug("agent tool calls",
				slog.String("agent", a.name),
				slog.Int("turn", turn),
				slog.Int("tool_calls", len(genResult.Message.ToolCalls)),
			)

			// Execute each tool call
			for _, tc := range genResult.Message.ToolCalls {
				toolStart := time.Now()
				toolMsg := executeToolCall(ctx, a.tools, tc)
				toolDuration := time.Since(toolStart)

				var toolErr error
				if toolMsg.ToolResult != nil && toolMsg.ToolResult.IsError {
					toolErr = fmt.Errorf("tool error: %s", toolMsg.ToolResult.Content)
				}

				toolExec := ToolExecution{
					Turn:     turn,
					Call:     tc,
					Duration: toolDuration,
					Error:    toolErr,
				}
				if toolMsg.ToolResult != nil {
					toolExec.Result = *toolMsg.ToolResult
				}

				result.ToolCalls = append(result.ToolCalls, toolExec)

				a.logger.Debug("tool executed",
					slog.String("agent", a.name),
					slog.String("tool", tc.Function.Name),
					slog.Duration("duration", toolDuration),
					slog.Bool("error", toolErr != nil),
				)

				conv.Add(toolMsg)
			}

			// Continue the loop for another generation
			continue
		}

		// No tool calls — we have a final response
		result.Output = genResult.Message
		result.Duration = time.Since(start)

		// Store the interaction in memory if configured
		if a.memory != nil {
			_ = a.memory.Add(ctx, message.UserMessage(conv.Messages[0].Text()))
			_ = a.memory.Add(ctx, genResult.Message)
		}

		a.logger.Debug("agent completed",
			slog.String("agent", a.name),
			slog.Int("turns", result.Turns),
			slog.Duration("duration", result.Duration),
		)

		return result, nil
	}

	// Max turns exceeded — return partial result in the error
	result.Duration = time.Since(start)
	return nil, &MaxTurnsError{
		AgentName: a.name,
		MaxTurns:  a.maxTurns,
		Partial:   result,
	}
}

// Stream executes the agent with streaming support, emitting events as they
// occur. This is the streaming equivalent of Run — it assembles the full
// context and runs the execution loop, but delivers intermediate results
// in real-time via a channel of AgentEvent values.
//
// Events are delivered on the returned channel. The channel is closed when
// execution completes (successfully or with an error). The final event
// will be EventDone on success or EventError on failure.
//
// The caller must drain the channel to prevent goroutine leaks. If the
// context is cancelled, an EventError is emitted and the channel is closed.
func (a *Agent) Stream(ctx context.Context, input string) (<-chan AgentEvent, error) {
	msgs, err := a.buildMessages(ctx, input)
	if err != nil {
		return nil, err
	}

	events := make(chan AgentEvent, 64)

	go func() {
		defer close(events)

		wrappedProvider := a.applyMiddleware(a.provider)
		conv := message.NewConversation()
		conv.Add(msgs...)

		var totalUsage provider.TokenUsage

		for turn := 0; turn < a.maxTurns; turn++ {
			// Check for cancellation
			if err := ctx.Err(); err != nil {
				emitEvent(ctx, events, AgentEvent{
					Type:      EventError,
					Error:     err,
					Turn:      turn,
					Timestamp: time.Now(),
				})
				return
			}

			// Build request
			req := provider.GenerateRequest{
				Model:    a.model,
				Messages: conv.Messages,
				Options:  provider.NewGenerateOptions(a.genOptions...),
			}
			if a.tools != nil && a.tools.Size() > 0 {
				req.Tools = a.tools.Definitions()
			}

			// Start streaming
			streamCh, err := wrappedProvider.Stream(ctx, req)
			if err != nil {
				emitEvent(ctx, events, AgentEvent{
					Type:      EventError,
					Error:     fmt.Errorf("stream start: %w", err),
					Turn:      turn,
					Timestamp: time.Now(),
				})
				return
			}

			emitEvent(ctx, events, AgentEvent{
				Type:      EventGenerateStart,
				Turn:      turn,
				Timestamp: time.Now(),
			})

			// Process stream events
			var textParts []string
			var collectedToolCalls []message.ToolCall

			for evt := range streamCh {
				switch evt.Type {
				case provider.StreamEventChunk:
					textParts = append(textParts, evt.Chunk)
					emitEvent(ctx, events, AgentEvent{
						Type:      EventGenerateChunk,
						Chunk:     evt.Chunk,
						Turn:      turn,
						Timestamp: time.Now(),
					})

				case provider.StreamEventToolCall:
					if evt.ToolCall != nil {
						collectedToolCalls = append(collectedToolCalls, *evt.ToolCall)
					}

				case provider.StreamEventDone:
					if evt.Usage != nil {
						totalUsage = totalUsage.Add(*evt.Usage)
					}

				case provider.StreamEventError:
					emitEvent(ctx, events, AgentEvent{
						Type:      EventError,
						Error:     evt.Error,
						Turn:      turn,
						Timestamp: time.Now(),
					})
					return
				}
			}

			// Build assistant message from accumulated stream data
			assistantMsg := message.AssistantMessage(strings.Join(textParts, ""))
			if len(collectedToolCalls) > 0 {
				assistantMsg.ToolCalls = collectedToolCalls
			}
			conv.Add(assistantMsg)

			emitEvent(ctx, events, AgentEvent{
				Type:      EventGenerateDone,
				Usage:     &totalUsage,
				Turn:      turn,
				Timestamp: time.Now(),
			})

			// Handle tool calls
			if len(collectedToolCalls) > 0 {
				for _, tc := range collectedToolCalls {
					emitEvent(ctx, events, AgentEvent{
						Type:      EventToolCallStart,
						ToolCall:  &tc,
						Turn:      turn,
						Timestamp: time.Now(),
					})

					toolStart := time.Now()
					toolMsg := executeToolCall(ctx, a.tools, tc)
					toolDuration := time.Since(toolStart)

					var toolErr error
					if toolMsg.ToolResult != nil && toolMsg.ToolResult.IsError {
						toolErr = fmt.Errorf("tool error: %s", toolMsg.ToolResult.Content)
					}

					toolExec := ToolExecution{
						Turn:     turn,
						Call:     tc,
						Duration: toolDuration,
						Error:    toolErr,
					}
					if toolMsg.ToolResult != nil {
						toolExec.Result = *toolMsg.ToolResult
					}

					emitEvent(ctx, events, AgentEvent{
						Type:      EventToolCallEnd,
						Result:    &toolExec,
						Turn:      turn,
						Timestamp: time.Now(),
					})

					conv.Add(toolMsg)
				}
				// Continue to next turn for another generation
				continue
			}

			// No tool calls — execution complete
			emitEvent(ctx, events, AgentEvent{
				Type:      EventDone,
				Usage:     &totalUsage,
				Turn:      turn,
				Timestamp: time.Now(),
			})
			return
		}

		// Max turns exceeded
		emitEvent(ctx, events, AgentEvent{
			Type: EventError,
			Error: &MaxTurnsError{
				AgentName: a.name,
				MaxTurns:  a.maxTurns,
			},
			Turn:      a.maxTurns,
			Timestamp: time.Now(),
		})
	}()

	return events, nil
}

// Clone creates an independent copy of the agent with a new name and ID.
// The cloned agent shares the underlying provider, tool registry, and
// template objects (which are immutable or thread-safe), but has its own
// identity. Middleware and generation options are also shared since they
// are read-only after creation.
//
// Use Clone when you need to run multiple agents with the same configuration
// in parallel, for example in a fan-out orchestration pattern.
//
// If name is empty, the clone's name defaults to "<parent>-clone".
func (a *Agent) Clone(name string) *Agent {
	if name == "" {
		name = a.name + "-clone"
	}
	return &Agent{
		id:         generateAgentID(),
		name:       name,
		provider:   a.provider,
		model:      a.model,
		system:     a.system,
		systemData: a.systemData,
		tools:      a.tools,
		memory:     a.memory,
		maxTurns:   a.maxTurns,
		middleware: a.middleware,
		genOptions: a.genOptions,
		logger:     a.logger,
	}
}

// ---------------------------------------------------------------------------
// Accessors
// ---------------------------------------------------------------------------

// ID returns the agent's unique identifier.
func (a *Agent) ID() string { return a.id }

// Name returns the agent's human-readable name.
func (a *Agent) Name() string { return a.name }

// Provider returns the agent's LLM provider.
func (a *Agent) Provider() provider.Provider { return a.provider }

// Model returns the agent's model identifier.
func (a *Agent) Model() string { return a.model }

// MaxTurns returns the configured maximum number of execution turns.
func (a *Agent) MaxTurns() int { return a.maxTurns }

// Logger returns the agent's structured logger.
func (a *Agent) Logger() *slog.Logger { return a.logger }

// HasTools returns true if the agent has any tools registered.
func (a *Agent) HasTools() bool {
	return a.tools != nil && a.tools.Size() > 0
}

// ToolCount returns the number of tools registered on the agent.
func (a *Agent) ToolCount() int {
	if a.tools == nil {
		return 0
	}
	return a.tools.Size()
}

// SystemTemplate returns the agent's system prompt template, or nil if
// none is configured.
func (a *Agent) SystemTemplate() *Template { return a.system }

// ---------------------------------------------------------------------------
// Setters for post-creation configuration
// ---------------------------------------------------------------------------

// SetModel updates the agent's model identifier.
func (a *Agent) SetModel(model string) { a.model = model }

// SetMaxTurns updates the maximum number of execution turns.
// If n is <= 0, the value is not changed.
func (a *Agent) SetMaxTurns(n int) {
	if n > 0 {
		a.maxTurns = n
	}
}

// SetSystemData updates the data passed to the system prompt template.
func (a *Agent) SetSystemData(data any) { a.systemData = data }

// SetLogger updates the agent's structured logger.
func (a *Agent) SetLogger(logger *slog.Logger) {
	if logger != nil {
		a.logger = logger
	}
}

// SetTools replaces the agent's tool registry.
func (a *Agent) SetTools(registry *ToolRegistry) { a.tools = registry }

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// applyMiddleware wraps the provider with the agent's middleware chain.
// If no middleware is configured, the original provider is returned.
func (a *Agent) applyMiddleware(p provider.Provider) provider.Provider {
	if len(a.middleware) == 0 {
		return p
	}
	chained := middleware.Chain(a.middleware...)
	return chained(p)
}

// renderSystemPrompt executes the system prompt template with the configured
// data. Returns empty string if no template is set.
func (a *Agent) renderSystemPrompt() (string, error) {
	if a.system == nil {
		return "", nil
	}
	return a.system.Execute(a.systemData)
}

// buildMessages assembles the full conversation context for a Run or Stream
// call: system prompt + memory context + user input.
func (a *Agent) buildMessages(ctx context.Context, input string) ([]message.Message, error) {
	var msgs []message.Message

	// System prompt
	systemText, err := a.renderSystemPrompt()
	if err != nil {
		return nil, fmt.Errorf("render system prompt: %w", err)
	}
	if systemText != "" {
		msgs = append(msgs, message.SystemMessage(systemText))
	}

	// Memory context
	if a.memory != nil && a.memory.Size() > 0 {
		memMsgs, err := a.memory.GetRelevant(ctx, input, 10)
		if err == nil && len(memMsgs) > 0 {
			msgs = append(msgs, memMsgs...)
		}
	}

	// User input
	msgs = append(msgs, message.UserMessage(input))

	return msgs, nil
}

// emitEvent sends an event to the channel, respecting context cancellation.
// If the context is cancelled while waiting to send, the event is dropped.
func emitEvent(ctx context.Context, ch chan<- AgentEvent, evt AgentEvent) {
	select {
	case ch <- evt:
	case <-ctx.Done():
		// Context cancelled; drop the event
	}
}

// generateAgentID creates a unique identifier for a new agent instance.
func generateAgentID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("agent-%x-%d", b[:4], time.Now().UnixMilli()%10000)
}
