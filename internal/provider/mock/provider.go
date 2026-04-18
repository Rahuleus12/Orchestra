// Package mock provides a mock implementation of the Provider interface
// for use in tests. It supports configurable responses, streaming,
// error injection, and call tracking.
package mock

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/user/orchestra/internal/message"
	"github.com/user/orchestra/internal/provider"
)

// MockResponse configures what the mock provider returns for a Generate call.
type MockResponse struct {
	// Message is the response message to return.
	Message message.Message

	// Usage is the token usage to report.
	Usage provider.TokenUsage

	// FinishReason is the reason generation stopped.
	FinishReason provider.FinishReason

	// Error is an optional error to return instead of a response.
	Error error

	// Delay is an optional duration to sleep before returning the response.
	Delay time.Duration
}

// StreamChunk configures a single chunk in a streaming response.
type StreamChunk struct {
	// Type is the stream event type.
	Type provider.StreamEventType

	// Chunk is the text content for chunk events.
	Chunk string

	// ToolCall is the tool call data for tool_call events.
	ToolCall *message.ToolCall

	// Usage is the token usage for done events.
	Usage *provider.TokenUsage

	// Error is the error for error events.
	Error error
}

// ModelConfig describes a model available in the mock provider.
type ModelConfig struct {
	// ID is the model identifier.
	ID string

	// Name is the human-readable model name.
	Name string

	// Capabilities describes what the model supports.
	Capabilities provider.ModelCapabilities
}

// Provider is a mock implementation of the provider.Provider interface.
// It records all calls and returns pre-configured responses.
//
// All methods are safe for concurrent use.
type Provider struct {
	mu sync.RWMutex

	// name is the provider name returned by Name().
	name string

	// models is the list of models this provider supports.
	models []ModelConfig

	// responses is a queue of responses for Generate calls.
	// Each call dequeues the next response. If the queue is empty,
	// the defaultResponse is used.
	responses []MockResponse

	// defaultResponse is used when the response queue is empty.
	defaultResponse MockResponse

	// streamChunks is a queue of chunk sequences for Stream calls.
	// Each call dequeues the next sequence. If empty, defaultStreamChunks is used.
	streamChunks [][]StreamChunk

	// defaultStreamChunks is used when the streamChunks queue is empty.
	defaultStreamChunks []StreamChunk

	// streamDelay is the delay between each streamed chunk.
	streamDelay time.Duration

	// generateCalls records all Generate call arguments.
	generateCalls []provider.GenerateRequest

	// streamCalls records all Stream call arguments.
	streamCalls []provider.GenerateRequest

	// callCount tracks the total number of Generate + Stream calls.
	callCount atomic.Int64
}

// NewProvider creates a new mock provider with the given name and default response.
func NewProvider(name string) *Provider {
	return &Provider{
		name: name,
		models: []ModelConfig{
			{
				ID:   "mock-model",
				Name: "Mock Model",
				Capabilities: provider.ModelCapabilities{
					Streaming:     true,
					ToolCalling:   true,
					Vision:        false,
					Audio:         false,
					JSONMode:      true,
					Seed:          true,
					MaxTokens:     4096,
					ContextWindow: 8192,
				},
			},
			{
				ID:   "mock-model-vision",
				Name: "Mock Model with Vision",
				Capabilities: provider.ModelCapabilities{
					Streaming:     true,
					ToolCalling:   true,
					Vision:        true,
					Audio:         false,
					JSONMode:      true,
					Seed:          true,
					MaxTokens:     8192,
					ContextWindow: 16384,
				},
			},
		},
		defaultResponse: MockResponse{
			Message: message.AssistantMessage("mock response"),
			Usage: provider.TokenUsage{
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
			},
			FinishReason: provider.FinishReasonStop,
		},
		defaultStreamChunks: []StreamChunk{
			{Type: provider.StreamEventStart},
			{Type: provider.StreamEventChunk, Chunk: "mock "},
			{Type: provider.StreamEventChunk, Chunk: "streaming "},
			{Type: provider.StreamEventChunk, Chunk: "response"},
			{Type: provider.StreamEventDone, Usage: &provider.TokenUsage{
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
			}},
		},
	}
}

// Name returns the mock provider name.
func (p *Provider) Name() string {
	return p.name
}

// Models returns the list of models configured on the mock provider.
func (p *Provider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]provider.ModelInfo, len(p.models))
	for i, m := range p.models {
		result[i] = provider.ModelInfo{
			ID:           m.ID,
			Name:         m.Name,
			Capabilities: m.Capabilities,
		}
	}
	return result, nil
}

