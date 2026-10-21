package http_channel

// Branch + export endpoint tests (t_6bf696e4): the webui parity batch
// for the TUI's /branch, /label and /export commands.
//
//   - POST /sessions/{id}/branch creates a child of the parent with a
//     summary system message in the child's rows (the /branch parity -
//     HandleBranchCommand's machinery at the store, one lineage marker
//     row the counts and the tree both exclude).
//   - GET /sessions rows carry parent_id (sessionPayload embeds
//     agent.Session, which serializes it).
//   - The child's active view (and therefore its transcript + its next
//     run's context) inherits the parent's history as a prefix.
//   - GET /sessions/{id}/export writes the active path as one
//     {"role","content"} JSONL line per message - the TUI /export shape.
//   - A message appended to the child chains onto the ancestor leaf
//     (cross-session pointer accepted).

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/ai"
)

func TestBranchEndpointCreatesChild(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	createSession(t, ts, "par")

	code, child := postJSON(t, ts.URL+"/sessions/par/branch", "")
	if code != http.StatusCreated {
		t.Fatalf("branch status = %d, want 201", code)
	}
	childID, _ := child["parent_id"].(string)
	id, _ := child["id"].(string)
	if id == "" || id == "par" {
		t.Fatalf("child id = %q, want a fresh id distinct from the parent", id)
	}
	if childID != "par" {
		t.Fatalf("child parent_id = %q, want par", childID)
	}

	// Rows carry parent_id.
	resp, err := http.Get(ts.URL + "/sessions")
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	defer resp.Body.Close()
	var rows []map[string]any
	json.NewDecoder(resp.Body).Decode(&rows)
	var childRow map[string]any
	for _, row := range rows {
		if row["id"] == id {
			childRow = row
		}
	}
	if childRow == nil || childRow["parent_id"] != "par" {
		t.Fatalf("rows = %v, want child row with parent_id par", rows)
	}

	// The summary system row lives in the child's rows (role system).
	stored, err := st.GetMessages(context.Background(), id)
	if err != nil {
		t.Fatalf("get child messages: %v", err)
	}
	found := false
	for _, m := range stored {
		sys, ok := m.(ai.System)
		if ok && strings.Contains(sys.Content, "branched from par") {
			found = true
		}
	}
	if !found {
		t.Fatalf("child rows %v, want one summary system row naming the parent", stored)
	}

	// Counts exclude the summary row (message_count 0, the child has no
	// conversation messages yet).
	counts, err := st.SessionCounts(context.Background())
	if err != nil {
		t.Fatalf("session counts: %v", err)
	}
	if counts[id].MessageCount != 0 {
		t.Fatalf("child message_count = %d, want 0 (summary row excluded)", counts[id].MessageCount)
	}
}

