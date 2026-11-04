package store

// routes.go: the session-MANAGEMENT REST face (the shell-ring shrink
// wave 2, the three-layer sessions law - ADR Workshop/omega-shell-ring-shrink.md).
//
//	GET  /sessions               the sessions list (browse)
//	GET  /sessions/search?q=     the unified FTS search (the search seat)
//	POST /sessions/{id}/branch   fork a session (the /branch parity)
//	GET  /sessions/{id}/export   the JSONL export (the /export parity)
//	GET  /insights               the cross-session analytics view
//
// The chat floor stays in the base set (agent/api/server.go): POST
// /sessions (session id creation), /sessions/{id} (detail, the
// remembered send context, the ephemeral close) and the live-run reads
// (events/stop/transcript/variants). The split point (the ADR): an
// endpoint the chat page calls live = base; an endpoint the sessions
// list/inspector calls = management = this capsule. The list/search/
// branch/export/insights endpoints exist only to serve the sessions
// surface and the insights panel - they mount and die with this
// capsule, and the capsule's own Open'd store answers them.
//
// INSIGHTS DISCLOSURE (the ADR's open call): insights rides THIS
// capsule, not an own harness/insights module. It is a pure view over
// store data (ComputeInsights on the same *Store the list reads) -
// an insights capsule would carry no seam of its own, mirror the same
// store requirement, and split one deletion proof (sessions+insights
// leave together, "removing store removes sessions+insights") into two
// coupled strands. One capsule, one strand, one deletability proof.
//
// The list's running flag resolves the "chat-runs" service per request
// through this capsule's OWN local ChatRunsReader face (the
// consumer-owned-face law, the seam constitution; the live run hub
// is offered by the serving channel). Missing offer = never-running:
// the honest empty state on a boot with no serving channel, never a
// fault.

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

// Routes is the capsule's management REST face, built on the store
// instance the plugin opened at mount. One Routes per mount; every
// handler reads the same handle.
type Routes struct {
	store *Store
	ctx   *core.Context
}

// newRoutes builds the face. ctx is the mount context - the handlers
// resolve the chat-runs service through it per request (the live
// re-wire law: the read follows whatever the serving channel offered).
func newRoutes(ctx *core.Context, s *Store) *Routes {
	return &Routes{store: s, ctx: ctx}
}

// writeJSON renders one 200-family response.
func (rt *Routes) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// httpError renders one failure; detail is operator-facing truth, the
// webui renders what it gets.
func (rt *Routes) httpError(w http.ResponseWriter, code int, format string, args ...any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf(format, args...)})
}

// routes returns the REST registrations in mount order. Patterns are
// exactly the ones the pre-move base set served (the wave-2 proof: no
// behavior change on a normal boot, only ownership).
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
		{"POST /sessions/{id}/branch", rt.handleSessionBranch},
		{"/sessions/{id}/export", rt.handleSessionExport},
		{"GET /insights", rt.handleInsights},
	}
}

// sessionRow is one GET /sessions list row: the session row (which
// carries the pinned flag, the persisted token totals, and the
// last-read stamp) plus the read-time counts, the last recorded model,
// the read-time unread state, and whether the session has a live run
// right now (the sidebar's streaming pulse after a page refresh - the
// in-memory busy map no longer survives the reload). The running flag
// is the backend's single truth for "is this session streaming" (it
// derives from the chat-runs service, never from a client's memory of
// its own stream), so it is ALWAYS present on the row - true and false
// alike - and every tab reconciles its pulse against it.
type sessionRow struct {
	core.Session
	MessageCount int    `json:"message_count"`
	ToolCount    int    `json:"tool_count"`
	Model        string `json:"model,omitempty"`
	Unread       bool   `json:"unread"`
	Running      bool   `json:"running"`
}

// ChatRunsReader is the consumer-owned local face of the "chat-runs"
// service (the first shell-offered capsule-service key, Endo ruling
// 2026-11-04: owner = the serving shell; consumers = any capsule
// reading live-run state through their own interface; missing offer =
// honest never-running). The serving channel offers the
// implementation; the run hub itself never leaves the chat surface -
// only this read crosses the seam. The owner never knows this face
// exists (no core-named seam type anywhere).
type ChatRunsReader interface {
	Active(id string) bool
}

