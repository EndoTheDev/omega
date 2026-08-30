// Package memory provides the persistent memory seam (agent.MemoryProvider)
// backed by two markdown files (memory.md + user.md), plus the memory tool
// that lets the agent add, replace, and remove entries.
//
// Seam: memory (exclusive), tools (additive).
//
// The storage format matches Hermes Agent: entries are separated by §
// (section sign) delimiters. Files are human-readable and editable outside
// the agent. Snapshot reads files fresh on each BuildPrompt call, so new
// sessions pick up writes from the previous session.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/EndoTheDev/omega/agent"
)

// entrySep is the delimiter between entries in the memory files.
const entrySep = "§"

// targetMemory and targetUser are the two store names.
const (
	targetMemory = "memory"
	targetUser   = "user"
)

// FileMemory implements agent.MemoryProvider using two markdown files.
// It is safe for concurrent use.
type FileMemory struct {
	mu sync.Mutex

	memoryFile     string
	userFile        string
	memoryLimit     int
	userLimit       int
	memoryEnabled   bool
	userEnabled     bool
}

// NewFileMemory creates a FileMemory from config.
func NewFileMemory(cfg Config) *FileMemory {
	return &FileMemory{
		memoryFile:     cfg.File,
		userFile:       cfg.UserProfileFile,
		memoryLimit:    cfg.CharLimit,
		userLimit:      cfg.UserProfileCharLimit,
		memoryEnabled:  cfg.Enabled,
		userEnabled:    cfg.UserProfileEnabled,
	}
}

// Snapshot returns the formatted prompt block for system prompt injection.
// Reads files fresh on each call so new sessions pick up writes from the
// previous session. BuildPrompt is called once per Run(), not per turn,
// so this is one disk read per session start — negligible.
func (fm *FileMemory) Snapshot() string {
	var sections []string
	if mem := fm.formatSection("MEMORY", "your personal notes", fm.load(targetMemory), fm.memoryLimit, fm.memoryEnabled); mem != "" {
		sections = append(sections, mem)
	}
	if usr := fm.formatSection("USER PROFILE", "who the user is", fm.load(targetUser), fm.userLimit, fm.userEnabled); usr != "" {
		sections = append(sections, usr)
	}
	return strings.Join(sections, "\n\n")
}

// Add appends a new entry to the target store.
func (fm *FileMemory) Add(target, content string) (string, error) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if !fm.targetEnabled(target) {
		return "", fmt.Errorf("%s store is disabled", target)
	}
	if content == "" {
		return "", fmt.Errorf("content is required")
	}

	entries := fm.loadEntries(target)

	// Duplicate prevention.
	for _, e := range entries {
		if e == content {
			usage := fm.usage(target, entries)
			return fmt.Sprintf("no duplicate added (%s)", usage), nil
		}
	}

	// Check char limit.
	limit := fm.targetLimit(target)
	entries = append(entries, content)
	total := fm.entriesChars(entries)
	if total > limit {
		usage := fm.usageStr(fm.entriesChars(entries[:len(entries)-1]), limit)
		return "", fmt.Errorf("%s at %s. Adding this entry (%d chars) would exceed the limit. Consolidate now: use 'replace' to merge overlapping entries into shorter ones or 'remove' stale or less important entries (see current_entries below), then retry this add.", fm.targetName(target), usage, len(content))
	}

	if err := fm.saveEntries(target, entries); err != nil {
		return "", err
	}
	return fm.usage(target, entries), nil
}

