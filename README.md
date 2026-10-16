# Ω omega

![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)
![License](https://img.shields.io/badge/license-MIT-blue)
![Status](https://img.shields.io/badge/status-v0.6.0-blue)

omega is a terminal-based AI assistant that reads files, runs
commands, and edits code. It talks to LLM providers (Ollama, OpenAI,
Anthropic) and streams responses in real time. Single binary, full-
screen TUI, persistent session tree, context compaction, and a
pluggable extension system.

> **Warning:** The shell tool executes commands the LLM generates with
> no sandboxing. Use Ollama for sensitive work — cloud providers
> receive file contents and command output.

## Community

Join the [Discord](https://discord.gg/vQvHrHWkbx) for release announcements and discussion.

## Quick Start

```bash
git clone https://github.com/EndoTheDev/omega.git
cd omega
./build.sh        # or build.bat on Windows
bin/omega         # config.yaml.example lands in bin/ on first run;
                  # create bin/config.yaml for non-default settings
```

Or install via `go install`:

```bash
go install github.com/EndoTheDev/omega/cmd/omega@latest
```

`go install` puts the binary in `~/go/bin` (or `$GOBIN`) — add it to PATH
if it isn't already:

```bash
export PATH="$HOME/go/bin:$PATH"   # add to ~/.bashrc or ~/.zshrc
```

Releases also ship prebuilt binaries for all platforms (with the webui
embedded) at [github.com/EndoTheDev/omega/releases](https://github.com/EndoTheDev/omega/releases).

### Runtime layout & update

omega's home is the executable's directory (override with `OMEGA_HOME`).
Installed into a `bin` directory (`go install`, `~/.local/bin`), the home
moves to `~/.omega` — config, example configs, the session store and logs
live there. Update an installed copy with:

```bash
omega update
```

## Architecture

```txt
HTTP channel -> agent (loop + tools) -> ai (provider streaming)
```

| Layer    | Responsibility                                             |
| -------- | ---------------------------------------------------------- |
| Agent    | Multi-turn loop, tools, compaction, extensions             |
| Provider | Ollama + OpenAI + Anthropic streaming                      |
| CLI      | Entry point, config loading, trust gate, Frontend dispatch |

See [docs/architecture.md](docs/architecture.md) for details.

## Extensions

All extensions are in-process Go packages under `extensions/`. See
[extensions/README.md](extensions/README.md) for the full list.

## Documentation

| Topic           | File                                               |
| --------------- | -------------------------------------------------- |
| Configuration   | [docs/configuration.md](docs/configuration.md)     |
| Extensions      | [docs/extensions.md](docs/extensions.md)           |
| TUI Commands    | [docs/tui.md](docs/tui.md)                         |
| Project Trust   | [docs/project-trust.md](docs/project-trust.md)     |
| Architecture    | [docs/architecture.md](docs/architecture.md)       |
| Roadmap         | [docs/roadmap.md](docs/roadmap.md)                 |
| Troubleshooting | [docs/troubleshooting.md](docs/troubleshooting.md) |

## License

[MIT](LICENSE)
