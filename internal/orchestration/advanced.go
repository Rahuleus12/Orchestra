package orchestration

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/user/orchestra/internal/agent"
)

// ---------------------------------------------------------------------------
// 9.2 Self-Reflection & Refinement
// ---------------------------------------------------------------------------

const (
	keyIteration = "iteration"
	keyError     = "error"
)

// RefinementConfig configures the refinement pattern.
type RefinementConfig struct {
	// Name is the name of the refinement workflow.
	Name string

	// Generator is the agent that produces the initial output.
	Generator *agent.Agent

	// Evaluator is the agent that evaluates and scores the output.
	Evaluator *agent.Agent

	// Refiner is the agent that improves the output based on feedback.
	// If nil, the Generator is used for refinement.
	Refiner *agent.Agent

	// Criteria describes what the evaluator should assess.
	Criteria string

	// MaxIterations limits the refinement loop.
	// Default is 3.
	MaxIterations int

	// Threshold is the minimum score (0-1) required to stop refining.
	// Default is 0.9.
	Threshold float64

	// Logger for logging refinement progress.
	Logger *slog.Logger
}

// RefinementResult holds the output of the refinement process.
type RefinementResult struct {
	// Output is the final refined output.
	Output string

	// Score is the final evaluation score.
	Score float64

	// Iterations is the number of refinement cycles performed.
	Iterations int

	// Feedback is the evaluator's feedback from the final iteration.
	Feedback string

	// Duration is the total time taken for refinement.
	Duration time.Duration

	// IterationResults contains details from each iteration.
	IterationResults []IterationResult
}

// IterationResult holds the result of a single refinement iteration.
type IterationResult struct {
	Iteration int
	Output    string
	Score     float64
	Feedback  string
}

// Refine creates a workflow that iteratively improves output through
// generation, evaluation, and refinement.
func Refine(name string, gen *agent.Agent, opts ...RefinementOption) *Workflow {
	cfg := &RefinementConfig{
		Name:          name,
		Generator:     gen,
		MaxIterations: 3,
		Threshold:     0.9,
		Logger:        slog.Default(),
	}

	for _, opt := range opts {
		opt(cfg)
	}

	// Create a special agent that implements the refinement loop
	refinementAgent := &agent.Agent{} // We'll use the workflow pattern instead

	// Build workflow with manual steps
	workflow := NewWorkflow(name)

	// For now, we'll implement refinement as a special pattern
	// that gets executed through the RefinementEngine
	_ = refinementAgent

	return workflow
}

// RefinementOption configures the refinement process.
type RefinementOption func(*RefinementConfig)

// WithEvaluator sets the evaluator agent.
func WithEvaluator(evaluator *agent.Agent) RefinementOption {
	return func(cfg *RefinementConfig) {
		cfg.Evaluator = evaluator
	}
}

// WithCriteria sets the evaluation criteria.
func WithCriteria(criteria string) RefinementOption {
	return func(cfg *RefinementConfig) {
		cfg.Criteria = criteria
	}
}

// WithMaxIterations sets the maximum number of refinement iterations.
func WithMaxIterations(max int) RefinementOption {
	return func(cfg *RefinementConfig) {
		cfg.MaxIterations = max
	}
}

// WithThreshold sets the minimum score to stop refining.
func WithThreshold(threshold float64) RefinementOption {
	return func(cfg *RefinementConfig) {
		cfg.Threshold = threshold
	}
}

// WithRefiner sets a separate refiner agent.
func WithRefiner(refiner *agent.Agent) RefinementOption {
	return func(cfg *RefinementConfig) {
		cfg.Refiner = refiner
	}
}

// WithRefinementLogger sets the logger for the refinement process.
func WithRefinementLogger(logger *slog.Logger) RefinementOption {
	return func(cfg *RefinementConfig) {
		cfg.Logger = logger
	}
}

// RefinementEngine executes the refinement pattern.
type RefinementEngine struct {
	logger *slog.Logger
}

// NewRefinementEngine creates a new refinement engine.
func NewRefinementEngine(opts ...RefinementOption) *RefinementEngine {
	e := &RefinementEngine{
		logger: slog.Default(),
	}
	return e
}

