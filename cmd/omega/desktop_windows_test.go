//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"

	"github.com/EndoTheDev/omega/core"
)

const instanceJobHelperEnv = "OMEGA_INSTANCE_JOB_HELPER"

// TestInstanceJobHelperProcess is the long-lived child TestInstanceJobKillOnClose puts
// in the job: it does nothing until the job handle closing terminates it. The parent
// re-executes this test binary with the env var set and -test.run pinned to it.
func TestInstanceJobHelperProcess(t *testing.T) {
	if os.Getenv(instanceJobHelperEnv) != "1" {
		t.Skip("helper process for TestInstanceJobKillOnClose")
	}
	time.Sleep(2 * testWaitTimeout)
}

// TestInstanceJobKillOnClose is the real mechanism end to end: a child assigned to the
// job dies when the job's last handle closes, which is what a desktop process exit does.
func TestInstanceJobKillOnClose(t *testing.T) {
	job, err := newInstanceJob()
	if err != nil {
		t.Fatalf("newInstanceJob: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(exe, "-test.run", "^TestInstanceJobHelperProcess$")
	cmd.Env = append(os.Environ(), instanceJobHelperEnv+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the helper: %v", err)
	}
	exited := make(chan struct{})
	t.Cleanup(func() {
		// Bounded: a failure before the waiter starts leaves the channel unclosed.
		_ = cmd.Process.Kill()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
		}
	})
	if err := job.assign(cmd.Process); err != nil {
		t.Fatalf("assign the helper to the job: %v", err)
	}
	go func() {
		_, _ = cmd.Process.Wait()
		close(exited)
	}()

	job.release()

	// The poll catches a hang; it does not measure the kill's latency.
	deadline := time.Now().Add(testWaitTimeout)
	for {
		select {
		case <-exited:
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("the helper outlived the closed job handle by %s", testWaitTimeout)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestDesktopAttachAnswerIsStable pins the memo behind the routing: main attaches
// before its first write and bareLaunchWantsDesktop asks again, while a second
// AttachConsole on an attached process fails. One answer per process keeps a terminal
// launch out of the desktop branch.
func TestDesktopAttachAnswerIsStable(t *testing.T) {
	first := attachCallerConsole()
	t.Logf("attach answer in the test process = %v", first)
	for i := 2; i <= 4; i++ {
		if got := attachCallerConsole(); got != first {
			t.Fatalf("attach answer #%d = %v, want the first answer %v", i, got, first)
		}
	}
	if got := bareLaunchWantsDesktop(); got == first {
		t.Fatalf("bareLaunchWantsDesktop = %v after attach = %v, want its negation", got, first)
	}
}

// TestDesktopWebView2RuntimeProbe is the host receipt for the gate's input: the same
// client-state keys the WebView2 loader reads, answered by this machine.
func TestDesktopWebView2RuntimeProbe(t *testing.T) {
	version, err := webView2RuntimeVersion()
	if err != nil {
		t.Fatalf("webView2RuntimeVersion on this host: %v", err)
	}
	t.Logf("webView2RuntimeVersion on this host = %q", version)
	if version == "" {
		t.Skip("no WebView2 runtime on this host; the missing path is covered by the seam test")
	}
	if version[0] < '0' || version[0] > '9' {
		t.Fatalf("probe answered %q, want the installed runtime's version", version)
	}
}

// TestDesktopLaunchStopsWhenWebView2IsMissing is the defect's end-to-end proof without a
// broken machine: the probe answers "nothing installed", and the launch must log, draw
// exactly one box, start no agent, and return the error main turns into exit 1.
func TestDesktopLaunchStopsWhenWebView2IsMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMEGA_HOME", home)
	cfgPath := desktopTestConfig(t, freeDesktopPort(t))
	cfg, _, err := desktopConfig(cfgPath)
	if err != nil {
		t.Fatalf("desktopConfig: %v", err)
	}

	oldProbe, oldBox, oldSpawn := desktopWebView2Version, desktopBootBox, spawnInstanceServe
	t.Cleanup(func() {
		desktopWebView2Version, desktopBootBox, spawnInstanceServe = oldProbe, oldBox, oldSpawn
	})
	desktopWebView2Version = func() (string, error) { return "", nil }
	var boxes []string
	desktopBootBox = func(title, msg string) { boxes = append(boxes, title+"\n"+msg) }
	spawnInstanceServe = func(string) (instanceProcess, instanceOwner, error) {
		t.Error("a WebView2-less launch started the agent: the gate must stop before ensureInstance")
		return nil, nil, errors.New("unreachable")
	}

	launchErr := desktopLaunch(cfg, cfgPath)
	if launchErr == nil {
		t.Fatal("desktopLaunch returned nil with no WebView2 runtime")
	}
	if !strings.Contains(launchErr.Error(), "WebView2") {
		t.Fatalf("launch error = %q, want it to name WebView2", launchErr)
	}
	if len(boxes) != 1 {
		t.Fatalf("boxes = %d, want exactly 1", len(boxes))
	}
	if !strings.HasPrefix(boxes[0], "omega\n") {
		t.Fatalf("box = %q, want the omega caption first", boxes[0])
	}
	for _, want := range []string{"omega could not open its window.", "WebView2", "go.microsoft.com/fwlink", cfg.Logging.File} {
		if !strings.Contains(boxes[0], want) {
			t.Fatalf("box text is missing %q:\n%s", want, boxes[0])
		}
	}

	body, readErr := os.ReadFile(cfg.Logging.File)
	if readErr != nil {
		t.Fatalf("read the desktop log: %v", readErr)
	}
	for _, want := range []string{"desktop: window boot failed:", "no WebView2 runtime is installed", "the user saw the fallback dialog"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("log is missing %q:\n%s", want, body)
		}
	}
}

// TestDesktopWindowOptionsIsAFramelessHostOwnedTitleBar pins the window the merged
// title bar and the dock's tab drag both depend on: without Frameless the system
// caption comes back above the header; with WebView2CompositionHosting the window's
// input never reaches Chromium's drag controller (a dock tab starts a drag and is
// cancelled at once), so the caption is the header's app-region surface answered by
// WebView2's own non-client support instead; turning either decoration or resize off
// costs the Win11 shadow and the edge resize borders.
func TestDesktopWindowOptionsIsAFramelessHostOwnedTitleBar(t *testing.T) {
	const origin = "http://127.0.0.1:18123"
	opts := desktopWindowOptions(origin)

	if !opts.Frameless {
		t.Error("Frameless is off: the system caption still stacks above omega's header")
	}
	if opts.Windows.WebView2CompositionHosting {
		t.Error("WebView2CompositionHosting is on: a composition-hosted controller has no OLE drag source, so a page drag dies at dragstart")
	}
	if !opts.Windows.NonClientRegionSupport {
		t.Error("NonClientRegionSupport is off: windowed hosting then has no native route for the header's app-region drag surface")
	}
	if opts.Windows.DisableFramelessWindowDecorations {
		t.Error("DisableFramelessWindowDecorations is on: the Win11 shadow and rounded corners go with it")
	}
	if opts.DisableResize {
		t.Error("DisableResize is on: a frameless window then has no resize borders")
	}
	if opts.MinWidth != 340 {
		t.Errorf("MinWidth = %d, want 340: the window can be dragged below the 340px narrow floor", opts.MinWidth)
	}
	if opts.MinHeight != 480 {
		t.Errorf("MinHeight = %d, want 480: header, one message row, composer and statusbar need that height", opts.MinHeight)
	}
	if !opts.AllowSimpleEventEmit {
		t.Error("AllowSimpleEventEmit is off: the caption cells' presses never reach the host")
	}
	for _, event := range desktopWindowEvents {
		if !strings.HasPrefix(event, "omega:window:") {
			t.Errorf("window event %q is not in the omega:window: namespace", event)
		}
	}
	if len(desktopWindowEvents) != 3 {
		t.Errorf("desktopWindowEvents = %v, want the three caption cells", desktopWindowEvents)
	}
	if opts.Title != "omega" {
		t.Errorf("Title = %q, want omega", opts.Title)
	}
	if opts.URL != origin {
		t.Errorf("URL = %q, want %q", opts.URL, origin)
	}
}

// hostProbe records what the desktop host asked the window to do. The seams are set by
// newTestDesktopHost, so every assertion below is a value, not a Wails call.
type hostProbe struct {
	events []hostEvent
	frames []bool
	navs   []string
}

type hostEvent struct {
	name    string
	payload any
}

func newTestDesktopHost(origin string, store desktopInstanceStore) (*desktopHost, *hostProbe) {
	probe := &hostProbe{}
	host := newDesktopHost(origin, store)
	host.emit = func(name string, payload any) {
		probe.events = append(probe.events, hostEvent{name: name, payload: payload})
	}
	host.setFrameless = func(frameless bool) { probe.frames = append(probe.frames, frameless) }
	host.navigate = func(url string) { probe.navs = append(probe.navs, url) }
	return host, probe
}

// TestDecodeDesktopRequest pins the raw message grammar: three members, the chrome
// greeting, and a refusal for everything else.
func TestDecodeDesktopRequest(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr string
	}{
		{name: "get", raw: `{"omega":"instances:get"}`, want: desktopMessageGetInstances},
		{name: "save", raw: `{"omega":"instances:save","instances":[{"name":"pi","url":"http://192.0.2.10:8098"}]}`, want: desktopMessageSaveInstances},
		{name: "save empty", raw: `{"omega":"instances:save","instances":[]}`, want: desktopMessageSaveInstances},
		{name: "switch", raw: `{"omega":"instances:switch","url":"http://127.0.0.1:18101"}`, want: desktopMessageSwitchInstance},
		{name: "chrome ready", raw: `{"omega":"chrome:ready"}`, want: desktopMessageChromeReady},
		{name: "window get", raw: `{"omega":"window:get"}`, want: desktopMessageGetWindow},
		{name: "window set", raw: `{"omega":"window:set","key":"close_to_tray","value":false}`, want: desktopMessageSetWindow},
		{name: "window set without a key", raw: `{"omega":"window:set","value":true}`, wantErr: "carries no key"},
		{name: "window set without a value", raw: `{"omega":"window:set","key":"close_to_tray"}`, wantErr: "carries no value"},
		{name: "unknown member", raw: `{"omega":"instances:delete"}`, wantErr: "unknown desktop message"},
		{name: "malformed json", raw: `{"omega":`, wantErr: "not a desktop message"},
		{name: "not an object", raw: `"instances:get"`, wantErr: "not a desktop message"},
		{name: "no omega member", raw: `{"instances":[]}`, wantErr: "no omega member"},
		{name: "switch without a url", raw: `{"omega":"instances:switch"}`, wantErr: "carries no url"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeDesktopRequest(tc.raw)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("decodeDesktopRequest(%s) error = %v, want it to name %q", tc.raw, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeDesktopRequest(%s): %v", tc.raw, err)
			}
			if got.kind != tc.want {
				t.Fatalf("kind = %q, want %q", got.kind, tc.want)
			}
		})
	}

	saved, err := decodeDesktopRequest(`{"omega":"instances:save","instances":[{"name":"pi","url":"http://192.0.2.10:8098"}]}`)
	if err != nil || len(saved.instances) != 1 || saved.instances[0].Name != "pi" {
		t.Fatalf("the save body did not carry its rows: %+v, %v", saved, err)
	}

	set, err := decodeDesktopRequest(`{"omega":"window:set","key":"close_to_tray","value":false}`)
	if err != nil || set.key != "close_to_tray" || set.value {
		t.Fatalf("the window body did not carry its row and state: %+v, %v", set, err)
	}
}

