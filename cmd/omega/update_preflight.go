package main

// The pre-flight copy-boot, the never-finalize guard: boot the staged binary against
// a COPY of the closed store, require /health ok AND migrations green, and only then is
// the staged release finalize-able. A migration failure is zero-downtime: the copy is
// thrown away, the staged artifact is discarded, and the running process never notices.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/harness/store"
)

// cappedBuffer keeps the LAST n bytes of a child's output - a migration failure prints
// its cause at the end, and an unbounded buffer would grow with a chatty boot.
type cappedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	b.buf.Write(p)
	if b.max > 0 && b.buf.Len() > b.max {
		drop := b.buf.Len() - b.max
		rest := append([]byte(nil), b.buf.Bytes()[drop:]...)
		b.buf.Reset()
		b.buf.Write(rest)
	}
	return n, nil
}

func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// copyBoot boots the staged binary on a throwaway home whose store is a snapshot of the
// live one. Migrations run there; nothing the child writes can reach the live install.
func (m *updateManager) copyBoot(ctx context.Context, staged, want string) error {
	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}
	home, err := os.MkdirTemp("", "omega-preflight-")
	if err != nil {
		return fmt.Errorf("pre-flight home: %w", err)
	}
	defer os.RemoveAll(home)
	for _, dir := range []string{"config", "data"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o700); err != nil {
			return fmt.Errorf("pre-flight home: %w", err)
		}
	}
	dbCopy := filepath.Join(home, "data", "omega.db")
	// The copy travels through SQLite (VACUUM INTO) because the source is live: a byte
	// copy of a WAL-mode store is not a database.
	if err := store.SnapshotStore(cfg.Store.DBPath, dbCopy); err != nil {
		return fmt.Errorf("copy the store: %w", err)
	}
	port, err := freeLoopbackPort()
	if err != nil {
		return fmt.Errorf("reserve a probe port: %w", err)
	}
	doc, err := preflightConfigYAML(cfg, port, dbCopy)
	if err != nil {
		return fmt.Errorf("write the pre-flight config: %w", err)
	}
	cfgPath := filepath.Join(home, "config", "config.yaml")
	if err := os.WriteFile(cfgPath, doc, 0o600); err != nil {
		return fmt.Errorf("write the pre-flight config: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, m.preflightLimit)
	defer cancel()
	cmd := exec.CommandContext(runCtx, staged, "serve", "--config", cfgPath)
	cmd.Env = preflightEnv(home)
	var stderr cappedBuffer
	stderr.max = 8 << 10
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start the staged binary: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	origin := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(m.preflightLimit)
	for {
		select {
		case waitErr := <-exited:
			// The process is gone: its own words are the receipt. A migration failure is
			// refused verbatim, exactly as the boot printed it.
			if msg := strings.TrimSpace(stderr.String()); msg != "" {
				return fmt.Errorf("%s", lastLines(msg, 6))
			}
			return fmt.Errorf("staged binary exited during its copy-boot: %v", waitErr)
		default:
		}
		probeCtx, probeCancel := context.WithTimeout(ctx, 2*time.Second)
		err := m.probeVersion(probeCtx, origin, want)
		probeCancel()
		if err == nil {
			stopChild(cmd, exited)
			return nil
		}
		if time.Now().After(deadline) {
			stopChild(cmd, exited)
			return fmt.Errorf("copy-boot never answered /health as %s within %s: %v%s", want, m.preflightLimit, err, tailOf(stderr.String()))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// stopChild kills a probe child and reaps it so no orphan outlives the pre-flight.
func stopChild(cmd *exec.Cmd, exited chan error) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
	}
}

// tailOf renders a child's captured stderr as a detail suffix.
func tailOf(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return ": " + lastLines(s, 6)
}

// lastLines keeps the newest n lines of a captured output.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
