package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Messages for async model fetching
// ---------------------------------------------------------------------------

// modelsFetchResultMsg is sent when model fetching completes.
type modelsFetchResultMsg struct {
	Provider string
	Models   []ModelEntry
	Error    error
}

// ModelEntry represents a model displayed in the models view.
type ModelEntry struct {
	// ID is the model identifier.
	ID string

	// Name is the display name.
	Name string

	// Provider is the provider name.
	Provider string

	// Description is an optional model description.
	Description string

	// ContextWindow is the context window size (0 if unknown).
	ContextWindow int

	// SupportsStreaming indicates if the model supports streaming.
	SupportsStreaming bool

	// SupportsToolCalling indicates if the model supports tool calling.
	SupportsToolCalling bool

	// SupportsVision indicates if the model supports image input.
	SupportsVision bool
}

// ---------------------------------------------------------------------------
// ModelsModel — Bubble Tea model for the models view
// ---------------------------------------------------------------------------

// ModelsModel is the Bubble Tea model for browsing available models.
type ModelsModel struct {
	// Theme holds the styling configuration.
	Theme *Theme

	// KeyMap holds the keybindings.
	KeyMap *KeyMap

	// KeyManager provides access to stored API keys.
	KeyManager *KeyManager

	// ModelFetcher fetches models from provider APIs.
	ModelFetcher ModelFetcher

	// Providers lists provider names with keys configured.
	Providers []ProviderEntry

	// SelectedProvider is the index of the currently selected provider.
	SelectedProvider int

	// Models is the list of models for the selected provider.
	Models []ModelEntry

	// SelectedModel is the index of the currently selected model.
	SelectedModel int

	// IsLoading indicates if models are being fetched.
	IsLoading bool

	// LoadingSpinner is the loading indicator.
	LoadingSpinner spinner.Model

	// SearchInput is the search text input for filtering models.
	SearchInput textinput.Model

	// IsSearching indicates if the user is in search mode.
	IsSearching bool

	// SearchFilter is the current search filter text.
	SearchFilter string

	// AddKeyInput is the text input for adding a new API key (provider name).
	AddKeyInput textinput.Model

	// AddKeyValueInput is the text input for the API key value.
	AddKeyValueInput textinput.Model

	// IsAddingKey indicates if the user is in the "add key" flow.
	IsAddingKey bool

	// AddKeyStep tracks the step in the add-key flow (0=provider, 1=key).
	AddKeyStep int

	// Width is the terminal width.
	Width int

	// Height is the terminal height.
	Height int

	// Ready indicates if the model is fully initialized.
	Ready bool

	// Error is a temporary error message.
	Error string

	// StatusMessage is a temporary status message.
	StatusMessage string

	// OnSelectModel is called when the user selects a model to use.
	OnSelectModel func(provider, model string) tea.Cmd
}

// ProviderEntry represents a provider in the list.
type ProviderEntry struct {
	Name    string
	HasKey  bool
	KeyMask string
}

// ModelFetcher is the interface for fetching models from provider APIs.
type ModelFetcher interface {
	// FetchModels fetches available models from the given provider using
	// the provided API key and optional base URL.
	FetchModels(ctx context.Context, provider, apiKey, baseURL string) ([]ModelEntry, error)
}

// NewModelsModel creates a new ModelsModel.
func NewModelsModel(theme *Theme, keyMap *KeyMap, km *KeyManager, fetcher ModelFetcher) *ModelsModel {
	si := textinput.New()
	si.Placeholder = "Filter models..."
	si.CharLimit = 100

	addInput := textinput.New()
	addInput.Placeholder = "Provider name (e.g., openai)"
	addInput.CharLimit = 50

	addValueInput := textinput.New()
	addValueInput.Placeholder = "API key"
	addValueInput.CharLimit = 500
	addValueInput.EchoMode = textinput.EchoPassword
	addValueInput.EchoCharacter = '*'

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = theme.Styles.Spinner

	return &ModelsModel{
		Theme:            theme,
		KeyMap:           keyMap,
		KeyManager:       km,
		ModelFetcher:     fetcher,
		LoadingSpinner:   s,
		SearchInput:      si,
		AddKeyInput:      addInput,
		AddKeyValueInput: addValueInput,
		Width:            80,
		Height:           24,
	}
}

// Init initializes the models model.
func (m *ModelsModel) Init() tea.Cmd {
	return tea.Batch(m.LoadingSpinner.Tick, m.refreshProviders())
}

// refreshProviders rebuilds the provider list from the key manager.
func (m *ModelsModel) refreshProviders() tea.Cmd {
	return func() tea.Msg {
		// This is synchronous and fast — just reading from memory
		return nil
	}
}

