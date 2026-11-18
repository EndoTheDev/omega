package approval

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// The gate (design Q3/Q11/Q13): one gate, one policy text - agent
// sessions AND cron workers consult the same object. The Gate is the
// approval module's agent.ApprovalProvider implementation.
type Gate struct {
	// cfgMu guards cfg: the host's settings hot-apply
	// path replaces the config (SetConfig) while the loop gates tool
	// calls in parallel. Every decision reads one consistent copy.
	cfgMu sync.RWMutex
	cfg   Config

	// verdicts/notifications are the consumer-side faces (part 3 seam
	// constitution): local interfaces with just what the gate calls,
	// resolved live from the universal registry, nil-safe by the
	// classic law. The owner capsules never know this gate exists.
	verdicts      func() verdictHub     // read live: the service stays the source of truth
	notifications func() finishedThings // read live; nil-safe
	logger        agent.LoggerProvider
	audit         *Audit
	cache         *Cache
	questionsFP   string
}

// verdictHub is the consumer-side face of the verdict capsules
// question cards (part 3 seam constitution): just the methods the gate
// calls. The verdict capsule satisfies it structurally; no shared
// type exists.
type verdictHub interface {
	// Ask registers the card and BLOCKS for the answer (see the
	// implementing capsule for the race and expiry law).
	Ask(ctx context.Context, card core.QuestionPrompt) (core.QuestionAnswer, error)
	// Answer records the user's answer to one pending card; FIRST
	// answer wins.
	Answer(sessionID, cardID string, v core.QuestionAnswer) (core.QuestionAnswer, error)
	// Pending lists the unanswered, unexpired cards, oldest first.
	Pending(sessionID string) []core.QuestionPrompt
}

// finishedThings is the consumer-side face of the finished-things
// store: just the emit method the escalation notice calls.
type finishedThings interface {
	Emit(n core.Notice) (core.Notice, error)
}

// The run-origin values the gate compares (the launch surfaces write
// them; the words stay stable on the wire while the identifiers carry
// no capsule noun).
const (
	originScheduled = "cron"
	originSubagent  = "delegate"
)

// NewGate builds the gate. verdicts and notifications are closures
// over the live Context (a seam rewire is visible at ask time); the
// logger may be nil. Smart mode without a reviewer host/model still
// mounts - every gated command then fails closed into escalation,
// honest about a dead review rather than silently open. The config is
// the boot value; the host's hot-apply path (SetConfig) keeps it live
// so a settings flip applies without a restart.
func NewGate(cfg Config, verdicts func() verdictHub, notifications func() finishedThings, logger agent.LoggerProvider) *Gate {
	return &Gate{
		cfg:           cfg,
		verdicts:      verdicts,
		notifications: notifications,
		logger:        logger,
		audit:         NewAudit(cfg.Home),
		cache:         NewCache(),
		questionsFP:   questionsFingerprint(),
	}
}

// SetConfig replaces the gate's config at runtime (card t_34cd1921,
// approvals goes live): the host's settings hot-apply path
// (WatchConfig / settings save) calls this so a mode, policy or model
// flip takes effect on the running agent without a restart. Every
// decision re-reads the config (config(), mode(), the reviewer
// request), so only this write needs coordination. The audit home is
// wiring, not a knob, and rides the boot value.
func (g *Gate) SetConfig(cfg Config) {
	g.cfgMu.Lock()
	defer g.cfgMu.Unlock()
	g.cfg = cfg
}

// config returns one consistent copy of the live config; callers read
// fields off the copy, never off g.cfg directly.
func (g *Gate) config() Config {
	g.cfgMu.RLock()
	defer g.cfgMu.RUnlock()
	return g.cfg
}

// The gate-line vocabulary (card t_1b3e2a8b, fixed by the design
// ruling): one decision record per gated command, rendered inside the
// guarded tool call's entry. The strings are the contract - no
// rewording.
const (
	GateAutoApproved = "smart gate: auto-approved"
	GateAutoDenied   = "smart gate: auto-denied"
	GateEscApproved  = "smart gate: escalated -> approved"
	GateEscDenied    = "smart gate: escalated -> denied - command skipped"
	GateEscExpired   = "smart gate: escalated -> expired"
	GateEscCancelled = "smart gate: escalated -> cancelled"
	// GateEscAnswered is the follow-up resolution (card t_63204dfd):
	// the card was answered with typed guidance instead of an option
	// pick. Deny with directions - the command does NOT run, the agent
	// receives the text. The stamp carries the guidance after the
	// vocabulary (truncTo renders the tail).
	GateEscAnswered = "smart gate: escalated -> answered"
)

