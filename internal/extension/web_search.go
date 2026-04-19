package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/nulzo/model-router-api/pkg/api"
)

// WebSearchExtension provides web search capabilities to the LLM.
type WebSearchExtension struct {
	SearxngURL string // e.g., "http://localhost:8080"
	Client     *http.Client
}

func NewWebSearchExtension(searxngURL string) *WebSearchExtension {
	if searxngURL == "" {
		// Default to a local instance, or you could configure a public one
		searxngURL = "http://localhost:8080"
	}
	return &WebSearchExtension{
		SearxngURL: searxngURL,
		Client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (t *WebSearchExtension) Name() string {
	return "prism:web_search"
}

func (t *WebSearchExtension) BuildTool(config api.ExtensionConfig) (api.Tool, error) {
	return api.Tool{
		Type: "function",
		Function: api.FunctionDescription{
			Name:        t.Name(),
			Description: "Search the web for current information, news, and facts.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "The search query.",
					},
					"max_results": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results to return. Defaults to 5.",
					},
				},
				"required": []string{"query"},
			},
		},
	}, nil
}

type webSearchArgs struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
}

type searxngResponse struct {
	Query   string `json:"query"`
	Results []struct {
		URL     string `json:"url"`
		Title   string `json:"title"`
		Content string `json:"content"`
	} `json:"results"`
}

type webSearchResult struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

func (t *WebSearchExtension) Execute(ctx context.Context, config api.ExtensionConfig, args []byte) (string, error) {
	var parsedArgs webSearchArgs
	if err := json.Unmarshal(args, &parsedArgs); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if parsedArgs.Query == "" {
		return "", fmt.Errorf("query is required")
	}

	if parsedArgs.MaxResults <= 0 {
		if config.Config != nil {
			switch v := config.Config["max_results"].(type) {
			case float64:
				parsedArgs.MaxResults = int(v)
			case int:
				parsedArgs.MaxResults = v
			}
		}
		if parsedArgs.MaxResults <= 0 {
			parsedArgs.MaxResults = 5
		}
	} else if parsedArgs.MaxResults > 10 {
		parsedArgs.MaxResults = 10
	}

	baseURL := t.SearxngURL
	if config.Config != nil {
		if override, ok := config.Config["searxng_url"].(string); ok && override != "" {
			baseURL = override
		}
	}

	// Build SearXNG request
	searchURL := fmt.Sprintf("%s/search?q=%s&format=json", baseURL, url.QueryEscape(parsedArgs.Query))
	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := t.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("search failed with status %d: %s", resp.StatusCode, string(body))
	}

	var sxResp searxngResponse
	if err := json.NewDecoder(resp.Body).Decode(&sxResp); err != nil {
		return "", fmt.Errorf("failed to decode search response: %w", err)
	}

	// Extract and format results
	var results []webSearchResult
	for i, r := range sxResp.Results {
		if i >= parsedArgs.MaxResults {
			break
		}
		results = append(results, webSearchResult{
			URL:     r.URL,
			Title:   r.Title,
			Snippet: r.Content,
		})
	}

	// If no results, return a helpful message
	if len(results) == 0 {
		return `{"message": "No results found for the given query."}`, nil
	}

	resBytes, err := json.Marshal(results)
	if err != nil {
		return "", fmt.Errorf("failed to marshal results: %w", err)
	}

	return string(resBytes), nil
}
