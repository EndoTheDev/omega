package main

// cmd/omega/agents.go: multi-agent profile wiring (card t_7e2b7002).
// An agent directory (~/agents-home/agents/<name>/config.yaml) LAYERS
// over the home config: only what it sets overrides. The resolved
// agent then gets its own skills, memory files, and log file - a
// delegated child's memory write and log lines never land in the
// default agent's files.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"gopkg.in/yaml.v3"
)

// resolveAgentName returns the OMEGA_AGENT target of this process:
// the registered agent profile a delegated child runs as. Empty
// everywhere outside agent-targeted child runs (the delegate tool and
// scaffolded child processes are the only writers).
func resolveAgentName() string {
	return strings.TrimSpace(os.Getenv("OMEGA_AGENT"))
}

// ApplyAgentLayer overlays home/agents/<name>/config.yaml onto cfg and
// resolves agent-scoped paths for everything that is not shared
// infrastructure. The provider/model/system_prompt knobs come from the
// layer; skills, memory, and logging are re-pointed at the agent's own
// directories so two agents never write each other's files. A missing
// layer is the caller's problem (profiles are validated before they
// spawn children) - a malformed layer is a hard error: run half of one
// agent's config is worse than a loud stop.
//
// resolveHomePaths (or its caller) must have already run on cfg: this
// function overwrites the home-relative defaults with agent ones.
func ApplyAgentLayer(cfg *Config, home, name string) error {
	// Validate the profile exists up front: an unknown agent never
	// half-runs on the home config.
	if _, err := agent.LoadProfile(home, name); err != nil {
		return err
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
	// Agent-scoped paths. The db stays shared (one omega.db: sessions
	// bind by row, not by process); everything the agent WRITES is
	// scoped to its directory. A path the layer set explicitly wins -
	// re-read the layer's path knobs so the defaults only apply to
	// what the layer left unset.
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
		cfg.Logging.File = filepath.Join(dir, "logs", "omega.log")
	}
	_ = os.MkdirAll(filepath.Join(dir, "logs"), 0o755)
	return nil
}

// cmdAgents is the `omega agents` CLI: inventory and scaffold.
//
//	omega agents            table of registered agents
//	omega agents create <name> [description...]   scaffold one
func cmdAgents(args []string) error {
	home := omegaHome()
	if err := agent.MigrateDefaultAgent(home); err != nil {
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