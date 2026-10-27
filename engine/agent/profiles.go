package agent

// engine/agent/profiles.go: continuous-agent homes (card t_72ecca67, settled
// design 2026-10-26). An agent IS a home directory:
//
//	main (the default agent) = the home root itself
//	  user.md, memory.md      persona files at the root
//	  skills/                 main's skill home
//	  data/                   main's sessions store, notifications, boards
//
//	agents/<name>/           every other agent
//	  config/config.yaml      layer over the home config (only what it
//	                          sets overrides)
//	  data/                   this agent's OWN sessions store,
//	                          notifications, boards - full data isolation
//	  skills/                 this agent's own skills
//	  user.md, memory.md      scoped persona files
//	  <name>.log              scoped log file (no logs/ directory)
//
// Folder presence is registration: there is no registry file. The
// former agents/default profile died migrating UP into the home root -
// MigrateAgentHomes performs that one-time move (the reverse of card
// t_7e2b7002's migration).
//
// The profile surface is store-free: LoadProfiles reads only the
// filesystem. Session binding (counts, last activity) is filled in by
// callers that hold a StoreProvider so this package never takes a
// dependency on SQLite shape.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultAgentName names the main agent. Sessions created before the
// agent column existed (or by callers that pass nothing) ride this;
// the database column keeps the literal too.
const DefaultAgentName = "default"

// Profile is one agent home rendered as data. MemorySize is the raw
// byte size of memory.md (0 = no memory written yet - the file is only
// ever created by an actual memory write, never seeded). Skills is the
// deduped scan list the host should configure for this agent.
type Profile struct {
	Name         string   `json:"name"`
	Dir          string   `json:"dir"`
	Description  string   `json:"description,omitempty"`
	ProviderType string   `json:"provider_type,omitempty"`
	ModelName    string   `json:"model_name,omitempty"`
	SkillCount   int      `json:"skill_count"`
	Skills       []string `json:"skills,omitempty"`
	MemorySize   int      `json:"memory_size"`
	// Filled by store-holding callers (never LoadProfiles):
	SessionCount int    `json:"session_count"`
	LastActivity string `json:"last_activity,omitempty"`
	// Continuous marks the main-vs-agent distinction for the roster:
	// the main agent lives at the home root and never wakes as a
	// subprocess - it is the daemon serving the HTTP channel.
	Continuous bool `json:"continuous"`
}

// agentConfig is the subset of an agent's config/config.yaml this
// package parses for display and layering decisions. Unknown keys
// (server, store, memory overrides...) are layering business for
// cmd/omega's Config and are ignored here on purpose - one parse shape
// per concern.
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

// AgentDir is the home directory of one agent. The main agent's home
// is the home root itself; every other agent lives at agents/<name>.
func AgentDir(home, name string) string {
	if name == DefaultAgentName {
		return home
	}
	return filepath.Join(home, "agents", name)
}

// AgentConfigPath is the profile's own config layer: the per-agent
// config/config.yaml for continuous agents; the home config for main
// (main layers nothing - the home config IS its config).
func AgentConfigPath(home, name string) string {
	if name == DefaultAgentName {
		return filepath.Join(home, "config", "config.yaml")
	}
	return filepath.Join(AgentDir(home, name), "config", "config.yaml")
}

// personaFiles are the main-agent persona files that migrate UP from
// the retired agents/default home into the home root.
var personaFiles = []string{"memory.md", "user.md"}

// Warnf prints a loud migration warning to stderr. Package-level so
// tests can capture it (mirrors modules/datadir's hook).
var Warnf = func(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "omega agents: "+format+"\n", args...)
}

