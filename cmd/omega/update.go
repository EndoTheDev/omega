package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// githubRelease represents the subset of the GitHub releases API response that omega.
type githubRelease struct {
	TagName string `json:"tag_name"`
	// Draft and Prerelease channel the behind-count: the stable channel is exactly what.
	Draft      bool          `json:"draft"`
	Prerelease bool          `json:"prerelease"`
	Assets     []githubAsset `json:"assets"`
	Message    string        `json:"message"` // API error message (e.g. "Not Found")
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// assetNameForOS returns the expected release asset name substring for the current.
func assetNameForOS(goos, goarch string) string {
	return goos + "_" + goarch
}

// findAsset returns the download URL for the asset matching the current OS/arch,.
func findAsset(assets []githubAsset, goos, goarch string) string {
	target := assetNameForOS(goos, goarch)
	for _, a := range assets {
		if strings.Contains(a.Name, target) {
			return a.BrowserDownloadURL
		}
	}
	return ""
}

// updateOptions carries the update subcommand's flags.
type updateOptions struct {
	nightly        bool // resolve the newest -nightly.* prerelease instead of the latest stable
	rollback       bool // swap back to omega.bak
	allowDowngrade bool // permit installing a version lower than the running one
	yes            bool // auto-accept the install prompt (unattended-safe install)
	force          bool // rollback: proceed under a live run (never under a live serve)
}

// parseUpdateFlags parses the flags accepted after "update".
// Unknown flags are errors — typo'd flags must not silently fall through to a plain
// stable update.
func parseUpdateFlags(args []string) (updateOptions, error) {
	var opts updateOptions
	for _, a := range args {
		switch a {
		case "--nightly":
			opts.nightly = true
		case "--rollback":
			opts.rollback = true
		case "--allow-downgrade":
			opts.allowDowngrade = true
		case "--yes", "-y":
			opts.yes = true
		case "--force":
			opts.force = true
		default:
			if strings.HasPrefix(a, "-") {
				return opts, fmt.Errorf("unknown update flag %q (known: --nightly, --rollback, --force, --allow-downgrade, --yes)", a)
			}
		}
	}
	if opts.force && !opts.rollback {
		return opts, fmt.Errorf("--force applies to --rollback only (a plain update never forces a restart)")
	}
	return opts, nil
}

// trimBuildHash strips a trailing short build hash ("-c268097e") from a version string:
// the hash is build metadata, invisible to semver ordering.
func trimBuildHash(v string) string {
	return versionShapeRe.ReplaceAllString(v, "$1")
}

// updateGate decides whether the channel's offered tag may be
// installed when the running version is omegaVersion. Returns:
//
// "skip" — offered equals current, nothing to do
//	"install" — offered is newer, or a downgrade was explicitly allowed
// "refuse" — offered is older than current (stale mirror protection)
func updateGate(offeredTag, current string, allowDowngrade bool) (string, error) {
	// The running version carries build metadata the offered tags never have.
	cmp, ok := compareVersions(offeredTag, trimBuildHash(current))
	if !ok {
		// Unparseable current version (dev build, foreign stamp):
		// can't order the versions, so don't gate.
		return "install", nil
	}
	switch {
	case cmp == 0:
		return "skip", nil
	case cmp > 0:
		return "install", nil
	case allowDowngrade:
		return "install", nil
	default:
		return "refuse", fmt.Errorf(
			"refusing downgrade to %s (running %s); pass --allow-downgrade to force it", offeredTag, current)
	}
}

// githubAPIBase is the GitHub API root for release lookups.
var githubAPIBase = "https://api.github.com"

// repoSlug is the repo whose releases omega installs.
const repoSlug = "EndoTheDev/omega"

// fetchStable resolves the LATEST STABLE release via the /releases/latest endpoint.
func fetchStable() (githubRelease, error) {
	resp, err := http.Get(githubAPIBase + "/repos/" + repoSlug + "/releases/latest")
	if err != nil {
		return githubRelease{}, fmt.Errorf("fetch releases: %w", err)
	}
	defer resp.Body.Close()

	var rel githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return githubRelease{}, fmt.Errorf("decode release: %w", err)
	}
	if rel.Message != "" {
		return githubRelease{}, fmt.Errorf("no releases found: %s; visit https://github.com/%s/releases", rel.Message, repoSlug)
	}
	if rel.TagName == "" {
		return githubRelease{}, fmt.Errorf("no releases found; visit https://github.com/%s/releases", repoSlug)
	}
	return rel, nil
}

