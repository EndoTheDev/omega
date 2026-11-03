package verdict

// The CLI loop's busy-time answering surface (card t_0744eb8e). The
// module owns the whole exchange - pending check, parse, record, echo -
// so the host stays a single call. That shape is load-bearing for the
// purity law (scripts/nm-purity.sh): the scrubber deletes cmd/ wires to
// this module line-by-line and never deletes a host function, so the
// loop's caller must survive the wire's deletion with the seam down.

import (
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// AnswerInline interprets one busy-time line as the answer to the
// session's oldest pending card. Returns false when no card is pending
// (the line belongs to the chat queue). Echoes land on printf - the
// host's renderer owns the palette.
func AnswerInline(vp answerSink, sessionID, line string, printf func(format string, args ...any)) bool {
	cards := vp.Pending(sessionID)
	if len(cards) == 0 {
		return false
	}
	card := cards[0]
	v, err := ParseCardAnswer(card, line)
	if err != nil {
		printf("verdict: %v\n", err)
		printf("answer with a number, y/n, skip, or text: %s\n", answerHint(card))
		return true
	}
	if _, aerr := vp.Answer(sessionID, card.ID, v); aerr != nil {
		printf("verdict answer failed: %v\n", aerr)
		return true
	}
	printf("verdict recorded: %s\n", formatVerdict(card, v))
	return true
}

// answerHint renders the one-line answer guide for a card.
func answerHint(card core.QuestionPrompt) string {
	parts := make([]string, 0, len(card.Options))
	for i, o := range card.Options {
		parts = append(parts, fmt.Sprintf("%d=%s", i+1, o.Label))
	}
	hint := strings.Join(parts, ", ")
	if card.Multi {
		hint += " (comma list)"
	}
	if card.AllowText {
		hint += ", or free text"
	}
	return hint
}