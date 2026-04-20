package config

import (
	"fmt"
	"time"
)

// Config is the top-level configuration for the Orchestra framework.
// It can be loaded from a YAML file, environment variables, or set
// programmatically via functional options.
type Config struct {
	// Providers maps provider names to their configurations.
	// Key is the provider name (e.g., "openai", "anthropic", "ollama").
	Providers map[string]ProviderConfig `json:"providers" yaml:"providers"`

	// Logging configures the logging subsystem.
	Logging LoggingConfig `json:"logging" yaml:"logging"`

	// Observability configures tracing and metrics collection.
	Observability ObservabilityConfig `json:"observability" yaml:"observability"`

	// DefaultProvider is the provider used when no provider is specified.
	// If empty, the first registered provider is used.
	DefaultProvider string `json:"default_provider,omitempty" yaml:"default_provider,omitempty"`

	// DefaultModel is the model used when no model is specified.
	// If empty, the provider's default model is used.
	DefaultModel string `json:"default_model,omitempty" yaml:"default_model,omitempty"`

	// MaxConcurrency limits the number of concurrent LLM requests across
	// all agents and workflows. 0 means unlimited.
	MaxConcurrency int `json:"max_concurrency,omitempty" yaml:"max_concurrency,omitempty"`

	// RequestTimeout is the default timeout for any single LLM request.
	// Providers may have their own timeouts; this is the global default.
	RequestTimeout time.Duration `json:"request_timeout,omitempty" yaml:"request_timeout,omitempty"`

	// AgentDefaults holds default configuration values applied to all
	// agents unless overridden at the agent level.
	AgentDefaults AgentDefaults `json:"agent_defaults,omitempty" yaml:"agent_defaults,omitempty"`
}

