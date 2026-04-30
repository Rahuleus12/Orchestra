package orchestration

import (
	"fmt"
	"time"

	"github.com/user/orchestra/internal/agent"
)

// RouteCondition determines if a route should be taken based on the input.
type RouteCondition func(input string) bool

// Router routes input to different agents based on condition matching.
type Router struct {
	name     string
	routes   []Route
	defaults *agent.Agent
}

// Route represents a conditional routing path to an agent.
type Route struct {
	Condition RouteCondition
	Agent     *agent.Agent
	Name      string
}

// NewRouter creates a new router with the given name.
func NewRouter(name string) *Router {
	return &Router{
		name:   name,
		routes: make([]Route, 0),
	}
}

// AddRoute adds a conditional route to the router.
// Returns the router for method chaining.
func (r *Router) AddRoute(name string, condition RouteCondition, agent *agent.Agent) *Router {
	r.routes = append(r.routes, Route{
		Condition: condition,
		Agent:     agent,
		Name:      name,
	})
	return r
}

// SetDefault sets the default agent to use if no routes match.
// Returns the router for method chaining.
func (r *Router) SetDefault(agent *agent.Agent) *Router {
	r.defaults = agent
	return r
}

// Build creates a workflow from the router configuration.
func (r *Router) Build() (*Workflow, error) {
	if len(r.routes) == 0 && r.defaults == nil {
		return nil, fmt.Errorf("router must have at least one route or a default agent")
	}

	builder := NewWorkflowBuilder(r.name)

	// Add a router step that dispatches to the appropriate agent
	routerStepID := "router-dispatcher"

	// We'll use a special agent for routing that will be replaced at execution time
	// This is a simplified approach - in practice, you might want a dedicated router agent
	if r.defaults != nil {
		builder.AddStep(routerStepID, r.defaults)
	} else if len(r.routes) > 0 {
		builder.AddStep(routerStepID, r.routes[0].Agent)
	}

	workflow, err := builder.Build()
	if err != nil {
		return nil, err
	}

	// Store routing information in workflow metadata
	workflow.SetMetadata("router_type", "router")
	workflow.SetMetadata("router_routes", r.routes)
	workflow.SetMetadata("router_has_default", r.defaults != nil)

	// Override the step execution to handle routing dynamically
	// This is done by replacing the input mapping
	if routerStep, err := workflow.GetStep(routerStepID); err == nil {
		routerStep.InputMap = func(ctx *WorkflowContext) (string, error) {
			input, ok := ctx.Get("topic").(string)
			if !ok {
				return "", fmt.Errorf("no 'topic' in workflow input for router")
			}

			// Find the first matching route
			for _, route := range r.routes {
				if route.Condition(input) {
					ctx.SetMetadata("selected_route", route.Name)
					ctx.SetMetadata("selected_agent", route.Agent.Name())
					return input, nil
				}
			}

			// Use default if no routes matched
			if r.defaults != nil {
				ctx.SetMetadata("selected_route", "default")
				ctx.SetMetadata("selected_agent", r.defaults.Name())
				return input, nil
			}

			return "", fmt.Errorf("no matching route found for router and no default agent")
		}
	}

	return workflow, nil
}

// DebateConfig configures a debate between multiple agents.
type DebateConfig struct {
	Name      string
	Debaters  []*agent.Agent
	Judge     *agent.Agent
	Rounds    int
	Topic     string
	StopEarly bool // Stop early if judge reaches consensus
}

