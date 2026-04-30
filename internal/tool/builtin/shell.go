// Package builtin provides built-in tools for the Orchestra tool system.
package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Shell Exec Tool
// ---------------------------------------------------------------------------

// ShellExecInput defines the input for the shell_exec tool.
type ShellExecInput struct {
	// Command is the shell command to execute.
	Command string `json:"command" description:"Shell command to execute"`

	// WorkingDir is the working directory for the command.
	// Relative paths are resolved against the tool's root directory.
	WorkingDir string `json:"working_dir,omitempty" description:"Working directory for the command (relative to root)"`

	// TimeoutSeconds is the maximum execution time in seconds. Defaults to 60.
	TimeoutSeconds int `json:"timeout_seconds,omitempty" description:"Maximum execution time in seconds" default:"60" min:"1" max:"600"`

	// Environment is a map of environment variables to set for the command.
	// These are merged with (and override) the inherited environment.
	Environment map[string]string `json:"environment,omitempty" description:"Environment variables to set (merged with inherited)"`

	// Shell specifies the shell to use. Defaults to "sh" on Unix, "cmd" on Windows.
	// Set to "none" to execute the command directly without a shell.
	Shell string `json:"shell,omitempty" description:"Shell to use: sh, bash, powershell, cmd, or none" default:"sh" enum:"sh,bash,powershell,cmd,none"`

	// MaxOutputBytes limits the combined stdout+stderr output size. Defaults to 1MB.
	MaxOutputBytes int `json:"max_output_bytes,omitempty" description:"Maximum output size in bytes (stdout+stderr)" default:"1048576" min:"1024" max:"104857600"`

	// CombineOutput combines stdout and stderr into a single output stream.
	// When false, they are captured separately.
	CombineOutput bool `json:"combine_output,omitempty" description:"Combine stdout and stderr into single output" default:"true"`

	// stdinInput provides input to the command's stdin.
	Stdin string `json:"stdin,omitempty" description:"Input to send to the command's stdin"`
}

// ShellExecOutput defines the output of the shell_exec tool.
type ShellExecOutput struct {
	// ExitCode is the process exit code. 0 indicates success.
	ExitCode int `json:"exit_code"`

	// Stdout contains the command's standard output.
	Stdout string `json:"stdout"`

	// Stderr contains the command's standard error.
	Stderr string `json:"stderr,omitempty"`

	// Output contains the combined stdout+stderr when CombineOutput was true.
	Output string `json:"output,omitempty"`

	// Truncated indicates if the output was truncated due to MaxOutputBytes.
	Truncated bool `json:"truncated,omitempty"`

	// DurationMs is the execution time in milliseconds.
	DurationMs int64 `json:"duration_ms"`

	// TimedOut indicates if the command was killed due to timeout.
	TimedOut bool `json:"timed_out,omitempty"`

	// WorkingDir is the resolved working directory that was used.
	WorkingDir string `json:"working_dir"`

	// Command shows the actual command that was executed.
	Command string `json:"command"`

	// Error contains an error message if the command couldn't be started.
	Error string `json:"error,omitempty"`
}

// ShellExecTool implements the shell_exec built-in tool.
// It executes shell commands in a sandboxed working directory with
// configurable allowlists, timeouts, and output limits.
//
// SECURITY WARNING: Shell execution is inherently dangerous. Always configure
// appropriate restrictions:
//   - Use AllowedCommands to whitelist only necessary commands
//   - Set BlockedPatterns to block dangerous operations
//   - Use Root to restrict filesystem access
//   - Set reasonable timeouts to prevent hanging
//   - Consider using more specific tools (file_edit, code_search, git_operations)
//     when possible instead of shell commands
type ShellExecTool struct {
	// Root is the root directory. Commands are restricted to this directory
	// tree when AllowEscapeRoot is false.
	Root string

	// AllowEscapeRoot controls whether commands can access files outside
	// the root directory. When false (default), working directory changes
	// outside root are blocked.
	AllowEscapeRoot bool

	// AllowedCommands is a whitelist of command names that can be executed.
	// If empty, all commands are allowed (not recommended for production).
	// Command names are checked against the first word of the command.
	// Examples: ["git", "go", "npm", "node", "python3", "make", "gcc"]
	AllowedCommands []string

	// BlockedPatterns is a list of patterns that, if found in the command,
	// cause it to be rejected. This provides defense-in-depth against
	// dangerous commands even if they're in the allowlist.
	// Examples: ["rm -rf /", "mkfs", "dd if=", "> /dev/sda"]
	BlockedPatterns []string

	// AllowedShells is a whitelist of shells that can be used.
	// If empty, defaults to ["sh", "bash", "cmd", "powershell", "none"].
	AllowedShells []string

	// DefaultTimeout is the default timeout if not specified in the command.
	DefaultTimeout time.Duration

	// MaxTimeout is the maximum allowed timeout, regardless of what's requested.
	MaxTimeout time.Duration

	// InheritEnvironment controls whether the tool inherits the process
	// environment. When false, only explicitly provided environment variables
	// are available (more secure but may break some commands).
	InheritEnvironment bool

	// AllowStdin controls whether stdin input is allowed.
	AllowStdin bool

	// MaxOutputBytes is the default maximum output size.
	MaxOutputBytes int64
}

