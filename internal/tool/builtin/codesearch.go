// Package builtin provides built-in tools for the Orchestra tool system.
package builtin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Code Search Tool
// ---------------------------------------------------------------------------

// CodeSearchInput defines the input for the code_search tool.
type CodeSearchInput struct {
	// Query is the search pattern. Can be plain text or a regex.
	Query string `json:"query" description:"Search pattern (plain text or regex depending on mode)"`

	// Path is the directory or file to search in. Defaults to root.
	Path string `json:"path,omitempty" description:"Directory or file to search in (relative to root)"`

	// Pattern filters files to search using glob patterns (e.g., "*.go", "*.ts").
	// Multiple patterns can be comma-separated. Empty means all files.
	Pattern string `json:"pattern,omitempty" description:"File glob pattern to filter (e.g., '*.go', '*.py'), comma-separated for multiple"`

	// ExcludePattern excludes files matching these glob patterns.
	ExcludePattern string `json:"exclude_pattern,omitempty" description:"File glob patterns to exclude (e.g., '*_test.go', 'vendor/*')"`

	// Mode controls how the query is interpreted.
	// "text" - plain text search (case-insensitive)
	// "text_case" - plain text search (case-sensitive)
	// "regex" - regular expression search
	// "literal" - literal string search (case-sensitive, no regex interpretation)
	Mode string `json:"mode,omitempty" description:"Search mode: text (case-insensitive), text_case (case-sensitive), regex, literal" default:"text" enum:"text,text_case,regex,literal"`

	// ContextLines is the number of lines to show before and after each match.
	ContextLines int `json:"context_lines,omitempty" description:"Number of context lines before and after each match" default:"2" min:"0" max:"20"`

	// MaxResults is the maximum number of matches to return. 0 means unlimited.
	MaxResults int `json:"max_results,omitempty" description:"Maximum number of matches to return (0 for unlimited)" default:"100" min:"0"`

	// IncludeHidden includes hidden files and directories (starting with .).
	IncludeHidden bool `json:"include_hidden,omitempty" description:"Include hidden files and directories" default:"false"`

	// MaxFileSize is the maximum file size in bytes to search. Files larger
	// than this are skipped.
	MaxFileSize int64 `json:"max_file_size,omitempty" description:"Maximum file size in bytes to search" default:"1048576" min:"1024"`

	// WholeWord requires the match to be a whole word (surrounded by word boundaries).
	WholeWord bool `json:"whole_word,omitempty" description:"Match whole words only" default:"false"`

	// MaxDepth limits directory recursion depth. 0 means unlimited.
	MaxDepth int `json:"max_depth,omitempty" description:"Maximum directory recursion depth (0 for unlimited)" min:"0"`
}

// CodeSearchOutput defines the output of the code_search tool.
type CodeSearchOutput struct {
	// Matches contains all search results.
	Matches []CodeMatch `json:"matches"`

	// TotalMatches is the total number of matches found.
	// May be greater than len(Matches) if MaxResults was set.
	TotalMatches int `json:"total_matches"`

	// FilesSearched is the number of files that were searched.
	FilesSearched int `json:"files_searched"`

	// FilesSkipped is the number of files that were skipped (e.g., too large, binary).
	FilesSkipped int `json:"files_skipped"`

	// Truncated indicates if results were truncated due to MaxResults.
	Truncated bool `json:"truncated,omitempty"`

	// Query shows the normalized query that was used.
	Query string `json:"query"`

	// Error contains an error message if the search failed.
	Error string `json:"error,omitempty"`
}

// CodeMatch represents a single search match within a file.
type CodeMatch struct {
	// File is the path to the file containing the match.
	File string `json:"file"`

	// Line is the 1-based line number where the match starts.
	Line int `json:"line"`

	// Column is the 1-based column number where the match starts.
	Column int `json:"column,omitempty"`

	// LineContent is the full content of the matched line.
	LineContent string `json:"line_content"`

	// MatchText is the text that matched the query.
	MatchText string `json:"match_text"`

	// ContextBefore contains lines before the match (for context).
	ContextBefore []string `json:"context_before,omitempty"`

	// ContextAfter contains lines after the match (for context).
	ContextAfter []string `json:"context_after,omitempty"`
}

