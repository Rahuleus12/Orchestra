// Package builtin provides built-in tools for the Orchestra tool system.
package builtin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// List Directory Tool
// ---------------------------------------------------------------------------

// ListDirectoryInput defines the input for the list_directory tool.
type ListDirectoryInput struct {
	// Path is the directory to list. Defaults to root.
	Path string `json:"path,omitempty" description:"Directory path to list (relative to root)"`

	// Pattern filters entries using glob patterns (e.g., "*.go", "test_*").
	// Multiple patterns can be comma-separated. Empty means all entries.
	Pattern string `json:"pattern,omitempty" description:"Glob pattern to filter entries (e.g., '*.go'), comma-separated for multiple"`

	// ExcludePattern excludes entries matching these glob patterns.
	ExcludePattern string `json:"exclude_pattern,omitempty" description:"Glob patterns to exclude (e.g., '*.test.go', 'vendor/*')"`

	// Recursive controls whether to list subdirectories recursively.
	// When false, only direct children are listed.
	Recursive bool `json:"recursive,omitempty" description:"List subdirectories recursively" default:"false"`

	// MaxDepth limits recursion depth when Recursive is true. 0 means unlimited.
	MaxDepth int `json:"max_depth,omitempty" description:"Maximum recursion depth (0 for unlimited, requires recursive=true)" min:"0"`

	// IncludeHidden includes hidden files and directories (starting with .).
	IncludeHidden bool `json:"include_hidden,omitempty" description:"Include hidden files and directories" default:"false"`

	// IncludeFiles includes regular files in the listing.
	IncludeFiles bool `json:"include_files,omitempty" description:"Include regular files in the listing" default:"true"`

	// IncludeDirs includes directories in the listing.
	IncludeDirs bool `json:"include_dirs,omitempty" description:"Include directories in the listing" default:"true"`

	// ShowSizes includes file sizes in the output.
	ShowSizes bool `json:"show_sizes,omitempty" description:"Include file sizes in the output" default:"false"`

	// ShowModTimes includes last modification times in the output.
	ShowModTimes bool `json:"show_mod_times,omitempty" description:"Include last modification times" default:"false"`

	// IgnoreFiles specifies additional ignore file names to check.
	// Defaults to [".gitignore", ".orchestaignore"].
	// Set to empty array to disable ignore file processing.
	IgnoreFiles []string `json:"ignore_files,omitempty" description:"Ignore file names to check (default: [.gitignore, .orchestaignore])"`

	// SortBy controls the sort order: "name" (default), "size", "time", or "none".
	SortBy string `json:"sort_by,omitempty" description:"Sort order: name, size, time, or none" default:"name" enum:"name,size,time,none"`

	// SortReverse reverses the sort order.
	SortReverse bool `json:"sort_reverse,omitempty" description:"Reverse the sort order" default:"false"`

	// MaxEntries limits the number of entries returned. 0 means unlimited.
	MaxEntries int `json:"max_entries,omitempty" description:"Maximum number of entries to return (0 for unlimited)" min:"0"`
}

// ListDirectoryOutput defines the output of the list_directory tool.
type ListDirectoryOutput struct {
	// Path is the resolved absolute path that was listed.
	Path string `json:"path"`

	// Entries contains the directory listing.
	Entries []DirEntry `json:"entries"`

	// TotalFiles is the total number of files found (may exceed len(Entries) if MaxEntries was set).
	TotalFiles int `json:"total_files"`

	// TotalDirs is the total number of directories found.
	TotalDirs int `json:"total_dirs"`

	// Truncated indicates if results were truncated due to MaxEntries.
	Truncated bool `json:"truncated,omitempty"`

	// Error contains an error message if the listing failed.
	Error string `json:"error,omitempty"`
}

// DirEntry represents a single entry in a directory listing.
type DirEntry struct {
	// Name is the entry name (not the full path).
	Name string `json:"name"`

	// Path is the relative path from the listed directory.
	Path string `json:"path"`

	// Type is the entry type: "file", "directory", or "symlink".
	Type string `json:"type"`

	// Size is the file size in bytes (only for files, 0 for directories).
	Size int64 `json:"size,omitempty"`

	// ModTime is the last modification time as an RFC3339 string.
	ModTime string `json:"mod_time,omitempty"`

	// IsSymlink indicates if this entry is a symbolic link.
	IsSymlink bool `json:"is_symlink,omitempty"`

	// SymlinkTarget is the target of a symbolic link.
	SymlinkTarget string `json:"symlink_target,omitempty"`
}

