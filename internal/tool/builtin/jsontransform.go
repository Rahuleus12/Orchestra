// Package builtin provides built-in tools for the Orchestra tool system.
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// JSON Transform Tool
// ---------------------------------------------------------------------------

// JSONTransformInput defines the input for the json_transform tool.
type JSONTransformInput struct {
	// Input is the JSON string to transform.
	Input string `json:"input" description:"The JSON string to transform"`

	// Query is the transformation query (jq-like syntax).
	// Examples:
	//   - ".key" - extract a key
	//   - ".key.subkey" - nested key access
	//   - ".[0]" - array index
	//   - ".[0:3]" - array slice
	//   - ".[]" - iterate array
	//   - ".[] | .key" - pipe: iterate then extract
	//   - ".[] | select(.key == \"value\")" - filter
	//   - "{name: .first, age: .years}" - object construction
	//   - "keys" - get object keys
	//   - "values" - get object values
	//   - "length" - get length
	//   - "type" - get type name
	//   - "sort" - sort array
	//   - "unique" - unique values in array
	//   - "flatten" - flatten nested arrays
	//   - "reverse" - reverse array
	//   - "min" - minimum value in array
	//   - "max" - maximum value in array
	//   - "sum" - sum numbers in array
	//   - "avg" - average of numbers in array
	//   - "to_entries" - convert object to [{key:k, value:v}]
	//   - "from_entries" - convert [{key:k, value:v}] to object
	//   - "group_by(.key)" - group array by key
	//   - "sort_by(.key)" - sort array by key
	//   - "map(.key)" - extract key from each element
	//   - "add" - sum/concat arrays or objects
	Query string `json:"query" description:"Transformation query (jq-like syntax)"`

	// Indent controls the indentation of the output JSON.
	// Empty string means compact (no indentation).
	Indent string `json:"indent,omitempty" description:"Output indentation (e.g., '  ', '\\t'), empty for compact"`

	// RawOutput returns raw values (strings without quotes, numbers as-is)
	// instead of JSON-encoded output. Useful for extracting single values.
	RawOutput bool `json:"raw_output,omitempty" description:"Return raw value without JSON encoding" default:"false"`
}

// JSONTransformOutput defines the output of the json_transform tool.
type JSONTransformOutput struct {
	// Result is the transformed JSON output.
	Result string `json:"result"`

	// Type is the type of the result: "object", "array", "string", "number", "boolean", "null".
	Type string `json:"type"`

	// Length is the length of the result (objects/arrays: count, strings: chars, null: 0).
	Length int `json:"length,omitempty"`

	// Error contains an error message if the transformation failed.
	Error string `json:"error,omitempty"`
}

// JSONTransformTool implements the json_transform built-in tool.
// It performs jq-like JSON transformations including path extraction,
// filtering, mapping, sorting, and aggregation.
//
// The query language supports a subset of jq syntax that covers the most
// common JSON manipulation use cases:
//   - Path navigation: .key, .key.subkey, .["key with spaces"]
//   - Array operations: .[n], .[start:end], .[]
//   - Pipe chains: .key | .subkey | .value
//   - Filtering: .[] | select(.key == "value")
//   - Object construction: {newKey: .oldKey}
//   - Built-in functions: keys, values, length, type, sort, etc.
type JSONTransformTool struct {
	// MaxInputSize is the maximum input JSON size in bytes. Defaults to 1MB.
	MaxInputSize int64

	// MaxOutputSize is the maximum output size in bytes. Defaults to 1MB.
	MaxOutputSize int64

	// MaxIterations limits the number of iterations for .[] operations.
	// Prevents runaway expansion on large arrays.
	MaxIterations int
}

// NewJSONTransformTool creates a json_transform tool with default settings.
func NewJSONTransformTool() JSONTransformTool {
	return JSONTransformTool{
		MaxInputSize:  1 * 1024 * 1024,
		MaxOutputSize: 1 * 1024 * 1024,
		MaxIterations: 10000,
	}
}

// Name returns the tool's identifier.
func (t JSONTransformTool) Name() string { return "json_transform" }

// Description returns the tool's description for the LLM.
func (t JSONTransformTool) Description() string {
	return `Transform and query JSON data using jq-like syntax.

This tool applies transformations to JSON input and returns the result.
It supports a subset of jq syntax for common JSON manipulation tasks.

Path access:
- ".key" - extract a key
- ".key.subkey" - nested access
- ".[\"key with spaces\"]" - quoted key access
- ".[0]" - array index (0-based)
- ".[-1]" - negative index (from end)
- ".[0:3]" - array slice [start:end)
- ".[] " - iterate all array elements

Pipe chains:
- ".items | .[] | .name" - navigate then iterate then extract
- ".data | .[].value" - shorthand for iteration in path

Filtering:
- ".[] | select(.active == true)" - filter by condition
- ".[] | select(.age > 18)" - numeric comparison
- ".[] | select(.name | test(\"^A\"))" - regex filter

Object construction:
- "{name: .first, age: .years}" - create new object
- "{(.key): .value}" - dynamic key from value

Built-in functions:
- keys, values, length, type - inspection
- sort, reverse, unique, flatten - array manipulation
- min, max, sum, avg, add - aggregation
- to_entries, from_entries - object/array conversion
- group_by(.key), sort_by(.key) - grouping/sorting
- map(.key), map_values(.key) - transformation
- first, last, nth(n) - element access
- has("key") - key existence check
- test("regex") - regex matching
- ascii_downcase, ascii_upcase - string case
- split("."), join(",") - string manipulation
- tonumber, tostring - type conversion
- not - boolean negation
- empty - produce no output
- null - produce null
- path(.key) - get path expression

Examples:
- Extract: .users[0].name
- Filter: .items[] | select(.price < 10)
- Transform: .items | map(.name)
- Aggregate: .items | length
- Group: .items | group_by(.category)`
}

