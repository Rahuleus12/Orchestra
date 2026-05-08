# Phase 12 Report: OpenRouter Provider & Model Discovery

**Status:** ✅ Complete  
**Start Date:** 2025-01-17  
**End Date:** 2025-01-17  

---

## Executive Summary

Phase 12 implements a first-class OpenRouter provider for Orchestra, providing access to 100+ LLM models through a single API key. OpenRouter acts as a unified gateway to models from OpenAI, Anthropic, Google, Meta, Mistral, and many others — all accessible through an OpenAI-compatible API surface. This phase covers the provider implementation, dynamic model discovery with rich metadata (pricing, context windows, capabilities), cost-aware routing with budget enforcement, provider routing hints, and comprehensive testing.

## Completed Tasks

### 12.1 OpenRouter Provider Implementation ✅

| Task | Status | Notes |
|------|--------|-------|
| Create `internal/provider/openrouter/openrouter.go` | ✅ | Full Provider interface implementation |
| Implement `Generate` via OpenAI-compatible endpoint | ✅ | `/chat/completions` with OpenRouter headers |
| Implement `Stream` via SSE streaming | ✅ | Identical wire format to OpenAI streaming |
| OpenRouter-specific HTTP headers | ✅ | `Authorization`, `HTTP-Referer`, `X-Title` |
| Handle OpenRouter response fields | ✅ | Usage, model ID mapping, error format |
| Handle error responses | ✅ | ProviderError with codes and status |
| Implement `Capabilities()` | ✅ | Dynamic from catalog + heuristic fallback |
| Standalone provider (not OpenAI subclass) | ✅ | Per TDR-007 decision |

### 12.2 Dynamic Model Discovery & Catalog ✅

| Task | Status | Notes |
|------|--------|-------|
| Implement `Models()` fetching from `/api/v1/models` | ✅ | Full catalog with rich metadata |
| OpenRouter model list response types | ✅ | `orModelResponse`, `orModel`, `orModelPricing`, etc. |
| Map OpenRouter → Orchestra `ModelInfo` | ✅ | Pricing, capabilities, modality, tokenizer |
| TTL-based model catalog caching | ✅ | `sync.RWMutex` + configurable TTL (default 5m) |
| Helper for model capability lookup | ✅ | `LookupCapabilities()` from cached catalog |
| Helper for model pricing lookup | ✅ | `LookupPricing()` from cached catalog |

### 12.3 Model Information CLI Command ✅

| Task | Status | Notes |
|------|--------|-------|
| `orchestra models` CLI subcommand | ✅ | Registered in main.go |
| `--provider` flag | ✅ | Defaults to "openrouter" |
| `--query` flag for filtering | ✅ | By model name/ID |
| `--sort` flag | ✅ | By name, cost, context |
| `--details` flag | ✅ | Show specific model details |
| `--json` flag | ✅ | JSON output support |
| `--help` flag | ✅ | Full usage documentation |

### 12.4 Configuration Integration ✅

| Task | Status | Notes |
|------|--------|-------|
| `openrouter` section in `orchestra.yaml` | ✅ | Full config with comments |
| Register OpenRouter provider factory | ✅ | `openrouter.Factory` available |
| `OPENROUTER_API_KEY` env var support | ✅ | Via `${OPENROUTER_API_KEY}` in config |
| `app_name` and `site_url` config | ✅ | For OpenRouter rankings |
| `model_cache_ttl` config | ✅ | Configurable cache duration |

### 12.5 Cost Tracking & Budget Enforcement ✅

| Task | Status | Notes |
|------|--------|-------|
| Parse pricing from model metadata | ✅ | Per-token prompt/completion costs |
| Track cumulative cost per request/session | ✅ | `CostTracker` with thread-safe recording |
| Budget enforcement middleware | ✅ | Per-request and per-session budget checks |
| Block requests exceeding budget | ✅ | Pre-request validation |
| Cost formatting utilities | ✅ | `FormatCost`, `FormatCostReport` |
| Per-model cost breakdowns | ✅ | `CostReport.ByModel` map |
| Cost metadata on results | ✅ | `estimated_cost_usd` in result metadata |

