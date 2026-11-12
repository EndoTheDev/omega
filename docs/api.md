# HTTP API Reference

The surface `omega serve` exposes: SSE chat streaming, session CRUD +
tree operations, the settings/theming/files/tasks surfaces, and the
embedded webui SPA at `/`. This is the only network surface omega has and
it is **unauthenticated** — there is no login, no token, no auth
middleware anywhere in the tree. **Loopback only by default**; extra
binds (e.g. a Tailscale IP for Traefik) are an explicit `server.bind`
opt-in and stay unauthenticated, so whatever sits beyond loopback is
exposed to whoever can reach the port. The only gate is the Host/Origin
heuristic in `api/hostguard.go`, which stops cross-site browser writes -
it is not authentication.

Routes are registered in `api/server.go`. Anything
documented below that is not in that mux is not real.

**The Host/Origin guard** (t_b52d09f3): the unauthenticated surface is
loopback-local, so cross-origin writes ride the browser's own rules —
the `hostGuard` wraps the whole mux and refuses a MUTATING request
whose Origin/Host is not the serving origin with 403 (GETs pass). The
guard covers the fixed routes AND the module capsule routes; curl
against localhost from the same host is unaffected.

| Route                                | Methods    | Notes                                          |
| ------------------------------------ | ---------- | ---------------------------------------------- |
| `/health`                            | GET        | liveness                                       |
| `/ws`                                | GET        | the push channel (one websocket, typed topics) |
| `/models`                            | GET        | model list + context window                    |
| `/chat`                              | POST       | SSE streaming chat                             |
| `/attach-image`                      | POST       | server-side URL fetch -> chat image            |
| `/attach-file`                       | POST       | multipart file upload -> session upload dir reference |
| `/sessions`                          | GET, POST  | list / create                                  |
| `/sessions/search?q=`                | GET        | FTS5 content+label search                      |
| `/sessions/{id}`                     | GET, PATCH, DELETE | detail / flags / remove                |
| `/sessions/{id}/events`              | GET        | SSE reattach to a live run                     |
| `/sessions/{id}/stop`                | POST       | cancel the live run                            |
| `/sessions/{id}/transcript`          | GET        | render-ready items (restore + live contract)   |
| `/sessions/{id}/variants`            | GET, POST, PATCH | branch points + active-variant select    |
| `/sessions/{id}/branch`              | POST       | fork a child session (store-linked)                    |
| `/sessions/{id}/export`              | GET        | active path as JSONL attachment                |
| `/sessions/{id}/verdicts`            | GET, POST  | one session's verdict cards / the answer POST  |
| `/verdicts`                          | GET        | cross-session verdict listing (the bell count) |
| `/agents`                            | GET        | agent homes (roster rows; each row carries `last_session_id`) |
| `/agents/{name}`                     | GET, PUT   | detail / the node-preserving config layer editor |
| `/agents/{name}/sessions`            | GET        | the agent's session list                       |
| `/disabled`                          | GET        | the registry-browser folded state (skills/tools/agents namespaces) |
| `/disabled/skills`, `/disabled/tools` | PUT       | registry-browser toggle fold (one entry per name) |
| `/disabled/agents`                   | PUT        | registry-browser toggle; the DEFAULT agent answers 409 (it must always be on) |
| `/modules`                           | GET        | mounted module capsules (introspection)         |
| `/modules/{name}`                    | PUT        | the live module toggle (`disabled:` fold)       |
| `/themes`                            | GET        | user theme tokens (`<home>/themes/*.yaml`)      |
| `/insights`                          | GET        | session usage analytics                        |
| `/workspaces/browse`, `/workspaces/files` | GET   | the workspace surface (browsers + Files tab root) |
| `/boards`                           | GET, POST  | board registry (multi-board, current flagged) / create |
| `/tasks`                             | GET, POST  | kanban board (list/create, `?board=` picker)    |
| `/tasks/{id}`                        | GET, PATCH | task detail (task+comments+events) / edits     |
| `/tasks/{id}/comments`               | POST       | add a comment (author default: `web`)          |
| `/notifications`                     | GET, DELETE | finished-things list + unread (`?unread=1`, `?limit/?offset`) / clear-all (every record drops; the reply carries `cleared`) |
| `/notifications/read`                | POST       | mark one (`{"id": N}`) or all (`{"all": true}`) |
| `/notifications/events`              | GET        | SSE record stream (the public API surface; the webui's toasts ride the canvas) |
| `/files/browse`, `/files/read`       | GET        | read-only working-dir surface for the inspector; read carries `mime` |
| `POST /files/write`, `POST /files/mkdir` | POST   | the toolbar's write half: add file / add folder (same root jail) |
| `/files/raw`                         | GET        | raw bytes under the read surface's root jail: `image/*` + `application/pdf` render (t_42c8aced), `?download=1` serves ANY file as a `Content-Disposition: attachment` never rendered |
| `/tools`                             | GET        | tool inventory (bare = executable registry; `?disabled=1` = fold-aware management read) |
| `/mcp/servers`                        | GET        | configured MCP servers (name, transport, connection state, recorded failure, server-qualified tools) over the live bridge |
| `/mcp/reload`                         | POST       | re-read mcp.yaml/MCP_SERVERS, swap the live bridge (mcp.yaml edits go live on the MCP tab's refresh - no restart); returns the fresh server list |
| `/mcp/toggle`                         | POST       | the live MCP toggle through the module seam     |
| `/mcp/save`                          | POST       | upsert one full MCP server entry into mcp.yaml (schema-validated, node-spliced, atomic), swap the live bridge; returns the fresh server list |
| `/mcp/delete`                        | POST       | remove one MCP server entry from mcp.yaml and swap the bridge (the removed entry's connections close in the swap); unknown name 404 |
| `/cron/jobs`                         | GET        | cron job list (empty when unmounted — not 501); rows carry the authored `prompt`, the fire-pinned `skills` and the last fire's `status` + `last` + `status_note` |
| `/cron/save`                         | POST       | upsert one full job definition into the main crons.yaml (schema-validated, hot-reloaded); returns the fresh job set |
| `/cron/delete`                       | POST       | remove one job from the main crons.yaml; unknown name 404 |
| `/cron/toggle`                       | POST       | the row's two-step toggle commit: writes the per-job `enabled:` flag only |
| `/skills`                            | GET        | loaded skills (name + description only)        |
| `/skills/content`                    | GET        | one skill's SKILL.md body for the viewer tab (64 KiB cap) |
| `/skills/save`                       | POST       | the skills tab editor's upsert: a new name composes `<dir>/<name>/SKILL.md` (frontmatter composed, loader-parsed), an existing name rewrites the body under the fence while the operator's other frontmatter keys stay byte-true; answers the fresh listing |
| `/skills/delete`                     | POST       | remove one skill: backed up to `.curator/backups`, ledger row at `.curator/ledger.jsonl` (`actor: webui`), folder removed; unknown name 404 |
| `/browser/last`, `/browser/frameable`| GET, PUT   | module REST (browser capsule): the resume URL + the embed-permission probe |
| `/commands`                          | GET        | extension command registry (palette rows)      |
| `/commands/dispatch`                 | POST       | run one command/skill locally — never the agent |
| `/logs?lines=N`                      | GET        | omega.log tail (default 200, clamped 1..2000)  |
| `/observability/events`              | GET        | structured run-event ring: `?session=`, `?level=error\|warn`, `?limit=` (newest N, oldest-first); 1024-record in-process cap |
| `/observability/logs`                | GET        | omega.log tail from the observability capsule (`?lines=`, default 200, cap 2000); 404 without a log file |
| `/settings`                          | GET, PUT   | config knobs schema / diff-only save (`GET ?key=<dotted path>` = one knob, 404 on a miss) |
| `/themes`                            | GET        | user theme tokens (`<home>/themes/*.yaml`)     |
| `/`                                  | GET        | embedded SPA catch-all (history-mode fallback) |

Optional seams degrade with a status code, never a panic: no store ->
`/sessions*` and `/sessions/search` answer 501; no settings seam ->
`/settings` answers 501. Module REST (`/tasks*`, `/boards`) is
capsule-registered (v0.12 phase F): the shell serves it from the live
registry (`module_routes.go`) - a registered-pattern miss answers 404,
and with the module absent its paths fall to the SPA catch-all (the
shell holds no knowledge of the module's roots - that ignorance is
the binary-purity law). A capsule's literal pattern also exact-matches
ahead of the base mux dispatch (the `moduleLiterals` rescue lane in
`agent/api`): ServeMux has no precedence view across muxes, so the
base's own fixed wildcard (`/sessions/{id}`) would otherwise out-specify
a literal like `GET /sessions/search` living behind the catch-all - the
50-sessions-reload nightly red (2026-11-05).

Session titles generate on first exchange via a detached second agent
run (`context.WithoutCancel`); failures are silent and rapid first
messages resolve last-write-wins. Titles are cosmetic - a missing one
is not an error state.

## POST /chat

Body: `{"messages": [...], "session_id": "...?", "model": "...?",
"provider": "...?", "thinking_level": 0..9, "tools": ["name", ...],
"retry": bool, "fork": "msg_id"}`.

- **`messages` is REQUIRED.** Each entry carries a `role` discriminator
  (`system`, `user`, `assistant`, or `tool`) plus its payload. A
  content-only payload (`{"content": "..."}` with no role) decodes to
  nothing and produces an empty turn — Ollama ends it with
  `done_reason "load"` (the load-without-generate path), which rides
  through to the client as `finish_reason: "load"`. Send real
  `{role, content}` entries.
- **session_id semantics**: empty + a mounted store = the server
  AUTO-CREATES the session (generates the id, opens the run, persists
  everything — the new id rides the first SSE `session` frame, so the
  create-then-send dance is dead). With `session_id` set, the
  conversation persists into that session. Without a store, runs stay
  ephemeral.
- **Retry**: `{"retry": true, "session_id": ...}` re-runs the session's
  LAST user turn as a SIBLING VARIANT — no messages accepted on the
  wire (400), the persisted history itself is the replay; nothing is
  appended, the fresh response heads to the same user message, and the
  prior response stays as a selectable variant.
- **Fork**: `{"fork": "<message_id>", "session_id": ...}` re-plays at
  an arbitrary branch point. A user-turn target behaves like a retry
  pinned there (no messages); an assistant/tool target CONTINUES FROM
  it — it takes the fork's user message and runs. Mutually exclusive
  with `retry` (400).
- **`provider`** (card t_7dcd4280): the named provider INSTANCE the run
  serves through (a `providers:` config key; the webui provider picker
  sends it). Empty = the default instance. An unknown name - the
  instance was removed, or never existed - is a loud 400, NEVER a
  silent fallback to the default. A wire name WINS and sticks on the
  session like the model pin: the next send resolves the pinned name
  with no wire field, so removing the pin's instance strands the
  session (its next send 400s) until a new instance is selected.
- **model**: per-run override via a provider copy — concurrent chats
  keep their own model, the default stays untouched. A session owned by
  an agent home is the exception: its home's config layer names the
  provider and model, so a wire `model`/`provider` pick (and one
  remembered on the row) is ignored for that run — resolving a
  daemon-catalog name against another home's provider would answer as
  the wrong agent.
- **thinking_level**: indexes the shared thinking ladder (0 = none = no
  thinking parameter sent; unset = provider default).
- **tools**: optional allowlist; empty enables the full server
  registry.
- **Workspace rooting**: every tool run executes in the session's
  workspace - the folder pinned on the session, or the
  `server.default_workspace` knob for workspace-less sessions
  (a vanished folder degrades to the unset case, the process cwd).
  The webui's workspace chip mirrors the effective folder. Per-call
  override: `shell.run` accepts an optional `dir` argument - absolute,
  or relative to the run's workspace; a missing
  or non-directory path is a named tool error, never a cryptic
  chdir failure.
- **Images**: any user message rides AI image content (base64), capped
  at 10 MB decoded per image (400 above it, checked before anything
  runs).
- **@-mentions expand server-side** (card t_07aed3f9): the LAST user
  message runs through the one `core.ExpandAtMentions` law
  (`core/atmention.go`) before the run starts. Relative
  paths root at the session's workspace; `@file.png` attaches, text
  files and `@glob` patterns inline, `@session:<id>` injects that
  session's history, `@skill:<name>` injects skill content;
  unresolvable tokens stay literal in the persisted message. The
  browser never reads or expands files.

**One live run per session.** A second `/chat` for a session with a
live run is 409. Runs are DETACHED from the request (runHub): a client
disconnect never cancels the run, and `server.run_timeout` (default
1800 s) walls off hung runs. Repeat `POST /sessions/{id}/stop` while a
run winds down is a clean 200; stop is 404 when idle, 409 in the rare
pre-launch window.

### SSE event stream

Response is `text/event-stream`; frames are `event: <name>\ndata:
<json>\n\n`, flushed as written:

| Event             | Carries                                                     |
| ----------------- | ----------------------------------------------------------- |
| `session`         | `{"id": "<session id>"}` — first frame, the durable handle  |
| `agent_start`     | run start                                                   |
| `turn_start`      | one agent turn begins                                       |
| `thinking_chunk`  | incremental reasoning text                                  |
| `response_chunk`  | incremental response text                                   |
| `tool_call`       | name + arguments                                            |
| `tool_result`     | the tool's result                                           |
| `stream_end`      | `finish_reason` (`stop`, `tool_call`, provider string, `cancelled`, `error`), usage counts when reported |
| `turn_end`        | turn complete (`pending_calls` counts outstanding calls - immediate-yield dispatches and inline-budget escalations - that ended the turn) |
| `agent_end`       | terminal state (`finish_reason`, incl. `cancelled` from a stop and `yielded` - the run ended on an outstanding call (immediate-yield or escalated past the inline budget); its result re-enters as a wake turn, see below) |
| `assistant_message`| the assembled assistant message                            |
| `view_model_delta`| one canvas panel descriptor (arc 3b): a module updated its panel state; dispatch on this frame, then merge the panel |
| `error`           | the error text (failures are events, not Go errors)         |

The delta rides every live run stream (the chat's origin stream and
`/sessions/{id}/events` alike) - panel state is global, not per-session,
and there is no second channel. A delta emitted between runs has no
stream to ride: the catch-up is the snapshot below.

## Sessions

### GET /sessions

List rows (`sessionPayload`): the session row plus read-time computed
fields — `message_count` + `tool_count` (SQL counts at read time, no
counters), `prompt_tokens` + `completion_tokens` (real usage persisted
per turn; 0 for pre-usage sessions), `model` (last model change,
omitted when none), `unread` (read-time from `updated_at` vs
`last_read_at`), and `running` (from the run registry — the backend's
single truth for "streaming"; ALWAYS present, true or false, and
survives page reloads). Archived sessions drop out of the default list;
`GET /sessions?archived=include` returns them (the sidebar's archived
toggle).

### POST /sessions

Creates the session **first** (server-generated id, persisted before
the chat) and answers 201 with the same shape a detail GET returns.
The pre-created id is what `/chat` and the SSE reattach then use.

### GET /sessions/{id}

Full history + the same live state the rows carry. GET-response
messages additionally carry `created_at` (store-level write time).

### PATCH /sessions/{id}

List concerns only — accepts `{"pinned": true|false}`, `{"read":
true}`, `{"archived": true|false}`. All write the row WITHOUT touching
`updated_at` (pinning, reading, archiving are not activity; the time
buckets read `updated_at`). Pinned flag also via the row-actions;
branch/rename/export round-trip through `/branch`, PUT label, and
`/export`.

### DELETE /sessions/{id}

Removes the session and its messages (the webui confirms first).

### GET /sessions/{id}/events

SSE subscribe to the session's live run. **404 when not active** (never
started or finished — the client renders settled history). Live-only:
a subscriber attaching mid-run receives frames from attach time on;
what already happened is readable from the history/transcript
endpoints. Headers go out on attach, not on the first frame, so an
attach landing between two frames doesn't hang the fetch.

### GET /sessions/{id}/transcript

The render-ready item stream — the backend's final answer to "what
does this session look like": items (user, assistant, thinking, tool
blocks) already ordered, thinking merged, tool results attached. The
client's restore path is "render items"; no reload-side
reconstruction. Items carry the message's store row id (as a string)
to address branch points; live-tail items carry none.

### GET /sessions/{id}/variants (+ POST/PATCH)

Branch-point metadata for the variant switcher; the select action sets
the active variant server-side (persisted via a `variant_select`
bookkeeping entry — survives reload). The body's `message_id` is either
a branch point (a message carrying sibling runs; an optional 0-based
`select` picks that sibling) or any conversation message (then the
selection resolves to the message's own run). Sessions without any
multi-variant point return an EMPTY list, not an absent key.

### POST /sessions/{id}/branch

Creates a child session through the store's branch machinery, then
appends one summary system row into the child. Read-time
the child inherits the whole ancestor chain as its active-path prefix.
501 without a store, 404 on an unknown parent, 201 with the child
session.

### GET /sessions/{id}/export

The session's ACTIVE PATH as JSONL — one `{"role","content"}` line per
message, identical shape to `omega export`. Served as an attachment
named `<session id>.jsonl`.

### GET /sessions/search?q=<term>

Unified FTS5 search over session labels, hygiene summaries AND message
content, deduped to ONE hit per session (label hits ranked first, then
summary hits, then content - each hit carries `title` or `summary` when
it matched that index). The term is
wrapped as the phrase-with-prefix
`"term"*` per FTS5 string rules — progressive typing keeps hitting
`"fib"` finds `"fibonacci"`). `200 []` on a missing/empty `q`; **400**
when FTS5 rejects the query itself; 501 without a store. Archived
sessions are excluded by default; `archived=include` keeps them (the
same param the list takes).

Each content/summary hit's `snippet` is a VERBATIM excerpt of the
stored content - a contiguous substring, byte-for-byte what the store
holds, with no highlight markers or synthetic `...` inserted (card
t_1901ce48). Where the matched terms sit inside the excerpt rides
structured metadata: each hit may carry `matches` - byte offsets
(`{start, end}`) into the snippet. A verbatim consumer can therefore
strip nothing and trust the string; a canary/echo check against an
exact token compares the snippet (or `snippet[m.start:m.end]`) against
stored content directly. The same contract holds for the agent-facing
`sessions.search` tool: its output lines are a `[label]` prefix
(presentation formatting) followed by the snippet text - literal
stored content, brackets included only when the stored content really
has them.

## Config and board surfaces

### GET/PUT /settings

GET returns the knob schema + effective values (`agent.SettingsSchema`
groups: provider, server, store, compaction, runtime, skills, memory,
logging, approvals — the same surface the settings pane renders;
model slots ride typed pickers, approvals.mode is a select; no-store
cache headers). `provider.api_key` is MASKED (`****`) and never
round-trips.

Every knob also carries `default`: the value it takes when config.yaml
names no key - the built-in defaults with the `OMEGA_*` environment
overrides applied (the file itself is not consulted: a settings save
materializes every key, so a file-derived value would never differ from
the current one). The field is always present; `null` means no default is
known, which today is only `provider.model_name` (config requires it).
The settings pane's modified-dot and reset-to-default compare against
this field (settings tier 2a/2b).

`GET /settings?key=<dotted path>` answers ONE knob - the same knob
object as it rides inside its group - so a script client reads a
single value without decoding the group envelope. An unknown key is
404 `unknown setting: <key>`; an empty `key` param is ignored (the
full payload answers). The one-knob read is the same masked schema
the full read serves (`provider.api_key` still rides `****`).

#### agent scopes (`scopes`, settings tier 3a)

The payload also carries `scopes`: one entry per agent home under
`<home>/agents/`, for the pane's agent tabs. The list always rides the
wire and is never null - an empty list (no agent dirs) means the pane
renders no scope strip at all. The main agent is never a scope: its
home is the home root and its config stays locked (the agents REST
edit endpoint answers 409 for it).

Each scope is `{name, groups[], error?}`. `groups` is the four
overridable knob groups in render order - `provider`,
`system_prompt`, `description`, `max_turns` - each with a roll-up
`overridden` and its `knobs[]`. Each knob is
`{key, value, overridden}`: `key` is the same dotted path the main
payload's knob uses, `value` is the EFFECTIVE value (the agent layer's
own when the layer names the key, main's otherwise, secrets masked
exactly like the main payload), and `overridden` is structural - the
agent's `config/config.yaml` names the key (a commented-out scaffold
line is not an override). A knob marked `overridden: false` inherits
the main group's knob with the same key, which is the pane's
differs-from-main base for its dot.

