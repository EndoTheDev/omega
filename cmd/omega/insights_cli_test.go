package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/extensions/store"
)

// setupInsightsEnv prepares an OMEGA_HOME temp dir with a config.yaml and a
// store. When withSession is true it creates a session with two messages.
// Mirrors setupExportEnv in export_test.go.
func setupInsightsEnv(t *testing.T, withSession bool) (home, sessionID string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("OMEGA_HOME", home)

	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(
		"provider:\n  model_name: test-model\nserver:\n  port: 8099\n",
	), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	s, err := store.Open(filepath.Join(home, "omega.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	if withSession {
		ctx := context.Background()
		sessionID = "test-session"
		if err := s.CreateSession(ctx, sessionID, "", "insights-test"); err != nil {
			t.Fatalf("create session: %v", err)
		}
		if err := s.AppendMessage(ctx, sessionID, ai.NewUser("hello world")); err != nil {
			t.Fatalf("append user message: %v", err)
		}
		if err := s.AppendMessage(ctx, sessionID, ai.NewAssistant("hi there")); err != nil {
			t.Fatalf("append assistant message: %v", err)
		}
	}
	return home, sessionID
}

// captureStdout swaps os.Stdout for a pipe, runs fn, and returns what was
// written plus any error. Restores os.Stdout via t.Cleanup.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() { os.Stdout = old })
	os.Stdout = w

	// Run fn synchronously so all writes land on the pipe before we close it.
	runErr := fn()

	w.Close()
	os.Stdout = old

	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, rerr := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if rerr != nil {
			break
		}
	}
	r.Close()

	return string(buf), runErr
}

func TestCmdInsightsWithSessions(t *testing.T) {
	setupInsightsEnv(t, true)

	out, err := captureStdout(t, func() error { return cmdInsights("", nil) })
	if err != nil {
		t.Fatalf("cmdInsights: %v", err)
	}
	if !strings.Contains(out, "Sessions:") {
		t.Errorf("output missing 'Sessions:' header:\n%s", out)
	}
}

func TestCmdInsightsNoSessions(t *testing.T) {
	setupInsightsEnv(t, false)

	out, err := captureStdout(t, func() error { return cmdInsights("", nil) })
	if err != nil {
		t.Fatalf("cmdInsights: %v", err)
	}
	if !strings.Contains(out, "No sessions") {
		t.Errorf("output missing 'No sessions':\n%s", out)
	}
}

func TestCmdInsightsDaysFlag(t *testing.T) {
	setupInsightsEnv(t, false)

	if err := cmdInsights("", []string{"--days", "7"}); err != nil {
		t.Fatalf("cmdInsights --days 7: %v", err)
	}
}