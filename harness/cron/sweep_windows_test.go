//go:build windows

package cron

// sweep_windows_test.go: the Windows-only pin for waitForState's retry. saveCronState's
// atomic swap (tmp + os.Rename) opens cronstate.json with DELETE access and no
// read-share for the length of the rename; a poller stepping into that window gets a
// sharing violation from os.ReadFile. waitForState must treat that as a retry, never a
// verdict.

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
	// Share mode 0: every os.ReadFile of the pinned file fails with the sharing violation.
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
}
