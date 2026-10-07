package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/user/orchestra/internal/config"
	"github.com/user/orchestra/internal/message"
	"github.com/user/orchestra/internal/provider"
	"github.com/user/orchestra/internal/provider/anthropic"
	"github.com/user/orchestra/internal/provider/cohere"
	"github.com/user/orchestra/internal/provider/gemini"
	"github.com/user/orchestra/internal/provider/llamacpp"
	"github.com/user/orchestra/internal/provider/mistral"
	"github.com/user/orchestra/internal/provider/ollama"
	"github.com/user/orchestra/internal/provider/openai"
	"github.com/user/orchestra/internal/provider/openrouter"
)

// ---------------------------------------------------------------------------
// Chat generation runtime
//
// The TUI drives generation through a chain of tea.Cmd values: each command
// waits for the next provider stream event and returns it as a message.
// AppModel.Update handles the message and schedules the next wait, which
// keeps the UI responsive while chunks arrive.
// ---------------------------------------------------------------------------

// chatStream carries the state of an in-flight chat generation between
// successive wait commands.
type chatStream struct {
	provider provider.Provider
	model    string
	events   <-chan provider.StreamEvent
	cancel   context.CancelFunc
}

// chatChunkMsg delivers one streamed text chunk.
type chatChunkMsg struct {
	stream *chatStream
	text   string
}

// chatStreamDoneMsg signals that the stream finished successfully.
type chatStreamDoneMsg struct {
	stream *chatStream
	usage  *TokenUsageInfo
}

// chatErrorMsg signals that the generation failed.
type chatErrorMsg struct {
	stream *chatStream
	err    error
}

// startChatGeneration sends the conversation to the selected provider and
// starts streaming the response. The user's new message must already be part
// of the chat history when this is called.
func (m *AppModel) startChatGeneration(content string) tea.Cmd {
	if m.Chat.IsGenerating || m.Chat.IsStreaming {
		m.Chat.AddMessage(ViewSystem, "Still generating — please wait for the current response.")
		return nil
	}

	p, model, err := m.resolveChatProvider()
	if err != nil {
		m.Chat.SetGenerating(false)
		m.Chat.AddMessage(ViewError, err.Error())
		return nil
	}

	m.Chat.SetGenerating(true)

	ctx, cancel := context.WithCancel(context.Background())
	events, err := p.Stream(ctx, provider.GenerateRequest{
		Model:    model,
		Messages: m.chatHistory(),
	})
	if err != nil {
		cancel()
		m.Chat.SetGenerating(false)
		m.Chat.AddMessage(ViewError, fmt.Sprintf("Failed to start generation via %s: %v", p.Name(), err))
		m.Logs.AddEntry(LogLevelError, "chat", fmt.Sprintf("Generation start failed: %v", err))
		return nil
	}

	m.Logs.AddEntry(LogLevelInfo, "chat", fmt.Sprintf("Sending message via %s (model %s)", p.Name(), model))

	s := &chatStream{provider: p, model: model, events: events, cancel: cancel}
	m.Chat.StartStreaming()
	return waitChatStream(s)
}

// waitChatStream returns a command that waits for the next stream event and
// surfaces it as a message for AppModel.Update to dispatch.
func waitChatStream(s *chatStream) tea.Cmd {
	return func() tea.Msg {
		for evt := range s.events {
			switch evt.Type {
			case provider.StreamEventChunk:
				return chatChunkMsg{stream: s, text: evt.Chunk}
			case provider.StreamEventError:
				if evt.Error != nil {
					return chatErrorMsg{stream: s, err: evt.Error}
				}
			case provider.StreamEventDone:
				return chatStreamDoneMsg{stream: s, usage: toTokenUsageInfo(evt.Usage)}
			default:
				// StreamEventStart and tool-call events carry no displayable
				// text for plain chat; keep waiting for the next event.
			}
		}
		// Channel closed without an explicit done event.
		return chatStreamDoneMsg{}
	}
}

// chatHistory converts the visible chat (plus the configured system prompt)
// into provider messages. The most recent user message is already included,
// since ChatModel appends it before invoking OnSubmit.
func (m *AppModel) chatHistory() []message.Message {
	var msgs []message.Message
	if m.SystemPrompt != "" {
		msgs = append(msgs, message.SystemMessage(m.SystemPrompt))
	}
	for _, msg := range m.Chat.Messages {
		switch msg.Type {
		case ViewUser:
			msgs = append(msgs, message.UserMessage(msg.Content))
		case ViewAssistant:
			msgs = append(msgs, message.AssistantMessage(msg.Content))
		}
	}
	return msgs
}

