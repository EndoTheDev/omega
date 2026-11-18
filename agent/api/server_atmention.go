package api

import (
	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/llm"
	"strings"
)

// The @-mention expansion on the webui submit path. The old shell ran extractImages
// inline before a send; the webui gets the same law SERVER-SIDE on /chat - the browser
// never expands.

// contextDocs snapshots the mounted skill capsule's inventory as name/content pairs for
// the @skill. An unmounted module (or a listing failure) degrades to nil - tokens stay
// as-is, exactly like every other unresolvable token.
func (s *Server) contextDocs() []core.AtDoc {
	sk := s.skills()
	if sk == nil {
		return nil
	}
	_, contents, listErr := sk.SkillsEntries()
	if listErr != nil || len(contents) == 0 {
		return nil
	}
	out := make([]core.AtDoc, 0, len(contents))
	for name, content := range contents {
		out = append(out, core.AtDoc{Name: name, Content: content})
	}
	return out
}

// expandAtMentions runs the core @-mention law over the run's new input.
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
		Docs:          s.contextDocs(),
		DetectImage:   llm.DetectImage,
		MessageText:   agent.MessageText,
		Dir:           dir,
		ReferenceOnly: true, // the webui attaches PATHS (card t_7f09bd0c)
	}
	prompt, images, _, err := core.ExpandAtMentions(um.Content, env)
	if err != nil {
		// The submit notice law: a failing token stays in the text and the send proceeds.
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
