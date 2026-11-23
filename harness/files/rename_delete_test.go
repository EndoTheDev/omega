package files

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// TestFilesRenameRoundtrip is the api-layer roundtrip for the files menu's Rename: one
// POST moves the leaf, the old name is gone, the bytes are untouched.
func TestFilesRenameRoundtrip(t *testing.T) {
	dir := chdirToWorkdir(t)
	_ = os.MkdirAll(filepath.Join(dir, "rn-arc"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "rn-arc", "old.txt"), []byte("payload"), 0o644)

	res, body := filesPost(t, "/files/rename", `{"path":"rn-arc/old.txt","name":"new.txt"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("rename status = %d, want 200 (%v)", res.StatusCode, body)
	}
	if got, _ := body["path"].(string); got != "rn-arc/new.txt" {
		t.Fatalf("rename answered path %q, want rn-arc/new.txt", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "rn-arc", "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("old leaf survived the rename: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "rn-arc", "new.txt"))
	if err != nil || string(got) != "payload" {
		t.Fatalf("renamed leaf = %q, %v; want payload", got, err)
	}

	// a directory renames the same way
	_ = os.Mkdir(filepath.Join(dir, "rn-dir"), 0o755)
	res, _ = filesPost(t, "/files/rename", `{"path":"rn-dir","name":"rn-dir2"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("directory rename status = %d, want 200", res.StatusCode)
	}
	if info, err := os.Stat(filepath.Join(dir, "rn-dir2")); err != nil || !info.IsDir() {
		t.Fatalf("renamed directory missing: %v", err)
	}
}

// TestFilesRenameRefusals pins the honest refusals: same-dir overwrite (409), a
// multi-segment or traversal name (400), a missing source (404) and a non-POST (405).
func TestFilesRenameRefusals(t *testing.T) {
	dir := chdirToWorkdir(t)
	_ = os.MkdirAll(filepath.Join(dir, "rn-ref"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "rn-ref", "a.txt"), []byte("a"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "rn-ref", "b.txt"), []byte("b"), 0o644)

	cases := []struct {
		json string
		want int
		why  string
	}{
		{`{"path":"rn-ref/a.txt","name":"b.txt"}`, http.StatusConflict, "target exists"},
		{`{"path":"rn-ref/a.txt","name":"sub/a.txt"}`, http.StatusBadRequest, "separator"},
		{`{"path":"rn-ref/a.txt","name":".."}`, http.StatusBadRequest, "traversal leaf"},
		{`{"path":"rn-ref/a.txt","name":""}`, http.StatusBadRequest, "empty name"},
		{`{"path":"rn-ref/ghost.txt","name":"c.txt"}`, http.StatusNotFound, "missing source"},
		{`{"path":"../escape.txt","name":"c.txt"}`, http.StatusBadRequest, "escaping source"},
	}
	for _, c := range cases {
		res, body := filesPost(t, "/files/rename", c.json)
		if res.StatusCode != c.want {
			t.Errorf("%s (%s) status = %d, want %d (%v)", c.json, c.why, res.StatusCode, c.want, body)
		}
	}
	// a dotfile is an ordinary row: the tree lists it, so a rename may carry one
	_ = os.WriteFile(filepath.Join(dir, "rn-ref", ".env"), []byte("k=v"), 0o644)
	dres, _ := filesPost(t, "/files/rename", `{"path":"rn-ref/.env","name":".env.bak"}`)
	if dres.StatusCode != http.StatusOK {
		t.Fatalf("dotfile rename status = %d, want 200", dres.StatusCode)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "rn-ref", ".env.bak")); err != nil || string(got) != "k=v" {
		t.Fatalf("dotfile rename = %q, %v; want k=v", got, err)
	}

	if got, err := os.ReadFile(filepath.Join(dir, "rn-ref", "a.txt")); err != nil || string(got) != "a" {
		t.Fatalf("a refused rename touched the source: %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "rn-ref", "b.txt")); err != nil || string(got) != "b" {
		t.Fatalf("a refused rename overwrote the target: %q, %v", got, err)
	}
}

// TestFilesDeleteRoundtrip is the Delete roundtrip: the file's bytes go, its siblings
// stay, and the root refuses to be a deletable row.
func TestFilesDeleteRoundtrip(t *testing.T) {
	dir := chdirToWorkdir(t)
	_ = os.MkdirAll(filepath.Join(dir, "del-arc"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "del-arc", "gone.txt"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "del-arc", "kept.txt"), []byte("y"), 0o644)

	res, body := filesPost(t, "/files/delete", `{"path":"del-arc/gone.txt"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d, want 200 (%v)", res.StatusCode, body)
	}
	if _, err := os.Stat(filepath.Join(dir, "del-arc", "gone.txt")); !os.IsNotExist(err) {
		t.Fatalf("deleted leaf survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "del-arc", "kept.txt")); err != nil {
		t.Fatalf("sibling went with the delete: %v", err)
	}

	// a directory row deletes the whole folder
	res, _ = filesPost(t, "/files/delete", `{"path":"del-arc"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("directory delete status = %d, want 200", res.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(dir, "del-arc")); !os.IsNotExist(err) {
		t.Fatalf("deleted directory survived: %v", err)
	}

	for _, c := range []struct {
		json string
		want int
		why  string
	}{
		{`{"path":""}`, http.StatusBadRequest, "the root is not a row"},
		{`{"path":"../x"}`, http.StatusBadRequest, "escaping path"},
		{`{"path":"never-there.txt"}`, http.StatusNotFound, "missing leaf"},
	} {
		res, _ := filesPost(t, "/files/delete", c.json)
		if res.StatusCode != c.want {
			t.Errorf("%s (%s) status = %d, want %d", c.json, c.why, res.StatusCode, c.want)
		}
	}
}
