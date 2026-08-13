package agent

import (
	"strings"
	"testing"
)

func TestBuildSystemPromptSections(t *testing.T) {
	prompt := BuildSystemPrompt(PromptOptions{
		ProjectContext: "# AGENTS\nrules",
		Skills: []Skill{
			{Name: "learn-skill", Description: "Teaches the agent"},
		},
		CWD:    "/tmp/proj",
		Custom: "Be concise.",
	})
	for _, want := range []string{
		"You are an AI coding agent with access to tools.",
		"## Project Context",
		"# AGENTS",
		"## Available Skills",
		"learn-skill: Teaches the agent",
		"## Environment",
		"CWD: /tmp/proj",
		"Date: ",
		"Be concise.",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q\n%s", want, prompt)
		}
	}
	// Tool descriptions are not in the system prompt (sent as JSON schemas).
	if strings.Contains(prompt, "## Available Tools") {
		t.Errorf("prompt should not list tools\n%s", prompt)
	}
}

func TestBuildSystemPromptOmitsEmptySections(t *testing.T) {
	prompt := BuildSystemPrompt(PromptOptions{CWD: "/tmp"})
	for _, absent := range []string{"## Project Context", "## Available Skills", "Be concise."} {
		if strings.Contains(prompt, absent) {
			t.Errorf("prompt should omit %q\n%s", absent, prompt)
		}
	}
	if !strings.Contains(prompt, "CWD: /tmp") {
		t.Errorf("prompt missing CWD\n%s", prompt)
	}
}
