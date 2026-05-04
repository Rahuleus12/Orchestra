package observability

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	provider "github.com/user/orchestra/internal/provider"
)

// InstrumentProvider wraps provider Generate calls with tracing and metrics.
// It creates a span for each provider call, records latency, token usage,
// and error status.
type InstrumentProvider struct {
	tracer  *Tracer
	metrics *OrchestraMetrics
	logger  *slog.Logger
}

// NewInstrumentProvider creates a new instrumentation helper for providers.
func NewInstrumentProvider(
	tracer *TracerProvider,
	metrics *OrchestraMetrics,
	logger *slog.Logger,
) *InstrumentProvider {
	return &InstrumentProvider{
		tracer:  tracer.Tracer("orchestra.provider"),
		metrics: metrics,
		logger: logger.With(
			slog.String("component", "instrumentation"),
			slog.String("target", "provider"),
		),
	}
}

// WrapGenerate instruments a provider Generate call with tracing and metrics.
// It returns a function that wraps the original Generate function.
func (ip *InstrumentProvider) WrapGenerate(
	providerName string,
	fn func(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResult, error),
) func(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResult, error) {
	return func(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResult, error) {
		spanName := fmt.Sprintf("orchestra.provider.%s.generate", providerName)
		ctx, span := ip.tracer.Start(ctx, spanName,
			WithAttributes(
				StringAttr("provider", providerName),
				StringAttr("model", req.Model),
				IntAttr("message_count", len(req.Messages)),
				IntAttr("tool_count", len(req.Tools)),
			),
			WithSpanKind(SpanKindClient),
		)
		defer span.End()

		start := time.Now()
		result, err := fn(ctx, req)
		duration := time.Since(start)

		// Record metrics
		if ip.metrics != nil {
			ip.metrics.ProviderLatency.RecordDuration(duration)
			ip.metrics.ProviderRequests.Inc()

			if err == nil {
				ip.metrics.TokensTotal.Add(
					int64(result.Usage.PromptTokens + result.Usage.CompletionTokens),
				)
			}
		}

		if err != nil {
			span.RecordError(err,
				StringAttr("provider", providerName),
				StringAttr("model", req.Model),
			)
			span.SetStatus(SpanStatusError, err.Error())

			ip.logger.Error("provider call failed",
				slog.String("provider", providerName),
				slog.String("model", req.Model),
				slog.Int64("duration_ms", duration.Milliseconds()),
				slog.String("error", err.Error()),
			)
			return nil, err
		}

		// Add result attributes to span
		span.SetAttributes(
			StringAttr("finish_reason", string(result.FinishReason)),
			IntAttr("prompt_tokens", result.Usage.PromptTokens),
			IntAttr("completion_tokens", result.Usage.CompletionTokens),
			IntAttr("total_tokens", result.Usage.TotalTokens),
		)
		span.SetStatus(SpanStatusOK, "")

		ip.logger.Debug("provider call completed",
			slog.String("provider", providerName),
			slog.String("model", req.Model),
			slog.Int64("duration_ms", duration.Milliseconds()),
			slog.Int("total_tokens", result.Usage.TotalTokens),
		)

		return result, nil
	}
}

// InstrumentAgent provides instrumentation helpers for agent execution.
type InstrumentAgent struct {
	tracer  *Tracer
	metrics *OrchestraMetrics
	logger  *slog.Logger
}

// NewInstrumentAgent creates a new instrumentation helper for agents.
func NewInstrumentAgent(
	tracer *TracerProvider,
	metrics *OrchestraMetrics,
	logger *slog.Logger,
) *InstrumentAgent {
	return &InstrumentAgent{
		tracer:  tracer.Tracer("orchestra.agent"),
		metrics: metrics,
		logger: logger.With(
			slog.String("component", "instrumentation"),
			slog.String("target", "agent"),
		),
	}
}

// StartRun creates a span for an agent run and increments the active agents gauge.
func (ia *InstrumentAgent) StartRun(
	ctx context.Context,
	agentName, model string,
) (context.Context, Span) {
	spanName := fmt.Sprintf("orchestra.agent.%s.run", agentName)
	ctx, span := ia.tracer.Start(ctx, spanName,
		WithAttributes(
			StringAttr("agent", agentName),
			StringAttr("model", model),
		),
		WithSpanKind(SpanKindInternal),
	)

	if ia.metrics != nil {
		ia.metrics.ActiveAgents.Inc()
	}

	ia.logger.Debug("agent run started",
		slog.String("agent", agentName),
		slog.String("model", model),
	)

	return ctx, span
}

// EndRun completes an agent run span, records metrics, and decrements the active gauge.
func (ia *InstrumentAgent) EndRun(
	span Span,
	agentName string,
	duration time.Duration,
	turns int,
	usage provider.TokenUsage,
	err error,
) {
	if err != nil {
		span.RecordError(err, StringAttr("agent", agentName))
		span.SetStatus(SpanStatusError, err.Error())
	} else {
		span.SetStatus(SpanStatusOK, "")
	}

	span.SetAttributes(
		IntAttr("turns", turns),
		IntAttr("prompt_tokens", usage.PromptTokens),
		IntAttr("completion_tokens", usage.CompletionTokens),
		IntAttr("total_tokens", usage.TotalTokens),
		Float64Attr("duration_seconds", duration.Seconds()),
	)
	span.End()

	if ia.metrics != nil {
		ia.metrics.ActiveAgents.Dec()
		ia.metrics.AgentTurns.Add(int64(turns))
		ia.metrics.TokensTotal.Add(int64(usage.TotalTokens))
	}

	ia.logger.Debug("agent run completed",
		slog.String("agent", agentName),
		slog.Int64("duration_ms", duration.Milliseconds()),
		slog.Int("turns", turns),
		slog.Int("total_tokens", usage.TotalTokens),
	)
}

