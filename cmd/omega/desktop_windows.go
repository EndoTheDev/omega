//go:build windows

package main

// The Windows desktop shell: a Wails v3 window over the home's agent. The window is a
// client - the agent is a detached background instance (docs/adr/0010).

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/observability"
)

// desktopTrayIcon is the tray glyph: a dark disc carrying a white omega, so it reads on
// both light and dark taskbars.
//
//go:embed assets/omega.ico
var desktopTrayIcon []byte

// desktopSingleInstanceID is the one Wails instance lock: a second `omega desktop`
// wakes the first window instead of starting a second shell.
const desktopSingleInstanceID = "dev.omega.desktop"

// desktopWindowSupported: the window is the 0.19.0 desktop surface.
func desktopWindowSupported() error { return nil }

// bareLaunchWantsDesktop reports a shell launch: no parent console to attach to. A
// terminal launch attached at startup and keeps the CLI loop; the console itself is the
// discriminator, so no process or file-manager list is involved.
func bareLaunchWantsDesktop() bool {
	return !attachCallerConsole()
}

// desktopLaunch connects the one window to the home's agent, starting a detached
// instance when none answers. application.New owns the single-instance lock, so a
// second launch notifies the first and exits from inside New.
func desktopLaunch(cfg Config, configPath string) error {
	// The fallback dialog's record: Wails discards its own logger, so the boot failure
	// is written here or nowhere. An unopenable file still draws the box.
	logger, logErr := observability.NewLogger(observability.Config{Enabled: cfg.Logging.Enabled, File: cfg.Logging.File})
	if logErr != nil {
		logger = observability.NopLogger{}
	}
	defer logger.Close()
	bootErrors := newDesktopBootErrors(logger, cfg.Logging.File, desktopBootBox)

	// The runtime is checked before the agent and the window exist. Wails installs its
	// error handler on the WebView2 instance and then resets it from that instance's
	// Close before the failure callback runs (internal/webview2 Chromium.Embed ->
	// EmbedWithError defer Close -> ShuttingDown), so a missing runtime exits the process
	// with the handler never called. The gate is the only surface that survives that
	// path, and stopping here also keeps a healthy agent from being spawned only to die.
	version, probeErr := desktopWebView2Version()
	if probeErr != nil {
		// An unreadable registry is not a missing runtime: let Wails try.
		logger.Warnf("desktop: check the WebView2 runtime: %v", probeErr)
	}
	if probeErr == nil && version == "" {
		bootErr := errors.New("no WebView2 runtime is installed (Software\\Microsoft\\EdgeUpdate\\ClientState)")
		bootErrors.Handle(bootErr)
		return fmt.Errorf("desktop: %w", bootErr)
	}

	notifier := notifications.New()
	// A second launch restores this one; a hidden window needs Show, not just Restore.
	var shell *desktopShell
	var app *application.App
	// The page -> host door: the app's own surfaces answer raw JSON, never a server's
	// route. The handler is installed at New and bound to the window below, so a message
	// that arrives before the window exists is dropped rather than misrouted.
	var host *desktopHost
	app = application.New(application.Options{
		Name:        "omega",
		Description: "omega desktop",
		// Wails' own fatal paths (a window that cannot be created, a service failure)
		// land here; the WebView2 init failure is caught by the runtime gate above.
		ErrorHandler: bootErrors.Handle,
		Services: []application.Service{
			application.NewService(notifier),
		},
		RawMessageHandler: func(_ application.Window, message string, info *application.OriginInfo) {
			if host == nil {
				return
			}
			if err := host.Handle(info, message); err != nil {
				logger.Warnf("desktop: dropped a page message: %v", err)
			}
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: desktopSingleInstanceID,
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if shell != nil {
					shell.openWindow()
				}
			},
		},
	})

	// The owner is this process's job handle when the launch spawned the agent, and nil
	// when it connected to one already running. It is held for the whole run: the close
	// on process exit is the only thing that kills an owned agent.
	origin, owner, err := ensureInstance(cfg, configPath)
	if err != nil {
		// The failure is diagnosed: a held port is named, never a silent spinner.
		showBootError(err.Error())
		return err
	}

	// One dialog for both directions: a registration the app cannot read or write says
	// so, and the row re-reads status afterwards instead of guessing.
	autostart := newAutostartControl(wailsAutostart{manager: app.Autostart}, func(err error) {
		app.Dialog.Error().SetTitle("omega").SetMessage("Start with Windows: " + err.Error()).Show()
	})

	win := app.Window.NewWithOptions(desktopWindowOptions(origin))

	// The app's own surfaces, bound to the window they drive: the list (stored in THIS
	// home's config.yaml, whatever server the page came from), the origin the window is
	// showing, and the frame state. omegaSettings is the one validator and writer.
	host = newDesktopHost(origin, &omegaSettings{path: configPath})
	host.emit = func(name string, payload any) { app.Event.Emit(name, payload) }
	host.setFrameless = func(frameless bool) { win.SetFrameless(frameless) }
	host.navigate = func(url string) { win.SetURL(url) }
	// Every load gets its chance to announce the page's own chrome; a silent page on
	// another origin hands the caption back to the OS.
	win.RegisterHook(events.Windows.WebViewNavigationCompleted, func(*application.WindowEvent) {
		host.beginNavigation()
	})

	// The caption cells' presses come back here: the hit test routes them to the page,
	// so the page names the action and the window performs it.
	for _, event := range desktopWindowEvents {
		app.Event.On(event, func(*application.CustomEvent) {
			desktopWindowAction(win, event)
		})
	}

	// The tray's update row talks to the home's own engine - the desktop adds an
	// affordance, never a second updater.
	updates := newTrayUpdates(origin,
		func(title, body string) {
			application.InvokeSync(func() {
				app.Dialog.Info().SetTitle(title).SetMessage(body).Show()
			})
		},
		func(title, body string, yes func()) {
			application.InvokeSync(func() {
				dialog := app.Dialog.Question()
				dialog.SetTitle(title)
				dialog.SetMessage(body)
				dialog.AddButtons([]*application.Button{
					{Label: "Restart now", IsDefault: true, Callback: yes},
					{Label: "Later", IsCancel: true},
				})
				dialog.Show()
			})
		})

	shell = newDesktopShell(
		func() { win.Show(); win.Restore(); win.Focus() },
		func() { win.Hide() },
		func() { app.Quit() },
		autostart,
	)
	shell.setUpdates(updates)
	// The way back is the app's too: the tray's Connection rows come from the app-local
	// list, not from whatever page the window is showing, and home is the origin this
	// launch resolved - the one row no list and no page can take away.
	shell.setInstances(host.instances, host.ActiveOrigin, host.SwitchTo)
	shell.setHome(host.HomeOrigin)
	win.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		shell.windowClosing(event.Cancel)
	})
	// The boot gate: an error after this event is a running app's complaint, not a
	// window that failed to open.
	win.RegisterHook(events.Common.WindowShow, func(*application.WindowEvent) {
		bootErrors.WindowUp()
	})

	tray := app.SystemTray.New()
	tray.SetIcon(desktopTrayIcon)
	tray.SetTooltip("omega")
	tray.OnClick(shell.openWindow)
	menu := app.Menu.New()
	type menuRow struct {
		entry desktopMenuEntry
		item  *application.MenuItem
	}
	rows := make([]menuRow, 0, 4)
	var connection *application.Menu
	for _, entry := range shell.menu() {
		// A submenu is filled where the popup is drawn (fillConnections below): its rows
		// are live data, not the snapshot a startup build would freeze.
		if entry.Children != nil {
			connection = menu.AddSubmenu(entry.Label)
			continue
		}
		var item *application.MenuItem
		if entry.Checked == nil {
			item = menu.Add(entry.Label)
		} else {
			item = menu.AddCheckbox(entry.Label, entry.Checked())
		}
		item.OnClick(func(*application.Context) {
			entry.Action()
			if entry.Checked == nil {
				return
			}
			// The native row flipped itself on the click; the registration decides.
			checked := entry.Checked()
			application.InvokeSync(func() { item.SetChecked(checked) })
		})
		rows = append(rows, menuRow{entry: entry, item: item})
	}
	tray.SetMenu(menu)

	// Every app-local row, the window's own origin checked, one click to switch. Read at
	// open, so a row the pane saved a moment ago is already there and the check follows a
	// switch the page made.
	fillConnections := func() {
		if connection == nil {
			return
		}
		connection.Clear()
		for _, child := range shell.connectionRows() {
			row := child
			var item *application.MenuItem
			if row.Checked == nil {
				item = connection.Add(row.Label)
			} else {
				item = connection.AddCheckbox(row.Label, row.Checked())
			}
			item.OnClick(func(*application.Context) {
				if row.Action != nil {
					row.Action()
				}
			})
		}
		// The tray's popup is its own native snapshot, so a row added to this tree stays
		// invisible until the popup is rebuilt from it. Update is that lever.
		application.InvokeSync(func() { menu.Update() })
	}

	// The popup is a native snapshot and Wails exposes no will-open hook, so every
	// checkable row is re-read in the click path that opens it. A registration changed
	// outside the tray (regedit, an installer) then shows the truth.
	tray.OnRightClick(func() {
		fillConnections()
		for _, row := range rows {
			if row.entry.Checked != nil {
				row.item.SetChecked(row.entry.Checked())
			}
		}
		tray.OpenMenu()
	})

	// A toast click asks for the window: the same action as the "Open omega" item.
	notifier.OnNotificationResponse(func(notifications.NotificationResult) { shell.openWindow() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go newNoticeConsumer(origin, wailsToastSink{svc: notifier}).Run(ctx)

	// A spawned agent dies with this process: its job handle closes on exit. An agent
	// this launch found is never touched, and closing the window only hides it.
	runErr := app.Run()
	runtime.KeepAlive(owner)
	// A window that never came up usually exits inside Run; this covers a run that
	// returns cleanly with the window still down, and the boot gate turns the usual case
	// into a second log line only.
	bootErrors.Handle(runErr)
	return runErr
}

// desktopWindowOptions is the window the desktop opens: omega's own header IS the
// title bar. Frameless hands the whole window rect to the client, so the system caption
// no longer stacks above the header; the page then publishes hit-test regions
// (wails:non-client-region) and the composition-hosted controller answers WM_NCHITTEST
// from them. That route is also what restores resize borders, the caption system menu
// and the double-click maximise. Decorations stay on for the Win11 shadow and rounded
// corners; resizing stays enabled.
//
// AllowSimpleEventEmit is the caption cells' return path: a press on a minimize /
// maximize / close region is forwarded to the page as mouse input instead of being
// performed, so the page sends the bare event name back (see desktopWindowEvents). The
// shortcut carries names only and cannot reach the binding path, but a script in this
// window could trigger those three handlers - window chrome, on the same loopback
// channel the whole app already trusts.
func desktopWindowOptions(origin string) application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Title:                "omega",
		URL:                  origin,
		Width:                1280,
		Height:               860,
		Frameless:            true,
		AllowSimpleEventEmit: true,
		Windows: application.WindowsWindow{
			NonClientRegionSupport:     true,
			WebView2CompositionHosting: true,
		},
	}
}

