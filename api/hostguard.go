package api

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
)

// The Host/Origin guard (v0.14.1 audit p10; read side closed at v0.15.0;
// the non-loopback surface closed at t_9caf16c8).
// The local surface is unauthenticated by design (loopback, one
// operator), so the browser same-site rules are the only firewall it
// has - and a cross-site POST of Content-Type text/plain triggers no
// CORS preflight, which made POST /files/write, POST /files/mkdir, the
// module writes (PATCH /tasks/{id}, DELETE /boards/{name}, ...) and
// the byte-returning reads (GET /files/read, GET /files/raw, the
// transcript GETs) reachable from any web page the operator visits
// while the server runs. DNS rebinding moves the same attack onto the
// Host header, and a top-level navigation GET carries no Origin, so
// reads cannot lean on the Origin rule. The guard closes the class on
// the wire: EVERY request must name a trusted identity in Host, and a
// mutating request (anything but GET/HEAD/OPTIONS) must also carry an
// Origin that identity owns. The SPA's own requests carry the serving
// host:port as both Host and Origin, so every legit caller passes
// untouched.
//
// Three surfaces, one law (the bind shape decides the identity set):
//   - loopback bind: the loopback names on the same port, plus any
//     declared public host. An empty Origin is exempt (local curl, the
//     agent loop) - nothing off-machine reaches a loopback socket.
//   - exact non-loopback bind (a LAN or tailnet address): that address
//     only, plus any declared public host. A mutating request must
//     carry an Origin - the empty-Origin exemption belongs to loopback
//     alone, so a no-Origin LAN client cannot write.
//   - wildcard bind (0.0.0.0, ::, a bare :port): the addresses answered
//     are not enumerable, so the operator's server.public_hosts
//     declaration IS the identity set - and an empty declaration
//     refuses to start (a wildcard bind would otherwise have no
//     identity to check and would fail open).

// mutatingRequest reports whether the method changes server state. The
// safe method set is the usual CORS-simple read set; everything else
// (POST/PUT/PATCH/DELETE and unknown verbs) is treated as a write.
func mutatingRequest(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// hostGuard wraps the mux with the guard. The bind address arrives at
// Serve (never at NewServer), so it rides an atomic value: zero before
// the first Serve means the guard has no bind to defend, which is only
// reachable by direct Handler() consumers in tests - the serving path
// always plants the bind before ListenAndServe answers its first
// request.
type hostGuard struct {
	addr atomic.Value // string
	// public (map[string]bool) carries the operator-declared public
	// identities (server.public_hosts) - a tunneled domain's Host
	// never matches the bind, so the guard needs the declaration.
	// Empty/nil = no public surface, and no wildcard bind either.
	public atomic.Value // map[string]bool
	next   http.Handler
}

// bind plants the bind address and refuses a bind the guard cannot
// defend. Server.Serve calls it before the listener accepts anything. A
// wildcard bind has no enumerable identity set, so without a non-empty
// server.public_hosts the process refuses to start rather than serve an
// open surface; the address is planted FIRST so a caller that ignored
// the error still answers 403 to everything.
func (g *hostGuard) bind(addr string) error {
	g.addr.Store(addr)
	return checkBindCoverage(addr, g.publicSet())
}

// publicSet reads the declared public identities (nil when undeclared).
func (g *hostGuard) publicSet() map[string]bool {
	pub, _ := g.public.Load().(map[string]bool)
	return pub
}

// checkBindCoverage refuses a bind whose identities the guard cannot
// enumerate while nothing was declared (t_9caf16c8). A loopback or exact
// address is its own identity and needs no declaration.
func checkBindCoverage(addr string, publicHosts map[string]bool) error {
	if !isWildcardBind(addr) || len(publicHosts) > 0 {
		return nil
	}
	return fmt.Errorf("refusing to serve on %q: a wildcard bind answers on every interface and cannot be enumerated by the Host guard, so server.public_hosts must name the identities this serve answers to (it is empty)",
		addr)
}

func (g *hostGuard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bind, _ := g.addr.Load().(string)
	if bind == "" {
		// no bind planted: direct Handler() consumers and mux-level
		// tests only. The serving path always plants one (bind, above).
		g.next.ServeHTTP(w, r)
		return
	}
	pub := g.publicSet()
	wildcard := isWildcardBind(bind)
	loopback := !wildcard && isLoopbackName(bindHost(bind))
	allowed := allowedHosts(bind)
	if wildcard {
		// nothing to derive from a wildcard bind: the declaration is
		// the whole identity set. An empty one passes nothing (the
		// startup refusal makes that unreachable on the serving path).
		allowed = pub
	}
	// Every request names a trusted identity in Host - reads included:
	// a rebinding GET carries no Origin to lean on, and /files/read
	// answers bytes (v0.15.0 audit p10).
	if !hostAllowed(r.Host, allowed, pub) {
		http.Error(w, "forbidden: request from a foreign host (Host guard)",
			http.StatusForbidden)
		return
	}
	if mutatingRequest(r.Method) && !originAllowed(r, allowed, pub, loopback) {
		http.Error(w, "forbidden: mutating request from a foreign origin (Host/Origin guard)",
			http.StatusForbidden)
		return
	}
	g.next.ServeHTTP(w, r)
}

// bindHost returns the host part of a bind address. An unbracketed IPv6
// wildcard spelling (":::port") makes SplitHostPort fail, so the host is
// everything before the last colon there.
func bindHost(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[:i]
	}
	return addr
}

