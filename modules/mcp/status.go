// Status: the read-only inventory the webui MCP dock tab lists (card
// t_c7136c50). GET /mcp/servers reports the configured servers as the
// bridge sees them — name, transport, connection state, per-server
// failure, and the discovered server-qualified tools the "mcp."
// namespace guard fronts (card t_6c0375b5). No live probe, no write
// path: enable/disable of servers is config.yaml domain (mcp.yaml /
// MCP_SERVERS), not this surface.
package mcp

import (
	"encoding/json"
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
	// Connected is true when a live connection survived the bridge's
	// mount-time connect (state as of mount, refreshed never - the
	// listing reads what the tool seam would route to).
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
// Read-only over data fixed at construction (the maps are written
// only in connect, at bridge-build time), so no locking.
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
	if p.bridge != nil {
		rep = p.bridge.Status()
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rep); err != nil {
		// header already sent; the connection carries the failure
		return
	}
}