package verdict

import (
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
)

// verdictActions is the structured sub-action metadata for the verdict
// domain tool (one source of truth for the schema enum, the prompt,
// and the /tools inventory - same shape the delegate tool uses).
var verdictActions = []core.ToolAction{
	{Name: "request", Description: "Issue a choice card and block until the user answers (click or type), the card expires (default 20 minutes), or the turn is cancelled. Ask when you cannot proceed without the user: approval, a pick between options, or free text. Returns the verdict as JSON: {verdict:\"answer\",option,option_index} | {verdict:\"answer\",options,option_indexes} when multi | {verdict:\"skip\"} | {verdict:\"expired\"}."},
}

// verdictToolProvider contributes the verdict domain tool. The tool is
// the card issuance point and the hard pause: the request sub-action
// blocks on the hub until the user answers, the card expires, or the
// run's context dies.
type verdictToolProvider struct {
	hub core.VerdictsProvider
}

// Tools implements core.ToolProvider.
func (p *verdictToolProvider) Tools() map[string]core.Tool {
	return map[string]core.Tool{
		"verdict": {
			Description: "Pause the conversation and present a choice card when a decision materially affects the user - ask when the decision needs human input and you cannot proceed without them. Never call this from a cron or delegated run; it only works in interactive turns.",
			Actions:     verdictActions,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{"type": "string", "description": "request (issue a card and block for the answer)."},
					"title":  map[string]any{"type": "string", "description": "Short question header, one line (required for action request)."},
					"body":   map[string]any{"type": "string", "description": "Optional context: one or two lines explaining what the answer decides."},
					"options": map[string]any{"type": "array", "items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"label":       map[string]any{"type": "string", "description": "The row's pick label."},
							"description": map[string]any{"type": "string", "description": "Optional one-line explanation, rendered under the label."},
						},
						"required": []string{"label"},
					}, "description": "Labeled pick list, one row per option. Empty (with allow_text) for an open question. Approval is the 2-option case."},
					"multi":      map[string]any{"type": "boolean", "description": "Allow selecting several options; every selected index comes back in the answer. Defaults false."},
					"allow_text": map[string]any{"type": "boolean", "description": "Also offer a type-your-own-answer row. Accepts a typed answer on top of the options, or alone for an open question. Defaults false."},
					"timeout_s":  map[string]any{"type": "integer", "description": "Per-card override of the 20-minute default. The safe answer (deny / no pick) fires on expiry and the run is told the card expired."},
				},
				"required": []string{"action", "title"},
			},
			Run: p.run,
		},
	}
}

// minTimeout floors a per-card override so a model error (0 or tiny)
// does not instantly expire a card the user never saw.
const minTimeout = 5

// run executes the verdict domain tool. The only sub-action today is
// request: validate origin, parse args, ask the hub, and return the
// verdict as the tool result string. A block here pauses the whole
// agent turn - the webui stream idles until the user answers, exactly
// the design's hard pause.
func (p *verdictToolProvider) run(ctx context.Context, args map[string]any) (string, error) {
	if p.hub == nil {
		return "", fmt.Errorf("verdict: the verdicts seam is not mounted on this host")
	}
	action, _ := args["action"].(string)
	if action != "request" {
		return "", fmt.Errorf("action is required: one of request")
	}
	return p.runRequest(ctx, args)
}

// runRequest executes verdict.request: origin gate, card shape, and
// the blocked Ask.
func (p *verdictToolProvider) runRequest(ctx context.Context, args map[string]any) (string, error) {
	// Issuer gate (design Q7a): cron fires and delegate subagents never
	// hard-pause for a click. Unknown origin (tests, one-shot) is
	// treated as interactive.
	switch core.RunOriginFromContext(ctx) {
	case core.RunOriginCron, core.RunOriginDelegate:
		return "", fmt.Errorf("verdict.request is not available in background turns (origin %q) - decide without the user or surface the question in the transcript",
			core.RunOriginFromContext(ctx))
	}

	title, _ := args["title"].(string)
	title = strings.TrimSpace(title)
	if title == "" {
		return "", fmt.Errorf("verdict.request: title is required")
	}
	body, _ := args["body"].(string)
	options := argOptions(args, "options")
	multi, _ := args["multi"].(bool)
	allowText, _ := args["allow_text"].(bool)
	timeoutS := 0
	if v, ok := args["timeout_s"].(float64); ok {
		timeoutS = int(v)
	}
	if timeoutS > 0 && timeoutS < minTimeout {
		timeoutS = minTimeout
	}

	// A pure pick list must have at least one option; an open question
	// (no options) requires allow_text. Multi with fewer than two
	// options is pointless but harmless - the card just picks one
	// row at a time. Approval is the 2-option case, nothing special
	// on the wire.
	if len(options) == 0 && !allowText {
		return "", fmt.Errorf("verdict.request: a card needs at least one option or allow_text=true")
	}

	card := core.VerdictPrompt{
		SessionID: agent.SessionIDFromContext(ctx),
		Title:     title,
		Body:      body,
		Options:   options,
		Multi:     multi,
		AllowText: allowText,
		TimeoutS:  timeoutS,
	}
	verdict, err := p.hub.Ask(ctx, card)
	if err != nil {
		return "", fmt.Errorf("verdict.request: %w", err)
	}
	return resultMap(verdict), nil
}

// argOptions pulls a JSON array of {label, description?} objects (or
// plain strings for one-word picks) out of the untyped tool args map.
// Non-conforming entries are skipped (the model sends objects;
// anything else is malformed and dropped, not fatal).
func argOptions(args map[string]any, key string) []ai.VerdictOption {
	raw, ok := args[key].([]any)
	if !ok {
		return nil
	}
	out := make([]ai.VerdictOption, 0, len(raw))
	for _, v := range raw {
		switch o := v.(type) {
		case string:
			if s := strings.TrimSpace(o); s != "" {
				out = append(out, ai.VerdictOption{Label: s})
			}
		case map[string]any:
			label, _ := o["label"].(string)
			label = strings.TrimSpace(label)
			if label == "" {
				continue
			}
			desc, _ := o["description"].(string)
			out = append(out, ai.VerdictOption{Label: label, Description: strings.TrimSpace(desc)})
		}
	}
	return out
}
