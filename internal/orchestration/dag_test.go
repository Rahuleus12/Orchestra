package orchestration

import (
	"context"
	"testing"
	"time"

	"github.com/user/orchestra/internal/agent"
	"github.com/user/orchestra/internal/message"
	"github.com/user/orchestra/internal/provider"
	"github.com/user/orchestra/internal/provider/mock"
)

// mockAgent creates a simple mock agent for testing
func mockAgent(name string) *agent.Agent {
	// Create a mock provider
	mockProvider := mock.NewProvider(name)

	// Create an agent with the mock provider
	a, _ := agent.New(
		name,
		agent.WithProvider(mockProvider, "mock-model"),
		agent.WithSystemPrompt("You are a test assistant."),
	)
	return a
}

// TestNewWorkflow tests creating a new workflow
func TestNewWorkflow(t *testing.T) {
	workflow := NewWorkflow("test-workflow")

	if workflow == nil {
		t.Fatal("NewWorkflow returned nil")
	}

	if workflow.Name() != "test-workflow" {
		t.Errorf("Expected name 'test-workflow', got '%s'", workflow.Name())
	}

	if workflow.ID() == "" {
		t.Error("Workflow ID should not be empty")
	}

	if len(workflow.GetAllSteps()) != 0 {
		t.Error("New workflow should have no steps")
	}

	if len(workflow.GetEdges()) != 0 {
		t.Error("New workflow should have no edges")
	}
}

// TestWorkflowAddStep tests adding steps to a workflow
func TestWorkflowAddStep(t *testing.T) {
	workflow := NewWorkflow("test-workflow")
	step := NewStep("step-1", mockAgent("agent-1"))

	err := workflow.AddStep(step)
	if err != nil {
		t.Errorf("Failed to add step: %v", err)
	}

	// Try to add a step with duplicate ID
	err = workflow.AddStep(step)
	if err == nil {
		t.Error("Expected error when adding duplicate step ID")
	}

	// Verify step was added
	retrieved, err := workflow.GetStep("step-1")
	if err != nil {
		t.Errorf("Failed to get step: %v", err)
	}

	if retrieved.ID != "step-1" {
		t.Errorf("Expected step ID 'step-1', got '%s'", retrieved.ID)
	}

	// Try to get non-existent step
	_, err = workflow.GetStep("non-existent")
	if err == nil {
		t.Error("Expected error when getting non-existent step")
	}
}

// TestWorkflowAddEdge tests adding edges to a workflow
func TestWorkflowAddEdge(t *testing.T) {
	workflow := NewWorkflow("test-workflow")
	step1 := NewStep("step-1", mockAgent("agent-1"))
	step2 := NewStep("step-2", mockAgent("agent-2"))

	workflow.AddStep(step1)
	workflow.AddStep(step2)

	// Add edge from step-1 to step-2
	err := workflow.AddEdge("step-1", "step-2", nil)
	if err != nil {
		t.Errorf("Failed to add edge: %v", err)
	}

	edges := workflow.GetEdges()
	if len(edges) != 1 {
		t.Errorf("Expected 1 edge, got %d", len(edges))
	}

	if edges[0].From != "step-1" || edges[0].To != "step-2" {
		t.Errorf("Edge incorrect: expected step-1 -> step-2, got %s -> %s", edges[0].From, edges[0].To)
	}

	// Try to add edge with non-existent from step
	err = workflow.AddEdge("non-existent", "step-1", nil)
	if err == nil {
		t.Error("Expected error when adding edge with non-existent from step")
	}

	// Try to add edge with non-existent to step
	err = workflow.AddEdge("step-1", "non-existent", nil)
	if err == nil {
		t.Error("Expected error when adding edge with non-existent to step")
	}
}

