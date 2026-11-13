package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/approval"
	"github.com/EndoTheDev/omega/harness/compactor"
	"github.com/EndoTheDev/omega/harness/store"
	"github.com/EndoTheDev/omega/llm"
	"gopkg.in/yaml.v3"
)

// settings.go exposes the config.yaml knobs to the webui settings pane
// over the http_channel Settings seam (core.Settings). The schema here
// is the single source of truth: one entry per Config field, grouped in
// config-file order — no new config keys, no duplicates. Save loads the
// current file, applies the submitted values by dotted yaml path,
// validates through the normal LoadConfig path, persists atomically,
// and hot-applies what the running process can use without a restart.

// The knob/group shapes are core.SettingsKnob / core.SettingsGroup /
// core.SettingsSchema - the seam is typed end to end, so cmd/omega and
// http_channel cannot drift apart on the wire shape. Masked
// secrets and the config-yaml-only gate live in this file: the host
// decides what the unauthenticated API may see and write.
//
// maskedSecret stands in for a secret knob's value everywhere the
// schema is served (GET and save refreshes alike): the pane treats
// api_key as write-only (diff-only saves), so the plain text never
// rides the wire. An empty value stays empty - masking a missing key
// would imply one exists.
const maskedSecret = "****"

// maskSecret renders a secret knob's value for the wire.
func maskSecret(v string) any {
	if v == "" {
		return ""
	}
	return maskedSecret
}

// num builds the min/max/step triple for a number knob.
func num(min, max, step float64) (*float64, *float64, *float64) {
	return &min, &max, &step
}

// settingsSchema loads the live config and renders it as groups of
// knobs. Values come from the same LoadConfig path the server booted
// with (file + env overrides + defaults), so the pane always shows the
// effective values.
func settingsSchema(path string) (*core.SettingsSchema, error) {
	return settingsPayload(path, nil)
}

// settingsPayload renders the whole GET payload: the knob groups plus
// the agent scopes (settings tier 3a, card t_22a5f70a). runtimeMaxTurns
// is the serving agents' LIVE turn cap (agent.Agent.MaxTurns, card
// t_f78acfd6); nil means no serving agent (the CLI, a save refresh) and
// the loaded config's own value stands in - after a save the hot-apply
// has just pushed exactly that value, so the two lanes agree there by
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

// settingsScopeHome anchors the agent-scope scan to the config file:
// the settled home layout keeps it at <home>/config/config.yaml, the
// legacy layout at <home>/config.yaml (the two locations
// resolveConfigPath probes). Reading the anchor off the file - not the
// process environment - keeps a `--config <file>` run and every test
// honest about WHICH home the scopes describe.
func settingsScopeHome(configPath string) string {
	dir := filepath.Dir(configPath)
	if filepath.Base(dir) == "config" {
		return filepath.Dir(dir)
	}
	return dir
}

// scopeGroupKeys is the R2 vocabulary in render order: the four
// overridable knob groups an agent tab shows (R3), and the dotted keys
// each carries. The keys are the SAME paths the main payload's knobs
// use wherever the two surfaces name the same knob, so the pane reads a
// knob's inherited base straight out of groups (dot = differs from
// main, the seam ruling) with no second copy of the values.
var scopeGroupKeys = []string{"provider", "system_prompt", "description", "max_turns"}

// settingsScopes renders one scope per agent home under <home>/agents/.
// The scope NAMES come from agent.LoadProfiles - the roster scan the
// agents tab already reads, so the two surfaces can never disagree on
// which dirs are agents (main's home is the root, so it is never a
// scope; dot/underscore and malformed dirs are skipped there and here).
// The VALUES come from the layer read because Profile carries no
// presence information (see agent.AgentConfigLayer).
//
// R8 empty world: no agent dirs = an empty, non-nil list. The pane
// renders no scope strip at all.
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
			// The dir IS a scope (R8: no special-casing), so it rides
			// with inherited values and no override flags - but the
			// error field says so, because those flags are then not the
			// file's truth. The write endpoint refuses the same layer.
			scope.Error = err.Error()
		}
		scopes = append(scopes, scope)
	}
	return scopes
}

