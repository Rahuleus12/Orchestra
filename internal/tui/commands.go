package tui

import (
	"strings"
)

// CommandType identifies the type of a slash command.
type CommandType string

const (
	// CommandAgent switches the active agent.
	CommandAgent CommandType = "agent"

	// CommandModel switches the model for the active agent.
	CommandModel CommandType = "model"

	// CommandHelpFn shows available commands.
	CommandHelpFn CommandType = "help"

	// CommandQuit exits the TUI.
	CommandQuit CommandType = "quit"

	// CommandSystem updates the system prompt.
	CommandSystem CommandType = "system"

	// CommandTools lists available tools.
	CommandTools CommandType = "tools"

	// CommandClear clears the conversation history.
	CommandClear CommandType = "clear"

	// CommandCompact triggers conversation compaction.
	CommandCompact CommandType = "compact"

	// CommandSave exports the conversation to a file.
	CommandSave CommandType = "save"

	// CommandTheme switches the color theme.
	CommandTheme CommandType = "theme"

	// CommandWorkflow loads a workflow definition.
	CommandWorkflow CommandType = "workflow"
)

// Command represents a parsed slash command.
type Command struct {
	// Type is the command type.
	Type CommandType

	// Args contains any arguments passed to the command.
	Args string

	// Raw is the original command string.
	Raw string
}

// CommandHandler processes a command and returns a response or error.
type CommandHandler func(cmd Command) (string, error)

// CommandRegistry holds registered command handlers.
type CommandRegistry struct {
	handlers map[CommandType]CommandHandler
	aliases  map[string]CommandType
}

// NewCommandRegistry creates a new CommandRegistry with default commands.
func NewCommandRegistry() *CommandRegistry {
	r := &CommandRegistry{
		handlers: make(map[CommandType]CommandHandler),
		aliases:  make(map[string]CommandType),
	}

	// Register default aliases
	r.aliases["a"] = CommandAgent
	r.aliases["m"] = CommandModel
	r.aliases["s"] = CommandSystem
	r.aliases["t"] = CommandTools
	r.aliases["c"] = CommandClear
	r.aliases["cp"] = CommandCompact
	r.aliases["w"] = CommandSave
	r.aliases["h"] = CommandHelpFn
	r.aliases["q"] = CommandQuit
	r.aliases["th"] = CommandTheme
	r.aliases["wf"] = CommandWorkflow

	return r
}

// Register adds a handler for the given command type.
func (r *CommandRegistry) Register(cmdType CommandType, handler CommandHandler) {
	r.handlers[cmdType] = handler
}

// RegisterAlias adds an alias for a command type.
func (r *CommandRegistry) RegisterAlias(alias string, cmdType CommandType) {
	r.aliases[alias] = cmdType
}

// Parse parses a string as a slash command.
// Returns nil if the string is not a command.
func (r *CommandRegistry) Parse(input string) *Command {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "/") {
		return nil
	}

	input = strings.TrimPrefix(input, "/")
	parts := strings.SplitN(input, " ", 2)

	cmdName := strings.ToLower(parts[0])
	var args string
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}

	// Check if it's a direct command type
	cmdType := CommandType(cmdName)
	if _, ok := r.handlers[cmdType]; ok || isKnownCommand(cmdType) {
		return &Command{
			Type: cmdType,
			Args: args,
			Raw:  "/" + input,
		}
	}

	// Check aliases
	if aliased, ok := r.aliases[cmdName]; ok {
		return &Command{
			Type: aliased,
			Args: args,
			Raw:  "/" + input,
		}
	}

	// Unknown command
	return &Command{
		Type: cmdType,
		Args: args,
		Raw:  "/" + input,
	}
}

// Execute executes a parsed command using the registered handler.
// Returns the response string and any error.
func (r *CommandRegistry) Execute(cmd Command) (string, error) {
	handler, ok := r.handlers[cmd.Type]
	if !ok {
		return "", &UnknownCommandError{Command: cmd.Raw}
	}
	return handler(cmd)
}

// IsCommand checks if the input string starts with a slash.
func IsCommand(input string) bool {
	return strings.HasPrefix(strings.TrimSpace(input), "/")
}

// isKnownCommand checks if a command type is a built-in command.
func isKnownCommand(cmdType CommandType) bool {
	switch cmdType {
	case CommandAgent, CommandModel, CommandSystem, CommandTools,
		CommandClear, CommandCompact, CommandSave, CommandHelpFn,
		CommandQuit, CommandTheme, CommandWorkflow:
		return true
	default:
		return false
	}
}

// GetCommandHelp returns a formatted help string for all known commands.
func GetCommandHelp() string {
	return `Available commands:

  /agent <name>      Switch active agent
  /model <name>      Switch model for active agent
  /system <prompt>   Update system prompt
  /tools             List available tools
  /clear             Clear conversation history
  /compact           Trigger conversation compaction
  /save [path]       Export conversation to file
  /theme [light|dark] Switch color theme
  /workflow <file>   Load a workflow definition
  /help              Show this help
  /quit              Exit the TUI

Short aliases: /a, /m, /s, /t, /c, /cp, /w, /th, /wf, /h, /q`
}

// UnknownCommandError is returned when an unknown command is executed.
type UnknownCommandError struct {
	Command string
}

func (e *UnknownCommandError) Error() string {
	return "unknown command: " + e.Command
}
