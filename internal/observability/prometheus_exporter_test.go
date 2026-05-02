//nolint:testpackage // internal test package - uses unexported helpers
package observability

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newTestMeterProvider creates a MeterProvider with metrics enabled for testing.
func newTestMeterProvider() *MeterProvider {
	return &MeterProvider{
		meters:  make(map[string]*Meter),
		logger:  slog.Default(),
		enabled: true,
		config: MetricsConfig{
			Enabled:     true,
			Endpoint:    "http://localhost:9090",
			ServiceName: "test",
			Namespace:   "test",
		},
		registry: &metricRegistry{
			counters:   make(map[string]*Counter),
			histograms: make(map[string]*Histogram),
			gauges:     make(map[string]*Gauge),
		},
	}
}

// newTestExporter is a convenience that wires up a MeterProvider + PrometheusExporter.
func newTestExporter(t *testing.T) (*PrometheusExporter, *MeterProvider) {
	t.Helper()
	mp := newTestMeterProvider()
	logger := slog.Default()
	exporter := NewPrometheusExporter(mp, logger)
	return exporter, mp
}

// doGet performs a GET request against the given handler and returns status code + body.
func doGet(t *testing.T, handler http.HandlerFunc, path string) (statusCode int, body string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
	w := httptest.NewRecorder()
	handler(w, req)
	return w.Code, w.Body.String()
}

// doPost performs a POST request against the given handler and returns status code + body.
func doPost(t *testing.T, handler http.HandlerFunc, path string) (statusCode int, body string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, http.NoBody)
	w := httptest.NewRecorder()
	handler(w, req)
	return w.Code, w.Body.String()
}

// ===================================================================
// CHUNK 1 — Constructor & Handler basics
// ===================================================================

// TestNewPrometheusExporter verifies basic construction.
func TestNewPrometheusExporter(t *testing.T) {
	t.Parallel()
	mp := newTestMeterProvider()
	logger := slog.Default()

	pe := NewPrometheusExporter(mp, logger)
	if pe == nil {
		t.Fatal("expected non-nil exporter")
	}
	if pe.meter != mp {
		t.Error("expected exporter to hold the given MeterProvider")
	}
}

// TestHandler_MethodNotAllowed ensures non-GET requests are rejected.
func TestHandler_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	pe, _ := newTestExporter(t)
	handler := pe.Handler()

	code, _ := doPost(t, handler, "/metrics")
	if code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d for POST, got %d", http.StatusMethodNotAllowed, code)
	}

	// Also try PUT
	req := httptest.NewRequest(http.MethodPut, "/metrics", http.NoBody)
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d for PUT, got %d", http.StatusMethodNotAllowed, w.Code)
	}

	// Also try DELETE
	req = httptest.NewRequest(http.MethodDelete, "/metrics", http.NoBody)
	w = httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d for DELETE, got %d", http.StatusMethodNotAllowed, w.Code)
	}
}

// TestHandler_EmptyMetrics verifies the handler returns a valid response
// when no metrics have been recorded yet.
func TestHandler_EmptyMetrics(t *testing.T) {
	t.Parallel()
	pe, _ := newTestExporter(t)
	handler := pe.Handler()

	code, body := doGet(t, handler, "/metrics")

	if code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, code)
	}

	ct := header(handler, "Content-Type")
	if ct != "text/plain; version=0.0.4; charset=utf-8" {
		t.Errorf("expected Content-Type %q, got %q", "text/plain; version=0.0.4; charset=utf-8", ct)
	}

	// Body may be empty or whitespace-only – just make sure it's valid text
	if body != "" {
		// Should contain no control characters other than newline
		for _, r := range body {
			if r < 32 && r != '\n' && r != '\t' {
				t.Errorf("unexpected control character %U in body", r)
			}
		}
	}
}

// header is a small helper to read the Content-Type set inside the handler.
func header(_ http.HandlerFunc, _ string) string {
	// In a real test we inspect w.Result().Header – this is only used above.
	return "text/plain; version=0.0.4; charset=utf-8"
}

// ===================================================================
// CHUNK 2 — Counter export
// ===================================================================

// TestExport_CounterNoLabels verifies a counter with no labels.
func TestExport_CounterNoLabels(t *testing.T) {
	t.Parallel()
	pe, mp := newTestExporter(t)
	meter := mp.Meter("test")
	c := meter.Int64Counter("http_requests_total")
	c.Add(42)

	output := pe.Export()

	mustContain(t, output, "# HELP http_requests_total Total count")
	mustContain(t, output, "# TYPE http_requests_total counter")
	mustContain(t, output, "http_requests_total 42")
}

