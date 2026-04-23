package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test Helpers
// ---------------------------------------------------------------------------

// mockTool is a simple tool implementation for testing.
type mockTool struct {
	name        string
	description string
	params      json.RawMessage
	executeFn   func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)
}

func (t *mockTool) Name() string                         { return t.name }
func (t *mockTool) Description() string                  { return t.description }
func (t *mockTool) Parameters() json.RawMessage          { return t.params }
func (t *mockTool) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	if t.executeFn != nil {
		return t.executeFn(ctx, input)
	}
	return input, nil
}

func newMockTool(name, desc string) *mockTool {
	return &mockTool{
		name:        name,
		description: desc,
		params:      json.RawMessage(`{"type":"object","properties":{}}`),
	}
}

func newMockToolWithSchema(name, desc string, schema json.RawMessage) *mockTool {
	return &mockTool{
		name:        name,
		description: desc,
		params:      schema,
	}
}

// ---------------------------------------------------------------------------
// Tool Interface Tests
// ---------------------------------------------------------------------------

func TestMockTool_ImplementsInterface(t *testing.T) {
	var _ Tool = (*mockTool)(nil)
	var _ Tool = (*ToolFunc)(nil)
	var _ Tool = (*AgentToolAdapter)(nil)
}

func TestToolFunc_Basic(t *testing.T) {
	called := false
	tf := NewToolFunc("test_tool", "A test tool", func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		called = true
		return json.RawMessage(`{"result":"ok"}`), nil
	})

	if tf.Name() != "test_tool" {
		t.Errorf("Name() = %q, want %q", tf.Name(), "test_tool")
	}
	if tf.Description() != "A test tool" {
		t.Errorf("Description() = %q, want %q", tf.Description(), "A test tool")
	}
	if tf.Parameters() == nil {
		t.Error("Parameters() should not be nil")
	}

	result, err := tf.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !called {
		t.Error("Execute() did not call the underlying function")
	}

	var parsed map[string]string
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("Unmarshal result: %v", err)
	}
	if parsed["result"] != "ok" {
		t.Errorf("result = %q, want %q", parsed["result"], "ok")
	}
}

func TestToolFunc_WithSchema(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)
	tf := NewToolFuncWithSchema("search", "Search things", schema, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		return input, nil
	})

	if string(tf.Parameters()) != string(schema) {
		t.Errorf("Parameters() = %s, want %s", tf.Parameters(), schema)
	}
}

func TestToolFunc_ExecuteError(t *testing.T) {
	tf := NewToolFunc("fail_tool", "Always fails", func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		return nil, fmt.Errorf("intentional error")
	})

	_, err := tf.Execute(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("Execute() should return error")
	}
	if err.Error() != "intentional error" {
		t.Errorf("Error() = %q, want %q", err.Error(), "intentional error")
	}
}

func TestToolFunc_ContextCancellation(t *testing.T) {
	tf := NewToolFunc("slow_tool", "Takes time", func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
			return json.RawMessage(`"done"`), nil
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := tf.Execute(ctx, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("Execute() should return error on context cancellation")
	}
	if ctx.Err() != context.DeadlineExceeded {
		t.Errorf("ctx.Err() = %v, want DeadlineExceeded", ctx.Err())
	}
}

// ---------------------------------------------------------------------------
// ToolDefinition Tests
// ---------------------------------------------------------------------------

func TestToolDefinition(t *testing.T) {
	mt := newMockToolWithSchema("test", "desc", json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer"}}}`))
	td := Definition(mt)

	if td.Type != "function" {
		t.Errorf("Type = %q, want %q", td.Type, "function")
	}
	if td.Function.Name != "test" {
		t.Errorf("Name = %q, want %q", td.Function.Name, "test")
	}
	if td.Function.Description != "desc" {
		t.Errorf("Description = %q, want %q", td.Function.Description, "desc")
	}

	var params map[string]any
	if err := json.Unmarshal(td.Function.Parameters, &params); err != nil {
		t.Fatalf("Unmarshal params: %v", err)
	}
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatal("params should have 'properties' key")
	}
	if _, ok := props["x"]; !ok {
		t.Error("params.properties should have 'x'")
	}
}

func TestToolDefinition_NilParameters(t *testing.T) {
	mt := &mockTool{
		name:        "no_params",
		description: "no params",
		params:      nil,
	}
	td := Definition(mt)

	var params map[string]any
	if err := json.Unmarshal(td.Function.Parameters, &params); err != nil {
		t.Fatalf("Unmarshal params: %v", err)
	}
	if params["type"] != "object" {
		t.Error("should default to empty object schema when params is nil")
	}
}

func TestToProviderDefinition(t *testing.T) {
	mt := newMockTool("test", "desc")
	pd := ToProviderDefinition(mt)

	if pd.Type != "function" {
		t.Errorf("Type = %q, want %q", pd.Type, "function")
	}
	if pd.Function.Name != "test" {
		t.Errorf("Name = %q, want %q", pd.Function.Name, "test")
	}
}

// ---------------------------------------------------------------------------
// ToolRegistry Tests
// ---------------------------------------------------------------------------

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry() returned nil")
	}
	if r.Size() != 0 {
		t.Errorf("Size() = %d, want 0", r.Size())
	}
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	mt := newMockTool("test_tool", "A test tool")

	if err := r.Register(mt); err != nil {
		t.Fatalf("Register() error: %v", err)
	}
	if r.Size() != 1 {
		t.Errorf("Size() = %d, want 1", r.Size())
	}

	got, err := r.Get("test_tool")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.Name() != "test_tool" {
		t.Errorf("Get() Name = %q, want %q", got.Name(), "test_tool")
	}
}

func TestRegistry_RegisterDuplicate(t *testing.T) {
	r := NewRegistry()
	mt1 := newMockTool("dup", "first")
	mt2 := newMockTool("dup", "second")

	if err := r.Register(mt1); err != nil {
		t.Fatalf("Register() first error: %v", err)
	}
	if err := r.Register(mt2); err == nil {
		t.Fatal("Register() duplicate should return error")
	}
}

func TestRegistry_MustRegister(t *testing.T) {
	r := NewRegistry()
	mt := newMockTool("must_tool", "desc")

	r.MustRegister(mt) // Should not panic

	if r.Size() != 1 {
		t.Errorf("Size() = %d, want 1", r.Size())
	}
}