// ListDirectoryTool implements the list_directory built-in tool.
// It recursively lists files and directories with ignore-file support
// (.gitignore, .orchestaignore) and flexible filtering options.
//
// This is a coding-specific tool designed for exploring directory structures
// and finding files. It supports:
//   - Recursive and non-recursive listing
//   - Glob pattern filtering
//   - Ignore file support (.gitignore style)
//   - File metadata (size, modification time)
//   - Configurable sorting
//   - Entry limits for large directories
type ListDirectoryTool struct {
	// Root is the root directory for file access.
	Root string

	// AllowAbsolute controls whether absolute paths outside the root are allowed.
	AllowAbsolute bool

	// DefaultIgnoreFiles specifies the default ignore files to check.
	DefaultIgnoreFiles []string
}

// NewListDirectoryTool creates a list_directory tool with default settings.
func NewListDirectoryTool() ListDirectoryTool {
	return ListDirectoryTool{
		DefaultIgnoreFiles: []string{".gitignore", ".orchestaignore"},
	}
}

// NewListDirectoryToolWithRoot creates a list_directory tool restricted to a root directory.
func NewListDirectoryToolWithRoot(root string) ListDirectoryTool {
	return ListDirectoryTool{
		Root:                root,
		DefaultIgnoreFiles: []string{".gitignore", ".orchestaignore"},
	}
}

// Name returns the tool's identifier.
func (t ListDirectoryTool) Name() string { return "list_directory" }

// Description returns the tool's description for the LLM.
func (t ListDirectoryTool) Description() string {
	return `List files and directories in a directory tree.

Returns a structured listing of directory contents with support for:
- Recursive traversal of subdirectories
- Glob pattern filtering (e.g., "*.go", "test_*")
- Exclude patterns to skip entries
- Ignore file support (.gitignore, .orchestaignore)
- File metadata (size, modification time)
- Configurable sorting and limits

Common use cases:
- Explore a project structure: list_directory with recursive=true
- Find all Go files: list_directory with pattern="*.go", recursive=true
- List top-level contents: list_directory with recursive=false
- Check what's in a specific directory: list_directory with path="src"

The listing respects .gitignore and .orchestaignore files by default.
Hidden files (starting with .) are excluded unless include_hidden is set.

Tips:
- Use max_entries to limit output for large directories
- Use show_sizes/show_mod_times when you need file metadata
- Use exclude_pattern to skip common directories like "vendor", "node_modules"
- Set sort_by="size" to find largest files, "time" for recently modified`
}

