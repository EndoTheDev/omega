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
	"github.com/EndoTheDev/omega/harness/memory"
	"github.com/EndoTheDev/omega/harness/observability"
	"github.com/EndoTheDev/omega/harness/provider"
	"github.com/EndoTheDev/omega/harness/skills"
	"github.com/EndoTheDev/omega/harness/store"
	"github.com/EndoTheDev/omega/harness/terminal"
	"github.com/EndoTheDev/omega/harness/trust"
	"gopkg.in/yaml.v3"
	"time"
)

type Config struct {
	Provider ProviderConfig `yaml:"provider"`
	Providers    map[string]legacyProviderConfig `yaml:"providers,omitempty"`
	Server       ServerConfig                    `yaml:"server"`
	Store        StoreConfig                     `yaml:"store"`
	Compaction   agent.CompactionConfig          `yaml:"compaction"`
	SystemPrompt string                          `yaml:"system_prompt"`
	// VisionModel is the auxiliary captioner (vision_model knob). The special value
	// "default" (provider.DefaultAuxModel) follows whichever model the current run serves
	// - the composer's chip at caption time - and persists as the literal word, never a
	// frozen model name. Empty keeps today's behavior - images ride to the main provider
	// and a model that cannot see them fails loudly.
	VisionModel string `yaml:"vision_model"`
	TitleModel string `yaml:"title_model"`
	// The four per-subsystem model slots. The schema keys ride the provider group
	// (provider.*_model), mirroring vision_model / title_model: top-level knobs, one
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
	Disabled DisabledNamespaces `yaml:"disabled"`

	// agentName and agentToggles are the applied agent layer's runtime facts, never config
	// file state (unexported: yaml.Marshal skips them, so a settings save can never
	// persist them).
	agentName    string
	agentToggles []string
	agentSkillsAllow    []string
	agentSkillsAllowSet bool
}

// UnmarshalYAML decodes the document over whatever value the caller already holds.
func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	// The alias type carries no UnmarshalYAML, so this cannot recurse.
	type plain Config
	decoded := plain(*c)
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	next := Config(decoded)
	next.foldLegacyProviders()
	*c = next
	return nil
}

func (c *Config) foldLegacyProviders() {
	if len(c.Providers) == 0 {
		return
	}
	names := make([]string, 0, len(c.Providers))
	for name := range c.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, ok := c.Provider.instance(name); ok {
			continue
		}
		legacy := c.Providers[name]
		c.Provider.setInstance(ProviderInstance{
			Name:   name,
			Type:   legacy.Type,
			Host:   legacy.Host,
			APIKey: legacy.APIKey,
		})
	}
	c.Providers = nil
}

// DisabledNamespaces is the config.yaml `disabled:` fold shape. An all-off namespace is
// never written: an empty list carries no state, and an all-empty key carries none at
// all.
type DisabledNamespaces struct {
	Modules    []string `yaml:"modules"`
	SkillNames []string `yaml:"skills"`
	Tools      []string `yaml:"tools"`
	Agents     []string `yaml:"agents"`
}

// empty reports whether every namespace is nil. The skills namespace guards
// early-return so its scrub deletes a whole statement block, never a boolean operand
// inside the return expression (nm-purity scrub shape).
func (d DisabledNamespaces) empty() bool {
	if len(d.SkillNames) > 0 {
		return false
	}
	return len(d.Modules) == 0 && len(d.Tools) == 0 && len(d.Agents) == 0
}

// UnmarshalYAML accepts both shapes the config.yaml `disabled:` key has ever carried.
// Anything else fails loud: a shape the toggle writer did not produce must not decode
// to silence.
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
		// zero-value first: a re-used decode target must not inherit namespaces an earlier.
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

// sortedCopy returns a sorted copy of ns (stable yaml output); a nil slice stays nil.
func sortedCopy(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	out := append([]string(nil), names...)
	sort.Strings(out)
	return out
}

// cleanNamespaces returns the fold state an empty namespace never keeps: empty lists
// become nil (the writer omits empty namespaces, the all-empty key omits itself).
func cleanNamespaces(d DisabledNamespaces) DisabledNamespaces {
	return DisabledNamespaces{
		Modules:    sortedCopy(d.Modules),
		SkillNames: sortedCopy(d.SkillNames),
		Tools:      sortedCopy(d.Tools),
		Agents:     sortedCopy(d.Agents),
	}
}

