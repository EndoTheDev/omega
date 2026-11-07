package api

// The shell-ring census (shrink wave 3, card t_b75df168; ADR
// Workshop/omega-shell-ring-shrink.md). agent/api is the serving
// shell's BASE set: chat + canvas + settings, and the chat floor a
// bare agent needs (sessions, the live-run stream, the tools the loop
// executes, the command palette the composer drives). Every production
// file here carries a census verdict - the table below IS the verdict
// table, mirrored in docs/architecture.md's "The shell-ring census"
// section. A NEW production file here is a new opinion in the base
// set: either it serves chat + canvas + settings directly (add it to
// this pin with its verdict) or it moves to its feature's capsule
// (harness/<name>/routes.go through api.RegisterRoute). The wave-1/2/3
// movers are gone from the tree already; this test is the law that
// keeps them gone.
//
// Wired into the same gate as the webui noun check
// (webui/test/capsule-noun.test.ts): the node gate walks agent/api too
// and forbids capsule-nouned production filenames there; this pin is
// the Go half (it knows the full census, the noun scan is the cheap
// tell).

import (
	"os"
	"strings"
	"testing"
)

// the census pin: production file -> its verdict (the chat/canvas/
// settings face it serves). Mirrored in docs/architecture.md.
var censusPin = map[string]string{
	"chat_panel.go":        "chat - the composer panel + the /chat turn handling",
	"runs.go":              "chat - the live run: the RunHub, detached runs, the SSE run frames + the store floor guards",
	"server.go":            "chat - the chat floor: /models, /chat, the session-existence floor (POST /sessions, /sessions/{id}), /tools, the command palette, canvas wiring",
	"server_atmention.go":  "chat - the @-mention expansion on the submit path (the browser never expands; /chat does)",
	"server_canvas.go":     "canvas - the transport: GET /ui/canvas snapshot + POST /ui/actions, deltas fan out on the run streams",
	"server_commands.go":   "chat - the command palette's server surface (GET /commands + POST /commands/dispatch, the composer's slash-command face over the universal registration seam)",
	"server_ephemeral.go":  "chat - ephemeral sessions: the no-persist chat mode of /chat (in-memory, eph- ids, gone at restart)",
	"server_images.go":     "chat - image serving + /attach-image (chat renders images)",
	"server_settings.go":   "settings - the third shell-ring face (GET/PUT /settings)",
	"server_transcript.go": "chat - the transcript serving (live + store-backed restore)",
	"server_variants.go":   "chat - message variants on the live run (regenerate/branch selection)",
	"wakes.go":             "chat - the yield harness's wake path (async agent loop t_f3f953d0): settled re-entry launches through the home input path, the per-home FIFO drain, the runaway guard and boot recovery",
	"web_tools.go":         "chat - the web.search/web.fetch tool pair: built-in shell vocabulary since v0.13 (shell law 1), always mounted",
}

// the cross-cutting test files: they guard the ring or package-wide
// machinery, not one production subject. Anything else must ride a
// pinned production file's name (subject_test.go).
var censusCrossTests = map[string]string{
	"api_census_test.go":    "this census pin",
	"dep_law_test.go":       "the importability law (t_905fa502)",
	"http_helpers_test.go":  "the package's HTTP helper faces",
	"server_pure_test.go":   "pure-logic tests (the http_channel law)",
	"server_retry_test.go":  "the stream retry surfaces",
	"server_test.go":        "the handler tests (the http_channel law)",
	"server_timing_test.go": "the timing-sensitive stream tests",
	"test_helpers_test.go":  "shared test fixtures",
}

// the capsule nouns (the webui noun check's list, verdict-era): a
// production filename carrying one is an opinion file squatting in
// the base set. Test files ride their subjects and are pinned above,
// so the noun scan runs on production files only.
var censusNouns = []string{
	"browser", "terminal", "notifications", "continuous", "kanban",
	"cron", "skills", "mcp", "files", "memory", "trust", "verdicts",
	"store", "sessions", "agents", "insights", "provider", "compactor",
	"prompt", "delegate", "process", "datadir",
}