// MigrateAgentHomes retires the agents/default profile (card
// t_72ecca67, settled ruling 3): its persona files move UP to the home
// root, its skills merge forward into the home skills/ (the old scan
// order resolved default's own skills BEFORE the legacy home/skills, so
// merging with overwrite preserves what the loader used to serve), and
// the directory itself dies - any residue that would be destroyed by
// the removal is archived intact to data/migrated-agents-default
// instead. Missing or already-migrated homes are the common case: the
// second boot is a no-op. The home config.yaml is deliberately not
// touched - it is the base layer every agent's config layers over.
func MigrateAgentHomes(home string) error {
	dir := filepath.Join(home, "agents", DefaultAgentName)
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return nil // no agents/default on this home: nothing to retire
	}
	// Persona files: rename up, exactly one copy. The home-root file
	// wins a conflict (it is the settled location); the default's
	// divergent copy is not silently destroyed - the whole-dir archive
	// below only fires when residue remains, so a conflicting persona
	// is reported and removed here, named loudly.
	for _, name := range personaFiles {
		legacy := filepath.Join(dir, name)
		target := filepath.Join(home, name)
		if _, err := os.Stat(legacy); err != nil {
			continue // moved already or never written
		}
		// The home-root persona file wins the conflict (it is the
		// settled location): the divergent legacy copy dies, named
		// loudly.
		if _, err := os.Stat(target); err == nil {
			if data, errRead := os.ReadFile(legacy); errRead == nil {
				if existing, err2 := os.ReadFile(target); err2 == nil && string(data) != string(existing) {
					Warnf("agents/default/%s diverged from the home-root %s - the home-root file wins (ruling 3)", name, name)
				}
			}
			if err := os.Remove(legacy); err != nil {
				return fmt.Errorf("retire divergent agents/default/%s: %w", name, err)
			}
			continue
		}
		if err := os.Rename(legacy, target); err != nil {
			return fmt.Errorf("migrate %s up from agents/default: %w", name, err)
		}
	}
	// Skills: the old default scan resolved agents/default/skills
	// before the legacy home/skills. Merge forward with overwrite so
	// an equally-named skill keeps resolving the same content; the
	// source subtree is then removed (copy, not copy-and-leave).
	skillSrc := filepath.Join(dir, "skills")
	if entries, err := os.ReadDir(skillSrc); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			dst := filepath.Join(home, "skills", e.Name())
			if err := copyTree(filepath.Join(skillSrc, e.Name()), dst); err != nil {
				return fmt.Errorf("merge default agent skill %s: %w", e.Name(), err)
			}
		}
		if err := os.RemoveAll(skillSrc); err != nil {
			return fmt.Errorf("retire merged default agent skills: %w", err)
		}
	}
	// The default dir dies. Anything left that this migration does not
	// understand (a hand-edited config layer, stray logs, who knows) is
	// archived intact - a profile retirement never destroys data.
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		archive := filepath.Join(home, "data", "migrated-agents-default")
		if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
			return fmt.Errorf("prepare migration archive: %w", err)
		}
		if _, err := os.Stat(archive); err == nil {
			// A previous archived attempt: keep both, do not clobber.
			archive = archive + fmt.Sprintf("-%d", time.Now().Unix())
		}
		if err := os.Rename(dir, archive); err != nil {
			return fmt.Errorf("archive agents/default residue: %w", err)
		}
		Warnf("agents/default had non-persona residue - archived intact to %s", archive)
		return nil
	} else if err != nil {
		return fmt.Errorf("read agents/default residue: %w", err)
	}
	if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove agents/default (retired, ruling 3): %w", err)
	}
	// The agents/ parent dies too when nothing else lives in it.
	_ = os.Remove(filepath.Join(home, "agents"))
	return nil
}

// copyTree copies one filesystem entry recursively, overwriting the
// destination file contents. Used only by the default-agent skills
// merge, where the old scan order resolved these files first.
func copyTree(src, dst string) error {
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// ScaffoldAgent creates agents/<name>/ in the settled home shape:
// config/ (with a commented config/config.yaml layer), data/, skills/.
// memory.md/user.md are NOT seeded - the memory tool's own description
// teaches the file conventions and an agent's first memory write
// creates them (privacy law: agent homes are never seeded with
// content, personal or otherwise). The home must already exist.
func ScaffoldAgent(home, name, description string) error {
	if err := ValidAgentName(name); err != nil {
		return err
	}
	if name == DefaultAgentName {
		return fmt.Errorf("%q is the main agent - it exists by definition at the home root", DefaultAgentName)
	}
	dir := AgentDir(home, name)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("agent %q already exists at %s", name, dir)
	}
	for _, sub := range []string{"config", "data", "skills"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return fmt.Errorf("scaffold agent %s: %w", name, err)
		}
	}
	cfg := fmt.Sprintf(defaultAgentConfigTemplate, name, description)
	if err := os.WriteFile(AgentConfigPath(home, name), []byte(cfg), 0o644); err != nil {
		return fmt.Errorf("scaffold agent %s: %w", name, err)
	}
	return nil
}

