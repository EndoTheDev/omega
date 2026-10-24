package tui

// Verdict cards v1 (card t_2ecf9c51): the TUI's typed answer path.
// While a run is blocked inside verdict.request, the typed input is an
// ANSWER, not a queued follow-up: "1"/"2"/... pick, "1,3" a
// multi-select pick, "y"/"n" the first two options, "skip" records a
// dismiss, anything else is typed text when the card allows it. The
// verdict module owns the /verdict command (idle answering +
// inspection); the shell owns this live path because Enter intercepts
// happen before any command dispatch.

import (
	"strconv"
	"strings"

	"github.com/EndoTheDev/omega/core"
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
	m.addNotice("\n" + m.theme.Info.Render("[verdict: "+tuiVerdictSummary(card, v)+"]") + "\n")
	m.refresh()
	return true
}

// verdictAnswerFromInput maps one typed line to a Verdict. The contract
// (design Q4): option number(s, comma-separated on a multi card), y/n,
// or typed text. An unknown answer that is neither a valid pick nor
// allowed text is an error, not a silent no-op - "no silent dismissal"
// applies to the typed path too.
func verdictAnswerFromInput(card core.VerdictPrompt, input string) (core.Verdict, error) {
	input = strings.TrimSpace(input)
	var err error
	switch strings.ToLower(input) {
	case "y", "yes":
		if card.Multi {
			return core.Verdict{}, &verdictInputError{card.Title + ": multi-select card - type the row numbers, e.g. 1,3"}
		}
		if len(card.Options) == 0 {
			return core.Verdict{}, errNoOptions()
		}
		return core.Verdict{OptionIndex: 1, Option: card.Options[0].Label}, nil
	case "n", "no":
		if card.Multi {
			return core.Verdict{}, &verdictInputError{card.Title + ": multi-select card - type the row numbers, e.g. 1,3"}
		}
		if len(card.Options) < 2 {
			return core.Verdict{}, errNoOptions()
		}
		return core.Verdict{OptionIndex: 2, Option: card.Options[1].Label}, nil
	case "skip", "dismiss", "s":
		return core.Verdict{Skipped: true}, nil
	}
	if card.Multi {
		if ixs, perr := parseOptionIndexes(input); perr == nil {
			labels := make([]string, 0, len(ixs))
			for _, ix := range ixs {
				if ix < 1 || ix > len(card.Options) {
					return core.Verdict{}, &verdictInputError{card.Title + ": option " + strconv.Itoa(ix) + " is out of range (1.." + strconv.Itoa(len(card.Options)) + ")"}
				}
				labels = append(labels, card.Options[ix-1].Label)
			}
			return core.Verdict{OptionIndexes: ixs, Options: labels}, nil
		}
	} else if n, cerr := strconv.Atoi(input); cerr == nil {
		if n >= 1 && n <= len(card.Options) {
			return core.Verdict{OptionIndex: n, Option: card.Options[n-1].Label}, nil
		}
	}
	if len(card.Options) > 0 && !card.AllowText {
		return core.Verdict{}, &verdictInputError{card.Title + ": type the option number (1.." + strconv.Itoa(len(card.Options)) + ") or 'skip'"}
	}
	if input == "" {
		return core.Verdict{}, &verdictInputError{card.Title + ": empty answer - type an option number, text, or 'skip'"}
	}
	return core.Verdict{Text: input}, err
}

// parseOptionIndexes parses a comma-separated pick list ("1,3"). It
// returns an error when any token is not an option number, so a
// malformed multi answer falls through to the typed-text path with a
// clear rejection rather than a partial pick. Range checks against a
// specific card's options happen at the callers.
func parseOptionIndexes(input string) ([]int, error) {
	fields := strings.Split(input, ",")
	ixs := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil {
			return nil, err
		}
		ixs = append(ixs, n)
	}
	return ixs, nil
}

// verdictInputError is a user-facing parse failure: the status line
// shows the message and the input stays in the buffer for a retry.
type verdictInputError struct{ msg string }

func (e *verdictInputError) Error() string { return e.msg }

func errNoOptions() error { return &verdictInputError{"this card has no y/n options"} }

// tuiVerdictSummary is the one-line confirmation the transcript prints
// after a typed answer lands.
func tuiVerdictSummary(card core.VerdictPrompt, v core.Verdict) string {
	switch {
	case v.Skipped:
		return "skipped: " + card.Title
	case len(v.OptionIndexes) > 0:
		return "picked " + strings.Join(v.Options, ", ") + " (" + card.Title + ")"
	case v.OptionIndex > 0:
		return "picked " + v.Option + " (" + card.Title + ")"
	default:
		return "answered: " + v.Text
	}
}

// verdictBanner renders the pending question block when the loop emits
// the verdict tool call. It replaces the raw args dump for this tool
// only (the tool's args ARE the card; pretty printing matters - the
// user answers from what they see).
func verdictBanner(toolArgs map[string]any, width int, theme Theme) string {
	title, _ := toolArgs["title"].(string)
	body, _ := toolArgs["body"].(string)
	multi, _ := toolArgs["multi"].(bool)
	allowText, _ := toolArgs["allow_text"].(bool)
	var options []core.VerdictOption
	if raw, ok := toolArgs["options"].([]any); ok {
		for _, v := range raw {
			switch o := v.(type) {
			case string:
				if o != "" {
					options = append(options, core.VerdictOption{Label: o})
				}
			case map[string]any:
				label, _ := o["label"].(string)
				if label == "" {
					continue
				}
				desc, _ := o["description"].(string)
				options = append(options, core.VerdictOption{Label: label, Description: desc})
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
		sb.WriteString(theme.Tool.Render("  " + strconv.Itoa(i+1) + ". " + opt.Label))
		sb.WriteString("\n")
		if opt.Description != "" {
			sb.WriteString(theme.Thinking.Render("     " + wordWrap(opt.Description, width)))
			sb.WriteString("\n")
		}
	}
	switch {
	case multi:
		sb.WriteString(theme.Tool.Render("multi-select: type the row numbers, e.g. 1,3"))
		sb.WriteString("\n")
	case allowText && len(options) > 0:
		sb.WriteString(theme.Tool.Render("  (or type your own answer)"))
		sb.WriteString("\n")
	}
	if allowText && len(options) == 0 {
		sb.WriteString(theme.Tool.Render("type your own answer, then Enter"))
		sb.WriteString("\n")
	}
	sb.WriteString(theme.Tool.Render("answer: type the number(s)/skip or text, then Enter (or /verdict later)"))
	sb.WriteString("\n")
	return sb.String()
}
