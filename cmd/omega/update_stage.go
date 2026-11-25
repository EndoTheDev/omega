package main

// The update channel's engine, serve mode: check -> stage -> pre-flight copy-boot ->
// finalize. One engine serves the CLI, the webui and the desktop affordances; the CLI's
// `omega update` keeps its own direct contract, and only the host mode decides
// whether the staged path exists.
//
// Nothing here touches the running install until Finalize: a check is a read, a stage
// writes only <exe>.staged and a throwaway pre-flight home, and a failed pre-flight is a
// NEVER-FINALIZE (staged artifacts discarded, the running process untouched).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/core"
	"gopkg.in/yaml.v3"
)

// Update channel states, as the webui and the tray render them.
const (
	updateIdle       = "idle"
	updateChecking   = "checking"
	updateUpToDate   = "up-to-date"
	updateAvailable  = "available"
	updateStaging    = "staging"
	updatePreflight  = "preflight"
	updateReady      = "ready"
	updateFinalizing = "finalizing"
	updateFailed     = "failed"
)

const (
	// updateCheckInterval is the auto-check ladder: one release read per boot hour, so a
	// published release lands on a running home the same day without polling GitHub hard.
	updateCheckInterval = 6 * time.Hour
	// updateFirstCheckDelay keeps the first auto-check off the boot path.
	updateFirstCheckDelay = 90 * time.Second
	// updatePreflightBudget bounds the staged binary's copy-boot: provider-free migration
	// boot on a small store answers in a second or two; the budget only catches a hang.
	updatePreflightBudget = 60 * time.Second
	// updateHealthWindow is the supervised window after the restart: /health ok AND
	// version == new, or the automatic revert runs.
	updateHealthWindow = 30 * time.Second
	// stagedSuffix names the staged artifact BESIDE the running binary: it never collides
	// with omega.bak (the one-generation rollback target) or the running file.
	stagedSuffix = ".staged"
)

// errUpdateBusy is one operation at a time: a second stage while a download runs would
// race two writers over the same staged path.
var errUpdateBusy = errors.New("an update operation is already running")

// updateManager is the serving process's update engine.
type updateManager struct {
	configPath string
	origin     string

	mu      sync.Mutex
	st      core.UpdateStatus
	busy    bool
	receipt *core.UpdateReceipt
	// finalizeDone closes when a finalize's supervisor finishes; main waits on it so the
	// process cannot return out from under the window (tests; production exits).
	finalizeDone chan struct{}

	// Seams. Production values are assigned in newUpdateManager; tests replace them.
	exePath        func() (string, error)
	fetchStable    func() (githubRelease, error)
	fetchList      func() ([]githubRelease, error)
	downloadAsset  func(ctx context.Context, url string, progress func(read, total int64)) (string, error)
	runBinary      func(exe string) (string, error)
	preflightBoot  func(ctx context.Context, staged string, want string) error
	probeVersion   func(ctx context.Context, origin, want string) error
	finalizeStep   func(req finalizeRequest) (finalizeOutcome, error)
	spawn          func(configPath string) (finalizeChild, error)
	swap           func(src, dst string) error
	rollback       func(exePath, bakPath string) error
	stopServing    func()
	activeRuns     func() int
	exit           func(int)
	sleep          func(time.Duration)
	logf           func(format string, args ...any)
	now            func() time.Time
	healthWindow   time.Duration
	preflightLimit time.Duration
}