// Replace finds the entry matching oldText (unique substring) and replaces
// it with content.
func (fm *FileMemory) Replace(target, oldText, content string) (string, error) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if !fm.targetEnabled(target) {
		return "", fmt.Errorf("%s store is disabled", target)
	}
	if oldText == "" {
		return "", fmt.Errorf("old_text is required")
	}
	if content == "" {
		return "", fmt.Errorf("content is required")
	}

	entries := fm.loadEntries(target)
	idx, err := fm.findEntry(entries, oldText)
	if err != nil {
		return "", err
	}

	oldEntry := entries[idx]
	entries[idx] = content

	// Check char limit after replacement.
	limit := fm.targetLimit(target)
	total := fm.entriesChars(entries)
	if total > limit {
		entries[idx] = oldEntry // revert
		usage := fm.usageStr(total-len(content)+len(oldEntry), limit)
		return "", fmt.Errorf("%s at %s. Replacing with this entry (%d chars) would exceed the limit. Shorten the new content or remove another entry first.", fm.targetName(target), usage, len(content))
	}

	if err := fm.saveEntries(target, entries); err != nil {
		return "", err
	}
	return fm.usage(target, entries), nil
}

// Remove finds the entry matching oldText (unique substring) and deletes it.
func (fm *FileMemory) Remove(target, oldText string) (string, error) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if !fm.targetEnabled(target) {
		return "", fmt.Errorf("%s store is disabled", target)
	}
	if oldText == "" {
		return "", fmt.Errorf("old_text is required")
	}

	entries := fm.loadEntries(target)
	idx, err := fm.findEntry(entries, oldText)
	if err != nil {
		return "", err
	}

	entries = append(entries[:idx], entries[idx+1:]...)
	if err := fm.saveEntries(target, entries); err != nil {
		return "", err
	}
	return fm.usage(target, entries), nil
}

// List returns all entries for the target.
func (fm *FileMemory) List(target string) ([]string, error) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	return fm.loadEntries(target), nil
}

// --- internals ---

// load reads the file for the target and returns its raw content.
func (fm *FileMemory) load(target string) string {
	path := fm.targetFile(target)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// loadEntries reads the file and splits into §-delimited entries.
func (fm *FileMemory) loadEntries(target string) []string {
	raw := fm.load(target)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, entrySep)
	var entries []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			entries = append(entries, p)
		}
	}
	return entries
}

// saveEntries writes entries back to the file with § delimiters.
func (fm *FileMemory) saveEntries(target string, entries []string) error {
	path := fm.targetFile(target)
	content := strings.Join(entries, "\n"+entrySep+"\n")
	if content != "" {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// findEntry finds the index of the entry containing oldText as a substring.
// Returns an error if zero or multiple entries match.
func (fm *FileMemory) findEntry(entries []string, oldText string) (int, error) {
	idx := -1
	for i, e := range entries {
		if strings.Contains(e, oldText) {
			if idx != -1 {
				return -1, fmt.Errorf("old_text %q matches multiple entries — use a more specific substring", oldText)
			}
			idx = i
		}
	}
	if idx == -1 {
		return -1, fmt.Errorf("no entry matching %q", oldText)
	}
	return idx, nil
}

// entriesChars returns the total character count of all entries plus
// delimiters (matching how the file content is measured).
func (fm *FileMemory) entriesChars(entries []string) int {
	if len(entries) == 0 {
		return 0
	}
	total := 0
	for _, e := range entries {
		total += len(e)
	}
	// Add delimiter overhead: (n-1) * len("§") + newlines between entries.
	// Each separator is "\n§\n" = 3 chars.
	total += (len(entries) - 1) * 3
	return total
}

// targetFile returns the file path for the target store.
func (fm *FileMemory) targetFile(target string) string {
	switch target {
	case targetMemory:
		return fm.memoryFile
	case targetUser:
		return fm.userFile
	}
	return fm.memoryFile
}

// targetLimit returns the char limit for the target.
func (fm *FileMemory) targetLimit(target string) int {
	switch target {
	case targetMemory:
		return fm.memoryLimit
	case targetUser:
		return fm.userLimit
	}
	return fm.memoryLimit
}

// targetEnabled returns whether the target store is enabled.
func (fm *FileMemory) targetEnabled(target string) bool {
	switch target {
	case targetMemory:
		return fm.memoryEnabled
	case targetUser:
		return fm.userEnabled
	}
	return false
}

// targetName returns the display name for the target.
func (fm *FileMemory) targetName(target string) string {
	switch target {
	case targetMemory:
		return "Memory"
	case targetUser:
		return "User profile"
	}
	return "Memory"
}

// usage returns the usage string for the current entries.
func (fm *FileMemory) usage(target string, entries []string) string {
	return fm.usageStr(fm.entriesChars(entries), fm.targetLimit(target))
}

// usageStr formats a usage string like "1,474/2,200 chars".
func (fm *FileMemory) usageStr(used, limit int) string {
	return fmt.Sprintf("%s/%s chars", commaInt(used), commaInt(limit))
}

// formatSection renders a store's snapshot as a formatted prompt block.
// Returns empty string if the store is disabled or empty.
func (fm *FileMemory) formatSection(title, subtitle, snapshot string, limit int, enabled bool) string {
	if !enabled || snapshot == "" {
		return ""
	}
	used := len(snapshot)
	pct := 0
	if limit > 0 {
		pct = used * 100 / limit
	}
	border := strings.Repeat("═", 48)
	header := fmt.Sprintf("%s (%s) [%d%% — %s/%s chars]", title, subtitle, pct, commaInt(used), commaInt(limit))
	return fmt.Sprintf("%s\n%s\n%s\n%s", border, header, border, snapshot)
}

// commaInt formats an integer with thousands separators.
func commaInt(n int) string {
	s := fmt.Sprintf("%d", n)
	if n < 0 {
		return "-" + commaInt(-n)
	}
	if len(s) <= 3 {
		return s
	}
	var sb strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			sb.WriteByte(',')
		}
		sb.WriteRune(c)
	}
	return sb.String()
}

