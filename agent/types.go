package agent

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// NewSessionID generates a random 32-character hex session ID.
// Used by the store, TUI, and HTTP channel to avoid duplicating
// the crypto/rand boilerplate.
func NewSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// SessionUnread reports whether a session has output newer than its last
// read stamp: updated_at after last_read_at. Computed at read time - no
// counters, no schema writes. Empty stamps read as seen, and the session
// being compared must have been persisted with RFC 3339 times.
func SessionUnread(s core.Session) bool {
	if s.LastReadAt == "" {
		return false
	}
	updated, err := time.Parse(time.RFC3339, s.UpdatedAt)
	if err != nil {
		return s.UpdatedAt > s.LastReadAt
	}
	read, err := time.Parse(time.RFC3339, s.LastReadAt)
	if err != nil {
		return s.UpdatedAt > s.LastReadAt
	}
	return updated.After(read)
}