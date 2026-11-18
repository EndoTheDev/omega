package main

// The command chain's contract, at the real mount.
//
// agent.RegisterCommand builds ONE chain: every module's handler wraps
// the previous one, and a handler that returns an ERROR lets the call
// fall through to the next module (chain-on-miss). A handler that
// answers a name it does not own - with a nil error - swallows
// everything mounted below it: the webui reported every module command
// returning "[cron] no cron jobs configured" because cronHandler
// discarded its name parameter.
//
// Two laws pin it:
// the canary law: a capsule mounted FIRST (the chain's innermost
//     link) answers a private name. Every other module is consulted
//     before it, so a canary answer proves no module in the chain
//     swallows a foreign name. A swallow hands back that module's own
//     text instead - the red names what it got.
// the per-name law: no module's banner may appear under another
//     module's command name.
//
// The CLI loop (cmd/omega/loop.go) rides this very chain through
// pctx.CommandHandler, so the same test covers the terminal surface;
// TestCLILoopCommandReachesTheOwningModule drives the loop's own
// dispatch on top of it.

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// commandCanary is a capsule that owns exactly one private command name.
type commandCanary struct{}

func (commandCanary) Name() string       { return "canary" }
func (commandCanary) Provides() []string { return nil }
func (commandCanary) Requires() []string { return nil }

func (commandCanary) MountWithAPI(_ *core.Context, api *agent.ExtensionAPI) error {
	return api.RegisterCommand("/zz-canary", "the chain's innermost probe", nil,
		func(_ context.Context, name, _ string) (core.CommandResult, error) {
			if name != "/zz-canary" {
				return core.CommandResult{}, core.CommandMiss(name)
			}
			return core.CommandResult{Text: "[canary] answered"}, nil
		})
}

// mountCommandChain mounts the plugin set into a fresh Context.
func mountCommandChain(t *testing.T, plugins []agent.Plugin) *agent.Context {
	t.Helper()
	ctx := &agent.Context{CWD: cwd(), Configs: agent.RouteConfigs(plugins, DefaultConfig())}
	reg, err := agent.MountAllAPI(plugins, ctx, nil, nil)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	dismountCleanup(t, reg, ctx)
	return ctx
}

// TestNoModuleSwallowsAForeignCommand is the canary law. A module that answers a
// foreign name with a nil error swallows the chain and the assertion reports the text
// it returned instead.
func TestNoModuleSwallowsAForeignCommand(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	plugins := append([]agent.Plugin{commandCanary{}}, fullPlugins(t)...)
	ctx := mountCommandChain(t, plugins)

	res, err := ctx.CommandHandler(context.Background(), "/zz-canary", "")
	if err != nil {
		t.Fatalf("the chain never reached the innermost capsule: %v", err)
	}
	if res.Text != "[canary] answered" {
		t.Fatalf("a module swallowed /zz-canary: got text %q - a handler answered a foreign name with a nil error "+
			"(chain-on-miss violated)", res.Text)
	}

	// A name nobody claims must end at the miss error, never at a command result.
	res, err = ctx.CommandHandler(context.Background(), "/no-such-command", "")
	if err == nil {
		t.Fatalf("/no-such-command answered with %q, want the chain's miss error", res.Text)
	}
}

// TestCommandChainAnswersPerName is the per-name law over the real mount.
func TestCommandChainAnswersPerName(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	ctx := mountCommandChain(t, fullPlugins(t))

	owners := map[string]string{
		"[cron]":                   "/cron",
		"[verdicts]":               "/verdict",
		"notifications: ":          "/notifications",
		"* active in this session": "/boards",
	}
	registered := map[string]bool{}
	for _, c := range ctx.Commands {
		registered[c.Name] = true
	}
	for _, want := range []string{"/cron", "/verdict", "/notifications", "/models", "/provider", "/tree", "/tools", "/logs", "/export"} {
		if !registered[want] {
			t.Fatalf("the mount lost %s: table = %v", want, ctx.Commands)
		}
	}

	for _, c := range ctx.Commands {
		res, _ := ctx.CommandHandler(context.Background(), c.Name, "")
		for banner, owner := range owners {
			if banner == owner || c.Name == owner {
				continue
			}
			if strings.Contains(res.Text, banner) {
				t.Fatalf("%s answered with %q's banner %q: text %q", c.Name, owner, banner, res.Text)
			}
		}
	}

	res, err := ctx.CommandHandler(context.Background(), "/cron", "")
	if err != nil || !strings.HasPrefix(res.Text, "[cron]") {
		t.Fatalf("/cron = %q err %v, want the cron listing", res.Text, err)
	}
	res, err = ctx.CommandHandler(context.Background(), "/verdict", "")
	if err != nil || !strings.HasPrefix(res.Text, "[verdicts]") {
		t.Fatalf("/verdict = %q err %v, want the verdict listing", res.Text, err)
	}
}

// TestCLILoopCommandReachesTheOwningModule drives the terminal surface: the loop's own
// commandExtension runs the same chain, so /models must print the models surface (or
// the provider's own refusal) and never another module's text.
func TestCLILoopCommandReachesTheOwningModule(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	ctx := mountCommandChain(t, fullPlugins(t))

	buf := &bytes.Buffer{}
	lo := &cliLoop{
		ag:   agent.NewAgent(nil, nil, 8),
		pctx: ctx,
		out:  bufio.NewWriter(buf),
	}
	run := func(name string) (string, error) {
		buf.Reset()
		err := lo.commandExtension(name, "")
		lo.out.Flush()
		return buf.String(), err
	}

	out, err := run("/verdict")
	if err != nil {
		t.Fatalf("/verdict in the loop: %v", err)
	}
	if strings.Contains(out, "[cron]") || !strings.Contains(out, "[verdicts]") {
		t.Fatalf("/verdict printed %q, want the verdict listing", out)
	}

	// /models needs a live provider. Either way it is never the cron text (the reported
	// symptom) and never a silent empty turn.
	out, err = run("/models")
	if strings.Contains(out, "[cron]") {
		t.Fatalf("/models printed the cron text in the loop: %q", out)
	}
	if err != nil && strings.Contains(err.Error(), "[cron]") {
		t.Fatalf("/models failed with the cron text in the loop: %v", err)
	}

	out, err = run("/cron")
	if err != nil {
		t.Fatalf("/cron in the loop: %v", err)
	}
	if !strings.Contains(out, "[cron]") {
		t.Fatalf("/cron printed %q, want the cron listing", out)
	}
}

// TestChainSurfacesTheOwnersRefusal: the second half of the chain contract. A module
// that owns a name and refuses for want of a session - /export with no active one -
// must have its refusal heard.
func TestChainSurfacesTheOwnersRefusal(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	ctx := mountCommandChain(t, fullPlugins(t))

	_, err := ctx.CommandHandler(context.Background(), "/export", "")
	if err == nil {
		t.Fatal("/export answered with no session bound: want the owner's refusal")
	}
	if !strings.Contains(err.Error(), "no active session to export") {
		t.Fatalf("/export = %q, want the owner's own words", err)
	}
	if errors.Is(err, core.ErrCommandMiss) {
		t.Fatal("/export read as an unclaimed name: an honest refusal must never look like a miss")
	}
}
