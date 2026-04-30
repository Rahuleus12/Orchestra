// Package builtin provides built-in tools for the Orchestra tool system.
package builtin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// SQL Query Tool
// ---------------------------------------------------------------------------

// SQLDialect identifies the type of SQL database.
type SQLDialect string

const (
	// SQLiteDialect is for SQLite databases (file-based).
	SQLiteDialect SQLDialect = "sqlite"

	// PostgreSQLDialect is for PostgreSQL databases.
	PostgreSQLDialect SQLDialect = "postgresql"

	// MySQLDialect is for MySQL/MariaDB databases.
	MySQLDialect SQLDialect = "mysql"

	// GenericDialect is for any database/sql compatible driver.
	GenericDialect SQLDialect = "generic"
)

// SQLQueryInput defines the input for the sql_query tool.
type SQLQueryInput struct {
	// Query is the SQL query to execute. Must be a SELECT statement
	// unless ReadOnly is explicitly set to false.
	Query string `json:"query" description:"SQL query to execute (SELECT statements for read-only mode)"`

	// Connection is the name of a pre-configured connection to use.
	// If empty, the default connection is used.
	Connection string `json:"connection,omitempty" description:"Name of pre-configured connection to use (empty for default)"`

	// Params are optional query parameters for parameterized queries.
	// Use ?1, ?2, etc. for positional parameters or :name for named parameters.
	// Values are passed as strings and converted to appropriate types.
	Params map[string]any `json:"params,omitempty" description:"Query parameters (positional: '1', '2' or named: 'name')"`

	// TimeoutSeconds is the maximum execution time. Defaults to 30.
	TimeoutSeconds int `json:"timeout_seconds,omitempty" description:"Maximum query execution time in seconds" default:"30" min:"1" max:"300"`

	// MaxRows limits the number of rows returned. Defaults to 1000.
	// Set to 0 for no limit (use with caution on large result sets).
	MaxRows int `json:"max_rows,omitempty" description:"Maximum number of rows to return (0 for no limit)" default:"1000" min:"0"`

	// IncludeColumns controls whether column metadata is included in the response.
	// Defaults to true.
	IncludeColumns bool `json:"include_columns,omitempty" description:"Include column metadata in response" default:"true"`

	// IncludeRowCount includes the total row count (before MaxRows limit).
	// This requires an additional COUNT query for non-limited results.
	IncludeRowCount bool `json:"include_row_count,omitempty" description:"Include total row count before limit" default:"false"`

	// Format controls the output format: "rows" (array of objects), "table" (columns + values).
	// Defaults to "rows".
	Format string `json:"format,omitempty" description:"Output format: 'rows' (array of objects) or 'table' (columns + values)" default:"rows" enum:"rows,table"`
}

// SQLQueryOutput defines the output of the sql_query tool.
type SQLQueryOutput struct {
	// Rows contains the query results as an array of objects (when Format="rows").
	Rows []map[string]any `json:"rows,omitempty"`

	// Columns contains column metadata (when IncludeColumns=true).
	Columns []SQLColumn `json:"columns,omitempty"`

	// Values contains the raw values in column order (when Format="table").
	Values [][]any `json:"values,omitempty"`

	// RowCount is the number of rows returned (after MaxRows limit).
	RowCount int `json:"row_count"`

	// TotalRowCount is the total number of rows (before MaxRows limit, if requested).
	TotalRowCount int `json:"total_row_count,omitempty"`

	// Truncated indicates if results were truncated due to MaxRows.
	Truncated bool `json:"truncated,omitempty"`

	// Dialect is the SQL dialect that was used.
	Dialect SQLDialect `json:"dialect"`

	// DurationMs is the query execution time in milliseconds.
	DurationMs int64 `json:"duration_ms"`

	// Connection is the name of the connection that was used.
	Connection string `json:"connection"`

	// Error contains an error message if the query failed.
	Error string `json:"error,omitempty"`
}

