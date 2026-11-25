package main

// Start with Windows: one checkable tray row over the Wails autostart surface
// (application.AutostartManager). Nothing here caches - every menu open re-reads the
// registration, and a failed write shows a dialog.

const (
	// autostartIdentifier is the Run value name: stable, so a repeated enable rewrites
	// the one entry instead of stacking a second.
	autostartIdentifier = "omega"
	// autostartDesktopArg is load-bearing: the Run key must launch the desktop, never
	// the bare CLI agent loop.
	autostartDesktopArg = "desktop"
	autostartItemLabel  = "Start with Windows"
)

// autostartBackend is the Wails autostart surface behind an interface. Tests drive a
// fake: a real enable writes the runner's HKCU.
type autostartBackend interface {
	Status() (bool, error)
	Enable(args []string) error
	Disable() error
}

// autostartControl is the row's behavior: read on open, flip on click.
type autostartControl struct {
	backend autostartBackend
	report  func(error)
}

func newAutostartControl(backend autostartBackend, report func(error)) *autostartControl {
	if report == nil {
		report = func(error) {}
	}
	return &autostartControl{backend: backend, report: report}
}

// launchArgs is what the registration launches.
func (c *autostartControl) launchArgs() []string { return []string{autostartDesktopArg} }

// checked is the row's state for the menu being opened. A status failure is reported
// and reads as off, so a broken registration never renders as on.
func (c *autostartControl) checked() bool {
	on, err := c.backend.Status()
	if err != nil {
		c.report(err)
	}
	return on
}

// flip inverts the live registration and returns the state it reports afterwards. A
// status failure is reported and writes nothing - the direction is unknown.
func (c *autostartControl) flip() bool {
	on, err := c.backend.Status()
	if err != nil {
		c.report(err)
		return false
	}
	return c.apply(!on)
}

// apply sets the registration, then re-reads: a failed write is reported, and the row
// follows status rather than the click's intent.
func (c *autostartControl) apply(want bool) bool {
	var err error
	if want {
		err = c.backend.Enable(c.launchArgs())
	} else {
		err = c.backend.Disable()
	}
	if err != nil {
		c.report(err)
	}
	return c.checked()
}
