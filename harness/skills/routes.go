// The skills content-view REST face, riding the skills capsule. The listing route never
// carries content; this route never carries the inventory - one skill per read,
// name-resolved.
package skills

import (
	"encoding/json"
	"net/http"
	"strings"
)

type skillsContentRow struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Truncated   bool   `json:"truncated"`
}

// validSkillRequestName is the handler-side name law. The provider resolves
// <dir>/<name>/SKILL.md, so this refuses traversal and hidden names BEFORE any provider
// call - the defense belongs to the wire, never to the provider implementation's good
// faith.
func validSkillRequestName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, `/\`) {
		return false
	}
	return !strings.HasPrefix(name, ".") && strings.TrimSpace(name) == name
}

const skillReadCap = 64 * 1024

type Routes struct {
	svc *SkillsProvider
}

func newRoutes(sp *SkillsProvider) *Routes { return &Routes{svc: sp} }

func getOnly(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

func fileError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// handleSkillsContent serves GET /skills/content?name=<skill>. The read is capped at
// the file surface's 64 KiB preview cap with the truncated flag - a viewer tab shows
// the cut, it never streams an unbounded file.
func (rt *Routes) handleSkillsContent(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	name := r.URL.Query().Get("name")
	if !validSkillRequestName(name) {
		fileError(w, http.StatusBadRequest, "name must be a single skill folder name")
		return
	}
	description, content, err := rt.svc.SkillRead(name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			fileError(w, http.StatusNotFound, err.Error())
			return
		}
		fileError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := false
	if len(content) > skillReadCap {
		content = content[:skillReadCap]
		truncated = true
	}
	writeJSON(w, http.StatusOK, skillsContentRow{
		Name:        name,
		Description: description,
		Content:     content,
		Truncated:   truncated,
	})
}
