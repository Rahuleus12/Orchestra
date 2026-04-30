package middleware_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/user/orchestra/internal/message"
	"github.com/user/orchestra/internal/middleware"
	"github.com/user/orchestra/internal/provider"
	"github.com/user/orchestra/internal/provider/mock"
)

// ---------------------------------------------------------------------------
// Test Helpers
// ---------------------------------------------------------------------------

// newTestProvider creates a mock provider with a default text response.
func newTestProvider(t *testing.T) *mock.Provider {
	t.Helper()
	p := mock.NewProvider("test")
	p.SetDefaultResponse(mock.MockResponse{
		Message:      message.AssistantMessage("test response"),
		Usage:        provider.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		FinishReason: provider.FinishReasonStop,
	})
	p.SetDefaultStreamChunks([]mock.StreamChunk{
		{Type: provider.StreamEventStart},
		{Type: provider.StreamEventChunk, Chunk: "test "},
		{Type: provider.StreamEventChunk, Chunk: "response"},
		{Type: provider.StreamEventDone, Usage: &provider.TokenUsage{
			PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15,
		}},
	})
	return p
}

// testRequest creates a basic GenerateRequest for testing.
func testRequest() provider.GenerateRequest {
	return provider.GenerateRequest{
		Model:    "mock-model",
		Messages: []message.Message{message.UserMessage("hello")},
	}
}

// errProvider is a provider that always returns errors.
type errProvider struct {
	name  string
	err   error
	calls atomic.Int64
}

func (e *errProvider) Name() string { return e.name }
func (e *errProvider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	return nil, e.err
}

func (e *errProvider) Capabilities(_ string) provider.ModelCapabilities {
	return provider.ModelCapabilities{}
}

func (e *errProvider) Generate(_ context.Context, _ provider.GenerateRequest) (*provider.GenerateResult, error) {
	e.calls.Add(1)
	return nil, e.err
}

func (e *errProvider) Stream(_ context.Context, _ provider.GenerateRequest) (<-chan provider.StreamEvent, error) {
	e.calls.Add(1)
	return nil, e.err
}

// flakyProvider succeeds after N failures.
type flakyProvider struct {
	name         string
	failCount    atomic.Int64
	succeedAfter int64
	result       *provider.GenerateResult
}

func (f *flakyProvider) Name() string { return f.name }
func (f *flakyProvider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	return []provider.ModelInfo{{ID: "test-model", Name: "Test Model"}}, nil
}

func (f *flakyProvider) Capabilities(_ string) provider.ModelCapabilities {
	return provider.ModelCapabilities{Streaming: true, ToolCalling: true}
}

func (f *flakyProvider) Generate(_ context.Context, req provider.GenerateRequest) (*provider.GenerateResult, error) {
	n := f.failCount.Add(1)
	if n <= f.succeedAfter {
		return nil, provider.NewProviderErrorWithCode(f.name, req.Model, "server_error", 500,
			fmt.Errorf("simulated failure %d", n))
	}
	return f.result, nil
}

func (f *flakyProvider) Stream(ctx context.Context, req provider.GenerateRequest) (<-chan provider.StreamEvent, error) {
	n := f.failCount.Add(1)
	if n <= f.succeedAfter {
		return nil, provider.NewProviderErrorWithCode(f.name, req.Model, "server_error", 500,
			fmt.Errorf("simulated failure %d", n))
	}
	ch := make(chan provider.StreamEvent, 4)
	go func() {
		defer close(ch)
		ch <- provider.StreamEvent{Type: provider.StreamEventStart}
		ch <- provider.StreamEvent{Type: provider.StreamEventChunk, Chunk: "ok"}
		ch <- provider.StreamEvent{Type: provider.StreamEventDone}
	}()
	return ch, nil
}

// ---------------------------------------------------------------------------
// Backoff Strategy Tests
// ---------------------------------------------------------------------------