// followUpReasonPrefix marks a follow-up resolution's reason (before
// the guidance text): the vocabulary the tool error and the audit row
// carry.
const followUpReasonPrefix = "follow-up guidance: "

// followUpMax is the guidance size cap (card t_63204dfd): the verdict
// endpoint caps its message field, and the gate caps whatever arrives
// by any other surface (the CLI's typed answer).
const followUpMax = 2000
const stampGuidanceMax = 240

// stamp renders the transcript-facing decision record (card
// t_1b3e2a8b). Only smart-mode decisions are machine decisions: a
// manual-mode ask is the human's own verdict (its in-the-moment card
// is the surface, P2 - no line). An escalation that never resolved
// (a background refusal, Q11) has no resolution to record - empty
// line, the tool error text stays the visible truth. fromCard marks
// the card-settled paths: the RESOLUTION lands as the line, never the
// model's original advice.
func (g *Gate) stamp(o Outcome, fromCard bool) string {
	if g.mode() != ModeSmart {
		return ""
	}
	if fromCard {
		if o.Step == "card_expired" {
			return GateEscExpired
		}
		switch o.Verdict {
		case VerdictApprove:
			return GateEscApproved
		case VerdictDeny:
			return GateEscDenied
		}
		return GateEscCancelled
	}
	switch o.Verdict {
	case VerdictApprove:
		return GateAutoApproved
	case VerdictDeny:
		return GateAutoDenied
	}
	return ""
}

// Gate implements agent.ApprovalProvider. Contract: nil error =
// execute, non-nil = the tool result refusing execution; the line is
// the decision record stamped onto the call's tool result (empty =
// no machine decision). Concurrency-safe: the loop runs a turn's tool
// calls in parallel.
func (g *Gate) Gate(ctx context.Context, toolName string, args map[string]any) (string, error) {
	return g.decide(ctx, toolName, args, nil)
}

// GateWithConversation is the agent.GateProvenance upgrade (card
// t_e3cd4381): the loop hands the run's conversation so the safeagents
// detectors can ask the provenance question - does the host this call
// reaches appear in the HUMAN's own words? A gate consulted the plain
// way sees no conversation, so nothing is sourced and every
// non-allowlisted host reads as novel (the documented floor).
func (g *Gate) GateWithConversation(ctx context.Context, toolName string, args map[string]any, conversation []core.Message) (string, error) {
	return g.decide(ctx, toolName, args, conversation)
}

