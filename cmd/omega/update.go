package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// cmdUpdate downloads the latest GitHub release archive (zip on
// Windows, tar.gz on Linux) and replaces the running executable + all
// extension binaries. User config files (config.yaml, mcp.yaml) are
// preserved — only .example files are extracted.
func cmdUpdate() error {
	fmt.Fprintln(os.Stderr, "omega: checking for latest release...")

	resp, err := http.Get("https://api.github.com/repos/EndoTheDev/omega/releases/latest")
	if err != nil {
		return fmt.Errorf("fetch releases: %w", err)
	}
	defer resp.Body.Close()

	var rel githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return fmt.Errorf("decode release: %w", err)
	}

	if rel.Message != "" {
		return fmt.Errorf("no releases found: %s; visit https://github.com/EndoTheDev/omega/releases", rel.Message)
	}
	if rel.TagName == "" {
		return fmt.Errorf("no releases found; visit https://github.com/EndoTheDev/omega/releases")
	}

	// Skip if already running the latest version.
	if rel.TagName == omegaVersion {
		fmt.Printf("omega: already up to date (%s)\n", omegaVersion)
		return nil
	}
	fmt.Fprintf(os.Stderr, "omega: updating from %s to %s...\n", omegaVersion, rel.TagName)

	url := findAsset(rel.Assets, runtime.GOOS, runtime.GOARCH)
	if url == "" {
		return fmt.Errorf("no release asset for %s/%s in %s; visit https://github.com/EndoTheDev/omega/releases", runtime.GOOS, runtime.GOARCH, rel.TagName)
	}

	dlResp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: HTTP %d", dlResp.StatusCode)
	}

	// Wrap with progress reader if we know the size.
	var body io.Reader = dlResp.Body
	if total := dlResp.ContentLength; total > 0 {
		body = &progressReader{r: dlResp.Body, total: total}
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	installDir := filepath.Dir(exePath)

	// Extract archive to a temp directory.
	tmpDir, err := os.MkdirTemp("", "omega-update-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if runtime.GOOS == "windows" {
		if err := extractZip(body, tmpDir); err != nil {
			return fmt.Errorf("extract zip: %w", err)
		}
	} else {
		if err := extractTarGz(body, tmpDir); err != nil {
			return fmt.Errorf("extract tar.gz: %w", err)
		}
	}

	// Find the omega binary in the extracted archive.
	// It may be at the root or inside a versioned directory.
	omegaBin := findInArchive(tmpDir, "omega")
	if omegaBin == "" {
		return fmt.Errorf("omega binary not found in release archive")
	}

	// Replace the running binary.
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
	fmt.Printf("omega: updated to %s (omega + %d extensions)\n", rel.TagName, extCount)
	return nil
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

// replaceBinary copies src to dst atomically. On Windows the running
// exe is renamed to .old first (Windows locks the running binary).
// On Linux/macOS a temp file is written and renamed (atomic, the
// running process keeps its inode).
func replaceBinary(src, dst string) error {
	if runtime.GOOS == "windows" {
		oldPath := dst + ".old"
		os.Remove(oldPath)
		if err := os.Rename(dst, oldPath); err != nil {
			return fmt.Errorf("rename current exe: %w", err)
		}
		if err := copyFile(src, dst); err != nil {
			os.Rename(oldPath, dst)
			return err
		}
		os.Remove(oldPath)
		return nil
	}
	tmp := dst + ".new"
	if err := copyFile(src, tmp); err != nil {
		os.Remove(tmp)
		return err
	}
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