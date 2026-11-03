package agent

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"
)

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

// WithToolless marks the run as deliberately toolless: the loop replaces
// the fully merged tool registry (the caller's map plus every tool
// provider's contribution) with an empty map, so the model sees no
// schemas and no dispatch entry exists - a stray tool call is an error
// result, never an execution. For metadata turns: the session-title call
// is generated from user content that can invite tool use, and nothing
// it does outlives the title, so nothing may execute - present or
// future. Explicit beats a deny list: a new side-effectful module cannot
// resurrect an execution path for a toolless run by registering under a
// fresh name the way WithDeniedTools would allow.
func WithToolless(ctx context.Context) context.Context {
	return context.WithValue(ctx, toollessKey{}, true)
}

// Toolless reports whether the run is explicitly toolless.
func Toolless(ctx context.Context) bool {
	v, _ := ctx.Value(toollessKey{}).(bool)
	return v
}

type deniedToolsKey struct{}

type toollessKey struct{}

type sessionIDKey struct{}

// runAffinityKey carries the unique per-run affinity key the injected
// subagent results are addressed by (t_f3fb7c52). Unlike the session id
// - which a session's title turn and its chat runs SHARE - the affinity
// key is one value per Run: the loop generates a fresh one when the
// host did not pin one, so results queued for this run can never drain
// into a sibling run on the same session.
type runAffinityKey struct{}

// RunAffinityFromContext returns the run's affinity key, or "" when the
// run carries none (a loop generates its own in that case).
func RunAffinityFromContext(ctx context.Context) string {
	key, _ := ctx.Value(runAffinityKey{}).(string)
	return key
}

// WithRunAffinity returns a child context carrying the run's affinity
// key. Empty keys are dropped - the loop generates one for such runs.
func WithRunAffinity(ctx context.Context, key string) context.Context {
	if key == "" {
		return ctx
	}
	return context.WithValue(ctx, runAffinityKey{}, key)
}

// NewRunAffinity mints a fresh run affinity key (a random nonce).
func NewRunAffinity() string {
	var b [8]byte
	if _, err := crand.Read(b[:]); err != nil {
		// crypto/rand never fails on the platforms omega serves; the
		// fallback keeps the key unique enough for the drain contract.
		fmt.Fprintf(os.Stderr, "agent: run affinity entropy: %v - falling back to time\n", err)
		return fmt.Sprintf("run-%d", time.Now().UnixNano())
	}
	return "run-" + hex.EncodeToString(b[:])
}