// Debate creates a workflow where multiple agents debate a topic for N rounds.
func Debate(config DebateConfig) (*Workflow, error) {
	if len(config.Debaters) < 2 {
		return nil, fmt.Errorf("debate must have at least 2 debaters")
	}
	if config.Judge == nil {
		return nil, fmt.Errorf("debate must have a judge agent")
	}
	if config.Rounds <= 0 {
		return nil, fmt.Errorf("debate rounds must be positive")
	}

	builder := NewWorkflowBuilder(config.Name)

	// Add setup step (optional, can be used to prepare initial context)
	setupStepID := "debate-setup"
	builder.AddStep(setupStepID, config.Debaters[0],
		WithInput(func(ctx *WorkflowContext) (string, error) {
			topic, ok := ctx.Get("topic").(string)
			if !ok {
				topic = config.Topic
			}
			return fmt.Sprintf("You are moderating a debate on: %s\nNumber of rounds: %d\nPrepare to evaluate arguments.",
				topic, config.Rounds), nil
		}),
	)

	// Create debate rounds
	var previousStepIDs []string
	for round := 0; round < config.Rounds; round++ {
		roundStepIDs := make([]string, 0, len(config.Debaters))

		// Each debater presents their argument for this round
		for i, debater := range config.Debaters {
			debaterStepID := fmt.Sprintf("debater-%d-round-%d", i, round)
			builder.AddStep(debaterStepID, debater,
				WithInput(func(ctx *WorkflowContext) (string, error) {
					topic, ok := ctx.Get("topic").(string)
					if !ok {
						topic = config.Topic
					}

					// Get previous arguments from this round
					var previousArgs string
					for j := 0; j < i; j++ {
						prevStepID := fmt.Sprintf("debater-%d-round-%d", j, round)
						if result, err := ctx.GetStepOutput(prevStepID); err == nil {
							previousArgs += fmt.Sprintf("\nPrevious argument in this round: %s\n", result.FinalText())
						}
					}

					// Get arguments from previous rounds
					if round > 0 {
						for j := 0; j < len(config.Debaters); j++ {
							prevStepID := fmt.Sprintf("debater-%d-round-%d", j, round-1)
							if result, err := ctx.GetStepOutput(prevStepID); err == nil {
								previousArgs += fmt.Sprintf("\nArgument from round %d by debater %d: %s\n",
									round-1, j, result.FinalText())
							}
						}
					}

					return fmt.Sprintf("You are debater %d in round %d of a debate on: %s\n%s\nPresent your argument:",
						i+1, round+1, topic, previousArgs), nil
				}),
			)

			// Make each debater depend on previous debaters in the same round (sequential)
			// but independent across rounds (parallel)
			if i > 0 {
				builder.DependsOn(roundStepIDs[i-1])
			}

			roundStepIDs = append(roundStepIDs, debaterStepID)
		}

		// All debaters in this round depend on previous round's judge (if not first round)
		if round > 0 && len(previousStepIDs) > 0 {
			for _, _ = range roundStepIDs {
				builder.DependsOn(previousStepIDs[len(previousStepIDs)-1])
			}
		}

		previousStepIDs = append(previousStepIDs, roundStepIDs...)

		// Add judge evaluation for this round
		judgeStepID := fmt.Sprintf("judge-round-%d", round)
		builder.AddStep(judgeStepID, config.Judge,
			WithInput(func(ctx *WorkflowContext) (string, error) {
				topic, ok := ctx.Get("topic").(string)
				if !ok {
					topic = config.Topic
				}

				// Collect all debater arguments for this round
				var arguments string
				for i := range config.Debaters {
					debaterStepID := fmt.Sprintf("debater-%d-round-%d", i, round)
					if result, err := ctx.GetStepOutput(debaterStepID); err == nil {
						arguments += fmt.Sprintf("\nDebater %d's argument: %s\n", i+1, result.FinalText())
					}
				}

				return fmt.Sprintf("You are the judge for round %d of a debate on: %s\n%s\nEvaluate the arguments and declare a winner or if it's a tie:",
					round+1, topic, arguments), nil
			}),
		)

		// Judge depends on all debaters in this round
		for _, stepID := range roundStepIDs {
			builder.DependsOn(stepID)
		}

		previousStepIDs = append(previousStepIDs, judgeStepID)

		// If stop early is enabled and not the last round, add a condition
		if config.StopEarly && round < config.Rounds-1 {
			// This would require implementing conditional execution
			// For now, we'll execute all rounds
		}
	}

	// Add final verdict step
	verdictStepID := "final-verdict"
	builder.AddStep(verdictStepID, config.Judge,
		WithInput(func(ctx *WorkflowContext) (string, error) {
			topic, ok := ctx.Get("topic").(string)
			if !ok {
				topic = config.Topic
			}

			// Collect all judge evaluations from all rounds
			var evaluations string
			for round := 0; round < config.Rounds; round++ {
				judgeStepID := fmt.Sprintf("judge-round-%d", round)
				if result, err := ctx.GetStepOutput(judgeStepID); err == nil {
					evaluations += fmt.Sprintf("\nRound %d evaluation: %s\n", round+1, result.FinalText())
				}
			}

			return fmt.Sprintf("You are the final judge for a debate on: %s\nAfter %d rounds of debate:\n%s\nProvide your final verdict and reasoning:",
				topic, config.Rounds, evaluations), nil
		}),
		WithOutput(func(result *agent.AgentResult, ctx *WorkflowContext) error {
			ctx.Set("output", result.FinalText())
			ctx.Set("verdict", result.FinalText())
			return nil
		}),
	)

	// Final verdict depends on the last judge
	builder.DependsOn(previousStepIDs[len(previousStepIDs)-1])

	return builder.Build()
}

