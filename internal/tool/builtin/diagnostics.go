// Package builtin provides built-in tools for the Orchestra tool system.
package builtin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Diagnostics Tool
// ---------------------------------------------------------------------------

// DiagnosticType defines the type of diagnostic to run.
type DiagnosticType string

const (
	// DiagLint runs a linter to find code quality issues.
	DiagLint DiagnosticType = "lint"

	// DiagTypeCheck runs a type checker to find type errors.
	DiagTypeCheck DiagnosticType = "type_check"

	// DiagTest runs tests to find failing tests.
	DiagTest DiagnosticType = "test"

	// DiagVet runs static analysis to find suspicious code.
	DiagVet DiagnosticType = "vet"

	// DiagFormat checks code formatting.
	DiagFormat DiagnosticType = "format"

	// DiagBuild attempts to build/compile the project.
	DiagBuild DiagnosticType = "build"

	// DiagCustom runs a custom diagnostic command.
	DiagCustom DiagnosticType = "custom"
)

// DiagnosticSeverity indicates the severity of a diagnostic issue.
type DiagnosticSeverity string

const (
	SeverityError   DiagnosticSeverity = "error"
	SeverityWarning DiagnosticSeverity = "warning"
	SeverityInfo    DiagnosticSeverity = "info"
	SeverityNote    DiagnosticSeverity = "note"
)

// DiagnosticsInput defines the input for the diagnostics tool.
type DiagnosticsInput struct {
	// Type is the type of diagnostic to run.
	Type DiagnosticType `json:"type" description:"Type of diagnostic to run" enum:"lint,type_check,test,vet,format,build,custom"`

	// Path is the directory or file to run diagnostics on. Defaults to root.
	Path string `json:"path,omitempty" description:"Directory or file to run diagnostics on (relative to root)"`

	// Language specifies the programming language. Auto-detected if not set.
	// Used to select appropriate tools when Type is "lint" or "type_check".
	// Supported: "go", "python", "javascript", "typescript", "rust", "java"
	Language string `json:"language,omitempty" description:"Programming language (auto-detected if not set)" enum:"go,python,javascript,typescript,rust,java"`

	// Command is the custom command to run (only for type="custom").
	// Example: "eslint src/", "pylint mymodule/", "mypy src/"
	Command string `json:"command,omitempty" description:"Custom command to run (for type='custom' only)"`

	// Args are additional arguments for the diagnostic command.
	// These are appended to the auto-generated command.
	Args string `json:"args,omitempty" description:"Additional arguments for the diagnostic command"`

	// TimeoutSeconds is the maximum execution time. Defaults to 120 for tests, 30 otherwise.
	TimeoutSeconds int `json:"timeout_seconds,omitempty" description:"Maximum execution time in seconds" min:"1" max:"600"`

	// MaxOutputBytes limits the output size. Defaults to 2MB.
	MaxOutputBytes int `json:"max_output_bytes,omitempty" description:"Maximum output size in bytes" default:"2097152" min:"1024" max:"104857600"`

	// FilterSeverity filters results to only include issues with this severity or higher.
	// Empty means include all severities.
	FilterSeverity string `json:"filter_severity,omitempty" description:"Filter to only show issues at this severity or higher" enum:"error,warning,info"`

	// MaxIssues limits the number of issues returned. 0 means unlimited.
	MaxIssues int `json:"max_issues,omitempty" description:"Maximum number of issues to return (0 for unlimited)" min:"0"`

	// TestArgs are additional arguments specifically for test runs.
	// Example: "-run TestMyFunction", "-v", "-short"
	TestArgs string `json:"test_args,omitempty" description:"Additional arguments for test runs (e.g., '-run TestName', '-v')"`

	// Environment is a map of environment variables to set.
	Environment map[string]string `json:"environment,omitempty" description:"Environment variables to set"`
}

// DiagnosticsOutput defines the output of the diagnostics tool.
type DiagnosticsOutput struct {
	// Type is the diagnostic type that was run.
	Type DiagnosticType `json:"type"`

	// Command shows the actual command that was executed.
	Command string `json:"command"`

	// ExitCode is the command's exit code. 0 typically indicates no issues.
	ExitCode int `json:"exit_code"`

	// Issues contains the parsed diagnostic issues.
	Issues []DiagnosticIssue `json:"issues"`

	// IssueCount is the total number of issues found.
	IssueCount int `json:"issue_count"`

	// ErrorCount is the number of error-severity issues.
	ErrorCount int `json:"error_count"`

	// WarningCount is the number of warning-severity issues.
	WarningCount int `json:"warning_count"`

	// RawOutput contains the unparsed command output.
	RawOutput string `json:"raw_output,omitempty"`

	// Truncated indicates if the output was truncated.
	Truncated bool `json:"truncated,omitempty"`

	// DurationMs is the execution time in milliseconds.
	DurationMs int64 `json:"duration_ms"`

	// Path is the resolved path that was diagnosed.
	Path string `json:"path"`

	// Language is the detected or specified language.
	Language string `json:"language"`

	// TimedOut indicates if the command was killed due to timeout.
	TimedOut bool `json:"timed_out,omitempty"`

	// Error contains an error message if the diagnostic couldn't be run.
	Error string `json:"error,omitempty"`
}