// newUpdateManager builds the serve-mode engine over a booted runtime. origin is the URL
// the process serves on; stopServing is the graceful stop the finalize needs before the
// restart (nil in a headless probe, where no restart happens).
func newUpdateManager(configPath, origin string, stopServing func()) *updateManager {
	if base := os.Getenv("OMEGA_UPDATE_API_BASE"); base != "" {
		githubAPIBase = base // the same fixture seam the CLI honours; never set in prod
	}
	m := &updateManager{
		configPath:     configPath,
		origin:         origin,
		stopServing:    stopServing,
		exePath:        os.Executable,
		fetchStable:    fetchStable,
		fetchList:      fetchReleaseList,
		downloadAsset:  downloadReleaseAsset,
		runBinary:      verifyBinaryHealth,
		probeVersion:   probeServeVersion,
		spawn:          spawnFinalizeChild,
		swap:           replaceBinary,
		rollback:       rollbackSwap,
		activeRuns:     func() int { return 0 },
		exit:           os.Exit,
		sleep:          time.Sleep,
		logf:           func(format string, args ...any) { fmt.Fprintf(os.Stderr, "omega update: "+format+"\n", args...) },
		now:            time.Now,
		healthWindow:   updateHealthWindow,
		preflightLimit: updatePreflightBudget,
	}
	m.preflightBoot = m.copyBoot
	m.finalizeStep = finalizePlatform
	cfg, err := LoadConfig(configPath)
	if err != nil {
		m.st = core.UpdateStatus{State: updateFailed, Current: omegaVersion, Error: err.Error()}
		return m
	}
	m.st = m.baseStatus(cfg)
	m.receipt = loadReceipt(m.receiptPath())
	m.st.Receipt = m.receipt
	return m
}

// loadConfig re-reads the config file: the updates knobs (channel, auto flags, managed)
// are live, so a settings save is the next check's truth with no restart.
func (m *updateManager) loadConfig() (Config, error) {
	cfg, err := LoadConfig(m.configPath)
	if err != nil {
		return Config{}, err
	}
	if err := resolveHomePaths(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// channelOf resolves the effective channel: the request's word wins, else the config's.
func channelOf(cfg Config, requested string) (string, bool) {
	channel := strings.ToLower(strings.TrimSpace(requested))
	if channel == "" {
		channel = strings.ToLower(strings.TrimSpace(cfg.Updates.Channel))
	}
	if channel != "nightly" {
		channel = "stable"
	}
	return channel, channel == "nightly"
}

// managedBuild reports whether this install is release-managed. Config declares it;
// "auto" derives it from the build stamp: a release tag with no local build hash was
// built by the release pipeline, and anything else (a dev build, an unstamped module
// build) is built by something else and never self-updates.
func managedBuild(cfg Config, version string) bool {
	switch strings.ToLower(strings.TrimSpace(cfg.Updates.Managed)) {
	case "on", "true", "yes":
		return true
	case "off", "false", "no":
		return false
	}
	return tagShapeRe.MatchString(version)
}

// baseStatus is the status with the config-derived facts and no operation in flight.
func (m *updateManager) baseStatus(cfg Config) core.UpdateStatus {
	channel, _ := channelOf(cfg, "")
	st := core.UpdateStatus{
		State:      updateIdle,
		Current:    omegaVersion,
		Channel:    channel,
		Managed:    managedBuild(cfg, omegaVersion),
		AutoCheck:  cfg.Updates.AutoCheck,
		AutoUpdate: cfg.Updates.AutoUpdate,
	}
	if m.receipt != nil {
		st.Receipt = m.receipt
	}
	return st
}

// Status reports the current state.
func (m *updateManager) Status() core.UpdateStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.st
}

// set applies a state change and returns the new status.
func (m *updateManager) set(fn func(*core.UpdateStatus)) core.UpdateStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(&m.st)
	return m.st
}

// fail records an error state and returns it with the status.
func (m *updateManager) fail(err error) (core.UpdateStatus, error) {
	st := m.set(func(s *core.UpdateStatus) {
		s.State = updateFailed
		s.Error = err.Error()
	})
	return st, err
}

