package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
	"github.com/EndoTheDev/omega/engine/ai"
	"github.com/EndoTheDev/omega/modules/compactor"
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
	cfg, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}
	// the whole payload is headed for the unauthenticated /settings GET
	return &core.SettingsSchema{Groups: settingGroups(&cfg)}, nil
}

// settingGroups builds the full knob inventory from a config. Field
// order follows the config.yaml layout (provider, server, store,
// compaction, runtime, skills, memory, logging).
func settingGroups(cfg *Config) []core.SettingsGroup {
	return []core.SettingsGroup{
		{
			Key:   "provider",
			Label: "provider",
			Knobs: []core.SettingsKnob{
				{Key: "provider.type", Label: "type", Type: "select",
					Options: []string{"ollama", "openai", "anthropic"},
					Value:   strings.ToLower(cfg.Provider.Type),
					Help:    "provider flavor; empty defaults to ollama"},
				{Key: "provider.model_name", Label: "model name", Type: "text",
					Value: cfg.Provider.ModelName,
					Help:  "default model the agent serves; the webui model picker overrides per chat"},
				{Key: "provider.vision_model", Label: "vision model", Type: "text",
					Value: cfg.VisionModel,
					Help:  "captioner model for image messages (e.g. gemma4:31b); empty = images reach the main model unchanged and may fail loudly; applies on restart"},
				{Key: "provider.title_model", Label: "title model", Type: "text",
					Value: cfg.TitleModel,
					Help:  "cheap model for session titles; empty = the main model; applies on restart"},
				{Key: "provider.host", Label: "host", Type: "text",
					Value: cfg.Provider.Host,
					Help:  "provider base URL (default http://localhost:11434)"},
				{Key: "provider.api_key", Label: "api key", Type: "secret",
					Value: maskSecret(cfg.Provider.APIKey),
					Help:  "masked here; edit config.yaml instead (blank for local ollama)"},
			},
		},
		{
			Key:   "server",
			Label: "server",
			Knobs: []core.SettingsKnob{
				{Key: "server.port", Label: "port", Type: "number",
					Min: ptrF(1), Max: ptrF(math.MaxUint16), Step: ptrF(1),
					Value: cfg.Server.Port,
					Help:  "HTTP channel port; applies on restart"},
				{Key: "server.bind", Label: "bind addresses", Type: "bindlist",
					Value: strings.Join(cfg.Server.Bind, ", "),
					Help:  "comma-separated listen addresses; loopback only by default (unauthenticated API); edit config.yaml instead (applies on restart)"},
				{Key: "server.run_timeout", Label: "run timeout (s)", Type: "number",
					Min: ptrF(1), Step: ptrF(1),
					Value: cfg.Server.RunTimeout,
					Help:  "wall-clock cap per detached chat run, in seconds; applies on restart"},
				{Key: "server.default_workspace", Label: "default workspace", Type: "text",
					Value: cfg.Server.DefaultWorkspace, Live: true,
					Help: "absolute path; the Files tab roots here when the focused session has no pinned workspace; empty = the process working directory; applies live"},
			},
		},
		{
			Key:   "store",
			Label: "store",
			Knobs: []core.SettingsKnob{
				{Key: "store.db_path", Label: "database path", Type: "text",
					Value: cfg.Store.DBPath,
					Help:  "SQLite file, relative to the working dir; applies on restart"},
			},
		},
		{
			Key:   "compaction",
			Label: "compaction",
			Knobs: compactionKnobs(cfg),
		},
		{
			Key:   "runtime",
			Label: "runtime",
			Knobs: []core.SettingsKnob{
				{Key: "system_prompt", Label: "system prompt", Type: "textarea",
					Value: cfg.SystemPrompt,
					Help:  "extra custom prompt lines appended to the system prompt; applies on restart"},
				{Key: "http_timeout", Label: "http timeout (s)", Type: "number",
					Min: ptrF(1), Step: ptrF(1),
					Value: cfg.HTTPTimeout,
					Help:  "LLM request timeout in seconds", Live: true},
				{Key: "max_turns", Label: "max turns", Type: "number",
					Min: ptrF(0), Step: ptrF(1),
					Value: cfg.MaxTurns,
					Help:  "tool-loop turn cap per chat; applies on restart"},
				{Key: "notifications", Label: "notifications", Type: "select",
					Options: []string{"bell", "desktop", "off"},
					Value:   notificationsValue(cfg.Notifications),
					Help:    "turn-complete notification (TUI)", Surface: "tui"},
				{Key: "theme", Label: "theme (TUI)", Type: "text",
					Value: cfg.Theme,
					Help:  "TUI theme name; the webui theme lives in this pane's theme select", Surface: "tui"},
			},
		},
		{
			Key:   "skills",
			Label: "skills",
			Knobs: []core.SettingsKnob{
				{Key: "skills.dir", Label: "skills directory", Type: "text",
					Value: cfg.Skills.Dir,
					Help:  "loaded at startup; applies on restart"},
			},
		},
		{
			Key:   "memory",
			Label: "memory",
			Knobs: []core.SettingsKnob{
				{Key: "memory.enabled", Label: "enabled", Type: "toggle",
					Value: cfg.Memory.Enabled,
					Help:  "persistent memory system; applies on restart"},
				{Key: "memory.user_profile_enabled", Label: "user profile enabled", Type: "toggle",
					Value: cfg.Memory.UserProfileEnabled,
					Help:  "user-profile memory file; applies on restart"},
				{Key: "memory.char_limit", Label: "char limit", Type: "number",
					Min: ptrF(1), Step: ptrF(1),
					Value: cfg.Memory.CharLimit,
					Help:  "memory file budget in characters; applies on restart"},
				{Key: "memory.user_char_limit", Label: "user profile char limit", Type: "number",
					Min: ptrF(1), Step: ptrF(1),
					Value: cfg.Memory.UserProfileCharLimit,
					Help:  "user-profile budget in characters; applies on restart"},
				{Key: "memory.file", Label: "memory file", Type: "text",
					Value: cfg.Memory.File, Help: "applies on restart"},
				{Key: "memory.user_file", Label: "user profile file", Type: "text",
					Value: cfg.Memory.UserProfileFile, Help: "applies on restart"},
			},
		},
		{
			Key:   "logging",
			Label: "logging",
			Knobs: []core.SettingsKnob{
				{Key: "logging.enabled", Label: "enabled", Type: "toggle",
					Value: cfg.Logging.Enabled,
					Help:  "operational log; applies on restart"},
				{Key: "logging.file", Label: "log file", Type: "text",
					Value: cfg.Logging.File,
					Help:  "applies on restart"},
			},
		},
	}
}

