package agent

import "context"

func SessionIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(sessionIDKey{}).(string)
	return id
}

// WithSessionID returns a child context carrying the session id for this run. Values are
// run-scoped: each RunWithProvider caller wraps its own context, so parallel runs never share.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionIDKey{}, sessionID)
}

// WithDeniedTools names the tools the current run must not execute (ephemeral sessions: the
// memory tool never runs - nothing it writes would outlive the session's in-memory contract).
func WithDeniedTools(ctx context.Context, names ...string) context.Context {
	if len(names) == 0 {
		return ctx
	}
	// copy: callers may reuse the slice between runs
	return context.WithValue(ctx, deniedToolsKey{}, append([]string(nil), names...))
}

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

// WithToolless marks the run as deliberately toolless: the fully merged registry (the
// caller's map plus every tool provider's contribution) is replaced with an empty map, so the
// model sees no schemas and no dispatch entry exists - a stray tool call is an error result,
// never an execution. Explicit beats a deny list: a new side-effectful module cannot
// resurrect an execution path for a toolless run by registering a fresh name.
func WithToolless(ctx context.Context) context.Context {
	return context.WithValue(ctx, toollessKey{}, true)
}

func Toolless(ctx context.Context) bool {
	v, _ := ctx.Value(toollessKey{}).(bool)
	return v
}

type deniedToolsKey struct{}

type toollessKey struct{}

type sessionIDKey struct{}
