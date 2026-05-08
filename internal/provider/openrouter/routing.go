package openrouter

import "strings"

// ---------------------------------------------------------------------------
// RoutingConfig — OpenRouter provider routing preferences
// ---------------------------------------------------------------------------

// RoutingConfig controls how OpenRouter routes requests to underlying providers.
type RoutingConfig struct {
	// Enabled indicates whether routing preferences should be sent.
	Enabled bool

	// ProviderOrder is the preferred provider order for model routing.
	// E.g., ["anthropic", "openai"] means prefer Anthropic's implementation
	// when available, fall back to OpenAI.
	ProviderOrder []string

	// Fallback permits falling back to alternative providers if the
	// preferred ones are unavailable.
	Fallback bool

	// DataCollection controls training data collection policy.
	// Valid values: "allow", "deny".
	DataCollection string
}

// parseRoutingConfig extracts RoutingConfig from the Extra config map.
func parseRoutingConfig(extra map[string]any) RoutingConfig {
	var routing RoutingConfig

	routingMap, ok := extra["routing"].(map[string]any)
	if !ok {
		return routing
	}

	routing.Enabled = true

	if order, ok := routingMap["provider_order"]; ok {
		switch v := order.(type) {
		case []string:
			routing.ProviderOrder = v
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					routing.ProviderOrder = append(routing.ProviderOrder, s)
				}
			}
		}
	}

	if fb, ok := routingMap["fallback"].(bool); ok {
		routing.Fallback = fb
	}

	if dc, ok := routingMap["data_collection"].(string); ok {
		dc = strings.ToLower(dc)
		if dc == "allow" || dc == "deny" {
			routing.DataCollection = dc
		}
	}

	return routing
}
