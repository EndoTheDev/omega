package prompt

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/modules/memory"
	"github.com/EndoTheDev/omega/modules/skills"
	"github.com/EndoTheDev/omega/modules/tools"
)

// updateGolden regenerates the golden files when OMEGA_UPDATE_GOLDEN=1.
// Set it only when the rendered prompt changes on purpose, never during
// refactors - the goldens must stay byte-stable across them.
var updateGolden = os.Getenv("OMEGA_UPDATE_GOLDEN") == "1"

// dateLine matches the environment date stamp. It is scrubbed before
// comparison so the goldens do not expire at midnight.
var dateLine = regexp.MustCompile(`(?m)^Date: .*$`)

// wireContext mounts the production extension set the same way
// cmd/omega does: memory, skills, tools, prompt, with their configs.
// Extensions then come from ctx.Infos, exactly like the real loop.
func wireContext(t *testing.T, skillDir string, memCfg memory.Config) *core.Context {
	t.Helper()
	ctx := &core.Context{
		Configs: map[string]any{
			"memory": memCfg,
			"skills": skills.Config{Dir: skillDir},
		},
	}
	plugins := []agent.Plugin{
		memory.NewPlugin(),
		skills.NewPlugin(),
		tools.NewPlugin(),
		NewPlugin(),
	}
	if err := agent.MountAll(plugins, ctx); err != nil {
		t.Fatalf("MountAll: %v", err)
	}
	return ctx
}

// seedSkills creates two skills in dir.
func seedSkills(t *testing.T, dir string) {
	t.Helper()
	for name, desc := range map[string]string{
		"alpha": "Alpha does the first thing",
		"beta":  "Beta does the second thing\nwith a second line",
	} {
		skillDir := filepath.Join(dir, name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\nBody.\n"
		if err := os.WriteFile(filepath.Join(skillDir, name+".md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// seedMemory creates both memory files with entries.
func seedMemory(t *testing.T, cfg memory.Config) {
	t.Helper()
	if err := os.WriteFile(cfg.File, []byte("User prefers concise responses\n§\nsecond memory entry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.UserProfileFile, []byte("The user lives in Springfield.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fullOpts exercises every prompt input: project context, custom, append.
func fullOpts(ctx *core.Context) core.PromptBuildOptions {
	return core.PromptBuildOptions{
		CWD:            "/test/cwd",
		ProjectContext: "# AGENTS.md\n\nProject rules live here.\n",
		Custom:         "Tone: direct.\nStay terse.",
		Append:         []string{"Extra appended prompt one.", "Extra appended prompt two."},
		Extensions:     ctx.Infos,
	}
}

// minimalOpts carries only CWD and Extensions.
func minimalOpts(ctx *core.Context) core.PromptBuildOptions {
	return core.PromptBuildOptions{
		CWD:        "/test/cwd",
		Extensions: ctx.Infos,
	}
}

// compareGolden renders a scenario and checks it against its golden
// file under testdata/. Scrubs the date line first.
func compareGolden(t *testing.T, name string, prompt string) {
	t.Helper()
	scrubbed := dateLine.ReplaceAllString(prompt, "Date: <date>")
	goldenPath := filepath.Join("testdata", name+".txt")
	if updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(scrubbed), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v (run with OMEGA_UPDATE_GOLDEN=1 to regenerate)", err)
	}
	if got, wantTxt := scrubbed, string(want); got != wantTxt {
		t.Errorf("prompt %s diverges from golden:\n--- got ---\n%s\n--- want ---\n%s", name, got, wantTxt)
	}
}

// TestPromptGoldenFull pins the fully rendered prompt with every
// extension section populated and every prompt input set.
func TestPromptGoldenFull(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills")
	seedSkills(t, skillDir)
	memCfg := memory.Config{
		Enabled:              true,
		UserProfileEnabled:   true,
		CharLimit:            2200,
		UserProfileCharLimit: 1375,
		File:                 filepath.Join(dir, "memory.md"),
		UserProfileFile:      filepath.Join(dir, "user.md"),
	}
	seedMemory(t, memCfg)

	ctx := wireContext(t, skillDir, memCfg)
	prompt, ok := ctx.PromptBuilder.BuildPrompt(context.Background(), fullOpts(ctx))
	if !ok {
		t.Fatal("BuildPrompt returned ok=false")
	}
	compareGolden(t, "golden_prompt_full", prompt)
}

// TestPromptGoldenMinimal pins the prompt with sections populated but
// no project context, custom, or append input.
func TestPromptGoldenMinimal(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills")
	seedSkills(t, skillDir)
	memCfg := memory.Config{
		Enabled:              true,
		UserProfileEnabled:   true,
		CharLimit:            2200,
		UserProfileCharLimit: 1375,
		File:                 filepath.Join(dir, "memory.md"),
		UserProfileFile:      filepath.Join(dir, "user.md"),
	}
	seedMemory(t, memCfg)

	ctx := wireContext(t, skillDir, memCfg)
	prompt, ok := ctx.PromptBuilder.BuildPrompt(context.Background(), minimalOpts(ctx))
	if !ok {
		t.Fatal("BuildPrompt returned ok=false")
	}
	compareGolden(t, "golden_prompt_minimal", prompt)
}

// TestPromptGoldenEmptyStores pins the prompt when skills and memory
// are mounted but hold nothing: no skills section, no memory block.
func TestPromptGoldenEmptyStores(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills") // exists, but empty
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	memCfg := memory.Config{
		Enabled:              true,
		UserProfileEnabled:   true,
		CharLimit:            2200,
		UserProfileCharLimit: 1375,
		File:                 filepath.Join(dir, "memory.md"), // file absent
		UserProfileFile:      filepath.Join(dir, "user.md"),
	}

	ctx := wireContext(t, skillDir, memCfg)
	prompt, ok := ctx.PromptBuilder.BuildPrompt(context.Background(), fullOpts(ctx))
	if !ok {
		t.Fatal("BuildPrompt returned ok=false")
	}
	if strings.Contains(prompt, "## Available Skills") {
		t.Error("prompt has Available Skills section with empty skills dir")
	}
	if !strings.Contains(prompt, "## Tools") {
		t.Error("prompt missing Tools section")
	}
	compareGolden(t, "golden_prompt_empty_stores", prompt)
}