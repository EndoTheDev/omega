package agent

// agent/profiles.go: multi-agent profile loading (card t_7e2b7002).
// Agents live as directories - the Hermes-proven shape:
//
//	~/agents-home/agents/<name>/
//	  config.yaml   provider/model/description - LAYERS over the home
//	                config.yaml (only what it sets overrides)
//	  skills/       this agent's own skills
//	  logs/         scoped per agent
//	  memory.md     scoped per agent
//	  user.md       scoped per agent
//
// The default agent is an agent like any other, except two legacy
// anchors that stay at the home root: the home config.yaml IS the base
// config every agent layers over (the default adds nothing), and the
// home skills/ directory remains the default agent's skill home.
// MigrateDefaultAgent moves the legacy home memory.md/user.md into
// agents/default/ on boot - see the boot call in cmd/omega.
//
// The profile surface is store-free: LoadProfiles reads only the
// filesystem. Session binding (counts, last activity) is filled in by
// callers that hold a StoreProvider (the /agents endpoint, store
// commands) so this package never takes a dependency on SQLite shape.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultAgentName is the agent every existing session, cron fire and
// file belongs to unless stated otherwise. Sessions created before the
// agent column existed (or by callers that pass nothing) ride this.
const DefaultAgentName = "default"

// Profile is one agent directory rendered as data. MemorySize is the
// raw byte size of memory.md (0 = no memory written yet - the file is
// only ever created by an actual memory write, never seeded). SkillDirs
// is the ordered scan list the host should configure for this agent.
type Profile struct {
	Name         string `json:"name"`
	Dir          string `json:"dir"`
	Description  string `json:"description,omitempty"`
	ProviderType string `json:"provider_type,omitempty"`
	ModelName    string `json:"model_name,omitempty"`
	SkillCount   int    `json:"skill_count"`
	Skills       []string `json:"skills,omitempty"`
	MemorySize   int    `json:"memory_size"`
	// Filled by store-holding callers (never LoadProfiles):
	SessionCount int    `json:"session_count"`
	LastActivity string `json:"last_activity,omitempty"`
}

// agentConfig is the subset of an agent's config.yaml this package
// parses for display and layering decisions. Unknown keys (server,
// store, memory overrides...) are layering business for cmd/omega's
// Config and are ignored here on purpose - one parse shape per concern.
type agentConfig struct {
	Description string `yaml:"description"`
	Provider    struct {
		Type      string `yaml:"type"`
		ModelName string `yaml:"model_name"`
	} `yaml:"provider"`
}

// ValidAgentName reports whether name is a usable agent directory name:
// lowercase alphanumeric, may carry internal hyphens, 1-32 chars. The
// rules match the kanban tenant slugs - familiar, and hostile to path
// traversal by construction.
func ValidAgentName(name string) error {
	if name == "" {
		return fmt.Errorf("agent name is empty")
	}
	if len(name) > 32 {
		return fmt.Errorf("agent name %q is longer than 32 chars", name)
	}
	for i, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (r == '-' && i > 0 && i < len(name)-1)
		if !ok {
			return fmt.Errorf("agent name %q is invalid (want lowercase letters, digits, interior hyphens)", name)
		}
	}
	return nil
}

// AgentDir is the directory of one agent profile under home.
func AgentDir(home, name string) string {
	return filepath.Join(home, "agents", name)
}

// AgentConfigPath is the profile's own config.yaml (the layer).
func AgentConfigPath(home, name string) string {
	return filepath.Join(AgentDir(home, name), "config.yaml")
}

// MigrateDefaultAgent moves the legacy home memory.md/user.md into
// agents/default/ (rename, not copy: exactly one copy must exist or
// the two readers diverge silently). Missing legacy files are the
// common case - nothing to do. Existing agent-scoped files win: a
// second migration on an already-migrated home is a no-op. The home
// config.yaml is deliberately NOT moved - it is the base layer every
// agent's config.yaml layers over (design ruling, card t_7e2b7002).
func MigrateDefaultAgent(home string) error {
	dir := AgentDir(home, DefaultAgentName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create default agent dir: %w", err)
	}
	for _, name := range []string{"memory.md", "user.md"} {
		legacy := filepath.Join(home, name)
		target := filepath.Join(dir, name)
		if _, err := os.Stat(target); err == nil {
			continue // already migrated
		}
		if _, err := os.Stat(legacy); err != nil {
			continue // never written / already moved
		}
		if err := os.Rename(legacy, target); err != nil {
			return fmt.Errorf("migrate %s into agents/default: %w", name, err)
		}
	}
	return nil
}

// ScaffoldAgent creates agents/<name>/ from the template: config.yaml
// (commented knobs, never live values), an empty skills/ and logs/.
// memory.md/user.md are NOT seeded - the memory tool's own description
// teaches the file conventions and an agent's first memory write
// creates them (privacy law: agent dirs are never seeded with content,
// personal or otherwise). The home must already exist.
func ScaffoldAgent(home, name, description string) error {
	if err := ValidAgentName(name); err != nil {
		return err
	}
	if name == DefaultAgentName {
		return fmt.Errorf("%q is the default agent - it exists by definition", DefaultAgentName)
	}
	dir := AgentDir(home, name)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("agent %q already exists at %s", name, dir)
	}
	if err := os.MkdirAll(filepath.Join(dir, "skills"), 0o755); err != nil {
		return fmt.Errorf("scaffold agent %s: %w", name, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o755); err != nil {
		return fmt.Errorf("scaffold agent %s: %w", name, err)
	}
	cfg := fmt.Sprintf(defaultAgentConfigTemplate, name, description)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o644); err != nil {
		return fmt.Errorf("scaffold agent %s: %w", name, err)
	}
	return nil
}

