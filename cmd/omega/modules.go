// The host-side module toggle adapter (v0.12 E2): the mount registry
// plus the omega.json persistence behind the core.ModuleToggles seam.
// The toggle machinery itself lives in agent.Registry (Q7's one code
// path with boot); this file owns only the disabled-list write - the
// same omega.json boot reads, so a UI toggle and a boot always agree.
package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// omegaModules implements core.ModuleToggles for the serve host.
type omegaModules struct {
	pctx *agent.Context
	reg  *agent.Registry
	// deps arms the fence handles of an enabled module - the same
	// values the host used for the boot-time RunGroup.StartAll.
	deps core.StartDeps
	// omegaPath is the omega.json file the toggle list persists to -
	// the file boot reads (one code path, Q7).
	omegaPath string
	// logger records every applied toggle; nil logs nothing.
	logger core.LoggerProvider
}

// Compile-time assertion that omegaModules implements the seam.
var _ core.ModuleToggles = (*omegaModules)(nil)

// Modules returns the registry introspection verbatim: the settings
// capsule renders it, the registry provides it (the small law).
func (m *omegaModules) Modules() []core.ModuleStatus { return m.reg.Modules() }

// Refusals returns the toggle ledger (Q12) verbatim.
func (m *omegaModules) Refusals() []core.Refusal { return m.reg.Refusals() }

// Toggle applies the mount machinery, then writes omega.json so the
// next boot starts from the same toggle state (one code path, Q7):
// the disabled list is exactly the catalog modules that are not live.
func (m *omegaModules) Toggle(ctx context.Context, name string, enabled bool) error {
	if err := m.reg.Toggle(m.pctx, name, enabled, m.deps); err != nil {
		return err
	}
	// The serving channels re-read the Context: a disabled module's
	// tools, panels, and routes die without a restart, a re-enabled
	// one's return (E2 - the toggle machinery changed the Context,
	// the surfaces built from it follow). No LiveSeams-capable channel
	// means nothing module-dependent is being served.
	for _, ch := range m.pctx.Channels {
		rewire, ok := ch.(core.LiveSeams)
		if !ok {
			continue
		}
		if err := rewire.RewireSeams(m.pctx); err != nil {
			// The toggle applied and omega.json will record it, but a
			// served surface failed to re-read: loud, not silent - a
			// stale route would render a module the runtime lost.
			err = fmt.Errorf("module %s applied, but the channel re-wire failed: %w", toggleWord(enabled), err)
			if m.logger != nil {
				m.logger.Errorf("omega: %v", err)
			}
			return err
		}
	}
	off := disabledNow(m.reg)
	if err := SaveDisabled(m.omegaPath, off); err != nil {
		// The toggle applied but the write failed: loud, not silent -
		// the next boot would silently resurrect the module.
		err = makeModuleToggleError(name, enabled, err)
		if m.logger != nil {
			m.logger.Errorf("omega: module %s applied but omega.json write failed: %v", toggleWord(enabled), err)
		}
		return err
	}
	if m.logger != nil {
		m.logger.Printf("omega: module %q %s (omega.json disabled=%v)", name, toggleWord(enabled), off)
	}
	return nil
}

// makeModuleToggleError wraps the persistence failure so the caller
// sees which module toggled and which direction the write lost.
func makeModuleToggleError(name string, enabled bool, cause error) error {
	return &moduleToggleError{name: name, enabled: enabled, cause: cause}
}

type moduleToggleError struct {
	name    string
	enabled bool
	cause   error
}

func (e *moduleToggleError) Error() string {
	return "module " + e.name + " " + toggleWord(e.enabled) + ", but the omega.json write failed: " + e.cause.Error()
}

func (e *moduleToggleError) Unwrap() error { return e.cause }

// toggleWord renders a toggle result for logs - past-tense verb.
func toggleWord(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

// disabledNow derives the omega.json list from the live registry: the
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
