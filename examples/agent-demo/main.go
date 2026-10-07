// Command agent-demo is a live smoke test for the Orchestra agent stack.
//
// It registers an OpenAI-compatible provider pointed at OpenRouter
// (https://openrouter.ai/api/v1), auto-discovers a free model for the
// supplied API key, then creates an agent and exercises three interaction
// paths:
//
//  1. A plain Generate round trip (agent.Run)
//  2. The tool-calling loop with a calculator tool (agent.Run)
//  3. Streaming output (agent.Stream)
//
// Usage:
//
//	OPENROUTER_API_KEY=sk-or-v1-... go run ./examples/agent-demo
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/user/orchestra/internal/agent"
	"github.com/user/orchestra/internal/config"
	"github.com/user/orchestra/internal/provider"
	openai "github.com/user/orchestra/internal/provider/openai"
)

const openrouterBaseURL = "https://openrouter.ai/api/v1"

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("\nAll checks passed — agent created and interacted with successfully.")
}

func run(ctx context.Context) error {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("OPENROUTER_API_KEY environment variable is required")
	}

	model, err := pickFreeModel(ctx, apiKey)
	if err != nil {
		return fmt.Errorf("pick free model: %w", err)
	}
	fmt.Printf("Using free model: %s\n", model)

	p, err := openai.NewProvider(config.ProviderConfig{
		APIKey:       apiKey,
		BaseURL:      openrouterBaseURL,
		DefaultModel: model,
	})
	if err != nil {
		return fmt.Errorf("create provider: %w", err)
	}

	a, err := agent.New("orchestra-demo",
		agent.WithProvider(p, model),
		agent.WithSystemPrompt("You are Orchestra-1, a concise assistant. Always use the calculator tool for any arithmetic, no matter how simple."),
		agent.WithTools(calculatorTool()),
		agent.WithMaxTurns(5),
		agent.WithGenerateOptions(provider.WithMaxTokens(4096)),
	)
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	fmt.Printf("Agent created: id=%s name=%s model=%s tools=%v maxTurns=%d\n\n",
		a.ID(), a.Name(), a.Model(), a.HasTools(), a.MaxTurns())

	// Test 1: plain generate round trip.
	if err := step("1. agent.Run — plain question", func() error {
		res, err := a.Run(ctx, "What is 2 + 2? Answer in one short sentence.")
		if err != nil {
			return err
		}
		fmt.Printf("   reply:   %s\n", strings.TrimSpace(res.FinalText()))
		fmt.Printf("   usage:   %+v (turns=%d, duration=%s)\n", res.Usage, res.Turns, res.Duration.Truncate(time.Millisecond))
		return nil
	}); err != nil {
		return err
	}

	// Test 2: tool-calling loop.
	if err := step("2. agent.Run — calculator tool call", func() error {
		res, err := a.Run(ctx, "Use the calculator tool to compute (18*73) + (941/31), then tell me the exact result.")
		if err != nil {
			return err
		}
		for _, tc := range res.ToolCalls {
			fmt.Printf("   tool:    %s(%s) -> %s (err=%v)\n", tc.Call.Function.Name, tc.Call.Function.Arguments, tc.Result.Content, tc.Error)
		}
		if !res.HasToolCalls() {
			fmt.Println("   note:    model answered without calling the tool")
		}
		fmt.Printf("   reply:   %s\n", strings.TrimSpace(res.FinalText()))
		fmt.Printf("   usage:   %+v (turns=%d, duration=%s)\n", res.Usage, res.Turns, res.Duration.Truncate(time.Millisecond))
		return nil
	}); err != nil {
		return err
	}

	// Test 3: streaming.
	if err := step("3. agent.Stream — streaming haiku", func() error {
		events, err := a.Stream(ctx, "Write a haiku about orchestras of AI agents.")
		if err != nil {
			return err
		}
		fmt.Print("   reply:   ")
		var chunks int
		var usage *provider.TokenUsage
		for evt := range events {
			switch evt.Type {
			case agent.EventGenerateChunk:
				fmt.Print(evt.Chunk)
				chunks++
			case agent.EventError:
				return fmt.Errorf("stream event error: %w", evt.Error)
			case agent.EventDone:
				usage = evt.Usage
			}
		}
		fmt.Printf("\n   stream:  %d chunks, usage=%+v\n", chunks, usage)
		if chunks == 0 {
			return fmt.Errorf("received no stream chunks")
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}

func step(name string, fn func() error) error {
	fmt.Printf("[%s]\n", name)
	start := time.Now()
	if err := fn(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	fmt.Printf("   PASS (%s)\n\n", time.Since(start).Round(time.Millisecond))
	return nil
}

// pickFreeModel queries OpenRouter's model catalog and selects a free model,
// preferring ones that advertise tool-calling support.
func pickFreeModel(ctx context.Context, apiKey string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openrouterBaseURL+"/models", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("models endpoint returned %d: %s", resp.StatusCode, truncate(string(body), 300))
	}

	var catalog struct {
		Data []struct {
			ID                  string   `json:"id"`
			SupportedParameters []string `json:"supported_parameters"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &catalog); err != nil {
		return "", fmt.Errorf("parse models: %w", err)
	}

	var free, freeWithTools []string
	for _, m := range catalog.Data {
		if !strings.HasSuffix(m.ID, ":free") {
			continue
		}
		free = append(free, m.ID)
		for _, p := range m.SupportedParameters {
			if p == "tools" {
				freeWithTools = append(freeWithTools, m.ID)
				break
			}
		}
	}
	if len(free) == 0 {
		return "", fmt.Errorf("no free models available for this key")
	}
	if model := os.Getenv("OPENROUTER_MODEL"); model != "" {
		for _, id := range append(freeWithTools, free...) {
			if id == model {
				return id, nil
			}
		}
		return model, nil
	}
	// Prefer fast, instruct-style models that also advertise tool support;
	// avoid small reasoning models that spend all their tokens thinking.
	preferences := []string{
		"nemotron-3.5-lightning",
		"gemma-4-31b-it",
		"inkling-small",
		"nemotron-3-super",
		"gemma-4-26b",
		"lfm-2.5",
	}
	for _, pref := range preferences {
		for _, id := range freeWithTools {
			if strings.Contains(id, pref) {
				return id, nil
			}
		}
	}
	if len(freeWithTools) > 0 {
		sort.Strings(freeWithTools)
		return freeWithTools[0], nil
	}
	sort.Strings(free)
	fmt.Println("warning: no free model advertises tool support; tool test may be skipped by the model")
	return free[0], nil
}

// calculatorTool returns an agent tool that evaluates arithmetic expressions.
func calculatorTool() agent.Tool {
	return agent.NewToolFuncWithSchema(
		"calculator",
		"Evaluate a basic arithmetic expression with +, -, *, / and parentheses.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"expression": map[string]any{
					"type":        "string",
					"description": "The arithmetic expression to evaluate, e.g. \"231 * 47\".",
				},
			},
			"required": []string{"expression"},
		},
		func(_ context.Context, args string) (string, error) {
			var req struct {
				Expression string `json:"expression"`
			}
			if err := json.Unmarshal([]byte(args), &req); err != nil {
				return "", fmt.Errorf("parse arguments: %w", err)
			}
			result, err := evalArithmetic(req.Expression)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%s = %s", req.Expression, formatNumber(result)), nil
		},
	)
}

func formatNumber(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%g", f)
}

// ---------------------------------------------------------------------------
// Minimal recursive-descent arithmetic evaluator (+ - * / and parentheses).
// ---------------------------------------------------------------------------

type arithParser struct {
	src string
	pos int
}

func evalArithmetic(expr string) (float64, error) {
	p := &arithParser{src: strings.ReplaceAll(expr, " ", "")}
	v, err := p.parseExpr()
	if err != nil {
		return 0, err
	}
	if p.pos != len(p.src) {
		return 0, fmt.Errorf("unexpected %q in expression", p.src[p.pos:])
	}
	return v, nil
}

func (p *arithParser) parseExpr() (float64, error) {
	v, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for p.pos < len(p.src) && (p.src[p.pos] == '+' || p.src[p.pos] == '-') {
		op := p.src[p.pos]
		p.pos++
		rhs, err := p.parseTerm()
		if err != nil {
			return 0, err
		}
		if op == '+' {
			v += rhs
		} else {
			v -= rhs
		}
	}
	return v, nil
}

func (p *arithParser) parseTerm() (float64, error) {
	v, err := p.parseFactor()
	if err != nil {
		return 0, err
	}
	for p.pos < len(p.src) && (p.src[p.pos] == '*' || p.src[p.pos] == '/') {
		op := p.src[p.pos]
		p.pos++
		rhs, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		if op == '*' {
			v *= rhs
		} else {
			if rhs == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			v /= rhs
		}
	}
	return v, nil
}

func (p *arithParser) parseFactor() (float64, error) {
	if p.pos >= len(p.src) {
		return 0, fmt.Errorf("unexpected end of expression")
	}
	if p.src[p.pos] == '(' {
		p.pos++
		v, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		if p.pos >= len(p.src) || p.src[p.pos] != ')' {
			return 0, fmt.Errorf("missing closing parenthesis")
		}
		p.pos++
		return v, nil
	}
	if p.src[p.pos] == '-' {
		p.pos++
		v, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		return -v, nil
	}
	start := p.pos
	for p.pos < len(p.src) && (p.src[p.pos] >= '0' && p.src[p.pos] <= '9' || p.src[p.pos] == '.') {
		p.pos++
	}
	if start == p.pos {
		return 0, fmt.Errorf("expected number at %q", p.src[start:])
	}
	var v float64
	if _, err := fmt.Sscanf(p.src[start:p.pos], "%g", &v); err != nil {
		return 0, fmt.Errorf("parse number %q: %w", p.src[start:p.pos], err)
	}
	return v, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
