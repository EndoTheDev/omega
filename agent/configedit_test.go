package agent

// agent/configedit_test.go: the agents-tab edit write path
// gates (card t_2d8deb8c). The law: node-preserving upserts only -
// a hand-edited layer's comments and unknown keys survive every
// write, the default agent never gets written, and an unscaffolded
// provider type refuses loudly.
//
// Settings tier 3b (card t_53523d5b) added the two knobs the settings
// scope tabs need: max_turns (a yaml INT node, 0 legal, nil = unset)
// and the reset verb `unset` (delete the named key pair, so the scope
// falls back to main instead of pinning a phantom override). The masked
// secret is refused on the write lane - no path may persist "****".

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func strPtr(s string) *string { return &s }

// TestUpdateAgentConfigLayerUpsertsAndPreserves pins the write law: a
// commented scaffold layer keeps its comments and gains the fields.
func TestUpdateAgentConfigLayerUpsertsAndPreserves(t *testing.T) {
	home := t.TempDir()
	if err := ScaffoldAgent(home, "scout", "probe duty"); err != nil {
		t.Fatal(err)
	}
	seeds := "description: \"probe duty\"\n# keep this comment\nprovider:\n  type: ollama\n  # model comment\n  model_name: glm-5.3\n"
	path := AgentConfigPath(home, "scout")
	if err := WriteFileAtomic(path, []byte(seeds), 0o644); err != nil {
		t.Fatal(err)
	}

	err := UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{
		Description: strPtr("night watch"),
		ModelName:   strPtr("claude-4"),
		Host:        strPtr("http://localhost:11434"),
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(out)
	if !strings.Contains(body, "night watch") {
		t.Errorf("description not upserted:\n%s", body)
	}
	if !strings.Contains(body, "model_name: claude-4") {
		t.Errorf("model_name not upserted:\n%s", body)
	}
	if !strings.Contains(body, "host: http://localhost:11434") {
		t.Errorf("host not upserted:\n%s", body)
	}
	if !strings.Contains(body, "keep this comment") || !strings.Contains(body, "model comment") {
		t.Errorf("untouched keys lost their comments:\n%s", body)
	}
	if !strings.Contains(body, "type: ollama") {
		t.Errorf("untouched provider.type was rewritten:\n%s", body)
	}
	// The API key is nil: absent facts can never be blanked.
	if strings.Contains(body, "api_key") {
		t.Errorf("nil api_key was written:\n%s", body)
	}

	// The profile read reflects the edit (the same parse the tab shows).
	p := loadProfile(home, "scout")
	if p.Description != "night watch" || p.ModelName != "claude-4" {
		t.Errorf("profile after edit: %+v", p)
	}
}

// TestUpdateAgentConfigLayerFreshFile writes a missing layer file.
func TestUpdateAgentConfigLayerFreshFile(t *testing.T) {
	home := t.TempDir()
	if err := ScaffoldAgent(home, "writer", ""); err != nil {
		t.Fatal(err)
	}
	path := AgentConfigPath(home, "writer")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	err := UpdateAgentConfigLayer(home, "writer", AgentConfigUpdates{
		SystemPrompt: strPtr("be terse"),
		ProviderType: strPtr("openai"),
	})
	if err != nil {
		t.Fatal(err)
	}
	p := loadProfile(home, "writer")
	if p.ProviderType != "openai" {
		t.Errorf("provider type = %q", p.ProviderType)
	}
}

// TestUpdateAgentConfigLayerRefusals pins the two refusal lanes.
func TestUpdateAgentConfigLayerRefusals(t *testing.T) {
	home := t.TempDir()
	if err := ScaffoldAgent(home, "scout", ""); err != nil {
		t.Fatal(err)
	}
	// Give the home config a sentinel body first: a missing file reads
	// as a phantom write otherwise (both "before" and "after" are
	// absence, indistinguishable from a write).
	sentinel := "# home config sentinel\nprovider:\n  type: ollama\n"
	if err := WriteFileAtomic(AgentConfigPath(home, DefaultAgentName), []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}
	err := UpdateAgentConfigLayer(home, DefaultAgentName, AgentConfigUpdates{Description: strPtr("x")})
	if !errors.Is(err, ErrAgentConfigLocked) {
		t.Fatalf("default edit err = %v, want ErrAgentConfigLocked", err)
	}
	if after, _ := os.ReadFile(AgentConfigPath(home, DefaultAgentName)); string(after) != sentinel {
		t.Errorf("the default agent's config was written")
	}
	// unscaffolded provider type: the loud 400 lane.
	err = UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{ProviderType: strPtr("gemini")})
	var scaffold *AgentConfigScaffoldError
	if !errors.As(err, &scaffold) {
		t.Fatalf("bad provider err = %v, want AgentConfigScaffoldError", err)
	}
	if !filepath.IsAbs(AgentConfigPath(home, "scout")) {
		t.Fatal("path helper broke")
	}
}

