package orchestration

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/user/orchestra/internal/agent"
)

// BasicWorkflow demonstrates creating a simple workflow with the builder API.
func BasicWorkflow() {
	// Create agents (in practice, you'd create these with actual providers)
	researcher := mockAgent("researcher")
	writer := mockAgent("writer")
	reviewer := mockAgent("reviewer")

	// Build a sequential workflow
	workflow, err := NewWorkflowBuilder("content-pipeline").
		AddStep("research", researcher,
			WithInput(func(ctx *WorkflowContext) (string, error) {
				topic, ok := ctx.Get("topic").(string)
				if !ok {
					return "", fmt.Errorf("no topic provided")
				}
				return fmt.Sprintf("Research: %s", topic), nil
			}),
		).
		AddStep("write", writer,
			WithInput(func(ctx *WorkflowContext) (string, error) {
				topic := ctx.Get("topic")
				researchOutput, _ := ctx.GetStepOutput("research")
				return fmt.Sprintf("Write about: %v\nBased on research: %s",
					topic, researchOutput.FinalText()), nil
			}),
		).DependsOn("research").
		AddStep("review", reviewer,
			WithInput(func(ctx *WorkflowContext) (string, error) {
				writeOutput, _ := ctx.GetStepOutput("write")
				return fmt.Sprintf("Review: %s", writeOutput.FinalText()), nil
			}),
		).DependsOn("write").
		WithOutput(func(result *agent.AgentResult, ctx *WorkflowContext) error {
			ctx.Set("final_review", result.FinalText())
			return nil
		}).
		Build()

	if err != nil {
		log.Fatalf("Failed to build workflow: %v", err)
	}

	// Execute workflow
	engine := NewEngine()
	ctx := context.Background()
	input := map[string]any{
		"topic": "AI orchestration patterns",
	}

	result, err := engine.Execute(ctx, workflow, input)
	if err != nil {
		log.Fatalf("Workflow execution failed: %v", err)
	}

	fmt.Printf("Workflow completed in %v\n", result.Duration)
	fmt.Printf("Total tokens used: %d\n", result.Usage.TotalTokens)
	fmt.Printf("Final output: %s\n", result.Output["final_review"])
}

// SequentialWorkflow demonstrates the Sequence convenience function.
func SequentialWorkflow() {
	agents := []*agent.Agent{
		mockAgent("step-1-processor"),
		mockAgent("step-2-processor"),
		mockAgent("step-3-processor"),
	}

	workflow, err := Sequence("processing-pipeline", agents)
	if err != nil {
		log.Fatalf("Failed to create sequence: %v", err)
	}

	engine := NewEngine()
	result, err := engine.Execute(context.Background(), workflow,
		map[string]any{"input": "process this data"})

	if err != nil {
		log.Fatalf("Execution failed: %v", err)
	}

	fmt.Printf("Pipeline completed with status: %s\n", result.Status)
	fmt.Printf("Steps executed: %d\n", len(result.Steps))
}

// ParallelWorkflow demonstrates parallel execution with aggregation.
func ParallelWorkflow() {
	// Create multiple perspectives on the same input
	optimist := mockAgent("optimist")
	pessimist := mockAgent("pessimist")
	neutral := mockAgent("neutral")

	// Custom aggregator: synthesizes multiple perspectives
	synthesizer := func(results []*agent.AgentResult) (string, error) {
		var synthesis string
		synthesis += "=== Synthesis of Multiple Perspectives ===\n\n"

		for i, result := range results {
			synthesis += fmt.Sprintf("Perspective %d:\n%s\n\n", i+1, result.FinalText())
		}

		synthesis += "=== Balanced View ===\n"
		synthesis += "The above perspectives have been synthesized into a balanced view."
		return synthesis, nil
	}

	workflow, err := Parallel("multi-perspective-analysis",
		[]*agent.Agent{optimist, pessimist, neutral},
		synthesizer,
	)
	if err != nil {
		log.Fatalf("Failed to create parallel workflow: %v", err)
	}

	engine := NewEngine()
	result, err := engine.Execute(context.Background(), workflow,
		map[string]any{"topic": "future of AI"})

	if err != nil {
		log.Fatalf("Execution failed: %v", err)
	}

	fmt.Printf("Parallel workflow completed in %v\n", result.Duration)
	fmt.Printf("Synthesized output: %s\n", result.Output["output"])
}

