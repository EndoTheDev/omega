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

// TestDesktopShellMinimizePress is the caption's minimize cell: minimize-to-tray hides the
// window through the shell and never asks the OS to minimize it; off is the OS's minimize
// and never a hide.
func TestDesktopShellMinimizePress(t *testing.T) {
	shell, probe := newTrayShell()
	shell.setWindowPolicy(func() desktopWindowPolicy {
		return desktopWindowPolicy{MinimizeToTray: true}
	})

	minimised := 0
	shell.minimizeWindow(func() { minimised++ })
	if probe.hidden != 1 {
		t.Fatalf("hides = %d, want 1: minimize-to-tray must route through the shell", probe.hidden)
	}
	if minimised != 0 {
		t.Fatalf("minimize-to-tray still called the window's own minimize (%d times)", minimised)
	}

	shell.setWindowPolicy(func() desktopWindowPolicy { return desktopWindowPolicy{} })
	shell.minimizeWindow(func() { minimised++ })
	if minimised != 1 {
		t.Fatalf("a plain minimize press called the window's minimize %d times, want 1", minimised)
	}
	if probe.hidden != 1 {
		t.Fatalf("a plain minimize press hid the window (hide count %d)", probe.hidden)
	}
}

// TestDesktopShellCloseFollowsCloseToTray: with close-to-tray off the close goes through
// and nothing hides - the documented lifetime law, the process exits. The untouched
// config's policy (no reader bound) keeps hiding.
func TestDesktopShellCloseFollowsCloseToTray(t *testing.T) {
	shell, probe := newTrayShell()
	shell.setWindowPolicy(func() desktopWindowPolicy { return desktopWindowPolicy{} })

	cancelled := false
	shell.windowClosing(func() { cancelled = true })
	if cancelled {
		t.Fatal("close-to-tray off still cancelled the close: the window would survive")
	}
	if probe.hidden != 0 {
		t.Fatalf("close-to-tray off hid the window (hide count %d)", probe.hidden)
	}

	// No reader bound is today's behavior: an untouched shell hides on close.
	plain, plainProbe := newTrayShell()
	cancelled = false
	plain.windowClosing(func() { cancelled = true })
	if !cancelled || plainProbe.hidden != 1 {
		t.Fatalf("the default policy answered cancelled=%v hides=%d, want a hidden window and a cancelled close",
			cancelled, plainProbe.hidden)
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
// pointer the test moves, the launch origin the tray reads, and the switch recorded.
func newConnectionShell(store desktopInstanceStore, active *string, home string) (*desktopShell, *[]string) {
	shell, _, _, _ := newTrayShellWithAutostart()
	switched := &[]string{}
	shell.setInstances(store, func() string { return *active }, func(url string) {
		*switched = append(*switched, url)
	})
	shell.setHome(func() string { return home })
	return shell, switched
}

// TestDesktopTrayConnectionRows: the Connection submenu is the app's own list - the home
// row first, then every saved row, the window's own origin checked, one click naming its
// URL. The rows are read where the popup is drawn, so a save that landed a moment ago is
// already there.
func TestDesktopTrayConnectionRows(t *testing.T) {
	home := "http://127.0.0.1:18101"
	store := &fakeInstanceStore{list: []core.SavedInstance{
		{Name: "main", URL: home},
		{Name: "pi", URL: "http://192.0.2.10:8098"},
	}}
	active := home
	shell, switched := newConnectionShell(store, &active, home)

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
		t.Fatalf("connection rows = %d, want the home row and the one other saved row", len(rows))
	}
	if rows[0].Label != "main - http://127.0.0.1:18101 (default)" {
		t.Fatalf("row 0 = %q, want the home row with the stored name and the default mark", rows[0].Label)
	}
	if rows[1].Label != "pi - http://192.0.2.10:8098" {
		t.Fatalf("row 1 = %q, want the saved name and its address", rows[1].Label)
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

// TestDesktopTrayHomeRow is the way back: the launch origin leads the submenu whatever the
// list holds, a stored row naming that address lends its name, the mark follows the active
// origin, and a click names the home origin. A saved path under home is another place and
// a second row for the home address never renders.
func TestDesktopTrayHomeRow(t *testing.T) {
	const home = "http://127.0.0.1:18101"
	const homePage = "http://127.0.0.1:18102"

	// no stored row names home: the label falls back to the word, and the click goes home
	active := homePage
	shell, switched := newConnectionShell(&fakeInstanceStore{}, &active, home)
	rows := shell.connectionRows()
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want the home row plus the empty-list note", len(rows))
	}
	if rows[0].Label != "Home - http://127.0.0.1:18101 (default)" {
		t.Fatalf("home row = %q, want Home and the launch origin", rows[0].Label)
	}
	if rows[0].Checked() {
		t.Error("the home row is checked while the window is on another origin")
	}
	rows[0].Action()
	if len(*switched) != 1 || (*switched)[0] != home {
		t.Fatalf("the home row's click named %q, want the launch origin", *switched)
	}
	active = home
	if rows = shell.connectionRows(); !rows[0].Checked() {
		t.Fatal("the home row is not checked while the window is on home")
	}

	// a saved path under home is another place: it keeps its own row and lends no name
	shell, _ = newConnectionShell(&fakeInstanceStore{list: []core.SavedInstance{
		{Name: "sub", URL: home + "/ui"},
	}}, &active, home)
	rows = shell.connectionRows()
	if len(rows) != 2 || rows[1].Label != "sub - "+home+"/ui" {
		t.Fatalf("rows = %+v, want the home row and the path row beside it", rows)
	}
	if rows[0].Label != "Home - http://127.0.0.1:18101 (default)" {
		t.Fatalf("home row = %q, want Home: a saved path does not name the home address", rows[0].Label)
	}

	// a stored row naming the home address exactly lends its name - and renders once
	active = homePage
	shell, _ = newConnectionShell(&fakeInstanceStore{list: []core.SavedInstance{
		{Name: "winbtw", URL: home},
		{Name: "pi", URL: "http://192.0.2.10:8098"},
	}}, &active, home)
	rows = shell.connectionRows()
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want the home row and pi: the home address never renders twice", len(rows))
	}
	if rows[0].Label != "winbtw - http://127.0.0.1:18101 (default)" {
		t.Fatalf("home row = %q, want the stored name", rows[0].Label)
	}
	for _, row := range rows[1:] {
		if row.Label == "winbtw - "+home {
			t.Fatal("the home address rendered a second time as a saved row")
		}
	}
}

// TestDesktopTrayConnectionRowsTellTheGap: an unreadable or empty list still answers rows,
// so the submenu is never a dead arrow with nothing to say - and a broken list never takes
// the way home with it.
func TestDesktopTrayConnectionRowsTellTheGap(t *testing.T) {
	const home = "http://127.0.0.1:18101"
	active := home

	shell, _ := newConnectionShell(&fakeInstanceStore{list: []core.SavedInstance{}}, &active, home)
	rows := shell.connectionRows()
	if len(rows) != 2 || rows[0].Label != "Home - http://127.0.0.1:18101 (default)" {
		t.Fatalf("empty list rows = %+v, want the home row first", rows)
	}
	if rows[1].Label != "no saved connections" {
		t.Fatalf("empty list note = %q, want the plain row", rows[1].Label)
	}
	if rows[1].Action != nil || rows[1].Checked != nil {
		t.Fatal("the empty-list row is clickable; it must only say so")
	}

	shell, _ = newConnectionShell(&fakeInstanceStore{readErr: errors.New("config.yaml is unreadable")}, &active, home)
	rows = shell.connectionRows()
	if len(rows) != 2 || rows[0].Label != "Home - http://127.0.0.1:18101 (default)" {
		t.Fatalf("refusal rows = %+v, want the home row first: a broken config keeps the way home", rows)
	}
	if rows[1].Label != "list unavailable: config.yaml is unreadable" {
		t.Fatalf("refusal row = %q, want the host's words", rows[1].Label)
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
