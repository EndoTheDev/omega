//go:build windows

package terminal

import "testing"

// The serializer's windows pin (card t_82107785, split out of
// process_test.go by card t_590976b0): the shell string must ride a
// RAW child command line so no argv escaping layer can rewrap it.
// syscall.SysProcAttr.CmdLine exists only on windows, so the field
// read lives behind this build tag - a runtime guard is not a
// compile guard, and the guarded read red the linux vet lane
// (nightly 42410701275).
func TestBuildShellRawCommandLineWindows(t *testing.T) {
	command := `powershell -NoProfile -Command "$x=5; Write-Output $x"`
	c := detachedShellCommand(command)
	if c.SysProcAttr == nil || c.SysProcAttr.CmdLine != "cmd /C "+command {
		t.Fatalf("the shell string must ride a raw command line verbatim, got CmdLine=%q", c.SysProcAttr.CmdLine)
	}
	if len(c.Args) != 1 {
		t.Fatalf("argv must stay empty (nothing to escape), got %v", c.Args)
	}
}
