package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- DefaultConfig ---

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg == nil {
		t.Fatal("DefaultConfig() returned nil")
	}
	if cfg.Logging.GetLevel() != "info" {
		t.Errorf("default logging level = %q, want %q", cfg.Logging.GetLevel(), "info")
	}
	if cfg.Logging.GetFormat() != "json" {
		t.Errorf("default logging format = %q, want %q", cfg.Logging.GetFormat(), "json")
	}
	if cfg.Logging.GetOutput() != "stderr" {
		t.Errorf("default logging output = %q, want %q", cfg.Logging.GetOutput(), "stderr")
	}
	if cfg.RequestTimeout != 5*time.Minute {
		t.Errorf("default request timeout = %v, want %v", cfg.RequestTimeout, 5*time.Minute)
	}
	if cfg.Providers == nil {
		t.Error("default Providers map is nil")
	}
}

// --- LoggingConfig ---

func TestLoggingConfig_GetLevel(t *testing.T) {
	tests := []struct {
		name  string
		level string
		want  string
	}{
		{"empty defaults to info", "", "info"},
		{"debug", "debug", "debug"},
		{"info", "info", "info"},
		{"warn", "warn", "warn"},
		{"error", "error", "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := LoggingConfig{Level: tt.level}
			if got := cfg.GetLevel(); got != tt.want {
				t.Errorf("GetLevel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoggingConfig_GetFormat(t *testing.T) {
	tests := []struct {
		name   string
		format string
		want   string
	}{
		{"empty defaults to json", "", "json"},
		{"json", "json", "json"},
		{"text", "text", "text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := LoggingConfig{Format: tt.format}
			if got := cfg.GetFormat(); got != tt.want {
				t.Errorf("GetFormat() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoggingConfig_GetOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{"empty defaults to stderr", "", "stderr"},
		{"stdout", "stdout", "stdout"},
		{"stderr", "stderr", "stderr"},
		{"file path", "/var/log/orchestra.log", "/var/log/orchestra.log"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := LoggingConfig{Output: tt.output}
			if got := cfg.GetOutput(); got != tt.want {
				t.Errorf("GetOutput() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoggingConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     LoggingConfig
		wantErr bool
	}{
		{"empty (defaults)", LoggingConfig{}, false},
		{"valid debug", LoggingConfig{Level: "debug"}, false},
		{"valid info", LoggingConfig{Level: "info"}, false},
		{"valid warn", LoggingConfig{Level: "warn"}, false},
		{"valid error", LoggingConfig{Level: "error"}, false},
		{"invalid level", LoggingConfig{Level: "verbose"}, true},
		{"valid json format", LoggingConfig{Format: "json"}, false},
		{"valid text format", LoggingConfig{Format: "text"}, false},
		{"invalid format", LoggingConfig{Format: "xml"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// --- RetryConfig ---

func TestRetryConfig_IsEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  RetryConfig
		want bool
	}{
		{"zero max attempts", RetryConfig{MaxAttempts: 0}, false},
		{"positive max attempts", RetryConfig{MaxAttempts: 3}, true},
		{"explicitly enabled with zero attempts", RetryConfig{Enabled: boolPtr(true)}, true},
		{"explicitly disabled with positive attempts", RetryConfig{MaxAttempts: 3, Enabled: boolPtr(false)}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRetryConfig_Defaults(t *testing.T) {
	cfg := RetryConfig{}

	if got := cfg.GetInitialBackoff(); got != 1*time.Second {
		t.Errorf("GetInitialBackoff() = %v, want %v", got, 1*time.Second)
	}
	if got := cfg.GetMaxBackoff(); got != 30*time.Second {
		t.Errorf("GetMaxBackoff() = %v, want %v", got, 30*time.Second)
	}
	if got := cfg.GetBackoffMultiplier(); got != 2.0 {
		t.Errorf("GetBackoffMultiplier() = %v, want %v", got, 2.0)
	}
}

func TestRetryConfig_CustomValues(t *testing.T) {
	cfg := RetryConfig{
		InitialBackoff:    500 * time.Millisecond,
		MaxBackoff:        10 * time.Second,
		BackoffMultiplier: 1.5,
	}

	if got := cfg.GetInitialBackoff(); got != 500*time.Millisecond {
		t.Errorf("GetInitialBackoff() = %v, want %v", got, 500*time.Millisecond)
	}
	if got := cfg.GetMaxBackoff(); got != 10*time.Second {
		t.Errorf("GetMaxBackoff() = %v, want %v", got, 10*time.Second)
	}
	if got := cfg.GetBackoffMultiplier(); got != 1.5 {
		t.Errorf("GetBackoffMultiplier() = %v, want %v", got, 1.5)
	}
}

// --- RateLimitConfig ---

func TestRateLimitConfig_IsEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  RateLimitConfig
		want bool
	}{
		{"zero values", RateLimitConfig{}, false},
		{"with RPM", RateLimitConfig{RequestsPerMinute: 60}, true},
		{"with TPM", RateLimitConfig{TokensPerMinute: 100000}, true},
		{"explicitly disabled", RateLimitConfig{RequestsPerMinute: 60, Enabled: boolPtr(false)}, false},
		{"explicitly enabled no limits", RateLimitConfig{Enabled: boolPtr(true)}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- ProviderConfig ---

func TestProviderConfig_IsEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  ProviderConfig
		want bool
	}{
		{"nil defaults to enabled", ProviderConfig{}, true},
		{"explicitly enabled", ProviderConfig{Enabled: boolPtr(true)}, true},
		{"explicitly disabled", ProviderConfig{Enabled: boolPtr(false)}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProviderConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ProviderConfig
		wantErr bool
	}{
		{"empty config is valid", ProviderConfig{}, false},
		{"valid config", ProviderConfig{
			APIKey:       "sk-test",
			DefaultModel: "gpt-4",
		}, false},
		{"negative max attempts", ProviderConfig{
			Retry: RetryConfig{MaxAttempts: -1, Enabled: boolPtr(true)},
		}, true},
		{"negative backoff multiplier", ProviderConfig{
			Retry: RetryConfig{BackoffMultiplier: -0.5, Enabled: boolPtr(true)},
		}, true},
		{"negative RPM", ProviderConfig{
			RateLimit: RateLimitConfig{RequestsPerMinute: -10, Enabled: boolPtr(true)},
		}, true},
		{"negative TPM", ProviderConfig{
			RateLimit: RateLimitConfig{TokensPerMinute: -1, Enabled: boolPtr(true)},
		}, true},
		{"disabled retry with bad values", ProviderConfig{
			Retry: RetryConfig{MaxAttempts: -5, Enabled: boolPtr(false)},
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// --- TracingConfig ---

func TestTracingConfig_Defaults(t *testing.T) {
	cfg := TracingConfig{}

	if got := cfg.GetEndpoint(); got != "http://localhost:4318" {
		t.Errorf("GetEndpoint() = %q, want %q", got, "http://localhost:4318")
	}
	if got := cfg.GetServiceName(); got != "orchestra" {
		t.Errorf("GetServiceName() = %q, want %q", got, "orchestra")
	}
	if got := cfg.GetSamplingRate(); got != 1.0 {
		t.Errorf("GetSamplingRate() = %v, want %v", got, 1.0)
	}
	if got := cfg.GetPropagator(); got != "w3c" {
		t.Errorf("GetPropagator() = %q, want %q", got, "w3c")
	}
}

func TestTracingConfig_SamplingRateClamping(t *testing.T) {
	tests := []struct {
		name string
		rate float64
		want float64
	}{
		{"negative clamped to 1.0", -0.5, 1.0},
		{"zero clamped to 1.0", 0.0, 1.0},
		{"valid 0.5", 0.5, 0.5},
		{"valid 1.0", 1.0, 1.0},
		{"above 1.0 clamped", 2.0, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := TracingConfig{SamplingRate: tt.rate}
			if got := cfg.GetSamplingRate(); got != tt.want {
				t.Errorf("GetSamplingRate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTracingConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     TracingConfig
		wantErr bool
	}{
		{"valid defaults", TracingConfig{}, false},
		{"valid sampling rate", TracingConfig{SamplingRate: 0.5}, false},
		{"negative sampling rate", TracingConfig{SamplingRate: -0.1}, true},
		{"sampling rate too high", TracingConfig{SamplingRate: 1.5}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// --- MetricsConfig ---

func TestMetricsConfig_Defaults(t *testing.T) {
	cfg := MetricsConfig{}

	if got := cfg.GetEndpoint(); got != "http://localhost:9090/metrics" {
		t.Errorf("GetEndpoint() = %q, want %q", got, "http://localhost:9090/metrics")
	}
	if got := cfg.GetServiceName(); got != "orchestra" {
		t.Errorf("GetServiceName() = %q, want %q", got, "orchestra")
	}
	if got := cfg.GetNamespace(); got != "orchestra" {
		t.Errorf("GetNamespace() = %q, want %q", got, "orchestra")
	}
	if got := cfg.GetExportInterval(); got != 15*time.Second {
		t.Errorf("GetExportInterval() = %v, want %v", got, 15*time.Second)
	}
	if got := cfg.GetExportTimeout(); got != 5*time.Second {
		t.Errorf("GetExportTimeout() = %v, want %v", got, 5*time.Second)
	}
}

// --- Config Validate ---

func TestConfig_Validate(t *testing.T) {
	t.Run("valid empty config", func(t *testing.T) {
		cfg := DefaultConfig()
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() error = %v", err)
		}
	})

	t.Run("invalid logging level", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Logging.Level = "verbose"
		if err := cfg.Validate(); err == nil {
			t.Error("Validate() should return error for invalid log level")
		}
	})

	t.Run("invalid provider config", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Providers["test"] = ProviderConfig{
			Retry: RetryConfig{MaxAttempts: -1, Enabled: boolPtr(true)},
		}
		if err := cfg.Validate(); err == nil {
			t.Error("Validate() should return error for invalid provider config")
		}
	})

	t.Run("invalid tracing config", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Observability.Tracing.Enabled = true
		cfg.Observability.Tracing.SamplingRate = -1.0
		if err := cfg.Validate(); err == nil {
			t.Error("Validate() should return error for invalid tracing config")
		}
	})
}

// --- LoadFromFile ---

func TestLoadFromFile_ValidYAML(t *testing.T) {
	yamlContent := `
providers:
  openai:
    api_key: sk-test-key-123
    base_url: https://api.openai.com/v1
    default_model: gpt-4-turbo
    rate_limit:
      requests_per_minute: 60
      tokens_per_minute: 150000
    retry:
      max_attempts: 3
      initial_backoff: 1s

  anthropic:
    api_key: sk-ant-test-key
    default_model: claude-sonnet-4-20250514
    rate_limit:
      requests_per_minute: 60

  ollama:
    base_url: http://localhost:11434
    default_model: llama3

logging:
  level: debug
  format: json

observability:
  tracing:
    enabled: true
    endpoint: http://localhost:4318
  metrics:
    enabled: true
    endpoint: http://localhost:9090/metrics

default_provider: openai
default_model: gpt-4-turbo
max_concurrency: 10
request_timeout: 2m
`

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	// Verify providers
	if len(cfg.Providers) != 3 {
		t.Fatalf("Providers count = %d, want 3", len(cfg.Providers))
	}

	openai, ok := cfg.Providers["openai"]
	if !ok {
		t.Fatal("openai provider not found")
	}
	if openai.APIKey != "sk-test-key-123" {
		t.Errorf("openai.APIKey = %q, want %q", openai.APIKey, "sk-test-key-123")
	}
	if openai.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("openai.BaseURL = %q, want %q", openai.BaseURL, "https://api.openai.com/v1")
	}
	if openai.DefaultModel != "gpt-4-turbo" {
		t.Errorf("openai.DefaultModel = %q, want %q", openai.DefaultModel, "gpt-4-turbo")
	}
	if openai.RateLimit.RequestsPerMinute != 60 {
		t.Errorf("openai.RateLimit.RequestsPerMinute = %d, want 60", openai.RateLimit.RequestsPerMinute)
	}
	if openai.RateLimit.TokensPerMinute != 150000 {
		t.Errorf("openai.RateLimit.TokensPerMinute = %d, want 150000", openai.RateLimit.TokensPerMinute)
	}
	if openai.Retry.MaxAttempts != 3 {
		t.Errorf("openai.Retry.MaxAttempts = %d, want 3", openai.Retry.MaxAttempts)
	}
	if openai.Retry.InitialBackoff != 1*time.Second {
		t.Errorf("openai.Retry.InitialBackoff = %v, want %v", openai.Retry.InitialBackoff, 1*time.Second)
	}

	anthropic, ok := cfg.Providers["anthropic"]
	if !ok {
		t.Fatal("anthropic provider not found")
	}
	if anthropic.APIKey != "sk-ant-test-key" {
		t.Errorf("anthropic.APIKey = %q, want %q", anthropic.APIKey, "sk-ant-test-key")
	}
	if anthropic.DefaultModel != "claude-sonnet-4-20250514" {
		t.Errorf("anthropic.DefaultModel = %q, want %q", anthropic.DefaultModel, "claude-sonnet-4-20250514")
	}

	ollama, ok := cfg.Providers["ollama"]
	if !ok {
		t.Fatal("ollama provider not found")
	}
	if ollama.BaseURL != "http://localhost:11434" {
		t.Errorf("ollama.BaseURL = %q, want %q", ollama.BaseURL, "http://localhost:11434")
	}
	if ollama.DefaultModel != "llama3" {
		t.Errorf("ollama.DefaultModel = %q, want %q", ollama.DefaultModel, "llama3")
	}

	// Logging
	if cfg.Logging.Level != "debug" {
		t.Errorf("Logging.Level = %q, want %q", cfg.Logging.Level, "debug")
	}
	if cfg.Logging.Format != "json" {
		t.Errorf("Logging.Format = %q, want %q", cfg.Logging.Format, "json")
	}

	// Observability
	if !cfg.Observability.Tracing.Enabled {
		t.Error("Tracing.Enabled = false, want true")
	}
	if cfg.Observability.Tracing.Endpoint != "http://localhost:4318" {
		t.Errorf("Tracing.Endpoint = %q, want %q", cfg.Observability.Tracing.Endpoint, "http://localhost:4318")
	}
	if !cfg.Observability.Metrics.Enabled {
		t.Error("Metrics.Enabled = false, want true")
	}

	// Top-level
	if cfg.DefaultProvider != "openai" {
		t.Errorf("DefaultProvider = %q, want %q", cfg.DefaultProvider, "openai")
	}
	if cfg.DefaultModel != "gpt-4-turbo" {
		t.Errorf("DefaultModel = %q, want %q", cfg.DefaultModel, "gpt-4-turbo")
	}
	if cfg.MaxConcurrency != 10 {
		t.Errorf("MaxConcurrency = %d, want 10", cfg.MaxConcurrency)
	}
	if cfg.RequestTimeout != 2*time.Minute {
		t.Errorf("RequestTimeout = %v, want %v", cfg.RequestTimeout, 2*time.Minute)
	}
}

func TestLoadFromFile_EnvInterpolation(t *testing.T) {
	// Set environment variables
	t.Setenv("TEST_OPENAI_KEY", "sk-from-env-123")
	t.Setenv("TEST_ANTHROPIC_KEY", "sk-ant-from-env-456")

	yamlContent := `
providers:
  openai:
    api_key: ${TEST_OPENAI_KEY}
    default_model: gpt-4-turbo
  anthropic:
    api_key: ${TEST_ANTHROPIC_KEY}
    default_model: claude-sonnet-4-20250514
`

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	if cfg.Providers["openai"].APIKey != "sk-from-env-123" {
		t.Errorf("openai.APIKey = %q, want %q", cfg.Providers["openai"].APIKey, "sk-from-env-123")
	}
	if cfg.Providers["anthropic"].APIKey != "sk-ant-from-env-456" {
		t.Errorf("anthropic.APIKey = %q, want %q", cfg.Providers["anthropic"].APIKey, "sk-ant-from-env-456")
	}
}

func TestLoadFromFile_EnvInterpolationWithDefault(t *testing.T) {
	// Don't set the env var — test the default value
	yamlContent := `
providers:
  openai:
    api_key: ${NONEXISTENT_ORCHESTRA_TEST_VAR:-sk-fallback-key}
    default_model: gpt-4-turbo
logging:
  level: ${NONEXISTENT_LOG_LEVEL:-info}
`

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	if cfg.Providers["openai"].APIKey != "sk-fallback-key" {
		t.Errorf("openai.APIKey = %q, want %q", cfg.Providers["openai"].APIKey, "sk-fallback-key")
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("Logging.Level = %q, want %q", cfg.Logging.Level, "info")
	}
}

func TestLoadFromFile_EnvInterpolation_MissingNoDefault(t *testing.T) {
	yamlContent := `
providers:
  openai:
    api_key: ${DEFINITELY_NONEXISTENT_VAR_12345}
    default_model: gpt-4-turbo
`

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	// Missing env var with no default should result in empty string
	if cfg.Providers["openai"].APIKey != "" {
		t.Errorf("openai.APIKey = %q, want empty string", cfg.Providers["openai"].APIKey)
	}
}

func TestLoadFromFile_NonexistentFile(t *testing.T) {
	_, err := LoadFromFile("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("LoadFromFile() should return error for nonexistent file")
	}
}

func TestLoadFromFile_InvalidYAML(t *testing.T) {
	yamlContent := `
providers:
  openai:
    api_key: [invalid
    default_model: {broken
`

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("LoadFromFile() should return error for invalid YAML")
	}
}

func TestLoadFromFile_InvalidConfig(t *testing.T) {
	yamlContent := `
logging:
  level: verbose
`

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("LoadFromFile() should return error for invalid config")
	}
}

// --- LoadOrDefault ---

func TestLoadOrDefault_ExistingFile(t *testing.T) {
	yamlContent := `
logging:
  level: debug
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadOrDefault(path)
	if err != nil {
		t.Fatalf("LoadOrDefault() error = %v", err)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("Logging.Level = %q, want %q", cfg.Logging.Level, "debug")
	}
}

func TestLoadOrDefault_NonexistentFile(t *testing.T) {
	cfg, err := LoadOrDefault("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("LoadOrDefault() error = %v", err)
	}
	// Should return default config
	if cfg.Logging.GetLevel() != "info" {
		t.Errorf("default Logging.Level = %q, want %q", cfg.Logging.GetLevel(), "info")
	}
}

// --- LoadFromEnv ---

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("ORCHESTRA_DEFAULT_PROVIDER", "openai")
	t.Setenv("ORCHESTRA_DEFAULT_MODEL", "gpt-4-turbo")
	t.Setenv("ORCHESTRA_MAX_CONCURRENCY", "10")
	t.Setenv("ORCHESTRA_REQUEST_TIMEOUT", "3m")
	t.Setenv("ORCHESTRA_LOGGING_LEVEL", "debug")
	t.Setenv("ORCHESTRA_LOGGING_FORMAT", "text")
	t.Setenv("ORCHESTRA_LOGGING_OUTPUT", "stdout")
	t.Setenv("ORCHESTRA_TRACING_ENABLED", "true")
	t.Setenv("ORCHESTRA_TRACING_ENDPOINT", "http://jaeger:4318")
	t.Setenv("ORCHESTRA_METRICS_ENABLED", "true")
	t.Setenv("ORCHESTRA_METRICS_ENDPOINT", "http://prometheus:9090/metrics")
	t.Setenv("ORCHESTRA_PROVIDER_OPENAI_API_KEY", "sk-env-key")
	t.Setenv("ORCHESTRA_PROVIDER_OPENAI_BASE_URL", "https://api.openai.com/v1")
	t.Setenv("ORCHESTRA_PROVIDER_OPENAI_DEFAULT_MODEL", "gpt-4o")
	t.Setenv("ORCHESTRA_PROVIDER_ANTHROPIC_API_KEY", "sk-ant-env-key")

	cfg := LoadFromEnv()

	if cfg.DefaultProvider != "openai" {
		t.Errorf("DefaultProvider = %q, want %q", cfg.DefaultProvider, "openai")
	}
	if cfg.DefaultModel != "gpt-4-turbo" {
		t.Errorf("DefaultModel = %q, want %q", cfg.DefaultModel, "gpt-4-turbo")
	}
	if cfg.MaxConcurrency != 10 {
		t.Errorf("MaxConcurrency = %d, want 10", cfg.MaxConcurrency)
	}
	if cfg.RequestTimeout != 3*time.Minute {
		t.Errorf("RequestTimeout = %v, want %v", cfg.RequestTimeout, 3*time.Minute)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("Logging.Level = %q, want %q", cfg.Logging.Level, "debug")
	}
	if cfg.Logging.Format != "text" {
		t.Errorf("Logging.Format = %q, want %q", cfg.Logging.Format, "text")
	}
	if cfg.Logging.Output != "stdout" {
		t.Errorf("Logging.Output = %q, want %q", cfg.Logging.Output, "stdout")
	}
	if !cfg.Observability.Tracing.Enabled {
		t.Error("Tracing.Enabled = false, want true")
	}
	if cfg.Observability.Tracing.Endpoint != "http://jaeger:4318" {
		t.Errorf("Tracing.Endpoint = %q, want %q", cfg.Observability.Tracing.Endpoint, "http://jaeger:4318")
	}
	if !cfg.Observability.Metrics.Enabled {
		t.Error("Metrics.Enabled = false, want true")
	}
	if cfg.Observability.Metrics.Endpoint != "http://prometheus:9090/metrics" {
		t.Errorf("Metrics.Endpoint = %q, want %q", cfg.Observability.Metrics.Endpoint, "http://prometheus:9090/metrics")
	}

	// Provider env overrides
	openai, ok := cfg.Providers["openai"]
	if !ok {
		t.Fatal("openai provider not found")
	}
	if openai.APIKey != "sk-env-key" {
		t.Errorf("openai.APIKey = %q, want %q", openai.APIKey, "sk-env-key")
	}
	if openai.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("openai.BaseURL = %q, want %q", openai.BaseURL, "https://api.openai.com/v1")
	}
	if openai.DefaultModel != "gpt-4o" {
		t.Errorf("openai.DefaultModel = %q, want %q", openai.DefaultModel, "gpt-4o")
	}

	anthropic, ok := cfg.Providers["anthropic"]
	if !ok {
		t.Fatal("anthropic provider not found")
	}
	if anthropic.APIKey != "sk-ant-env-key" {
		t.Errorf("anthropic.APIKey = %q, want %q", anthropic.APIKey, "sk-ant-env-key")
	}
}

func TestLoadFromEnv_ProviderRateLimit(t *testing.T) {
	t.Setenv("ORCHESTRA_PROVIDER_OPENAI_API_KEY", "sk-test")
	t.Setenv("ORCHESTRA_PROVIDER_OPENAI_RATE_LIMIT_RPM", "100")
	t.Setenv("ORCHESTRA_PROVIDER_OPENAI_RATE_LIMIT_TPM", "200000")
	t.Setenv("ORCHESTRA_PROVIDER_OPENAI_RETRY_MAX_ATTEMPTS", "5")
	t.Setenv("ORCHESTRA_PROVIDER_OPENAI_RETRY_INITIAL_BACKOFF", "2s")

	cfg := LoadFromEnv()

	openai := cfg.Providers["openai"]
	if openai.RateLimit.RequestsPerMinute != 100 {
		t.Errorf("RPM = %d, want 100", openai.RateLimit.RequestsPerMinute)
	}
	if openai.RateLimit.TokensPerMinute != 200000 {
		t.Errorf("TPM = %d, want 200000", openai.RateLimit.TokensPerMinute)
	}
	if openai.Retry.MaxAttempts != 5 {
		t.Errorf("MaxAttempts = %d, want 5", openai.Retry.MaxAttempts)
	}
	if openai.Retry.InitialBackoff != 2*time.Second {
		t.Errorf("InitialBackoff = %v, want %v", openai.Retry.InitialBackoff, 2*time.Second)
	}
}

func TestLoadFromEnv_ProviderEnabled(t *testing.T) {
	t.Setenv("ORCHESTRA_PROVIDER_OLLAMA_BASE_URL", "http://localhost:11434")
	t.Setenv("ORCHESTRA_PROVIDER_OLLAMA_ENABLED", "false")

	cfg := LoadFromEnv()

	ollama := cfg.Providers["ollama"]
	if ollama.IsEnabled() {
		t.Error("ollama should be disabled")
	}
}

// --- Merge ---

func TestMerge_Empty(t *testing.T) {
	cfg := Merge()
	if cfg == nil {
		t.Fatal("Merge() returned nil")
	}
	// Should return default config
	if cfg.Logging.GetLevel() != "info" {
		t.Errorf("default level = %q, want %q", cfg.Logging.GetLevel(), "info")
	}
}

func TestMerge_Single(t *testing.T) {
	cfg := Merge(&Config{
		DefaultProvider: "openai",
		Logging: LoggingConfig{
			Level: "debug",
		},
	})

	if cfg.DefaultProvider != "openai" {
		t.Errorf("DefaultProvider = %q, want %q", cfg.DefaultProvider, "openai")
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("Logging.Level = %q, want %q", cfg.Logging.Level, "debug")
	}
}

func TestMerge_Multiple(t *testing.T) {
	base := &Config{
		DefaultProvider: "openai",
		DefaultModel:    "gpt-4",
		Providers: map[string]ProviderConfig{
			"openai": {
				APIKey:       "sk-base",
				DefaultModel: "gpt-4",
			},
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
	}

	override := &Config{
		DefaultModel: "gpt-4-turbo",
		Providers: map[string]ProviderConfig{
			"openai": {
				APIKey: "sk-override",
			},
			"anthropic": {
				APIKey:       "sk-ant-override",
				DefaultModel: "claude-sonnet-4-20250514",
			},
		},
		Logging: LoggingConfig{
			Level: "debug",
		},
	}

	cfg := Merge(base, override)

	// DefaultModel should be overridden
	if cfg.DefaultModel != "gpt-4-turbo" {
		t.Errorf("DefaultModel = %q, want %q", cfg.DefaultModel, "gpt-4-turbo")
	}

	// DefaultProvider should remain from base
	if cfg.DefaultProvider != "openai" {
		t.Errorf("DefaultProvider = %q, want %q", cfg.DefaultProvider, "openai")
	}

	// OpenAI API key should be overridden, default model preserved
	if cfg.Providers["openai"].APIKey != "sk-override" {
		t.Errorf("openai.APIKey = %q, want %q", cfg.Providers["openai"].APIKey, "sk-override")
	}
	if cfg.Providers["openai"].DefaultModel != "gpt-4" {
		t.Errorf("openai.DefaultModel = %q, want %q (preserved from base)", cfg.Providers["openai"].DefaultModel, "gpt-4")
	}

	// Anthropic should be added
	if cfg.Providers["anthropic"].APIKey != "sk-ant-override" {
		t.Errorf("anthropic.APIKey = %q, want %q", cfg.Providers["anthropic"].APIKey, "sk-ant-override")
	}

	// Logging should be merged: level overridden, format preserved
	if cfg.Logging.Level != "debug" {
		t.Errorf("Logging.Level = %q, want %q", cfg.Logging.Level, "debug")
	}
	if cfg.Logging.Format != "json" {
		t.Errorf("Logging.Format = %q, want %q (preserved from base)", cfg.Logging.Format, "json")
	}
}

func TestMerge_NilConfigs(t *testing.T) {
	cfg := Merge(nil, &Config{DefaultProvider: "openai"}, nil)
	if cfg.DefaultProvider != "openai" {
		t.Errorf("DefaultProvider = %q, want %q", cfg.DefaultProvider, "openai")
	}
}

func TestMerge_Observability(t *testing.T) {
	base := &Config{
		Observability: ObservabilityConfig{
			Tracing: TracingConfig{
				Enabled:      true,
				Endpoint:     "http://localhost:4318",
				ServiceName:  "base-service",
				SamplingRate: 0.5,
			},
			Metrics: MetricsConfig{
				Enabled:  true,
				Endpoint: "http://localhost:9090/metrics",
			},
		},
	}

	override := &Config{
		Observability: ObservabilityConfig{
			Tracing: TracingConfig{
				Endpoint:     "http://jaeger:4318",
				SamplingRate: 1.0,
			},
		},
	}

	cfg := Merge(base, override)

	if cfg.Observability.Tracing.Endpoint != "http://jaeger:4318" {
		t.Errorf("Tracing.Endpoint = %q, want %q", cfg.Observability.Tracing.Endpoint, "http://jaeger:4318")
	}
	if cfg.Observability.Tracing.ServiceName != "base-service" {
		t.Errorf("Tracing.ServiceName = %q, want %q (from base)", cfg.Observability.Tracing.ServiceName, "base-service")
	}
	if cfg.Observability.Tracing.SamplingRate != 1.0 {
		t.Errorf("Tracing.SamplingRate = %v, want %v", cfg.Observability.Tracing.SamplingRate, 1.0)
	}
	if !cfg.Observability.Tracing.Enabled {
		t.Error("Tracing.Enabled should be true (from base)")
	}
	if !cfg.Observability.Metrics.Enabled {
		t.Error("Metrics.Enabled should be true (from base)")
	}
}

// --- Internal helpers ---

func TestInterpolateEnvVars(t *testing.T) {
	t.Setenv("TEST_VAR_SIMPLE", "hello")
	t.Setenv("TEST_VAR_WITH_SPECIAL", "value with spaces & special chars!")

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no interpolation needed", "plain text", "plain text"},
		{"simple variable", "${TEST_VAR_SIMPLE}", "hello"},
		{"variable in text", "key: ${TEST_VAR_SIMPLE}", "key: hello"},
		{"multiple variables", "${TEST_VAR_SIMPLE} ${TEST_VAR_WITH_SPECIAL}", "hello value with spaces & special chars!"},
		{"missing var no default", "${NONEXISTENT_TEST_VAR_XYZ}", ""},
		{"missing var with default", "${NONEXISTENT_TEST_VAR_XYZ:-fallback}", "fallback"},
		{"empty default", "${NONEXISTENT_TEST_VAR_XYZ:-}", ""},
		{"var with dash in default", "${NONEXISTENT:-some-default-value}", "some-default-value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := interpolateEnvVars(tt.input)
			if got != tt.want {
				t.Errorf("interpolateEnvVars(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseEnvVarWithDefault(t *testing.T) {
	tests := []struct {
		input       string
		wantVar     string
		wantDefault string
	}{
		{"MY_VAR", "MY_VAR", ""},
		{"MY_VAR:-default", "MY_VAR", "default"},
		{"MY_VAR:-", "MY_VAR", ""},
		{"MY_VAR:-complex:default:value", "MY_VAR", "complex:default:value"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			varName, defaultVal := parseEnvVarWithDefault(tt.input)
			if varName != tt.wantVar {
				t.Errorf("varName = %q, want %q", varName, tt.wantVar)
			}
			if defaultVal != tt.wantDefault {
				t.Errorf("defaultVal = %q, want %q", defaultVal, tt.wantDefault)
			}
		})
	}
}

func TestBoolValue(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"1", true},
		{"yes", true},
		{"Yes", true},
		{"on", true},
		{"enabled", true},
		{"false", false},
		{"0", false},
		{"no", false},
		{"off", false},
		{"", false},
		{"random", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := boolValue(tt.input); got != tt.want {
				t.Errorf("boolValue(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSplitProviderEnvField(t *testing.T) {
	tests := []struct {
		input        string
		wantProvider string
		wantField    string
	}{
		{"OPENAI_API_KEY", "OPENAI", "API_KEY"},
		{"OPENAI_BASE_URL", "OPENAI", "BASE_URL"},
		{"OPENAI_DEFAULT_MODEL", "OPENAI", "DEFAULT_MODEL"},
		{"OPENAI_ENABLED", "OPENAI", "ENABLED"},
		{"OPENAI_RATE_LIMIT_RPM", "OPENAI", "RATE_LIMIT_RPM"},
		{"OPENAI_RATE_LIMIT_TPM", "OPENAI", "RATE_LIMIT_TPM"},
		{"OPENAI_RETRY_MAX_ATTEMPTS", "OPENAI", "RETRY_MAX_ATTEMPTS"},
		{"OPENAI_RETRY_INITIAL_BACKOFF", "OPENAI", "RETRY_INITIAL_BACKOFF"},
		{"MY_CUSTOM_PROVIDER_API_KEY", "MY_CUSTOM_PROVIDER", "API_KEY"},
		{"UNKNOWN_FIELD", "UNKNOWN_FIELD", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			provider, field := splitProviderEnvField(tt.input)
			if provider != tt.wantProvider {
				t.Errorf("provider = %q, want %q", provider, tt.wantProvider)
			}
			if field != tt.wantField {
				t.Errorf("field = %q, want %q", field, tt.wantField)
			}
		})
	}
}

// --- Integration ---

func TestLoadFromFile_FullIntegration(t *testing.T) {
	t.Setenv("TEST_ORCHESTRA_OPENAI_KEY", "sk-integration-test-key")

	yamlContent := `
providers:
  openai:
    api_key: ${TEST_ORCHESTRA_OPENAI_KEY}
    base_url: https://api.openai.com/v1
    default_model: gpt-4-turbo
    rate_limit:
      requests_per_minute: 60
      tokens_per_minute: 150000
    retry:
      max_attempts: 3
      initial_backoff: 1s

  anthropic:
    api_key: ${NONEXISTENT_ANTHROPIC_KEY:-sk-ant-default}
    default_model: claude-sonnet-4-20250514

logging:
  level: info
  format: json

observability:
  tracing:
    enabled: true
    endpoint: http://localhost:4318
    sampling_rate: 0.8
  metrics:
    enabled: false

default_provider: openai
max_concurrency: 5
request_timeout: 3m

agent_defaults:
  max_turns: 20
  system_prompt: "You are a helpful assistant."
`

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	// Verify interpolated env var
	if cfg.Providers["openai"].APIKey != "sk-integration-test-key" {
		t.Errorf("openai API key = %q, want env-interpolated value", cfg.Providers["openai"].APIKey)
	}

	// Verify fallback default
	if cfg.Providers["anthropic"].APIKey != "sk-ant-default" {
		t.Errorf("anthropic API key = %q, want default value", cfg.Providers["anthropic"].APIKey)
	}

	// Validate passes
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() error = %v", err)
	}

	// Agent defaults
	if cfg.AgentDefaults.MaxTurns != 20 {
		t.Errorf("AgentDefaults.MaxTurns = %d, want 20", cfg.AgentDefaults.MaxTurns)
	}
	if cfg.AgentDefaults.SystemPrompt != "You are a helpful assistant." {
		t.Errorf("AgentDefaults.SystemPrompt = %q, want %q", cfg.AgentDefaults.SystemPrompt, "You are a helpful assistant.")
	}
}

// --- Helper ---

func boolPtr(b bool) *bool {
	return &b
}
