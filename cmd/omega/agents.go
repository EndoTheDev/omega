package main

// cmd/omega/agents.go: continuous-agent home wiring. The resolved agent then gets its
// own skills, memory files, log file, and data dir - a wake's writes never land in
// another agent's files (full data isolation).

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/harness/datadir"
	"github.com/EndoTheDev/omega/harness/store"
	"gopkg.in/yaml.v3"
)

// resolveAgentName returns the OMEGA_AGENT target of this process.
func resolveAgentName() string {
	return strings.TrimSpace(os.Getenv("OMEGA_AGENT"))
}

// providerInstanceNames renders the configured instance ids for an
// error message, sorted so the text is stable.
func providerInstanceNames(cfg *Config) string {
	if len(cfg.Provider.Instances) == 0 {
		return "none configured"
	}
	names := make([]string, 0, len(cfg.Provider.Instances))
	for _, inst := range cfg.Provider.Instances {
		names = append(names, inst.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// containsString is the small membership test the layer reads use.
func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// applyModelEnvOverride re-asserts the OMEGA_MODEL env table entry on a config.
// applyEnv runs inside LoadConfig, BEFORE the layer, so a layer defining provider.model
// silently clobbered an explicit override (wake children combine both: OMEGA_AGENT for
// the layer, OMEGA_MODEL for the fire's model - the wake contract says the env table
// entry wins).
func applyModelEnvOverride(cfg *Config) {
	m := strings.TrimSpace(os.Getenv("OMEGA_MODEL"))
	if m != "" {
		cfg.Provider.ModelName = m
	}
}

// applyDBPathEnvOverride re-asserts the OMEGA_DB_PATH env table entry on a config. The
// layer's data isolation re-points a store that still reads as the home default at
// <home>/agents/<name>/data/omega.db; but an env-set path is CALLER INTENT - a delegate
// child must open the store its parent pre-created its session in, or `run --session
// <id>` answers "session not found" and the child dies before the loop.
func applyDBPathEnvOverride(cfg *Config) {
	if p := strings.TrimSpace(os.Getenv("OMEGA_DB_PATH")); p != "" {
		cfg.Store.DBPath = canonPath(p)
	}
}

// ApplyAgentLayer overlays agents/<name>/config/config.yaml onto cfg and resolves. A
// continuous agent gets its own skills, memory/user files, log file, and data directory
// so one wake never writes into another agent's home. A missing layer is the caller's
// problem (profiles are validated before they spawn children) - a malformed layer is a
// hard error: run half of one agent's config is worse than a loud stop.
// resolveHomePaths (or its caller) must have already run on cfg: this function
// overwrites the home-relative defaults with agent ones.
func ApplyAgentLayer(cfg *Config, home, name string) error {
	// Validate the profile exists up front: an unknown agent never half-runs on the home.
	if _, err := agent.LoadProfile(home, name); err != nil {
		return err
	}
	if name == agent.DefaultAgentName {
		return nil // main = the home root itself (rule 1)
	}
	data, err := os.ReadFile(agent.AgentConfigPath(home, name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil // valid dir, empty layer: home config untouched
		}
		return fmt.Errorf("read agent config: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("parse agent config %s: %w", name, err)
	}
	lay, err := agent.ReadAgentConfigLayer(home, name)
	if err != nil {
		return err
	}
	// SECRET-FREE: a file that still names a provider type, host or api_key is a law. The
	// values were never read (the read lane drops them); the wake stops here and names the
	// scrub verb.
	if !lay.SecretFree() {
		return fmt.Errorf("agent config %s carries %s - agent configs are secret-free: type, host and api_key are defined once in settings; reference an instance with provider.instance, and scrub the legacy keys with unset: [\"provider.api_key\"] etc.", name, strings.Join(lay.LegacySecrets, ", "))
	}
	// PROVIDER = A REFERENCED INSTANCE.
	if lay.ProviderInstance.Set {
		inst := lay.ProviderInstance.Value
		if inst != "" && inst != agent.DefaultProviderInstance {
			resolved, ok := cfg.Provider.instance(inst)
			if !ok {
				return fmt.Errorf("agent config %s references provider instance %q, which settings does not define (want one of: %s, or %q)", name, inst, providerInstanceNames(cfg), agent.DefaultProviderInstance)
			}
			cfg.setDefaultInstance(resolved)
		}
	}
	// INHERIT-AS-DISABLED: a hand-edited flag materializes the same way a written one does
	// - idempotent, and it never clobbers a file the agent already owns.
	if err := agent.MaterializeInheritance(home, name, lay); err != nil {
		return err
	}
	// ALWAYS OFF / SKILL ALLOWLIST. The allowlist is intersected with the host's
	// disabled.skills fold (the subset law), and it is SET even when empty - empty means
	// NO skills, never inherited.
	cfg.agentName = name
	cfg.agentToggles = lay.Toggles
	allow := make([]string, 0, len(lay.SkillsAllow))
	for _, skill := range lay.SkillsAllow {
		if containsString(cfg.Disabled.SkillNames, skill) {
			continue // the host (or this layer) has it off
		}
		allow = append(allow, skill)
	}
	cfg.agentSkillsAllow, cfg.agentSkillsAllowSet = allow, true
	var pathKnobs struct {
		Skills  SkillsConfig  `yaml:"skills"`
		Memory  MemoryConfig  `yaml:"memory"`
		Logging LoggingConfig `yaml:"logging"`
	}
	if err := yaml.Unmarshal(data, &pathKnobs); err != nil {
		return fmt.Errorf("parse agent config %s: %w", name, err)
	}
	dir := canonPath(agent.AgentDir(home, name))
	if pathKnobs.Skills.Dir == "" {
		cfg.Skills.Dir = canonPath(filepath.Join(dir, "skills"))
	}
	if pathKnobs.Skills.SharedDir == "" {
		cfg.Skills.SharedDir = canonPath(filepath.Join(home, "skills", "shared"))
	}
	if pathKnobs.Memory.File == "" {
		cfg.Memory.File = canonPath(filepath.Join(dir, "memory.md"))
	}
	if pathKnobs.Memory.UserProfileFile == "" {
		cfg.Memory.UserProfileFile = canonPath(filepath.Join(dir, "user.md"))
	}
	if pathKnobs.Logging.File == "" {
		cfg.Logging.File = canonPath(filepath.Join(dir, name+".log"))
	}
	agentDataDir := canonPath(filepath.Join(dir, "data"))
	if cfg.Store.DBPath == "" || cfg.Store.DBPath == "omega.db" ||
		cfg.Store.DBPath == canonPath(datadir.DBPath(home, "omega.db")) {
		cfg.Store.DBPath = canonPath(filepath.Join(agentDataDir, "omega.db"))
	}
	if cfg.Kanban.DBPath == "" || cfg.Kanban.DBPath == "kanban.db" ||
		cfg.Kanban.DBPath == canonPath(datadir.DBPath(home, "kanban.db")) {
		// one kanban.db owns all boards.
		cfg.Kanban.DBPath = canonPath(datadir.DBPath(dir, "kanban.db"))
	}
	_ = os.MkdirAll(agentDataDir, 0o755)
	return nil
}

func migrateAgentStores(home string) error {
	mainDSN := datadir.DBPath(home, "omega.db")
	if _, err := os.Stat(mainDSN); err != nil {
		return nil // no store yet: nothing was ever written
	}
	entries, err := os.ReadDir(filepath.Join(home, "agents"))
	if err != nil {
		return nil // no agent homes: no taggable rows either
	}
	dsns := map[string]string{}
	for _, e := range entries {
		if !e.IsDir() || e.Name()[0] == '.' || e.Name()[0] == '_' {
			continue
		}
		if err := agent.ValidAgentName(e.Name()); err != nil {
			continue // malformed dir: never a session owner
		}
		dsns[e.Name()] = datadir.DBPath(filepath.Join(home, "agents", e.Name()), "omega.db")
	}
	return store.MigrateAgentRows(mainDSN, dsns)
}

// cmdAgents is the `omega agents` CLI: inventory and scaffold.
func cmdAgents(args []string) error {
	home := omegaHome()
	if err := agent.MigrateAgentHomes(home); err != nil {
		return err
	}
	if err := migrateAgentStores(home); err != nil {
		return err
	}
	if len(args) == 0 {
		return printAgents(home)
	}
	switch args[0] {
	case "create":
		if len(args) < 2 {
			return fmt.Errorf("usage: omega agents create <name> [description]")
		}
		name := args[1]
		description := strings.Join(args[2:], " ")
		if err := agent.ScaffoldAgent(home, name, description); err != nil {
			return err
		}
		fmt.Printf("agent %s scaffolded at %s\n", name, agent.AgentDir(home, name))
		fmt.Println("edit config.yaml to set its model; skills/, memory.md, user.md are its own")
		return nil
	case "--help", "-h", "help":
		fmt.Print("usage: omega agents [create <name> [description...]]\n\n  omega agents                 list registered agents\n  omega agents create <name>   scaffold a new agent directory\n")
		return nil
	default:
		return fmt.Errorf("unknown agents subcommand %q (want create)", args[0])
	}
}

// printAgents renders the CLI table.
func printAgents(home string) error {
	profiles := agent.LoadProfiles(home)
	fmt.Printf("%-14s %-28s %-22s %6s %6s  %s\n", "AGENT", "DESCRIPTION", "MODEL", "SKILLS", "MEMORY", "DIR")
	for _, p := range profiles {
		model := p.ModelName
		if model == "" && p.Name == agent.DefaultAgentName {
			model = "(home config)"
		}
		mem := fmt.Sprintf("%db", p.MemorySize)
		if p.MemorySize == 0 {
			mem = "-"
		}
		fmt.Printf("%-14s %-28s %-22s %6d %6s  %s\n", p.Name, p.Description, model,
			p.SkillCount, mem, p.Dir)
	}
	return nil
}
