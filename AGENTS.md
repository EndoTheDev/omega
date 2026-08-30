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
- **Entry point:** `omega` (single binary: `serve`, `run`, `health`, `chat`,
  `export`, `update`, `insights`;
  no arg → TUI, `<path>` → chdir + TUI)

## Architecture

```txt
HTTP channel → agent (loop + tools) → ai (provider streaming)
    │
    └── extensions/ (in-process plugins mounted into Context)
```

Each layer has a single responsibility. No layer skips over another.
Events are typed structs, dispatched via type switch. The provider
layer emits events on a channel. The agent layer consumes them and
runs the tool loop. The HTTP channel exposes the agent over HTTP.
Extensions are in-process Go packages that mount into a shared Context
via the Plugin interface.

## Local Contracts

- **No layer skipping.** Each layer imports only from the layer
  directly below it. The `chat` subcommand (TUI) imports internal
  packages for in-process streaming. The `serve` subcommand exposes
  everything over HTTP for external clients. External clients talk to
  the HTTP channel only.
- **No re-exports at intermediate layers.** If a type is defined in
  a layer, consumers import it from that layer.
- **`model_name` everywhere.** Provider references use `model_name`,
  not `model` or `provider_model`.
- **Tool errors are structured returns.** Tools do not panic into
  the agent. They return a structured error response.
- **No backwards compatibility.** Breaking changes are normal. They
  are not marked with `!` in commit messages (see `.agents/COMMIT.md`).
- **Secrets never committed.** `.env` is gitignored. See `.gitignore`.
- **Extensions are in-process.** All extensions are Go packages under
  `extensions/`. No stdio, no IPC, no separate processes. The host
  calls `MountAll` at startup; the agent loop reads from `Context`.

## Read Before Editing

1. `.agents/COMMIT.md` - commit convention and voice.
2. Check the Child DOX Index below for the layer you are editing.
3. If a layer has an AGENTS.md, read it before touching its code.

## Update After Editing

1. If you add, remove, or rename a symbol referenced in any AGENTS.md,
   update that AGENTS.md in the same change.
2. If you add a new package directory with non-trivial code, create an
   AGENTS.md for it and add it to the Child DOX Index.
3. Run the relevant tests before declaring done.

## Work Guidance

- Voice, commit format: `.agents/COMMIT.md`.
- Dependencies: add only when a layer requires them. Prefer the
  standard library. Prefer a dependency already in `go.mod`.
- Go: 1.26.5. Build with `go build`, test with `go test ./...`.

## Verification

Each non-trivial package leaves a `_test.go` file behind - the
Go equivalent of an assert-based self-check. No frameworks until
there is a reason for one.

```bash
go test ./...     # all layer tests
go build ./...    # all packages compile
go vet ./...      # no suspicious constructs
```

## Hierarchy

```txt
AGENTS.md (root - this file)
├── build.sh                   # build script (Linux/macOS): vet + test + build to bin/
├── build.bat                  # build script (Windows): vet + test + build to bin\
├── .agents/                   # conventions (COMMIT.md)
├── docs/                      # user documentation (tracked)
├── ai/                        # provider contract (interface, messages, events), shared HTTP infra, ThinkingLevels, HTTP timeout (OMEGA_HTTP_TIMEOUT)
├── agent/                     # multi-turn loop contract (LoopProvider interface), tool execution, compaction (config + token estimation), capability seams (CompactionProvider, ToolProvider, LoopProvider, StoreProvider, SkillsProvider, LoggerProvider, MemoryProvider, PromptBuilder, Channel, Frontend + FrontendOptions), Plugin system (Context, Plugin, MountAll); events: AgentStart, TurnStart, TurnEnd, AgentEnd, StreamEvent, AssistantMessageEvent, ToolResultEvent; shared data types (Session, SessionNode, Skill, Insights, SearchResult, ExtensionInfo, ExtensionCommand, InjectedMessage, PromptBuildOptions, Configs map)
├── extensions/                # in-process extension packages (importable Go packages)
│   ├── README.md              # extension authoring guide
│   ├── agent_loop/            # default conversation loop (LoopProvider implementation)
│   ├── provider/              # LLM provider (Ollama, OpenAI, Anthropic streaming)
│   ├── store/                 # session store (SQLite implementation, sessions.search tool)
│   ├── skills/                # skill loading (skills.read tool, /skills command)
│   ├── compactor/             # context compaction (keep-first/last + LLM summarize middle)
│   ├── logging/               # operational logging (file logger, LoggerProvider seam)
│   ├── memory/                # persistent memory (memory tool, two-file §-delimited store)
│   ├── http_channel/          # HTTP/SSE delivery channel (Channel seam, omega serve transport)
│   ├── tui/                   # terminal frontend (Frontend seam, Bubble Tea TUI, themes, @file input)
│   ├── prompt/                # system prompt builder + guidelines
│   ├── tools/                 # shell and file tools (shell.run, files.read, files.write, files.edit)
│   ├── mcp/                   # MCP bridge (stdio + HTTP MCP servers)
│   ├── delegate/              # subagent delegation (delegate.task, delegate.status, InjectedMessages)
│   └── web/                   # web search/fetch (Ollama Cloud API)
├── bin/                       # runtime directory (gitignored except templates)
│   ├── omega.exe              # built binary (gitignored)
│   ├── config.yaml            # personal config (gitignored)
│   ├── config.yaml.example    # config template (tracked)
│   ├── mcp.yaml.example       # MCP bridge config template (tracked)
│   ├── omega.db               # session database (gitignored)
│   ├── trust.yaml             # trust state (gitignored)
│   └── skills/                # skill templates (tracked)
└── cmd/
    └── omega/                 # single binary: serve, run, health, chat (no arg → TUI, `<path>` → chdir + TUI); project trust (--approve/--no-approve, trust.yaml); export, update, insights; `@file` image input; /new --ephemeral, /sessions (table, delete, resume by #/label/id), /tree (table), /copy, /export, /search, /insights, /thinking, /tools, /extensions, /skills, /theme, /models, /model (number or name)
```