// TestWorkflowValidate tests workflow validation
func TestWorkflowValidate(t *testing.T) {
	t.Run("Empty workflow", func(t *testing.T) {
		workflow := NewWorkflow("empty")
		err := workflow.Validate()
		if err == nil {
			t.Error("Expected error for empty workflow")
		}
	})

	t.Run("Valid workflow", func(t *testing.T) {
		workflow := NewWorkflow("valid")
		step1 := NewStep("step-1", mockAgent("agent-1"))
		step2 := NewStep("step-2", mockAgent("agent-2"))
		step3 := NewStep("step-3", mockAgent("agent-3"))

		workflow.AddStep(step1)
		workflow.AddStep(step2)
		workflow.AddStep(step3)

		err := workflow.Validate()
		if err != nil {
			t.Errorf("Valid workflow should pass validation: %v", err)
		}
	})

	t.Run("Workflow with cycle", func(t *testing.T) {
		workflow := NewWorkflow("cycle")
		step1 := NewStep("step-1", mockAgent("agent-1"))
		step2 := NewStep("step-2", mockAgent("agent-2"))
		step3 := NewStep("step-3", mockAgent("agent-3"))

		workflow.AddStep(step1)
		workflow.AddStep(step2)
		workflow.AddStep(step3)

		// Add edges that create a cycle: step-1 -> step-2 -> step-3 -> step-1
		workflow.AddEdge("step-1", "step-2", nil)
		workflow.AddEdge("step-2", "step-3", nil)
		workflow.AddEdge("step-3", "step-1", nil)

		err := workflow.Validate()
		if err == nil {
			t.Error("Expected error for workflow with cycle")
		}
	})

	t.Run("Self-loop", func(t *testing.T) {
		workflow := NewWorkflow("self-loop")
		step := NewStep("step-1", mockAgent("agent-1"))

		workflow.AddStep(step)
		workflow.AddEdge("step-1", "step-1", nil)

		err := workflow.Validate()
		if err == nil {
			t.Error("Expected error for workflow with self-loop")
		}
	})
}

// TestGetTopologicalOrder tests topological ordering of workflow steps
func TestGetTopologicalOrder(t *testing.T) {
	t.Run("Single step", func(t *testing.T) {
		workflow := NewWorkflow("single")
		step := NewStep("step-1", mockAgent("agent-1"))
		workflow.AddStep(step)

		order, err := workflow.GetTopologicalOrder()
		if err != nil {
			t.Errorf("Failed to get topological order: %v", err)
		}

		if len(order) != 1 {
			t.Errorf("Expected 1 step, got %d", len(order))
		}

		if order[0] != "step-1" {
			t.Errorf("Expected 'step-1', got '%s'", order[0])
		}
	})

	t.Run("Linear chain", func(t *testing.T) {
		workflow := NewWorkflow("chain")
		step1 := NewStep("step-1", mockAgent("agent-1"))
		step2 := NewStep("step-2", mockAgent("agent-2"))
		step3 := NewStep("step-3", mockAgent("agent-3"))

		workflow.AddStep(step1)
		workflow.AddStep(step2)
		workflow.AddStep(step3)

		workflow.AddEdge("step-1", "step-2", nil)
		workflow.AddEdge("step-2", "step-3", nil)

		order, err := workflow.GetTopologicalOrder()
		if err != nil {
			t.Errorf("Failed to get topological order: %v", err)
		}

		if len(order) != 3 {
			t.Errorf("Expected 3 steps, got %d", len(order))
		}

		// Verify order: step-1 must come before step-2, step-2 before step-3
		pos1 := indexOf(order, "step-1")
		pos2 := indexOf(order, "step-2")
		pos3 := indexOf(order, "step-3")

		if !(pos1 < pos2 && pos2 < pos3) {
			t.Errorf("Invalid order: step-1 at %d, step-2 at %d, step-3 at %d", pos1, pos2, pos3)
		}
	})

	t.Run("Diamond pattern", func(t *testing.T) {
		workflow := NewWorkflow("diamond")
		start := NewStep("start", mockAgent("agent-1"))
		left := NewStep("left", mockAgent("agent-2"))
		right := NewStep("right", mockAgent("agent-3"))
		end := NewStep("end", mockAgent("agent-4"))

		workflow.AddStep(start)
		workflow.AddStep(left)
		workflow.AddStep(right)
		workflow.AddStep(end)

		workflow.AddEdge("start", "left", nil)
		workflow.AddEdge("start", "right", nil)
		workflow.AddEdge("left", "end", nil)
		workflow.AddEdge("right", "end", nil)

		order, err := workflow.GetTopologicalOrder()
		if err != nil {
			t.Errorf("Failed to get topological order: %v", err)
		}

		// Verify constraints
		posStart := indexOf(order, "start")
		posLeft := indexOf(order, "left")
		posRight := indexOf(order, "right")
		posEnd := indexOf(order, "end")

		if !(posStart < posLeft && posStart < posRight) {
			t.Error("Start should come before left and right")
		}

		if !(posLeft < posEnd && posRight < posEnd) {
			t.Error("Left and right should come before end")
		}
	})

	t.Run("Invalid workflow", func(t *testing.T) {
		workflow := NewWorkflow("invalid")
		workflow.AddStep(NewStep("step-1", mockAgent("agent-1")))
		workflow.AddStep(NewStep("step-2", mockAgent("agent-2")))

		// Create a cycle
		workflow.AddEdge("step-1", "step-2", nil)
		workflow.AddEdge("step-2", "step-1", nil)

		_, err := workflow.GetTopologicalOrder()
		if err == nil {
			t.Error("Expected error for invalid workflow")
		}
	})
}

