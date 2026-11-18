package main

// registry.go: the host's registry-browser toggle adapter (v0.14.1). The toggle order
// is always: validate the name against its registry (disable direction only - a
// re-enable must always work, even for a name whose registry entry has since vanished),
// mutate the live fold, write config.yaml, then apply the live effect.

import (
	"fmt"
	"slices"
	"sort"
	"sync"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// omegaRegistries implements core.RegistryToggles for the serve host.
type omegaRegistries struct {
	pctx       *agent.Context
	reg        *agent.Registry
	fold       *DisabledNamespaces // the live fold pointer (shared with omegaModules)
	configPath string
	home       string
	setAgents  func([]string) // the continuous capsule's disabled-agents fold, nil when it is not mounted
	logger     agent.LoggerProvider

	mu *sync.Mutex // one fold mutation + write at a time - SHARED pointer with omegaModules (gate must-fix 1)
}

// Compile-time assertion that omegaRegistries implements the seam.
var _ core.RegistryToggles = (*omegaRegistries)(nil)

// Sync applies a hand-edited disabled fold (the config watcher path): the
// skills/tools/agents namespaces take live, exactly as a toggle would leave them. The
// modules namespace is untouched - the mount machinery is boot business; a hand edit
// there keeps applying at the next boot exactly as it did before v0.14.1.
func (r *omegaRegistries) Sync(d DisabledNamespaces) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.fold.SkillNames = sortedCopy(d.SkillNames)
	r.fold.Tools = sortedCopy(d.Tools)
	r.fold.Agents = sortedCopy(d.Agents)
	r.mu.Unlock()
	r.applyLive()
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

// applyLive pushes the live fold state into the live registries.
func (r *omegaRegistries) applyLive() {
	if r.pctx == nil {
		return
	}
	r.mu.Lock()
	tools := r.fold.Tools
	skillList := r.fold.SkillNames
	agentList := r.fold.Agents
	r.mu.Unlock()
	r.pctx.DisabledTools = tools
	if sp, ok := r.pctx.Services().Resolve("skills").(skillDisabler); ok && sp != nil {
		sp.SetDisabled(skillList)
	}
	if r.setAgents != nil {
		r.setAgents(agentList)
	}
}

// skillDisabler is the host registry's own need face of the skills capsule (part 3).
type skillDisabler interface {
	SetDisabled(names []string)
}

// skillInventory is the registry's validation face.
type skillInventory interface {
	SkillsEntries() (index map[string]string, contents map[string]string, err error)
}

// Disabled returns the three registry lists verbatim.
func (r *omegaRegistries) Disabled() core.DisabledNames {
	r.mu.Lock()
	defer r.mu.Unlock()
	return core.DisabledNames{
		Skills: slices.Clone(r.fold.SkillNames),
		Tools:  slices.Clone(r.fold.Tools),
		Agents: slices.Clone(r.fold.Agents),
	}
}

// ToggleSkill implements core.RegistryToggles.
func (r *omegaRegistries) ToggleSkill(name string, enabled bool) error {
	return r.toggle("skills", name, enabled, r.skillKnown, func(next []string) {
		if sp, ok := r.pctx.Services().Resolve("skills").(skillDisabler); ok && sp != nil {
			sp.SetDisabled(next)
		}
	})
}

// ToggleTool implements core.RegistryToggles.
func (r *omegaRegistries) ToggleTool(name string, enabled bool) error {
	return r.toggle("tools", name, enabled, r.toolKnown, func(next []string) {
		if r.pctx != nil {
			r.pctx.DisabledTools = next
		}
	})
}

// ToggleAgent implements core.RegistryToggles - the soft toggle. The agent home
// directory is never touched: registration is folder presence, and the fold only
// removes the roster row and the wake. The DEFAULT agent refuses HERE at the seam as
// well as the handler: the lock is belt-and-suspenders - every caller of the seam (curl
// through the handler, the setup wizard, a future CLI verb) hits the same wall.
func (r *omegaRegistries) ToggleAgent(name string, enabled bool) error {
	if !enabled && name == agent.DefaultAgentName {
		return &registryToggleError{kind: "agents", name: name,
			cause: fmt.Errorf("the default agent cannot be disabled - it must always be on")}
	}
	return r.toggle("agents", name, enabled, r.agentKnown, func(next []string) {
		if r.setAgents != nil {
			r.setAgents(next)
		}
	})
}

// toggle is the one mutation+write+apply path for all three registries.
func (r *omegaRegistries) toggle(kind, name string, enabled bool, known func(name string) error, apply func(next []string)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var cur []string
	switch kind {
	case "skills":
		cur = r.fold.SkillNames
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
		r.fold.SkillNames = next
	case "tools":
		r.fold.Tools = next
	case "agents":
		r.fold.Agents = next
	}
	r.fold.Modules = disabledNow(r.reg)
	if err := writeDisabled(r.configPath, *r.fold); err != nil {
		switch kind {
		case "skills":
			r.fold.SkillNames = prev
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
	apply(next)
	if r.logger != nil {
		r.logger.Printf("omega: %s %q %s (config.yaml disabled fold)", kind, name, toggleWord(enabled))
	}
	return nil
}

// skillKnown answers an unknown skill name loudly.
func (r *omegaRegistries) skillKnown(name string) error {
	if r.pctx == nil {
		return nil // no skills module: nothing to validate
	}
	lister, hasSkills := r.pctx.Services().Resolve("skills").(skillInventory)
	if !hasSkills {
		return nil
	}
	_, contents, listErr := lister.SkillsEntries()
	if listErr != nil {
		return fmt.Errorf("skills registry: %w", listErr)
	}
	if _, ok := contents[name]; ok {
		return nil
	}
	return &registryToggleError{kind: "skills", name: name, unknown: true,
		cause: fmt.Errorf("no such skill in the loaded inventory")}
}

// toolKnown answers an unknown tool name loudly.
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

// agentKnown answers unknown + non-continuous agents loudly.
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
