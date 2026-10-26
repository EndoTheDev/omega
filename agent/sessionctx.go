package agent

import "context"

// SessionIDFromContext returns the id of the session the current agent
// run belongs to, or "" when the run has no session (one-shot runs,
// tests). The host wraps the run context with WithSessionID so tools
// can attribute board state to the conversation they ran in.
func SessionIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(sessionIDKey{}).(string)
	return id
}

// WithSessionID returns a child context carrying the session id for
// the current run. Values are run-scoped: each RunWithProvider caller
// wraps its own context, so parallel runs never share one.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionIDKey{}, sessionID)
}

// WithDeniedTools returns a child context listing tool names the current
// run must not execute (ephemeral sessions: the memory tool never runs -
// nothing it writes would outlive the session's own in-memory contract).
// The loop applies the deny AFTER every provider merge, so a denied tool
// is absent from both the model's schema list and the dispatch map no
// matter which layer registered it.
func WithDeniedTools(ctx context.Context, names ...string) context.Context {
	if len(names) == 0 {
		return ctx
	}
	// copy: callers may reuse the slice between runs
	return context.WithValue(ctx, deniedToolsKey{}, append([]string(nil), names...))
}

// DeniedTools returns the run's tool-name deny set, or nil.
func DeniedTools(ctx context.Context) map[string]bool {
	names, _ := ctx.Value(deniedToolsKey{}).([]string)
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}

type deniedToolsKey struct{}

type sessionIDKey struct{}
