package main

// The update channel's serve-mode contract: channel parity with the CLI, release-managed
// scoping, the never-finalize copy-boot gate, the quiet check, the window's version
// requirement, and the snapshot/rollback shapes. No test here touches the real install or
// waits the real 30s window: every side effect rides a seam.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// updateTestManager builds a serve-mode manager over a temp home whose config declares a
// release-managed install, with the clock seams collapsed.
func updateTestManager(t *testing.T) (*updateManager, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o755); err != nil {
		t.Fatalf("home: %v", err)
	}
	cfgPath := filepath.Join(home, "config", "config.yaml")
	cfg := "provider:\n  model_name: mock\nupdates:\n  managed: on\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("config: %v", err)
	}
	m := newUpdateManager(cfgPath, "http://127.0.0.1:1", nil)
	m.sleep = func(time.Duration) {}
	return m, cfgPath
}

// makeTestStore writes a real SQLite file so checkpoint, snapshot and restore run against
// the shape the production store has.
func makeTestStore(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("store dir: %v", err)
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("store open: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS probes (n INTEGER)`); err != nil {
		t.Fatalf("store schema: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO probes (n) VALUES (1)`); err != nil {
		t.Fatalf("store row: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("store close: %v", err)
	}
}

func TestManagedBuildDerivesFromStamp(t *testing.T) {
	cases := []struct {
		managed, version string
		want             bool
	}{
		{"", "v0.19.0", true},
		{"", "v0.19.0-nightly.4", true},
		{"", "v0.18.0-abc12345", false}, // dev build: tag + local hash
		{"", "dev", false},              // unstamped module build
		{"auto", "v0.19.0", true},
		{"on", "v0.18.0-abc12345", true},   // explicit declaration wins
		{"off", "v0.19.0", false},          // explicit declaration wins
		{"true", "dev", true},              // alias accepted
		{"no", "v0.19.0-nightly.1", false}, // alias accepted
	}
	for _, tt := range cases {
		cfg := Config{}
		cfg.Updates.Managed = tt.managed
		if got := managedBuild(cfg, tt.version); got != tt.want {
			t.Errorf("managedBuild(managed=%q, version=%q) = %v, want %v", tt.managed, tt.version, got, tt.want)
		}
	}
}

// TestUpdateChannelParity pins the same table the CLI resolves: a -nightly.*
// prerelease sorts BELOW its stable target, and the nightly channel never offers a stable
// tag.
func TestUpdateChannelParity(t *testing.T) {
	if !compareTagNewer("v0.19.0", "v0.19.0-nightly.7") {
		t.Fatal("stable v0.19.0 does not sort above its nightly - a nightly could outrank its release")
	}
	if compareTagNewer("v0.19.0-nightly.7", "v0.19.0") {
		t.Fatal("nightly sorts above its stable target - switching channels would read as a downgrade")
	}
	if !compareTagNewer("v0.19.0-nightly.8", "v0.19.0-nightly.7") {
		t.Fatal("nightly ordering is broken")
	}
	rels := []githubRelease{
		{TagName: "v0.19.0"},
		{TagName: "v0.19.0-nightly.9", Prerelease: true},
		{TagName: "v0.19.0-nightly.10", Prerelease: true},
		{TagName: "v0.18.0"},
	}
	if tags := channelTags(rels, false); len(tags) != 2 || tags[0] != "v0.19.0" || tags[1] != "v0.18.0" {
		t.Fatalf("stable channel tags = %q, want the two stable tags only", tags)
	}
	if tags := channelTags(rels, true); len(tags) != 2 {
		t.Fatalf("nightly channel tags = %q, want the two nightlies", tags)
	}
	got, err := newestNightly(rels)
	if err != nil {
		t.Fatalf("newestNightly: %v", err)
	}
	if got.TagName != "v0.19.0-nightly.10" {
		t.Fatalf("newestNightly = %q, want v0.19.0-nightly.10", got.TagName)
	}
	// The gate: offered == current skips, newer installs, older refuses without the flag.
	if gate, _ := updateGate("v0.19.0", "v0.19.0", false); gate != "skip" {
		t.Fatalf("gate same version = %q, want skip", gate)
	}
	if _, err := updateGate("v0.18.0", "v0.19.0", false); err == nil {
		t.Fatal("the gate allowed a downgrade without --allow-downgrade")
	}
}

// TestFindAssetDesktopNameDeconflicted is R10 in both directions: the desktop token never
// matches the CLI's GOOS_GOARCH test, and a real windows_amd64 asset still does.
func TestFindAssetDesktopNameDeconflicted(t *testing.T) {
	assets := []githubAsset{
		{Name: "omega-v0.19.0-windows-desktop.zip", BrowserDownloadURL: "https://example.com/desktop.zip"},
	}
	if got := findAsset(assets, "windows", "amd64"); got != "" {
		t.Fatalf("findAsset selected the desktop asset %q for windows/amd64 - the CLI matcher was widened", got)
	}
	assets = append(assets, githubAsset{Name: "omega-v0.19.0-windows_amd64.zip", BrowserDownloadURL: "https://example.com/win.zip"})
	if got := findAsset(assets, "windows", "amd64"); got != "https://example.com/win.zip" {
		t.Fatalf("findAsset = %q, want the windows_amd64 asset", got)
	}
	// Order must not matter: the desktop name is never a prefix-match of the platform tag.
	if strings.Contains("omega-v0.19.0-windows-desktop.zip", assetNameForOS("windows", "amd64")) {
		t.Fatal("the desktop asset name carries the windows_amd64 token")
	}
}

// TestPreflightGateNeverFinalizes is the v0.18.0 incident as a pin: a staged binary whose
// migrations fail is refused, discarded, and the running process is untouched.
func TestPreflightGateNeverFinalizes(t *testing.T) {
	m, _ := updateTestManager(t)
	exe := filepath.Join(t.TempDir(), "omega.exe")
	staged := exe + stagedSuffix
	if err := os.WriteFile(staged, []byte("staged"), 0o700); err != nil {
		t.Fatalf("staged: %v", err)
	}
	migrationErr := "migration 12: table sessions already exists"
	m.preflightBoot = func(context.Context, string, string) error { return errors.New(migrationErr) }

	err := m.preflightGate(context.Background(), staged, "v9.9.9")
	if err == nil {
		t.Fatal("preflightGate accepted a copy-boot whose migrations failed")
	}
	st := m.Status()
	if st.State != updateFailed {
		t.Fatalf("state = %q after a failed copy-boot, want %q", st.State, updateFailed)
	}
	if !strings.Contains(st.Error, migrationErr) {
		t.Fatalf("status error %q does not carry the migration failure verbatim", st.Error)
	}
	if st.StagedVer != "" {
		t.Fatalf("a refused stage kept staged_version %q", st.StagedVer)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("the staged artifact survived a NEVER-FINALIZE refusal (err %v)", err)
	}
}

// TestFinalizeRefusesBusyHome is R7's quiet gate: a live run makes the restart a prompt,
// and only force goes through.
func TestFinalizeRefusesBusyHome(t *testing.T) {
	m, _ := updateTestManager(t)
	exe := filepath.Join(t.TempDir(), "omega.exe")
	m.exePath = func() (string, error) { return exe, nil }
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatalf("exe: %v", err)
	}
	if err := os.WriteFile(exe+stagedSuffix, []byte("new"), 0o700); err != nil {
		t.Fatalf("staged: %v", err)
	}
	m.set(func(s *core.UpdateStatus) { s.State = updateReady; s.StagedVer = "v9.9.9" })
	m.activeRuns = func() int { return 2 }

	if _, err := m.Finalize(context.Background(), false); err == nil {
		t.Fatal("Finalize restarted the host under two live runs without force")
	}
	if _, err := os.Stat(exe + stagedSuffix); err != nil {
		t.Fatalf("the staged artifact was dropped by the refused finalize: %v", err)
	}
	if st := m.Status(); st.State != updateReady {
		t.Fatalf("a refused finalize left state %q, want the staged state intact", st.State)
	}
}

// TestFinalizePassWritesReceipt: the pass path commits, records the receipt, and exits 0.
func TestFinalizePassWritesReceipt(t *testing.T) {
	m, cfgPath := updateTestManager(t)
	exe := filepath.Join(t.TempDir(), "omega.exe")
	m.exePath = func() (string, error) { return exe, nil }
	if err := os.WriteFile(exe, []byte("old"), 0o700); err != nil {
		t.Fatalf("exe: %v", err)
	}
	if err := os.WriteFile(exe+stagedSuffix, []byte("new"), 0o700); err != nil {
		t.Fatalf("staged: %v", err)
	}
	m.set(func(s *core.UpdateStatus) { s.State = updateReady; s.StagedVer = "v9.9.9" })
	m.activeRuns = func() int { return 0 }
	var got finalizeRequest
	m.finalizeStep = func(req finalizeRequest) (finalizeOutcome, error) {
		got = req
		return finalizeOutcome{}, nil
	}
	exitCode := make(chan int, 1)
	m.exit = func(code int) { exitCode <- code }

	if _, err := m.Finalize(context.Background(), true); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	select {
	case code := <-exitCode:
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (a passed window commits)", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the finalize never reached the exit seam")
	}
	if got.NewVersion != "v9.9.9" || got.ConfigPath != cfgPath {
		t.Fatalf("finalize request = %+v, want the staged version and the booted config", got)
	}
	if _, err := os.Stat(exe + stagedSuffix); !os.IsNotExist(err) {
		t.Fatalf("a passed finalize left the staged artifact behind (err %v): a later check would finalize it twice", err)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(mustCfg(t, m).Store.DBPath), ".update-receipt.json"))
	if err != nil {
		t.Fatalf("read receipt: %v", err)
	}
	var r core.UpdateReceipt
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("decode receipt: %v", err)
	}
	if r.Outcome != "pass" || r.Version != "v9.9.9" {
		t.Fatalf("receipt = %+v, want a pass for v9.9.9", r)
	}
}

