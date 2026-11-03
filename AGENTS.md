# AGENTS.md - omega

## Purpose

omega is a Go port of the Pi/Tau event-stream agent architecture.
Three layers, one job each. Events are the contract. The whole thing is
readable as a textbook.

This is a fresh implementation, not a port of agent.d. No RSI, no
self-awareness, no evolution tracking. Just a clean event-stream agent
in Go.

This file is the single worker map. There is no other AGENTS.md:
per-module files are consolidated into this map and into `docs/`
(Endo ruling, 2026-10-21). A new AGENTS.md anywhere outside the root
is a review finding; its content lands in its mapped home instead.

## Ownership

- **Repository:** `github.com/EndoTheDev/omega` (development checkout: `~/dev/omega`; installed instance: `~/.omega`)
- **Language:** Go 1.26.5
- **Module:** `github.com/EndoTheDev/omega`
- **Dependencies:** Added when a layer requires them, not before.
  Prefer the standard library. Prefer a dependency already in `go.mod`.
- **Entry point:** `omega` (single binary: `serve`, `run`, `health`,
  `chat`, `export`, `update`, `insights`, `rollback`, `setup`;
  no arg -> the CLI agent loop (`cmd/omega/loop.go`; unconfigured
  refuses exit 1 pointing at `omega setup`), `<path>` -> chdir +
  loop). CLI reference and law:
  `docs/cli.md`.

## Never-violate rules

- **No layer skipping.** Each layer imports only from the layer
  directly below it. `chat` imports internal packages for in-process
  streaming; `serve` exposes everything over HTTP. External clients
  talk to the HTTP channel only.
- **No re-exports at intermediate layers.** If a type is defined in
  a layer, consumers import it from that layer.
- **`model_name` everywhere.** Provider references use `model_name`,
  not `model` or `provider_model`.
- **Tool errors are structured returns.** Tools return `(string, error)`;
  the error becomes an `IsError` tool result message. No panic into
  the agent.
- **No backwards compatibility.** Breaking changes are normal. They
  are not marked with `!` in commit messages (see `.agents/COMMIT.md`).
- **Secrets never committed.** `.env` and the home `config/*.yaml`
  files are gitignored.
- **Extensions are in-process.** All extensions are Go packages under
  `harness/`. No stdio, no IPC, no runtime loading. The host calls
  `MountAllAPI` at startup - the universal registration surface is the
  only mount path; the agent loop reads from `Context`.
- **The agent loop is the only place tools execute.** The HTTP channel
  and the CLI loop route tool calls through the agent, never directly. Each
  run drains a fresh events channel (channel reuse panics on the
  second write).
- **Bind loopback only by default.** The HTTP channel is unauthenticated
  and `/chat` reaches the shell tools. Anything beyond loopback is an
  explicit `server.bind` opt-in behind an auth wall.
- **One map.** A new AGENTS.md outside this root is a review finding;
  move its content into its mapped home.

## Module laws

Terse operational rules. Layer references live in `docs/` (map below);
`harness/README.md` is the extension authoring guide.

- **Plugin contract.** Every extension implements `agent.Plugin`
  (`Name`, `Provides`, `Requires`, `MountWithAPI`). Exclusive seams take
  one plugin per slot (`provider`, `compactor`, `store`, `skills`,
  `loop`, `logging`, `memory`, `prompt_builder`, `captioner`,
  `frontend`, `trust`, `cron`, `kanban`, `notifications`, `verdicts`).
  15 exclusive slots; `kanban` is a seam claim only (no Context field).
  `tools` and `channel` are additive. Commands register via
  `ctx.Commands` + `ctx.CommandHandler` (chain the previous handler on
  miss); a handler returns `CommandResult` with optional `CmdAction`
  directives - the host interprets actions, extensions declare intent.
