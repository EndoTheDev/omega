package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// fakeInstanceSettings is a settings host that also carries the switcher's list: the
// route reaches it through the instanceLister assertion, never through core.Settings.
type fakeInstanceSettings struct {
	*fakeSettings
	list    []core.SavedInstance
	saveErr error
	saves   int
}

func (f *fakeInstanceSettings) Instances() ([]core.SavedInstance, error) { return f.list, nil }

func (f *fakeInstanceSettings) SaveInstances(list []core.SavedInstance) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saves++
	f.list = list
	return nil
}

func instancesGet(t *testing.T, url string) (int, []core.SavedInstance) {
	t.Helper()
	resp, err := http.Get(url + "/instances")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Instances []core.SavedInstance `json:"instances"`
	}
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode %q: %v", body, err)
		}
	}
	return resp.StatusCode, payload.Instances
}

func instancesPut(t *testing.T, url, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url+"/instances", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(out)
}

func TestInstancesGETEmpty(t *testing.T) {
	ts := settingsServer(&fakeInstanceSettings{fakeSettings: &fakeSettings{}})
	defer ts.Close()

	status, list := instancesGet(t, ts.URL)
	if status != http.StatusOK {
		t.Fatalf("status %d, want 200", status)
	}
	if list == nil {
		t.Fatal("empty list decoded as null, want []")
	}
	if len(list) != 0 {
		t.Fatalf("list = %+v, want empty", list)
	}
}

func TestInstancesPUTRoundTrip(t *testing.T) {
	fake := &fakeInstanceSettings{fakeSettings: &fakeSettings{}}
	ts := settingsServer(fake)
	defer ts.Close()

	status, body := instancesPut(t, ts.URL, `{"instances":[{"name":"  laptop ","url":"HTTP://127.0.0.1:8099/"}]}`)
	if status != http.StatusOK {
		t.Fatalf("status %d (%s), want 200", status, body)
	}
	if fake.saves != 1 {
		t.Fatalf("saves = %d, want the list written once", fake.saves)
	}

	// The PUT answers the NORMALIZED list, and the GET reads that same list back.
	var echoed struct {
		Instances []core.SavedInstance `json:"instances"`
	}
	if err := json.Unmarshal([]byte(body), &echoed); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	want := core.SavedInstance{Name: "laptop", URL: "http://127.0.0.1:8099"}
	if len(echoed.Instances) != 1 || echoed.Instances[0] != want {
		t.Fatalf("PUT echo = %+v, want [%+v]", echoed.Instances, want)
	}

	status, list := instancesGet(t, ts.URL)
	if status != http.StatusOK {
		t.Fatalf("GET status %d, want 200", status)
	}
	if len(list) != 1 || list[0] != want {
		t.Fatalf("GET = %+v, want [%+v]", list, want)
	}
}

func TestInstancesPUTValidation(t *testing.T) {
	// the cap table needs DISTINCT urls: one host per row
	rows := make([]string, 0, core.MaxSavedInstances+1)
	for i := 0; i <= core.MaxSavedInstances; i++ {
		rows = append(rows, `{"name":"n","url":"http://h`+strings.Repeat("a", i+1)+`.test"}`)
	}

	cases := []struct {
		name string
		body string
	}{
		{"invalid scheme", `{"instances":[{"name":"x","url":"ftp://127.0.0.1:8099"}]}`},
		{"no host", `{"instances":[{"name":"x","url":"http://"}]}`},
		{"empty name", `{"instances":[{"name":"   ","url":"http://127.0.0.1:8099"}]}`},
		{"name too long", `{"instances":[{"name":"` + strings.Repeat("n", core.MaxInstanceNameRunes+1) + `","url":"http://127.0.0.1:8099"}]}`},
		{"duplicate url", `{"instances":[{"name":"a","url":"http://127.0.0.1:8099"},{"name":"b","url":"http://127.0.0.1:8099/"}]}`},
		{"over the cap", `{"instances":[` + strings.Join(rows, ",") + `]}`},
		{"not json", `{"instances":`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeInstanceSettings{fakeSettings: &fakeSettings{}}
			ts := settingsServer(fake)
			defer ts.Close()
			status, body := instancesPut(t, ts.URL, tc.body)
			if status != http.StatusBadRequest {
				t.Fatalf("status %d (%s), want 400", status, body)
			}
			if fake.saves != 0 {
				t.Fatalf("saves = %d, a rejected list must not be written", fake.saves)
			}
		})
	}
}

func TestInstancesNotImplemented(t *testing.T) {
	// fakeSettings alone satisfies core.Settings but not instanceLister: the 501 floor.
	ts := settingsServer(&fakeSettings{})
	defer ts.Close()

	status, _ := instancesGet(t, ts.URL)
	if status != http.StatusNotImplemented {
		t.Fatalf("GET status %d, want 501", status)
	}
	status, body := instancesPut(t, ts.URL, `{"instances":[]}`)
	if status != http.StatusNotImplemented {
		t.Fatalf("PUT status %d (%s), want 501", status, body)
	}
}

func TestInstancesSaveErrorIsBadRequest(t *testing.T) {
	fake := &fakeInstanceSettings{fakeSettings: &fakeSettings{}, saveErr: errors.New("config: desktop.instances: row 0: name is required")}
	ts := settingsServer(fake)
	defer ts.Close()

	status, body := instancesPut(t, ts.URL, `{"instances":[{"name":"x","url":"http://127.0.0.1:8099"}]}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status %d, want 400 (the settings door's answer to a host refusal)", status)
	}
	if !strings.Contains(body, "name is required") {
		t.Fatalf("body %q must carry the host's own sentence", body)
	}
}

func TestInstancesMethodNotAllowed(t *testing.T) {
	ts := settingsServer(&fakeInstanceSettings{fakeSettings: &fakeSettings{}})
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/instances", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405", resp.StatusCode)
	}
}
