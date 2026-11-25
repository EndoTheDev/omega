package main

// The tray's controller: what closing the window means and what the two menu items do.
// The Wails calls live in desktop_windows.go, so the semantics are asserted here on
// every platform.

import "sync"

// desktopShell holds the shell's exit state and the tray's one checkable row.
type desktopShell struct {
	mu        sync.Mutex
	quitting  bool
	autostart *autostartControl
	show      func()
	hide      func()
	quit      func()
}

func newDesktopShell(show, hide, quit func(), autostart *autostartControl) *desktopShell {
	return &desktopShell{show: show, hide: hide, quit: quit, autostart: autostart}
}

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

// desktopMenuEntry is one tray item: a label and the action behind it. Checked marks a
// checkable row - it reports the live state each time the menu opens, never a copy.
type desktopMenuEntry struct {
	Label   string
	Action  func()
	Checked func() bool
}

// menu is the whole tray surface: no switcher, no settings.
func (s *desktopShell) menu() []desktopMenuEntry {
	return []desktopMenuEntry{
		{Label: "Open omega", Action: s.openWindow},
		{Label: autostartItemLabel, Action: s.flipAutostart, Checked: s.autostart.checked},
		{Label: "Quit", Action: s.quitApp},
	}
}

// flipAutostart applies the click to the live registration; the row's own Checked read
// renders the result.
func (s *desktopShell) flipAutostart() { s.autostart.flip() }
