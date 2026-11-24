//go:build windows

package main

// The Windows desktop shell: a Wails v3 window over the in-process core.

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"
)

// desktopSingleInstanceID is the one Wails instance lock: a second `omega desktop`
// wakes the first instead of booting a second core.
const desktopSingleInstanceID = "dev.omega.desktop"

// desktopWindowSupported: the window is the 0.19.0 desktop surface.
func desktopWindowSupported() error { return nil }

// desktopLaunch serves the core, then runs one window at it. application.New owns the
// single-instance lock, so a second launch notifies the first and exits from inside
// New - before this process ever binds the desktop port.
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

	rt, err := startRuntime(cfg, configPath, nil, trustFlags{})
	if err != nil {
		// The one loud port failure: the dialog names the port, and the process exits 1.
		// There is no fallback port - a new origin would reset the webui's localStorage.
		app.Dialog.Error().SetTitle("omega").SetMessage(err.Error()).Show()
		return err
	}

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "omega",
		URL:    rt.URL,
		Width:  1280,
		Height: 860,
	})
	runErr := app.Run()
	rt.shutdown()
	return runErr
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
