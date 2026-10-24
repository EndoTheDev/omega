package main

import (
	"os"

	"github.com/EndoTheDev/omega/core"
)

// cwd returns the current working directory, or "." if it cannot be
// resolved.
func cwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}

// trustGate walks the trust seam's ResolveContext on behalf of the
// host entry paths. Nil-guarded by capsule law: an unmounted trust
// plugin answers "" (the gate simply does not exist) instead of panics
// at serve/run startup - proof: TestTrustRemovalProof (removal_test.go,
// eighth).
func trustGate(seam core.TrustProvider, dir string, flags trustFlags, interactive bool) string {
	if seam == nil {
		return ""
	}
	return seam.ResolveContext(dir, flags.approve, flags.noApprove, interactive)
}

// trustStatus is trustGate for the frontend status bar: the same
// nil-guard, an unmounted seam leaves the status line bare.
func trustStatus(seam core.TrustProvider, dir string, flags trustFlags) string {
	if seam == nil {
		return ""
	}
	return seam.State(dir, flags.approve, flags.noApprove)
}

// trustFlags holds the trust-related CLI flags. CLI-only: no YAML or
// env equivalent.
type trustFlags struct {
	approve   bool // --approve
	noApprove bool // --no-approve
}

// parseTrustArgs extracts --approve and --no-approve from args.
func parseTrustArgs(args []string) trustFlags {
	var f trustFlags
	for _, a := range args {
		switch a {
		case "--approve":
			f.approve = true
		case "--no-approve":
			f.noApprove = true
		}
	}
	return f
}

// stripTrustArgs removes --approve and --no-approve from args, so the
// remaining arguments are the run prompt.
func stripTrustArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--approve" || a == "--no-approve" {
			continue
		}
		out = append(out, a)
	}
	return out
}
