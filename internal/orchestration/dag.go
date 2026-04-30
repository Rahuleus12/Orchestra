package orchestration

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/user/orchestra/internal/agent"
	"github.com/user/orchestra/internal/provider"
)

// WorkflowStatus represents the current state of a workflow execution.
type WorkflowStatus string

const (
	// StatusPending indicates the workflow has been created but not started.
	StatusPending WorkflowStatus = "pending"
	// StatusRunning indicates the workflow is currently executing.
	StatusRunning WorkflowStatus = "running"
	// StatusCompleted indicates the workflow finished successfully.
	StatusCompleted WorkflowStatus = "completed"
	// StatusFailed indicates the workflow failed due to an error.
	StatusFailed WorkflowStatus = "failed"
	// StatusCancelled indicates the workflow was cancelled.
	StatusCancelled WorkflowStatus = "cancelled"
)

// Workflow represents a directed acyclic graph of steps that compose multiple agents.
type Workflow struct {
	mu       sync.RWMutex
	id       string
	name     string
	steps    map[string]*Step
	edges    []Edge
	input    InputMapping
	output   OutputMapping
	metadata map[string]any
}

// NewWorkflow creates a new empty workflow with the given name.
func NewWorkflow(name string) *Workflow {
	return &Workflow{
		id:       generateWorkflowID(),
		name:     name,
		steps:    make(map[string]*Step),
		edges:    make([]Edge, 0),
		input:    defaultInputMapping,
		output:   defaultOutputMapping,
		metadata: make(map[string]any),
	}
}

// ID returns the unique identifier for this workflow.
func (w *Workflow) ID() string {
	return w.id
}

// Name returns the workflow name.
func (w *Workflow) Name() string {
	return w.name
}

// AddStep adds a step to the workflow.
func (w *Workflow) AddStep(step *Step) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.steps[step.ID]; exists {
		return fmt.Errorf("step with ID %s already exists", step.ID)
	}

	w.steps[step.ID] = step
	return nil
}

// GetStep retrieves a step by its ID.
func (w *Workflow) GetStep(id string) (*Step, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	step, exists := w.steps[id]
	if !exists {
		return nil, fmt.Errorf("step %s not found", id)
	}
	return step, nil
}

// AddEdge adds an edge between two steps.
func (w *Workflow) AddEdge(from, to string, condition Condition) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.steps[from]; !exists {
		return fmt.Errorf("from step %s not found", from)
	}

	if _, exists := w.steps[to]; !exists {
		return fmt.Errorf("to step %s not found", to)
	}

	w.edges = append(w.edges, Edge{From: from, To: to, Condition: condition})
	return nil
}

// GetEdges returns all edges in the workflow.
func (w *Workflow) GetEdges() []Edge {
	w.mu.RLock()
	defer w.mu.RUnlock()

	edges := make([]Edge, len(w.edges))
	copy(edges, w.edges)
	return edges
}

// GetAllSteps returns all steps in the workflow.
func (w *Workflow) GetAllSteps() []*Step {
	w.mu.RLock()
	defer w.mu.RUnlock()

	steps := make([]*Step, 0, len(w.steps))
	for _, step := range w.steps {
		steps = append(steps, step)
	}
	return steps
}

// SetInputMapping sets the workflow-level input mapping.
func (w *Workflow) SetInputMapping(mapping InputMapping) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.input = mapping
}

// GetInputMapping returns the workflow-level input mapping.
func (w *Workflow) GetInputMapping() InputMapping {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.input
}

// SetOutputMapping sets the workflow-level output mapping.
func (w *Workflow) SetOutputMapping(mapping OutputMapping) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.output = mapping
}

// GetOutputMapping returns the workflow-level output mapping.
func (w *Workflow) GetOutputMapping() OutputMapping {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.output
}

// SetMetadata sets a metadata value for the workflow.
func (w *Workflow) SetMetadata(key string, value any) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.metadata[key] = value
}

