package api

import (
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

// TestSetAgentPrompt covers the hot-apply hook (card t_f78acfd6): the
// custom prompt and turn cap land on the served agent's fields - the
// loop re-reads both per Run - and a nil agent is a no-op.
func TestSetAgentPrompt(t *testing.T) {
	ag := agent.NewAgent(nil, nil, 0)
	s := NewServer(ag, nil, nil, nil, nil, nil)
	s.SetAgentPrompt("custom-live", 42)
	if ag.PromptCustom() != "custom-live" || ag.MaxTurns() != 42 {
		t.Fatalf("agent fields = %q/%d, want \"custom-live\"/42", ag.PromptCustom(), ag.MaxTurns())
	}
	// Nil agent (an unwired bind): no panic, no-op.
	NewServer(nil, nil, nil, nil, nil, nil).SetAgentPrompt("x", 3)
}