// isWildcardBind reports whether the bind answers on every interface,
// which leaves the guard no enumerable identity to check.
func isWildcardBind(addr string) bool {
	switch bindHost(addr) {
	case "", "0.0.0.0", "::", "[::]":
		return true
	}
	return false
}

// allowedHosts derives the permitted Host values from the bind address.
// A loopback bind allows every loopback name on the same port (the
// operator types localhost, the SPA sees 127.0.0.1, both must pass). An
// exact non-loopback IP bind allows only that name. A wildcard bind
// (::, 0.0.0.0, a bare :port) cannot be enumerated and returns nil: the
// caller substitutes the declared public set there.
func allowedHosts(addr string) map[string]bool {
	host := bindHost(addr)
	port := ""
	if _, p, err := net.SplitHostPort(addr); err == nil {
		port = p
	}
	if isWildcardBind(addr) {
		return nil
	}
	if isLoopbackName(host) {
		set := map[string]bool{
			"127.0.0.1:" + port: true,
			"localhost:" + port: true,
			"[::1]:" + port:     true,
		}
		if !strings.Contains(addr, ":") {
			set[host] = true
		}
		return set
	}
	// an exact bind (a LAN address): the address is the identity, with
	// and without the port (a client may hit the default-port spelling)
	return map[string]bool{addr: true, host: true}
}

func isLoopbackName(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
}

// hostAllowed checks the request's Host header against the derived set
// plus the operator-declared public hosts. An empty set passes nothing:
// a bind the guard cannot map to a trusted identity fails closed, and a
// request with no Host at all never passes (HTTP/1.1 requires one, and a
// missing Host is exactly what a raw-crafted rebinding probe looks
// like).
func hostAllowed(host string, allowed map[string]bool, public map[string]bool) bool {
	if host == "" {
		return false
	}
	return allowed[host] || public[host]
}

// originAllowed checks the Origin header on a mutating request:
// browsers attach it to every cross-site write, so a foreign Origin is
// the fingerprint of the drive-by attack. The header must parse as an
// absolute origin URL - a garbage Origin is refused, not shrugged at.
//
// An absent Origin is exempt on a loopback bind only (non-browser
// clients - curl, the agent loop - pass there). On a non-loopback bind
// an empty Origin is unattributable, so the write is refused: nothing
// off-machine reaches a loopback socket, but a LAN client does.
//
// A same-origin caller matches the request Host (the SPA served from
// this very bind). On a non-loopback bind that equality alone is a DNS
// rebinding hole - Host == Origin == attacker-domain - so the Host must
// also be a trusted identity; the hostAllowed pass already proves it,
// and the re-read keeps this rule correct standing alone.
func originAllowed(r *http.Request, allowed map[string]bool, public map[string]bool, loopback bool) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return loopback
	}
	u, err := url.Parse(o)
	if err != nil || u.Host == "" {
		return false
	}
	if r.Host != "" && u.Host == r.Host {
		if loopback {
			return true
		}
		return allowed[r.Host] || public[r.Host]
	}
	// a known bind: the derived set is the second chance (localhost
	// origin hitting the 127.0.0.1 spelling and vice versa), and a
	// declared public host is the third.
	return allowed[u.Host] || public[u.Host]
}