// DiagnosticIssue represents a single diagnostic issue found by a linter,
// type checker, or test runner.
type DiagnosticIssue struct {
	// File is the path to the file containing the issue.
	File string `json:"file,omitempty"`

	// Line is the 1-based line number where the issue starts.
	Line int `json:"line,omitempty"`

	// Column is the 1-based column number where the issue starts.
	Column int `json:"column,omitempty"`

	// EndLine is the 1-based line number where the issue ends (if applicable).
	EndLine int `json:"end_line,omitempty"`

	// EndColumn is the 1-based column number where the issue ends (if applicable).
	EndColumn int `json:"end_column,omitempty"`

	// Severity is the issue severity (error, warning, info, note).
	Severity DiagnosticSeverity `json:"severity"`

	// Message is the human-readable description of the issue.
	Message string `json:"message"`

	// Rule is the linter rule or check that triggered this issue.
	// Example: "unused-variable", "E501", "no-unused-vars"
	Rule string `json:"rule,omitempty"`

	// Category is a broader category for the issue.
	// Example: "style", "bug", "performance", "security"
	Category string `json:"category,omitempty"`

	// Fix contains a suggested fix if available.
	Fix string `json:"fix,omitempty"`

	// Context contains additional lines around the issue for context.
	Context []string `json:"context,omitempty"`
}

// DiagnosticsTool implements the diagnostics built-in tool.
// It runs linters, type-checkers, and test runners, parsing their
// output into structured error information.
//
// This is a coding-specific tool designed for identifying and understanding
// code issues. It supports:
//   - Multiple diagnostic types (lint, type_check, test, vet, format, build)
//   - Automatic language detection and tool selection
//   - Custom diagnostic commands
//   - Structured error parsing with file, line, column, and message
//   - Severity filtering
//   - Output size limits and timeouts
type DiagnosticsTool struct {
	// Root is the root directory for file access.
	Root string

	// AllowAbsolute controls whether absolute paths outside the root are allowed.
	AllowAbsolute bool

	// DefaultTimeout is the default timeout for non-test diagnostics.
	DefaultTimeout time.Duration

	// TestTimeout is the default timeout for test runs.
	TestTimeout time.Duration

	// MaxTimeout is the maximum allowed timeout.
	MaxTimeout time.Duration

	// MaxOutputBytes is the default maximum output size.
	MaxOutputBytes int64

	// CustomLinters allows registering custom linter configurations.
	// Map key is the language, value is the command to run.
	CustomLinters map[string]string

	// CustomTypeCheckers allows registering custom type checker configurations.
	CustomTypeCheckers map[string]string
}

// NewDiagnosticsTool creates a diagnostics tool with default settings.
func NewDiagnosticsTool() DiagnosticsTool {
	return DiagnosticsTool{
		DefaultTimeout:     30 * time.Second,
		TestTimeout:        120 * time.Second,
		MaxTimeout:         10 * time.Minute,
		MaxOutputBytes:     2 * 1024 * 1024,
		CustomLinters:      make(map[string]string),
		CustomTypeCheckers: make(map[string]string),
	}
}

// NewDiagnosticsToolWithRoot creates a diagnostics tool restricted to a root directory.
func NewDiagnosticsToolWithRoot(root string) DiagnosticsTool {
	t := NewDiagnosticsTool()
	t.Root = root
	return t
}

// Name returns the tool's identifier.
func (t DiagnosticsTool) Name() string { return "diagnostics" }

// Description returns the tool's description for the LLM.
func (t DiagnosticsTool) Description() string {
	return `Run linters, type-checkers, and test runners to find code issues.

Returns structured error output with file paths, line numbers, columns,
error messages, and severity levels. This tool helps identify bugs, style
issues, type errors, and failing tests.

Supported diagnostic types:
- lint: Run a linter for code quality issues (language-specific)
- type_check: Run type checking (language-specific)
- test: Run tests and report failures
- vet: Run static analysis (Go-specific: go vet)
- format: Check code formatting (Go-specific: gofmt)
- build: Attempt to build/compile the project
- custom: Run a custom diagnostic command

Language support (auto-detected):
- Go: golint/staticcheck, go vet, go test, gofmt, go build
- Python: pylint/flake8, mypy, pytest/unittest
- JavaScript/TypeScript: eslint, tsc, jest/mocha
- Rust: clippy, cargo check, cargo test
- Java: checkstyle/pmd, javac, junit

Common usage:
- "Find all lint errors": type="lint"
- "Check for type errors": type="type_check"
- "Run tests": type="test"
- "Check formatting": type="format"
- "Run go vet": type="vet"
- "Custom command": type="custom", command="mypy src/"

The tool parses output into structured issues with file, line, column,
and message for easy consumption by the agent.`
}

