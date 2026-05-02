package observability

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	config "github.com/user/orchestra/internal/config"
)

// setupMeterProvider creates a new MeterProvider based on the configuration.
func setupMeterProvider(
	ctx context.Context,
	cfg config.MetricsConfig,
	logger *slog.Logger,
) (*MeterProvider, func(context.Context) error, error) {
	internalCfg := MetricsConfig{
		Enabled:        cfg.Enabled,
		Endpoint:       cfg.GetEndpoint(),
		ServiceName:    cfg.GetServiceName(),
		Namespace:      cfg.GetNamespace(),
		ExportInterval: cfg.GetExportInterval(),
	}

	mp := &MeterProvider{
		meters:  make(map[string]*Meter),
		logger:  logger.With(slog.String("component", "metrics")),
		enabled: cfg.Enabled,
		config:  internalCfg,
		registry: &metricRegistry{
			counters:   make(map[string]*Counter),
			histograms: make(map[string]*Histogram),
			gauges:     make(map[string]*Gauge),
		},
	}

	if !cfg.Enabled {
		mp.logger.Info("metrics disabled, using no-op provider")
		return mp, func(context.Context) error { return nil }, nil
	}

	mp.logger.Info("metrics enabled",
		slog.String("endpoint", internalCfg.Endpoint),
		slog.String("service_name", internalCfg.ServiceName),
		slog.String("namespace", internalCfg.Namespace),
		slog.String("export_interval", internalCfg.ExportInterval.String()),
	)

	cleanup := func(ctx context.Context) error {
		mp.logger.Info("shutting down meter provider")
		return mp.Shutdown(ctx)
	}

	return mp, cleanup, nil
}

// MetricsConfig holds internal metrics configuration.
type MetricsConfig struct {
	Enabled        bool
	Endpoint       string
	ServiceName    string
	Namespace      string
	ExportInterval time.Duration
}

// MeterProvider creates and manages Meters.
type MeterProvider struct {
	mu       sync.RWMutex
	meters   map[string]*Meter
	logger   *slog.Logger
	enabled  bool
	config   MetricsConfig
	registry *metricRegistry
}

// Meter creates metric instruments for a specific component.
type Meter struct {
	name     string
	logger   *slog.Logger
	registry *metricRegistry
}

// metricRegistry is the central store for all metric data.
type metricRegistry struct {
	mu         sync.RWMutex
	counters   map[string]*Counter
	histograms map[string]*Histogram
	gauges     map[string]*Gauge
}

// newNoopMeterProvider creates a meter provider that discards all metrics.
func newNoopMeterProvider() *MeterProvider {
	return &MeterProvider{
		meters:  make(map[string]*Meter),
		logger:  slog.Default(),
		enabled: false,
		registry: &metricRegistry{
			counters:   make(map[string]*Counter),
			histograms: make(map[string]*Histogram),
			gauges:     make(map[string]*Gauge),
		},
	}
}

// Meter returns a named meter for creating instruments.
func (mp *MeterProvider) Meter(name string) *Meter {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if m, ok := mp.meters[name]; ok {
		return m
	}

	m := &Meter{
		name:     name,
		logger:   mp.logger.With(slog.String("meter", name)),
		registry: mp.registry,
	}
	mp.meters[name] = m
	return m
}

// Shutdown shuts down the meter provider.
func (mp *MeterProvider) Shutdown(ctx context.Context) error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	mp.enabled = false
	mp.meters = make(map[string]*Meter)
	return nil
}

// ForceFlush flushes all pending metrics.
func (mp *MeterProvider) ForceFlush(ctx context.Context) error {
	return nil
}

// IsEnabled returns whether metrics collection is active.
func (mp *MeterProvider) IsEnabled() bool {
	return mp.enabled
}

