package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// envVarPattern matches ${VAR} or ${VAR:-default} patterns in strings.
var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// LoadFromFile reads configuration from a YAML file, performs environment
// variable interpolation, and returns the parsed Config.
//
// Environment variables in the YAML are expressed as ${ENV_VAR} or
// ${ENV_VAR:-default_value}. For example:
//
//	api_key: ${OPENAI_API_KEY}
//	api_key: ${OPENAI_API_KEY:-sk-default}
//
// Returns an error if the file cannot be read, parsed, or if validation fails.
func LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read config file %q: %w", path, err)
	}

	// Interpolate environment variables
	expanded := interpolateEnvVars(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config file %q: %w", path, err)
	}

	// Merge environment variable overrides
	applyEnvOverrides(&cfg)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}

// LoadFromEnv loads configuration exclusively from environment variables
// prefixed with ORCHESTRA_. This is useful for containerized deployments
// where configuration is injected via environment.
//
// Supported environment variables:
//
//	ORCHESTRA_DEFAULT_PROVIDER        -> Config.DefaultProvider
//	ORCHESTRA_DEFAULT_MODEL           -> Config.DefaultModel
//	ORCHESTRA_MAX_CONCURRENCY         -> Config.MaxConcurrency
//	ORCHESTRA_REQUEST_TIMEOUT          -> Config.RequestTimeout (duration string)
//	ORCHESTRA_LOGGING_LEVEL           -> Config.Logging.Level
//	ORCHESTRA_LOGGING_FORMAT          -> Config.Logging.Format
//	ORCHESTRA_LOGGING_OUTPUT          -> Config.Logging.Output
//	ORCHESTRA_TRACING_ENABLED         -> Config.Observability.Tracing.Enabled
//	ORCHESTRA_TRACING_ENDPOINT        -> Config.Observability.Tracing.Endpoint
//	ORCHESTRA_METRICS_ENABLED         -> Config.Observability.Metrics.Enabled
//	ORCHESTRA_METRICS_ENDPOINT        -> Config.Observability.Metrics.Endpoint
//	ORCHESTRA_PROVIDER_<NAME>_API_KEY -> Providers[<name>].APIKey
//	ORCHESTRA_PROVIDER_<NAME>_BASE_URL -> Providers[<name>].BaseURL
//	ORCHESTRA_PROVIDER_<NAME>_DEFAULT_MODEL -> Providers[<name>].DefaultModel
//	ORCHESTRA_PROVIDER_<NAME>_ENABLED -> Providers[<name>].Enabled
//	ORCHESTRA_PROVIDER_<NAME>_RATE_LIMIT_RPM -> Providers[<name>].RateLimit.RequestsPerMinute
//	ORCHESTRA_PROVIDER_<NAME>_RETRY_MAX_ATTEMPTS -> Providers[<name>].Retry.MaxAttempts
func LoadFromEnv() *Config {
	cfg := DefaultConfig()

	// Top-level settings
	if v := os.Getenv("ORCHESTRA_DEFAULT_PROVIDER"); v != "" {
		cfg.DefaultProvider = v
	}
	if v := os.Getenv("ORCHESTRA_DEFAULT_MODEL"); v != "" {
		cfg.DefaultModel = v
	}
	if v := os.Getenv("ORCHESTRA_MAX_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxConcurrency = n
		}
	}
	if v := os.Getenv("ORCHESTRA_REQUEST_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.RequestTimeout = d
		}
	}

	// Logging
	if v := os.Getenv("ORCHESTRA_LOGGING_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
	if v := os.Getenv("ORCHESTRA_LOGGING_FORMAT"); v != "" {
		cfg.Logging.Format = v
	}
	if v := os.Getenv("ORCHESTRA_LOGGING_OUTPUT"); v != "" {
		cfg.Logging.Output = v
	}

	// Tracing
	if v := os.Getenv("ORCHESTRA_TRACING_ENABLED"); v != "" {
		cfg.Observability.Tracing.Enabled = boolValue(v)
	}
	if v := os.Getenv("ORCHESTRA_TRACING_ENDPOINT"); v != "" {
		cfg.Observability.Tracing.Endpoint = v
	}

	// Metrics
	if v := os.Getenv("ORCHESTRA_METRICS_ENABLED"); v != "" {
		cfg.Observability.Metrics.Enabled = boolValue(v)
	}
	if v := os.Getenv("ORCHESTRA_METRICS_ENDPOINT"); v != "" {
		cfg.Observability.Metrics.Endpoint = v
	}

	// Provider configs from env
	applyEnvOverrides(cfg)

	return cfg
}

