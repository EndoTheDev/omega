# Architecture

```txt
webui (Nuxt SPA, embedded) -> HTTP channel -> agent (loop + tools) -> llm (provider streaming)
```

| Layer    | Package     | Responsibility                                                                                                                                          |
| -------- | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Core     | `core`      | Zero-behavior contract layer: wire vocabulary (`QuestionPrompt`/`QuestionAnswer`, `Notice`), channel + mount machinery interfaces, the `Services` registry, Context, canvas panel types, captioner, universal-mount vocabulary (`api.go`: Refusal/RefusalSink, ModulePanicError); zero capsule nouns (the noun tests) |
| Agent    | `agent`     | Multi-turn loop machinery + the loop's need-face declarations (`agent/seams.go`), Plugin system (Context alias, MountAllAPI, RouteConfigs), the ExtensionAPI registration surface (the universal mount, v0.12), TurnSink seam, canvas runtime, insights/caption/compaction helpers   |
| LLM      | `llm`       | Shared provider infra: SSE streaming client, retry, the think-level ladder, image sniffing; the core wire-shape aliases (`llm.ToolSchema`, `llm.ModelInfo`) - it carries no provider contract (consumers declare their own faces) |
| CLI      | `cmd/omega` | Entry point, CLI loop + setup, project context, trust gate, config loading (YAML + env + defaults), hot-reload                                                       |
| Modules  | `harness/`  | In-process Go packages: provider, store, datadir, skills, compactor, logging, memory, prompt, tools, mcp, delegate, cron, verdict, approval, continuous, notifications, process, terminal, browser, kanban, trust + `datadir` (the imported leaf) |

No layer skips another. Events are typed structs, dispatched via type
switch. The provider layer emits events on a channel. The agent layer
consumes them and runs the tool loop. The HTTP channel exposes
everything over HTTP and serves the embedded webui SPA (built by
webui/scripts/build.sh, committed in api/static/,
baked in at compile time). Mutating routes ride the Host/Origin guard
(t_b52d09f3): a write from a foreign origin answers 403. Config and
capsule writes are atomic - a temp file plus rename, so a killed
process never leaves a half-written yaml - and shared toggle state
sits behind one mutex.

## Project Structure

