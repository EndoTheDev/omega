package main

// cmd/omega/agents.go: continuous-agent home wiring (card t_72ecca67,
// settled design; superseding card t_7e2b7002's layer shape).
// An agent home (agents/<name>/config/config.yaml) LAYERS over the
// home config: only what it sets overrides. The resolved agent then
// gets its own skills, memory files, log file, and data dir - a wake's
// writes never land in another agent's files (full data isolation).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/modules/datadir"
	kb "github.com/EndoTheDev/omega/modules/kanban"
	"github.com/EndoTheDev/omega/modules/store"
	"gopkg.in/yaml.v3"
)

// resolveAgentName returns the OMEGA_AGENT target of this process:
// the registered agent profile a delegated child runs as. Empty
// everywhere outside agent-targeted child runs (the delegate tool and
// scaffolded child processes are the only writers).
func resolveAgentName() string {
	return strings.TrimSpace(os.Getenv("OMEGA_AGENT"))
}

// ApplyAgentLayer overlays agents/<name>/config/config.yaml onto cfg
// and resolves agent-scoped paths for everything that is not shared
// infrastructure. The main agent (default) layers nothing: the home
// config IS its config and its persona files live at the home root
// (ruling 3). A continuous agent gets its own skills, memory/user
// files, log file, and data directory so one wake never writes into
// another agent's home. A missing layer is the caller's problem
// (profiles are validated before they spawn children) - a malformed
// layer is a hard error: run half of one agent's config is worse than
// a loud stop.
//
// resolveHomePaths (or its caller) must have already run on cfg: this
// function overwrites the home-relative defaults with agent ones.
func ApplyAgentLayer(cfg *Config, home, name string) error {
	// Validate the profile exists up front: an unknown agent never
	// half-runs on the home config.
	if _, err := agent.LoadProfile(home, name); err != nil {
		return err
	}
	if name == agent.DefaultAgentName {
		return nil // main = the home root itself (ruling 1)
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
	// Agent-scoped paths. Sessions, notifications, and boards resolve
	// through the agent's own data/ (datadir.DataHome honors
	// OMEGA_AGENT); everything the agent WRITES personal is scoped to
	// its directory. A path the layer set explicitly wins - re-read
	// the layer's path knobs so the defaults only apply to what the
	// layer left unset.
	var files struct {
		Skills  SkillsConfig  `yaml:"skills"`
		Memory  MemoryConfig  `yaml:"memory"`
		Logging LoggingConfig `yaml:"logging"`
	}
	if err := yaml.Unmarshal(data, &files); err != nil {
		return fmt.Errorf("parse agent config %s: %w", name, err)
	}
	dir := agent.AgentDir(home, name)
	if files.Skills.Dir == "" {
		cfg.Skills.Dir = filepath.Join(dir, "skills")
	}
	if files.Skills.SharedDir == "" {
		cfg.Skills.SharedDir = filepath.Join(home, "skills", "shared")
	}
	if files.Memory.File == "" {
		cfg.Memory.File = filepath.Join(dir, "memory.md")
	}
	if files.Memory.UserProfileFile == "" {
		cfg.Memory.UserProfileFile = filepath.Join(dir, "user.md")
	}
	if files.Logging.File == "" {
		cfg.Logging.File = filepath.Join(dir, name+".log")
	}
	// Data isolation (ruling 4): the agent's sessions store and boards
	// resolve under its own data/, not the main home's. resolveHomePaths
	// already pinned the main-resolved dsn values in; re-point those
	// defaults. A path the layer set explicitly wins - it is the agent's
	// own knob, not cross-home traffic.
	agentDataDir := filepath.Join(dir, "data")
	if cfg.Store.DBPath == "" || cfg.Store.DBPath == "omega.db" ||
		cfg.Store.DBPath == datadir.DBPath(home, "omega.db") {
		cfg.Store.DBPath = filepath.Join(agentDataDir, "omega.db")
	}
	if mainBoard, err := kb.PointerDSN(home); err == nil {
		if cfg.Kanban.DBPath == "" || cfg.Kanban.DBPath == "kanban.db" || cfg.Kanban.DBPath == mainBoard {
			agentBoard, aerr := kb.PointerDSN(dir)
			if aerr != nil {
				return fmt.Errorf("agent %s board registry: %w", name, aerr)
			}
			cfg.Kanban.DBPath = agentBoard
		}
	}
	_ = os.MkdirAll(agentDataDir, 0o755)
	return nil
}

// migrateAgentStores runs the one-time ruling-4 row migration (card
// t_72ecca67): agent-tagged sessions left in the main store under the
// retired shared-db shape move into their agent homes' own stores. No
// main store and no agents/ directory both mean nothing was ever
// written - the migration is skipped without creating either. Boot
// order: after MigrateAgentHomes (persona up first, no reader races
// the move), before any store opens.
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
//
//	omega agents            table of registered agents
//	omega agents create <name> [description...]   scaffold one
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

// printAgents renders the CLI table (same data as the webui /agents
// list and the TUI /agents command - one scan, three renderers).
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