// RefreshProviders rebuilds the provider list from the key manager.
func (m *ModelsModel) RefreshProviders() {
	if m.KeyManager == nil {
		return
	}

	keys := m.KeyManager.ListKeys()
	known := KnownProviders()

	// Build provider entries for known providers
	seen := make(map[string]bool)
	var providers []ProviderEntry

	for _, name := range known {
		seen[name] = true
		entry := ProviderEntry{Name: name, HasKey: false}
		for _, k := range keys {
			if k.Provider == name {
				entry.HasKey = true
				entry.KeyMask = MaskKey(k.APIKey)
				break
			}
		}
		providers = append(providers, entry)
	}

	// Add any providers with keys that aren't in the known list
	for _, k := range keys {
		if !seen[k.Provider] {
			providers = append(providers, ProviderEntry{
				Name:    k.Provider,
				HasKey:  true,
				KeyMask: MaskKey(k.APIKey),
			})
		}
	}

	m.Providers = providers
}

// Update handles messages.
func (m *ModelsModel) Update(msg tea.Msg) (*ModelsModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		m.Ready = true
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.LoadingSpinner, cmd = m.LoadingSpinner.Update(msg)
		cmds = append(cmds, cmd)

	case modelsFetchResultMsg:
		m.IsLoading = false
		if msg.Error != nil {
			m.Error = fmt.Sprintf("Failed to fetch models: %s", msg.Error)
			m.Models = nil
		} else {
			m.Models = msg.Models
			m.Error = ""
			m.StatusMessage = fmt.Sprintf("Loaded %d models from %s", len(m.Models), ProviderDisplayName(msg.Provider))
		}
		m.SelectedModel = 0
		return m, tea.Batch(cmds...)

	case tea.KeyMsg:
		// Handle add-key flow
		if m.IsAddingKey {
			return m.handleAddKeyKeys(msg)
		}

		// Handle search mode
		if m.IsSearching {
			return m.handleSearchKeys(msg)
		}

		switch {
		case key.Matches(msg, m.KeyMap.Models.SelectNext):
			m.selectNext()

		case key.Matches(msg, m.KeyMap.Models.SelectPrev):
			m.selectPrev()

		case key.Matches(msg, m.KeyMap.Models.Search):
			m.IsSearching = true
			m.SearchInput.Focus()
			return m, nil

		case key.Matches(msg, m.KeyMap.Models.AddKey):
			m.IsAddingKey = true
			m.AddKeyStep = 0
			m.AddKeyInput.SetValue("")
			m.AddKeyInput.Placeholder = "Provider name (e.g., openai, anthropic)"
			m.AddKeyInput.Focus()
			return m, nil

		case key.Matches(msg, m.KeyMap.Models.RemoveKey):
			if m.SelectedProvider >= 0 && m.SelectedProvider < len(m.Providers) {
				provider := m.Providers[m.SelectedProvider].Name
				if m.Providers[m.SelectedProvider].HasKey {
					_ = m.KeyManager.RemoveKey(provider)
					m.RefreshProviders()
					m.Models = nil
					m.StatusMessage = fmt.Sprintf("Removed key for %s", ProviderDisplayName(provider))
				}
			}
			return m, nil

		case key.Matches(msg, m.KeyMap.Models.CheckModels):
			if cmd := m.fetchModelsForSelectedProvider(); cmd != nil {
				return m, cmd
			}
			return m, tea.Batch(cmds...)

		case key.Matches(msg, m.KeyMap.Models.UseModel):
			model := m.getSelectedModel()
			if model != nil && m.OnSelectModel != nil {
				return m, m.OnSelectModel(model.Provider, model.ID)
			}
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *ModelsModel) handleSearchKeys(msg tea.KeyMsg) (*ModelsModel, tea.Cmd) {
	switch {
	case key.Matches(msg, m.KeyMap.Help.Close), key.Matches(msg, m.KeyMap.Chat.Send):
		if m.SearchInput.Value() == "" {
			m.IsSearching = false
			m.SearchInput.Blur()
		} else {
			m.SearchFilter = m.SearchInput.Value()
			m.IsSearching = false
			m.SearchInput.Blur()
		}
		return m, nil

	case key.Matches(msg, m.KeyMap.Chat.ClearInput):
		m.SearchInput.SetValue("")
		m.SearchFilter = ""
		return m, nil
	}

	var cmd tea.Cmd
	m.SearchInput, cmd = m.SearchInput.Update(msg)
	m.SearchFilter = m.SearchInput.Value()
	return m, cmd
}

func (m *ModelsModel) handleAddKeyKeys(msg tea.KeyMsg) (*ModelsModel, tea.Cmd) {
	switch {
	case key.Matches(msg, m.KeyMap.Help.Close):
		m.IsAddingKey = false
		m.AddKeyInput.Blur()
		m.AddKeyValueInput.Blur()
		return m, nil

	case key.Matches(msg, m.KeyMap.Chat.Send):
		if m.AddKeyStep == 0 {
			// Validate provider name
			provider := strings.TrimSpace(m.AddKeyInput.Value())
			if provider == "" {
				m.Error = "Provider name cannot be empty"
				return m, nil
			}
			m.AddKeyStep = 1
			m.AddKeyInput.Blur()
			m.AddKeyValueInput.SetValue("")
			m.AddKeyValueInput.Placeholder = fmt.Sprintf("API key for %s", ProviderDisplayName(provider))
			m.AddKeyValueInput.Focus()
			return m, nil
		}

		// Step 1: Save the key
		provider := strings.TrimSpace(m.AddKeyInput.Value())
		apiKey := strings.TrimSpace(m.AddKeyValueInput.Value())
		if apiKey == "" {
			m.Error = "API key cannot be empty"
			return m, nil
		}

		if err := m.KeyManager.AddKey(provider, apiKey, "", ""); err != nil {
			m.Error = fmt.Sprintf("Failed to save key: %s", err)
			return m, nil
		}

		m.IsAddingKey = false
		m.AddKeyValueInput.Blur()
		m.RefreshProviders()
		m.StatusMessage = fmt.Sprintf("Key added for %s", ProviderDisplayName(provider))
		m.Error = ""
		return m, nil
	}

	// Route to appropriate input
	var cmd tea.Cmd
	if m.AddKeyStep == 0 {
		m.AddKeyInput, cmd = m.AddKeyInput.Update(msg)
	} else {
		m.AddKeyValueInput, cmd = m.AddKeyValueInput.Update(msg)
	}
	return m, cmd
}

func (m *ModelsModel) selectNext() {
	if len(m.Providers) == 0 {
		return
	}

	// If we're showing models, navigate the model list
	if len(m.Models) > 0 {
		if m.SelectedModel < len(m.filteredModels())-1 {
			m.SelectedModel++
		}
		return
	}

	// Otherwise navigate providers
	if m.SelectedProvider < len(m.Providers)-1 {
		m.SelectedProvider++
	}
}

func (m *ModelsModel) selectPrev() {
	if len(m.Providers) == 0 {
		return
	}

	if len(m.Models) > 0 {
		if m.SelectedModel > 0 {
			m.SelectedModel--
		}
		return
	}

	if m.SelectedProvider > 0 {
		m.SelectedProvider--
	}
}

// providerNeedsKeyForModels returns true if the provider requires an API key
// to list its models. OpenRouter and Ollama expose public model endpoints.
func providerNeedsKeyForModels(name string) bool {
	switch strings.ToLower(name) {
	case "openrouter", "ollama":
		return false
	default:
		return true
	}
}

func (m *ModelsModel) fetchModelsForSelectedProvider() tea.Cmd {
	if m.SelectedProvider < 0 || m.SelectedProvider >= len(m.Providers) {
		return nil
	}

	entry := m.Providers[m.SelectedProvider]

	var apiKey, baseURL string
	if entry.HasKey {
		if key, ok := m.KeyManager.GetKey(entry.Name); ok {
			apiKey = key.APIKey
			baseURL = key.BaseURL
		}
	} else if providerNeedsKeyForModels(entry.Name) {
		m.Error = fmt.Sprintf("No API key configured for %s. Press 'a' to add one.", ProviderDisplayName(entry.Name))
		return nil
	}

	m.IsLoading = true
	m.Error = ""
	m.Models = nil

	// Capture values for the closure.
	provider := entry.Name
	fetcher := m.ModelFetcher

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		models, err := fetcher.FetchModels(ctx, provider, apiKey, baseURL)
		return modelsFetchResultMsg{
			Provider: provider,
			Models:   models,
			Error:    err,
		}
	}
}

