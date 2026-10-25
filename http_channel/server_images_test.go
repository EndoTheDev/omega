package http_channel

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/core"
)

// onePNG is a minimal valid PNG header (enough for the magic sniff, not
// a decodable image - the HTTP lane sniffs bytes, it never decodes).
var onePNG = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}

func TestCheckImageSizes(t *testing.T) {
	// a size whose base64 encodes without padding slack - ok. The guard's
	// decoded estimate (len*3/4) overestimates for padded base64, so the
	// exact-cap boundary sits just under: a multiple of 3 raw bytes is
	// the largest representation under 10MB.
	ok := make([]byte, maxChatImageBytes-maxChatImageBytes%3)
	messages := []core.Message{core.NewUserWithImages("look", []core.ImageContent{
		{MediaType: "image/png", Base64: base64.StdEncoding.EncodeToString(ok)},
	})}
	if err := checkImageSizes(messages); err != nil {
		t.Fatalf("checkImageSizes ok-case: %v", err)
	}

	// just over the cap -> error names the message/image indexes
	big := make([]byte, maxChatImageBytes+1)
	messages[0] = core.NewUserWithImages("look", []core.ImageContent{
		{MediaType: "image/png", Base64: base64.StdEncoding.EncodeToString(big)},
	})
	err := checkImageSizes(messages)
	if err == nil {
		t.Fatal("oversize image passed the guard")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("error = %q, want a 'too large' message", err)
	}

	// non-user messages and imageless users never trip it
	if err := checkImageSizes([]core.Message{core.NewUser("no images"), core.NewAssistant("hi")}); err != nil {
		t.Fatalf("imageless check: %v", err)
	}
}

func TestChatOversizeImageRejected(t *testing.T) {
	s := newTestServer()
	big := make([]byte, maxChatImageBytes+1)
	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]any{{
			"role":    "user",
			"content": "look",
			"images": []map[string]string{
				{"media_type": "image/png", "base64": base64.StdEncoding.EncodeToString(big)},
			},
		}},
	})
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "too large") {
		t.Fatalf("body = %q, want a size message", rec.Body.String())
	}
}

func TestAttachImage(t *testing.T) {
	oldGuard := internalHostGuard
	internalHostGuard = func(string) bool { return false }
	defer func() { internalHostGuard = oldGuard }()

	// upstream: a server that answers with PNG bytes
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(onePNG) //nolint:errcheck // test server
	}))
	defer upstream.Close()

	s := newTestServer()
	body, _ := json.Marshal(attachImageRequest{URL: upstream.URL + "/img.png"})
	req := httptest.NewRequest(http.MethodPost, "/attach-image", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var img core.ImageContent
	if err := json.Unmarshal(rec.Body.Bytes(), &img); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if img.MediaType != "image/png" {
		t.Fatalf("media_type = %q, want image/png", img.MediaType)
	}
	raw, err := base64.StdEncoding.DecodeString(img.Base64)
	if err != nil {
		t.Fatalf("base64: %v", err)
	}
	if string(raw) != string(onePNG) {
		t.Fatalf("base64 payload mismatch")
	}
}

func TestAttachImageNonImage(t *testing.T) {
	oldGuard := internalHostGuard
	internalHostGuard = func(string) bool { return false }
	defer func() { internalHostGuard = oldGuard }()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html>this is text</html>")) //nolint:errcheck // test server
	}))
	defer upstream.Close()

	s := newTestServer()
	body, _ := json.Marshal(attachImageRequest{URL: upstream.URL})
	req := httptest.NewRequest(http.MethodPost, "/attach-image", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not an image") {
		t.Fatalf("body = %q, want 'not an image'", rec.Body.String())
	}
}

func TestAttachImageOversize(t *testing.T) {
	oldGuard := internalHostGuard
	internalHostGuard = func(string) bool { return false }
	defer func() { internalHostGuard = oldGuard }()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(onePNG) //nolint:errcheck // test server
		// pad past the cap
		if _, err := w.Write(make([]byte, maxChatImageBytes+1)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer upstream.Close()

	s := newTestServer()
	body, _ := json.Marshal(attachImageRequest{URL: upstream.URL})
	req := httptest.NewRequest(http.MethodPost, "/attach-image", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestAttachImageValidation(t *testing.T) {
	s := newTestServer()
	cases := []struct {
		name string
		url  string
		want int
	}{
		{"missing", "", http.StatusBadRequest},
		{"non-http-scheme", "file:///etc/hosts", http.StatusBadRequest},
		{"bad-scheme", "ftp://example.invalid/i.png", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(attachImageRequest{URL: tc.url})
			req := httptest.NewRequest(http.MethodPost, "/attach-image", strings.NewReader(string(body)))
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}

	// GET is refused
	req := httptest.NewRequest(http.MethodGet, "/attach-image", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405", rec.Code)
	}
}

func TestStoreImagesRoundTrip(t *testing.T) {
	s, st := newTestServerWithStore(t)
	if err := st.CreateSession(t.Context(), "s-img", "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	orig := core.NewUserWithImages("what is this", []core.ImageContent{
		{MediaType: "image/png", Base64: "iVBORw0KGgo="},
	})
	if err := st.AppendMessage(t.Context(), "s-img", orig); err != nil {
		t.Fatalf("append: %v", err)
	}
	got, err := st.GetMessages(t.Context(), "s-img")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("messages = %d, want 1", len(got))
	}
	user, ok := got[0].(core.User)
	if !ok {
		t.Fatalf("type = %T, want core.User", got[0])
	}
	if len(user.Images) != 1 || user.Images[0].MediaType != "image/png" || user.Images[0].Base64 != "iVBORw0KGgo=" {
		t.Fatalf("images round-trip mismatch: %+v", user.Images)
	}
	_ = s
}

// TestAttachImageRefusesInternal exercises the wire contract itself: IP
// literals inside the box, the LAN, and the Tailscale ring are refused
// before any fetch happens; a public literal is allowed through the guard.
func TestAttachImageRefusesInternal(t *testing.T) {
	s := newTestServer()
	for _, host := range []string{
		"127.0.0.1", "10.1.2.3", "192.168.1.4", "172.16.0.9",
		"169.254.169.254", "100.101.126.30", "[::1]",
	} {
		body := `{"url":"http://` + host + `/x.png"}`
		req := httptest.NewRequest(http.MethodPost, "/attach-image", strings.NewReader(body))
		w := httptest.NewRecorder()
		s.handleAttachImage(w, req)
		if w.Code != 400 || !strings.Contains(w.Body.String(), "internal address") {
			t.Fatalf("%s: status = %d, body = %q, want 400 internal-address refusal", host, w.Code, w.Body.String())
		}
	}
}

// TestAttachImageRedirectToInternalBlocked closes the redirect hole: a
// public-looking first hop that 302s to an internal target must abort
// mid-chain, not follow it. The guard stub lets the httptest first hop
// (127.0.0.1) through and blocks only the redirect target - the same
// seam production uses for every hop.
func TestAttachImageRedirectToInternalBlocked(t *testing.T) {
	oldGuard := internalHostGuard
	internalHostGuard = func(host string) bool { return host == "192.0.2.9" }
	defer func() { internalHostGuard = oldGuard }()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://192.0.2.9/secret.png", http.StatusFound)
	}))
	defer upstream.Close()

	s := newTestServer()
	body, _ := json.Marshal(attachImageRequest{URL: upstream.URL})
	req := httptest.NewRequest(http.MethodPost, "/attach-image", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "refusing to fetch internal address 192.0.2.9") {
		t.Fatalf("body = %q, want the per-hop internal-address refusal", rec.Body.String())
	}
}
