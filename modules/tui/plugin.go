package tui

import (
	"context"
	"fmt"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	tea "github.com/charmbracelet/bubbletea"
)

// Plugin implements agent.Plugin for the TUI frontend.
type Plugin struct{}

var _ agent.Plugin = (*Plugin)(nil)

func (Plugin) Name() string       { return "tui" }

// Description implements agent.Describer.
func (Plugin) Description() string {
	return "terminal UI: the Bubble Tea frontend with slash commands"
}
// ConfigKey names the config section this plugin reads from
// ctx.Configs (see agent.Describer). Empty = reads no section.
func (p *Plugin) ConfigKey() string { return "" }

func (Plugin) Provides() []string { return []string{"frontend"} }
func (Plugin) Requires() []string { return nil }

func (p *Plugin) Mount(ctx *core.Context) error {
	ctx.Frontend = &Frontend{}
	return nil
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
