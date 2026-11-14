package main

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// --- the whole-document door (card t_a82d39c8) -----------------------

// TestMaskConfigSecretsMasksEveryAPIKey pins the export mask: a secret
// VALUE never leaves the process, in block or flow style, keyed by
// name anywhere in the document - while every comment and every other
// byte of the operator's file survives.
func TestMaskConfigSecretsMasksEveryAPIKey(t *testing.T) {
	src := strings.Join([]string{
		"# the operator's own file",
		"provider:",
		"  type: openai",
		"  api_key: sk-hunter2 # prod",
		"  model_name: m",
		"providers:",
		"  work: {type: openai, host: http://h, api_key: sk-work}",
		"  local:",
		"    type: ollama",
		"    api_key:",
		"compaction:",
		"  enabled: true # keep me",
		"",
	}, "\n")
	out, err := maskConfigSecrets(src)
	if err != nil {
		t.Fatalf("mask: %v", err)
	}
	for _, leaked := range []string{"sk-hunter2", "sk-work"} {
		if strings.Contains(out, leaked) {
			t.Errorf("masked document still carries %q:\n%s", leaked, out)
		}
	}
	if n := strings.Count(out, maskedSecret); n != 2 {
		t.Errorf("mask count = %d, want 2 (one per populated api_key):\n%s", n, out)
	}
	// comments and the untouched knobs survive the node round trip
	for _, want := range []string{"# the operator's own file", "# keep me", "model_name: m"} {
		if !strings.Contains(out, want) {
			t.Errorf("masked document lost %q:\n%s", want, out)
		}
	}
	// an empty api_key stays empty: masking it would imply a key exists
	var doc struct {
		Providers map[string]struct {
			APIKey string `yaml:"api_key"`
		} `yaml:"providers"`
	}
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("masked document does not parse: %v", err)
	}
	if got := doc.Providers["local"].APIKey; got != "" {
		t.Errorf("local api_key %q, want empty", got)
	}
}

// TestMaskConfigSecretsRefusesBrokenDocument: a document that will not
// parse cannot be masked, so it is an error - never the raw bytes.
func TestMaskConfigSecretsRefusesBrokenDocument(t *testing.T) {
	if _, err := maskConfigSecrets("provider: [unclosed\n"); err == nil {
		t.Fatal("expected a parse error for a broken document")
	}
}

// TestResetValuesAreApplicable is the drift guard between the schema's
// defaults and the setter: every value the reset ships must land
// through applySettingValues untouched by coercion errors, and the
// config.yaml-only doors plus the one default-less knob must be absent.
func TestResetValuesAreApplicable(t *testing.T) {
	values, err := resetValues()
	if err != nil {
		t.Fatalf("resetValues: %v", err)
	}
	for _, gated := range []string{"provider.api_key", "server.bind", "provider.instances", "provider.model_name"} {
		if _, ok := values[gated]; ok {
			t.Errorf("reset map carries %s - reset must never delete a config.yaml-only value or a required one", gated)
		}
	}
	cfg := DefaultConfig()
	cfg.Provider.ModelName = "kept" // Validate requires it; reset never writes it
	if err := applySettingValues(&cfg, values); err != nil {
		t.Fatalf("reset map does not apply: %v", err)
	}
	// spot-check that the map carries the schema's own defaults, not
	// hand-typed literals
	def := defaultConfig()
	if cfg.Server.Port != def.Server.Port {
		t.Errorf("server.port %d, want the default %d", cfg.Server.Port, def.Server.Port)
	}
	if cfg.Compaction.Threshold != def.Compaction.Threshold {
		t.Errorf("compaction.threshold %v, want the default %v", cfg.Compaction.Threshold, def.Compaction.Threshold)
	}
	if cfg.Provider.ModelName != "kept" {
		t.Errorf("model_name %q - the reset map touched a knob with no default", cfg.Provider.ModelName)
	}
}

