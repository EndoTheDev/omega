package http_channel

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
)

// The mutating-request Host/Origin guard (v0.14.1 audit p10). The local
// surface is unauthenticated by design (loopback, one operator), so the
// browser same-site rules are the only firewall it has - and a cross-site
// POST of Content-Type text/plain triggers no CORS preflight, which made
// POST /files/write, POST /files/mkdir, the module writes (PATCH
// /tasks/{id}, DELETE /boards/{name}, ...) reachable from any web page
// the operator visits while the server runs. DNS rebinding moves the
// same attack onto the Host header. The guard closes both on the wire:
// a mutating request (anything but GET/HEAD/OPTIONS) must name the bind
// address in Host and, when it carries an Origin, that Origin must be
// the same surface. Reads stay open - no secret leaves through them, and
// the SPA's own POSTs carry the serving host:port as both Host and
// Origin, so every legit caller passes untouched.

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
// the first Serve means fail-open, which is only reachable by direct
// Handler() consumers in tests - the serving path always sets it before
// ListenAndServe answers its first request.
type hostGuard struct {
	addr atomic.Value // string
	next http.Handler
}

func (g *hostGuard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if mutatingRequest(r.Method) {
		bind, _ := g.addr.Load().(string)
		allowed := allowedHosts(bind)
		if !hostAllowed(r.Host, allowed) || !originAllowed(r, allowed) {
			http.Error(w, "forbidden: mutating request from a foreign origin (Host/Origin guard)",
				http.StatusForbidden)
			return
		}
	}
	g.next.ServeHTTP(w, r)
}

// allowedHosts derives the permitted Host values from the bind address.
// A loopback bind allows every loopback name on the same port (the
// operator types localhost, the SPA sees 127.0.0.1, both must pass). An
// exact non-loopback IP bind allows only that name. A wildcard bind
// (::, 0.0.0.0, a bare :port) cannot be enumerated, so the set is empty
// and enforcement falls through to the Origin rule - which still holds
// for the whole rebinding class, because a browser always sends the
// true Origin on a cross-site write no matter what name resolved.
func allowedHosts(addr string) map[string]bool {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = addr, ""
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
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

// hostAllowed checks the request's Host header against the derived set.
// An empty set passes (wildcard bind - the Origin rule answers); a
// request with no Host at all never does (HTTP/1.1 requires one, and a
// missing Host is exactly what a raw-crafted rebinding probe looks like).
func hostAllowed(host string, allowed map[string]bool) bool {
	if allowed == nil {
		return true
	}
	if host == "" {
		return false
	}
	return allowed[host]
}

// originAllowed checks the Origin header, when the client sent one:
// browsers attach it to every cross-site write, so a foreign Origin is
// the fingerprint of the drive-by attack. Same-origin callers match the
// request Host (the SPA served from this very bind). Non-browser
// clients (curl, the agent loop) send no Origin and pass. The header
// must parse as an absolute origin URL - a garbage Origin is refused,
// not shrugged at.
func originAllowed(r *http.Request, allowed map[string]bool) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	if err != nil || u.Host == "" {
		return false
	}
	// same-origin callers match the request Host (the SPA served from
	// this very bind always does, whatever the bind shape)
	if r.Host != "" && u.Host == r.Host {
		return true
	}
	// a known bind: the derived set is the second chance (localhost
	// origin hitting the 127.0.0.1 spelling and vice versa). A wildcard
	// bind: nothing else passes - this is the branch that keeps the DNS
	// rebinding class dead on unenumerable binds.
	return allowed != nil && allowed[u.Host]
}