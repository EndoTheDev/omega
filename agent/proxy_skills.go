package agent

import (
	"context"
	"encoding/json"
)

// SkillsDispatcher routes skills-seam JSON-RPC calls to the extension
// that declared the "skills" seam.
type SkillsDispatcher interface {
	SkillsRequest(ctx context.Context, method string, params map[string]any) (json.RawMessage, error)
}

// ProxySkills forwards SkillsProvider methods to a skills-seam
// extension via JSON-RPC.
type ProxySkills struct {
	Dispatcher SkillsDispatcher
}

func (p *ProxySkills) LoadSkills(dir string) ([]Skill, error) {
	raw, err := p.Dispatcher.SkillsRequest(context.Background(), "skills/load", map[string]any{"dir": dir})
	if err != nil {
		return nil, err
	}
	var skills []Skill
	if err := json.Unmarshal(raw, &skills); err != nil {
		return nil, err
	}
	return skills, nil
}