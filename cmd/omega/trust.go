package main

import "os"

// cwd returns the current working directory, or "." if it cannot be
// resolved.
func cwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
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
