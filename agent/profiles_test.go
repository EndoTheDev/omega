package agent

// profiles_test.go: the multi-agent profile surface (card t_7e2b7002).
// Every test runs in a scratch home - nothing here touches a real
// ~/.omega (privacy law: no personal content beyond the fixtures).

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
	// the layout: config.yaml + empty skills/ + logs/; no memory or
	// user file is ever seeded.
	for _, want := range []string{"config.yaml", "skills", "logs"} {
		if _, err := os.Stat(filepath.Join(home, "agents", "scout", want)); err != nil {
			t.Errorf("scaffold missing %s: %v", want, err)
		}
	}
	for _, absent := range []string{"memory.md", "user.md"} {
		if _, err := os.Stat(filepath.Join(home, "agents", "scout", absent)); !os.IsNotExist(err) {
			t.Errorf("scaffold seeded %s (privacy law: dirs start empty of content)", absent)
		}
	}
	data, err := os.ReadFile(filepath.Join(home, "agents", "scout", "config.yaml"))
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
	// default is not scaffoldable.
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

func TestLoadProfilesDefaultAlwaysExists(t *testing.T) {
	home := scratchHome(t)
	profiles := LoadProfiles(home)
	if len(profiles) != 1 || profiles[0].Name != DefaultAgentName {
		t.Fatalf("bare home returned %v, want exactly the default profile", profiles)
	}
	// no memory: size 0, never fabricated.
	if profiles[0].MemorySize != 0 {
		t.Errorf("phantom memory size %d for agent-less home", profiles[0].MemorySize)
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
		t.Errorf("default profile missing or not first: %v", names)
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
}

func TestLoadProfileParsesConfig(t *testing.T) {
	home := scratchHome(t)
	os.MkdirAll(filepath.Join(home, "agents", "scout"), 0o755)
	os.WriteFile(filepath.Join(home, "agents", "scout", "config.yaml"),
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
	// default always resolves even with no dir (pre-migration boot).
	d, err := LoadProfile(home, DefaultAgentName)
	if err != nil || d.Name != DefaultAgentName {
		t.Errorf("default profile did not synthesize: %+v %v", d, err)
	}
}

func TestDefaultProfileMemoryFallback(t *testing.T) {
	// A legacy home that has not migrated yet still reports its
	// memory - the default profile counts whichever file exists.
	home := scratchHome(t)
	os.WriteFile(filepath.Join(home, "memory.md"), []byte("# memory\nnoted"), 0o644)
	p := LoadProfiles(home)[0]
	if p.MemorySize == 0 {
		t.Errorf("legacy home memory.md not counted for the default profile")
	}
}

func TestMigrateDefaultAgent(t *testing.T) {
	home := scratchHome(t)
	os.WriteFile(filepath.Join(home, "memory.md"), []byte("keep me"), 0o644)
	os.WriteFile(filepath.Join(home, "user.md"), []byte("about endo"), 0o644)
	if err := MigrateDefaultAgent(home); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// rename, not copy: the legacy files must be gone.
	for _, name := range []string{"memory.md", "user.md"} {
		if _, err := os.Stat(filepath.Join(home, name)); !os.IsNotExist(err) {
			t.Errorf("%s still at home root (migration must move, not copy)", name)
		}
		if data, err := os.ReadFile(filepath.Join(home, "agents", "default", name)); err != nil {
			t.Errorf("migrated %s unreadable: %v", name, err)
		} else if name == "user.md" && string(data) != "about endo" {
			t.Errorf("migrated content lost: %q", string(data))
		}
	}
	// idempotent: a second boot moves nothing and loses nothing.
	if err := MigrateDefaultAgent(home); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "agents", "default", "memory.md")); err != nil {
		t.Errorf("second migration deleted the migrated file")
	}
	// a home that never wrote memory: no error, dir created anyway.
	fresh := scratchHome(t)
	if err := MigrateDefaultAgent(fresh); err != nil {
		t.Errorf("fresh home migration: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(fresh, "agents", "default")); err != nil || !fi.IsDir() {
		t.Errorf("migration did not create agents/default")
	}
}

func TestAgentSkillDirs(t *testing.T) {
	home := scratchHome(t)
	os.MkdirAll(filepath.Join(home, "skills"), 0o755)
	os.MkdirAll(filepath.Join(home, "skills", "shared"), 0o755)
	os.MkdirAll(filepath.Join(home, "agents", "scout", "skills"), 0o755)

	got := AgentSkillDirs(home, "scout")
	// scout: shared first, own second, NO legacy home/skills.
	if len(got) != 2 || !strings.HasSuffix(got[0], "shared") || !strings.Contains(got[1], "agents") {
		t.Errorf("scout skill dirs = %v", got)
	}
	got = AgentSkillDirs(home, DefaultAgentName)
	// default: shared, own (if present), legacy last.
	if len(got) != 2 || !strings.HasSuffix(got[0], "shared") || !strings.HasSuffix(got[1], "skills") {
		t.Errorf("default skill dirs = %v", got)
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
