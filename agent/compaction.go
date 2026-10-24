package agent

import (
	"github.com/EndoTheDev/omega/core"
)

// charsPerToken is the char-to-token ratio used by estimateTokens.
// ponytail: 4 chars/token is a rough average across models; good enough
// to trigger compaction. Upgrade path: use the provider's real tokenizer
// when one is exposed.
const charsPerToken = 4

// DefaultContextWindow moved to core with CompactionConfig (arc 1b);
// re-exported for the modules that import agent.DefaultContextWindow.
const DefaultContextWindow = core.DefaultContextWindow

// CompactionConfig moved to core with its Budget methods (arc 1b);
// FrontendOptions and the host config reference it.
type CompactionConfig = core.CompactionConfig

// EstimateTokens returns a rough token count for the message history.
func EstimateTokens(history []core.Message) int {
	total := 0
	for _, m := range history {
		total += len(MessageText(m))
	}
	return total / charsPerToken
}

// MessageText returns the user-visible text of a message, used for token
// estimation, summary rendering, and export.
func MessageText(m core.Message) string {
	switch v := m.(type) {
	case core.System:
		return v.Content
	case core.User:
		return v.Content
	case core.Assistant:
		return v.Content
	case core.ToolResult:
		return v.Content
	}
	return ""
}

// BuildCompactedMessages assembles the compacted message list from a
// pre-computed summary. Shared by the branch summary feature and the
// compactor plugin.
func BuildCompactedMessages(history []core.Message, summary string, keepFirst, keepLast int) []core.Message {
	result := make([]core.Message, 0, keepFirst+keepLast+1)
	result = append(result, history[:keepFirst]...)
	result = append(result, core.NewSystem("[compacted: "+summary+"]"))
	result = append(result, history[len(history)-keepLast:]...)
	return result
}
