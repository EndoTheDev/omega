package agent

// EphemeralIDPrefix is the reserved id namespace of no-persist sessions: `eph-` ids
// live in memory only, so a durable session id may never fall inside it.
const EphemeralIDPrefix = "eph-"

func IsEphemeralID(id string) bool {
	return len(id) > len(EphemeralIDPrefix) && id[:len(EphemeralIDPrefix)] == EphemeralIDPrefix
}