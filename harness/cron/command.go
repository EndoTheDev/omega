package cron

// Module-owned command surface (card t_0744eb8e): /cron belongs to the
// cron module - the same law that owns harness/cron/ui/CronJobs.vue on
// the webui. The command reads the same seam the webui Cron tab reads
// (core.CronLister over the runner mounted into Context.Cron); no
// storage is reimplemented and nothing writes.

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// cronHandler lists the cron jobs. A missing scheduler or a provider
// without the listing reads as an empty job set - an unconfigured
// scheduler is a normal state, not a missing feature.
func cronHandler(p *Plugin) func(context.Context, string, string) (core.CommandResult, error) {
	return func(_ context.Context, _ string, _ string) (core.CommandResult, error) {
		jobs := []core.CronJobInfo{}
		if p.runner != nil {
			if lister, ok := p.runner.(core.CronLister); ok {
				if got := lister.Jobs(); got != nil {
					jobs = got
				}
			}
		}
		if len(jobs) == 0 {
			return core.CommandResult{Text: "[cron] no cron jobs configured"}, nil
		}
		return core.CommandResult{Text: "[cron]" + renderCronTable(jobs)}, nil
	}
}

// renderCronTable renders the cron job list the way the webui Cron tab
// orders and renders it: sorted by name, disabled jobs dimmed, empty
// Next shown as "-", kind carried next to the schedule (interval rows
// speak minutes, cron rows the 5-field expression). Pure on jobs (the
// renderer's copy is sorted).
func renderCronTable(jobs []core.CronJobInfo) string {
	out := append([]core.CronJobInfo(nil), jobs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	nameW, schedW, agentW := len("NAME"), len("SCHEDULE"), len("AGENT")
	for _, j := range out {
		if len(j.Name) > nameW {
			nameW = len(j.Name)
		}
		if w := len(cadenceText(j)); w > schedW {
			schedW = w
		}
		if len(j.Agent) > agentW {
			agentW = len(j.Agent)
		}
	}
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("  %-*s  %-*s  %-*s  %-3s  %s\n", nameW, "NAME", schedW, "SCHEDULE", agentW, "AGENT", "ON", "NEXT"))
	for _, j := range out {
		next := j.Next
		if next == "" {
			next = "-"
		}
		agent := j.Agent
		if agent == "" {
			agent = "-"
		}
		state := "yes"
		if !j.Enabled {
			state = "no"
		}
		sb.WriteString(fmt.Sprintf("  %-*s  %-*s  %-*s  %-3s  %s\n", nameW, j.Name, schedW, cadenceText(j), agentW, agent, state, next))
	}
	return sb.String()
}

// cadenceText renders the row's firing cadence: the 5-field expression
// for kind cron, the minute interval for kind interval.
func cadenceText(j core.CronJobInfo) string {
	if j.Kind == "interval" {
		return fmt.Sprintf("%dm", j.Minutes)
	}
	return j.Schedule
}