// Parameters returns the JSON Schema for the tool's input.
func (t JSONTransformTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"input": {
				"type": "string",
				"description": "The JSON string to transform"
			},
			"query": {
				"type": "string",
				"description": "Transformation query (jq-like syntax)"
			},
			"indent": {
				"type": "string",
				"description": "Output indentation (e.g., '  ', '\\t'), empty for compact"
			},
			"raw_output": {
				"type": "boolean",
				"description": "Return raw value without JSON encoding",
				"default": false
			}
		},
		""required"": ["input", "query"]
	}`)
}

// Execute performs the JSON transformation.
func (t JSONTransformTool) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req JSONTransformInput
	if err := json.Unmarshal(input, &req); err != nil {
		return marshalJSONTransformError(fmt.Errorf("parse input: %w", err))
	}

	if req.Input == "" {
		return marshalJSONTransformError(fmt.Errorf("input JSON is required"))
	}
	if req.Query == "" {
		return marshalJSONTransformError(fmt.Errorf("query is required"))
	}

	// Check context
	if ctx.Err() != nil {
		return marshalJSONTransformError(ctx.Err())
	}

	// Check input size
	maxInput := t.MaxInputSize
	if maxInput <= 0 {
		maxInput = 1 * 1024 * 1024
	}
	if int64(len(req.Input)) > maxInput {
		return marshalJSONTransformError(fmt.Errorf("input too large: %d bytes (max %d)", len(req.Input), maxInput))
	}

	// Parse input JSON
	var data any
	if err := json.Unmarshal([]byte(req.Input), &data); err != nil {
		return marshalJSONTransformError(fmt.Errorf("parse JSON input: %w", err))
	}

	// Parse and execute the query
	maxIter := t.MaxIterations
	if maxIter <= 0 {
		maxIter = 10000
	}

	jq := newJQ(req.Query, maxIter)
	results, err := jq.Execute(data)
	if err != nil {
		return marshalJSONTransformError(err)
	}

	// If no results, return null
	if len(results) == 0 {
		return marshalJSONTransformResult(nil, req.RawOutput, req.Indent)
	}

	// If single result, return it directly
	if len(results) == 1 {
		return marshalJSONTransformResult(results[0], req.RawOutput, req.Indent)
	}

	// Multiple results - return as array
	return marshalJSONTransformResult(results, req.RawOutput, req.Indent)
}

// marshalJSONTransformResult converts a result value to the output format.
func marshalJSONTransformResult(value any, rawOutput bool, indent string) (json.RawMessage, error) {
	output := JSONTransformOutput{
		Type: jsonType(value),
	}

	switch v := value.(type) {
	case nil:
		output.Result = "null"
	case string:
		output.Length = len(v)
		if rawOutput {
			output.Result = v
		} else {
			data, _ := json.Marshal(v)
			output.Result = string(data)
		}
	case []any:
		output.Length = len(v)
		data, _ := jsonMarshalIndent(v, indent)
		output.Result = string(data)
	case map[string]any:
		output.Length = len(v)
		data, _ := jsonMarshalIndent(v, indent)
		output.Result = string(data)
	case float64:
		output.Length = 1
		if rawOutput {
			output.Result = formatFloat(v)
		} else {
			data, _ := json.Marshal(v)
			output.Result = string(data)
		}
	case bool:
		output.Length = 1
		if rawOutput {
			if v {
				output.Result = "true"
			} else {
				output.Result = "false"
			}
		} else {
			data, _ := json.Marshal(v)
			output.Result = string(data)
		}
	default:
		data, _ := jsonMarshalIndent(v, indent)
		output.Result = string(data)
	}

	return json.Marshal(output)
}

// marshalJSONTransformError creates a JSON error response.
func marshalJSONTransformError(err error) (json.RawMessage, error) {
	output := JSONTransformOutput{
		Error: err.Error(),
	}
	return json.Marshal(output)
}

// ---------------------------------------------------------------------------
// JQ Implementation (subset)
// ---------------------------------------------------------------------------

// jq is a simple jq-like query executor.
type jq struct {
	query     string
	pos       int
	maxIter   int
	iteration int
}

// newJQ creates a new jq executor.
func newJQ(query string, maxIter int) *jq {
	return &jq{
		query:   strings.TrimSpace(query),
		maxIter: maxIter,
	}
}

// Execute runs the query against the input data and returns all results.
func (j *jq) Execute(data any) ([]any, error) {
	j.pos = 0
	j.iteration = 0
	return j.parsePipeExpr(data)
}

// parsePipeExpr handles pipe expressions: expr | expr | ...
func (j *jq) parsePipeExpr(data any) ([]any, error) {
	results := []any{data}

	for j.pos < len(j.query) {
		// Skip whitespace
		j.skipWhitespace()

		if j.pos >= len(j.query) {
			break
		}

		// Check for pipe
		if j.peek() == '|' {
			j.advance()
			j.skipWhitespace()
			continue
		}

		// Parse next expression
		var nextResults []any

		for _, r := range results {
			j.iteration++
			if j.iteration > j.maxIter {
				return nil, fmt.Errorf("iteration limit exceeded (%d)", j.maxIter)
			}
			partial, e := j.parseExpr(r)
			if e != nil {
				return nil, e
			}
			nextResults = append(nextResults, partial...)
		}

		if len(nextResults) == 0 {
			return nil, nil
		}
		results = nextResults
	}

	return results, nil
}

// parseExpr parses a single expression.
func (j *jq) parseExpr(data any) ([]any, error) {
	j.skipWhitespace()

	if j.pos >= len(j.query) {
		return []any{data}, nil
	}

	ch := j.peek()

	switch ch {
	case '.':
		return j.parseDotExpr(data)
	case '[':
		return j.parseArrayExpr(data)
	case '{':
		return j.parseObjectConstruct(data)
	default:
		// Try to parse a function name
		return j.parseFunction(data)
	}
}

// parseDotExpr handles .key, .key.subkey, .[], .["key"]
func (j *jq) parseDotExpr(data any) ([]any, error) {
	if j.peek() != '.' {
		return []any{data}, nil
	}
	j.advance() // consume '.'

	// Check for [] iterator
	if j.peek() == '[' {
		return j.parseArrayExpr(data)
	}

	// Check for just "." (identity)
	if j.pos >= len(j.query) || isWhitespace(j.peek()) || j.peek() == '|' {
		return []any{data}, nil
	}

	// Parse key path
	return j.parseKeyPath(data)
}

// parseKeyPath handles .key.subkey and .["key with spaces"]
func (j *jq) parseKeyPath(data any) ([]any, error) {
	current := []any{data}

	for j.pos < len(j.query) {
		ch := j.peek()

		// Stop at pipe, whitespace followed by pipe, end
		if ch == '|' {
			break
		}
		if isWhitespace(ch) {
			// Look ahead for pipe
			nextPos := j.pos + 1
			for nextPos < len(j.query) && isWhitespace(j.query[nextPos]) {
				nextPos++
			}
			if nextPos >= len(j.query) || j.query[nextPos] == '|' {
				break
			}
			// Not a pipe, might be part of key (shouldn't be, but handle gracefully)
			break
		}

		// Handle .["key"] syntax
		if ch == '[' && j.pos+1 < len(j.query) && j.query[j.pos+1] == '"' {
			j.advance() // consume '['
			key, err := j.parseQuotedString()
			if err != nil {
				return nil, fmt.Errorf("parse quoted key: %w", err)
			}
			if j.peek() != ']' {
				return nil, fmt.Errorf("expected ']' after quoted key at position %d", j.pos)
			}
			j.advance() // consume ']'

			var next []any
			for _, c := range current {
				result := accessKey(c, key)
				if result != nil {
					next = append(next, result)
				}
			}
			current = next
			continue
		}

		// Handle regular key
		if !isIdentChar(ch) {
			break
		}

		key := j.parseIdent()
		if key == "" {
			break
		}

		var next []any
		for _, c := range current {
			result := accessKey(c, key)
			if result != nil {
				next = append(next, result)
			}
		}
		current = next
	}

	return current, nil
}

// parseArrayExpr handles .[n], .[start:end], .[]
func (j *jq) parseArrayExpr(data any) ([]any, error) {
	if j.peek() != '[' {
		return nil, fmt.Errorf("expected '[' at position %d", j.pos)
	}
	j.advance() // consume '['

	j.skipWhitespace()

	// Check for empty iterator .[]
	if j.peek() == ']' {
		j.advance() // consume ']'
		return j.iterateArray(data)
	}

	// Parse index or slice
	var results []any

	// Check for negative index
	negIdx := false
	if j.peek() == '-' {
		negIdx = true
		j.advance()
	}

	// Try to parse as number
	startStr := j.parseNumber()
	if startStr != "" {
		start, err := strconv.Atoi(startStr)
		if err != nil {
			return nil, fmt.Errorf("parse array index: %w", err)
		}
		if negIdx {
			start = -start
		}

		j.skipWhitespace()

		// Check for slice :end
		if j.peek() == ':' {
			j.advance() // consume ':'
			j.skipWhitespace()

			negEnd := false
			if j.peek() == '-' {
				negEnd = true
				j.advance()
			}

			endStr := j.parseNumber()
			end := 0
			if endStr != "" {
				end, err = strconv.Atoi(endStr)
				if err != nil {
					return nil, fmt.Errorf("parse slice end: %w", err)
				}
				if negEnd {
					end = -end
				}
			}

			j.skipWhitespace()
			if j.peek() != ']' {
				return nil, fmt.Errorf("expected ']' at position %d", j.pos)
			}
			j.advance() // consume ']'

			return j.sliceArray(data, start, end)
		}

		// Single index
		j.skipWhitespace()
		if j.peek() != ']' {
			return nil, fmt.Errorf("expected ']' at position %d", j.pos)
		}
		j.advance() // consume ']'

		result := accessIndex(data, start)
		if result != nil {
			results = append(results, result)
		}
		return results, nil
	}

	// Handle .[] iterator (already handled above, but just in case)
	if j.peek() == ']' {
		j.advance()
		return j.iterateArray(data)
	}

	return nil, fmt.Errorf("invalid array expression at position %d", j.pos)
}

// parseObjectConstruct handles {key: .path, ...}
func (j *jq) parseObjectConstruct(data any) ([]any, error) {
	if j.peek() != '{' {
		return nil, fmt.Errorf("expected '{' at position %d", j.pos)
	}
	j.advance() // consume '{'

	result := make(map[string]any)

	for {
		j.skipWhitespace()

		if j.peek() == '}' {
			j.advance() // consume '}'
			break
		}

		// Parse key (possibly dynamic with parentheses)
		var key string
		if j.peek() == '(' {
			j.advance() // consume '('
			// Dynamic key - evaluate expression
			keyResults, err := j.parseExpr(data)
			if err != nil {
				return nil, fmt.Errorf("parse dynamic key: %w", err)
			}
			if len(keyResults) == 0 || keyResults[0] == nil {
				return nil, fmt.Errorf("dynamic key produced no value at position %d", j.pos)
			}
			switch v := keyResults[0].(type) {
			case string:
				key = v
			case float64:
				key = formatFloat(v)
			default:
				key = fmt.Sprintf("%v", v)
			}
			if j.peek() != ')' {
				return nil, fmt.Errorf("expected ')' at position %d", j.pos)
			}
			j.advance() // consume ')'
		} else if j.peek() == '"' {
			var err error
			key, err = j.parseQuotedString()
			if err != nil {
				return nil, fmt.Errorf("parse object key: %w", err)
			}
		} else {
			key = j.parseIdent()
			if key == "" {
				return nil, fmt.Errorf("expected object key at position %d", j.pos)
			}
		}

		j.skipWhitespace()

		// Expect ':'
		if j.peek() != ':' {
			return nil, fmt.Errorf("expected ':' after key %q at position %d", key, j.pos)
		}
		j.advance() // consume ':'

		j.skipWhitespace()

		// Parse value expression
		valueResults, err := j.parseExpr(data)
		if err != nil {
			return nil, fmt.Errorf("parse object value: %w", err)
		}

		if len(valueResults) > 0 {
			result[key] = valueResults[0]
		}

		j.skipWhitespace()

		// Optional comma
		if j.peek() == ',' {
			j.advance()
		}
	}

	return []any{result}, nil
}

// parseFunction handles built-in functions.
func (j *jq) parseFunction(data any) ([]any, error) {
	name := j.parseIdent()
	if name == "" {
		return nil, fmt.Errorf("unexpected character %q at position %d", string(j.peek()), j.pos)
	}

	j.skipWhitespace()

	// Check for function arguments
	var args []string
	if j.peek() == '(' {
		j.advance() // consume '('
		for {
			j.skipWhitespace()
			if j.peek() == ')' {
				j.advance() // consume ')'
				break
			}
			// Parse argument - could be a string, number, or path expression
			arg := j.parseFunctionArg()
			args = append(args, arg)
			j.skipWhitespace()
			if j.peek() == ',' {
				j.advance()
			}
		}
	}

	return j.callFunction(name, args, data)
}

// parseFunctionArg parses a function argument.
func (j *jq) parseFunctionArg() string {
	j.skipWhitespace()

	if j.peek() == '"' {
		s, _ := j.parseQuotedString()
		return s
	}

	// Read until , or )
	var arg strings.Builder
	depth := 0
	for j.pos < len(j.query) {
		ch := j.peek()
		if ch == '(' {
			depth++
		} else if ch == ')' {
			if depth == 0 {
				break
			}
			depth--
		} else if ch == ',' && depth == 0 {
			break
		}
		arg.WriteByte(ch)
		j.advance()
	}
	return strings.TrimSpace(arg.String())
}

// callFunction executes a built-in function.
func (j *jq) callFunction(name string, args []string, data any) ([]any, error) {
	switch name {
	case "keys":
		return j.fnKeys(data)
	case "values":
		return j.fnValues(data)
	case "length":
		return j.fnLength(data)
	case "type":
		return []any{jsonType(data)}, nil
	case "sort":
		return j.fnSort(data)
	case "reverse":
		return j.fnReverse(data)
	case "unique":
		return j.fnUnique(data)
	case "flatten":
		return j.fnFlatten(data)
	case "min":
		return j.fnMin(data)
	case "max":
		return j.fnMax(data)
	case "sum":
		return j.fnSum(data)
	case "avg":
		return j.fnAvg(data)
	case "add":
		return j.fnAdd(data)
	case "to_entries":
		return j.fnToEntries(data)
	case "from_entries":
		return j.fnFromEntries(data)
	case "first":
		return j.fnFirst(data)
	case "last":
		return j.fnLast(data)
	case "nth":
		return j.fnNth(args, data)
	case "has":
		return j.fnHas(args, data)
	case "map":
		return j.fnMap(args, data)
	case "map_values":
		return j.fnMapValues(args, data)
	case "select":
		return j.fnSelect(args, data)
	case "group_by":
		return j.fnGroupBy(args, data)
	case "sort_by":
		return j.fnSortBy(args, data)
	case "unique_by":
		return j.fnUniqueBy(args, data)
	case "test":
		return j.fnTest(args, data)
	case "match":
		return j.fnMatch(args, data)
	case "capture":
		return j.fnCapture(args, data)
	case "split":
		return j.fnSplit(args, data)
	case "join":
		return j.fnJoin(args, data)
	case "ascii_downcase":
		return j.fnAsciiDowncase(data)
	case "ascii_upcase":
		return j.fnAsciiUpcase(data)
	case "tostring":
		return j.fnToString(data)
	case "tonumber":
		return j.fnToNumber(data)
	case "not":
		return j.fnNot(data)
	case "null":
		return []any{nil}, nil
	case "empty":
		return nil, nil
	case "path":
		return j.fnPath(args, data)
	case "contains":
		return j.fnContains(args, data)
	case "inside":
		return j.fnInside(args, data)
	case "startswith":
		return j.fnStartsWith(args, data)
	case "endswith":
		return j.fnEndsWith(args, data)
	case "ltrimstr":
		return j.fnLtrimstr(args, data)
	case "rtrimstr":
		return j.fnRtrimstr(args, data)
	case "trimstr":
		return j.fnTrimstr(args, data)
	case "indices":
		return j.fnIndices(args, data)
	case "recurse":
		return j.fnRecurse(data)
	case "tojson":
		data, _ := json.Marshal(data)
		return []any{string(data)}, nil
	case "fromjson":
		return j.fnFromJson(data)
	case "input":
		return []any{data}, nil
	case "debug":
		return []any{data}, nil
	case "env":
		return []any{""}, nil // Not implemented - would need os.Getenv
	default:
		return nil, fmt.Errorf("unknown function: %s", name)
	}
}

// ---------------------------------------------------------------------------
// Function Implementations
// ---------------------------------------------------------------------------

func (j *jq) fnKeys(data any) ([]any, error) {
	m, ok := data.(map[string]any)
	if !ok {
		return nil, nil
	}
	keys := make([]any, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return []any{keys}, nil
}

func (j *jq) fnValues(data any) ([]any, error) {
	m, ok := data.(map[string]any)
	if !ok {
		return nil, nil
	}
	values := make([]any, 0, len(m))
	for _, v := range m {
		values = append(values, v)
	}
	return []any{values}, nil
}

func (j *jq) fnLength(data any) ([]any, error) {
	var length int
	switch v := data.(type) {
	case nil:
		length = 0
	case string:
		length = len(v)
	case []any:
		length = len(v)
	case map[string]any:
		length = len(v)
	case float64:
		length = int(v)
	case bool:
		if v {
			length = 1
		}
	default:
		length = 0
	}
	return []any{float64(length)}, nil
}

func (j *jq) fnSort(data any) ([]any, error) {
	arr, ok := data.([]any)
	if !ok {
		return []any{data}, nil
	}
	sorted := make([]any, len(arr))
	copy(sorted, arr)
	sortAny(sorted)
	return []any{sorted}, nil
}

func (j *jq) fnReverse(data any) ([]any, error) {
	arr, ok := data.([]any)
	if !ok {
		return []any{data}, nil
	}
	reversed := make([]any, len(arr))
	for i, v := range arr {
		reversed[len(arr)-1-i] = v
	}
	return []any{reversed}, nil
}

func (j *jq) fnUnique(data any) ([]any, error) {
	arr, ok := data.([]any)
	if !ok {
		return []any{data}, nil
	}
	seen := make(map[string]bool)
	var unique []any
	for _, v := range arr {
		key := fmt.Sprintf("%v", v)
		if !seen[key] {
			seen[key] = true
			unique = append(unique, v)
		}
	}
	return []any{unique}, nil
}

func (j *jq) fnFlatten(data any) ([]any, error) {
	arr, ok := data.([]any)
	if !ok {
		return []any{data}, nil
	}
	var result []any
	flatten(arr, &result)
	return []any{result}, nil
}

func flatten(arr []any, result *[]any) {
	for _, v := range arr {
		sub, ok := v.([]any)
		if ok {
			flatten(sub, result)
		} else {
			*result = append(*result, v)
		}
	}
}

func (j *jq) fnMin(data any) ([]any, error) {
	arr, ok := data.([]any)
	if !ok || len(arr) == 0 {
		return []any{nil}, nil
	}
	min := arr[0]
	for _, v := range arr[1:] {
		if compareAny(v, min) < 0 {
			min = v
		}
	}
	return []any{min}, nil
}

func (j *jq) fnMax(data any) ([]any, error) {
	arr, ok := data.([]any)
	if !ok || len(arr) == 0 {
		return []any{nil}, nil
	}
	max := arr[0]
	for _, v := range arr[1:] {
		if compareAny(v, max) > 0 {
			max = v
		}
	}
	return []any{max}, nil
}

func (j *jq) fnSum(data any) ([]any, error) {
	arr, ok := data.([]any)
	if !ok || len(arr) == 0 {
		return []any{float64(0)}, nil
	}
	var sum float64
	for _, v := range arr {
		n, ok := toFloat64(v)
		if !ok {
			return []any{nil}, nil
		}
		sum += n
	}
	return []any{sum}, nil
}

func (j *jq) fnAvg(data any) ([]any, error) {
	arr, ok := data.([]any)
	if !ok || len(arr) == 0 {
		return []any{nil}, nil
	}
	var sum float64
	for _, v := range arr {
		n, ok := toFloat64(v)
		if !ok {
			return []any{nil}, nil
		}
		sum += n
	}
	return []any{sum / float64(len(arr))}, nil
}

func (j *jq) fnAdd(data any) ([]any, error) {
	switch v := data.(type) {
	case nil:
		return []any{nil}, nil
	case []any:
		if len(v) == 0 {
			return []any{nil}, nil
		}
		// Check if all elements are numbers
		allNumbers := true
		for _, elem := range v {
			if _, ok := elem.(float64); !ok {
				allNumbers = false
				break
			}
		}
		if allNumbers {
			return j.fnSum(v)
		}
		// Otherwise concatenate arrays
		var result []any
		for _, elem := range v {
			if sub, ok := elem.([]any); ok {
				result = append(result, sub...)
			} else {
				result = append(result, elem)
			}
		}
		return []any{result}, nil
	case map[string]any:
		// Merge objects
		result := make(map[string]any)
		result["0"] = v
		return []any{result}, nil
	default:
		return []any{data}, nil
	}
}

func (j *jq) fnToEntries(data any) ([]any, error) {
	m, ok := data.(map[string]any)
	if !ok {
		return nil, nil
	}
	entries := make([]any, 0, len(m))
	for k, v := range m {
		entry := map[string]any{"key": k, "value": v}
		entries = append(entries, entry)
	}
	return []any{entries}, nil
}

func (j *jq) fnFromEntries(data any) ([]any, error) {
	arr, ok := data.([]any)
	if !ok {
		return nil, nil
	}
	result := make(map[string]any)
	for _, v := range arr {
		entry, ok := v.(map[string]any)
		if !ok {
			continue
		}
		key, _ := entry["key"].(string)
		result[key] = entry["value"]
	}
	return []any{result}, nil
}

func (j *jq) fnFirst(data any) ([]any, error) {
	arr, ok := data.([]any)
	if !ok || len(arr) == 0 {
		return []any{nil}, nil
	}
	return []any{arr[0]}, nil
}

func (j *jq) fnLast(data any) ([]any, error) {
	arr, ok := data.([]any)
	if !ok || len(arr) == 0 {
		return []any{nil}, nil
	}
	return []any{arr[len(arr)-1]}, nil
}

func (j *jq) fnNth(args []string, data any) ([]any, error) {
	arr, ok := data.([]any)
	if !ok || len(arr) == 0 {
		return []any{nil}, nil
	}
	n := 0
	if len(args) > 0 {
		n, _ = strconv.Atoi(strings.TrimSpace(args[0]))
	}
	if n < 0 {
		n = len(arr) + n
	}
	if n < 0 || n >= len(arr) {
		return []any{nil}, nil
	}
	return []any{arr[n]}, nil
}

func (j *jq) fnHas(args []string, data any) ([]any, error) {
	if len(args) == 0 {
		return []any{false}, nil
	}
	key := strings.TrimSpace(args[0])
	// Remove quotes if present
	key = strings.Trim(key, "\"")
	m, ok := data.(map[string]any)
	if !ok {
		return []any{false}, nil
	}
	_, exists := m[key]
	return []any{exists}, nil
}

func (j *jq) fnMap(args []string, data any) ([]any, error) {
	arr, ok := data.([]any)
	if !ok {
		return nil, nil
	}
	if len(args) == 0 {
		return nil, nil
	}

	pathExpr := strings.TrimSpace(args[0])
	var results []any
	for _, item := range arr {
		subJQ := newJQ(pathExpr, j.maxIter-j.iteration)
		subResults, err := subJQ.Execute(item)
		if err != nil {
			continue
		}
		if len(subResults) > 0 {
			results = append(results, subResults[0])
		}
	}
	return []any{results}, nil
}

func (j *jq) fnMapValues(args []string, data any) ([]any, error) {
	m, ok := data.(map[string]any)
	if !ok {
		return nil, nil
	}
	if len(args) == 0 {
		return nil, nil
	}

	pathExpr := strings.TrimSpace(args[0])
	result := make(map[string]any)
	for k, v := range m {
		subJQ := newJQ(pathExpr, j.maxIter-j.iteration)
		subResults, err := subJQ.Execute(v)
		if err != nil {
			result[k] = v
			continue
		}
		if len(subResults) > 0 {
			result[k] = subResults[0]
		}
	}
	return []any{result}, nil
}

func (j *jq) fnSelect(args []string, data any) ([]any, error) {
	if len(args) == 0 {
		return []any{data}, nil
	}

	// Parse the condition
	condition := strings.TrimSpace(args[0])

	// Simple comparisons: .key == "value", .key != "value", .key > 10, .key < 10, .key >= 10, .key <= 10
	if matches, value, op, err := parseComparison(condition); err == nil {
		actual := accessKey(data, matches)
		if actual == nil {
			return nil, nil
		}
		if evaluateComparison(actual, op, value) {
			return []any{data}, nil
		}
		return nil, nil
	}

	// Check for boolean expression
	if condition == "true" {
		return []any{data}, nil
	}
	if condition == "false" {
		return nil, nil
	}

	// Try to evaluate as a path that should return truthy
	subJQ := newJQ(condition, j.maxIter-j.iteration)
	results, err := subJQ.Execute(data)
	if err != nil {
		return nil, nil
	}
	if len(results) > 0 && isTruthy(results[0]) {
		return []any{data}, nil
	}
	return nil, nil
}

func (j *jq) fnGroupBy(args []string, data any) ([]any, error) {
	arr, ok := data.([]any)
	if !ok {
		return nil, nil
	}
	if len(args) == 0 {
		return nil, nil
	}

	pathExpr := strings.TrimSpace(args[0])
	groups := make(map[string][]any)
	var order []string

	for _, item := range arr {
		subJQ := newJQ(pathExpr, j.maxIter-j.iteration)
		results, err := subJQ.Execute(item)
		if err != nil || len(results) == 0 {
			continue
		}
		key := fmt.Sprintf("%v", results[0])
		if _, exists := groups[key]; !exists {
			order = append(order, key)
		}
		groups[key] = append(groups[key], item)
	}

	result := make([]any, 0, len(groups))
	for _, key := range order {
		entry := map[string]any{"key": key, "value": groups[key]}
		result = append(result, entry)
	}
	return []any{result}, nil
}

func (j *jq) fnSortBy(args []string, data any) ([]any, error) {
	arr, ok := data.([]any)
	if !ok {
		return nil, nil
	}
	if len(args) == 0 {
		return nil, nil
	}

	pathExpr := strings.TrimSpace(args[0])
	sorted := make([]any, len(arr))
	copy(sorted, arr)

	sortAnyFunc(sorted, func(a, b any) bool {
		subJQ := newJQ(pathExpr, j.maxIter-j.iteration)
		aResults, _ := subJQ.Execute(a)
		bResults, _ := subJQ.Execute(b)

		var aVal, bVal any
		if len(aResults) > 0 {
			aVal = aResults[0]
		}
		if len(bResults) > 0 {
			bVal = bResults[0]
		}

		return compareAny(aVal, bVal) < 0
	})

	return []any{sorted}, nil
}

func (j *jq) fnUniqueBy(args []string, data any) ([]any, error) {
	arr, ok := data.([]any)
	if !ok {
		return nil, nil
	}
	if len(args) == 0 {
		return j.fnUnique(data)
	}

	pathExpr := strings.TrimSpace(args[0])
	seen := make(map[string]bool)
	var result []any

	for _, item := range arr {
		subJQ := newJQ(pathExpr, j.maxIter-j.iteration)
		results, _ := subJQ.Execute(item)

		var key string
		if len(results) > 0 {
			key = fmt.Sprintf("%v", results[0])
		}

		if !seen[key] {
			seen[key] = true
			result = append(result, item)
		}
	}

	return []any{result}, nil
}

func (j *jq) fnTest(args []string, data any) ([]any, error) {
	if len(args) == 0 {
		return []any{false}, nil
	}

	str, ok := data.(string)
	if !ok {
		return []any{false}, nil
	}

	pattern := strings.Trim(args[0], "\"")
	matched := simpleMatch(pattern, str)
	return []any{matched}, nil
}

func (j *jq) fnMatch(args []string, data any) ([]any, error) {
	if len(args) == 0 {
		return []any{nil}, nil
	}

	str, ok := data.(string)
	if !ok {
		return []any{nil}, nil
	}

	pattern := strings.Trim(args[0], "\"")
	captures := simpleMatchCapture(pattern, str)
	if captures == nil {
		return nil, nil
	}

	result := map[string]any{
		"offset":   captures.offset,
		"length":   captures.length,
		"string":   captures.match,
		"captures": captures.captures,
	}
	return []any{result}, nil
}

func (j *jq) fnCapture(args []string, data any) ([]any, error) {
	if len(args) == 0 {
		return []any{nil}, nil
	}

	str, ok := data.(string)
	if !ok {
		return []any{nil}, nil
	}

	pattern := strings.Trim(args[0], "\"")
	captures := simpleMatchCapture(pattern, str)
	if captures == nil {
		return nil, nil
	}

	return []any{captures.captures}, nil
}

func (j *jq) fnSplit(args []string, data any) ([]any, error) {
	if len(args) == 0 {
		return []any{data}, nil
	}

	str, ok := data.(string)
	if !ok {
		return []any{data}, nil
	}

	sep := strings.Trim(args[0], "\"")
	parts := strings.Split(str, sep)
	result := make([]any, len(parts))
	for i, p := range parts {
		result[i] = p
	}
	return []any{result}, nil
}

func (j *jq) fnJoin(args []string, data any) ([]any, error) {
	if len(args) == 0 {
		return []any{data}, nil
	}

	arr, ok := data.([]any)
	if !ok {
		return []any{data}, nil
	}

	sep := strings.Trim(args[0], "\"")
	parts := make([]string, len(arr))
	for i, v := range arr {
		parts[i] = fmt.Sprintf("%v", v)
	}
	return []any{strings.Join(parts, sep)}, nil
}

func (j *jq) fnAsciiDowncase(data any) ([]any, error) {
	str, ok := data.(string)
	if !ok {
		return []any{data}, nil
	}
	return []any{strings.ToLower(str)}, nil
}

func (j *jq) fnAsciiUpcase(data any) ([]any, error) {
	str, ok := data.(string)
	if !ok {
		return []any{data}, nil
	}
	return []any{strings.ToUpper(str)}, nil
}

func (j *jq) fnToString(data any) ([]any, error) {
	switch v := data.(type) {
	case string:
		return []any{v}, nil
	case nil:
		return []any{"null"}, nil
	case float64:
		return []any{formatFloat(v)}, nil
	case bool:
		return []any{fmt.Sprintf("%t", v)}, nil
	default:
		b, _ := json.Marshal(v)
		return []any{string(b)}, nil
	}
}

func (j *jq) fnToNumber(data any) ([]any, error) {
	switch v := data.(type) {
	case float64:
		return []any{v}, nil
	case string:
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return []any{nil}, nil
		}
		return []any{n}, nil
	case bool:
		if v {
			return []any{float64(1)}, nil
		}
		return []any{float64(0)}, nil
	case nil:
		return []any{float64(0)}, nil
	default:
		return []any{nil}, nil
	}
}

func (j *jq) fnNot(data any) ([]any, error) {
	return []any{!isTruthy(data)}, nil
}

func (j *jq) fnPath(args []string, data any) ([]any, error) {
	if len(args) == 0 {
		return []any{[]any{}}, nil
	}
	// Simplified: return the path expression as a string
	pathExpr := strings.TrimSpace(args[0])
	return []any{pathExpr}, nil
}

func (j *jq) fnContains(args []string, data any) ([]any, error) {
	if len(args) == 0 {
		return []any{false}, nil
	}
	// Simple containment check
	other := parseJSONValue(strings.TrimSpace(args[0]))
	return []any{containsValue(data, other)}, nil
}

func (j *jq) fnInside(args []string, data any) ([]any, error) {
	if len(args) == 0 {
		return []any{false}, nil
	}
	other := parseJSONValue(strings.TrimSpace(args[0]))
	return []any{containsValue(other, data)}, nil
}

func (j *jq) fnStartsWith(args []string, data any) ([]any, error) {
	str, ok := data.(string)
	if !ok || len(args) == 0 {
		return []any{false}, nil
	}
	prefix := strings.Trim(args[0], "\"")
	return []any{strings.HasPrefix(str, prefix)}, nil
}

func (j *jq) fnEndsWith(args []string, data any) ([]any, error) {
	str, ok := data.(string)
	if !ok || len(args) == 0 {
		return []any{false}, nil
	}
	suffix := strings.Trim(args[0], "\"")
	return []any{strings.HasSuffix(str, suffix)}, nil
}

func (j *jq) fnLtrimstr(args []string, data any) ([]any, error) {
	str, ok := data.(string)
	if !ok || len(args) == 0 {
		return []any{data}, nil
	}
	prefix := strings.Trim(args[0], "\"")
	return []any{strings.TrimPrefix(str, prefix)}, nil
}

func (j *jq) fnRtrimstr(args []string, data any) ([]any, error) {
	str, ok := data.(string)
	if !ok || len(args) == 0 {
		return []any{data}, nil
	}
	suffix := strings.Trim(args[0], "\"")
	return []any{strings.TrimSuffix(str, suffix)}, nil
}

func (j *jq) fnTrimstr(args []string, data any) ([]any, error) {
	str, ok := data.(string)
	if !ok || len(args) == 0 {
		return []any{data}, nil
	}
	cutset := strings.Trim(args[0], "\"")
	return []any{strings.Trim(str, cutset)}, nil
}

func (j *jq) fnIndices(args []string, data any) ([]any, error) {
	str, ok := data.(string)
	if !ok || len(args) == 0 {
		return []any{[]any{}}, nil
	}
	substr := strings.Trim(args[0], "\"")
	var indices []any
	start := 0
	for {
		idx := strings.Index(str[start:], substr)
		if idx == -1 {
			break
		}
		indices = append(indices, float64(start+idx))
		start += idx + 1
	}
	return []any{indices}, nil
}

func (j *jq) fnRecurse(data any) ([]any, error) {
	var result []any
	collectRecursive(data, &result, 0, 100)
	return []any{result}, nil
}

func collectRecursive(data any, result *[]any, depth, maxDepth int) {
	if depth > maxDepth {
		return
	}
	*result = append(*result, data)
	switch v := data.(type) {
	case map[string]any:
		for _, val := range v {
			collectRecursive(val, result, depth+1, maxDepth)
		}
	case []any:
		for _, val := range v {
			collectRecursive(val, result, depth+1, maxDepth)
		}
	}
}

func (j *jq) fnFromJson(data any) ([]any, error) {
	str, ok := data.(string)
	if !ok {
		return nil, nil
	}
	var result any
	if err := json.Unmarshal([]byte(str), &result); err != nil {
		return nil, nil
	}
	return []any{result}, nil
}

// ---------------------------------------------------------------------------
// Array Operations
// ---------------------------------------------------------------------------

func (j *jq) iterateArray(data any) ([]any, error) {
	arr, ok := data.([]any)
	if !ok {
		return nil, nil
	}
	results := make([]any, len(arr))
	copy(results, arr)
	return results, nil
}

func (j *jq) sliceArray(data any, start, end int) ([]any, error) {
	arr, ok := data.([]any)
	if !ok {
		return nil, nil
	}
	length := len(arr)

	// Handle negative indices
	if start < 0 {
		start = length + start
	}
	if end <= 0 {
		end = length + end
	}

	// Clamp
	if start < 0 {
		start = 0
	}
	if end > length {
		end = length
	}
	if start >= end {
		return nil, nil
	}

	result := make([]any, end-start)
	copy(result, arr[start:end])
	return result, nil
}

// ---------------------------------------------------------------------------
// Data Access
// ---------------------------------------------------------------------------

func accessKey(data any, key string) any {
	m, ok := data.(map[string]any)
	if !ok {
		return nil
	}
	return m[key]
}

func accessIndex(data any, index int) any {
	arr, ok := data.([]any)
	if !ok {
		return nil
	}
	length := len(arr)
	if index < 0 {
		index = length + index
	}
	if index < 0 || index >= length {
		return nil
	}
	return arr[index]
}

// ---------------------------------------------------------------------------
// Parser Helpers
// ---------------------------------------------------------------------------

func (j *jq) peek() byte {
	if j.pos >= len(j.query) {
		return 0
	}
	return j.query[j.pos]
}

func (j *jq) advance() {
	if j.pos < len(j.query) {
		j.pos++
	}
}

func (j *jq) skipWhitespace() {
	for j.pos < len(j.query) && isWhitespace(j.query[j.pos]) {
		j.pos++
	}
}

func (j *jq) parseIdent() string {
	start := j.pos
	for j.pos < len(j.query) && isIdentChar(j.query[j.pos]) {
		j.pos++
	}
	return j.query[start:j.pos]
}

func (j *jq) parseNumber() string {
	start := j.pos
	if j.pos < len(j.query) && (j.query[j.pos] == '-' || j.query[j.pos] == '+') {
		j.pos++
	}
	for j.pos < len(j.query) && (j.query[j.pos] >= '0' && j.query[j.pos] <= '9') {
		j.pos++
	}
	if j.pos < len(j.query) && j.query[j.pos] == '.' {
		j.pos++
		for j.pos < len(j.query) && (j.query[j.pos] >= '0' && j.query[j.pos] <= '9') {
			j.pos++
		}
	}
	return j.query[start:j.pos]
}

func (j *jq) parseQuotedString() (string, error) {
	if j.peek() != '"' {
		return "", fmt.Errorf("expected '\"' at position %d", j.pos)
	}
	j.advance() // consume opening quote

	var result strings.Builder
	escaped := false

	for j.pos < len(j.query) {
		ch := j.query[j.pos]
		j.advance()

		if escaped {
			switch ch {
			case 'n':
				result.WriteByte('\n')
			case 't':
				result.WriteByte('\t')
			case 'r':
				result.WriteByte('\r')
			case '\\':
				result.WriteByte('\\')
			case '"':
				result.WriteByte('"')
			default:
				result.WriteByte(ch)
			}
			escaped = false
			continue
		}

		if ch == '\\' {
			escaped = true
			continue
		}

		if ch == '"' {
			return result.String(), nil
		}

		result.WriteByte(ch)
	}

	return "", fmt.Errorf("unterminated string at position %d", j.pos)
}

func isIdentChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-'
}

func isWhitespace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

// ---------------------------------------------------------------------------
// Comparison Helpers
// ---------------------------------------------------------------------------

type matchCapture struct {
	offset   float64
	length   float64
	match    string
	captures []any
}

func parseComparison(expr string) (string, string, string, error) {
	// Try patterns: .key op value
	ops := []string{"==", "!=", ">=", "<=", ">", "<"}
	for _, op := range ops {
		idx := strings.Index(expr, " "+op+" ")
		if idx == -1 {
			idx = strings.Index(expr, op+" ")
		}
		if idx == -1 {
			continue
		}

		left := strings.TrimSpace(expr[:idx])
		rightStart := idx + len(op)
		// Skip space
		for rightStart < len(expr) && expr[rightStart] == ' ' {
			rightStart++
		}
		right := strings.TrimSpace(expr[rightStart:])

		// Remove leading dot from left
		left = strings.TrimPrefix(left, ".")
		right = strings.Trim(right, "\"")

		return left, right, op, nil
	}
	return "", "", "", fmt.Errorf("not a comparison")
}

func evaluateComparison(actual any, op, expected string) bool {
	actualStr := fmt.Sprintf("%v", actual)

	switch op {
	case "==":
		return actualStr == expected
	case "!=":
		return actualStr != expected
	case ">":
		return compareAsNumbers(actualStr, expected) > 0
	case "<":
		return compareAsNumbers(actualStr, expected) < 0
	case ">=":
		return compareAsNumbers(actualStr, expected) >= 0
	case "<=":
		return compareAsNumbers(actualStr, expected) <= 0
	default:
		return false
	}
}

func compareAsNumbers(a, b string) int {
	aNum, aErr := strconv.ParseFloat(a, 64)
	bNum, bErr := strconv.ParseFloat(b, 64)
	if aErr != nil || bErr != nil {
		// Fall back to string comparison
		if a < b {
			return -1
		} else if a > b {
			return 1
		}
		return 0
	}
	if aNum < bNum {
		return -1
	} else if aNum > bNum {
		return 1
	}
	return 0
}

func isTruthy(v any) bool {
	switch val := v.(type) {
	case nil:
		return false
	case bool:
		return val
	case float64:
		return val != 0
	case string:
		return val != ""
	case []any:
		return len(val) > 0
	case map[string]any:
		return len(val) > 0
	default:
		return v != nil
	}
}

// ---------------------------------------------------------------------------
// Simple Regex Matching (subset - no full regex)
// ---------------------------------------------------------------------------

func simpleMatch(pattern, str string) bool {
	// Very simple matching: exact, prefix*, *suffix, *contains*
	if pattern == str {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(str, pattern[:len(pattern)-1])
	}
	if strings.HasPrefix(pattern, "*") {
		return strings.HasSuffix(str, pattern[1:])
	}
	if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") {
		return strings.Contains(str, pattern[1:len(pattern)-1])
	}
	// Treat as literal substring for simplicity
	return strings.Contains(str, pattern)
}

func simpleMatchCapture(pattern, str string) *matchCapture {
	idx := strings.Index(str, pattern)
	if idx == -1 {
		return nil
	}
	return &matchCapture{
		offset: float64(idx),
		length: float64(len(pattern)),
		match:  str[idx : idx+len(pattern)],
	}
}

// ---------------------------------------------------------------------------
// Sorting Helpers
// ---------------------------------------------------------------------------

func sortAny(arr []any) {
	sortAnyFunc(arr, func(a, b any) bool { return compareAny(a, b) < 0 })
}

func sortAnyFunc(arr []any, less func(a, b any) bool) {
	n := len(arr)
	for i := 0; i < n-1; i++ {
		for j := i + 1; j < n; j++ {
			if less(arr[j], arr[i]) {
				arr[i], arr[j] = arr[j], arr[i]
			}
		}
	}
}

func compareAny(a, b any) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -1
	}
	if b == nil {
		return 1
	}

	aStr := fmt.Sprintf("%v", a)
	bStr := fmt.Sprintf("%v", b)

	// Try numeric comparison
	aNum, aErr := strconv.ParseFloat(aStr, 64)
	bNum, bErr := strconv.ParseFloat(bStr, 64)
	if aErr == nil && bErr == nil {
		if aNum < bNum {
			return -1
		} else if aNum > bNum {
			return 1
		}
		return 0
	}

	// String comparison
	if aStr < bStr {
		return -1
	} else if aStr > bStr {
		return 1
	}
	return 0
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

// ---------------------------------------------------------------------------
// Type Helpers
// ---------------------------------------------------------------------------

func jsonType(v any) string {
	if v == nil {
		return "null"
	}
	switch v.(type) {
	case float64:
		return "number"
	case string:
		return "string"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "null"
	}
}

func formatFloat(f float64) string {
	if f == float64(int(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func jsonMarshalIndent(v any, indent string) ([]byte, error) {
	if indent == "" {
		return json.Marshal(v)
	}
	return json.MarshalIndent(v, "", indent)
}

func parseJSONValue(s string) any {
	s = strings.TrimSpace(s)
	if s == "null" {
		return nil
	}
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}
	if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return n
	}
	var v any
	if json.Unmarshal([]byte(s), &v) == nil {
		return v
	}
	return s
}

func containsValue(container, value any) bool {
	switch c := container.(type) {
	case map[string]any:
		for _, v := range c {
			if fmt.Sprintf("%v", v) == fmt.Sprintf("%v", value) {
				return true
			}
		}
	case []any:
		for _, v := range c {
			if fmt.Sprintf("%v", v) == fmt.Sprintf("%v", value) {
				return true
			}
		}
	case string:
		if s, ok := value.(string); ok {
			return strings.Contains(c, s)
		}
	}
	return false
}
