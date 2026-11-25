//go:build windows

package main

// The Windows desktop shell: a Wails v3 window over the home's agent. The window is a
// client - the agent is a detached background instance (docs/adr/0010).

import (
	"context"
	_ "embed"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
	"golang.org/x/sys/windows"
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

// bareLaunchWantsDesktop reports a shell launch - a double-click or a shortcut. A
// console check cannot separate the cases: Windows gives a shell-launched console exe a
// console of its own, and an agent/script shell launches children with none. The parent
// image is the discriminator, so a script, service or terminal never lands in a GUI.
func bareLaunchWantsDesktop() bool {
	return strings.EqualFold(parentProcessImage(), shellParentImage)
}

// shellParentImage is the Explorer image name: every double-click and shortcut launch
// arrives from it.
const shellParentImage = "explorer.exe"

// parentProcessImage returns the parent process's executable name, or "".
func parentProcessImage() string {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(snap)
	rows := make(map[uint32]struct {
		parent uint32
		image  string
	})
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err := windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		rows[entry.ProcessID] = struct {
			parent uint32
			image  string
		}{entry.ParentProcessID, windows.UTF16ToString(entry.ExeFile[:])}
	}
	if me, ok := rows[uint32(os.Getpid())]; ok {
		return rows[me.parent].image
	}
	return ""
}

// desktopLaunch connects the one window to the home's agent, starting a detached
// instance when none answers. application.New owns the single-instance lock, so a
// second launch notifies the first and exits from inside New.
func desktopLaunch(cfg Config, configPath string) error {
	notifier := notifications.New()
	// A second launch restores this one; a hidden window needs Show, not just Restore.
	var shell *desktopShell
	var app *application.App
	app = application.New(application.Options{
		Name:        "omega",
		Description: "omega desktop",
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

	origin, err := ensureInstance(cfg, configPath)
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

	// Quitting the shell never kills the agent: the instance is detached, and the next
	// launch connects to the one already running.
	return app.Run()
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

// spawnDetachedServe starts `omega serve` from this binary as a detached background
// process: no console window, no inherited handles, no parent-death tie.
func spawnDetachedServe(configPath string) (instanceProcess, error) {
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
