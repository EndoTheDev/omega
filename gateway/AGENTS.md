# gateway

## Purpose

The gateway layer exposes the agent over HTTP. It runs the HTTP server,
streams agent lifecycle events to clients via Server-Sent Events, manages
session persistence in SQLite, and loads runtime configuration from YAML
and environment variables. It is the only layer external clients talk to.

## Ownership

- `server.go` - HTTP server, route handlers (`/health`, `/models`, `/chat`,
  `/sessions`, `/sessions/{id}`, `/static/`), SSE event mapping, request
  decoding, session-aware chat flow with message persistence
- `store.go` - SQLite session store: `Session`, `SessionNode`, `Store`,
  schema migration, session CRUD, message append/read, session tree,
  ancestor message walk, message encode/decode (including
  `model_change` and `thinking_level_change` entry types),
  `ComputeInsights` (cross-session analytics: `Insights`, `ToolStat`,
  `DayStat`, `NotableStat`; skips non-conversation entries).
  Compile-time assertion: `Store` implements `agent.SessionStore`.
- `config.go` - `Config` and sub-configs (including `PluginsConfig`),
  `LoadConfig` (YAML + env + defaults), `DefaultConfig`, `applyEnv`, `Validate`
- `config_test.go` - config loading and env override tests
- `server_test.go` - server endpoint and SSE streaming tests with a
  scripted mock provider
- `store_test.go` - store CRUD, tree, ancestor, and message tests
  (`:memory:` SQLite)
- `static/index.html` - embedded web frontend served at `/static/`

## Local Contracts

- **SSE is the only streaming transport.** The `/chat` endpoint sets
  `text/event-stream` and writes `event: <type>\ndata: <json>\n\n` frames,
  flushing after each one. No WebSocket, no long polling.
- **SSE event names are snake_case and fixed.** `agent_start`, `turn_start`,
  `response_chunk`, `thinking_chunk`, `tool_call`, `stream_end`,
  `assistant_message`, `tool_result`, `turn_end`, `agent_end`. New agent
  or stream event types must be mapped in `sseEvent` / `eventTypeOf` /
  `sseStreamEvent`.
- **StreamEvent is unwrapped before serialization.** `agent.StreamEvent`
  carries an `ai.StreamEvent` with `json:"-"`; the gateway emits the inner
  event under its own SSE type, not a generic wrapper.
- **Session persistence is optional.** A nil `Store` disables the
  `/sessions` endpoints (they return 501) and skips all message
  persistence in `/chat`.
- **Message persistence is user + final assistant only.** During a
  session-backed `/chat` run, incoming user messages are appended before
  the run and the accumulated final assistant response is appended after
  streaming completes. Intermediate tool-loop messages are not persisted.
- **Message wire format uses role discrimination.** Both `decodeMessages`
  (wire) and `encodeMessage`/`decodeMessage` (store) switch on the
  `role` field: `system`, `user`, `assistant`, `tool`, `model_change`,
  `thinking_level_change`. The store payload mirrors the wire JSON.
  Non-conversation entries (`model_change`, `thinking_level_change`)
  are skipped in `ComputeInsights` message/token counts.
- **Config layering is YAML, then env, then defaults.** `LoadConfig`
  starts from `DefaultConfig`, overlays YAML, applies `OMEGA_*` env
  overrides, then validates. `provider.model_name` and `server.port` are
  required after layering.
- **No re-exports.** Types from `agent` and `ai` are
  imported directly, not re-exported from this package.

## Work Guidance

- Add new HTTP routes in `NewServer` and implement the handler on
  `Server`. Use `http.NewServeMux` pattern routing (`/sessions/{id}`).
- When adding a new agent or stream event type, update the three SSE
  mapping functions in `server.go` (`sseEvent`, `sseStreamEvent`,
  `eventTypeOf`) or the event will serialize under a generic name.
- The store uses a single SQLite connection (`SetMaxOpenConns(1)`) to
  serialize writes and avoid `SQLITE_BUSY`. Switch to WAL plus a pool
  only if write contention becomes measurable.
- Schema migration in `migrate` uses `ALTER TABLE` with duplicate-column
  error tolerance for backward compatibility. Add new columns there.
- Session IDs are 16-byte random hex (`newSessionID`) with a timestamp
  fallback if `crypto/rand` fails. Do not introduce sequential IDs.
- `Serve` takes a context for graceful shutdown; signal wiring lives in
  `cmd/omega`, not here. Keep the 5-second shutdown timeout.
- Prefer stdlib for HTTP handling. The only non-stdlib dependency is
  `modernc.org/sqlite` (pure-Go SQLite driver) and `gopkg.in/yaml.v3`.

## Verification

```bash
go test ./gateway/   # unit + integration tests
go build ./...                 # everything compiles
go vet ./...                   # no suspicious constructs
```

## Child DOX Index

No sub-packages.