// TestDesktopHostServesTheAppLocalList is the whole round trip without a window: the page
// asks, the host answers with the app's rows; a save is validated and written; a refusal
// answers with the host's words and the list unchanged.
func TestDesktopHostServesTheAppLocalList(t *testing.T) {
	const origin = "http://127.0.0.1:18101"
	store := &fakeInstanceStore{list: []core.SavedInstance{{Name: "main", URL: origin}}}
	host, probe := newTestDesktopHost(origin, store)
	page := &application.OriginInfo{Origin: origin + "/", TopOrigin: origin + "/"}

	if err := host.Handle(page, `{"omega":"instances:get"}`); err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(probe.events) != 1 || probe.events[0].name != desktopInstancesEvent {
		t.Fatalf("events = %+v, want one %q", probe.events, desktopInstancesEvent)
	}
	first, ok := probe.events[0].payload.(desktopInstancesReply)
	if !ok {
		t.Fatalf("payload type = %T, want desktopInstancesReply", probe.events[0].payload)
	}
	if first.Error != "" || len(first.Instances) != 1 || first.Instances[0].Name != "main" {
		t.Fatalf("get answered %+v, want the app's one row and no error", first)
	}

	// a save lands through the one writer, normalized on the way in
	if err := host.Handle(page, `{"omega":"instances:save","instances":[{"name":" pi ","url":"HTTP://192.0.2.10:8098/"}]}`); err != nil {
		t.Fatalf("save: %v", err)
	}
	if len(store.saved) != 1 || store.saved[0] != (core.SavedInstance{Name: "pi", URL: "http://192.0.2.10:8098"}) {
		t.Fatalf("saved = %+v, want the normalized row", store.saved)
	}
	if last := probe.events[len(probe.events)-1].payload.(desktopInstancesReply); last.Error != "" {
		t.Fatalf("a clean save refused: %q", last.Error)
	}

	// a refused save answers the validator's words and leaves the list alone
	before := store.saved
	if err := host.Handle(page, `{"omega":"instances:save","instances":[{"name":"","url":"http://192.0.2.10:8099"}]}`); err != nil {
		t.Fatalf("a refused save is the page's answer, not a dropped message: %v", err)
	}
	refusal := probe.events[len(probe.events)-1].payload.(desktopInstancesReply)
	if !strings.Contains(refusal.Error, "name is required") {
		t.Fatalf("refusal = %q, want the validator's words", refusal.Error)
	}
	if len(refusal.Instances) != 1 || refusal.Instances[0] != before[0] {
		t.Fatalf("a refused save answered %+v, want the unchanged list", refusal.Instances)
	}
	if len(store.saved) != len(before) {
		t.Fatal("a refused save still reached the writer")
	}

	// the writer's own refusal is carried the same way
	store.saveErr = errors.New("config.yaml is read-only")
	if err := host.Handle(page, `{"omega":"instances:save","instances":[]}`); err != nil {
		t.Fatalf("writer refusal: %v", err)
	}
	if got := probe.events[len(probe.events)-1].payload.(desktopInstancesReply); !strings.Contains(got.Error, "read-only") {
		t.Fatalf("writer refusal answered %q, want the writer's words", got.Error)
	}
}