// TestFinalizeWindowFailRollsBack: the platform half reverted (outcome.Serving false), so
// the snapshots come back, the old binary is respawned, and the receipt says why.
func TestFinalizeWindowFailRollsBack(t *testing.T) {
	m, cfgPath := updateTestManager(t)
	exe := filepath.Join(t.TempDir(), "omega.exe")
	m.exePath = func() (string, error) { return exe, nil }
	if err := os.WriteFile(exe, []byte("old-binary"), 0o700); err != nil {
		t.Fatalf("exe: %v", err)
	}
	if err := os.WriteFile(exe+stagedSuffix, []byte("new-binary"), 0o700); err != nil {
		t.Fatalf("staged: %v", err)
	}
	cfg := mustCfg(t, m)
	makeTestStore(t, cfg.Store.DBPath)
	m.set(func(s *core.UpdateStatus) { s.State = updateReady; s.StagedVer = "v9.9.9" })
	m.activeRuns = func() int { return 0 }
	m.finalizeStep = func(req finalizeRequest) (finalizeOutcome, error) {
		// The platform half: the swap happened, the window failed, and it reverted the
		// binary - the listener is gone, so the caller must respawn.
		if err := os.WriteFile(req.ExePath, []byte("new-binary"), 0o700); err != nil {
			t.Fatalf("swap: %v", err)
		}
		return finalizeOutcome{}, errors.New("health window failed after 30s: /health version \"v0.19.0\", want \"v9.9.9\"")
	}
	var respawned bool
	m.spawn = func(string) (finalizeChild, error) {
		respawned = true
		return fakeChild{}, nil
	}
	exitCode := make(chan int, 1)
	m.exit = func(code int) { exitCode <- code }

	if _, err := m.Finalize(context.Background(), true); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	select {
	case code := <-exitCode:
		if code != 1 {
			t.Fatalf("exit code = %d after a window failure, want 1", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the failed finalize never exited")
	}
	body, err := os.ReadFile(exe)
	if err != nil {
		t.Fatalf("read exe: %v", err)
	}
	if string(body) != "new-binary" {
		t.Fatalf("exe content = %q after rollback, want the platform half's reverted binary", string(body))
	}
	if _, err := os.Stat(exe + stagedSuffix); !os.IsNotExist(err) {
		t.Fatalf("the failed artifact stayed staged (err %v)", err)
	}
	receipt := m.Status().Receipt
	if receipt == nil || receipt.Outcome != "rollback" || !strings.Contains(receipt.Why, "health window failed") {
		t.Fatalf("receipt = %+v, want a rollback naming the window failure", receipt)
	}
	if len(receipt.Restored) == 0 || receipt.Restored[0] != "omega.db" {
		t.Fatalf("receipt restored = %q, want the store named", receipt.Restored)
	}
	if !respawned {
		t.Fatal("the old binary was not respawned after the failed window")
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("config.yaml disappeared across the rollback: %v", err)
	}
}

type fakeChild struct{}

func (fakeChild) Kill() error { return nil }

// mustCfg loads the manager's config with resolved home paths.
func mustCfg(t *testing.T, m *updateManager) Config {
	t.Helper()
	cfg, err := m.loadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

// TestSnapshotRetentionOne: two consecutive finalizes leave exactly one .pre-* snapshot.
func TestSnapshotRetentionOne(t *testing.T) {
	cfg := DefaultConfig()
	dir := t.TempDir()
	cfg.Store.DBPath = filepath.Join(dir, "data", "omega.db")
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("provider:\n  model_name: mock\n"), 0o600); err != nil {
		t.Fatalf("config: %v", err)
	}
	makeTestStore(t, cfg.Store.DBPath)

	if _, err := snapshotBeforeFinalize(cfg.Store.DBPath, cfgPath, "v0.19.0"); err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	if _, err := snapshotBeforeFinalize(cfg.Store.DBPath, cfgPath, "v0.20.0"); err != nil {
		t.Fatalf("second snapshot: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatalf("read data dir: %v", err)
	}
	var snaps []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), snapshotPrefix) {
			snaps = append(snaps, e.Name())
		}
	}
	if len(snaps) != 1 {
		t.Fatalf("snapshots = %q, want exactly one (retention 1)", snaps)
	}
	if snaps[0] != snapshotPrefix+"v0.20.0" {
		t.Fatalf("retained snapshot = %q, want the latest (.pre-v0.20.0)", snaps[0])
	}
}

