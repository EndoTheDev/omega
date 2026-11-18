package api

// The HTTP channel's serving skeleton.

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/agent"
	agentapi "github.com/EndoTheDev/omega/agent/api"
	"github.com/EndoTheDev/omega/core"
)

//go:embed static/*
var staticFS embed.FS

// webuiFS is the embedded SPA at the FS root, so the FileServer at "/" finds.
var webuiFS, _ = fs.Sub(staticFS, "static")

// Server is the serving shell around the base API server.
type Server struct {
	*agentapi.Server
	httpSrv *http.Server
	version string
	startedAt time.Time
	// themes serves GET /themes backed by the OMEGA_HOME/themes folder, watched with the.
	// Nil-safe: no home configured means an empty list and no watcher.
	themes *themeRegistry
	// healthMu guards healthProvider, the cached provider reachability verdict: the
	// monitor goroutine writes it, every health read copies it. Nil until the first probe
	// lands - the health body then carries no provider fact at all.
	healthMu       sync.Mutex
	healthProvider *agentapi.HealthProvider
	// logSeeded is test observability, nil in production. A test appends only after that
	// baseline, so it cannot race the seed and watch its line get swallowed. Set before
	// the follower starts; read once, never written back.
	logSeeded chan struct{}
}

// NewServer creates the shell server.
func NewServer(a *agent.Agent, tools map[string]core.Tool, store agent.StoreProvider, settings core.Settings, modules core.ModuleToggles, registries core.RegistryToggles) *Server {
	s := &Server{Server: agentapi.NewServer(a, tools, store, settings, modules, registries), startedAt: time.Now(), themes: newThemeRegistry(resolveThemesDir())}
	mux := s.Server.Mux()
	mux.HandleFunc("/health", s.handleHealth)
	// The push channel: one websocket per browser connection, every update a typed topic.
	mux.HandleFunc("/ws", s.handleSocket)
	mux.HandleFunc("/themes", s.handleThemes)
	// v0.12 E2: the module toggle surface.
	mux.HandleFunc("GET /modules", s.handleModules)
	mux.HandleFunc("PUT /modules/{name}", s.handleModuleToggle)
	// v0.14.1: the registry-browser toggle surface.
	mux.HandleFunc("GET /disabled", s.handleDisabled)
	mux.HandleFunc("PUT /disabled/skills", s.handleDisabledSkills)
	mux.HandleFunc("PUT /disabled/tools", s.handleDisabledTools)
	mux.HandleFunc("PUT /disabled/agents", s.handleDisabledAgents)
	mux.HandleFunc("/cron/jobs", s.handleCronJobs)
	mux.HandleFunc("/skills", s.handleSkills)
	mux.HandleFunc("/logs", s.handleLogs)
	mux.HandleFunc("/extensions", s.handleExtensions)
	// SPA + module REST: the built webui is embedded in the binary and served at "/".
	mux.HandleFunc("/", s.handleModuleOrSPA)
	// v0.14.1 audit p10: the unauthenticated surface is loopback-local, so cross-origin.
	s.httpSrv = &http.Server{Handler: &hostGuard{next: s.Server.Handler()}}
	return s
}

// SetPublicHosts hands the Host guard the operator-declared public identities
// (server.public_hosts): a reverse proxy or tunnel forwards the browser's domain as
// Host, which never matches the bind, so the domain must be declared to pass.
func (s *Server) SetPublicHosts(hosts []string) {
	if g, ok := s.httpSrv.Handler.(*hostGuard); ok && len(hosts) > 0 {
		set := make(map[string]bool, len(hosts))
		for _, h := range hosts {
			set[h] = true
		}
		g.public.Store(set)
	}
}

// rewireSeams computes the capsule mux (the module vocab is the shell's) and hands.
func (s *Server) rewireSeams(ctx *core.Context) error {
	modmux, roots := moduleRouter(ctx.ModuleRoutes)
	return s.Server.RewireSeams(ctx, modmux, roots)
}

// writeJSON / getOnly: the shell reuses the base helpers through the exported seams.
func writeJSON(w http.ResponseWriter, status int, v any)   { agentapi.WriteJSON(w, status, v) }
func getOnly(w http.ResponseWriter, r *http.Request) bool  { return agentapi.GetOnly(w, r) }
func writeSettingsJSON(w http.ResponseWriter, payload any) { agentapi.WriteSettingsJSON(w, payload) }

func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.Trim(r.URL.Path, "/")
		if name != "" {
			if _, err := fs.Stat(fsys, name); err != nil {
				// asset-like miss: /_nuxt/** explicitly, plus any dotted path (favicon.ico,
				// robots.txt, *.js) - the shell is for route paths, never for missing files
				assetLike := strings.HasPrefix(name, "_nuxt/") ||
					strings.Contains(path.Base(name), ".")
				if assetLike {
					fileServer.ServeHTTP(w, r)
					return
				}
				b, err := fs.ReadFile(fsys, "index.html")
				if err != nil {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write(b) // best-effort static response
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}

// Handler returns the HTTP handler, for tests and embedding.
func (s *Server) Handler() http.Handler {
	return s.httpSrv.Handler
}

// Serve runs the HTTP server on addr until ctx is cancelled, then shuts down. It
// returns the ListenAndServe error, or nil on a clean shutdown.
func (s *Server) Serve(ctx context.Context, addr string) error {
	s.httpSrv.Addr = addr
	// the Host/Origin guard learns the bind here (before the listener accepts anything) -
	// the allowed-Host set is derived from it, and a bind the guard cannot defend refuses
	// to start instead of failing open
	if g, ok := s.httpSrv.Handler.(*hostGuard); ok {
		if err := g.bind(addr); err != nil {
			return err
		}
	}
	go s.themes.watch(ctx)
	// the push channel's heartbeat: started here, after the listener is
	// up (the goroutine fence - mount never starts background work).
	go s.pushHealth(ctx)
	// the logs topic's delta follower.
	go s.followLogs(ctx)
	// the provider reachability probe: one model-list round trip a minute, cached for the
	// health body's provider fact - never on the 5s heartbeat frame.
	go s.monitorHealth(ctx)
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.httpSrv.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.httpSrv.Shutdown(shutdownCtx)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// The same body the health topic pushes (api/ws.go).
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.healthState()) // best-effort response body
}
