// Package builtin provides built-in tools for the Orchestra tool system.
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Web Search Tool
// ---------------------------------------------------------------------------

// SearchBackend defines the type of search backend to use.
type SearchBackend string

const (
	// SerpAPIBackend uses Google search via SerpAPI.
	SerpAPIBackend SearchBackend = "serpapi"

	// BraveBackend uses Brave Search API.
	BraveBackend SearchBackend = "brave"

	// DuckDuckGoBackend uses DuckDuckGo (no API key required, limited results).
	DuckDuckGoBackend SearchBackend = "duckduckgo"

	// TavilyBackend uses Tavily Search API (optimized for AI agents).
	TavilyBackend SearchBackend = "tavily"
)

// WebSearchInput defines the input for the web_search tool.
type WebSearchInput struct {
	// Query is the search query string.
	Query string `json:"query" description:"The search query to look up"`

	// Count is the maximum number of results to return. Defaults to 10.
	Count int `json:"count,omitempty" description:"Maximum number of results to return" default:"10" min:"1" max:"50"`

	// Offset is the number of results to skip (for pagination). Defaults to 0.
	Offset int `json:"offset,omitempty" description:"Number of results to skip for pagination" default:"0" min:"0"`

	// SafeSearch enables safe search filtering. Defaults to true.
	SafeSearch bool `json:"safe_search,omitempty" description:"Enable safe search filtering" default:"true"`

	// Country restricts results to a specific country code (e.g., "us", "uk", "de").
	Country string `json:"country,omitempty" description:"Country code to restrict results (e.g., 'us', 'uk', 'de')" pattern:"^[a-z]{2}$"`

	// Language restricts results to a specific language code (e.g., "en", "es", "fr").
	Language string `json:"language,omitempty" description:"Language code for results (e.g., 'en', 'es', 'fr')" pattern:"^[a-z]{2}$"`

	// Freshness restricts results to a time period: "day", "week", "month", "year", or empty for any time.
	Freshness string `json:"freshness,omitempty" description:"Time restriction: 'day', 'week', 'month', 'year', or empty for any" enum:"day,week,month,year"`

	// Backend specifies which search backend to use. Defaults to the tool's configured default.
	Backend SearchBackend `json:"backend,omitempty" description:"Search backend to use (default: tool's configured backend)" enum:"serpapi,brave,duckduckgo,tavily"`
}

// WebSearchOutput defines the output of the web_search tool.
type WebSearchOutput struct {
	// Query is the normalized query that was searched.
	Query string `json:"query"`

	// Results contains the search results.
	Results []SearchResult `json:"results"`

	// TotalResults is the estimated total number of results (backend-dependent).
	TotalResults int `json:"total_results,omitempty"`

	// Backend is the backend that was used.
	Backend SearchBackend `json:"backend"`

	// DurationMs is the time taken to perform the search.
	DurationMs int64 `json:"duration_ms"`

	// Error contains an error message if the search failed.
	Error string `json:"error,omitempty"`
}

// SearchResult represents a single web search result.
type SearchResult struct {
	// Title is the title of the search result.
	Title string `json:"title"`

	// URL is the URL of the search result.
	URL string `json:"url"`

	// Snippet is a brief excerpt from the page.
	Snippet string `json:"snippet"`

	// Domain is the domain name of the result.
	Domain string `json:"domain,omitempty"`

	// Position is the 1-based position of this result in the search results.
	Position int `json:"position"`

	// Date is the publication date if available (RFC3339 format).
	Date string `json:"date,omitempty"`

	// Score is the relevance score if provided by the backend.
	Score float64 `json:"score,omitempty"`
}

// WebSearchTool implements the web_search built-in tool.
// It searches the web using configurable backends and returns structured results.
//
// Supported backends:
//   - SerpAPI: Google search results via SerpAPI (requires API key)
//   - Brave: Brave Search API (requires API key)
//   - Tavily: AI-optimized search API (requires API key)
//   - DuckDuckGo: Basic search without API key (limited results)
//
// API keys are configured via environment variables:
//   - SERPAPI_API_KEY
//   - BRAVE_API_KEY
//   - TAVILY_API_KEY
type WebSearchTool struct {
	// DefaultBackend is the backend to use when not specified in the query.
	DefaultBackend SearchBackend

	// APIKeys contains API keys for backends that require them.
	// Map keys are: "serpapi", "brave", "tavily"
	APIKeys map[string]string

	// Timeout is the default timeout for search requests. Defaults to 15 seconds.
	Timeout time.Duration

	// MaxTimeout is the maximum allowed timeout.
	MaxTimeout time.Duration

	// HTTPClient is the HTTP client to use for requests.
	// If nil, a default client is created.
	HTTPClient *http.Client

	// MaxResults is the maximum number of results that can be requested.
	// Requests for more results are capped at this value.
	MaxResults int

	// UserAgent is the User-Agent header to send with requests.
	UserAgent string
}

