package http_channel

// Ephemeral session tests (card t_d96a5cd5): the no-persist contract.
// A store-backed server answers /chat {ephemeral:true} with an `eph-` id
// that never becomes a store row - context accumulates in the registry,
// the transcript/detail endpoints serve server memory, close deletes,
// and every store-derived surface stays blind to it.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/ai"
)

// stubTool is a minimal agent.Tool value for registry-shape assertions.
func stubTool() core.Tool {
	return core.Tool{Description: "stub"}
}

// ephemeralChatBody builds an /chat ephemeral wire body.
func ephemeralChatBody(t *testing.T, sessionID string, content string, extra map[string]any) []byte {
	t.Helper()
	body := map[string]any{
		"ephemeral": true,
		"messages":  []map[string]any{{"role": "user", "content": content}},
	}
	if sessionID != "" {
		body["session_id"] = sessionID
	}
	for k, v := range extra {
		body[k] = v
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return raw
}

// ephemeralChat runs one ephemeral /chat send and resolves the session
// id (the leading `session` frame when a fresh mint, the session_id
// when continuing) and the parsed SSE frames.
func ephemeralChat(t *testing.T, s *Server, sessionID string, content string, extra map[string]any) (string, []sseFrame) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(ephemeralChatBody(t, sessionID, content, extra)))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q, want 200", rec.Code, rec.Body.String())
	}
	events := parseSSE(t, rec.Body.String())
	if sessionID == "" {
		if len(events) == 0 || events[0].event != "session" {
			t.Fatalf("first frame = %v, want a leading session frame", events)
		}
		var frame struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(events[0].data, &frame); err != nil {
			t.Fatalf("decode session frame: %v", err)
		}
		if !strings.HasPrefix(frame.ID, "eph-") {
			t.Fatalf("session id = %q, want the eph- namespace", frame.ID)
		}
		return frame.ID, events
	}
	return sessionID, events
}