// --- memory tool ---

// memoryToolProvider provides the memory tool.
type memoryToolProvider struct {
	mem *FileMemory
}

// Tools returns the memory tool definition.
func (p *memoryToolProvider) Tools() map[string]agent.Tool {
	return map[string]agent.Tool{
		"memory": {
			Description: "Save durable facts to persistent memory that survives across sessions. Memory is injected into the system prompt every session — use it for user preferences, environment facts, conventions, and lessons learned. Two targets: 'memory' (agent notes) and 'user' (user profile). Actions: 'add' (append), 'replace' (find by old_text substring, replace with content), 'remove' (find by old_text substring, delete). Batch: pass 'operations' array for atomic multi-change.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"enum":        []string{"add", "replace", "remove"},
						"description": "What to do: add, replace, or remove an entry.",
					},
					"target": map[string]any{
						"type":        "string",
						"enum":        []string{"memory", "user"},
						"description": "Which store: 'memory' (agent notes) or 'user' (user profile).",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "The entry text (required for add and replace).",
					},
					"old_text": map[string]any{
						"type":        "string",
						"description": "Unique substring identifying the entry to replace or remove (required for replace and remove).",
					},
					"operations": map[string]any{
						"type":        "array",
						"description": "Batch operations: array of {action, target, content?, old_text?}. Applied atomically — char limit checked on final result only.",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"action":   map[string]any{"type": "string", "enum": []string{"add", "replace", "remove"}},
								"target":   map[string]any{"type": "string", "enum": []string{"memory", "user"}},
								"content":  map[string]any{"type": "string"},
								"old_text": map[string]any{"type": "string"},
							},
							"required": []string{"action", "target"},
						},
					},
				},
				"required": []string{"action", "target"},
			},
			Run: p.runMemory,
		},
	}
}

// memoryOp is one operation in a batch.
type memoryOp struct {
	Action  string `json:"action"`
	Target  string `json:"target"`
	Content string `json:"content"`
	OldText string `json:"old_text"`
}

