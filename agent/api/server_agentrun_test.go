package api

// server_agentrun_test.go: per-agent run config (card t_a7fab745, Endo's
// ruling A). Law: a session owned by an agent home runs on THAT home's
// agent (provider, model, system prompt, max_turns) and its turn
// persists into that home's store only; a daemon-owned session keeps
// today's agent and store untouched.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/datadir"
	"github.com/EndoTheDev/omega/harness/store"
)

// captureLoop records the LoopOptions a run was built with (the values
// the agent home owns) and then runs the canned turn.
type captureLoop struct {
	seen *agent.LoopOptions
}

func (c captureLoop) Run(ctx context.Context, opts agent.LoopOptions) error {
	*c.seen = opts
	return testLoop{}.Run(ctx, opts)
}

// agentHomeServer builds one daemon home with an agent-owned session and
// a main session, plus the agent the host factory serves for each.
func agentHomeServer(t *testing.T) (home string, srv *Server, ts *httptest.Server, daemonSeen, scoutSeen *agent.LoopOptions) {
	t.Helper()
	ctx := context.Background()
	home = t.TempDir()

	// Main store: one daemon-owned session, titled (no title kick).
	main, err := store.Open(datadir.DBPath(home, "omega.db"))
	if err != nil {
		t.Fatalf("open main store: %v", err)
	}
	main.SetHome(home)
	t.Cleanup(func() { main.Close() })
	if err := main.CreateSession(ctx, "sess-main", "", ""); err != nil {
		t.Fatalf("create main session: %v", err)
	}
	if err := main.SetSessionTitle(ctx, "sess-main", "Main session", true); err != nil {
		t.Fatalf("title main session: %v", err)
	}

	// Agent home: the session the roster lists, in its own file.
	scout, err := store.Open(datadir.DBPath(filepath.Join(home, "agents", "scout"), "omega.db"))
	if err != nil {
		t.Fatalf("open scout store: %v", err)
	}
	if err := scout.CreateSession(ctx, "sess-scout-2", "", ""); err != nil {
		t.Fatalf("create scout session: %v", err)
	}
	if err := scout.SetSessionTitle(ctx, "sess-scout-2", "Scout session", true); err != nil {
		t.Fatalf("title scout session: %v", err)
	}
	if err := scout.AppendMessage(ctx, "sess-scout-2", core.NewUser("probe one")); err != nil {
		t.Fatalf("append scout history: %v", err)
	}
	if err := scout.Close(); err != nil {
		t.Fatalf("close scout store: %v", err)
	}

	script := []core.ProviderStreamEvent{
		core.ResponseChunk{Type: "response_chunk", Content: "answer"},
		core.StreamEnd{Type: "stream_end", FinishReason: "stop"},
	}
	daemonSeen, scoutSeen = &agent.LoopOptions{}, &agent.LoopOptions{}
	daemonAgent := agent.NewAgent(newMockProvider("daemon-model", script), nil, 0)
	daemonAgent.SetLoopProvider(captureLoop{seen: daemonSeen})
	daemonAgent.SetPromptCustom("daemon-prompt")
	daemonAgent.SetMaxTurns(3)

	scoutAgent := agent.NewAgent(newMockProvider("scout-model", script), nil, 0)
	scoutAgent.SetLoopProvider(captureLoop{seen: scoutSeen})
	scoutAgent.SetPromptCustom("scout-prompt")
	scoutAgent.SetMaxTurns(7)

	srv = NewServer(daemonAgent, nil, main, nil, nil, nil)
	srv.SetAgentRunFactory(func(_ context.Context, name string) (*agent.Agent, error) {
		if name != "scout" {
			t.Errorf("factory asked for %q, want the session owner", name)
		}
		return scoutAgent, nil
	})
	ts = httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return home, srv, ts, daemonSeen, scoutSeen
}

