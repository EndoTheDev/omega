# Ω omega

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
- **Tools** - `shell`, `read_file`, `write_file`, `edit`. Structured
  error returns, no panics.

> **Warning:** The shell tool executes commands the LLM generates with
> no sandboxing, allowlist, or confirmation prompt. The agent can read,
> modify, and delete files on your machine. When using cloud providers
> (OpenAI, Anthropic), file contents read by `read_file` and command
> output from `shell` are sent to the provider's API. Use Ollama for
> sensitive work.

- **Skills** - Load `SKILL.md` files from a `skills/` directory.
  Invoke via `/skill-name` slash commands or inline in messages.
- **Ephemeral sessions** - `/new --ephemeral` for throwaway
  conversations with no persistence.
- **Session auto-naming** - Generates a title from the first exchange
  using the active model.
- **Prompt history** - Up/Down recalls previous prompts.
- **Slash-command autocomplete** - Vertical dropup panel with
  two-level matching (commands + enum arguments).

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

### Configure

```bash
cp config.yaml.example config.yaml
```

Edit `config.yaml` to set your provider, model, and API key:

```yaml
provider:
  type: ollama # ollama, openai, or anthropic
  model_name: llama3 # required
  host: http://localhost:11434 # use https://ollama.com for Ollama Cloud
  api_key: # required for Ollama Cloud, OpenAI, and Anthropic
```

### Run

```bash
# Interactive TUI
./omega chat

# One-shot prompt
./omega run "explain channel-based event streams"

# HTTP server (SSE streaming, session store)
./omega serve

# Health check
./omega health
```

## Architecture

```txt
gateway (HTTP API) -> agent (loop + tools) -> ai (provider streaming)
```

| Layer    | Package            | Responsibility                                                                       |
| -------- | ------------------ | ------------------------------------------------------------------------------------ |
| Gateway  | `internal/gateway` | HTTP server, SSE streaming, session store (SQLite), config, session tree             |
| Agent    | `internal/agent`   | Multi-turn loop, tool execution, compaction, project context, system prompt, skills  |
| Provider | `internal/ai`      | Provider interface, Ollama + OpenAI + Anthropic, stream events, message types, retry |

No layer skips another. Events are typed structs, dispatched via type
switch. The provider layer emits events on a channel. The agent layer
consumes them and runs the tool loop. The gateway layer exposes
everything over HTTP.

## Configuration

All values can be set in `config.yaml` or overridden by environment
variables.

| Key                         | Env var                           | Default                  | Description                                                                                     |
| --------------------------- | --------------------------------- | ------------------------ | ----------------------------------------------------------------------------------------------- |
| `provider.type`             | `OMEGA_PROVIDER`                  | `ollama`                 | Provider: `ollama`, `openai`, `anthropic`                                                       |
| `provider.model_name`       | `OMEGA_MODEL`                     | `llama3`                 | Model name                                                                                      |
| `provider.host`             | `OMEGA_HOST`                      | `http://localhost:11434` | Provider base URL                                                                               |
| `provider.api_key`          | `OMEGA_API_KEY`                   |                          | API key (Ollama Cloud, OpenAI, Anthropic). Falls back to `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` |
| `server.port`               | `OMEGA_PORT`                      | `8099`                   | HTTP listen port                                                                                |
| `store.db_path`             | `OMEGA_DB_PATH`                   | `omega.db`               | SQLite database path                                                                            |
| `compaction.enabled`        |                                   | `true`                   | Enable context compaction                                                                       |
| `compaction.threshold`      | `OMEGA_COMPACTION_THRESHOLD`      | `0.6`                    | Fraction of context window that triggers compaction                                             |
| `compaction.context_window` | `OMEGA_COMPACTION_CONTEXT_WINDOW` | `32768`                  | Model context window in tokens                                                                  |
| `compaction.keep_first`     |                                   | `2`                      | Messages preserved verbatim at start                                                            |
| `compaction.keep_last`      |                                   | `10`                     | Messages preserved verbatim at end                                                              |

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

## TUI Commands

| Command                               | Description                                          |
| ------------------------------------- | ---------------------------------------------------- |
| `/new [--ephemeral]`                  | Start a new session (`--ephemeral` = no persistence) |
| `/sessions`                           | List persisted sessions as a table                   |
| `/sessions delete <# \| id \| label>` | Delete a session                                     |
| `/resume <# \| id \| label>`          | Resume a session by line number, ID, or label        |
| `/branch [id]`                        | Branch a new session from the current (or given) one |
| `/label [text]`                       | Set or clear the current session's label             |
| `/tree`                               | Show the session tree                                |
| `/model <name>`                       | Switch the model at runtime                          |
| `/provider <type>`                    | Switch the provider at runtime                       |
| `/compact [focus]`                    | Manually compact conversation history                |
| `/copy`                               | Copy last message to clipboard                       |
| `/thinking [on \| off]`               | Toggle thinking block visibility                     |
| `/tools [on \| off \| auto]`          | Toggle tool result display mode                      |
| `/exit`                               | Quit                                                 |

## Project Structure

```txt
cmd/omega/        Single binary entry point (serve, run, health, chat)
internal/ai/      Provider abstraction, stream events, message types, retry
internal/agent/   Multi-turn loop, tool execution, compaction, skills
internal/gateway/ HTTP server, SSE streaming, session store, config
agents/           Commit conventions (COMMIT.md)
skills/           Skill files (SKILL.md), loaded at startup
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

## Status

Early alpha. All four layers are implemented and tested. The TUI is
fully functional. Expect breaking changes - this project does not
maintain backwards compatibility.

### What works

- Three providers with streaming, retry, and backoff
- Multi-turn agent loop with tool execution
- Session tree with branching, labeling, and full persistence
- Context compaction with overflow auto-retry
- Skills system with slash-command and inline invocation
- Complete TUI with streaming, markdown, autocomplete, and history

### What is planned

- Desktop notifications
- More tools
- Web UI (via the gateway HTTP API)

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