// fetchReleaseList fetches one page of the release list, newest first.
func fetchReleaseList() ([]githubRelease, error) {
	resp, err := http.Get(githubAPIBase + "/repos/" + repoSlug + "/releases?per_page=100")
	if err != nil {
		return nil, fmt.Errorf("fetch releases: %w", err)
	}
	defer resp.Body.Close()

	var rels []githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&rels); err != nil {
		return nil, fmt.Errorf("decode releases: %w", err)
	}
	return rels, nil
}

// newestNightly picks the NEWEST -nightly.* prerelease tag from a release list.
func newestNightly(rels []githubRelease) (githubRelease, error) {
	newest := -1
	for i, rel := range rels {
		if !isNightlyTag(rel.TagName) {
			continue // stable and non-nightly prereleases (rc, beta) are not the nightly channel
		}
		if newest == -1 || compareTagNewer(rel.TagName, rels[newest].TagName) {
			newest = i
		}
	}
	if newest == -1 {
		return githubRelease{}, fmt.Errorf("no nightly releases found; visit https://github.com/%s/releases", repoSlug)
	}
	return rels[newest], nil
}

// fetchNightly resolves the NEWEST -nightly.* prerelease tag from the release list.
func fetchNightly() (githubRelease, error) {
	rels, err := fetchReleaseList()
	if err != nil {
		return githubRelease{}, err
	}
	return newestNightly(rels)
}

// compareTagNewer reports whether tag a sorts above tag b, treating an
// unparseable tag as never-newer so oddball prerelease tags can't win
// the nightly pick over valid semver tags.
func compareTagNewer(a, b string) bool {
	cmp, ok := compareVersions(a, b)
	return ok && cmp > 0
}

// channelTags returns the tags on the requested channel from the release list,.
// stable is exactly what /releases/latest serves: not draft, not prerelease.
func channelTags(rels []githubRelease, nightly bool) []string {
	var tags []string
	for _, rel := range rels {
		if nightly {
			if isNightlyTag(rel.TagName) {
				tags = append(tags, rel.TagName)
			}
			continue
		}
		if !rel.Draft && !rel.Prerelease {
			tags = append(tags, rel.TagName)
		}
	}
	return tags
}

// countNewer counts the tags strictly newer (semver) than current.
// The current version's build hash ("-c268097e") is build metadata and
// is trimmed first, exactly like updateGate. Unparseable tags never
// count: a tag that cannot be ordered must not claim a version ahead.
func countNewer(tags []string, current string) int {
	behind := 0
	for _, tag := range tags {
		if cmp, ok := compareVersions(tag, trimBuildHash(current)); ok && cmp > 0 {
			behind++
		}
	}
	return behind
}

