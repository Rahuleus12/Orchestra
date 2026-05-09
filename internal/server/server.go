// Package server implements the Orchestra HTTP server, providing a REST API
// for multi-agent orchestration. It uses only the Go standard library
// (net/http) — no third-party frameworks.
//
// Endpoints:
//
//	GET    /v1/health              Health check
//	GET    /v1/providers           List registered providers
//	GET    /v1/providers/{name}    Get provider details
//	GET    /v1/models              List available models
//	POST   /v1/generate            Generate a completion
//	POST   /v1/generate/stream     Stream a completion (SSE)
//	POST   /v1/agents              Create and run an agent
//	POST   /v1/agents/{id}/run     Run a named agent
//	POST   /v1/workflows           Execute a workflow
//	POST   /v1/workflows/stream    Stream workflow execution (SSE)
package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/user/orchestra/internal/config"
	"github.com/user/orchestra/internal/provider"
)

// ServerConfig holds server-specific configuration, separate from the
// orchestra engine config.
type ServerConfig struct {
	// Addr is the address to listen on (e.g., ":8080").
	Addr string `json:"addr" yaml:"addr"`

	// APIKeys is a list of valid API keys for authentication.
	// If empty, authentication is disabled (useful for local dev).
	APIKeys []string `json:"api_keys,omitempty" yaml:"api_keys,omitempty"`

	// ReadTimeout is the maximum duration for reading the entire request.
	ReadTimeout time.Duration `json:"read_timeout,omitempty" yaml:"read_timeout,omitempty"`

	// WriteTimeout is the maximum duration before timing out writes.
	WriteTimeout time.Duration `json:"write_timeout,omitempty" yaml:"write_timeout,omitempty"`

	// IdleTimeout is the maximum amount of time to wait for the next request.
	IdleTimeout time.Duration `json:"idle_timeout,omitempty" yaml:"idle_timeout,omitempty"`

	// ShutdownTimeout is the maximum duration to wait for in-flight requests
	// to complete during graceful shutdown.
	ShutdownTimeout time.Duration `json:"shutdown_timeout,omitempty" yaml:"shutdown_timeout,omitempty"`

	// CORSAllowedOrigins lists allowed origins for CORS. Use "*" to allow all.
	// If empty, CORS headers are not set.
	CORSAllowedOrigins []string `json:"cors_allowed_origins,omitempty" yaml:"cors_allowed_origins,omitempty"`

	// ConfigPath is the path to the orchestra configuration file.
	ConfigPath string `json:"config_path,omitempty" yaml:"config_path,omitempty"`
}

// DefaultServerConfig returns a ServerConfig with sensible defaults.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Addr:             ":8080",
		ReadTimeout:      30 * time.Second,
		WriteTimeout:     120 * time.Second,
		IdleTimeout:      120 * time.Second,
		ShutdownTimeout:  30 * time.Second,
		CORSAllowedOrigins: []string{"*"},
	}
}

// Server is the Orchestra HTTP server.
type Server struct {
	cfg      ServerConfig
	registry *provider.Registry
	config   *config.Config
	logger   *slog.Logger
	handler  http.Handler
	server   *http.Server

	// agentStore holds named agents that can be run on demand.
	agentStore sync.Map // map[string]*agentEntry

	// requestID counter for generating unique request IDs.
	requestIDCounter uint64
	requestIDMu      sync.Mutex
}

// agentEntry stores a named agent definition.
type agentEntry struct {
	Name     string
	Provider string
	Model    string
	System   string
	MaxTurns int
}

// New creates a new Orchestra server with the given configuration and
// provider registry. The registry should already have providers registered.
func New(cfg ServerConfig, registry *provider.Registry, orchestraCfg *config.Config, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}

	s := &Server{
		cfg:      cfg,
		registry: registry,
		config:   orchestraCfg,
		logger:   logger,
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	var handler http.Handler = mux
	handler = s.corsMiddleware(handler)
	handler = s.authMiddleware(handler)
	handler = s.requestIDMiddleware(handler)
	handler = s.loggingMiddleware(handler)
	handler = s.recoveryMiddleware(handler)

	s.handler = handler

	return s
}

// ListenAndServe starts the HTTP server and blocks until the server is
// shut down or an error occurs. It sets up graceful shutdown handling
// for SIGINT and SIGTERM signals.
func (s *Server) ListenAndServe() error {
	s.server = &http.Server{
		Addr:         s.cfg.Addr,
		Handler:      s.handler,
		ReadTimeout:  s.cfg.ReadTimeout,
		WriteTimeout: s.cfg.WriteTimeout,
		IdleTimeout:  s.cfg.IdleTimeout,
		BaseContext: func(ln net.Listener) context.Context {
			return context.Background()
		},
	}

	// Channel to listen for errors from the server
	serverErr := make(chan error, 1)
	go func() {
		s.logger.Info("Orchestra server starting",
			"addr", s.cfg.Addr,
			"auth_enabled", len(s.cfg.APIKeys) > 0,
			"providers", s.registry.ListProviders(),
		)
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	case sig := <-quit:
		s.logger.Info("Received shutdown signal", "signal", sig.String())
	}

	return s.Shutdown()
}

