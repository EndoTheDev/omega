package http_channel

import (
	"net/http"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// handleModuleOrSPA is the catch-all the fixed engine handlers fall
// through to (v0.12 phase F): a module-rest request rides the capsule
// mux (404 when the pattern is not registered - the capsule mux has
// no fallback); anything else serves the SPA shell. The engine holds
// no module shape: the routes ride the live Context.
func (s *Server) handleModuleOrSPA(w http.ResponseWriter, r *http.Request) {
	s.seams.mu.RLock()
	modmux := s.seams.modules
	roots := s.seams.routeRoots
	s.seams.mu.RUnlock()
	if modmux != nil {
		seg := ""
		if rest, ok := strings.CutPrefix(r.URL.Path, "/"); ok && rest != "" {
			seg, _, _ = strings.Cut(rest, "/")
			seg = "/" + seg
		}
		if roots[seg] { // module-rooted: the capsule mux answers, missing = 404
			modmux.ServeHTTP(w, r)
			return
		}
	}
	spaHandler(webuiFS).ServeHTTP(w, r)
}

// moduleRouter builds the capsule mux and the route-root set. The
// routes arrive already tenant-checked and cross-claim-checked by the
// registry (RegisterRoute), so re-registration here cannot panic: one
// pattern, one owner in the ledger, deduped before the mux sees it.
func moduleRouter(routes []core.ModuleRoute) (*http.ServeMux, map[string]bool) {
	mux := http.NewServeMux()
	roots := map[string]bool{}
	seen := map[string]bool{}
	for _, rt := range routes {
		if seen[rt.Pattern] {
			continue
		}
		seen[rt.Pattern] = true
		pattern, handler := rt.Pattern, rt.Handler
		mux.HandleFunc(pattern, handler)
		// the first path segment any method-scoped registration owns
		p := pattern
		if i := strings.IndexByte(p, ' '); i > 0 && !strings.ContainsAny(p[:i], "/{") {
			p = p[i+1:] // strip the "GET " method prefix
		}
		rest := strings.TrimPrefix(p, "/")
		if rest == "" {
			continue
		}
		root := "/" + rest
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			root = "/" + rest[:i]
		}
		roots[root] = true
	}
	return mux, roots
}