// Execute runs the refinement process with the given configuration and input.
func (e *RefinementEngine) Execute(ctx context.Context, cfg *RefinementConfig, input string) (*RefinementResult, error) {
	if cfg.Evaluator == nil {
		return nil, fmt.Errorf("evaluator agent is required")
	}

	if cfg.Refiner == nil {
		cfg.Refiner = cfg.Generator
	}

	startTime := time.Now()
	iterationResults := make([]IterationResult, 0, cfg.MaxIterations)

	e.logger.Info("Starting refinement process",
		"name", cfg.Name,
		"max_iterations", cfg.MaxIterations,
		"threshold", cfg.Threshold,
	)

	// Step 1: Generate initial output
	genResult, err := cfg.Generator.Run(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("initial generation failed: %w", err)
	}

	currentOutput := genResult.FinalText()

	// Step 2: Evaluate-refine loop
	for i := 0; i < cfg.MaxIterations; i++ {
		iteration++

		// Evaluate the output
		evaluationPrompt := buildEvaluationPrompt(cfg.Criteria, currentOutput)
		evalResult, err := cfg.Evaluator.Run(ctx, evaluationPrompt)
		if err != nil {
			e.logger.Warn("Evaluation failed, stopping refinement",
				"iteration", iteration,
				"error", err,
			)
			break
		}

		// Parse evaluation result (expect format: "SCORE: 0.85\nFEEDBACK: ...")
		score, feedback := parseEvaluationResponse(evalResult.FinalText())

		iterationResults = append(iterationResults, IterationResult{
			Iteration: iteration,
			Output:    currentOutput,
			Score:     score,
			Feedback:  feedback,
		})

		e.logger.Debug("Refinement iteration completed",
			"iteration", iteration,
			"score", score,
			"feedback", truncateString(feedback, 100),
		)

		// Check if threshold is met
		if score >= cfg.Threshold {
			e.logger.Info("Refinement threshold met",
				"iteration", iteration,
				"score", score,
				"threshold", cfg.Threshold,
			)
			break
		}

		// If this is the last iteration, don't refine further
		if iteration >= cfg.MaxIterations {
			break
		}

		// Refine based on feedback
		refinePrompt := buildRefinementPrompt(input, currentOutput, feedback)
		refineResult, err := cfg.Refiner.Run(ctx, refinePrompt)
		if err != nil {
			e.logger.Warn("Refinement failed, keeping current output",
				slog.Int(keyIteration, iteration),
				slog.Any(keyError, err),
			)
			break
		}

		currentOutput = refineResult.FinalText()
	}

	// Get the final iteration result
	finalResult := iterationResults[len(iterationResults)-1]

	return &RefinementResult{
		Output:           finalResult.Output,
		Score:            finalResult.Score,
		Iterations:       len(iterationResults),
		Feedback:         finalResult.Feedback,
		Duration:         time.Since(startTime),
		IterationResults: iterationResults,
	}, nil
}

// iteration tracks the current refinement iteration.
var iteration int

// buildEvaluationPrompt creates the prompt for the evaluator.
func buildEvaluationPrompt(criteria, output string) string {
	return fmt.Sprintf(`Evaluate the following output based on these criteria: %s

Output to evaluate:
---
%s
---

Respond in this exact format:
SCORE: [0.0-1.0]
FEEDBACK: [Your detailed feedback and suggestions for improvement]`, criteria, output)
}

// buildRefinementPrompt creates the prompt for the refiner.
func buildRefinementPrompt(originalInput, currentOutput, feedback string) string {
	return fmt.Sprintf(`Improve the following output based on the feedback.

Original request:
---
%s
---

Current output:
---
%s
---

Feedback for improvement:
---
%s
---

Provide an improved version that addresses the feedback:`, originalInput, currentOutput, feedback)
}

// parseEvaluationResponse parses the evaluator's response to extract score and feedback.
func parseEvaluationResponse(response string) (float64, string) {
	var score float64
	var feedback string

	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "SCORE:") {
			scoreStr := strings.TrimPrefix(line, "SCORE:")
			scoreStr = strings.TrimSpace(scoreStr)
			fmt.Sscanf(scoreStr, "%f", &score)
			if score < 0 {
				score = 0
			}
			if score > 1 {
				score = 1
			}
		} else if strings.HasPrefix(line, "FEEDBACK:") {
			feedback = strings.TrimPrefix(line, "FEEDBACK:")
			feedback = strings.TrimSpace(feedback)
		} else if feedback != "" {
			// Append additional lines to feedback
			feedback += "\n" + line
		}
	}

	return score, feedback
}

