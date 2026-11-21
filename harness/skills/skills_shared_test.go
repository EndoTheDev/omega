package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, dir, name, description string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + description + "\n---\nbody of " + name
	if err := os.WriteFile(filepath.Join(dir, name, "SKILL.md"), []byte(body), 0o644); err != nil {
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
	if skills[0].Name != "deploy" || skills[1].Name != "probe" {
		t.Errorf("order = %v, want [deploy probe]", names(skills))
	}
	if !strings.Contains(skills[0].Dir, "shared") {
		t.Errorf("shared skill dir = %q", skills[0].Dir)
	}
	if !strings.Contains(skills[1].Dir, "scout") {
		t.Errorf("own skill dir = %q", skills[1].Dir)
	}
}

func TestSharedNameCollisionSharedWins(t *testing.T) {
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

func names(skills []Skill) []string {
	out := make([]string, len(skills))
	for i, s := range skills {
		out[i] = s.Name
	}
	return out
}

func TestAllowlistFiltersTheRegistry(t *testing.T) {
	home := t.TempDir()
	shared := filepath.Join(home, "skills", "shared")
	own := filepath.Join(home, "agents", "scout", "skills")
	writeSkill(t, shared, "deploy", "the shared deploy skill")
	writeSkill(t, own, "probe", "scout's own probe skill")
	writeSkill(t, own, "ghost", "not allowlisted")

	sp := &SkillsProvider{Dir: own, SharedDir: shared}
	all, err := sp.LoadSkills("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("unset allowlist loaded %v, want all three", names(all))
	}

	sp = &SkillsProvider{Dir: own, SharedDir: shared, Allow: []string{"deploy"}, AllowSet: true}
	got, err := sp.LoadSkills("")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(names(got), ",") != "deploy" {
		t.Fatalf("allowlist loaded %v, want only deploy", names(got))
	}
	if _, err := sp.ReadSkill("ghost"); err == nil {
		t.Errorf("an allowlisted-out skill was still readable")
	}

	sp = &SkillsProvider{Dir: own, SharedDir: shared, AllowSet: true}
	none, err := sp.LoadSkills("")
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("empty allowlist loaded %v, want none (empty = NO skills)", names(none))
	}
}