// The caption cells' page -> host names. The page's WindowControls sends the bare name
// (wails:event:emit:<name>); desktopWindowAction performs it.
const (
	desktopWindowEventMinimise       = "omega:window:minimise"
	desktopWindowEventToggleMaximise = "omega:window:toggle-maximise"
	desktopWindowEventClose          = "omega:window:close"
)

var desktopWindowEvents = []string{
	desktopWindowEventMinimise,
	desktopWindowEventToggleMaximise,
	desktopWindowEventClose,
}

// desktopWindowAction performs one caption-cell press on the window. Close rides the
// existing WindowClosing hook, so the close cell still hides to the tray.
func desktopWindowAction(win *application.WebviewWindow, event string) {
	switch event {
	case desktopWindowEventMinimise:
		win.Minimise()
	case desktopWindowEventToggleMaximise:
		win.ToggleMaximise()
	case desktopWindowEventClose:
		win.Close()
	}
}

// The app-local surfaces the page asks for over the raw message door. Each body is one
// JSON object whose `omega` member names it; anything that is not one of these is
// dropped, never guessed at.
const (
	desktopMessageGetInstances    = "instances:get"
	desktopMessageSaveInstances   = "instances:save"
	desktopMessageSwitchInstance  = "instances:switch"
	desktopMessageChromeReady     = "chrome:ready"
	desktopInstancesEvent         = "omega:instances"
	desktopChromeReadyBoundWindow = 5 * time.Second
)