## Child Doc Shape

Every child AGENTS.md must follow this section order:

1. **Purpose** - what this layer does in one paragraph.
2. **Ownership** - what files and directories it owns.
3. **Local Contracts** - rules specific to this layer.
4. **Work Guidance** - conventions, patterns, pitfalls.
5. **Verification** - how to check this layer's code.
6. **Child DOX Index** - sub-directories with AGENTS.md, if any.

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

- Commit voice: see `.agents/COMMIT.md` - the sole authority on voice.
- Approve-then-apply: present a plan, wait for "lgtm", then act.

## Child DOX Index

| Path          | Status      | What it owns                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| ------------- | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `.agents/`    | Reference   | Commit conventions (COMMIT.md)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| `ai/`         | Implemented | Provider contract (interface, messages, events, tool schema), shared HTTP infra (httpClient, retryHTTP), exported HTTPClient / RetryHTTP / SSEData for extension use, ThinkingLevels, HTTP timeout (OMEGA_HTTP_TIMEOUT), `DetectImage` (magic-byte image detection)                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| `agent/`      | Implemented | Multi-turn loop contract (`LoopProvider` interface), tool execution, compaction (config + token estimation, overflow auto-retry, friendly error when no compactor), capability seams (`CompactionProvider`, `ToolProvider`, `LoopProvider`, `StoreProvider`, `SkillsProvider`, `LoggerProvider`, `MemoryProvider`, `PromptBuilder`, `Channel`, `Frontend`), Plugin system (`Context`, `Plugin`, `MountAll`); events: AgentStart, TurnStart, TurnEnd, AgentEnd, StreamEvent, AssistantMessageEvent, ToolResultEvent; shared data types (Session, SessionNode, Skill, Insights, SearchResult, ExtensionInfo, ExtensionCommand, InjectedMessage, PromptBuildOptions, Configs map). Loop implementation in `extensions/agent_loop/` |
| `cmd/omega/`  | Implemented | Single binary: serve, run, health, chat (Frontend dispatch), export, update, insights; project trust (--approve/--no-approve, trust.yaml); `@file` image input; config loading, plugin wiring, `omegaHome` resolution                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `bin/`        | Runtime     | Build output, config, database, skills. Gitignored except `config.yaml.example`, `mcp.yaml.example`, `skills/` (templates). Build via `./build.sh` or `build.bat`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| `extensions/` | Implemented | In-process extension packages: agent_loop (default conversation loop), provider (Ollama, OpenAI, Anthropic), store (SQLite + sessions.search), skills (skill loading + /skills command), compactor (context compaction), logging (file logger + LoggerProvider seam), memory (persistent memory + memory tool), http_channel (HTTP/SSE delivery channel), tui (terminal frontend), prompt (system prompt builder), tools (shell, files), mcp (MCP bridge), delegate (subagent delegation), web (web search/fetch). Each implements the `agent.Plugin` interface.                                                                                                                                                                |