// ProviderConfig holds the configuration for a single LLM provider.
type ProviderConfig struct {
	// APIKey is the authentication key for the provider's API.
	// Can be set via environment variable using the ${ENV_VAR} syntax in YAML.
	APIKey string `json:"api_key,omitempty" yaml:"api_key,omitempty"`

	// BaseURL is the provider's API endpoint.
	// If empty, the provider's default URL is used.
	BaseURL string `json:"base_url,omitempty" yaml:"base_url,omitempty"`

	// DefaultModel is the model used when none is specified in a request.
	DefaultModel string `json:"default_model,omitempty" yaml:"default_model,omitempty"`

	// OrganizationID is an optional organization identifier (used by some providers).
	OrganizationID string `json:"organization_id,omitempty" yaml:"organization_id,omitempty"`

	// Enabled controls whether this provider is active.
	// Disabled providers are registered but cannot be used.
	Enabled *bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`

	// RateLimit specifies rate limiting configuration.
	RateLimit RateLimitConfig `json:"rate_limit,omitempty" yaml:"rate_limit,omitempty"`

	// Retry specifies retry configuration for transient failures.
	Retry RetryConfig `json:"retry,omitempty" yaml:"retry,omitempty"`

	// Models is an optional list of model-specific overrides.
	// Key is the model ID.
	Models map[string]ModelConfig `json:"models,omitempty" yaml:"models,omitempty"`

	// Extra contains provider-specific configuration options that
	// don't map to standard fields.
	Extra map[string]any `json:"extra,omitempty" yaml:"extra,omitempty"`
}

// IsEnabled returns true if the provider is enabled. A nil Enabled field
// defaults to true (providers are enabled unless explicitly disabled).
func (c ProviderConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// ModelConfig holds model-specific configuration overrides.
type ModelConfig struct {
	// DefaultTemperature is the default temperature for this model.
	DefaultTemperature *float64 `json:"default_temperature,omitempty" yaml:"default_temperature,omitempty"`

	// DefaultMaxTokens is the default max output tokens for this model.
	DefaultMaxTokens *int `json:"default_max_tokens,omitempty" yaml:"default_max_tokens,omitempty"`

	// MaxContextWindow overrides the model's maximum context window size.
	MaxContextWindow int `json:"max_context_window,omitempty" yaml:"max_context_window,omitempty"`

	// Aliases is a list of alternative names for this model.
	Aliases []string `json:"aliases,omitempty" yaml:"aliases,omitempty"`

	// Deprecated indicates whether this model should show deprecation warnings.
	Deprecated bool `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`

	// ReplacementModel is the recommended replacement if this model is deprecated.
	ReplacementModel string `json:"replacement_model,omitempty" yaml:"replacement_model,omitempty"`

	// CostPerInputToken is the cost in USD per input token for this model.
	CostPerInputToken float64 `json:"cost_per_input_token,omitempty" yaml:"cost_per_input_token,omitempty"`

	// CostPerOutputToken is the cost in USD per output token for this model.
	CostPerOutputToken float64 `json:"cost_per_output_token,omitempty" yaml:"cost_per_output_token,omitempty"`

	// Extra contains model-specific configuration options.
	Extra map[string]any `json:"extra,omitempty" yaml:"extra,omitempty"`
}

// RateLimitConfig configures rate limiting behavior for a provider.
type RateLimitConfig struct {
	// RequestsPerMinute is the maximum number of API requests per minute.
	// 0 means no limit.
	RequestsPerMinute int `json:"requests_per_minute,omitempty" yaml:"requests_per_minute,omitempty"`

	// TokensPerMinute is the maximum number of tokens (prompt + completion)
	// per minute. 0 means no limit.
	TokensPerMinute int `json:"tokens_per_minute,omitempty" yaml:"tokens_per_minute,omitempty"`

	// BurstSize is the maximum number of requests that can be made
	// instantaneously before the rate limiter kicks in.
	// Defaults to RequestsPerMinute / 10 if 0.
	BurstSize int `json:"burst_size,omitempty" yaml:"burst_size,omitempty"`

	// Enabled controls whether rate limiting is active.
	// nil defaults to true if any limit is set.
	Enabled *bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
}

// IsEnabled returns true if rate limiting should be active.
func (c RateLimitConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return c.RequestsPerMinute > 0 || c.TokensPerMinute > 0
	}
	return *c.Enabled
}

// RetryConfig configures retry behavior for transient failures.
type RetryConfig struct {
	// MaxAttempts is the maximum number of retry attempts.
	// 0 means no retries. The initial request is not counted.
	MaxAttempts int `json:"max_attempts,omitempty" yaml:"max_attempts,omitempty"`

	// InitialBackoff is the duration to wait before the first retry.
	// Defaults to 1 second if not set and MaxAttempts > 0.
	InitialBackoff time.Duration `json:"initial_backoff,omitempty" yaml:"initial_backoff,omitempty"`

	// MaxBackoff is the maximum backoff duration between retries.
	// Defaults to 30 seconds if not set.
	MaxBackoff time.Duration `json:"max_backoff,omitempty" yaml:"max_backoff,omitempty"`

	// BackoffMultiplier is the factor by which backoff increases after each
	// retry. A value of 2.0 means each retry waits twice as long as the previous.
	// Defaults to 2.0.
	BackoffMultiplier float64 `json:"backoff_multiplier,omitempty" yaml:"backoff_multiplier,omitempty"`

	// RetryableErrors is a list of error substrings that should trigger a retry.
	// If empty, sensible defaults are used (rate limit, timeout, server errors).
	RetryableErrors []string `json:"retryable_errors,omitempty" yaml:"retryable_errors,omitempty"`

	// Enabled controls whether retries are active.
	// nil defaults to true if MaxAttempts > 0.
	Enabled *bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
}

// IsEnabled returns true if retries should be active.
func (c RetryConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return c.MaxAttempts > 0
	}
	return *c.Enabled
}

// GetInitialBackoff returns the initial backoff, defaulting to 1 second.
func (c RetryConfig) GetInitialBackoff() time.Duration {
	if c.InitialBackoff <= 0 {
		return 1 * time.Second
	}
	return c.InitialBackoff
}

// GetMaxBackoff returns the max backoff, defaulting to 30 seconds.
func (c RetryConfig) GetMaxBackoff() time.Duration {
	if c.MaxBackoff <= 0 {
		return 30 * time.Second
	}
	return c.MaxBackoff
}

// GetBackoffMultiplier returns the backoff multiplier, defaulting to 2.0.
func (c RetryConfig) GetBackoffMultiplier() float64 {
	if c.BackoffMultiplier <= 0 {
		return 2.0
	}
	return c.BackoffMultiplier
}

// LoggingConfig configures the logging subsystem.
type LoggingConfig struct {
	// Level is the minimum log level. Valid values are "debug", "info", "warn", "error".
	// Defaults to "info".
	Level string `json:"level,omitempty" yaml:"level,omitempty"`

	// Format is the log output format. Valid values are "json", "text".
	// Defaults to "json".
	Format string `json:"format,omitempty" yaml:"format,omitempty"`

	// Output is the destination for log output. Valid values are "stdout", "stderr",
	// or a file path. Defaults to "stderr".
	Output string `json:"output,omitempty" yaml:"output,omitempty"`

	// AddSource controls whether the source file and line number are included.
	AddSource bool `json:"add_source,omitempty" yaml:"add_source,omitempty"`

	// TimeFormat is the format string for timestamps. Defaults to RFC3339.
	TimeFormat string `json:"time_format,omitempty" yaml:"time_format,omitempty"`
}

// GetLevel returns the log level, defaulting to "info".
func (c LoggingConfig) GetLevel() string {
	if c.Level == "" {
		return "info"
	}
	return c.Level
}

// GetFormat returns the log format, defaulting to "json".
func (c LoggingConfig) GetFormat() string {
	if c.Format == "" {
		return "json"
	}
	return c.Format
}

// GetOutput returns the log output, defaulting to "stderr".
func (c LoggingConfig) GetOutput() string {
	if c.Output == "" {
		return "stderr"
	}
	return c.Output
}

// ObservabilityConfig configures tracing and metrics collection.
type ObservabilityConfig struct {
	// Tracing configures distributed tracing.
	Tracing TracingConfig `json:"tracing,omitempty" yaml:"tracing,omitempty"`

	// Metrics configures metrics collection and export.
	Metrics MetricsConfig `json:"metrics,omitempty" yaml:"metrics,omitempty"`
}

// TracingConfig configures distributed tracing.
type TracingConfig struct {
	// Enabled controls whether tracing is active.
	Enabled bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`

	// Endpoint is the OTLP endpoint URL (e.g., "http://localhost:4318").
	Endpoint string `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`

	// ServiceName is the service name reported in traces.
	// Defaults to "orchestra".
	ServiceName string `json:"service_name,omitempty" yaml:"service_name,omitempty"`

	// SamplingRate is the fraction of traces to sample (0.0 to 1.0).
	// 1.0 means all traces are sampled. Defaults to 1.0.
	SamplingRate float64 `json:"sampling_rate,omitempty" yaml:"sampling_rate,omitempty"`

	// Propagator is the trace context propagation format.
	// Valid values are "w3c", "b3". Defaults to "w3c".
	Propagator string `json:"propagator,omitempty" yaml:"propagator,omitempty"`

	// Headers is additional headers to include in trace export requests.
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`

	// Insecure controls whether to use TLS for the trace endpoint.
	// Only use in development or with a local collector.
	Insecure bool `json:"insecure,omitempty" yaml:"insecure,omitempty"`
}

// GetEndpoint returns the tracing endpoint, defaulting to "http://localhost:4318".
func (c TracingConfig) GetEndpoint() string {
	if c.Endpoint == "" {
		return "http://localhost:4318"
	}
	return c.Endpoint
}

// GetServiceName returns the service name, defaulting to "orchestra".
func (c TracingConfig) GetServiceName() string {
	if c.ServiceName == "" {
		return "orchestra"
	}
	return c.ServiceName
}

// GetSamplingRate returns the sampling rate, defaulting to 1.0.
func (c TracingConfig) GetSamplingRate() float64 {
	if c.SamplingRate <= 0 {
		return 1.0
	}
	if c.SamplingRate > 1.0 {
		return 1.0
	}
	return c.SamplingRate
}

// GetPropagator returns the trace propagator format, defaulting to "w3c".
func (c TracingConfig) GetPropagator() string {
	if c.Propagator == "" {
		return "w3c"
	}
	return c.Propagator
}

// MetricsConfig configures metrics collection and export.
type MetricsConfig struct {
	// Enabled controls whether metrics collection is active.
	Enabled bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`

	// Endpoint is the metrics export endpoint (e.g., "http://localhost:9090/metrics").
	Endpoint string `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`

	// ServiceName is the service name label on metrics.
	// Defaults to "orchestra".
	ServiceName string `json:"service_name,omitempty" yaml:"service_name,omitempty"`

	// Namespace is a prefix for all metric names.
	// Defaults to "orchestra".
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`

	// ExportInterval is how frequently metrics are exported.
	// Defaults to 15 seconds.
	ExportInterval time.Duration `json:"export_interval,omitempty" yaml:"export_interval,omitempty"`

	// ExportTimeout is the timeout for a single metrics export call.
	// Defaults to 5 seconds.
	ExportTimeout time.Duration `json:"export_timeout,omitempty" yaml:"export_timeout,omitempty"`
}

// GetEndpoint returns the metrics endpoint, defaulting to "http://localhost:9090/metrics".
func (c MetricsConfig) GetEndpoint() string {
	if c.Endpoint == "" {
		return "http://localhost:9090/metrics"
	}
	return c.Endpoint
}

// GetServiceName returns the service name, defaulting to "orchestra".
func (c MetricsConfig) GetServiceName() string {
	if c.ServiceName == "" {
		return "orchestra"
	}
	return c.ServiceName
}

// GetNamespace returns the metrics namespace, defaulting to "orchestra".
func (c MetricsConfig) GetNamespace() string {
	if c.Namespace == "" {
		return "orchestra"
	}
	return c.Namespace
}

// GetExportInterval returns the export interval, defaulting to 15 seconds.
func (c MetricsConfig) GetExportInterval() time.Duration {
	if c.ExportInterval <= 0 {
		return 15 * time.Second
	}
	return c.ExportInterval
}

// GetExportTimeout returns the export timeout, defaulting to 5 seconds.
func (c MetricsConfig) GetExportTimeout() time.Duration {
	if c.ExportTimeout <= 0 {
		return 5 * time.Second
	}
	return c.ExportTimeout
}

// AgentDefaults holds default configuration values applied to all agents.
type AgentDefaults struct {
	// MaxTurns is the maximum number of agent turns in a single run.
	// 0 means unlimited.
	MaxTurns int `json:"max_turns,omitempty" yaml:"max_turns,omitempty"`

	// Temperature is the default generation temperature.
	Temperature *float64 `json:"temperature,omitempty" yaml:"temperature,omitempty"`

	// TopP is the default top-p sampling parameter.
	TopP *float64 `json:"top_p,omitempty" yaml:"top_p,omitempty"`

	// MaxTokens is the default maximum number of output tokens.
	MaxTokens *int `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`

	// SystemPrompt is an optional default system prompt applied to all agents.
	SystemPrompt string `json:"system_prompt,omitempty" yaml:"system_prompt,omitempty"`

	// Timeout is the default timeout for a single agent run.
	Timeout time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`

	// RetryOnFailure controls whether agents automatically retry on transient errors.
	RetryOnFailure bool `json:"retry_on_failure,omitempty" yaml:"retry_on_failure,omitempty"`

	// Extra contains arbitrary default agent configuration.
	Extra map[string]any `json:"extra,omitempty" yaml:"extra,omitempty"`
}

