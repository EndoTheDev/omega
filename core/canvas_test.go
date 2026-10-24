package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

// The canvas wire contract is the v1 schema's law: transport work
// (SSE/REST translation, arc 3b) reads these exact keys, so the JSON
// names are pinned like the rest of the wire vocabulary. No backwards
// compatibility - if a key changes, this test changes with it.

func TestPanelJSONShape(t *testing.T) {
	p := Panel{
		ID:      "board",
		Title:   "Kanban",
		Kind:    "kanban.board",
		Payload: map[string]any{"columns": 3},
		Actions: []PanelAction{{ID: "refresh", Label: "Refresh", Module: "kanban"}},
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"id", "title", "kind", "payload", "actions"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("Panel JSON missing key %q: %s", key, b)
		}
	}
	actions, ok := raw["actions"].([]any)
	if !ok || len(actions) != 1 {
		t.Fatalf("actions missing from %s", b)
	}
	first, _ := actions[0].(map[string]any)
	for _, key := range []string{"id", "label", "module"} {
		if _, ok := first[key]; !ok {
			t.Errorf("PanelAction JSON missing key %q", key)
		}
	}
}

func TestActionCallJSONShape(t *testing.T) {
	call := ActionCall{
		Module: "kanban",
		Action: "refresh",
		Args:   json.RawMessage(`{"lane":"todo"}`),
	}
	b, _ := json.Marshal(call)
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"module", "action", "args"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("ActionCall JSON missing key %q: %s", key, b)
		}
	}
	var back ActionCall
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("roundtrip: %v", err)
	}
	if back.Module != call.Module || back.Action != call.Action ||
		string(back.Args) != string(call.Args) {
		t.Errorf("roundtrip mismatch: %+v", back)
	}
}

func TestViewModelDeltaRidesEventSpine(t *testing.T) {
	// The sealed-event law: the delta is an Event on the same spine,
	// dispatched by type switch like every other kind.
	var _ Event = ViewModelDelta{Type: "view_model_delta", Panel: Panel{ID: "board"}}
}

func TestViewModelDeltaJSONShape(t *testing.T) {
	d := ViewModelDelta{
		Type:  "view_model_delta",
		Panel: Panel{ID: "board", Title: "Kanban", Kind: "kanban.board"},
	}
	b, _ := json.Marshal(d)
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"type", "panel"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("ViewModelDelta JSON missing key %q: %s", key, b)
		}
	}
	if raw["type"] != "view_model_delta" {
		t.Errorf("type = %v, want view_model_delta", raw["type"])
	}
}

func TestCanvas404Sentinels(t *testing.T) {
	err := ErrUnknownModule
	if !errors.Is(fmt.Errorf("%w: %q", ErrUnknownModule, "kanban"), err) {
		t.Error("wrapped module error must satisfy errors.Is(ErrUnknownModule)")
	}
	if !errors.Is(fmt.Errorf("%w: %q", ErrUnknownAction, "refresh"), ErrUnknownAction) {
		t.Error("wrapped action error must satisfy errors.Is(ErrUnknownAction)")
	}
}

// The chat panel vocabulary's wire law (chat-last 1/3): both frontends
// render the chat panel from these exact keys - payload session/running/
// rows, row id/role/status/blocks, block type/content. No backwards
// compatibility - if a key changes, this test changes with it.
func TestChatPanelJSONShape(t *testing.T) {
	p := Panel{
		ID:   "chat",
		Kind: PanelKindChat,
		Payload: ChatPayload{
			Session: "sess-x",
			Running: true,
			Rows: []ChatRow{{
				ID:     "u1",
				Role:   ChatRoleUser,
				Status: ChatStatusStreaming,
				Blocks: []ChatBlock{{Type: ChatBlockText, Content: "hi", Running: true}},
			}},
		},
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	payload, ok := raw["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing from %s", b)
	}
	for _, key := range []string{"session", "running", "rows"} {
		if _, present := payload[key]; !present {
			t.Errorf("ChatPayload JSON missing key %q: %s", key, b)
		}
	}
	rows, _ := payload["rows"].([]any)
	if len(rows) != 1 {
		t.Fatalf("rows missing from %s", b)
	}
	row, _ := rows[0].(map[string]any)
	for _, key := range []string{"id", "role", "status", "blocks"} {
		if _, present := row[key]; !present {
			t.Errorf("ChatRow JSON missing key %q: %s", key, b)
		}
	}
	blocks, _ := row["blocks"].([]any)
	block, _ := blocks[0].(map[string]any)
	for _, key := range []string{"type", "content", "running"} {
		if _, present := block[key]; !present {
			t.Errorf("ChatBlock JSON missing key %q: %s", key, b)
		}
	}
}