// decide is the one decision path: the legacy shell lane (parse,
// reviewer, ladder) plus the safeagents lane (request-only detectors,
// severity tiers). Both feed the same escalation machinery.
func (g *Gate) decide(ctx context.Context, toolName string, args map[string]any, conversation []core.Message) (string, error) {
	start := time.Now()
	if g.mode() == ModeOff {
		return "", nil // Q10: an off gate decides nothing, logs nothing
	}
	live := g.config()
	origin := core.RunOriginFromContext(ctx)
	session := agent.SessionIDFromContext(ctx)

	// The legacy lane (Q1/Q7): shell commands only - parse + static
	// expansion, then the would-interrupt rule set.
	command, _ := args["command"].(string)
	var (
		p    Parsed
		risk Risk
	)
	if toolName == "shell.run" && strings.TrimSpace(command) != "" {
		p = Parse(command)
		risk = Assess(p)
	}

	// The safeagents lane (card t_e3cd4381): request-only detectors over
	// EVERY tool call. In the log-only soak a hit is recorded as a
	// WOULD-BLOCK and the call proceeds exactly as it would have.
	call := Call{Tool: toolName, Args: args}
	prov := NewProvenance(conversation)
	findings := DetectCall(call, prov)
	if len(findings) > 0 {
		if !live.DetectorsEnforce() {
			g.audit.Append(g.row(rowInput{
				mode: g.mode(), origin: origin, session: session, tool: toolName,
				command: callText(call), rules: reasonsOf(findings),
				findings: findings, monitor: true, judge: JudgeNotConsulted,
				outcome:   Outcome{Verdict: MonitorVerdict, Step: MonitorStep, Reason: detectorSummary(findings)},
				decidedBy: "monitor", latency: time.Since(start),
			}))
		} else {
			return g.detectorLane(ctx, live, prov, call, findings, risk, start)
		}
	}

	if !risk.WouldInterrupt {
		return "", nil // benign traffic: no review, no calls, no rows
	}
	sanitized, redacted := sanitize(p.Canonical)
	targets := TargetContentHash(p.Segments)
	if p.ParseErr != nil {
		// unparsable static form: review the raw shape minus
		// hygiene; the parse failure is itself a risk reason
		sanitized, redacted = sanitize(command)
	}
	key := g.cache.Key(sanitized, targets, live.SmartPolicy)
	background := origin == originScheduled || origin == originSubagent

	if rec, ok := g.cache.Lookup(key); ok {
		// Q8: a recorded APPROVE/DENY replays with zero cloud calls;
		// the replay binds to the same content hashes, so a mutated
		// script re-asks. (Settlements above never store
		// escalations - a background refusal must not poison an
		// interactive ask.) The replay carries the recorded decision's
		// own stamp: a card-approved resolution replays as
		// "escalated -> approved", never as an auto verdict.
		g.audit.Append(g.row(rowInput{
			mode: g.mode(), origin: origin, session: session,
			command: sanitized, rules: risk.Reasons, findings: risk.Findings, rec: rec,
			key: key, cached: true, redacted: redacted,
			executed: rec.outcome.Verdict == VerdictApprove,
			latency:  time.Since(start),
		}))
		return rec.stamp, outcomeError(rec.outcome)
	}

	// The deciding pass. The reviewer is clientless (one forward pass,
	// no cached client), so it is rebuilt per decision from the live
	// config - a settings flip to the mode, policy, or the model slot
	// (host/key included) applies at ask time (card t_34cd1921).
	var (
		outcome   Outcome
		answers   Answers
		reviewErr error
		fromCard  bool
		followUp  string // the card's typed guidance when follow-up answered
	)
	switch g.mode() {
	case ModeManual:
		outcome = Outcome{VerdictEscalate, "manual_mode",
			"manual approval: shell command flagged (" + strings.Join(risk.Reasons, ", ") + ")"}
	case ModeSmart:
		reviewer := Reviewer{Host: live.HTTPHost, APIKey: live.APIKey, Model: live.Model}
		answers, reviewErr = reviewer.Review(ctx, sanitized,
			strings.Join(risk.Reasons, ", "), live.SmartPolicy, live.SmartPolicy != "")
		if reviewErr != nil {
			// Q4/Q12: unreachable, timeout, malformed = fail-closed
			outcome = Outcome{VerdictEscalate, "reviewer_failure",
				fmt.Sprintf("reviewer unreachable (%v) - escalating, nothing runs on a dead review", reviewErr)}
		} else {
			answers.HasPolicy = live.SmartPolicy != ""
			outcome = Ladder(answers)
		}
	default: // defensively off - a mounted gate never answers here
		return "", nil
	}

	if outcome.Verdict == VerdictEscalate {
		if background {
			// Q11: workers never block on a human. Clean tool error
			// the worker routes around; verdict rides audit +
			// notification. Not cached: a background refusal must
			// not replay against an interactive ask. No stamp: no
			// card, no resolution - the tool error text is the
			// visible truth on the record.
			g.notifyEscalation(session, origin, sanitized, outcome)
			g.audit.Append(g.row(rowInput{
				mode: g.mode(), origin: origin, session: session,
				command: sanitized, rules: risk.Reasons, findings: risk.Findings, answers: answers,
				rawVerdict: answers.Verdict, outcome: outcome, key: key,
				redacted: redacted, recErr: reviewErr,
				latency: time.Since(start),
			}))
			if reviewErr != nil {
				return "", fmt.Errorf("approval escalated, command not executed (reviewer unreachable: %v)", reviewErr)
			}
			return "", fmt.Errorf("approval escalated, command not executed (%s): %s", outcome.Step, outcome.Reason)
		}
		// Q13: the interactive escalation rides the EXISTING verdict
		// card machinery - the card answers with approve, deny, or
		// typed follow-up guidance (card t_63204dfd). Expiry =
		// fail-closed. The card's RESOLUTION (or fail-closed outcome)
		// stamps this call's record - the permanent gate line (card
		// t_1b3e2a8b).
		var cardErr error
		outcome, followUp, cardErr = g.humanCard(ctx, sanitized, outcome)
		if cardErr != nil {
			g.audit.Append(g.row(rowInput{
				mode: g.mode(), origin: origin, session: session,
				command: sanitized, rules: risk.Reasons, findings: risk.Findings, answers: answers,
				rawVerdict: answers.Verdict, outcome: outcome, key: key,
				redacted: redacted, recErr: reviewErr, cardErr: cardErr,
				latency: time.Since(start),
			}))
			return g.stamp(outcome, true), fmt.Errorf("approval escalated, command not executed (%s): %s", outcome.Step, outcome.Reason)
		}
		fromCard = true
	}

	stamp := g.stamp(outcome, fromCard)
	if fromCard && followUp != "" && g.mode() == ModeSmart {
		// the follow-up resolution's stamp carries the guidance (card
		// t_63204dfd): "escalated -> answered" plus the typed text.
		// Smart-only: manual mode stamps nothing (the t_1b3e2a8b law -
		// a manual ask is the human's own verdict), the guidance still
		// rides the tool error.
		stamp = GateEscAnswered + ": " + truncTo(followUp, stampGuidanceMax)
	}
	g.audit.Append(g.row(rowInput{
		mode: g.mode(), origin: origin, session: session,
		command: sanitized, rules: risk.Reasons, findings: risk.Findings, answers: answers,
		rawVerdict: answers.Verdict, outcome: outcome, key: key,
		redacted: redacted, recErr: reviewErr, fromCard: fromCard,
		executed: outcome.Verdict == VerdictApprove,
		latency:  time.Since(start),
	}))
	if (outcome.Verdict == VerdictApprove || outcome.Verdict == VerdictDeny) &&
		outcome.Step != "human_card_followup" {
		// Only settlements cache: approve/deny are the stable answers;
		// an escalation (model or human refusal) is situational and
		// never replays in place of a fresh decision. A follow-up
		// answer is deny-with-directions (card t_63204dfd) - equally
		// situational, never a standing verdict for the command. The
		// stamp rides the record: the replay carries the decision's
		// own line.
		g.cache.Store(key, record{outcome: outcome, answers: answers, modelUsed: live.Model, stamp: stamp})
	}
	return stamp, outcomeError(outcome)
}

