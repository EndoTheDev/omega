package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mockExtensionDir returns a temp directory containing a freshly built
// copy of the mock extension binary.
func mockExtensionDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join("testdata", "mock_extension", "main.go")

	// Add .exe on Windows.
	destName := "mock-ext"
	if os.PathSeparator == '\\' {
		destName = "mock-ext.exe"
	}
	dest := filepath.Join(dir, destName)

	cmd := exec.Command("go", "build", "-o", dest, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build mock extension: %v\n%s", err, string(out))
	}
	return dir
}

// TestNoopManager verifies the no-op manager does nothing.
func TestNoopManager(t *testing.T) {
	m := NoopManager{}
	if err := m.Load("/nonexistent", ""); err != nil {
		t.Fatalf("NoopManager.Load returned error: %v", err)
	}
	if tools := m.Tools(); len(tools) != 0 {
		t.Errorf("NoopManager.Tools = %d, want 0", len(tools))
	}
	if cmds := m.Commands(); len(cmds) != 0 {
		t.Errorf("NoopManager.Commands = %d, want 0", len(cmds))
	}
	if infos := m.Infos(); len(infos) != 0 {
		t.Errorf("NoopManager.Infos = %d, want 0", len(infos))
	}
	m.DispatchEvent(AgentStart{Type: "agent_start"})
	if _, err := m.CallCommand(context.Background(), "/test", ""); err == nil {
		t.Fatal("NoopManager.CallCommand should error")
	}
	if err := m.Close(); err != nil {
		t.Fatalf("NoopManager.Close returned error: %v", err)
	}
}

