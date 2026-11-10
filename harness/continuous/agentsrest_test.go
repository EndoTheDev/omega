package continuous

// agentsrest_test.go: the /agents roster face's gates (card t_490fed88,
// the shell-ring shrink wave 2). The handlers moved whole from
// agent/api/server_agents.go, so the tests port with them - the
// fixture wires the REAL registrationRows() into a local mux (the way
// the plugin's mount does) and serves it on a real listener.
//
// The scaffold probes use a scratch home dir (no OMEGA_HOME games -
// the home is the struct's field now): no personal content is ever
// seeded, the store is :memory: and offered under the exact key the
// fixture resolves.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
)

// newAgentsFixture mounts the roster REST face on a real listener with
// a scratch home and an in-memory store under the "store" seam key.
func newAgentsFixture(t *testing.T) (*httptest.Server, *store.Store, string) {
	t.Helper()
	home := t.TempDir()
	if err := agent.ScaffoldAgent(home, "scout", "probe duty"); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := &core.Context{}
	ctx.ApplyOffer("store", "test", st)
	ar := &agentRoutes{home: home, ctx: ctx}
	mux := http.NewServeMux()
	for _, row := range ar.registrationRows() {
		mux.HandleFunc(row.pattern, row.handler)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, st, home
}

func getJSON(t *testing.T, url string) (int, []byte) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, body
}

// postAny posts a JSON body and returns the status only (the roster
// creations here assert statuses, the created payload rides the store).
func postAny(t *testing.T, url string, payload any) (int, error) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	res, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	return res.StatusCode, nil
}

// putAny PUTs a JSON body and returns the status + body head.
func putAny(t *testing.T, url string, payload any) (int, string) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, err.Error()
	}
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return 0, err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	return res.StatusCode, string(body)
}

func TestAgentsListEndpoint(t *testing.T) {
	srv, _, _ := newAgentsFixture(t)
	code, body := getJSON(t, srv.URL+"/agents")
	if code != http.StatusOK {
		t.Fatalf("status %d: %s", code, body)
	}
	var profiles []agent.Profile
	if err := json.Unmarshal(body, &profiles); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("profiles = %d, want default+scout", len(profiles))
	}
	if profiles[0].Name != agent.DefaultAgentName || profiles[1].Name != "scout" {
		t.Errorf("order = %v, want default first, then scout", profiles)
	}
	if profiles[1].Description != "probe duty" {
		t.Errorf("scout description = %q", profiles[1].Description)
	}
	// no phantom memory rows in a fresh home.
	for _, p := range profiles {
		if p.MemorySize != 0 {
			t.Errorf("%s carries phantom memory %d", p.Name, p.MemorySize)
		}
	}
}

func TestScaffoldEndpoint(t *testing.T) {
	srv, _, home := newAgentsFixture(t)
	res, _ := postAny(t, srv.URL+"/agents", map[string]any{"name": "writer", "description": "docs agent"})
	if res != http.StatusCreated {
		t.Fatalf("scaffold status = %d", res)
	}
	if _, err := os.Stat(filepath.Join(home, "agents", "writer", "config", "config.yaml")); err != nil {
		t.Fatalf("scaffold dir missing: %v", err)
	}
	// duplicate: 409.
	if res, _ := postAny(t, srv.URL+"/agents", map[string]any{"name": "writer"}); res != http.StatusConflict {
		t.Errorf("duplicate scaffold status = %d, want 409", res)
	}
	// hostile name: 409 too (the scaffold error), never a write.
	if res, _ := postAny(t, srv.URL+"/agents", map[string]any{"name": "../evil"}); res != http.StatusConflict {
		t.Errorf("hostile name status = %d, want 4xx", res)
	}
}

func TestAgentDetailEndpoint(t *testing.T) {
	srv, _, _ := newAgentsFixture(t)
	code, body := getJSON(t, srv.URL+"/agents/scout")
	if code != http.StatusOK {
		t.Fatalf("status %d: %s", code, body)
	}
	var p agent.Profile
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatal(err)
	}
	if p.Name != "scout" {
		t.Errorf("detail name = %q", p.Name)
	}
	// unknown agent 404s.
	if code, _ := getJSON(t, srv.URL+"/agents/ghost"); code != http.StatusNotFound {
		t.Errorf("ghost status = %d, want 404", code)
	}
}