// Check resolves the channel's offered release. Read-only: no artifact is touched.
func (m *updateManager) Check(ctx context.Context, requested string) (core.UpdateStatus, error) {
	cfg, err := m.loadConfig()
	if err != nil {
		return m.fail(err)
	}
	m.mu.Lock()
	if m.busy {
		st := m.st
		m.mu.Unlock()
		return st, errUpdateBusy
	}
	m.busy = true
	m.st.State = updateChecking
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.busy = false
		m.mu.Unlock()
	}()

	channel, nightly := channelOf(cfg, requested)
	rel, behind, err := resolveRelease(m, nightly)
	if err != nil {
		return m.fail(err)
	}
	state := updateAvailable
	gate, gerr := updateGate(rel.TagName, omegaVersion, false)
	switch {
	case gerr != nil:
		// Running ahead of the channel (a stale mirror, a dev stamp above the tag): nothing
		// to install, and updateGate's downgrade refusal is the whole story.
		state = updateUpToDate
	case gate == "skip":
		state = updateUpToDate
	}
	st := m.set(func(s *core.UpdateStatus) {
		s.State = state
		s.Channel = channel
		s.Managed = managedBuild(cfg, omegaVersion)
		s.AutoCheck = cfg.Updates.AutoCheck
		s.AutoUpdate = cfg.Updates.AutoUpdate
		s.Available = ""
		s.Behind = 0
		s.Error = ""
		s.CheckedAt = m.now().Unix()
		if state == updateAvailable {
			s.Available = rel.TagName
			s.Behind = behind
		}
	})
	return st, nil
}

// resolveRelease picks the channel's release plus the behind count. It is the same table
// the CLI reads: the stable channel is /releases/latest's answer, and the nightly
// channel is the newest -nightly.* prerelease, which sorts BELOW its stable target.
func resolveRelease(m *updateManager, nightly bool) (githubRelease, int, error) {
	if nightly {
		rels, err := m.fetchList()
		if err != nil {
			return githubRelease{}, 0, err
		}
		rel, err := newestNightly(rels)
		if err != nil {
			return githubRelease{}, 0, err
		}
		return rel, countNewer(channelTags(rels, true), omegaVersion), nil
	}
	rel, err := m.fetchStable()
	if err != nil {
		return githubRelease{}, 0, err
	}
	behind := 0
	if rels, lerr := m.fetchList(); lerr == nil {
		behind = countNewer(channelTags(rels, false), omegaVersion)
	}
	return rel, behind, nil
}

// Stage downloads and verifies the offered release, then pre-flight copy-boots it. It
// returns while the work runs; Status reports progress.
func (m *updateManager) Stage(ctx context.Context, requested string) (core.UpdateStatus, error) {
	cfg, err := m.loadConfig()
	if err != nil {
		return m.fail(err)
	}
	if !managedBuild(cfg, omegaVersion) {
		return m.fail(fmt.Errorf("this build is not release-managed (version %s): the dev lane never self-updates", omegaVersion))
	}
	channel, nightly := channelOf(cfg, requested)

	m.mu.Lock()
	if m.busy {
		st := m.st
		m.mu.Unlock()
		return st, errUpdateBusy
	}
	if m.st.State == updateReady && m.st.Available != "" {
		st := m.st
		m.mu.Unlock()
		return st, nil // already staged and pre-flight green
	}
	m.busy = true
	m.st.State = updateStaging
	m.st.Channel = channel
	m.st.Error = ""
	m.st.Progress = 0
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m.mu.Unlock()

	go func() {
		defer cancel()
		defer func() {
			m.mu.Lock()
			m.busy = false
			m.mu.Unlock()
		}()
		if err := m.stageRun(runCtx, cfg, nightly); err != nil {
			m.fail(err)
		}
	}()
	return m.Status(), nil
}