// TestResetRestoresDefaultsAndKeepsTheDoors is the end-to-end law: a
// drifted config comes back to the defaults, while the values the API
// cannot write (the credential, the bind list, the named instances)
// stay exactly as the file had them.
func TestResetRestoresDefaultsAndKeepsTheDoors(t *testing.T) {
	path := writeTestConfig(t, strings.Join([]string{
		"provider:",
		"  type: openai",
		"  model_name: test-model",
		"  api_key: sk-hunter2",
		"  host: http://elsewhere:1234",
		"providers:",
		"  work:",
		"    type: openai",
		"    host: http://work:1234",
		"    api_key: sk-work",
		"server:",
		"  port: 9911",
		"  bind:",
		"    - 0.0.0.0",
		"compaction:",
		"  enabled: false",
		"  keep_last: 3",
		"max_turns: 7",
		"",
	}, "\n"))
	s := &omegaSettings{path: path}
	if _, err := s.Reset(); err != nil {
		t.Fatalf("reset: %v", err)
	}
	cfg, err := fileConfig(path)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	def := defaultConfig()
	// knobs with a default came back
	if cfg.Compaction.Enabled != def.Compaction.Enabled {
		t.Errorf("compaction.enabled %v, want %v", cfg.Compaction.Enabled, def.Compaction.Enabled)
	}
	if cfg.Compaction.KeepLast != def.Compaction.KeepLast {
		t.Errorf("compaction.keep_last %d, want %d", cfg.Compaction.KeepLast, def.Compaction.KeepLast)
	}
	if cfg.MaxTurns != def.MaxTurns {
		t.Errorf("max_turns %d, want %d", cfg.MaxTurns, def.MaxTurns)
	}
	if cfg.Server.Port != def.Server.Port {
		t.Errorf("server.port %d, want %d", cfg.Server.Port, def.Server.Port)
	}
	// the doors stayed shut
	if cfg.Provider.APIKey != "sk-hunter2" {
		t.Errorf("provider.api_key %q - reset destroyed the credential", cfg.Provider.APIKey)
	}
	if len(cfg.Server.Bind) != 1 || cfg.Server.Bind[0] != "0.0.0.0" {
		t.Errorf("server.bind %v - reset rewrote the LAN door", cfg.Server.Bind)
	}
	if cfg.Providers["work"].APIKey != "sk-work" {
		t.Errorf("providers.work.api_key %q - reset deleted an instance secret", cfg.Providers["work"].APIKey)
	}
	if cfg.Provider.ModelName != "test-model" {
		t.Errorf("provider.model_name %q - reset dropped the one required knob", cfg.Provider.ModelName)
	}
	// and the document still loads through the boot path
	if _, err := LoadConfig(path); err != nil {
		t.Fatalf("reset left a config the boot path rejects: %v", err)
	}
}

// TestRawMasksAndSaveRawKeepsTheSecret is the editor round trip: what
// the pane opens carries no credential, and saving that text unchanged
// leaves the stored credential intact.
func TestRawMasksAndSaveRawKeepsTheSecret(t *testing.T) {
	path := writeTestConfig(t, strings.Join([]string{
		"provider:",
		"  type: openai",
		"  model_name: test-model",
		"  api_key: sk-hunter2",
		"providers:",
		"  work:",
		"    type: openai",
		"    api_key: sk-work",
		"",
	}, "\n"))
	s := &omegaSettings{path: path}
	text, err := s.Raw()
	if err != nil {
		t.Fatalf("raw: %v", err)
	}
	if strings.Contains(text, "sk-hunter2") || strings.Contains(text, "sk-work") {
		t.Fatalf("raw document leaked a credential:\n%s", text)
	}
	if _, err := s.SaveRaw(text); err != nil {
		t.Fatalf("save the masked document back: %v", err)
	}
	cfg, err := fileConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.APIKey != "sk-hunter2" {
		t.Errorf("provider.api_key %q after the round trip, want the stored key", cfg.Provider.APIKey)
	}
	if cfg.Providers["work"].APIKey != "sk-work" {
		t.Errorf("providers.work.api_key %q after the round trip, want the stored key", cfg.Providers["work"].APIKey)
	}
}

