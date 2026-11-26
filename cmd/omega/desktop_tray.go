package main

// The tray's controller: what closing the window means, what the window shows, and what
// the menu items do. The Wails calls live in desktop_windows.go, so the semantics are
// asserted here on every platform.

import (
	"strings"
	"sync"

	"github.com/EndoTheDev/omega/core"
)

// desktopConnectionLabel is the submenu that carries the app's own connection list.
const desktopConnectionLabel = "Connection"

// desktopInstanceStore is the app-local connection list: the host's Settings
// implementation is the ONE validator (core.NormalizeInstances) and the ONE atomic
// writer, so the tray, the pane and the raw message door all go through it.
type desktopInstanceStore interface {
	Instances() ([]core.SavedInstance, error)
	SaveInstances([]core.SavedInstance) error
}

// desktopShell holds the shell's exit state, the tray's rows and the list the tray's
// Connection submenu switches between.
type desktopShell struct {
	mu        sync.Mutex
	quitting  bool
	autostart *autostartControl
	updates   *trayUpdates
	show      func()
	hide      func()
	quit      func()

	instances desktopInstanceStore
	active    func() string
	home      func() string
	switchTo  func(string)
}

func newDesktopShell(show, hide, quit func(), autostart *autostartControl) *desktopShell {
	return &desktopShell{show: show, hide: hide, quit: quit, autostart: autostart}
}

// setUpdates binds the update row's client. Nil leaves the row out (a hand-built shell in
// a test never talks to a server).
func (s *desktopShell) setUpdates(u *trayUpdates) { s.updates = u }

// setInstances binds the app-local connection list and the window's origin. Nil leaves the
// Connection submenu out, the same way a nil updates client leaves that row out.
func (s *desktopShell) setInstances(store desktopInstanceStore, active func() string, switchTo func(string)) {
	s.instances = store
	s.active = active
	s.switchTo = switchTo
}

// setHome binds the origin the window was launched on. The host holds that value (it is
// the resolved launch origin); the tray only reads it, so no list and no page can retarget
// the way back.
func (s *desktopShell) setHome(home func() string) { s.home = home }

// openWindow backs the "Open omega" item and a tray click.
func (s *desktopShell) openWindow() { s.show() }

// quitApp sets the flag FIRST: the destroy that follows fires the same closing hook,
// and that hook must let the close through.
func (s *desktopShell) quitApp() {
	s.mu.Lock()
	s.quitting = true
	s.mu.Unlock()
	s.quit()
}

// windowClosing is the close-to-tray hook: hide, then cancel the native close.
func (s *desktopShell) windowClosing(cancelClose func()) {
	s.mu.Lock()
	quitting := s.quitting
	s.mu.Unlock()
	if quitting {
		return
	}
	s.hide()
	cancelClose()
}

// desktopMenuEntry is one tray item: a label, the action behind it, and - for a checkable
// row - the live state. Children makes the row a submenu, rebuilt on every open.
type desktopMenuEntry struct {
	Label    string
	Action   func()
	Checked  func() bool
	Children []desktopMenuEntry
}

// menu is the whole tray surface: open, the app's connections, autostart, updates, quit.
func (s *desktopShell) menu() []desktopMenuEntry {
	rows := []desktopMenuEntry{{Label: "Open omega", Action: s.openWindow}}
	if s.instances != nil {
		rows = append(rows, desktopMenuEntry{Label: desktopConnectionLabel, Children: []desktopMenuEntry{}})
	}
	rows = append(rows, desktopMenuEntry{Label: autostartItemLabel, Action: s.flipAutostart, Checked: s.autostart.checked})
	if s.updates != nil {
		rows = append(rows, desktopMenuEntry{Label: trayUpdateLabel, Action: s.updates.act})
	}
	return append(rows, desktopMenuEntry{Label: "Quit", Action: s.quitApp})
}

// connectionRows is the Connection submenu, read from the store on every open: the home
// row first, then every app-local row, the window's own origin checked, one click to show
// it. A refusal or an empty list still answers rows, so the submenu is never a dead arrow,
// and neither condition removes the way home: home is the launch origin, not a stored row.
func (s *desktopShell) connectionRows() []desktopMenuEntry {
	list, err := s.instances.Instances()
	rows := make([]desktopMenuEntry, 0, len(list)+2)
	home := s.homeOrigin()
	if home != "" {
		rows = append(rows, s.homeRow(list, home))
	}
	if err != nil {
		return append(rows, desktopMenuEntry{Label: "list unavailable: " + err.Error()})
	}
	if len(list) == 0 {
		return append(rows, desktopMenuEntry{Label: "no saved connections"})
	}
	for _, row := range list {
		url := row.URL
		// the saved row naming the home address IS the home row: one place never renders twice.
		if url == home {
			continue
		}
		rows = append(rows, desktopMenuEntry{
			Label:  row.Name + " - " + url,
			Action: func() { s.switchToInstance(url) },
			// the check is read where the popup is drawn: the window may have switched
			// since the menu was built.
			Checked: func() bool {
				if s.active == nil {
					return false
				}
				active := desktopOriginKey(s.active())
				return active != "" && desktopOriginKey(url) == active
			},
		})
	}
	return rows
}

// homeRow is the way back: the origin the window was launched on. It is derived state -
// the resolved launch origin - never a stored row, so no save, no switch and no store
// refusal can move it or take it away. The label borrows a stored row's name when that row
// names the home address exactly; a saved row carrying a path under home is another place
// and keeps its own row.
func (s *desktopShell) homeRow(list []core.SavedInstance, home string) desktopMenuEntry {
	name := "Home"
	for _, row := range list {
		if row.URL == home {
			name = row.Name
			break
		}
	}
	return desktopMenuEntry{
		Label:  name + " - " + home + " (default)",
		Action: func() { s.switchToInstance(home) },
		Checked: func() bool {
			if s.active == nil {
				return false
			}
			active := desktopOriginKey(s.active())
			return active != "" && active == desktopOriginKey(home)
		},
	}
}

// homeOrigin reads the launch origin through the bound reader. A shell handed no reader
// (a hand-built one) has no home to offer.
func (s *desktopShell) homeOrigin() string {
	if s.home == nil {
		return ""
	}
	return s.home()
}

// switchToInstance performs a Connection click: the host records the origin and moves the
// window, so the next open's check mark follows.
func (s *desktopShell) switchToInstance(url string) {
	if s.switchTo == nil {
		return
	}
	s.switchTo(url)
}

// flipAutostart applies the click to the live registration; the row's own Checked read
// renders the result.
func (s *desktopShell) flipAutostart() { s.autostart.flip() }

// desktopOriginKey is the identity two addresses are compared by: scheme and host, never
// the path or a trailing slash. A page's own URL carries a path, a saved row may carry a
// prefix, and the window's active origin is whatever the host last navigated to - all
// three can name one instance.
func desktopOriginKey(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	at := strings.Index(s, "://")
	if at < 0 {
		return strings.TrimSuffix(s, "/")
	}
	scheme, rest := s[:at], s[at+3:]
	if cut := strings.IndexAny(rest, "/?#"); cut >= 0 {
		rest = rest[:cut]
	}
	return scheme + "://" + rest
}
