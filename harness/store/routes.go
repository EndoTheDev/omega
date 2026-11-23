package store

// routes.go: the session-MANAGEMENT REST face.
// Missing offer = never-running: the honest empty state on a boot with no serving
// channel, never a fault.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/datadir"
)

type Routes struct {
	store *Store
	ctx   *core.Context
}

func newRoutes(ctx *core.Context, s *Store) *Routes {
	return &Routes{store: s, ctx: ctx}
}

func (rt *Routes) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (rt *Routes) httpError(w http.ResponseWriter, code int, format string, args ...any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf(format, args...)})
}

func (rt *Routes) registrationRows() []struct {
	pattern string
	handler func(http.ResponseWriter, *http.Request)
} {
	return []struct {
		pattern string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"GET /sessions", rt.handleSessionsList},
		{"GET /sessions/search", rt.handleSessionSearch},
		{"POST /sessions/reindex", rt.handleSessionReindex},
		{"POST /sessions/{id}/branch", rt.handleSessionBranch},
		{"/sessions/{id}/export", rt.handleSessionExport},
		{"GET /insights", rt.handleInsights},
	}
}

// sessionRow is one GET /sessions list row. The running flag is the backend's single
// truth for "is this session streaming" (it derives from the chat-runs service, never
// from a client's memory of its own stream), so it is ALWAYS present on the row - true
// and false alike - and every tab reconciles its pulse against it.
type sessionRow struct {
	core.Session
	MessageCount int    `json:"message_count"`
	ToolCount    int    `json:"tool_count"`
	Model        string `json:"model,omitempty"`
	Unread       bool   `json:"unread"`
	Running      bool   `json:"running"`
}

// ChatRunsReader is the consumer-owned local face of the "chat-runs" service. The
// serving channel offers the implementation; the run hub itself never leaves the chat
// surface - only this read crosses the seam. The owner never knows this face exists (no
// core-named seam type anywhere).
type ChatRunsReader interface {
	Active(id string) bool
}

func (rt *Routes) chatRuns() ChatRunsReader {
	svc, _ := rt.ctx.Services().Resolve("chat-runs").(ChatRunsReader)
	return svc
}

func (rt *Routes) running(id string) bool {
	if reader := rt.chatRuns(); reader != nil {
		return reader.Active(id)
	}
	return false
}

// handleSessionsList serves GET /sessions (the sessions browse list).
func (rt *Routes) handleSessionsList(w http.ResponseWriter, r *http.Request) {
	sessions, err := rt.store.ListSessions(r.Context())
	if err != nil {
		rt.httpError(w, http.StatusInternalServerError, "list sessions: %v", err)
		return
	}
	includeArchived := r.URL.Query().Get("archived") == "include"
	// ponytail: counts are computed at read time from SQL COUNT, so the list stays.
	counts, err := rt.store.SessionCounts(r.Context())
	if err != nil {
		rt.httpError(w, http.StatusInternalServerError, "session counts: %v", err)
		return
	}
	models, err := rt.store.SessionModels(r.Context())
	if err != nil {
		rt.httpError(w, http.StatusInternalServerError, "session models: %v", err)
		return
	}
	out := make([]sessionRow, 0, len(sessions))
	for _, sess := range sessions {
		if sess.Archived && !includeArchived {
			continue
		}
		payload := sessionRow{Session: sess}
		if c, ok := counts[sess.ID]; ok {
			payload.MessageCount = c.MessageCount
			payload.ToolCount = c.ToolCount
		}
		// The pinned model wins; the model_change trail is the fallback for sessions that
		// never carried a pin.
		payload.Model = models[sess.ID]
		if sess.Model != "" {
			payload.Model = sess.Model
		}
		payload.Unread = agent.SessionUnread(sess)
		payload.Running = rt.running(sess.ID)
		out = append(out, payload)
	}
	rt.writeJSON(w, http.StatusOK, out)
}

// searchHit is one /sessions/search row.
type searchHit struct {
	ID         string              `json:"id"`
	Snippet    string              `json:"snippet,omitempty"`
	Matches    []core.SearchMatch  `json:"matches,omitempty"`
	TitleMatch bool                `json:"titleMatch,omitempty"`
}

