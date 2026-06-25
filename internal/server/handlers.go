package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/user/orchestra/internal/agent"
	"github.com/user/orchestra/internal/message"
	"github.com/user/orchestra/internal/orchestration"
	"github.com/user/orchestra/internal/provider"
)

// ---------------------------------------------------------------------------
// Health & Info
// ---------------------------------------------------------------------------

// handleHealth returns the server health status.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"status":    "ok",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.Marshal(resp)
	writeJSON(w, http.StatusOK, data)
}

// handleInfo returns server information including version and providers.
func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	providers := s.registry.ListProviders()
	resp := map[string]any{
		"name":      "Orchestra",
		"version":   "1.0.0",
		"providers": providers,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.Marshal(resp)
	writeJSON(w, http.StatusOK, data)
}

// ---------------------------------------------------------------------------
// Providers
// ---------------------------------------------------------------------------

// handleListProviders lists all registered providers.
func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	names := s.registry.ListProviders()
	providerInfos := make([]map[string]any, 0, len(names))

	for _, name := range names {
		p, err := s.registry.Get(name)
		if err != nil {
			continue
		}
		info := map[string]any{
			"name": p.Name(),
		}

		// Try to get models, but don't fail if unavailable
		models, err := p.Models(r.Context())
		if err == nil {
			modelIDs := make([]string, 0, len(models))
			for _, m := range models {
				modelIDs = append(modelIDs, m.ID)
			}
			info["models"] = modelIDs
		}

		providerInfos = append(providerInfos, info)
	}

	resp := map[string]any{
		"providers": providerInfos,
	}
	data, _ := json.Marshal(resp)
	writeJSON(w, http.StatusOK, data)
}

// handleGetProvider returns details for a specific provider.
func (s *Server) handleGetProvider(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "provider name is required")
		return
	}

	p, err := s.registry.Get(name)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("provider %q not found", name))
		return
	}

	info := map[string]any{
		"name": p.Name(),
	}

	models, err := p.Models(r.Context())
	if err == nil {
		modelInfos := make([]map[string]any, 0, len(models))
		for _, m := range models {
			mi := map[string]any{
				"id":          m.ID,
				"name":        m.Name,
				"description": m.Description,
			}
			if m.Deprecated {
				mi["deprecated"] = true
			}
			modelInfos = append(modelInfos, mi)
		}
		info["models"] = modelInfos
	}

	data, _ := json.Marshal(info)
	writeJSON(w, http.StatusOK, data)
}

// ---------------------------------------------------------------------------
// Models
// ---------------------------------------------------------------------------

// handleListModels lists all available models across all providers, or
// filtered by the "provider" query parameter.
func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	providerFilter := r.URL.Query().Get("provider")

	var names []string
	if providerFilter != "" {
		if !s.registry.IsRegistered(providerFilter) {
			writeError(w, http.StatusNotFound, fmt.Sprintf("provider %q not found", providerFilter))
			return
		}
		names = []string{providerFilter}
	} else {
		names = s.registry.ListProviders()
	}

	type modelEntry struct {
		ID       string `json:"id"`
		Name     string `json:"name,omitempty"`
		Provider string `json:"provider"`
	}

	var allModels []modelEntry
	for _, name := range names {
		p, err := s.registry.Get(name)
		if err != nil {
			continue
		}
		models, err := p.Models(r.Context())
		if err != nil {
			continue
		}
		for _, m := range models {
			allModels = append(allModels, modelEntry{
				ID:       m.ID,
				Name:     m.Name,
				Provider: name,
			})
		}
	}

	resp := map[string]any{
		"models": allModels,
		"count":  len(allModels),
	}
	data, _ := json.Marshal(resp)
	writeJSON(w, http.StatusOK, data)
}

// ---------------------------------------------------------------------------
// Generate
// ---------------------------------------------------------------------------

// generateRequest is the JSON request body for the /v1/generate endpoint.
type generateRequest struct {
	// Model is the model reference (e.g., "openai::gpt-4o", "gpt-4o", or an alias).
	Model string `json:"model"`

	// Messages is the conversation to send.
	Messages []messageRequest `json:"messages"`

	// Tools is an optional list of tool definitions.
	Tools []provider.ToolDefinition `json:"tools,omitempty"`

	// Options configures generation parameters.
	Options *generateOptionsRequest `json:"options,omitempty"`

	// Stream indicates whether to stream the response via SSE.
	Stream bool `json:"stream,omitempty"`
}

