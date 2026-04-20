package provider_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/user/orchestra/internal/message"
	"github.com/user/orchestra/internal/provider"
)

// ---------------------------------------------------------------------------
// ProviderContract — shared test suite that every Provider must pass.
//
// To test a new provider, create a test file in the provider's package and
// call ProviderContract with a factory function that creates a ready-to-use
// provider (or a mock/fake that faithfully implements the interface).
//
// Example:
//
//	func TestContract(t *testing.T) {
//	    provider.ProviderContract(t, func() provider.Provider {
//	        return openai.NewTestProvider(t) // with recorded responses
//	    })
//	}
// ---------------------------------------------------------------------------

// ProviderFactory is a function that creates a fresh Provider for each test.
type ProviderFactory func(t *testing.T) provider.Provider

// ProviderContract runs the full contract test suite against the provider
// returned by factory.
func ProviderContract(t *testing.T, factory ProviderFactory) {
	t.Helper()

	t.Run("Name", func(t *testing.T) {
		p := factory(t)
		name := p.Name()
		if name == "" {
			t.Fatal("Name() must not return an empty string")
		}
	})

	t.Run("Models", func(t *testing.T) {
		p := factory(t)
		models, err := p.Models(context.Background())
		if err != nil {
			t.Fatalf("Models() returned error: %v", err)
		}
		if len(models) == 0 {
			t.Fatal("Models() should return at least one model")
		}

		for _, m := range models {
			if m.ID == "" {
				t.Error("ModelInfo.ID must not be empty")
			}
			if m.Name == "" {
				t.Errorf("ModelInfo.Name must not be empty for model %q", m.ID)
			}
		}
	})

	t.Run("Capabilities", func(t *testing.T) {
		p := factory(t)

		models, err := p.Models(context.Background())
		if err != nil {
			t.Fatalf("Models() returned error: %v", err)
		}

		// Test capabilities for known models
		for _, m := range models {
			caps := p.Capabilities(m.ID)
			if caps.MaxTokens < 0 {
				t.Errorf("Capabilities(%q).MaxTokens should be >= 0, got %d", m.ID, caps.MaxTokens)
			}
			if caps.ContextWindow < 0 {
				t.Errorf("Capabilities(%q).ContextWindow should be >= 0, got %d", m.ID, caps.ContextWindow)
			}
		}

		// Test capabilities for an unknown model — should return defaults, not panic
		caps := p.Capabilities("nonexistent-model-xyz-123")
		if caps.MaxTokens < 0 {
			t.Errorf("Capabilities for unknown model should have MaxTokens >= 0, got %d", caps.MaxTokens)
		}
	})

	t.Run("Generate_RequiresMessages", func(t *testing.T) {
		p := factory(t)

		_, err := p.Generate(context.Background(), provider.GenerateRequest{
			Model:    "test-model",
			Messages: []message.Message{},
		})
		if err == nil {
			t.Fatal("Generate with empty messages should return an error")
		}
	})

	t.Run("Generate_SingleTextMessage", func(t *testing.T) {
		p := factory(t)

		models, _ := p.Models(context.Background())
		model := models[0].ID

		result, err := p.Generate(context.Background(), provider.GenerateRequest{
			Model:    model,
			Messages: []message.Message{message.UserMessage("Hello, respond with exactly: pong")},
		})
		if err != nil {
			t.Fatalf("Generate() returned error: %v", err)
		}

		validateGenerateResult(t, result)
	})

	t.Run("Generate_SystemAndUserMessages", func(t *testing.T) {
		p := factory(t)

		models, _ := p.Models(context.Background())
		model := models[0].ID

		result, err := p.Generate(context.Background(), provider.GenerateRequest{
			Model: model,
			Messages: []message.Message{
				message.SystemMessage("You are a helpful assistant. Respond with exactly: confirmed"),
				message.UserMessage("Please confirm."),
			},
		})
		if err != nil {
			t.Fatalf("Generate() returned error: %v", err)
		}

		validateGenerateResult(t, result)
	})

	t.Run("Generate_WithTemperature", func(t *testing.T) {
		p := factory(t)

		models, _ := p.Models(context.Background())
		model := models[0].ID
		temp := 0.5

		result, err := p.Generate(context.Background(), provider.GenerateRequest{
			Model:    model,
			Messages: []message.Message{message.UserMessage("Hello")},
			Options:  provider.NewGenerateOptions(provider.WithTemperature(temp)),
		})
		if err != nil {
			t.Fatalf("Generate() with temperature returned error: %v", err)
		}

		validateGenerateResult(t, result)
	})

	t.Run("Generate_WithMaxTokens", func(t *testing.T) {
		p := factory(t)

		models, _ := p.Models(context.Background())
		model := models[0].ID

		result, err := p.Generate(context.Background(), provider.GenerateRequest{
			Model:    model,
			Messages: []message.Message{message.UserMessage("Hello")},
			Options:  provider.NewGenerateOptions(provider.WithMaxTokens(100)),
		})
		if err != nil {
			t.Fatalf("Generate() with max_tokens returned error: %v", err)
		}

		validateGenerateResult(t, result)
	})

	t.Run("Generate_ConversationHistory", func(t *testing.T) {
		p := factory(t)

		models, _ := p.Models(context.Background())
		model := models[0].ID

		result, err := p.Generate(context.Background(), provider.GenerateRequest{
			Model: model,
			Messages: []message.Message{
				message.SystemMessage("You are helpful."),
				message.UserMessage("What is 2+2?"),
				message.AssistantMessage("2+2 equals 4."),
				message.UserMessage("And 3+3?"),
			},
		})
		if err != nil {
			t.Fatalf("Generate() with conversation history returned error: %v", err)
		}

		validateGenerateResult(t, result)
	})

	t.Run("Generate_ContextCancellation", func(t *testing.T) {
		p := factory(t)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		models, _ := p.Models(context.Background())

		_, err := p.Generate(ctx, provider.GenerateRequest{
			Model:    models[0].ID,
			Messages: []message.Message{message.UserMessage("Hello")},
		})
		if err == nil {
			t.Fatal("Generate with cancelled context should return an error")
		}
	})

	t.Run("Generate_WithTools", func(t *testing.T) {
		p := factory(t)

		models, _ := p.Models(context.Background())
		// Find a model that supports tool calling
		var model string
		for _, m := range models {
			if m.Capabilities.ToolCalling {
				model = m.ID
				break
			}
		}
		if model == "" {
			t.Skip("No model with tool calling support")
		}

		tools := []provider.ToolDefinition{
			provider.FunctionTool("get_weather", "Get weather for a location", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"location": map[string]any{
						"type":        "string",
						"description": "City name",
					},
				},
				"required": []string{"location"},
			}),
		}

		result, err := p.Generate(context.Background(), provider.GenerateRequest{
			Model:    model,
			Messages: []message.Message{message.UserMessage("What's the weather in Tokyo?")},
			Tools:    tools,
		})
		if err != nil {
			t.Fatalf("Generate() with tools returned error: %v", err)
		}

		validateGenerateResult(t, result)

		// If the model made a tool call, validate it
		if result.IsToolCall() {
			if len(result.Message.ToolCalls) == 0 {
				t.Fatal("IsToolCall() returned true but Message.ToolCalls is empty")
			}
			for _, tc := range result.Message.ToolCalls {
				if tc.ID == "" {
					t.Error("ToolCall.ID must not be empty")
				}
				if tc.Function.Name == "" {
					t.Error("ToolCall.Function.Name must not be empty")
				}
			}
		}
	})

	t.Run("Stream_SingleTextMessage", func(t *testing.T) {
		p := factory(t)

		models, _ := p.Models(context.Background())
		model := models[0].ID

		ch, err := p.Stream(context.Background(), provider.GenerateRequest{
			Model:    model,
			Messages: []message.Message{message.UserMessage("Say hello in one word")},
		})
		if err != nil {
			t.Fatalf("Stream() returned error: %v", err)
		}

		validateStreamEvents(t, ch)
	})

	t.Run("Stream_SystemAndUserMessages", func(t *testing.T) {
		p := factory(t)

		models, _ := p.Models(context.Background())
		model := models[0].ID

		ch, err := p.Stream(context.Background(), provider.GenerateRequest{
			Model: model,
			Messages: []message.Message{
				message.SystemMessage("Respond with exactly: yes"),
				message.UserMessage("Do you understand?"),
			},
		})
		if err != nil {
			t.Fatalf("Stream() returned error: %v", err)
		}

		validateStreamEvents(t, ch)
	})

	t.Run("Stream_RequiresMessages", func(t *testing.T) {
		p := factory(t)

		_, err := p.Stream(context.Background(), provider.GenerateRequest{
			Model:    "test-model",
			Messages: []message.Message{},
		})
		if err == nil {
			t.Fatal("Stream with empty messages should return an error")
		}
	})

	t.Run("Stream_ContextCancellation", func(t *testing.T) {
		p := factory(t)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		models, _ := p.Models(context.Background())

		_, err := p.Stream(ctx, provider.GenerateRequest{
			Model:    models[0].ID,
			Messages: []message.Message{message.UserMessage("Hello")},
		})
		if err == nil {
			t.Fatal("Stream with cancelled context should return an error")
		}
	})

	t.Run("Stream_WithTemperature", func(t *testing.T) {
		p := factory(t)

		models, _ := p.Models(context.Background())
		model := models[0].ID

		ch, err := p.Stream(context.Background(), provider.GenerateRequest{
			Model:    model,
			Messages: []message.Message{message.UserMessage("Hello")},
			Options:  provider.NewGenerateOptions(provider.WithTemperature(0.7)),
		})
		if err != nil {
			t.Fatalf("Stream() with temperature returned error: %v", err)
		}

		validateStreamEvents(t, ch)
	})

	t.Run("InterfaceCompliance", func(t *testing.T) {
		p := factory(t)

		// Verify the provider implements the interface
		var _ provider.Provider = p
	})

	t.Run("MultipleModelsCapabilities", func(t *testing.T) {
		p := factory(t)

		models, err := p.Models(context.Background())
		if err != nil {
			t.Fatalf("Models() returned error: %v", err)
		}

		// Each model should have valid capabilities
		for _, m := range models {
			caps := p.Capabilities(m.ID)
			if !caps.Streaming {
				// Non-streaming models are acceptable but unusual
				t.Logf("Model %q does not support streaming", m.ID)
			}
		}
	})

	t.Run("Generate_MultiTurnConversation", func(t *testing.T) {
		p := factory(t)

		models, _ := p.Models(context.Background())
		model := models[0].ID

		// Simulate a multi-turn conversation
		messages := []message.Message{
			message.SystemMessage("You are a math tutor. Give very brief answers."),
			message.UserMessage("What is 1+1?"),
			message.AssistantMessage("1+1 = 2"),
			message.UserMessage("What about 2+2?"),
			message.AssistantMessage("2+2 = 4"),
			message.UserMessage("And 4+4?"),
		}

		result, err := p.Generate(context.Background(), provider.GenerateRequest{
			Model:    model,
			Messages: messages,
		})
		if err != nil {
			t.Fatalf("Generate() with multi-turn conversation returned error: %v", err)
		}

		validateGenerateResult(t, result)
		if result.Text() == "" {
			t.Error("Generate result should have text content for a simple math question")
		}
	})
}

