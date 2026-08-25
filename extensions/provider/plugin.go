package provider

import (
	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/gateway"
)

// Plugin is the in-process adapter for the provider extension.
// It reads provider configuration from the Context.Config (a
// gateway.Config) and mounts a Provider into the provider seam.
type Plugin struct{}

// Name returns the plugin name shown in the /extensions listing.
func (Plugin) Name() string { return "core-provider" }

// Provides lists the seam names this plugin mounts.
func (Plugin) Provides() []string { return []string{"provider"} }

// Requires lists seam names that must be mounted before this plugin.
func (Plugin) Requires() []string { return nil }

// Mount reads the provider config from Context.Config and sets
// ctx.Provider. If Config is nil or not a gateway.Config, the
// provider is created with zero values (type defaults to ollama).
func (Plugin) Mount(ctx *agent.Context) error {
	var typ, model, host, key string
	if cfg, ok := ctx.Config.(gateway.Config); ok {
		typ = cfg.Provider.Type
		model = cfg.Provider.ModelName
		host = cfg.Provider.Host
		key = cfg.Provider.APIKey
	}
	ctx.Provider = NewProvider(typ, model, host, key)
	return nil
}