// messageRequest is a single message in a generate request.
type messageRequest struct {
	Role       string             `json:"role"`
	Content    string             `json:"content"`
	Name       string             `json:"name,omitempty"`
	ToolCalls  []message.ToolCall `json:"tool_calls,omitempty"`
	ToolResult *toolResultRequest `json:"tool_result,omitempty"`
}

// toolResultRequest represents a tool result in a request message.
type toolResultRequest struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error,omitempty"`
}

// generateOptionsRequest maps to provider.GenerateOptions.
type generateOptionsRequest struct {
	Temperature    *float64 `json:"temperature,omitempty"`
	TopP           *float64 `json:"top_p,omitempty"`
	MaxTokens      *int     `json:"max_tokens,omitempty"`
	StopSequences  []string `json:"stop_sequences,omitempty"`
	Seed           *int64   `json:"seed,omitempty"`
	ResponseFormat string   `json:"response_format,omitempty"`
}

// handleGenerate handles POST /v1/generate — a single completion request.
func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r, 10<<20) // 10 MB max
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var req generateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages are required")
		return
	}

	// Resolve provider and model
	p, modelID, err := s.resolveProvider(req.Model)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Build the provider request
	msgs := convertMessages(req.Messages)
	opts := buildGenerateOptions(req.Options)

	genReq := provider.GenerateRequest{
		Model:    modelID,
		Messages: msgs,
		Tools:    req.Tools,
		Options:  opts,
	}

	// Execute
	result, err := p.Generate(r.Context(), genReq)
	if err != nil {
		s.logger.Error("generate failed",
			"provider", p.Name(),
			"model", modelID,
			"error", err,
		)
		status := http.StatusInternalServerError
		if pe, ok := err.(*provider.ProviderError); ok && pe.StatusCode > 0 {
			status = pe.StatusCode
		}
		writeError(w, status, fmt.Sprintf("generation failed: %v", err))
		return
	}

	data, _ := json.Marshal(result)
	writeJSON(w, http.StatusOK, data)
}

// handleGenerateStream handles POST /v1/generate/stream — a streaming SSE completion.
func (s *Server) handleGenerateStream(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r, 10<<20)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var req generateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages are required")
		return
	}

	p, modelID, err := s.resolveProvider(req.Model)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	msgs := convertMessages(req.Messages)
	opts := buildGenerateOptions(req.Options)

	genReq := provider.GenerateRequest{
		Model:    modelID,
		Messages: msgs,
		Tools:    req.Tools,
		Options:  opts,
	}

	// Start streaming
	events, err := p.Stream(r.Context(), genReq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("stream failed: %v", err))
		return
	}

	// SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, canFlush := w.(http.Flusher)

	for event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		if canFlush {
			flusher.Flush()
		}
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	if canFlush {
		flusher.Flush()
	}
}

// ---------------------------------------------------------------------------
// Agents
// ---------------------------------------------------------------------------

