package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// example is a minimal omega extension that speaks JSON-RPC over
// stdin/stdout. It registers one tool (example.greet) and one slash
// command (/greet) and subscribes to lifecycle events.
//
// Build:
//   go build -o extensions/example extensions/example/main.go
//
// Enable in config.yaml:
//   extensions:
//     enabled: true
//     dir: extensions

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
	Name          string        `json:"name"`
	Tools         []toolDef     `json:"tools"`
	Commands      []commandDef  `json:"commands"`
	Subscriptions []string      `json:"subscriptions"`
}

type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type commandDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type toolCallParams struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

type toolCallResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

type commandParams struct {
	Name string `json:"name"`
	Args string `json:"args"`
}

type commandResult struct {
	Output string `json:"output"`
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
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
				Name: "example",
				Tools: []toolDef{{
					Name:        "example.greet",
					Description: "Return a greeting for the given name.",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"name": map[string]any{"type": "string", "description": "Name to greet"},
						},
						"required": []string{"name"},
					},
				}},
				Commands: []commandDef{{
					Name:        "greet",
					Description: "Show a greeting from the example extension",
				}},
				Subscriptions: []string{"agent_start", "agent_end", "turn_start", "turn_end", "tool_result"},
			})

		case "tool_call":
			var params toolCallParams
			_ = json.Unmarshal(req.Params, &params)
			content := ""
			if name, ok := params.Args["name"].(string); ok {
				content = fmt.Sprintf("Hello, %s! (from example extension)", name)
			} else {
				content = "Missing 'name' argument"
			}
			resp.Result = mustMarshal(toolCallResult{Content: content, IsError: false})

		case "command":
			var params commandParams
			_ = json.Unmarshal(req.Params, &params)
			output := fmt.Sprintf("Hello from /greet! Args: %q", params.Args)
			resp.Result = mustMarshal(commandResult{Output: output})

		case "event", "shutdown":
			// Notifications need no response.
			continue
		}

		if req.ID != nil {
			fmt.Println(string(mustMarshal(resp)))
		}
	}
}

func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{}`)
	}
	return b
}