// RouterWorkflow demonstrates dynamic routing based on input content.
func RouterWorkflow() {
	// Create specialized agents for different task types
	codeAgent := mockAgent("code-specialist")
	creativeAgent := mockAgent("creative-writer")
	mathAgent := mockAgent("math-expert")
	generalAgent := mockAgent("general-assistant")

	// Create router with conditional routes
	router := NewRouter("task-router").
		AddRoute("code", ContainsCode, codeAgent).
		AddRoute("creative", IsCreative, creativeAgent).
		AddRoute("math", IsMath, mathAgent).
		SetDefault(generalAgent)

	workflow, err := router.Build()
	if err != nil {
		log.Fatalf("Failed to build router workflow: %v", err)
	}

	engine := NewEngine()

	// Example 1: Code-related task
	fmt.Println("--- Example 1: Code Task ---")
	result1, err := engine.Execute(context.Background(), workflow,
		map[string]any{"topic": "Debug this Python function"})
	if err != nil {
		log.Printf("Execution failed: %v", err)
	} else {
		fmt.Printf("Status: %s\n", result1.Status)
	}

	// Example 2: Creative task
	fmt.Println("\n--- Example 2: Creative Task ---")
	result2, err := engine.Execute(context.Background(), workflow,
		map[string]any{"topic": "Write a short story about space exploration"})
	if err != nil {
		log.Printf("Execution failed: %v", err)
	} else {
		fmt.Printf("Status: %s\n", result2.Status)
	}

	// Example 3: Math task
	fmt.Println("\n--- Example 3: Math Task ---")
	result3, err := engine.Execute(context.Background(), workflow,
		map[string]any{"topic": "Calculate the derivative of x^2"})
	if err != nil {
		log.Printf("Execution failed: %v", err)
	} else {
		fmt.Printf("Status: %s\n", result3.Status)
	}
}

// DebateWorkflow demonstrates multi-round debate with judge.
func DebateWorkflow() {
	// Create debaters with different perspectives
	proponent := mockAgent("pro-debater")
	opponent := mockAgent("con-debater")
	judge := mockAgent("judge")

	// Configure debate
	config := DebateConfig{
		Name:    "ai-safety-debate",
		Debaters: []*agent.Agent{proponent, opponent},
		Judge:    judge,
		Rounds:   3,
		Topic:    "AI development should be paused until safety is proven",
	}

	workflow, err := Debate(config)
	if err != nil {
		log.Fatalf("Failed to create debate workflow: %v", err)
	}

	engine := NewEngine()
	result, err := engine.Execute(context.Background(), workflow,
		map[string]any{})

	if err != nil {
		log.Fatalf("Debate execution failed: %v", err)
	}

	    fmt.Printf("Debate completed in %v\n", result.Duration)
	    fmt.Printf("Judge's verdict: %s\n", result.Output["verdict"])
}

// HierarchicalWorkflow demonstrates manager-worker delegation.
func HierarchicalWorkflow() {
	// Create manager and specialized workers
	manager := mockAgent("project-manager")
	researcher := mockAgent("research-worker")
	coder := mockAgent("coding-worker")
	tester := mockAgent("testing-worker")

	config := HierarchicalConfig{
		Name:   "software-development",
		Manager: manager,
		Workers: map[string]*agent.Agent{
			"research": researcher,
			"coding":   coder,
			"testing":  tester,
		},
		MaxDelegations: 5,
		MaxDepth:       2,
	}

	workflow, err := Hierarchical(config)
	if err != nil {
		log.Fatalf("Failed to create hierarchical workflow: %v", err)
	}

	engine := NewEngine()
	result, err := engine.Execute(context.Background(), workflow,
		map[string]any{"topic": "Build a simple REST API"})

	if err != nil {
		log.Fatalf("Hierarchical execution failed: %v", err)
	}

	fmt.Printf("Project completed in %v\n", result.Duration)
	fmt.Printf("Final report:\n%s\n", result.Output["final_report"])
}

