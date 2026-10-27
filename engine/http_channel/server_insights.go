package http_channel

// server_insights.go: GET /insights?days=N - the webui's /insights
// parity (card t_e1e61680). The aggregation is store-owned
// (ComputeInsights on the StoreProvider seam); the TUI's
// HandleInsightsCommand calls the same store surface, so both hosts
// read identical numbers by construction. This file only shapes the
// payload. NOTE (card t_e1e61680, same-file ruling): the mux mount
// line `mux.HandleFunc("/insights", s.handleInsights)` is deferred to
// the review pass - server.go is owned by card t_d7a7beaa while it
// runs.

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// insightsPayload is the wire shape of one /insights response. It
// mirrors core.Insights field-for-field (minus the TUI's Bar glyphs -
// the webui renders bar width from Count itself). The notable stats
// keep the TUI's honest zero-omission: a NotableStat of 0 renders as
// nothing on the client, not as a fake row.
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
	Tools          []insightsToolRow  `json:"tools"`
	Daily          [7]insightsDayRow  `json:"daily"`
	NotableMsgs    insightsNotableRow `json:"notable_msgs"`
	NotableTokens  insightsNotableRow `json:"notable_tokens"`
	NotableTools   insightsNotableRow `json:"notable_tools"`
}

type insightsToolRow struct {
	Name  string `json:"name"`
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
		Tools:         []insightsToolRow{},
		NotableMsgs:   insightsNotableRow{Value: in.NotableMsgs.Value, Detail: in.NotableMsgs.Detail},
		NotableTokens: insightsNotableRow{Value: in.NotableTokens.Value, Detail: in.NotableTokens.Detail},
		NotableTools:  insightsNotableRow{Value: in.NotableTools.Value, Detail: in.NotableTools.Detail},
	}
	for _, t := range in.Tools {
		p.Tools = append(p.Tools, insightsToolRow{Name: t.Name, Count: t.Count})
	}
	for i, d := range in.Daily {
		p.Daily[i] = insightsDayRow{Day: d.Day, Count: d.Count}
	}
	return p
}

// handleInsights serves GET /insights?days=N. days semantics mirror
// HandleInsightsCommand exactly: default 30, an explicit value >= 0 is
// honored (0 = all time), anything else falls back to the default.
func (s *Server) handleInsights(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		http.Error(w, "session store not configured", http.StatusNotImplemented)
		return
	}
	days := 30
	if d, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("days"))); err == nil && d >= 0 {
		days = d
	}
	in, err := s.store.ComputeInsights(r.Context(), days)
	if err != nil {
		http.Error(w, "insights: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, insightsPayloadOf(in))
}