// TestDesktopHostRefusesEveryOtherPage is the door's trust gate: the window's own top
// document may ask; a foreign page and a child frame may not.
func TestDesktopHostRefusesEveryOtherPage(t *testing.T) {
	const origin = "http://127.0.0.1:18101"
	const foreign = "http://192.0.2.10:8098"
	host, probe := newTestDesktopHost(origin, &fakeInstanceStore{})
	host.setFrameless = func(bool) { t.Error("a foreign frame changed the frame state") }

	cases := []struct {
		name string
		info *application.OriginInfo
	}{
		{"nil record", nil},
		{"foreign page", &application.OriginInfo{Origin: foreign + "/", TopOrigin: foreign + "/"}},
		{"child frame on a foreign page", &application.OriginInfo{Origin: foreign + "/frame", TopOrigin: foreign + "/"}},
		{"child frame on this page", &application.OriginInfo{Origin: foreign + "/frame", TopOrigin: origin + "/"}},
		{"no top origin", &application.OriginInfo{Origin: origin + "/"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := host.Handle(tc.info, `{"omega":"instances:get"}`); err == nil {
				t.Fatal("a message outside the window's own top document was accepted")
			}
		})
	}
	if len(probe.events) != 0 {
		t.Fatalf("refused messages emitted %+v", probe.events)
	}

	// a main-frame record on the platform that reports the bit is honoured
	if err := host.Handle(&application.OriginInfo{Origin: origin + "/", TopOrigin: origin + "/", IsMainFrame: true}, `{"omega":"instances:get"}`); err != nil {
		t.Fatalf("the window's own main frame was refused: %v", err)
	}
	if len(probe.events) != 1 {
		t.Fatalf("the accepted message answered %+v, want one event", probe.events)
	}
}

