package orchestration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/user/orchestra/internal/agent"
	"github.com/user/orchestra/internal/provider"
)

// FailureStrategy defines how the engine handles step failures.
type FailureStrategy string

const (
	// FailFast stops the entire workflow when any step fails.
	FailFast FailureStrategy = "fail_fast"
	// ContinueOnError continues executing other steps even if one fails.
	ContinueOnError FailureStrategy = "continue_on_error"
	// Fallback executes a fallback step or function when the main step fails.
	Fallback FailureStrategy = "fallback"
)

// Engine executes workflows with parallel execution and failure handling.
type Engine struct {
	logger *slog.Logger
	tracer interface{} // trace.Tracer (placeholder for OpenTelemetry)
	meter  interface{} // metric.Meter (placeholder for OpenTelemetry)
}

// EngineOption configures an Engine instance.
type EngineOption func(*Engine)

// WithLogger sets the logger for the engine.
func WithLogger(logger *slog.Logger) EngineOption {
	return func(e *Engine) {
		e.logger = logger
	}
}

// NewEngine creates a new workflow engine with the given options.
func NewEngine(opts ...EngineOption) *Engine {
	engine := &Engine{
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(engine)
	}

	return engine
}

// Execute runs a workflow synchronously with the given input and returns the result.
func (e *Engine) Execute(ctx context.Context, workflow *Workflow, input map[string]any) (*WorkflowResult, error) {
	// Create workflow context
	wfCtx := NewWorkflowContext(ctx, input)

	// Initialize result
	result := &WorkflowResult{
		ID:        workflow.ID(),
		Name:      workflow.Name(),
		Status:    StatusPending,
		Steps:     make(map[string]*StepResult),
		Output:    make(map[string]any),
		Usage:     provider.TokenUsage{},
		Metadata:  make(map[string]any),
		StartTime: time.Now(),
	}

	// Log start
	e.logger.Info("Starting workflow execution",
		"workflow_id", result.ID,
		"workflow_name", result.Name,
		"input_keys", inputKeys(input),
	)

	// Get independent steps (levels for parallel execution)
	levels, err := workflow.GetIndependentSteps()
	if err != nil {
		result.Status = StatusFailed
		result.Error = fmt.Errorf("failed to get execution order: %w", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		e.logger.Error("Workflow execution failed",
			"workflow_id", result.ID,
			"error", result.Error,
		)
		return result, result.Error
	}

	// Set status to running
	result.Status = StatusRunning

	// Execute each level sequentially
	var lastError error
	for levelIdx, levelSteps := range levels {
		e.logger.Debug("Executing workflow level",
			"workflow_id", result.ID,
			"level", levelIdx,
			"steps", levelSteps,
		)

		// Execute steps in this level in parallel
		levelResults := e.executeLevel(ctx, wfCtx, workflow, result, levelSteps)

		// Check for errors and apply failure strategy
		for _, stepResult := range levelResults {
			result.Steps[stepResult.StepID] = stepResult

			if stepResult.Error != nil {
				lastError = stepResult.Error
				e.logger.Error("Step failed",
					"workflow_id", result.ID,
					"step_id", stepResult.StepID,
					"error", stepResult.Error,
					"attempts", stepResult.Attempts,
				)

				// For now, implement fail-fast strategy
				// TODO: Implement continue-on-error and fallback strategies
				break
			}
		}

		// If any step failed with fail-fast, stop execution
		if lastError != nil {
			break
		}
	}

	// Apply workflow output mapping
	if lastError == nil {
		if workflow.GetOutputMapping() != nil {
			// Create a synthetic agent result for the final output
			// This is a simplified approach - in practice, you might want to aggregate step outputs
			var finalResult *agent.AgentResult
			if len(result.Steps) > 0 {
				// Use the last step's result as a basis
				for _, stepID := range levels[len(levels)-1] {
					if sr, ok := result.Steps[stepID]; ok && sr.Result != nil {
						finalResult = sr.Result
						break
					}
				}
			}

			if finalResult != nil {
				if err := workflow.GetOutputMapping()(finalResult, wfCtx); err != nil {
					e.logger.Warn("Failed to apply output mapping",
						"workflow_id", result.ID,
						"error", err,
					)
				}
			}
		}

		// Collect final output from context
		if output, ok := wfCtx.GetOutput("output"); ok {
			result.Output["output"] = output
		}
	}

	// Finalize result
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	// Aggregate token usage
	for _, stepResult := range result.Steps {
		if stepResult.Result != nil {
			result.Usage.PromptTokens += stepResult.Result.Usage.PromptTokens
			result.Usage.CompletionTokens += stepResult.Result.Usage.CompletionTokens
			result.Usage.TotalTokens += stepResult.Result.Usage.TotalTokens
		}
	}

	// Set final status
	if lastError != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			result.Status = StatusCancelled
			result.Error = ctx.Err()
		} else {
			result.Status = StatusFailed
			result.Error = lastError
		}
	} else {
		result.Status = StatusCompleted
	}

	// Log completion
	e.logger.Info("Workflow execution completed",
		"workflow_id", result.ID,
		"status", result.Status,
		"duration", result.Duration,
		"total_tokens", result.Usage.TotalTokens,
	)

	return result, result.Error
}

