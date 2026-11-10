package agent

// agent/configedit.go: the agents-tab edit write path (card
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
	"slices"
	"strconv"
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
var ScaffoldProviderTypes = []string{"ollama", "openai", "anthropic", "mock"}

// AgentConfigUpdates is the PUT /agents/{name} write set. A nil field
// is untouched - the endpoint writes only what the request carries;
// absent facts (the layer's host/api_key are not in the profile
// payload) can never be blanked by a UI round-trip that did not
// receive them.
//
// MaxTurns and Unset are the settings tier 3b additions (card
// t_53523d5b). MaxTurns writes the runtime knob as a yaml INT node -
// the layer's max_turns is unmarshalled straight into Config.MaxTurns
// int at the next wake, so a quoted "42" would be a boot error, and 0
// ("no cap") is a real value: nil is what means unset.
//
// Unset is the reset verb: each entry names a knob in the SAME dotted
// vocabulary the settings scope payload uses, and its key pair is
// DELETED from the layer file instead of being written down with main's
// value. That difference is the whole point - the scope payload flags a
// knob "overridden" structurally (the file names the key), so a
// reset-by-write would leave a permanent phantom override, and for
// provider.api_key it would persist the literal mask "****" as the
// agent's secret. Deleting is the only reset that falls back to main.
type AgentConfigUpdates struct {
	Description  *string
	ProviderType *string
	ModelName    *string
	Host         *string
	APIKey       *string
	SystemPrompt *string
	MaxTurns     *int
	Unset        []string
}

// MaskedSecretPlaceholder is the mask the read lanes put in place of a
// stored provider.api_key (cmd/omega's maskedSecret, the settings
// payload's secret knob). It is NOT a secret, so the write lane refuses
// it: a UI echoing the mask back must never persist "****" as the
// agent's key. The reset verb for the secret is
// unset: ["provider.api_key"]. The agent package cannot import the
// host's constant (layer law), so the literal is mirrored here and a
// cmd/omega test pins the two strings equal.
const MaskedSecretPlaceholder = "****"

// ScaffoldUnsetKeys is the dotted vocabulary `unset` accepts: exactly
// the scaffold's knob set (the upsert lane's fields) plus max_turns,
// spelled the way the settings scope payload spells them
// (cmd/omega/settings.go scopeGroups). An entry outside it is refused -
// the wire names a real layer key, never a guess at one.
var ScaffoldUnsetKeys = []string{
	"description",
	"system_prompt",
	"provider.type",
	"provider.model_name",
	"provider.host",
	"provider.api_key",
	"max_turns",
}

// updatesSetKey reports whether this request also SETS the dotted key -
// the set/unset conflict check. A key both written and deleted in one
// request has two answers and no ordering that is obviously right, so
// it is refused loudly rather than resolved silently.
func updatesSetKey(u AgentConfigUpdates, key string) bool {
	switch key {
	case "description":
		return u.Description != nil
	case "system_prompt":
		return u.SystemPrompt != nil
	case "provider.type":
		return u.ProviderType != nil
	case "provider.model_name":
		return u.ModelName != nil
	case "provider.host":
		return u.Host != nil
	case "provider.api_key":
		return u.APIKey != nil
	case "max_turns":
		return u.MaxTurns != nil
	}
	return false
}