// validateGenerateResult checks that a GenerateResult has all required fields.
func validateGenerateResult(t *testing.T, result *provider.GenerateResult) {
	t.Helper()

	if result == nil {
		t.Fatal("GenerateResult must not be nil")
	}

	if result.ID == "" {
		t.Error("GenerateResult.ID must not be empty")
	}

	if result.Model == "" {
		t.Error("GenerateResult.Model must not be empty")
	}

	if result.Message.Role != message.RoleAssistant {
		t.Errorf("GenerateResult.Message.Role should be 'assistant', got %q", result.Message.Role)
	}

	if result.FinishReason == "" {
		t.Error("GenerateResult.FinishReason must not be empty")
	}

	if result.CreatedAt.IsZero() {
		t.Error("GenerateResult.CreatedAt must not be zero")
	}

	// TotalTokens should be the sum of Prompt and Completion tokens
	if result.Usage.TotalTokens > 0 {
		expected := result.Usage.PromptTokens + result.Usage.CompletionTokens
		if result.Usage.TotalTokens != expected {
			t.Errorf("Usage.TotalTokens (%d) should equal PromptTokens (%d) + CompletionTokens (%d) = %d",
				result.Usage.TotalTokens, result.Usage.PromptTokens, result.Usage.CompletionTokens, expected)
		}
	}
}

