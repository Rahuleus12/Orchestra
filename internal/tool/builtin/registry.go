// Package builtin provides a registry of all built-in tools for the Orchestra tool system.
//
// The built-in tools are organized into two categories:
//   - General-purpose tools: HTTP requests, calculator, file I/O, web search, JSON transform, SQL query
//   - Coding-specific tools: file edit, code search, shell exec, git operations, list directory, diagnostics
//
// Use NewAllToolsRegistry() to get a registry with all 13 built-in tools,
// or use the more specific functions to get subsets.
package builtin

import (
	"fmt"

	"github.com/user/orchestra/internal/tool"
)

// General-purpose tool names.
const (
	HTTPRequestToolName  = "http_request"
	CalculatorToolName   = "calculator"
	FileReadToolName     = "file_read"
	FileWriteToolName    = "file_write"
	WebSearchToolName    = "web_search"
	JSONTransformToolName = "json_transform"
	SQLQueryToolName     = "sql_query"
)

// Coding-specific tool names.
const (
	FileEditToolName      = "file_edit"
	CodeSearchToolName    = "code_search"
	ShellExecToolName     = "shell_exec"
	GitOperationsToolName = "git_operations"
	ListDirectoryToolName = "list_directory"
	DiagnosticsToolName   = "diagnostics"
)

// GeneralPurposeToolNames returns the names of all general-purpose built-in tools.
func GeneralPurposeToolNames() []string {
	return []string{
		HTTPRequestToolName,
		CalculatorToolName,
		FileReadToolName,
		FileWriteToolName,
		WebSearchToolName,
		JSONTransformToolName,
		SQLQueryToolName,
	}
}

// CodingToolNames returns the names of all coding-specific built-in tools.
func CodingToolNames() []string {
	return []string{
		FileEditToolName,
		CodeSearchToolName,
		ShellExecToolName,
		GitOperationsToolName,
		ListDirectoryToolName,
		DiagnosticsToolName,
	}
}

// AllToolNames returns the names of all 13 built-in tools.
func AllToolNames() []string {
	return append(GeneralPurposeToolNames(), CodingToolNames()...)
}

// ---------------------------------------------------------------------------
// Registry Creation Functions
// ---------------------------------------------------------------------------

// NewAllToolsRegistry creates a new tool.Registry with all 13 built-in tools.
//
// Tools are registered in the default namespace (no prefix). Use
// NewAllToolsRegistryWithNamespace() to add a namespace prefix to avoid
// collisions with custom tools.
//
// File-based tools (file_read, file_write, file_edit, code_search,
// list_directory, shell_exec, git_operations, diagnostics) are configured
// with the specified root directory for filesystem access restrictions.
//
// The shell_exec tool is created with an empty allowlist, meaning all
// commands are permitted. For production use, configure the tool with
// NewRestrictedShellExecTool() before registering.
func NewAllToolsRegistry(root string) (*tool.ToolRegistry, error) {
	return NewAllToolsRegistryWithNamespace("", root)
}

// NewAllToolsRegistryWithNamespace creates a new tool.Registry with all
// 13 built-in tools registered under the given namespace.
//
// If namespace is "fs", tools will be registered as "fs:file_read",
// "fs:file_write", etc. Use an empty string for no namespace.
func NewAllToolsRegistryWithNamespace(namespace, root string) (*tool.ToolRegistry, error) {
	registry := tool.NewRegistry()

	if err := RegisterGeneralTools(registry, namespace, root); err != nil {
		return nil, fmt.Errorf("register general tools: %w", err)
	}

	if err := RegisterCodingTools(registry, namespace, root); err != nil {
		return nil, fmt.Errorf("register coding tools: %w", err)
	}

	return registry, nil
}