// mode returns the live config's normalized mode (read-through: a
// settings flip applies at the next decision, card t_34cd1921).
func (g *Gate) mode() string { return g.config().NormalizedMode() }

// outcomeError maps a resolution to the tool error (nil runs). The
// card_expired shape is deliberately DISTINCT (card t_304afb4e): the
// QA pass read every escalation as one flat "approval escalated"
// prefix, so an expired card - a fail-closed non-decision the agent
// can act on by simply running the command again - drowned in the
// same vocabulary as refusals. The prefix names the expiry, the
// fail-closed law, and the act-on-it path.
func outcomeError(o Outcome) error {
	switch o.Verdict {
	case VerdictApprove:
		return nil
	case VerdictDeny:
		return fmt.Errorf("approval denied, command not executed (%s): %s", o.Step, o.Reason)
	default:
		if o.Step == "card_expired" {
			return fmt.Errorf("approval card expired - command not executed (fail-closed, nothing ran): re-run the command to raise a fresh approval card")
		}
		return fmt.Errorf("approval escalated, command not executed (%s): %s", o.Step, o.Reason)
	}
}

// humanCard rides the verdicts seam (Q13 - no new webui UI): the
// card's title = command, body = the ladder step + why, options
// approve/deny (+ typed guidance, card t_63204dfd). Approve and deny
// are decisions; a typed follow-up answer denies the command and hands
// the agent the text as its resolution guidance (the tool error
// carries "follow-up guidance: <text>"); expiry, skip and a dead
// verdicts seam are fail-closed non-decisions (executed = false,
// never cached). The second return is the follow-up guidance (empty
// on every other resolution) for the "answered" stamp.
func (g *Gate) humanCard(ctx context.Context, command string, o Outcome) (Outcome, string, error) {
	hub := g.verdicts()
	if hub == nil {
		return Outcome{VerdictEscalate, "card_failure", "no verdicts seam mounted"}, "", fmt.Errorf("no verdicts seam mounted")
	}
	title := command
	if len(title) > 120 {
		title = title[:117] + "…"
	}
	card := core.QuestionPrompt{
		SessionID: agent.SessionIDFromContext(ctx),
		Title:     title,
		Body:      "approval gate - " + o.Step + ": " + o.Reason,
		Options: []core.QuestionOption{
			{Label: "approve", Description: "run the command"},
			{Label: "deny", Description: "refuse; the agent is told the command did not run"},
		},
		AllowText: true, // the follow-up surface (card t_63204dfd)
		TimeoutS:  0,    // the hub's 20-minute default (Q13)
	}
	verdict, err := hub.Ask(ctx, card)
	if err != nil {
		// run stopped / the seam closed - fail-closed
		return Outcome{VerdictEscalate, "card_failure", err.Error()}, "", err
	}
	switch verdict.Kind() {
	case "expired":
		return Outcome{VerdictEscalate, "card_expired", "the approval card expired - fail-closed, nothing ran"}, "",
			fmt.Errorf("card expired")
	case "skip":
		return Outcome{VerdictEscalate, "card_skipped", "the approval card was skipped - treated as non-execution"}, "",
			fmt.Errorf("card skipped")
	default:
		// The card's third way (card t_63204dfd): ANSWERED - typed
		// guidance wins over any option pick, the command never runs.
		if msg := strings.TrimSpace(verdict.Text); msg != "" {
			if len(msg) > followUpMax {
				msg = truncTo(msg, followUpMax)
			}
			return Outcome{VerdictDeny, "human_card_followup", followUpReasonPrefix + msg}, msg, nil
		}
		if verdict.OptionIndex == 1 && verdict.Option == "approve" {
			return Outcome{VerdictApprove, "human_card", "the user approved"}, "", nil
		}
		return Outcome{VerdictDeny, "human_card", "the user refused"}, "", nil
	}
}

