package agent

import (
	"strings"
	"testing"
)

func TestBuildSystemPromptSections(t *testing.T) {
	prompt := BuildSystemPrompt(PromptOptions{
		ProjectContext: "# AGENTS\nrules",
		Tools: map[string]Tool{
			"shell": {Description: "Run a shell command."},
		},
		CWD:    "/tmp/proj",
		Custom: "Be concise.",
	})
	for _, want := range []string{
		"You are an AI coding agent with access to tools.",
		"## Project Context",
		"# AGENTS",
		"## Available Tools",
		"- shell: Run a shell command.",
		"## Environment",
		"CWD: /tmp/proj",
		"Date: ",
		"Be concise.",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q\n%s", want, prompt)
		}
	}
}

func TestBuildSystemPromptOmitsEmptySections(t *testing.T) {
	prompt := BuildSystemPrompt(PromptOptions{CWD: "/tmp"})
	for _, absent := range []string{"## Project Context", "## Available Tools", "Be concise."} {
		if strings.Contains(prompt, absent) {
			t.Errorf("prompt should omit %q\n%s", absent, prompt)
		}
	}
	if !strings.Contains(prompt, "CWD: /tmp") {
		t.Errorf("prompt missing CWD\n%s", prompt)
	}
}
