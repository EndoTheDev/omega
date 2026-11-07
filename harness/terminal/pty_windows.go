//go:build windows

// Package terminal - the windows pty door: ConPTY (the Windows
// pseudoconsole API) behind the same process seam the linux build
// carries in pty_linux.go. The whole surface is reachable with
// golang.org/x/sys/windows alone (CreatePseudoConsole /
// ResizePseudoConsole / ClosePseudoConsole, StartInfoEx +
// ProcThreadAttributeListContainer, PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE
// are all exported from v0.47.0), so no vendored wrapper and no cgo -
// the linux door's dependency law stands.
package terminal

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// spawnProcess starts the shell attached to a fresh pseudoconsole,
// cwd=workdir. process (the seam interface) lives in terminal.go.
//
// Two pipes carry the console traffic: we write keystrokes into the
// input pipe's write end (ConPTY owns the read end) and read the
// output pipe's read end (ConPTY owns the write end). ConPTY's host
// (conhost.exe) keeps its own inherited copies of the child-side ends,
// so ours are closed the moment CreatePseudoConsole returns.
func spawnProcess(workdir string) (process, error) {
	var inR, inW, outR, outW windows.Handle
	if err := windows.CreatePipe(&inR, &inW, nil, 0); err != nil {
		return nil, fmt.Errorf("conpty input pipe: %w", err)
	}
	if err := windows.CreatePipe(&outR, &outW, nil, 0); err != nil {
		closeHandles(inR, inW)
		return nil, fmt.Errorf("conpty output pipe: %w", err)
	}
	// a real shell opens at a conventional size and Resize fixes it
	// with the pane's true geometry right after attach
	ptyCoord := windows.Coord{X: 120, Y: 30}
	var hPC windows.Handle
	if err := windows.CreatePseudoConsole(ptyCoord, inR, outW, 0, &hPC); err != nil {
		closeHandles(inR, inW, outR, outW)
		return nil, fmt.Errorf("CreatePseudoConsole: %w", err)
	}
	// conhost holds its inherited copies - our two child-side ends are
	// dead weight from here
	closeHandles(inR, outW)

	proc, err := attachShell(hPC, inW, outR, workdir)
	if err != nil {
		// the pty and both our ends die with the failed spawn
		windows.ClosePseudoConsole(hPC)
		closeHandles(inW, outR)
		return nil, err
	}
	return proc, nil
}

// procUpdateProcThreadAttribute is called raw (not through x/sys's
// ProcThreadAttributeListContainer.Update) because the pseudoconsole
// attribute is special twice over:
//
//   - its lpValue must carry the handle VALUE itself - the kernel
//     stores the raw handle, it does not copy from the pointed-to
//     address. Passing &hPC (the x/sys Update shape) hands the child
//     a garbage pseudoconsole handle: the shell attaches to nothing
//     and dies with STATUS_DLL_INIT_FAILED or sits alive-but-silent.
//     The MS sample, node-pty and every known-good wrapper pass the
//     value. That one-address bug burned this door's first
//     150-iteration run.
//   - x/sys's Update signature takes the value as unsafe.Pointer, and
//     converting a handle (uintptr) through unsafe.Pointer is a go
//     vet unsafepointer red; the raw call passes uintptrs end to end.
//
// The value shape is pinned by TestConPTYShellEcho.
var procUpdateProcThreadAttribute = windows.NewLazySystemDLL("kernel32.dll").
	NewProc("UpdateProcThreadAttribute")

func updatePseudoConsoleAttribute(list *windows.ProcThreadAttributeList,
	hPC windows.Handle) error {
	ret, _, callErr := procUpdateProcThreadAttribute.Call(
		uintptr(unsafe.Pointer(list)), // pointer->uintptr in call args: vet-legal
		0,
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		uintptr(hPC), // the handle VALUE rides lpValue itself
		unsafe.Sizeof(hPC),
		0,
		0)
	if ret == 0 {
		return callErr
	}
	return nil
}

// attachShell launches the shell with the pseudoconsole riding the
// process attribute list (the EXTENDED_STARTUPINFO_PRESENT shape -
// exec.Cmd cannot carry a custom attribute list, so CreateProcess is
// called directly).
func attachShell(hPC, inW, outR windows.Handle, workdir string) (*conptyProcess, error) {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, fmt.Errorf("attribute list: %w", err)
	}
	defer attrs.Delete()

	if err := updatePseudoConsoleAttribute(attrs.List(), hPC); err != nil {
		return nil, fmt.Errorf("pseudoconsole attribute: %w", err)
	}

	siex := &windows.StartupInfoEx{}
	siex.Cb = uint32(unsafe.Sizeof(*siex))
	siex.ProcThreadAttributeList = attrs.List()
	// STARTF_USESTDHANDLES with zero std handles: the child must open
	// CONIN$/CONOUT$ on ITS console (the pseudoconsole) instead of
	// inheriting the parent's console std handles. Without the flag the
	// shell attaches to the pty (the conhost handshake flows) but every
	// byte it writes lands on OMEGA'S console and the pane stays
	// alive-but-silent - the second bug that burned the first run,
	// pinned by TestConPTYShellEcho. This is the known-good wrapper
	// shape (conpty, node-pty): the zero std handles are the point,
	// not an omission.
	siex.Flags = windows.STARTF_USESTDHANDLES

	shell := shellPath()
	cmd16, err := windows.UTF16FromString(shell)
	if err != nil {
		return nil, fmt.Errorf("shell path: %w", err)
	}
	// CreateProcessW is allowed to mutate its command line; a spare
	// slot keeps the UTF16FromString copy untouched
	cmd16 = append(cmd16, 0)

	var cwdPtr *uint16
	if workdir != "" {
		if cwdPtr, err = windows.UTF16PtrFromString(workdir); err != nil {
			return nil, fmt.Errorf("workdir: %w", err)
		}
	}

	var pi windows.ProcessInformation
	// startupInfo points at the STARTUPINFOEX prefix: Cb already
	// declares the ex size, and CreateProcessW reads the attribute
	// list past it. The child inherits our environment.
	if err := windows.CreateProcess(nil, &cmd16[0], nil, nil, false,
		windows.EXTENDED_STARTUPINFO_PRESENT,
		nil, cwdPtr, &siex.StartupInfo, &pi); err != nil {
		return nil, fmt.Errorf("start %s: %w", shell, err)
	}
	windows.CloseHandle(pi.Thread) // the primary thread handle is dead weight

	return &conptyProcess{
		hPC:      hPC,
		inW:      inW,
		outR:     outR,
		hProcess: pi.Process,
		pid:      int(pi.ProcessId),
	}, nil
}

