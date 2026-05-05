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
//	chat         Start the interactive TUI
//	version      Print version information
//	healthcheck  Run a health check
package main

import (
	"fmt"
	"os"
	"runtime"

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
	case "chat":
		return runChat(rest)
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
  chat         Start the interactive TUI
  version      Print version information
  healthcheck  Run a health check
  help         Show this help message

Flags:
  -h, --help      Show help
  -v, --version   Show version

Use "orchestra [command] --help" for more information about a command.
`)
}

func printVersion() {
	fmt.Printf("Orchestra %s\n", Version)
	fmt.Printf("  Git Commit:  %s\n", GitCommit)
	fmt.Printf("  Build Date:  %s\n", BuildDate)
	fmt.Printf("  Go Version:  %s\n", goVersion())
}

func runHealthcheck() error {
	fmt.Println("OK")
	return nil
}

func runServe(args []string) error {
	var configPath string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config", "-c":
			if i+1 >= len(args) {
				return fmt.Errorf("flag --config requires a path argument")
			}
			i++
			configPath = args[i]
		case "--help", "-h":
			fmt.Printf(`Usage:
  orchestra serve [flags]

Flags:
  -c, --config string   Path to configuration file (default "configs/orchestra.yaml")
  -h, --help            Show help for serve command
`)
			return nil
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}

	if configPath == "" {
		configPath = "configs/orchestra.yaml"
	}

	fmt.Printf("Starting Orchestra server with config: %s\n", configPath)
	fmt.Println("Server mode is not yet implemented (Phase 1 — foundation)")
	fmt.Println("The library foundation (types, interfaces, registry, config) is ready for use.")
	return nil
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
  orchestra chat [flags]

Start an interactive terminal UI for chatting with Orchestra agents.

Flags:
  -a, --agent string       Start with a specific agent
  -m, --model string       Start with a specific model
  -r, --resume string      Resume a previous session by ID
      --session-dir string  Directory for session storage (default: ~/.orchestra/sessions)
  -h, --help               Show help for chat command

Environment Variables:
  NO_COLOR                 Disable colored output

Examples:
  orchestra chat
  orchestra chat --agent assistant --model gpt-4
  orchestra chat --resume 1234567890
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
