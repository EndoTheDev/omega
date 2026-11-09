package delegate

// law_test.go: the defaulting law (Endo, 2026-11-01) plus the gap-close
// pins for THE delegation ruling - the mount-side recursion guard, the
// stored+searchable child session, and the summary-back surfaces.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
)

// TestDefaultingLawAgent pins the law's agent half: empty (or the
// literal "default", or whitespace) resolves to the default agent -
// never an error, never a ghost layer.
func TestDefaultingLawAgent(t *testing.T) {
	cases := map[string]string{
		"":         agent.DefaultAgentName,
		"default":  agent.DefaultAgentName,
		"  scout ": "scout",
		"scout":    "scout",
	}
	for in, want := range cases {
		if got := resolveAgent(in); got != want {
			t.Errorf("resolveAgent(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestDefaultingLawModel pins the law's model half: empty = inherit
// (the child's own config IS the session provider model - the parent
// never re-guesses it), a named model passes through trimmed.
func TestDefaultingLawModel(t *testing.T) {
	if got := resolveModel(""); got != "" {
		t.Errorf("resolveModel(\"\") = %q, want \"\" (inherit)", got)
	}
	if got := resolveModel("  glm-5.2  "); got != "glm-5.2" {
		t.Errorf("resolveModel trimmed = %q, want glm-5.2", got)
	}
}

// TestChildEnvDefaultingLaw pins the spawn deltas: the recursion guard
// always, OMEGA_AGENT only off-default, OMEGA_MODEL only explicit, and
// the caller's store path only when the host routed one (card
// t_93b4e521 - the pin is what keeps `run --session <id>` on the
// database the host pre-created the child session in).
func TestChildEnvDefaultingLaw(t *testing.T) {
	onlyGuard := []string{"OMEGA_SUBAGENT=1"}
	if got := childEnv("", "", ""); got[0] != "OMEGA_SUBAGENT=1" || len(got) != 1 {
		t.Errorf("childEnv(\"\",\"\",\"\") = %v, want exactly %v", got, onlyGuard)
	}
	deltas := strings.Join(childEnv("default", "", ""), ",")
	if strings.Contains(deltas, "OMEGA_AGENT") || strings.Contains(deltas, "OMEGA_MODEL") {
		t.Errorf("default agent + empty model leaked env deltas: %v", deltas)
	}
	deltas = strings.Join(childEnv("scout", " glm-5.2 ", ""), ",")
	if !strings.Contains(deltas, "OMEGA_AGENT=scout") || !strings.Contains(deltas, "OMEGA_MODEL=glm-5.2") {
		t.Errorf("childEnv(scout, glm-5.2) missing overrides: %v", deltas)
	}
	if strings.Contains(deltas, "OMEGA_DB_PATH") {
		t.Errorf("no store path given, yet OMEGA_DB_PATH appeared: %v", deltas)
	}
	deltas = strings.Join(childEnv("scout", "", "/home/e/.omega/data/omega.db"), ",")
	if !strings.Contains(deltas, "OMEGA_DB_PATH=/home/e/.omega/data/omega.db") {
		t.Errorf("childEnv lost the caller's store pin: %v", deltas)
	}
}

// TestWorkerFenceStamp pins the machine-fence spawn deltas (card
// t_8b32a09a): every delegate child is stamped OMEGA_WORKER=1 and
// OMEGA_WORKER_CARD=<task>; the PATH delta appears only when an
// executable shim sits beside the binary.
func TestWorkerFenceStamp(t *testing.T) {
	stamp := workerStamp("task-1", "")
	if stamp[0] != "OMEGA_WORKER=1" {
		t.Errorf("workerStamp[0] = %q, want OMEGA_WORKER=1", stamp[0])
	}
	if stamp[1] != "OMEGA_WORKER_CARD=task-1" {
		t.Errorf("workerStamp[1] = %q, want OMEGA_WORKER_CARD=task-1", stamp[1])
	}
	if len(stamp) != 2 {
		t.Errorf("workerStamp(no binary) must carry no PATH delta, got %v", stamp)
	}

	// Real shim beside the binary: exe dir + shims/git, executable.
	// Windows: the exec-bit gate cannot fire on NTFS (perms live in
	// ACLs), so the fence honestly degrades to the env-only posture -
	// a real windows fence is the parked windows-fence design item.
	if runtime.GOOS == "windows" {
		t.Skip("windows fence = design item; exec-bit gate cannot fire on NTFS; see the fence design card")
	}
	bindir := t.TempDir()
	shimsdir := filepath.Join(bindir, "shims")
	if err := os.MkdirAll(shimsdir, 0o755); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(shimsdir, "git")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	stamp = workerStamp("task-2", filepath.Join(bindir, "omega"))
	if len(stamp) != 3 {
		t.Errorf("workerStamp(beside shim) lost deltas: %v", stamp)
	} else if want := shimsdir + ":" + os.Getenv("PATH"); stamp[2] != "PATH="+want {
		t.Errorf("workerStamp PATH delta = %q, want PATH=<shims>:<inherited PATH>", stamp[2])
	}

	// A non-executable or missing shim yields no PATH delta.
	if err := os.Chmod(shim, 0o644); err != nil {
		t.Fatal(err)
	}
	if s := workerStamp("task-3", filepath.Join(bindir, "omega")); len(s) != 2 {
		t.Errorf("non-executable shim leaked a PATH delta: %v", s)
	}
	if s := workerStamp("task-4", filepath.Join(t.TempDir(), "elsewhere", "omega")); len(s) != 2 {
		t.Errorf("absent shims dir leaked a PATH delta: %v", s)
	}
}

// TestSpawnRefusedInsideSubagent pins the recursion guard's tool face:
// inside a subagent child (OMEGA_SUBAGENT set) delegate.task refuses at
// dispatch - depth stays at one level even if nothing else holds.
func TestSpawnRefusedInsideSubagent(t *testing.T) {
	t.Setenv("OMEGA_SUBAGENT", "1")
	d := NewDelegate()
	if d.spawnable {
		t.Fatal("delegate must be non-spawnable with OMEGA_SUBAGENT set")
	}
	out, err := d.runDelegateTask(context.Background(), map[string]any{"prompt": " recurse"})
	if err == nil {
		t.Fatalf("subagent spawned a child, output %q", out)
	}
	if !strings.Contains(err.Error(), "subagent") {
		t.Fatalf("refusal names the subagent origin, got %v", err)
	}
	d.mu.Lock()
	count := len(d.tasks)
	d.mu.Unlock()
	if count != 0 {
		t.Fatal("a refused spawn must not register a task")
	}
}

// TestSubagentChildMountsNoDelegateTools pins the guard's mount face:
// the module mounts in a child (the plugin census keeps counting it)
// but registers ZERO delegate tools there.
func TestSubagentChildMountsNoDelegateTools(t *testing.T) {
	t.Setenv("OMEGA_SUBAGENT", "1")
	ctx := &core.Context{}
	plugins := []agent.Plugin{loggingPlugin{}, NewPlugin()}
	if _, err := agent.MountAllAPI(plugins, ctx, nil, nil); err != nil {
		t.Fatalf("mount in subagent: %v", err)
	}
	for _, tp := range ctx.ToolProviders {
		for name := range tp.Tools() {
			if strings.HasPrefix(name, "delegate.") {
				t.Fatalf("subagent child registered %s - the recursion guard leaks", name)
			}
		}
	}
}

// stubStore is the minimal store seam for the session pins. Only the
// create paths are reachable - everything else rides the nil embedded
// interface (calling it panics, and nothing does). The two create
// surfaces are recorded apart so a test can prove which lineage kind a
// spawn used (t_6c4ab302: delegate lineage must never ride parent_id).
type stubStore struct {
	agent.StoreProvider
	created  []string // "id|delegate_parent|label" rows, in call order
	forked   []string // CreateSession rows - must stay empty for delegate
	failWith error
}

func (s *stubStore) CreateSession(ctx context.Context, id, parentID, label string) error {
	if s.failWith != nil {
		return s.failWith
	}
	s.forked = append(s.forked, id+"|"+parentID+"|"+label)
	return nil
}

func (s *stubStore) CreateDelegateSession(ctx context.Context, id, delegateParent, label string) error {
	if s.failWith != nil {
		return s.failWith
	}
	s.created = append(s.created, id+"|"+delegateParent+"|"+label)
	return nil
}

// writeStubArgs is writeStub pattern plus argc/argv echo: the stub
// prints its own argument vector so the test can see the flags the
// host built (run --session <id> <prompt>).
func writeStubArgs(t *testing.T, name string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), name)
	var body string
	if runtime.GOOS == "windows" {
		bin += ".bat"
		// `args=%*` (the posix branches echo `args="$*"`): the tests
		// pin `args=` as the echo marker, so the token must match
		// across stub shapes
		body = "@echo off\r\necho args=%*\r\nexit /b 0\r\n"
	} else {
		bin += ".sh"
		body = "#!/bin/sh\necho args=\"$*\"\nexit 0\n"
	}
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// TestChildSessionStoredAndPassed pins the stored+searchable ruling:
// with the store seam mounted, the host pre-creates the child's
// session (label delegate:<task>, lineage = the calling session when
// the context carries one) and the child inherits it via run
// --session - the --session persist contract makes the child's prompt,
// report and tool results land in the sessions list. Lineage rides
// delegate_parent by ruling (t_6c4ab302): the fork parent_id stays
// empty so the child never inherits the caller's context.
func TestChildSessionStoredAndPassed(t *testing.T) {
	bin := writeStubArgs(t, "stub-args-omega")
	t.Setenv("OMEGA_BIN", bin)
	store := &stubStore{}
	ctx := agent.WithSessionID(context.Background(), "caller-session-99")

	d := NewDelegate()
	d.store = store
	out, err := d.runDelegateTask(ctx, map[string]any{"prompt": "digest the logs"})
	if err != nil {
		t.Fatalf("runDelegateTask: %v (%s)", err, out)
	}
	// The report IS the tool result now (card t_0805d438): the child's
	// own stdout returns to the caller.
	if !strings.Contains(out, "--session") {
		t.Fatalf("child argv missing --session: %q", out)
	}

	if len(store.created) != 1 {
		t.Fatalf("delegate session creates = %d, want 1", len(store.created))
	}
	if len(store.forked) != 0 {
		t.Fatalf("delegate spawn used the FORK create path: %v - delegate lineage must never ride parent_id", store.forked)
	}
	parts := strings.Split(store.created[0], "|")
	id, delegateParent, label := parts[0], parts[1], parts[2]
	if id == "" {
		t.Fatalf("malformed created row %q", store.created[0])
	}
	if delegateParent != "caller-session-99" {
		t.Errorf("child session delegate_parent = %q, want caller-session-99", delegateParent)
	}
	if label != "delegate:task-1" {
		t.Errorf("child session label = %q, want delegate:task-1", label)
	}
	// The session reference rides the status surface (the ID-uniqueness
	// shape: the id alone is unsafe to match on).
	status, err := d.runDelegateStatus(context.Background(), nil)
	if err != nil {
		t.Fatalf("runDelegateStatus: %v", err)
	}
	if !strings.Contains(status, "(session "+id+")") {
		t.Errorf("status missing the session handle: %q", status)
	}
}

// forkOnlyStore is a store seam that predates the delegate surface (no
// CreateDelegateSession). The spawn must degrade to an UNLINKED root -
// a store without the split cannot express delegate lineage, and the
// context-inheriting parent link is never an acceptable fallback.
type forkOnlyStore struct {
	agent.StoreProvider
	created []string
}

func (s *forkOnlyStore) CreateSession(ctx context.Context, id, parentID, label string) error {
	s.created = append(s.created, id+"|"+parentID+"|"+label)
	return nil
}

// TestLegacyStoreGetsUnlinkedChild pins the fallback: a store without
// the delegate surface cannot inherit the caller's history.
func TestLegacyStoreGetsUnlinkedChild(t *testing.T) {
	bin := writeStubArgs(t, "stub-forkonly-omega")
	t.Setenv("OMEGA_BIN", bin)
	st := &forkOnlyStore{}
	ctx := agent.WithSessionID(context.Background(), "caller-session-99")

	d := NewDelegate()
	d.store = st
	out, err := d.runDelegateTask(ctx, map[string]any{"prompt": "fresh context please"})
	if err != nil {
		t.Fatalf("runDelegateTask: %v (%s)", err, out)
	}
	if !strings.Contains(out, "--session") {
		t.Fatalf("child argv missing --session: %q", out)
	}
	if len(st.created) != 1 {
		t.Fatalf("creates = %d, want 1", len(st.created))
	}
	if parts := strings.Split(st.created[0], "|"); parts[1] != "" {
		t.Fatalf("legacy-store child parent = %q, want empty (fresh context)", parts[1])
	}
}

// TestSessionCreateFailureDegradesToPlainRun pins the nil-safe law:
// a failing store NEVER fails the spawn - the child degrades to the
// old plain run shape, nothing persisted, result still injected.
func TestSessionCreateFailureDegradesToPlainRun(t *testing.T) {
	bin := writeStubArgs(t, "stub-degrade-omega")
	t.Setenv("OMEGA_BIN", bin)
	store := &stubStore{failWith: errors.New("db wedged")}

	d := NewDelegate()
	d.store = store
	out, err := d.runDelegateTask(context.Background(), map[string]any{"prompt": "still delegate"})
	if err != nil {
		t.Fatalf("runDelegateTask: %v", err)
	}
	if strings.Contains(out, "(session ") {
		t.Fatalf("degraded spawn leaked a session handle: %q", out)
	}
	// The report IS the tool result: the child's args echo rides it,
	// and the degraded shape carries no --session.
	if !strings.Contains(out, "args=") {
		t.Fatalf("plain-run result missing: %q", out)
	}
	if strings.Contains(out, "--session") {
		t.Fatalf("degraded spawn still carried --session: %q", out)
	}
}

// TestSummaryCapTruncation pins the small-caller-context law: an
// oversized child answer is cut at the cap, middle out, with a named
// marker - head kept, tail kept, nothing silent.
func TestSummaryCapTruncation(t *testing.T) {
	s := strings.Repeat("a", summaryHead) + strings.Repeat("m", 10000) + strings.Repeat("z", summaryTail)
	clipped := clipSummary(s)
	runes := []rune(clipped)
	if len(runes) > summaryCap+200 {
		t.Fatalf("clipped summary %d runes, beyond cap+marker", len(runes))
	}
	if !strings.Contains(clipped, "[delegate:") {
		t.Fatal("clipped summary lacks the truncation marker")
	}
	if !strings.HasPrefix(clipped, strings.Repeat("a", 100)) {
		t.Fatal("head not kept")
	}
	if !strings.HasSuffix(clipped, strings.Repeat("z", summaryTail)) {
		t.Fatal("tail not kept")
	}
}

// TestRenderResultEmptyOutput pins the blank-turn law: a clean exit
// with nothing printed still delivers a named turn, never a blank one.
func TestRenderResultEmptyOutput(t *testing.T) {
	task := &delegateTask{id: "task-3"}
	if got := task.renderResult(nil); got != "delegate task-3: finished without output" {
		t.Fatalf("empty-output shape = %q", got)
	}
}

// TestRenderResultTrimWhitespace pins the trimmed summary: the child's
// trailing newline noise must not ride into the injected turn.
func TestRenderResultTrimWhitespace(t *testing.T) {
	task := &delegateTask{id: "task-4"}
	task.out.WriteString("\n\nthe answer\n")
	if got := task.renderResult(nil); got != "the answer" {
		t.Fatalf("trimmed summary = %q", got)
	}
}
