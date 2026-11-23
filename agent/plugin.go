package agent

import (
	"fmt"
	"sort"

	"github.com/EndoTheDev/omega/core"
)

// Context is the shared service container that extensions mount.
type Context = core.Context

// Plugin is the contract every extension implements.
type Plugin interface {
	Name() string
	// Provides lists seam names this plugin mounts (e.g. "provider", "store", "tools").
	Provides() []string
	// Requires lists seam names that must be mounted before this
	// plugin. MountAllAPI uses these for topological ordering.
	Requires() []string
	// MountWithAPI mounts the plugin through its registrations.
	MountWithAPI(ctx *Context, api *ExtensionAPI) error
}

// Describer is an optional Plugin capability. Description is a one- line,
// human-readable description shown by inventory surfaces (the serving surface
// extensions listing and the webui GET /extensions endpoint); plugins that don't
// implement it are listed with an empty description. The host never routes configs by
// hand: RouteConfigs consults the declaration below and pulls each declared section
// from the host's raw config.
type Describer interface {
	Description() string
	ConfigKey() string
}

// Dismounter is an optional Plugin capability.

// Toggleable is an optional Plugin capability (the tier law). The
// always-on tier - agent-loop, provider and logging - and the shell's channel (served
// by api/) never implement it, and disabling them fails the boot loud.
type Toggleable interface {
	Toggleable() bool
}

// filterDisabled applies the config.yaml `disabled:` list to the plugin set
// before anything mounts: a disabled name must exist and name a toggleable capsule (the
// tier law), and a remaining plugin whose required seam is mounted only by a disabled
// module fails the boot naming the edge, never a silent degradation.
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
			return nil, fmt.Errorf("module %q is not toggleable - the disable list only names the opinionated capsules, %q is in the always-on tier (agent-loop, provider, logging)", name, name)
		}
		off[name] = true
	}
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

// AlwaysOffDisabled is the ALWAYS OFF law's runtime half: given the capsules an agent
// home enables, it answers the disable list that leaves exactly those (plus their
// required seams) mounted and every other toggleable capsule off. A REQUIRES edge is
// satisfied by keeping that seam's provider on: enabling cron keeps notifications on
// (its seam), never the other way around. Nothing else is enabled silently - a capsule
// the agent did not name is off even when the host runs it.
func AlwaysOffDisabled(plugins []Plugin, enabled []string) ([]string, error) {
	byName := make(map[string]Plugin, len(plugins))
	providers := map[string][]string{}
	for _, p := range plugins {
		if _, dup := byName[p.Name()]; dup {
			return nil, fmt.Errorf("two plugins named %q", p.Name())
		}
		byName[p.Name()] = p
		for _, seam := range p.Provides() {
			providers[seam] = append(providers[seam], p.Name())
		}
	}
	on := map[string]bool{}
	queue := make([]string, 0, len(enabled))
	for _, name := range enabled {
		p, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("agent toggle %q: no such module", name)
		}
		if t, ok := p.(Toggleable); !ok || !t.Toggleable() {
			return nil, fmt.Errorf("agent toggle %q: the module is not toggleable - the always-on tier (agent-loop + provider + logging) is always on and cannot be toggled", name)
		}
		if !on[name] {
			on[name] = true
			queue = append(queue, name)
		}
	}
	for i := 0; i < len(queue); i++ {
		for _, seam := range byName[queue[i]].Requires() {
			satisfied := false
			for _, name := range providers[seam] {
				if on[name] {
					satisfied = true
					break
				}
			}
			if satisfied || len(providers[seam]) == 0 {
				continue // already on, or nothing provides it (the mount fails loud elsewhere)
			}
			pick := providers[seam][0]
			for _, name := range providers[seam] {
				if t, ok := byName[name].(Toggleable); !ok || !t.Toggleable() {
					pick = name
					break
				}
			}
			if !on[pick] {
				on[pick] = true
				queue = append(queue, pick)
			}
		}
	}
	var disabled []string
	for _, p := range plugins {
		if on[p.Name()] {
			continue
		}
		if t, ok := p.(Toggleable); ok && t.Toggleable() {
			disabled = append(disabled, p.Name())
		}
	}
	sort.Strings(disabled)
	return disabled, nil
}

// ConfigSource is the host's raw config as the config router sees it.
type ConfigSource interface {
	ConfigSection(name string) (any, bool)
}

// RouteConfigs builds the per-plugin config map from the host's raw config.
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
var exclusiveSeams = map[string]bool{
	"provider":      true,
	"compactor":     true,
	"store":         true,
	"skills":        true,
	"loop":          true,
	"logger":        true,
	"memory":        true,
	"prompt":        true,
	"approvals":     true,
	"trust":         true,
	"cron":          true,
	"kanban":        true,
	"notifications": true,
	"verdicts":      true,
}

// MountAllAPI lives in extensionapi.go: it owns the plugin-set wiring (provides sweep
// for conflicts, topo sort, mount dispatch, emitter delivery).

// buildProvidesMap builds the seam -> plugin name map and detects conflicts where two.
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

	for _, p := range plugins {
		for _, req := range p.Requires() {
			if _, ok := provides[req]; !ok {
				return nil, fmt.Errorf("plugin %q requires seam %q but no plugin provides it", p.Name(), req)
			}
		}
	}

	return provides, nil
}

// topoSort returns plugins in dependency order so that a plugin's required seams are
// always provided by a plugin earlier in the list.
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

// mountOne lives in dismount.go.
