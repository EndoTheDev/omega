package files

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/harness/datadir"
)


// guardFor builds a provider whose guard knows a fake instance db in dir.
func guardFor(t *testing.T, dir string) *FileProvider {
	t.Helper()
	return &FileProvider{guard: datadir.NewGuard(dir + "/omega.db")}
}

// TestGuardedFilesRefuse verifies each file tool entry point individually.
func TestGuardedFilesRefuse(t *testing.T) {
	dir := t.TempDir()
	db := dir + "/omega.db"
	if err := os.WriteFile(db, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := guardFor(t, dir)

	if _, err := p.RunReadFile(context.Background(), map[string]any{"path": db}); err == nil || !strings.Contains(err.Error(), datadir.DBDenyLaw) {
		t.Errorf("RunReadFile: err = %v, want denial", err)
	}
	if _, err := p.RunWriteFile(context.Background(), map[string]any{"path": db + "-wal", "content": "x"}); err == nil || !strings.Contains(err.Error(), datadir.DBDenyLaw) {
		t.Errorf("RunWriteFile on -wal: err = %v, want denial", err)
	}
	if _, err := p.RunEdit(context.Background(), map[string]any{"path": db, "old_string": "x", "new_string": "y"}); err == nil || !strings.Contains(err.Error(), datadir.DBDenyLaw) {
		t.Errorf("RunEdit: err = %v, want denial", err)
	}
	fresh := t.TempDir()
	pf := &FileProvider{guard: datadir.NewGuard(fresh + "/omega.db")}
	target := fresh + "/omega.db"
	if _, serr := os.Stat(target); !os.IsNotExist(serr) {
		t.Fatal("precondition: db file should not exist yet")
	}
	if _, err := pf.RunWriteFile(context.Background(), map[string]any{"path": target, "content": "x"}); err == nil || !strings.Contains(err.Error(), datadir.DBDenyLaw) {
		t.Errorf("RunWriteFile on not-yet-existing instance db: err = %v, want denial", err)
	}
	if _, serr := os.Stat(target); !os.IsNotExist(serr) {
		t.Error("denied RunWriteFile created the instance db file")
	}
}

// TestGuardedFilesAllowNormal: a guard-carrying provider keeps normal file operation.
func TestGuardedFilesAllowNormal(t *testing.T) {
	dir := t.TempDir()
	p := guardFor(t, dir)

	f := dir + "/note.txt"
	if _, err := p.RunWriteFile(context.Background(), map[string]any{"path": f, "content": "hi"}); err != nil {
		t.Fatalf("RunWriteFile normal write: %v", err)
	}
	if _, err := p.RunReadFile(context.Background(), map[string]any{"path": f}); err != nil {
		t.Fatalf("RunReadFile normal read: %v", err)
	}
	if _, err := p.RunEdit(context.Background(), map[string]any{"path": f, "old_string": "hi", "new_string": "ho"}); err != nil {
		t.Fatalf("RunEdit normal edit: %v", err)
	}

	ok := dir + "/omegadocs/omega.db.md"
	if _, err := p.RunWriteFile(context.Background(), map[string]any{"path": ok, "content": "x"}); err != nil {
		t.Errorf("false positive on %q: %v", ok, err)
	}

	bare := &FileProvider{}
	if _, err := bare.RunWriteFile(context.Background(), map[string]any{"path": f, "content": "x"}); err != nil {
		t.Errorf("nil-guard write: %v", err)
	}
}