// filteredModels returns models filtered by the search filter.
func (m *ModelsModel) filteredModels() []ModelEntry {
	if m.SearchFilter == "" {
		return m.Models
	}

	var filtered []ModelEntry
	for _, model := range m.Models {
		if containsFold(model.ID, m.SearchFilter) ||
			containsFold(model.Name, m.SearchFilter) ||
			containsFold(model.Description, m.SearchFilter) {
			filtered = append(filtered, model)
		}
	}
	return filtered
}

// getSelectedModel returns the currently selected model entry.
func (m *ModelsModel) getSelectedModel() *ModelEntry {
	models := m.filteredModels()
	if m.SelectedModel >= 0 && m.SelectedModel < len(models) {
		return &models[m.SelectedModel]
	}
	return nil
}

// SetSize updates the model dimensions.
func (m *ModelsModel) SetSize(width, height int) {
	m.Width = width
	m.Height = height
	m.SearchInput.Width = width - 20
	m.AddKeyInput.Width = width - 20
	m.AddKeyValueInput.Width = width - 20
}

// View renders the models view.
func (m *ModelsModel) View() string {
	if !m.Ready {
		return "Loading..."
	}

	var b strings.Builder

	// Render header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Handle add-key overlay
	if m.IsAddingKey {
		b.WriteString(m.renderAddKeyOverlay())
		return b.String()
	}

	// Render two-column layout: providers | models
	b.WriteString(m.renderTwoColumns())

	// Render footer
	b.WriteString("\n")
	b.WriteString(m.renderFooter())

	return b.String()
}

