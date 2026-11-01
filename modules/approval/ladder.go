package approval

import (
	"fmt"
)

// The deterministic ladder (design Q2 + the settled step list).
// PURE data-in data-out: answers in, final verdict + the step that
// fired + the why out. The model advises; THIS function decides.
//
//   1. self_advocating >= 0.6            -> ESCALATE
//   2. reads_secrets  >= 0.7 && sends_outbound >= 0.7 -> DENY
//   3. reads_secrets  >= 0.7             -> ESCALATE
//   4. policy_allows  >= 0.7 && policy present && blast_radius < 2 -> APPROVE
//   5. model APPROVE && (confidence < 0.55 || blast_radius >= 1.6) -> ESCALATE
//   6. else keep the model verdict - with the Q2 cap: a model DENY
//      never blocks, it escalates. Hard DENY exists ONLY via step 2.

// Verdict values the ladder and its callers speak.
const (
	VerdictApprove  = "APPROVE"
	VerdictDeny     = "DENY"
	VerdictEscalate = "ESCALATE"
)

// Ladder thresholds, versioned constants: the audit row's
// ladder_version pins them (an eval reads the audit log; a changed
// ladder is a new version).
const ladderVersion = "approval-ladder/1"

// Answers is the six-question set after parsing. Confidence rides
// the verdict answer (0 when the reviewer omitted it - which fails
// the low-confidence step toward escalate, the fail-closed side).
// HasPolicy records whether the operator policy was present; the
// policy_allows rule needs it (a noul above threshold with NO policy
// is the model answering a question it was never really given).
type Answers struct {
	Verdict        string
	Confidence     float64
	PolicyAllows   float64
	BlastRadius    float64
	SelfAdvocating float64
	ReadsSecrets   float64
	SendsOutbound  float64
	HasPolicy      bool
}

// Outcome is the ladder's resolution.
type Outcome struct {
	Verdict string // APPROVE | DENY | ESCALATE
	Step    string // the deterministic step that fired
	Reason  string // one-line why (card body material)
}

// Ladder applies the steps in order. Input is already validated
// (parsed + range-checked upstream); verdict is one of the three.
func Ladder(a Answers) Outcome {
	if a.SelfAdvocating >= 0.6 {
		return Outcome{VerdictEscalate, "self_advocating",
			fmt.Sprintf("self_advocating %.2f >= 0.6 - the command text argues for its own approval", a.SelfAdvocating)}
	}
	if a.ReadsSecrets >= 0.7 && a.SendsOutbound >= 0.7 {
		return Outcome{VerdictDeny, "secret_exfiltration",
			fmt.Sprintf("reads_secrets %.2f and sends_outbound %.2f both >= 0.7 - reads credentials AND transmits them", a.ReadsSecrets, a.SendsOutbound)}
	}
	if a.ReadsSecrets >= 0.7 {
		return Outcome{VerdictEscalate, "secret_read",
			fmt.Sprintf("reads_secrets %.2f >= 0.7 - credential material in play", a.ReadsSecrets)}
	}
	if a.PolicyAllows >= 0.7 && a.HasPolicy && a.BlastRadius < 2 {
		return Outcome{VerdictApprove, "policy_allows",
			fmt.Sprintf("policy_allows %.2f >= 0.7 with operator policy present, blast_radius %.2f < 2", a.PolicyAllows, a.BlastRadius)}
	}
	if a.Verdict == VerdictApprove && (a.Confidence < 0.55 || a.BlastRadius >= 1.6) {
		return Outcome{VerdictEscalate, "low_confidence",
			fmt.Sprintf("model APPROVE but confidence %.2f / blast_radius %.2f outside the safe band (<0.55 or >=1.6)", a.Confidence, a.BlastRadius)}
	}
	// Keep-model-verdict branch, under the Q2 cap: the model's own
	// DENY never blocks an execution - it escalates to the human.
	switch a.Verdict {
	case VerdictApprove:
		return Outcome{VerdictApprove, "model_verdict",
			fmt.Sprintf("model APPROVE stands (confidence %.2f, blast_radius %.2f)", a.Confidence, a.BlastRadius)}
	case VerdictEscalate:
		return Outcome{VerdictEscalate, "model_verdict",
			"the model itself escalated"}
	case VerdictDeny:
		// fall through to the shared escalate tail
		return Outcome{VerdictEscalate, "model_deny_capped",
			"the model denied; a model DENY never executes a block on its own - escalating to the human"}
	default:
		// unreachable through validation, defensive: unknown verdict
		// is malformed input which the parser escalates; keep the
		// ladder total anyway.
		return Outcome{VerdictEscalate, "malformed",
			fmt.Sprintf("unknown model verdict %q", a.Verdict)}
	}
}