// LoadOrDefault attempts to load configuration from the given file path.
// If the file does not exist, it returns the default configuration.
// Other errors (parse errors, validation errors) are propagated.
func LoadOrDefault(path string) (*Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return DefaultConfig(), nil
	}
	return LoadFromFile(path)
}

// Merge merges multiple Configs together. Later configs override earlier ones.
// Map fields (Providers, Extra) are merged key-by-key rather than replaced.
// If no configs are provided, the default config is returned.
func Merge(configs ...*Config) *Config {
	result := DefaultConfig()

	for _, cfg := range configs {
		if cfg == nil {
			continue
		}

		// Merge providers map
		if cfg.Providers != nil {
			if result.Providers == nil {
				result.Providers = make(map[string]ProviderConfig)
			}
			for name, pc := range cfg.Providers {
				existing, ok := result.Providers[name]
				if ok {
					result.Providers[name] = mergeProviderConfig(existing, pc)
				} else {
					result.Providers[name] = pc
				}
			}
		}

		// Simple field overrides
		if cfg.DefaultProvider != "" {
			result.DefaultProvider = cfg.DefaultProvider
		}
		if cfg.DefaultModel != "" {
			result.DefaultModel = cfg.DefaultModel
		}
		if cfg.MaxConcurrency != 0 {
			result.MaxConcurrency = cfg.MaxConcurrency
		}
		if cfg.RequestTimeout != 0 {
			result.RequestTimeout = cfg.RequestTimeout
		}

		// Logging
		result.Logging = mergeLoggingConfig(result.Logging, cfg.Logging)

		// Observability
		result.Observability = mergeObservabilityConfig(result.Observability, cfg.Observability)

		// Agent defaults
		result.AgentDefaults = mergeAgentDefaults(result.AgentDefaults, cfg.AgentDefaults)
	}

	return result
}

// --- Internal helpers ---

// interpolateEnvVars replaces ${VAR} and ${VAR:-default} patterns in the
// input string with the corresponding environment variable values.
func interpolateEnvVars(input string) string {
	return envVarPattern.ReplaceAllStringFunc(input, func(match string) string {
		inner := match[2 : len(match)-1] // strip ${ and }

		// Check for default value syntax: ${VAR:-default}
		varName, defaultVal := parseEnvVarWithDefault(inner)

		value := os.Getenv(varName)
		if value == "" {
			return defaultVal
		}
		return value
	})
}

// parseEnvVarWithDefault splits a "VAR" or "VAR:-default" string into the
// variable name and the default value.
func parseEnvVarWithDefault(s string) (varName, defaultVal string) {
	const separator = ":-"
	idx := strings.Index(s, separator)
	if idx < 0 {
		return s, ""
	}
	return s[:idx], s[idx+len(separator):]
}