```txt
cmd/omega/        Single binary entry point (serve, run, health, chat, update)
cmd/omega/examples/  Config templates, embedded for first-run bootstrap
core/             Zero-behavior contracts: wire vocabulary (QuestionPrompt,
                  Notice), channel/mount machinery, the Services registry,
                  Context, canvas panel types, captioner
llm/               Shared provider infra (SSE client, retry, think-level
                  ladder, image sniffing); wire-shape aliases only - no
                  provider contract (consumers declare their own faces)
agent/            Multi-turn loop machinery + the loop's need-face
                  declarations (agent/seams.go), Plugin system, TurnSink
                  seam, canvas runtime, insights/caption/compaction helpers
harness/          In-process module packages (21 packages: 20 plugins +
                  datadir, an imported leaf; every capsule removable -
                  compiled into omega)
  harness/continuous  Continuous-agent machinery: agent homes (folder-as-
                  registration under agents/), wake subprocesses, the
                  canonical chat, the evidence ledger, the roster REST + webui pane
api/static/  Built webui SPA (tracked, embedded via go:embed)
webui        webui - Nuxt 4 SPA (see docs/webui.md); built output embedded into the binary
docs/             User documentation (cli.md, api.md, webui.md,
                  configuration.md, architecture.md, troubleshooting.md)
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
- payloads crossing a registration are raw JSON; the canvas action
  seam refuses unknown modules or action ids at dispatch - module
  handlers own their payload shapes (binary purity), the ledger
  (module, seam, failed predicate) records toggle-level refusals

The registration census and its mapping table live in the header of
`agent/extensionapi.go`; the vocabulary types
(Refusal, RefusalSink, ModulePanicError) live in
`core/api.go`. Hosts call `agent.MountAllAPI` (it returns the
registry: claims, module routes, the refusal ledger) - every host
does, including the binary and the shell. Module REST rides
`core.ModuleRoute` declarations (`core/routes.go`) registered through
`RegisterRoute`; `api/module_routes.go` serves exactly what
the live registry carries, so a disabled capsule's routes answer 404
and a re-enabled one's return, through the same RewireSeams re-read
every other served seam follows. Binary purity is machine law:
`scripts/nm-purity.sh` proves a capsule leaves the binary whole
(kanban canary every push, full rotation on the nightly).

## The seam constitution (part 3)

One universal seam mechanism; the substrate follows the capsules'
grammar. At mount every extension runs the same moves:

```txt
mount -> declare surface (the additive rails, api.Register*) ->
offer services under a key (api.Offer) -> consume others' services
by resolving the key and asserting the consumer's OWN local
interface -> subscribe to events.
```

core carries zero domain or capsule nouns: wire types (the shapes
every consumer and the serving surface marshal identically), mount
machinery, the `Services` registry. Deleting a capsule deletes every
owner-shaped type, register and consult site with it; the consumer
keeps its own need statement (a private local interface) plus
nil handling. Offers ride the same one-name-one-tenant law as every
claim: a key held by another tenant is a boot error
(`core.ErrSeamForeignTenant`, `core/registry.go`). Import law
unchanged: harness -> agent -> llm -> core.

### The need-key namespace (the closed list, 8)

The loop machinery declares its needs in `agent/seams.go` (Go
implicit interfaces - the interface belongs to the consumer), and
resolves + asserts every need at bind: a missing supply is the
documented floor, a wrong-shaped offer is a loud boot failure naming
the need. Never a silent nil discovered mid-turn, never a lazy
resolve. Capsules offer supplies under these keys; proposing a ninth
is a design event, argued in a card:

| Key         | The need                        | Supplying capsule               |
| ----------- | ------------------------------- | --------------------------------|
| `provider`  | the model client                | `harness/provider`              |
| `store`     | session persistence             | `harness/store`                 |
| `loop`      | the multi-turn loop             | `agent/loop` (the default; a capsule may replace it) |
| `logger`    | operational logging             | `harness/logging`               |
| `compactor` | context compaction              | `harness/compactor`             |
| `prompt`    | system prompt + guidelines      | `harness/prompt`                |
| `nudger`    | the learning-loop reminder      | `harness/skills`                |
| `approvals` | the smart approval gate         | `harness/approval`              |

Floor detail: `provider` refuses the boot upstream (the unconfigured
host posture). The host binds `store` itself (`cmd/omega/main.go`,
newAgent): a wrong shape is the loud named-need boot failure, and a
MISSING store capsule makes the host offer its own in-memory fallback
under the same key ("host" tenant, sessions will not persist). The
rest degrade to nil-safe floors: no logging, no compaction (context
overflow surfaces a friendly error), no system prompt, no nudger, no
gate.

### The capsule-service namespace (the closed list, 7)

`skills`, `memory`, `trust`, `cron`, `notifications`, `verdicts`,
`chat-runs` - services one capsule (or, for `chat-runs`, the serving
shell - the first shell-offered key, Endo ruling 2026-11-04) owns and
others (capsules, the serving surface, the host) consume: resolve the
key, assert to the consumer's own 1-2 method local interface (e.g. the
host's `trustResolver` in `cmd/omega/trust.go`, the store capsule's
`ChatRunsReader` in `harness/store/routes.go`), nil-handle absence.
The owner never knows its consumers; deleting it removes the service
whole. The census flags each mounted key: `core/census.go`'s slot list
(the 15 keys above, plus the host-built captioner - not an offer).
`harness/kanban`'s `"kanban"` Provides() string is a mount-order
claim only; nothing is offered under it.

### Enforcement

- `agent/seams_test.go`: the substrate noun scan (core/ + llm/ carry
  no capsule name - comments and string keys never trip, the scan is
  `go/ast` identifier-level), the consumer-home pin (each need face
  declared in `agent/seams.go`), the deletability scan (the Class B
  owner faces dead in core, llm, agent, api, cmd), and the
  closed-list scan (every `api.Offer` call site inside the 8 + 6,
  and every need key actually supplied).
- `core/importability_test.go`: the substrate's own leg - no capsule
  noun in core/, beside the stdlib-only import law. If a name
  returns, the recipe's stop signal fires in the test, not in
  production.
- `scripts/nm-purity.sh`: the same removal at binary level (kanban
  canary every push, full rotation on the nightly).

The residual trace (the honest ceiling of the grammar): a consumer's
resolve call keeps the string key - one inert, nil-handled data
trace per consumer. Strings are data, not type-system vocabulary.

## The shell-ring census (shrink wave 3)

The executable sentence (Endo ruling 2026-11-04, ADR
Workshop/omega-shell-ring-shrink.md): **remove every capsule and what
remains is a usable agent with a chat face.** `agent/api` is the
serving shell's BASE set - chat + canvas + settings - and carries no
capsule opinion surface. Every production file there carries a census
verdict; the table below IS the verdict table, pinned in
`agent/api/api_census_test.go` (a new production file either gets a
verdict or moves to its feature's capsule's `routes.go` through
`api.RegisterRoute`). Every wave-1/2/3 mover carries a tombstone in
the same test - the moved files cannot come back.

Verdict principle: serves chat + canvas directly = base; everything
else = opinion, moves with its capsule. Trivial opinion surface with
no capsule home rides the closest capsule (disclosed, not invented
into a new package).

| File | Verdict |
| ---- | ------- |
| `chat_panel.go` | chat - the composer panel + the /chat turn handling |
| `runs.go` | chat - the live run: the RunHub, detached runs, the SSE run frames + the store floor guards |
| `server.go` | chat - the chat floor: /models, /chat, the session-existence floor (POST /sessions, /sessions/{id}), /tools, the command palette, canvas wiring |
| `server_atmention.go` | chat - the @-mention expansion on the submit path (the browser never expands; /chat does) |
| `server_canvas.go` | canvas - the transport: GET /ui/canvas snapshot + POST /ui/actions, deltas fan out on the run streams |
| `server_commands.go` | chat - the command palette's server surface (GET /commands + POST /commands/dispatch, the composer's slash-command face over the universal registration seam) |
| `server_ephemeral.go` | chat - ephemeral sessions: the no-persist chat mode of /chat (in-memory, eph- ids, gone at restart) |
| `server_images.go` | chat - image serving + /attach-image (chat renders images) |
| `server_settings.go` | settings - the third shell-ring face (GET/PUT /settings) |
| `server_transcript.go` | chat - the transcript serving (live + store-backed restore) |
| `server_variants.go` | chat - message variants on the live run (regenerate/branch selection) |
| `web_tools.go` | chat - the web.search/web.fetch tool pair: built-in shell vocabulary since v0.13 (shell law 1), always mounted |

The movers' tombstones (the capsule their surface rides, the wave that
moved it):

| File | Rides | Wave |
| ---- | ----- | ---- |
| `server_files.go` | harness/files (tool + explorer endpoints) | t_48eeb015 |
| `server_sessions.go` | harness/store (the session MANAGEMENT surface) | t_490fed88 |
| `server_branch.go` | harness/store (/sessions/{id}/branch) | t_490fed88 |
| `server_insights.go` | harness/store (GET /insights) | t_490fed88 |
| `server_agents.go` | harness/continuous (the roster) | t_490fed88 |
| `server_workspaces.go` | harness/files (the workspaces picker) | t_b75df168 |
| `server_verdicts.go` | harness/verdict (the verdict answer surface) | t_b75df168 |
| `server_notify.go` | harness/notifications (the finished-things face) | t_b75df168 |
| `server_skills.go` | harness/skills (the skills content view) | t_b75df168 |

Machine-enforced in the same gate as the webui noun check:
`agent/api/api_census_test.go` (the pin + the tombstones + the
production-filename noun scan) and
`webui/test/capsule-noun.test.ts` (capsule names must not appear under
`webui/app/components/`, beside the agent/api production-filename walk
- `/tools` is base vocabulary there: the inventory endpoint serves the
chat loop).

## The three-layer sessions law (shrink wave 2)

A session's surface exists in exactly one of three layers:

1. **The chat floor** - `agent/api` base (the census above): session
   EXISTENCE. POST /sessions (create + read-back), /sessions/{id}
   detail/delete, the transcript the restore path serves, the
   ephemeral registry. Everything a live chat touches.
2. **The management face** - the store capsule: the roster
   (list/search/pin/archive/read-stamps/counts), /sessions/search,
   /sessions/{id}/branch, /sessions/{id}/export, GET /insights. List-
   row concerns never touch `updated_at`.
3. **The host's default supply** - when no store capsule offers the
   `store` seam, the host opens `<home>/data/omega.db` itself
   (`openDefaultSupply` in cmd/omega/main.go; never :memory:) and
   offers the same handle back under `store`/`host`: a fresh chat
   boots and persists, only the management face is gone. In the
   every-capsule-removed proof build the sqlite driver rides the
   capsule package out entirely, so the fallback is the host's
   in-memory proof supply (`cmd/omega/proofstore.go`, written by
   scripts/proof-strip.py): the chat face answers, persistence is the
   store capsule's opinion and rides out with it - that is the proof
   script's PASS line.
