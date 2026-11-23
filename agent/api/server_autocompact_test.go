package api

// server_autocompact_test.go: the growth gate end to end. A run over a fat session compacts
// on first sight, the block lands in the store, and the next request's wire is derived from
// it - head, carrier, kept tail - instead of the raw history.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	httptest2 "net/http/httptest"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/agent/loop"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/compactor"
	"github.com/EndoTheDev/omega/harness/store"
)

// TestChatAutoCompactsThroughTheGrowthGate drives the real loop: the gate fires on the
// first turn of a session already over budget (the mass bypass), the run summarizes, the
// recorder persists the block through the turn sink, and the stored wire keeps its head.
func TestChatAutoCompactsThroughTheGrowthGate(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.CreateSession(ctx, "auto-1", "", "fat conversation"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	// 11 rows of 5000 chars: ~13.7K tokens against a 9830-token budget, with ~10K of
	// compactable material - over the budget AND over the benefit floor.
	for i := 0; i < 11; i++ {
		msg := core.NewUser(fmt.Sprintf("row %d %s", i, strings.Repeat("x", 5000)))
		if err := st.AppendMessage(ctx, "auto-1", msg); err != nil {
			t.Fatalf("append row %d: %v", i, err)
		}
	}

	runProvider := newMockProvider("mock", scripted(
		core.ResponseChunk{Type: "response_chunk", Content: "ok"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	))
	a := agent.NewAgent(runProvider, nil, 0)
	a.SetLoopProvider(loop.Loop{})
	summaryProvider := agent.NewFakeProvider("mock",
		core.ResponseChunk{Type: "response_chunk", Content: "the history in one paragraph"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	)
	a.SetCompactionProvider(compactor.New(summaryProvider, agent.CompactionConfig{
		Enabled: true, Threshold: 0.6, ContextWindow: 32768, KeepLast: 2,
	}))

	s := NewServer(a, nil, nil, nil, nil, nil)
	s.store = st
	s.chat.batch = 0
	if err := s.newCanvas(&core.Context{PanelProviders: []core.PanelProvider{s.chat}}); err != nil {
		t.Fatalf("newCanvas: %v", err)
	}
	ts := httptest2.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	res, err := http.Post(ts.URL+"/chat", "application/json", strings.NewReader(
		`{"session_id":"auto-1","messages":[{"role":"user","content":"carry on"}]}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if _, err := io.Copy(io.Discard, bufio.NewReader(res.Body)); err != nil {
		t.Fatalf("drain stream: %v", err)
	}

	blocks, err := st.ContextBlocks(ctx, "auto-1")
	if err != nil {
		t.Fatalf("blocks: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want the one automatic compact", len(blocks))
	}
	if blocks[0].From == 0 || blocks[0].To == 0 || blocks[0].Carrier == 0 {
		t.Fatalf("block = %+v, want a real span and carrier", blocks[0])
	}
	wire, err := agent.SessionContext(ctx, st, "auto-1")
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	if len(wire) < 4 {
		t.Fatalf("wire = %d messages, want head + carrier pair + kept tail", len(wire))
	}
	if msg := agent.MessageText(wire[0]); !strings.HasPrefix(msg, "row 0 ") {
		t.Fatalf("wire head = %q, want the first user message verbatim", msg)
	}
	if !agent.IsCarrierResult(wire[2]) || agent.MessageText(wire[2]) != "the history in one paragraph" {
		t.Fatalf("carrier = %q, want the run's summary persisted as a block", agent.MessageText(wire[2]))
	}
}