// GetMetadata retrieves a metadata value from the workflow.
func (w *Workflow) GetMetadata(key string) (any, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	value, exists := w.metadata[key]
	return value, exists
}

// Validate checks if the workflow is valid (no cycles, all dependencies resolvable).
func (w *Workflow) Validate() error {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if len(w.steps) == 0 {
		return fmt.Errorf("workflow must have at least one step")
	}

	// Check for cycles using DFS
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	for stepID := range w.steps {
		if !visited[stepID] {
			if w.hasCycleDFS(stepID, visited, recStack) {
				return fmt.Errorf("workflow contains a cycle")
			}
		}
	}

	return nil
}

// hasCycleDFS performs DFS to detect cycles.
func (w *Workflow) hasCycleDFS(node string, visited, recStack map[string]bool) bool {
	visited[node] = true
	recStack[node] = true

	for _, edge := range w.edges {
		if edge.From == node {
			if !visited[edge.To] {
				if w.hasCycleDFS(edge.To, visited, recStack) {
					return true
				}
			} else if recStack[edge.To] {
				return true
			}
		}
	}

	recStack[node] = false
	return false
}

// GetTopologicalOrder returns steps in topological order for execution.
func (w *Workflow) GetTopologicalOrder() ([]string, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if err := w.Validate(); err != nil {
		return nil, err
	}

	// Create adjacency list and in-degree count
	adj := make(map[string][]string)
	inDegree := make(map[string]int)

	for stepID := range w.steps {
		adj[stepID] = []string{}
		inDegree[stepID] = 0
	}

	for _, edge := range w.edges {
		adj[edge.From] = append(adj[edge.From], edge.To)
		inDegree[edge.To]++
	}

	// Kahn's algorithm for topological sort
	queue := make([]string, 0)
	for stepID, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, stepID)
		}
	}

	order := make([]string, 0, len(w.steps))
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		order = append(order, current)

		for _, neighbor := range adj[current] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if len(order) != len(w.steps) {
		return nil, fmt.Errorf("failed to compute topological order")
	}

	return order, nil
}

// GetIndependentSteps returns the IDs of steps that can run in parallel at the given level.
func (w *Workflow) GetIndependentSteps() ([][]string, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	order, err := w.GetTopologicalOrder()
	if err != nil {
		return nil, err
	}

	// Group steps by level (steps at the same level can run in parallel)
	levels := [][]string{}
	stepToLevel := make(map[string]int)
	adj := make(map[string][]string)

	for stepID := range w.steps {
		adj[stepID] = []string{}
	}

	for _, edge := range w.edges {
		adj[edge.From] = append(adj[edge.From], edge.To)
	}

	for _, stepID := range order {
		maxParentLevel := -1
		for _, edge := range w.edges {
			if edge.To == stepID {
				if level, exists := stepToLevel[edge.From]; exists && level > maxParentLevel {
					maxParentLevel = level
				}
			}
		}

		stepLevel := maxParentLevel + 1
		stepToLevel[stepID] = stepLevel

		if stepLevel >= len(levels) {
			levels = append(levels, []string{})
		}
		levels[stepLevel] = append(levels[stepLevel], stepID)
	}

	return levels, nil
}

// Step represents a single execution step in a workflow.
type Step struct {
	ID          string
	Agent       *agent.Agent
	InputMap    InputMapping  // Maps workflow context → agent input
	OutputMap   OutputMapping // Maps agent output → workflow context
	Condition   Condition     // Optional: only execute if true
	RetryPolicy *RetryPolicy
	Timeout     time.Duration
	metadata    map[string]any
}

// NewStep creates a new step with the given ID and agent.
func NewStep(id string, agent *agent.Agent) *Step {
	return &Step{
		ID:          id,
		Agent:       agent,
		InputMap:    defaultStepInputMapping,
		OutputMap:   defaultStepOutputMapping,
		Condition:   alwaysTrue,
		RetryPolicy: DefaultRetryPolicy(),
		Timeout:     5 * time.Minute,
		metadata:    make(map[string]any),
	}
}

