package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/approval"
	"github.com/EndoTheDev/omega/harness/compactor"
	"github.com/EndoTheDev/omega/harness/provider"
	"github.com/EndoTheDev/omega/harness/store"
	"github.com/EndoTheDev/omega/llm"
	"gopkg.in/yaml.v3"
)

// settings.go exposes the config.yaml knobs to the webui settings pane over the. The
// schema here is the single source of truth: one entry per Config field, grouped in
// config-file order — no new config keys, no duplicates.

// The knob/group shapes are core.SettingsKnob / core.SettingsGroup /
// core.SettingsSchema - the seam is typed end to end, so cmd/omega and http_channel
// cannot drift apart on the wire shape. maskedSecret stands in for a secret knob's
// value everywhere the schema is served (GET and save refreshes alike): the pane treats
// api_key as write-only (diff-only saves), so the plain text never rides the wire.
const maskedSecret = "****"

// maskSecret renders a secret knob's value for the wire.
func maskSecret(v string) any {
	if v == "" {
		return ""
	}
	return maskedSecret
}

// settingsSchema loads the live config and renders it as groups of
// knobs. Values come from the same LoadConfig path the server booted
// with (file + env overrides + defaults), so the pane always shows the
// effective values.
func settingsSchema(path string) (*core.SettingsSchema, error) {
	return settingsPayload(path, nil)
}

// settingsPayload renders the whole GET payload: the knob groups plus the agent scopes.
// runtimeMaxTurns is the serving agents' LIVE turn cap; nil means no serving agent (the
// CLI, a save refresh) and the loaded config's own value stands in - after a save the
// hot-apply has just pushed exactly that value, so the two lanes agree there by
// construction.
func settingsPayload(path string, runtimeMaxTurns func() int) (*core.SettingsSchema, error) {
	cfg, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}
	// the whole payload is headed for the unauthenticated /settings GET
	return &core.SettingsSchema{
		Groups: settingGroups(&cfg),
		Scopes: settingsScopes(settingsScopeHome(path), &cfg, runtimeMaxTurns),
	}, nil
}

// settingsScopeHome anchors the agent-scope scan to the config file.
func settingsScopeHome(configPath string) string {
	dir := filepath.Dir(configPath)
	if filepath.Base(dir) == "config" {
		return filepath.Dir(dir)
	}
	return dir
}

// scopeGroupKeys is the R2 vocabulary in render order.
var scopeGroupKeys = []string{"provider", "system_prompt", "description", "max_turns"}

// settingsScopes renders one scope per agent home under <home>/agents/. The scope NAMES
// come from agent.LoadProfiles - the roster scan the agents tab already reads, so the
// two surfaces can never disagree on which dirs are agents (main's home is the root, so
// it is never a scope; dot/underscore and malformed dirs are skipped there and here).
// R8 empty world: no agent dirs = an empty, non-nil list.
func settingsScopes(home string, cfg *Config, runtimeMaxTurns func() int) []core.SettingsScope {
	scopes := []core.SettingsScope{}
	if home == "" || cfg == nil {
		return scopes
	}
	inheritedTurns := cfg.MaxTurns
	if runtimeMaxTurns != nil {
		inheritedTurns = runtimeMaxTurns()
	}
	for _, p := range agent.LoadProfiles(home) {
		if p.Name == agent.DefaultAgentName {
			continue // main's home is the home root - not a scope (R5)
		}
		lay, err := agent.ReadAgentConfigLayer(home, p.Name)
		scope := core.SettingsScope{
			Name:   p.Name,
			Groups: scopeGroups(cfg, lay, inheritedTurns),
		}
		if err != nil {
			// The dir IS a scope (R8: no special-casing), so it rides with inherited values.
			scope.Error = err.Error()
		}
		scopes = append(scopes, scope)
	}
	return scopes
}

// scopeGroups renders the four groups for one scope. A knob whose layer entry is absent
// inherits the main value and answers overridden=false; a knob the layer NAMES - even
// with an empty value - answers the layer's value and overridden=true (the write lane
// stores an empty string as an override, so the flag must agree with it).
func scopeGroups(cfg *Config, lay agent.AgentConfigLayer, inheritedTurns int) []core.SettingsScopeGroup {
	value := func(key string, own agent.LayerValue, inherited string) core.SettingsScopeKnob {
		if own.Set {
			return core.SettingsScopeKnob{Key: key, Value: own.Value, Overridden: true}
		}
		return core.SettingsScopeKnob{Key: key, Value: inherited}
	}
	provider := []core.SettingsScopeKnob{
		// The secret-free provider law: an agent names an INSTANCE, and the type, host and
		// api_key live in settings exactly once. The scope payload therefore shows two knobs,
		// never a secret.
		value("provider.instance", lay.ProviderInstance, agent.DefaultProviderInstance),
		value("provider.model_name", lay.ModelName, cfg.Provider.ModelName),
	}
	turns := core.SettingsScopeKnob{Key: "max_turns", Value: inheritedTurns}
	if lay.MaxTurns != nil {
		turns = core.SettingsScopeKnob{Key: "max_turns", Value: *lay.MaxTurns, Overridden: true}
	}
	return []core.SettingsScopeGroup{
		{Key: scopeGroupKeys[0], Overridden: anyOverridden(provider), Knobs: provider},
		{Key: scopeGroupKeys[1], Overridden: lay.SystemPrompt.Set, Knobs: []core.SettingsScopeKnob{
			value("system_prompt", lay.SystemPrompt, cfg.SystemPrompt),
		}},
		{Key: scopeGroupKeys[2], Overridden: lay.Description.Set, Knobs: []core.SettingsScopeKnob{
			value("description", lay.Description, ""),
		}},
		{Key: scopeGroupKeys[3], Overridden: turns.Overridden, Knobs: []core.SettingsScopeKnob{turns}},
	}
}

// anyOverridden is the group roll-up.
func anyOverridden(knobs []core.SettingsScopeKnob) bool {
	for _, k := range knobs {
		if k.Overridden {
			return true
		}
	}
	return false
}

// defaultConfig returns the config layer BENEATH the file: the built-in defaults
// (DefaultConfig) with the OMEGA_* environment overrides applied - exactly the value
// LoadConfig gives a knob whose key config.yaml does not name. The file itself is
// deliberately excluded: writeConfigFile marshals the whole Config, so one settings
// save materializes EVERY key, and a file-derived "default" would equal every current
// value - the settings pane's modified-dot could never light. Every knob's Default
// reads a field of this struct, never a hand-typed literal, so a Config rename breaks
// the build instead of silently drifting from the schema.
func defaultConfig() Config {
	cfg := DefaultConfig()
	applyEnv(&cfg)
	return cfg
}