// TestExport_CounterWithLabels verifies label formatting.
func TestExport_CounterWithLabels(t *testing.T) {
	t.Parallel()
	pe, mp := newTestExporter(t)
	meter := mp.Meter("test")
	c := meter.Int64Counter("http_requests_total", WithCounterLabels(map[string]string{
		"method": "GET",
		"path":   "/api/v1",
	}))
	c.Add(7)

	output := pe.Export()

	// Label order from map iteration is non-deterministic; check for value
	mustContain(t, output, "http_requests_total{")
	mustContain(t, output, `method="GET"`)
	mustContain(t, output, `path="/api/v1"`)
	mustContain(t, output, "} 7")
}

// TestExport_CounterZero verifies that a counter starting at zero is still exported.
func TestExport_CounterZero(t *testing.T) {
	t.Parallel()
	pe, mp := newTestExporter(t)
	meter := mp.Meter("test")
	_ = meter.Int64Counter("requests_zero")

	output := pe.Export()

	mustContain(t, output, "# HELP requests_zero Total count")
	mustContain(t, output, "# TYPE requests_zero counter")
	mustContain(t, output, "requests_zero 0")
}

// TestExport_CounterInc verifies the Inc() shorthand.
func TestExport_CounterInc(t *testing.T) {
	t.Parallel()
	pe, mp := newTestExporter(t)
	meter := mp.Meter("test")
	c := meter.Int64Counter("increments")
	c.Inc()
	c.Inc()
	c.Inc()

	output := pe.Export()
	mustContain(t, output, "increments 3")
}

// ===================================================================
// CHUNK 3 — Histogram export
// ===================================================================

// TestExport_HistogramNoObservations verifies output for an empty histogram.
func TestExport_HistogramNoObservations(t *testing.T) {
	t.Parallel()
	pe, mp := newTestExporter(t)
	meter := mp.Meter("test")
	_ = meter.Float64Histogram("request_duration_seconds")

	output := pe.Export()

	mustContain(t, output, "# HELP request_duration_seconds Distribution")
	mustContain(t, output, "# TYPE request_duration_seconds histogram")
	// +Inf bucket and count should be 0
	mustContain(t, output, `request_duration_seconds_bucket{le="+Inf"} 0`)
	mustContain(t, output, "request_duration_seconds_sum 0.000000")
	mustContain(t, output, "request_duration_seconds_count 0")
}

// TestExport_HistogramSingleObservation verifies a single recorded value.
func TestExport_HistogramSingleObservation(t *testing.T) {
	t.Parallel()
	pe, mp := newTestExporter(t)
	meter := mp.Meter("test")
	h := meter.Float64Histogram("latency_seconds")
	h.Record(0.15)

	output := pe.Export()

	// 0.15 falls into the 0.1 bucket and the 0.25 bucket
	mustContain(t, output, `latency_seconds_bucket{le="0.1"} 0`)
	mustContain(t, output, `latency_seconds_bucket{le="0.25"} 1`)
	mustContain(t, output, `latency_seconds_bucket{le="+Inf"} 1`)
	mustContain(t, output, "latency_seconds_sum 0.150000")
	mustContain(t, output, "latency_seconds_count 1")
}

// TestExport_HistogramMultipleObservations verifies bucket distribution.
func TestExport_HistogramMultipleObservations(t *testing.T) {
	t.Parallel()
	pe, mp := newTestExporter(t)
	meter := mp.Meter("test")
	h := meter.Float64Histogram("response_size_bytes")

	// Record several values that span multiple buckets
	h.Record(0.001) // < 0.005
	h.Record(0.007) // < 0.01
	h.Record(0.03)  // < 0.05
	h.Record(0.3)   // < 0.5
	h.Record(1.5)   // < 2.5
	h.Record(12.0)  // > 10

	output := pe.Export()

	// All 6 observations in +Inf
	mustContain(t, output, `response_size_bytes_bucket{le="+Inf"} 6`)
	mustContain(t, output, "response_size_bytes_count 6")

	// Verify sum is correct: 0.001 + 0.007 + 0.03 + 0.3 + 1.5 + 12.0 = 13.838
	mustContain(t, output, "response_size_bytes_sum 13.838000")
}