// GetAllMetrics returns a snapshot of all collected metrics.
// This is useful for Prometheus-style scraping and testing.
func (mp *MeterProvider) GetAllMetrics() MetricsSnapshot {
	mp.registry.mu.RLock()
	defer mp.registry.mu.RUnlock()

	snapshot := MetricsSnapshot{
		Counters:   make(map[string]CounterSnapshot, len(mp.registry.counters)),
		Histograms: make(map[string]HistogramSnapshot, len(mp.registry.histograms)),
		Gauges:     make(map[string]GaugeSnapshot, len(mp.registry.gauges)),
	}

	for name, counter := range mp.registry.counters {
		snapshot.Counters[name] = CounterSnapshot{
			Name:   counter.name,
			Value:  counter.value.Load(),
			Labels: counter.labels,
		}
	}

	for name, hist := range mp.registry.histograms {
		hist.mu.Lock()
		hs := HistogramSnapshot{
			Name:    hist.name,
			Count:   len(hist.values),
			Sum:     hist.sum,
			Min:     hist.min,
			Max:     hist.max,
			Labels:  hist.labels,
			Buckets: make(map[float64]int64),
		}
		for b, count := range hist.buckets {
			hs.Buckets[b] = count
		}
		hist.mu.Unlock()
		snapshot.Histograms[name] = hs
	}

	for name, gauge := range mp.registry.gauges {
		snapshot.Gauges[name] = GaugeSnapshot{
			Name:   gauge.name,
			Value:  gauge.value.Load(),
			Labels: gauge.labels,
		}
	}

	return snapshot
}

// MetricsSnapshot is a point-in-time snapshot of all metrics.
type MetricsSnapshot struct {
	Counters   map[string]CounterSnapshot
	Histograms map[string]HistogramSnapshot
	Gauges     map[string]GaugeSnapshot
}

// CounterSnapshot is a snapshot of a counter metric.
type CounterSnapshot struct {
	Name   string
	Value  int64
	Labels map[string]string
}

// HistogramSnapshot is a snapshot of a histogram metric.
type HistogramSnapshot struct {
	Name    string
	Count   int
	Sum     float64
	Min     float64
	Max     float64
	Labels  map[string]string
	Buckets map[float64]int64
}

// GaugeSnapshot is a snapshot of a gauge metric.
type GaugeSnapshot struct {
	Name   string
	Value  int64
	Labels map[string]string
}

// ---------------------------------------------------------------------------
// Counter
// ---------------------------------------------------------------------------

// Counter is a monotonically increasing counter metric.
// Use counters for request counts, error counts, token usage, etc.
type Counter struct {
	name   string
	value  atomic.Int64
	labels map[string]string
}

// CounterOption configures a counter.
type CounterOption func(*Counter)

// WithCounterLabels sets static labels on a counter.
func WithCounterLabels(labels map[string]string) CounterOption {
	return func(counter *Counter) {
		counter.labels = labels
	}
}

// Int64Counter creates a new counter that tracks an int64 value.
func (m *Meter) Int64Counter(name string, opts ...CounterOption) *Counter {
	m.registry.mu.Lock()
	defer m.registry.mu.Unlock()

	if existing, ok := m.registry.counters[name]; ok {
		return existing
	}

	counter := &Counter{
		name:   name,
		labels: make(map[string]string),
	}
	for _, opt := range opts {
		opt(counter)
	}
	m.registry.counters[name] = counter
	return counter
}

// Inc increments the counter by 1.
func (c *Counter) Inc() {
	c.Add(1)
}

// Add increments the counter by the given amount. The value must be non-negative.
func (c *Counter) Add(delta int64) {
	c.value.Add(delta)
}

// Value returns the current counter value.
func (c *Counter) Value() int64 {
	return c.value.Load()
}

// Name returns the counter name.
func (c *Counter) Name() string {
	return c.name
}

// ---------------------------------------------------------------------------
// Histogram
// ---------------------------------------------------------------------------

// Histogram tracks the distribution of values (latency, size, etc.).
// It records individual observations and computes count, sum, min, max,
// and configurable bucket-based percentiles.
type Histogram struct {
	name    string
	mu      sync.Mutex
	values  []float64
	sum     float64
	min     float64
	max     float64
	buckets map[float64]int64
	labels  map[string]string
}

