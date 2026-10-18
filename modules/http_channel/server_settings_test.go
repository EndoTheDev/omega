package http_channel

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/agent"
)

// fakeSettings records the values a PUT carried and replays them into
// the schema payload, like the real host does after a save.
type fakeSettings struct {
	schemaErr    error
	saveErr      error
	saved        map[string]any
	defaultValue any
}

func (f *fakeSettings) Schema() (any, error) {
	if f.schemaErr != nil {
		return nil, f.schemaErr
	}
	return map[string]any{
		"groups": []map[string]any{{
			"key":   "compaction",
			"label": "compaction",
			"knobs": []map[string]any{{
				"key":   "compaction.enabled",
				"label": "enabled",
				"type":  "toggle",
				"value": f.current("compaction.enabled"),
			}},
		}},
	}, nil
}

func (f *fakeSettings) Save(values map[string]any) (any, error) {
	if f.saveErr != nil {
		return nil, f.saveErr
	}
	if f.saved == nil {
		f.saved = map[string]any{}
	}
	for k, v := range values {
		f.saved[k] = v
	}
	return f.Schema()
}

func (f *fakeSettings) current(key string) any {
	if v, ok := f.saved[key]; ok {
		return v
	}
	return f.defaultValue
}

// settingsServer wires a server with a fake seam.
func settingsServer(s agent.Settings) *httptest.Server {
	return httptest.NewServer(NewServer(nil, nil, nil, s).Handler())
}

func TestSettingsGET(t *testing.T) {
	ts := settingsServer(&fakeSettings{defaultValue: true})
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("cache-control %q, want no-store (stale GET after save shows dead values)", cc)
	}
	var payload struct {
		Groups []struct {
			Key   string `json:"key"`
			Knobs []struct {
				Key   string `json:"key"`
				Type  string `json:"type"`
				Value any    `json:"value"`
			} `json:"knobs"`
		} `json:"groups"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Groups) != 1 || payload.Groups[0].Key != "compaction" {
		t.Fatalf("unexpected groups: %+v", payload)
	}
	if payload.Groups[0].Knobs[0].Value != true {
		t.Errorf("knob value %v, want true", payload.Groups[0].Knobs[0].Value)
	}
}

func TestSettingsPUTRoundTripsAndRefreshes(t *testing.T) {
	fake := &fakeSettings{defaultValue: true}
	ts := settingsServer(fake)
	defer ts.Close()
	// the webui sends only changed knobs -> the toggle flips to false
	body := `{"values": {"compaction.enabled": false}}`
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if fake.saved["compaction.enabled"] != false {
		t.Errorf("seam received %+v", fake.saved)
	}
	var payload struct {
		Groups []struct {
			Knobs []struct {
				Value any `json:"value"`
			} `json:"knobs"`
		} `json:"groups"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Groups[0].Knobs[0].Value != false {
		t.Errorf("refreshed payload value %v, want false", payload.Groups[0].Knobs[0].Value)
	}
}

func TestSettingsNilSeamGives501(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		req, _ := http.NewRequest(method, "/settings", nil)
		rec := httptest.NewRecorder()
		NewServer(nil, nil, nil, nil).Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotImplemented {
			t.Errorf("%s: code %d, want 501", method, rec.Code)
		}
	}
}

func TestSettingsPUTRejectsBadBodies(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"empty values", `{"values": {}}`, http.StatusBadRequest},
		{"missing values", `{}`, http.StatusBadRequest},
		{"garbage json", `not json`, http.StatusBadRequest},
	}
	ts := settingsServer(&fakeSettings{})
	defer ts.Close()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.NewRequest(http.MethodPut, ts.URL+"/settings", bytes.NewReader([]byte(tc.body)))
			if err != nil {
				t.Fatal(err)
			}
			resp.Header.Set("Content-Type", "application/json")
			got := httptest.NewRecorder()
			NewServer(nil, nil, nil, &fakeSettings{}).Handler().ServeHTTP(got, resp)
			if got.Code != tc.want {
				t.Errorf("code %d, want %d", got.Code, tc.want)
			}
		})
	}
}

func TestSettingsPUTSurfacesSeamErrors(t *testing.T) {
	fake := &fakeSettings{saveErr: errors.New("config: server.port must be > 0, got 0")}
	ts := settingsServer(fake)
	defer ts.Close()
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/settings",
		strings.NewReader(`{"values": {"server.port": 0}}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "server.port") {
		t.Errorf("error body %q omits the knob message", string(b))
	}
}

func TestSettingsMethodsRestricted(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		req, _ := http.NewRequest(method, "/settings", nil)
		rec := httptest.NewRecorder()
		NewServer(nil, nil, nil, &fakeSettings{}).Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: code %d, want 405", method, rec.Code)
		}
	}
}