// notifyEscalation emits the background-run escalation (Q11).Nil-safe:
// a host without the notifications module still gates.
func (g *Gate) notifyEscalation(session, origin, command string, o Outcome) {
	prov := g.notifications()
	if prov == nil {
		return
	}
	kind := "cron"
	if origin == originSubagent {
		kind = "chat"
	}
	_, _ = prov.Emit(core.Notice{
		Kind:     kind,
		SourceID: session,
		Title:    "approval escalated (" + o.Step + "): " + truncTo(command, 80),
	})
}

func truncTo(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// detectorLane is the safeagents lane's escalation path (card
// t_e3cd4381): a HIGH finding blocks on its own (the cheap regex
// catches the obvious), a suspicious one passes the evidence-only
// judge and then the human - the judge/human see only the gray zone.
// The judge can only ADD a block: its finding rides the record and the
// card, never lifts a deterministic one. The legacy shell rules ride
// along as EVIDENCE (the card and the row name them) but their routing
// stays theirs: this lane was reached because a tiered finding fired.
//
// Nothing here is cached: the lane is deterministic and costs no model
// call on the block path, and a judge-augmented escalation is
// situational (the legacy lane's cache law, unchanged).
func (g *Gate) detectorLane(ctx context.Context, live Config, prov Provenance, call Call, findings []Finding, legacy Risk, start time.Time) (string, error) {
	origin := core.RunOriginFromContext(ctx)
	session := agent.SessionIDFromContext(ctx)
	sanitized, redacted := sanitize(callText(call))
	background := origin == originScheduled || origin == originSubagent

	var (
		outcome    Outcome
		judge      = JudgeNotConsulted
		judgeBlock float64
		reviewErr  error
		noTask     bool
	)

	// every deterministic finding is evidence; only the TIERED ones route
	evidence := append(append([]Finding(nil), findings...), legacy.Findings...)
	judged := evidence
	blockers := tieredAtLeast(evidence, SeverityHigh)
	if len(blockers) == 0 {
		// SUSPICIOUS: the evidence-only judge reads the human's task,
		// the call and the findings - never the agent's reasoning.
		// A run with no human turn at all (a wake, a synthetic fire)
		// has nothing to judge accountability AGAINST: the judge stays
		// unconsulted rather than answering from half the record.
		task := prov.Task()
		switch {
		case task == "":
			noTask = true
		default:
			answer, err := Judge{Host: live.HTTPHost, APIKey: live.APIKey, Model: live.Model}.
				Judge(ctx, task, call, evidence)
			judge = answer.State(err)
			judgeBlock = answer.Block
			switch {
			case err == nil:
				judged = ratchet(evidence, answer)
			case live.JudgeFailClosed:
				// configured closed: an unreachable judge is a block
				judged = ratchetFailure(evidence, err)
				blockers = tieredAtLeast(judged, SeverityHigh)
				reviewErr = err
			default:
				// the default failure law: fail OPEN - the judge adds
				// nothing and the human still decides
				reviewErr = err
			}
		}
	}

	if len(blockers) > 0 {
		outcome = Outcome{VerdictDeny, DetectorBlockStep, detectorSummary(blockers)}
	} else {
		reason := detectorSummary(evidence)
		switch {
		case judge == JudgeBlocked:
			reason += "; the evidence-only judge also blocked it (block " + trimFloat(judgeBlock) + ")"
		case judge == JudgeUnavailable:
			reason += "; the evidence-only judge was unreachable (fail-open: this card decides)"
		case noTask:
			reason += "; no human task in this run, so the evidence-only judge had nothing to judge accountability against"
		}
		if len(legacy.Reasons) > 0 {
			reason += "; the shell rule set also flagged: " + strings.Join(legacy.Reasons, ", ")
		}
		outcome = Outcome{VerdictEscalate, DetectorSuspectStep, reason}
	}

	if outcome.Verdict == VerdictDeny {
		g.audit.Append(g.row(rowInput{
			mode: g.mode(), origin: origin, session: session, tool: call.Tool,
			command: sanitized, rules: reasonsOf(judged), findings: judged,
			judge: judge, judgeBlock: judgeBlock, recErr: reviewErr,
			outcome: outcome, redacted: redacted, decidedBy: "detector",
			latency: time.Since(start),
		}))
		return "", outcomeError(outcome)
	}
	if background {
		// Q11: workers never block on a human - the refusal rides the
		// tool error, the notification reaches the operator
		g.notifyEscalation(session, origin, sanitized, outcome)
		g.audit.Append(g.row(rowInput{
			mode: g.mode(), origin: origin, session: session, tool: call.Tool,
			command: sanitized, rules: reasonsOf(judged), findings: judged,
			judge: judge, judgeBlock: judgeBlock, recErr: reviewErr,
			outcome: outcome, redacted: redacted, decidedBy: "detector",
			latency: time.Since(start),
		}))
		return "", fmt.Errorf("approval escalated, command not executed (%s): %s", outcome.Step, outcome.Reason)
	}
	resolved, followUp, cardErr := g.humanCard(ctx, sanitized, outcome)
	if cardErr != nil {
		g.audit.Append(g.row(rowInput{
			mode: g.mode(), origin: origin, session: session, tool: call.Tool,
			command: sanitized, rules: reasonsOf(judged), findings: judged,
			judge: judge, judgeBlock: judgeBlock, recErr: reviewErr, cardErr: cardErr,
			outcome: resolved, redacted: redacted, decidedBy: "human_card",
			latency: time.Since(start),
		}))
		return g.stamp(resolved, true), fmt.Errorf("approval escalated, command not executed (%s): %s", resolved.Step, resolved.Reason)
	}
	stamp := g.stamp(resolved, true)
	if followUp != "" && g.mode() == ModeSmart {
		stamp = GateEscAnswered + ": " + truncTo(followUp, stampGuidanceMax)
	}
	g.audit.Append(g.row(rowInput{
		mode: g.mode(), origin: origin, session: session, tool: call.Tool,
		command: sanitized, rules: reasonsOf(judged), findings: judged,
		judge: judge, judgeBlock: judgeBlock, recErr: reviewErr,
		outcome: resolved, redacted: redacted, decidedBy: "human_card", fromCard: true,
		executed: resolved.Verdict == VerdictApprove,
		latency:  time.Since(start),
	}))
	return stamp, outcomeError(resolved)
}

// Step names the safeagents lane stamps on its outcomes (the audit
// row's step column and the escalation card's body).
const (
	DetectorBlockStep   = "detector_high_severity"
	DetectorSuspectStep = "detector_suspicious"
	// MonitorVerdict / MonitorStep are the log-only dry run's record: a
	// WOULD-BLOCK measurement, never a decision (nothing about the call
	// changed, the row exists for the soak's false-positive count).
	MonitorVerdict = "FLAG"
	MonitorStep    = "monitor_would_block"
)

// tieredAtLeast returns the findings of the safeagents set at or above
// one tier (the legacy lane's findings never route here: Tiered=false).
func tieredAtLeast(findings []Finding, min Severity) []Finding {
	var out []Finding
	for _, f := range findings {
		if f.Tiered && severityRank(f.Severity) >= severityRank(min) {
			out = append(out, f)
		}
	}
	return out
}

// reasonsOf lists the detector names (the audit row's rules column,
// deduped and sorted: the legacy row shape, extended to the new set).
func reasonsOf(findings []Finding) []string {
	seen := make(map[string]bool, len(findings))
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		if seen[f.Detector] {
			continue
		}
		seen[f.Detector] = true
		out = append(out, f.Detector)
	}
	sort.Strings(out)
	return out
}

