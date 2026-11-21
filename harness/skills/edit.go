package skills

// edit.go: the webui skill editor's write face (the skills tab's CRUD).
// The tab showed the inventory and
// opened one SKILL.md in the viewer tab; the editor creates, rewrites
// and removes skills - the human's own surface for the skill folders
// the agent could only touch through the domain tool.
//
// Three write laws, one per operation:
//
// Create composes the file fresh (--- / description: / ---) exactly
//           the way the write action composes it - the loader's parse
//           of what was written is the shape proof.
// Update rewrites the BODY and the description line while every
//           frontmatter key the operator wrote stays byte-true: the
//           editor owns the markdown, never the whole frontmatter.
//           The merge-winner rule picks the file (shared first), the
//           same file the reader tool and the runUpdate action patch.
// Delete backs the folder up into .curator/backups, ledger row
//           attached, and removes it - a removal is receipted, never
//           a silent rm.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (sp *SkillsProvider) SaveSkill(name, description, body string) ([]Skill, error) {
	name = strings.TrimSpace(name)
	if !validSkillName(name) {
		return nil, fmt.Errorf("save: name must be a single folder component (no separators, no leading dot), got %q", name)
	}
	description = strings.TrimSpace(description)
	if description == "" || strings.ContainsAny(description, "\n\r") {
		return nil, fmt.Errorf("save: description is required and must be one line")
	}
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("save: body is required")
	}
	if strings.HasPrefix(strings.TrimLeft(body, " \t"), "---") {
		return nil, fmt.Errorf(`save: body must not start with a "---" fence - the frontmatter is composed for you`)
	}
	if sp.Dir == "" {
		return nil, fmt.Errorf("skills directory not configured")
	}
	body = strings.TrimRight(body, "\n") + "\n"

	if skillFile, err := sp.resolveSkillFile(name); err == nil {
		raw, err := os.ReadFile(skillFile)
		if err != nil {
			return nil, fmt.Errorf("read SKILL.md: %w", err)
		}
		front, _ := splitFrontmatter(string(raw))
		if front == "" {
			front = frontmatterOf(description)
		} else {
			front = replaceDescription(front, description)
		}
		skillFile, err = sp.writePatched(skillFile, front+body)
		if err != nil {
			return nil, err
		}
		TouchUsage(filepath.Dir(filepath.Dir(skillFile)), name, "write", "")
		return sp.ListSkills()
	}

	skillDir := filepath.Join(sp.Dir, name)
	if _, err := os.Stat(skillDir); err == nil {
		return nil, fmt.Errorf("save: a folder named %q already exists in %s - give the skill another name", name, skillDir)
	}
	content := frontmatterOf(description) + body
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return nil, fmt.Errorf("create skill dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("write SKILL.md: %w", err)
	}
	if _, err := loadSkill(filepath.Join(skillDir, "SKILL.md")); err != nil {
		return nil, fmt.Errorf("save: composed SKILL.md failed the loader's frontmatter parse: %w", err)
	}
	TouchUsage(sp.Dir, name, "write", "")
	return sp.ListSkills()
}

// DeleteSkill removes one skill's folder. An unknown name is the loud not-found error
// (a no-op delete must be visible - the silent-persist law in reverse).
func (sp *SkillsProvider) DeleteSkill(name string) ([]Skill, error) {
	return sp.DeleteSkillAs(name, ActorWebui, "removed from the skills tab")
}

func (sp *SkillsProvider) DeleteSkillAs(name, actor, reason string) ([]Skill, error) {
	name = strings.TrimSpace(name)
	if !validSkillName(name) {
		return nil, fmt.Errorf("delete: invalid skill name %q", name)
	}
	skillFile, err := sp.resolveSkillFile(name)
	if err != nil {
		return nil, err
	}
	srcDir := filepath.Dir(skillFile)
	root, err := sp.backupDir()
	if err != nil {
		return nil, err
	}
	if _, err := sp.backupFrom(srcDir, root, name, "delete"); err != nil {
		return nil, err
	}
	if _, err := sp.AppendLedger(actor, LedgerRemove, name, reason,
		[]LedgerFile{{Path: filepath.ToSlash(skillFile)}}, nil); err != nil {
		return nil, err
	}
	if err := os.RemoveAll(srcDir); err != nil {
		return nil, fmt.Errorf("remove skill %s: %w", name, err)
	}
	return sp.ListSkills()
}

const ActorWebui = "webui"

const ActorCLI = "cli"

type SkillsEditor interface {
	SaveSkill(name, description, body string) ([]Skill, error)
	DeleteSkill(name string) ([]Skill, error)
}

func frontmatterOf(description string) string {
	return fmt.Sprintf("---\ndescription: %s\n---\n", description)
}

// replaceDescription swaps the frontmatter's description line and keeps every other key
// byte-true (version, tags, the operator's own frontmatter law).
func replaceDescription(front, description string) string {
	front = strings.TrimSuffix(front, "\n")
	lines := strings.Split(front, "\n")
	out := []string{lines[0]} // the opening fence
	inner := lines[1 : len(lines)-1]
	replaced := false
	for _, l := range inner {
		if !replaced {
			key, _, found := strings.Cut(strings.TrimSpace(l), ":")
			if found && strings.TrimSpace(key) == "description" {
				out = append(out, "description: "+description)
				replaced = true
				continue
			}
		}
		out = append(out, l)
	}
	if !replaced {
		out = append(out, "description: "+description)
	}
	out = append(out, lines[len(lines)-1])
	return strings.Join(out, "\n") + "\n"
}

type skillSaveRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

type skillDeleteRequest struct {
	Name string `json:"name"`
}

type skillListingRow struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func serveEditorAction[reqT any](w http.ResponseWriter, r *http.Request, label string, act func(reqT) ([]Skill, error)) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req reqT
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, label+": bad body: "+err.Error(), http.StatusBadRequest)
		return
	}
	rows, err := act(req)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		http.Error(w, label+": "+err.Error(), status)
		return
	}
	lrows := make([]skillListingRow, 0, len(rows))
	for _, s := range rows {
		lrows = append(lrows, skillListingRow{Name: s.Name, Description: s.Description})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(lrows)
}

func (rt *Routes) handleSkillsSave(w http.ResponseWriter, r *http.Request) {
	serveEditorAction(w, r, "skills save", func(req skillSaveRequest) ([]Skill, error) {
		return rt.svc.SaveSkill(req.Name, req.Description, req.Content)
	})
}

func (rt *Routes) handleSkillsDelete(w http.ResponseWriter, r *http.Request) {
	serveEditorAction(w, r, "skills delete", func(req skillDeleteRequest) ([]Skill, error) {
		return rt.svc.DeleteSkill(req.Name)
	})
}

var _ SkillsEditor = (*SkillsProvider)(nil)
