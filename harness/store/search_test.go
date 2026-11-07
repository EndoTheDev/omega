package store

// Search snippet verbatim contract (t_1901ce48): snippets are
// byte-for-byte stored content - no in-band match markers, no
// synthetic ellipses. Matched-term positions ride structured
// metadata (core.SearchResult.Matches, byte offsets).

import (
	"context"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

func searchFixture(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func assertVerbatim(t *testing.T, s *Store, query, content string, wantSpans int) core.SearchResult {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateSession(ctx, content[:4], "", "sess "+content[:4]); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := s.AppendMessage(ctx, content[:4], core.NewUser(content)); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	results, err := s.SearchMessages(ctx, query)
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	r := results[0]
	// The snippet must be verbatim stored content: the raw text rides
	// inside the stored envelope unchanged, no marker characters
	// inserted anywhere.
	if !strings.Contains(r.Snippet, content) {
		t.Fatalf("snippet %q does not carry the stored content verbatim %q", r.Snippet, content)
	}
	if strings.ContainsAny(r.Snippet, "\x01\x02\x03") {
		t.Fatalf("snippet %q carries in-band marker characters", r.Snippet)
	}
	// Every span must land on the query term itself.
	if len(r.Matches) != wantSpans {
		t.Fatalf("matches = %+v, want %d spans", r.Matches, wantSpans)
	}
	for _, m := range r.Matches {
		if m.Start < 0 || m.End > len(r.Snippet) || m.Start >= m.End {
			t.Fatalf("span %+v out of bounds for snippet %q", m, r.Snippet)
		}
		if !strings.Contains(strings.ToLower(r.Snippet[m.Start:m.End]), strings.ToLower(query)) {
			t.Fatalf("span [%d,%d] = %q does not cover the query %q", m.Start, m.End, r.Snippet[m.Start:m.End], query)
		}
	}
	return r
}

// TestSearchSnippetVerbatim is the card's acceptance case: a term
// stored without brackets never renders bracketed, and the span points
// at the term.
func TestSearchSnippetVerbatim(t *testing.T) {
	s := searchFixture(t)
	r := assertVerbatim(t, s, "PowerShell",
		"the setup ran under PowerShell and printed the broken or mangled or truncated or silently query", 1)
	if strings.Contains(r.Snippet, "[") || strings.Contains(r.Snippet, "]") {
		t.Fatalf("bracket markers rendered into bracket-free content: %q", r.Snippet)
	}
}

// TestSearchSnippetLiteralBrackets: brackets that ARE content stay
// content; the span still points at the plain term.
func TestSearchSnippetLiteralBrackets(t *testing.T) {
	s := searchFixture(t)
	assertVerbatim(t, s, "PowerShell",
		"stored [PowerShell] and PowerShell both render as written here", 2)
}

// TestSearchSnippetMultipleMatches: repeated terms all get spans.
func TestSearchSnippetMultipleMatches(t *testing.T) {
	s := searchFixture(t)
	assertVerbatim(t, s, "echo",
		"echo one echo two echo three", 3)
}

// TestResolveSnippet pins the marked+plain referee (the store's live
// envelope can never hold a raw sentinel byte - JSON escapes control
// bytes - so the ambiguity lane is a pure unit case): a clean pair
// parses to exact spans; a pair whose content side holds a sentinel
// byte falls back to the plain verbatim with no spans.
func TestResolveSnippet(t *testing.T) {
	verbatim, spans := resolveSnippet("x \x01needle\x02 y and \x01needle\x02 again", "x needle y and needle again")
	if verbatim != "x needle y and needle again" {
		t.Fatalf("verbatim = %q", verbatim)
	}
	want := []core.SearchMatch{{Start: 2, End: 8}, {Start: 15, End: 21}}
	if len(spans) != 2 || spans[0] != want[0] || spans[1] != want[1] {
		t.Fatalf("spans = %+v, want %+v", spans, want)
	}
	// The no-match case: marked equals plain, no spans.
	if v, sp := resolveSnippet("plain text", "plain text"); v != "plain text" || sp != nil {
		t.Fatalf("no-match case = %q %+v", v, sp)
	}
	// The ambiguity case: the content side itself holds a sentinel
	// byte - the parse would corrupt, so the plain excerpt ships.
	plain := "a \x01 b is here"
	verbatim, spans = resolveSnippet("a \x01 b is \x01here\x02", plain)
	if verbatim != plain || spans != nil {
		t.Fatalf("ambiguity fallback = %q %+v, want the plain excerpt with no spans", verbatim, spans)
	}
}

// TestSearchSessionsVerbatim: the unified lane - a title hit carries
// the label verbatim, a content hit carries verbatim content with the
// spans, and no bracket markers survive on either.
func TestSearchSessionsVerbatim(t *testing.T) {
	s := searchFixture(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "t-title", "", "the needle title"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := s.CreateSession(ctx, "t-body", "", "calm body"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := s.AppendMessage(ctx, "t-body", core.NewUser("a needle in the body")); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	results, err := s.SearchSessions(ctx, "needle")
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	spansBySpanCount := 0
	for _, r := range results {
		if strings.ContainsAny(r.Snippet, "[]") {
			t.Fatalf("snippet %q carries bracket markers", r.Snippet)
		}
		if r.Title {
			if r.Snippet != "the needle title" {
				t.Fatalf("title snippet = %q, want the label verbatim", r.Snippet)
			}
			continue
		}
		if !strings.Contains(r.Snippet, "a needle in the body") {
			t.Fatalf("content snippet %q lost the verbatim body text", r.Snippet)
		}
		if len(r.Matches) == 0 {
			t.Fatalf("content hit has no match spans")
		}
		for _, m := range r.Matches {
			if r.Snippet[m.Start:m.End] != "needle" {
				t.Fatalf("span = %q, want needle", r.Snippet[m.Start:m.End])
			}
		}
		spansBySpanCount += len(r.Matches)
	}
	if spansBySpanCount == 0 {
		t.Fatalf("no content match seen at all: %+v", results)
	}
}

// TestSearchToolSnippetVerbatim: the agent-facing sessions.search tool
// output renders stored text verbatim, with only the label prefix as
// presentation formatting.
func TestSearchToolSnippetVerbatim(t *testing.T) {
	s := searchFixture(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, "s1", "", "my labeled session"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := s.AppendMessage(ctx, "s1", core.NewUser("canary PowerShell echo check")); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	tp := &searchToolProvider{store: s}
	result, err := tp.Tools()["sessions.search"].Run(ctx, map[string]any{"query": "PowerShell"})
	if err != nil {
		t.Fatalf("tool.Run: %v", err)
	}
	// The tool line is "[label] <verbatim content>" - the label prefix
	// is formatting; the content after it must appear unbracketed.
	line := strings.TrimSuffix(result, "\n")
	if !strings.HasPrefix(line, "[my labeled session]") {
		t.Fatalf("result = %q, want the label prefix", line)
	}
	if !strings.Contains(line, "canary PowerShell echo check") {
		t.Fatalf("verbatim content lost from the tool line: %q", line)
	}
	if strings.Contains(line, "[PowerShell]") {
		t.Fatalf("in-band match bracket rendered: %q", line)
	}
}
