package http_channel

import (
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
	"github.com/EndoTheDev/omega/engine/ai"
	"strings"
)

// The @-mention expansion on the webui submit path (card t_07aed3f9).
// The TUI ran extractImages inline before a send; the webui gets the
// same law SERVER-SIDE on /chat - the browser never expands. One law,
// one function: core.ExpandAtMentions.

// contextSkills snapshots the mounted skills module's list. An
// unmounted module (or a listing failure) degrades to nil - @skill:
// tokens stay as-is, exactly like every other unresolvable token.
func (s *Server) contextSkills() []core.Skill {
	sk := s.Skills()
	if sk == nil {
		return nil
	}
	if lister, ok := sk.(core.SkillsLister); ok {
		skills, err := lister.ListSkills()
		if err == nil {
			return skills
		}
	}
	return nil
}

// expandAtMentions runs the core @-mention law over the run's new
// input: the LAST incoming user message is the prompt, text-file
// tokens inline, image tokens become base64 ImageContent on the
// message, @session:/@skill: inject context. dir roots relative paths
// (the session's workspace in the store-backed path, the process cwd
// in the legacy one). Resolvable tokens are persisted in their
// expanded form (the TUI's parity: the model and the store both see
// the expansion); unresolvable ones ride through untouched.
func (s *Server) expandAtMentions(incoming []core.Message, dir string) []core.Message {
	if len(incoming) == 0 {
		return incoming
	}
	last := len(incoming) - 1
	um, ok := incoming[last].(core.User)
	if !ok || !strings.Contains(um.Content, "@") {
		return incoming
	}
	env := core.AtMentionEnv{
		Store:       s.store,
		Skills:      s.contextSkills(),
		DetectImage: ai.DetectImage,
		MessageText: agent.MessageText,
		Dir:         dir,
	}
	prompt, images, _, err := core.ExpandAtMentions(um.Content, env)
	if err != nil {
		// The TUI notice path: a failing token stays in the text and
		// the send proceeds. There is no notice channel on the wire;
		// the persisted message carries the token instead.
		_ = err
		return incoming
	}
	um.Content = prompt
	if len(images) > 0 {
		um.Images = append(um.Images, images...)
	}
	incoming[last] = um
	return incoming
}