// stageRun is the background half of Stage: release resolution, download, verify,
// extract, then the pre-flight copy-boot that alone makes finalize legal.
func (m *updateManager) stageRun(ctx context.Context, cfg Config, nightly bool) error {
	rel, _, err := resolveRelease(m, nightly)
	if err != nil {
		return err
	}
	if gate, gerr := updateGate(rel.TagName, omegaVersion, false); gerr != nil || gate == "skip" {
		m.set(func(s *core.UpdateStatus) {
			s.State = updateUpToDate
			s.Available = ""
			s.Progress = 0
			s.CheckedAt = m.now().Unix()
		})
		return nil
	}
	exePath, err := m.exePath()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	m.set(func(s *core.UpdateStatus) { s.Available = rel.TagName })

	url := findAsset(rel.Assets, runtime.GOOS, runtime.GOARCH)
	if url == "" {
		return fmt.Errorf("no release asset for %s/%s in %s", runtime.GOOS, runtime.GOARCH, rel.TagName)
	}
	assetName := assetNameForOS(runtime.GOOS, runtime.GOARCH)

	archivePath, err := m.downloadAsset(ctx, url, func(read, total int64) {
		if total <= 0 {
			return
		}
		m.set(func(s *core.UpdateStatus) { s.Progress = int(read * 100 / total) })
	})
	if err != nil {
		return fmt.Errorf("download %s: %w", assetName, err)
	}
	defer os.RemoveAll(filepath.Dir(archivePath))

	// The checksum rule the CLI already lives by: verify when the release carries the
	// file, and skip with a warning when it does not - never silently claim verification.
	if checksumsURL := findChecksumURL(rel.Assets); checksumsURL != "" {
		if err := verifyChecksum(archivePath, checksumsURL, assetName); err != nil {
			return fmt.Errorf("checksum verification failed: %w", err)
		}
	} else {
		m.logf("warning: no checksums.txt in %s, skipping verification", rel.TagName)
	}

	staged, err := extractStagedBinary(archivePath, exePath)
	if err != nil {
		return err
	}
	// The staged binary must at least execute and name the offered release before the
	// copy-boot spends a boot on it.
	out, err := m.runBinary(staged)
	if err != nil {
		os.Remove(staged)
		return fmt.Errorf("staged binary does not run: %w", err)
	}
	if !strings.Contains(out, rel.TagName) {
		os.Remove(staged)
		return fmt.Errorf("staged binary reports %q, expected %s", strings.TrimSpace(out), rel.TagName)
	}

	m.set(func(s *core.UpdateStatus) {
		s.State = updatePreflight
		s.StagedVer = rel.TagName
		s.Progress = 100
	})
	if err := m.preflightGate(ctx, staged, rel.TagName); err != nil {
		return nil // the gate already recorded the refusal
	}
	return nil
}

// preflightGate runs the copy-boot and applies its verdict: green makes the release
// finalize-able, red discards the staged artifact and records the refusal verbatim -
// NEVER-FINALIZE, with the running process untouched.
func (m *updateManager) preflightGate(ctx context.Context, staged, want string) error {
	if err := m.preflightBoot(ctx, staged, want); err != nil {
		os.Remove(staged)
		m.set(func(s *core.UpdateStatus) {
			s.State = updateFailed
			s.StagedVer = ""
			s.Progress = 0
			s.Error = "pre-flight refused to finalize: " + err.Error()
		})
		return err
	}
	m.set(func(s *core.UpdateStatus) { s.State = updateReady })
	m.logf("staged %s: pre-flight copy-boot green (health ok, migrations applied)", want)
	return nil
}

// downloadReleaseAsset streams url into a fresh temp file, reporting progress.
func downloadReleaseAsset(ctx context.Context, url string, progress func(read, total int64)) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	dir, err := os.MkdirTemp("", "omega-download-")
	if err != nil {
		return "", err
	}
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	path := filepath.Join(dir, "archive"+ext)
	out, err := os.Create(path)
	if err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	total := resp.ContentLength
	var read int64
	buf := make([]byte, 32<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				os.RemoveAll(dir)
				return "", werr
			}
			read += int64(n)
			progress(read, total)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			os.RemoveAll(dir)
			return "", rerr
		}
	}
	if err := out.Close(); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return path, nil
}

// extractStagedBinary unpacks the release archive and copies its omega binary beside the
// running one as <exe>.staged. The archive's extension files are NOT installed here: the
// finalize's replaceBinary is the install, exactly like the CLI's.
func extractStagedBinary(archivePath, exePath string) (string, error) {
	archiveFile, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("open archive: %w", err)
	}
	defer archiveFile.Close()
	extractDir, err := os.MkdirTemp("", "omega-extract-")
	if err != nil {
		return "", fmt.Errorf("create extract dir: %w", err)
	}
	defer os.RemoveAll(extractDir)
	if runtime.GOOS == "windows" {
		if err := extractZip(archiveFile, extractDir); err != nil {
			return "", fmt.Errorf("extract zip: %w", err)
		}
	} else if err := extractTarGz(archiveFile, extractDir); err != nil {
		return "", fmt.Errorf("extract tar.gz: %w", err)
	}
	bin := findInArchive(extractDir, "omega")
	if bin == "" {
		return "", fmt.Errorf("omega binary not found in release archive")
	}
	staged := exePath + stagedSuffix
	if err := copyFile(bin, staged); err != nil {
		return "", fmt.Errorf("write staged binary: %w", err)
	}
	return staged, nil
}

