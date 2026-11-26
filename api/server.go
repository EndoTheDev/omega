package api

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

//go:embed all:static/*
var staticFS embed.FS

// webuiFS is the embedded SPA at the FS root, where the FileServer at "/" looks.
var webuiFS, _ = fs.Sub(staticFS, "static")

// Server is the serving shell around the base API server.
type Server struct {
	*agentapi.Server
	httpSrv *http.Server
	version string
	startedAt time.Time
	// themes serves GET /themes from the OMEGA_HOME/themes folder, watched; nil-safe.
	themes *themeRegistry
	// healthMu guards healthProvider, the cached provider reachability verdict: the monitor
	// goroutine writes it, every health read copies it. Nil until the first probe lands.
	healthMu       sync.Mutex
	healthProvider *agentapi.HealthProvider
	// logSeeded is test observability, nil in production: a test appends only after that
	// baseline, set before the follower starts and never written back.
	logSeeded chan struct{}
	// update is the host's update engine behind /updates; nil answers 501.
	update core.UpdateSurface
}

// NewServer creates the shell server.
func NewServer(a *agent.Agent, tools map[string]core.Tool, store agent.StoreProvider, settings core.Settings, modules core.ModuleToggles, registries core.RegistryToggles) *Server {
	s := &Server{Server: agentapi.NewServer(a, tools, store, settings, modules, registries), startedAt: time.Now(), themes: newThemeRegistry(resolveThemesDir())}
	mux := s.Server.Mux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ws", s.handleSocket)
	mux.HandleFunc("/themes", s.handleThemes)
	mux.HandleFunc("GET /modules", s.handleModules)
	mux.HandleFunc("PUT /modules/{name}", s.handleModuleToggle)
	mux.HandleFunc("GET /disabled", s.handleDisabled)
	mux.HandleFunc("PUT /disabled/skills", s.handleDisabledSkills)
	mux.HandleFunc("PUT /disabled/tools", s.handleDisabledTools)
	mux.HandleFunc("PUT /disabled/agents", s.handleDisabledAgents)
	mux.HandleFunc("/cron/jobs", s.handleCronJobs)
	mux.HandleFunc("/skills", s.handleSkills)
	mux.HandleFunc("/logs", s.handleLogs)
	mux.HandleFunc("/extensions", s.handleExtensions)
	// The update channel: one host engine behind check / stage / finalize / status.
	mux.HandleFunc("GET /updates/status", s.handleUpdateStatus)
	mux.HandleFunc("POST /updates/check", s.handleUpdateCheck)
	mux.HandleFunc("POST /updates/stage", s.handleUpdateStage)
	mux.HandleFunc("POST /updates/finalize", s.handleUpdateFinalize)
	mux.HandleFunc("POST /updates/discard", s.handleUpdateDiscard)
	// SPA + module REST: the built webui is embedded in the binary and served at "/".
	mux.HandleFunc("/", s.handleModuleOrSPA)
	// The unauthenticated surface is loopback-local, so cross-origin writes are the threat.
	s.httpSrv = &http.Server{Handler: &hostGuard{next: s.Server.Handler()}}
	return s
}

// SetPublicHosts hands the Host guard the operator-declared public identities
// (server.public_hosts): a tunneled browser domain arrives as Host, never matching the
// bind, so the declaration is what lets it pass.
func (s *Server) SetPublicHosts(hosts []string) {
	if g, ok := s.httpSrv.Handler.(*hostGuard); ok && len(hosts) > 0 {
		set := make(map[string]bool, len(hosts))
		for _, h := range hosts {
			// lowercased: the guard compares a lowercased Host header, and a declaration is
			// a name the operator typed, not a wire spelling.
			set[strings.ToLower(h)] = true
		}
		g.public.Store(set)
	}
}

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
				_, _ = w.Write(b)
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

// Serve runs the HTTP server on addr until ctx is cancelled, then shuts down.
func (s *Server) Serve(ctx context.Context, addr string) error {
	s.httpSrv.Addr = addr
	// the Host/Origin guard learns the bind here, before the listener accepts anything:
	// the allowed-Host set derives from it, and an undefendable bind refuses to start
	if g, ok := s.httpSrv.Handler.(*hostGuard); ok {
		if err := g.bind(addr); err != nil {
			return err
		}
	}
	go s.themes.watch(ctx)
	// The push channel's heartbeat starts only here, once the listener is up: mount never
	// starts background work.
	go s.pushHealth(ctx)
	go s.followLogs(ctx)
	// The provider reachability probe: one model-list round trip a minute, never on the
	// 5s heartbeat frame.
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
	_ = json.NewEncoder(w).Encode(s.healthState())
}
