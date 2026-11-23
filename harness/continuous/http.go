package continuous

// http.go: the JSON response pair shared by both REST faces in this capsule (the /continuous
// handlers in plugin.go and the roster's /agents handlers in agentsrest.go). One body, so a
// change to the error envelope or the content type lands everywhere at once.

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, code int, format string, args ...any) {
	writeJSON(w, code, map[string]string{"error": fmt.Sprintf(format, args...)})
}