// TestSaveRawWritesTheSubmittedDocument: the document IS the write - a
// changed knob lands, and the refreshed payload comes back so the pane
// can re-render without a second GET.
func TestSaveRawWritesTheSubmittedDocument(t *testing.T) {
	path := writeTestConfig(t, testConfigYAML)
	s := &omegaSettings{path: path}
	payload, err := s.SaveRaw("provider:\n  model_name: test-model\ncompaction:\n  keep_last: 4\nmax_turns: 9\n")
	if err != nil {
		t.Fatalf("save raw: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Compaction.KeepLast != 4 || cfg.MaxTurns != 9 {
		t.Errorf("config after the raw save: keep_last=%d max_turns=%d", cfg.Compaction.KeepLast, cfg.MaxTurns)
	}
	if payload == nil || len(payload.Groups) == 0 {
		t.Fatalf("refreshed payload missing groups: %v", payload)
	}
}

// TestSaveRawRejectsADocumentTheBootPathRejects: validation is the same
// one a knob save rides, so a raw document cannot smuggle an
// unloadable config onto disk.
func TestSaveRawRejectsADocumentTheBootPathRejects(t *testing.T) {
	path := writeTestConfig(t, testConfigYAML)
	s := &omegaSettings{path: path}
	if _, err := s.SaveRaw("provider:\n  model_name: \"\"\n"); err == nil {
		t.Fatal("expected the model_name validation error")
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.ModelName != "test-model" {
		t.Errorf("a refused document rewrote the file: model_name %q", cfg.Provider.ModelName)
	}
}

// TestSaveRawKeepsAnEnvOnlySecretOutOfTheFile: a mask stands for a
// value the FILE holds. When the file has no key at all (the secret is
// env-supplied), writing "****" must not materialize the env value into
// config.yaml.
func TestSaveRawKeepsAnEnvOnlySecretOutOfTheFile(t *testing.T) {
	t.Setenv("OMEGA_API_KEY", "sk-from-env")
	path := writeTestConfig(t, testConfigYAML)
	s := &omegaSettings{path: path}
	text, err := s.Raw()
	if err != nil {
		t.Fatalf("raw: %v", err)
	}
	if strings.Contains(text, maskedSecret) {
		t.Fatalf("raw masked a key the file does not hold:\n%s", text)
	}
	if _, err := s.SaveRaw(text); err != nil {
		t.Fatalf("save: %v", err)
	}
	cfg, err := fileConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider.APIKey != "" {
		t.Errorf("the env key landed in config.yaml: %q", cfg.Provider.APIKey)
	}
}

// TestSettleSecretsRefusesAnUnreadableDocument: a submitted mask stands
// for the value the FILE holds. When that document cannot be read there
// is nothing to keep, so the save REFUSES instead of writing "****" out
// as the literal credential.
func TestSettleSecretsRefusesAnUnreadableDocument(t *testing.T) {
	cfg := DefaultConfig()
	doc := DefaultConfig()
	doc.Provider.APIKey = maskedSecret
	path := writeTestConfig(t, testConfigYAML) + ".absent"
	if err := settleSecretsFromDisk(path, &cfg, &doc); err == nil {
		t.Fatal("expected a refusal when the on-disk document cannot be read")
	}
	if cfg.Provider.APIKey == maskedSecret {
		t.Errorf("the mask landed in the credential layer: %q", cfg.Provider.APIKey)
	}
}

// TestOmegaSettingsRawOnMissingFile: the export read surfaces the read
// error instead of an empty document.
func TestOmegaSettingsRawOnMissingFile(t *testing.T) {
	s := &omegaSettings{path: writeTestConfig(t, testConfigYAML) + ".absent"}
	if _, err := s.Raw(); err == nil {
		t.Fatal("expected a read error for a missing config file")
	}
	if _, err := os.Stat(s.path); err == nil {
		t.Fatal("the test wrote the missing file")
	}
}