// scopeGroups renders the four groups for one scope. A knob whose layer
// entry is absent inherits the main value and answers overridden=false;
// a knob the layer NAMES - even with an empty value - answers the
// layer's value and overridden=true (the write lane stores an empty
// string as an override, so the flag must agree with it).
func scopeGroups(cfg *Config, lay agent.AgentConfigLayer, inheritedTurns int) []core.SettingsScopeGroup {
	value := func(key string, own agent.LayerValue, inherited string) core.SettingsScopeKnob {
		if own.Set {
			return core.SettingsScopeKnob{Key: key, Value: own.Value, Overridden: true}
		}
		return core.SettingsScopeKnob{Key: key, Value: inherited}
	}
	provider := []core.SettingsScopeKnob{
		value("provider.type", lay.ProviderType, strings.ToLower(cfg.Provider.Type)),
		value("provider.model_name", lay.ModelName, cfg.Provider.ModelName),
		value("provider.host", lay.Host, cfg.Provider.Host),
		// the secret is masked on the way out exactly like the main
		// payload (maskSecret lives here: the host decides what the
		// unauthenticated API may see)
		{Key: "provider.api_key", Value: maskSecret(layerSecret(lay.APIKey, cfg.Provider.APIKey)), Overridden: lay.APIKey.Set},
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
		// main carries no description knob (the home config has no such
		// field), so the inherited base is the empty string
		{Key: scopeGroupKeys[2], Overridden: lay.Description.Set, Knobs: []core.SettingsScopeKnob{
			value("description", lay.Description, ""),
		}},
		{Key: scopeGroupKeys[3], Overridden: turns.Overridden, Knobs: []core.SettingsScopeKnob{turns}},
	}
}

// layerSecret picks the secret a scope shows: the layer's own when it
// names the key, main's otherwise.
func layerSecret(own agent.LayerValue, inherited string) string {
	if own.Set {
		return own.Value
	}
	return inherited
}

// anyOverridden is the group roll-up: one overridden knob makes the
// group the agent's own.
func anyOverridden(knobs []core.SettingsScopeKnob) bool {
	for _, k := range knobs {
		if k.Overridden {
			return true
		}
	}
	return false
}

// defaultConfig returns the config layer BENEATH the file: the built-in
// defaults (DefaultConfig) with the OMEGA_* environment overrides
// applied - exactly the value LoadConfig gives a knob whose key
// config.yaml does not name. The file itself is deliberately excluded:
// writeConfigFile marshals the whole Config, so one settings save
// materializes EVERY key, and a file-derived "default" would equal every
// current value - the settings pane's modified-dot (tier 2b, card
// t_c04c959e) could never light. Every knob's Default reads a field of
// this struct, never a hand-typed literal, so a Config rename breaks the
// build instead of silently drifting from the schema (card t_d13c7264).
func defaultConfig() Config {
	cfg := DefaultConfig()
	applyEnv(&cfg)
	return cfg
}