// NewWebSearchTool creates a web_search tool with default settings.
// It attempts to detect available API keys from environment variables
// and selects the best available backend.
func NewWebSearchTool() WebSearchTool {
	t := WebSearchTool{
		APIKeys:    make(map[string]string),
		Timeout:    15 * time.Second,
		MaxTimeout: 30 * time.Second,
		MaxResults: 50,
		UserAgent:  "Orchestra/1.0 (AI Agent Tool)",
	}

	// Detect API keys from environment
	if key := getEnv("SERPAPI_API_KEY"); key != "" {
		t.APIKeys["serpapi"] = key
	}
	if key := getEnv("BRAVE_API_KEY"); key != "" {
		t.APIKeys["brave"] = key
	}
	if key := getEnv("TAVILY_API_KEY"); key != "" {
		t.APIKeys["tavily"] = key
	}

	// Select default backend based on available keys
	if _, ok := t.APIKeys["tavily"]; ok {
		t.DefaultBackend = TavilyBackend
	} else if _, ok := t.APIKeys["serpapi"]; ok {
		t.DefaultBackend = SerpAPIBackend
	} else if _, ok := t.APIKeys["brave"]; ok {
		t.DefaultBackend = BraveBackend
	} else {
		t.DefaultBackend = DuckDuckGoBackend
	}

	return t
}

// NewWebSearchToolWithBackend creates a web_search tool with a specific default backend.
func NewWebSearchToolWithBackend(backend SearchBackend) WebSearchTool {
	t := NewWebSearchTool()
	t.DefaultBackend = backend
	return t
}

// Name returns the tool's identifier.
func (t WebSearchTool) Name() string { return "web_search" }

// Description returns the tool's description for the LLM.
func (t WebSearchTool) Description() string {
	return `Search the web for information and return structured results.

This tool performs web searches and returns results including titles, URLs,
and snippets. Use it to find current information, documentation, news,
or any content available on the web.

Features:
- Multiple search backends (SerpAPI, Brave, Tavily, DuckDuckGo)
- Configurable result count and pagination
- Country and language filtering
- Time-based freshness filtering (day, week, month, year)
- Safe search option

Common use cases:
- Look up current events or news
- Find documentation for libraries or APIs
- Research topics or concepts
- Verify facts or get recent data
- Find tutorials or examples

Tips for effective searches:
- Be specific with your queries
- Use quotes for exact phrases: "machine learning"
- Use minus to exclude terms: python -snake
- For technical topics, include version numbers
- Use freshness to get recent results

Note: The available backend depends on configuration. DuckDuckGo works
without an API key but may have limited results.`
}

// Parameters returns the JSON Schema for the tool's input.
func (t WebSearchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "The search query to look up"
			},
			"count": {
				"type": "integer",
				"description": "Maximum number of results to return",
				"default": 10,
				"minimum": 1,
				"maximum": 50
			},
			"offset": {
				"type": "integer",
				"description": "Number of results to skip for pagination",
				"default": 0,
				"minimum": 0
			},
			"safe_search": {
				"type": "boolean",
				"description": "Enable safe search filtering",
				"default": true
			},
			"country": {
				"type": "string",
				"description": "Country code to restrict results (e.g., 'us', 'uk', 'de')"
			},
			"language": {
				"type": "string",
				"description": "Language code for results (e.g., 'en', 'es', 'fr')"
			},
			"freshness": {
				"type": "string",
				"description": "Time restriction: 'day', 'week', 'month', 'year', or empty for any",
				"enum": ["day", "week", "month", "year"]
			},
			"backend": {
				"type": "string",
				"description": "Search backend to use (default: tool's configured backend)",
				"enum": ["serpapi", "brave", "duckduckgo", "tavily"]
			}
		},
		"required": ["query"]
	}`)
}

// Execute performs the web search.
func (t WebSearchTool) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req WebSearchInput
	if err := json.Unmarshal(input, &req); err != nil {
		return marshalWebSearchError(fmt.Errorf("parse input: %w", err))
	}

	if req.Query == "" {
		return marshalWebSearchError(fmt.Errorf("query is required"))
	}

	// Check context
	if ctx.Err() != nil {
		return marshalWebSearchError(ctx.Err())
	}

	// Apply defaults
	if req.Count <= 0 {
		req.Count = 10
	}
	if req.Count > t.MaxResults {
		req.Count = t.MaxResults
	}

	// Select backend
	backend := req.Backend
	if backend == "" {
		backend = t.DefaultBackend
	}

	// Create timeout context
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	if t.MaxTimeout > 0 && timeout > t.MaxTimeout {
		timeout = t.MaxTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Perform the search
	startTime := time.Now()
	results, totalResults, err := t.search(ctx, backend, req)
	duration := time.Since(startTime)

	output := WebSearchOutput{
		Query:      req.Query,
		Results:    results,
		Backend:    backend,
		DurationMs: duration.Milliseconds(),
	}

	if totalResults > 0 {
		output.TotalResults = totalResults
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			output.Error = "search timed out"
		} else {
			output.Error = err.Error()
		}
	}

	return json.Marshal(output)
}

// search dispatches to the appropriate backend implementation.
func (t WebSearchTool) search(ctx context.Context, backend SearchBackend, req WebSearchInput) ([]SearchResult, int, error) {
	switch backend {
	case SerpAPIBackend:
		return t.searchSerpAPI(ctx, req)
	case BraveBackend:
		return t.searchBrave(ctx, req)
	case TavilyBackend:
		return t.searchTavily(ctx, req)
	case DuckDuckGoBackend:
		return t.searchDuckDuckGo(ctx, req)
	default:
		return nil, 0, fmt.Errorf("unknown search backend: %q", backend)
	}
}

// ---------------------------------------------------------------------------
// SerpAPI Backend
// ---------------------------------------------------------------------------

// serpAPIResponse represents the SerpAPI JSON response.
type serpAPIResponse struct {
	OrganicResults    []serpAPIResult `json:"organic_results"`
	SearchInformation struct {
		TotalResults string `json:"total_results"`
	} `json:"search_information"`
	Error string `json:"error,omitempty"`
}

type serpAPIResult struct {
	Position int    `json:"position"`
	Title    string `json:"title"`
	Link     string `json:"link"`
	Snippet  string `json:"snippet"`
	Date     string `json:"date,omitempty"`
}

func (t WebSearchTool) searchSerpAPI(ctx context.Context, req WebSearchInput) ([]SearchResult, int, error) {
	apiKey, ok := t.APIKeys["serpapi"]
	if !ok || apiKey == "" {
		return nil, 0, fmt.Errorf("SerpAPI backend requires an API key; set SERPAPI_API_KEY environment variable or configure APIKeys['serpapi']")
	}

	// Build request URL
	params := url.Values{}
	params.Set("q", req.Query)
	params.Set("api_key", apiKey)
	params.Set("num", strconv.Itoa(req.Count))
	params.Set("start", strconv.Itoa(req.Offset))
	if req.SafeSearch {
		params.Set("safe", "active")
	}
	if req.Country != "" {
		params.Set("gl", req.Country)
	}
	if req.Language != "" {
		params.Set("hl", req.Language)
	}
	if req.Freshness != "" {
		params.Set("tbs", "qdr:"+req.Freshness)
	}

	searchURL := "https://serpapi.com/search?" + params.Encode()

	// Make request
	resp, err := t.doGet(ctx, searchURL)
	if err != nil {
		return nil, 0, fmt.Errorf("serpapi request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return nil, 0, fmt.Errorf("read serpapi response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("serpapi returned status %d: %s", resp.StatusCode, truncate(string(body), 500))
	}

	// Parse response
	var apiResp serpAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, 0, fmt.Errorf("parse serpapi response: %w", err)
	}

	if apiResp.Error != "" {
		return nil, 0, fmt.Errorf("serpapi error: %s", apiResp.Error)
	}

	// Convert results
	results := make([]SearchResult, 0, len(apiResp.OrganicResults))
	for _, r := range apiResp.OrganicResults {
		results = append(results, SearchResult{
			Title:    r.Title,
			URL:      r.Link,
			Snippet:  r.Snippet,
			Domain:   extractDomain(r.Link),
			Position: r.Position + 1,
			Date:     r.Date,
		})
	}

	// Parse total results
	totalResults := 0
	if apiResp.SearchInformation.TotalResults != "" {
		totalResults, _ = strconv.Atoi(strings.ReplaceAll(apiResp.SearchInformation.TotalResults, ",", ""))
	}

	return results, totalResults, nil
}

// ---------------------------------------------------------------------------
// Brave Search Backend
// ---------------------------------------------------------------------------

// braveResponse represents the Brave Search API response.
type braveResponse struct {
	Web struct {
		Results []braveResult `json:"results"`
		Total   int           `json:"total_results,omitempty"`
	} `json:"web"`
}

type braveResult struct {
	Title string  `json:"title"`
	URL   string  `json:"url"`
	Desc  string  `json:"description"`
	Age   string  `json:"age,omitempty"`
	Extra string  `json:"extra_snippets,omitempty"`
	Score float64 `json:"score,omitempty"`
}

func (t WebSearchTool) searchBrave(ctx context.Context, req WebSearchInput) ([]SearchResult, int, error) {
	apiKey, ok := t.APIKeys["brave"]
	if !ok || apiKey == "" {
		return nil, 0, fmt.Errorf("Brave backend requires an API key; set BRAVE_API_KEY environment variable or configure APIKeys['brave']")
	}

	// Build request URL
	params := url.Values{}
	params.Set("q", req.Query)
	params.Set("count", strconv.Itoa(req.Count))
	params.Set("offset", strconv.Itoa(req.Offset))
	if req.SafeSearch {
		params.Set("safesearch", "strict")
	} else {
		params.Set("safesearch", "off")
	}
	if req.Country != "" {
		params.Set("country", req.Country)
	}
	if req.Language != "" {
		params.Set("search_lang", req.Language)
	}
	if req.Freshness != "" {
		params.Set("freshness", req.Freshness)
	}

	searchURL := "https://api.search.brave.com/res/v1/web/search?" + params.Encode()

	// Make request
	httpReq, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create brave request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Accept-Encoding", "gzip")
	httpReq.Header.Set("X-Subscription-Token", apiKey)

	client := t.getHTTPClient()
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("brave request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, 0, fmt.Errorf("read brave response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("brave returned status %d: %s", resp.StatusCode, truncate(string(body), 500))
	}

	// Parse response
	var apiResp braveResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, 0, fmt.Errorf("parse brave response: %w", err)
	}

	// Convert results
	results := make([]SearchResult, 0, len(apiResp.Web.Results))
	for i, r := range apiResp.Web.Results {
		snippet := r.Desc
		if snippet == "" && r.Extra != "" {
			snippet = r.Extra
		}
		results = append(results, SearchResult{
			Title:    r.Title,
			URL:      r.URL,
			Snippet:  snippet,
			Domain:   extractDomain(r.URL),
			Position: req.Offset + i + 1,
			Date:     r.Age,
			Score:    r.Score,
		})
	}

	return results, apiResp.Web.Total, nil
}

// ---------------------------------------------------------------------------
// Tavily Backend
// ---------------------------------------------------------------------------

// tavilyResponse represents the Tavily Search API response.
type tavilyResponse struct {
	Results []tavilyResult `json:"results"`
}

type tavilyResult struct {
	Title      string  `json:"title"`
	URL        string  `json:"url"`
	Content    string  `json:"content"`
	Score      float64 `json:"score"`
	RawContent string  `json:"raw_content,omitempty"`
}

func (t WebSearchTool) searchTavily(ctx context.Context, req WebSearchInput) ([]SearchResult, int, error) {
	apiKey, ok := t.APIKeys["tavily"]
	if !ok || apiKey == "" {
		return nil, 0, fmt.Errorf("Tavily backend requires an API key; set TAVILY_API_KEY environment variable or configure APIKeys['tavily']")
	}

	// Build request body
	reqBody := map[string]any{
		"query":          req.Query,
		"max_results":    req.Count,
		"include_answer": false,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal tavily request: %w", err)
	}

	// Make request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.tavily.com/search", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, 0, fmt.Errorf("create tavily request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	client := t.getHTTPClient()
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("tavily request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, 0, fmt.Errorf("read tavily response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("tavily returned status %d: %s", resp.StatusCode, truncate(string(body), 500))
	}

	// Parse response
	var apiResp tavilyResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, 0, fmt.Errorf("parse tavily response: %w", err)
	}

	// Convert results
	results := make([]SearchResult, 0, len(apiResp.Results))
	for i, r := range apiResp.Results {
		results = append(results, SearchResult{
			Title:    r.Title,
			URL:      r.URL,
			Snippet:  r.Content,
			Domain:   extractDomain(r.URL),
			Position: i + 1,
			Score:    r.Score,
		})
	}

	return results, len(results), nil
}

// ---------------------------------------------------------------------------
// DuckDuckGo Backend (HTML scraping - no API key required)
// ---------------------------------------------------------------------------

func (t WebSearchTool) searchDuckDuckGo(ctx context.Context, req WebSearchInput) ([]SearchResult, int, error) {
	// Build request URL
	params := url.Values{}
	params.Set("q", req.Query)
	if req.SafeSearch {
		params.Set("safe", "on")
	}

	searchURL := "https://html.duckduckgo.com/html/?" + params.Encode()

	// Make request
	httpReq, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create duckduckgo request: %w", err)
	}
	httpReq.Header.Set("User-Agent", t.getUserAgent())

	client := t.getHTTPClient()
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("duckduckgo request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, 0, fmt.Errorf("read duckduckgo response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("duckduckgo returned status %d", resp.StatusCode)
	}

	// Parse HTML response (simplified parser)
	results := parseDuckDuckGoHTML(string(body), req.Count, req.Offset)

	return results, len(results), nil
}

// parseDuckDuckGoHTML extracts search results from DuckDuckGo HTML.
// This is a simplified parser that handles the basic HTML structure.
func parseDuckDuckGoHTML(html string, count, offset int) []SearchResult {
	var results []SearchResult

	// DuckDuckGo HTML results are in <a class="result__a"> for titles
	// and <a class="result__snippet"> for snippets
	// URLs are in the href of the result link (redirect URL)

	// Find all result blocks
	resultStart := strings.Index(html, "class=\"result__a\"")
	position := 0

	for resultStart != -1 && len(results) < count {
		// Extract the block containing this result
		blockEnd := strings.Index(html[resultStart:], "class=\"result__a\"")
		if blockEnd == -1 {
			blockEnd = len(html) - resultStart
		} else if blockEnd == 0 {
			blockEnd = len(html) - resultStart
		}
		block := html[resultStart : resultStart+blockEnd]

		// Find the next result boundary
		nextResult := strings.Index(html[resultStart+1:], "class=\"result__a\"")
		if nextResult != -1 {
			block = html[resultStart : resultStart+nextResult+1]
		}

		// Extract title and URL from <a class="result__a" href="...">
		title, href := extractDDGTitleAndURL(block)
		if title == "" || href == "" {
			// Move to next result
			nextIdx := strings.Index(html[resultStart+20:], "class=\"result__a\"")
			if nextIdx == -1 {
				break
			}
			resultStart = resultStart + 20 + nextIdx
			continue
		}

		// Extract snippet from <a class="result__snippet">
		snippet := extractDDGSnippet(block)

		position++
		if position <= offset {
			// Skip results for pagination
			nextIdx := strings.Index(html[resultStart+20:], "class=\"result__a\"")
			if nextIdx == -1 {
				break
			}
			resultStart = resultStart + 20 + nextIdx
			continue
		}

		// Clean up DuckDuckGo redirect URL to get actual URL
		actualURL := cleanDDGRedirectURL(href)

		results = append(results, SearchResult{
			Title:    cleanHTML(title),
			URL:      actualURL,
			Snippet:  cleanHTML(snippet),
			Domain:   extractDomain(actualURL),
			Position: position,
		})

		// Find next result
		nextIdx := strings.Index(html[resultStart+20:], "class=\"result__a\"")
		if nextIdx == -1 {
			break
		}
		resultStart = resultStart + 20 + nextIdx
	}

	return results
}

// extractDDGTitleAndURL extracts the title and href from a DuckDuckGo result anchor.
func extractDDGTitleAndURL(block string) (title, href string) {
	// Find <a class="result__a" href="...">
	idx := strings.Index(block, "class=\"result__a\"")
	if idx == -1 {
		return "", ""
	}

	// Find the href before or after the class
	hrefIdx := strings.Index(block[:idx], "href=\"")
	if hrefIdx == -1 {
		hrefIdx = strings.Index(block[idx:], "href=\"")
		if hrefIdx != -1 {
			hrefIdx += idx
		}
	}
	if hrefIdx == -1 {
		return "", ""
	}

	hrefStart := hrefIdx + 6
	hrefEnd := strings.Index(block[hrefStart:], "\"")
	if hrefEnd == -1 {
		return "", ""
	}
	href = block[hrefStart : hrefStart+hrefEnd]

	// Extract title (text between > and </a>)
	titleStart := strings.Index(block[idx:], ">")
	if titleStart == -1 {
		return href, ""
	}
	titleStart += idx + 1
	titleEnd := strings.Index(block[titleStart:], "</a>")
	if titleEnd == -1 {
		return href, block[titleStart:]
	}
	title = block[titleStart : titleStart+titleEnd]

	return title, href
}

// extractDDGSnippet extracts the snippet from a DuckDuckGo result block.
func extractDDGSnippet(block string) string {
	// Look for class="result__snippet"
	idx := strings.Index(block, "class=\"result__snippet\"")
	if idx == -1 {
		// Try alternate class
		idx = strings.Index(block, "class=\"result__excerpt\"")
	}
	if idx == -1 {
		return ""
	}

	// Find the content after >
	contentStart := strings.Index(block[idx:], ">")
	if contentStart == -1 {
		return ""
	}
	contentStart += idx + 1

	// Find the closing </a> or </div>
	contentEnd := strings.Index(block[contentStart:], "</a>")
	if contentEnd == -1 {
		contentEnd = strings.Index(block[contentStart:], "</div>")
	}
	if contentEnd == -1 {
		contentEnd = len(block) - contentStart
	}

	return block[contentStart : contentStart+contentEnd]
}

// cleanDDGRedirectURL extracts the actual URL from a DuckDuckGo redirect URL.
// DuckDuckGo uses redirect URLs like: //duckduckgo.com/l/?uddg=actual_url
func cleanDDGRedirectURL(href string) string {
	// Check for DuckDuckGo redirect pattern
	if strings.Contains(href, "uddg=") {
		if idx := strings.Index(href, "uddg="); idx != -1 {
			encoded := href[idx+5:]
			// URL decode
			if decoded, err := url.QueryUnescape(encoded); err == nil {
				// Remove any trailing parameters
				if ampIdx := strings.Index(decoded, "&"); ampIdx != -1 {
					decoded = decoded[:ampIdx]
				}
				return decoded
			}
			return encoded
		}
	}
	return href
}

// cleanHTML removes basic HTML tags from a string.
func cleanHTML(s string) string {
	// Remove common HTML entities
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&nbsp;", " ")

	// Remove HTML tags (simple approach)
	result := strings.Builder{}
	inTag := false
	for _, c := range s {
		if c == '<' {
			inTag = true
			continue
		}
		if c == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(c)
		}
	}

	// Clean up whitespace
	resultStr := strings.TrimSpace(result.String())
	resultStr = strings.Join(strings.Fields(resultStr), " ")

	return resultStr
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// getHTTPClient returns the HTTP client, creating a default one if needed.
func (t WebSearchTool) getHTTPClient() *http.Client {
	if t.HTTPClient != nil {
		return t.HTTPClient
	}
	return &http.Client{
		Timeout: t.Timeout,
	}
}

// getUserAgent returns the User-Agent string.
func (t WebSearchTool) getUserAgent() string {
	if t.UserAgent != "" {
		return t.UserAgent
	}
	return "Orchestra/1.0 (AI Agent Tool)"
}

// doGet performs a simple GET request.
func (t WebSearchTool) doGet(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", t.getUserAgent())
	return t.getHTTPClient().Do(req)
}

// extractDomain extracts the domain from a URL.
func extractDomain(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return u.Hostname()
}

// getEnv gets an environment variable.
func getEnv(key string) string {
	return os.Getenv(key)
}

// truncate shortens a string to maxLen characters, adding "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// marshalWebSearchError creates a JSON error response for web_search.
func marshalWebSearchError(err error) (json.RawMessage, error) {
	output := WebSearchOutput{
		Error: err.Error(),
	}
	return json.Marshal(output)
}