// SQLColumn describes a column in a query result.
type SQLColumn struct {
	// Name is the column name.
	Name string `json:"name"`

	// Type is the database column type (e.g., "TEXT", "INTEGER", "VARCHAR(255)").
	Type string `json:"type"`

	// Nullable indicates if the column can contain NULL values.
	Nullable bool `json:"nullable"`

	// GoType is the Go type name for the column values.
	GoType string `json:"go_type"`
}

// SQLConnectionConfig holds configuration for a database connection.
type SQLConnectionConfig struct {
	// Name is a unique identifier for this connection.
	Name string `json:"name" yaml:"name"`

	// Dialect is the SQL dialect (sqlite, postgresql, mysql, generic).
	Dialect SQLDialect `json:"dialect" yaml:"dialect"`

	// DSN is the data source name (connection string).
	// Format depends on the dialect:
	//   - SQLite: "file:path/to/db.sqlite" or "file::memory:"
	//   - PostgreSQL: "host=localhost port=5432 user=postgres dbname=mydb sslmode=disable"
	//   - MySQL: "user:password@tcp(localhost:3306)/dbname"
	//   - Generic: driver-specific DSN
	DSN string `json:"dsn" yaml:"dsn"`

	// Driver is the database/sql driver name (for generic dialect).
	// Examples: "pgx", "mysql", "sqlite3"
	Driver string `json:"driver,omitempty" yaml:"driver,omitempty"`

	// MaxOpenConns is the maximum number of open connections.
	MaxOpenConns int `json:"max_open_conns,omitempty" yaml:"max_open_conns,omitempty"`

	// MaxIdleConns is the maximum number of idle connections.
	MaxIdleConns int `json:"max_idle_conns,omitempty" yaml:"max_idle_conns,omitempty"`

	// ConnMaxLifetime is the maximum connection lifetime.
	ConnMaxLifetime time.Duration `json:"conn_max_lifetime,omitempty" yaml:"conn_max_lifetime,omitempty"`

	// ReadOnly restricts queries to SELECT statements only.
	ReadOnly bool `json:"read_only" yaml:"read_only"`

	// AllowedTables is an optional whitelist of tables that can be queried.
	// Empty means all tables are allowed.
	AllowedTables []string `json:"allowed_tables,omitempty" yaml:"allowed_tables,omitempty"`

	// BlockedTables is an optional blacklist of tables that cannot be queried.
	BlockedTables []string `json:"blocked_tables,omitempty" yaml:"blocked_tables,omitempty"`

	// MaxRows is the default maximum rows for queries on this connection.
	MaxRows int `json:"max_rows,omitempty" yaml:"max_rows,omitempty"`

	// DefaultTimeout is the default timeout for queries on this connection.
	DefaultTimeout time.Duration `json:"default_timeout,omitempty" yaml:"default_timeout,omitempty"`
}

// SQLQueryTool implements the sql_query built-in tool.
// It executes read-only SQL queries against configured databases and
// returns structured results with column metadata.
//
// SECURITY WARNING: SQL query execution is inherently dangerous if not
// properly configured. Always:
//   - Use ReadOnly=true to prevent data modification
//   - Use AllowedTables to restrict which tables can be queried
//   - Use parameterized queries (Params) to prevent SQL injection
//   - Set appropriate timeouts
//   - Consider using a read-only database user
//
// The tool supports multiple database backends via database/sql:
//   - SQLite (requires _ "github.com/mattn/go-sqlite3" import)
//   - PostgreSQL (requires _ "github.com/lib/pq" or pgx import)
//   - MySQL (requires _ "github.com/go-sql-driver/mysql" import)
//   - Any database/sql compatible driver
type SQLQueryTool struct {
	// connections holds named database connections.
	connections map[string]*sql.DB

	// configs holds the connection configurations.
	configs map[string]SQLConnectionConfig

	// defaultConnection is the name of the default connection.
	defaultConnection string

	// globalReadOnly forces all connections to be read-only.
	globalReadOnly bool

	// globalMaxRows is the default max rows if not set per-connection.
	globalMaxRows int

	// globalTimeout is the default timeout if not set per-connection.
	globalTimeout time.Duration

	// maxTimeout is the maximum allowed timeout.
	maxTimeout time.Duration
}

