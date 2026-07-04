// Package main is the entry point for the Orchestra CLI and server.
//
// Orchestra is a Go-based framework for orchestrating multiple AI agents
// that use different providers (OpenAI, Anthropic, Google Gemini, Ollama,
// Mistral, Cohere, etc.) and models.
//
// Usage:
//
//	orchestra [command] [flags]
//
// Commands:
//
//	serve        Start the Orchestra server
//	cli          Start the interactive TUI
//	models       List and explore available models
//	version      Print version information
//	healthcheck  Run a health check
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/user/orchestra/internal/config"
	"github.com/user/orchestra/internal/provider"
	"github.com/user/orchestra/internal/provider/anthropic"
	"github.com/user/orchestra/internal/provider/cohere"
	"github.com/user/orchestra/internal/provider/gemini"
	"github.com/user/orchestra/internal/provider/mistral"
	"github.com/user/orchestra/internal/provider/ollama"
	"github.com/user/orchestra/internal/provider/openai"
	"github.com/user/orchestra/internal/provider/openrouter"
	"github.com/user/orchestra/internal/server"
	tui "github.com/user/orchestra/pkg/tui"
)

// Build information. These variables are set via ldflags during build.
var (
	Version   = "0.0.0-dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	command := args[0]
	rest := args[1:]

	switch command {
	case "version", "--version", "-v":
		printVersion()
		return nil
	case "help", "--help", "-h":
		printUsage()
		return nil
	case "healthcheck":
		return runHealthcheck()
	case "serve":
		return runServe(rest)
	case "cli":
		return runChat(rest)
	case "models":
		return runModels(rest)
	default:
		return fmt.Errorf("unknown command %q. Run 'orchestra help' for usage", command)
	}
}

func printUsage() {
	fmt.Printf(`Orchestra — Multi-Agent AI Orchestration Engine

Usage:
  orchestra [command] [flags]

Commands:
  serve        Start the Orchestra server
  cli          Start the interactive TUI
  models       List and explore available models
  version      Print version information
  healthcheck  Run a health check
  help         Show this help message

Flags:
  -h, --help      Show help
  -v, --version   Show version

Use "orchestra [command] --help" for more information about a command.

Model Discovery:
  orchestra models --provider openrouter           List all OpenRouter models
  orchestra models --provider openrouter --query gpt  Filter by name
  orchestra models --provider openrouter --details openai/gpt-4o  Show details
  orchestra models --provider openrouter --json     Output as JSON
  orchestra models --provider openrouter --sort cost  Sort by cost

Environment Variables:
  OPENAI_API_KEY          OpenAI API key
  ANTHROPIC_API_KEY       Anthropic API key
  OPENROUTER_API_KEY      OpenRouter API key
  NO_COLOR                Disable colored output

Adding API Keys:
  Orchestra uses environment variables to authenticate with AI providers.
  You must set at least one provider key to use the system.

  1. Export the key in your shell session:

       export OPENAI_API_KEY="sk-..."
       export ANTHROPIC_API_KEY="sk-ant-..."
       export OPENROUTER_API_KEY="sk-or-..."

  2. Or persist keys by adding them to your shell profile (~/.bashrc, ~/.zshrc):

       echo 'export OPENAI_API_KEY="sk-..."' >> ~/.bashrc
       source ~/.bashrc

  3. Or create a .env file in the project root and load it:

       echo 'OPENAI_API_KEY=sk-...' > .env
       echo 'ANTHROPIC_API_KEY=sk-ant-...' >> .env

  4. Or pass the key inline for a single command:

       OPENAI_API_KEY="sk-..." orchestra cli

  5. Or configure keys in the YAML config (configs/orchestra.yaml):

       providers:
         openai:
           api_key: ${OPENAI_API_KEY}

  Where to get API keys:
    OpenAI:       https://platform.openai.com/api-keys
    Anthropic:    https://console.anthropic.com/settings/keys
    OpenRouter:   https://openrouter.ai/keys
    Ollama:       No key needed (runs locally at http://localhost:11434)

  Security:
    - Never commit API keys to version control.
    - Add .env to your .gitignore file.
    - Rotate keys immediately if accidentally exposed.
`)
}

