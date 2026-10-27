package agent

// profiles_test.go: the continuous-agent home surface (card
// t_72ecca67). Every test runs in a scratch home - nothing here touches
// a real ~/.omega (privacy law: no personal content beyond fixtures).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func scratchHome(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func TestValidAgentName(t *testing.T) {
	for _, ok := range []string{"scout", "a", "deep-research", "x1-2-3"} {
		if err := ValidAgentName(ok); err != nil {
			t.Errorf("ValidAgentName(%q) = %v, want ok", ok, err)
		}
	}
	for _, bad := range []string{"", "Scout", "a_b", "-lead", "trailing-", "with space", "a/b/c", "../../etc"} {
		if err := ValidAgentName(bad); err == nil {
			t.Errorf("ValidAgentName(%q) accepted", bad)
		}
	}
}

func TestScaffoldAgent(t *testing.T) {
	home := scratchHome(t)
	if err := ScaffoldAgent(home, "scout", "fast code scout"); err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	// the settled layout: config/, data/, skills/; no persona file is
	// ever seeded (ruling 1; privacy law).
	for _, want := range []string{"config", "data", "skills"} {
		if fi, err := os.Stat(filepath.Join(home, "agents", "scout", want)); err != nil || !fi.IsDir() {
			t.Errorf("scaffold missing dir %s: %v", want, err)
		}
	}
	for _, absent := range []string{"memory.md", "user.md", "scout.log"} {
		if _, err := os.Stat(filepath.Join(home, "agents", "scout", absent)); !os.IsNotExist(err) {
			t.Errorf("scaffold seeded %s (privacy law: homes start empty of content)", absent)
		}
	}
	data, err := os.ReadFile(AgentConfigPath(home, "scout"))
	if err != nil {
		t.Fatalf("read scaffolded config: %v", err)
	}
	// description is written; every knob is a comment (nothing live to
	// break a boot).
	if !strings.Contains(string(data), `"fast code scout"`) {
		t.Errorf("scaffolded config lost the description")
	}
	if strings.Contains(string(data), "\nprovider:\n") {
		t.Errorf("scaffolded config carries a live provider block; knobs must be commented")
	}
	// second scaffold of the same name is a conflict.
	if err := ScaffoldAgent(home, "scout", "again"); err == nil {
		t.Errorf("re-scaffold of existing agent accepted")
	}
	// default is not scaffoldable: it is the home root by definition.
	if err := ScaffoldAgent(home, "default", ""); err == nil {
		t.Errorf("scaffolding 'default' accepted")
	}
	// hostile names never reach the filesystem.
	if err := ScaffoldAgent(home, "../../etc", ""); err == nil {
		t.Errorf("path-shaped agent name accepted")
	}
	if _, err := os.Stat(filepath.Join(home, "agents", "etc")); !os.IsNotExist(err) {
		t.Errorf("path-shaped scaffold escaped agents/")
	}
}

func TestAgentDirMainIsRoot(t *testing.T) {
	home := scratchHome(t)
	if got := AgentDir(home, DefaultAgentName); got != home {
		t.Errorf("AgentDir(main) = %q, want the home root itself (ruling 1)", got)
	}
	if got := AgentDir(home, "scout"); got != filepath.Join(home, "agents", "scout") {
		t.Errorf("AgentDir(scout) = %q", got)
	}
	if got := AgentConfigPath(home, DefaultAgentName); got != filepath.Join(home, "config", "config.yaml") {
		t.Errorf("AgentConfigPath(main) = %q", got)
	}
	if got := AgentConfigPath(home, "scout"); got != filepath.Join(home, "agents", "scout", "config", "config.yaml") {
		t.Errorf("AgentConfigPath(scout) = %q", got)
	}
}

func TestLoadProfilesMainAlwaysExists(t *testing.T) {
	home := scratchHome(t)
	profiles := LoadProfiles(home)
	if len(profiles) != 1 || profiles[0].Name != DefaultAgentName || profiles[0].Continuous {
		t.Fatalf("bare home returned %+v, want exactly the main profile at the root", profiles)
	}
	// no memory: size 0, never fabricated.
	if profiles[0].MemorySize != 0 {
		t.Errorf("phantom memory size %d for agent-less home", profiles[0].MemorySize)
	}
	if profiles[0].Dir != home {
		t.Errorf("main profile dir = %q, want home root", profiles[0].Dir)
	}
}

