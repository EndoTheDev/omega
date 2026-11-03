package api

// server_agents_test.go: the /agents REST surface gates (card
// t_7e2b7002). Route note: the mux mount lines live in server.go,
// owned by card t_14f73742 while it runs (same-file ruling, recorded
// on the card). These tests wire the REAL handlers into a local mux
// exactly the way the mount lines will - so the review pass's mount
// drops tests-already-green endpoints in.
//
// PROFILE probes set OMEGA_HOME at a scratch home: the surface is
// store-free on the profile side (no personal content is ever seeded).

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/store"
)

// newAgentsTestServer wires the handlers with the deferred mount
// shape and returns the server + the store.
func newAgentsTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	s, st := newTestServerWithStore(t)
	t.Setenv("OMEGA_HOME", t.TempDir())
	if err := agent.ScaffoldAgent(os.Getenv("OMEGA_HOME"), "scout", "probe duty"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	// the deferred-mount lines (see server_agents.go header) plus the
	// edit affordance (card t_2d8deb8c): the method-pinned PUT rides a
	// pattern of its own - GET /agents/{name} stays on the unpinned
	// line above it.
	mux.HandleFunc("/agents", s.handleAgents)
	mux.HandleFunc("/agents/{name}", s.handleAgent)
	mux.HandleFunc("PUT /agents/{name}", s.handleAgentUpdate)
	mux.HandleFunc("/agents/{name}/sessions", s.handleAgentSessions)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, st
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

// postAny posts a JSON body and returns the status only (the generic
// postJSON helper in server_variants_test.go returns a decoded map,
// this file mostly asserts statuses).
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

func TestAgentsListEndpoint(t *testing.T) {
	srv, _ := newAgentsTestServer(t)
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
	srv, _ := newAgentsTestServer(t)
	home := os.Getenv("OMEGA_HOME")
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
	srv, _ := newAgentsTestServer(t)
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
	srv, st := newAgentsTestServer(t)

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
	// asserts the store filter the deferred GET /sessions rides on.
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
// Scout is fresh here: TestAgentSessionsEndpoint runs its own server.
func TestAgentSessionsEmptyListEmitsArray(t *testing.T) {
	srv, _ := newAgentsTestServer(t)
	code, body := getJSON(t, srv.URL+"/agents/scout/sessions")
	if code != http.StatusOK {
		t.Fatalf("scout sessions status %d", code)
	}
	if string(body) != "[]\n" {
		t.Fatalf("empty agent sessions body = %q, want \"[]\" (null crashes the webui render)", body)
	}
}

func TestAgentsListCarriesSessionCounts(t *testing.T) {
	srv, _ := newAgentsTestServer(t)
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

// TestAgentUpdateEndpoint pins the expansion's edit affordance (card
// t_2d8deb8c, ruling 3): PUT /agents/{name} writes the scaffold field
// set through the agent config layer, nil fields stay untouched, the
// DEFAULT agent answers 409 locked, an unknown agent 404s, and an
// unscaffolded provider type is the 400 lane.
func TestAgentUpdateEndpoint(t *testing.T) {
	srv, _ := newAgentsTestServer(t)

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
