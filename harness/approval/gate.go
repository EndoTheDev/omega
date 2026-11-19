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

// Gate is the approval gate: every tool call passes through it.
type Gate struct {
	cfgMu sync.RWMutex
	cfg   Config

	// verdicts/notifications are consumer-side faces: local interfaces with just what the
	// gate calls, resolved live from the registry and nil-safe. The owner capsules never
	// know this gate exists.
	verdicts      func() verdictHub     // read live: the service stays the source of truth
	notifications func() finishedThings // read live; nil-safe
	logger        agent.LoggerProvider
	audit         *Audit
	cache         *Cache
	questionsFP   string
}

// verdictHub is the consumer-side face of the verdict capsules question cards.
type verdictHub interface {
	// Ask registers the card and BLOCKS for the answer (the capsule owns the expiry law).
	Ask(ctx context.Context, card core.QuestionPrompt) (core.QuestionAnswer, error)
	// Answer records the user's answer to one pending card; FIRST answer wins.
	Answer(sessionID, cardID string, v core.QuestionAnswer) (core.QuestionAnswer, error)
	Pending(sessionID string) []core.QuestionPrompt
}

// finishedThings is the consumer-side face of the finished-things store.
type finishedThings interface {
	Emit(n core.Notice) (core.Notice, error)
}

// The run-origin values the gate compares.
const (
	originScheduled = "cron"
	originSubagent  = "delegate"
)

// NewGate builds the gate. verdicts and notifications are closures over the live Context
// (a seam rewire is visible at ask time); the logger may be nil. Smart mode without a
// reviewer host/model still mounts - every gated command then fails closed into
// escalation, honest about a dead review rather than silently open.
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

// SetConfig replaces the gate's config at runtime.
func (g *Gate) SetConfig(cfg Config) {
	g.cfgMu.Lock()
	defer g.cfgMu.Unlock()
	g.cfg = cfg
}

// config returns one consistent copy of the live config.
func (g *Gate) config() Config {
	g.cfgMu.RLock()
	defer g.cfgMu.RUnlock()
	return g.cfg
}

// The gate-line vocabulary. The strings are the contract - no rewording.
const (
	GateAutoApproved = "smart gate: auto-approved"
	GateAutoDenied   = "smart gate: auto-denied"
	GateEscApproved  = "smart gate: escalated -> approved"
	GateEscDenied    = "smart gate: escalated -> denied - command skipped"
	GateEscExpired   = "smart gate: escalated -> expired"
	GateEscCancelled = "smart gate: escalated -> cancelled"
	// GateEscAnswered is the follow-up resolution.
	GateEscAnswered = "smart gate: escalated -> answered"
)

// followUpReasonPrefix marks a follow-up resolution's reason.
const followUpReasonPrefix = "follow-up guidance: "

// followUpMax is the guidance size cap.
const followUpMax = 2000
const stampGuidanceMax = 240

// stamp renders the transcript-facing decision record. An escalation that never resolved
// (a background refusal) has no resolution to record - empty line, the tool error text
// stays the visible truth. fromCard marks the card-settled paths: the RESOLUTION lands as
// the line, never the model's original advice.
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

// Gate implements agent.ApprovalProvider. Contract: nil error = execute, non-nil = the
// tool result refusing execution; the line is the decision record stamped onto the tool
// result (empty = no machine decision). Safe for concurrent calls.
func (g *Gate) Gate(ctx context.Context, toolName string, args map[string]any) (string, error) {
	return g.decide(ctx, toolName, args, nil)
}

// GateWithConversation is the agent.GateProvenance upgrade: the loop hands the run's
// conversation so the safeagents detectors can ask the provenance question - does the host
// this call reaches appear in the HUMAN's own words? Consulted the plain way there is no
// conversation, so nothing is sourced and every non-allowlisted host reads as novel.
func (g *Gate) GateWithConversation(ctx context.Context, toolName string, args map[string]any, conversation []core.Message) (string, error) {
	return g.decide(ctx, toolName, args, conversation)
}