// RegisterGeneralTools registers the 7 general-purpose built-in tools.
func RegisterGeneralTools(registry *tool.ToolRegistry, namespace, root string) error {
	// HTTP Request
	httpTool := NewHTTPRequestTool()
	if err := registry.RegisterInNamespace(namespace, httpTool); err != nil {
		return fmt.Errorf("register %s: %w", HTTPRequestToolName, err)
	}

	// Calculator
	calcTool := NewCalculatorTool()
	if err := registry.RegisterInNamespace(namespace, calcTool); err != nil {
		return fmt.Errorf("register %s: %w", CalculatorToolName, err)
	}

	// File Read
	fileReadTool := NewFileReadToolWithRoot(root)
	if err := registry.RegisterInNamespace(namespace, fileReadTool); err != nil {
		return fmt.Errorf("register %s: %w", FileReadToolName, err)
	}

	// File Write
	fileWriteTool := NewFileWriteToolWithRoot(root)
	if err := registry.RegisterInNamespace(namespace, fileWriteTool); err != nil {
		return fmt.Errorf("register %s: %w", FileWriteToolName, err)
	}

	// Web Search
	webSearchTool := NewWebSearchTool()
	if err := registry.RegisterInNamespace(namespace, webSearchTool); err != nil {
		return fmt.Errorf("register %s: %w", WebSearchToolName, err)
	}

	// JSON Transform
	jsonTool := NewJSONTransformTool()
	if err := registry.RegisterInNamespace(namespace, jsonTool); err != nil {
		return fmt.Errorf("register %s: %w", JSONTransformToolName, err)
	}

	// SQL Query
	sqlTool := NewSQLQueryTool()
	if err := registry.RegisterInNamespace(namespace, sqlTool); err != nil {
		return fmt.Errorf("register %s: %w", SQLQueryToolName, err)
	}

	return nil
}