`max_turns` follows the runtime lane: the inherited value is the
serving agent's live turn cap, not the yaml file's, because a run is
capped by the live knob (the same hot-apply the settings pane's own
`max_turns` save rides). `error` is present only on a layer file that
could not be read or parsed; the scope then carries inherited values
and no override flags, and the field says so.

The write path does not live here: every scope write goes through the
agents REST edit endpoint (`PUT /agents/{name}`), unchanged.

PUT applies a **diff-only** values map keyed by the schema's dotted
knob paths (`provider.api_key`, `compaction.keep_first`, ...) — note
these are not all literal yaml paths (the vision/title models are root
yaml keys exposed as `provider.vision_model` / `provider.title_model`,
and the four per-subsystem model slots — compaction, summarizer,
skills curator, approvals — are the same shape as
`provider.*_model`);
unknown keys are 400; `server.bind`, `provider.api_key` and a
`provider.instances` row's host / api_key are gated - config.yaml is
their only door (instance rows stay renameable / removable in the
pane; the endpoint and the secret are not settable over the API). Knob
help lines note which apply on
restart vs hot-apply live (http timeout + compaction hot-apply; context
window help reports what startup auto-probed).

### The registry-browser toggles and the agent editor

- `GET /disabled` — the folded state of the registry-browser
  namespaces (skills, tools, agents) as the config `disabled:` fold
  carries them.
