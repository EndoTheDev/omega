package agent

// agent/configedit_test.go: the definition layer's laws (0.17.3
// make-over, card t_f47319d2). What is pinned here:
//
//	- the node-preserving write (comments and unknown keys survive);
//	- SECRET-FREE: no host, api_key or type can be written, and the
//	  read lane reports a legacy one without leaking its value;
//	- PROVIDER = a referenced instance, validated against settings;
//	- ALWAYS OFF + SUBSET: toggles are the enabled set, the locked pair
//	  cannot be toggled, and a capsule the host has off is refused;
//	- SKILLS = explicit allowlist over the visible registry;
//	- INHERIT-AS-DISABLED materializes a copy and never clobbers.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func strPtr(s string) *string { return &s }

// homeConfig seeds the host config the definition layer validates
// against: two named provider instances and a disabled fold.
func homeConfig(t *testing.T, home string) {
	t.Helper()
	body := "provider:\n  type: ollama\n  model_name: glm-5.3\n" +
		"providers:\n  work:\n    type: openai\n    host: http://localhost:9000\n    api_key: sk-live\n  home:\n    type: ollama\n    host: http://localhost:11434\n" +
		"disabled:\n  modules: [cron]\n  skills: [old-skill]\n"
	if err := WriteFileAtomic(AgentConfigPath(home, DefaultAgentName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// seedSkill writes one <dir>/<name>/SKILL.md tree.
func seedSkill(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name, "SKILL.md"), []byte("# "+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readConfigBody(t *testing.T, home, name string) string {
	t.Helper()
	body, err := os.ReadFile(AgentConfigPath(home, name))
	if err != nil {
		t.Fatalf("read %s layer: %v", name, err)
	}
	return string(body)
}

// TestUpdateAgentConfigLayerUpsertsAndPreserves pins the write law: a
// commented scaffold layer keeps its comments and gains the fields, and
// the provider reference lands as an INSTANCE, never a secret.
func TestUpdateAgentConfigLayerUpsertsAndPreserves(t *testing.T) {
	home := t.TempDir()
	homeConfig(t, home)
	if err := ScaffoldAgent(home, "scout", "probe duty"); err != nil {
		t.Fatal(err)
	}
	seeds := "description: \"probe duty\"\n# keep this comment\nprovider:\n  # model comment\n  model_name: glm-5.3\n"
	path := AgentConfigPath(home, "scout")
	if err := WriteFileAtomic(path, []byte(seeds), 0o644); err != nil {
		t.Fatal(err)
	}

	err := UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{
		Description:      strPtr("night watch"),
		ModelName:        strPtr("claude-4"),
		ProviderInstance: strPtr("work"),
	})
	if err != nil {
		t.Fatal(err)
	}
	body := readConfigBody(t, home, "scout")
	for _, want := range []string{"night watch", "model_name: claude-4", "instance: work", "keep this comment", "model comment"} {
		if !strings.Contains(body, want) {
			t.Errorf("layer lost %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "api_key") || strings.Contains(body, "host:") {
		t.Errorf("a secret or endpoint was written into an agent file:\n%s", body)
	}

	p := loadProfile(home, "scout")
	if p.Description != "night watch" || p.ModelName != "claude-4" || p.ProviderInstance != "work" {
		t.Errorf("profile after edit: %+v", p)
	}
}

// TestUpdateAgentConfigLayerFreshFile writes a missing layer file.
func TestUpdateAgentConfigLayerFreshFile(t *testing.T) {
	home := t.TempDir()
	homeConfig(t, home)
	if err := ScaffoldAgent(home, "writer", ""); err != nil {
		t.Fatal(err)
	}
	path := AgentConfigPath(home, "writer")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := UpdateAgentConfigLayer(home, "writer", AgentConfigUpdates{
		SystemPrompt:     strPtr("be terse"),
		ProviderInstance: strPtr(DefaultProviderInstance),
	}); err != nil {
		t.Fatal(err)
	}
	body := readConfigBody(t, home, "writer")
	if !strings.Contains(body, "system_prompt: be terse") || !strings.Contains(body, "instance: default") {
		t.Errorf("fresh layer body:\n%s", body)
	}
}

// TestUpdateAgentConfigLayerRefusals pins the refusal lanes: the
// default agent never writes, and every secret knob is refused with the
// secret-free law spelled out.
func TestUpdateAgentConfigLayerRefusals(t *testing.T) {
	home := t.TempDir()
	homeConfig(t, home)
	if err := ScaffoldAgent(home, "scout", ""); err != nil {
		t.Fatal(err)
	}
	sentinel := readConfigBody(t, home, DefaultAgentName)
	err := UpdateAgentConfigLayer(home, DefaultAgentName, AgentConfigUpdates{Description: strPtr("x")})
	if !errors.Is(err, ErrAgentConfigLocked) {
		t.Fatalf("default edit err = %v, want ErrAgentConfigLocked", err)
	}
	if after := readConfigBody(t, home, DefaultAgentName); after != sentinel {
		t.Errorf("the default agent's config was written")
	}
	for _, tc := range []struct {
		name string
		u    AgentConfigUpdates
		want string
	}{
		{"host", AgentConfigUpdates{Host: strPtr("http://x")}, "secret-free"},
		{"api_key", AgentConfigUpdates{APIKey: strPtr("sk-x")}, "secret-free"},
		{"type", AgentConfigUpdates{ProviderType: strPtr("openai")}, "secret-free"},
		{"masked", AgentConfigUpdates{APIKey: strPtr(MaskedSecretPlaceholder)}, "read mask"},
		{"instance", AgentConfigUpdates{ProviderInstance: strPtr("nope")}, "not defined in settings"},
		{"negative turns", AgentConfigUpdates{MaxTurns: func() *int { n := -1; return &n }()}, "negative"},
		{"unknown unset", AgentConfigUpdates{Unset: []string{"provider.model"}}, "unknown unset key"},
		{"set and unset", AgentConfigUpdates{SystemPrompt: strPtr("x"), Unset: []string{"system_prompt"}}, "both set and unset"},
		{"toggling the locked pair", AgentConfigUpdates{Toggles: &[]string{"provider"}}, "locked pair"},
		{"toggling a host-off capsule", AgentConfigUpdates{Toggles: &[]string{"cron"}}, "subset law"},
		{"unknown skill", AgentConfigUpdates{SkillsAllow: &[]string{"ghost"}}, "visible registry"},
	} {
		err := UpdateAgentConfigLayer(home, "scout", tc.u)
		var scaffold *AgentConfigScaffoldError
		if !errors.As(err, &scaffold) {
			t.Errorf("%s: err = %v, want AgentConfigScaffoldError", tc.name, err)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to name %q", tc.name, err, tc.want)
		}
	}
}

// TestUpdateAgentConfigLayerMaxTurns pins the int-node law: the knob
// lands UNQUOTED, a hand-written quoted string is corrected, and 0 is a
// real value ("no cap") - nil is what means unset.
func TestUpdateAgentConfigLayerMaxTurns(t *testing.T) {
	home := t.TempDir()
	homeConfig(t, home)
	if err := ScaffoldAgent(home, "scout", "probe duty"); err != nil {
		t.Fatal(err)
	}
	path := AgentConfigPath(home, "scout")
	if err := WriteFileAtomic(path, []byte("max_turns: \"42\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	turns := 42
	if err := UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{MaxTurns: &turns}); err != nil {
		t.Fatal(err)
	}
	body := readConfigBody(t, home, "scout")
	if strings.Contains(body, `"42"`) || !strings.Contains(body, "max_turns: 42") {
		t.Errorf("max_turns is not an int node:\n%s", body)
	}
	zero := 0
	if err := UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{MaxTurns: &zero}); err != nil {
		t.Fatal(err)
	}
	if lay, err := ReadAgentConfigLayer(home, "scout"); err != nil || lay.MaxTurns == nil || *lay.MaxTurns != 0 {
		t.Errorf("zero max_turns read back as %+v (err %v) - 0 is a real value", lay.MaxTurns, err)
	}
}

// TestUpdateAgentConfigLayerUnset pins the reset verb: the key pair is
// deleted, an emptied provider mapping is pruned, and the legacy secret
// keys are scrubbable.
func TestUpdateAgentConfigLayerUnset(t *testing.T) {
	home := t.TempDir()
	homeConfig(t, home)
	if err := ScaffoldAgent(home, "scout", "probe duty"); err != nil {
		t.Fatal(err)
	}
	legacy := "description: probe duty\nprovider:\n  type: ollama\n  host: http://x\n  api_key: sk-old\n  model_name: glm\n"
	if err := WriteFileAtomic(AgentConfigPath(home, "scout"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	// The read lane reports the violation without the value.
	lay, err := ReadAgentConfigLayer(home, "scout")
	if err != nil {
		t.Fatal(err)
	}
	if lay.SecretFree() {
		t.Fatal("a legacy secret file read as secret-free")
	}
	if len(lay.LegacySecrets) != 3 {
		t.Errorf("legacy secrets = %v, want type/host/api_key", lay.LegacySecrets)
	}
	if err := UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{
		Unset: []string{"provider.api_key", "provider.host", "provider.type", "provider.model_name"},
	}); err != nil {
		t.Fatal(err)
	}
	body := readConfigBody(t, home, "scout")
	if hasYAMLKey(body, "  api_key") || strings.Contains(body, "sk-old") || strings.Contains(body, "host:") || strings.Contains(body, "type:") {
		t.Errorf("legacy secret keys survived the scrub:\n%s", body)
	}
	if strings.Contains(body, "provider:") {
		t.Errorf("the emptied provider mapping was not pruned:\n%s", body)
	}
}

// hasYAMLKey reports whether body carries an UNCOMMENTED key line.
func hasYAMLKey(body, key string) bool {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, key+":") || strings.HasPrefix(trimmed, strings.TrimSpace(key)+":") {
			return true
		}
	}
	return false
}

// TestUpdateAgentConfigLayerToggles pins ALWAYS OFF: toggles is the
// enabled set, written as a sorted set, and an empty request writes the
// explicit zero state.
func TestUpdateAgentConfigLayerToggles(t *testing.T) {
	home := t.TempDir()
	homeConfig(t, home)
	if err := ScaffoldAgent(home, "scout", ""); err != nil {
		t.Fatal(err)
	}
	enabled := []string{"kanban", "memory", "kanban"}
	if err := UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{Toggles: &enabled}); err != nil {
		t.Fatal(err)
	}
	lay, err := ReadAgentConfigLayer(home, "scout")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(lay.Toggles, ",") != "kanban,memory" || !lay.TogglesSet {
		t.Errorf("toggles = %v (set %v), want the sorted pair", lay.Toggles, lay.TogglesSet)
	}
	// A scaffolded agent that never enabled anything reads empty - the
	// always-off default, not main's set.
	if err := ScaffoldAgent(home, "quiet", ""); err != nil {
		t.Fatal(err)
	}
	quiet, err := ReadAgentConfigLayer(home, "quiet")
	if err != nil {
		t.Fatal(err)
	}
	if len(quiet.Toggles) != 0 {
		t.Errorf("a fresh agent starts with toggles %v, want none", quiet.Toggles)
	}
	empty := []string{}
	if err := UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{Toggles: &empty}); err != nil {
		t.Fatal(err)
	}
	if body := readConfigBody(t, home, "scout"); !strings.Contains(body, "toggles: []") {
		t.Errorf("an explicit zero toggle state did not render as `toggles: []`:\n%s", body)
	}
}

// TestUpdateAgentConfigLayerSkillsAllowlist pins the allowlist: names
// must exist in the visible registry, and empty means none.
func TestUpdateAgentConfigLayerSkillsAllowlist(t *testing.T) {
	home := t.TempDir()
	homeConfig(t, home)
	if err := ScaffoldAgent(home, "scout", ""); err != nil {
		t.Fatal(err)
	}
	seedSkill(t, filepath.Join(AgentDir(home, "scout"), "skills"), "omega-dev")
	seedSkill(t, filepath.Join(home, "skills", "shared"), "ponytail")
	allow := []string{"ponytail", "omega-dev"}
	if err := UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{SkillsAllow: &allow}); err != nil {
		t.Fatal(err)
	}
	lay, err := ReadAgentConfigLayer(home, "scout")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(lay.SkillsAllow, ",") != "omega-dev,ponytail" || !lay.SkillsAllowSet {
		t.Errorf("allowlist = %v (set %v)", lay.SkillsAllow, lay.SkillsAllowSet)
	}
	if p := loadProfile(home, "scout"); len(p.SkillsAllow) != 2 {
		t.Errorf("profile allowlist = %v", p.SkillsAllow)
	}
	empty := []string{}
	if err := UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{SkillsAllow: &empty}); err != nil {
		t.Fatal(err)
	}
	if body := readConfigBody(t, home, "scout"); !strings.Contains(body, "allow: []") {
		t.Errorf("empty allowlist did not render explicitly:\n%s", body)
	}
}

