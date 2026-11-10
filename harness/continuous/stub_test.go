package continuous

// stub_test.go: the windows wake-child stub. CreateProcess cannot run
// a shebang script, and a .bat sleeper spawns a grandchild that keeps
// the wake log open after the kill - an open file cannot be removed,
// so the temp-dir cleanup dies with it. The stub is a compiled Go exe
// instead: one process, no grandchildren, TestMain builds it once per
// test run. Posix keeps the sh stub in wake_test.go.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// stubSource is the wake-child stub program: it records argv, cwd, and
// the OMEGA_ env deltas into the file named by WAKE_STUB_OUT, prints
// the DONE sentinel, then sleeps out its 30s budget.
const stubSource = `package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

func main() {
	out := os.Getenv("WAKE_STUB_OUT")
	if out == "" {
		return
	}
	f, err := os.OpenFile(out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	cwd, _ := os.Getwd()
	fmt.Fprintf(f, "ARGV:%s\n", strings.Join(os.Args[1:], " "))
	fmt.Fprintf(f, "PWD:%s\n", cwd)
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "OMEGA_") {
			env = append(env, e)
		}
	}
	sort.Strings(env)
	for _, e := range env {
		fmt.Fprintln(f, e)
	}
	fmt.Fprintln(f, "DONE")
	// The stub never exits on its own inside a test's window: the
	// sleep IS the live wake the guards observe and Sleep breaks.
	time.Sleep(30 * time.Second)
}
`

// stubExePath is the built stub binary for this test run (windows only).
var stubExePath string

func TestMain(m *testing.M) {
	if runtime.GOOS == "windows" {
		dir, err := os.MkdirTemp("", "wake-stub")
		if err != nil {
			fmt.Fprintf(os.Stderr, "wake stub: %v\n", err)
			os.Exit(1)
		}
		defer os.RemoveAll(dir)
		src := filepath.Join(dir, "stub_main.go")
		if err := os.WriteFile(src, []byte(stubSource), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "wake stub: %v\n", err)
			os.Exit(1)
		}
		exe := filepath.Join(dir, "wake-stub.exe")
		build := exec.Command("go", "build", "-o", exe, src)
		if out, err := build.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "wake stub build: %v\n%s\n", err, out)
			os.Exit(1)
		}
		stubExePath = exe
	}
	os.Exit(m.Run())
}

// wakeSleep ends a live wake and waits for it to die: the roster's
// sleep button plus the death the guards observe. Windows cannot
// remove a temp-dir file an in-flight wake still holds, so a test
// never ends with a live child.
func wakeSleep(t *testing.T, m *Machine, name string) {
	t.Helper()
	m.Sleep(name)
	deadline := time.Now().Add(testWaitTimeout)
	for time.Now().Before(deadline) && m.LiveWake(name) {
		time.Sleep(50 * time.Millisecond)
	}
	if m.LiveWake(name) {
		t.Fatalf("wake %s never cleared after sleep", name)
	}
}