// detectorSummary renders the findings as one line for the audit row
// and the escalation card (the human reads the evidence, not a code).
// Each part carries its SEVERITY first: the card is the approval
// surface, so the tier renders as plain text - no chip redesign needed.
func detectorSummary(findings []Finding) string {
	var parts []string
	for _, f := range findings {
		part := string(f.Severity) + " " + f.Detector
		if f.Detail != "" {
			part += ": " + f.Detail
		}
		parts = append(parts, part)
	}
	return truncTo(strings.Join(parts, "; "), 300)
}

// callText renders a call for the audit row and the escalation card:
// the tool and its decoded arguments, capped like a command.
func callText(call Call) string {
	return truncTo(call.Tool+" "+call.Text(), maxCommandChars)
}

// trimFloat renders a probability with two decimals (the audit row and
// the card carry a readable number, never a float noise tail).
func trimFloat(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
}

// rowInput assembles the audit row; each pass fills what it has.
type rowInput struct {
	mode, origin, session string
	tool                  string // empty = shell.run (the legacy lane's only tool)
	command               string
	rules                 []string
	findings              []Finding
	monitor               bool
	judge                 string
	judgeBlock            float64
	decidedBy             string
	cached                bool
	redacted              bool
	key                   string
	answers               Answers
	rawVerdict            string
	outcome               Outcome
	rec                   record
	recErr                error
	cardErr               error
	fromCard              bool
	executed              bool
	latency               time.Duration
}

