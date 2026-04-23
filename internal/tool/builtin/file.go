// Package builtin provides built-in tools for the Orchestra tool system.
package builtin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// File Read Tool
// ---------------------------------------------------------------------------

// FileReadInput defines the input for the file_read tool.
type FileReadInput struct {
	// Path is the file path to read. Relative paths are resolved against
	// the tool's root directory.
	Path string `json:"path" description:"Path to the file to read (relative to root or absolute if allowed)"`

	// Encoding specifies the file encoding. Defaults to "utf-8".
	Encoding string `json:"encoding,omitempty" description:"File encoding (utf-8, binary)" default:"utf-8" enum:"utf-8,binary"`

	// Offset is the byte offset to start reading from. Zero means start of file.
	Offset int64 `json:"offset,omitempty" description:"Byte offset to start reading from" default:"0" min:"0"`

	// Limit is the maximum number of bytes to read. Zero means no limit.
	Limit int64 `json:"limit,omitempty" description:"Maximum bytes to read (0 for no limit)" default:"0" min:"0"`

	// IncludeLineNumbers adds line numbers to the output when true.
	IncludeLineNumbers bool `json:"include_line_numbers,omitempty" description:"Prefix each line with its line number" default:"false"`

	// LineStart is the 1-based line number to start reading from. Only used
	// when IncludeLineNumbers is true and LineEnd is set. Overrides Offset.
	LineStart int `json:"line_start,omitempty" description:"1-based line number to start reading from" min:"1"`

	// LineEnd is the 1-based line number to stop reading at (inclusive).
	// Only used when IncludeLineNumbers is true and LineStart is set.
	LineEnd int `json:"line_end,omitempty" description:"1-based line number to stop reading at (inclusive)" min:"1"`
}

// FileReadOutput defines the output of the file_read tool.
type FileReadOutput struct {
	// Content is the file contents.
	Content string `json:"content"`

	// Size is the file size in bytes.
	Size int64 `json:"size"`

	// Path is the resolved absolute path that was read.
	Path string `json:"path"`

	// BytesRead is the number of bytes actually read.
	BytesRead int64 `json:"bytes_read"`

	// Truncated indicates if the output was truncated due to the limit.
	Truncated bool `json:"truncated,omitempty"`

	// Encoding is the encoding used to read the file.
	Encoding string `json:"encoding"`

	// LineCount is the number of lines in the content (if text mode).
	LineCount int `json:"line_count,omitempty"`

	// Error contains an error message if reading failed.
	Error string `json:"error,omitempty"`
}

// FileReadTool implements the file_read built-in tool.
// It reads file contents with support for byte ranges, line ranges,
// and line numbering.
//
// The tool can be configured with a root directory that restricts
// file access to a specific directory tree for security.
type FileReadTool struct {
	// Root is the root directory for file access. If empty, the current
	// working directory is used. File paths are resolved relative to
	// this directory unless they are absolute.
	Root string

	// AllowAbsolute controls whether absolute paths outside the root
	// are allowed. When false (default), all paths are resolved
	// relative to the root directory.
	AllowAbsolute bool

	// MaxFileSize is the maximum file size in bytes that can be read.
	// Defaults to 10MB. Files larger than this will return an error.
	MaxFileSize int64
}

// NewFileReadTool creates a file_read tool with default settings.
func NewFileReadTool() FileReadTool {
	return FileReadTool{
		MaxFileSize: 10 * 1024 * 1024, // 10MB
	}
}

// NewFileReadToolWithRoot creates a file_read tool restricted to a root directory.
func NewFileReadToolWithRoot(root string) FileReadTool {
	return FileReadTool{
		Root:        root,
		MaxFileSize: 10 * 1024 * 1024,
	}
}

// Name returns the tool's identifier.
func (t FileReadTool) Name() string { return "file_read" }

// Description returns the tool's description for the LLM.
func (t FileReadTool) Description() string {
	return `Read the contents of a file.

Returns the file content as a string. Supports reading specific byte ranges
or line ranges, and can optionally include line numbers.

Options:
- Set include_line_numbers to true to prefix each line with its number
- Use line_start and line_end to read a specific range of lines
- Use offset and limit to read a specific byte range
- Set encoding to "binary" for non-text files (returns base64-encoded content)

The tool restricts file access to a configured root directory for security.
Path traversal attempts (e.g., "../../etc/passwd") are blocked.`
}

