//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const instanceJobHelperEnv = "OMEGA_INSTANCE_JOB_HELPER"

// TestInstanceJobHelperProcess is the long-lived child TestInstanceJobKillOnClose puts
// in the job: it does nothing until the job handle closing terminates it. The parent
// re-executes this test binary with the env var set and -test.run pinned to it.
func TestInstanceJobHelperProcess(t *testing.T) {
	if os.Getenv(instanceJobHelperEnv) != "1" {
		t.Skip("helper process for TestInstanceJobKillOnClose")
	}
	time.Sleep(2 * testWaitTimeout)
}

// TestInstanceJobKillOnClose is the real mechanism end to end: a child assigned to the
// job dies when the job's last handle closes, which is what a desktop process exit does.
func TestInstanceJobKillOnClose(t *testing.T) {
	job, err := newInstanceJob()
	if err != nil {
		t.Fatalf("newInstanceJob: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(exe, "-test.run", "^TestInstanceJobHelperProcess$")
	cmd.Env = append(os.Environ(), instanceJobHelperEnv+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the helper: %v", err)
	}
	exited := make(chan struct{})
	t.Cleanup(func() {
		// Bounded: a failure before the waiter starts leaves the channel unclosed.
		_ = cmd.Process.Kill()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
		}
	})
	if err := job.assign(cmd.Process); err != nil {
		t.Fatalf("assign the helper to the job: %v", err)
	}
	go func() {
		_, _ = cmd.Process.Wait()
		close(exited)
	}()

	job.release()

	// The poll catches a hang; it does not measure the kill's latency.
	deadline := time.Now().Add(testWaitTimeout)
	for {
		select {
		case <-exited:
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("the helper outlived the closed job handle by %s", testWaitTimeout)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

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
