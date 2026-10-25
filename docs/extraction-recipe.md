# The omega extraction recipe (split-ready criterion c)

Status: DORMANT. Nothing below has fired. This is a recipe, not a
plan in motion — phase 7 stays dormant until Endo's word moves it.

Written 2026-10-25 against dev @ 2e9ae66. Every dir list and seam
count here was re-verified against the real build at that commit:
`ls modules/`, `buildPlugins()` (cmd/omega/main.go:308), the seam
declarations in the module plugins, and the seam inventory in
core/seams.go + agent/turnsink.go (+ canvas, caption). Numbers that
differ from older notes are corrected here, not inherited.

## 0. What the split is

One repo becomes two:

- The **engine (omega)** — the model runtime: message vocabulary, the
  agent loop machinery, the seam contracts, the canvas runtime, the
  connector surfaces, the bare-minimum tools, and a minimal shell.
  Stays at `github.com/EndoTheDev/omega`.
- The **harness (TigerClaw)** — every opinionated capsule: the
  services and the opinions about how an agent's work is
  organized (boards, schedules, memory files, notifications, the
  provider choice, session persistence). New module path; a new repo.

The direction law already exists and is already enforced today:
`gate-deps.sh` fails the build if `ai/` or `agent/` (and transitively
anything below `core/`) imports `modules/...`, directly or
transitively. The extraction changes nothing about that law; it moves
the enforced line from "import rule inside one repo" to "one module
edge between two repos". Everything in this recipe keeps that
direction: the harness depends on the engine, never the reverse.

## 1. Which dirs move

### The engine side (stays in omega)

| Dir | What it is | Why engine |
|---|---|---|
| `core/` | Zero-behavior contract layer: message vocabulary, seam interfaces, Context, canvas panel types, captioner, kanban task vocabulary | stdlib-only by law (core/importability_test.go) |
| `agent/` | Multi-turn loop machinery, Plugin system (Context alias, MountAll, RouteConfigs, Describer, ConfigSource), TurnSink seam, canvas runtime, insights/caption/compaction helpers | imports core only (verified by gate-deps.sh today) |
| `ai/` | HTTP-client, retry, image sniffing, think-level ladder, the concrete `ai.Provider` streaming contract, fake provider | imports core only; imported by engine (agent_loop) and harness capsules alike — it must sit under the engine or the agent loop would reach upward |
| `modules/agent_loop` | The default LoopProvider implementation (the turn loop) | no opinions and no harness deps; rides the loop seam |
| `modules/tools` | The bare-minimum tools: `shell.run`, `files` (read/write/edit + batch), /files browser, db deny-guard | the north-star ruling; verdict tool status is separate (see §6) |
| `http_channel` | The web connector: SSE/REST transport for the canvas, channels, runs, board/verdict/settings HTTP shapes | the connector/canvas runtime per the ruling |
| `tui` | The terminal connector: the TUI shell, canvas pane rendering | frontend seam implementation on the engine side |
| `modules/ui` | The webui SPA source (Nuxt/TS, no Go) — embedded into http_channel's `static/` | part of the connector/canvas runtime |
| `cmd/omega` (the shell parts) | main.go, config.go, home.go wire the engine packages into the minimal shell | see HURT-4: today's shell is NOT yet minimal |

### The harness side (moves to TigerClaw)

Verified against `buildPlugins()` in cmd/omega/main.go (19 plugins,
order is data, not semantics — MountAll's topoSort is the only
ordering) plus the plugins' `Provides()` declarations:

| Dir | Plugin name | Provides | ConfigKey |
|---|---|---|---|
| `modules/store` | store | store, tools | store |
| `modules/kanban` | kanban | kanban, tools | kanban |
| `modules/cron` | cron | cron | (none) |
| `modules/memory` | memory | memory, tools, prompt_sections | memory |
| `modules/delegate` | delegate | tools | (none) |
| `modules/mcp` | mcp | tools | (none) |
| `modules/web` | web | tools | web |
| `modules/notifications` | notifications | notifications | notifications |
| `modules/trust` | trust | trust | trust |
| `modules/compactor` | compactor | compactor | compactor |
| `modules/prompt` | prompt | prompt_builder | (none) |
| `modules/logging` | logging | logging | logging |
| `modules/provider` | provider | provider | provider |
| `modules/skills` | skills | skills, tools, prompt_sections | skills |

That is 14 capsules. (Older notes say 13 and omit skills; the
registration above is the list that compiles, skills included.)

`modules/verdict` (verdict, tools, prompt_sections — ConfigKey none)
is PENDING: the ruling leaves the verdict tool's home unresolved
(§6). Today it sits in buildPlugins between tools and mcp.

### Not a capsule, moves with the store capsule

