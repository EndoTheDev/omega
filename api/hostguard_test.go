package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// The Host/Origin guard tests: cross-origin mutating requests refuse, loopback mutating
// requests pass, every request - reads included - must name a trusted identity in Host,
// a wildcard bind carries no identity without server.public_hosts, and an empty Origin
// is exempt on loopback only. Driven against a real httptest server whose handler chain
// is the guard only (the bind is faked through addr.Store) so the Host header is
// settable per request - httptest auto-fills Host from the listener, and the guard must
// judge the header the CLIENT sent, not the one the test process happens to listen.

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

// guardServerPublic is guardServer with an operator declaration (server.public_hosts).
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

// declared public hosts ------------------------------------

// The operator declares a tunneled domain (server.public_hosts).
func TestGuardPublicHostPassesWhenDeclared(t *testing.T) {
	srv := guardServer(t, "127.0.0.1:8099")
	g := srv.Config.Handler.(*hostGuard)
	g.public.Store(map[string]bool{"omega.endothe.dev": true})

	if got := doReq(t, srv, http.MethodGet, "omega.endothe.dev", ""); got != http.StatusOK {
		t.Errorf("GET via declared public host: got %d, want 200", got)
	}
	// the declared domain: same-origin writes pass.
	if got := doReq(t, srv, http.MethodPost, "omega.endothe.dev", "https://omega.endothe.dev"); got != http.StatusOK {
		t.Errorf("POST via declared public host, same-origin: got %d, want 200", got)
	}
	// any OTHER domain still dies.
	for _, host := range []string{"evil.example", "attacker.com", "omega.endothe.dev.evil.example"} {
		if got := doReq(t, srv, http.MethodGet, host, ""); got != http.StatusForbidden {
			t.Errorf("GET via undeclared host %s: got %d, want 403", host, got)
		}
	}
}

// loopback mutate passes ---------------------------------------------

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

// cross-origin mutate refuses ----------------------------------------

func TestGuardCrossOriginMutateRefused(t *testing.T) {
	srv := guardServer(t, "127.0.0.1:8098")
	// the text/plain cross-site POST: foreign Host, no preflight
	if got := doReq(t, srv, http.MethodPost, "evil.example:8098", ""); got != http.StatusForbidden {
		t.Errorf("POST with rebinding Host: got %d, want 403", got)
	}
	if got := doReq(t, srv, http.MethodPost, "127.0.0.1:8098", "http://evil.example:8098"); got != http.StatusForbidden {
		t.Errorf("POST with foreign Origin: got %d, want 403", got)
	}
	if got := doReq(t, srv, http.MethodPost, "127.0.0.1:8098", "not a url"); got != http.StatusForbidden {
		t.Errorf("POST with garbage Origin: got %d, want 403", got)
	}
	if got := doReq(t, srv, http.MethodPut, "127.0.0.1:8098", "http://127.0.0.1:9999"); got != http.StatusForbidden {
		t.Errorf("PUT with wrong-port Origin: got %d, want 403", got)
	}
	if got := doReq(t, srv, http.MethodGet, "evil.example:8098", ""); got != http.StatusForbidden {
		t.Errorf("GET with rebinding Host: got %d, want 403", got)
	}
	for _, host := range []string{"127.0.0.1:8098", "localhost:8098", "[::1]:8098"} {
		if got := doReq(t, srv, http.MethodGet, host, ""); got != http.StatusOK {
			t.Errorf("GET with Host %s: got %d, want 200", host, got)
		}
	}
}

// wildcard binds: the declaration IS the identity set ---

func TestGuardWildcardBindWithoutPublicHostsFailsClosed(t *testing.T) {
	srv := guardServer(t, ":8098")
	if got := doReq(t, srv, http.MethodGet, "192.168.1.10:8098", ""); got != http.StatusForbidden {
		t.Errorf("GET on an undeclared wildcard bind: got %d, want 403", got)
	}
	if got := doReq(t, srv, http.MethodPost, "192.168.1.10:8098", "http://192.168.1.10:8098"); got != http.StatusForbidden {
		t.Errorf("POST on an undeclared wildcard bind: got %d, want 403", got)
	}
}

