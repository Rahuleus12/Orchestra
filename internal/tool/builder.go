// Package tool provides a declarative builder for creating tools.
//
// The builder supports both an untyped API (matching the PLAN.md example)
// and a type-safe generic API that catches input/output mismatches at
// compile time.
//
// # Untyped API
//
//	searchTool := tool.New("web_search",
//	    tool.WithDescription("Search the web for information"),
//	    tool.WithInputSchema[SearchInput](),
//	    tool.WithHandler(func(ctx context.Context, input SearchInput) (SearchOutput, error) {
//	        // implementation
//	    }),
//	)
//
// # Typed API (compile-time safe)
//
//	searchTool := tool.NewTyped[SearchInput]("web_search",
//	    tool.WithTypedDescription("Search the web for information"),
//	    tool.WithTypedHandler(func(ctx context.Context, input SearchInput) (SearchOutput, error) {
//	        // implementation
//	    }),
//	)
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
)

// ---------------------------------------------------------------------------
// Untyped Builder API
// ---------------------------------------------------------------------------

// BuilderOption configures a tool being built by New.
type BuilderOption func(*toolBuilder)

// WithDescription sets the tool's description. The description is sent to
// the LLM so it can decide when to call the tool.
func WithDescription(desc string) BuilderOption {
	return func(b *toolBuilder) {
		b.description = desc
	}
}

// WithInputSchema generates the tool's input JSON Schema from a Go type T.
// The schema is produced by the SchemaGenerator and supports all custom
// struct tags (description, default, enum, etc.).
//
// If WithHandler is also used, the handler's input type should match T.
// Mismatches are detected at runtime when the LLM calls the tool.
func WithInputSchema[T any]() BuilderOption {
	return func(b *toolBuilder) {
		schema, err := GenerateSchema[T]()
		if err != nil {
			b.buildErr = fmt.Errorf("generate input schema for %T: %w", (*T)(nil), err)
			return
		}
		b.schema = schema
		b.inputType = reflect.TypeOf((*T)(nil)).Elem()
	}
}

// WithRawSchema sets the tool's input schema from pre-built JSON.
// Use this when you need full control over the schema or want to
// avoid reflection-based generation.
func WithRawSchema(schema json.RawMessage) BuilderOption {
	return func(b *toolBuilder) {
		b.schema = schema
	}
}

// WithHandler sets the tool's execution function with typed input and output.
// The input is unmarshaled from the LLM's JSON arguments into I, and the
// output O is marshaled back to JSON for the tool result.
//
// This is the most common option for building tools.
func WithHandler[I, O any](handler func(ctx context.Context, input I) (O, error)) BuilderOption {
	return func(b *toolBuilder) {
		// Dereference pointer types for the input schema
		inputType := reflect.TypeOf((*I)(nil)).Elem()
		for inputType.Kind() == reflect.Ptr {
			inputType = inputType.Elem()
		}

		b.handler = func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var input I
			if len(raw) > 0 && string(raw) != "null" && string(raw) != "{}" {
				if err := json.Unmarshal(raw, &input); err != nil {
					return nil, fmt.Errorf("unmarshal input to %T: %w", input, err)
				}
			}
			output, err := handler(ctx, input)
			if err != nil {
				return nil, err
			}
			return json.Marshal(output)
		}
		if b.inputType == nil {
			b.inputType = inputType
		}
	}
}

// WithStringHandler sets the tool's execution function with typed input and
// a plain string output. The string is returned directly as the tool result
// content without JSON encoding (no wrapping quotes).
//
// Use this for tools that return human-readable text, which is the majority
// of tool use cases.
func WithStringHandler[I any](handler func(ctx context.Context, input I) (string, error)) BuilderOption {
	return func(b *toolBuilder) {
		inputType := reflect.TypeOf((*I)(nil)).Elem()
		for inputType.Kind() == reflect.Ptr {
			inputType = inputType.Elem()
		}

		b.handler = func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var input I
			if len(raw) > 0 && string(raw) != "null" && string(raw) != "{}" {
				if err := json.Unmarshal(raw, &input); err != nil {
					return nil, fmt.Errorf("unmarshal input to %T: %w", input, err)
				}
			}
			result, err := handler(ctx, input)
			if err != nil {
				return nil, err
			}
			return json.RawMessage(result), nil
		}
		if b.inputType == nil {
			b.inputType = inputType
		}
	}
}

// WithRawHandler sets the tool's execution function with raw JSON input and
// output. Use this when you need full control over serialization or when
// working with dynamically-typed data.
func WithRawHandler(handler func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)) BuilderOption {
	return func(b *toolBuilder) {
		b.handler = handler
	}
}