// TestGetIndependentSteps tests grouping steps by dependency level
func TestGetIndependentSteps(t *testing.T) {
	t.Run("Single level (no dependencies)", func(t *testing.T) {
		workflow := NewWorkflow("no-deps")
		workflow.AddStep(NewStep("step-1", mockAgent("agent-1")))
		workflow.AddStep(NewStep("step-2", mockAgent("agent-2")))
		workflow.AddStep(NewStep("step-3", mockAgent("agent-3")))

		levels, err := workflow.GetIndependentSteps()
		if err != nil {
			t.Errorf("Failed to get independent steps: %v", err)
		}

		if len(levels) != 1 {
			t.Errorf("Expected 1 level, got %d", len(levels))
		}

		if len(levels[0]) != 3 {
			t.Errorf("Expected 3 steps in level 0, got %d", len(levels[0]))
		}
	})

	t.Run("Multiple levels", func(t *testing.T) {
		workflow := NewWorkflow("multi-level")
		workflow.AddStep(NewStep("start", mockAgent("agent-1")))
		workflow.AddStep(NewStep("middle-1", mockAgent("agent-2")))
		workflow.AddStep(NewStep("middle-2", mockAgent("agent-3")))
		workflow.AddStep(NewStep("end", mockAgent("agent-4")))

		workflow.AddEdge("start", "middle-1", nil)
		workflow.AddEdge("start", "middle-2", nil)
		workflow.AddEdge("middle-1", "end", nil)
		workflow.AddEdge("middle-2", "end", nil)

		levels, err := workflow.GetIndependentSteps()
		if err != nil {
			t.Errorf("Failed to get independent steps: %v", err)
		}

		if len(levels) != 3 {
			t.Errorf("Expected 3 levels, got %d", len(levels))
		}

		// Level 0: start
		if len(levels[0]) != 1 {
			t.Errorf("Expected 1 step in level 0, got %d", len(levels[0]))
		}

		// Level 1: middle-1, middle-2 (parallel)
		if len(levels[1]) != 2 {
			t.Errorf("Expected 2 steps in level 1, got %d", len(levels[1]))
		}

		// Level 2: end
		if len(levels[2]) != 1 {
			t.Errorf("Expected 1 step in level 2, got %d", len(levels[2]))
		}
	})
}

// TestWorkflowMetadata tests metadata operations on workflow
func TestWorkflowMetadata(t *testing.T) {
	workflow := NewWorkflow("test")

	// Set metadata
	workflow.SetMetadata("key1", "value1")
	workflow.SetMetadata("key2", 42)
	workflow.SetMetadata("key3", true)

	// Get metadata
	value, exists := workflow.GetMetadata("key1")
	if !exists {
		t.Error("Expected metadata to exist")
	}

	if value != "value1" {
		t.Errorf("Expected 'value1', got %v", value)
	}

	value, exists = workflow.GetMetadata("key2")
	if !exists {
		t.Error("Expected metadata to exist")
	}

	if value != 42 {
		t.Errorf("Expected 42, got %v", value)
	}

	// Get non-existent metadata
	_, exists = workflow.GetMetadata("non-existent")
	if exists {
		t.Error("Expected non-existent metadata to not exist")
	}
}

// TestStepMetadata tests metadata operations on steps
func TestStepMetadata(t *testing.T) {
	step := NewStep("test-step", mockAgent("test-agent"))

	// Set metadata
	step.SetMetadata("key1", "value1")
	step.SetMetadata("priority", 1)

	// Get metadata
	value, exists := step.GetMetadata("key1")
	if !exists {
		t.Error("Expected metadata to exist")
	}

	if value != "value1" {
		t.Errorf("Expected 'value1', got %v", value)
	}
}

// TestWorkflowInputOutputMappings tests input and output mappings
func TestWorkflowInputOutputMappings(t *testing.T) {
	workflow := NewWorkflow("test")

	// Test input mapping
	inputMapping := func(ctx *WorkflowContext) (string, error) {
		topic, ok := ctx.Get("topic").(string)
		if !ok {
			return "", nil
		}
		return "Topic: " + topic, nil
	}
	workflow.SetInputMapping(inputMapping)

	mapping := workflow.GetInputMapping()
	if mapping == nil {
		t.Error("Expected input mapping to be set")
	}

	// Test output mapping
	outputMapping := func(result *agent.AgentResult, ctx *WorkflowContext) error {
		ctx.Set("output", result.FinalText())
		return nil
	}
	workflow.SetOutputMapping(outputMapping)

	outputMapping2 := workflow.GetOutputMapping()
	if outputMapping2 == nil {
		t.Error("Expected output mapping to be set")
	}
}