// validateStreamEvents reads all events from a stream channel and validates
// the event sequence follows the expected lifecycle.
func validateStreamEvents(t *testing.T, ch <-chan provider.StreamEvent) {
	t.Helper()

	var (
		hasStart bool
		hasDone  bool
		chunks   int
		errors   int
	)

	timeout := time.After(60 * time.Second)

	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				if !hasDone {
					t.Error("Stream channel closed without a Done event")
				}
				goto done
			}

			switch evt.Type {
			case provider.StreamEventStart:
				if hasStart {
					t.Error("Multiple Start events received")
				}
				hasStart = true

			case provider.StreamEventChunk:
				if evt.Chunk == "" {
					t.Error("StreamEventChunk should have non-empty Chunk")
				}
				chunks++

			case provider.StreamEventToolCall:
				if evt.ToolCall == nil {
					t.Error("StreamEventToolCall should have non-nil ToolCall")
				} else if evt.ToolCall.Function.Name == "" {
					t.Error("ToolCall in stream should have non-empty Function.Name")
				}

			case provider.StreamEventDone:
				if hasDone {
					t.Error("Multiple Done events received")
				}
				hasDone = true

			case provider.StreamEventError:
				errors++
				if evt.Error == nil {
					t.Error("StreamEventError should have non-nil Error")
				}
			}

		case <-timeout:
			t.Fatal("Stream did not complete within timeout")
		}
	}

