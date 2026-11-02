package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/EndoTheDev/omega/engine/agent"
)

// registerCLI claims the `omega curator` subcommand - the CLI face of
// the learning loop (card t_9972dfd0). The argv mirrors the /curator
// command: no verb or "run" = one full pass, plus stale | ledger |
// rollback <id>. The CLI dispatch mounts on configForCLI, whose
// home-relative skills dir is NOT pre-resolved (the serve path's
// resolveHomePaths is host-side), so a relative dir re-roots against
// OMEGA_HOME here.
func (p *Plugin) registerCLI(api *agent.ExtensionAPI, sp *SkillsProvider) error {
	return api.RegisterSubcommand("curator", "run the weekly skills curator pass (verbs: run | stale | ledger | rollback <id>)",
		func(argv []string) error {
			if !filepath.IsAbs(sp.Dir) {
				home := os.Getenv("OMEGA_HOME")
				if home == "" {
					return fmt.Errorf("curator: skills directory %q is home-relative and OMEGA_HOME is unset", sp.Dir)
				}
				sp.Dir = filepath.Join(home, sp.Dir)
			}
			if _, err := os.Stat(sp.Dir); err != nil {
				return fmt.Errorf("skills directory %s: %w", sp.Dir, err)
			}
			args := stripCLIConfigFlag(argv)
			verb := "run"
			if len(args) > 0 {
				verb = args[0]
				args = args[1:]
			}
			out, err := sp.HandleCommand(context.Background(), "/curator", strings.TrimSpace(verb+" "+strings.Join(args, " ")))
			if out != "" {
				fmt.Println(out)
			}
			return err
		})
}

// stripCLIConfigFlag drops the global --config flag pair: the CLI
// dispatch already consumed it for the mount (configForCLI).
func stripCLIConfigFlag(argv []string) []string {
	out := make([]string, 0, len(argv))
	for i := 0; i < len(argv); i++ {
		if argv[i] == "--config" && i+1 < len(argv) {
			i++ // skip the value
			continue
		}
		if strings.HasPrefix(argv[i], "--config=") {
			continue
		}
		out = append(out, argv[i])
	}
	return out
}