// decide is the one decision path: the legacy shell lane (parse, reviewer, ladder) plus the
// safeagents lane (request-only detectors, severity tiers). The tiered lane decides first
// when it fires; the legacy lane decides everything it does not claim. Both end in the same
// escalation machinery: a card, a background refusal, or a stamped record.
func (g *Gate) decide(ctx context.Context, toolName string, args map[string]any, conversation []core.Message) (string, error) {
	start := time.Now()
	if g.mode() == ModeOff {
		return "", nil // an off gate decides nothing and logs nothing
	}
	live := g.config()
	origin := core.RunOriginFromContext(ctx)
	session := agent.SessionIDFromContext(ctx)

	// The legacy lane: shell commands only - parse plus the would-interrupt rule set.
	command, _ := args["command"].(string)
	var (
		p    Parsed
		risk Risk
	)
	if toolName == "shell.run" && strings.TrimSpace(command) != "" {
		p = Parse(command)
		risk = Assess(p)
	}

	// The safeagents lane: request-only detectors over EVERY tool call. In the log-only
	// soak a hit is recorded as a WOULD-BLOCK and the call proceeds untouched.
	call := Call{Tool: toolName, Args: args}
	prov := NewProvenance(conversation)
	findings := DetectCall(call, prov)
	if len(findings) > 0 {
		if !live.DetectorsEnforce() {
			// the row rides the same egress boundary as every other row this gate writes
			// (redact.go): a flagged call carrying a credential must not persist it.
			monitorText, monitorRedacted := sanitize(callText(call))
			g.audit.Append(g.row(rowInput{
				mode: g.mode(), origin: origin, session: session, tool: toolName,
				command: monitorText, rules: reasonsOf(findings),
				findings: findings, monitor: true, judge: JudgeNotConsulted,
				redacted:  monitorRedacted,
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
		sanitized, redacted = sanitize(command)
	}
	key := g.cache.Key(sanitized, targets, live.SmartPolicy)
	background := origin == originScheduled || origin == originSubagent

	if rec, ok := g.cache.Lookup(key); ok {
		// a recorded APPROVE/DENY replays with zero cloud calls. Settlements never store
		// escalations - a background refusal must not poison an interactive ask. The replay
		// carries the recorded decision's own stamp: a card-approved resolution replays as
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
			// workers never block on a human. Not cached: a background refusal must not
			// replay against an interactive ask.
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
		// the interactive escalation rides the EXISTING verdict card machinery.
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
		// Only settlements cache: approve/deny are the stable answers; an escalation (model or
		// human refusal) is situational and never replays in place of a fresh decision. A
		// follow-up answer is deny-with-directions - equally situational.
		g.cache.Store(key, record{outcome: outcome, answers: answers, modelUsed: live.Model, stamp: stamp})
	}
	return stamp, outcomeError(outcome)
}

// mode returns the live config's normalized mode.
func (g *Gate) mode() string { return g.config().NormalizedMode() }

// outcomeError maps a resolution to the tool error (nil runs).
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

// humanCard rides the verdicts seam. Approve and deny are decisions; a typed follow-up
// answer denies the command and hands the agent the text as its resolution guidance (the
// tool error carries "follow-up guidance: <text>"); expiry, skip and a dead verdicts seam
// are fail-closed non-decisions (executed = false, never cached).
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
		AllowText: true, // the follow-up surface
		TimeoutS:  0,    // the hub's 20-minute default
	}
	verdict, err := hub.Ask(ctx, card)
	if err != nil {
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
		// The card's third way: ANSWERED - typed guidance wins over any option pick and the
		// command never runs.
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

// notifyEscalation emits the background-run escalation. Nil-safe.
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

// detectorLane is the safeagents lane's escalation path: a HIGH finding blocks on its own,
// a suspicious one passes the evidence-only judge and then the human - the judge and human
// see only the gray zone. The judge can only ADD a block: its finding rides the record and
// the card, never lifts a deterministic one. The legacy shell rules ride along as EVIDENCE
// (the card and the row name them) but their routing stays theirs.
//
// Nothing here is cached: the block path costs no model call, and a judge-augmented
// escalation is situational.
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
		// SUSPICIOUS: the evidence-only judge reads the human's task, the call and the
		// findings - never the agent's reasoning. A run with no human turn at all (a wake, a
		// synthetic fire) has nothing to judge accountability AGAINST, so the judge stays
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
				// the default failure law: fail OPEN - the judge adds nothing and the human
				// still decides
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
		// workers never block on a human - the refusal rides the tool error, the notification
		// reaches the operator
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

// Step names the safeagents lane stamps on its outcomes (the audit row's step column and
// the escalation card's body).
const (
	DetectorBlockStep   = "detector_high_severity"
	DetectorSuspectStep = "detector_suspicious"
	// MonitorVerdict / MonitorStep are the log-only dry run's record: a WOULD-BLOCK
	// measurement, never a decision.
	MonitorVerdict = "FLAG"
	MonitorStep    = "monitor_would_block"
)

// tieredAtLeast returns the safeagents findings at or above one tier (the legacy lane's
// findings never route here: Tiered=false).
func tieredAtLeast(findings []Finding, min Severity) []Finding {
	var out []Finding
	for _, f := range findings {
		if f.Tiered && severityRank(f.Severity) >= severityRank(min) {
			out = append(out, f)
		}
	}
	return out
}

// reasonsOf lists the detector names for the audit row's rules column, deduped and sorted.
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

// detectorSummary renders the findings as one line for the audit row and the escalation
// card, each part carrying its SEVERITY first so the card needs no chip redesign.
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

// callText renders a call for the audit row and the escalation card, capped like a command.
func callText(call Call) string {
	return truncTo(call.Tool+" "+call.Text(), maxCommandChars)
}

// trimFloat renders a probability with two decimals, never a float noise tail.
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

// row renders the audit record.
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
