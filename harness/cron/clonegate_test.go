package cron

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

// TestCloneGateMirrorsEveryAgentHomeRefusal is the anti-drift pin for the
// clone gate: agent.cloneCronLegality exists ONLY to keep a copy of
// main's crons.yaml from becoming a boot-fatal load error inside an
// agent home, so its refusal set must equal the LOADER's refusal set.
//
// The class has drifted once already (the gate mirrored `agent:` and
// `deliver:` but not `skills:`, and a skills-pinned nightly cloned fine
// and then refused to boot the whole daemon). So this test does not
// hand-list the laws: it ASKS the loader. Each shape is written into an
// agent home and loaded (the loader's verdict is the truth), then the
// same shape is written into main's file and cloned (the gate's
// verdict), and the two must agree - refuse what the loader refuses,
// carry what the loader accepts.
func TestCloneGateMirrorsEveryAgentHomeRefusal(t *testing.T) {
	shapes := []struct{ name, body string }{
		{"plain wake", "jobs:\n  - name: wake\n    schedule: \"0 8 * * *\"\n    prompt: patrol\n"},
		{"deliver target", "jobs:\n  - name: journal\n    schedule: \"0 8 * * *\"\n    prompt: write\n    deliver: new-session\n"},
		{"deliver none (the wake-itself alias)", "jobs:\n  - name: quiet\n    schedule: \"0 8 * * *\"\n    prompt: write\n    deliver: none\n"},
		{"authored agent", "jobs:\n  - name: handoff\n    schedule: \"0 8 * * *\"\n    prompt: write\n    agent: scout\n"},
		{"pinned skills", "jobs:\n  - name: nightly\n    schedule: \"0 3 * * *\"\n    prompt: patrol\n    skills:\n      - omega-dev\n"},
		{"empty skills list", "jobs:\n  - name: blank\n    schedule: \"0 9 * * *\"\n    prompt: write\n    skills: []\n"},
		{"whitespace-only skills", "jobs:\n  - name: blank\n    schedule: \"0 9 * * *\"\n    prompt: write\n    skills:\n      - \"  \"\n"},
		{"unparseable", "jobs: not-a-list\n"},
	}
	for _, s := range shapes {
		t.Run(s.name, func(t *testing.T) {
			// The loader's verdict, executed for real in an agent home.
			loadHome := t.TempDir()
			if err := agent.ScaffoldAgent(loadHome, "probe", ""); err != nil {
				t.Fatal(err)
			}
			writeCronFile(t, filepath.Join(loadHome, "agents", "probe", "config", "crons.yaml"), s.body)
			_, loadErr := LoadAllConfigs(loadHome)

			// The gate's verdict: the same shape in main's file, cloned.
			cloneHome := t.TempDir()
			writeCronFile(t, filepath.Join(cloneHome, "config", "crons.yaml"), s.body)
			cloneErr := agent.CloneAgent(cloneHome, agent.DefaultAgentName, "twin", "")

			switch {
			case loadErr != nil && cloneErr == nil:
				t.Fatalf("the loader refuses this shape in an agent home (%v) but the clone gate carried it - the clone would brick the next boot", loadErr)
			case loadErr == nil && cloneErr != nil:
				t.Fatalf("the loader accepts this shape in an agent home but the clone gate refused it (%v) - the gate refuses more than the law", cloneErr)
			}
			if loadErr != nil && cloneErr != nil {
				// Both refuse. Every per-job law must name the job AND
				// the way out (the clone law's posture: never a silent
				// drop). An unparseable file has no job to name.
				if strings.Contains(cloneErr.Error(), "does not parse") {
					return
				}
				if !strings.Contains(cloneErr.Error(), "clone main: job ") ||
					!strings.Contains(cloneErr.Error(), "not being silently dropped") {
					t.Errorf("gate refusal neither names the offending job nor the escape: %v", cloneErr)
				}
			}
		})
	}
}

func writeCronFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
