# omega Extensions

Extensions are in-process Go packages that provide capabilities to
omega. Each extension implements the `Plugin` interface from
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

| Seam             | Type      | Description                                 |
| ---------------- | --------- | ------------------------------------------- |
| `provider`       | exclusive | LLM provider (Ollama, OpenAI, Anthropic)    |
| `compactor`      | exclusive | Context compaction                          |
| `store`          | exclusive | Session persistence                         |
| `skills`         | exclusive | Skill loading                               |
| `loop`           | exclusive | Agent loop                                  |
| `prompt_builder` | exclusive | System prompt builder                       |
| `tools`          | additive  | Tool provider (multiple plugins contribute) |

Exclusive seams conflict if two plugins provide the same one. The
`tools` seam is additive: multiple plugins contribute to
`Context.ToolProviders`.

## Available extensions

| Package      | Seam(s)        | Description                                          |
| ------------ | -------------- | ---------------------------------------------------- |
| `provider/`  | provider       | Ollama, OpenAI, Anthropic streaming                  |
| `store/`     | store, tools   | SQLite session store + sessions.search tool          |
| `skills/`    | skills, tools  | Skill loading + skills.read tool + /skills command   |
| `compactor/` | compactor      | Context compaction (keep-first/last + LLM summarize) |
| `prompt/`    | prompt_builder | System prompt builder + guidelines                   |
| `tools/`     | tools          | Shell command + file tools                           |
| `mcp/`       | tools          | MCP bridge (stdio + HTTP MCP servers)                |
| `delegate/`  | tools          | Subagent delegation (delegate.task, delegate.status) |
| `web/`       | tools          | Web search/fetch (Ollama Cloud API)                  |

## Writing an extension

1. Create a directory under `extensions/<name>/`.
2. Implement the extension logic as a struct.
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

func (p *Plugin) Name() string       { return "my-extension" }
func (p *Plugin) Provides() []string { return []string{"tools"} }
func (p *Plugin) Requires() []string { return nil }
func (p *Plugin) Mount(ctx *agent.Context) error {
    ctx.ToolProviders = append(ctx.ToolProviders, myToolProvider{})
    return nil
}

func NewPlugin() *Plugin { return &Plugin{} }
```