// NewShellExecTool creates a shell_exec tool with secure default settings.
func NewShellExecTool() ShellExecTool {
	return ShellExecTool{
		AllowedCommands:    nil, // All commands allowed - caller should configure
		BlockedPatterns:    defaultBlockedPatterns(),
		DefaultTimeout:     60 * time.Second,
		MaxTimeout:         10 * time.Minute,
		InheritEnvironment: true,
		AllowStdin:         true,
		MaxOutputBytes:     1 * 1024 * 1024,
	}
}

// NewShellExecToolWithRoot creates a shell_exec tool restricted to a root directory.
func NewShellExecToolWithRoot(root string) ShellExecTool {
	t := NewShellExecTool()
	t.Root = root
	return t
}

// NewRestrictedShellExecTool creates a shell_exec tool with strict security settings.
// Only the specified commands are allowed.
func NewRestrictedShellExecTool(allowedCommands []string, root string) ShellExecTool {
	t := NewShellExecToolWithRoot(root)
	t.AllowedCommands = allowedCommands
	t.AllowEscapeRoot = false
	return t
}

// defaultBlockedPatterns returns patterns for obviously dangerous commands.
func defaultBlockedPatterns() []string {
	return []string{
		"rm -rf /",
		"rm -rf /*",
		"mkfs.",
		"dd if=/dev/zero",
		"dd if=/dev/random",
		":(){ :|:& };:", // Fork bomb
		"> /dev/sd",
		"chmod -R 777 /",
		"chown -R",
		"mv / /",
		"cp /dev/null",
	}
}

// Name returns the tool's identifier.
func (t ShellExecTool) Name() string { return "shell_exec" }

// Description returns the tool's description for the LLM.
func (t ShellExecTool) Description() string {
	return `Execute a shell command and return its output.

This tool runs commands in a shell and captures stdout and stderr. Use it for
tasks that can't be accomplished with more specific tools like file_edit,
code_search, or git_operations.

Common use cases:
- Running build commands (make, go build, npm run build)
- Running tests (go test, pytest, npm test)
- Package management (go mod, npm install, pip install)
- Interpreted scripts (python script.py, node script.js)
- Quick one-liners that don't warrant a dedicated tool

Safety features:
- Configurable command allowlist to restrict what can run
- Blocked patterns to prevent dangerous operations
- Timeout to prevent hanging commands
- Output size limits to prevent memory exhaustion
- Working directory restrictions

Best practices:
- Prefer specific tools (file_edit, git_operations) when available
- Set appropriate timeouts for long-running commands
- Use CombineOutput=false to separate stdout from stderr for debugging
- Check exit codes to detect failures
- Quote arguments properly to avoid injection issues`
}

