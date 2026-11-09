package skills

// cli_test.go: the `omega skills` subcommand's self-check (card
// t_05d06973). Every verb rides the provider faces the webui tab
// calls, so the assertions read LANDED FILE TRUTH, not rendered
// snapshots: a write lands the composed SKILL.md, an update keeps the
// operator's frontmatter keys, a delete backs the folder up and
// removes it.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCLI drives one argv through the dispatcher with captured stdout
// and a scripted stdin.
func runCLI(t *testing.T, sp *SkillsProvider, args []string, stdin string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runSkillsCLI(sp, args, &out, strings.NewReader(stdin))
	return out.String(), err
}

func TestSkillsCLIBareInvocationIsList(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "alpha", "the alpha skill")
	writeSkill(t, dir, "beta", "the beta skill")
	sp := NewSkillsProvider(dir)

	bare, err := runCLI(t, sp, nil, "")
	if err != nil {
		t.Fatalf("bare invocation: %v", err)
	}
	listed, err := runCLI(t, sp, []string{"list"}, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if bare != listed {
		t.Errorf("bare invocation != list:\n%q\n%q", bare, listed)
	}
	for _, want := range []string{"NAME", "DESCRIPTION", "alpha", "the alpha skill", "beta", "the beta skill"} {
		if !strings.Contains(bare, want) {
			t.Errorf("listing misses %q: %q", want, bare)
		}
	}
}

func TestSkillsCLIReadPrintsTheBodyOnly(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "alpha", "the alpha skill")
	out, err := runCLI(t, NewSkillsProvider(dir), []string{"read", "alpha"}, "")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.TrimSpace(out) != "body of alpha" {
		t.Errorf("read output = %q, want the body under the fence", out)
	}
	if strings.Contains(out, "description:") || strings.Contains(out, "---") {
		t.Errorf("read must not print the frontmatter: %q", out)
	}
}

func TestSkillsCLIReadUnknownNamesTheSet(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "alpha", "the alpha skill")
	_, err := runCLI(t, NewSkillsProvider(dir), []string{"read", "ghost"}, "")
	if err == nil {
		t.Fatal("unknown skill must error")
	}
	if !strings.Contains(err.Error(), `"ghost" not found`) || !strings.Contains(err.Error(), "alpha") {
		t.Errorf("not-found error must name the miss and the set: %v", err)
	}
}

func TestSkillsCLIWriteCreatesFromFile(t *testing.T) {
	dir := t.TempDir()
	bodyFile := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(bodyFile, []byte("# probe\n\nsay nothing.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, NewSkillsProvider(dir),
		[]string{"write", "probe", "--description", "a probe skill", "--file", bodyFile}, "")
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if !strings.Contains(out, `saved skill "probe" (created): 1 skill(s)`) {
		t.Errorf("confirmation = %q", out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "probe", "SKILL.md"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	content := string(raw)
	if !strings.HasPrefix(content, "---\ndescription: a probe skill\n---\n") {
		t.Errorf("composed frontmatter missing: %q", content)
	}
	if !strings.HasSuffix(content, "say nothing.\n") || strings.Contains(content, "author") {
		t.Errorf("body not seated / author stamped: %q", content)
	}
}

func TestSkillsCLIWriteUpdateKeepsDescriptionAndFrontmatter(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "deploy")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	before := "---\ndescription: old one-liner\nauthor: agent\nversion: 4.0.0\n---\n# deploy\n\nold body\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, NewSkillsProvider(dir), []string{"write", "deploy"}, "# deploy\n\nnew body\n")
	if err != nil {
		t.Fatalf("write update: %v", err)
	}
	if !strings.Contains(out, `(updated)`) {
		t.Errorf("an existing skill is an update: %q", out)
	}
	raw, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	for _, keep := range []string{"description: old one-liner", "author: agent", "version: 4.0.0", "new body"} {
		if !strings.Contains(content, keep) {
			t.Errorf("lost %q in: %q", keep, content)
		}
	}
	if strings.Contains(content, "old body") {
		t.Errorf("body not rewritten: %q", content)
	}
}

func TestSkillsCLIWriteRequiresDescriptionOnCreate(t *testing.T) {
	dir := t.TempDir()
	_, err := runCLI(t, NewSkillsProvider(dir), []string{"write", "probe", "--content", "# probe\n"}, "")
	if err == nil || !strings.Contains(err.Error(), "--description is required") {
		t.Fatalf("error = %v, want the --description law", err)
	}
	if _, serr := os.Stat(filepath.Join(dir, "probe")); !os.IsNotExist(serr) {
		t.Error("a refused create must not land a folder")
	}
}

func TestSkillsCLIWriteRefusesTwoBodySources(t *testing.T) {
	_, err := runCLI(t, NewSkillsProvider(t.TempDir()),
		[]string{"write", "probe", "--description", "d", "--content", "x", "--file", "y"}, "")
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("error = %v", err)
	}
}

func TestSkillsCLIWriteReadsStdin(t *testing.T) {
	dir := t.TempDir()
	if _, err := runCLI(t, NewSkillsProvider(dir),
		[]string{"write", "probe", "--description", "stdin skill"}, "piped body\n"); err != nil {
		t.Fatalf("write from stdin: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "probe", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "piped body") {
		t.Errorf("stdin body not landed: %q", raw)
	}
}

func TestSkillsCLIDeleteBacksUpAndRemoves(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "alpha", "the alpha skill")
	out, err := runCLI(t, NewSkillsProvider(dir), []string{"delete", "alpha"}, "")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !strings.Contains(out, `deleted skill "alpha"; 0 skill(s) remain`) {
		t.Errorf("confirmation = %q", out)
	}
	if _, serr := os.Stat(filepath.Join(dir, "alpha")); !os.IsNotExist(serr) {
		t.Errorf("the folder must be gone: %v", serr)
	}
	backups, err := os.ReadDir(filepath.Join(dir, ".curator", "backups"))
	if err != nil || len(backups) != 1 {
		t.Errorf("a delete is backed up: %v / %v", backups, err)
	}
	// the ledger row names the CLI as the writer, not the tab
	ledgerRaw, err := os.ReadFile(filepath.Join(dir, ".curator", "ledger.jsonl"))
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	var entry struct {
		Actor  string `json:"actor"`
		Action string `json:"action"`
		Skill  string `json:"skill"`
	}
	_ = json.Unmarshal([]byte(strings.SplitN(string(ledgerRaw), "\n", 2)[0]), &entry)
	if entry.Actor != ActorCLI || entry.Action != LedgerRemove || entry.Skill != "alpha" {
		t.Errorf("ledger entry = %+v", entry)
	}
	_, err = runCLI(t, NewSkillsProvider(dir), []string{"delete", "ghost"}, "")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("unknown delete error = %v", err)
	}
}