// TestWorkflowContext tests workflow context operations
func TestWorkflowContext(t *testing.T) {
	ctx := context.Background()
	input := map[string]any{
		"topic": "test topic",
		"value": 123,
	}

	wfCtx := NewWorkflowContext(ctx, input)

	// Test Get
	topic := wfCtx.Get("topic")
	if topic != "test topic" {
		t.Errorf("Expected 'test topic', got %v", topic)
	}

	value := wfCtx.Get("value")
	if value != 123 {
		t.Errorf("Expected 123, got %v", value)
	}

	// Test Set and GetOutput
	wfCtx.Set("result", "success")
	output, exists := wfCtx.GetOutput("result")
	if !exists {
		t.Error("Expected output to exist")
	}

	if output != "success" {
		t.Errorf("Expected 'success', got %v", output)
	}

	// Test SetStepOutput and GetStepOutput
	mockResult := &agent.AgentResult{
		Output:   message.AssistantMessage("test output"),
		Usage:    provider.TokenUsage{TotalTokens: 100},
		Turns:    1,
		Duration: time.Second,
		Metadata: map[string]any{},
	}
	wfCtx.SetStepOutput("step-1", mockResult)

	result, err := wfCtx.GetStepOutput("step-1")
	if err != nil {
		t.Errorf("Failed to get step output: %v", err)
	}

	if result.FinalText() != "test output" {
		t.Errorf("Expected 'test output', got '%s'", result.FinalText())
	}

	// Test GetStepOutput for non-existent step
	_, err = wfCtx.GetStepOutput("non-existent")
	if err == nil {
		t.Error("Expected error when getting non-existent step output")
	}

	// Test metadata
	wfCtx.SetMetadata("meta-key", "meta-value")
	metaValue, exists := wfCtx.GetMetadata("meta-key")
	if !exists {
		t.Error("Expected metadata to exist")
	}

	if metaValue != "meta-value" {
		t.Errorf("Expected 'meta-value', got %v", metaValue)
	}

	// Test current step
	wfCtx.SetCurrentStep("step-2")
	if wfCtx.CurrentStep() != "step-2" {
		t.Errorf("Expected 'step-2', got '%s'", wfCtx.CurrentStep())
	}

	// Test Context() - returns a valid context (wrapped, not the original due to WithCancel)
	wrappedCtx := wfCtx.Context()
	if wrappedCtx == nil {
		t.Error("Expected wrapped context to not be nil")
	}
	// Verify the wrapped context is valid (no blocking check)
	if wrappedCtx.Done() == nil {
		t.Error("Expected valid context with Done channel")
	}
}

// TestRetryPolicy tests retry policy configuration and delay computation
func TestRetryPolicy(t *testing.T) {
	t.Run("Default retry policy", func(t *testing.T) {
		policy := DefaultRetryPolicy()

		if policy.MaxAttempts != 3 {
			t.Errorf("Expected MaxAttempts=3, got %d", policy.MaxAttempts)
		}

		if policy.InitialDelay != time.Second {
			t.Errorf("Expected InitialDelay=1s, got %v", policy.InitialDelay)
		}

		if policy.MaxDelay != 30*time.Second {
			t.Errorf("Expected MaxDelay=30s, got %v", policy.MaxDelay)
		}

		if policy.Multiplier != 2.0 {
			t.Errorf("Expected Multiplier=2.0, got %f", policy.Multiplier)
		}

		if !policy.Jitter {
			t.Error("Expected Jitter=true")
		}
	})

	t.Run("Delay computation", func(t *testing.T) {
		policy := DefaultRetryPolicy()

		// Attempt 0: no delay
		delay := policy.ComputeDelay(0)
		if delay != 0 {
			t.Errorf("Expected 0 delay for attempt 0, got %v", delay)
		}

		// Attempt 1: initial delay
		// Attempt 1: initial delay (with jitter: 750ms - 1250ms)
		delay = policy.ComputeDelay(1)
		if delay < 750*time.Millisecond || delay > 1250*time.Millisecond {
			t.Errorf("Expected delay around 1s (with jitter) for attempt 1, got %v", delay)
		}

		// Attempt 2: doubled (with jitter: 1.5s - 2.5s)
		delay = policy.ComputeDelay(2)
		if delay < 1500*time.Millisecond || delay > 2500*time.Millisecond {
			t.Errorf("Expected delay around 2s (with jitter) for attempt 2, got %v", delay)
		}

		// Attempt 3: quadrupled (with jitter: 3s - 5s)
		delay = policy.ComputeDelay(3)
		if delay < 3*time.Second || delay > 5*time.Second {
			t.Errorf("Expected delay around 4s (with jitter) for attempt 3, got %v", delay)
		}

		// Test max delay cap
		policy.MaxDelay = 3 * time.Second
		delay = policy.ComputeDelay(10)
		// With jitter, capped delay could be up to 3.75s (3s + 25%)
		if delay > 4*time.Second {
			t.Errorf("Expected delay to be capped at ~3s (with jitter), got %v", delay)
		}
	})

	t.Run("No jitter", func(t *testing.T) {
		policy := &RetryPolicy{
			MaxAttempts:  3,
			InitialDelay: time.Second,
			MaxDelay:     30 * time.Second,
			Multiplier:   2.0,
			Jitter:       false,
		}

		// Without jitter, delay should be deterministic
		delay := policy.ComputeDelay(1)
		if delay != time.Second {
			t.Errorf("Expected 1s delay without jitter, got %v", delay)
		}

		delay = policy.ComputeDelay(2)
		if delay != 2*time.Second {
			t.Errorf("Expected 2s delay without jitter, got %v", delay)
		}
	})
}

