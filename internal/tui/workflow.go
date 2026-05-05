package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// StepStatus represents the status of a workflow step.
type StepStatus string

const (
	// StepPending indicates the step hasn't started yet.
	StepPending StepStatus = "pending"

	// StepRunning indicates the step is currently executing.
	StepRunning StepStatus = "running"

	// StepDone indicates the step completed successfully.
	StepDone StepStatus = "done"

	// StepError indicates the step failed.
	StepError StepStatus = "error"

	// StepSkipped indicates the step was skipped.
	StepSkipped StepStatus = "skipped"

	// StepCancelled indicates the step was cancelled.
	StepCancelled StepStatus = "cancelled"
)

// WorkflowStep represents a step in the workflow visualization.
type WorkflowStep struct {
	// ID is the step identifier.
	ID string

	// Name is the display name.
	Name string

	// Agent is the agent assigned to this step.
	Agent string

	// Status is the current status.
	Status StepStatus

	// Duration is how long the step took (or has taken so far).
	Duration time.Duration

	// Input is the step input (shown in detail view).
	Input string

	// Output is the step output (shown in detail view).
	Output string

	// Error contains any error message.
	Error string

	// Dependencies are IDs of steps that must complete before this one.
	Dependencies []string

	// StartTime records when the step started.
	StartTime time.Time
}

// WorkflowModel is the Bubble Tea model for the workflow view.
type WorkflowModel struct {
	// Theme holds the styling configuration.
	Theme *Theme

	// KeyMap holds the keybindings.
	KeyMap *KeyMap

	// Steps is the list of workflow steps.
	Steps []WorkflowStep

	// SelectedStep is the index of the currently selected step.
	SelectedStep int

	// ShowDetails indicates if step details are shown.
	ShowDetails bool

	// Status is the overall workflow status.
	Status StepStatus

	// StartTime records when the workflow started.
	StartTime time.Time

	// TotalDuration is the total workflow duration.
	TotalDuration time.Duration

	// Width is the terminal width.
	Width int

	// Height is the terminal height.
	Height int

	// Ready indicates if the model is fully initialized.
	Ready bool

	// OnStart is called when the user starts the workflow.
	OnStart func() tea.Cmd

	// OnPause is called when the user pauses the workflow.
	OnPause func() tea.Cmd

	// OnCancel is called when the user cancels the workflow.
	OnCancel func() tea.Cmd

	// OnStepSelect is called when the user selects a step.
	OnStepSelect func(stepID string) tea.Cmd
}

// NewWorkflowModel creates a new WorkflowModel.
func NewWorkflowModel(theme *Theme, keyMap *KeyMap) *WorkflowModel {
	return &WorkflowModel{
		Theme:        theme,
		KeyMap:       keyMap,
		Steps:        []WorkflowStep{},
		SelectedStep: -1,
		ShowDetails:  false,
		Status:       StepPending,
		Width:        80,
		Height:       24,
	}
}

// Init initializes the workflow model.
func (m *WorkflowModel) Init() tea.Cmd {
	return nil
}

// SetSteps sets the workflow steps.
func (m *WorkflowModel) SetSteps(steps []WorkflowStep) {
	m.Steps = steps
	if m.SelectedStep >= len(steps) {
		m.SelectedStep = len(steps) - 1
	}
}

// UpdateStep updates a specific step.
func (m *WorkflowModel) UpdateStep(stepID string, status StepStatus, output string) {
	for i, step := range m.Steps {
		if step.ID == stepID {
			m.Steps[i].Status = status
			if output != "" {
				m.Steps[i].Output = output
			}
			if status == StepRunning && step.StartTime.IsZero() {
				m.Steps[i].StartTime = time.Now()
			}
			if status == StepDone || status == StepError || status == StepCancelled {
				if !step.StartTime.IsZero() {
					m.Steps[i].Duration = time.Since(step.StartTime)
				}
			}
			break
		}
	}
}

// SetStatus sets the overall workflow status.
func (m *WorkflowModel) SetStatus(status StepStatus) {
	m.Status = status
	if status == StepRunning && m.StartTime.IsZero() {
		m.StartTime = time.Now()
	}
	if status == StepDone || status == StepError || status == StepCancelled {
		if !m.StartTime.IsZero() {
			m.TotalDuration = time.Since(m.StartTime)
		}
	}
}

