package observability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// PrometheusExporter exposes collected metrics in Prometheus text exposition format.
// It serves metrics via an HTTP endpoint that can be scraped by Prometheus.
type PrometheusExporter struct {
	mu     sync.Mutex
	meter  *MeterProvider
	logger *slog.Logger
}

// NewPrometheusExporter creates a new Prometheus exporter backed by the given
// meter provider.
func NewPrometheusExporter(mp *MeterProvider, logger *slog.Logger) *PrometheusExporter {
	return &PrometheusExporter{
		meter:  mp,
		logger: logger.With(slog.String("component", "prometheus_exporter")),
	}
}

// Handler returns an http.HandlerFunc that serves metrics in Prometheus format.
//
// GET /metrics
//
// The response is in Prometheus text exposition format, compatible with
// Prometheus scraping. Example output:
//
//	# HELP orchestra_provider_requests_total Total provider requests
//	# TYPE orchestra_provider_requests_total counter
//	orchestra_provider_requests_total 42
//
//	# HELP orchestra_provider_latency_seconds Provider request latency
//	# TYPE orchestra_provider_latency_seconds histogram
//	orchestra_provider_latency_seconds_bucket{le="0.005"} 10
//	orchestra_provider_latency_seconds_bucket{le="0.01"} 15
//	...
//	orchestra_provider_latency_seconds_count 42
//	orchestra_provider_latency_seconds_sum 1.234
func (pe *PrometheusExporter) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		content := pe.Export()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if _, err := io.WriteString(w, content); err != nil {
			pe.logger.Error("failed to write metrics response",
				slog.String("error", err.Error()),
			)
		}
	}
}

// Export generates the Prometheus text exposition format for all collected metrics.
func (pe *PrometheusExporter) Export() string {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	snapshot := pe.meter.GetAllMetrics()
	var sb strings.Builder

	// Export counters
	for _, counter := range snapshot.Counters {
		sb.WriteString(fmt.Sprintf("# HELP %s Total count\n", counter.Name))
		sb.WriteString(fmt.Sprintf("# TYPE %s counter\n", counter.Name))
		sb.WriteString(formatLabels(counter.Name, counter.Value, counter.Labels))
		sb.WriteString("\n")
	}

	// Export histograms
	for _, hist := range snapshot.Histograms {
		sb.WriteString(fmt.Sprintf("# HELP %s Distribution\n", hist.Name))
		sb.WriteString(fmt.Sprintf("# TYPE %s histogram\n", hist.Name))

		// Bucket entries
		for _, boundary := range sortedFloat64Keys(hist.Buckets) {
			sb.WriteString(fmt.Sprintf("%s_bucket{le=\"%.6g\"} %d\n",
				hist.Name, boundary, hist.Buckets[boundary]))
		}
		// +Inf bucket
		sb.WriteString(fmt.Sprintf("%s_bucket{le=\"+Inf\"} %d\n", hist.Name, hist.Count))
		// Sum and count
		sb.WriteString(fmt.Sprintf("%s_sum %.6f\n", hist.Name, hist.Sum))
		sb.WriteString(fmt.Sprintf("%s_count %d\n", hist.Name, hist.Count))
		sb.WriteString("\n")
	}

	// Export gauges
	for _, gauge := range snapshot.Gauges {
		sb.WriteString(fmt.Sprintf("# HELP %s Current value\n", gauge.Name))
		sb.WriteString(fmt.Sprintf("# TYPE %s gauge\n", gauge.Name))
		sb.WriteString(formatLabels(gauge.Name, gauge.Value, gauge.Labels))
		sb.WriteString("\n")
	}

	return sb.String()
}

// formatLabels formats a metric name with optional labels and a value.
func formatLabels(name string, value int64, labels map[string]string) string {
	if len(labels) == 0 {
		return fmt.Sprintf("%s %d\n", name, value)
	}

	pairs := make([]string, 0, len(labels))
	for k, v := range labels {
		pairs = append(pairs, fmt.Sprintf("%s=%q", k, v))
	}
	return fmt.Sprintf("%s{%s} %d\n", name, strings.Join(pairs, ","), value)
}

// sortedFloat64Keys returns sorted float64 keys of a map.
func sortedFloat64Keys(m map[float64]int64) []float64 {
	keys := make([]float64, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple insertion sort (buckets are small)
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// StartServer starts a Prometheus metrics HTTP server on the given address.
// This is a convenience function for running a metrics endpoint alongside
// the main application.
//
// The server serves metrics at /metrics and a health check at /health.
// It runs in a goroutine and can be stopped via the returned shutdown function.
func StartServer(mp *MeterProvider, addr string, logger *slog.Logger) (shutdown func() error, err error) {
	if logger == nil {
		logger = slog.Default()
	}

	exporter := NewPrometheusExporter(mp, logger)
	health := newHealthChecker(logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", exporter.Handler())
	mux.HandleFunc("/health", health.HandleHealth())
	mux.HandleFunc("/ready", health.HandleReady())
	mux.HandleFunc("/live", health.HandleLive())

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.Info("starting metrics server", slog.String("addr", addr))
		if listenErr := server.ListenAndServe(); listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			logger.Error("metrics server error", slog.String("error", listenErr.Error()))
		}
	}()

	shutdown = func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		logger.Info("shutting down metrics server")
		return server.Shutdown(ctx)
	}

	return shutdown, nil
}