func TestAgentOwnedSessionReadsAndRunsOnItsHome(t *testing.T) {
	ctx := context.Background()
	home, srv, ts, daemonSeen, scoutSeen := agentHomeServer(t)

	// READ routing: the transcript receipt that used to 404 answers
	// with the agent-owned history.
	res, err := http.Get(ts.URL + "/sessions/sess-scout-2/transcript")
	if err != nil {
		t.Fatalf("transcript: %v", err)
	}
	var body struct {
		Items []transcriptItem `json:"items"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode transcript: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("transcript status = %d, want 200", res.StatusCode)
	}
	if len(body.Items) == 0 {
		t.Fatal("transcript has no items for the agent-owned session")
	}
	// The session row answers too.
	res2, err := http.Get(ts.URL + "/sessions/sess-scout-2")
	if err != nil {
		t.Fatalf("session row: %v", err)
	}
	res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("session row status = %d, want 200", res2.StatusCode)
	}

	// RUN config: the send runs on the agent home's agent.
	postChatAndWait(t, ts, "sess-scout-2")

	if got := scoutSeen.Provider.ModelName(); got != "scout-model" {
		t.Fatalf("agent-owned run used model %q, want %q", got, "scout-model")
	}
	if scoutSeen.PromptCustom != "scout-prompt" || scoutSeen.MaxTurns != 7 {
		t.Fatalf("agent-owned run used prompt %q / max_turns %d, want %q / 7",
			scoutSeen.PromptCustom, scoutSeen.MaxTurns, "scout-prompt")
	}
	if daemonSeen.Provider != nil {
		t.Fatal("daemon agent served an agent-owned session")
	}

	// WRITE routing: the turn persisted into the agent's own file.
	waitFor(t, "the agent-owned turn to persist", func() bool {
		msgs, err := srv.store.GetMessages(ctx, "sess-scout-2")
		return err == nil && len(msgs) >= 3
	})
	cold, err := store.Open(datadir.DBPath(filepath.Join(home, "agents", "scout"), "omega.db"))
	if err != nil {
		t.Fatalf("reopen scout store: %v", err)
	}
	defer cold.Close()
	coldMsgs, err := cold.GetMessages(ctx, "sess-scout-2")
	if err != nil {
		t.Fatalf("cold scout messages: %v", err)
	}
	if len(coldMsgs) != 3 {
		t.Fatalf("cold scout history has %d messages, want 3 (probe one, hi, answer)", len(coldMsgs))
	}
	// ... and not into the daemon's file: a second plain handle on it
	// (routing off, one file) sees no rows for the agent session.
	daemonDB, err := store.Open(datadir.DBPath(home, "omega.db"))
	if err != nil {
		t.Fatalf("reopen daemon store: %v", err)
	}
	defer daemonDB.Close()
	daemonMsgs, err := daemonDB.GetMessages(ctx, "sess-scout-2")
	if err != nil {
		t.Fatalf("daemon messages: %v", err)
	}
	if len(daemonMsgs) != 0 {
		t.Fatalf("daemon store holds %d rows of the agent turn, want 0", len(daemonMsgs))
	}
}

func TestDaemonOwnedSessionKeepsItsAgent(t *testing.T) {
	ctx := context.Background()
	_, srv, ts, daemonSeen, scoutSeen := agentHomeServer(t)

	postChatAndWait(t, ts, "sess-main")

	if got := daemonSeen.Provider.ModelName(); got != "daemon-model" {
		t.Fatalf("daemon run used model %q, want %q", got, "daemon-model")
	}
	if daemonSeen.PromptCustom != "daemon-prompt" || daemonSeen.MaxTurns != 3 {
		t.Fatalf("daemon run used prompt %q / max_turns %d, want %q / 3",
			daemonSeen.PromptCustom, daemonSeen.MaxTurns, "daemon-prompt")
	}
	if scoutSeen.Provider != nil {
		t.Fatal("the agent home's agent served a daemon-owned session")
	}
	waitFor(t, "the daemon turn to persist", func() bool {
		msgs, err := srv.store.GetMessages(ctx, "sess-main")
		return err == nil && len(msgs) >= 2
	})
	// The daemon session owns itself: no agent home answers for it.
	if got := srv.store.(agent.SessionOwnerStore).SessionOwner("sess-main"); got != "" {
		t.Fatalf("sess-main owner = %q, want empty (daemon)", got)
	}
}

// postChatAndWait sends one turn and waits for the run to finish
// persisting (the run detaches from the request; the frames tell us the
// stream ended, the store tells us the rows landed).
func postChatAndWait(t *testing.T, ts *httptest.Server, sessionID string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"messages":   []map[string]any{{"role": "user", "content": "hi"}},
		"session_id": sessionID,
	})
	res, err := http.Post(ts.URL+"/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post chat: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("chat status = %d, want 200", res.StatusCode)
	}
	rd := bufio.NewReader(res.Body)
	for {
		frame, ok := nextFrame(t, rd)
		if !ok {
			t.Fatalf("stream closed before agent_end (session %s)", sessionID)
		}
		if frame.event == "agent_end" {
			return
		}
	}
}

