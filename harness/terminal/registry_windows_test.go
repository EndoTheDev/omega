//go:build windows

package terminal

import "testing"

// The serializer's windows pin: the shell string must ride a RAW child command line.
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
