package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// fakeSettings records the values a PUT carried and replays them into
// the schema payload, like the real host does after a save.
type fakeSettings struct {
	schemaErr    error
	saveErr      error
	saved        map[string]any
	defaultValue any
	// the whole-document door (card t_a82d39c8)
	rawText    string
	rawErr     error
	savedRaw   string
	rawSaveErr error
	resets     int
	resetErr   error
}

func (f *fakeSettings) Schema() (*core.SettingsSchema, error) {
	if f.schemaErr != nil {
		return nil, f.schemaErr
	}
	return &core.SettingsSchema{
		Groups: []core.SettingsGroup{{
			Key:   "compaction",
			Label: "compaction",
			Knobs: []core.SettingsKnob{
				{
					Key:   "compaction.enabled",
					Label: "enabled",
					Type:  "toggle",
					Value: f.current("compaction.enabled"),
				},
			},
		}},
	}, nil
}

func (f *fakeSettings) Save(values map[string]any) (*core.SettingsSchema, error) {
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

// Raw replays the document the fake was seeded with.
func (f *fakeSettings) Raw() (string, error) {
	if f.rawErr != nil {
		return "", f.rawErr
	}
	return f.rawText, nil
}

// SaveRaw records the submitted document and refreshes the schema, the
// way the host's write path answers.
func (f *fakeSettings) SaveRaw(text string) (*core.SettingsSchema, error) {
	if f.rawSaveErr != nil {
		return nil, f.rawSaveErr
	}
	f.savedRaw = text
	return f.Schema()
}

// Reset records the call and answers the refreshed schema.
func (f *fakeSettings) Reset() (*core.SettingsSchema, error) {
	if f.resetErr != nil {
		return nil, f.resetErr
	}
	f.resets++
	return f.Schema()
}

func (f *fakeSettings) current(key string) any {
	if v, ok := f.saved[key]; ok {
		return v
	}
	return f.defaultValue
}

// settingsServer wires a server with a fake seam.
func settingsServer(s core.Settings) *httptest.Server {
	return httptest.NewServer(NewServer(nil, nil, nil, s, nil, nil).Handler())
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Groups) != 1 || payload.Groups[0].Key != "compaction" {
		t.Fatalf("unexpected groups: %+v", payload)
	}
	if len(payload.Groups[0].Knobs) != 1 {
		t.Errorf("knob count = %d, want the one usable knob: %+v", len(payload.Groups[0].Knobs), payload.Groups[0].Knobs)
	}
	if payload.Groups[0].Knobs[0].Value != true {
		t.Errorf("knob value %v, want true", payload.Groups[0].Knobs[0].Value)
	}
}

// TestSettingsGETOneKey pins the one-knob read (card t_013b4643): the
// ?key= form answers the knob object as it rides inside its group, so a
// script client reads a single value without the group walk.
func TestSettingsGETOneKey(t *testing.T) {
	ts := settingsServer(&fakeSettings{defaultValue: true})
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/settings?key=compaction.enabled")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("cache-control %q, want no-store", cc)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var knob struct {
		Key   string   `json:"key"`
		Label string   `json:"label"`
		Type  string   `json:"type"`
		Value any      `json:"value"`
		Min   *float64 `json:"min"`
	}
	if err := json.Unmarshal(body, &knob); err != nil {
		t.Fatalf("decode knob %q: %v", body, err)
	}
	if knob.Key != "compaction.enabled" || knob.Type != "toggle" || knob.Value != true {
		t.Errorf("knob = %+v, want the compaction.enabled toggle true", knob)
	}
	// the one-knob answer is the knob, not a group envelope
	if strings.Contains(string(body), `"groups"`) {
		t.Errorf("?key= answered the full payload: %s", body)
	}
}

// TestSettingsGETUnknownKey: a typo is a 404 naming the key - never a
// full payload that reads as a successful fetch.
func TestSettingsGETUnknownKey(t *testing.T) {
	ts := settingsServer(&fakeSettings{defaultValue: true})
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/settings?key=nope.nope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d, want 404", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "unknown setting: nope.nope") {
		t.Errorf("body %q omits the key", string(b))
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
		NewServer(nil, nil, nil, nil, nil, nil).Handler().ServeHTTP(rec, req)
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
			NewServer(nil, nil, nil, &fakeSettings{}, nil, nil).Handler().ServeHTTP(got, resp)
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
		NewServer(nil, nil, nil, &fakeSettings{}, nil, nil).Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: code %d, want 405", method, rec.Code)
		}
	}
}

// --- the whole-document door (card t_a82d39c8) -----------------------