done:
	if !hasStart && errors == 0 {
		t.Error("Stream should emit a Start event")
	}

	if !hasDone && errors == 0 {
		t.Error("Stream should emit a Done event")
	}

	if chunks == 0 && errors == 0 {
		t.Error("Stream should emit at least one Chunk event")
	}

	if errors > 0 && hasDone {
		t.Error("Stream with errors should not emit a Done event")
	}
}

// ---------------------------------------------------------------------------
// Helper types and functions for testing
// ---------------------------------------------------------------------------

// NewTestConversation creates a simple multi-turn conversation for testing.
func NewTestConversation() []message.Message {
	return []message.Message{
		message.SystemMessage("You are a helpful test assistant. Keep responses brief."),
		message.UserMessage("Hello!"),
		message.AssistantMessage("Hi there! How can I help you?"),
		message.UserMessage("What is the capital of France?"),
	}
}

// NewTestToolDefinitions creates sample tool definitions for testing.
func NewTestToolDefinitions() []provider.ToolDefinition {
	return []provider.ToolDefinition{
		provider.FunctionTool(
			"get_weather",
			"Get the current weather for a location",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"location": map[string]any{
						"type":        "string",
						"description": "The city and state, e.g. San Francisco, CA",
					},
					"unit": map[string]any{
						"type":        "string",
						"enum":        []string{"celsius", "fahrenheit"},
						"description": "Temperature unit",
					},
				},
				"required": []string{"location"},
			},
		),
		provider.FunctionTool(
			"calculator",
			"Evaluate a mathematical expression",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"expression": map[string]any{
						"type":        "string",
						"description": "The mathematical expression to evaluate",
					},
				},
				"required": []string{"expression"},
			},
		),
	}
}

// NewTestToolCallMessages creates messages that include tool calls and results
// for testing multi-turn tool use conversations.
func NewTestToolCallMessages() []message.Message {
	return []message.Message{
		message.UserMessage("What's the weather in Tokyo?"),
		message.AssistantToolCallMessage([]message.ToolCall{
			{
				ID:   "call_123",
				Type: "function",
				Function: message.ToolCallFunction{
					Name:      "get_weather",
					Arguments: `{"location":"Tokyo","unit":"celsius"}`,
				},
			},
		}),
		message.ToolResultMessage("call_123", `{"temperature":22,"condition":"sunny"}`, false),
	}
}

// NewTestMultiModalMessages creates messages with image content for testing.
func NewTestMultiModalMessages() []message.Message {
	return []message.Message{
		{
			Role: message.RoleUser,
			Content: []message.ContentBlock{
				message.TextContentBlock("What do you see in this image?"),
				message.ImageContentBlock("https://example.com/test.png", "image/png"),
			},
		},
	}
}

// AssertErrorContains checks that err's message contains substr.
func AssertErrorContains(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Errorf("expected error containing %q, got %q", substr, err.Error())
	}
}

// AssertProviderError checks that err is a *provider.ProviderError.
func AssertProviderError(t *testing.T, err error) *provider.ProviderError {
	t.Helper()
	if err == nil {
		t.Fatal("expected a ProviderError, got nil")
	}
	pErr, ok := err.(*provider.ProviderError)
	if !ok {
		t.Fatalf("expected *provider.ProviderError, got %T: %v", err, err)
	}
	return pErr
}

// ContextWithTimeout creates a context with a 30-second timeout for tests.
func ContextWithTimeout(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// DescribeModel returns a human-readable description of a model for test output.
func DescribeModel(p provider.Provider, modelID string) string {
	caps := p.Capabilities(modelID)
	return fmt.Sprintf("%s/%s (streaming=%v, tools=%v, vision=%v, context=%d)",
		p.Name(), modelID, caps.Streaming, caps.ToolCalling, caps.Vision, caps.ContextWindow)
}
