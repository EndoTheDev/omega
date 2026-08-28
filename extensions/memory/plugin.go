package memory

import (
	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/gateway"
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
	cfg, ok := ctx.Config.(gateway.Config)
	if !ok {
		// No config — use defaults.
		cfg = gateway.DefaultConfig()
	}
	fm := NewFileMemory(cfg.Memory)
	ctx.Memory = fm
	ctx.ToolProviders = append(ctx.ToolProviders, &memoryToolProvider{mem: fm})
	return nil
}

// NewPlugin returns a memory Plugin instance.
func NewPlugin() *Plugin { return &Plugin{} }
