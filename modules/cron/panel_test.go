package cron

// The canvas surface tests (arc 6, t_31174f80): the jobs panel is
// declared at mount, a hot reload pushes the refreshed panel through
// the sink the canvas runtime installed, and the read-only action
// posture holds. The removal proof that runs against the full plugin
// set lives in cmd/omega/removal_test.go (TestCronRemovalProof).

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// mountedPanelPlugin mounts a plugin (explicit config + temp home) and
// returns the context with the PanelProviders registered.
func mountedPanelPlugin(t *testing.T, jobs ...Job) (*core.Context, *Plugin) {
	t.Helper()
	home := t.TempDir()
	p := NewPluginWithConfig(&Config{Jobs: jobs}, home)
	ctx := &core.Context{}
	if _, err := agent.MountAllAPI([]agent.Plugin{notificationsPlugin{}, p}, ctx, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	return ctx, p
}

func findPanel(panels []core.Panel, id string) (core.Panel, bool) {
	for _, p := range panels {
		if p.ID == id {
			return p, true
		}
	}
	return core.Panel{}, false
}

// TestCapsulePanelDeclaresJobs: the mount registers the jobs panel;
// the snapshot carries it with the job rows verbatim, sorted by name.
func TestCapsulePanelDeclaresJobs(t *testing.T) {
	ctx, _ := mountedPanelPlugin(t,
		Job{Name: "zz-daily", Schedule: "0 8 * * *", Prompt: "p"},
		Job{Name: "aa-minute", Schedule: "*/5 * * * *", Prompt: "p", Enabled: enabledOff()},
	)
	canvas, err := agent.NewCanvas(ctx, nil)
	if err != nil {
		t.Fatalf("canvas: %v", err)
	}
	panel, ok := findPanel(canvas.Snapshot(), jobsPanelID)
	if !ok {
		t.Fatalf("jobs panel missing from snapshot: %+v", canvas.Snapshot())
	}
	if panel.Title != "Cron" || panel.Kind != "list" {
		t.Fatalf("panel head = %q/%q, want Cron/list", panel.Title, panel.Kind)
	}
	payload, err := jsonOf(panel.Payload)
	if err != nil {
		t.Fatalf("payload: %v", err)
	}
	if !strings.Contains(payload, `"name":"aa-minute"`) || !strings.Contains(payload, `"total":2`) {
		t.Fatalf("payload lost the jobs: %s", payload)
	}
	// the capsule's declared face: exactly one action, the
	// mutation-free refresh - cron proposes nothing, edits nothing
	if len(panel.Actions) != 1 {
		t.Fatalf("actions = %+v, want exactly the refresh binding", panel.Actions)
	}
	a := panel.Actions[0]
	if a.ID != actionRefresh || a.Module != "cron" || a.Label != "refresh" {
		t.Fatalf("action = %+v, want refresh_jobs@cron", a)
	}
}

// TestHotReloadPushesPanelDelta: a crons.yaml reload (the module's one
// internal mutation face) pushes the refreshed jobs panel through the
// live canvas - the snapshot reflects it too.
func TestHotReloadPushesPanelDelta(t *testing.T) {
	ctx, p := mountedPanelPlugin(t, Job{Name: "starter", Schedule: "0 5 1 * *", Prompt: "p"})
	var deltas []core.Panel
	canvas, err := agent.NewCanvas(ctx, func(ev core.Event) {
		if d, ok := ev.(core.ViewModelDelta); ok {
			deltas = append(deltas, d.Panel)
		}
	})
	if err != nil {
		t.Fatalf("canvas: %v", err)
	}

	r := p.runner.(*Runner)
	r.applyReload(t.Context(), &Config{
		Jobs: []Job{
			{Name: "starter", Schedule: "0 5 1 * *", Prompt: "p"},
			{Name: "added", Schedule: "0 6 1 * *", Prompt: "p"},
		},
	})

	found := false
	for _, d := range deltas {
		if d.ID != jobsPanelID {
			continue
		}
		payload, jerr := jsonOf(d.Payload)
		if jerr != nil {
			t.Fatalf("delta payload: %v", jerr)
		}
		if strings.Contains(payload, `"name":"added"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("reload pushed no refreshed jobs panel (deltas=%d): %+v", len(deltas), deltas)
	}
	// the snapshot converges on the same state (applyDelta replaces in
	// place; the runtime's store is the snapshot source)
	panel, ok := findPanel(canvas.Snapshot(), jobsPanelID)
	if !ok {
		t.Fatal("jobs panel missing after reload")
	}
	payload, err := jsonOf(panel.Payload)
	if err != nil {
		t.Fatalf("snapshot payload: %v", err)
	}
	if !strings.Contains(payload, `"name":"added"`) {
		t.Fatalf("snapshot stale after reload: %s", payload)
	}
}

// TestReloadWithoutCanvasNeverBlocks: the nil-surface law - a runner
// without a canvas (the runner tests, embeds) reloads exactly as
// before; the push is a no-op, not a panic.
func TestReloadWithoutCanvasNeverBlocks(t *testing.T) {
	home := t.TempDir()
	r := newTestRunner(t, home, Job{Name: "lone", Schedule: "0 5 1 * *", Prompt: "p"})
	if r.panels != nil {
		t.Fatal("panels surface mounted without the plugin")
	}
	r.applyReload(t.Context(), &Config{Jobs: []Job{{Name: "lone", Schedule: "1 5 1 * *", Prompt: "p"}}})
	if jobs := r.jobsSnapshot(); jobs[0].Schedule != "1 5 1 * *" {
		t.Fatalf("reload broken under nil surface: %+v", jobs[0])
	}
}

// TestCapsuleActionsAreReadOnly: the module's declared posture - the
// refresh arm answers the live payload (a read, no mutation), and any
// other action id is refused with core.ErrUnknownAction. An unknown
// module keeps its own sentinel - the two 404s stay distinguishable.
func TestCapsuleActionsAreReadOnly(t *testing.T) {
	ctx, _ := mountedPanelPlugin(t, Job{Name: "j", Schedule: "0 5 1 * *", Prompt: "p"})
	canvas, err := agent.NewCanvas(ctx, nil)
	if err != nil {
		t.Fatalf("canvas: %v", err)
	}
	got, err := canvas.Dispatch(core.ActionCall{Module: "cron", Action: actionRefresh})
	if err != nil {
		t.Fatalf("refresh_jobs: %v", err)
	}
	payload, err := jsonOf(got)
	if err != nil {
		t.Fatalf("refresh payload: %v", err)
	}
	if !strings.Contains(payload, `"name":"j"`) || !strings.Contains(payload, `"total":1`) {
		t.Fatalf("refresh answered the wrong payload: %s", payload)
	}
	if _, err := canvas.Dispatch(core.ActionCall{Module: "cron", Action: "toggle_job", Args: []byte(`{"name":"j"}`)}); !errors.Is(err, core.ErrUnknownAction) {
		t.Fatalf("toggle_job answered %v, want ErrUnknownAction", err)
	}
	// an unknown module has its own sentinel - the two 404s stay
	// distinguishable
	if _, err := canvas.Dispatch(core.ActionCall{Module: "nope", Action: "x"}); !errors.Is(err, core.ErrUnknownModule) {
		t.Fatalf("unknown module answered %v, want ErrUnknownModule", err)
	}
}

// jsonOf marshals a panel payload through its wire serialization (the
// same law removal_test.go's payloadJSON follows).
func jsonOf(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
