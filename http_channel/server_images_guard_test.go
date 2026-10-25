package http_channel

// Guard-shape tests for the /attach-image internal-address refusal,
// covering the live-probed bypass forms (card t_860033e3): non-canonical
// numeric IPv4 spellings that netip cannot parse but the OS dialer
// still numeric-parses, and the IPv6 rings the first guard revision
// left out. The plain fetch-flow tests stub the guard seam; these
// exercise the real guard on the wire (400 + "internal address").

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAttachImageRefusesNonCanonicalIPs walks the bypass forms: every
// one must be refused with the internal-address 400 before any fetch.
func TestAttachImageRefusesNonCanonicalIPs(t *testing.T) {
	s := newTestServer()
	for _, host := range []string{
		// combined decimal forms: "2130706433" -> 127.0.0.1, "127.1" too
		"2130706433", "127.1", "999.1.1",
		// hex-marked labels
		"0x7f.0.0.1", "0X7F.0.0.1", "0x7f000001",
		// zero-padded octal-ish labels
		"0177.0.0.1", "010.0.0.1",
		// trailing root dot must not smuggle a numeric spelling
		"127.0.0.1.",
		// 0.0.0.0/8 and the v6 local rings
		"0.0.0.0", "[::]", "[::ffff:127.0.0.1]", "[fe80::1]", "[fd00:1::5]", "[fc00::9]",
	} {
		body := `{"url":"http://` + host + `/x.png"}`
		req := httptest.NewRequest(http.MethodPost, "/attach-image", strings.NewReader(body))
		w := httptest.NewRecorder()
		s.handleAttachImage(w, req)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "internal address") {
			t.Fatalf("%s: status = %d, body = %q, want 400 internal-address refusal", host, w.Code, w.Body.String())
		}
	}
}

// TestIsInternalHostRingsAndHostnames unit-checks the guard both ways:
// the internal rings (old and new) must trip it, public canonical
// literals and real hostnames must pass through.
func TestIsInternalHostRingsAndHostnames(t *testing.T) {
	for _, host := range []string{
		"127.0.0.1", "::1", "::", "0.0.0.0",
		"169.254.169.254", "10.1.2.3", "192.168.1.4", "172.16.0.9",
		"100.101.126.30", "fe80::1", "fe80::1%eth0", "fc00::1234", "fdaa::1",
		// 4-in-6 spellings of an internal v4
		"::ffff:127.0.0.1", "::ffff:10.0.0.1",
	} {
		if !isInternalHost(host) {
			t.Fatalf("%s: want internal", host)
		}
	}
	for _, host := range []string{
		"192.0.2.10", "198.51.100.9", "9.9.9.9", "8.8.8.8",
		"2606:4700::6810:84e5",
		"example.com", "abc.xyz", "omega.invalid", "localhost.local",
	} {
		if isInternalHost(host) {
			t.Fatalf("%s: want public/passable", host)
		}
	}
}