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
// The cheap verifier covers file paths only (ruling 11's file stat);
// URL and free-text claims have no cheap evidence and fail closed -
// they strip. Tuning that heuristics is a named follow-up, not here.

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

// shapeReport is the pure shaping core: content + rows -> report. The
// sink feeds it the run's ledger rows; tests feed it fixtures.
func shapeReport(content string, rows []record) string {
	verified, failed, seen := evidenceSets(rows)
	paragraphs := strings.Split(content, "\n")
	out := make([]string, 0, len(paragraphs))
	for _, para := range paragraphs {
		out = append(out, shapeParagraph(para, verified, failed, seen, rows)...)
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
func shapeParagraph(para string, verified map[string][]int64, failed, seen map[string]bool, rows []record) []string {
	if strings.TrimSpace(para) == "" {
		return []string{para}
	}
	kept := []string{}
	for _, sent := range splitSentences(para) {
		switch judgeSentence(sent, verified, failed, seen) {
		case "cite":
			kept = append(kept, citeSentence(sent, verified, rows))
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

// judgeSentence answers keep / cite / strip for one sentence.
func judgeSentence(sent string, verified map[string][]int64, failed, seen map[string]bool) string {
	if !claimVerbs.MatchString(sent) {
		return "keep"
	}
	paths := matchPaths(sent, verified, failed)
	if len(paths) == 0 {
		return "keep" // a verb with no checkable object is not a claim
	}
	citable, backed := 0, true
	for _, p := range paths {
		if failed[p] || !seen[p] {
			backed = false
		}
		if len(verified[p]) > 0 {
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
func matchPaths(sent string, verified map[string][]int64, failed map[string]bool) []string {
	raw := pathTokens.FindAllString(sent, -1)
	seenAny := map[string]bool{}
	var out []string
	for _, tok := range raw {
		tok = strings.Trim(tok, "`\"'.,;:!?")
		if tok == "" || isNumericToken(tok) {
			continue // versions and dates are not claims
		}
		p := resolve(tok, verified, failed)
		if !seenAny[p] {
			seenAny[p] = true
			out = append(out, p)
		}
	}
	return out
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
func resolve(tok string, verified map[string][]int64, failed map[string]bool) string {
	token := strings.TrimPrefix(strings.TrimPrefix(tok, "./"), "~/")
	present := func(p string) bool { return verified[p] != nil || failed[p] }
	if present(token) {
		return token
	}
	for p := range verified {
		if strings.HasSuffix(p, "/"+token) {
			return p
		}
	}
	for p := range failed {
		if strings.HasSuffix(p, "/"+token) {
			return p
		}
	}
	return token
}

// evidenceSets folds the run's rows into the claim tables.
func evidenceSets(rows []record) (verified map[string][]int64, failed, seen map[string]bool) {
	verified = map[string][]int64{}
	failed = map[string]bool{}
	seen = map[string]bool{}
	for _, r := range rows {
		i := strings.Index(r.Verifier, ":")
		if !strings.HasPrefix(r.Verifier, "file:") || i < 0 {
			continue // the call carried nothing cheap to check
		}
		rest := r.Verifier[i+1:]
		exists := strings.HasPrefix(rest, "exists:")
		path := strings.TrimPrefix(strings.TrimPrefix(rest, "exists:"), "missing:")
		if path == "" {
			continue
		}
		seen[path] = true
		switch {
		case r.OK && exists:
			verified[path] = append(verified[path], r.ID)
		case !r.OK || !exists:
			failed[path] = true
		}
	}
	return verified, failed, seen
}

// citeSentence appends the sentence's backing row ids. Citations are
// runtime text (the evidence ledger owns their truth); the ids come
// straight from the rows.
func citeSentence(sent string, verified map[string][]int64, rows []record) string {
	ids := map[int64]bool{}
	for _, p := range matchPaths(sent, verified, nil) {
		for _, id := range verified[p] {
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