// SetSize updates the model dimensions.
func (m *WorkflowModel) SetSize(width, height int) {
	m.Width = width
	m.Height = height
}

// SelectNext selects the next step.
func (m *WorkflowModel) SelectNext() {
	if m.SelectedStep < len(m.Steps)-1 {
		m.SelectedStep++
	}
}

// SelectPrev selects the previous step.
func (m *WorkflowModel) SelectPrev() {
	if m.SelectedStep > 0 {
		m.SelectedStep--
	}
}

// GetSelectedStep returns the currently selected step.
func (m *WorkflowModel) GetSelectedStep() *WorkflowStep {
	if m.SelectedStep >= 0 && m.SelectedStep < len(m.Steps) {
		return &m.Steps[m.SelectedStep]
	}
	return nil
}

// Update handles messages.
func (m *WorkflowModel) Update(msg tea.Msg) (*WorkflowModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		m.Ready = true
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.KeyMap.Workflow.Start):
			if m.OnStart != nil {
				return m, m.OnStart()
			}
		case key.Matches(msg, m.KeyMap.Workflow.Pause):
			if m.OnPause != nil {
				return m, m.OnPause()
			}
		case key.Matches(msg, m.KeyMap.Workflow.Cancel):
			if m.OnCancel != nil {
				return m, m.OnCancel()
			}
		case key.Matches(msg, m.KeyMap.Workflow.SelectNext):
			m.SelectNext()
		case key.Matches(msg, m.KeyMap.Workflow.SelectPrev):
			m.SelectPrev()
		case key.Matches(msg, m.KeyMap.Workflow.StepDetail):
			m.ShowDetails = !m.ShowDetails
			step := m.GetSelectedStep()
			if step != nil && m.OnStepSelect != nil {
				return m, m.OnStepSelect(step.ID)
			}
		}
	}

	return m, nil
}

// View renders the workflow view.
func (m *WorkflowModel) View() string {
	if !m.Ready {
		return "Loading..."
	}

	var b strings.Builder

	// Render header
	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")

	// Render DAG
	b.WriteString(m.renderDAG())

	// Render step details if enabled
	if m.ShowDetails && m.SelectedStep >= 0 {
		b.WriteString("\n")
		b.WriteString(m.renderStepDetails())
	}

	return b.String()
}

