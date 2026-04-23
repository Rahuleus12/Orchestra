// Package tool provides JSON Schema generation from Go types for tool definitions.
package tool

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// SchemaGenerator converts Go types to JSON Schema representations.
// It supports structs, slices, maps, pointers, and all basic types.
//
// Custom tags supported:
//   - `json` - field name, omitempty, "-"
//   - `description` - human-readable description for the schema
//   - `default` - default value as a string (parsed to appropriate type)
//   - `example` - example value as a string
//   - `enum` - comma-separated list of allowed values
//   - `min` / `max` - numeric bounds
//   - `minLength` / `maxLength` - string length bounds
//   - `pattern` - regex pattern for strings
//   - `format` - JSON Schema format (e.g., "date-time", "email", "uri")
type SchemaGenerator struct {
	// RequireAllFields makes all fields required by default, even if omitempty
	// is set. When false (default), fields with omitempty are optional.
	RequireAllFields bool

	// UseFieldNames uses the Go field names when json tag is missing.
	// When false (default), fields without json tags are omitted.
	UseFieldNames bool

	// IgnoreUnexported skips unexported fields. Default is true.
	IgnoreUnexported bool

	// AdditionalProperties controls whether objects allow additional properties.
	// nil means not specified (depends on context).
	AdditionalProperties *bool

	// visited tracks types we've already processed to handle recursive types.
	visited map[reflect.Type]bool
}

// NewSchemaGenerator creates a SchemaGenerator with default settings.
func NewSchemaGenerator() *SchemaGenerator {
	return &SchemaGenerator{
		IgnoreUnexported: true,
		visited:          make(map[reflect.Type]bool),
	}
}

// Generate creates a JSON Schema from a Go type using generics.
// The schema describes the shape of JSON that can unmarshal into T.
//
// Example:
//
//	type SearchInput struct {
//	    Query string `json:"query" description:"The search query"`
//	    Count int    `json:"count" description:"Number of results" default:"5"`
//	}
//
//	schema, err := GenerateSchema[SearchInput]()
func GenerateSchema[T any]() (json.RawMessage, error) {
	var zero T
	g := NewSchemaGenerator()
	schema := g.Generate(reflect.TypeOf(zero))
	return json.Marshal(schema)
}

// MustGenerateSchema is like GenerateSchema but panics on error.
func MustGenerateSchema[T any]() json.RawMessage {
	schema, err := GenerateSchema[T]()
	if err != nil {
		panic(fmt.Sprintf("GenerateSchema: %v", err))
	}
	return schema
}

// Generate creates a JSON Schema map from a Go reflect.Type.
// This is the core method that handles all type conversions.
func (g *SchemaGenerator) Generate(t reflect.Type) map[string]any {
	// Reset visited for a new generation
	g.visited = make(map[reflect.Type]bool)
	return g.typeToSchema(t)
}

// GenerateFromValue creates a JSON Schema map from a Go value.
// It uses the value's type to generate the schema.
func (g *SchemaGenerator) GenerateFromValue(v any) map[string]any {
	g.visited = make(map[reflect.Type]bool)
	if v == nil {
		return map[string]any{"type": "null"}
	}
	return g.typeToSchema(reflect.TypeOf(v))
}

// GenerateRaw creates a JSON Schema as raw JSON bytes from a Go type.
func (g *SchemaGenerator) GenerateRaw(t reflect.Type) (json.RawMessage, error) {
	schema := g.Generate(t)
	return json.Marshal(schema)
}

// typeToSchema converts a Go type to a JSON Schema.
func (g *SchemaGenerator) typeToSchema(t reflect.Type) map[string]any {
	// Handle indirect types (pointers)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Check for recursive types
	if g.visited[t] {
		return map[string]any{"type": "object"} // Placeholder for recursive types
	}

	switch t.Kind() {
	case reflect.Struct:
		// Special case for time.Time
		if isTimeType(t) {
			return map[string]any{
				"type":   "string",
				"format": "date-time",
			}
		}
		g.visited[t] = true
		schema := g.structToSchema(t)
		delete(g.visited, t)
		return schema

	case reflect.Slice, reflect.Array:
		return g.sliceToSchema(t)

	case reflect.Map:
		return g.mapToSchema(t)

	case reflect.String:
		return map[string]any{"type": "string"}

	case reflect.Bool:
		return map[string]any{"type": "boolean"}

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return map[string]any{"type": "integer"}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}

	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}

	case reflect.Interface:
		return map[string]any{} // Any type

	default:
		return map[string]any{"type": "string"}
	}
}

