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
//	version      Print version information
//	healthcheck  Run a health check
package main

import (
	"fmt"
	"os"
	"runtime"
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

// goVersion returns the Go version used to build the binary.
func goVersion() string {
	return runtime.Version()
}
