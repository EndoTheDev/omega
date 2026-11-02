package approval

import (
	"regexp"
	"strings"
)

// Egress hygiene (design Q7 + the reference's boundary): two passes
// run over the reviewed text before any third party sees it.
//
//  1. Schema stripping: answer-schema fragments (the six question
//     keys, their wire types) are cut from the command text - a
//     command that quotes the gate's own answer vocabulary at the
//     reviewer is the parse-divergence/self-advocacy class, and the
//     model must judge the OPERATIONS, never the framing.
//  2. Credential redaction: key-shaped material never leaves the
//     machine, even to the configured reviewer.

var schemaPairRes = schemaPairPatterns()

// schemaPairPatterns builds one regex per wire fragment the reviewer
// answers with (the six question keys plus the answer-envelope keys),
// matching the `"<key>": <value>` JSON-pair shape in any value form.
func schemaPairPatterns() []*regexp.Regexp {
	keys := []string{"verdict", "policy_allows", "blast_radius", "self_advocating",
		"reads_secrets", "sends_outbound", "choice", "confidence",
		"probabilities", "noul", "score", "answers", "questions"}
	out := make([]*regexp.Regexp, 0, len(keys))
	for _, k := range keys {
		pat := `"?` + k + `"?\s*:\s*(?:"(?:\\.|[^"\\])*"|[^\s,{};\"]*(?:\.[^\s,{};\"]*)*)`
		out = append(out, regexp.MustCompile(`(?i)`+pat))
	}
	return out
}

// stripSchemaFragments removes answer-schema fragments from the
// reviewed text. Idempotent; the OPERATIONS in the command stay.
func stripSchemaFragments(text string) string {
	for _, re := range schemaPairRes {
		text = re.ReplaceAllString(text, "")
	}
	return text
}

var redactionRes = []*regexp.Regexp{
	// key=value / key: value shapes the shell actually carries
	regexp.MustCompile(`(?i)\b((?:api[_-]?key|secret|token|password|passwd|access[_-]?key|private[_-]?key)\s*[:=]\s*)(\S+)`),
	// bearer headers
	regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._\-]{8,}`),
	// known token prefixes
	regexp.MustCompile(`\b(gh[pousr]_|sk-|sk-proj-|xox[baprs]-|AKIA|ASIA|glpat-|dop_v1_|nz_[A-Za-z0-9])[A-Za-z0-9_\-]{8,}`),
	// PEM blocks
	regexp.MustCompile(`-----BEGIN [^-]*PRIVATE KEY-----[\s\S]*?-----END [^-]*PRIVATE KEY-----`),
	// URL userinfo user:pass@
	regexp.MustCompile(`\b([A-Za-z][A-Za-z0-9+.\-]*://)([^/@\s:]{1,64}):[^@\s]{1,256}@`),
	// query-string credentials
	regexp.MustCompile(`(?i)([?&](?:token|key|password|api[_-]?key|secret|access[_-]?token|sig|signature)=)[^&\s]{3,}`),
	// cookie headers
	regexp.MustCompile(`(?i)(-H\s*["']?Cookie\s*:\s*)\S+`),
	// curl credential flags
	regexp.MustCompile(`(?i)((?:--data-binary|--data|-d|-u|--user|--header)\s+(?:["']?))(-?\s*(?:ghp_|sk-|Bearer\s)[^"'\s]{6,})`),
	// long random hex/base64-ish blobs (heuristic, 32+)
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

// sanitize is the egress boundary: strip schema fragments first (so a
// cut never splits a secret inside a stripped span), then redact.
// Redacted = true in the audit row when either pass changed text.
func sanitize(text string) (string, bool) {
	stripped := stripSchemaFragments(text)
	out := redactCredentials(stripped)
	return out, out != text
}

// hasSecretVocabulary is the cheap signal the AUDIT redaction bit
// rides (not a rule): does the command name credential material at
// all? The reviewer still makes the reads_secrets call - this only
// reports redaction honestly.
func hasSecretVocabulary(text string) bool {
	low := strings.ToLower(text)
	for _, k := range []string{".env", "id_rsa", "id_ed25519", "private key", "credentials", "keyring", "secret", "passwor", "token", "auth.json", ".aws", ".gnupg", ".ssh"} {
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}
