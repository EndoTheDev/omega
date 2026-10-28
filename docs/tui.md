# TUI Commands

The shell owns slash commands for the input loop, layout and theme;
feature slash commands are owned by their module (card t_88bb7dcf).
Each module registers its commands during `Mount()` through the
`agent.TUIProvider` seam (`engine/agent/tui.go`) - the same law the webui
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
| `/modules` /extensions                | shell    | List loaded modules; /extensions is the alias (the plugin system has no owning module)                  |
| `/skills`                             | skills   | List loaded skills                                                                                      |
| `/theme [name]`                       | shell    | Switch theme (dark, light, auto; no arg lists all)                                                      |
| `/files [path]`                       | tools    | Browse the working directory (enter opens, esc/q closes)                                                |
| `/cron`                               | cron     | List cron jobs (read-only)                                                                              |
| `/verdict [<N> [<num|1,3|y|n|skip|text>]]` | verdict | List pending verdict cards; `<N>` picks one, then an answer: option number / comma list (multi cards) / y/n / skip / typed text. While a run blocks on the card, typed input + Enter answers it directly (card t_2ecf9c51, v1.1 t_42d4dc09) |
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
   The canvas carries no cron panel either - the jobs panel rides the
   same `Mount()` as the command (arc 6 capsule); the canvas host tab
   simply renders the other modules. Removal proof:
   `go test ./cmd/omega/ -run TestCronRemovalProof`.
3. webui end: the serve host mounts no `CronProvider`, so `GET
   /cron/jobs` reads an empty listing and `modules/cron/`'s
   `ui/CronJobs.vue` panel has no rows to render. A full removal
   (deleting `modules/cron/`) additionally drops the panel component
   from the nuxt build entirely - directory registration, no manifest.
4. Rebuild: `scripts/build.sh` (embed the lane bundle only when the
   webui changed; `scripts/embed-lane.sh` owns `static/` commits).

## @ mentions

`@<path>` tokens in a message attach files to the send: an image file
goes to the provider as an image, a text file is inlined, a glob
(`@*.go`) expands to the matching files. `@session:<id>` injects the
session's message history, `@skill:<name>` injects the skill content.
Paths resolve against the TUI process cwd; a token that does not
resolve is sent literally - the send is never blocked, and a
`[@foo.txt not resolved - sent literally]` notice tells you when.

Typing `@` (at input start or after a space) opens the same
autocomplete panel as `/`: candidates are cwd files, `session:` refs
(current session first, then saved session ids) and `skill:` names,
filtered as you type, accepted with Tab/Enter. The panel and the
notice are affordances only - resolution never rewrites the send
semantics; see `engine/tui/image.go` (`extractImages`). A mid-word `@`
(an email address, a handle) is never treated as a mention.

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

Pane shell keys (active above the minimum size; 100x22 and larger):

| Key       | Action                                            |
| --------- | --------------------------------------------------|
| Tab       | Cycle pane focus (left -> chat -> panels -> run) with a sidebar or the run band focused |
| Shift+Tab | Cycle pane focus backward                          |
| 1-4       | Jump pane focus (1 sessions, 2 chat, 3 panels, 4 run) |
| d         | /panels only: toggle the raw payload JSON view     |

File paths pasted or dragged into the terminal are inserted into the
prompt as text (bracketed paste support). See [cli.md](cli.md) for the
binary's subcommands and [configuration.md](configuration.md) for the
`theme` knob and the `themes/` folder (webui themes share the same
token family).