type WebConfig struct {
	BaseURL string `yaml:"base_url"`
}

type SkillsConfig struct {
	Dir string `yaml:"dir"`
	SharedDir string `yaml:"shared_dir"`
	NudgeInterval int `yaml:"nudge_interval"`
}

type MemoryConfig struct {
	Enabled              bool   `yaml:"enabled"`
	UserProfileEnabled   bool   `yaml:"user_profile_enabled"`
	CharLimit            int    `yaml:"char_limit"`
	UserProfileCharLimit int    `yaml:"user_char_limit"`
	File                 string `yaml:"file"`
	UserProfileFile      string `yaml:"user_file"`
}

type LoggingConfig struct {
	Enabled bool   `yaml:"enabled"`
	File    string `yaml:"file"`
}

type ProviderConfig struct {
	ModelName string             `yaml:"model_name"`
	Instances []ProviderInstance `yaml:"instances,omitempty"`
}

type ProviderInstance struct {
	Name   string `yaml:"name"`
	Type   string `yaml:"type"`
	Host   string `yaml:"host"`
	APIKey string `yaml:"api_key"`
}

const PrimaryProviderInstance = "default"

type legacyProviderConfig struct {
	ModelName string             `yaml:"model_name"`
	Instances []ProviderInstance `yaml:"instances"`
	Type      string             `yaml:"type"`
	Host      string             `yaml:"host"`
	APIKey    string             `yaml:"api_key"`
}

// UnmarshalYAML reads a provider. A flat trio becomes the "default" row; an absent
// instances key keeps whatever the value already held (the caller decoded over
// DefaultConfig, so a config that sets only provider.model_name keeps the default
// instance).
func (p *ProviderConfig) UnmarshalYAML(node *yaml.Node) error {
	prev := *p
	var raw legacyProviderConfig
	if err := node.Decode(&raw); err != nil {
		return err
	}
	out := ProviderConfig{ModelName: raw.ModelName, Instances: prev.Instances}
	// An absent model_name keeps the value already held: the caller decodes.
	if out.ModelName == "" {
		out.ModelName = prev.ModelName
	}
	if raw.Instances != nil {
		out.Instances = raw.Instances
	}
	if raw.Type != "" || raw.Host != "" || raw.APIKey != "" {
		out.setInstance(ProviderInstance{
			Name:   PrimaryProviderInstance,
			Type:   raw.Type,
			Host:   raw.Host,
			APIKey: raw.APIKey,
		})
	}
	*p = out
	return nil
}

func (p ProviderConfig) instanceIndex(name string) int {
	for i, inst := range p.Instances {
		if inst.Name == name {
			return i
		}
	}
	return -1
}

func (p ProviderConfig) instance(name string) (ProviderInstance, bool) {
	if i := p.instanceIndex(name); i >= 0 {
		return p.Instances[i], true
	}
	return ProviderInstance{}, false
}

func (p *ProviderConfig) setInstance(inst ProviderInstance) {
	if i := p.instanceIndex(inst.Name); i >= 0 {
		p.Instances[i] = inst
	} else {
		p.Instances = append(p.Instances, inst)
	}
	p.primaryFirst()
}

func (p *ProviderConfig) primaryFirst() {
	i := p.instanceIndex(PrimaryProviderInstance)
	if i <= 0 {
		return
	}
	primary := p.Instances[i]
	copy(p.Instances[1:i+1], p.Instances[0:i])
	p.Instances[0] = primary
}

func (c Config) defaultInstance() (ProviderInstance, bool) {
	return c.Provider.instance(PrimaryProviderInstance)
}

// setDefaultInstance replaces the primary row's endpoint fields, the one way a caller.
func (c *Config) setDefaultInstance(inst ProviderInstance) {
	inst.Name = PrimaryProviderInstance
	c.Provider.setInstance(inst)
}

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

// ServerConfig configures the HTTP listener(s). Additional addresses - e.g. the
// Tailscale IP so Traefik can route to omega - are an explicit opt-in, never a default.
// RunTimeout caps each detached /chat run's wall-clock time, in seconds: a hung tool
// call must not brick the session's run slot until restart.
type ServerConfig struct {
	Port       int      `yaml:"port"`
	Bind       []string `yaml:"bind"`
	RunTimeout int      `yaml:"run_timeout"`
	// PublicHosts (config key public_hosts) declares the public identities this serve
	// answers to - domains fronted by a reverse proxy or tunnel (the tunnel forwards the
	// browser's Host, which never matches the bind, so the Host guard refuses them without
	// this declaration).
	PublicHosts []string `yaml:"public_hosts"`
	DefaultWorkspace string `yaml:"default_workspace"`
}

