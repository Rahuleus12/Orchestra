package agent

import (
	"time"

	"github.com/user/orchestra/internal/message"
	"github.com/user/orchestra/internal/provider"
)

// AgentEventType identifies the kind of event emitted during agent execution.
type AgentEventType string

const (
	// EventThinking is emitted when the agent begins a reasoning/generation step.
	EventThinking AgentEventType = "thinking"

	// EventGenerateStart is emitted just before a provider Generate call.
	EventGenerateStart AgentEventType = "generate_start"

	// EventGenerateChunk is emitted for each streaming text fragment.
	EventGenerateChunk AgentEventType = "generate_chunk"

	// EventGenerateDone is emitted after a provider call completes.
	EventGenerateDone AgentEventType = "generate_done"

	// EventToolCallStart is emitted when the agent begins executing a tool.
	EventToolCallStart AgentEventType = "tool_call_start"

	// EventToolCallEnd is emitted when a tool execution completes.
	EventToolCallEnd AgentEventType = "tool_call_end"

	// EventDone is emitted when the agent finishes its entire run.
	EventDone AgentEventType = "done"

	// EventError is emitted when an error occurs during agent execution.
	EventError AgentEventType = "error"
)

// AgentEvent represents a single event emitted during agent execution.
// Events are produced on a channel for real-time observability and streaming.
type AgentEvent struct {
	// Type identifies the kind of event.
	Type AgentEventType `json:"type" yaml:"type"`

	// Chunk contains streamed text content for EventGenerateChunk events.
	Chunk string `json:"chunk,omitempty" yaml:"chunk,omitempty"`

	// ToolCall contains the tool call data for EventToolCallStart events.
	ToolCall *message.ToolCall `json:"tool_call,omitempty" yaml:"tool_call,omitempty"`

	// Result contains the tool execution result for EventToolCallEnd events.
	Result *ToolExecution `json:"result,omitempty" yaml:"result,omitempty"`

	// Usage contains token usage information, populated on EventGenerateDone
	// and EventDone events.
	Usage *provider.TokenUsage `json:"usage,omitempty" yaml:"usage,omitempty"`

	// Error contains the error for EventError events.
	Error error `json:"-" yaml:"-"`

	// Turn indicates which execution turn this event belongs to (0-indexed).
	Turn int `json:"turn" yaml:"turn"`

	// Timestamp is when the event was produced.
	Timestamp time.Time `json:"timestamp" yaml:"timestamp"`
}

// ToolExecution records the details of a single tool invocation during
// the agent execution loop.
type ToolExecution struct {
	// Turn is the execution turn in which this tool was called (0-indexed).
	Turn int `json:"turn" yaml:"turn"`

	// Call is the original tool call requested by the model.
	Call message.ToolCall `json:"call" yaml:"call"`

	// Result is the output of the tool execution.
	Result message.ToolResult `json:"result" yaml:"result"`

	// Duration is how long the tool took to execute.
	Duration time.Duration `json:"duration" yaml:"duration"`

	// Error is set if the tool execution failed. When Error is non-nil,
	// Result will contain an error message in its Content field and
	// IsError will be true.
	Error error `json:"-" yaml:"-"`
}

// AgentResult holds the complete result of an agent execution, including
// the final output, the full conversation trace, tool call details,
// aggregate token usage, and timing information.
type AgentResult struct {
	// Output is the final message produced by the agent.
	Output message.Message `json:"output" yaml:"output"`

	// Conversation is the complete conversation history from this execution,
	// including the system prompt, user input, assistant responses, and
	// tool result messages.
	Conversation *message.Conversation `json:"conversation" yaml:"conversation"`

	// ToolCalls is the ordered list of all tool executions that occurred
	// during the agent run.
	ToolCalls []ToolExecution `json:"tool_calls" yaml:"tool_calls"`

	// Usage is the aggregate token usage across all provider calls
	// made during this execution.
	Usage provider.TokenUsage `json:"usage" yaml:"usage"`

	// Duration is the total wall-clock time of the agent execution.
	Duration time.Duration `json:"duration" yaml:"duration"`

	// Turns is the number of provider calls made during execution.
	// A single-turn conversation has Turns=1; each tool-call round
	// adds another turn.
	Turns int `json:"turns" yaml:"turns"`

	// Metadata contains arbitrary data associated with this result.
	Metadata map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// HasToolCalls returns true if the result includes any tool executions.
func (r *AgentResult) HasToolCalls() bool {
	return len(r.ToolCalls) > 0
}

// ToolCallsByTurn returns all tool executions that occurred in the given turn.
func (r *AgentResult) ToolCallsByTurn(turn int) []ToolExecution {
	var result []ToolExecution
	for _, tc := range r.ToolCalls {
		if tc.Turn == turn {
			result = append(result, tc)
		}
	}
	return result
}

// FinalText returns the text content of the final output message.
func (r *AgentResult) FinalText() string {
	return r.Output.Text()
}