- **Mount order.** Mount order is declared data: an extension whose
  `Mount()` reads a seam another plugin owns needs a `Requires()` edge
  naming that seam (delegate, kanban and cron require `notifications`).
  `MountAllAPI`'s topoSort is the only ordering - slice position in
  `buildPlugins()` is a stable tiebreak (prompt-section authoring order
  among independent plugins) and carries no dependency semantics.
  Comment-law ordering is retired (card t_13a2af8a).
- **Goroutine fence.** Modules register Start/Stop lifecycle handles on
  Context.RunGroup; Mount never starts background work. The host calls
  Start after the mount commit; StopAll walks handles in reverse. A
  blocking Start is a fence violation.
- **Import law.** Modules import `core/` for contracts (types, events,
  seams, messagetree); `agent/` is for the loop runtime and real helpers
  only (`Plugin`, `MountAllAPI`, `TurnSink`, profiles, `NewSessionID`).
  No module imports another module - shared shapes live in core
  (card t_fb49d5cf). TIER AMENDMENT (v0.12 tier law, 2026-10-26;
  TUI retired 2026-11-01, card t_0744eb8e):
  `engine/http_channel/` is ENGINE ring, not modules - a module
  importing it is module-imports-engine, not module-imports-module.
- **New extension recipe.** Create `harness/<name>/` with `<name>.go`,
  `plugin.go` (Plugin adapter), `<name>_test.go`; register in
  `buildPlugins()` in `cmd/omega/main.go`; append tools to
  `ctx.ToolProviders` in `Mount()`. Full steps: `harness/README.md`.
- **Tool merge.** Extension tools merge into the registry at run time;
  built-in tools win on name conflict.
- **Binary purity.** core carries ZERO module nouns; module-shaped
  types live in their capsules. Machine law: scripts/nm-purity.sh
  (kanban canary every push, full rotation on the nightly); capsules
  with known removal dependencies live in scripts/nm-purity.modules
  and leave the manifest only by a green removal.
- **Module REST.** module REST is capsule-registered (api.RegisterRoute,
  core.ModuleRoute) and served by engine/http_channel/module_routes.go from the
  live Context; a module-rooted miss answers 404, an absent module's
  paths ride the SPA catch-all. Seam-nil 501 guards survive only for
  engine-served seams (store, settings, notifications).
- **http_channel.** Persistence order is provider-correct and old
  baked sessions are never rewritten: user -> assistant(A + thinking +
  calls) -> tool results -> assistant(B). List-row concerns
  (pinned/read/archived) never touch `updated_at`. Runs are detached
  from the request (runHub): a disconnect never cancels, one live run
  per session (409), `server.run_timeout` ends hung runs, detached
  title generation fails silently (last write wins under races). Pure
  logic tests go in `server_pure_test.go`, handler tests in
  `server_test.go`. SPA-affecting changes ship via
  `engine/webui/scripts/build.sh` with the committed `static/` output in
  the same change (`scripts/embed-lane.sh` owns `static/` commits at
  merge time). A rebuilt `server.go` needs a serve restart - the
  binary is a snapshot of its build time.
- **webui.** The product reference is `docs/webui.md` (geometry
  contracts, design tokens, dock model, test law, embed lane,
  poisoned-bundle diagnosis). Verify served content, never source
  strings. Module-UI narrow recipe: a module component measures
  its OWN rendered container and flips a narrow class below
  NARROW_MAX_PX=360; each module keeps its own copy (module UI
  imports no shell code); modulenarrow.test.ts pins threshold
  agreement.
- **cli loop.** (TUI retired 2026-11-01, card t_0744eb8e - the
  three-shells-to-two flip.) `omega setup` is the mandatory first-run
  wizard (provider/host/model, live connection test);
  unconfigured `serve`/`run`/loop refuse exit 1 pointing at it. The
  bare `omega` loop (`cmd/omega/loop.go`) is a streaming agent
  conversation with tech-support posture: a leading slash is a
  command (`/help` lists), `"""` opens a multiline block, a trailing
  backslash continues the line; mid-run typing queues or answers a
  verdict card. The webui is the app; the terminal is setup and
  config. Context window priority: provider probe > compaction
  config > `core.DefaultContextWindow` (32768); `ModelInfo` runs on a
  per-lookup `WithModel` copy.
