# TUI Commands

This is the registry from `cmd/tui` (`renderHelp` in
`modules/tui/tui.go`) — `/help` prints exactly this list plus any
extension-registered commands. Loaded skills are invocable too, both
standalone (`/skill-name`) and inline as `/name` tokens inside a
normal message.

| Command                               | Description                                                                                            |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| `/exit`                               | Quit                                                                                                    |
| `/new [--ephemeral]`                  | Start a new conversation (`--ephemeral`: nothing persisted)                                             |
| `/sessions`                           | List saved sessions as a table                                                                          |
| `/sessions delete <# \| id \| label>` | Delete a session                                                                                        |
| `/resume <# \| id \| label>`          | Resume a session by line number, ID, or label                                                           |
| `/branch [id]`                        | Branch a new session from the current (or given) one                                                    |
| `/retry`                              | Re-run the last user turn as a new response variant                                                     |
| `/variant [<n> \| prev \| next]`      | Navigate response variants (no arg lists branch points)                                                 |
| `/fork [<path #>]`                    | List the active path; `<path #>` pins where the next message continues                                  |
| `/label [text]`                       | Set a label on the current session (no text clears it)                                                  |
| `/tree`                               | Show the session tree                                                                                   |
| `/model <# \| name>`                  | Switch the model (line # from /models, or name)                                                         |
| `/models`                             | List available models from the current provider                                                         |
| `/provider`                           | Show current provider type                                                                              |
| `/compact [focus]`                    | Manually compact conversation history                                                                   |
| `/copy`                               | Copy the last message to clipboard                                                                      |
| `/export [path]`                      | Export session messages to JSONL (default: `<session_id>.jsonl`)                                        |
| `/insights [days]`                    | Show cross-session usage analytics (default: 30 days)                                                   |
| `/search <query>`                     | Search session messages (full-text; FTS5 phrase + prefix — `"fib"` finds `"fibonacci"`)                 |
| `/thinking [level]`                   | Set thinking level (none, off, on, minimal, low, medium, high, extra high, max, ultra; no arg cycles)   |
| `/tools [on \| off \| auto \| list]`  | Tool results: expanded / collapsed / auto, or list all tools                                            |
| `/extensions`                         | List loaded extensions                                                                                  |
| `/skills`                             | List loaded skills                                                                                      |
| `/theme [name]`                       | Switch theme (dark, light, auto; no arg lists all)                                                      |
| `/files [path]`                       | Browse the working directory (enter opens, esc/q closes)                                                |
| `/cron`                               | List cron jobs (read-only)                                                                              |
| `/logs [-f]`                          | Tail omega.log (`-f`: keep following)                                                                   |
| `/help`                               | Show help                                                                                               |

## Keybindings

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
prompt as text (bracketed paste support). See [cli.md](cli.md) for the
binary's subcommands and [configuration.md](configuration.md) for the
`theme` knob and the `themes/` folder (webui themes share the same
token family).