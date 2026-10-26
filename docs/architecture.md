# Architecture

```txt
webui (Nuxt SPA, embedded) -> HTTP channel -> agent (loop + tools) -> ai (provider streaming)
```

| Layer    | Package     | Responsibility                                                                                                                                          |
| -------- | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Core     | `core`      | Zero-behavior contract layer: message vocabulary, seam interfaces (22 of them), Context, canvas panel types, captioner, kanban task vocabulary, universal-mount vocabulary (`api.go`: ShapeCheck, Refusal/RefusalSink, ModulePanicError) |
| Agent    | `agent`     | Multi-turn loop machinery, Plugin system (Context alias, MountAll/MountAllAPI, RouteConfigs), the ExtensionAPI registration surface (dual mount, v0.12 phase B), TurnSink seam, canvas runtime, insights/caption/compaction helpers   |
| Provider | `ai`        | The `ai.Provider`/`ai.ToolSchema`/`ai.ModelInfo` aliases of the core contract (the module-boundary address), HTTP client, retry, think-level ladder, image sniffing |
| CLI      | `cmd/omega` | Entry point, TUI, project context, trust gate, config loading (YAML + env + defaults), hot-reload                                                       |
| Modules  | `modules/`  | In-process Go packages: agent_loop, provider, store, datadir, skills, compactor, logging, memory, prompt, tools, mcp, delegate, web, http_channel, tui, trust, kanban, notifications, verdict (19 plugins) + `datadir` (imported leaf) + `ui`, the Nuxt webui source |

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
modules/          In-process module packages (21 packages: 19 plugins +
                  datadir, an imported leaf, + ui, the webui SPA; compiled
                  into omega)
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
  skills/         Skill files
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

## Universal mount (v0.12 phase B)

The pi-style registration surface lives beside the classic seam
assignment; both mount styles mount ALL capsules simultaneously and
produce the same Context. `agent.APIPlugin` is the optional capability
a plugin implements (like Describer) to mount through the typed
`agent.ExtensionAPI` view instead of writing Context slots by hand:

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

The seam census (23 Context surfaces) and its registration mapping
live in the header of `agent/extensionapi.go`; the vocabulary types
(ShapeCheck, Refusal, RefusalSink, ModulePanicError) live in
`core/api.go`. Hosts that consume registrations call
`agent.MountAllAPI` (it returns the registry: claims, module routes,
the refusal ledger); `agent.MountAll` keeps its classic signature and
every existing host works unchanged. Capsules migrate in phase C;
the module authoring recipe is unchanged until then.
