package main

// The desktop's update affordance. The shell never owns an updater: it calls the home's
// own /updates API, the same engine the webui's Settings pane drives (R1 - one updater,
// one exe). Everything here is HTTP + two dialog callbacks, so the semantics are asserted
// on every platform.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/core"
)

// trayUpdateLabel is the menu row. The row is one door: it checks when idle and offers
// the restart when a release is staged.
const trayUpdateLabel = "Updates..."

// trayUpdates is the tray's client of the home's update channel.
type trayUpdates struct {
	origin  string
	client  *http.Client
	info    func(title, body string)
	confirm func(title, body string, yes func())
}

func newTrayUpdates(origin string, info func(title, body string), confirm func(title, body string, yes func())) *trayUpdates {
	return &trayUpdates{origin: strings.TrimSuffix(origin, "/"), client: &http.Client{Timeout: 5 * time.Second}, info: info, confirm: confirm}
}

// act is the row's click: status first, then either the restart offer or a check.
func (t *trayUpdates) act() {
	st, err := t.status()
	if err != nil {
		t.info("omega updates", "The home's update channel did not answer: "+err.Error())
		return
	}
	switch st.State {
	case updateReady, updateFinalizing:
		staged := st.StagedVer
		if staged == "" {
			staged = "the staged release"
		}
		t.confirm("omega updates", fmt.Sprintf("omega %s is staged and pre-flight green. Restart now?", staged), func() {
			if _, err := t.post("/updates/finalize", map[string]any{"force": true}); err != nil {
				t.info("omega updates", "Restart failed: "+err.Error())
			}
		})
	case updateStaging, updatePreflight:
		t.info("omega updates", "omega "+st.Available+" is staging - watch Settings > Updates.")
	default:
		st, err = t.check()
		if err != nil {
			t.info("omega updates", err.Error())
			return
		}
		if st.Receipt != nil && st.Receipt.Outcome == "rollback" {
			t.info("omega updates", receiptLine(st.Receipt))
			return
		}
		if st.State == updateAvailable && st.Available != "" {
			t.info("omega updates", fmt.Sprintf("omega %s is available (%s channel). Stage it from Settings > Updates.", st.Available, st.Channel))
			return
		}
		t.info("omega updates", "omega is up to date ("+st.Current+").")
	}
}

// receiptLine renders a rollback receipt: version, why, what was restored.
func receiptLine(r *core.UpdateReceipt) string {
	restored := "nothing"
	if len(r.Restored) > 0 {
		restored = strings.Join(r.Restored, ", ")
	}
	return fmt.Sprintf("omega %s was rolled back: %s. Restored: %s. The previous version is running again.", r.Version, r.Why, restored)
}

// status reads GET /updates/status.
func (t *trayUpdates) status() (core.UpdateStatus, error) {
	resp, err := t.client.Get(t.origin + "/updates/status")
	if err != nil {
		return core.UpdateStatus{}, err
	}
	defer resp.Body.Close()
	return decodeUpdateStatus(resp)
}

// check asks the server to resolve the channel's release.
func (t *trayUpdates) check() (core.UpdateStatus, error) {
	resp, err := t.client.Post(t.origin+"/updates/check", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		return core.UpdateStatus{}, err
	}
	defer resp.Body.Close()
	return decodeUpdateStatus(resp)
}

// post sends one body to a write route and returns the status it answered.
func (t *trayUpdates) post(path string, body map[string]any) (core.UpdateStatus, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return core.UpdateStatus{}, err
	}
	resp, err := t.client.Post(t.origin+path, "application/json", bytes.NewReader(payload))
	if err != nil {
		return core.UpdateStatus{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		if data, rerr := io.ReadAll(io.LimitReader(resp.Body, 1<<16)); rerr == nil {
			if json.Unmarshal(data, &e) == nil && e.Error != "" {
				return core.UpdateStatus{}, fmt.Errorf("%s", e.Error)
			}
		}
		return core.UpdateStatus{}, fmt.Errorf("%s answered %s", path, resp.Status)
	}
	return decodeUpdateStatus(resp)
}

// decodeUpdateStatus reads a status body.
func decodeUpdateStatus(resp *http.Response) (core.UpdateStatus, error) {
	var st core.UpdateStatus
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&st); err != nil {
		return core.UpdateStatus{}, fmt.Errorf("decode the update status: %w", err)
	}
	return st, nil
}
