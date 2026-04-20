// Package middleware provides cross-cutting provider decorators for the Orchestra
// framework. Each middleware wraps a provider.Provider and adds behavior such as
// retry logic, rate limiting, logging, caching, and circuit breaking.
//
// Middleware uses the decorator pattern. Multiple middleware can be composed:
//
//	p := baseProvider
//	p = middleware.WithRetry(3, middleware.ExponentialBackoff)(p)
//	p = middleware.WithRateLimit(60, 150000)(p)
//	p = middleware.WithLogging(logger)(p)
//	p = middleware.WithCircuitBreaker(5, 30*time.Second)(p)
package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/user/orchestra/internal/provider"
)

// ProviderMiddleware is a function that wraps a Provider with additional behavior.
// It takes a Provider and returns a new Provider that decorates the original.
type ProviderMiddleware func(provider.Provider) provider.Provider

// ---------------------------------------------------------------------------
// Retry Middleware
// ---------------------------------------------------------------------------

// BackoffStrategy calculates the delay between retry attempts.
type BackoffStrategy interface {
	// Delay returns the duration to wait before the given attempt (0-indexed).
	Delay(attempt int) time.Duration
}

// ExponentialBackoff implements BackoffStrategy with exponential delays and jitter.
type ExponentialBackoff struct {
	// Initial is the base delay for the first retry.
	Initial time.Duration
	// Max is the maximum delay between retries.
	Max time.Duration
	// Multiplier is the factor by which the delay increases each attempt.
	// Defaults to 2.0 if zero.
	Multiplier float64
	// Jitter adds randomness to avoid thundering herd. When true, the actual
	// delay is a random value between 0 and the computed delay.
	Jitter bool
}

// Delay returns the exponential backoff duration for the given attempt.
func (b ExponentialBackoff) Delay(attempt int) time.Duration {
	if attempt < 0 {
		return b.Initial
	}

	multiplier := b.Multiplier
	if multiplier <= 0 {
		multiplier = 2.0
	}

	initial := b.Initial
	if initial <= 0 {
		initial = time.Second
	}

	// Calculate exponential delay: initial * multiplier^attempt
	delay := float64(initial)
	for i := 0; i < attempt; i++ {
		delay *= multiplier
	}

	// Apply max cap
	if maxDelay := float64(b.Max); maxDelay > 0 && delay > maxDelay {
		delay = maxDelay
	}

	d := time.Duration(delay)

	// Apply jitter: randomize between 0 and d
	if b.Jitter && d > 0 {
		d = time.Duration(rand.Int64N(int64(d)))
	}

	return d
}

// ConstantBackoff implements BackoffStrategy with a fixed delay.
type ConstantBackoff time.Duration

// Delay returns the constant backoff duration.
func (b ConstantBackoff) Delay(_ int) time.Duration {
	return time.Duration(b)
}

// WithRetry returns a middleware that retries failed requests up to maxAttempts
// times with the given backoff strategy. A maxAttempts of 0 means no retries.
// Requests are only retried on retryable errors (transient failures).
func WithRetry(maxAttempts int, backoff BackoffStrategy) ProviderMiddleware {
	if backoff == nil {
		backoff = ExponentialBackoff{
			Initial:    time.Second,
			Max:        30 * time.Second,
			Multiplier: 2.0,
			Jitter:     true,
		}
	}
	return func(p provider.Provider) provider.Provider {
		return &retryProvider{
			inner:       p,
			maxAttempts: maxAttempts,
			backoff:     backoff,
		}
	}
}

// retryProvider wraps a Provider with retry logic.
type retryProvider struct {
	inner       provider.Provider
	maxAttempts int
	backoff     BackoffStrategy
}

func (r *retryProvider) Name() string                                        { return r.inner.Name() }
func (r *retryProvider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	return r.inner.Models(ctx)
}
func (r *retryProvider) Capabilities(model string) provider.ModelCapabilities {
	return r.inner.Capabilities(model)
}

