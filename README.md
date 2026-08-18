# Ω omega

![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)
![License](https://img.shields.io/badge/license-MIT-blue)
![Status](https://img.shields.io/badge/status-WIP-orange)

omega is a terminal-based AI assistant that can read your files, run
commands, and edit code. It talks to LLM providers (Ollama, OpenAI,
Anthropic) and streams responses in real time. It ships as a single
binary with a full-screen TUI, persistent session tree, context
compaction, and a tool loop.

This is a Go port of the [Pi](https://github.com/earendil-works/pi)
(TypeScript) and [Tau](https://github.com/huggingface/tau) (Python)
event-stream agent architecture. It leans into Go's strengths: channels
for event streams, `context.Context` for cancellation, interfaces for
provider abstraction, and the standard library for everything HTTP.

## Features

- **Three providers** - Ollama (local), OpenAI, Anthropic. Switch at
  runtime via `/model` and `/provider` in the TUI.
- **Full-screen TUI** - Bubble Tea + Lipgloss + Glamour. Streaming
  responses, markdown rendering, thinking blocks, tool output.
- **Session tree** - Branch, label, and resume sessions. Full
  persistence to SQLite (messages, tool results, thinking).
- **Context compaction** - Summarizes old messages when the
  conversation nears the context window. Configurable threshold and
  context window size.
- **Overflow recovery** - Detects context overflow errors and
  auto-compacts + retries once.
- **Tools** - `shell`, `read_file`, `write_file`, `edit`, `load_skill`. Structured
  error returns, no panics.
- **Extensions** - Load external tools via JSON-RPC over stdio. Each
  extension is a separate process with crash isolation.
- **Skills** - Self-contained skill directories (`<name>/<name>.md`)
  with frontmatter. The agent can load skill content on demand via the
  `load_skill` tool, or the user can invoke via `/skill-name` slash
  commands. Each skill folder can hold its own scripts, references,
  and templates.
- **Ephemeral sessions** - `/new --ephemeral` for throwaway
  conversations with no persistence.
- **Session auto-naming** - Generates a title from the first exchange
  using the active model.
- **Prompt history** - Up/Down recalls previous prompts.
- **Slash-command autocomplete** - Vertical dropup panel with
  two-level matching (commands + enum arguments). Triggers mid-sentence
  on any `/` after a space.

> **Warning:** The shell tool executes commands the LLM generates with
> no sandboxing, allowlist, or confirmation prompt. The agent can read,
> modify, and delete files on your machine. When using cloud providers
> (OpenAI, Anthropic), file contents read by `read_file` and command
> output from `shell` are sent to the provider's API. Use Ollama for
> sensitive work.

## Quick Start

### Prerequisites

- Go 1.26.5+
- An LLM provider:
  - [Ollama](https://ollama.com) - local (free, default) or Ollama Cloud (API key)
  - OpenAI API key
  - Anthropic API key

### Build

```bash
git clone https://github.com/EndoTheDev/omega.git
cd omega
go build -o omega ./cmd/omega
```

On Windows the build produces `omega.exe` - use `.\omega.exe` instead of
`./omega` in the commands below.

### Install

Install with `go install` (requires Go 1.26.5+):

```bash
go install github.com/EndoTheDev/omega/cmd/omega@latest
```

The binary is placed in `$GOPATH/bin` (typically already in your PATH).

Or build from source:

Add the build directory to your `PATH`. omega resolves config, skills,
extensions, and the session database from the directory containing the
binary. Your working directory is used only for AGENTS.md project
context and tool file operations.

```bash
# Example: add to PATH in your shell profile
export PATH="$PATH:/path/to/omega"
```

### Configure

```bash
cp config.yaml.example config.yaml
```

Edit `config.yaml` to set your provider, model, and API key:

```yaml
provider:
  type: ollama # ollama, openai, or anthropic
  model_name: # required - e.g. llama3, gpt-4o, claude-sonnet-4-20250514
  host: http://localhost:11434 # use https://ollama.com for Ollama Cloud
  api_key: # required for Ollama Cloud, OpenAI, and Anthropic
```

### Run

```bash
# Interactive TUI (default - no subcommand needed)
./omega

# Interactive TUI in a specific project directory
./omega /path/to/project

# One-shot prompt
./omega run "explain channel-based event streams"

# One-shot with image input (vision models)
./omega run @screenshot.png "what's wrong with this code?"

# HTTP server (SSE streaming, session store)
./omega serve

# Export a session as JSONL (by ID or label)
./omega export <session-id-or-label> [output-path]
./omega export my-session -    # stdout

# Session usage analytics (last N days, default: 30)
./omega insights --days 7

# Self-update from GitHub releases
./omega update

# Health check
./omega health

# Show help
./omega --help

# Show version
./omega --version
```

`omega` with no argument starts the TUI. `omega <path>` chdirs into that
directory first, so AGENTS.md discovery, project context, and tool file
operations resolve relative to it. Subcommand names (`serve`, `run`,
`health`, `chat`) always win over a same-named directory.

## Architecture

```txt
gateway (HTTP API) -> agent (loop + tools) -> ai (provider streaming)
```

| Layer    | Package            | Responsibility                                                                                  |
| -------- | ------------------ | ----------------------------------------------------------------------------------------------- |
| Gateway  | `internal/gateway` | HTTP server, SSE streaming, session store (SQLite), config, session tree                        |
| Agent    | `internal/agent`   | Multi-turn loop, tool execution, compaction, project context, system prompt, skills, extensions |
| Provider | `internal/ai`      | Provider interface, Ollama + OpenAI + Anthropic, stream events, message types, retry            |

No layer skips another. Events are typed structs, dispatched via type
switch. The provider layer emits events on a channel. The agent layer
consumes them and runs the tool loop. The gateway layer exposes
everything over HTTP.

## Configuration

All values can be set in `config.yaml` or overridden by environment
variables. When omega is installed globally (in PATH), it looks for
`config.yaml`, `omega.db`, `skills/`, and `extensions/` in the binary's
directory (or `OMEGA_HOME`). The working directory is used only for
AGENTS.md project context and tool file operations.

| Key                          | Env var                            | Default                  | Description                                                                                     |
| ---------------------------- | ---------------------------------- | ------------------------ | ----------------------------------------------------------------------------------------------- |
| -                            | `OMEGA_HOME`                       | Binary directory         | Omega home: config, db, skills, extensions live here                                            |
| `provider.type`              | `OMEGA_PROVIDER`                   | `ollama`                 | Provider: `ollama`, `openai`, `anthropic`                                                       |
| `provider.model_name`        | `OMEGA_MODEL`                      | (required)               | Model name                                                                                      |
| `provider.host`              | `OMEGA_HOST`                       | `http://localhost:11434` | Provider base URL                                                                               |
| `provider.api_key`           | `OMEGA_API_KEY`                    |                          | API key (Ollama Cloud, OpenAI, Anthropic). Falls back to `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` |
| `server.port`                | `OMEGA_PORT`                       | `8099`                   | HTTP listen port                                                                                |
| `store.db_path`              | `OMEGA_DB_PATH`                    | `<home>/omega.db`        | SQLite database path                                                                            |
| `compaction.enabled`         |                                    | `true`                   | Enable context compaction                                                                       |
| `compaction.threshold`       | `OMEGA_COMPACTION_THRESHOLD`       | `0.6`                    | Fraction of context window that triggers compaction                                             |
| `compaction.context_window`  | `OMEGA_COMPACTION_CONTEXT_WINDOW`  | `32768`                  | Model context window in tokens                                                                  |
| `compaction.keep_first`      | `OMEGA_COMPACTION_KEEP_FIRST`      | `2`                      | Messages preserved verbatim at start                                                            |
| `compaction.keep_last`       | `OMEGA_COMPACTION_KEEP_LAST`       | `10`                     | Messages preserved verbatim at end                                                              |
| `compaction.reserve_tokens`  | `OMEGA_COMPACTION_RESERVE_TOKENS`  | `16384`                  | Tokens reserved for the model response                                                          |
| `compaction.max_tool_output` | `OMEGA_COMPACTION_MAX_TOOL_OUTPUT` | `32768`                  | Maximum bytes of tool output before truncation                                                  |
| `extensions.enabled`         | `OMEGA_EXTENSIONS_ENABLED`         | `false`                  | Enable extension loading                                                                        |
| `extensions.dir`             | `OMEGA_EXTENSIONS_DIR`             | `<home>/extensions`      | Directory to scan for extension executables                                                     |
| `skills.dir`                 | `OMEGA_SKILLS_DIR`                 | `<home>/skills`          | Skills directory                                                                                |
| `http_timeout`               | `OMEGA_HTTP_TIMEOUT`               | `300`                    | HTTP timeout for provider requests (seconds)                                                    |
| `theme`                      | `OMEGA_THEME`                      | `dark`                   | TUI color theme (dark, light, auto)                                                             |
| `notifications`              | `OMEGA_NOTIFICATIONS`              | `bell`                   | Turn-complete notification (bell, desktop, off)                                                 |

## Project Trust

omega loads `AGENTS.md` files from the working directory up to the
filesystem root and injects them into the system prompt. Because those
files can contain instructions, omega gates them behind a trust check.

The trust unit is the nearest directory (walking up from cwd) containing
an `AGENTS.md`. Trust decisions are stored in `<home>/trust.yaml`:

```yaml
trusted:
  - path: /home/user/Code
    level: parent # trust this directory and everything under it
  - path: /home/user/Code/specific-repo
    level: exact # trust this directory only
```

Behavior:

- **TUI** - an untrusted project prompts `Trust files in <dir>? [y/N]`.
- **`run`/`serve`** - an untrusted project skips context with a warning.
- **`--approve`** - trust the current project (records an `exact` entry).
- **`--no-approve`** - skip the current project's context. Wins over
  `--approve`.

## Providers

| Provider       | Type        | Requires      | Default Host                   |
| -------------- | ----------- | ------------- | ------------------------------ |
| Ollama (local) | `ollama`    | Local install | `http://localhost:11434`       |
| Ollama Cloud   | `ollama`    | API key       | `https://ollama.com`           |
| OpenAI         | `openai`    | API key       | `https://api.openai.com/v1`    |
| Anthropic      | `anthropic` | API key       | `https://api.anthropic.com/v1` |

Ollama supports three connection modes:

- **Local** - Default. Set `host: http://localhost:11434`, leave
  `api_key` empty.
- **Cloud via local proxy** - Keep `host` as localhost. Your local
  Ollama instance handles cloud auth transparently (e.g.
  `ollama run gpt-oss:120b-cloud`).
- **Cloud direct** - Set `host: https://ollama.com` and
  `api_key: <your-key>`. omega sends a Bearer token in the
  Authorization header.

Switch providers at runtime in the TUI:

```txt
/provider openai
/model gpt-4o
```

## Extensions

Extensions are external processes that provide tools and event
subscriptions to omega. Each extension runs as a separate process and
communicates via JSON-RPC over stdio. A crash in one extension does not
affect others or the host.

### Enabling

```yaml
extensions:
  enabled: true
  dir: extensions # relative to omega home, or absolute path
```

Extensions can also be controlled from the command line:

```bash
./omega chat --extension ./my-ext            # load a specific extension (repeatable)
./omega chat -e ./my-ext                     # short form
./omega chat --no-extensions                 # disable extension loading entirely
./omega chat --project-extensions            # also load <cwd>/.omega/extensions/
```

`--no-extensions` wins over everything. `--extension`/`-e` and
`--project-extensions` each force extensions on even when
`extensions.enabled` is `false`.

### Example: Web Extension

omega ships an example extension in `extensions/example/` that provides
web search and fetch tools via the [Ollama Cloud API](https://ollama.com).

```bash
# Build the example extension
go build -o extensions/example/example.exe ./extensions/example/

# Enable extensions in config.yaml (see above), then:
./omega chat
# The web extension provides web.search and web.fetch tools.
# Ask the agent to search the web and it will call them as tool calls.
```

The extension receives the provider API key via the `OLLAMA_API_KEY`
environment variable, passed by the host from `config.yaml`.

### Building Custom Extensions

An extension is any executable that speaks JSON-RPC over stdio:

1. On startup, receive an `initialize` request. Respond with your
   extension name, tools, and event subscriptions.
2. Receive `tool_call` requests when the agent invokes your tools.
3. Receive `event` notifications for subscribed lifecycle events
   (`agent_start`, `turn_start`, `turn_end`, `assistant_message`,
   `tool_result`, `agent_end`).
4. Receive a `shutdown` notification on exit.

See `extensions/README.md` for the full protocol reference and
`extensions/example/main.go` for a complete implementation.

## TUI Commands

| Command                               | Description                                                                                           |
| ------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| `/new [--ephemeral]`                  | Start a new session (`--ephemeral` = no persistence)                                                  |
| `/sessions`                           | List persisted sessions as a table                                                                    |
| `/sessions delete <# \| id \| label>` | Delete a session                                                                                      |
| `/resume <# \| id \| label>`          | Resume a session by line number, ID, or label                                                         |
| `/branch [id]`                        | Branch a new session from the current (or given) one                                                  |
| `/label [text]`                       | Set or clear the current session's label                                                              |
| `/tree`                               | Show the session tree                                                                                 |
| `/model <# \| name>`                  | Switch the model (line # from /models, or name)                                                       |
| `/models`                             | List available models from the current provider                                                       |
| `/provider <type>`                    | Switch the provider at runtime                                                                        |
| `/compact [focus]`                    | Manually compact conversation history                                                                 |
| `/copy`                               | Copy last message to clipboard                                                                        |
| `/export [path]`                      | Export session messages to JSONL                                                                      |
| `/insights [days]`                    | Show cross-session usage analytics (default: 30 days)                                                 |
| `/thinking [level]`                   | Set thinking level (none, off, on, minimal, low, medium, high, extra high, max, ultra; no arg cycles) |
| `/tools [on \| off \| auto]`          | Toggle tool result display mode                                                                       |
| `/extensions`                         | List loaded extensions                                                                                |
| `/skills`                             | List loaded skills                                                                                    |
| `/theme [name]`                       | Switch theme (dark, light, auto; no arg lists all)                                                    |
| `/help`                               | Show help                                                                                             |
| `/exit`                               | Quit                                                                                                  |

### Keybindings

| Key       | Action                                            |
| --------- | ------------------------------------------------- |
| Enter     | Send message (or accept autocomplete match)       |
| Ctrl+J    | Insert newline (multi-line input)                 |
| Ctrl+P    | Cycle to next model (fetches model list if empty) |
| Tab       | Accept autocomplete match                         |
| Up/Down   | Cycle autocomplete / recall prompt history        |
| PgUp/PgDn | Scroll transcript                                 |
| Esc       | Cancel running turn / close autocomplete          |
| Ctrl+C    | Quit                                              |

File paths pasted or dragged into the terminal are inserted into the
prompt as text (bracketed paste support).

## Project Structure

```txt
cmd/omega/        Single binary entry point (serve, run, health, chat)
internal/ai/      Provider abstraction, stream events, message types, retry
internal/agent/   Multi-turn loop, tool execution, compaction, skills, extensions
internal/gateway/ HTTP server, SSE streaming, session store, config
agents/           Commit conventions (COMMIT.md)
skills/           Skill directories (<name>/<name>.md), loaded at startup
extensions/       Extension binaries (JSON-RPC over stdio)
config.yaml       Configuration (copy from config.yaml.example)
```

## Development

```bash
go build ./...    # compile all packages
go test ./...     # run all tests
go vet ./...      # static analysis
```

Each package includes test files with no external test framework -
just the Go testing package. Tests are deterministic via a fake
provider that scripts stream events.

## Roadmap

### Done

- Three providers with streaming, retry, and backoff
- Multi-turn agent loop with tool execution
- Session tree with branching, labeling, and full persistence
- Context compaction with overflow auto-retry, reserve tokens, and branch summarization
- Skills system with folder-per-skill, agent-driven `load_skill` tool, and slash-command invocation
- Extension system with JSON-RPC over stdio, crash isolation, event dispatch
- Complete TUI with streaming, markdown, autocomplete, and history
- Global installation via PATH with binary-dir resolution
- 10-level thinking control across all providers
- Model discovery (`/models` command, `/model <#|name>` selection)
- HTTP proxy support (`HTTP_PROXY`, `HTTPS_PROXY`)
- AGENTS.md ancestor walk (CWD to root, concatenated)
- Resource diagnostics (warnings for unreadable context files)
- Prompt guidelines (deduplicated bullets in system prompt)
- Tool result truncation (configurable max bytes)
- Session export (`/export` writes JSONL)
- Extension CLI flags (`--extension`/`-e`, `--no-extensions`, `--project-extensions`)
- Desktop notifications (`notifications` config: bell, desktop, off)
- Model quick-cycle (Ctrl+P)
- File drop (bracketed paste support)
- Export session subcommand (`omega export`)
- Self-update (`omega update`)
- Image input (`@file` args with vision models)
- Session insights (`omega insights [--days N]`, `/insights [days]`)

### Planned

- Desktop notifications
- More tools (grep, glob, multi-file edit)
- More providers (Gemini, Mistral)
- Web UI (via the gateway HTTP API)
- Project trust system for per-project skills and extensions
- Extension hooks (pre/post on input, tool_call, tool_result)
- Prompt templates with variable interpolation
- Session stats and entry types

### Known Limitations

- `omega serve` has no authentication, TLS, or CORS - do not expose it
  on a public network
- `omega health` only checks HTTP 200 on `/health` - does not probe the
  provider or database
- Compaction is irreversible - summarized messages cannot be restored
  to their original form
- No concurrent session safety across HTTP requests
- SQLite uses a pure-Go driver (`modernc.org/sqlite`, no CGO) but is
  untested under heavy load

Report issues at <https://github.com/EndoTheDev/omega/issues>

## License

[MIT](LICENSE)
