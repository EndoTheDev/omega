package main

// The finalize orchestration, shared by both hosts: validate, snapshot, hand off to the
// platform mechanism, supervise nothing itself. The platform half lives behind ONE seam -
// platformFinalize - selected at build time (update_finalize_windows.go /
// update_finalize_other.go). Both hosts answer the same contract: exactly one
// restart, a supervised window, an automatic revert when the window fails.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// finalizeRequest carries everything a platform finalize needs; every side effect rides a
// field so the tests never touch the real install.
type finalizeRequest struct {
	ExePath    string
	StagedPath string
	NewVersion string
	ConfigPath string
	Origin     string
	PortAddr   string
	Window     time.Duration

	// Stop gracefully stops the serving runtime (the parent must release the port and the
	// store before the child boots). Unix exec needs none: the image is replaced.
	Stop func()
	// Checkpoint re-takes the store snapshot once Stop has closed the store: the serve
	// accepted writes until that moment, and a row committed before the stop must still be
	// in the rollback point. Unix exec has no stop to hook, so its snapshot stays the
	// pre-finalize copy.
	Checkpoint func() error
	// Probe reads one /health answer and requires the new version.
	Probe func(ctx context.Context, origin, want string) error
	// Spawn starts the binary at ExePath detached and returns its handle.
	Spawn func(configPath string) (finalizeChild, error)
	// Swap installs StagedPath over ExePath, preserving omega.bak.
	Swap func(src, dst string) error
	// Rollback puts omega.bak back into the executable slot.
	Rollback func(exePath, bakPath string) error
	Sleep    func(time.Duration)
	Logf     func(string, ...any)
}

// finalizeChild is the restarted process the supervisor may have to kill.
type finalizeChild interface {
	Kill() error
}

// finalizeOutcome reports what the failed platform finalize left behind.
type finalizeOutcome struct {
	// Serving: the old process is still healthy and answering (nothing was stopped).
	Serving bool
}

// finalizePlatform is the ONE host-mode seam: the build tag picks the mechanism
// (update_finalize_windows.go / update_finalize_other.go), the contract stays
// platform-stable.

// Finalize validates, snapshots, and starts the restart. It answers before the restart so
// the caller gets the status; the window and the revert run behind it.
func (m *updateManager) Finalize(ctx context.Context, force bool) (core.UpdateStatus, error) {
	cfg, err := m.loadConfig()
	if err != nil {
		return m.fail(err)
	}
	if !managedBuild(cfg, omegaVersion) {
		return m.fail(fmt.Errorf("this build is not release-managed (version %s): the dev lane never self-updates", omegaVersion))
	}
	exePath, err := m.exePath()
	if err != nil {
		return m.fail(fmt.Errorf("resolve executable: %w", err))
	}
	staged := exePath + stagedSuffix
	if _, err := os.Stat(staged); err != nil {
		return m.fail(fmt.Errorf("nothing staged - check and stage a release first"))
	}

	m.mu.Lock()
	if m.busy {
		st := m.st
		m.mu.Unlock()
		return st, errUpdateBusy
	}
	if m.st.State != updateReady || m.st.StagedVer == "" {
		st := m.st
		m.mu.Unlock()
		return st, fmt.Errorf("no pre-flight-green release staged (state %s)", st.State)
	}
	active := m.activeRuns()
	if active > 0 && !force {
		// auto-update never restarts under a live run. A human may force it.
		st := m.st
		m.mu.Unlock()
		return st, busyRefusal(active)
	}
	m.busy = true
	m.st.State = updateFinalizing
	m.st.Error = ""
	version := m.st.StagedVer
	done := make(chan struct{})
	m.finalizeDone = done
	m.mu.Unlock()

	snap, err := snapshotBeforeFinalize(cfg.Store.DBPath, m.configPath, version)
	if err != nil {
		m.mu.Lock()
		m.busy = false
		m.mu.Unlock()
		return m.fail(err)
	}

	req := finalizeRequest{
		ExePath:    exePath,
		StagedPath: staged,
		NewVersion: version,
		ConfigPath: m.configPath,
		Origin:     m.origin,
		PortAddr:   hostPort(m.origin),
		Window:     m.healthWindow,
		Stop:       m.stopServing,
		Checkpoint: func() error { return recheckpointSnapshot(snap) },
		Probe:      m.probeVersion,
		Spawn:      m.spawn,
		Swap:       m.swap,
		Rollback:   m.rollback,
		Sleep:      m.sleep,
		Logf:       m.logf,
	}
	go m.finalizeRun(req, snap, active, done)
	return m.Status(), nil
}

