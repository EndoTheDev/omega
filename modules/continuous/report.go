package continuous

// report.go: the wake report's shape (card t_72ecca67, ruling 11's
// second half). The run's closing message IS the report (ruling 10);
// before it lands in the agent's canonical chat the runtime shapes it,
// never the agent:
//
//   - a claim sentence gains its ledger citation when the run's
//     evidence backs it (an ok row whose verifier proves the path);
//   - a claim sentence with no backing - or one the ledger
//     contradicts - is stripped before persistence (ruling 11:
//     unverified claims do not land);
//   - the runtime evidence tail (call counts, row ids) closes the
//     report; a run that called no tools gets no tail (nothing to
//     cite, never a fabricated verdict).
//
// Verifier scope, kept honest (ruling 11): the cheap check is the file
// stat ONLY. URL claims ride their own fetch: a claim over a URL is
// backed when the run's ledger carries an ok row FOR that URL (the
// fetch tool's own result, the HTTP outcome) and strips when the fetch
// failed or the run never fetched it - fail closed either way. The
// verifier string `url:...` is a locator, not a check.

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/modules/datadir"
)

// WrapWakeSink wraps the run's TurnSink with the report shaper. The
// shape applies only inside a wake child (OMEGA_AGENT names a
// continuous profile); every other process gets the sink untouched -
// the report is a wake artifact, a manual run is never reshaped.
func WrapWakeSink(inner agent.TurnSink) agent.TurnSink {
	name := os.Getenv("OMEGA_AGENT")
	if name == "" {
		return inner
	}
	home := datadir.Home()
	if home == "" {
		return inner
	}
	p, err := agent.LoadProfile(home, name)
	if err != nil || !p.Continuous {
		return inner
	}
	return &wakeReportSink{inner: inner}
}

// wakeReportSink wraps the store TurnSink. Intermediate turns pass
// through untouched; the run's CLOSING message is the one assistant
// turn carrying content and no tool calls (the loop ends exactly
// there) - only it is shaped.
type wakeReportSink struct {
	inner agent.TurnSink
}

func (s *wakeReportSink) AppendTurnMessage(ctx context.Context, msg core.Message) error {
	if a, ok := msg.(core.Assistant); ok && a.Content != "" && len(a.ToolCalls) == 0 {
		a.Content = s.shape(ctx, a.Content)
		msg = a
	}
	return s.inner.AppendTurnMessage(ctx, msg)
}

// shape reads this run's ledger rows and reshapes the closing text.
// A broken ledger cannot block the report: it degrades to no
// evidence, which strips claims - fail closed, still honest.
// AgentHome answers a HOME; the ledger is a data-dir consumer.
func (s *wakeReportSink) shape(ctx context.Context, content string) string {
	data, err := datadir.AgentHome()
	if err != nil {
		return shapeReport(content, nil)
	}
	lg, openErr := OpenLedger(datadir.Dir(data))
	if openErr != nil {
		return shapeReport(content, nil)
	}
	defer lg.Close()
	rows, err := lg.List(ctx, spawnRunID)
	if err != nil {
		return shapeReport(content, nil)
	}
	return shapeReport(content, rows)
}

// claimVerbs are the action words a claim sentence leans on.
var claimVerbs = regexp.MustCompile(`(?i)\b(created|wrote|updated|deleted|removed|edited|modified|renamed|moved|fetched|downloaded|uploaded|saved|read|installed|deployed)\b`)

// pathTokens finds the checkable objects of a claim: a path or a URL.
var pathTokens = regexp.MustCompile(`https?://[\w.@%+=:/~#?&-]+|(?:\.{1,2}/|~/|/)[\w.@%+=:/~-]+|\b[\w-]+(?:\.[\w-]+)+\b`)

// maxTailRows caps the id list the evidence tail names.
const maxTailRows = 12

// evidence is the run's folded ledger tables. File tokens match the
// file tables (the verifier's stat), URL tokens the url tables (the
// fetch row's ok/fail). A claim is backed only when EVERY token it
// names resolves seen and unfailed, and at least one token carries a
// verifiable row.
type evidence struct {
	fileVerified map[string][]int64
	fileFailed   map[string]bool
	fileSeen     map[string]bool
	urlVerified  map[string][]int64
	urlFailed    map[string]bool
	urlSeen      map[string]bool
}