// Validate performs validation on the configuration and returns an error
// describing any issues found.
func (c *Config) Validate() error {
	if c.Providers != nil {
		for name, pc := range c.Providers {
			if err := pc.Validate(); err != nil {
				return fmt.Errorf("provider %q: %w", name, err)
			}
		}
	}

	if err := c.Logging.Validate(); err != nil {
		return fmt.Errorf("logging: %w", err)
	}

	if c.Observability.Tracing.Enabled {
		if err := c.Observability.Tracing.Validate(); err != nil {
			return fmt.Errorf("tracing: %w", err)
		}
	}

	return nil
}

// Validate performs validation on a ProviderConfig.
func (c ProviderConfig) Validate() error {
	if c.IsEnabled() && c.APIKey == "" && c.BaseURL == "" {
		// Providers need at least an API key or a base URL (e.g., Ollama)
		// This is a warning-level check, not a hard error
	}

	if c.Retry.IsEnabled() {
		if c.Retry.MaxAttempts < 0 {
			return fmt.Errorf("max_attempts must be non-negative, got %d", c.Retry.MaxAttempts)
		}
		if c.Retry.BackoffMultiplier < 0 {
			return fmt.Errorf("backoff_multiplier must be non-negative, got %f", c.Retry.BackoffMultiplier)
		}
	}

	if c.RateLimit.IsEnabled() {
		if c.RateLimit.RequestsPerMinute < 0 {
			return fmt.Errorf("requests_per_minute must be non-negative, got %d", c.RateLimit.RequestsPerMinute)
		}
		if c.RateLimit.TokensPerMinute < 0 {
			return fmt.Errorf("tokens_per_minute must be non-negative, got %d", c.RateLimit.TokensPerMinute)
		}
	}

	return nil
}