// StreamingWorkflow demonstrates streaming workflow events.
func StreamingWorkflow() {
	workflow, err := Sequence("streaming-pipeline",
		[]*agent.Agent{
			mockAgent("processor-1"),
			mockAgent("processor-2"),
		})
	if err != nil {
		log.Fatalf("Failed to create workflow: %v", err)
	}

	engine := NewEngine()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	eventChan, err := engine.Stream(ctx, workflow,
		map[string]any{"input": "stream this data"})
	if err != nil {
		log.Fatalf("Failed to start streaming: %v", err)
	}

	fmt.Println("=== Streaming Workflow Events ===")
	for event := range eventChan {
		switch event.Type {
		case WorkflowEventStarted:
			fmt.Printf("[START] Workflow started\n")
		case WorkflowEventStepStarted:
			fmt.Printf("[STEP] Step started: %s\n", event.StepID)
		case WorkflowEventStepCompleted:
			fmt.Printf("[STEP] Step completed: %s\n", event.StepID)
		case WorkflowEventCompleted:
			fmt.Printf("[DONE] Workflow completed successfully\n")
		case WorkflowEventFailed:
			fmt.Printf("[ERROR] Workflow failed: %v\n", event.Error)
			return
		case WorkflowEventCancelled:
			fmt.Printf("[CANCEL] Workflow cancelled\n")
			return
		}
	}
}

// RetryPolicyDemo demonstrates custom retry configuration.
func RetryPolicyDemo() {
	retryPolicy := &RetryPolicy{
		MaxAttempts:  5,
		InitialDelay: 2 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   3.0,
		Jitter:       true,
	}

	workflow, err := NewWorkflowBuilder("retry-example").
		AddStep("unreliable-step", mockAgent("flaky-agent"),
			WithRetry(retryPolicy),
			WithTimeout(10*time.Second),
		).
		Build()

	if err != nil {
		log.Fatalf("Failed to build workflow: %v", err)
	}

	engine := NewEngine()
	result, err := engine.Execute(context.Background(), workflow,
		map[string]any{"task": "this might fail"})

	if err != nil {
		fmt.Printf("Workflow failed after retries: %v\n", err)
	} else {
		fmt.Printf("Workflow succeeded on attempt %d\n", result.Steps["unreliable-step"].Attempts)
	}
}

// ConditionalExecution demonstrates conditional step execution.
func ConditionalExecution() {
	workflow, err := NewWorkflowBuilder("conditional-workflow").
		AddStep("analyze", mockAgent("analyzer")).
		AddStep("process-a", mockAgent("processor-a"),
			WithCondition(func(ctx *WorkflowContext) bool {
				// Only execute if analysis flag is set
				flag, ok := ctx.Get("use_processor_a").(bool)
				return ok && flag
			}),
		).DependsOn("analyze").
		AddStep("process-b", mockAgent("processor-b"),
			WithCondition(func(ctx *WorkflowContext) bool {
				// Only execute if use_processor_a is not set or false
				flag, ok := ctx.Get("use_processor_a").(bool)
				return !ok || !flag
			}),
		).DependsOn("analyze").
		AddStep("finalize", mockAgent("finalizer")).
		DependsOn("process-a").
		DependsOn("process-b").
		Build()

	if err != nil {
		log.Fatalf("Failed to build workflow: %v", err)
	}

	engine := NewEngine()

	// Example 1: Use processor A
	fmt.Println("--- Example 1: Using Processor A ---")
	result1, _ := engine.Execute(context.Background(), workflow,
		map[string]any{"use_processor_a": true})
	fmt.Printf("Completed steps: %d\n", len(result1.Steps))

	// Example 2: Use processor B
	fmt.Println("\n--- Example 2: Using Processor B ---")
	result2, _ := engine.Execute(context.Background(), workflow,
		map[string]any{"use_processor_a": false})
	fmt.Printf("Completed steps: %d\n", len(result2.Steps))
}

