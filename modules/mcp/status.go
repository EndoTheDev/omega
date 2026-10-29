// Status: the read-only inventory the webui MCP dock tab lists
// (t_c7136c50) plus the reload write path that keeps it truthful
// (card t_d6e94e79). GET /mcp/servers reports the configured servers
// as the LIVE bridge sees them - name, transport, connection state,
// per-server failure, and the discovered server-qualified tools the
// "mcp." namespace guard fronts (card t_6c0375b5). No live probe:
// enable/disable of servers is config.yaml domain (mcp.yaml /
// MCP_SERVERS), not this surface - its refresh button POSTs
// /mcp/reload, the module's only trigger (no file watcher).
package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// ServerStatus is the wire view of one configured MCP server.
type ServerStatus struct {
	Name      string   `json:"name"`
	// Transport names the config's transport: "http" (a url entry),
	// "stdio" (a command entry), "" (neither - the entry is invalid
	// and was never connected).
	Transport string   `json:"transport,omitempty"`
	Command   string   `json:"command,omitempty"`
	URL       string   `json:"url,omitempty"`
// Connected is true when a live connection survived the LIVE bridge's
// connect (the bridge never reconnects a server in place - a reload
// swaps the whole bridge, card t_d6e94e79).
	Connected bool     `json:"connected"`
	// Error carries the connect/tools-list failure, empty when the
	// server is healthy. A configured-but-unconnectable server stays
	// listed instead of vanishing into a stderr line.
	Error     string   `json:"error,omitempty"`
	// Tools lists the server-qualified names ("server.mcp-tool") the
	// bridge discovered - the identities the tool registry and the
	// namespace guard carry. Unsorted source, sorted view.
	Tools     []string `json:"tools,omitempty"`
}

// StatusReport is the GET /mcp/servers body. Source echoes where the
// config came from ("env" = MCP_SERVERS, "mcp.yaml"/"mcp.json" = the
// config file that was read, "none" = nothing configured).
type StatusReport struct {
	Source  string         `json:"source"`
	Servers []ServerStatus `json:"servers"`
}

// Status assembles the report from the bridge's retained config,
// live connections, recorded failures, and discovered tool names.
// Read-only over data fixed at connect (the maps are written only
// in connect, at bridge-build time; a reload swaps the whole bridge,
// it never mutates this one), so no locking.
func (b *Bridge) Status() StatusReport {
	rep := StatusReport{Source: b.source, Servers: []ServerStatus{}}
	if b.cfg == nil {
		return rep
	}
	for _, sc := range b.cfg.Servers {
		st := ServerStatus{
			Name:    sc.Name,
			Command: sc.Command,
			URL:     sc.URL,
		}
		switch {
		case sc.URL != "":
			st.Transport = "http"
		case sc.Command != "":
			st.Transport = "stdio"
		}
		st.Connected = b.servers[sc.Name] != nil
		st.Error = b.errs[sc.Name]
		// the server-qualified names group by prefix (the t_6c0375b5
		// convention: sc.Name + "." + mcp tool name)
		prefix := sc.Name + "."
		for name := range b.toolDefs {
			if strings.HasPrefix(name, prefix) {
				st.Tools = append(st.Tools, name)
			}
		}
		sort.Strings(st.Tools)
		rep.Servers = append(rep.Servers, st)
	}
	return rep
}

// handleStatus serves GET /mcp/servers. Registered even on a nil
// bridge: with no servers configured the tab reads an empty list, not
// a module-rooted miss (the empty state is contract, card t_c7136c50).
func (p *Plugin) handleStatus(w http.ResponseWriter, r *http.Request) {
	rep := StatusReport{Source: "none", Servers: []ServerStatus{}}
	if active := p.activeBridge(); active != nil {
		rep = active.Status()
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rep); err != nil {
		// header already sent; the connection carries the failure
		return
	}
}

// handleReload serves POST /mcp/reload (card t_d6e94e79): re-reads the
// mcp config with the existing loader, connects every configured
// server into a fresh bridge, and swaps it in - mcp.yaml edits go live
// with no server restart. Partial failure is per-server (the existing
// StatusReport shape): a server that cannot connect stays listed with
// its Error field and the reload still succeeds. A config-load failure
// fails the request and keeps the previous bridge serving untouched.
func (p *Plugin) handleReload(w http.ResponseWriter, r *http.Request) {
	rep, err := p.reload()
	if err != nil {
		http.Error(w, "mcp reload: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rep); err != nil {
		// header already sent; the connection carries the failure
		return
	}
}

// reload re-reads the configured servers and swaps the live bridge.
// The swap itself is the plugin's mutex-guarded one-liner: readers
// (every Tools() read, the status route) race it safely - they hold
// either the old bridge or the new one whole - while reloads serialize
// against each other on reloadMu. No lock is held across the connect
// sequence.
func (p *Plugin) reload() (StatusReport, error) {
	p.reloadMu.Lock()
	defer p.reloadMu.Unlock()

	cfg, source, err := loadConfig()
	if err != nil {
		// a broken config keeps the old bridge serving: the refresh
		// surfaces the failure on the panel's error line
		return StatusReport{}, fmt.Errorf("read config: %w", err)
	}
	next, connErr := NewBridge(cfg)
	if connErr != nil {
		return StatusReport{}, fmt.Errorf("build bridge: %w", connErr)
	}
	next.source = source

	old := p.swapBridge(next)
	if old != nil {
		// outside the swap lock, after the swap: removed servers
		// disconnect here, in-flight calls to them answer IsError
		old.shutdown()
	}
	return next.Status(), nil
}