type StoreConfig struct {
	DBPath string `yaml:"db_path"`
}

type SessionsConfig struct {
	// RetentionDays prunes sessions older than this. 0 = never prune.
	RetentionDays int `yaml:"retention_days"`

	SummarizeAfterHours int `yaml:"summarize_after_hours"`
}

type KanbanConfig struct {
	DBPath string `yaml:"db_path"`
}

type ApprovalConfig struct {
	Mode        string `yaml:"mode"`
	SmartPolicy string `yaml:"smart_policy"`
	// Monitor is the safeagents detector set's rollout (card
	// t_e3cd4381): "log-only" (or empty) reports WOULD-BLOCK findings
	// and never blocks - the soak; "enforce" lets the severity tiers
	// decide.
	Monitor string `yaml:"monitor"`
	// JudgeFailClosed flips the evidence-only judge's failure law:
	// false (the default) fails open - the human still decides;
	// true refuses the call.
	JudgeFailClosed bool `yaml:"judge_fail_closed"`
}

type ProcessConfig struct {
	LogMaxBytes      int64 `yaml:"log_max_bytes"`
	KillGraceSeconds int   `yaml:"kill_grace_seconds"`
}

func DefaultConfig() Config {
	return Config{
		Provider: ProviderConfig{
			Instances: []ProviderInstance{{
				Name: PrimaryProviderInstance,
				Host: "http://localhost:11434",
			}},
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
			File: "",
		},
		HTTPTimeout: 300,
		MaxTurns:    100,
		// the ship-dark default (Q5): manual IS the explicit default - the knob render reads
		// the field directly, no helper (the pure-failure law: the approval vocabulary must
		// die whole under scripts/nm-purity.sh --module approval, and a helper derived
		// entirely from the config field leaves an illegal empty func behind).
		Approvals: ApprovalConfig{
			Mode:    "manual",
			Monitor: approval.MonitorLogOnly,
		},
	}
}

func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return Config{}, configReadError(path, err)
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config: %w", err)
		}
	}

	applyEnv(&cfg)

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	folded, foldTarget, err := foldOmegaJSON(path)
	if err != nil {
		return Config{}, err
	}
	if path == "" {
		path = resolveConfigPath("")
		if path == "" && len(folded) > 0 {
			path = foldTarget
		}
	}

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return Config{}, configReadError(path, err)
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

// configReadError wraps a failed config read. When the read hit the pre-v0.8 home-root
// location, the relocation hint rides along: a first user pointing --config at
// <home>/config.yaml used to see only the raw OS message ("The system cannot find the
// file specified") and no sign the file had moved.
func configReadError(path string, err error) error {
	if !os.IsNotExist(err) {
		return fmt.Errorf("read config: %w", err)
	}
	return fmt.Errorf("read config: %w%s", err, legacyConfigHint(path))
}

func legacyConfigHint(path string) string {
	if filepath.Base(path) != "config.yaml" {
		return ""
	}
	home := omegaHome()
	if !strings.EqualFold(canonPath(filepath.Dir(path)), home) {
		return ""
	}
	moved := home + "/config/config.yaml"
	if _, err := os.Stat(moved); err == nil {
		return fmt.Sprintf(" - the config moved to %s (v0.8 layout); pass --config %s", moved, moved)
	}
	return fmt.Sprintf(" - config.yaml lives at %s now (v0.8 layout); run `omega setup` to write one", moved)
}

// foldOmegaJSON migrates the v0.12 omega.json module toggle list into config..
// Entries a config.yaml that already carries the key would also name are reported in
// the boot warning but never double-written.
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
	// Boot warning, loud, naming the folded entries.
	fmt.Fprintf(os.Stderr, "omega: folded module disable list %v from omega.json into %s (disabled: key, v0.13 shell law 3)\n", entries, target)
	return entries, target, nil
}

