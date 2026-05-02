package observability

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// HealthStatus represents the health state of the system.
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
)

// HealthCheckResult is the result of a single health check.
type HealthCheckResult struct {
	Name      string       `json:"name"`
	Status    HealthStatus `json:"status"`
	Message   string       `json:"message,omitempty"`
	Timestamp time.Time    `json:"timestamp"`
	Duration  string       `json:"duration,omitempty"`
}

// HealthResponse is the response from a health check endpoint.
type HealthResponse struct {
	Status    HealthStatus        `json:"status"`
	Timestamp time.Time           `json:"timestamp"`
	Version   string              `json:"version,omitempty"`
	Uptime    string              `json:"uptime,omitempty"`
	Checks    []HealthCheckResult `json:"checks,omitempty"`
}

// HealthCheck is a function that checks the health of a component.
// It returns the status, an optional message, and any error encountered.
type HealthCheck func() (HealthStatus, string, error)

// HealthChecker manages health checks and exposes HTTP endpoints for
// readiness and liveness probes.
type HealthChecker struct {
	logger    *slog.Logger
	mu        sync.RWMutex
	checks    map[string]HealthCheck
	ready     atomic.Bool
	startTime time.Time
	version   string
}

// newHealthChecker creates a new health checker.
func newHealthChecker(logger *slog.Logger) *HealthChecker {
	hc := &HealthChecker{
		logger:    logger.With("component", "health"),
		checks:    make(map[string]HealthCheck),
		startTime: time.Now(),
		version:   "0.1.0",
	}
	hc.ready.Store(true)
	return hc
}

// RegisterCheck registers a named health check function.
// Health checks are executed when the health endpoint is queried.
func (hc *HealthChecker) RegisterCheck(name string, check HealthCheck) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.checks[name] = check
	hc.logger.Debug("health check registered", "check", name)
}

// UnregisterCheck removes a health check by name.
func (hc *HealthChecker) UnregisterCheck(name string) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	delete(hc.checks, name)
}

// SetReady marks the service as ready to accept traffic.
func (hc *HealthChecker) SetReady() {
	hc.ready.Store(true)
	hc.logger.Info("service marked as ready")
}

// SetNotReady marks the service as not ready to accept traffic.
func (hc *HealthChecker) SetNotReady(reason string) {
	hc.ready.Store(false)
	hc.logger.Warn("service marked as not ready", "reason", reason)
}

// IsReady returns whether the service is ready.
func (hc *HealthChecker) IsReady() bool {
	return hc.ready.Load()
}

// SetVersion sets the version string reported in health responses.
func (hc *HealthChecker) SetVersion(version string) {
	hc.version = version
}

// CheckHealth runs all registered health checks and returns the aggregate result.
func (hc *HealthChecker) CheckHealth() HealthResponse {
	hc.mu.RLock()
	checks := make(map[string]HealthCheck, len(hc.checks))
	for k, v := range hc.checks {
		checks[k] = v
	}
	hc.mu.RUnlock()

	response := HealthResponse{
		Timestamp: time.Now(),
		Version:   hc.version,
		Uptime:    time.Since(hc.startTime).Round(time.Second).String(),
		Checks:    make([]HealthCheckResult, 0, len(checks)),
		Status:    HealthStatusHealthy,
	}

	for name, check := range checks {
		start := time.Now()
		status, message, err := check()
		duration := time.Since(start)

		result := HealthCheckResult{
			Name:      name,
			Status:    status,
			Message:   message,
			Timestamp: start,
			Duration:  duration.Round(time.Millisecond).String(),
		}

		if err != nil {
			result.Message = err.Error()
			if result.Status == HealthStatusHealthy {
				result.Status = HealthStatusUnhealthy
			}
		}

		response.Checks = append(response.Checks, result)

		// Aggregate status
		if result.Status == HealthStatusUnhealthy {
			response.Status = HealthStatusUnhealthy
		} else if result.Status == HealthStatusDegraded && response.Status != HealthStatusUnhealthy {
			response.Status = HealthStatusDegraded
		}
	}

	return response
}

// HandleHealth returns an http.HandlerFunc that serves the health check endpoint.
// The response is a JSON object with the overall status and individual check results.
//
// GET /health
//
// Response: 200 (healthy/degraded) or 503 (unhealthy)
func (hc *HealthChecker) HandleHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		response := hc.CheckHealth()

		statusCode := http.StatusOK
		if response.Status == HealthStatusUnhealthy {
			statusCode = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)

		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(response); err != nil {
			hc.logger.Error("failed to encode health response", "error", err)
		}
	}
}

// HandleReady returns an http.HandlerFunc that serves the readiness probe.
// Returns 200 if the service is ready, 503 otherwise.
//
// GET /ready
func (hc *HealthChecker) HandleReady() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if !hc.IsReady() {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"status":"not_ready","timestamp":"%s"}`, time.Now().Format(time.RFC3339))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ready","timestamp":"%s","uptime":"%s"}`,
			time.Now().Format(time.RFC3339),
			time.Since(hc.startTime).Round(time.Second),
		)
	}
}

// HandleLive returns an http.HandlerFunc that serves the liveness probe.
// Always returns 200 if the process is running.
//
// GET /live
func (hc *HealthChecker) HandleLive() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"alive","timestamp":"%s"}`,
			time.Now().Format(time.RFC3339),
		)
	}
}
