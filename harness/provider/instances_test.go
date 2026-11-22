package provider

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
	copied, ok := prov.WithModel("other").(agent.ProviderInstances)
	if !ok {
		t.Fatal("WithModel copy lost the instances resolver shape")
	}
	if inst2, ok := copied.Instance("backup"); !ok || inst2.ModelName() != "" {
		t.Fatal("derived copy does not resolve the named instance")
	}
	// InstanceNames: the serving shell's grouped catalog reads the id list here
	names, ok := prov.(agent.ProviderInstanceNames)
	if !ok {
		t.Fatal("mounted provider must satisfy agent.ProviderInstanceNames")
	}
	if got := names.InstanceNames(); len(got) != 2 || got[0] != DefaultInstance || got[1] != "backup" {
		t.Fatalf("InstanceNames() = %v, want [default backup]", got)
	}
	// the primary IS an instance: "default" resolves to the receiver
	if self, ok := inst.Instance(DefaultInstance); !ok || self == nil {
		t.Fatal("the primary row must resolve by name like any other instance")
	}
	// no configured instances: unknown names stay (nil, false), and the primary is still.
	bare := (&Plugin{}).mountForTest(t, Config{Type: "ollama", ModelName: "main"})
	if ir, ok := bare.(agent.ProviderInstances); ok {
		if got, ok2 := ir.Instance("anything"); ok2 || got != nil {
			t.Fatal("instance-free provider must not resolve anything but the primary")
		}
	}
	bareNames, ok := bare.(agent.ProviderInstanceNames)
	if !ok {
		t.Fatal("instance-free provider must still satisfy ProviderInstanceNames")
	}
	if got := bareNames.InstanceNames(); len(got) != 1 || got[0] != DefaultInstance {
		t.Fatalf("instance-free InstanceNames() = %v, want [default]", got)
	}
}

// mountForTest mounts through agent.MountAllAPI.
func (p *Plugin) mountForTest(t *testing.T, cfg Config) agent.Provider {
	t.Helper()
	ctx := &core.Context{Configs: map[string]any{"provider": cfg}}
	if _, err := agent.MountAllAPI([]agent.Plugin{p}, ctx, nil, nil); err != nil {
		t.Fatalf("api mount: %v", err)
	}
	return ctx.Services().Resolve("provider").(agent.Provider)
}
