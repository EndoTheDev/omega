package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
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
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func main() {
	eventsFile := os.Getenv("OMEGA_TEST_EVENTS")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}
		var resp rpcResponse
		if req.ID != nil {
			resp.ID = *req.ID
		}
		resp.JSONRPC = "2.0"
		switch req.Method {
		case "initialize":
			resp.Result = []byte(`{"name":"mock-ext","tools":[{"name":"echo_tool","description":"Echoes back the argument","parameters":{"type":"object","properties":{"text":{"type":"string","description":"Text to echo"}},"required":["text"]}}],"commands":[{"name":"ext-test","description":"Test command"}],"subscriptions":["turn_start","turn_end","agent_start","agent_end"]}`)
		case "tool_call":
			var params struct {
				Tool string            `json:"tool"`
				Args map[string]string `json:"args"`
			}
			_ = json.Unmarshal(req.Params, &params)
			content := "echo: " + params.Args["text"]
			resp.Result = []byte(fmt.Sprintf(`{"content":"%s","is_error":false}`, content))
		case "command":
			resp.Result = []byte(`{"output":"command executed"}`)
		case "event":
			if eventsFile != "" {
				appendEvent(eventsFile, line)
			}
		case "shutdown":
			os.Exit(0)
		}
		if req.ID != nil {
			out, _ := json.Marshal(resp)
			fmt.Println(string(out))
		}
	}
}

func appendEvent(path, line string) {
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f == nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, line)
}
