package cron

// The /cron command's name law. A nil error for a foreign name swallows the whole chain
// - every later command returned "[cron] no cron jobs configured".

import (
	"context"
	"strings"
	"testing"
)

func TestCronCommandAnswersOnlyItsOwnName(t *testing.T) {
	h := cronHandler(NewPlugin())

	res, err := h(context.Background(), "/cron", "")
	if err != nil {
		t.Fatalf("/cron: %v", err)
	}
	if !strings.HasPrefix(res.Text, "[cron]") {
		t.Fatalf("/cron = %q, want the cron listing", res.Text)
	}

	for _, foreign := range []string{"/models", "/tree", "/logs", "/verdict", "/board"} {
		res, err := h(context.Background(), foreign, "")
		if err == nil {
			t.Fatalf("%s answered %q with a nil error - the chain swallow", foreign, res.Text)
		}
		if res.Text != "" {
			t.Fatalf("%s returned text %q alongside the miss error, want a bare error", foreign, res.Text)
		}
	}
}
