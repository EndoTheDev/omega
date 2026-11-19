package skills

// cli.go: the module's CLI face (v0.12 phase D Q6 - the capsule
// registers its own names at mount and owns its argv):
//
// omega curator the weekly skills curator pass
// omega skills the registry editor - list | read | write | delete
//
//	                tab's CRUD round)
//
// Every `omega skills` verb rides this capsule's own faces, never a
// second implementation: list renders the /skills command's table
// (handleSkillsCommand, the same renderer the loop's slash command
// prints), read calls ReadSkill (the seam behind GET /skills/content),
// write rides SaveSkill (the tab's one writer) and delete rides
// DeleteSkillAs (the same removal law DeleteSkill delegates to, with
// the ledger's actor naming `cli` instead of the tab). The CLI adds no
// second writer.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/EndoTheDev/omega/agent"
)

// curatorHelp documents the curator subcommand.
const curatorHelp = `omega curator - the weekly skills curator pass

Usage:
  omega curator                one full pass (same as "run")
  omega curator run            one full pass
  omega curator stale          list the stale skills
  omega curator ledger         list the curator ledger
  omega curator rollback <id>  roll back one ledger entry
`

// skillsSummary is the one-line entry the global `omega --help` module section shows.
const skillsSummary = "the skills registry: list, read, write, delete (see: omega skills --help)"

// skillsHelp is the module's own help: `omega skills --help` prints it.
const skillsHelp = `omega skills - the skills registry (SKILL.md folders)

Usage:
  omega skills                        list every skill (same as "omega skills list")
  omega skills list                   NAME / DESCRIPTION table (the /skills table)
  omega skills read <name>            print one skill's SKILL.md body
  omega skills write <name> [flags]   create a skill, or rewrite an existing one
  omega skills delete <name>          back the skill up, then remove it

Write flags:
  --description <line>  the one-line frontmatter description (required on
                        create; an update keeps the current one when omitted)
  --file <path>         read the markdown body from a file ("-" = stdin)
  --content <text>      the markdown body inline (exclusive with --file)
  With neither --file nor --content the body is read from stdin.

Writes ride the same provider faces the webui skills tab calls: an
editor save and an "omega skills write" compose the identical
SKILL.md, and a delete backs the folder up into .curator/backups with
a ledger row before removing it. A name present in both the shared
and the agent's own directory resolves to the shared copy - the same
winner the tab edits.
`

