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
	"github.com/EndoTheDev/omega/harness/files"
	"github.com/EndoTheDev/omega/harness/memory"
	"github.com/EndoTheDev/omega/harness/observability"
	"github.com/EndoTheDev/omega/harness/skills"
	"github.com/EndoTheDev/omega/harness/terminal"
	"github.com/EndoTheDev/omega/harness/tools"
)

// updateGolden regenerates the golden files when OMEGA_UPDATE_GOLDEN=1.
// Set it only when the rendered prompt changes on purpose, never during
// refactors - the goldens must stay byte-stable across them.
var updateGolden = os.Getenv("OMEGA_UPDATE_GOLDEN") == "1"

// envLine matches the environment stamp lines that vary by host.
var envLine = regexp.MustCompile(`(?m)^(OS|Shell): .*$`)

// scrubEnv replaces the host-varying environment lines with placeholders.
func scrubEnv(prompt string) string {
	return envLine.ReplaceAllStringFunc(prompt, func(line string) string {
		if strings.HasPrefix(line, "OS: ") {
			return "OS: <env>"
		}
		return "Shell: <env>"
	})
}

// wireContext mounts the production extension set the same way cmd/omega does.
// Extensions then come from ctx.Inventory(), exactly like the real loop.
func wireContext(t *testing.T, skillDir string, memCfg memory.Config) *core.Context {
	t.Helper()
	ctx := &core.Context{
		Configs: map[string]any{
			"memory":  memCfg,
			"skills":  skills.Config{Dir: skillDir},
			"logging": observability.Config{Enabled: false},
		},
	}
	plugins := []agent.Plugin{
		observability.NewPlugin(),
		memory.NewPlugin(),
		skills.NewPlugin(),
		tools.NewPlugin(),
		files.NewPlugin(),
		terminal.NewShellPlugin(),
		NewPlugin(),
	}
	if _, err := agent.MountAllAPI(plugins, ctx, nil, nil); err != nil {
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
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
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
		Extensions:     ctx.Inventory(),
	}
}

// minimalOpts carries only CWD and Extensions.
func minimalOpts(ctx *core.Context) core.PromptBuildOptions {
	return core.PromptBuildOptions{
		CWD:        "/test/cwd",
		Extensions: ctx.Inventory(),
	}
}

// compareGolden renders a scenario and checks it against its golden file.
func compareGolden(t *testing.T, name string, prompt string) {
	t.Helper()
	scrubbed := scrubEnv(prompt)
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

// TestPromptGoldenFull pins the fully rendered prompt with every extension section.
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
	prompt, ok := ctx.Services().Resolve("prompt").(agent.PromptBuilder).BuildPrompt(context.Background(), fullOpts(ctx))
	if !ok {
		t.Fatal("BuildPrompt returned ok=false")
	}
	compareGolden(t, "golden_prompt_full", prompt)
}

// TestPromptGoldenMinimal pins the prompt with sections populated but no project.
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
	prompt, ok := ctx.Services().Resolve("prompt").(agent.PromptBuilder).BuildPrompt(context.Background(), minimalOpts(ctx))
	if !ok {
		t.Fatal("BuildPrompt returned ok=false")
	}
	compareGolden(t, "golden_prompt_minimal", prompt)
}

// TestPromptToolsFoldDropsSections pins the acceptance pair on the real capsule set: with
// the fold empty every section renders, and with the fold naming every tool the sections
// that describe those tools are gone from the prompt. This is the defect that shipped -
// the Tools list emptied while the prose kept describing the tools it had dropped.
func TestPromptToolsFoldDropsSections(t *testing.T) {
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
	builder := ctx.Services().Resolve("prompt").(agent.PromptBuilder)

	// Empty fold: the sections render.
	on, ok := builder.BuildPrompt(context.Background(), minimalOpts(ctx))
	if !ok {
		t.Fatal("BuildPrompt returned ok=false")
	}
	for _, want := range []string{"## Available Skills", "Call skills.read", "access to tools", "- Use tools to read files"} {
		if !strings.Contains(on, want) {
			t.Errorf("unfolded prompt missing %q\n--- prompt ---\n%s", want, on)
		}
	}
	// The store guideline is presence-gated too: this scenario mounts no store capsule, so
	// the prompt must not send the model to store tools that are not on the wire.
	if strings.Contains(on, "store tools") {
		t.Errorf("prompt names store tools although the scenario mounts no store capsule:\n%s", on)
	}

	// Every tool folded: the sections that describe them are gone, and the posture stops
	// claiming tools.
	fold := core.ToolNames(ctx.Inventory())
	if len(fold) == 0 {
		t.Fatal("inventory carries no tools, the pin proves nothing")
	}
	opts := minimalOpts(ctx)
	opts.DisabledTools = fold
	opts.Extensions = core.StampDisabledTools(ctx.Inventory(), fold)
	off, ok := builder.BuildPrompt(context.Background(), opts)
	if !ok {
		t.Fatal("BuildPrompt returned ok=false")
	}
	for _, banned := range []string{"## Available Skills", "skills.read", "## Approval Gate", "## Tools", "- files.read", "access to tools", "Use tools to read files", "store tools"} {
		if strings.Contains(off, banned) {
			t.Errorf("fully folded prompt still carries %q\n--- prompt ---\n%s", banned, off)
		}
	}
	if !strings.Contains(off, "No tools are enabled for this session") {
		t.Errorf("fully folded prompt does not say the session has no tools:\n%s", off)
	}
}

// TestPromptGoldenEmptyStores pins the prompt when skills and memory are mounted.
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
	prompt, ok := ctx.Services().Resolve("prompt").(agent.PromptBuilder).BuildPrompt(context.Background(), fullOpts(ctx))
	if !ok {
		t.Fatal("BuildPrompt returned ok=false")
	}
	if strings.Contains(prompt, "## Available Skills") {
		t.Error("prompt has Available Skills section with empty skills dir")
	}
	if strings.Contains(prompt, "## Tools") {
		t.Error("prompt carries a Tools section although no wired tool declares Actions")
	}
	compareGolden(t, "golden_prompt_empty_stores", prompt)
}