// settingGroups builds the full knob inventory from a config. Field
// order follows the config.yaml layout (provider, server, store,
// compaction, runtime, skills, memory, logging). Every knob carries
// Default from defaultConfig(); the only omission is
// provider.model_name - config.Validate requires it, so no value can be
// its default and the wire answers null (the pane's "no default known").
func settingGroups(cfg *Config) []core.SettingsGroup {
	def := defaultConfig()
	return []core.SettingsGroup{
		{
			Key:   "provider",
			Label: "provider",
			Knobs: []core.SettingsKnob{
				{Key: "provider.type", Label: "type", Type: "select",
					Options: []string{"ollama", "openai", "anthropic", "mock"},
					Value:   strings.ToLower(cfg.Provider.Type),
					// config-layer default is empty ("empty defaults to
					// ollama" is the provider capsule's read of it), so a
					// fresh config is NOT shown as modified.
					Default: strings.ToLower(def.Provider.Type),
					Help:    "provider flavor; empty defaults to ollama; mock answers deterministically with no network (tests, journeys)"},
				// Card t_1914a8c9: the provider group is ordered CONNECTION
				// FIRST (type, base url, api key), then the model knobs,
				// then the instance list and the slots - the fields render
				// in the shape the pane groups them.
				{Key: "provider.host", Label: "base url", Type: "text",
					Value:   cfg.Provider.Host,
					Default: def.Provider.Host,
					Help:    "provider base URL (default http://localhost:11434)"},
				{Key: "provider.api_key", Label: "api key", Type: "secret",
					Value: maskSecret(cfg.Provider.APIKey),
					// The default rides masked exactly like the value: an
					// env-supplied OMEGA_API_KEY must never leak through the
					// unauthenticated GET, and no stored key means the default
					// is the empty string (masking that would imply a key).
					Default: maskSecret(def.Provider.APIKey),
					Help:    "masked here; edit config.yaml instead (blank for local ollama)"},
				// card t_70c28128: model fields are typed pickers - the
				// webui renders type "model" as a dropdown of the
				// provider's live models, filtered by the knob's Filter
				// (vision slots see vision-capable models only).
				{Key: "provider.model_name", Label: "model name", Type: "model",
					Value: cfg.Provider.ModelName,
					// NO Default: config.Validate rejects an empty
					// model_name, so no value can be "the default" - the
					// knob answers JSON null and the pane renders no dot
					// and no reset (the one omission in this schema).
					Help: "default model the agent serves; the webui model picker overrides per chat; applies on restart"},
				{Key: "provider.vision_model", Label: "vision model", Type: "model", Filter: "vision", DefaultOption: true,
					Value:   cfg.VisionModel,
					Default: def.VisionModel,
					Help:    "auxiliary captioner model for image messages; \"default\" follows the model the current chat runs (the composer's chip), resolved per call - useful only when that model can see images (the caption hop exists for a blind main model, so \"default\" on a blind one sends the image there and the run fails on the caption); the dropdown lists vision-capable models only; empty = images reach the main model unchanged and may fail loudly; applies on restart"},
				{Key: "provider.title_model", Label: "title model", Type: "model", DefaultOption: true,
					Value:   cfg.TitleModel,
					Default: def.TitleModel,
					Help:    "cheap model for session titles; empty or \"default\" = the model this chat runs (resolved per call); applies on restart"},
				// card t_7dcd4280, reshaped t_1914a8c9: the named
				// instances - one row per instance, each a NAMED ENDPOINT
				// only (name, type, base url, api key; no model column -
				// models come from the instance's catalog through the
				// model pickers). The list REPLACES the providers: map on
				// save; a masked key round-trips as "keep the existing
				// secret".
				{Key: "provider.instances", Label: "provider instances", Type: "providers",
					Value: providerInstancesKnob(cfg),
					// The default is the empty list, rendered through the
					// same function as the value so the shapes cannot drift.
					Default: providerInstancesKnob(&def),
					Help:    "named provider endpoints: the name is the instance id, so two accounts of one type are two rows; the provider section above stays the default; rows can be renamed, retyped and removed here, but a row's host and api key are config.yaml-only (changes apply on restart)"},
				// card t_fc0b2f2d: the four per-subsystem model
				// slots - provider settings assigns one named model
				// per subsystem ("instance:model" or a bare model;
				// the selector also lists the named instances). The
				// four call sites (compaction, summarizer, curator,
				// approval reviewer) resolve the slot live.
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
					// Config-layer default is empty; the api capsule's
					// api.Default() supplies 127.0.0.1 for an unset bind
					// (api/config.go:60), so the loopback-only law is the
					// effective default while an empty config stays
					// unmodified. An explicit 0.0.0.0 lights the dot.
					Default: strings.Join(def.Server.Bind, ", "),
					Help:    "comma-separated listen addresses; loopback only by default (unauthenticated API); edit config.yaml instead (applies on restart)"},
				{Key: "server.run_timeout", Label: "run timeout (s)", Type: "number",
					Min: ptrF(1), Step: ptrF(1),
					Value:   cfg.Server.RunTimeout,
					Default: def.Server.RunTimeout,
					Help:    "wall-clock cap per detached chat run and per cron fire, in seconds; a chat run takes a change on restart, a cron fire on its next fire"},
				{Key: "server.default_workspace", Label: "default workspace", Type: "text",
					Value: cfg.Server.DefaultWorkspace, Live: true,
					// empty is the default (the cwd fallback stands)
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
					// store.DefaultHygiene().RetentionDays (config.go:348)
					Default: def.Sessions.RetentionDays,
					Help:    "the hygiene job deletes sessions inactive longer than this many days (pinned sessions are exempt); 0 = never prune; applies live - the section rides the same hot-apply push as summarizer_model"},
				{Key: "sessions.summarize_after_hours", Label: "summarize after (h)", Type: "number",
					Min: ptrF(0), Step: ptrF(1),
					Value: cfg.Sessions.SummarizeAfterHours, Live: true,
					// The 0-pseudo-default knob: 0 is the runtime sentinel
					// ("the default, 24h"), the machine default is the real
					// window - store.DefaultHygiene().SummarizeAfterHours
					// (config.go:349), the number this pane shows as
					// unmodified. A written 0 therefore reads as modified,
					// which is honest: it is a different setting.
					Default: def.Sessions.SummarizeAfterHours,
					Help:    "the hygiene job summarizes settled sessions that lack a summary once they are idle this many hours; 0 = the default (24h); applies live - the section rides the same hot-apply push as summarizer_model"},
				// The summarizer's model moved to provider settings
				// (card t_fc0b2f2d): provider.summarizer_model.
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
					// 0 is a runtime sentinel (the same 100); the machine
					// default is the number DefaultConfig carries
					// (config.go:384).
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
					// skills.DefaultNudgeInterval (config.go:365)
					Default: def.Skills.NudgeInterval,
					Help:    "learning-loop reminder cadence: tool iterations without a persisted lesson before the next turn prep carries one reminder line; 0 = off; applies on restart"},
				// The curator's model moved to provider settings
				// (card t_fc0b2f2d): provider.skills_model.
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
					// The default filename is deliberately empty: a bare
					// relative name would open a log in the process CWD
					// (config.go:377-381); resolveHomePaths computes the
					// real path at boot.
					Default: def.Logging.File,
					Help:    "applies on restart"},
			},
		},
		// The approval gate group (card t_c2b52cb5): mode off|manual|smart
		// and the operator policy text. The decision model moved to
		// provider settings (card t_fc0b2f2d). The gate goes live (card
		// t_34cd1921): mode and policy hot-apply - a flip answers with the
		// new behavior at the next decision, no restart. Mode off at BOOT
		// still mounts nothing (Q10) - re-mounting stays a restart.
		{
			Key:   "approvals",
			Label: "approvals",
			Knobs: []core.SettingsKnob{
				{Key: "approvals.mode", Label: "mode", Type: "select",
					Options: []string{"off", "manual", "smart"},
					Value:   cfg.Approvals.Mode, Live: true,
					// config.go:392-394: manual IS the explicit default
					// (the ship-dark ruling).
					Default: def.Approvals.Mode,
					Help:    "off = no gate at all; manual = flagged commands ask you (default); smart = a decision model reviews them first; applies live for mounted gates (off at boot keeps the gate unmounted)"},
				// The decision model moved to provider settings (card
				// t_fc0b2f2d): provider.approvals_model.
				{Key: "approvals.smart_policy", Label: "operator policy", Type: "textarea",
					Value: cfg.Approvals.SmartPolicy, Live: true,
					Default: def.Approvals.SmartPolicy,
					Help:    "trusted text the reviewer reads beside every command: describe routine operations that should not interrupt you; applies live; a policy edit also re-keys the gate cache"},
			},
		},
	}
}

