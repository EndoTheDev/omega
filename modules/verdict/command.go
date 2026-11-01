package verdict

// Module-owned command surface (card t_0744eb8e): /verdict belongs to
// the verdict module and rides the extension command seam - the CLI
// loop dispatches it when idle, the busy-time answer path uses
// ParseCardAnswer directly. The webui answers cards in the pane; no
// storage or seam is reimplemented here.

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// verdictHandler serves the /verdict extension command:
//
//	/verdict            list every pending card (oldest first), numbered
//	/verdict N          pick one to answer: prints the card and its options
//	/verdict N 2        answer card N with option number 2
//	/verdict N 1,3      answer a multi card with options 1 and 3
//	/verdict N y        y/n shortcuts map to the first two options
//	/verdict N skip     record a skip
//	/verdict N <text>   free text (only when the card allows it)
func verdictHandler(p *Plugin) func(context.Context, string, string) (core.CommandResult, error) {
	return func(_ context.Context, _ string, args string) (core.CommandResult, error) {
		if p.hub == nil {
			return core.CommandResult{}, fmt.Errorf("verdicts seam not mounted")
		}
		cards := p.hub.Pending("")
		if len(cards) == 0 {
			return core.CommandResult{Text: "[verdicts] no pending verdict cards"}, nil
		}
		parts := strings.Fields(args)
		if len(parts) == 0 {
			return core.CommandResult{Text: "[verdicts] " + formatCardList(cards)}, nil
		}
		idx, err := strconv.Atoi(parts[0])
		if err != nil || idx < 1 || idx > len(cards) {
			return core.CommandResult{}, fmt.Errorf("usage: /verdict           list pending\n       /verdict <N> <num|1,3|y|n|skip|text>")
		}
		card := cards[idx-1]

		// Select-only: print the card so the next command can answer it.
		if len(parts) == 1 {
			return core.CommandResult{
				Text: "card " + strconv.Itoa(idx) + ": " + formatCard(card) +
					"\nanswer with: /verdict " + strconv.Itoa(idx) + " <option # | 1,3 (multi) | y | n | skip | text>",
			}, nil
		}

		v, perr := parseVerdictAnswer(card, parts[1:])
		if perr != nil {
			return core.CommandResult{}, perr
		}
		if _, err := p.hub.Answer("", card.ID, v); err != nil {
			return core.CommandResult{}, err
		}
		return core.CommandResult{Text: "answered: " + formatVerdict(card, v)}, nil
	}
}

// ParseCardAnswer maps one free-typed line to a core.Verdict for the
// answering surface's busy-time path: option number / comma-multi /
// y-n / skip / free text, exactly the /verdict N <answer> contract.
func ParseCardAnswer(card core.VerdictPrompt, input string) (core.Verdict, error) {
	return parseVerdictAnswer(card, strings.Fields(input))
}

// parseVerdictAnswer implements the typed-token contract above.
func parseVerdictAnswer(card core.VerdictPrompt, rest []string) (core.Verdict, error) {
	tok := strings.TrimSpace(strings.Join(rest, " "))
	if tok == "" {
		return core.Verdict{}, fmt.Errorf("empty answer")
	}
	switch strings.ToLower(tok) {
	case "skip", "dismiss", "s":
		return core.Verdict{Skipped: true}, nil
	case "y", "yes":
		if err := ensureNotMulti(card); err != nil {
			return core.Verdict{}, err
		}
		if len(card.Options) == 0 {
			return core.Verdict{}, fmt.Errorf("this card has no y/n options")
		}
		return core.Verdict{OptionIndex: 1, Option: card.Options[0].Label}, nil
	case "n", "no":
		if err := ensureNotMulti(card); err != nil {
			return core.Verdict{}, err
		}
		if len(card.Options) < 2 {
			return core.Verdict{}, fmt.Errorf("this card has no 'no' option")
		}
		return core.Verdict{OptionIndex: 2, Option: card.Options[1].Label}, nil
	}
	// a multi card takes a comma-separated pick list ("1,3"); a lone
	// number ("2") is the one-row multi-select.
	if card.Multi {
		if ixs, err := parseOptionIndexes(tok); err == nil {
			labels := make([]string, 0, len(ixs))
			for _, ix := range ixs {
				if ix < 1 || ix > len(card.Options) {
					return core.Verdict{}, fmt.Errorf("option %d is out of range (1..%d)", ix, len(card.Options))
				}
				labels = append(labels, card.Options[ix-1].Label)
			}
			return core.Verdict{OptionIndexes: ixs, Options: labels}, nil
		}
	} else if n, err := strconv.Atoi(tok); err == nil {
		if n < 1 || n > len(card.Options) {
			return core.Verdict{}, fmt.Errorf("option %d is out of range (1..%d)", n, len(card.Options))
		}
		return core.Verdict{OptionIndex: n, Option: card.Options[n-1].Label}, nil
	}
	if len(card.Options) > 0 && !card.AllowText {
		return core.Verdict{}, fmt.Errorf("this card takes an option number, not free text")
	}
	return core.Verdict{Text: tok}, nil
}

// parseOptionIndexes parses a comma-separated pick list ("1,3"). It
// returns an error when any token is not an option number, so a
// malformed multi answer falls through to the free-text path with a
// clear rejection rather than a partial pick.
func parseOptionIndexes(tok string) ([]int, error) {
	fields := strings.Split(tok, ",")
	ixs := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil || n < 1 || n > 32 {
			return nil, fmt.Errorf("not an option number: %q", f)
		}
		ixs = append(ixs, n)
	}
	return ixs, nil
}

// ensureNotMulti rejects single-pick tokens (y/n/one number) on a
// multi card, so a single tap can never silently under-answer.
func ensureNotMulti(card core.VerdictPrompt) error {
	if card.Multi {
		return fmt.Errorf("this card is multi-select: pick numbers, e.g. /verdict N 1,3")
	}
	return nil
}

func formatCardList(cards []core.VerdictPrompt) string {
	var sb strings.Builder
	for i, c := range cards {
		fmt.Fprintf(&sb, "%d. [%s] %s\n", i+1, shortID(c.ID), c.Title)
		for _, opt := range c.Options {
			fmt.Fprintf(&sb, "    - %s\n", opt.Label)
			if opt.Description != "" {
				fmt.Fprintf(&sb, "      %s\n", firstN(opt.Description, 72))
			}
		}
		if c.Body != "" {
			fmt.Fprintf(&sb, "    %s\n", firstN(c.Body, 80))
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

func formatCard(c core.VerdictPrompt) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s", c.Title)
	if c.Multi {
		sb.WriteString(" (multi-select)")
	}
	sb.WriteString("\n")
	if c.Body != "" {
		fmt.Fprintf(&sb, "%s\n", c.Body)
	}
	for i, opt := range c.Options {
		fmt.Fprintf(&sb, "  %d. %s\n", i+1, opt.Label)
		if opt.Description != "" {
			fmt.Fprintf(&sb, "     %s\n", opt.Description)
		}
	}
	if c.AllowText {
		sb.WriteString("  (or type free text)\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func formatVerdict(card core.VerdictPrompt, v core.Verdict) string {
	switch {
	case v.Skipped:
		return "skipped: " + card.Title
	case v.Expired:
		return "expired: " + card.Title
	case len(v.OptionIndexes) > 0:
		return fmt.Sprintf("picked %v: %s", v.OptionIndexes, card.Title)
	case v.OptionIndex > 0:
		return fmt.Sprintf("picked %d (%s): %s", v.OptionIndex, v.Option, card.Title)
	default:
		return "answered: " + v.Text
	}
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func firstN(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