// shapeReport is the pure shaping core: content + rows -> report. The
// sink feeds it the run's ledger rows; tests feed it fixtures.
func shapeReport(content string, rows []record) string {
	ev := evidenceSets(rows)
	paragraphs := strings.Split(content, "\n")
	out := make([]string, 0, len(paragraphs))
	for _, para := range paragraphs {
		out = append(out, shapeParagraph(para, ev, rows)...)
	}
	text := strings.Join(out, "\n")
	if tail := evidenceTail(rows); tail != "" {
		if strings.TrimSpace(text) != "" {
			text += "\n\n" + tail
		} else {
			text = "[report body stripped: no claim carried ledger evidence]\n\n" + tail
		}
	} else if strings.TrimSpace(text) == "" {
		// no tools ran and everything stripped: say no more than that
		text = "[report body stripped: no claim carried ledger evidence]"
	}
	return text
}

// shapeParagraph shapes one line: sentence-split, claims judged,
// stripped sentences dropped whole.
func shapeParagraph(para string, ev evidence, rows []record) []string {
	if strings.TrimSpace(para) == "" {
		return []string{para}
	}
	kept := []string{}
	for _, sent := range splitSentences(para) {
		switch judgeSentence(sent, ev) {
		case "cite":
			kept = append(kept, citeSentence(sent, ev, rows))
		case "strip":
			// a claim with no evidence does not land
		default: // "keep"
			kept = append(kept, sent)
		}
	}
	return kept
}

// splitSentences breaks one line at sentence ends; the line itself
// already splits paragraphs, so bullets and headings stay whole.
func splitSentences(line string) []string {
	end := regexp.MustCompile(`[.!?]["')\]]?(?:\s+|$)`)
	var out []string
	last := 0
	for _, loc := range end.FindAllStringIndex(line, -1) {
		seg := strings.TrimSpace(line[last:loc[1]])
		if seg != "" {
			out = append(out, seg)
		}
		last = loc[1]
	}
	if rest := strings.TrimSpace(line[last:]); rest != "" {
		out = append(out, rest)
	}
	if len(out) == 0 {
		out = []string{strings.TrimSpace(line)}
	}
	return out
}

// judgeSentence answers keep / cite / strip for one sentence. A
// sentence's tokens split by kind: file-like tokens judge against the
// file (stat) tables, URL tokens against the url (fetch row) tables.
func judgeSentence(sent string, ev evidence) string {
	if !claimVerbs.MatchString(sent) {
		return "keep"
	}
	paths := matchPaths(sent, ev)
	urls := matchURLs(sent, ev)
	if len(paths) == 0 && len(urls) == 0 {
		return "keep" // a verb with no checkable object is not a claim
	}
	citable, backed := 0, true
	for _, p := range paths {
		if ev.fileFailed[p] || !ev.fileSeen[p] {
			backed = false
		}
		if len(ev.fileVerified[p]) > 0 {
			citable++
		}
	}
	for _, u := range urls {
		if ev.urlFailed[u] || !ev.urlSeen[u] {
			backed = false
		}
		if len(ev.urlVerified[u]) > 0 {
			citable++
		}
	}
	if backed && citable > 0 {
		return "cite"
	}
	return "strip"
}

// matchPaths extracts the sentence's file-like tokens and resolves
// each onto the ledger's path key for the same file.
func matchPaths(sent string, ev evidence) []string {
	var out []string
	seenAny := map[string]bool{}
	for _, tok := range pathTokens.FindAllString(sent, -1) {
		tok = strings.Trim(tok, "`\"'.,;:!?")
		if tok == "" || isNumericToken(tok) || isURLToken(tok) {
			continue // URLs judge in matchURLs; versions are prose
		}
		p := resolve(tok, ev)
		if !seenAny[p] {
			seenAny[p] = true
			out = append(out, p)
		}
	}
	return out
}

// matchURLs extracts the sentence's http(s) tokens, normalized, and
// resolves each onto the ledger's url key for the same fetch. Every
// miss resolves to the token itself (unseen -> strips).
func matchURLs(sent string, ev evidence) []string {
	var out []string
	seenAny := map[string]bool{}
	for _, tok := range pathTokens.FindAllString(sent, -1) {
		tok = strings.Trim(tok, "`\"'.,;:!?)(")
		if !isURLToken(tok) {
			continue
		}
		u := normURL(tok)
		if !seenAny[u] {
			seenAny[u] = true
			out = append(out, u)
		}
	}
	return out
}

// isURLToken reports whether a raw token is an http(s) URL.
func isURLToken(tok string) bool {
	return strings.HasPrefix(tok, "http://") || strings.HasPrefix(tok, "https://")
}

// normURL is the URL match key: trimmed, trailing slash folded away -
// a fetched page and the claim over it agree modulo the slash.
func normURL(u string) string {
	return strings.TrimSuffix(strings.TrimSpace(u), "/")
}