// Parameters returns the JSON Schema for the tool's input.
func (t FileReadTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Path to the file to read (relative to root or absolute if allowed)"
			},
			"encoding": {
				"type": "string",
				"description": "File encoding (utf-8, binary)",
				"default": "utf-8",
				"enum": ["utf-8", "binary"]
			},
			"offset": {
				"type": "integer",
				"description": "Byte offset to start reading from",
				"default": 0,
				"minimum": 0
			},
			"limit": {
				"type": "integer",
				"description": "Maximum bytes to read (0 for no limit)",
				"default": 0,
				"minimum": 0
			},
			"include_line_numbers": {
				"type": "boolean",
				"description": "Prefix each line with its line number",
				"default": false
			},
			"line_start": {
				"type": "integer",
				"description": "1-based line number to start reading from",
				"minimum": 1
			},
			"line_end": {
				"type": "integer",
				"description": "1-based line number to stop reading at (inclusive)",
				"minimum": 1
			}
		},
		"required": ["path"]
	}`)
}

// Execute reads the file and returns its contents.
func (t FileReadTool) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req FileReadInput
	if err := json.Unmarshal(input, &req); err != nil {
		return marshalFileReadError(fmt.Errorf("parse input: %w", err))
	}

	if req.Path == "" {
		return marshalFileReadError(fmt.Errorf("path is required"))
	}

	// Check context
	if ctx.Err() != nil {
		return marshalFileReadError(ctx.Err())
	}

	// Resolve the file path
	resolvedPath, err := t.resolvePath(req.Path)
	if err != nil {
		return marshalFileReadError(err)
	}

	// Check file exists and get info
	info, err := os.Stat(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return marshalFileReadError(fmt.Errorf("file not found: %s", resolvedPath))
		}
		return marshalFileReadError(fmt.Errorf("stat file: %w", err))
	}

	// Check it's a regular file
	if !info.Mode().IsRegular() {
		return marshalFileReadError(fmt.Errorf("not a regular file: %s", resolvedPath))
	}

	// Check file size
	maxSize := t.MaxFileSize
	if maxSize <= 0 {
		maxSize = 10 * 1024 * 1024
	}
	if info.Size() > maxSize {
		return marshalFileReadError(fmt.Errorf("file too large: %d bytes (max %d)", info.Size(), maxSize))
	}

	// Handle encoding
	if req.Encoding == "binary" {
		return t.readBinary(resolvedPath, info.Size(), req)
	}

	// Read as text
	return t.readText(resolvedPath, info.Size(), req)
}

// readBinary reads a file in binary mode and returns base64-encoded content.
func (t FileReadTool) readBinary(path string, size int64, req FileReadInput) (json.RawMessage, error) {
	file, err := os.Open(path)
	if err != nil {
		return marshalFileReadError(fmt.Errorf("open file: %w", err))
	}
	defer file.Close()

	// Apply offset
	if req.Offset > 0 {
		if _, err := file.Seek(req.Offset, io.SeekStart); err != nil {
			return marshalFileReadError(fmt.Errorf("seek: %w", err))
		}
	}

	// Apply limit
	readSize := size - req.Offset
	if req.Limit > 0 && req.Limit < readSize {
		readSize = req.Limit
	}

	data := make([]byte, readSize)
	n, err := file.Read(data)
	if err != nil {
		return marshalFileReadError(fmt.Errorf("read file: %w", err))
	}

	output := FileReadOutput{
		Content:    base64.StdEncoding.EncodeToString(data[:n]),
		Size:       size,
		Path:       path,
		BytesRead:  int64(n),
		Truncated:  int64(n) < readSize,
		Encoding:   "binary",
	}
	return json.Marshal(output)
}

// readText reads a file as UTF-8 text.
func (t FileReadTool) readText(path string, size int64, req FileReadInput) (json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return marshalFileReadError(fmt.Errorf("read file: %w", err))
	}

	content := string(data)

	// Handle line range if specified
	if req.LineStart > 0 && req.LineEnd > 0 && req.LineEnd >= req.LineStart {
		lines := strings.Split(content, "\n")
		if req.LineStart > len(lines) {
			return marshalFileReadError(fmt.Errorf("line_start %d exceeds line count %d", req.LineStart, len(lines)))
		}
		if req.LineEnd > len(lines) {
			req.LineEnd = len(lines)
		}
		selectedLines := lines[req.LineStart-1 : req.LineEnd]
		content = strings.Join(selectedLines, "\n")
	} else if req.Offset > 0 || req.Limit > 0 {
		// Handle byte range
		runes := []rune(content)
		startIdx := int(req.Offset)
		if startIdx > len(runes) {
			startIdx = len(runes)
		}
		endIdx := len(runes)
		if req.Limit > 0 && startIdx+int(req.Limit) < endIdx {
			endIdx = startIdx + int(req.Limit)
		}
		content = string(runes[startIdx:endIdx])
	}

	// Add line numbers if requested
	lineCount := strings.Count(content, "\n")
	if content != "" && !strings.HasSuffix(content, "\n") {
		lineCount++
	}

	if req.IncludeLineNumbers {
		content = addLineNumbers(content)
	}

	output := FileReadOutput{
		Content:   content,
		Size:      size,
		Path:      path,
		BytesRead: int64(len(content)),
		Encoding:  "utf-8",
		LineCount: lineCount,
	}

	// Check if content was truncated
	if req.Limit > 0 && int64(len(content)) >= req.Limit {
		output.Truncated = true
	}

	return json.Marshal(output)
}

// resolvePath resolves a user-provided path to an absolute path,
// applying root directory restrictions and path traversal protection.
func (t FileReadTool) resolvePath(userPath string) (string, error) {
	// Clean the user path
	userPath = filepath.Clean(userPath)

	// Handle absolute paths
	if filepath.IsAbs(userPath) {
		if !t.AllowAbsolute {
			return "", fmt.Errorf("absolute paths not allowed: %s", userPath)
		}
		return userPath, nil
	}

	// Resolve relative to root
	root := t.Root
	if root == "" {
		root = "."
	}
	root = filepath.Clean(root)

	// Join and clean to resolve any ".." components
	resolved := filepath.Clean(filepath.Join(root, userPath))

	// Verify the resolved path is within the root
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	// Check for path traversal
	if !strings.HasPrefix(absResolved, absRoot+string(filepath.Separator)) && absResolved != absRoot {
		return "", fmt.Errorf("path traversal detected: %s resolves outside root %s", userPath, absRoot)
	}

	return absResolved, nil
}

// addLineNumbers prefixes each line with its line number.
func addLineNumbers(content string) string {
	lines := strings.Split(content, "\n")
	width := len(fmt.Sprintf("%d", len(lines)))
	var result strings.Builder
	for i, line := range lines {
		if i > 0 {
			result.WriteString("\n")
		}
		fmt.Fprintf(&result, "%*d | %s", width, i+1, line)
	}
	return result.String()
}

// marshalFileReadError creates a JSON error response for file_read.
func marshalFileReadError(err error) (json.RawMessage, error) {
	output := FileReadOutput{
		Error: err.Error(),
	}
	return json.Marshal(output)
}

// ---------------------------------------------------------------------------
// File Write Tool
// ---------------------------------------------------------------------------

// FileWriteInput defines the input for the file_write tool.
type FileWriteInput struct {
	// Path is the file path to write. Relative paths are resolved against
	// the tool's root directory.
	Path string `json:"path" description:"Path to the file to write (relative to root or absolute if allowed)"`

	// Content is the content to write to the file.
	Content string `json:"content" description:"Content to write to the file"`

	// Mode controls how the file is opened: "write" (truncate), "append", or "create".
	// Defaults to "write".
	Mode string `json:"mode,omitempty" description:"Write mode: write (truncate), append, create (fail if exists)" default:"write" enum:"write,append,create"`

	// CreateDirs creates parent directories if they don't exist.
	CreateDirs bool `json:"create_dirs,omitempty" description:"Create parent directories if they don't exist" default:"false"`

	// Permissions sets the file permissions in octal notation (e.g., "0644").
	// Defaults to "0644" for new files.
	Permissions string `json:"permissions,omitempty" description:"File permissions in octal (e.g., 0644)" default:"0644"`
}

// FileWriteOutput defines the output of the file_write tool.
type FileWriteOutput struct {
	// Path is the resolved absolute path that was written.
	Path string `json:"path"`

	// BytesWritten is the number of bytes written.
	BytesWritten int64 `json:"bytes_written"`

	// Created indicates if a new file was created.
	Created bool `json:"created,omitempty"`

	// Appended indicates if content was appended to an existing file.
	Appended bool `json:"appended,omitempty"`

	// Error contains an error message if writing failed.
	Error string `json:"error,omitempty"`
}

// FileWriteTool implements the file_write built-in tool.
// It writes content to files with support for append mode,
// directory creation, and permission control.
//
// The tool can be configured with a root directory that restricts
// file access to a specific directory tree for security.
type FileWriteTool struct {
	// Root is the root directory for file access. If empty, the current
	// working directory is used.
	Root string

	// AllowAbsolute controls whether absolute paths outside the root
	// are allowed. When false (default), all paths are resolved
	// relative to the root directory.
	AllowAbsolute bool

	// AllowOverwrite controls whether existing files can be overwritten.
	// When false, writing to an existing file returns an error unless
	// mode is "append".
	AllowOverwrite bool

	// MaxFileSize is the maximum file size in bytes that can be written.
	// Defaults to 10MB.
	MaxFileSize int64
}

// NewFileWriteTool creates a file_write tool with default settings.
func NewFileWriteTool() FileWriteTool {
	return FileWriteTool{
		MaxFileSize: 10 * 1024 * 1024, // 10MB
	}
}

// NewFileWriteToolWithRoot creates a file_write tool restricted to a root directory.
func NewFileWriteToolWithRoot(root string) FileWriteTool {
	return FileWriteTool{
		Root:        root,
		MaxFileSize: 10 * 1024 * 1024,
	}
}

// Name returns the tool's identifier.
func (t FileWriteTool) Name() string { return "file_write" }

// Description returns the tool's description for the LLM.
func (t FileWriteTool) Description() string {
	return `Write content to a file.

