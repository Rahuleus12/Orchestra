package openrouter

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/user/orchestra/internal/provider"
)

// ---------------------------------------------------------------------------
// CostBudget — budget enforcement configuration
// ---------------------------------------------------------------------------

// CostBudget configures budget limits for OpenRouter API usage.
type CostBudget struct {
	// MaxCostPerRequest is the maximum cost allowed for a single request (USD).
	// 0 means no limit.
	MaxCostPerRequest float64

	// MaxCostPerSession is the maximum cumulative cost for the provider
	// instance lifetime (USD). 0 means no limit.
	MaxCostPerSession float64
}

// parseBudgetConfig extracts CostBudget from the Extra config map.
func parseBudgetConfig(extra map[string]any) CostBudget {
	var budget CostBudget

	if budgetMap, ok := extra["cost_budget"].(map[string]any); ok {
		if v, ok := asFloat64(budgetMap["max_cost_per_request"]); ok {
			budget.MaxCostPerRequest = v
		}
		if v, ok := asFloat64(budgetMap["max_cost_per_session"]); ok {
			budget.MaxCostPerSession = v
		}
	}

	return budget
}

// ---------------------------------------------------------------------------
// CostTracker — tracks cumulative costs
// ---------------------------------------------------------------------------

// CostTracker tracks cumulative costs for the OpenRouter provider.
// It is safe for concurrent use.
type CostTracker struct {
	mu      sync.Mutex
	entries []CostEntry
	total   float64
}

// CostEntry records the cost of a single request.
type CostEntry struct {
	// Model is the model used for this request.
	Model string
	// Usage is the token usage for this request.
	Usage provider.TokenUsage
	// CostUSD is the estimated cost in USD.
	CostUSD float64
	// Timestamp is when this entry was recorded.
	Timestamp time.Time
}

// CostReport is a summary of cumulative costs.
type CostReport struct {
	// TotalCost is the cumulative cost in USD across all requests.
	TotalCost float64
	// TotalTokens is the cumulative token usage.
	TotalTokens provider.TokenUsage
	// RequestCount is the total number of cost-tracked requests.
	RequestCount int
	// ByModel is a breakdown of costs per model.
	ByModel map[string]ModelCostSummary
}

// ModelCostSummary is a cost summary for a specific model.
type ModelCostSummary struct {
	CostUSD          float64
	RequestCount     int
	PromptTokens     int
	CompletionTokens int
}

// NewCostTracker creates a new CostTracker.
func NewCostTracker() *CostTracker {
	return &CostTracker{
		entries: make([]CostEntry, 0),
	}
}

// Record records the cost of a completed request.
func (t *CostTracker) Record(model string, usage provider.TokenUsage, costUSD float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.entries = append(t.entries, CostEntry{
		Model:     model,
		Usage:     usage,
		CostUSD:   costUSD,
		Timestamp: time.Now(),
	})
	t.total += costUSD
}

// Report returns a summary of all tracked costs.
func (t *CostTracker) Report() CostReport {
	t.mu.Lock()
	defer t.mu.Unlock()

	report := CostReport{
		TotalCost:    t.total,
		RequestCount: len(t.entries),
		ByModel:      make(map[string]ModelCostSummary),
	}

	for _, entry := range t.entries {
		report.TotalTokens = report.TotalTokens.Add(entry.Usage)

		summary := report.ByModel[entry.Model]
		summary.CostUSD += entry.CostUSD
		summary.RequestCount++
		summary.PromptTokens += entry.Usage.PromptTokens
		summary.CompletionTokens += entry.Usage.CompletionTokens
		report.ByModel[entry.Model] = summary
	}

	return report
}

// Reset clears all tracked costs.
func (t *CostTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.entries = make([]CostEntry, 0)
	t.total = 0
}

// ---------------------------------------------------------------------------
// Cost Formatting
// ---------------------------------------------------------------------------

// FormatCost formats a USD cost value for display.
func FormatCost(cost float64) string {
	switch {
	case cost < 0.0001:
		return fmt.Sprintf("$%.6f", cost)
	case cost < 0.01:
		return fmt.Sprintf("$%.4f", cost)
	case cost < 1.0:
		return fmt.Sprintf("$%.4f", cost)
	default:
		return fmt.Sprintf("$%.2f", cost)
	}
}

// FormatCostReport formats a CostReport for display.
func FormatCostReport(report CostReport) string {
	if report.RequestCount == 0 {
		return "No costs tracked."
	}

	result := fmt.Sprintf(
		"Total Cost: %s (%d requests, %d prompt + %d completion tokens)\n",
		FormatCost(report.TotalCost),
		report.RequestCount,
		report.TotalTokens.PromptTokens,
		report.TotalTokens.CompletionTokens,
	)

	if len(report.ByModel) > 0 {
		// Sort models by cost descending
		models := make([]string, 0, len(report.ByModel))
		for m := range report.ByModel {
			models = append(models, m)
		}
		sort.Slice(models, func(i, j int) bool {
			return report.ByModel[models[i]].CostUSD > report.ByModel[models[j]].CostUSD
		})

		result += "\nBy Model:\n"
		for _, model := range models {
			s := report.ByModel[model]
			result += fmt.Sprintf("  %-40s %s (%d requests)\n", model, FormatCost(s.CostUSD), s.RequestCount)
		}
	}

	return result
}