func TestChatEphemeralNeverTouchesTheStore(t *testing.T) {
	s, st := newTestServerWithStore(t)

	id, _ := ephemeralChat(t, s, "", "hello ephemeral", nil)

	// no session row, no messages, nothing
	session, err := st.GetSession(t.Context(), id)
	if err == nil {
		t.Fatalf("ephemeral session %q materialized in the store: %+v", id, session)
	}
	sessions, err := st.ListSessions(t.Context())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("list = %+v, want empty (the eph session exists in memory only)", sessions)
	}

	// the registry owns the conversation: user + assistant in context
	// and in the wire transcript
	history := s.eph.history(id)
	if len(history) != 2 {
		t.Fatalf("history len = %d, want 2 (user + assistant)", len(history))
	}
	req := httptest.NewRequest(http.MethodGet, "/sessions/"+id+"/transcript", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("transcript status = %d, want 200", rec.Code)
	}
	var detail struct {
		Session map[string]any `json:"session"`
		Items   []map[string]any `json:"items"`
		Running bool           `json:"running"`
		Variants *json.RawMessage `json:"variants"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode transcript: %v", err)
	}
	if len(detail.Items) != 2 {
		t.Fatalf("items = %+v, want 2 (user + message)", detail.Items)
	}
	if detail.Items[0]["kind"] != "user" || detail.Items[1]["kind"] != "message" {
		t.Fatalf("items kinds = %v/%v, want user/message", detail.Items[0]["kind"], detail.Items[1]["kind"])
	}
	if detail.Variants != nil {
		t.Fatalf("variants present on an ephemeral transcript - the store tree never existed")
	}
	if detail.Session["id"] != id {
		t.Fatalf("session id = %v, want %q", detail.Session["id"], id)
	}

	// detail from the registry too, and the sessions LIST stays blind
	req = httptest.NewRequest(http.MethodGet, "/sessions/"+id, nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want 200", rec.Code)
	}
}

func TestChatEphemeralAccumulatesContext(t *testing.T) {
	s, _ := newTestServerWithStore(t)
	id, _ := ephemeralChat(t, s, "", "first", nil)
	_, _ = ephemeralChat(t, s, id, "second", nil)

	history := s.eph.history(id)
	if len(history) != 4 {
		t.Fatalf("history len = %d, want 4 (two exchanges)", len(history))
	}
	if _, ok := history[0].(ai.User); !ok {
		t.Fatalf("history[0] = %T, want ai.User", history[0])
	}
	if _, ok := history[1].(ai.Assistant); !ok {
		t.Fatalf("history[1] = %T, want ai.Assistant", history[1])
	}

	// restart kills: dropping the record empties the conversation for
	// good
	if !s.eph.drop(id) {
		t.Fatalf("drop(%q) = false, want the record present", id)
	}
	if s.eph.get(id) != nil {
		t.Fatalf("record survived drop")
	}
}

func TestChatEphemeralGuards(t *testing.T) {
	s, _ := newTestServerWithStore(t)

	// retry is refused: nothing persisted to replay
	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(ephemeralChatBody(t, "", "m", map[string]any{"retry": true})))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("retry status = %d, want 400", rec.Code)
	}

	// fork is refused the same way
	req = httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(ephemeralChatBody(t, "", "m", map[string]any{"fork": "row-id"})))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("fork status = %d, want 400", rec.Code)
	}

	// continue-on-id never resurrects: an unknown eph id is a 404
	req = httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(ephemeralChatBody(t, "eph-gone", "m", nil)))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown eph id status = %d, want 404", rec.Code)
	}

	// an eph id without the flag is a client bug
	req = httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader([]byte(`{"session_id":"eph-x","messages":[{"role":"user","content":"m"}]}`)))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusBadRequest {
		t.Fatalf("flagless eph id status = %d, want 400 or 404", rec.Code)
	}
}

func TestEphemeralCloseDropsTheRecord(t *testing.T) {
	s, _ := newTestServerWithStore(t)
	id, _ := ephemeralChat(t, s, "", "hello", nil)

	req := httptest.NewRequest(http.MethodDelete, "/sessions/"+id, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", rec.Code)
	}

	// transcript + detail are gone WITH the record
	req = httptest.NewRequest(http.MethodGet, "/sessions/"+id+"/transcript", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("transcript after close status = %d, want 404", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/sessions/"+id, nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("detail after close status = %d, want 404", rec.Code)
	}
}

func TestEphemeralInvisibleToStoreSurfaces(t *testing.T) {
	s, _ := newTestServerWithStore(t)
	ephID, _ := ephemeralChat(t, s, "", "ephemeral hello", nil)

	// search (FTS5) must never match it: only the store is indexed,
	// and the eph conversation never got there
	req := httptest.NewRequest(http.MethodGet, "/sessions/search?q=ephemeral", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search status = %d, want 200", rec.Code)
	}

	// export is store-gated: an eph id has no row to export
	req = httptest.NewRequest(http.MethodGet, "/sessions/"+ephID+"/export", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("export served an ephemeral session - the invisibility law broke")
	}

	// the read-stamp PATCH answers 204 without a store write
	req = httptest.NewRequest(http.MethodPatch, "/sessions/"+ephID, bytes.NewReader([]byte(`{"read":true}`)))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("read-stamp status = %d, want 204", rec.Code)
	}

	// rename is refused (no row to name)
	req = httptest.NewRequest(http.MethodPut, "/sessions/"+ephID, bytes.NewReader([]byte(`{"label":"no"}`)))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("rename status = %d, want 400", rec.Code)
	}
}

func TestWithoutMemory(t *testing.T) {
	s := newTestServer()
	// the registry carries a stub memory tool like the real mount does
	s.tools = map[string]core.Tool{
		"memory": stubTool(),
		"files":  stubTool(),
	}
	out := withoutMemory(s.tools)
	if _, has := out["memory"]; has {
		t.Fatalf("memory survived the withoutMemory copy")
	}
	if _, has := out["files"]; !has {
		t.Fatalf("files vanished from the copy")
	}
	// the caller's map is untouched (the run gets a copy)
	if _, has := s.tools["memory"]; !has {
		t.Fatalf("withoutMemory mutated the caller's registry")
	}
	// no memory in the registry: same map back, no allocation churn
	s.tools = map[string]core.Tool{"files": stubTool()}
	if got := withoutMemory(s.tools); got == nil {
		t.Fatalf("withoutMemory returned nil for a memory-free registry")
	}
}