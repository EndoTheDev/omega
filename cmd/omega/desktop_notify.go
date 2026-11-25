package main

// The tray's notification source: the local instance's SSE stream. The desktop is a
// client of the home's agent (docs/adr/0010), so notices ride the same wire the dock's
// tab uses - the tray never subscribes in process and never polls.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// toastSink is the native surface. Tests script it; the Windows shell wraps the Wails
// notifications service. A refused toast is dropped - the record is already stored.
type toastSink interface {
	SendToast(id, title, body string) error
}

// noticeEventsPath is the notifications capsule's stream route.
const noticeEventsPath = "/notifications/events"

// noticeBackoffMin/Max bound the reconnect ladder: one request per drop, never a poll.
const (
	noticeBackoffMin = 500 * time.Millisecond
	noticeBackoffMax = 30 * time.Second
)

// noticeConsumer streams the local instance's notices into native toasts. It reconnects
// on a dropped stream with a bounded backoff; the only requests it makes are the initial
// connect and one per drop.
type noticeConsumer struct {
	origin string
	sink   toastSink
	client *http.Client
}

func newNoticeConsumer(origin string, sink toastSink) *noticeConsumer {
	return &noticeConsumer{
		origin: strings.TrimRight(origin, "/"),
		sink:   sink,
		client: &http.Client{Transport: &http.Transport{
			DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		}},
	}
}

// Run consumes notices until ctx is cancelled.
func (c *noticeConsumer) Run(ctx context.Context) {
	backoff := noticeBackoffMin
	for {
		delivered := c.stream(ctx)
		if ctx.Err() != nil {
			return
		}
		if delivered {
			// A stream that carried notices was healthy: reconnect at once.
			backoff = noticeBackoffMin
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if !delivered && backoff < noticeBackoffMax {
			if backoff *= 2; backoff > noticeBackoffMax {
				backoff = noticeBackoffMax
			}
		}
	}
}

// stream is one connection's lifetime: true when at least one notice was toasted.
func (c *noticeConsumer) stream(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.origin+noticeEventsPath, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	return pumpNotices(ctx, resp.Body, c.sink)
}

// pumpNotices decodes SSE frames until the body ends or ctx is cancelled. A frame that
// does not decode is skipped: one bad notice never takes the stream down.
func pumpNotices(ctx context.Context, body io.Reader, sink toastSink) bool {
	rd := bufio.NewReader(body)
	delivered := false
	for {
		if ctx.Err() != nil {
			return delivered
		}
		event, data, err := readSSEFrame(rd)
		if err != nil {
			return delivered
		}
		if event != "notification" {
			continue
		}
		var n core.Notice
		if err := json.Unmarshal(data, &n); err != nil {
			continue
		}
		title, text := noticeToast(n)
		_ = sink.SendToast(noticeToastID(n), title, text)
		delivered = true
	}
}

// readSSEFrame returns one event's name and data payload. Blank lines inside a frame's
// field run and lines starting with ':' (the ping comment) carry nothing.
func readSSEFrame(rd *bufio.Reader) (string, []byte, error) {
	var event string
	var payload []string
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			return "", nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if len(payload) == 0 {
				continue
			}
			return event, []byte(strings.Join(payload, "\n")), nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			payload = append(payload, value)
		}
	}
}

// noticeToast maps one record to native toast text. The title rides verbatim: the record
// is metadata, and the shell invents nothing.
func noticeToast(n core.Notice) (string, string) {
	if n.Verdict == "failed" {
		return n.Title, fmt.Sprintf("failed: %s", n.Kind)
	}
	return n.Title, fmt.Sprintf("%s - %s", n.Kind, n.Verdict)
}

// noticeToastID is the native notification's identity, derived from the record's own id
// so two notices never collapse into one toast. The native surface refuses an empty one.
func noticeToastID(n core.Notice) string {
	return fmt.Sprintf("notice-%d", n.ID)
}
