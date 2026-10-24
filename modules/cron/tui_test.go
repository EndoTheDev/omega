package cron

// Tests for the module-owned /cron TUI command (card t_88bb7dcf;
// behavior ported unchanged from the old modules/tui browser.go):
// table rendering from a fixture and the command contract over a
// recording console.

import (
	"context"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// fakeCronLister satisfies core.CronProvider + core.CronLister for
// the /cron test, the same shape the webui handler test uses.
type fakeCronLister struct{ jobs []core.CronJobInfo }

func (f fakeCronLister) Start(context.Context, core.CronDeps) error { return nil }
func (f fakeCronLister) Stop() error                                { return nil }
func (f fakeCronLister) Jobs() []core.CronJobInfo                   { return f.jobs }

// noListerCron is a CronProvider that is not a CronLister - the shape
// the inspector must treat as an empty listing.
type noListerCron struct{}

func (noListerCron) Start(context.Context, core.CronDeps) error { return nil }
func (noListerCron) Stop() error                                { return nil }

// consoleSpy is an core.TUIConsole that records output and renders
// styles unchanged.
type consoleSpy struct {
	lines   []string
	errs    []string
	ext     *core.Context
	cleared int
}

func (c *consoleSpy) Labeled(label, body string) { c.lines = append(c.lines, label+"|"+body) }
func (c *consoleSpy) Error(msg string)           { c.errs = append(c.errs, msg) }
func (c *consoleSpy) ClearError()                { c.cleared++ }
func (c *consoleSpy) Style(string) core.TUIStyle {
	return func(s string) string { return s }
}
func (c *consoleSpy) CWD() string        { return "" }
func (c *consoleSpy) Ext() *core.Context { return c.ext }
func (c *consoleSpy) transcript() string { return strings.Join(c.lines, "\n") }

// TestRenderCronTable is pure: fixtures in, table out.
func TestRenderCronTable(t *testing.T) {
	jobs := []core.CronJobInfo{
		{Name: "zeta", Schedule: "0 3 * * *", Enabled: true, Next: "tomorrow"},
		{Name: "beta", Schedule: "*/5 * * * *", Enabled: true, Next: "soon"},
		{Name: "off", Schedule: "garbage", Enabled: false},
	}
	out := renderCronTable(jobs, identity, identity)
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "SCHEDULE") {
		t.Fatalf("table header missing: %q", out)
	}
	if strings.Index(out, "beta") > strings.Index(out, "zeta") {
		t.Fatalf("rows not sorted: %q", out)
	}
	// the disabled job's empty Next shows as the dash placeholder
	offRow := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "off") {
			offRow = line
		}
	}
	if offRow == "" || strings.Contains(offRow, "tomorrow") {
		t.Fatalf("disabled row wrong: %q", offRow)
	}
	if !strings.Contains(offRow, "no") || strings.HasSuffix(strings.TrimSpace(offRow), "yes") {
		t.Fatalf("enabled column wrong on %q", offRow)
	}
}

// identity renders its argument unchanged - the fake theme in tests.
func identity(s string) string { return s }

// TestCronCommand renders the fixture through the provider contract:
// rows sorted by name, the disabled/empty variants handled, the webui's
// empty-list semantics (unconfigured scheduler, not an error).
func TestCronCommand(t *testing.T) {
	jobs := []core.CronJobInfo{
		{Name: "nightly", Schedule: "0 3 * * *", Enabled: true, Next: "2026-10-22T03:00:00+07:00"},
		{Name: "alpha", Schedule: "5 * * * *", Enabled: false},
	}
	p := &tui{p: &Plugin{runner: fakeCronLister{jobs: jobs}}}
	c := &consoleSpy{}
	if res := p.HandleTUICommand("/cron", "", c); res.Overlay != nil || res.Tick != nil {
		t.Fatal("/cron returned interactive instructions")
	}
	out := c.transcript()
	if !strings.Contains(out, "[cron]") {
		t.Fatalf("output missing [cron] header: %q", out)
	}
	// sorted by name: alpha row before nightly row
	alpha, nightly := strings.Index(out, "alpha"), strings.Index(out, "nightly")
	if alpha < 0 || nightly < 0 || alpha > nightly {
		t.Fatalf("rows not sorted by name: %q", out)
	}
	if !strings.Contains(out, "0 3 * * *") || !strings.Contains(out, "2026-10-22T03:00:00+07:00") {
		t.Errorf("schedule/next missing: %q", out)
	}

	// empty set reads as a configured-but-empty scheduler
	c = &consoleSpy{}
	_ = (&tui{p: &Plugin{runner: fakeCronLister{}}}).HandleTUICommand("/cron", "", c)
	if !strings.Contains(c.transcript(), "no cron jobs configured") {
		t.Fatalf("empty set not handled: %q", c.transcript())
	}

	// a CronProvider without the listing reads as empty, like the webui
	c = &consoleSpy{}
	_ = (&tui{p: &Plugin{runner: noListerCron{}}}).HandleTUICommand("/cron", "", c)
	if !strings.Contains(c.transcript(), "no cron jobs configured") {
		t.Fatalf("no-lister provider not handled: %q", c.transcript())
	}

	// an unmounted scheduler (plugin mounted without the runner) is
	// the same empty state
	c = &consoleSpy{}
	_ = (&tui{p: &Plugin{}}).HandleTUICommand("/cron", "", c)
	if !strings.Contains(c.transcript(), "no cron jobs configured") {
		t.Fatalf("unmounted scheduler not handled: %q", c.transcript())
	}
}

// TestCronTUIRegistration pins the law: the plugin's Mount registers
// the provider, so a cron plugin absent from a mount never surfaces
// /cron in a TUI command table.
func TestCronTUIRegistration(t *testing.T) {
	ctx := &core.Context{}
	if err := (NewPluginWithConfig(&Config{}, t.TempDir())).Mount(ctx); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range ctx.TUIProviders {
		for _, cmd := range p.TUICommands() {
			got = append(got, cmd.Name)
		}
	}
	if len(got) != 1 || got[0] != "/cron" {
		t.Fatalf("mounted TUI commands = %v, want [/cron]", got)
	}
}
