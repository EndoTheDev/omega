package api

// The health popover's sourced facts (card t_5e2f3095). Three truths are
// pinned here:
//
//  1. unsourced = the OLD body, byte for byte. A process with no probe,
//     no store and no logger upgrade serves exactly what it served
//     before the facts existed - the wire stays additive.
//  2. sourced = the real value on the shared body (GET /health and the
//     pushed health topic are one body by law, so one shape is proven
//     once).
//  3. the provider probe is CACHED: reading health never probes. Five
//     reads after one probe leave the provider's call count at one.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/agent"
)

// probeProvider is a Provider whose model list fails on demand; the
// probe is the only method the health path may call on it.
type probeProvider struct {
	agent.Provider
	mu    sync.Mutex
	calls int
	err   error
}

func (p *probeProvider) ListModels() ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return []string{"m"}, nil
}

func (p *probeProvider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// pingingStore is a store that carries the optional StorePinger upgrade
// and nothing else (the embedded interface is never called).
type pingingStore struct {
	agent.StoreProvider
	err error
}

func (s pingingStore) Ping(context.Context) error { return s.err }

// plainStore carries no Ping: the health body must then carry no
// database fact at all.
type plainStore struct{ agent.StoreProvider }

// lastErrLogger carries the optional LoggerLastError upgrade.
type lastErrLogger struct {
	mu  sync.Mutex
	msg string
	at  time.Time
	had bool
}

func (l *lastErrLogger) Printf(string, ...any) {}
func (l *lastErrLogger) Warnf(string, ...any)  {}
func (l *lastErrLogger) Errorf(string, ...any) {}
func (l *lastErrLogger) Close() error          { return nil }
func (l *lastErrLogger) LastError() (string, time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.msg, l.at, l.had
}

// quietLogger is a logger WITHOUT the upgrade: the row stays unsourced.
type quietLogger struct{}

func (quietLogger) Printf(string, ...any) {}
func (quietLogger) Warnf(string, ...any)  {}
func (quietLogger) Errorf(string, ...any) {}
func (quietLogger) Close() error          { return nil }

// probeServer builds a shell server whose agent streams from stub (the
// newTestServer fixture, with the provider under test).
func probeServer(stub agent.Provider) *Server {
	a := agent.NewAgent(stub, nil, 0)
	a.SetLoopProvider(testLoop{})
	return NewServer(a, nil, nil, nil, nil, nil)
}

func healthBody(t *testing.T, s *Server) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return body
}

func healthProviderFact(t *testing.T, s *Server) map[string]any {
	t.Helper()
	body := healthBody(t, s)
	provider, ok := body["provider"].(map[string]any)
	if !ok {
		t.Fatalf("sourced body carries no provider fact: %v", body)
	}
	return provider
}

// TestHealthUnsourcedShapeUnchanged: nothing sourced - no probe has run,
// no store is mounted, no logger upgrade - so the body is exactly the
// pre-fact shape. This is the additive-wire proof and the older-client
// degrade the popover's "not reported" rows rest on.
func TestHealthUnsourcedShapeUnchanged(t *testing.T) {
	s := newTestServer()
	body := healthBody(t, s)

	for _, key := range []string{"provider", "database", "last_error"} {
		if _, ok := body[key]; ok {
			t.Fatalf("unsourced body carries %q: %v", key, body[key])
		}
	}
	want := map[string]bool{"status": true, "uptime_seconds": true}
	for key := range body {
		if !want[key] {
			t.Fatalf("unexpected body key %q: %v", key, body)
		}
	}
}

// TestHealthProviderFact: a failed model-list probe is an unreachable
// provider with the probe's error as its detail; a passing one is
// reachable with no detail.
func TestHealthProviderFact(t *testing.T) {
	stub := &probeProvider{err: errString("dial tcp: connection refused")}
	s := probeServer(stub)
	s.probeProvider(context.Background())

	provider := healthProviderFact(t, s)
	if provider["reachable"] != false {
		t.Fatalf("reachable = %v, want false", provider["reachable"])
	}
	if detail, _ := provider["detail"].(string); !strings.Contains(detail, "connection refused") {
		t.Fatalf("detail = %q, want the probe's error", detail)
	}
	if at, _ := provider["checked_at"].(float64); at <= 0 {
		t.Fatalf("checked_at = %v, want a unix second", provider["checked_at"])
	}

	up := probeServer(&probeProvider{})
	up.probeProvider(context.Background())
	provider = healthProviderFact(t, up)
	if provider["reachable"] != true {
		t.Fatalf("reachable = %v, want true", provider["reachable"])
	}
	if _, ok := provider["detail"]; ok {
		t.Fatalf("a passing probe carries a detail: %v", provider)
	}
}