func TestBranchEndpointUnknownParent(t *testing.T) {
	s, _ := newTestServerWithStore(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	code, _ := postJSON(t, ts.URL+"/sessions/missing/branch", "")
	if code != http.StatusNotFound {
		t.Fatalf("branch status = %d, want 404", code)
	}
}

// TestBranchChildInheritsAncestorPath: the child's active view - the
// read the transcript AND the context assembly use - starts with the
// parent's conversation.
func TestBranchChildInheritsAncestorPath(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	createSession(t, ts, "src")

	// seed the parent by hand (no provider needed here)
	if err := st.AppendMessage(context.Background(), "src", ai.NewUser("parent question")); err != nil {
		t.Fatalf("seed parent user: %v", err)
	}
	if err := st.AppendMessage(context.Background(), "src", ai.NewAssistant("parent answer")); err != nil {
		t.Fatalf("seed parent assistant: %v", err)
	}

	code, child := postJSON(t, ts.URL+"/sessions/src/branch", "")
	if code != http.StatusCreated {
		t.Fatalf("branch status = %d, want 201", code)
	}
	childID, _ := child["id"].(string)

	// Transcript of the child carries the inherited prefix.
	code, tx := transcriptOf(t, ts, childID)
	if code != http.StatusOK {
		t.Fatalf("child transcript status = %d, want 200", code)
	}
	items, _ := tx["items"].([]any)
	got := itemContents(items)
	want := []string{"user:parent question", "message:parent answer"}
	if len(got) < 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("child transcript = %v, want inherited prefix %v (then the summary)", got, want)
	}

	// The first message appended to the child chains onto the ancestor
	// leaf: an explicitly pointed user message passes validation, then
	// the next continues by adjacency.
	view, err := st.GetActiveView(context.Background(), childID)
	if err != nil {
		t.Fatalf("child active view: %v", err)
	}
	leaf := view.Path[len(view.Path)-1:][0]
	if leaf.Message.(ai.Assistant).Content != "parent answer" {
		t.Fatalf("child path leaf = %+v, want the parent assistant", leaf)
	}
	if err := st.AppendMessage(context.Background(), childID, ai.NewUser("child question")); err != nil {
		t.Fatalf("append child message: %v", err)
	}
	view, err = st.GetActiveView(context.Background(), childID)
	if err != nil {
		t.Fatalf("child active view after append: %v", err)
	}
	if _, ok := view.Path[len(view.Path)-1].Message.(ai.User); !ok {
		t.Fatalf("child path after append = %T, want the child user last", view.Path[len(view.Path)-1].Message)
	}
}

// TestExportJSONLShape: the export endpoint emits one
// {"role","content"} line per active-path message (the TUI /export
// shape) as an attachment download.
func TestExportJSONLShape(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	createSession(t, ts, "exp1")
	if err := st.AppendMessage(context.Background(), "exp1", ai.NewUser("hello there")); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := st.AppendMessage(context.Background(), "exp1", ai.NewAssistant("hi, human")); err != nil {
		t.Fatalf("seed assistant: %v", err)
	}

	resp, err := http.Get(ts.URL + "/sessions/exp1/export")
	if err != nil {
		t.Fatalf("export get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status = %d, want 200", resp.StatusCode)
	}
	cd := resp.Header.Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") || !strings.Contains(cd, `exp1.jsonl`) {
		t.Fatalf("content-disposition = %q, want attachment named exp1.jsonl", cd)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/jsonl") {
		t.Fatalf("content-type = %q, want application/jsonl", ct)
	}
	var lines []map[string]string
	dec := json.NewDecoder(resp.Body)
	for dec.More() {
		var line map[string]string
		if err := dec.Decode(&line); err != nil {
			t.Fatalf("decode line: %v", err)
		}
		lines = append(lines, line)
	}
	if len(lines) != 2 ||
		lines[0]["role"] != "user" || lines[0]["content"] != "hello there" ||
		lines[1]["role"] != "assistant" || lines[1]["content"] != "hi, human" {
		t.Fatalf("export lines = %v, want TUI /export shape {role, content} per message", lines)
	}

	// 404 for a missing session.
	if resp, err := http.Get(ts.URL + "/sessions/missing/export"); err == nil {
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("export missing status = %d, want 404", resp.StatusCode)
		}
	}
}

// HEAD export: same headers as the GET (the download probe - the webui
// anchor, curl -I - reads Content-Disposition without a body). The
// branch tests lean on the shape test for the GET.
func TestExportHead(t *testing.T) {
	s, st := newTestServerWithStore(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	createSession(t, ts, "exph")
	if err := st.AppendMessage(context.Background(), "exph", ai.NewUser("head seed")); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	resp, err := http.Head(ts.URL + "/sessions/exph/export")
	if err != nil {
		t.Fatalf("export head: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export head status = %d, want 200", resp.StatusCode)
	}
	cd := resp.Header.Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") || !strings.Contains(cd, `exph.jsonl`) {
		t.Fatalf("head content-disposition = %q, want attachment named exph.jsonl", cd)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(string(body)) != 0 {
		t.Fatalf("head body = %q, want empty", body)
	}
}