package agent

// agent/configedit.go: the agents DEFINITION LAYER (0.17.3 make-over,
// card t_f47319d2 - the backend half of the two-card flip). The
// agents/<name>/config/config.yaml file is the whole definition of an
// agent; this file is its read, its write, and the laws both enforce:
//
//  1. FIELD SET - name (the directory, identity set at create),
//     description, system prompt, provider, model name, max turns,
//     toggles, skills. Nothing else is agent state.
//  2. SECRET-FREE PROVIDER - an agent names a provider INSTANCE that
//     settings already defines (type, host and api_key live there,
//     once). provider.instance is the only provider reference an agent
//     file may carry: a host, an api_key or a type written into an
//     agent file is refused on write and refused at wake. The unset
//     verb still deletes the three legacy keys, so a file written
//     before this law can be scrubbed without an editor.
//  3. ALWAYS OFF - the toggles list is the ENABLED set, and an empty
//     list enables nothing. A secondary agent never inherits main's
//     feature set. The locked pair (agent-loop + provider) is not a
//     toggle at all: it is not in the toggleable tier, so no agent can
//     turn it off, and naming it in toggles is refused as noise.
//  4. SUBSET - an agent can only enable what its host has enabled.
//     Enabling a capsule the host's disabled: fold names is refused;
//     the runtime mount intersects the two sets anyway.
//  5. SKILLS ARE AN EXPLICIT ALLOWLIST over the visible registry, and
//     empty means NO skills. Never silently inherited.
//  6. INHERIT-AS-DISABLED - main's skills, soul, user.md and memory.md
//     are VISIBLE to a secondary agent but inert until the agent
//     enables that kind. Enabling MATERIALIZES a copy into the agent
//     home (the clone law's philosophy: no live links), and never
//     clobbers a file the agent already owns.
//  7. SOUL.MD - a personality/behaviour file beside user.md and
//     memory.md, one per agent (profiles.go owns the home shape).
//  8. CLONE - a materialized full copy at birth (profiles.go).
//  9. NAME - identity, set at create; there is no rename machinery.
// 10. DELETE = delete the HOME - the definition, the persona files, the
//     agent's own skills and its own session store go together, so no
//     half-deleted folder keeps registering an agent. The main agent's
//     home is the daemon's home and never deletes.
//
// The write is node-preserving: the file is parsed as a yaml.Node and
// only the named fields are upserted, so comments, quoting and keys this
// layer does not own survive byte-intact.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// ErrAgentHomeLocked marks the main agent's DELETE: its home is the
// daemon's home (the config, the store, the whole running instance), so
// the delete verb refuses it. The endpoint maps it to 409.
var ErrAgentHomeLocked = errors.New("the main agent's home is the daemon's home - it cannot be deleted; delete a secondary agent home instead")

// ErrAgentNotExist marks a delete whose target home is not on disk. The
// endpoint maps it to 404 - an unknown agent is the caller's miss, never
// a server fault, and deleting an absent home is not "already done".
var ErrAgentNotExist = errors.New("unknown agent")

// AgentConfigScaffoldError marks a caller's-mistake field (an unknown
// provider instance, a toggle the host has off): the 400 lane, not a
// 500.
type AgentConfigScaffoldError struct{ Message string }

func (e *AgentConfigScaffoldError) Error() string { return e.Message }

// MaskedSecretPlaceholder is the mask the read lanes put in place of a
// stored provider.api_key (cmd/omega's maskedSecret, the settings
// payload's secret knob). It is NOT a secret: a UI echoing the mask
// back is refused rather than persisted. The agent package cannot
// import the host's constant (layer law), so the literal is mirrored
// here and a cmd/omega test pins the two strings equal.
const MaskedSecretPlaceholder = "****"

// DefaultProviderInstance is the reserved instance id that names the
// host's own provider: section - the instance an agent gets when it
// references none. It mirrors the reserved name the host config refuses
// as a NAMED instance (cmd/omega Config.Validate), so one word cannot
// mean two endpoints.
const DefaultProviderInstance = "default"