// chatRuns resolves the live-run read per request.
func (rt *Routes) chatRuns() ChatRunsReader {
	svc, _ := rt.ctx.Services().Resolve("chat-runs").(ChatRunsReader)
	return svc
}

// running stamps the row's running flag through the chat-runs service.
func (rt *Routes) running(id string) bool {
	if reader := rt.chatRuns(); reader != nil {
		return reader.Active(id)
	}
	return false
}

// handleSessionsList serves GET /sessions (the sessions browse list) -
// formerly the base GET /sessions. It is the chat floor's complement:
// the id creation lives in the base set, the browsing is this capsule.
func (rt *Routes) handleSessionsList(w http.ResponseWriter, r *http.Request) {
	sessions, err := rt.store.ListSessions(r.Context())
	if err != nil {
		rt.httpError(w, http.StatusInternalServerError, "list sessions: %v", err)
		return
	}
	// Archived filter: default excludes archived sessions from the
	// list; archived=include returns them (with the rest). The flag
	// is a user-list concern, so the filter lives at the read.
	includeArchived := r.URL.Query().Get("archived") == "include"
	// ponytail: counts are computed at read time from SQL COUNT, so
	// the list stays accurate with no maintained counters.
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
		// The pinned model wins; the model_change trail is the
		// fallback for sessions that never carried a pin (card t_9de5b009).
		payload.Model = models[sess.ID]
		if sess.Model != "" {
			payload.Model = sess.Model
		}
		// computed at read time from the row itself - no counters,
		// no schema writes (same pattern as the counts)
		payload.Unread = agent.SessionUnread(sess)
		payload.Running = rt.running(sess.ID)
		out = append(out, payload)
	}
	rt.writeJSON(w, http.StatusOK, out)
}

// searchHit is one /sessions/search row: the matching session id plus
// the hit (a title hit carries the label with titleMatch set; a
// content hit carries a snippet stub - a later card renders the
// context).
type searchHit struct {
	ID         string `json:"id"`
	Snippet    string `json:"snippet,omitempty"`
	TitleMatch bool   `json:"titleMatch,omitempty"`
}

// handleSessionSearch serves GET /sessions/search?q=<term>: the store's
// unified FTS5 search (SearchSessions) - one endpoint, one result
// shape, session labels AND message content in the same index pass,
// deduped to one hit per session with title hits ranked first. It
// answers 200 [] on a missing/empty q (nothing to search), and 400
// when FTS5 rejects the query syntax.
func (rt *Routes) handleSessionSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		rt.writeJSON(w, http.StatusOK, []searchHit{})
		return
	}
	results, err := rt.store.SearchSessions(r.Context(), q)
	if err != nil {
		// The only input-dependent failure mode of this read-only
		// query is the query itself; the sqlite driver surfaces FTS5
		// syntax errors as plain "SQL logic error" strings, so the
		// bad call gets 400 (a real store fault trips on the /sessions
		// list too, not just here).
		rt.httpError(w, http.StatusBadRequest, "search: %v", err)
		return
	}
	// Archived filter: default excludes hits whose session is archived
	// (the sidebar's toggle drives the same archived=include param the
	// list takes); archived=include keeps them. One read of the session
	// list builds the exclusion set - the search stays a pure FTS
	// query, the flag is a user-list concern applied at read time.
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
		out = append(out, searchHit{ID: res.SessionID, Snippet: res.Snippet, TitleMatch: res.Title})
	}
	rt.writeJSON(w, http.StatusOK, out)
}

// slicesCut drops the elements the keep-predicate rejects (the base
// set's name was slices.DeleteFunc).
func slicesCut[T any](s []T, drop func(T) bool) []T {
	out := make([]T, 0, len(s))
	for _, v := range s {
		if !drop(v) {
			out = append(out, v)
		}
	}
	return out
}

