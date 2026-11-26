//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const instanceJobHelperEnv = "OMEGA_INSTANCE_JOB_HELPER"

// TestInstanceJobHelperProcess is the long-lived child TestInstanceJobKillOnClose puts
// in the job: it does nothing until the job handle closing terminates it. The parent
// re-executes this test binary with the env var set and -test.run pinned to it.
func TestInstanceJobHelperProcess(t *testing.T) {
	if os.Getenv(instanceJobHelperEnv) != "1" {
		t.Skip("helper process for TestInstanceJobKillOnClose")
	}
	time.Sleep(2 * testWaitTimeout)
}

// TestInstanceJobKillOnClose is the real mechanism end to end: a child assigned to the
// job dies when the job's last handle closes, which is what a desktop process exit does.
func TestInstanceJobKillOnClose(t *testing.T) {
	job, err := newInstanceJob()
	if err != nil {
		t.Fatalf("newInstanceJob: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(exe, "-test.run", "^TestInstanceJobHelperProcess$")
	cmd.Env = append(os.Environ(), instanceJobHelperEnv+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the helper: %v", err)
	}
	exited := make(chan struct{})
	t.Cleanup(func() {
		// Bounded: a failure before the waiter starts leaves the channel unclosed.
		_ = cmd.Process.Kill()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
		}
	})
	if err := job.assign(cmd.Process); err != nil {
		t.Fatalf("assign the helper to the job: %v", err)
	}
	go func() {
		_, _ = cmd.Process.Wait()
		close(exited)
	}()

	job.release()

	// The poll catches a hang; it does not measure the kill's latency.
	deadline := time.Now().Add(testWaitTimeout)
	for {
		select {
		case <-exited:
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("the helper outlived the closed job handle by %s", testWaitTimeout)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestDesktopAttachAnswerIsStable pins the memo behind the routing: main attaches
// before its first write and bareLaunchWantsDesktop asks again, while a second
// AttachConsole on an attached process fails. One answer per process keeps a terminal
// launch out of the desktop branch.
func TestDesktopAttachAnswerIsStable(t *testing.T) {
	first := attachCallerConsole()
	t.Logf("attach answer in the test process = %v", first)
	for i := 2; i <= 4; i++ {
		if got := attachCallerConsole(); got != first {
			t.Fatalf("attach answer #%d = %v, want the first answer %v", i, got, first)
		}
	}
	if got := bareLaunchWantsDesktop(); got == first {
		t.Fatalf("bareLaunchWantsDesktop = %v after attach = %v, want its negation", got, first)
	}
}

// TestDesktopWebView2RuntimeProbe is the host receipt for the gate's input: the same
// client-state keys the WebView2 loader reads, answered by this machine.
func TestDesktopWebView2RuntimeProbe(t *testing.T) {
	version, err := webView2RuntimeVersion()
	if err != nil {
		t.Fatalf("webView2RuntimeVersion on this host: %v", err)
	}
	t.Logf("webView2RuntimeVersion on this host = %q", version)
	if version == "" {
		t.Skip("no WebView2 runtime on this host; the missing path is covered by the seam test")
	}
	if version[0] < '0' || version[0] > '9' {
		t.Fatalf("probe answered %q, want the installed runtime's version", version)
	}
}

// TestDesktopLaunchStopsWhenWebView2IsMissing is the defect's end-to-end proof without a
// broken machine: the probe answers "nothing installed", and the launch must log, draw
// exactly one box, start no agent, and return the error main turns into exit 1.
func TestDesktopLaunchStopsWhenWebView2IsMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	cfgPath := desktopTestConfig(t, freeDesktopPort(t))
	cfg, _, err := desktopConfig(cfgPath)
	if err != nil {
		t.Fatalf("desktopConfig: %v", err)
	}

	oldProbe, oldBox, oldSpawn := desktopWebView2Version, desktopBootBox, spawnInstanceServe
	t.Cleanup(func() {
		desktopWebView2Version, desktopBootBox, spawnInstanceServe = oldProbe, oldBox, oldSpawn
	})
	desktopWebView2Version = func() (string, error) { return "", nil }
	var boxes []string
	desktopBootBox = func(title, msg string) { boxes = append(boxes, title+"\n"+msg) }
	spawnInstanceServe = func(string) (instanceProcess, instanceOwner, error) {
		t.Error("a WebView2-less launch started the agent: the gate must stop before ensureInstance")
		return nil, nil, errors.New("unreachable")
	}

	launchErr := desktopLaunch(cfg, cfgPath)
	if launchErr == nil {
		t.Fatal("desktopLaunch returned nil with no WebView2 runtime")
	}
	if !strings.Contains(launchErr.Error(), "WebView2") {
		t.Fatalf("launch error = %q, want it to name WebView2", launchErr)
	}
	if len(boxes) != 1 {
		t.Fatalf("boxes = %d, want exactly 1", len(boxes))
	}
	if !strings.HasPrefix(boxes[0], "omega\n") {
		t.Fatalf("box = %q, want the omega caption first", boxes[0])
	}
	for _, want := range []string{"omega could not open its window.", "WebView2", "go.microsoft.com/fwlink", cfg.Logging.File} {
		if !strings.Contains(boxes[0], want) {
			t.Fatalf("box text is missing %q:\n%s", want, boxes[0])
		}
	}

	body, readErr := os.ReadFile(cfg.Logging.File)
	if readErr != nil {
		t.Fatalf("read the desktop log: %v", readErr)
	}
	for _, want := range []string{"desktop: window boot failed:", "no WebView2 runtime is installed", "the user saw the fallback dialog"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("log is missing %q:\n%s", want, body)
		}
	}
}

// TestDesktopWindowOptionsIsAFramelessNativeHitTestTitleBar pins the window a merged
// title bar depends on: without Frameless the system caption comes back above the
// header, without the region route the page's controls are dead pixels, and turning
// either decoration or resize off costs the Win11 shadow and the edge resize borders.
func TestDesktopWindowOptionsIsAFramelessNativeHitTestTitleBar(t *testing.T) {
	const origin = "http://127.0.0.1:18123"
	opts := desktopWindowOptions(origin)

	if !opts.Frameless {
		t.Error("Frameless is off: the system caption still stacks above omega's header")
	}
	if !opts.Windows.NonClientRegionSupport {
		t.Error("NonClientRegionSupport is off: the page's app-region fallback is unavailable")
	}
	if !opts.Windows.WebView2CompositionHosting {
		t.Error("WebView2CompositionHosting is off: no native hit test for the published regions")
	}
	if opts.Windows.DisableFramelessWindowDecorations {
		t.Error("DisableFramelessWindowDecorations is on: the Win11 shadow and rounded corners go with it")
	}
	if opts.DisableResize {
		t.Error("DisableResize is on: a frameless window then has no resize borders")
	}
	if !opts.AllowSimpleEventEmit {
		t.Error("AllowSimpleEventEmit is off: the caption cells' presses never reach the host")
	}
	for _, event := range desktopWindowEvents {
		if !strings.HasPrefix(event, "omega:window:") {
			t.Errorf("window event %q is not in the omega:window: namespace", event)
		}
	}
	if len(desktopWindowEvents) != 3 {
		t.Errorf("desktopWindowEvents = %v, want the three caption cells", desktopWindowEvents)
	}
	if opts.Title != "omega" {
		t.Errorf("Title = %q, want omega", opts.Title)
	}
	if opts.URL != origin {
		t.Errorf("URL = %q, want %q", opts.URL, origin)
	}
}
