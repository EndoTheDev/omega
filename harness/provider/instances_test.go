package provider

// card t_7dcd4280 / t_1914a8c9: the named provider instances - mount +
// resolver law. An instance is a NAMED ENDPOINT only (name, type,
// host, api_key; no model column - models come from the instance's
// catalog through the pickers).

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
			"backup": {Type: "openai", Host: "https://api.openai.example", APIKey: "k"},
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
	if backup.ModelName() != "" {
		t.Fatalf("instance model = %q, want empty (named endpoints carry no model)", backup.ModelName())
	}
	if backup, _ := inst.Instance("backup"); backup.ModelName() != "" {
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
	if inst2, ok := copied.Instance("backup"); !ok || inst2.ModelName() != "" {
		t.Fatal("derived copy does not resolve the named instance")
	}
	// InstanceNames (card t_1914a8c9): the serving shell's grouped
	// catalog reads the id list here
	names, ok := prov.(agent.ProviderInstanceNames)
	if !ok {
		t.Fatal("mounted provider must satisfy agent.ProviderInstanceNames")
	}
	if got := names.InstanceNames(); len(got) != 1 || got[0] != "backup" {
		t.Fatalf("InstanceNames() = %v, want [backup]", got)
	}
	// no configured instances: unknown names stay (nil, false), the
	// names list answers nil
	bare := (&Plugin{}).mountForTest(t, Config{Type: "ollama", ModelName: "main"})
	if ir, ok := bare.(agent.ProviderInstances); ok {
		if got, ok2 := ir.Instance("anything"); ok2 || got != nil {
			t.Fatal("instance-free provider must not resolve anything")
		}
	}
	bareNames, ok := bare.(agent.ProviderInstanceNames)
	if !ok {
		t.Fatal("instance-free provider must still satisfy ProviderInstanceNames")
	}
	if got := bareNames.InstanceNames(); got != nil {
		t.Fatalf("instance-free InstanceNames() = %v, want nil", got)
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