- `modules/datadir/` — a no-import leaf (OMEGA_HOME resolution, db
  path layout, `datadir.Migrate`). Five capsules import it today:
  store, kanban, notifications, tools, cmd/omega. Because the
  bare-minimum tools (engine) and the store capsule (harness) both
  need it, its placement is a real decision, not mechanical — see HURT-3.

### The cross-check (paste)

`ls modules/` at 2e9ae66, alphabetical:

    agent_loop compactor cron datadir delegate http_channel kanban
    logging mcp memory notifications prompt provider skills store
    tools trust tui ui verdict web

buildPlugins() registers, in authoring order: logging, agent_loop,
provider, store, skills, compactor, memory, notifications, prompt,
tools, verdict, mcp, delegate, web, http_channel, tui, trust, cron,
kanban — 19.

Difference, accounted for: `datadir` and `ui` are not plugins
(datadir is an imported leaf; ui is the SPA), and `modules/README.md`
is prose. 21 dirs + README = 19 plugins + 2 non-plugins. The table
above and the registration agree one-for-one.

### The verified seam inventory: 22

core/seams.go holds 19 seam interfaces (line numbers at 2e9ae66):

    LoopProvider 13, CompactionProvider 44, ToolProvider 53,
    StoreProvider 69, AgentSessionStore 155, SkillsProvider 170,
    LoggerProvider 177, MemoryProvider 190, PromptBuilder 216,
    PromptSectioner 232, Channel 246, Settings 294, CronProvider 324,
    CronLister 361, SkillsLister 370, VerdictsProvider 430,
    Frontend 480, TrustProvider 488, NotificationsProvider 512

plus three seams that live on their own because the run loop and the
rendering surfaces need them: `TurnSink` (agent/turnsink.go:17),
`PanelProvider` (core/canvas.go:166, the canvas registration seam),
`Captioner` (core/caption.go:20). 19 + 3 = 22.

`agent/plugin.go:84` declares 14 of them exclusive (two providers
conflict); `tools` is additive. The split does not change a single
one of these declarations — they are already layer-blind, which is
precisely why the recipe can be mechanical.

## 2. What each side's go.mod says

Engine (the only go.mod today is at repo root):

    module github.com/EndoTheDev/omega

After the split the engine keeps this path and this go.mod, minus the
deps that belonged to moved capsules. `go mod tidy` after the move
prunes: sqlite (`modernc.org/sqlite`, imported by store only),
`robfig/cron/v3` (cron), `gen2brain/beeep` + toast (notifications).
It keeps yaml.v3 as long as the engine shell parses YAML config
(cmd/omega/config.go does today) and the charm suite + termenv as
long as the TUI stays engine-side.

Harness:

    module github.com/EndoTheDev/tigerclaw   // name is a split-day choice

The ONE dependency line on the engine:

    require github.com/EndoTheDev/omega v0.11.0

During the afternoon of the split, before the engine tag exists, ride
a replace instead of a tag:

    replace github.com/EndoTheDev/omega => ../omega

and drop the replace once the engine publishes its next tag. The
dependency line never says anything more: the whole harness→engine
surface is the three packages in §3, so a single `require` carries it.
The harness inherits the rest of the deps (sqlite, cron scheduler,
beeep) through its own moved capsules when `go mod tidy` runs on the
harness side.

ai/: nothing "lives" there by accident. After the split ai/ stays
engine-side exactly as it is (imports core + stdlib only); the
provider capsule (harness) and the loop (engine) both keep importing
it. No file in ai/ moves. `core/message_types.go:277` documents why
MessageRole had to land in core rather than ai — that history stays
put too.

## 3. The module-boundary API

The harness is ALLOWED to import exactly three engine packages. Each
is already layer-clean (imports core + stdlib only), so no engine
file needs to move for this surface to hold:

- **core** — the contracts: every seam interface (§1, 22 of them),
  `Context` (core/context.go, the service container plugins mount
  into), message vocabulary (`Message`, `System`, `Assistant`,
  `ToolResult`, `MessageRole`, messagetree), `Agent`/`NewFromContext`
  (core/agent.go) + `AgentOptions`, `Tool`/`ToolAction`
  (core/agent.go:12), canvas panel vocabulary (`Panel`,
  `PanelAction`, `ViewModelDelta`, `ChatPayload`), `ExtensionInfo`,
  `Insights`, `Session`/`ActiveView`, kanban task vocabulary
  (core/kanban.go:11).
- **agent** — the wiring: `Plugin` (agent/plugin.go:19, the contract
  every capsule implements), `Describer`/`ConfigSource`
  (agent/plugin.go:42,51), `RouteConfigs` (agent/plugin.go:61),
  `MountAll` (agent/plugin.go:106), `Context = core.Context`
  (agent/plugin.go:15), the TurnSink seam with `WithTurnSink` /
  `RunTurnSink` / `WithPersistInput` (agent/turnsink.go), session
  ctx helpers (`WithSessionID`, `SessionIDFromContext`, `WithDeniedTools`)
  (agent/sessionctx.go), the canvas runtime (agent/canvas.go),
  `FormatInsights` (agent/insights.go), caption helpers
  (agent/caption.go — aliases of core types).