// WithNoArgsHandler sets the tool's execution function for tools that take
// no arguments. The handler receives only the context.
func WithNoArgsHandler(handler func(ctx context.Context) (string, error)) BuilderOption {
	return func(b *toolBuilder) {
		b.handler = func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
			result, err := handler(ctx)
			if err != nil {
				return nil, err
			}
			return json.RawMessage(result), nil
		}
		b.schema = EmptyObjectSchema
	}
}

// WithNoArgsJSONHandler sets the tool's execution function for tools that
// take no arguments but return structured JSON output.
func WithNoArgsJSONHandler[O any](handler func(ctx context.Context) (O, error)) BuilderOption {
	return func(b *toolBuilder) {
		b.handler = func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
			output, err := handler(ctx)
			if err != nil {
				return nil, err
			}
			return json.Marshal(output)
		}
		b.schema = EmptyObjectSchema
	}
}

// WithToolMiddleware adds middleware that wraps the built tool.
// Middleware is applied in reverse order (last middleware wraps first).
func WithToolMiddleware(mw ToolMiddleware) BuilderOption {
	return func(b *toolBuilder) {
		b.middlewares = append(b.middlewares, mw)
	}
}

// ---------------------------------------------------------------------------
// Internal builder state (untyped)
// ---------------------------------------------------------------------------

type toolBuilder struct {
	name        string
	description string
	schema      json.RawMessage
	handler     func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)
	inputType   reflect.Type
	middlewares []ToolMiddleware
	buildErr    error
}

// build constructs the Tool from the accumulated options.
func (b *toolBuilder) build() (Tool, error) {
	if b.buildErr != nil {
		return nil, b.buildErr
	}
	if b.name == "" {
		return nil, fmt.Errorf("tool name must not be empty")
	}
	if b.handler == nil {
		return nil, fmt.Errorf("tool %q: no handler specified; use WithHandler, WithStringHandler, or WithRawHandler", b.name)
	}

	// Auto-generate schema from input type if not explicitly set
	if len(b.schema) == 0 && b.inputType != nil {
		g := NewSchemaGenerator()
		schema, err := g.GenerateRaw(b.inputType)
		if err != nil {
			return nil, fmt.Errorf("tool %q: auto-generate schema: %w", b.name, err)
		}
		b.schema = schema
	}
	if len(b.schema) == 0 {
		b.schema = EmptyObjectSchema
	}

	var t Tool = &builtTool{
		name:        b.name,
		description: b.description,
		schema:      b.schema,
		handler:     b.handler,
	}

	// Apply middleware in reverse order so the first middleware in the
	// option list is the outermost wrapper.
	for i := len(b.middlewares) - 1; i >= 0; i-- {
		t = b.middlewares[i](t)
	}

	return t, nil
}

// New creates a new Tool using the declarative builder pattern.
//
// Returns an error if the tool cannot be built (missing handler, invalid
// schema, etc.). Use MustNew for a panicking variant.
func New(name string, opts ...BuilderOption) (Tool, error) {
	b := &toolBuilder{name: name}
	for _, opt := range opts {
		opt(b)
	}
	return b.build()
}

// MustNew is like New but panics on error. Useful for tool registration
// at init time where failures should be fatal.
func MustNew(name string, opts ...BuilderOption) Tool {
	t, err := New(name, opts...)
	if err != nil {
		panic(fmt.Sprintf("tool.MustNew(%q): %v", name, err))
	}
	return t
}

// ---------------------------------------------------------------------------
// Typed Builder API (compile-time safe)
// ---------------------------------------------------------------------------

// TypedBuilderOption configures a typed tool builder.
type TypedBuilderOption[I any] func(*typedBuilder[I])

// WithTypedDescription sets the description for a typed builder.
func WithTypedDescription[I any](desc string) TypedBuilderOption[I] {
	return func(b *typedBuilder[I]) {
		b.description = desc
	}
}

// WithTypedSchema sets the input schema explicitly for a typed builder.
// If not set, the schema is auto-generated from I.
func WithTypedSchema[I any](schema json.RawMessage) TypedBuilderOption[I] {
	return func(b *typedBuilder[I]) {
		b.schema = schema
	}
}