- `PUT /disabled/skills|tools|agents` — body `{"name": ...,  "enabled":
  true|false}`; the write lands in config.yaml's `disabled:` fold and a
  live toggle re-reads the seams. One refusal stands above the fold:
  `PUT /disabled/agents` for the default agent answers **409**
  ("the default agent cannot be disabled - it must always be on",
  t_2d8deb8c) and leaves config.yaml untouched.
- `PUT /agents/{name}` — the node-preserving agent config layer
  editor: description, the provider layer (type, model_name, host,
  api_key), system_prompt, max_turns. Absent/nil fields leave the layer
  untouched; the default agent answers 409 (`ErrAgentConfigLocked`). Sits
  beside the Agents tab's expansion editor.

  Settings tier 3b (t_53523d5b) added the two fields the scope tabs
  need, both additive — an old body is unchanged in meaning:

  - `max_turns` (JSON **number**, not a string): written as a yaml int,
    because a wake unmarshals the layer straight into the runtime's int
    knob. `0` is a real value ("no cap"); omitting the field is what
    means unset. Negative answers **400** (`max_turns must be >= 0`,
    the same floor `config.yaml` validation applies).
  - `unset` (array of dotted knob paths: `description`,
    `system_prompt`, `provider.type`, `provider.model_name`,
    `provider.host`, `provider.api_key`, `max_turns`): the RESET verb.
    Each named key pair is deleted from the layer file, every other key,
    comment and style stays byte-intact, and a `provider` block left with
    no sub-keys is removed with them. Deleting is what makes a scope fall
    back to main — the settings payload's `overridden` flag is structural
    (the file names the key), so writing main's value down would leave a
    permanent phantom override, and for `provider.api_key` would persist
    the read mask as the agent's secret. Unknown names, a group name
    (`provider` alone), and a key both set and unset in one request
    answer **400**. Sending the read mask (`****`) as `api_key` is
    **400** too: the reset verb for the secret is
    `unset: ["provider.api_key"]`.