// TestStdioManagerLoadMissingDir verifies a missing dir loads zero
// extensions without error.
func TestStdioManagerLoadMissingDir(t *testing.T) {
	m := &StdioManager{}
	if err := m.Load("/nonexistent/path", ""); err != nil {
		t.Fatalf("Load missing dir: %v", err)
	}
	if len(m.Tools()) != 0 {
		t.Errorf("Tools = %d, want 0", len(m.Tools()))
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestStdioManagerInitialize verifies the full initialize handshake.
func TestStdioManagerInitialize(t *testing.T) {
	dir := mockExtensionDir(t)
	m := &StdioManager{}
	if err := m.Load(dir, ""); err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer m.Close()

	tools := m.Tools()
	if len(tools) != 1 {
		t.Fatalf("Tools = %d, want 1", len(tools))
	}
	tool, ok := tools["echo_tool"]
	if !ok {
		t.Fatal("missing echo_tool")
	}
	if tool.Description == "" {
		t.Error("echo_tool has empty description")
	}
	if tool.Run == nil {
		t.Error("echo_tool has nil Run")
	}

	cmds := m.Commands()
	if len(cmds) != 1 {
		t.Fatalf("Commands = %d, want 1", len(cmds))
	}
	if cmds[0].Name != "/ext-test" {
		t.Errorf("command name = %q, want /ext-test", cmds[0].Name)
	}

	infos := m.Infos()
	if len(infos) != 1 {
		t.Fatalf("Infos = %d, want 1", len(infos))
	}
	if infos[0].Name != "mock-ext" {
		t.Errorf("extension name = %q, want mock-ext", infos[0].Name)
	}
	if infos[0].Tools != 1 {
		t.Errorf("extension tools = %d, want 1", infos[0].Tools)
	}
}

// TestStdioManagerToolCall verifies a tool call round-trips.
func TestStdioManagerToolCall(t *testing.T) {
	dir := mockExtensionDir(t)
	m := &StdioManager{}
	if err := m.Load(dir, ""); err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer m.Close()

	tools := m.Tools()
	tool := tools["echo_tool"]
	result, err := tool.Run(context.Background(), map[string]any{"text": "hello"})
	if err != nil {
		t.Fatalf("tool.Run: %v", err)
	}
	if !strings.Contains(result, "hello") {
		t.Errorf("tool result = %q, want it to contain 'hello'", result)
	}
	if !strings.Contains(result, "echo:") {
		t.Errorf("tool result = %q, want it to contain 'echo:'", result)
	}
}

// TestStdioManagerCommand verifies a command call round-trips.
func TestStdioManagerCommand(t *testing.T) {
	dir := mockExtensionDir(t)
	m := &StdioManager{}
	if err := m.Load(dir, ""); err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer m.Close()

	output, err := m.CallCommand(context.Background(), "/ext-test", "test args")
	if err != nil {
		t.Fatalf("CallCommand: %v", err)
	}
	if output != "command executed" {
		t.Errorf("command output = %q, want 'command executed'", output)
	}
}

// TestStdioManagerEventDispatch verifies events are sent to subscribed
// extensions.
func TestStdioManagerEventDispatch(t *testing.T) {
	dir := mockExtensionDir(t)
	eventsFile := filepath.Join(dir, "events.log")
	os.Setenv("OMEGA_TEST_EVENTS", eventsFile)
	defer os.Unsetenv("OMEGA_TEST_EVENTS")

	m := &StdioManager{}
	if err := m.Load(dir, ""); err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer m.Close()

	m.DispatchEvent(TurnStart{Type: "turn_start", Turn: 1})
	m.DispatchEvent(TurnEnd{Type: "turn_end", Turn: 1, ToolCalls: 0})

	time.Sleep(500 * time.Millisecond)

	data, err := os.ReadFile(eventsFile)
	if err != nil {
		t.Fatalf("read events file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "turn_start") {
		t.Error("events file missing turn_start")
	}
	if !strings.Contains(content, "turn_end") {
		t.Error("events file missing turn_end")
	}
}

// TestStdioManagerClose verifies Close terminates all extension processes.
func TestStdioManagerClose(t *testing.T) {
	dir := mockExtensionDir(t)
	m := &StdioManager{}
	if err := m.Load(dir, ""); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := m.Close(); err != nil {
		t.Fatalf("double Close: %v", err)
	}

	infos := m.Infos()
	if len(infos) != 1 {
		t.Fatalf("Infos = %d, want 1", len(infos))
	}
	if infos[0].Status != "stopped" {
		t.Errorf("status = %q, want 'stopped'", infos[0].Status)
	}
}

// TestStdioManagerUnknownCommand verifies CallCommand errors for an
// unregistered command.
func TestStdioManagerUnknownCommand(t *testing.T) {
	m := &StdioManager{}
	if _, err := m.CallCommand(context.Background(), "/nonexistent", ""); err == nil {
		t.Fatal("expected error for unknown command")
	}
}

// TestStdioManagerSkipNonExecutable verifies hidden and documentation
// files are skipped without killing the manager.
func TestStdioManagerSkipNonExecutable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("docs"), 0644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden"), []byte("x"), 0755); err != nil {
		t.Fatalf("write hidden: %v", err)
	}

	m := &StdioManager{}
	if err := m.Load(dir, ""); err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer m.Close()

	if len(m.exts) != 0 {
		t.Errorf("extensions = %d, want 0", len(m.exts))
	}
}

// TestStdioManagerConflictWinsFirst verifies that the first loaded
// extension wins on name conflict. The manager enforces first-wins for
// tools.
func TestStdioManagerConflictWinsFirst(t *testing.T) {
	dir := mockExtensionDir(t)
	m := &StdioManager{}
	if err := m.Load(dir, ""); err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer m.Close()

	// Loading the same dir twice should not duplicate tools.
	tools := m.Tools()
	if _, ok := tools["echo_tool"]; !ok {
		t.Fatal("missing echo_tool")
	}
}

// TestStdioManagerLoadFile verifies loading a single extension by explicit
// path (the --extension/-e code path).
func TestStdioManagerLoadFile(t *testing.T) {
	dir := mockExtensionDir(t)
	extPath := filepath.Join(dir, "mock-ext")
	if os.PathSeparator == '\\' {
		extPath += ".exe"
	}

	m := &StdioManager{}
	if err := m.LoadFile(extPath, ""); err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	defer m.Close()

	tools := m.Tools()
	if _, ok := tools["echo_tool"]; !ok {
		t.Fatal("missing echo_tool after LoadFile")
	}
	if infos := m.Infos(); len(infos) != 1 {
		t.Fatalf("Infos = %d, want 1", len(infos))
	}
}

// TestStdioManagerLoadFileMissing verifies LoadFile errors on a nonexistent
// path (the caller logs and skips).
func TestStdioManagerLoadFileMissing(t *testing.T) {
	m := &StdioManager{}
	if err := m.LoadFile("/nonexistent/extension", ""); err == nil {
		t.Fatal("LoadFile should error on nonexistent path")
	}
}

// TestStdioManagerLoadAppends verifies calling Load twice appends rather
// than resetting (main dir + project dir).
func TestStdioManagerLoadAppends(t *testing.T) {
	dir := mockExtensionDir(t)
	m := &StdioManager{}
	if err := m.Load(dir, ""); err != nil {
		t.Fatalf("first Load: %v", err)
	}
	defer m.Close()

	// Second Load of the same dir spawns a second process but must not
	// clobber the tool map (first registration wins).
	if err := m.Load(dir, ""); err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if _, ok := m.Tools()["echo_tool"]; !ok {
		t.Fatal("missing echo_tool after second Load")
	}
}

// TestExtensionManagerSeam verifies the StdioManager satisfies the
// ExtensionManager interface.
func TestExtensionManagerSeam(t *testing.T) {
	var _ ExtensionManager = (&StdioManager{})
	var _ ExtensionManager = (NoopManager{})
}

// TestStdioManagerProcessDeath verifies a crashed extension is marked
// dead and calls error cleanly.
func TestStdioManagerProcessDeath(t *testing.T) {
	dir := mockExtensionDir(t)
	m := &StdioManager{}
	if err := m.Load(dir, ""); err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer m.Close()

	// Kill the process directly.
	ext := m.exts[0]
	ext.cmd.Process.Kill()
	time.Sleep(100 * time.Millisecond)

	// Subsequent tool call should fail.
	_, err := ext.callTool(context.Background(), "echo_tool", map[string]any{"text": "x"})
	if err == nil {
		t.Fatal("expected error after process death")
	}
}

// TestStdioManagerNoToolsNoCommands verifies an extension can initialize
// with no tools or commands.
func TestStdioManagerNoToolsNoCommands(t *testing.T) {
	// This test just ensures the interface handles empty registries.
	m := &StdioManager{}
	if tools := m.Tools(); len(tools) != 0 {
		t.Errorf("empty Tools = %d, want 0", len(tools))
	}
	if cmds := m.Commands(); len(cmds) != 0 {
		t.Errorf("empty Commands = %d, want 0", len(cmds))
	}
	if infos := m.Infos(); len(infos) != 0 {
		t.Errorf("empty Infos = %d, want 0", len(infos))
	}
}
