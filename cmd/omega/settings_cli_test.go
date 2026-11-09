package main

// settings_cli_test.go pins the terminal face of the settings pane
// (card t_013b4643): `omega settings` / `get` / `set`. The load-bearing
// assertions are the ones that prove there is no second truth - a CLI
// set lands in the same config.yaml the webui's Schema() reads back,
// and a refusal is the seam's own sentence with the file untouched.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// settingsCLIHome prepares an OMEGA_HOME whose config.yaml is the file
// both verbs resolve through resolveConfigPath. It returns the home and
// the config path.
func settingsCLIHome(t *testing.T, body string) (home, cfgPath string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	cfgPath = filepath.Join(home, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(body), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return home, cfgPath
}

// TestSettingsCLIListsEveryKnobGroup proves the bare verb renders the
// whole schema the pane's GET serves: groups as headers, every knob as
// `key = value`, and the api_key masked.
func TestSettingsCLIListsEveryKnobGroup(t *testing.T) {
	settingsCLIHome(t, testConfigYAML)

	out, err := captureStdout(t, func() error { return cmdSettings("", nil) })
	if err != nil {
		t.Fatalf("cmdSettings: %v", err)
	}
	for _, want := range []string{
		`provider.model_name = "test-model"`,
		"provider.api_key = \"\"", // unset stays visibly empty, never masked into implying a key
		"server",
		"compaction.keep_first",
		"approvals.mode",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list output omits %q\n---\n%s", want, out)
		}
	}
	if !strings.HasPrefix(out, "provider\n") {
		t.Errorf("list output does not open with the provider group header:\n%s", out)
	}
	// every advertised knob appears exactly once
	keys := schemaKnobKeysFromCLI(t, out)
	if want := len(settingsKnobKeys(t)); len(keys) != want {
		t.Errorf("list printed %d knobs, schema inventory has %d", len(keys), want)
	}
}

// schemaKnobKeysFromCLI parses the `  key = value` lines of the list.
func schemaKnobKeysFromCLI(t *testing.T, out string) map[string]bool {
	t.Helper()
	keys := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimPrefix(line, "  ")
		if !strings.HasPrefix(line, " ") && strings.Contains(line, " = ") {
			key, _, _ := strings.Cut(line, " = ")
			keys[key] = true
		}
	}
	return keys
}

