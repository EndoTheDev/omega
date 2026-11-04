# omega Harness

The harness is the opinions tier: in-process Go capsules that provide
capabilities to omega. Each module implements the `Plugin` interface
from `agent/plugin.go` and mounts into a shared `Context`.

## Reading rule

A NEED lives with its consumer; an IMPLEMENTATION lives in harness/
(that is the part-3 seam constitution - the design record is
docs/architecture.md's seam-constitution section). The loop machinery
declares its need faces in `agent/seams.go`; the substrate `core/`
carries no seam types at all; every other consumer (a capsule, the
serving surface, the host) declares its own private local interface
for what it calls and resolves the service by its key from the
universal registry. The deletability test strengthened accordingly:
`rm -r harness/cron` removes cron from source AND binary - zero
cron-shaped vocabulary survives outside the folder, because the only
things naming it are the owner and the consumers' resolve-by-key
strings. `agent/` and `agent/loop` are constitutional (no loop, no
product); every harness capsule is removable by design (the
three-tier restructure, card t_768c6cc1). Note the one legal edge the
older grammar had: harness -> harness imports stay CORRECT opinions
reading other opinions (prompt -> tools, continuous -> store), and
sharing a service without the import goes through
`api.Offer(key, svc)` + the consumer's own face - never through a
substrate-named type.

Two load-bearing facts, settled and not open for re-litigation:

- **harness -> harness imports are LEGAL.** prompt -> tools,
  continuous -> store are correct opinions reading other opinions.
  The forbidden edge is the reverse: harness is below agent and llm
  and never imported by them (the agent import-law test reds on any
  `omega/harness/...` import).
- **store is a load-bearing harness capsule.** Harness capsules read
  it (continuous, tools; more in tests and cmd), every session table
  lives under it; the need face is the consumer's (agent.StoreProvider
  and its local sub-faces), the sqlite
  implementation here is a choice. Removing store removes every
  capsule that stores something - it exits the binary only with them;
  the host falls back to an in-memory store it offers under the same
  "store" key (cmd/omega/main.go).

## How it works

1. The host collects plugins from compiled-in packages.
2. `agent.MountAllAPI` sorts plugins by dependencies (`Requires()`,
   topology is the only ordering) and mounts each through the typed
   `agent.ExtensionAPI` registration surface (`MountWithAPI`) - the
   only mount path (v0.12 phase F retired the classic seam-assignment
   `Mount` and its adapters). Every registration is journaled; the
   dismount replays it backward.
3. The loop resolves its needs from the registry at bind and asserts
   shape there (wrong shape = loud boot failure naming the need) -
   zero IPC, no substrate-named types, the consumers own their faces.

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
    in the `agent/extensionapi.go` header and the capsule table below
    for what each registration builds.
