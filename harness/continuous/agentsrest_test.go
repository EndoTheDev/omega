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
	machine := NewMachine(home, Default())
	machine.SetStoreResolver(func() agent.StoreProvider { return st })
	ar := &agentRoutes{home: home, ctx: ctx, machine: machine}
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

// TestCloneEndpoint pins the CLONE law on the wire: one create door with
// `inherit_from` materializes a full copy (definition, skills, persona
// files, session store) and re-tags the copied sessions, so the clone's
// roster row carries the source's history from birth.
func TestCloneEndpoint(t *testing.T) {
	srv, st, home := newAgentsFixture(t)
	// a session owned by scout, so the copy has history to re-tag
	ctx := context.Background()
	if err := st.CreateAgentSession(ctx, "scout-sess", "", "probe", "scout"); err != nil {
		t.Fatal(err)
	}
	scoutData := filepath.Join(agent.AgentDir(home, "scout"), "data")
	if err := os.MkdirAll(scoutData, 0o755); err != nil {
		t.Fatal(err)
	}
	// close the live store's handle-free copy: the roster reads the
	// home's own file, so the fixture writes one by opening it.
	src, err := store.Open(filepath.Join(scoutData, "omega.db"))
	if err != nil {
		t.Fatalf("open source store: %v", err)
	}
	if err := src.CreateAgentSession(ctx, "scout-sess", "", "probe", "scout"); err != nil {
		t.Fatal(err)
	}
	if err := src.AppendMessage(ctx, "scout-sess", core.NewUser("wake prompt")); err != nil {
		t.Fatal(err)
	}
	if err := src.Close(); err != nil {
		t.Fatal(err)
	}
	// a persona file and a skill travel with the clone
	if err := os.WriteFile(filepath.Join(agent.AgentDir(home, "scout"), "memory.md"), []byte("scout memory"), 0o644); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(agent.AgentDir(home, "scout"), "skills", "alpha")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# alpha"), 0o644); err != nil {
		t.Fatal(err)
	}

	if res, err := postAny(t, srv.URL+"/agents", map[string]any{
		"name": "probe", "inherit_from": "scout",
	}); err != nil || res != http.StatusCreated {
		t.Fatalf("clone status = %d (%v), want 201", res, err)
	}
	for _, want := range []string{"config/config.yaml", "memory.md", filepath.Join("skills", "alpha", "SKILL.md"), filepath.Join("data", "omega.db")} {
		if _, err := os.Stat(filepath.Join(agent.AgentDir(home, "probe"), want)); err != nil {
			t.Errorf("clone missing %s: %v", want, err)
		}
	}
	// The copied rows read as the CLONE's sessions.
	sessions, err := os.ReadFile(filepath.Join(agent.AgentDir(home, "probe"), "data", "omega.db"))
	if err != nil || len(sessions) == 0 {
		t.Fatalf("cloned store unreadable: %v", err)
	}
	code, body := getJSON(t, srv.URL+"/agents/probe/sessions")
	if code != http.StatusOK {
		t.Fatalf("clone sessions status = %d: %s", code, body)
	}
	if !strings.Contains(string(body), "scout-sess") {
		t.Errorf("the cloned session never arrived under the clone: %s", body)
	}
	// cloning a ghost source is the 409 lane, never a half-built home.
	if res, _ := postAny(t, srv.URL+"/agents", map[string]any{"name": "nope", "inherit_from": "ghost"}); res != http.StatusConflict {
		t.Errorf("ghost source status = %d, want 409", res)
	}
}