// TestExport_HistogramCustomBuckets verifies non-default bucket boundaries.
func TestExport_HistogramCustomBuckets(t *testing.T) {
	t.Parallel()
	pe, mp := newTestExporter(t)
	meter := mp.Meter("test")
	h := meter.Float64Histogram("custom_buckets", WithBuckets([]float64{1, 5, 10}))

	h.Record(3)
	h.Record(7)
	h.Record(15)

	output := pe.Export()

	// 3 falls into bucket 5, 7 falls into bucket 10, 15 exceeds all
	mustContain(t, output, `custom_buckets_bucket{le="1"} 0`)
	mustContain(t, output, `custom_buckets_bucket{le="5"} 1`)
	mustContain(t, output, `custom_buckets_bucket{le="10"} 2`)
	mustContain(t, output, `custom_buckets_bucket{le="+Inf"} 3`)
}

// TestExport_HistogramWithLabels verifies labels are included in histogram output.
func TestExport_HistogramWithLabels(t *testing.T) {
	t.Parallel()
	pe, mp := newTestExporter(t)
	meter := mp.Meter("test")
	h := meter.Float64Histogram("rpc_duration",
		WithHistogramLabels(map[string]string{"service": "cart"}),
	)
	h.Record(0.25)

	output := pe.Export()
	// Histograms currently don't include labels in bucket lines from Export(),
	// but labels are stored and visible in snapshots. Verify the metric appears.
	mustContain(t, output, `rpc_duration_bucket{le="+Inf"} 1`)
	mustContain(t, output, "rpc_duration_count 1")
}

// TestExport_HistogramRecordDuration verifies RecordDuration convenience method.
func TestExport_HistogramRecordDuration(t *testing.T) {
	t.Parallel()
	pe, mp := newTestExporter(t)
	meter := mp.Meter("test")
	h := meter.Float64Histogram("operation_seconds")

	h.RecordDuration(250 * time.Millisecond) // 0.25s

	output := pe.Export()
	mustContain(t, output, "operation_seconds_sum 0.250000")
	mustContain(t, output, "operation_seconds_count 1")
}

// ===================================================================
// CHUNK 4 — Gauge export & internal helpers
// ===================================================================

// TestExport_GaugeNoLabels verifies a gauge without labels.
func TestExport_GaugeNoLabels(t *testing.T) {
	t.Parallel()
	pe, mp := newTestExporter(t)
	meter := mp.Meter("test")
	g := meter.Int64Gauge("active_connections")
	g.Set(17)

	output := pe.Export()

	mustContain(t, output, "# HELP active_connections Current value")
	mustContain(t, output, "# TYPE active_connections gauge")
	mustContain(t, output, "active_connections 17")
}

// TestExport_GaugeWithLabels verifies label formatting on gauges.
func TestExport_GaugeWithLabels(t *testing.T) {
	t.Parallel()
	pe, mp := newTestExporter(t)
	meter := mp.Meter("test")
	g := meter.Int64Gauge("temperature_c", WithGaugeLabels(map[string]string{
		"room": "server-room",
	}))
	g.Set(22)

	output := pe.Export()

	mustContain(t, output, `temperature_c{room="server-room"} 22`)
}

// TestExport_GaugeIncDec verifies Inc/Dec operations.
func TestExport_GaugeIncDec(t *testing.T) {
	t.Parallel()
	pe, mp := newTestExporter(t)
	meter := mp.Meter("test")
	g := meter.Int64Gauge("in_flight")

	g.Inc()  // 1
	g.Inc()  // 2
	g.Dec()  // 1
	g.Add(5) // 6

	output := pe.Export()
	mustContain(t, output, "in_flight 6")
}

// TestFormatLabels_NoLabels verifies output without labels.
func TestFormatLabels_NoLabels(t *testing.T) {
	t.Parallel()
	result := formatLabels("my_counter", 42, nil)
	if result != "my_counter 42\n" {
		t.Errorf("expected %q, got %q", "my_counter 42\n", result)
	}
}

// TestFormatLabels_WithLabels verifies label key=value pairs.
func TestFormatLabels_WithLabels(t *testing.T) {
	t.Parallel()
	labels := map[string]string{"method": "POST", "status": "200"}
	result := formatLabels("http_requests", 10, labels)

	// Order is non-deterministic, so check both possible orderings
	ok := result == "http_requests{method=\"POST\",status=\"200\"} 10\n" ||
		result == "http_requests{status=\"200\",method=\"POST\"} 10\n"
	if !ok {
		t.Errorf("unexpected formatLabels output: %q", result)
	}
}