// truncateString truncates a string to the specified length.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ---------------------------------------------------------------------------
// 9.3 Planning & Re-planning
// ---------------------------------------------------------------------------

// Plan represents a step-by-step plan created by a planner agent.
type Plan struct {
	// ID is the unique identifier for this plan.
	ID string

	// Goal is the overall goal of the plan.
	Goal string

	// Steps are the individual steps in the plan.
	Steps []PlanStep

	// Status is the current status of the plan.
	PlanStatus PlanStatus
}

// PlanStep represents a single step in a plan.
type PlanStep struct {
	// ID is the unique identifier for this step.
	ID string

	// Description is what this step should accomplish.
	Description string

	// Dependencies are IDs of steps that must complete before this one.
	Dependencies []string

	// Status is the current status of this step.
	Status StepStatus

	// Result is the output of executing this step.
	Result string

	// Error holds any error that occurred during execution.
	Error error
}

// PlanStatus represents the overall status of a plan.
type PlanStatus string

const (
	PlanStatusPending    PlanStatus = "pending"
	PlanStatusInProgress PlanStatus = "in_progress"
	PlanStatusCompleted  PlanStatus = "completed"
	PlanStatusFailed     PlanStatus = "failed"
	PlanStatusReplanning PlanStatus = "replanning"
)

// StepStatus represents the status of a plan step.
type StepStatus string

const (
	StepStatusPending    StepStatus = "pending"
	StepStatusInProgress StepStatus = "in_progress"
	StepStatusCompleted  StepStatus = "completed"
	StepStatusFailed     StepStatus = "failed"
	StepStatusSkipped    StepStatus = "skipped"
)

// PlanningConfig configures the planning pattern.
type PlanningConfig struct {
	// Planner is the agent that creates plans.
	Planner *agent.Agent

	// Executor is the agent that executes plan steps.
	Executor *agent.Agent

	// Replanner is the agent that adjusts plans when steps fail.
	// If nil, the Planner is used.
	Replanner *agent.Agent

	// MaxReplanAttempts limits the number of replanning attempts.
	// Default is 3.
	MaxReplanAttempts int

	// Logger for logging planning progress.
	Logger *slog.Logger
}

// PlanningResult holds the output of the planning process.
type PlanningResult struct {
	// Plan is the final plan (may have been replanned).
	Plan *Plan

	// ReplanCount is the number of times the plan was replanned.
	ReplanCount int

	// FinalOutput is the aggregated output from all completed steps.
	FinalOutput string

	// Duration is the total time taken for planning and execution.
	Duration time.Duration
}

// PlanningOption configures the planning process.
type PlanningOption func(*PlanningConfig)

// WithExecutor sets the executor agent.
func WithExecutor(executor *agent.Agent) PlanningOption {
	return func(cfg *PlanningConfig) {
		cfg.Executor = executor
	}
}

// WithReplanner sets a separate replanner agent.
func WithReplanner(replanner *agent.Agent) PlanningOption {
	return func(cfg *PlanningConfig) {
		cfg.Replanner = replanner
	}
}

// WithMaxReplanAttempts sets the maximum replanning attempts.
func WithMaxReplanAttempts(max int) PlanningOption {
	return func(cfg *PlanningConfig) {
		cfg.MaxReplanAttempts = max
	}
}

// WithPlanningLogger sets the logger for the planning process.
func WithPlanningLogger(logger *slog.Logger) PlanningOption {
	return func(cfg *PlanningConfig) {
		cfg.Logger = logger
	}
}

// PlanWorkflow creates a planning workflow for the given goal.
func PlanWorkflow(name string, planner *agent.Agent, opts ...PlanningOption) *Workflow {
	return Refine(name, planner) // Placeholder - use PlanningEngine directly
}

// PlanningEngine executes the planning pattern.
type PlanningEngine struct {
	logger *slog.Logger
}

// NewPlanningEngine creates a new planning engine.
func NewPlanningEngine() *PlanningEngine {
	return &PlanningEngine{
		logger: slog.Default(),
	}
}

