// core-store is an omega extension that implements the store seam.
// It wraps gateway.Store (SQLite) and exposes all session/message
// operations via JSON-RPC. It also provides the sessions.search tool
// so the agent can search past conversations.
//
// Seam: store
// Methods: store/open, store/close, store/create_session,
// store/get_session, store/list_sessions, store/delete_session,
// store/update_session, store/append_message, store/get_messages,
// store/get_session_tree, store/get_ancestor_messages,
// store/search_messages, store/compute_insights, store/count_messages
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/gateway"
)

// --- omega extension protocol types ---

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type extTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

var toolDefs = []extTool{
	{
		Name:        "sessions.search",
		Description: "Search past session messages by keyword. Returns matching sessions with snippets of the matching content.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Search query (FTS5 syntax: words, phrases, OR, NOT)",
				},
			},
			"required": []string{"query"},
		},
	},
}

var store *gateway.Store

func main() {
	stdin := bufio.NewReader(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	for {
		line, err := stdin.ReadString('\n')
		if err != nil {
			return
		}
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}

		switch req.Method {
		case "initialize":
			result, _ := json.Marshal(map[string]any{
				"name":          "core-store",
				"seams":         []string{"store"},
				"tools":         toolDefs,
				"subscriptions": []string{},
			})
			if req.ID != nil {
				encoder.Encode(rpcResponse{JSONRPC: "2.0", ID: *req.ID, Result: result})
			}

		case "tool_call":
			var params struct {
				Name string         `json:"name"`
				Args map[string]any `json:"args"`
			}
			json.Unmarshal(req.Params, &params)

			if params.Name == "sessions.search" {
				query, _ := params.Args["query"].(string)
				results, err := store.SearchMessages(context.Background(), query)
				if err != nil {
					encoder.Encode(rpcResponse{
						JSONRPC: "2.0", ID: *req.ID,
						Error: &rpcError{Code: -32603, Message: err.Error()},
					})
					continue
				}
				if len(results) == 0 {
					encoder.Encode(rpcResponse{JSONRPC: "2.0", ID: *req.ID, Result: json.RawMessage(`"no results"`)})
					continue
				}
				var out string
				for _, r := range results {
					label := r.SessionID
					if sess, err := store.GetSession(context.Background(), r.SessionID); err == nil && sess.Label != "" {
						label = sess.Label
					}
					out += fmt.Sprintf("[%s] %s\n", label, r.Snippet)
				}
				result, _ := json.Marshal(out)
				encoder.Encode(rpcResponse{JSONRPC: "2.0", ID: *req.ID, Result: result})
			} else {
				encoder.Encode(rpcResponse{
					JSONRPC: "2.0", ID: *req.ID,
					Error: &rpcError{Code: -32601, Message: "unknown tool: " + params.Name},
				})
			}

		case "store/open":
			var params struct {
				DSN string `json:"dsn"`
			}
			json.Unmarshal(req.Params, &params)
			s, err := gateway.Open(params.DSN)
			if err != nil {
				encoder.Encode(rpcResponse{
					JSONRPC: "2.0", ID: *req.ID,
					Error: &rpcError{Code: -32603, Message: err.Error()},
				})
				continue
			}
			store = s
			encoder.Encode(rpcResponse{JSONRPC: "2.0", ID: *req.ID, Result: json.RawMessage(`null`)})

		case "store/close":
			if store != nil {
				store.Close()
			}
			encoder.Encode(rpcResponse{JSONRPC: "2.0", ID: *req.ID, Result: json.RawMessage(`null`)})

		case "store/create_session":
			var p struct {
				ID       string `json:"id"`
				ParentID string `json:"parent_id"`
				Label    string `json:"label"`
			}
			json.Unmarshal(req.Params, &p)
			err := store.CreateSession(context.Background(), p.ID, p.ParentID, p.Label)
			encodeStoreResult(encoder, req.ID, nil, err)

		case "store/get_session":
			var p struct{ ID string `json:"id"` }
			json.Unmarshal(req.Params, &p)
			sess, err := store.GetSession(context.Background(), p.ID)
			if err != nil {
				encodeStoreResult(encoder, req.ID, nil, err)
				continue
			}
			encodeStoreResult(encoder, req.ID, sess, nil)

		case "store/list_sessions":
			sessions, err := store.ListSessions(context.Background())
			if err != nil {
				encodeStoreResult(encoder, req.ID, nil, err)
				continue
			}
			encodeStoreResult(encoder, req.ID, sessions, nil)

		case "store/delete_session":
			var p struct{ ID string `json:"id"` }
			json.Unmarshal(req.Params, &p)
			err := store.DeleteSession(context.Background(), p.ID)
			encodeStoreResult(encoder, req.ID, nil, err)

		case "store/update_session":
			var p struct {
				ID    string `json:"id"`
				Label string `json:"label"`
			}
			json.Unmarshal(req.Params, &p)
			err := store.UpdateSession(context.Background(), p.ID, p.Label)
			encodeStoreResult(encoder, req.ID, nil, err)

		case "store/append_message":
			var p struct {
				SessionID string `json:"session_id"`
				Role      string `json:"role"`
				Payload   string `json:"payload"`
			}
			json.Unmarshal(req.Params, &p)
			msg, err := ai.DecodeMessage(p.Role, []byte(p.Payload))
			if err != nil {
				encodeStoreResult(encoder, req.ID, nil, err)
				continue
			}
			err = store.AppendMessage(context.Background(), p.SessionID, msg)
			encodeStoreResult(encoder, req.ID, nil, err)

		case "store/get_messages":
			var p struct{ SessionID string `json:"session_id"` }
			json.Unmarshal(req.Params, &p)
			msgs, err := store.GetMessages(context.Background(), p.SessionID)
			if err != nil {
				encodeStoreResult(encoder, req.ID, nil, err)
				continue
			}
			pairs := make([]struct {
				Role    string `json:"role"`
				Payload string `json:"payload"`
			}, len(msgs))
			for i, m := range msgs {
				role, payload, _ := ai.EncodeMessage(m)
				pairs[i].Role = role
				pairs[i].Payload = string(payload)
			}
			encodeStoreResult(encoder, req.ID, pairs, nil)

		case "store/get_session_tree":
			tree, err := store.GetSessionTree(context.Background())
			encodeStoreResult(encoder, req.ID, tree, err)

		case "store/get_ancestor_messages":
			var p struct{ SessionID string `json:"session_id"` }
			json.Unmarshal(req.Params, &p)
			msgs, err := store.GetAncestorMessages(context.Background(), p.SessionID)
			if err != nil {
				encodeStoreResult(encoder, req.ID, nil, err)
				continue
			}
			pairs := make([]struct {
				Role    string `json:"role"`
				Payload string `json:"payload"`
			}, len(msgs))
			for i, m := range msgs {
				role, payload, _ := ai.EncodeMessage(m)
				pairs[i].Role = role
				pairs[i].Payload = string(payload)
			}
			encodeStoreResult(encoder, req.ID, pairs, nil)

		case "store/search_messages":
			var p struct{ Query string `json:"query"` }
			json.Unmarshal(req.Params, &p)
			results, err := store.SearchMessages(context.Background(), p.Query)
			encodeStoreResult(encoder, req.ID, results, err)

		case "store/compute_insights":
			var p struct{ Days int `json:"days"` }
			json.Unmarshal(req.Params, &p)
			insights, err := store.ComputeInsights(context.Background(), p.Days)
			if err != nil {
				encodeStoreResult(encoder, req.ID, nil, err)
				continue
			}
			encodeStoreResult(encoder, req.ID, insights, nil)

		case "store/count_messages":
			var p struct{ SessionID string `json:"session_id"` }
			json.Unmarshal(req.Params, &p)
			n, err := store.CountMessages(context.Background(), p.SessionID)
			if err != nil {
				encodeStoreResult(encoder, req.ID, nil, err)
				continue
			}
			encodeStoreResult(encoder, req.ID, n, nil)

		case "shutdown":
			if store != nil {
				store.Close()
			}
			return

		default:
			if req.ID != nil {
				encoder.Encode(rpcResponse{
					JSONRPC: "2.0", ID: *req.ID,
					Error: &rpcError{Code: -32601, Message: "unknown method: " + req.Method},
				})
			}
		}
	}
}

func encodeStoreResult(encoder *json.Encoder, id *int, result any, err error) {
	if id == nil {
		return
	}
	if err != nil {
		encoder.Encode(rpcResponse{
			JSONRPC: "2.0", ID: *id,
			Error:   &rpcError{Code: -32603, Message: err.Error()},
		})
		return
	}
	data, _ := json.Marshal(result)
	encoder.Encode(rpcResponse{JSONRPC: "2.0", ID: *id, Result: data})
}



