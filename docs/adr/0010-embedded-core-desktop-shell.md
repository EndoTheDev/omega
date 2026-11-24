# ADR 0010: the desktop shell runs the core in-process on a loopback port

Status: accepted (card 1 of the 0.19.0 desktop chain)

## Context

`omega desktop` has to show the existing webui in a native window. The webui is
not a static document: it talks to `/chat` (SSE), `/ws` (the push socket), the
module REST routes and the session API, all served by the same HTTP channel
`omega serve` starts. The SPA is embedded in the binary and served by that
channel.

Three ways to put a window over that surface were on the table:

1. A child process: the window spawns `omega serve` and points at it.
2. Wails' own asset server: the window serves the SPA from Wails and the app
   reaches the core through Wails bindings instead of HTTP.
3. The shell runs the core in-process, on a loopback port, and the window is a
   plain client of that origin.

## Decision

Option 3. `omega desktop` is one process. It mounts the same plugin set
`omega serve` mounts (`startRuntime`, shared with `cmdServe`), binds
`127.0.0.1:<desktop.port>`, waits for `/health`, and opens one Wails v3 window
at that URL.

`desktop.port` is fixed (default 8097) and is never replaced by a free
neighbour: the webui keeps theme and dock layout in origin-keyed localStorage,
so a moving port would quietly reset the UI on every launch. A held port is a
dialog naming the port and exit 1.

`application.New` owns the single-instance lock, so it runs before the core
boots. A second launch notifies the first, which restores and focuses its
window, and exits - it never binds a second core.

## Why not a child process

- Two processes on one home means two writers of the same store and the same
  config: the parent cannot see the child's failures, the child outlives the
  window on a crash, and shutdown becomes a signal dance with no proof.
- The desktop must own the lifecycle: the window closing should tear the core
  down, not leave an orphan serving a port.
- One process keeps `omega setup`, the updater and the Run-key autostart on one
  executable path - the AGENTS.md subcommand law (one entry surface).

## Why not the Wails asset server

- `/ws` and SSE need a real HTTP origin with the same routes and the same
  Host/Origin guard `omega serve` uses. Re-serving the SPA from Wails' asset
  handler and bridging the rest through Wails bindings means a second,
  divergent transport for every route the webui already calls.
- The webui must stay byte-identical to what `serve` serves: same embedded
  files, same channel. Any asset path through Wails is a second asset pipeline
  to keep in sync.
- The window as a plain client also means the same URL works in a browser for
  debugging, and the release gate can exercise the core without a webview
  (`omega desktop --headless-health`).

## Consequences

- The desktop reuses the home; a simultaneous `omega serve` on the same home is
  not prevented (two writers). Out of scope for 0.19.0.
- The Windows surface is the Wails webview; mac/linux follow in 0.19.x. The
  headless health probe is portable, so gate smoke does not need a desktop.
- A desktop-only knob (`desktop.port`) now lives in config.yaml beside
  `server.port`; it is not a settings-pane knob.
