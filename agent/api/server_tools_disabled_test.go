package api

// GET /tools?disabled=1 - the fold-aware management read (card
// t_5c9bb50a, Endo's report: "i disabled web, and now its so gone, i
// cant even re-enable it"). The BARE read stays the executable
// registry the chat loop consumes - disabled tools are absent and the
// wire carries no new key. The fold-aware read serves the settings
// tab: the same union BEFORE the fold filters it, every entry stamped
// with the fold's verdict, so a disabled tool keeps its row and its
// re-enable toggle. The one-way door (a disabled tool vanishing from
// the only surface that could flip it back) is the red shape both
// halves below pin dead.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

func TestToolsFoldAwareRead(t *testing.T) {
	registry := map[string]core.Tool{
		"shell.run":  {Description: "run a shell command"},
		"web.search": {Description: "search the web"},
		"web.fetch":  {Description: "fetch a page"},
	}
	a := agent.NewAgent(nil, registry, 0)
	s := NewServer(a, nil, nil, nil, nil, nil)

	// the fold switches web.* off - the exact live-lane report shape
	a.SetDisabledTools([]string{"web.search", "web.fetch"})

	// bare read: the executable registry - web.* absent (the model
	// must not see disabled tools), and the wire carries no new key
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tools", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tools: %d %s", rec.Code, rec.Body)
	}
	var bare []toolListing
	if err := json.Unmarshal(rec.Body.Bytes(), &bare); err != nil {
		t.Fatalf("decode bare: %v (%s)", err, rec.Body)
	}
	if len(bare) != 1 || bare[0].Name != "shell.run" {
		t.Fatalf("bare inventory = %+v, want shell.run only (web.* folded off)", bare)
	}
	if strings.Contains(rec.Body.String(), "disabled") {
		t.Fatalf("bare /tools wire gained a key: %s", rec.Body)
	}

	// fold-aware read: the SAME union pre-fold, web.* back with the
	// stamp - the settings tab can render and re-enable them
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/tools?disabled=1", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET /tools?disabled=1: %d %s", rec2.Code, rec2.Body)
	}
	var fold []toolListing
	if err := json.Unmarshal(rec2.Body.Bytes(), &fold); err != nil {
		t.Fatalf("decode fold-aware: %v (%s)", err, rec2.Body)
	}
	if len(fold) != 3 {
		t.Fatalf("fold-aware inventory = %+v, want all 3 rows (the fold must not shrink the management surface)", fold)
	}
	byName := map[string]toolListing{}
	for _, row := range fold {
		byName[row.Name] = row
	}
	for _, name := range []string{"web.search", "web.fetch"} {
		if !byName[name].Disabled {
			t.Fatalf("%s: fold-aware row not stamped disabled: %+v", name, byName[name])
		}
	}
	if byName["shell.run"].Disabled {
		t.Fatalf("shell.run: an on-tool must never carry the disabled stamp: %+v", byName["shell.run"])
	}
	// the re-enable path is data the PUT already serves: the names the
	// fold stores are exactly the names the read kept
	for _, name := range []string{"web.search", "web.fetch"} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("%s vanished from the fold-aware read - the one-way door: %s", name, rec2.Body)
		}
	}

	// partial fold: one member off stamps only that member
	a.SetDisabledTools([]string{"web.search"})
	rec3 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec3, httptest.NewRequest(http.MethodGet, "/tools?disabled=1", nil))
	var partial []toolListing
	if err := json.Unmarshal(rec3.Body.Bytes(), &partial); err != nil {
		t.Fatalf("decode partial: %v (%s)", err, rec3.Body)
	}
	for _, row := range partial {
		want := row.Name == "web.search"
		if row.Disabled != want {
			t.Fatalf("%s: disabled = %v, want %v", row.Name, row.Disabled, want)
		}
	}
}

// TestToolsFoldAwareReadHonorOtherValues pins the query contract: any
// value other than the literal 1 is the bare read - a stray consumer
// appending "?disabled=true" must not silently leak disabled tools to
// the model.
func TestToolsFoldAwareReadHonorOtherValues(t *testing.T) {
	registry := map[string]core.Tool{
		"web.search": {Description: "search the web"},
	}
	a := agent.NewAgent(nil, registry, 0)
	a.SetDisabledTools([]string{"web.search"})
	s := NewServer(a, nil, nil, nil, nil, nil)

	for _, q := range []string{"", "?disabled=true", "?disabled=", "?fold=1"} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tools"+q, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /tools%s: %d %s", q, rec.Code, rec.Body)
		}
		if strings.Contains(rec.Body.String(), "web.search") {
			t.Fatalf("GET /tools%s leaked a folded tool to the executable registry: %s", q, rec.Body)
		}
	}
}
