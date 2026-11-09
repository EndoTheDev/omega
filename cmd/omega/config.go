package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	agentapi "github.com/EndoTheDev/omega/agent/api"
	"github.com/EndoTheDev/omega/api"
	"github.com/EndoTheDev/omega/harness/approval"
	"github.com/EndoTheDev/omega/harness/continuous"
	files "github.com/EndoTheDev/omega/harness/files"
	kanbanModule "github.com/EndoTheDev/omega/harness/kanban"
	"github.com/EndoTheDev/omega/harness/logging"
	"github.com/EndoTheDev/omega/harness/memory"
	"github.com/EndoTheDev/omega/harness/process"
	"github.com/EndoTheDev/omega/harness/provider"
	"github.com/EndoTheDev/omega/harness/skills"
	"github.com/EndoTheDev/omega/harness/store"
	"github.com/EndoTheDev/omega/harness/trust"
	"gopkg.in/yaml.v3"
	"time"
)

// Config holds the runtime configuration. It is loaded from
// config.yaml, then overridden by environment variables, then defaults
// are applied for anything still unset.
type Config struct {
	Provider ProviderConfig `yaml:"provider"`
	// Providers are the NAMED provider instances (card t_7dcd4280):
	// the map key IS the instance id, so two accounts of one type are
	// two entries. The top-level provider: section stays the DEFAULT
	// instance the agent serves; "default" is a reserved instance
	// name (it would read as the provider: section). A session or
	// run pinned to an instance that is gone from this map fails
	// loudly (config: unknown instance) - never a silent fallback.
	Providers    map[string]ProviderConfig `yaml:"providers,omitempty"`
	Server       ServerConfig              `yaml:"server"`
	Store        StoreConfig               `yaml:"store"`
	Compaction   agent.CompactionConfig    `yaml:"compaction"`
	SystemPrompt string                    `yaml:"system_prompt"`
	// VisionModel is the auxiliary captioner (vision_model knob): when
	// set, image-carrying user messages are described once with this
	// model and the main model sees the caption as text. The special
	// value "default" (provider.DefaultAuxModel) follows whichever
	// model the current run serves - the composer's chip at caption
	// time - and persists as the literal word, never a frozen model
	// name. Empty keeps today's behavior - images ride to the main
	// provider and a model that cannot see them fails loudly.
	VisionModel string `yaml:"vision_model"`
	// TitleModel is the auxiliary title generator (title_model knob):
	// maybeTitleSession's cheap calls (provisional + final) route
	// through it when set. Empty = the main model (today's behavior).
	TitleModel string `yaml:"title_model"`
	// The four per-subsystem model slots (card t_fc0b2f2d): provider
	// settings assigns one named model per subsystem - compaction,
	// summarizer, skills curator, approval gate - and the four call
	// sites resolve it through agent.ParseModelSlot (the
	// "instance:model" grammar; empty = the consumer's default). The
	// schema keys ride the provider group (provider.*_model),
	// mirroring vision_model / title_model: top-level knobs, one
	// settings home, no per-section duplicates.
	CompactionModel string            `yaml:"compaction_model"`
	SummarizerModel string            `yaml:"summarizer_model"`
	SkillsModel     string            `yaml:"skills_model"`
	ApprovalsModel  string            `yaml:"approvals_model"`
	HTTPTimeout     int               `yaml:"http_timeout"`
	MaxTurns        int               `yaml:"max_turns"`
	Skills          SkillsConfig      `yaml:"skills"`
	Sessions        SessionsConfig    `yaml:"sessions"`
	Memory          MemoryConfig      `yaml:"memory"`
	Logging         LoggingConfig     `yaml:"logging"`
	Web             WebConfig         `yaml:"web"`
	Kanban          KanbanConfig      `yaml:"kanban"`
	Continuous      continuous.Config `yaml:"continuous"`
	Approvals       ApprovalConfig    `yaml:"approvals"`
	Process         ProcessConfig     `yaml:"process"`
	// Disabled is the config.yaml `disabled:` toggle fold (v0.12 E1,
	// grill Q1; home moved from the omega.json sidecar to this key in
	// the v0.13 shell law 3 - the sidecar folds at boot). v0.14.1
	// keyfolds it: one key, one namespace per registry (modules keeps
	// the capsule toggle list the mount machinery owns; skills, tools,
	// agents are the registry-browser lists). A pre-v0.14 flat list
	// (the shape every earlier boot wrote) unmarshals into the modules
	// namespace - older config.yaml keeps meaning the same thing.
	Disabled DisabledNamespaces `yaml:"disabled"`
}

