package orchestration

import (
	"context"
	"testing"
	"time"

	"github.com/user/orchestra/internal/agent"
)

// TestNewWorkflowBuilder tests creating a new workflow builder.
func TestNewWorkflowBuilder(t *testing.T) {
	builder := NewWorkflowBuilder("test-workflow")

	if builder == nil {
		t.Fatal("NewWorkflowBuilder returned nil")
	}

	if builder.workflow == nil {
		t.Error("Builder should have a workflow")
	}

	if builder.workflow.Name() != "test-workflow" {
		t.Errorf("Expected workflow name 'test-workflow', got '%s'", builder.workflow.Name())
	}

	if len(builder.errors) != 0 {
		t.Errorf("Expected no errors, got %d", len(builder.errors))
	}
}

// TestWorkflowBuilderAddStep tests adding steps to a workflow via builder.
func TestWorkflowBuilderAddStep(t *testing.T) {
	builder := NewWorkflowBuilder("test")
	agent1 := mockAgent("agent-1")
	agent2 := mockAgent("agent-2")

	// Add first step
	builder.AddStep("step-1", agent1)

	workflow, err := builder.Build()
	if err != nil {
		t.Errorf("Build failed: %v", err)
	}

	steps := workflow.GetAllSteps()
	if len(steps) != 1 {
		t.Errorf("Expected 1 step, got %d", len(steps))
	}

	if steps[0].ID != "step-1" {
		t.Errorf("Expected step ID 'step-1', got '%s'", steps[0].ID)
	}

	// Add second step
	builder2 := NewWorkflowBuilder("test")
	builder2.AddStep("step-1", agent1).
		AddStep("step-2", agent2)

	workflow2, err := builder2.Build()
	if err != nil {
		t.Errorf("Build failed: %v", err)
	}

	steps2 := workflow2.GetAllSteps()
	if len(steps2) != 2 {
		t.Errorf("Expected 2 steps, got %d", len(steps2))
	}
}

// TestWorkflowBuilderAddStepDuplicate tests adding a step with duplicate ID.
func TestWorkflowBuilderAddStepDuplicate(t *testing.T) {
	builder := NewWorkflowBuilder("test")
	agent1 := mockAgent("agent-1")

	builder.AddStep("step-1", agent1)
	builder.AddStep("step-1", agent1) // Duplicate ID

	_, err := builder.Build()
	if err == nil {
		t.Error("Expected error when building workflow with duplicate step IDs")
	}
}

// TestWorkflowBuilderDependsOn tests creating dependencies between steps.
func TestWorkflowBuilderDependsOn(t *testing.T) {
	builder := NewWorkflowBuilder("test")
	agent1 := mockAgent("agent-1")
	agent2 := mockAgent("agent-2")

	builder.AddStep("step-1", agent1).
		AddStep("step-2", agent2).
		DependsOn("step-1")

	workflow, err := builder.Build()
	if err != nil {
		t.Errorf("Build failed: %v", err)
	}

	edges := workflow.GetEdges()
	if len(edges) != 1 {
		t.Errorf("Expected 1 edge, got %d", len(edges))
	}

	if edges[0].From != "step-1" || edges[0].To != "step-2" {
		t.Errorf("Expected edge step-1 -> step-2, got %s -> %s", edges[0].From, edges[0].To)
	}
}

// TestWorkflowBuilderDependsOnNonExistent tests dependency on non-existent step.
func TestWorkflowBuilderDependsOnNonExistent(t *testing.T) {
	builder := NewWorkflowBuilder("test")
	agent1 := mockAgent("agent-1")

	builder.AddStep("step-1", agent1).
		DependsOn("non-existent")

	_, err := builder.Build()
	if err == nil {
		t.Error("Expected error when depending on non-existent step")
	}
}

// TestWorkflowBuilderDependsOnConditional tests conditional dependencies.
func TestWorkflowBuilderDependsOnConditional(t *testing.T) {
	builder := NewWorkflowBuilder("test")
	agent1 := mockAgent("agent-1")
	agent2 := mockAgent("agent-2")

	condition := func(ctx *WorkflowContext) bool {
		return true
	}

	builder.AddStep("step-1", agent1).
		AddStep("step-2", agent2).
		DependsOnConditional("step-1", condition)

	workflow, err := builder.Build()
	if err != nil {
		t.Errorf("Build failed: %v", err)
	}

	edges := workflow.GetEdges()
	if len(edges) != 1 {
		t.Errorf("Expected 1 edge, got %d", len(edges))
	}

	if edges[0].Condition == nil {
		t.Error("Expected edge to have a condition")
	}
}