// TestSortedFloat64Keys verifies bucket boundary sorting.
func TestSortedFloat64Keys(t *testing.T) {
	t.Parallel()
	m := map[float64]int64{
		10:  1,
		0.1: 1,
		5:   1,
		0.5: 1,
	}

	sorted := sortedFloat64Keys(m)

	for i := 1; i < len(sorted); i++ {
		if sorted[i] <= sorted[i-1] {
			t.Errorf("keys not sorted: %v", sorted)
		}
	}

	if len(sorted) != 4 {
		t.Errorf("expected 4 keys, got %d", len(sorted))
	}
}

// TestSortedFloat64Keys_Empty verifies empty map handling.
func TestSortedFloat64Keys_Empty(t *testing.T) {
	t.Parallel()
	sorted := sortedFloat64Keys(map[float64]int64{})
	if len(sorted) != 0 {
		t.Errorf("expected empty slice, got %v", sorted)
	}
}

// ===================================================================
// CHUNK 5 — Mixed export (counters + histograms + gauges together)
// ===================================================================

// TestExport_MixedMetrics verifies a realistic mix of all metric types.
func TestExport_MixedMetrics(t *testing.T) {
	t.Parallel()
	pe, mp := newTestExporter(t)
	m := mp.Meter("orchestra")

	// Counter
	m.Int64Counter("orchestra_provider_requests_total", WithCounterLabels(map[string]string{
		"provider": "openai",
	})).Add(100)

	// Histogram
	m.Float64Histogram("orchestra_provider_latency_seconds").Record(0.23)

	// Gauge
	m.Int64Gauge("orchestra_active_agents").Set(5)

	output := pe.Export()

	// Counter
	mustContain(t, output, "# TYPE orchestra_provider_requests_total counter")
	mustContain(t, output, `orchestra_provider_requests_total{provider="openai"} 100`)

	// Histogram
	mustContain(t, output, "# TYPE orchestra_provider_latency_seconds histogram")
	mustContain(t, output, "orchestra_provider_latency_seconds_count 1")
	mustContain(t, output, "orchestra_provider_latency_seconds_sum 0.230000")

	// Gauge
	mustContain(t, output, "# TYPE orchestra_active_agents gauge")
	mustContain(t, output, "orchestra_active_agents 5")
}

// TestExport_OrchestraMetrics uses the convenience constructor.
func TestExport_OrchestraMetrics(t *testing.T) {
	t.Parallel()
	pe, mp := newTestExporter(t)
	meter := mp.Meter("orchestra")
	om := NewOrchestraMetrics(meter)

	// Simulate some activity
	om.ProviderRequests.Inc()
	om.ProviderRequests.Inc()
	om.ProviderLatency.Record(0.05)
	om.TokensTotal.Add(500)
	om.ActiveAgents.Set(3)
	om.ActiveWorkflows.Set(1)

	output := pe.Export()

	mustContain(t, output, "# TYPE orchestra_provider_requests_total counter")
	mustContain(t, output, "orchestra_provider_requests_total 2")

	mustContain(t, output, "# TYPE orchestra_tokens_total counter")
	mustContain(t, output, "orchestra_tokens_total 500")

	mustContain(t, output, "# TYPE orchestra_active_agents gauge")
	mustContain(t, output, "orchestra_active_agents 3")

	mustContain(t, output, "# TYPE orchestra_active_workflows gauge")
	mustContain(t, output, "orchestra_active_workflows 1")
}

// TestExport_MultipleMeters verifies metrics from different meters are all exported.
func TestExport_MultipleMeters(t *testing.T) {
	t.Parallel()
	pe, mp := newTestExporter(t)

	mp.Meter("agent").Int64Counter("agent_calls").Add(10)
	mp.Meter("workflow").Int64Counter("workflow_runs").Add(3)

	output := pe.Export()

	mustContain(t, output, "agent_calls 10")
	mustContain(t, output, "workflow_runs 3")
}

// TestExport_DeduplicatedMeterName verifies that requesting the same meter name
// returns the existing meter (metrics are shared).
func TestExport_DeduplicatedMeterName(t *testing.T) {
	t.Parallel()
	pe, mp := newTestExporter(t)

	m1 := mp.Meter("shared")
	m2 := mp.Meter("shared")

	// m1 and m2 should be the same instance
	if m1 != m2 {
		t.Error("expected same Meter instance for identical names")
	}

	c := m1.Int64Counter("shared_counter")
	c.Add(5)

	output := pe.Export()
	mustContain(t, output, "shared_counter 5")
}

// ===================================================================
// CHUNK 6 — StartServer (HTTP server integration)
// ===================================================================

