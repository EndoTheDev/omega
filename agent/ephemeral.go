package agent

// EphemeralSessionID reads: the ephemeral (no-persist) session id
// namespace. The `eph-` ids live in memory only (the /chat ephemeral
// lane, agent/api/server_ephemeral.go); a durable session id may never
// fall inside the namespace. The predicate is here, in the agent
// package, because every writer of session ids enforces it - the id
// creators in the serving shell and the roster-management capsule
// (the shell-ring shrink wave 2, ADR Workshop/omega-shell-ring-shrink.md)
// both guard their create flows with it.

// EphemeralIDPrefix is the reserved id namespace of no-persist sessions.
const EphemeralIDPrefix = "eph-"

// IsEphemeralID reports whether an id belongs to the ephemeral
// namespace (the "eph-" prefix).
func IsEphemeralID(id string) bool {
	return len(id) > len(EphemeralIDPrefix) && id[:len(EphemeralIDPrefix)] == EphemeralIDPrefix
}