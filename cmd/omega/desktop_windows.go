//go:build windows

package main

// The Windows desktop shell: a Wails v3 window over the home's agent. The window is a
// client - the agent is a detached background instance (docs/adr/0010).

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

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
	app = application.New(application.Options{
		Name:        "omega",
		Description: "omega desktop",
		// Wails' own fatal paths (a window that cannot be created, a service failure)
		// land here; the WebView2 init failure is caught by the runtime gate above.
		ErrorHandler: bootErrors.Handle,
		Services: []application.Service{
			application.NewService(notifier),
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

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "omega",
		URL:    origin,
		Width:  1280,
		Height: 860,
	})

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
	rows := make([]menuRow, 0, 3)
	for _, entry := range shell.menu() {
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

	// The popup is a native snapshot and Wails exposes no will-open hook, so every
	// checkable row is re-read in the click path that opens it. A registration changed
	// outside the tray (regedit, an installer) then shows the truth.
	tray.OnRightClick(func() {
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
