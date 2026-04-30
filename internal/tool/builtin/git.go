// Package builtin provides built-in tools for the Orchestra tool system.
package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Git Operations Tool
// ---------------------------------------------------------------------------

// GitOperationType defines the type of git operation to perform.
type GitOperationType string

const (
	// GitDiff shows differences between commits, branches, or working tree.
	GitDiff GitOperationType = "diff"

	// GitLog shows commit history.
	GitLog GitOperationType = "log"

	// GitBlame shows who last modified each line of a file.
	GitBlame GitOperationType = "blame"

	// GitStatus shows the working tree status.
	GitStatus GitOperationType = "status"

	// GitApply applies a patch to the working tree.
	GitApply GitOperationType = "apply"

	// GitShow shows various types of objects (commits, blobs, tags).
	GitShow GitOperationType = "show"

	// GitBranch lists, creates, or deletes branches.
	GitBranch GitOperationType = "branch"

	// GitStash stashes changes away.
	GitStash GitOperationType = "stash"
)

// GitOperationsInput defines the input for the git_operations tool.
type GitOperationsInput struct {
	// Operation is the git operation to perform.
	Operation GitOperationType `json:"operation" description:"Git operation to perform" enum:"diff,log,blame,status,apply,show,branch,stash"`

	// Args are additional arguments for the git command.
	// These are appended to the git command after the operation.
	// Examples:
	//   - diff: "HEAD~1", "main..feature", "--cached", "path/to/file"
	//   - log: "-10", "--oneline", "--since='1 week ago'", "main..HEAD"
	//   - blame: "path/to/file", "-L 10,20 path/to/file"
	//   - show: "HEAD", "abc123", "HEAD:path/to/file"
	//   - branch: "-a", "-v", "new-branch", "-d old-branch"
	//   - stash: "list", "pop", "drop stash@{0}"
	Args string `json:"args,omitempty" description:"Additional arguments for the git command"`

	// Patch is the patch content to apply (only for 'apply' operation).
	Patch string `json:"patch,omitempty" description:"Patch content to apply (for 'apply' operation only)"`

	// RepoPath is the path to the git repository. Defaults to root.
	RepoPath string `json:"repo_path,omitempty" description:"Path to the git repository (relative to root)"`

	// TimeoutSeconds is the maximum execution time. Defaults to 30.
	TimeoutSeconds int `json:"timeout_seconds,omitempty" description:"Maximum execution time in seconds" default:"30" min:"1" max:"300"`

	// MaxOutputBytes limits the output size. Defaults to 1MB.
	MaxOutputBytes int `json:"max_output_bytes,omitempty" description:"Maximum output size in bytes" default:"1048576" min:"1024" max:"10485760"`
}

// GitOperationsOutput defines the output of the git_operations tool.
type GitOperationsOutput struct {
	// Operation is the operation that was performed.
	Operation GitOperationType `json:"operation"`

	// Output contains the command's output.
	Output string `json:"output"`

	// ExitCode is the git command's exit code.
	ExitCode int `json:"exit_code"`

	// Truncated indicates if the output was truncated.
	Truncated bool `json:"truncated,omitempty"`

	// DurationMs is the execution time in milliseconds.
	DurationMs int64 `json:"duration_ms"`

	// RepoPath is the resolved repository path.
	RepoPath string `json:"repo_path"`

	// Command shows the actual git command that was executed.
	Command string `json:"command"`

	// Error contains an error message if the operation failed.
	Error string `json:"error,omitempty"`
}

