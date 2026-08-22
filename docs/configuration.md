# Configuration

All values can be set in `config.yaml` or overridden by environment
variables. When omega is installed globally (in PATH), it looks for
`config.yaml`, `omega.db`, `skills/`, and `extensions/` in the binary's
directory (or `OMEGA_HOME`). The working directory is used only for
AGENTS.md project context and tool file operations.

## config.yaml

```yaml
provider:
  type: ollama # ollama, openai, or anthropic
  model_name: # required - e.g. llama3, gpt-4o, claude-sonnet-4-20250514
  host: http://localhost:11434 # use https://ollama.com for Ollama Cloud
  api_key: # required for Ollama Cloud, OpenAI, and Anthropic
```

> **YAML indentation matters.** The keys under `provider:` must be
> indented 2 spaces. If they are at the root level, omega will not
> find them and report "provider.model_name is required".

## Reference

| Key                          | Env var                            | Default                  | Description                                                                                                                 |
| ---------------------------- | ---------------------------------- | ------------------------ | --------------------------------------------------------------------------------------------------------------------------- |
| -                            | `OMEGA_HOME`                       | Binary directory         | Omega home: config, db, skills, extensions live here                                                                        |
| `provider.type`              | `OMEGA_PROVIDER`                   | `ollama`                 | Provider: `ollama`, `openai`, `anthropic`                                                                                   |
| `provider.model_name`        | `OMEGA_MODEL`                      | (required)               | Model name                                                                                                                  |
| `provider.host`              | `OMEGA_HOST`                       | `http://localhost:11434` | Provider base URL                                                                                                           |
| `provider.api_key`           | `OMEGA_API_KEY`                    |                          | API key (Ollama Cloud, OpenAI, Anthropic). Falls back to `OPENAI_API_KEY` / `ANTHROPIC_API_KEY`                             |
| `server.port`                | `OMEGA_PORT`                       | `8099`                   | HTTP listen port                                                                                                            |
| `store.db_path`              | `OMEGA_DB_PATH`                    | `<home>/omega.db`        | SQLite database path                                                                                                        |
| `compaction.enabled`         | `OMEGA_COMPACTION_ENABLED`         | `true`                   | Enable context compaction                                                                                                   |
| `compaction.threshold`       | `OMEGA_COMPACTION_THRESHOLD`       | `0.6`                    | Fraction of context window that triggers compaction                                                                         |
| `compaction.context_window`  | `OMEGA_COMPACTION_CONTEXT_WINDOW`  | `32768`                  | Model context window in tokens. Fallback when the provider doesn't auto-discover it (Ollama auto-discovers via `/api/show`) |
| `compaction.keep_first`      | `OMEGA_COMPACTION_KEEP_FIRST`      | `2`                      | Messages preserved verbatim at start                                                                                        |
| `compaction.keep_last`       | `OMEGA_COMPACTION_KEEP_LAST`       | `10`                     | Messages preserved verbatim at end                                                                                          |
| `compaction.reserve_tokens`  | `OMEGA_COMPACTION_RESERVE_TOKENS`  | `16384`                  | Tokens reserved for the model response                                                                                      |
| `compaction.max_tool_output` | `OMEGA_COMPACTION_MAX_TOOL_OUTPUT` | `32768`                  | Maximum bytes of tool output before truncation                                                                              |
| `extensions.enabled`         | `OMEGA_EXTENSIONS_ENABLED`         | `false`                  | Enable extension loading                                                                                                    |
| `extensions.dir`             | `OMEGA_EXTENSIONS_DIR`             | `<home>/extensions`      | Directory to scan for extension executables                                                                                 |
| `skills.dir`                 | `OMEGA_SKILLS_DIR`                 | `<home>/skills`          | Skills directory                                                                                                            |
| `http_timeout`               | `OMEGA_HTTP_TIMEOUT`               | `300`                    | HTTP timeout for provider requests (seconds)                                                                                |
| `max_turns`                  | `OMEGA_MAX_TURNS`                  | `100`                    | Maximum tool-call turns per run                                                                                             |
| `theme`                      | `OMEGA_THEME`                      | `dark`                   | TUI color theme (dark, light, auto)                                                                                         |
| `notifications`              | `OMEGA_NOTIFICATIONS`              | `bell`                   | Turn-complete notification (bell, desktop, off)                                                                             |

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