// finalizeRun is the restart behind the accepted finalize: platform finalize, then the
// receipt, then the process fate.
func (m *updateManager) finalizeRun(req finalizeRequest, snap *updateSnapshot, activeAtShutdown int, done chan struct{}) {
	defer close(done)
	// The 202 must reach the caller before the listener goes away.
	m.sleep(300 * time.Millisecond)
	outcome, err := m.finalizeStep(req)
	if err == nil {
		receipt := core.UpdateReceipt{Version: req.NewVersion, Outcome: "pass", At: m.now().Unix()}
		m.recordReceipt(receipt)
		// The staged artifact is the installed binary's duplicate now: drop it so a later
		// check cannot finalize the same release twice.
		_ = os.Remove(req.StagedPath)
		resumed := m.resumedRuns(req.Origin)
		m.logf("update %s: window passed (health ok, version confirmed); resume receipt: %d active at shutdown, %d running after boot",
			req.NewVersion, activeAtShutdown, resumed)
		m.exit(0)
		return
	}
	restored, restoreErr := restoreSnapshot(snap)
	receipt := core.UpdateReceipt{
		Version:  req.NewVersion,
		Outcome:  "rollback",
		Why:      err.Error(),
		Restored: restored,
		At:       m.now().Unix(),
	}
	m.recordReceipt(receipt)
	os.Remove(req.StagedPath)
	m.logf("update %s rolled back: %v (restored: %s)", req.NewVersion, err, strings.Join(restored, ", "))
	if restoreErr != nil {
		m.logf("update %s rollback: the store restore failed: %v", req.NewVersion, restoreErr)
	}
	if outcome.Serving {
		// Nothing was stopped (the Unix exec path refused): keep serving, let the user retry.
		m.set(func(s *core.UpdateStatus) {
			s.State = updateFailed
			s.Error = err.Error()
			s.Receipt = &receipt
		})
		m.mu.Lock()
		m.busy = false
		m.mu.Unlock()
		return
	}
	// The listener is gone: bring the old binary back up before exiting.
	if _, serr := req.Spawn(req.ConfigPath); serr != nil {
		m.logf("rollback: restarting the old binary failed: %v", serr)
	}
	m.exit(1)
}

// recordReceipt stores the receipt on the status and on disk: a rollback respawns the old
// binary as a NEW process, and the pane must still show why it reverted.
func (m *updateManager) recordReceipt(r core.UpdateReceipt) {
	m.mu.Lock()
	m.receipt = &r
	m.st.Receipt = &r
	m.mu.Unlock()
	if path := m.receiptPath(); path != "" {
		if data, err := json.Marshal(r); err == nil {
			_ = os.WriteFile(path, data, 0o600)
		}
	}
}

// receiptPath is <data>/.update-receipt.json beside the store.
func (m *updateManager) receiptPath() string {
	cfg, err := m.loadConfig()
	if err != nil {
		return ""
	}
	if cfg.Store.DBPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(cfg.Store.DBPath), ".update-receipt.json")
}

// loadReceipt reads the retained receipt, if any.
func loadReceipt(path string) *core.UpdateReceipt {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var r core.UpdateReceipt
	if err := json.Unmarshal(data, &r); err != nil {
		return nil
	}
	return &r
}

// resumedRuns reads the restarted process's live run count: the resume receipt's second
// half, evidence for the log only (R5 - never a pass condition).
func (m *updateManager) resumedRuns(origin string) int {
	req, err := http.NewRequest(http.MethodGet, strings.TrimSuffix(origin, "/")+"/updates/status", nil)
	if err != nil {
		return 0
	}
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	var st core.UpdateStatus
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&st); err != nil {
		return 0
	}
	return st.ActiveRuns
}

// superviseWindow is the shared 30s window: /health ok AND version == want, or the
// platform half reverts. Sleep and Probe are seams so a test never waits 30s.
func superviseWindow(req finalizeRequest) error {
	deadline := time.Now().Add(req.Window)
	var last error
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		last = req.Probe(ctx, req.Origin, req.NewVersion)
		cancel()
		if last == nil {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("health window failed after %s: %w", req.Window, last)
		}
		req.Sleep(250 * time.Millisecond)
	}
}
