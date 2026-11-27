package main

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestReplaceBinaryWritesBak pins the one-file rollback rule: every update overwrites.
func TestReplaceBinaryWritesBak(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "omega")
	src := filepath.Join(dir, "staged-bin")
	writeExe(t, dst, "OLD-BINARY")
	writeExe(t, src, "NEW-BINARY")

	if err := replaceBinary(src, dst); err != nil {
		t.Fatalf("replaceBinary: %v", err)
	}
	assertFile := func(path, want string) {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if string(b) != want {
			t.Errorf("%s = %q, want %q", path, b, want)
		}
	}
	assertFile(dst, "NEW-BINARY")
	assertFile(dst+".bak", "OLD-BINARY")

	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	want := []string{"omega", "omega.bak", "staged-bin"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("dir = %v, want %v (no .new/.old leftovers)", names, want)
	}
}

// TestReplaceBinaryBakOneGeneration: a second update overwrites the.bak slot — it never
// grows, only ever holds the previous generation.
func TestReplaceBinaryBakOneGeneration(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "omega")
	writeExe(t, dst, "V1")
	src := filepath.Join(dir, "next")
	writeExe(t, src, "V2")

	if err := replaceBinary(src, dst); err != nil {
		t.Fatalf("first update: %v", err)
	}
	writeExe(t, src, "V3")
	if err := replaceBinary(src, dst); err != nil {
		t.Fatalf("second update: %v", err)
	}

	for name, want := range map[string]string{
		"omega":     "V3",
		"omega.bak": "V2", // previous generation, not V1
	} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != want {
			t.Errorf("%s = %q, want %q", name, b, want)
		}
	}
}

// TestRollbackSwapRoundTrip: the swap must preserve BOTH versions — the backup lands.
func TestRollbackSwapRoundTrip(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "omega")
	bak := exe + ".bak"
	writeExe(t, exe, "BAD-NIGHTLY")
	writeExe(t, bak, "GOOD-STABLE")

	if err := rollbackSwap(exe, bak); err != nil {
		t.Fatalf("rollbackSwap: %v", err)
	}
	read := func(p string) string {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		return string(b)
	}
	if got := read(exe); got != "GOOD-STABLE" {
		t.Errorf("exe after rollback = %q, want GOOD-STABLE", got)
	}
	if got := read(bak); got != "BAD-NIGHTLY" {
		t.Errorf("bak after rollback = %q, want BAD-NIGHTLY (one generation, overwritten)", got)
	}
	// The side file must be gone: a second rollback round-trips.
	if err := rollbackSwap(exe, bak); err != nil {
		t.Fatalf("second rollbackSwap: %v", err)
	}
	if got := read(exe); got != "BAD-NIGHTLY" {
		t.Errorf("exe after second rollback = %q, want BAD-NIGHTLY", got)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), "rollback") {
			t.Errorf("side file %s left behind", e.Name())
		}
	}
}

// TestRollbackToRoundTrip: the full rollback path — swap, health check against.
func TestRollbackToRoundTrip(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "omega")
	writeExe(t, exe, "BAD-NIGHTLY")
	writeExe(t, exe+".bak", "GOOD-STABLE")

	stubRollbackQuiet(t)
	oldHealth := verifyBinaryHealth
	t.Cleanup(func() { verifyBinaryHealth = oldHealth })
	var checked string
	verifyBinaryHealth = func(path string) (string, error) {
		checked = path
		return "omega v0.9.1\n", nil
	}
	if err := rollbackTo(rollbackRequest{ExePath: exe}); err != nil {
		t.Fatalf("rollbackTo: %v", err)
	}
	if checked != exe {
		t.Errorf("health checked %q, want the restored exe %q", checked, exe)
	}
}

func TestRollbackToNoBak(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "omega")
	writeExe(t, exe, "RUNNING")
	stubRollbackQuiet(t)
	if err := rollbackTo(rollbackRequest{ExePath: exe}); err == nil {
		t.Fatal("no omega.bak present: rollbackTo must error, not wipe the running binary")
	}
	if b, _ := os.ReadFile(exe); string(b) != "RUNNING" {
		t.Error("failed rollback must leave the running binary alone")
	}
}