// executeLevel executes all steps in a level in parallel.
func (e *Engine) executeLevel(ctx context.Context, wfCtx *WorkflowContext, workflow *Workflow, result *WorkflowResult, stepIDs []string) []*StepResult {
	var wg sync.WaitGroup
	results := make([]*StepResult, len(stepIDs))
	resultMu := sync.Mutex{}

	for i, stepID := range stepIDs {
		wg.Add(1)
		go func(idx int, id string) {
			defer wg.Done()

			stepResult := e.executeStep(ctx, wfCtx, workflow, id)

			resultMu.Lock()
			results[idx] = stepResult
			resultMu.Unlock()
		}(i, stepID)
	}

	wg.Wait()
	return results
}

// executeStep executes a single step with retry logic.
func (e *Engine) executeStep(ctx context.Context, wfCtx *WorkflowContext, workflow *Workflow, stepID string) *StepResult {
	step, err := workflow.GetStep(stepID)
	if err != nil {
		return &StepResult{
			StepID:    stepID,
			Status:    StatusFailed,
			Error:     err,
			Duration:  0,
			Attempts:  0,
			Timestamp: time.Now(),
		}
	}

	stepResult := &StepResult{
		StepID:    stepID,
		Status:    StatusPending,
		Duration:  0,
		Attempts:  0,
		Timestamp: time.Now(),
	}

	e.logger.Debug("Executing step",
		"workflow_id", workflow.ID(),
		"step_id", stepID,
		"agent", step.Agent.Name(),
	)

	// Check condition
	if step.Condition != nil && !step.Condition(wfCtx) {
		stepResult.Status = StatusCompleted
		e.logger.Debug("Step condition not met, skipping",
			"workflow_id", workflow.ID(),
			"step_id", stepID,
		)
		return stepResult
	}

	// Set current step in context
	wfCtx.SetCurrentStep(stepID)

	// Prepare input
	var input string
	if step.InputMap != nil {
		input, err = step.InputMap(wfCtx)
		if err != nil {
			stepResult.Status = StatusFailed
			stepResult.Error = fmt.Errorf("failed to map input: %w", err)
			stepResult.Duration = time.Since(stepResult.Timestamp)
			e.logger.Error("Failed to map input for step",
				"workflow_id", workflow.ID(),
				"step_id", stepID,
				"error", err,
			)
			return stepResult
		}
	}

	// Execute with retry logic
	lastError := error(nil)
	retryPolicy := step.RetryPolicy
	if retryPolicy == nil {
		retryPolicy = DefaultRetryPolicy()
	}

	for attempt := 1; attempt <= retryPolicy.MaxAttempts; attempt++ {
		stepResult.Attempts = attempt

		// Create timeout context if specified
		execCtx := ctx
		if step.Timeout > 0 {
			var cancel context.CancelFunc
			execCtx, cancel = context.WithTimeout(ctx, step.Timeout)
			defer cancel()
		}

		// Execute agent
		startTime := time.Now()
		var agentResult *agent.AgentResult

		// Check if agent has Run method (single turn) or RunConversation (multi-turn)
		if input != "" {
			// Run the agent with the input
			agentResult, err = step.Agent.Run(execCtx, input)
		} else {
			// Run the agent without input (for agents with internal state or memory)
			err = fmt.Errorf("no input provided for step %s", stepID)
		}

		duration := time.Since(startTime)

		if err == nil {
			// Success
			stepResult.Result = agentResult
			stepResult.Duration = duration
			stepResult.Status = StatusCompleted

			// Store step output in context
			if step.OutputMap != nil {
				if err := step.OutputMap(agentResult, wfCtx); err != nil {
					e.logger.Warn("Failed to apply output mapping for step",
						"workflow_id", workflow.ID(),
						"step_id", stepID,
						"error", err,
					)
					// Don't fail the step for output mapping errors
				}
			}

			wfCtx.SetStepOutput(stepID, agentResult)

			e.logger.Debug("Step completed successfully",
				"workflow_id", workflow.ID(),
				"step_id", stepID,
				"attempt", attempt,
				"duration", duration,
				"tokens", agentResult.Usage.TotalTokens,
			)

			return stepResult
		}

		// Failure
		lastError = err
		e.logger.Warn("Step attempt failed",
			"workflow_id", workflow.ID(),
			"step_id", stepID,
			"attempt", attempt,
			"max_attempts", retryPolicy.MaxAttempts,
			"error", err,
		)

		// Check if we should retry
		if attempt < retryPolicy.MaxAttempts && !errors.Is(err, context.Canceled) {
			delay := retryPolicy.ComputeDelay(attempt)
			e.logger.Debug("Retrying step after delay",
				"workflow_id", workflow.ID(),
				"step_id", stepID,
				"attempt", attempt,
				"delay", delay,
			)

			select {
			case <-time.After(delay):
				// Continue to retry
			case <-ctx.Done():
				// Context cancelled during retry wait
				lastError = ctx.Err()
				break
			}
		}
	}

	// All attempts failed
	stepResult.Status = StatusFailed
	stepResult.Error = lastError
	stepResult.Duration = time.Since(stepResult.Timestamp)

	e.logger.Error("Step failed after all attempts",
		"workflow_id", workflow.ID(),
		"step_id", stepID,
		"attempts", stepResult.Attempts,
		"error", lastError,
	)

	return stepResult
}

// Stream runs a workflow and streams events through a channel.
func (e *Engine) Stream(ctx context.Context, workflow *Workflow, input map[string]any) (<-chan WorkflowEvent, error) {
	eventChan := make(chan WorkflowEvent, 100)

	go func() {
		defer close(eventChan)

		// Send started event
		eventChan <- WorkflowEvent{
			Type:      WorkflowEventStarted,
			Timestamp: time.Now(),
		}

		// Execute workflow
		_, err := e.Execute(ctx, workflow, input)

		// Send completion event
		if err != nil {
			eventType := WorkflowEventFailed
			if errors.Is(err, context.Canceled) {
				eventType = WorkflowEventCancelled
			}
			eventChan <- WorkflowEvent{
				Type:      eventType,
				Error:     err,
				Timestamp: time.Now(),
			}
		} else {
			eventChan <- WorkflowEvent{
				Type:      WorkflowEventCompleted,
				Timestamp: time.Now(),
			}
		}
	}()

	return eventChan, nil
}

// inputKeys returns the keys from an input map for logging.
func inputKeys(input map[string]any) []string {
	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	return keys
}
