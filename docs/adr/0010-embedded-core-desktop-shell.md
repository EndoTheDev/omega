# ADR 0010: the desktop shell is a client of a detached agent

Status: accepted (card 1 of the 0.19.0 desktop chain; supersedes the in-process shape
this ADR first recorded). AMENDED 2026-11-25: ruling 16 ("the spawned server survives
quit") is superseded by the ownership rule - effective at the merge of the lifetime
card into dev.

## Context

`omega desktop` has to show the existing webui in a native window. The webui is not
a static document: it talks to `/chat` (SSE), `/ws` (the push socket), the module
REST routes and the session API, all served by the same HTTP channel `omega serve`
starts. The SPA is embedded in the binary and served by that channel.

Three shapes were on the table:

1. The window runs the core in its own process on a loopback port and points at it.
2. The window spawns `omega serve` as a child process it owns.
3. The agent is the background process, and the desktop is a client that guarantees
   one exists.

## Decision

Option 3. The agent stays what it already is: a long-lived `omega serve` on the home,
serving the SPA and the API on `server.port`. `omega desktop` probes `/health` on
that port. A live instance is connected to as-is; when nothing answers the shell
spawns `omega serve` DETACHED from the same binary and waits a bounded 15 s for
`/health`. The window then loads the instance's own origin.

Consequences of the shape:

- The desktop process never opens the home's stores. There is no second writer and
  no second core, so the old "desktop beside serve is two writers" limitation is
  gone by construction.
- A spawned instance dies with the desktop process. The launch keeps one Job Object
  handle open with kill-on-close, so tray Quit, a crash and a hard kill all take the
  server down; the next launch starts a fresh one. An instance the launch found
  running is never touched, and a window close kills nothing. Autostart stays
  ensure-an-instance at login.
- The window's origin is the instance's origin. No separate desktop port exists, so
  the webui's origin-keyed localStorage (theme, dock layout) follows the instance.
- A failure is diagnosed, never a spinner: a port another process holds is named in
  a dialog and the launch exits 1. A silent "not ready" loop is the ollama-class bug
  this design exists to avoid.

`application.New` owns the single-instance lock, so a second launch notifies the
first window and exits without starting anything.

## Why not a core inside the window

- Two processes on one home means two writers of the same store and the same config:
  the parent cannot see the child's failures, and quitting the window either kills
  the agent or leaves an orphan on the port.
- The agent must outlive the window: a session running a cron fire or a wake must not
  die because a UI was closed.

## Why the spawn is owned, not supervised

- Ownership is the lifetime: the launch that spawned the server keeps one Job Object
  handle open, so the server dies with the desktop process. That covers a clean quit, a
  crash and a hard kill with one mechanism, and needs no supervisor, no heartbeat and
  no kill path.
- An instance the launch found running was started by something else - a terminal, a
  service, another launch - so it is connected to and never touched.
- The spawn is console-detached, from the same executable: no PATH resolution, no
  second binary, no console window. The job membership is the only tie.

## Why the exe links GUI-subsystem

- A console-subsystem exe opened from a file manager paints a console the window makes
  pointless, and the parent-image check that used to tell a shell launch apart knew one
  image (Explorer): another file manager, a shortcut host or an installer reads as a
  terminal. The exe links `-H=windowsgui`, so no shell launch ever paints a console.
- The discriminator is the console itself. Startup calls
  `AttachConsole(ATTACH_PARENT_PROCESS)`: success means a terminal launched it, so the
  CLI loop runs on that console with the standard streams rebound to `CONIN$` /
  `CONOUT$` (a GUI-subsystem process gets no console handles of its own); failure means
  no parent console - a shell launch - and a bare `omega` opens the window.
- The check is per launch and needs no process list, so there is no file-manager list
  to keep current and no image name to miss.
- cmd.exe and PowerShell do not wait for a GUI-subsystem child, so the release tree
  carries `omega.cmd` beside the exe: two lines that run the sibling binary, which is
  what makes those shells wait for the loop. git-bash waits on its own.

## Why not the Wails asset server

- `/ws` and SSE need a real HTTP origin with the same routes and the same Host/Origin
  guard `omega serve` uses. Serving the SPA from Wails' asset handler and bridging
  the rest through Wails bindings means a second, divergent transport for every route
  the webui already calls.
- The webui must stay byte-identical to what `serve` serves: same embedded files,
  same channel. Any asset path through Wails is a second pipeline to keep in sync.
- The same origin works in a browser, so the window is a client like any other and
  the gate can exercise the core without a webview (`--headless-health`).

## Consequences

- The desktop needs the agent's port free when it must start one; a held port is a
  loud failure (named in a dialog) rather than a fallback port.
- The rest of the chain (connection switcher, tray, autostart) is built on the client
  model: a tray follows the local instance's SSE notification stream rather than an
  in-process subscription.
- The Windows surface is the Wails webview; mac/linux follow in 0.19.x. The headless
  health probe is portable, so gate smoke does not need a desktop.
