package provider

// card t_7dcd4280: the named provider instances - mount + resolver law.

import (
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// TestInstanceResolution: the resolver serves configured names (each
// its own model/host), refuses unknown ones (false, never a default
// fallback), and copies derive without losing the set.
func TestInstanceResolution(t *testing.T) {
	prov := (&Plugin{}).mountForTest(t, Config{
		Type:      "ollama",
		ModelName: "main",
		Instances: map[string]Config{
			"backup": {Type: "openai", ModelName: "gpt-4o", Host: "https://api.openai.example", APIKey: "sk"},
		},
	})
	inst, ok := prov.(agent.ProviderInstances)
	if !ok {
		t.Fatal("mounted provider must satisfy agent.ProviderInstances")
	}
	backup, ok := inst.Instance("backup")
	if !ok {
		t.Fatal("configured instance did not resolve")
	}
	if backup.ModelName() != "gpt-4o" {
		t.Fatalf("instance model = %q, want gpt-4o", backup.ModelName())
	}
	if backup, _ := inst.Instance("backup"); backup.ModelName() != "gpt-4o" {
		t.Fatal("second resolution must return the same named instance")
	}
	if got, ok := inst.Instance("gone"); ok || got != nil {
		t.Fatal("unknown instance must answer (nil, false) - the shell turns that into a loud failure")
	}
	// a with-model copy still resolves the same named set
	copied, ok := prov.WithModel("other").(agent.ProviderInstances)
	if !ok {
		t.Fatal("WithModel copy lost the instances resolver shape")
	}
	if inst2, ok := copied.Instance("backup"); !ok || inst2.ModelName() != "gpt-4o" {
		t.Fatal("derived copy does not resolve the named instance")
	}
	// no configured instances: unknown names stay (nil, false)
	bare := (&Plugin{}).mountForTest(t, Config{Type: "ollama", ModelName: "main"})
	if ir, ok := bare.(agent.ProviderInstances); ok {
		if got, ok2 := ir.Instance("anything"); ok2 || got != nil {
			t.Fatal("instance-free provider must not resolve anything")
		}
	}
}

// mountForTest mounts through agent.MountAllAPI (the one real mount
// path - a nil ExtensionAPI has no Offer to call) and returns the
// offered provider service.
func (p *Plugin) mountForTest(t *testing.T, cfg Config) agent.Provider {
	t.Helper()
	ctx := &core.Context{Configs: map[string]any{"provider": cfg}}
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	return ctx.Services().Resolve("provider").(agent.Provider)
}