// TestDesktopHostSwitchResolvesTheShellsRows: a page switch names a ROW, never a
// destination. The host resolves the key against its own set - an exact saved row, then
// an origin match, then home - and the window moves to the member's own spelling. A miss
// navigates nothing and answers the refusal on the event.
func TestDesktopHostSwitchResolvesTheShellsRows(t *testing.T) {
	const origin = "http://127.0.0.1:18101"
	const saved = "http://192.0.2.10:8098"
	const prefixed = "http://192.0.2.10:8100/omega"
	store := &fakeInstanceStore{list: []core.SavedInstance{
		{Name: "pi", URL: saved},
		{Name: "work", URL: prefixed},
	}}
	host, probe := newTestDesktopHost(origin, store)
	page := func(at string) *application.OriginInfo {
		return &application.OriginInfo{Origin: at + "/", TopOrigin: at + "/"}
	}
	last := func() desktopInstancesReply {
		if len(probe.events) == 0 {
			t.Fatal("the host answered nothing")
		}
		return probe.events[len(probe.events)-1].payload.(desktopInstancesReply)
	}

	// an exact saved row: the key's spelling is normalized, the window gets the row's own
	if err := host.Handle(page(origin), `{"omega":"instances:switch","url":"HTTP://192.0.2.10:8098/"}`); err != nil {
		t.Fatalf("switch: %v", err)
	}
	if len(probe.navs) != 1 || probe.navs[0] != saved {
		t.Fatalf("navigations = %q, want the saved row %q", probe.navs, saved)
	}
	if got := host.ActiveOrigin(); got != saved {
		t.Fatalf("active origin = %q, want %q", got, saved)
	}

	// an origin-key match resolves a prefixed row, and the window gets the stored path
	if err := host.Handle(page(saved), `{"omega":"instances:switch","url":"http://192.0.2.10:8100"}`); err != nil {
		t.Fatalf("origin-key switch: %v", err)
	}
	if len(probe.navs) != 2 || probe.navs[1] != prefixed {
		t.Fatalf("navigations = %q, want the stored row %q", probe.navs, prefixed)
	}

	// home is a member: the page names it with the address the answer carried
	if err := host.Handle(page(prefixed), `{"omega":"instances:switch","url":"`+origin+`"}`); err != nil {
		t.Fatalf("switch home: %v", err)
	}
	if len(probe.navs) != 3 || probe.navs[2] != origin {
		t.Fatalf("navigations = %q, want home %q", probe.navs, origin)
	}
	if got := host.HomeOrigin(); got != origin {
		t.Fatalf("a switch rewrote home to %q", got)
	}

	// a stranger address is a miss: the refusal answers, the window does not move
	before := len(probe.navs)
	if err := host.Handle(page(origin), `{"omega":"instances:switch","url":"http://198.51.100.7:1234"}`); err != nil {
		t.Fatalf("a miss is the page's answer, not a dropped message: %v", err)
	}
	if len(probe.navs) != before {
		t.Fatalf("a refused switch navigated the window: %q", probe.navs)
	}
	if got := host.ActiveOrigin(); got != origin {
		t.Fatalf("a refused switch moved the active origin to %q", got)
	}
	refusal := last()
	if !strings.Contains(refusal.Error, "not one of this desktop's connections") {
		t.Fatalf("a miss answered %q, want the refusal", refusal.Error)
	}
	if len(refusal.Instances) != len(store.list) {
		t.Fatalf("a refused switch answered %d rows, want the unchanged list of %d", len(refusal.Instances), len(store.list))
	}

	// a non-http key never reaches the list
	if err := host.Handle(page(origin), `{"omega":"instances:switch","url":"javascript:alert(1)"}`); err != nil {
		t.Fatalf("a bad scheme is the page's answer, not a dropped message: %v", err)
	}
	if len(probe.navs) != before {
		t.Fatal("a refused scheme still navigated the window")
	}
	if got := last(); !strings.Contains(got.Error, "http") {
		t.Fatalf("a refused scheme answered %q, want the validator's words", got.Error)
	}

	// a page the window has left is refused, as before: the door's origin gate stands
	if err := host.Handle(page(prefixed), `{"omega":"instances:switch","url":"`+saved+`"}`); err == nil {
		t.Fatal("the origin the window left could still ask")
	}
	if len(probe.navs) != before {
		t.Fatal("a refused page still navigated the window")
	}

	// an unreadable list costs the list's members, never home
	store.readErr = errors.New("config.yaml is unreadable")
	if err := host.Handle(page(origin), `{"omega":"instances:switch","url":"`+origin+`"}`); err != nil {
		t.Fatalf("switch home with an unreadable list: %v", err)
	}
	if len(probe.navs) != before+1 || probe.navs[before] != origin {
		t.Fatalf("navigations = %q, want home still resolvable", probe.navs)
	}
}

