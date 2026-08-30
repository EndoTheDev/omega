# Architecture

```txt
HTTP channel -> agent (loop + tools) -> ai (provider streaming)
```

| Layer      | Package       | Responsibility                                                                                                                                          |
| ---------- | ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Agent      | `agent`       | Multi-turn loop, parallel tool execution, compaction, capability seams, Plugin system (Context, Plugin, MountAll)                                       |
| Provider   | `ai`          | Provider interface, Ollama + OpenAI + Anthropic, stream events, message types, retry                                                                    |
| CLI        | `cmd/omega`   | Entry point, TUI, project context, trust gate, config loading (YAML + env + defaults), hot-reload                                                       |
| Extensions | `extensions/` | 15 in-process Go packages: agent_loop, provider, store, skills, compactor, logging, memory, prompt, tools, mcp, delegate, web, http_channel, tui, trust |

No layer skips another. Events are typed structs, dispatched via type
switch. The provider layer emits events on a channel. The agent layer
consumes them and runs the tool loop. The HTTP channel exposes
everything over HTTP.

## Project Structure

```txt
cmd/omega/        Single binary entry point (serve, run, health, chat)
ai/               Provider abstraction, stream events, message types, retry
agent/            Multi-turn loop, tool execution, compaction, seams, Plugin system
extensions/        In-process extension packages (15 extensions, compiled into omega)
.agents/          Commit conventions (COMMIT.md)
bin/skills/       Skill templates (tracked)
build.sh          Build script (Linux/macOS): vet + test + build
build.bat         Build script (Windows): vet + test + build
docs/             User documentation (this directory)
bin/              Runtime directory (gitignored except templates)
  omega.exe       Built binary
  config.yaml     Personal configuration (gitignored)
  config.yaml.example  Configuration template (tracked)
  omega.db        Session database (gitignored)
  trust.yaml      Trust state (gitignored)
  skills/         Skill files
```

## Development

```bash
./build.sh        # vet + test + build to bin/ (or build.bat on Windows)
```

Each package includes test files with no external test framework -
just the Go testing package. Tests are deterministic via a fake
provider that scripts stream events.