// --- tier 3b: max_turns + the unset verb (card t_53523d5b) -----------

// readConfigBody reads one agent's layer file as text.
func readConfigBody(t *testing.T, home, name string) string {
	t.Helper()
	body, err := os.ReadFile(AgentConfigPath(home, name))
	if err != nil {
		t.Fatalf("read %s layer: %v", name, err)
	}
	return string(body)
}

// hasYAMLKey reports whether body carries an UNCOMMENTED `key:` line -
// at the root or nested. The scaffold template comments every knob, so a
// plain substring match would find `# provider:` / `# api_key:` and lie.
func hasYAMLKey(body, key string) bool {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, key+":") {
			return true
		}
	}
	return false
}

// TestUpdateAgentConfigLayerMaxTurns pins the int-node law: the knob
// lands UNQUOTED (ApplyAgentLayer unmarshals it straight into
// Config.MaxTurns int, so a quoted value would be the next wake's boot
// error), a hand-written quoted string sitting in the key is corrected,
// and 0 is a real value that round-trips ("no cap") - nil is what means
// unset.
func TestUpdateAgentConfigLayerMaxTurns(t *testing.T) {
	home := t.TempDir()
	if err := ScaffoldAgent(home, "scout", "probe duty"); err != nil {
		t.Fatal(err)
	}
	// A hand-edited layer carrying a QUOTED value where the int
	// belongs: the write must retag it, not leave a string in the file.
	seeds := "description: \"probe duty\"\nprovider:\n  type: ollama\nmax_turns: \"7\"\n"
	if err := WriteFileAtomic(AgentConfigPath(home, "scout"), []byte(seeds), 0o644); err != nil {
		t.Fatal(err)
	}

	turns := 42
	if err := UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{MaxTurns: &turns}); err != nil {
		t.Fatalf("write max_turns: %v", err)
	}
	body := readConfigBody(t, home, "scout")
	if !strings.Contains(body, "max_turns: 42") {
		t.Fatalf("max_turns not written as a plain int:\n%s", body)
	}
	if strings.Contains(body, `max_turns: "42"`) || strings.Contains(body, "'42'") {
		t.Fatalf("max_turns landed as a quoted string (a wake boot error):\n%s", body)
	}

	// The layer read reports it ...
	lay, err := ReadAgentConfigLayer(home, "scout")
	if err != nil {
		t.Fatal(err)
	}
	if lay.MaxTurns == nil || *lay.MaxTurns != 42 {
		t.Fatalf("layer max_turns = %v, want 42", lay.MaxTurns)
	}
	// ... and so does the wake's own parse (ApplyAgentLayer decodes the
	// same bytes into Config.MaxTurns int - the shape this asserts).
	var wake struct {
		MaxTurns int `yaml:"max_turns"`
	}
	if err := yaml.Unmarshal([]byte(body), &wake); err != nil {
		t.Fatalf("a wake could not parse the layer: %v", err)
	}
	if wake.MaxTurns != 42 {
		t.Fatalf("wake parse max_turns = %d, want 42", wake.MaxTurns)
	}

	// 0 = no cap, and it is NOT "unset".
	zero := 0
	if err := UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{MaxTurns: &zero}); err != nil {
		t.Fatalf("write max_turns 0: %v", err)
	}
	body = readConfigBody(t, home, "scout")
	if !strings.Contains(body, "max_turns: 0") {
		t.Fatalf("max_turns 0 not written:\n%s", body)
	}
	lay, err = ReadAgentConfigLayer(home, "scout")
	if err != nil {
		t.Fatal(err)
	}
	if lay.MaxTurns == nil || *lay.MaxTurns != 0 {
		t.Fatalf("layer max_turns = %v, want the explicit 0 (nil would be unset)", lay.MaxTurns)
	}
	// The other keys are untouched by either write (yaml.Marshal keeps
	// the document's original quoting, so match the value, not a style).
	if !hasYAMLKey(body, "type") || !strings.Contains(body, "probe duty") {
		t.Fatalf("a max_turns write clobbered its neighbours:\n%s", body)
	}

	// A hand-edited value node of the wrong KIND is repaired, not
	// silently skipped: a sequence where the int belongs would fail the
	// next wake's parse, and "the write did nothing" would hide it.
	if err := WriteFileAtomic(AgentConfigPath(home, "scout"), []byte("description: \"probe duty\"\nmax_turns: [1, 2]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{MaxTurns: &turns}); err != nil {
		t.Fatalf("write over a wrong-kind node: %v", err)
	}
	body = readConfigBody(t, home, "scout")
	if !strings.Contains(body, "max_turns: 42") {
		t.Fatalf("a wrong-kind max_turns node survived the write:\n%s", body)
	}
}