// Validate performs validation on LoggingConfig.
func (c LoggingConfig) Validate() error {
	switch c.GetLevel() {
	case "debug", "info", "warn", "error", "":
		// valid
	default:
		return fmt.Errorf("invalid log level %q; valid values are debug, info, warn, error", c.Level)
	}

	switch c.GetFormat() {
	case "json", "text", "":
		// valid
	default:
		return fmt.Errorf("invalid log format %q; valid values are json, text", c.Format)
	}

	return nil
}

// Validate performs validation on TracingConfig.
func (c TracingConfig) Validate() error {
	if c.SamplingRate < 0 || c.SamplingRate > 1.0 {
		return fmt.Errorf("sampling_rate must be between 0.0 and 1.0, got %f", c.SamplingRate)
	}
	return nil
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Providers: make(map[string]ProviderConfig),
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
			Output: "stderr",
		},
		Observability: ObservabilityConfig{
			Tracing: TracingConfig{
				Enabled:      false,
				Endpoint:     "http://localhost:4318",
				ServiceName:  "orchestra",
				SamplingRate: 1.0,
				Propagator:   "w3c",
			},
			Metrics: MetricsConfig{
				Enabled:       false,
				Endpoint:      "http://localhost:9090/metrics",
				ServiceName:   "orchestra",
				Namespace:     "orchestra",
				ExportInterval: 15 * time.Second,
				ExportTimeout:  5 * time.Second,
			},
		},
		MaxConcurrency:  0, // unlimited
		RequestTimeout:  5 * time.Minute,
	}
}