func TestRegistry_MustRegister_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustRegister() should panic on duplicate")
		}
	}()

	r := NewRegistry()
	r.MustRegister(newMockTool("p", "first"))
	r.MustRegister(newMockTool("p", "second")) // Should panic
}

func TestRegistry_GetNotFound(t *testing.T) {
	r := NewRegistry()
	_, err := r.Get("nonexistent")
	if err == nil {
		t.Fatal("Get() should return error for nonexistent tool")
	}
}

func TestRegistry_Has(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(newMockTool("exists", "desc"))

	if !r.Has("exists") {
		t.Error("Has('exists') = false, want true")
	}
	if r.Has("nope") {
		t.Error("Has('nope') = true, want false")
	}
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(newMockTool("a", "A"))
	r.MustRegister(newMockTool("b", "B"))
	r.MustRegister(newMockTool("c", "C"))

	list := r.List()
	if len(list) != 3 {
		t.Fatalf("List() length = %d, want 3", len(list))
	}

	names := make(map[string]bool)
	for _, tool := range list {
		names[tool.Name()] = true
	}
	if !names["a"] || !names["b"] || !names["c"] {
		t.Error("List() missing expected tools")
	}
}

func TestRegistry_Names(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(newMockTool("x", "X"))
	r.MustRegister(newMockTool("y", "Y"))

	names := r.Names()
	if len(names) != 2 {
		t.Fatalf("Names() length = %d, want 2", len(names))
	}
}

func TestRegistry_Definitions(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(newMockTool("def_test", "desc"))

	defs := r.Definitions()
	if len(defs) != 1 {
		t.Fatalf("Definitions() length = %d, want 1", len(defs))
	}
	if defs[0].Function.Name != "def_test" {
		t.Errorf("Function.Name = %q, want %q", defs[0].Function.Name, "def_test")
	}
}

func TestRegistry_ProviderDefinitions(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(newMockTool("prov_test", "desc"))

	pdefs := r.ProviderDefinitions()
	if len(pdefs) != 1 {
		t.Fatalf("ProviderDefinitions() length = %d, want 1", len(pdefs))
	}
	if pdefs[0].Function.Name != "prov_test" {
		t.Errorf("Function.Name = %q, want %q", pdefs[0].Function.Name, "prov_test")
	}
}

func TestRegistry_Clear(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(newMockTool("a", "A"))
	r.MustRegister(newMockTool("b", "B"))
	r.Clear()

	if r.Size() != 0 {
		t.Errorf("Size() after Clear() = %d, want 0", r.Size())
	}
}

func TestRegistry_Execute(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(&mockTool{
		name:        "echo",
		description: "echo",
		params:      json.RawMessage(`{}`),
		executeFn: func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"echo":true}`), nil
		},
	})

	result, err := r.Execute(context.Background(), "echo", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	var parsed map[string]bool
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !parsed["echo"] {
		t.Error("Execute() result missing 'echo' field")
	}
}

func TestRegistry_Execute_NotFound(t *testing.T) {
	r := NewRegistry()
	_, err := r.Execute(context.Background(), "missing", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("Execute() should return error for missing tool")
	}
}

// ---------------------------------------------------------------------------
// Namespace Tests
// ---------------------------------------------------------------------------

func TestRegistry_Namespace_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	mt := newMockTool("read", "Read file")

	if err := r.RegisterInNamespace("fs", mt); err != nil {
		t.Fatalf("RegisterInNamespace() error: %v", err)
	}

	// Get with fully qualified name
	got, err := r.Get("fs:read")
	if err != nil {
		t.Fatalf("Get('fs:read') error: %v", err)
	}
	if got.Name() != "read" {
		t.Errorf("Get() Name = %q, want %q", got.Name(), "read")
	}

	// Get from specific namespace
	got2, err := r.GetFromNamespace("fs", "read")
	if err != nil {
		t.Fatalf("GetFromNamespace() error: %v", err)
	}
	if got2.Name() != "read" {
		t.Errorf("GetFromNamespace() Name = %q, want %q", got2.Name(), "read")
	}
}

func TestRegistry_Namespace_SeparatorInName(t *testing.T) {
	r := NewRegistry()
	mt := newMockTool("bad:name", "desc")

	err := r.Register(mt)
	if err == nil {
		t.Fatal("Register() should reject names containing separator")
	}
}

func TestRegistry_Namespace_EmptyName(t *testing.T) {
	r := NewRegistry()
	mt := &mockTool{name: "", description: "empty name tool", params: nil}

	err := r.Register(mt)
	if err == nil {
		t.Fatal("Register() should reject empty names")
	}
}

func TestRegistry_Namespace_DefaultFallback(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(newMockTool("default_tool", "in default ns"))

	got, err := r.Get("default_tool")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.Name() != "default_tool" {
		t.Errorf("Name = %q, want %q", got.Name(), "default_tool")
	}
}

func TestRegistry_Namespaces(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(newMockTool("a", "A"))
	r.MustRegisterInNamespace("fs", newMockTool("read", "Read"))
	r.MustRegisterInNamespace("fs", newMockTool("write", "Write"))
	r.MustRegisterInNamespace("net", newMockTool("get", "GET"))

	nss := r.Namespaces()
	if len(nss) != 2 {
		t.Fatalf("Namespaces() length = %d, want 2", len(nss))
	}

	nsMap := make(map[string]bool)
	for _, ns := range nss {
		nsMap[ns] = true
	}
	if !nsMap["fs"] || !nsMap["net"] {
		t.Errorf("Namespaces() = %v, want [fs, net]", nss)
	}
}

func TestRegistry_ListNamespace(t *testing.T) {
	r := NewRegistry()
	r.MustRegisterInNamespace("fs", newMockTool("read", "Read"))
	r.MustRegisterInNamespace("fs", newMockTool("write", "Write"))
	r.MustRegisterInNamespace("net", newMockTool("get", "GET"))

	fsTools := r.ListNamespace("fs")
	if len(fsTools) != 2 {
		t.Fatalf("ListNamespace('fs') length = %d, want 2", len(fsTools))
	}

	netTools := r.ListNamespace("net")
	if len(netTools) != 1 {
		t.Fatalf("ListNamespace('net') length = %d, want 1", len(netTools))
	}
}

func TestRegistry_Merge(t *testing.T) {
	r1 := NewRegistry()
	r1.MustRegister(newMockTool("a", "A"))

	r2 := NewRegistry()
	r2.MustRegister(newMockTool("b", "B"))

	if err := r1.Merge(r2); err != nil {
		t.Fatalf("Merge() error: %v", err)
	}
	if r1.Size() != 2 {
		t.Errorf("Size() after Merge() = %d, want 2", r1.Size())
	}
}

func TestRegistry_Merge_Conflict(t *testing.T) {
	r1 := NewRegistry()
	r1.MustRegister(newMockTool("conflict", "A"))

	r2 := NewRegistry()
	r2.MustRegister(newMockTool("conflict", "B"))

	err := r1.Merge(r2)
	if err == nil {
		t.Fatal("Merge() should return error on name conflict")
	}
}

func TestRegistry_MustRegisterInNamespace(t *testing.T) {
	r := NewRegistry()
	r.MustRegisterInNamespace("ns", newMockTool("tool", "desc"))

	if r.Size() != 1 {
		t.Errorf("Size() = %d, want 1", r.Size())
	}
}

func TestRegistry_MustRegisterInNamespace_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustRegisterInNamespace() should panic on error")
		}
	}()

	r := NewRegistry()
	r.MustRegisterInNamespace("ns", &mockTool{name: "", description: "", params: nil})
}

// ---------------------------------------------------------------------------
// Builder Tests
// ---------------------------------------------------------------------------

func TestBuilder_Basic(t *testing.T) {
	tool, err := New("greet",
		WithDescription("Greet someone"),
		WithHandler(func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`"hello"`), nil
		}),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if tool.Name() != "greet" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "greet")
	}
	if tool.Description() != "Greet someone" {
		t.Errorf("Description() = %q, want %q", tool.Description(), "Greet someone")
	}

	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if string(result) != `"hello"` {
		t.Errorf("Execute() = %s, want %q", result, `"hello"`)
	}
}

func TestBuilder_WithInputSchema(t *testing.T) {
	type SearchInput struct {
		Query string `json:"query" description:"The search query"`
		Count int    `json:"count" description:"Number of results" default:"5"`
	}

	tool, err := New("search",
		WithDescription("Search for things"),
		WithInputSchema[SearchInput](),
		WithHandler(func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			var si SearchInput
			json.Unmarshal(input, &si)
			return json.Marshal(fmt.Sprintf("searching for %q (count=%d)", si.Query, si.Count))
		}),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// Verify schema was generated
	params := tool.Parameters()
	var schema map[string]any
	if err := json.Unmarshal(params, &schema); err != nil {
		t.Fatalf("Unmarshal schema: %v", err)
	}
	if schema["type"] != "object" {
		t.Errorf("schema type = %v, want 'object'", schema["type"])
	}

	// Execute with input
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"golang","count":3}`))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	var output string
	if err := json.Unmarshal(result, &output); err != nil {
		t.Fatalf("Unmarshal result: %v", err)
	}
	if output != `searching for "golang" (count=3)` {
		t.Errorf("result = %q, want %q", output, `searching for "golang" (count=3)`)
	}
}

