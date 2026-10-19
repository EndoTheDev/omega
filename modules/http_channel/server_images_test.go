package http_channel

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EndoTheDev/omega/ai"
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
	messages := []ai.Message{ai.NewUserWithImages("look", []ai.ImageContent{
		{MediaType: "image/png", Base64: base64.StdEncoding.EncodeToString(ok)},
	})}
	if err := checkImageSizes(messages); err != nil {
		t.Fatalf("checkImageSizes ok-case: %v", err)
	}

	// just over the cap -> error names the message/image indexes
	big := make([]byte, maxChatImageBytes+1)
	messages[0] = ai.NewUserWithImages("look", []ai.ImageContent{
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
	if err := checkImageSizes([]ai.Message{ai.NewUser("no images"), ai.NewAssistant("hi")}); err != nil {
		t.Fatalf("imageless check: %v", err)
	}
}

func TestChatOversizeImageRejected(t *testing.T) {
	s := newTestServer()
	big := make([]byte, maxChatImageBytes+1)
	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]any{{
			"role": "user",
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
	var img ai.ImageContent
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
	orig := ai.NewUserWithImages("what is this", []ai.ImageContent{
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
	user, ok := got[0].(ai.User)
	if !ok {
		t.Fatalf("type = %T, want ai.User", got[0])
	}
	if len(user.Images) != 1 || user.Images[0].MediaType != "image/png" || user.Images[0].Base64 != "iVBORw0KGgo=" {
		t.Fatalf("images round-trip mismatch: %+v", user.Images)
	}
	_ = s
}