// TestDesktopHostHomeIsShellState: home is the launch origin - answered with every list,
// outside the message door, and untouched by a switch. A page that names a home of its own
// either gets its body refused or has the field dropped; the answer keeps the launch
// origin either way.
func TestDesktopHostHomeIsShellState(t *testing.T) {
	const origin = "http://127.0.0.1:18101"
	const other = "http://192.0.2.10:8098"
	store := &fakeInstanceStore{list: []core.SavedInstance{{Name: "pi", URL: other}}}
	host, probe := newTestDesktopHost(origin, store)
	page := func(at string) *application.OriginInfo {
		return &application.OriginInfo{Origin: at + "/", TopOrigin: at + "/"}
	}
	last := func() desktopInstancesReply {
		if len(probe.events) == 0 {
			t.Fatal("the host answered nothing")
		}
		return probe.events[len(probe.events)-1].payload.(desktopInstancesReply)
	}

	if got := host.HomeOrigin(); got != origin {
		t.Fatalf("HomeOrigin = %q, want the launch origin", got)
	}
	if err := host.Handle(page(origin), `{"omega":"instances:get"}`); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := last().Home; got != origin {
		t.Fatalf("the answer carries home %q, want the launch origin", got)
	}

	// a page-named home is not a body the door knows
	if err := host.Handle(page(origin), `{"omega":"instances:home","home":"http://198.51.100.7:1234"}`); err == nil {
		t.Fatal("a page message naming home was accepted")
	}
	// and the same field smuggled onto a known body is dropped, not read
	if err := host.Handle(page(origin), `{"omega":"instances:get","home":"http://198.51.100.7:1234"}`); err != nil {
		t.Fatalf("get with an extra member: %v", err)
	}
	if got := last().Home; got != origin {
		t.Fatalf("a page-supplied home reached the answer: %q", got)
	}

	// a switch shows a member of the shell's own set and moves the active origin only
	if err := host.Handle(page(origin), `{"omega":"instances:switch","url":"http://192.0.2.10:8098"}`); err != nil {
		t.Fatalf("switch: %v", err)
	}
	if got := host.HomeOrigin(); got != origin {
		t.Fatalf("a switch rewrote home to %q", got)
	}
	if got := host.ActiveOrigin(); got != other {
		t.Fatalf("active origin = %q, want the switch target", got)
	}

	// the remote page's own answer still carries the desktop's home
	if err := host.Handle(page(other), `{"omega":"instances:get"}`); err != nil {
		t.Fatalf("the page the window shows was refused: %v", err)
	}
	if got := last().Home; got != origin {
		t.Fatalf("the answer on another origin carries home %q, want the launch origin", got)
	}

	// a save answers the same home: home is never a row a write can move
	if err := host.Handle(page(other), `{"omega":"instances:save","instances":[{"name":"pi","url":"http://192.0.2.10:8098"}]}`); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := last().Home; got != origin {
		t.Fatalf("a save's answer carries home %q, want the launch origin", got)
	}
}