func (m *WorkflowModel) renderHeader() string {
	var parts []string

	// Title
	parts = append(parts, m.Theme.Styles.Title.Render("Workflow"))

	// Status
	statusStr := m.renderStatus(m.Status)
	parts = append(parts, statusStr)

	// Duration
	if m.TotalDuration > 0 {
		durationStr := m.Theme.Styles.Muted.Render(fmt.Sprintf("Duration: %s", m.TotalDuration.Round(time.Millisecond)))
		parts = append(parts, durationStr)
	} else if m.Status == StepRunning && !m.StartTime.IsZero() {
		elapsed := time.Since(m.StartTime).Round(time.Second)
		durationStr := m.Theme.Styles.Muted.Render(fmt.Sprintf("Elapsed: %s", elapsed))
		parts = append(parts, durationStr)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

func (m *WorkflowModel) renderStatus(status StepStatus) string {
	var color lipgloss.Color
	var icon string

	switch status {
	case StepPending:
		color = m.Theme.Colors.Muted
		icon = "○"
	case StepRunning:
		color = m.Theme.Colors.Primary
		icon = "◉"
	case StepDone:
		color = m.Theme.Colors.Success
		icon = "✓"
	case StepError:
		color = m.Theme.Colors.Error
		icon = "✗"
	case StepSkipped:
		color = m.Theme.Colors.Dim
		icon = "⊘"
	case StepCancelled:
		color = m.Theme.Colors.Warning
		icon = "⊘"
	}

	style := lipgloss.NewStyle().Foreground(color).Bold(true)
	return style.Render(fmt.Sprintf("%s %s", icon, status))
}

func (m *WorkflowModel) renderDAG() string {
	if len(m.Steps) == 0 {
		return m.Theme.Styles.Muted.Render("  No workflow steps defined.\n")
	}

	var b strings.Builder
	for i, step := range m.Steps {
		selected := i == m.SelectedStep
		line := m.renderStepLine(step, selected)
		b.WriteString(line)
		b.WriteString("\n")

		// Draw connections to dependent steps
		if i < len(m.Steps)-1 {
			b.WriteString(m.renderConnection(step, m.Steps[i+1]))
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (m *WorkflowModel) renderStepLine(step WorkflowStep, selected bool) string {
	// Status icon
	icon := m.stepIcon(step.Status)
	iconStyle := m.stepStyle(step.Status)
	iconStr := iconStyle.Render(icon)

	// Step name
	var nameStyle lipgloss.Style
	if selected {
		nameStyle = m.Theme.Styles.ListSelected
	} else {
		nameStyle = m.Theme.Styles.ListNormal
	}
	nameStr := nameStyle.Render(step.Name)

	// Agent name
	agentStr := m.Theme.Styles.Muted.Render(fmt.Sprintf(" [%s]", step.Agent))

	// Duration
	var durationStr string
	if step.Duration > 0 {
		durationStr = m.Theme.Styles.Muted.Render(fmt.Sprintf(" (%.1fs)", step.Duration.Seconds()))
	}

	return fmt.Sprintf("  %s %s%s%s", iconStr, nameStr, agentStr, durationStr)
}

func (m *WorkflowModel) renderConnection(from, to WorkflowStep) string {
	// Simple vertical connection
	conn := "  │"
	if to.Dependencies != nil {
		for _, dep := range to.Dependencies {
			if dep == from.ID {
				return m.Theme.Styles.Dim.Render(conn)
			}
		}
	}
	// No direct dependency - show as disconnected
	return m.Theme.Styles.Dim.Render("  ")
}

func (m *WorkflowModel) renderStepDetails() string {
	step := m.GetSelectedStep()
	if step == nil {
		return ""
	}

	box := lipgloss.NewStyle().
		Border(m.Theme.Styles.Border).
		BorderForeground(m.Theme.Colors.Primary).
		Padding(1, 2)

	var b strings.Builder

	// Header
	b.WriteString(m.Theme.Styles.Title.Render(step.Name))
	b.WriteString("\n")

	// Status
	b.WriteString(fmt.Sprintf("Status: %s\n", m.renderStatus(step.Status)))

	// Agent
	b.WriteString(fmt.Sprintf("Agent: %s\n", step.Agent))

	// Duration
	if step.Duration > 0 {
		b.WriteString(fmt.Sprintf("Duration: %s\n", step.Duration.Round(time.Millisecond)))
	}

	// Input (truncated)
	if step.Input != "" {
		b.WriteString("\nInput:\n")
		input := step.Input
		if len(input) > 200 {
			input = input[:197] + "..."
		}
		b.WriteString(m.Theme.Styles.Dim.Render(input))
		b.WriteString("\n")
	}

	// Output (truncated)
	if step.Output != "" {
		b.WriteString("\nOutput:\n")
		output := step.Output
		if len(output) > 200 {
			output = output[:197] + "..."
		}
		b.WriteString(output)
		b.WriteString("\n")
	}

	// Error
	if step.Error != "" {
		b.WriteString(m.Theme.Styles.Error.Render(fmt.Sprintf("\nError: %s\n", step.Error)))
	}

	return box.Render(b.String())
}

func (m *WorkflowModel) stepIcon(status StepStatus) string {
	switch status {
	case StepPending:
		return "○"
	case StepRunning:
		return "◉"
	case StepDone:
		return "✓"
	case StepError:
		return "✗"
	case StepSkipped:
		return "⊘"
	case StepCancelled:
		return "⊘"
	default:
		return "?"
	}
}

func (m *WorkflowModel) stepStyle(status StepStatus) lipgloss.Style {
	var color lipgloss.Color
	switch status {
	case StepPending:
		color = m.Theme.Colors.Muted
	case StepRunning:
		color = m.Theme.Colors.Primary
	case StepDone:
		color = m.Theme.Colors.Success
	case StepError:
		color = m.Theme.Colors.Error
	case StepSkipped:
		color = m.Theme.Colors.Dim
	case StepCancelled:
		color = m.Theme.Colors.Warning
	default:
		color = m.Theme.Colors.Foreground
	}
	return lipgloss.NewStyle().Foreground(color)
}
