package main

// registry.go: the host's registry-browser toggle adapter (v0.14.1) -
// core.RegistryToggles behind the /disabled surface, one write path
// into config.yaml's `disabled:` fold (the same key boot reads, Q7's
// one code path). The toggle order is always: validate the name
// against its registry (disable direction only - a re-enable must
// always work, even for a name whose registry entry has since
// vanished), mutate the live fold, write config.yaml, then apply the
// live effect. A persistence failure leaves the fold and the live
// state untouched rather than half-applied.

import (
	"fmt"
	"slices"
	"sort"
	"sync"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
	"github.com/EndoTheDev/omega/modules/continuous"
	"github.com/EndoTheDev/omega/modules/skills"
)

// omegaRegistries implements core.RegistryToggles for the serve host.
// modules is the mount machinery's toggle adapter (omegaModules);
// this is the sibling for the skills/tools/agents registries - both
// write DISABLED: folds through the same writeDisabled, and both name
// the modules namespace from the live registry at write time.
type omegaRegistries struct {
	pctx       *agent.Context
	reg        *agent.Registry
	fold       *DisabledNamespaces // the live fold pointer (shared with omegaModules)
	configPath string
	home       string
	cont       *continuous.Plugin // nil when the continuous capsule is not mounted
	logger     core.LoggerProvider

	mu sync.Mutex // one fold mutation + write at a time
}

// Compile-time assertion that omegaRegistries implements the seam.
var _ core.RegistryToggles = (*omegaRegistries)(nil)

// Sync applies a hand-edited disabled fold (the config watcher path):
// the skills/tools/agents namespaces take live, exactly as a toggle
// would leave them. The modules namespace is untouched - the mount
// machinery is boot business; a hand edit there keeps applying at the
// next boot exactly as it did before v0.14.1.
func (r *omegaRegistries) Sync(d DisabledNamespaces) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.fold.Skills = sortedCopy(d.Skills)
	r.fold.Tools = sortedCopy(d.Tools)
	r.fold.Agents = sortedCopy(d.Agents)
	skillNames, toolNames, agentNames := r.fold.Skills, r.fold.Tools, r.fold.Agents
	r.mu.Unlock()
	r.applyLive(skillNames, toolNames, agentNames)
	// The serving channels re-read: same law the module toggle carries
	// (the fold changed; every LiveSeams surface follows).
	if r.pctx == nil {
		return
	}
	for _, ch := range r.pctx.Channels {
		rewire, ok := ch.(core.LiveSeams)
		if !ok {
			continue
		}
		if err := rewire.RewireSeams(r.pctx); err != nil && r.logger != nil {
			r.logger.Errorf("omega: disabled fold hand-edit re-wire failed: %v", err)
		}
	}
}

// applyLive pushes one fold state into the live registries (the
// skills provider, the tool opt-out, the continuous machine).
func (r *omegaRegistries) applyLive(skillNames, toolNames, agentNames []string) {
	if r.pctx != nil {
		r.pctx.DisabledTools = toolNames
		if sp, ok := r.pctx.Skills.(*skills.SkillsProvider); ok && sp != nil {
			sp.SetDisabled(skillNames)
		}
	}
	if r.cont != nil {
		r.cont.SetDisabledAgents(agentNames)
	}
}

// Disabled returns the three registry lists verbatim (sorted; the wire
// shows the persisted order).
func (r *omegaRegistries) Disabled() core.DisabledNames {
	r.mu.Lock()
	defer r.mu.Unlock()
	return core.DisabledNames{
		Skills: slices.Clone(r.fold.Skills),
		Tools:  slices.Clone(r.fold.Tools),
		Agents: slices.Clone(r.fold.Agents),
	}
}

// ToggleSkill implements core.RegistryToggles: a disabled skill leaves
// the session prompt build; the registry keeps it (the tab lists it).
func (r *omegaRegistries) ToggleSkill(name string, enabled bool) error {
	return r.toggle("skills", name, enabled, r.skillKnown, func(next []string) {
		if sp, ok := r.pctx.Skills.(*skills.SkillsProvider); ok && sp != nil {
			sp.SetDisabled(next)
		}
	})
}

// ToggleTool implements core.RegistryToggles: a disabled tool is
// absent from every tool union - the merge law is untouched, the
// filter runs after it (Agent.Tools / the loop's loop-merge filter).
func (r *omegaRegistries) ToggleTool(name string, enabled bool) error {
	return r.toggle("tools", name, enabled, r.toolKnown, func(next []string) {
		if r.pctx != nil {
			r.pctx.DisabledTools = next
		}
	})
}

// ToggleAgent implements core.RegistryToggles - the soft toggle. The
// agent home directory is never touched: registration is folder
// presence, and the fold only removes the roster row and the wake.
func (r *omegaRegistries) ToggleAgent(name string, enabled bool) error {
	return r.toggle("agents", name, enabled, r.agentKnown, func(next []string) {
		if r.cont != nil {
			r.cont.SetDisabledAgents(next)
		}
	})
}

