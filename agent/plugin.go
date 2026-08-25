package agent

import (
	"context"
	"fmt"

	"github.com/EndoTheDev/omega/ai"
)

// Context is the shared service container that extensions mount into.
// Each slot corresponds to a capability seam. The host creates a Context,
// runs MountAll to populate it, then passes it to the agent loop.
//
// Seam slots (Provider, Compactor, Store, Skills, Loop, PromptBuilder)
// are exclusive: one plugin per slot. ToolProviders is additive: multiple
// plugins contribute tools.
type Context struct {
	// Seam slots (exclusive — one plugin per slot).
	Provider      ai.Provider
	Compactor     CompactionProvider
	Store         StoreProvider
	Skills        SkillsProvider
	Loop          LoopProvider
	PromptBuilder PromptBuilder

	// Tool providers (additive — multiple plugins contribute).
	ToolProviders []ToolProvider

	// Cross-cutting (set by specific plugins).
	Commands          []ExtensionCommand
	CommandHandler    func(ctx context.Context, name, args string) (string, error)
	InjectedMessages  <-chan InjectedMessage
	PendingDelegations func() int

	// Shared state.
	CWD    string
	Config any // host passes gateway.Config, plugins type-assert

	// Metadata (built during MountAll).
	Infos []ExtensionInfo
}

// Plugin is the contract every extension implements. The host collects
// plugins, sorts them by dependencies, and mounts each into a Context.
type Plugin interface {
	// Name is shown in the /extensions listing.
	Name() string
	// Provides lists seam names this plugin mounts (e.g. "provider",
	// "store", "tools"). Exclusive seams conflict if two plugins
	// provide the same one. The "tools" seam is additive.
	Provides() []string
	// Requires lists seam names that must be mounted before this
	// plugin. MountAll uses these for topological ordering.
	Requires() []string
	// Mount populates Context slots. Called once, in dependency order.
	Mount(ctx *Context) error
}

// Exclusive seams: two plugins providing the same one is a conflict.
// "tools" is additive: multiple plugins contribute to ToolProviders.
var exclusiveSeams = map[string]bool{
	"provider":       true,
	"compactor":      true,
	"store":          true,
	"skills":         true,
	"loop":           true,
	"prompt_builder": true,
}

// MountAll sorts plugins by dependencies and mounts each into ctx.
// Returns an error if:
//   - two plugins provide the same exclusive seam (conflict)
//   - a required seam is not provided by any plugin (missing dep)
//   - a plugin's Mount returns an error
func MountAll(plugins []Plugin, ctx *Context) error {
	// Build provides map: seam -> plugin name.
	provides := make(map[string]string)
	for _, p := range plugins {
		for _, seam := range p.Provides() {
			if exclusiveSeams[seam] {
				if existing, ok := provides[seam]; ok {
					return fmt.Errorf("seam %q provided by both %q and %q", seam, existing, p.Name())
				}
			}
			provides[seam] = p.Name()
		}
	}

	// Check for missing deps.
	for _, p := range plugins {
		for _, req := range p.Requires() {
			if _, ok := provides[req]; !ok {
				return fmt.Errorf("plugin %q requires seam %q but no plugin provides it", p.Name(), req)
			}
		}
	}

	// Topological sort: mount plugins whose deps are satisfied first.
	// ponytail: O(n^2) over a small list (10 plugins). Upgrade path:
	// Kahn's algorithm if the list grows past ~50.
	mounted := make(map[string]bool)
	var ordered []Plugin
	for len(ordered) < len(plugins) {
		progress := false
		for _, p := range plugins {
			if mounted[p.Name()] {
				continue
			}
			depsOK := true
			for _, req := range p.Requires() {
				provider := provides[req]
				if !mounted[provider] {
					depsOK = false
					break
				}
			}
			if depsOK {
				ordered = append(ordered, p)
				mounted[p.Name()] = true
				progress = true
			}
		}
		if !progress {
			// Cycle: remaining plugins have unsatisfiable deps.
			var remaining []string
			for _, p := range plugins {
				if !mounted[p.Name()] {
					remaining = append(remaining, p.Name())
				}
			}
			return fmt.Errorf("circular dependency among plugins: %v", remaining)
		}
	}

	// Mount in order.
	for _, p := range ordered {
		if err := p.Mount(ctx); err != nil {
			return fmt.Errorf("mount %q: %w", p.Name(), err)
		}
		ctx.Infos = append(ctx.Infos, ExtensionInfo{
			Name:   p.Name(),
			Seams:  p.Provides(),
		})
	}

	return nil
}