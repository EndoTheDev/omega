package cron

// Module-owned command surface.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// cronHandler lists the cron jobs. chain-on-miss is the command seam's contract
// (agent.RegisterCommand): the wrapper falls through to the next module when a handler
// returns an error, so a handler MUST reject a foreign name. Answering every name with
// a nil error swallows the whole chain and every later command.
func cronHandler(p *Plugin) func(context.Context, string, string) (core.CommandResult, error) {
	return func(_ context.Context, name, _ string) (core.CommandResult, error) {
		if name != "/cron" {
			return core.CommandResult{}, core.CommandMiss(name)
		}
		jobs := []JobInfo{}
		if p.runner != nil {
			if got := p.runner.Jobs(); got != nil {
				jobs = got
			}
		}
		if len(jobs) == 0 {
			return core.CommandResult{Text: "[cron] no cron jobs configured"}, nil
		}
		return core.CommandResult{Text: "[cron]" + renderCronTable(jobs)}, nil
	}
}

// renderCronTable renders the cron job list the way the webui Cron tab orders and
// renders it: sorted by name, disabled jobs dimmed, empty Next shown as "-", kind
// carried next to the schedule (interval rows speak minutes, cron rows the 5-field
// expression). Pure on jobs (the renderer's copy is sorted).
func renderCronTable(jobs []JobInfo) string {
	out := append([]JobInfo(nil), jobs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	nameW, schedW, agentW, lastW := len("NAME"), len("SCHEDULE"), len("AGENT"), len("LAST")
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
		if w := len(lastText(j)); w > lastW {
			lastW = w
		}
	}
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("  %-*s  %-*s  %-*s  %-*s  %-3s  %s\n", nameW, "NAME", schedW, "SCHEDULE", agentW, "AGENT", lastW, "LAST", "ON", "NEXT"))
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
		sb.WriteString(fmt.Sprintf("  %-*s  %-*s  %-*s  %-*s  %-3s  %s\n", nameW, j.Name, schedW, cadenceText(j), agentW, agent, lastW, lastText(j), state, next))
	}
	return sb.String()
}

// lastText renders the row's last-fire voice: the status + the local short time of the
// completion (the same "01/02 15:04" compactness the webui row rides), "-" when the job
// never fired.
func lastText(j JobInfo) string {
	if j.Status == "" || j.Last == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, j.Last)
	if err != nil {
		return j.Status
	}
	return fmt.Sprintf("%s %s", j.Status, t.Format("01-02 15:04"))
}

// cadenceText renders the row's firing cadence.
func cadenceText(j JobInfo) string {
	if j.Kind == "interval" {
		return fmt.Sprintf("%dm", j.Minutes)
	}
	return j.Schedule
}