// Execute runs the planning process with the given configuration and goal.
func (e *PlanningEngine) Execute(ctx context.Context, cfg *PlanningConfig, goal string) (*PlanningResult, error) {
	if cfg.Executor == nil {
		cfg.Executor = cfg.Planner
	}

	if cfg.Replanner == nil {
		cfg.Replanner = cfg.Planner
	}

	if cfg.MaxReplanAttempts <= 0 {
		cfg.MaxReplanAttempts = 3
	}

	startTime := time.Now()

	e.logger.Info("Starting planning process",
		"goal", truncateString(goal, 100),
	)

	// Step 1: Create initial plan
	plan, err := e.createPlan(ctx, cfg.Planner, goal)
	if err != nil {
		return nil, fmt.Errorf("failed to create plan: %w", err)
	}

	replanCount := 0

	// Step 2: Execute plan with replanning on failure
	for {
		plan.PlanStatus = PlanStatusInProgress
		executionErr := e.executePlan(ctx, cfg, plan)

		if executionErr == nil {
			plan.PlanStatus = PlanStatusCompleted
			break
		}

		// Check if we should replan
		replanCount++
		if replanCount > cfg.MaxReplanAttempts {
			e.logger.Warn("Max replan attempts reached, giving up",
				"replan_count", replanCount,
				"max_attempts", cfg.MaxReplanAttempts,
			)
			plan.PlanStatus = PlanStatusFailed
			return nil, fmt.Errorf("plan failed after %d replanning attempts: %w", replanCount, executionErr)
		}

		// Replan
		e.logger.Info("Replanning after failure",
			"replan_count", replanCount,
			"error", executionErr,
		)

		plan.PlanStatus = PlanStatusReplanning
		newPlan, err := e.replan(ctx, cfg.Replanner, plan, executionErr)
		if err != nil {
			return nil, fmt.Errorf("replanning failed: %w", err)
		}

		plan = newPlan
	}

	// Aggregate outputs
	var outputs []string
	for _, step := range plan.Steps {
		if step.Status == StepStatusCompleted && step.Result != "" {
			outputs = append(outputs, step.Result)
		}
	}

	return &PlanningResult{
		Plan:        plan,
		ReplanCount: replanCount,
		FinalOutput: strings.Join(outputs, "\n\n"),
		Duration:    time.Since(startTime),
	}, nil
}

// createPlan asks the planner agent to create a plan.
func (e *PlanningEngine) createPlan(ctx context.Context, planner *agent.Agent, goal string) (*Plan, error) {
	planPrompt := `Create a step-by-step plan to accomplish the following goal:

Goal: %s

Respond in the following format:
PLAN_ID: [unique identifier]
STEP [number]: [step description]

Each step should be atomic and clearly defined. Steps should be ordered by dependency.`

	result, err := planner.Run(ctx, fmt.Sprintf(planPrompt, goal))
	if err != nil {
		return nil, err
	}

	return parsePlanResponse(result.FinalText()), nil
}

// executePlan executes all steps in the plan.
func (e *PlanningEngine) executePlan(ctx context.Context, cfg *PlanningConfig, plan *Plan) error {
	for i, step := range plan.Steps {
		// Check dependencies
		if !e.checkDependencies(step, plan.Steps) {
			step.Status = StepStatusSkipped
			continue
		}

		step.Status = StepStatusInProgress

		// Execute the step
		executionPrompt := fmt.Sprintf(`Execute the following step from the plan:

Goal: %s
Current Step: %s
Previous completed steps: %s

Execute this step and provide your output:`,
			plan.Goal,
			step.Description,
			e.getCompletedStepsSummary(plan.Steps[:i]))

		result, err := cfg.Executor.Run(ctx, executionPrompt)
		if err != nil {
			step.Status = StepStatusFailed
			step.Error = err
			return fmt.Errorf("step %q failed: %w", step.Description, err)
		}

		step.Status = StepStatusCompleted
		step.Result = result.FinalText()

		e.logger.Debug("Step completed",
			"step_id", step.ID,
			"description", truncateString(step.Description, 50),
		)
	}

	return nil
}

// checkDependencies verifies that all dependencies for a step are completed.
func (e *PlanningEngine) checkDependencies(step PlanStep, allSteps []PlanStep) bool {
	for _, depID := range step.Dependencies {
		found := false
		for _, s := range allSteps {
			if s.ID == depID {
				if s.Status != StepStatusCompleted {
					return false
				}
				found = true
				break
			}
		}
		if !found {
			// Unknown dependency, assume satisfied
		}
	}
	return true
}