func TestLoadProfilesScansAndSkipsMalformed(t *testing.T) {
	home := scratchHome(t)
	for _, name := range []string{"scout", "writer", ".hidden", "_private", "bad_NAME"} {
		os.MkdirAll(filepath.Join(home, "agents", name), 0o755)
	}
	profiles := LoadProfiles(home)
	var names []string
	for _, p := range profiles {
		names = append(names, p.Name)
	}
	if names[0] != DefaultAgentName {
		t.Errorf("main profile missing or not first: %v", names)
	}
	// sorted, malformed dirs skipped, dot/underscore dirs invisible.
	joined := strings.Join(names, ",")
	for _, want := range []string{"scout", "writer"} {
		if !strings.Contains(joined, ","+want) {
			t.Errorf("profile %s missing from %v", want, names)
		}
	}
	for _, absent := range []string{".hidden", "_private", "bad_NAME"} {
		if strings.Contains(joined, absent) {
			t.Errorf("dir %s leaked into the scan", absent)
		}
	}
	for _, p := range profiles {
		if p.Name != DefaultAgentName && !p.Continuous {
			t.Errorf("agent profile %s lost its Continuous flag", p.Name)
		}
	}
}

func TestLoadProfileParsesConfigLayer(t *testing.T) {
	home := scratchHome(t)
	os.MkdirAll(filepath.Join(home, "agents", "scout", "config"), 0o755)
	os.WriteFile(filepath.Join(home, "agents", "scout", "config", "config.yaml"),
		[]byte("description: probe duty\nprovider:\n  type: ollama\n  model_name: qwen3\n"), 0o644)
	p, err := LoadProfile(home, "scout")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	if p.ModelName != "qwen3" || p.ProviderType != "ollama" || p.Description != "probe duty" {
		t.Errorf("profile parse off: %+v", p)
	}
	if _, err := LoadProfile(home, "ghost"); err == nil {
		t.Errorf("unknown agent returned no error")
	}
	// main always resolves (its home is the root).
	d, err := LoadProfile(home, DefaultAgentName)
	if err != nil || d.Name != DefaultAgentName {
		t.Errorf("main profile did not synthesize: %+v %v", d, err)
	}
}

func TestMainProfileMemoryAtRoot(t *testing.T) {
	// main's persona files live at the home root (ruling 3); the
	// pre-migration agents/default copy still counts so the panel
	// never shows a phantom zero between boots.
	home := scratchHome(t)
	os.WriteFile(filepath.Join(home, "memory.md"), []byte("# memory\nnoted"), 0o644)
	if p := LoadProfiles(home)[0]; p.MemorySize == 0 {
		t.Errorf("home-root memory.md not counted for the main profile")
	}
	legacy := scratchHome(t)
	os.MkdirAll(filepath.Join(legacy, "agents", "default"), 0o755)
	os.WriteFile(filepath.Join(legacy, "agents", "default", "memory.md"), []byte("old"), 0o644)
	if p := LoadProfiles(legacy)[0]; p.MemorySize == 0 {
		t.Errorf("pre-migration default memory not counted for the main profile")
	}
}