// handleSessionBranch serves POST /sessions/{id}/branch (/branch
// parity): creates a child session of id through the same machinery
// the command drives (CreateSession with the parent link - the store
// rejects a missing parent), then appends ONE summary system row into
// the child so the lineage marker lives in the store (the webui paints
// the equivalent "[branched from ...]" affordance client-side; the
// webui row badge reads parent_id). Read-time semantics ride the
// store's merged view: the child's active path is the whole ancestor
// chain, so the transcript restore and the context assembly both
// inherit the parent's history as a prefix. 404 on an unknown parent,
// 201 with the child session (the shape POST /sessions already
// returns).
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
	// New child id - the same fallback the POST /sessions flow uses
	// (crypto/rand never fails on supported platforms; a timestamp
	// keeps the branch alive if it ever does).
	childID, err := agent.NewSessionID()
	if err != nil {
		childID = fmt.Sprintf("sess-%d", time.Now().UnixNano())
	}
	if err := rt.store.CreateSession(r.Context(), childID, id, ""); err != nil {
		rt.httpError(w, http.StatusInternalServerError, "branch session: %v", err)
		return
	}
	// Summary system row: excluded from counts and the message tree, so
	// only the transcript-side lineage marker and this record exist.
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

// exportLine is one JSONL line of GET /sessions/{id}/export - the exact
// shape the /export command writes{"role": "<role>", "content":
// "<text>"} per message.
type exportLine struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// handleSessionExport serves GET /sessions/{id}/export (/export
// parity): the session's ACTIVE PATH as JSONL - one {"role","content"}
// line per conversation message, the same shape HandleExportCommand
// writes to disk for the channel. For a branched session the active
// path is the whole lineage (store-side ancestors), so the exported
// file matches what the webui renders. Served as an attachment
// (Content-Disposition download named <session id>.jsonl); 404 for an
// unknown session, 500 on store faults.
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
		// headers only - the download probe (the webui anchor, curl -I)
		// reads Content-Disposition without the body; net/http would
		// discard a written body for HEAD anyway
		return
	}
	for _, msg := range messages {
		line := exportLine{Role: core.MessageRole(msg), Content: agent.MessageText(msg)}
		if _, err := fmt.Fprintf(w, "{\"role\":%q,\"content\":%q}\n", line.Role, line.Content); err != nil {
			return
		}
	}
}

// insightsRow shapes one tools-count row / day row / notable row of
// the insights payload (the webui renders bar width from Count itself).
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

// insightsPayload is the wire shape of one GET /insights response. It
// mirrors core.Insights field-for-field (the same shape the base set
// served pre-move).
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

// insightsPayloadOf copies the computed Insights into the wire shape.
// One place to map, so a future store-field addition has exactly one
// place to surface.
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
		// arrays stay arrays on the wire: a nil Go slice marshals as
		// null, and the webui's insights template (data.tools.length)
		// dereferences the field on every boot - the null crashed the
		// SPA boot on any lane whose window holds no tool calls.
		Tools:       []insightsRow{},
		NotableMsgs: insightsNotableRow{Value: in.NotableMsgs.Value, Detail: in.NotableMsgs.Detail},
		// Store file sizes ride every response (the webui's insights
		// pane renders them when non-zero - a ":memory:" test store
		// sends 0/0 and that is honest).
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

// handleInsights serves GET /insights?days=N (the insights panel's
// read; disclosure in the file header). days semantics mirror
// HandleInsightsCommand exactly: default 30, an explicit value >= 0 is
// honored (0 = all time), anything else falls back to the default.
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

// DefaultSupplyPath is the substrate-class default supply's database
// path for this home: <home>/data/omega.db, or the bare omega.db
// fallback when no data home resolves. It is the exact resolution the
// store plugin's mount runs (datadir.AgentHome / DBPath) - the host's
// no-capsule boot opens the same file the capsule would have, so the
// supply survives the capsule's removal (the wave-2 acceptance proof).
func DefaultSupplyPath() string {
	home, err := datadir.AgentHome()
	if err != nil || home == "" {
		return "omega.db"
	}
	return datadir.DBPath(home, "omega.db")
}