// desktopInstancesReply is the answer to every instance request: the app's list and the
// refusal when a write was declined. The pane renders the words; the bar repaints.
//
// `home` is the desktop's own local origin, the one place this window was launched on. It
// is shell-side state, not page-callable: home is resolved from the launch config, held by
// the host, and read by the tray from the shell's own store. No page message can set,
// retarget, hide or redefine it - the message door admits four bodies and none of them is
// home, a switch moves the active origin only, and the tray is app-level, so a loaded page
// has no channel to it at all. The field is a READ on the answer the shell already sends
// that page; it widens nothing beyond the connection list that answer already carries, and
// a page that ignores it loses only its own badge, never the route.
type desktopInstancesReply struct {
	Instances []core.SavedInstance `json:"instances"`
	Error     string               `json:"error"`
	Home      string               `json:"home"`
}

// desktopRequest is one decoded message body.
type desktopRequest struct {
	kind      string
	instances []core.SavedInstance
	url       string
}

// decodeDesktopRequest parses one page message. Malformed JSON and an unknown `omega`
// member are refusals carried back to the caller's log, never a silent no-op.
func decodeDesktopRequest(raw string) (desktopRequest, error) {
	var body struct {
		Omega     string               `json:"omega"`
		Instances []core.SavedInstance `json:"instances"`
		URL       string               `json:"url"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return desktopRequest{}, fmt.Errorf("not a desktop message: %v", err)
	}
	switch body.Omega {
	case desktopMessageGetInstances, desktopMessageChromeReady:
		return desktopRequest{kind: body.Omega}, nil
	case desktopMessageSaveInstances:
		return desktopRequest{kind: body.Omega, instances: body.Instances}, nil
	case desktopMessageSwitchInstance:
		if strings.TrimSpace(body.URL) == "" {
			return desktopRequest{}, errors.New("instances:switch carries no url")
		}
		return desktopRequest{kind: body.Omega, url: body.URL}, nil
	case "":
		return desktopRequest{}, errors.New("not a desktop message: no omega member")
	default:
		return desktopRequest{}, fmt.Errorf("unknown desktop message %q", body.Omega)
	}
}

// desktopFrameDecision is what the host does about the caption at the end of a
// navigation's bound.
type desktopFrameDecision int

const (
	// desktopFrameKeep: the page carries omega's own title bar, so the frame stays off.
	desktopFrameKeep desktopFrameDecision = iota
	// desktopFrameRestore: nothing announced a title bar on a foreign page, so the OS
	// draws the caption and the three cells exist again.
	desktopFrameRestore
)

// desktopFrameOf decides the frame at the end of a navigation's bound. A page that
// announced its chrome keeps the frameless frame; a silent page keeps it too when it is
// the app's own origin, because a cold start must never flash a native bar under omega's
// header.
func desktopFrameOf(chromeReady bool, activeOrigin, appOrigin string) desktopFrameDecision {
	if chromeReady {
		return desktopFrameKeep
	}
	if desktopOriginKey(activeOrigin) == desktopOriginKey(appOrigin) {
		return desktopFrameKeep
	}
	return desktopFrameRestore
}

// desktopChromeBoundWindow bounds how long a freshly loaded page gets to announce its own
// chrome. A test seam: a test must not wait on a wall clock.
var desktopChromeBoundWindow = func(d time.Duration, fn func()) *time.Timer {
	return time.AfterFunc(d, fn)
}

// desktopHost is the app behind the window: the connection list, the origin the window
// shows, and the frame state. It takes no Wails type in its decisions, so the whole
// surface is assertable without a window.
type desktopHost struct {
	mu           sync.Mutex
	appOrigin    string
	activeOrigin string
	chromeReady  bool
	bound        *time.Timer

	instances    desktopInstanceStore
	emit         func(name string, payload any)
	setFrameless func(bool)
	navigate     func(string)
}

func newDesktopHost(appOrigin string, store desktopInstanceStore) *desktopHost {
	return &desktopHost{appOrigin: appOrigin, activeOrigin: appOrigin, instances: store}
}

// ActiveOrigin is the origin the window is showing: the tray's check mark and the frame
// rule both read it.
func (h *desktopHost) ActiveOrigin() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.activeOrigin
}

// HomeOrigin is the origin the window was launched on. It never moves - a switch rewrites
// the active origin only - and it is the ONE holder of the value: the tray's home row and
// the instances answer's `home` member both read it here.
func (h *desktopHost) HomeOrigin() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.appOrigin
}

func (h *desktopHost) setActiveOrigin(origin string) {
	h.mu.Lock()
	h.activeOrigin = origin
	h.mu.Unlock()
}

// SwitchTo shows url in the window. The host records it first, so the check mark and the
// frame rule already know the page the load is about to produce.
func (h *desktopHost) SwitchTo(url string) {
	h.switchTo(url)
}

// accepts is the trust gate on the door: only the window's own top document may ask, and
// a child frame is a frame whose own source differs from that document's. Windows reports
// no main-frame bit, so the two sources ARE the main-frame test there; a platform that
// reports one is honoured.
func (h *desktopHost) accepts(info *application.OriginInfo) bool {
	if info == nil {
		return false
	}
	top := desktopOriginKey(info.TopOrigin)
	if top == "" || top != desktopOriginKey(h.ActiveOrigin()) {
		return false
	}
	if !info.IsMainFrame && info.Origin != "" && desktopOriginKey(info.Origin) != top {
		return false
	}
	return true
}

// Handle routes one raw page message. The error is the caller's log line, never the page's
// answer: an accepted request answers on the event.
func (h *desktopHost) Handle(info *application.OriginInfo, raw string) error {
	if !h.accepts(info) {
		return fmt.Errorf("not the window's own page (%q over %q, showing %q)",
			originOf(info), topOriginOf(info), h.ActiveOrigin())
	}
	request, err := decodeDesktopRequest(raw)
	if err != nil {
		return err
	}
	switch request.kind {
	case desktopMessageGetInstances:
		h.answerInstances("")
	case desktopMessageSaveInstances:
		h.saveInstances(request.instances)
	case desktopMessageSwitchInstance:
		h.switchTo(request.url)
	case desktopMessageChromeReady:
		h.chromeArmed()
	}
	return nil
}

// answerInstances emits the app's list, with a refusal in place of an empty error when a
// write was declined.
func (h *desktopHost) answerInstances(refusal string) {
	list, err := h.instances.Instances()
	if err != nil && refusal == "" {
		refusal = err.Error()
	}
	h.emit(desktopInstancesEvent, h.instancesReply(list, refusal))
}

// instancesReply is the one shape of every instance answer, so a read, a save and a
// refusal all carry the same list, the same words and the same home.
func (h *desktopHost) instancesReply(list []core.SavedInstance, refusal string) desktopInstancesReply {
	if list == nil {
		list = []core.SavedInstance{}
	}
	return desktopInstancesReply{Instances: list, Error: refusal, Home: h.HomeOrigin()}
}

// saveInstances validates through core.NormalizeInstances and writes through the one
// atomic writer. A refusal answers with the host's words and the list unchanged.
func (h *desktopHost) saveInstances(rows []core.SavedInstance) {
	list, err := core.NormalizeInstances(rows)
	if err != nil {
		h.answerInstances(err.Error())
		return
	}
	if err := h.instances.SaveInstances(list); err != nil {
		h.answerInstances(err.Error())
		return
	}
	h.emit(desktopInstancesEvent, h.instancesReply(list, ""))
}

// switchTo navigates the WINDOW: every switch inside the desktop is the host's, so the
// active origin is never a guess. A browser tab keeps navigating itself.
func (h *desktopHost) switchTo(raw string) {
	rows, err := core.NormalizeInstances([]core.SavedInstance{{Name: "switch", URL: raw}})
	if err != nil {
		h.answerInstances(err.Error())
		return
	}
	target := rows[0].URL
	h.setActiveOrigin(target)
	h.navigate(target)
}

// beginNavigation arms the chrome bound for the page that just loaded.
func (h *desktopHost) beginNavigation() {
	h.mu.Lock()
	if h.bound != nil {
		h.bound.Stop()
	}
	h.chromeReady = false
	h.bound = desktopChromeBoundWindow(desktopChromeReadyBoundWindow, h.expireChromeBound)
	h.mu.Unlock()
}

// expireChromeBound is what the bound runs: a silent page on another origin gets the OS
// caption. It is a method rather than a closure so the test drives it without a clock.
func (h *desktopHost) expireChromeBound() {
	h.mu.Lock()
	ready := h.chromeReady
	active := h.activeOrigin
	h.bound = nil
	h.mu.Unlock()
	if desktopFrameOf(ready, active, h.appOrigin) == desktopFrameRestore {
		h.setFrameless(false)
	}
}

// chromeArmed answers the page's announcement: the frame goes frameless (already is, on a
// cold start) and the bound stands down. A late arm after the OS took the caption puts
// omega's own title bar back.
func (h *desktopHost) chromeArmed() {
	h.mu.Lock()
	h.chromeReady = true
	if h.bound != nil {
		h.bound.Stop()
		h.bound = nil
	}
	h.mu.Unlock()
	h.setFrameless(true)
}

// originOf / topOriginOf keep the refusal message honest when a nil record arrives.
func originOf(info *application.OriginInfo) string {
	if info == nil {
		return ""
	}
	return info.Origin
}

func topOriginOf(info *application.OriginInfo) string {
	if info == nil {
		return ""
	}
	return info.TopOrigin
}

// wailsAutostart is the production autostartBackend: Wails v3 owns the registration
// (the Run key on Windows), so omega never hand-rolls one.
type wailsAutostart struct {
	manager *application.AutostartManager
}

// Status reports whether a registration points at THIS binary.
func (w wailsAutostart) Status() (bool, error) {
	st, err := w.manager.Status()
	if err != nil {
		return false, err
	}
	return st.Enabled, nil
}

// Enable pins the value name and carries the caller's launch arguments.
func (w wailsAutostart) Enable(args []string) error {
	return w.manager.EnableWithOptions(application.AutostartOptions{
		Identifier: autostartIdentifier,
		Arguments:  args,
	})
}

// Disable removes the registration; Wails no-ops when there is none.
func (w wailsAutostart) Disable() error { return w.manager.Disable() }

// wailsToastSink is the native toast surface behind the consumer.
type wailsToastSink struct {
	svc *notifications.NotificationService
}

func (s wailsToastSink) SendToast(id, title, body string) error {
	return s.svc.SendNotification(notifications.NotificationOptions{ID: id, Title: title, Body: body})
}

// spawnDetachedServe starts `omega serve` from this binary as a console-detached
// background process and ties it to this process's job object: the child gets no
// console, and it dies when the desktop does.
func spawnDetachedServe(configPath string) (instanceProcess, instanceOwner, error) {
	job, err := newInstanceJob()
	if err != nil {
		return nil, nil, err
	}
	proc, err := startDetachedServe(configPath)
	if err != nil {
		return nil, nil, err
	}
	if err := job.assign(proc); err != nil {
		return nil, nil, fmt.Errorf("the agent did not join the desktop's job, so it would outlive the desktop: %w", err)
	}
	return proc, job, nil
}

// startDetachedServe starts `omega serve` from this binary with no console window and
// no parent-death tie. The updater's restart uses it too: that child must outlive the
// process that spawned it.
func startDetachedServe(configPath string) (*os.Process, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	args := []string{"serve"}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd.Process, nil
}

// desktopWebView2Version is the runtime probe. A test seam: the missing-runtime gate is
// asserted on a host whose WebView2 works.
var desktopWebView2Version = webView2RuntimeVersion

// webView2ClientStateKeys are the Evergreen runtime's registration keys in the loader's
// channel order - stable, beta, dev, canary.
var webView2ClientStateKeys = []string{
	`{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`,
	`{2CD8A007-E189-409D-A2C8-9AF4EF3C72AA}`,
	`{0D50BFEC-CD6A-4F9A-964C-C7416E3ACB10}`,
	`{65C35B14-6C1D-4122-AC46-7148CC9D6497}`,
}

// webView2RuntimeVersion is the same lookup the WebView2 loader makes: an install writes
// an EBWebView folder naming its version under one of the client-state keys. An empty
// version with no error is the "nobody is home" answer, which Wails cannot survive. The
// loader's fixed-version override is honoured too, so a developer pointing at a local
// runtime is never told the runtime is missing.
func webView2RuntimeVersion() (string, error) {
	if dir := os.Getenv("WEBVIEW2_BROWSER_EXECUTABLE_FOLDER"); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return "fixed-version runtime at " + dir, nil
		}
	}
	for _, uuid := range webView2ClientStateKeys {
		for _, hive := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
			version, err := webView2ClientStateVersion(hive, uuid)
			if err != nil {
				return "", err
			}
			if version != "" {
				return version, nil
			}
		}
	}
	return "", nil
}

func webView2ClientStateVersion(hive registry.Key, uuid string) (string, error) {
	key, err := registry.OpenKey(hive, `Software\Microsoft\EdgeUpdate\ClientState\`+uuid, registry.READ|registry.WOW64_32KEY)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	defer key.Close()
	folder, _, err := key.GetStringValue("EBWebView")
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	if folder == "" {
		return "", nil
	}
	return filepath.Base(folder), nil
}

// desktopBootBox is the surface the fallback dialog draws on; the native box hardcodes
// the "omega" caption. A test seam: a native box would block a headless test.
var desktopBootBox = func(_ string, msg string) { showBootError(msg) }

// showBootError names a pre-Run boot failure in a native message box. The Wails
// dialog manager refuses to draw before app.Run, and a boot failure is exactly that
// state, so the box is the native one.
func showBootError(msg string) {
	text, terr := windows.UTF16PtrFromString(msg)
	caption, cerr := windows.UTF16PtrFromString("omega")
	if terr != nil || cerr != nil {
		return
	}
	_, _ = windows.MessageBox(0, text, caption, windows.MB_OK|windows.MB_ICONERROR)
}

// spawnSetupConsole runs the reused setup wizard in its own console, so a first-run
// desktop launch is not a dead end. The parent exits immediately; the next launch
// finds config.yaml and opens the window.
func spawnSetupConsole() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "setup")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE}
	// A console-attached parent can hand the wizard the live console; a windowless
	// parent (Explorer launch) cannot, and the child then reads the null device.
	if in, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		defer in.Close()
		cmd.Stdin = in
		if out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
			defer out.Close()
			cmd.Stdout, cmd.Stderr = out, out
		}
	}
	return cmd.Start()
}
