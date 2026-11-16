package api

// /commands palette surface tests (card t_07aed3f9): the registry read
// is the rewire's snapshot (ctx.Commands), dispatch runs the same
// chain-on-miss handler the loop uses, and an unregistered "/skill"
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

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/cron"
	"github.com/EndoTheDev/omega/harness/notifications"
	"github.com/EndoTheDev/omega/harness/observability"
	"github.com/EndoTheDev/omega/harness/store"
	"github.com/EndoTheDev/omega/harness/verdict"
)

// listingSkills serves the serving surface's own face (part 3): the
// std-typed inventory view for the /<skill-name> dispatch path.
type listingSkills struct{}

// errNotFound: the skill provider's plain not-found phrasing (the
// skills capsule moved home with the shrink wave 3; the phrase law
// pins here for the fake reader).
type errNotFound struct{}

func (*errNotFound) Error() string { return `skill %q not found. Available: none` }

func (listingSkills) SkillRead(name string) (description, content string, err error) {
	index, contents, _ := listingSkills{}.SkillsEntries()
	if _, ok := index[name]; !ok {
		return "", "", &errNotFound{}
	}
	return index[name], contents[name], nil
}

func (listingSkills) SkillsEntries() (index map[string]string, contents map[string]string, err error) {
	return map[string]string{
		"greet-writer": "greets",
		"other":        "other",
	}, map[string]string{
		"greet-writer": "skill body for greet-writer",
		"other":        "skill body other",
	}, nil
}

func TestHandleCommandsListsRegistrySnapshot(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil, nil)
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
	s2 := NewServer(nil, nil, nil, nil, nil, nil)
	rec2 := httptest.NewRecorder()
	s2.Handler().ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/commands", nil))
	if rec2.Code != http.StatusOK || strings.TrimSpace(rec2.Body.String()) != "[]" {
		t.Fatalf("empty registry: %d %s, want []", rec2.Code, rec2.Body)
	}
}

func TestHandleCommandDispatchRegisteredCommand(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil, nil)
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
	s := NewServer(nil, nil, nil, nil, nil, nil)
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
	s2 := NewServer(nil, nil, nil, nil, nil, nil)
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
	s := NewServer(nil, nil, nil, nil, nil, nil)
	offerServices(t, s, map[string]any{"skills": listingSkills{}})

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

// realChainCapsules mounts the capsules a dispatch actually reaches -
// for real, not as fakes - and hands the server their live Context.
// cron mounts LAST, which is the pre-fix shape that made it the chain's
// head: every dispatch answered with cron's text (card t_d923b50a).
func realChainCapsules(t *testing.T) *Server {
	t.Helper()
	t.Setenv("OMEGA_HOME", t.TempDir())
	plugins := []agent.Plugin{
		observability.NewPlugin(),
		store.NewPlugin(),
		notifications.NewPlugin(),
		verdict.NewPlugin(),
		cron.NewPlugin(),
	}
	ctx := &core.Context{CWD: t.TempDir(), Configs: agent.RouteConfigs(plugins, nil)}
	reg, err := agent.MountAllAPI(plugins, ctx, nil, nil)
	if err != nil {
		t.Fatalf("mount capsules: %v", err)
	}
	t.Cleanup(func() { _ = agent.DismountAll(reg, ctx) })

	s := NewServer(nil, nil, nil, nil, nil, nil)
	if err := s.RewireSeams(ctx, nil, nil); err != nil {
		t.Fatalf("rewire: %v", err)
	}
	return s
}

// TestCommandDispatchReachesTheOwningCapsule is the wire boundary of
// the chain-on-miss law: POST /commands/dispatch answers with the
// capsule that OWNS the name, and no other capsule's text leaks in.
// /logs is the argument-less case (its answer may be the capsule's own
// refusal on a home with no log file) - it still may never carry the
// cron text.
func TestCommandDispatchReachesTheOwningCapsule(t *testing.T) {
	s := realChainCapsules(t)

	cases := []struct {
		name     string
		wantText string // "" = the capsule may refuse; only the absence law applies
	}{
		{"/cron", "[cron]"},
		{"/verdict", "[verdicts]"},
		{"/notifications", "notifications:"},
		{"/logs", ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/commands/dispatch",
				strings.NewReader(`{"name":"`+tt.name+`"}`)))
			body := rec.Body.String()
			if tt.name != "/cron" && strings.Contains(body, "[cron]") {
				t.Fatalf("%s answered with the CRON text (the swallow): %d %s", tt.name, rec.Code, body)
			}
			if tt.wantText == "" {
				return
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: %d %s, want 200 with the capsule's own output", tt.name, rec.Code, body)
			}
			var got commandDispatchResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !strings.Contains(got.Text, tt.wantText) {
				t.Fatalf("%s answered %q, want the owner's output containing %q", tt.name, got.Text, tt.wantText)
			}
		})
	}
}