// validateAgentUpdates is the refusal lane: every caller's-mistake field
// answers AgentConfigScaffoldError (the endpoint's 400), never a half
// write. The max_turns floor matches config.go's own
// "max_turns must be >= 0" rule, so a negative can never reach a wake
// and fail the agent's boot instead of this request.
func validateAgentUpdates(u AgentConfigUpdates) error {
	if u.ProviderType != nil {
		pt := strings.TrimSpace(*u.ProviderType)
		if pt != "" && !slices.Contains(ScaffoldProviderTypes, pt) {
			return &AgentConfigScaffoldError{Message: fmt.Sprintf("provider type %q is not scaffolded (want one of: %s)", pt, strings.Join(ScaffoldProviderTypes, ", "))}
		}
	}
	if u.MaxTurns != nil && *u.MaxTurns < 0 {
		return &AgentConfigScaffoldError{Message: fmt.Sprintf("max_turns %d is negative (want >= 0; 0 means no cap, omit the knob to inherit)", *u.MaxTurns)}
	}
	if u.APIKey != nil && strings.TrimSpace(*u.APIKey) == MaskedSecretPlaceholder {
		return &AgentConfigScaffoldError{Message: fmt.Sprintf("api_key %q is the read mask, not a secret - use unset: [\"provider.api_key\"] to clear the agent's key", MaskedSecretPlaceholder)}
	}
	for _, key := range u.Unset {
		if !slices.Contains(ScaffoldUnsetKeys, key) {
			return &AgentConfigScaffoldError{Message: fmt.Sprintf("unknown unset key %q (want one of: %s)", key, strings.Join(ScaffoldUnsetKeys, ", "))}
		}
		if updatesSetKey(u, key) {
			return &AgentConfigScaffoldError{Message: fmt.Sprintf("%q is both set and unset in one request", key)}
		}
	}
	return nil
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
	if err := validateAgentUpdates(u); err != nil {
		return err
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
	if u.MaxTurns != nil {
		// An INT node, never a string: ApplyAgentLayer unmarshals this
		// key straight into Config.MaxTurns int, so `max_turns: "42"`
		// would be the next wake's boot error.
		upsertInt(mapping, "max_turns", *u.MaxTurns)
	}
	// The reset verb, applied after the upserts: no key can be both set
	// and unset (validateAgentUpdates refuses it), so the order is a
	// readability choice, not a semantic one.
	for _, key := range u.Unset {
		unsetLayerKey(mapping, key)
	}

	out, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("render agent config %s: %w", name, err)
	}
	return WriteFileAtomic(path, out, 0o644)
}

// --- the read half (settings tier 3a, card t_22a5f70a) ---------------
//
// The settings agent-scope payload needs the same field set the upsert
// lane writes, WITH presence: which keys the layer file actually names.
// Profile (profiles.go) cannot answer that - its fields are omitempty
// strings, so "unset" and "set to empty" carry the same value - and
// presence is exactly the payload's override flag. So this read lives
// beside the write it mirrors, so one field added to the scaffold set
// is one place, not two.

// LayerValue is one string knob as the layer file gives it: the value
// and whether the file names the key at all. An explicitly empty value
// is a real override ("the agent set it empty" is not "inherit").
type LayerValue struct {
	Value string
	Set   bool
}

// AgentConfigLayer is the presence-aware read of one agent's layer
// file: the scaffold-supported knobs (UpdateAgentConfigLayer's set)
// plus max_turns, the runtime knob a layer may also carry. A knob the
// file leaves commented out or absent reads Set=false - the scaffold
// template comments every knob, so an unedited agent overrides nothing.
// MaxTurns is nil when the layer does not set it.
type AgentConfigLayer struct {
	Description  LayerValue
	SystemPrompt LayerValue
	ProviderType LayerValue
	ModelName    LayerValue
	Host         LayerValue
	APIKey       LayerValue
	MaxTurns     *int
}

