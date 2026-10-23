package core

import (
	"context"
)

// Context is the shared service container that extensions mount into.
// Each slot corresponds to a capability seam. The host creates a Context,
// runs MountAll to populate it, then passes it to the agent loop.
//
// Seam slots (Provider, Compactor, Store, Skills, Loop, PromptBuilder)
// are exclusive: one plugin per slot. ToolProviders and Channels are
// additive: multiple plugins contribute.
type Context struct {
	// Seam slots (exclusive — one plugin per slot).
	Provider      Provider
	Compactor     CompactionProvider
	Store         StoreProvider
	Skills        SkillsProvider
	Loop          LoopProvider
	Logger        LoggerProvider
	Memory        MemoryProvider
	PromptBuilder PromptBuilder

	// Captioner is optional (vision_model knob): the host builds it
	// once config and store are resolved, before NewFromContext, and
	// every agent from this Context captions images through it.
	Captioner Captioner

	// Tool providers (additive — multiple plugins contribute).
	ToolProviders []ToolProvider

	// Prompt section contributors (additive — multiple plugins
	// contribute). Modules that own prompt content implement
	// PromptSectioner; the prompt builder renders their sections.
	PromptSectioners []PromptSectioner

	// Frontend (exclusive — one frontend at a time). The host calls
	// Run after mounting all extensions.
	Frontend Frontend

	// TUI providers (additive — module-owned TUI commands and
	// screens, card t_88bb7dcf). Each module appends its TUIProvider
	// during Mount - the registration point; a plugin left out of
	// buildPlugins never mounts and its TUI surface is simply absent,
	// the same law the webui enforces through directory registration.
	TUIProviders []TUIProvider

	// Panel providers (additive - canvas contract, arc 3a). Modules
	// that own view-model surface append their PanelProvider during
	// Mount - one seam declares both the panels and their action
	// handlers. The canvas runtime seeds the snapshot from these.
	PanelProviders []PanelProvider
	Trust          TrustProvider
	Cron         CronProvider
	Kanban       KanbanProvider
	// Notifications is the finished-things store seam (card t_5d9032d3):
	// kanban completions, cron fires and dispatched chat runs each emit
	// one record the webui bell serves. Nil when the notifications
	// plugin is absent - every emitter is nil-safe, the HTTP endpoints
	// answer 501.
	Notifications NotificationsProvider

	// Verdicts is the interactive-question seam (verdict cards v1):
	// the verdict tool blocks on Ask, the HTTP channel and the
	// TUI answer through it. Nil when the verdict module is absent -
	// the tool refuses to block without the seam, the HTTP endpoints
	// answer 501.
	Verdicts VerdictsProvider

	// Channels (additive — multiple plugins contribute delivery
	// transports). The host starts all mounted channels after MountAll.
	Channels []Channel

	// Cross-cutting (set by specific plugins).
	Commands           []ExtensionCommand
	CommandHandler     func(ctx context.Context, name, args string) (CommandResult, error)
	InjectedMessages   <-chan InjectedMessage
	PendingDelegations func() int

	// Shared state.
	CWD     string
	Configs map[string]any // per-plugin typed config, keyed by plugin Name()

	// Metadata (built during MountAll).
	Infos []ExtensionInfo
}