// defaultAgentConfigTemplate is the scaffold config layer. Every knob
// it mentions is commented out: an unscaffolded edit cannot break boot.
// The layering rule is written into the file so editing agents see it
// without a doc trip.
const defaultAgentConfigTemplate = `# omega continuous agent: %s
# This file layers over the home config.yaml - only what you un-comment
# here overrides. memory.md, user.md, skills/ and data/ are scoped to
# this agent home; this agent's sessions live in its own data/ store.
#
# Schedules: config/crons.yaml in this directory holds this agent's
# wake schedules - human-owned config, the agent has no tool that
# touches it.
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
// the shared root first (every agent sees home/skills/shared), then
// the agent's own skills/. Main additionally keeps the home/skills
// root as its own skill home. Loaders dedupe skills by name in this
// scan order with first-match winning.
func AgentSkillDirs(home, name string) []string {
	var dirs []string
	if shared := filepath.Join(home, "skills", "shared"); dirExists(shared) {
		dirs = append(dirs, shared)
	}
	if name == DefaultAgentName {
		if dirExists(filepath.Join(home, "skills")) {
			dirs = append(dirs, filepath.Join(home, "skills"))
		}
		return dirs
	}
	if own := filepath.Join(AgentDir(home, name), "skills"); dirExists(own) {
		dirs = append(dirs, own)
	}
	return dirs
}

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// LoadProfiles scans the home's agent homes into profile records. The
// main profile always exists (its home is the root itself); the rest
// are directory children of agents/, sorted by name. Name validation
// is defensive: a malformed directory is skipped, never fatal - a
// stray dir must not take the surface down.
func LoadProfiles(home string) []Profile {
	out := []Profile{mainProfile(home)}
	entries, err := os.ReadDir(filepath.Join(home, "agents"))
	if err != nil {
		return out // no agents/ yet: just the main agent
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
		return mainProfile(home), nil
	}
	dir := AgentDir(home, name)
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return Profile{}, fmt.Errorf("unknown agent %q", name)
	}
	return loadProfile(home, name), nil
}

// mainProfile renders the home root as the main agent's profile.
// Memory size counts the home-root memory.md; a pre-migration home
// that still holds agents/default also counts its copy so the panel
// never shows a phantom zero between boots.
func mainProfile(home string) Profile {
	p := Profile{Name: DefaultAgentName, Dir: home}
	for _, mem := range []string{filepath.Join(home, "memory.md"), filepath.Join(home, "agents", "default", "memory.md")} {
		if fi, err := os.Stat(mem); err == nil && fi.Size() > 0 {
			p.MemorySize = int(fi.Size())
			break
		}
	}
	p.Skills = scanProfileSkillNames(AgentSkillDirs(home, DefaultAgentName))
	p.SkillCount = len(p.Skills)
	return p
}

// loadProfile parses one existing agent home. Read failures degrade
// gracefully - a profile row with defaults beats a broken list.
func loadProfile(home, name string) Profile {
	p := Profile{Name: name, Dir: AgentDir(home, name), Continuous: true}
	if data, err := os.ReadFile(AgentConfigPath(home, name)); err == nil {
		var cfg agentConfig
		if yaml.Unmarshal(data, &cfg) == nil {
			p.Description = strings.TrimSpace(cfg.Description)
			p.ProviderType = strings.TrimSpace(cfg.Provider.Type)
			p.ModelName = strings.TrimSpace(cfg.Provider.ModelName)
		}
	}
	// Memory size is the raw byte count of this agent's memory.md.
	// A pre-migration default copy still resolves through the retired
	// dir so the panel never shows a phantom zero mid-migration.
	for _, mem := range []string{filepath.Join(p.Dir, "memory.md"), filepath.Join(home, "agents", DefaultAgentName, "memory.md")} {
		if fi, err := os.Stat(mem); err == nil && fi.Size() > 0 {
			p.MemorySize = int(fi.Size())
			break
		}
	}
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