func TestGuardWildcardBindRequiresDeclaredIdentity(t *testing.T) {
	srv := guardServerPublic(t, ":8098", "omega.endothe.dev")
	if got := doReq(t, srv, http.MethodGet, "omega.endothe.dev", ""); got != http.StatusOK {
		t.Errorf("GET via declared host on wildcard bind: got %d, want 200", got)
	}
	if got := doReq(t, srv, http.MethodPost, "omega.endothe.dev", "https://omega.endothe.dev"); got != http.StatusOK {
		t.Errorf("POST via declared host, same-origin: got %d, want 200", got)
	}
	// a wildcard bind has no enumerable address, so the bare LAN name is NOT an identity.
	if got := doReq(t, srv, http.MethodGet, "192.168.1.10:8098", ""); got != http.StatusForbidden {
		t.Errorf("GET via the raw LAN address on a wildcard bind: got %d, want 403", got)
	}
	if got := doReq(t, srv, http.MethodPost, "omega.endothe.dev", ""); got != http.StatusForbidden {
		t.Errorf("POST with no Origin on a non-loopback bind: got %d, want 403", got)
	}
	if got := doReq(t, srv, http.MethodPost, "omega.endothe.dev", "http://evil.example"); got != http.StatusForbidden {
		t.Errorf("POST with foreign Origin on wildcard bind: got %d, want 403", got)
	}
	srv2 := guardServer(t, "127.0.0.1:8098")
	if got := doReq(t, srv2, http.MethodPost, "localhost.evil.example:8098", ""); got != http.StatusForbidden {
		t.Errorf("POST rebinding-lookalike Host: got %d, want 403", got)
	}
}

// The DNS-rebinding write: the attacker's page resolves its own domain.
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

// exact binds: own identity only, Origin required -------------------

func TestGuardExactIPBindAllowsOnlyThatHost(t *testing.T) {
	srv := guardServer(t, "192.168.1.10:8098")
	// a browser on the LAN address: Host and Origin name the bind
	if got := doReq(t, srv, http.MethodPost, "192.168.1.10:8098", "http://192.168.1.10:8098"); got != http.StatusOK {
		t.Errorf("POST the bound host, same-origin: got %d, want 200", got)
	}
	if got := doReq(t, srv, http.MethodPost, "192.168.1.10:8098", ""); got != http.StatusForbidden {
		t.Errorf("POST no-Origin against an exact LAN bind: got %d, want 403", got)
	}
	if got := doReq(t, srv, http.MethodPost, "127.0.0.1:8098", "http://127.0.0.1:8098"); got != http.StatusForbidden {
		t.Errorf("POST loopback against an exact LAN bind: got %d, want 403", got)
	}
	if got := doReq(t, srv, http.MethodGet, "192.168.1.10:8098", ""); got != http.StatusOK {
		t.Errorf("GET the bound host: got %d, want 200", got)
	}
}

// fail-closed startup: a wildcard bind needs a declaration ---------

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
		// the refusal still plants the bind, so a caller that ignored the error answers 403.
		if addr, _ := g.addr.Load().(string); addr != bind {
			t.Errorf("bind(%q) did not plant the address (got %q)", bind, addr)
		}
	}

	g := newGuard()
	g.public.Store(map[string]bool{"omega.endothe.dev": true})
	for _, bind := range []string{"0.0.0.0:8099", ":8099"} {
		if err := g.bind(bind); err != nil {
			t.Errorf("bind(%q) with a declared public host: %v", bind, err)
		}
	}

	for _, bind := range []string{"127.0.0.1:8099", "localhost:8099", "[::1]:8099", "192.168.1.10:8099"} {
		if err := newGuard().bind(bind); err != nil {
			t.Errorf("bind(%q) needs no declaration: %v", bind, err)
		}
	}
}

// The refusal rides the real Serve wiring.
func TestServerServeRefusesWildcardBindWithoutPublicHosts(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil, nil)
	err := s.Serve(context.Background(), "0.0.0.0:0")
	if err == nil {
		t.Fatal("Serve accepted a wildcard bind with no server.public_hosts")
	}
	if !strings.Contains(err.Error(), "public_hosts") {
		t.Fatalf("Serve error = %q, want it to name server.public_hosts", err)
	}

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

// the machine's own name: every bind answers to it ------------------

