package approval

import (
	"sort"
	"strings"
)

// The would-interrupt detector (design Q1). Benign tool traffic pays one pass, never a
// cloud call. The detector is deliberately over-eager, never under: a false ask costs a
// click (or, in smart mode, one cheap forward pass - the cache makes repeats free); a
// false pass costs whatever the command does.

// Risk is the detector's verdict for one command.
type Risk struct {
	// WouldInterrupt - true when a reviewer must see it.
	WouldInterrupt bool
	Reasons []string
}

// wordSet is one rule's command vocabulary.
type wordSet map[string]bool

func set(words ...string) wordSet {
	m := make(wordSet, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// Plain vocabulary rules: any of these words anywhere in a segment's parsed words.
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
		"scp", "sftp", "ssh", "mosh", "nc", "ncat",
		"netcat", "socat", "telnet", "ftp", "gitsend")},
	{
		name: "secret-material", words: wordSet{}, // name/arg matching, see secretMaterialHit
	},
	{"privilege-escalation", set(
		"sudo", "su", "doas", "pkexec", "chroot", "setpriv")},
	{"dynamic-exec", set(
		"eval", "exec", "xargs", "nohup", "setsid")},
}

// Interpreters: run code a file or stdin carries - the one family where the CODE is not
// the command text (a reviewer cannot read what `bash script.sh` does), so any
// meaningful interpreter-invocation flags.
var interpreters = set(
	"bash", "sh", "zsh", "bash5", "dash", "ksh", "ash", "source", "node",
	"deno", "python", "python2", "python3", "pypy", "pypy3", "perl",
	"ruby", "php", "lua", "npx", "uvx", "bun", "javac")

// interpreterInerts are interpreter args that execute nothing.
var interpreterInerts = set(
	"--version", "-V", "-v", "-h", "--help", "-help", "--usage", "-?")

// actionRules: (tool, action) pairs.
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

// gitHardDestruct: git subcommands only destructive with a flag (reset --hard, checkout
// --force, push --force).
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

	// interpreter invocation (any interpreter, any position): flag when the segment would
	// run code the reviewer cannot certify - a bare interpreter (stdin REPL), a
	// code-carrying flag (-c/-e/--eval), or an unvetted arg.
	if segHitsInterpreter(seg) {
		invoked, work := false, 0
		for _, w := range seg.Words {
			b := baseWord(w)
			if interpreters[b] {
				invoked = true
				continue
			}
			switch {
			case interpreterInerts[b], runnerFlags[b], b == "":
			case strings.HasPrefix(b, "<<"), strings.HasPrefix(b, ">"):
				// heredoc markers and redirect wrappers are not interpreter work.
			case interpreterCodeFlags[b]:
				work++
			case strings.Contains(w, ".test."):
			case hasScriptExtension(w):
				work++
			case strings.HasPrefix(b, "-"):
			default:
				work++
			}
		}
		if invoked && (work > 0 || len(seg.Words) == 1) {
			fired["script-execution"] = true
		}
	}
	// a script file named as a command word:./deploy.sh, bin/setup.sh (*.test.* files stay
	// inert.
	for _, w := range seg.Words {
		if hasScriptExtension(w) && !strings.Contains(w, ".test.") {
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
	if dataEgressHit(seg) {
		fired["network-egress"] = true
	}
	if secretMaterialHit(seg) {
		fired["secret-material"] = true
	}
	for r := range fired {
		out = append(out, r)
	}
	return out
}

// actionsHit walks forward from every tool word.
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

// toolsAreFlags removed: flag-form tools (pacman -Syu) are modeled by (tool, action)
// pairs directly.

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

// segHitsInterpreter reports whether an interpreter word rides the segment (any
// position.
func segHitsInterpreter(seg Segment) bool {
	for _, w := range seg.Words {
		if interpreters[baseWord(w)] {
			return true
		}
	}
	return false
}

// runnerFlags: interpreter arguments that run a project's OWN declared surface (node
// --test on the repo's *.test.* files) - routine dev work, never an ask.
var runnerFlags = set("--test", "--check", "--compile-only", "lint")

// interpreterCodeFlags: flags whose value IS code (visible, but it still executes
// unvetted.
var interpreterCodeFlags = set("-c", "-e", "-x", "--eval", "-X")

// dataEgressHit: curl/wget/fetch that SENDS data (POST/PUT/form/
// upload) flags; a bare GET/HEAD fetch reads a remote and writes
// nothing - the corpus's real traffic includes exactly those.
func dataEgressHit(seg Segment) bool {
	dataFlags := set("-d", "--data", "--data-binary", "--data-raw",
		"--data-urlencode", "-F", "--form", "--form-string", "-T",
		"--upload-file", "--upload", "-P", "--publish")
	for i := range seg.Words {
		b := baseWord(seg.Words[i])
		if b != "curl" && b != "wget" && b != "fetch" && b != "httpie" {
			continue
		}
		for j := i + 1; j < len(seg.Words); j++ {
			ab := baseWord(seg.Words[j])
			if dataFlags[ab] {
				return true
			}
			if (ab == "-X" || ab == "--request") && j+1 < len(seg.Words) {
				next := strings.ToUpper(baseWord(seg.Words[j+1]))
				if next == "POST" || next == "PUT" || next == "PATCH" || next == "DELETE" {
					return true
				}
				j++
				continue
			}
			// XPOST / --request=POST fused forms
			if strings.HasPrefix(ab, "-X") {
				tail := strings.TrimPrefix(ab, "-X")
				tail = strings.TrimPrefix(tail, "--request=")
				if tail == "POST" || tail == "PUT" || tail == "PATCH" || tail == "DELETE" {
					return true
				}
			}
		}
	}
	return false
}

// secretMaterialHit: credential-material paths and names ride the would-interrupt set.
var secretVocabulary = []string{
	".ssh", "id_rsa", "id_ed25519", "id_ecdsa", ".aws/credentials",
	".netrc", ".git-credentials", ".env", "auth.json", "keyring",
	".gnupg", "private_key", "secret", "password", "shadow", ".npmrc",
	".docker/config.json", "known_hosts",
}

func secretMaterialHit(seg Segment) bool {
	for _, w := range seg.Words {
		low := strings.ToLower(w)
		for _, vocab := range secretVocabulary {
			if strings.Contains(low, vocab) {
				return true
			}
		}
	}
	return false
}

// writesThrough reports redirect writes in parsed form.
func writesThrough(seg Segment) bool {
	for _, w := range seg.Words {
		if w == "tee" {
			return true
		}
		body := strings.TrimLeft(w, "0123456789") // fd prefix
		switch {
		case body == ">" || body == ">>":
			// the operator stands alone; its file operand rides as the next word (spaced form).
			return true
		case strings.HasPrefix(body, ">&"):
			if isFDOperand(body[2:]) {
				continue // 2>&1: descriptor duplication, no file
			}
			// >&f, >&f.log, or >& still waiting on the next word
			return true
		case strings.HasPrefix(body, "&>"),
			strings.HasPrefix(body, "<>"), strings.HasPrefix(body, ">"),
			strings.HasPrefix(body, "&"):
			return true
		}
	}
	return false
}

// isFDOperand reports an all-digit fd operand (>&1, 2>&12).
func isFDOperand(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// hasScriptExtension reports whether a word names a script by extension.
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

// baseWord strips a path prefix (basename) and trailing punctuation so `/usr/bin/rm`.
func baseWord(w string) string {
	if i := strings.LastIndexAny(w, "/"); i >= 0 {
		w = w[i+1:]
	}
	return strings.Trim(w, ",;")
}
