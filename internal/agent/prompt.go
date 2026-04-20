package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync"
	texttemplate "text/template"

	"gopkg.in/yaml.v3"
)

// Template represents a compiled prompt template that can be executed
// with variable data to produce rendered text. Templates use Go's
// text/template syntax with additional built-in functions for prompt
// engineering.
//
// A Template is safe for concurrent use after creation.
type Template struct {
	// name is a human-readable identifier for this template.
	name string

	// tmpl is the compiled Go text/template.
	tmpl *texttemplate.Template

	// source is the original template text, preserved for inspection
	// and debugging.
	source string

	// once guards template compilation for deferred compilation.
	once sync.Once
}

// builtinFuncs returns the map of built-in template functions available
// in all prompt templates.
func builtinFuncs() texttemplate.FuncMap {
	return texttemplate.FuncMap{
		// json marshals a value to a JSON string.
		"json": func(v any) (string, error) {
			b, err := json.Marshal(v)
			if err != nil {
				return "", fmt.Errorf("json marshal: %w", err)
			}
			return string(b), nil
		},

		// jsonPretty marshals a value to indented JSON.
		"json_pretty": func(v any) (string, error) {
			b, err := json.MarshalIndent(v, "", "  ")
			if err != nil {
				return "", fmt.Errorf("json marshal: %w", err)
			}
			return string(b), nil
		},

		// yaml marshals a value to a YAML string.
		"yaml": func(v any) (string, error) {
			b, err := yaml.Marshal(v)
			if err != nil {
				return "", fmt.Errorf("yaml marshal: %w", err)
			}
			return strings.TrimRight(string(b), "\n"), nil
		},

		// indent prefixes every non-empty line of text with the given prefix.
		"indent": func(spaces int, text string) string {
			prefix := strings.Repeat(" ", spaces)
			lines := strings.Split(text, "\n")
			for i, line := range lines {
				if line != "" {
					lines[i] = prefix + line
				}
			}
			return strings.Join(lines, "\n")
		},

		// trim removes leading and trailing whitespace.
		"trim": strings.TrimSpace,

		// trimPrefix removes the given prefix from the text, if present.
		"trimPrefix": strings.TrimPrefix,

		// trimSuffix removes the given suffix from the text, if present.
		"trimSuffix": strings.TrimSuffix,

		// upper converts text to uppercase.
		"upper": strings.ToUpper,

		// lower converts text to lowercase.
		"lower": strings.ToLower,

		// title converts text to title case.
		"title": func(s string) string {
			// Use a simple title case that capitalizes the first letter
			// of each word.
			return strings.Title(s)
		},

		// join concatenates elements of a string slice with a separator.
		"join": strings.Join,

		// split splits text by the given separator.
		"split": func(sep, text string) []string {
			return strings.Split(text, sep)
		},

		// replace replaces all occurrences of old with new in text.
		"replace": func(old, new, text string) string {
			return strings.ReplaceAll(text, old, new)
		},

		// repeat repeats the text n times.
		"repeat": strings.Repeat,

		// default returns the fallback value if the primary is empty.
		// "Empty" means the zero value for the type: "", 0, nil, false, empty slice/map.
		"default": func(fallback, primary any) any {
			if isEmpty(primary) {
				return fallback
			}
			return primary
		},

		// coalesce returns the first non-empty argument.
		"coalesce": func(vals ...any) any {
			for _, v := range vals {
				if !isEmpty(v) {
					return v
				}
			}
			return nil
		},

		// truncate shortens text to at most n characters, appending "..." if truncated.
		"truncate": func(n int, text string) string {
			if len(text) <= n {
				return text
			}
			if n <= 3 {
				return text[:n]
			}
			return text[:n-3] + "..."
		},

		// truncateWords shortens text to at most n words, appending "..." if truncated.
		"truncateWords": func(n int, text string) string {
			words := strings.Fields(text)
			if len(words) <= n {
				return text
			}
			return strings.Join(words[:n], " ") + "..."
		},

		// newline adds n newline characters.
		"newline": func(n int) string {
			if n <= 0 {
				return ""
			}
			return strings.Repeat("\n", n)
		},

		// wrap wraps text to the given line width, preserving existing newlines.
		"wrap": func(width int, text string) string {
			return wrapText(text, width)
		},

		// contains reports whether substr is inside text.
		"contains": strings.Contains,

		// hasPrefix reports whether text starts with prefix.
		"hasPrefix": strings.HasPrefix,

		// hasSuffix reports whether text ends with suffix.
		"hasSuffix": strings.HasSuffix,
	}
}

