package agent

// engine/agent/configedit.go: the agents-tab edit write path (card
// t_2d8deb8c, ruling 3 - "maybe lets me edit/update them there too").
//
// ScaffoldAgent seeds agents/<name>/config/config.yaml as a COMMENTED
// template: the file is the agent's layer over the home config and an
// unscaffolded edit cannot break boot. The edit path must preserve
// that property - it parses the file as a yaml.Node and upserts ONLY
// the scaffold-supported fields (description, provider.type,
// provider.model_name, provider.host, provider.api_key,
// system_prompt), mutating values in place and leaving every other
// key, comment and style byte-intact. Keys the layer already carries
// beyond the scaffold's set never leave the file.

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrAgentConfigLocked marks the DEFAULT agent's read-only config: the
// layer file is the home config itself (the daemon's brain), so the
// agents surface must never write it. The endpoint maps it to 409 and
// the tab renders the default row's detail read-only.
var ErrAgentConfigLocked = errors.New("the default agent's config is the home config - it is not editable from the agents surface")

// AgentConfigScaffoldError marks a caller's-mistake field (an unknown
// provider type, say): the 400 lane, not a 500.
type AgentConfigScaffoldError struct{ Message string }

func (e *AgentConfigScaffoldError) Error() string { return e.Message }

// ScaffoldProviderTypes is the provider vocabulary the scaffold
// template names (defaultAgentConfigTemplate's comment line). An edit
// outside it is refused loudly - a typo'd type would half-break the
// next wake only, which is the worst kind of failure.
var ScaffoldProviderTypes = []string{"ollama", "openai", "anthropic"}

// AgentConfigUpdates is the PUT /agents/{name} write set. A nil field
// is untouched - the endpoint writes only what the request carries;
// absent facts (the layer's host/api_key are not in the profile
// payload) can never be blanked by a UI round-trip that did not
// receive them.
type AgentConfigUpdates struct {
	Description  *string
	ProviderType *string
	ModelName    *string
	Host         *string
	APIKey       *string
	SystemPrompt *string
}

// UpdateAgentConfigLayer upserts the scaffold-supported fields into
// agents/<name>/config/config.yaml (the agent's own layer - NEVER the
// home config: the DEFAULT agent refuses here, its config IS the home
// config and the agents tab renders it read-only). The write is
// atomic; untouched keys keep their comments.
func UpdateAgentConfigLayer(home, name string, u AgentConfigUpdates) error {
	if name == DefaultAgentName {
		return ErrAgentConfigLocked
	}
	if u.ProviderType != nil {
		pt := strings.TrimSpace(*u.ProviderType)
		if pt != "" && !slicesContains(ScaffoldProviderTypes, pt) {
			return &AgentConfigScaffoldError{Message: fmt.Sprintf("provider type %q is not scaffolded (want one of: %s)", pt, strings.Join(ScaffoldProviderTypes, ", "))}
		}
	}

	path := AgentConfigPath(home, name)
	root := &yaml.Node{}
	if data, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		if err := yaml.Unmarshal(data, root); err != nil {
			return fmt.Errorf("parse agent config %s: %w", name, err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read agent config %s: %w", name, err)
	}

	mapping := rootMapping(root)
	if mapping == nil {
		root = docOf(mappingNode())
		mapping = rootMapping(root)
	}
	if u.Description != nil {
		upsertScalar(mapping, "description", strings.TrimSpace(*u.Description))
	}
	if u.SystemPrompt != nil {
		upsertScalar(mapping, "system_prompt", strings.TrimSpace(*u.SystemPrompt))
	}
	if u.ProviderType != nil || u.ModelName != nil || u.Host != nil || u.APIKey != nil {
		prov := ensureMapping(mapping, "provider")
		for _, sub := range []struct {
			key   string
			value *string
		}{
			{"type", u.ProviderType},
			{"model_name", u.ModelName},
			{"host", u.Host},
			{"api_key", u.APIKey},
		} {
			if sub.value != nil {
				upsertScalar(prov, sub.key, strings.TrimSpace(*sub.value))
			}
		}
	}

	out, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("render agent config %s: %w", name, err)
	}
	return WriteFileAtomic(path, out, 0o644)
}

// rootMapping resolves the working yamlnode mapping under a parsed
// document (or the parsed root itself when a plain mapping).
func rootMapping(root *yaml.Node) *yaml.Node {
	switch root.Kind {
	case 0, yaml.DocumentNode:
		if len(root.Content) == 0 {
			return nil
		}
		if inner := root.Content[0]; inner.Kind == yaml.MappingNode {
			return inner
		}
		return nil
	case yaml.MappingNode:
		return root
	default:
		return nil
	}
}

// SetDocument seats m inside a fresh document node.
func docOf(m *yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{m}}
}

func mappingNode() *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode} }

// upsertScalar sets (or appends) a scalar string value under key,
// mutating the mapping's Content in place (yaml.v3 stores mapping
// pairs as key/value node pairs inside one Content slice).
func upsertScalar(m *yaml.Node, key, value string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.Value == key {
			if v.Kind == yaml.ScalarNode {
				v.Value = value
				v.Tag = "!!str"
			}
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

// ensureMapping returns the mapping under key, creating it (plain
// inline style, no comment) when absent.
func ensureMapping(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.Value == key {
			if v.Kind == yaml.MappingNode {
				return v
			}
			replace := mappingNode()
			v.Kind = yaml.MappingNode
			v.Tag = ""
			v.Value = ""
			v.Content = replace.Content
			return v
		}
	}
	prov := mappingNode()
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		prov)
	return prov
}

func slicesContains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