// compactionKnobs renders the agent.CompactionConfig section. Every
// compaction knob is read per Compact call, so all hot-apply live.
// def carries the default layer (defaultConfig) the Default fields read.
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
			// The config default is the 32768 floor DefaultConfig carries
			// (config.go:357); 0 is the "probe the provider" sentinel and
			// therefore reads as modified.
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

// providerInstancesKnob renders the named provider instances (card
// t_7dcd4280, reshaped t_1914a8c9) as the list editor's value: one
// object per instance - name, type, host, api_key, NO model column
// (models come from the instance's catalog through the pickers) -
// names sorted (a yaml map iterates unordered - the pane must not
// reshuffle its rows between reloads). The api_key rides masked; the
// save path treats a masked value as "keep the existing secret".
func providerInstancesKnob(cfg *Config) []map[string]any {
	names := make([]string, 0, len(cfg.Providers))
	for n := range cfg.Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]map[string]any, 0, len(names))
	for _, n := range names {
		inst := cfg.Providers[n]
		out = append(out, map[string]any{
			"name":    n,
			"type":    strings.ToLower(inst.Type),
			"host":    inst.Host,
			"api_key": maskSecret(inst.APIKey),
		})
	}
	return out
}

// saveSettings applies values (dotted yaml path -> value), validates,
// and writes config.yaml atomically, then hot-applies the live knobs to
// the running context (the workspace hook only on a value change -
// prevWorkspace is what the load saw before the values applied, card
// t_9825147c part 3). Returns the refreshed schema payload.
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
	// the refreshed payload carries the scopes too, or the pane would
	// lose its agent tabs after every save. nil runtime getter: the
	// hot-apply above just pushed cfg.MaxTurns onto the serving agents,
	// so the file lane IS the runtime lane for this response.
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
		case "provider.type":
			err = setString(&cfg.Provider.Type, key, raw, trim)
		case "provider.model_name", "provider.host", "provider.vision_model", "provider.title_model":
			err = setStringField(cfg, key, raw)
		case "provider.instances":
			// The named instances (card t_7dcd4280): the card's point
			// is add/remove-with-key through the settings UI, so the
			// per-instance api_key IS hot-PUT-able here (masked read,
			// masked round-trip keeps the secret). The default
			// section's provider.api_key stays config.yaml-only -
			// the gated knob below is untouched.
			err = applyProviderInstances(cfg, key, raw)
		case "provider.api_key", "server.bind":
			// gated knobs: bind is the escalation lever for the whole
			// LAN surface, api_key is the credential. Neither a
			// restart-required nor a secret knob belongs on the
			// unauthenticated hot-PUT path - config.yaml is the only
			// door, and the error says so.
			err = fmt.Errorf("%s: config.yaml is the only door for this knob - not settable over the API", key)
		case "server.port":
			err = setInt(&cfg.Server.Port, key, raw, 1, math.MaxUint16)
		case "server.run_timeout":
			err = setInt(&cfg.Server.RunTimeout, key, raw, 1, math.MaxInt32)
		case "server.default_workspace":
			// empty clears (the law's cwd fallback stands); anything
			// else must be absolute and an existing directory - the
			// same shape a session workspace pin validates, validated
			// HERE so a typo never silently roots nothing. A "/"- or
			// "\"-rooted spelling counts as absolute: it is volume
			// rooted in the operator's eyes even where filepath.IsAbs
			// disagrees (windows drive-relative roots).
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
				// One not-exist message on every OS: the windows stat
				// answers "The system cannot find the file specified."
				// where posix answers ENOENT's words - the missing
				// directory is the same fact, one canon sentence.
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
			"logging.enabled":
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
		case "provider.compaction_model", "provider.summarizer_model",
			"provider.skills_model", "provider.approvals_model":
			// card t_fc0b2f2d: the per-subsystem model slots live in
			// the provider group (setStringField routes them).
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
	case "provider.host":
		cfg.Provider.Host = v
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

