package main

import (
	"archive/tar"
	"archive/zip"
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
)

// githubRelease represents the subset of the GitHub releases API
// response that omega update needs.
type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
	Message string        `json:"message"` // API error message (e.g. "Not Found")
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// assetNameForOS returns the expected release asset name substring for
// the current GOOS/GOARCH (e.g. "windows_amd64", "linux_amd64").
func assetNameForOS(goos, goarch string) string {
	return goos + "_" + goarch
}

// findAsset returns the download URL for the asset matching the current
// OS/arch, or "" if none matches. Matches by substring (e.g.
// "omega_v0.1.0_windows_amd64.zip" matches "windows_amd64").
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
}

// parseUpdateFlags parses the flags accepted after "update" (or after
// the "rollback" subcommand). Unknown flags are errors — typo'd flags
// must not silently fall through to a plain stable update.
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
		default:
			if strings.HasPrefix(a, "-") {
				return opts, fmt.Errorf("unknown update flag %q (known: --nightly, --rollback, --allow-downgrade)", a)
			}
			// Positional arg: ignore. omega accepts trailing noise
			// in other subcommands too.
		}
	}
	return opts, nil
}

// updateGate decides whether the channel's offered tag may be
// installed when the running version is omegaVersion. Returns:
//
//	"skip"    — offered equals current, nothing to do
//	"install" — offered is newer, or a downgrade was explicitly allowed
//	"refuse"  — offered is older than current (stale mirror protection)
func updateGate(offeredTag, current string, allowDowngrade bool) (string, error) {
	cmp, ok := compareVersions(offeredTag, current)
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

// githubAPIBase is the GitHub API root for release lookups. Tests and
// fixture flows point it at a local server via OMEGA_UPDATE_API_BASE.
var githubAPIBase = "https://api.github.com"

// repoSlug is the repo whose releases omega installs from.
const repoSlug = "EndoTheDev/omega"

// fetchStable resolves the LATEST STABLE release via the /releases/latest
// endpoint, which GitHub serves only for non-prerelease, non-draft
// releases — nightly tags (and other prereleases) are invisible to it.
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

// fetchNightly resolves the NEWEST -nightly.* prerelease tag from the
// release list (one page of 100 — the nightly channel tags at most one
// per day, so the page always covers recent nightlies).
func fetchNightly() (githubRelease, error) {
	resp, err := http.Get(githubAPIBase + "/repos/" + repoSlug + "/releases?per_page=100")
	if err != nil {
		return githubRelease{}, fmt.Errorf("fetch releases: %w", err)
	}
	defer resp.Body.Close()

	var rels []githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&rels); err != nil {
		return githubRelease{}, fmt.Errorf("decode releases: %w", err)
	}

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

// compareTagNewer reports whether tag a sorts above tag b, treating an
// unparseable tag as never-newer so oddball prerelease tags can't win
// the nightly pick over valid semver tags.
func compareTagNewer(a, b string) bool {
	cmp, ok := compareVersions(a, b)
	return ok && cmp > 0
}

// cmdUpdate installs a release. With no flags it resolves the latest
// stable; --nightly resolves the newest prerelease on the nightly
// channel; --rollback swaps back to omega.bak. Every install preserves
// the replaced binary as omega.bak beside it (one generation).
func cmdUpdate(rest []string) error {
	if base := os.Getenv("OMEGA_UPDATE_API_BASE"); base != "" {
		githubAPIBase = base // fixture flow for tests; never set in prod
	}

	opts, err := parseUpdateFlags(rest)
	if err != nil {
		return err
	}
	if opts.rollback {
		return cmdRollback()
	}

	channel := "stable"
	if opts.nightly {
		channel = "nightly"
	}
	fmt.Fprintf(os.Stderr, "omega: checking for latest %s release...\n", channel)

	var rel githubRelease
	if opts.nightly {
		rel, err = fetchNightly()
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

	fmt.Fprintf(os.Stderr, "omega: updating from %s to %s (%s)...\n", omegaVersion, rel.TagName, channel)
	return installArchive(rel, opts.nightly)
}

// installArchive is the shared download-verify-extract-replace flow for
// every release channel, plus the extension data + example files copy.
func installArchive(rel githubRelease, nightly bool) error {
	url := findAsset(rel.Assets, runtime.GOOS, runtime.GOARCH)
	if url == "" {
		return fmt.Errorf("no release asset for %s/%s in %s; visit https://github.com/%s/releases", runtime.GOOS, runtime.GOARCH, rel.TagName, repoSlug)
	}

	// Determine the asset name for checksum lookup.
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

	// Download archive to a temp file so we can verify its checksum
	// before extracting untrusted code.
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

	// Wrap with progress reader if we know the size.
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

	// Extract archive from the verified temp file.
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

	// Find the omega binary in the extracted archive.
	// It may be at the root or inside a versioned directory.
	omegaBin := findInArchive(extractDir, "omega")
	if omegaBin == "" {
		return fmt.Errorf("omega binary not found in release archive")
	}

	// Replace the running binary, preserving the old one as omega.bak.
	if err := replaceBinary(omegaBin, exePath); err != nil {
		return fmt.Errorf("replace omega binary: %w", err)
	}

	// Copy extension data files from subdirectories (config, mcp.yaml,
	// etc.). Extensions themselves are compiled into omega.exe.
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

	// Print newline after progress bar.
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

// cmdRollback swaps the current binary back to omega.bak (one
// generation, overwritten on every update) and verifies the restored
// binary actually runs. For the bad-nightly case.
func cmdRollback() error {
	if base := os.Getenv("OMEGA_UPDATE_API_BASE"); base != "" {
		githubAPIBase = base // keep the env seam consistent, though fetch is unused
	}
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	return rollbackTo(exePath)
}

// rollbackTo is the platform-independent core of rollback, given the
// executable path. Package var seam only for the health check.
var rollbackTo = func(exePath string) error {
	bakPath := exePath + ".bak"
	if _, err := os.Stat(bakPath); err != nil {
		return fmt.Errorf("no omega.bak beside %s — nothing to roll back to", exePath)
	}

	fmt.Fprintf(os.Stderr, "omega: rolling back %s...\n", exePath)
	if err := rollbackSwap(exePath, bakPath); err != nil {
		return fmt.Errorf("rollback: %w", err)
	}

	// Verify the restored binary executes before declaring victory.
	out, err := verifyBinaryHealth(exePath)
	if err != nil {
		return fmt.Errorf("rollback installed, but the restored binary failed its health check: %w\nomega.bak and the previous binary are intact — try again", err)
	}
	fmt.Printf("omega: rolled back to %s\n", strings.TrimSpace(out))
	return nil
}

// rollbackSwap moves the backup into the executable slot and the
// replaced binary into the backup slot, via a side temp name so both
// versions survive any interruption mid-swap. The running process keeps
// its inode on Linux; on Windows renaming a running exe is allowed
// while deleting it is not.
func rollbackSwap(exePath, bakPath string) error {
	side := exePath + ".rollback"
	if err := os.Rename(exePath, side); err != nil {
		return fmt.Errorf("move running binary aside: %w", err)
	}
	if err := os.Rename(bakPath, exePath); err != nil {
		os.Rename(side, exePath) // put the running binary back
		return fmt.Errorf("install omega.bak: %w", err)
	}
	if err := copyFile(side, bakPath); err != nil {
		// The restored binary is already live; recording the replaced
		// version into the .bak slot is best-effort.
		fmt.Fprintf(os.Stderr, "omega: warning — could not record replaced version into omega.bak: %v\n", err)
	}
	os.Remove(side)
	return nil
}

// verifyBinaryHealth runs the binary with --version to confirm it
// executes, returning its version output. Package var so tests stub it.
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
// Prevents path traversal (CWE-22) from malicious archive entries.
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
	// Limit to 200MB to prevent OOM from malicious archives.
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

// extractTarGz extracts a .tar.gz archive from r into dest.
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

// findInArchive searches dir for a file named "omega" or "omega.exe",
// checking the root first, then one level down.
func findInArchive(dir, name string) string {
	// Check root.
	for _, ext := range []string{".exe", ""} {
		p := filepath.Join(dir, name+ext)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Check one level down (versioned directory).
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
		// Windows locks the running binary: renaming it away is
		// allowed, deleting/updating in place is not. The rename itself
		// moves the old binary into the .bak slot (overwrites fail if
		// an old .bak lingers, so remove it first).
		os.Remove(bakPath)
		if err := os.Rename(dst, bakPath); err != nil {
			return fmt.Errorf("back up current exe (omega.bak): %w", err)
		}
		return copyFile(src, dst)
	}
	// Linux/macOS: copy the running binary into the .bak slot (the
	// running process keeps its inode), then install via a temp rename.
	if err := copyFile(dst, bakPath); err != nil {
		return fmt.Errorf("back up current exe (omega.bak): %w", err)
	}
	tmp := dst + ".new"
	if err := copyFile(src, tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	// Unlink the running executable before renaming. Linux allows
	// unlinking a running binary — the inode stays alive until the
	// process exits, but the path is freed for the rename.
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

// progressReader wraps an io.Reader and prints a progress bar to stderr
// showing download progress against the total size.
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
