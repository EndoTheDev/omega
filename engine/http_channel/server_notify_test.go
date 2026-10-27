package http_channel

import (
	"bufio"
	"encoding/json"
	"net/http"
	httptest "net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/modules/notifications"
)

// notifyTestServer builds a Server + a real finished-things store, and
// a test server over it. Emissions drive through the store directly -
// the emitter modules own the emit-path tests; these pin the HTTP
// contract (list / read / live SSE frames).
func serveNotifications(t *testing.T) (*httptest.Server, *notifications.Store) {
	t.Helper()
	s := NewServer(nil, nil, nil, nil, nil)
	st, err := notifications.Open(":memory:")
	if err != nil {
		t.Fatalf("open notifications: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s.seams.notifications = st
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, st
}

func TestNotificationsEndpoints501WithoutSeam(t *testing.T) {
	ts := httptest.NewServer(NewServer(nil, nil, nil, nil, nil).Handler())
	t.Cleanup(ts.Close)

	for _, probe := range []struct{ method, path string }{
		{"GET", "/notifications"},
		{"GET", "/notifications/events"},
		{"POST", "/notifications/read"},
	} {
		req, _ := http.NewRequest(probe.method, ts.URL+probe.path, strings.NewReader(`{}`))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotImplemented {
			t.Fatalf("%s %s = %d, want 501", probe.method, probe.path, res.StatusCode)
		}
	}
}

func TestNotificationsListUnreadAndPagination(t *testing.T) {
	ts, st := serveNotifications(t)
	for i := 0; i < 3; i++ {
		if _, err := st.Emit(core.Notification{Kind: "task", SourceID: "t_test", Title: "task t_test done"}); err != nil {
			t.Fatalf("emit: %v", err)
		}
	}

	// full list: newest first, unread count in the payload
	res, err := http.Get(ts.URL + "/notifications")
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Notifications []core.Notification `json:"notifications"`
		Unread        int                 `json:"unread"`
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
		Notifications []core.Notification `json:"notifications"`
		Unread        int                 `json:"unread"`
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
	ts, st := serveNotifications(t)

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
	if _, err := st.Emit(core.Notification{Kind: "cron", SourceID: "nightly", Title: "cron nightly finished", Verdict: "failed"}); err != nil {
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
