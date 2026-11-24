package main

// desktop_test.go: the desktop boot contract - the runtime the window points at, the
// loud port failure, the setup gate, the headless health probe the release gate calls,
// and ensure-an-instance (connect to a live agent, never boot a second one).

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// freeDesktopPort reserves a port and releases it for the test's own bind.
func freeDesktopPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// desktopTestConfig writes the minimal network-free config: the mock provider answers
// without a host, so a boot needs nothing but the temp home.
func desktopTestConfig(t *testing.T, port int) string {
	t.Helper()
	return writeTempConfig(t, fmt.Sprintf(`provider:
  model_name: mock-model
  type: mock
server:
  port: %d
`, port))
}

// fakeInstance is the detached-serve handle a test injects.
type fakeInstance struct {
	err  error
	done chan struct{}
}

func (f *fakeInstance) Wait() (*os.ProcessState, error) {
	<-f.done
	return nil, f.err
}

// useSpawn swaps the spawn seam for one test.
func useSpawn(t *testing.T, fn func(string) (instanceProcess, error)) {
	t.Helper()
	old := spawnInstanceServe
	t.Cleanup(func() { spawnInstanceServe = old })
	spawnInstanceServe = fn
}

func TestDesktopRuntimeServesSPA(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	port := freeDesktopPort(t)

	cfgPath := desktopTestConfig(t, port)
	cfg, _, err := desktopConfig(cfgPath)
	if err != nil {
		t.Fatalf("desktopConfig: %v", err)
	}
	origin, gotPort := instanceOrigin(cfg)
	if gotPort != port {
		t.Fatalf("instanceOrigin port = %d, want %d", gotPort, port)
	}

	rt, err := startRuntime(cfg, cfgPath, nil, trustFlags{})
	if err != nil {
		t.Fatalf("startRuntime: %v", err)
	}
	if rt.URL != origin {
		t.Fatalf("runtime URL = %q, want the instance origin %q", rt.URL, origin)
	}
	client := &http.Client{Timeout: 5 * time.Second}

	health, err := healthBody(client, rt.URL)
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	if !strings.Contains(health, `"status":"ok"`) {
		t.Fatalf("/health body = %q, want status ok", health)
	}

	resp, err := client.Get(rt.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		t.Fatalf("read /: %v", readErr)
	}
	if !strings.Contains(string(body), "<title>omega</title>") {
		t.Fatalf("served index has no <title>omega</title>: %.200q", string(body))
	}

	rt.shutdown()

	if _, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port))); err != nil {
		t.Fatalf("port %d is still bound after shutdown: %v", port, err)
	}
}

func TestDesktopPortOccupiedFailsLoud(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())

	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("hold a port: %v", err)
	}
	defer held.Close()
	port := held.Addr().(*net.TCPAddr).Port

	cfgPath := desktopTestConfig(t, port)
	cfg, _, err := desktopConfig(cfgPath)
	if err != nil {
		t.Fatalf("desktopConfig: %v", err)
	}

	rt, err := startRuntime(cfg, cfgPath, nil, trustFlags{})
	if err == nil {
		rt.shutdown()
		t.Fatalf("startRuntime bound a port that is already held")
	}
	if !strings.Contains(err.Error(), strconv.Itoa(port)) {
		t.Fatalf("error %q does not name the held port %d", err, port)
	}
}

func TestDesktopNeedsSetup(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	if _, _, err := desktopConfig(""); !errors.Is(err, ErrNeedsSetup) {
		t.Fatalf("desktopConfig on an empty home = %v, want ErrNeedsSetup", err)
	}
}

func TestDesktopHeadlessHealth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	port := freeDesktopPort(t)
	cfgPath := desktopTestConfig(t, port)

	out, err := captureStdout(t, func() error {
		return run([]string{"desktop", "--headless-health", "--config", cfgPath})
	})
	if err != nil {
		t.Fatalf("omega desktop --headless-health: %v", err)
	}
	if !strings.Contains(out, `"status":"ok"`) {
		t.Fatalf("headless output carries no healthy body: %q", out)
	}
	if conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 250*time.Millisecond); err == nil {
		conn.Close()
		t.Fatalf("headless run left the listener up on %d", port)
	}
}

// TestDesktopEnsureInstanceConnectsToLiveInstance is the ruling-15 core: a live agent is
// connected to, never duplicated.
func TestDesktopEnsureInstanceConnectsToLiveInstance(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	port := freeDesktopPort(t)
	cfgPath := desktopTestConfig(t, port)
	cfg, _, err := desktopConfig(cfgPath)
	if err != nil {
		t.Fatalf("desktopConfig: %v", err)
	}
	rt, err := startRuntime(cfg, cfgPath, nil, trustFlags{})
	if err != nil {
		t.Fatalf("startRuntime: %v", err)
	}
	defer rt.shutdown()

	useSpawn(t, func(string) (instanceProcess, error) {
		t.Fatal("ensureInstance spawned although an instance was already live")
		return nil, nil
	})

	origin, err := ensureInstance(cfg, cfgPath)
	if err != nil {
		t.Fatalf("ensureInstance: %v", err)
	}
	if origin != rt.URL {
		t.Fatalf("ensureInstance origin = %q, want the live instance %q", origin, rt.URL)
	}
}

// TestDesktopEnsureInstanceDiagnosesHeldPort: a port that answers but is not ours is
// named, and the wait never degrades into a silent spinner.
func TestDesktopEnsureInstanceDiagnosesHeldPort(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())

	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not omega", http.StatusServiceUnavailable)
	}))
	defer foreign.Close()
	u, err := url.Parse(foreign.URL)
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("test server port: %v", err)
	}

	cfgPath := desktopTestConfig(t, port)
	cfg, _, err := desktopConfig(cfgPath)
	if err != nil {
		t.Fatalf("desktopConfig: %v", err)
	}

	done := make(chan struct{})
	close(done)
	useSpawn(t, func(string) (instanceProcess, error) {
		return &fakeInstance{err: errors.New("listen tcp: bind: address already in use"), done: done}, nil
	})

	if _, err = ensureInstance(cfg, cfgPath); err == nil {
		t.Fatal("ensureInstance accepted a port that answers but is not an omega instance")
	}
	msg := err.Error()
	if !strings.Contains(msg, strconv.Itoa(port)) {
		t.Fatalf("error %q does not name port %d", msg, port)
	}
	if !strings.Contains(msg, "held by another process") {
		t.Fatalf("error %q does not diagnose the foreign listener", msg)
	}
}

// TestDesktopBareLaunchWithoutConsole: a windowless bare `omega` is the desktop, and a
// configless home hands it the setup gate instead of a dead end.
func TestDesktopBareLaunchWithoutConsole(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())

	oldLaunch, oldWizard := desktopForBareLaunch, spawnSetupWizard
	t.Cleanup(func() { desktopForBareLaunch, spawnSetupWizard = oldLaunch, oldWizard })
	desktopForBareLaunch = func() bool { return true }
	wizards := 0
	spawnSetupWizard = func() error {
		wizards++
		return nil
	}

	if err := run(nil); err != nil {
		t.Fatalf("bare windowless launch: %v", err)
	}
	if wizards != 1 {
		t.Fatalf("setup wizard spawns = %d, want 1", wizards)
	}
}
