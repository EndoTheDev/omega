package api

// The health popover's sourced facts (card t_5e2f3095). The popover's
// server / uptime rows always had a wire source; these three did not:
//
//	provider    the model endpoint's reachability - a cached probe
//	database    the store seam's liveness ping - inline, bounded
//	last error  the newest error the process logger recorded
//
// Every one of them is OPTIONAL on the wire: the field is absent when
// there is no source (no probe yet, no store mounted, an older build, a
// logger without the upgrade), and the webui renders "not reported"
// rather than inventing a value. The /health body and the pushed health
// topic share ONE body (api/ws.go healthState), so a reconnect fetch and
// the 5s frames can never disagree.
//
// The provider probe deliberately does NOT run on the heartbeat: a
// network round trip every 5s would both hammer the endpoint and hold
// the push ticker. It runs on its own slow ladder and caches its verdict
// with the time it was taken, so the row can say how fresh the verdict
// is.

import (
	"context"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/agent"
	agentapi "github.com/EndoTheDev/omega/agent/api"
)

// healthProbeInterval is the provider reachability ladder. One probe a
// minute is a reachability fact, not a load probe: a provider that comes
// back up is reported reachable within a minute, and an idle shell costs
// one model-list round trip a minute.
const healthProbeInterval = 60 * time.Second

// storeProbeBudget bounds the store ping. A health read is one frame on
// a 5s heartbeat and one reconnect fetch - a wedged database must not
// hold either, so a ping that cannot answer inside this budget is
// reported unhealthy with the timeout as its detail.
const storeProbeBudget = 2 * time.Second

// lastErrorCap caps the last-error message on the wire (runes). The
// logger writes whole messages - a wrapped SQL error or a provider's
// error body can be a wall of text - and the popover row is one line.
// The tail (GET /logs) stays the full record.
const lastErrorCap = 240

// monitorHealth runs the provider probe on its ladder until the server
// context ends. Started by Serve after the listener is up - the
// goroutine fence (mount never starts background work; the host does).
func (s *Server) monitorHealth(ctx context.Context) {
	s.probeProvider(ctx)
	ticker := time.NewTicker(healthProbeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.probeProvider(ctx)
		}
	}
}

// probeProvider asks the provider for its model list and caches the
// verdict with its timestamp. The model LIST is the honest reachability
// read: it is the same call the webui's model picker makes on every page
// load, and it fails only when the endpoint is unreachable or refuses
// the credentials. ModelInfo would be the wrong probe - it 404s on every
// non-ollama host (openai/anthropic/keypool-fronted lanes do not serve
// /api/show), which would paint a perfectly healthy provider "not
// reachable".
//
// The provider seam carries no context, so the call is bounded by the
// provider's own HTTP client timeout; the previous verdict keeps riding
// the health frames while a slow probe is in flight.
func (s *Server) probeProvider(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	ag := s.Agent()
	if ag == nil {
		return
	}
	provider := ag.Provider()
	if provider == nil {
		return
	}
	fact := &agentapi.HealthProvider{CheckedAt: time.Now().Unix()}
	if _, err := provider.ListModels(); err != nil {
		fact.Detail = clampOneLine(err.Error(), lastErrorCap)
	} else {
		fact.Reachable = true
	}
	s.healthMu.Lock()
	s.healthProvider = fact
	s.healthMu.Unlock()
}

// providerHealth returns a copy of the cached verdict, or nil before the
// first probe has landed (the documented not-reported floor).
func (s *Server) providerHealth() *agentapi.HealthProvider {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	if s.healthProvider == nil {
		return nil
	}
	fact := *s.healthProvider
	return &fact
}

// databaseHealth pings the store seam inline - a liveness read against a
// local pool is cheap enough that a cached verdict would only buy
// staleness. Nil when no store module is mounted, or when the mounted
// store does not carry the optional agent.StorePinger upgrade (a test
// double, an alternative capsule): the body then carries no database
// fact.
func (s *Server) databaseHealth() *agentapi.HealthDatabase {
	store := s.Store()
	if store == nil {
		return nil
	}
	pinger, ok := store.(agent.StorePinger)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeProbeBudget)
	defer cancel()
	if err := pinger.Ping(ctx); err != nil {
		return &agentapi.HealthDatabase{Detail: clampOneLine(err.Error(), lastErrorCap)}
	}
	return &agentapi.HealthDatabase{Healthy: true}
}

// lastErrorFact reads the logger seam's newest error through the
// optional agent.LoggerLastError upgrade. Nil when no logger is mounted
// or it does not carry the upgrade; a sourced fact with an EMPTY message
// when the logger has recorded no error since it opened (the popover
// renders that as "none", not as a gap).
func (s *Server) lastErrorFact() *agentapi.HealthError {
	logger := s.Logger()
	if logger == nil {
		return nil
	}
	records, ok := logger.(agent.LoggerLastError)
	if !ok {
		return nil
	}
	message, at, has := records.LastError()
	if !has {
		return &agentapi.HealthError{}
	}
	return &agentapi.HealthError{Message: clampOneLine(message, lastErrorCap), At: at.Unix()}
}

// clampOneLine flattens a message onto one line and caps it at n runes.
// Whitespace runs (newlines, tabs, indentation from a wrapped error)
// collapse to single spaces: the popover row is one line, and the
// untruncated record stays in the log file.
func clampOneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
