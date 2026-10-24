package http_channel

// Multi-turn timing persistence: every assistant segment of a tool-loop
// run - including post-tool segments - must carry a timing record on
// reload. The live meters die with the SSE stream, so the server-side
// numbers are all the webui has. Regression for the reproduced report:
// the 'say one -> sleep -> say two -> say done' sequence persisted
// timing only on the first segment; post-tool segments reloaded with
// timing=nil.
//
// The real agent loop (modules/agent_loop) runs against a delayed fake
// provider: each streamed event lands ~timingDelay after the previous
// one, so every phase boundary is measurable wall-clock time.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
	agent_loop "github.com/EndoTheDev/omega/modules/agent_loop"
	"github.com/EndoTheDev/omega/modules/store"
)

// eventDelay spaces consecutive streamed events so chunk-to-chunk and
// boundary-to-chunk deltas stay above any scheduler jitter floor the
// assertions use.
const eventDelay = 60 * time.Millisecond

// toolSleep is the scripted tool duration. Assert floors must stay
// strictly below it and well above eventDelay.
const toolSleep = 150 * time.Millisecond

func timingTestAgent(t *testing.T) (*core.Agent, *store.Store, *httptest.Server, *http.Client) {
	t.Helper()
	tools := map[string]core.Tool{
		"sleep": {
			Description: "sleep for testing",
			Run: func(_ context.Context, _ map[string]any) (string, error) {
				select {
				case <-time.After(toolSleep):
					return "slept", nil
				case <-context.Background().Done():
					return "", context.Canceled
				}
			},
		},
	}
	script1 := []core.ProviderStreamEvent{
		core.ThinkingChunk{Type: "thinking_chunk", Content: "t1"},
		core.ThinkingChunk{Type: "thinking_chunk", Content: "t2"},
		core.ResponseChunk{Type: "response_chunk", Content: "one"},
		core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "c1", Name: "sleep"}},
		core.StreamEnd{Type: "stream_end", FinishReason: "tool_use"},
	}
	script2 := []core.ProviderStreamEvent{
		core.ResponseChunk{Type: "response_chunk", Content: "two"},
		core.ToolCallEvent{Type: "tool_call", ToolCall: core.ToolCall{ID: "c2", Name: "sleep"}},
		core.StreamEnd{Type: "stream_end", FinishReason: "tool_use"},
	}
	script3 := []core.ProviderStreamEvent{
		core.ResponseChunk{Type: "response_chunk", Content: "done"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	}
	prov := ai.NewFakeProviderScripts("fake", script1, script2, script3).WithDelay(eventDelay)
	a := core.NewAgent(prov, nil, 0)
	a.SetLoopProvider(agent_loop.Loop{})
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s := NewServer(a, tools, st, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return a, st, ts, ts.Client()
}

type persistedMessage struct {
	Role    string       `json:"role"`
	Content string       `json:"content"`
	Timing  *core.Timing `json:"timing"`
}

// fetchPersistedMessages GETs the session detail and decodes every
// message with its timing record.
func fetchPersistedMessages(t *testing.T, ts *httptest.Server, id string) []persistedMessage {
	t.Helper()
	resp, err := ts.Client().Get(ts.URL + "/sessions/" + id)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get session status = %d, want 200", resp.StatusCode)
	}
	var detail struct {
		Messages []persistedMessage `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	return detail.Messages
}

// TestChatTimingPostToolSegments drives the full repro turn shape and
// asserts the persisted history: three assistant segments, each with a
// timing record whose numbers reflect the paced stream.
func TestChatTimingPostToolSegments(t *testing.T) {
	_, st, ts, _ := timingTestAgent(t)
	createSession(t, ts, "s1")
	muteTitles(t, st, "s1")

	body, _ := json.Marshal(map[string]any{
		"messages":   []map[string]any{{"role": "user", "content": "do the sequence"}},
		"session_id": "s1",
	})
	resp, err := http.Post(ts.URL+"/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("chat request: %v", err)
	}
	sc := bufio.NewScanner(resp.Body)
	// Detached run: drain the wire until the channel closes (run hub
	// finish), then wait for every message to land.
	for sc.Scan() {
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close chat body: %v", err)
	}

	// user + assistant(one) + tool + assistant(two) + tool + assistant(done)
	waitFor(t, "6 persisted messages", func() bool {
		msgs, err := st.GetMessages(context.Background(), "s1")
		return err == nil && len(msgs) == 6
	})

	msgs := fetchPersistedMessages(t, ts, "s1")
	if len(msgs) != 6 {
		t.Fatalf("persisted %d messages, want 6", len(msgs))
	}
	wantContents := []string{"do the sequence", "one", "slept", "two", "slept", "done"}
	for i, m := range msgs {
		if m.Role == "" {
			t.Fatalf("message %d has no role", i)
		}
		if m.Content != wantContents[i] {
			t.Errorf("message %d content = %q, want %q", i, m.Content, wantContents[i])
		}
	}

	// Every assistant message must carry a timing record - nil timing is
	// exactly the reported bug (post-tool segments showed no meter).
	assistants := []int{1, 3, 5}
	for _, i := range assistants {
		suffix := ""
		if i == 3 || i == 5 {
			suffix = fmt.Sprintf(" (post-tool segment, content %q)", msgs[i].Content)
		}
		t.Logf("segment %d%s: timing=%+v", i, suffix, msgs[i].Timing)
		if msgs[i].Timing == nil {
			t.Errorf("assistant message %d timing = nil%s", i, suffix)
			continue
		}
		// Thinking/response spans must reflect the paced stream: every
		// event is at least eventDelay after the previous one, so any
		// recorded phase has at least half a step of wall clock behind it.
		if got := msgs[i].Timing.ResponseMs; got < int64(eventDelay/(2*time.Millisecond)) {
			t.Errorf("assistant message %d%s response_ms = %d, want >= %d",
				i, suffix, got, eventDelay/(2*time.Millisecond))
		}
		if msgs[i].Timing.ThinkingMs < 0 {
			t.Errorf("assistant message %d negative thinking_ms", i)
		}
	}

	// Turn 1 had a thinking phase: two chunks spaced eventDelay apart.
	if got := msgs[1].Timing.ThinkingMs; got < int64(eventDelay/(2*time.Millisecond)) {
		t.Errorf("assistant message 1 thinking_ms = %d, want >= %d (paced think burst)",
			got, eventDelay/(2*time.Millisecond))
	}

	// Tool results carry call-to-result elapsed.
	for i := 0; i < len(msgs); i++ {
		if msgs[i].Role != "tool" {
			continue
		}
		if msgs[i].Timing == nil {
			t.Errorf("tool message %d timing = nil", i)
			continue
		}
		floor := int64(toolSleep/(2*time.Millisecond)) - 5
		if got := msgs[i].Timing.ElapsedMs; got < floor || got > 2000 {
			t.Errorf("tool message %d elapsed_ms = %d, want in [%d, 2000]", i, got, floor)
		}
	}
}
