package api

// /commands palette surface tests.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

// listingSkills serves the serving surface's own face (part 3).
type listingSkills struct{}

// errNotFound: the skill provider's plain not-found phrasing.
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

// realChainCapsules mounts the capsules a dispatch actually reaches.
func realChainCapsules(t *testing.T) *Server {
	t.Helper()
	s := NewServer(nil, nil, nil, nil, nil, nil)
	if err := s.RewireSeams(mountRealChain(t), nil, nil); err != nil {
		t.Fatalf("rewire: %v", err)
	}
	return s
}

// realChainServer is realChainCapsules with the store seam wired into the server too.
func realChainServer(t *testing.T) *Server {
	t.Helper()
	ctx := mountRealChain(t)
	st, _ := ctx.Services().Resolve("store").(agent.StoreProvider)
	if st == nil {
		t.Fatal("the chain mounted no store seam")
	}
	s := NewServer(nil, nil, st, nil, nil, nil)
	if err := s.RewireSeams(ctx, nil, nil); err != nil {
		t.Fatalf("rewire: %v", err)
	}
	return s
}

// mountRealChain mounts the real capsule chain into a fresh temp home.
func mountRealChain(t *testing.T) *core.Context {
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
	return ctx
}

// TestCommandDispatchReachesTheOwningCapsule is the wire boundary of the. /logs is the
// argument-less case (its answer may be the capsule's own refusal on a home with no log
// file) - it still may never carry the cron text.
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

// - the dispatch contract ---------------------

// TestCommandDispatchDistinguishesMissFromRefusal is the wire boundary of the miss.
func TestCommandDispatchDistinguishesMissFromRefusal(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil, nil)
	s.seams.mu.Lock()
	s.seams.commands = []core.ExtensionCommand{{Name: "/ghost"}, {Name: "/export"}}
	s.seams.commandHandler = func(_ context.Context, name, _ string) (core.CommandResult, error) {
		if name == "/ghost" {
			return core.CommandResult{}, core.CommandMiss(name)
		}
		return core.CommandResult{}, errors.New("no active session to export")
	}
	s.seams.mu.Unlock()

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/commands/dispatch",
		strings.NewReader(`{"name":"/ghost"}`)))
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "unknown command: /ghost") {
		t.Fatalf("miss: %d %q, want 404 unknown command", rec.Code, rec.Body.String())
	}

	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/commands/dispatch",
		strings.NewReader(`{"name":"/export"}`)))
	body := rec2.Body.String()
	if rec2.Code != http.StatusInternalServerError {
		t.Fatalf("refusal: %d %q, want 500 carrying the owner's error", rec2.Code, body)
	}
	if !strings.Contains(body, "no active session to export") {
		t.Fatalf("refusal body = %q, want the owner's own message", body)
	}
	if strings.Contains(body, "unknown command") || strings.Contains(body, "no handler") {
		t.Fatalf("refusal body = %q: an owner's refusal must never read as a miss", body)
	}
}

// TestCommandDispatchBindsTheInvokingSession: the wire session reaches the handler.
func TestCommandDispatchBindsTheInvokingSession(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil, nil)
	s.seams.mu.Lock()
	s.seams.commands = []core.ExtensionCommand{{Name: "/ping"}}
	s.seams.commandHandler = func(ctx context.Context, _, args string) (core.CommandResult, error) {
		return core.CommandResult{Text: "session=" + core.CommandSession(ctx) + " args=" + args}, nil
	}
	s.seams.mu.Unlock()

	cases := []struct {
		body string
		want string
	}{
		{`{"name":"/ping","args":"x","session_id":"sess-9"}`, "session=sess-9 args=x"},
		{`{"name":"/ping","args":"x"}`, "session= args=x"},
	}
	for _, tt := range cases {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/commands/dispatch",
			strings.NewReader(tt.body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", tt.body, rec.Code, rec.Body)
		}
		var got commandDispatchResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.Text != tt.want {
			t.Fatalf("%s -> %q, want %q", tt.body, got.Text, tt.want)
		}
	}
}