// isNumericToken skips bare numbers, versions, and dotted dates: "1.5"
// and "v0.12.0" are prose furniture, never file claims.
func isNumericToken(tok string) bool {
	body := strings.TrimPrefix(tok, "v")
	if body == "" {
		return false
	}
	for _, r := range body {
		if !strings.ContainsRune("0123456789._-", r) {
			return false
		}
	}
	return true
}

// resolve maps a claim token onto the ledger's path key for the same
// file: exact, or the row path's basename when the token is file-like.
// Every miss resolves to the token itself (unseen -> strip).
func resolve(tok string, ev evidence) string {
	token := strings.TrimPrefix(strings.TrimPrefix(tok, "./"), "~/")
	present := func(p string) bool { return ev.fileVerified[p] != nil || ev.fileFailed[p] }
	if present(token) {
		return token
	}
	for p := range ev.fileVerified {
		if strings.HasSuffix(p, "/"+token) {
			return p
		}
	}
	for p := range ev.fileFailed {
		if strings.HasSuffix(p, "/"+token) {
			return p
		}
	}
	return token
}

// evidenceSets folds the run's rows into the claim tables. Rows with a
// file-stat verifier fold the file tables; rows with a url locator
// fold the url tables (the row's ok flag is the fetch's verdict);
// anything else carried nothing cheap to check.
func evidenceSets(rows []record) evidence {
	ev := evidence{
		fileVerified: map[string][]int64{},
		fileFailed:   map[string]bool{},
		fileSeen:     map[string]bool{},
		urlVerified:  map[string][]int64{},
		urlFailed:    map[string]bool{},
		urlSeen:      map[string]bool{},
	}
	for _, r := range rows {
		switch {
		case strings.HasPrefix(r.Verifier, "file:"):
			i := strings.Index(r.Verifier, ":")
			if i < 0 {
				continue
			}
			rest := r.Verifier[i+1:]
			exists := strings.HasPrefix(rest, "exists:")
			path := strings.TrimPrefix(strings.TrimPrefix(rest, "exists:"), "missing:")
			if path == "" {
				continue
			}
			ev.fileSeen[path] = true
			switch {
			case r.OK && exists:
				ev.fileVerified[path] = append(ev.fileVerified[path], r.ID)
			case !r.OK || !exists:
				ev.fileFailed[path] = true
			}
		case strings.HasPrefix(r.Verifier, "url:"):
			u := normURL(strings.TrimPrefix(r.Verifier, "url:"))
			if u == "" {
				continue
			}
			ev.urlSeen[u] = true
			switch {
			case r.OK:
				ev.urlVerified[u] = append(ev.urlVerified[u], r.ID)
			default:
				ev.urlFailed[u] = true
			}
		}
	}
	return ev
}

// citeSentence appends the sentence's backing row ids, file rows and
// url rows alike. Citations are runtime text (the evidence ledger owns
// their truth); the ids come straight from the rows.
func citeSentence(sent string, ev evidence, rows []record) string {
	ids := map[int64]bool{}
	for _, p := range matchPaths(sent, ev) {
		for _, id := range ev.fileVerified[p] {
			ids[id] = true
		}
	}
	for _, u := range matchURLs(sent, ev) {
		for _, id := range ev.urlVerified[u] {
			ids[id] = true
		}
	}
	if len(ids) == 0 {
		return sent
	}
	var list []string
	for _, r := range rows {
		if ids[r.ID] {
			list = append(list, fmt.Sprintf("#%d", r.ID))
		}
	}
	return fmt.Sprintf("%s [ledger %s]", sent, strings.Join(list, ", "))
}

// evidenceTail is the runtime-owned closing stamp: what the run did,
// row by row. A run with zero tool calls writes nothing - there is
// nothing to cite and nothing to invent.
func evidenceTail(rows []record) string {
	if len(rows) == 0 {
		return ""
	}
	okN := 0
	ids := make([]string, 0, maxTailRows)
	for _, r := range rows {
		if r.OK {
			okN++
		}
		if len(ids) < maxTailRows {
			ids = append(ids, fmt.Sprintf("#%d", r.ID))
		}
	}
	more := ""
	if len(rows) > maxTailRows {
		more = fmt.Sprintf(", +%d more", len(rows)-maxTailRows)
	}
	return fmt.Sprintf("_Evidence (runtime ledger): %d tool calls, %d ok - rows %s%s._",
		len(rows), okN, strings.Join(ids, ", "), more)
}