// trim normalizes plain string inputs: surrounding whitespace is never
// significant in any config field.
func trim(s string) string { return strings.TrimSpace(s) }

// applyProviderInstances applies the instances knob (card t_7dcd4280,
// reshaped t_1914a8c9; gated t_9825147c part 2). The submitted list
// REPLACES cfg.Providers. Each entry carries name, type, host and
// api_key - an instance is a named endpoint only, a model_name field
// is a loud error (models come from the instance's catalog through
// the model pickers). The stored rows are the trust anchor for the
// two gated columns: a row may be RENAMED, RETYPED or REMOVED over
// the API, but the endpoint (host) and the secret (api_key) are
// config.yaml's property - a changed host on a stored row, a key
// set or wiped on one, or a planted host config.yaml does not carry
// all answer with the config.yaml door (the same law provider.api_key
// and server.bind hold). Renames keep their secret via the masked
// round trip: the masked key inherits from the one stored row that
// carries the submitted host (ambiguous or missing matches refuse).
// A masked api_key keeps the entry's EXISTING secret ("****" means
// unchanged, not blank - the plain text never rides the wire, so the
// round trip must not wipe it). Unknown types, reserved names and
// duplicate ids fail through apply + cfg.Validate() in saveSettings -
// the schema is the contract and a typo must be loud, never silently
// dropped.
func applyProviderInstances(cfg *Config, key string, raw any) error {
	list, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("%s: expected a list of instance objects, got %T", key, raw)
	}
	instancesDoor := func(name, field string) error {
		return fmt.Errorf("%s: %s.%s: config.yaml is the only door for instance %s - not settable over the API", key, name, field, field)
	}
	// the submitted names, pre-computed in full (the rename source
	// below must know what the WHOLE list removes, not just the rows
	// so far) - card t_9825147c part 2
	sent := make(map[string]bool, len(list))
	for _, item := range list {
		if obj, ok := item.(map[string]any); ok {
			if n, e := asTrimmedString(obj["name"]); e == nil && n != "" {
				sent[n] = true
			}
		}
	}
	providers := make(map[string]ProviderConfig, len(list))
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
		inst := ProviderConfig{}
		if inst.Type, err = asTrimmedString(obj["type"]); err != nil {
			return fmt.Errorf("%s: %s.type: %w", key, name, err)
		}
		inst.Type = strings.ToLower(inst.Type)
		if inst.ModelName, err = asTrimmedString(obj["model_name"]); err != nil {
			return fmt.Errorf("%s: %s.model_name: %w", key, name, err)
		}
		if inst.Host, err = asTrimmedString(obj["host"]); err != nil {
			return fmt.Errorf("%s: %s.host: %w", key, name, err)
		}
		if inst.APIKey, err = asTrimmedString(obj["api_key"]); err != nil {
			return fmt.Errorf("%s: %s.api_key: %w", key, name, err)
		}
		// host gate (card t_9825147c part 2): a stored row keeps its
		// endpoint; a new row is either hostless (the provider
		// default, loopback) or carries a host config.yaml already
		// serves - that is a rename of a stored row, never a planted
		// endpoint reaching a fresh network target.
		existing, has := cfg.Providers[name]
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
			// "****" = keep the EXISTING secret. On a known name that
			// is the row's own key; on a rename it inherits the
			// submitted host's stored key - exactly one source, else
			// the secret is config.yaml's to name.
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
			// clearing a stored secret is a credential change too
			if has && existing.APIKey != "" {
				return instancesDoor(name, "api_key")
			}
		default:
			// a plain key: an identical echo of the stored secret is a
			// no-op, anything else is a credential write - config.yaml
			if !(has && existing.APIKey == inst.APIKey) {
				return instancesDoor(name, "api_key")
			}
		}
		providers[name] = inst
	}
	if len(providers) == 0 {
		// An emptied list removes every instance - the writer omits
		// the nil map entirely.
		cfg.Providers = nil
		return nil
	}
	cfg.Providers = providers
	return nil
}

// storedHostIn reports whether config already serves the host (the
// rename path's anchor, card t_9825147c part 2).
func storedHostIn(cfg *Config, host string) bool {
	for _, inst := range cfg.Providers {
		if inst.Host == host {
			return true
		}
	}
	return false
}

