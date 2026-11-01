package http_channel

// server_registry_test.go: the registry-toggle handler gates (card
// t_2d8deb8c). The locked default agent is a HANDLER law: a PUT
// disable for the default agent must be refused before the seam is
// touched, answer a visible 4xx, and leave the fold unchanged - a
// curl must hit the same wall the UI's locked pill renders.

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// foldStub records ToggleAgent calls and serves a fixed fold.
type foldStub struct {
	agents  []string
	calls   [][2]string // [name, direction]
	refused bool
}

func (f *foldStub) Disabled() core.DisabledNames {
	return core.DisabledNames{Skills: []string{}, Tools: []string{}, Agents: append([]string{}, f.agents...)}
}
func (f *foldStub) ToggleSkill(name string, enabled bool) error { return nil }
func (f *foldStub) ToggleTool(name string, enabled bool) error  { return nil }
func (f *foldStub) ToggleAgent(name string, enabled bool) error {
	if f.refused {
		return errors.New("toggled anyway")
	}
	f.calls = append(f.calls, [2]string{name, dirWord(enabled)})
	return nil
}

func dirWord(enabled bool) string {
	if enabled {
		return "enable"
	}
	return "disable"
}

func newToggleTestServer(t *testing.T, rt core.RegistryToggles) *httptest.Server {
	t.Helper()
	s, _ := newTestServerWithStore(t)
	s.registries = rt
	mux := http.NewServeMux()
	mux.HandleFunc("GET /disabled", s.handleDisabled)
	mux.HandleFunc("PUT /disabled/agents", s.handleDisabledAgents)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func putDisabled(t *testing.T, url, name string, enabled bool) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(
		`{"name": `+quoteJSON(name)+`, "enabled": `+boolJSON(enabled)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body := make([]byte, 4096)
	n, _ := res.Body.Read(body)
	return res.StatusCode, string(body[:n])
}

func quoteJSON(s string) string {
	return "\"" + strings.ReplaceAll(s, "\"", "\\\"") + "\""
}
func boolJSON(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// TestPutDisabledAgentsRefusesDefaultAgent pins the trust boundary
// (card t_2d8deb8c, Endo verbatim "it cannot be disabled EVER!"): the
// refusal lives in the handler - the seam is never called, the
// response is a visible 4xx with the reason, and the fold is
// unchanged afterwards.
func TestPutDisabledAgentsRefusesDefaultAgent(t *testing.T) {
	stub := &foldStub{}
	srv := newToggleTestServer(t, stub)
	code, body := putDisabled(t, srv.URL+"/disabled/agents", "default", false)
	if code < 400 || code >= 500 {
		t.Fatalf("PUT default status = %d (%s), want 4xx", code, body)
	}
	if !strings.Contains(body, "cannot be disabled") {
		t.Errorf("refusal text missing the law: %q", body)
	}
	if len(stub.calls) != 0 {
		t.Errorf("the seam was called with %v - the default disable escaped the handler", stub.calls)
	}
	// the fold is unchanged: GET answers without "default".
	code, fold := getJSON(t, srv.URL+"/disabled")
	if code != http.StatusOK || strings.Contains(string(fold), "\"default\"") {
		t.Fatalf("fold changed after the refusal: %d %s", code, fold)
	}
	// the ENABLE direction still passes through (stale-entry cleanup).
	code, _ = putDisabled(t, srv.URL+"/disabled/agents", "default", true)
	if code != http.StatusOK {
		t.Errorf("enable default status = %d, want 200 no-op", code)
	}
	if len(stub.calls) != 1 || stub.calls[0][0] != "default" || stub.calls[0][1] != "enable" {
		t.Errorf("enable-default calls = %v, want one enable passthrough", stub.calls)
	}
}

// TestPutDisabledAgentsStillToggles agents other than the default:
// the guard is scoped to the default agent only.
func TestPutDisabledAgentsStillToggles(t *testing.T) {
	stub := &foldStub{}
	srv := newToggleTestServer(t, stub)
	code, body := putDisabled(t, srv.URL+"/disabled/agents", "coder", false)
	if code != http.StatusOK {
		t.Fatalf("PUT coder status = %d (%s)", code, body)
	}
	if len(stub.calls) != 1 || stub.calls[0][0] != "coder" {
		t.Fatalf("seam calls = %v, want one coder toggle", stub.calls)
	}
}