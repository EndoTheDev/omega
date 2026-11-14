package observability

// Config configures the outbound logging half of the capsule (the
// config section stays `logging:` - the fold moved the code, not the
// operator's file).
type Config struct {
	Enabled bool   `yaml:"enabled"`
	File    string `yaml:"file"`
}

// Default returns the default logging configuration. File is EMPTY on
// purpose: a bare relative filename would make any early mount open a
// log file in the process CWD. cmd/omega homes the path (OMEGA_HOME/omega.log)
// before the plugin mounts - card t_0ccda501.
func Default() Config {
	return Config{Enabled: true, File: ""}
}
