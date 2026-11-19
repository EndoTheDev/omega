package loop

import (
	"fmt"
	"unicode/utf8"
)

// The tool-output bound (MaxToolOutput, agent.LoopOptions) lands a result in the. They
// used to each slice with content[:max] and stamp the same bare total - a byte cut that
// can split a UTF-8 rune and a note that never says how much survived.

// truncateToolOutput bounds content to at most max bytes of KEPT payload and appends.
// The cut backs off to a rune boundary, so the kept prefix is always valid UTF-8: a
// byte cut through a multi-byte rune reaches the model as a replacement character in
// the middle of a path. The note names the kept and total byte counts because the model
// has no other way to tell a 36-byte tail from a 36 KB one.
func truncateToolOutput(content string, max int) string {
	if max <= 0 || len(content) <= max {
		return content
	}
	kept := max
	for kept > 0 && !utf8.RuneStart(content[kept]) {
		kept--
	}
	return fmt.Sprintf("%s\n... [truncated: kept the first %d bytes of %d - the tail was dropped]",
		content[:kept], kept, len(content))
}