// stdinIsTerminal reports whether stdin is an interactive terminal (character device).
var stdinIsTerminal = func() bool {
	st, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

// confirmInstall asks prompt as y/N on stderr and reads one line from stdin.
// Non-terminal stdin declines without reading, so an unattended bare update never
// blocks and never replaces the binary — the scripted path is omega update --yes.
func confirmInstall(prompt string) bool {
	if !stdinIsTerminal() {
		fmt.Fprintf(os.Stderr, "%s [y/N] not a terminal: declining\n", prompt)
		return false
	}
	fmt.Fprintf(os.Stderr, "%s [y/N] ", prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false // EOF (empty answer): default N
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes"
}

// installArchiveFn is the install seam.
var installArchiveFn = installArchive

// cmdUpdate installs a release.
func cmdUpdate(rest []string) error {
	if base := os.Getenv("OMEGA_UPDATE_API_BASE"); base != "" {
		githubAPIBase = base // fixture flow for tests; never set in prod
	}

	opts, err := parseUpdateFlags(rest)
	if err != nil {
		return err
	}
	if opts.rollback {
		return cmdRollback(opts.force)
	}

	channel := "stable"
	if opts.nightly {
		channel = "nightly"
	}
	fmt.Fprintf(os.Stderr, "omega: checking for latest %s release...\n", channel)

	var rel githubRelease
	var rels []githubRelease // release list, when one fetch already surfaced it
	if opts.nightly {
		rels, err = fetchReleaseList()
		if err == nil {
			rel, err = newestNightly(rels)
		}
	} else {
		rel, err = fetchStable()
	}
	if err != nil {
		return err
	}

	gate, err := updateGate(rel.TagName, omegaVersion, opts.allowDowngrade)
	if err != nil {
		return err
	}
	if gate == "skip" {
		fmt.Printf("omega: already up to date (%s)\n", omegaVersion)
		return nil
	}

	behind := 0
	if rels != nil {
		behind = countNewer(channelTags(rels, opts.nightly), omegaVersion)
	} else if list, listErr := fetchReleaseList(); listErr == nil {
		behind = countNewer(channelTags(list, opts.nightly), omegaVersion)
	}
	if behind > 0 {
		fmt.Fprintf(os.Stderr, "omega: you are %d versions behind: %s -> %s\n", behind, omegaVersion, rel.TagName)
	}

	if !opts.yes {
		prompt := fmt.Sprintf("omega: install %s (%s channel)?", rel.TagName, channel)
		if !confirmInstall(prompt) {
			fmt.Fprintln(os.Stderr, "omega: not installing - run omega update --yes to install")
			return nil // declined: exit 0, binary untouched
		}
	}

	fmt.Fprintf(os.Stderr, "omega: updating from %s to %s (%s)...\n", omegaVersion, rel.TagName, channel)
	return installArchiveFn(rel, opts.nightly)
}

// installArchive is the shared download-verify-extract-replace flow for every release.
func installArchive(rel githubRelease, nightly bool) error {
	url := findAsset(rel.Assets, runtime.GOOS, runtime.GOARCH)
	if url == "" {
		return fmt.Errorf("no release asset for %s/%s in %s; visit https://github.com/%s/releases", runtime.GOOS, runtime.GOARCH, rel.TagName, repoSlug)
	}

	assetName := assetNameForOS(runtime.GOOS, runtime.GOARCH)

	dlResp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: HTTP %d", dlResp.StatusCode)
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	installDir := filepath.Dir(exePath)

	tmpDir, err := os.MkdirTemp("", "omega-update-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	archiveExt := ".tar.gz"
	if runtime.GOOS == "windows" {
		archiveExt = ".zip"
	}
	archivePath := filepath.Join(tmpDir, "archive"+archiveExt)

	var body io.Reader = dlResp.Body
	if total := dlResp.ContentLength; total > 0 {
		body = &progressReader{r: dlResp.Body, total: total}
	}

	out, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("create archive file: %w", err)
	}
	if _, err := io.Copy(out, body); err != nil {
		out.Close()
		return fmt.Errorf("download archive: %w", err)
	}
	out.Close()

	// Verify checksum if the release includes checksums.txt.
	if checksumsURL := findChecksumURL(rel.Assets); checksumsURL != "" {
		if err := verifyChecksum(archivePath, checksumsURL, assetName); err != nil {
			return fmt.Errorf("checksum verification failed: %w", err)
		}
		fmt.Fprintln(os.Stderr, "omega: checksum verified")
	} else {
		fmt.Fprintln(os.Stderr, "omega: warning — no checksums.txt in release, skipping verification")
	}

	archiveFile, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer archiveFile.Close()

	extractDir, err := os.MkdirTemp("", "omega-extract-")
	if err != nil {
		return fmt.Errorf("create extract dir: %w", err)
	}
	defer os.RemoveAll(extractDir)

	if runtime.GOOS == "windows" {
		if err := extractZip(archiveFile, extractDir); err != nil {
			return fmt.Errorf("extract zip: %w", err)
		}
	} else {
		if err := extractTarGz(archiveFile, extractDir); err != nil {
			return fmt.Errorf("extract tar.gz: %w", err)
		}
	}

	omegaBin := findInArchive(extractDir, "omega")
	if omegaBin == "" {
		return fmt.Errorf("omega binary not found in release archive")
	}

	// Replace the running binary, preserving the old one as omega.bak.
	if err := replaceBinary(omegaBin, exePath); err != nil {
		return fmt.Errorf("replace omega binary: %w", err)
	}

	extSrcDir := filepath.Join(filepath.Dir(omegaBin), "extensions")
	extDstDir := filepath.Join(installDir, "extensions")
	if entries, err := os.ReadDir(extSrcDir); err == nil {
		os.MkdirAll(extDstDir, 0o755)
		for _, e := range entries {
			if !e.IsDir() {
				continue // flat-file layout (legacy), skip non-dirs
			}
			subDir := filepath.Join(extSrcDir, e.Name())
			dstDir := filepath.Join(extDstDir, e.Name())
			os.MkdirAll(dstDir, 0o755)
			if subEntries, err := os.ReadDir(subDir); err == nil {
				for _, f := range subEntries {
					if f.IsDir() || strings.HasSuffix(f.Name(), ".md") || strings.HasSuffix(f.Name(), ".txt") {
						continue
					}
					copyFile(filepath.Join(subDir, f.Name()), filepath.Join(dstDir, f.Name()))
				}
			}
		}
	}

	// Copy example files only if they don't already exist.
	copyIfMissing(filepath.Join(filepath.Dir(omegaBin), "config.yaml.example"), filepath.Join(installDir, "config.yaml.example"))
	copyIfMissing(filepath.Join(filepath.Dir(omegaBin), "mcp.yaml.example"), filepath.Join(installDir, "mcp.yaml.example"))

	if total := dlResp.ContentLength; total > 0 {
		fmt.Fprintln(os.Stderr)
	}

	extCount := 0
	if entries, err := os.ReadDir(extDstDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if subEntries, err := os.ReadDir(filepath.Join(extDstDir, e.Name())); err == nil {
				for _, f := range subEntries {
					if !f.IsDir() && !strings.HasSuffix(f.Name(), ".md") && !strings.HasSuffix(f.Name(), ".txt") {
						extCount++
					}
				}
			}
		}
	}
	fmt.Printf("omega: updated to %s (%s channel: omega + %d extensions)\n", rel.TagName, boolChannel(nightly), extCount)
	return nil
}

// boolChannel names the channel for the completion line.
func boolChannel(nightly bool) string {
	if nightly {
		return "nightly"
	}
	return "stable"
}

// rollbackRequest is the one manual rollback: the executable slot, the home the update
// code resolved for it, and the operator's force.
type rollbackRequest struct {
	ExePath    string
	Origin     string // the serve origin to probe; empty when no config resolved
	DBPath     string
	ConfigPath string
	Force      bool
}

// cmdRollback restores the retained store snapshot, then swaps omega.bak into the
// executable slot.
func cmdRollback(force bool) error {
	if base := os.Getenv("OMEGA_UPDATE_API_BASE"); base != "" {
		githubAPIBase = base // keep the env seam consistent, though fetch is unused
	}
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	req := rollbackRequest{ExePath: exePath, Force: force}
	// The home comes from the loaded config, never from the exe's directory: a
	// PATH-style install keeps its store in ~/.omega while the binary lives in go/bin.
	if configPath := resolveConfigPath(""); configPath != "" {
		cfg, cfgErr := LoadConfig(configPath)
		if cfgErr == nil {
			cfgErr = resolveHomePaths(&cfg)
		}
		if cfgErr != nil {
			fmt.Fprintf(os.Stderr, "omega: warning - %v: the store half cannot be resolved, this rollback swaps the binary only\n", cfgErr)
		} else {
			req.Origin = fmt.Sprintf("http://127.0.0.1:%d", cfg.Server.Port)
			req.DBPath = cfg.Store.DBPath
			req.ConfigPath = configPath
		}
	}
	return rollbackTo(req)
}

// rollbackTo is the platform-independent core of rollback. The store goes back FIRST: an
// old binary booting against a new store is the boot-kill this path exists to prevent,
// while a new binary against an old store re-runs its own migrations.
var rollbackTo = func(req rollbackRequest) error {
	bakPath := req.ExePath + ".bak"
	if _, err := os.Stat(bakPath); err != nil {
		return fmt.Errorf("no omega.bak beside %s - nothing to roll back to", req.ExePath)
	}

	live, active := rollbackProbe(req.Origin)
	if active > 0 && !req.Force {
		return busyRefusal(active)
	}

	snap, err := rollbackFindSnapshot(req.DBPath, req.ConfigPath)
	if err != nil {
		return fmt.Errorf("rollback: %w", err)
	}
	switch {
	case snap == nil && req.DBPath == "":
		fmt.Fprintf(os.Stderr, "omega: warning - no store resolved (no config.yaml in the home): the store is NOT restored, this rollback swaps the binary only\n")
	case snap == nil:
		fmt.Fprintf(os.Stderr, "omega: warning - no store snapshot beside %s: the store is NOT restored, this rollback swaps the binary only\n", req.DBPath)
	case live:
		// Force cannot help: the serve holds the sqlite handle, so a copy under it races
		// a write in flight against the file being replaced.
		return fmt.Errorf("omega serve is answering at %s - the store cannot be swapped under a live serve: stop it (Ctrl-C in its terminal, or quit the desktop window), then run the rollback again", req.Origin)
	default:
		fmt.Fprintf(os.Stderr, "omega: restoring %s from %s...\n", req.DBPath, snap.Dir)
		restored, restoreErr := rollbackRestore(snap)
		if restoreErr != nil {
			return fmt.Errorf("rollback stopped before the binary swap: %w", restoreErr)
		}
		fmt.Printf("omega: restored store: %s\n", strings.Join(restored, ", "))
	}

	fmt.Fprintf(os.Stderr, "omega: rolling back %s...\n", req.ExePath)
	if err := rollbackSwap(req.ExePath, bakPath); err != nil {
		return fmt.Errorf("rollback: %w", err)
	}

	out, err := verifyBinaryHealth(req.ExePath)
	if err != nil {
		return fmt.Errorf("rollback installed, but the restored binary failed its health check: %w\nomega.bak and the previous binary are intact - try again", err)
	}
	fmt.Printf("omega: rolled back to %s\n", strings.TrimSpace(out))
	return nil
}

// The rollback's seams: a test drives every half without a real install, a real serve or
// a real store.
var (
	rollbackFindSnapshot = snapshotForRollback
	rollbackRestore      = restoreSnapshot
	rollbackProbe        = probeLiveServe
	rollbackSwap         = func(exePath, bakPath string) error {
		side := exePath + ".rollback"
		if err := os.Rename(exePath, side); err != nil {
			return fmt.Errorf("move running binary aside: %w", err)
		}
		if err := os.Rename(bakPath, exePath); err != nil {
			os.Rename(side, exePath) // put the running binary back
			return fmt.Errorf("install omega.bak: %w", err)
		}
		if err := copyFile(side, bakPath); err != nil {
			fmt.Fprintf(os.Stderr, "omega: warning - could not record replaced version into omega.bak: %v\n", err)
		}
		os.Remove(side)
		return nil
	}
)

// probeLiveServe reports whether an omega serve answers on origin and how many runs it
// holds. Reachability is the fact the store half needs - the serve owns the sqlite handle -
// and the run count only decides whether force may proceed.
func probeLiveServe(origin string) (bool, int) {
	if origin == "" {
		return false, 0
	}
	client := &http.Client{Timeout: 2 * time.Second}
	body, err := healthBody(client, origin)
	if err != nil || !strings.Contains(body, `"status":"ok"`) {
		return false, 0
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimSuffix(origin, "/")+"/updates/status", nil)
	if err != nil {
		return true, 0
	}
	if h := probeHost(origin); h != "" {
		req.Host = h
	}
	resp, err := client.Do(req)
	if err != nil {
		return true, 0
	}
	defer resp.Body.Close()
	var st core.UpdateStatus
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&st); err != nil {
		return true, 0
	}
	return true, st.ActiveRuns
}

