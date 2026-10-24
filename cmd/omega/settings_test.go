package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/modules/compactor"
	"gopkg.in/yaml.v3"
)

// settingsKnobKeys lists every knob the schema exposes, as the single
// inventory assertion: adding a Config field without a knob here (or a
// knob without a field) fails this set comparison.
func settingsKnobKeys(t *testing.T) map[string]bool {
	t.Helper()
	return map[string]bool{
		"provider.type":               true,
		"provider.model_name":         true,
		"provider.vision_model":       true,
		"provider.title_model":        true,
		"provider.host":               true,
		"provider.api_key":            true,
		"server.port":                 true,
		"server.bind":                 true,
		"server.run_timeout":          true,
		"store.db_path":               true,
		"compaction.enabled":          true,
		"compaction.threshold":        true,
		"compaction.context_window":   true,
		"compaction.keep_first":       true,
		"compaction.keep_last":        true,
		"compaction.reserve_tokens":   true,
		"compaction.max_tool_output":  true,
		"system_prompt":               true,
		"http_timeout":                true,
		"max_turns":                   true,
		"notifications":               true,
		"theme":                       true,
		"skills.dir":                  true,
		"memory.enabled":              true,
		"memory.user_profile_enabled": true,
		"memory.char_limit":           true,
		"memory.user_char_limit":      true,
		"memory.file":                 true,
		"memory.user_file":            true,
		"logging.enabled":             true,
		"logging.file":                true,
	}
}

// schemaKnobKeys flattens the schema groups into a path set.
func schemaKnobKeys(groups []core.SettingsGroup) map[string]bool {
	out := map[string]bool{}
	for _, g := range groups {
		for _, k := range g.Knobs {
			out[k.Key] = true
		}
	}
	return out
}

func TestSettingGroupsCoverEveryConfigKnob(t *testing.T) {
	cfg := DefaultConfig()
	got := schemaKnobKeys(settingGroups(&cfg))
	want := settingsKnobKeys(t)
	for key := range want {
		if !got[key] {
			t.Errorf("no knob for config key %q", key)
		}
	}
	for key := range got {
		if !want[key] {
			t.Errorf("schema knob %q has no Config field behind it (invented key?)", key)
		}
	}
}

// writeTestConfig lands a minimal valid config.yaml in a temp dir.
func writeTestConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

const testConfigYAML = "provider:\n  model_name: test-model\n"