// SetMetadata sets metadata for this step.
func (s *Step) SetMetadata(key string, value any) {
	s.metadata[key] = value
}

// GetMetadata retrieves metadata from this step.
func (s *Step) GetMetadata(key string) (any, bool) {
	value, exists := s.metadata[key]
	return value, exists
}

// Edge represents a directed connection between two steps.
type Edge struct {
	From      string
	To        string
	Condition Condition // Optional: conditional routing
}

// InputMapping maps workflow context to agent input.
type InputMapping func(ctx *WorkflowContext) (string, error)

// OutputMapping maps agent output to workflow context.
type OutputMapping func(result *agent.AgentResult, ctx *WorkflowContext) error

// Condition determines if a step should execute or if an edge should be followed.
type Condition func(ctx *WorkflowContext) bool

// RetryPolicy defines how a failed step should be retried.
type RetryPolicy struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
	Jitter       bool
}

// DefaultRetryPolicy returns a default retry policy.
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts:  3,
		InitialDelay: time.Second,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		Jitter:       true,
	}
}

// ComputeDelay computes the delay for the given attempt.
func (rp *RetryPolicy) ComputeDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}

	delay := time.Duration(float64(rp.InitialDelay) * math.Pow(rp.Multiplier, float64(attempt-1)))

	if delay > rp.MaxDelay {
		delay = rp.MaxDelay
	}

	if rp.Jitter {
		// Add random jitter up to 25% of the delay
		jitter := time.Duration(float64(delay) * 0.25)
		delay += time.Duration(float64(jitter) * (2.0*randFloat64() - 1.0))
	}

	return delay
}

// WorkflowContext holds shared state across workflow execution.
type WorkflowContext struct {
	mu          sync.RWMutex
	ctx         context.Context
	input       map[string]any
	output      map[string]any
	stepOutputs map[string]*agent.AgentResult
	metadata    map[string]any
	currentStep string
	cancel      context.CancelFunc
}

// NewWorkflowContext creates a new workflow context.
func NewWorkflowContext(ctx context.Context, input map[string]any) *WorkflowContext {
	wrappedCtx, cancel := context.WithCancel(ctx)

	return &WorkflowContext{
		ctx:         wrappedCtx,
		input:       input,
		output:      make(map[string]any),
		stepOutputs: make(map[string]*agent.AgentResult),
		metadata:    make(map[string]any),
		cancel:      cancel,
	}
}

// Context returns the underlying context.
func (wc *WorkflowContext) Context() context.Context {
	return wc.ctx
}

// Get retrieves a value from the workflow input.
func (wc *WorkflowContext) Get(key string) any {
	wc.mu.RLock()
	defer wc.mu.RUnlock()

	return wc.input[key]
}

// Set sets a value in the workflow output.
func (wc *WorkflowContext) Set(key string, value any) {
	wc.mu.Lock()
	defer wc.mu.Unlock()

	wc.output[key] = value
}

// GetOutput retrieves a value from the workflow output.
func (wc *WorkflowContext) GetOutput(key string) (any, bool) {
	wc.mu.RLock()
	defer wc.mu.RUnlock()

	value, exists := wc.output[key]
	return value, exists
}

// SetStepOutput stores the result of a step execution.
func (wc *WorkflowContext) SetStepOutput(stepID string, result *agent.AgentResult) {
	wc.mu.Lock()
	defer wc.mu.Unlock()

	wc.stepOutputs[stepID] = result
}

// GetStepOutput retrieves the result of a step execution.
func (wc *WorkflowContext) GetStepOutput(stepID string) (*agent.AgentResult, error) {
	wc.mu.RLock()
	defer wc.mu.RUnlock()

	result, exists := wc.stepOutputs[stepID]
	if !exists {
		return nil, fmt.Errorf("no output found for step %s", stepID)
	}
	return result, nil
}

// SetMetadata sets metadata on the workflow context.
func (wc *WorkflowContext) SetMetadata(key string, value any) {
	wc.mu.Lock()
	defer wc.mu.Unlock()

	wc.metadata[key] = value
}

