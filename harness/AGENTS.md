# harness

Harness layer: system prompt building, skill loading, and project
context loading. These are harness concerns, not agent core — they
produce data consumed by the agent's capability seams.

## Ownership

- `harness.go` - `BuildSystemPrompt`, `PromptOptions` (with `Extensions`
  field), `LoadSkills`, `loadSkill`, `readRemaining`, `ProjectRoot`,
  `LoadProjectContext`. System prompt sections: Guidelines, Project Context,
  Available Skills, Tools (all from extensions, first line of description),
  Environment (CWD, OS, Shell, Date), Custom, Append.
- `context_test.go` - project context loading tests
- `prompt_test.go` - system prompt builder tests
- `skills_test.go` - skill loading tests

## Local Contracts

- **PromptOptions references agent.Skill.** The `Skill` type lives in
  `agent/` (it's a data type used by `SetSkills` for system prompt listing).
  The harness imports agent, not the other way around.
- **BuildSystemPrompt is the default prompt builder.** `main.go` calls
  it, passes the result to `agent.DefaultPromptBuilder`, which implements
  the `PromptBuilder` seam. Extensions can replace the entire prompt via
  `BuildPrompt`.
- **LoadSkills returns []agent.Skill.** The harness loads skills from
  the filesystem and returns them. `main.go` passes them to
  `agent.SetSkills` which stores them for the system prompt listing.
  The `skills.read` tool is provided by the core-tools extension.
- **LoadProjectContext walks up from CWD.** It collects AGENTS.md files
  at each directory level, root-to-leaf order. The trust system in
  `cmd/omega/trust.go` gates which projects are loaded.