// TestHealthReadsNeverProbe: the probe is cached. Five health reads
// after one probe leave the provider's call count at one - the reason
// the probe is not on the 5s heartbeat.
func TestHealthReadsNeverProbe(t *testing.T) {
	stub := &probeProvider{}
	s := probeServer(stub)
	s.probeProvider(context.Background())
	if got := stub.count(); got != 1 {
		t.Fatalf("probe calls = %d, want 1", got)
	}
	for i := 0; i < 5; i++ {
		healthProviderFact(t, s)
	}
	if got := stub.count(); got != 1 {
		t.Fatalf("health reads probed the provider: calls = %d, want 1", got)
	}
}

// TestHealthDatabaseFact: the store seam's ping rides the body inline.
// A store without the upgrade, and no store at all, leave the fact out.
func TestHealthDatabaseFact(t *testing.T) {
	healthy := NewServer(newTestServer().Agent(), nil, pingingStore{}, nil, nil, nil)
	fact, ok := healthBody(t, healthy)["database"].(map[string]any)
	if !ok || fact["healthy"] != true {
		t.Fatalf("database fact = %v, want healthy", healthBody(t, healthy)["database"])
	}

	broken := NewServer(newTestServer().Agent(), nil, pingingStore{err: errString("database is locked")}, nil, nil, nil)
	fact, ok = healthBody(t, broken)["database"].(map[string]any)
	if !ok || fact["healthy"] != false {
		t.Fatalf("database fact = %v, want unhealthy", healthBody(t, broken)["database"])
	}
	if detail, _ := fact["detail"].(string); !strings.Contains(detail, "locked") {
		t.Fatalf("detail = %q, want the ping's error", detail)
	}

	upgradeless := NewServer(newTestServer().Agent(), nil, plainStore{}, nil, nil, nil)
	if _, ok := healthBody(t, upgradeless)["database"]; ok {
		t.Fatal("a store without Ping sourced a database fact")
	}
}

// TestHealthLastErrorFact: the logger upgrade sources the newest error
// with its time; a sourced "no errors yet" stays distinguishable from
// unsourced (an empty message vs an absent object).
func TestHealthLastErrorFact(t *testing.T) {
	s := newTestServer()
	at := time.Unix(1794300000, 0)
	wireInspector(t, s, nil, &lastErrLogger{msg: "stream died: eof", at: at, had: true})

	fact, ok := healthBody(t, s)["last_error"].(map[string]any)
	if !ok {
		t.Fatalf("sourced body carries no last_error fact: %v", healthBody(t, s))
	}
	if fact["message"] != "stream died: eof" {
		t.Fatalf("message = %v", fact["message"])
	}
	if got, _ := fact["at"].(float64); int64(got) != at.Unix() {
		t.Fatalf("at = %v, want %d", fact["at"], at.Unix())
	}

	clean := newTestServer()
	wireInspector(t, clean, nil, &lastErrLogger{})
	fact, ok = healthBody(t, clean)["last_error"].(map[string]any)
	if !ok {
		t.Fatal("a logger with no errors must still source the fact (sourced-none)")
	}
	if fact["message"] != "" {
		t.Fatalf("message = %v, want empty", fact["message"])
	}

	plain := newTestServer()
	wireInspector(t, plain, nil, quietLogger{})
	if _, ok := healthBody(t, plain)["last_error"]; ok {
		t.Fatal("a logger without the upgrade sourced a last_error fact")
	}
}

// TestHealthClampOneLine: an error message is flattened and capped for
// the one-line popover row (the full record stays in the log file).
func TestHealthClampOneLine(t *testing.T) {
	got := clampOneLine("boom:\n  wrapped\tcause", 40)
	if got != "boom: wrapped cause" {
		t.Fatalf("clampOneLine = %q", got)
	}
	long := clampOneLine(strings.Repeat("x", 100), 10)
	if len([]rune(long)) != 11 || !strings.HasSuffix(long, "…") {
		t.Fatalf("cap = %q, want 10 runes + ellipsis", long)
	}
}

// errString is a tiny error carrier (no fmt import churn in the fixtures).
type errString string

func (e errString) Error() string { return string(e) }