### /boards (multi-board registry)

- `GET /boards` — the board registry, the current board flagged.
  `POST /boards` (`{"name": ...}`) creates one; `DELETE /boards/{name}`
  removes an empty, non-current, non-default board (guards refused
  with a 400 carrying the refusal text - there is no force).
  Registered by the capsule while the module is mounted (see module
  REST above); an error from the handler is the capsule's own answer
  (single-board implementations stay legal).

### /tasks (kanban)

- Every task route takes `?board=<name>`; the empty value is the
  current board (the is_current row in the one kanban.db, CLI:
  `omega kanban board use <name>`). There is no cross-board mutation
  endpoint: a PATCH moves a card inside its board only, switching
  boards is a pointer operation, never a card op.

- `GET /tasks` — list rows in board order (the column's drag order,
  newest-first fallback); `?status=` filters. The EMPTY value lists the
  active board: every status but `archived` - the default list is not
  an archive dump (t_02a8beea A2). `?status=archived` lists the
  archive, `?status=all` lists everything. The board grid module UI
  requests `all` deliberately (it renders the archived column).
- `POST /tasks` — create; `title` required, unspecified status defaults
  to `triage` in the store. A fresh card lands on top of its column.
- `GET /tasks/{id}` — the task plus its thread (comments + events).
- `PATCH /tasks/{id}` — any legal status transition; PATCH pointer
  fields distinguish "set to this value (possibly empty)" from "leave
  alone". An `after_id` body is the drag-order slot instead: the card
  moves to `status` ("" keeps the column) and lands after that sibling
  ("" = top), the target column resequencing to dense positions - the
  order survives every reload. Requires the store to implement the
  `Reorderer` capability (the kanban store does; without it the PATCH
  answers 400 and the board refetches, the card snapping back).