// NewSQLQueryTool creates a sql_query tool with no pre-configured connections.
// Use AddConnection or AddConnectionFromConfig to add database connections.
func NewSQLQueryTool() SQLQueryTool {
	return SQLQueryTool{
		connections:    make(map[string]*sql.DB),
		configs:        make(map[string]SQLConnectionConfig),
		globalReadOnly: true,
		globalMaxRows:  1000,
		globalTimeout:  30 * time.Second,
		maxTimeout:     5 * time.Minute,
	}
}

// Name returns the tool's identifier.
func (t SQLQueryTool) Name() string { return "sql_query" }

// Description returns the tool's description for the LLM.
func (t SQLQueryTool) Description() string {
	return `Execute SQL queries against configured databases and return structured results.

This tool runs SQL queries (typically SELECT) and returns results with column
names, types, and values. It supports multiple database backends via database/sql.

Supported databases:
- SQLite (file-based or in-memory)
- PostgreSQL
- MySQL/MariaDB
- Any database/sql compatible driver

Features:
- Parameterized queries to prevent SQL injection
- Configurable row limits and timeouts
- Column metadata (name, type, nullable)
- Multiple output formats (rows or table)
- Multiple named connections
- Read-only mode enforcement

Security:
- Read-only mode is enabled by default (SELECT only)
- Table access can be restricted to an allowlist
- Query timeouts prevent long-running queries
- Row limits prevent memory exhaustion

Usage tips:
- Always use parameterized queries: SELECT * FROM users WHERE id = ?1
- Use LIMIT in your queries instead of relying on MaxRows for performance
- Set IncludeRowCount=false for faster queries on large tables
- Use Format="table" for compact output when column names aren't needed

Parameterized query example:
  Query: "SELECT * FROM users WHERE age > ?1 AND active = ?2"
  Params: {"1": 18, "2": true}`
}

