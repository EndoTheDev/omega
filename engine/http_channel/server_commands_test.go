package http_channel

// /commands palette surface tests (card t_07aed3f9): the registry read
// is the rewire's snapshot (ctx.Commands), dispatch runs the same
// chain-on-miss handler the TUI uses, and an unregistered "/skill"
// name resolves into the skill kind. Direct seam injection for the
// registry - the rewire machinery (mount → ctx → rewireSeams) is
// extensionapi's job and already covered there.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// listingSkills is a SkillsProvider + SkillsLister double for the
// skill dispatch path (content IS loadable here - the double keeps it
// in memory rather than reading a directory).
type listingSkills struct{}

func (listingSkills) LoadSkills(dir string) ([]core.Skill, error) { return nil, errors.New("not used") }
func (listingSkills) ListSkills() ([]core.Skill, error) {
	return []core.Skill{
		{Name: "greet-writer", Content: "skill body for greet-writer"},
		{Name: "other", Content: "skill body other"},
	}, nil
}

func TestHandleCommandsListsRegistrySnapshot(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil)
	s.seams.mu.Lock()
	s.seams.commands = []core.ExtensionCommand{
		{Name: "/ping", Description: "answers pong"},
		{Name: "/greet", Description: "greets", Args: []string{"formal", "casual"}},
	}
	s.seams.mu.Unlock()

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/commands", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /commands: %d %s", rec.Code, rec.Body)
	}
	var got []commandListing
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("rows = %d, want 2: %s", len(got), rec.Body)
	}
	if got[0].Name != "/ping" || got[1].Name != "/greet" {
		t.Fatalf("rows = %+v, want /ping then /greet", got)
	}
	if len(got[1].Args) != 2 || got[1].Args[0] != "formal" {
		t.Fatalf("args = %+v, want the declared completions", got[1].Args)
	}
	// empty registry still lists as an array
	s2 := NewServer(nil, nil, nil, nil, nil)
	rec2 := httptest.NewRecorder()
	s2.Handler().ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/commands", nil))
	if rec2.Code != http.StatusOK || strings.TrimSpace(rec2.Body.String()) != "[]" {
		t.Fatalf("empty registry: %d %s, want []", rec2.Code, rec2.Body)
	}
}

func TestHandleCommandDispatchRegisteredCommand(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil)
	s.seams.mu.Lock()
	s.seams.commands = []core.ExtensionCommand{{Name: "/ping", Description: "answers pong"}}
	s.seams.commandHandler = func(ctx context.Context, name, args string) (core.CommandResult, error) {
		if name != "/ping" {
			t.Fatalf("handler got %q", name)
		}
		return core.CommandResult{Text: "pong " + args, Actions: []core.CmdAction{{Type: "set_model", Value: "m1"}}}, nil
	}
	s.seams.mu.Unlock()

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/commands/dispatch",
		strings.NewReader(`{"name":"/ping","args":"world"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("dispatch: %d %s", rec.Code, rec.Body)
	}
	var got commandDispatchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Kind != "command" || got.Text != "pong world" {
		t.Fatalf("response = %+v, want kind command + pong text", got)
	}
	if len(got.Actions) != 1 || got.Actions[0].Type != "set_model" {
		t.Fatalf("actions = %+v, want the set_model passthrough", got.Actions)
	}
}

func TestHandleCommandDispatchErrors(t *testing.T) {
	cases := []struct {
		name string
		cmds []core.ExtensionCommand
		body string
		want int
	}{
		{"unknown command", nil, `{"name":"/nope"}`, http.StatusNotFound},
		{"bare name rejected", nil, `{"name":"ping"}`, http.StatusBadRequest},
		{"empty name", nil, `{"name":""}`, http.StatusBadRequest},
	}
	s := NewServer(nil, nil, nil, nil, nil)
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/commands/dispatch",
				strings.NewReader(tt.body)))
			if rec.Code != tt.want {
				t.Fatalf("status = %d %s, want %d", rec.Code, rec.Body, tt.want)
			}
		})
	}
	// handler failure surfaces as 500 with the error text
	s2 := NewServer(nil, nil, nil, nil, nil)
	s2.seams.mu.Lock()
	s2.seams.commands = []core.ExtensionCommand{{Name: "/boom"}}
	s2.seams.commandHandler = func(ctx context.Context, name, args string) (core.CommandResult, error) {
		return core.CommandResult{}, errors.New("module exploded")
	}
	s2.seams.mu.Unlock()
	rec := httptest.NewRecorder()
	s2.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/commands/dispatch",
		strings.NewReader(`{"name":"/boom"}`)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("handler failure: %d %s, want 500", rec.Code, rec.Body)
	}
}

func TestHandleCommandDispatchSkillResolves(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil)
	s.seams.mu.Lock()
	s.seams.skills = listingSkills{}
	s.seams.mu.Unlock()

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/commands/dispatch",
		strings.NewReader(`{"name":"/greet-writer"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("skill dispatch: %d %s", rec.Code, rec.Body)
	}
	var got commandDispatchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Kind != "skill" || got.Name != "greet-writer" || got.Content != "skill body for greet-writer" {
		t.Fatalf("response = %+v, want the skill kind with content", got)
	}

	// an unknown /name that is not a skill is a 404, never silent
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/commands/dispatch",
		strings.NewReader(`{"name":"/ghost-skill"}`)))
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("unknown skill: %d %s, want 404", rec2.Code, rec2.Body)
	}
}