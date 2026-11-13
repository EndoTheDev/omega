//go:build linux

// The linux pty door's kill handshake tests (card t_5dba57a3). The
// card's cited diagnosis read the -race report's frames - "Kill's
// master.Close races readLoop's parked master.Read" - off the seam
// call lines in terminal.go; the racing memory the detector actually
// paired lived in the test fake's announce/wait WaitGroup (terminal_
// test.go names it). The real door's direct master.Close against a
// parked master.Read is fdmu-protected on a pollable /dev/ptmx and
// stays -race green - proven by probe at this card's base - so a real
// bypass cannot be made -race-observable. What this file pins
// instead is the handshake's behavioral contract on the real door,
// which is the reason it exists: on a master the runtime could not
// put in the poller (a blocking fd), the close under a parked read is
// the classic fd-reuse race, and the handshake removes that whole
// class - the master is closed by the last op out, never under one.
package terminal

import (
	"errors"
	"os"
	"testing"
	"time"
)

// TestKillHandshakeOnRealPty: Kill with a reader parked on the master
// must leave the door semantics standing - the parked read returns at
// once (the SIGKILL to the child's group hangs the pty up), the exit
// watcher fires with the signal death, the handshake owns the master's
// close (the reader's exit IS the close landing), and a later op
// refuses at the door.
func TestKillHandshakeOnRealPty(t *testing.T) {
	if _, err := os.Stat("/dev/ptmx"); err != nil {
		t.Skipf("no /dev/ptmx on this box: %v", err)
	}
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skipf("no /bin/sh: %v", err)
	}
	t.Setenv("SHELL", "/bin/sh")
	proc, err := spawnProcess(t.TempDir())
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	// a reader parks on the master, readLoop-shaped: it only leaves on
	// an error, and its exit proves the parked Read returned (and the
	// deferred master close landed inside that Read's settle).
	left := make(chan error, 1)
	go func() {
		buf := make([]byte, 4096)
		for {
			_, err := proc.Read(buf)
			if err != nil {
				left <- err
				return
			}
		}
	}()
	time.Sleep(100 * time.Millisecond) // the read parks on the master

	if err := proc.Kill(); err != nil {
		t.Fatalf("kill: %v", err)
	}
	select {
	case err := <-left:
		if err == nil {
			t.Fatal("the parked read survived the kill")
		}
	case <-time.After(testWaitTimeout):
		t.Fatal("the parked read never returned - the kill left it hanging")
	}

	// the reap: the exit watcher fires, the code is the signal death
	select {
	case <-proc.Done():
	case <-time.After(testWaitTimeout):
		t.Fatal("the exit watcher never fired")
	}
	if code := proc.ExitCode(); code != -1 {
		t.Fatalf("exit code %d, want -1 (the signal death)", code)
	}

	// the handshake owns the close: a later op refuses at the door
	if _, err := proc.Read(make([]byte, 8)); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("post-kill read: %v, want os.ErrClosed", err)
	}
	if _, err := proc.Write([]byte("x")); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("post-kill write: %v, want os.ErrClosed", err)
	}
}

// TestIdleKillClosesMasterDirectly: with nothing parked on the master
// the handshake's close is immediate - Kill closes the master itself
// (the deferred path is only for a parked op).
func TestIdleKillClosesMasterDirectly(t *testing.T) {
	if _, err := os.Stat("/dev/ptmx"); err != nil {
		t.Skipf("no /dev/ptmx on this box: %v", err)
	}
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skipf("no /bin/sh: %v", err)
	}
	t.Setenv("SHELL", "/bin/sh")
	proc, err := spawnProcess(t.TempDir())
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	// no reader: the shell idles, nobody parks on the master
	if err := proc.Kill(); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if _, err := proc.Read(make([]byte, 8)); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("post-kill read: %v, want os.ErrClosed", err)
	}
	select {
	case <-proc.Done():
	case <-time.After(testWaitTimeout):
		t.Fatal("the exit watcher never fired")
	}
}
