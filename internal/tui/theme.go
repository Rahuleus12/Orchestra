// Package tui provides an interactive terminal UI for Orchestra.
//
// The TUI uses the Bubble Tea framework for terminal interaction,
// Lip Gloss for styling, Glamour for markdown rendering, and
// Bubbles for common UI components.
package tui

import "github.com/charmbracelet/lipgloss"

// ColorScheme defines the color palette used throughout the TUI.
type ColorScheme struct {
	// Primary is the main accent color.
	Primary lipgloss.Color

	// Secondary is the secondary accent color.
	Secondary lipgloss.Color

	// Muted is used for less important text.
	Muted lipgloss.Color

	// Dim is used for very subtle/disabled elements.
	Dim lipgloss.Color

	// Success is used for positive indicators.
	Success lipgloss.Color

	// Warning is used for caution indicators.
	Warning lipgloss.Color

	// Error is used for error states.
	Error lipgloss.Color

	// Background is the main background color.
	Background lipgloss.Color

	// Foreground is the main text color.
	Foreground lipgloss.Color

	// UserMessage is the color for user messages.
	UserMessage lipgloss.Color

	// AssistantMessage is the color for assistant messages.
	AssistantMessage lipgloss.Color

	// ToolCall is the color for tool call indicators.
	ToolCall lipgloss.Color

	// System is the color for system messages.
	System lipgloss.Color
}

// DarkTheme is the default dark color scheme.
var DarkTheme = ColorScheme{
	Primary:          "#61AFEF",
	Secondary:        "#C678DD",
	Muted:            "#5C6370",
	Dim:              "#3E4451",
	Success:          "#98C379",
	Warning:          "#E5C07B",
	Error:            "#E06C75",
	Background:       "#282C34",
	Foreground:       "#ABB2BF",
	UserMessage:      "#61AFEF",
	AssistantMessage: "#98C379",
	ToolCall:         "#E5C07B",
	System:           "#C678DD",
}

// LightTheme is a light color scheme for bright terminals.
var LightTheme = ColorScheme{
	Primary:          "#0366D6",
	Secondary:        "#6F42C1",
	Muted:            "#959DA5",
	Dim:              "#D1D5DA",
	Success:          "#28A745",
	Warning:          "#DBAB09",
	Error:            "#D73A49",
	Background:       "#FFFFFF",
	Foreground:       "#24292E",
	UserMessage:      "#0366D6",
	AssistantMessage: "#28A745",
	ToolCall:         "#DBAB09",
	System:           "#6F42C1",
}

// Theme holds the complete styling configuration for the TUI.
type Theme struct {
	// Colors is the color scheme to use.
	Colors ColorScheme

	// Styles contains pre-computed lipgloss styles.
	Styles *Styles
}

// Styles contains all the lipgloss styles used in the TUI.
type Styles struct {
	// Title is used for view titles.
	Title lipgloss.Style

	// Status is used for status bar text.
	Status lipgloss.Style

	// UserMsg is used for user message bubbles.
	UserMsg lipgloss.Style

	// AssistantMsg is used for assistant message bubbles.
	AssistantMsg lipgloss.Style

	// SystemMsg is used for system message bubbles.
	SystemMsg lipgloss.Style

	// ToolCallMsg is used for tool call indicators.
	ToolCallMsg lipgloss.Style

	// Error is used for error messages.
	Error lipgloss.Style

	// Success is used for success messages.
	Success lipgloss.Style

	// Warning is used for warning messages.
	Warning lipgloss.Style

	// Muted is used for less important text.
	Muted lipgloss.Style

	// Dim is used for disabled or very subtle text.
	Dim lipgloss.Style

	// Input is used for the input field.
	Input lipgloss.Style

	// InputPrompt is used for the input prompt character.
	InputPrompt lipgloss.Style

	// InputPlaceholder is used for placeholder text in the input.
	InputPlaceholder lipgloss.Style

	// Border is the base border style.
	Border lipgloss.Border

	// ActiveTab is the style for active tabs.
	ActiveTab lipgloss.Style

	// InactiveTab is the style for inactive tabs.
	InactiveTab lipgloss.Style

	// Help is used for help text and keybinding hints.
	Help lipgloss.Style

	// HelpKey is used for key names in help text.
	HelpKey lipgloss.Style

	// Command is used for /command text in chat.
	Command lipgloss.Style

	// Spinner is used for loading spinners.
	Spinner lipgloss.Style

	// ListSelected is used for selected items in lists.
	ListSelected lipgloss.Style

	// ListNormal is used for normal items in lists.
	ListNormal lipgloss.Style
}

