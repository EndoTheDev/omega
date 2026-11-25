package main

// omega desktop: a window over the home's agent. The desktop is a CLIENT - it
// guarantees one instance exists and connects to it; it never runs a second core on
// the home. docs/adr/0010 records why.

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/agent"
)

// ErrNeedsSetup is the desktop's first-run signal: with no config.yaml there is no
// instance to connect to, so the wizard runs in its own console and this launch exits.
var ErrNeedsSetup = errors.New("desktop: no config.yaml - run `omega setup` first")

// instanceHealthBudget bounds the wait for a spawned serve to answer /health. A boot
// that never turns green is a diagnosed failure, never a silent spinner.
const instanceHealthBudget = 15 * time.Second

// instanceProcess is the detached serve handle ensureInstance watches. *os.Process
// satisfies it; tests inject a fake.
type instanceProcess interface {
	Wait() (*os.ProcessState, error)
}

// instanceOwner is the lifetime of a server this launch started. Nil means the desktop
// connected to an instance it does not own, and nothing is ever closed.
type instanceOwner interface {
	release()
}

// spawnInstanceServe starts `omega serve` as a detached background instance and hands
// back the handle that owns it. The test seam: a fake proves the green path spawns
// nothing.
var spawnInstanceServe = func(configPath string) (instanceProcess, instanceOwner, error) {
	return spawnDetachedServe(configPath)
}

// desktopForBareLaunch answers the bare-launch routing. A test seam: the go test binary
// has no console of its own, so the branch must be selectable rather than ambient.
var desktopForBareLaunch = bareLaunchWantsDesktop

// spawnSetupWizard runs the console wizard for a configless home. A test seam: the
// gate is asserted without opening a console window.
var spawnSetupWizard = spawnSetupConsole

// desktopConfig loads the serve config the window will point at. It is the plain home
// config: the window loads the instance's own origin, so there is no desktop bind.
func desktopConfig(configPath string) (Config, string, error) {
	cfgPath := resolveConfigPath(configPath)
	if cfgPath == "" {
		return Config{}, "", ErrNeedsSetup
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		return Config{}, "", err
	}
	if err := resolveHomePaths(&cfg); err != nil {
		return Config{}, "", err
	}
	return cfg, cfgPath, nil
}

// cmdDesktop dispatches the three faces: the window, the headless health probe, and
// the setup gate. The window is Windows-only in 0.19.0; the probe stays portable so
// the release-gate smoke can run anywhere.
func cmdDesktop(args []string) error {
	headless := hasFlag(args, "--headless-health")
	if !headless {
		if err := desktopWindowSupported(); err != nil {
			return err
		}
	}
	configPath := parseConfigFlag(args)
	cfg, cfgPath, err := desktopConfig(configPath)
	if errors.Is(err, ErrNeedsSetup) && !headless {
		// The gate: a first-run home opens the wizard in its own console and this
		// process exits 0. The window opens on the NEXT launch, once the wizard has
		// written config.yaml.
		if serr := spawnSetupWizard(); serr != nil {
			return fmt.Errorf("%w (and the setup wizard did not start: %v)", ErrNeedsSetup, serr)
		}
		return nil
	}
	if err != nil {
		return err
	}
	if headless {
		return desktopHeadless(cfg, cfgPath)
	}
	return desktopLaunch(cfg, cfgPath)
}