func TestSkillsCLIUsageLaws(t *testing.T) {
	sp := NewSkillsProvider(t.TempDir())
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"unknown verb", []string{"frobnicate"}, `unknown skills subcommand "frobnicate"`},
		{"read without a name", []string{"read"}, "usage: omega skills read <name>"},
		{"write flag before the name", []string{"write", "--description", "x"}, "the skill name comes first"},
		{"list takes no argument", []string{"list", "alpha"}, "skills list: unexpected argument"},
	}
	for _, c := range cases {
		_, err := runCLI(t, sp, c.args, "")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error = %v, want %q", c.name, err, c.want)
		}
	}
}

func TestSkillsCLIHelpAnswersWithoutTheNameLaws(t *testing.T) {
	sp := NewSkillsProvider(t.TempDir())
	for _, args := range [][]string{{"--help"}, {"read", "--help"}, {"help"}} {
		out, err := runCLI(t, sp, args, "")
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !strings.Contains(out, "omega skills - the skills registry") || !strings.Contains(out, "omega skills write <name> [flags]") {
			t.Errorf("%v: help output = %q", args, out)
		}
	}
}

func TestSkillsCLIReadRefusesTraversalNames(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "skills")
	writeSkill(t, dir, "alpha", "the alpha skill")
	// A SKILL.md-shaped file OUTSIDE the skills inventory: read resolves
	// <dir>/<name>/SKILL.md verbatim, so an unvalidated traversal name
	// would print this body.
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "SKILL.md"), []byte("---\ndescription: leaked\n---\nSECRET BODY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sp := NewSkillsProvider(dir)
	for _, name := range []string{"../outside", `..\outside`, "shared/../alpha", ".", "..", "alpha/beta"} {
		out, err := runCLI(t, sp, []string{"read", name}, "")
		if err == nil {
			t.Errorf("read %q must refuse, got %q", name, out)
			continue
		}
		if !strings.Contains(err.Error(), "single folder component") {
			t.Errorf("read %q error = %v, want the folder-name law", name, err)
		}
		if out != "" {
			t.Errorf("read %q printed a body before refusing: %q", name, out)
		}
	}
	// the same law guards write and delete at the shared door
	if _, err := runCLI(t, sp, []string{"write", "../outside", "--description", "x", "--content", "y"}, ""); err == nil || !strings.Contains(err.Error(), "single folder component") {
		t.Errorf("write traversal error = %v", err)
	}
	if _, err := runCLI(t, sp, []string{"delete", "../outside"}, ""); err == nil || !strings.Contains(err.Error(), "single folder component") {
		t.Errorf("delete traversal error = %v", err)
	}
}

func TestSkillsCLIHelpRefusesTrailingArguments(t *testing.T) {
	sp := NewSkillsProvider(t.TempDir())
	for _, args := range [][]string{{"--help", "list"}, {"read", "--help", "alpha"}, {"help", "write"}} {
		out, err := runCLI(t, sp, args, "")
		if err == nil {
			t.Errorf("%v: trailing argument after help must error, got %q", args, out)
			continue
		}
		if !strings.Contains(err.Error(), "unexpected argument") {
			t.Errorf("%v: error = %v", args, err)
		}
	}
}

func TestResolveSkillsDirsReRootsAgainstOmegaHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	sp := &SkillsProvider{Dir: "skills", SharedDir: "skills/shared"}
	if _, err := resolveSkillsDirs(sp); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if sp.Dir != filepath.Join(home, "skills") || sp.SharedDir != filepath.Join(home, "skills", "shared") {
		t.Errorf("resolved dirs = %q / %q", sp.Dir, sp.SharedDir)
	}
	// an absolute dir passes through untouched (the serve path's shape)
	abs := &SkillsProvider{Dir: filepath.Join(home, "own")}
	t.Setenv("OMEGA_HOME", "")
	if got, err := resolveSkillsDirs(abs); err != nil || got != abs.Dir {
		t.Errorf("absolute dir = %q, %v", got, err)
	}
	// a relative dir with no home is the loud refusal
	rel := &SkillsProvider{Dir: "skills"}
	if _, err := resolveSkillsDirs(rel); err == nil || !strings.Contains(err.Error(), "OMEGA_HOME is unset") {
		t.Errorf("relative dir without OMEGA_HOME error = %v", err)
	}
}