// Parameters returns the JSON Schema for the tool's input.
func (t DiagnosticsTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"type": {
				"type": "string",
				"description": "Type of diagnostic to run",
				"enum": ["lint", "type_check", "test", "vet", "format", "build", "custom"]
			},
			"path": {
				"type": "string",
				"description": "Directory or file to run diagnostics on (relative to root)"
			},
			"language": {
				"type": "string",
				"description": "Programming language (auto-detected if not set)",
				"enum": ["go", "python", "javascript", "typescript", "rust", "java"]
			},
			"command": {
				"type": "string",
				"description": "Custom command to run (for type='custom' only)"
			},
			"args": {
				"type": "string",
				"description": "Additional arguments for the diagnostic command"
			},
			"timeout_seconds": {
				"type": "integer",
				"description": "Maximum execution time in seconds",
				"minimum": 1,
				"maximum": 600
			},
			"max_output_bytes": {
				"type": "integer",
				"description": "Maximum output size in bytes",
				"default": 2097152,
				"minimum": 1024,
				"maximum": 104857600
			},
			"filter_severity": {
				"type": "string",
				"description": "Filter to only show issues at this severity or higher",
				"enum": ["error", "warning", "info"]
			},
			"max_issues": {
				"type": "integer",
				"description": "Maximum number of issues to return (0 for unlimited)",
				"minimum": 0
			},
			"test_args": {
				"type": "string",
				"description": "Additional arguments for test runs (e.g., '-run TestName', '-v')"
			},
			"environment": {
				"type": "object",
				"description": "Environment variables to set",
				"additionalProperties": {"type": "string"}
			}
		},
		"required": ["type"]
	}`)
}

// Execute runs the diagnostic and returns structured issues.
func (t DiagnosticsTool) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req DiagnosticsInput
	if err := json.Unmarshal(input, &req); err != nil {
		return marshalDiagnosticsError(fmt.Errorf("parse input: %w", err))
	}

	if req.Type == "" {
		return marshalDiagnosticsError(fmt.Errorf("type is required"))
	}

	// Check context
	if ctx.Err() != nil {
		return marshalDiagnosticsError(ctx.Err())
	}

	// Resolve the path
	diagPath, err := t.resolvePath(req.Path)
	if err != nil {
		return marshalDiagnosticsError(err)
	}

	// Detect language if not specified
	language := req.Language
	if language == "" {
		language = t.detectLanguage(diagPath)
	}

	// Apply defaults
	timeout := t.getTimeout(req.Type, req.TimeoutSeconds)
	maxOutput := t.MaxOutputBytes
	if maxOutput <= 0 {
		maxOutput = 2 * 1024 * 1024
	}
	if req.MaxOutputBytes > 0 {
		maxOutput = int64(req.MaxOutputBytes)
	}

	// Validate custom command
	if req.Type == DiagCustom && req.Command == "" {
		return marshalDiagnosticsError(fmt.Errorf("command is required for type='custom'"))
	}

	// Build the command
	cmdArgs, err := t.buildCommand(req, language, diagPath)
	if err != nil {
		return marshalDiagnosticsError(err)
	}

	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Execute the command
	startTime := time.Now()
	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	cmd.Dir = diagPath

	// Set environment
	cmd.Env = nil // Clear default env for security
	for key, value := range req.Environment {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitedWriter{buf: &stdout, limit: maxOutput}
	cmd.Stderr = &limitedWriter{buf: &stderr, limit: maxOutput}

	execErr := cmd.Run()
	duration := time.Since(startTime)

	// Build output
	output := DiagnosticsOutput{
		Type:       req.Type,
		Command:    strings.Join(cmdArgs, " "),
		DurationMs: duration.Milliseconds(),
		Path:       diagPath,
		Language:   language,
	}

	// Set exit code
	if exitErr, ok := execErr.(*exec.ExitError); ok {
		output.ExitCode = exitErr.ExitCode()
	} else if execErr != nil {
		output.ExitCode = -1
	}

	// Check for timeout
	if ctx.Err() == context.DeadlineExceeded {
		output.TimedOut = true
		output.ExitCode = -1
		output.Error = "diagnostic timed out"
	} else if execErr != nil && output.ExitCode == -1 {
		output.Error = fmt.Sprintf("failed to run diagnostic: %v", execErr)
	}

	// Combine output
	rawOutput := stdout.String()
	if stderr.Len() > 0 {
		if rawOutput != "" {
			rawOutput += "\n"
		}
		rawOutput += stderr.String()
	}
	output.RawOutput = rawOutput
	output.Truncated = stdout.Len()+stderr.Len() >= int(maxOutput)

	// Parse the output into structured issues
	output.Issues = t.parseOutput(req.Type, language, rawOutput)

	// Filter by severity if requested
	if req.FilterSeverity != "" {
		output.Issues = t.filterBySeverity(output.Issues, req.FilterSeverity)
	}

	// Limit issues if requested
	if req.MaxIssues > 0 && len(output.Issues) > req.MaxIssues {
		output.Issues = output.Issues[:req.MaxIssues]
	}

	// Count issues
	output.IssueCount = len(output.Issues)
	for _, issue := range output.Issues {
		switch issue.Severity {
		case SeverityError:
			output.ErrorCount++
		case SeverityWarning:
			output.WarningCount++
		}
	}

	return json.Marshal(output)
}

// buildCommand constructs the command to run based on the diagnostic type and language.
func (t DiagnosticsTool) buildCommand(req DiagnosticsInput, language, path string) ([]string, error) {
	var args []string

	switch req.Type {
	case DiagCustom:
		parts := strings.Fields(req.Command)
		if len(parts) == 0 {
			return nil, fmt.Errorf("empty command")
		}
		if req.Args != "" {
			parts = append(parts, strings.Fields(req.Args)...)
		}
		return parts, nil

	case DiagLint:
		args = t.getLintCommand(language, path, req.Args)

	case DiagTypeCheck:
		args = t.getTypeCheckCommand(language, path, req.Args)

	case DiagTest:
		args = t.getTestCommand(language, path, req.TestArgs, req.Args)

	case DiagVet:
		args = t.getVetCommand(language, path, req.Args)

	case DiagFormat:
		args = t.getFormatCommand(language, path, req.Args)

	case DiagBuild:
		args = t.getBuildCommand(language, path, req.Args)

	default:
		return nil, fmt.Errorf("unknown diagnostic type: %q", req.Type)
	}

	if len(args) == 0 {
		return nil, fmt.Errorf("no command available for type=%q, language=%q", req.Type, language)
	}

	return args, nil
}

// getLintCommand returns the lint command for the given language.
func (t DiagnosticsTool) getLintCommand(language, path, extraArgs string) []string {
	// Check for custom linter
	if cmd, ok := t.CustomLinters[language]; ok {
		parts := strings.Fields(cmd)
		if extraArgs != "" {
			parts = append(parts, strings.Fields(extraArgs)...)
		}
		parts = append(parts, path)
		return parts
	}

	switch language {
	case "go":
		// Try staticcheck first, fall back to go vet for linting
		cmd := []string{"staticcheck", path}
		if extraArgs != "" {
			cmd = append(cmd, strings.Fields(extraArgs)...)
		}
		return cmd

	case "python":
		// Try pylint first (more detailed output), fall back to flake8
		cmd := []string{"pylint", path}
		if extraArgs != "" {
			cmd = append(cmd, strings.Fields(extraArgs)...)
		}
		return cmd

	case "javascript", "typescript":
		cmd := []string{"eslint", path}
		if extraArgs != "" {
			cmd = append(cmd, strings.Fields(extraArgs)...)
		}
		return cmd

	case "rust":
		cmd := []string{"cargo", "clippy", "--message-format=short"}
		if extraArgs != "" {
			cmd = append(cmd, strings.Fields(extraArgs)...)
		}
		return cmd

	case "java":
		cmd := []string{"checkstyle", "-c", "/google_checks.xml", path}
		if extraArgs != "" {
			cmd = append(cmd, strings.Fields(extraArgs)...)
		}
		return cmd

	default:
		return nil
	}
}

// getTypeCheckCommand returns the type check command for the given language.
func (t DiagnosticsTool) getTypeCheckCommand(language, path, extraArgs string) []string {
	// Check for custom type checker
	if cmd, ok := t.CustomTypeCheckers[language]; ok {
		parts := strings.Fields(cmd)
		if extraArgs != "" {
			parts = append(parts, strings.Fields(extraArgs)...)
		}
		parts = append(parts, path)
		return parts
	}

	switch language {
	case "go":
		// Go is statically typed, use go build for type checking
		cmd := []string{"go", "build", "-o", "/dev/null", path}
		if extraArgs != "" {
			cmd = append(cmd, strings.Fields(extraArgs)...)
		}
		return cmd

	case "python":
		cmd := []string{"mypy", path}
		if extraArgs != "" {
			cmd = append(cmd, strings.Fields(extraArgs)...)
		}
		return cmd

	case "typescript":
		cmd := []string{"tsc", "--noEmit", path}
		if extraArgs != "" {
			cmd = append(cmd, strings.Fields(extraArgs)...)
		}
		return cmd

	case "rust":
		cmd := []string{"cargo", "check"}
		if extraArgs != "" {
			cmd = append(cmd, strings.Fields(extraArgs)...)
		}
		return cmd

	case "java":
		// Find .java files and compile them
		cmd := []string{"javac", "-d", "/tmp", "-sourcepath", path}
		if extraArgs != "" {
			cmd = append(cmd, strings.Fields(extraArgs)...)
		}
		return cmd

	default:
		return nil
	}
}

// getTestCommand returns the test command for the given language.
func (t DiagnosticsTool) getTestCommand(language, path, testArgs, extraArgs string) []string {
	switch language {
	case "go":
		cmd := []string{"go", "test", "-json"}
		if testArgs != "" {
			cmd = append(cmd, strings.Fields(testArgs)...)
		}
		if extraArgs != "" {
			cmd = append(cmd, strings.Fields(extraArgs)...)
		}
		cmd = append(cmd, path)
		return cmd

	case "python":
		cmd := []string{"pytest", "-v", "--tb=short"}
		if testArgs != "" {
			cmd = append(cmd, strings.Fields(testArgs)...)
		}
		if extraArgs != "" {
			cmd = append(cmd, strings.Fields(extraArgs)...)
		}
		cmd = append(cmd, path)
		return cmd

	case "javascript", "typescript":
		cmd := []string{"npm", "test", "--", "--verbose"}
		if testArgs != "" {
			cmd = append(cmd, strings.Fields(testArgs)...)
		}
		if extraArgs != "" {
			cmd = append(cmd, strings.Fields(extraArgs)...)
		}
		return cmd

	case "rust":
		cmd := []string{"cargo", "test"}
		if testArgs != "" {
			cmd = append(cmd, strings.Fields(testArgs)...)
		}
		if extraArgs != "" {
			cmd = append(cmd, strings.Fields(extraArgs)...)
		}
		return cmd

	case "java":
		cmd := []string{"mvn", "test"}
		if testArgs != "" {
			cmd = append(cmd, strings.Fields(testArgs)...)
		}
		if extraArgs != "" {
			cmd = append(cmd, strings.Fields(extraArgs)...)
		}
		return cmd

	default:
		return nil
	}
}

// getVetCommand returns the vet/static analysis command.
func (t DiagnosticsTool) getVetCommand(language, path, extraArgs string) []string {
	if language != "go" {
		return nil
	}

	cmd := []string{"go", "vet"}
	if extraArgs != "" {
		cmd = append(cmd, strings.Fields(extraArgs)...)
	}
	cmd = append(cmd, path)
	return cmd
}

// getFormatCommand returns the format check command.
func (t DiagnosticsTool) getFormatCommand(language, path, extraArgs string) []string {
	if language != "go" {
		return nil
	}

	cmd := []string{"gofmt", "-l"}
	if extraArgs != "" {
		cmd = append(cmd, strings.Fields(extraArgs)...)
	}
	cmd = append(cmd, path)
	return cmd
}

// getBuildCommand returns the build command.
func (t DiagnosticsTool) getBuildCommand(language, path, extraArgs string) []string {
	switch language {
	case "go":
		cmd := []string{"go", "build", "-o", "/dev/null"}
		if extraArgs != "" {
			cmd = append(cmd, strings.Fields(extraArgs)...)
		}
		cmd = append(cmd, path)
		return cmd

	case "rust":
		cmd := []string{"cargo", "build"}
		if extraArgs != "" {
			cmd = append(cmd, strings.Fields(extraArgs)...)
		}
		return cmd

	case "javascript", "typescript":
		cmd := []string{"npm", "run", "build"}
		if extraArgs != "" {
			cmd = append(cmd, strings.Fields(extraArgs)...)
		}
		return cmd

	case "java":
		cmd := []string{"mvn", "compile"}
		if extraArgs != "" {
			cmd = append(cmd, strings.Fields(extraArgs)...)
		}
		return cmd

	case "python":
		// Python doesn't have a standard build check, use compileall
		cmd := []string{"python", "-m", "compileall", "-q", path}
		if extraArgs != "" {
			cmd = append(cmd, strings.Fields(extraArgs)...)
		}
		return cmd

	default:
		return nil
	}
}

// detectLanguage attempts to detect the programming language of the project.
func (t DiagnosticsTool) detectLanguage(path string) string {
	// Check for explicit marker files first
	markerFiles := map[string]string{
		"go.mod":           "go",
		"pyproject.toml":   "python",
		"setup.py":         "python",
		"requirements.txt": "python",
		"Pipfile":          "python",
		"package.json":     "javascript",
		"tsconfig.json":    "typescript",
		"Cargo.toml":       "rust",
		"pom.xml":          "java",
		"build.gradle":     "java",
	}

	// Check for marker files
	entries, err := os.ReadDir(path)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				if lang, ok := markerFiles[entry.Name()]; ok {
					// Special case: if we have tsconfig.json AND package.json with typescript deps,
					// prefer typescript
					if lang == "javascript" {
						// Check if there's a tsconfig.json
						for _, e := range entries {
							if e.Name() == "tsconfig.json" {
								return "typescript"
							}
						}
					}
					return lang
				}
			}
		}

		// Check for source file extensions
		extCounts := make(map[string]int)
		for _, entry := range entries {
			if !entry.IsDir() {
				ext := filepath.Ext(entry.Name())
				if ext != "" {
					extCounts[ext]++
				}
			}
		}

		// Map extensions to languages
		extLangs := map[string]string{
			".go":   "go",
			".py":   "python",
			".js":   "javascript",
			".jsx":  "javascript",
			".ts":   "typescript",
			".tsx":  "typescript",
			".rs":   "rust",
			".java": "java",
		}

		// Find the most common extension
		maxCount := 0
		var detectedLang string
		for ext, count := range extCounts {
			if lang, ok := extLangs[ext]; ok && count > maxCount {
				maxCount = count
				detectedLang = lang
			}
		}
		if detectedLang != "" {
			return detectedLang
		}
	}

	// Default based on current runtime
	if runtime.GOOS == "windows" {
		return "go" // Reasonable default for this project
	}
	return "go"
}

// getTimeout returns the appropriate timeout based on diagnostic type.
func (t DiagnosticsTool) getTimeout(diagType DiagnosticType, requestedSeconds int) time.Duration {
	if requestedSeconds > 0 {
		timeout := time.Duration(requestedSeconds) * time.Second
		if t.MaxTimeout > 0 && timeout > t.MaxTimeout {
			return t.MaxTimeout
		}
		return timeout
	}

	switch diagType {
	case DiagTest:
		return t.TestTimeout
	default:
		return t.DefaultTimeout
	}
}

// parseOutput parses diagnostic output into structured issues.
func (t DiagnosticsTool) parseOutput(diagType DiagnosticType, language, output string) []DiagnosticIssue {
	if output == "" {
		return nil
	}

	// Select parser based on type and language
	switch {
	case diagType == DiagTest && language == "go":
		return parseGoTestJSON(output)
	case diagType == DiagVet && language == "go":
		return parseGoVetOutput(output)
	case diagType == DiagFormat && language == "go":
		return parseGofmtOutput(output)
	case diagType == DiagLint && language == "go":
		return parseGoLintOutput(output)
	case diagType == DiagTypeCheck && language == "go":
		return parseGoBuildOutput(output)
	case diagType == DiagBuild && language == "go":
		return parseGoBuildOutput(output)
	case diagType == DiagLint && language == "python":
		return parsePylintOutput(output)
	case diagType == DiagTypeCheck && language == "python":
		return parseMypyOutput(output)
	case diagType == DiagTest && language == "python":
		return parsePytestOutput(output)
	case (diagType == DiagLint || diagType == DiagTypeCheck) && (language == "javascript" || language == "typescript"):
		return parseESLintOutput(output)
	case diagType == DiagTest && (language == "javascript" || language == "typescript"):
		return parseJestOutput(output)
	case diagType == DiagLint && language == "rust":
		return parseClippyOutput(output)
	default:
		// Generic parser - try to extract file:line:col patterns
		return parseGenericOutput(output)
	}
}

// filterBySeverity filters issues to only include those at or above the specified severity.
func (t DiagnosticsTool) filterBySeverity(issues []DiagnosticIssue, minSeverity string) []DiagnosticIssue {
	severityOrder := map[DiagnosticSeverity]int{
		SeverityError:   0,
		SeverityWarning: 1,
		SeverityInfo:    2,
		SeverityNote:    3,
	}

	minLevel, ok := severityOrder[DiagnosticSeverity(minSeverity)]
	if !ok {
		return issues
	}

	var filtered []DiagnosticIssue
	for _, issue := range issues {
		issueLevel, ok := severityOrder[issue.Severity]
		if ok && issueLevel <= minLevel {
			filtered = append(filtered, issue)
		}
	}
	return filtered
}

// resolvePath resolves a user-provided path to an absolute path.
func (t DiagnosticsTool) resolvePath(userPath string) (string, error) {
	root := t.Root
	if root == "" {
		root = "."
	}
	root = filepath.Clean(root)

	if userPath == "" {
		userPath = "."
	}
	userPath = filepath.Clean(userPath)

	if filepath.IsAbs(userPath) {
		if !t.AllowAbsolute {
			return "", fmt.Errorf("absolute paths not allowed: %s", userPath)
		}
		return userPath, nil
	}

	resolved := filepath.Clean(filepath.Join(root, userPath))

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	if !strings.HasPrefix(absResolved, absRoot+string(filepath.Separator)) && absResolved != absRoot {
		return "", fmt.Errorf("path traversal detected: %s resolves outside root %s", userPath, absRoot)
	}

	return absResolved, nil
}

// ---------------------------------------------------------------------------
// Go Output Parsers
// ---------------------------------------------------------------------------

// goTestEvent represents a JSON-encoded Go test event.
type goTestEvent struct {
	Time    string  `json:"Time"`
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Output  string  `json:"Output"`
	Elapsed float64 `json:"Elapsed"`
}

// parseGoTestJSON parses Go test JSON output.
func parseGoTestJSON(output string) []DiagnosticIssue {
	var issues []DiagnosticIssue
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "{") {
			continue
		}

		var event goTestEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		// Look for failure output
		if event.Action == "output" && event.Output != "" {
			// Parse failure messages
			if strings.Contains(event.Output, "FAIL") || strings.Contains(event.Output, "Error") {
				issue := DiagnosticIssue{
					File:     event.Package,
					Severity: SeverityError,
					Message:  strings.TrimSpace(event.Output),
				}
				// Try to extract file:line from the output
				if match := goFileLineRegex.FindStringSubmatch(event.Output); len(match) >= 3 {
					issue.File = match[1]
					if lineNum, err := parseInt(match[2]); err == nil {
						issue.Line = lineNum
					}
				}
				issues = append(issues, issue)
			}
		}

		// Look for test failures
		if event.Action == "fail" {
			issues = append(issues, DiagnosticIssue{
				File:     event.Package,
				Severity: SeverityError,
				Message:  fmt.Sprintf("Test failed: %s", event.Test),
				Category: "test",
			})
		}
	}

	return issues
}

// parseGoVetOutput parses go vet output.
func parseGoVetOutput(output string) []DiagnosticIssue {
	var issues []DiagnosticIssue

	for _, match := range goVetRegex.FindAllStringSubmatch(output, -1) {
		if len(match) < 5 {
			continue
		}
		issue := DiagnosticIssue{
			File:     match[1],
			Severity: SeverityWarning,
			Message:  match[4],
		}
		if lineNum, err := parseInt(match[2]); err == nil {
			issue.Line = lineNum
		}
		if colNum, err := parseInt(match[3]); err == nil {
			issue.Column = colNum
		}
		issues = append(issues, issue)
	}

	return issues
}

// parseGofmtOutput parses gofmt -l output (just lists unformatted files).
func parseGofmtOutput(output string) []DiagnosticIssue {
	var issues []DiagnosticIssue

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			issues = append(issues, DiagnosticIssue{
				File:     line,
				Severity: SeverityWarning,
				Message:  "File is not properly formatted (run gofmt -w)",
				Rule:     "gofmt",
				Fix:      "gofmt -w " + line,
				Category: "formatting",
			})
		}
	}

	return issues
}

// parseGoLintOutput parses staticcheck output.
func parseGoLintOutput(output string) []DiagnosticIssue {
	var issues []DiagnosticIssue

	for _, match := range staticcheckRegex.FindAllStringSubmatch(output, -1) {
		if len(match) < 5 {
			continue
		}
		severity := SeverityWarning
		if strings.Contains(match[1], "SA") || strings.Contains(match[1], "S") {
			severity = SeverityError // Security and bug-risk checks are errors
		}
		issue := DiagnosticIssue{
			File:     match[2],
			Severity: severity,
			Message:  match[5],
			Rule:     match[1],
		}
		if lineNum, err := parseInt(match[3]); err == nil {
			issue.Line = lineNum
		}
		if colNum, err := parseInt(match[4]); err == nil {
			issue.Column = colNum
		}
		issues = append(issues, issue)
	}

	// Also try generic Go error format
	if len(issues) == 0 {
		issues = parseGoErrorFormat(output)
	}

	return issues
}

// parseGoBuildOutput parses go build error output.
func parseGoBuildOutput(output string) []DiagnosticIssue {
	return parseGoErrorFormat(output)
}

// parseGoErrorFormat parses the standard Go error format: file:line:col: message
func parseGoErrorFormat(output string) []DiagnosticIssue {
	var issues []DiagnosticIssue

	for _, match := range goFileLineRegex.FindAllStringSubmatch(output, -1) {
		if len(match) < 3 {
			continue
		}
		issue := DiagnosticIssue{
			File:     match[1],
			Severity: SeverityError,
			Message:  match[3],
		}
		if lineNum, err := parseInt(match[2]); err == nil {
			issue.Line = lineNum
		}
		issues = append(issues, issue)
	}

	return issues
}

// ---------------------------------------------------------------------------
// Python Output Parsers
// ---------------------------------------------------------------------------

// parsePylintOutput parses pylint output.
func parsePylintOutput(output string) []DiagnosticIssue {
	var issues []DiagnosticIssue

	for _, match := range pylintRegex.FindAllStringSubmatch(output, -1) {
		if len(match) < 6 {
			continue
		}
		severity := parsePylintSeverity(match[1])
		issue := DiagnosticIssue{
			File:     match[2],
			Severity: severity,
			Message:  match[5],
			Rule:     match[4],
			Category: match[3],
		}
		// Parse line from the file path if it's in format "file:line"
		if parts := strings.Split(match[2], ":"); len(parts) >= 2 {
			issue.File = parts[0]
			if lineNum, err := parseInt(parts[1]); err == nil {
				issue.Line = lineNum
			}
		}
		issues = append(issues, issue)
	}

	return issues
}

// parseMypyOutput parses mypy output.
func parseMypyOutput(output string) []DiagnosticIssue {
	var issues []DiagnosticIssue

	for _, match := range mypyRegex.FindAllStringSubmatch(output, -1) {
		if len(match) < 5 {
			continue
		}
		severity := SeverityError
		if strings.Contains(match[1], "warning") {
			severity = SeverityWarning
		} else if strings.Contains(match[1], "note") {
			severity = SeverityNote
		}
		issue := DiagnosticIssue{
			File:     match[2],
			Severity: severity,
			Message:  match[4],
		}
		if lineNum, err := parseInt(match[3]); err == nil {
			issue.Line = lineNum
		}
		issues = append(issues, issue)
	}

	return issues
}

// parsePytestOutput parses pytest output.
func parsePytestOutput(output string) []DiagnosticIssue {
	var issues []DiagnosticIssue

	// Parse FAILED lines
	for _, match := range pytestFailRegex.FindAllStringSubmatch(output, -1) {
		if len(match) < 2 {
			continue
		}
		issue := DiagnosticIssue{
			File:     match[1],
			Severity: SeverityError,
			Message:  "Test failed",
			Category: "test",
		}
		issues = append(issues, issue)
	}

	// Parse error traceback lines
	for _, match := range pytestErrorRegex.FindAllStringSubmatch(output, -1) {
		if len(match) < 4 {
			continue
		}
		issue := DiagnosticIssue{
			File:     match[1],
			Severity: SeverityError,
			Message:  match[3],
			Category: "error",
		}
		if lineNum, err := parseInt(match[2]); err == nil {
			issue.Line = lineNum
		}
		issues = append(issues, issue)
	}

	return issues
}

// parsePylintSeverity converts a pylint message type to severity.
func parsePylintSeverity(msgType string) DiagnosticSeverity {
	switch strings.ToLower(msgType) {
	case "error", "fatal":
		return SeverityError
	case "warning":
		return SeverityWarning
	case "convention", "refactor":
		return SeverityInfo
	case "info":
		return SeverityNote
	default:
		return SeverityWarning
	}
}

// ---------------------------------------------------------------------------
// JavaScript/TypeScript Output Parsers
// ---------------------------------------------------------------------------

// parseESLintOutput parses ESLint output (default format).
func parseESLintOutput(output string) []DiagnosticIssue {
	var issues []DiagnosticIssue

	for _, match := range eslintRegex.FindAllStringSubmatch(output, -1) {
		if len(match) < 5 {
			continue
		}
		severity := SeverityError
		if match[2] == "warning" {
			severity = SeverityWarning
		}
		issue := DiagnosticIssue{
			File:     match[1],
			Severity: severity,
			Message:  match[4],
			Rule:     match[3],
		}
		_ = issue // issue is populated below via direct parsing
		// ESLint format: file:line:col: severity: message (rule)
		parts := strings.SplitN(match[0], ":", 4)
		if len(parts) >= 4 {
			issue.File = parts[0]
			if lineNum, err := parseInt(parts[1]); err == nil {
				issue.Line = lineNum
			}
			if colNum, err := parseInt(parts[2]); err == nil {
				issue.Column = colNum
			}
			rest := strings.TrimSpace(parts[3])
			restParts := strings.SplitN(rest, " ", 2)
			if len(restParts) >= 1 {
				switch restParts[0] {
				case "error":
					issue.Severity = SeverityError
				case "warning":
					issue.Severity = SeverityWarning
				}
			}
			if len(restParts) >= 2 {
				issue.Message = restParts[1]
				// Extract rule from parentheses
				if idx := strings.LastIndex(issue.Message, "("); idx >= 0 {
					ruleStr := issue.Message[idx:]
					if strings.HasPrefix(ruleStr, "(") && strings.HasSuffix(ruleStr, ")") {
						issue.Rule = ruleStr[1 : len(ruleStr)-1]
						issue.Message = strings.TrimSpace(issue.Message[:idx])
					}
				}
			}
		}
		issues = append(issues, issue)
	}

	return issues
}

// parseJestOutput parses Jest test output.
func parseJestOutput(output string) []DiagnosticIssue {
	var issues []DiagnosticIssue

	// Parse FAIL lines
	for _, match := range jestFailRegex.FindAllStringSubmatch(output, -1) {
		if len(match) < 2 {
			continue
		}
		issues = append(issues, DiagnosticIssue{
			File:     match[1],
			Severity: SeverityError,
			Message:  "Test failed",
			Category: "test",
		})
	}

	// Parse error lines with file locations
	for _, match := range jestErrorRegex.FindAllStringSubmatch(output, -1) {
		if len(match) < 4 {
			continue
		}
		issue := DiagnosticIssue{
			File:     match[1],
			Severity: SeverityError,
			Message:  match[3],
			Category: "error",
		}
		if n, err := parseInt(match[2]); err == nil {
			issue.Line = n
		}
		issues = append(issues, issue)
	}

	return issues
}

// ---------------------------------------------------------------------------
// Rust Output Parsers
// ---------------------------------------------------------------------------

// parseClippyOutput parses cargo clippy output.
func parseClippyOutput(output string) []DiagnosticIssue {
	var issues []DiagnosticIssue

	for _, match := range clippyRegex.FindAllStringSubmatch(output, -1) {
		if len(match) < 6 {
			continue
		}
		severity := parseRustSeverity(match[1])
		issue := DiagnosticIssue{
			File:     match[2],
			Severity: severity,
			Message:  match[5],
		}
		if lineNum, err := parseInt(match[3]); err == nil {
			issue.Line = lineNum
		}
		if colNum, err := parseInt(match[4]); err == nil {
			issue.Column = colNum
		}
		issues = append(issues, issue)
	}

	return issues
}

// parseRustSeverity converts a Rust diagnostic level to severity.
func parseRustSeverity(level string) DiagnosticSeverity {
	switch strings.ToLower(level) {
	case "error":
		return SeverityError
	case "warning":
		return SeverityWarning
	case "note":
		return SeverityNote
	case "help":
		return SeverityInfo
	default:
		return SeverityWarning
	}
}

// ---------------------------------------------------------------------------
// Generic Parser
// ---------------------------------------------------------------------------

// parseGenericOutput attempts to parse any diagnostic output using common patterns.
func parseGenericOutput(output string) []DiagnosticIssue {
	var issues []DiagnosticIssue

	// Pattern 1: file:line:col: message
	for _, match := range genericFileLineColRegex.FindAllStringSubmatch(output, -1) {
		if len(match) < 5 {
			continue
		}
		issue := DiagnosticIssue{
			File:     match[1],
			Severity: SeverityError,
			Message:  match[4],
		}
		if lineNum, err := parseInt(match[2]); err == nil {
			issue.Line = lineNum
		}
		if colNum, err := parseInt(match[3]); err == nil {
			issue.Column = colNum
		}
		// Try to detect severity from message
		if strings.Contains(strings.ToLower(match[4]), "warning") {
			issue.Severity = SeverityWarning
		}
		issues = append(issues, issue)
	}

	// Pattern 2: file:line: message (no column)
	if len(issues) == 0 {
		for _, match := range genericFileLineRegex.FindAllStringSubmatch(output, -1) {
			if len(match) < 4 {
				continue
			}
			issue := DiagnosticIssue{
				File:     match[1],
				Severity: SeverityError,
				Message:  match[3],
			}
			if lineNum, err := parseInt(match[2]); err == nil {
				issue.Line = lineNum
			}
			if strings.Contains(strings.ToLower(match[3]), "warning") {
				issue.Severity = SeverityWarning
			}
			issues = append(issues, issue)
		}
	}

	return issues
}

// ---------------------------------------------------------------------------
// Regular Expressions
// ---------------------------------------------------------------------------

var (
	// Go error format: file:line:col: message
	goFileLineRegex = regexp.MustCompile(`^([^:]+):(\d+):(\d+):\s*(.+)$`)

	// go vet format: file:line:col: message
	goVetRegex = regexp.MustCompile(`^([^:]+):(\d+):(\d+):\s*(.+)$`)

	// staticcheck format: file:line:col: rule message
	staticcheckRegex = regexp.MustCompile(`^([^:]+):(\d+):(\d+):\s*(\S+)\s+(.+)$`)

	// pylint format: type: module:line: message (category)
	pylintRegex = regexp.MustCompile(`^(\w+):\s*([^:]+):\s*(\w+):\s*\(([^)]+)\)\s*(.+)$`)

	// mypy format: file:line: error/warning: message
	mypyRegex = regexp.MustCompile(`^([^:]+):(\d+):\s*(error|warning|note):\s*(.+)$`)

	// pytest FAIL format: FAILED file::test_name
	pytestFailRegex = regexp.MustCompile(`^FAILED\s+(.+)$`)

	// pytest error format: file:line: error message
	pytestErrorRegex = regexp.MustCompile(`^([^:]+):(\d+):\s*(.+)$`)

	// eslint format: file:line:col: error/warning message (rule)
	eslintRegex = regexp.MustCompile(`^([^:]+:\d+:\d+):\s*(error|warning)\s+(.+)$`)

	// jest FAIL format: FAIL path/to/file
	jestFailRegex = regexp.MustCompile(`^FAIL\s+(.+)$`)

	// jest error format: file (line:col) message
	jestErrorRegex = regexp.MustCompile(`^\s*at\s+.+\s+\(([^:]+):(\d+):\d+\)\s*$`)

	// clippy format: level file:line:col message
	clippyRegex = regexp.MustCompile(`^(error|warning|note|help)[^:]*:\s*([^:]+):(\d+):(\d+):\s*(.+)$`)

	// Generic file:line:col: message
	genericFileLineColRegex = regexp.MustCompile(`^([^:\s]+):(\d+):(\d+):\s*(.+)$`)

	// Generic file:line: message
	genericFileLineRegex = regexp.MustCompile(`^([^:\s]+):(\d+):\s*(.+)$`)
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// parseInt parses a string to an int, returning 0 on error.
func parseInt(s string) (int, error) {
	s = strings.TrimSpace(s)
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// marshalDiagnosticsError creates a JSON error response for diagnostics.
func marshalDiagnosticsError(err error) (json.RawMessage, error) {
	output := DiagnosticsOutput{
		Error: err.Error(),
	}
	return json.Marshal(output)
}