// TestUpdateAgentConfigLayerUnset pins the reset verb: the named key
// pairs are DELETED (never rewritten with main's value - the scope's
// overridden flag is structural, so a write-down would leave a phantom
// override), every other key, comment and style survives, and a
// provider that loses its last sub-key loses the provider key too (an
// empty provider block would shadow the home provider with nothing).
func TestUpdateAgentConfigLayerUnset(t *testing.T) {
	home := t.TempDir()
	if err := ScaffoldAgent(home, "scout", "probe duty"); err != nil {
		t.Fatal(err)
	}
	seeds := "# layer header comment\n" +
		"description: \"probe duty\"\n" +
		"provider:\n" +
		"  type: ollama\n" +
		"  # model comment\n" +
		"  model_name: glm-5.3\n" +
		"  host: http://localhost:11434\n" +
		"system_prompt: \"be terse\"\n" +
		"max_turns: 7\n"
	if err := WriteFileAtomic(AgentConfigPath(home, "scout"), []byte(seeds), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{
		Unset: []string{"system_prompt", "max_turns"},
	}); err != nil {
		t.Fatalf("unset: %v", err)
	}
	body := readConfigBody(t, home, "scout")
	if hasYAMLKey(body, "system_prompt") || hasYAMLKey(body, "max_turns") {
		t.Fatalf("unset left the keys in the layer:\n%s", body)
	}
	// Untouched keys, their comments and their values stand.
	for _, want := range []string{"# layer header comment", "probe duty", "type: ollama", "# model comment", "model_name: glm-5.3", "host: http://localhost:11434"} {
		if !strings.Contains(body, want) {
			t.Fatalf("unset lost %q:\n%s", want, body)
		}
	}

	// The layer read - the settings scope's source of truth - says the
	// knobs are NOT named any more, which is exactly what makes the
	// scope answer overridden=false and inherit main.
	lay, err := ReadAgentConfigLayer(home, "scout")
	if err != nil {
		t.Fatal(err)
	}
	if lay.SystemPrompt.Set {
		t.Errorf("system_prompt still reads Set after unset: %+v", lay.SystemPrompt)
	}
	if lay.MaxTurns != nil {
		t.Errorf("max_turns still reads %d after unset (want nil = inherit)", *lay.MaxTurns)
	}
	if !lay.Description.Set || lay.Description.Value != "probe duty" {
		t.Errorf("description = %+v, want the untouched override", lay.Description)
	}
	if !lay.ProviderType.Set || !lay.ModelName.Set || !lay.Host.Set {
		t.Errorf("provider knobs lost their presence: %+v", lay)
	}

	// A single sub-key removal falls back for that knob only.
	if err := UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{
		Unset: []string{"provider.model_name"},
	}); err != nil {
		t.Fatalf("unset model_name: %v", err)
	}
	body = readConfigBody(t, home, "scout")
	if hasYAMLKey(body, "model_name") {
		t.Fatalf("provider.model_name survived the unset:\n%s", body)
	}
	lay, err = ReadAgentConfigLayer(home, "scout")
	if err != nil {
		t.Fatal(err)
	}
	if lay.ModelName.Set {
		t.Errorf("model_name still overridden after unset: %+v", lay.ModelName)
	}
	if !lay.ProviderType.Set || lay.ProviderType.Value != "ollama" {
		t.Errorf("provider.type = %+v, want the untouched ollama override", lay.ProviderType)
	}

	// Removing the last provider sub-keys removes the provider key.
	if err := UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{
		Unset: []string{"provider.type", "provider.host"},
	}); err != nil {
		t.Fatalf("unset rest of provider: %v", err)
	}
	body = readConfigBody(t, home, "scout")
	if hasYAMLKey(body, "provider") {
		t.Fatalf("an empty provider block survived (it would shadow the home provider):\n%s", body)
	}
	if !strings.Contains(body, "probe duty") || !strings.Contains(body, "# layer header comment") {
		t.Fatalf("the provider prune ate a neighbour:\n%s", body)
	}
	var wake map[string]any
	if err := yaml.Unmarshal([]byte(body), &wake); err != nil {
		t.Fatalf("a wake could not parse the pruned layer: %v", err)
	}
}

