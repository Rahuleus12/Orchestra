package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
)

// MarkdownRenderer renders markdown content for terminal display.
type MarkdownRenderer struct {
	renderer *glamour.TermRenderer
	width    int
}

// NewMarkdownRenderer creates a new MarkdownRenderer with the given width.
func NewMarkdownRenderer(width int) (*MarkdownRenderer, error) {
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
		glamour.WithPreservedNewLines(),
	)
	if err != nil {
		return nil, err
	}
	return &MarkdownRenderer{
		renderer: r,
		width:    width,
	}, nil
}

// Render renders the given markdown content to styled terminal output.
func (r *MarkdownRenderer) Render(markdown string) (string, error) {
	return r.renderer.Render(markdown)
}

// SetWidth updates the rendering width.
func (r *MarkdownRenderer) SetWidth(width int) error {
	if width < 20 {
		width = 20
	}
	r.width = width
	newRenderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
		glamour.WithPreservedNewLines(),
	)
	if err != nil {
		return err
	}
	r.renderer = newRenderer
	return nil
}

// RenderInline renders markdown content as a single line (no block elements).
// This is useful for message previews and status indicators.
func (r *MarkdownRenderer) RenderInline(markdown string) string {
	// Remove block-level elements and render simple inline formatting
	result := strings.ReplaceAll(markdown, "\n", " ")
	result = strings.ReplaceAll(result, "\r", " ")
	result = collapseSpaces(result)
	return result
}

// RenderCodeBlock renders a code block with the given language and content.
func (r *MarkdownRenderer) RenderCodeBlock(language, code string) (string, error) {
	markdown := "```" + language + "\n" + code + "\n```"
	return r.renderer.Render(markdown)
}

// RenderDiff renders a diff-style content with +/- prefixes.
func (r *MarkdownRenderer) RenderDiff(diff string) (string, error) {
	markdown := "```diff\n" + diff + "\n```"
	return r.renderer.Render(markdown)
}

// collapseSpaces collapses multiple consecutive spaces into a single space.
func collapseSpaces(s string) string {
	var result []rune
	prevSpace := false
	for _, r := range s {
		if r == ' ' {
			if !prevSpace {
				result = append(result, r)
				prevSpace = true
			}
		} else {
			result = append(result, r)
			prevSpace = false
		}
	}
	return string(result)
}
