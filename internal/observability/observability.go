// Package observability provides structured logging, distributed tracing, and
// metrics for Orchestra using OpenTelemetry.
//
// This package integrates with the existing config system and provides a
// one-stop initialization function (Setup) that configures all observability
// signals based on the application configuration.
//
// # Quick Start
//
//	cfg := config.DefaultConfig()
//	cfg.Observability.Tracing.Enabled = true
//	cfg.Observability.Metrics.Enabled = true
//
//	otel, cleanup, err := observability.Setup(context.Background(), cfg)
//	if err != nil { /* handle */ }
//	defer cleanup()
//
//	// Use otel.Tracer("my-component") to create spans
//	// Use otel.Meter("my-component") to create instruments
//
// # No-Op Mode
//
// When tracing and metrics are disabled (the default), all operations return
// no-op implementations that discard data. This ensures zero overhead when
// observability is not needed.
package observability

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	config "github.com/user/orchestra/internal/config"
)

// Orchestra holds initialized observability providers.
// All fields are safe for concurrent use and are non-nil even when
// the corresponding signal is disabled (no-op implementations are used).
type Orchestra struct {
	logger   *slog.Logger
	config   config.Config
	mu       sync.RWMutex
	tracer   *TracerProvider
	meter    *MeterProvider
	health   *HealthChecker
	setupErr error
}

// Setup initializes all observability signals based on the provided configuration.
// It returns the Orchestra instance, a cleanup function, and any setup error.
// The cleanup function must be called when the application shuts down to flush
// pending telemetry data.
//
// If setup encounters an error (e.g., invalid endpoint), it returns a non-nil
// Orchestra with no-op providers and the error. This allows the application to
// continue running without observability rather than crashing.
func Setup(ctx context.Context, cfg config.Config) (*Orchestra, func(context.Context) error, error) {
	orch := &Orchestra{
		config: cfg,
	}

	// Create cleanup function
	var cleanups []func(context.Context) error
	cleanup := func(ctx context.Context) error {
		var firstErr error
		for idx := len(cleanups) - 1; idx >= 0; idx-- {
			if err := cleanups[idx](ctx); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return firstErr
	}

	// Setup structured logging first so other components can use it
	orch.logger = setupLogger(cfg.Logging)

	// Setup tracing
	tp, tpCleanup, err := setupTracerProvider(ctx, cfg.Observability.Tracing, orch.logger)
	if err != nil {
		orch.logger.Warn("tracing setup failed, using no-op provider",
			slog.String("error", err.Error()),
		)
		orch.setupErr = err
		// Continue with no-op tracer
		tp, tpCleanup = newNoopTracerProvider(), func(context.Context) error { return nil }
	}
	orch.tracer = tp
	cleanups = append(cleanups, tpCleanup)

	// Setup metrics
	mp, mpCleanup, err := setupMeterProvider(ctx, cfg.Observability.Metrics, orch.logger)
	if err != nil {
		orch.logger.Warn("metrics setup failed, using no-op provider",
			slog.String("error", err.Error()),
		)
		orch.setupErr = err
		mp, mpCleanup = newNoopMeterProvider(), func(context.Context) error { return nil }
	}
	orch.meter = mp
	cleanups = append(cleanups, mpCleanup)

	// Setup health checker
	orch.health = newHealthChecker(orch.logger)

	orch.logger.Info("observability initialized",
		slog.Bool("tracing_enabled", cfg.Observability.Tracing.Enabled),
		slog.Bool("metrics_enabled", cfg.Observability.Metrics.Enabled),
		slog.String("service_name", cfg.Observability.Tracing.GetServiceName()),
	)

	return orch, cleanup, orch.setupErr
}

// Logger returns the configured structured logger.
func (o *Orchestra) Logger() *slog.Logger {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.logger
}

// SetLogger updates the structured logger.
func (o *Orchestra) SetLogger(l *slog.Logger) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.logger = l
}

// Tracer returns the tracer provider for creating trace spans.
func (o *Orchestra) Tracer() *TracerProvider {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.tracer
}

// Meter returns the meter provider for creating metric instruments.
func (o *Orchestra) Meter() *MeterProvider {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.meter
}

// Health returns the health checker for readiness/liveness probes.
func (o *Orchestra) Health() *HealthChecker {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.health
}

// Config returns a copy of the observability configuration.
func (o *Orchestra) Config() config.Config {
	return o.config
}

// IsReady returns true if all configured observability systems are operational.
func (o *Orchestra) IsReady() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if o.tracer == nil || o.meter == nil {
		return false
	}
	return o.health.IsReady()
}

// SetupError returns any non-fatal error encountered during setup.
func (o *Orchestra) SetupError() error {
	return o.setupErr
}

// ComponentLogger creates a logger with component context for a subsystem.
// This is the recommended way to create loggers for different parts of
// Orchestra (agents, workflows, providers, tools).
func (o *Orchestra) ComponentLogger(component string) *slog.Logger {
	return o.logger.With(slog.String("component", component))
}

// setupLogger creates a slog.Logger based on the logging configuration.
func setupLogger(cfg config.LoggingConfig) *slog.Logger {
	level := parseLogLevel(cfg.GetLevel())
	handler := createHandler(cfg, level)
	return slog.New(handler)
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func createHandler(cfg config.LoggingConfig, level slog.Level) slog.Handler {
	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: cfg.AddSource,
	}

	switch cfg.GetFormat() {
	case "text":
		return slog.NewTextHandler(createWriter(cfg), opts)
	default:
		return slog.NewJSONHandler(createWriter(cfg), opts)
	}
}

func createWriter(cfg config.LoggingConfig) *lockedWriter {
	return &lockedWriter{}
}

// lockedWriter is a thread-safe writer that writes to stderr.
type lockedWriter struct {
	mu sync.Mutex
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := fmt.Fprintf(stderrWriter, "%s", p)
	if err != nil {
		return n, fmt.Errorf("writing to stderr: %w", err)
	}
	return n, nil
}

// No-op Orchestra for when observability is completely disabled.
var noopOrchestra = &Orchestra{
	logger: slog.Default(),
	tracer: newNoopTracerProvider(),
	meter:  newNoopMeterProvider(),
	health: newHealthChecker(slog.Default()),
}

// Noop returns a fully no-op Orchestra instance that discards all telemetry.
// Useful for tests and when observability is explicitly disabled.
func Noop() *Orchestra {
	return noopOrchestra
}

// ComponentLogger is a convenience function that creates a component logger
// from the global slog.Default(). Use when you don't have access to an
// Orchestra instance.
func ComponentLogger(component string) *slog.Logger {
	return slog.Default().With(slog.String("component", component))
}
