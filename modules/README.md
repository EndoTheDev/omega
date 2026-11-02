# omega Modules

Modules are in-process Go packages that provide capabilities to
omega. Each module implements the `Plugin` interface from
`engine/agent/plugin.go` and mounts into a shared `Context`.

## How it works

1. The host collects plugins from compiled-in packages.
2. `agent.MountAllAPI` sorts plugins by dependencies (`Requires()`,
   topology is the only ordering) and mounts each through the typed
   `agent.ExtensionAPI` registration surface (`MountWithAPI`) - the
   only mount path (v0.12 phase F retired the classic seam-assignment
   `Mount` and its adapters). Every registration is journaled; the
   dismount replays it backward.
3. The agent loop reads from `Context` slots directly — zero IPC,
   full type safety.

## Plugin interface

```go
type Plugin interface {
    Name() string
    Provides() []string              // seam names: "provider", "store", etc.
    Requires() []string              // seam names this plugin needs first
    MountWithAPI(ctx *Context,       // the one typed registration
                 api *agent.ExtensionAPI) error // surface (*)
}
```

(*) `MountWithAPI` is the ONLY mount method. Every seam claim,
    additive append, tool, command, panel, route and emitter is a
    journaled `api.Register*` call inside it - see the census table
    in the `engine/agent/extensionapi.go` header and the capsule table below
    for what each registration builds.