- **ai** — the runtime plumbing: `ai.Provider` (the module-boundary
  address of the provider contract — a Go alias of the same interface,
  `type Provider = core.Provider`; not a second streaming contract),
  `ai.ToolSchema`,
  `ai.RetryHTTP`, thinking ladder helpers, image magic/type sniffing,
  and the fake provider for harness tests.

Forbidden to the harness: importing anything else of the engine
(notably `modules/...` after the move, `cmd/...` — nothing there is
part of the surface). Forbidden to the engine: every harness
package. Both directions are the same dep law the gate already
checks; post-split it becomes two go.mod edges instead of an import
rule.

## 4. The recipe steps (an afternoon, ordered, mechanical)

The split fires as one series of moves. Each step's success has a
compiler-checkable end state.

1. **Create the harness repo and go.mod.** New repo, `module
   github.com/EndoTheDev/tigerclaw`, the one `require` line plus
   `replace ../omega` (§2). Empty otherwise.
2. **Move the 14 harness capsule dirs.** `git mv modules/<x>
   <tigerclaw>/modules/<x>` for store, kanban, cron, memory,
   delegate, mcp, web, notifications, trust, compactor, prompt,
   logging, provider, skills. Nothing inside a capsule dir needs to
   change at this step.
3. **Move the capsule wiring out of the shell.** In the harness repo,
   write its main: the full 19-plugin `buildPlugins` list (today's,
   cmd/omega/main.go:308, minus any PENDING decisions from §6),
   `agent.RouteConfigs`, `MountAll`, and the `ConfigSection` adapter
   body (cmd/omega/config.go:324-355) adapted to the two-module
   world: the shell's yaml structs stay on the engine side, the
   section-adapter switch constructs the harness `Config` types per
   §3's allowed surface. cmd/omega/kanban.go (the kanban CLI surface)
   and the sub-CLI commands whose only imports are store/kanban move
   with it. The engine's shell keeps main.go, config.go, home.go with
   the harness sections and files deleted, and HURT-4's split of
   main.go applied.
4. **Move `modules/datadir`** to the side HURT-3 decided (engine is
   the default; the move is one `git mv` either way — everything that
   imports it updates in step 5).
5. **Update imports inside moved capsules.** Mechanical, per capsule:
   package paths change only where they referenced the old single
   module — `github.com/EndoTheDev/omega/modules/<x>` becomes
   the harness module path. Imports of `omega/core`, `omega/agent`,
   `omega/ai` are rewritten by the compiler pointing at the engine's
   require line (import lines stay literally the same path if the
   replace directive is a filesystem path — that is the point of
   replace). `gofmt -w` + `go build` in both repos is the check.
   Expected count: a handful per capsule; the tests that cross
   capsules (http_channel's, cron's) move with their own capsule.
6. **Split the remaining harness code in cmd/omega if any.** grep for
   any `omega/modules/...` still reachable from the engine build;
   every hit is either a HURT from §5 that this step resolves, or a
   finding to file, not a hedge. Target state is zero.
7. **Build and test both sides.** Engine: `go build ./...`,
   `go vet ./...`, `go test ./...`, plus `gate-deps.sh` (the dep law
   gate keeps working verbatim — it reads go list, not directory
   layout). Harness: the same four commands in the new repo.
8. **Run the journeys against the rebuilt harness binary.**
   scripts/journeys/run.sh with JOURNEY_BASE_URL pointed at a lane
   booted from the new binary. The journeys are the customer-level
   net: chat send/response, sessions reload, dock, verdict cards,
   notifications, module parity. All previously green journeys must
   stay green; a red one means the moved wiring dropped a seam.

Machine-checkable end state: in the engine repo,
`go list -deps ./... | grep tigerclaw` is empty AND in the harness
repo `go list -deps ./... | grep EndoTheDev/omega` contains exactly
the three packages of §3. Nothing else.

## 5. HURTS (named findings)

Places the recipe could not be written cleanly. Each is a finding,
not a hedge. None is fixed by this card except where noted.

1. **tui imports the store capsule for one helper function**
   — http_channel/tui.go:23 imports
   `github.com/EndoTheDev/omega/modules/store`; the only
   package-qualified use is `store.SessionDisplayName(...)` at
   tui.go:1525 (the other `store.X` greps are calls through the
   `core.StoreProvider` field at tui.go:305 — seam-clean). The helper
   (modules/store/commands.go:173-181) is an 8-line pure function.
   Trivial remedy, named here because the split itself must not fire
   first: move `SessionDisplayName` into core (the agent.FormatNumber
   pattern — a label/id presentation helper) or inline it in the tui;
   either is a one-file change, and then tui imports only core/agent
   /ai. This is the ONLY prod import edge from an engine connector
   into a harness capsule.