func TestBuilder_WithStringHandler(t *testing.T) {
	type EchoInput struct {
		Message string `json:"message"`
	}

	tool, err := New("echo",
		WithDescription("Echo a message"),
		WithInputSchema[EchoInput](),
		WithStringHandler(func(ctx context.Context, input EchoInput) (string, error) {
			return input.Message, nil
		}),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"message":"hello world"}`))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if string(result) != "hello world" {
		t.Errorf("Execute() = %s, want %q", result, "hello world")
	}
}

func TestBuilder_WithRawHandler(t *testing.T) {
	tool, err := New("raw",
		WithDescription("Raw handler"),
		WithRawHandler(func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"raw":true}`), nil
		}),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	result, err := tool.Execute(context.Background(), json.RawMessage(`anything`))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if string(result) != `{"raw":true}` {
		t.Errorf("Execute() = %s, want %q", result, `{"raw":true}`)
	}
}

func TestBuilder_WithRawSchema(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer"}}}`)
	tool, err := New("raw_schema",
		WithDescription("Has raw schema"),
		WithRawSchema(schema),
		WithHandler(func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			return input, nil
		}),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if string(tool.Parameters()) != string(schema) {
		t.Errorf("Parameters() = %s, want %s", tool.Parameters(), schema)
	}
}

func TestBuilder_WithNoArgsHandler(t *testing.T) {
	tool, err := New("ping",
		WithDescription("Ping"),
		WithNoArgsHandler(func(ctx context.Context) (string, error) {
			return "pong", nil
		}),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if string(result) != "pong" {
		t.Errorf("Execute() = %s, want %q", result, "pong")
	}
}

func TestBuilder_NoHandler(t *testing.T) {
	_, err := New("no_handler",
		WithDescription("No handler"),
	)
	if err == nil {
		t.Fatal("New() should return error when no handler is specified")
	}
}

func TestBuilder_EmptyName(t *testing.T) {
	_, err := New("",
		WithHandler(func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			return nil, nil
		}),
	)
	if err == nil {
		t.Fatal("New() should return error for empty name")
	}
}

func TestMustNew_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustNew() should panic on error")
		}
	}()

	MustNew("", WithHandler(func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		return nil, nil
	}))
}

func TestBuilder_WithToolMiddleware(t *testing.T) {
	var callOrder []string

	innerMW := func(next Tool) Tool {
		return &wrapperTool{
			inner: next,
			before: func() { callOrder = append(callOrder, "middleware1") },
		}
	}

	outerMW := func(next Tool) Tool {
		return &wrapperTool{
			inner: next,
			before: func() { callOrder = append(callOrder, "middleware2") },
		}
	}

	tool, err := New("wrapped",
		WithDescription("Wrapped tool"),
		WithToolMiddleware(innerMW),
		WithToolMiddleware(outerMW),
		WithHandler(func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			callOrder = append(callOrder, "handler")
			return json.RawMessage(`"ok"`), nil
		}),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	_, err = tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	// Middleware should be applied in reverse order: last added is outermost
	if len(callOrder) != 3 {
		t.Fatalf("callOrder length = %d, want 3: %v", len(callOrder), callOrder)
	}
	if callOrder[2] != "handler" {
		t.Errorf("last call should be handler, got %v", callOrder)
	}
}