func TestMigrateAgentHomesMovesPersonaUp(t *testing.T) {
	home := scratchHome(t)
	os.MkdirAll(filepath.Join(home, "agents", "default"), 0o755)
	os.WriteFile(filepath.Join(home, "agents", "default", "memory.md"), []byte("keep me"), 0o644)
	os.WriteFile(filepath.Join(home, "agents", "default", "user.md"), []byte("about endo"), 0o644)
	if err := MigrateAgentHomes(home); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// rename up: the retired dir must be gone and its content live at
	// the root (ruling 3: agents/default dies in the move, main's
	// persona files live at root).
	for _, name := range []string{"memory.md", "user.md"} {
		if _, err := os.Stat(filepath.Join(home, "agents", "default")); !os.IsNotExist(err) {
			t.Errorf("agents/default survived the migration")
		}
		data, err := os.ReadFile(filepath.Join(home, name))
		if err != nil {
			t.Errorf("migrated-up %s unreadable: %v", name, err)
			continue
		}
		want := "keep me"
		if name == "user.md" {
			want = "about endo"
		}
		if string(data) != want {
			t.Errorf("migrated %s content lost: %q", name, string(data))
		}
	}
	// idempotent: a second boot moves nothing and loses nothing.
	if err := MigrateAgentHomes(home); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	if string(mustRead(t, filepath.Join(home, "memory.md"))) != "keep me" {
		t.Errorf("second migration damaged the migrated file")
	}
	// a home that never had agents/default: no error, no dirs created.
	fresh := scratchHome(t)
	if err := MigrateAgentHomes(fresh); err != nil {
		t.Errorf("fresh home migration: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fresh, "agents")); !os.IsNotExist(err) {
		t.Errorf("fresh home migration created agents/")
	}
}

func TestMigrateAgentHomesRootWinsConflict(t *testing.T) {
	home := scratchHome(t)
	os.MkdirAll(filepath.Join(home, "agents", "default"), 0o755)
	os.WriteFile(filepath.Join(home, "memory.md"), []byte("current root copy"), 0o644)
	os.WriteFile(filepath.Join(home, "agents", "default", "memory.md"), []byte("diverged"), 0o644)
	var warned bool
	old := Warnf
	Warnf = func(format string, args ...any) { warned = true }
	MigrateAgentHomes(home)
	Warnf = old
	if !warned {
		t.Errorf("divergent persona file migrated without a loud warning")
	}
	if string(mustRead(t, filepath.Join(home, "memory.md"))) != "current root copy" {
		t.Errorf("home-root persona file did not win the conflict (ruling 3)")
	}
}

func TestMigrateAgentHomesMergesSkills(t *testing.T) {
	home := scratchHome(t)
	os.MkdirAll(filepath.Join(home, "agents", "default", "skills", "own-skill"), 0o755)
	os.WriteFile(filepath.Join(home, "agents", "default", "skills", "own-skill", "SKILL.md"), []byte("own"), 0o644)
	os.MkdirAll(filepath.Join(home, "skills", "legacy-skill"), 0o755)
	// an equally-named skill: the old scan resolved default's own copy
	// first, so the merge must overwrite the legacy one.
	os.MkdirAll(filepath.Join(home, "agents", "default", "skills", "legacy-skill"), 0o755)
	os.WriteFile(filepath.Join(home, "agents", "default", "skills", "legacy-skill", "SKILL.md"), []byte("default's copy"), 0o644)
	os.WriteFile(filepath.Join(home, "skills", "legacy-skill", "SKILL.md"), []byte("legacy copy"), 0o644)
	if err := MigrateAgentHomes(home); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if string(mustRead(t, filepath.Join(home, "skills", "own-skill", "SKILL.md"))) != "own" {
		t.Errorf("default agent's own skill lost in the merge")
	}
	if string(mustRead(t, filepath.Join(home, "skills", "legacy-skill", "SKILL.md"))) != "default's copy" {
		t.Errorf("skill merge did not preserve the old first-match resolution order")
	}
	if _, err := os.Stat(filepath.Join(home, "agents")); !os.IsNotExist(err) {
		t.Errorf("agents/ survived a full migration (ruling 3: the dir dies)")
	}
}

func TestMigrateAgentHomesArchivesResidue(t *testing.T) {
	home := scratchHome(t)
	os.MkdirAll(filepath.Join(home, "agents", "default", "logs"), 0o755)
	os.WriteFile(filepath.Join(home, "agents", "default", "config.yaml"), []byte("hand-edited layer"), 0o644)
	os.WriteFile(filepath.Join(home, "agents", "default", "user.md"), []byte("about endo"), 0o644)
	var warned bool
	old := Warnf
	Warnf = func(format string, args ...any) { warned = true }
	if err := MigrateAgentHomes(home); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	Warnf = old
	if !warned {
		t.Errorf("residue archived without a loud warning")
	}
	// persona still moves up; the unknowable remainder is preserved.
	if string(mustRead(t, filepath.Join(home, "user.md"))) != "about endo" {
		t.Errorf("persona move lost alongside residue archive")
	}
	matches, _ := filepath.Glob(filepath.Join(home, "data", "migrated-agents-default*"))
	found := false
	for _, m := range matches {
		if _, err := os.Stat(filepath.Join(m, "config.yaml")); err == nil {
			found = true
		}
	}
	if !found {
		t.Errorf("agents/default residue not archived intact: %v", matches)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func TestAgentSkillDirs(t *testing.T) {
	home := scratchHome(t)
	os.MkdirAll(filepath.Join(home, "skills"), 0o755)
	os.MkdirAll(filepath.Join(home, "skills", "shared"), 0o755)
	os.MkdirAll(filepath.Join(home, "agents", "scout", "skills"), 0o755)

	got := AgentSkillDirs(home, "scout")
	// scout: shared first, own second, no home/skills.
	if len(got) != 2 || !strings.HasSuffix(got[0], "shared") || !strings.Contains(got[1], "agents") {
		t.Errorf("scout skill dirs = %v", got)
	}
	got = AgentSkillDirs(home, DefaultAgentName)
	// main: shared first, then the home skills root.
	if len(got) != 2 || !strings.HasSuffix(got[0], "shared") || !strings.HasSuffix(got[1], "skills") {
		t.Errorf("main skill dirs = %v", got)
	}
}

func TestScanProfileSkillNames(t *testing.T) {
	home := scratchHome(t)
	os.MkdirAll(filepath.Join(home, "skills", "shared", "alpha"), 0o755)
	os.MkdirAll(filepath.Join(home, "agents", "scout", "skills", "alpha"), 0o755)
	os.MkdirAll(filepath.Join(home, "agents", "scout", "skills", "beta"), 0o755)
	names := scanProfileSkillNames(AgentSkillDirs(home, "scout"))
	// dedupe: alpha appears in shared and own - exactly once.
	if len(names) != 2 || names[0] != "alpha" || names[1] != "beta" {
		t.Errorf("scan = %v, want [alpha beta]", names)
	}
}