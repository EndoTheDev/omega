package mcp

// entries.go: the MCP server entry CRUD write face (card t_2a649d87).
// Entries live in mcp.yaml - the file the bridge loads (the migration
// shape stated on the card: no store migration existed to make, the
// file IS the persistent registry, and the store capsule carries no
// generic KV to move them into). What this file adds is what was
// missing: add, update and delete at runtime, no restart.
//
// ONE write law, shared by the HTTP routes (POST /mcp/save, POST
// /mcp/delete) and the CLI verbs (`omega mcp add|edit|delete`): the
// entry is schema-validated first (nothing invalid reaches the disk),
// spliced into the document at the yaml.Node level (comments, key
// order and the file's style survive everywhere except inside the
// replaced entry itself - the cron save.go fold-write law), written
// atomically (tmp+rename, the toggle route's crash law), and then the
// LIVE bridge is swapped through the same reloadLocked path POST
// /mcp/reload rides. The old bridge's shutdown() is the unmount: stdio
// processes are killed and Wait()ed, in-flight calls to a removed
// server answer a structured IsError tool result - no orphan
// connections, no second lifecycle.
//
// Legacy sources stay read-only, with the toggle route's exact
// refusal: MCP_SERVERS is an env var (nowhere to persist) and mcp.json
// is hand-edited JSON (a fresh mcp.yaml would be shadowed by the
// loader's lookup order, so writing it would be a lie).

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// UnknownServerError marks a delete of a name no entry carries - the
// 404 lane (the silent-persist law in reverse: a no-op delete must be
// visible).
type UnknownServerError struct {
	Name string
}

func (e UnknownServerError) Error() string {
	return fmt.Sprintf("no mcp server named %q", e.Name)
}

// writableConfigFile resolves the mcp.yaml a CRUD write lands in.
// Refusals name the source when the entries are not writable through
// this module; a home with no config file resolves to the bootstrap
// path (config/mcp.yaml), created by the first add - the crons
// jobsDocument bootstrap shape.
func writableConfigFile() (string, error) {
	if os.Getenv("MCP_SERVERS") != "" {
		return "", fmt.Errorf("config came from MCP_SERVERS - it is not a file; edit the env var")
	}
	path, name, ok := configFile()
	if ok {
		if name != "mcp.yaml" {
			return "", fmt.Errorf("config came from %s - entries live in mcp.yaml; move the server there or edit it directly", name)
		}
		return path, nil
	}
	return filepath.Join(mcpHome(), "config", "mcp.yaml"), nil
}

// validateServerEntry is the one schema law every entry write obeys -
// the same fields the loader parses, checked before any byte reaches
// the disk. Rejections name the field (the crons ParseConfig voice).
func validateServerEntry(sc serverConfig) error {
	name := sc.Name
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("name is required")
	}
	if name != strings.TrimSpace(name) {
		return fmt.Errorf("name %q carries surrounding whitespace", name)
	}
	if strings.HasPrefix(name, "-") || strings.ContainsAny(name, " \t\r\n/\\") {
		return fmt.Errorf("name %q: a server name is one bare word (no spaces, slashes, leading dash)", name)
	}
	// Discovered tools surface as <server>.<tool>; a dotted server name
	// would mint a second dot and break the one-dot namespace law.
	if strings.Contains(name, ".") {
		return fmt.Errorf("name %q: a server name carries no dot - its tools already read <server>.<tool>", name)
	}
	switch {
	case sc.Command == "" && sc.URL == "":
		return fmt.Errorf("server %q: a command (stdio) or a url (http) is required", name)
	case sc.Command != "" && sc.URL != "":
		return fmt.Errorf("server %q: command and url are exclusive - one transport per entry", name)
	case sc.URL != "":
		if !strings.HasPrefix(sc.URL, "http://") && !strings.HasPrefix(sc.URL, "https://") {
			return fmt.Errorf("server %q: url must be http(s), got %q", name, sc.URL)
		}
		if len(sc.Args) != 0 {
			return fmt.Errorf("server %q: args belong to a stdio (command) entry", name)
		}
		if len(sc.Env) != 0 {
			return fmt.Errorf("server %q: env belongs to a stdio (command) entry", name)
		}
	default:
		if strings.TrimSpace(sc.Command) != sc.Command {
			return fmt.Errorf("server %q: command carries surrounding whitespace", name)
		}
		if len(sc.Headers) != 0 {
			return fmt.Errorf("server %q: headers belong to an http (url) entry", name)
		}
	}
	return nil
}

// serverEntryWire is the on-disk shape of one entry - struct field
// order is the key order a written entry carries. Separate from
// serverConfig so the reader's zero values never marshal as empty
// scalars (`command: ""` on an http entry, say).
type serverEntryWire struct {
	Name    string            `yaml:"name"`
	Command string            `yaml:"command,omitempty"`
	Args    []string          `yaml:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
	URL     string            `yaml:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty"`
	Enabled *bool             `yaml:"enabled,omitempty"`
}

type serverWriteDoc struct {
	Servers []serverEntryWire `yaml:"servers"`
}

