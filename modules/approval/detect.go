package approval

import (
	"sort"
	"strings"
)

// The would-interrupt detector (design Q1): a rule-based filter over
// the PARSED command. Only commands it flags reach the reviewer - in
// smart mode the decision model, in manual mode the human card.
// Benign tool traffic pays one pass, never a cloud call.
//
// The detector is deliberately over-eager, never under: a false ask
// costs a click (or, in smart mode, one cheap forward pass - the
// cache makes repeats free); a false pass costs whatever the command
// does. It decides nothing - it only decides WHO looks next.

// Risk is the detector's verdict for one command.
type Risk struct {
	// WouldInterrupt - true when a reviewer must see it.
	WouldInterrupt bool
	// Reasons names the rules that fired, stable and greppable; they
	// ride the audit row ("rules") and the reviewer prompt
	// ("flagged_as").
	Reasons []string
}

// wordSet is one rule's command vocabulary: basename form of words
// whose ABSENCE in a segment means nothing risky via this rule.
type wordSet map[string]bool

func set(words ...string) wordSet {
	m := make(wordSet, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// Plain vocabulary rules: any of these words anywhere in a segment's
// parsed words flags the command. Substitution spans ($( ... ),
// backticks) are opaque words - they surface via command-substitution,
// not via a member scan of their inner text.
var plainRules = []struct {
	name  string
	words wordSet
}{
	{"system-control", set(
		"systemctl", "service", "sv", "rc-service", "rc-update", "reboot",
		"shutdown", "poweroff", "halt", "timedatectl", "hostnamectl",
		"losetup", "mount", "umount", "swapon", "swapoff", "useradd",
		"userdel", "usermod", "groupadd", "groupdel", "visudo", "crontab",
		"modprobe", "insmod", "rmmod", "kexec", "ip6tables", "iptables",
		"nft", "ufw", "firewall-cmd")},
	{"process-control", set(
		"kill", "pkill", "killall", "renice", "taskset")},
	{"file-writes", set(
		"rm", "rmdir", "unlink", "shred", "srm", "mkfs", "mke2fs", "mkswap",
		"truncate", "dd", "mv", "cp", "rsync", "ln", "chmod", "chown",
		"chgrp", "chattr", "install", "patch")},
	{"network-egress", set(
		"curl", "wget", "fetch", "scp", "sftp", "ssh", "mosh", "nc", "ncat",
		"netcat", "socat", "telnet", "ftp", "gitsend")},
	{"privilege-escalation", set(
		"sudo", "su", "doas", "pkexec", "chroot", "setpriv")},
	{"dynamic-exec", set(
		"eval", "exec", "xargs", "nohup", "setsid")},
}

// Interpreters: run code a file or stdin carries - the one family
// where the CODE is not the command text (a reviewer cannot read what
// `bash script.sh` does), so any meaningful interpreter-invocation
// flags. Version/probe args (--version, -V, -h) stay invisible to the
// gate; code passed with -c/-e is visible but still runs unvetted -
// it flags too, and the reviewer sorts benign one-liners.
var interpreters = set(
	"bash", "sh", "zsh", "bash5", "dash", "ksh", "ash", "source", "node",
	"deno", "python", "python2", "python3", "pypy", "pypy3", "perl",
	"ruby", "php", "lua", "npx", "uvx", "bun", "javac")

// interpreterInerts are interpreter args that execute nothing: probing
// version/help only. A segment of JUST interpreter + inerts is no risk.
var interpreterInerts = set(
	"--version", "-V", "-v", "-h", "--help", "-help", "--usage", "-?")

// actionRules: (tool, action) pairs - install/remove-class invocations
// of the package tools, and the git/history-touching family. Matching
// walks forward from each tool word.
var actionRules = []struct {
	name string
	tool map[string]wordSet
}{
	{"package-managers", map[string]wordSet{
		"apt":      set("install", "remove", "purge", "upgrade", "full-upgrade", "dist-upgrade", "autoremove", "hold", "unhold", "download"),
		"apt-get":  set("install", "remove", "purge", "upgrade", "dist-upgrade", "autoremove", "download"),
		"aptitude": set("install", "remove", "purge", "upgrade"),
		"dnf":      set("install", "remove", "erase", "upgrade", "downgrade", "autoremove", "distro-sync"),
		"yum":      set("install", "remove", "erase", "upgrade", "downgrade"),
		"zypper":   set("install", "remove", "rm", "in", "dup", "up"),
		"pacman":   set("-S", "-R", "-Rs", "-Rns", "-U", "-Syu", "-Su", "-Syy"),
		"yay":      set("-S", "-R", "-U", "-Syu"),
		"paru":     set("-S", "-R", "-U", "-Syu"),
		"apk":      set("add", "del", "del", "upgrade"),
		"emerge":   set("install", "unmerge", "emerge", "resume", "sync"),
		"brew":     set("install", "uninstall", "upgrade", "tap", "untap", "link"),
		"snap":     set("install", "remove", "refresh", "revert"),
		"flatpak":  set("install", "uninstall", "update", "override"),
		"dpkg":     set("-i", "-r", "--install", "--remove", "-P"),
		"rpm":      set("-i", "-U", "-e", "-F"),
		"gem":      set("install", "uninstall", "update", "cleanup"),
		"cargo":    set("install", "uninstall"),
		"npm":      set("install", "i", "rm", "uninstall", "remove", "link", "add", "dedupe", "exec", "cache"),
		"pnpm":     set("install", "i", "add", "remove", "rm", "link", "dlx", "store"),
		"yarn":     set("add", "remove", "install", "link", "dlx"),
		"bun":      set("add", "remove", "install", "link", "x", "pm"),
		"pip":      set("install", "uninstall"),
		"pip3":     set("install", "uninstall"),
		"uv":       set("pip", "tool", "add", "remove"),
		"poetry":   set("add", "remove", "install", "update", "publish"),
		"go":       set("install"),
		"deno":     set("install", "uninstall", "vendor"),
		"docker":   set("load", "import", "commit", "system", "image", "login", "logout", "tag", "buildx", "context", "save", "push", "pull", "rm", "rmi", "prune", "kill", "restart", "stop", "pause", "unpause", "update", "cp", "rename", "down", "up", "scale"),
	}},
	{"git-history", map[string]wordSet{
		"git": set("push", "remote", "filter-branch", "filter-repo", "repack", "gc", "prune", "am", "apply", "cherry-pick", "revert", "send-email", "im", "bundle", "bisect", "replace", "notes", "submodule", "worktree", "maintenance", "config", "init", "clone", "fetch", "stash", "tag", "branch", "checkout", "restore", "reset", "clean", "mv", "rebase", "merge", "commit", "add", "rm", "archive", "upload-pack", "daemon"),
	}},
}

// gitHardDestruct: git subcommands only destructive with a flag
// (reset --hard, checkout --force, push --force) - matched separately
// so the plain (tool, action) scan can name the action, this scan the
// hardness.
var gitHardFlags = set("--hard", "--force", "-f", "--prune", "--amend", "--no-gpg-sign", "--delete")

// Assess runs the rule set over the parsed command. Substitution
// presence and an unparsable command are risks in themselves (Q7:
// command substitutions stay visible and flagged; a command whose
// static form cannot be certified is treated as would-interrupt).
func Assess(p Parsed) Risk {
	reasons := map[string]bool{}
	if p.ParseErr != nil {
		reasons["unparsable-command"] = true
	}
	for _, seg := range p.Segments {
		for _, r := range assessSegment(seg) {
			reasons[r] = true
		}
	}
	if len(reasons) == 0 {
		return Risk{}
	}
	out := make([]string, 0, len(reasons))
	for r := range reasons {
		out = append(out, r)
	}
	sort.Strings(out)
	return Risk{WouldInterrupt: true, Reasons: out}
}

// assessSegment is the per-segment rule pass.
func assessSegment(seg Segment) []string {
	var out []string
	if seg.Substitution {
		out = append(out, "command-substitution")
	}
	if len(seg.Words) == 0 {
		return out
	}
	fired := map[string]bool{}
	first := baseWord(seg.Words[0])

	// interpreter invocation (any interpreter + non-inert args)
	if interpreters[first] {
		inert := true
		for _, w := range seg.Words[1:] {
			if !interpreterInerts[baseWord(w)] {
				inert = false
				break
			}
		}
		if !inert || len(seg.Words) == 1 {
			// `python3` bare: runs a REPL reading stdin - code not
			// visible, flag; `python3 --version` never reaches here
			fired["script-execution"] = true
		}
	}
	// a script file named as a command word: ./deploy.sh, bin/setup.sh
	for _, w := range seg.Words {
		if hasScriptExtension(w) {
			fired["script-execution"] = true
			break
		}
	}
	for _, r := range plainRules {
		for _, w := range seg.Words {
			b := baseWord(w)
			if b == "" || r.words[b] {
				if b == "" {
					continue
				}
				fired[r.name] = true
				break
			}
		}
	}
	for _, r := range actionRules {
		if actionsHit(seg, r.tool) {
			fired[r.name] = true
		}
	}
	if gitHardFlagsHit(seg) {
		fired["git-history"] = true
	}
	if writesThrough(seg) {
		fired["redirect-writes"] = true
	}
	for r := range fired {
		out = append(out, r)
	}
	return out
}

// actionsHit walks forward from every tool word: one of its actions
// appears later in the segment.
func actionsHit(seg Segment, tool map[string]wordSet) bool {
	for i, w := range seg.Words {
		actions, ok := tool[baseWord(w)]
		if !ok {
			continue
		}
		for _, a := range seg.Words[i+1:] {
			b := baseWord(a)
			if b != "" && actions[b] {
				return true
			}
		}
	}
	return false
}

// toolsAreFlags removed: flag-form tools (pacman -Syu) are modeled by
// (tool, action) pairs directly - `pacman` followed by `-S` matches
// the action scan; no second table.

func gitHardFlagsHit(seg Segment) bool {
	for i, w := range seg.Words {
		if baseWord(w) != "git" {
			continue
		}
		for _, a := range seg.Words[i+1:] {
			b := baseWord(a)
			switch b {
			case "push", "reset", "clean", "checkout", "rebase", "filter-branch", "filter-repo":
				for _, f := range seg.Words[i+1:] {
					if gitHardFlags[baseWord(f)] {
						return true
					}
				}
			}
		}
	}
	return false
}

// writesThrough reports redirect writes in parsed form: a standalone
// > or >> word, a word starting with them (2>>x keeps its fd prefix),
// or tee (which writes whatever flows through it).
func writesThrough(seg Segment) bool {
	for _, w := range seg.Words {
		switch w {
		case ">", ">>", "<>", "tee":
			return true
		}
		for _, p := range []string{">>", ">", "&>", "&>>", "<>"} {
			if strings.HasPrefix(w, p) && len(w) > len(p) {
				return true
			}
		}
	}
	return false
}

// hasScriptExtension reports whether a word names a script by
// extension - the reviewer-visible static fact that the shell would
// interpret the file.
func hasScriptExtension(w string) bool {
	low := strings.ToLower(baseWord(w))
	if len(low) <= len(".sh") { // the bare extension and shorter
		return false
	}
	for _, ext := range []string{".sh", ".bash", ".zsh", ".ksh", ".py", ".pl", ".rb", ".php", ".lua", ".mjs", ".cjs", ".ts", ".fish"} {
		if strings.HasSuffix(low, ext) {
			return true
		}
	}
	return false
}

// baseWord strips a path prefix (basename) and trailing punctuation
// so `/usr/bin/rm` and `install -m` match their command words.
func baseWord(w string) string {
	if i := strings.LastIndexAny(w, "/"); i >= 0 {
		w = w[i+1:]
	}
	return strings.Trim(w, ",;")
}
