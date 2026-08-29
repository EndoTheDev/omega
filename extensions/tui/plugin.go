package tui

import (
	"context"
	"fmt"

	"github.com/EndoTheDev/omega/agent"
	tea "github.com/charmbracelet/bubbletea"
)

// Plugin implements agent.Plugin for the TUI frontend.
type Plugin struct{}

var _ agent.Plugin = (*Plugin)(nil)

func (Plugin) Name() string       { return "tui" }
func (Plugin) Provides() []string { return []string{"frontend"} }
func (Plugin) Requires() []string { return nil }

func (p *Plugin) Mount(ctx *agent.Context) error {
	ctx.Frontend = &Frontend{}
	return nil
}

// Frontend implements agent.Frontend by running the Bubble Tea program.
type Frontend struct{}

func (f *Frontend) Run(ctx context.Context, pctx *agent.Context, opts agent.FrontendOptions) error {
	m := NewModel(pctx, opts)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

func NewPlugin() *Plugin { return &Plugin{} }