- `POST /tasks/{id}/comments` — add a comment; an ABSENT author is
  `"web"`, so the board can tell a human UI comment from an agent tool
  comment.

All registered by the capsule (`harness/kanban/rest.go`) and served
from the live registry - absent module, unregistered path (the SPA
catch-all answers it).

#### The batch contract (the `kanban.batch` tool's `operations` array)

The batch is ATOMIC on the mounted store (card t_02a8beea): the whole
`operations` array runs inside one SQLite transaction (the `Batcher`
capability) - every operation applies or none does, and a failed batch
rolls back the operations it already applied, so no create inside a
failed batch survives to orphan on the board. The failure report reads
`failed at operations[N]: ... (batch rolled back, M applied
operations reverted)`. In-batch referencing rides the same machinery:
an operation's `id` may be `LAST` (the task the most recent create in
the batch made) or `#N` (1-based, the task created at
`operations[N-1]`, matching the numbered result lines). References are
validated BEFORE anything applies - a malformed or forward-pointing
reference fails the whole batch at the door with zero side effects.
Done-notifications flush only after the batch commits; a rolled-back
batch emits none. A board whose provider lacks the `Batcher`
capability keeps the fail-forward semantics honestly reported (applied
ops stand, no rollback claim). Create-then-act in one call is the
intended shape:

```
kanban.batch {operations: [
  {action: create, title: "ship it"},
  {action: comment, id: "LAST", body: "opened by the pipeline"},
  {action: complete, id: "LAST"}
]}
```

### Finished things (notifications)

- `GET /notifications` — records newest-first (`?unread=1` filter,
  `?limit/?offset` paging, default 50, cap 200); the payload carries the
  live unread count over the whole set, not the page.
- `POST /notifications/read` — mark one (`{"id": N}`; a ghost id 404s)
  or everything (`{"all": true}`).
