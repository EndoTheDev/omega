package cron

// Module-owned TUI surface (card t_88bb7dcf): /cron belongs to the
// cron module the same law that owns modules/cron/ui/CronJobs.vue on
// the webui. The screen reads the same seam the webui Cron tab reads
// (core.CronLister over the runner mounted into Context.Cron); no
// storage is reimplemented and nothing writes.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// tui implements agent.TUIProvider for the /cron command.
type tui struct{ p *Plugin }

var _ core.TUIProvider = (*tui)(nil)

// TUICommands declares the /cron command.
func (t *tui) TUICommands() []core.TUICommand {
	return []core.TUICommand{
		{Name: "/cron", Help: "list cron jobs (read-only)"},
	}
}

// HandleTUICommand lists the cron jobs. A missing scheduler or a
// provider without the listing reads as an empty job set - an
// unconfigured scheduler is a normal state, not a missing feature.
func (t *tui) HandleTUICommand(name, args string, c core.TUIConsole) core.TUIResult {
	jobs := []core.CronJobInfo{}
	if t.p.runner != nil {
		if lister, ok := t.p.runner.(core.CronLister); ok {
			if got := lister.Jobs(); got != nil {
				jobs = got
			}
		}
	}
	if len(jobs) == 0 {
		c.Labeled("", c.Style("info")("[cron] no cron jobs configured"))
		return core.TUIResult{}
	}
	c.Labeled("[cron]", renderCronTable(jobs, c.Style("info"), c.Style("dim")))
	return core.TUIResult{}
}

// renderCronTable renders the cron job list the way the webui Cron tab
// orders and renders it: sorted by name, disabled jobs dimmed, empty
// Next shown as "-". Pure on jobs (the renderer's copy is sorted).
func renderCronTable(jobs []core.CronJobInfo, info, dim core.TUIStyle) string {
	out := append([]core.CronJobInfo(nil), jobs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	nameW, schedW := len("NAME"), len("SCHEDULE")
	for _, j := range out {
		if len(j.Name) > nameW {
			nameW = len(j.Name)
		}
		if len(j.Schedule) > schedW {
			schedW = len(j.Schedule)
		}
	}
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(info(fmt.Sprintf("  %-*s  %-*s  %-3s  %s", nameW, "NAME", schedW, "SCHEDULE", "ON", "NEXT")))
	sb.WriteString("\n")
	for _, j := range out {
		next := j.Next
		if next == "" {
			next = "-"
		}
		state := "yes"
		if !j.Enabled {
			state = "no"
		}
		row := fmt.Sprintf("  %-*s  %-*s  %-3s  %s", nameW, j.Name, schedW, j.Schedule, state, next)
		if !j.Enabled {
			row = dim(row)
		}
		sb.WriteString(row)
		sb.WriteString("\n")
	}
	return sb.String()
}