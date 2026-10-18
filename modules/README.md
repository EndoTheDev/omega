# omega Modules

Modules are in-process Go packages that provide capabilities to
omega. Each module implements the `Plugin` interface from
`agent/plugin.go` and mounts into a shared `Context`.

## How it works

1. The host collects plugins from compiled-in packages.
2. `MountAll` sorts plugins by dependencies and mounts each into
   `Context`.
3. The agent loop reads from `Context` slots directly — zero IPC,
   full type safety.

## Plugin interface

```go
type Plugin interface {
    Name() string
    Provides() []string       // seam names: "provider", "store", etc.
    Requires() []string       // seam names this plugin needs first
    Mount(ctx *Context) error // populates ctx slots
}
```

## Seams

| Seam             | Type      | Description                                    |
| ---------------- | --------- | ---------------------------------------------- |
| `provider`       | exclusive | LLM provider (Ollama, OpenAI, Anthropic)       |
| `compactor`      | exclusive | Context compaction                             |
| `store`          | exclusive | Session persistence                            |
| `skills`         | exclusive | Skill loading                                  |
| `loop`           | exclusive | Agent loop                                     |
| `logging`        | exclusive | Operational logging (file logger)              |
| `memory`         | exclusive | Persistent memory (two-file store)             |
| `prompt_builder` | exclusive | System prompt builder                          |
| `frontend`       | exclusive | User-facing interface (TUI, web UI)            |
| `trust`          | exclusive | Project trust gate (AGENTS.md context loading) |
| `cron`           | exclusive | Scheduled agent turns (crons.yaml)             |
| `tools`          | additive  | Tool provider (multiple plugins contribute)    |
| `channel`        | additive  | Delivery transport (HTTP, Discord, etc.)       |
| `prompt_sections` | additive | Prompt sections (modules render their own block) |

Exclusive seams conflict if two plugins provide the same one. The
`tools` seam is additive: multiple plugins contribute to
`Context.ToolProviders`. The `prompt_sections` seam is additive:
modules that own prompt content append a `PromptSectioner` to
`Context.PromptSectioners`; the prompt builder renders their
sections between Project Context and Environment.

## Available modules

| Package         | Seam(s)        | Description                                          |
| --------------- | -------------- | ---------------------------------------------------- |
| `agent_loop/`   | loop           | Default conversation loop (LoopProvider impl)        |
| `provider/`     | provider       | Ollama, OpenAI, Anthropic streaming                  |
| `store/`        | store, tools   | SQLite session store + sessions.search tool          |
| `skills/`       | skills, tools, prompt_sections | Skill loading + skills.read tool + /skills command + Available Skills section |
| `compactor/`    | compactor      | Context compaction (keep-first/last + LLM summarize) |
| `logging/`      | logging        | Operational logging (file logger, LoggerProvider)    |
| `memory/`       | memory, tools, prompt_sections | Persistent memory (memory tool, two-file store) + snapshot block |
| `prompt/`       | prompt_builder | Generic prompt assembler (sections from owning modules) |
| `tools/`        | tools, prompt_sections | Shell command + file tools + Tools section (firstLine descriptions) |
| `mcp/`          | tools          | MCP bridge (stdio + HTTP MCP servers)                |
| `delegate/`     | tools          | Subagent delegation (delegate.task, delegate.status) |
| `web/`          | tools          | Web search/fetch (Ollama Cloud API)                  |
| `http_channel/` | channel        | HTTP/SSE delivery channel (omega serve transport)    |
| `tui/`          | frontend       | Terminal frontend (Bubble Tea TUI, themes, @file)    |
| `trust/`        | trust          | Project trust gate (trust.yaml, AGENTS.md loading)   |
| `cron/`         | cron           | Scheduled prompts (crons.yaml, cron:<name> sessions) |

## Writing an module

1. Create a directory under `modules/<name>/`.
2. Implement the module logic as a struct.
3. Create a `plugin.go` with a `NewPlugin(...)` function returning
   a `Plugin` implementation.
4. `Mount` populates `Context` slots from config.

Example:

```go
package myext

import (
    "agent"
)

type Plugin struct{}

func (p *Plugin) Name() string       { return "my-module" }
func (p *Plugin) Provides() []string { return []string{"tools"} }
func (p *Plugin) Requires() []string { return nil }
func (p *Plugin) Mount(ctx *agent.Context) error {
    ctx.ToolProviders = append(ctx.ToolProviders, myToolProvider{})
    return nil
}

func NewPlugin() *Plugin { return &Plugin{} }
```
