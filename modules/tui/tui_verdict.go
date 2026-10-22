package tui

// Verdict cards v1 (card t_2ecf9c51): the TUI's typed answer path.
// While a run is blocked inside request_verdict, the typed input is an
// ANSWER, not a queued follow-up: "1"/"2"/... pick, "y"/"n" the first
// two options, "skip" records a dismiss, anything else is free text
// when the card allows it. The verdict module owns the /verdict command
// (idle answering + inspection); the shell owns this live path because
// Enter intercepts happen before any command dispatch.

import (
	"strconv"
	"strings"

	"github.com/EndoTheDev/omega/agent"
)

// tryAnswerVerdict handles the busy-Enter path. It returns handled=true
// when the input answered a pending card (or was consumed as a verdict
// attempt that failed - e.g. out-of-range option). handled=false means
// no pending card existed and the caller queues the input as normal.
func (m *model) tryAnswerVerdict(input string) (handed bool) {
	if m.extensions == nil || m.extensions.Verdicts == nil || m.sessionID == "" {
		return false
	}
	pending := m.extensions.Verdicts.Pending(m.sessionID)
	if len(pending) == 0 {
		return false
	}
	card := pending[0] // oldest pending, same rule the HTTP POST uses
	v, err := verdictAnswerFromInput(card, input)
	if err != nil {
		m.err = err.Error()
		return true // consumed as a verdict attempt; the error line says why
	}
	if _, err := m.extensions.Verdicts.Answer(m.sessionID, card.ID, v); err != nil {
		m.err = err.Error()
		return true
	}
	m.err = ""
	m.transcript += "\n" + m.theme.Info.Render("[verdict: "+tuiVerdictSummary(card, v)+"]") + "\n"
	m.refresh()
	return true
}

// verdictAnswerFromInput maps one typed line to a Verdict. The contract
// (design Q4): option number, y/n, or free text. An unknown answer that
// is neither a valid pick nor allowed text is an error, not a silent
// no-op - "no silent dismissal" applies to the typed path too.
func verdictAnswerFromInput(card agent.VerdictPrompt, input string) (agent.Verdict, error) {
	input = strings.TrimSpace(input)
	var err error
	switch strings.ToLower(input) {
	case "y", "yes":
		if len(card.Options) == 0 {
			return agent.Verdict{}, errNoOptions()
		}
		return agent.Verdict{OptionIndex: 1, Option: card.Options[0]}, nil
	case "n", "no":
		if len(card.Options) < 2 {
			return agent.Verdict{}, errNoOptions()
		}
		return agent.Verdict{OptionIndex: 2, Option: card.Options[1]}, nil
	case "skip", "dismiss", "s":
		return agent.Verdict{Skipped: true}, nil
	}
	if n, cerr := strconv.Atoi(input); cerr == nil {
		if n >= 1 && n <= len(card.Options) {
			return agent.Verdict{OptionIndex: n, Option: card.Options[n-1]}, nil
		}
	}
	if len(card.Options) > 0 && !card.FreeText {
		return agent.Verdict{}, &verdictInputError{card.Title + ": type the option number (1.." + strconv.Itoa(len(card.Options)) + ") or 'skip'"}
	}
	if input == "" {
		return agent.Verdict{}, &verdictInputError{card.Title + ": empty answer - type an option number, free text, or 'skip'"}
	}
	return agent.Verdict{Text: input}, err
}

// verdictInputError is a user-facing parse failure: the status line
// shows the message and the input stays in the buffer for a retry.
type verdictInputError struct{ msg string }

func (e *verdictInputError) Error() string { return e.msg }

func errNoOptions() error { return &verdictInputError{"this card has no y/n options"} }

// tuiVerdictSummary is the one-line confirmation the transcript prints
// after a typed answer lands.
func tuiVerdictSummary(card agent.VerdictPrompt, v agent.Verdict) string {
	switch {
	case v.Skipped:
		return "skipped: " + card.Title
	case v.OptionIndex > 0:
		return "picked " + v.Option + " (" + card.Title + ")"
	default:
		return "answered: " + v.Text
	}
}

// verdictBanner renders the pending question block when the loop emits
// the request_verdict tool call. It replaces the raw args dump for this
// tool only (the tool's args ARE the card; pretty printing matters -
// the user answers from what they see).
func verdictBanner(toolArgs map[string]any, width int, theme Theme) string {
	title, _ := toolArgs["title"].(string)
	body, _ := toolArgs["body"].(string)
	freeText, _ := toolArgs["free_text"].(bool)
	var options []string
	if raw, ok := toolArgs["options"].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok && s != "" {
				options = append(options, s)
			}
		}
	}
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(theme.Info.Render("[question] " + title))
	sb.WriteString("\n")
	if body != "" {
		sb.WriteString(theme.Thinking.Render(wordWrap(body, width)))
		sb.WriteString("\n")
	}
	for i, opt := range options {
		sb.WriteString(theme.Tool.Render("  " + strconv.Itoa(i+1) + ". " + opt))
		sb.WriteString("\n")
	}
	if freeText {
		sb.WriteString(theme.Tool.Render("  (or type free text)"))
		sb.WriteString("\n")
	}
	sb.WriteString(theme.Tool.Render("answer: type 1/y/n/skip or text, then Enter (or /verdict later)"))
	sb.WriteString("\n")
	return sb.String()
}