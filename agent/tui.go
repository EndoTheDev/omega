package agent

import "github.com/EndoTheDev/omega/core"

// Re-export shims (arc 1b, t_7bb4790d): the module-owned TUI seam moved
// to core - TUIConsole.Ext() returns *Context by type, and Context
// carries TUIProviders, so the family rides with the seam contracts.
// The aliases disappear when modules rewire in phase 2.
//
// DO NOT add new TUI seam types here - they belong in core.

type (
	// TUICommand is one module-owned slash command.
	TUICommand = core.TUICommand
	// TUIStyle applies one named theme style.
	TUIStyle = core.TUIStyle
	// TUIConsole is the output surface the TUI shell hands a module handler.
	TUIConsole = core.TUIConsole
	// TUITick is a follow loop the shell drives for a module command.
	TUITick = core.TUITick
	// TUIScreen is a module-owned full-screen view.
	TUIScreen = core.TUIScreen
	// TUIProvider is the additive module-TUI ownership seam.
	TUIProvider = core.TUIProvider
	// TUIResult is what a module command handler asks the shell to do.
	TUIResult = core.TUIResult
)