// GitOperationsTool implements the git_operations built-in tool.
// It provides safe access to common git commands for understanding
// and modifying codebases.
//
// Read-only operations (diff, log, blame, status, show, branch list) are
// always allowed. Write operations (apply, branch create/delete, stash)
// can be disabled for additional safety.
//
// The tool enforces:
//   - Repository path restrictions (root directory)
//   - Output size limits
//   - Execution timeouts
//   - Optional write operation blocking
type GitOperationsTool struct {
	// Root is the root directory for repository access.
	Root string

	// AllowAbsolute controls whether absolute paths outside the root are allowed.
	AllowAbsolute bool

	// AllowWrite controls whether write operations are permitted.
	// When false, only read operations (diff, log, blame, status, show, branch list) are allowed.
	// Write operations include: apply, branch create/delete, stash.
	AllowWrite bool

	// BlockedArgs is a list of argument patterns that are blocked.
	// This provides defense-in-depth against dangerous git commands.
	BlockedArgs []string

	// DefaultTimeout is the default timeout if not specified.
	DefaultTimeout time.Duration

	// MaxTimeout is the maximum allowed timeout.
	MaxTimeout time.Duration

	// MaxOutputBytes is the default maximum output size.
	MaxOutputBytes int64
}

// NewGitOperationsTool creates a git_operations tool with default settings.
// Write operations are disabled by default for safety.
func NewGitOperationsTool() GitOperationsTool {
	return GitOperationsTool{
		AllowWrite:     false,
		BlockedArgs:    defaultGitBlockedArgs(),
		DefaultTimeout: 30 * time.Second,
		MaxTimeout:     2 * time.Minute,
		MaxOutputBytes: 1 * 1024 * 1024,
	}
}

// NewGitOperationsToolWithRoot creates a git_operations tool restricted to a root directory.
func NewGitOperationsToolWithRoot(root string) GitOperationsTool {
	t := NewGitOperationsTool()
	t.Root = root
	return t
}

// NewGitOperationsToolReadWrite creates a git_operations tool with write access enabled.
func NewGitOperationsToolReadWrite(root string) GitOperationsTool {
	t := NewGitOperationsToolWithRoot(root)
	t.AllowWrite = true
	return t
}

// defaultGitBlockedArgs returns patterns for dangerous git arguments.
func defaultGitBlockedArgs() []string {
	return []string{
		"--exec=",
		"--upload-pack=",
		"--receive-pack=",
		"config --global",
		"config --system",
	}
}

// Name returns the tool's identifier.
func (t GitOperationsTool) Name() string { return "git_operations" }

// Description returns the tool's description for the LLM.
func (t GitOperationsTool) Description() string {
	return `Execute git commands to inspect and modify repositories.

Supported operations:
- diff: Show differences between commits, branches, or working tree
- log: Show commit history (use -10 for last 10 commits, --oneline for compact)
- blame: Show who last modified each line of a file
- status: Show working tree status (modified, staged, untracked files)
- show: Display commit details, file contents at a commit, or tag info
- branch: List, create, or delete branches (-a for remote, -v for verbose)
- stash: Stash and manage changes (list, pop, apply, drop)
- apply: Apply a patch to the working tree

Common usage patterns:
- "diff HEAD~1" - changes in the last commit
- "diff main..feature" - differences between branches
- "log --oneline -10" - last 10 commits in compact form
- "blame path/to/file.go" - line-by-line author info
- "status" - current repository state
- "show HEAD:path/to/file.go" - file contents at HEAD
- "branch -a" - list all branches including remotes

Note: Write operations (apply, branch create/delete, stash) may be disabled
for safety depending on configuration.`
}

