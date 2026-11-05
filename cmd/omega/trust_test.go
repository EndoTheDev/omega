package main

import "testing"

// setTestHome points omegaHome at a temp dir for the duration of a test.
func setTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("OMEGA_HOME", t.TempDir())
}

func TestParseTrustArgs(t *testing.T) {
	tests := []struct {
		args []string
		want trustFlags
	}{
		{[]string{"hello"}, trustFlags{}},
		{[]string{"--approve"}, trustFlags{approve: true}},
		{[]string{"--no-approve"}, trustFlags{noApprove: true}},
		{[]string{"--approve", "--no-approve"}, trustFlags{approve: true, noApprove: true}},
	}
	for _, tt := range tests {
		got := parseTrustArgs(tt.args)
		if got != tt.want {
			t.Errorf("parseTrustArgs(%v) = %+v, want %+v", tt.args, got, tt.want)
		}
	}
}

func TestStripTrustArgs(t *testing.T) {
	got := stripTrustArgs([]string{"--approve", "what", "--no-approve", "is", "up"})
	want := []string{"what", "is", "up"}
	if len(got) != len(want) {
		t.Fatalf("stripTrustArgs = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestSplitSubcommandBareFlags is the card t_18b13390 pin: the bare
// REPL entry must parse trust flags identically to subcommand paths. It
// used to return an empty rest, so `omega --approve` reached the loop
// with approve=false and the documented flag was dead (warning printed,
// no trust.yaml written).
func TestSplitSubcommandBareFlags(t *testing.T) {
	sub, rest := splitSubcommand([]string{"--approve"})
	if sub != "" {
		t.Fatalf("sub = %q, want empty (bare entry)", sub)
	}
	if got := parseTrustArgs(rest); !got.approve {
		t.Fatalf("parseTrustArgs(rest) = %+v, want approve=true (bare entry must honor --approve)", got)
	}
	sub, rest = splitSubcommand([]string{"--no-approve", "--approve"})
	if sub != "" {
		t.Fatalf("sub = %q, want empty (bare entry)", sub)
	}
	got := parseTrustArgs(rest)
	if !got.approve || !got.noApprove {
		t.Fatalf("parseTrustArgs(rest) = %+v, want both flags parsed (ResolveContext owns precedence)", got)
	}
}

// TestSplitSubcommandLeadingFlagsBeforePath: trust flags also survive
// when they sit before a project path - previously anything before the
// first non-flag argument was dropped from rest.
func TestSplitSubcommandLeadingFlagsBeforePath(t *testing.T) {
	sub, rest := splitSubcommand([]string{"--approve", "D:/proj"})
	if sub != "D:/proj" {
		t.Fatalf("sub = %q, want D:/proj", sub)
	}
	if got := parseTrustArgs(rest); !got.approve {
		t.Fatalf("parseTrustArgs(rest) = %+v, want approve=true", got)
	}
}

// TestSplitSubcommandTrailingFlagsBeforeSub: flags after the
// subcommand keep parsing through the run prompt unchanged.
func TestSplitSubcommandTrailingFlagsBeforeSub(t *testing.T) {
	sub, rest := splitSubcommand([]string{"run", "prompt", "--approve"})
	if sub != "run" {
		t.Fatalf("sub = %q, want run", sub)
	}
	if got := parseTrustArgs(rest); !got.approve {
		t.Fatalf("parseTrustArgs(rest) = %+v, want approve=true", got)
	}
	found := false
	for _, a := range rest {
		if a == "prompt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("rest lost the run prompt: %v", rest)
	}
}
