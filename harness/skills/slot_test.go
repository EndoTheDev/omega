package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
)

// The curator's model slot (card t_fc0b2f2d): an "instance:model"
// value routes the LLM consult through the named provider instance; a
// bare value (or an unconfigured prefix) stays on the default
// instance. The observable is the proposal outcome - only a provider
// carrying the merge script produces the merge.

const slotMergeScript = `{"merges":[{"source":"narrow","into":"umbrella","reason":"same job, one umbrella"}],"distills":[]}`

// slotFixture builds a skills dir with the merge pair and the default
// provider with one named instance. The instance fake carries the
// merge script; the base fake never proposes one.
func slotFixture(t *testing.T) (*SkillsProvider, agent.Provider) {
	t.Helper()
	dir := t.TempDir()
	sp := NewSkillsProvider(dir)
	writeAgentSkill(t, dir, "umbrella", "# umbrella\n\nexisting rules")
	writeAgentSkill(t, dir, "narrow", "narrow insight one")

	base := agent.NewFakeProviderScripts("base-model",
		[]core.ProviderStreamEvent{ // the proposal consult: no proposals
			core.ResponseChunk{Type: "response", Content: `{"merges":[],"distills":[]}`},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
		chat(""),
	)
	instScript := agent.NewFakeProviderScripts("inst-model",
		[]core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response", Content: slotMergeScript},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
		[]core.ProviderStreamEvent{
			core.ResponseChunk{Type: "response", Content: `{"title":"Narrow rules","body":"- rule one"}`},
			core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
		},
	)
	base.SetInstances(map[string]agent.Provider{"big": instScript})
	return sp, base
}

// TestCuratorSlotRoutesInstance: the slot "big:x-model" serves the
// proposal consult from the instance - the merge lands.
func TestCuratorSlotRoutesInstance(t *testing.T) {
	sp, prov := slotFixture(t)
	rep, repErr := sp.RunCurator(context.Background(), prov, "big:x-model")
	if repErr != nil {
		t.Fatal(repErr)
	}
	if rep.Merges != 1 {
		t.Fatalf("merges %d, want 1 (skipped: %v)", rep.Merges, rep.Skipped)
	}
	if _, err := os.Stat(filepath.Join(sp.Dir, ".archive", "narrow", "SKILL.md")); err != nil {
		t.Fatalf("narrow not archived by the instance-routed merge: %v", err)
	}
}

// A bare slot (and an unconfigured instance prefix over it) consults
// the default instance - no merge proposed (the base fake proposes
// nothing), the narrow skill stays.
func TestCuratorSlotBareStays(t *testing.T) {
	sp, prov := slotFixture(t)
	rep, repErr := sp.RunCurator(context.Background(), prov, "gemma3:27b")
	if repErr != nil {
		t.Fatal(repErr)
	}
	if rep.Merges != 0 {
		t.Fatalf("merges %d, want 0 (the bare slot must consult the base)", rep.Merges)
	}
	if _, err := os.Stat(filepath.Join(sp.Dir, "narrow", "SKILL.md")); err != nil {
		t.Fatalf("narrow survived: %v", err)
	}
	if strings.Contains(strings.Join(rep.Skipped, ";"), "big") {
		t.Fatalf("unexpected skip text: %v", rep.Skipped)
	}
}