// Parameters returns the JSON Schema for the tool's input.
func (t GitOperationsTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"operation": {
				"type": "string",
				"description": "Git operation to perform",
				"enum": ["diff", "log", "blame", "status", "apply", "show", "branch", "stash"]
			},
			"args": {
				"type": "string",
				"description": "Additional arguments for the git command"
			},
			"patch": {
				"type": "string",
				"description": "Patch content to apply (for 'apply' operation only)"
			},
			"repo_path": {
				"type": "string",
				"description": "Path to the git repository (relative to root)"
			},
			"timeout_seconds": {
				"type": "integer",
				"description": "Maximum execution time in seconds",
				"default": 30,
				"minimum": 1,
				"maximum": 300
			},
			"max_output_bytes": {
				"type": "integer",
				"description": "Maximum output size in bytes",
				"default": 1048576,
				"minimum": 1024,
				"maximum": 10485760
			}
		},
		"required": ["operation"]
	}`)
}

// Execute performs the git operation.
func (t GitOperationsTool) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req GitOperationsInput
	if err := json.Unmarshal(input, &req); err != nil {
		return marshalGitError(fmt.Errorf("parse input: %w", err))
	}

	if req.Operation == "" {
		return marshalGitError(fmt.Errorf("operation is required"))
	}

	// Check context
	if ctx.Err() != nil {
		return marshalGitError(ctx.Err())
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

	// Validate timeout
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if t.MaxTimeout > 0 && timeout > t.MaxTimeout {
		timeout = t.MaxTimeout
	}

	// Check if operation is valid
	if !isValidGitOperation(req.Operation) {
		return marshalGitError(fmt.Errorf("unknown operation %q (valid: diff, log, blame, status, apply, show, branch, stash)", req.Operation))
	}

	// Check write permissions
	if isWriteOperation(req.Operation) && !t.AllowWrite {
		return marshalGitError(fmt.Errorf("write operation %q is not allowed (AllowWrite is false)", req.Operation))
	}

	// Validate arguments
	if err := t.validateArgs(req.Operation, req.Args); err != nil {
		return marshalGitError(err)
	}

	// Handle apply operation specially (patch is provided directly, not via args)
	if req.Operation == GitApply {
		return t.executeApply(ctx, req, timeout, maxOutput)
	}

	// Resolve repository path
	repoPath, err := t.resolveRepoPath(req.RepoPath)
	if err != nil {
		return marshalGitError(err)
	}

	// Build the git command
	gitArgs := []string{string(req.Operation)}
	if req.Args != "" {
		gitArgs = append(gitArgs, strings.Fields(req.Args)...)
	}

	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Execute the command
	startTime := time.Now()
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	cmd.Dir = repoPath

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitedWriter{buf: &stdout, limit: maxOutput}
	cmd.Stderr = &limitedWriter{buf: &stderr, limit: maxOutput / 10} // Less space for stderr

	err = cmd.Run()
	duration := time.Since(startTime)

	// Build output
	output := GitOperationsOutput{
		Operation:  req.Operation,
		RepoPath:   repoPath,
		Command:    "git " + strings.Join(gitArgs, " "),
		DurationMs: duration.Milliseconds(),
	}

	// Set exit code
	if exitErr, ok := err.(*exec.ExitError); ok {
		output.ExitCode = exitErr.ExitCode()
	} else if err != nil {
		output.ExitCode = -1
	}

	// Check for timeout
	if ctx.Err() == context.DeadlineExceeded {
		output.ExitCode = -1
		output.Error = "git command timed out"
	} else if err != nil && output.ExitCode == -1 {
		output.Error = fmt.Sprintf("failed to execute git: %v", err)
	}

	// Combine output (stdout + stderr if there's an error)
	output.Output = stdout.String()
	if output.ExitCode != 0 && stderr.Len() > 0 {
		if output.Output != "" {
			output.Output += "\n"
		}
		output.Output += stderr.String()
	}
	output.Truncated = stdout.Len() >= int(maxOutput)

	return json.Marshal(output)
}

// executeApply handles the apply operation which uses stdin for the patch.
func (t GitOperationsTool) executeApply(ctx context.Context, req GitOperationsInput, timeout time.Duration, maxOutput int64) (json.RawMessage, error) {
	if req.Patch == "" {
		return marshalGitError(fmt.Errorf("patch content is required for 'apply' operation"))
	}

	// Resolve repository path
	repoPath, err := t.resolveRepoPath(req.RepoPath)
	if err != nil {
		return marshalGitError(err)
	}

	// Build git apply command
	gitArgs := []string{"apply"}
	if req.Args != "" {
		gitArgs = append(gitArgs, strings.Fields(req.Args)...)
	}

	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Execute with patch as stdin
	startTime := time.Now()
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	cmd.Dir = repoPath
	cmd.Stdin = strings.NewReader(req.Patch)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitedWriter{buf: &stdout, limit: maxOutput}
	cmd.Stderr = &limitedWriter{buf: &stderr, limit: maxOutput}

	err = cmd.Run()
	duration := time.Since(startTime)

	output := GitOperationsOutput{
		Operation:  GitApply,
		RepoPath:   repoPath,
		Command:    "git apply " + req.Args,
		DurationMs: duration.Milliseconds(),
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		output.ExitCode = exitErr.ExitCode()
	} else if err != nil {
		output.ExitCode = -1
	}

	if ctx.Err() == context.DeadlineExceeded {
		output.ExitCode = -1
		output.Error = "git apply timed out"
	} else if err != nil && output.ExitCode == -1 {
		output.Error = fmt.Sprintf("failed to execute git apply: %v", err)
	}

	output.Output = stdout.String()
	if output.ExitCode != 0 && stderr.Len() > 0 {
		if output.Output != "" {
			output.Output += "\n"
		}
		output.Output += stderr.String()
	}

	return json.Marshal(output)
}

// validateArgs checks the arguments for blocked patterns.
func (t GitOperationsTool) validateArgs(operation GitOperationType, args string) error {
	if args == "" {
		return nil
	}

	for _, blocked := range t.BlockedArgs {
		if strings.Contains(args, blocked) {
			return fmt.Errorf("arguments contain blocked pattern: %q", blocked)
		}
	}

	// Additional validation for specific operations
	switch operation {
	case GitBranch:
		// Block force push related args
		if strings.Contains(args, "--force") || strings.Contains(args, "-f ") {
			if !t.AllowWrite {
				return fmt.Errorf("force operations are not allowed")
			}
		}
	case GitStash:
		// Block stash clear (too destructive)
		if strings.Contains(args, "clear") {
			return fmt.Errorf("'stash clear' is not allowed (too destructive)")
		}
	}

	return nil
}

// resolveRepoPath resolves the repository path to an absolute path.
func (t GitOperationsTool) resolveRepoPath(requestedPath string) (string, error) {
	root := t.Root
	if root == "" {
		root = "."
	}
	root = filepath.Clean(root)

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}

	if requestedPath == "" {
		return absRoot, nil
	}

	requestedPath = filepath.Clean(requestedPath)
	absRequested, err := filepath.Abs(filepath.Join(absRoot, requestedPath))
	if err != nil {
		return "", fmt.Errorf("resolve repo path: %w", err)
	}

	if !t.AllowAbsolute && filepath.IsAbs(requestedPath) {
		return "", fmt.Errorf("absolute paths not allowed: %s", requestedPath)
	}

	if !strings.HasPrefix(absRequested, absRoot+string(filepath.Separator)) && absRequested != absRoot {
		return "", fmt.Errorf("repository path %q is outside the allowed root %q", requestedPath, absRoot)
	}

	return absRequested, nil
}

// isValidGitOperation checks if the operation is a supported git operation.
func isValidGitOperation(op GitOperationType) bool {
	switch op {
	case GitDiff, GitLog, GitBlame, GitStatus, GitApply, GitShow, GitBranch, GitStash:
		return true
	default:
		return false
	}
}

// isWriteOperation returns true if the operation modifies the repository.
func isWriteOperation(op GitOperationType) bool {
	switch op {
	case GitApply:
		return true
	case GitBranch:
		// branch can be read-only (list) or write (create/delete)
		// We conservatively treat it as write; the caller can use args to list
		return true
	case GitStash:
		return true
	default:
		return false
	}
}

// isReadOnlyBranchArgs returns true if the branch args indicate a read-only operation.
func isReadOnlyBranchArgs(args string) bool {
	// Common read-only branch patterns
	readOnlyPatterns := []string{
		"-a", "-v", "-r", "--all", "--verbose", "--remotes",
		"--list", "--contains", "--no-merged", "--merged",
	}
	args = strings.TrimSpace(args)
	if args == "" || args[0] == '-' {
		// Args starting with - are typically flags (read-only)
		for _, pat := range readOnlyPatterns {
			if strings.Contains(args, pat) {
				return true
			}
		}
	}
	return false
}

// marshalGitError creates a JSON error response for git_operations.
func marshalGitError(err error) (json.RawMessage, error) {
	output := GitOperationsOutput{
		Error: err.Error(),
	}
	return json.Marshal(output)
}