// applyEnvOverrides applies environment variable overrides to the config.
// This handles both top-level env vars and provider-specific ones.
func applyEnvOverrides(cfg *Config) {
	if cfg.Providers == nil {
		cfg.Providers = make(map[string]ProviderConfig)
	}

	// Scan known provider names from env
	// Format: ORCHESTRA_PROVIDER_<NAME>_API_KEY
	envVars := os.Environ()
	for _, envVar := range envVars {
		if !strings.HasPrefix(envVar, "ORCHESTRA_PROVIDER_") {
			continue
		}

		parts := strings.SplitN(envVar, "=", 2)
		key := parts[0]
		value := ""
		if len(parts) == 2 {
			value = parts[1]
		}

		// Parse: ORCHESTRA_PROVIDER_<NAME>_<FIELD>
		remaining := strings.TrimPrefix(key, "ORCHESTRA_PROVIDER_")
		providerName, field := splitProviderEnvField(remaining)
		if providerName == "" || field == "" {
			continue
		}

		providerName = strings.ToLower(providerName)

		pc, ok := cfg.Providers[providerName]
		if !ok {
			pc = ProviderConfig{}
		}

		switch field {
		case "API_KEY":
			pc.APIKey = value
		case "BASE_URL":
			pc.BaseURL = value
		case "DEFAULT_MODEL":
			pc.DefaultModel = value
		case "ORGANIZATION_ID":
			pc.OrganizationID = value
		case "ENABLED":
			b := boolValue(value)
			pc.Enabled = &b
		case "RATE_LIMIT_RPM":
			if n, err := strconv.Atoi(value); err == nil {
				pc.RateLimit.RequestsPerMinute = n
			}
		case "RATE_LIMIT_TPM":
			if n, err := strconv.Atoi(value); err == nil {
				pc.RateLimit.TokensPerMinute = n
			}
		case "RETRY_MAX_ATTEMPTS":
			if n, err := strconv.Atoi(value); err == nil {
				pc.Retry.MaxAttempts = n
			}
		case "RETRY_INITIAL_BACKOFF":
			if d, err := time.ParseDuration(value); err == nil {
				pc.Retry.InitialBackoff = d
			}
		}

		cfg.Providers[providerName] = pc
	}
}

// splitProviderEnvField splits "OPENAI_API_KEY" into ("OPENAI", "API_KEY").
// It tries the longest known field suffix first.
func splitProviderEnvField(s string) (provider, field string) {
	knownFields := []string{
		"API_KEY",
		"BASE_URL",
		"DEFAULT_MODEL",
		"ORGANIZATION_ID",
		"ENABLED",
		"RATE_LIMIT_RPM",
		"RATE_LIMIT_TPM",
		"RETRY_MAX_ATTEMPTS",
		"RETRY_INITIAL_BACKOFF",
	}

	for _, f := range knownFields {
		if strings.HasSuffix(s, "_"+f) {
			provider = s[:len(s)-len(f)-1]
			field = f
			return
		}
	}
	return s, ""
}

// boolValue parses a string as a boolean, accepting common variations.
func boolValue(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}

// mergeProviderConfig merges two ProviderConfigs, with 'override' taking
// precedence over 'base'.
func mergeProviderConfig(base, override ProviderConfig) ProviderConfig {
	result := base

	if override.APIKey != "" {
		result.APIKey = override.APIKey
	}
	if override.BaseURL != "" {
		result.BaseURL = override.BaseURL
	}
	if override.DefaultModel != "" {
		result.DefaultModel = override.DefaultModel
	}
	if override.OrganizationID != "" {
		result.OrganizationID = override.OrganizationID
	}
	if override.Enabled != nil {
		result.Enabled = override.Enabled
	}

	// Rate limit
	if override.RateLimit.RequestsPerMinute != 0 {
		result.RateLimit.RequestsPerMinute = override.RateLimit.RequestsPerMinute
	}
	if override.RateLimit.TokensPerMinute != 0 {
		result.RateLimit.TokensPerMinute = override.RateLimit.TokensPerMinute
	}
	if override.RateLimit.BurstSize != 0 {
		result.RateLimit.BurstSize = override.RateLimit.BurstSize
	}
	if override.RateLimit.Enabled != nil {
		result.RateLimit.Enabled = override.RateLimit.Enabled
	}

	// Retry
	if override.Retry.MaxAttempts != 0 {
		result.Retry.MaxAttempts = override.Retry.MaxAttempts
	}
	if override.Retry.InitialBackoff != 0 {
		result.Retry.InitialBackoff = override.Retry.InitialBackoff
	}
	if override.Retry.MaxBackoff != 0 {
		result.Retry.MaxBackoff = override.Retry.MaxBackoff
	}
	if override.Retry.BackoffMultiplier != 0 {
		result.Retry.BackoffMultiplier = override.Retry.BackoffMultiplier
	}
	if override.Retry.Enabled != nil {
		result.Retry.Enabled = override.Retry.Enabled
	}

	// Models map
	if override.Models != nil {
		if result.Models == nil {
			result.Models = make(map[string]ModelConfig)
		}
		for k, v := range override.Models {
			result.Models[k] = v
		}
	}

	// Extra
	if override.Extra != nil {
		if result.Extra == nil {
			result.Extra = make(map[string]any)
		}
		for k, v := range override.Extra {
			result.Extra[k] = v
		}
	}

	return result
}

