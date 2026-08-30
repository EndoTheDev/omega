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
