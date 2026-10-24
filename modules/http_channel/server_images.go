package http_channel

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/core"
)

// maxChatImageBytes caps one user message image accepted via /chat
// (10MB decoded). The TUI's ai.MaxImageBytes file cap (20MB) still
// holds for local files; the browser lane sits behind the HTTP
// channel, so it picks a tighter cap - a 400 with a clear message
// beats a multi-second stream the model then chokes on.
const maxChatImageBytes = 10 * 1024 * 1024 // 10 MB

// checkImageSizes validates the images on every incoming user message.
// Base64 payloads decode to len(raw)*3/4-ish bytes; an exact ceiling
// is computed from the string length (padded base64 rounds up to a
// multiple of 4 bytes, so 3*len/4 slightly overestimates, which is the
// safe direction for a cap). Returns the offending image info on
// oversize - handleChat answers 400 with it.
func checkImageSizes(messages []core.Message) error {
	for i, m := range messages {
		user, ok := m.(core.User)
		if !ok {
			continue
		}
		for j, img := range user.Images {
			if decoded := len(img.Base64) * 3 / 4; decoded > maxChatImageBytes {
				return fmt.Errorf("message %d image %d too large: %d bytes (max %d)", i, j, decoded, maxChatImageBytes)
			}
		}
	}
	return nil
}

type attachImageRequest struct {
	URL string `json:"url"`
}

// urlHost extracts the host (IP or hostname) from an absolute http(s)
// URL, or "" when parsing fails.
func urlHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// internalRings is the deny list of IP rings that point back into the
// box or its private networks: the loopback and unspecified-address
// rings (0.0.0.0/8, ::/128 - dialing the unspecified address lands on
// the box), RFC1918, link-local (incl. cloud metadata), the Tailscale
// CGNAT 100.64/10 range, and the IPv6 local rings (ULA fc00::/7,
// link-local fe80::/10).
var internalRings = func() []netip.Prefix {
	rings := []string{
		"127.0.0.0/8", "10.0.0.0/8", "192.168.0.0/16", "172.16.0.0/12",
		"100.64.0.0/10", "0.0.0.0/8", "169.254.0.0/16",
		"::1/128", "::/128", "fe80::/10", "fc00::/7", "fd00::/8",
	}
	out := make([]netip.Prefix, 0, len(rings))
	for _, spec := range rings {
		out = append(out, netip.MustParsePrefix(spec))
	}
	return out
}()

func addrInInternalRing(addr netip.Addr) bool {
	// A zone ("fe80::1%eth0") would fail every prefix membership check:
	// Contains compares zones too. Drop it, then unmap to plain v4 so
	// the v4 rings catch 4-in-6 spellings ("::ffff:127.0.0.1").
	addr = addr.WithZone("").Unmap()
	for _, ring := range internalRings {
		if ring.Contains(addr) {
			return true
		}
	}
	return false
}

// looksLikeNumericIPv4 reports whether host is a numeric IPv4 spelling
// the OS dialer accepts but netip (and so any ring check on the parsed
// result) cannot express: a combined decimal form ("2130706433",
// "127.1" - single- or two-label all-decimal hosts dial the 32-bit
// value), hex-marked labels ("0x7f.0.0.1"), or zero-padded octal-ish
// labels ("0177.0.0.1"). These are refused, not treated as hostnames.
// Real hostnames keep working: they carry a non-numeric label
// somewhere, and zero-padded labels stopped resolving as hostnames
// years ago.
func looksLikeNumericIPv4(host string) bool {
	// A trailing root dot is legal DNS for a real hostname; drop it so
	// the trailing "." cannot smuggle a numeric spelling through.
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if strings.Contains(host, "0x") {
		return true
	}
	allDigits := true
	for _, label := range strings.Split(host, ".") {
		if label == "" || (len(label) > 1 && label[0] == '0') {
			// empty label (malformed) or a zero-padded label
			return true
		}
		for _, c := range label {
			if c < '0' || c > '9' {
				allDigits = false
				break
			}
		}
	}
	return allDigits
}

// isInternalHost reports whether host is an IP literal pointing back
// into the box itself or the private rings (internalRings), or a
// non-canonical numeric IPv4 spelling (looksLikeNumericIPv4). Canonical
// literals parse through netip; a zoned v6 literal ("fe80::1%eth0")
// parses where net.ParseIP refuses it.
func isInternalHost(host string) bool {
	if addr, err := netip.ParseAddr(host); err == nil {
		return addrInInternalRing(addr)
	}
	return looksLikeNumericIPv4(host)
}

// internalHostGuard is the seam the fetch-flow tests stub out (httptest
// servers always bind 127.0.0.1); the wire contract itself refuses
// every internal address via isInternalHost below.
var internalHostGuard = isInternalHost

// attachClient guards EVERY redirect hop. The first-hop check in
// handleAttachImage is not enough: a public 302 can point back into
// loopback, the LAN, or the Tailscale ring, and a plain http.Client
// follows up to 10 hops without looking. Each hop re-runs the same
// internalHostGuard; an internal target aborts the fetch.
var attachClient = &http.Client{
	Transport: ai.HTTPClient().Transport,
	Timeout:   60 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if host := req.URL.Hostname(); internalHostGuard(host) {
			return fmt.Errorf("refusing to fetch internal address %s", host)
		}
		return nil
	},
}

// handleAttachImage serves POST /attach-image: the SERVER fetches the
// URL's bytes (the webui never fetches cross-origin itself), sniffs
// the magic bytes, and answers {media_type, base64} in the ai.Image-
// Content shape - the client then rides the normal /chat image field.
// Fetches ride attachClient (per-hop internal-address guard, no
// retry); the web module's mirror fetch is text-only so binary image
// bytes can't ride it.
func (s *Server) handleAttachImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req attachImageRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&req); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	trimmed := strings.TrimSpace(req.URL)
	if trimmed == "" {
		http.Error(w, "invalid request: url is required", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
		http.Error(w, "invalid request: url must be http(s)", http.StatusBadRequest)
		return
	}
	// internal-address guard: the server fetches this URL, so it must not
	// become a probe of the box itself or the LAN/Tailscale ring. IP
	// literals only - hostnames that resolve internally are the ponytail:
	// ceiling (no DNS games); upgrade path is resolving + range-checking
	// if omega ever serves beyond a single user.
	if host := urlHost(trimmed); internalHostGuard(host) {
		http.Error(w, "refusing to fetch internal address "+host, http.StatusBadRequest)
		return
	}

	httpReq, err := http.NewRequestWithContext(r.Context(), "GET", trimmed, nil)
	if err != nil {
		http.Error(w, "build request: "+err.Error(), http.StatusBadRequest)
		return
	}
	// One content-length cap: anything answering with a larger body is
	// dropped before buffering.
	resp, err := attachClient.Do(httpReq)
	if err != nil {
		http.Error(w, "fetch: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		http.Error(w, fmt.Sprintf("fetch: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body))), http.StatusBadRequest)
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxChatImageBytes+1))
	if err != nil {
		http.Error(w, "read: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(data) > maxChatImageBytes {
		http.Error(w, fmt.Sprintf("image too large (%d bytes, max %d)", len(data), maxChatImageBytes), http.StatusRequestEntityTooLarge)
		return
	}
	img, err := ai.DetectImageBytes(data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		return
	}
	if img == nil {
		http.Error(w, "not an image (unrecognized format)", http.StatusBadRequest)
		return
	}
	encoded, err := json.Marshal(img)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(encoded) //nolint:errcheck // best-effort response
}