// TestDesktopFrameOf is the frame policy table: the page's own announcement wins, and the
// app's own origin never un-frames.
func TestDesktopFrameOf(t *testing.T) {
	const app = "http://127.0.0.1:18101"
	const foreign = "http://192.0.2.10:8098"
	cases := []struct {
		name        string
		chromeReady bool
		active      string
		want        desktopFrameDecision
	}{
		{"app origin, armed", true, app, desktopFrameKeep},
		{"app origin, silent", false, app, desktopFrameKeep},
		{"app origin with a path, silent", false, app + "/index.html", desktopFrameKeep},
		{"foreign origin, armed", true, foreign, desktopFrameKeep},
		{"foreign origin, silent", false, foreign, desktopFrameRestore},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := desktopFrameOf(tc.chromeReady, tc.active, app); got != tc.want {
				t.Fatalf("desktopFrameOf(%v, %q) = %v, want %v", tc.chromeReady, tc.active, got, tc.want)
			}
		})
	}
}

// TestDesktopChromeBound: a page that announces its chrome keeps the frameless frame, a
// silent page on another origin gets the OS caption, and a late announcement puts omega's
// own bar back.
func TestDesktopChromeBound(t *testing.T) {
	const app = "http://127.0.0.1:18101"
	const foreign = "http://192.0.2.10:8098"

	if desktopChromeReadyBoundWindow != 5*time.Second {
		t.Fatalf("the chrome bound is %s, want the 5s window", desktopChromeReadyBoundWindow)
	}

	// a fake clock: the bound records what it was armed with and never runs on its own
	var armed time.Duration
	var fired []func()
	old := desktopChromeBoundWindow
	t.Cleanup(func() { desktopChromeBoundWindow = old })
	desktopChromeBoundWindow = func(d time.Duration, fn func()) *time.Timer {
		armed = d
		fired = append(fired, fn)
		return time.NewTimer(time.Hour)
	}

	host, probe := newTestDesktopHost(app, &fakeInstanceStore{})
	host.beginNavigation()
	if armed != 5*time.Second || len(fired) != 1 {
		t.Fatalf("beginNavigation armed %s / %d bounds, want one 5s bound", armed, len(fired))
	}
	if err := host.Handle(&application.OriginInfo{Origin: app + "/", TopOrigin: app + "/"}, `{"omega":"chrome:ready"}`); err != nil {
		t.Fatalf("chrome:ready: %v", err)
	}
	if len(probe.frames) != 1 || probe.frames[0] != true {
		t.Fatalf("the announcement asked for frames %v, want one frameless request", probe.frames)
	}
	fired[0]()
	if len(probe.frames) != 1 {
		t.Fatalf("an announced page was un-framed anyway: %v", probe.frames)
	}

	// the app's own origin never un-frames, announced or not
	host, probe = newTestDesktopHost(app, &fakeInstanceStore{})
	host.beginNavigation()
	fired[1]()
	if len(probe.frames) != 0 {
		t.Fatalf("a silent cold start un-framed the app's own page: %v", probe.frames)
	}

	// a silent foreign page gets the OS caption
	host, probe = newTestDesktopHost(app, &fakeInstanceStore{})
	host.SwitchTo(foreign)
	host.beginNavigation()
	fired[2]()
	if len(probe.frames) != 1 || probe.frames[0] != false {
		t.Fatalf("a silent foreign page asked for frames %v, want the caption back", probe.frames)
	}

	// a late announcement after the caption returned puts omega's bar back
	if err := host.Handle(&application.OriginInfo{Origin: foreign + "/", TopOrigin: foreign + "/"}, `{"omega":"chrome:ready"}`); err != nil {
		t.Fatalf("late chrome:ready: %v", err)
	}
	if len(probe.frames) != 2 || probe.frames[1] != true {
		t.Fatalf("the late announcement asked for frames %v, want frameless again", probe.frames)
	}
}

