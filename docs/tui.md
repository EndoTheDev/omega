# TUI Commands

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
| `/provider`                           | Show current provider type                                                                            |
| `/compact [focus]`                    | Manually compact conversation history                                                                 |
| `/copy`                               | Copy last message to clipboard                                                                        |
| `/export [path]`                      | Export session messages to JSONL                                                                      |
| `/insights [days]`                    | Show cross-session usage analytics (default: 30 days)                                                 |
| `/search <query>`                     | Full-text search across all session messages                                                          |
| `/thinking [level]`                   | Set thinking level (none, off, on, minimal, low, medium, high, extra high, max, ultra; no arg cycles) |
| `/tools [on \| off \| auto]`          | Toggle tool result display mode                                                                       |
| `/extensions`                         | List loaded extensions                                                                                |
| `/skills`                             | List loaded skills                                                                                    |
| `/theme [name]`                       | Switch theme (dark, light, auto; no arg lists all)                                                    |
| `/help`                               | Show help                                                                                             |
| `/exit`                               | Quit                                                                                                  |

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
prompt as text (bracketed paste support).