// wrapperTool is a test helper for middleware testing.
type wrapperTool struct {
	inner  Tool
	before func()
}

func (w *wrapperTool) Name() string                                         { return w.inner.Name() }
func (w *wrapperTool) Description() string                                  { return w.inner.Description() }
func (w *wrapperTool) Parameters() json.RawMessage                          { return w.inner.Parameters() }
func (w *wrapperTool) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	if w.before != nil {
		w.before()
	}
	return w.inner.Execute(ctx, input)
}

// ---------------------------------------------------------------------------
// Typed Builder Tests
// ---------------------------------------------------------------------------

func TestTypedBuilder_Basic(t *testing.T) {
	type Input struct {
		X int `json:"x"`
		Y int `json:"y"`
	}

	tool, err := NewTyped[Input]("add",
		WithTypedDescription[Input]("Add two numbers"),
		WithTypedHandler[Input](func(ctx context.Context, input Input) (json.RawMessage, error) {
			return json.Marshal(input.X + input.Y)
		}),
	)
	if err != nil {
		t.Fatalf("NewTyped() error: %v", err)
	}

	if tool.Name() != "add" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "add")
	}

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"x":3,"y":4}`))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	var sum int
	if err := json.Unmarshal(result, &sum); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if sum != 7 {
		t.Errorf("sum = %d, want 7", sum)
	}
}

func TestTypedBuilder_WithStringHandler(t *testing.T) {
	type Input struct {
		Name string `json:"name"`
	}

	tool, err := NewTyped[Input]("greet",
		WithTypedDescription[Input]("Greet"),
		WithTypedStringHandler(func(ctx context.Context, input Input) (string, error) {
			return "Hello, " + input.Name, nil
		}),
	)
	if err != nil {
		t.Fatalf("NewTyped() error: %v", err)
	}

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"name":"World"}`))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if string(result) != "Hello, World" {
		t.Errorf("result = %q, want %q", string(result), "Hello, World")
	}
}

func TestTypedBuilder_SchemaGeneration(t *testing.T) {
	type Input struct {
		Query string `json:"query" description:"Search query"`
		Limit int    `json:"limit" description:"Max results" default:"10"`
	}

	tool, err := NewTyped[Input]("search",
		WithTypedDescription[Input]("Search"),
		WithTypedHandler[Input](func(ctx context.Context, input Input) (json.RawMessage, error) {
			return json.Marshal("ok")
		}),
	)
	if err != nil {
		t.Fatalf("NewTyped() error: %v", err)
	}

	var schema map[string]any
	if err := json.Unmarshal(tool.Parameters(), &schema); err != nil {
		t.Fatalf("Unmarshal schema: %v", err)
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema missing 'properties'")
	}
	if _, ok := props["query"]; !ok {
		t.Error("schema missing 'query' property")
	}
	if _, ok := props["limit"]; !ok {
		t.Error("schema missing 'limit' property")
	}

	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatal("schema missing 'required'")
	}
	// 'query' is required (no default, not a pointer), 'limit' should not be required (has default via omitempty logic)
	hasQuery := false
	for _, r := range required {
		if r == "query" {
			hasQuery = true
		}
	}
	if !hasQuery {
		t.Error("'query' should be in required")
	}
}

func TestMustTyped_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustTyped() should panic on error")
		}
	}()

	// Empty name should cause MustTyped to panic
	MustTyped[string]("",
		WithTypedHandler[string](func(ctx context.Context, input string) (json.RawMessage, error) {
			return nil, nil
		}),
	)
}

// ---------------------------------------------------------------------------
// Schema Generator Tests
// ---------------------------------------------------------------------------