// structToSchema converts a Go struct to a JSON Schema object.
func (g *SchemaGenerator) structToSchema(t reflect.Type) map[string]any {
	properties := make(map[string]any)
	var required []string

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Skip unexported fields
		if g.IgnoreUnexported && !field.IsExported() {
			continue
		}

		// Get field name from json tag
		name, omitEmpty, skip := parseJSONTag(field.Tag.Get("json"))
		if skip {
			continue
		}
		if name == "" {
			if g.UseFieldNames {
				name = field.Name
			} else {
				continue
			}
		}

		// Build property schema
		propSchema := g.typeToSchema(field.Type)

		// Apply custom tags
		g.applyFieldTags(propSchema, field)

		properties[name] = propSchema

		// Determine if field is required
		// A field is required if:
		// - RequireAllFields is true, OR
		// - The field is not a pointer and doesn't have omitempty
		isPtr := field.Type.Kind() == reflect.Ptr
		if g.RequireAllFields || (!omitEmpty && !isPtr) {
			required = append(required, name)
		}
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}

	if len(required) > 0 {
		schema["required"] = required
	}

	if g.AdditionalProperties != nil {
		schema["additionalProperties"] = *g.AdditionalProperties
	}

	return schema
}

// sliceToSchema converts a slice/array type to a JSON Schema.
func (g *SchemaGenerator) sliceToSchema(t reflect.Type) map[string]any {
	elemType := t.Elem()

	// Handle []byte as base64-encoded string
	if elemType.Kind() == reflect.Uint8 {
		return map[string]any{
			"type":   "string",
			"format": "byte",
		}
	}

	items := g.typeToSchema(elemType)
	schema := map[string]any{
		"type":  "array",
		"items": items,
	}

	// Add min/max length from tags if this is called from a struct field context
	// (handled in applyFieldTags)

	return schema
}

// mapToSchema converts a map type to a JSON Schema.
func (g *SchemaGenerator) mapToSchema(t reflect.Type) map[string]any {
	// JSON only supports string keys
	valueSchema := g.typeToSchema(t.Elem())

	return map[string]any{
		"type":                 "object",
		"additionalProperties": valueSchema,
	}
}

// applyFieldTags applies custom tags to a property schema.
func (g *SchemaGenerator) applyFieldTags(schema map[string]any, field reflect.StructField) {
	// Description
	if desc, ok := field.Tag.Lookup("description"); ok {
		schema["description"] = desc
	}

	// Default value
	if def, ok := field.Tag.Lookup("default"); ok {
		if parsed := parseDefaultValue(def, field.Type); parsed != nil {
			schema["default"] = parsed
		}
	}

	// Example
	if ex, ok := field.Tag.Lookup("example"); ok {
		if parsed := parseDefaultValue(ex, field.Type); parsed != nil {
			schema["example"] = parsed
		}
	}

	// Enum values
	if enum, ok := field.Tag.Lookup("enum"); ok {
		values := parseEnumValues(enum, field.Type)
		if len(values) > 0 {
			schema["enum"] = values
		}
	}

	// Format
	if format, ok := field.Tag.Lookup("format"); ok {
		schema["format"] = format
	}

	// Numeric bounds
	if min, ok := field.Tag.Lookup("min"); ok {
		if v, err := strconv.ParseFloat(min, 64); err == nil {
			schema["minimum"] = v
		}
	}
	if max, ok := field.Tag.Lookup("max"); ok {
		if v, err := strconv.ParseFloat(max, 64); err == nil {
			schema["maximum"] = v
		}
	}

	// String length bounds
	if minLen, ok := field.Tag.Lookup("minLength"); ok {
		if v, err := strconv.Atoi(minLen); err == nil {
			schema["minLength"] = v
		}
	}
	if maxLen, ok := field.Tag.Lookup("maxLength"); ok {
		if v, err := strconv.Atoi(maxLen); err == nil {
			schema["maxLength"] = v
		}
	}

	// Pattern
	if pattern, ok := field.Tag.Lookup("pattern"); ok {
		schema["pattern"] = pattern
	}

	// Array-specific: minItems/maxItems
	if schema["type"] == "array" {
		if minItems, ok := field.Tag.Lookup("minItems"); ok {
			if v, err := strconv.Atoi(minItems); err == nil {
				schema["minItems"] = v
			}
		}
		if maxItems, ok := field.Tag.Lookup("maxItems"); ok {
			if v, err := strconv.Atoi(maxItems); err == nil {
				schema["maxItems"] = v
			}
		}
		// Unique items
		if unique, ok := field.Tag.Lookup("uniqueItems"); ok {
			if v, err := strconv.ParseBool(unique); err == nil && v {
				schema["uniqueItems"] = true
			}
		}
	}
}

