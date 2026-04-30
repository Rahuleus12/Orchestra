// Package builtin provides built-in tools for the Orchestra tool system.
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// File Edit Tool
// ---------------------------------------------------------------------------

// EditOperation represents a single edit to apply to a file.
// Only one of the edit types should be set.
type EditOperation struct {
	// SearchReplace performs an exact text search and replace.
	// The old_text is searched for in the file and replaced with new_text.
	SearchReplace *SearchReplaceEdit `json:"search_replace,omitempty"`

	// LineRange replaces a range of lines (1-based, inclusive).
	LineRange *LineRangeEdit `json:"line_range,omitempty"`
}

// SearchReplaceEdit finds exact text and replaces it.
type SearchReplaceEdit struct {
	// OldText is the exact text to find in the file.
	OldText string `json:"old_text" description:"Exact text to find in the file"`

	// NewText is the replacement text.
	NewText string `json:"new_text" description:"Text to replace the old text with"`

	// RequireUnique ensures old_text appears exactly once in the file.
	// If false and there are multiple matches, only the first is replaced.
	RequireUnique bool `json:"require_unique,omitempty" description:"Fail if old_text appears more than once" default:"true"`
}

// LineRangeEdit replaces a range of lines.
type LineRangeEdit struct {
	// StartLine is the 1-based line number where replacement starts (inclusive).
	StartLine int `json:"start_line" description:"1-based line number where replacement starts (inclusive)" min:"1"`

	// EndLine is the 1-based line number where replacement ends (inclusive).
	// If 0, replaces from start_line to end of file.
	EndLine int `json:"end_line,omitempty" description:"1-based line number where replacement ends (inclusive, 0 for end of file)" min:"0"`

	// NewText is the text to insert in place of the specified line range.
	// If empty, the lines are deleted.
	NewText string `json:"new_text" description:"Text to replace the line range with (empty to delete lines)"`
}

// FileEditInput defines the input for the file_edit tool.
type FileEditInput struct {
	// Path is the file path to edit.
	Path string `json:"path" description:"Path to the file to edit"`

	// Edits is the list of edit operations to apply.
	// Edits are applied sequentially; each edit operates on the result
	// of the previous edit.
	Edits []EditOperation `json:"edits" description:"List of edit operations to apply sequentially"`

	// DryRun previews the changes without modifying the file.
	DryRun bool `json:"dry_run,omitempty" description:"Preview changes without modifying the file" default:"false"`

	// CreateBackup creates a .bak copy of the file before editing.
	CreateBackup bool `json:"create_backup,omitempty" description:"Create a .bak backup before editing" default:"false"`
}

// FileEditOutput defines the output of the file_edit tool.
type FileEditOutput struct {
	// Path is the resolved absolute path that was edited.
	Path string `json:"path"`

	// EditsApplied is the number of edits that were successfully applied.
	EditsApplied int `json:"edits_applied"`

	// EditsFailed is the number of edits that failed.
	EditsFailed int `json:"edits_failed"`

	// Changes contains details about each edit that was applied or failed.
	Changes []EditChange `json:"changes,omitempty"`

	// OriginalContent is the file content before edits (only in dry_run mode).
	OriginalContent string `json:"original_content,omitempty"`

	// NewContent is the file content after edits (only in dry_run mode).
	NewContent string `json:"new_content,omitempty"`

	// Error contains an error message if the overall operation failed.
	Error string `json:"error,omitempty"`
}

// EditChange describes the result of a single edit operation.
type EditChange struct {
	// EditIndex is the 0-based index of the edit in the input array.
	EditIndex int `json:"edit_index"`

	// Type indicates the type of edit: "search_replace" or "line_range".
	Type string `json:"type"`

	// Success indicates whether the edit was applied.
	Success bool `json:"success"`

	// OldText shows the text that was replaced (for search_replace edits).
	OldText string `json:"old_text,omitempty"`

	// NewText shows the replacement text.
	NewText string `json:"new_text,omitempty"`

	// LineRange shows the affected line range (for line_range edits).
	LineRange *LineRangeInfo `json:"line_range,omitempty"`

	// Error describes why the edit failed, if applicable.
	Error string `json:"error,omitempty"`
}