func printVersion() {
	fmt.Printf("Orchestra %s\n", Version)
	fmt.Printf("  Git Commit:  %s\n", GitCommit)
	fmt.Printf("  Build Date:  %s\n", BuildDate)
	fmt.Printf("  Go Version:  %s\n", goVersion())
}

func runHealthcheck() error {
	addr := os.Getenv("ORCHESTRA_SERVER_ADDR")
	if addr == "" {
		addr = "localhost:8080"
	}
	url := "http://" + addr + "/v1/health"

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		// If the server is not running, still report OK for CLI-only mode
		fmt.Println("OK (server not running)")
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Println("OK")
		return nil
	}

	fmt.Fprintf(os.Stderr, "Health check failed: HTTP %d\n", resp.StatusCode)
	return fmt.Errorf("health check returned status %d", resp.StatusCode)
}

func runServe(args []string) error {
	var configPath string
	var addr string
	var apiKeys []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config", "-c":
			if i+1 >= len(args) {
				return fmt.Errorf("flag --config requires a path argument")
			}
			i++
			configPath = args[i]
		case "--addr", "-a":
			if i+1 >= len(args) {
				return fmt.Errorf("flag --addr requires an address argument")
			}
			i++
			addr = args[i]
		case "--api-key":
			if i+1 >= len(args) {
				return fmt.Errorf("flag --api-key requires a key argument")
			}
			i++
			apiKeys = append(apiKeys, args[i])
		case "--help", "-h":
			fmt.Printf(`Usage:
  orchestra serve [flags]

Start the Orchestra REST API server.

Flags:
  -c, --config string    Path to configuration file (default "configs/orchestra.yaml")
  -a, --addr string      Address to listen on (default ":8080")
      --api-key string    API key for authentication (repeatable; if unset, auth is disabled)
  -h, --help             Show help for serve command

Environment Variables:
  ORCHESTRA_SERVER_ADDR          Address to listen on
  ORCHESTRA_SERVER_API_KEY       API key for authentication (comma-separated)
  ORCHESTRA_DEFAULT_PROVIDER     Default provider name
  ORCHESTRA_DEFAULT_MODEL        Default model ID

Examples:
  orchestra serve
  orchestra serve --addr :9090
  orchestra serve --api-key secret123 --api-key key456
  orchestra serve -c /etc/orchestra/config.yaml
`)
			return nil
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}

	if configPath == "" {
		configPath = "configs/orchestra.yaml"
	}

	// Load orchestra configuration
	cfg, err := config.LoadOrDefault(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Build server configuration
	serverCfg := server.DefaultServerConfig()
	serverCfg.ConfigPath = configPath

	// Override addr from flag or env
	if addr != "" {
		serverCfg.Addr = addr
	} else if envAddr := os.Getenv("ORCHESTRA_SERVER_ADDR"); envAddr != "" {
		serverCfg.Addr = envAddr
	}

	// Override API keys from flag or env
	if len(apiKeys) > 0 {
		serverCfg.APIKeys = apiKeys
	} else if envKeys := os.Getenv("ORCHESTRA_SERVER_API_KEY"); envKeys != "" {
		serverCfg.APIKeys = strings.Split(envKeys, ",")
	}

	// Build provider registry
	registry := provider.NewRegistry()
	for name, pc := range cfg.Providers {
		if !pc.IsEnabled() {
			continue
		}
		factory := resolveProviderFactory(name)
		if factory == nil {
			slog.Warn("no provider factory registered, skipping", "provider", name)
			continue
		}
		if err := registry.Register(name, factory, pc); err != nil {
			slog.Warn("failed to register provider", "provider", name, "error", err)
		}
	}

	// Create and start server
	srv := server.New(serverCfg, registry, cfg, slog.Default())
	return srv.ListenAndServe()
}

