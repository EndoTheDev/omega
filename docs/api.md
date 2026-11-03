# HTTP API Reference

The surface `omega serve` exposes: SSE chat streaming, session CRUD +
tree operations, the settings/theming/files/tasks surfaces, and the
embedded webui SPA at `/`. This is the only unauthenticated network
surface omega has — **loopback only by default**; extra binds (e.g. a
Tailscale IP for Traefik) are an explicit `server.bind` opt-in, and
the public edge always sits behind an auth wall.

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
| `/models`                            | GET        | model list + context window                    |
| `/chat`                              | POST       | SSE streaming chat                             |
| `/attach-image`                      | POST       | server-side URL fetch -> chat image            |
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
| `/agents`                            | GET        | agent homes (roster rows)                      |
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
| `/notifications`                     | GET        | finished-things list + unread (`?unread=1`, `?limit/?offset`) |
| `/notifications/read`                | POST       | mark one (`{"id": N}`) or all (`{"all": true}`) |
| `/notifications/events`              | GET        | SSE record stream (the public API surface; the webui's toasts ride the canvas) |
| `/files/browse`, `/files/read`       | GET        | read-only working-dir surface for the inspector; read carries `mime` |
| `POST /files/write`, `POST /files/mkdir` | POST   | the toolbar's write half: add file / add folder (same root jail) |
| `/files/raw`                         | GET        | raw bytes for `image/*` files ONLY, under the read surface's root jail |
| `/tools`                             | GET        | tool inventory                                 |
| `/mcp/servers`                        | GET        | configured MCP servers (name, transport, connection state, recorded failure, server-qualified tools) over the live bridge |
| `/mcp/reload`                         | POST       | re-read mcp.yaml/MCP_SERVERS, swap the live bridge (mcp.yaml edits go live on the MCP tab's refresh - no restart); returns the fresh server list |
| `/mcp/toggle`                         | POST       | the live MCP toggle through the module seam     |
| `/cron/jobs`                         | GET        | cron job list (empty when unmounted — not 501) |
| `/cron/save`                         | POST       | upsert one full job definition into the main crons.yaml (schema-validated, hot-reloaded); returns the fresh job set |
| `/cron/delete`                       | POST       | remove one job from the main crons.yaml; unknown name 404 |
| `/cron/toggle`                       | POST       | the row's two-step toggle commit: writes the per-job `enabled:` flag only |
| `/skills`                            | GET        | loaded skills (name + description only)        |
| `/skills/content`                    | GET        | one skill's SKILL.md body for the viewer tab (64 KiB cap) |
| `/browser/last`, `/browser/frameable`| GET, PUT   | module REST (browser capsule): the resume URL + the embed-permission probe |
| `/commands`                          | GET        | extension command registry (palette rows)      |
| `/commands/dispatch`                 | POST       | run one command/skill locally — never the agent |
| `/logs?lines=N`                      | GET        | omega.log tail (default 200, clamped 1..2000)  |
| `/settings`                          | GET, PUT   | config knobs schema / diff-only save           |
| `/themes`                            | GET        | user theme tokens (`<home>/themes/*.yaml`)     |
| `/`                                  | GET        | embedded SPA catch-all (history-mode fallback) |

Optional seams degrade with a status code, never a panic: no store ->
`/sessions*` and `/sessions/search` answer 501; no settings seam ->
`/settings` answers 501. Module REST (`/tasks*`, `/boards`) is
capsule-registered (v0.12 phase F): the shell serves it from the live
registry (`module_routes.go`) - a registered-pattern miss answers 404,
and with the module absent its paths fall to the SPA catch-all (the
shell holds no knowledge of the module's roots - that ignorance is
the binary-purity law).

Session titles generate on first exchange via a detached second agent
run (`context.WithoutCancel`); failures are silent and rapid first
messages resolve last-write-wins. Titles are cosmetic - a missing one
is not an error state.

## POST /chat

Body: `{"messages": [...], "session_id": "...?", "model": "...?",
"thinking_level": 0..9, "tools": ["name", ...], "retry": bool, "fork":
"msg_id"}`.

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
- **model**: per-run override via a provider copy — concurrent chats
  keep their own model, the default stays untouched.
- **thinking_level**: indexes the shared thinking ladder (0 = none = no
  thinking parameter sent; unset = provider default).
- **tools**: optional allowlist; empty enables the full server
  registry.
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
| `turn_end`        | turn complete                                               |
| `agent_end`       | terminal state (`finish_reason`, incl. `cancelled` from a stop) |
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
it matched that index, with a bracketed excerpt otherwise). The term is
wrapped as the phrase-with-prefix
`"term"*` per FTS5 string rules — progressive typing keeps hitting
`"fib"` finds `"fibonacci"`). `200 []` on a missing/empty `q`; **400**
when FTS5 rejects the query itself; 501 without a store. Archived
sessions are excluded by default; `archived=include` keeps them (the
same param the list takes).

## Config and board surfaces

### GET/PUT /settings

GET returns the knob schema + effective values (`agent.SettingsSchema`
groups: provider, server, store, compaction, runtime, skills, memory,
logging, approvals — the same surface the settings pane renders;
model slots ride typed pickers, approvals.mode is a select; no-store
cache headers). `provider.api_key` is MASKED (`****`) and never
round-trips.
PUT applies a **diff-only** values map keyed by the schema's dotted
knob paths (`provider.api_key`, `compaction.keep_first`, ...) — note
these are not all literal yaml paths (the vision/title models are root
yaml keys exposed as `provider.vision_model` / `provider.title_model`);
unknown keys are 400; `server.bind` and `provider.api_key` are gated —
config.yaml is their only door. Knob help lines note which apply on
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
  api_key), system_prompt. Absent/nil fields leave the layer untouched;
  the default agent answers 409 (`ErrAgentConfigLocked`). Sits beside
  the Agents tab's expansion editor.

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

- `GET /tasks` — list rows; `?status=` filters, the empty value lists
  every status.
- `POST /tasks` — create; `title` required, unspecified status defaults
  to `triage` in the store.
- `GET /tasks/{id}` — the task plus its thread (comments + events).
- `PATCH /tasks/{id}` — any legal status transition; PATCH pointer
  fields distinguish "set to this value (possibly empty)" from "leave
  alone".
- `POST /tasks/{id}/comments` — add a comment; an ABSENT author is
  `"web"`, so the board can tell a human UI comment from an agent tool
  comment.

All registered by the capsule (`harness/kanban/rest.go`) and served
from the live registry - absent module, unregistered path (the SPA
catch-all answers it).

### Finished things (notifications)

- `GET /notifications` — records newest-first (`?unread=1` filter,
  `?limit/?offset` paging, default 50, cap 200); the payload carries the
  live unread count over the whole set, not the page.
- `POST /notifications/read` — mark one (`{"id": N}`; a ghost id 404s)
  or everything (`{"all": true}`).
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

- `GET /tools` — the tool inventory (name + description).
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
  (sniffed from the bytes); the viewer branches on it.
- `GET /files/raw` (t_08d822bb) — the mime's byte face: raw bytes for
  `image/*` files ONLY, under the read surface's exact root jail.
  Non-image or binary requests answer 400, missing files 404; the
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

## SPA catch-all

Exact API patterns win over `/`. The catch-all serves real files, falls
back to `index.html` for client routes (`/chat/<id>`), and still 404s
asset-like misses (anything under `/_nuxt/` or a dotted path) — a bad
asset URL is a bug a deep link must not mask.

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