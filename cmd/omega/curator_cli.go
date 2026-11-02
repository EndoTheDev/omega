package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/EndoTheDev/omega/modules/skills"
)

// cmdCurator is the CLI face of the learning loop's weekly pass (card
// t_9972dfd0):
//
//	omega curator               one full pass (staleness + LLM proposals)
//	omega curator stale         the stale list
//	omega curator ledger        recent audit rows
//	omega curator rollback <id> single-edit revert of one ledger row
//
// The provider rides the config (provider.type/model_name/host);
// the consult model is skills.curator_model, empty = the provider's own.
// The pass itself lives in the skills module; this command routes the
// same surfaces the chat /curator command serves.
func cmdCurator(rest []string) error {
	configPath := parseConfigFlag(rest)
	cfg, err := LoadConfig(resolveConfigPath(configPath))
	if err != nil {
		return err
	}
	if err := resolveHomePaths(&cfg); err != nil {
		return err
	}
	argv := stripConfigFlag(rest)
	verb := ""
	var args []string
	if len(argv) > 0 {
		verb = argv[0]
		args = argv[1:]
	} else {
		verb = "run"
	}
	dir := cfg.Skills.Dir
	if dir == "" {
		dir = skills.Default().Dir
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("skills directory %s: %w", dir, err)
	}
	sp := skills.NewSkillsProvider(dir)
	sp.CuratorModel = cfg.Skills.CuratorModel
	out, runErr := sp.HandleCommand(context.Background(), "/curator", strings.TrimSpace(verb+" "+strings.Join(args, " ")))
	if out != "" {
		fmt.Println(out)
	}
	return runErr
}