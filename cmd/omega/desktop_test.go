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
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/harness/observability"
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

// TestDesktopStartsHidden pins the launch decision: only the config value decides it, and
// an untouched config shows the window.
func TestDesktopStartsHidden(t *testing.T) {
	if desktopStartsHidden(DefaultConfig()) {
		t.Fatal("an untouched config launches to the tray")
	}
	cfg := DefaultConfig()
	cfg.Desktop.StartMinimized = true
	if !desktopStartsHidden(cfg) {
		t.Fatal("start_minimized did not decide a hidden launch")
	}
}

// TestDesktopWindowPolicyNow pins the per-press read: a save (or a hand edit) lands under
// the next press, a file that never named the keys keeps close-to-tray on, and an
// unreadable file keeps the launch snapshot.
func TestDesktopWindowPolicyNow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("provider:\n  model_name: m\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got := desktopWindowPolicyNow(path, desktopPolicyToday)
	if got.MinimizeToTray || !got.CloseToTray {
		t.Fatalf("a file without the keys = %+v, want minimize off and close on", got)
	}

	body := "desktop:\n  minimize_to_tray: true\n  close_to_tray: false\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	got = desktopWindowPolicyNow(path, desktopPolicyToday)
	if !got.MinimizeToTray || got.CloseToTray {
		t.Fatalf("the file's keys = %+v, want minimize on and close off", got)
	}

	fallback := desktopWindowPolicy{MinimizeToTray: true}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got = desktopWindowPolicyNow(path, fallback); got != fallback {
		t.Fatalf("an unreadable file answered %+v, want the launch snapshot %+v", got, fallback)
	}
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

// fakeOwner is the lifetime handle a fake spawn hands back; production never releases.
type fakeOwner struct {
	released int
}

func (f *fakeOwner) release() { f.released++ }

// useSpawn swaps the spawn seam for one test.
func useSpawn(t *testing.T, fn func(string) (instanceProcess, instanceOwner, error)) {
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

	useSpawn(t, func(string) (instanceProcess, instanceOwner, error) {
		t.Fatal("ensureInstance spawned although an instance was already live")
		return nil, nil, nil
	})

	origin, owner, err := ensureInstance(cfg, cfgPath)
	if err != nil {
		t.Fatalf("ensureInstance: %v", err)
	}
	if origin != rt.URL {
		t.Fatalf("ensureInstance origin = %q, want the live instance %q", origin, rt.URL)
	}
	if owner != nil {
		t.Fatalf("ensureInstance owner = %T, want nil for an instance it only connected to", owner)
	}
}

// TestDesktopEnsureInstanceSpawnOwnsInstance: a launch that started the agent keeps the
// handle that ties the agent to it.
func TestDesktopEnsureInstanceSpawnOwnsInstance(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	port := freeDesktopPort(t)
	cfgPath := desktopTestConfig(t, port)
	cfg, _, err := desktopConfig(cfgPath)
	if err != nil {
		t.Fatalf("desktopConfig: %v", err)
	}

	owner := &fakeOwner{}
	useSpawn(t, func(string) (instanceProcess, instanceOwner, error) {
		ln, lerr := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if lerr != nil {
			return nil, nil, lerr
		}
		serve := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"status":"ok"}`)
		})}
		go func() { _ = serve.Serve(ln) }()
		t.Cleanup(func() { _ = serve.Close() })
		proc := &fakeInstance{done: make(chan struct{})}
		t.Cleanup(func() { close(proc.done) })
		return proc, owner, nil
	})

	origin, got, err := ensureInstance(cfg, cfgPath)
	if err != nil {
		t.Fatalf("ensureInstance: %v", err)
	}
	if origin != fmt.Sprintf("http://127.0.0.1:%d", port) {
		t.Fatalf("ensureInstance origin = %q, want the config origin", origin)
	}
	if got != owner {
		t.Fatalf("ensureInstance owner = %v, want the owner its spawn returned", got)
	}
	if owner.released != 0 {
		t.Fatalf("ensureInstance released the owner %d times: production never releases", owner.released)
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
	useSpawn(t, func(string) (instanceProcess, instanceOwner, error) {
		return &fakeInstance{err: errors.New("listen tcp: bind: address already in use"), done: done}, nil, nil
	})

	if _, _, err = ensureInstance(cfg, cfgPath); err == nil {
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

// TestDesktopBootErrors is the fallback-dialog contract without a window, a WebView2
// runtime or an OS: one box for the first boot failure, a log line before it, and no
// second box for anything after - a storm included.
func TestDesktopBootErrors(t *testing.T) {
	logLines := func(t *testing.T, path string) []string {
		t.Helper()
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read the log: %v", err)
		}
		var lines []string
		for _, line := range strings.Split(string(body), "\n") {
			if strings.TrimSpace(line) != "" {
				lines = append(lines, line)
			}
		}
		return lines
	}
	newTestBootErrors := func(t *testing.T) (*desktopBootErrors, *[]string, string) {
		t.Helper()
		logPath := filepath.Join(t.TempDir(), "omega.log")
		logf, err := observability.NewLogger(observability.Config{Enabled: true, File: logPath})
		if err != nil {
			t.Fatalf("NewLogger: %v", err)
		}
		t.Cleanup(func() { _ = logf.Close() })
		var boxes []string
		boot := newDesktopBootErrors(logf, logPath, func(title, msg string) {
			boxes = append(boxes, title+"\n"+msg)
		})
		return boot, &boxes, logPath
	}

	t.Run("first boot error boxes once and logs the line", func(t *testing.T) {
		boot, boxes, logPath := newTestBootErrors(t)
		cause := errors.New("error getting Webview2 runtime version: the runtime is not installed")
		boot.Handle(cause)

		if len(*boxes) != 1 {
			t.Fatalf("boxes = %d, want exactly 1", len(*boxes))
		}
		box := (*boxes)[0]
		if !strings.HasPrefix(box, "omega\n") {
			t.Fatalf("box title = %q, want the omega caption", strings.SplitN(box, "\n", 2)[0])
		}
		for _, want := range []string{"omega could not open its window.", "WebView2", cause.Error(), logPath} {
			if !strings.Contains(box, want) {
				t.Fatalf("box text is missing %q:\n%s", want, box)
			}
		}

		lines := logLines(t, logPath)
		if len(lines) != 1 {
			t.Fatalf("log lines = %d, want exactly 1: %q", len(lines), lines)
		}
		for _, want := range []string{"desktop: window boot failed:", cause.Error(), "the user saw the fallback dialog"} {
			if !strings.Contains(lines[0], want) {
				t.Fatalf("log line is missing %q: %q", want, lines[0])
			}
		}
	})

	t.Run("second boot error logs and never stacks a box", func(t *testing.T) {
		boot, boxes, logPath := newTestBootErrors(t)
		boot.Handle(errors.New("browser path 'C:/gone' does not exist"))
		boot.Handle(errors.New("unable to initialise WebView2 for window \"omega\""))

		if len(*boxes) != 1 {
			t.Fatalf("boxes = %d, want exactly 1 for the storm", len(*boxes))
		}
		lines := logLines(t, logPath)
		if len(lines) != 2 {
			t.Fatalf("log lines = %d, want 2: %q", len(lines), lines)
		}
		if !strings.Contains(lines[1], "already showed") {
			t.Fatalf("second log line does not say the box already showed: %q", lines[1])
		}
	})

	t.Run("an error after the window is up logs and never boxes", func(t *testing.T) {
		boot, boxes, logPath := newTestBootErrors(t)
		boot.WindowUp()
		boot.Handle(errors.New("rasterization scale unavailable"))

		if len(*boxes) != 0 {
			t.Fatalf("boxes = %d, want 0 after the window is up", len(*boxes))
		}
		lines := logLines(t, logPath)
		if len(lines) != 1 || !strings.Contains(lines[0], "window error after boot") {
			t.Fatalf("post-boot log = %q, want one after-boot line", lines)
		}
	})

	t.Run("a disabled logger never suppresses the dialog", func(t *testing.T) {
		var boxes []string
		boot := newDesktopBootErrors(observability.NopLogger{}, "", func(title, msg string) {
			boxes = append(boxes, title+"\n"+msg)
		})
		boot.Handle(errors.New("unable to initialise WebView2 for window \"omega\""))

		if len(boxes) != 1 {
			t.Fatalf("boxes = %d, want 1 with logging disabled", len(boxes))
		}
		if !strings.Contains(boxes[0], "WebView2") {
			t.Fatalf("box text names no WebView2 cause:\n%s", boxes[0])
		}
	})
}

// TestDesktopBareLaunchWithoutConsole: a windowless bare `omega` is routed to the
// desktop, and a configless home hands it the setup gate instead of a dead end. The
// window is Windows-only in 0.19.0, so every other GOOS must land on that platform
// gate: the seam proves the routing even where the window cannot open.
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

	err := run(nil)
	if runtime.GOOS != "windows" {
		if err == nil || !strings.Contains(err.Error(), "Windows-only") {
			t.Fatalf("bare launch on %s = %v, want the platform gate's Windows-only error", runtime.GOOS, err)
		}
		if wizards != 0 {
			t.Fatalf("setup wizard spawns = %d, want 0 behind the platform gate", wizards)
		}
		return
	}
	if err != nil {
		t.Fatalf("bare windowless launch: %v", err)
	}
	if wizards != 1 {
		t.Fatalf("setup wizard spawns = %d, want 1", wizards)
	}
}
