package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/compactor"
	"github.com/EndoTheDev/omega/llm"
	"gopkg.in/yaml.v3"
)

// settingsKnobKeys lists every knob the schema exposes, as the single
// inventory assertion: adding a Config field without a knob here (or a
// knob without a field) fails this set comparison.
func settingsKnobKeys(t *testing.T) map[string]bool {
	t.Helper()
	return map[string]bool{
		"provider.model_name":            true,
		"provider.vision_model":          true,
		"provider.title_model":           true,
		"provider.compaction_model":      true,
		"provider.summarizer_model":      true,
		"provider.skills_model":          true,
		"provider.approvals_model":       true,
		"provider.instances":             true,
		"server.port":                    true,
		"server.bind":                    true,
		"server.run_timeout":             true,
		"server.default_workspace":       true,
		"store.db_path":                  true,
		"sessions.retention_days":        true,
		"sessions.summarize_after_hours": true,
		"compaction.enabled":             true,
		"compaction.threshold":           true,
		"compaction.context_window":      true,
		"compaction.keep_first":          true,
		"compaction.keep_last":           true,
		"compaction.reserve_tokens":      true,
		"compaction.max_tool_output":     true,
		"system_prompt":                  true,
		"http_timeout":                   true,
		"max_turns":                      true,
		"skills.dir":                     true,
		"skills.nudge_interval":          true,
		"memory.enabled":                 true,
		"memory.user_profile_enabled":    true,
		"memory.char_limit":              true,
		"memory.user_char_limit":         true,
		"memory.file":                    true,
		"memory.user_file":               true,
		"logging.enabled":                true,
		"logging.file":                   true,
		"approvals.mode":                 true,
		"approvals.smart_policy":         true,
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

// TestModelKnobsTyped pins the typed model pickers' schema shape
// (card t_70c28128): every model field rides type "model", the vision
// slot carries the vision filter, and no other knob type claims the
// model rendering.
func TestModelKnobsTyped(t *testing.T) {
	cfg := DefaultConfig()
	for _, g := range settingGroups(&cfg) {
		for _, k := range g.Knobs {
			switch k.Key {
			case "provider.model_name":
				if k.Type != "model" {
					t.Errorf("%s type = %q, want model", k.Key, k.Type)
				}
				if k.Filter != "" {
					t.Errorf("%s filter = %q, want empty (full chat list)", k.Key, k.Filter)
				}
			case "provider.title_model":
				if k.Type != "model" {
					t.Errorf("%s type = %q, want model", k.Key, k.Type)
				}
				if k.Filter != "" {
					t.Errorf("%s filter = %q, want empty (full chat list)", k.Key, k.Filter)
				}
			case "provider.vision_model":
				if k.Type != "model" {
					t.Errorf("%s type = %q, want model", k.Key, k.Type)
				}
				if k.Filter != "vision" {
					t.Errorf("%s filter = %q, want vision", k.Key, k.Filter)
				}
			case "approvals.model", "skills.curator_model", "sessions.summarizer_model":
				t.Errorf("%s: the per-subsystem model knobs moved to provider settings (card t_fc0b2f2d); this case must die with the schema", k.Key)
			case "provider.compaction_model", "provider.summarizer_model", "provider.skills_model":
				if k.Type != "model" {
					t.Errorf("%s type = %q, want model", k.Key, k.Type)
				}
				if k.Filter != "" {
					t.Errorf("%s filter = %q, want empty (the full model list)", k.Key, k.Filter)
				}
				if !k.Slot {
					t.Errorf("%s is not marked slot = true (the instance selector)", k.Key)
				}
			case "provider.approvals_model":
				if k.Type != "model" {
					t.Errorf("%s type = %q, want model", k.Key, k.Type)
				}
				if k.Filter != "decision" {
					t.Errorf("%s filter = %q, want decision (the decision-family dropdown)", k.Key, k.Filter)
				}
				if !k.Slot {
					t.Errorf("%s is not marked slot = true (the instance selector)", k.Key)
				}
			default:
				if k.Type == "model" {
					t.Errorf("%s renders a model picker but is not a model slot", k.Key)
				}
			}
		}
	}
}

// TestModelKnobFilterValidValues fails a filter value the webui has no
// renderer for - the seam vocabulary stays closed until the approval
// gate lands its decision slot with its own value here.
func TestModelKnobFilterValidValues(t *testing.T) {
	valid := map[string]bool{"": true, "vision": true, "decision": true}
	cfg := DefaultConfig()
	for _, g := range settingGroups(&cfg) {
		for _, k := range g.Knobs {
			if k.Type == "model" && !valid[k.Filter] {
				t.Errorf("%s: unknown model filter %q", k.Key, k.Filter)
			}
		}
	}
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
		"provider.instances": []any{
			map[string]any{
				"name":    " default ",
				"type":    " OpenAI ",
				"host":    "  http://localhost:11434  ",
				"api_key": "",
			},
		},
		"provider.compaction_model": "big:big-model",
		"provider.summarizer_model": "small-model",
		"provider.skills_model":     "small-model",
		"provider.approvals_model":  "big:big-model",
		"server.port":               float64(9000),
		"compaction.enabled":        false,
		"compaction.threshold":      0.75,
		"compaction.keep_first":     float64(3),
		"http_timeout":              float64(120),
		"logging.file":              "x.log",
		"server.default_workspace":  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(cfg.Provider.Instances) != 1 {
		t.Fatalf("instances: got %d rows, want 1", len(cfg.Provider.Instances))
	}
	inst, ok := cfg.Provider.instance(PrimaryProviderInstance)
	if !ok {
		t.Fatalf("the primary row %q is missing", PrimaryProviderInstance)
	}
	if inst.Type != "openai" {
		t.Errorf("type not trimmed and lowercased: %q", inst.Type)
	}
	if inst.Host != "http://localhost:11434" {
		t.Errorf("host not trimmed: %q", inst.Host)
	}
	if cfg.CompactionModel != "big:big-model" || cfg.SummarizerModel != "small-model" ||
		cfg.SkillsModel != "small-model" || cfg.ApprovalsModel != "big:big-model" {
		t.Errorf("per-subsystem model slots not applied: %q %q %q %q",
			cfg.CompactionModel, cfg.SummarizerModel, cfg.SkillsModel, cfg.ApprovalsModel)
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
	if cfg.Logging.File != "x.log" {
		t.Errorf("logging.file: %q", cfg.Logging.File)
	}
	if cfg.Server.DefaultWorkspace == "" {
		t.Error("default_workspace not applied from an existing absolute dir")
	}
	// an empty value clears the knob (the cwd fallback stands)
	err = applySettingValues(&cfg, map[string]any{"server.default_workspace": ""})
	if err != nil {
		t.Fatalf("apply clear: %v", err)
	}
	if cfg.Server.DefaultWorkspace != "" {
		t.Errorf("default_workspace empty clear: got %q", cfg.Server.DefaultWorkspace)
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
		{"string where number", map[string]any{"http_timeout": "120s"}, "whole number"},
		{"default workspace relative", map[string]any{"server.default_workspace": "rel/path"}, "must be an absolute path"},
		{"default workspace missing", map[string]any{"server.default_workspace": "/omega-definitely-not-here"}, "no such file or directory"},
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
	pctx := &agent.Context{}
	pctx.ApplyOffer("compactor", "test", &compactor.Compactor{})

	payload, err := saveSettings(path, pctx, nil, nil, nil, nil, map[string]any{
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
	if _, err := saveSettings(path, nil, nil, nil, nil, nil, map[string]any{
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

// TestSchemaMasksAPIKey: the providers list rides masked - the plain
// text never reaches the pane, and a missing key stays visibly empty
// (a mask on a missing key would imply one exists).
func TestSchemaMasksAPIKey(t *testing.T) {
	keyOf := func(p *core.SettingsSchema, name string) string {
		for _, g := range p.Groups {
			for _, k := range g.Knobs {
				if k.Key != "provider.instances" {
					continue
				}
				list, _ := k.Value.([]map[string]any)
				for _, row := range list {
					if row["name"] == name {
						got, _ := row["api_key"].(string)
						return got
					}
				}
			}
		}
		t.Errorf("no %q row in the provider.instances knob", name)
		return ""
	}
	path := writeTestConfig(t, testConfigYAML+"  api_key: sk-hunter2\n")
	payload, err := settingsSchema(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := keyOf(payload, PrimaryProviderInstance); got != maskedSecret {
		t.Errorf("GET schema api_key %q, want masked", got)
	}
	// the save-refresh payload rides the same channel: masked too
	refreshed, err := saveSettings(path, nil, nil, nil, nil, nil, map[string]any{
		"compaction.keep_last": float64(4),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := keyOf(refreshed, PrimaryProviderInstance); got != maskedSecret {
		t.Errorf("save-refresh api_key %q, want masked", got)
	}
	// empty stays empty
	path = writeTestConfig(t, testConfigYAML)
	payload, err = settingsSchema(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := keyOf(payload, PrimaryProviderInstance); got != "" {
		t.Errorf("api_key without a stored key %q, want empty", got)
	}
}

func TestHotApplyFlowsToCompactorAndHTTPClient(t *testing.T) {
	comp := &compactor.Compactor{}
	// the files knob hook stub: the files capsule owns the
	// default_workspace knob (the shell-ring shrink wave 1) - the
	// host threads the capsule's SetDefaultWorkspace as a bare func
	// and the hot-apply pushes a save through it.
	pushed := ""
	channelPushed := ""
	terminalPushed := ""
	agentPrompt := ""
	agentTurns := 0
	setFilesWorkspace := func(ws string) { pushed = ws }
	setChannelWorkspace := func(ws string) { channelPushed = ws }
	setTerminalWorkspace := func(ws string) { terminalPushed = ws }
	// t_f78acfd6: the runtime-knob hook - the hot-apply pushes
	// system_prompt + max_turns through it (both agents ride it).
	setAgentConfig := func(prompt string, maxTurns int) { agentPrompt, agentTurns = prompt, maxTurns }
	pctx := &agent.Context{}
	pctx.ApplyOffer("compactor", "test", comp)
	cfg := DefaultConfig()
	cfg.HTTPTimeout = 42
	cfg.Compaction.Enabled = false
	cfg.Compaction.KeepLast = 7
	cfg.Server.DefaultWorkspace = "/tmp/lane"
	cfg.SystemPrompt = "be terse"
	cfg.MaxTurns = 33
	hotApply(cfg, "", pctx, setFilesWorkspace, setChannelWorkspace, setTerminalWorkspace, setAgentConfig)

	if got := llm.HTTPClient().Timeout; got != 42*time.Second {
		t.Errorf("http client timeout %v, want 42s", got)
	}
	if pushed != "/tmp/lane" {
		t.Errorf("SetDefaultWorkspace hook: files knob received %q, want /tmp/lane (the hot-apply must push to the knob hook)", pushed)
	}
	if channelPushed != "/tmp/lane" {
		t.Errorf("SetDefaultWorkspace hook: channel knob received %q, want /tmp/lane (the hot-apply must push to the knob hook)", channelPushed)
	}
	if terminalPushed != "/tmp/lane" {
		t.Errorf("SetDefaultWorkspace hook: terminal knob received %q, want /tmp/lane (the hot-apply must push to the knob hook)", terminalPushed)
	}
	if agentPrompt != "be terse" || agentTurns != 33 {
		t.Errorf("SetAgentConfig hook: prompt %q turns %d, want \"be terse\"/33 (the runtime knobs must ride the hook live)", agentPrompt, agentTurns)
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

	// card t_9825147c part 3: the workspace hook push is gated on a
	// value change - an unchanged knob is NOT re-pushed, a changed one
	// rides both hooks
	pushes := 0
	countingHook := func(string) { pushes++ }
	hotApply(cfg, cfg.Server.DefaultWorkspace, pctx, countingHook, countingHook, countingHook, setAgentConfig)
	if pushes != 0 {
		t.Errorf("unchanged workspace re-pushed %d times, want 0", pushes)
	}
	hotApply(cfg, "/elsewhere", pctx, countingHook, countingHook, countingHook, setAgentConfig)
	if pushes != 3 {
		t.Errorf("changed workspace pushed %d times, want 3 (files + channel + terminal hooks)", pushes)
	}

	// nil hook (no files capsule mounted) and nil context must not panic
	hotApply(cfg, "", pctx, nil, nil, nil, nil)
	hotApply(cfg, "", nil, setFilesWorkspace, nil, nil, nil)
	hotApply(cfg, "", nil, nil, nil, nil, setAgentConfig)
	hotApply(cfg, "", nil, nil, nil, nil, nil)
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

// card t_bf21ee07: the merged provider.instances knob - apply law.
// Every provider is a row and the primary is the row named "default".
// An instance is a NAMED ENDPOINT only: name, type, host, api_key. A
// model_name row is a loud refusal (models come from the instance's
// catalog through the pickers).
func TestApplyProviderInstancesKnob(t *testing.T) {
	inst := func(name, typ string) map[string]any {
		return map[string]any{"name": name, "type": typ, "host": "", "api_key": ""}
	}
	masked := func(name string) map[string]any {
		m := inst(name, "openai")
		m["api_key"] = maskedSecret
		return m
	}
	add := func(cfg *Config, rows ...map[string]any) error {
		list := make([]any, len(rows))
		for i, r := range rows {
			list[i] = r
		}
		return applySettingValues(cfg, map[string]any{"provider.instances": list})
	}
	// newCfg starts from the defaults with a hostless primary row, the
	// shape the pane round-trips on a fresh home.
	newCfg := func() Config {
		cfg := DefaultConfig()
		cfg.Provider.ModelName = "m"
		cfg.Provider.setInstance(ProviderInstance{Name: PrimaryProviderInstance, Type: "ollama"})
		return cfg
	}

	// add a second instance beside the primary: the card's headline
	cfg := newCfg()
	if err := add(&cfg, inst("default", "ollama"), inst("backup", "openai")); err != nil {
		t.Fatalf("add: %v", err)
	}
	if len(cfg.Provider.Instances) != 2 {
		t.Fatalf("instances not applied: %+v", cfg.Provider.Instances)
	}
	backup, ok := cfg.Provider.instance("backup")
	if !ok || backup.APIKey != "" || backup.Type != "openai" {
		t.Fatalf("the added row did not land: %+v", backup)
	}
	// the primary leads the list whatever order was submitted
	if first := cfg.Provider.Instances[0]; first.Name != PrimaryProviderInstance {
		t.Fatalf("list does not lead with %q: %+v", PrimaryProviderInstance, cfg.Provider.Instances)
	}

	// the primary row cannot be renamed away
	if err := add(&cfg, inst("primary", "ollama")); err == nil || !strings.Contains(err.Error(), PrimaryProviderInstance) {
		t.Fatalf("expected the missing-primary refusal, got %v", err)
	}
	// ... nor can the list be emptied: the agent would have no provider
	if err := add(&cfg); err == nil || !strings.Contains(err.Error(), PrimaryProviderInstance) {
		t.Fatalf("expected the empty-list refusal, got %v", err)
	}

	// a model_name row is a loud refusal, never silently dropped
	cfg = newCfg()
	if err := add(&cfg, map[string]any{"name": "default", "type": "ollama", "model_name": "gpt-4o", "host": "", "api_key": ""}); err == nil || !strings.Contains(err.Error(), "no longer settable") {
		t.Fatalf("expected the model_name refusal, got %v", err)
	}

	// save-side validation: a bad type lands in the row and cfg.Validate()
	// (run by saveSettings after apply) refuses it
	cfg = newCfg()
	if err := add(&cfg, inst("default", "azure")); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "not recognized") {
		t.Fatal("expected Validate to refuse the unknown type, got", err)
	}

	// duplicate names fail before anything else
	cfg = newCfg()
	if err := add(&cfg, inst("default", "ollama"), inst("default", "ollama")); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatal("expected duplicated name in the list to fail, got", err)
	}

	// masked round trip: "****" on a NEW row has no stored secret behind
	// it, so the save refuses rather than writing an empty one
	cfg = newCfg()
	if err := add(&cfg, inst("default", "ollama"), masked("backup")); err == nil || !strings.Contains(err.Error(), "masked") {
		t.Fatal("expected masked key on a NEW instance to refuse, got", err)
	}
	// with a stored secret, "****" keeps it
	cfg = newCfg()
	cfg.Provider.setInstance(ProviderInstance{Name: "backup", Type: "openai", APIKey: "sk-kept"})
	if err := add(&cfg, inst("default", "ollama"), masked("backup")); err != nil {
		t.Fatalf("masked round trip refused: %v", err)
	}
	backup, _ = cfg.Provider.instance("backup")
	if backup.APIKey != "sk-kept" {
		t.Fatalf("masked round trip wiped the secret: %q", backup.APIKey)
	}
}

// TestSchemaProviderInstancesKnob: the merge folds every older provider
// shape into ONE list - the flat trio becomes the "default" row, the
// pre-merge top-level providers: map follows in name order - and the
// schema renders that list with the primary first, types lowercase, the
// keys masked and no model column anywhere.
func TestSchemaProviderInstancesKnob(t *testing.T) {
	path := writeTestConfig(t, testConfigYAML+`
  host: http://localhost:11434
providers:
  zeta:
    type: anthropic
    api_key: sk-z
  alpha:
    type: openai
`)
	payload, err := settingsSchema(path)
	if err != nil {
		t.Fatal(err)
	}
	var knob *core.SettingsKnob
	for _, g := range payload.Groups {
		for i, k := range g.Knobs {
			if k.Key == "provider.instances" {
				knob = &g.Knobs[i]
			}
		}
	}
	if knob == nil {
		t.Fatal("no provider.instances knob in schema")
	}
	list, ok := knob.Value.([]map[string]any)
	if !ok || len(list) != 3 {
		t.Fatalf("instances knob value: %v", knob.Value)
	}
	if list[0]["name"] != PrimaryProviderInstance || list[1]["name"] != "alpha" || list[2]["name"] != "zeta" {
		t.Fatalf("rows not primary-first then name order: %v", list)
	}
	if list[0]["host"] != "http://localhost:11434" {
		t.Fatalf("the flat trio did not become the primary row: %v", list[0])
	}
	if list[2]["api_key"] != maskedSecret {
		t.Fatalf("instance key not masked: %v", list[2])
	}
	if list[0]["api_key"] != "" || list[1]["api_key"] != "" {
		t.Fatalf("a missing key must stay empty: %v %v", list[0], list[1])
	}
	for _, row := range list {
		if _, has := row["model_name"]; has {
			t.Fatalf("rows must carry no model column: %v", row)
		}
	}
}

// card t_9825147c part 2: a row's host and api_key are config.yaml's
// property - the settings PUT may keep (masked round trip), rename,
// retype or remove rows, never plant a host config.yaml does not
// carry or set/wipe a secret. Every refusal names config.yaml.
func TestApplyProviderInstancesGate(t *testing.T) {
	row := func(name, host, key string) map[string]any {
		return map[string]any{"name": name, "type": "openai", "host": host, "api_key": key}
	}
	const sharedHost = "http://10.0.0.9:8080"
	stored := func() *Config {
		cfg := DefaultConfig()
		cfg.Provider.Instances = []ProviderInstance{
			{Name: PrimaryProviderInstance, Type: "openai", Host: "", APIKey: "sk-default"},
			{Name: "alpha", Type: "openai", Host: sharedHost, APIKey: "sk-alpha"},
			{Name: "beta", Type: "openai", Host: sharedHost, APIKey: "sk-beta"},
		}
		return &cfg
	}
	primary := row(PrimaryProviderInstance, "", maskedSecret)
	dkey := func(cfg *Config, name string) string {
		inst, _ := cfg.Provider.instance(name)
		return inst.APIKey
	}
	door := func(err error, t *testing.T) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), "config.yaml is the only door") {
			t.Fatalf("expected the config.yaml door refusal, got %v", err)
		}
	}
	apply := func(t *testing.T, cfg *Config, rows ...map[string]any) error {
		t.Helper()
		list := make([]any, len(rows))
		for i, r := range rows {
			list[i] = r
		}
		return applySettingValues(cfg, map[string]any{"provider.instances": list})
	}

	// a changed host on a stored row: refused, config untouched
	cfg := stored()
	if err := apply(t, cfg, primary, row("alpha", "http://evil:9999", maskedSecret)); err == nil {
		t.Fatal("host change accepted")
	} else {
		door(err, t)
	}
	if inst, _ := cfg.Provider.instance("alpha"); inst.Host != sharedHost || inst.APIKey != "sk-alpha" {
		t.Fatalf("failed apply mutated the stored row: %+v", inst)
	}

	// a planted host on a new row: refused even keyless
	if err := apply(t, stored(), primary, row("plant", "http://169.254.1.1:8080", "")); err == nil {
		t.Fatal("planted host accepted")
	} else {
		door(err, t)
	}

	// a secret set on a stored row (plain key): refused
	if err := apply(t, stored(), primary, row("alpha", sharedHost, "sk-new")); err == nil {
		t.Fatal("plain key change accepted")
	} else {
		door(err, t)
	}
	// a secret wipe on a stored row: refused
	if err := apply(t, stored(), primary, row("alpha", sharedHost, "")); err == nil {
		t.Fatal("key wipe accepted")
	} else {
		door(err, t)
	}
	// a secret on a NEW row: refused outright
	if err := apply(t, stored(), primary, row("plant", sharedHost, "sk-fresh")); err == nil {
		t.Fatal("new-row plain key accepted")
	} else {
		door(err, t)
	}

	// the hot lane keeps working: masked round trip, plain echo,
	// rename with key inheritance, retype, removal
	cfg = stored()
	if err := apply(t, cfg,
		primary,                                 // keep the served provider
		row("alpha", sharedHost, maskedSecret),  // keep
		row("beta-r", sharedHost, maskedSecret), // rename, key inherits
		row("gamma", "", ""),                    // keyless loopback row adds
	); err != nil {
		t.Fatalf("name-level edit refused: %v", err)
	}
	if dkey(cfg, PrimaryProviderInstance) != "sk-default" {
		t.Fatalf("masked round trip wiped the primary secret: %q", dkey(cfg, PrimaryProviderInstance))
	}
	if dkey(cfg, "alpha") != "sk-alpha" {
		t.Fatalf("masked round trip wiped the secret: %q", dkey(cfg, "alpha"))
	}
	// beta-r is beta's rename (beta left the list): its masked key
	// inherits beta's secret even though alpha shares the host
	if dkey(cfg, "beta-r") != "sk-beta" {
		t.Fatalf("rename did not inherit the removed row's key: %q", dkey(cfg, "beta-r"))
	}
	if _, has := cfg.Provider.instance("beta"); has {
		t.Fatal("old name beta should be gone after its rename")
	}
	if _, has := cfg.Provider.instance("gamma"); !has {
		t.Fatal("keyless hostless row was not added")
	}
	// the plain echo of the stored key applies unchanged
	cfg = stored()
	if err := apply(t, cfg, primary, row("alpha", sharedHost, "sk-alpha")); err != nil {
		t.Fatalf("identical plain-key echo refused: %v", err)
	}
	if dkey(cfg, "alpha") != "sk-alpha" {
		t.Fatalf("echo changed the secret: %q", dkey(cfg, "alpha"))
	}
}

// TestAuxModelDefaultOptionSet pins WHICH knobs offer the auxiliary
// "default (inherit from the main model)" entry (card t_b403ad88 - it
// widens t_df46c8cf's vision-only pin): every auxiliary model lane
// follows the model the chat runs, EXCEPT the approval gate - an
// approval decision is a deliberate choice, not an inherited default
// (Endo's exception) - and the main model row, which has no default to
// follow (its schema comment above says so). Exact set equality: a new
// model knob has to be a deliberate decision, never a silent
// inheritance.
func TestAuxModelDefaultOptionSet(t *testing.T) {
	cfg := DefaultConfig()
	want := []string{
		"provider.vision_model",
		"provider.title_model",
		"provider.compaction_model",
		"provider.summarizer_model",
		"provider.skills_model",
	}
	flagged := map[string]bool{}
	seen := map[string]bool{}
	for _, g := range settingGroups(&cfg) {
		for _, k := range g.Knobs {
			seen[k.Key] = true
			if k.DefaultOption {
				flagged[k.Key] = true
			}
		}
	}
	for _, key := range want {
		if !flagged[key] {
			t.Errorf("%s does not offer the default entry", key)
		}
	}
	if len(flagged) != len(want) {
		t.Fatalf("default_option knobs = %v, want exactly %v", flagged, want)
	}
	// The exclusions are asserted by NAME: a flag drifting onto either
	// one breaks Endo's exception (approval) or the main row.
	for _, key := range []string{"provider.approvals_model", "provider.model_name"} {
		if !seen[key] {
			t.Fatalf("%s missing from the schema - the exclusion assert went vacuous", key)
		}
		if flagged[key] {
			t.Errorf("%s offers the default entry, want it excluded", key)
		}
	}
}

// The "default" choice persists as the WORD on every auxiliary knob
// that offers it (card t_b403ad88, widening t_df46c8cf's vision pin):
// saving writes <knob>: default to config.yaml - never resolved against
// whatever main model happened to be selected - and the refreshed
// schema reads it back verbatim. The runtime resolves the word at each
// call; the file keeps the intent.
func TestAuxModelDefaultPersistsVerbatim(t *testing.T) {
	cases := []struct {
		knob  string
		field func(Config) string
	}{
		{"provider.vision_model", func(c Config) string { return c.VisionModel }},
		{"provider.title_model", func(c Config) string { return c.TitleModel }},
		{"provider.compaction_model", func(c Config) string { return c.CompactionModel }},
		{"provider.summarizer_model", func(c Config) string { return c.SummarizerModel }},
		{"provider.skills_model", func(c Config) string { return c.SkillsModel }},
	}
	for _, c := range cases {
		t.Run(c.knob, func(t *testing.T) {
			path := writeTestConfig(t, testConfigYAML)
			payload, err := saveSettings(path, nil, nil, nil, nil, nil, map[string]any{
				c.knob: "default",
			})
			if err != nil {
				t.Fatalf("save: %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var saved Config
			if err := yaml.Unmarshal(data, &saved); err != nil {
				t.Fatal(err)
			}
			if got := c.field(saved); got != "default" {
				t.Fatalf("persisted %s = %q, want the literal \"default\"", c.knob, got)
			}
			knob, ok := payload.Knob(c.knob)
			if !ok {
				t.Fatalf("refreshed schema lost the %s knob", c.knob)
			}
			if knob.Value != "default" {
				t.Fatalf("refreshed %s value = %v, want default", c.knob, knob.Value)
			}
		})
	}
}

// --- settings tier 2a (card t_d13c7264): machine-readable Default ------

// knobWire renders the schema as the /settings GET really carries it and
// returns knob key -> the decoded knob object. The defaults are asserted
// HERE, not on the Go struct, because the pane reads the JSON.
func knobWire(t *testing.T, payload *core.SettingsSchema) map[string]map[string]any {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	var wire struct {
		Groups []struct {
			Knobs []map[string]any `json:"knobs"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	out := map[string]map[string]any{}
	for _, g := range wire.Groups {
		for _, k := range g.Knobs {
			key, _ := k["key"].(string)
			out[key] = k
		}
	}
	return out
}

// TestSchemaKnobDefaultRidesTheGET pins three representative knobs on the
// wire: a text default, a numeric knob whose help prose calls 0 "the
// default" (the pseudo-default case), and a live knob. tier 2b's
// modified-dot compares against exactly this field.
func TestSchemaKnobDefaultRidesTheGET(t *testing.T) {
	path := writeTestConfig(t, testConfigYAML)
	payload, err := settingsSchema(path)
	if err != nil {
		t.Fatal(err)
	}
	knobs := knobWire(t, payload)
	for _, want := range []struct {
		key  string
		def  any
		live bool
	}{
		{"store.db_path", "omega.db", false},                  // text default
		{"sessions.summarize_after_hours", float64(24), true}, // "0 = the default (24h)"
		{"compaction.threshold", 0.6, true},                   // live knob
	} {
		knob, ok := knobs[want.key]
		if !ok {
			t.Fatalf("no %s knob on the wire", want.key)
		}
		def, present := knob["default"]
		if !present {
			t.Fatalf("%s: no default key on the wire (omitempty must not ride this field)", want.key)
		}
		if !reflect.DeepEqual(def, want.def) {
			t.Errorf("%s default = %v (%T), want %v", want.key, def, def, want.def)
		}
		if live, _ := knob["live"].(bool); live != want.live {
			t.Errorf("%s live = %v, want %v", want.key, live, want.live)
		}
	}
}

// TestSchemaDefaultsAreTheFreshConfigLayer is the whole-table pin: a
// config.yaml naming only the required model_name leaves every knob at
// its default, so Value must equal Default for all 40 - and the single
// documented omission (provider.model_name, required by Validate) must
// answer JSON null rather than being absent. This fails the moment a
// Default drifts from the config layer it claims to describe.
func TestSchemaDefaultsAreTheFreshConfigLayer(t *testing.T) {
	path := writeTestConfig(t, testConfigYAML)
	payload, err := settingsSchema(path)
	if err != nil {
		t.Fatal(err)
	}
	knobs := knobWire(t, payload)
	if len(knobs) != len(settingsKnobKeys(t)) {
		t.Fatalf("schema carries %d knobs, inventory says %d", len(knobs), len(settingsKnobKeys(t)))
	}
	omitted := []string{}
	for key, knob := range knobs {
		def, present := knob["default"]
		if !present {
			t.Fatalf("%s: default key missing from the wire", key)
		}
		if def == nil {
			omitted = append(omitted, key)
			continue
		}
		if !reflect.DeepEqual(knob["value"], def) {
			t.Errorf("%s: value %v != default %v on a config naming no key", key, knob["value"], def)
		}
	}
	if len(omitted) != 1 || omitted[0] != "provider.model_name" {
		t.Errorf("knobs without a default = %v, want exactly [provider.model_name]", omitted)
	}
}

// recordingRunTimeout is the cron capsule's offered face as the host
// sees it: the consumer-owned SetRunTimeout the hot-apply resolves.
type recordingRunTimeout struct {
	got    time.Duration
	pushed bool
}

func (rr *recordingRunTimeout) SetRunTimeout(d time.Duration) {
	rr.got, rr.pushed = d, true
}

// TestHotApplyPushesCronRunTimeout pins the host half of card
// t_e635df3a: every settings save/reload hands the cron scheduler the
// server.run_timeout THIS host resolved from the config it booted on
// (an explicit --config path included), so a fire's wall-clock cap
// follows the operator's knob instead of the module's own home guess.
// The capsule is reached through its offered seam - never an import.
func TestHotApplyPushesCronRunTimeout(t *testing.T) {
	stub := &recordingRunTimeout{}
	pctx := &agent.Context{}
	pctx.ApplyOffer("cron", "test", stub)
	cfg := DefaultConfig()
	cfg.Server.RunTimeout = 120
	hotApply(cfg, cfg.Server.DefaultWorkspace, pctx, nil, nil, nil, nil)
	if !stub.pushed {
		t.Fatalf("cron SetRunTimeout never called: the fire would fall back to its own home-candidate read")
	}
	if stub.got != 120*time.Second {
		t.Fatalf("pushed run timeout = %s, want the config's 120s", stub.got)
	}
	// A second save pushes the NEW value, not the first one.
	stub.pushed = false
	cfg.Server.RunTimeout = 45
	hotApply(cfg, cfg.Server.DefaultWorkspace, pctx, nil, nil, nil, nil)
	if !stub.pushed || stub.got != 45*time.Second {
		t.Fatalf("re-push = %s (called %v), want 45s", stub.got, stub.pushed)
	}
	// An unmounted cron capsule (or a disabled module) is a no-op: no
	// panic, and the fire keeps its own read.
	hotApply(cfg, cfg.Server.DefaultWorkspace, &agent.Context{}, nil, nil, nil, nil)
}
