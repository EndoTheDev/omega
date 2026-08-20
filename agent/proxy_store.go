package agent

import (
	"context"
	"encoding/json"

	"github.com/EndoTheDev/omega/ai"
)

// StoreDispatcher routes store-seam JSON-RPC calls to the extension
// that declared the "store" seam.
type StoreDispatcher interface {
	StoreRequest(ctx context.Context, method string, params map[string]any) (json.RawMessage, error)
}

// ProxyStore forwards all StoreProvider methods to a store-seam
// extension via JSON-RPC. Messages are encoded as (role, payload)
// pairs — the same wire format used by the gateway.
type ProxyStore struct {
	Dispatcher StoreDispatcher
}

func (p *ProxyStore) Open(dsn string) error {
	_, err := p.Dispatcher.StoreRequest(context.Background(), "store/open", map[string]any{"dsn": dsn})
	return err
}

func (p *ProxyStore) Close() error {
	_, err := p.Dispatcher.StoreRequest(context.Background(), "store/close", nil)
	return err
}

func (p *ProxyStore) CreateSession(ctx context.Context, id, parentID, label string) error {
	_, err := p.Dispatcher.StoreRequest(ctx, "store/create_session", map[string]any{
		"id": id, "parent_id": parentID, "label": label,
	})
	return err
}

func (p *ProxyStore) GetSession(ctx context.Context, id string) (Session, error) {
	raw, err := p.Dispatcher.StoreRequest(ctx, "store/get_session", map[string]any{"id": id})
	if err != nil {
		return Session{}, err
	}
	var s Session
	if err := json.Unmarshal(raw, &s); err != nil {
		return Session{}, err
	}
	return s, nil
}

func (p *ProxyStore) ListSessions(ctx context.Context) ([]Session, error) {
	raw, err := p.Dispatcher.StoreRequest(ctx, "store/list_sessions", nil)
	if err != nil {
		return nil, err
	}
	var out []Session
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *ProxyStore) DeleteSession(ctx context.Context, id string) error {
	_, err := p.Dispatcher.StoreRequest(ctx, "store/delete_session", map[string]any{"id": id})
	return err
}

func (p *ProxyStore) UpdateSession(ctx context.Context, id, label string) error {
	_, err := p.Dispatcher.StoreRequest(ctx, "store/update_session", map[string]any{
		"id": id, "label": label,
	})
	return err
}

func (p *ProxyStore) AppendMessage(ctx context.Context, sessionID string, msg ai.Message) error {
	role, payload, err := ai.EncodeMessage(msg)
	if err != nil {
		return err
	}
	_, err = p.Dispatcher.StoreRequest(ctx, "store/append_message", map[string]any{
		"session_id": sessionID, "role": role, "payload": string(payload),
	})
	return err
}

func (p *ProxyStore) GetMessages(ctx context.Context, sessionID string) ([]ai.Message, error) {
	raw, err := p.Dispatcher.StoreRequest(ctx, "store/get_messages", map[string]any{"session_id": sessionID})
	if err != nil {
		return nil, err
	}
	return decodeMessages(raw)
}

func (p *ProxyStore) GetSessionTree(ctx context.Context) ([]*SessionNode, error) {
	raw, err := p.Dispatcher.StoreRequest(ctx, "store/get_session_tree", nil)
	if err != nil {
		return nil, err
	}
	var out []*SessionNode
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *ProxyStore) GetAncestorMessages(ctx context.Context, sessionID string) ([]ai.Message, error) {
	raw, err := p.Dispatcher.StoreRequest(ctx, "store/get_ancestor_messages", map[string]any{"session_id": sessionID})
	if err != nil {
		return nil, err
	}
	return decodeMessages(raw)
}

// decodeMessages unmarshals a JSON array of (role, payload) pairs
// into ai.Message values.
func decodeMessages(raw json.RawMessage) ([]ai.Message, error) {
	var pairs []struct {
		Role    string `json:"role"`
		Payload string `json:"payload"`
	}
	if err := json.Unmarshal(raw, &pairs); err != nil {
		return nil, err
	}
	out := make([]ai.Message, len(pairs))
	for i, p := range pairs {
		msg, err := ai.DecodeMessage(p.Role, []byte(p.Payload))
		if err != nil {
			return nil, err
		}
		out[i] = msg
	}
	return out, nil
}

func (p *ProxyStore) SearchMessages(ctx context.Context, query string) ([]SearchResult, error) {
	raw, err := p.Dispatcher.StoreRequest(ctx, "store/search_messages", map[string]any{"query": query})
	if err != nil {
		return nil, err
	}
	var out []SearchResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *ProxyStore) ComputeInsights(ctx context.Context, days int) (*Insights, error) {
	raw, err := p.Dispatcher.StoreRequest(ctx, "store/compute_insights", map[string]any{"days": days})
	if err != nil {
		return nil, err
	}
	var out Insights
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (p *ProxyStore) CountMessages(ctx context.Context, sessionID string) (int, error) {
	raw, err := p.Dispatcher.StoreRequest(ctx, "store/count_messages", map[string]any{"session_id": sessionID})
	if err != nil {
		return 0, err
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, err
	}
	return n, nil
}
