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

// Cache is gate-scoped, process-resident, concurrency-safe.
type Cache struct {
	mu sync.RWMutex
	m  map[string]record
}

type record struct {
	outcome Outcome
	answers Answers
	// stamp is the decision's transcript record: the replay carries the recorded
	// decision's own line, so a card-approved resolution replays as "escalated ->
	// approved", never as an auto verdict.
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

func (c *Cache) Key(canonical, targetHash, policy string) string {
	sum := sha256.Sum256([]byte(canonical + "\x00" + targetHash + "\x00" + policyHash(policy)))
	return hex.EncodeToString(sum[:])
}

func (c *Cache) Lookup(key string) (record, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.m[key]
	return r, ok
}

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

// contentLimits bounds the hashing pass. The pass never opens device nodes, sockets or
// directories, and never reads the databases or logs it would be recording.
const (
	targetCapCount = 16
	targetCapBytes = 1 << 20
)

// TargetContentHash hashes the content of the FILES the parsed command names.
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
		return true
	}
	return false
}