func TestExponentialBackoff_Delay(t *testing.T) {
	tests := []struct {
		name     string
		backoff  middleware.ExponentialBackoff
		attempt  int
		minDelay time.Duration
		maxDelay time.Duration
	}{
		{
			name:     "first attempt with defaults",
			backoff:  middleware.ExponentialBackoff{Initial: time.Second, Max: 30 * time.Second, Multiplier: 2.0},
			attempt:  0,
			minDelay: 800 * time.Millisecond, // Allow some tolerance
			maxDelay: 2 * time.Second,
		},
		{
			name:     "second attempt doubles",
			backoff:  middleware.ExponentialBackoff{Initial: time.Second, Max: 30 * time.Second, Multiplier: 2.0},
			attempt:  1,
			minDelay: 1800 * time.Millisecond,
			maxDelay: 2500 * time.Millisecond,
		},
		{
			name:     "respects max delay",
			backoff:  middleware.ExponentialBackoff{Initial: time.Second, Max: 5 * time.Second, Multiplier: 2.0},
			attempt:  10,
			minDelay: 4 * time.Second,
			maxDelay: 6 * time.Second,
		},
		{
			name:     "negative attempt returns initial",
			backoff:  middleware.ExponentialBackoff{Initial: 2 * time.Second, Max: 30 * time.Second, Multiplier: 2.0},
			attempt:  -1,
			minDelay: 1500 * time.Millisecond,
			maxDelay: 3 * time.Second,
		},
		{
			name:     "zero multiplier defaults to 2.0",
			backoff:  middleware.ExponentialBackoff{Initial: time.Second, Max: 30 * time.Second, Multiplier: 0},
			attempt:  2,
			minDelay: 3500 * time.Millisecond,
			maxDelay: 5 * time.Second,
		},
		{
			name:     "zero initial defaults to 1s",
			backoff:  middleware.ExponentialBackoff{Initial: 0, Max: 30 * time.Second, Multiplier: 2.0},
			attempt:  0,
			minDelay: 800 * time.Millisecond,
			maxDelay: 2 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delay := tt.backoff.Delay(tt.attempt)
			if delay < tt.minDelay || delay > tt.maxDelay {
				t.Errorf("Delay(%d) = %v, want between %v and %v", tt.attempt, delay, tt.minDelay, tt.maxDelay)
			}
		})
	}
}

func TestExponentialBackoff_WithJitter(t *testing.T) {
	b := middleware.ExponentialBackoff{
		Initial:    time.Second,
		Max:        30 * time.Second,
		Multiplier: 2.0,
		Jitter:     true,
	}

	// With jitter, delays should vary across calls
	delays := make(map[time.Duration]bool)
	for i := 0; i < 100; i++ {
		d := b.Delay(3)
		delays[d] = true
	}

	// With jitter, we should see multiple distinct values
	if len(delays) < 10 {
		t.Errorf("expected jitter to produce varied delays, got %d distinct values out of 100 calls", len(delays))
	}
}

func TestExponentialBackoff_NoJitter(t *testing.T) {
	b := middleware.ExponentialBackoff{
		Initial:    time.Second,
		Max:        30 * time.Second,
		Multiplier: 2.0,
		Jitter:     false,
	}

	// Without jitter, delays should be deterministic
	d1 := b.Delay(2)
	d2 := b.Delay(2)
	if d1 != d2 {
		t.Errorf("expected deterministic delays without jitter, got %v and %v", d1, d2)
	}
}

func TestConstantBackoff(t *testing.T) {
	b := middleware.ConstantBackoff(5 * time.Second)

	for attempt := 0; attempt < 5; attempt++ {
		d := b.Delay(attempt)
		if d != 5*time.Second {
			t.Errorf("ConstantBackoff.Delay(%d) = %v, want 5s", attempt, d)
		}
	}
}

// ---------------------------------------------------------------------------
// Retry Middleware Tests
// ---------------------------------------------------------------------------

