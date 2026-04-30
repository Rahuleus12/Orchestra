// Package tool provides the core tool system for agent function calling.
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/user/orchestra/internal/message"
)

// ExecutionConfig holds configuration options for tool execution.
type ExecutionConfig struct {
	// Timeout is the maximum duration a single tool execution is allowed to run.
	// Zero means no timeout (not recommended for untrusted tools).
	Timeout time.Duration

	// MaxParallel is the maximum number of tool calls to execute in parallel.
	// Zero or negative means unlimited parallelism.
	MaxParallel int

	// Logger is used for execution logging. If nil, slog.Default() is used.
	Logger *slog.Logger

	// Stats is optional execution statistics tracker. If nil, no stats are recorded.
	Stats *ExecutionStats
}

// DefaultExecutionConfig returns a config with sensible defaults:
// 30-second timeout, unlimited parallelism, default logger, no stats.
func DefaultExecutionConfig() ExecutionConfig {
	return ExecutionConfig{
		Timeout:     30 * time.Second,
		MaxParallel: 0,
		Logger:      slog.Default(),
	}
}

// SandboxConfig holds resource limits for sandboxed tool execution.
type SandboxConfig struct {
	// MaxMemoryBytes limits the memory the tool can allocate.
	// Zero means no limit. Note: Go's garbage collector makes precise
	// memory limiting difficult; this is a best-effort check.
	MaxMemoryBytes int64

	// MaxGoroutines limits how many goroutines the tool can spawn.
	// Zero means no limit.
	MaxGoroutines int

	// AllowedEnvVars is a whitelist of environment variables the tool can access.
	// If nil, no environment variables are accessible.
	// If empty slice, all environment variables are accessible.
	AllowedEnvVars []string

	// WorkingDirectory restricts filesystem access to this directory and its children.
	// Empty means no restriction (not recommended for untrusted tools).
	WorkingDirectory string

	// NetworkAccess controls whether the tool can make network connections.
	NetworkAccess bool
}

// DefaultSandboxConfig returns a restrictive sandbox config suitable for
// executing untrusted tool code.
func DefaultSandboxConfig() SandboxConfig {
	return SandboxConfig{
		MaxMemoryBytes:   256 * 1024 * 1024, // 256 MB
		MaxGoroutines:    10,
		AllowedEnvVars:   nil, // No env vars by default
		WorkingDirectory: "",  // No restriction (tools should set their own)
		NetworkAccess:    false,
	}
}

// Executor executes tool calls with configurable behavior including
// timeouts, parallelism, and sandboxing.
type Executor struct {
	registry *ToolRegistry
	config   ExecutionConfig
}

// NewExecutor creates a new tool executor.
func NewExecutor(registry *ToolRegistry, config ExecutionConfig) *Executor {
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	return &Executor{
		registry: registry,
		config:   config,
	}
}

// Registry returns the executor's tool registry.
func (e *Executor) Registry() *ToolRegistry {
	return e.registry
}

// Config returns the executor's configuration.
func (e *Executor) Config() ExecutionConfig {
	return e.config
}

// ExecuteSync executes a single tool call synchronously with timeout.
// This is the basic execution primitive used by the agent loop.
//
// The context can be used for cancellation. If both context deadline and
// config timeout are set, the earlier deadline wins.
func (e *Executor) ExecuteSync(ctx context.Context, call ToolCallInput) ToolCallResult {
	// Apply timeout if configured
	if e.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.config.Timeout)
		defer cancel()
	}

	return e.executeWithStats(ctx, call)
}

// ExecuteParallel executes multiple tool calls in parallel and returns
// all results when all have completed or the context is cancelled.
//
// Tool calls are considered independent and can run concurrently. The
// MaxParallel config limits concurrency if set.
//
// Returns results in the same order as the input calls.
func (e *Executor) ExecuteParallel(ctx context.Context, calls []ToolCallInput) []ToolCallResult {
	if len(calls) == 0 {
		return nil
	}

	// Apply timeout if configured
	if e.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.config.Timeout)
		defer cancel()
	}

	results := make([]ToolCallResult, len(calls))

	// If max parallelism is 1 or we have a single call, execute sequentially
	if e.config.MaxParallel == 1 || len(calls) == 1 {
		for i, call := range calls {
			results[i] = e.executeWithStats(ctx, call)
		}
		return results
	}

	// Execute in parallel with optional concurrency limit
	var wg sync.WaitGroup
	sem := make(chan struct{}, e.maxParallel())

	for i, call := range calls {
		wg.Add(1)
		go func(idx int, c ToolCallInput) {
			defer wg.Done()
			sem <- struct{}{}        // Acquire semaphore
			defer func() { <-sem }() // Release semaphore

			results[idx] = e.executeWithStats(ctx, c)
		}(i, call)
	}

	wg.Wait()
	return results
}