// fakeWindowStore is the app's own window options behind the door: three booleans and the
// writes that landed.
type fakeWindowStore struct {
	options DesktopConfig
	saved   map[string]bool
	err     error
}

func (f *fakeWindowStore) WindowOptions() (DesktopConfig, error) {
	if f.err != nil {
		return DesktopConfig{}, f.err
	}
	return f.options, nil
}

func (f *fakeWindowStore) SaveWindowOption(key string, on bool) error {
	if f.err != nil {
		return f.err
	}
	if f.saved == nil {
		f.saved = make(map[string]bool, 3)
	}
	f.saved[key] = on
	switch key {
	case desktopOptionStartMinimized:
		f.options.StartMinimized = on
	case desktopOptionMinimizeToTray:
		f.options.MinimizeToTray = on
	case desktopOptionCloseToTray:
		f.options.CloseToTray = on
	}
	return nil
}

// TestDesktopHostServesTheWindowOptions is the pane's four-row surface without a window:
// the read answers the registration and the app's three config values, a write lands
// through the one store (or the ONE autostart control the tray row flips), and a refusal
// answers with the writer's words.
func TestDesktopHostServesTheWindowOptions(t *testing.T) {
	const origin = "http://127.0.0.1:18101"
	store := &fakeWindowStore{options: DesktopConfig{CloseToTray: true}}
	backend := &fakeAutostart{}
	host, probe := newTestDesktopHost(origin, &fakeInstanceStore{})
	host.setWindowOptions(store, newAutostartControl(backend, nil))
	page := &application.OriginInfo{Origin: origin + "/", TopOrigin: origin + "/"}

	last := func() desktopWindowReply {
		t.Helper()
		if len(probe.events) == 0 {
			t.Fatal("the host answered nothing")
		}
		event := probe.events[len(probe.events)-1]
		if event.name != desktopWindowOptionsEvent {
			t.Fatalf("event = %q, want %q", event.name, desktopWindowOptionsEvent)
		}
		reply, ok := event.payload.(desktopWindowReply)
		if !ok {
			t.Fatalf("payload type = %T, want desktopWindowReply", event.payload)
		}
		return reply
	}

	if err := host.Handle(page, `{"omega":"window:get"}`); err != nil {
		t.Fatalf("window:get: %v", err)
	}
	first := last()
	if first.Error != "" || first.StartWithSystem || first.StartMinimized || first.MinimizeToTray || !first.CloseToTray {
		t.Fatalf("the first answer = %+v, want the registration off and the app's false/false/true", first)
	}

	// a config row writes through the store and the answer is the re-read state
	if err := host.Handle(page, `{"omega":"window:set","key":"minimize_to_tray","value":true}`); err != nil {
		t.Fatalf("window:set minimize: %v", err)
	}
	if !store.saved[desktopOptionMinimizeToTray] {
		t.Fatalf("the write did not reach the store: %+v", store.saved)
	}
	if got := last(); !got.MinimizeToTray {
		t.Fatalf("the answer after the write = %+v, want minimize_to_tray on", got)
	}

	// start with system flips the SAME registration the tray row flips
	if err := host.Handle(page, `{"omega":"window:set","key":"start_with_system","value":true}`); err != nil {
		t.Fatalf("window:set autostart: %v", err)
	}
	if !backend.enabled {
		t.Fatal("the pane's Start with Windows row did not write the registration")
	}
	if len(backend.args) != 1 || len(backend.args[0]) != 1 || backend.args[0][0] != autostartDesktopArg {
		t.Fatalf("registration args = %q, want exactly [%q]", backend.args, autostartDesktopArg)
	}
	if got := last(); !got.StartWithSystem {
		t.Fatalf("the answer after the autostart flip = %+v, want start_with_system on", got)
	}
	if _, configWrite := store.saved[desktopOptionStartWithSystem]; configWrite {
		t.Fatal("the autostart row wrote a config key")
	}

	// a refused write answers the writer's words; the read behind the same broken store
	// cannot answer a state either, so the rows fall to their zero values under the error
	// line - never a row that silently reads the pre-write state as truth
	store.err = errors.New("config.yaml is read-only")
	if err := host.Handle(page, `{"omega":"window:set","key":"close_to_tray","value":false}`); err != nil {
		t.Fatalf("window:set refusal: %v", err)
	}
	if got := last(); !strings.Contains(got.Error, "read-only") {
		t.Fatalf("a refused write answered %+v, want the writer's words", got)
	}
}