// createAgentRequest is the JSON body for POST /v1/agents.
type createAgentRequest struct {
	Name     string `json:"name"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	System   string `json:"system_prompt,omitempty"`
	MaxTurns int    `json:"max_turns,omitempty"`

	// Run immediately with this input
	Input string `json:"input,omitempty"`
}

// handleCreateAgent creates a named agent (and optionally runs it).
func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r, 10<<20)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var req createAgentRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "agent name is required")
		return
	}

	// Determine provider and model
	modelRef := req.Model
	if modelRef == "" {
		modelRef = s.config.DefaultModel
	}
	if modelRef == "" && req.Provider != "" {
		// Use provider's default model
		if pc, ok := s.config.Providers[req.Provider]; ok && pc.DefaultModel != "" {
			modelRef = pc.DefaultModel
		}
	}

	p, modelID, err := s.resolveProvider(modelRef)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Build agent options
	agentOpts := []agent.Option{
		agent.WithProvider(p, modelID),
	}

	if req.System != "" {
		agentOpts = append(agentOpts, agent.WithSystemPrompt(req.System))
	}
	if req.MaxTurns > 0 {
		agentOpts = append(agentOpts, agent.WithMaxTurns(req.MaxTurns))
	}

	a, err := agent.New(req.Name, agentOpts...)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("failed to create agent: %v", err))
		return
	}

	// Store the agent definition
	entry := &agentEntry{
		Name:     req.Name,
		Provider: p.Name(),
		Model:    modelID,
		System:   req.System,
		MaxTurns: req.MaxTurns,
	}
	s.agentStore.Store(a.ID(), entry)

	// If input is provided, run immediately
	if req.Input != "" {
		result, err := a.Run(r.Context(), req.Input)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("agent execution failed: %v", err))
			return
		}

		resp := map[string]any{
			"agent_id": a.ID(),
			"agent": map[string]any{
				"name":     a.Name(),
				"provider": p.Name(),
				"model":    modelID,
			},
			"result": result,
		}
		data, _ := json.Marshal(resp)
		writeJSON(w, http.StatusOK, data)
		return
	}

	// Just return the agent info
	resp := map[string]any{
		"agent_id": a.ID(),
		"agent": map[string]any{
			"name":     a.Name(),
			"provider": p.Name(),
			"model":    modelID,
		},
	}
	data, _ := json.Marshal(resp)
	writeJSON(w, http.StatusCreated, data)
}

// handleListAgents lists all stored agents.
func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	var agents []map[string]any
	s.agentStore.Range(func(key, value any) bool {
		entry := value.(*agentEntry)
		agents = append(agents, map[string]any{
			"id":        key,
			"name":      entry.Name,
			"provider":  entry.Provider,
			"model":     entry.Model,
			"max_turns": entry.MaxTurns,
		})
		return true
	})

	resp := map[string]any{
		"agents": agents,
		"count":  len(agents),
	}
	data, _ := json.Marshal(resp)
	writeJSON(w, http.StatusOK, data)
}

// runAgentRequest is the body for POST /v1/agents/{id}/run.
type runAgentRequest struct {
	Input string `json:"input"`
}

// handleRunAgent runs a stored agent with the given input.
func (s *Server) handleRunAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "agent id is required")
		return
	}

	val, ok := s.agentStore.Load(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("agent %q not found", id))
		return
	}
	entry := val.(*agentEntry)

	body, err := readBody(r, 10<<20)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var req runAgentRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	if req.Input == "" {
		writeError(w, http.StatusBadRequest, "input is required")
		return
	}

	// Reconstruct the agent
	p, modelID, err := s.resolveProvider(entry.Model)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to resolve provider: %v", err))
		return
	}

	agentOpts := []agent.Option{
		agent.WithProvider(p, modelID),
	}
	if entry.System != "" {
		agentOpts = append(agentOpts, agent.WithSystemPrompt(entry.System))
	}
	if entry.MaxTurns > 0 {
		agentOpts = append(agentOpts, agent.WithMaxTurns(entry.MaxTurns))
	}

	a, err := agent.New(entry.Name, agentOpts...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create agent: %v", err))
		return
	}

	result, err := a.Run(r.Context(), req.Input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("agent execution failed: %v", err))
		return
	}

	data, _ := json.Marshal(result)
	writeJSON(w, http.StatusOK, data)
}

// handleDeleteAgent removes a stored agent.
func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "agent id is required")
		return
	}

	_, ok := s.agentStore.LoadAndDelete(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("agent %q not found", id))
		return
	}

	writeJSON(w, http.StatusNoContent, nil)
}

// handleRunAgentStream streams an agent run via Server-Sent Events.
func (s *Server) handleRunAgentStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "agent id is required")
		return
	}

	val, ok := s.agentStore.Load(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("agent %q not found", id))
		return
	}
	entry := val.(*agentEntry)

	body, err := readBody(r, 10<<20)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var req runAgentRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	if req.Input == "" {
		writeError(w, http.StatusBadRequest, "input is required")
		return
	}

	p, modelID, err := s.resolveProvider(entry.Model)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to resolve provider: %v", err))
		return
	}

	agentOpts := []agent.Option{
		agent.WithProvider(p, modelID),
	}
	if entry.System != "" {
		agentOpts = append(agentOpts, agent.WithSystemPrompt(entry.System))
	}
	if entry.MaxTurns > 0 {
		agentOpts = append(agentOpts, agent.WithMaxTurns(entry.MaxTurns))
	}

	a, err := agent.New(entry.Name, agentOpts...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create agent: %v", err))
		return
	}

	eventCh, err := a.Stream(r.Context(), req.Input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("agent stream failed: %v", err))
		return
	}

	// SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, canFlush := w.(http.Flusher)

	for event := range eventCh {
		data, err := json.Marshal(event)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		if canFlush {
			flusher.Flush()
		}
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	if canFlush {
		flusher.Flush()
	}
}

// ---------------------------------------------------------------------------
// Workflows
// ---------------------------------------------------------------------------

// workflowRequest is the JSON body for POST /v1/workflows.
type workflowRequest struct {
	Name  string         `json:"name"`
	Steps []stepDef      `json:"steps"`
	Edges []edgeDef      `json:"edges,omitempty"`
	Input map[string]any `json:"input"`
}

// stepDef defines a single step in a workflow request.
type stepDef struct {
	ID       string `json:"id"`
	Agent    string `json:"agent"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	System   string `json:"system_prompt,omitempty"`
	MaxTurns int    `json:"max_turns,omitempty"`
}

