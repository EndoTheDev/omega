package skills

// skills_shared_test.go: the shared-skills resolution (card
// t_7e2b7002) - root skills/shared first (every agent sees it), then
// the agent's own dir, name collisions resolving to the agent's copy
// (first-wins after the merge order: shared is scanned first, so the
// agent's own dir must WIN - see the scan order note in LoadSkills).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

func writeSkill(t *testing.T, dir, name, description string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + description + "\n---\nbody of " + name
	if err := os.WriteFile(filepath.Join(dir, name, name+".md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSharedDirLoadsFirstThenOwn(t *testing.T) {
	home := t.TempDir()
	shared := filepath.Join(home, "skills", "shared")
	own := filepath.Join(home, "agents", "scout", "skills")
	writeSkill(t, shared, "deploy", "the shared deploy skill")
	writeSkill(t, own, "probe", "scout's own probe skill")

	sp := &SkillsProvider{Dir: own, SharedDir: shared}
	skills, err := sp.LoadSkills("")
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("loaded = %v, want deploy+probe", names(skills))
	}
	// scan order: shared first - the prompt renders shared entries
	// before the agent's own.
	if skills[0].Name != "deploy" || skills[1].Name != "probe" {
		t.Errorf("order = %v, want [deploy probe]", names(skills))
	}
	// the surviving Dir field says where the skill actually lives.
	if !strings.Contains(skills[0].Dir, "shared") {
		t.Errorf("shared skill dir = %q", skills[0].Dir)
	}
	if !strings.Contains(skills[1].Dir, "scout") {
		t.Errorf("own skill dir = %q", skills[1].Dir)
	}
}

func TestSharedNameCollisionSharedWins(t *testing.T) {
	// The card's resolution order is literal: shared first. A name
	// present in both resolves to the SHARED copy - an agent that
	// needs a variant gives that copy its own name rather than
	// shadowing (this keeps "every agent sees shared" always true).
	home := t.TempDir()
	shared := filepath.Join(home, "skills", "shared")
	own := filepath.Join(home, "agents", "default", "skills")
	writeSkill(t, shared, "deploy", "shared copy")
	writeSkill(t, own, "deploy", "agent's copy")

	sp := &SkillsProvider{Dir: own, SharedDir: shared}
	skills, err := sp.LoadSkills("")
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("collision produced %d skills, want exactly 1", len(skills))
	}
	if !strings.Contains(skills[0].Description, "shared copy") {
		t.Errorf("collision winner: %q, want the shared copy (shared is scanned first)", skills[0].Description)
	}
}

func TestMissingDirectoriesAreEmptyNotFatal(t *testing.T) {
	sp := &SkillsProvider{Dir: filepath.Join(t.TempDir(), "nope"),
		SharedDir: filepath.Join(t.TempDir(), "nope-either")}
	skills, err := sp.LoadSkills("")
	if err != nil || len(skills) != 0 {
		t.Errorf("missing dirs = %v, %v", len(skills), err)
	}
}

func TestPerAgentSkillOnlyToItsOwner(t *testing.T) {
	// no SharedDir configured: the other agent's dir is invisible.
	home := t.TempDir()
	own := filepath.Join(home, "agents", "scout", "skills")
	writeSkill(t, own, "secret-scout-thing", "scout only")

	spOther := &SkillsProvider{Dir: filepath.Join(home, "agents", "default", "skills")}
	skills, err := spOther.LoadSkills("")
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 0 {
		t.Errorf("scout's skill leaked to the other agent: %v", names(skills))
	}
}

func names(skills []agent.Skill) []string {
	out := make([]string, len(skills))
	for i, s := range skills {
		out[i] = s.Name
	}
	return out
}