// resolveChatProvider determines which provider and model the chat should
// use, based on the current selection. If nothing is selected it probes the
// local servers (llama.cpp on 8080, Ollama on 11434) before giving up.
func (m *AppModel) resolveChatProvider() (provider.Provider, string, error) {
	name := normalizeProvider(strings.TrimSpace(m.ProviderName))
	model := strings.TrimSpace(m.ModelName)

	if name == "" {
		return m.detectLocalProvider(model)
	}

	p, err := buildChatProvider(name, m.KeyManager)
	if err != nil {
		return nil, "", err
	}

	if model == "" {
		// Ask the provider which model to use (e.g. llama.cpp reports the
		// model it currently has loaded).
		if infos, mErr := p.Models(context.Background()); mErr == nil && len(infos) > 0 {
			model = infos[0].ID
		}
	}
	return p, model, nil
}

// detectLocalProvider probes the keyless local servers, preferring llama.cpp
// on its default port 8080, then Ollama on 11434.
func (m *AppModel) detectLocalProvider(model string) (provider.Provider, string, error) {
	candidates := []struct {
		name string
		newP func() (provider.Provider, error)
	}{
		{"llamacpp", func() (provider.Provider, error) {
			return llamacpp.NewProvider(config.ProviderConfig{BaseURL: os.Getenv("LLAMACPP_BASE_URL")})
		}},
		{"ollama", func() (provider.Provider, error) {
			return ollama.NewProvider(config.ProviderConfig{BaseURL: os.Getenv("OLLAMA_BASE_URL")})
		}},
	}

	for _, c := range candidates {
		p, err := c.newP()
		if err != nil {
			continue
		}
		av, ok := p.(interface {
			IsAvailable(ctx context.Context) error
		})
		if !ok || av.IsAvailable(context.Background()) != nil {
			continue
		}
		if model == "" {
			if infos, mErr := p.Models(context.Background()); mErr == nil && len(infos) > 0 {
				model = infos[0].ID
			}
		}
		return p, model, nil
	}

	return nil, "", fmt.Errorf(
		"no model selected — pick one in the Keys view ('u' on a model), start llama-server on port 8080, or use /model <name> with /provider")
}

// buildChatProvider constructs a provider instance for chatting.
func buildChatProvider(name string, km *KeyManager) (provider.Provider, error) {
	switch name {
	case "llamacpp", "llama.cpp", "llama-cpp":
		return llamacpp.NewProvider(config.ProviderConfig{BaseURL: os.Getenv("LLAMACPP_BASE_URL")})
	case "ollama":
		return ollama.NewProvider(config.ProviderConfig{BaseURL: os.Getenv("OLLAMA_BASE_URL")})
	case "openai", "openrouter", "anthropic", "gemini", "mistral", "cohere":
		apiKey, baseURL := chatProviderCredentials(name, km)
		if apiKey == "" {
			return nil, fmt.Errorf("no API key for %s — add one in the Keys view (press 'a') or export the %s environment variable",
				ProviderDisplayName(name), providerAPIKeyEnvVar(name))
		}
		cfg := config.ProviderConfig{APIKey: apiKey, BaseURL: baseURL}
		switch name {
		case "openai":
			return openai.NewProvider(cfg)
		case "openrouter":
			return openrouter.NewProvider(cfg)
		case "anthropic":
			return anthropic.NewProvider(cfg)
		case "gemini":
			return gemini.NewProvider(cfg)
		case "mistral":
			return mistral.NewProvider(cfg)
		case "cohere":
			return cohere.NewProvider(cfg)
		}
	}
	return nil, fmt.Errorf("unknown provider %q", name)
}

// chatProviderCredentials looks up credentials from the KeyManager first
// (which already imports environment keys at startup), falling back to the
// provider's environment variable.
func chatProviderCredentials(name string, km *KeyManager) (apiKey, baseURL string) {
	if km != nil {
		if key, ok := km.GetKey(name); ok {
			return key.APIKey, key.BaseURL
		}
	}
	if v := strings.TrimSpace(os.Getenv(providerAPIKeyEnvVar(name))); v != "" {
		return v, ""
	}
	return "", ""
}

// providerAPIKeyEnvVar returns the conventional API-key environment variable
// for a provider, or "" when the provider needs no key.
func providerAPIKeyEnvVar(name string) string {
	switch name {
	case "openai":
		return "OPENAI_API_KEY"
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "openrouter":
		return "OPENROUTER_API_KEY"
	case "gemini":
		return "GEMINI_API_KEY"
	case "mistral":
		return "MISTRAL_API_KEY"
	case "cohere":
		return "COHERE_API_KEY"
	default:
		return ""
	}
}

// toTokenUsageInfo converts provider token usage to the TUI display type.
func toTokenUsageInfo(u *provider.TokenUsage) *TokenUsageInfo {
	if u == nil || (u.PromptTokens == 0 && u.CompletionTokens == 0 && u.TotalTokens == 0) {
		return nil
	}
	return &TokenUsageInfo{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
	}
}
