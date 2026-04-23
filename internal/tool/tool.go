// Package tool provides the core tool system for agent function calling.
// It defines the Tool interface, a registry with namespacing, tool definitions,
// and adapter types for bridging to the agent package.
//
// Tools enable agents to interact with the outside world — making HTTP requests,
// reading/writing files, executing shell commands, searching codebases, and more.
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/user/orchestra/internal/provider"
)

// Tool is the interface that all tools must implement.
//
// A Tool represents a capability that an agent can invoke during its execution
// loop. When the LLM decides to call a tool, the agent's execution engine
// looks up the tool by name, unmarshals the arguments JSON, and calls Execute.
//
// Implementations must be safe for concurrent use.
type Tool interface {
	// Name returns the unique identifier for this tool.
	// Names should be lowercase_with_underscores and unique within their namespace.
	Name() string

	// Description returns a human-readable description of what this tool does.
	// This is sent to the LLM to help it decide when to use the tool.
	// Good descriptions explain what the tool does, when to use it, and any
	// important constraints or gotchas.
	Description() string

	// Parameters returns the JSON Schema for this tool's input parameters.
	// The schema should describe the expected shape of the JSON object passed
	// to Execute. Returns nil if the tool takes no parameters.
	//
	// The returned value is json.RawMessage to allow pre-compiled schemas
	// and to avoid map[string]any overhead.
	Parameters() json.RawMessage

	// Execute runs the tool with the given JSON-encoded input and returns
	// the result as JSON-encoded output.
	//
	// The context can be used for cancellation and timeouts. Implementations
	// should respect context cancellation and return quickly when cancelled.
	//
	// The input is the raw JSON string from the LLM's tool call arguments.
	// Implementations should validate and unmarshal this into their input type.
	//
	// The output should be JSON-encoded. For simple string results, use
	// json.Marshal("result string"). For complex results, marshal a struct.
	Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error)
}

// ToolDefinition is the wire format for describing a tool to an LLM provider.
// It follows the OpenAI function calling format which is widely adopted.
type ToolDefinition struct {
	// Type is the tool type, always "function" for standard tools.
	Type string `json:"type" yaml:"type"`

	// Function describes the function that can be called.
	Function FunctionDef `json:"function" yaml:"function"`
}

