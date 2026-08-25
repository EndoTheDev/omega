// Package store provides the session persistence seam (agent.StoreProvider)
// by wrapping gateway.Store (SQLite), plus the sessions.search tool that
// lets the agent search past conversations via FTS5.
//
// Seam: store (exclusive), tools (additive).
package store

import (
	"context"
	"fmt"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/gateway"
)

// NewStore opens the SQLite database at dsn and returns it as a
// StoreProvider. gateway.Store already implements agent.StoreProvider
// fully; this is a thin constructor so callers don't import gateway
// directly.
func NewStore(dsn string) (agent.StoreProvider, error) {
	return gateway.Open(dsn)
}

// searchToolProvider provides the sessions.search tool.
type searchToolProvider struct {
	store agent.StoreProvider
}

// Tools returns the sessions.search tool definition.
func (p *searchToolProvider) Tools() map[string]agent.Tool {
	return map[string]agent.Tool{
		"sessions.search": {
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
			Run: p.runSearch,
		},
	}
}

// runSearch executes the sessions.search tool: queries the FTS5 index
// and formats results as "[label] snippet" lines, using the session
// label when available (falling back to the session ID).
func (p *searchToolProvider) runSearch(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	results, err := p.store.SearchMessages(ctx, query)
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "no results", nil
	}
	var out string
	for _, r := range results {
		label := r.SessionID
		if sess, err := p.store.GetSession(ctx, r.SessionID); err == nil && sess.Label != "" {
			label = sess.Label
		}
		out += fmt.Sprintf("[%s] %s\n", label, r.Snippet)
	}
	return out, nil
}