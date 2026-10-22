package verdict

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/EndoTheDev/omega/agent"
)

// tuiProvider is the verdict module's TUI surface (card t_88bb7dcf):
// a /verdict command for idle answering and inspection. The live
// answer path during a run is the shell's busy-Enter intercept
// (modules/tui); this command covers the case the prompt is still up
// after the run finished (a pending card whose run was cancelled).
type tuiProvider struct {
	hub agent.VerdictsProvider
}

// TUICommands implements agent.TUIProvider.
func (p *tuiProvider) TUICommands() []agent.TUICommand {
	return []agent.TUICommand{
		{Name: "/verdict", Help: "list pending verdict cards / answer one: /verdict <N> <num|y|n|skip|text>"},
	}
}

// HandleTUICommand implements agent.TUIProvider.
//
//	/verdict            list every pending card (oldest first), numbered
//	/verdict N          pick one to answer: prints the card and its options
//	/verdict N 2        answer card N with option number 2
//	/verdict N y        y/n shortcuts map to the first two options
//	/verdict N skip     record a skip
//	/verdict N <text>   free text (only when the card allows it)
func (p *tuiProvider) HandleTUICommand(name, args string, c agent.TUIConsole) agent.TUIResult {
	if name != "/verdict" {
		return agent.TUIResult{}
	}
	if p.hub == nil {
		c.Error("verdicts seam not mounted")
		return agent.TUIResult{}
	}
	cards := p.hub.Pending("")
	if len(cards) == 0 {
		c.Labeled("verdicts", "no pending verdict cards")
		return agent.TUIResult{}
	}

	parts := strings.Fields(args)
	if len(parts) == 0 {
		c.Labeled("pending", formatCardList(cards))
		return agent.TUIResult{}
	}

	idx, err := strconv.Atoi(parts[0])
	if err != nil || idx < 1 || idx > len(cards) {
		c.Error("usage: /verdict           list pending\n       /verdict <N> <num|y|n|skip|text>")
		return agent.TUIResult{}
	}
	card := cards[idx-1]

	// Select-only: print the card so the next command can answer it.
	if len(parts) == 1 {
		c.Labeled("card "+strconv.Itoa(idx), formatCard(card))
		c.Labeled("answer with", fmt.Sprintf("/verdict %d <option # | y | n | skip | text>", idx))
		return agent.TUIResult{}
	}

	v, perr := parseTUIAnswer(card, parts[1:])
	if perr != nil {
		c.Error(perr.Error())
		return agent.TUIResult{}
	}
	if err := p.hub.Answer("", card.ID, v); err != nil {
		c.Error(err.Error())
		return agent.TUIResult{}
	}
	c.ClearError()
	c.Labeled("answered", formatVerdict(card, v))
	return agent.TUIResult{}
}

// parseTUIAnswer maps typed tokens to an agent.Verdict, mirroring the
// design's option-number / y-n / free-text contract.
func parseTUIAnswer(card agent.VerdictPrompt, rest []string) (agent.Verdict, error) {
	tok := strings.TrimSpace(strings.Join(rest, " "))
	if tok == "" {
		return agent.Verdict{}, fmt.Errorf("empty answer")
	}
	switch strings.ToLower(tok) {
	case "skip", "dismiss", "s":
		return agent.Verdict{Skipped: true}, nil
	case "y", "yes":
		if len(card.Options) == 0 {
			return agent.Verdict{}, fmt.Errorf("this card has no y/n options")
		}
		return agent.Verdict{OptionIndex: 1, Option: card.Options[0]}, nil
	case "n", "no":
		if len(card.Options) < 2 {
			return agent.Verdict{}, fmt.Errorf("this card has no 'no' option")
		}
		return agent.Verdict{OptionIndex: 2, Option: card.Options[1]}, nil
	}
	if n, err := strconv.Atoi(tok); err == nil {
		if n < 1 || n > len(card.Options) {
			return agent.Verdict{}, fmt.Errorf("option %d is out of range (1..%d)", n, len(card.Options))
		}
		return agent.Verdict{OptionIndex: n, Option: card.Options[n-1]}, nil
	}
	if len(card.Options) > 0 && !card.FreeText {
		return agent.Verdict{}, fmt.Errorf("this card takes an option number, not free text")
	}
	return agent.Verdict{Text: tok}, nil
}

func formatCardList(cards []agent.VerdictPrompt) string {
	var sb strings.Builder
	for i, c := range cards {
		fmt.Fprintf(&sb, "%d. [%s] %s\n", i+1, shortID(c.ID), c.Title)
		if len(c.Options) > 0 {
			fmt.Fprintf(&sb, "    options: %s\n", strings.Join(c.Options, ", "))
		}
		if c.Body != "" {
			fmt.Fprintf(&sb, "    %s\n", firstN(c.Body, 80))
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

func formatCard(c agent.VerdictPrompt) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n", c.Title)
	if c.Body != "" {
		fmt.Fprintf(&sb, "%s\n", c.Body)
	}
	for i, opt := range c.Options {
		fmt.Fprintf(&sb, "  %d. %s\n", i+1, opt)
	}
	if c.FreeText {
		sb.WriteString("  (or type free text)\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func formatVerdict(card agent.VerdictPrompt, v agent.Verdict) string {
	switch {
	case v.Skipped:
		return "skipped: " + card.Title
	case v.Expired:
		return "expired: " + card.Title
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