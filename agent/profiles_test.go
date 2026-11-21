package agent

// profiles_test.go: the continuous-agent home surface.

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
	for _, want := range []string{"config", "data", "skills"} {
		if fi, err := os.Stat(filepath.Join(home, "agents", "scout", want)); err != nil || !fi.IsDir() {
			t.Errorf("scaffold missing dir %s: %v", want, err)
		}
	}
	for _, absent := range []string{"memory.md", "user.md", SoulFileName, "scout.log"} {
		if _, err := os.Stat(filepath.Join(home, "agents", "scout", absent)); !os.IsNotExist(err) {
			t.Errorf("scaffold seeded %s (privacy law: homes start empty of content)", absent)
		}
	}
	data, err := os.ReadFile(AgentConfigPath(home, "scout"))
	if err != nil {
		t.Fatalf("read scaffolded config: %v", err)
	}
	if !strings.Contains(string(data), `"fast code scout"`) {
		t.Errorf("scaffolded config lost the description")
	}
	if strings.Contains(string(data), "\nprovider:\n") {
		t.Errorf("scaffolded config carries a live provider block; knobs must be commented")
	}
	if err := ScaffoldAgent(home, "scout", "again"); err == nil {
		t.Errorf("re-scaffold of existing agent accepted")
	}
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
		[]byte("description: probe duty\nprovider:\n  instance: work\n  model_name: qwen3\ntoggles: [kanban]\nskills:\n  allow: [omega-dev]\ninherit:\n  soul: true\n"), 0o644)
	p, err := LoadProfile(home, "scout")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	if p.ModelName != "qwen3" || p.ProviderInstance != "work" || p.Description != "probe duty" {
		t.Errorf("profile parse off: %+v", p)
	}
	if len(p.Toggles) != 1 || p.Toggles[0] != "kanban" {
		t.Errorf("profile toggles = %v", p.Toggles)
	}
	if len(p.SkillsAllow) != 1 || p.SkillsAllow[0] != "omega-dev" {
		t.Errorf("profile allowlist = %v", p.SkillsAllow)
	}
	if len(p.Inherits) != 1 || p.Inherits[0] != "soul" {
		t.Errorf("profile inherits = %v", p.Inherits)
	}
	if _, err := LoadProfile(home, "ghost"); err == nil {
		t.Errorf("unknown agent returned no error")
	}
	d, err := LoadProfile(home, DefaultAgentName)
	if err != nil || d.Name != DefaultAgentName {
		t.Errorf("main profile did not synthesize: %+v %v", d, err)
	}
}