2. **tools reads the store capsule's config type**
   — modules/tools/plugin.go:9 imports the store capsule and :41 does
   `ctx.Configs["store"].(store.Config)` to build the db deny-guard
   (modules/tools/dbguard.go, ENDO'S LAW 2026-10-18). tools stays
   engine-side (bare-minimum tools, §1); store moves harness-side. An
   engine capsule reading a harness capsule's config type crosses the
   forbidden direction the moment the split fires. The mechanical
   remedy is small and belongs to split day: the deny-guard's trigger
   input is one string (`c.DBPath`) — route it through a ctx field or
   make the guard resolve the standard layout itself (it already
   does, at dbguard.go:52-56). Trivial + one line, recorded because
   the config-shape choice (whose `db_path` field feeds the guard)
   is Endo's to settle at split time.

3. **datadir is a cross-side shared leaf**
   — modules/datadir imports nothing and is imported (prod) by
   modules/store/plugin.go:10, store_impl.go:17,
   modules/kanban/plugin.go:12 + boards.go:22 + store.go:16,
   modules/notifications/plugin.go:10 + notify.go:24, and
   modules/tools/dbguard.go:9 + :52,56 — plus cmd/omega. Three of
   those capsules are harness; one is engine; so the leaf cannot move
   "with its capsule": there is no single side. Remedy on split day:
   keep it in the engine and add it to the §3 allowed surface (it is
   already stdlib-only and opinion-free), or duplicate its 2 helpers
   per side. Default for the recipe: engine-side.

4. **cmd/omega is not the minimal shell yet**
   — the ruling gives the engine side a minimal shell; today's shell wires all
   19 plugins (main.go:308-351), hand-routes every capsule's config
   (config.go:324-355, a hand-written adapter that constructs each
   capsule's `Config` value), and carries harness CLI surfaces
   (kanban.go imports the kanban capsule; export.go and insights.go
   import the store capsule; settings.go rides the compactor). Step 3
   of the recipe splits this mechanically, but the split point is
   "every file whose only engine role is wiring a harness capsule" —
   a per-file judgement, not a dir move. Named so the afternoon does
   not pretend cmd/omega is a clean `git mv`. Also the reason step 6
   exists. The engine-only binary after this split runs core+agent
   +ai+tools+connectors with the provider capsule absent — the model
   transport arrives with the harness assembly. If that reading of
   the ruling is wrong, the dir lists are unaffected; only step 3's
   file split changes.

5. **the section-adapter switch is a duplicate of every capsule's
   config shape** — cmd/omega/config.go:324-355 constructs
   `store.Config{DBPath: ...}` etc., while each capsule carries its
   own `modules/<x>/config.go` with the same fields. Not a boundary
   break (the shell may know everything; see §3) — but it is the one
   place where "moved a capsule" is not enough: step 3 must rewrite
   every case in the switch. Listed so the step's real size is
   visible: 10 cases, mechanical.

No other prod import edge crosses the capsule/engine line today: the
engine layers (core, agent, ai) import no module at all (go list
verified, gate-deps.sh enforces), config routing is by name without
type coupling except finding 2 (RouteConfigs → ConfigSection → the
capsule types), and no two capsules share a file (datadir is a shared
PACKAGE — separate directories, no file-level sharing anywhere).

## 6. Pending verdicts (not hedges)

- **The verdict capsule.** buildPlugins registers
  `verdict.NewPlugin()` (verdicts seam, verdict tool, /verdict TUI
  command; requires store + logging seams). The ruling names
  read/write/exec as the bare-minimum tools "pending verdict" — the
  tool is either the fourth member of the engine's minimal toolset or
  a harness capsule on the TigerClaw side; undecided. The recipe
  carries it both ways: buildPlugins' 19th entry moves or stays per
  the ruling's answer, and NO other step in the list changes
  (nothing imports `omega/modules/verdict` outside cmd/omega
  wiring; tests only). core/seams.go's VerdictsProvider is a seam
  contract and stays in core regardless of where the implementation
  lands.
- **The production assembly's home.** Per the ruling the harness
  takes "one dependency line on the engine", which makes the
  TigerClaw main the production assembly (§4 step 3). The engine's
  minimal shell then exists as the engine-only developer binary. If
  Endo's ruling instead keeps the production main on the engine side,
  steps 3 and 6 change shape (the harness would ship capsules for
  that main to import) but every other step and every HURT stands
  unchanged.

Both pendings are named where they affect the steps; neither is
resolved here. This card's deliverable is the boundary as it stands
at 2e9ae66 — v0.12 continues from there.