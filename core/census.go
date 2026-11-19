// The teardown census (v0.12 phase E1, implementation sketch 2). stdlib-only by the
// core law: this is vocabulary, never behavior.
package core

import (
	"maps"
	"slices"
)

// CensusSnapshot is one census. Every field is derived from the
// Context alone - a fresh Context and a fully dismounted Context must
// produce equal snapshots (zero surface, zero run-group entries).
type CensusSnapshot struct {
	Slots map[string]bool

	Tools []string

	Commands []string

	Channels         int
	PromptSectioners int
	PanelProviders   int

	// Panels lists every declared panel id plus its action ids as
	// "panel" and "panel/action" entries, sorted - duplicates reveal
	// a double-registered additive surface.
	Panels []string

	// Routes are the registered module REST route patterns, in registration order.
	// Sort-stable like Panels.
	Routes []string

	Handles []string
}

// Census snapshots every registration surface of ctx.
func Census(ctx *Context) CensusSnapshot {
	s := CensusSnapshot{Slots: map[string]bool{}}
	for _, rt := range ctx.ModuleRoutes {
		s.Routes = append(s.Routes, rt.Pattern)
	}
	for _, slot := range []string{
		"provider",
		"store",
		"loop",
		"logger",
		"compactor",
		"prompt",
		"nudger",
		"approvals",
		"skills",
		"memory",
		"trust",
		"cron",
		"notifications",
		"verdicts",
		"chat-runs",
	} {
		if ctx.Services().Has(slot) {
			s.Slots[slot] = true
		}
	}
	if ctx.Captioner != nil {
		if sw, ok := ctx.Captioner.(interface{ CaptioningEnabled() bool }); !ok || sw.CaptioningEnabled() {
			s.Slots["captioner"] = true
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

// Equal reports whether two censuses hold the same surface.
func (s CensusSnapshot) Equal(o CensusSnapshot) bool {
	return maps.Equal(s.Slots, o.Slots) &&
		slices.Equal(s.Tools, o.Tools) &&
		slices.Equal(s.Commands, o.Commands) &&
		slices.Equal(s.Routes, o.Routes) &&
		s.Channels == o.Channels &&
		s.PromptSectioners == o.PromptSectioners &&
		s.PanelProviders == o.PanelProviders &&
		slices.Equal(s.Panels, o.Panels) &&
		slices.Equal(s.Handles, o.Handles)
}

// Diff lists the first surface names that differ between two censuses.
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
	fail("routes", s.Routes, o.Routes)
	fail("handles", s.Handles, o.Handles)
	if s.Channels != o.Channels {
		out = append(out, "channels")
	}
	if s.PromptSectioners != o.PromptSectioners {
		out = append(out, "prompt_sectioners")
	}
	if s.PanelProviders != o.PanelProviders {
		out = append(out, "panel_providers")
	}
	return out
}