// isEmpty reports whether a value is considered empty for template
// functions like "default" and "coalesce".
func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	switch val := v.(type) {
	case string:
		return val == ""
	case int:
		return val == 0
	case int64:
		return val == 0
	case float64:
		return val == 0
	case bool:
		return !val
	case []any:
		return len(val) == 0
	case map[string]any:
		return len(val) == 0
	default:
		return false
	}
}

// wrapText wraps text to the given width, preserving existing newlines.
func wrapText(text string, width int) string {
	if width <= 0 {
		width = 80
	}
	var result strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if len(line) <= width {
			result.WriteString(line)
			result.WriteByte('\n')
			continue
		}
		wrapped := wrapLine(line, width)
		result.WriteString(wrapped)
		result.WriteByte('\n')
	}
	return strings.TrimRight(result.String(), "\n")
}

// wrapLine wraps a single line to the given width.
func wrapLine(line string, width int) string {
	words := strings.Fields(line)
	if len(words) == 0 {
		return ""
	}

	var buf strings.Builder
	buf.WriteString(words[0])
	lineLen := len(words[0])

	for _, word := range words[1:] {
		if lineLen+1+len(word) > width {
			buf.WriteByte('\n')
			buf.WriteString(word)
			lineLen = len(word)
		} else {
			buf.WriteByte(' ')
			buf.WriteString(word)
			lineLen += 1 + len(word)
		}
	}
	return buf.String()
}

// NewTemplate creates a new prompt template from the given name and text.
// The text is parsed using Go's text/template syntax with additional
// built-in functions for prompt engineering.
//
// Returns an error if the template text has syntax errors.
func NewTemplate(name, text string) (*Template, error) {
	if name == "" {
		name = "unnamed"
	}

	tmpl, err := texttemplate.New(name).Funcs(builtinFuncs()).Parse(text)
	if err != nil {
		return nil, fmt.Errorf("parse template %q: %w", name, err)
	}

	return &Template{
		name:   name,
		tmpl:   tmpl,
		source: text,
	}, nil
}

// MustTemplate creates a new prompt template, panicking on error.
// This is useful for templates defined in code that are known to be valid.
func MustTemplate(name, text string) *Template {
	t, err := NewTemplate(name, text)
	if err != nil {
		panic(fmt.Sprintf("failed to parse template %q: %v", name, err))
	}
	return t
}

// Execute renders the template with the given data and returns the
// resulting string. The data can be any Go value — typically a struct
// or a map[string]any.
//
// Returns an error if template execution fails (e.g., missing fields,
// function errors).
func (t *Template) Execute(data any) (string, error) {
	var buf bytes.Buffer
	if err := t.tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template %q: %w", t.name, err)
	}
	return buf.String(), nil
}

// ExecuteMap is a convenience method that renders the template with
// a map[string]any as data.
func (t *Template) ExecuteMap(data map[string]any) (string, error) {
	return t.Execute(data)
}

// MustExecute renders the template, panicking on error. This is useful
// in test code or when the template and data are known to be valid.
func (t *Template) MustExecute(data any) string {
	result, err := t.Execute(data)
	if err != nil {
		panic(err)
	}
	return result
}

// Name returns the template's human-readable name.
func (t *Template) Name() string {
	return t.name
}

// Source returns the original template source text.
func (t *Template) Source() string {
	return t.source
}

// Clone returns a deep copy of the template that can be executed
// independently without affecting the original.
func (t *Template) Clone() (*Template, error) {
	cloned, err := t.tmpl.Clone()
	if err != nil {
		return nil, fmt.Errorf("clone template %q: %w", t.name, err)
	}
	return &Template{
		name:   t.name,
		tmpl:   cloned,
		source: t.source,
	}, nil
}

// AddBlock adds a named sub-template block to this template. This allows
// composing multiple templates together, where the base template can
// reference {{template "block_name" .}}.
func (t *Template) AddBlock(name, text string) error {
	_, err := t.tmpl.New(name).Parse(text)
	if err != nil {
		return fmt.Errorf("add block %q to template %q: %w", name, t.name, err)
	}
	return nil
}

// LoadTemplateFile loads a prompt template from a file on disk.
// The file name (without extension) is used as the template name.
func LoadTemplateFile(path string) (*Template, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read template file %q: %w", path, err)
	}

	name := templateNameFromPath(path)
	return NewTemplate(name, string(data))
}