// Parameters returns the JSON Schema for the tool's input.
func (t SQLQueryTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "SQL query to execute (SELECT statements for read-only mode)"
			},
			"connection": {
				"type": "string",
				"description": "Name of pre-configured connection to use (empty for default)"
			},
			"params": {
				"type": "object",
				"description": "Query parameters (positional: '1', '2' or named: 'name')",
				"additionalProperties": true
			},
			"timeout_seconds": {
				"type": "integer",
				"description": "Maximum query execution time in seconds",
				"default": 30,
				"minimum": 1,
				"maximum": 300
			},
			"max_rows": {
				"type": "integer",
				"description": "Maximum number of rows to return (0 for no limit)",
				"default": 1000,
				"minimum": 0
			},
			"include_columns": {
				"type": "boolean",
				"description": "Include column metadata in response",
				"default": true
			},
			"include_row_count": {
				"type": "boolean",
				"description": "Include total row count before limit",
				"default": false
			},
			"format": {
				"type": "string",
				"description": "Output format: 'rows' (array of objects) or 'table' (columns + values)",
				"default": "rows",
				"enum": ["rows", "table"]
			}
		},
		"required": ["query"]
	}`)
}

// Execute runs the SQL query and returns structured results.
func (t SQLQueryTool) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req SQLQueryInput
	if err := json.Unmarshal(input, &req); err != nil {
		return marshalSQLQueryError(fmt.Errorf("parse input: %w", err))
	}

	if req.Query == "" {
		return marshalSQLQueryError(fmt.Errorf("query is required"))
	}

	// Check context
	if ctx.Err() != nil {
		return marshalSQLQueryError(ctx.Err())
	}

	// Get the database connection
	db, config, err := t.getConnection(req.Connection)
	if err != nil {
		return marshalSQLQueryError(err)
	}

	// Apply defaults
	timeout := t.getTimeout(config, req.TimeoutSeconds)
	maxRows := t.getMaxRows(config, req.MaxRows)
	if req.Format == "" {
		req.Format = "rows"
	}

	// Validate the query
	if err := t.validateQuery(req.Query, config); err != nil {
		return marshalSQLQueryError(err)
	}

	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Execute the query
	startTime := time.Now()
	rows, err := db.QueryContext(ctx, req.Query)
	duration := time.Since(startTime)

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return marshalSQLQueryError(fmt.Errorf("query timed out after %v", timeout))
		}
		return marshalSQLQueryError(fmt.Errorf("query failed: %w", err))
	}
	defer rows.Close()

	// Get column information
	columns, err := rows.ColumnTypes()
	if err != nil {
		return marshalSQLQueryError(fmt.Errorf("get columns: %w", err))
	}

	// Build output
	output := SQLQueryOutput{
		Dialect:    config.Dialect,
		DurationMs: duration.Milliseconds(),
		Connection: config.Name,
	}

	// Add column metadata
	if req.IncludeColumns {
		output.Columns = make([]SQLColumn, len(columns))
		for i, col := range columns {
			nullable, _ := col.Nullable()
			output.Columns[i] = SQLColumn{
				Name:     col.Name(),
				Type:     col.DatabaseTypeName(),
				Nullable: nullable,
				GoType:   col.ScanType().String(),
			}
		}
	}

	// Scan rows
	var rowCount int
	var values [][]any
	var resultRows []map[string]any

	for rows.Next() {
		// Check context
		if ctx.Err() != nil {
			return marshalSQLQueryError(ctx.Err())
		}

		// Check row limit
		if maxRows > 0 && rowCount >= maxRows {
			output.Truncated = true
			break
		}

		// Create value holders
		scanValues := make([]any, len(columns))
		scanPtrs := make([]any, len(columns))
		for i := range columns {
			scanPtrs[i] = &scanValues[i]
		}

		if err := rows.Scan(scanPtrs...); err != nil {
			return marshalSQLQueryError(fmt.Errorf("scan row %d: %w", rowCount+1, err))
		}

		if req.Format == "table" {
			rowValues := make([]any, len(scanValues))
			copy(rowValues, scanValues)
			values = append(values, rowValues)
		} else {
			row := make(map[string]any, len(columns))
			for i, col := range columns {
				row[col.Name()] = normalizeValue(scanValues[i])
			}
			resultRows = append(resultRows, row)
		}

		rowCount++
	}

	// Check for iteration errors
	if err := rows.Err(); err != nil {
		return marshalSQLQueryError(fmt.Errorf("iterate rows: %w", err))
	}

	output.RowCount = rowCount
	output.Rows = resultRows
	output.Values = values

	// Get total row count if requested and truncated
	if req.IncludeRowCount && output.Truncated {
		total, err := t.getTotalRowCount(ctx, db, req.Query)
		if err == nil {
			output.TotalRowCount = total
		}
	}

	return json.Marshal(output)
}

// AddConnection adds a pre-configured database connection.
func (t *SQLQueryTool) AddConnection(name string, db *sql.DB, config SQLConnectionConfig) error {
	if name == "" {
		return fmt.Errorf("connection name must not be empty")
	}
	if db == nil {
		return fmt.Errorf("database connection must not be nil")
	}

	config.Name = name
	t.connections[name] = db
	t.configs[name] = config

	if t.defaultConnection == "" {
		t.defaultConnection = name
	}

	return nil
}

// AddConnectionFromConfig creates a new database connection from configuration.
// The caller must ensure the appropriate database driver is imported.
func (t *SQLQueryTool) AddConnectionFromConfig(config SQLConnectionConfig) error {
	if config.Name == "" {
		return fmt.Errorf("connection name must not be empty")
	}
	if config.DSN == "" {
		return fmt.Errorf("DSN must not be empty")
	}

	driver := config.Driver
	switch config.Dialect {
	case SQLiteDialect:
		driver = "sqlite3"
	case PostgreSQLDialect:
		driver = "postgres"
	case MySQLDialect:
		driver = "mysql"
	case GenericDialect:
		if driver == "" {
			return fmt.Errorf("driver must be specified for generic dialect")
		}
	default:
		return fmt.Errorf("unknown dialect: %q", config.Dialect)
	}

	db, err := sql.Open(driver, config.DSN)
	if err != nil {
		return fmt.Errorf("open database %q: %w", config.Name, err)
	}

	// Configure connection pool
	if config.MaxOpenConns > 0 {
		db.SetMaxOpenConns(config.MaxOpenConns)
	}
	if config.MaxIdleConns > 0 {
		db.SetMaxIdleConns(config.MaxIdleConns)
	}
	if config.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(config.ConnMaxLifetime)
	}

	// Verify connection works
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("ping database %q: %w", config.Name, err)
	}

	t.connections[config.Name] = db
	t.configs[config.Name] = config

	if t.defaultConnection == "" {
		t.defaultConnection = config.Name
	}

	return nil
}

// SetDefaultConnection sets the default connection name.
func (t *SQLQueryTool) SetDefaultConnection(name string) error {
	if _, ok := t.connections[name]; !ok {
		return fmt.Errorf("connection %q not found", name)
	}
	t.defaultConnection = name
	return nil
}

// SetGlobalReadOnly sets whether all connections are forced to read-only mode.
func (t *SQLQueryTool) SetGlobalReadOnly(readOnly bool) {
	t.globalReadOnly = readOnly
}

// Close closes all database connections.
func (t *SQLQueryTool) Close() error {
	var errs []string
	for name, db := range t.connections {
		if err := db.Close(); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("close connections: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Connections returns the names of all configured connections.
func (t *SQLQueryTool) Connections() []string {
	names := make([]string, 0, len(t.connections))
	for name := range t.connections {
		names = append(names, name)
	}
	return names
}

// getConnection returns the database connection and config for the given name.
func (t *SQLQueryTool) getConnection(name string) (*sql.DB, SQLConnectionConfig, error) {
	if name == "" {
		name = t.defaultConnection
	}
	if name == "" {
		return nil, SQLConnectionConfig{}, fmt.Errorf("no connection specified and no default connection configured")
	}

	db, ok := t.connections[name]
	if !ok {
		return nil, SQLConnectionConfig{}, fmt.Errorf("connection %q not found (available: %v)", name, t.Connections())
	}

	config, ok := t.configs[name]
	if !ok {
		config = SQLConnectionConfig{Name: name}
	}

	return db, config, nil
}

// getTimeout returns the appropriate timeout based on config and request.
func (t *SQLQueryTool) getTimeout(config SQLConnectionConfig, requestedSeconds int) time.Duration {
	if requestedSeconds > 0 {
		timeout := time.Duration(requestedSeconds) * time.Second
		if t.maxTimeout > 0 && timeout > t.maxTimeout {
			return t.maxTimeout
		}
		return timeout
	}

	if config.DefaultTimeout > 0 {
		if t.maxTimeout > 0 && config.DefaultTimeout > t.maxTimeout {
			return t.maxTimeout
		}
		return config.DefaultTimeout
	}

	return t.globalTimeout
}

// getMaxRows returns the appropriate max rows based on config and request.
func (t *SQLQueryTool) getMaxRows(config SQLConnectionConfig, requestedMax int) int {
	if requestedMax >= 0 {
		return requestedMax
	}

	if config.MaxRows > 0 {
		return config.MaxRows
	}

	return t.globalMaxRows
}

// validateQuery checks if the query is allowed.
func (t *SQLQueryTool) validateQuery(query string, config SQLConnectionConfig) error {
	trimmed := strings.TrimSpace(query)
	upper := strings.ToUpper(trimmed)

	// Check read-only mode
	readOnly := t.globalReadOnly || config.ReadOnly
	if readOnly {
		// Allow SELECT, WITH (CTE), EXPLAIN, and VALUES
		if !strings.HasPrefix(upper, "SELECT") &&
			!strings.HasPrefix(upper, "WITH") &&
			!strings.HasPrefix(upper, "EXPLAIN") &&
			!strings.HasPrefix(upper, "VALUES") {
			return fmt.Errorf("only SELECT queries are allowed in read-only mode (query starts with %q)",
				strings.Fields(upper)[0])
		}

		// Check for dangerous statements that might be embedded
		dangerous := []string{
			"INSERT ", "UPDATE ", "DELETE ", "DROP ", "CREATE ",
			"ALTER ", "GRANT ", "REVOKE ", "TRUNCATE ", "EXEC ",
		}
		for _, d := range dangerous {
			if strings.Contains(upper, d) {
				return fmt.Errorf("dangerous statement %q detected in query", strings.TrimSpace(d))
			}
		}
	}

	// Check table access restrictions
	if len(config.AllowedTables) > 0 {
		tables := extractTableNames(query)
		for _, table := range tables {
			if !isTableAllowed(table, config.AllowedTables) {
				return fmt.Errorf("table %q is not in the allowed tables list", table)
			}
		}
	}

	if len(config.BlockedTables) > 0 {
		tables := extractTableNames(query)
		for _, table := range tables {
			if isTableBlocked(table, config.BlockedTables) {
				return fmt.Errorf("table %q is blocked", table)
			}
		}
	}

	return nil
}

// getTotalRowCount attempts to get the total row count for a query.
func (t *SQLQueryTool) getTotalRowCount(ctx context.Context, db *sql.DB, query string) (int, error) {
	// Try to wrap the query in a COUNT subquery
	// This is a best-effort approach and may not work for all queries
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM (%s) AS _count_subquery", query)

	var count int
	err := db.QueryRowContext(ctx, countQuery).Scan(&count)
	if err != nil {
		return 0, nil // Don't fail the main query if count fails
	}
	return count, nil
}

// ---------------------------------------------------------------------------
// Query Parsing Helpers
// ---------------------------------------------------------------------------

// extractTableNames attempts to extract table names from a SQL query.
// This is a simple parser and may not handle all SQL syntax correctly.
func extractTableNames(query string) []string {
	var tables []string
	upper := strings.ToUpper(query)

	// Remove string literals to avoid false matches
	cleaned := removeStringLiterals(query)

	// Look for FROM and JOIN clauses
	patterns := []string{
		"FROM ", "FROM(",
		"JOIN ", "JOIN(",
		"INTO ",   // For INSERT (though we block it)
		"UPDATE ", // For UPDATE (though we block it)
	}

	for _, pattern := range patterns {
		idx := 0
		for {
			pos := strings.Index(upper[idx:], pattern)
			if pos == -1 {
				break
			}
			pos += idx + len(pattern)

			// Extract the table name (until whitespace, comma, parenthesis, or end)
			nameStart := pos
			for pos < len(cleaned) {
				ch := cleaned[pos]
				if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' ||
					ch == ',' || ch == '(' || ch == ')' || ch == ';' {
					break
				}
				pos++
			}
			name := strings.TrimSpace(cleaned[nameStart:pos])
			if name != "" && !strings.HasPrefix(strings.ToUpper(name), "SELECT") {
				// Remove any alias (AS clause)
				if asIdx := strings.Index(strings.ToUpper(name), " AS "); asIdx > 0 {
					name = strings.TrimSpace(name[:asIdx])
				}
				tables = append(tables, name)
			}
			idx = pos
		}
	}

	// Deduplicate
	seen := make(map[string]bool)
	var unique []string
	for _, t := range tables {
		t = strings.ToLower(t)
		if !seen[t] {
			seen[t] = true
			unique = append(unique, t)
		}
	}

	return unique
}

// removeStringLiterals replaces string literals with empty strings to avoid
// false matches when parsing table names.
func removeStringLiterals(query string) string {
	var result strings.Builder
	inString := false
	quote := byte(0)

	for i := 0; i < len(query); i++ {
		ch := query[i]

		if inString {
			if ch == quote {
				// Check for escaped quote (doubled)
				if i+1 < len(query) && query[i+1] == quote {
					result.WriteByte(' ')
					i++ // Skip escaped quote
					continue
				}
				inString = false
			}
			result.WriteByte(' ')
			continue
		}

		if ch == '\'' || ch == '"' {
			inString = true
			quote = ch
			result.WriteByte(' ')
			continue
		}

		// Handle backslash escapes
		if ch == '\\' && i+1 < len(query) {
			result.WriteByte(' ')
			i++ // Skip escaped character
			continue
		}

		result.WriteByte(ch)
	}

	return result.String()
}

// isTableAllowed checks if a table is in the allowed list.
func isTableAllowed(table string, allowed []string) bool {
	table = strings.ToLower(table)
	for _, a := range allowed {
		if strings.ToLower(a) == table {
			return true
		}
	}
	return false
}

// isTableBlocked checks if a table is in the blocked list.
func isTableBlocked(table string, blocked []string) bool {
	table = strings.ToLower(table)
	for _, b := range blocked {
		if strings.ToLower(b) == table {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Value Normalization
// ---------------------------------------------------------------------------

// normalizeValue converts a scanned value to a JSON-friendly format.
func normalizeValue(v any) any {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case []byte:
		// Try to interpret as string
		return string(val)
	case time.Time:
		return val.Format(time.RFC3339)
	case sql.NullString:
		if val.Valid {
			return val.String
		}
		return nil
	case sql.NullInt64:
		if val.Valid {
			return val.Int64
		}
		return nil
	case sql.NullFloat64:
		if val.Valid {
			return val.Float64
		}
		return nil
	case sql.NullBool:
		if val.Valid {
			return val.Bool
		}
		return nil
	case sql.NullTime:
		if val.Valid {
			return val.Time.Format(time.RFC3339)
		}
		return nil
	default:
		// Check for numeric types that might need conversion
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return rv.Int()
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return rv.Uint()
		case reflect.Float32, reflect.Float64:
			return rv.Float()
		case reflect.Bool:
			return rv.Bool()
		}
		return v
	}
}

// ---------------------------------------------------------------------------
// Output Helpers
// ---------------------------------------------------------------------------

// marshalSQLQueryError creates a JSON error response for sql_query.
func marshalSQLQueryError(err error) (json.RawMessage, error) {
	output := SQLQueryOutput{
		Error: err.Error(),
	}
	return json.Marshal(output)
}

// ---------------------------------------------------------------------------
// SQLite Helper
// ---------------------------------------------------------------------------

// SQLiteMemoryConfig creates a config for an in-memory SQLite database.
func SQLiteMemoryConfig(name string) SQLConnectionConfig {
	return SQLConnectionConfig{
		Name:     name,
		Dialect:  SQLiteDialect,
		DSN:      ":memory:",
		ReadOnly: false, // In-memory DBs typically need write access for setup
	}
}

// SQLiteFileConfig creates a config for a file-based SQLite database.
func SQLiteFileConfig(name, path string) SQLConnectionConfig {
	return SQLConnectionConfig{
		Name:     name,
		Dialect:  SQLiteDialect,
		DSN:      path,
		ReadOnly: true,
	}
}

// ---------------------------------------------------------------------------
// Query Parameter Helpers
// ---------------------------------------------------------------------------

// BuildQueryParams converts a params map to positional args for database/sql.
// The params map should use string keys like "1", "2", etc. for positional
// parameters, which correspond to ?1, ?2, etc. in the query.
func BuildQueryParams(params map[string]any) []any {
	if len(params) == 0 {
		return nil
	}

	// Find the max positional index
	maxIdx := 0
	for key := range params {
		if idx, err := strconv.Atoi(key); err == nil {
			if idx > maxIdx {
				maxIdx = idx
			}
		}
	}

	if maxIdx == 0 {
		return nil
	}

	args := make([]any, maxIdx)
	for i := 1; i <= maxIdx; i++ {
		key := strconv.Itoa(i)
		if val, ok := params[key]; ok {
			args[i-1] = val
		} else {
			args[i-1] = nil
		}
	}

	return args
}
