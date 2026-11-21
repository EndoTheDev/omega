// The host-side module toggle adapter.
package main

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// omegaModules implements core.ModuleToggles for the serve host.
type omegaModules struct {
	pctx *agent.Context
	reg  *agent.Registry
	deps core.StartDeps
	configPath string
	// fold is the live disabled: fold - the namespaces the module toggle doesn't
	// own (skills/tools/agents) must survive the module namespace's rewrite, so the write
	// fold is the shared pointer with omegaRegistries.
	fold *DisabledNamespaces
	// mu serializes every fold mutation + writeDisabled across BOTH
	// toggle surfaces (module + registry) - one shared pointer, one
	// shared lock; without it two live PUTs race the fold slice and
	// the atomic config.yaml rename.
	mu *sync.Mutex
	// logger records every applied toggle; nil logs nothing.
	logger agent.LoggerProvider
}

// Compile-time assertion that omegaModules implements the seam.
var _ core.ModuleToggles = (*omegaModules)(nil)

// Modules returns the registry introspection verbatim.
func (m *omegaModules) Modules() []core.ModuleStatus { return m.reg.Modules() }

// Refusals returns the toggle ledger verbatim.
func (m *omegaModules) Refusals() []core.Refusal { return m.reg.Refusals() }

// Toggle applies the mount machinery, then writes the config.yaml `disabled:` list so
// the next boot starts from the same toggle state: the disabled
// list is exactly the catalog modules that are not live.
func (m *omegaModules) Toggle(ctx context.Context, name string, enabled bool) error {
	// the WHOLE toggle is the critical section (a snapshot race found the narrow one):
	// reg.Toggle mutates the registry and disabledNow
	// snapshots ALL of it, so a concurrent toggle running beside the snapshot reads a
	// half-mutated registry and the last full-fold write overwrites the earlier toggles.
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.reg.Toggle(m.pctx, name, enabled, m.deps); err != nil {
		return err
	}
	for _, ch := range m.pctx.Channels {
		rewire, ok := ch.(core.LiveSeams)
		if !ok {
			continue
		}
		if err := rewire.RewireSeams(m.pctx); err != nil {
			// The toggle applied and config.yaml will record it, but a served surface failed.
			err = fmt.Errorf("module %s applied, but the channel re-wire failed: %w", toggleWord(enabled), err)
			if m.logger != nil {
				m.logger.Errorf("omega: %v", err)
			}
			return err
		}
	}
	off := disabledNow(m.reg)
	m.fold.Modules = off // registry truth at write time; siblings keep their namespaces
	if err := writeDisabled(m.configPath, *m.fold); err != nil {
		// The toggle applied but the write failed: loud, not silent -
		// the next boot would silently resurrect the module.
		err = makeModuleToggleError(name, enabled, err)
		if m.logger != nil {
			m.logger.Errorf("omega: module %s applied but config.yaml disabled write failed: %v", toggleWord(enabled), err)
		}
		return err
	}
	if m.logger != nil {
		m.logger.Printf("omega: module %q %s (config.yaml disabled=%v)", name, toggleWord(enabled), off)
	}
	return nil
}

// makeModuleToggleError wraps the persistence failure so the caller sees which module.
func makeModuleToggleError(name string, enabled bool, cause error) error {
	return &moduleToggleError{name: name, enabled: enabled, cause: cause}
}

type moduleToggleError struct {
	name    string
	enabled bool
	cause   error
}

func (e *moduleToggleError) Error() string {
	return "module " + e.name + " " + toggleWord(e.enabled) + ", but the config.yaml disabled write failed: " + e.cause.Error()
}

func (e *moduleToggleError) Unwrap() error { return e.cause }

// toggleWord renders a toggle result for logs - past-tense verb.
func toggleWord(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

// disabledNow derives the config.yaml disabled: list from the live registry: the
// catalog names that are not mounted, sorted (stable file output).
func disabledNow(reg *agent.Registry) []string {
	var off []string
	for _, m := range reg.Modules() {
		if m.State != "mounted" {
			off = append(off, m.Name)
		}
	}
	sort.Strings(off)
	return off
}