// CustomMappings demonstrates custom input/output mappings.
func CustomMappings() {
	workflow, err := NewWorkflowBuilder("custom-mappings").
		AddStep("transform", mockAgent("transformer"),
			WithInput(func(ctx *WorkflowContext) (string, error) {
				// Extract and transform input data
				data := ctx.Get("raw_data").(string)
				prefix := ctx.Get("prefix").(string)
				return fmt.Sprintf("%s: %s", prefix, data), nil
			}),
			WithOutput(func(result *agent.AgentResult, ctx *WorkflowContext) error {
				// Store multiple outputs
				ctx.Set("transformed_output", result.FinalText())
				ctx.Set("output_length", len(result.FinalText()))
				ctx.Set("timestamp", time.Now().Format(time.RFC3339))
				return nil
			}),
		).
		Build()

	if err != nil {
		log.Fatalf("Failed to build workflow: %v", err)
	}

	engine := NewEngine()
	result, err := engine.Execute(context.Background(), workflow,
		map[string]any{
			"raw_data": "sample input data",
			"prefix":  "TRANSFORMED",
		})

	if err != nil {
		log.Fatalf("Execution failed: %v", err)
	}

	fmt.Printf("Transformed output: %s\n", result.Output["transformed_output"])
	fmt.Printf("Output length: %d\n", result.Output["output_length"])
}

// DiamondPattern demonstrates a diamond-shaped DAG.
func DiamondPattern() {
	// Diamond pattern:
	//   start
	//   /   \
	// left  right (parallel)
	//   \   /
	//    end
	workflow, err := NewWorkflowBuilder("diamond").
		AddStep("start", mockAgent("starter")).
		AddStep("left", mockAgent("left-processor")).
		DependsOn("start").
		AddStep("right", mockAgent("right-processor")).
		DependsOn("start").
		AddStep("end", mockAgent("finalizer")).
		DependsOn("left").
		DependsOn("right").
		Build()

	if err != nil {
		log.Fatalf("Failed to build diamond workflow: %v", err)
	}

	// Verify topological order
	order, _ := workflow.GetTopologicalOrder()
	fmt.Printf("Execution order: %v\n", order)

	// Verify independent steps grouping
	levels, _ := workflow.GetIndependentSteps()
	for level, steps := range levels {
		fmt.Printf("Level %d (parallel): %v\n", level, steps)
	}

	// Execute workflow
	engine := NewEngine()
	result, err := engine.Execute(context.Background(), workflow,
		map[string]any{"input": "diamond pattern test"})

	if err != nil {
		log.Fatalf("Execution failed: %v", err)
	}

	fmt.Printf("Diamond workflow completed in %v\n", result.Duration)
}

// ComplexWorkflow demonstrates a more complex workflow with multiple patterns.
func ComplexWorkflow() {
	// Create agents for different roles
	routerAgent := mockAgent("router")
	processorAgents := []*agent.Agent{
		mockAgent("worker-1"),
		mockAgent("worker-2"),
		mockAgent("worker-3"),
	}
	aggregatorAgent := mockAgent("aggregator")
	validatorAgent := mockAgent("validator")

	workflow, err := NewWorkflowBuilder("complex-workflow").
		AddStep("route", routerAgent,
			WithInput(func(ctx *WorkflowContext) (string, error) {
				topic := ctx.Get("topic").(string)
				return fmt.Sprintf("Route task: %s", topic), nil
			}),
		).
		AddStep("worker-1", processorAgents[0]).
		DependsOn("route").
		AddStep("worker-2", processorAgents[1]).
		DependsOn("route").
		AddStep("worker-3", processorAgents[2]).
		DependsOn("route").
		AddStep("aggregate", aggregatorAgent,
			WithInput(func(ctx *WorkflowContext) (string, error) {
				// Collect outputs from all workers
				var outputs string
				for i := 1; i <= 3; i++ {
					stepID := fmt.Sprintf("worker-%d", i)
					if result, err := ctx.GetStepOutput(stepID); err == nil {
						outputs += fmt.Sprintf("\nWorker %d:\n%s", i, result.FinalText())
					}
				}
				return fmt.Sprintf("Aggregate these results:%s", outputs), nil
			}),
		).
		DependsOn("worker-1").
		DependsOn("worker-2").
		DependsOn("worker-3").
		AddStep("validate", validatorAgent,
			WithInput(func(ctx *WorkflowContext) (string, error) {
				aggregateOutput, _ := ctx.GetStepOutput("aggregate")
				return fmt.Sprintf("Validate: %s", aggregateOutput.FinalText()), nil
			}),
		).
		DependsOn("aggregate").
		WithOutput(func(result *agent.AgentResult, ctx *WorkflowContext) error {
			ctx.Set("final_result", result.FinalText())
			return nil
		}).
		Build()

	if err != nil {
		log.Fatalf("Failed to build complex workflow: %v", err)
	}

	// Show independent steps (workers should be in same level)
	levels, _ := workflow.GetIndependentSteps()
	fmt.Println("Execution levels:")
	for level, steps := range levels {
		fmt.Printf("  Level %d: %v\n", level, steps)
	}

	// Execute workflow
	engine := NewEngine()
	result, err := engine.Execute(context.Background(), workflow,
		map[string]any{"topic": "complex multi-agent task"})

	if err != nil {
		log.Fatalf("Execution failed: %v", err)
	}

	fmt.Printf("Complex workflow completed with status: %s\n", result.Status)
	fmt.Printf("Total steps: %d\n", len(result.Steps))
}