// ownName is the OS's own name at TEST time - never a literal, because CI hostnames differ.
func ownName(t *testing.T) string {
	t.Helper()
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		t.Skip("this host has no name to answer to")
	}
	return strings.ToLower(strings.TrimSpace(name))
}

// A tailnet short name IS the machine's name: every bind shape accepts it, bare and with
// its port, in any case - and a foreign name still dies on the same request.
func TestGuardOwnNamePassesOnEveryBind(t *testing.T) {
	name := ownName(t)
	binds := []string{"127.0.0.1:8098", "192.168.1.10:8098", "localhost:8098", "[::1]:8098"}
	for _, bind := range binds {
		srv := guardServer(t, bind)
		port := bind[strings.LastIndex(bind, ":")+1:]
		for _, host := range []string{name, name + ":" + port, strings.ToUpper(name) + ":" + port} {
			if got := doReq(t, srv, http.MethodGet, host, ""); got != http.StatusOK {
				t.Errorf("bind %q: GET with the machine's own name %q: got %d, want 200", bind, host, got)
			}
		}
		if got := doReq(t, srv, http.MethodGet, name+".evil.example:"+port, ""); got != http.StatusForbidden {
			t.Errorf("bind %q: GET with a lookalike of the own name: got %d, want 403", bind, got)
		}
	}
}

// A wildcard bind cannot be enumerated, so server.public_hosts stays the declaration it
// needs to start - and the own name rides IN ADDITION to it.
func TestGuardOwnNameOnAWildcardBind(t *testing.T) {
	name := ownName(t)
	srv := guardServerPublic(t, ":8098", "omega.endothe.dev")
	if got := doReq(t, srv, http.MethodGet, name+":8098", ""); got != http.StatusOK {
		t.Errorf("GET the own name on a wildcard bind: got %d, want 200", got)
	}
	if got := doReq(t, srv, http.MethodGet, "omega.endothe.dev", ""); got != http.StatusOK {
		t.Errorf("GET the declared public host: got %d, want 200", got)
	}
	// a wildcard bind still refuses to START without the declaration, own name or not.
	if err := newGuard().bind("0.0.0.0:8098"); err == nil {
		t.Fatal("a wildcard bind started with no server.public_hosts")
	}
}

// The mutation law is unchanged: a write by name still needs an Origin that name owns, and
// an empty Origin off loopback is still refused.
func TestGuardOwnNameMutateStillNeedsTheOrigin(t *testing.T) {
	name := ownName(t)
	srv := guardServer(t, "192.168.1.10:8098")
	byName := name + ":8098"
	if got := doReq(t, srv, http.MethodPost, byName, ""); got != http.StatusForbidden {
		t.Errorf("POST by the own name with no Origin: got %d, want 403", got)
	}
	if got := doReq(t, srv, http.MethodPost, byName, "http://"+byName); got != http.StatusOK {
		t.Errorf("POST by the own name, same-origin: got %d, want 200", got)
	}
	if got := doReq(t, srv, http.MethodPost, byName, "http://evil.example:8098"); got != http.StatusForbidden {
		t.Errorf("POST by the own name with a foreign Origin: got %d, want 403", got)
	}
}

// un-set guard (pre-Serve) fails open, so plain Handler consumers ---- --.
func TestGuardUnsetAddrFailsOpen(t *testing.T) {
	srv := guardServer(t, "")
	if got := doReq(t, srv, http.MethodPost, "evil.example:8098", ""); got != http.StatusOK {
		t.Errorf("POST before Serve: got %d, want 200 (no bind known yet)", got)
	}
}

// the guard rides the real Server wiring -----------------------------

func TestServerWrapsMuxInHostGuard(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil, nil)
	g, ok := s.Handler().(*hostGuard)
	if !ok {
		t.Fatalf("Handler() is %T, want *hostGuard", s.Handler())
	}
	if g.next == nil {
		t.Fatal("guard wraps nothing")
	}
	// Serve plants the bind: the guard field must accept a string store without racing.
	g.addr.Store("127.0.0.1:0")
	if got := allowedHosts("127.0.0.1:8098"); !got["localhost:8098"] {
		t.Fatalf("loopback bind must allow localhost spelling, got %v", got)
	}
}