func TestAgentSessionsEndpoint(t *testing.T) {
	srv, st, _ := newAgentsFixture(t)

	// create a session as scout.
	res, err := postAny(t, srv.URL+"/agents/scout/sessions", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res != http.StatusCreated {
		t.Fatalf("create session status = %d", res)
	}

	// the scout list shows it.
	code, body := getJSON(t, srv.URL+"/agents/scout/sessions")
	if code != http.StatusOK {
		t.Fatalf("scout sessions status %d", code)
	}
	var sessions []core.Session
	if err := json.Unmarshal(body, &sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Agent != "scout" {
		t.Fatalf("scout sessions = %+v", sessions)
	}

	// THE list-scoping law: the default list never shows it. This
	// asserts the store filter the capsule list rides on.
	def, err := st.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(def) != 0 {
		t.Errorf("default list leaked scout sessions: %v", def)
	}

	// unknown agent: 404, even for creations.
	if res, _ := postAny(t, srv.URL+"/agents/ghost/sessions", map[string]any{}); res != http.StatusNotFound {
		t.Errorf("ghost create status = %d, want 404", res)
	}
}

// TestAgentSessionsEmptyListEmitsArray pins the list contract (card
// t_619832ee): a freshly scaffolded agent has zero sessions, the store
// returns a nil slice, and the endpoint must still answer literal []
// with 200 - the webui session read crashes on a null .length body.
func TestAgentSessionsEmptyListEmitsArray(t *testing.T) {
	srv, _, _ := newAgentsFixture(t)
	code, body := getJSON(t, srv.URL+"/agents/scout/sessions")
	if code != http.StatusOK {
		t.Fatalf("scout sessions status %d", code)
	}
	if string(body) != "[]\n" {
		t.Fatalf("empty agent sessions body = %q, want \"[]\" (null crashes the webui render)", body)
	}
}

func TestAgentsListCarriesSessionCounts(t *testing.T) {
	srv, _, _ := newAgentsFixture(t)
	if res, err := postAny(t, srv.URL+"/agents/scout/sessions", map[string]any{}); err != nil || res != 201 {
		t.Fatalf("create session: %d %v", res, err)
	}
	_, body := getJSON(t, srv.URL+"/agents")
	var profiles []struct {
		Name         string `json:"name"`
		SessionCount int    `json:"session_count"`
	}
	if err := json.Unmarshal(body, &profiles); err != nil {
		t.Fatal(err)
	}
	var scoutCount int
	for _, p := range profiles {
		if p.Name == "scout" {
			scoutCount = p.SessionCount
		}
	}
	if scoutCount != 1 {
		t.Errorf("scout session_count = %d, want 1", scoutCount)
	}
}

// TestAgentUpdateEndpoint pins the roster's edit affordance (card
// t_2d8deb8c, ruling 3): PUT writes the scaffold field set through the
// agent config layer, an empty update is an honest no-op, the DEFAULT
// agent answers 409 locked, an unknown agent 404s, and an
// unscaffolded provider type is the 400 lane.
func TestAgentUpdateEndpoint(t *testing.T) {
	srv, _, _ := newAgentsFixture(t)

	if res, _ := putAny(t, srv.URL+"/agents/scout", map[string]any{
		"description":   "night watch",
		"model_name":    "claude-4",
		"provider_type": "openai",
	}); res != http.StatusOK {
		t.Fatalf("update status = %d", res)
	}
	code, body := getJSON(t, srv.URL+"/agents/scout")
	if code != http.StatusOK {
		t.Fatalf("re-read status %d: %s", code, body)
	}
	var p agent.Profile
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatal(err)
	}
	if p.Description != "night watch" || p.ModelName != "claude-4" || p.ProviderType != "openai" {
		t.Fatalf("after update: %+v", p)
	}

	// nil fields untouched: an empty update is an honest no-op.
	if res, _ := putAny(t, srv.URL+"/agents/scout", map[string]any{}); res != http.StatusOK {
		t.Errorf("empty update status = %d", res)
	}
	if _, body := getJSON(t, srv.URL+"/agents/scout"); len(body) == 0 {
		t.Errorf("empty update broke the profile")
	}

	// the DEFAULT agent: 409 locked (its config is the home config).
	if res, body := putAny(t, srv.URL+"/agents/default", map[string]any{"description": "x"}); res != http.StatusConflict {
		t.Fatalf("default update status = %d (%s), want 409 locked", res, body)
	}
	// unknown agent: 404.
	if res, _ := putAny(t, srv.URL+"/agents/ghost", map[string]any{"description": "x"}); res != http.StatusNotFound {
		t.Errorf("ghost update status = %d, want 404", res)
	}
	// unscaffolded provider type: the 400 lane.
	if res, body := putAny(t, srv.URL+"/agents/scout", map[string]any{"provider_type": "gemini"}); res != http.StatusBadRequest {
		t.Errorf("bad provider status = %d (%s), want 400", res, body)
	}
}

// TestAgentUpdateEndpointMaxTurns pins the wire shape of the first
// additive field (settings tier 3b, card t_53523d5b): max_turns arrives
// as a JSON NUMBER and lands as a yaml int the wake can parse, 0 is a
// real value, and every malformed shape is the 400 lane.
func TestAgentUpdateEndpointMaxTurns(t *testing.T) {
	srv, _, home := newAgentsFixture(t)

	if res, body := putAny(t, srv.URL+"/agents/scout", map[string]any{"max_turns": 42}); res != http.StatusOK {
		t.Fatalf("max_turns update status = %d (%s)", res, body)
	}
	lay, err := agent.ReadAgentConfigLayer(home, "scout")
	if err != nil {
		t.Fatal(err)
	}
	if lay.MaxTurns == nil || *lay.MaxTurns != 42 {
		t.Fatalf("layer max_turns = %v, want 42", lay.MaxTurns)
	}
	// The file itself carries the int, not a quoted string.
	body, err := os.ReadFile(agent.AgentConfigPath(home, "scout"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "max_turns: 42") {
		t.Fatalf("layer file:\n%s", body)
	}

	// 0 = no cap, a real value.
	if res, body := putAny(t, srv.URL+"/agents/scout", map[string]any{"max_turns": 0}); res != http.StatusOK {
		t.Fatalf("max_turns 0 status = %d (%s)", res, body)
	}
	lay, err = agent.ReadAgentConfigLayer(home, "scout")
	if err != nil {
		t.Fatal(err)
	}
	if lay.MaxTurns == nil || *lay.MaxTurns != 0 {
		t.Fatalf("layer max_turns = %v, want the explicit 0", lay.MaxTurns)
	}

	// Refusals: negative (config.go's floor) and a string (the wire
	// says JSON number - "42" must never reach the layer).
	if res, body := putAny(t, srv.URL+"/agents/scout", map[string]any{"max_turns": -1}); res != http.StatusBadRequest {
		t.Errorf("negative max_turns status = %d (%s), want 400", res, body)
	}
	if res, body := putAny(t, srv.URL+"/agents/scout", map[string]any{"max_turns": "42"}); res != http.StatusBadRequest {
		t.Errorf("string max_turns status = %d (%s), want 400", res, body)
	}
}

// TestAgentUpdateEndpointUnset pins the second additive field: the
// dotted reset vocabulary deletes the layer's key pairs (the fallback to
// main), and the refusals - unknown name, group name, set+unset in one
// request, the read mask as a secret - are all 400.
func TestAgentUpdateEndpointUnset(t *testing.T) {
	srv, _, home := newAgentsFixture(t)

	// Seed overrides through the same endpoint (the one write path).
	if res, body := putAny(t, srv.URL+"/agents/scout", map[string]any{
		"system_prompt": "be terse",
		"max_turns":     42,
		"model_name":    "claude-4",
		"provider_type": "ollama",
	}); res != http.StatusOK {
		t.Fatalf("seed update status = %d (%s)", res, body)
	}

	// The reset: both keys leave the file, the provider knob stays.
	if res, body := putAny(t, srv.URL+"/agents/scout", map[string]any{
		"unset": []string{"system_prompt", "max_turns"},
	}); res != http.StatusOK {
		t.Fatalf("unset status = %d (%s)", res, body)
	}
	lay, err := agent.ReadAgentConfigLayer(home, "scout")
	if err != nil {
		t.Fatal(err)
	}
	if lay.SystemPrompt.Set || lay.MaxTurns != nil {
		t.Fatalf("after unset the layer still overrides: %+v", lay)
	}
	if !lay.ModelName.Set || lay.ModelName.Value != "claude-4" {
		t.Fatalf("provider.model_name was not left alone: %+v", lay.ModelName)
	}

	// A sub-key reset falls back for that knob only.
	if res, body := putAny(t, srv.URL+"/agents/scout", map[string]any{
		"unset": []string{"provider.model_name"},
	}); res != http.StatusOK {
		t.Fatalf("unset model_name status = %d (%s)", res, body)
	}
	lay, err = agent.ReadAgentConfigLayer(home, "scout")
	if err != nil {
		t.Fatal(err)
	}
	if lay.ModelName.Set {
		t.Fatalf("model_name still overridden after its unset: %+v", lay.ModelName)
	}
	if !lay.ProviderType.Set {
		t.Fatalf("provider.type was collateral damage: %+v", lay.ProviderType)
	}

	// Refusal lanes.
	for name, payload := range map[string]map[string]any{
		"unknown unset name":           {"unset": []string{"provider.model"}},
		"group name is not a knob":     {"unset": []string{"provider"}},
		"set and unset in one request": {"system_prompt": "x", "unset": []string{"system_prompt"}},
		"masked api_key":               {"api_key": agent.MaskedSecretPlaceholder},
	} {
		if res, body := putAny(t, srv.URL+"/agents/scout", payload); res != http.StatusBadRequest {
			t.Errorf("%s: status = %d (%s), want 400", name, res, body)
		}
	}

	// The DEFAULT agent still answers 409 first, whatever the body.
	if res, body := putAny(t, srv.URL+"/agents/default", map[string]any{"unset": []string{"max_turns"}}); res != http.StatusConflict {
		t.Errorf("default agent unset status = %d (%s), want 409", res, body)
	}
}

// TestAgentsRosterLastSessionID pins the field the Agents tab binds a
// chat to (card t_81109e1c): every roster row carries the agent's MOST
// RECENT session id - most recent by updated_at, ties to the
// later-created session - and the DEFAULT (main) agent rides the same
// roster with Continuous set. A row with no sessions omits the field
// rather than inventing an id.
func TestAgentsRosterLastSessionID(t *testing.T) {
	srv, st, home := newAgentsFixture(t)

	// The store that OWNS scout's sessions: the agent home's own db
	// (per-agent data isolation - the daemon's store never holds them
	// after the boot migration).
	scoutStore, err := store.Open(filepath.Join(home, "agents", "scout", "data", "omega.db"))
	if err != nil {
		t.Fatalf("open scout store: %v", err)
	}
	t.Cleanup(func() { scoutStore.Close() })
	// main's sessions ride the mounted store (the fixture's seam store).

	type row struct {
		Name          string `json:"name"`
		SessionCount  int    `json:"session_count"`
		LastSessionID string `json:"last_session_id"`
		Continuous    bool   `json:"continuous"`
	}
	roster := func() map[string]row {
		t.Helper()
		code, body := getJSON(t, srv.URL+"/agents")
		if code != http.StatusOK {
			t.Fatalf("roster status %d: %s", code, body)
		}
		var rows []row
		if err := json.Unmarshal(body, &rows); err != nil {
			t.Fatalf("decode: %v", err)
		}
		out := make(map[string]row, len(rows))
		for _, r := range rows {
			out[r.Name] = r
		}
		return out
	}

	// A fresh home: main is in the roster, nothing to bind yet.
	first := roster()
	main, ok := first[agent.DefaultAgentName]
	if !ok {
		t.Fatalf("default agent missing from the roster: %+v", first)
	}
	if main.Continuous {
		t.Errorf("default continuous = true, want false - Continuous marks a WAKE home; the main marker is the name (the webui's DefaultAgentName)")
	}
	if scout := first["scout"]; !scout.Continuous {
		t.Errorf("scout continuous = false, want true (a agents/<name> wake home)")
	}
	if main.LastSessionID != "" {
		t.Errorf("default last_session_id = %q before any session, want empty", main.LastSessionID)
	}
	if scout := first["scout"]; scout.LastSessionID != "" {
		t.Errorf("scout last_session_id = %q with zero sessions, want empty", scout.LastSessionID)
	}

	// Two sessions as scout: the newer one is the tab's click target.
	ctx := context.Background()
	if err := scoutStore.CreateAgentSession(ctx, "sess-old", "", "", "scout"); err != nil {
		t.Fatal(err)
	}
	if err := scoutStore.CreateAgentSession(ctx, "sess-new", "", "", "scout"); err != nil {
		t.Fatal(err)
	}
	scout := roster()["scout"]
	if scout.SessionCount != 2 {
		t.Fatalf("scout session_count = %d, want 2", scout.SessionCount)
	}
	if scout.LastSessionID != "sess-new" {
		t.Errorf("scout last_session_id = %q, want sess-new (newest)", scout.LastSessionID)
	}

	// ACTIVITY wins over creation order: touching the old session moves
	// the click target (the continuous session is the one being used).
	if err := scoutStore.UpdateSession(ctx, "sess-old", "touched"); err != nil {
		t.Fatal(err)
	}
	if got := roster()["scout"].LastSessionID; got != "sess-old" {
		t.Errorf("scout last_session_id = %q after touching sess-old, want sess-old", got)
	}

	// The main agent binds too - it is a roster row like any other.
	if err := st.CreateSession(ctx, "sess-main", "", ""); err != nil {
		t.Fatal(err)
	}
	if got := roster()[agent.DefaultAgentName].LastSessionID; got != "sess-main" {
		t.Errorf("default last_session_id = %q, want sess-main", got)
	}

	// GET /agents/{name} answers the same row shape.
	code, body := getJSON(t, srv.URL+"/agents/scout")
	if code != http.StatusOK {
		t.Fatalf("detail status %d: %s", code, body)
	}
	var one row
	if err := json.Unmarshal(body, &one); err != nil {
		t.Fatal(err)
	}
	if one.LastSessionID != "sess-old" {
		t.Errorf("detail last_session_id = %q, want sess-old", one.LastSessionID)
	}
}