func TestAPICensus(t *testing.T) {
	ents, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read agent/api: %v", err)
	}
	prod, tests := map[string]bool{}, map[string]bool{}
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			tests[name] = true
		} else {
			prod[name] = true
		}
	}
	if len(prod) == 0 {
		t.Fatal("agent/api walked empty - the census walk is broken")
	}

	// every production file is pinned, every pin exists
	for p := range prod {
		_, ok := censusPin[p]
		if !ok {
			t.Errorf("unpinned production file %s - agent/api is the chat/canvas/settings base: get a census verdict (docs/architecture.md) or move the file to its capsule", p)
		}
	}
	for p := range censusPin {
		if !prod[p] {
			t.Errorf("census pin names %s but the file is gone - drop the pin or restore the file", p)
		}
	}

	// every test file rides a pinned production subject
	for tp := range tests {
		if _, ok := censusCrossTests[tp]; ok {
			continue
		}
		stem := strings.TrimSuffix(tp, "_test.go") // e.g. server_canvas
		rides := false
		for p := range prod {
			ps := strings.TrimSuffix(p, ".go")
			if stem == ps || (strings.HasPrefix(stem, ps) && len(stem) > len(ps) && stem[len(ps)] == '_') {
				rides = true
				break
			}
		}
		if !rides {
			t.Errorf("orphan test file %s - a test file in agent/api rides its production subject's name (subject_test.go); the subject moved, the test moves with it", tp)
		}
	}
	for tp := range censusCrossTests {
		if !tests[tp] {
			t.Errorf("cross-cutting census pin names %s but the file is gone", tp)
		}
	}

	// no capsule noun in a production filename
	for p := range prod {
		lower := strings.ToLower(p)
		for _, n := range censusNouns {
			if strings.Contains(lower, n) {
				t.Errorf("%s carries the %q noun - capsule-owned opinion files do not live here (the wave-3 census); move it to harness/<capsule>/routes.go", p, n)
			}
		}
	}
}

// The removal proof at unit level (card t_b75df168): the census pin
// plus the noun scan say the base carries no capsule face, and the
// full-tree proof - build with every harness capsule dir deleted, the
// binary boots and serves chat + canvas + settings and nothing else -
// is scripts/removal-proof.sh (the lane gate re-runs it on the merged
// tip). This test checks the pin's own hygiene: no pin entry names a
// capsule noun (a moved file's verdict must leave with the file).
func TestAPICensusPinHygiene(t *testing.T) {
	for p := range censusPin {
		lower := strings.ToLower(p)
		for _, n := range censusNouns {
			if strings.Contains(lower, n) {
				t.Errorf("census pin entry %s carries the %q noun - it is a mover, not base", p, n)
			}
		}
	}
}

// harnessCapsuleGone names every production file the census says MUST
// NOT exist because its surface rode a capsule out (the waves' movers,
// kept as a tombstone list): if the noun scan or pin ever misses a
// comeback, the tombstone still holds. The names are the moved
// files' agent/api homes as the waves found them.
func TestAPICensusTombstones(t *testing.T) {
	tombstones := map[string]string{
		"server_files.go":       "t_48eeb015 - the file domain rides harness/files",
		"server_sessions.go":    "t_490fed88 - the session MANAGEMENT surface rides harness/store",
		"server_insights.go":    "t_490fed88 - GET /insights rides the store capsule",
		"server_agents.go":      "t_490fed88 - the roster rides harness/continuous",
		"server_workspaces.go":  "t_b75df168 - the workspaces picker rides harness/files",
		"server_verdicts.go":    "t_b75df168 - the verdict answer surface rides harness/verdict",
		"server_notify.go":      "t_b75df168 - the finished-things face rides harness/notifications",
		"server_notify_test.go": "t_b75df168 - rides its capsule",
		"server_skills.go":      "t_b75df168 - the skills content view rides harness/skills",
		"server_skills_test.go": "t_b75df168 - rides its capsule",
		"server_branch.go":      "t_490fed88 - /sessions/{id}/branch rides the store capsule",
	}
	ents, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read agent/api: %v", err)
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		if why, ok := tombstones[e.Name()]; ok {
			t.Errorf("%s came back - %s", e.Name(), why)
		}
	}
}