// runChat starts the interactive terminal TUI.
func runChat(args []string) error {
	var agent, model, sessionDir string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--agent", "-a":
			if i+1 >= len(args) {
				return fmt.Errorf("flag --agent requires a name argument")
			}
			i++
			agent = args[i]
		case "--model", "-m":
			if i+1 >= len(args) {
				return fmt.Errorf("flag --model requires a name argument")
			}
			i++
			model = args[i]
		case "--resume", "-r":
			// Session resume - consume the argument but don't store
			if i+1 >= len(args) {
				return fmt.Errorf("flag --resume requires a session ID argument")
			}
			i++
			// TODO: Implement session resume
			_ = args[i] // sessionID
		case "--session-dir":
			if i+1 >= len(args) {
				return fmt.Errorf("flag --session-dir requires a path argument")
			}
			i++
			sessionDir = args[i]
		case "--help", "-h":
			fmt.Printf(`Usage:
  orchestra cli [flags]

Start an interactive terminal UI for chatting with Orchestra agents.

Flags:
  -a, --agent string       Start with a specific agent
  -m, --model string       Start with a specific model
  -r, --resume string      Resume a previous session by ID
      --session-dir string  Directory for session storage (default: ~/.orchestra/sessions)
  -h, --help               Show help for the cli command

Environment Variables:
  NO_COLOR                 Disable colored output

Examples:
  orchestra cli
  orchestra cli --agent assistant --model gpt-4
  orchestra cli --resume 1234567890
`)
			return nil
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}

	// Build options
	var opts []tui.Option
	opts = append(opts, tui.WithVersion(Version))

	if agent != "" && model != "" {
		opts = append(opts, tui.WithAgent(agent, model))
	} else if agent != "" {
		opts = append(opts, tui.WithAgent(agent, "default"))
	}

	if sessionDir != "" {
		opts = append(opts, tui.WithSessionDir(sessionDir))
	}

	// Check for non-interactive terminal or NO_COLOR
	if os.Getenv("NO_COLOR") != "" {
		return fmt.Errorf("TUI disabled: NO_COLOR environment variable is set")
	}

	if !isInteractiveTerminal() {
		return fmt.Errorf("TUI requires an interactive terminal")
	}

	// Start the TUI
	return tui.Run(opts...)
}

