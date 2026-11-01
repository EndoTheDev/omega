package approval

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// The verdict cache (design Q8): keyed by (canonical command,
// resolved-target content hash, policy hash). A repeat of the same
// command - same static form, same file contents on every referenced
// target, same policy text - replays the verdict with ZERO model
// calls. A mutated script re-asks (the TOCTOU bind); a policy edit
// invalidates the whole cache (every prior key's policy component
// changes, so every prior key misses).

// Cache is gate-scoped, process-resident, concurrency-safe. Entries
// are final resolutions, including a recorded replay source.
type Cache struct {
	mu sync.RWMutex
	m  map[string]record
}

type record struct {
	outcome Outcome
	// answers carries the six answers the deciding pass produced, so
	// the audit row of the replay shows the same shape as the original.
	answers Answers
	// stamp is the decision's transcript record (card t_1b3e2a8b):
	// the replay carries the recorded decision's own line, so a
	// card-approved resolution replays as "escalated -> approved",
	// never as an auto verdict.
	stamp     string
	modelUsed string
	cached    bool
}

func NewCache() *Cache {
	return &Cache{m: make(map[string]record)}
}

// policyHash is the cache + audit fingerprint of the operator policy.
func policyHash(policy string) string {
	sum := sha256.Sum256([]byte(policy))
	return hex.EncodeToString(sum[:])[:16]
}

// Key builds a cache key from the reviewed state.
func (c *Cache) Key(canonical, targetHash, policy string) string {
	sum := sha256.Sum256([]byte(canonical + "\x00" + targetHash + "\x00" + policyHash(policy)))
	return hex.EncodeToString(sum[:])
}

// Lookup returns the cached resolution, if any.
func (c *Cache) Lookup(key string) (record, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.m[key]
	return r, ok
}

// Store records one resolution.
func (c *Cache) Store(key string, r record) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = r
}

// Len reports the entry count (tests, and the debug log line).
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.m)
}

// contentLimits bounds the hashing pass: a few readable targets per
// command, modest sizes. The pass never opens device nodes, sockets
// or directories, and never reads the databases or logs it would be
// recording into.
const (
	targetCapCount = 16
	targetCapBytes = 1 << 20
)

// TargetContentHash hashes the content of the FILES the parsed
// command names: script arguments (executed content is the TOCTOU
// substance) and any other named path that already exists as a
// readable regular file. Missing targets hash as "absent" (the
// canonical form already carries their name; a future create is the
// cache's next subject, not this key's).
func TargetContentHash(segments []Segment) string {
	var paths []string
	seen := map[string]bool{}
	for _, seg := range segments {
		for _, w := range seg.Words {
			if isTargetPath(w) && !seen[w] {
				seen[w] = true
				paths = append(paths, w)
			}
		}
	}
	if len(paths) == 0 {
		return ""
	}
	sort.Strings(paths)
	if len(paths) > targetCapCount {
		paths = paths[:targetCapCount]
	}
	hash := sha256.New()
	for _, p := range paths {
		if p == os.DevNull || p == "-" {
			continue
		}
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			absent := "absent:" + p
			fmt.Fprintf(hash, "%d:%s", len(absent), absent)
			continue
		}
		if info.Size() > targetCapBytes {
			fmt.Fprintf(hash, "%d:%s", len("oversized:"+p), "oversized:"+p)
			continue
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			absent := "absent:" + p
			fmt.Fprintf(hash, "%d:%s", len(absent), absent)
			continue
		}
		sum := sha256.Sum256(raw)
		fmt.Fprintf(hash, "%d:%s:%s", len(p), p, hex.EncodeToString(sum[:]))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// isTargetPath picks the words that could name a file: pathy tokens
// only (contain a slash, a dot-extension, or start with ~). Values
// like -m or https://host/path stay out (URLs are not local reads).
func isTargetPath(w string) bool {
	if w == "" || strings.Contains(w, "://") {
		return false
	}
	if strings.HasPrefix(w, "-") {
		return false
	}
	if strings.HasPrefix(w, "/") || strings.HasPrefix(w, "~/") {
		return true
	}
	if strings.Contains(w, "/") {
		return true // relative path
	}
	if strings.Contains(filepath.Clean(w), ".") && strings.ContainsAny(w, ".") {
		// a bare name with an extension is a plausible file target
		return true
	}
	return false
}
