package memory

import (
	"github.com/EndoTheDev/omega/agent"
)

// Plugin implements agent.Plugin for the memory seam. It opens the
// memory files from config and mounts them as the MemoryProvider,
// plus contributes the memory tool.
type Plugin struct{}

// Compile-time assertion that *Plugin implements agent.Plugin.
var _ agent.Plugin = (*Plugin)(nil)

func (p *Plugin) Name() string       { return "memory" }
func (p *Plugin) Provides() []string { return []string{"memory", "tools"} }
func (p *Plugin) Requires() []string { return nil }

// Mount creates the FileMemory from config and populates ctx.Memory +
// a memory tool provider.
func (p *Plugin) Mount(ctx *agent.Context) error {
	cfg := Default()
	if c, ok := ctx.Configs["memory"].(Config); ok {
		cfg = c
	}
	fm := NewFileMemory(cfg)
	ctx.Memory = fm
	ctx.ToolProviders = append(ctx.ToolProviders, &memoryToolProvider{mem: fm})
	return nil
}

// NewPlugin creates a memory plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }
