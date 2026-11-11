package main

// agentruns.go: the host's per-agent run factory (card t_a7fab745,
// Endo's ruling A 2026-11-10). The chat floor serves a session the
// agents roster lists; that row lives in the owning agent home's own
// store, and the home's config layer carries its provider, model, system
// prompt, skills and max_turns. Answering on the daemon's config would
// be the WRONG AGENT, so the api server asks this factory for the agent
// that owns the session.
//
// The build is the same one `omega run` performs for an OMEGA_AGENT
// child, minus the process boundary: re-read config.yaml, overlay
// agents/<name>/config/config.yaml (ApplyAgentLayer), mount the plugin
// set for that config. Mounting is startup work, never per turn - the
// result is cached per agent name for the process lifetime, and its
// handles (store, logger) stay with it.

import (
	"context"
	"fmt"
	"sync"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// agentHomeRun is one agent home's built run config plus the mount it
// owns. The mount lives as long as the daemon serves that home.
type agentHomeRun struct {
	agent  *agent.Agent
	logger agent.LoggerProvider
	reg    *agent.Registry
	ctx    *core.Context
}

// agentRuns is the per-name build cache. The daemon never closes it -
// the handles live as long as the process serves those homes; the
// factory's test does, so a temp home can be removed on windows.
type agentRuns struct {
	mu            sync.Mutex
	built         map[string]*agentHomeRun
	configPath    string
	appendPrompts []string
	trust         trustFlags
}

// agentRunFactory returns the AgentRunFactory closure the api server
// holds: one build per agent home, cached.
func agentRunFactory(configPath string, appendPrompts []string, trust trustFlags) func(context.Context, string) (*agent.Agent, error) {
	runs := &agentRuns{configPath: configPath, appendPrompts: appendPrompts, trust: trust}
	return runs.agent
}

// agent builds (once) and returns the run agent for one agent home. A
// config the home's layer cannot parse is a hard error - running on half
// of one agent's config is worse than a loud stop.
func (c *agentRuns) agent(_ context.Context, name string) (*agent.Agent, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if run, ok := c.built[name]; ok {
		return run.agent, nil
	}
	cfg, err := LoadConfig(resolveConfigPath(c.configPath))
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	if err := resolveHomePaths(&cfg); err != nil {
		return nil, err
	}
	if err := ApplyAgentLayer(&cfg, omegaHome(), name); err != nil {
		return nil, fmt.Errorf("agent layer %s: %w", name, err)
	}
	ag, _, logger, pctx, reg, _, _, _, _, _, err := newAgent(cfg, c.configPath, c.appendPrompts, c.trust)
	if err != nil {
		return nil, fmt.Errorf("mount agent %s: %w", name, err)
	}
	if c.built == nil {
		c.built = map[string]*agentHomeRun{}
	}
	c.built[name] = &agentHomeRun{agent: ag, logger: logger, reg: reg, ctx: pctx}
	return ag, nil
}

// close dismounts every home this cache built - the store handles, the
// module registrations and the goroutine fence go back to the unmounted
// census. Only a caller that owns a process-lifetime boundary needs it
// (the daemon's handles live as long as it serves the homes).
func (c *agentRuns) close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var first error
	for name, run := range c.built {
		if err := agent.DismountAll(run.reg, run.ctx); err != nil && first == nil {
			first = fmt.Errorf("dismount agent %s: %w", name, err)
		}
		// The log handle is host-owned too (the module teardown never
		// closes data): the capsule's logger keeps the file open.
		if run.logger != nil {
			if err := run.logger.Close(); err != nil && first == nil {
				first = fmt.Errorf("close agent %s log: %w", name, err)
			}
		}
		delete(c.built, name)
	}
	return first
}