// TestStep tests step creation and configuration
func TestStep(t *testing.T) {
	t.Run("New step defaults", func(t *testing.T) {
		step := NewStep("test-step", mockAgent("test-agent"))

		if step.ID != "test-step" {
			t.Errorf("Expected ID 'test-step', got '%s'", step.ID)
		}

		if step.Agent == nil {
			t.Error("Agent should not be nil")
		}

		if step.Timeout != 5*time.Minute {
			t.Errorf("Expected default timeout of 5m, got %v", step.Timeout)
		}

		if step.RetryPolicy == nil {
			t.Error("RetryPolicy should not be nil")
		}

		if step.Condition == nil {
			t.Error("Condition should not be nil")
		}

		if step.InputMap == nil {
			t.Error("InputMap should not be nil")
		}

		if step.OutputMap == nil {
			t.Error("OutputMap should not be nil")
		}
	})

	t.Run("Step with custom configuration", func(t *testing.T) {
		customRetry := &RetryPolicy{
			MaxAttempts:  5,
			InitialDelay: 2 * time.Second,
			MaxDelay:     60 * time.Second,
			Multiplier:   3.0,
			Jitter:       true,
		}

		customCondition := func(ctx *WorkflowContext) bool {
			return false
		}

		step := NewStep("test-step", mockAgent("test-agent"))
		step.RetryPolicy = customRetry
		step.Condition = customCondition
		step.Timeout = 10 * time.Minute

		if step.RetryPolicy.MaxAttempts != 5 {
			t.Errorf("Expected MaxAttempts=5, got %d", step.RetryPolicy.MaxAttempts)
		}

		if step.Timeout != 10*time.Minute {
			t.Errorf("Expected timeout of 10m, got %v", step.Timeout)
		}
	})
}

// TestConditions tests condition functions
func TestConditions(t *testing.T) {
	t.Run("Always true condition", func(t *testing.T) {
		ctx := NewWorkflowContext(context.Background(), map[string]any{})
		if !alwaysTrue(ctx) {
			t.Error("alwaysTrue should always return true")
		}
	})

	t.Run("Default mappings", func(t *testing.T) {
		ctx := NewWorkflowContext(context.Background(), map[string]any{
			"topic": "test",
		})

		// Default input mapping
		input, err := defaultInputMapping(ctx)
		if err != nil {
			t.Errorf("Default input mapping failed: %v", err)
		}

		if input != "test" {
			t.Errorf("Expected 'test', got '%s'", input)
		}

		// Default input mapping with no topic
		ctx = NewWorkflowContext(context.Background(), map[string]any{})
		_, err = defaultInputMapping(ctx)
		if err == nil {
			t.Error("Expected error when no 'topic' in context")
		}

		// Default output mapping
		mockResult := &agent.AgentResult{
			Output: message.AssistantMessage("test output"),
			Usage:  provider.TokenUsage{},
			Turns:  1,
		}
		err = defaultOutputMapping(mockResult, ctx)
		if err != nil {
			t.Errorf("Default output mapping failed: %v", err)
		}

		output, exists := ctx.GetOutput("output")
		if !exists {
			t.Error("Expected output to be set")
		}

		if output != "test output" {
			t.Errorf("Expected 'test output', got %v", output)
		}
	})
}

// Helper functions

func indexOf(slice []string, item string) int {
	for i, s := range slice {
		if s == item {
			return i
		}
	}
	return -1
}