// verifyBinaryHealth runs the binary with --version to confirm it executes, returning.
var verifyBinaryHealth = func(exePath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, exePath, "--version").CombinedOutput()
	if ctx.Err() != nil {
		return "", fmt.Errorf("timed out running %s --version", exePath)
	}
	if err != nil {
		return "", fmt.Errorf("%s --version failed: %v: %s", exePath, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// safeJoin joins dest + name and validates the result stays within dest.
func safeJoin(dest, name string) (string, error) {
	path := filepath.Join(dest, name)
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(absPath, absDest+string(filepath.Separator)) && absPath != absDest {
		return "", fmt.Errorf("path traversal: %s", name)
	}
	return path, nil
}

// extractZip extracts a zip archive from r into dest.
func extractZip(r io.Reader, dest string) error {
	// Read all into memory — archives are small (~50MB max).
	data, err := io.ReadAll(io.LimitReader(r, 200<<20))
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		path, err := safeJoin(dest, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(path, 0o755)
			continue
		}
		os.MkdirAll(filepath.Dir(path), 0o755)
		out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.FileInfo().Mode())
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			out.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return err
		}
		out.Close()
		rc.Close()
	}
	return nil
}

// extractTarGz extracts a.tar.gz archive from r into dest.
func extractTarGz(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		path, err := safeJoin(dest, hdr.Name)
		if err != nil {
			return err
		}
		if hdr.FileInfo().IsDir() {
			os.MkdirAll(path, 0o755)
			continue
		}
		os.MkdirAll(filepath.Dir(path), 0o755)
		out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, hdr.FileInfo().Mode())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		out.Close()
	}
	return nil
}