// DelegationTask represents a task that can be delegated to a worker.
type DelegationTask struct {
	ID          string
	Description string
	WorkerType  string // Identifies which worker type should handle this
	Input       string
	Priority    int
}

// DelegationResult represents the result of a delegated task.
type DelegationResult struct {
	TaskID   string
	Worker   string
	Output   string
	Error    error
	Duration time.Duration
}

// HierarchicalConfig configures a hierarchical delegation workflow.
type HierarchicalConfig struct {
	Name           string
	Manager        *agent.Agent
	Workers        map[string]*agent.Agent // Worker type -> Worker agent
	MaxDelegations int                     // Maximum number of delegations
	MaxDepth       int                     // Maximum depth of delegation hierarchy
}

// Hierarchical creates a workflow where a manager decomposes tasks and delegates to workers.
func Hierarchical(config HierarchicalConfig) (*Workflow, error) {
	if config.Manager == nil {
		return nil, fmt.Errorf("hierarchical workflow must have a manager agent")
	}
	if len(config.Workers) == 0 {
		return nil, fmt.Errorf("hierarchical workflow must have at least one worker")
	}
	if config.MaxDelegations <= 0 {
		config.MaxDelegations = 10
	}
	if config.MaxDepth <= 0 {
		config.MaxDepth = 3
	}

	builder := NewWorkflowBuilder(config.Name)

	// Store configuration in workflow metadata
	builder.WithMetadata("max_delegations", config.MaxDelegations)
	builder.WithMetadata("max_depth", config.MaxDepth)
	builder.WithMetadata("worker_types", getWorkerTypes(config.Workers))

	// Step 1: Manager decomposes the task
	analysisStepID := "manager-analysis"
	builder.AddStep(analysisStepID, config.Manager,
		WithInput(func(ctx *WorkflowContext) (string, error) {
			topic, ok := ctx.Get("topic").(string)
			if !ok {
				return "", fmt.Errorf("no 'topic' in workflow input for hierarchical workflow")
			}

			workerTypes := getWorkerTypes(config.Workers)
			return fmt.Sprintf("You are a project manager. Analyze the following task and decompose it into subtasks:\n\nTask: %s\n\nAvailable worker types: %v\n\nDecompose this task into at most %d subtasks. For each subtask, specify which worker type should handle it.\nFormat each subtask as:\nTASK_ID: [unique ID]\nDESCRIPTION: [task description]\nWORKER_TYPE: [worker type from list]",
				topic, workerTypes, config.MaxDelegations), nil
		}),
		WithOutput(func(result *agent.AgentResult, ctx *WorkflowContext) error {
			ctx.Set("decomposition", result.FinalText())
			ctx.Set("tasks", parseDelegations(result.FinalText()))
			return nil
		}),
	)

	// Step 2: Create worker tasks dynamically based on manager's decomposition
	// We'll add steps for each worker type
	workerStepIDs := make([]string, 0, len(config.Workers))
	for workerType, worker := range config.Workers {
		workerStepID := fmt.Sprintf("worker-%s", workerType)

		builder.AddStep(workerStepID, worker,
			WithInput(func(ctx *WorkflowContext) (string, error) {
				topic, ok := ctx.Get("topic").(string)
				if !ok {
					return "", fmt.Errorf("no 'topic' in workflow input")
				}

				// Get tasks for this worker type
				tasks := getTasksForWorker(ctx, workerType)
				if len(tasks) == 0 {
					// No tasks for this worker
					return fmt.Sprintf("No tasks assigned to you for this project:\n\n%s\n\nWait for instructions.", topic), nil
				}

				// Format tasks for this worker
				var taskList string
				for _, task := range tasks {
					taskList += fmt.Sprintf("\n- Task %s: %s\n", task.ID, task.Description)
				}

				return fmt.Sprintf("You are a %s worker. Execute the following tasks:\n%s\nProject context: %s\n\nComplete all assigned tasks and provide a summary report.",
					workerType, taskList, topic), nil
			}),
			WithOutput(func(result *agent.AgentResult, ctx *WorkflowContext) error {
				workerType := fmt.Sprintf("worker-%s", workerType)
				ctx.Set(fmt.Sprintf("%s-output", workerType), result.FinalText())
				return nil
			}),
		)

		// All workers depend on manager analysis
		builder.DependsOn(analysisStepID)
		workerStepIDs = append(workerStepIDs, workerStepID)
	}

	// Step 3: Manager synthesizes worker results
	synthesisStepID := "manager-synthesis"
	builder.AddStep(synthesisStepID, config.Manager,
		WithInput(func(ctx *WorkflowContext) (string, error) {
			topic, ok := ctx.Get("topic").(string)
			if !ok {
				return "", fmt.Errorf("no 'topic' in workflow input")
			}

			// Get worker outputs
			var workerResults string
			for workerType := range config.Workers {
				outputKey := fmt.Sprintf("worker-%s-output", workerType)
				if output, ok := ctx.GetOutput(outputKey); ok {
					workerResults += fmt.Sprintf("\n\n=== %s Worker Results ===\n%s\n", workerType, output)
				}
			}

			// Get original decomposition
			var decomposition string
			if dec, ok := ctx.GetOutput("decomposition"); ok {
				decomposition = dec.(string)
			}

			return fmt.Sprintf("You are a project manager. Synthesize the worker results into a final deliverable:\n\nOriginal task: %s\n\nOriginal decomposition:\n%s\n\nWorker results:%s\n\nProvide a comprehensive final report that integrates all work.",
				topic, decomposition, workerResults), nil
		}),
		WithOutput(func(result *agent.AgentResult, ctx *WorkflowContext) error {
			ctx.Set("output", result.FinalText())
			ctx.Set("final_report", result.FinalText())
			return nil
		}),
	)

	// Synthesis depends on all workers
	for _, stepID := range workerStepIDs {
		builder.DependsOn(stepID)
	}

	return builder.Build()
}

