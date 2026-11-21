package api

// server_agentrun.go: per-agent run config.

import (
	"context"
	"fmt"

	"github.com/EndoTheDev/omega/agent"
)

// AgentRunFactory builds the agent that serves one agent home's sessions. A config the
// layer cannot parse is a hard error: a run on half of the agent's config is worse than
// a loud stop.
type AgentRunFactory func(ctx context.Context, agentName string) (*agent.Agent, error)

// SetAgentRunFactory wires the host's per-agent run factory (nil = the daemon's own
// agent serves every session, today's behavior for every caller that never sets it).
func (s *Server) SetAgentRunFactory(f AgentRunFactory) {
	s.agentRuns = f
}

// runAgentFor resolves the agent that serves this run: nil unless the store names an
// agent home. A nil answer means "the daemon's agent", the single home every session
// lived in before per-agent routing.
func (s *Server) runAgentFor(ctx context.Context, sessionID string) (*agent.Agent, error) {
	if s.agentRuns == nil || s.store == nil || sessionID == "" || isEphemeralID(sessionID) {
		return nil, nil
	}
	owners, ok := s.store.(agent.SessionOwnerStore)
	if !ok {
		return nil, nil
	}
	name := owners.SessionOwner(sessionID)
	if name == "" || name == agent.DefaultAgentName {
		return nil, nil
	}
	ag, err := s.agentRuns(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("agent home %q: %w", name, err)
	}
	return ag, nil
}

// runAgent is the agent a prepared run serves through.
func (s *Server) runAgent(req *chatRequest) *agent.Agent {
	if req.runAgent != nil {
		return req.runAgent
	}
	return s.agent
}

// baseProvider is the provider the run pins against - the owning agent
// home's provider, so the picker's model/instance choices resolve in
// that home's config, never.
func (s *Server) baseProvider(req *chatRequest) agent.Provider {
	if ag := s.runAgent(req); ag != nil {
		return ag.Provider()
	}
	return nil
}