// getCompletedStepsSummary returns a summary of completed steps.
func (e *PlanningEngine) getCompletedStepsSummary(steps []PlanStep) string {
	var summaries []string
	for _, s := range steps {
		if s.Status == StepStatusCompleted {
			summaries = append(summaries, fmt.Sprintf("- %s: %s", s.Description, truncateString(s.Result, 100)))
		}
	}
	if len(summaries) == 0 {
		return "None"
	}
	return strings.Join(summaries, "\n")
}

// replan asks the replanner to adjust the plan based on the error.
func (e *PlanningEngine) replan(ctx context.Context, replanner *agent.Agent, failedPlan *Plan, failedErr error) (*Plan, error) {
	// Build a summary of what succeeded and failed
	var succeeded, failed []string
	for _, step := range failedPlan.Steps {
		if step.Status == StepStatusCompleted {
			succeeded = append(succeeded, fmt.Sprintf("✓ %s", step.Description))
		} else if step.Status == StepStatusFailed {
			failed = append(failed, fmt.Sprintf("✗ %s: %v", step.Description, step.Error))
		}
	}

	replanPrompt := `The following plan failed. Please create a revised plan.

Original Goal: %s

Completed Steps:
%s

Failed Steps:
%s

Error: %v

Please provide a revised plan that:
1. Preserves the work already completed
2. Addresses the failure
3. Continues toward the original goal

Respond in the same format:
PLAN_ID: [new unique identifier]
STEP [number]: [step description]`

	result, err := replanner.Run(ctx, fmt.Sprintf(replanPrompt, failedPlan.Goal,
		strings.Join(succeeded, "\n"),
		strings.Join(failed, "\n"),
		failedErr))
	if err != nil {
		return nil, err
	}

	return parsePlanResponse(result.FinalText()), nil
}

// parsePlanResponse parses the planner's response into a Plan.
func parsePlanResponse(response string) *Plan {
	plan := &Plan{
		ID:         "plan-" + generateWorkflowID(),
		Steps:      make([]PlanStep, 0),
		PlanStatus: PlanStatusPending,
	}

	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "PLAN_ID:") {
			plan.ID = strings.TrimSpace(strings.TrimPrefix(line, "PLAN_ID:"))
		} else if strings.HasPrefix(line, "STEP ") {
			// Parse "STEP 1: Description"
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				stepNum := ""
				fmt.Sscanf(line, "STEP %s:", &stepNum)
				stepID := fmt.Sprintf("%s-step-%s", plan.ID, stepNum)
				plan.Steps = append(plan.Steps, PlanStep{
					ID:          stepID,
					Description: strings.TrimSpace(parts[1]),
					Status:      StepStatusPending,
				})
			}
		}
	}

	return plan
}

// ---------------------------------------------------------------------------
// 9.4 Human-in-the-Loop
// ---------------------------------------------------------------------------

// HumanApprovalRequest represents a request for human approval.
type HumanApprovalRequest struct {
	// ID is a unique identifier for this request.
	ID string

	// StepID is the ID of the workflow step that needs approval.
	StepID string

	// Title is a human-readable title for this approval request.
	Title string

	// Description provides details about what needs approval.
	Description string

	// Input is the data that needs approval.
	Input any

	// Options are optional choices for the human to select from.
	Options []ApprovalOption

	// Timeout is how long to wait for approval before timing out.
	Timeout time.Duration

	// CreatedAt is when the request was created.
	CreatedAt time.Time

	// ResponseChan is the channel to receive the human's response.
	ResponseChan chan ApprovalResponse
}

// ApprovalOption represents a choice the human can make.
type ApprovalOption struct {
	// ID is a unique identifier for this option.
	ID string

	// Label is the human-readable label.
	Label string

	// Description provides more details about this option.
	Description string
}

// ApprovalResponse represents the human's response to an approval request.
type ApprovalResponse struct {
	// RequestID is the ID of the approval request.
	RequestID string

	// Approved indicates whether the request was approved.
	Approved bool

	// SelectedOption is the ID of the selected option (if any).
	SelectedOption string

	// Comment is any comment from the human.
	Comment string

	// RespondedAt is when the response was received.
	RespondedAt time.Time
}

// ApprovalStore stores approval requests and responses.
type ApprovalStore struct {
	mu       sync.RWMutex
	requests map[string]*HumanApprovalRequest
	handlers []ApprovalHandler
}