func (r *retryProvider) Generate(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResult, error) {
	var lastErr error
	for attempt := 0; attempt <= r.maxAttempts; attempt++ {
		if attempt > 0 {
			delay := r.backoff.Delay(attempt - 1)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		result, err := r.inner.Generate(ctx, req)
		if err == nil {
			return result, nil
		}

		lastErr = err
		if !isRetryableError(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("retry exhausted after %d attempts: %w", r.maxAttempts+1, lastErr)
}

func (r *retryProvider) Stream(ctx context.Context, req provider.GenerateRequest) (<-chan provider.StreamEvent, error) {
	var lastErr error
	for attempt := 0; attempt <= r.maxAttempts; attempt++ {
		if attempt > 0 {
			delay := r.backoff.Delay(attempt - 1)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		ch, err := r.inner.Stream(ctx, req)
		if err == nil {
			return ch, nil
		}

		lastErr = err
		if !isRetryableError(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("retry exhausted after %d attempts: %w", r.maxAttempts+1, lastErr)
}

// isRetryableError determines if an error is transient and worth retrying.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for context cancellation — never retry
	if ctx := context.Cause(context.Background()); ctx != nil {
		// If the error is a context error, don't retry
		if err == context.Canceled || err == context.DeadlineExceeded {
			return false
		}
	}

	// Check for ProviderError with retryable status codes
	var pErr *provider.ProviderError
	if asProviderError(err, &pErr) {
		switch {
		case pErr.StatusCode >= 500:
			return true // Server errors are retryable
		case pErr.StatusCode == 429:
			return true // Rate limit — retryable (backoff will handle delay)
		case pErr.StatusCode == 408:
			return true // Request timeout
		case pErr.StatusCode == 409:
			return true // Conflict
		}
		return false
	}

	// For generic errors, assume retryable for connection/timeout issues
	return true
}

// asProviderError checks if err or its wrapped chain is a *provider.ProviderError.
func asProviderError(err error, target **provider.ProviderError) bool {
	for e := err; e != nil; {
		if pe, ok := e.(*provider.ProviderError); ok {
			*target = pe
			return true
		}
		if unwrapper, ok := e.(interface{ Unwrap() error }); ok {
			e = unwrapper.Unwrap()
		} else {
			break
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Rate Limit Middleware
// ---------------------------------------------------------------------------

// WithRateLimit returns a middleware that limits the rate of requests using
// a token bucket algorithm. rpm is requests per minute; tpm is tokens per minute.
// A value of 0 for either means unlimited for that dimension.
func WithRateLimit(rpm, tpm int) ProviderMiddleware {
	return func(p provider.Provider) provider.Provider {
		var limiter *rateLimiter
		if rpm > 0 || tpm > 0 {
			limiter = newRateLimiter(rpm, tpm)
		}
		return &rateLimitProvider{
			inner:   p,
			limiter: limiter,
		}
	}
}

// rateLimitProvider wraps a Provider with rate limiting.
type rateLimitProvider struct {
	inner   provider.Provider
	limiter *rateLimiter
}

func (r *rateLimitProvider) Name() string { return r.inner.Name() }
func (r *rateLimitProvider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	return r.inner.Models(ctx)
}
func (r *rateLimitProvider) Capabilities(model string) provider.ModelCapabilities {
	return r.inner.Capabilities(model)
}

func (r *rateLimitProvider) Generate(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResult, error) {
	if r.limiter != nil {
		if err := r.limiter.wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limit exceeded: %w", err)
		}
	}
	return r.inner.Generate(ctx, req)
}

func (r *rateLimitProvider) Stream(ctx context.Context, req provider.GenerateRequest) (<-chan provider.StreamEvent, error) {
	if r.limiter != nil {
		if err := r.limiter.wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limit exceeded: %w", err)
		}
	}
	return r.inner.Stream(ctx, req)
}

// rateLimiter implements a simple token bucket rate limiter.
type rateLimiter struct {
	mu           sync.Mutex
	rpm          int
	tpm          int
	tokens       float64
	maxTokens    float64
	refillRate   float64 // tokens per nanosecond
	lastRefill   time.Time
	tpmTokens    float64
	tpmMaxTokens float64
	tpmRefill    float64 // tokens per nanosecond
	tpmLastRefill time.Time
}

func newRateLimiter(rpm, tpm int) *rateLimiter {
	now := time.Now()
	rl := &rateLimiter{
		rpm:          rpm,
		tpm:          tpm,
		lastRefill:   now,
		tpmLastRefill: now,
	}

	if rpm > 0 {
		rl.maxTokens = float64(rpm)
		rl.tokens = float64(rpm)
		rl.refillRate = float64(rpm) / float64(time.Minute)
	}

	if tpm > 0 {
		rl.tpmMaxTokens = float64(tpm)
		rl.tpmTokens = float64(tpm)
		rl.tpmRefill = float64(tpm) / float64(time.Minute)
	}

	return rl
}

// wait blocks until a token is available or the context is cancelled.
func (r *rateLimiter) wait(ctx context.Context) error {
	for {
		r.mu.Lock()
		delay := r.calculateDelay()
		r.mu.Unlock()

		if delay == 0 {
			r.mu.Lock()
			r.consume()
			r.mu.Unlock()
			return nil
		}

		select {
		case <-time.After(delay):
			// Continue and try again
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// calculateDelay returns how long to wait for the next token (0 if available).
// Must be called with mu held.
func (r *rateLimiter) calculateDelay() time.Duration {
	now := time.Now()
	var requestDelay time.Duration

	if r.rpm > 0 {
		elapsed := now.Sub(r.lastRefill)
		r.tokens += float64(elapsed) * r.refillRate
		if r.tokens > r.maxTokens {
			r.tokens = r.maxTokens
		}
		r.lastRefill = now

		if r.tokens < 1 {
			needed := 1 - r.tokens
			requestDelay = time.Duration(needed / r.refillRate)
		}
	}

	if r.tpm > 0 {
		elapsed := now.Sub(r.tpmLastRefill)
		r.tpmTokens += float64(elapsed) * r.tpmRefill
		if r.tpmTokens > r.tpmMaxTokens {
			r.tpmTokens = r.tpmMaxTokens
		}
		r.tpmLastRefill = now

		if r.tpmTokens < 1 {
			needed := 1 - r.tpmTokens
			tpmDelay := time.Duration(needed / r.tpmRefill)
			if tpmDelay > requestDelay {
				requestDelay = tpmDelay
			}
		}
	}

	return requestDelay
}

// consume deducts one token from each bucket.
// Must be called with mu held.
func (r *rateLimiter) consume() {
	if r.rpm > 0 {
		r.tokens -= 1
	}
	if r.tpm > 0 {
		r.tpmTokens -= 1
	}
}

// ---------------------------------------------------------------------------
// Logging Middleware
// ---------------------------------------------------------------------------

// WithLogging returns a middleware that logs each Generate and Stream call
// with structured logging via slog. Request details, duration, token usage,
// and errors are logged.
func WithLogging(logger *slog.Logger) ProviderMiddleware {
	return func(p provider.Provider) provider.Provider {
		if logger == nil {
			logger = slog.Default()
		}
		return &loggingProvider{
			inner:  p,
			logger: logger,
		}
	}
}

// loggingProvider wraps a Provider with structured logging.
type loggingProvider struct {
	inner  provider.Provider
	logger *slog.Logger
}

func (l *loggingProvider) Name() string { return l.inner.Name() }
func (l *loggingProvider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	return l.inner.Models(ctx)
}
func (l *loggingProvider) Capabilities(model string) provider.ModelCapabilities {
	return l.inner.Capabilities(model)
}

func (l *loggingProvider) Generate(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResult, error) {
	start := time.Now()
	logger := l.logger.With(
		"provider", l.inner.Name(),
		"model", req.Model,
		"operation", "generate",
		"message_count", len(req.Messages),
	)

	logger.Info("provider request started")

	result, err := l.inner.Generate(ctx, req)
	duration := time.Since(start)

	if err != nil {
		logger.Error("provider request failed",
			"duration_ms", duration.Milliseconds(),
			"error", err,
		)
		return nil, err
	}

	attrs := []any{
		"duration_ms", duration.Milliseconds(),
		"finish_reason", result.FinishReason,
		"result_id", result.ID,
	}
	if result.Usage.TotalTokens > 0 {
		attrs = append(attrs,
			"prompt_tokens", result.Usage.PromptTokens,
			"completion_tokens", result.Usage.CompletionTokens,
			"total_tokens", result.Usage.TotalTokens,
		)
	}

	logger.Info("provider request completed", attrs...)
	return result, nil
}

func (l *loggingProvider) Stream(ctx context.Context, req provider.GenerateRequest) (<-chan provider.StreamEvent, error) {
	start := time.Now()
	logger := l.logger.With(
		"provider", l.inner.Name(),
		"model", req.Model,
		"operation", "stream",
		"message_count", len(req.Messages),
	)

	logger.Info("provider stream started")

	innerCh, err := l.inner.Stream(ctx, req)
	if err != nil {
		duration := time.Since(start)
		logger.Error("provider stream setup failed",
			"duration_ms", duration.Milliseconds(),
			"error", err,
		)
		return nil, err
	}

	// Wrap the channel to log events
	outCh := make(chan provider.StreamEvent, cap(innerCh)+1)
	go func() {
		defer close(outCh)
		chunkCount := 0
		toolCallCount := 0
		var usage *provider.TokenUsage

		for evt := range innerCh {
			switch evt.Type {
			case provider.StreamEventChunk:
				chunkCount++
			case provider.StreamEventToolCall:
				toolCallCount++
			case provider.StreamEventDone:
				usage = evt.Usage
			case provider.StreamEventError:
				logger.Error("provider stream error",
					"error", evt.Error,
				)
			}

			select {
			case outCh <- evt:
			case <-ctx.Done():
				logger.Warn("provider stream cancelled")
				return
			}
		}

		duration := time.Since(start)
		attrs := []any{
			"duration_ms", duration.Milliseconds(),
			"chunks", chunkCount,
			"tool_calls", toolCallCount,
		}
		if usage != nil {
			attrs = append(attrs,
				"prompt_tokens", usage.PromptTokens,
				"completion_tokens", usage.CompletionTokens,
				"total_tokens", usage.TotalTokens,
			)
		}
		logger.Info("provider stream completed", attrs...)
	}()

	return outCh, nil
}

// ---------------------------------------------------------------------------
// Caching Middleware
// ---------------------------------------------------------------------------

// CacheStore is the interface for caching Generate results.
type CacheStore interface {
	// Get retrieves a cached result by key. Returns nil if not found.
	Get(ctx context.Context, key string) (*provider.GenerateResult, bool)
	// Set stores a result with the given TTL.
	Set(ctx context.Context, key string, result *provider.GenerateResult, ttl time.Duration)
	// Delete removes a cached result.
	Delete(ctx context.Context, key string)
	// Clear removes all cached results.
	Clear(ctx context.Context)
}

// MemoryCacheStore implements CacheStore with an in-memory map.
type MemoryCacheStore struct {
	mu    sync.RWMutex
	items map[string]*cacheEntry
}

type cacheEntry struct {
	result    *provider.GenerateResult
	expiresAt time.Time
}

// NewMemoryCacheStore creates a new in-memory cache store.
func NewMemoryCacheStore() *MemoryCacheStore {
	return &MemoryCacheStore{
		items: make(map[string]*cacheEntry),
	}
}

func (m *MemoryCacheStore) Get(_ context.Context, key string) (*provider.GenerateResult, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, ok := m.items[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.result, true
}

func (m *MemoryCacheStore) Set(_ context.Context, key string, result *provider.GenerateResult, ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.items[key] = &cacheEntry{
		result:    result,
		expiresAt: time.Now().Add(ttl),
	}
}

func (m *MemoryCacheStore) Delete(_ context.Context, key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, key)
}

func (m *MemoryCacheStore) Clear(_ context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = make(map[string]*cacheEntry)
}

// WithCaching returns a middleware that caches Generate results using the
// provided cache store. Stream calls are never cached.
// The TTL specifies how long cached results are valid.
func WithCaching(store CacheStore, ttl time.Duration) ProviderMiddleware {
	return func(p provider.Provider) provider.Provider {
		return &cachingProvider{
			inner: p,
			store: store,
			ttl:   ttl,
		}
	}
}

// cachingProvider wraps a Provider with result caching.
type cachingProvider struct {
	inner provider.Provider
	store CacheStore
	ttl   time.Duration
}

func (c *cachingProvider) Name() string { return c.inner.Name() }
func (c *cachingProvider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	return c.inner.Models(ctx)
}
func (c *cachingProvider) Capabilities(model string) provider.ModelCapabilities {
	return c.inner.Capabilities(model)
}

func (c *cachingProvider) Generate(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResult, error) {
	// Only cache non-streaming, non-tool-result requests
	cacheKey := cacheKeyForRequest(c.inner.Name(), req)

	if cached, ok := c.store.Get(ctx, cacheKey); ok {
		return cached, nil
	}

	result, err := c.inner.Generate(ctx, req)
	if err != nil {
		return nil, err
	}

	// Cache the result (don't cache tool calls since they need execution)
	if !result.IsToolCall() {
		c.store.Set(ctx, cacheKey, result, c.ttl)
	}

	return result, nil
}

func (c *cachingProvider) Stream(ctx context.Context, req provider.GenerateRequest) (<-chan provider.StreamEvent, error) {
	// Streaming is never cached — pass through directly
	return c.inner.Stream(ctx, req)
}

// cacheKeyForRequest generates a deterministic cache key from a request.
func cacheKeyForRequest(providerName string, req provider.GenerateRequest) string {
	h := sha256.New()
	h.Write([]byte(providerName))
	h.Write([]byte(req.Model))

	// Hash messages
	for _, msg := range req.Messages {
		h.Write([]byte(string(msg.Role)))
		h.Write([]byte(msg.Text()))
		for _, tc := range msg.ToolCalls {
			h.Write([]byte(tc.ID))
			h.Write([]byte(tc.Function.Name))
			h.Write([]byte(tc.Function.Arguments))
		}
	}

	// Hash tools
	for _, tool := range req.Tools {
		h.Write([]byte(tool.Function.Name))
	}

	// Hash options (deterministic serialization)
	opts, _ := json.Marshal(map[string]interface{}{
		"temperature":   req.Options.Temperature,
		"top_p":         req.Options.TopP,
		"max_tokens":    req.Options.MaxTokens,
		"stop":          req.Options.StopSequences,
		"seed":          req.Options.Seed,
		"response_fmt":  req.Options.ResponseFormat,
	})
	h.Write(opts)

	return fmt.Sprintf("%x", h.Sum(nil))
}

// ---------------------------------------------------------------------------
// Circuit Breaker Middleware
// ---------------------------------------------------------------------------

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	// CircuitClosed means requests flow through normally.
	CircuitClosed CircuitState = iota
	// CircuitOpen means requests are rejected immediately.
	CircuitOpen
	// CircuitHalfOpen means a limited number of requests are allowed to test recovery.
	CircuitHalfOpen
)

// String returns the string representation of the circuit state.
func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerStats holds current circuit breaker statistics.
type CircuitBreakerStats struct {
	State           CircuitState
	Failures        int
	Successes       int
	ConsecFailures  int
	LastFailure     time.Time
	LastStateChange time.Time
	TotalRejected   int64
}

// WithCircuitBreaker returns a middleware that implements the circuit breaker pattern.
// If threshold consecutive failures occur, the circuit opens and rejects requests
// for resetTimeout duration. After that, it enters half-open state, allowing one
// request through to test if the provider has recovered.
func WithCircuitBreaker(threshold int, resetTimeout time.Duration) ProviderMiddleware {
	return func(p provider.Provider) provider.Provider {
		return &circuitBreakerProvider{
			inner:        p,
			threshold:    threshold,
			resetTimeout: resetTimeout,
			state:        CircuitClosed,
			stateChanged: time.Now(),
		}
	}
}

// circuitBreakerProvider wraps a Provider with circuit breaker logic.
type circuitBreakerProvider struct {
	inner        provider.Provider
	threshold    int
	resetTimeout time.Duration

	mu           sync.Mutex
	state        CircuitState
	stateChanged time.Time
	consecFails  int
	totalRejected int64
	lastFailure  time.Time
}

func (cb *circuitBreakerProvider) Name() string { return cb.inner.Name() }
func (cb *circuitBreakerProvider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	return cb.inner.Models(ctx)
}
func (cb *circuitBreakerProvider) Capabilities(model string) provider.ModelCapabilities {
	return cb.inner.Capabilities(model)
}

func (cb *circuitBreakerProvider) Generate(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResult, error) {
	if err := cb.beforeRequest(); err != nil {
		return nil, err
	}

	result, err := cb.inner.Generate(ctx, req)
	cb.afterRequest(err)
	return result, err
}

func (cb *circuitBreakerProvider) Stream(ctx context.Context, req provider.GenerateRequest) (<-chan provider.StreamEvent, error) {
	if err := cb.beforeRequest(); err != nil {
		return nil, err
	}

	ch, err := cb.inner.Stream(ctx, req)
	if err != nil {
		cb.recordFailure()
		return nil, err
	}

	// Wrap the channel to track stream success/failure
	outCh := make(chan provider.StreamEvent, cap(ch)+1)
	go func() {
		defer close(outCh)
		hadError := false
		for evt := range ch {
			if evt.Type == provider.StreamEventError {
				hadError = true
			}
			select {
			case outCh <- evt:
			case <-ctx.Done():
				return
			}
		}
		if hadError {
			cb.recordFailure()
		} else {
			cb.recordSuccess()
		}
	}()

	return outCh, nil
}

// beforeRequest checks if the circuit allows the request.
func (cb *circuitBreakerProvider) beforeRequest() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return nil

	case CircuitOpen:
		if time.Since(cb.stateChanged) > cb.resetTimeout {
			cb.state = CircuitHalfOpen
			cb.stateChanged = time.Now()
			return nil
		}
		cb.totalRejected++
		return fmt.Errorf("circuit breaker open for provider %q: rejecting request (consecutive failures: %d)",
			cb.inner.Name(), cb.consecFails)

	case CircuitHalfOpen:
		// Allow one probe request through
		return nil

	default:
		return nil
	}
}

// afterRequest records the result of a request and updates circuit state.
func (cb *circuitBreakerProvider) afterRequest(err error) {
	if err != nil {
		cb.recordFailure()
	} else {
		cb.recordSuccess()
	}
}

// recordFailure records a failed request. Must NOT be called with mu held
// from afterRequest, but CAN be called from wrapped stream goroutine.
func (cb *circuitBreakerProvider) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecFails++
	cb.lastFailure = time.Now()

	switch cb.state {
	case CircuitClosed:
		if cb.consecFails >= cb.threshold {
			cb.state = CircuitOpen
			cb.stateChanged = time.Now()
		}
	case CircuitHalfOpen:
		// Probe failed — reopen circuit
		cb.state = CircuitOpen
		cb.stateChanged = time.Now()
	}
}

// recordSuccess records a successful request. Must NOT be called with mu held.
func (cb *circuitBreakerProvider) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecFails = 0

	if cb.state == CircuitHalfOpen {
		// Probe succeeded — close circuit
		cb.state = CircuitClosed
		cb.stateChanged = time.Now()
	}
}

// Stats returns the current circuit breaker statistics.
func (cb *circuitBreakerProvider) Stats() CircuitBreakerStats {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	return CircuitBreakerStats{
		State:           cb.state,
		ConsecFailures:  cb.consecFails,
		LastFailure:     cb.lastFailure,
		LastStateChange: cb.stateChanged,
		TotalRejected:   cb.totalRejected,
	}
}

// ---------------------------------------------------------------------------
// Chain Helper
// ---------------------------------------------------------------------------

// Chain composes multiple middleware into a single ProviderMiddleware.
// Middleware are applied in order: the first in the list is the outermost wrapper.
//
// Example:
//
//	p := Chain(
//	    WithLogging(logger),
//	    WithCircuitBreaker(5, 30*time.Second),
//	    WithRetry(3, nil),
//	    WithRateLimit(60, 0),
//	)(baseProvider)
//
// In this example, the call order is: Logging → CircuitBreaker → Retry → RateLimit → BaseProvider
func Chain(middlewares ...ProviderMiddleware) ProviderMiddleware {
	return func(p provider.Provider) provider.Provider {
		// Apply in reverse so the first middleware is the outermost
		for i := len(middlewares) - 1; i >= 0; i-- {
			if middlewares[i] != nil {
				p = middlewares[i](p)
			}
		}
		return p
	}
}
