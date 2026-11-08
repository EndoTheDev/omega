//go:build windows

package cron

// sweep_windows_test.go: the Windows-only pin for card t_e9965bc6.
// saveCronState's atomic swap (tmp + os.Rename) opens cronstate.json
// with DELETE access and no read-share for the length of the rename;
// a poller stepping into that window gets a sharing violation from
// os.ReadFile. waitForState must treat that as a retry, never a
// verdict. This pin replays the window deterministically: an exclusive
// handle (share mode 0) pins the file for 200ms while waitForState
// polls; the fatal-on-first-error shape reds instantly, the tolerant
// poll lands green the moment the pin releases.

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestWaitForStateRetriesThroughPinnedFile(t *testing.T) {
	home := t.TempDir()
	writeState(t, home, cronState{Jobs: map[string]int64{}})
	p, err := syscall.UTF16PtrFromString(filepath.Join(home, cronStateFileName))
	if err != nil {
		t.Fatal(err)
	}
	// Share mode 0: every os.ReadFile of the pinned file fails with
	// the sharing violation - the rename window, held open.
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatalf("pin open: %v", err)
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = syscall.CloseHandle(h)
	}()
	waitForState(t, home, func(st cronState) bool {
		return len(st.Jobs) == 0
	})
	// waitForState returned, so a read landed - the pin is released.
}