// DisabledNamespaces is the config.yaml `disabled:` fold shape: one
// top-level key, one namespace per registry. The mount machinery owns
// modules (the catalog list boot's filterDisabled reads); skills,
// tools, and agents are file/folder registries the host filters at
// their own seams. An all-off namespace is never written: an empty
// list carries no state, and an all-empty key carries none at all.
type DisabledNamespaces struct {
	Modules    []string `yaml:"modules"`
	SkillNames []string `yaml:"skills"`
	Tools      []string `yaml:"tools"`
	Agents     []string `yaml:"agents"`
}

// empty reports whether every namespace is nil. Slices make the struct
// non-comparable, so the writer asks instead of comparing. The skills
// namespace guards early-return so its scrub deletes a whole statement
// block, never a boolean operand inside the return expression
// (nm-purity scrub shape).
func (d DisabledNamespaces) empty() bool {
	if len(d.SkillNames) > 0 {
		return false
	}
	return len(d.Modules) == 0 && len(d.Tools) == 0 && len(d.Agents) == 0
}

// UnmarshalYAML accepts both shapes the config.yaml `disabled:` key
// has ever carried: the v0.12-v0.13 flat sequence (a legacy module
// list - the only namespace earlier boots wrote) and the v0.14.1
// mapping fold. Anything else fails loud: a shape the toggle writer
// did not produce must not decode to silence.
func (d *DisabledNamespaces) UnmarshalYAML(node *yaml.Node) error {
	switch {
	case node.Kind == yaml.DocumentNode && len(node.Content) == 1:
		return d.UnmarshalYAML(node.Content[0])
	case node.Kind == yaml.SequenceNode:
		var flat []string
		if err := node.Decode(&flat); err != nil {
			return err
		}
		*d = DisabledNamespaces{Modules: flat}
		return nil
	case node.Kind == yaml.MappingNode:
		// zero-value first: a re-used decode target must not inherit
		// namespaces an earlier parse left in.
		type fold DisabledNamespaces
		*d = DisabledNamespaces{}
		var f fold
		if err := node.Decode(&f); err != nil {
			return err
		}
		*d = DisabledNamespaces(f)
		return nil
	case node.Tag == "!!null":
		*d = DisabledNamespaces{}
		return nil
	}
	return fmt.Errorf("config: disabled: must be a list (legacy module fold) or a key fold - got a %s", yamlDocKind(node))
}

// yamlDocKind names a node for the disabled: decode failure only - the
// error string a malformed config deserves.
func yamlDocKind(node *yaml.Node) string {
	switch node.Kind {
	case yaml.DocumentNode:
		return "document"
	case yaml.SequenceNode:
		return "sequence"
	case yaml.MappingNode:
		return "mapping"
	case yaml.ScalarNode:
		return "scalar"
	default:
		return "unknown"
	}
}

// sortedCopy returns a sorted copy of ns (stable yaml output); a nil
// slice stays nil so the write fold can omit it.
func sortedCopy(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	out := append([]string(nil), names...)
	sort.Strings(out)
	return out
}

// cleanNamespaces returns the fold state an empty namespace never
// keeps: empty lists become nil (the writer omits empty namespaces,
// the all-empty key omits itself).
func cleanNamespaces(d DisabledNamespaces) DisabledNamespaces {
	return DisabledNamespaces{
		Modules:    sortedCopy(d.Modules),
		SkillNames: sortedCopy(d.SkillNames),
		Tools:      sortedCopy(d.Tools),
		Agents:     sortedCopy(d.Agents),
	}
}

// WebConfig carries optional overrides for the web tools. BaseURL, when
// set, overrides the web.search/web.fetch endpoint host; otherwise
// ConfigSection derives both from provider.host (arc 2b: the host
// exposes sections, the router pulls what each plugin declared).
type WebConfig struct {
	BaseURL string `yaml:"base_url"`
}

// SkillsConfig controls where skills are loaded from.
type SkillsConfig struct {
	Dir string `yaml:"dir"`
	// SharedDir is the root skills/shared home, scanned first (card
	// t_7e2b7002): every agent sees it; an agent's own dir shadows.
	SharedDir string `yaml:"shared_dir"`
	// NudgeInterval is the learning-loop reminder cadence (card
	// t_9972dfd0): tool iterations between "persist a lesson"
	// reminders in turn prep. 0 = nudging off. Default 15.
	NudgeInterval int `yaml:"nudge_interval"`
	// The curator's model moved to provider settings (card
	// t_fc0b2f2d): it rides the top-level skills_model slot, routed
	// into the capsule section by ConfigSection.
}

// MemoryConfig controls the persistent memory system.
type MemoryConfig struct {
	Enabled              bool   `yaml:"enabled"`
	UserProfileEnabled   bool   `yaml:"user_profile_enabled"`
	CharLimit            int    `yaml:"char_limit"`
	UserProfileCharLimit int    `yaml:"user_char_limit"`
	File                 string `yaml:"file"`
	UserProfileFile      string `yaml:"user_file"`
}