// settingGroups builds the full knob inventory from a config.
func settingGroups(cfg *Config) []core.SettingsGroup {
	def := defaultConfig()
	return []core.SettingsGroup{
		{
			Key:   "provider",
			Label: "provider",
			Knobs: []core.SettingsKnob{
				// the connection subgroup and the instances subgroup MERGED.
				{Key: "provider.instances", Label: "provider instances", Type: "providers",
					Value: providerInstancesKnob(cfg),
					// The default is the same single row rendered through the same function, so the
					// shapes (and the modified-dot comparison) cannot drift.
					Default: providerInstancesKnob(&def), Live: true,
					Help: "every provider is a named instance: the name is the instance id the model pickers and slots speak (instance:model), so two accounts of one type are two rows; the row named \"default\" is the provider the agent serves. Rows can be added, renamed, retyped and removed here; a row's base url and api key are config.yaml-only, and changes apply live"},
				{Key: "provider.model_name", Label: "model name", Type: "model",
					Value: cfg.Provider.ModelName,
					Help: "default model the agent serves; the webui model picker overrides per chat; applies on restart"},
				{Key: "provider.vision_model", Label: "vision model", Type: "model", Filter: "vision", DefaultOption: true,
					Value:   cfg.VisionModel,
					Default: def.VisionModel, Live: true,
					Help: "auxiliary captioner model for image messages; \"default\" follows the model the current chat runs (the composer's chip), resolved per call - useful only when that model can see images (the caption hop exists for a blind main model, so \"default\" on a blind one sends the image there and the run fails on the caption); the dropdown lists vision-capable models only; empty = images reach the main model unchanged and may fail loudly; applies live"},
				{Key: "provider.title_model", Label: "title model", Type: "model", DefaultOption: true,
					Value:   cfg.TitleModel,
					Default: def.TitleModel, Live: true,
					Help: "cheap model for session titles; empty or \"default\" = the model this chat runs (resolved per call); applies live"},
				{Key: "provider.compaction_model", Label: "compaction model", Type: "model", Slot: true, DefaultOption: true,
					Value:   cfg.CompactionModel,
					Default: def.CompactionModel, Live: true,
					Help: "model the compaction summarizer consults; instance:model rides a named provider instance (empty or \"default\" = the session provider model, resolved per call); applies live"},
				{Key: "provider.summarizer_model", Label: "summarizer model", Type: "model", Slot: true, DefaultOption: true,
					Value:   cfg.SummarizerModel,
					Default: def.SummarizerModel, Live: true,
					Help: "model the hygiene job consults for session summaries; instance:model rides a named provider instance (empty or \"default\" = the session provider model, resolved per call); applies live - the next sweep consults it (the report names the slot)"},
				{Key: "provider.skills_model", Label: "skills curator model", Type: "model", Slot: true, DefaultOption: true,
					Value:   cfg.SkillsModel,
					Default: def.SkillsModel, Live: true,
					Help: "model the weekly skills curator consults for merge/distill proposals; instance:model rides a named provider instance (empty or \"default\" = the session provider model, resolved per call); applies live - the next curator action consults it"},
				{Key: "provider.approvals_model", Label: "approval decision model", Type: "model", Slot: true, Filter: "decision",
					Value:   cfg.ApprovalsModel,
					Default: def.ApprovalsModel, Live: true,
					Help: "decision model the smart approval gate consults; instance:model routes the consult through a named provider instance (empty = fail-closed, every gated command escalates); applies live"},
			},
		},
		{
			Key:   "server",
			Label: "server",
			Knobs: []core.SettingsKnob{
				{Key: "server.port", Label: "port", Type: "number",
					Min: ptrF(1), Max: ptrF(math.MaxUint16), Step: ptrF(1),
					Value:   cfg.Server.Port,
					Default: def.Server.Port,
					Help:    "HTTP channel port; applies on restart"},
				{Key: "server.bind", Label: "bind addresses", Type: "bindlist",
					Value: strings.Join(cfg.Server.Bind, ", "),
					Default: strings.Join(def.Server.Bind, ", "),
					Help:    "comma-separated listen addresses; loopback only by default (unauthenticated API); edit config.yaml instead (applies on restart)"},
				{Key: "server.run_timeout", Label: "run timeout (s)", Type: "number",
					Min: ptrF(1), Step: ptrF(1),
					Value:   cfg.Server.RunTimeout,
					Default: def.Server.RunTimeout,
					Help:    "wall-clock cap per detached chat run and per cron fire, in seconds; a chat run takes a change on restart, a cron fire on its next fire"},
				{Key: "server.default_workspace", Label: "default workspace", Type: "text",
					Value: cfg.Server.DefaultWorkspace, Live: true,
					Default: def.Server.DefaultWorkspace,
					Help:    "absolute path; the Files tab roots here when the focused session has no pinned workspace, and a workspace-less session's tool runs (shell, files, processes) root here instead of the process working directory; empty = the process working directory; applies live"},
			},
		},
		{
			Key:   "store",
			Label: "store",
			Knobs: []core.SettingsKnob{
				{Key: "store.db_path", Label: "database path", Type: "text",
					Value:   cfg.Store.DBPath,
					Default: def.Store.DBPath,
					Help:    "SQLite file, relative to the working dir; applies on restart"},
			},
		},
		{
			Key:   "sessions",
			Label: "sessions",
			Knobs: []core.SettingsKnob{
				{Key: "sessions.retention_days", Label: "retention (days)", Type: "number",
					Min: ptrF(0), Step: ptrF(1),
					Value: cfg.Sessions.RetentionDays, Live: true,
					Default: def.Sessions.RetentionDays,
					Help:    "the hygiene job deletes sessions inactive longer than this many days (pinned sessions are exempt); 0 = never prune; applies live - the section rides the same hot-apply push as summarizer_model"},
				{Key: "sessions.summarize_after_hours", Label: "summarize after (h)", Type: "number",
					Min: ptrF(0), Step: ptrF(1),
					Value: cfg.Sessions.SummarizeAfterHours, Live: true,
					Default: def.Sessions.SummarizeAfterHours,
					Help:    "the hygiene job summarizes settled sessions that lack a summary once they are idle this many hours; 0 = the default (24h); applies live - the section rides the same hot-apply push as summarizer_model"},
				// The summarizer's model moved to provider settings: provider.summarizer_model.
			},
		},
		{
			Key:   "compaction",
			Label: "compaction",
			Knobs: compactionKnobs(cfg, &def),
		},
		{
			Key:   "runtime",
			Label: "runtime",
			Knobs: []core.SettingsKnob{
				{Key: "system_prompt", Label: "system prompt", Type: "textarea",
					Value: cfg.SystemPrompt, Live: true,
					Default: def.SystemPrompt,
					Help:    "extra custom prompt lines appended to the system prompt; applies live - the next run reads it"},
				{Key: "http_timeout", Label: "http timeout (s)", Type: "number",
					Min: ptrF(1), Step: ptrF(1),
					Value:   cfg.HTTPTimeout,
					Default: def.HTTPTimeout,
					Help:    "LLM request timeout in seconds", Live: true},
				{Key: "max_turns", Label: "max turns", Type: "number",
					Min: ptrF(0), Step: ptrF(1),
					Value: cfg.MaxTurns, Live: true,
					Default: def.MaxTurns,
					Help:    "tool-loop turn cap per chat; applies live - the next run reads it (0 = the 100 default)"},
			},
		},
		{
			Key:   "skills",
			Label: "skills",
			Knobs: []core.SettingsKnob{
				{Key: "skills.dir", Label: "skills directory", Type: "text",
					Value:   cfg.Skills.Dir,
					Default: def.Skills.Dir,
					Help:    "loaded at startup; applies on restart"},
				{Key: "skills.nudge_interval", Label: "nudge interval", Type: "number",
					Min: ptrF(0), Step: ptrF(1),
					Value: cfg.Skills.NudgeInterval,
					Default: def.Skills.NudgeInterval,
					Help:    "learning-loop reminder cadence: tool iterations without a persisted lesson before the next turn prep carries one reminder line; 0 = off; applies on restart"},
				// The curator's model moved to provider settings: provider.skills_model.
			},
		},
		{
			Key:   "memory",
			Label: "memory",
			Knobs: []core.SettingsKnob{
				{Key: "memory.enabled", Label: "enabled", Type: "toggle",
					Value:   cfg.Memory.Enabled,
					Default: def.Memory.Enabled,
					Help:    "persistent memory system; applies on restart"},
				{Key: "memory.user_profile_enabled", Label: "user profile enabled", Type: "toggle",
					Value:   cfg.Memory.UserProfileEnabled,
					Default: def.Memory.UserProfileEnabled,
					Help:    "user-profile memory file; applies on restart"},
				{Key: "memory.char_limit", Label: "char limit", Type: "number",
					Min: ptrF(1), Step: ptrF(1),
					Value:   cfg.Memory.CharLimit,
					Default: def.Memory.CharLimit,
					Help:    "memory file budget in characters; applies on restart"},
				{Key: "memory.user_char_limit", Label: "user profile char limit", Type: "number",
					Min: ptrF(1), Step: ptrF(1),
					Value:   cfg.Memory.UserProfileCharLimit,
					Default: def.Memory.UserProfileCharLimit,
					Help:    "user-profile budget in characters; applies on restart"},
				{Key: "memory.file", Label: "memory file", Type: "text",
					Value:   cfg.Memory.File,
					Default: def.Memory.File, Help: "applies on restart"},
				{Key: "memory.user_file", Label: "user profile file", Type: "text",
					Value:   cfg.Memory.UserProfileFile,
					Default: def.Memory.UserProfileFile, Help: "applies on restart"},
			},
		},
		{
			Key:   "logging",
			Label: "logging",
			Knobs: []core.SettingsKnob{
				{Key: "logging.enabled", Label: "enabled", Type: "toggle",
					Value:   cfg.Logging.Enabled,
					Default: def.Logging.Enabled,
					Help:    "operational log; applies on restart"},
				{Key: "logging.file", Label: "log file", Type: "text",
					Value: cfg.Logging.File,
					Default: def.Logging.File,
					Help:    "applies on restart"},
			},
		},
		{
			Key:   "approvals",
			Label: "approvals",
			Knobs: []core.SettingsKnob{
				{Key: "approvals.mode", Label: "mode", Type: "select",
					Options: []string{"off", "manual", "smart"},
					Value:   cfg.Approvals.Mode, Live: true,
					Default: def.Approvals.Mode,
					Help:    "off = no gate at all; manual = flagged commands ask you (default); smart = a decision model reviews them first; applies live for mounted gates (off at boot keeps the gate unmounted)"},
				// The decision model moved to provider settings: provider.approvals_model.
				{Key: "approvals.smart_policy", Label: "operator policy", Type: "textarea",
					Value: cfg.Approvals.SmartPolicy, Live: true,
					Default: def.Approvals.SmartPolicy,
					Help:    "trusted text the reviewer reads beside every command: describe routine operations that should not interrupt you; applies live; a policy edit also re-keys the gate cache"},
				// The safeagents detector set's rollout (card t_e3cd4381).
				// New detectors ship in the log-only soak: findings land
				// in the audit log as WOULD-BLOCK and no call is touched
				// until a real-run soak shows no false-positive pile.
				{Key: "approvals.monitor", Label: "new detectors", Type: "select",
					Options: []string{approval.MonitorLogOnly, approval.MonitorEnforce},
					Value:   cfg.Approvals.Monitor, Live: true,
					Default: def.Approvals.Monitor,
					Help:    "log-only = host-provenance and reach detectors report would-block in the audit log without blocking (the soak); enforce = severity tiers decide: high blocks, milder tiers ask you"},
				{Key: "approvals.judge_fail_closed", Label: "judge fails closed", Type: "toggle",
					Value: cfg.Approvals.JudgeFailClosed, Live: true,
					Default: def.Approvals.JudgeFailClosed,
					Help:    "the evidence-only judge unreachable: off = it adds nothing and your card still decides (fail open); on = the call is refused"},
			},
		},
	}
}