// AlwaysOnAgentModules is the always-on tier, under the names the mount
// machinery knows (0.17.3 tier re-law, card t_ee3a968b: agent-loop +
// provider + logging, the folded observability module). None is in the
// toggleable tier (agent/plugin.go), so none can be switched off by an
// agent or by the host - the law is structural - and naming one in
// toggles is refused so a file never claims a choice it does not have.
var AlwaysOnAgentModules = []string{"agent-loop", "provider", "observability"}

// AgentInherit is the inherit-as-disabled block: main's artifacts are
// visible to every secondary agent and inert until the matching flag is
// on. An absent block, or a flag the block omits, reads off.
type AgentInherit struct {
	Skills bool `yaml:"skills" json:"skills"`
	Soul   bool `yaml:"soul" json:"soul"`
	User   bool `yaml:"user" json:"user"`
	Memory bool `yaml:"memory" json:"memory"`
}

// Any reports whether at least one artifact kind is enabled.
func (i AgentInherit) Any() bool { return i.Skills || i.Soul || i.User || i.Memory }

// AgentConfigUpdates is the PUT /agents/{name} write set, one field per
// item of the FIELD SET. A nil field is untouched - the endpoint writes
// only what the request carries.
//
// MaxTurns writes a yaml INT node: the layer's max_turns is
// unmarshalled straight into Config.MaxTurns int at the next wake, so a
// quoted "42" would be a boot error, and 0 ("no cap") is a real value:
// nil is what means unset.
//
// ProviderType, Host and APIKey are RETIRED knobs kept as explicit
// refusals: a client that still sends one gets the secret-free law
// spelled out (400) instead of a silent drop that would leave the
// operator believing the file changed.
//
// Unset is the reset verb: each entry names a knob in the SAME dotted
// vocabulary the settings scope payload uses, and its key pair is
// DELETED from the layer file instead of being written down with main's
// value. That difference is the whole point - the scope payload flags a
// knob "overridden" structurally (the file names the key), so a
// reset-by-write would leave a permanent phantom override. Deleting is
// the only reset that falls back to main.
type AgentConfigUpdates struct {
	Description      *string
	SystemPrompt     *string
	ProviderInstance *string
	ModelName        *string
	MaxTurns         *int
	Toggles          *[]string
	SkillsAllow      *[]string
	Inherit          *AgentInherit
	Unset            []string

	// Retired: refused on every write (the secret-free provider law).
	ProviderType *string
	Host         *string
	APIKey       *string
}

// ScaffoldUnsetKeys is the dotted vocabulary `unset` accepts: every
// scalar knob of the field set, spelled the way the settings scope
// payload spells it (cmd/omega/settings.go scopeGroups). An entry
// outside it is refused - the wire names a real layer key, never a
// guess at one. The legacy secret keys stay in the vocabulary on
// purpose: deleting them is the scrub verb, writing them is refused.
var ScaffoldUnsetKeys = []string{
	"description",
	"system_prompt",
	"provider.instance",
	"provider.model_name",
	"provider.type",
	"provider.host",
	"provider.api_key",
	"max_turns",
	"toggles",
	"skills.allow",
	"inherit",
}

// updatesSetKey reports whether this request also SETS the dotted key -
// the set/unset conflict check. A key both written and deleted in one
// request has two answers and no ordering that is obviously right, so it
// is refused loudly rather than resolved silently.
func updatesSetKey(u AgentConfigUpdates, key string) bool {
	switch key {
	case "description":
		return u.Description != nil
	case "system_prompt":
		return u.SystemPrompt != nil
	case "provider.instance":
		return u.ProviderInstance != nil
	case "provider.model_name":
		return u.ModelName != nil
	case "provider.type":
		return u.ProviderType != nil
	case "provider.host":
		return u.Host != nil
	case "provider.api_key":
		return u.APIKey != nil
	case "max_turns":
		return u.MaxTurns != nil
	case "toggles":
		return u.Toggles != nil
	case "skills.allow":
		return u.SkillsAllow != nil
	case "inherit":
		return u.Inherit != nil
	}
	return false
}

