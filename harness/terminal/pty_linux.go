//go:build linux

// Package terminal — the PTY seam: one real pty pair per terminal
// session, the shell running on the child side. Stdlib-only plus
// golang.org/x/sys/unix (already a dependency): no cgo, no pty library.
//
// Build-tagged: the pty vocabulary is linux-only (/dev/ptmx + the Linux
// pty ioctl set - darwin carries no equivalent constants, so its build
// never reaches here). windows carries its own ConPTY door in
// pty_windows.go; pty_other.go carries the stub for both remaining
// platforms so the module still compiles everywhere - spawn returns
// an error, the routes answer it.
package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"sync"

	"golang.org/x/sys/unix"
)

// spawnProcess starts the shell on a fresh pty pair, cwd=workdir.
// process (the seam interface) lives in terminal.go.
func spawnProcess(workdir string) (process, error) {
	shell := shellPath()
	masterFD, slave, err := openpty()
	if err != nil {
		return nil, fmt.Errorf("openpty: %w", err)
	}
	master := os.NewFile(uintptr(masterFD), "/dev/ptmx")

	cmd := exec.Command(shell)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		// the pty is a fresh session - make sure it looks and behaves
		// like a real terminal, never like the omega process's stdin
		"TERM_PROGRAM=omega",
	)
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	// the child leads its own session with the slave as controlling
	// terminal - job control (Ctrl-C, Ctrl-Z) works, Ctrl-C never
	// reaches omega
	cmd.SysProcAttr = &unix.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		slave.Close()
		master.Close()
		return nil, fmt.Errorf("start %s: %w", shell, err)
	}
	slave.Close() // the child owns the slave now; the parent holds the master
	return &ptyProcess{master: master, cmd: cmd}, nil
}

// openpty allocates a pty pair (open /dev/ptmx, unlock, resolve the
// slave) and returns the master fd with the open slave file.
func openpty() (int, *os.File, error) {
	m, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, nil, err
	}
	cleanup := func() {
		unix.Close(m)
	}
	// grantpt/unlockpt: clear the TIOCSPTLCK lock flag on the master
	// (the ioctl reads a POINTER to the int - IoctlSetInt would pass
	// the value itself and the kernel answers EFAULT)
	if err := unix.IoctlSetPointerInt(m, unix.TIOCSPTLCK, 0); err != nil {
		cleanup()
		return -1, nil, fmt.Errorf("unlock pty: %w", err)
	}
	n, err := unix.IoctlGetInt(m, unix.TIOCGPTN)
	if err != nil {
		cleanup()
		return -1, nil, fmt.Errorf("pty name: %w", err)
	}
	s, err := unix.Open(fmt.Sprintf("/dev/pts/%d", n), unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		cleanup()
		return -1, nil, fmt.Errorf("open slave: %w", err)
	}
	return m, os.NewFile(uintptr(s), fmt.Sprintf("/dev/pts/%d", n)), nil
}

// shellPath picks the shell the same way VS Code's default profile
// does: $SHELL, else bash, else whatever /bin/sh is.
func shellPath() string {
	candidates := []string{}
	if sh := os.Getenv("SHELL"); sh != "" {
		candidates = append(candidates, sh)
	}
	candidates = append(candidates, "/bin/bash", "/bin/sh")
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return "/bin/sh"
}

// ptyProcess is the unix process: master + child command.
type ptyProcess struct {
	master *os.File
	cmd    *exec.Cmd
	done   chan struct{}
	code   int

	// The master close handshake (card t_5dba57a3): the session loops
	// park in Read/Write on the master for the shell's whole life, and
	// the kill path must never yank the fd out from under a parked op.
	// On a pollable master (the normal /dev/ptmx) os.File's fdmu makes
	// a concurrent Close race-clean today, but the fd is then loose in
	// the open/close table while the syscall is still parked on it, and
	// a master the runtime could NOT put in the poller (a blocking fd)
	// turns the same close into the classic fd-reuse race: the parked
	// read can come back with another file's bytes. So the close is
	// owned, never raced: an op announces itself under mu before it
	// touches the master and settles after; closeMaster marks the
	// close and performs it only when the count is zero, otherwise the
	// LAST op out closes the master itself. Kill still breaks a parked
	// read the way it always did - the SIGKILL to the child's group
	// hangs the pty up, the read returns EIO at once - so the deferred
	// close lands on the parked op's heels, never under it.
	mu       sync.Mutex
	closed   bool
	inFlight int
}

// enter announces one op about to touch the master; false = closed.
func (p *ptyProcess) enter() bool {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return false
	}
	p.inFlight++
	p.mu.Unlock()
	return true
}

// leave settles one op; the last op out after a close request owns
// the master's close.
func (p *ptyProcess) leave() {
	p.mu.Lock()
	p.inFlight--
	own := p.closed && p.inFlight == 0
	p.mu.Unlock()
	if own {
		_ = p.master.Close()
	}
}

func (p *ptyProcess) Read(b []byte) (int, error) {
	if !p.enter() {
		return 0, os.ErrClosed
	}
	defer p.leave()
	return p.master.Read(b)
}

func (p *ptyProcess) Write(b []byte) (int, error) {
	if !p.enter() {
		return 0, os.ErrClosed
	}
	defer p.leave()
	return p.master.Write(b)
}

// Close hands the master its close without ever overlapping a parked
// op: idle closes now, else the last op out owns it. Idempotent.
func (p *ptyProcess) Close() error {
	return p.closeMaster()
}

// closeMaster marks the close and performs it only when no op is
// parked on the master; a parked op (woken by the child's death)
// finishes the close on its way out. The one lingering shape: a
// descendant that escaped the child's process group and still holds
// the slave keeps a parked read up (Linux's own pty semantics - the
// session is gone either way); the close lands when it exits.
func (p *ptyProcess) closeMaster() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	idle := p.inFlight == 0
	p.mu.Unlock()
	if idle {
		return p.master.Close()
	}
	return nil
}

// Done runs the watcher: reap the child, then close the done fence.
// The session's watchLoop owns the exit broadcast, so the exit code
// must be stored BEFORE the fence closes.
func (p *ptyProcess) start() {
	p.done = make(chan struct{})
	go func() {
		// ProcessState.ExitCode: -1 when the child was killed by a
		// signal (Kill()'s SIGKILL included) - the session frames it
		// as the signal death it is.
		p.cmd.Wait()
		p.code = p.cmd.ProcessState.ExitCode()
		close(p.done)
	}()
}

func (p *ptyProcess) Done() <-chan struct{} {
	if p.done == nil {
		p.start()
	}
	return p.done
}

// Kill signals the child's process group (the child leads its own
// session, so -pid is the whole tree) and hands the master its close.
// The group's death hangs the pty up, so a parked read returns EIO at
// once and the deferred close (if any) lands on its heels - the door
// semantics stand: the reap rides Done's watcher, the exit watcher
// fires.
func (p *ptyProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	if err := unix.Kill(-p.cmd.Process.Pid, unix.SIGKILL); err != nil {
		_ = p.cmd.Process.Kill()
	}
	return p.closeMaster()
}

func (p *ptyProcess) ExitCode() int { return p.code }

// Resize sets the child's window size on the master.
func (p *ptyProcess) Resize(rows, cols uint16) error {
	ws := unix.Winsize{Row: rows, Col: cols, Xpixel: 0, Ypixel: 0}
	if err := unix.IoctlSetWinsize(int(p.master.Fd()), unix.TIOCSWINSZ, &ws); err != nil {
		return fmt.Errorf("resize: %w", err)
	}
	return nil
}