// defaultAgentConfigTemplate is the scaffold config. Every knob it
// mentions is commented out: an unscaffolded edit cannot break boot.
// The layering rule is written into the file so editing agents see it
// without a doc trip.
const defaultAgentConfigTemplate = `# omega agent: %s
# This file layers over the home config.yaml - only what you un-comment
# here overrides. memory.md, user.md, skills/ and logs/ are scoped to
# this agent; sessions created as this agent never appear in the
# default agent's session list.
#
# provider:
#   type: ollama        # ollama, openai, or anthropic
#   model_name: glm-5.3
#   host: http://localhost:11434
#   api_key: ""
# system_prompt: ""
description: %q
`

// AgentSkillDirs returns the ordered skill scan list for an agent:
// the shared root first (every agent sees home/skills/shared), then the
// agent's own skills/. The default agent additionally keeps the legacy
// home/skills home - existing installs lose nothing. Loaders dedupe
// skills by name in this scan order with first-match winning, so a
// name present in both shared/ and an agent dir resolves to SHARED
// (the card's order is literal); an agent needing a variant gives it
// its own name.
func AgentSkillDirs(home, name string) []string {
	var dirs []string
	if shared := filepath.Join(home, "skills", "shared"); dirExists(shared) {
		dirs = append(dirs, shared)
	}
	if own := filepath.Join(AgentDir(home, name), "skills"); dirExists(own) {
		dirs = append(dirs, own)
	}
	if name == DefaultAgentName {
		legacy := filepath.Join(home, "skills")
		if dirExists(legacy) {
			dirs = append(dirs, legacy)
		}
	}
	return dirs
}

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// LoadProfiles scans home/agents/ into profile records. The default
// profile always exists (synthesized when its directory has not been
// created by the boot migration yet); the rest are directory children,
// sorted by name. Name validation is defensive: a malformed directory
// is skipped, never fatal - a stray dir must not take the surface down.
func LoadProfiles(home string) []Profile {
	out := []Profile{defaultProfile(home)}
	entries, err := os.ReadDir(filepath.Join(home, "agents"))
	if err != nil {
		return out // no agents/ yet: just the default
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() || e.Name() == DefaultAgentName || e.Name()[0] == '.' || e.Name()[0] == '_' {
			continue
		}
		if ValidAgentName(e.Name()) != nil {
			continue // malformed dir: skip, never fail the scan
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		out = append(out, loadProfile(home, name))
	}
	return out
}

// LoadProfile returns one agent's profile, or an error for an unknown
// name (the caller renders "unknown agent" rather than an empty row).
func LoadProfile(home, name string) (Profile, error) {
	if name == DefaultAgentName {
		return defaultProfile(home), nil
	}
	dir := AgentDir(home, name)
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return Profile{}, fmt.Errorf("unknown agent %q", name)
	}
	return loadProfile(home, name), nil
}

func defaultProfile(home string) Profile {
	// The default is computed through the same loader as any other
	// agent, so its legacy memory fallback (home/memory.md on an
	// unmigrated home) follows the identical path.
	return loadProfile(home, DefaultAgentName)
}

// loadProfile parses one existing agent dir. Read failures degrade
// gracefully - a profile row with defaults beats a broken list.
func loadProfile(home, name string) Profile {
	p := Profile{Name: name, Dir: AgentDir(home, name)}
	if data, err := os.ReadFile(AgentConfigPath(home, name)); err == nil {
		var cfg agentConfig
		if yaml.Unmarshal(data, &cfg) == nil {
			p.Description = strings.TrimSpace(cfg.Description)
			p.ProviderType = strings.TrimSpace(cfg.Provider.Type)
			p.ModelName = strings.TrimSpace(cfg.Provider.ModelName)
		}
	}
	// Memory size is the raw byte count of this agent's memory.md.
	// Legacy homes that have not run the migration yet keep theirs at
	// the home root - count whichever exists so the panel never shows
	// a phantom zero for the default agent.
	for _, mem := range []string{filepath.Join(p.Dir, "memory.md"), filepath.Join(home, "memory.md")} {
		if fi, err := os.Stat(mem); err == nil && fi.Size() > 0 {
			p.MemorySize = int(fi.Size())
			break
		}
	}
	// SkillCount/ Skills: scan own + shared dirs, dedupe by name (the
	// same merge rule AgentSkillDirs documents). A missing dir is a
	// scan that finds nothing - never an error to surface.
	p.Skills = scanProfileSkillNames(AgentSkillDirs(home, name))
	p.SkillCount = len(p.Skills)
	return p
}

// scanProfileSkillNames walks the ordered dirs for <name>/<name>.md
// layouts and returns deduped skill names in scan order. The skills
// module owns full parsing; only the names are profile data.
func scanProfileSkillNames(dirs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if seen[e.Name()] {
				continue
			}
			seen[e.Name()] = true
			out = append(out, e.Name())
		}
	}
	return out
}