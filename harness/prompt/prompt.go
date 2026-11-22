// Package prompt builds the omega system prompt.
package prompt

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// PromptBuilder is the in-process system prompt builder.
type PromptBuilder struct {
	pctx *core.Context
	now  func() time.Time // injectable clock for tests
}

// NewPromptBuilder creates a PromptBuilder.
func NewPromptBuilder(ctx *core.Context) *PromptBuilder {
	return &PromptBuilder{pctx: ctx, now: time.Now}
}

// BuildPrompt assembles the full system prompt from the build options.
func (b *PromptBuilder) BuildPrompt(_ context.Context, opts core.PromptBuildOptions) (string, bool) {
	var sb strings.Builder
	sb.WriteString(posture(opts))

	sb.WriteString("\n## Guidelines\n")
	for _, g := range b.Guidelines(opts) {
		fmt.Fprintf(&sb, "- %s\n", g)
	}

	if opts.ProjectContext != "" {
		sb.WriteString("\n## Project Context\n")
		sb.WriteString(opts.ProjectContext)
		sb.WriteString("\n")
	}

	b.writeSections(&sb, opts)

	sb.WriteString("\n## Environment\n")
	fmt.Fprintf(&sb, "CWD: %s\n", opts.CWD)
	fmt.Fprintf(&sb, "OS: %s\n", runtime.GOOS)
	if runtime.GOOS == "windows" {
		sb.WriteString("Shell: cmd.exe\n")
	} else {
		sb.WriteString("Shell: bash\n")
	}
	fmt.Fprintf(&sb, "Date: %s\n", b.now().Format("2006-01-02"))

	if opts.Custom != "" {
		sb.WriteString("\n")
		sb.WriteString(opts.Custom)
		sb.WriteString("\n")
	}
	for _, extra := range opts.Append {
		sb.WriteString("\n")
		sb.WriteString(extra)
		sb.WriteString("\n")
	}

	return sb.String(), true
}

// writeSections collects PromptSections from every contributing capsule and renders them.
func (b *PromptBuilder) writeSections(sb *strings.Builder, opts core.PromptBuildOptions) {
	if b.pctx == nil {
		return
	}

	var sections []core.PromptSection
	for _, s := range b.pctx.PromptSectioners {
		if s == nil {
			continue
		}
		sections = append(sections, s.PromptSections(opts)...)
	}
	sort.SliceStable(sections, func(i, j int) bool {
		return sections[i].Order < sections[j].Order
	})

	for _, sec := range sections {
		if sec.Title != "" {
			fmt.Fprintf(sb, "\n## %s\n", sec.Title)
			sb.WriteString(sec.Body)
		} else {
			sb.WriteString("\n")
			sb.WriteString(sec.Body)
			sb.WriteString("\n")
		}
	}
}

// posture is the prompt's opening line. It never claims a capability the run cannot
// execute: when the fold leaves no callable tool, the session says so instead of opening
// with "access to tools" the model then acts on.
func posture(opts core.PromptBuildOptions) string {
	if opts.HasTools() {
		return "You are an AI coding agent with access to tools.\n"
	}
	return "You are an AI coding agent. No tools are enabled for this session - you cannot read files, run commands, search the web, or query the store. Answer from your own knowledge, and say plainly when a request needs a capability you do not have.\n"
}

// Guidelines returns the default guideline lines rendered under "## Guidelines", gated by
// the run's fold: a line that names a capability the wire does not carry never renders, so
// the prose cannot advertise what the tools block left out.
func (b *PromptBuilder) Guidelines(opts core.PromptBuildOptions) []string {
	var out []string
	if opts.ToolsEnabled("files.read", "files.grep", "files.glob", "shell.run") {
		out = append(out, "Use tools to read files and run commands before making assumptions.")
	}
	out = append(out, "Prefer the simplest solution that works. Avoid unnecessary abstraction.")
	if opts.ToolsEnabled("files.edit", "files.write", "files.batch") {
		out = append(out, "When editing files, match the existing style and conventions.")
	}
	if opts.ToolsEnabled("sessions.search", "sessions.hygiene") {
		out = append(out, "Never open omega.db directly - the store tools are the interface to the database.")
	}
	return append(out,
		"Report what you did concisely. Do not repeat file contents back.",
		"If something fails, report the error honestly rather than guessing.",
	)
}
