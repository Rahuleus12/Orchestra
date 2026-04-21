package orchestration

import (
	"fmt"
	"time"

	"github.com/user/orchestra/internal/agent"
)

// WorkflowBuilder provides a fluent API for constructing workflows.
type WorkflowBuilder struct {
	workflow   *Workflow
	currentStep *Step
	errors     []error
}

// NewWorkflowBuilder creates a new workflow builder with the given name.
func NewWorkflowBuilder(name string) *WorkflowBuilder {
	return &WorkflowBuilder{
		workflow: NewWorkflow(name),
		errors:   make([]error, 0),
	}
}

// AddStep adds a step to the workflow and applies the given options.
// Returns the builder for method chaining.
func (wb *WorkflowBuilder) AddStep(id string, agent *agent.Agent, opts ...StepOption) *WorkflowBuilder {
	step := NewStep(id, agent)

	// Apply step options
	for _, opt := range opts {
		opt(step)
	}

	// Set as current step for dependency tracking
	wb.currentStep = step

	// Add to workflow
	if err := wb.workflow.AddStep(step); err != nil {
		wb.errors = append(wb.errors, err)
	}

	return wb
}

// DependsOn adds a dependency from the current step to the step with the given ID.
// This can only be called after AddStep.
func (wb *WorkflowBuilder) DependsOn(stepID string) *WorkflowBuilder {
	if wb.currentStep == nil {
		wb.errors = append(wb.errors, fmt.Errorf("no current step to add dependency to"))
		return wb
	}

	// Add edge from dependency to current step
	if err := wb.workflow.AddEdge(stepID, wb.currentStep.ID, nil); err != nil {
		wb.errors = append(wb.errors, err)
	}

	return wb
}

// DependsOnConditional adds a conditional dependency from the current step to the step with the given ID.
// This can only be called after AddStep.
func (wb *WorkflowBuilder) DependsOnConditional(stepID string, condition Condition) *WorkflowBuilder {
	if wb.currentStep == nil {
		wb.errors = append(wb.errors, fmt.Errorf("no current step to add dependency to"))
		return wb
	}

	// Add edge with condition
	if err := wb.workflow.AddEdge(stepID, wb.currentStep.ID, condition); err != nil {
		wb.errors = append(wb.errors, err)
	}

	return wb
}

// WithInput sets the workflow-level input mapping.
func (wb *WorkflowBuilder) WithInput(mapping InputMapping) *WorkflowBuilder {
	wb.workflow.SetInputMapping(mapping)
	return wb
}

// WithOutput sets the workflow-level output mapping.
func (wb *WorkflowBuilder) WithOutput(mapping OutputMapping) *WorkflowBuilder {
	wb.workflow.SetOutputMapping(mapping)
	return wb
}

// WithMetadata sets a metadata value for the workflow.
func (wb *WorkflowBuilder) WithMetadata(key string, value any) *WorkflowBuilder {
	wb.workflow.SetMetadata(key, value)
	return wb
}

// Build finalizes the workflow and returns it, validating it first.
// Returns an error if the workflow is invalid or if any errors occurred during building.
func (wb *WorkflowBuilder) Build() (*Workflow, error) {
	// Collect any errors
	if len(wb.errors) > 0 {
		return nil, fmt.Errorf("workflow build errors: %v", wb.errors)
	}

	// Validate workflow
	if err := wb.workflow.Validate(); err != nil {
		return nil, fmt.Errorf("workflow validation failed: %w", err)
	}

	return wb.workflow, nil
}

// StepOption configures a step during creation.
type StepOption func(*Step)

// WithInput sets the input mapping for a step.
func WithInput(mapping InputMapping) StepOption {
	return func(s *Step) {
		s.InputMap = mapping
	}
}

// WithOutput sets the output mapping for a step.
func WithOutput(mapping OutputMapping) StepOption {
	return func(s *Step) {
		s.OutputMap = mapping
	}
}

// WithCondition sets the execution condition for a step.
func WithCondition(condition Condition) StepOption {
	return func(s *Step) {
		s.Condition = condition
	}
}