// RegisterCodingTools registers the 6 coding-specific built-in tools.
func RegisterCodingTools(registry *tool.ToolRegistry, namespace, root string) error {
	// File Edit
	fileEditTool := NewFileEditToolWithRoot(root)
	if err := registry.RegisterInNamespace(namespace, fileEditTool); err != nil {
		return fmt.Errorf("register %s: %w", FileEditToolName, err)
	}

	// Code Search
	codeSearchTool := NewCodeSearchToolWithRoot(root)
	if err := registry.RegisterInNamespace(namespace, codeSearchTool); err != nil {
		return fmt.Errorf("register %s: %w", CodeSearchToolName, err)
	}

	// Shell Exec
	shellTool := NewShellExecToolWithRoot(root)
	if err := registry.RegisterInNamespace(namespace, shellTool); err != nil {
		return fmt.Errorf("register %s: %w", ShellExecToolName, err)
	}

	// Git Operations
	gitTool := NewGitOperationsToolWithRoot(root)
	if err := registry.RegisterInNamespace(namespace, gitTool); err != nil {
		return fmt.Errorf("register %s: %w", GitOperationsToolName, err)
	}

	// List Directory
	listDirTool := NewListDirectoryToolWithRoot(root)
	if err := registry.RegisterInNamespace(namespace, listDirTool); err != nil {
		return fmt.Errorf("register %s: %w", ListDirectoryToolName, err)
	}

	// Diagnostics
	diagTool := NewDiagnosticsToolWithRoot(root)
	if err := registry.RegisterInNamespace(namespace, diagTool); err != nil {
		return fmt.Errorf("register %s: %w", DiagnosticsToolName, err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Subset Registry Functions
// ---------------------------------------------------------------------------

// NewReadOnlyToolsRegistry creates a registry with tools that don't modify
// the filesystem. This includes:
//   - http_request, calculator, web_search, json_transform, sql_query
//   - file_read, code_search, git_operations (read mode), list_directory, diagnostics
//
// Excludes: file_write, file_edit, shell_exec
func NewReadOnlyToolsRegistry(root string) (*tool.ToolRegistry, error) {
	registry := tool.NewRegistry()

	// General tools (all are read-only or external)
	if err := RegisterGeneralTools(registry, "", root); err != nil {
		return nil, err
	}

	// Coding tools (read-only subset)
	fileReadTool := NewFileReadToolWithRoot(root)
	if err := registry.Register(fileReadTool); err != nil {
		return nil, err
	}

	codeSearchTool := NewCodeSearchToolWithRoot(root)
	if err := registry.Register(codeSearchTool); err != nil {
		return nil, err
	}

	gitTool := NewGitOperationsToolWithRoot(root) // Read-only by default
	if err := registry.Register(gitTool); err != nil {
		return nil, err
	}

	listDirTool := NewListDirectoryToolWithRoot(root)
	if err := registry.Register(listDirTool); err != nil {
		return nil, err
	}

	diagTool := NewDiagnosticsToolWithRoot(root)
	if err := registry.Register(diagTool); err != nil {
		return nil, err
	}

	return registry, nil
}

// NewSafeToolsRegistry creates a registry with "safe" tools that have minimal
// security risk. This includes:
//   - calculator, web_search, json_transform
//   - file_read, code_search, list_directory
//
// Excludes: http_request (network), file_write, file_edit, shell_exec,
// git_operations, sql_query, diagnostics
func NewSafeToolsRegistry(root string) (*tool.ToolRegistry, error) {
	registry := tool.NewRegistry()

	calcTool := NewCalculatorTool()
	if err := registry.Register(calcTool); err != nil {
		return nil, err
	}

	webSearchTool := NewWebSearchTool()
	if err := registry.Register(webSearchTool); err != nil {
		return nil, err
	}

	jsonTool := NewJSONTransformTool()
	if err := registry.Register(jsonTool); err != nil {
		return nil, err
	}

	fileReadTool := NewFileReadToolWithRoot(root)
	if err := registry.Register(fileReadTool); err != nil {
		return nil, err
	}

	codeSearchTool := NewCodeSearchToolWithRoot(root)
	if err := registry.Register(codeSearchTool); err != nil {
		return nil, err
	}

	listDirTool := NewListDirectoryToolWithRoot(root)
	if err := registry.Register(listDirTool); err != nil {
		return nil, err
	}

	return registry, nil
}

// NewFileSystemToolsRegistry creates a registry with only filesystem tools:
// file_read, file_write, file_edit, list_directory, code_search.
func NewFileSystemToolsRegistry(root string) (*tool.ToolRegistry, error) {
	registry := tool.NewRegistry()

	fileReadTool := NewFileReadToolWithRoot(root)
	if err := registry.Register(fileReadTool); err != nil {
		return nil, err
	}

	fileWriteTool := NewFileWriteToolWithRoot(root)
	if err := registry.Register(fileWriteTool); err != nil {
		return nil, err
	}

	fileEditTool := NewFileEditToolWithRoot(root)
	if err := registry.Register(fileEditTool); err != nil {
		return nil, err
	}

	listDirTool := NewListDirectoryToolWithRoot(root)
	if err := registry.Register(listDirTool); err != nil {
		return nil, err
	}

	codeSearchTool := NewCodeSearchToolWithRoot(root)
	if err := registry.Register(codeSearchTool); err != nil {
		return nil, err
	}

	return registry, nil
}

// NewCLIToolsRegistry creates a registry with the coding-specific tools
// needed for CLI mode: file_edit, code_search, shell_exec, git_operations,
// list_directory, diagnostics.
func NewCLIToolsRegistry(root string) (*tool.ToolRegistry, error) {
	registry := tool.NewRegistry()
	return registry, RegisterCodingTools(registry, "", root)
}

// ---------------------------------------------------------------------------
// Individual Tool Constructors (with namespace support)
// ---------------------------------------------------------------------------

// NewHTTPTool creates and returns the http_request tool.
func NewHTTPTool() tool.Tool {
	return NewHTTPRequestTool()
}

// NewCalculator creates and returns the calculator tool.
func NewCalculator() tool.Tool {
	return NewCalculatorTool()
}

// NewFileRead creates a file_read tool restricted to the given root directory.
func NewFileRead(root string) tool.Tool {
	return NewFileReadToolWithRoot(root)
}

// NewFileWrite creates a file_write tool restricted to the given root directory.
func NewFileWrite(root string) tool.Tool {
	return NewFileWriteToolWithRoot(root)
}

// NewFileEdit creates a file_edit tool restricted to the given root directory.
func NewFileEdit(root string) tool.Tool {
	return NewFileEditToolWithRoot(root)
}

// NewCodeSearch creates a code_search tool restricted to the given root directory.
func NewCodeSearch(root string) tool.Tool {
	return NewCodeSearchToolWithRoot(root)
}

// NewShell creates a shell_exec tool restricted to the given root directory.
// For production use, configure with NewRestrictedShellExecTool() instead.
func NewShell(root string) tool.Tool {
	return NewShellExecToolWithRoot(root)
}

// NewGit creates a git_operations tool restricted to the given root directory.
// Write operations are disabled by default.
func NewGit(root string) tool.Tool {
	return NewGitOperationsToolWithRoot(root)
}

// NewGitReadWrite creates a git_operations tool with write access enabled.
func NewGitReadWrite(root string) tool.Tool {
	return NewGitOperationsToolReadWrite(root)
}

// NewListDir creates a list_directory tool restricted to the given root directory.
func NewListDir(root string) tool.Tool {
	return NewListDirectoryToolWithRoot(root)
}

// NewDiagnostics creates a diagnostics tool restricted to the given root directory.
func NewDiagnostics(root string) tool.Tool {
	return NewDiagnosticsToolWithRoot(root)
}

// NewWebSearch creates a web_search tool with automatic backend selection.
func NewWebSearch() tool.Tool {
	return NewWebSearchTool()
}

// NewJSONTransform creates a json_transform tool.
func NewJSONTransform() tool.Tool {
	return NewJSONTransformTool()
}

// NewSQLQuery creates an sql_query tool with no pre-configured connections.
// Use SQLQueryTool.AddConnection() or AddConnectionFromConfig() to add databases.
func NewSQLQuery() tool.Tool {
	return NewSQLQueryTool()
}

// ---------------------------------------------------------------------------
// Provider Definition Helpers
// ---------------------------------------------------------------------------

// AllToolDefinitions returns provider.ToolDefinition for all 13 built-in tools.
// This is useful when you need to send tool definitions to an LLM provider
// without using the tool registry.
func AllToolDefinitions(root string) []tool.ToolDefinition {
	tools := []tool.Tool{
		NewHTTPRequestTool(),
		NewCalculatorTool(),
		NewFileReadToolWithRoot(root),
		NewFileWriteToolWithRoot(root),
		NewWebSearchTool(),
		NewJSONTransformTool(),
		NewSQLQueryTool(),
		NewFileEditToolWithRoot(root),
		NewCodeSearchToolWithRoot(root),
		NewShellExecToolWithRoot(root),
		NewGitOperationsToolWithRoot(root),
		NewListDirectoryToolWithRoot(root),
		NewDiagnosticsToolWithRoot(root),
	}

	defs := make([]tool.ToolDefinition, len(tools))
	for i, t := range tools {
		defs[i] = tool.Definition(t)
	}
	return defs
}

// GeneralToolDefinitions returns provider.ToolDefinition for general-purpose tools only.
func GeneralToolDefinitions(root string) []tool.ToolDefinition {
	tools := []tool.Tool{
		NewHTTPRequestTool(),
		NewCalculatorTool(),
		NewFileReadToolWithRoot(root),
		NewFileWriteToolWithRoot(root),
		NewWebSearchTool(),
		NewJSONTransformTool(),
		NewSQLQueryTool(),
	}

	defs := make([]tool.ToolDefinition, len(tools))
	for i, t := range tools {
		defs[i] = tool.Definition(t)
	}
	return defs
}

// CodingToolDefinitions returns provider.ToolDefinition for coding-specific tools only.
func CodingToolDefinitions(root string) []tool.ToolDefinition {
	tools := []tool.Tool{
		NewFileEditToolWithRoot(root),
		NewCodeSearchToolWithRoot(root),
		NewShellExecToolWithRoot(root),
		NewGitOperationsToolWithRoot(root),
		NewListDirectoryToolWithRoot(root),
		NewDiagnosticsToolWithRoot(root),
	}

	defs := make([]tool.ToolDefinition, len(tools))
	for i, t := range tools {
		defs[i] = tool.Definition(t)
	}
	return defs
}

// ---------------------------------------------------------------------------
// Tool Count Constants
// ---------------------------------------------------------------------------

const (
	// GeneralToolCount is the number of general-purpose built-in tools.
	GeneralToolCount = 7

	// CodingToolCount is the number of coding-specific built-in tools.
	CodingToolCount = 6

	// TotalToolCount is the total number of built-in tools.
	TotalToolCount = GeneralToolCount + CodingToolCount
)

// ---------------------------------------------------------------------------
// Notes on code_interpreter
// ---------------------------------------------------------------------------

// Note: The code_interpreter tool (listed in the original Phase 6 plan) is
// not implemented as a built-in tool because it requires true process-level
// isolation (e.g., gVisor, containers, WebAssembly sandbox) which is beyond
// the scope of an in-process tool implementation.
//
// For code execution needs, use the shell_exec tool with appropriate
// restrictions, or implement a custom code_interpreter tool that wraps
// a sandboxed execution environment of your choice.
//
// Example custom implementation:
//
//	codeTool := tool.New("code_interpreter",
//	    tool.WithDescription("Execute code in a sandboxed environment"),
//	    tool.WithInputSchema[CodeInput](),
//	    tool.WithStringHandler(func(ctx context.Context, input CodeInput) (string, error) {
//	        // Call your sandboxed execution backend (e.g., Jupyter, WASM runtime)
//	        return mySandbox.Execute(ctx, input.Language, input.Code)
//	    }),
//	)
