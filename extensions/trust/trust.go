package trust

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// TrustEntry is one trusted directory in the trust store. Level is
// "exact" (trust this directory only) or "parent" (trust this directory
// and everything under it).
type TrustEntry struct {
	Path  string `yaml:"path"`
	Level string `yaml:"level"`
}

// trustStore is the on-disk shape of trust.yaml.
type trustStore struct {
	Trusted []TrustEntry `yaml:"trusted"`
}

// Provider implements agent.TrustProvider. It reads and writes
// trust.yaml in the configured home directory.
type Provider struct {
	home string
}

// NewProvider creates a TrustProvider with the given home directory.
func NewProvider(home string) *Provider {
	return &Provider{home: home}
}

// trustPath returns the path to trust.yaml in omega home.
func (p *Provider) trustPath() string {
	return p.home + "/trust.yaml"
}

// loadTrusted reads the trust store. A missing or unreadable store is
// not an error: it returns an empty slice.
func (p *Provider) loadTrusted() []TrustEntry {
	data, err := os.ReadFile(p.trustPath())
	if err != nil {
		return nil
	}
	var store trustStore
	if err := yaml.Unmarshal(data, &store); err != nil {
		return nil
	}
	return store.Trusted
}

// saveTrusted writes the trust store, creating the home dir if needed.
func (p *Provider) saveTrusted(entries []TrustEntry) error {
	data, err := yaml.Marshal(trustStore{Trusted: entries})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p.home, 0755); err != nil {
		return err
	}
	return os.WriteFile(p.trustPath(), data, 0600)
}

// isTrusted reports whether dir is covered by a trust entry. An "exact"
// entry matches only dir itself; a "parent" entry matches dir and any
// path under it.
func isTrusted(entries []TrustEntry, dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		ep, err := filepath.Abs(e.Path)
		if err != nil {
			continue
		}
		if e.Level == "parent" {
			if abs == ep || strings.HasPrefix(abs, ep+string(filepath.Separator)) {
				return true
			}
		} else if abs == ep {
			return true
		}
	}
	return false
}

// State reports the trust status of cwd for the status bar:
// "trusted" (context loaded), "untrusted" (context skipped), or ""
// (no AGENTS.md anywhere). It has no side effects and never prompts;
// the actual load/persist happens in ResolveContext.
func (p *Provider) State(cwd string, approve, noApprove bool) string {
	root := ProjectRoot(cwd)
	if root == "" {
		return ""
	}
	if noApprove {
		return "untrusted"
	}
	if approve {
		return "trusted"
	}
	if isTrusted(p.loadTrusted(), root) {
		return "trusted"
	}
	return "untrusted"
}

// ResolveContext returns the project context string for cwd,
// applying the trust gate. approve/noApprove are the CLI flags;
// interactive enables the stdin trust prompt (TUI). Non-interactive
// callers skip untrusted context with a warning. --no-approve wins
// over --approve.
func (p *Provider) ResolveContext(cwd string, approve, noApprove, interactive bool) string {
	root := ProjectRoot(cwd)
	if root == "" {
		// No AGENTS.md anywhere up the tree: nothing to load or gate.
		return ""
	}

	entries := p.loadTrusted()

	if noApprove {
		return ""
	}
	if approve {
		entries = append(entries, TrustEntry{Path: root, Level: "exact"})
		if err := p.saveTrusted(entries); err != nil {
			fmt.Fprintf(os.Stderr, "omega: save trust store: %v\n", err)
		}
		return LoadProjectContext(cwd)
	}
	if isTrusted(entries, root) {
		return LoadProjectContext(cwd)
	}

	// Untrusted, no flag.
	if interactive {
		if promptTrust(root) {
			entries = append(entries, TrustEntry{Path: root, Level: "exact"})
			if err := p.saveTrusted(entries); err != nil {
				fmt.Fprintf(os.Stderr, "omega: save trust store: %v\n", err)
			}
			return LoadProjectContext(cwd)
		}
		return ""
	}

	fmt.Fprintf(os.Stderr, "omega: untrusted project %s; AGENTS.md context skipped (use --approve to trust)\n", root)
	return ""
}

// promptTrust asks the user whether to trust dir, reading a single y/n
// answer from stdin. Empty input defaults to no.
func promptTrust(dir string) bool {
	fmt.Printf("Trust files in %s? [y/N] ", dir)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}