// LoggingConfig controls the operational logging system.
type LoggingConfig struct {
	Enabled bool   `yaml:"enabled"`
	File    string `yaml:"file"`
}

// ProviderConfig configures the LLM provider connection.
type ProviderConfig struct {
	Type      string `yaml:"type"`
	ModelName string `yaml:"model_name"`
	Host      string `yaml:"host"`
	APIKey    string `yaml:"api_key"`
}

// validInstanceName is the instance-id syntax law (card t_7dcd4280):
// conservative lower case so the id is safe in every consumer (config
// keys, session pins, model-selection slots). Reserved name "default"
// is checked by Validate separately.
func validInstanceName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return name[0] >= 'a' && name[0] <= 'z' || name[0] >= '0' && name[0] <= '9'
}

// ServerConfig configures the HTTP listener(s). Bind is the list of
// addresses the channel listens on (default: loopback only). Additional
// addresses - e.g. the Tailscale IP so Traefik can route to omega - are
// an explicit opt-in, never a default. RunTimeout caps each detached
// /chat run's wall-clock time, in seconds: a hung tool call must not
// brick the session's run slot until restart.
type ServerConfig struct {
	Port       int      `yaml:"port"`
	Bind       []string `yaml:"bind"`
	RunTimeout int      `yaml:"run_timeout"`
	// PublicHosts (config key public_hosts) declares the public
	// identities this serve answers to - domains fronted by a reverse
	// proxy or tunnel (the tunnel forwards the browser's Host, which
	// never matches the bind, so the Host guard refuses them without
	// this declaration). Exact names, no wildcards.
	PublicHosts []string `yaml:"public_hosts"`
	// DefaultWorkspace (config key default_workspace) is the tools'
	// fallback working directory (card t_ccf74d88): a session without a
	// pinned workspace roots its tool runs (shell, files, processes)
	// here instead of the process working directory, and the Files tab
	// roots here when the focused session has no pin - both consumers
	// share the one knob. Empty = unset - the cwd fallback
	// stands (the tool-rooting law lives in agent/api's prepareRun).
	DefaultWorkspace string `yaml:"default_workspace"`
}

// StoreConfig configures the SQLite session store.
type StoreConfig struct {
	DBPath string `yaml:"db_path"`
}

// SessionsConfig configures the sessions hygiene pass (card t_329b9312):
// the LLM session summarizer and the retention window. Mirrors
// store.HygieneConfig without importing the module knob-for-knob - the
// router below copies the fields.
type SessionsConfig struct {
	// RetentionDays prunes sessions older than this. 0 = never prune.
	RetentionDays int `yaml:"retention_days"`

	// SummarizeAfterHours is the minimum age before the summarizer
	// writes a session's summary.
	SummarizeAfterHours int `yaml:"summarize_after_hours"`
}

// KanbanConfig configures the board database. The relative default
// (kanban.db) resolves against OMEGA_HOME in the plugin mount.
type KanbanConfig struct {
	DBPath string `yaml:"db_path"`
}

// ApprovalConfig configures the approval gate (card t_c2b52cb5):
// mode off|manual|smart and the operator policy text. THE MODEL moved
// to provider settings (card t_fc0b2f2d): the decision model rides the
// top-level approvals_model slot; ConfigSection resolves it into the
// approval capsule's wiring (the capsule keeps the field, the config
// key is gone). Type name carries the host's config vocabulary for the
// nm-purity scrubber (SharedTypes law).
type ApprovalConfig struct {
	Mode        string `yaml:"mode"`
	SmartPolicy string `yaml:"smart_policy"`
}

// ProcessConfig configures the background process registry (card
// t_5782d11e). Zero values are valid: the module applies its own
// defaults, so an absent `process:` section rides the constants
// (DefaultLogMaxBytes, DefaultKillGraceSeconds).
type ProcessConfig struct {
	LogMaxBytes      int64 `yaml:"log_max_bytes"`
	KillGraceSeconds int   `yaml:"kill_grace_seconds"`
}