// handleSessionSearch serves GET /sessions/search?q=<term>.
func (rt *Routes) handleSessionSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		rt.writeJSON(w, http.StatusOK, []searchHit{})
		return
	}
	results, err := rt.store.SearchSessions(r.Context(), q)
	if err != nil {
		rt.httpError(w, http.StatusBadRequest, "search: %v", err)
		return
	}
	if r.URL.Query().Get("archived") != "include" {
		sessions, err := rt.store.ListSessions(r.Context())
		if err != nil {
			rt.httpError(w, http.StatusInternalServerError, "list sessions: %v", err)
			return
		}
		archived := make(map[string]bool, len(sessions))
		for _, sess := range sessions {
			archived[sess.ID] = sess.Archived
		}
		results = slicesCut(results, func(res core.SearchResult) bool {
			return archived[res.SessionID]
		})
	}
	out := make([]searchHit, 0, len(results))
	for _, res := range results {
		out = append(out, searchHit{ID: res.SessionID, Snippet: res.Snippet, Matches: res.Matches, TitleMatch: res.Title})
	}
	rt.writeJSON(w, http.StatusOK, out)
}

// reindexReport is the POST /sessions/reindex answer: what the rebuild covered.
type reindexReport struct {
	Rebuilt  bool `json:"rebuilt"`
	Sessions int  `json:"sessions"`
}

// handleSessionReindex serves POST /sessions/reindex - the manual rebuild of the derived
// index from the session files. It is the corruption-recovery door: search results after
// a rebuild are the same fold of the same files, so the only thing it can change is a
// stale or damaged index.
func (rt *Routes) handleSessionReindex(w http.ResponseWriter, r *http.Request) {
	if err := rt.store.RebuildIndex(r.Context()); err != nil {
		rt.httpError(w, http.StatusInternalServerError, "reindex: %v", err)
		return
	}
	sessions, err := rt.store.sessionsOnDisk()
	if err != nil {
		rt.httpError(w, http.StatusInternalServerError, "reindex: %v", err)
		return
	}
	rt.writeJSON(w, http.StatusOK, reindexReport{Rebuilt: true, Sessions: len(sessions)})
}

func slicesCut[T any](s []T, drop func(T) bool) []T {
	out := make([]T, 0, len(s))
	for _, v := range s {
		if !drop(v) {
			out = append(out, v)
		}
	}
	return out
}

// handleSessionBranch serves POST /sessions/{id}/branch (/branch parity).
func (rt *Routes) handleSessionBranch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		rt.httpError(w, http.StatusBadRequest, "missing session id")
		return
	}
	parent, err := rt.store.GetSession(r.Context(), id)
	if err != nil {
		rt.httpError(w, http.StatusNotFound, "session not found: %v", err)
		return
	}
	childID, err := agent.NewSessionID()
	if err != nil {
		childID = fmt.Sprintf("sess-%d", time.Now().UnixNano())
	}
	if err := rt.store.CreateSession(r.Context(), childID, id, ""); err != nil {
		rt.httpError(w, http.StatusInternalServerError, "branch session: %v", err)
		return
	}
	summary := "branched from " + id
	if parent.Label != "" {
		summary = "branched from " + parent.Label + " (" + id + ")"
	}
	if err := rt.store.AppendMessage(r.Context(), childID, core.NewSystem(summary)); err != nil {
		rt.httpError(w, http.StatusInternalServerError, "summary message: %v", err)
		return
	}
	sess, err := rt.store.GetSession(r.Context(), childID)
	if err != nil {
		rt.httpError(w, http.StatusInternalServerError, "get session: %v", err)
		return
	}
	rt.writeJSON(w, http.StatusCreated, sess)
}

// exportLine is one JSONL line of GET /sessions/{id}/export.
type exportLine struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// handleSessionExport serves GET /sessions/{id}/export (/export parity).
func (rt *Routes) handleSessionExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		rt.httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		rt.httpError(w, http.StatusBadRequest, "missing session id")
		return
	}
	sess, err := rt.store.GetSession(r.Context(), id)
	if err != nil {
		rt.httpError(w, http.StatusNotFound, "session not found: %v", err)
		return
	}
	messages, err := rt.store.GetActiveMessages(r.Context(), id)
	if err != nil {
		rt.httpError(w, http.StatusInternalServerError, "get messages: %v", err)
		return
	}
	w.Header().Set("Content-Type", "application/jsonl")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.jsonl"`, sess.ID))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	for _, msg := range messages {
		line := exportLine{Role: core.MessageRole(msg), Content: agent.MessageText(msg)}
		if _, err := fmt.Fprintf(w, "{\"role\":%q,\"content\":%q}\n", line.Role, line.Content); err != nil {
			return
		}
	}
}

