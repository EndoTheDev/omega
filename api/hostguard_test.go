package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The Host/Origin guard tests (v0.14.1 audit p10 acceptance; read side
// closed at v0.15.0; the non-loopback surface closed at t_9caf16c8):
// cross-origin mutating requests refuse, loopback mutating requests
// pass, every request - reads included - must name a trusted identity in
// Host, a wildcard bind carries no identity without server.public_hosts,
// and an empty Origin is exempt on loopback only. Driven against a real
// httptest server whose handler chain is the
// guard only (the bind is faked through addr.Store) so the Host header is
// settable per request - httptest auto-fills Host from the listener, and
// the guard must judge the header the CLIENT sent, not the one the test
// process happens to listen on.

func newGuard() *hostGuard {
	return &hostGuard{next: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok") // test sink
	})}
}

func guardServer(t *testing.T, bind string) *httptest.Server {
	t.Helper()
	g := newGuard()
	if bind != "" {
		g.addr.Store(bind)
	}
	srv := httptest.NewServer(g)
	t.Cleanup(srv.Close)
	return srv
}

// guardServerPublic is guardServer with an operator declaration
// (server.public_hosts) planted on the guard.
func guardServerPublic(t *testing.T, bind string, public ...string) *httptest.Server {
	t.Helper()
	g := newGuard()
	if bind != "" {
		g.addr.Store(bind)
	}
	declared := make(map[string]bool, len(public))
	for _, h := range public {
		declared[h] = true
	}
	g.public.Store(declared)
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

// -- declared public hosts (v0.15.1) ---------------------------------------

// The operator declares a tunneled domain (server.public_hosts); the
// guard answers it and keeps refusing every name not on the list.
func TestGuardPublicHostPassesWhenDeclared(t *testing.T) {
	srv := guardServer(t, "127.0.0.1:8099")
	g := srv.Config.Handler.(*hostGuard)
	g.public.Store(map[string]bool{"omega.endothe.dev": true})

	// the declared domain: reads pass
	if got := doReq(t, srv, http.MethodGet, "omega.endothe.dev", ""); got != http.StatusOK {
		t.Errorf("GET via declared public host: got %d, want 200", got)
	}
	// the declared domain: same-origin writes pass (the SPA served
	// through the tunnel posts with Origin = the domain)
	if got := doReq(t, srv, http.MethodPost, "omega.endothe.dev", "https://omega.endothe.dev"); got != http.StatusOK {
		t.Errorf("POST via declared public host, same-origin: got %d, want 200", got)
	}
	// any OTHER domain still dies - the declaration is exact, the
	// rebinding class stays dead
	for _, host := range []string{"evil.example", "attacker.com", "omega.endothe.dev.evil.example"} {
		if got := doReq(t, srv, http.MethodGet, host, ""); got != http.StatusForbidden {
			t.Errorf("GET via undeclared host %s: got %d, want 403", host, got)
		}
	}
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
	// reads on a foreign host die too - a rebinding GET carries no
	// Origin to lean on, and /files/read answers bytes (v0.15.0)
	if got := doReq(t, srv, http.MethodGet, "evil.example:8098", ""); got != http.StatusForbidden {
		t.Errorf("GET with rebinding Host: got %d, want 403", got)
	}
	// legit reads keep passing: the operator's browser and curl both
	// name the true serving host
	for _, host := range []string{"127.0.0.1:8098", "localhost:8098", "[::1]:8098"} {
		if got := doReq(t, srv, http.MethodGet, host, ""); got != http.StatusOK {
			t.Errorf("GET with Host %s: got %d, want 200", host, got)
		}
	}
}

// -- wildcard binds: the declaration IS the identity set (t_9caf16c8) ------

func TestGuardWildcardBindWithoutPublicHostsFailsClosed(t *testing.T) {
	srv := guardServer(t, ":8098")
	// nothing declared: the guard has no identity to trust, so every
	// request dies - the request-time backstop behind the startup refusal
	if got := doReq(t, srv, http.MethodGet, "192.168.1.10:8098", ""); got != http.StatusForbidden {
		t.Errorf("GET on an undeclared wildcard bind: got %d, want 403", got)
	}
	if got := doReq(t, srv, http.MethodPost, "192.168.1.10:8098", "http://192.168.1.10:8098"); got != http.StatusForbidden {
		t.Errorf("POST on an undeclared wildcard bind: got %d, want 403", got)
	}
}

func TestGuardWildcardBindRequiresDeclaredIdentity(t *testing.T) {
	srv := guardServerPublic(t, ":8098", "omega.endothe.dev")
	// the declared domain: reads and same-origin writes pass
	if got := doReq(t, srv, http.MethodGet, "omega.endothe.dev", ""); got != http.StatusOK {
		t.Errorf("GET via declared host on wildcard bind: got %d, want 200", got)
	}
	if got := doReq(t, srv, http.MethodPost, "omega.endothe.dev", "https://omega.endothe.dev"); got != http.StatusOK {
		t.Errorf("POST via declared host, same-origin: got %d, want 200", got)
	}
	// a wildcard bind has no enumerable address, so the bare LAN name is
	// NOT an identity - only the declaration is
	if got := doReq(t, srv, http.MethodGet, "192.168.1.10:8098", ""); got != http.StatusForbidden {
		t.Errorf("GET via the raw LAN address on a wildcard bind: got %d, want 403", got)
	}
	// empty Origin is not exempt off loopback: a no-Origin LAN client
	// would otherwise reach shell.run
	if got := doReq(t, srv, http.MethodPost, "omega.endothe.dev", ""); got != http.StatusForbidden {
		t.Errorf("POST with no Origin on a non-loopback bind: got %d, want 403", got)
	}
	// foreign Origin still dies
	if got := doReq(t, srv, http.MethodPost, "omega.endothe.dev", "http://evil.example"); got != http.StatusForbidden {
		t.Errorf("POST with foreign Origin on wildcard bind: got %d, want 403", got)
	}
	// DNS rebinding via a same-port spelling dies on Host under a
	// loopback bind ...
	srv2 := guardServer(t, "127.0.0.1:8098")
	if got := doReq(t, srv2, http.MethodPost, "localhost.evil.example:8098", ""); got != http.StatusForbidden {
		t.Errorf("POST rebinding-lookalike Host: got %d, want 403", got)
	}
}

// The DNS-rebinding write: the attacker's page resolves its own domain
// to the host's IP, so Host and Origin AGREE - equality is not identity.
// Only a declared (or bind-derived) identity passes.
func TestGuardRebindingShapedHostOriginRefused(t *testing.T) {
	wildcard := guardServerPublic(t, ":8098", "omega.endothe.dev")
	if got := doReq(t, wildcard, http.MethodPost, "attacker.example:8098", "http://attacker.example:8098"); got != http.StatusForbidden {
		t.Errorf("rebinding write on wildcard bind: got %d, want 403", got)
	}

	exact := guardServer(t, "192.168.1.10:8098")
	if got := doReq(t, exact, http.MethodPost, "attacker.example:8098", "http://attacker.example:8098"); got != http.StatusForbidden {
		t.Errorf("rebinding write on exact bind: got %d, want 403", got)
	}
	// the SAME equality on a loopback bind is a real local caller
	loopback := guardServer(t, "127.0.0.1:8098")
	if got := doReq(t, loopback, http.MethodPost, "127.0.0.1:8098", "http://127.0.0.1:8098"); got != http.StatusOK {
		t.Errorf("same-origin loopback write: got %d, want 200", got)
	}
}

// -- exact binds: own identity only, Origin required ----------------------

func TestGuardExactIPBindAllowsOnlyThatHost(t *testing.T) {
	srv := guardServer(t, "192.168.1.10:8098")
	// a browser on the LAN address: Host and Origin name the bind
	if got := doReq(t, srv, http.MethodPost, "192.168.1.10:8098", "http://192.168.1.10:8098"); got != http.StatusOK {
		t.Errorf("POST the bound host, same-origin: got %d, want 200", got)
	}
	// the empty-Origin exemption is loopback-only: a no-Origin client on
	// a LAN bind is unattributable
	if got := doReq(t, srv, http.MethodPost, "192.168.1.10:8098", ""); got != http.StatusForbidden {
		t.Errorf("POST no-Origin against an exact LAN bind: got %d, want 403", got)
	}
	if got := doReq(t, srv, http.MethodPost, "127.0.0.1:8098", "http://127.0.0.1:8098"); got != http.StatusForbidden {
		t.Errorf("POST loopback against an exact LAN bind: got %d, want 403", got)
	}
	// reads on the bind name stay open on a non-loopback bind
	if got := doReq(t, srv, http.MethodGet, "192.168.1.10:8098", ""); got != http.StatusOK {
		t.Errorf("GET the bound host: got %d, want 200", got)
	}
}

// -- fail-closed startup: a wildcard bind needs a declaration ------------

func TestGuardWildcardBindWithoutPublicHostsRefusesStartup(t *testing.T) {
	for _, bind := range []string{"0.0.0.0:8099", ":8099", ":::8099", "[::]:8099"} {
		g := newGuard()
		err := g.bind(bind)
		if err == nil {
			t.Fatalf("bind(%q) accepted a wildcard bind with no server.public_hosts", bind)
		}
		for _, want := range []string{"public_hosts", bind} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("bind(%q) error = %q, want it to name %q", bind, err, want)
			}
		}
		// the refusal still plants the bind, so a caller that ignored
		// the error answers 403 instead of failing open
		if addr, _ := g.addr.Load().(string); addr != bind {
			t.Errorf("bind(%q) did not plant the address (got %q)", bind, addr)
		}
	}

	// with a declaration the same binds start
	g := newGuard()
	g.public.Store(map[string]bool{"omega.endothe.dev": true})
	for _, bind := range []string{"0.0.0.0:8099", ":8099"} {
		if err := g.bind(bind); err != nil {
			t.Errorf("bind(%q) with a declared public host: %v", bind, err)
		}
	}

	// loopback and exact binds are their own identity - untouched
	for _, bind := range []string{"127.0.0.1:8099", "localhost:8099", "[::1]:8099", "192.168.1.10:8099"} {
		if err := newGuard().bind(bind); err != nil {
			t.Errorf("bind(%q) needs no declaration: %v", bind, err)
		}
	}
}

// The refusal rides the real Serve wiring: a wildcard bind with no
// declaration returns from Serve before the listener answers anything.
func TestServerServeRefusesWildcardBindWithoutPublicHosts(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil, nil)
	err := s.Serve(context.Background(), "0.0.0.0:0")
	if err == nil {
		t.Fatal("Serve accepted a wildcard bind with no server.public_hosts")
	}
	if !strings.Contains(err.Error(), "public_hosts") {
		t.Fatalf("Serve error = %q, want it to name server.public_hosts", err)
	}

	// the declared surface passes the same check (bind, not Serve: Serve
	// would then listen)
	declared := NewServer(nil, nil, nil, nil, nil, nil)
	declared.SetPublicHosts([]string{"omega.endothe.dev"})
	g, ok := declared.Handler().(*hostGuard)
	if !ok {
		t.Fatalf("Handler() is %T, want *hostGuard", declared.Handler())
	}
	if err := g.bind("0.0.0.0:0"); err != nil {
		t.Fatalf("bind with a declared public host: %v", err)
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
