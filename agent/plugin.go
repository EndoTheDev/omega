package agent

import (
	"context"
	"fmt"
	"sort"

	"github.com/EndoTheDev/omega/ai"
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
	Provider      ai.Provider
	Compactor     CompactionProvider
	Store         StoreProvider
	Skills        SkillsProvider
	Loop          LoopProvider
	Logger        LoggerProvider
	Memory        MemoryProvider
	PromptBuilder PromptBuilder

	// Tool providers (additive — multiple plugins contribute).
	ToolProviders []ToolProvider

	// Frontend (exclusive — one frontend at a time). The host calls
	// Run after mounting all extensions.
	Frontend Frontend

	// Channels (additive — multiple plugins contribute delivery
	// transports). The host starts all mounted channels after MountAll.
	Channels []Channel

	// Cross-cutting (set by specific plugins).
	Commands          []ExtensionCommand
	CommandHandler    func(ctx context.Context, name, args string) (CommandResult, error)
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
	"logging":        true,
	"memory":         true,
	"prompt_builder": true,
	"frontend":       true,
}

// MountAll sorts plugins by dependencies and mounts each into ctx.
// Returns an error if:
//   - two plugins provide the same exclusive seam (conflict)
//   - a required seam is not provided by any plugin (missing dep)
//   - a plugin's Mount returns an error
func MountAll(plugins []Plugin, ctx *Context) error {
	provides, err := buildProvidesMap(plugins)
	if err != nil {
		return err
	}

	ordered, err := topoSort(plugins, provides)
	if err != nil {
		return err
	}

	return mountPlugins(ordered, ctx)
}

// buildProvidesMap builds the seam -> plugin name map and detects conflicts
// where two plugins provide the same exclusive seam.
func buildProvidesMap(plugins []Plugin) (map[string]string, error) {
	provides := make(map[string]string)
	for _, p := range plugins {
		for _, seam := range p.Provides() {
			if exclusiveSeams[seam] {
				if existing, ok := provides[seam]; ok {
					return nil, fmt.Errorf("seam %q provided by both %q and %q", seam, existing, p.Name())
				}
			}
			provides[seam] = p.Name()
		}
	}

	// Check for missing deps.
	for _, p := range plugins {
		for _, req := range p.Requires() {
			if _, ok := provides[req]; !ok {
				return nil, fmt.Errorf("plugin %q requires seam %q but no plugin provides it", p.Name(), req)
			}
		}
	}

	return provides, nil
}

// topoSort returns plugins in dependency order so that a plugin's required
// seams are always provided by a plugin earlier in the list.
//
// ponytail: O(n^2) over a small list (10 plugins). Upgrade path:
// Kahn's algorithm if the list grows past ~50.
func topoSort(plugins []Plugin, provides map[string]string) ([]Plugin, error) {
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
			return nil, fmt.Errorf("circular dependency among plugins: %v", remaining)
		}
	}

	return ordered, nil
}

// mountPlugins mounts each plugin in order and records what it contributed
// so /extensions can display accurate counts.
func mountPlugins(ordered []Plugin, ctx *Context) error {
	for _, p := range ordered {
		beforeTools := len(ctx.ToolProviders)
		beforeCmds := len(ctx.Commands)
		if err := p.Mount(ctx); err != nil {
			return fmt.Errorf("mount %q: %w", p.Name(), err)
		}
		info := ExtensionInfo{
			Name:  p.Name(),
			Seams: p.Provides(),
		}
		// Count tools this plugin added.
		for _, tp := range ctx.ToolProviders[beforeTools:] {
			if tp == nil {
				continue
			}
			for name, t := range tp.Tools() {
				info.Tools++
				info.ToolList = append(info.ToolList, ToolInfo{
					Name:        name,
					Description: t.Description,
				})
			}
		}
		// Ensure deterministic order: map iteration is non-deterministic in Go.
		sort.SliceStable(info.ToolList, func(i, j int) bool { return info.ToolList[i].Name < info.ToolList[j].Name })
		// Count commands this plugin added.
		info.Commands = len(ctx.Commands) - beforeCmds
		ctx.Infos = append(ctx.Infos, info)
	}

	return nil
}