// TestRollbackToUnhealthy: a backup that fails its health check still leaves the swap
// state intact and reports an error — never a silent success on a broken restore.
func TestRollbackToUnhealthy(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "omega")
	writeExe(t, exe, "BAD-NIGHTLY")
	writeExe(t, exe+".bak", "GOOD-STABLE")

	stubRollbackQuiet(t)
	oldHealth := verifyBinaryHealth
	t.Cleanup(func() { verifyBinaryHealth = oldHealth })
	verifyBinaryHealth = func(path string) (string, error) {
		return "", os.ErrPermission
	}
	if err := rollbackTo(rollbackRequest{ExePath: exe}); err == nil {
		t.Fatal("unhealthy restored binary: rollbackTo must error")
	}
	// The restored version is live and the.bak slot intact.
	read := func(p string) string {
		b, _ := os.ReadFile(p)
		return string(b)
	}
	if read(exe) != "GOOD-STABLE" || read(exe+".bak") != "BAD-NIGHTLY" {
		t.Error("failed health check must leave both slots intact, not destructive")
	}
}

// stubRollbackQuiet pins the instance probe and the health seam so a rollback test never
// dials a serve and never runs a binary.
func stubRollbackQuiet(t *testing.T) {
	t.Helper()
	oldProbe := rollbackProbe
	t.Cleanup(func() { rollbackProbe = oldProbe })
	rollbackProbe = func(string) (bool, int) { return false, 0 }
}

// stubRollbackSeams pins every half the rollback moves; each test overrides what it cares
// about after this returns.
func stubRollbackSeams(t *testing.T) {
	t.Helper()
	stubRollbackQuiet(t)
	oldFind, oldRestore, oldSwap, oldHealth := rollbackFindSnapshot, rollbackRestore, rollbackSwap, verifyBinaryHealth
	t.Cleanup(func() {
		rollbackFindSnapshot, rollbackRestore, rollbackSwap, verifyBinaryHealth = oldFind, oldRestore, oldSwap, oldHealth
	})
	verifyBinaryHealth = func(string) (string, error) { return "omega v0.18.0\n", nil }
}

// rollbackFixture builds the card's store pair: omega.db snapshotted at v0.19.0, then a
// newer row and a stale WAL sidecar on the live file.
func rollbackFixture(t *testing.T) (dir, dbPath, cfgPath string) {
	t.Helper()
	dir = t.TempDir()
	dbPath = filepath.Join(dir, "data", "omega.db")
	cfgPath = filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("updates:\n  channel: stable\n"), 0o600); err != nil {
		t.Fatalf("config: %v", err)
	}
	makeTestStore(t, dbPath) // probes(n) with the marker row n = 1
	if _, err := snapshotBeforeFinalize(dbPath, cfgPath, "v0.19.0"); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	execStore(t, dbPath, `INSERT INTO probes (n) VALUES (2)`)
	if err := os.WriteFile(dbPath+"-wal", []byte("stale"), 0o600); err != nil {
		t.Fatalf("stale wal: %v", err)
	}
	return dir, dbPath, cfgPath
}

// execStore runs one statement against a store file.
func execStore(t *testing.T, path, stmt string) {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("store open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(stmt); err != nil {
		t.Fatalf("store exec %q: %v", stmt, err)
	}
}