// edgeDef defines a directed edge between steps.
type edgeDef struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Condition string `json:"condition,omitempty"`
}

// handleExecuteWorkflow handles POST /v1/workflows — create and execute a
// workflow from a JSON definition.
func (s *Server) handleExecuteWorkflow(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r, 10<<20)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var req workflowRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	if len(req.Steps) == 0 {
		writeError(w, http.StatusBadRequest, "at least one step is required")
		return
	}

	workflow, err := s.buildWorkflow(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	engine := orchestration.NewEngine(
		orchestration.WithLogger(s.logger),
	)

	result, err := engine.Execute(r.Context(), workflow, req.Input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("workflow execution failed: %v", err))
		return
	}

	data, _ := json.Marshal(result)
	writeJSON(w, http.StatusOK, data)
}

// handleStreamWorkflow handles POST /v1/workflows/stream — stream workflow execution via SSE.
func (s *Server) handleStreamWorkflow(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r, 10<<20)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var req workflowRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	if len(req.Steps) == 0 {
		writeError(w, http.StatusBadRequest, "at least one step is required")
		return
	}

	workflow, err := s.buildWorkflow(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	engine := orchestration.NewEngine(
		orchestration.WithLogger(s.logger),
	)

	events, err := engine.Stream(r.Context(), workflow, req.Input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("workflow stream failed: %v", err))
		return
	}

	// SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, canFlush := w.(http.Flusher)

	for event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		if canFlush {
			flusher.Flush()
		}
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	if canFlush {
		flusher.Flush()
	}
}

// ---------------------------------------------------------------------------
// Internal Helpers
// ---------------------------------------------------------------------------

// resolveProvider resolves a model reference to a Provider and model ID.
func (s *Server) resolveProvider(modelRef string) (provider.Provider, string, error) {
	if modelRef == "" {
		modelRef = s.config.DefaultModel
	}
	if modelRef == "" {
		// Try default provider
		if s.config.DefaultProvider != "" {
			p, err := s.registry.Get(s.config.DefaultProvider)
			if err != nil {
				return nil, "", fmt.Errorf("default provider %q not available: %w", s.config.DefaultProvider, err)
			}
			// Get default model from config
			if pc, ok := s.config.Providers[s.config.DefaultProvider]; ok && pc.DefaultModel != "" {
				return p, pc.DefaultModel, nil
			}
			return p, "", fmt.Errorf("no default model configured for provider %q", s.config.DefaultProvider)
		}
		return nil, "", fmt.Errorf("no model specified and no default configured")
	}

	p, modelID, err := s.registry.Resolve(modelRef)
	if err != nil {
		return nil, "", fmt.Errorf("cannot resolve model %q: %w", modelRef, err)
	}
	return p, modelID, nil
}