- `DELETE /notifications` — clear-all: every record drops (the webui
  tab's button); the reply carries `{"ok":true,"cleared":N}`.
- `GET /notifications/events` — SSE: each subsequent emit becomes one
  `notification` frame.

These routes STAY (settled Q6a): they are the capsule's public REST
surface. The module's view-model rides the canvas (arc 4b): the list
panel and the toast panel appear on `GET /ui/canvas`, every emit pushes
a toast delta, and row mark-read actions dispatch through
`POST /ui/actions` with `{"id": N}` args. The webui renders toasts from
the canvas, not from this SSE route. All answer 501 when the
notifications seam is not mounted - removing the module removes the
panels, the toast events and this surface together.

### Observability (event ring + log tail)

The `observability` capsule owns the two reads behind the webui logging
tab. Both are GET-only; both die with the capsule (a disabled module's
paths answer 404 at the module mux), and neither exports anything - no
OTLP, no exporter, no telemetry dependency (t_8de82cff; OTel is a
future concern, deliberately out of v1).

- `GET /observability/events` — the structured run-event stream, read
  from a bounded in-process ring (1024 records, oldest dropped). Each
  row: `seq` (monotonic, the client's ordering key), `time`
  (RFC3339Nano UTC), `session`, `type` (`agent_start`, `turn_start`,
  `turn_end`, `tool_call`, `tool_result`, `stream_end`, `agent_end`),
  `level` (`info` / `warn` / `error`), `tool`, `turn`, `duration_ms`,
  `summary`, `error`. Filters: `?session=<id>` (absent = every
  session), `?level=error` (errors only) or `?level=warn` (warnings and
  errors; `all` / `info` = unfiltered - an unknown value is a 400, not
  a silently empty page), `?limit=N` (default 200, cap 1000; the page
  is the newest N matches, returned oldest-first). The body also
  carries `returned`, `buffered` and `capacity`, so a client can tell
  "nothing yet" from "the ring rolled over while you were away".
  Sessionless runs (title generation runs detached from the
  conversation, a one-shot CLI run) carry no session and appear only in
  the unfiltered view - the session filter never has to guess.
  Durations are DERIVED, never measured: a turn's start→end span and a
  tool call's call→result span, both paired in-process (`duration_ms`
  is absent when nothing could be paired). Levels are derived too:
  `stream_end`/`agent_end` errors and `IsError` tool results are
  errors, a `cancelled`/`max_turns` finish and a `length`-capped
  stream are warnings. Token-level stream chunks are not recorded -
  the ring stays structural.
- `GET /observability/logs?lines=N` — the tail of the log the running
  process writes (default 200 lines, clamped 1..2000, 512 KiB read
  window), as `{"path": ..., "lines": [...]}` with the lines verbatim.
  404 when there is no log file (logging disabled, or a logger without
  a readable path). This is the secondary view: the disk side of the
  picture, and the only history across restarts. The shell's
  `GET /logs` remains the Logs tab's own door to the same file.

The ring is fed from the run event spine through the session-attributed
observer (`core.SessionEventObserver`): the spine's events carry no
session, so the capsule subscribes through the shape that receives the
run's session id alongside each event. Recording stops when the capsule
dismounts (its lifecycle handle's Stop) - the leftover subscription
costs one atomic read per event thereafter.

### POST /attach-file

`multipart/form-data`: one `file` part plus an optional `session_id` text
part. PART ORDER IS LOAD-BEARING: the handler reads the parts in wire
order, so `session_id` must precede `file` - reversed, the bytes stage
under `_pending` and only `/chat`'s relocation moves them into the
session dir. The bytes are written into the session's upload directory
(`<home>/uploads/<session_id>/`), or - when no session id is supplied, as
on a draft chat - into `<home>/uploads/_pending/<token>/`; `/chat` moves
a staged file into the session's directory as soon as the id exists and
rewrites the reference before the turn is persisted. The answer is the
reference record `{kind, name, size, path}` (kind = the lowercased
extension without its dot, `file` when the name has none); the client
echoes it back in the user message's `attachments` array, which is what
the store persists.

One file per request, 10 MB per file (the same class as the image cap;
413 past it). No content is sniffed, extracted, inlined or parsed on
this lane - the answer is a reference, a path the agent opens with its
own tools.
Path rule on `/chat`: every `attachments[].path` must resolve to a
regular file inside `<home>/uploads/` (symlinks resolved, traversal and
anything outside the tree refused with 400).

What the agent sees (`agent/loop/attachments.go`): the reference rides
the message untouched through the store, the transcript and the
provider wire, and is rendered into the model-facing content of that
user message at each provider call - one block per attachment:

```
--- attached file ---
name: report.csv
type: csv
size: 220 bytes
path: <home>/uploads/<session>/report.csv
content:
a,b
1,2
--- end attached file content ---
```

`name`/`type`/`size`/`path` mirror `core.Attachment`. The `content:`
section is the ONE inlining exception: `.md`, `.txt`, `.csv` and
`.json` under 8 KB are inlined so the agent answers without a tool
round trip; every other file (pdf, archives, oversize or non-UTF-8
text) closes the block after `path:` with `content: not inlined - read
it with files.read`, and no path allow-list is needed - the uploads
tree is reachable by `files.read` with an absolute path. The rendered
copy is per call and never persisted.

### POST /attach-image

Body `{"url": "<http(s) URL>"}`. The SERVER fetches the bytes (the
webui never fetches cross-origin itself), sniffs the magic bytes, and
answers `{media_type, base64}` in the AI image shape the client then
rides through `/chat`. The internal-address guard (loopback, link-local
incl. cloud metadata, RFC1918, Tailscale CGNAT) runs on the first hop
AND on EVERY redirect hop — a public 302 pointing back into loopback or
the LAN aborts the fetch. 400 on non-http(s) URLs, oversize bodies, or
a guard hit. IP-literal guard only; hostnames resolving internally are
the known ceiling.

### GET /themes

The parsed theme files from `<home>/themes/` (token overrides +
`name`/`base`). A file that fails to parse rides along with `Error`
set and no tokens, so the picker can flag it; hot folder polling picks
up edits without restart.

## Read-only inspector surfaces

- `GET /tools` — the tool inventory (name + description). The bare
  read is the agent's executable registry the chat loop consumes:
  disabled tools are absent. `?disabled=1` is the fold-aware
  management read the settings tab rides (t_5c9bb50a): the same union
  before the fold filters it, each entry stamped with the fold's
  verdict (`disabled: true`), so a disabled tool keeps its row and
  its re-enable toggle.
- `GET /cron/jobs` — the job definitions (name, kind, schedule or
  minutes, resolved deliver target, agent, model, file, enabled, next
  fire time), sorted by name; an empty list when cron is unmounted (a
  normal state, not 501).
- `GET /skills` — name + description only; skill content, directories,
  and the prompt section never ride the wire.
- `GET /logs?lines=N` — tail of the omega log (default 200, clamped
  1..2000, 512 KiB read window); 404 when there is no log file
  (logging disabled).
- `GET /files/browse` + `GET /files/read` — the process working
  directory for the webui inspector: read-only, GET-only, 64 KiB
  preview cap, and the path guard is the traversal set (absolute
  paths, empty segments and only `.` / `..` rejected — dotfiles list
  and read like everything else; t_ccd4a000) plus
  symlink-containment. `/files/read` carries `mime` for every file
  (sniffed from the bytes) and `size` (the full size on disk,
  t_42c8aced); the viewer branches on the mime: `image/*` -> `<img>`,
  `application/pdf` -> the browser's native viewer in an iframe,
  otherwise text, with a binary flag keeping the no-preview state.
- `GET /files/raw` (t_08d822bb, t_42c8aced) — the mime's byte face: raw
  bytes for `image/*` + `application/pdf`, under the read surface's
  exact root jail. `?download=1` is the download lane: any file serves
  as `Content-Disposition: attachment` (never rendered as a document).
  Anything still renderable-unfit answers 400, missing files 404; the
  response is `Content-Type: <mime>`, `nosniff`, byte-identical body.
- `POST /files/write` + `POST /files/mkdir` — the Files tab toolbar's
  write half (add file / add folder), same root jail and guard,
  POST-only bodies.

## Commands (the slash palette's server surface)

The webui's `/` command palette rides two endpoints (card t_07aed3f9).
Slash commands run LOCALLY and never hit the agent — the command law,
same registration seams:

- `GET /commands` — the extension command registry
  (`ctx.Commands`): name + description + args per row. Skills are NOT
  here — they ride `/skills` and the palette renders them as
  `/<skill-name>` rows itself.
- `POST /commands/dispatch` — body `{"name": "/<command>"}` (plus
  optional `"args"`). Resolution order: a
  registered extension command executes through the chain-on-miss
  `ctx.CommandHandler` (rewired at boot and on every live module
  toggle) and answers `{"kind":"command","text":...,"actions":[...]}`;
  if no command matches and the name resolves to a skill, it answers
  `{"kind":"skill","name":...,"content":...}` — the content the client
  arms as a System preamble on its next send. Unknown names are 404,
  non-slash
  names 400, a missing handler 501. Dispatch is a local execution
  surface: it never enqueues an agent run.

## Canvas (view-model)

The web half of the canvas contract (arc 3b): the webui loads the
snapshot, then applies delta frames; actions go back through the pipe.
The per-module REST routes stay; these two endpoints are additive.

- `GET /ui/canvas` — the current panel snapshot of every mounted
  module, one JSON array of panel descriptors verbatim (`id`, `title`,
  `kind`, `payload`, `actions`). Mount order, panel ids sorted within
  a module. Nothing mounted is a 200 with `[]`, not an error.
- `POST /ui/actions` — the one action pipe. Body: `{"module",
  "action", "args"}` (module-qualified, `args` raw JSON verbatim).
  Routes through the canvas runtime to the capsule's registered
  handler - zero per-module code. **404** on unknown module or
  unknown action (route failures read as 404s, never 500s); the
  handler's own error is a **500**; success returns the handler's
  result as `{"result": ...}`. Non-POST/GET method mismatches are 405.

The chat panel (chat-last 1/3, sharpened by 2b): the conversation itself
is a canvas surface - one chat panel per live frontend, module-owned.
In a `serve` process the `chat` panel (module `http_channel`) rides the
snapshot (`GET /ui/canvas`) and `view_model_delta` frames on the run
streams: its payload is the focus session's message rows (`session`,
`running`, `rows` - role, content block, status per row), snapshots
cover the newest 40 rows, streaming tokens batch onto
`view_model_delta` at the cadence and structural stream events flush
immediately. Its actions (`stop`, `retry`, module `http_channel`) route
through the pipe and serve the same faces the REST routes serve. The
registration is Start-gated: the panel exists only once a channel
actually starts - a process that never serves carries no dead http
chat panel. The `/chat` REST + SSE API
surface stays untouched - the panel is additive for canvas renderers.

## The push channel (`GET /ws`)

One websocket per browser connection, and every live update is a typed
topic on it (card t_7624bab1, design of record
`Workshop/omega/websocket-channel.md`). The socket replaces the webui's
polling: no shell view polls a GET route on a cadence beside it (the
kanban board's 5s `GET /tasks` died with card t_b5784de3, the logs
pane's tail with t_0e7bc0bb). One module pane is the exception and polls
on purpose: the observability tab re-reads `/observability/events` every
5s while it is mounted (`harness/observability/ui/rows.ts`, `REFRESH_MS`),
because the closed topic set carries no observability topic - a topic
there would be an engine change (`docs/webui.md`).

**One hub per process.** The fan-out is bound once per process, never
per bind: a multi-bind server (several `server.bind` addresses) wires
every bind's `/ws` to the same `SocketHub` (`BindSocketHub`, the same
binding the run registry follows at channel start), so a frame any bind
publishes reaches every connected browser. Per-bind hubs served each
bind's clients from its own subscriber set - a browser on one bind
never saw a frame published on another (card t_229edd92).

**Events are hints, state is truth.** Nothing is buffered or replayed
server-side. On close the client reconnects with backoff, does one
`GET /health`, and refetches the visible panes through the ordinary GET
routes - which stay complete and authoritative. The socket is never the
only way to read a fact.

A module pane may render beyond a panel's view-model (the kanban board
carries bodies, priorities and drag positions the `tasks` panel does
not), so the canvas topic has a window seam of its own: every applied
panel delta is republished as `omega-canvas-delta` from
`webui/app/composables/canvas.ts`, and the reconnect catch-up fires the
same event with the reset id so such a pane re-reads its own REST route.
A delta that landed while the socket was down is never replayed, so
without that pulse the pane would stay stale until some later mutation
happened to fire (card t_b5784de3).