// closeHandles squashes a cleanup list - every handle is closed best-
// effort; spawn's error already names the real failure.
func closeHandles(h ...windows.Handle) {
	for _, handle := range h {
		_ = windows.CloseHandle(handle)
	}
}

// shellPath picks the shell the way VS Code's Windows profiles do:
// ComSpec (cmd.exe on virtually every box) is the bulletproof ConPTY
// client, PowerShell the capable fallback, bare "cmd.exe" the root
// drive's last resort.
func shellPath() string {
	if comspec := os.Getenv("COMSPEC"); comspec != "" {
		return comspec
	}
	root := os.Getenv("SystemRoot")
	if root != "" {
		cmd := filepath.Join(root, "System32", "cmd.exe")
		if exist(cmd) {
			return cmd
		}
		ps := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		if exist(ps) {
			return ps
		}
	}
	return "cmd.exe"
}

func exist(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// conptyProcess is the windows process: the pseudoconsole, our two
// pipe ends, and the shell's process handle.
type conptyProcess struct {
	hPC       windows.Handle
	inW       windows.Handle // keystrokes -> ConPTY
	outR      windows.Handle // ConPTY -> shell output
	hProcess  windows.Handle
	pid       int
	done      chan struct{}
	code      int
	closeOnce sync.Once
}

// Read blocks on the output pipe; the hangup shape matches the unix
// master: ConPTY's host exits (child exit or pseudoconsole close) and
// the read returns an error.
func (p *conptyProcess) Read(b []byte) (int, error) {
	var n uint32
	err := windows.ReadFile(p.outR, b, &n, nil)
	if n > 0 {
		return int(n), err
	}
	if err != nil {
		return 0, err
	}
	// a synchronous pipe read never lands here, but a zero-byte read
	// with no error is EOF-shaped regardless
	return 0, fmt.Errorf("terminal: conpty read returned zero bytes")
}

func (p *conptyProcess) Write(b []byte) (int, error) {
	var n uint32
	if err := windows.WriteFile(p.inW, b, &n, nil); err != nil {
		return int(n), err
	}
	return int(n), nil
}

// closePTY tears the pseudoconsole down exactly once - the ConPTY
// answer to the linux door's master close. ClosePseudoConsole is the
// hangup door: the host flushes its last output through the pipe and
// exits, which unblocks (and errors) every pending read, the same
// shape a slave-side EOF gives the linux master. The child's own
// death does NOT do this on windows - the host owns the pipe ends -
// so both Kill and the exit watcher route through here.
func (p *conptyProcess) closePTY() {
	p.closeOnce.Do(func() { windows.ClosePseudoConsole(p.hPC) })
}

// Close is the idempotent teardown: the pseudoconsole (once), then
// our two pipe ends and the process handle.
func (p *conptyProcess) Close() error {
	p.closePTY()
	closeHandles(p.inW, p.outR, p.hProcess)
	return nil
}

// start runs the watcher: wait for exit, record the code, hang the
// console up, then close the done fence. The close-before-fence order
// is the hangup contract - a read parked on the output pipe is
// released by the console teardown, not by the fence. The session's
// watchLoop owns the exit broadcast, so the code must be stored
// BEFORE the fence closes.
func (p *conptyProcess) start() {
	p.done = make(chan struct{})
	go func() {
		_, _ = windows.WaitForSingleObject(p.hProcess, windows.INFINITE)
		var code uint32
		if err := windows.GetExitCodeProcess(p.hProcess, &code); err != nil {
			p.code = -1
		} else {
			p.code = int(code)
		}
		p.closePTY()
		close(p.done)
	}()
}

func (p *conptyProcess) Done() <-chan struct{} {
	if p.done == nil {
		p.start()
	}
	return p.done
}

// Kill terminates the shell and hangs the console up - the Terminate
// Process answer to the linux door's SIGKILL-then-master-close pair.
// Windows has no process-group notion worth racing; the host hangs up
// on its own once the console tears down (closePTY).
func (p *conptyProcess) Kill() error {
	err := windows.TerminateProcess(p.hProcess, 1)
	p.closePTY()
	return err
}

func (p *conptyProcess) ExitCode() int { return p.code }

// Resize sets the pseudoconsole's buffer size (X=columns, Y=rows) -
// the ConPTY answer to TIOCSWINSZ, the child gets a window-size
// change event instead of a signal.
func (p *conptyProcess) Resize(rows, cols uint16) error {
	return windows.ResizePseudoConsole(p.hPC, windows.Coord{X: int16(cols), Y: int16(rows)})
}