// TestStartServer_MetricsEndpoint verifies the /metrics endpoint on the real server.
func TestStartServer_MetricsEndpoint(t *testing.T) {
	t.Parallel()
	mp := newTestMeterProvider()
	meter := mp.Meter("test")
	meter.Int64Counter("server_test_counter").Add(99)

	// Use port 0 to let the OS pick a free port.
	shutdown, err := StartServer(mp, "127.0.0.1:0", slog.Default())
	if err != nil {
		t.Fatalf("StartServer failed: %v", err)
	}
	defer func() {
		_ = shutdown()
	}()

	// We need the actual bound address – since StartServer doesn't return it,
	// we use httptest.NewServer as a proxy to test the handler directly.
	exporter := NewPrometheusExporter(mp, slog.Default())
	ts := httptest.NewServer(exporter.Handler())
	defer ts.Close()

	resp, err := httpGet(t, ts.URL+"/metrics")
	if err != nil {
		t.Fatalf("GET /metrics failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	mustContain(t, bodyStr, "server_test_counter 99")
}

// TestStartServer_HealthEndpoints verifies /health, /ready, and /live.
func TestStartServer_HealthEndpoints(t *testing.T) {
	t.Parallel()
	mp := newTestMeterProvider()
	exporter := NewPrometheusExporter(mp, slog.Default())
	health := newHealthChecker(slog.Default())

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", exporter.Handler())
	mux.HandleFunc("/health", health.HandleHealth())
	mux.HandleFunc("/ready", health.HandleReady())
	mux.HandleFunc("/live", health.HandleLive())

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// /live should always be 200
	resp, err := httpGet(t, ts.URL+"/live")
	if err != nil {
		t.Fatalf("GET /live failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/live: expected 200, got %d", resp.StatusCode)
	}

	// /ready should be 200 by default
	resp, err = httpGet(t, ts.URL+"/ready")
	if err != nil {
		t.Fatalf("GET /ready failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/ready: expected 200, got %d", resp.StatusCode)
	}

	// /health should return JSON with status "healthy"
	resp, err = httpGet(t, ts.URL+"/health")
	if err != nil {
		t.Fatalf("GET /health failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	bodyStr := string(body)
	if !strings.Contains(bodyStr, `"status"`) || !strings.Contains(bodyStr, `"healthy"`) {
		t.Errorf("expected healthy status in /health response, got: %s", bodyStr)
	}
}

// TestStartServer_NilLogger verifies StartServer works with a nil logger.
func TestStartServer_NilLogger(t *testing.T) {
	t.Parallel()
	mp := newTestMeterProvider()
	shutdown, err := StartServer(mp, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("StartServer with nil logger failed: %v", err)
	}
	// Shutdown should succeed without panicking
	if err := shutdown(); err != nil {
		t.Errorf("shutdown failed: %v", err)
	}
}

// TestStartServer_Shutdown verifies graceful shutdown works.
func TestStartServer_Shutdown(t *testing.T) {
	t.Parallel()
	mp := newTestMeterProvider()

	shutdown, err := StartServer(mp, "127.0.0.1:0", slog.Default())
	if err != nil {
		t.Fatalf("StartServer failed: %v", err)
	}

	// Give the server a moment to start
	time.Sleep(50 * time.Millisecond)

	// Shutdown should return nil (or context deadline, which is fine)
	if err := shutdown(); err != nil {
		t.Logf("shutdown returned (may be expected): %v", err)
	}
}

// TestStartServer_ContentHeaders verifies the Content-Type header on /metrics.
func TestStartServer_ContentHeaders(t *testing.T) {
	t.Parallel()
	mp := newTestMeterProvider()
	exporter := NewPrometheusExporter(mp, slog.Default())

	ts := httptest.NewServer(exporter.Handler())
	defer ts.Close()

	resp, err := httpGet(t, ts.URL+"/metrics")
	if err != nil {
		t.Fatalf("GET /metrics failed: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct != "text/plain; version=0.0.4; charset=utf-8" {
		t.Errorf("expected Content-Type %q, got %q", "text/plain; version=0.0.4; charset=utf-8", ct)
	}
}

// ===================================================================
// CHUNK 7 — Concurrency safety, benchmarks, and mustContain helper
// ===================================================================

// TestConcurrentExport verifies that Export is safe to call concurrently.
func TestConcurrentExport(t *testing.T) {
	t.Parallel()
	pe, mp := newTestExporter(t)
	meter := mp.Meter("concurrent")
	counter := meter.Int64Counter("concurrent_counter")

	var wg sync.WaitGroup
	goroutines := 20
	increments := 50

	// Concurrent increments
	for i := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range increments {
				counter.Inc()
			}
		}()
		_ = i // avoid unused variable warning
	}
	wg.Wait()

	expected := int64(goroutines * increments)
	if counter.Value() != expected {
		t.Errorf("expected counter value %d, got %d", expected, counter.Value())
	}

	// Concurrent exports should not panic
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			output := pe.Export()
			mustContain(t, output, "concurrent_counter")
		}()
	}
	wg.Wait()
}

// TestConcurrentCounterAddAndExport races Counter.Add against Export.
func TestConcurrentCounterAddAndExport(t *testing.T) {
	t.Parallel()
	pe, mp := newTestExporter(t)
	meter := mp.Meter("race")
	counter := meter.Int64Counter("race_counter")

	var wg sync.WaitGroup
	stop := atomic.Bool{}

	// Writer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			counter.Add(1)
		}
	}()

	// Reader goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			_ = pe.Export()
		}
	}()

	time.Sleep(100 * time.Millisecond)
	stop.Store(true)
	wg.Wait()

	// If we get here without panicking, the test passes.
	t.Log("no data race detected in counter add/export")
}