// TestCommandDispatchSessionCommandsActOnTheTab drives the five session commands.
func TestCommandDispatchSessionCommandsActOnTheTab(t *testing.T) {
	s := realChainServer(t)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sessions",
		strings.NewReader(`{"id":"tab-sess"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", rec.Code, rec.Body)
	}

	dispatch := func(t *testing.T, body string) (int, commandDispatchResponse, string) {
		t.Helper()
		r := httptest.NewRecorder()
		s.Handler().ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/commands/dispatch",
			strings.NewReader(body)))
		var out commandDispatchResponse
		_ = json.Unmarshal(r.Body.Bytes(), &out)
		return r.Code, out, r.Body.String()
	}
	// body marshals a dispatch request properly: a Windows path in args
	// is JSON-escaped, never spliced raw into the literal
	body := func(name, args, session string) string {
		req := commandDispatchRequest{Name: name, Args: args, SessionID: session}
		b, _ := json.Marshal(req)
		return string(b)
	}

	exportPath := t.TempDir() + "/tab.jsonl"
	code, out, raw := dispatch(t, body("/export", exportPath, "tab-sess"))
	if code != http.StatusOK || !strings.Contains(out.Text, "exported") || !strings.Contains(out.Text, "tab.jsonl") {
		t.Fatalf("/export with the wire session: %d %q, want the export of the tab's session", code, raw)
	}

	code, out, raw = dispatch(t, body("/label", "from the palette", "tab-sess"))
	if code != http.StatusOK || !strings.Contains(out.Text, "from the palette") {
		t.Fatalf("/label with the wire session: %d %q", code, raw)
	}
	if len(out.Actions) != 0 {
		t.Fatalf("/label actions = %+v, want none - set_label is an intent no surface executes", out.Actions)
	}

	code, out, raw = dispatch(t, body("/branch", "", "tab-sess"))
	if code != http.StatusOK || !strings.Contains(out.Text, "[branched ") {
		t.Fatalf("/branch with the wire session: %d %q", code, raw)
	}
	if len(out.Actions) != 0 {
		t.Fatalf("/branch actions = %+v, want none - branch_session is an intent no surface executes", out.Actions)
	}

	code, out, raw = dispatch(t, body("/resume", "#1", ""))
	if code != http.StatusOK || !strings.Contains(out.Text, "[resume: ") {
		t.Fatalf("/resume: %d %q, want the resolved session's line", code, raw)
	}
	if len(out.Actions) != 0 {
		t.Fatalf("/resume actions = %+v, want none - load_session is an intent no surface executes", out.Actions)
	}

	// /new is NOT a store command any more: it declared new_session, nothing ran it, and
	// the two surfaces that can open a conversation do it themselves (t_146d3133).
	code, _, raw = dispatch(t, body("/new", "", ""))
	if code != http.StatusNotFound {
		t.Fatalf("/new: %d %q, want 404 - the store cannot open a conversation", code, raw)
	}

	// no session on the wire: the owner's honest refusal, never
	// "no handler for /export" and never a silent success
	code, _, raw = dispatch(t, body("/export", "", ""))
	if code != http.StatusInternalServerError || !strings.Contains(raw, "no active session to export") {
		t.Fatalf("/export without a session: %d %q, want the owner's refusal", code, raw)
	}

	// the session-free commands answer exactly.
	for _, name := range []string{"/sessions", "/tree", "/search"} {
		code, _, got := dispatch(t, body(name, "", ""))
		if strings.Contains(got, "no handler for") {
			t.Fatalf("%s read as an unclaimed name: %d %q", name, code, got)
		}
	}
}

// TestDispatchShipsOnlyExecutedActions is the executor law (t_146d3133): an intent may
// leave /commands/dispatch only when a host in THIS repo runs it. The dispatch consumes
// run_compact itself (it owns the session shape), so that is the whole allowed set -
// new_session, load_session, branch_session and set_label had no executor, rode a wire the
// client had no field for, and turned a command into a silent no-op.
func TestDispatchShipsOnlyExecutedActions(t *testing.T) {
	s := realChainServer(t)
	s.seams.mu.RLock()
	names := make([]string, 0, len(s.seams.commands))
	for _, c := range s.seams.commands {
		names = append(names, c.Name)
	}
	s.seams.mu.RUnlock()
	if len(names) == 0 {
		t.Fatal("the real chain registered no commands - the law would pass vacuously")
	}

	for _, name := range names {
		args := ""
		if name == "/export" {
			// the only command that writes: keep its output in the test's own temp dir
			args = filepath.Join(t.TempDir(), "export.jsonl")
		}
		req, _ := json.Marshal(commandDispatchRequest{Name: name, Args: args, SessionID: "law-sess"})
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/commands/dispatch",
			strings.NewReader(string(req))))
		if rec.Code != http.StatusOK {
			continue // a refusal carries no action - the next surface answers it
		}
		var out commandDispatchResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
		for _, a := range out.Actions {
			if a.Type != "run_compact" {
				t.Fatalf("%s shipped action %q - nothing in this repo executes that type", name, a.Type)
			}
		}
	}
}