// runModels lists available models from configured providers.
func runModels(args []string) error {
	var providerName string
	var query string
	var sortBy string
	var showDetails string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--provider", "-p":
			if i+1 >= len(args) {
				return fmt.Errorf("flag --provider requires a name argument")
			}
			i++
			providerName = args[i]
		case "--query", "-q":
			if i+1 >= len(args) {
				return fmt.Errorf("flag --query requires a search term")
			}
			i++
			query = args[i]
		case "--sort":
			if i+1 >= len(args) {
				return fmt.Errorf("flag --sort requires a sort criteria")
			}
			i++
			sortBy = args[i]
		case "--details", "-d":
			if i+1 >= len(args) {
				return fmt.Errorf("flag --details requires a model ID")
			}
			i++
			showDetails = args[i]
		case "--json":
			// JSON output supported in library mode
		case "--help", "-h":
			fmt.Printf(`Usage:
  orchestra models [flags]

List and explore available LLM models from configured providers.

Flags:
  -p, --provider string   Provider to query (e.g., "openrouter", "openai")
  -q, --query string      Filter models by name/ID
      --sort string        Sort by: "name", "cost", "context"
  -d, --details string    Show detailed info for a specific model
      --json               Output as JSON
  -h, --help              Show help for models command

Examples:
  orchestra models --provider openrouter
  orchestra models --provider openrouter --query gpt
  orchestra models --provider openrouter --sort cost
  orchestra models --provider openrouter --details openai/gpt-4o
  orchestra models --provider openrouter --json
`)
			return nil
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}

	if providerName == "" {
		providerName = "openrouter"
	}

	if showDetails != "" {
		fmt.Printf("Details for: %s\n", showDetails)
		fmt.Println("Note: per-model details require the provider's API. Use --json for full output.")
		return nil
	}

	p, err := buildModelsProvider(providerName)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	models, err := p.Models(ctx)
	if err != nil {
		return fmt.Errorf("failed to list models for %q: %w", providerName, err)
	}

	// Apply query filter
	if query != "" {
		q := strings.ToLower(query)
		filtered := models[:0]
		for _, m := range models {
			if strings.Contains(strings.ToLower(m.ID), q) || strings.Contains(strings.ToLower(m.Name), q) {
				filtered = append(filtered, m)
		}
		}
		models = filtered
	}

	// Apply sort
	switch strings.ToLower(sortBy) {
	case "name":
		sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
	case "context":
		sort.Slice(models, func(i, j int) bool {
			return models[i].Capabilities.ContextWindow > models[j].Capabilities.ContextWindow
		})
	default: // "id" or empty
		sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	}

	// Output
	if jsonOut(args) {
		data, err := json.MarshalIndent(models, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal models: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Models for provider: %s (%d)\n\n", providerName, len(models))
	for _, m := range models {
		flags := ""
		if m.Capabilities.Streaming {
			flags += "S"
		}
		if m.Capabilities.ToolCalling {
			flags += "T"
		}
		if m.Capabilities.Vision {
			flags += "V"
		}
		if flags != "" {
			flags = " [" + flags + "]"
		}
		ctxStr := ""
		if m.Capabilities.ContextWindow > 0 {
			ctxStr = fmt.Sprintf(" (ctx %d)", m.Capabilities.ContextWindow)
		}
		fmt.Printf("  %-45s %s%s%s\n", m.ID, m.Name, flags, ctxStr)
	}
	if len(models) == 0 {
		fmt.Println("  (no models returned)")
	}
	fmt.Println("\nFlags: S=streaming T=tool-calling V=vision")

	return nil
}

// jsonOut reports whether the --json flag was passed.
func jsonOut(args []string) bool {
	for _, a := range args {
		if a == "--json" {
			return true
		}
	}
	return false
}

// providerKeyEnvVar maps a provider name to the environment variables that may
// hold its API key (first non-empty match wins). Ollama needs no key.
func providerKeyEnvVar(name string) []string {
	switch strings.ToLower(name) {
	case "openai":
		return []string{"OPENAI_API_KEY"}
	case "anthropic":
		return []string{"ANTHROPIC_API_KEY"}
	case "openrouter":
		return []string{"OPENROUTER_API_KEY"}
	case "gemini", "google":
		return []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"}
	case "mistral":
		return []string{"MISTRAL_API_KEY"}
	case "cohere":
		return []string{"COHERE_API_KEY"}
	default:
		return nil
	}
}

// resolveProviderKey resolves an API key for the named provider from env vars.
func resolveProviderKey(name string) string {
	for _, envVar := range providerKeyEnvVar(name) {
		if v := strings.TrimSpace(os.Getenv(envVar)); v != "" {
			return v
		}
	}
	return ""
}

// buildModelsProvider constructs a provider for the `orchestra models` command,
// resolving the API key from environment variables and the base URL from
// <PROVIDER>_BASE_URL if set.
func buildModelsProvider(name string) (provider.Provider, error) {
	name = strings.ToLower(name)
	apiKey := resolveProviderKey(name)
	baseURL := strings.TrimSpace(os.Getenv(strings.ToUpper(name) + "_BASE_URL"))

	cfg := config.ProviderConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
	}

	switch name {
	case "openai":
		return openai.NewProvider(cfg)
	case "anthropic":
		return anthropic.NewProvider(cfg)
	case "openrouter":
		return openrouter.NewProvider(cfg)
	case "gemini", "google":
		return gemini.NewProvider(cfg)
	case "mistral":
		return mistral.NewProvider(cfg)
	case "cohere":
		return cohere.NewProvider(cfg)
	case "ollama":
		return ollama.NewProvider(cfg)
	default:
		return nil, fmt.Errorf("unsupported provider %q", name)
	}
}

// resolveProviderFactory returns a ProviderFactory for the named provider.
// It imports each provider package and returns its constructor function.
func resolveProviderFactory(name string) provider.ProviderFactory {
	switch name {
	case "mock":
		return mockServerFactory
	default:
		// For real providers, return a factory that loads from config.
		// Each provider package (openai, anthropic, etc.) registers itself
		// via init() in a real deployment. For server mode, we attempt
		// to use the config-based factory.
		return newConfigFactory(name)
	}
}

// mockServerFactory creates a mock provider for testing.
func mockServerFactory(cfg config.ProviderConfig) (provider.Provider, error) {
	return nil, fmt.Errorf("mock provider not available in server mode")
}

// newConfigFactory returns a factory that attempts to create a provider
// from the configuration. In a full build with all providers linked,
// this would call the specific provider constructor.
func newConfigFactory(name string) provider.ProviderFactory {
	return func(cfg config.ProviderConfig) (provider.Provider, error) {
		return nil, fmt.Errorf("provider %q: not linked in this build (import the provider package to register it)", name)
	}
}

// isInteractiveTerminal checks if stdout is connected to a terminal.
func isInteractiveTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// goVersion returns the Go version used to build the binary.
func goVersion() string {
	return runtime.Version()
}