// Parameters returns the JSON Schema for the tool's input.
func (t ShellExecTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {
				"type": "string",
				"description": "Shell command to execute"
			},
			"working_dir": {
				"type": "string",
				"description": "Working directory for the command (relative to root)"
			},
			"timeout_seconds": {
				"type": "integer",
				"description": "Maximum execution time in seconds",
				"default": 60,
				"minimum": 1,
				"maximum": 600
			},
			"environment": {
				"type": "object",
				"description": "Environment variables to set (merged with inherited)",
				"additionalProperties": {"type": "string"}
			},
			"shell": {
				"type": "string",
				"description": "Shell to use: sh, bash, powershell, cmd, or none",
				"default": "sh",
				"enum": ["sh", "bash", "powershell", "cmd", "none"]
			},
			"max_output_bytes": {
				"type": "integer",
				"description": "Maximum output size in bytes (stdout+stderr)",
				"default": 1048576,
				"minimum": 1024,
				"maximum": 104857600
			},
			"combine_output": {
				"type": "boolean",
				"description": "Combine stdout and stderr into single output",
				"default": true
			},
			"stdin": {
				"type": "string",
				"description": "Input to send to the command's stdin"
			}
		},
		"required": ["command"]
	}`)
}

// Execute runs the shell command.
func (t ShellExecTool) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req ShellExecInput
	if err := json.Unmarshal(input, &req); err != nil {
		return marshalShellExecError(fmt.Errorf("parse input: %w", err))
	}

	if req.Command == "" {
		return marshalShellExecError(fmt.Errorf("command is required"))
	}

	// Check context
	if ctx.Err() != nil {
		return marshalShellExecError(ctx.Err())
	}

	// Apply defaults
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = int(t.DefaultTimeout.Seconds())
	}
	maxOutput := t.MaxOutputBytes
	if maxOutput <= 0 {
		maxOutput = 1 * 1024 * 1024
	}
	if req.MaxOutputBytes > 0 {
		maxOutput = int64(req.MaxOutputBytes)
	}

	// Validate and apply timeout limits
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if t.MaxTimeout > 0 && timeout > t.MaxTimeout {
		timeout = t.MaxTimeout
	}

	// Security checks
	if err := t.validateCommand(req.Command); err != nil {
		return marshalShellExecError(err)
	}

	if err := t.validateShell(req.Shell); err != nil {
		return marshalShellExecError(err)
	}

	if !t.AllowStdin && req.Stdin != "" {
		return marshalShellExecError(fmt.Errorf("stdin input is not allowed"))
	}

	// Resolve working directory
	workingDir, err := t.resolveWorkingDir(req.WorkingDir)
	if err != nil {
		return marshalShellExecError(err)
	}

	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Build and execute the command
	startTime := time.Now()
	cmd := t.buildCommand(ctx, req, workingDir)

	var stdout, stderr bytes.Buffer
	var combinedOutput bytes.Buffer

	if req.CombineOutput {
		cmd.Stdout = &limitedWriter{buf: &combinedOutput, limit: maxOutput}
		cmd.Stderr = &limitedWriter{buf: &combinedOutput, limit: maxOutput}
	} else {
		cmd.Stdout = &limitedWriter{buf: &stdout, limit: maxOutput}
		cmd.Stderr = &limitedWriter{buf: &stderr, limit: maxOutput}
	}

	// Handle stdin
	if req.Stdin != "" {
		cmd.Stdin = strings.NewReader(req.Stdin)
	}

	// Execute the command
	err = cmd.Run()
	duration := time.Since(startTime)

	// Build output
	output := ShellExecOutput{
		DurationMs: duration.Milliseconds(),
		WorkingDir: workingDir,
		Command:    req.Command,
	}

	// Set exit code
	if exitErr, ok := err.(*exec.ExitError); ok {
		output.ExitCode = exitErr.ExitCode()
	} else if err != nil {
		output.ExitCode = -1
	}

	// Check for timeout
	if ctx.Err() == context.DeadlineExceeded {
		output.TimedOut = true
		output.ExitCode = -1
		output.Error = "command timed out"
	} else if err != nil && output.ExitCode == -1 {
		// Error starting the command (not an exit error)
		output.Error = fmt.Sprintf("command failed to start: %v", err)
	}

	// Set output based on combine mode
	if req.CombineOutput {
		output.Output = combinedOutput.String()
		output.Truncated = combinedOutput.Len() >= int(maxOutput)
	} else {
		output.Stdout = stdout.String()
		output.Stderr = stderr.String()
		output.Truncated = stdout.Len()+stderr.Len() >= int(maxOutput)
	}

	return json.Marshal(output)
}

// validateCommand checks the command against the allowlist and blocked patterns.
func (t ShellExecTool) validateCommand(command string) error {
	// Extract the base command (first word)
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}
	baseCmd := filepath.Base(parts[0])

	// Check allowlist
	if len(t.AllowedCommands) > 0 {
		allowed := false
		for _, allowedCmd := range t.AllowedCommands {
			if baseCmd == allowedCmd || baseCmd == filepath.Base(allowedCmd) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("command %q is not in the allowed commands list", baseCmd)
		}
	}

	// Check blocked patterns
	for _, pattern := range t.BlockedPatterns {
		if strings.Contains(command, pattern) {
			return fmt.Errorf("command contains blocked pattern: %q", pattern)
		}
	}

	return nil
}

// validateShell checks if the requested shell is allowed.
func (t ShellExecTool) validateShell(shell string) error {
	if shell == "" {
		return nil // Will use default
	}

	allowedShells := t.AllowedShells
	if len(allowedShells) == 0 {
		allowedShells = []string{"sh", "bash", "powershell", "cmd", "none"}
	}

	for _, allowed := range allowedShells {
		if shell == allowed {
			return nil
		}
	}

	return fmt.Errorf("shell %q is not allowed (allowed: %v)", shell, allowedShells)
}

// resolveWorkingDir resolves the working directory for the command.
func (t ShellExecTool) resolveWorkingDir(requestedDir string) (string, error) {
	root := t.Root
	if root == "" {
		root = "."
	}
	root = filepath.Clean(root)

	// Resolve root to absolute path
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}

	// If no working dir specified, use root
	if requestedDir == "" {
		return absRoot, nil
	}

	// Clean and resolve the requested working directory
	requestedDir = filepath.Clean(requestedDir)
	absRequested, err := filepath.Abs(filepath.Join(absRoot, requestedDir))
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}

	// Check if we're allowed to escape root
	if !t.AllowEscapeRoot {
		if !strings.HasPrefix(absRequested, absRoot+string(filepath.Separator)) && absRequested != absRoot {
			return "", fmt.Errorf("working directory %q is outside the allowed root %q", requestedDir, absRoot)
		}
	}

	return absRequested, nil
}

// buildCommand creates the exec.Cmd based on the shell setting.
func (t ShellExecTool) buildCommand(ctx context.Context, req ShellExecInput, workingDir string) *exec.Cmd {
	shell := req.Shell
	if shell == "" {
		shell = defaultShell()
	}

	var cmd *exec.Cmd

	switch shell {
	case "none":
		// Execute command directly without a shell
		parts := strings.Fields(req.Command)
		if len(parts) == 0 {
			parts = []string{req.Command}
		}
		cmd = exec.CommandContext(ctx, parts[0], parts[1:]...)

	case "powershell":
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", req.Command)

	case "cmd":
		cmd = exec.CommandContext(ctx, "cmd", "/C", req.Command)

	case "bash":
		cmd = exec.CommandContext(ctx, "bash", "-c", req.Command)

	default: // "sh" or any other value
		cmd = exec.CommandContext(ctx, "sh", "-c", req.Command)
	}

	cmd.Dir = workingDir

	// Set environment
	if t.InheritEnvironment {
		cmd.Env = os.Environ()
	} else {
		cmd.Env = []string{}
	}

	// Add/override environment variables from the request
	for key, value := range req.Environment {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	return cmd
}

// defaultShell returns the default shell for the current platform.
func defaultShell() string {
	if isWindows() {
		return "cmd"
	}
	return "sh"
}

// isWindows returns true if running on Windows.
func isWindows() bool {
	return os.PathSeparator == '\\' || os.PathListSeparator == ';'
}

// ---------------------------------------------------------------------------
// Limited Writer
// ---------------------------------------------------------------------------

// limitedWriter is an io.Writer that stops writing after a limit is reached.
// This prevents memory exhaustion from commands that produce excessive output.
type limitedWriter struct {
	buf   *bytes.Buffer
	limit int64
	wrote int64
	trunc bool
}

// Write writes data to the buffer, stopping when the limit is reached.
func (w *limitedWriter) Write(p []byte) (n int, err error) {
	if w.trunc {
		return len(p), nil // Discard after truncation
	}

	remaining := w.limit - w.wrote
	if remaining <= 0 {
		w.trunc = true
		return len(p), nil
	}

	if int64(len(p)) > remaining {
		n, _ = w.buf.Write(p[:remaining])
		w.wrote += int64(n)
		w.trunc = true
		return len(p), nil
	}

	n, err = w.buf.Write(p)
	w.wrote += int64(n)
	return n, err
}

// marshalShellExecError creates a JSON error response for shell_exec.
func marshalShellExecError(err error) (json.RawMessage, error) {
	output := ShellExecOutput{
		Error: err.Error(),
	}
	return json.Marshal(output)
}
