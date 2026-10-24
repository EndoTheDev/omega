package verdict

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// tuiProvider is the verdict module's TUI surface (card t_88bb7dcf):
// a /verdict command for idle answering and inspection. The live
// answer path during a run is the shell's busy-Enter intercept
// (modules/tui); this command covers the case the prompt is still up
// after the run finished (a pending card whose run was cancelled).
type tuiProvider struct {
	hub core.VerdictsProvider
}

// TUICommands implements core.TUIProvider.
func (p *tuiProvider) TUICommands() []core.TUICommand {
	return []core.TUICommand{
		{Name: "/verdict", Help: "list pending verdict cards / answer one: /verdict <N> <num|1,3|y|n|skip|text>"},
	}
}

// HandleTUICommand implements core.TUIProvider.
//
//	/verdict            list every pending card (oldest first), numbered
//	/verdict N          pick one to answer: prints the card and its options
//	/verdict N 2        answer card N with option number 2
//	/verdict N 1,3      answer a multi card with options 1 and 3
//	/verdict N y        y/n shortcuts map to the first two options
//	/verdict N skip     record a skip
//	/verdict N <text>   free text (only when the card allows it)
func (p *tuiProvider) HandleTUICommand(name, args string, c core.TUIConsole) core.TUIResult {
	if name != "/verdict" {
		return core.TUIResult{}
	}
	if p.hub == nil {
		c.Error("verdicts seam not mounted")
		return core.TUIResult{}
	}
	cards := p.hub.Pending("")
	if len(cards) == 0 {
		c.Labeled("verdicts", "no pending verdict cards")
		return core.TUIResult{}
	}

	parts := strings.Fields(args)
	if len(parts) == 0 {
		c.Labeled("pending", formatCardList(cards))
		return core.TUIResult{}
	}

	idx, err := strconv.Atoi(parts[0])
	if err != nil || idx < 1 || idx > len(cards) {
		c.Error("usage: /verdict           list pending\n       /verdict <N> <num|1,3|y|n|skip|text>")
		return core.TUIResult{}
	}
	card := cards[idx-1]

	// Select-only: print the card so the next command can answer it.
	if len(parts) == 1 {
		c.Labeled("card "+strconv.Itoa(idx), formatCard(card))
		c.Labeled("answer with", fmt.Sprintf("/verdict %d <option # | 1,3 (multi) | y | n | skip | text>", idx))
		return core.TUIResult{}
	}

	v, perr := parseTUIAnswer(card, parts[1:])
	if perr != nil {
		c.Error(perr.Error())
		return core.TUIResult{}
	}
	if _, err := p.hub.Answer("", card.ID, v); err != nil {
		c.Error(err.Error())
		return core.TUIResult{}
	}
	c.ClearError()
	c.Labeled("answered", formatVerdict(card, v))
	return core.TUIResult{}
}

// parseTUIAnswer maps typed tokens to an core.Verdict, mirroring the
// design's option-number / comma-multi / y-n / free-text contract.
func parseTUIAnswer(card core.VerdictPrompt, rest []string) (core.Verdict, error) {
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
