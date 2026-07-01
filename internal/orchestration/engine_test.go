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

// delayedAgent creates an agent backed by a mock provider whose response blocks
// for the given delay (and respects context cancellation).
func delayedAgent(name string, delay time.Duration) *agent.Agent {
	mp := mock.NewProvider(name)
	mp.SetDefaultResponse(mock.MockResponse{
		Message:      message.AssistantMessage("delayed response"),
		Delay:        delay,
		Usage:        provider.TokenUsage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
		FinishReason: provider.FinishReasonStop,
	})
	a, _ := agent.New(name, agent.WithProvider(mp, "mock-model"))
	return a
}

// inputFromMap returns a step input mapping that reads the given key from the
// workflow input, falling back to the literal default.
func inputFromMap(key, fallback string) func(*WorkflowContext) (string, error) {
	return func(wfCtx *WorkflowContext) (string, error) {
		if v, ok := wfCtx.Get(key).(string); ok && v != "" {
			return v, nil
		}
		return fallback, nil
	}
}

// TestEngine_CancelReportsCancelledStatus is a regression test for a bug where
// cancelling a workflow was reported as StatusFailed instead of StatusCancelled.
// The engine checked the caller's parent context rather than the workflow
// context / the cancellation error in the step result.
func TestEngine_CancelReportsCancelledStatus(t *testing.T) {
	a := delayedAgent("slow-agent", 10*time.Second)

	workflow, err := NewWorkflowBuilder("cancel-test").
		AddStep("step-1", a, WithInput(inputFromMap("input", "go"))).
		Build()
	if err != nil {
		t.Fatalf("build workflow: %v", err)
	}

	engine := NewEngine()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel shortly after execution starts, while the step is blocked on the
	// provider's artificial delay.
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	result, err := engine.Execute(ctx, workflow, map[string]any{"input": "go"})
	if err == nil {
		t.Fatal("expected an error from a cancelled workflow, got nil")
	}
	if result.Status != StatusCancelled {
		t.Errorf("expected status %q, got %q (err=%v)", StatusCancelled, result.Status, err)
	}
}

// TestEngine_SuccessReportsCompletedStatus is a sanity check that a normally
// completing workflow reports StatusCompleted.
func TestEngine_SuccessReportsCompletedStatus(t *testing.T) {
	a := delayedAgent("fast-agent", 0)

	workflow, err := NewWorkflowBuilder("ok-test").
		AddStep("step-1", a, WithInput(inputFromMap("input", "go"))).
		Build()
	if err != nil {
		t.Fatalf("build workflow: %v", err)
	}

	engine := NewEngine()
	result, err := engine.Execute(context.Background(), workflow, map[string]any{"input": "go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusCompleted {
		t.Errorf("expected status %q, got %q", StatusCompleted, result.Status)
	}
}
