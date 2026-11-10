package main

// cmd/omega/settings_scope_test.go: the agent-scope read payload
// (settings tier 3a, card t_22a5f70a). The pins:
//
//   - the scope list is the agent homes under <home>/agents/ (the same
//     roster scan the agents tab reads), main is never a scope, and an
//     empty agents/ answers an empty list (R8);
//   - a knob the layer names is overridden and carries the layer's
//     value, a knob it does not name inherits MAIN - asserted against
//     the payload's OWN main knob, so the two surfaces cannot drift;
//   - inherited max_turns comes from the serving agent's runtime knob,
//     not the yaml file.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// scopeHome lays out a settled home: <home>/config/config.yaml plus one
// <home>/agents/<name>/config/config.yaml per entry.
func scopeHome(t *testing.T, mainConfig string, agents map[string]string) string {
	t.Helper()
	home := t.TempDir()
	writeScopeFile(t, filepath.Join(home, "config", "config.yaml"), mainConfig)
	for name, body := range agents {
		writeScopeFile(t, filepath.Join(home, "agents", name, "config", "config.yaml"), body)
	}
	return home
}

func writeScopeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// scopeByName returns one scope's payload (fatal on a miss - a missing
// scope is the failure under test, never a silent zero value).
func scopeByName(t *testing.T, schema *core.SettingsSchema, name string) core.SettingsScope {
	t.Helper()
	for _, s := range schema.Scopes {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("scope %q missing from the payload (have %v)", name, scopeNames(schema))
	return core.SettingsScope{}
}

func scopeNames(schema *core.SettingsSchema) []string {
	names := make([]string, 0, len(schema.Scopes))
	for _, s := range schema.Scopes {
		names = append(names, s.Name)
	}
	return names
}

// scopeKnob locates one knob inside a scope by its dotted key.
func scopeKnob(t *testing.T, scope core.SettingsScope, key string) core.SettingsScopeKnob {
	t.Helper()
	for _, g := range scope.Groups {
		for _, k := range g.Knobs {
			if k.Key == key {
				return k
			}
		}
	}
	t.Fatalf("scope %q carries no knob %q", scope.Name, key)
	return core.SettingsScopeKnob{}
}

// scopeGroup locates one group inside a scope.
func scopeGroup(t *testing.T, scope core.SettingsScope, key string) core.SettingsScopeGroup {
	t.Helper()
	for _, g := range scope.Groups {
		if g.Key == key {
			return g
		}
	}
	t.Fatalf("scope %q carries no group %q", scope.Name, key)
	return core.SettingsScopeGroup{}
}

// mainKnob reads a knob off the MAIN payload - the inherited base every
// scope knob is compared against (the pane's "differs from main").
func mainKnob(t *testing.T, schema *core.SettingsSchema, key string) core.SettingsKnob {
	t.Helper()
	k, ok := schema.Knob(key)
	if !ok {
		t.Fatalf("main payload carries no knob %q", key)
	}
	return *k
}

const scopeMainConfig = `provider:
  type: ollama
  model_name: main-model
  host: http://main-host:11434
  api_key: main-secret
system_prompt: main prompt
max_turns: 55
`

// alpha overrides: provider.type, provider.api_key, system_prompt,
// description, max_turns. It leaves provider.model_name and
// provider.host INHERITED - the mixed shape a group roll-up must catch.
const scopeAlphaConfig = `description: alpha agent
provider:
  type: openai
  api_key: alpha-secret
system_prompt: alpha prompt
max_turns: 7
`

func TestSettingsScopesListsAgentHomes(t *testing.T) {
	home := scopeHome(t, scopeMainConfig, map[string]string{
		"alpha": scopeAlphaConfig,
		// beta is the scaffold shape: every knob commented out, so it
		// overrides nothing.
		"beta": "# provider:\n#   model_name: glm\n# system_prompt: \"\"\n",
	})
	schema, err := settingsSchema(filepath.Join(home, "config", "config.yaml"))
	if err != nil {
		t.Fatalf("settingsSchema: %v", err)
	}
	if got := scopeNames(schema); strings.Join(got, ",") != "alpha,beta" {
		t.Fatalf("scopes = %v, want [alpha beta]", got)
	}

	alpha := scopeByName(t, schema, "alpha")
	// The four R2 groups, in render order.
	var groupKeys []string
	for _, g := range alpha.Groups {
		groupKeys = append(groupKeys, g.Key)
	}
	if strings.Join(groupKeys, ",") != "provider,system_prompt,description,max_turns" {
		t.Fatalf("alpha groups = %v, want the four R2 groups in order", groupKeys)
	}
	// An overridden knob carries the layer's value.
	if k := scopeKnob(t, alpha, "provider.type"); k.Value != "openai" || !k.Overridden {
		t.Errorf("provider.type = %+v, want openai/overridden", k)
	}
	// An INHERITED knob inside an overridden group: same value as main,
	// no override flag.
	inherited := scopeKnob(t, alpha, "provider.model_name")
	if inherited.Overridden {
		t.Errorf("provider.model_name flagged overridden for a layer that does not name it")
	}
	if want := mainKnob(t, schema, "provider.model_name").Value; inherited.Value != want {
		t.Errorf("provider.model_name = %v, want main's %v", inherited.Value, want)
	}
	if k := scopeKnob(t, alpha, "provider.host"); k.Overridden || k.Value != mainKnob(t, schema, "provider.host").Value {
		t.Errorf("provider.host = %+v, want inherited from main", k)
	}
	// A layer secret never rides plaintext.
	if k := scopeKnob(t, alpha, "provider.api_key"); k.Value != maskedSecret || !k.Overridden {
		t.Errorf("provider.api_key = %+v, want the mask + overridden", k)
	}
	if k := scopeKnob(t, alpha, "system_prompt"); k.Value != "alpha prompt" || !k.Overridden {
		t.Errorf("system_prompt = %+v, want alpha prompt/overridden", k)
	}
	if k := scopeKnob(t, alpha, "description"); k.Value != "alpha agent" || !k.Overridden {
		t.Errorf("description = %+v, want alpha agent/overridden", k)
	}
	if k := scopeKnob(t, alpha, "max_turns"); k.Value != 7 || !k.Overridden {
		t.Errorf("max_turns = %+v, want the layer's 7", k)
	}
	// Group roll-ups: provider overridden (type + api_key), all four
	// groups the agent's own in this layer.
	for _, key := range []string{"provider", "system_prompt", "description", "max_turns"} {
		if !scopeGroup(t, alpha, key).Overridden {
			t.Errorf("group %q not flagged overridden for a layer that names its knob", key)
		}
	}

	// beta: the commented scaffold overrides NOTHING and inherits main
	// value for value.
	beta := scopeByName(t, schema, "beta")
	for _, g := range beta.Groups {
		if g.Overridden {
			t.Errorf("beta group %q flagged overridden for a fully commented layer", g.Key)
		}
		for _, k := range g.Knobs {
			if k.Overridden {
				t.Errorf("beta knob %q flagged overridden for a fully commented layer", k.Key)
			}
		}
	}
	for _, key := range []string{"provider.type", "provider.model_name", "provider.host", "provider.api_key", "system_prompt", "max_turns"} {
		if got, want := scopeKnob(t, beta, key).Value, mainKnob(t, schema, key).Value; got != want {
			t.Errorf("beta %s = %v, want main's %v", key, got, want)
		}
	}
	if k := scopeKnob(t, beta, "description"); k.Value != "" {
		t.Errorf("beta description = %v, want the empty inherited base (main has no description knob)", k.Value)
	}
}

func TestSettingsScopesEmptyWorld(t *testing.T) {
	// No agents/ directory at all: the R8 empty world.
	home := t.TempDir()
	homeConfig := filepath.Join(home, "config", "config.yaml")
	writeScopeFile(t, homeConfig, scopeMainConfig)

	schema, err := settingsSchema(homeConfig)
	if err != nil {
		t.Fatalf("settingsSchema: %v", err)
	}
	if schema.Scopes == nil {
		t.Fatalf("scopes = nil, want an empty list (the pane distinguishes null from none)")
	}
	if len(schema.Scopes) != 0 {
		t.Fatalf("scopes = %v, want none", scopeNames(schema))
	}
	// The wire carries the field as [] - never null, never absent.
	body, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(body), `"scopes":[]`) {
		t.Errorf("wire payload carries no empty scopes list: %s", body)
	}

	// An empty agents/ dir is the same world.
	if err := os.MkdirAll(filepath.Join(home, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	schema, err = settingsSchema(homeConfig)
	if err != nil {
		t.Fatalf("settingsSchema (empty agents/): %v", err)
	}
	if len(schema.Scopes) != 0 {
		t.Fatalf("scopes = %v, want none for an empty agents/", scopeNames(schema))
	}
}

func TestSettingsScopesRuntimeMaxTurns(t *testing.T) {
	home := scopeHome(t, scopeMainConfig, map[string]string{
		"alpha": scopeAlphaConfig, // sets max_turns: 7
		"beta":  "# nothing set\n",
	})
	mainConfig := filepath.Join(home, "config", "config.yaml")

	// The serving agent's LIVE knob (42) differs from the file (55):
	// the inherited value must be the runtime one.
	schema, err := settingsPayload(mainConfig, func() int { return 42 })
	if err != nil {
		t.Fatalf("settingsPayload: %v", err)
	}
	if k := scopeKnob(t, scopeByName(t, schema, "beta"), "max_turns"); k.Value != 42 || k.Overridden {
		t.Errorf("beta max_turns = %+v, want the runtime knob's 42, inherited", k)
	}
	// The layer's own value still wins for the scope that sets it.
	if k := scopeKnob(t, scopeByName(t, schema, "alpha"), "max_turns"); k.Value != 7 || !k.Overridden {
		t.Errorf("alpha max_turns = %+v, want the layer's 7, overridden", k)
	}
	// No serving agent (CLI, a save refresh): the file lane stands.
	schema, err = settingsSchema(mainConfig)
	if err != nil {
		t.Fatalf("settingsSchema: %v", err)
	}
	if got, want := scopeKnob(t, scopeByName(t, schema, "beta"), "max_turns").Value, mainKnob(t, schema, "max_turns").Value; got != want {
		t.Errorf("beta max_turns = %v, want the file lane's %v", got, want)
	}
}

func TestSettingsScopesSkipsMainAndMalformedDirs(t *testing.T) {
	home := scopeHome(t, scopeMainConfig, map[string]string{
		"alpha":       scopeAlphaConfig,
		"default":     "description: the retired default home\n",
		"Bad_Name":    "description: malformed\n",
		".hidden":     "description: hidden\n",
		"_scratch":    "description: scratch\n",
		"no-config-a": "", // a valid dir with a config file (the map writes one)
	})
	schema, err := settingsSchema(filepath.Join(home, "config", "config.yaml"))
	if err != nil {
		t.Fatalf("settingsSchema: %v", err)
	}
	for _, s := range schema.Scopes {
		switch s.Name {
		case "default":
			t.Error("agents/default rode as a scope - main's home is the root (R5)")
		case "Bad_Name", ".hidden", "_scratch":
			t.Errorf("malformed/hidden dir %q rode as a scope", s.Name)
		}
	}
	// a valid dir whose layer file is gone still rides (inherits all).
	if err := os.Remove(filepath.Join(home, "agents", "no-config-a", "config", "config.yaml")); err != nil {
		t.Fatal(err)
	}
	schema, err = settingsSchema(filepath.Join(home, "config", "config.yaml"))
	if err != nil {
		t.Fatalf("settingsSchema (deleted layer): %v", err)
	}
	scope := scopeByName(t, schema, "no-config-a")
	if scope.Error != "" {
		t.Errorf("missing layer reported as an error: %s", scope.Error)
	}
	for _, g := range scope.Groups {
		if g.Overridden {
			t.Errorf("group %q overridden with no layer file", g.Key)
		}
	}
}

func TestSettingsScopesReportsUnparsableLayer(t *testing.T) {
	home := scopeHome(t, scopeMainConfig, map[string]string{
		"broken": "max_turns: not-a-number\n",
	})
	schema, err := settingsSchema(filepath.Join(home, "config", "config.yaml"))
	if err != nil {
		t.Fatalf("settingsSchema: %v", err)
	}
	scope := scopeByName(t, schema, "broken")
	if scope.Error == "" {
		t.Fatal("unparsable layer answered no error - the override flags would be a silent guess")
	}
	// The dir is still a scope (R8) and carries inherited values.
	if k := scopeKnob(t, scope, "provider.model_name"); k.Overridden || k.Value != mainKnob(t, schema, "provider.model_name").Value {
		t.Errorf("broken layer's provider.model_name = %+v, want inherited from main", k)
	}
}

func TestSettingsScopeHome(t *testing.T) {
	cases := []struct{ configPath, want string }{
		// the settled layout: <home>/config/config.yaml
		{filepath.Join("/home", "u", ".omega", "config", "config.yaml"), filepath.Join("/home", "u", ".omega")},
		// the legacy layout: <home>/config.yaml
		{filepath.Join("/home", "u", ".omega", "config.yaml"), filepath.Join("/home", "u", ".omega")},
	}
	for _, c := range cases {
		if got := settingsScopeHome(c.configPath); got != c.want {
			t.Errorf("settingsScopeHome(%q) = %q, want %q", c.configPath, got, c.want)
		}
	}
}

func TestSettingsScopesEmptyAPIKeyStaysEmpty(t *testing.T) {
	// An explicitly empty secret is an override with no key behind it:
	// masked-empty stays empty (masking a missing key would imply one).
	home := scopeHome(t, scopeMainConfig, map[string]string{
		"alpha": "provider:\n  api_key: \"\"\n",
	})
	schema, err := settingsSchema(filepath.Join(home, "config", "config.yaml"))
	if err != nil {
		t.Fatalf("settingsSchema: %v", err)
	}
	if k := scopeKnob(t, scopeByName(t, schema, "alpha"), "provider.api_key"); k.Value != "" || !k.Overridden {
		t.Errorf("provider.api_key = %+v, want empty + overridden", k)
	}
}