// HistogramOption configures a histogram.
type HistogramOption func(*Histogram)

// WithHistogramLabels sets static labels on a histogram.
func WithHistogramLabels(labels map[string]string) HistogramOption {
	return func(hist *Histogram) {
		hist.labels = labels
	}
}

// WithBuckets sets custom bucket boundaries for the histogram.
func WithBuckets(boundaries []float64) HistogramOption {
	return func(hist *Histogram) {
		hist.buckets = make(map[float64]int64, len(boundaries))
		for _, b := range boundaries {
			hist.buckets[b] = 0
		}
	}
}

// DefaultLatencyBuckets returns bucket boundaries suitable for measuring
// request latency in seconds.
func DefaultLatencyBuckets() []float64 {
	return []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
}

// Float64Histogram creates a new histogram that tracks float64 observations.
func (m *Meter) Float64Histogram(name string, opts ...HistogramOption) *Histogram {
	m.registry.mu.Lock()
	defer m.registry.mu.Unlock()

	if existing, ok := m.registry.histograms[name]; ok {
		return existing
	}

	hist := &Histogram{
		name:    name,
		buckets: make(map[float64]int64),
		labels:  make(map[string]string),
	}

	// Apply default buckets if none specified
	defaultBuckets := DefaultLatencyBuckets()
	for _, b := range defaultBuckets {
		hist.buckets[b] = 0
	}

	for _, opt := range opts {
		opt(hist)
	}

	m.registry.histograms[name] = hist
	return hist
}

// Record records a new observation.
func (h *Histogram) Record(value float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.values = append(h.values, value)
	h.sum += value

	if len(h.values) == 1 || value < h.min {
		h.min = value
	}
	if value > h.max {
		h.max = value
	}

	// Update buckets
	for boundary := range h.buckets {
		if value <= boundary {
			h.buckets[boundary]++
		}
	}
}

// RecordDuration records a duration as a float64 value (in seconds by default).
func (h *Histogram) RecordDuration(d time.Duration) {
	h.Record(d.Seconds())
}

// Count returns the number of recorded observations.
func (h *Histogram) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.values)
}

// Sum returns the sum of all recorded observations.
func (h *Histogram) Sum() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sum
}

// Min returns the minimum recorded observation.
func (h *Histogram) Min() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.min
}

// Max returns the maximum recorded observation.
func (h *Histogram) Max() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.max
}

// Mean returns the average of all recorded observations.
func (h *Histogram) Mean() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.values) == 0 {
		return 0
	}
	return h.sum / float64(len(h.values))
}

// Percentile returns the value at the given percentile (0-100).
func (h *Histogram) Percentile(p float64) float64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.values) == 0 {
		return 0
	}

	// Simple percentile calculation
	idx := int(float64(len(h.values)) * p / 100.0)
	if idx >= len(h.values) {
		idx = len(h.values) - 1
	}
	return h.values[idx]
}

// Name returns the histogram name.
func (h *Histogram) Name() string {
	return h.name
}

// ---------------------------------------------------------------------------
// Gauge
// ---------------------------------------------------------------------------

// Gauge tracks a value that can go up or down (active connections,
// in-flight requests, etc.).
type Gauge struct {
	name   string
	value  atomic.Int64
	labels map[string]string
}

// GaugeOption configures a gauge.
type GaugeOption func(*Gauge)

// WithGaugeLabels sets static labels on a gauge.
func WithGaugeLabels(labels map[string]string) GaugeOption {
	return func(gauge *Gauge) {
		gauge.labels = labels
	}
}