Creates or modifies files on disk. Supports three modes:
- "write" (default): Truncate the file and write new content
- "append": Add content to the end of an existing file
- "create": Create a new file, fail if it already exists

Options:
- Set create_dirs to true to create parent directories automatically
- Set permissions to control file access (e.g., "0644", "0755")

The tool restricts file access to a configured root directory for security.
Path traversal attempts are blocked.`
}

// Parameters returns the JSON Schema for the tool's input.
func (t FileWriteTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Path to the file to write (relative to root or absolute if allowed)"
			},
			"content": {
				"type": "string",
				"description": "Content to write to the file"
			},
			"mode": {
				"type": "string",
				"description": "Write mode: write (truncate), append, create (fail if exists)",
				"default": "write",
				"enum": ["write", "append", "create"]
			},
			"create_dirs": {
				"type": "boolean",
				"description": "Create parent directories if they don't exist",
				"default": false
			},
			"permissions": {
				"type": "string",
				"description": "File permissions in octal (e.g., 0644)",
				"default": "0644"
			}
		},
		"required": ["path", "content"]
	}`)
}

// Execute writes content to the file.
func (t FileWriteTool) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req FileWriteInput
	if err := json.Unmarshal(input, &req); err != nil {
		return marshalFileWriteError(fmt.Errorf("parse input: %w", err))
	}

	if req.Path == "" {
		return marshalFileWriteError(fmt.Errorf("path is required"))
	}

	// Check context
	if ctx.Err() != nil {
		return marshalFileWriteError(ctx.Err())
	}

	// Apply defaults
	if req.Mode == "" {
		req.Mode = "write"
	}
	if req.Permissions == "" {
		req.Permissions = "0644"
	}

	// Parse permissions
	perm, err := parseFilePermissions(req.Permissions)
	if err != nil {
		return marshalFileWriteError(fmt.Errorf("invalid permissions: %w", err))
	}

	// Resolve the file path
	resolvedPath, err := t.resolvePath(req.Path)
	if err != nil {
		return marshalFileWriteError(err)
	}

	// Check if file exists
	_, statErr := os.Stat(resolvedPath)
	fileExists := statErr == nil

	// Handle mode-specific logic
	switch req.Mode {
	case "create":
		if fileExists {
			return marshalFileWriteError(fmt.Errorf("file already exists: %s (use mode 'write' to overwrite)", resolvedPath))
		}
	case "write":
		if fileExists && !t.AllowOverwrite {
			return marshalFileWriteError(fmt.Errorf("file exists and overwriting is disabled: %s", resolvedPath))
		}
	case "append":
		if !fileExists {
			// For append to non-existent file, fall through to create
		}
	default:
		return marshalFileWriteError(fmt.Errorf("invalid mode: %q (must be write, append, or create)", req.Mode))
	}

	// Check content size
	maxSize := t.MaxFileSize
	if maxSize <= 0 {
		maxSize = 10 * 1024 * 1024
	}
	if int64(len(req.Content)) > maxSize {
		return marshalFileWriteError(fmt.Errorf("content too large: %d bytes (max %d)", len(req.Content), maxSize))
	}

	// Create parent directories if requested
	if req.CreateDirs {
		dir := filepath.Dir(resolvedPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return marshalFileWriteError(fmt.Errorf("create directories: %w", err))
		}
	} else {
		// Ensure parent directory exists
		dir := filepath.Dir(resolvedPath)
		if _, err := os.Stat(dir); err != nil {
			if os.IsNotExist(err) {
				return marshalFileWriteError(fmt.Errorf("parent directory does not exist: %s (set create_dirs to true)", dir))
			}
			return marshalFileWriteError(fmt.Errorf("check parent directory: %w", err))
		}
	}

	// Determine open flags
	var flags int
	switch req.Mode {
	case "write":
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	case "append":
		flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	case "create":
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}

	// Open the file
	file, err := os.OpenFile(resolvedPath, flags, perm)
	if err != nil {
		return marshalFileWriteError(fmt.Errorf("open file: %w", err))
	}
	defer file.Close()

	// Write content
	n, err := file.WriteString(req.Content)
	if err != nil {
		return marshalFileWriteError(fmt.Errorf("write file: %w", err))
	}

	// Sync to disk for durability
	if err := file.Sync(); err != nil {
		return marshalFileWriteError(fmt.Errorf("sync file: %w", err))
	}

	output := FileWriteOutput{
		Path:         resolvedPath,
		BytesWritten: int64(n),
		Created:      !fileExists,
		Appended:     req.Mode == "append" && fileExists,
	}

	return json.Marshal(output)
}