// ReadAgentConfigLayer reads one agent's layer with presence. A missing
// (or empty) file is the empty layer - a scaffolded agent whose config
// was deleted still reads, it simply inherits everything. A file that
// will not parse is a loud error: reporting half-parsed values would
// make every override flag a guess.
//
// The DEFAULT agent refuses here: its "layer" is the home config, so
// reading it would report the daemon's own knobs as agent overrides.
// It is not a scope (its home is the home root, ruling R5).
func ReadAgentConfigLayer(home, name string) (AgentConfigLayer, error) {
	var lay AgentConfigLayer
	if name == DefaultAgentName {
		return lay, fmt.Errorf("read agent config: %q is the main agent - its config is the home config, not an agent scope", name)
	}
	data, err := os.ReadFile(AgentConfigPath(home, name))
	if err != nil {
		if os.IsNotExist(err) {
			return lay, nil
		}
		return lay, fmt.Errorf("read agent config %s: %w", name, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return lay, nil
	}
	// Pointers carry presence: nil = the key is absent, a pointer to ""
	// = the file explicitly set the knob empty.
	var raw struct {
		Description  *string `yaml:"description"`
		SystemPrompt *string `yaml:"system_prompt"`
		MaxTurns     *int    `yaml:"max_turns"`
		Provider     *struct {
			Type      *string `yaml:"type"`
			ModelName *string `yaml:"model_name"`
			Host      *string `yaml:"host"`
			APIKey    *string `yaml:"api_key"`
		} `yaml:"provider"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return lay, fmt.Errorf("parse agent config %s: %w", name, err)
	}
	// The write lane trims every value (upsertScalar), so the read
	// trims too: the payload must show what the write path would store.
	set := func(dst *LayerValue, v *string) {
		if v == nil {
			return
		}
		dst.Value, dst.Set = strings.TrimSpace(*v), true
	}
	set(&lay.Description, raw.Description)
	set(&lay.SystemPrompt, raw.SystemPrompt)
	if raw.Provider != nil {
		set(&lay.ProviderType, raw.Provider.Type)
		set(&lay.ModelName, raw.Provider.ModelName)
		set(&lay.Host, raw.Provider.Host)
		set(&lay.APIKey, raw.Provider.APIKey)
	}
	lay.MaxTurns = raw.MaxTurns
	return lay, nil
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
func upsertScalar(m *yaml.Node, key, value string) { upsertTagged(m, key, "!!str", value) }

// upsertInt sets (or appends) an INT value under key. The tag is what
// makes the rendered node `max_turns: 42` rather than `max_turns: "42"`
// - the value's quoting style is cleared too, since a hand-quoted
// string sitting where the int belongs must not survive the write.
func upsertInt(m *yaml.Node, key string, value int) {
	upsertTagged(m, key, "!!int", strconv.Itoa(value))
	if in := findScalar(m, key); in != nil {
		in.Style = 0
	}
}

// upsertTagged is the shared upsert: an existing scalar value is
// retagged in place (comments and quoting survive the assignment), a
// missing key is appended as a fresh pair. A hand-edited value node of
// the wrong KIND (a sequence where the int belongs, say) is replaced -
// leaving it would make the write silently do nothing and the wake's
// own parse fail at the next boot.
func upsertTagged(m *yaml.Node, key, tag, value string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.Value == key {
			if v.Kind == yaml.ScalarNode {
				v.Value = value
				v.Tag = tag
				return
			}
			v.Kind = yaml.ScalarNode
			v.Content = nil
			v.Style = 0
			v.Value = value
			v.Tag = tag
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value})
}

// findScalar returns the value node stored under key (nil when the key
// is absent or its value is not a scalar).
func findScalar(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.Value == key && v.Kind == yaml.ScalarNode {
			return v
		}
	}
	return nil
}

// deleteKey removes key's key/value pair from mapping m, reporting
// whether the key was there. Every other pair - and its comments and
// quoting - stays byte-identical: the reset verb deletes, it never
// rewrites the neighbours.
func deleteKey(m *yaml.Node, key string) bool {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if k := m.Content[i]; k.Kind == yaml.ScalarNode && k.Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return true
		}
	}
	return false
}

// unsetLayerKey removes one dotted knob from the layer mapping. A
// sub-key removal prunes its parent mapping when the last sub-key goes:
// the layer must never carry an empty `provider:` block into the next
// wake (an empty mapping would override the home provider with
// nothing). An absent key is a no-op - resetting an untouched knob is
// the common case, not an error.
func unsetLayerKey(mapping *yaml.Node, key string) {
	parent, leaf, dotted := strings.Cut(key, ".")
	if !dotted {
		deleteKey(mapping, key)
		return
	}
	var sub *yaml.Node
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		k, v := mapping.Content[i], mapping.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.Value == parent && v.Kind == yaml.MappingNode {
			sub = v
			break
		}
	}
	if sub == nil {
		return
	}
	deleteKey(sub, leaf)
	if len(sub.Content) == 0 {
		deleteKey(mapping, parent)
	}
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
