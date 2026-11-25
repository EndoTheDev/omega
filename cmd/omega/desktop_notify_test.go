package main

// desktop_notify_test.go: the notice-to-toast contract. The sink is scripted everywhere;
// no native toast is ever raised by a test.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/harness/notifications"
)

// fakeToastSink records the toasts the consumer raised.
type fakeToastSink struct {
	mu     sync.Mutex
	toasts [][3]string
}

func (f *fakeToastSink) SendToast(id, title, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.toasts = append(f.toasts, [3]string{id, title, body})
	return nil
}

func (f *fakeToastSink) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.toasts)
}

func (f *fakeToastSink) at(i int) (string, string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.toasts[i][0], f.toasts[i][1], f.toasts[i][2]
}

func TestNoticeToastMapping(t *testing.T) {
	cases := []struct {
		name      string
		notice    core.Notice
		wantTitle string
		wantBody  string
	}{
		{
			name:      "ok task",
			notice:    core.Notice{Kind: "task", Title: "omega: tray card landed", Verdict: "ok"},
			wantTitle: "omega: tray card landed",
			wantBody:  "task - ok",
		},
		{
			name:      "ok cron",
			notice:    core.Notice{Kind: "cron", Title: "nightly", Verdict: "ok"},
			wantTitle: "nightly",
			wantBody:  "cron - ok",
		},
		{
			name:      "failed chat",
			notice:    core.Notice{Kind: "chat", Title: "chat 12", Verdict: "failed"},
			wantTitle: "chat 12",
			wantBody:  "failed: chat",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			title, body := noticeToast(tc.notice)
			if title != tc.wantTitle {
				t.Fatalf("title = %q, want %q (the record's title rides verbatim)", title, tc.wantTitle)
			}
			if body != tc.wantBody {
				t.Fatalf("body = %q, want %q", body, tc.wantBody)
			}
		})
	}
}

// openNoticeStore is the capsule's real store in a temp home: the source of truth the
// tray consumes.
func openNoticeStore(t *testing.T) *notifications.Store {
	t.Helper()
	store, err := notifications.Open(filepath.Join(t.TempDir(), "notifications.db"))
	if err != nil {
		t.Fatalf("open notifications store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// sseServer mirrors the capsule's /notifications/events wire over a real store, so the
// consumer parses the production frame shape.
func sseServer(t *testing.T, store *notifications.Store) (*httptest.Server, <-chan struct{}) {
	t.Helper()
	connected := make(chan struct{}, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != noticeEventsPath {
			http.NotFound(w, r)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		frames, cancel := store.Subscribe()
		defer cancel()
		connected <- struct{}{}
		for {
			select {
			case <-r.Context().Done():
				return
			case n, ok := <-frames:
				if !ok {
					return
				}
				data, err := json.Marshal(n)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "event: notification\ndata: %s\n\n", data)
				flusher.Flush()
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv, connected
}

// waitConnected blocks until a subscriber is live on the stream.
func waitConnected(t *testing.T, connected <-chan struct{}) {
	t.Helper()
	select {
	case <-connected:
	case <-time.After(testWaitTimeout):
		t.Fatal("the consumer never connected to the notification stream")
	}
}

// waitToasts blocks until the sink holds n toasts, then settles: a burst of extra toasts
// for one notice would surface just after.
func waitToasts(t *testing.T, sink *fakeToastSink, n int) {
	t.Helper()
	deadline := time.Now().Add(testWaitTimeout)
	for sink.count() < n {
		if time.Now().After(deadline) {
			t.Fatalf("toast count = %d, want %d", sink.count(), n)
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)
}

func TestNoticeConsumerToastsEmittedNotice(t *testing.T) {
	store := openNoticeStore(t)
	sink := &fakeToastSink{}
	srv, connected := sseServer(t, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go newNoticeConsumer(srv.URL, sink).Run(ctx)

	waitConnected(t, connected)
	emitted, err := store.Emit(core.Notice{Kind: "task", SourceID: "t_tray", Title: "omega: a card landed"})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	waitToasts(t, sink, 1)
	if got := sink.count(); got != 1 {
		t.Fatalf("toast count = %d, want exactly 1", got)
	}
	id, title, body := sink.at(0)
	if title != "omega: a card landed" || body != "task - ok" {
		t.Fatalf("toast = %q / %q, want the emitted record", title, body)
	}
	if id != noticeToastID(emitted) {
		t.Fatalf("toast id = %q, want the record's own id %q", id, noticeToastID(emitted))
	}
}

// TestNoticeConsumerReconnectsOnDrop: a dropped stream reconnects and the next notice
// still lands. One toast for one notice - the drop never duplicates or loses it.
func TestNoticeConsumerReconnectsOnDrop(t *testing.T) {
	store := openNoticeStore(t)
	sink := &fakeToastSink{}

	var mu sync.Mutex
	attempts := 0
	secondConnect := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		if n == 1 {
			// Drop the first stream before any frame: the consumer must come back.
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		frames, cancel := store.Subscribe()
		defer cancel()
		select {
		case secondConnect <- struct{}{}:
		default:
		}
		for {
			select {
			case <-r.Context().Done():
				return
			case notice, ok := <-frames:
				if !ok {
					return
				}
				data, err := json.Marshal(notice)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "event: notification\ndata: %s\n\n", data)
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go newNoticeConsumer(srv.URL, sink).Run(ctx)

	select {
	case <-secondConnect:
	case <-time.After(testWaitTimeout):
		t.Fatal("the consumer never reconnected after the stream dropped")
	}
	emittedNotice, err := store.Emit(core.Notice{Kind: "cron", SourceID: "nightly", Title: "nightly", Verdict: "failed"})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	waitToasts(t, sink, 1)
	if got := sink.count(); got != 1 {
		t.Fatalf("toast count = %d, want exactly 1 across the reconnect", got)
	}
	if id, title, body := sink.at(0); title != "nightly" || body != "failed: cron" || id != noticeToastID(emittedNotice) {
		t.Fatalf("toast = %q / %q / %q, want the failed cron record", id, title, body)
	}
}

// TestPumpNoticesSkipsNonNotices pins the frame filter: keep-alive comments, other event
// names and undecodable payloads never raise a toast.
func TestPumpNoticesSkipsNonNotices(t *testing.T) {
	sink := &fakeToastSink{}
	body := ": ping\n\n" +
		"event: other\ndata: {\"kind\":\"task\",\"title\":\"ignored\"}\n\n" +
		"event: notification\ndata: {not json}\n\n" +
		"event: notification\ndata: {\"kind\":\"cron\",\"verdict\":\"ok\",\"title\":\"nightly\"}\n\n"
	if !pumpNotices(context.Background(), strings.NewReader(body), sink) {
		t.Fatal("pumpNotices reported no delivery although a valid notification frame followed")
	}
	if got := sink.count(); got != 1 {
		t.Fatalf("toast count = %d, want 1", got)
	}
	if id, title, body := sink.at(0); title != "nightly" || body != "cron - ok" || id == "" {
		t.Fatalf("toast = %q / %q / %q, want the valid frame only", id, title, body)
	}
}
