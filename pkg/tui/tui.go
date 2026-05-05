// Package tui provides the public API for the Orchestra Terminal UI.
//
// This package re-exports the internal TUI functionality for use
// by external applications and the CLI.
package tui

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"
	internal "github.com/user/orchestra/internal/tui"
)

// Run starts the interactive Terminal UI.
// This is the main entry point for the `orchestra chat` command.
//
// Options:
//   - version: The Orchestra version string
//   - agent: The default agent name
//   - model: The default model name
//   - sessionDir: Directory for session persistence
//
// Example:
//
//	err := tui.Run(tui.WithVersion("1.0.0"), tui.WithAgent("assistant", "gpt-4"))
func Run(opts ...Option) error {
	// Check if terminal is interactive
	if !isInteractive() {
		return nil
	}

	// Check NO_COLOR environment variable
	if os.Getenv("NO_COLOR") != "" {
		return nil
	}

	// Convert public options to internal options
	var internalOpts []internal.AppOption
	for _, opt := range opts {
		if o, ok := opt.(internalOption); ok {
			internalOpts = append(internalOpts, o.apply)
		}
	}

	// Create the app model
	model, err := internal.NewAppModel(internalOpts...)
	if err != nil {
		return err
	}

	// Run the Bubble Tea program
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	_, err = p.Run()
	return err
}

// Option is a configuration option for the TUI.
type Option interface {
	isOption()
}

// internalOption wraps an internal AppOption.
type internalOption struct {
	apply internal.AppOption
}

func (o internalOption) isOption() {}

// WithVersion sets the version string displayed in the TUI.
func WithVersion(version string) Option {
	return internalOption{apply: internal.WithVersion(version)}
}

// WithAgent sets the default agent and model for new conversations.
func WithAgent(agent, model string) Option {
	return internalOption{apply: internal.WithAgent(agent, model)}
}

// WithSessionDir sets the directory for session persistence.
func WithSessionDir(dir string) Option {
	return internalOption{apply: internal.WithSessionDir(dir)}
}

// isInteractive checks if the current terminal is interactive.
func isInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