// TestInheritMaterializesACopy pins INHERIT-AS-DISABLED: enabling a kind
// copies main's artifact in, and an agent that already owns its file
// keeps it.
func TestInheritMaterializesACopy(t *testing.T) {
	home := t.TempDir()
	homeConfig(t, home)
	if err := ScaffoldAgent(home, "scout", ""); err != nil {
		t.Fatal(err)
	}
	// main's artifacts
	if err := os.WriteFile(filepath.Join(home, SoulFileName), []byte("main soul\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "memory.md"), []byte("main memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedSkill(t, filepath.Join(home, "skills"), "grill-me")
	// the agent's own memory must survive
	own := filepath.Join(AgentDir(home, "scout"), "memory.md")
	if err := os.WriteFile(own, []byte("agent memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Nothing is inherited by default.
	if _, err := os.Stat(filepath.Join(AgentDir(home, "scout"), SoulFileName)); !os.IsNotExist(err) {
		t.Fatal("a fresh agent already carries a soul - inherit is supposed to be off")
	}
	allow := []string{"grill-me"}
	if err := UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{
		Inherit:     &AgentInherit{Skills: true, Soul: true, Memory: true},
		SkillsAllow: &allow,
	}); err != nil {
		t.Fatal(err)
	}
	soul, err := os.ReadFile(filepath.Join(AgentDir(home, "scout"), SoulFileName))
	if err != nil || string(soul) != "main soul\n" {
		t.Errorf("soul not materialized: %q (err %v)", soul, err)
	}
	if got, _ := os.ReadFile(own); string(got) != "agent memory\n" {
		t.Errorf("the agent's own memory.md was clobbered: %q", got)
	}
	if _, err := os.Stat(filepath.Join(AgentDir(home, "scout"), "skills", "grill-me", "SKILL.md")); err != nil {
		t.Errorf("an allowlisted main skill was not materialized: %v", err)
	}
	lay, err := ReadAgentConfigLayer(home, "scout")
	if err != nil {
		t.Fatal(err)
	}
	if !lay.Inherit.Soul || !lay.Inherit.Skills || lay.Inherit.User {
		t.Errorf("inherit block read back as %+v", lay.Inherit)
	}
}

// TestLoadHostDefinition pins the settings truth: named instances and a
// namespaced OR legacy-flat disabled fold.
func TestLoadHostDefinition(t *testing.T) {
	home := t.TempDir()
	homeConfig(t, home)
	host, err := LoadHostDefinition(home)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(host.ProviderInstances, ",") != "home,work" {
		t.Errorf("instances = %v", host.ProviderInstances)
	}
	if host.HostEnabledModule("cron") || !host.HostEnabledModule("kanban") {
		t.Errorf("disabled modules misread: %v", host.DisabledModules)
	}
	if host.HostEnabledSkill("old-skill") {
		t.Errorf("disabled skills misread: %v", host.DisabledSkills)
	}
	// The legacy flat shape is still host truth.
	legacy := t.TempDir()
	if err := WriteFileAtomic(AgentConfigPath(legacy, DefaultAgentName), []byte("disabled: [kanban, cron]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hostLegacy, err := LoadHostDefinition(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(hostLegacy.DisabledModules, ",") != "kanban,cron" {
		t.Errorf("legacy fold = %v", hostLegacy.DisabledModules)
	}
}

// TestMalformedDisabledFoldIsLoud pins Nit 2 of the review: a fold that
// will not parse must not read as "nothing is disabled". That default
// direction would validate an agent's write against a phantom
// everything-enabled host.
func TestMalformedDisabledFoldIsLoud(t *testing.T) {
	home := t.TempDir()
	// A mapping whose values are the wrong type: yaml accepts the node,
	// decoding it into []string does not.
	body := "providers:\n  work:\n    type: openai\ndisabled:\n  modules: not-a-list\n"
	if err := WriteFileAtomic(AgentConfigPath(home, DefaultAgentName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadHostDefinition(home); err == nil {
		t.Fatalf("a malformed disabled fold loaded as an empty fold - the subset law would validate against a phantom host")
	}
	// A bare scalar is not a legal fold shape either.
	scalar := t.TempDir()
	if err := WriteFileAtomic(AgentConfigPath(scalar, DefaultAgentName), []byte("disabled: yes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadHostDefinition(scalar); err == nil {
		t.Fatalf("a scalar disabled fold loaded as an empty fold")
	}
}

// TestSkillsAllowlistRefusesAHostDisabledSkill pins Important 4: the
// subset law binds skills exactly as it binds toggles. A host-disabled
// skill used to be accepted 200 into the allowlist and then silently
// dropped at wake - an enabled skill that never loads.
func TestSkillsAllowlistRefusesAHostDisabledSkill(t *testing.T) {
	home := t.TempDir()
	homeConfig(t, home) // disabled.skills: [old-skill]
	if err := ScaffoldAgent(home, "scout", ""); err != nil {
		t.Fatal(err)
	}
	// The skill is on disk and VISIBLE to the agent - the host fold is
	// the only thing that switches it off, which is exactly the pair the
	// write lane must catch.
	seedSkill(t, filepath.Join(home, "skills", "shared"), "old-skill")
	allow := []string{"old-skill"}
	err := UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{SkillsAllow: &allow})
	var refusal *AgentConfigScaffoldError
	if !errors.As(err, &refusal) {
		t.Fatalf("host-disabled skill in the allowlist = %v, want an AgentConfigScaffoldError", err)
	}
	if !strings.Contains(refusal.Message, "subset law") {
		t.Errorf("refusal message = %q, want the subset-law wording the toggle lane uses", refusal.Message)
	}
	// Nothing was half-written.
	if body := readConfigBody(t, home, "scout"); strings.Contains(body, "old-skill") {
		t.Errorf("the refused allowlist reached disk:\n%s", body)
	}
}
