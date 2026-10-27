# Architecture

```txt
webui (Nuxt SPA, embedded) -> HTTP channel -> agent (loop + tools) -> ai (provider streaming)
```

| Layer    | Package     | Responsibility                                                                                                                                          |
| -------- | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Core     | `core`      | Zero-behavior contract layer: message vocabulary, seam interfaces (22 of them), Context, canvas panel types, captioner, kanban task vocabulary, universal-mount vocabulary (`api.go`: ShapeCheck, Refusal/RefusalSink, ModulePanicError) |
| Agent    | `agent`     | Multi-turn loop machinery, Plugin system (Context alias, MountAllAPI, RouteConfigs), the ExtensionAPI registration surface (the universal mount, v0.12), TurnSink seam, canvas runtime, insights/caption/compaction helpers   |
| Provider | `ai`        | The `ai.Provider`/`ai.ToolSchema`/`ai.ModelInfo` aliases of the core contract (the module-boundary address), HTTP client, retry, think-level ladder, image sniffing |
| CLI      | `cmd/omega` | Entry point, TUI, project context, trust gate, config loading (YAML + env + defaults), hot-reload                                                       |
| Modules  | `modules/`  | In-process Go packages: agent_loop, provider, store, datadir, skills, compactor, logging, memory, prompt, tools, mcp, delegate, web, http_channel, tui, trust, kanban, notifications, verdict, continuous (20 plugins) + `datadir` (imported leaf) + `ui`, the Nuxt webui source |

No layer skips another. Events are typed structs, dispatched via type
switch. The provider layer emits events on a channel. The agent layer
consumes them and runs the tool loop. The HTTP channel exposes
everything over HTTP and serves the embedded webui SPA (built by
modules/ui/scripts/build.sh, committed in http_channel/static/,
baked in at compile time).

## Project Structure

```txt
cmd/omega/        Single binary entry point (serve, run, health, chat, update)
cmd/omega/examples/  Config templates, embedded for first-run bootstrap
core/             Zero-behavior contracts: message vocabulary, seam interfaces,
                  Context, canvas panel types, captioner, kanban vocabulary
ai/               Provider contract (ai.Provider aliases core.Provider), retry,
                  HTTP client, think-level ladder, image sniffing
agent/            Multi-turn loop machinery, Plugin system, TurnSink seam,
                  canvas runtime, insights/caption/compaction helpers
modules/          In-process module packages (22 packages: 20 plugins +
                  datadir, an imported leaf, + ui, the webui SPA; compiled
                  into omega)
  modules/continuous  Continuous-agent machinery: agent homes (folder-as-
                  registration under agents/), wake subprocesses, the
                  canonical chat, the evidence ledger, the roster REST + webui pane
http_channel/static/  Built webui SPA (tracked, embedded via go:embed)
modules/ui        webui - Nuxt 4 SPA (see docs/webui.md); built output embedded into the binary
docs/             User documentation (cli.md, api.md, webui.md, tui.md,
                  configuration.md, architecture.md, troubleshooting.md, roadmap.md)
deploy/           Install artifacts (omega.service systemd unit)
.agents/          Commit conventions (COMMIT.md)
build.sh          Build script (Linux/macOS): vet + test + build
build.bat         Build script (Windows): vet + test + build
bin/              Build output + local runtime (gitignored; skills/ templates tracked)
  omega           Built binary
  config/         Config + trust state (config.yaml, trust.yaml, *.example seeds; legacy root trust.yaml still wins)
    config.yaml   Personal configuration (gitignored)
    trust.yaml    Trust state (gitignored)
  data/           Databases (boot migration moves legacy home-root dbs here)
    omega.db      Session database (gitignored)
    kanban.db      Task board database (gitignored)
  user.md/memory.md  Main persona files (the home root IS the main agent)
  skills/         Skill files
  agents/         Continuous-agent homes (folder presence = registration)
    <name>/
      config/     The agent's config.yaml layer + its own crons.yaml
      data/       The agent's stores (omega.db, notifications.db, kanban.db, ledger.db)
      skills/     The agent's own skills
      user.md     Persona file
      memory.md   The agent's memory
      <name>.log  The agent's wake log (stdout+stderr of wake children)
```

A released/installed copy is self-contained: the binary, config/,
omega.db, memory files, and skills live in one home directory - the
executable's directory for release-folder installs, or `~/.omega` when
the binary is installed into a PATH bin directory (`~/go/bin`,
`~/.local/bin`). `OMEGA_HOME` overrides both.

## Development

```bash
./build.sh        # vet + test + build to bin/ (or build.bat on Windows)
```

Each package includes test files with no external test framework -
just the Go testing package. Tests are deterministic via a fake
provider that scripts stream events.

## Universal mount (v0.12)

The registration surface is the ONLY mount path. Every capsule
implements `agent.Plugin`'s `MountWithAPI(ctx, api)` and mounts through
the typed `agent.ExtensionAPI` view; the classic seam-assignment
`Mount` and `agent.MountAll` retired in phase F (the adapters, the
dual-mount parity tests and the fail-loud stubs went with them):

- identity is a namespaced string the tenant half must match
  ("kanban.view"); the registry enforces one name one tenant (a
  cross-tenant claim is a boot error, core.ErrSeamForeignTenant), and
  same-tenant re-registration replaces (clean-idempotent remount)
- machine-facing names (tool names, panel action ids) carry the
  prefix; user-facing names (slash commands, route patterns) keep
  their natural shape and are owner-recorded
- payloads crossing a registration are raw JSON; the action pipe
  (the canvas action seam) carries the working boundary ShapeCheck -
  a failed check is a refusal in the ledger (module, seam, failed
  predicate), nothing else acts on it yet

The seam census (15 exclusive slots) and its registration mapping
live in the header of `agent/extensionapi.go`; the vocabulary types
(ShapeCheck, Refusal, RefusalSink, ModulePanicError) live in
`core/api.go`. Hosts call `agent.MountAllAPI` (it returns the
registry: claims, module routes, the refusal ledger) - every host
does, including the binary and the engines. Module REST rides
`core.ModuleRoute` declarations (`core/routes.go`) registered through
`RegisterRoute`; `http_channel/module_routes.go` serves exactly what
the live registry carries, so a disabled capsule's routes answer 404
and a re-enabled one's return, through the same RewireSeams re-read
every other served seam follows. Binary purity is machine law:
`scripts/nm-purity.sh` proves a capsule leaves the binary whole
(kanban canary every push, full rotation on the nightly).