// Discard drops the staged artifacts and returns to idle.
func (m *updateManager) Discard() (core.UpdateStatus, error) {
	m.mu.Lock()
	if m.busy {
		st := m.st
		m.mu.Unlock()
		return st, errUpdateBusy
	}
	m.mu.Unlock()
	exePath, err := m.exePath()
	if err == nil {
		os.Remove(exePath + stagedSuffix)
	}
	st := m.set(func(s *core.UpdateStatus) {
		s.State = updateIdle
		s.Available = ""
		s.StagedVer = ""
		s.Progress = 0
		s.Behind = 0
		s.Error = ""
	})
	return st, nil
}

// probeServeVersion asks a serving origin for its /health version and requires want. The
// version half is the pass condition: /health ok alone lets a stale server on the
// port read as a healthy restart.
func probeServeVersion(ctx context.Context, origin, want string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(origin, "/")+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("/health answered %s", resp.Status)
	}
	var body struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return fmt.Errorf("decode /health: %w", err)
	}
	if body.Status != "ok" {
		return fmt.Errorf("/health status %q", body.Status)
	}
	if !sameVersion(body.Version, want) {
		return fmt.Errorf("/health version %q, want %q", body.Version, want)
	}
	return nil
}

// sameVersion compares a served version with the release tag, ignoring the local build
// hash a dev stamp carries.
func sameVersion(served, want string) bool {
	return strings.TrimSpace(served) == want || trimBuildHash(strings.TrimSpace(served)) == want
}

// freeLoopbackPort reserves and releases a loopback port for the pre-flight boot.
func freeLoopbackPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// waitPortFree waits until addr stops answering a dial: the restart needs the port
// released, and shutdown is asynchronous.
func waitPortFree(addr string, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	for {
		conn, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
		if err != nil {
			return nil
		}
		conn.Close()
		if time.Now().After(deadline) {
			return fmt.Errorf("port %s still held after %s", addr, budget)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// hostPort splits the serving origin into dialable host:port.
func hostPort(origin string) string {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(origin, "http://"), "https://")
	if i := strings.IndexByte(trimmed, '/'); i >= 0 {
		trimmed = trimmed[:i]
	}
	if _, _, err := net.SplitHostPort(trimmed); err == nil {
		return trimmed
	}
	return net.JoinHostPort(strings.Trim(trimmed, "[]"), "80")
}

// preflightPort rewrites a config's server section for the throwaway boot.
func preflightConfigYAML(cfg Config, port int, dbCopy string) ([]byte, error) {
	type serverSec struct {
		Port int      `yaml:"port"`
		Bind []string `yaml:"bind"`
	}
	type storeSec struct {
		DBPath string `yaml:"db_path"`
	}
	doc := struct {
		Provider ProviderConfig `yaml:"provider"`
		Server   serverSec      `yaml:"server"`
		Store    storeSec       `yaml:"store"`
	}{
		Provider: cfg.Provider,
		Server:   serverSec{Port: port, Bind: []string{"127.0.0.1"}},
		Store:    storeSec{DBPath: dbCopy},
	}
	return yaml.Marshal(doc)
}

// preflightConfigEnv is the child's environment: the throwaway home, then the parent's
// env (provider keys and the like) minus nothing - a copy-boot must boot the same stack.
func preflightEnv(home string) []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "OMEGA_HOME=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "OMEGA_HOME="+home)
}

var _ core.UpdateSurface = (*updateManager)(nil)
