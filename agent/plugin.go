package agent

import (
	"fmt"

	"github.com/EndoTheDev/omega/core"
)

// Context is the shared service container that extensions mount into
// (arc 1b, t_7bb4790d: it moved to core with the seam contracts -
// ChannelDeps, CronDeps and the Frontend seam reference it by type).
// The mount machinery below stays in the agent layer until the
// connector phase rewrites it.
type Context = core.Context

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

// Describer is an optional Plugin capability. Description is a one-
// line, human-readable description shown by inventory surfaces (the
// TUI /extensions listing and the webui GET /extensions endpoint);
// plugins that don't implement it are listed with an empty
// description. ConfigKey names the config section the plugin reads
// from ctx.Configs - empty means the plugin reads no section. The
// host never routes configs by hand: RouteConfigs consults the
// declaration below and pulls each declared section from the host's
// raw config (arc 2b, t_889388cc).
type Describer interface {
	Description() string
	ConfigKey() string
}

// Dismounter is an optional Plugin capability (v0.12 E1): it lives in
// dismount.go with the teardown machinery it belongs to.

// Toggleable is an optional Plugin capability (the tier law, grill
// Q1/Q13): only the opinionated capsules may appear in the disable
// list. Machine-critical mounts (provider, store, loop, logging - the
// behavior the machine needs to be a machine) and the engine ring
// (http_channel, tui) never implement it, and disabling them fails
// the boot loud. Default is NOT toggleable: a plugin opts in.
type Toggleable interface {
	Toggleable() bool
}

// filterDisabled applies the omega.json disable list (grill Q1) to the
// plugin set before anything mounts: a disabled name must exist and
// name a toggleable capsule (the tier law), and a remaining plugin
// whose required seam is mounted only by a disabled module fails the
// boot naming the edge (Q3 - no silent degradation).
func filterDisabled(plugins []Plugin, disabled []string) ([]Plugin, error) {
	if len(disabled) == 0 {
		return plugins, nil
	}
	byName := make(map[string]Plugin, len(plugins))
	for _, p := range plugins {
		if _, dup := byName[p.Name()]; dup {
			return nil, fmt.Errorf("two plugins named %q", p.Name())
		}
		byName[p.Name()] = p
	}
	off := make(map[string]bool, len(disabled))
	for _, name := range disabled {
		p, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("disabled %q: no such module", name)
		}
		if t, ok := p.(Toggleable); !ok || !t.Toggleable() {
			return nil, fmt.Errorf("module %q is not toggleable - the disable list only names the opinionated capsules, %q is machine-critical or engine ring", name, name)
		}
		off[name] = true
	}
	// Seam providers, split live/disabled, for the Q3 edge check.
	provides := make(map[string]string)
	offProvides := make(map[string]string)
	for _, p := range plugins {
		if off[p.Name()] {
			for _, seam := range p.Provides() {
				if _, ok := offProvides[seam]; !ok {
					offProvides[seam] = p.Name()
				}
			}
			continue
		}
		for _, seam := range p.Provides() {
			provides[seam] = p.Name()
		}
	}
	var enabled []Plugin
	for _, p := range plugins {
		if off[p.Name()] {
			continue
		}
		for _, req := range p.Requires() {
			if _, ok := provides[req]; ok {
				continue
			}
			if by, ok := offProvides[req]; ok {
				return nil, fmt.Errorf("module %q requires seam %q, mounted only by disabled module %q - disable %q too, or re-enable %q (Q3: no silent degradation)", p.Name(), req, by, p.Name(), by)
			}
			return nil, fmt.Errorf("plugin %q requires seam %q but no plugin provides it", p.Name(), req)
		}
		enabled = append(enabled, p)
	}
	return enabled, nil
}

// ConfigSource is the host's raw config as the config router sees
// it: typed config sections by name. cmd/omega implements it on its
// Config type; a future or embedding host serves whatever sections
// its own modules declare.
type ConfigSource interface {
	ConfigSection(name string) (any, bool)
}

// RouteConfigs builds the per-plugin config map from the host's raw
// config. Each plugin that declares a config section (Describer's
// ConfigKey) receives the matching section as Configs[key]; sections
// the host does not provide are absent, and the plugin falls back to
// its default at Mount. Call once per host wiring, after the plugins
// are collected and before MountAll.
func RouteConfigs(plugins []Plugin, raw ConfigSource) map[string]any {
	configs := make(map[string]any)
	if raw == nil {
		return configs
	}
	for _, p := range plugins {
		d, ok := p.(Describer)
		if !ok {
			continue
		}
		key := d.ConfigKey()
		if key == "" {
			continue
		}
		if v, ok := raw.ConfigSection(key); ok {
			configs[key] = v
		}
	}
	return configs
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
	"trust":          true,
	"cron":           true,
	"kanban":         true,
	"notifications":  true,
	"verdicts":       true,
}

// MountAll lives in extensionapi.go: it keeps its classic signature
// and delegates to MountAllAPI, which owns the plugin-set wiring
// (provides sweep for conflicts, topo sort, mount dispatch, emitter
// delivery) in both mount styles.

// buildProvidesMap builds the seam -> plugin name map and detects conflicts
// where two plugins provide the same exclusive seam. The exclusive
// claims also land on the registry's owner ledger, so classic provides
// and API seam registrations conflict with each other, not just among
// themselves (one name, one tenant - Q5).
func buildProvidesMap(plugins []Plugin, reg *Registry) (map[string]string, error) {
	provides := make(map[string]string)
	for _, p := range plugins {
		for _, seam := range p.Provides() {
			if exclusiveSeams[seam] {
				if existing, ok := provides[seam]; ok {
					return nil, fmt.Errorf("seam %q provided by both %q and %q", seam, existing, p.Name())
				}
				reg.claimSlotQuiet(seam, p.Name())
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

// mountPlugins lives in extensionapi.go - it is the dispatch point of
// the dual mount (APIPlugin -> registration surface, Plugin ->
// classic Mount) and records what it contributed so /extensions can
// display accurate counts.