// NewTheme creates a new Theme with the given color scheme.
func NewTheme(colors ColorScheme) *Theme {
	t := &Theme{
		Colors: colors,
		Styles: &Styles{},
	}
	t.computeStyles()
	return t
}

// DefaultTheme returns the default dark theme.
func DefaultTheme() *Theme {
	return NewTheme(DarkTheme)
}

// computeStyles initializes all lipgloss styles based on the color scheme.
func (t *Theme) computeStyles() {
	c := t.Colors

	t.Styles.Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(c.Primary).
		MarginBottom(1)

	t.Styles.Status = lipgloss.NewStyle().
		Foreground(c.Muted).
		Padding(0, 1)

	t.Styles.UserMsg = lipgloss.NewStyle().
		Foreground(c.UserMessage).
		Bold(true)

	t.Styles.AssistantMsg = lipgloss.NewStyle().
		Foreground(c.AssistantMessage)

	t.Styles.SystemMsg = lipgloss.NewStyle().
		Foreground(c.System).
		Italic(true)

	t.Styles.ToolCallMsg = lipgloss.NewStyle().
		Foreground(c.ToolCall)

	t.Styles.Error = lipgloss.NewStyle().
		Foreground(c.Error).
		Bold(true)

	t.Styles.Success = lipgloss.NewStyle().
		Foreground(c.Success)

	t.Styles.Warning = lipgloss.NewStyle().
		Foreground(c.Warning)

	t.Styles.Muted = lipgloss.NewStyle().
		Foreground(c.Muted)

	t.Styles.Dim = lipgloss.NewStyle().
		Foreground(c.Dim)

	t.Styles.Input = lipgloss.NewStyle().
		Foreground(c.Foreground)

	t.Styles.InputPrompt = lipgloss.NewStyle().
		Foreground(c.Primary).
		Bold(true)

	t.Styles.InputPlaceholder = lipgloss.NewStyle().
		Foreground(c.Dim).
		Italic(true)

	t.Styles.Border = lipgloss.RoundedBorder()

	t.Styles.ActiveTab = lipgloss.NewStyle().
		Bold(true).
		Foreground(c.Primary).
		BorderBottom(true).
		BorderBottomForeground(c.Primary).
		Padding(0, 1)

	t.Styles.InactiveTab = lipgloss.NewStyle().
		Foreground(c.Muted).
		Padding(0, 1)

	t.Styles.Help = lipgloss.NewStyle().
		Foreground(c.Muted)

	t.Styles.HelpKey = lipgloss.NewStyle().
		Foreground(c.Primary).
		Bold(true).
		Padding(0, 1)

	t.Styles.Command = lipgloss.NewStyle().
		Foreground(c.Secondary).
		Bold(true)

	t.Styles.Spinner = lipgloss.NewStyle().
		Foreground(c.Primary)

	t.Styles.ListSelected = lipgloss.NewStyle().
		Foreground(c.Primary).
		Bold(true).
		Background(c.Dim)

	t.Styles.ListNormal = lipgloss.NewStyle().
		Foreground(c.Foreground)
}

// SetLightTheme switches the theme to light mode.
func (t *Theme) SetLightTheme() {
	t.Colors = LightTheme
	t.computeStyles()
}

// SetDarkTheme switches the theme to dark mode.
func (t *Theme) SetDarkTheme() {
	t.Colors = DarkTheme
	t.computeStyles()
}
