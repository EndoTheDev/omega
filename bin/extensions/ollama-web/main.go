package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// web is an omega extension that provides web search and fetch tools
// using the Ollama Cloud API. It reads OLLAMA_API_KEY from the
// environment (passed by the host from config.yaml's provider.api_key).
//
// Build:
//   go build -o ollama-web/ollama-web.exe ollama-web/main.go
//
// Enable in config.yaml:
//   extensions:
//     enabled: true
//     dir: extensions

const (
	searchURL = "https://ollama.com/api/web_search"
	fetchURL  = "https://ollama.com/api/web_fetch"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type initResult struct {
	Name          string       `json:"name"`
	Tools         []toolDef    `json:"tools"`
	Subscriptions []string     `json:"subscriptions"`
}

type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}



type toolCallResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}


type searchRequest struct {
	Query     string `json:"query"`
	MaxResults int   `json:"max_results,omitempty"`
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
	Title  string   `json:"title"`
	Content string  `json:"content"`
	Links  []string `json:"links"`
}

func main() {
	apiKey := os.Getenv("OLLAMA_API_KEY")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}

		resp := rpcResponse{JSONRPC: "2.0"}
		if req.ID != nil {
			resp.ID = *req.ID
		}

		switch req.Method {
		case "initialize":
			resp.Result = mustMarshal(initResult{
				Name: "web",
				Tools: []toolDef{
					{
						Name:        "web.search",
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
					},
					{
						Name:        "web.fetch",
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
					},
				},
				Subscriptions: []string{},
			})

		case "tool_call":
			var params struct {
				Tool string                 `json:"tool"`
				Args map[string]any          `json:"args"`
			}
			_ = json.Unmarshal(req.Params, &params)

			content, isErr := handleToolCall(params.Tool, params.Args, apiKey)
			resp.Result = mustMarshal(toolCallResult{Content: content, IsError: isErr})

		case "event", "shutdown":
			continue
		}

		if req.ID != nil {
			fmt.Println(string(mustMarshal(resp)))
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "web: scanner error: %v\n", err)
	}
}

func handleToolCall(tool string, args map[string]any, apiKey string) (string, bool) {
	switch tool {
	case "web.search":
		query, _ := args["query"].(string)
		if query == "" {
			return "error: query is required", true
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
		return doSearch(query, maxResults, apiKey)

	case "web.fetch":
		url, _ := args["url"].(string)
		if url == "" {
			return "error: url is required", true
		}
		return doFetch(url, apiKey)

	default:
		return fmt.Sprintf("error: unknown tool %q", tool), true
	}
}

func doSearch(query string, maxResults int, apiKey string) (string, bool) {
	if apiKey == "" {
		return "error: OLLAMA_API_KEY not set", true
	}
	body, _ := json.Marshal(searchRequest{
		Query:      query,
		MaxResults: maxResults,
	})
	req, err := http.NewRequest("POST", searchURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Sprintf("error: building request: %v", err), true
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpDo(req)
	if err != nil {
		return fmt.Sprintf("error: search request failed: %v", err), true
	}

	var sr searchResponse
	if err := json.Unmarshal(resp, &sr); err != nil {
		return fmt.Sprintf("error: parsing search response: %v", err), true
	}

	var sb strings.Builder
	for i, r := range sr.Results {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		fmt.Fprintf(&sb, "## %s\n%s\n\nURL: %s", r.Title, r.Content, r.URL)
	}
	if sb.Len() == 0 {
		return "No results found.", false
	}
	return sb.String(), false
}

func doFetch(url string, apiKey string) (string, bool) {
	if apiKey == "" {
		return "error: OLLAMA_API_KEY not set", true
	}
	body, _ := json.Marshal(fetchRequest{URL: url})
	req, err := http.NewRequest("POST", fetchURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Sprintf("error: building request: %v", err), true
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpDo(req)
	if err != nil {
		return fmt.Sprintf("error: fetch request failed: %v", err), true
	}

	var fr fetchResponse
	if err := json.Unmarshal(resp, &fr); err != nil {
		return fmt.Sprintf("error: parsing fetch response: %v", err), true
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
	return sb.String(), false
}

// httpDo sends a request with a 30s timeout and returns the body.
func httpDo(req *http.Request) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{}`)
	}
	return b
}
