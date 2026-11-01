package main

// The setup-wizard + CLI-loop proofs (card t_0744eb8e): the wizard is
// the gate (no-config refusal everywhere), a fresh home walks the
// wizard and lands a working config, and the loop's dispatch and
// renderer behave as plain text on paper.

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
)

// stubLister is the setup wizard's fake transport: a ListModels answer
// without a network. The real one is provider.NewProvider.
type stubLister struct {
	models []string
	err    error
}

func (s stubLister) ListModels() ([]string, error) { return s.models, s.err }

func withStubProvider(t *testing.T, stub stubLister) {
	t.Helper()
	old := newSetupProvider
	newSetupProvider = func(typ, modelName, baseURL, apiKey string) modelsLister {
		return stub
	}
	t.Cleanup(func() { newSetupProvider = old })
}

// TestSetupWizardWritesConfig is the fresh-install flow end to end at
// the wizard level: an empty home, the wizard driven over an in-memory
// stream, and the written config parses with the real loader.
func TestSetupWizardWritesConfig(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	withStubProvider(t, stubLister{models: []string{"qwen3:8b", "llama3.2:3b"}})

	out := &bytes.Buffer{}
	s := setupSession{
		in:  bufio.NewReader(strings.NewReader("1\n\n1\n")), // ollama, default host, first model
		out: out,
	}
	if err := s.Run(); err != nil {
		t.Fatalf("setup run: %v", err)
	}

	path := filepath.Join(omegaHome(), "config", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("setup wrote no config: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "type: ollama") {
		t.Errorf("config missing provider type:\n%s", body)
	}
	if !strings.Contains(body, "model_name: qwen3:8b") {
		t.Errorf("config missing picked model:\n%s", body)
	}
	if _, err := LoadConfig(path); err != nil {
		t.Errorf("setup-written config does not load: %v", err)
	}

	// the door prints: webui URL + the loop hint
	text := out.String()
	if !strings.Contains(text, "webui: http://localhost:8099/") {
		t.Errorf("setup output misses the webui URL:\n%s", text)
	}
	if !strings.Contains(text, "`omega` starts a terminal session") {
		t.Errorf("setup output misses the loop hint:\n%s", text)
	}
}

// TestSetupWizardRetriedHostLandsInConfig: a failed ListModels on the
// default host that is corrected by a retried base url must write the
// RETRIED host into the config - the model list came from there.
func TestSetupWizardRetriedHostLandsInConfig(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	// the transport fails on the first call (the dead default host),
	// succeeds on the retry - keyed on the host, the real install flow.
	old := newSetupProvider
	newSetupProvider = func(typ, modelName, baseURL, apiKey string) modelsLister {
		if strings.Contains(baseURL, "11335") {
			return stubLister{models: []string{"qwen3:8b"}}
		}
		return stubLister{err: &fakeNetError{}}
	}
	t.Cleanup(func() { newSetupProvider = old })

	retried := "http://127.0.0.1:11335"
	out := &bytes.Buffer{}
	s := setupSession{
		in:  bufio.NewReader(strings.NewReader("1\n\n" + retried + "\n1\n")),
		out: out,
	}
	if err := s.Run(); err != nil {
		t.Fatalf("setup run: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(omegaHome(), "config", "config.yaml"))
	if !strings.Contains(string(body), "host: "+retried) {
		t.Errorf("retried host did not land in the config:\n%s", string(body))
	}
	if !strings.Contains(out.String(), "via "+retried) {
		t.Errorf("summary printed the stale host:\n%s", out.String())
	}
}

// TestSetupWizardCancelledHost is the stranded-install guard: a host
// that fails the live test never strands the install silently - the
// failure names the error and the wizard offers a retry or a forced
// model name. A bare <enter> on ollama aborts with the honest message.
func TestSetupWizardCancelledHost(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	withStubProvider(t, stubLister{err: &fakeNetError{}})

	out := &bytes.Buffer{}
	s := setupSession{
		in:  bufio.NewReader(strings.NewReader("1\n\n\n")), // ollama, default host, abort
		out: out,
	}
	err := s.Run()
	if err == nil {
		t.Fatal("wizard succeeded against a dead host")
	}
	if !strings.Contains(err.Error(), "no model chosen") {
		t.Errorf("abort error not the no-model message: %v", err)
	}
	if !strings.Contains(out.String(), "connection test failed") {
		t.Errorf("wizard hid the connection failure:\n%s", out.String())
	}
}

type fakeNetError struct{}

func (*fakeNetError) Error() string { return "connect refused" }

// TestSetupOverwriteGuard: an existing config is never eaten silently -
// any answer but "y" cancels and leaves the file untouched.
func TestSetupOverwriteGuard(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	dir := filepath.Join(omegaHome(), "config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("hand-tuned: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	withStubProvider(t, stubLister{models: []string{"qwen3:8b"}})

	out := &bytes.Buffer{}
	s := setupSession{
		in:  bufio.NewReader(strings.NewReader("1\n\n1\nn\n")), // wizard answers, then refuses the overwrite
		out: out,
	}
	err := s.Run()
	if err == nil {
		t.Fatal("setup overwrote without a yes")
	}
	if !strings.Contains(err.Error(), "setup cancelled") {
		t.Errorf("cancel error not phrased: %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "hand-tuned: true\n" {
		t.Errorf("existing config was modified: %q", string(data))
	}
}

// TestSetupInteractiveGuard: a non-character stdin refuses with the
// by-hand pointer - the wizard never half-fires from a pipe.
func TestSetupInteractiveGuard(t *testing.T) {
	err := runnableSetup()
	if err == nil {
		t.Skip("stdin is a character device in this test run")
	}
	if !strings.Contains(err.Error(), "interactive") {
		t.Errorf("guard error not phrased: %v", err)
	}
}

// TestRequireConfigGate is the setup gate for the config-dependent
// subcommands: an empty home refuses pointing at setup, a broken
// config refuses pointing at setup, a valid config passes.
func TestRequireConfigGate(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())

	err := requireConfig("")
	if err == nil {
		t.Fatal("empty home accepted by the gate")
	}
	if !strings.Contains(err.Error(), "run `omega setup` first") {
		t.Errorf("no-config refusal does not point at setup: %v", err)
	}

	dir := filepath.Join(omegaHome(), "config")
	if mkerr := os.MkdirAll(dir, 0o755); mkerr != nil {
		t.Fatal(mkerr)
	}
	broken := filepath.Join(dir, "config.yaml")
	if verr := os.WriteFile(broken, []byte(":\nbad:\n - :\n"), 0o600); verr != nil {
		t.Fatal(verr)
	}
	err = requireConfig("")
	if err == nil {
		t.Fatal("broken config accepted by the gate")
	}
	if !strings.Contains(err.Error(), "`omega setup`") {
		t.Errorf("broken-config refusal does not point at setup: %v", err)
	}

	if err := os.WriteFile(broken, []byte("provider:\n  type: ollama\n  model_name: qwen3:8b\n  host: http://localhost:11434\n  api_key: \"\"\nserver:\n  port: 8099\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := requireConfig(""); err != nil {
		t.Errorf("valid config refused by the gate: %v", err)
	}
}

// TestNoConfigRefusesForCLI is the whole-surface fresh-install proof
// at the dispatcher level: serve and run refuse; bare omega drops into
// the wizard, which refuses a non-tty stdin with the honest message.
func TestNoConfigRefusesForCLI(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())

	if err := run([]string{"serve"}); err == nil {
		t.Error("serve without config did not refuse")
	} else if !strings.Contains(err.Error(), "run `omega setup`") {
		t.Errorf("serve refusal does not point at setup: %v", err)
	}

	if err := run([]string{"run", "hello"}); err == nil {
		t.Error("run without config did not refuse")
	} else if !strings.Contains(err.Error(), "run `omega setup`") {
		t.Errorf("run refusal does not point at setup: %v", err)
	}

	// bare omega: the wizard is the gate - with a dead host under a
	// stubbed transport it refuses with the no-model abort (and never
	// probes a live provider from a test).
	withStubProvider(t, stubLister{err: &fakeNetError{}})
	err := run([]string{})
	if err == nil {
		t.Fatal("bare omega in an empty home silently succeeded")
	}
	if !strings.Contains(err.Error(), "setup") {
		t.Errorf("bare-omega path does not name setup: %v", err)
	}
}

func newTestLoop(t *testing.T) (*cliLoop, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	lo := &cliLoop{
		ag: core.NewAgent(nil, nil, 8),
		pctx: &agent.Context{
			Commands: []core.ExtensionCommand{
				{Name: "/cron", Description: "the cron board"},
			},
		},
		out: bufio.NewWriter(buf),
	}
	return lo, buf
}

// stripANSI strips the loop's escape codes so assertions read the
// text, not the palette.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' {
			for ; i < len(s) && s[i] != 'm'; i++ { // the loop only emits sgr
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// TestLoopDispatch pins the loop's own command surface: the exit set
// stops, /help lists, /workspace shows and validates, /session is
// shown or reset, an unclaimed name lists what exists.
func TestLoopDispatch(t *testing.T) {
	lo, buf := newTestLoop(t)

	for _, line := range []string{"/exit", "/quit", "/q", "/bye", "/exit now"} {
		if !lo.dispatch(line) {
			t.Errorf("dispatch(%q) did not stop the loop", line)
		}
	}

	if lo.dispatch("/help") {
		t.Error("/help stopped the loop")
	}
	lo.out.Flush()
	if !strings.Contains(buf.String(), "/session [id|new]") || !strings.Contains(buf.String(), "/cron") {
		t.Errorf("/help output incomplete:\n%s", buf.String())
	}

	buf.Reset()
	if lo.dispatch("/workspace") {
		t.Error("/workspace stopped the loop")
	}
	lo.out.Flush()
	if !strings.Contains(buf.String(), "workspace:") {
		t.Errorf("/workspace show failed:\n%s", buf.String())
	}

	buf.Reset()
	if lo.dispatch("/workspace /definitely/not/a/dir") {
		t.Error("/workspace bad dir stopped the loop")
	}
	lo.out.Flush()
	if !strings.Contains(buf.String(), "is not a directory") {
		t.Errorf("/workspace bad dir not named:\n%s", buf.String())
	}

	buf.Reset()
	lo.sessionID = "s-42"
	if lo.dispatch("/session") {
		t.Error("/session stopped the loop")
	}
	lo.out.Flush()
	if !strings.Contains(buf.String(), "session: s-42") {
		t.Errorf("/session show failed:\n%s", buf.String())
	}

	buf.Reset()
	if lo.dispatch("/session new") {
		t.Error("/session new stopped the loop")
	}
	lo.out.Flush()
	if lo.sessionID != "" || lo.history != nil {
		t.Error("/session new did not reset the conversation")
	}

	buf.Reset()
	if lo.dispatch("/nope things") {
		t.Error("unknown command stopped the loop")
	}
	lo.out.Flush()
	if !strings.Contains(buf.String(), "not a command") {
		t.Errorf("unknown command not refused:\n%s", buf.String())
	}
}

// TestLoopRendererStreams pins the renderer's paper behavior: stream
// chunks print as they arrive (not line-dumped), tool calls are one
// compact cyan line, errors end the block, and closeBlock flushes the
// trailing newline exactly once.
func TestLoopRendererStreams(t *testing.T) {
	lo, buf := newTestLoop(t)
	lo.ansi = true
	r := &loopRenderer{lo: lo}
	plain := func() string { lo.out.Flush(); return stripANSI(buf.String()) }

	r.event(core.StreamEvent{Event: core.ResponseChunk{Content: "hel"}})
	r.event(core.StreamEvent{Event: core.ResponseChunk{Content: "lo"}})
	if !strings.Contains(plain(), "hello") {
		t.Errorf("streaming reply missing:\n%q", plain())
	}

	buf.Reset()
	r.event(core.StreamEvent{Event: core.ToolCallEvent{ToolCall: core.ToolCall{
		ID: "tc1", Name: "read_file", Arguments: map[string]any{"path": "/a/b.txt"},
	}}})
	r.event(core.ToolResultEvent{Message: core.ToolResult{ToolCallID: "tc1", Content: "..", IsError: false}})
	text := plain()
	if !strings.Contains(text, "> read_file") {
		t.Errorf("tool call line missing:\n%q", text)
	}
	if !strings.Contains(text, "path=/a/b.txt") {
		t.Errorf("tool args missing:\n%q", text)
	}
	if !strings.Contains(text, "< read_file (2 chars, ok)") {
		t.Errorf("tool result line missing:\n%q", text)
	}

	buf.Reset()
	r.event(core.StreamEvent{Event: core.StreamEnd{Error: "provider down"}})
	if !strings.Contains(plain(), "provider down") {
		t.Errorf("stream error missing:\n%q", plain())
	}

	// after a text turn, AgentEnd closes the block with one newline
	buf.Reset()
	r.event(core.StreamEvent{Event: core.ResponseChunk{Content: "again"}})
	r.event(core.AgentEnd{})
	if !strings.HasSuffix(plain(), "again\n") {
		t.Errorf("AgentEnd did not close the text block:\n%q", plain())
	}
}

// TestCompactToolArgs pins the one-line law: keys sorted, values
// flattened, fat payloads clipped.
func TestCompactToolArgs(t *testing.T) {
	out := compactToolArgs(map[string]any{
		"b": "x",
		"a": 1,
		"c": strings.Repeat("y", 200),
	})
	lines := strings.Split(out, " ")
	if len(lines) < 3 {
		t.Fatalf("args lost: %q", out)
	}
	if !strings.HasPrefix(out, "a=1 ") {
		t.Errorf("keys not sorted: %q", out)
	}
	if !strings.Contains(out, "...") {
		t.Errorf("fat value not clipped: %q", out)
	}
	if strings.Contains(out, "\n") {
		t.Errorf("newline survived flattening: %q", out)
	}
	if compactToolArgs(nil) != "" {
		t.Error("empty args did not stay empty")
	}
}

// TestLoopPersona pins the tech-support ruling: the loop's fixed
// persona is setup-support shaped (voice only - the knowledge lives in
// the omega-dev skill via the skill port).
func TestLoopPersona(t *testing.T) {
	p := loopPersona()
	if !strings.Contains(p, "tech support") {
		t.Errorf("persona is not setup-support shaped: %s", p)
	}
	if !strings.Contains(p, "omega setup") {
		t.Errorf("persona does not point at setup: %s", p)
	}
}

// stubTrust records the dirs the loop re-gates through the seam and
// answers whatever ResolveContext would.
type stubTrust struct {
	gated []string
}

func (s *stubTrust) ResolveContext(cwd string, approve, noApprove, interactive bool) string {
	s.gated = append(s.gated, cwd)
	return "project context for " + cwd
}

func (s *stubTrust) State(cwd string, approve, noApprove bool) string { return "trusted" }

// TestLoopWorkspaceSwitch pins the real /workspace: a valid dir changes
// the loop's CWD (echoed as an absolute path) and re-runs the trust
// gate for the new dir - the same gate startup runs, never a silent
// no-op.
func TestLoopWorkspaceSwitch(t *testing.T) {
	lo, buf := newTestLoop(t)
	tr := &stubTrust{}
	lo.pctx.Trust = tr
	dir := t.TempDir()

	if lo.dispatch("/workspace " + dir) {
		t.Error("/workspace switch stopped the loop")
	}
	lo.out.Flush()
	if !strings.Contains(buf.String(), "workspace: "+dir) {
		t.Errorf("switch echo missing the absolute dir:\n%s", buf.String())
	}
	if len(tr.gated) != 1 || tr.gated[0] != dir {
		t.Errorf("trust gate not re-run for the new dir: %v", tr.gated)
	}
}
