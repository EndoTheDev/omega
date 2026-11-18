package api

import (
	agentapi "github.com/EndoTheDev/omega/agent/api"
)

type Config struct {
	Port int `yaml:"port"`
	Bind []string `yaml:"bind"`
	RunTimeout int `yaml:"run_timeout"`
	TitleModel string `yaml:"title_model"`
	// PublicHosts (config key server.public_hosts) names the public identities this serve
	// answers to - a domain fronted by a reverse proxy or tunnel (the tunnel forwards the
	// browser's Host, which is never the bind address, so the Host guard would refuse it).
	PublicHosts []string `yaml:"public_hosts"`
	DefaultWorkspace string `yaml:"default_workspace"`
	WebTools agentapi.WebToolsConfig
}

const defaultRunTimeout = 1800

func (c Config) RunTimeoutSeconds() int {
	if c.RunTimeout <= 0 {
		return defaultRunTimeout
	}
	return c.RunTimeout
}

func Default() Config {
	return Config{Port: 8099, Bind: []string{"127.0.0.1"}, RunTimeout: defaultRunTimeout}
}
