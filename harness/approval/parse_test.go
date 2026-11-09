package approval

import (
	"strings"
	"testing"
)

// TestFDRedirectParsePins pin the tokenizer over the fd-prefixed
// redirect forms (card t_2919b7a3, the execution-validated probe
// table): fd-prefixed forms are REDIRECT tokens, one word each - '&'
// inside them is never a control operator, and a shape the parser
// cannot classify surfaces as ParseErr (would-interrupt), never a
// silently reshaped segment.
func TestFDRedirectParsePins(t *testing.T) {
	cases := []struct {
		in      string
		want    []string // canonical words, segments flattened
		wantErr bool
	}{
		// spaced forms: the operator is its own redirect word
		{in: "echo hi 2> f.txt", want: []string{"echo", "hi", "2>", "f.txt"}},
		{in: "echo hi 2>> f.txt", want: []string{"echo", "hi", "2>>", "f.txt"}},
		// dup form parses clean as ONE redirect word, no file operand;
		// the old tokenizer split it into "2>" + op "&" + "1"
		{in: "echo hi 2>&1", want: []string{"echo", "hi", "2>&1"}},
		{in: "cmd 1>&2", want: []string{"cmd", "1>&2"}},
		{in: "cmd >&2", want: []string{"cmd", ">&2"}},
		{in: "cmd 2>&12", want: []string{"cmd", "2>&12"}},
		// stream-both forms: &> / &>> are redirect tokens, not '&'
		// background operators
		{in: "echo hi &> f.txt", want: []string{"echo", "hi", "&>", "f.txt"}},
		{in: "echo hi &>> all.log", want: []string{"echo", "hi", "&>>", "all.log"}},
		// fused forms stay whole words
		{in: "cmd 1>out.log", want: []string{"cmd", "1>out.log"}},
		{in: "cmd 2>err.log >out.log", want: []string{"cmd", "2>err.log", ">out.log"}},
		{in: "cmd 2>>err.log", want: []string{"cmd", "2>>err.log"}},
		// file-dup: the operand is not a digit, so it is a file (the
		// word continues past the '&')
		{in: "cmd 2>&log.txt", want: []string{"cmd", "2>&log.txt"}},
		{in: "cmd >& f.log", want: []string{"cmd", ">&", "f.log"}},
		{in: "cmd 2>&", want: []string{"cmd", "2>&"}},
		// a dup redirect followed by a real control chain stays clean
		{in: "cmd 2>&1 | wc -l", want: []string{"cmd", "2>&1", "wc", "-l"}},
		{in: "cmd 2>&1&&echo done", want: []string{"cmd", "2>&1", "echo", "done"}},
		// plain spaced redirect: unchanged tokenizer shape
		{in: "echo hi > f.txt", want: []string{"echo", "hi", ">", "f.txt"}},
		{in: "echo hi >> f.txt", want: []string{"echo", "hi", ">>", "f.txt"}},
		// fail-visible guard: a control operator while a redirect
		// token awaits its operand cannot be classified - ParseErr,
		// never a silently reshaped segment
		{in: "cmd 2>|f", wantErr: true},
		{in: "cmd 2>;", wantErr: true},
		{in: "cmd 2>)", wantErr: true},
		{in: "cmd 2>&|x", wantErr: true},
	}
	for _, tc := range cases {
		p := Parse(tc.in)
		if (p.ParseErr != nil) != tc.wantErr {
			t.Errorf("%q ParseErr=%v, wantErr=%v", tc.in, p.ParseErr, tc.wantErr)
		}
		if tc.wantErr {
			// fail visible: the raw text stays visibly handed over
			if len(p.Segments) != 1 || len(p.Segments[0].Words) != 1 ||
				p.Segments[0].Words[0] != tc.in {
				t.Errorf("%q: raw text not preserved on ParseErr: %#v", tc.in, p.Segments)
			}
			continue
		}
		var got []string
		for _, seg := range p.Segments {
			got = append(got, seg.Words...)
		}
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("%q words=%q, want %q (canonical %q)", tc.in, got, tc.want, p.Canonical)
		}
	}
}
