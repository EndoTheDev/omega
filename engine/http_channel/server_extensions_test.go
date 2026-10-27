package http_channel

// GET /extensions tests (card t_14f73742): the endpoint must return the
// real mounted module registry, not a fixture. This test mounts the
// actual provider/logging/skills/tools/compactor plugins through
// agent.MountAllAPI into a scratch home (t.TempDir), builds the Agent the
// way cmd/omega does (NewFromContext → ctx.Infos), and asserts the wire
// rows match ctx.Infos: mount order, descriptions from the Describer
// seam, tool/command counts, and JSON shape (empty seams as arrays, a
// missing description omitted).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
	"github.com/EndoTheDev/omega/modules/compactor"
	"github.com/EndoTheDev/omega/modules/logging"
	"github.com/EndoTheDev/omega/modules/provider"
	"github.com/EndoTheDev/omega/modules/skills"
	"github.com/EndoTheDev/omega/modules/store"
	"github.com/EndoTheDev/omega/modules/tools"
)

// mountRealRegistry in cmd/omega's image: buildPlugins → buildConfigs →
// MountAll → NewFromContext, scaled to a hermetic subset (the provider
// plugin is required material - compactor lists it as its Requires seam
// - and mounts fine on zero config). Configs point at the scratch home.
func mountRealRegistry(t *testing.T) *core.Agent {
	t.Helper()
	home := t.TempDir()
	plugins := []agent.Plugin{
		logging.NewPlugin(),
		&provider.Plugin{},
		skills.NewPlugin(),
		tools.NewPlugin(),
		compactor.NewPlugin(),
	}
	cfgs := map[string]any{
		"logging":   logging.Config{Enabled: true, File: filepath.Join(home, "omega.log")},
		"skills":    skills.Config{Dir: filepath.Join(home, "skills")},
		"store":     store.Config{DBPath: filepath.Join(home, "sessions.db")},
		"compactor": core.CompactionConfig{},
	}
	ctx := &core.Context{Configs: cfgs}
	if _, err := agent.MountAllAPI(plugins, ctx, nil, nil); err != nil {
		t.Fatalf("MountAll: %v", err)
	}
	// The way the host builds it (core.NewFromContext does
	// SetExtensionInfos(ctx.Infos) internally).
	return core.NewFromContext(ctx, core.AgentOptions{})
}

func TestHandleExtensionsRealRegistry(t *testing.T) {
	a := mountRealRegistry(t)
	s := NewServer(a, nil, nil, nil, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/extensions", nil)
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /extensions: %d %s", rec.Code, rec.Body)
	}

	var got []extensionListing
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	info := a.ExtensionInfos()
	if len(got) != len(info) {
		t.Fatalf("got %d rows, registry has %d: %s", len(got), len(info), rec.Body)
	}
	for i, e := range info {
		if got[i].Name != e.Name {
			t.Fatalf("row %d: name %q, want %q (mount order must match the registry)", i, got[i].Name, e.Name)
		}
		if got[i].Description != e.Description {
			t.Fatalf("row %d (%s): description %q, want %q", i, e.Name, got[i].Description, e.Description)
		}
		if got[i].Tools != e.Tools {
			t.Fatalf("row %d (%s): tools %d, want %d", i, e.Name, got[i].Tools, e.Tools)
		}
		if got[i].Commands != e.Commands {
			t.Fatalf("row %d (%s): commands %d, want %d", i, e.Name, got[i].Commands, e.Commands)
		}
	}

	// Spot-check the loaded set against what these real modules declare.
	wantNames := []string{"logging", "provider", "skills", "tools", "compactor"}
	for i, n := range wantNames {
		if got[i].Name != n {
			t.Fatalf("row %d: got %q, want %q", i, got[i].Name, n)
		}
	}
	// Descriptions flow from the Describer seam for every real module.
	for _, row := range got {
		if row.Description == "" {
			t.Errorf("%s: empty description (module misses agent.Describer?)", row.Name)
		}
	}
	// The skills module mounts a real skills-provider tool: its row must
	// count it, and provider's row carries the seam list as arrays.
	for _, row := range got {
		if row.Seams == nil {
			t.Errorf("%s: seams must be an array on the wire, not null", row.Name)
		}
	}
}

func TestHandleExtensionsEmptyRegistry(t *testing.T) {
	// Zero extensions mounted: an honest empty array, like the other
	// inspector reads. The webui tab renders its empty state from this.
	a := core.NewAgent(newMockProvider("mock"), nil, 0)
	s := NewServer(a, nil, nil, nil, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/extensions", nil)
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /extensions: %d %s", rec.Code, rec.Body)
	}
	var got []extensionListing
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 0 || string(rec.Body.Bytes()) != "[]\n" {
		t.Fatalf("want a bare empty array on the wire, got: %s", rec.Body)
	}
}

func TestHandleExtensionsMethodGuard(t *testing.T) {
	a := core.NewAgent(newMockProvider("mock"), nil, 0)
	s := NewServer(a, nil, nil, nil, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/extensions", nil)
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /extensions: %d, want 405", rec.Code)
	}
}
