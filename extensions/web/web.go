package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/gateway"
)

const (
	searchURL = "https://ollama.com/api/web_search"
	fetchURL  = "https://ollama.com/api/web_fetch"
)

// Extension provides web search and fetch tools via the Ollama
// Cloud API. It implements agent.ToolProvider directly.
type Extension struct {
	apiKey string
}

// New creates a web Extension from a gateway config. The API key
// is read from Provider.APIKey.
func New(cfg *gateway.Config) *Extension {
	var key string
	if cfg != nil {
		key = cfg.Provider.APIKey
	}
	return &Extension{apiKey: key}
}

// Tools returns the web.search and web.fetch tools.
func (e *Extension) Tools() map[string]agent.Tool {
	return map[string]agent.Tool{
		"web.search": {
			Description: "Search the web for the given query and return relevant results with titles, URLs, and content snippets.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "The search query string.",
					},
					"max_results": map[string]any{
						"type":        "integer",
						"description": "Maximum results to return (default 5, max 10).",
					},
				},
				"required": []string{"query"},
			},
			Run: e.runSearch,
		},
		"web.fetch": {
			Description: "Fetch a single web page by URL and return its title, main content, and links found on the page.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "The URL to fetch.",
					},
				},
				"required": []string{"url"},
			},
			Run: e.runFetch,
		},
	}
}

// runSearch handles the web.search tool call.
func (e *Extension) runSearch(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "error: query is required", nil
	}
	maxResults := 5
	if mr, ok := args["max_results"]; ok {
		switch v := mr.(type) {
		case float64:
			maxResults = int(v)
		case int:
			maxResults = v
		}
	}
	return e.doSearch(ctx, query, maxResults)
}

// runFetch handles the web.fetch tool call.
func (e *Extension) runFetch(ctx context.Context, args map[string]any) (string, error) {
	url, _ := args["url"].(string)
	if url == "" {
		return "error: url is required", nil
	}
	return e.doFetch(ctx, url)
}

type searchRequest struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results,omitempty"`
}

type searchResponse struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"results"`
}

type fetchRequest struct {
	URL string `json:"url"`
}

type fetchResponse struct {
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Links   []string `json:"links"`
}

// doSearch calls the Ollama Cloud search API and formats results.
func (e *Extension) doSearch(ctx context.Context, query string, maxResults int) (string, error) {
	if e.apiKey == "" {
		return "error: API key not set", nil
	}
	body, _ := json.Marshal(searchRequest{Query: query, MaxResults: maxResults})
	req, err := http.NewRequestWithContext(ctx, "POST", searchURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Sprintf("error: building request: %v", err), nil
	}
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := ai.RetryHTTP(req)
	if err != nil {
		return fmt.Sprintf("error: search request failed: %v", err), nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("error: reading search response: %v", err), nil
	}

	var sr searchResponse
	if err := json.Unmarshal(respBody, &sr); err != nil {
		return fmt.Sprintf("error: parsing search response: %v", err), nil
	}

	var sb strings.Builder
	for i, r := range sr.Results {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		fmt.Fprintf(&sb, "## %s\n%s\n\nURL: %s", r.Title, r.Content, r.URL)
	}
	if sb.Len() == 0 {
		return "No results found.", nil
	}
	return sb.String(), nil
}

// doFetch calls the Ollama Cloud fetch API and formats the page.
func (e *Extension) doFetch(ctx context.Context, url string) (string, error) {
	if e.apiKey == "" {
		return "error: API key not set", nil
	}
	body, _ := json.Marshal(fetchRequest{URL: url})
	req, err := http.NewRequestWithContext(ctx, "POST", fetchURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Sprintf("error: building request: %v", err), nil
	}
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := ai.RetryHTTP(req)
	if err != nil {
		return fmt.Sprintf("error: fetch request failed: %v", err), nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("error: reading fetch response: %v", err), nil
	}

	var fr fetchResponse
	if err := json.Unmarshal(respBody, &fr); err != nil {
		return fmt.Sprintf("error: parsing fetch response: %v", err), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n%s", fr.Title, fr.Content)
	if len(fr.Links) > 0 {
		sb.WriteString("\n\n## Links\n")
		for _, link := range fr.Links {
			sb.WriteString("- ")
			sb.WriteString(link)
			sb.WriteString("\n")
		}
	}
	return sb.String(), nil
}