func TestMainProfileMemoryAtRoot(t *testing.T) {
	// main's persona files live at the home root; the pre-migration agents/default copy
	// still counts so the panel never shows a phantom zero between boots.
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
	// rename up: the retired dir must be gone and its content live at the root.
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
	if len(got) != 2 || !strings.HasSuffix(got[0], "shared") || !strings.Contains(got[1], "agents") {
		t.Errorf("scout skill dirs = %v", got)
	}
	got = AgentSkillDirs(home, DefaultAgentName)
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

// TestCloneAgentMaterializesEverything pins the CLONE law.
func TestCloneAgentMaterializesEverything(t *testing.T) {
	home := scratchHome(t)
	installStubSnapshot(t)
	if err := ScaffoldAgent(home, "scout", "probe duty"); err != nil {
		t.Fatal(err)
	}
	seeded := "description: probe duty\nprovider:\n  instance: work\n  model_name: qwen3\ntoggles: [kanban]\nskills:\n  allow: [alpha]\n"
	if err := WriteFileAtomic(AgentConfigPath(home, "scout"), []byte(seeded), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, skill := range []string{"alpha"} {
		os.MkdirAll(filepath.Join(home, "agents", "scout", "skills", skill), 0o755)
		os.WriteFile(filepath.Join(home, "agents", "scout", "skills", skill, "SKILL.md"), []byte("# "+skill), 0o644)
	}
	os.WriteFile(filepath.Join(home, "agents", "scout", "memory.md"), []byte("scout memory"), 0o644)
	os.WriteFile(filepath.Join(home, "agents", "scout", "user.md"), []byte("scout user"), 0o644)
	os.WriteFile(filepath.Join(home, "agents", "scout", SoulFileName), []byte("scout soul"), 0o644)
	os.MkdirAll(filepath.Join(home, "agents", "scout", "data"), 0o755)
	os.WriteFile(filepath.Join(home, "agents", "scout", "data", "omega.db"), []byte("source-db"), 0o644)

	if err := CloneAgent(home, "scout", "probe", ""); err != nil {
		t.Fatalf("clone: %v", err)
	}
	for _, want := range []string{
		filepath.Join("config", "config.yaml"),
		filepath.Join("skills", "alpha", "SKILL.md"),
		"memory.md", "user.md", SoulFileName,
		filepath.Join("data", "omega.db"),
	} {
		if _, err := os.Stat(filepath.Join(home, "agents", "probe", want)); err != nil {
			t.Errorf("clone missing %s: %v", want, err)
		}
	}
	p, err := LoadProfile(home, "probe")
	if err != nil {
		t.Fatal(err)
	}
	if p.ProviderInstance != "work" || p.ModelName != "qwen3" || len(p.Toggles) != 1 {
		t.Errorf("clone definition did not travel: %+v", p)
	}
	// Divergence, not a link: editing the clone never touches the source.
	if err := UpdateAgentConfigLayer(home, "probe", AgentConfigUpdates{Description: strPtr("now a probe")}); err != nil {
		t.Fatal(err)
	}
	if got := loadProfile(home, "scout").Description; got != "probe duty" {
		t.Errorf("clone edit leaked into the source (%q)", got)
	}
	if err := os.RemoveAll(filepath.Join(home, "agents", "scout")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProfile(home, "probe"); err != nil {
		t.Errorf("the clone did not survive deleting its source: %v", err)
	}
}

// TestCloneAgentFromMainIsSecretFree pins the exception to "copy everything".
func TestCloneAgentFromMainIsSecretFree(t *testing.T) {
	home := scratchHome(t)
	if err := WriteFileAtomic(AgentConfigPath(home, DefaultAgentName),
		[]byte("provider:\n  type: openai\n  api_key: sk-live\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(home, "skills", "keeper"), 0o755)
	os.WriteFile(filepath.Join(home, "skills", "keeper", "SKILL.md"), []byte("# keeper"), 0o644)
	os.WriteFile(filepath.Join(home, "memory.md"), []byte("main memory"), 0o644)
	os.WriteFile(filepath.Join(home, SoulFileName), []byte("main soul"), 0o644)

	if err := CloneAgent(home, DefaultAgentName, "twin", "a twin"); err != nil {
		t.Fatalf("clone main: %v", err)
	}
	body, err := os.ReadFile(AgentConfigPath(home, "twin"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "sk-live") || hasYAMLKey(string(body), "api_key") {
		t.Errorf("a clone of main carried the provider secret:\n%s", body)
	}
	if _, err := os.Stat(filepath.Join(home, "agents", "twin", "skills", "keeper", "SKILL.md")); err != nil {
		t.Errorf("main's skills did not travel with the clone: %v", err)
	}
	if string(mustRead(t, filepath.Join(home, "agents", "twin", "memory.md"))) != "main memory" {
		t.Errorf("main's memory.md did not travel with the clone")
	}
	if _, err := os.Stat(filepath.Join(home, "agents", "twin", SoulFileName)); err != nil {
		t.Errorf("main's soul.md did not travel with the clone: %v", err)
	}
}

// TestMigrateAgentHomesMovesSoulUp pins soul.md as a persona file.
func TestMigrateAgentHomesMovesSoulUp(t *testing.T) {
	home := scratchHome(t)
	os.MkdirAll(filepath.Join(home, "agents", "default"), 0o755)
	os.WriteFile(filepath.Join(home, "agents", "default", SoulFileName), []byte("old soul"), 0o644)
	if err := MigrateAgentHomes(home); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if string(mustRead(t, filepath.Join(home, SoulFileName))) != "old soul" {
		t.Errorf("soul.md did not migrate up")
	}
}

// installStubSnapshot wires a fake StoreSnapshot for a test and returns the (src,dst).
// The real copier lives in harness/store (this package never touches SQLite); the stub
// proves the clone CALLS it once per store and never falls back to a byte copy.
func installStubSnapshot(t *testing.T) *[][2]string {
	t.Helper()
	prev := StoreSnapshot
	var calls [][2]string
	StoreSnapshot = func(src, dst string) error {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		calls = append(calls, [2]string{src, dst})
		return WriteFileAtomic(dst, append([]byte("snapshot:"), data...), 0o644)
	}
	t.Cleanup(func() { StoreSnapshot = prev })
	return &calls
}

func TestCloneAgentCopiesTheWholeHome(t *testing.T) {
	home := scratchHome(t)
	calls := installStubSnapshot(t)
	if err := ScaffoldAgent(home, "scout", "probe duty"); err != nil {
		t.Fatal(err)
	}
	src := AgentDir(home, "scout")
	if err := WriteFileAtomic(filepath.Join(src, "config", "crons.yaml"), []byte("jobs: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// the store set, plus WAL sidecars that must NOT be copied
	for _, name := range []string{"omega.db", "notifications.db", "kanban.db", "ledger.db"} {
		if err := os.WriteFile(filepath.Join(src, "data", name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, sidecar := range []string{"omega.db-wal", "omega.db-shm"} {
		if err := os.WriteFile(filepath.Join(src, "data", sidecar), []byte("stale"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, "data", "process.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := CloneAgent(home, "scout", "probe", ""); err != nil {
		t.Fatalf("clone: %v", err)
	}
	dst := AgentDir(home, "probe")
	if _, err := os.Stat(filepath.Join(dst, "config", "crons.yaml")); err != nil {
		t.Errorf("the clone lost the source's hand-owned crons.yaml: %v", err)
	}
	if len(*calls) != 4 {
		t.Errorf("snapshot calls = %d, want 4 (one per store)", len(*calls))
	}
	for _, name := range []string{"omega.db", "notifications.db", "kanban.db", "ledger.db"} {
		body, err := os.ReadFile(filepath.Join(dst, "data", name))
		if err != nil {
			t.Errorf("clone lost store %s: %v", name, err)
			continue
		}
		// copied through the SQLite snapshot, never byte-read
		if !strings.HasPrefix(string(body), "snapshot:") {
			t.Errorf("store %s was byte-copied, not snapshotted: %q", name, body)
		}
	}
	for _, sidecar := range []string{"omega.db-wal", "omega.db-shm"} {
		if _, err := os.Stat(filepath.Join(dst, "data", sidecar)); err == nil {
			t.Errorf("clone copied the meaningless %s sidecar", sidecar)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "data", "process.json")); err == nil {
		t.Errorf("clone copied runtime scratch (process.json) - live PIDs mean nothing in another home")
	}
}

// TestCloneAgentFromMainCarriesItsSchedules pins the same completeness for a clone.
func TestCloneAgentFromMainCarriesItsSchedules(t *testing.T) {
	home := scratchHome(t)
	installStubSnapshot(t)
	if err := WriteFileAtomic(filepath.Join(home, "config", "config.yaml"),
		[]byte("provider:\n  type: openai\n  api_key: sk-live\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(filepath.Join(home, "config", "crons.yaml"),
		[]byte("jobs: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "data", "omega.db"), []byte("main-db"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CloneAgent(home, DefaultAgentName, "twin", "a twin"); err != nil {
		t.Fatalf("clone main: %v", err)
	}
	dst := AgentDir(home, "twin")
	if _, err := os.Stat(filepath.Join(dst, "config", "crons.yaml")); err != nil {
		t.Errorf("a clone of main lost main's schedules: %v", err)
	}
	body, err := os.ReadFile(AgentConfigPath(home, "twin"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "sk-live") || hasYAMLKey(string(body), "api_key") {
		t.Errorf("a clone of main carried the provider secret:\n%s", body)
	}
	if _, err := os.Stat(filepath.Join(dst, "data", "omega.db")); err != nil {
		t.Errorf("a clone of main lost main's session store: %v", err)
	}
}

// TestCloneAgentFromMainRefusesMainOnlySchedules pins the guard on a clone of main:
// main's crons.yaml may carry main-home-only fields that the cron loader refuses INSIDE
// an agent home (fatally, at boot), so the clone refuses instead of copying a file that
// would break the next boot or silently dropping the operator's schedules.
func TestCloneAgentFromMainRefusesMainOnlySchedules(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{
			name: "deliver",
			body: "jobs:\n  - name: journal\n    schedule: \"0 8 * * *\"\n    prompt: write\n    deliver: new-session\n",
			want: "delivers to",
		},
		{
			name: "authored agent",
			body: "jobs:\n  - name: handoff\n    schedule: \"0 8 * * *\"\n    prompt: write\n    agent: scout\n",
			want: "authors agent",
		},
		{
			name: "skills pinned",
			body: "jobs:\n  - name: nightly\n    schedule: \"0 3 * * *\"\n    prompt: patrol\n    skills:\n      - omega-dev\n",
			want: "pins skills",
		},
		{
			name: "unparseable",
			body: "jobs: not-a-list\n",
			want: "does not parse",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			home := scratchHome(t)
			installStubSnapshot(t)
			if err := WriteFileAtomic(filepath.Join(home, "config", "crons.yaml"), []byte(c.body), 0o644); err != nil {
				t.Fatal(err)
			}
			err := CloneAgent(home, DefaultAgentName, "twin", "")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("clone of main with %s crons = %v, want a refusal naming %q", c.name, err, c.want)
			}
			if _, statErr := os.Stat(AgentDir(home, "twin")); !os.IsNotExist(statErr) {
				t.Errorf("the refused clone left a registered phantom agent (%v)", statErr)
			}
		})
	}
	home := scratchHome(t)
	installStubSnapshot(t)
	legal := "jobs:\n  - name: journal\n    schedule: \"0 8 * * *\"\n    prompt: write\n"
	if err := WriteFileAtomic(filepath.Join(home, "config", "crons.yaml"), []byte(legal), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CloneAgent(home, DefaultAgentName, "twin", ""); err != nil {
		t.Fatalf("clone of main with an agent-legal schedule file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(AgentDir(home, "twin"), "config", "crons.yaml")); err != nil {
		t.Errorf("an agent-legal schedule file did not travel: %v", err)
	}
	// The loader normalizes skills before judging them, so a list of
	// whitespace entries is NOT a pinned list and the file must still
	// travel - the gate refuses what the loader refuses, not more.
	home2 := scratchHome(t)
	installStubSnapshot(t)
	blank := "jobs:\n  - name: blank\n    schedule: \"0 9 * * *\"\n    prompt: write\n    skills:\n      - \"  \"\n"
	if err := WriteFileAtomic(filepath.Join(home2, "config", "crons.yaml"), []byte(blank), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CloneAgent(home2, DefaultAgentName, "twin", ""); err != nil {
		t.Fatalf("clone of main with a whitespace-only skills list (normalizes to empty): %v", err)
	}
}

// TestCloneAgentFailureLeavesNoPhantom pins it: folder presence
// IS registration, so a clone that dies mid-copy must not leave a
// half-built agent home behind.
func TestCloneAgentFailureLeavesNoPhantom(t *testing.T) {
	home := scratchHome(t)
	installStubSnapshot(t)
	if err := ScaffoldAgent(home, "scout", "probe duty"); err != nil {
		t.Fatal(err)
	}
	src := AgentDir(home, "scout")
	if err := os.MkdirAll(filepath.Join(src, "skills", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "elsewhere.md")
	if err := os.WriteFile(target, []byte("# out"), 0o644); err != nil {
		t.Fatal(err)
	}
	// copyTree refuses a symlink; that refusal is the mid-clone failure.
	if err := os.Symlink(target, filepath.Join(src, "skills", "alpha", "escape.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	err := CloneAgent(home, "scout", "probe", "")
	if err == nil {
		t.Fatalf("clone of a home holding a symlink must fail loud")
	}
	if _, statErr := os.Stat(AgentDir(home, "probe")); !os.IsNotExist(statErr) {
		t.Errorf("a failed clone left a registered phantom agent home (%v)", statErr)
	}
	for _, p := range LoadProfiles(home) {
		if p.Name == "probe" {
			t.Errorf("the phantom clone is listed as an agent")
		}
	}
}

// TestCloneAgentWithoutSnapshotIsLoud pins the no-fallback rule: with no
// SQLite copier wired, a clone that has a store to copy fails and leaves
// nothing behind rather than byte-copying a live WAL database.
func TestCloneAgentWithoutSnapshotIsLoud(t *testing.T) {
	home := scratchHome(t)
	prev := StoreSnapshot
	StoreSnapshot = nil
	t.Cleanup(func() { StoreSnapshot = prev })
	if err := ScaffoldAgent(home, "scout", "probe duty"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(AgentDir(home, "scout"), "data", "omega.db"), []byte("source-db"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := CloneAgent(home, "scout", "probe", "")
	if err == nil || !strings.Contains(err.Error(), "no store snapshot wired") {
		t.Fatalf("clone without a snapshot hook = %v, want the loud no-fallback error", err)
	}
	if _, statErr := os.Stat(AgentDir(home, "probe")); !os.IsNotExist(statErr) {
		t.Errorf("a failed clone left a registered phantom agent home (%v)", statErr)
	}
}

func TestMigrateAgentHomesRefusesSymlinkedSkill(t *testing.T) {
	home := scratchHome(t)
	legacy := filepath.Join(home, "agents", "default")
	if err := os.MkdirAll(filepath.Join(legacy, "skills", "linked"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(home, "outside.md")
	if err := os.WriteFile(outside, []byte("# outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(legacy, "skills", "linked", "escape.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	err := MigrateAgentHomes(home)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("migration of a symlinked skill = %v, want the loud symlink refusal", err)
	}
	// The refusal is loud and leaves the migration unfinished: the legacy tree is still
	// there for the operator to fix, never half merged into the home.
	if _, statErr := os.Stat(legacy); statErr != nil {
		t.Errorf("the refused migration destroyed the legacy tree: %v", statErr)
	}
}