// convertMessages converts messageRequests to internal Message types.
func convertMessages(msgs []messageRequest) []message.Message {
	result := make([]message.Message, 0, len(msgs))
	for _, m := range msgs {
		var role message.Role
		switch strings.ToLower(m.Role) {
		case "system":
			role = message.RoleSystem
		case "user":
			role = message.RoleUser
		case "assistant":
			role = message.RoleAssistant
		case "tool":
			role = message.RoleTool
		case "function":
			role = message.RoleFunction
		default:
			role = message.RoleUser
		}

		msg := message.TextMessage(role, m.Content)
		msg.Name = m.Name
		if len(m.ToolCalls) > 0 {
			msg.ToolCalls = m.ToolCalls
		}
		if m.ToolResult != nil {
			msg.ToolResult = &message.ToolResult{
				ToolCallID: m.ToolResult.ToolCallID,
				Content:    m.ToolResult.Content,
				IsError:    m.ToolResult.IsError,
			}
		}
		result = append(result, msg)
	}
	return result
}

// buildGenerateOptions converts the request options to provider.GenerateOptions.
func buildGenerateOptions(opts *generateOptionsRequest) provider.GenerateOptions {
	if opts == nil {
		return provider.NewGenerateOptions()
	}

	var genOptAppliers []provider.GenerateOption
	if opts.Temperature != nil {
		genOptAppliers = append(genOptAppliers, provider.WithTemperature(*opts.Temperature))
	}
	if opts.TopP != nil {
		genOptAppliers = append(genOptAppliers, provider.WithTopP(*opts.TopP))
	}
	if opts.MaxTokens != nil {
		genOptAppliers = append(genOptAppliers, provider.WithMaxTokens(*opts.MaxTokens))
	}
	if opts.Seed != nil {
		genOptAppliers = append(genOptAppliers, provider.WithSeed(*opts.Seed))
	}
	if len(opts.StopSequences) > 0 {
		genOptAppliers = append(genOptAppliers, provider.WithStopSequences(opts.StopSequences...))
	}
	switch strings.ToLower(opts.ResponseFormat) {
	case "json":
		genOptAppliers = append(genOptAppliers, provider.WithJSONMode())
	case "text":
		genOptAppliers = append(genOptAppliers, provider.WithTextMode())
	}

	return provider.NewGenerateOptions(genOptAppliers...)
}

// buildWorkflow constructs an orchestration.Workflow from a request definition.
func (s *Server) buildWorkflow(req workflowRequest) (*orchestration.Workflow, error) {
	wf := orchestration.NewWorkflow(req.Name)

	// Create agents for each step and add as workflow steps
	for _, sd := range req.Steps {
		modelRef := sd.Model
		if modelRef == "" {
			modelRef = s.config.DefaultModel
		}

		p, modelID, err := s.resolveProvider(modelRef)
		if err != nil {
			return nil, fmt.Errorf("step %q: %w", sd.ID, err)
		}

		agentOpts := []agent.Option{
			agent.WithProvider(p, modelID),
		}
		if sd.System != "" {
			agentOpts = append(agentOpts, agent.WithSystemPrompt(sd.System))
		}
		if sd.MaxTurns > 0 {
			agentOpts = append(agentOpts, agent.WithMaxTurns(sd.MaxTurns))
		}

		a, err := agent.New(sd.Agent, agentOpts...)
		if err != nil {
			return nil, fmt.Errorf("step %q: failed to create agent %q: %w", sd.ID, sd.Agent, err)
		}

		step := &orchestration.Step{
			ID:    sd.ID,
			Agent: a,
			InputMap: func(ctx *orchestration.WorkflowContext) (string, error) {
				// Try to get input from workflow context
				if topic, ok := ctx.Get("topic").(string); ok && topic != "" {
					return topic, nil
				}
				// Try generic input
				if input, ok := ctx.Get("input").(string); ok && input != "" {
					return input, nil
				}
				return "", fmt.Errorf("no input found in workflow context")
			},
		}

		if err := wf.AddStep(step); err != nil {
			return nil, fmt.Errorf("failed to add step %q: %w", sd.ID, err)
		}
	}

	// Add edges
	for _, ed := range req.Edges {
		if err := wf.AddEdge(ed.From, ed.To, nil); err != nil {
			return nil, fmt.Errorf("failed to add edge %q -> %q: %w", ed.From, ed.To, err)
		}
	}

	return wf, nil
}