// resolvePath resolves a user-provided path to an absolute path,
// applying root directory restrictions and path traversal protection.
func (t FileWriteTool) resolvePath(userPath string) (string, error) {
	// Clean the user path
	userPath = filepath.Clean(userPath)

	// Handle absolute paths
	if filepath.IsAbs(userPath) {
		if !t.AllowAbsolute {
			return "", fmt.Errorf("absolute paths not allowed: %s", userPath)
		}
		return userPath, nil
	}

	// Resolve relative to root
	root := t.Root
	if root == "" {
		root = "."
	}
	root = filepath.Clean(root)

	// Join and clean to resolve any ".." components
	resolved := filepath.Clean(filepath.Join(root, userPath))

	// Verify the resolved path is within the root
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	// Check for path traversal
	if !strings.HasPrefix(absResolved, absRoot+string(filepath.Separator)) && absResolved != absRoot {
		return "", fmt.Errorf("path traversal detected: %s resolves outside root %s", userPath, absRoot)
	}

	return absResolved, nil
}

// marshalFileWriteError creates a JSON error response for file_write.
func marshalFileWriteError(err error) (json.RawMessage, error) {
	output := FileWriteOutput{
		Error: err.Error(),
	}
	return json.Marshal(output)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// parseFilePermissions parses an octal permission string (e.g., "0644").
func parseFilePermissions(s string) (fs.FileMode, error) {
	var perm uint32
	_, err := fmt.Sscanf(s, "%o", &perm)
	if err != nil {
		return 0, fmt.Errorf("parse octal permissions %q: %w", s, err)
	}
	if perm > 0o777 {
		return 0, fmt.Errorf("invalid permissions %o: must be between 000 and 777", perm)
	}
	return fs.FileMode(perm), nil
}
