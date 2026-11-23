package store

// index_rebuild_test.go: the index is DERIVED. Delete it and the next open rebuilds it
// from the session files; the rebuild produces the same sessions and the same search
// results, because both are the same fold of the same lines.

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// rebuildFixture writes two sessions with searchable content, a label, a summary and a
// token count.
func rebuildFixture(t *testing.T) string {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "rebuild.db")
	ctx := context.Background()
	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	if err := s.CreateSession(ctx, "alpha", "", "syncthing setup"); err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	if err := s.AppendMessage(ctx, "alpha", core.NewUser("the syncthing device pairing failed on the pi")); err != nil {
		t.Fatalf("append alpha: %v", err)
	}
	if err := s.AppendMessage(ctx, "alpha", core.NewAssistant("paired it by hand")); err != nil {
		t.Fatalf("append alpha 2: %v", err)
	}
	if err := s.SetSessionSummary(ctx, "alpha", "syncthing pairing walkthrough"); err != nil {
		t.Fatalf("summary: %v", err)
	}
	if err := s.AddSessionTokens(ctx, "alpha", 100, 20); err != nil {
		t.Fatalf("tokens: %v", err)
	}
	if err := s.CreateSession(ctx, "beta", "", "kanban"); err != nil {
		t.Fatalf("create beta: %v", err)
	}
	if err := s.AppendMessage(ctx, "beta", core.NewUser("card dispatch stalls after a reindex")); err != nil {
		t.Fatalf("append beta: %v", err)
	}
	return dsn
}

func TestColdRebuildMatchesTheLiveIndex(t *testing.T) {
	dsn := rebuildFixture(t)
	ctx := context.Background()

	before, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	wantSessions, err := before.ListSessions(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	wantSearch, err := before.SearchSessions(ctx, "syncthing")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	wantMessages, err := before.GetMessages(ctx, "alpha")
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if err := before.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Delete the whole derived index: the database and its journal sidecars.
	for _, path := range []string{dsn, dsn + "-wal", dsn + "-shm"} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Fatalf("delete index %s: %v", path, err)
		}
	}

	after, err := Open(dsn)
	if err != nil {
		t.Fatalf("reopen on a deleted index: %v", err)
	}
	defer after.Close()
	gotSessions, err := after.ListSessions(ctx)
	if err != nil {
		t.Fatalf("list after rebuild: %v", err)
	}
	if !reflect.DeepEqual(summarize(wantSessions), summarize(gotSessions)) {
		t.Fatalf("sessions after a cold rebuild = %+v, want %+v", summarize(gotSessions), summarize(wantSessions))
	}
	gotSearch, err := after.SearchSessions(ctx, "syncthing")
	if err != nil {
		t.Fatalf("search after rebuild: %v", err)
	}
	if !reflect.DeepEqual(wantSearch, gotSearch) {
		t.Fatalf("search after a cold rebuild = %+v, want %+v", gotSearch, wantSearch)
	}
	gotMessages, err := after.GetMessages(ctx, "alpha")
	if err != nil {
		t.Fatalf("messages after rebuild: %v", err)
	}
	if len(gotMessages) != len(wantMessages) {
		t.Fatalf("messages after a cold rebuild = %d, want %d", len(gotMessages), len(wantMessages))
	}
	// The token totals survive: they are folded deltas, not a column that only the
	// dropped database remembered.
	alpha, err := after.GetSession(ctx, "alpha")
	if err != nil {
		t.Fatalf("get alpha: %v", err)
	}
	if alpha.PromptTokens != 100 || alpha.CompletionTokens != 20 {
		t.Fatalf("token totals after rebuild = %d/%d, want 100/20", alpha.PromptTokens, alpha.CompletionTokens)
	}
	if alpha.Summary == "" {
		t.Fatal("summary lost by the rebuild")
	}
}

// summarize drops the volatile bits so two reads compare as the same store.
func summarize(sessions []core.Session) []string {
	out := make([]string, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, s.ID+"|"+s.Label+"|"+s.CreatedAt+"|"+s.UpdatedAt+"|"+s.Agent+"|"+s.Summary)
	}
	return out
}

// TestRebuildVerbRestoresACorruptedIndex: the manual verb is the recovery door. Damage
// the index (rows and the FTS content), run the verb, and the store answers from the
// files again.
func TestRebuildVerbRestoresACorruptedIndex(t *testing.T) {
	dsn := rebuildFixture(t)
	ctx := context.Background()
	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	if _, err := s.db.Exec(`DELETE FROM messages`); err != nil {
		t.Fatalf("damage the index: %v", err)
	}
	if res, err := s.SearchMessages(ctx, "syncthing"); err != nil {
		t.Fatalf("search on a damaged index: %v", err)
	} else if len(res) != 0 {
		t.Fatalf("damaged index still answers: %+v", res)
	}

	if err := s.RebuildIndex(ctx); err != nil {
		t.Fatalf("RebuildIndex: %v", err)
	}
	results, err := s.SearchMessages(ctx, "syncthing")
	if err != nil {
		t.Fatalf("search after rebuild: %v", err)
	}
	if len(results) != 1 || results[0].SessionID != "alpha" {
		t.Fatalf("search after RebuildIndex = %+v, want the alpha hit", results)
	}
	msgs, err := s.GetMessages(ctx, "alpha")
	if err != nil {
		t.Fatalf("messages after rebuild: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("messages after rebuild = %d, want 2", len(msgs))
	}
}

// TestPreSearchCatchUpIndexesGrownFiles: a line another writer appended (the file grew,
// the index did not follow) becomes searchable on the next search, and the watermark
// advances to the file's size - the catch-up read only the grown bytes.
func TestPreSearchCatchUpIndexesGrownFiles(t *testing.T) {
	dsn := rebuildFixture(t)
	ctx := context.Background()
	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	path, err := s.sessionPath("beta")
	if err != nil {
		t.Fatalf("session path: %v", err)
	}
	before, err := s.watermark(ctx, "beta")
	if err != nil {
		t.Fatalf("watermark: %v", err)
	}
	if _, _, err := appendLines(path, fileLine{
		V: jsonlVersion, Kind: kindMessage, Session: "beta", ID: 99,
		Role:    "assistant",
		Payload: []byte(`{"content":"a straggler line from another writer"}`),
		At:      "2026-11-23T09:00:00Z",
	}); err != nil {
		t.Fatalf("foreign append: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	results, err := s.SearchMessages(ctx, "straggler")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].SessionID != "beta" {
		t.Fatalf("pre-search catch-up missed the grown line: %+v", results)
	}
	after, err := s.watermark(ctx, "beta")
	if err != nil {
		t.Fatalf("watermark after search: %v", err)
	}
	if after != info.Size() {
		t.Fatalf("watermark = %d, want the file size %d", after, info.Size())
	}
	if after <= before {
		t.Fatalf("watermark did not advance: %d -> %d", before, after)
	}
}