func TestApplySettingValuesTyped(t *testing.T) {
	cfg := DefaultConfig()
	err := applySettingValues(&cfg, map[string]any{
		"provider.host":         "  http://host:1234  ",
		"provider.type":         " OpenAI ",
		"server.port":           float64(9000),
		"compaction.enabled":    false,
		"compaction.threshold":  0.75,
		"compaction.keep_first": float64(3),
		"http_timeout":          float64(120),
		"notifications":         "OFF",
		"logging.file":          "x.log",
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if cfg.Provider.Host != "http://host:1234" {
		t.Errorf("host not trimmed: %q", cfg.Provider.Host)
	}
	if cfg.Provider.Type != "OpenAI" {
		t.Errorf("type not preserved case: %q", cfg.Provider.Type)
	}
	if cfg.Server.Port != 9000 {
		t.Errorf("port: %d", cfg.Server.Port)
	}
	if cfg.Compaction.Enabled {
		t.Error("compaction.enabled stay true")
	}
	if cfg.Compaction.Threshold != 0.75 {
		t.Errorf("threshold: %v", cfg.Compaction.Threshold)
	}
	if cfg.Compaction.KeepFirst != 3 {
		t.Errorf("keep_first: %d", cfg.Compaction.KeepFirst)
	}
	if cfg.HTTPTimeout != 120 {
		t.Errorf("http_timeout: %d", cfg.HTTPTimeout)
	}
	if cfg.Notifications != "off" {
		t.Errorf("notifications not normalized: %q", cfg.Notifications)
	}
	if cfg.Logging.File != "x.log" {
		t.Errorf("logging.file: %q", cfg.Logging.File)
	}
}

func TestApplySettingValuesErrors(t *testing.T) {
	cases := []struct {
		name   string
		values map[string]any
		want   string
	}{
		{"unknown key", map[string]any{"nope.nope": 1}, "unknown setting"},
		{"int as float fraction", map[string]any{"server.port": 9000.5}, "whole number"},
		{"int as non-numeric", map[string]any{"server.port": "big"}, "whole number"},
		{"int out of range", map[string]any{"server.port": float64(0)}, "out of range"},
		{"port above uint16", map[string]any{"server.port": float64(70000)}, "out of range"},
		{"float out of range", map[string]any{"compaction.threshold": float64(2)}, "out of range"},
		{"bool as string", map[string]any{"compaction.enabled": "yes"}, "true or false"},
		{"bad notifications", map[string]any{"notifications": "morse"}, "bell, desktop, off"},
		{"string where number", map[string]any{"http_timeout": "120s"}, "whole number"},
		// gated knobs: the hot-PUT path never writes these, and the
		// error names config.yaml as the only door
		{"api_key write", map[string]any{"provider.api_key": "sk-x"}, "config.yaml"},
		{"bind write", map[string]any{"server.bind": "0.0.0.0"}, "config.yaml"},
		{"masked echo write", map[string]any{"provider.api_key": maskedSecret}, "config.yaml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			err := applySettingValues(&cfg, tc.values)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestSaveSettingsPersistsAndReturnsRefreshedSchema(t *testing.T) {
	path := writeTestConfig(t, testConfigYAML)
	pctx := &agent.Context{Compactor: &compactor.Compactor{}}

	payload, err := saveSettings(path, pctx, map[string]any{
		"compaction.enabled":  false,
		"provider.model_name": "new-model",
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	// the file on disk now carries the change (materialized config)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved Config
	if err := yaml.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Compaction.Enabled {
		t.Error("compaction.enabled not persisted to disk")
	}
	if saved.Provider.ModelName != "new-model" {
		t.Errorf("model_name not persisted: %q", saved.Provider.ModelName)
	}

	// the returned payload reflects the saved values, and groups survive
	if payload == nil || len(payload.Groups) == 0 {
		t.Fatalf("refreshed payload missing groups: %v", payload)
	}
	groups := payload.Groups
	if got := schemaKnobKeys(groups); len(got) != len(settingsKnobKeys(t)) {
		t.Errorf("refreshed schema knob count %d != inventory %d", len(got), len(settingsKnobKeys(t)))
	}
	for _, g := range groups {
		for _, k := range g.Knobs {
			if k.Key == "compaction.enabled" && k.Value != false {
				t.Errorf("compaction.enabled knob value %v, want false", k.Value)
			}
		}
	}
}

func TestSaveSettingsRejectsInvalidValuesWithoutWriting(t *testing.T) {
	path := writeTestConfig(t, testConfigYAML)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := saveSettings(path, nil, map[string]any{
		"server.port": float64(0),
	}); err == nil {
		t.Fatal("expected validation error for port 0")
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("config file was rewritten despite validation failure")
	}
}

func TestWriteConfigFileAtomicNoStrayTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := DefaultConfig()
	cfg.Provider.ModelName = "m"
	if err := writeConfigFile(path, &cfg); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.yaml" {
		for _, e := range entries {
			t.Logf("leftover: %s", e.Name())
		}
		t.Fatalf("temp file survived the atomic write: %d entries", len(entries))
	}
}

func TestSchemaMasksAPIKey(t *testing.T) {
	path := writeTestConfig(t, testConfigYAML+"  api_key: sk-hunter2\n")
	payload, err := settingsSchema(path)
	if err != nil {
		t.Fatal(err)
	}
	apiKey := func(p *core.SettingsSchema) any {
		for _, g := range p.Groups {
			for _, k := range g.Knobs {
				if k.Key == "provider.api_key" {
					return k.Value
				}
			}
		}
		t.Errorf("no provider.api_key knob in schema")
		return nil
	}
	if got := apiKey(payload); got != maskedSecret {
		t.Errorf("GET schema api_key %q, want masked", got)
	}
	// the save-refresh payload rides the same channel: masked too
	refreshed, err := saveSettings(path, nil, map[string]any{
		"compaction.keep_last": float64(4),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := apiKey(refreshed); got != maskedSecret {
		t.Errorf("save-refresh api_key %q, want masked", got)
	}
	// empty stays empty: a mask on a missing key would imply one exists
	path = writeTestConfig(t, testConfigYAML)
	payload, err = settingsSchema(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := apiKey(payload); got != "" {
		t.Errorf("api_key without a stored key %q, want empty", got)
	}
}

func TestHotApplyFlowsToCompactorAndHTTPClient(t *testing.T) {
	comp := &compactor.Compactor{}
	pctx := &agent.Context{Compactor: comp}
	cfg := DefaultConfig()
	cfg.HTTPTimeout = 42
	cfg.Compaction.Enabled = false
	cfg.Compaction.KeepLast = 7
	hotApply(cfg, pctx)

	if got := ai.HTTPClient().Timeout; got != 42*time.Second {
		t.Errorf("http client timeout %v, want 42s", got)
	}
	// the compactor honors the saved config on every Compact call:
	// disabled compaction returns the input untouched regardless of size
	msgs := []core.Message{core.User{Content: "hello"}, core.Assistant{Content: "hi there"}}
	out, err := comp.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(msgs) {
		t.Errorf("disabled compaction changed the message count")
	}

	// nil context must not panic
	hotApply(cfg, nil)
}

func TestOmegaSettingsImplementsSeam(t *testing.T) {
	path := writeTestConfig(t, testConfigYAML)
	s := &omegaSettings{path: path, pctx: nil}
	payload, err := s.Schema()
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	if payload == nil || len(payload.Groups) == 0 {
		t.Fatalf("schema missing groups")
	}
	// compile-time assertion also covers the interface; this round-trips
	// Save through the seam shape too
	if _, err := s.Save(map[string]any{"compaction.keep_last": float64(4)}); err != nil {
		t.Fatalf("save: %v", err)
	}
}