// CodeSearchTool implements the code_search built-in tool.
// It searches codebases by text or regex, returning matching file paths,
// line numbers, and context.
//
// This is a coding-specific tool designed for exploring and understanding
// codebases. It supports:
//   - Plain text search (case-sensitive or insensitive)
//   - Regular expression search
//   - File pattern filtering (e.g., only search *.go files)
//   - Exclusion patterns (e.g., skip vendor/, *_test.go)
//   - Context lines around matches
//   - Whole word matching
//   - Parallel file searching for performance
type CodeSearchTool struct {
	// Root is the root directory for file access.
	Root string

	// AllowAbsolute controls whether absolute paths outside the root are allowed.
	AllowAbsolute bool

	// IgnoreFiles specifies additional ignore file names to check
	// (in addition to .gitignore). Default is [".gitignore", ".orchestaignore"].
	IgnoreFiles []string

	// Concurrency controls how many files are searched in parallel.
	// Defaults to 4.
	Concurrency int
}

// NewCodeSearchTool creates a code_search tool with default settings.
func NewCodeSearchTool() CodeSearchTool {
	return CodeSearchTool{
		IgnoreFiles: []string{".gitignore", ".orchestaignore"},
		Concurrency: 4,
	}
}

// NewCodeSearchToolWithRoot creates a code_search tool restricted to a root directory.
func NewCodeSearchToolWithRoot(root string) CodeSearchTool {
	return CodeSearchTool{
		Root:         root,
		IgnoreFiles:  []string{".gitignore", ".orchestaignore"},
		Concurrency:  4,
	}
}

// Name returns the tool's identifier.
func (t CodeSearchTool) Name() string { return "code_search" }

// Description returns the tool's description for the LLM.
func (t CodeSearchTool) Description() string {
	return `Search a codebase for text patterns or regular expressions.

Returns matching file paths, line numbers, matched text, and optional context
lines. This is the primary tool for exploring and understanding codebases.

Search modes:
- "text" (default): Case-insensitive text search
- "text_case": Case-sensitive text search
- "regex": Regular expression search (RE2 syntax)
- "literal": Literal string match (case-sensitive, no regex)

Filtering options:
- Use "pattern" to limit search to specific file types (e.g., "*.go", "*.py")
- Use "exclude_pattern" to skip files/directories (e.g., "vendor/*", "*_test.go")
- Set "whole_word" to true to match complete words only

Performance tips:
- Use specific patterns to reduce files searched
- Set max_results to limit output size
- Use context_lines=0 if you only need line numbers
- Narrow the search path when possible

The search respects .gitignore and .orchestaignore files by default.`
}