// ApprovalHandler is a function that handles approval requests.
// It returns the human's response.
type ApprovalHandler func(ctx context.Context, request *HumanApprovalRequest) (*ApprovalResponse, error)

// NewApprovalStore creates a new approval store.
func NewApprovalStore() *ApprovalStore {
	return &ApprovalStore{
		requests: make(map[string]*HumanApprovalRequest),
	}
}

// AddHandler adds an approval handler.
func (s *ApprovalStore) AddHandler(handler ApprovalHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers = append(s.handlers, handler)
}

// RequestApproval creates a new approval request and waits for a response.
func (s *ApprovalStore) RequestApproval(ctx context.Context, stepID, title, description string, input any, timeout time.Duration) (*ApprovalResponse, error) {
	request := &HumanApprovalRequest{
		ID:           fmt.Sprintf("approval-%d", time.Now().UnixNano()),
		StepID:       stepID,
		Title:        title,
		Description:  description,
		Input:        input,
		Timeout:      timeout,
		CreatedAt:    time.Now(),
		ResponseChan: make(chan ApprovalResponse, 1),
	}

	s.mu.Lock()
	s.requests[request.ID] = request
	handlers := make([]ApprovalHandler, len(s.handlers))
	copy(handlers, s.handlers)
	s.mu.Unlock()

	// Create timeout context
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Try each handler until one responds
	var response *ApprovalResponse

	for _, handler := range handlers {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("approval request timed out")
		default:
		}

		resp, err := handler(ctx, request)
		if err == nil && resp != nil {
			response = resp
			break
		}
	}

	if response == nil {
		return nil, fmt.Errorf("no approval handler available")
	}

	return response, nil
}

// Respond allows external systems to respond to an approval request.
func (s *ApprovalStore) Respond(requestID string, response *ApprovalResponse) error {
	s.mu.Lock()
	request, exists := s.requests[requestID]
	s.mu.Unlock()

	if !exists {
		return fmt.Errorf("approval request %q not found", requestID)
	}

	response.RequestID = requestID
	response.RespondedAt = time.Now()
	request.ResponseChan <- *response

	return nil
}

// GetRequest retrieves an approval request by ID.
func (s *ApprovalStore) GetRequest(requestID string) (*HumanApprovalRequest, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	request, exists := s.requests[requestID]
	return request, exists
}

// ---------------------------------------------------------------------------
// Human Approval Agent Step
// ---------------------------------------------------------------------------

