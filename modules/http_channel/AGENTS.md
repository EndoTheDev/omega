# modules/http_channel

## Purpose

The HTTP/SSE delivery channel: exposes the agent over HTTP (chat
streaming, session CRUD, model metadata) and serves the embedded webui
SPA at `/`. This is the surface omega.endothe.dev routes to, and the
only unauthenticated network surface omega has - which makes its bind
rule a security contract, not a preference.

## Ownership

- `server.go` - HTTP server and handlers. Routes: `/health`, `/models`
  (context window cached per model after one provider probe),
  `/chat` (SSE), `/sessions` + `/sessions/{id}` (CRUD + label PUT +
  pinned PATCH; the GET response's messages additionally carry
  `created_at`, the store-level write time injected alongside `role`
  with no payload rewrite), `/files/browse` + `/files/read`
  (read-only surface for the webui inspector: browse and preview the
  process working directory - GET only, 64 KiB preview cap,
  dot-segment + absolute + symlink-containment guards), `/` (embedded
  SPA catch-all via `fs.Sub` - exact patterns win, so API routes keep
  working; the catch-all falls back to index.html for non-file paths -
  history-mode routes like `/chat/<id>` - and still 404s asset-like
  misses: anything under `/_nuxt/` or any dotted path. `spaHandler`
  in server.go wraps the FileServer). `/sessions` rows carry
  `message_count` + `tool_count` (SQL counts at read time - no
  counters, no schema change), `prompt_tokens` + `completion_tokens`
  (real usage accumulated per turn at persist, 0 for sessions saved
  before that landed), `model` (the last `model_change` entry, read at
  read time - omitted when the session never switched models), and
  `unread` (computed at read time from `updated_at` after `last_read_at` -
  no counters; GET `/sessions/{id}` exposes the same state on the
  response). PATCH `/sessions/{id}` accepts `{"pinned": true|false}` and
  `{"read": true}`; both are list concerns that write the sessions row
  WITHOUT touching `updated_at` - pinning and reading are not activity,
  and the webui time buckets read `updated_at`. GET
  `/sessions/search?q=<term>` exposes the store's FTS5 content search
  (one hit per session, best-rank first; malformed FTS query -> 400);
  the sidebar search unions it with the in-memory title filter. GET
  `/sessions/{id}/events` is the reattach surface: SSE subscribe to a
  session's live run (404 when not active, live-only frames for v1).
  GET/PUT `/settings` serve the Settings seam (typed:
  agent.SettingsSchema): GET returns the knob schema + values (no-store;
  a host without the seam answers 501; the surface filter drops knobs
  whose `surface` is not this channel's; secret values are masked -
  `provider.api_key` never round-trips), PUT applies a diff-only values
  map (unknown key -> 400; `server.bind` and `provider.api_key` are
  gated - config.yaml is their only door). Runs are
  DETACHED from the /chat request (runHub in runs.go): a client
  disconnect never cancels the run, one live run per session (second
  /chat -> 409), a wall-clock `server.run_timeout` (default 30m) ends
  hung runs. Session persistence:
  accumulate per response segment (a response chunk after tool results
  starts a new segment), persisted as assistant(A + tool calls) ->
  tool results -> assistant(B) - the provider-correct order and the
  shape the webui renders on reload. Session titles generate on first
  exchange via a detached second agent run (`context.WithoutCancel`)
  so a back-to-back chat never starves behind it.
- `plugin.go` - bind list construction. **Loopback only by default**;
  additional addresses (e.g. the Tailscale IP for Traefik) are an
  explicit `server.bind` opt-in.
- `config.go` - channel Config (Port, Bind, RunTimeout).
- `http_channel.go` - Channel seam implementation (`Serve` per addr).
- `static/` - the built webui SPA, embedded via `go:embed` and
  COMMITTED (synced by `modules/ui/scripts/build.sh`), so a plain
  `go build` always produces the complete product.
- `server_test.go` - handler tests (sessions CRUD, chat persistence,
  static index, method gates)
- `server_pure_test.go` - pure logic tests (tool selection, message
  decode, SSE events, segmented accumulation)
- `server_files.go` - read-only file surface handlers plus the path
  safety helpers (segment validation, symlink resolution, containment
  check, entry sort). No write path exists on this surface.
- `server_files_test.go` - file surface tests (browse shape + sort,
  read cap + truncation flag, binary flag, path rejections, method
  gates)
- `http_channel_test.go` - plugin mount tests (bind list, defaults)

## Local Contracts

- **Loopback only by default.** The API is unauthenticated and `/chat`
  can execute shell commands through the tool registry. The JSON
  decoder ignores Content-Type, so a cross-origin no-cors POST is
  exploitable from any browser that can reach the port - which is why
  the bind list never includes a LAN address, and extra binds (the
  Tailscale IP for Traefik routing) require an explicit config entry.
  The public edge is always behind an auth wall (Pocket ID
  forward-auth on every path - unlike postiz, there is NO open API
  sub-router; the API IS the sensitive surface).
- **SSE contract.** POST /chat with `{messages, session_id?, tools?,
  model?}` -> event stream: agent_start, turn_start, thinking_chunk,
  response_chunk, tool_call, tool_result, stream_end, turn_end,
  agent_end, error. Frames are `event: <name>\ndata: <json>\n\n`;
  each is flushed as written.
- **Persistence order is provider-correct.** user -> assistant(A +
  tool_calls + thinking) -> tool-role results -> assistant(B). A
  response chunk arriving after tool results starts a new segment;
  empty records (no content, thinking, or calls) are skipped. Old
  sessions saved before a fix keep their old shape - baked data is
  never rewritten.
- **Model override is per-run provider state.** `/chat` builds a
  copy via `Provider().WithModel(model)` and runs it through
  `RunWithProvider` - concurrent chats each keep their own model and
  the default stays untouched. Runs are parallel; the loop is
  stateless.
- **Title generation is detached.** It runs after the stream via
  `context.WithoutCancel` in a goroutine; failures are silent (an
  untitled session is cosmetic). Two rapid first-messages can race the
  label check - last write wins, acceptable at single-user load (see
  the `ponytail:` comment).

## Work Guidance

- Pure logic gets a test in `server_pure_test.go`; handler behavior in
  `server_test.go`. Non-trivial accumulation logic leaves a runnable
  check behind (the segmentation tests are the example).
- Webui changes that alter the SPA require `modules/ui/scripts/build.sh`
  and committing the synced `static/` output in the same change -
  the embedded bytes are the product, not a build artifact.
- A `go build` after touching `server.go` requires a serve restart to
  take effect (the binary is a snapshot of its build time).

## Verification

```bash
go test ./modules/http_channel/    # handler + pure + mount tests
go vet ./modules/http_channel/
python3 webui/scripts/stream_check.py http://127.0.0.1:8099   # live streaming probe
```

## Child DOX Index

No sub-packages.