// writeDisabled writes the config.yaml `disabled:` toggle fold: parse, modify, marshal
// at the yaml node level, so comments, key order, and unknown keys survive (the
// struct-marshal settings-save path normalizes; this one must not). The v0.14.1 fold
// shape is one `disabled:` mapping with a namespace key per registry; a namespace with
// no entries is never written, and when every namespace is empty the whole key goes
// away - an all-on home carries no toggle state. A legacy flat sequence under
// `disabled:` is REPLACED on the first write: the caller's fold always carries the
// authoritative full state (a partial write would re-shape history it did not read). A
// malformed config.yaml fails loud - a write that destroys the file boot must parse is
// worse than a failed toggle.
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

	// disabledSeq renders the `disabled:` value node from the fold: a mapping whose
	// namespace keys are the NON-EMPTY lists (an empty list never persists). Every empty
	// fold returns nil - the key goes away, an all-on home carries no toggle state.
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
		mapping.Content = append(mapping.Content[:idx], mapping.Content[idx+2:]...)
	case val != nil && idx >= 0:
		// The fold replaces whatever shape the key carried (a legacy
		// flat sequence included): the caller's fold is the full state.
		mapping.Content[idx+1] = val
	case val != nil:
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
		_ = os.Remove(tmp) // best-effort cleanup
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