```

## Seams

| Seam             | Type      | Description                                    |
| ---------------- | --------- | ---------------------------------------------- |
| `provider`       | exclusive | LLM provider (Ollama, OpenAI, Anthropic)       |
| `compactor`      | exclusive | Context compaction                             |
| `store`          | exclusive | Session persistence                            |
| `skills`         | exclusive | Skill loading                                  |
| `loop`           | exclusive | Agent loop                                     |
| `logging`        | exclusive | Operational logging (file logger)              |
| `memory`         | exclusive | Persistent memory (two-file store)             |
| `prompt_builder` | exclusive | System prompt builder                          |
| `trust`          | exclusive | Project trust gate (AGENTS.md context loading) |
| `cron`           | exclusive | Scheduled agent turns (crons.yaml)             |
| `notifications`  | exclusive | Finished-things store (SQLite)                 |
| `verdicts`       | exclusive | Verdict cards (the blocking `verdict` tool)    |
| `approvals`      | exclusive | Smart approval gate ([approvals.md](../docs/approvals.md)) |
| `kanban`         | exclusive | Task board (the kanban SQLite store)           |
| `tools`          | additive  | Tool provider (multiple plugins contribute)    |
| `channel`        | additive  | Delivery transport (HTTP, Discord, etc.)       |
| `prompt_sections` | additive | Prompt sections (modules render their own block) |

Exclusive seams conflict if two plugins provide the same one. The
`tools` seam is additive: multiple plugins contribute to
`Context.ToolProviders`. The `prompt_sections` seam is additive:
modules that own prompt content append a `PromptSectioner` to
`Context.PromptSectioners`; the prompt builder renders their
sections between Project Context and Environment.

## Module-owned surfaces

A module's surface is whatever it registers at Mount: seams, tools,
slash commands (`ctx.Commands` via the extension command seam), REST
routes (`api.RegisterRoute`, served by the live registry), webui UI
(`modules/<name>/ui/`), canvas panels. Removing the module removes the
feature whole; the removal proofs under each capsule pin the claim.

## Available modules

The table below lists the toggleable capsules in `modules/`. Note the
web search/fetch tool pair: it dissolved into the engine's
`http_channel` package in v0.13 (shell law 1) and is engine vocabulary
now - natural names `web.search`/`web.fetch`, always mounted, never in
the disable list. No web module exists.

| Package         | Seam(s)        | Description                                          |
| --------------- | -------------- | ---------------------------------------------------- |
| `agent_loop/`   | loop           | Default conversation loop (LoopProvider impl)        |
| `provider/`     | provider       | Ollama, OpenAI, Anthropic streaming                  |
| `store/`        | store, tools   | SQLite session store + sessions.search tool          |
| `skills/`       | skills, tools, prompt_sections | Skill loading + skills domain tool (read/write/update) + /skills command + Available Skills section |
| `compactor/`    | compactor      | Context compaction (keep-first/last + LLM summarize) |
| `logging/`      | logging        | Operational logging (file logger, LoggerProvider) + the /logs module command (the old follow mode died with the TUI shell) |
| `memory/`       | memory, tools, prompt_sections | Persistent memory (memory tool, two-file store) + snapshot block. The born-capsule: seam + tool + prompt section all ship from the same Mount; no command, REST route or canvas panel exists. Removal proof `TestMemoryRemovalProof` - arc 6 capsule |
| `prompt/`       | prompt_builder | Generic prompt assembler (sections from owning modules) |
| `tools/`        | tools, prompt_sections | Shell command + file tools + Tools section (firstLine descriptions); ships `ui/ToolsList.vue` (webui inspector panel from GET /tools) |
| `mcp/`          | tools          | MCP bridge (stdio + Streamable HTTP servers, MCP_SERVERS env or mcp.yaml). The additive born-capsule: the bridge tool provider is the whole surface - every discovered server tool ships server-qualified from the single Mount; no command, REST route, skills or canvas panel exists. Removal proof `TestMCPRemovalProof` - arc 6 capsule |
| `delegate/`     | tools          | Subagent delegation (`delegate.task` + `delegate.status`, the ctx.InjectedMessages/PendingDelegations seam, notifications emitter). The wired born-capsule: seam + tool + emitter capture all ship from the same Mount; no command, REST route, skills or canvas panel exists. Removal proof `TestDelegateRemovalProof` - arc 6 capsule. t_77e27ef8 gap-close: the recursion guard has a mount face (a child with OMEGA_SUBAGENT set registers NO delegate tools - depth stays at one level), children ride stored sessions (host pre-creates `delegate:<task>`, parented to the caller session, child inherits via `run --session`; `delegate.status` shows the handle), the inject is a capped summary-shaped turn (stdout only, named cut marker, short failure line on error), and `delegate.task` takes optional `agent`/`model` fields under the defaulting law (empty = default agent / session provider model, explicit model = OMEGA_MODEL, re-asserted over persona layers in cmdRun) |
| `verdict/`      | verdicts, tools, prompt_sections | Verdict cards (the single-answer hub, the blocking `verdict` tool, /verdict command + Verdict Cards prompt section). The interactive born-capsule: seam + tool + command + prompt section all ship from the same Mount; no canvas panel or actions exist (the card renders off the tool call; the /verdicts routes live in http_channel behind the seam-nil guard). Removal proof `TestVerdictRemovalProof` - arc 6 capsule |
| `approval/`     | approvals, prompt_sections | Smart approval gate: risky shell commands reach a reviewer before they run (decision model + deterministic ladder in smart mode, a verdict card in manual mode). Loop-side born-capsule: detector, parser, reviewer client, ladder, cache and audit log all ship from the one Mount and the whole feature is the `ctx.Approvals` seam the agent loop consults; no tool, command, route or webui surface exists (see [docs/approvals.md](../docs/approvals.md)). Removal proof `TestApprovalRemovalProof` |
| `datadir/`      | -              | Database directory layout (data/ folder + boot-time migration of legacy home-root dbs) |
| `kanban/`       | channel, tools, ui | Task board (SQLite store, /tasks endpoints, kanban seam + tools, Board.vue webui tab) |
| `notifications/` | notifications, panels | Finished-things store (SQLite, the bell) + the first module capsule: list panel with row mark-read actions through the action pipe, the toast as a canvas event, emitter wiring via the same registration (arc 4b) |
| `engine/http_channel/` | channel, panels | HTTP/SSE delivery channel (omega serve transport) + the chat canvas panel (chat-last 1/3, Start-gated registration per 2b): the conversation rides the snapshot + deltas + action pipe contract; the /chat REST + SSE wire stays untouched |
| `terminal/`     | -              | The webui terminal pane (t_55a3fc4b): a PTY in the bottom host over the ws channel; the dockable rides the engine registry, journey 95 |
| `browser/`      | -              | The webui browser pane (t_95c91f9c): any-origin iframe tab, GET/PUT /browser/last resume URL + GET /browser/frameable embed probe, journey 96 |
| `trust/`        | trust          | Project trust gate (trust.yaml, AGENTS.md loading). The gating born-capsule: the trust seam is the whole surface - ResolveContext gates the project context, State feeds the status bar; the host reads both through nil-guarded wrappers (cmd/omega/trust.go) before serve/loop launch. No tool, command, REST route, skills or canvas panel exists. Removal proof `TestTrustRemovalProof` - arc 6 capsule |
| `cron/`         | cron, panels   | Scheduled prompts (crons.yaml, cron:<name> sessions, CronJobs.vue + /cron module command, the jobs canvas panel - arc 6 capsule; POST /cron/toggle /cron/save /cron/delete) |
| `process/`      | tools          | Background process registry (card t_5782d11e): the five `process.spawn`/`list`/`read`/`kill`/`wait` verbs over detached long ops - state at `<home>/data/process.json`, one size-capped log per process under `<home>/logs/process/`, TERM-then-KILL kills, pid-recheck at every read (a second omega shares the state file; a gone pid = exited-unknown). The tool-face born-capsule: verbs only, no command/route/panel. Removal proof `TestProcessRemovalProof` |

### Authored skill files

The `skills/` module reads one directory per skill, the file always
named `SKILL.md` (`<skills-dir>/<skill-name>/SKILL.md`) - the skill
name is the FOLDER name, never frontmatter (only `description` is
read from it; reference layout: the prod home's `skills/` and the
dev home's `bin/skills/`). A directory holding `<name>.md` without
`SKILL.md` is invisible to discovery and gets one boot warning
naming the skill + "rename the file to SKILL.md" - no
auto-migration, the rename is the fix (card t_5baad951).

## Writing an module

1. Create a directory under `modules/<name>/`.
2. Implement the module logic as a struct.
3. Create a `plugin.go` with a `NewPlugin(...)` function returning
   a `Plugin` implementation.
4. `MountWithAPI` populates `Context` slots and registers every
   claim/tool/command/route through `api.Register*`.
5. Register the plugin in `buildPlugins()` in `cmd/omega/main.go` -
   an unregistered plugin never mounts.
6. Tools append to `ctx.ToolProviders` in `MountWithAPI`; commands
   append to `ctx.Commands` and chain `ctx.CommandHandler` (call the
   previous handler on miss).
7. Leave `<name>_test.go` behind - each non-trivial package carries
   a self-check.

Never create an `AGENTS.md` for a module - the root `AGENTS.md` is
the single worker map; worker-facing rules for a new module land in
the root map's module laws and in `docs/architecture.md`.

### Module-owned UI

UI for module X lives in `modules/X/ui/`; shell components live in
`engine/webui/app/components/<area>/`; components only move to a module
when the module owns the domain. The registration is a directory: put
top-level `.vue` components there (e.g. the tools module's
`ui/ToolsList.vue`) and the webui build (engine/webui/nuxt.config.ts)
discovers them - no manifest, no registry. Each file becomes one panel
in the shell's inspector, and the module name is the namespace prefix,
so module and shell component names never collide. A module's UI is
tested there and reviewed as part of the module.

Example:

```go
package myext

import (
    "agent"
)

type Plugin struct{}

func (p *Plugin) Name() string       { return "my-module" }
func (p *Plugin) Provides() []string { return []string{"tools"} }
func (p *Plugin) Requires() []string { return nil }
func (p *Plugin) MountWithAPI(ctx *agent.Context, api *agent.ExtensionAPI) error {
    ctx.ToolProviders = append(ctx.ToolProviders, myToolProvider{})
    return nil
}

func NewPlugin() *Plugin { return &Plugin{} }
```