// toggle is the one mutation+write+apply path for all three registries.
// known is consulted only when DISABLING (the add direction) - enabling
// an entry whose registry has moved on just cleans the fold.
func (r *omegaRegistries) toggle(kind, name string, enabled bool, known func(name string) error, apply func(next []string)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var cur []string
	switch kind {
	case "skills":
		cur = r.fold.Skills
	case "tools":
		cur = r.fold.Tools
	case "agents":
		cur = r.fold.Agents
	}
	var next []string
	if enabled {
		if !slices.Contains(cur, name) {
			return nil // already enabled: honest no-op
		}
		next = slices.DeleteFunc(slices.Clone(cur), func(n string) bool { return n == name })
	} else {
		if slices.Contains(cur, name) {
			return nil // already disabled: honest no-op
		}
		if err := known(name); err != nil {
			return err
		}
		next = sortAppend(slices.Clone(cur), name)
	}
	prev := slices.Clone(cur)
	switch kind {
	case "skills":
		r.fold.Skills = next
	case "tools":
		r.fold.Tools = next
	case "agents":
		r.fold.Agents = next
	}
	// The modules namespace is registry truth at write time - the live
	// mount state, not a stale boot list.
	r.fold.Modules = disabledNow(r.reg)
	if err := writeDisabled(r.configPath, *r.fold); err != nil {
		switch kind {
		case "skills":
			r.fold.Skills = prev
		case "tools":
			r.fold.Tools = prev
		case "agents":
			r.fold.Agents = prev
		}
		if r.logger != nil {
			r.logger.Errorf("omega: %s %q toggle: config.yaml write failed: %v", kind, name, err)
		}
		return fmt.Errorf("%s %s, but the config.yaml disabled write failed: %w", kind, name, err)
	}
	// The live effect is second: the fold is written, the serving
	// surfaces follow (the channel also re-reads the response lists).
	apply(next)
	if r.logger != nil {
		r.logger.Printf("omega: %s %q %s (config.yaml disabled fold)", kind, name, toggleWord(enabled))
	}
	return nil
}

// skillKnown answers an unknown skill name loudly: the skills tab only
// lists loaded skills, so a persisted unknown name would be invisible
// forever (and re-enabling one would look like a silent no-op).
func (r *omegaRegistries) skillKnown(name string) error {
	if r.pctx == nil || r.pctx.Skills == nil {
		return nil // no skills module: nothing to validate
	}
	lister, ok := r.pctx.Skills.(core.SkillsLister)
	if !ok {
		return nil
	}
	inventory, err := lister.ListSkills()
	if err != nil {
		return fmt.Errorf("skills registry: %w", err)
	}
	for _, s := range inventory {
		if s.Name == name {
			return nil
		}
	}
	return &registryToggleError{kind: "skills", name: name, unknown: true,
		cause: fmt.Errorf("no such skill in the loaded inventory")}
}

// toolKnown answers an unknown tool name loudly: the tools tab lists
// the agent's registry, so the same invisibility law applies. The
// check is against the FULL registry (the agent's built-ins plus every
// ToolProvider), not the current filtered union.
func (r *omegaRegistries) toolKnown(name string) error {
	if r.pctx == nil {
		return nil
	}
	for _, tp := range r.pctx.ToolProviders {
		if tp == nil {
			continue
		}
		if _, ok := tp.Tools()[name]; ok {
			return nil
		}
	}
	return &registryToggleError{kind: "tools", name: name, unknown: true,
		cause: fmt.Errorf("no tool with that name in the registry")}
}

// agentKnown answers unknown + non-continuous agents loudly - the soft
// toggle's semantics (roster + wake exclusion) only mean something for
// agent homes; the main home is the daemon and has neither.
func (r *omegaRegistries) agentKnown(name string) error {
	p, err := agent.LoadProfile(r.home, name)
	if err != nil {
		return &registryToggleError{kind: "agents", name: name, unknown: true,
			cause: fmt.Errorf("no agent home: %w", err)}
	}
	if !p.Continuous {
		return fmt.Errorf("agent %s is the main home (the daemon) - it has no roster to leave and no wake to refuse", name)
	}
	return nil
}

// registryToggleError is one refused registry toggle. Unknown marks the
// 404 lane: a name no registry knows is refused, never persisted.
type registryToggleError struct {
	kind    string
	name    string
	unknown bool
	cause   error
}

func (e *registryToggleError) Error() string {
	return e.kind + " toggle refused: " + e.cause.Error()
}

func (e *registryToggleError) Unwrap() error { return e.cause }

// Unknown implements the 404 lane (core.UnknownName).
func (e *registryToggleError) Unknown() bool { return e.unknown }

// sortAppend appends one name to a sorted list and keeps it sorted.
func sortAppend(names []string, name string) []string {
	names = append(names, name)
	sort.Strings(names)
	return names
}