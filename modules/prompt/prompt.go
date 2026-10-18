// Package prompt builds the omega system prompt.
//
// It is a generic assembler: identity, guidelines, project context,
// environment, user-supplied custom/append, plus whatever sections
// other modules contribute via the agent.PromptSectioner seam. Modules
// own their prompt content; this package owns only the assembly order.
// It implements agent.PromptBuilder.
package prompt

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/agent"
)

// PromptBuilder is the in-process system prompt builder.
type PromptBuilder struct {
	pctx *agent.Context
	now  func() time.Time // injectable clock for tests
}

// NewPromptBuilder creates a PromptBuilder. Contributing prompt
// sections are read from the Context at build time, so every
// PromptSectioner plugin mounted anywhere is picked up.
func NewPromptBuilder(ctx *agent.Context) *PromptBuilder {
	return &PromptBuilder{pctx: ctx, now: time.Now}
}

// BuildPrompt assembles the full system prompt from the build options.
// Returns (prompt, true) on success. It always returns true — the
// prompt builder is the default source of the system prompt.
func (b *PromptBuilder) BuildPrompt(_ context.Context, opts agent.PromptBuildOptions) (string, bool) {
	var sb strings.Builder
	sb.WriteString("You are an AI coding agent with access to tools.\n")

	sb.WriteString("\n## Guidelines\n")
	for _, g := range b.Guidelines() {
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

// writeSections collects PromptSections from every contributing module
// and renders them in stable order. Untitled sections render as raw
// blocks with a blank line before and after; titled sections render
// under a "## <Title>" heading.
func (b *PromptBuilder) writeSections(sb *strings.Builder, opts agent.PromptBuildOptions) {
	if b.pctx == nil {
		return
	}

	var sections []agent.PromptSection
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

// Guidelines returns the default guideline lines appended under
// "## Guidelines" in the system prompt.
func (b *PromptBuilder) Guidelines() []string {
	return []string{
		"Use tools to read files and run commands before making assumptions.",
		"Prefer the simplest solution that works. Avoid unnecessary abstraction.",
		"When editing files, match the existing style and conventions.",
		"Report what you did concisely. Do not repeat file contents back.",
		"If something fails, report the error honestly rather than guessing.",
	}
}