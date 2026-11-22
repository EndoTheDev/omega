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

type ServerStatus struct {
	Name string `json:"name"`
	// Transport names the config's transport: "http" (a url entry), "stdio" (a command
	// entry), "" (neither - the entry is invalid and was never connected).
	Transport string `json:"transport,omitempty"`
	Command   string `json:"command,omitempty"`
	URL       string `json:"url,omitempty"`
	// Connected is true when a live connection survived the LIVE bridge's connect. A
	// disabled server (Enabled false) is never connected: the toggle is a config decision,
	// not a connect failure.
	Connected bool `json:"connected"`
	Enabled bool `json:"enabled"`
	Error string `json:"error,omitempty"`
	// Tools lists the server-qualified names ("server.mcp-tool") the bridge discovered.
	// Unsorted source, sorted view.
	Tools []string `json:"tools,omitempty"`
}

// StatusReport is the GET /mcp/servers body.
type StatusReport struct {
	Source  string         `json:"source"`
	Servers []ServerStatus `json:"servers"`
}

// Status assembles the report from the bridge's retained config, live connections.
// Read-only over data fixed at connect (the maps are written only in connect, at
// bridge-build time; a reload swaps the whole bridge, it never mutates this one), so no
// locking.
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

// handleStatus serves GET /mcp/servers. Registered even on a nil bridge: with no
// servers configured the tab reads an empty list, not a module-rooted miss.
func (p *Plugin) handleStatus(w http.ResponseWriter, r *http.Request) {
	rep := StatusReport{Source: "none", Servers: []ServerStatus{}}
	if active := p.activeBridge(); active != nil {
		rep = active.Status()
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rep); err != nil {
		return
	}
}

// handleReload serves POST /mcp/reload. Partial failure is per-server (the existing
// StatusReport shape): a server that cannot connect stays listed with its Error field
// and the reload still succeeds.
func (p *Plugin) handleReload(w http.ResponseWriter, r *http.Request) {
	rep, err := p.reload()
	if err != nil {
		http.Error(w, "mcp reload: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rep); err != nil {
		return
	}
}

// reload re-reads the configured servers and swaps the live bridge. The swap itself is
// the plugin's mutex-guarded one-liner: readers (every Tools read, the status route)
// race it safely - they hold either the old bridge or the new one whole - while reloads
// serialize against each other on reloadMu.
func (p *Plugin) reload() (StatusReport, error) {
	p.reloadMu.Lock()
	defer p.reloadMu.Unlock()
	return p.reloadLocked()
}

// setServerEnabledAndReload performs one toggle whole: the flag write and the bridge
// swap under ONE reloadMu hold, so two quick toggles cannot race the
// read-parse-marshal-write of mcp.yaml and lose the first write.
func (p *Plugin) setServerEnabledAndReload(path, name string, enabled bool) (rep StatusReport, writeErr, reloadErr error) {
	p.reloadMu.Lock()
	defer p.reloadMu.Unlock()
	if err := setServerEnabled(path, name, enabled); err != nil {
		return StatusReport{}, err, nil
	}
	rep, reloadErr = p.reloadLocked()
	return rep, nil, reloadErr
}

// reloadLocked is reload's body without the lock.
func (p *Plugin) reloadLocked() (StatusReport, error) {
	cfg, source, err := loadConfig()
	if err != nil {
		return StatusReport{}, fmt.Errorf("read config: %w", err)
	}
	next, connErr := NewBridge(cfg)
	if connErr != nil {
		return StatusReport{}, fmt.Errorf("build bridge: %w", connErr)
	}
	next.source = source

	old := p.swapBridge(next)
	if old != nil {
		old.shutdown()
	}
	return next.Status(), nil
}

// toggleRequest is the POST /mcp/toggle body: one server name, one target state.
type toggleRequest struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// handleToggle serves POST /mcp/toggle. The wire responds with the fresh StatusReport,
// everything the tab needs to re-render, so toggle.ts/reload.ts need no follow-up GET.
// Refusals (400, the panel's error line): MCP_SERVERS - the env var is not a file, there
// is nowhere to persist; "none" - nothing configured; mcp.json - JSON entries stay
// hand-edited, writing YAML semantics into JSON would corrupt them.
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
	rep, writeErr, reloadErr := p.setServerEnabledAndReload(path, req.Name, req.Enabled)
	if writeErr != nil {
		http.Error(w, "mcp toggle: "+writeErr.Error(), http.StatusBadRequest)
		return
	}
	if reloadErr != nil {
		http.Error(w, "mcp toggle: reload: "+reloadErr.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rep); err != nil {
		return
	}
}

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
	matches := 0
	for _, item := range servers.Content {
		if item.Kind == yaml.MappingNode && namesServer(item, name) {
			matches++
		}
	}
	if matches > 1 {
		return fmt.Errorf("parse %s: mcp.yaml carries more than one server named %s - edit the file by hand and rename one", path, name)
	}
	for _, item := range servers.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if !namesServer(item, name) {
			continue
		}
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

func namesServer(item *yaml.Node, name string) bool {
	for i := 0; i+1 < len(item.Content); i += 2 {
		key, val := item.Content[i], item.Content[i+1]
		if key.Kind != yaml.ScalarNode || key.Value != "name" {
			continue
		}
		return val.Kind == yaml.ScalarNode && val.Value == name
	}
	return false
}

func enabledValue(enabled bool) string {
	if enabled {
		return "true"
	}
	return "false"
}

// writeYAML marshals the whole document node and replaces the file atomically: a crash
// mid-write must never truncate the live mcp.yaml and silently drop every configured
// server - the tmp dies alone and the old file survives the rename miss. The write is
// the caller's responsibility to surface: a parse or lookup failure never reaches this
// point (the file stays untouched).
func writeYAML(path string, doc *yaml.Node) error {
	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	mode := os.FileMode(0o644)
	if fi, statErr := os.Stat(path); statErr == nil {
		mode = fi.Mode()
	}
	tmp := path + ".omega-toggle.tmp"
	if err := os.WriteFile(tmp, out, mode); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp) // best-effort cleanup
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