// LoadTemplateFS loads a prompt template from a filesystem abstraction.
// This supports embedded filesystems (embed.FS), os.DirFS, and any
// other fs.FS implementation.
func LoadTemplateFS(fsys fs.FS, name string) (*Template, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("read template %q from FS: %w", name, err)
	}

	tmplName := templateNameFromPath(name)
	return NewTemplate(tmplName, string(data))
}

// templateNameFromPath extracts a human-friendly template name from
// a file path by stripping directories and the file extension.
func templateNameFromPath(path string) string {
	// Remove directory components
	name := path
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	if idx := strings.LastIndex(name, "\\"); idx >= 0 {
		name = name[idx+1:]
	}
	// Remove extension
	if idx := strings.LastIndex(name, "."); idx > 0 {
		name = name[:idx]
	}
	return name
}

// TemplateRegistry manages a collection of named templates, typically
// loaded from a filesystem or embedded resource. It supports lazy loading
// and template inheritance via block composition.
type TemplateRegistry struct {
	mu         sync.RWMutex
	templates  map[string]*Template
	factories  map[string]func() (*Template, error)
}

// NewTemplateRegistry creates a new empty template registry.
func NewTemplateRegistry() *TemplateRegistry {
	return &TemplateRegistry{
		templates: make(map[string]*Template),
		factories: make(map[string]func() (*Template, error)),
	}
}

// Register adds a pre-built template to the registry.
// Returns an error if a template with the same name already exists.
func (r *TemplateRegistry) Register(t *Template) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.templates[t.name]; exists {
		return fmt.Errorf("template %q already registered", t.name)
	}
	r.templates[t.name] = t
	return nil
}

// RegisterLazy registers a factory function that will create the template
// on first access. This is useful for deferring expensive template
// parsing until the template is actually needed.
func (r *TemplateRegistry) RegisterLazy(name string, factory func() (*Template, error)) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[name]; exists {
		return fmt.Errorf("lazy template %q already registered", name)
	}
	if _, exists := r.templates[name]; exists {
		return fmt.Errorf("template %q already registered", name)
	}
	r.factories[name] = factory
	return nil
}

// Get retrieves a template by name. If the template was registered lazily,
// it is created on first access and cached for subsequent calls.
func (r *TemplateRegistry) Get(name string) (*Template, error) {
	// Fast path: check existing templates
	r.mu.RLock()
	t, exists := r.templates[name]
	r.mu.RUnlock()
	if exists {
		return t, nil
	}

	// Slow path: check factories
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock
	if t, exists = r.templates[name]; exists {
		return t, nil
	}

	factory, exists := r.factories[name]
	if !exists {
		return nil, fmt.Errorf("template %q not found", name)
	}

	t, err := factory()
	if err != nil {
		return nil, fmt.Errorf("create template %q: %w", name, err)
	}

	r.templates[name] = t
	delete(r.factories, name)

	return t, nil
}

// MustGet retrieves a template by name, panicking if it doesn't exist.
func (r *TemplateRegistry) MustGet(name string) *Template {
	t, err := r.Get(name)
	if err != nil {
		panic(err)
	}
	return t
}

// List returns the names of all registered templates.
func (r *TemplateRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.templates)+len(r.factories))
	for name := range r.templates {
		names = append(names, name)
	}
	for name := range r.factories {
		names = append(names, name)
	}
	return names
}

// LoadFromFS loads all templates from a filesystem. Files ending in
// ".tmpl", ".prompt", or ".txt" are loaded as templates. The template
// name is derived from the file path (without extension).
func (r *TemplateRegistry) LoadFromFS(fsys fs.FS, root string) error {
	return fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		name := d.Name()
		if !isTemplateFile(name) {
			return nil
		}

		tmpl, err := LoadTemplateFS(fsys, path)
		if err != nil {
			return fmt.Errorf("load template %q: %w", path, err)
		}

		r.mu.Lock()
		r.templates[tmpl.name] = tmpl
		r.mu.Unlock()

		return nil
	})
}

// isTemplateFile reports whether the filename looks like a template file.
func isTemplateFile(name string) bool {
	for _, ext := range []string{".tmpl", ".prompt", ".txt", ".gotmpl"} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

// Compile-time checks.
var (
	_ fmt.Stringer = (*Template)(nil)
)

// String returns a debug representation of the template.
func (t *Template) String() string {
	return fmt.Sprintf("Template(%q, source=%d bytes)", t.name, len(t.source))
}
