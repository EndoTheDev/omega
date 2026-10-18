# Architecture

```txt
webui (Nuxt SPA, embedded) -> HTTP channel -> agent (loop + tools) -> ai (provider streaming)
```

| Layer      | Package       | Responsibility                                                                                                                                          |
| ---------- | ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Agent      | `agent`       | Multi-turn loop, parallel tool execution, compaction, capability seams, Plugin system (Context, Plugin, MountAll)                                       |
| Provider   | `ai`          | Provider interface, Ollama + OpenAI + Anthropic, stream events, message types, retry                                                                    |
| CLI        | `cmd/omega`   | Entry point, TUI, project context, trust gate, config loading (YAML + env + defaults), hot-reload                                                       |
| Modules    | `modules/` | 15 in-process Go packages: agent_loop, provider, store, skills, compactor, logging, memory, prompt, tools, mcp, delegate, web, http_channel, tui, trust |

No layer skips another. Events are typed structs, dispatched via type
switch. The provider layer emits events on a channel. The agent layer
consumes them and runs the tool loop. The HTTP channel exposes
everything over HTTP and serves the embedded webui SPA (built by
modules/ui/scripts/build.sh, committed in modules/http_channel/static/,
baked in at compile time).

## Project Structure

```txt
cmd/omega/        Single binary entry point (serve, run, health, chat, update)
cmd/omega/examples/  Config templates, embedded for first-run bootstrap
ai/               Provider abstraction, stream events, message types, retry
agent/            Multi-turn loop, tool execution, seams, Plugin system
modules/        In-process module packages (15 modules, compiled into omega)
modules/http_channel/static/  Built webui SPA (tracked, embedded via go:embed)
modules/ui        webui - Nuxt 4 SPA (see modules/ui/AGENTS.md); built output embedded into the binary
deploy/           Install artifacts (omega.service systemd unit)
.agents/          Commit conventions (COMMIT.md)
build.sh          Build script (Linux/macOS): vet + test + build
build.bat         Build script (Windows): vet + test + build
docs/             User documentation (this directory)
bin/              Build output + local runtime (gitignored; skills/ templates tracked)
  omega           Built binary
  config/         Config + trust state (config.yaml, trust.yaml, *.example seeds; legacy root trust.yaml still wins)
    config.yaml   Personal configuration (gitignored)
    trust.yaml    Trust state (gitignored)
  omega.db        Session database (gitignored)
  skills/         Skill files
```

A released/installed copy is self-contained: the binary, config/,
omega.db, memory files, and skills live in one home directory - the
executable's directory for release-folder installs, or `~/.omega` when
the binary is installed into a PATH bin directory (`~/go/bin`,
`~/.local/bin`). `OMEGA_HOME` overrides both.

## Development

```bash
./build.sh        # vet + test + build to bin/ (or build.bat on Windows)
```

Each package includes test files with no external test framework -
just the Go testing package. Tests are deterministic via a fake
provider that scripts stream events.
