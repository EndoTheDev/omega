package http_channel

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