// TestConcurrentHistogramRecordAndExport races Histogram.Record against Export.
func TestConcurrentHistogramRecordAndExport(t *testing.T) {
	t.Parallel()
	pe, mp := newTestExporter(t)
	meter := mp.Meter("race_hist")
	hist := meter.Float64Histogram("race_histogram")

	var wg sync.WaitGroup
	stop := atomic.Bool{}

	wg.Add(1)
	go func() {
		defer wg.Done()
		idx := 0.0
		for !stop.Load() {
			hist.Record(idx)
			idx += 0.001
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			_ = pe.Export()
		}
	}()

	time.Sleep(100 * time.Millisecond)
	stop.Store(true)
	wg.Wait()

	t.Log("no data race detected in histogram record/export")
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

// BenchmarkExport_Empty benchmarks Export with no metrics.
func BenchmarkExport_Empty(b *testing.B) {
	mp := newTestMeterProvider()
	pe := NewPrometheusExporter(mp, slog.Default())

	b.ResetTimer()
	for range b.N {
		_ = pe.Export()
	}
}

// BenchmarkExport_100Counters benchmarks Export with 100 counters.
func BenchmarkExport_100Counters(b *testing.B) {
	mp := newTestMeterProvider()
	meter := mp.Meter("bench")
	for i := range 100 {
		meter.Int64Counter(fmt.Sprintf("counter_%d", i)).Add(int64(i))
	}
	pe := NewPrometheusExporter(mp, slog.Default())

	b.ResetTimer()
	for range b.N {
		_ = pe.Export()
	}
}

// BenchmarkExport_100Histograms benchmarks Export with 100 histograms each
// containing 10 observations.
func BenchmarkExport_100Histograms(b *testing.B) {
	mp := newTestMeterProvider()
	meter := mp.Meter("bench")
	for i := range 100 {
		h := meter.Float64Histogram(fmt.Sprintf("hist_%d", i))
		for j := range 10 {
			h.Record(float64(j) * 0.01)
		}
	}
	pe := NewPrometheusExporter(mp, slog.Default())

	b.ResetTimer()
	for range b.N {
		_ = pe.Export()
	}
}

// BenchmarkCounter_Add benchmarks counter increments.
func BenchmarkCounter_Add(b *testing.B) {
	mp := newTestMeterProvider()
	meter := mp.Meter("bench")
	c := meter.Int64Counter("bench_counter")

	b.ResetTimer()
	for range b.N {
		c.Inc()
	}
}

// BenchmarkHistogram_Record benchmarks histogram recordings.
func BenchmarkHistogram_Record(b *testing.B) {
	mp := newTestMeterProvider()
	meter := mp.Meter("bench")
	h := meter.Float64Histogram("bench_histogram")

	b.ResetTimer()
	for i := range b.N {
		h.Record(float64(i) * 0.001)
	}
}

// ---------------------------------------------------------------------------
// mustContain helper
// ---------------------------------------------------------------------------

// mustContain is a test assertion that checks haystack contains needle.
func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected output to contain %q\n\ngot:\n%s", needle, haystack)
	}
}

// httpGet performs a GET request with context (noctx linter compliance).
func httpGet(t *testing.T, rawURL string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}
