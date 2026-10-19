package logging

import (
	"path/filepath"
	"testing"
)

// Path reports the file the logger writes to (card t_db430fea - the
// /logs endpoint tails exactly this path against the exact open file).
func TestFileLoggerPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	l, err := NewFileLogger(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if got := l.Path(); got != path {
		t.Fatalf("Path() = %q, want %q", got, path)
	}
}
