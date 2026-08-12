# AGENTS.md - omega

## Purpose

omega is a Go port of the Pi/Tau event-stream agent architecture.
Three layers, one job each. Events are the contract. The whole thing is
readable as a textbook.

This is a fresh implementation, not a port of agent.d. No RSI, no
self-awareness, no evolution tracking. Just a clean event-stream agent
in Go.

## Ownership

- **Repository:** `D:\Code\ideas\omega-dev-golang\omega-dev`
- **Language:** Go 1.26.5
- **Module:** `github.com/EndoTheDev/omega`
- **Dependencies:** Added when a layer requires them, not before.
  Prefer the standard library.
- **Entry point:** `omega` (single binary: `serve`, `run`, `health`, `chat`)

## Architecture

```txt
gateway (HTTP API) → agent (loop + tools) → ai (provider streaming)
```

Each layer has a single responsibility. No layer skips over another.
Events are typed structs, dispatched via type switch. The provider
layer emits events on a channel. The agent layer consumes them and
runs the tool loop. The gateway layer exposes everything over HTTP.

## Local Contracts

- **No layer skipping.** Each layer imports only from the layer
  directly below it. The `chat` subcommand (TUI) imports internal
  packages for in-process streaming. The `serve` subcommand exposes
  everything over HTTP for external clients. External clients talk to
  the gateway over HTTP only.
- **No re-exports at intermediate layers.** If a type is defined in
  a layer, consumers import it from that layer.
- **`model_name` everywhere.** Provider references use `model_name`,
  not `model` or `provider_model`.
- **Tool errors are structured returns.** Tools do not panic into
  the agent. They return a structured error response.
- **No backwards compatibility.** Breaking changes are normal. They
  are not marked with `!` in commit messages (see `agents/COMMIT.md`).
- **Secrets never committed.** `.env` is gitignored. See `.gitignore`.

## Read Before Editing

1. `agents/COMMIT.md` — commit convention and voice.
2. Check the Child DOX Index below for the layer you are editing.
3. If a layer has an AGENTS.md, read it before touching its code.

## Update After Editing

1. If you add, remove, or rename a symbol referenced in any AGENTS.md,
   update that AGENTS.md in the same change.
2. If you add a new package directory with non-trivial code, create an
   AGENTS.md for it and add it to the Child DOX Index.
3. Run the relevant tests before declaring done.

## Work Guidance

- Voice, commit format: `agents/COMMIT.md`.
- Dependencies: add only when a layer requires them. Prefer the
  standard library. Prefer a dependency already in `go.mod`.
- Go: 1.26.5. Build with `go build`, test with `go test ./...`.

## Verification

Each non-trivial package leaves a `_test.go` file behind — the
Go equivalent of an assert-based self-check. No frameworks until
there is a reason for one.

```bash
go test ./...     # all layer tests
go build ./...    # all packages compile
go vet ./...      # no suspicious constructs
```

## Hierarchy

```txt
AGENTS.md (root — this file)
├── agents/                   # conventions (COMMIT.md)
├── internal/
│   ├── ai/                   # provider abstraction, stream events, message types, retry, multi-provider, API key auth (Ollama Cloud, OpenAI, Anthropic)
│   ├── agent/                # multi-turn loop, tool execution, compaction (threshold + overflow auto-retry), project context, system prompt, skills; events: AgentStart, TurnStart, TurnEnd, AgentEnd (carries assistant message), StreamEvent, AssistantMessageEvent, ToolResultEvent
│   └── gateway/              # HTTP server, SSE streaming, session store, config, session tree; SSE events: agent_start, turn_start, response_chunk, thinking_chunk, tool_call, stream_end, assistant_message, tool_result, turn_end, agent_end
└── cmd/
    └── omega/                # single binary: serve, run, health, chat; /new --ephemeral, /sessions (table, delete, resume by #/label/id), /tree (table), /copy, /thinking, /tools
```

## Child Doc Shape

Every child AGENTS.md must follow this section order:

1. **Purpose** — what this layer does in one paragraph.
2. **Ownership** — what files and directories it owns.
3. **Local Contracts** — rules specific to this layer.
4. **Work Guidance** — conventions, patterns, pitfalls.
5. **Verification** — how to check this layer's code.
6. **Child DOX Index** — sub-directories with AGENTS.md, if any.

No child doc may weaken the root contract. A child may add local
rules but cannot override core contracts.

## Style

- Markdown is linted. Wrap bare URLs in angle brackets:
  `<https://example.com>`.
- No diary entries or TODO comments in AGENTS.md. Keep docs factual
  and contract-focused.
- No emoji in AGENTS.md.
- Descriptive variable names in public APIs. Short names OK for
  local variables (Go convention).
- No long dashes. Use normal hyphens (`-`).

## User Preferences

- Commit voice: see `agents/COMMIT.md` — the sole authority on voice.
- Approve-then-apply: present a plan, wait for "lgtm", then act.

## Child DOX Index

| Path                | Status      | What it owns                                                                                                                                                                                                                                             |
| ------------------- | ----------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `agents/`           | Reference   | Commit conventions (COMMIT.md)                                                                                                                                                                                                                           |
| `internal/ai/`      | Implemented | Provider abstraction, stream events, message + tool types, retry, multi-provider, API key auth (Ollama Cloud, OpenAI, Anthropic)                                                                                                                         |
| `internal/agent/`   | Implemented | Multi-turn loop, tool execution, compaction (threshold + overflow auto-retry), project context, system prompt, skills; events: AgentStart, TurnStart, TurnEnd, AgentEnd (carries assistant message), StreamEvent, AssistantMessageEvent, ToolResultEvent |
| `internal/gateway/` | Implemented | HTTP server, SSE streaming, session store, config, session tree; SSE events: agent_start, turn_start, response_chunk, thinking_chunk, tool_call, stream_end, assistant_message, tool_result, turn_end, agent_end                                         |
| `cmd/omega/`        | Implemented | Single binary: serve, run, health, chat; /new --ephemeral, /sessions (table, delete, resume by #/label/id), /tree (table), /copy, /thinking, /tools                                                                                                      |
