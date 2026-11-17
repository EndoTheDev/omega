package main

// The command chain's contract, at the real mount (card t_d923b50a).
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
//   - the canary law: a capsule mounted FIRST (the chain's innermost
//     link) answers a private name. Every other module is consulted
//     before it, so a canary answer proves no module in the chain
//     swallows a foreign name. A swallow hands back that module's own
//     text instead - the red names what it got.
//   - the per-name law: no module's banner may appear under another
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
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// commandCanary is a capsule that owns exactly one private command
// name. It is mounted first so every real module sits above it in the
// chain and gets first refusal (RegisterCommand wraps outward).
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

// mountCommandChain mounts the plugin set into a fresh Context (a temp
// home, so no capsule writes into the operator's files) and returns the
// live Context.
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

// TestNoModuleSwallowsAForeignCommand is the canary law: the innermost
// capsule's own name still answers after every real module was offered
// it first. A module that answers a foreign name with a nil error
// (cron's discarded name parameter was exactly this) swallows the
// chain and the assertion reports the text it returned instead.
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

	// A name nobody claims must end at the miss error, never at a
	// command result.
	res, err = ctx.CommandHandler(context.Background(), "/no-such-command", "")
	if err == nil {
		t.Fatalf("/no-such-command answered with %q, want the chain's miss error", res.Text)
	}
}

// TestCommandChainAnswersPerName is the per-name law over the real
// mount: each registered command answers its own name, and no module's
// banner shows up under another module's name.
func TestCommandChainAnswersPerName(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	ctx := mountCommandChain(t, fullPlugins(t))

	// Every banner a module prints for itself. A banner may appear only
	// under the name that owns it.
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
		// one run per name; args stay empty - this asserts OWNERSHIP of
		// the answer, not the argument handling. A module MAY refuse an
		// argument-less call (session commands need a session id,
		// /search needs a query); what it may not do is answer with
		// someone else's output - the banner law is the assertion.
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

	// The two capsule answers the webui report named are exact.
	res, err := ctx.CommandHandler(context.Background(), "/cron", "")
	if err != nil || !strings.HasPrefix(res.Text, "[cron]") {
		t.Fatalf("/cron = %q err %v, want the cron listing", res.Text, err)
	}
	res, err = ctx.CommandHandler(context.Background(), "/verdict", "")
	if err != nil || !strings.HasPrefix(res.Text, "[verdicts]") {
		t.Fatalf("/verdict = %q err %v, want the verdict listing", res.Text, err)
	}
}

// TestCLILoopCommandReachesTheOwningModule drives the terminal surface:
// the loop's own commandExtension runs the same chain, so /models must
// print the models surface (or the provider's own refusal) and never
// another module's text. /verdict answers offline, which pins the
// positive case on a lane with no provider.
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

	// /models needs a live provider: the listing on a configured lane,
	// the provider's own refusal otherwise. Either way it is never the
	// cron text (the reported symptom) and never a silent empty turn.
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
