package api

// The served-SPA-mismatch ratchet. Twice the lane served a bundle the build
// never embedded: a stale bundle (2026-11-13 BandSelect) and a black page
// (2026-11-22, `//go:embed static/*` drops every '_'-prefixed name, so the
// whole _nuxt entry chunk vanished while /health stayed green). Compare the
// embedded FS against the committed api/static on disk, file by file: the
// class is any embed-exclusion rule, not the one directive that bit.

import (
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"testing"
)

func embeddedStaticSizes(t *testing.T) map[string]int64 {
	t.Helper()
	sizes := map[string]int64{}
	err := fs.WalkDir(staticFS, "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		sizes[strings.TrimPrefix(p, "static/")] = info.Size()
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded staticFS: %v", err)
	}
	return sizes
}

func diskStaticSizes(t *testing.T) map[string]int64 {
	t.Helper()
	sizes := map[string]int64{}
	err := fs.WalkDir(os.DirFS("."), "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		sizes[strings.TrimPrefix(p, "static/")] = info.Size()
		return nil
	})
	if err != nil {
		t.Fatalf("walk api/static on disk: %v", err)
	}
	return sizes
}

// TestEmbeddedStaticCompleteness fails when api/static holds a file the binary
// does not embed, or embeds it at a different size.
func TestEmbeddedStaticCompleteness(t *testing.T) {
	onDisk := diskStaticSizes(t)
	if len(onDisk) == 0 {
		t.Fatal("api/static is empty on disk - the SPA build output is missing")
	}
	embedded := embeddedStaticSizes(t)

	var missing, resized []string
	for name, size := range onDisk {
		got, ok := embedded[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		if got != size {
			resized = append(resized, fmt.Sprintf("%s: disk %d bytes, embedded %d", name, size, got))
		}
	}
	sort.Strings(missing)
	sort.Strings(resized)

	if len(missing) > 0 || len(resized) > 0 {
		t.Fatalf("embedded static does not match api/static (%d on disk, %d embedded):\n"+
			"missing from the binary (go:embed excluded them):\n  %s\n"+
			"size mismatch:\n  %s",
			len(onDisk), len(embedded),
			strings.Join(missing, "\n  "), strings.Join(resized, "\n  "))
	}
}
