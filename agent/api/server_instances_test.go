package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

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

// --- the add-time probe -----------------------------------------------------------
//
// The four classes each answer their own failure and reason, and a passing probe proves
// three things: the address parses through the ONE validator, something answers at it, and
// what answers carries omega's health shape.

// probeServer is the channel with NO settings seam: the probe route answers on a host that
// carries no instance list at all.
func probeServer(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(NewServer(nil, nil, nil, nil, nil, nil).Handler())
	t.Cleanup(ts.Close)
	return ts
}

// probeAddress asks the route about one address and decodes the envelope.
func probeAddress(t *testing.T, base, address string) (instanceProbeAnswer, http.Header) {
	t.Helper()
	resp, err := http.Get(base + "/instances/probe?url=" + url.QueryEscape(address))
	if err != nil {
		t.Fatalf("probe %q: %v", address, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("probe %q: status %d, want 200 (a refusal is an answer)", address, resp.StatusCode)
	}
	var answer instanceProbeAnswer
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		t.Fatalf("probe %q: decode: %v", address, err)
	}
	return answer, resp.Header
}

// omegaHealth fakes a real instance's /health answer.
func omegaHealth(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestInstanceProbeOmegaPasses(t *testing.T) {
	body := `{"status":"ok","uptime_seconds":128912,"version":"v0.18.1","provider":{"reachable":true,"checked_at":1}}`
	target := omegaHealth(t, body)
	ts := probeServer(t)

	answer, header := probeAddress(t, ts.URL, target)
	if !answer.OK || answer.State != "omega" {
		t.Fatalf("answer = %+v, want ok/omega", answer)
	}
	if answer.URL != target {
		t.Fatalf("url = %q, want the normalized %q", answer.URL, target)
	}
	if !strings.Contains(string(answer.Facts), `"uptime_seconds":128912`) {
		t.Fatalf("facts = %s, want the health body's own facts", answer.Facts)
	}
	if answer.Failure != "" || answer.Reason != "" {
		t.Fatalf("a passing probe carries no failure: %+v", answer)
	}
	if cc := header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("cache-control %q, want no-store", cc)
	}
}

// The bare host:port shorthand the user types is the same address: the probe normalizes it
// with the ONE validator and reports the normalized form back.
func TestInstanceProbeTakesTheShorthand(t *testing.T) {
	target := omegaHealth(t, `{"status":"ok","uptime_seconds":1}`)
	short := strings.TrimPrefix(target, "http://")
	ts := probeServer(t)

	answer, _ := probeAddress(t, ts.URL, short)
	if !answer.OK || answer.URL != target {
		t.Fatalf("answer = %+v, want ok on the normalized %q", answer, target)
	}
}

