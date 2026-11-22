package approval

import (
	"regexp"
)

// Egress hygiene: two passes run over the reviewed text before any third party sees it.
//  1. Schema stripping cuts answer-schema fragments (the six question keys, their wire
//     types) - a command quoting the gate's own answer vocabulary at the reviewer is the
//     parse-divergence/self-advocacy class, and the model must judge the OPERATIONS.
//  2. Credential redaction: key-shaped material never leaves the machine, even to the
//     configured reviewer.

var schemaPairRes = schemaPairPatterns()

func schemaPairPatterns() []*regexp.Regexp {
	keys := []string{"verdict", "policy_allows", "blast_radius", "self_advocating",
		"reads_secrets", "sends_outbound", "choice", "confidence",
		"probabilities", "noul", "score", "answers", "questions",
		// the evidence-only judge's own answer vocabulary: a call quoting `block: 0.9` at
		// the judge is the same parse-divergence/self-advocacy class, so it is cut too.
		"block"}
	out := make([]*regexp.Regexp, 0, len(keys))
	for _, k := range keys {
		pat := `"?` + k + `"?\s*:\s*(?:"(?:\\.|[^"\\])*"|[^\s,{};\"]*(?:\.[^\s,{};\"]*)*)`
		out = append(out, regexp.MustCompile(`(?i)`+pat))
	}
	return out
}

// stripSchemaFragments removes answer-schema fragments; idempotent, the OPERATIONS stay.
func stripSchemaFragments(text string) string {
	for _, re := range schemaPairRes {
		text = re.ReplaceAllString(text, "")
	}
	return text
}

var redactionRes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b((?:api[_-]?key|secret|token|password|passwd|access[_-]?key|private[_-]?key)\s*[:=]\s*)(\S+)`),
	regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._\-]{8,}`),
	regexp.MustCompile(`\b(gh[pousr]_|sk-|sk-proj-|xox[baprs]-|AKIA|ASIA|glpat-|dop_v1_|nz_[A-Za-z0-9])[A-Za-z0-9_\-]{8,}`),
	// PEM blocks
	regexp.MustCompile(`-----BEGIN [^-]*PRIVATE KEY-----[\s\S]*?-----END [^-]*PRIVATE KEY-----`),
	// URL userinfo user:pass@
	regexp.MustCompile(`\b([A-Za-z][A-Za-z0-9+.\-]*://)([^/@\s:]{1,64}):[^@\s]{1,256}@`),
	regexp.MustCompile(`(?i)([?&](?:token|key|password|api[_-]?key|secret|access[_-]?token|sig|signature)=)[^&\s]{3,}`),
	regexp.MustCompile(`(?i)(-H\s*["']?Cookie\s*:\s*)\S+`),
	regexp.MustCompile(`(?i)((?:--data-binary|--data|-d|-u|--user|--header)\s+(?:["']?))(-?\s*(?:ghp_|sk-|Bearer\s)[^"'\s]{6,})`),
	regexp.MustCompile(`\b[A-Za-z0-9+/_\-]{40,}={0,2}\b`),
}

// redactCredentials masks credential-shaped material before egress.
// Idempotent - a second pass masks nothing new.
func redactCredentials(text string) string {
	text = regexp.MustCompile(`-----BEGIN [^-]*PRIVATE KEY-----`).
		ReplaceAllString(text, "[REDACTED PRIVATE KEY]")
	for _, re := range redactionRes {
		text = re.ReplaceAllString(text, "[REDACTED]")
	}
	return text
}

// sanitize is the egress boundary: strip schema fragments first.
func sanitize(text string) (string, bool) {
	stripped := stripSchemaFragments(text)
	out := redactCredentials(stripped)
	return out, out != text
}