// row renders the audit record (Q6: parsed command, all six answers,
// the step, policy hash, mode). The model and policy presence read the
// LIVE config: the row is the receipt for the mode/model flips (card
// t_34cd1921), not for the boot snapshot.
func (g *Gate) row(in rowInput) AuditRow {
	answers, rawVerdict, outcome := in.answers, in.rawVerdict, in.outcome
	if in.cached {
		answers, outcome = in.rec.answers, in.rec.outcome
	}
	live := g.config()
	tool := in.tool
	if tool == "" {
		tool = "shell.run"
	}
	decidedBy := "reviewer"
	switch {
	case in.decidedBy != "":
		decidedBy = in.decidedBy
	case in.cached:
		decidedBy = "cache"
	case in.fromCard:
		decidedBy = "human_card"
	case in.mode == ModeManual:
		decidedBy = "manual"
	}
	row := AuditRow{
		Mode:           in.mode,
		Origin:         in.origin,
		Session:        in.session,
		Tool:           tool,
		Command:        truncTo(in.command, 600),
		Rules:          in.rules,
		Findings:       in.findings,
		Monitor:        in.monitor,
		Judge:          in.judge,
		JudgeBlock:     in.judgeBlock,
		Verdict:        outcome.Verdict,
		RawVerdict:     rawVerdict,
		Step:           outcome.Step,
		Reason:         outcome.Reason,
		Confidence:     answers.Confidence,
		PolicyAllows:   answers.PolicyAllows,
		BlastRadius:    answers.BlastRadius,
		SelfAdvocating: answers.SelfAdvocating,
		ReadsSecrets:   answers.ReadsSecrets,
		SendsOutbound:  answers.SendsOutbound,
		Model:          live.Model,
		HasPolicy:      live.SmartPolicy != "",
		PolicyHash:     policyHash(live.SmartPolicy),
		DecidedBy:      decidedBy,
		CacheKey:       in.key,
		Cached:         in.cached,
		Redacted:       in.redacted,
		Executed:       in.executed,
		Truncated:      len(in.command) > maxCommandChars,
		LatencyMs:      in.latency.Milliseconds(),
	}
	if in.mode == ModeManual {
		row.Model = ""
	}
	if in.cached {
		row.CachedIn = in.rec.modelUsed
	}
	if in.recErr != nil {
		row.Error = in.recErr.Error()
	}
	if in.cardErr != nil {
		row.Error = in.cardErr.Error()
	}
	row.PolicyFP = g.questionsFP
	row.LadderVersion = ladderVersion
	return row
}