// TestRestoreSnapshotBothFiles: the retained pair comes back, and the stale WAL sidecar
// never grafts onto the restored file.
func TestRestoreSnapshotBothFiles(t *testing.T) {
	dir := t.TempDir()
	liveDB := filepath.Join(dir, "data", "omega.db")
	liveCfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(liveCfg, []byte("updates:\n  channel: stable\n"), 0o600); err != nil {
		t.Fatalf("config: %v", err)
	}
	makeTestStore(t, liveDB)

	snap, err := snapshotBeforeFinalize(liveDB, liveCfg, "v0.19.0")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	// The new version damages both files and leaves a stale WAL beside the store.
	if err := os.WriteFile(liveDB, []byte("not a database"), 0o600); err != nil {
		t.Fatalf("damage db: %v", err)
	}
	if err := os.WriteFile(liveDB+"-wal", []byte("stale"), 0o600); err != nil {
		t.Fatalf("stale wal: %v", err)
	}
	if err := os.WriteFile(liveCfg, []byte("updates:\n  channel: nightly\n"), 0o600); err != nil {
		t.Fatalf("damage config: %v", err)
	}

	restored := restoreSnapshot(snap)
	if len(restored) != 2 {
		t.Fatalf("restored = %q, want omega.db and config.yaml", restored)
	}
	if _, err := os.Stat(liveDB + "-wal"); !os.IsNotExist(err) {
		t.Fatalf("the stale -wal survived the restore (err %v)", err)
	}
	body, _ := os.ReadFile(liveCfg)
	if !strings.Contains(string(body), "channel: stable") {
		t.Fatalf("config.yaml = %q, want the snapshot's channel", body)
	}
	db, err := sql.Open("sqlite", liveDB+"?mode=ro")
	if err != nil {
		t.Fatalf("restored db: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM probes`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("restored store query = (%d, %v), want one probe row", n, err)
	}
}

// TestProbeServeVersionRequiresNewVersion: /health ok alone is not a pass - a stale server
// answering on the port must not read as the restart.
func TestProbeServeVersionRequiresNewVersion(t *testing.T) {
	serving := func(version string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"status":"ok","version":%q}`, version)
		}))
	}
	old := serving("v0.18.0")
	defer old.Close()
	if err := probeServeVersion(context.Background(), old.URL, "v0.19.0"); err == nil {
		t.Fatal("a stale server answering /health ok passed the window for the new version")
	}
	newer := serving("v0.19.0")
	defer newer.Close()
	if err := probeServeVersion(context.Background(), newer.URL, "v0.19.0"); err != nil {
		t.Fatalf("the new version's healthy answer failed the window: %v", err)
	}
	// A dev stamp of the same release (tag + hash) still counts as the new version.
	devStamp := serving("v0.19.0-abc12345")
	defer devStamp.Close()
	if err := probeServeVersion(context.Background(), devStamp.URL, "v0.19.0"); err != nil {
		t.Fatalf("a build-hash stamp of the new release failed the window: %v", err)
	}
}

// TestProbeServeVersionSendsSelfHost: the post-restart probe must reach a wildcard bind,
// whose guard refuses a loopback Host - the version confirmation would fail otherwise.
func TestProbeServeVersionSendsSelfHost(t *testing.T) {
	var want string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != want {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok","version":"v0.19.0"}`)
	}))
	defer ts.Close()
	want = selfHost(strconv.Itoa(ts.Listener.Addr().(*net.TCPAddr).Port))

	if err := probeServeVersion(context.Background(), ts.URL, "v0.19.0"); err != nil {
		t.Fatalf("probeServeVersion: %v", err)
	}
}