// WithRetry sets the retry policy for a step.
func WithRetry(policy *RetryPolicy) StepOption {
	return func(s *Step) {
		s.RetryPolicy = policy
	}
}

// WithTimeout sets the timeout for a step.
func WithTimeout(dur time.Duration) StepOption {
	return func(s *Step) {
		s.Timeout = dur
	}
}

// WithStepMetadata sets a metadata value for a step.
func WithStepMetadata(key string, value any) StepOption {
	return func(s *Step) {
		s.SetMetadata(key, value)
	}
}

// DependsOnStep returns a StepOption that adds a dependency from this step to another step.
// This is an alternative to calling DependsOn() on the builder after AddStep.
func DependsOnStep(stepID string) StepOption {
	return func(s *Step) {
		// This is a placeholder - the actual edge must be added through the builder
		// The builder will need to handle this specially
	}
}

// Sequence creates a sequential pipeline from a list of agents.
// This is a convenience function for creating a linear workflow.
func Sequence(name string, agents []*agent.Agent) (*Workflow, error) {
	if len(agents) == 0 {
		return nil, fmt.Errorf("sequence must have at least one agent")
	}

	builder := NewWorkflowBuilder(name)
	var prevStepID string

	for i, agent := range agents {
		stepID := fmt.Sprintf("step-%d", i)
		builder.AddStep(stepID, agent)

		if i > 0 {
			builder.DependsOn(prevStepID)
		}

		prevStepID = stepID
	}

	return builder.Build()
}

// Parallel creates a workflow where multiple agents run in parallel.
// All agents receive the same input, and outputs are aggregated.
func Parallel(name string, agents []*agent.Agent, aggregator AggregatorFunc) (*Workflow, error) {
	if len(agents) == 0 {
		return nil, fmt.Errorf("parallel workflow must have at least one agent")
	}

	builder := NewWorkflowBuilder(name)

	// Add parallel steps
	for i, agent := range agents {
		stepID := fmt.Sprintf("parallel-%d", i)
		builder.AddStep(stepID, agent,
			WithInput(func(ctx *WorkflowContext) (string, error) {
				// All parallel steps get the same input
				if topic, ok := ctx.Get("topic").(string); ok {
					return topic, nil
				}
				return "", fmt.Errorf("no 'topic' in workflow input")
			}),
		)
	}

	// Add aggregator step
	aggregatorID := "aggregator"
	builder.AddStep(aggregatorID, agents[0], // Use first agent as placeholder
		WithInput(func(ctx *WorkflowContext) (string, error) {
			// Collect all parallel step outputs
			results := make([]*agent.AgentResult, 0, len(agents))
			for i := range agents {
				stepID := fmt.Sprintf("parallel-%d", i)
				if result, err := ctx.GetStepOutput(stepID); err == nil {
					results = append(results, result)
				}
			}

			// Call aggregator
			if aggregator != nil {
				return aggregator(results)
			}

			// Default aggregator: concatenate all outputs
			var combined string
			for _, result := range results {
				combined += result.FinalText() + "\n"
			}
			return combined, nil
		}),
	)

	// Make aggregator depend on all parallel steps
	for i := range agents {
		stepID := fmt.Sprintf("parallel-%d", i)
		builder.DependsOn(stepID)
	}

	return builder.Build()
}

// AggregatorFunc aggregates multiple agent results into a single input string.
type AggregatorFunc func(results []*agent.AgentResult) (string, error)

// ConcatAggregator concatenates all agent outputs.
func ConcatAggregator(results []*agent.AgentResult) (string, error) {
	var combined string
	for _, result := range results {
		combined += result.FinalText() + "\n"
	}
	return combined, nil
}

// FirstAggregator returns the output of the first agent.
func FirstAggregator(results []*agent.AgentResult) (string, error) {
	if len(results) == 0 {
		return "", fmt.Errorf("no results to aggregate")
	}
	return results[0].FinalText(), nil
}
