package loop

import (
	"fmt"
	"unicode/utf8"
)

// truncateToolOutput bounds content to at most max bytes of KEPT payload and appends a note
// naming the kept and total byte counts - the model has no other way to tell a 36-byte tail
// from a 36 KB one. The cut backs off to a rune boundary, so the kept prefix is always valid
// UTF-8: a byte cut through a multi-byte rune reaches the model as a replacement character.
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