// compactionKnobs renders the agent.CompactionConfig section. Every
// compaction knob is read per Compact call, so all hot-apply live.
func compactionKnobs(cfg *Config) []core.SettingsKnob {
	c := &cfg.Compaction
	return []core.SettingsKnob{
		{Key: "compaction.enabled", Label: "enabled", Type: "toggle",
			Value: c.Enabled,
			Help:  "summarize history when the token budget is exceeded", Live: true},
		{Key: "compaction.threshold", Label: "threshold", Type: "number",
			Min: ptrF(0.05), Max: ptrF(1), Step: ptrF(0.05),
			Value: c.Threshold,
			Help:  "fraction of the context window that triggers compaction", Live: true},
		{Key: "compaction.context_window", Label: "context window", Type: "number",
			Min: ptrF(0), Step: ptrF(1),
			Value: c.ContextWindow,
			Help:  "model context window in tokens the budget is measured against; 0 = auto (probed from the provider at startup)", Live: true},
		{Key: "compaction.keep_first", Label: "keep first", Type: "number",
			Min: ptrF(0), Step: ptrF(1),
			Value: c.KeepFirst,
			Help:  "messages kept unsummarized at the head", Live: true},
		{Key: "compaction.keep_last", Label: "keep last", Type: "number",
			Min: ptrF(0), Step: ptrF(1),
			Value: c.KeepLast,
			Help:  "messages kept unsummarized at the tail", Live: true},
		{Key: "compaction.reserve_tokens", Label: "reserve tokens", Type: "number",
			Min: ptrF(1), Step: ptrF(1),
			Value: c.ReserveTokens,
			Help:  "tokens reserved for the summary response", Live: true},
		{Key: "compaction.max_tool_output", Label: "max tool output", Type: "number",
			Min: ptrF(1), Step: ptrF(1),
			Value: c.MaxToolOutput,
			Help:  "tool-result truncation length in characters; applies on restart"},
	}
}

func ptrF(n float64) *float64 { return &n }

// notificationsValue normalizes an unset notifications value to the
// default the TUI ends up using, so the select never shows a stray
// empty option.
func notificationsValue(v string) string {
	if strings.TrimSpace(v) == "" {
		return "bell"
	}
	return strings.TrimSpace(v)
}

