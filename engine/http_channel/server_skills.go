package http_channel

import (
	"net/http"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// The skills content-view surface (card t_36ceb99b): the webui's skill
// viewer tab reads ONE skill's full SKILL.md. GET /skills/content?name=
// rides the SkillsReader seam the same way /skills rides SkillsLister -
// a host without the seam answers 501, not a made-up empty body. The
// listing route never carries content; this route never carries the
// inventory - one skill per read, name-resolved.

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
// seam resolves <dir>/<name>/SKILL.md, so this refuses traversal and
// hidden names BEFORE any seam call - the defense belongs to the wire,
// never to the provider implementation's good faith.
func validSkillRequestName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, `/\`) {
		return false
	}
	return !strings.HasPrefix(name, ".") && strings.TrimSpace(name) == name
}

// handleSkillsContent serves GET /skills/content?name=<skill>: the full
// SKILL.md body for the webui viewer tab. The read is capped at the
// file surface's 64 KiB preview cap with the truncated flag - a viewer
// tab shows the cut, it never streams an unbounded file.
func (s *Server) handleSkillsContent(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	name := r.URL.Query().Get("name")
	if !validSkillRequestName(name) {
		fileError(w, http.StatusBadRequest, "name must be a single skill folder name")
		return
	}
	reader, ok := s.Skills().(core.SkillsReader)
	if !ok || reader == nil {
		fileError(w, http.StatusNotImplemented, "skills content seam not mounted")
		return
	}
	skill, err := reader.ReadSkill(name)
	if err != nil {
		// the seam's not-found is a loud 404 (the viewer tab shows it);
		// everything else is a server failure
		if strings.Contains(err.Error(), "not found") {
			fileError(w, http.StatusNotFound, err.Error())
			return
		}
		fileError(w, http.StatusInternalServerError, err.Error())
		return
	}
	content := skill.Content
	truncated := false
	if len(content) > fileReadCap {
		content = content[:fileReadCap]
		truncated = true
	}
	writeJSON(w, http.StatusOK, skillsContentRow{
		Name:        skill.Name,
		Description: skill.Description,
		Content:     content,
		Truncated:   truncated,
	})
}