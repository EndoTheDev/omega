package memory

import (
	"fmt"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// Plugin implements agent.Plugin for the memory seam. It opens the
// memory files from config and mounts them as the MemoryProvider,
// plus contributes the memory tool.
type Plugin struct{}

// Compile-time assertion that *Plugin implements agent.Plugin.
var _ agent.Plugin = (*Plugin)(nil)

func (p *Plugin) Name() string { return "memory" }

// Description implements agent.Describer.
func (p *Plugin) Description() string {
	return "durable memory: facts saved and recalled across sessions by the agent"
}

// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "memory" }

func (p *Plugin) Provides() []string { return []string{"memory", "tools", "prompt_sections"} }
func (p *Plugin) Requires() []string { return nil }

// Mount satisfies agent.Plugin (the interface keeps its classic shape
// until phase F). It is never dispatched: MountWithAPI exists, and
// APIPlugin wins (the strangler rule). If ever reached it fails loud -
// memory does not mount the old way, which is the proof of migration.
func (p *Plugin) Mount(ctx *core.Context) error {
	return fmt.Errorf("memory: classic mount retired - the capsule mounts through the ExtensionAPI (v0.12 phase C2, docs/migrating-a-module.md)")
}

// MountWithAPI implements agent.APIPlugin. This registration is the
// module's whole surface (arc 6 capsule, card t_da1fcc76): the seam,
// the memory.entry tool (the namespaced identity, formerly "memory")
// and the prompt section all register here, and the module ships
// nothing else - no command, no REST route, no canvas panel (nothing
// rendered memory before the arc). Removal proof: TestMemoryRemovalProof
// (cmd/omega/removal_test.go).
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	cfg := Default()
	if c, ok := ctx.Configs["memory"].(Config); ok {
		cfg = c
	}
	fm := NewFileMemory(cfg)
	if err := api.RegisterMemory(fm); err != nil {
		return err
	}
	for name, tool := range (&memoryToolProvider{mem: fm}).Tools() {
		if err := api.RegisterTool(name, tool); err != nil {
			return err
		}
	}
	return api.RegisterPromptSectioner(fm)
}

// NewPlugin creates a memory plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }

// Toggleable marks this capsule as a module capsule (v0.12 E1, the
// tier law): the omega.json disable list may name it.
func (p *Plugin) Toggleable() bool { return true }