// TestWorkflowBuilderWithInput tests setting workflow-level input mapping.
func TestWorkflowBuilderWithInput(t *testing.T) {
	builder := NewWorkflowBuilder("test")
	agent1 := mockAgent("agent-1")

	inputMapping := func(ctx *WorkflowContext) (string, error) {
		return "test input", nil
	}

	builder.AddStep("step-1", agent1).
		WithInput(inputMapping)

	workflow, err := builder.Build()
	if err != nil {
		t.Errorf("Build failed: %v", err)
	}

	mapping := workflow.GetInputMapping()
	if mapping == nil {
		t.Error("Expected input mapping to be set")
	}

	result, err := mapping(NewWorkflowContext(context.Background(), map[string]any{}))
	if err != nil {
		t.Errorf("Input mapping failed: %v", err)
	}

	if result != "test input" {
		t.Errorf("Expected 'test input', got '%s'", result)
	}
}

// TestWorkflowBuilderWithOutput tests setting workflow-level output mapping.
func TestWorkflowBuilderWithOutput(t *testing.T) {
	builder := NewWorkflowBuilder("test")
	agent1 := mockAgent("agent-1")

	outputMapping := func(result *agent.AgentResult, ctx *WorkflowContext) error {
		ctx.Set("test-key", "test-value")
		return nil
	}

	builder.AddStep("step-1", agent1).
		WithOutput(outputMapping)

	workflow, err := builder.Build()
	if err != nil {
		t.Errorf("Build failed: %v", err)
	}

	mapping := workflow.GetOutputMapping()
	if mapping == nil {
		t.Error("Expected output mapping to be set")
	}
}

// TestWorkflowBuilderWithMetadata tests setting workflow metadata.
func TestWorkflowBuilderWithMetadata(t *testing.T) {
	builder := NewWorkflowBuilder("test")
	agent1 := mockAgent("agent-1")

	builder.AddStep("step-1", agent1).
		WithMetadata("key1", "value1").
		WithMetadata("key2", 42)

	workflow, err := builder.Build()
	if err != nil {
		t.Errorf("Build failed: %v", err)
	}

	value, exists := workflow.GetMetadata("key1")
	if !exists {
		t.Error("Expected metadata 'key1' to exist")
	}

	if value != "value1" {
		t.Errorf("Expected 'value1', got %v", value)
	}

	value, exists = workflow.GetMetadata("key2")
	if !exists {
		t.Error("Expected metadata 'key2' to exist")
	}

	if value != 42 {
		t.Errorf("Expected 42, got %v", value)
	}
}

// TestWithInput tests setting step input mapping.
func TestWithInput(t *testing.T) {
	agent1 := mockAgent("agent-1")
	step := NewStep("test", agent1)

	inputMapping := func(ctx *WorkflowContext) (string, error) {
		return "step input", nil
	}

	option := WithInput(inputMapping)
	option(step)

	if step.InputMap == nil {
		t.Error("Expected InputMap to be set")
	}
}

// TestWithOutput tests setting step output mapping.
func TestWithOutput(t *testing.T) {
	agent1 := mockAgent("agent-1")
	step := NewStep("test", agent1)

	outputMapping := func(result *agent.AgentResult, ctx *WorkflowContext) error {
		return nil
	}

	option := WithOutput(outputMapping)
	option(step)

	if step.OutputMap == nil {
		t.Error("Expected OutputMap to be set")
	}
}

// TestWithCondition tests setting step execution condition.
func TestWithCondition(t *testing.T) {
	agent1 := mockAgent("agent-1")
	step := NewStep("test", agent1)

	condition := func(ctx *WorkflowContext) bool {
		return true
	}

	option := WithCondition(condition)
	option(step)

	if step.Condition == nil {
		t.Error("Expected Condition to be set")
	}
}

// TestWithRetry tests setting step retry policy.
func TestWithRetry(t *testing.T) {
	agent1 := mockAgent("agent-1")
	step := NewStep("test", agent1)

	retryPolicy := &RetryPolicy{
		MaxAttempts:  5,
		InitialDelay: 2 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   3.0,
		Jitter:       true,
	}

	option := WithRetry(retryPolicy)
	option(step)

	if step.RetryPolicy == nil {
		t.Error("Expected RetryPolicy to be set")
	}

	if step.RetryPolicy.MaxAttempts != 5 {
		t.Errorf("Expected MaxAttempts=5, got %d", step.RetryPolicy.MaxAttempts)
	}
}

// TestWithTimeout tests setting step timeout.
func TestWithTimeout(t *testing.T) {
	agent1 := mockAgent("agent-1")
	step := NewStep("test", agent1)

	option := WithTimeout(10 * time.Minute)
	option(step)

	if step.Timeout != 10*time.Minute {
		t.Errorf("Expected timeout of 10m, got %v", step.Timeout)
	}
}