func (m *ModelsModel) renderHeader() string {
	title := m.Theme.Styles.Title.Render("🔑 API Keys & Models")
	return title
}

func (m *ModelsModel) renderTwoColumns() string {
	// Left column: providers (30% width)
	leftWidth := m.Width * 3 / 10
	if leftWidth < 20 {
		leftWidth = 20
	}
	rightWidth := m.Width - leftWidth - 3
	if rightWidth < 30 {
		rightWidth = 30
	}

	leftPanel := m.renderProviderList(leftWidth)
	rightPanel := m.renderModelList(rightWidth)

	// Join with separator
	separator := m.Theme.Styles.Dim.Render("│")

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, separator, rightPanel)
}

func (m *ModelsModel) renderProviderList(width int) string {
	var b strings.Builder

	header := m.Theme.Styles.Muted.Render("Providers")
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", width))
	b.WriteString("\n")

	m.RefreshProviders()

	if len(m.Providers) == 0 {
		b.WriteString(m.Theme.Styles.Muted.Render("  No providers configured.\n  Press 'a' to add an API key.\n"))
		return b.String()
	}

	for i, entry := range m.Providers {
		selected := i == m.SelectedProvider

		var line string
		name := ProviderDisplayName(entry.Name)

		if entry.HasKey {
			keyBadge := m.Theme.Styles.Success.Render("✓")
			line = fmt.Sprintf(" %s %s %s", keyBadge, name, m.Theme.Styles.Dim.Render(entry.KeyMask))
		} else if !providerNeedsKeyForModels(entry.Name) {
			// Public model endpoint — can fetch without a key
			publicBadge := m.Theme.Styles.Dim.Render("○")
			line = fmt.Sprintf(" %s %s", publicBadge, name)
		} else {
			noKeyBadge := m.Theme.Styles.Dim.Render("✗")
			line = fmt.Sprintf(" %s %s", noKeyBadge, m.Theme.Styles.Dim.Render(name))
		}

		if selected {
			line = m.Theme.Styles.ListSelected.Render(line)
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

func (m *ModelsModel) renderModelList(width int) string {
	var b strings.Builder

	// Header with search bar
	if m.IsSearching {
		b.WriteString(m.SearchInput.View())
	} else if m.SearchFilter != "" {
		filterLabel := m.Theme.Styles.Muted.Render("Filter: ")
		filterValue := m.Theme.Styles.HelpKey.Render(m.SearchFilter)
		b.WriteString(filterLabel + filterValue)
	} else {
		b.WriteString(m.Theme.Styles.Muted.Render("Models"))
	}
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", width))
	b.WriteString("\n")

	// Loading state
	if m.IsLoading {
		s := m.LoadingSpinner.View()
		b.WriteString(m.Theme.Styles.Muted.Render(fmt.Sprintf("  %s Fetching models...", s)))
		return b.String()
	}

	// Error state
	if m.Error != "" {
		b.WriteString(m.Theme.Styles.Error.Render(fmt.Sprintf("  %s", m.Error)))
		b.WriteString("\n")
	}

	// Status message
	if m.StatusMessage != "" {
		b.WriteString(m.Theme.Styles.Success.Render(fmt.Sprintf("  %s", m.StatusMessage)))
		b.WriteString("\n\n")
	}

	// No provider selected or no models
	models := m.filteredModels()
	if len(m.Models) == 0 && m.Error == "" {
		if m.SelectedProvider >= 0 && m.SelectedProvider < len(m.Providers) {
			entry := m.Providers[m.SelectedProvider]
			if entry.HasKey || !providerNeedsKeyForModels(entry.Name) {
				b.WriteString(m.Theme.Styles.Muted.Render(fmt.Sprintf("  Press Enter or 'r' to fetch models from %s.\n", ProviderDisplayName(entry.Name))))
			} else {
				b.WriteString(m.Theme.Styles.Muted.Render(fmt.Sprintf("  No API key for %s.\n  Press 'a' to add a key.\n", ProviderDisplayName(entry.Name))))
			}
		} else {
			b.WriteString(m.Theme.Styles.Muted.Render("  Select a provider and press Enter to see available models.\n"))
		}
		return b.String()
	}

	// Render model list
	visibleHeight := m.Height - 8
	if visibleHeight < 5 {
		visibleHeight = 5
	}

	start := 0
	if m.SelectedModel >= visibleHeight {
		start = m.SelectedModel - visibleHeight + 1
	}
	end := start + visibleHeight
	if end > len(models) {
		end = len(models)
	}

	for i := start; i < end; i++ {
		model := models[i]
		selected := i == m.SelectedModel

		line := m.renderModelEntry(model, width, selected)
		b.WriteString(line)
		b.WriteString("\n")
	}

	// Show count
	total := len(models)
	if total > visibleHeight {
		count := m.Theme.Styles.Dim.Render(fmt.Sprintf("\n  Showing %d-%d of %d models", start+1, end, total))
		b.WriteString(count)
	}

	return b.String()
}

func (m *ModelsModel) renderModelEntry(model ModelEntry, width int, selected bool) string {
	// Model ID (bold)
	id := model.ID

	// Capabilities badges
	var badges []string
	if model.SupportsStreaming {
		badges = append(badges, m.Theme.Styles.Dim.Render("stream"))
	}
	if model.SupportsToolCalling {
		badges = append(badges, m.Theme.Styles.Dim.Render("tools"))
	}
	if model.SupportsVision {
		badges = append(badges, m.Theme.Styles.Dim.Render("vision"))
	}

	// Context window
	var ctxInfo string
	if model.ContextWindow > 0 {
		ctxInfo = m.Theme.Styles.Dim.Render(fmt.Sprintf("%dk ctx", model.ContextWindow/1024))
	}

	// Build line
	parts := []string{"  ", id}
	if len(badges) > 0 {
		parts = append(parts, " ", strings.Join(badges, " "))
	}
	if ctxInfo != "" {
		parts = append(parts, " ", ctxInfo)
	}

	line := strings.Join(parts, "")

	// Truncate if too long
	if len(line) > width {
		line = line[:width-3] + "..."
	}

	if selected {
		line = m.Theme.Styles.ListSelected.Render(line)
	}

	return line
}

func (m *ModelsModel) renderAddKeyOverlay() string {
	var b strings.Builder

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.Theme.Colors.Primary).
		Padding(1, 2).
		Width(m.Width - 8)

	b.WriteString(m.Theme.Styles.Title.Render("Add API Key"))
	b.WriteString("\n\n")

	if m.AddKeyStep == 0 {
		b.WriteString(m.Theme.Styles.Muted.Render("Step 1: Enter provider name"))
		b.WriteString("\n\n")
		b.WriteString("  Supported providers: ")
		b.WriteString(m.Theme.Styles.Dim.Render(strings.Join(KnownProviders(), ", ")))
		b.WriteString("\n\n")
		b.WriteString("  " + m.AddKeyInput.View())
		b.WriteString("\n\n")
	} else {
		provider := strings.TrimSpace(m.AddKeyInput.Value())
		b.WriteString(m.Theme.Styles.Muted.Render(fmt.Sprintf("Step 2: Enter API key for %s", ProviderDisplayName(provider))))
		b.WriteString("\n\n")
		b.WriteString("  " + m.AddKeyValueInput.View())
		b.WriteString("\n\n")
	}

	b.WriteString(m.Theme.Styles.Dim.Render("  Enter to confirm • Esc to cancel"))

	return lipgloss.Place(m.Width, m.Height-4, lipgloss.Center, lipgloss.Center, box.Render(b.String()))
}

func (m *ModelsModel) renderFooter() string {
	keys := []struct {
		key  string
		help string
	}{
		{"↑/↓", "navigate"},
		{"a", "add key"},
		{"d", "remove key"},
		{"r/enter", "fetch models"},
		{"/", "filter"},
		{"u", "use model"},
		{"esc", "back"},
	}

	var parts []string
	for _, k := range keys {
		keyStr := m.Theme.Styles.HelpKey.Render(k.key)
		helpStr := m.Theme.Styles.Help.Render(k.help)
		parts = append(parts, keyStr+helpStr)
	}

	return strings.Join(parts, "  ")
}
