package main

// The host-side update chores that are not per-request: the auto-check ladder and the
// quiet gate that keeps an opted-in auto-update from restarting a busy home. The
// routes, the tray and the pane all read the same manager.

import (
	"context"
	"time"
)

// runAutoUpdates is started by the host after the channels are up (the goroutine fence is
// for modules; this is host work). It never blocks the boot and stops with the process.
func (m *updateManager) runAutoUpdates(ctx context.Context) {
	first := time.NewTimer(updateFirstCheckDelay)
	select {
	case <-ctx.Done():
		first.Stop()
		return
	case <-first.C:
	}
	for {
		m.autoTick(ctx)
		next := time.NewTimer(updateCheckInterval)
		select {
		case <-ctx.Done():
			next.Stop()
			return
		case <-next.C:
		}
	}
}

// autoTick is one pass: check when auto-check is on, and stage+finalize only when the
// user opted into auto-update AND the system is quiet. A busy home keeps the staged
// release and lets the pane ask.
func (m *updateManager) autoTick(ctx context.Context) {
	cfg, err := m.loadConfig()
	if err != nil {
		return
	}
	if !managedBuild(cfg, omegaVersion) || !cfg.Updates.AutoCheck {
		return
	}
	if _, err := m.Check(ctx, ""); err != nil {
		m.logf("auto-check: %v", err)
		return
	}
	st := m.Status()
	if st.State != updateAvailable || st.Available == "" {
		return
	}
	m.logf("auto-check: %s available (channel %s)", st.Available, st.Channel)
	if !cfg.Updates.AutoUpdate {
		return
	}
	if _, err := m.Stage(ctx, ""); err != nil {
		m.logf("auto-update: stage %s: %v", st.Available, err)
		return
	}
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		st = m.Status()
		switch st.State {
		case updateReady, updateFailed, updateUpToDate, updateIdle:
			goto staged
		}
		time.Sleep(2 * time.Second)
	}
staged:
	if st.State != updateReady {
		return
	}
	if n := m.activeRuns(); n > 0 {
		m.logf("auto-update: %d run(s) active - %s staged, the restart waits for the pane", n, st.StagedVer)
		return
	}
	if _, err := m.Finalize(ctx, false); err != nil {
		m.logf("auto-update: %v", err)
	}
}

// waitFinalize blocks while a finalize owns the process fate. main must not return out
// from under the supervisor: on Windows the parent is the one watching the window.
func (m *updateManager) waitFinalize() {
	m.mu.Lock()
	done := m.finalizeDone
	finalizing := m.st.State == updateFinalizing
	m.mu.Unlock()
	if done == nil || !finalizing {
		return
	}
	<-done
}
