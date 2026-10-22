package verdict

import (
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/agent"
)

// verdictToolProvider contributes the request_verdict tool. The tool is
// the card issuance point and the hard pause: Run blocks on the hub
// until the user answers, the card expires, or the run's context dies.
type verdictToolProvider struct {
	hub agent.VerdictsProvider
}

// Tools implements agent.ToolProvider.
func (p *verdictToolProvider) Tools() map[string]agent.Tool {
	return map[string]agent.Tool{
		"request_verdict": {
			Description: "Ask the user a question and block until they answer (click or type), the card expires (default 20 minutes), or the turn is cancelled. Use it whenever a decision needs human input - approval, a pick between options, or free text. Returns the verdict as JSON: {verdict:\"answer\",option,option_index} | {verdict:\"skip\"} | {verdict:\"expired\"}. Never call this from a cron or delegated run; it only works in interactive turns.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":   map[string]any{"type": "string", "description": "Short question header, one line."},
					"body":    map[string]any{"type": "string", "description": "Optional context: one or two lines explaining what the answer decides."},
					"options": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Labeled pick list. Empty (with free_text) for an open question. Approval is the 2-option case (e.g. [\"yes\",\"no\"])."},
					"free_text": map[string]any{"type": "boolean", "description": "Also accept a free-text answer alongside the options. Defaults false."},
					"timeout_s": map[string]any{"type": "integer", "description": "Per-card override of the 20-minute default. The safe answer (deny / no pick) fires on expiry and the run is told the card expired."},
				},
				"required": []string{"title"},
			},
			Run: p.run,
		},
	}
}

// minTimeout floors a per-card override so a model error (0 or tiny)
// does not instantly expire a card the user never saw.
const minTimeout = 5

// run executes request_verdict: validates origin, parses args, asks the
// hub, and returns the verdict as the tool result string. A block here
// pauses the whole agent turn - the webui stream idles until the user
// answers, exactly the design's hard pause.
func (p *verdictToolProvider) run(ctx context.Context, args map[string]any) (string, error) {
	if p.hub == nil {
		return "", fmt.Errorf("request_verdict: the verdicts seam is not mounted on this host")
	}

	// Issuer gate (design Q7a): cron fires and delegate subagents never
	// hard-pause for a click. Unknown origin (tests, one-shot) is
	// treated as interactive.
	switch agent.RunOriginFromContext(ctx) {
	case agent.RunOriginCron, agent.RunOriginDelegate:
		return "", fmt.Errorf("request_verdict is not available in background turns (origin %q) - decide without the user or surface the question in the transcript",
			agent.RunOriginFromContext(ctx))
	}

	title, _ := args["title"].(string)
	title = strings.TrimSpace(title)
	if title == "" {
		return "", fmt.Errorf("request_verdict: title is required")
	}
	body, _ := args["body"].(string)
	options := argStringSlice(args, "options")
	freeText, _ := args["free_text"].(bool)
	timeoutS := 0
	if v, ok := args["timeout_s"].(float64); ok {
		timeoutS = int(v)
	}
	if timeoutS > 0 && timeoutS < minTimeout {
		timeoutS = minTimeout
	}

	// A pure pick list must have at least one option; an open question
	// (no options) requires free_text. Approval is just the 2-option
	// case, nothing special on the wire.
	if len(options) == 0 && !freeText {
		return "", fmt.Errorf("request_verdict: a card needs at least one option or free_text=true")
	}

	card := agent.VerdictPrompt{
		SessionID: agent.SessionIDFromContext(ctx),
		Title:     title,
		Body:      body,
		Options:   options,
		FreeText:  freeText,
		TimeoutS:  timeoutS,
	}
	verdict, err := p.hub.Ask(ctx, card)
	if err != nil {
		return "", fmt.Errorf("request_verdict: %w", err)
	}
	return resultMap(verdict), nil
}

// argStringSlice pulls a JSON array-of-strings out of the untyped tool
// args map. Non-strings in the array are skipped (the model sends
// strings; anything else is malformed and dropped, not fatal).
func argStringSlice(args map[string]any, key string) []string {
	raw, ok := args[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}