// findInArchive searches dir for a file named "omega" or "omega.exe", checking.
func findInArchive(dir, name string) string {
	for _, ext := range []string{".exe", ""} {
		p := filepath.Join(dir, name+ext)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		for _, ext := range []string{".exe", ""} {
			p := filepath.Join(dir, e.Name(), name+ext)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// replaceBinary installs src over dst atomically, preserving the
// replaced binary as dst+".bak" (one generation, overwritten on every
// update — the rollback target for omega update --rollback).
func replaceBinary(src, dst string) error {
	bakPath := dst + ".bak"
	if runtime.GOOS == "windows" {
		// Windows locks the running binary. The rename itself moves the old binary into
		// the.bak slot (overwrites fail if an old.bak lingers, so remove it first).
		os.Remove(bakPath)
		if err := os.Rename(dst, bakPath); err != nil {
			return fmt.Errorf("back up current exe (omega.bak): %w", err)
		}
		return copyFile(src, dst)
	}
	// Linux/macOS: copy the running binary into the.bak slot.
	if err := copyFile(dst, bakPath); err != nil {
		return fmt.Errorf("back up current exe (omega.bak): %w", err)
	}
	tmp := dst + ".new"
	if err := copyFile(src, tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	os.Remove(dst)
	return os.Rename(tmp, dst)
}

// copyFile copies src to dst, setting permissions.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, _ := os.Stat(src)
	mode := os.FileMode(0o755)
	if info != nil {
		mode = info.Mode()
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// copyIfMissing copies src to dst only if dst does not exist.
func copyIfMissing(src, dst string) {
	if _, err := os.Stat(dst); err == nil {
		return // already exists, don't overwrite
	}
	copyFile(src, dst)
}

// progressReader wraps an io.Reader and prints a progress bar to stderr showing.
type progressReader struct {
	r       io.Reader
	total   int64
	read    int64
	lastPct int
}

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.r.Read(buf)
	p.read += int64(n)
	if p.total > 0 {
		pct := int(p.read * 100 / p.total)
		if pct > p.lastPct+4 || err != nil {
			p.lastPct = pct
			bar := 30
			filled := bar * pct / 100
			fmt.Fprintf(os.Stderr, "\r[")
			for i := 0; i < filled; i++ {
				fmt.Fprint(os.Stderr, "█")
			}
			for i := filled; i < bar; i++ {
				fmt.Fprint(os.Stderr, "░")
			}
			fmt.Fprintf(os.Stderr, "] %d%% %s/%s", pct, humanBytes(p.read), humanBytes(p.total))
		}
	}
	return n, err
}

func humanBytes(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}
