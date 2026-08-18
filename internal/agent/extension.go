package agent

import (
	"context"
	"fmt"

	"github.com/EndoTheDev/omega/internal/ai"
)

// ExtensionProtocolVersion is the JSON-RPC protocol version extensions
// negotiate during initialize. Bumping it is a breaking change.
const ExtensionProtocolVersion = "0.1"

// ExtensionManager is the seam between the agent and any extension
// transport. The agent and TUI talk to this interface; they never know
// whether extensions run over stdio, WASM, or nothing at all.
//
// The first implementation is StdioManager (external processes via
// JSON-RPC over stdin/stdout). A future WasmManager would be a drop-in
// replacement — the interface is the architecture, the transport is a
// detail.
type ExtensionManager interface {
	// Load discovers and initializes extensions from dir. A missing or
	// empty dir is not an error — it loads zero extensions. apiKey is
	// passed to extensions for authentication; the transport determines
	// how it reaches them.
	Load(dir string, apiKey string) error

	// Tools returns extension-provided tools keyed by tool name. The
	// agent merges these with built-in tools; built-ins win on name
	// conflict.
	Tools() map[string]Tool

	// Commands returns extension-provided slash commands for the TUI.
	Commands() []ExtensionCommand

	// Infos returns metadata about each loaded extension, for the
	// /extensions command.
	Infos() []ExtensionInfo

	// DispatchEvent sends an agent event to all extensions that
	// subscribed to it during initialize. Non-blocking and best-effort:
	// a stalled extension must not block the agent loop.
	DispatchEvent(event Event)

	// CallCommand invokes an extension-provided slash command.
	CallCommand(ctx context.Context, name string, args string) (string, error)

	// Close shuts down all extensions and releases resources. Safe to
	// call when no extensions were loaded.
	Close() error

	// PromptGuidelines returns extra guideline lines that extensions
	// want injected into the system prompt. Called once before the
	// agent loop starts.
	PromptGuidelines() []string

	// CustomizeCompaction lets an extension provide a custom compaction
	// summary. Returns ok=false if no extension wants to customize;
	// the agent uses the default provider-based compaction.
	CustomizeCompaction(ctx context.Context, messages []ai.Message, focus string) (string, bool)

	// CustomizeBranchSummary lets an extension provide a custom branch
	// summary. Returns ok=false if no extension wants to customize.
	CustomizeBranchSummary(ctx context.Context, messages []ai.Message) (string, bool)

	// BuildPrompt asks extensions to build the complete system prompt.
	// Returns ok=false if no extension wants to build it; the agent uses
	// the default PromptBuilder. An extension that returns ok=true
	// fully replaces the default prompt.
	BuildPrompt(ctx context.Context, opts PromptBuildOptions) (string, bool)

	// CompactMessages asks extensions to compact the message history
	// completely. Returns ok=false if no extension wants to handle it;
	// the agent uses the default Compactor. An extension that returns
	// ok=true fully replaces the default compaction.
	CompactMessages(ctx context.Context, messages []ai.Message) ([]ai.Message, bool)
}

// ExtensionCommand is a slash command registered by an extension.
type ExtensionCommand struct {
	Name        string // includes leading slash, e.g. "/mycmd"
	Description string
}

// ExtensionInfo is metadata about a loaded extension, for display.
type ExtensionInfo struct {
	Name     string
	Tools    int
	Commands int
	Status   string // "running" or "error: ..."
}

// PromptBuildOptions carries context for extension-built system prompts.
type PromptBuildOptions struct {
	CWD       string
	Messages  []ai.Message
}

// NoopManager is the default ExtensionManager when extensions are
// disabled or the directory is empty. Every method is a no-op.
type NoopManager struct{}

func (NoopManager) Load(dir string, apiKey string) error          { return nil }
func (NoopManager) Tools() map[string]Tool                        { return nil }
func (NoopManager) Commands() []ExtensionCommand                  { return nil }
func (NoopManager) Infos() []ExtensionInfo                        { return nil }
func (NoopManager) DispatchEvent(event Event)                     {}
func (NoopManager) CallCommand(ctx context.Context, name, args string) (string, error) {
	return "", fmt.Errorf("no extensions loaded")
}
func (NoopManager) Close() error { return nil }

func (NoopManager) PromptGuidelines() []string                                        { return nil }
func (NoopManager) CustomizeCompaction(ctx context.Context, messages []ai.Message, focus string) (string, bool) {
	return "", false
}
func (NoopManager) CustomizeBranchSummary(ctx context.Context, messages []ai.Message) (string, bool) {
	return "", false
}
func (NoopManager) BuildPrompt(ctx context.Context, opts PromptBuildOptions) (string, bool) {
	return "", false
}
func (NoopManager) CompactMessages(ctx context.Context, messages []ai.Message) ([]ai.Message, bool) {
	return nil, false
}
