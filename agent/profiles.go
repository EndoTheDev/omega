package agent

// agent/profiles.go: continuous-agent homes (card t_72ecca67, settled
// design 2026-10-26; the definition layer of 0.17.3 rides on top). An
// agent IS a home directory:
//
//	main (the default agent) = the home root itself
//	  user.md, memory.md, soul.md   persona files at the root
//	  skills/                       main's skill home
//	  data/                         main's sessions store, notifications, boards
//
//	agents/<name>/                every other agent
//	  config/config.yaml          the DEFINITION layer (configedit.go)
//	  data/                       this agent's OWN sessions store,
//	                              notifications, boards - full data isolation
//	  skills/                     this agent's own skills
//	  user.md, memory.md, soul.md scoped persona files, ONE soul per agent
//	  <name>.log                  scoped log file (no logs/ directory)
//
// Folder presence is registration: there is no registry file. The
// former agents/default profile died migrating UP into the home root -
// MigrateAgentHomes performs that one-time move (the reverse of card
// t_7e2b7002's migration).
//
// Two laws live here because both are HOME-SHAPE facts:
//
//	INHERIT-AS-DISABLED - main's persona files and skills are visible to
//	a secondary agent and inert until that agent enables the kind;
//	enabling MATERIALIZES a copy into the agent home (never a live
//	link), so the agent's copies diverge freely afterwards.
//	CLONE - "inherit from agent" at create is a materialized full copy
//	at birth (config, skills, toggles, persona files, the session
//	store), not a link. Deleting the source afterwards is legal;
//	clone-then-delete IS the rename path.
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

// SoulFileName is the personality/behaviour file, beside user.md and
// memory.md. One per agent: main's lives at the home root, every other
// agent's in its own home.
const SoulFileName = "soul.md"

// Profile is one agent home rendered as data. MemorySize is the raw
// byte size of memory.md (0 = no memory written yet - the file is only
// ever created by an actual memory write, never seeded); SoulSize is
// the same for soul.md. Skills is the VISIBLE skill list; SkillsAllow
// is the explicit allowlist that actually runs (empty = none).
type Profile struct {
	Name             string   `json:"name"`
	Dir              string   `json:"dir"`
	Description      string   `json:"description,omitempty"`
	ProviderInstance string   `json:"provider_instance,omitempty"`
	ModelName        string   `json:"model_name,omitempty"`
	Toggles          []string `json:"toggles,omitempty"`
	SkillsAllow      []string `json:"skills_allow,omitempty"`
	Inherits         []string `json:"inherits,omitempty"`
	SkillCount       int      `json:"skill_count"`
	Skills           []string `json:"skills,omitempty"`
	MemorySize       int      `json:"memory_size"`
	SoulSize         int      `json:"soul_size"`
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
		Instance  string `yaml:"instance"`
		ModelName string `yaml:"model_name"`
	} `yaml:"provider"`
	Toggles []string `yaml:"toggles"`
	Skills  *struct {
		Allow []string `yaml:"allow"`
	} `yaml:"skills"`
	Inherit *AgentInherit `yaml:"inherit"`
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

// AgentDir is the home directory of one agent. The main agent's home is
// the home root itself; every other agent lives at agents/<name>.
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

// personaFiles are the agent persona files: the main-agent copies
// migrate UP from the retired agents/default home into the home root,
// and the same three are what the inherit flags materialize.
var personaFiles = []string{"memory.md", "user.md", SoulFileName}