// Parameters returns the JSON Schema for the tool's input.
func (t ListDirectoryTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Directory path to list (relative to root)"
			},
			"pattern": {
				"type": "string",
				"description": "Glob pattern to filter entries (e.g., '*.go'), comma-separated for multiple"
			},
			"exclude_pattern": {
				"type": "string",
				"description": "Glob patterns to exclude (e.g., '*.test.go', 'vendor/*')"
			},
			"recursive": {
				"type": "boolean",
				"description": "List subdirectories recursively",
				"default": false
			},
			"max_depth": {
				"type": "integer",
				"description": "Maximum recursion depth (0 for unlimited, requires recursive=true)",
				"minimum": 0
			},
			"include_hidden": {
				"type": "boolean",
				"description": "Include hidden files and directories",
				"default": false
			},
			"include_files": {
				"type": "boolean",
				"description": "Include regular files in the listing",
				"default": true
			},
			"include_dirs": {
				"type": "boolean",
				"description": "Include directories in the listing",
				"default": true
			},
			"show_sizes": {
				"type": "boolean",
				"description": "Include file sizes in the output",
				"default": false
			},
			"show_mod_times": {
				"type": "boolean",
				"description": "Include last modification times",
				"default": false
			},
			"ignore_files": {
				"type": "array",
				"description": "Ignore file names to check (default: [.gitignore, .orchestaignore])",
				"items": {"type": "string"}
			},
			"sort_by": {
				"type": "string",
				"description": "Sort order: name, size, time, or none",
				"default": "name",
				"enum": ["name", "size", "time", "none"]
			},
			"sort_reverse": {
				"type": "boolean",
				"description": "Reverse the sort order",
				"default": false
			},
			"max_entries": {
				"type": "integer",
				"description": "Maximum number of entries to return (0 for unlimited)",
				"minimum": 0
			}
		}
	}`)
}

// Execute lists the directory contents.
func (t ListDirectoryTool) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req ListDirectoryInput
	if err := json.Unmarshal(input, &req); err != nil {
		return marshalListDirError(fmt.Errorf("parse input: %w", err))
	}

	// Check context
	if ctx.Err() != nil {
		return marshalListDirError(ctx.Err())
	}

	// Apply defaults
	if !req.IncludeFiles && !req.IncludeDirs {
		req.IncludeFiles = true
		req.IncludeDirs = true
	}

	// Resolve the directory path
	dirPath, err := t.resolvePath(req.Path)
	if err != nil {
		return marshalListDirError(err)
	}

	// Check if path exists and is a directory
	info, err := os.Stat(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return marshalListDirError(fmt.Errorf("directory not found: %s", dirPath))
		}
		return marshalListDirError(fmt.Errorf("stat directory: %w", err))
	}
	if !info.IsDir() {
		return marshalListDirError(fmt.Errorf("not a directory: %s", dirPath))
	}

	// Parse patterns
	includePatterns, err := parseGlobPatterns(req.Pattern)
	if err != nil {
		return marshalListDirError(fmt.Errorf("parse pattern: %w", err))
	}
	excludePatterns, err := parseGlobPatterns(req.ExcludePattern)
	if err != nil {
		return marshalListDirError(fmt.Errorf("parse exclude_pattern: %w", err))
	}

	// Determine ignore files
	ignoreFiles := req.IgnoreFiles
	if len(ignoreFiles) == 0 {
		ignoreFiles = t.DefaultIgnoreFiles
	}

	// Load ignore rules
	var ignoreRules *ignoreRules
	if len(ignoreFiles) > 0 {
		ignoreRules, _ = loadIgnoreRules(dirPath, ignoreFiles)
	}

	// Collect entries
	var entries []DirEntry
	var totalFiles, totalDirs int

	err = filepath.WalkDir(dirPath, func(path string, d os.DirEntry, err error) error {
		// Check context
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err != nil {
			// Skip errors (e.g., permission denied)
			return nil
		}

		// Get relative path from the listed directory
		relPath, err := filepath.Rel(dirPath, path)
		if err != nil {
			return nil
		}

		// Handle the root directory itself
		if relPath == "." {
			return nil
		}

		// Calculate depth
		depth := strings.Count(relPath, string(filepath.Separator))

		// Check max depth for recursion
		if req.Recursive && req.MaxDepth > 0 && depth >= req.MaxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// For non-recursive mode, skip subdirectories' contents
		if !req.Recursive && depth > 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Get entry info
		entryInfo, err := d.Info()
		if err != nil {
			return nil
		}

		isDir := d.IsDir()
		isSymlink := entryInfo.Mode()&os.ModeSymlink != 0

		// Check if we should include this entry type
		if isDir {
			if !req.IncludeDirs {
				return nil
			}
		} else {
			if !req.IncludeFiles {
				return nil
			}
		}

		// Skip hidden files unless requested
		if !req.IncludeHidden {
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") {
				if isDir {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Check ignore rules
		if ignoreRules != nil && ignoreRules.IsMatch(relPath) {
			if isDir {
				return filepath.SkipDir
			}
			return nil
		}

		// Check include patterns
		if len(includePatterns) > 0 {
			if !matchesAnyPattern(relPath, includePatterns) && !matchesAnyPattern(filepath.Base(path), includePatterns) {
				if isDir {
					// For directories, still recurse if a child might match
					// But don't include the directory itself in the listing
					return nil
				}
				return nil
			}
		}

		// Check exclude patterns
		if matchesAnyPattern(relPath, excludePatterns) || matchesAnyPattern(filepath.Base(path), excludePatterns) {
			if isDir {
				return filepath.SkipDir
			}
			return nil
		}

		// Build the entry
		entry := DirEntry{
			Name:      filepath.Base(path),
			Path:      relPath,
			IsSymlink: isSymlink,
		}

		if isDir {
			entry.Type = "directory"
			totalDirs++
		} else {
			entry.Type = "file"
			totalFiles++
		}

		// Add size if requested
		if req.ShowSizes && !isDir {
			entry.Size = entryInfo.Size()
		}

		// Add modification time if requested
		if req.ShowModTimes {
			entry.ModTime = entryInfo.ModTime().Format("2006-01-02T15:04:05Z07:00")
		}

		// Resolve symlink target if it's a symlink
		if isSymlink {
			if target, err := os.Readlink(path); err == nil {
				entry.SymlinkTarget = target
			}
		}

		entries = append(entries, entry)

		// Check max entries
		if req.MaxEntries > 0 && len(entries) >= req.MaxEntries {
			return fmt.Errorf("max entries reached")
		}

		return nil
	})

	// Handle max entries truncation
	truncated := false
	if err != nil && err.Error() == "max entries reached" {
		truncated = true
		err = nil
	}
	if err != nil {
		return marshalListDirError(fmt.Errorf("walk directory: %w", err))
	}

	// Sort entries
	entries = t.sortEntries(entries, req.SortBy, req.SortReverse)

	output := ListDirectoryOutput{
		Path:        dirPath,
		Entries:     entries,
		TotalFiles:  totalFiles,
		TotalDirs:   totalDirs,
		Truncated:   truncated,
	}

	return json.Marshal(output)
}

// resolvePath resolves a user-provided path to an absolute path.
func (t ListDirectoryTool) resolvePath(userPath string) (string, error) {
	root := t.Root
	if root == "" {
		root = "."
	}
	root = filepath.Clean(root)

	if userPath == "" {
		userPath = "."
	}
	userPath = filepath.Clean(userPath)

	// Handle absolute paths
	if filepath.IsAbs(userPath) {
		if !t.AllowAbsolute {
			return "", fmt.Errorf("absolute paths not allowed: %s", userPath)
		}
		return userPath, nil
	}

	// Resolve relative to root
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

// sortEntries sorts the entries according to the specified sort order.
func (t ListDirectoryTool) sortEntries(entries []DirEntry, sortBy string, reverse bool) []DirEntry {
	if sortBy == "none" || len(entries) == 0 {
		return entries
	}

	sort.SliceStable(entries, func(i, j int) bool {
		var less bool

		switch sortBy {
		case "name":
			less = entries[i].Name < entries[j].Name
			// Secondary sort: directories before files
			if entries[i].Name == entries[j].Name {
				less = entries[i].Type == "directory" && entries[j].Type != "directory"
			}
		case "size":
			less = entries[i].Size < entries[j].Size
		case "time":
			less = entries[i].ModTime < entries[j].ModTime
		default:
			less = entries[i].Name < entries[j].Name
		}

		if reverse {
			return !less
		}
		return less
	})

	return entries
}

// loadIgnoreRules loads ignore rules from the specified files in the directory.
func loadIgnoreRules(dir string, filenames []string) (*ignoreRules, error) {
	rules := &ignoreRules{}

	for _, filename := range filenames {
		path := filepath.Join(dir, filename)
		file, err := os.Open(path)
		if err != nil {
			continue
		}

		scanner := lineScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())

			// Skip empty lines and comments
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			pat := ignorePattern{}

			// Check for negation
			if strings.HasPrefix(line, "!") {
				pat.negate = true
				line = line[1:]
			}

			// Check for directory-only pattern
			if strings.HasSuffix(line, "/") {
				pat.dirOnly = true
				line = strings.TrimSuffix(line, "/")
			}

			pat.pattern = line
			rules.patterns = append(rules.patterns, pat)
		}
		file.Close()
	}

	return rules, nil
}

// lineScanner creates a buffered scanner for reading lines.
func lineScanner(file *os.File) *bufio.Scanner {
	return bufio.NewScanner(file)
}

// marshalListDirError creates a JSON error response for list_directory.
func marshalListDirError(err error) (json.RawMessage, error) {
	output := ListDirectoryOutput{
		Error: err.Error(),
	}
	return json.Marshal(output)
}