// HostDefinition is what the HOST allows an agent to reference: the
// named provider instances settings defines, and the host's own
// disabled: fold (the enabled set an agent's toggles must stay inside).
// Read from the home config, which may live at the settled
// config/config.yaml or the legacy home root.
type HostDefinition struct {
	ProviderInstances []string
	DisabledModules   []string
	DisabledSkills    []string
	DisabledTools     []string
	DisabledAgents    []string
}

// HostEnabledModule is the subset law's single predicate: the host runs
// the capsule unless its fold names it off.
func (h HostDefinition) HostEnabledModule(name string) bool {
	return !slices.Contains(h.DisabledModules, name)
}

// HostEnabledSkill reports whether the host has a skill switched on.
func (h HostDefinition) HostEnabledSkill(name string) bool {
	return !slices.Contains(h.DisabledSkills, name)
}

// HostConfigPath resolves the host's config file for the home: the
// settled layout first (config/config.yaml), the legacy home root
// second. Empty when neither exists.
func HostConfigPath(home string) string {
	for _, p := range []string{
		filepath.Join(home, "config", "config.yaml"),
		filepath.Join(home, "config.yaml"),
	} {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// LoadHostDefinition reads the host config's provider instances and
// disabled: fold. A home with no config file answers the zero
// definition: validation then refuses every instance reference loudly,
// which is the truth - an agent cannot reference settings that do not
// exist.
func LoadHostDefinition(home string) (HostDefinition, error) {
	var host HostDefinition
	path := HostConfigPath(home)
	if path == "" {
		return host, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return host, fmt.Errorf("read host config: %w", err)
	}
	var raw struct {
		Providers map[string]struct {
			Type   string `yaml:"type"`
			Host   string `yaml:"host"`
			APIKey string `yaml:"api_key"`
		} `yaml:"providers"`
		Disabled yaml.Node `yaml:"disabled"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return host, fmt.Errorf("parse host config: %w", err)
	}
	for name := range raw.Providers {
		host.ProviderInstances = append(host.ProviderInstances, name)
	}
	slices.Sort(host.ProviderInstances)
	// The disabled: fold carries two legal shapes (cmd/omega's
	// DisabledNamespaces): the namespaced mapping this version writes,
	// and the legacy flat sequence of module names. Both are host truth.
	if raw.Disabled.Kind != 0 {
		modules, skills, tools, agents, err := parseDisabledFold(&raw.Disabled)
		if err != nil {
			return host, fmt.Errorf("host config %s: %w", path, err)
		}
		host.DisabledModules, host.DisabledSkills, host.DisabledTools, host.DisabledAgents = modules, skills, tools, agents
	}
	return host, nil
}

// disabledFold is the namespaced shape of the host config's disabled:
// fold. NAMED, not inline: an anonymous struct spells its yaml tags into
// the type symbols the linker emits, and this fold's tags name three
// capsules (skills, tools, agents) - see agentLayerFile for the law
// (card t_61ad4490, pinned by symbolvocab_test.go).
type disabledFold struct {
	Modules []string `yaml:"modules"`
	Skills  []string `yaml:"skills"`
	Tools   []string `yaml:"tools"`
	Agents  []string `yaml:"agents"`
}

// parseDisabledFold decodes one disabled: node in either shape. A node
// of neither shape, or one that will not decode, is an ERROR: a fold
// that silently reads as "nothing disabled" would validate agent writes
// against a phantom everything-enabled host - the wrong direction for a
// law that refuses what the host has switched off.
func parseDisabledFold(node *yaml.Node) (modules, skills, tools, agents []string, err error) {
	if node.Kind == yaml.SequenceNode {
		var flat []string
		if err := node.Decode(&flat); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("disabled fold: %w", err)
		}
		return flat, nil, nil, nil, nil
	}
	if node.Kind != yaml.MappingNode {
		return nil, nil, nil, nil, fmt.Errorf("disabled fold: node kind %d is neither a mapping nor a sequence", node.Kind)
	}
	var fold disabledFold
	if err := node.Decode(&fold); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("disabled fold: %w", err)
	}
	return fold.Modules, fold.Skills, fold.Tools, fold.Agents, nil
}

// LayerValue is one string knob as the layer file gives it: the value
// and whether the file names the key at all. An explicitly empty value
// is a real override ("the agent set it empty" is not "inherit").
type LayerValue struct {
	Value string
	Set   bool
}

// AgentConfigLayer is the presence-aware read of one agent's layer
// file: every knob of the field set, plus whether the file names it at
// all. A knob the file leaves commented out or absent reads Set=false -
// the scaffold template comments every knob, so an unedited agent
// overrides nothing.
type AgentConfigLayer struct {
	Description      LayerValue
	SystemPrompt     LayerValue
	ProviderInstance LayerValue
	ModelName        LayerValue
	MaxTurns         *int
	// Toggles is the ENABLED capsule set. Empty (or absent) is the
	// always-off default, never "inherit main's set".
	Toggles    []string
	TogglesSet bool
	// SkillsAllow is the explicit allowlist. Empty is NO skills.
	SkillsAllow    []string
	SkillsAllowSet bool
	Inherit        AgentInherit
	InheritSet     bool
	// LegacySecrets names the secret keys this file still carries. The
	// VALUES never leave the read: the lane reports the law violation,
	// never the secret.
	LegacySecrets []string
}

// SecretFree reports whether the file honors the secret-free provider
// law.
func (l AgentConfigLayer) SecretFree() bool { return len(l.LegacySecrets) == 0 }

// agentLayerFile is the presence-aware decode of one agent layer file.
// Pointers carry presence: nil = the key is absent, a pointer to "" = the
// file explicitly set the knob empty. MaxTurns is decoded loosely on
// purpose: a hand-quoted "42" must not make the whole layer unreadable -
// the write path exists to correct exactly that node.
//
// It is a NAMED type, never an inline `var raw struct { ... }`. The
// compiler spells an ANONYMOUS struct's field tags into the type
// descriptor and equality symbols it emits for it, and go1.26 prints them
// whole: `type:.eq.struct { ... Provider *struct { ... } "yaml:\"provider\"" ... }`.
// A capsule noun in a host-owned symbol survives that capsule's removal -
// the nm-purity red this file filed (card t_61ad4490: the provider
// rotation failed on master while the scrub and the build were clean).
// A named type's symbol is its name, so the tags never reach the binary.
type agentLayerFile struct {
	Description  *string                  `yaml:"description"`
	SystemPrompt *string                  `yaml:"system_prompt"`
	MaxTurns     any                      `yaml:"max_turns"`
	Provider     *agentLayerProviderBlock `yaml:"provider"`
	Toggles      *[]string                `yaml:"toggles"`
	Skills       *agentLayerSkillsBlock   `yaml:"skills"`
	Inherit      *AgentInherit            `yaml:"inherit"`
}

// agentLayerProviderBlock is the layer's provider block: the instance
// reference this law allows, its model name, and the three RETIRED
// secret keys, read for PRESENCE only - their values never leave the
// read.
type agentLayerProviderBlock struct {
	Instance  *string `yaml:"instance"`
	ModelName *string `yaml:"model_name"`
	Type      *string `yaml:"type"`
	Host      *string `yaml:"host"`
	APIKey    *string `yaml:"api_key"`
}

// agentLayerSkillsBlock is the layer's skills block: the explicit
// allowlist over the visible registry.
type agentLayerSkillsBlock struct {
	Allow *[]string `yaml:"allow"`
}

// ReadAgentConfigLayer reads one agent's layer with presence. A missing
// (or empty) file is the empty layer - a scaffolded agent whose config
// was deleted still reads, it simply enables nothing. A file that will
// not parse is a loud error: reporting half-parsed values would make
// every override flag a guess.
//
// The DEFAULT agent refuses here: its "layer" is the home config, so
// reading it would report the daemon's own knobs as agent overrides. It
// is not a scope (its home is the home root).
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
	var raw agentLayerFile
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
		set(&lay.ProviderInstance, raw.Provider.Instance)
		set(&lay.ModelName, raw.Provider.ModelName)
		// Presence only - the values are dropped on the floor.
		for key, present := range map[string]bool{
			"provider.api_key": raw.Provider.APIKey != nil,
			"provider.host":    raw.Provider.Host != nil,
			"provider.type":    raw.Provider.Type != nil,
		} {
			if present {
				lay.LegacySecrets = append(lay.LegacySecrets, key)
			}
		}
		slices.Sort(lay.LegacySecrets)
	}
	lay.MaxTurns = coerceTurns(raw.MaxTurns)
	if raw.Toggles != nil {
		lay.Toggles, lay.TogglesSet = normalizeNames(*raw.Toggles), true
	}
	if raw.Skills != nil && raw.Skills.Allow != nil {
		lay.SkillsAllow, lay.SkillsAllowSet = normalizeNames(*raw.Skills.Allow), true
	}
	if raw.Inherit != nil {
		lay.Inherit, lay.InheritSet = *raw.Inherit, true
	}
	return lay, nil
}

// coerceTurns reads max_turns from its decoded YAML value: an int is the
// write path's own shape, a numeric string is a hand edit (the write
// path corrects it to an int node), and anything else means unset.
func coerceTurns(v any) *int {
	switch t := v.(type) {
	case int:
		return &t
	case int64:
		n := int(t)
		return &n
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(t)); err == nil {
			return &n
		}
	}
	return nil
}

// normalizeNames trims, drops empties and duplicates, and sorts - the
// toggles list and the skills allowlist are SETS, so two files that name
// the same entries read byte-identically.
func normalizeNames(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	slices.Sort(out)
	return out
}

// validateAgentUpdates is the refusal lane: every caller's-mistake field
// answers AgentConfigScaffoldError (the endpoint's 400), never a half
// write. host is the settings truth the request must respect; visible is
// the skill registry the agent can see (own + shared + main's own when
// inherit.skills is on).
func validateAgentUpdates(u AgentConfigUpdates, host HostDefinition, visible []string) error {
	// The secret-free law: a secret written into an agent file is a law
	// violation, not a field. Name the endpoint that actually owns it so
	// the caller has a next step.
	for _, retired := range []struct {
		key   string
		value *string
	}{
		{"provider.type", u.ProviderType},
		{"provider.host", u.Host},
		{"provider.api_key", u.APIKey},
	} {
		if retired.value == nil {
			continue
		}
		if strings.TrimSpace(*retired.value) == MaskedSecretPlaceholder && retired.key == "provider.api_key" {
			return &AgentConfigScaffoldError{Message: fmt.Sprintf("%s is the read mask, not a secret - agent configs hold no secrets at all", retired.key)}
		}
		return &AgentConfigScaffoldError{Message: fmt.Sprintf("%s is not an agent field - agent configs are secret-free: provider type, host and api_key are defined once in settings; reference an instance with provider.instance (unset: [\"%s\"] scrubs a legacy value)", retired.key, retired.key)}
	}
	if u.ProviderInstance != nil {
		inst := strings.TrimSpace(*u.ProviderInstance)
		if inst != "" && inst != DefaultProviderInstance && !slices.Contains(host.ProviderInstances, inst) {
			known := "none configured"
			if len(host.ProviderInstances) > 0 {
				known = strings.Join(host.ProviderInstances, ", ")
			}
			return &AgentConfigScaffoldError{Message: fmt.Sprintf("provider instance %q is not defined in settings (want one of: %s, or %q for the host's own provider)", inst, known, DefaultProviderInstance)}
		}
	}
	if u.MaxTurns != nil && *u.MaxTurns < 0 {
		return &AgentConfigScaffoldError{Message: fmt.Sprintf("max_turns %d is negative (want >= 0; 0 means no cap, omit the knob to inherit)", *u.MaxTurns)}
	}
	// The always-off and subset laws, one entry at a time.
	if u.Toggles != nil {
		for _, name := range *u.Toggles {
			name = strings.TrimSpace(name)
			if name == "" {
				return &AgentConfigScaffoldError{Message: "toggles holds an empty name"}
			}
			if slices.Contains(AlwaysOnAgentModules, name) {
				return &AgentConfigScaffoldError{Message: fmt.Sprintf("%q is in the always-on tier (agent-loop + provider + logging) - it is always on and cannot be toggled", name)}
			}
			if err := ValidAgentName(name); err != nil {
				return &AgentConfigScaffoldError{Message: fmt.Sprintf("toggle %q is not a capsule name: %v", name, err)}
			}
			if !host.HostEnabledModule(name) {
				return &AgentConfigScaffoldError{Message: fmt.Sprintf("toggle %q is off on the host - an agent can only enable what its host has enabled (subset law)", name)}
			}
		}
	}
	// The allowlist names real, visible skills: a typo must not silently
	// enable nothing forever, and the subset law binds here exactly as it
	// does for toggles - a skill the host switched off is refused loudly
	// at write, never accepted and then silently dropped at wake.
	if u.SkillsAllow != nil {
		for _, name := range *u.SkillsAllow {
			name = strings.TrimSpace(name)
			if name == "" {
				return &AgentConfigScaffoldError{Message: "skills.allow holds an empty name"}
			}
			if !slices.Contains(visible, name) {
				known := "none visible"
				if len(visible) > 0 {
					known = strings.Join(visible, ", ")
				}
				return &AgentConfigScaffoldError{Message: fmt.Sprintf("skill %q is not in this agent's visible registry (want one of: %s)", name, known)}
			}
			if !host.HostEnabledSkill(name) {
				return &AgentConfigScaffoldError{Message: fmt.Sprintf("skill %q is off on the host - an agent can only enable what its host has enabled (subset law)", name)}
			}
		}
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

// UpdateAgentConfigLayer upserts the definition layer into
// agents/<name>/config/config.yaml (the agent's own layer - NEVER the
// home config: the DEFAULT agent refuses here, its config IS the home
// config and the agents surface renders it read-only). The write is
// atomic; untouched keys keep their comments. Enabling an inherit kind
// materializes main's artifact into the agent home, so the flag can
// never read as on while the artifact is missing.
func UpdateAgentConfigLayer(home, name string, u AgentConfigUpdates) error {
	if name == DefaultAgentName {
		return ErrAgentConfigLocked
	}
	host, err := LoadHostDefinition(home)
	if err != nil {
		return err
	}
	current, err := ReadAgentConfigLayer(home, name)
	if err != nil {
		return err
	}
	// The inherit block this request leaves in place decides whether
	// main's skills are visible to the allowlist being written.
	inherit := current.Inherit
	if u.Inherit != nil {
		inherit = *u.Inherit
	}
	if err := validateAgentUpdates(u, host, VisibleSkillNames(home, name, inherit.Skills)); err != nil {
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
	if u.ProviderInstance != nil || u.ModelName != nil {
		prov := ensureMapping(mapping, "provider")
		if u.ProviderInstance != nil {
			upsertScalar(prov, "instance", strings.TrimSpace(*u.ProviderInstance))
		}
		if u.ModelName != nil {
			upsertScalar(prov, "model_name", strings.TrimSpace(*u.ModelName))
		}
	}
	if u.MaxTurns != nil {
		// An INT node, never a string: ApplyAgentLayer unmarshals this
		// key straight into Config.MaxTurns int, so `max_turns: "42"`
		// would be the next wake's boot error.
		upsertInt(mapping, "max_turns", *u.MaxTurns)
	}
	if u.Toggles != nil {
		upsertSequence(mapping, "toggles", normalizeNames(*u.Toggles))
	}
	if u.SkillsAllow != nil {
		skillsMap := ensureMapping(mapping, "skills")
		upsertSequence(skillsMap, "allow", normalizeNames(*u.SkillsAllow))
	}
	if u.Inherit != nil {
		block := ensureMapping(mapping, "inherit")
		upsertBool(block, "skills", u.Inherit.Skills)
		upsertBool(block, "soul", u.Inherit.Soul)
		upsertBool(block, "user", u.Inherit.User)
		upsertBool(block, "memory", u.Inherit.Memory)
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
	if err := WriteFileAtomic(path, out, 0o644); err != nil {
		return err
	}
	// Materialize whatever the file now enables. A copy that fails is
	// reported: an enabled flag with no artifact would be a lie the
	// agent runs on.
	final, err := ReadAgentConfigLayer(home, name)
	if err != nil {
		return err
	}
	return MaterializeInheritance(home, name, final)
}

// DeleteAgentHome removes one secondary agent's HOME as one verb: the
// definition file, the persona files, the agent's own skills and its own
// session store (data/omega.db) all go together. Ruling 2 of the 0.17.3
// make-over: delete means delete the home, sessions included - a
// half-deleted agent that still registers by folder presence would be an
// orphan home, and folder presence IS registration (profiles.go).
//
// The main agent refuses (ErrAgentHomeLocked): its home is the daemon's
// home. A name with no home answers ErrAgentNotExist, and a malformed
// name answers AgentConfigScaffoldError (the 400 lane) - the target is
// re-derived from AgentDir and asserted to sit exactly at
// <home>/agents/<name>, because a delete verb is the one place a path
// bug cannot be undone.
//
// Callers that need the session count for their confirm dialog read it
// BEFORE calling this (the counts are store business, and this package
// stays store-free - profiles.go).
func DeleteAgentHome(home, name string) error {
	if name == DefaultAgentName {
		return ErrAgentHomeLocked
	}
	if err := ValidAgentName(name); err != nil {
		return &AgentConfigScaffoldError{Message: fmt.Sprintf("delete agent: %v", err)}
	}
	dir := AgentDir(home, name)
	// Containment: the target must be a DIRECT child of <home>/agents/,
	// named exactly by the validated name. AgentDir already guarantees
	// that shape, so this only fires if a future change moves it.
	root := filepath.Join(home, "agents")
	if filepath.Dir(dir) != root || filepath.Base(dir) != name {
		return &AgentConfigScaffoldError{Message: fmt.Sprintf("delete agent %q: refusing a target outside %s", name, root)}
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return ErrAgentNotExist
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("delete agent %q: %w", name, err)
	}
	return nil
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

// docOf seats m inside a fresh document node.
func docOf(m *yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{m}}
}

func mappingNode() *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode} }

// upsertScalar sets (or appends) a scalar string value under key,
// mutating the mapping's Content in place (yaml.v3 stores mapping
// pairs as key/value node pairs inside one Content slice).
func upsertScalar(m *yaml.Node, key, value string) { upsertTagged(m, key, "!!str", value) }

// upsertBool sets (or appends) a BOOL value under key.
func upsertBool(m *yaml.Node, key string, value bool) {
	upsertTagged(m, key, "!!bool", strconv.FormatBool(value))
}

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

// upsertSequence replaces (or appends) a string sequence under key. An
// empty slice renders `key: []` - the explicit zero state, which is a
// real answer for toggles and for the skills allowlist (always off).
func upsertSequence(m *yaml.Node, key string, values []string) {
	replacement := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	for _, v := range values {
		replacement.Content = append(replacement.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v})
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if k := m.Content[i]; k.Kind == yaml.ScalarNode && k.Value == key {
			v := m.Content[i+1]
			// The node keeps its identity (and its comments); only its
			// shape and payload are replaced.
			v.Kind, v.Tag, v.Value = yaml.SequenceNode, "!!seq", ""
			v.Content, v.Style = replacement.Content, replacement.Style
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		replacement)
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
