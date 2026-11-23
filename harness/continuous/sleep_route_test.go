package continuous

// The sleep route's roundtrip: the roster row's Stop verb (POST /continuous/{name}/sleep)
// answers asleep, and an idle/unknown name is not an error - there is simply nothing to
// end.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSleepRouteRoundtrip(t *testing.T) {
	m := NewMachine(t.TempDir(), Default())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/continuous/scout/sleep", strings.NewReader(""))
	sleepHandler(m)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sleep = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"status":"asleep"`) {
		t.Fatalf("sleep answer = %s, want the asleep status", body)
	}
	if m.LiveWake("scout") {
		t.Fatal("scout still holds a live wake after sleep")
	}
}
