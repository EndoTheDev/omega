package main

// desktop_tray_test.go: the tray's semantics - close hides, quit exits, and the menu is
// the three rows the shell promises, one of them checkable.

import "testing"

// trayProbe counts what the shell did through the Wails seams.
type trayProbe struct {
	shown, hidden, quit int
}

func newTrayShell() (*desktopShell, *trayProbe) {
	shell, probe, _, _ := newTrayShellWithAutostart()
	return shell, probe
}

func newTrayShellWithAutostart() (*desktopShell, *trayProbe, *fakeAutostart, *reportLog) {
	p := &trayProbe{}
	backend := &fakeAutostart{}
	log := &reportLog{}
	shell := newDesktopShell(
		func() { p.shown++ },
		func() { p.hidden++ },
		func() { p.quit++ },
		newAutostartControl(backend, log.report),
	)
	return shell, p, backend, log
}

func TestDesktopShellCloseHidesToTray(t *testing.T) {
	shell, probe := newTrayShell()

	cancelled := false
	shell.windowClosing(func() { cancelled = true })

	if !cancelled {
		t.Fatal("a plain window close was not cancelled: the window would have been destroyed")
	}
	if probe.hidden != 1 {
		t.Fatalf("hides = %d, want 1", probe.hidden)
	}
	if probe.quit != 0 {
		t.Fatalf("a window close quit the shell (quit count %d)", probe.quit)
	}
}

func TestDesktopShellQuitLetsCloseThrough(t *testing.T) {
	shell, probe := newTrayShell()

	shell.quitApp()
	if probe.quit != 1 {
		t.Fatalf("quit calls = %d, want 1", probe.quit)
	}

	cancelled := false
	shell.windowClosing(func() { cancelled = true })
	if cancelled {
		t.Fatal("the close after Quit was cancelled: the window would survive the quit")
	}
	if probe.hidden != 0 {
		t.Fatalf("the close after Quit hid the window (hide count %d)", probe.hidden)
	}
}

func TestDesktopTrayMenuShape(t *testing.T) {
	shell, probe := newTrayShell()

	menu := shell.menu()
	want := []string{"Open omega", autostartItemLabel, "Quit"}
	if len(menu) != len(want) {
		t.Fatalf("menu has %d items, want %d: %+v", len(menu), len(want), menu)
	}
	for i, label := range want {
		if menu[i].Label != label {
			t.Fatalf("menu[%d] = %q, want %q", i, menu[i].Label, label)
		}
	}

	checkable := 0
	for i, entry := range menu {
		if entry.Checked == nil {
			continue
		}
		checkable++
		if i != 1 {
			t.Fatalf("row %d is checkable; only the autostart row may be", i)
		}
	}
	if checkable != 1 {
		t.Fatalf("menu has %d checkable rows, want 1", checkable)
	}

	menu[0].Action()
	if probe.shown != 1 {
		t.Fatalf("\"Open omega\" show count = %d, want 1", probe.shown)
	}
	menu[2].Action()
	if probe.quit != 1 {
		t.Fatalf("\"Quit\" quit count = %d, want 1", probe.quit)
	}
}

// TestDesktopTrayAutostartRow: the row the menu draws is the registration - one click
// enables it with the desktop argument, the next disables it, and the tick the row
// shows is a fresh status read every time.
func TestDesktopTrayAutostartRow(t *testing.T) {
	shell, _, backend, log := newTrayShellWithAutostart()
	row := shell.menu()[1]

	if row.Checked() {
		t.Fatal("the row opened checked with no registration on disk")
	}

	row.Action()
	if !backend.enabled {
		t.Fatal("clicking the unchecked row left the registration off")
	}
	if len(backend.args) != 1 || len(backend.args[0]) != 1 || backend.args[0][0] != autostartDesktopArg {
		t.Fatalf("tray enable args = %q, want exactly [%q]: a login launch must open the desktop",
			backend.args, autostartDesktopArg)
	}
	if !row.Checked() {
		t.Fatal("the row still reads unchecked after the registration was written")
	}

	row.Action()
	if backend.enabled {
		t.Fatal("clicking the checked row left the registration on")
	}
	if len(log.errs) != 0 {
		t.Fatalf("clean flips reported %v", log.errs)
	}

	// The state is read on every open: a change made outside the tray shows at once.
	backend.enabled = true
	if !row.Checked() {
		t.Fatal("the row kept a cached state after the registration changed underneath it")
	}
}
