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

type sessionIDKey struct{}