// Warnf prints a loud migration warning to stderr. Package-level so
// tests can capture it (mirrors harness/datadir's hook).
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
	// below only fires when residue remains, so a conflicting persona is
	// reported and removed here, named loudly.
	for _, name := range personaFiles {
		legacy := filepath.Join(dir, name)
		target := filepath.Join(home, name)
		if _, err := os.Stat(legacy); err != nil {
			continue // moved already or never written
		}
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
	// Skills: the old default scan resolved agents/default/skills before
	// the legacy home/skills. Merge forward with overwrite so an
	// equally-named skill keeps resolving the same content; the source
	// subtree is then removed (copy, not copy-and-leave).
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
// destination file contents. A SYMLINK in the source is refused: the
// destination is a different home, and a link copied across it either
// escapes the agent's own tree or dangles. Callers (migration, clone)
// would rather fail loud than half-copy a jail escape.
func copyTree(src, dst string) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink - refusing to copy a link into another home", src)
	}
	if !fi.IsDir() {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		return WriteFileAtomic(dst, data, 0o644)
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
// memory.md, user.md and soul.md are NOT seeded - the memory tool's own
// description teaches the file conventions and an agent's first write
// creates them (privacy law: agent homes are never seeded with content,
// personal or otherwise). Toggles and skills allowlist are absent, which
// IS the always-off starting state. The home must already exist.
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
	if err := WriteFileAtomic(AgentConfigPath(home, name), []byte(cfg), 0o644); err != nil {
		return fmt.Errorf("scaffold agent %s: %w", name, err)
	}
	return nil
}

// defaultAgentConfigTemplate is the scaffold config layer. Every knob it
// mentions is commented out: an unscaffolded edit cannot break boot.
// The laws are written into the file so editing agents see them without
// a doc trip.
const defaultAgentConfigTemplate = `# omega continuous agent: %s
# This file is the whole definition of this agent. It layers over the
# home config.yaml only where it names a key. memory.md, user.md,
# soul.md, skills/ and data/ are scoped to this agent home; this
# agent's sessions live in its own data/ store.
#
# Schedules: config/crons.yaml in this directory holds this agent's
# wake schedules - human-owned config, the agent has no tool that
# touches it.
#
# SECRET-FREE: provider type, host and api_key are defined ONCE in the
# home settings. An agent names an instance; it never carries a secret.
# "default" is the host's own provider section.
#
#   provider:
#     instance: default
#     model_name: glm-5.3
#   system_prompt: ""
#
# ALWAYS OFF: the locked pair (agent-loop + provider) runs; nothing else
# does until it is named here. An agent can only enable what its host
# has enabled.
#
#   toggles: [kanban, cron]
#
# SKILLS are an explicit allowlist over the registry. Empty = NO skills
# (never silently inherited from main).
#
#   skills:
#     allow: [omega-dev]
#
# main's skills, soul.md, user.md and memory.md are visible but OFF by
# default. Enabling a kind MATERIALIZES a copy into this home.
#
#   inherit:
#     skills: false
#     soul: false
#     user: false
#     memory: false
description: %q
`

// AgentSkillDirs returns the ordered skill scan list for an agent: the
// shared root first (every agent sees home/skills/shared), then the
// agent's own skills/. Main additionally keeps the home/skills root as
// its own skill home. Loaders dedupe skills by name in this scan order
// with first-match winning.
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