```

## Seams (the part-3 grammar: Offer/Resolve, not slots)

One universal seam mechanism. A capsule does not fill a named
slot - it OFFERS services under a key (`api.Offer(key, svc)` at
`MountWithAPI`, journaled like every registration; one owner per
key, a different tenant on a held key is a boot error,
`core.ErrSeamForeignTenant`). The consumer declares its OWN private
interface for what it calls and resolves the key. Two closed
namespaces (proposing a new key is a design event, argued in a
card):

### Need keys (8; the loop's shopping list, `agent/seams.go` faces)

Resolved + shape-asserted at bind; missing supply = the documented
floor (nil-safe degradation), wrong shape = loud boot failure naming
the need:

| Key         | Need                       | Supplying capsule |
| ----------- | -------------------------- | ------------------- |
| `provider`  | the model client           | `provider/`     |
| `store`     | session persistence        | `store/` (the host offers an in-memory fallback when the capsule is absent) |
| `loop`      | the multi-turn loop        | `agent/loop` is the default supply, offered under the key like any capsule's |
| `logger`    | operational logging        | `logging/`      |
| `compactor` | context compaction         | `compactor/`    |
| `prompt`    | system prompt + guidelines | `prompt/`       |
| `nudger`    | the learning-loop reminder | `skills/`       |
| `approvals` | the smart approval gate    | `approval/`     |

### Capsule-service keys (6; owner-offered sharing)

| Key              | Service                                    | Example consumers |
| ---------------- | ------------------------------------------ | ------------------ |
| `skills`         | Skill loading                              | the serving surface (registry browser), the prompt sections |
| `memory`         | Persistent memory (two-file store)         | `cron/` for deliver=memory jobs (`memoryStore` face) |
| `trust`          | Project trust gate (AGENTS.md context)     | `approval/`'s gate, the host (`cmd/omega/trust.go` need faces), the prompt context path |
| `cron`           | Scheduled prompts (crons.yaml)             | the jobs surface |
| `notifications`  | Finished-things store (SQLite)             | `cron/`, `delegate/`, `kanban/` (the finished-notifier faces) |
| `verdicts`       | Verdict cards (the blocking `verdict` tool)| `approval/` (the manual-mode card), the verdict routes |

### The additive rails (multiple tenants, appended)

`tools` (RegisterTool / RegisterRuntimeTools), `channel`
(RegisterChannel), `prompt_sections` (RegisterPromptSectioner),
panels (RegisterPanelProvider), module REST routes (RegisterRoute),
commands (RegisterCommand / the `ctx.CommandHandler` chain),
subcommands (RegisterSubcommand), delegation (RegisterDelegation),
emitters (RegisterEmitter), lifecycle handles (RunGroup). The
machine-facing names are namespaced strings ("module.verb"); the
registry records the owner and refuses a cross-tenant claim.
`kanban`'s Provides() `"kanban"` string is a mount-order claim only -
nothing is offered under it.

## Module-owned surfaces

A module's surface is whatever it registers or offers at Mount:
service offers (the closed keys above), tools,
slash commands (`ctx.Commands` via the extension command seam), REST
routes (`api.RegisterRoute`, served by the live registry), webui UI
(`harness/<name>/ui/`), canvas panels. Removing the module removes the
feature whole; the removal proofs under each capsule pin the claim.

## Available modules

The table below lists the toggleable capsules in `harness/`. Note the
web search/fetch tool pair: it dissolved into the serving
`http_channel` package in v0.13 (shell law 1) and is built-in vocabulary
now - natural names `web.search`/`web.fetch`, always mounted, never in
the disable list. No web module exists.

| Package         | Offers          | Description                                          |
| --------------- | --------------- | ---------------------------------------------------- |
| `provider/`     | provider       | Ollama, OpenAI, Anthropic streaming                  |
| `store/`        | store, tools   | SQLite session store + sessions.search tool          |
| `skills/`       | skills, nudger, tools, prompt_sections | Skill loading + skills domain tool (read/write/update) + /skills command + Available Skills section (the capsule offers the `nudger` need supply too) |
| `compactor/`    | compactor      | Context compaction (keep-first/last + LLM summarize) |
| `logging/`      | logger         | Operational logging (file logger) + the /logs module command (the old follow mode died with the TUI shell) |
| `memory/`       | memory, tools, prompt_sections | Persistent memory (memory tool, two-file store) + snapshot block. The born-capsule: seam + tool + prompt section all ship from the same Mount; no command, REST route or canvas panel exists. Removal proof `TestMemoryRemovalProof` - arc 6 capsule |
| `prompt/`       | prompt         | Generic prompt assembler (sections from owning modules) |
| `tools/`        | tools, prompt_sections | Shell command + file tools + Tools section (firstLine descriptions); ships `ui/ToolsList.vue` (webui inspector panel from GET /tools) |
| `mcp/`          | tools          | MCP bridge (stdio + Streamable HTTP servers, MCP_SERVERS env or mcp.yaml). The additive born-capsule: the bridge tool provider is the whole surface - every discovered server tool ships server-qualified from the single Mount; no command, REST route, skills or canvas panel exists. Removal proof `TestMCPRemovalProof` - arc 6 capsule |
| `delegate/`     | tools          | Subagent delegation (`delegate.task` + `delegate.status`, the ctx.InjectedMessages/PendingDelegations seam, notifications emitter). The wired born-capsule: seam + tool + emitter capture all ship from the same Mount; no command, REST route, skills or canvas panel exists. Removal proof `TestDelegateRemovalProof` - arc 6 capsule. t_77e27ef8 gap-close: the recursion guard has a mount face (a child with OMEGA_SUBAGENT set registers NO delegate tools - depth stays at one level), children ride stored sessions (host pre-creates `delegate:<task>`, parented to the caller session, child inherits via `run --session`; `delegate.status` shows the handle), the inject is a capped summary-shaped turn (stdout only, named cut marker, short failure line on error), and `delegate.task` takes optional `agent`/`model` fields under the defaulting law (empty = default agent / session provider model, explicit model = OMEGA_MODEL, re-asserted over persona layers in cmdRun) |
| `verdict/`      | verdicts, tools, prompt_sections | Verdict cards (the single-answer hub, the blocking `verdict` tool, /verdict command + Verdict Cards prompt section). The interactive born-capsule: seam + tool + command + prompt section all ship from the same Mount; no canvas panel or actions exist (the card renders off the tool call; the /verdicts routes live in agent/api behind the seam-nil guard). Removal proof `TestVerdictRemovalProof` - arc 6 capsule |
| `approval/`     | approvals, prompt_sections | Smart approval gate: risky shell commands reach a reviewer before they run (decision model + deterministic ladder in smart mode, a verdict card in manual mode). Loop-side born-capsule: detector, parser, reviewer client, ladder, cache and audit log all ship from the one Mount and the whole feature is the approvals offer (the "approvals" key; the agent loop binds and consults it at boot); no tool, command, route or webui surface exists (see [docs/approvals.md](../docs/approvals.md)). Removal proof `TestApprovalRemovalProof` |
| `datadir/`      | -              | Database directory layout (data/ folder + boot-time migration of legacy home-root dbs) |
| `kanban/`       | kanban, tools  | Task board (SQLite store, /tasks endpoints, the Provides "kanban" mount-order claim + domain tool, Board.vue webui tab) |
| `notifications/` | notifications, panels | Finished-things store (SQLite, the bell) + the first module capsule: list panel with row mark-read actions through the action pipe, the toast as a canvas event, emitter wiring via the same registration (arc 4b) |
| `terminal/`     | -              | The webui terminal pane (t_55a3fc4b): a PTY in the bottom host over the ws channel; the dockable rides the shell registry, journey 95 |
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

## Writing a module

1. Create a directory under `harness/<name>/`.
2. Implement the module logic as a struct.
3. Create a `plugin.go` with a `NewPlugin(...)` function returning
   a `Plugin` implementation.
4. `MountWithAPI` declares the surface and offers the supply:
   - additive surfaces via `api.Register*` (tools, commands, routes,
     panels, prompt sections, channels, emitters, lifecycle handles);
   - a service other consumers resolve by key via
     `api.Offer("<key>", svc)` - one owner per key;
   - a need the loop fills? offer under a need key (`provider`,
     `store`, `logger`, ...) - the bind asserts your shape at boot
     and dies loud naming the need on a mismatch.
5. Consume other capsules through your OWN local interface: declare
   the few methods you call, resolve their key and type-assert
   (`ctx.Services().Resolve("memory").(memoryStore)`, the cron
   capsule's shape), nil-handle absence. Deleting the owner capsule
   leaves your need statement plus nil handling - nothing else. Never
   rely on a substrate-named type; never import a consumer of yours.
6. Subscribe to events (core/events.go) the loop raises per turn -
   the notifications and continuous capsules are the reference.
7. Register the plugin in `buildPlugins()` in `cmd/omega/main.go` -
   an unregistered plugin never mounts.
8. Leave `<name>_test.go` behind - each non-trivial package carries
   a self-check.

Never create an `AGENTS.md` for a module - the root `AGENTS.md` is
the single worker map; worker-facing rules for a new module land in
the root map's module laws and in `docs/architecture.md`.

### Module-owned UI

UI for module X lives in `harness/X/ui/`; shell components live in
`webui/app/components/<area>/`; components only move to a module
when the module owns the domain. The registration is a directory: put
top-level `.vue` components there (e.g. the tools module's
`ui/ToolsList.vue`) and the webui build (webui/nuxt.config.ts)
discovers them - no manifest, no registry. Each file becomes one panel
in the shell's inspector, and the module name is the namespace prefix,
so module and shell component names never collide. A module's UI is
tested there and reviewed as part of the module.

Card t_617c2ad0: modules the shell hosts as DOCK TABS - not inspector
panels - also live here (the tab capsules: `harness/terminal/ui/`,
`harness/browser/ui/`, `harness/notifications/ui/`). A tab module
ships exactly ONE .vue view; the build carries it in
`module-tabs.generated.ts` (the shell's dock outlets bind from that
manifest - never a hardcoded shell import), the composition decision
(which modules are tabs, their order and pinned roots) stays the
shell's in `dockables.ts` (`TAB_MODULES`).

Example:

```go
package myext

import (
    "agent"
    "core"
)

type Plugin struct{}

func (p *Plugin) Name() string       { return "my-module" }
func (p *Plugin) Provides() []string { return []string{"tools"} }
func (p *Plugin) Requires() []string { return nil }
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
    // declare: additive rails
    if err := api.RegisterTool("my-module.ping", myTool{}); err != nil {
        return err
    }
    // offer: a service consumers resolve by key
    return api.Offer("my-service", p.service{})
}

func NewPlugin() *Plugin { return &Plugin{} }
```

A consumer elsewhere reads it back through its OWN face - no import,
no shared type:

```go
// somewhere else: myservice consumer (its own private interface)
type service interface {
    Do(string) string
}
svc, _ := ctx.Services().Resolve("my-service")
if s, ok := svc.(service); ok {
    _ = s
}
```
