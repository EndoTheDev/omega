package observability

// The /logs command's name law. The command seam's chain-on-miss wrapper falls through
// only on an error, so a nil error for a foreign name swallows every command registered
// below this capsule in the chain.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

func TestLogsCommandAnswersOnlyItsOwnName(t *testing.T) {
	t.Setenv("OMEGA_HOME", t.TempDir())
	logger, err := NewLogger(Config{Enabled: true, File: filepath.Join(t.TempDir(), "omega.log")})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	if fl, ok := logger.(*FileLogger); ok {
		// windows keeps the handle open.
		t.Cleanup(func() { _ = fl.Close() })
	}
	ctx := &core.Context{}
	ctx.ApplyOffer("logger", "observability", logger)
	h := logsHandler(ctx)

	res, err := h(context.Background(), "/logs", "")
	if err != nil {
		t.Fatalf("/logs: %v", err)
	}
	if !strings.HasPrefix(res.Text, "[logs: ") {
		t.Fatalf("/logs = %q, want the log tail header", res.Text)
	}

	for _, foreign := range []string{"/models", "/cron", "/verdict", "/board"} {
		res, err := h(context.Background(), foreign, "")
		if err == nil {
			t.Fatalf("%s answered %q with a nil error - the chain swallow", foreign, res.Text)
		}
		if strings.Contains(err.Error(), "log file") {
			t.Fatalf("%s was read as a /logs request (%v) - the name check must come first", foreign, err)
		}
	}
}