// serverEntryNode builds one entry's mapping node from the validated
// entry. The bytes come from a marshal of the same shape the loader
// parses back, so an editor-produced mcp.yaml is lawful by
// construction.
func serverEntryNode(sc serverConfig) (*yaml.Node, error) {
	data, err := yaml.Marshal(serverWriteDoc{Servers: []serverEntryWire{{
		Name:    sc.Name,
		Command: sc.Command,
		Args:    sc.Args,
		Env:     sc.Env,
		URL:     sc.URL,
		Headers: sc.Headers,
		Enabled: sc.Enabled,
	}}})
	if err != nil {
		return nil, fmt.Errorf("marshal server: %w", err)
	}
	var mini yaml.Node
	if err := yaml.Unmarshal(data, &mini); err != nil {
		return nil, fmt.Errorf("marshal server: %w", err)
	}
	if len(mini.Content) != 1 {
		return nil, fmt.Errorf("marshal server: empty document")
	}
	root := mini.Content[0]
	if root.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(root.Content); i += 2 {
			key := root.Content[i]
			if key.Kind == yaml.ScalarNode && key.Value == "servers" && root.Content[i+1].Kind == yaml.SequenceNode {
				if seq := root.Content[i+1]; len(seq.Content) == 1 && seq.Content[0].Kind == yaml.MappingNode {
					return seq.Content[0], nil
				}
			}
		}
	}
	return nil, fmt.Errorf("marshal server: no entry produced")
}

// serversDocument reads the mcp.yaml at path and returns its document
// node plus the servers: sequence, creating a minimal `servers:` list
// when the file does not exist yet (the first add is the bootstrap for
// a fresh home) and appending the key when the file talks about
// something else.
func serversDocument(path string) (*yaml.Node, *yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("read %s: %w", path, err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, nil, fmt.Errorf("create %s: %w", filepath.Dir(path), err)
		}
		root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "servers"},
			&yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"},
		)
		return root, root.Content[1], nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(doc.Content) != 1 {
		return nil, nil, fmt.Errorf("parse %s: empty document", path)
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("parse %s: top-level document is not a mapping", path)
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i]
		if key.Kind == yaml.ScalarNode && key.Value == "servers" {
			val := root.Content[i+1]
			if val.Kind != yaml.SequenceNode {
				return nil, nil, fmt.Errorf("parse %s: 'servers' must be a list", path)
			}
			return &doc, val, nil
		}
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "servers"},
		seq,
	)
	return &doc, seq, nil
}

// upsertServerEntry writes the entry into the mcp.yaml at path: the
// mapping named sc.Name is replaced in place, or appended. The entry
// node comes from the very bytes that validated. Duplicate names are
// refused before any write touches the file (the toggle route's
// ambiguity law: the bridge resolves one name, two rows would lie).
func upsertServerEntry(path string, sc serverConfig) error {
	if err := validateServerEntry(sc); err != nil {
		return err
	}
	entry, err := serverEntryNode(sc)
	if err != nil {
		return err
	}
	doc, seq, err := serversDocument(path)
	if err != nil {
		return err
	}
	matches := 0
	for _, item := range seq.Content {
		if item.Kind == yaml.MappingNode && namesServer(item, sc.Name) {
			matches++
		}
	}
	if matches > 1 {
		return fmt.Errorf("parse %s: more than one server named %s - edit the file by hand and rename one", path, sc.Name)
	}
	replaced := false
	for _, item := range seq.Content {
		if item.Kind == yaml.MappingNode && namesServer(item, sc.Name) {
			item.Content = entry.Content
			replaced = true
			break
		}
	}
	if !replaced {
		seq.Content = append(seq.Content, entry)
	}
	return writeYAML(path, doc)
}

// removeServerEntry deletes the entry named name from the mcp.yaml at
// path. A missing file or a missing entry is UnknownServerError - never
// a silent no-op. Removing the last entry from a document that carries
// nothing else (no comments, no other keys) removes the FILE too: an
// add/delete round trip leaves the home exactly as it found it (the no-
// residue law). A hand-owned file with comments or other keys is never
// deleted - it just loses the entry.
func removeServerEntry(path, name string) error {
	doc, seq, err := serversDocument(path)
	if err != nil {
		return err
	}
	for i, item := range seq.Content {
		if item.Kind == yaml.MappingNode && namesServer(item, name) {
			seq.Content = append(seq.Content[:i], seq.Content[i+1:]...)
			if bareEmptyServersDocument(doc, seq) {
				if err := os.Remove(path); err != nil {
					return fmt.Errorf("remove %s: %w", path, err)
				}
				return nil
			}
			return writeYAML(path, doc)
		}
	}
	return UnknownServerError{Name: name}
}

