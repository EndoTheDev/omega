package main

// The autostart row's contract: a registration always launches the DESKTOP, the check
// state follows status on every read, a failed write is reported and re-read, and
// disable is idempotent. The fake is the only backend a test may drive - a real enable
// writes the runner's HKCU.

import (
	"errors"
	"testing"
)

// fakeAutostart is a registration store that never touches a registry.
type fakeAutostart struct {
	enabled      bool
	statusCalls  int
	enableCalls  int
	disableCalls int
	args         [][]string
	statusErr    error
	enableErr    error
	disableErr   error
}

func (f *fakeAutostart) Status() (bool, error) {
	f.statusCalls++
	if f.statusErr != nil {
		return false, f.statusErr
	}
	return f.enabled, nil
}

func (f *fakeAutostart) Enable(args []string) error {
	f.enableCalls++
	f.args = append(f.args, args)
	if f.enableErr != nil {
		return f.enableErr
	}
	f.enabled = true
	return nil
}

func (f *fakeAutostart) Disable() error {
	f.disableCalls++
	if f.disableErr != nil {
		return f.disableErr
	}
	f.enabled = false
	return nil
}

// reportLog records the errors the row would show in a dialog.
type reportLog struct{ errs []error }

func (r *reportLog) report(err error) { r.errs = append(r.errs, err) }

func newFakeControl() (*autostartControl, *fakeAutostart, *reportLog) {
	backend := &fakeAutostart{}
	log := &reportLog{}
	return newAutostartControl(backend, log.report), backend, log
}

// TestAutostartEnableCarriesDesktop: the Run key must launch the desktop face. Without
// the argument a login launch lands in the bare CLI agent loop.
func TestAutostartEnableCarriesDesktop(t *testing.T) {
	control, backend, log := newFakeControl()

	control.flip()
	if !backend.enabled {
		t.Fatal("flip did not enable a disabled registration")
	}
	if backend.enableCalls != 1 {
		t.Fatalf("enable calls = %d, want 1", backend.enableCalls)
	}
	if len(backend.args) != 1 {
		t.Fatalf("enable args recorded %d times, want 1", len(backend.args))
	}
	got := backend.args[0]
	if len(got) != 1 || got[0] != autostartDesktopArg {
		t.Fatalf("enable args = %q, want exactly [%q]", got, autostartDesktopArg)
	}
	if len(log.errs) != 0 {
		t.Fatalf("a clean enable reported %v", log.errs)
	}
}

// TestAutostartCheckFollowsStatus: the row's state is whatever status answers, read
// fresh each time - an enable that happened outside the tray shows up without a cache
// to invalidate.
func TestAutostartCheckFollowsStatus(t *testing.T) {
	control, backend, _ := newFakeControl()

	if control.checked() {
		t.Fatal("checked read as on for an unregistered backend")
	}
	backend.enabled = true
	if !control.checked() {
		t.Fatal("checked read as off after the backend registered")
	}
	if backend.statusCalls != 2 {
		t.Fatalf("status calls = %d, want 2: the row cached a previous answer", backend.statusCalls)
	}
}

// TestAutostartDisableIsIdempotent: disabling an absent registration is not an error
// and reports nothing, twice.
func TestAutostartDisableIsIdempotent(t *testing.T) {
	control, backend, log := newFakeControl()

	for i := 0; i < 2; i++ {
		if control.apply(false) {
			t.Fatalf("apply(false) reported the registration as on (round %d)", i+1)
		}
	}
	if backend.disableCalls != 2 {
		t.Fatalf("disable calls = %d, want 2", backend.disableCalls)
	}
	if backend.enabled {
		t.Fatal("the registration reads enabled after two disables")
	}
	if len(log.errs) != 0 {
		t.Fatalf("disabling an absent registration reported %v", log.errs)
	}
}

// TestAutostartEnableErrorReportsAndRereads: a failed write shows a dialog and the row
// ends up where status says it is - not where the click wanted it.
func TestAutostartEnableErrorReportsAndRereads(t *testing.T) {
	control, backend, log := newFakeControl()
	writeErr := errors.New("registry: access is denied")
	backend.enableErr = writeErr

	control.flip()
	if backend.statusCalls != 2 {
		t.Fatalf("status calls = %d, want 2 (flip's read plus the post-write re-read)", backend.statusCalls)
	}
	if control.checked() {
		t.Fatal("the row read on after a failed write: it must follow status, not the click")
	}
	if len(log.errs) != 1 || !errors.Is(log.errs[0], writeErr) {
		t.Fatalf("reported %v, want the write error once", log.errs)
	}
}

// TestAutostartStatusErrorReportsAndReadsOff: a registration the app cannot read shows
// a dialog and renders unchecked rather than keeping a stale tick.
func TestAutostartStatusErrorReportsAndReadsOff(t *testing.T) {
	control, backend, log := newFakeControl()
	backend.enabled = true
	readErr := errors.New("registry: access is denied")
	backend.statusErr = readErr

	if control.checked() {
		t.Fatal("checked rendered on while status failed")
	}
	if len(log.errs) != 1 || !errors.Is(log.errs[0], readErr) {
		t.Fatalf("reported %v, want the status error once", log.errs)
	}
	if backend.enableCalls+backend.disableCalls != 0 {
		t.Fatal("a status failure wrote the registration: the direction is unknown")
	}
}