// registerCuratorCLI claims the `omega curator` subcommand.
func (p *Plugin) registerCuratorCLI(api *agent.ExtensionAPI, sp *SkillsProvider) error {
	return api.RegisterSubcommand("curator", "run the weekly skills curator pass (verbs: run | stale | ledger | rollback <id>)",
		func(argv []string) error {
			if len(argv) > 0 && (argv[0] == "--help" || argv[0] == "-h") {
				fmt.Print(curatorHelp)
				return nil
			}
			if _, err := resolveSkillsDirs(sp); err != nil {
				return fmt.Errorf("curator: %w", err)
			}
			if _, err := os.Stat(sp.Dir); err != nil {
				return fmt.Errorf("curator: skills directory %s: %w", sp.Dir, err)
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

// registerSkillsCLI claims the `omega skills` subcommand.
func (p *Plugin) registerSkillsCLI(api *agent.ExtensionAPI, sp *SkillsProvider) error {
	return api.RegisterSubcommand("skills", skillsSummary, func(argv []string) error {
		if _, err := resolveSkillsDirs(sp); err != nil {
			return err
		}
		return runSkillsCLI(sp, stripCLIConfigFlag(argv), os.Stdout, os.Stdin)
	})
}

// resolveSkillsDirs re-roots the provider's home-relative directories against. The CLI
// dispatch mounts on configForCLI, which never runs the host-side resolveHomePaths, so
// the configured dirs keep their bare "skills" / "skills/shared" literals there; a
// relative dir in the serve path has already been resolved.
func resolveSkillsDirs(sp *SkillsProvider) (string, error) {
	needsHome := !filepath.IsAbs(sp.Dir) || (sp.SharedDir != "" && !filepath.IsAbs(sp.SharedDir))
	if !needsHome {
		return sp.Dir, nil
	}
	home := os.Getenv("OMEGA_HOME")
	if home == "" {
		return "", fmt.Errorf("skills directory %q is home-relative and OMEGA_HOME is unset", sp.Dir)
	}
	if !filepath.IsAbs(sp.Dir) {
		sp.Dir = filepath.Join(home, sp.Dir)
	}
	if sp.SharedDir != "" && !filepath.IsAbs(sp.SharedDir) {
		sp.SharedDir = filepath.Join(home, sp.SharedDir)
	}
	return sp.Dir, nil
}

// stripCLIConfigFlag drops the global --config flag pair.
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

// runSkillsCLI is the argv dispatch (name-first grammar, the crons CLI's shape).
func runSkillsCLI(sp *SkillsProvider, args []string, stdout io.Writer, stdin io.Reader) error {
	if len(args) == 0 {
		return skillsList(sp, stdout)
	}
	sub, rest := args[0], args[1:]
	if isHelpToken(sub) {
		if len(rest) != 0 {
			return fmt.Errorf("skills --help: unexpected argument %q (help takes none)", rest[0])
		}
		fmt.Fprint(stdout, skillsHelp)
		return nil
	}
	if len(rest) > 0 && isHelpToken(rest[0]) {
		if len(rest) > 1 {
			return fmt.Errorf("skills %s --help: unexpected argument %q (help takes none)", sub, rest[1])
		}
		fmt.Fprint(stdout, skillsHelp)
		return nil
	}
	switch sub {
	case "list":
		if len(rest) != 0 {
			return fmt.Errorf("skills list: unexpected argument %q (list takes none, omega skills --help lists the verbs)", rest[0])
		}
		return skillsList(sp, stdout)
	case "read":
		return skillsRead(sp, stdout, rest)
	case "write":
		return skillsWrite(sp, stdout, stdin, rest)
	case "delete":
		return skillsDelete(sp, stdout, rest)
	default:
		fmt.Fprint(os.Stderr, skillsHelp)
		return fmt.Errorf("unknown skills subcommand %q", sub)
	}
}

// isHelpToken reports the three spellings of the module help.
func isHelpToken(arg string) bool {
	return arg == "--help" || arg == "-h" || arg == "help"
}

// skillName peels the one positional a skill verb needs. A leading dash is a usage
// error, never a guess.
func skillName(verb string, args []string) (string, []string, error) {
	withFlags := ""
	if verb == "write" {
		withFlags = " [flags]"
	}
	if len(args) == 0 {
		return "", nil, fmt.Errorf("usage: omega skills %s <name>%s (omega skills --help lists the verbs)", verb, withFlags)
	}
	if strings.HasPrefix(args[0], "-") {
		return "", nil, fmt.Errorf("usage: omega skills %s <name>%s - the skill name comes first", verb, withFlags)
	}
	if !validSkillName(args[0]) {
		return "", nil, fmt.Errorf("omega skills %s: name must be a single folder component (no separators, no leading dot), got %q", verb, args[0])
	}
	return args[0], args[1:], nil
}

// skillsList renders the inventory with the /skills command's own renderer.
func skillsList(sp *SkillsProvider, stdout io.Writer) error {
	out, err := sp.handleSkillsCommand()
	if err != nil {
		return err
	}
	fmt.Fprint(stdout, out)
	if out != "" && !strings.HasSuffix(out, "\n") {
		fmt.Fprintln(stdout)
	}
	return nil
}

// skillsRead prints one skill's markdown body - the viewer tab's read
// (the body under the frontmatter fence, never the frontmatter).
func skillsRead(sp *SkillsProvider, stdout io.Writer, args []string) error {
	name, rest, err := skillName("read", args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("skills read: unexpected argument %q (only the name is positional)", rest[0])
	}
	s, err := sp.ReadSkill(name)
	if err != nil {
		return err
	}
	fmt.Fprint(stdout, s.Content)
	if s.Content != "" && !strings.HasSuffix(s.Content, "\n") {
		fmt.Fprintln(stdout)
	}
	return nil
}

// skillsWriteFlags is the parsed flag set of `omega skills write`.
type skillsWriteFlags struct {
	description string
	file        string
	content     string
}

// parseSkillsWriteArgs parses the write flags. Unknown flags and bad values error - the
// operator gets the flag name, never a silent default.
func parseSkillsWriteArgs(args []string) (*skillsWriteFlags, []string, error) {
	f := &skillsWriteFlags{}
	fs := flag.NewFlagSet("skills write", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&f.description, "description", "", "one-line frontmatter description")
	fs.StringVar(&f.file, "file", "", "markdown body file (- = stdin)")
	fs.StringVar(&f.content, "content", "", "markdown body inline")
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	return f, fs.Args(), nil
}

// skillsWrite is the upsert: a new name creates the skill folder, an existing name
// rewrites the body and (when --description is passed) the description line, leaving
// every other frontmatter key byte-true.
func skillsWrite(sp *SkillsProvider, stdout io.Writer, stdin io.Reader, args []string) error {
	name, flagArgs, err := skillName("write", args)
	if err != nil {
		return err
	}
	f, pos, err := parseSkillsWriteArgs(flagArgs)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fmt.Errorf("skills write: unexpected argument %q (only the name is positional)", pos[0])
	}
	verb := "created"
	description := strings.TrimSpace(f.description)
	if existing, rerr := sp.ReadSkill(name); rerr == nil {
		verb = "updated"
		if description == "" {
			description = existing.Description
		}
	}
	if description == "" {
		return fmt.Errorf("skills write %s: --description is required (the one-line frontmatter description)", name)
	}
	body, err := skillBody(f, stdin)
	if err != nil {
		return fmt.Errorf("skills write %s: %w", name, err)
	}
	list, err := sp.SaveSkill(name, description, body)
	if err != nil {
		return fmt.Errorf("skills write %s: %w", name, err)
	}
	fmt.Fprintf(stdout, "saved skill %q (%s): %d skill(s)\n", name, verb, len(list))
	return nil
}

// skillBody resolves the markdown body from --content, --file or stdin.
func skillBody(f *skillsWriteFlags, stdin io.Reader) (string, error) {
	if f.content != "" && f.file != "" {
		return "", fmt.Errorf("--content and --file are mutually exclusive")
	}
	if f.content != "" {
		return f.content, nil
	}
	if f.file != "" && f.file != "-" {
		raw, err := os.ReadFile(f.file)
		if err != nil {
			return "", fmt.Errorf("read body file: %w", err)
		}
		return string(raw), nil
	}
	raw, err := io.ReadAll(stdin)
	if err != nil {
		return "", fmt.Errorf("read body from stdin: %w", err)
	}
	return string(raw), nil
}

// skillsDelete removes one skill through the tab's own removal law.
func skillsDelete(sp *SkillsProvider, stdout io.Writer, args []string) error {
	name, rest, err := skillName("delete", args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("skills delete: unexpected argument %q (only the name is positional)", rest[0])
	}
	list, err := sp.DeleteSkillAs(name, ActorCLI, "removed from the CLI")
	if err != nil {
		return fmt.Errorf("skills delete %s: %w", name, err)
	}
	fmt.Fprintf(stdout, "deleted skill %q; %d skill(s) remain\n", name, len(list))
	return nil
}