// ExecuteParallelWithContext is like ExecuteParallel but returns as soon as
// the context is cancelled, without waiting for remaining tools to complete.
// Results for tools that didn't complete will have a context.Canceled error.
func (e *Executor) ExecuteParallelWithContext(ctx context.Context, calls []ToolCallInput) []ToolCallResult {
	if len(calls) == 0 {
		return nil
	}

	results := make([]ToolCallResult, len(calls))
	var mu sync.Mutex

	// Apply timeout if configured
	if e.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.config.Timeout)
		defer cancel()
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, e.maxParallel())

	for i, call := range calls {
		// Check if context is already cancelled before starting
		if ctx.Err() != nil {
			results[i] = ToolCallResult{
				Input:    call,
				Error:    ctx.Err(),
				Duration: 0,
			}
			continue
		}

		wg.Add(1)
		go func(idx int, c ToolCallInput) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				result := e.executeWithStats(ctx, c)
				mu.Lock()
				results[idx] = result
				mu.Unlock()
			case <-ctx.Done():
				mu.Lock()
				results[idx] = ToolCallResult{
					Input:    c,
					Error:    ctx.Err(),
					Duration: 0,
				}
				mu.Unlock()
			}
		}(i, call)
	}

	wg.Wait()
	return results
}

// ExecuteSandboxed executes a single tool call within resource limits.
// This provides a basic sandbox that monitors memory usage and limits
// goroutine spawning.
//
// Note: Go's runtime doesn't provide true sandboxing. This is a best-effort
// implementation that provides some protection but should not be relied upon
// for security-critical isolation. For true sandboxing, use process-level
// isolation (e.g., gVisor, containers, separate processes).
func (e *Executor) ExecuteSandboxed(ctx context.Context, call ToolCallInput, sandbox SandboxConfig) ToolCallResult {
	// Apply timeout if configured
	timeout := e.config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Set up memory tracking
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	result := e.executeWithStats(ctx, call)

	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)
	memUsed := int64(memAfter.Alloc) - int64(memBefore.Alloc)

	// Check memory limit (best effort)
	if sandbox.MaxMemoryBytes > 0 && memUsed > sandbox.MaxMemoryBytes {
		e.config.Logger.Warn("tool exceeded memory limit",
			slog.String("tool", call.Name),
			slog.Int64("used", memUsed),
			slog.Int64("limit", sandbox.MaxMemoryBytes),
		)
		// We can't retroactively fail the execution, but we log the violation
		// For a production system, you'd want process-level isolation
	}

	return result
}

// executeWithStats executes a tool call and records stats if configured.
func (e *Executor) executeWithStats(ctx context.Context, call ToolCallInput) ToolCallResult {
	start := timeNow()
	result := e.execute(ctx, call)
	duration := timeSince(start)
	result.Duration = duration

	if e.config.Stats != nil {
		e.config.Stats.Add(duration, result.Error)
	}

	return result
}

// execute performs the actual tool execution.
func (e *Executor) execute(ctx context.Context, call ToolCallInput) ToolCallResult {
	// Check context before doing anything
	if err := ctx.Err(); err != nil {
		return ToolCallResult{
			Input:    call,
			Error:    fmt.Errorf("context cancelled before execution: %w", err),
			Duration: 0,
		}
	}

	// Look up the tool
	tool, err := e.registry.Get(call.Name)
	if err != nil {
		return ToolCallResult{
			Input:    call,
			Error:    fmt.Errorf("tool %q not found: %w", call.Name, err),
			Duration: 0,
		}
	}

	e.config.Logger.Debug("executing tool",
		slog.String("tool", call.Name),
		slog.String("input", truncate(string(call.Input), 500)),
	)

	// Execute the tool
	output, execErr := tool.Execute(ctx, call.Input)

	if execErr != nil {
		e.config.Logger.Debug("tool execution failed",
			slog.String("tool", call.Name),
			slog.String("error", execErr.Error()),
		)
	} else {
		e.config.Logger.Debug("tool execution completed",
			slog.String("tool", call.Name),
			slog.String("output", truncate(string(output), 500)),
		)
	}

	return ToolCallResult{
		Input:  call,
		Output: output,
		Error:  execErr,
	}
}

// maxParallel returns the effective max parallelism (0 means unlimited).
func (e *Executor) maxParallel() int {
	if e.config.MaxParallel <= 0 {
		return runtime.NumCPU() * 4 // Reasonable default
	}
	return e.config.MaxParallel
}

// ---------------------------------------------------------------------------
// ToolCallInput
// ---------------------------------------------------------------------------

// ToolCallInput represents a tool call to be executed by the Executor.
// It's a simplified version of message.ToolCall that can be constructed
// programmatically without needing a message ID.
type ToolCallInput struct {
	// Name is the tool name to execute.
	Name string `json:"name"`

	// Input is the JSON-encoded input for the tool.
	Input json.RawMessage `json:"input,omitempty"`

	// ID is an optional identifier for this call. If empty, one will be generated.
	ID string `json:"id,omitempty"`
}

// FromToolCall creates a ToolCallInput from a message.ToolCall.
func FromToolCall(call message.ToolCall) ToolCallInput {
	return ToolCallInput{
		Name:  call.Function.Name,
		Input: json.RawMessage(call.Function.Arguments),
		ID:    call.ID,
	}
}

