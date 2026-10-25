package core

import "time"

// The module-owned TUI seam (card t_88bb7dcf): the same law the webui
// option-C registration gives every module's panels, terminal-shaped.
// The webui registers by directory (modules/<name>/ui/); the TUI shell
// registers whatever modules mount into Context.TUIProviders - a plugin
// left out of buildPlugins never mounts, so its commands and screens
// are simply absent from both ends. The shell (tui) stays the
// input loop, layout and theme; it owns no feature commands.

// TUICommand is one module-owned slash command. Name carries the
// slash ("/cron"); Help is the /help one-liner; Args optionally lists
// first-argument completions (the shell's old commandOptions rows).
type TUICommand struct {
	Name string
	Help string
	Args []string
}

// TUIStyle applies one named theme style. The core seam stays free of
// styling libraries: the shell resolves the name to its render func,
// unknown kinds fall back to the identity render.
type TUIStyle func(string) string

// TUIConsole is the output surface the TUI shell hands a module
// handler. Handlers write through it and never touch the bubbletea
// model directly.
type TUIConsole interface {
	// Labeled appends one paragraph shaped like the shell's own
	// command output today: a themed "[label]" line, then the plain
	// body, then a redraw. An empty label renders the body alone.
	Labeled(label, body string)
	// Error surfaces a status-line error - the same channel the
	// shell's bad-command path uses.
	Error(msg string)
	// ClearError drops the status-line error (a screen path that
	// succeeded outlives whatever message preceded it).
	ClearError()
	// Style resolves a named theme style ("info", "status", "dim",
	// "tool", "match", "error").
	Style(kind string) TUIStyle
	// CWD returns the host working directory (the /files root).
	CWD() string
	// Ext is the mounted plugin Context (Cron, Logger, Store seams).
	Ext() *Context
}

// TUITick is a follow loop the shell drives for a module command (the
// /logs -f pattern): every Interval the shell calls Step and appends
// the returned lines; keep=false stops the loop. A keypress stops it
// too - following a log is a watch mode, and typing means the user
// wants the prompt back.
type TUITick struct {
	Interval time.Duration // zero defaults to one second
	Step     func(c TUIConsole) (lines string, keep bool)
}

// TUIScreen is a module-owned full-screen view (the /files browser
// shape): while it is open it consumes every key. Bubbletea messages
// arrive as the values they are - the seam stays UI-library-free, so
// Update takes them untyped and screens type-assert locally (only
// key and window-size messages arrive). Screens surface errors
// through the console, never a return value.
type TUIScreen interface {
	Update(msg any, c TUIConsole)
	// View renders the body between the top edge and the status line.
	View(width, height int) string
	// Closed reports the screen wants to close (its q/esc path).
	Closed() bool
}

// TUIProvider is the additive module-TUI ownership seam. A module that
// owns TUI surface implements it and appends itself to
// Context.TUIProviders during Mount - the registration point, exactly
// like ToolProviders. The shell builds its command table, autocomplete
// and dispatch from the mounted providers and nothing else.
type TUIProvider interface {
	TUICommands() []TUICommand
	// HandleTUICommand executes one command this provider owns. The
	// handler writes output through the console; the result carries
	// only the interactive leftovers (a screen to focus, a follow
	// loop to run).
	HandleTUICommand(name, args string, c TUIConsole) TUIResult
}

// TUIResult is what a module command handler asks the shell to do.
type TUIResult struct {
	Overlay TUIScreen // full-screen view to focus (nil = none)
	Tick    *TUITick  // follow loop to start (nil = none)
}