// TestSettingsCLIGetReadsOneKnob: a single key prints the raw value (no
// quotes) so a script can consume it; an unknown key is loud.
func TestSettingsCLIGetReadsOneKnob(t *testing.T) {
	settingsCLIHome(t, testConfigYAML)

	out, err := captureStdout(t, func() error {
		return cmdSettings("", []string{"get", "provider.model_name"})
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if out != "test-model\n" {
		t.Errorf("get output %q, want %q", out, "test-model\n")
	}

	if _, err := captureStdout(t, func() error {
		return cmdSettings("", []string{"get", "nope.nope"})
	}); err == nil || !strings.Contains(err.Error(), "unknown setting: nope.nope") {
		t.Fatalf("unknown key error = %v, want unknown setting", err)
	}
}

// TestSettingsCLISetLandsInTheSeam: a CLI write persists into the same
// config.yaml the pane's Schema() loads, and the refreshed schema the
// seam returns is what prints - the readback, not the typed text.
func TestSettingsCLISetLandsInTheSeam(t *testing.T) {
	_, cfgPath := settingsCLIHome(t, testConfigYAML)

	out, err := captureStdout(t, func() error {
		return cmdSettings("", []string{"set", "server.port", "18432"})
	})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if strings.TrimSpace(out) != "server.port = 18432" {
		t.Errorf("set receipt %q", out)
	}

	// the pane's own read path (settingsSchema) sees it - one source of truth
	schema, err := settingsSchema(cfgPath)
	if err != nil {
		t.Fatalf("settingsSchema: %v", err)
	}
	knob, ok := schema.Knob("server.port")
	if !ok {
		t.Fatal("schema lost server.port")
	}
	if knob.Value != 18432 {
		t.Errorf("schema value %v, want 18432", knob.Value)
	}

	// and the CLI get agrees
	out, err = captureStdout(t, func() error {
		return cmdSettings("", []string{"get", "server.port"})
	})
	if err != nil {
		t.Fatalf("get after set: %v", err)
	}
	if out != "18432\n" {
		t.Errorf("get after set = %q", out)
	}
}

// TestSettingsCLISetCoercesByKnobType: toggles accept the operator's
// words, numbers the wire's float64, and the instances list the JSON the
// pane posts.
func TestSettingsCLISetCoercesByKnobType(t *testing.T) {
	_, cfgPath := settingsCLIHome(t, testConfigYAML)

	for _, tc := range []struct{ key, val string }{
		{"compaction.enabled", "off"},
		{"compaction.threshold", "0.75"},
		{"system_prompt", "be brief"},
	} {
		if _, err := captureStdout(t, func() error {
			return cmdSettings("", []string{"set", tc.key, tc.val})
		}); err != nil {
			t.Fatalf("set %s=%s: %v", tc.key, tc.val, err)
		}
	}
	schema, err := settingsSchema(cfgPath)
	if err != nil {
		t.Fatalf("settingsSchema: %v", err)
	}
	if k, _ := schema.Knob("compaction.enabled"); k.Value != false {
		t.Errorf("compaction.enabled = %v, want false", k.Value)
	}
	if k, _ := schema.Knob("compaction.threshold"); k.Value != 0.75 {
		t.Errorf("compaction.threshold = %v, want 0.75", k.Value)
	}
	if k, _ := schema.Knob("system_prompt"); k.Value != "be brief" {
		t.Errorf("system_prompt = %v", k.Value)
	}

	// a non-boolean word never reaches the seam
	if _, err := captureStdout(t, func() error {
		return cmdSettings("", []string{"set", "compaction.enabled", "maybe"})
	}); err == nil || !strings.Contains(err.Error(), "expected true or false") {
		t.Errorf("non-boolean error = %v", err)
	}
}

// TestSettingsCLISetRefusesLikeThePane: every refusal is the seam's own
// sentence, and a refused write leaves the file byte-identical.
func TestSettingsCLISetRefusesLikeThePane(t *testing.T) {
	_, cfgPath := settingsCLIHome(t, testConfigYAML)
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct{ key, val, want string }{
		{"provider.api_key", "sk-secret", "config.yaml is the only door"},
		{"server.bind", "0.0.0.0", "config.yaml is the only door"},
		{"compaction.threshold", "3", "out of range"},
		{"server.port", "18432.5", "whole number"},
		{"server.port", "NaN", "finite number"},
		{"nope.nope", "1", "unknown setting"},
	}
	for _, tc := range cases {
		_, err := captureStdout(t, func() error {
			return cmdSettings("", []string{"set", tc.key, tc.val})
		})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("set %s=%s error = %v, want %q", tc.key, tc.val, err, tc.want)
			continue
		}
		after, rerr := os.ReadFile(cfgPath)
		if rerr != nil {
			t.Fatal(rerr)
		}
		if string(after) != string(before) {
			t.Fatalf("set %s=%s rewrote the config despite refusing:\n%s", tc.key, tc.val, after)
		}
	}
}

// TestSettingsCLIRequiresConfig: no config in the home is the setup
// gate's refusal for both verbs - a write never invents a file.
func TestSettingsCLIRequiresConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)

	for _, args := range [][]string{nil, {"get", "server.port"}, {"set", "server.port", "18432"}} {
		if _, err := captureStdout(t, func() error {
			return cmdSettings("", args)
		}); err == nil || !strings.Contains(err.Error(), "omega setup") {
			t.Errorf("args %v error = %v, want the setup gate", args, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "config.yaml")); !os.IsNotExist(err) {
		t.Error("a refused run created config.yaml")
	}
}

// TestSettingsCLIUsageErrors: wrong arity and a leading-dash key are
// usage refusals, not silent no-ops.
func TestSettingsCLIUsageErrors(t *testing.T) {
	settingsCLIHome(t, testConfigYAML)

	for _, args := range [][]string{
		{"set"},
		{"set", "server.port"},
		{"set", "server.port", "18432", "extra"},
		{"get", "a", "b"},
		{"get", "--help"},
		{"bogus"},
	} {
		if _, err := captureStdout(t, func() error {
			return cmdSettings("", args)
		}); err == nil {
			t.Errorf("args %v: expected an error", args)
		}
	}
}

// TestSettingsArgsStripsConfigFlag: --config is the file selector and
// keeps working before or after the verb.
func TestSettingsArgsStripsConfigFlag(t *testing.T) {
	got := settingsArgs([]string{"--config", "C:/tmp/x.yaml", "set", "server.port", "18432"})
	want := []string{"set", "server.port", "18432"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("args = %v, want %v", got, want)
	}
	got = settingsArgs([]string{"get", "--config=C:/tmp/x.yaml", "server.port"})
	if strings.Join(got, "|") != "get|server.port" {
		t.Errorf("=form args = %v", got)
	}
}

// TestSettingsCLIHonorsConfigFlag: the write lands in the --config file,
// not the home's.
func TestSettingsCLIHonorsConfigFlag(t *testing.T) {
	settingsCLIHome(t, testConfigYAML) // home config (untouched)
	other := filepath.Join(t.TempDir(), "other.yaml")
	if err := os.WriteFile(other, []byte(testConfigYAML), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := captureStdout(t, func() error {
		return cmdSettings(other, []string{"set", "server.port", "18333"})
	}); err != nil {
		t.Fatalf("set: %v", err)
	}
	homeCfg := filepath.Join(os.Getenv("OMEGA_HOME"), "config.yaml")
	if b, _ := os.ReadFile(homeCfg); strings.Contains(string(b), "18333") {
		t.Error("write landed in the home config instead of the --config file")
	}
	if b, _ := os.ReadFile(other); !strings.Contains(string(b), "18333") {
		t.Errorf("--config file misses the write:\n%s", b)
	}
}