func TestRetry_SuccessOnFirstAttempt(t *testing.T) {
	mp := newTestProvider(t)
	p := middleware.WithRetry(3, middleware.ExponentialBackoff{
		Initial: 10 * time.Millisecond, Max: time.Second, Multiplier: 2.0,
	})(mp)

	result, err := p.Generate(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text() != "test response" {
		t.Errorf("expected 'test response', got %q", result.Text())
	}
	if mp.CallCount() != 1 {
		t.Errorf("expected 1 call, got %d", mp.CallCount())
	}
}

func TestRetry_SucceedsAfterFailures(t *testing.T) {
	fp := &flakyProvider{
		name:         "flaky",
		succeedAfter: 2,
		result: &provider.GenerateResult{
			ID:           "test-123",
			Message:      message.AssistantMessage("recovered"),
			Usage:        provider.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			FinishReason: provider.FinishReasonStop,
			Model:        "test-model",
			CreatedAt:    time.Now(),
		},
	}

	p := middleware.WithRetry(3, middleware.ConstantBackoff(5*time.Millisecond))(fp)

	result, err := p.Generate(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text() != "recovered" {
		t.Errorf("expected 'recovered', got %q", result.Text())
	}
	if fp.failCount.Load() != 3 {
		t.Errorf("expected 3 attempts (2 failures + 1 success), got %d", fp.failCount.Load())
	}
}

func TestRetry_Exhausted(t *testing.T) {
	ep := &errProvider{
		name: "always-fail",
		err:  provider.NewProviderErrorWithCode("always-fail", "test-model", "server_error", 500, errors.New("internal server error")),
	}

	p := middleware.WithRetry(2, middleware.ConstantBackoff(5*time.Millisecond))(ep)

	_, err := p.Generate(context.Background(), testRequest())
	if err == nil {
		t.Fatal("expected error after retry exhaustion")
	}
	if !strings.Contains(err.Error(), "retry exhausted") {
		t.Errorf("expected retry exhausted error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "internal server error") {
		t.Errorf("expected wrapped error message, got: %v", err)
	}
	if ep.calls.Load() != 3 { // 1 initial + 2 retries
		t.Errorf("expected 3 total calls, got %d", ep.calls.Load())
	}
}

func TestRetry_NonRetryableError(t *testing.T) {
	ep := &errProvider{
		name: "auth-fail",
		err:  provider.NewProviderErrorWithCode("auth-fail", "test-model", "auth_error", 401, errors.New("invalid api key")),
	}

	p := middleware.WithRetry(3, middleware.ConstantBackoff(5*time.Millisecond))(ep)

	_, err := p.Generate(context.Background(), testRequest())
	if err == nil {
		t.Fatal("expected error")
	}
	// Should only be called once since 401 is not retryable
	if ep.calls.Load() != 1 {
		t.Errorf("expected 1 call for non-retryable error, got %d", ep.calls.Load())
	}
}

func TestRetry_RateLimitIsRetryable(t *testing.T) {
	ep := &errProvider{
		name: "rate-limited",
		err:  provider.NewProviderErrorWithCode("rate-limited", "test-model", "rate_limit", 429, errors.New("too many requests")),
	}

	p := middleware.WithRetry(2, middleware.ConstantBackoff(5*time.Millisecond))(ep)

	_, err := p.Generate(context.Background(), testRequest())
	if err == nil {
		t.Fatal("expected error after retry exhaustion")
	}
	// 429 should be retryable: 1 initial + 2 retries = 3 calls
	if ep.calls.Load() != 3 {
		t.Errorf("expected 3 calls for retryable 429 error, got %d", ep.calls.Load())
	}
}

func TestRetry_ContextCancellation(t *testing.T) {
	ep := &errProvider{
		name: "slow-fail",
		err:  provider.NewProviderErrorWithCode("slow-fail", "test-model", "server_error", 500, errors.New("fail")),
	}

	p := middleware.WithRetry(10, middleware.ConstantBackoff(50*time.Millisecond))(ep)

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	_, err := p.Generate(ctx, testRequest())
	if err == nil {
		t.Fatal("expected error")
	}
	// Should have been interrupted by context, not exhausted all retries
	if ep.calls.Load() >= 10 {
		t.Error("expected early termination due to context cancellation")
	}
}

func TestRetry_PassesThroughName(t *testing.T) {
	mp := newTestProvider(t)
	p := middleware.WithRetry(3, nil)(mp)
	if p.Name() != "test" {
		t.Errorf("expected Name() = 'test', got %q", p.Name())
	}
}

func TestRetry_PassesThroughCapabilities(t *testing.T) {
	mp := newTestProvider(t)
	p := middleware.WithRetry(3, nil)(mp)
	caps := p.Capabilities("mock-model")
	if !caps.Streaming {
		t.Error("expected Streaming = true")
	}
}

func TestRetry_PassesThroughModels(t *testing.T) {
	mp := newTestProvider(t)
	p := middleware.WithRetry(3, nil)(mp)
	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() returned error: %v", err)
	}
	if len(models) == 0 {
		t.Error("expected at least one model")
	}
}

func TestRetry_StreamRetries(t *testing.T) {
	fp := &flakyProvider{
		name:         "flaky-stream",
		succeedAfter: 1,
		result: &provider.GenerateResult{
			Message:      message.AssistantMessage("streamed"),
			FinishReason: provider.FinishReasonStop,
			Model:        "test-model",
			CreatedAt:    time.Now(),
		},
	}

	p := middleware.WithRetry(2, middleware.ConstantBackoff(5*time.Millisecond))(fp)

	ch, err := p.Stream(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read all events
	var chunks int
	for evt := range ch {
		if evt.Type == provider.StreamEventChunk {
			chunks++
		}
	}
	if chunks == 0 {
		t.Error("expected at least one chunk")
	}
}

func TestRetry_ZeroAttempts_NoRetry(t *testing.T) {
	ep := &errProvider{
		name: "fail",
		err:  provider.NewProviderErrorWithCode("fail", "m", "err", 500, errors.New("fail")),
	}

	p := middleware.WithRetry(0, nil)(ep)
	_, err := p.Generate(context.Background(), testRequest())
	if err == nil {
		t.Fatal("expected error")
	}
	if ep.calls.Load() != 1 {
		t.Errorf("expected 1 call with 0 retries, got %d", ep.calls.Load())
	}
}

func TestRetry_NilBackoff_UsesDefault(t *testing.T) {
	mp := newTestProvider(t)
	p := middleware.WithRetry(3, nil)(mp)

	result, err := p.Generate(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text() != "test response" {
		t.Errorf("expected 'test response', got %q", result.Text())
	}
}

// ---------------------------------------------------------------------------
// Rate Limit Middleware Tests
// ---------------------------------------------------------------------------

func TestRateLimit_AllowsBurst(t *testing.T) {
	mp := newTestProvider(t)
	p := middleware.WithRateLimit(100, 0)(mp)

	// Should allow a burst of requests
	for i := 0; i < 5; i++ {
		_, err := p.Generate(context.Background(), testRequest())
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
	}
}

func TestRateLimit_ZeroRPM_Unlimited(t *testing.T) {
	mp := newTestProvider(t)
	p := middleware.WithRateLimit(0, 0)(mp)

	// With 0 limits, should be unlimited
	for i := 0; i < 100; i++ {
		_, err := p.Generate(context.Background(), testRequest())
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
	}
}

func TestRateLimit_PassesThroughName(t *testing.T) {
	mp := newTestProvider(t)
	p := middleware.WithRateLimit(60, 0)(mp)
	if p.Name() != "test" {
		t.Errorf("expected 'test', got %q", p.Name())
	}
}

func TestRateLimit_PassesThroughCapabilities(t *testing.T) {
	mp := newTestProvider(t)
	p := middleware.WithRateLimit(60, 0)(mp)
	caps := p.Capabilities("mock-model")
	if !caps.Streaming {
		t.Error("expected Streaming = true")
	}
}

func TestRateLimit_StreamAllowed(t *testing.T) {
	mp := newTestProvider(t)
	p := middleware.WithRateLimit(100, 0)(mp)

	ch, err := p.Stream(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Stream() returned error: %v", err)
	}

	var hasDone bool
	for evt := range ch {
		if evt.Type == provider.StreamEventDone {
			hasDone = true
		}
	}
	if !hasDone {
		t.Error("expected stream to complete")
	}
}

func TestRateLimit_ContextCancellation(t *testing.T) {
	mp := newTestProvider(t)
	// Very low rate limit: 1 request per minute
	p := middleware.WithRateLimit(1, 0)(mp)

	// First request should succeed (consumes the single token)
	_, err := p.Generate(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}

	// Use a short-lived context for the blocked request
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// This should fail with context error since rate limiter has no tokens
	// and the context expires before a token is refilled (1 RPM = 1 token per 60s)
	_, err = p.Generate(ctx, testRequest())
	if err == nil {
		t.Error("expected error due to rate limit + context cancellation")
	}
}

func TestRateLimit_ConcurrentDrain(t *testing.T) {
	mp := newTestProvider(t)
	// Low rate limit
	p := middleware.WithRateLimit(10, 0)(mp)

	// Fire many concurrent requests — only 10 should succeed immediately,
	// the rest will wait for tokens. All should eventually complete because
	// the bucket refills over time.
	var wg sync.WaitGroup
	var successes atomic.Int64
	var errors atomic.Int64

	// Use a generous timeout so all can complete
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.Generate(ctx, testRequest())
			if err != nil {
				errors.Add(1)
			} else {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()

	if successes.Load() == 0 {
		t.Error("expected at least one successful request")
	}
}

// ---------------------------------------------------------------------------
// Logging Middleware Tests
// ---------------------------------------------------------------------------

func TestLogging_GenerateSuccess(t *testing.T) {
	mp := newTestProvider(t)

	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	p := middleware.WithLogging(logger)(mp)

	result, err := p.Generate(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text() != "test response" {
		t.Errorf("expected 'test response', got %q", result.Text())
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "provider request started") {
		t.Error("expected log to contain 'provider request started'")
	}
	if !strings.Contains(logOutput, "provider request completed") {
		t.Error("expected log to contain 'provider request completed'")
	}
	if !strings.Contains(logOutput, "generate") {
		t.Error("expected log to contain 'generate'")
	}
	if !strings.Contains(logOutput, "duration_ms") {
		t.Error("expected log to contain 'duration_ms'")
	}
}

func TestLogging_GenerateError(t *testing.T) {
	ep := &errProvider{
		name: "fail-log",
		err:  errors.New("something went wrong"),
	}

	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	p := middleware.WithLogging(logger)(ep)

	_, err := p.Generate(context.Background(), testRequest())
	if err == nil {
		t.Fatal("expected error")
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "provider request failed") {
		t.Error("expected log to contain 'provider request failed'")
	}
}

func TestLogging_StreamSuccess(t *testing.T) {
	mp := newTestProvider(t)

	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	p := middleware.WithLogging(logger)(mp)

	ch, err := p.Stream(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Drain the channel
	for range ch {
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "provider stream started") {
		t.Error("expected log to contain 'provider stream started'")
	}
	if !strings.Contains(logOutput, "provider stream completed") {
		t.Error("expected log to contain 'provider stream completed'")
	}
	if !strings.Contains(logOutput, "chunks") {
		t.Error("expected log to contain 'chunks'")
	}
}

func TestLogging_StreamSetupError(t *testing.T) {
	ep := &errProvider{
		name: "stream-fail",
		err:  errors.New("stream setup failed"),
	}

	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	p := middleware.WithLogging(logger)(ep)

	_, err := p.Stream(context.Background(), testRequest())
	if err == nil {
		t.Fatal("expected error")
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "provider stream setup failed") {
		t.Error("expected log to contain 'provider stream setup failed'")
	}
}

func TestLogging_NilLogger_UsesDefault(t *testing.T) {
	mp := newTestProvider(t)
	p := middleware.WithLogging(nil)(mp)

	result, err := p.Generate(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("unexpected error with nil logger: %v", err)
	}
	if result.Text() != "test response" {
		t.Errorf("expected 'test response', got %q", result.Text())
	}
}

func TestLogging_PassesThroughName(t *testing.T) {
	mp := newTestProvider(t)
	logger := slog.Default()
	p := middleware.WithLogging(logger)(mp)
	if p.Name() != "test" {
		t.Errorf("expected 'test', got %q", p.Name())
	}
}

func TestLogging_LogsTokenUsage(t *testing.T) {
	mp := newTestProvider(t)

	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	p := middleware.WithLogging(logger)(mp)

	_, err := p.Generate(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "total_tokens") {
		t.Error("expected log to contain 'total_tokens'")
	}
	if !strings.Contains(logOutput, "prompt_tokens") {
		t.Error("expected log to contain 'prompt_tokens'")
	}
}

// ---------------------------------------------------------------------------
// Caching Middleware Tests
// ---------------------------------------------------------------------------

func TestCaching_HitsCache(t *testing.T) {
	mp := newTestProvider(t)
	store := middleware.NewMemoryCacheStore()
	p := middleware.WithCaching(store, 5*time.Minute)(mp)

	// First call — should hit provider
	result1, err := p.Generate(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	// Second call with same request — should hit cache
	result2, err := p.Generate(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	// Results should be identical
	if result1.ID != result2.ID {
		t.Errorf("cached result ID mismatch: %q vs %q", result1.ID, result2.ID)
	}
	if result1.Text() != result2.Text() {
		t.Errorf("cached result text mismatch: %q vs %q", result1.Text(), result2.Text())
	}

	// Provider should have been called only once
	if mp.GenerateCallCount() != 1 {
		t.Errorf("expected 1 provider call, got %d", mp.GenerateCallCount())
	}
}

func TestCaching_DifferentRequests_CacheMiss(t *testing.T) {
	mp := newTestProvider(t)
	store := middleware.NewMemoryCacheStore()
	p := middleware.WithCaching(store, 5*time.Minute)(mp)

	req1 := provider.GenerateRequest{
		Model:    "mock-model",
		Messages: []message.Message{message.UserMessage("hello")},
	}
	req2 := provider.GenerateRequest{
		Model:    "mock-model",
		Messages: []message.Message{message.UserMessage("goodbye")},
	}

	_, err := p.Generate(context.Background(), req1)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	_, err = p.Generate(context.Background(), req2)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	// Both should hit the provider since requests differ
	if mp.GenerateCallCount() != 2 {
		t.Errorf("expected 2 provider calls for different requests, got %d", mp.GenerateCallCount())
	}
}

func TestCaching_TTLExpiration(t *testing.T) {
	mp := newTestProvider(t)
	store := middleware.NewMemoryCacheStore()
	p := middleware.WithCaching(store, 10*time.Millisecond)(mp)

	// First call
	_, err := p.Generate(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	if mp.GenerateCallCount() != 1 {
		t.Fatalf("expected 1 call after first request, got %d", mp.GenerateCallCount())
	}

	// Wait for cache to expire
	time.Sleep(30 * time.Millisecond)

	// Second call — should miss cache
	_, err = p.Generate(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	if mp.GenerateCallCount() != 2 {
		t.Errorf("expected 2 calls after cache expiration, got %d", mp.GenerateCallCount())
	}
}

func TestCaching_StreamNotCached(t *testing.T) {
	mp := newTestProvider(t)
	store := middleware.NewMemoryCacheStore()
	p := middleware.WithCaching(store, 5*time.Minute)(mp)

	// Stream call — should not be cached
	ch1, err := p.Stream(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("first stream failed: %v", err)
	}
	for range ch1 {
	}

	ch2, err := p.Stream(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("second stream failed: %v", err)
	}
	for range ch2 {
	}

	// Provider should have been called twice
	if mp.StreamCallCount() != 2 {
		t.Errorf("expected 2 stream calls (streaming is never cached), got %d", mp.StreamCallCount())
	}
}

func TestCaching_ToolCallsNotCached(t *testing.T) {
	mp := newTestProvider(t)
	// Set up a response with tool calls
	mp.SetDefaultResponse(mock.MockResponse{
		Message: message.Message{
			Role: message.RoleAssistant,
			ToolCalls: []message.ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: message.ToolCallFunction{
						Name:      "test_func",
						Arguments: `{"key": "value"}`,
					},
				},
			},
		},
		Usage:        provider.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		FinishReason: provider.FinishReasonToolCall,
	})

	store := middleware.NewMemoryCacheStore()
	p := middleware.WithCaching(store, 5*time.Minute)(mp)

	// First call — tool call should not be cached
	_, err := p.Generate(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	// Second call — should NOT hit cache since result was a tool call
	_, err = p.Generate(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	if mp.GenerateCallCount() != 2 {
		t.Errorf("expected 2 calls (tool calls should not be cached), got %d", mp.GenerateCallCount())
	}
}

func TestCaching_PassesThroughName(t *testing.T) {
	mp := newTestProvider(t)
	store := middleware.NewMemoryCacheStore()
	p := middleware.WithCaching(store, time.Minute)(mp)
	if p.Name() != "test" {
		t.Errorf("expected 'test', got %q", p.Name())
	}
}

// ---------------------------------------------------------------------------
// MemoryCacheStore Tests
// ---------------------------------------------------------------------------

func TestMemoryCacheStore_BasicOperations(t *testing.T) {
	store := middleware.NewMemoryCacheStore()
	ctx := context.Background()

	result := &provider.GenerateResult{
		ID:           "cached-123",
		Message:      message.AssistantMessage("cached"),
		Usage:        provider.TokenUsage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
		FinishReason: provider.FinishReasonStop,
		Model:        "test-model",
		CreatedAt:    time.Now(),
	}

	// Get non-existent key
	_, ok := store.Get(ctx, "nonexistent")
	if ok {
		t.Error("expected cache miss for non-existent key")
	}

	// Set and Get
	store.Set(ctx, "test-key", result, 5*time.Minute)
	cached, ok := store.Get(ctx, "test-key")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if cached.ID != "cached-123" {
		t.Errorf("expected ID 'cached-123', got %q", cached.ID)
	}

	// Delete
	store.Delete(ctx, "test-key")
	_, ok = store.Get(ctx, "test-key")
	if ok {
		t.Error("expected cache miss after delete")
	}
}

func TestMemoryCacheStore_Expiration(t *testing.T) {
	store := middleware.NewMemoryCacheStore()
	ctx := context.Background()

	result := &provider.GenerateResult{
		ID:           "expiring",
		Message:      message.AssistantMessage("expiring content"),
		FinishReason: provider.FinishReasonStop,
		CreatedAt:    time.Now(),
	}

	store.Set(ctx, "expire-key", result, 10*time.Millisecond)

	// Should be available immediately
	_, ok := store.Get(ctx, "expire-key")
	if !ok {
		t.Fatal("expected cache hit immediately after set")
	}

	// Wait for expiration
	time.Sleep(30 * time.Millisecond)

	_, ok = store.Get(ctx, "expire-key")
	if ok {
		t.Error("expected cache miss after expiration")
	}
}

func TestMemoryCacheStore_Clear(t *testing.T) {
	store := middleware.NewMemoryCacheStore()
	ctx := context.Background()

	result := &provider.GenerateResult{
		ID:           "clearable",
		Message:      message.AssistantMessage("clear me"),
		FinishReason: provider.FinishReasonStop,
		CreatedAt:    time.Now(),
	}

	// Set multiple items
	for i := 0; i < 10; i++ {
		store.Set(ctx, fmt.Sprintf("key-%d", i), result, 5*time.Minute)
	}

	// Clear all
	store.Clear(ctx)

	// Verify all are gone
	for i := 0; i < 10; i++ {
		_, ok := store.Get(ctx, fmt.Sprintf("key-%d", i))
		if ok {
			t.Errorf("expected cache miss for key-%d after clear", i)
		}
	}
}

func TestMemoryCacheStore_ConcurrentAccess(t *testing.T) {
	store := middleware.NewMemoryCacheStore()
	ctx := context.Background()

	result := &provider.GenerateResult{
		ID:           "concurrent",
		Message:      message.AssistantMessage("concurrent"),
		FinishReason: provider.FinishReasonStop,
		CreatedAt:    time.Now(),
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", idx%10)
			store.Set(ctx, key, result, 5*time.Minute)
			store.Get(ctx, key)
		}(i)
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Circuit Breaker Middleware Tests
// ---------------------------------------------------------------------------

func TestCircuitBreaker_ClosedState_PassesRequests(t *testing.T) {
	mp := newTestProvider(t)
	p := middleware.WithCircuitBreaker(5, 30*time.Second)(mp)

	result, err := p.Generate(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("unexpected error in closed state: %v", err)
	}
	if result.Text() != "test response" {
		t.Errorf("expected 'test response', got %q", result.Text())
	}
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	ep := &errProvider{
		name: "failing",
		err:  provider.NewProviderErrorWithCode("failing", "test-model", "error", 500, errors.New("fail")),
	}

	p := middleware.WithCircuitBreaker(3, 100*time.Millisecond)(ep)

	// Make requests until circuit opens
	for i := 0; i < 3; i++ {
		_, err := p.Generate(context.Background(), testRequest())
		if err == nil {
			t.Fatalf("expected error on attempt %d", i+1)
		}
	}

	// Next request should be rejected by circuit breaker
	_, err := p.Generate(context.Background(), testRequest())
	if err == nil {
		t.Fatal("expected error when circuit is open")
	}
	if !strings.Contains(err.Error(), "circuit breaker open") {
		t.Errorf("expected circuit breaker error, got: %v", err)
	}
}

func TestCircuitBreaker_HalfOpen_AfterResetTimeout(t *testing.T) {
	ep := &errProvider{
		name: "recovering",
		err:  provider.NewProviderErrorWithCode("recovering", "test-model", "error", 500, errors.New("fail")),
	}

	p := middleware.WithCircuitBreaker(2, 50*time.Millisecond)(ep)

	// Trip the circuit
	for i := 0; i < 2; i++ {
		p.Generate(context.Background(), testRequest())
	}

	// Wait for reset timeout
	time.Sleep(80 * time.Millisecond)

	// Now the circuit should be half-open, allowing one request through
	_, err := p.Generate(context.Background(), testRequest())
	// It will still fail (errProvider always fails), but the request should get through
	if err == nil {
		t.Fatal("expected error from errProvider")
	}
	// Should have been allowed through (half-open state)
	// After failure, circuit should re-open
}

func TestCircuitBreaker_ClosesOnSuccess(t *testing.T) {
	fp := &flakyProvider{
		name:         "recovering",
		succeedAfter: 2,
		result: &provider.GenerateResult{
			ID:           "recovered",
			Message:      message.AssistantMessage("ok"),
			FinishReason: provider.FinishReasonStop,
			Model:        "test-model",
			CreatedAt:    time.Now(),
		},
	}

	p := middleware.WithCircuitBreaker(2, 50*time.Millisecond)(fp)

	// Trip the circuit with 2 failures
	p.Generate(context.Background(), testRequest())
	p.Generate(context.Background(), testRequest())

	// Wait for reset timeout
	time.Sleep(80 * time.Millisecond)

	// This should succeed (flakyProvider succeeds after 2 failures)
	result, err := p.Generate(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("expected success in half-open state: %v", err)
	}
	if result.Text() != "ok" {
		t.Errorf("expected 'ok', got %q", result.Text())
	}

	// Circuit should be closed again — subsequent requests should work
	result, err = p.Generate(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("expected success after circuit closed: %v", err)
	}
	if result.Text() != "ok" {
		t.Errorf("expected 'ok', got %q", result.Text())
	}
}

func TestCircuitBreaker_StreamOpen(t *testing.T) {
	ep := &errProvider{
		name: "stream-fail",
		err:  provider.NewProviderErrorWithCode("stream-fail", "test-model", "error", 500, errors.New("fail")),
	}

	p := middleware.WithCircuitBreaker(2, 30*time.Second)(ep)

	// Trip the circuit
	for i := 0; i < 2; i++ {
		p.Generate(context.Background(), testRequest())
	}

	// Stream should also be blocked
	_, err := p.Stream(context.Background(), testRequest())
	if err == nil {
		t.Fatal("expected stream to be blocked by open circuit")
	}
	if !strings.Contains(err.Error(), "circuit breaker open") {
		t.Errorf("expected circuit breaker error, got: %v", err)
	}
}

func TestCircuitBreaker_StreamSuccess(t *testing.T) {
	mp := newTestProvider(t)
	p := middleware.WithCircuitBreaker(5, 30*time.Second)(mp)

	ch, err := p.Stream(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var hasDone bool
	for evt := range ch {
		if evt.Type == provider.StreamEventDone {
			hasDone = true
		}
	}
	if !hasDone {
		t.Error("expected stream to complete successfully")
	}
}

func TestCircuitBreaker_PassesThroughName(t *testing.T) {
	mp := newTestProvider(t)
	p := middleware.WithCircuitBreaker(5, 30*time.Second)(mp)
	if p.Name() != "test" {
		t.Errorf("expected 'test', got %q", p.Name())
	}
}

// ---------------------------------------------------------------------------
// Chain Middleware Tests
// ---------------------------------------------------------------------------

func TestChain_AppliesInOrder(t *testing.T) {
	mp := newTestProvider(t)

	var order []string

	mw1 := func(p provider.Provider) provider.Provider {
		order = append(order, "mw1")
		return p
	}
	mw2 := func(p provider.Provider) provider.Provider {
		order = append(order, "mw2")
		return p
	}
	mw3 := func(p provider.Provider) provider.Provider {
		order = append(order, "mw3")
		return p
	}

	_ = middleware.Chain(mw1, mw2, mw3)(mp)

	// First middleware should be outermost, so applied last during wrapping
	// Chain applies in reverse: mw3 → mw2 → mw1
	// So order of wrapping is: mw3 wraps mp, mw2 wraps that, mw1 wraps that
	if len(order) != 3 {
		t.Fatalf("expected 3 middleware applied, got %d", len(order))
	}

	// mw1 is outermost (applied last), mw3 is innermost (applied first)
	if order[0] != "mw3" {
		t.Errorf("expected mw3 to be applied first, got %q", order[0])
	}
	if order[2] != "mw1" {
		t.Errorf("expected mw1 to be applied last, got %q", order[2])
	}
}

func TestChain_NilMiddleware_Skipped(t *testing.T) {
	mp := newTestProvider(t)

	called := false
	mw := func(p provider.Provider) provider.Provider {
		called = true
		return p
	}

	p := middleware.Chain(nil, mw, nil)(mp)

	result, err := p.Generate(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text() != "test response" {
		t.Errorf("expected 'test response', got %q", result.Text())
	}
	if !called {
		t.Error("expected non-nil middleware to be called")
	}
}

func TestChain_EmptyChain_ReturnsOriginal(t *testing.T) {
	mp := newTestProvider(t)
	p := middleware.Chain()(mp)

	result, err := p.Generate(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text() != "test response" {
		t.Errorf("expected 'test response', got %q", result.Text())
	}
}

func TestChain_FullStack(t *testing.T) {
	mp := newTestProvider(t)

	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	p := middleware.Chain(
		middleware.WithLogging(logger),
		middleware.WithCircuitBreaker(5, 30*time.Second),
		middleware.WithRetry(3, middleware.ConstantBackoff(time.Millisecond)),
		middleware.WithRateLimit(100, 0),
	)(mp)

	result, err := p.Generate(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("unexpected error with full middleware stack: %v", err)
	}
	if result.Text() != "test response" {
		t.Errorf("expected 'test response', got %q", result.Text())
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "provider request started") {
		t.Error("expected logging middleware to produce output")
	}
}

// ---------------------------------------------------------------------------
// Integration / Concurrency Tests
// ---------------------------------------------------------------------------

func TestConcurrentAccess_RetryProvider(t *testing.T) {
	mp := newTestProvider(t)
	p := middleware.WithRetry(3, middleware.ConstantBackoff(time.Millisecond))(mp)

	var wg sync.WaitGroup
	errors := make(chan error, 50)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.Generate(context.Background(), testRequest())
			if err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent request failed: %v", err)
	}
}

func TestConcurrentAccess_CircuitBreaker(t *testing.T) {
	mp := newTestProvider(t)
	p := middleware.WithCircuitBreaker(100, 30*time.Second)(mp)

	var wg sync.WaitGroup
	errors := make(chan error, 50)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.Generate(context.Background(), testRequest())
			if err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent request failed: %v", err)
	}
}

func TestConcurrentAccess_CachingProvider(t *testing.T) {
	mp := newTestProvider(t)
	store := middleware.NewMemoryCacheStore()
	p := middleware.WithCaching(store, 5*time.Minute)(mp)

	var wg sync.WaitGroup
	errors := make(chan error, 50)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.Generate(context.Background(), testRequest())
			if err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent request failed: %v", err)
	}

	// Most requests should have hit the cache
	if mp.GenerateCallCount() > 10 {
		t.Errorf("expected most requests to be cached, but provider was called %d times", mp.GenerateCallCount())
	}
}

// ---------------------------------------------------------------------------
// Edge Case Tests
// ---------------------------------------------------------------------------

func TestRetry_WithProviderError_StatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		retryable  bool
	}{
		{"500 is retryable", 500, true},
		{"502 is retryable", 502, true},
		{"503 is retryable", 503, true},
		{"429 is retryable", 429, true},
		{"408 is retryable", 408, true},
		{"400 not retryable", 400, false},
		{"401 not retryable", 401, false},
		{"403 not retryable", 403, false},
		{"404 not retryable", 404, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ep := &errProvider{
				name: "status-test",
				err:  provider.NewProviderErrorWithCode("status-test", "m", "err", tt.statusCode, errors.New("fail")),
			}

			p := middleware.WithRetry(2, middleware.ConstantBackoff(time.Millisecond))(ep)

			_, _ = p.Generate(context.Background(), testRequest())

			expectedCalls := int64(1) // First call always happens
			if tt.retryable {
				expectedCalls = 3 // 1 + 2 retries
			}

			if ep.calls.Load() != expectedCalls {
				t.Errorf("status %d: expected %d calls, got %d",
					tt.statusCode, expectedCalls, ep.calls.Load())
			}
		})
	}
}

func TestCircuitBreaker_Stats(t *testing.T) {
	ep := &errProvider{
		name: "stats-test",
		err:  provider.NewProviderErrorWithCode("stats-test", "m", "err", 500, errors.New("fail")),
	}

	cb := middleware.WithCircuitBreaker(2, 30*time.Second)(ep)

	// Type assert to get the circuit breaker for stats
	type statsProvider interface {
		Stats() middleware.CircuitBreakerStats
	}

	cbP, ok := cb.(statsProvider)
	if !ok {
		t.Fatal("circuit breaker provider should implement Stats()")
	}

	stats := cbP.Stats()
	if stats.State != middleware.CircuitClosed {
		t.Errorf("expected initial state Closed, got %v", stats.State)
	}

	// Trip the circuit
	cb.Generate(context.Background(), testRequest())
	cb.Generate(context.Background(), testRequest())

	stats = cbP.Stats()
	if stats.State != middleware.CircuitOpen {
		t.Errorf("expected state Open after threshold failures, got %v", stats.State)
	}
	if stats.ConsecFailures != 2 {
		t.Errorf("expected 2 consecutive failures, got %d", stats.ConsecFailures)
	}
}