// Helper functions

func getWorkerTypes(workers map[string]*agent.Agent) []string {
	types := make([]string, 0, len(workers))
	for workerType := range workers {
		types = append(types, workerType)
	}
	return types
}

func parseDelegations(decomposition string) []DelegationTask {
	// Simple parsing implementation
	// In practice, you might want to use a more sophisticated parser
	tasks := []DelegationTask{}

	// This is a simplified parser that looks for TASK_ID, DESCRIPTION, and WORKER_TYPE patterns
	// A production implementation would be more robust
	lines := splitLines(decomposition)
	var currentTask *DelegationTask

	for _, line := range lines {
		line = trimSpace(line)
		if startsWith(line, "TASK_ID:") {
			if currentTask != nil {
				tasks = append(tasks, *currentTask)
			}
			currentTask = &DelegationTask{
				ID: trimSpace(line[8:]),
			}
		} else if startsWith(line, "DESCRIPTION:") && currentTask != nil {
			currentTask.Description = trimSpace(line[12:])
		} else if startsWith(line, "WORKER_TYPE:") && currentTask != nil {
			currentTask.WorkerType = trimSpace(line[12:])
		}
	}

	if currentTask != nil {
		tasks = append(tasks, *currentTask)
	}

	return tasks
}

func getTasksForWorker(ctx *WorkflowContext, workerType string) []DelegationTask {
	tasks, ok := ctx.GetOutput("tasks")
	if !ok {
		return []DelegationTask{}
	}

	delegationTasks, ok := tasks.([]DelegationTask)
	if !ok {
		return []DelegationTask{}
	}

	filtered := []DelegationTask{}
	for _, task := range delegationTasks {
		if task.WorkerType == workerType {
			filtered = append(filtered, task)
		}
	}

	return filtered
}

// String utilities (simplified versions to avoid importing strings package)

func splitLines(s string) []string {
	if s == "" {
		return []string{}
	}

	lines := []string{}
	current := ""
	for _, ch := range s {
		if ch == '\n' {
			lines = append(lines, current)
			current = ""
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func trimSpace(s string) string {
	start := 0
	end := len(s)

	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}

	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}

	return s[start:end]
}

func startsWith(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}

// Common route conditions

// ContainsKeyword returns a condition that checks if the input contains a keyword.
func ContainsKeyword(keyword string) RouteCondition {
	return func(input string) bool {
		return contains(input, keyword)
	}
}

// ContainsCode returns a condition that checks if the input appears to be code-related.
func ContainsCode(input string) bool {
	keywords := []string{"code", "function", "programming", "algorithm", "debug", "syntax"}
	for _, kw := range keywords {
		if contains(input, kw) {
			return true
		}
	}
	return false
}

// IsCreative returns a condition that checks if the input appears to be creative in nature.
func IsCreative(input string) bool {
	keywords := []string{"write", "story", "poem", "creative", "fiction", "narrative"}
	for _, kw := range keywords {
		if contains(input, kw) {
			return true
		}
	}
	return false
}

// IsMath returns a condition that checks if the input appears to be math-related.
func IsMath(input string) bool {
	keywords := []string{"calculate", "math", "equation", "solve", "number", "formula"}
	for _, kw := range keywords {
		if contains(input, kw) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr) >= 0
}

func findSubstring(s, substr string) int {
	if len(substr) == 0 {
		return 0
	}
	if len(s) < len(substr) {
		return -1
	}

	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
