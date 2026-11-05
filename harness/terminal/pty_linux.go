//go:build linux

// Package terminal — the PTY seam: one real pty pair per terminal
// session, the shell running on the child side. Stdlib-only plus
// golang.org/x/sys/unix (already a dependency): no cgo, no pty library.
//
// Build-tagged: the pty vocabulary is linux-only (/dev/ptmx + the Linux
// pty ioctl set — darwin carries no equivalent constants, so its build
// never reaches here). pty_other.go carries the !linux stub so the module
// still compiles on every other platform - spawn returns an error, the
// routes answer it.
package terminal

import (
	"fmt"
	"os"
	"os/exec"

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
}

func (p *ptyProcess) Read(b []byte) (int, error)  { return p.master.Read(b) }
func (p *ptyProcess) Write(b []byte) (int, error) { return p.master.Write(b) }
func (p *ptyProcess) Close() error                { return p.master.Close() }

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
// session, so -pid is the whole tree) and closes the master, which
// hangs the slave up.
func (p *ptyProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	if err := unix.Kill(-p.cmd.Process.Pid, unix.SIGKILL); err != nil {
		_ = p.cmd.Process.Kill()
	}
	return p.master.Close()
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
