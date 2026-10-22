# TUI Commands

The shell owns slash commands for the input loop, layout and theme;
feature slash commands are owned by their module (card t_88bb7dcf).
Each module registers its commands during `Mount()` through the
`agent.TUIProvider` seam (`agent/tui.go`) - the same law the webui
enforces through directory registration (`modules/<name>/ui/`):

- remove a module's `buildPlugins` registration (or build-tag a
  plugin out) and its TUI commands are **absent from the command
  table and the dispatch**, exactly like its webui surface;
- mounted module commands sort alphabetically after the shell
  commands in autocomplete; `/help` lists module rows between the
  shell rows and the extension command rows.

`/help` prints exactly this list plus any extension-registered
commands (`ctx.Commands`). Loaded skills are invocable too, both
standalone (`/skill-name`) and inline as `/name` tokens inside a
normal message.

| Command                               | Owner    | Description                                                                                            |
| ------------------------------------- | -------- | ------------------------------------------------------------------------------------------------------ |
| `/exit`                               | shell    | Quit                                                                                                    |
| `/new [--ephemeral]`                  | store    | Start a new conversation (`--ephemeral`: nothing persisted)                                             |
| `/sessions`                           | store    | List saved sessions as a table                                                                          |
| `/sessions delete <# \| id \| label>` | store    | Delete a session                                                                                        |
| `/resume <# \| id \| label>`          | store    | Resume a session by line number, ID, or label                                                           |
| `/branch [id]`                        | store    | Branch a new session from the current (or given) one                                                    |
| `/retry`                              | shell    | Re-run the last user turn as a new response variant                                                     |
| `/variant [<n> \| prev \| next]`      | shell    | Navigate response variants (no arg lists branch points)                                                 |
| `/fork [<path #>]`                    | shell    | List the active path; `<path #>` pins where the next message continues                                  |
| `/label [text]`                       | store    | Set a label on the current session (no text clears it)                                                  |
| `/tree`                               | store    | Show the session tree                                                                                   |
| `/model <# \| name>`                  | provider | Switch the model (line # from /models, or name)                                                         |
| `/models`                             | provider | List available models from the current provider                                                         |
| `/provider`                           | provider | Show current provider type                                                                              |
| `/compact [focus]`                    | compactor| Manually compact conversation history                                                                   |
| `/copy`                               | shell    | Copy the last message to clipboard                                                                      |
| `/export [path]`                      | store    | Export session messages to JSONL (default: `<session_id>.jsonl`)                                        |
| `/insights [days]`                    | store    | Show cross-session usage analytics (default: 30 days)                                                   |
| `/search <query>`                     | store    | Search session messages (full-text; FTS5 phrase + prefix — `"fib"` finds `"fibonacci"`)                 |
| `/thinking [level]`                   | provider | Set thinking level (none, off, on, minimal, low, medium, high, extra high, max, ultra; no arg cycles)   |
| `/tools [on \| off \| auto \| list]`  | tools    | Tool results: expanded / collapsed / auto, or list all tools                                            |
| `/extensions`                         | shell    | List loaded extensions (the plugin system has no owning module)                                         |
| `/skills`                             | skills   | List loaded skills                                                                                      |
| `/theme [name]`                       | shell    | Switch theme (dark, light, auto; no arg lists all)                                                      |
| `/files [path]`                       | tools    | Browse the working directory (enter opens, esc/q closes)                                                |
| `/cron`                               | cron     | List cron jobs (read-only)                                                                              |
| `/verdict [<N> [<num|y|n|skip|text>]]` | verdict | List pending verdict cards; `<N>` picks one, then an answer: option number / y/n / skip / free text. While a run blocks on the card, typed input + Enter answers it directly (card t_2ecf9c51) |
| `/logs [-f]`                          | logging  | Tail omega.log (`-f`: keep following)                                                                   |
| `/help`                               | shell    | Show help                                                                                               |

## Module registration recipe (TUI side)

A module that owns TUI surface implements `agent.TUIProvider`
(`TUICommands()` + `HandleTUICommand(name, args, console)`) in
`<module>/tui.go` and appends itself to `ctx.TUIProviders` during
`Mount()`. The handler writes through the `agent.TUIConsole` the shell
hands it - never the bubbletea model - and optionally returns one of:

- `Result{Overlay: TUIScreen}`: a full-screen view the shell routes
  keys to and renders above the status line (the `/files` shape);
- `Result{Tick: TUITick}`: a follow loop the shell drives on an
  interval, stopped by any keypress (the `/logs -f` shape).

The registration is the plugin: `TestModuleOmissionDropsTUICommands`
(`cmd/omega/plugins_test.go`) gates the law.

## Module removal recipe (both ends)

Removing a module removes its TUI and webui surfaces. Worked example:
the cron module (`modules/cron/`).

1. Take the plugin out of `buildPlugins()` in `cmd/omega/main.go`
   (or gate the registration with a build tag).
2. TUI end: the cron plugin never mounts, so no `TUIProvider`
   registers `/cron` - the command is absent from `knownCommands`
   successors, `/help`, and dispatch. Gate:
   `go test ./cmd/omega/ -run TestModuleOmissionDropsTUICommands`.
3. webui end: the serve host mounts no `CronProvider`, so `GET
   /cron/jobs` reads an empty listing and `modules/cron/`'s
   `ui/CronJobs.vue` panel has no rows to render. A full removal
   (deleting `modules/cron/`) additionally drops the panel component
   from the nuxt build entirely - directory registration, no manifest.
4. Rebuild: `scripts/build.sh` (embed the lane bundle only when the
   webui changed; `scripts/embed-lane.sh` owns `static/` commits).

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