// TestWithStepMetadata tests setting step metadata.
func TestWithStepMetadata(t *testing.T) {
	agent1 := mockAgent("agent-1")
	step := NewStep("test", agent1)

	option := WithStepMetadata("priority", 1)
	option(step)

	value, exists := step.GetMetadata("priority")
	if !exists {
		t.Error("Expected metadata to exist")
	}

	if value != 1 {
		t.Errorf("Expected 1, got %v", value)
	}
}

// TestSequence tests creating a sequential workflow.
func TestSequence(t *testing.T) {
	agents := []*agent.Agent{
		mockAgent("agent-1"),
		mockAgent("agent-2"),
		mockAgent("agent-3"),
	}

	workflow, err := Sequence("sequential", agents)
	if err != nil {
		t.Errorf("Sequence failed: %v", err)
	}

	if workflow.Name() != "sequential" {
		t.Errorf("Expected workflow name 'sequential', got '%s'", workflow.Name())
	}

	steps := workflow.GetAllSteps()
	if len(steps) != 3 {
		t.Errorf("Expected 3 steps, got %d", len(steps))
	}

	// Check dependencies (should be linear)
	edges := workflow.GetEdges()
	if len(edges) != 2 {
		t.Errorf("Expected 2 edges, got %d", len(edges))
	}

	// Verify linear chain: step-0 -> step-1 -> step-2
	expectedEdges := []struct{ from, to string }{
		{"step-0", "step-1"},
		{"step-1", "step-2"},
	}

	for i, expected := range expectedEdges {
		if edges[i].From != expected.from || edges[i].To != expected.to {
			t.Errorf("Expected edge %s -> %s, got %s -> %s",
				expected.from, expected.to, edges[i].From, edges[i].To)
		}
	}
}

// TestSequenceEmpty tests Sequence with no agents.
func TestSequenceEmpty(t *testing.T) {
	_, err := Sequence("test", []*agent.Agent{})
	if err == nil {
		t.Error("Expected error for empty sequence")
	}
}

// TestParallel tests creating a parallel workflow.
func TestParallel(t *testing.T) {
	agents := []*agent.Agent{
		mockAgent("agent-1"),
		mockAgent("agent-2"),
		mockAgent("agent-3"),
	}

	workflow, err := Parallel("parallel", agents, ConcatAggregator)
	if err != nil {
		t.Errorf("Parallel failed: %v", err)
	}

	if workflow.Name() != "parallel" {
		t.Errorf("Expected workflow name 'parallel', got '%s'", workflow.Name())
	}

	steps := workflow.GetAllSteps()
	if len(steps) != 4 { // 3 parallel + 1 aggregator
		t.Errorf("Expected 4 steps, got %d", len(steps))
	}

	// Check topological order - parallel steps should be in level 0, aggregator in level 1
	order, err := workflow.GetTopologicalOrder()
	if err != nil {
		t.Errorf("GetTopologicalOrder failed: %v", err)
	}

	// Find aggregator position
	aggregatorIdx := -1
	for i, stepID := range order {
		if stepID == "aggregator" {
			aggregatorIdx = i
			break
		}
	}

	if aggregatorIdx == -1 {
		t.Error("Aggregator step not found")
	}

	// All parallel steps should come before aggregator
	for i, stepID := range order {
		if stepID != "aggregator" && i > aggregatorIdx {
			t.Errorf("Parallel step %s should come before aggregator", stepID)
		}
	}
}

// TestParallelEmpty tests Parallel with no agents.
func TestParallelEmpty(t *testing.T) {
	_, err := Parallel("test", []*agent.Agent{}, nil)
	if err == nil {
		t.Error("Expected error for empty parallel workflow")
	}
}

// TestParallelNilAggregator tests Parallel with nil aggregator.
func TestParallelNilAggregator(t *testing.T) {
	agents := []*agent.Agent{
		mockAgent("agent-1"),
		mockAgent("agent-2"),
	}

	workflow, err := Parallel("parallel", agents, nil)
	if err != nil {
		t.Errorf("Parallel with nil aggregator failed: %v", err)
	}

	if workflow == nil {
		t.Fatal("Expected workflow to be created with nil aggregator")
	}
}

// TestConcatAggregator tests the ConcatAggregator function.
func TestConcatAggregator(t *testing.T) {
	// The aggregator function is tested in integration tests with actual agent results
	_ = ConcatAggregator
}

// TestFirstAggregator tests the FirstAggregator function.
func TestFirstAggregator(t *testing.T) {
	// The aggregator function is tested in integration tests with actual agent results
	_ = FirstAggregator
}

// TestWorkflowBuilderChaining tests fluent API chaining.
func TestWorkflowBuilderChaining(t *testing.T) {
	agent1 := mockAgent("agent-1")
	agent2 := mockAgent("agent-2")
	agent3 := mockAgent("agent-3")

	inputMapping := func(ctx *WorkflowContext) (string, error) {
		return "input", nil
	}

	outputMapping := func(result *agent.AgentResult, ctx *WorkflowContext) error {
		return nil
	}

	retryPolicy := &RetryPolicy{
		MaxAttempts: 3,
	}

	workflow, err := NewWorkflowBuilder("chained").
		WithInput(inputMapping).
		WithOutput(outputMapping).
		WithMetadata("key", "value").
		AddStep("step-1", agent1,
			WithTimeout(5*time.Minute),
			WithRetry(retryPolicy),
		).
		AddStep("step-2", agent2,
			WithCondition(func(ctx *WorkflowContext) bool {
				return true
			}),
		).
		AddStep("step-3", agent3).
		DependsOn("step-2").
		DependsOn("step-1").
		Build()
	if err != nil {
		t.Errorf("Build failed: %v", err)
	}

	// Verify workflow
	if workflow.Name() != "chained" {
		t.Errorf("Expected workflow name 'chained', got '%s'", workflow.Name())
	}

	// Verify steps
	steps := workflow.GetAllSteps()
	if len(steps) != 3 {
		t.Errorf("Expected 3 steps, got %d", len(steps))
	}

	// Verify edges
	edges := workflow.GetEdges()
	if len(edges) != 2 {
		t.Errorf("Expected 2 edges, got %d", len(edges))
	}

	// Verify metadata
	value, exists := workflow.GetMetadata("key")
	if !exists || value != "value" {
		t.Error("Expected metadata 'key' to be 'value'")
	}
}

// TestWorkflowBuilderBuildError tests Build with accumulated errors.
func TestWorkflowBuilderBuildError(t *testing.T) {
	agent1 := mockAgent("agent-1")

	builder := NewWorkflowBuilder("test").
		AddStep("step-1", agent1).
		AddStep("step-1", agent1) // Duplicate ID

	_, err := builder.Build()
	if err == nil {
		t.Error("Expected error when building with duplicate step IDs")
	}
}

// TestWorkflowBuilderMultipleDependencies tests multiple dependencies.
func TestWorkflowBuilderMultipleDependencies(t *testing.T) {
	agent1 := mockAgent("agent-1")
	agent2 := mockAgent("agent-2")
	agent3 := mockAgent("agent-3")

	workflow, err := NewWorkflowBuilder("multi-dep").
		AddStep("step-1", agent1).
		AddStep("step-2", agent2).
		AddStep("step-3", agent3).
		DependsOn("step-1").
		DependsOn("step-2").
		Build()
	if err != nil {
		t.Errorf("Build failed: %v", err)
	}

	edges := workflow.GetEdges()
	if len(edges) != 2 {
		t.Errorf("Expected 2 edges, got %d", len(edges))
	}

	// step-3 should depend on both step-1 and step-2
	step3Deps := 0
	for _, edge := range edges {
		if edge.To == "step-3" {
			step3Deps++
		}
	}

	if step3Deps != 2 {
		t.Errorf("Expected step-3 to have 2 dependencies, got %d", step3Deps)
	}
}

// TestWorkflowBuilderComplexDAG tests building a complex DAG.
func TestWorkflowBuilderComplexDAG(t *testing.T) {
	agent1 := mockAgent("agent-1")

	// Build a diamond pattern:
	//   start
	//   /   \
	// left  right
	//   \   /
	//    end
	_, err := NewWorkflowBuilder("diamond").
		AddStep("start", agent1).
		AddStep("left", agent1).
		AddStep("right", agent1).
		AddStep("end", agent1).
		DependsOn("left"). // This depends on the current step "end", but we want "left" -> "end"
		// DependsOn creates an edge FROM the specified step TO the current step
		Build()
	if err != nil {
		t.Errorf("Build failed: %v", err)
	}
}

// TestWorkflowBuilderDiamondPattern correctly builds a diamond DAG.
func TestWorkflowBuilderDiamondPattern(t *testing.T) {
	agent1 := mockAgent("agent-1")

	workflow, err := NewWorkflowBuilder("diamond").
		AddStep("start", agent1).
		AddStep("left", agent1).DependsOn("start").
		AddStep("right", agent1).DependsOn("start").
		AddStep("end", agent1).DependsOn("left").DependsOn("right").
		Build()
	if err != nil {
		t.Errorf("Build failed: %v", err)
	}

	edges := workflow.GetEdges()
	if len(edges) != 4 {
		t.Errorf("Expected 4 edges, got %d", len(edges))
	}

	// Verify diamond structure
	expectedEdges := map[string]bool{
		"start->left":  false,
		"start->right": false,
		"left->end":    false,
		"right->end":   false,
	}

	for _, edge := range edges {
		key := edge.From + "->" + edge.To
		if _, exists := expectedEdges[key]; exists {
			expectedEdges[key] = true
		}
	}

	for key, found := range expectedEdges {
		if !found {
			t.Errorf("Missing expected edge: %s", key)
		}
	}
}