// LineRangeInfo describes a line range that was affected by an edit.
type LineRangeInfo struct {
	StartLine    int `json:"start_line"`
	EndLine      int `json:"end_line"`
	LinesRemoved int `json:"lines_removed"`
	LinesAdded   int `json:"lines_added"`
}

// FileEditTool implements the file_edit built-in tool.
// It applies targeted edits to files using search-and-replace blocks
// or line-range replacements.
//
// This is a coding-specific tool designed for making precise, surgical
// changes to source files without rewriting entire files.
type FileEditTool struct {
	// Root is the root directory for file access.
	Root string

	// AllowAbsolute controls whether absolute paths outside the root are allowed.
	AllowAbsolute bool

	// MaxFileSize is the maximum file size in bytes that can be edited.
	MaxFileSize int64
}

// NewFileEditTool creates a file_edit tool with default settings.
func NewFileEditTool() FileEditTool {
	return FileEditTool{
		MaxFileSize: 10 * 1024 * 1024,
	}
}

// NewFileEditToolWithRoot creates a file_edit tool restricted to a root directory.
func NewFileEditToolWithRoot(root string) FileEditTool {
	return FileEditTool{
		Root:        root,
		MaxFileSize: 10 * 1024 * 1024,
	}
}

// Name returns the tool's identifier.
func (t FileEditTool) Name() string { return "file_edit" }

// Description returns the tool's description for the LLM.
func (t FileEditTool) Description() string {
	return `Apply targeted edits to a file.

This tool makes precise, surgical changes to files without requiring you to
rewrite the entire file. It supports two edit modes:

1. Search-and-Replace: Find exact text and replace it with new text.
   - The old_text must match exactly (including whitespace and newlines)
   - By default, old_text must be unique in the file to prevent accidental changes
   - Use this for replacing specific functions, blocks, or lines

2. Line Range: Replace a range of lines by line number.
   - Line numbers are 1-based and inclusive
   - Use end_line=0 to replace from start_line to end of file
   - Use empty new_text to delete the specified lines

Multiple edits can be applied in a single call. Edits are applied sequentially,
so each edit operates on the result of the previous edit.

Tips for reliable edits:
- Include enough context in old_text to ensure a unique match
- Preserve exact whitespace and indentation
- Use line-range edits when you know the exact line numbers
- Set dry_run=true to preview changes before applying them

The tool restricts file access to a configured root directory for security.`
}