// Metadata demonstrates workflow and step metadata.
func Metadata() {
	workflow, err := NewWorkflowBuilder("metadata-example").
		WithMetadata("version", "1.0").
		WithMetadata("author", "orchestration-team").
		WithMetadata("priority", "high").
		AddStep("step-1", mockAgent("worker"),
			WithStepMetadata("estimated-duration", "30s"),
			WithStepMetadata("requires-resource", "GPU"),
		).
		AddStep("step-2", mockAgent("worker"),
			WithStepMetadata("estimated-duration", "60s"),
			WithStepMetadata("requires-resource", "CPU"),
		).
		Build()

	if err != nil {
		log.Fatalf("Failed to build workflow: %v", err)
	}

	// Access workflow metadata
	if version, ok := workflow.GetMetadata("version"); ok {
		fmt.Printf("Workflow version: %v\n", version)
	}

	if priority, ok := workflow.GetMetadata("priority"); ok {
		fmt.Printf("Workflow priority: %v\n", priority)
	}

	// Access step metadata
	for _, step := range workflow.GetAllSteps() {
		if duration, ok := step.GetMetadata("estimated-duration"); ok {
			fmt.Printf("Step %s estimated duration: %v\n", step.ID, duration)
		}
	}

	// Execute workflow
	engine := NewEngine()
	result, err := engine.Execute(context.Background(), workflow,
		map[string]any{"input": "metadata test"})

	if err != nil {
		log.Printf("Execution failed: %v", err)
	} else {
		fmt.Printf("Workflow completed successfully in %v\n", result.Duration)
	}
}

// ErrorHandling demonstrates workflow error handling.
func ErrorHandling() {
	// Create an agent that might fail (in practice, configure mock to fail)
	failingAgent := mockAgent("failing-agent")

	workflow, err := NewWorkflowBuilder("error-handling").
		AddStep("step-1", mockAgent("stable-agent")).
		AddStep("step-2", failingAgent,
			WithRetry(&RetryPolicy{
				MaxAttempts:  3,
				InitialDelay: 1 * time.Second,
				MaxDelay:     5 * time.Second,
				Multiplier:   2.0,
				Jitter:       true,
			}),
		).
		DependsOn("step-1").
		AddStep("step-3", mockAgent("stable-agent")).
		DependsOn("step-2").
		Build()

	if err != nil {
		log.Fatalf("Failed to build workflow: %v", err)
	}

	engine := NewEngine()
	result, err := engine.Execute(context.Background(), workflow,
		map[string]any{"input": "error handling test"})

	if err != nil {
		fmt.Printf("Workflow failed: %v\n", err)
	} else {
		fmt.Printf("Workflow succeeded\n")
	}

	// Check individual step results
	for stepID, stepResult := range result.Steps {
		if stepResult.Error != nil {
			fmt.Printf("Step %s failed after %d attempts: %v\n",
				stepID, stepResult.Attempts, stepResult.Error)
		} else {
			fmt.Printf("Step %s succeeded after %d attempt(s)\n",
				stepID, stepResult.Attempts)
		}
	}
}
