package tui

import (
	"context"
	"fmt"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	tea "github.com/charmbracelet/bubbletea"
)

// Plugin implements agent.Plugin for the TUI frontend. It mounts
// through the ExtensionAPI exclusively (v0.12 phase F).
type Plugin struct{}

var _ agent.Plugin = (*Plugin)(nil)

func (Plugin) Name() string { return "tui" }

// Description implements agent.Describer.
func (Plugin) Description() string {
	return "terminal UI: the Bubble Tea frontend with slash commands"
}

// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "" }

func (Plugin) Provides() []string { return []string{"frontend"} }
func (Plugin) Requires() []string { return nil }

// Mount is the retired classic body (v0.12 phase F): the frontend
// seam registers through the ExtensionAPI now. Fail loud if reached.
func (p *Plugin) Mount(ctx *core.Context) error {
	return fmt.Errorf("tui: classic mount retired - the capsule mounts through the ExtensionAPI (v0.12 phase F)")
}

// MountWithAPI implements agent.APIPlugin: the frontend seam, one
// registration.
func (p *Plugin) MountWithAPI(ctx *core.Context, api *agent.ExtensionAPI) error {
	return api.RegisterFrontend(&Frontend{})
}

// Frontend implements core.Frontend by running the Bubble Tea program.
type Frontend struct{}

func (f *Frontend) Run(ctx context.Context, pctx *core.Context, opts core.FrontendOptions) error {
	m := NewModel(pctx, opts)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

func NewPlugin() *Plugin { return &Plugin{} }