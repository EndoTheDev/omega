package approval

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
)

// The gate (design Q3/Q11/Q13): one gate, one policy text - agent
// sessions AND cron workers consult the same object. The Gate is the
// approval module's core.ApprovalProvider implementation.
type Gate struct {
	cfg Config

	verdicts      func() core.VerdictsProvider      // read live: the seam stays the source of truth
	notifications func() core.NotificationsProvider // read live; nil-safe
	logger        core.LoggerProvider
	reviewer      Reviewer
	audit         *Audit
	cache         *Cache
	questionsFP   string
	policyFP      string
}

// NewGate builds the gate. verdicts and notifications are closures
// over the live Context (a seam rewire is visible at ask time); the
// logger may be nil. Smart mode without a reviewer host/model still
// mounts - every gated command then fails closed into escalation,
// honest about a dead review rather than silently open.
func NewGate(cfg Config, verdicts func() core.VerdictsProvider, notifications func() core.NotificationsProvider, logger core.LoggerProvider) *Gate {
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

// Gate implements core.ApprovalProvider. Contract: nil = execute,
// non-nil = the tool result refusing execution. Concurrency-safe:
// the loop runs a turn's tool calls in parallel.
func (g *Gate) Gate(ctx context.Context, toolName string, args map[string]any) error {
	start := time.Now()
	if toolName != "shell.run" {
		return nil // Q1: only shell commands, ever
	}
	command, _ := args["command"].(string)
	if strings.TrimSpace(command) == "" {
		return nil // the tool itself refuses an empty command; not ours
	}

	// Q7: parse + static expansion. The raw string never travels.
	p := Parse(command)
	risk := Assess(p)
	if !risk.WouldInterrupt {
		return nil // benign traffic: no review, no calls, no rows
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
	background := origin == core.RunOriginCron || origin == core.RunOriginDelegate

	if rec, ok := g.cache.Lookup(key); ok {
		// Q8: a recorded APPROVE/DENY replays with zero cloud calls;
		// the replay binds to the same content hashes, so a mutated
		// script re-asks. (Settlements above never store
		// escalations - a background refusal must not poison an
		// interactive ask.)
		g.audit.Append(g.row(rowInput{
			mode: g.mode(), origin: origin, session: session,
			command: sanitized, rules: risk.Reasons, rec: rec,
			key: key, cached: true, redacted: redacted,
			executed: rec.outcome.Verdict == VerdictApprove,
			latency:  time.Since(start),
		}))
		return outcomeError(rec.outcome)
	}

	// The deciding pass.
	var (
		outcome   Outcome
		answers   Answers
		reviewErr error
		fromCard  bool
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
		return nil
	}

	if outcome.Verdict == VerdictEscalate {
		if background {
			// Q11: workers never block on a human. Clean tool error
			// the worker routes around; verdict rides audit +
			// notification. Not cached: a background refusal must
			// not replay against an interactive ask.
			g.notifyEscalation(session, origin, sanitized, outcome)
			g.audit.Append(g.row(rowInput{
				mode: g.mode(), origin: origin, session: session,
				command: sanitized, rules: risk.Reasons, answers: answers,
				rawVerdict: answers.Verdict, outcome: outcome, key: key,
				redacted: redacted, recErr: reviewErr,
				latency: time.Since(start),
			}))
			if reviewErr != nil {
				return fmt.Errorf("approval escalated, command not executed (reviewer unreachable: %v)", reviewErr)
			}
			return fmt.Errorf("approval escalated, command not executed (%s): %s", outcome.Step, outcome.Reason)
		}
		// Q13: the interactive escalation rides the EXISTING verdict
		// card machinery - a 2-option approve/deny card, title =
		// command, body = the ladder step + why. Expiry = fail-closed.
		var cardErr error
		outcome, cardErr = g.humanCard(ctx, sanitized, outcome)
		if cardErr != nil {
			g.audit.Append(g.row(rowInput{
				mode: g.mode(), origin: origin, session: session,
				command: sanitized, rules: risk.Reasons, answers: answers,
				rawVerdict: answers.Verdict, outcome: outcome, key: key,
				redacted: redacted, recErr: reviewErr, cardErr: cardErr,
				latency: time.Since(start),
			}))
			return fmt.Errorf("approval escalated, command not executed (%s): %s", outcome.Step, outcome.Reason)
		}
		fromCard = true
	}

	g.audit.Append(g.row(rowInput{
		mode: g.mode(), origin: origin, session: session,
		command: sanitized, rules: risk.Reasons, answers: answers,
		rawVerdict: answers.Verdict, outcome: outcome, key: key,
		redacted: redacted, recErr: reviewErr, fromCard: fromCard,
		executed: outcome.Verdict == VerdictApprove,
		latency:  time.Since(start),
	}))
	if outcome.Verdict == VerdictApprove || outcome.Verdict == VerdictDeny {
		// Only settlements cache: approve/deny are the stable answers;
		// an escalation (model or human refusal) is situational and
		// never replays in place of a fresh decision.
		g.cache.Store(key, record{outcome: outcome, answers: answers, modelUsed: g.cfg.Model})
	}
	return outcomeError(outcome)
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

// humanCard rides the verdicts seam (Q13 - no new webui UI): a
// 2-option approve/deny card, title = command, body = ladder step +
// why. Approve and deny are decisions; expiry, skip and a dead
// verdicts seam are fail-closed non-decisions (executed = false,
// never cached).
func (g *Gate) humanCard(ctx context.Context, command string, o Outcome) (Outcome, error) {
	hub := g.verdicts()
	if hub == nil {
		return Outcome{VerdictEscalate, "card_failure", "no verdicts seam mounted"}, fmt.Errorf("no verdicts seam mounted")
	}
	title := command
	if len(title) > 120 {
		title = title[:117] + "…"
	}
	card := core.VerdictPrompt{
		SessionID: agent.SessionIDFromContext(ctx),
		Title:     title,
		Body:      "approval gate - " + o.Step + ": " + o.Reason,
		Options: []core.VerdictOption{
			{Label: "approve", Description: "run the command"},
			{Label: "deny", Description: "refuse; the agent is told the command did not run"},
		},
		TimeoutS: 0, // the hub's 20-minute default (Q13)
	}
	verdict, err := hub.Ask(ctx, card)
	if err != nil {
		// run stopped / the seam closed - fail-closed
		return Outcome{VerdictEscalate, "card_failure", err.Error()}, err
	}
	switch verdict.Kind() {
	case "expired":
		return Outcome{VerdictEscalate, "card_expired", "the approval card expired - fail-closed, nothing ran"},
			fmt.Errorf("card expired")
	case "skip":
		return Outcome{VerdictEscalate, "card_skipped", "the approval card was skipped - treated as non-execution"},
			fmt.Errorf("card skipped")
	default:
		if verdict.OptionIndex == 1 && verdict.Option == "approve" {
			return Outcome{VerdictApprove, "human_card", "the user approved"}, nil
		}
		return Outcome{VerdictDeny, "human_card", "the user refused"}, nil
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
	if origin == core.RunOriginDelegate {
		kind = "chat"
	}
	_, _ = prov.Emit(core.Notification{
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