// StartToolCall creates a span for a tool execution within an agent run.
func (ia *InstrumentAgent) StartToolCall(
	ctx context.Context,
	agentName, toolName string,
) (context.Context, Span) {
	spanName := fmt.Sprintf("orchestra.agent.%s.tool.%s", agentName, toolName)
	ctx, span := ia.tracer.Start(ctx, spanName,
		WithAttributes(
			StringAttr("agent", agentName),
			StringAttr("tool", toolName),
		),
		WithSpanKind(SpanKindInternal),
	)

	if ia.metrics != nil {
		ia.metrics.ToolExecutions.Inc()
	}

	return ctx, span
}

// EndToolCall completes a tool execution span.
func (ia *InstrumentAgent) EndToolCall(span Span, toolName string, duration time.Duration, err error) {
	if err != nil {
		span.RecordError(err, StringAttr("tool", toolName))
		span.SetStatus(SpanStatusError, err.Error())
	} else {
		span.SetStatus(SpanStatusOK, "")
	}

	span.SetAttributes(
		Float64Attr("duration_seconds", duration.Seconds()),
	)
	span.End()

	if ia.metrics != nil {
		ia.metrics.ToolLatency.RecordDuration(duration)
	}

	ia.logger.Debug("tool execution completed",
		slog.String("tool", toolName),
		slog.Int64("duration_ms", duration.Milliseconds()),
	)
}

// InstrumentWorkflow provides instrumentation helpers for workflow execution.
type InstrumentWorkflow struct {
	tracer  *Tracer
	metrics *OrchestraMetrics
	logger  *slog.Logger
}

// NewInstrumentWorkflow creates a new instrumentation helper for workflows.
func NewInstrumentWorkflow(
	tracer *TracerProvider,
	metrics *OrchestraMetrics,
	logger *slog.Logger,
) *InstrumentWorkflow {
	return &InstrumentWorkflow{
		tracer:  tracer.Tracer("orchestra.workflow"),
		metrics: metrics,
		logger: logger.With(
			slog.String("component", "instrumentation"),
			slog.String("target", "workflow"),
		),
	}
}

// StartWorkflow creates a span for a workflow execution and increments the active gauge.
func (iw *InstrumentWorkflow) StartWorkflow(
	ctx context.Context,
	workflowName string,
	stepCount int,
) (context.Context, Span) {
	spanName := fmt.Sprintf("orchestra.workflow.%s.execute", workflowName)
	ctx, span := iw.tracer.Start(ctx, spanName,
		WithAttributes(
			StringAttr("workflow", workflowName),
			IntAttr("step_count", stepCount),
		),
		WithSpanKind(SpanKindInternal),
	)

	if iw.metrics != nil {
		iw.metrics.ActiveWorkflows.Inc()
	}

	iw.logger.Debug("workflow execution started",
		slog.String("workflow", workflowName),
		slog.Int("step_count", stepCount),
	)

	return ctx, span
}

// EndWorkflow completes a workflow execution span.
func (iw *InstrumentWorkflow) EndWorkflow(
	span Span,
	workflowName string,
	duration time.Duration,
	usage provider.TokenUsage,
	err error,
) {
	if err != nil {
		span.RecordError(err, StringAttr("workflow", workflowName))
		span.SetStatus(SpanStatusError, err.Error())
	} else {
		span.SetStatus(SpanStatusOK, "")
	}

	span.SetAttributes(
		IntAttr("total_tokens", usage.TotalTokens),
		Float64Attr("duration_seconds", duration.Seconds()),
	)
	span.End()

	if iw.metrics != nil {
		iw.metrics.ActiveWorkflows.Dec()
		iw.metrics.WorkflowDuration.RecordDuration(duration)
	}

	iw.logger.Debug("workflow execution completed",
		slog.String("workflow", workflowName),
		slog.Int64("duration_ms", duration.Milliseconds()),
		slog.Int("total_tokens", usage.TotalTokens),
	)
}

// StartStep creates a span for a workflow step.
func (iw *InstrumentWorkflow) StartStep(
	ctx context.Context,
	workflowName, stepID, agentName string,
) (context.Context, Span) {
	spanName := fmt.Sprintf("orchestra.workflow.%s.step.%s", workflowName, stepID)
	ctx, span := iw.tracer.Start(ctx, spanName,
		WithAttributes(
			StringAttr("workflow", workflowName),
			StringAttr("step_id", stepID),
			StringAttr("agent", agentName),
		),
		WithSpanKind(SpanKindInternal),
	)

	return ctx, span
}

// EndStep completes a workflow step span.
func (iw *InstrumentWorkflow) EndStep(span Span, stepID string, duration time.Duration, err error) {
	if err != nil {
		span.RecordError(err, StringAttr("step_id", stepID))
		span.SetStatus(SpanStatusError, err.Error())
	} else {
		span.SetStatus(SpanStatusOK, "")
	}

	span.SetAttributes(
		Float64Attr("duration_seconds", duration.Seconds()),
	)
	span.End()
}