// mainSkillNames lists main's OWN skills (home/skills, minus the shared
// root every agent already sees). These are the skills a secondary
// agent may inherit - visible in its registry, inert until allowlisted.
func mainSkillNames(home string) []string {
	root := filepath.Join(home, "skills")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "shared" || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

// VisibleSkillNames is the registry an agent can see and allowlist: its
// own skills, the shared root, and - when the inherit flag is on -
// main's own skills. Sorted, deduped.
func VisibleSkillNames(home, name string, inheritMain bool) []string {
	names := scanProfileSkillNames(AgentSkillDirs(home, name))
	if inheritMain {
		names = append(names, mainSkillNames(home)...)
	}
	sort.Strings(names)
	deduped := names[:0]
	for i, n := range names {
		if i > 0 && n == names[i-1] {
			continue
		}
		deduped = append(deduped, n)
	}
	return deduped
}

// MaterializeInheritance makes an enabled inherit kind real: main's
// soul.md / user.md / memory.md are copied into the agent home, and
// main's skills named in the allowlist are copied into the agent's own
// skills/ - always WITHOUT clobbering a file or skill the agent already
// owns. Copies, never links (the clone law's philosophy): once
// materialized, the agent's artifact diverges freely, and switching a
// flag back off never deletes what the agent has since made its own.
func MaterializeInheritance(home, name string, lay AgentConfigLayer) error {
	if name == DefaultAgentName {
		return nil
	}
	dir := AgentDir(home, name)
	for _, kind := range []struct {
		on   bool
		file string
	}{
		{lay.Inherit.Soul, SoulFileName},
		{lay.Inherit.User, "user.md"},
		{lay.Inherit.Memory, "memory.md"},
	} {
		if !kind.on {
			continue
		}
		src := filepath.Join(home, kind.file)
		dst := filepath.Join(dir, kind.file)
		if _, err := os.Stat(src); err != nil {
			continue // main has no such file: nothing to inherit
		}
		if _, err := os.Stat(dst); err == nil {
			continue // the agent owns its copy already - never clobber
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("inherit %s: %w", kind.file, err)
		}
		if err := WriteFileAtomic(dst, data, 0o644); err != nil {
			return fmt.Errorf("inherit %s: %w", kind.file, err)
		}
	}
	if !lay.Inherit.Skills {
		return nil
	}
	for _, skill := range lay.SkillsAllow {
		dst := filepath.Join(dir, "skills", skill)
		if _, err := os.Stat(dst); err == nil {
			continue // already the agent's own skill
		}
		src := filepath.Join(home, "skills", skill)
		if _, err := os.Stat(src); err != nil {
			continue // not a main skill
		}
		if err := copyTree(src, dst); err != nil {
			return fmt.Errorf("inherit skill %s: %w", skill, err)
		}
	}
	return nil
}

// StoreSnapshot copies one SQLite store file into a NEW destination
// through SQLite itself, so a LIVE WAL-mode database yields a
// consistent snapshot. It is a hook, not an implementation: this
// package owns home shape and never touches SQLite (the host wires it
// to harness/store, which owns the driver).
//
// A nil hook FAILS the clone loudly. Falling back to a byte copy is not
// an option: the source store is open at clone time (main's is the
// serving daemon's own; a wake child's is open while it runs), and a
// byte copy of a live WAL database is a torn snapshot - committed rows
// still in the -wal are missed or the file is corrupt (card
// t_f47319d2 review, Important 1).
var StoreSnapshot func(srcDSN, dstPath string) error

// CloneAgent materializes a whole agent home at birth (the CLONE law):
// the source's config directory, skill tree, persona files and stores
// are COPIED into agents/<dst>, never linked. The clone diverges
// freely afterwards, and deleting the source is legal - clone-then-
// delete is the rename path, which is why no rename machinery exists.
//
// The enumeration is deliberately COMPLETE, because the rename path
// leans on it: an agent home whose schedules or boards were left behind
// is data the operator cannot regenerate (Important 2 of the same
// review). See materializeClone for the list.
//
// Cloning MAIN copies its skills, persona files, schedules and stores
// but NOT the home config: that file carries the host's provider
// secret, and an agent file holds no secret ever (the clone starts on
// the shared scaffold template, inheriting the host's provider by
// reference).
//
// A failure anywhere after the scaffold removes the destination: folder
// presence IS registration, and the directory was born in this call
// (pre-existence is refused above), so removal restores the pre-call
// state instead of registering a phantom agent (Important 3).
//
// Rows in a copied store still carry the SOURCE agent tag - the caller
// re-tags them (harness/store.RetagAgentSessions) because this package
// never touches SQLite shape.
func CloneAgent(home, src, dst, description string) error {
	if err := ValidAgentName(dst); err != nil {
		return err
	}
	if dst == DefaultAgentName {
		return fmt.Errorf("%q is the main agent - it exists by definition at the home root", DefaultAgentName)
	}
	if src != DefaultAgentName {
		if err := ValidAgentName(src); err != nil {
			return fmt.Errorf("clone source: %w", err)
		}
	}
	srcDir := AgentDir(home, src)
	if fi, err := os.Stat(srcDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("unknown agent %q", src)
	}
	dstDir := AgentDir(home, dst)
	if _, err := os.Stat(dstDir); err == nil {
		return fmt.Errorf("agent %q already exists at %s", dst, dstDir)
	}
	if err := ScaffoldAgent(home, dst, description); err != nil {
		return err
	}
	if err := materializeClone(home, src, dst, description); err != nil {
		return discardHalfBuilt(dstDir, err)
	}
	return nil
}

// materializeClone is the copy half of the clone law. Everything it
// names is a COPY, never a link:
//
//	config/      the whole directory - the definition layer AND the
//	             hand-owned files beside it (crons.yaml schedules; per
//	             the cron law the agent has no tool that can rewrite
//	             them). Main's config.yaml is the one exclusion: it is
//	             the host definition and carries the provider secret.
//	skills/      the source's own skill tree
//	personas     memory.md, user.md, soul.md
//	data/*.db    every store the agent owns, snapshotted through SQLite:
//	             omega.db (sessions), notifications.db, kanban.db and
//	             ledger.db. The -wal/-shm sidecars are NOT copied; the
//	             snapshot carries the wal's committed frames into the
//	             destination file.
//
// A store copied here is the CLONE's own from that instant - main's
// included: cloning main copies main's stores, which are the host's
// boards and feed only because main IS the host.
func materializeClone(home, src, dst, description string) error {
	srcDir, dstDir := AgentDir(home, src), AgentDir(home, dst)
	if err := cloneConfigDir(home, src, dst); err != nil {
		return err
	}
	// Skills: the source's own tree (for main, home/skills minus the
	// shared root every agent already sees).
	if src == DefaultAgentName {
		for _, skill := range mainSkillNames(home) {
			if err := copyTree(filepath.Join(home, "skills", skill), filepath.Join(dstDir, "skills", skill)); err != nil {
				return fmt.Errorf("clone skill %s: %w", skill, err)
			}
		}
	} else if entries, err := os.ReadDir(filepath.Join(srcDir, "skills")); err == nil {
		for _, e := range entries {
			if err := copyTree(filepath.Join(srcDir, "skills", e.Name()), filepath.Join(dstDir, "skills", e.Name())); err != nil {
				return fmt.Errorf("clone skill %s: %w", e.Name(), err)
			}
		}
	}
	// Persona files: one copy of each, never seeded from nothing.
	for _, file := range personaFiles {
		srcPath := filepath.Join(srcDir, file)
		if _, err := os.Stat(srcPath); err != nil {
			continue
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("clone %s: %w", file, err)
		}
		if err := WriteFileAtomic(filepath.Join(dstDir, file), data, 0o644); err != nil {
			return fmt.Errorf("clone %s: %w", file, err)
		}
	}
	if err := cloneStores(srcDir, dstDir); err != nil {
		return err
	}
	// A description on the create request replaces the source's, in the
	// copied layer or the scaffold alike.
	if description != "" && src != DefaultAgentName {
		if err := UpdateAgentConfigLayer(home, dst, AgentConfigUpdates{Description: &description}); err != nil {
			return fmt.Errorf("clone description: %w", err)
		}
	}
	return nil
}

// cloneConfigDir copies the source's whole config directory. Main's
// config directory is the host's (home/config), and its config.yaml is
// skipped: it is the host definition and carries the provider secret -
// the clone keeps the scaffold template and references the host
// provider by name. Every other file travels verbatim (crons.yaml above
// all: hand-owned schedules with no tool to rewrite them; trust.yaml
// too - the clone inherits the decisions and diverges from there, which
// is what "materialize a copy" means for a hand-owned file).
func cloneConfigDir(home, src, dst string) error {
	srcCfg := filepath.Join(AgentDir(home, src), "config")
	if src == DefaultAgentName {
		srcCfg = filepath.Join(home, "config")
	}
	entries, err := os.ReadDir(srcCfg)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no config dir: the scaffold's layer stands
		}
		return fmt.Errorf("clone config: %w", err)
	}
	dstCfg := filepath.Join(AgentDir(home, dst), "config")
	for _, e := range entries {
		if src == DefaultAgentName {
			if e.Name() == "config.yaml" {
				continue // the host definition: secret carrier, never an agent file
			}
			if e.Name() == "crons.yaml" {
				// Main's schedules travel only when they can LIVE in an
				// agent home; see cloneCronLegality.
				if err := cloneCronLegality(filepath.Join(srcCfg, e.Name())); err != nil {
					return err
				}
			}
		}
		if err := copyTree(filepath.Join(srcCfg, e.Name()), filepath.Join(dstCfg, e.Name())); err != nil {
			return fmt.Errorf("clone config/%s: %w", e.Name(), err)
		}
	}
	return nil
}

