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