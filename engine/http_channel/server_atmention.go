package http_channel

import (
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/engine/agent"
	"github.com/EndoTheDev/omega/llm"
	"strings"
)

// The @-mention expansion on the webui submit path (card t_07aed3f9).
// The old shell ran extractImages inline before a send; the webui gets the
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
// input: the LAST incoming user message is the prompt. Card t_7f09bd0c
// rules the webui's attach semantics: the composer attaches PATHS - the
// @file token rides through as a REFERENCE the model follows with its
// own tools; no content inline, no image attach, no mime work on this
// path (the pill'd token survives in the text, the transcript renders
// it). @session:/@skill: still inject. dir roots the injections'
// relative file reads (the session's workspace in the store-backed
// path, the process cwd in the legacy one).
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
		Store:         s.store,
		Skills:        s.contextSkills(),
		DetectImage:   llm.DetectImage,
		MessageText:   agent.MessageText,
		Dir:           dir,
		ReferenceOnly: true, // the webui attaches PATHS (card t_7f09bd0c)
	}
	prompt, images, _, err := core.ExpandAtMentions(um.Content, env)
	if err != nil {
		// The submit notice law: a failing token stays in the text and
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