// Parameters returns the JSON Schema for the tool's input.
func (t FileEditTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Path to the file to edit"
			},
			"edits": {
				"type": "array",
				"description": "List of edit operations to apply sequentially",
				"items": {
					"type": "object",
					"properties": {
						"search_replace": {
							"type": "object",
							"description": "Find exact text and replace it",
							"properties": {
								"old_text": {
									"type": "string",
									"description": "Exact text to find in the file"
								},
								"new_text": {
									"type": "string",
									"description": "Text to replace the old text with"
								},
								"require_unique": {
									"type": "boolean",
									"description": "Fail if old_text appears more than once",
									"default": true
								}
							},
							"required": ["old_text", "new_text"]
						},
						"line_range": {
							"type": "object",
							"description": "Replace a range of lines",
							"properties": {
								"start_line": {
									"type": "integer",
									"description": "1-based line number where replacement starts (inclusive)",
									"minimum": 1
								},
								"end_line": {
									"type": "integer",
									"description": "1-based line number where replacement ends (inclusive, 0 for end of file)",
									"minimum": 0
								},
								"new_text": {
									"type": "string",
									"description": "Text to replace the line range with (empty to delete lines)"
								}
							},
							"required": ["start_line"]
						}
					}
				},
				"minItems": 1
			},
			"dry_run": {
				"type": "boolean",
				"description": "Preview changes without modifying the file",
				"default": false
			},
			"create_backup": {
				"type": "boolean",
				"description": "Create a .bak backup before editing",
				"default": false
			}
		},
		"required": ["path", "edits"]
	}`)
}

// Execute applies the edits to the file.
func (t FileEditTool) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req FileEditInput
	if err := json.Unmarshal(input, &req); err != nil {
		return marshalFileEditError(fmt.Errorf("parse input: %w", err))
	}

	if req.Path == "" {
		return marshalFileEditError(fmt.Errorf("path is required"))
	}
	if len(req.Edits) == 0 {
		return marshalFileEditError(fmt.Errorf("at least one edit is required"))
	}

	// Check context
	if ctx.Err() != nil {
		return marshalFileEditError(ctx.Err())
	}

	// Resolve the file path
	resolvedPath, err := t.resolvePath(req.Path)
	if err != nil {
		return marshalFileEditError(err)
	}

	// Read the file
	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return marshalFileEditError(fmt.Errorf("file not found: %s", resolvedPath))
		}
		return marshalFileEditError(fmt.Errorf("read file: %w", err))
	}

	// Check file size
	maxSize := t.MaxFileSize
	if maxSize <= 0 {
		maxSize = 10 * 1024 * 1024
	}
	if int64(len(content)) > maxSize {
		return marshalFileEditError(fmt.Errorf("file too large: %d bytes (max %d)", len(content), maxSize))
	}

	originalContent := string(content)
	currentContent := originalContent

	// Apply each edit sequentially
	output := FileEditOutput{
		Path:    resolvedPath,
		Changes: make([]EditChange, 0, len(req.Edits)),
	}

	for i, edit := range req.Edits {
		change := EditChange{EditIndex: i}

		if edit.SearchReplace != nil {
			change.Type = "search_replace"
			currentContent, change = t.applySearchReplace(currentContent, edit.SearchReplace, i)
		} else if edit.LineRange != nil {
			change.Type = "line_range"
			currentContent, change = t.applyLineRange(currentContent, edit.LineRange, i)
		} else {
			change.Success = false
			change.Error = "no edit type specified (set search_replace or line_range)"
			output.EditsFailed++
		}

		output.Changes = append(output.Changes, change)

		// Stop on first failure if not in dry_run mode
		if !change.Success && !req.DryRun {
			break
		}
	}

	output.EditsApplied = output.EditsFailed // Will be corrected below
	output.EditsFailed = 0
	for _, c := range output.Changes {
		if c.Success {
			output.EditsApplied++
		} else {
			output.EditsFailed++
		}
	}

	// Handle dry run mode
	if req.DryRun {
		output.OriginalContent = originalContent
		output.NewContent = currentContent
		return json.Marshal(output)
	}

	// If any edits failed, don't write the file
	if output.EditsFailed > 0 {
		output.Error = fmt.Sprintf("%d edit(s) failed; file not modified", output.EditsFailed)
		return json.Marshal(output)
	}

	// Create backup if requested
	if req.CreateBackup {
		backupPath := resolvedPath + ".bak"
		if err := os.WriteFile(backupPath, []byte(originalContent), 0644); err != nil {
			return marshalFileEditError(fmt.Errorf("create backup: %w", err))
		}
	}

	// Write the modified content
	if err := os.WriteFile(resolvedPath, []byte(currentContent), 0644); err != nil {
		return marshalFileEditError(fmt.Errorf("write file: %w", err))
	}

	return json.Marshal(output)
}

// applySearchReplace applies a search-and-replace edit.
func (t FileEditTool) applySearchReplace(content string, edit *SearchReplaceEdit, index int) (string, EditChange) {
	change := EditChange{
		EditIndex: index,
		Type:      "search_replace",
		OldText:   edit.OldText,
		NewText:   edit.NewText,
	}

	if edit.OldText == "" {
		change.Success = false
		change.Error = "old_text must not be empty"
		return content, change
	}

	// Count occurrences
	count := strings.Count(content, edit.OldText)
	if count == 0 {
		change.Success = false
		change.Error = "old_text not found in file"
		// Try to provide a helpful hint about partial matches
		hint := findPartialMatch(content, edit.OldText)
		if hint != "" {
			change.Error += "; " + hint
		}
		return content, change
	}

	// Check uniqueness if required
	requireUnique := edit.RequireUnique
	if count > 1 && requireUnique {
		change.Success = false
		change.Error = fmt.Sprintf("old_text found %d times in file; make old_text more specific or set require_unique=false", count)
		return content, change
	}

	// Apply the replacement (only first occurrence if multiple)
	if count > 1 {
		change.Error = fmt.Sprintf("warning: old_text found %d times, only first occurrence replaced", count)
	}
	newContent := strings.Replace(content, edit.OldText, edit.NewText, 1)

	change.Success = true
	return newContent, change
}

// applyLineRange applies a line-range edit.
func (t FileEditTool) applyLineRange(content string, edit *LineRangeEdit, index int) (string, EditChange) {
	change := EditChange{
		EditIndex: index,
		Type:      "line_range",
	}

	if edit.StartLine < 1 {
		change.Success = false
		change.Error = "start_line must be at least 1"
		return content, change
	}

	lines := strings.Split(content, "\n")
	lineCount := len(lines)

	// Adjust for files not ending with newline
	// A file "a\nb\nc" splits into ["a", "b", "c"]
	// A file "a\nb\nc\n" splits into ["a", "b", "c", ""]
	// We treat the trailing empty string as not a real line
	if lineCount > 0 && lines[lineCount-1] == "" {
		lineCount--
	}

	if edit.StartLine > lineCount {
		change.Success = false
		change.Error = fmt.Sprintf("start_line %d exceeds file line count %d", edit.StartLine, lineCount)
		return content, change
	}

	// Determine end line
	endLine := edit.EndLine
	if endLine <= 0 || endLine > lineCount {
		endLine = lineCount
	}

	if endLine < edit.StartLine {
		change.Success = false
		change.Error = fmt.Sprintf("end_line %d is less than start_line %d", endLine, edit.StartLine)
		return content, change
	}

	// Calculate lines being removed
	linesRemoved := endLine - edit.StartLine + 1
	newLines := strings.Split(edit.NewText, "\n")
	linesAdded := len(newLines)

	// Handle trailing newline in new_text
	// If new_text ends with \n, the last element is empty and shouldn't count
	if linesAdded > 0 && newLines[len(newLines)-1] == "" {
		// Keep the empty string to preserve the trailing newline
	}

	// Build the new content
	// Lines are 1-based, array is 0-based
	before := lines[:edit.StartLine-1]
	after := lines[endLine:]

	var newContent string

	newContent = strings.Join(before, "\n")
	if len(before) > 0 {
		newContent += "\n"
	}
	newContent += edit.NewText
	if len(after) > 0 {
		if !strings.HasSuffix(newContent, "\n") {
			newContent += "\n"
		}
		newContent += strings.Join(after, "\n")
	}

	change.Success = true
	change.LineRange = &LineRangeInfo{
		StartLine:    edit.StartLine,
		EndLine:      endLine,
		LinesRemoved: linesRemoved,
		LinesAdded:   linesAdded,
	}
	change.NewText = edit.NewText

	return newContent, change
}

// findPartialMatch tries to find a partial match for the search text
// and returns a hint about what might be wrong.
func findPartialMatch(content, search string) string {
	if len(search) < 10 {
		return ""
	}

	// Try to find a substring of the search in the content
	// Use progressively shorter prefixes/suffixes
	for subLen := len(search) - 1; subLen >= 20; subLen-- {
		prefix := search[:subLen]
		if idx := strings.Index(content, prefix); idx >= 0 {
			// Found a partial match - show context
			end := idx + subLen
			if end > len(content) {
				end = len(content)
			}
			contextLines := strings.Split(content[idx:end], "\n")
			if len(contextLines) > 3 {
				contextLines = contextLines[:3]
			}
			return fmt.Sprintf("partial match found at line ~%d: %q", strings.Count(content[:idx], "\n")+1, strings.Join(contextLines, "\n"))
		}
	}

	return ""
}

// resolvePath resolves a user-provided path to an absolute path,
// applying root directory restrictions and path traversal protection.
func (t FileEditTool) resolvePath(userPath string) (string, error) {
	userPath = filepath.Clean(userPath)

	if filepath.IsAbs(userPath) {
		if !t.AllowAbsolute {
			return "", fmt.Errorf("absolute paths not allowed: %s", userPath)
		}
		return userPath, nil
	}

	root := t.Root
	if root == "" {
		root = "."
	}
	root = filepath.Clean(root)

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

// marshalFileEditError creates a JSON error response for file_edit.
func marshalFileEditError(err error) (json.RawMessage, error) {
	output := FileEditOutput{
		Error: err.Error(),
	}
	return json.Marshal(output)
}
