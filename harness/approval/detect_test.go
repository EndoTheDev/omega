package approval

import "testing"

// TestFDRedirectWritesPins pin the trust boundary (card t_2919b7a3):
// every fd-prefixed redirect that targets a FILE writes - in the
// spaced and in the unspaced shape - while the descriptor-dup forms
// (2>&1, >&2) write no file and must read clean.
func TestFDRedirectWritesPins(t *testing.T) {
	cases := []struct {
		in  string
		want bool // writesThrough verdict (via assess -> redirect-writes)
	}{
		{in: "echo hi 2> f.txt", want: true},
		{in: "echo hi 2>> f.txt", want: true},
		{in: "echo hi &> f.txt", want: true},
		{in: "echo hi &>> all.log", want: true},
		{in: "cmd 2>err.log >out.log", want: true},
		{in: "cmd 2>err.log", want: true},
		{in: "cmd 2>>err.log", want: true},
		{in: "cmd 1>out.log", want: true},
		{in: "cmd 21>err.log", want: true},
		{in: "cmd >& f.log", want: true},
		{in: "cmd 2>&log.txt", want: true},
		{in: "cmd 2>&", want: true}, // operand rides the next word
		{in: "cmd 2<>log", want: true},
		// writesThrough feeds the assessment; echo stays benign
		{in: "echo hi", want: false},
		// descriptor duplication writes no file - parses clean,
		// reads as a non-write
		{in: "echo hi 2>&1", want: false},
		{in: "cmd 1>&2", want: false},
		{in: "cmd >&2", want: false},
		{in: "cmd 2>&12", want: false},
		// input redirects read; heredocs read
		{in: "grep x < in.txt", want: false},
		{in: "python3 - <<'EOF'\nprint(1)\nEOF", want: false},
	}
	for _, tc := range cases {
		p := Parse(tc.in)
		wrote := false
		for _, seg := range p.Segments {
			if writesThrough(seg) {
				wrote = true
			}
		}
		if wrote != tc.want {
			t.Errorf("%q writesThrough=%v, want %v", tc.in, wrote, tc.want)
		}
	}
}

// TestFDRedirectAssess: writesThrough feeds the would-interrupt
// decision (the trust boundary) - a stderr file write presenting as a
// read is the WRONG direction to fail.
func TestFDRedirectAssess(t *testing.T) {
	for _, cmd := range []string{
		"echo hi 2> f.txt", "echo hi 2>> f.txt", "echo hi &> f.txt",
		"cmd 2>err.log >out.log", "cmd 2>err.log",
	} {
		if r := Assess(Parse(cmd)); !r.WouldInterrupt {
			t.Errorf("write not interrupted: %q (reasons %v)", cmd, r.Reasons)
		}
	}
	for _, cmd := range []string{"echo hi 2>&1", "cmd 1>&2", "cmd >&2", "echo hi"} {
		p := Parse(cmd)
		if p.ParseErr != nil || Assess(p).WouldInterrupt {
			t.Errorf("dup/benign command interrupted: %q (reasons %v, err %v)",
				cmd, Assess(p).Reasons, p.ParseErr)
		}
	}
	// the guard surfaces as would-interrupt (unparsable-command)
	for _, cmd := range []string{"cmd 2>|f", "cmd 2>;", "cmd 2>&|x"} {
		p := Parse(cmd)
		if p.ParseErr == nil {
			t.Fatalf("unclassifiable shape parsed silently clean: %q", cmd)
		}
		if r := Assess(p); !r.WouldInterrupt {
			t.Errorf("ParseErr must interrupt: %q (reasons %v)", cmd, r.Reasons)
		}
	}
}