// TestDesktopHostWindowOptionsWithoutASurface: a host built without the store answers the
// refusal instead of guessing a state - never a row that silently reads off.
func TestDesktopHostWindowOptionsWithoutASurface(t *testing.T) {
	const origin = "http://127.0.0.1:18101"
	host, probe := newTestDesktopHost(origin, &fakeInstanceStore{})
	page := &application.OriginInfo{Origin: origin + "/", TopOrigin: origin + "/"}

	if err := host.Handle(page, `{"omega":"window:get"}`); err != nil {
		t.Fatalf("window:get: %v", err)
	}
	reply := probe.events[len(probe.events)-1].payload.(desktopWindowReply)
	if !strings.Contains(reply.Error, "no options surface") {
		t.Fatalf("a host without a surface answered %+v, want the refusal", reply)
	}
}

// fakeCaptionWindow counts the caption presses that reached the window itself.
type fakeCaptionWindow struct{ minimised, toggled, closed int }

func (f *fakeCaptionWindow) Minimise() application.Window { f.minimised++; return nil }
func (f *fakeCaptionWindow) ToggleMaximise()              { f.toggled++ }
func (f *fakeCaptionWindow) Close()                       { f.closed++ }

// TestDesktopCaptionPressesReachTheShell: the minimize cell asks the shell, so
// minimize-to-tray hides and never touches the window's own minimize; the other two cells
// are the window's.
func TestDesktopCaptionPressesReachTheShell(t *testing.T) {
	win := &fakeCaptionWindow{}
	shell, probe := newTrayShell()
	shell.setWindowPolicy(func() desktopWindowPolicy {
		return desktopWindowPolicy{MinimizeToTray: true}
	})

	desktopWindowAction(win, shell, desktopWindowEventMinimise)
	if win.minimised != 0 {
		t.Fatalf("the caption's minimize called the window's own minimize (%d times)", win.minimised)
	}
	if probe.hidden != 1 {
		t.Fatalf("hides = %d, want 1", probe.hidden)
	}

	desktopWindowAction(win, shell, desktopWindowEventToggleMaximise)
	desktopWindowAction(win, shell, desktopWindowEventClose)
	if win.toggled != 1 || win.closed != 1 {
		t.Fatalf("toggles = %d closes = %d, want one each", win.toggled, win.closed)
	}
}