// Shutdown gracefully shuts down the server, waiting for in-flight requests
// to complete up to the configured shutdown timeout.
func (s *Server) Shutdown() error {
	if s.server == nil {
		return nil
	}

	s.logger.Info("Shutting down server gracefully",
		"timeout", s.cfg.ShutdownTimeout,
	)

	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		s.logger.Error("Server shutdown error", "error", err)
		return fmt.Errorf("server shutdown: %w", err)
	}

	s.logger.Info("Server stopped gracefully")
	return nil
}

// Handler returns the http.Handler for use with external HTTP servers
// or testing. This is useful for embedding the Orchestra server in a
// larger HTTP application.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// registerRoutes registers all API routes on the given mux.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Health & Info
	mux.HandleFunc("GET /v1/health", s.handleHealth)
	mux.HandleFunc("GET /v1/info", s.handleInfo)

	// Providers
	mux.HandleFunc("GET /v1/providers", s.handleListProviders)
	mux.HandleFunc("GET /v1/providers/{name}", s.handleGetProvider)

	// Models
	mux.HandleFunc("GET /v1/models", s.handleListModels)

	// Generate
	mux.HandleFunc("POST /v1/generate", s.handleGenerate)
	mux.HandleFunc("POST /v1/generate/stream", s.handleGenerateStream)

	// Agents
	mux.HandleFunc("POST /v1/agents", s.handleCreateAgent)
	mux.HandleFunc("GET /v1/agents", s.handleListAgents)
	mux.HandleFunc("POST /v1/agents/{id}/run", s.handleRunAgent)
	mux.HandleFunc("DELETE /v1/agents/{id}", s.handleDeleteAgent)

	// Workflows
	mux.HandleFunc("POST /v1/workflows", s.handleExecuteWorkflow)
	mux.HandleFunc("POST /v1/workflows/stream", s.handleStreamWorkflow)
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

// requestIDMiddleware adds a unique request ID to each request.
func (s *Server) requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := generateRequestID()
		ctx := context.WithValue(r.Context(), ctxKeyRequestID{}, id)
		r = r.WithContext(ctx)

		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs each request with method, path, status, and duration.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID, _ := r.Context().Value(ctxKeyRequestID{}).(string)

		// Wrap ResponseWriter to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)
		s.logger.Info("request",
			"request_id", reqID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.statusCode,
			"duration", duration.String(),
			"remote_addr", r.RemoteAddr,
		)
	})
}

// recoveryMiddleware recovers from panics and returns a 500 error.
func (s *Server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				reqID, _ := r.Context().Value(ctxKeyRequestID{}).(string)
				s.logger.Error("panic recovered",
					"request_id", reqID,
					"error", rec,
					"path", r.URL.Path,
				)
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// authMiddleware validates API key or Bearer token authentication.
// If no API keys are configured, authentication is skipped.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth if no keys configured
		if len(s.cfg.APIKeys) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		// Skip auth for health endpoint
		if r.URL.Path == "/v1/health" {
			next.ServeHTTP(w, r)
			return
		}

		key := extractAPIKey(r)
		if key == "" {
			writeError(w, http.StatusUnauthorized, "authentication required: set Authorization: Bearer <key> or X-API-Key header")
			return
		}

		if !s.validateAPIKey(key) {
			reqID, _ := r.Context().Value(ctxKeyRequestID{}).(string)
			s.logger.Warn("invalid API key",
				"request_id", reqID,
				"remote_addr", r.RemoteAddr,
			)
			writeError(w, http.StatusUnauthorized, "invalid API key")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// corsMiddleware adds CORS headers to responses.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if s.isOriginAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// isOriginAllowed checks if the given origin is in the allowed list.
func (s *Server) isOriginAllowed(origin string) bool {
	if len(s.cfg.CORSAllowedOrigins) == 0 || origin == "" {
		return false
	}
	for _, allowed := range s.cfg.CORSAllowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}
	return false
}

// validateAPIKey checks if the provided key matches any configured key
// using constant-time comparison to prevent timing attacks.
func (s *Server) validateAPIKey(key string) bool {
	for _, valid := range s.cfg.APIKeys {
		if subtle.ConstantTimeCompare([]byte(key), []byte(valid)) == 1 {
			return true
		}
	}
	return false
}

// extractAPIKey extracts the API key from the request headers.
// It checks both the Authorization: Bearer header and the X-API-Key header.
func extractAPIKey(r *http.Request) string {
	// Check X-API-Key header
	if key := r.Header.Get("X-API-Key"); key != "" {
		return strings.TrimSpace(key)
	}

	// Check Authorization: Bearer header
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}

	return ""
}

// ---------------------------------------------------------------------------
// Context Keys
// ---------------------------------------------------------------------------

type ctxKeyRequestID struct{}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// generateRequestID creates a unique request ID.
func generateRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader captures the status code before delegating to the wrapped writer.
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// WriteError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":{"message":%q,"status":%d}}`, message, status)
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

// readBody reads the request body, limited to maxBytes.
func readBody(r *http.Request, maxBytes int64) ([]byte, error) {
	if r.Body == nil {
		return nil, errors.New("request body is empty")
	}
	defer r.Body.Close()
	return io.ReadAll(io.LimitReader(r.Body, maxBytes))
}