// WithTypedHandler sets the execution function for a typed builder.
// The output is marshaled to JSON.
func WithTypedHandler[I, O any](handler func(ctx context.Context, input I) (O, error)) TypedBuilderOption[I] {
	return func(b *typedBuilder[I]) {
		b.handler = func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var input I
			if len(raw) > 0 && string(raw) != "null" && string(raw) != "{}" {
				if err := json.Unmarshal(raw, &input); err != nil {
					return nil, fmt.Errorf("unmarshal input to %T: %w", input, err)
				}
			}
			output, err := handler(ctx, input)
			if err != nil {
				return nil, err
			}
			return json.Marshal(output)
		}
	}
}

// WithTypedStringHandler sets the execution function for a typed builder
// that returns a plain string (not JSON-encoded).
func WithTypedStringHandler[I any](handler func(ctx context.Context, input I) (string, error)) TypedBuilderOption[I] {
	return func(b *typedBuilder[I]) {
		b.handler = func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var input I
			if len(raw) > 0 && string(raw) != "null" && string(raw) != "{}" {
				if err := json.Unmarshal(raw, &input); err != nil {
					return nil, fmt.Errorf("unmarshal input to %T: %w", input, err)
				}
			}
			result, err := handler(ctx, input)
			if err != nil {
				return nil, err
			}
			return json.RawMessage(result), nil
		}
	}
}

// WithTypedRawHandler sets a raw JSON handler for a typed builder.
// This bypasses input unmarshaling but still uses I for schema generation.
func WithTypedRawHandler[I any](handler func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)) TypedBuilderOption[I] {
	return func(b *typedBuilder[I]) {
		b.handler = handler
	}
}

// WithTypedMiddleware adds middleware to a typed builder.
func WithTypedMiddleware[I any](mw ToolMiddleware) TypedBuilderOption[I] {
	return func(b *typedBuilder[I]) {
		b.middlewares = append(b.middlewares, mw)
	}
}

// typedBuilder holds the state for a type-safe tool build.
type typedBuilder[I any] struct {
	name        string
	description string
	schema      json.RawMessage
	handler     func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)
	middlewares []ToolMiddleware
	buildErr    error
}

// build constructs the Tool from the typed builder's state.
func (b *typedBuilder[I]) build() (Tool, error) {
	if b.buildErr != nil {
		return nil, b.buildErr
	}
	if b.name == "" {
		return nil, fmt.Errorf("tool name must not be empty")
	}
	if b.handler == nil {
		return nil, fmt.Errorf("tool %q: no handler specified", b.name)
	}

	// Auto-generate schema from I if not set
	if len(b.schema) == 0 {
		schema, err := GenerateSchema[I]()
		if err != nil {
			return nil, fmt.Errorf("tool %q: auto-generate schema: %w", b.name, err)
		}
		b.schema = schema
	}
	if len(b.schema) == 0 {
		b.schema = EmptyObjectSchema
	}

	var t Tool = &builtTool{
		name:        b.name,
		description: b.description,
		schema:      b.schema,
		handler:     b.handler,
	}

	for i := len(b.middlewares) - 1; i >= 0; i-- {
		t = b.middlewares[i](t)
	}

	return t, nil
}

// NewTyped creates a new Tool with a compile-time enforced input type I.
// The schema is auto-generated from I unless WithTypedSchema is used.
//
// This is the preferred API when you want the compiler to verify that
// your handler's input type matches the schema.
func NewTyped[I any](name string, opts ...TypedBuilderOption[I]) (Tool, error) {
	b := &typedBuilder[I]{name: name}
	for _, opt := range opts {
		opt(b)
	}
	return b.build()
}

// MustTyped is like NewTyped but panics on error.
func MustTyped[I any](name string, opts ...TypedBuilderOption[I]) Tool {
	t, err := NewTyped[I](name, opts...)
	if err != nil {
		panic(fmt.Sprintf("tool.MustTyped[%T](%q): %v", (*I)(nil), name, err))
	}
	return t
}

// ---------------------------------------------------------------------------
// Built Tool (concrete Tool implementation)
// ---------------------------------------------------------------------------

// builtTool is the concrete Tool produced by the builder. It stores all
// configuration at construction time and delegates execution to a closure.
type builtTool struct {
	name        string
	description string
	schema      json.RawMessage
	handler     func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)
	mu          sync.RWMutex // for potential future mutable state
}

// Name returns the tool's identifier.
func (t *builtTool) Name() string { return t.name }

// Description returns the tool's description.
func (t *builtTool) Description() string { return t.description }

// Parameters returns the tool's input JSON Schema.
func (t *builtTool) Parameters() json.RawMessage {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.schema
}

// Execute runs the tool's handler with the given JSON input.
func (t *builtTool) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	// Default empty input to empty object
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	return t.handler(ctx, input)
}

// Compile-time interface check.
var _ Tool = (*builtTool)(nil)