type insightsRow struct {
	Name  string `json:"name,omitempty"`
	Count int    `json:"count"`
}

type insightsDayRow struct {
	Day   string `json:"day"`
	Count int    `json:"count"`
}

type insightsNotableRow struct {
	Value  int    `json:"value"`
	Detail string `json:"detail"`
}

type insightsPayload struct {
	Period         string             `json:"period"`
	PeriodStart    string             `json:"period_start"`
	PeriodEnd      string             `json:"period_end"`
	Days           int                `json:"days"`
	Sessions       int                `json:"sessions"`
	Messages       int                `json:"messages"`
	UserMessages   int                `json:"user_messages"`
	ToolCalls      int                `json:"tool_calls"`
	TotalTokens    int                `json:"total_tokens"`
	AvgSessionMsgs float64            `json:"avg_session_msgs"`
	Tools          []insightsRow      `json:"tools"`
	Daily          [7]insightsDayRow  `json:"daily"`
	NotableMsgs    insightsNotableRow `json:"notable_msgs"`
	NotableTokens  insightsNotableRow `json:"notable_tokens"`
	NotableTools   insightsNotableRow `json:"notable_tools"`
	DBSizeBytes    int64              `json:"db_size_bytes"`
	WALSizeBytes   int64              `json:"wal_size_bytes"`
}

// insightsPayloadOf copies the computed Insights into the wire shape. One place to map,
// so a future store-field addition has exactly one place to surface.
func insightsPayloadOf(in *core.Insights) insightsPayload {
	p := insightsPayload{
		Period:         in.Period,
		PeriodStart:    in.PeriodStart,
		PeriodEnd:      in.PeriodEnd,
		Days:           in.Days,
		Sessions:       in.Sessions,
		Messages:       in.Messages,
		UserMessages:   in.UserMessages,
		ToolCalls:      in.ToolCalls,
		TotalTokens:    in.TotalTokens,
		AvgSessionMsgs: in.AvgSessionMsgs,
		// arrays stay arrays on the wire: a nil Go slice marshals as null,.
		Tools:       []insightsRow{},
		NotableMsgs: insightsNotableRow{Value: in.NotableMsgs.Value, Detail: in.NotableMsgs.Detail},
		DBSizeBytes:  in.DBSizeBytes,
		WALSizeBytes: in.WALSizeBytes,
	}
	p.NotableTokens = insightsNotableRow{Value: in.NotableTokens.Value, Detail: in.NotableTokens.Detail}
	p.NotableTools = insightsNotableRow{Value: in.NotableTools.Value, Detail: in.NotableTools.Detail}
	for _, t := range in.Tools {
		p.Tools = append(p.Tools, insightsRow{Name: t.Name, Count: t.Count})
	}
	for i, d := range in.Daily {
		p.Daily[i] = insightsDayRow{Day: d.Day, Count: d.Count}
	}
	return p
}

// handleInsights serves GET /insights?days=N (the insights panel's read. days semantics
// mirror HandleInsightsCommand exactly: default 30, an explicit value >= 0 is honored
// (0 = all time), anything else falls back to the default.
func (rt *Routes) handleInsights(w http.ResponseWriter, r *http.Request) {
	days := 30
	if d, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("days"))); err == nil && d >= 0 {
		days = d
	}
	in, err := rt.store.ComputeInsights(r.Context(), days)
	if err != nil {
		rt.httpError(w, http.StatusInternalServerError, "insights: %v", err)
		return
	}
	rt.writeJSON(w, http.StatusOK, insightsPayloadOf(in))
}

// DefaultSupplyPath is the substrate-class default supply's database path.
func DefaultSupplyPath() string {
	home, err := datadir.AgentHome()
	if err != nil || home == "" {
		return "omega.db"
	}
	return datadir.DBPath(home, "omega.db")
}
