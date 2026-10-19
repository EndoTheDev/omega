package http_channel

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/EndoTheDev/omega/ai"
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
func checkImageSizes(messages []ai.Message) error {
	for i, m := range messages {
		user, ok := m.(ai.User)
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

// handleAttachImage serves POST /attach-image: the SERVER fetches the
// URL's bytes (the webui never fetches cross-origin itself), sniffs
// the magic bytes, and answers {media_type, base64} in the ai.Image-
// Content shape - the client then rides the normal /chat image field.
// Transport is the same ai.RetryHTTP every provider and tool call
// uses; the web module's mirror fetch is text-only so binary image
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

	httpReq, err := http.NewRequestWithContext(r.Context(), "GET", trimmed, nil)
	if err != nil {
		http.Error(w, "build request: "+err.Error(), http.StatusBadRequest)
		return
	}
	// One content-length cap: anything answering with a larger body is
	// dropped before buffering.
	resp, err := ai.RetryHTTP(httpReq)
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