package notifications

// The finished-things REST face's self-check (moved whole from
// agent/api/server_notify_test.go by the shell-ring shrink wave 3,
// card t_b75df168 - re-aimed at the capsule's Routes over a real
// Store). The wire contract: list (newest first, the live unread count
// whole-set even under limit=1 - the badge math), pagination, the
// read marks (one, ghost-id 404, all) and the live SSE stream where a
// background emission surfaces as one "notification" frame. The old
// 501-without-seam probe died with the surface's home: the route is a
// module route now, a disabled capsule answers 404 at the module mux.

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// serveRoutes builds the capsule routes over a fresh emittingStore on
// a throwaway db, muxed with the exact patterns RegisterRoute uses.
func serveRoutes(t *testing.T) (*httptest.Server, *Store) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "notifications.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open notifications: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	rt := newRoutes(&emittingStore{store: st, capsule: newCapsule(st)})
	mux := http.NewServeMux()
	mux.HandleFunc("/notifications", rt.handleNotifications)
	mux.HandleFunc("/notifications/read", rt.handleNotificationsRead)
	mux.HandleFunc("/notifications/events", rt.handleNotificationEvents)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, st
}

var _ = os.Getpid // keep the os import honest if the seeds change shape

func TestNotificationsListUnreadAndPagination(t *testing.T) {
	ts, st := serveRoutes(t)
	for i := 0; i < 3; i++ {
		if _, err := st.Emit(core.Notice{Kind: "task", SourceID: "t_test", Title: "task t_test done"}); err != nil {
			t.Fatalf("emit: %v", err)
		}
	}

	// full list: newest first, unread count in the payload
	res, err := http.Get(ts.URL + "/notifications")
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Notifications []core.Notice `json:"notifications"`
		Unread        int           `json:"unread"`
	}
	json.NewDecoder(res.Body).Decode(&page)
	res.Body.Close()
	if res.StatusCode != 200 || len(page.Notifications) != 3 || page.Unread != 3 {
		t.Fatalf("list = %d rows / %d unread (http %d)", len(page.Notifications), page.Unread, res.StatusCode)
	}
	if page.Notifications[0].Kind != "task" || page.Notifications[0].Read {
		t.Fatalf("newest row wrong: %+v", page.Notifications[0])
	}

	// pagination: limit=1 keeps the unread count whole-set
	res, err = http.Get(ts.URL + "/notifications?limit=1")
	if err != nil {
		t.Fatal(err)
	}
	var page2 struct {
		Notifications []core.Notice `json:"notifications"`
		Unread        int           `json:"unread"`
	}
	json.NewDecoder(res.Body).Decode(&page2)
	res.Body.Close()
	if len(page2.Notifications) != 1 || page2.Unread != 3 {
		t.Fatalf("limit=1 = %d rows / %d unread", len(page2.Notifications), page2.Unread)
	}

	// bad limit is a 400, not a panic
	res, _ = http.Get(ts.URL + "/notifications?limit=zero")
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad limit = %d, want 400", res.StatusCode)
	}

	// mark one read
	res, err = http.Post(ts.URL+"/notifications/read", "application/json", strings.NewReader(`{"id":1}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("mark one = %d", res.StatusCode)
	}
	res, _ = http.Get(ts.URL + "/notifications?unread=1")
	json.NewDecoder(res.Body).Decode(&page)
	res.Body.Close()
	if len(page.Notifications) != 2 || page.Unread != 2 {
		t.Fatalf("after mark one: %d unread rows / %d unread", len(page.Notifications), page.Unread)
	}

	// ghost id is a 404
	res, _ = http.Post(ts.URL+"/notifications/read", "application/json", strings.NewReader(`{"id":99}`))
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("ghost mark = %d, want 404", res.StatusCode)
	}

	// mark all: the badge clears server-side
	res, _ = http.Post(ts.URL+"/notifications/read", "application/json", strings.NewReader(`{"all":true}`))
	res.Body.Close()
	res, _ = http.Get(ts.URL + "/notifications")
	json.NewDecoder(res.Body).Decode(&page)
	res.Body.Close()
	if page.Unread != 0 {
		t.Fatalf("mark all left %d unread", page.Unread)
	}
}

func TestNotificationEventsStreamDeliversEmissions(t *testing.T) {
	ts, st := serveRoutes(t)

	req, _ := http.NewRequest("GET", ts.URL+"/notifications/events", nil)
	req.Header.Set("Accept", "text/event-stream")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("events = %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("events content type %q", ct)
	}

	// a background emission must surface as a "notification" frame
	done := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(res.Body)
		scanner.Buffer(make([]byte, 64*1024), 64*1024)
		var frame strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			frame.WriteString(line + "\n")
			if strings.HasPrefix(line, "data:") {
				done <- frame.String()
				return
			}
		}
		done <- ""
	}()

	time.Sleep(100 * time.Millisecond) // the client is attached
	if _, err := st.Emit(core.Notice{Kind: "cron", SourceID: "nightly", Title: "cron nightly finished", Verdict: "failed"}); err != nil {
		t.Fatalf("emit: %v", err)
	}

	select {
	case frame := <-done:
		if !strings.Contains(frame, "event: notification") {
			t.Fatalf("frame lost the event type: %q", frame)
		}
		if !strings.Contains(frame, `"kind":"cron"`) || !strings.Contains(frame, `"verdict":"failed"`) {
			t.Fatalf("frame lost the record body: %q", frame)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("no notification frame within 3s")
	}
}