// desktopHeadless is the release-gate surface: boot the runtime in THIS process, print
// the /health body, shut down, exit 0. A boot that never turns healthy exits 1.
func desktopHeadless(cfg Config, configPath string) error {
	rt, err := startRuntimeWithin(cfg, configPath, nil, trustFlags{}, 15*time.Second)
	if err != nil {
		return err
	}
	defer rt.shutdown()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(rt.URL + "/health")
	if err != nil {
		return fmt.Errorf("desktop: read %s/health: %w", rt.URL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("desktop: read /health body: %w", err)
	}
	fmt.Print(string(body))
	return nil
}

// instanceOrigin is where the home's agent answers: server.port on the first bind
// (a wildcard bind is probed on loopback).
func instanceOrigin(cfg Config) (string, int) {
	host := "127.0.0.1"
	if len(cfg.Server.Bind) > 0 {
		if b := cfg.Server.Bind[0]; b != "" && b != "0.0.0.0" && b != "::" {
			host = b
		}
	}
	return fmt.Sprintf("http://%s:%d", host, cfg.Server.Port), cfg.Server.Port
}

// ensureInstance returns the origin of a live agent, spawning one detached when
// nothing answers, plus the owner a spawn came with. The desktop never holds the
// home's stores itself.
func ensureInstance(cfg Config, configPath string) (string, instanceOwner, error) {
	origin, port := instanceOrigin(cfg)
	if instanceHealthy(origin) {
		return origin, nil, nil
	}
	proc, owner, err := spawnInstanceServe(configPath)
	if err != nil {
		return "", nil, fmt.Errorf("desktop: start the agent on port %d: %w", port, err)
	}
	deadline := time.Now().Add(instanceHealthBudget)
	exited := make(chan error, 1)
	go func() {
		st, werr := proc.Wait()
		if werr == nil && st != nil && !st.Success() {
			werr = fmt.Errorf("exit status %d", st.ExitCode())
		}
		exited <- werr
	}()
	for {
		if instanceHealthy(origin) {
			return origin, owner, nil
		}
		select {
		case werr := <-exited:
			return "", nil, fmt.Errorf("desktop: the agent exited before port %d answered (%v)%s", port, werr, portDiagnosis(port))
		default:
		}
		if time.Now().After(deadline) {
			return "", nil, fmt.Errorf("desktop: no agent answered on port %d within %s%s", port, instanceHealthBudget, portDiagnosis(port))
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// instanceHealthy reports whether an omega instance answers /health on origin.
func instanceHealthy(origin string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	body, err := healthBody(client, origin)
	return err == nil && strings.Contains(body, `"status":"ok"`)
}

// portDiagnosis names a foreign listener. If the port cannot be bound, somebody else
// holds it - a dial test lies when a stuck listener stops accepting.
func portDiagnosis(port int) string {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err == nil {
		_ = ln.Close()
		return ""
	}
	return fmt.Sprintf(" - port %d is held by another process (server.port must be free for the agent)", port)
}

// hasFlag reports whether args carries the exact flag.
func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name || strings.HasPrefix(a, name+"=") {
			return true
		}
	}
	return false
}

// desktopWebView2Help is the actionable half of the fallback text: the box names the
// cause, then the one thing a user can do about it.
const desktopWebView2Help = "The Microsoft Edge WebView2 Runtime is missing or failed to start. " +
	"Install it from https://go.microsoft.com/fwlink/p/?LinkId=2124703 and start omega again."

// desktopBootErrors owns the fallback dialog for a window that never came up. The first
// error before the window is on screen writes the log line and shows the box; every
// later error is a log entry only. Wails reports runtime WebView2 errors - a non-fatal
// rasterization complaint included - through the same callback, so the gate is the
// window's own show event, not the error's shape.
type desktopBootErrors struct {
	logf    agent.LoggerProvider
	logPath string
	box     func(title, msg string)

	mu   sync.Mutex
	up   bool
	seen bool
}

// newDesktopBootErrors takes no Wails type and starts no app: the whole decision is a
// value a test can drive without a window, a WebView2 runtime or an OS.
func newDesktopBootErrors(logf agent.LoggerProvider, logPath string, box func(title, msg string)) *desktopBootErrors {
	return &desktopBootErrors{logf: logf, logPath: logPath, box: box}
}

// Message is the exact text of the box: what failed, the WebView2 cause, what to install,
// and where the record is.
func (b *desktopBootErrors) Message(cause error) string {
	text := "unknown error"
	if cause != nil {
		text = cause.Error()
	}
	details := b.logPath
	if details == "" {
		details = "no log file (logging is disabled)"
	}
	return fmt.Sprintf("omega could not open its window.\n\nWebView2: %s\n\n%s\n\nDetails: %s", text, desktopWebView2Help, details)
}

// Handle is the Wails error callback. The log line lands before the box, so a user who
// leaves the dialog open still leaves a record; a disabled logger is silent and the box
// still shows.
func (b *desktopBootErrors) Handle(err error) {
	if err == nil {
		return
	}
	b.mu.Lock()
	if b.up {
		b.mu.Unlock()
		b.line("desktop: window error after boot: %v; no fallback dialog, the window is up", err)
		return
	}
	if b.seen {
		b.mu.Unlock()
		b.line("desktop: window boot failed again: %v; the fallback dialog already showed", err)
		return
	}
	b.seen = true
	b.mu.Unlock()

	b.line("desktop: window boot failed: %v; the user saw the fallback dialog", err)
	b.box("omega", b.Message(err))
}

// WindowUp records that the window reached the screen: from here on an error is a
// running app's complaint, never a boot failure, and it must not stack a box over a
// usable window.
func (b *desktopBootErrors) WindowUp() {
	b.mu.Lock()
	b.up = true
	b.mu.Unlock()
}

// line writes one log entry. The box never waits on logging: a nil logger skips the
// write and the dialog still shows.
func (b *desktopBootErrors) line(format string, args ...any) {
	if b.logf == nil {
		return
	}
	b.logf.Errorf(format, args...)
}