// saveSettings applies values (dotted yaml path -> value), validates,
// and writes config.yaml atomically, then hot-applies the live knobs to
// the running context. Returns the refreshed schema payload.
func saveSettings(path string, pctx *agent.Context, values map[string]any) (*core.SettingsSchema, error) {
	cfg, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}
	if err := applySettingValues(&cfg, values); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if err := writeConfigFile(path, &cfg); err != nil {
		return nil, err
	}
	hotApply(cfg, pctx)
	return &core.SettingsSchema{Groups: settingGroups(&cfg)}, nil
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
			// else must be an absolute, existing directory - the same
			// shape a session workspace pin validates, validated HERE
			// so a typo never silently roots nothing
			v, verr := asTrimmedString(raw)
			if verr != nil {
				err = fmt.Errorf("%s: %w", key, verr)
				break
			}
			if v == "" {
				cfg.Server.DefaultWorkspace = ""
				break
			}
			if !filepath.IsAbs(v) {
				err = fmt.Errorf("%s: must be an absolute path, got %q", key, v)
				break
			}
			abs, aerr := filepath.EvalSymlinks(filepath.Clean(v))
			if aerr != nil {
				err = fmt.Errorf("%s: %w", key, aerr)
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
			cfg.Server.DefaultWorkspace = abs
		case "store.db_path", "system_prompt", "theme",
			"skills.dir", "memory.file", "memory.user_file", "logging.file":
			err = setStringField(cfg, key, raw)
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
		case "notifications":
			s, serr := asTrimmedString(raw)
			if serr != nil {
				err = fmt.Errorf("%s: %w", key, serr)
				break
			}
			switch strings.ToLower(s) {
			case "bell", "desktop", "off":
				cfg.Notifications = strings.ToLower(s)
			default:
				err = fmt.Errorf("%s: %q is not one of bell, desktop, off", key, s)
			}
		case "memory.char_limit":
			err = setInt(&cfg.Memory.CharLimit, key, raw, 1, math.MaxInt32)
		case "memory.user_char_limit":
			err = setInt(&cfg.Memory.UserProfileCharLimit, key, raw, 1, math.MaxInt32)
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
	case "provider.host":
		cfg.Provider.Host = v
	case "store.db_path":
		cfg.Store.DBPath = v
	case "system_prompt":
		cfg.SystemPrompt = v
	case "theme":
		cfg.Theme = v
	case "skills.dir":
		cfg.Skills.Dir = v
	case "memory.file":
		cfg.Memory.File = v
	case "memory.user_file":
		cfg.Memory.UserProfileFile = v
	case "logging.file":
		cfg.Logging.File = v
	default:
		return fmt.Errorf("unknown setting: %s", key)
	}
	return nil
}

// trim normalizes plain string inputs: surrounding whitespace is never
// significant in any config field.
func trim(s string) string { return strings.TrimSpace(s) }

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
		os.Remove(tmp) //nolint:errcheck // best-effort cleanup
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

// hotApply runs the knobs the running process can use without a
// restart: the shared HTTP client timeout, the compactor's config
// (Compact re-reads it on every call), and the files surface's default
// workspace - hot because the resolution law reads it per request, so
// a settings-pane save re-roots the Files tab live. WatchConfig's
// on-disk reload goes through the same path, so a hand edit and a
// settings-pane save behave identically.
func hotApply(cfg Config, pctx *agent.Context) {
	ai.SetHTTPTimeout(cfg.HTTPTimeout)
	if pctx == nil {
		return
	}
	// default_workspace rides the mounted channels (SetDefaultWorkspace
	// is an http_channel capability): every bind's server roots on it
	// the next file request. The seam-style assertion mirrors the
	// compactor's type check below - only channels that opt in move.
	for _, ch := range pctx.Channels {
		if setter, ok := ch.(interface{ SetDefaultWorkspace(string) }); ok {
			setter.SetDefaultWorkspace(cfg.Server.DefaultWorkspace)
		}
	}
	if pctx.Compactor == nil {
		return
	}
	if c, ok := pctx.Compactor.(*compactor.Compactor); ok {
		c.SetConfig(cfg.Compaction)
	}
}

// omegaSettings implements core.Settings for the serve host: the webui
// settings pane rides the http_channel through this.
type omegaSettings struct {
	path string
	pctx *agent.Context
}

func (s *omegaSettings) Schema() (*core.SettingsSchema, error) {
	schema, err := settingsSchema(s.path)
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
	c, ok := s.pctx.Compactor.(*compactor.Compactor)
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
	return saveSettings(s.path, s.pctx, values)
}

// Compile-time assertion that omegaSettings implements core.Settings.
var _ core.Settings = (*omegaSettings)(nil)
