package skills

// export.go: the skills tab's two row verbs that are not edits - Export (the folder as a
// downloadable tar.gz) and Archive (the folder moved out of the listing into the
// skills dir's .archive, one ledger row behind it).

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ExportSkill builds the whole skill folder as a tar.gz in memory. The tree is small by
// law (a SKILL.md plus references), so no temp file rides the disk.
func (sp *SkillsProvider) ExportSkill(name string) ([]byte, error) {
	name = strings.TrimSpace(name)
	if !validSkillName(name) {
		return nil, fmt.Errorf("export: invalid skill name %q", name)
	}
	skillFile, err := sp.resolveSkillFile(name)
	if err != nil {
		return nil, err
	}
	srcDir := filepath.Dir(skillFile)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	walkErr := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		rel, rerr := filepath.Rel(srcDir, path)
		if rerr != nil {
			return rerr
		}
		entryName := name + "/"
		if rel != "." {
			entryName = name + "/" + filepath.ToSlash(rel)
			if d.IsDir() {
				entryName += "/"
			}
		}
		hdr, herr := tar.FileInfoHeader(info, "")
		if herr != nil {
			return herr
		}
		hdr.Name = entryName
		// ModTime is the whole reason the archive is reproducible-ish: the skill's own
		// stamp, never the export moment.
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		f, oerr := os.Open(path)
		if oerr != nil {
			return oerr
		}
		_, cerr := io.Copy(tw, f)
		if closeErr := f.Close(); cerr == nil {
			cerr = closeErr
		}
		return cerr
	})
	if walkErr != nil {
		return nil, fmt.Errorf("export %s: %w", name, walkErr)
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("export %s: %w", name, err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("export %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// ArchiveSkillAs moves one skill's folder into the .archive beside the skills dir it was
// read from, with the same sidecar backup + ledger row Delete runs. Returns the fresh
// listing (the tab's one-response contract).
func (sp *SkillsProvider) ArchiveSkillAs(name, actor, reason string) ([]Skill, error) {
	name = strings.TrimSpace(name)
	if !validSkillName(name) {
		return nil, fmt.Errorf("archive: invalid skill name %q", name)
	}
	skillFile, err := sp.resolveSkillFile(name)
	if err != nil {
		return nil, err
	}
	srcDir := filepath.Dir(skillFile)
	root, err := sp.backupDir()
	if err != nil {
		return nil, err
	}
	if _, err := sp.backupFrom(srcDir, root, name, "archive"); err != nil {
		return nil, err
	}
	archivedName := time.Now().UTC().Format("20060102-150405") + "-" + name
	archRoot := filepath.Join(filepath.Dir(srcDir), archiveDirName)
	dest := filepath.Join(archRoot, archivedName)
	if _, err := sp.AppendLedger(actor, LedgerArchive, name, reason,
		[]LedgerFile{{Path: filepath.ToSlash(skillFile)}}, map[string]any{"archived_to": filepath.ToSlash(dest)}); err != nil {
		return nil, err
	}
	if err := moveDir(srcDir, dest); err != nil {
		return nil, fmt.Errorf("archive %s: %w", name, err)
	}
	return sp.ListSkills()
}

const archiveDirName = ".archive"
