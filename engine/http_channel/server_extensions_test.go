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
	"context"
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
	s := NewServer(a, nil, nil, nil, nil, nil)

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
		// Expansion detail (card t_210a0d19): the name arrays ride the
		// same wire and must agree with the counts - the webui's expanded
		// row renders them verbatim, a count/name split would lie.
		if row.ToolNames == nil || len(row.ToolNames) != row.Tools {
			t.Errorf("%s: tool_names %v, want %d names", row.Name, row.ToolNames, row.Tools)
		}
		if row.CommandNames == nil || len(row.CommandNames) != row.Commands {
			t.Errorf("%s: command_names %v, want %d names", row.Name, row.CommandNames, row.Commands)
		}
	}
	// Skill inventory names ride verbatim: find the skills row's provider
	// tool, and the /skills slash command in its command_names.
	for _, row := range got {
		if row.Name != "skills" {
			continue
		}
		found := false
		for _, n := range row.ToolNames {
			if n == "skills" {
				found = true
			}
		}
		if !found {
			t.Errorf("skills: tool_names %v does not name the skills provider tool", row.ToolNames)
		}
		if len(row.CommandNames) != 1 || row.CommandNames[0] != "/skills" {
			t.Errorf("skills: command_names %v, want [\"/skills\"]", row.CommandNames)
		}
	}
}

func TestHandleExtensionsEmptyRegistry(t *testing.T) {
	// Zero extensions mounted: an honest empty array, like the other
	// inspector reads. The webui tab renders its empty state from this.
	a := core.NewAgent(newMockProvider("mock"), nil, 0)
	s := NewServer(a, nil, nil, nil, nil, nil)

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
	s := NewServer(a, nil, nil, nil, nil, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/extensions", nil)
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /extensions: %d, want 405", rec.Code)
	}
}

// toggleRowCapsule is the E2 probe for the /extensions staleness law
// (card t_b2654d6b): a toggleable capsule that mounts one tool and one
// command, so its inventory row has counts to unwind with it.
type toggleRowCapsule struct{ name string }

func (c *toggleRowCapsule) Name() string                 { return c.name }
func (c *toggleRowCapsule) Provides() []string           { return []string{c.name} }
func (c *toggleRowCapsule) Requires() []string           { return nil }
func (c *toggleRowCapsule) Toggleable() bool             { return true }
func (c *toggleRowCapsule) Mount(*core.Context) error    { return nil }
func (c *toggleRowCapsule) Dismount(*core.Context) error { return nil }
func (c *toggleRowCapsule) Description() string          { return c.name + " capsule" }

func (c *toggleRowCapsule) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	if err := api.RegisterTool(c.name+".probe", core.Tool{
		Description: "probe",
		Run:         func(context.Context, map[string]any) (string, error) { return "ok", nil },
	}); err != nil {
		return err
	}
	return api.RegisterCommand("/"+c.name, "probe", nil,
		func(context.Context, string, string) (core.CommandResult, error) { return core.CommandResult{}, nil })
}

// TestHandleExtensionsFollowsLiveToggle pins the inventory law: after
// a live module toggle plus the host's re-wire (core.LiveSeams, what
// cmd/omega does on every successful toggle), GET /extensions lists
// exactly what is mounted - the disabled capsule's row drops, the
// re-enabled one's row returns with its counts. Before the fix the
// endpoint read the mount-time snapshot and kept listing a disabled
// module while GET /modules said "disabled".
func TestHandleExtensionsFollowsLiveToggle(t *testing.T) {
	ctx := &core.Context{}
	plugins := []agent.Plugin{
		&provider.Plugin{},
		&toggleRowCapsule{name: "cron"},
	}
	reg, err := agent.MountAllAPI(plugins, ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := core.NewFromContext(ctx, core.AgentOptions{})
	s := NewServer(a, nil, nil, nil, nil, nil)

	extensionRows := func() []extensionListing {
		t.Helper()
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/extensions", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /extensions: %d %s", rec.Code, rec.Body)
		}
		var rows []extensionListing
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return rows
	}
	rowOf := func(rows []extensionListing, name string) *extensionListing {
		for i := range rows {
			if rows[i].Name == name {
				return &rows[i]
			}
		}
		return nil
	}

	// Boot state: cron mounted, row present with its mounted counts.
	rows := extensionRows()
	cronRow := rowOf(rows, "cron")
	if cronRow == nil {
		t.Fatalf("boot: cron missing from /extensions: %+v", rows)
	}
	if cronRow.Tools != 1 || cronRow.Commands != 1 {
		t.Fatalf("boot: cron row counts tools=%d commands=%d, want 1/1", cronRow.Tools, cronRow.Commands)
	}

	// Toggle OFF + the host's re-wire: the row must drop.
	if err := reg.Toggle(ctx, "cron", false, core.StartDeps{}); err != nil {
		t.Fatal(err)
	}
	if err := s.rewireSeams(ctx); err != nil {
		t.Fatal(err)
	}
	if rows = extensionRows(); rowOf(rows, "cron") != nil {
		t.Fatalf("toggle-off: cron still listed on /extensions: %+v (stale inventory)", rows)
	}
	if len(rows) != 1 || rows[0].Name != "provider" {
		t.Fatalf("toggle-off: want only provider, got %+v", rows)
	}

	// Toggle ON + re-wire: the row returns with the same counts.
	if err := reg.Toggle(ctx, "cron", true, core.StartDeps{}); err != nil {
		t.Fatal(err)
	}
	if err := s.rewireSeams(ctx); err != nil {
		t.Fatal(err)
	}
	if rows = extensionRows(); rowOf(rows, "cron") == nil {
		t.Fatalf("toggle-on: cron not back on /extensions: %+v", rows)
	}
	if cronRow := rowOf(rows, "cron"); cronRow.Tools != 1 || cronRow.Commands != 1 {
		t.Fatalf("toggle-on: cron row counts tools=%d commands=%d, want 1/1", cronRow.Tools, cronRow.Commands)
	}

	// The inventory matches the Context verbatim after every toggle:
	// one authoritative listing, the journal replay.
	if got := a.ExtensionInfos(); len(got) != len(ctx.Infos) {
		t.Fatalf("agent inventory %d rows, ctx.Infos has %d", len(got), len(ctx.Infos))
	}
}