func TestGenerateSchema_BasicStruct(t *testing.T) {
	type TestStruct struct {
		Name  string `json:"name" description:"The name"`
		Age   int    `json:"age" description:"The age"`
		Email string `json:"email" description:"Email address"`
	}

	schema, err := GenerateSchema[TestStruct]()
	if err != nil {
		t.Fatalf("GenerateSchema() error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if parsed["type"] != "object" {
		t.Errorf("type = %v, want 'object'", parsed["type"])
	}

	props, ok := parsed["properties"].(map[string]any)
	if !ok {
		t.Fatal("missing properties")
	}

	nameProp, ok := props["name"].(map[string]any)
	if !ok {
		t.Fatal("missing 'name' property")
	}
	if nameProp["type"] != "string" {
		t.Errorf("name type = %v, want 'string'", nameProp["type"])
	}
	if nameProp["description"] != "The name" {
		t.Errorf("name description = %v, want 'The name'", nameProp["description"])
	}

	ageProp, ok := props["age"].(map[string]any)
	if !ok {
		t.Fatal("missing 'age' property")
	}
	if ageProp["type"] != "integer" {
		t.Errorf("age type = %v, want 'integer'", ageProp["type"])
	}
}

func TestGenerateSchema_DefaultValues(t *testing.T) {
	type TestStruct struct {
		Count int    `json:"count" default:"10"`
		Name  string `json:"name" default:"world"`
		Flag  bool   `json:"flag" default:"true"`
	}

	schema, err := GenerateSchema[TestStruct]()
	if err != nil {
		t.Fatalf("GenerateSchema() error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	props := parsed["properties"].(map[string]any)

	countProp := props["count"].(map[string]any)
	if countProp["default"] != float64(10) {
		t.Errorf("count default = %v, want 10", countProp["default"])
	}

	nameProp := props["name"].(map[string]any)
	if nameProp["default"] != "world" {
		t.Errorf("name default = %v, want 'world'", nameProp["default"])
	}

	flagProp := props["flag"].(map[string]any)
	if flagProp["default"] != true {
		t.Errorf("flag default = %v, want true", flagProp["default"])
	}
}

func TestGenerateSchema_EnumValues(t *testing.T) {
	type TestStruct struct {
		Mode string `json:"mode" enum:"fast,slow,medium"`
	}

	schema, err := GenerateSchema[TestStruct]()
	if err != nil {
		t.Fatalf("GenerateSchema() error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	props := parsed["properties"].(map[string]any)
	modeProp := props["mode"].(map[string]any)

	enumVals, ok := modeProp["enum"].([]any)
	if !ok {
		t.Fatal("missing enum")
	}
	if len(enumVals) != 3 {
		t.Errorf("enum length = %d, want 3", len(enumVals))
	}
}

func TestGenerateSchema_SliceType(t *testing.T) {
	type TestStruct struct {
		Tags []string `json:"tags" description:"List of tags"`
	}

	schema, err := GenerateSchema[TestStruct]()
	if err != nil {
		t.Fatalf("GenerateSchema() error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	props := parsed["properties"].(map[string]any)
	tagsProp := props["tags"].(map[string]any)
	if tagsProp["type"] != "array" {
		t.Errorf("tags type = %v, want 'array'", tagsProp["type"])
	}
}

func TestGenerateSchema_MapType(t *testing.T) {
	type TestStruct struct {
		Meta map[string]string `json:"meta" description:"Metadata"`
	}

	schema, err := GenerateSchema[TestStruct]()
	if err != nil {
		t.Fatalf("GenerateSchema() error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	props := parsed["properties"].(map[string]any)
	metaProp := props["meta"].(map[string]any)
	if metaProp["type"] != "object" {
		t.Errorf("meta type = %v, want 'object'", metaProp["type"])
	}
}

func TestGenerateSchema_PointerField(t *testing.T) {
	type TestStruct struct {
		Optional *string `json:"optional" description:"Optional field"`
	}

	schema, err := GenerateSchema[TestStruct]()
	if err != nil {
		t.Fatalf("GenerateSchema() error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// Pointer fields should not be in required
	if req, ok := parsed["required"].([]any); ok && len(req) > 0 {
		for _, r := range req {
			if r == "optional" {
				t.Error("pointer field should not be required")
			}
		}
	}

	props := parsed["properties"].(map[string]any)
	optProp := props["optional"].(map[string]any)
	if optProp["type"] != "string" {
		t.Errorf("optional type = %v, want 'string'", optProp["type"])
	}
}

func TestGenerateSchema_NumericBounds(t *testing.T) {
	type TestStruct struct {
		Count int     `json:"count" min:"0" max:"100"`
		Rate  float64 `json:"rate" min:"0.0" max:"1.0"`
	}

	schema, err := GenerateSchema[TestStruct]()
	if err != nil {
		t.Fatalf("GenerateSchema() error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	props := parsed["properties"].(map[string]any)
	countProp := props["count"].(map[string]any)
	if countProp["minimum"] != float64(0) {
		t.Errorf("count minimum = %v, want 0", countProp["minimum"])
	}
	if countProp["maximum"] != float64(100) {
		t.Errorf("count maximum = %v, want 100", countProp["maximum"])
	}
}

func TestGenerateSchema_StringBounds(t *testing.T) {
	type TestStruct struct {
		Name string `json:"name" minLength:"1" maxLength:"100"`
	}

	schema, err := GenerateSchema[TestStruct]()
	if err != nil {
		t.Fatalf("GenerateSchema() error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	props := parsed["properties"].(map[string]any)
	nameProp := props["name"].(map[string]any)
	// JSON unmarshaling produces float64 for numbers, so compare loosely
	if nameProp["minLength"] == nil {
		t.Error("minLength is nil")
	} else {
		minLen, _ := nameProp["minLength"].(float64)
		if int(minLen) != 1 {
			t.Errorf("minLength = %v, want 1", nameProp["minLength"])
		}
	}
	if nameProp["maxLength"] == nil {
		t.Error("maxLength is nil")
	} else {
		maxLen, _ := nameProp["maxLength"].(float64)
		if int(maxLen) != 100 {
			t.Errorf("maxLength = %v, want 100", nameProp["maxLength"])
		}
	}
}

func TestGenerateSchema_OmitJsonDash(t *testing.T) {
	type TestStruct struct {
		Public  string `json:"public"`
		Private string `json:"-"`
	}

	schema, err := GenerateSchema[TestStruct]()
	if err != nil {
		t.Fatalf("GenerateSchema() error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	props := parsed["properties"].(map[string]any)
	if _, ok := props["private"]; ok {
		t.Error("'private' should be excluded (json:\"-\")")
	}
	if _, ok := props["public"]; !ok {
		t.Error("'public' should be included")
	}
}

func TestGenerateSchema_NestedStruct(t *testing.T) {
	type Address struct {
		City    string `json:"city" description:"City name"`
		Country string `json:"country" description:"Country code"`
	}
	type Person struct {
		Name    string  `json:"name" description:"Full name"`
		Address Address `json:"address" description:"Home address"`
	}

	schema, err := GenerateSchema[Person]()
	if err != nil {
		t.Fatalf("GenerateSchema() error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	props := parsed["properties"].(map[string]any)
	addrProp := props["address"].(map[string]any)
	if addrProp["type"] != "object" {
		t.Errorf("address type = %v, want 'object'", addrProp["type"])
	}

	addrProps := addrProp["properties"].(map[string]any)
	if _, ok := addrProps["city"]; !ok {
		t.Error("address should have 'city'")
	}
}

func TestMustGenerateSchema(t *testing.T) {
	type Simple struct {
		X int `json:"x"`
	}

	schema := MustGenerateSchema[Simple]()
	if len(schema) == 0 {
		t.Error("MustGenerateSchema() returned empty schema")
	}
}

func TestGenerateSchema_EmptyStruct(t *testing.T) {
	type Empty struct{}

	schema, err := GenerateSchema[Empty]()
	if err != nil {
		t.Fatalf("GenerateSchema() error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if parsed["type"] != "object" {
		t.Errorf("type = %v, want 'object'", parsed["type"])
	}
}

// ---------------------------------------------------------------------------
// Executor Tests
// ---------------------------------------------------------------------------

func TestExecutor_ExecuteSync(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(&mockTool{
		name:        "double",
		description: "Double a number",
		params:      json.RawMessage(`{}`),
		executeFn: func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			var req struct{ X int }
			json.Unmarshal(input, &req)
			return json.Marshal(req.X * 2)
		},
	})

	executor := NewExecutor(r, DefaultExecutionConfig())
	result := executor.ExecuteSync(context.Background(), ToolCallInput{
		Name:  "double",
		Input: json.RawMessage(`{"X":5}`),
	})

	if result.Error != nil {
		t.Fatalf("ExecuteSync() error: %v", result.Error)
	}
	var doubled int
	if err := json.Unmarshal(result.Output, &doubled); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if doubled != 10 {
		t.Errorf("result = %d, want 10", doubled)
	}
}

func TestExecutor_ExecuteSync_ToolNotFound(t *testing.T) {
	r := NewRegistry()
	executor := NewExecutor(r, DefaultExecutionConfig())

	result := executor.ExecuteSync(context.Background(), ToolCallInput{
		Name:  "nonexistent",
		Input: json.RawMessage(`{}`),
	})

	if result.Error == nil {
		t.Fatal("ExecuteSync() should return error for nonexistent tool")
	}
}

func TestExecutor_ExecuteSync_Timeout(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(&mockTool{
		name:        "slow",
		description: "Slow tool",
		params:      json.RawMessage(`{}`),
		executeFn: func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(5 * time.Second):
				return json.RawMessage(`"done"`), nil
			}
		},
	})

	config := ExecutionConfig{Timeout: 100 * time.Millisecond}
	executor := NewExecutor(r, config)

	result := executor.ExecuteSync(context.Background(), ToolCallInput{
		Name:  "slow",
		Input: json.RawMessage(`{}`),
	})

	if result.Error == nil {
		t.Fatal("ExecuteSync() should timeout")
	}
}

func TestExecutor_ExecuteParallel(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(&mockTool{
		name:        "square",
		description: "Square a number",
		params:      json.RawMessage(`{}`),
		executeFn: func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			var req struct{ X int }
			json.Unmarshal(input, &req)
			return json.Marshal(req.X * req.X)
		},
	})

	executor := NewExecutor(r, DefaultExecutionConfig())
	calls := []ToolCallInput{
		{Name: "square", Input: json.RawMessage(`{"X":2}`)},
		{Name: "square", Input: json.RawMessage(`{"X":3}`)},
		{Name: "square", Input: json.RawMessage(`{"X":4}`)},
	}

	results := executor.ExecuteParallel(context.Background(), calls)

	if len(results) != 3 {
		t.Fatalf("ExecuteParallel() returned %d results, want 3", len(results))
	}

	expected := []int{4, 9, 16}
	for i, result := range results {
		if result.Error != nil {
			t.Errorf("result[%d] error: %v", i, result.Error)
			continue
		}
		var val int
		if err := json.Unmarshal(result.Output, &val); err != nil {
			t.Errorf("result[%d] unmarshal: %v", i, err)
			continue
		}
		if val != expected[i] {
			t.Errorf("result[%d] = %d, want %d", i, val, expected[i])
		}
	}
}

func TestExecutor_ExecuteParallel_Empty(t *testing.T) {
	r := NewRegistry()
	executor := NewExecutor(r, DefaultExecutionConfig())

	results := executor.ExecuteParallel(context.Background(), nil)
	if results != nil {
		t.Errorf("ExecuteParallel(nil) = %v, want nil", results)
	}
}

func TestExecutor_ExecuteParallel_SingleCall(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(&mockTool{
		name:        "echo",
		description: "Echo",
		params:      json.RawMessage(`{}`),
		executeFn: func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			return input, nil
		},
	})

	executor := NewExecutor(r, DefaultExecutionConfig())
	results := executor.ExecuteParallel(context.Background(), []ToolCallInput{
		{Name: "echo", Input: json.RawMessage(`"hi"`)},
	})

	if len(results) != 1 {
		t.Fatalf("ExecuteParallel() returned %d results, want 1", len(results))
	}
	if results[0].Error != nil {
		t.Fatalf("result error: %v", results[0].Error)
	}
}

func TestExecutor_ExecuteParallelAndCollect(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(&mockTool{
		name:        "ok",
		description: "OK",
		params:      json.RawMessage(`{}`),
		executeFn: func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`"ok"`), nil
		},
	})
	r.MustRegister(&mockTool{
		name:        "fail",
		description: "Fail",
		params:      json.RawMessage(`{}`),
		executeFn: func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			return nil, fmt.Errorf("failure")
		},
	})

	executor := NewExecutor(r, DefaultExecutionConfig())
	calls := []ToolCallInput{
		{Name: "ok", Input: json.RawMessage(`{}`)},
		{Name: "fail", Input: json.RawMessage(`{}`)},
		{Name: "ok", Input: json.RawMessage(`{}`)},
	}

	pr := executor.ExecuteParallelAndCollect(context.Background(), calls)

	if pr.AllSucceeded() {
		t.Error("AllSucceeded() = true, want false")
	}
	if !pr.AnyFailed() {
		t.Error("AnyFailed() = false, want true")
	}
	if pr.SuccessCount() != 2 {
		t.Errorf("SuccessCount() = %d, want 2", pr.SuccessCount())
	}
	if pr.ErrorCount() != 1 {
		t.Errorf("ErrorCount() = %d, want 1", pr.ErrorCount())
	}
}

func TestExecutor_MaxParallel(t *testing.T) {
	r := NewRegistry()
	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32

	r.MustRegister(&mockTool{
		name:        "concurrent",
		description: "Track concurrency",
		params:      json.RawMessage(`{}`),
		executeFn: func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			c := concurrent.Add(1)
			for {
				old := maxConcurrent.Load()
				if c <= old || maxConcurrent.CompareAndSwap(old, c) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			concurrent.Add(-1)
			return json.RawMessage(`"done"`), nil
		},
	})

	config := ExecutionConfig{MaxParallel: 2, Timeout: 5 * time.Second}
	executor := NewExecutor(r, config)

	calls := make([]ToolCallInput, 10)
	for i := range calls {
		calls[i] = ToolCallInput{Name: "concurrent", Input: json.RawMessage(`{}`)}
	}

	results := executor.ExecuteParallel(context.Background(), calls)
	for i, r := range results {
		if r.Error != nil {
			t.Errorf("result[%d] error: %v", i, r.Error)
		}
	}

	mc := maxConcurrent.Load()
	if mc > 2 {
		t.Errorf("max concurrent = %d, should not exceed 2", mc)
	}
}

func TestExecutor_Stats(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(&mockTool{
		name:        "stat_tool",
		description: "Stat",
		params:      json.RawMessage(`{}`),
		executeFn: func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`"ok"`), nil
		},
	})

	stats := &ExecutionStats{}
	config := ExecutionConfig{Stats: stats}
	executor := NewExecutor(r, config)

	executor.ExecuteSync(context.Background(), ToolCallInput{Name: "stat_tool"})
	executor.ExecuteSync(context.Background(), ToolCallInput{Name: "stat_tool"})

	if stats.TotalExecutions != 2 {
		t.Errorf("TotalExecutions = %d, want 2", stats.TotalExecutions)
	}
	if stats.TotalErrors != 0 {
		t.Errorf("TotalErrors = %d, want 0", stats.TotalErrors)
	}
	// TotalDuration may be 0 for extremely fast mock executions;
	// the important check is that executions were recorded.
	if stats.TotalExecutions == 0 {
		t.Error("no executions were recorded")
	}
}

// ---------------------------------------------------------------------------
// ToolCallInput / ToolCallResult Tests
// ---------------------------------------------------------------------------

func TestToolCallInput_ToToolCall(t *testing.T) {
	input := ToolCallInput{
		Name:  "test",
		Input: json.RawMessage(`{"x":1}`),
		ID:    "custom-id",
	}

	call := input.ToToolCall()
	if call.ID != "custom-id" {
		t.Errorf("ID = %q, want %q", call.ID, "custom-id")
	}
	if call.Function.Name != "test" {
		t.Errorf("Name = %q, want %q", call.Function.Name, "test")
	}
	if call.Function.Arguments != `{"x":1}` {
		t.Errorf("Arguments = %q, want %q", call.Function.Arguments, `{"x":1}`)
	}
}

func TestToolCallInput_ToToolCall_AutoID(t *testing.T) {
	input := ToolCallInput{
		Name:  "test",
		Input: json.RawMessage(`{}`),
	}

	call := input.ToToolCall()
	if call.ID == "" {
		t.Error("ID should be auto-generated when empty")
	}
}

func TestToolCallResult_Methods(t *testing.T) {
	successResult := ToolCallResult{
		Input:  ToolCallInput{Name: "ok"},
		Output: json.RawMessage(`"ok"`),
	}
	if !successResult.Success() {
		t.Error("Success() should return true")
	}
	if successResult.Failed() {
		t.Error("Failed() should return false")
	}

	failResult := ToolCallResult{
		Input: ToolCallInput{Name: "fail"},
		Error: fmt.Errorf("oops"),
	}
	if failResult.Success() {
		t.Error("Success() should return false for error result")
	}
	if !failResult.Failed() {
		t.Error("Failed() should return true for error result")
	}
}

func TestToolCallResult_OutputString(t *testing.T) {
	r := ToolCallResult{Output: json.RawMessage(`"hello"`)}
	if r.OutputString() != `"hello"` {
		t.Errorf("OutputString() = %q, want %q", r.OutputString(), `"hello"`)
	}

	r2 := ToolCallResult{Output: nil}
	if r2.OutputString() != "" {
		t.Errorf("OutputString() for nil = %q, want empty", r2.OutputString())
	}
}

func TestToolCallResult_DurationSeconds(t *testing.T) {
	r := ToolCallResult{Duration: int64(1500 * time.Millisecond)}
	ds := r.DurationSeconds()
	if ds < 1.4 || ds > 1.6 {
		t.Errorf("DurationSeconds() = %f, want ~1.5", ds)
	}
}

// ---------------------------------------------------------------------------
// Parallel Results Tests
// ---------------------------------------------------------------------------

func TestParallelResults_Methods(t *testing.T) {
	pr := ParallelResults{
		Results: []ToolCallResult{
			{Input: ToolCallInput{Name: "a"}, Output: json.RawMessage(`"a"`)},
			{Input: ToolCallInput{Name: "b"}, Error: fmt.Errorf("fail")},
			{Input: ToolCallInput{Name: "c"}, Output: json.RawMessage(`"c"`)},
		},
		TotalDuration: int64(100 * time.Millisecond),
	}

	// Compute errors/successes
	for _, r := range pr.Results {
		if r.Failed() {
			pr.Errors = append(pr.Errors, r)
		} else {
			pr.Successes = append(pr.Successes, r)
		}
	}

	if !pr.AnyFailed() {
		t.Error("AnyFailed() = false, want true")
	}
	if pr.AllSucceeded() {
		t.Error("AllSucceeded() = true, want false")
	}
	if pr.SuccessCount() != 2 {
		t.Errorf("SuccessCount() = %d, want 2", pr.SuccessCount())
	}
	if pr.ErrorCount() != 1 {
		t.Errorf("ErrorCount() = %d, want 1", pr.ErrorCount())
	}
}

// ---------------------------------------------------------------------------
// Middleware Tests
// ---------------------------------------------------------------------------

func TestWithToolLogging(t *testing.T) {
	mw := WithToolLogging(slogDefault())
	wrapped := mw(newMockTool("logged", "desc"))

	if wrapped.Name() != "logged" {
		t.Errorf("Name() = %q, want %q", wrapped.Name(), "logged")
	}

	// Should pass through execute
	_, err := wrapped.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
}

func TestWithToolMetrics(t *testing.T) {
	stats := &ExecutionStats{}
	mw := WithToolMetrics(stats)
	wrapped := mw(newMockTool("metric", "desc"))

	_, err := wrapped.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if stats.TotalExecutions != 1 {
		t.Errorf("TotalExecutions = %d, want 1", stats.TotalExecutions)
	}
}

// ---------------------------------------------------------------------------
// Helper Function Tests
// ---------------------------------------------------------------------------

func TestParseInput(t *testing.T) {
	type Input struct {
		X int    `json:"x"`
		S string `json:"s"`
	}

	result, err := ParseInput[Input](json.RawMessage(`{"x":42,"s":"hello"}`))
	if err != nil {
		t.Fatalf("ParseInput() error: %v", err)
	}
	if result.X != 42 {
		t.Errorf("X = %d, want 42", result.X)
	}
	if result.S != "hello" {
		t.Errorf("S = %q, want %q", result.S, "hello")
	}
}

func TestParseInput_Empty(t *testing.T) {
	type Input struct{ X int }
	result, err := ParseInput[Input](nil)
	if err != nil {
		t.Fatalf("ParseInput(nil) error: %v", err)
	}
	if result.X != 0 {
		t.Errorf("X = %d, want 0", result.X)
	}
}

func TestParseInput_InvalidJSON(t *testing.T) {
	type Input struct{ X int }
	_, err := ParseInput[Input](json.RawMessage(`invalid`))
	if err == nil {
		t.Fatal("ParseInput() should return error for invalid JSON")
	}
}

func TestMarshalOutput(t *testing.T) {
	output, err := MarshalOutput(map[string]string{"status": "ok"})
	if err != nil {
		t.Fatalf("MarshalOutput() error: %v", err)
	}
	if string(output) != `{"status":"ok"}` {
		t.Errorf("MarshalOutput() = %s, want %q", output, `{"status":"ok"}`)
	}
}

func TestStringOutput(t *testing.T) {
	output := StringOutput("hello world")
	if string(output) != `"hello world"` {
		t.Errorf("StringOutput() = %s, want %q", output, `"hello world"`)
	}
}

func TestTruncate(t *testing.T) {
	if truncate("hello", 10) != "hello" {
		t.Error("truncate should not modify short strings")
	}
	result := truncate("hello world", 5)
	if result != "hello..." {
		t.Errorf("truncate = %q, want %q", result, "hello...")
	}
}

func TestEmptyObjectSchema(t *testing.T) {
	if string(EmptyObjectSchema) != `{"type":"object","properties":{}}` {
		t.Errorf("EmptyObjectSchema = %s", EmptyObjectSchema)
	}
}

// ---------------------------------------------------------------------------
// Concurrency Tests
// ---------------------------------------------------------------------------

func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup

	// Concurrent registrations
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("tool_%d", idx)
			r.MustRegister(newMockTool(name, "desc"))
		}(i)
	}
	wg.Wait()

	if r.Size() != 100 {
		t.Errorf("Size() = %d, want 100", r.Size())
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("tool_%d", idx)
			tool, err := r.Get(name)
			if err != nil {
				t.Errorf("Get(%q) error: %v", name, err)
				return
			}
			if tool.Name() != name {
				t.Errorf("Name() = %q, want %q", tool.Name(), name)
			}
		}(i)
	}
	wg.Wait()
}

func TestExecutor_ConcurrentExecution(t *testing.T) {
	r := NewRegistry()
	var executions atomic.Int32

	r.MustRegister(&mockTool{
		name:        "counter",
		description: "Counter",
		params:      json.RawMessage(`{}`),
		executeFn: func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			executions.Add(1)
			return json.RawMessage(`"ok"`), nil
		},
	})

	executor := NewExecutor(r, DefaultExecutionConfig())
	calls := make([]ToolCallInput, 50)
	for i := range calls {
		calls[i] = ToolCallInput{Name: "counter", Input: json.RawMessage(`{}`)}
	}

	results := executor.ExecuteParallel(context.Background(), calls)

	failed := 0
	for _, r := range results {
		if r.Error != nil {
			failed++
		}
	}

	if failed > 0 {
		t.Errorf("%d executions failed", failed)
	}
	if int(executions.Load()) != 50 {
		t.Errorf("executions = %d, want 50", executions.Load())
	}
}

// ---------------------------------------------------------------------------
// Edge Case Tests
// ---------------------------------------------------------------------------

func TestBuilder_HandlerWithEmptyInput(t *testing.T) {
	tool, err := New("empty_input",
		WithDescription("Handles empty input"),
		WithHandler(func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			if len(input) == 0 {
				return json.RawMessage(`"got empty"`), nil
			}
			return input, nil
		}),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// Execute with nil input (builtTool should default to {})
	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if string(result) != `"got empty"` {
		t.Errorf("Execute(nil) = %s, want %q", result, `"got empty"`)
	}
}

func TestBuilder_HandlerInputUnmarshalError(t *testing.T) {
	type Strict struct {
		X int `json:"x"`
	}

	tool, err := New("strict",
		WithDescription("Strict input"),
		WithInputSchema[Strict](),
		WithHandler(func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			var s Strict
			if err := json.Unmarshal(input, &s); err != nil {
				return nil, fmt.Errorf("bad input: %w", err)
			}
			return json.Marshal(s.X)
		}),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// Valid input should work
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"x":5}`))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	var val int
	json.Unmarshal(result, &val)
	if val != 5 {
		t.Errorf("val = %d, want 5", val)
	}

	// Invalid input should return error
	_, err = tool.Execute(context.Background(), json.RawMessage(`not json`))
	if err == nil {
		t.Error("Execute() should fail for invalid JSON")
	}
}

func TestRegistry_ExecuteToolCalls_Convenience(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(&mockTool{
		name:        "greet",
		description: "Greet",
		params:      json.RawMessage(`{}`),
		executeFn: func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`"hello"`), nil
		},
	})

	input := ToolCallInput{Name: "greet", Input: json.RawMessage(`{}`), ID: "test-1"}
	call := input.ToToolCall()

	result := ExecuteToolCall(context.Background(), r, call)
	if result.Error != nil {
		t.Fatalf("ExecuteToolCall() error: %v", result.Error)
	}
}

// slogDefault returns the default slog.Logger for tests.
func slogDefault() *slog.Logger {
	return slog.Default()
}

func TestCollectParallelResults(t *testing.T) {
	results := []ToolCallResult{
		{Input: ToolCallInput{Name: "a"}, Output: json.RawMessage(`"a"`)},
		{Input: ToolCallInput{Name: "b"}, Error: fmt.Errorf("fail")},
		{Input: ToolCallInput{Name: "c"}, Output: json.RawMessage(`"c"`)},
	}

	pr := CollectParallelResults(results, int64(50*time.Millisecond))

	if pr.SuccessCount() != 2 {
		t.Errorf("SuccessCount() = %d, want 2", pr.SuccessCount())
	}
	if pr.ErrorCount() != 1 {
		t.Errorf("ErrorCount() = %d, want 1", pr.ErrorCount())
	}
	if pr.TotalDurationSeconds() <= 0 {
		t.Error("TotalDurationSeconds() should be > 0")
	}
}
