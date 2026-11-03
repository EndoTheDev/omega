package approval

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
)

// The gate (design Q3/Q11/Q13): one gate, one policy text - agent
// sessions AND cron workers consult the same object. The Gate is the
// approval module's agent.ApprovalProvider implementation.
type Gate struct {
	cfg Config

	// verdicts/notifications are the consumer-side faces (part 3 seam
	// constitution): local interfaces with just what the gate calls,
	// resolved live from the universal registry, nil-safe by the
	// classic law. The owner capsules never know this gate exists.
	verdicts      func() verdictHub         // read live: the service stays the source of truth
	notifications func() finishedThings     // read live; nil-safe
	logger        agent.LoggerProvider
	reviewer      Reviewer
	audit         *Audit
	cache         *Cache
	questionsFP   string
	policyFP      string
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
// honest about a dead review rather than silently open.
func NewGate(cfg Config, verdicts func() verdictHub, notifications func() finishedThings, logger agent.LoggerProvider) *Gate {
	return &Gate{
		cfg:           cfg,
		verdicts:      verdicts,
		notifications: notifications,
		logger:        logger,
		reviewer: Reviewer{
			Host:   cfg.HTTPHost,
			APIKey: cfg.APIKey,
			Model:  cfg.Model,
		},
		audit:       NewAudit(cfg.Home),
		cache:       NewCache(),
		questionsFP: questionsFingerprint(),
		policyFP:    policyHash(cfg.SmartPolicy),
	}
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
// no machine decision). Concurrency-safe: the loop runs a turn's
// tool calls in parallel.
func (g *Gate) Gate(ctx context.Context, toolName string, args map[string]any) (string, error) {
	start := time.Now()
	if toolName != "shell.run" {
		return "", nil // Q1: only shell commands, ever
	}
	command, _ := args["command"].(string)
	if strings.TrimSpace(command) == "" {
		return "", nil // the tool itself refuses an empty command; not ours
	}

	// Q7: parse + static expansion. The raw string never travels.
	p := Parse(command)
	risk := Assess(p)
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
	key := g.cache.Key(sanitized, targets, g.cfg.SmartPolicy)
	origin := core.RunOriginFromContext(ctx)
	session := agent.SessionIDFromContext(ctx)
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
			command: sanitized, rules: risk.Reasons, rec: rec,
			key: key, cached: true, redacted: redacted,
			executed: rec.outcome.Verdict == VerdictApprove,
			latency:  time.Since(start),
		}))
		return rec.stamp, outcomeError(rec.outcome)
	}

	// The deciding pass.
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
		answers, reviewErr = g.reviewer.Review(ctx, sanitized,
			strings.Join(risk.Reasons, ", "), g.cfg.SmartPolicy, g.cfg.SmartPolicy != "")
		if reviewErr != nil {
			// Q4/Q12: unreachable, timeout, malformed = fail-closed
			outcome = Outcome{VerdictEscalate, "reviewer_failure",
				fmt.Sprintf("reviewer unreachable (%v) - escalating, nothing runs on a dead review", reviewErr)}
		} else {
			answers.HasPolicy = g.cfg.SmartPolicy != ""
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
				command: sanitized, rules: risk.Reasons, answers: answers,
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
				command: sanitized, rules: risk.Reasons, answers: answers,
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
		command: sanitized, rules: risk.Reasons, answers: answers,
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
		g.cache.Store(key, record{outcome: outcome, answers: answers, modelUsed: g.cfg.Model, stamp: stamp})
	}
	return stamp, outcomeError(outcome)
}

// mode returns the config's normalized mode.
func (g *Gate) mode() string { return g.cfg.NormalizedMode() }

// outcomeError maps a resolution to the tool error (nil runs).
func outcomeError(o Outcome) error {
	switch o.Verdict {
	case VerdictApprove:
		return nil
	case VerdictDeny:
		return fmt.Errorf("approval denied, command not executed (%s): %s", o.Step, o.Reason)
	default:
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

// rowInput assembles the audit row; each pass fills what it has.
type rowInput struct {
	mode, origin, session string
	command               string
	rules                 []string
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
// the step, policy hash, mode).
func (g *Gate) row(in rowInput) AuditRow {
	answers, rawVerdict, outcome := in.answers, in.rawVerdict, in.outcome
	if in.cached {
		answers, outcome = in.rec.answers, in.rec.outcome
	}
	decidedBy := "reviewer"
	switch {
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
		Tool:           "shell.run",
		Command:        truncTo(in.command, 600),
		Rules:          in.rules,
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
		Model:          g.cfg.Model,
		HasPolicy:      g.cfg.SmartPolicy != "",
		PolicyHash:     g.policyFP,
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
