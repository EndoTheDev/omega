//go:build windows

package main

// The Windows desktop shell: a Wails v3 window over the home's agent. The window is a
// client - the agent is a detached background instance (docs/adr/0010).

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"
)

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
	var app *application.App
	app = application.New(application.Options{
		Name:        "omega",
		Description: "omega desktop",
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: desktopSingleInstanceID,
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if win := app.Window.Current(); win != nil {
					win.Restore()
					win.Focus()
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

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "omega",
		URL:    origin,
		Width:  1280,
		Height: 860,
	})
	// Quitting the shell never kills the agent: the instance is detached, and the next
	// launch connects to the one already running.
	return app.Run()
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