func applyEnv(cfg *Config) {
	primary, _ := cfg.defaultInstance()
	envProvider := primary
	overridden := false
	if v := os.Getenv("OMEGA_PROVIDER"); v != "" {
		envProvider.Type = v
		overridden = true
	}
	if v := os.Getenv("OMEGA_API_KEY"); v != "" {
		envProvider.APIKey = v
		overridden = true
	}
	if v := os.Getenv("OMEGA_HOST"); v != "" {
		envProvider.Host = v
		overridden = true
	}
	if overridden {
		cfg.setDefaultInstance(envProvider)
	}

	for _, s := range []struct {
		env    string
		target *string
	}{
		{"OMEGA_MODEL", &cfg.Provider.ModelName},
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

	if v := os.Getenv("OMEGA_COMPACTION_THRESHOLD"); v != "" {
		if threshold, err := strconv.ParseFloat(v, 64); err == nil && threshold > 0 && threshold <= 1 {
			cfg.Compaction.Threshold = threshold
		}
	}
}

func (c Config) Validate() error {
	if c.Provider.ModelName == "" {
		return fmt.Errorf("config: provider.model_name is required")
	}
	if c.Server.Port <= 0 {
		return fmt.Errorf("config: server.port must be > 0, got %d", c.Server.Port)
	}
	// The provider INSTANCES: every provider is a row, the primary is the row named. The
	// names must survive every consumer that records one (session pins, model slots), and
	// a config with no primary row has no provider the agent can serve - both are loud
	// here, never a silent fallback mid-run.
	providerTypes := map[string]string{
		"ollama": "ollama", "openai": "openai", "anthropic": "anthropic", "mock": "mock",
	}
	seenInstances := make(map[string]bool, len(c.Provider.Instances))
	for _, inst := range c.Provider.Instances {
		if !validInstanceName(inst.Name) {
			return fmt.Errorf("config: provider instance name %q is invalid (want lowercase letters, digits, - or _)", inst.Name)
		}
		if seenInstances[inst.Name] {
			return fmt.Errorf("config: provider instance %q is defined twice", inst.Name)
		}
		seenInstances[inst.Name] = true
		if strings.ToLower(inst.Type) != "" && providerTypes[strings.ToLower(inst.Type)] == "" {
			return fmt.Errorf("config: provider %s.type %q is not recognized (want ollama, openai, anthropic, or mock)", inst.Name, inst.Type)
		}
		if inst.Host != "" {
			if _, err := url.Parse(inst.Host); err != nil {
				return fmt.Errorf("config: provider %s.host is not a valid URL: %w", inst.Name, err)
			}
		}
	}
	if !seenInstances[PrimaryProviderInstance] {
		return fmt.Errorf("config: provider.instances must carry the row named %q - it is the provider the agent serves", PrimaryProviderInstance)
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
	if !approval.ValidMonitor(c.Approvals.Monitor) {
		return fmt.Errorf("config: approvals.monitor must be log-only or enforce, got %q", c.Approvals.Monitor)
	}
	return nil
}

// WatchConfig polls the config file at path for modification time changes and calls. If
// the reload fails (e.g. file is temporarily empty during an atomic save), the error is
// logged and the old config stays in effect. Runs until the caller's goroutine exits.
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

func (c Config) ConfigSection(name string) (any, bool) {
	switch name {
	case "store":
		return store.Config{DBPath: c.Store.DBPath, Home: omegaHome()}, true
	case "sessions":
		return sessionsSection(c), true
	case "provider":
		return providerSection(c), true
	case "http_channel":
		primary, _ := c.defaultInstance()
		return api.Config{Port: c.Server.Port, Bind: c.Server.Bind, RunTimeout: c.Server.RunTimeout, DefaultWorkspace: c.Server.DefaultWorkspace, TitleModel: c.TitleModel, PublicHosts: c.Server.PublicHosts, WebTools: agentapi.WebToolsConfig{APIKey: primary.APIKey, BaseURL: webBaseURL(c.Web.BaseURL, primary.Host)}}, true
	case "skills":
		return skillsSection(c), true
	case "memory":
		return memory.Config{Enabled: c.Memory.Enabled, UserProfileEnabled: c.Memory.UserProfileEnabled, CharLimit: c.Memory.CharLimit, UserProfileCharLimit: c.Memory.UserProfileCharLimit, File: c.Memory.File, UserProfileFile: c.Memory.UserProfileFile}, true
	case "logging":
		return observability.Config{Enabled: c.Logging.Enabled, File: c.Logging.File}, true
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
		return terminal.RegistryConfig{
			Home:             omegaHome(),
			LogMaxBytes:      c.Process.LogMaxBytes,
			KillGraceSeconds: c.Process.KillGraceSeconds,
		}, true
	}
	return nil, false
}

func providerSection(c Config) provider.Config {
	primary, _ := c.defaultInstance()
	instances := make(map[string]provider.Config, len(c.Provider.Instances))
	for _, inst := range c.Provider.Instances {
		if inst.Name == PrimaryProviderInstance {
			continue // the primary is the base provider itself
		}
		instances[inst.Name] = provider.Config{
			Type:   inst.Type,
			Host:   inst.Host,
			APIKey: inst.APIKey,
		}
	}
	return provider.Config{
		Type:      primary.Type,
		ModelName: c.Provider.ModelName,
		Host:      primary.Host,
		APIKey:    primary.APIKey,
		Instances: instances,
	}
}

func approvalSection(c Config) approval.Config {
	host, key, model := approvalSlot(c)
	return approval.Config{
		Mode:            c.Approvals.Mode,
		Model:           model,
		SmartPolicy:     c.Approvals.SmartPolicy,
		Monitor:         c.Approvals.Monitor,
		JudgeFailClosed: c.Approvals.JudgeFailClosed,
		HTTPHost:        host,
		APIKey:          key,
		Home:            omegaHome(),
	}
}

func sessionsSection(c Config) store.HygieneConfig {
	return store.HygieneConfig{
		RetentionDays:       c.Sessions.RetentionDays,
		SummarizeAfterHours: c.Sessions.SummarizeAfterHours,
		SummarizerModel:     c.SummarizerModel,
	}
}

func skillsSection(c Config) skills.Config {
	return skills.Config{Dir: c.Skills.Dir, SharedDir: c.Skills.SharedDir, Disabled: slices.Clone(c.Disabled.SkillNames), NudgeInterval: c.Skills.NudgeInterval, CuratorModel: c.SkillsModel,
		Allow: slices.Clone(c.agentSkillsAllow), AllowSet: c.agentSkillsAllowSet}
}

func webBaseURL(baseURL, providerHost string) string {
	if baseURL != "" {
		return baseURL
	}
	return strings.TrimSuffix(providerHost, "/v1")
}

func compactionSection(c Config) agent.CompactionConfig {
	comp := c.Compaction
	comp.Model = c.CompactionModel
	return comp
}

func approvalSlot(c Config) (host, key, model string) {
	primary, _ := c.defaultInstance()
	host = primary.Host
	key = primary.APIKey
	model = c.ApprovalsModel
	name, m := agent.ParseModelSlot(c.ApprovalsModel)
	if name == "" {
		return host, key, model
	}
	if name == PrimaryProviderInstance {
		return host, key, m
	}
	if inst, ok := c.Provider.instance(name); ok {
		// A prefix with no model part names an ENDPOINT only (an instance carries no model of
		// its own): the consult has no model to ask, so the slot fails closed and the gate
		// escalates, exactly like an empty one.
		host = inst.Host
		key = inst.APIKey
		model = m
	}
	return host, key, model
}