// DefaultConfig returns the configuration used when neither YAML nor
// environment variables provide a value.
func DefaultConfig() Config {
	return Config{
		Provider: ProviderConfig{
			Host: "http://localhost:11434",
		},
		Server: ServerConfig{
			Port:       8099,
			RunTimeout: 1800,
		},
		Store: StoreConfig{
			DBPath: "omega.db",
		},
		Sessions: SessionsConfig{
			RetentionDays:       store.DefaultHygiene().RetentionDays,
			SummarizeAfterHours: store.DefaultHygiene().SummarizeAfterHours,
		},
		Kanban: KanbanConfig{
			DBPath: "kanban.db",
		},
		Compaction: agent.CompactionConfig{
			Enabled:       true,
			Threshold:     0.6,
			ContextWindow: 32768,
			KeepFirst:     2,
			KeepLast:      10,
			ReserveTokens: 16384,
			MaxToolOutput: 32768,
		},
		Skills: SkillsConfig{
			Dir:           "skills",
			NudgeInterval: skills.DefaultNudgeInterval,
		},
		Memory: MemoryConfig{
			Enabled:              true,
			UserProfileEnabled:   true,
			CharLimit:            2200,
			UserProfileCharLimit: 1375,
			File:                 "memory.md",
			UserProfileFile:      "user.md",
		},
		Logging: LoggingConfig{
			Enabled: true,
			// No default filename here: a bare relative name would
			// make any early consumer open a log file in the process
			// CWD. resolveHomePaths computes the home-rooted path
			// before the logger is created (card t_0ccda501).
			File: "",
		},
		HTTPTimeout: 300,
		MaxTurns:    100,
		// card t_c2b52cb5, the ship-dark default (Q5): manual IS the
		// explicit default - the knob render reads the field directly,
		// no helper (the pure-failure law: the approval vocabulary
		// must die whole under scripts/nm-purity.sh --module approval,
		// and a helper derived entirely from the config field leaves
		// an illegal empty func behind). approval.NormalizedMode
		// still treats empty as manual for yaml that wrote "".
		Approvals: ApprovalConfig{
			Mode: "manual",
		},
	}
}