// renamedOffKey finds the secret a masked round-trip on a NEW name
// inherits (card t_9825147c part 2): the stored rows this submission
// REMOVES (names not re-sent) that carry the submitted host. A rename
// removes the old name, so its key is the source - one distinct key
// inherits, none or several refuse at config.yaml.
func renamedOffKey(cfg *Config, submitted map[string]bool, want string) (string, int) {
	keys := map[string]bool{}
	for name, inst := range cfg.Providers {
		if submitted[name] || inst.Host != want {
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

// asInt coerces a submitted JSON value into an int. JSON decodes every
// number as float64; integral values round-trip exactly, non-integral
// ones are rejected.
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
	default:
		return fmt.Errorf("unknown setting: %s", key)
	}
	return nil
}

// writeConfigFile persists the config as YAML, atomically (temp file +
// rename) and mode-preserving: a config.yaml can hold an api_key, so a
// fresh file is 0600 and an existing file keeps its permissions.
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

// hotApply runs the knobs the running process can use without a
// restart: the shared HTTP client timeout, the compactor's config
// (Compact re-reads it on every call), the files surface's default
// workspace - hot because the resolution law reads it per request, so
// a settings-pane save re-roots the Files tab live (but only on a
// VALUE CHANGE, prevWorkspace is the knob as the load saw it before
// this save/reload applied - card t_9825147c part 3) - the hygiene and
// skills capsules' model slots (SetConfig, re-read per pass), and the
// serving agents' system_prompt / max_turns through the boot-captured
// agent hook (card t_f78acfd6). WatchConfig's on-disk reload goes
// through the same path, so a hand edit and a settings-pane save
// behave identically.
func hotApply(cfg Config, prevWorkspace string, pctx *agent.Context, setFilesWorkspace func(string), setChannelWorkspace func(string), setTerminalWorkspace func(string), setAgentConfig func(string, int)) {
	llm.SetHTTPTimeout(cfg.HTTPTimeout)
	if pctx == nil {
		return
	}
	// default_workspace rides the files capsule's knob hook (the
	// shell-ring shrink wave 1: the files routes own the root
	// resolution law, the channels stopped carrying the knob). The
	// hook is a bare func threaded from the mount (the continuous
	// capsule's setAgents shape): nil means no files capsule is
	// mounted and the knob stays where it was. No service-registry
	// key - the offer namespaces stay closed at fourteen.
	if cfg.Server.DefaultWorkspace != prevWorkspace {
		if setFilesWorkspace != nil {
			setFilesWorkspace(cfg.Server.DefaultWorkspace)
		}
		// The same knob re-roots the tools' fallback workdir on the
		// HTTP channel (card t_ccf74d88): a workspace-less session's
		// tool runs follow the live save, not the boot copy.
		if setChannelWorkspace != nil {
			setChannelWorkspace(cfg.Server.DefaultWorkspace)
		}
		// The terminal capsule rides the same knob (card t_62d72028):
		// the pane's spawn workspace chain reads it per claim, so the
		// next spawned shell lands in the freshly saved workspace.
		if setTerminalWorkspace != nil {
			setTerminalWorkspace(cfg.Server.DefaultWorkspace)
		}
	}
	svc := pctx.Services().Resolve("compactor")
	if c, ok := svc.(*compactor.Compactor); ok {
		c.SetConfig(compactionSection(cfg))
	}
	// Approvals goes live (card t_34cd1921): the smart approval gate
	// re-reads its config per decision, so the hot-apply path hands it
	// a fresh section - the host resolves the slot/host/key knobs
	// (approvalSlot) just like at boot; the gate swaps the value
	// behind its lock. Not mounted (mode off at boot) = a nil
	// resolve, no-op: re-mounting stays out of scope.
	if a, ok := pctx.Services().Resolve("approvals").(*approval.Gate); ok {
		a.SetConfig(approvalSection(cfg))
	}
	// The store's hygiene pass goes live (card t_f78acfd6): the sweep
	// resolves its model slot + retention window from the live section
	// (store.SetHygieneConfig), so a summarizer_model flip reaches the
	// next sweep. Unmounted store (the host's own supply) = a nil
	// resolve, no-op.
	if s, ok := pctx.Services().Resolve("store").(*store.Store); ok {
		s.SetHygieneConfig(sessionsSection(cfg))
	}
	// The skills curator does the same for its consult slot: the next
	// curator action resolves provider.skills_model live. Consumer law
	// (the noun test): the host declares its own local face, the
	// capsule's type name never leaves harness/.
	if sp, ok := pctx.Services().Resolve("skills").(curatorSlotSetter); ok {
		sp.SetCuratorModel(skillsSection(cfg).CuratorModel)
	}
	// The runtime knobs land on the AGENTS (card t_f78acfd6, the probe
	// verdict): the loop re-reads prompt.Custom and the turn cap per
	// Run, so a system_prompt / max_turns save answers with the new
	// values at the next run - through the one boot-captured hook that
	// covers the host-owned agent AND the HTTP channel's serving
	// agent. Nil hook = the surface is not mounted (host fallback
	// supply, tests).
	if setAgentConfig != nil {
		setAgentConfig(cfg.SystemPrompt, cfg.MaxTurns)
	}
	// The cron fire's cap follows the operator's knob (card
	// t_e635df3a): a fire bounds a hung turn with
	// server.run_timeout, and only this host knows which config file
	// it booted on - so every save/reload hands the resolution to the
	// scheduler instead of leaving it to re-guess a home.
	pushRunTimeout(pctx, cfg)
}

// runTimeoutSetter is the host's consumer-owned face for the cron
// capsule's fire cap (card t_e635df3a): hotApply declares its need
// here, the capsule satisfies it with SetRunTimeout - the owner name
// never leaves harness/ (the curatorSlotSetter discipline).
type runTimeoutSetter interface {
	SetRunTimeout(time.Duration)
}

// pushRunTimeout hands the cron runner the server.run_timeout this
// host resolved - the config file the process booted on, an explicit
// `--config` path included - so a fire's cap follows the operator's
// knob even when the module's own home candidates point elsewhere
// (card t_e635df3a). Both the boot push and every settings save/reload
// ride this one helper (the setAgentConfig shape). A nil context or an
// unmounted cron capsule is a no-op: the fire keeps its own
// home-candidate read, then the 30-minute default.
func pushRunTimeout(pctx *agent.Context, cfg Config) {
	if pctx == nil {
		return
	}
	if rt, ok := pctx.Services().Resolve("cron").(runTimeoutSetter); ok {
		rt.SetRunTimeout(time.Duration(cfg.Server.RunTimeout) * time.Second)
	}
}

// curatorSlotSetter is the host's consumer-owned face for the skills
// capsule's live consult slot (card t_f78acfd6): hotApply declares its
// need here, the capsule satisfies it with SetCuratorModel - the owner
// name never leaves harness/ (the noun test holds the namespace).
type curatorSlotSetter interface {
	SetCuratorModel(model string)
}

// omegaSettings implements core.Settings for the serve host: the webui
// settings pane rides the http_channel through this.
type omegaSettings struct {
	path                 string
	pctx                 *agent.Context
	setFilesWorkspace    func(string)      // the files capsule's hot knob, nil when it is not mounted
	setChannelWorkspace  func(string)      // the HTTP channel's default-workspace hot knob, nil when unset
	setTerminalWorkspace func(string)      // the terminal capsule's default-workspace hot knob (t_62d72028), nil when unset
	setAgentConfig       func(string, int) // the serving agents' runtime-knob hot hook (t_f78acfd6)
	maxTurns             func() int        // the serving agents' LIVE turn cap (t_f78acfd6 GETTER), the agent scopes' inherited max_turns
}

func (s *omegaSettings) Schema() (*core.SettingsSchema, error) {
	schema, err := settingsPayload(s.path, s.maxTurns)
	if err != nil {
		return nil, err
	}
	// Read-only hint for the auto-resolved compaction window: the pane
	// shows what startup probed the provider with as part of the
	// knob's help line. No probe here - only the value cached at
	// startup.
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

// Raw returns the config FILE as YAML text for the pane's export and
// edit-as-YAML surfaces (card t_a82d39c8). The bytes are the
// operator's own document - comments, key order and style included -
// with each api_key VALUE replaced by the pane's mask: the config file
// is a credential's only home, so the unauthenticated wire never
// carries one. A file that will not parse answers the parse error
// instead of its text (an unmaskable document is a leak, not a read).
func (s *omegaSettings) Raw() (string, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return "", err
	}
	return maskConfigSecrets(string(data))
}

// SaveRaw validates a whole YAML document and persists it through the
// same path a knob save rides: the same LoadConfig layer (built-in
// defaults + the document + OMEGA_* overrides), the same Validate, the
// same atomic writer, the same hot-apply. A submitted mask at a secret
// path KEEPS the value already on disk (the provider.instances
// round-trip law), so opening the editor from an export and saving it
// back can never destroy a credential.
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
	// the credential config.yaml will hold: the document's own value, or
	// the masked value the file already carries. Neither the env layer
	// nor the mask itself ever lands on disk.
	if onDisk, derr := fileConfig(s.path); derr == nil {
		settleSecrets(&cfg, &doc, &onDisk)
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

// Reset restores every knob that HAS a default to it (card t_a82d39c8)
// through saveSettings - the pane's own PUT path, so the write
// validates, persists and hot-applies exactly like a hand save.
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

// resetValues is the write map the reset-to-default action ships: the
// schema's own Default for every knob that has one, minus the gated
// doors. The defaults come from settingGroups(defaultConfig()) - the
// layer beneath the file, the same field the pane's per-knob reset
// compares against - so no second default set exists anywhere.
//
// The map is normalized through JSON before it ships: applySettingValues
// expects the shape a PUT body decodes to (numbers as float64), and the
// struct-typed Go values in the schema are not that shape.
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

// fileConfig loads the config FILE alone: the built-in defaults with
// the document overlaid and NO environment layer. LoadConfig is the
// wrong reader for the mask-keep law - it answers the effective value,
// which may come from OMEGA_* rather than from the file.
func fileConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return documentConfig(string(data))
}

// documentConfig parses ONE YAML document over the built-in defaults,
// with no environment layer: it answers what the text itself says.
func documentConfig(text string) (Config, error) {
	cfg := DefaultConfig()
	if err := yaml.Unmarshal([]byte(text), &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

// configFromYAML parses a submitted document exactly the way LoadConfig
// parses the file: defaults, then the document, then the environment
// overrides. Validate is the caller's, so the layer order stays in one
// place.
func configFromYAML(text string) (Config, error) {
	cfg, err := documentConfig(text)
	if err != nil {
		return Config{}, err
	}
	applyEnv(&cfg)
	return cfg, nil
}

// settleSecrets pins the credential layer a raw save writes: the
// document's own value wins (a literal key typed in the editor), the
// mask keeps what the file already holds, and an OMEGA_*-supplied
// secret never lands in config.yaml - the variable is its source, and a
// copy in the file would outlive it. Clearing the line therefore clears
// the credential, the way a text editor would.
func settleSecrets(cfg *Config, doc *Config, onDisk *Config) {
	cfg.Provider.APIKey = secretLayerValue(doc.Provider.APIKey, onDisk.Provider.APIKey)
	for name, inst := range cfg.Providers {
		inst.APIKey = secretLayerValue(doc.Providers[name].APIKey, onDisk.Providers[name].APIKey)
		cfg.Providers[name] = inst
	}
}

// secretLayerValue is the one rule: the mask stands for the value on
// disk, anything else is what the document says.
func secretLayerValue(docValue, diskValue string) string {
	if docValue == maskedSecret {
		return diskValue
	}
	return docValue
}

// maskConfigSecrets renders a config document with every api_key value
// masked, every other byte (comments, order, flow/block style) kept.
// The walk is BY NAME, not by path: an api_key is a credential wherever
// it sits, so a provider-shaped section added later cannot introduce an
// unmasked one. An empty value stays empty - masking a missing key
// would imply one exists.
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

// --- omega settings: the terminal face of the settings pane ---------
//
// Card t_013b4643: the settings surface outside the webui. Reads call
// settingsSchema (the same function the pane's GET rides) and writes
// call saveSettings (the same function the pane's PUT rides) - one
// schema, one setter, one validator, one writer, so a CLI save and a
// pane save land byte-identically in config.yaml and every refusal
// (unknown knob, out-of-range value, a config.yaml-only door) is the
// seam's own sentence. The CLI's only job is argv: pick the knob,
// coerce the operator's text into the wire-typed value the pane would
// have sent, hand it to the seam.

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
validates exactly like a pane save; provider.api_key and server.bind
stay config.yaml's door, and a refusal writes nothing.
`

// cmdSettings dispatches the settings verbs. configPath is the --config
// flag (empty = the home's config), rest is the argv after the
// subcommand - the root owns the settings argv, so --config still
// works before or after the verb.
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

// settingsArgs drops the --config pair (the file selector, already
// parsed by the caller) from the verb argv. Every other token stays
// positional: a value is arbitrary text and may itself start with a
// dash only in string values, which the verb handlers guard.
func settingsArgs(rest []string) []string {
	out := make([]string, 0, len(rest))
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "--config":
			i++ // skip the value
		case strings.HasPrefix(a, "--config="):
			// selector only
		default:
			out = append(out, a)
		}
	}
	return out
}

// settingsConfigPath resolves the file both verbs read and write. The
// bare home without a config is the setup gate's refusal - a write
// against no file would have to invent one, and the seam never
// creates config from nothing.
func settingsConfigPath(flagPath string) (string, error) {
	path := resolveConfigPath(flagPath)
	if path == "" {
		return "", fmt.Errorf("no config found in %s - run `omega setup` first", omegaHome())
	}
	return path, nil
}

// settingsList prints every knob group: the group key as a header, one
// `key = value` line per knob. Strings are quoted so an empty value is
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

// settingsGet prints one knob's value, unquoted so a script reads it
// straight: `omega settings get provider.model_name`. An unknown key is
// loud - a silent empty line would look like an empty setting.
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
// The refreshed schema the seam returns is the receipt: the value is
// printed as it landed, not as it was typed.
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

// coerceSettingValue turns the operator's text into the JSON-shaped
// value the webui would submit for this knob: a toggle becomes a bool,
// a number a float64 (the wire's only number type), the instances list
// the JSON array the pane posts, everything else the string as typed.
//
// This is an input adapter, never a second validator: ranges, enum
// membership, absolute-path checks, the config.yaml-only doors and
// every cross-field rule stay behind saveSettings (applySettingValues +
// cfg.Validate), so the CLI can never accept what the pane refuses or
// refuse what the pane accepts. The one guard added here is finiteness:
// JSON cannot carry NaN/Inf, so the pane can never submit them while
// the CLI text can - and the seam's range test would let NaN through.
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

// settingsValueText renders a knob value for the list and the set
// receipt: strings quoted (an empty value stays visible), numbers in
// plain decimal, everything structured as JSON.
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

// settingsRawText renders a single knob's value for script consumption:
// a string comes back bare, everything else as settingsValueText prints
// it.
func settingsRawText(k core.SettingsKnob) string {
	if s, ok := k.Value.(string); ok {
		return s
	}
	return settingsValueText(k)
}