// GetMetadata retrieves metadata from the workflow context.
func (wc *WorkflowContext) GetMetadata(key string) (any, bool) {
	wc.mu.RLock()
	defer wc.mu.RUnlock()

	value, exists := wc.metadata[key]
	return value, exists
}

// CurrentStep returns the ID of the currently executing step.
func (wc *WorkflowContext) CurrentStep() string {
	wc.mu.RLock()
	defer wc.mu.RUnlock()

	return wc.currentStep
}

// SetCurrentStep sets the ID of the currently executing step.
func (wc *WorkflowContext) SetCurrentStep(stepID string) {
	wc.mu.Lock()
	defer wc.mu.Unlock()

	wc.currentStep = stepID
}

// Cancel cancels the workflow execution.
func (wc *WorkflowContext) Cancel() {
	wc.cancel()
}

// StepResult represents the result of a single step execution.
type StepResult struct {
	StepID    string
	Status    WorkflowStatus
	Result    *agent.AgentResult
	Error     error
	Duration  time.Duration
	Attempts  int
	Timestamp time.Time
}

// WorkflowResult represents the final result of a workflow execution.
type WorkflowResult struct {
	ID        string
	Name      string
	Status    WorkflowStatus
	Steps     map[string]*StepResult
	Output    map[string]any
	Usage     provider.TokenUsage
	Duration  time.Duration
	Error     error
	Metadata  map[string]any
	StartTime time.Time
	EndTime   time.Time
}

// WorkflowEvent represents an event emitted during workflow execution.
type WorkflowEventType string

const (
	// WorkflowEventStarted is emitted when workflow execution begins.
	WorkflowEventStarted WorkflowEventType = "started"
	// WorkflowEventStepStarted is emitted when a step begins execution.
	WorkflowEventStepStarted WorkflowEventType = "step_started"
	// WorkflowEventStepCompleted is emitted when a step completes successfully.
	WorkflowEventStepCompleted WorkflowEventType = "step_completed"
	// WorkflowEventStepFailed is emitted when a step fails.
	WorkflowEventStepFailed WorkflowEventType = "step_failed"
	// WorkflowEventCompleted is emitted when workflow execution completes successfully.
	WorkflowEventCompleted WorkflowEventType = "completed"
	// WorkflowEventFailed is emitted when workflow execution fails.
	WorkflowEventFailed WorkflowEventType = "failed"
	// WorkflowEventCancelled is emitted when workflow execution is cancelled.
	WorkflowEventCancelled WorkflowEventType = "cancelled"
)

// WorkflowEvent represents a single workflow event.
type WorkflowEvent struct {
	Type      WorkflowEventType `json:"type" yaml:"type"`
	StepID    string            `json:"step_id,omitempty" yaml:"step_id,omitempty"`
	Result    *StepResult       `json:"result,omitempty" yaml:"result,omitempty"`
	Error     error             `json:"-" yaml:"-"`
	Timestamp time.Time         `json:"timestamp" yaml:"timestamp"`
}

// Default implementations

func defaultInputMapping(ctx *WorkflowContext) (string, error) {
	if topic, ok := ctx.Get("topic").(string); ok {
		return topic, nil
	}
	return "", fmt.Errorf("no 'topic' in workflow input")
}

func defaultOutputMapping(result *agent.AgentResult, ctx *WorkflowContext) error {
	ctx.Set("output", result.FinalText())
	return nil
}

func defaultStepInputMapping(ctx *WorkflowContext) (string, error) {
	return "", nil
}

func defaultStepOutputMapping(result *agent.AgentResult, ctx *WorkflowContext) error {
	return nil
}

func alwaysTrue(ctx *WorkflowContext) bool {
	return true
}

// Helper functions

func generateWorkflowID() string {
	return fmt.Sprintf("wf-%d", time.Now().UnixNano())
}

func randFloat64() float64 {
	// Simple deterministic pseudo-random for jitter
	return float64(time.Now().UnixNano()%1000) / 1000.0
}