// bareEmptyServersDocument reports whether the document carries an
// empty servers: list and nothing else worth keeping - no other keys,
// no comments anywhere. That is exactly the shape the first add
// bootstraps, so the last delete can remove it without touching
// operator-owned content.
func bareEmptyServersDocument(doc, seq *yaml.Node) bool {
	if seq == nil || len(seq.Content) != 0 {
		return false
	}
	if seq.HeadComment != "" || seq.LineComment != "" || seq.FootComment != "" {
		return false
	}
	root := doc
	if doc != nil && len(doc.Content) == 1 && doc.Content[0].Kind == yaml.MappingNode {
		root = doc.Content[0]
	}
	if root == nil || root.Kind != yaml.MappingNode || len(root.Content) != 2 {
		return false
	}
	if root.HeadComment != "" || root.LineComment != "" || root.FootComment != "" {
		return false
	}
	key := root.Content[0]
	if key.Kind != yaml.ScalarNode || key.Value != "servers" {
		return false
	}
	return key.HeadComment == "" && key.LineComment == "" && key.FootComment == ""
}

// ServerEntry returns the configured entry named name as the LIVE
// config carries it - the CLI's edit base (only flags the operator
// passed are applied over it). ok=false is an unknown name.
func (p *Plugin) ServerEntry(name string) (serverConfig, bool) {
	active := p.activeBridge()
	if active == nil || active.cfg == nil {
		return serverConfig{}, false
	}
	for _, sc := range active.cfg.Servers {
		if sc.Name == name {
			return sc, true
		}
	}
	return serverConfig{}, false
}

// saveServerAndReload performs one save whole: the validated node write
// and the bridge swap under ONE reloadMu hold (the toggle route's
// v0.14.1 audit shape - two quick saves can no longer race the
// read-parse-marshal-write). The write error and the reload error come
// back separately: a failed write answers 400 (the request's fault,
// file untouched), a failed reload 500 (the entry is written; the
// previous bridge keeps serving).
func (p *Plugin) saveServerAndReload(sc serverConfig) (rep StatusReport, writeErr, reloadErr error) {
	p.reloadMu.Lock()
	defer p.reloadMu.Unlock()
	path, err := writableConfigFile()
	if err != nil {
		return StatusReport{}, err, nil
	}
	if err := upsertServerEntry(path, sc); err != nil {
		return StatusReport{}, err, nil
	}
	rep, reloadErr = p.reloadLocked()
	return rep, nil, reloadErr
}

// deleteServerAndReload is saveServerAndReload's delete sibling: the
// node splice and the swap under one reloadMu hold. An unknown name is
// UnknownServerError (the 404 lane) and writes nothing.
func (p *Plugin) deleteServerAndReload(name string) (rep StatusReport, writeErr, reloadErr error) {
	p.reloadMu.Lock()
	defer p.reloadMu.Unlock()
	path, err := writableConfigFile()
	if err != nil {
		return StatusReport{}, err, nil
	}
	if err := removeServerEntry(path, name); err != nil {
		return StatusReport{}, err, nil
	}
	rep, reloadErr = p.reloadLocked()
	return rep, nil, reloadErr
}

// serverRequest is the POST /mcp/save body: one full entry, the
// serverConfig fields as the editor knows them. Enabled is optional -
// absent reads as on, the pre-flag default.
type serverRequest struct {
	Name    string            `json:"name"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Enabled *bool             `json:"enabled,omitempty"`
}

func (r serverRequest) toServerConfig() serverConfig {
	return serverConfig{
		Name:    r.Name,
		Command: r.Command,
		Args:    r.Args,
		Env:     r.Env,
		URL:     r.URL,
		Headers: r.Headers,
		Enabled: r.Enabled,
	}
}

// serverDeleteRequest is the POST /mcp/delete body: the entry's name as
// the editor knows it.
type serverDeleteRequest struct {
	Name string `json:"name"`
}

// handleSaveServer serves POST /mcp/save: upsert one entry into
// mcp.yaml, swap the live bridge, answer the fresh StatusReport (one
// response carries the landed truth - the crons save route's law). The
// route mounts even on a nil bridge: the first add wakes an idle
// module, exactly like the reload route.
func (p *Plugin) handleSaveServer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req serverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "mcp save: bad body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "mcp save: name is required", http.StatusBadRequest)
		return
	}
	rep, writeErr, reloadErr := p.saveServerAndReload(req.toServerConfig())
	if writeErr != nil {
		http.Error(w, "mcp save: "+writeErr.Error(), http.StatusBadRequest)
		return
	}
	if reloadErr != nil {
		http.Error(w, "mcp save: reload: "+reloadErr.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rep); err != nil {
		// header already sent; the connection carries the failure
		return
	}
}

// handleDeleteServer serves POST /mcp/delete: remove one entry from
// mcp.yaml, swap the live bridge (the removed entry's connections close
// in the swap), answer the fresh StatusReport. Unknown name = 404.
func (p *Plugin) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req serverDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "mcp delete: bad body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "mcp delete: name is required", http.StatusBadRequest)
		return
	}
	rep, writeErr, reloadErr := p.deleteServerAndReload(req.Name)
	if writeErr != nil {
		var unknown UnknownServerError
		if errors.As(writeErr, &unknown) {
			http.Error(w, "mcp delete: "+writeErr.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, "mcp delete: "+writeErr.Error(), http.StatusBadRequest)
		return
	}
	if reloadErr != nil {
		http.Error(w, "mcp delete: reload: "+reloadErr.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rep); err != nil {
		return
	}
}
