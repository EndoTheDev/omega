package delegate

// delegate_agent_test.go: the delegate agent target (card t_7e2b7002).
// The child must receive OMEGA_AGENT=<name>; a ghost name must fail at
// spawn time, not silently run on unconfigured defaults.

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/engine/agent"
)

// writeAgentStub is the writeStub pattern plus an OMEGA_AGENT line.
func writeAgentStub(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "stub-agent-omega")
	var body string
	if runtime.GOOS == "windows" {
		bin += ".bat"
		body = "@echo off\r\necho agent=%OMEGA_AGENT%\r\nexit /b 0\r\n"
	} else {
		bin += ".sh"
		body = "#!/bin/sh\necho \"agent=$OMEGA_AGENT\"\nexit 0\n"
	}
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func scaffoldScout(t *testing.T) string {
	t.Helper()
	t.Setenv("OMEGA_HOME", t.TempDir())
	home := os.Getenv("OMEGA_HOME")
	if err := agent.ScaffoldAgent(home, "scout", "test agent"); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestDelegateAgentTargetEnv(t *testing.T) {
	scaffoldScout(t)
	bin := writeAgentStub(t)
	t.Setenv("OMEGA_BIN", bin)

	d := NewDelegate()
	out, err := d.runDelegateTask(context.Background(), map[string]any{
		"prompt": "probe",
		"agent":  "scout",
		// wait for the stub itself so the child actually runs
		"timeout": 10,
	})
	if err != nil {
		t.Fatalf("delegate to scout: %v (%s)", err, out)
	}
	// The result lands async, but the spawn must carry the env var.
	// The stub result is injected; poll the injected channel.
	select {
	case msg := <-d.injected:
		if !strings.Contains(msg.text, "agent=scout") {
			t.Errorf("child saw agent=%q... want agent=scout", extractAgent(msg.text))
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("no injected result in 10s: %s", out)
	}
}

func TestDelegateUnknownAgentFails(t *testing.T) {
	scaffoldScout(t)
	bin := writeAgentStub(t)
	t.Setenv("OMEGA_BIN", bin)

	d := NewDelegate()
	out, err := d.runDelegateTask(context.Background(), map[string]any{
		"prompt": "probe",
		"agent":  "ghost",
	})
	if err == nil {
		t.Errorf("ghost agent accepted, output %q", out)
	}
	if !strings.Contains(err.Error(), "unknown agent") {
		t.Errorf("error = %v, want unknown-agent message", err)
	}
}

// extractAgent pulls the agent= value from a stub result line.
func extractAgent(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if v, ok := strings.CutPrefix(line, "agent="); ok {
			return v
		}
	}
	return ""
}
