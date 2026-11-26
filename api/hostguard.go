package api

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"

	"github.com/EndoTheDev/omega/core"
)

// The Host/Origin guard. The local surface is unauthenticated by design (loopback, one
// operator), so the browser's same-site rules are its only firewall: a cross-site POST
// of Content-Type text/plain triggers no CORS preflight, and DNS rebinding moves the
// same attack onto the Host header. Every request must name a trusted identity in Host,
// and a mutating request (anything but GET/HEAD/OPTIONS) must also carry an Origin that
// identity owns. The SPA passes untouched: it sends the serving host:port as both.
//
// The bind shape decides the identity set:
//   - every bind also answers to the MACHINE'S OWN NAME, bare and with this port: a
//     tailnet short name (winbtw, pibtw) is exactly that, so a connection saved as
//     `pibtw:8099` reaches the box it names. The tailnet FQDN is not derived - that one
//     stays a server.public_hosts declaration.
//   - loopback bind: the loopback names on the same port plus any declared public host.
//     An empty Origin is exempt (local curl, the agent loop) - nothing off-machine
//     reaches a loopback socket.
//   - exact non-loopback bind (a LAN or tailnet address): that address and the own name,
//     plus any declared public host. An empty Origin is never exempt here, so a no-Origin
//     LAN client cannot write.
//   - wildcard bind (0.0.0.0, a bare :port): the addresses answered are not enumerable,
//     so server.public_hosts IS the identity set (plus the own name), and an empty
//     declaration refuses to start rather than fail open.
//
// The guard is a rebinding heuristic, never authentication: a page cannot forge Host, and
// a rebinding attacker's Host is its own domain, not the machine's name.

// mutatingRequest reports whether the method changes server state.
func mutatingRequest(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// hostGuard wraps the mux with the guard. The bind address arrives at Serve (never at
// NewServer), so it rides an atomic value: zero before the first Serve is only reachable
// by direct Handler consumers in tests - the serving path plants the bind before
// ListenAndServe answers its first request.
type hostGuard struct {
	addr atomic.Value // string
	// public carries the operator-declared public identities (server.public_hosts): a
	// tunneled domain's Host never matches the bind. Empty = no public surface.
	public atomic.Value // map[string]bool
	next   http.Handler
}

// bind plants the bind address and refuses one the guard cannot defend: a wildcard bind
// has no enumerable identity set, so without a non-empty server.public_hosts the process
// refuses to start. The address is planted FIRST so a caller that ignored the error
// still answers 403 to everything.
func (g *hostGuard) bind(addr string) error {
	g.addr.Store(addr)
	return checkBindCoverage(addr, g.publicSet())
}

// publicSet reads the declared public identities (nil when undeclared).
func (g *hostGuard) publicSet() map[string]bool {
	pub, _ := g.public.Load().(map[string]bool)
	return pub
}

// checkBindCoverage refuses a bind whose identities the guard cannot enumerate.
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
		g.next.ServeHTTP(w, r)
		return
	}
	pub := g.publicSet()
	wildcard := isWildcardBind(bind)
	loopback := !wildcard && isLoopbackName(bindHost(bind))
	allowed := allowedHosts(bind)
	if wildcard {
		allowed = unionHosts(allowed, pub)
	}
	if !hostAllowed(r.Host, allowed, pub) {
		http.Error(w, core.GuardHostRefusal, http.StatusForbidden)
		return
	}
	if mutatingRequest(r.Method) && !originAllowed(r, allowed, pub, loopback) {
		http.Error(w, "forbidden: mutating request from a foreign origin (Host/Origin guard)",
			http.StatusForbidden)
		return
	}
	g.next.ServeHTTP(w, r)
}

// bindHost returns the host part of a bind address.
func bindHost(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[:i]
	}
	return addr
}

// isWildcardBind reports whether the bind answers on every interface, which leaves no
// identity to enumerate.
func isWildcardBind(addr string) bool {
	switch bindHost(addr) {
	case "", "0.0.0.0", "::", "[::]":
		return true
	}
	return false
}

// ownHostNames is the machine's own identity as a Host header may carry it: the OS's short
// name, bare and with this port. A tailnet short name IS that name (winbtw, pibtw), which
// is why a connection saved as typed reaches the box it names. A FQDN from the OS keeps its
// first label too; the tailnet DNS name is not derived - that stays a declaration.
func ownHostNames(port string) map[string]bool {
	set := map[string]bool{}
	name, err := os.Hostname()
	if err != nil {
		return set
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return set
	}
	names := []string{name}
	if i := strings.Index(name, "."); i > 0 {
		names = append(names, name[:i])
	}
	for _, n := range names {
		set[n] = true
		if port != "" {
			set[n+":"+port] = true
		}
	}
	return set
}

// unionHosts merges the bind-derived identities with the operator's declaration. Keys are
// lowercased because a Host header may arrive uppercased.
func unionHosts(derived, declared map[string]bool) map[string]bool {
	out := make(map[string]bool, len(derived)+len(declared))
	for h := range derived {
		out[strings.ToLower(h)] = true
	}
	for h := range declared {
		out[strings.ToLower(h)] = true
	}
	return out
}

// allowedHosts derives the permitted Host values from the bind. Every bind answers to the
// machine's own name; a loopback bind also allows every loopback name on the same port
// (the operator types localhost, the SPA sees 127.0.0.1, both must pass); a wildcard bind
// answers on 0.0.0.0 or a bare :port, cannot be enumerated, and returns only the own names
// - the caller unions in the declared public set.
func allowedHosts(addr string) map[string]bool {
	host := strings.ToLower(bindHost(addr))
	port := ""
	if _, p, err := net.SplitHostPort(addr); err == nil {
		port = p
	}
	set := ownHostNames(port)
	if isWildcardBind(addr) {
		return set
	}
	if isLoopbackName(host) {
		set["127.0.0.1:"+port] = true
		set["localhost:"+port] = true
		set["[::1]:"+port] = true
		if !strings.Contains(addr, ":") {
			set[host] = true
		}
		return set
	}
	// an exact bind (a LAN or tailnet address).
	set[strings.ToLower(addr)] = true
	set[host] = true
	return set
}

func isLoopbackName(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
}

// hostAllowed checks the request's Host header against the derived set plus the declared
// public one, compared lowercased (a Host header may arrive uppercased). An empty set
// passes nothing: a bind the guard cannot map to a trusted identity fails closed, and a
// request with no Host at all never passes (HTTP/1.1 requires one, and a missing Host is
// exactly what a raw rebinding probe looks like).
func hostAllowed(host string, allowed map[string]bool, public map[string]bool) bool {
	if host == "" {
		return false
	}
	host = strings.ToLower(host)
	return allowed[host] || public[host]
}

// originAllowed checks the Origin header on a mutating request. A non-empty header must
// parse as an absolute origin URL - a garbage Origin is refused, not shrugged at. On a
// non-loopback bind, Host == Origin alone is a DNS rebinding hole (both read
// attacker-domain), so the Host must also be a trusted identity; the hostAllowed pass
// already proves it, and this re-read keeps the rule correct standing alone.
func originAllowed(r *http.Request, allowed map[string]bool, public map[string]bool, loopback bool) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return loopback
	}
	u, err := url.Parse(o)
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(r.Host)
	origin := strings.ToLower(u.Host)
	if host != "" && origin == host {
		if loopback {
			return true
		}
		return allowed[host] || public[host]
	}
	return allowed[origin] || public[origin]
}