// mergeLoggingConfig merges two LoggingConfigs.
func mergeLoggingConfig(base, override LoggingConfig) LoggingConfig {
	result := base
	if override.Level != "" {
		result.Level = override.Level
	}
	if override.Format != "" {
		result.Format = override.Format
	}
	if override.Output != "" {
		result.Output = override.Output
	}
	if override.AddSource {
		result.AddSource = override.AddSource
	}
	if override.TimeFormat != "" {
		result.TimeFormat = override.TimeFormat
	}
	return result
}

// mergeObservabilityConfig merges two ObservabilityConfigs.
func mergeObservabilityConfig(base, override ObservabilityConfig) ObservabilityConfig {
	result := base

	// Tracing
	if override.Tracing.Endpoint != "" {
		result.Tracing.Endpoint = override.Tracing.Endpoint
	}
	if override.Tracing.ServiceName != "" {
		result.Tracing.ServiceName = override.Tracing.ServiceName
	}
	if override.Tracing.SamplingRate != 0 {
		result.Tracing.SamplingRate = override.Tracing.SamplingRate
	}
	if override.Tracing.Propagator != "" {
		result.Tracing.Propagator = override.Tracing.Propagator
	}
	if override.Tracing.Insecure {
		result.Tracing.Insecure = override.Tracing.Insecure
	}
	// If tracing is explicitly enabled in override, enable it
	if override.Tracing.Enabled {
		result.Tracing.Enabled = true
	}
	if override.Tracing.Headers != nil {
		result.Tracing.Headers = override.Tracing.Headers
	}

	// Metrics
	if override.Metrics.Endpoint != "" {
		result.Metrics.Endpoint = override.Metrics.Endpoint
	}
	if override.Metrics.ServiceName != "" {
		result.Metrics.ServiceName = override.Metrics.ServiceName
	}
	if override.Metrics.Namespace != "" {
		result.Metrics.Namespace = override.Metrics.Namespace
	}
	if override.Metrics.ExportInterval != 0 {
		result.Metrics.ExportInterval = override.Metrics.ExportInterval
	}
	if override.Metrics.ExportTimeout != 0 {
		result.Metrics.ExportTimeout = override.Metrics.ExportTimeout
	}
	if override.Metrics.Enabled {
		result.Metrics.Enabled = true
	}

	return result
}

// mergeAgentDefaults merges two AgentDefaults.
func mergeAgentDefaults(base, override AgentDefaults) AgentDefaults {
	result := base

	if override.MaxTurns != 0 {
		result.MaxTurns = override.MaxTurns
	}
	if override.Temperature != nil {
		result.Temperature = override.Temperature
	}
	if override.TopP != nil {
		result.TopP = override.TopP
	}
	if override.MaxTokens != nil {
		result.MaxTokens = override.MaxTokens
	}
	if override.SystemPrompt != "" {
		result.SystemPrompt = override.SystemPrompt
	}
	if override.Timeout != 0 {
		result.Timeout = override.Timeout
	}
	if override.RetryOnFailure {
		result.RetryOnFailure = override.RetryOnFailure
	}
	if override.Extra != nil {
		if result.Extra == nil {
			result.Extra = make(map[string]any)
		}
		for k, v := range override.Extra {
			result.Extra[k] = v
		}
	}

	return result
}