// TestSettingsRawGET pins the export read: the host's document bytes,
// unmasked here (masking is the host's law, pinned in cmd/omega), with
// the yaml content type and no-store caching.
func TestSettingsRawGET(t *testing.T) {
	fake := &fakeSettings{rawText: "provider:\n  model_name: m\n  api_key: " + "****" + "\n"}
	ts := settingsServer(fake)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/settings/raw")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/yaml") {
		t.Errorf("content-type %q, want application/yaml", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("cache-control %q, want no-store", cc)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != fake.rawText {
		t.Errorf("body %q, want the host's document %q", body, fake.rawText)
	}
}

// TestSettingsRawPUTCarriesTheDocument: the submitted text reaches the
// seam verbatim and the refreshed payload comes back, so the pane can
// re-render the knobs from one round trip.
func TestSettingsRawPUTCarriesTheDocument(t *testing.T) {
	fake := &fakeSettings{defaultValue: true}
	ts := settingsServer(fake)
	defer ts.Close()
	doc := "provider:\n  model_name: other\n"
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/settings/raw", strings.NewReader(doc))
	req.Header.Set("Content-Type", "application/yaml")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if fake.savedRaw != doc {
		t.Errorf("seam received %q, want %q", fake.savedRaw, doc)
	}
	var payload struct {
		Groups []struct {
			Key string `json:"key"`
		} `json:"groups"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("refreshed payload: %v", err)
	}
	if len(payload.Groups) != 1 {
		t.Errorf("refreshed payload carried %d groups, want the schema", len(payload.Groups))
	}
}

// TestSettingsRawPUTRefusals: an empty body is the caller's mistake
// (400) and a document the host rejects is a 400 carrying the host's
// own sentence - never a 500 that reads as a server fault.
func TestSettingsRawPUTRefusals(t *testing.T) {
	fake := &fakeSettings{rawSaveErr: errors.New("config: provider.model_name is required")}
	ts := settingsServer(fake)
	defer ts.Close()
	resp, err := http.NewRequest(http.MethodPut, ts.URL+"/settings/raw", strings.NewReader("   \n"))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	NewServer(nil, nil, nil, fake, nil, nil).Handler().ServeHTTP(rec, resp)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty body: code %d, want 400", rec.Code)
	}

	bad, _ := http.NewRequest(http.MethodPut, ts.URL+"/settings/raw", strings.NewReader("not: [valid"))
	r, err := ts.Client().Do(bad)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("refused document: status %d, want 400", r.StatusCode)
	}
	b, _ := io.ReadAll(r.Body)
	if !strings.Contains(string(b), "provider.model_name") {
		t.Errorf("error body %q omits the host's sentence", string(b))
	}
}

// TestSettingsRawMethodsRestricted: the document door has exactly GET
// and PUT - POST/DELETE answer 405 rather than reaching the seam.
func TestSettingsRawMethodsRestricted(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		req, _ := http.NewRequest(method, "/settings/raw", nil)
		rec := httptest.NewRecorder()
		NewServer(nil, nil, nil, &fakeSettings{}, nil, nil).Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: code %d, want 405", method, rec.Code)
		}
	}
}

// TestSettingsResetPOST: the pane's reset action rides the host's own
// defaults - the request carries no body, so a client can never invent
// them.
func TestSettingsResetPOST(t *testing.T) {
	fake := &fakeSettings{defaultValue: true}
	ts := settingsServer(fake)
	defer ts.Close()
	resp, err := ts.Client().Post(ts.URL+"/settings/reset", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if fake.resets != 1 {
		t.Errorf("seam resets = %d, want 1", fake.resets)
	}
	var payload struct {
		Groups []struct {
			Knobs []struct {
				Value any `json:"value"`
			} `json:"knobs"`
		} `json:"groups"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("refreshed payload: %v", err)
	}
	if len(payload.Groups) == 0 || payload.Groups[0].Knobs[0].Value != true {
		t.Errorf("refreshed payload %+v, want the schema", payload)
	}
}

// TestSettingsResetRestricted: GET on the reset route is not a read.
func TestSettingsResetRestricted(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req, _ := http.NewRequest(method, "/settings/reset", nil)
		rec := httptest.NewRecorder()
		NewServer(nil, nil, nil, &fakeSettings{}, nil, nil).Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: code %d, want 405", method, rec.Code)
		}
	}
}

// TestSettingsRawAndResetNilSeam: a host with no settings seam answers
// 501 on the new doors too - the pane's "no config surface" branch.
func TestSettingsRawAndResetNilSeam(t *testing.T) {
	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/settings/raw"},
		{http.MethodPut, "/settings/raw"},
		{http.MethodPost, "/settings/reset"},
	}
	for _, tc := range cases {
		req, _ := http.NewRequest(tc.method, tc.path, strings.NewReader("provider: {}\n"))
		rec := httptest.NewRecorder()
		NewServer(nil, nil, nil, nil, nil, nil).Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotImplemented {
			t.Errorf("%s %s: code %d, want 501", tc.method, tc.path, rec.Code)
		}
	}
}