// cloneCronLegality reports why main's crons.yaml cannot live in an
// agent home, or nil when it can. harness/cron owns these laws and
// enforces them at LOAD time, where they are fatal - so copying main's
// file verbatim would not merely duplicate schedules, it would break the
// daemon's next boot. Refusing the clone names the offending job and the
// way out; the alternative (silently dropping the schedules) is the data
// loss the clone law exists to prevent.
//
// The three laws, mirrored from the loader: a job in an agent home may
// not author `agent:` (the owning agent is the loader's, not the file's),
// may not carry a deliver target (a wake delivers into the agent's own
// canonical chat), and may not pin skills (a wake's fire prompt is the
// continuous capsule's property - the agent's own skills stand). Every
// one of them is a boot-fatal load error, so the gate has to mirror ALL
// three: missing any one of them turns a legal main-home file into a
// daemon that will not start.
func cloneCronLegality(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no schedules: nothing to carry
		}
		return fmt.Errorf("clone main's crons.yaml: %w", err)
	}
	var raw struct {
		Jobs []struct {
			Name    string   `yaml:"name"`
			Agent   string   `yaml:"agent"`
			Deliver string   `yaml:"deliver"`
			Skills  []string `yaml:"skills"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("clone main's crons.yaml: it does not parse (%v) - a clone cannot carry a schedule file the cron loader will refuse", err)
	}
	for _, job := range raw.Jobs {
		if job.Agent != "" {
			return fmt.Errorf("clone main: job %q authors agent: %q, which an agent home's crons.yaml may not own - strip it from main's file (or clone a secondary agent), the schedules are not being silently dropped", job.Name, job.Agent)
		}
		if job.Deliver != "" && job.Deliver != "none" {
			return fmt.Errorf("clone main: job %q delivers to %q, a main-home feature the cron loader refuses in an agent home - strip deliver: from that job (or clone a secondary agent), the schedules are not being silently dropped", job.Name, job.Deliver)
		}
		// The loader normalizes the list before it judges it (entries
		// trim, empties drop), so `skills: [""]` is legal in an agent
		// home. Mirror that, or the gate refuses a file that would load.
		if pinsSkills(job.Skills) {
			return fmt.Errorf("clone main: job %q pins skills, a main-home feature the cron loader refuses in an agent home (a wake's fire prompt follows the agent's own skills) - strip skills: from that job (or clone a secondary agent), the schedules are not being silently dropped", job.Name)
		}
	}
	return nil
}

// pinsSkills mirrors harness/cron's normalizeSkills + the agent-home
// skills law: whitespace entries trim and empty entries drop, and only a
// non-empty remainder is a pinned list.
func pinsSkills(in []string) bool {
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

// cloneStores copies every store the source owns. data/ also holds
// runtime scratch that means nothing in another home (the terminal
// process registry is live PIDs), so the enumeration is the *.db set -
// the stores the home shape names, plus any store a later capsule adds.
func cloneStores(srcDir, dstDir string) error {
	srcData := filepath.Join(srcDir, "data")
	entries, err := os.ReadDir(srcData)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // never ran: no stores to copy
		}
		return fmt.Errorf("clone data: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".db") {
			continue // sidecars (-wal/-shm) and scratch are not stores
		}
		if StoreSnapshot == nil {
			return fmt.Errorf("clone store %s: no store snapshot wired - the host wires harness/store's SQLite snapshot, and a byte copy of a live WAL store is not a copy", e.Name())
		}
		if err := StoreSnapshot(filepath.Join(srcData, e.Name()), filepath.Join(dstDir, "data", e.Name())); err != nil {
			return fmt.Errorf("clone store %s: %w", e.Name(), err)
		}
	}
	return nil
}

// discardHalfBuilt removes a destination this clone created but did not
// finish. Registration is folder presence, so a half-built home would
// list as an agent forever with no cleanup path; the caller's
// pre-existence check guarantees the directory was born in this call.
// A removal that fails is reported WITH the original cause (the operator
// has to clean it by hand, and silent registration is worse than noise).
func discardHalfBuilt(dir string, cause error) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("%w - and the half-built %s could not be removed (%v); folder presence registers an agent, so delete it by hand", cause, dir, err)
	}
	return cause
}

// LoadProfiles scans the home's agent homes into profile records. The
// main profile always exists (its home is the root itself); the rest
// are directory children of agents/, sorted by name. Name validation is
// defensive: a malformed directory is skipped, never fatal - a stray
// dir must not take the surface down.
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
// MemorySize counts the home-root memory.md; a pre-migration home that
// still holds agents/default also counts its copy so the panel never
// shows a phantom zero between boots.
func mainProfile(home string) Profile {
	p := Profile{Name: DefaultAgentName, Dir: home}
	for _, mem := range []string{filepath.Join(home, "memory.md"), filepath.Join(home, "agents", "default", "memory.md")} {
		if fi, err := os.Stat(mem); err == nil && fi.Size() > 0 {
			p.MemorySize = int(fi.Size())
			break
		}
	}
	if fi, err := os.Stat(filepath.Join(home, SoulFileName)); err == nil && fi.Size() > 0 {
		p.SoulSize = int(fi.Size())
	}
	p.Skills = scanProfileSkillNames(AgentSkillDirs(home, DefaultAgentName))
	p.SkillCount = len(p.Skills)
	return p
}

// loadProfile parses one existing agent home. Read failures degrade
// gracefully - a profile row with defaults beats a broken list.
func loadProfile(home, name string) Profile {
	p := Profile{Name: name, Dir: AgentDir(home, name), Continuous: true}
	var inherit AgentInherit
	if data, err := os.ReadFile(AgentConfigPath(home, name)); err == nil {
		var cfg agentConfig
		if yaml.Unmarshal(data, &cfg) == nil {
			p.Description = strings.TrimSpace(cfg.Description)
			p.ProviderInstance = strings.TrimSpace(cfg.Provider.Instance)
			p.ModelName = strings.TrimSpace(cfg.Provider.ModelName)
			p.Toggles = normalizeNames(cfg.Toggles)
			if cfg.Skills != nil {
				p.SkillsAllow = normalizeNames(cfg.Skills.Allow)
			}
			if cfg.Inherit != nil {
				inherit = *cfg.Inherit
			}
		}
	}
	for _, kind := range []struct {
		on   bool
		name string
	}{
		{inherit.Skills, "skills"},
		{inherit.Soul, "soul"},
		{inherit.User, "user"},
		{inherit.Memory, "memory"},
	} {
		if kind.on {
			p.Inherits = append(p.Inherits, kind.name)
		}
	}
	// Memory and soul size are the raw byte counts of this agent's own
	// files. A pre-migration default copy still resolves through the
	// retired dir so the panel never shows a phantom zero mid-migration.
	for _, mem := range []string{filepath.Join(p.Dir, "memory.md"), filepath.Join(home, "agents", DefaultAgentName, "memory.md")} {
		if fi, err := os.Stat(mem); err == nil && fi.Size() > 0 {
			p.MemorySize = int(fi.Size())
			break
		}
	}
	if fi, err := os.Stat(filepath.Join(p.Dir, SoulFileName)); err == nil && fi.Size() > 0 {
		p.SoulSize = int(fi.Size())
	}
	p.Skills = scanProfileSkillNames(VisibleSkillDirs(home, name, inherit.Skills))
	p.SkillCount = len(p.Skills)
	return p
}

// VisibleSkillDirs is AgentSkillDirs plus main's own skill root when
// the agent inherits skills - the visible registry, in scan order.
func VisibleSkillDirs(home, name string, inheritMain bool) []string {
	dirs := AgentSkillDirs(home, name)
	if inheritMain && name != DefaultAgentName {
		if dir := filepath.Join(home, "skills"); dirExists(dir) {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// scanProfileSkillNames walks the ordered dirs for <name>/<name>.md
// layouts and returns deduped skill names in scan order. The skills
// module owns full parsing; only the names are profile data. The
// shared root is a container of skills, never a skill itself.
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
			if e.Name() == "shared" {
				continue // the shared container, not a skill
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
