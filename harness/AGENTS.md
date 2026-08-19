# harness

Harness layer: skill loading and project context loading. These are
data-gathering concerns, not agent core — they produce data consumed
by the agent's extensions (core-prompt reads skills, the trust system
reads project context).

## Ownership

- `harness.go` - `LoadSkills`, `loadSkill`, `readRemaining`, `ProjectRoot`,
  `LoadProjectContext`
- `context_test.go` - project context loading tests
- `skills_test.go` - skill loading tests

## Local Contracts

- **LoadSkills returns []agent.Skill.** The harness loads skills from
  the filesystem and returns them. The core-prompt extension calls
  `LoadSkills` via `OMEGA_SKILLS_DIR` to populate the system prompt.
  The `skills.read` tool is provided by the core-tools extension.
- **LoadProjectContext walks up from CWD.** It collects AGENTS.md files
  at each directory level, root-to-leaf order. The trust system in
  `cmd/omega/trust.go` gates which projects are loaded. The result is
  passed to the core-prompt extension via `PromptBuildOptions.ProjectContext`.
- **ProjectRoot is the trust unit.** It returns the nearest directory
  containing an AGENTS.md, used by the trust system to identify projects.
- **harness imports agent, not the other way around.** The `Skill`
  type lives in `agent/` and is returned by `LoadSkills`.
