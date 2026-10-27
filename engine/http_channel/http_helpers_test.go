package http_channel

import (
	"net/http"
	"strings"
)

// postRaw is the shared POST-for-JSON helper across the server tests.
func postRaw(tsURL, path, body string) (*http.Response, error) {
	return http.Post(tsURL+path, "application/json", strings.NewReader(body))
}