// TestStagingIsQueryable: Stage answers while the work runs, and the status reports it.
// The download outlives the HTTP request: a client that hangs up mid-stage must not
// abort the artifact (the request context is detached on purpose).
func TestStagingIsQueryable(t *testing.T) {
	m, _ := updateTestManager(t)
	// The asset must match the RUNNING platform: findAsset filters by
	// assetNameForOS(GOOS, GOARCH), so a hardcoded windows_amd64 name makes
	// stageRun fail before the download on every non-Windows runner - the
	// exact hang the linux CI saw twice (found while landing t_a7d6b182).
	platformAsset := fmt.Sprintf("omega-x-%s.zip", assetNameForOS(runtime.GOOS, runtime.GOARCH))
	release := githubRelease{TagName: "v9.9.9", Assets: []githubAsset{{Name: platformAsset, BrowserDownloadURL: "https://example.com/x.zip"}}}
	m.fetchStable = func() (githubRelease, error) { return release, nil }
	m.fetchList = func() ([]githubRelease, error) { return []githubRelease{release}, nil }
	started := make(chan struct{})
	blocked := make(chan struct{})
	ctxErr := make(chan error, 1)
	m.downloadAsset = func(ctx context.Context, url string, progress func(int64, int64)) (string, error) {
		progress(50, 100)
		close(started)
		<-blocked
		ctxErr <- ctx.Err()
		return "", errors.New("cancelled")
	}
	reqCtx, cancelReq := context.WithCancel(context.Background())
	if _, err := m.Stage(reqCtx, ""); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if st := m.Status(); st.State != updateStaging {
		t.Fatalf("state right after Stage = %q, want %q (the work must be background)", st.State, updateStaging)
	}
	<-started
	cancelReq() // the HTTP client hung up
	close(blocked)
	select {
	case err := <-ctxErr:
		if err != nil {
			t.Fatalf("the staged download inherited the request's cancellation (%v): a hung-up client must not abort it", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the download seam never observed its context")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st := m.Status(); st.State == updateFailed {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the background stage never settled: %q", m.Status().State)
}

// TestStageRefusesDevBuild: R8 - the dev lane never self-updates.
func TestStageRefusesDevBuild(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o755); err != nil {
		t.Fatalf("home: %v", err)
	}
	cfgPath := filepath.Join(home, "config", "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("provider:\n  model_name: mock\n"), 0o600); err != nil {
		t.Fatalf("config: %v", err)
	}
	m := newUpdateManager(cfgPath, "http://127.0.0.1:1", nil)
	prev := omegaVersion
	omegaVersion = "v0.19.0-abc12345"
	defer func() { omegaVersion = prev }()

	if _, err := m.Stage(context.Background(), ""); err == nil {
		t.Fatal("a dev build staged a release: the tick owns dev builds, not the updater")
	}
	if st := m.Status(); st.State != updateFailed || !strings.Contains(st.Error, "release-managed") {
		t.Fatalf("status = %+v, want a failed state naming the release-managed scope", st)
	}
}