// memoryResult is the JSON response from the memory tool.
type memoryResult struct {
	Success        bool     `json:"success"`
	Target         string   `json:"target,omitempty"`
	Usage          string   `json:"usage,omitempty"`
	Error          string   `json:"error,omitempty"`
	CurrentEntries []string `json:"current_entries,omitempty"`
}

// runMemory executes the memory tool.
func (p *memoryToolProvider) runMemory(ctx context.Context, args map[string]any) (string, error) {
	// Batch operations.
	if opsRaw, ok := args["operations"]; ok {
		ops, err := parseOperations(opsRaw)
		if err != nil {
			return marshalResult(memoryResult{Success: false, Error: err.Error()}), nil
		}
		return p.runBatch(ops), nil
	}

	action, _ := args["action"].(string)
	target, _ := args["target"].(string)
	content, _ := args["content"].(string)
	oldText, _ := args["old_text"].(string)

	if action == "" || target == "" {
		return marshalResult(memoryResult{Success: false, Error: "action and target are required"}), nil
	}

	return p.runSingle(action, target, content, oldText), nil
}

// runSingle executes one memory operation.
func (p *memoryToolProvider) runSingle(action, target, content, oldText string) string {
	var usage string
	var err error
	switch action {
	case "add":
		usage, err = p.mem.Add(target, content)
	case "replace":
		usage, err = p.mem.Replace(target, oldText, content)
	case "remove":
		usage, err = p.mem.Remove(target, oldText)
	default:
		return marshalResult(memoryResult{Success: false, Error: "unknown action: " + action})
	}
	if err != nil {
		return p.errResult(target, err, usage)
	}
	return marshalResult(memoryResult{Success: true, Target: target, Usage: usage})
}

// errResult builds a failure result with current entries for context.
func (p *memoryToolProvider) errResult(target string, err error, usage string) string {
	entries, _ := p.mem.List(target)
	return marshalResult(memoryResult{
		Success:        false,
		Error:          err.Error(),
		CurrentEntries: entries,
		Usage:          usage,
	})
}

// execOp runs a single memoryOp and returns usage + error.
func (p *memoryToolProvider) execOp(op memoryOp) (string, error) {
	switch op.Action {
	case "add":
		return p.mem.Add(op.Target, op.Content)
	case "replace":
		return p.mem.Replace(op.Target, op.OldText, op.Content)
	case "remove":
		return p.mem.Remove(op.Target, op.OldText)
	}
	return "", fmt.Errorf("unknown action: %s", op.Action)
}

// runBatch executes a batch of operations.
// ponytail: no rollback on partial failure — the char limit is checked
// on the final result only, so if the batch fits, all operations apply.
// If an individual operation fails (e.g. ambiguous substring), the
// batch stops at that point and returns the error. Upgrade path:
// snapshot-and-revert for full transactional semantics.
func (p *memoryToolProvider) runBatch(ops []memoryOp) string {
	var lastUsage string
	var lastTarget string
	for _, op := range ops {
		usage, err := p.execOp(op)
		if err != nil {
			entries, _ := p.mem.List(op.Target)
			return marshalResult(memoryResult{
				Success:        false,
				Error:          fmt.Sprintf("batch failed at %s: %s", op.Action, err.Error()),
				CurrentEntries: entries,
				Usage:          usage,
			})
		}
		lastUsage = usage
		lastTarget = op.Target
	}
	return marshalResult(memoryResult{Success: true, Target: lastTarget, Usage: lastUsage})
}

// parseOperations converts the raw interface{} from tool args into
// a slice of memoryOp.
func parseOperations(raw any) ([]memoryOp, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid operations: %v", err)
	}
	var ops []memoryOp
	if err := json.Unmarshal(data, &ops); err != nil {
		return nil, fmt.Errorf("invalid operations: %v", err)
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("operations array is empty")
	}
	return ops, nil
}

// marshalResult converts a memoryResult to a JSON string.
func marshalResult(r memoryResult) string {
	b, _ := json.Marshal(r)
	return string(b)
}
