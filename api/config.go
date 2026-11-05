package api

import (
	agentapi "github.com/EndoTheDev/omega/agent/api"
)

// Config configures the HTTP channel extension.
type Config struct {
	Port int `yaml:"port"`
	// Bind: addresses to listen on, one channel per entry.
	// Default: loopback only - the API is unauthenticated and exposes
	// shell.run, so anything beyond loopback is an explicit opt-in
	// (e.g. the Tailscale IP so Traefik can route to omega).
	Bind []string `yaml:"bind"`
	// RunTimeout caps each detached /chat run's wall-clock time, in
	// seconds. This is the session-brick backstop: an LLM call carries
	// the HTTP timeout, but a hung tool call runs forever without a
	// wall-clock cap, holding the session's run slot until restart.
	// 0 or negative falls back to Default().RunTimeout at mount.
	RunTimeout int `yaml:"run_timeout"`
	// TitleModel is the title_model knob: session-title calls
	// (provisional + final) route through it when set. Empty = the
	// main model (today's behavior).
	TitleModel string `yaml:"title_model"`
	// PublicHosts (config key server.public_hosts) names the public
	// identities this serve answers to - a domain fronted by a reverse
	// proxy or tunnel (the tunnel forwards the browser's Host, which
	// is never the bind address, so the Host guard would refuse it).
	// Operator-declared opt-in, exact match, no wildcards: the guard
	// keeps refusing every name not on this list.
	PublicHosts []string `yaml:"public_hosts"`
	// DefaultWorkspace is the server.default_workspace knob (card
	// t_ccf74d88): the tools' fallback working directory. A chat
	// session without a pinned workspace roots its tool runs here
	// instead of the process working directory - a defined default,
	// not an implicit cwd. Empty = unset, the cwd fallback stands.
	DefaultWorkspace string `yaml:"default_workspace"`
	// WebTools carries the web.search/web.fetch settings (v0.13 shell
	// law 1: the former modules/web capsule dissolved into the
	// channel). The host's ConfigSection derives both fields; an
	// empty BaseURL falls back to the default upstream mirror.
	WebTools agentapi.WebToolsConfig
}

// defaultRunTimeout is the detached-run wall-clock cap: 30 minutes is
// generous for a single turn and still un-bricks a hung session.
const defaultRunTimeout = 1800

// RunTimeoutSeconds returns the effective run cap in seconds, clamping
// unconfigured values to the default.
func (c Config) RunTimeoutSeconds() int {
	if c.RunTimeout <= 0 {
		return defaultRunTimeout
	}
	return c.RunTimeout
}

// Default returns the default HTTP channel configuration.
func Default() Config {
	return Config{Port: 8099, Bind: []string{"127.0.0.1"}, RunTimeout: defaultRunTimeout}
}
