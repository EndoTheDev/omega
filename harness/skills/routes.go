// The skills content-view REST face, riding the skills capsule
// (shell-ring shrink wave 3, card t_b75df168 - moved whole from
// agent/api/server_skills.go, wire unchanged). The webui's skill
// viewer tab reads ONE skill's full SKILL.md: GET
// /skills/content?name= rides the SkillRead face of the SAME provider
// the mount offers under the "skills" key - a host without the capsule
// answers 404 at the module mux (the route mounts and dies with it).
// The listing route never carries content; this route never carries
// the inventory - one skill per read, name-resolved.
package skills

import (
	"encoding/json"
	"net/http"
	"strings"
)

// skillsContentRow is one GET /skills/content body: the parsed
// frontmatter description plus the markdown body under the fence. The
// name echoes the request (the folder-name law makes it authoritative).
type skillsContentRow struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Truncated   bool   `json:"truncated"`
}

// validSkillRequestName is the handler-side name law: a skill name is a
// single directory component (the skill loader's folder-name rule). The
// provider resolves <dir>/<name>/SKILL.md, so this refuses traversal and
// hidden names BEFORE any provider call - the defense belongs to the
// wire, never to the provider implementation's good faith.
func validSkillRequestName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, `/\`) {
		return false
	}
	return !strings.HasPrefix(name, ".") && strings.TrimSpace(name) == name
}

// skillReadCap is the viewer's SKILL.md preview cap - the 64 KiB cut
// the file explorer's preview enforces (the files capsule's fileReadCap;
// the wire shapes match, one rule, two faces).
const skillReadCap = 64 * 1024

// Routes is the capsule's REST face over the mounted provider.
type Routes struct {
	svc *SkillsProvider
}

// newRoutes builds the content-view face on the mount's provider handle.
func newRoutes(sp *SkillsProvider) *Routes { return &Routes{svc: sp} }

// getOnly is the GET-only guard.
func getOnly(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

// fileError writes the structured JSON error envelope.
func fileError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// writeJSON serves the JSON response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// handleSkillsContent serves GET /skills/content?name=<skill>: the full
// SKILL.md body for the webui viewer tab. The read is capped at the
// file surface's 64 KiB preview cap with the truncated flag - a viewer
// tab shows the cut, it never streams an unbounded file.
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
		// the provider's not-found is a loud 404 (the viewer tab shows
		// it); everything else is a server failure
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