// Int64Gauge creates a new gauge that tracks an int64 value.
func (m *Meter) Int64Gauge(name string, opts ...GaugeOption) *Gauge {
	m.registry.mu.Lock()
	defer m.registry.mu.Unlock()

	if existing, ok := m.registry.gauges[name]; ok {
		return existing
	}

	gauge := &Gauge{
		name:   name,
		labels: make(map[string]string),
	}
	for _, opt := range opts {
		opt(gauge)
	}
	m.registry.gauges[name] = gauge
	return gauge
}

// Set sets the gauge to the given value.
func (g *Gauge) Set(value int64) {
	g.value.Store(value)
}

// Inc increments the gauge by 1.
func (g *Gauge) Inc() {
	g.value.Add(1)
}

// Dec decrements the gauge by 1.
func (g *Gauge) Dec() {
	g.value.Add(-1)
}

// Add increments the gauge by the given delta (can be negative).
func (g *Gauge) Add(delta int64) {
	g.value.Add(delta)
}

// Value returns the current gauge value.
func (g *Gauge) Value() int64 {
	return g.value.Load()
}

// Name returns the gauge name.
func (g *Gauge) Name() string {
	return g.name
}

// ---------------------------------------------------------------------------
// Orchestra-specific metric names
// ---------------------------------------------------------------------------

const (
	// MetricProviderRequestsTotal counts provider API requests.
	MetricProviderRequestsTotal = "orchestra_provider_requests_total"

	// MetricProviderLatencySeconds tracks provider request latency.
	MetricProviderLatencySeconds = "orchestra_provider_latency_seconds"

	// MetricTokensTotal tracks token usage.
	MetricTokensTotal = "orchestra_tokens_total"

	// MetricToolExecutionsTotal counts tool executions.
	MetricToolExecutionsTotal = "orchestra_tool_executions_total"

	// MetricToolLatencySeconds tracks tool execution latency.
	MetricToolLatencySeconds = "orchestra_tool_latency_seconds"

	// MetricActiveAgents tracks currently running agents.
	MetricActiveAgents = "orchestra_active_agents"

	// MetricActiveWorkflows tracks currently running workflows.
	MetricActiveWorkflows = "orchestra_active_workflows"

	// MetricWorkflowDurationSeconds tracks workflow execution time.
	MetricWorkflowDurationSeconds = "orchestra_workflow_duration_seconds"

	// MetricAgentTurnsTotal counts agent execution turns.
	MetricAgentTurnsTotal = "orchestra_agent_turns_total"
)

// OrchestraMetrics provides pre-built metric instruments for Orchestra
// components. It's a convenience wrapper that creates the standard set
// of metrics with the correct names and labels.
type OrchestraMetrics struct {
	// Provider metrics
	ProviderRequests *Counter
	ProviderLatency  *Histogram

	// Token metrics
	TokensTotal *Counter

	// Tool metrics
	ToolExecutions *Counter
	ToolLatency    *Histogram

	// Agent metrics
	ActiveAgents    *Gauge
	ActiveWorkflows *Gauge
	AgentTurns      *Counter

	// Workflow metrics
	WorkflowDuration *Histogram
}

// NewOrchestraMetrics creates the standard set of Orchestra metrics
// using the given meter. This is the recommended way to create metrics
// for Orchestra components.
func NewOrchestraMetrics(m *Meter) *OrchestraMetrics {
	return &OrchestraMetrics{
		ProviderRequests: m.Int64Counter(MetricProviderRequestsTotal),
		ProviderLatency:  m.Float64Histogram(MetricProviderLatencySeconds),
		TokensTotal:      m.Int64Counter(MetricTokensTotal),
		ToolExecutions:   m.Int64Counter(MetricToolExecutionsTotal),
		ToolLatency:      m.Float64Histogram(MetricToolLatencySeconds),
		ActiveAgents:     m.Int64Gauge(MetricActiveAgents),
		ActiveWorkflows:  m.Int64Gauge(MetricActiveWorkflows),
		AgentTurns:       m.Int64Counter(MetricAgentTurnsTotal),
		WorkflowDuration: m.Float64Histogram(MetricWorkflowDurationSeconds),
	}
}