// TestUpdateAgentConfigLayerTier3bRefusals pins the 400 lane for the two
// new fields: a negative max_turns (config.go's own floor), an unknown
// unset name (including a GROUP name - `provider` is not a knob), and a
// key both set and unset in one request. Every refusal leaves the layer
// byte-identical: a refusal is a door, not a half write.
func TestUpdateAgentConfigLayerTier3bRefusals(t *testing.T) {
	home := t.TempDir()
	if err := ScaffoldAgent(home, "scout", "probe duty"); err != nil {
		t.Fatal(err)
	}
	sentinel := "description: \"probe duty\"\nprovider:\n  type: ollama\n"
	if err := WriteFileAtomic(AgentConfigPath(home, "scout"), []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}
	negative := -1
	fortyTwo := 42
	cases := []struct {
		name string
		u    AgentConfigUpdates
	}{
		{"negative max_turns", AgentConfigUpdates{MaxTurns: &negative}},
		{"unknown unset name", AgentConfigUpdates{Unset: []string{"provider.model"}}},
		{"group name is not a knob", AgentConfigUpdates{Unset: []string{"provider"}}},
		{"set and unset the same key", AgentConfigUpdates{SystemPrompt: strPtr("x"), Unset: []string{"system_prompt"}}},
		{"set and unset max_turns", AgentConfigUpdates{MaxTurns: &fortyTwo, Unset: []string{"max_turns"}}},
		{"set and unset a provider sub-key", AgentConfigUpdates{ModelName: strPtr("glm"), Unset: []string{"provider.model_name"}}},
	}
	for _, tc := range cases {
		err := UpdateAgentConfigLayer(home, "scout", tc.u)
		var scaffold *AgentConfigScaffoldError
		if !errors.As(err, &scaffold) {
			t.Errorf("%s: err = %v, want AgentConfigScaffoldError", tc.name, err)
		}
		if after := readConfigBody(t, home, "scout"); after != sentinel {
			t.Errorf("%s wrote the layer on a refusal:\n%s", tc.name, after)
		}
	}
	// A legal request through the same door still lands (the refusals
	// above are not a blanket lockout).
	if err := UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{MaxTurns: &fortyTwo}); err != nil {
		t.Fatalf("legal max_turns after refusals: %v", err)
	}
}

// TestUpdateAgentConfigLayerRefusesMaskedSecret pins the masked-secret
// hazard: the read lanes mask provider.api_key to the placeholder, so a
// UI that echoed the mask back would persist the literal asterisks as
// the agent's key. No path may write it - the set lane refuses it
// loudly, and the reset lane DELETES the key instead of writing any
// value at all.
func TestUpdateAgentConfigLayerRefusesMaskedSecret(t *testing.T) {
	home := t.TempDir()
	if err := ScaffoldAgent(home, "scout", "probe duty"); err != nil {
		t.Fatal(err)
	}
	if err := UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{APIKey: strPtr("sk-real-secret")}); err != nil {
		t.Fatalf("seed the secret: %v", err)
	}
	seeded := readConfigBody(t, home, "scout")
	if !strings.Contains(seeded, "api_key: sk-real-secret") {
		t.Fatalf("the secret did not land (premise broken):\n%s", seeded)
	}

	// The masked echo is refused, and nothing is written.
	err := UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{APIKey: strPtr(MaskedSecretPlaceholder)})
	var scaffold *AgentConfigScaffoldError
	if !errors.As(err, &scaffold) {
		t.Fatalf("masked api_key err = %v, want AgentConfigScaffoldError", err)
	}
	if after := readConfigBody(t, home, "scout"); after != seeded {
		t.Fatalf("the mask write landed:\n%s", after)
	}
	if strings.Contains(seeded, MaskedSecretPlaceholder) {
		t.Fatalf("the layer carries the read mask:\n%s", seeded)
	}

	// The reset lane clears the key by deletion - no value, no mask.
	if err := UpdateAgentConfigLayer(home, "scout", AgentConfigUpdates{
		Unset: []string{"provider.api_key"},
	}); err != nil {
		t.Fatalf("unset api_key: %v", err)
	}
	body := readConfigBody(t, home, "scout")
	if hasYAMLKey(body, "api_key") || strings.Contains(body, "sk-real-secret") || strings.Contains(body, MaskedSecretPlaceholder) {
		t.Fatalf("the secret reset left a value behind:\n%s", body)
	}
	// api_key was the provider's only sub-key, so the block goes with it.
	// (The scaffold template's COMMENTED provider block is not a key - it
	// is documentation and stays.)
	if hasYAMLKey(body, "provider") {
		t.Fatalf("empty provider block after the secret reset:\n%s", body)
	}
}
