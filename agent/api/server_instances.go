package api

// The connection switcher's backend: GET/PUT /instances (a SERVE's own list) and GET
// /instances/probe (the add-time check). The list's home is config.yaml
// (`desktop.instances`); the host's Settings implementation owns the write (one atomic
// writer, one validation pass), this file owns the wire shape. A desktop window never
// calls these doors: its own list is answered by the app (desktop_windows.go), because
// the page it is showing may have been served by an older build.

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// instanceLister is the channel's own face on the host's saved-instance store. It rides
// the settings seam's implementation instead of widening core.Settings: a host without
// it answers 501, the same nil-safe floor the settings face keeps.
type instanceLister interface {
	Instances() ([]core.SavedInstance, error)
	SaveInstances([]core.SavedInstance) error
}

// handleInstances reads the list (GET) or replaces it whole (PUT).
func (s *Server) handleInstances(w http.ResponseWriter, r *http.Request) {
	lister, ok := s.settings.(instanceLister)
	if !ok {
		http.Error(w, "instances not configured", http.StatusNotImplemented)
		return
	}
	switch r.Method {
	case http.MethodGet:
		list, err := lister.Instances()
		if err != nil {
			http.Error(w, "load instances: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeInstancesJSON(w, list)
	case http.MethodPut:
		var body struct {
			Instances []core.SavedInstance `json:"instances"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
			return
		}
		// The validator is core's: a hand edit of config.yaml passes the same one.
		list, err := core.NormalizeInstances(body.Instances)
		if err != nil {
			http.Error(w, "instances: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := lister.SaveInstances(list); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeInstancesJSON(w, list)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// writeInstancesJSON answers the {"instances":[...]} envelope - never a bare array - and
// an empty list, never null.
func writeInstancesJSON(w http.ResponseWriter, list []core.SavedInstance) {
	if list == nil {
		list = []core.SavedInstance{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(struct {
		Instances []core.SavedInstance `json:"instances"`
	}{Instances: list})
}

// The add-time check. A browser cannot read another origin's health body (no CORS on this
// surface by design), so the SERVE fetches on the page's behalf and answers one envelope:
// the address has a valid shape, something answers there, and what answers is omega. The
// route never writes and never caches - it answers a question about one address.

// The probe's bounds. A failing probe must land in a human's attention span, and a hostile
// or broken target must not be able to hold the connection or the memory.
var (
	instanceProbeTimeout     = 5 * time.Second
	instanceProbeDialTimeout = 3 * time.Second
	instanceProbeBodyCap     = int64(64 << 10)
)

// The failure classes. Each one is a different thing for the operator to do, so the
// envelope names which one answered - the pane prints `reason` and offers the hatch on
// every class but `syntax` (a shape the file's own validator refuses can never be saved).
const (
	probeFailureSyntax      = "syntax"
	probeFailureUnreachable = "unreachable"
	probeFailureNotOmega    = "not-omega"
	probeFailureGuard       = "guard"
)

// probeRowName names the throwaway row the ONE validator sees. The validator prefixes its
// refusals with the row's name, and the probe strips it back off so the pane prints the
// validator's own words.
const probeRowName = "address"

// InstanceProbeAnswer is the wire shape: one envelope for both outcomes, so a caller reads
// `ok` and then either `facts` or `failure`+`reason`. 200 either way - a refusal is an
// answer, not a transport failure.
type InstanceProbeAnswer struct {
	OK      bool            `json:"ok"`
	URL     string          `json:"url"`
	State   string          `json:"state,omitempty"`
	Facts   json.RawMessage `json:"facts,omitempty"`
	Failure string          `json:"failure,omitempty"`
	Reason  string          `json:"reason,omitempty"`
}

// handleInstanceProbe serves GET /instances/probe?url=<address>. Same-origin only (the
// shell's own Host/Origin guard), loopback by default, and deliberately NOT
// internal-address filtered: a tailnet or LAN address is the whole point of the list.
func (s *Server) handleInstanceProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	answer := probeInstanceAddress(r.URL.Query().Get("url"))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(answer)
}

// probeInstanceAddress runs the four checks in order and answers the first that fails.
func probeInstanceAddress(typed string) InstanceProbeAnswer {
	refused := func(address, failure, reason string) InstanceProbeAnswer {
		return InstanceProbeAnswer{URL: address, Failure: failure, Reason: reason}
	}

	raw := strings.TrimSpace(typed)
	if raw == "" {
		return refused("", probeFailureSyntax, "url is required")
	}
	const maxAddress = 2048
	if len(raw) > maxAddress {
		return refused("", probeFailureSyntax, fmt.Sprintf("an address is at most %d characters", maxAddress))
	}
	// syntax: the same validator a hand edit of config.yaml and a PUT ride, so the pane can
	// never teach a spelling the file refuses.
	rows, err := core.NormalizeInstances([]core.SavedInstance{{Name: probeRowName, URL: raw}})
	if err != nil {
		return refused("", probeFailureSyntax, strings.TrimPrefix(err.Error(), probeRowName+": "))
	}
	target := rows[0].URL

	parsed, err := url.Parse(target)
	if err != nil || parsed.Host == "" {
		// unreachable through the ONE validator above; kept as a refusal rather than a panic.
		return refused(target, probeFailureSyntax, "url needs a host")
	}
	healthURL := strings.TrimSuffix(target, "/") + "/health"

	client := instanceProbeClient()
	defer client.CloseIdleConnections()
	req, err := http.NewRequest(http.MethodGet, healthURL, nil)
	if err != nil {
		return refused(target, probeFailureSyntax, "url is not a valid address")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "omega-instance-probe")

	resp, err := client.Do(req)
	if err != nil {
		return refused(target, probeFailureUnreachable, "unreachable at that address ("+clampProbeCause(err)+")")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, instanceProbeBodyCap))
	if err != nil {
		return refused(target, probeFailureUnreachable, "unreachable at that address (the answer could not be read)")
	}

	// guard: an omega answering 403 with its own Host-guard sentence IS an instance - it
	// refuses the NAME the operator typed. Its own class, because calling it not-omega would
	// tell the operator a real server is not one and leave the hatch as the only path.
	if resp.StatusCode == http.StatusForbidden && strings.Contains(string(body), core.GuardHostRefusal) {
		return refused(target, probeFailureGuard, fmt.Sprintf(
			"its Host guard refuses the name %s - that box needs the same own-name allowance in its build, or `%s` in its server.public_hosts",
			parsed.Hostname(), parsed.Host))
	}

	// omega-ness: the shape the health panel already reads - status "ok" and a numeric
	// uptime. Anything else, a 200 SPA page included, is not an omega.
	var health struct {
		Status        string   `json:"status"`
		UptimeSeconds *float64 `json:"uptime_seconds"`
	}
	if resp.StatusCode != http.StatusOK ||
		json.Unmarshal(body, &health) != nil ||
		health.Status != "ok" || health.UptimeSeconds == nil {
		return refused(target, probeFailureNotOmega, fmt.Sprintf(
			"that endpoint is not an omega instance (answered %d, body is not omega's health shape)", resp.StatusCode))
	}

	return InstanceProbeAnswer{OK: true, URL: target, State: "omega", Facts: json.RawMessage(body)}
}

// instanceProbeClient is the probe's outbound client. No redirect hops: a hop is a
// different address than the one the operator named, and the probe's job is to prove THAT
// address. No proxy either - the environment's HTTP_PROXY does not reach a tailnet peer.
func instanceProbeClient() *http.Client {
	return &http.Client{
		Timeout: instanceProbeTimeout,
		Transport: &http.Transport{
			DialContext:         (&net.Dialer{Timeout: instanceProbeDialTimeout}).DialContext,
			TLSHandshakeTimeout: instanceProbeDialTimeout,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// clampProbeCause is one line of transport cause, bounded: the pane prints it as the
// parenthetical reason, not as a stack.
func clampProbeCause(err error) string {
	line := strings.Join(strings.Fields(err.Error()), " ")
	const max = 160
	if len(line) > max {
		line = line[:max] + "..."
	}
	return line
}

// --- the list's per-row states ----------------------------------------------------
// GET /instances/status?urls=<a>&urls=<b> is the panel's one read: the URLs come from the
// CLIENT because the desktop's list is app-local, so a page served by a remote must never
// be handed dots for that remote's own (empty) list. Stateless - no cache, no server-side
// polling loop - and one dead row never holds another's verdict: the probes run four at a
// time under the same per-address bound as the add-time check.

// The dot's inputs. `unverified` is the pane's own read of the hatch flag, which lives in
// the app's list and never on the wire; the server's rows say the other three.
const (
	InstanceHealthy    = "healthy"
	InstanceWarning    = "warning"
	InstanceError      = "error"
	InstanceUnverified = "unverified"
)

// instanceStatusWorkers bounds the probes in flight, so 32 rows answer in bounded rounds
// instead of one slow address per round.
var instanceStatusWorkers = 4

// instanceStatusRow is one address's verdict: the state the dot wears, the failure class
// when the probe refused, the operator-facing reason, and the health body when one came
// back (the tooltip's facts).
type instanceStatusRow struct {
	URL     string          `json:"url"`
	State   string          `json:"state"`
	Failure string          `json:"failure,omitempty"`
	Reason  string          `json:"reason,omitempty"`
	Facts   json.RawMessage `json:"facts,omitempty"`
}

// InstanceState maps one probe answer to the dot's state. Stated once here, so the route
// and its tests can never disagree about what green means:
//   - an ANSWER that is not omega is error whatever the flag says (we know what it is,
//     and it is wrong);
//   - no answer at all is error, softened to unverified on a hatched row;
//   - an omega answer is warning when a SOURCED fact is false, healthy otherwise - a body
//     that does not carry a fact is unknown, not degraded.
//
// last_error is deliberately not an input: it is a historical fact, so a row would sit
// orange forever. It stays in the tooltip.
func InstanceState(answer InstanceProbeAnswer, unverified bool) string {
	switch answer.Failure {
	case probeFailureNotOmega:
		return InstanceError
	case probeFailureSyntax, probeFailureUnreachable, probeFailureGuard:
		if unverified {
			return InstanceUnverified
		}
		return InstanceError
	}
	if !answer.OK {
		return InstanceError
	}
	var facts struct {
		Provider *struct {
			Reachable *bool `json:"reachable"`
		} `json:"provider"`
		Database *struct {
			Healthy *bool `json:"healthy"`
		} `json:"database"`
	}
	if json.Unmarshal(answer.Facts, &facts) == nil {
		if (facts.Provider != nil && facts.Provider.Reachable != nil && !*facts.Provider.Reachable) ||
			(facts.Database != nil && facts.Database.Healthy != nil && !*facts.Database.Healthy) {
			return InstanceWarning
		}
	}
	return InstanceHealthy
}

// handleInstanceStatus serves the batch read beside /instances/probe.
func (s *Server) handleInstanceStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	targets := dedupeInstanceTargets(r.URL.Query()["urls"])
	if len(targets) > core.MaxSavedInstances {
		http.Error(w, fmt.Sprintf("%d addresses exceed the %d-row cap", len(targets), core.MaxSavedInstances),
			http.StatusBadRequest)
		return
	}
	rows := probeInstanceBatch(targets, probeInstanceAddress, instanceStatusWorkers)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(struct {
		Rows []instanceStatusRow `json:"rows"`
	}{Rows: rows})
}

// dedupeInstanceTargets keeps the first ask for every address, in ask order: the same
// endpoint listed twice costs one probe and answers one row.
func dedupeInstanceTargets(asked []string) []string {
	seen := make(map[string]bool, len(asked))
	out := make([]string, 0, len(asked))
	for _, raw := range asked {
		target := strings.TrimSpace(raw)
		if seen[target] {
			continue
		}
		seen[target] = true
		out = append(out, target)
	}
	return out
}

// probeInstanceBatch answers one row per address, in the order asked. probe is a parameter
// so the concurrency bound is testable without a network.
func probeInstanceBatch(targets []string, probe func(string) InstanceProbeAnswer, workers int) []instanceStatusRow {
	rows := make([]instanceStatusRow, len(targets))
	if len(rows) == 0 {
		return rows
	}
	if workers < 1 {
		workers = 1
	}
	gate := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, target := range targets {
		wg.Add(1)
		go func(i int, target string) {
			defer wg.Done()
			gate <- struct{}{}
			defer func() { <-gate }()
			answer := probe(target)
			rows[i] = instanceStatusRow{
				URL:     answer.URL,
				State:   InstanceState(answer, false),
				Failure: answer.Failure,
				Reason:  answer.Reason,
				Facts:   answer.Facts,
			}
			if rows[i].URL == "" {
				rows[i].URL = target
			}
		}(i, target)
	}
	wg.Wait()
	return rows
}