// compactionKnobs renders the agent.CompactionConfig section.
func compactionKnobs(cfg, def *Config) []core.SettingsKnob {
	c := &cfg.Compaction
	d := &def.Compaction
	return []core.SettingsKnob{
		{Key: "compaction.enabled", Label: "enabled", Type: "toggle",
			Value:   c.Enabled,
			Default: d.Enabled,
			Help:    "summarize history when the token budget is exceeded", Live: true},
		{Key: "compaction.threshold", Label: "threshold", Type: "number",
			Min: ptrF(0.05), Max: ptrF(1), Step: ptrF(0.05),
			Value:   c.Threshold,
			Default: d.Threshold,
			Help:    "fraction of the context window that triggers compaction", Live: true},
		{Key: "compaction.context_window", Label: "context window", Type: "number",
			Min: ptrF(0), Step: ptrF(1),
			Value: c.ContextWindow,
			Default: d.ContextWindow,
			Help:    "model context window in tokens the budget is measured against; 0 = auto (probed from the provider at startup)", Live: true},
		{Key: "compaction.keep_first", Label: "keep first", Type: "number",
			Min: ptrF(0), Step: ptrF(1),
			Value:   c.KeepFirst,
			Default: d.KeepFirst,
			Help:    "messages kept unsummarized at the head", Live: true},
		{Key: "compaction.keep_last", Label: "keep last", Type: "number",
			Min: ptrF(0), Step: ptrF(1),
			Value:   c.KeepLast,
			Default: d.KeepLast,
			Help:    "messages kept unsummarized at the tail", Live: true},
		{Key: "compaction.reserve_tokens", Label: "reserve tokens", Type: "number",
			Min: ptrF(1), Step: ptrF(1),
			Value:   c.ReserveTokens,
			Default: d.ReserveTokens,
			Help:    "tokens reserved for the summary response", Live: true},
		{Key: "compaction.max_tool_output", Label: "max tool output", Type: "number",
			Min: ptrF(1), Step: ptrF(1),
			Value:   c.MaxToolOutput,
			Default: d.MaxToolOutput,
			Help:    "tool-result truncation length in characters; applies on restart"},
	}
}

func ptrF(n float64) *float64 { return &n }

// providerInstancesKnob renders the provider instances as the list editor's value. The
// list order is the config.yaml order (the primary first), so the pane never reshuffles
// its rows between reloads.
func providerInstancesKnob(cfg *Config) []map[string]any {
	out := make([]map[string]any, 0, len(cfg.Provider.Instances))
	for _, inst := range cfg.Provider.Instances {
		out = append(out, map[string]any{
			"name":    inst.Name,
			"type":    strings.ToLower(inst.Type),
			"host":    inst.Host,
			"api_key": maskSecret(inst.APIKey),
		})
	}
	return out
}

