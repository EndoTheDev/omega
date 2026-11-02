package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// The curator's LLM access (card t_9972dfd0): the thin bridge over
// the provider seam. The pass streams one completion per consult and
// collects the text - no tool loop, no retries beyond the client's
// own (llm/RetryHTTP), failures surface to the caller and the
// pass moves on.

// complete runs ONE collect-the-text completion over the provider.
func complete(ctx context.Context, prov core.Provider, prompt string) (string, error) {
	if prov == nil {
		return "", fmt.Errorf("no provider")
	}
	var sb strings.Builder
	for ev := range prov.Stream(ctx, []core.Message{core.NewUser(prompt)}, nil) {
		switch e := ev.(type) {
		case core.ResponseChunk:
			sb.WriteString(e.Content)
		case core.StreamEnd:
			if e.FinishReason == "error" {
				return "", fmt.Errorf("curator consult: %s", e.Error)
			}
		}
	}
	return sb.String(), nil
}

// extractJSON pulls the first JSON object out of a model answer that
// may wrap it in prose or fences.
func extractJSON(s string) string {
	if i := strings.Index(s, "{"); i >= 0 {
		// Prefer a fenced block when one opens after the first brace.
		if start := strings.Index(s, "```"); start >= 0 && start <= i {
			b := strings.TrimPrefix(s[start:], "```")
			b = strings.TrimPrefix(b, "json")
			return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(b), "```"))
		}
		if j := strings.LastIndex(s, "}"); j > i {
			return s[i : j+1]
		}
	}
	return s
}

// absorbedSection is the drafted consolidation text for one merge.
type absorbedSection struct {
	title string
	body  string
}

// draftAbsorption asks for ONE call: the umbrella's current body plus
// the source's contents, out comes rules-only text for a new section.
// The answer format is JSON ({"title","body"}) so the code path is
// deterministic about what is section text.
func (sp *SkillsProvider) draftAbsorption(ctx context.Context, prov core.Provider, umbrella, umbrellaBody, source, sourceBody string) (absorbedSection, error) {
	var sb strings.Builder
	sb.WriteString("You are the skills curator. A narrow skill is being absorbed into an umbrella skill. Distill the SOURCE's unique insights into concise rules that will live inside the UMBRELLA as one new section. Keep only what the umbrella does not already say; rules, not narration.\n\n")
	fmt.Fprintf(&sb, "UMBRELLA: %s (current body below)\n%s\n\n", umbrella, oneLine(umbrellaBody, 6000))
	fmt.Fprintf(&sb, "SOURCE: %s (full body below)\n%s\n\n", source, oneLine(sourceBody, 24000))
	sb.WriteString(`Answer with ONLY a JSON object: {"title":"one short section title","body":"the distilled markdown section"}.` + "\n")
	out, err := complete(ctx, prov, sb.String())
	if err != nil {
		return absorbedSection{}, err
	}
	var d struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := jsonUnmarshalStrict(extractJSON(out), &d); err != nil {
		return absorbedSection{}, fmt.Errorf("absorption draft: %w", err)
	}
	d.Title = strings.TrimSpace(d.Title)
	d.Body = strings.TrimSpace(d.Body)
	// Never let an invented title rename reality: the section line
	// names the source skill either way.
	return absorbedSection{title: d.Title, body: d.Body}, nil
}

// draftDistill asks for ONE call: rewrite the oversized body tighter,
// same procedures, no loss of load-bearing facts.
func (sp *SkillsProvider) draftDistill(ctx context.Context, prov core.Provider, name, body string) (string, error) {
	var sb strings.Builder
	sb.WriteString("You are the skills curator. The skill below exceeds the body-size floor. Rewrite its body tighter: same procedures, preserved commands and exact identifiers, cut narration and repetition. No frontmatter - body text only.\n\n")
	fmt.Fprintf(&sb, "SKILL: %s (full body below)\n%s\n\n", name, body)
	sb.WriteString("Answer with ONLY the rewritten markdown body, nothing else.\n")
	return complete(ctx, prov, sb.String())
}

// jsonUnmarshalStrict is extractJSON's pair: decode or fail loudly.
func jsonUnmarshalStrict(data string, v any) error {
	if s := strings.TrimSpace(data); s == "" || s[0] != '{' {
		return fmt.Errorf("answer carried no JSON object")
	}
	return json.Unmarshal([]byte(data), v)
}
