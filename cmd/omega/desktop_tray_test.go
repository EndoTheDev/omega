package main

// desktop_tray_test.go: the tray's semantics - close hides, quit exits, and the menu is
// exactly the two items the shell promises.

import "testing"

// trayProbe counts what the shell did through the Wails seams.
type trayProbe struct {
	shown, hidden, quit int
}

func newTrayShell() (*desktopShell, *trayProbe) {
	p := &trayProbe{}
	return newDesktopShell(
		func() { p.shown++ },
		func() { p.hidden++ },
		func() { p.quit++ },
	), p
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
	want := []string{"Open omega", "Quit"}
	if len(menu) != len(want) {
		t.Fatalf("menu has %d items, want %d: %+v", len(menu), len(want), menu)
	}
	for i, label := range want {
		if menu[i].Label != label {
			t.Fatalf("menu[%d] = %q, want %q", i, menu[i].Label, label)
		}
	}

	menu[0].Action()
	if probe.shown != 1 {
		t.Fatalf("\"Open omega\" show count = %d, want 1", probe.shown)
	}
	menu[1].Action()
	if probe.quit != 1 {
		t.Fatalf("\"Quit\" quit count = %d, want 1", probe.quit)
	}
}
