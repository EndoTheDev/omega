package main

// The desktop's update affordance against a real HTTP server: it never owns an updater,
// it drives the home's own routes.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// updateServer scripts the update channel's four routes.
func updateServer(t *testing.T, status core.UpdateStatus, onFinalize func(body map[string]any)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /updates/status", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(status)
	})
	mux.HandleFunc("POST /updates/check", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(status)
	})
	mux.HandleFunc("POST /updates/finalize", func(w http.ResponseWriter, r *http.Request) {
		if onFinalize != nil {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			onFinalize(body)
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(status)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestTrayUpdatesOffersRestartWhenReady: the row is one door - a staged release asks, and
// the yes path posts the forced finalize.
func TestTrayUpdatesOffersRestartWhenReady(t *testing.T) {
	posted := make(chan map[string]any, 1)
	srv := updateServer(t, core.UpdateStatus{State: updateReady, Current: "v0.19.0", StagedVer: "v0.19.1"}, func(body map[string]any) {
		posted <- body
	})
	var confirmed string
	var info string
	tray := newTrayUpdates(srv.URL,
		func(_, body string) { info = body },
		func(_, body string, yes func()) { confirmed = body; yes() },
	)
	tray.act()
	if !strings.Contains(confirmed, "v0.19.1") {
		t.Fatalf("the confirm prompt = %q, want the staged version", confirmed)
	}
	if info != "" {
		t.Fatalf("a ready release raised an info dialog (%q) instead of the restart prompt", info)
	}
	select {
	case body := <-posted:
		if forced, _ := body["force"].(bool); !forced {
			t.Fatalf("finalize body = %v, want force=true (a human clicked the restart)", body)
		}
	default:
		t.Fatal("the restart never posted /updates/finalize")
	}
}

// TestTrayUpdatesChecksWhenIdle: no staged release means a check, and its answer is the
// dialog - available, or up to date.
func TestTrayUpdatesChecksWhenIdle(t *testing.T) {
	srv := updateServer(t, core.UpdateStatus{State: updateAvailable, Current: "v0.19.0", Available: "v0.19.1", Channel: "stable"}, nil)
	var info string
	tray := newTrayUpdates(srv.URL,
		func(_, body string) { info = body },
		func(_, _ string, _ func()) { t.Fatal("an idle home asked for a restart") },
	)
	tray.act()
	if !strings.Contains(info, "v0.19.1") || !strings.Contains(info, "Settings > Updates") {
		t.Fatalf("check dialog = %q, want the available version and the pane hint", info)
	}
}

// TestTrayUpdatesShowsRollbackReceipt: every rollback shows its receipt - version, why,
// what was restored.
func TestTrayUpdatesShowsRollbackReceipt(t *testing.T) {
	st := core.UpdateStatus{
		State:   updateUpToDate,
		Current: "v0.19.0",
		Receipt: &core.UpdateReceipt{Version: "v0.19.1", Outcome: "rollback", Why: "health window failed", Restored: []string{"omega.db", "config.yaml"}},
	}
	srv := updateServer(t, st, nil)
	var info string
	tray := newTrayUpdates(srv.URL, func(_, body string) { info = body }, func(_, _ string, _ func()) {})
	tray.act()
	for _, want := range []string{"v0.19.1", "health window failed", "omega.db, config.yaml"} {
		if !strings.Contains(info, want) {
			t.Fatalf("receipt dialog = %q, missing %q", info, want)
		}
	}
}

// TestTrayMenuCarriesUpdateRow: the row sits before Quit and only when the affordance is
// bound - a hand-built shell (tests) keeps its three rows.
func TestTrayMenuCarriesUpdateRow(t *testing.T) {
	shell, probe := newTrayShell()
	shell.setUpdates(newTrayUpdates("http://127.0.0.1:1", func(_, _ string) {}, func(_, _ string, _ func()) {}))
	menu := shell.menu()
	if len(menu) != 4 {
		t.Fatalf("menu has %d rows, want 4", len(menu))
	}
	if menu[2].Label != trayUpdateLabel || menu[3].Label != "Quit" {
		t.Fatalf("menu = %q, want the update row before Quit", []string{menu[2].Label, menu[3].Label})
	}
	menu[2].Action() // unreachable origin: the dialog reports it, nothing panics
	if probe.quit != 0 {
		t.Fatal("the update row quit the shell")
	}
}