// saveSettings applies values (dotted yaml path -> value), validates, and writes.
func saveSettings(path string, pctx *agent.Context, setFilesWorkspace func(string), setChannelWorkspace func(string), setTerminalWorkspace func(string), setAgentConfig func(string, int), values map[string]any) (*core.SettingsSchema, error) {
	cfg, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}
	prevWorkspace := cfg.Server.DefaultWorkspace
	if err := applySettingValues(&cfg, values); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if err := writeConfigFile(path, &cfg); err != nil {
		return nil, err
	}
	hotApply(cfg, prevWorkspace, pctx, setFilesWorkspace, setChannelWorkspace, setTerminalWorkspace, setAgentConfig)
	// the refreshed payload carries the scopes too, or the pane would lose its agent. nil
	// runtime getter: the hot-apply above just pushed cfg.MaxTurns onto the serving
	// agents, so the file lane IS the runtime lane for this response.
	return &core.SettingsSchema{
		Groups: settingGroups(&cfg),
		Scopes: settingsScopes(settingsScopeHome(path), &cfg, nil),
	}, nil
}

// applySettingValues routes each submitted value into the Config by
// dotted yaml path. Unknown paths are rejected — the schema is the
// contract, typos must fail, not silently do nothing.
func applySettingValues(cfg *Config, values map[string]any) error {
	for key, raw := range values {
		var err error
		switch key {
		case "provider.model_name", "provider.vision_model", "provider.title_model":
			err = setStringField(cfg, key, raw)
		case "provider.instances":
			// The provider instances: the ONE list every provider lives in, the primary row.
			err = applyProviderInstances(cfg, key, raw)
		case "provider.api_key", "server.bind":
			// gated knobs: bind is the escalation lever for the whole LAN surface, api_key is.
			err = fmt.Errorf("%s: config.yaml is the only door for this knob - not settable over the API", key)
		case "server.port":
			err = setInt(&cfg.Server.Port, key, raw, 1, math.MaxUint16)
		case "server.run_timeout":
			err = setInt(&cfg.Server.RunTimeout, key, raw, 1, math.MaxInt32)
		case "server.default_workspace":
			// empty clears (the law's cwd fallback stands); anything else must be absolute and
			// an existing directory - the same shape a session workspace pin validates,
			// validated HERE so a typo never silently roots nothing.
			v, verr := asTrimmedString(raw)
			if verr != nil {
				err = fmt.Errorf("%s: %w", key, verr)
				break
			}
			if v == "" {
				cfg.Server.DefaultWorkspace = ""
				break
			}
			if !filepath.IsAbs(v) && !strings.HasPrefix(v, "/") && !strings.HasPrefix(v, `\`) {
				err = fmt.Errorf("%s: must be an absolute path, got %q", key, v)
				break
			}
			abs, aerr := filepath.EvalSymlinks(filepath.Clean(v))
			if aerr != nil {
				// One not-exist message on every OS: the windows stat answers "The system cannot.
				if os.IsNotExist(aerr) {
					err = fmt.Errorf("%s: no such file or directory: %s", key, v)
				} else {
					err = fmt.Errorf("%s: %w", key, aerr)
				}
				break
			}
			info, serr2 := os.Stat(abs)
			if serr2 != nil {
				err = fmt.Errorf("%s: %w", key, serr2)
				break
			}
			if !info.IsDir() {
				err = fmt.Errorf("%s: not a directory: %s", key, abs)
				break
			}
			cfg.Server.DefaultWorkspace = canonPath(abs)
		case "store.db_path", "system_prompt", "theme",
			"skills.dir", "memory.file", "memory.user_file", "logging.file":
			err = setStringField(cfg, key, raw)
		case "skills.nudge_interval":
			err = setInt(&cfg.Skills.NudgeInterval, key, raw, 0, math.MaxInt32) // 0 = off
		case "sessions.retention_days":
			err = setInt(&cfg.Sessions.RetentionDays, key, raw, 0, math.MaxInt32) // 0 = never prune
		case "sessions.summarize_after_hours":
			err = setInt(&cfg.Sessions.SummarizeAfterHours, key, raw, 0, math.MaxInt32) // 0 = the default
		case "compaction.enabled", "memory.enabled", "memory.user_profile_enabled",
			"logging.enabled", "approvals.judge_fail_closed":
			err = setBool(cfg, key, raw)
		case "compaction.threshold":
			err = setFloat(&cfg.Compaction.Threshold, key, raw, 0.05, 1)
		case "compaction.context_window":
			err = setInt(&cfg.Compaction.ContextWindow, key, raw, 0, math.MaxInt32) // 0 = auto
		case "compaction.keep_first":
			err = setInt(&cfg.Compaction.KeepFirst, key, raw, 0, math.MaxInt32)
		case "compaction.keep_last":
			err = setInt(&cfg.Compaction.KeepLast, key, raw, 0, math.MaxInt32)
		case "compaction.reserve_tokens":
			err = setInt(&cfg.Compaction.ReserveTokens, key, raw, 1, math.MaxInt32)
		case "compaction.max_tool_output":
			err = setInt(&cfg.Compaction.MaxToolOutput, key, raw, 1, math.MaxInt32)
		case "http_timeout":
			err = setInt(&cfg.HTTPTimeout, key, raw, 1, math.MaxInt32)
		case "max_turns":
			err = setInt(&cfg.MaxTurns, key, raw, 0, math.MaxInt32)
		case "memory.char_limit":
			err = setInt(&cfg.Memory.CharLimit, key, raw, 1, math.MaxInt32)
		case "memory.user_char_limit":
			err = setInt(&cfg.Memory.UserProfileCharLimit, key, raw, 1, math.MaxInt32)
		case "approvals.mode":
			var m string
			if err = setString(&m, key, raw, trim); err != nil {
				return err
			}
			if !approval.ValidMode(m) {
				return fmt.Errorf("%s: must be off, manual, or smart, got %q", key, m)
			}
			cfg.Approvals.Mode = m
		case "approvals.smart_policy":
			err = setStringField(cfg, key, raw)
		case "approvals.monitor":
			var m string
			if err = setString(&m, key, raw, trim); err != nil {
				return err
			}
			if !approval.ValidMonitor(m) {
				return fmt.Errorf("%s: must be log-only or enforce, got %q", key, m)
			}
			cfg.Approvals.Monitor = m
		case "provider.compaction_model", "provider.summarizer_model",
			"provider.skills_model", "provider.approvals_model":
			// the per-subsystem model slots live in the provider group.
			err = setStringField(cfg, key, raw)
		default:
			err = fmt.Errorf("unknown setting: %s", key)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// setStringField applies a plain string knob, dispatching on the path.
// Small and total: every text path the schema advertises is handled
// here, so the schema and the setter can never drift apart silently.
func setStringField(cfg *Config, key string, raw any) error {
	v, err := asTrimmedString(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	switch key {
	case "provider.model_name":
		cfg.Provider.ModelName = v
	case "provider.vision_model":
		cfg.VisionModel = v
	case "provider.title_model":
		cfg.TitleModel = v
	case "provider.compaction_model":
		cfg.CompactionModel = v
	case "provider.summarizer_model":
		cfg.SummarizerModel = v
	case "provider.skills_model":
		cfg.SkillsModel = v
	case "provider.approvals_model":
		cfg.ApprovalsModel = v
	case "store.db_path":
		cfg.Store.DBPath = v
	case "system_prompt":
		cfg.SystemPrompt = v
	case "skills.dir":
		cfg.Skills.Dir = v
	case "memory.file":
		cfg.Memory.File = v
	case "memory.user_file":
		cfg.Memory.UserProfileFile = v
	case "logging.file":
		cfg.Logging.File = v
	case "approvals.smart_policy":
		cfg.Approvals.SmartPolicy = v
	default:
		return fmt.Errorf("unknown setting: %s", key)
	}
	return nil
}

// trim normalizes plain string inputs: surrounding whitespace is never significant.
func trim(s string) string { return strings.TrimSpace(s) }

// applyProviderInstances applies the instances knob. The PRIMARY is the row named
// "default": the list must carry it (a list without it would leave the agent with no
// provider to serve), and it can be retyped like any row but not renamed away. A masked
// api_key keeps the entry's EXISTING secret ("****" means unchanged, not blank - the
// plain text never rides the wire, so the round trip must not wipe it). Unknown types,
// illegal names and duplicate ids fail through apply + cfg.Validate in saveSettings -
// the schema is the contract and a typo must be loud, never silently dropped.
func applyProviderInstances(cfg *Config, key string, raw any) error {
	list, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("%s: expected a list of instance objects, got %T", key, raw)
	}
	instancesDoor := func(name, field string) error {
		return fmt.Errorf("%s: %s.%s: config.yaml is the only door for instance %s - not settable over the API", key, name, field, field)
	}
	// the submitted names, pre-computed in full.
	sent := make(map[string]bool, len(list))
	for _, item := range list {
		if obj, ok := item.(map[string]any); ok {
			if n, e := asTrimmedString(obj["name"]); e == nil && n != "" {
				sent[n] = true
			}
		}
	}
	instances := make([]ProviderInstance, 0, len(list))
	seen := make(map[string]bool, len(list))
	for i, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: entry %d: expected an object, got %T", key, i, item)
		}
		name, err := asTrimmedString(obj["name"])
		if err != nil {
			return fmt.Errorf("%s: entry %d: name: %w", key, i, err)
		}
		if name == "" {
			return fmt.Errorf("%s: entry %d: name is required", key, i)
		}
		if seen[name] {
			return fmt.Errorf("%s: entry %d: duplicate name %q", key, i, name)
		}
		seen[name] = true
		if v, err := asTrimmedString(obj["model_name"]); err == nil && v != "" {
			return fmt.Errorf("%s: %s.model_name is no longer settable - providers are named endpoints only, pick the model in the model picker (instance:model)", key, name)
		}
		inst := ProviderInstance{Name: name}
		if inst.Type, err = asTrimmedString(obj["type"]); err != nil {
			return fmt.Errorf("%s: %s.type: %w", key, name, err)
		}
		inst.Type = strings.ToLower(inst.Type)
		if inst.Host, err = asTrimmedString(obj["host"]); err != nil {
			return fmt.Errorf("%s: %s.host: %w", key, name, err)
		}
		if inst.APIKey, err = asTrimmedString(obj["api_key"]); err != nil {
			return fmt.Errorf("%s: %s.api_key: %w", key, name, err)
		}
		// host gate: a stored row keeps its endpoint; a new row is either hostless (the
		// provider default, loopback) or carries a host config.yaml already serves - that is
		// a rename of a stored row, never a planted endpoint reaching a fresh network target.
		existing, has := cfg.Provider.instance(name)
		if has {
			if inst.Host != existing.Host {
				return instancesDoor(name, "host")
			}
		} else if inst.Host != "" {
			if !storedHostIn(cfg, inst.Host) {
				return instancesDoor(name, "host")
			}
		}
		switch {
		case inst.APIKey == maskedSecret:
			// "****" = keep the EXISTING secret. On a known name that is the row's own key; on a
			// rename it inherits the submitted host's stored key - exactly one source, else the
			// secret is config.yaml's to name.
			if has {
				if existing.APIKey == "" {
					return fmt.Errorf("%s: %s.api_key is masked but the stored instance has no key - set a real key in config.yaml", key, name)
				}
				inst.APIKey = existing.APIKey
			} else {
				inherited, distinct := renamedOffKey(cfg, sent, inst.Host)
				switch {
				case distinct == 0:
					return fmt.Errorf("%s: %s.api_key is masked - no removed instance keeps a secret at that host; set a real key in config.yaml", key, name)
				case distinct > 1:
					return fmt.Errorf("%s: %s.api_key is masked and %d removed instances keep secrets at that host - the source is ambiguous; edit config.yaml", key, name, distinct)
				default:
					inst.APIKey = inherited
				}
			}
		case inst.APIKey == "":
			if has && existing.APIKey != "" {
				return instancesDoor(name, "api_key")
			}
		default:
			// a plain key: an identical echo of the stored secret is a no-op, anything else is.
			if !(has && existing.APIKey == inst.APIKey) {
				return instancesDoor(name, "api_key")
			}
		}
		instances = append(instances, inst)
	}
	if !seen[PrimaryProviderInstance] {
		// Every provider is a row now: without the primary row there is no provider the agent
		// can serve, and a RENAME of it is exactly this error - the primary keeps its name,
		// its endpoint is what may change (in config.yaml).
		return fmt.Errorf("%s: the row named %q is required - it is the provider the agent serves (rename it by editing config.yaml, never away)", key, PrimaryProviderInstance)
	}
	cfg.Provider.Instances = instances
	cfg.Provider.primaryFirst()
	return nil
}

// storedHostIn reports whether config already serves the host.
func storedHostIn(cfg *Config, host string) bool {
	for _, inst := range cfg.Provider.Instances {
		if inst.Host == host {
			return true
		}
	}
	return false
}

// renamedOffKey finds the secret a masked round-trip on a NEW name inherits.
func renamedOffKey(cfg *Config, submitted map[string]bool, want string) (string, int) {
	keys := map[string]bool{}
	for _, inst := range cfg.Provider.Instances {
		if submitted[inst.Name] || inst.Host != want {
			continue
		}
		keys[inst.APIKey] = true
	}
	if len(keys) != 1 {
		return "", len(keys)
	}
	key := ""
	for k := range keys {
		key = k
	}
	return key, 1
}

// asTrimmedString coerces a submitted JSON value into a trimmed string.
func asTrimmedString(raw any) (string, error) {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v), nil
	case nil:
		return "", nil
	case float64:
		// JS number-to-string without a scientific-notation detour
		return strings.TrimSpace(fmt.Sprintf("%v", v)), nil
	default:
		return "", fmt.Errorf("expected a string, got %T", raw)
	}
}

// setString applies a string knob through a normalizer.
func setString(dst *string, key string, raw any, norm func(string) string) error {
	s, err := asTrimmedString(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	*dst = norm(s)
	return nil
}

// asInt coerces a submitted JSON value into an int. JSON decodes every number as
// float64; integral values round-trip exactly, non-integral ones are rejected.
func asInt(raw any) (int, bool) {
	switch v := raw.(type) {
	case float64:
		if v != math.Trunc(v) {
			return 0, false
		}
		return int(v), true
	case bool:
		return 0, false
	default:
		return 0, false
	}
}

// setInt applies an integer knob within [min, max].
func setInt(dst *int, key string, raw any, min, max int) error {
	v, ok := asInt(raw)
	if !ok {
		return fmt.Errorf("%s: expected a whole number, got %v", key, raw)
	}
	if v < min || v > max {
		return fmt.Errorf("%s: %d out of range [%d, %d]", key, v, min, max)
	}
	*dst = v
	return nil
}

// setFloat applies a float knob within [min, max].
func setFloat(dst *float64, key string, raw any, min, max float64) error {
	v, ok := raw.(float64)
	if !ok {
		return fmt.Errorf("%s: expected a number, got %v", key, raw)
	}
	if v < min || v > max {
		return fmt.Errorf("%s: %v out of range [%v, %v]", key, v, min, max)
	}
	*dst = v
	return nil
}

// setBool applies a toggle knob.
func setBool(cfg *Config, key string, raw any) error {
	b, ok := raw.(bool)
	if !ok {
		return fmt.Errorf("%s: expected true or false, got %v", key, raw)
	}
	switch key {
	case "compaction.enabled":
		cfg.Compaction.Enabled = b
	case "memory.enabled":
		cfg.Memory.Enabled = b
	case "memory.user_profile_enabled":
		cfg.Memory.UserProfileEnabled = b
	case "logging.enabled":
		cfg.Logging.Enabled = b
	case "approvals.judge_fail_closed":
		cfg.Approvals.JudgeFailClosed = b
	default:
		return fmt.Errorf("unknown setting: %s", key)
	}
	return nil
}

// writeConfigFile persists the config as YAML, atomically (temp file + rename).
func writeConfigFile(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	mode := os.FileMode(0600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode()
	}
	tmp := path + ".omega-settings.tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp) // best-effort cleanup
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

// hotApply runs the knobs the running process can use without a restart.
func hotApply(cfg Config, prevWorkspace string, pctx *agent.Context, setFilesWorkspace func(string), setChannelWorkspace func(string), setTerminalWorkspace func(string), setAgentConfig func(string, int)) {
	llm.SetHTTPTimeout(cfg.HTTPTimeout)
	if pctx == nil {
		return
	}
	// default_workspace rides the files capsule's knob hook.
	// The hook is a bare func threaded from the mount (the continuous capsule's setAgents
	// shape): nil means no files capsule is mounted and the knob stays where it was.
	if cfg.Server.DefaultWorkspace != prevWorkspace {
		if setFilesWorkspace != nil {
			setFilesWorkspace(cfg.Server.DefaultWorkspace)
		}
		// The same knob re-roots the tools' fallback workdir on the HTTP channel.
		if setChannelWorkspace != nil {
			setChannelWorkspace(cfg.Server.DefaultWorkspace)
		}
		if setTerminalWorkspace != nil {
			setTerminalWorkspace(cfg.Server.DefaultWorkspace)
		}
	}
	svc := pctx.Services().Resolve("compactor")
	if c, ok := svc.(*compactor.Compactor); ok {
		c.SetConfig(compactionSection(cfg))
	}
	// Approvals goes live: the smart approval gate re-reads its config per decision,.
	// Not mounted (mode off at boot) = a nil resolve, no-op: re-mounting stays out of
	// scope.
	if a, ok := pctx.Services().Resolve("approvals").(*approval.Gate); ok {
		a.SetConfig(approvalSection(cfg))
	}
	if pp, ok := pctx.Services().Resolve("provider").(providerConfigSetter); ok {
		pp.SetConfig(providerSection(cfg))
	}
	// vision_model goes live on the SAME stroke.
	if c, ok := pctx.Captioner.(visionModelSetter); ok {
		c.SetModel(cfg.VisionModel)
	}
	// title_model rides the channel's own setter. The push is unconditional - a CLEARED
	// knob must reach the wire too, and the setter is idempotent.
	for _, ch := range pctx.Channels {
		if tm, ok := ch.(titleModelSetter); ok {
			tm.SetTitleModel(cfg.TitleModel)
		}
	}
	// The store's hygiene pass goes live. Unmounted store (the host's own supply) = a nil
	// resolve, no-op.
	if s, ok := pctx.Services().Resolve("store").(*store.Store); ok {
		s.SetHygieneConfig(sessionsSection(cfg))
	}
	// The skills curator does the same for its consult slot. Consumer law (the noun test):
	// the host declares its own local face, the capsule's type name never leaves harness/.
	if sp, ok := pctx.Services().Resolve("skills").(curatorSlotSetter); ok {
		sp.SetCuratorModel(skillsSection(cfg).CuratorModel)
	}
	// The runtime knobs land on the AGENTS. Nil hook = the surface is not mounted (host
	// fallback supply, tests).
	if setAgentConfig != nil {
		setAgentConfig(cfg.SystemPrompt, cfg.MaxTurns)
	}
	pushRunTimeout(pctx, cfg)
}

// runTimeoutSetter is the host's consumer-owned face for the cron capsule's fire cap:
// hotApply declares its need here, the capsule satisfies it with SetRunTimeout - the
// owner name never leaves harness/ (the curatorSlotSetter discipline).
type runTimeoutSetter interface {
	SetRunTimeout(time.Duration)
}

// pushRunTimeout hands the cron runner the server.run_timeout this host resolved. A nil
// context or an unmounted cron capsule is a no-op: the fire keeps its own
// home-candidate read, then the 30-minute default.
func pushRunTimeout(pctx *agent.Context, cfg Config) {
	if pctx == nil {
		return
	}
	if rt, ok := pctx.Services().Resolve("cron").(runTimeoutSetter); ok {
		rt.SetRunTimeout(time.Duration(cfg.Server.RunTimeout) * time.Second)
	}
}

// curatorSlotSetter is the host's consumer-owned face for the skills capsule's live
// consult slot: hotApply declares its need here, the capsule satisfies it with
// SetCuratorModel - the owner name never leaves harness/ (the noun test holds the
// namespace).
type curatorSlotSetter interface {
	SetCuratorModel(model string)
}

// providerConfigSetter is the host's consumer-owned face for the provider.
type providerConfigSetter interface {
	SetConfig(provider.Config)
}

// visionModelSetter is the captioner's live lane.
type visionModelSetter interface {
	SetModel(model string)
}

// titleModelSetter is the HTTP channel's live title lane.
type titleModelSetter interface {
	SetTitleModel(configured string)
}

// omegaSettings implements core.Settings for the serve host.
type omegaSettings struct {
	path                 string
	pctx                 *agent.Context
	setFilesWorkspace    func(string)      // the files capsule's hot knob, nil when it is not mounted
	setChannelWorkspace  func(string)      // the HTTP channel's default-workspace hot knob, nil when unset
	setTerminalWorkspace func(string)      // the terminal capsule's default-workspace hot knob, nil when unset
	setAgentConfig       func(string, int) // the serving agents' runtime-knob hot hook
	maxTurns             func() int        // the serving agents' LIVE turn cap, the agent scopes' inherited max_turns
}

func (s *omegaSettings) Schema() (*core.SettingsSchema, error) {
	schema, err := settingsPayload(s.path, s.maxTurns)
	if err != nil {
		return nil, err
	}
	if s.pctx == nil {
		return schema, nil
	}
	c, ok := s.pctx.Services().Resolve("compactor").(*compactor.Compactor)
	if !ok {
		return schema, nil
	}
	if w := c.AutoWindow(); w > 0 {
		for _, g := range schema.Groups {
			for ki, k := range g.Knobs {
				if k.Key == "compaction.context_window" {
					g.Knobs[ki].Help = fmt.Sprintf("%s; auto resolves to %d", k.Help, w)
				}
			}
		}
	}
	return schema, nil
}

func (s *omegaSettings) Save(values map[string]any) (*core.SettingsSchema, error) {
	return saveSettings(s.path, s.pctx, s.setFilesWorkspace, s.setChannelWorkspace, s.setTerminalWorkspace, s.setAgentConfig, values)
}

// Raw returns the config FILE as YAML text for the pane's export and edit-as-YAML. The
// bytes are the operator's own document - comments, key order and style included - with
// each api_key VALUE replaced by the pane's mask: the config file is a credential's
// only home, so the unauthenticated wire never carries one. A file that will not parse
// answers the parse error instead of its text (an unmaskable document is a leak, not a
// read).
func (s *omegaSettings) Raw() (string, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return "", err
	}
	return maskConfigSecrets(string(data))
}

// SaveRaw validates a whole YAML document and persists it through the same path a knob
// save rides: the same LoadConfig layer (built-in defaults + the document + OMEGA_*
// overrides), the same Validate, the same atomic writer, the same hot-apply. A
// submitted mask at a secret path KEEPS the value already on disk (the
// provider.instances round-trip law), so opening the editor from an export and saving
// it back can never destroy a credential.
func (s *omegaSettings) SaveRaw(text string) (*core.SettingsSchema, error) {
	prev, err := LoadConfig(s.path)
	if err != nil {
		return nil, err
	}
	cfg, err := configFromYAML(text)
	if err != nil {
		return nil, err
	}
	doc, err := documentConfig(text)
	if err != nil {
		return nil, err
	}
	// the credential config.yaml will hold. An unreadable on-disk document is a REFUSAL,
	// never a skip: with nothing to keep, a submitted mask would be persisted as the
	// literal credential.
	if err := settleSecretsFromDisk(s.path, &cfg, &doc); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if err := writeConfigFile(s.path, &cfg); err != nil {
		return nil, err
	}
	hotApply(cfg, prev.Server.DefaultWorkspace, s.pctx, s.setFilesWorkspace, s.setChannelWorkspace, s.setTerminalWorkspace, s.setAgentConfig)
	return &core.SettingsSchema{
		Groups: settingGroups(&cfg),
		Scopes: settingsScopes(settingsScopeHome(s.path), &cfg, nil),
	}, nil
}

// Reset restores every knob that HAS a default to it through saveSettings - the pane's
// own PUT path, so the write validates, persists and hot-applies exactly like a hand
// save.
func (s *omegaSettings) Reset() (*core.SettingsSchema, error) {
	values, err := resetValues()
	if err != nil {
		return nil, err
	}
	return saveSettings(s.path, s.pctx, s.setFilesWorkspace, s.setChannelWorkspace, s.setTerminalWorkspace, s.setAgentConfig, values)
}

// resetGated are the knob paths a reset never writes: config.yaml is
// their only door (provider.api_key, server.bind), and the named
// instances' rows carry endpoints and secrets the API cannot set. A
// reset must never delete a value the same API cannot put back.
var resetGated = map[string]bool{
	"provider.api_key":   true,
	"server.bind":        true,
	"provider.instances": true,
}

// resetValues is the write map the reset-to-default action ships.
func resetValues() (map[string]any, error) {
	def := defaultConfig()
	values := map[string]any{}
	for _, g := range settingGroups(&def) {
		for _, k := range g.Knobs {
			if k.Default == nil || resetGated[k.Key] {
				continue
			}
			values[k.Key] = k.Default
		}
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("reset: the schema carries no default")
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("encode defaults: %w", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("decode defaults: %w", err)
	}
	return wire, nil
}

// fileConfig loads the config FILE alone.
func fileConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return documentConfig(string(data))
}

// documentConfig parses ONE YAML document over the built-in defaults, with no.
func documentConfig(text string) (Config, error) {
	cfg := DefaultConfig()
	if err := yaml.Unmarshal([]byte(text), &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

// configFromYAML parses a submitted document exactly the way LoadConfig parses.
// Validate is the caller's, so the layer order stays in one place.
func configFromYAML(text string) (Config, error) {
	cfg, err := documentConfig(text)
	if err != nil {
		return Config{}, err
	}
	applyEnv(&cfg)
	return cfg, nil
}

// settleSecretsFromDisk pins the credential layer a raw save writes against the. An
// unreadable document is an ERROR and never a skip: with nothing to keep, a submitted
// mask ("****") would be written out as the literal credential.
func settleSecretsFromDisk(path string, cfg *Config, doc *Config) error {
	onDisk, err := fileConfig(path)
	if err != nil {
		return fmt.Errorf("save config: cannot read the document on disk to resolve a masked secret: %w", err)
	}
	settleSecrets(cfg, doc, &onDisk)
	return nil
}

// settleSecrets pins the credential layer a raw save writes: the document's own value
// wins (a literal key typed in the editor), the mask keeps what the file already holds,
// and an OMEGA_*-supplied secret never lands in config.yaml - the variable is its
// source, and a copy in the file would outlive it.
func settleSecrets(cfg *Config, doc *Config, onDisk *Config) {
	instances := make([]ProviderInstance, 0, len(cfg.Provider.Instances))
	for _, inst := range cfg.Provider.Instances {
		docKey, diskKey := "", ""
		if d, ok := doc.Provider.instance(inst.Name); ok {
			docKey = d.APIKey
		}
		if d, ok := onDisk.Provider.instance(inst.Name); ok {
			diskKey = d.APIKey
		}
		inst.APIKey = secretLayerValue(docKey, diskKey)
		instances = append(instances, inst)
	}
	cfg.Provider.Instances = instances
}

// secretLayerValue is the one rule.
func secretLayerValue(docValue, diskValue string) string {
	if docValue == maskedSecret {
		return diskValue
	}
	return docValue
}

// maskConfigSecrets renders a config document with every api_key value masked, every
// other byte (comments, order, flow/block style) kept. The walk is BY NAME, not by
// path: an api_key is a credential wherever it sits, so a provider-shaped section added
// later cannot introduce an unmasked one.
func maskConfigSecrets(text string) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return "", fmt.Errorf("parse config: %w", err)
	}
	if len(doc.Content) == 0 {
		return text, nil // no document, no secret
	}
	maskSecretNodes(doc.Content[0])
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2) // the config file's own nesting: 2 spaces per level
	if err := enc.Encode(doc.Content[0]); err != nil {
		return "", fmt.Errorf("encode config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("encode config: %w", err)
	}
	return buf.String(), nil
}

// maskSecretNodes masks every populated api_key scalar under n.
func maskSecretNodes(n *yaml.Node) {
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Value == "api_key" && v.Kind == yaml.ScalarNode && v.Value != "" {
				v.Value = maskedSecret
				v.Tag = "!!str"
				v.Style = 0
				continue
			}
			maskSecretNodes(v)
		}
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range n.Content {
			maskSecretNodes(c)
		}
	}
}

// Compile-time assertion that omegaSettings implements core.Settings.
var _ core.Settings = (*omegaSettings)(nil)

// omega settings: the terminal face of the settings pane. Reads call settingsSchema
// (the same function the pane's GET rides) and writes call saveSettings (the same
// function the pane's PUT rides) - one schema, one setter, one validator, one writer,
// so a CLI save and a pane save land byte-identically in config.yaml and every refusal
// (unknown knob, out-of-range value, a config.yaml-only door) is the seam's own
// sentence.

// settingsUsage is the verb grammar, printed on a bad verb.
const settingsUsage = `omega settings - read and write config knobs

Usage:
  omega settings                    every knob, grouped, as key = value
  omega settings get [<key>]        every knob, or one knob's value
  omega settings set <key> <value>  write one knob through the settings seam

Keys are the dotted yaml paths the webui settings pane uses
(provider.model_name, server.port, compaction.enabled, ...).

Values: true/false (yes/no, on/off) for a toggle, a number for a
number, a JSON array for provider.instances, text otherwise. A write
validates exactly like a pane save; server.bind stays config.yaml's
door, and so does a stored instance's host and secret. A refusal
writes nothing.
`

// cmdSettings dispatches the settings verbs.
func cmdSettings(configPath string, rest []string) error {
	args := settingsArgs(rest)
	if len(args) == 0 {
		return settingsList(configPath)
	}
	switch args[0] {
	case "get":
		if len(args) > 2 {
			return fmt.Errorf("usage: omega settings get [<key>]")
		}
		if len(args) == 2 {
			return settingsGet(configPath, args[1], os.Stdout)
		}
		return settingsList(configPath)
	case "set":
		if len(args) != 3 {
			return fmt.Errorf("usage: omega settings set <key> <value>")
		}
		return settingsSet(configPath, args[1], args[2], os.Stdout)
	case "--help", "-h", "help":
		fmt.Print(settingsUsage)
		return nil
	default:
		fmt.Fprint(os.Stderr, settingsUsage)
		return fmt.Errorf("unknown settings subcommand %q", args[0])
	}
}

// settingsArgs drops the --config pair (the file selector, already parsed by the
// caller) from the verb argv.
func settingsArgs(rest []string) []string {
	out := make([]string, 0, len(rest))
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "--config":
			i++ // skip the value
		case strings.HasPrefix(a, "--config="):
		default:
			out = append(out, a)
		}
	}
	return out
}

// settingsConfigPath resolves the file both verbs read and write. The bare home without
// a config is the setup gate's refusal - a write against no file would have to invent
// one, and the seam never creates config from nothing.
func settingsConfigPath(flagPath string) (string, error) {
	path := resolveConfigPath(flagPath)
	if path == "" {
		return "", fmt.Errorf("no config found in %s - run `omega setup` first", omegaHome())
	}
	return path, nil
}

// settingsList prints every knob group. Strings are quoted so an empty value is
// visible; secrets ride masked exactly as the pane's GET masks them.
func settingsList(configPath string) error {
	path, err := settingsConfigPath(configPath)
	if err != nil {
		return err
	}
	schema, err := settingsSchema(path)
	if err != nil {
		return err
	}
	for i, g := range schema.Groups {
		if i > 0 {
			fmt.Fprintln(os.Stdout)
		}
		fmt.Fprintln(os.Stdout, g.Key)
		for _, k := range g.Knobs {
			fmt.Fprintf(os.Stdout, "  %s = %s\n", k.Key, settingsValueText(k))
		}
	}
	return nil
}

// settingsGet prints one knob's value, unquoted so a script reads it straight.
func settingsGet(configPath, key string, w io.Writer) error {
	if strings.HasPrefix(key, "-") {
		return fmt.Errorf("usage: omega settings get [<key>]")
	}
	path, err := settingsConfigPath(configPath)
	if err != nil {
		return err
	}
	schema, err := settingsSchema(path)
	if err != nil {
		return err
	}
	knob, ok := schema.Knob(key)
	if !ok {
		return fmt.Errorf("unknown setting: %s", key)
	}
	fmt.Fprintln(w, settingsRawText(*knob))
	return nil
}

// settingsSet writes one knob through the seam the pane's PUT rides.
func settingsSet(configPath, key, raw string, w io.Writer) error {
	if strings.HasPrefix(key, "-") {
		return fmt.Errorf("usage: omega settings set <key> <value>")
	}
	path, err := settingsConfigPath(configPath)
	if err != nil {
		return err
	}
	schema, err := settingsSchema(path)
	if err != nil {
		return err
	}
	knob, ok := schema.Knob(key)
	if !ok {
		return fmt.Errorf("unknown setting: %s", key)
	}
	value, err := coerceSettingValue(*knob, raw)
	if err != nil {
		return err
	}
	refreshed, err := saveSettings(path, nil, nil, nil, nil, nil, map[string]any{key: value})
	if err != nil {
		return err
	}
	applied, ok := refreshed.Knob(key)
	if !ok {
		return fmt.Errorf("%s: saved, but the refreshed schema does not carry the knob", key)
	}
	fmt.Fprintf(w, "%s = %s\n", key, settingsValueText(*applied))
	return nil
}

// coerceSettingValue turns the operator's text into the JSON-shaped value the webui.
// This is an input adapter, never a second validator: ranges, enum membership,
// absolute-path checks, the config.yaml-only doors and every cross-field rule stay
// behind saveSettings (applySettingValues + cfg.Validate), so the CLI can never accept
// what the pane refuses or refuse what the pane accepts. The one guard added here is
// finiteness: JSON cannot carry NaN/Inf, so the pane can never submit them while the
// CLI text can - and the seam's range test would let NaN through.
func coerceSettingValue(k core.SettingsKnob, raw string) (any, error) {
	switch k.Type {
	case "toggle":
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "true", "1", "yes", "on":
			return true, nil
		case "false", "0", "no", "off":
			return false, nil
		}
		return nil, fmt.Errorf("%s: expected true or false, got %q", k.Key, raw)
	case "number":
		v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("%s: expected a finite number, got %q", k.Key, raw)
		}
		return v, nil
	case "providers":
		var list []any
		if err := json.Unmarshal([]byte(raw), &list); err != nil {
			return nil, fmt.Errorf("%s: expected a JSON array of instances, got %q", k.Key, raw)
		}
		return list, nil
	default:
		return raw, nil
	}
}

func settingsValueText(k core.SettingsKnob) string {
	switch v := k.Value.(type) {
	case string:
		return strconv.Quote(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case nil:
		return `""`
	}
	if b, err := json.Marshal(k.Value); err == nil {
		return string(b)
	}
	return fmt.Sprintf("%v", k.Value)
}

// settingsRawText renders a single knob's value for script consumption.
func settingsRawText(k core.SettingsKnob) string {
	if s, ok := k.Value.(string); ok {
		return s
	}
	return settingsValueText(k)
}
