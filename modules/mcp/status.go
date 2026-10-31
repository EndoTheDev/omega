// Status: the read-only inventory the webui MCP dock tab lists
// (t_c7136c50) plus the reload write path that keeps it truthful
// (card t_d6e94e79) and the per-server toggle persist (card
// t_83f37525). GET /mcp/servers reports the configured servers
// as the LIVE bridge sees them - name, transport, connection state,
// per-server failure, and the discovered server-qualified tools the
// "mcp." namespace guard fronts (card t_6c0375b5). No live probe:
// enable/disable of servers is mcp.yaml domain - refresh POSTs
// /mcp/reload; the per-server toggle POSTs /mcp/toggle, which writes
// the `enabled:` flag into mcp.yaml (the file owns the servers) and
// reloads through the same bridge swap the refresh rides.
package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ServerStatus is the wire view of one configured MCP server.
type ServerStatus struct {
	Name string `json:"name"`
	// Transport names the config's transport: "http" (a url entry),
	// "stdio" (a command entry), "" (neither - the entry is invalid
	// and was never connected).
	Transport string `json:"transport,omitempty"`
	Command   string `json:"command,omitempty"`
	URL       string `json:"url,omitempty"`
	// Connected is true when a live connection survived the LIVE bridge's
	// connect (the bridge never reconnects a server in place - a reload
	// swaps the whole bridge, card t_d6e94e79). A disabled server
	// (Enabled false) is never connected: the toggle is a config
	// decision, not a connect failure.
	Connected bool `json:"connected"`
	// Enabled is the per-server toggle as the LIVE config carries it
	// (card t_83f37525): false means the bridge deliberately skipped
	// this server - no connection, no discovered tools, no Error - so
	// the UI renders the row muted with the toggle off. The field is
	// plain bool (always serialized): the wire's absent-flag shape
	// reads as enabled, so old views keep rendering.
	Enabled bool `json:"enabled"`
	// Error carries the connect/tools-list failure, empty when the
	// server is healthy. A configured-but-unconnectable server stays
	// listed instead of vanishing into a stderr line.
	Error string `json:"error,omitempty"`
	// Tools lists the server-qualified names ("server.mcp-tool") the
	// bridge discovered - the identities the tool registry and the
	// namespace guard carry. Unsorted source, sorted view.
	Tools []string `json:"tools,omitempty"`
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
			Enabled: sc.isEnabled(),
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

// toggleRequest is the POST /mcp/toggle body (card t_83f37525): one
// server name, one target state.
type toggleRequest struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// handleToggle serves POST /mcp/toggle: write the per-server
// `enabled:` flag into the config file the LIVE bridge was loaded
// from (mcp.yaml owns the servers, card t_83f37525), then reload
// through the shared bridge swap so the listing, the tools, and the
// connections all follow. The wire responds with the fresh
// StatusReport (everything the tab needs to re-render, no second
// fetch required by the route contract - the UI still re-fetches, the
// reload.ts ruling).
//
// Refusals (400, the panel's error line): MCP_SERVERS - the env var
// is not a file, there is nowhere to persist; "none" - nothing
// configured; mcp.json - JSON entries stay hand-edited, writing YAML
// semantics into JSON would corrupt them. A write that fails after
// the config parse keeps the file untouched (the flag write happens
// whole) and the previous bridge serving untouched.
func (p *Plugin) handleToggle(w http.ResponseWriter, r *http.Request) {
	var req toggleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, "mcp toggle: name and enabled are required", http.StatusBadRequest)
		return
	}
	if os.Getenv("MCP_SERVERS") != "" {
		http.Error(w,
			"mcp toggle: config came from MCP_SERVERS - it is not a file, set the flag there",
			http.StatusBadRequest)
		return
	}
	path, name, ok := configFile()
	if !ok {
		http.Error(w, "mcp toggle: no mcp config file found - nothing to toggle",
			http.StatusBadRequest)
		return
	}
	if name != "mcp.yaml" {
		http.Error(w,
			"mcp toggle: config came from "+name+" - the enabled flag lives on the mcp.yaml entry; move the server there or edit it directly",
			http.StatusBadRequest)
		return
	}
	if err := setServerEnabled(path, req.Name, req.Enabled); err != nil {
		http.Error(w, "mcp toggle: "+err.Error(), http.StatusBadRequest)
		return
	}
	rep, err := p.reload()
	if err != nil {
		// the file is written; the failed reload keeps the previous
		// bridge serving (the refresh surfaces it the same way)
		http.Error(w, "mcp toggle: reload: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rep); err != nil {
		// header already sent; the connection carries the failure
		return
	}
}

// setServerEnabled flips the per-server `enabled:` flag on the named
// server entry in mcp.yaml. The write is the fold-write pattern (the
// config.yaml writeDisabled lesson): parse at the yaml.Node level,
// mutate the one scalar, marshal the WHOLE document node, so comments,
// key order, unknown keys, and the file's style survive - a struct
// round-trip would normalize them away. Missing `enabled:` keys are
// appended to the server's mapping; the bool is written as the bare
// `true`/`false` scalar YAML writes at the node level.
func setServerEnabled(path, name string, enabled bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if len(doc.Content) != 1 {
		return fmt.Errorf("parse %s: empty document", path)
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("parse %s: top-level document is not a mapping", path)
	}
	var servers *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, val := root.Content[i], root.Content[i+1]
		if key.Kind == yaml.ScalarNode && key.Value == "servers" && val.Kind == yaml.SequenceNode {
			servers = val
			break
		}
	}
	if servers == nil {
		return fmt.Errorf("parse %s: no servers: array", path)
	}
	flag := enabledValue(enabled)
	for _, item := range servers.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		named := false
		for i := 0; i+1 < len(item.Content); i += 2 {
			key, val := item.Content[i], item.Content[i+1]
			if key.Kind != yaml.ScalarNode || key.Value != "name" {
				continue
			}
			named = val.Kind == yaml.ScalarNode && val.Value == name
			break
		}
		if !named {
			continue
		}
		// the named entry: replace an existing `enabled:` scalar in
		// place, or append the key/value pair.
		for i := 0; i+1 < len(item.Content); i += 2 {
			key, val := item.Content[i], item.Content[i+1]
			if key.Kind == yaml.ScalarNode && key.Value == "enabled" {
				val.Kind, val.Tag, val.Value = yaml.ScalarNode, "!!bool", flag
				return writeYAML(path, &doc)
			}
		}
		item.Content = append(item.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "enabled"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: flag},
		)
		return writeYAML(path, &doc)
	}
	return fmt.Errorf("parse %s: no server named %s", path, name)
}

// enabledValue renders the flag the way yaml node scalars carry bools.
func enabledValue(enabled bool) string {
	if enabled {
		return "true"
	}
	return "false"
}

// writeYAML marshals the whole document node and replaces the file -
// comments and style survive because they rode the node tree. The
// write is the caller's responsibility to surface: a parse or lookup
// failure never reaches this point (the file stays untouched).
func writeYAML(path string, doc *yaml.Node) error {
	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	// the config files are operator-owned: the same mode the
	// fold-write uses (no chmod churn)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
