//go:build windows

package main

import "testing"

// TestDesktopAttachAnswerIsStable pins the memo behind the routing: main attaches
// before its first write and bareLaunchWantsDesktop asks again, while a second
// AttachConsole on an attached process fails. One answer per process keeps a terminal
// launch out of the desktop branch.
func TestDesktopAttachAnswerIsStable(t *testing.T) {
	first := attachCallerConsole()
	t.Logf("attach answer in the test process = %v", first)
	for i := 2; i <= 4; i++ {
		if got := attachCallerConsole(); got != first {
			t.Fatalf("attach answer #%d = %v, want the first answer %v", i, got, first)
		}
	}
	if got := bareLaunchWantsDesktop(); got == first {
		t.Fatalf("bareLaunchWantsDesktop = %v after attach = %v, want its negation", got, first)
	}
}