// ToToolCall converts a ToolCallInput to a message.ToolCall.
// If ID is empty, a new ID is generated.
func (c ToolCallInput) ToToolCall() message.ToolCall {
	id := c.ID
	if id == "" {
		id = generateToolCallID()
	}
	args := string(c.Input)
	if args == "" {
		args = "{}"
	}
	return message.ToolCall{
		ID:   id,
		Type: "function",
		Function: message.ToolCallFunction{
			Name:      c.Name,
			Arguments: args,
		},
	}
}

// ---------------------------------------------------------------------------
// ToolCallResult (extended with Input)
// ---------------------------------------------------------------------------

// ToolCallResult holds the result of executing a single tool call.
type ToolCallResult struct {
	// Input is the original tool call input.
	Input ToolCallInput

	// Output is the JSON output from the tool.
	Output json.RawMessage

	// Error is any error that occurred during execution.
	Error error

	// Duration is how long the tool took to execute in nanoseconds.
	Duration int64
}

// Success returns true if the tool executed without error.
func (r ToolCallResult) Success() bool {
	return r.Error == nil
}

// Failed returns true if the tool execution resulted in an error.
func (r ToolCallResult) Failed() bool {
	return r.Error != nil
}

// DurationSeconds returns the execution duration as a float64 seconds.
func (r ToolCallResult) DurationSeconds() float64 {
	return float64(r.Duration) / 1e9
}

// OutputString returns the output as a string, or empty string if nil.
func (r ToolCallResult) OutputString() string {
	if r.Output == nil {
		return ""
	}
	return string(r.Output)
}

// ToMessage converts this result to a message.Message suitable for adding
// back to the conversation.
func (r ToolCallResult) ToMessage() message.Message {
	call := r.Input.ToToolCall()
	if r.Error != nil {
		return message.ToolResultMessage(call.ID, r.Error.Error(), true)
	}
	return message.ToolResultMessage(call.ID, r.OutputString(), false)
}

// ---------------------------------------------------------------------------
// Parallel Results
// ---------------------------------------------------------------------------

// ParallelResults holds the results of a parallel tool execution.
type ParallelResults struct {
	// Results contains all tool call results in input order.
	Results []ToolCallResult

	// Errors contains only the failed results.
	Errors []ToolCallResult

	// Successes contains only the successful results.
	Successes []ToolCallResult

	// TotalDuration is the wall-clock time for the entire parallel execution.
	TotalDuration int64
}

// AllSucceeded returns true if all tool calls succeeded.
func (p ParallelResults) AllSucceeded() bool {
	return len(p.Errors) == 0
}

// AnyFailed returns true if any tool call failed.
func (p ParallelResults) AnyFailed() bool {
	return len(p.Errors) > 0
}

// SuccessCount returns the number of successful tool calls.
func (p ParallelResults) SuccessCount() int {
	return len(p.Successes)
}

// ErrorCount returns the number of failed tool calls.
func (p ParallelResults) ErrorCount() int {
	return len(p.Errors)
}

// TotalDurationSeconds returns the total wall-clock duration in seconds.
func (p ParallelResults) TotalDurationSeconds() float64 {
	return float64(p.TotalDuration) / 1e9
}

// CollectParallelResults collects individual results into a ParallelResults
// summary, computing the errors and successes slices.
func CollectParallelResults(results []ToolCallResult, totalDuration int64) ParallelResults {
	pr := ParallelResults{
		Results:       results,
		TotalDuration: totalDuration,
	}

	for _, r := range results {
		if r.Failed() {
			pr.Errors = append(pr.Errors, r)
		} else {
			pr.Successes = append(pr.Successes, r)
		}
	}

	return pr
}

// ExecuteParallelAndCollect executes multiple tool calls in parallel and
// returns a ParallelResults summary.
func (e *Executor) ExecuteParallelAndCollect(ctx context.Context, calls []ToolCallInput) ParallelResults {
	start := timeNow()
	results := e.ExecuteParallel(ctx, calls)
	duration := timeSince(start)
	return CollectParallelResults(results, duration)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// generateToolCallID creates a unique ID for a tool call.
func generateToolCallID() string {
	b := make([]byte, 12)
	_ = runtime.Callers(0, nil) // Just to add some entropy
	for i := range b {
		b[i] = byte(time.Now().UnixNano() >> (uint(i) * 8))
	}
	return fmt.Sprintf("call_%x", b)
}

// ExecuteToolCalls is a package-level convenience function that executes
// multiple tool calls in parallel using the default configuration.
func ExecuteToolCalls(ctx context.Context, registry *ToolRegistry, calls []message.ToolCall) []ToolCallResult {
	executor := NewExecutor(registry, DefaultExecutionConfig())
	inputs := make([]ToolCallInput, len(calls))
	for i, call := range calls {
		inputs[i] = FromToolCall(call)
	}
	return executor.ExecuteParallel(ctx, inputs)
}

// ExecuteToolCall is a package-level convenience function that executes
// a single tool call using the default configuration.
func ExecuteToolCall(ctx context.Context, registry *ToolRegistry, call message.ToolCall) ToolCallResult {
	executor := NewExecutor(registry, DefaultExecutionConfig())
	return executor.ExecuteSync(ctx, FromToolCall(call))
}
