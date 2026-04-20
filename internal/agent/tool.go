package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/user/orchestra/internal/message"
	"github.com/user/orchestra/internal/provider"
)

// Tool is the interface that agents use to execute tools. This is a minimal
// interface for Phase 3 — the full tool system with validation, schemas,
// and built-in tools will be built in Phase 6.
//
// Implementations must be safe for concurrent use.
type Tool interface {
	// Name returns the unique identifier of this tool.
	Name() string

	// Description returns a human-readable description of what the tool does.
	// This is sent to the LLM so it can decide when to call the tool.
	Description() string

	// Parameters returns the JSON Schema describing the tool's input parameters.
	// Returns nil if the tool takes no parameters.
	Parameters() map[string]any

	// Execute runs the tool with the given JSON-encoded arguments and returns
	// the result as a string. The context can be used for cancellation and timeouts.
	Execute(ctx context.Context, arguments string) (string, error)
}

// ToolDefinition converts a Tool into a provider.ToolDefinition that can be
// sent to an LLM provider as part of a generation request.
func ToolDefinition(t Tool) provider.ToolDefinition {
	return provider.FunctionTool(t.Name(), t.Description(), t.Parameters())
}

// ToolFunc is an adapter that allows using a plain function as a Tool.
// This is useful for quickly creating tools without defining a full struct.
type ToolFunc struct {
	name        string
	description string
	parameters  map[string]any
	fn          func(ctx context.Context, arguments string) (string, error)
}

// NewToolFunc creates a new tool from a function.
func NewToolFunc(name, description string, fn func(ctx context.Context, arguments string) (string, error)) *ToolFunc {
	return &ToolFunc{
		name:        name,
		description: description,
		fn:          fn,
	}
}

// NewToolFuncWithSchema creates a new tool from a function with explicit parameter schema.
func NewToolFuncWithSchema(name, description string, parameters map[string]any, fn func(ctx context.Context, arguments string) (string, error)) *ToolFunc {
	return &ToolFunc{
		name:        name,
		description: description,
		parameters:  parameters,
		fn:          fn,
	}
}

// Name returns the tool's name.
func (t *ToolFunc) Name() string { return t.name }

// Description returns the tool's description.
func (t *ToolFunc) Description() string { return t.description }

// Parameters returns the tool's parameter schema.
func (t *ToolFunc) Parameters() map[string]any { return t.parameters }

// Execute calls the underlying function.
func (t *ToolFunc) Execute(ctx context.Context, arguments string) (string, error) {
	return t.fn(ctx, arguments)
}

// ToolRegistry manages a collection of tools that an agent can invoke.
// It provides lookup by name and converts tools to provider definitions.
//
// A ToolRegistry is safe for concurrent use.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry creates a new empty tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry. Returns an error if a tool with
// the same name is already registered.
func (r *ToolRegistry) Register(t Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := t.Name()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %q already registered", name)
	}
	r.tools[name] = t
	return nil
}

// MustRegister adds a tool to the registry, panicking on error.
func (r *ToolRegistry) MustRegister(t Tool) {
	if err := r.Register(t); err != nil {
		panic(err)
	}
}

// Get retrieves a tool by name.
func (r *ToolRegistry) Get(name string) (Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool %q not found", name)
	}
	return t, nil
}

// List returns all registered tools.
func (r *ToolRegistry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	return tools
}

// Definitions returns all tools as provider.ToolDefinitions suitable for
// sending to an LLM provider.
func (r *ToolRegistry) Definitions() []provider.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]provider.ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, ToolDefinition(t))
	}
	return defs
}

// Names returns the names of all registered tools.
func (r *ToolRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// Has reports whether a tool with the given name is registered.
func (r *ToolRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, ok := r.tools[name]
	return ok
}

// Size returns the number of registered tools.
func (r *ToolRegistry) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.tools)
}

// Clear removes all registered tools.
func (r *ToolRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.tools = make(map[string]Tool)
}

// ExecuteTool looks up a tool by name and executes it with the given arguments.
// This is a convenience method that combines Get and Execute.
func (r *ToolRegistry) ExecuteTool(ctx context.Context, toolName, arguments string) (string, error) {
	t, err := r.Get(toolName)
	if err != nil {
		return "", err
	}
	return t.Execute(ctx, arguments)
}

// executeToolCall executes a tool call from the model and returns a ToolResult message.
// If the tool is not found or execution fails, an error result is returned.
func executeToolCall(ctx context.Context, registry *ToolRegistry, call message.ToolCall) message.Message {
	t, err := registry.Get(call.Function.Name)
	if err != nil {
		// Tool not found — return an error result
		errMsg := fmt.Sprintf("tool %q not found", call.Function.Name)
		return message.ToolResultMessage(call.ID, errMsg, true)
	}

	result, err := t.Execute(ctx, call.Function.Arguments)
	if err != nil {
		// Tool execution failed — return an error result
		errMsg := fmt.Sprintf("tool %q execution failed: %v", call.Function.Name, err)
		return message.ToolResultMessage(call.ID, errMsg, true)
	}

	return message.ToolResultMessage(call.ID, result, false)
}

// ParseArguments is a helper that unmarshals JSON tool call arguments into
// a Go value. This is a convenience for tool implementations.
func ParseArguments[T any](arguments string) (T, error) {
	var result T
	if err := json.Unmarshal([]byte(arguments), &result); err != nil {
		return result, fmt.Errorf("parse arguments: %w", err)
	}
	return result, nil
}

// Compile-time interface checks.
var (
	_ Tool = (*ToolFunc)(nil)
)