// FunctionDef describes a function that can be called by the model.
type FunctionDef struct {
	// Name is the function identifier.
	Name string `json:"name" yaml:"name"`

	// Description explains what the function does.
	Description string `json:"description" yaml:"description"`

	// Parameters is a JSON Schema object describing the function's input.
	Parameters json.RawMessage `json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

// ToProviderDefinition converts this ToolDefinition to the provider package's
// ToolDefinition format. This bridges the tool system to the provider layer.
func (td ToolDefinition) ToProviderDefinition() provider.ToolDefinition {
	var params map[string]any
	if len(td.Function.Parameters) > 0 {
		_ = json.Unmarshal(td.Function.Parameters, &params)
	}
	return provider.ToolDefinition{
		Type: td.Type,
		Function: provider.FunctionDef{
			Name:        td.Function.Name,
			Description: td.Function.Description,
			Parameters:  params,
		},
	}
}

// Definition creates a ToolDefinition from a Tool, suitable for sending
// to an LLM provider.
func Definition(t Tool) ToolDefinition {
	params := t.Parameters()
	if params == nil {
		params = json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return ToolDefinition{
		Type: "function",
		Function: FunctionDef{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  params,
		},
	}
}

// ToProviderDefinition is a convenience function that converts a Tool directly
// to a provider.ToolDefinition.
func ToProviderDefinition(t Tool) provider.ToolDefinition {
	return Definition(t).ToProviderDefinition()
}

// ---------------------------------------------------------------------------
// Tool Registry
// ---------------------------------------------------------------------------

// NamespaceSeparator is used to separate namespace and tool name in fully
// qualified tool names. For example, "fs:read_file" where "fs" is the namespace.
const NamespaceSeparator = ":"

// ToolRegistry manages a collection of tools that agents can invoke.
// It supports namespacing to avoid name collisions when tools from different
// sources are combined.
//
// A ToolRegistry is safe for concurrent use.
type ToolRegistry struct {
	mu       sync.RWMutex
	tools    map[string]Tool          // fully qualified name -> tool
	names    map[string]string        // fully qualified name -> namespace (empty for default)
	original map[string]string        // fully qualified name -> original name before namespacing
	logger   *slog.Logger
}

// RegistryOption is a functional option for configuring a ToolRegistry.
type RegistryOption func(*ToolRegistry)

// WithRegistryLogger sets the logger for the registry.
func WithRegistryLogger(logger *slog.Logger) RegistryOption {
	return func(r *ToolRegistry) {
		r.logger = logger
	}
}

// NewRegistry creates a new empty tool registry.
func NewRegistry(opts ...RegistryOption) *ToolRegistry {
	r := &ToolRegistry{
		tools:    make(map[string]Tool),
		names:    make(map[string]string),
		original: make(map[string]string),
		logger:   slog.Default(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Register adds a tool to the registry in the default namespace.
// Returns an error if a tool with the same name is already registered.
func (r *ToolRegistry) Register(t Tool) error {
	return r.RegisterInNamespace("", t)
}

// RegisterInNamespace adds a tool to the registry under the given namespace.
// Namespaces help avoid name collisions. The tool can be retrieved by either
// its fully qualified name ("namespace:name") or, if the namespace is empty,
// by its simple name.
//
// Returns an error if a tool with the same fully qualified name is already
// registered, or if the name is invalid.
func (r *ToolRegistry) RegisterInNamespace(namespace string, t Tool) error {
	name := t.Name()
	if name == "" {
		return fmt.Errorf("tool name must not be empty")
	}
	if strings.Contains(name, NamespaceSeparator) {
		return fmt.Errorf("tool name %q must not contain namespace separator %q", name, NamespaceSeparator)
	}

	fqn := qualifyName(namespace, name)

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[fqn]; exists {
		return fmt.Errorf("tool %q already registered", fqn)
	}

	r.tools[fqn] = t
	r.names[fqn] = namespace
	r.original[fqn] = name

	r.logger.Debug("tool registered",
		slog.String("name", name),
		slog.String("namespace", namespace),
		slog.String("fqn", fqn),
	)

	return nil
}

// MustRegister adds a tool to the registry, panicking on error.
func (r *ToolRegistry) MustRegister(t Tool) {
	if err := r.Register(t); err != nil {
		panic(err)
	}
}

// MustRegisterInNamespace adds a tool to the registry under a namespace,
// panicking on error.
func (r *ToolRegistry) MustRegisterInNamespace(namespace string, t Tool) {
	if err := r.RegisterInNamespace(namespace, t); err != nil {
		panic(err)
	}
}

// Get retrieves a tool by name. If the name contains a namespace separator,
// it is treated as a fully qualified name. Otherwise, it is looked up in the
// default namespace.
func (r *ToolRegistry) Get(name string) (Tool, error) {
	return r.resolve(name, "")
}

// GetFromNamespace retrieves a tool by name from a specific namespace.
func (r *ToolRegistry) GetFromNamespace(namespace, name string) (Tool, error) {
	return r.resolve(name, namespace)
}

// resolve finds a tool by name, using the hint namespace if the name is not
// fully qualified.
func (r *ToolRegistry) resolve(name, hintNamespace string) (Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// If the name contains a namespace separator, treat it as fully qualified
	if strings.Contains(name, NamespaceSeparator) {
		t, ok := r.tools[name]
		if !ok {
			return nil, fmt.Errorf("tool %q not found", name)
		}
		return t, nil
	}

	// Try the hint namespace first
	if hintNamespace != "" {
		fqn := qualifyName(hintNamespace, name)
		if t, ok := r.tools[fqn]; ok {
			return t, nil
		}
	}

	// Fall back to default namespace
	if t, ok := r.tools[name]; ok {
		return t, nil
	}

	return nil, fmt.Errorf("tool %q not found", name)
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

// ListNamespace returns all tools in a specific namespace.
func (r *ToolRegistry) ListNamespace(namespace string) []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var tools []Tool
	for fqn, t := range r.tools {
		if r.names[fqn] == namespace {
			tools = append(tools, t)
		}
	}
	return tools
}

// Definitions returns all tools as ToolDefinitions suitable for sending
// to an LLM provider.
func (r *ToolRegistry) Definitions() []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, Definition(t))
	}
	return defs
}

// ProviderDefinitions returns all tools as provider.ToolDefinitions.
func (r *ToolRegistry) ProviderDefinitions() []provider.ToolDefinition {
	defs := r.Definitions()
	result := make([]provider.ToolDefinition, len(defs))
	for i, d := range defs {
		result[i] = d.ToProviderDefinition()
	}
	return result
}

// Names returns the names of all registered tools (fully qualified).
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

	if strings.Contains(name, NamespaceSeparator) {
		_, ok := r.tools[name]
		return ok
	}
	// Check default namespace
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
	r.names = make(map[string]string)
	r.original = make(map[string]string)
}

// Execute looks up a tool by name and executes it with the given input.
// This is a convenience method that combines Get and Execute.
func (r *ToolRegistry) Execute(ctx context.Context, toolName string, input json.RawMessage) (json.RawMessage, error) {
	t, err := r.Get(toolName)
	if err != nil {
		return nil, err
	}
	return t.Execute(ctx, input)
}

// Namespaces returns a list of all namespaces that have registered tools.
func (r *ToolRegistry) Namespaces() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]struct{})
	var namespaces []string
	for _, ns := range r.names {
		if ns != "" {
			if _, exists := seen[ns]; !exists {
				seen[ns] = struct{}{}
				namespaces = append(namespaces, ns)
			}
		}
	}
	return namespaces
}

// Merge adds all tools from another registry to this one.
// Tools are registered in their original namespaces.
// Returns an error if any tool name conflicts.
func (r *ToolRegistry) Merge(other *ToolRegistry) error {
	other.mu.RLock()
	defer other.mu.RUnlock()

	for fqn, t := range other.tools {
		ns := other.names[fqn]
		if err := r.RegisterInNamespace(ns, t); err != nil {
			return fmt.Errorf("merge: %w", err)
		}
	}
	return nil
}

// qualifyName creates a fully qualified name from namespace and tool name.
func qualifyName(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + NamespaceSeparator + name
}

// ---------------------------------------------------------------------------
// Tool Adapter (bridges agent.Tool to tool.Tool)
// ---------------------------------------------------------------------------

// AgentToolAdapter wraps an agent.Tool (from the internal/agent package) to
// implement the tool.Tool interface. This allows tools written for the simpler
// agent.Tool interface to be used with the enhanced tool system.
//
// The adapter converts between string-based arguments/results and JSON.
type AgentToolAdapter struct {
	inner agentTool
}

// agentTool is an interface matching the agent.Tool signature without
// creating an import cycle.
type agentTool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Execute(ctx context.Context, arguments string) (string, error)
}

// NewAgentToolAdapter wraps an agent.Tool to implement tool.Tool.
func NewAgentToolAdapter(t agentTool) *AgentToolAdapter {
	return &AgentToolAdapter{inner: t}
}

// Name returns the wrapped tool's name.
func (a *AgentToolAdapter) Name() string { return a.inner.Name() }

// Description returns the wrapped tool's description.
func (a *AgentToolAdapter) Description() string { return a.inner.Description() }

// Parameters converts the wrapped tool's map-based schema to JSON.
func (a *AgentToolAdapter) Parameters() json.RawMessage {
	params := a.inner.Parameters()
	if params == nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	data, err := json.Marshal(params)
	if err != nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return json.RawMessage(data)
}

// Execute converts the JSON input to a string, calls the wrapped tool,
// and converts the string result back to JSON.
func (a *AgentToolAdapter) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	// Convert JSON input to string for the inner tool
	var args string
	if len(input) > 0 {
		// Try to extract a string value if the input is a JSON string
		var str string
		if err := json.Unmarshal(input, &str); err == nil {
			args = str
		} else {
			// Use the raw JSON as the string argument
			args = string(input)
		}
	}

	result, err := a.inner.Execute(ctx, args)
	if err != nil {
		return nil, err
	}

	// Convert string result to JSON
	return json.Marshal(result)
}

// ---------------------------------------------------------------------------
// ToolFunc (adapter for simple functions)
// ---------------------------------------------------------------------------

// ToolFunc is an adapter that allows using a plain function as a Tool.
// This is useful for quickly creating tools without defining a full struct.
type ToolFunc struct {
	name        string
	description string
	parameters  json.RawMessage
	fn          func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)
}

// ToolFuncOption is a functional option for ToolFunc.
type ToolFuncOption func(*ToolFunc)

// WithToolFuncParams sets the parameter schema for a ToolFunc.
func WithToolFuncParams(schema json.RawMessage) ToolFuncOption {
	return func(t *ToolFunc) {
		t.parameters = schema
	}
}

// NewToolFunc creates a new tool from a function.
// The tool takes no parameters by default.
func NewToolFunc(name, description string, fn func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)) *ToolFunc {
	return &ToolFunc{
		name:        name,
		description: description,
		parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		fn:          fn,
	}
}

// NewToolFuncWithSchema creates a new tool from a function with explicit schema.
func NewToolFuncWithSchema(name, description string, schema json.RawMessage, fn func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)) *ToolFunc {
	return &ToolFunc{
		name:        name,
		description: description,
		parameters:  schema,
		fn:          fn,
	}
}

// Name returns the tool's name.
func (t *ToolFunc) Name() string { return t.name }

// Description returns the tool's description.
func (t *ToolFunc) Description() string { return t.description }

// Parameters returns the tool's parameter schema.
func (t *ToolFunc) Parameters() json.RawMessage { return t.parameters }

// Execute calls the underlying function.
func (t *ToolFunc) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	return t.fn(ctx, input)
}

// ---------------------------------------------------------------------------
// Tool Middleware
// ---------------------------------------------------------------------------

// ToolMiddleware wraps a Tool with additional behavior such as logging,
// metrics, validation, or rate limiting.
type ToolMiddleware func(Tool) Tool

// WithToolLogging wraps a tool with logging of inputs, outputs, and errors.
func WithToolLogging(logger *slog.Logger) ToolMiddleware {
	return func(next Tool) Tool {
		return &loggingTool{
			inner:  next,
			logger: logger,
		}
	}
}

type loggingTool struct {
	inner  Tool
	logger *slog.Logger
}

func (t *loggingTool) Name() string        { return t.inner.Name() }
func (t *loggingTool) Description() string { return t.inner.Description() }
func (t *loggingTool) Parameters() json.RawMessage {
	return t.inner.Parameters()
}

func (t *loggingTool) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	start := timeNow()
	t.logger.Debug("tool execution started",
		slog.String("tool", t.inner.Name()),
		slog.String("input", truncate(string(input), 500)),
	)

	output, err := t.inner.Execute(ctx, input)
	duration := time.Duration(timeSince(start))

	if err != nil {
		t.logger.Error("tool execution failed",
			slog.String("tool", t.inner.Name()),
			slog.Duration("duration", duration),
			slog.String("error", err.Error()),
		)
	} else {
		t.logger.Debug("tool execution completed",
			slog.String("tool", t.inner.Name()),
			slog.Duration("duration", duration),
			slog.String("output", truncate(string(output), 500)),
		)
	}

	return output, err
}

// ---------------------------------------------------------------------------
// Execution Counter (for metrics)
// ---------------------------------------------------------------------------

// ExecutionStats tracks tool execution statistics.
type ExecutionStats struct {
	TotalExecutions int64
	TotalErrors     int64
	TotalDuration   int64 // nanoseconds
}

// Add records a tool execution.
func (s *ExecutionStats) Add(duration int64, err error) {
	atomic.AddInt64(&s.TotalExecutions, 1)
	atomic.AddInt64(&s.TotalDuration, duration)
	if err != nil {
		atomic.AddInt64(&s.TotalErrors, 1)
	}
}

// WithToolMetrics wraps a tool with execution statistics tracking.
func WithToolMetrics(stats *ExecutionStats) ToolMiddleware {
	return func(next Tool) Tool {
		return &metricsTool{
			inner: next,
			stats: stats,
		}
	}
}

type metricsTool struct {
	inner Tool
	stats *ExecutionStats
}

func (t *metricsTool) Name() string        { return t.inner.Name() }
func (t *metricsTool) Description() string { return t.inner.Description() }
func (t *metricsTool) Parameters() json.RawMessage {
	return t.inner.Parameters()
}

func (t *metricsTool) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	start := timeNow()
	output, err := t.inner.Execute(ctx, input)
	duration := timeSince(start)
	t.stats.Add(duration, err)
	return output, err
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// timeNow and timeSince are package-level functions to allow testing.
var (
	timeNow   = func() int64 { return time.Now().UnixNano() }
	timeSince = func(start int64) int64 { return time.Now().UnixNano() - start }
)

// ParseInput is a helper that unmarshals JSON tool input into a Go value.
func ParseInput[T any](input json.RawMessage) (T, error) {
	var result T
	if len(input) == 0 || string(input) == "null" {
		return result, nil
	}
	if err := json.Unmarshal(input, &result); err != nil {
		return result, fmt.Errorf("parse input: %w", err)
	}
	return result, nil
}

// MarshalOutput is a helper that marshals a Go value to JSON for tool output.
func MarshalOutput(v any) (json.RawMessage, error) {
	return json.Marshal(v)
}

// StringOutput is a helper that wraps a string as JSON tool output.
func StringOutput(s string) json.RawMessage {
	data, _ := json.Marshal(s)
	return data
}

// ErrorOutput is a helper that creates an error result as JSON.
func ErrorOutput(err error) (json.RawMessage, error) {
	return nil, err
}

// truncate shortens a string to maxLen characters, adding "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// Compile-time interface checks.
var (
	_ Tool = (*ToolFunc)(nil)
	_ Tool = (*AgentToolAdapter)(nil)
	_ Tool = (*loggingTool)(nil)
	_ Tool = (*metricsTool)(nil)
)