The client half (card t_896cf5a8, `webui/app/composables/socket.ts`)
owns one connection per page load: it subscribes to five live topics by
default (the sixth, `logs`, is added while a logs pane is mounted),
reconnects on an exponential ladder (0.5s doubling, 15s cap, jitter),
and runs a quiet-wire check - 15s without a frame while the health topic
beats every 5s means a half-open socket that will never fire `close`
(a wifi flap, a suspended laptop, the devtools offline toggle), so it is
cut by hand and the ladder rebuilds it. That budget is a DEADLINE
measured from the last frame: a check that finds the wire alive re-arms
for the remainder, never for a fresh whole budget, so the cut lands
within one budget of the final frame. A full-budget re-arm instead let a
dead wire sit undetected for up to 2x the budget (30s), longer than the
outage a reconnect catch-up is given - the half-open socket then revived
with the network and the catch-up refetch never ran (card t_71f28526).
The cut also hands recovery to the ladder ITSELF: a half-open socket's
`close` event is not a promise. Measured under `ab set offline` (both
agent-browser 0.26.0 and 0.42.0), `close()` was called at 15-16s of
silence and the close event stayed absent for the entire outage - it
only landed when the network returned, and a client that waits for it
sits dark with no timer armed (no reconnect, no catch-up refetch, a
stale pane, journey 111 red at 45s of polling). So the deadline tears
the socket out of the client's hands and schedules the ladder at once;
the browser's late close is ignored by the stale-socket guard in
`onclose` (card t_71f28526, pinned by
`test/socket.test.ts` "the ladder runs even when the dead socket never
reports close").
That timer is the liveness
heartbeat, not a poller: no GET repeats on a cadence anywhere in the
shell (the one repeated GET in the product is a module pane, the
observability tab's 5s ring read, see above). The `chips` topic feeds
the pending chip's live tail; `runs`
edges clear the pulse and wake an awaiting yielded call; `canvas` feeds
the panel store and the toast funnel; the verdict list has no topic of
its own and is refetched on chat call/turn edges, run edges and
reconnect.

The sixth topic, `logs`, is **subscription-gated** (card t_0e7bc0bb):
the shell adds it to that one connection only while a logs pane is
mounted and drops it when the last one unmounts, so a page with no logs
pane receives no log frames at all. It carries only the lines appended
to the log file since the previous frame - the snapshot half stays
`GET /logs` (200 lines), and the pane appends the deltas onto it. The
pane is module UI, so the two directions ride window `CustomEvent`s
(`omega-logs-subscribe` out, `omega-log-stream` in) rather than an
import - the same seam the kanban board uses for `omega-select-board`.

### Frame envelope

Every pushed frame is one JSON text message:

```json
{"topic": "chat", "session": "s-1", "event": "response_chunk", "seq": 42, "data": {...}}
```

- `topic` - one of the six below (the closed set).
- `session` - the conversation the frame scopes to; absent on the
  global topics (`canvas`, `health`, `logs`).
- `event` - the chat topic's SSE event name (`response_chunk`,
  `tool_call`, `tool_result`, `turn_end`, `agent_end`, `error`,
  `view_model_delta`...). Absent elsewhere: the topic is the type and
  `data` is a typed payload.
- `seq` - the hub's monotonic frame counter, one publish order across
  all topics. It is diagnostic, not a loss detector: a subscriber to a
  subset sees the jumps of the frames it does not hold, and the `chips`
  topic may drop frames by design. Loss is signalled by the connection
  ending, never by a number.
- `data` - the payload: the chat/canvas frames carry the SSE bytes
  **verbatim**, so a client dispatches a socket frame exactly like a
  stream frame; the other topics carry a typed struct.

### Topics

| Topic | Payload | Source (the broadcast seam) |
| ----- | ------- | -------------------------- |
| `chat` | the run's SSE frame verbatim | the run broadcast funnel - every frame `launchRun` broadcasts (token/thinking deltas, `tool_call`, `tool_result`, `turn_end`, `agent_end`, `error`). Wake turns launch through the same pipe, so yielded results stream on the same topic |
| `chips` | `{call_id, tool, fill, text, content, truncated, is_error, timeout, elapsed_ms}` | `fill` false: the run output tap - a tool's live output as it lands (the shell's capped buffer). `fill` true: the pending chip settles (`wakeHome`, the one settle entry point) |
| `canvas` | one `view_model_delta` panel descriptor | `CanvasEmit` - every mounted capsule's panel state, notifications toasts and board changes included. Panel state names itself in the payload, so the shell carries no capsule noun |
| `runs` | `{active}` | the run slot: taken in `prepareRun`, released when the run's finish hook fires. This is the live-run truth `GET /sessions` used to be polled for |
| `health` | `{status, uptime_seconds, version}` + the optional facts `{provider: {reachable, checked_at, detail?}, database: {healthy, detail?}, last_error: {message, at?}}` | the server heartbeat, every 5s. The same body `GET /health` serves |
| `logs` | `{lines: string[]}` | the log follower (`api/server_inspector.go`): the lines appended to the log file since the previous frame, read by offset from the same path `GET /logs` tails (the write site is `harness/logging`'s `FileLogger`). A DELTA, never the tail - the pane's snapshot is `GET /logs`. Subscription-gated: nothing is read or published while no connection holds the topic, and a subscriber that did not ask receives nothing |

The `chips` topic is the one **lossy** topic: a consumer that falls
behind drops frames rather than losing the wire (the tool result and
the transcript remain the truth). On every other topic a stalled
consumer is cut and reconnects.

**The health body's optional facts** (card t_5e2f3095) are all-or-nothing
per key: an ABSENT key means the process has no source for that fact -
no provider probe has landed yet, no store module is mounted, the
logger carries no last-error record, or the server predates the
enrichment - and the webui's health popover renders `not reported`
rather than inventing a value. A sourced fact always rides as an
object, so `last_error: {message: ""}` (a sourced "none since boot") is
distinguishable from an absent `last_error`. `provider` is a CACHED
probe - one model-list round trip a minute (`api/health.go`), never one
per 5s frame - and `checked_at` dates it; `database` is the store seam's
`Ping`, probed inline with a 2s budget; `last_error` is the newest
`ERROR:` entry the logger itself recorded.

### Client -> server

The only message is a subscription:

```json
{"type": "subscribe", "topics": ["chat", "chips"]}
```

It **replaces** the connection's topic set (a connection starts
subscribed to all six). Unknown names are dropped; an empty list keeps
the current set. Any other `type` is ignored.