func TestInstanceProbeNotOmega(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"a foreign json body", `{"hello":"world"}`},
		{"omega's own spa page", `<!doctype html><html><body>omega</body></html>`},
		{"the health shape with a bad status", `{"status":"degraded","uptime_seconds":5}`},
		{"the health shape without an uptime", `{"status":"ok"}`},
		{"an uptime that is not a number", `{"status":"ok","uptime_seconds":"5"}`},
		{"a json array", `[{"status":"ok","uptime_seconds":5}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := omegaHealth(t, tc.body)
			ts := probeServer(t)
			answer, _ := probeAddress(t, ts.URL, target)
			if answer.OK || answer.Failure != probeFailureNotOmega {
				t.Fatalf("answer = %+v, want not-omega", answer)
			}
			if !strings.Contains(answer.Reason, "not an omega instance") || !strings.Contains(answer.Reason, "200") {
				t.Fatalf("reason = %q, want it to name the answer and the code", answer.Reason)
			}
		})
	}
}

// A 403 carrying the guard's own sentence IS omega: its own class, naming the cure.
func TestInstanceProbeGuardRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, core.GuardHostRefusal, http.StatusForbidden)
	}))
	defer srv.Close()
	ts := probeServer(t)

	answer, _ := probeAddress(t, ts.URL, srv.URL)
	if answer.OK || answer.Failure != probeFailureGuard {
		t.Fatalf("answer = %+v, want guard", answer)
	}
	host := strings.TrimPrefix(srv.URL, "http://")
	for _, want := range []string{"Host guard refuses the name", host, "public_hosts"} {
		if !strings.Contains(answer.Reason, want) {
			t.Fatalf("reason = %q, want it to carry %q", answer.Reason, want)
		}
	}
	// a 403 that does NOT carry the sentence is just a foreign answer.
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer foreign.Close()
	answer, _ = probeAddress(t, ts.URL, foreign.URL)
	if answer.Failure != probeFailureNotOmega {
		t.Fatalf("answer = %+v, want not-omega for a 403 without the guard's sentence", answer)
	}
}

func TestInstanceProbeUnreachable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := "http://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	ts := probeServer(t)

	answer, _ := probeAddress(t, ts.URL, address)
	if answer.OK || answer.Failure != probeFailureUnreachable {
		t.Fatalf("answer = %+v, want unreachable", answer)
	}
	if !strings.HasPrefix(answer.Reason, "unreachable at that address") {
		t.Fatalf("reason = %q, want it to lead with the unreachable sentence", answer.Reason)
	}
	if answer.URL != address {
		t.Fatalf("url = %q, want the normalized %q", answer.URL, address)
	}
}

func TestInstanceProbeSyntax(t *testing.T) {
	cases := []struct {
		name    string
		address string
		want    string
	}{
		{"empty", "", "url is required"},
		{"no host", "http://", "url needs a host"},
		{"a foreign scheme", "ftp://127.0.0.1:8099", `url scheme must be http or https, got "ftp"`},
		{"spaces", "http://127.0.0.1:80 99", "url must not contain spaces"},
		{"too long", "http://" + strings.Repeat("a", 2100) + ".test", "at most"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := probeServer(t)
			answer, _ := probeAddress(t, ts.URL, tc.address)
			if answer.OK || answer.Failure != probeFailureSyntax {
				t.Fatalf("answer = %+v, want syntax", answer)
			}
			if !strings.Contains(answer.Reason, tc.want) {
				t.Fatalf("reason = %q, want the validator's own words %q", answer.Reason, tc.want)
			}
		})
	}
}

// A redirect is not followed: the address the operator typed is the one proved.
func TestInstanceProbeDoesNotFollowRedirects(t *testing.T) {
	var hops int
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hops++
		_, _ = io.WriteString(w, `{"status":"ok","uptime_seconds":5}`)
	}))
	defer elsewhere.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/health", http.StatusFound)
	}))
	defer redirector.Close()

	ts := probeServer(t)
	answer, _ := probeAddress(t, ts.URL, redirector.URL)
	if answer.OK || answer.Failure != probeFailureNotOmega {
		t.Fatalf("answer = %+v, want not-omega (the 302 itself is the answer)", answer)
	}
	if hops != 0 {
		t.Fatalf("the redirect target was fetched %d time(s); a hop is a different address", hops)
	}
	if !strings.Contains(answer.Reason, "302") {
		t.Fatalf("reason = %q, want the redirect status named", answer.Reason)
	}
}

// The body cap: an omega-shaped prefix with a huge string behind it truncates mid-string, so
// the answer is a refusal rather than a parse over an unbounded read.
func TestInstanceProbeBodyCap(t *testing.T) {
	huge := `{"status":"ok","uptime_seconds":5,"pad":"` + strings.Repeat("a", 100<<10) + `"}`
	target := omegaHealth(t, huge)
	ts := probeServer(t)

	answer, _ := probeAddress(t, ts.URL, target)
	if answer.OK || answer.Failure != probeFailureNotOmega {
		t.Fatalf("answer = %+v, want not-omega (the read stopped at the cap)", answer)
	}
}

// A handler that never answers hits the probe's own deadline.
func TestInstanceProbeTimeout(t *testing.T) {
	previous := instanceProbeTimeout
	instanceProbeTimeout = 150 * time.Millisecond
	t.Cleanup(func() { instanceProbeTimeout = previous })

	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(func() { close(release); slow.Close() })
	ts := probeServer(t)

	answer, _ := probeAddress(t, ts.URL, slow.URL)
	if answer.OK || answer.Failure != probeFailureUnreachable {
		t.Fatalf("answer = %+v, want unreachable (the deadline)", answer)
	}
	if !strings.HasPrefix(answer.Reason, "unreachable at that address") {
		t.Fatalf("reason = %q, want the unreachable sentence", answer.Reason)
	}
}

func TestInstanceProbeMethodNotAllowed(t *testing.T) {
	ts := probeServer(t)
	resp, err := http.Post(ts.URL+"/instances/probe?url=127.0.0.1:1", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405", resp.StatusCode)
	}
}

// A host carrying the list answers the probe too: the route reads no settings seam.
func TestInstanceProbeAnswersBesideTheList(t *testing.T) {
	target := omegaHealth(t, `{"status":"ok","uptime_seconds":5}`)
	fake := &fakeInstanceSettings{fakeSettings: &fakeSettings{}}
	ts := settingsServer(fake)
	defer ts.Close()

	answer, _ := probeAddress(t, ts.URL, target)
	if !answer.OK {
		t.Fatalf("answer = %+v, want ok", answer)
	}
	if fake.saves != 0 {
		t.Fatalf("saves = %d, a probe never writes", fake.saves)
	}
	if list, err := fake.Instances(); err != nil || len(list) != 0 {
		t.Fatalf("the probe touched the list: %+v, %v", list, err)
	}
}