// TestCloneEndpointRetagFailureLeavesNoPhantom pins Important 3 on the
// wire: the clone door is ONE step. A copy that lands and then fails to
// re-tag its rows must not leave a registered folder behind - the source's
// rows would read as the source's session AND the roster would list an
// agent nobody finished creating.
func TestCloneEndpointRetagFailureLeavesNoPhantom(t *testing.T) {
	srv, _, home := newAgentsFixture(t)
	prev := retagAgentSessions
	retagAgentSessions = func(string, string, ...string) error {
		return io.ErrUnexpectedEOF
	}
	t.Cleanup(func() { retagAgentSessions = prev })

	res, err := postAny(t, srv.URL+"/agents", map[string]any{"name": "probe", "inherit_from": "scout"})
	if err != nil {
		t.Fatal(err)
	}
	if res != http.StatusInternalServerError {
		t.Errorf("re-tag failure status = %d, want 500", res)
	}
	if _, statErr := os.Stat(agent.AgentDir(home, "probe")); !os.IsNotExist(statErr) {
		t.Errorf("a failed clone left a registered phantom agent (%v)", statErr)
	}
	// The name is free again: a retry with a working re-tag succeeds.
	retagAgentSessions = prev
	if res, _ := postAny(t, srv.URL+"/agents", map[string]any{"name": "probe", "inherit_from": "scout"}); res != http.StatusCreated {
		t.Errorf("retry after the failed clone = %d, want 201", res)
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

// TestAgentUpdateEndpoint pins the roster's edit affordance: PUT writes
// the definition layer's field set, an empty update is an honest no-op,
// the DEFAULT agent answers 409 locked, an unknown agent 404s, and the
// retired secret knobs are the 400 lane (the secret-free provider law).
func TestAgentUpdateEndpoint(t *testing.T) {
	srv, _, home := newAgentsFixture(t)
	seedHostProviders(t, home)

	if res, _ := putAny(t, srv.URL+"/agents/scout", map[string]any{
		"description":       "night watch",
		"model_name":        "claude-4",
		"provider_instance": "work",
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
	if p.Description != "night watch" || p.ModelName != "claude-4" || p.ProviderInstance != "work" {
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
	// every retired secret knob: the 400 lane, naming the law.
	for _, retired := range []string{"provider_type", "host", "api_key"} {
		res, body := putAny(t, srv.URL+"/agents/scout", map[string]any{retired: "x"})
		if res != http.StatusBadRequest {
			t.Errorf("%s status = %d (%s), want 400", retired, res, body)
		}
	}
	// an unknown provider instance is the same 400 lane.
	if res, body := putAny(t, srv.URL+"/agents/scout", map[string]any{"provider_instance": "nope"}); res != http.StatusBadRequest {
		t.Errorf("unknown instance status = %d (%s), want 400", res, body)
	}
}

// seedHostProviders gives the fixture home a settings file with one
// named provider instance, so the definition layer has something legal
// to reference.
func seedHostProviders(t *testing.T, home string) {
	t.Helper()
	body := "provider:\n  type: ollama\n  model_name: glm\nproviders:\n  work:\n    type: openai\n    host: http://localhost:9000\n    api_key: sk-test\n"
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
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
		"system_prompt":     "be terse",
		"max_turns":         42,
		"model_name":        "claude-4",
		"provider_instance": agent.DefaultProviderInstance,
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
	if !lay.ProviderInstance.Set || lay.ProviderInstance.Value != agent.DefaultProviderInstance {
		t.Fatalf("provider.instance was collateral damage: %+v", lay.ProviderInstance)
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

// TestAgentsRosterCanonicalChat pins the field the Agents tab binds a
// chat to (card t_10ac04f0): every roster row carries the agent's
// CANONICAL chat - one stable conversation, never the most recent
// session - and the DEFAULT (main) agent rides the same roster with
// Continuous set. A home with no live canonical omits the field: the
// click mints one, so the absence never makes a row unclickable.
func TestAgentsRosterCanonicalChat(t *testing.T) {
	srv, st, home := newAgentsFixture(t)

	// The store that OWNS scout's sessions: the agent home's own db
	// (per-agent data isolation - the daemon's store never holds them
	// after the boot migration).
	scoutStore, err := store.Open(filepath.Join(home, "agents", "scout", "data", "omega.db"))
	if err != nil {
		t.Fatalf("open scout store: %v", err)
	}
	t.Cleanup(func() { scoutStore.Close() })

	type row struct {
		Name         string `json:"name"`
		SessionCount int    `json:"session_count"`
		Canonical    string `json:"canonical_session_id"`
		LastReport   string `json:"last_report"`
		LastReportAt string `json:"last_report_at"`
		Continuous   bool   `json:"continuous"`
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
	mint := func(name string) (int, row) {
		t.Helper()
		res, err := http.Post(srv.URL+"/agents/"+name+"/canonical", "application/json", nil)
		if err != nil {
			t.Fatalf("POST canonical %s: %v", name, err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		var one row
		_ = json.Unmarshal(body, &one)
		return res.StatusCode, one
	}

	// A fresh home: the rows are there, no canonical exists yet.
	first := roster()
	main, ok := first[agent.DefaultAgentName]
	if !ok {
		t.Fatalf("default agent missing from the roster: %+v", first)
	}
	if main.Continuous {
		t.Errorf("default continuous = true, want false - Continuous marks a WAKE home; the main marker is the name (the webui's DefaultAgentName)")
	}
	if scout := first["scout"]; !scout.Continuous {
		t.Errorf("scout continuous = false, want true (an agents/<name> wake home)")
	}
	if main.Canonical != "" {
		t.Errorf("default canonical = %q before any mint, want empty", main.Canonical)
	}
	if scout := first["scout"]; scout.Canonical != "" {
		t.Errorf("scout canonical = %q with no canonical chat, want empty", scout.Canonical)
	}

	// The click MINTS on a home that holds none: the id is the agent's
	// one permanent conversation, stable across clicks.
	if code, _ := mint("scout"); code != http.StatusOK {
		t.Fatalf("mint scout canonical status %d, want 200", code)
	}
	minted := roster()["scout"].Canonical
	if minted != "scout-canonical" {
		t.Fatalf("scout canonical = %q, want scout-canonical (the seeded stable id)", minted)
	}
	// ...and a second click opens the SAME conversation: no re-mint.
	if code, _ := mint("scout"); code != http.StatusOK {
		t.Fatalf("second mint status %d, want 200", code)
	}
	if again := roster()["scout"].Canonical; again != minted {
		t.Errorf("canonical moved on a second click: %q -> %q", minted, again)
	}

	// A worker run lands (a NEWER session, the most recently touched
	// one): the click target does not move. This is the whole card.
	ctx := context.Background()
	if err := scoutStore.CreateAgentSession(ctx, "sess-run", "", "", "scout"); err != nil {
		t.Fatal(err)
	}
	if err := scoutStore.UpdateSession(ctx, "sess-run", "worker run"); err != nil {
		t.Fatal(err)
	}
	if got := roster()["scout"].Canonical; got != minted {
		t.Fatalf("canonical = %q after a worker run, want %q (most-recent is dead as a rule)", got, minted)
	}

	// The preview line: the newest assistant reply in the canonical chat.
	if err := scoutStore.AppendMessage(ctx, minted, core.NewAssistant("patrol complete,\n0 anomalies")); err != nil {
		t.Fatal(err)
	}
	scout := roster()["scout"]
	if scout.LastReport != "patrol complete, 0 anomalies" {
		t.Errorf("last_report = %q, want the reply collapsed to ONE line", scout.LastReport)
	}
	if scout.LastReportAt == "" {
		t.Error("last_report_at is empty - the preview line carries no age")
	}

	// ARCHIVE = RETIRE: the retired canonical leaves the roster and the
	// next click mints the next generation. The old row stays archived.
	if err := scoutStore.SetSessionArchived(ctx, minted, true); err != nil {
		t.Fatal(err)
	}
	if got := roster()["scout"].Canonical; got != "" {
		t.Fatalf("canonical = %q after archiving it, want empty (the roster never resurrects a retired chat)", got)
	}
	if code, _ := mint("scout"); code != http.StatusOK {
		t.Fatalf("mint after retire status %d, want 200", code)
	}
	if got := roster()["scout"].Canonical; got != "scout-canonical-2" {
		t.Errorf("canonical after retire = %q, want scout-canonical-2", got)
	}
	old, err := scoutStore.GetSession(ctx, minted)
	if err != nil {
		t.Fatalf("retired row: %v", err)
	}
	if !old.Archived {
		t.Error("the retired canonical is not archived - retire means it stays archived")
	}

	// The main agent binds too - and its canonical is MACHINERY-owned, so
	// it never joins the human's session list.
	if code, _ := mint(agent.DefaultAgentName); code != http.StatusOK {
		t.Fatalf("mint main canonical status %d, want 200", code)
	}
	if got := roster()[agent.DefaultAgentName].Canonical; got != CanonicalSessionID {
		t.Errorf("default canonical = %q, want the bare id", got)
	}
	if err := st.CreateSession(ctx, "sess-main", "", ""); err != nil {
		t.Fatal(err)
	}
	listed, err := st.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, sess := range listed {
		if sess.ID == CanonicalSessionID {
			t.Error("main's canonical chat is in the human's session list - the roster's chat must stay out of it")
		}
	}
	if got := roster()[agent.DefaultAgentName].Canonical; got != CanonicalSessionID {
		t.Errorf("default canonical moved when a human session landed: got %q", got)
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
	if one.Canonical != "scout-canonical-2" {
		t.Errorf("detail canonical = %q, want scout-canonical-2", one.Canonical)
	}
}

// TestCreateHomeSeedsTheRosterCanonical pins the round-1 fix (card
// t_10ac04f0) on the path a real agent takes: POST /continuous runs
// CreateHome, and the chat it seeds is BOTH what the roster row hands
// the click AND what /new folds. The fold keys on the machine-owned bit
// (agent/api's handleSessionNew answers "fork" otherwise), so a seed
// written as an ordinary listed row forked the relationship - ruling 2 -
// for every created agent.
func TestCreateHomeSeedsTheRosterCanonical(t *testing.T) {
	home := t.TempDir()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := &core.Context{}
	ctx.ApplyOffer("store", "test", st)
	machine := NewMachine(home, Default())
	machine.SetStoreResolver(func() agent.StoreProvider { return st })
	mux := http.NewServeMux()
	mux.HandleFunc("POST /continuous", createHandler(machine))
	mux.HandleFunc("/agents", (&agentRoutes{home: home, ctx: ctx, machine: machine}).handleAgents)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res, err := http.Post(srv.URL+"/continuous", "application/json",
		strings.NewReader(`{"name":"runner","directive":"watch the door","schedule":"*/5 * * * *"}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("create status %d: %s", res.StatusCode, body)
	}

	// The roster row hands the seeded chat, with no mint needed.
	code, rosterBody := getJSON(t, srv.URL+"/agents")
	if code != http.StatusOK {
		t.Fatalf("roster status %d: %s", code, rosterBody)
	}
	var rows []struct {
		Name      string `json:"name"`
		Canonical string `json:"canonical_session_id"`
	}
	if err := json.Unmarshal(rosterBody, &rows); err != nil {
		t.Fatalf("decode roster: %v", err)
	}
	found := false
	for _, row := range rows {
		if row.Name != "runner" {
			continue
		}
		found = true
		if row.Canonical != "runner-canonical" {
			t.Errorf("runner canonical = %q, want the seeded runner-canonical", row.Canonical)
		}
	}
	if !found {
		t.Fatalf("created agent missing from the roster: %s", rosterBody)
	}

	// The seeded row is machinery-owned and out of the human's list.
	ast, err := store.Open(filepath.Join(agent.AgentDir(home, "runner"), "data", "omega.db"))
	if err != nil {
		t.Fatalf("runner store: %v", err)
	}
	defer ast.Close()
	row, err := ast.GetSession(context.Background(), "runner-canonical")
	if err != nil {
		t.Fatalf("seeded canonical: %v", err)
	}
	if !row.MachineOwned {
		t.Error("the created agent's chat is not machine-owned - /new would fork it instead of compacting it")
	}
	if sessions, err := ast.ListSessions(context.Background()); err != nil || len(sessions) != 0 {
		t.Errorf("created agent's store lists %d sessions, %v - its chat belongs to the roster, not the sidebar", len(sessions), err)
	}
}

// TestAgentsDeleteRemovesTheWholeHome pins ruling 2 on the wire:
// DELETE /agents/{name} removes the HOME (sessions included) and
// answers the session count it took with it, so the panel's receipt
// names the same number its confirm dialog promised. The main agent is
// refused (its home is the daemon's home), a ghost 404s.
func TestAgentsDeleteRemovesTheWholeHome(t *testing.T) {
	srv, _, home := newAgentsFixture(t)
	// two sessions in the agent's OWN store, the rows the delete takes
	for i := 0; i < 2; i++ {
		if res, err := postAny(t, srv.URL+"/agents/scout/sessions", map[string]any{}); err != nil || res != http.StatusCreated {
			t.Fatalf("seed session %d = %d, %v", i, res, err)
		}
	}
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/agents/scout", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete scout = %d: %s", res.StatusCode, body)
	}
	var receipt struct {
		Deleted  string `json:"deleted"`
		Sessions int    `json:"sessions"`
	}
	if err := json.Unmarshal(body, &receipt); err != nil {
		t.Fatalf("decode receipt %s: %v", body, err)
	}
	if receipt.Deleted != "scout" || receipt.Sessions != 2 {
		t.Fatalf("receipt = %+v, want {scout 2}", receipt)
	}
	// The home is gone and the roster no longer lists it - no orphan row.
	if _, err := os.Stat(agent.AgentDir(home, "scout")); !os.IsNotExist(err) {
		t.Fatalf("the agent home survived the DELETE (stat err = %v)", err)
	}
	code, roster := getJSON(t, srv.URL+"/agents")
	if code != http.StatusOK {
		t.Fatalf("roster after delete = %d", code)
	}
	if strings.Contains(string(roster), `"scout"`) {
		t.Fatalf("the deleted agent still rides the roster: %s", roster)
	}
	// Refusals: the main agent's home is the daemon's home (409), a
	// second delete of the same name finds nothing (404).
	del := func(name string) int {
		req, err := http.NewRequest(http.MethodDelete, srv.URL+"/agents/"+name, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		return res.StatusCode
	}
	if got := del("default"); got != http.StatusConflict {
		t.Fatalf("delete default = %d, want 409", got)
	}
	if got := del("scout"); got != http.StatusNotFound {
		t.Fatalf("delete of an already-deleted agent = %d, want 404", got)
	}
}

// forgetRecorder is a store that remembers the delete verb's release
// call (card t_5df01d0f). The routing handle is what locks an agent
// home's files, so the delete verb must release it before removing the
// directory - asserted here without depending on the host's file
// locking.
type forgetRecorder struct {
	*store.Store
	forgot []string
}

func (f *forgetRecorder) ForgetHome(name string) bool {
	f.forgot = append(f.forgot, name)
	return true
}

func TestAgentsDeleteReleasesTheHomeHandle(t *testing.T) {
	home := t.TempDir()
	if err := agent.ScaffoldAgent(home, "scout", "probe duty"); err != nil {
		t.Fatal(err)
	}
	inner, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { inner.Close() })
	rec := &forgetRecorder{Store: inner}
	ctx := &core.Context{}
	ctx.ApplyOffer("store", "test", rec)
	mux := http.NewServeMux()
	for _, row := range (&agentRoutes{home: home, ctx: ctx}).registrationRows() {
		mux.HandleFunc(row.pattern, row.handler)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/agents/scout", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete scout = %d", res.StatusCode)
	}
	if len(rec.forgot) != 1 || rec.forgot[0] != "scout" {
		t.Fatalf("ForgetHome calls = %v, want [scout] (the delete must release the home's store handle first)", rec.forgot)
	}
	if _, err := os.Stat(agent.AgentDir(home, "scout")); !os.IsNotExist(err) {
		t.Fatalf("the agent home survived the DELETE (stat err = %v)", err)
	}
}