### Refusals

`/ws` is GET-only (`405` otherwise) and must carry upgrade headers - a
plain GET answers `400`. The same origin law as every other route
applies: the upgrader refuses an `Origin` whose host is not the serving
host, and the host guard stays wrapped around the mux. Loopback bind
until the gateway arc owns auth.

## SPA catch-all

Exact API patterns win over `/`. The catch-all serves real files, falls
back to `index.html` for client routes (`/chat/<id>`), and still 404s
asset-like misses (anything under `/_nuxt/` or a dotted path) — a bad
asset URL is a bug a deep link must not mask.

## Yielded turns (async agent loop)

An outstanding tool call - an immediate-yield dispatch
(`delegate.task`) or a call that ran past the loop's inline budget and
escalated (card t_1501cb04) - ends the run with `agent_end`
`finish_reason: "yielded"`: the result re-enters when it settles as a
wake run on the same session: every live subscriber sees TWO runs on
one send (detach at the first `agent_end`; reattach via
`/sessions/{id}/events` to ride the wake). The synthetic result
persists with store role `yield_result` (render role tool), the same
shape any future out-of-process executor writes. A home parked by the
runaway guard (25 consecutive wake turns with no human input) carries
a plain user-row note in the conversation.

## Provider quirks that leak onto the wire

- **finish_reason is the provider's string**, passed through: Ollama
  maps its `done_reason` field directly (`tool_call`, `length`, ...).
  The one exception is `load` on a bare stream (nothing streamed):
  cloud load-without-generate is retried and, when it persists, ends
  the run as `finish_reason: "error"` instead of an empty turn. A
  `load` that follows streamed content passes through unchanged.
- **Errors are events, not Go errors**: provider failures come back as
  `stream_end` with `finish_reason: "error"` plus an `error` event;
  the channel always closes.
- **Retries are invisible**: 429 and 5xx retry with backoff inside the
  provider layer (budget `OMEGA_MAX_RETRIES`, default 3) before any
  frame reaches you; other 4xx and cancellation return immediately.
  The same budget applies to the Ollama `load` retry above.
- **Model override is per-run state**: a `/chat` model builds a provider
  copy; concurrent streams cannot bleed models into each other.
- **Context window fallback**: `/models` probes the provider once per
  model per process (then caches) and falls back to the config default
  (32768) when the provider cannot report one (keypool/ollama-cloud).
- **Anthropic**: consecutive tool results fold into one user message
  server-side; OpenAI-compat reasoning arrives as `reasoning` deltas
  surfaced as thinking chunks, and usage rides `stream_end` when the
  provider reports it.

## Related

- [cli.md](cli.md) — the binary behind this server
- [configuration.md](configuration.md) — the knobs `/settings` serves
- [webui.md](webui.md) — the SPA this wire serves