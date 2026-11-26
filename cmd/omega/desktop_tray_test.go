package main

// desktop_tray_test.go: the tray's semantics - close hides, quit exits, the menu is the
// shell's rows, and the Connection submenu is the app's own list with a live check.

import (
	"errors"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// fakeInstanceStore stands in for the host's one writer: the rows a read answers and the
// rows a write landed.
type fakeInstanceStore struct {
	list    []core.SavedInstance
	readErr error
	saved   []core.SavedInstance
	saveErr error
}

func (f *fakeInstanceStore) Instances() ([]core.SavedInstance, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	return f.list, nil
}

func (f *fakeInstanceStore) SaveInstances(list []core.SavedInstance) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = list
	f.list = list
	return nil
}

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

// newConnectionShell is the production shape: a store bound, the window's origin behind a
// pointer the test moves, and the switch recorded.
func newConnectionShell(store desktopInstanceStore, active *string) (*desktopShell, *[]string) {
	shell, _, _, _ := newTrayShellWithAutostart()
	switched := &[]string{}
	shell.setInstances(store, func() string { return *active }, func(url string) {
		*switched = append(*switched, url)
	})
	return shell, switched
}

// TestDesktopTrayConnectionRows: the Connection submenu is the app's own list - every
// row, the window's own origin checked, one click naming its URL. The rows are read where
// the popup is drawn, so a save that landed a moment ago is already there.
func TestDesktopTrayConnectionRows(t *testing.T) {
	store := &fakeInstanceStore{list: []core.SavedInstance{
		{Name: "main", URL: "http://127.0.0.1:18101"},
		{Name: "pi", URL: "http://192.0.2.10:8098"},
	}}
	active := "http://127.0.0.1:18101"
	shell, switched := newConnectionShell(store, &active)

	menu := shell.menu()
	want := []string{"Open omega", desktopConnectionLabel, autostartItemLabel, "Quit"}
	if len(menu) != len(want) {
		t.Fatalf("menu has %d items, want %d: %+v", len(menu), len(want), menu)
	}
	for i, label := range want {
		if menu[i].Label != label {
			t.Fatalf("menu[%d] = %q, want %q", i, menu[i].Label, label)
		}
	}
	if menu[1].Children == nil {
		t.Fatal("the Connection row is not a submenu: the tray has no way back")
	}

	rows := shell.connectionRows()
	if len(rows) != 2 {
		t.Fatalf("connection rows = %d, want the two saved rows", len(rows))
	}
	if rows[0].Label != "main - http://127.0.0.1:18101" {
		t.Fatalf("row 0 = %q, want the name and its address", rows[0].Label)
	}
	if rows[0].Checked == nil || rows[1].Checked == nil {
		t.Fatal("a connection row is not checkable: the popup cannot show the active one")
	}
	if !rows[0].Checked() {
		t.Error("the row the window is showing is not checked")
	}
	if rows[1].Checked() {
		t.Error("a row the window is not showing is checked")
	}

	rows[1].Action()
	if len(*switched) != 1 || (*switched)[0] != "http://192.0.2.10:8098" {
		t.Fatalf("a row's click named %q, want its own URL", *switched)
	}

	// A switch moves the check: the state is read from the window, never cached.
	active = "http://192.0.2.10:8098/"
	rows = shell.connectionRows()
	if rows[0].Checked() || !rows[1].Checked() {
		t.Fatal("the check did not follow the window's active origin (a trailing slash is the same instance)")
	}

	// The list is read at every open: a row saved since the menu was built appears.
	store.list = append(store.list, core.SavedInstance{Name: "desk", URL: "http://127.0.0.1:18102"})
	if rows = shell.connectionRows(); len(rows) != 3 {
		t.Fatalf("connection rows = %d after a save, want the store's live list", len(rows))
	}
}

// TestDesktopTrayConnectionRowsTellTheGap: an unreadable or empty list still answers a row,
// so the submenu is never a dead arrow with nothing to say.
func TestDesktopTrayConnectionRowsTellTheGap(t *testing.T) {
	active := "http://127.0.0.1:18101"

	shell, _ := newConnectionShell(&fakeInstanceStore{list: []core.SavedInstance{}}, &active)
	rows := shell.connectionRows()
	if len(rows) != 1 || rows[0].Label != "no saved connections" {
		t.Fatalf("empty list rows = %+v, want the one plain row", rows)
	}
	if rows[0].Action != nil || rows[0].Checked != nil {
		t.Fatal("the empty-list row is clickable; it must only say so")
	}

	shell, _ = newConnectionShell(&fakeInstanceStore{readErr: errors.New("config.yaml is unreadable")}, &active)
	rows = shell.connectionRows()
	if len(rows) != 1 || rows[0].Label != "list unavailable: config.yaml is unreadable" {
		t.Fatalf("refusal rows = %+v, want the host's words", rows)
	}
}

// TestDesktopOriginKey: the check mark and the frame rule compare instances by scheme and
// host, so a page's path never reads as another connection.
func TestDesktopOriginKey(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"http://127.0.0.1:18101", "http://127.0.0.1:18101"},
		{"HTTP://127.0.0.1:18101/", "http://127.0.0.1:18101"},
		{"http://127.0.0.1:18101/index.html", "http://127.0.0.1:18101"},
		{"https://omega.example.test/base/", "https://omega.example.test"},
		{"  http://127.0.0.1:18101  ", "http://127.0.0.1:18101"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := desktopOriginKey(tc.raw); got != tc.want {
			t.Errorf("desktopOriginKey(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
	if desktopOriginKey("http://127.0.0.1:18101") == desktopOriginKey("http://127.0.0.1:18102") {
		t.Error("two ports on one host read as the same instance")
	}
}