// parseJSONTag parses a json tag value, returning (name, omitempty, skip).
func parseJSONTag(tag string) (name string, omitempty bool, skip bool) {
	if tag == "" || tag == "-" {
		return "", false, tag == "-"
	}

	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = ""
	}
	for _, part := range parts[1:] {
		if part == "omitempty" {
			omitempty = true
		}
	}

	return name, omitempty, false
}

// parseDefaultValue parses a default value string to the appropriate Go type.
func parseDefaultValue(s string, t reflect.Type) any {
	// Handle indirect types
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.String:
		return s

	case reflect.Bool:
		if v, err := strconv.ParseBool(s); err == nil {
			return v
		}
		return s

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			return v
		}
		return s

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if v, err := strconv.ParseUint(s, 10, 64); err == nil {
			return v
		}
		return s

	case reflect.Float32, reflect.Float64:
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			return v
		}
		return s

	case reflect.Slice:
		if s == "" {
			return []any{}
		}
		// Try to parse as JSON array
		var arr []any
		if err := json.Unmarshal([]byte(s), &arr); err == nil {
			return arr
		}
		return s

	case reflect.Map:
		if s == "" {
			return map[string]any{}
		}
		// Try to parse as JSON object
		var m map[string]any
		if err := json.Unmarshal([]byte(s), &m); err == nil {
			return m
		}
		return s

	default:
		// For complex types, try JSON
		var v any
		if err := json.Unmarshal([]byte(s), &v); err == nil {
			return v
		}
		return s
	}
}

// parseEnumValues parses a comma-separated enum tag into typed values.
func parseEnumValues(s string, t reflect.Type) []any {
	if s == "" {
		return nil
	}

	parts := strings.Split(s, ",")
	values := make([]any, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		values = append(values, parseDefaultValue(part, t))
	}

	return values
}

// isTimeType checks if a type is time.Time.
func isTimeType(t reflect.Type) bool {
	return t.PkgPath() == "time" && t.Name() == "Time"
}

// ---------------------------------------------------------------------------
// Convenience Functions
// ---------------------------------------------------------------------------

// SchemaForType returns a JSON Schema for the given type as a map.
// This is a shorthand for NewSchemaGenerator().Generate().
func SchemaForType(t reflect.Type) map[string]any {
	return NewSchemaGenerator().Generate(t)
}

// SchemaForValue returns a JSON Schema for the given value as a map.
func SchemaForValue(v any) map[string]any {
	return NewSchemaGenerator().GenerateFromValue(v)
}

// SchemaJSONForType returns a JSON Schema for the given type as raw JSON.
func SchemaJSONForType(t reflect.Type) (json.RawMessage, error) {
	g := NewSchemaGenerator()
	schema := g.Generate(t)
	return json.Marshal(schema)
}

// EmptyObjectSchema returns a schema for an object with no properties.
// This is useful for tools that take no parameters.
var EmptyObjectSchema = json.RawMessage(`{"type":"object","properties":{}}`)

// EmptySchema returns a schema with no constraints (accepts anything).
var EmptySchema = json.RawMessage(`{}`)