- **ai.** Provider failures are stream events, not Go errors:
  `StreamEnd` with `FinishReason="error"` and `Error` set; the channel
  always closes. Messages and events are sealed interfaces, dispatched
  by type switch on concrete types. HTTP goes through `RetryHTTP`
  (429 and 5xx retry with backoff, `OMEGA_MAX_RETRIES`; other 4xx and
  cancellation return immediately). Upstream answers ending with
  `done_reason: "load"` (ollama-cloud cold-load) are retried with the
  same budget before surfacing as a LOUD stream error - never a silent
  empty turn (t_854eef1b). New provider: a `streamX` function
  plus a case in the `Stream` switch in `harness/provider/`.
  Fragmented tool-call deltas (OpenAI, Anthropic) accumulate by index
  and flush in order after the stream ends; consecutive Anthropic
  `ToolResult`s fold into one user message. `FakeProvider` scripts
  deterministic stream events for agent loop tests.
- **cmd/omega.** Subcommands are the only entry surface;
  `--help`/`-h`/`--version`/`-v` print globally and exit before
  dispatch - there is no per-subcommand help. No subcommand means the
  CLI agent loop (unconfigured: refuse exit 1 pointing at
  `omega setup`); a non-subcommand argument is a project path
  (chdir + loop);
  subcommand names beat same-named directories. `resolveHomePaths`
  runs after `LoadConfig` and before opening the store. `/settings`
  masks `provider.api_key` and rejects a hot PUT of
  `provider.api_key` or `server.bind` (config.yaml is their only
  door).
- **trust.** The trust unit is the nearest directory (walking up from
  cwd) containing an AGENTS.md. Decisions live in
  `<home>/config/trust.yaml` (`exact` / `parent` levels);
  `--approve`/`--no-approve` are CLI-only overrides (`--no-approve`
  wins); every untrusted surface (the CLI loop, `run`, `serve`, the
  `/workspace` command) skips context with a stderr warning; the
  interactive stdin prompt is a TUI-era shape no live host enables.
- **cron.** omega writes only the per-job `enabled:` flag in
  `crons.yaml` (via the `POST /cron/toggle` route, preserving comments);
  everything else in the file is hand-owned (boot seeds only
  `crons.yaml.example`). A fire arriving while its job still runs is
  skipped (logged, never queued); different jobs run in parallel. A
  failed fire still persists its session; deliver targets apply only
  to a non-empty final assistant message. Stale `cronstate.json`
  entries are ignored.

## Gate law

The review gate's operating law (Anthropic agent-volume review steal-list,
card t_8abdbb15; full contract and templates in `docs/release-gate.md`):

- **Gate rounds are capped at 3.** After 3 gate rounds on one card, a
  4th round is FORBIDDEN: stop, and card the approach reconsideration
  for Endo (author-agent vs reviewer-agent can ping-pong forever
  otherwise). From round 3 on, a re-review covers the NEW DIFF only -
  the changed lines against the last round's diff, never a fresh full
  pass.
- **Every finding is tagged at post:** `Important` (blocks the merge),
  `Nit` (never blocks a merge), `Pre-existing` (present at the
  merge-base - the change did not introduce it; never the card
  author's blame, never a blocker).
- **A finding posted in one round is never repeated in a later round.**
  Re-stating is noise; the original comment stands until resolved.
- **Incidents become evals.** Every red that burned a lane once
  enters the named regression set (`scripts/journeys/regression.list`)
  and runs forever - `scripts/journeys/run.sh --regression` at gate
  time, the full suite on the nightly. A recipe (where the browser is
  the only honest witness) or a pin test (where the shape is
  compile-time). A deliberately-broken shape must stay red.

## Wide-angle map

| Area | What it is | Docs |
| ---- | ---------- | ---- |
| `llm/` | LLM provider contract: interface, message/event types, tool schema, shared HTTP infra, retry, ThinkingLevels, `DetectImage` | `docs/api.md` |
| `agent/` | Multi-turn loop contract (`LoopProvider`), capability seams, Plugin system (`Context`, `MountAllAPI`), events, shared types | `docs/architecture.md` |
| `core/` | Core types, events, seams, message vocabulary, messagetree; stdlib-only imports, enforced by `core/importability_test.go` | `docs/architecture.md` |
| `engine/http_channel/` | ENGINE ring (always on, never disabled): HTTP/SSE delivery channel + embedded SPA serving | `docs/api.md` |
| `cmd/omega/loop.go` + `setup.go` | The CLI agent loop + the mandatory first-run setup wizard (the TUI retired 2026-11-01) | `docs/cli.md` |
| `harness/` | In-process extension packages (one `agent.Plugin` each) - the TOGGLEABLE tier; new extensions go here, never into the engine ring | `docs/architecture.md` (table), `harness/README.md` (authoring) |
| `engine/webui/` | ENGINE shell (always on, never disabled): Nuxt 4 webui SPA source at checkout root, built output embedded into the binary via the embed lane | `docs/webui.md` |
| `harness/cron/` | Scheduled prompts (`crons.yaml`, `cron:<name>` sessions) | `docs/cli.md` (`/cron`) |
| `harness/trust/` | Project trust gate (`TrustProvider`, `trust.yaml`, AGENTS.md loading) | `docs/project-trust.md` |
| `cmd/omega/` | Single binary entry point, config, home resolution, self-update | `docs/cli.md` |
| `docs/` | User and worker documentation | start at `docs/architecture.md` |

## Read path

Read AGENTS.md, follow its pointers. One map, no hierarchy of maps.

1. `.agents/COMMIT.md` - commit convention and voice.
2. This map for the layer you are editing - then its docs entry.

## Update after editing

1. Symbol renames do not carry into `docs/` automatically - sync the
   mapped doc home in the same change when it names what you changed.
2. Run the relevant tests before declaring done.
3. Never create an AGENTS.md outside the root.

## Verification

Each non-trivial package leaves a `_test.go` file behind - the
Go equivalent of an assert-based self-check. No frameworks until
there is a reason for one.

```bash
go test ./...     # all layer tests
go build ./...    # all packages compile
go vet ./...      # no suspicious constructs
```

## Pointers

- `docs/cli.md` - subcommands, flags, home resolution, OMEGA_* env vars
- `docs/api.md` - HTTP routes, `/chat` SSE semantics, provider quirks on the wire
- `docs/webui.md` - webui reference: geometry, tokens, dock, test law, embed lane
- `docs/architecture.md` - layer table and project structure
- `docs/configuration.md` - config.yaml knobs
- `docs/troubleshooting.md` - known failure modes
- `docs/project-trust.md` - trust gate
- `docs/pipeline.md` - the landing pipeline (merge train)
- `docs/release-gate.md` - the review gate law: rounds, tags, the lens brief, the regression set
- `docs/removing-a-module.md` - the module removal recipe (capsule law)
- `docs/migrating-a-module.md` - the module migration recipe (universal mount)
- `harness/README.md` - extension authoring guide
- `.agents/COMMIT.md` - commit convention and voice

## Style

- Markdown is linted. Wrap bare URLs in angle brackets:
  `<https://example.com>`.
- No diary entries or TODO comments in this file. Keep it factual
  and contract-focused.
- No emoji, no long dashes - use normal hyphens (`-`).
- Descriptive variable names in public APIs. Short names OK for
  local variables (Go convention).

## User Preferences

- Commit voice: see `.agents/COMMIT.md` - the sole authority on voice.
- Approve-then-apply: present a plan, wait for "lgtm", then act.