// Parameters returns the JSON Schema for the tool's input.
func (t CodeSearchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "Search pattern (plain text or regex depending on mode)"
			},
			"path": {
				"type": "string",
				"description": "Directory or file to search in (relative to root)"
			},
			"pattern": {
				"type": "string",
				"description": "File glob pattern to filter (e.g., '*.go', '*.py'), comma-separated for multiple"
			},
			"exclude_pattern": {
				"type": "string",
				"description": "File glob patterns to exclude (e.g., '*_test.go', 'vendor/*')"
			},
			"mode": {
				"type": "string",
				"description": "Search mode: text (case-insensitive), text_case (case-sensitive), regex, literal",
				"default": "text",
				"enum": ["text", "text_case", "regex", "literal"]
			},
			"context_lines": {
				"type": "integer",
				"description": "Number of context lines before and after each match",
				"default": 2,
				"minimum": 0,
				"maximum": 20
			},
			"max_results": {
				"type": "integer",
				"description": "Maximum number of matches to return (0 for unlimited)",
				"default": 100,
				"minimum": 0
			},
			"include_hidden": {
				"type": "boolean",
				"description": "Include hidden files and directories",
				"default": false
			},
			"max_file_size": {
				"type": "integer",
				"description": "Maximum file size in bytes to search",
				"default": 1048576,
				"minimum": 1024
			},
			"whole_word": {
				"type": "boolean",
				"description": "Match whole words only",
				"default": false
			},
			"max_depth": {
				"type": "integer",
				"description": "Maximum directory recursion depth (0 for unlimited)",
				"minimum": 0
			}
		},
		"required": ["query"]
	}`)
}

// Execute searches the codebase for matches.
func (t CodeSearchTool) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req CodeSearchInput
	if err := json.Unmarshal(input, &req); err != nil {
		return marshalCodeSearchError(fmt.Errorf("parse input: %w", err))
	}

	if req.Query == "" {
		return marshalCodeSearchError(fmt.Errorf("query is required"))
	}

	// Check context
	if ctx.Err() != nil {
		return marshalCodeSearchError(ctx.Err())
	}

	// Apply defaults
	if req.Mode == "" {
		req.Mode = "text"
	}
	if req.MaxFileSize <= 0 {
		req.MaxFileSize = 1048576 // 1MB
	}
	if req.MaxResults < 0 {
		req.MaxResults = 0
	}
	if req.ContextLines < 0 {
		req.ContextLines = 0
	}
	if req.ContextLines > 20 {
		req.ContextLines = 20
	}
	concurrency := t.Concurrency
	if concurrency <= 0 {
		concurrency = 4
	}

	// Build the matcher
	matcher, err := t.buildMatcher(req)
	if err != nil {
		return marshalCodeSearchError(err)
	}

	// Parse file patterns
	includePatterns, err := parseGlobPatterns(req.Pattern)
	if err != nil {
		return marshalCodeSearchError(fmt.Errorf("parse pattern: %w", err))
	}
	excludePatterns, err := parseGlobPatterns(req.ExcludePattern)
	if err != nil {
		return marshalCodeSearchError(fmt.Errorf("parse exclude_pattern: %w", err))
	}

	// Resolve search path
	searchPath := t.Root
	if searchPath == "" {
		searchPath = "."
	}
	if req.Path != "" {
		searchPath = filepath.Join(searchPath, req.Path)
	}
	searchPath = filepath.Clean(searchPath)

	// Check if search path exists
	info, err := os.Stat(searchPath)
	if err != nil {
		if os.IsNotExist(err) {
			return marshalCodeSearchError(fmt.Errorf("path not found: %s", searchPath))
		}
		return marshalCodeSearchError(fmt.Errorf("stat path: %w", err))
	}

	// Collect files to search
	var files []string
	if info.IsDir() {
		files, err = t.collectFiles(ctx, searchPath, includePatterns, excludePatterns, req)
		if err != nil {
			return marshalCodeSearchError(fmt.Errorf("collect files: %w", err))
		}
	} else {
		files = []string{searchPath}
	}

	if len(files) == 0 {
		output := CodeSearchOutput{
			Query:        req.Query,
			FilesSearched: 0,
		}
		return json.Marshal(output)
	}

	// Search files in parallel
	output := t.searchFiles(ctx, files, matcher, req, concurrency)

	output.Query = req.Query
	return json.Marshal(output)
}

// matcher is an interface for matching lines against a search query.
type matcher interface {
	Match(line string) []matchSpan
}

// matchSpan represents the start and end positions of a match within a line.
type matchSpan struct {
	start int
	end   int
}

// regexMatcher matches lines using a regular expression.
type regexMatcher struct {
	re *regexp.Regexp
}

func (m *regexMatcher) Match(line string) []matchSpan {
	matches := m.re.FindAllStringIndex(line, -1)
	if matches == nil {
		return nil
	}
	spans := make([]matchSpan, len(matches))
	for i, m := range matches {
		spans[i] = matchSpan{start: m[0], end: m[1]}
	}
	return spans
}

// textMatcher matches lines using plain text search.
type textMatcher struct {
	search string
	cased  bool
	whole  bool
}

func (m *textMatcher) Match(line string) []matchSpan {
	var searchIn string
	var searchFor string

	if !m.cased {
		searchIn = strings.ToLower(line)
		searchFor = strings.ToLower(m.search)
	} else {
		searchIn = line
		searchFor = m.search
	}

	var spans []matchSpan
	start := 0
	for {
		idx := strings.Index(searchIn[start:], searchFor)
		if idx == -1 {
			break
		}
		absStart := start + idx
		absEnd := absStart + len(m.search)

		// Check word boundaries if whole word is required
		if m.whole {
			if absStart > 0 && isWordChar(rune(line[absStart-1])) {
				start = absEnd
				continue
			}
			if absEnd < len(line) && isWordChar(rune(line[absEnd])) {
				start = absEnd
				continue
			}
		}

		spans = append(spans, matchSpan{start: absStart, end: absEnd})
		start = absEnd
	}

	return spans
}

// isWordChar returns true if the rune is a word character (letter, digit, or underscore).
func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// buildMatcher creates a matcher based on the search mode.
func (t CodeSearchTool) buildMatcher(req CodeSearchInput) (matcher, error) {
	query := req.Query

	switch req.Mode {
	case "text":
		return &textMatcher{search: query, cased: false, whole: req.WholeWord}, nil

	case "text_case":
		return &textMatcher{search: query, cased: true, whole: req.WholeWord}, nil

	case "literal":
		return &textMatcher{search: query, cased: true, whole: req.WholeWord}, nil

	case "regex":
		// Add word boundary anchors if whole word is requested
		if req.WholeWord {
			query = `\b` + query + `\b`
		}
		re, err := regexp.Compile(query)
		if err != nil {
			return nil, fmt.Errorf("invalid regex %q: %w", query, err)
		}
		return &regexMatcher{re: re}, nil

	default:
		return nil, fmt.Errorf("unknown search mode: %q (use text, text_case, regex, or literal)", req.Mode)
	}
}

// collectFiles walks the directory tree and collects files matching the patterns.
func (t CodeSearchTool) collectFiles(ctx context.Context, root string, includePatterns, excludePatterns []string, req CodeSearchInput) ([]string, error) {
	var files []string
	var mu sync.Mutex

	// Load ignore rules
	ignoreRules, err := t.loadIgnoreRules(root)
	if err != nil {
		// Non-fatal: just log and continue without ignore rules
		ignoreRules = nil
	}

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		// Check context
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err != nil {
			// Skip errors (e.g., permission denied)
			return nil
		}

		// Get relative path for pattern matching
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}

		// Check max depth
		if req.MaxDepth > 0 {
			depth := strings.Count(relPath, string(filepath.Separator))
			if depth >= req.MaxDepth {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Skip hidden files/directories unless requested
		if !req.IncludeHidden {
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Skip directories (but continue walking into them)
		if d.IsDir() {
			// Check exclude patterns for directories
			if matchesAnyPattern(relPath, excludePatterns) || matchesAnyPattern(filepath.Base(path), excludePatterns) {
				return filepath.SkipDir
			}
			return nil
		}

		// Check ignore rules
		if ignoreRules != nil && ignoreRules.IsMatch(relPath) {
			return nil
		}

		// Check include patterns
		if len(includePatterns) > 0 && !matchesAnyPattern(relPath, includePatterns) && !matchesAnyPattern(filepath.Base(path), includePatterns) {
			return nil
		}

		// Check exclude patterns
		if matchesAnyPattern(relPath, excludePatterns) || matchesAnyPattern(filepath.Base(path), excludePatterns) {
			return nil
		}

		// Check file size
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Size() > req.MaxFileSize {
			return nil
		}

		// Skip binary files (heuristic)
		if isBinaryFile(path, info) {
			return nil
		}

		mu.Lock()
		files = append(files, path)
		mu.Unlock()

		return nil
	})

	return files, err
}

// searchFiles searches the given files and returns matches.
func (t CodeSearchTool) searchFiles(ctx context.Context, files []string, m matcher, req CodeSearchInput, concurrency int) CodeSearchOutput {
	output := CodeSearchOutput{
		FilesSearched: len(files),
	}

	// Create channels for parallel processing
	type fileResult struct {
		matches []CodeMatch
		skipped bool
	}

	fileChan := make(chan string, len(files))
	resultChan := make(chan fileResult, len(files))

	// Start worker goroutines
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range fileChan {
				// Check context
				if ctx.Err() != nil {
					resultChan <- fileResult{skipped: true}
					continue
				}

				matches := t.searchFile(path, m, req)
				resultChan <- fileResult{matches: matches, skipped: len(matches) == 0}
			}
		}()
	}

	// Send files to workers
	for _, f := range files {
		fileChan <- f
	}
	close(fileChan)

	// Wait for all workers and close result channel
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	maxResults := req.MaxResults
	for result := range resultChan {
		if result.skipped {
			output.FilesSkipped++
			continue
		}

		for _, match := range result.matches {
			output.Matches = append(output.Matches, match)
			output.TotalMatches++

			// Check if we've hit the limit
			if maxResults > 0 && len(output.Matches) >= maxResults {
				output.Truncated = true
				break
			}
		}

		if output.Truncated {
			// Drain remaining results to let workers finish
			go func() {
				for range resultChan {
				}
			}()
			break
		}
	}

	return output
}

// searchFile searches a single file for matches.
func (t CodeSearchTool) searchFile(path string, m matcher, req CodeSearchInput) []CodeMatch {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	// Get relative path for display
	relPath := path
	if t.Root != "" {
		if rel, err := filepath.Rel(t.Root, path); err == nil {
			relPath = rel
		}
	}

	var matches []CodeMatch
	scanner := bufio.NewScanner(file)
	lineNum := 0

	// Buffer for context lines
	var lineBuffer []string
	var matchBuffer []int // indices into lineBuffer that had matches

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		lineBuffer = append(lineBuffer, line)

		spans := m.Match(line)
		if len(spans) > 0 {
			matchBuffer = append(matchBuffer, len(lineBuffer)-1)

			for _, span := range spans {
				match := CodeMatch{
					File:        relPath,
					Line:        lineNum,
					Column:      span.start + 1,
					LineContent: line,
					MatchText:   line[span.start:span.end],
				}
				matches = append(matches, match)
			}
		}

		// Trim the buffer if we don't need context anymore
		maxKeep := req.ContextLines + 1
		if len(lineBuffer) > maxKeep*2 {
			// Check if we can trim from the front
			minMatchIdx := -1
			for _, idx := range matchBuffer {
				if minMatchIdx == -1 || idx < minMatchIdx {
					minMatchIdx = idx
				}
			}
			if minMatchIdx > req.ContextLines {
				trimCount := minMatchIdx - req.ContextLines
				lineBuffer = lineBuffer[trimCount:]
				// Adjust match buffer indices
				newMatchBuffer := make([]int, 0, len(matchBuffer))
				for _, idx := range matchBuffer {
					if idx >= trimCount {
						newMatchBuffer = append(newMatchBuffer, idx-trimCount)
					}
				}
				matchBuffer = newMatchBuffer
			}
		}
	}

	// Now go back and add context to matches
	if req.ContextLines > 0 {
		// Re-scan to add context (simpler approach for correctness)
		contextMatches := t.addContext(path, relPath, m, req)
		return contextMatches
	}

	return matches
}

// addContext re-scans a file and adds context lines to matches.
func (t CodeSearchTool) addContext(path, relPath string, m matcher, req CodeSearchInput) []CodeMatch {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	var allLines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil
	}

	var matches []CodeMatch
	for lineNum, line := range allLines {
		spans := m.Match(line)
		if len(spans) == 0 {
			continue
		}

		for _, span := range spans {
			match := CodeMatch{
				File:        relPath,
				Line:        lineNum + 1,
				Column:      span.start + 1,
				LineContent: line,
				MatchText:   line[span.start:span.end],
			}

			// Add context before
			ctxStart := lineNum - req.ContextLines
			if ctxStart < 0 {
				ctxStart = 0
			}
			if ctxStart < lineNum {
				match.ContextBefore = make([]string, lineNum-ctxStart)
				copy(match.ContextBefore, allLines[ctxStart:lineNum])
			}

			// Add context after
			ctxEnd := lineNum + req.ContextLines + 1
			if ctxEnd > len(allLines) {
				ctxEnd = len(allLines)
			}
			if ctxEnd > lineNum+1 {
				match.ContextAfter = make([]string, ctxEnd-lineNum-1)
				copy(match.ContextAfter, allLines[lineNum+1:ctxEnd])
			}

			matches = append(matches, match)
		}
	}

	return matches
}

// ---------------------------------------------------------------------------
// Ignore Rules (.gitignore style)
// ---------------------------------------------------------------------------

// ignoreRules holds parsed ignore patterns.
type ignoreRules struct {
	patterns []ignorePattern
}

type ignorePattern struct {
	pattern string
	dirOnly bool
	negate  bool
}

// loadIgnoreRules loads ignore rules from .gitignore and .orchestaignore files.
func (t CodeSearchTool) loadIgnoreRules(root string) (*ignoreRules, error) {
	ignoreFiles := t.IgnoreFiles
	if len(ignoreFiles) == 0 {
		ignoreFiles = []string{".gitignore", ".orchestaignore"}
	}

	rules := &ignoreRules{}

	for _, filename := range ignoreFiles {
		path := filepath.Join(root, filename)
		file, err := os.Open(path)
		if err != nil {
			continue // File doesn't exist, skip
		}

		scanner := bufio.NewScanner(file)
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

			// Check for directory-only pattern (trailing /)
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

// IsMatch checks if a path matches any ignore rule.
func (r *ignoreRules) IsMatch(path string) bool {
	matched := false
	for _, pat := range r.patterns {
		if matchIgnorePattern(path, pat) {
			if pat.negate {
				matched = false
			} else {
				matched = true
			}
		}
	}
	return matched
}

// matchIgnorePattern checks if a path matches an ignore pattern.
func matchIgnorePattern(path string, pat ignorePattern) bool {
	// Simple glob matching
	matched, _ := filepath.Match(pat.pattern, filepath.Base(path))
	if matched {
		return true
	}

	// Try matching against the full path
	matched, _ = filepath.Match(pat.pattern, path)
	if matched {
		return true
	}

	// Try matching path components (for patterns like "dir/file")
	matched, _ = filepath.Match(pat.pattern, path)
	return matched
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// parseGlobPatterns parses a comma-separated list of glob patterns.
func parseGlobPatterns(s string) ([]string, error) {
	if s == "" {
		return nil, nil
	}

	patterns := strings.Split(s, ",")
	var result []string
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Validate the pattern
		if _, err := filepath.Match(p, "test"); err != nil {
			return nil, fmt.Errorf("invalid glob pattern %q: %w", p, err)
		}
		result = append(result, p)
	}
	return result, nil
}

// matchesAnyPattern checks if a path matches any of the given glob patterns.
func matchesAnyPattern(path string, patterns []string) bool {
	for _, pat := range patterns {
		// Try matching just the filename
		if matched, _ := filepath.Match(pat, filepath.Base(path)); matched {
			return true
		}
		// Try matching the full path
		if matched, _ := filepath.Match(pat, path); matched {
			return true
		}
		// Try matching with */ prefix for simple extensions like "*.go"
		if !strings.Contains(pat, "/") && strings.HasPrefix(pat, "*") {
			if matched, _ := filepath.Match(pat, filepath.Base(path)); matched {
				return true
			}
		}
	}
	return false
}

// isBinaryFile checks if a file is likely binary by reading the first few bytes.
func isBinaryFile(path string, info os.FileInfo) bool {
	// Skip obvious binary extensions
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".ico", ".webp",
		".mp3", ".mp4", ".avi", ".mov", ".wmv", ".flv",
		".zip", ".tar", ".gz", ".bz2", ".xz", ".7z", ".rar",
		".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
		".exe", ".dll", ".so", ".dylib", ".bin",
		".wasm", ".o", ".a", ".lib",
		".pyc", ".class", ".jar", ".war",
		".sqlite", ".db", ".pdb":
		return true
	}

	// For small files, check the content
	if info.Size() < 8192 {
		file, err := os.Open(path)
		if err != nil {
			return false
		}
		defer file.Close()

		buf := make([]byte, 512)
		n, err := file.Read(buf)
		if err != nil {
			return false
		}

		// Check for null bytes (common in binary files)
		for i := 0; i < n; i++ {
			if buf[i] == 0 {
				return true
			}
		}
	}

	return false
}

// marshalCodeSearchError creates a JSON error response for code_search.
func marshalCodeSearchError(err error) (json.RawMessage, error) {
	output := CodeSearchOutput{
		Error: err.Error(),
	}
	return json.Marshal(output)
}
