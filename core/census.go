// The teardown census (v0.12 phase E1, implementation sketch 2): a
// snapshot of every registration surface on a Context plus the
// lifecycle run-group. Dismount proofs demand after-dismount ==
// before-mount; the census is the equality that proves - or fails.
//
// stdlib-only by the core law: this is vocabulary, never behavior.
package core

import (
	"maps"
	"slices"
)

// CensusSnapshot is one census. Every field is derived from the
// Context alone - a fresh Context and a fully dismounted Context must
// produce equal snapshots (zero surface, zero run-group entries).
type CensusSnapshot struct {
	// Slots records the exclusive seams that are mounted (one flag
	// each of the 15 exclusive slots + the host-built captioner).
	Slots map[string]bool

	// Tools are the model-facing tool names across every provider,
	// alias entries hidden from the inventory.
	Tools []string

	// Commands are the slash command names, in registration order.
	Commands []string

	// Additive surfaces by count; channels and providers carry no
	// name seam, so counts are the identity the census can hold.
	Channels         int
	TUIProviders     int
	PromptSectioners int
	PanelProviders   int

	// Panels lists every declared panel id plus its action ids as
	// "panel" and "panel/action" entries, sorted - duplicates reveal
	// a double-registered additive surface.
	Panels []string

	// Delegation pipe surfaces (nil or mounted).
	InjectedMessages   bool
	PendingDelegations bool

	// Routes are the registered module REST route patterns, in
	// registration order - the module surface the serving channels
	// re-read live (E2). Sort-stable like Panels.
	Routes []string

	// Handles are the run-group entries as "owner/name" plus an
	// started marker folded into the string, in registration order.
	Handles []string
}

// Census snapshots every registration surface of ctx.
func Census(ctx *Context) CensusSnapshot {
	s := CensusSnapshot{Slots: map[string]bool{}}
	for _, rt := range ctx.ModuleRoutes {
		s.Routes = append(s.Routes, rt.Pattern)
	}
	for _, slot := range []struct {
		name string
		set  bool
	}{
		{"provider", ctx.Provider != nil},
		{"compactor", ctx.Compactor != nil},
		{"store", ctx.Store != nil},
		{"skills", ctx.Skills != nil},
		{"loop", ctx.Loop != nil},
		{"logging", ctx.Logger != nil},
		{"memory", ctx.Memory != nil},
		{"prompt_builder", ctx.PromptBuilder != nil},
		{"captioner", ctx.Captioner != nil},
		{"frontend", ctx.Frontend != nil},
		{"trust", ctx.Trust != nil},
		{"cron", ctx.Cron != nil},
		{"notifications", ctx.Notifications != nil},
		{"verdicts", ctx.Verdicts != nil},
	} {
		if slot.set {
			s.Slots[slot.name] = true
		}
	}
	for _, tp := range ctx.ToolProviders {
		if tp == nil {
			continue
		}
		for name, t := range tp.Tools() {
			if t.AliasOf != "" {
				continue // alias shims are not inventory
			}
			s.Tools = append(s.Tools, name)
		}
	}
	slices.Sort(s.Tools)
	for _, c := range ctx.Commands {
		s.Commands = append(s.Commands, c.Name)
	}
	slices.Sort(s.Commands)
	s.Channels = len(ctx.Channels)
	s.TUIProviders = len(ctx.TUIProviders)
	s.PromptSectioners = len(ctx.PromptSectioners)
	s.PanelProviders = len(ctx.PanelProviders)
	for _, pp := range ctx.PanelProviders {
		if pp == nil {
			continue
		}
		for _, p := range pp.Panels() {
			s.Panels = append(s.Panels, p.ID)
			for _, a := range p.Actions {
				s.Panels = append(s.Panels, p.ID+"/"+a.ID)
			}
		}
	}
	slices.Sort(s.Panels)
	s.InjectedMessages = ctx.InjectedMessages != nil
	s.PendingDelegations = ctx.PendingDelegations != nil
	if ctx.Lifecycle != nil {
		for _, h := range ctx.Lifecycle.Snapshot() {
			marker := "off"
			if h.Started {
				marker = "on"
			}
			s.Handles = append(s.Handles, h.Owner+"/"+h.Name+":"+marker)
		}
	}
	return s
}

// Equal reports whether two censuses hold the same surface. Empty
// slice and missing slice are the same zero.
func (s CensusSnapshot) Equal(o CensusSnapshot) bool {
	return maps.Equal(s.Slots, o.Slots) &&
		slices.Equal(s.Tools, o.Tools) &&
		slices.Equal(s.Commands, o.Commands) &&
		s.Channels == o.Channels &&
		s.TUIProviders == o.TUIProviders &&
		s.PromptSectioners == o.PromptSectioners &&
		s.PanelProviders == o.PanelProviders &&
		slices.Equal(s.Panels, o.Panels) &&
		s.InjectedMessages == o.InjectedMessages &&
		s.PendingDelegations == o.PendingDelegations &&
		slices.Equal(s.Handles, o.Handles)
}

// Diff lists the first surface names that differ between two
// censuses - the failure message a dismount proof wants. Returns an
// empty slice when they are equal.
func (s CensusSnapshot) Diff(o CensusSnapshot) []string {
	var out []string
	if !maps.Equal(s.Slots, o.Slots) {
		seen := map[string]bool{}
		for k := range s.Slots {
			seen[k] = true
			if !o.Slots[k] {
				out = append(out, "slot "+k)
			}
		}
		for k := range o.Slots {
			if !seen[k] {
				out = append(out, "slot "+k)
			}
		}
	}
	out = slices.Clone(out)
	fail := func(name string, a, b []string) {
		if !slices.Equal(a, b) {
			out = append(out, name)
		}
	}
	fail("tools", s.Tools, o.Tools)
	fail("commands", s.Commands, o.Commands)
	fail("panels", s.Panels, o.Panels)
	fail("handles", s.Handles, o.Handles)
	if s.Channels != o.Channels {
		out = append(out, "channels")
	}
	if s.TUIProviders != o.TUIProviders {
		out = append(out, "tui_providers")
	}
	if s.PromptSectioners != o.PromptSectioners {
		out = append(out, "prompt_sectioners")
	}
	if s.PanelProviders != o.PanelProviders {
		out = append(out, "panel_providers")
	}
	if s.InjectedMessages != o.InjectedMessages {
		out = append(out, "injected_messages")
	}
	if s.PendingDelegations != o.PendingDelegations {
		out = append(out, "pending_delegations")
	}
	return out
}
