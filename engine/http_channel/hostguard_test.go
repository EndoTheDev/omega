package http_channel

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The Host/Origin guard tests (v0.14.1 audit p10 acceptance): cross-origin
// mutating requests refuse, loopback mutating requests pass, reads stay
// open. Driven against a real httptest server whose handler chain is the
// guard only (the bind is faked through addr.Store) so the Host header is
// settable per request - httptest auto-fills Host from the listener, and
// the guard must judge the header the CLIENT sent, not the one the test
// process happens to listen on.

func guardServer(t *testing.T, bind string) *httptest.Server {
	t.Helper()
	g := &hostGuard{next: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok") //nolint:errcheck // test sink
	})}
	if bind != "" {
		g.addr.Store(bind)
	}
	srv := httptest.NewServer(g)
	t.Cleanup(srv.Close)
	return srv
}

func doReq(t *testing.T, srv *httptest.Server, method, host, origin string) int {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if host != "" {
		req.Host = host
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, host, err)
	}
	defer res.Body.Close()
	return res.StatusCode
}

// -- loopback mutate passes ------------------------------------------------

func TestGuardLoopbackMutatePasses(t *testing.T) {
	srv := guardServer(t, "127.0.0.1:8098")
	for _, host := range []string{"127.0.0.1:8098", "localhost:8098", "[::1]:8098"} {
		if got := doReq(t, srv, http.MethodPost, host, ""); got != http.StatusOK {
			t.Errorf("POST with Host %s: got %d, want 200", host, got)
		}
	}
	// same-origin browser caller: the SPA sends the serving origin
	if got := doReq(t, srv, http.MethodPost, "127.0.0.1:8098", "http://127.0.0.1:8098"); got != http.StatusOK {
		t.Errorf("POST with same-origin Origin: got %d, want 200", got)
	}
	if got := doReq(t, srv, http.MethodDelete, "localhost:8098", ""); got != http.StatusOK {
		t.Errorf("DELETE with allowed Host: got %d, want 200", got)
	}
}

// -- cross-origin mutate refuses -------------------------------------------

func TestGuardCrossOriginMutateRefused(t *testing.T) {
	srv := guardServer(t, "127.0.0.1:8098")
	// the text/plain cross-site POST: foreign Host, no preflight
	if got := doReq(t, srv, http.MethodPost, "evil.example:8098", ""); got != http.StatusForbidden {
		t.Errorf("POST with rebinding Host: got %d, want 403", got)
	}
	// allowed Host but a foreign Origin (the classic cross-site form post)
	if got := doReq(t, srv, http.MethodPost, "127.0.0.1:8098", "http://evil.example:8098"); got != http.StatusForbidden {
		t.Errorf("POST with foreign Origin: got %d, want 403", got)
	}
	// allowed Host, garbage Origin
	if got := doReq(t, srv, http.MethodPost, "127.0.0.1:8098", "not a url"); got != http.StatusForbidden {
		t.Errorf("POST with garbage Origin: got %d, want 403", got)
	}
	// a foreign Origin that merely borrows the port
	if got := doReq(t, srv, http.MethodPut, "127.0.0.1:8098", "http://127.0.0.1:9999"); got != http.StatusForbidden {
		t.Errorf("PUT with wrong-port Origin: got %d, want 403", got)
	}
	// reads on the foreign host still pass - no secret rides a GET
	if got := doReq(t, srv, http.MethodGet, "evil.example:8098", ""); got != http.StatusOK {
		t.Errorf("GET with rebinding Host: got %d, want 200 (reads stay open)", got)
	}
}

// -- wildcard and exact binds ----------------------------------------------

func TestGuardWildcardBindStillChecksOrigin(t *testing.T) {
	srv := guardServer(t, ":8098")
	// no Host check is possible on a wildcard bind - the header check
	// passes; the no-Origin non-browser caller passes (curl is not the
	// attack class)
	if got := doReq(t, srv, http.MethodPost, "192.168.1.10:8098", ""); got != http.StatusOK {
		t.Errorf("POST no-Origin on wildcard bind: got %d, want 200", got)
	}
	// but the browser attack still dies: the Origin never matches the
	// request Host the attacker needs
	if got := doReq(t, srv, http.MethodPost, "192.168.1.10:8098", "http://evil.example"); got != http.StatusForbidden {
		t.Errorf("POST foreign Origin on wildcard bind: got %d, want 403", got)
	}
	// DNS rebinding via a same-port spelling dies on Host under a
	// loopback bind ...
	srv2 := guardServer(t, "127.0.0.1:8098")
	if got := doReq(t, srv2, http.MethodPost, "localhost.evil.example:8098", ""); got != http.StatusForbidden {
		t.Errorf("POST rebinding-lookalike Host: got %d, want 403", got)
	}
}

func TestGuardExactIPBindAllowsOnlyThatHost(t *testing.T) {
	srv := guardServer(t, "192.168.1.10:8098")
	if got := doReq(t, srv, http.MethodPost, "192.168.1.10:8098", ""); got != http.StatusOK {
		t.Errorf("POST the bound host: got %d, want 200", got)
	}
	if got := doReq(t, srv, http.MethodPost, "127.0.0.1:8098", ""); got != http.StatusForbidden {
		t.Errorf("POST loopback against an exact LAN bind: got %d, want 403", got)
	}
}

// -- un-set guard (pre-Serve) fails open, so plain Handler() consumers ----
// -- and every existing mux-level test keep working -------------------------

func TestGuardUnsetAddrFailsOpen(t *testing.T) {
	srv := guardServer(t, "")
	if got := doReq(t, srv, http.MethodPost, "evil.example:8098", ""); got != http.StatusOK {
		t.Errorf("POST before Serve: got %d, want 200 (no bind known yet)", got)
	}
}

// -- the guard rides the real Server wiring --------------------------------

func TestServerWrapsMuxInHostGuard(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil, nil)
	g, ok := s.Handler().(*hostGuard)
	if !ok {
		t.Fatalf("Handler() is %T, want *hostGuard", s.Handler())
	}
	if g.next == nil {
		t.Fatal("guard wraps nothing")
	}
	// Serve plants the bind: the guard field must accept a string store
	// without racing (Serve serializes against ListenAndServe before any
	// request is accepted)
	g.addr.Store("127.0.0.1:0")
	if got := allowedHosts("127.0.0.1:8098"); !got["localhost:8098"] {
		t.Fatalf("loopback bind must allow localhost spelling, got %v", got)
	}
}