// Generate returns the next configured response, or the default response
// if no more configured responses are available.
func (p *Provider) Generate(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResult, error) {
	p.mu.Lock()

	p.generateCalls = append(p.generateCalls, req)
	p.callCount.Add(1)

	// Dequeue the next response if available, else use default
	var resp MockResponse
	if len(p.responses) > 0 {
		resp = p.responses[0]
		p.responses = p.responses[1:]
	} else {
		resp = p.defaultResponse
	}

	p.mu.Unlock()

	// Simulate delay outside of lock
	if resp.Delay > 0 {
		select {
		case <-time.After(resp.Delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	model := req.Model
	if model == "" {
		model = "mock-model"
	}

	return &provider.GenerateResult{
		ID:           fmt.Sprintf("mock-%d", p.callCount.Load()),
		Message:      resp.Message,
		Usage:        resp.Usage,
		FinishReason: resp.FinishReason,
		Model:        model,
		CreatedAt:    time.Now(),
	}, nil
}

// Stream returns a channel that emits the next configured sequence of chunks,
// or the default sequence if no more configured sequences are available.
func (p *Provider) Stream(ctx context.Context, req provider.GenerateRequest) (<-chan provider.StreamEvent, error) {
	p.mu.Lock()

	p.streamCalls = append(p.streamCalls, req)
	p.callCount.Add(1)

	// Dequeue the next chunk sequence if available
	var chunks []StreamChunk
	if len(p.streamChunks) > 0 {
		chunks = p.streamChunks[0]
		p.streamChunks = p.streamChunks[1:]
	} else {
		chunks = p.defaultStreamChunks
	}

	delay := p.streamDelay

	p.mu.Unlock()

	ch := make(chan provider.StreamEvent, len(chunks)+1)

	go func() {
		defer close(ch)

		for _, chunk := range chunks {
			if delay > 0 {
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					ch <- provider.StreamEvent{
						Type:  provider.StreamEventError,
						Error: ctx.Err(),
					}
					return
				}
			}

			evt := provider.StreamEvent{
				Type:     chunk.Type,
				Chunk:    chunk.Chunk,
				ToolCall: chunk.ToolCall,
				Usage:    chunk.Usage,
				Error:    chunk.Error,
			}

			select {
			case ch <- evt:
			case <-ctx.Done():
				ch <- provider.StreamEvent{
					Type:  provider.StreamEventError,
					Error: ctx.Err(),
				}
				return
			}

			// Stop processing after an error event
			if chunk.Type == provider.StreamEventError {
				return
			}
		}
	}()

	return ch, nil
}

// Capabilities returns the capabilities for the given model.
func (p *Provider) Capabilities(model string) provider.ModelCapabilities {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, m := range p.models {
		if m.ID == model {
			return m.Capabilities
		}
	}

	// Default capabilities
	return provider.ModelCapabilities{
		Streaming:     true,
		ToolCalling:   true,
		Vision:        false,
		Audio:         false,
		JSONMode:      false,
		Seed:          false,
		MaxTokens:     4096,
		ContextWindow: 8192,
	}
}

// --- Configuration methods ---

// SetDefaultResponse sets the response returned when the response queue is empty.
func (p *Provider) SetDefaultResponse(resp MockResponse) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.defaultResponse = resp
}

// SetDefaultStreamChunks sets the chunks emitted when the stream chunk queue is empty.
func (p *Provider) SetDefaultStreamChunks(chunks []StreamChunk) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.defaultStreamChunks = chunks
}

// SetStreamDelay sets the delay between emitted stream chunks.
func (p *Provider) SetStreamDelay(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.streamDelay = d
}

// AddResponse enqueues a response to be returned by the next Generate call.
// Responses are consumed in FIFO order.
func (p *Provider) AddResponse(resp MockResponse) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.responses = append(p.responses, resp)
}

// AddStreamChunks enqueues a sequence of chunks to be emitted by the next Stream call.
// Sequences are consumed in FIFO order.
func (p *Provider) AddStreamChunks(chunks []StreamChunk) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.streamChunks = append(p.streamChunks, chunks)
}

// SetModels replaces the list of available models.
func (p *Provider) SetModels(models []ModelConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.models = models
}

// AddModel adds a model to the list of available models.
func (p *Provider) AddModel(model ModelConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.models = append(p.models, model)
}

// --- Inspection methods ---

// GenerateCalls returns a copy of all Generate call arguments.
func (p *Provider) GenerateCalls() []provider.GenerateRequest {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]provider.GenerateRequest, len(p.generateCalls))
	copy(result, p.generateCalls)
	return result
}

// StreamCalls returns a copy of all Stream call arguments.
func (p *Provider) StreamCalls() []provider.GenerateRequest {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]provider.GenerateRequest, len(p.streamCalls))
	copy(result, p.streamCalls)
	return result
}

// CallCount returns the total number of Generate + Stream calls made.
func (p *Provider) CallCount() int64 {
	return p.callCount.Load()
}

// Reset clears all recorded calls, queued responses, and resets the call counter.
func (p *Provider) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.generateCalls = nil
	p.streamCalls = nil
	p.responses = nil
	p.streamChunks = nil
	p.callCount.Store(0)
}

// LastGenerateCall returns the most recent Generate request, or an error if none.
func (p *Provider) LastGenerateCall() (provider.GenerateRequest, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.generateCalls) == 0 {
		return provider.GenerateRequest{}, fmt.Errorf("no generate calls recorded")
	}
	return p.generateCalls[len(p.generateCalls)-1], nil
}

// LastStreamCall returns the most recent Stream request, or an error if none.
func (p *Provider) LastStreamCall() (provider.GenerateRequest, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.streamCalls) == 0 {
		return provider.GenerateRequest{}, fmt.Errorf("no stream calls recorded")
	}
	return p.streamCalls[len(p.streamCalls)-1], nil
}

// GenerateCallCount returns the number of Generate calls made.
func (p *Provider) GenerateCallCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.generateCalls)
}

// StreamCallCount returns the number of Stream calls made.
func (p *Provider) StreamCallCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.streamCalls)
}

// --- Compile-time interface check ---

var _ provider.Provider = (*Provider)(nil)