// storeRows reads the probes table in insertion order.
func storeRows(t *testing.T, path string) []int {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		t.Fatalf("store read: %v", err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT n FROM probes ORDER BY n`)
	if err != nil {
		t.Fatalf("store query: %v", err)
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("store scan: %v", err)
		}
		out = append(out, n)
	}
	return out
}

// TestRollbackRestoresTheStoreWithTheBinary is the card's acceptance pin: the marker row
// is back, the row written after the snapshot is gone, the stale WAL is removed, the
// binary swap runs after the store is in place, and the receipt lists omega.db.
func TestRollbackRestoresTheStoreWithTheBinary(t *testing.T) {
	dir, dbPath, cfgPath := rollbackFixture(t)
	exe := filepath.Join(dir, "omega")
	writeExe(t, exe, "NEW-BINARY")
	writeExe(t, exe+".bak", "OLD-BINARY")

	stubRollbackSeams(t)
	var swapped string
	rollbackSwap = func(exePath, bakPath string) error {
		swapped = exePath
		if rows := storeRows(t, dbPath); !reflect.DeepEqual(rows, []int{1}) {
			t.Errorf("the binary swap ran with the live store at %v, want the marker row only", rows)
		}
		return nil
	}

	req := rollbackRequest{ExePath: exe, Origin: "http://127.0.0.1:1", DBPath: dbPath, ConfigPath: cfgPath}
	out, err := captureStdout(t, func() error { return rollbackTo(req) })
	if err != nil {
		t.Fatalf("rollbackTo: %v", err)
	}
	if !strings.Contains(out, "omega.db") {
		t.Errorf("receipt %q does not list the restored store", out)
	}
	if !strings.Contains(out, "v0.18.0") {
		t.Errorf("receipt %q does not print the restored binary's version", out)
	}
	if swapped != exe {
		t.Fatalf("binary swap ran on %q, want %q", swapped, exe)
	}
	if rows := storeRows(t, dbPath); !reflect.DeepEqual(rows, []int{1}) {
		t.Errorf("store rows after rollback = %v, want the marker row only (the newer row is gone)", rows)
	}
	if _, err := os.Stat(dbPath + "-wal"); !os.IsNotExist(err) {
		t.Errorf("the stale -wal survived the rollback (err %v)", err)
	}
	body, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(body), "channel: stable") {
		t.Errorf("config.yaml = %q, want the snapshot's channel", body)
	}
}

// TestRollbackWithoutSnapshotSwapsTheBinaryAndSaysSo: a home that never finalized has no
// store rollback point. The binary still swaps - under a live serve, which is the only
// half that may - and the warning names the missing store restore.
func TestRollbackWithoutSnapshotSwapsTheBinaryAndSaysSo(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "data", "omega.db")
	makeTestStore(t, dbPath)
	exe := filepath.Join(dir, "omega")
	writeExe(t, exe, "NEW-BINARY")
	writeExe(t, exe+".bak", "OLD-BINARY")

	stubRollbackSeams(t)
	rollbackProbe = func(string) (bool, int) { return true, 0 }
	var swapped bool
	rollbackSwap = func(string, string) error { swapped = true; return nil }

	req := rollbackRequest{ExePath: exe, Origin: "http://127.0.0.1:1", DBPath: dbPath}
	var rollbackErr error
	stderr := captureStderr(t, func() { rollbackErr = rollbackTo(req) })
	if rollbackErr != nil {
		t.Fatalf("rollbackTo: %v", rollbackErr)
	}
	if !swapped {
		t.Fatal("a home with no snapshot must still swap the binary")
	}
	if !strings.Contains(stderr, "NOT restored") || !strings.Contains(stderr, "no store snapshot") {
		t.Errorf("warning %q does not name the missing store restore", stderr)
	}
}

// TestRollbackRefusesALiveRun: the quiet gate's own line, and neither half moves.
func TestRollbackRefusesALiveRun(t *testing.T) {
	dir, dbPath, cfgPath := rollbackFixture(t)
	exe := filepath.Join(dir, "omega")
	writeExe(t, exe, "NEW-BINARY")
	writeExe(t, exe+".bak", "OLD-BINARY")

	stubRollbackSeams(t)
	rollbackProbe = func(string) (bool, int) { return true, 2 }
	var restored, swapped bool
	rollbackRestore = func(*updateSnapshot) ([]string, error) { restored = true; return []string{"omega.db"}, nil }
	rollbackSwap = func(string, string) error { swapped = true; return nil }

	err := rollbackTo(rollbackRequest{ExePath: exe, Origin: "http://127.0.0.1:1", DBPath: dbPath, ConfigPath: cfgPath})
	if err == nil {
		t.Fatal("a live run must refuse the rollback")
	}
	if want := busyRefusal(2).Error(); err.Error() != want {
		t.Errorf("refusal = %q, want the finalize's own line %q", err, want)
	}
	if restored || swapped {
		t.Errorf("a refused rollback moved a half (restore %v, swap %v)", restored, swapped)
	}
}

// TestRollbackRefusesALiveServe: the store half needs the serve stopped, force cannot
// help, and neither half moves.
func TestRollbackRefusesALiveServe(t *testing.T) {
	dir, dbPath, cfgPath := rollbackFixture(t)
	exe := filepath.Join(dir, "omega")
	writeExe(t, exe, "NEW-BINARY")
	writeExe(t, exe+".bak", "OLD-BINARY")

	stubRollbackSeams(t)
	rollbackProbe = func(string) (bool, int) { return true, 0 }
	var restored, swapped bool
	rollbackRestore = func(*updateSnapshot) ([]string, error) { restored = true; return []string{"omega.db"}, nil }
	rollbackSwap = func(string, string) error { swapped = true; return nil }

	for _, force := range []bool{false, true} {
		err := rollbackTo(rollbackRequest{ExePath: exe, Origin: "http://127.0.0.1:8080", DBPath: dbPath, ConfigPath: cfgPath, Force: force})
		if err == nil {
			t.Fatalf("force=%v: a live serve must refuse the store restore", force)
		}
		if !strings.Contains(err.Error(), "stop it") || !strings.Contains(err.Error(), "http://127.0.0.1:8080") {
			t.Errorf("force=%v: refusal %q does not name the stop step and the origin", force, err)
		}
	}
	if restored || swapped {
		t.Errorf("a refused rollback moved a half (restore %v, swap %v)", restored, swapped)
	}
}

// TestRollbackStopsBeforeTheSwapOnRestoreFailure: a half rollback is the failure this path
// exists to prevent - a failed store restore must leave the binary alone.
func TestRollbackStopsBeforeTheSwapOnRestoreFailure(t *testing.T) {
	dir, dbPath, cfgPath := rollbackFixture(t)
	exe := filepath.Join(dir, "omega")
	writeExe(t, exe, "NEW-BINARY")
	writeExe(t, exe+".bak", "OLD-BINARY")

	stubRollbackSeams(t)
	rollbackRestore = func(*updateSnapshot) ([]string, error) {
		return nil, errors.New("disk full")
	}
	var swapped bool
	rollbackSwap = func(string, string) error { swapped = true; return nil }

	err := rollbackTo(rollbackRequest{ExePath: exe, Origin: "http://127.0.0.1:1", DBPath: dbPath, ConfigPath: cfgPath})
	if err == nil {
		t.Fatal("a failed store restore must fail the rollback")
	}
	if swapped {
		t.Fatal("the binary was swapped after the store restore failed - a half rollback")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("error %q does not carry the restore failure", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "NEW-BINARY" {
		t.Errorf("the executable slot = %q, want the untouched NEW-BINARY", b)
	}
}

// TestRollbackRefusesTwoSnapshots: retention is one, so two directories are a broken
// invariant - never a guess about which generation pairs with omega.bak.
func TestRollbackRefusesTwoSnapshots(t *testing.T) {
	dir, dbPath, cfgPath := rollbackFixture(t)
	exe := filepath.Join(dir, "omega")
	writeExe(t, exe, "NEW-BINARY")
	writeExe(t, exe+".bak", "OLD-BINARY")
	if err := os.MkdirAll(filepath.Join(filepath.Dir(dbPath), snapshotPrefix+"v0.18.0"), 0o700); err != nil {
		t.Fatalf("second snapshot: %v", err)
	}

	stubRollbackSeams(t)
	var swapped bool
	rollbackSwap = func(string, string) error { swapped = true; return nil }

	err := rollbackTo(rollbackRequest{ExePath: exe, Origin: "http://127.0.0.1:1", DBPath: dbPath, ConfigPath: cfgPath})
	if err == nil {
		t.Fatal("two snapshots must refuse the rollback")
	}
	if swapped {
		t.Error("the binary was swapped on an ambiguous snapshot set")
	}
	if !strings.Contains(err.Error(), "retention is one") {
		t.Errorf("error %q does not name the retention contract", err)
	}
}