### 12.6 Provider Routing & Fallback ✅

| Task | Status | Notes |
|------|--------|-------|
| Provider routing hints in config | ✅ | `provider_order`, `fallback`, `data_collection` |
| Map routing to request parameters | ✅ | `orProviderPrefs` struct → `provider` field |
| Provider fallback configuration | ✅ | `allow_fallbacks` in request body |
| Data collection opt-out | ✅ | `data_collection: "deny"` support |

### 12.7 Contract Tests & Integration Tests ✅

| Task | Status | Notes |
|------|--------|-------|
| Provider construction tests | ✅ | API key validation, defaults, extra config |
| Model catalog parsing tests | ✅ | `convertModel`, `inferToolSupport`, `hasModality` |
| Model caching behavior tests | ✅ | TTL-based caching, single API call |
| Generate tests with mock server | ✅ | Text responses, tool calls, API errors |
| Stream tests with mock server | ✅ | SSE streaming, content assembly |
| Cost estimation tests | ✅ | Tracker, formatting, concurrent safety |
| Budget enforcement tests | ✅ | Session exceeded, no budget |
| Routing tests | ✅ | Provider prefs, config parsing |
| Message conversion tests | ✅ | All message roles and types |
| Header verification tests | ✅ | OpenRouter-specific headers |

## Project Structure Additions

```
├── cmd/
│   └── orchestra/
│       └── main.go                  # Updated: added `models` subcommand
├── configs/
│   └── orchestra.yaml               # Updated: added openrouter provider section
├── internal/
│   └── provider/
│       └── openrouter/              # NEW: OpenRouter provider
│           ├── openrouter.go        # Provider implementation (Generate, Stream, Models, Capabilities)
│           ├── models.go            # Model catalog types, fetching, TTL-based caching
│           ├── cost.go              # CostTracker, CostBudget, formatting
│           ├── routing.go           # Provider routing preferences configuration
│           └── openrouter_test.go   # Comprehensive tests (35+ test cases)
```

## Configuration Example

```yaml
providers:
  openrouter:
    api_key: ${OPENROUTER_API_KEY}
    base_url: https://openrouter.ai/api/v1
    default_model: openai/gpt-4o
    # Optional: app identification for OpenRouter rankings
    # app_name: orchestra
    # site_url: https://github.com/user/orchestra
    rate_limit:
      requests_per_minute: 60
      tokens_per_minute: 500000
    retry:
      max_attempts: 3
      initial_backoff: 1s
    # Optional: model catalog caching (default: 5m)
    # model_cache_ttl: 5m
    # Optional: cost budget enforcement
    # cost_budget:
    #   max_cost_per_request: 0.10    # USD
    #   max_cost_per_session: 5.00     # USD
    # Optional: provider routing preferences
    # routing:
    #   provider_order: ["anthropic", "openai"]
    #   fallback: true
    #   data_collection: "deny"
```

## Key Design Decisions

### TDR-007: Standalone Provider (Not OpenAI Wrapper)

OpenRouter is implemented as a standalone provider rather than wrapping the OpenAI provider, per these considerations:

1. **Dynamic model catalog** — OpenRouter fetches models with pricing/capability metadata, unlike OpenAI's static hardcoded list
2. **Unique features** — Cost tracking, budget enforcement, provider routing are OpenRouter-specific
3. **Error handling differs** — OpenRouter has distinct error codes (insufficient credits, model not found in catalog)
4. **Request headers differ** — `HTTP-Referer` and `X-Title` headers are OpenRouter-specific

Shared wire-format types (SSE parsing, message conversion) are independently implemented for clarity and maintainability.

## Test Coverage

```
=== Test Results ===
TestNewProvider_RequiresAPIKey           PASS
TestNewProvider_Defaults                 PASS
TestNewProvider_CustomBaseURL            PASS
TestNewProvider_ExtraConfig              PASS
TestFactory                              PASS
TestSetHeaders                           PASS
TestModelCache_BasicFlow                 PASS
TestModelCache_LookupCapabilities_Empty  PASS
TestModelCache_LookupPricing_Empty       PASS
TestConvertModel                         PASS
TestInferToolSupport (4 subtests)        PASS
TestHasModality (3 subtests)             PASS
TestParseModelPricing (3 subtests)       PASS
TestModels_MockServer                    PASS
TestModels_Caching                       PASS
TestGenerate_MockServer                  PASS
TestGenerate_RequiresMessages            PASS
TestGenerate_APIError                    PASS
TestGenerate_ToolCalls                   PASS
TestStream_MockServer                    PASS
TestStream_RequiresMessages              PASS
TestCapabilities_KnownModel              PASS
TestCapabilities_UnknownModel            PASS
TestCostTracker_BasicFlow                PASS
TestCostTracker_Reset                    PASS
TestCostTracker_Concurrent               PASS
TestCostFormatting (5 subtests)          PASS
TestFormatCostReport                     PASS
TestFormatCostReport_Empty               PASS
TestBudgetEnforcement_SessionExceeded    PASS
TestBudgetEnforcement_NoBudget           PASS
TestBuildProviderPrefs_Disabled          PASS
TestBuildProviderPrefs_Enabled           PASS
TestParseRoutingConfig (2 subtests)      PASS
TestConvertMessages                      PASS
```

## Usage Examples

### Programmatic Usage

```go
// Create OpenRouter provider
p, err := openrouter.NewProvider(config.ProviderConfig{
    APIKey:       os.Getenv("OPENROUTER_API_KEY"),
    DefaultModel: "openai/gpt-4o",
    Extra: map[string]any{
        "app_name": "my-app",
        "site_url": "https://myapp.com",
        "cost_budget": map[string]any{
            "max_cost_per_session": 5.00,
        },
    },
})

// List available models
models, _ := p.Models(ctx)
for _, m := range models {
    fmt.Printf("%-40s %s (ctx: %d)\n", m.ID, m.Name, m.Capabilities.ContextWindow)
}

// Generate completion
result, err := p.Generate(ctx, provider.GenerateRequest{
    Model:   "anthropic/claude-sonnet-4-20250514",
    Messages: []message.Message{...},
})

// Check costs
report := p.CostReport()
fmt.Println(openrouter.FormatCostReport(report))
```

### CLI Usage

```bash
# List models
orchestra models --provider openrouter

# Filter and sort
orchestra models --provider openrouter --query gpt --sort cost

# Model details
orchestra models --provider openrouter --details openai/gpt-4o
```

## Deliverables Summary

| Deliverable | Status |
|-------------|--------|
| OpenRouter provider implementing full `Provider` interface | ✅ |
| Dynamic model catalog with TTL-based caching and rich metadata | ✅ |
| `orchestra models` CLI command for model discovery | ✅ |
| Cost tracking with budget enforcement | ✅ |
| Provider routing and fallback configuration | ✅ |
| Full test suite with mock server (35+ test cases) | ✅ |
| Configuration documentation in `orchestra.yaml` | ✅ |

## Milestone Criteria

| Criteria | Status |
|----------|--------|
| `registry.Register("openrouter", openrouter.Factory, cfg)` works | ✅ |
| `Generate` and `Stream` produce correct results | ✅ (mock-tested) |
| `Models()` returns catalog with pricing, context windows, capabilities | ✅ |
| Model catalog caching reduces API calls (max 1 fetch per TTL) | ✅ |
| Cost tracking records per-request and per-session spend | ✅ |
| Budget enforcement blocks requests exceeding limits | ✅ |
| Provider routing hints propagated in request body | ✅ |
| All tests pass | ✅ |
| Full project test suite passes | ✅ |

## Conclusion

Phase 12 successfully implements the OpenRouter provider as a first-class citizen in Orchestra. Users can now access 100+ models through a single API key with rich metadata, cost tracking, and budget enforcement. The provider follows the established patterns from Phase 2 (Provider Integrations) while adding OpenRouter-specific features like dynamic model discovery, pricing metadata, and provider routing preferences.

The implementation is fully tested with mock HTTP servers, ensuring reliability without requiring an actual OpenRouter API key for unit tests. The cost tracking system provides transparent visibility into spending with per-model breakdowns and configurable budget limits.