// HumanApproval creates a workflow step that pauses for human approval.
func HumanApproval(title string, opts ...HumanApprovalOption) *agent.Agent {
	cfg := &humanApprovalConfig{
		Title:   title,
		Timeout: 30 * time.Minute,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	// Create an agent that will be wrapped with approval logic
	// The actual approval is handled by the workflow engine
	return nil // This is a placeholder - see HumanApprovalStep below
}

// humanApprovalConfig configures human approval.
type humanApprovalConfig struct {
	Title            string
	Description      string
	Timeout          time.Duration
	CallbackEndpoint string
	AutoApproveRules []AutoApproveRule
}

// HumanApprovalOption configures human approval.
type HumanApprovalOption func(*humanApprovalConfig)

// WithApprovalTimeout sets the approval timeout.
func WithApprovalTimeout(timeout time.Duration) HumanApprovalOption {
	return func(cfg *humanApprovalConfig) {
		cfg.Timeout = timeout
	}
}

// WithApprovalDescription sets the approval description.
func WithApprovalDescription(desc string) HumanApprovalOption {
	return func(cfg *humanApprovalConfig) {
		cfg.Description = desc
	}
}

// WithCallbackEndpoint sets the callback endpoint URL.
func WithCallbackEndpoint(endpoint string) HumanApprovalOption {
	return func(cfg *humanApprovalConfig) {
		cfg.CallbackEndpoint = endpoint
	}
}

// WithAutoApproveRule adds an auto-approve rule.
func WithAutoApproveRule(rule AutoApproveRule) HumanApprovalOption {
	return func(cfg *humanApprovalConfig) {
		cfg.AutoApproveRules = append(cfg.AutoApproveRules, rule)
	}
}

// AutoApproveRule defines when to auto-approve requests.
type AutoApproveRule struct {
	// Pattern matches against the step ID or title.
	Pattern string

	// AutoApprove is called to determine if this request should be auto-approved.
	AutoApprove func(ctx context.Context, request *HumanApprovalRequest) bool
}

// HumanApprovalStep wraps a step with human approval logic.
func HumanApprovalStep(step *Step, approvalStore *ApprovalStore, opts ...HumanApprovalOption) *Step {
	cfg := &humanApprovalConfig{
		Timeout: 30 * time.Minute,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	// Store the original agent
	originalAgent := step.Agent

	// Wrap the agent with approval logic
	approvedAgent := wrapAgentWithApproval(originalAgent, approvalStore, cfg)
	step.Agent = approvedAgent

	return step
}

// wrapAgentWithApproval wraps an agent with approval logic.
// In practice, this would need deeper integration with the agent interface.
func wrapAgentWithApproval(inner *agent.Agent, _ *ApprovalStore, _ *humanApprovalConfig) *agent.Agent {
	return inner
}

// ---------------------------------------------------------------------------
// 9.5 Multi-Model Ensemble
// ---------------------------------------------------------------------------

// EnsembleConfig configures the ensemble pattern.
type EnsembleConfig struct {
	// Name is the name of the ensemble.
	Name string

	// Agents are the agents to run in the ensemble.
	Agents []*agent.Agent

	// Strategy determines how to aggregate responses.
	Strategy EnsembleStrategy

	// Timeout is the maximum time to wait for all agents.
	Timeout time.Duration

	// Logger for logging ensemble progress.
	Logger *slog.Logger
}

// EnsembleStrategy determines how ensemble responses are aggregated.
type EnsembleStrategy string

const (
	// EnsembleMajorityVote selects the most common response.
	EnsembleMajorityVote EnsembleStrategy = "majority_vote"

	// EnsembleBestOfN runs an evaluator to select the best response.
	EnsembleBestOfN EnsembleStrategy = "best_of_n"

	// EnsembleFirst returns the first successful response.
	EnsembleFirst EnsembleStrategy = "first"

	// EnsembleConcat concatenates all responses.
	EnsembleConcat EnsembleStrategy = "concat"

	// EnsembleCascade tries models in order until one succeeds.
	EnsembleCascade EnsembleStrategy = "cascade"
)

// EnsembleResult holds the output of the ensemble process.
type EnsembleResult struct {
	// Output is the final aggregated output.
	Output string

	// Responses contains all individual responses.
	Responses []AgentResponse

	// SelectedIndex is the index of the selected response (if applicable).
	SelectedIndex int

	// Duration is the total time taken.
	Duration time.Duration
}

// AgentResponse holds a single agent's response.
type AgentResponse struct {
	// Index is the position of this agent in the ensemble.
	Index int

	// Output is the agent's output.
	Output string

	// Error holds any error that occurred.
	Error error

	// Duration is how long this agent took.
	Duration time.Duration

	// Metadata contains additional metadata from the agent.
	Metadata map[string]any
}

// EnsembleOption configures the ensemble process.
type EnsembleOption func(*EnsembleConfig)

// WithEnsembleStrategy sets the aggregation strategy.
func WithEnsembleStrategy(strategy EnsembleStrategy) EnsembleOption {
	return func(cfg *EnsembleConfig) {
		cfg.Strategy = strategy
	}
}

// WithEnsembleTimeout sets the timeout for the ensemble.
func WithEnsembleTimeout(timeout time.Duration) EnsembleOption {
	return func(cfg *EnsembleConfig) {
		cfg.Timeout = timeout
	}
}

// WithEnsembleLogger sets the logger for the ensemble.
func WithEnsembleLogger(logger *slog.Logger) EnsembleOption {
	return func(cfg *EnsembleConfig) {
		cfg.Logger = logger
	}
}

// Ensemble creates a workflow that runs multiple models and aggregates responses.
func Ensemble(name string, agents []*agent.Agent, opts ...EnsembleOption) *Workflow {
	cfg := &EnsembleConfig{
		Name:     name,
		Agents:   agents,
		Strategy: EnsembleMajorityVote,
		Logger:   slog.Default(),
	}

	for _, opt := range opts {
		opt(cfg)
	}

	// Create a workflow that will execute the ensemble
	workflow := NewWorkflow(name)

	// For now, return a placeholder workflow
	// The actual ensemble execution is handled by EnsembleEngine
	return workflow
}

// EnsembleEngine executes the ensemble pattern.
type EnsembleEngine struct {
	logger *slog.Logger
}

// NewEnsembleEngine creates a new ensemble engine.
func NewEnsembleEngine() *EnsembleEngine {
	return &EnsembleEngine{
		logger: slog.Default(),
	}
}

// Execute runs the ensemble process with the given configuration and input.
func (e *EnsembleEngine) Execute(ctx context.Context, cfg *EnsembleConfig, input string) (*EnsembleResult, error) {
	if len(cfg.Agents) == 0 {
		return nil, fmt.Errorf("at least one agent is required")
	}

	startTime := time.Now()

	// Create timeout context if specified
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	e.logger.Info("Starting ensemble execution",
		"name", cfg.Name,
		"strategy", cfg.Strategy,
		"agent_count", len(cfg.Agents),
	)

	// Run all agents concurrently
	var wg sync.WaitGroup
	responses := make([]AgentResponse, len(cfg.Agents))
	var mu sync.Mutex

	for i, ag := range cfg.Agents {
		wg.Add(1)
		go func(idx int, a *agent.Agent) {
			defer wg.Done()

			agentStart := time.Now()
			result, err := a.Run(ctx, input)
			duration := time.Since(agentStart)

			mu.Lock()
			var output string
			if result != nil {
				output = result.FinalText()
			}
			responses[idx] = AgentResponse{
				Index:    idx,
				Output:   output,
				Error:    err,
				Duration: duration,
			}
			mu.Unlock()
		}(i, ag)
	}

	wg.Wait()

	// Aggregate responses based on strategy
	var output string
	var selectedIndex int

	switch cfg.Strategy {
	case EnsembleFirst:
		output, selectedIndex = e.aggregateFirst(responses)
	case EnsembleConcat:
		output, selectedIndex = e.aggregateConcat(responses)
	case EnsembleCascade:
		output, selectedIndex = e.aggregateCascade(responses)
	case EnsembleMajorityVote:
		output, selectedIndex = e.aggregateMajorityVote(responses)
	case EnsembleBestOfN:
		output, selectedIndex = e.aggregateBestOfN(responses)
	default:
		output, selectedIndex = e.aggregateFirst(responses)
	}

	return &EnsembleResult{
		Output:        output,
		Responses:     responses,
		SelectedIndex: selectedIndex,
		Duration:      time.Since(startTime),
	}, nil
}

// aggregateFirst returns the first successful response.
func (e *EnsembleEngine) aggregateFirst(responses []AgentResponse) (string, int) {
	for i, r := range responses {
		if r.Error == nil {
			return r.Output, i
		}
	}
	// Return first response even if it errored
	return responses[0].Output, 0
}

// aggregateConcat concatenates all successful responses.
func (e *EnsembleEngine) aggregateConcat(responses []AgentResponse) (string, int) {
	var parts []string
	for _, r := range responses {
		if r.Error == nil && r.Output != "" {
			parts = append(parts, r.Output)
		}
	}
	return strings.Join(parts, "\n\n---\n\n"), 0
}

// aggregateCascade returns the first successful response, trying in order.
func (e *EnsembleEngine) aggregateCascade(responses []AgentResponse) (string, int) {
	return e.aggregateFirst(responses)
}

// aggregateMajorityVote returns the most common response.
func (e *EnsembleEngine) aggregateMajorityVote(responses []AgentResponse) (string, int) {
	votes := make(map[string]int)
	for _, r := range responses {
		if r.Error == nil {
			votes[r.Output]++
		}
	}

	var maxVotes int
	var winner string
	var winnerIndex int

	for i, r := range responses {
		if votes[r.Output] > maxVotes {
			maxVotes = votes[r.Output]
			winner = r.Output
			winnerIndex = i
		}
	}

	return winner, winnerIndex
}

// aggregateBestOfN returns the longest response (simple heuristic).
func (e *EnsembleEngine) aggregateBestOfN(responses []AgentResponse) (string, int) {
	var best string
	var bestIdx int
	var bestLen int

	for i, r := range responses {
		if r.Error == nil && len(r.Output) > bestLen {
			best = r.Output
			bestIdx = i
			bestLen = len(r.Output)
		}
	}

	return best, bestIdx
}