// LoadConfig reads configuration from path (a config.yaml file), applies
// environment variable overrides, fills defaults, and validates the result.
// An empty path skips YAML loading entirely.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config: %w", err)
		}
	}

	applyEnv(&cfg)

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	// The toggle list lives in the config.yaml `disabled:` key (v0.13
	// shell law 3): fold any leftover omega.json sidecar in before the
	// decode, so one boot both migrates and honors the operator's off
	// list. A broken sidecar still fails the boot loud.
	folded, foldTarget, err := foldOmegaJSON(path)
	if err != nil {
		return Config{}, err
	}
	if path == "" {
		// No --config: skip YAML only when no config.yaml exists on
		// disk anywhere. A fold (or any prior boot) may have created
		// the default one - read it, like the omega.json sidecar
		// search always did for legacy homes.
		path = resolveConfigPath("")
		if path == "" && len(folded) > 0 {
			path = foldTarget
		}
	}

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config: %w", err)
		}
	}

	applyEnv(&cfg)

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// foldOmegaJSON migrates the v0.12 omega.json module toggle list into
// config.yaml's `disabled:` key (v0.13 shell law 3). Search order: the
// explicit config's dir, then home/config, then home - the order the
// sidecar was read in. Every omega.json found folds (union, first-wins
// dedupe) and is deleted; no silent drops. Entries a config.yaml that
// already carries the key would also name are reported in the boot
// warning but never double-written. Returns the folded entries and the
// config.yaml they landed in.
func foldOmegaJSON(configPath string) ([]string, string, error) {
	home := omegaHome()
	dirs := []string{}
	if configPath != "" {
		dirs = append(dirs, filepath.Dir(configPath))
	}
	dirs = append(dirs, home+"/config", home)

	var entries []string
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, "omega.json")
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, "", fmt.Errorf("read omega.json: %w", err)
		}
		var doc struct {
			Disabled []string `json:"disabled"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, "", fmt.Errorf("parse omega.json: %w", err)
		}
		if len(doc.Disabled) == 0 {
			// No toggle state: the sidecar is dead weight, bury it.
			if err := os.Remove(path); err != nil {
				return nil, "", fmt.Errorf("delete omega.json: %w", err)
			}
			continue
		}
		for _, name := range doc.Disabled {
			if !slices.Contains(entries, name) {
				entries = append(entries, name)
			}
		}
		if err := os.Remove(path); err != nil {
			return nil, "", fmt.Errorf("delete omega.json: %w", err)
		}
	}
	if len(entries) == 0 {
		return nil, "", nil
	}
	// The fold lands in the config the boot reads; with none on disk
	// anywhere, the config/ layout default gets created.
	target := configPath
	if target == "" {
		target = resolveConfigPath("")
		if target == "" {
			target = home + "/config/config.yaml"
		}
	}
	if err := writeDisabled(target, DisabledNamespaces{Modules: entries}); err != nil {
		return nil, "", err
	}
	// Boot warning, loud, naming the folded entries - the list moved
	// files and the operator should see it happen.
	fmt.Fprintf(os.Stderr, "omega: folded module disable list %v from omega.json into %s (disabled: key, v0.13 shell law 3)\n", entries, target)
	return entries, target, nil
}

// writeDisabled writes the config.yaml `disabled:` toggle fold: parse,
// modify, marshal at the yaml node level, so comments, key order, and
// unknown keys survive (the struct-marshal settings-save path
// normalizes; this one must not). The v0.14.1 fold shape is one
// `disabled:` mapping with a namespace key per registry; a namespace
// with no entries is never written, and when every namespace is empty
// the whole key goes away - an all-on home carries no toggle state. A
// legacy flat sequence under `disabled:` is REPLACED on the first
// write: the caller's fold always carries the authoritative full
// state (a partial write would re-shape history it did not read).
// Editing a missing config.yaml creates a minimal one holding just
// the disabled: key; an all-empty fold against a missing file writes
// nothing. A malformed config.yaml fails loud - a write that destroys
// the file boot must parse is worse than a failed toggle.
func writeDisabled(path string, fold DisabledNamespaces) error {
	fold = cleanNamespaces(fold)
	var mapping yaml.Node
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		if fold.empty() {
			return nil // nothing to say, nowhere to say it
		}
		mapping = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	case err != nil:
		return fmt.Errorf("read config: %w", err)
	default:
		if strings.TrimSpace(string(data)) != "" {
			var doc yaml.Node
			if err := yaml.Unmarshal(data, &doc); err != nil {
				return fmt.Errorf("parse config: %w", err)
			}
			if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
				mapping = *doc.Content[0]
			} else {
				mapping = doc
			}
		} else {
			mapping = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		}
	}
	if mapping.Kind != yaml.MappingNode {
		return fmt.Errorf("config: top-level document is not a mapping - cannot write the disabled: key into %s", path)
	}

	// disabledSeq renders the `disabled:` value node from the fold: a
	// mapping whose namespace keys are the NON-EMPTY lists (an empty
	// list never persists). Every empty fold returns nil - the key
	// goes away, an all-on home carries no toggle state.
	disabledSeq := func() *yaml.Node {
		if fold.empty() {
			return nil
		}
		sub := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, ns := range []struct {
			key   string
			names []string
		}{
			{"modules", fold.Modules},
			{"skills", fold.SkillNames},
			{"tools", fold.Tools},
			{"agents", fold.Agents},
		} {
			if len(ns.names) == 0 {
				continue
			}
			seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			for _, name := range ns.names {
				seq.Content = append(seq.Content,
					&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name})
			}
			sub.Content = append(sub.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: ns.key}, seq)
		}
		return sub
	}

	idx := -1
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Kind != yaml.ScalarNode {
			continue
		}
		if mapping.Content[i].Value == "disabled" {
			idx = i
			break
		}
	}
	val := disabledSeq()
	switch {
	case val == nil && idx >= 0:
		// All-on: the key goes away (an all-on home carries no state).
		mapping.Content = append(mapping.Content[:idx], mapping.Content[idx+2:]...)
	case val != nil && idx >= 0:
		// The fold replaces whatever shape the key carried (a legacy
		// flat sequence included): the caller's fold is the full state.
		mapping.Content[idx+1] = val
	case val != nil:
		// New key appends last: existing key order is untouched.
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "disabled"}, val)
	}
	out, err := yaml.Marshal(&mapping)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	mode := os.FileMode(0600)
	if fi, statErr := os.Stat(path); statErr == nil {
		mode = fi.Mode()
	}
	tmp := path + ".omega-disabled.tmp"
	if err := os.WriteFile(tmp, out, mode); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp) //nolint:errcheck // best-effort cleanup
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

// applyEnv overrides config fields from OMEGA_* environment variables.
func applyEnv(cfg *Config) {
	// String assignments: env var -> *string field.
	for _, s := range []struct {
		env    string
		target *string
	}{
		{"OMEGA_PROVIDER", &cfg.Provider.Type},
		{"OMEGA_API_KEY", &cfg.Provider.APIKey},
		{"OMEGA_MODEL", &cfg.Provider.ModelName},
		{"OMEGA_HOST", &cfg.Provider.Host},
		{"OMEGA_VISION_MODEL", &cfg.VisionModel},
		{"OMEGA_TITLE_MODEL", &cfg.TitleModel},
		{"OMEGA_DB_PATH", &cfg.Store.DBPath},
		{"OMEGA_SKILLS_DIR", &cfg.Skills.Dir},
		{"OMEGA_LOGGING_FILE", &cfg.Logging.File},
		{"OMEGA_DEFAULT_WORKSPACE", &cfg.Server.DefaultWorkspace},
		{"OMEGA_APPROVALS_MODE", &cfg.Approvals.Mode},
	} {
		if v := os.Getenv(s.env); v != "" {
			*s.target = v
		}
	}

	// Bool assignments: env var -> *bool field, true when "1" or "true" (case-insensitive).
	for _, b := range []struct {
		env    string
		target *bool
	}{
		{"OMEGA_COMPACTION_ENABLED", &cfg.Compaction.Enabled},
		{"OMEGA_MEMORY_ENABLED", &cfg.Memory.Enabled},
		{"OMEGA_USER_PROFILE_ENABLED", &cfg.Memory.UserProfileEnabled},
		{"OMEGA_LOGGING_ENABLED", &cfg.Logging.Enabled},
	} {
		if v := os.Getenv(b.env); v != "" {
			*b.target = v == "1" || strings.EqualFold(v, "true")
		}
	}

	// Int assignments with min validation: env var -> *int field.
	// min is the smallest accepted value (0 for keep_first/keep_last
	// and context_window, 1 otherwise).
	for _, n := range []struct {
		env    string
		target *int
		min    int
	}{
		{"OMEGA_PORT", &cfg.Server.Port, math.MinInt32},
		{"OMEGA_COMPACTION_CONTEXT_WINDOW", &cfg.Compaction.ContextWindow, 0},
		{"OMEGA_COMPACTION_KEEP_FIRST", &cfg.Compaction.KeepFirst, 0},
		{"OMEGA_COMPACTION_KEEP_LAST", &cfg.Compaction.KeepLast, 0},
		{"OMEGA_COMPACTION_RESERVE_TOKENS", &cfg.Compaction.ReserveTokens, 1},
		{"OMEGA_COMPACTION_MAX_TOOL_OUTPUT", &cfg.Compaction.MaxToolOutput, 1},
		{"OMEGA_HTTP_TIMEOUT", &cfg.HTTPTimeout, 1},
		{"OMEGA_MAX_TURNS", &cfg.MaxTurns, 1},
		{"OMEGA_MEMORY_CHAR_LIMIT", &cfg.Memory.CharLimit, 1},
		{"OMEGA_USER_CHAR_LIMIT", &cfg.Memory.UserProfileCharLimit, 1},
	} {
		if v := os.Getenv(n.env); v != "" {
			if i, err := strconv.Atoi(v); err == nil && i >= n.min {
				*n.target = i
			}
		}
	}

	// Float assignment with range validation (0, 1].
	if v := os.Getenv("OMEGA_COMPACTION_THRESHOLD"); v != "" {
		if threshold, err := strconv.ParseFloat(v, 64); err == nil && threshold > 0 && threshold <= 1 {
			cfg.Compaction.Threshold = threshold
		}
	}
}

// Validate checks that required fields are present and values are sane.
func (c Config) Validate() error {
	if c.Provider.ModelName == "" {
		return fmt.Errorf("config: provider.model_name is required")
	}
	if c.Server.Port <= 0 {
		return fmt.Errorf("config: server.port must be > 0, got %d", c.Server.Port)
	}
	if c.Provider.Type != "" {
		switch strings.ToLower(c.Provider.Type) {
		case "ollama", "openai", "anthropic", "mock":
		default:
			return fmt.Errorf("config: provider.type %q is not recognized (want ollama, openai, anthropic, or mock)", c.Provider.Type)
		}
	}
	// Named instances (card t_7dcd4280): the key IS the instance id.
	// A name must survive every consumer that records it (session
	// pins, model-selection slots), so the id space is conservative
	// and "default" stays reserved for the provider: section.
	providerTypes := map[string]string{
		"ollama": "ollama", "openai": "openai", "anthropic": "anthropic", "mock": "mock",
	}
	for name, inst := range c.Providers {
		if name == "default" {
			return fmt.Errorf("config: provider instance %q is reserved (the provider: section is the default instance)", name)
		}
		if !validInstanceName(name) {
			return fmt.Errorf("config: provider instance name %q is invalid (want lowercase letters, digits, - or _)", name)
		}
		if strings.ToLower(inst.Type) != "" && providerTypes[strings.ToLower(inst.Type)] == "" {
			return fmt.Errorf("config: provider %s.type %q is not recognized (want ollama, openai, or anthropic)", name, inst.Type)
		}
		// Card t_1914a8c9: an instance is a NAMED ENDPOINT only (the
		// Hermes shape) - name, type, host, api_key. Models come from
		// the instance's own catalog through the pickers; a bound
		// model is a loud config error, never an ignored field.
		if inst.ModelName != "" {
			return fmt.Errorf("config: provider %s.model_name is no longer settable - providers are named endpoints only, pick the model in the model picker (instance:model)", name)
		}
	}
	if c.Provider.Host != "" {
		if _, err := url.Parse(c.Provider.Host); err != nil {
			return fmt.Errorf("config: provider.host is not a valid URL: %w", err)
		}
	}
	if c.Memory.Enabled && c.Memory.CharLimit <= 0 {
		return fmt.Errorf("config: memory.char_limit must be > 0 when memory is enabled")
	}
	if c.Memory.UserProfileEnabled && c.Memory.UserProfileCharLimit <= 0 {
		return fmt.Errorf("config: memory.user_char_limit must be > 0 when user profile is enabled")
	}
	if c.MaxTurns < 0 {
		return fmt.Errorf("config: max_turns must be >= 0, got %d", c.MaxTurns)
	}
	if c.HTTPTimeout < 0 {
		return fmt.Errorf("config: http_timeout must be >= 0, got %d", c.HTTPTimeout)
	}
	if !approval.ValidMode(c.Approvals.Mode) {
		return fmt.Errorf("config: approvals.mode must be off, manual, or smart, got %q", c.Approvals.Mode)
	}
	return nil
}

// WatchConfig polls the config file at path for modification time changes
// and calls onChange when it changes. The callback receives the freshly
// loaded config. If the reload fails (e.g. file is temporarily empty
// during an atomic save), the error is logged and the old config stays
// in effect. Runs until the caller's goroutine exits.
func WatchConfig(path string, onChange func(Config)) {
	go func() {
		var lastMod time.Time
		for range time.Tick(5 * time.Second) {
			fi, err := os.Stat(path)
			if err != nil {
				continue
			}
			if fi.ModTime().Equal(lastMod) {
				continue
			}
			lastMod = fi.ModTime()
			cfg, err := LoadConfig(path)
			if err == nil {
				onChange(cfg)
			} else {
				fmt.Fprintf(os.Stderr, "omega: config reload failed (keeping old config): %v\n", err)
			}
		}
	}()
}

// ConfigSection implements agent.ConfigSource (arc 2b, t_889388cc):
// the host exposes its raw sections by name; agent.RouteConfigs pulls
// each plugin's declared section into ctx.Configs. Section names are
// the keys the plugins read ("ctx.Configs[\"<name>\"].(<mod>.Config)").
func (c Config) ConfigSection(name string) (any, bool) {
	switch name {
	case "store":
		return store.Config{DBPath: c.Store.DBPath}, true
	case "sessions":
		return sessionsSection(c), true
	case "provider":
		// card t_7dcd4280: the named instances ride the section - the
		// provider capsule builds one resolver per entry at mount.
		instances := make(map[string]provider.Config, len(c.Providers))
		for name, inst := range c.Providers {
			instances[name] = provider.Config{
				Type:      inst.Type,
				ModelName: inst.ModelName,
				Host:      inst.Host,
				APIKey:    inst.APIKey,
			}
		}
		return provider.Config{Type: c.Provider.Type, ModelName: c.Provider.ModelName, Host: c.Provider.Host, APIKey: c.Provider.APIKey, Instances: instances}, true
	case "http_channel":
		return api.Config{Port: c.Server.Port, Bind: c.Server.Bind, RunTimeout: c.Server.RunTimeout, DefaultWorkspace: c.Server.DefaultWorkspace, TitleModel: c.TitleModel, PublicHosts: c.Server.PublicHosts, WebTools: agentapi.WebToolsConfig{APIKey: c.Provider.APIKey, BaseURL: webBaseURL(c.Web.BaseURL, c.Provider.Host)}}, true
	case "skills":
		return skillsSection(c), true
	case "memory":
		return memory.Config{Enabled: c.Memory.Enabled, UserProfileEnabled: c.Memory.UserProfileEnabled, CharLimit: c.Memory.CharLimit, UserProfileCharLimit: c.Memory.UserProfileCharLimit, File: c.Memory.File, UserProfileFile: c.Memory.UserProfileFile}, true
	case "logging":
		return logging.Config{Enabled: c.Logging.Enabled, File: c.Logging.File}, true
	case "compactor":
		return compactionSection(c), true
	case "trust":
		return trust.Config{Home: omegaHome()}, true
	case "files":
		return files.Config{DefaultWorkspace: c.Server.DefaultWorkspace}, true
	case "kanban":
		return kanbanModule.Config{DBPath: c.Kanban.DBPath}, true
	case "continuous":
		cont := c.Continuous
		cont.Disabled = slices.Clone(c.Disabled.Agents) // v0.14.1: the disabled.agents fold rides the section
		return cont, true
	case "approvals":
		return approvalSection(c), true
	case "process":
		// card t_5782d11e: zero knobs ride the module's constants -
		// the section only carries explicit user overrides.
		return process.Config{
			Home:             omegaHome(),
			LogMaxBytes:      c.Process.LogMaxBytes,
			KillGraceSeconds: c.Process.KillGraceSeconds,
		}, true
	}
	return nil, false
}

// approvalSection returns the approvals section (card t_c2b52cb5):
// mode/policy are Endo's knobs; host and home are wiring - the
// reviewer targets the provider host (the keypool mirror), the audit
// lands in the home. The decision model rides the provider settings
// slot (card t_fc0b2f2d): approvalSlot routes an "instance:model"
// prefix through the named provider instance. One shape for BOTH
// the boot wiring (RouteConfigs) and the hot-apply path (card
// t_34cd1921: settings.go calls this on every save/reload so a flip
// reaches the running gate without a restart).
func approvalSection(c Config) approval.Config {
	host, key, model := approvalSlot(c)
	return approval.Config{
		Mode:        c.Approvals.Mode,
		Model:       model,
		SmartPolicy: c.Approvals.SmartPolicy,
		HTTPHost:    host,
		APIKey:      key,
		Home:        omegaHome(),
	}
}

// sessionsSection returns the store capsule's hygiene section (card
// t_329b9312): the hygiene pass reads its knobs from the store capsule
// (the config session rides cfg.Sessions). The summarizer's model slot
// (card t_fc0b2f2d) moved to provider settings - the capsule keeps the
// field, the slot is global. One shape for BOTH the boot wiring
// (RouteConfigs) and the hot-apply path (card t_f78acfd6: settings.go
// re-sends the section on every save/reload via store.SetHygieneConfig,
// so a summarizer_model flip reaches the next sweep without a restart).
func sessionsSection(c Config) store.HygieneConfig {
	return store.HygieneConfig{
		RetentionDays:       c.Sessions.RetentionDays,
		SummarizeAfterHours: c.Sessions.SummarizeAfterHours,
		SummarizerModel:     c.SummarizerModel,
	}
}

// skillsSection returns the skills capsule's section. One shape for
// BOTH the boot wiring (RouteConfigs) and the hot-apply path (card
// t_f78acfd6: settings.go re-sends it on every save/reload, and the
// curator consult slot flows through skillsSection(...).CuratorModel).
func skillsSection(c Config) skills.Config {
	return skills.Config{Dir: c.Skills.Dir, SharedDir: c.Skills.SharedDir, Disabled: slices.Clone(c.Disabled.SkillNames), NudgeInterval: c.Skills.NudgeInterval, CuratorModel: c.SkillsModel}
}

// webBaseURL returns the host the channel's web.search/web.fetch tools
// target: the explicit web.base_url override when set, otherwise the
// provider host (with a trailing /v1 stripped, mirroring provider.go's
// probe), so the keypool mirror serves /api/web_search and
// /api/web_fetch with the same auth swap + rotation as every other
// call. The web config stanza stays a config.yaml top-level key even
// though the capsule dissolved into the channel (v0.13 shell law 1 -
// no config churn). The fields arrive as arguments, not as the Config
// value: removing the provider capsule scrubs every cfg.Provider
// access site, and a helper reading whole Config would strand this
// branch with the module gone (missing return).
func webBaseURL(baseURL, providerHost string) string {
	if baseURL != "" {
		return baseURL
	}
	return strings.TrimSuffix(providerHost, "/v1")
}

// compactionSection returns the compactor section (card t_fc0b2f2d):
// the compaction knobs plus the model slot copied in from the
// top-level compaction_model knob. The Model field is carrier wiring
// (yaml "-"), so ConfigSection and hotApply keep one shape.
func compactionSection(c Config) agent.CompactionConfig {
	comp := c.Compaction
	comp.Model = c.CompactionModel
	return comp
}

// approvalSlot resolves the approval gate's decision slot (card
// t_fc0b2f2d) into the reviewer's wiring: the ONE slot rule (a colon
// prefix naming a configured instance, or "default") routes the
// consult through that instance's host and key; anything else is the
// whole slot as a bare model on the default provider. An instance
// prefix with no model part serves the instance's own model.
func approvalSlot(c Config) (host, key, model string) {
	host = c.Provider.Host
	key = c.Provider.APIKey
	model = c.ApprovalsModel
	name, m := agent.ParseModelSlot(c.ApprovalsModel)
	if name == "" {
		return host, key, model
	}
	if name == "default" {
		return host, key, m
	}
	if inst, ok := c.Providers[name]; ok {
		host = inst.Host
		key = inst.APIKey
		if m == "" {
			m = inst.ModelName
		}
		model = m
	}
	// An unconfigured prefix stays a bare model: the whole slot rides
	// to the default host and a missing model fails loudly there.
	return host, key, model
}
