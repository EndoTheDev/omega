package core

import (
	"context"
	"time"
)

// ToolProvider supplies tools to the agent. Called once per Run to
// build the tool set. Extension-provided tools are merged on top.
type ToolProvider interface {
	Tools() map[string]Tool
}

// DefaultToolProvider wraps a static tool map. Used by tests to
// satisfy the ToolProvider interface with a simple map.
type DefaultToolProvider struct {
	ToolsMap map[string]Tool
}

// Tools returns the tool map.
func (d DefaultToolProvider) Tools() map[string]Tool { return d.ToolsMap }

// PromptSectioner is the additive prompt-section seam. Modules that
// own a piece of the system prompt (memory snapshot, skills list,
// tools list) contribute it as a PromptSection; the prompt builder is
// a generic assembler that renders whatever is contributed. Section
// layout order: memory 10, skills 20, tools 30 - new sections pick a
// value between them.
type PromptSectioner interface {
	// PromptSections returns the sections this module contributes for
	// this build. Returns nil when it has nothing to render.
	PromptSections(opts PromptBuildOptions) []PromptSection
}

// LiveSeams is the optional Channel capability (v0.12 E2): the host
// calls RewireSeams after every successful live module toggle, so the
// channel's optional-module surface follows the Context it was built
// from instead of mount-time pointer copies - a disabled module's
// tools, panels, and routes die without a restart, a re-enabled one's
// return. A channel that serves nothing module-dependent (or cannot)
// does not implement it and the host skips it.
type LiveSeams interface {
	// RewireSeams re-reads the module seams from the live Context:
	// the optional-module route surface, the canvas panel registry,
	// and the agent's tool providers. It returns the wiring error of
	// a re-enabled module's panels, if any - the toggle machinery has
	// already changed the Context by the time this runs.
	RewireSeams(ctx *Context) error
}

// Channel is a delivery transport that connects external clients to
// the agent. Channels are additive: multiple can run simultaneously
// (HTTP + Discord + Telegram). The host starts all mounted channels
// after MountAllAPI and stops them on shutdown.
//
// Start blocks until the channel stops or the context is cancelled.
// Each channel creates agents from deps.Ctx as needed — HTTP reuses
// one agent; a chat-platform channel may create one per conversation.
type Channel interface {
	// Start runs the channel until the context is cancelled or Stop
	// is called. Blocks.
	Start(ctx context.Context, deps ChannelDeps) error
	// Stop shuts the channel down. Called by the host after the
	// context is cancelled.
	Stop() error
}

// SettingsKnob is one exposed config field. Type drives the webui
// control (toggle, number, text, secret, select, textarea, bindlist,
// model). Live marks knobs the hot-apply path applies without a
// restart.
type SettingsKnob struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Type  string `json:"type"`
	Value any    `json:"value"`
	// Default is the knob's effective default: the value it takes when
	// config.yaml names no key (the built-in defaults with the OMEGA_*
	// env overrides applied - the layer beneath the file). It rides
	// EVERY knob with NO omitempty, because the settings pane must tell
	// "no default known" apart from default=false / "" / 0; a knob with
	// no locatable default therefore answers JSON null. Card
	// t_d13c7264 (settings tier 2a); the pane's modified-dot and
	// reset-to-default (tier 2b) compare against exactly this field.
	Default any      `json:"default"`
	Options []string `json:"options,omitempty"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Step    *float64 `json:"step,omitempty"`
	Help    string   `json:"help,omitempty"`
	Live    bool     `json:"live,omitempty"`
	// Filter narrows a model-type knob's dropdown to the slot's
	// capability (card t_70c28128): "decision" lists the System One
	// decision family only, "vision" lists vision-capable models only,
	// empty = the full available list. Non-model knobs leave it empty.
	Filter string `json:"filter,omitempty"`
	// Slot marks the per-subsystem model knobs (card t_fc0b2f2d):
	// the dropdown also lists the named provider instances' models as
	// "instance:model" values the call sites resolve
	// (agent.ParseModelSlot). Non-model knobs leave it empty.
	Slot bool `json:"slot,omitempty"`
	// DefaultOption marks an AUXILIARY model knob whose picker offers
	// the "default" entry: the aux lane then follows whatever model
	// the current run serves (the composer's chip), resolved at call
	// time - the choice persists as the literal word, never frozen to
	// a model name. Non-model knobs leave it empty.
	DefaultOption bool `json:"default_option,omitempty"`
}

// SettingsGroup is one config section: a labeled set of knobs.
type SettingsGroup struct {
	Key   string         `json:"key"`
	Label string         `json:"label"`
	Knobs []SettingsKnob `json:"knobs"`
}

// SettingsSchema is the JSON-marshalable settings payload: knob groups
// in config-file order with current values. Mirrors the wire shape the
// webui settings pane already consumes.
type SettingsSchema struct {
	Groups []SettingsGroup `json:"groups"`
	// Scopes lists the per-agent settings scopes (settings tier 3a,
	// card t_22a5f70a): one entry per agent home under <home>/agents/,
	// carrying the scaffold-set knob groups an agent tab renders and
	// the flags that say which values are the agent's own. The field
	// always rides the wire and is never null - an empty list is the
	// R8 empty world (no agent dirs), where the pane renders no scope
	// strip at all. Read-only: every write still lands on the agents
	// REST edit endpoint (R4), never on this payload.
	Scopes []SettingsScope `json:"scopes"`
}

// SettingsScope is one agent scope in the settings payload: an agent
// home under <home>/agents/. The main agent is NOT a scope - its home
// is the home root itself and its config stays locked (settings tier
// 3a, ruling R5).
type SettingsScope struct {
	Name   string               `json:"name"`
	Groups []SettingsScopeGroup `json:"groups"`
	// Error names a layer file that could not be read or parsed: the
	// scope still rides (R8 - every agent dir is a scope, a broken one
	// must not vanish from the strip) with inherited values and no
	// override flags, and this field is what tells the pane the flags
	// are not the file's truth. Empty on a healthy scope.
	Error string `json:"error,omitempty"`
}

// SettingsScopeGroup is one overridable knob group inside a scope - the
// four R2 groups, in render order: provider, system_prompt,
// description, max_turns. Overridden is the group roll-up: true when
// the agent's layer file names ANY knob of the group.
type SettingsScopeGroup struct {
	Key        string              `json:"key"`
	Overridden bool                `json:"overridden"`
	Knobs      []SettingsScopeKnob `json:"knobs"`
}

// SettingsScopeKnob is one knob inside a scope. Key is the SAME dotted
// path the main payload's knob carries where the two surfaces name the
// same knob (provider.type, system_prompt, max_turns...), so a scope
// row and its main row share one vocabulary and the inherited base is
// the main group's knob with that key. Value is the EFFECTIVE value:
// the layer's own when the layer names the key, main's otherwise.
// Overridden is structural - the layer file names the key (a
// commented-out scaffold line parses as absent and is not an override).
// Secrets ride masked exactly like the main payload.
type SettingsScopeKnob struct {
	Key        string `json:"key"`
	Value      any    `json:"value"`
	Overridden bool   `json:"overridden"`
}

// Knob looks one knob up by its dotted key. The schema is the contract,
// so every one-knob reader locates its knob here instead of walking the
// groups itself: the channel's `?key=` read and the CLI's
// `omega settings get` address the same knob through this method, and
// an unknown key is one answer, not two. The returned pointer aliases
// the schema's own knob - readers must not mutate it.
func (s *SettingsSchema) Knob(key string) (*SettingsKnob, bool) {
	if s == nil {
		return nil, false
	}
	for gi := range s.Groups {
		knobs := s.Groups[gi].Knobs
		for ki := range knobs {
			if knobs[ki].Key == key {
				return &knobs[ki], true
			}
		}
	}
	return nil, false
}

// Settings is the runtime config knobs seam. Channels that host a UI
// (the webui settings pane lives behind the HTTP channel's /settings)
// use it to read the host's config knobs and save changes - the host
// owns the schema, validation, persistence, and hot-apply; the channel
// never touches config.yaml itself. Nil when the host does not expose
// settings (endpoints then answer 501).
type Settings interface {
	// Schema returns the settings payload: knob groups, each knob's
	// label/type/controls, and current values. Secret values MUST be
	// masked - the plain text never rides the wire.
	Schema() (*SettingsSchema, error)
	// Save applies values (map of dotted yaml path -> JSON value),
	// validates, persists the config file, hot-applies what the
	// running process can apply, and returns the refreshed payload.
	Save(values map[string]any) (*SettingsSchema, error)
}

// ModuleStatus is one module in the registry introspection (v0.12 E2,
// the settings-toggle small law: the settings capsule RENDERS the
// registry, core provides the vocabulary). State is "mounted" or
// "disabled" (the config.yaml disabled: toggle list). Dependents names the live
// modules that require a seam this one alone provides - the Q3 edge
// semantics surfaced, so a disable confirms with what goes down too.
type ModuleStatus struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Provides    []string `json:"provides,omitempty"`
	Requires    []string `json:"requires,omitempty"`
	State       string   `json:"state"` // "mounted" | "disabled"
	Toggleable  bool     `json:"toggleable"`
	Dependents  []string `json:"dependents,omitempty"`
}

// ModuleToggles is the module-toggle seam (v0.12 E2). The host wires
// the mount registry behind it; the serving channel renders Modules
// and applies Toggle. Toggle goes through the same mount machinery as
// boot (Q7: one code path) and rejects loudly (Q11); a host that does
// not expose toggles is nil and endpoints answer 501.
type ModuleToggles interface {
	// Modules returns every module in the build's registry catalog
	// with its live state (shell modules never appear - the
	// serving surface filters them, tier law).
	Modules() []ModuleStatus
	// Toggle enables or disables one module at runtime. A rejection
	// (tier law, Q3 edge, roll-back panic) returns an error naming
	// the module and the failed check.
	Toggle(ctx context.Context, name string, enabled bool) error
	// Refusals returns the recorded boundary rejections (Q12) - the
	// toggle ledger the UI surfaces as events.
	Refusals() []Refusal
}

// DisabledNames is the registry-browser toggle state (v0.14.1): the
// three file/folder registries the webui's skills/tools/agents tabs
// toggle, each as the names list config.yaml's `disabled:` fold
// carries for it. Modules have their own seam (ModuleToggles) - this
// is the sibling for the registry lists.
type DisabledNames struct {
	Skills []string `json:"skills"`
	Tools  []string `json:"tools"`
	Agents []string `json:"agents"`
}

// RegistryToggles is the registry-browser toggle seam (v0.14.1). The
// host wires it next to ModuleToggles: write the config.yaml
// `disabled:` fold, then apply the live effect (a re-listing and the
// next session build both honor it). Every Toggle is ONE namespace
// entry: enabled=false adds, enabled=true removes. A name the owning
// registry does not know returns an error - silence would persist a
// name toggle UIs can never show or clear.
type RegistryToggles interface {
	// Disabled returns the three lists verbatim.
	Disabled() DisabledNames
	// ToggleSkill enables/disables one skill (prompt-build exclusion).
	ToggleSkill(name string, enabled bool) error
	// ToggleTool enables/disables one tool (registry exclusion).
	ToggleTool(name string, enabled bool) error
	// ToggleAgent soft-disables one agent (roster + wake exclusion,
	// the home untouched).
	ToggleAgent(name string, enabled bool) error
}

// UnknownName marks a toggle error whose name no registry knows - the
// 404 lane. The error contract for the serving surface: a refused name
// must be distinguishable from a failed persistence write.
type UnknownName interface {
	Unknown() bool
}

// ChannelDeps carries everything a channel needs to run. Ctx is the
// fully-wired plugin Context — channels create agents from it. Store
// is the offered store service (resolved by the channel from the
// universal registry, the offer under the "store" key - the `any` is
// the mount machinery's neutral carrier; the channel type-asserts it
// to its OWN consumer face). Settings (optional) exposes the config
// knobs. Version is the host's build tag, surfaced through /health.
type ChannelDeps struct {
	Ctx      *Context
	Store    any
	Settings Settings
	Modules  ModuleToggles
	// Registries (optional) exposes the registry-browser toggle seam -
	// the skills/tools/agents namespaces of the disabled: fold. Nil and
	// the /disabled endpoints answer 501, like the module seam.
	Registries RegistryToggles
	Version    string
}

// QuestionPrompt is one agent-issued question card (verdict cards
// v1) as it rides the wire and the transcript surfaces: the issuing
// tool's arguments plus the runtime identity (id, session, deadline).
// Each option carries a label and an optional description (empty = no
// options, allow-text only). Multi allows several picks in one answer;
// AllowText additionally allows a typed answer. Defaults ride the
// implementing capsule (20 minutes). Wire vocabulary, not seam
// vocabulary: the card shape survives in core so every consumer and
// the serving surface marshal the same bytes; the seam interface
// itself lives with each consumer.
type QuestionPrompt struct {
	ID        string
	SessionID string
	Title     string
	Body      string
	Options   []QuestionOption
	Multi     bool
	AllowText bool
	TimeoutS  int
	ExpiresAt time.Time
}

// QuestionAnswer is the recorded answer to one QuestionPrompt: a
// single pick (index is 1-based; Option the label), a multi pick
// (OptionIndexes every selected index, Options the labels), free
// text, a recorded skip (no silent dismissal), or the expiry firing.
// Exactly one of the five applies.
type QuestionAnswer struct {
	CardID        string
	OptionIndex   int
	Option        string
	OptionIndexes []int
	Options       []string
	Text          string
	Skipped       bool
	Expired       bool
}

// Kind labels the answer for tool results and wire payloads:
// "answer", "skip", "expired".
func (v QuestionAnswer) Kind() string {
	switch {
	case v.Expired:
		return "expired"
	case v.Skipped:
		return "skip"
	default:
		return "answer"
	}
}

// WithRunOrigin tags the run context with its launching surface. The
// verdict tool reads it to refuse background origins. The origin is a
// plain string value supplied by the launching surface ("interactive",
// "cron", "delegate" today) - data, not vocabulary.
func WithRunOrigin(ctx context.Context, origin string) context.Context {
	if origin == "" {
		return ctx
	}
	return context.WithValue(ctx, runOriginKey{}, origin)
}

// RunOriginFromContext returns the run's origin tag ("" when unset -
// callers treat that as interactive).
func RunOriginFromContext(ctx context.Context) string {
	origin, _ := ctx.Value(runOriginKey{}).(string)
	return origin
}

type runOriginKey struct{}

// runScope is one run's per-session working-folder override (card
// t_9de5b009). CWD roots the run's relative file operations (the tools
// and the prompt's CWD line); PromptContext is the trust-gated
// AGENTS.md project context of THAT folder, resolved by the channel at
// send time.
type runScope struct {
	CWD           string
	PromptContext string
}

type runScopeKey struct{}

// RunWorkspace returns the run's session workspace root ("" = none - the
// run is not rooted in a session folder). File tools resolve relative
// paths against it; the loop serves it as the prompt's CWD. The channel
// injects the value from the session's Workspace column at send time.
func RunWorkspace(ctx context.Context) string {
	scope, _ := ctx.Value(runScopeKey{}).(runScope)
	return scope.CWD
}

// RunPromptContext returns the run's trust-gated project context override
// ("" = unset - the agent's host-level prompt context applies).
func RunPromptContext(ctx context.Context) string {
	scope, _ := ctx.Value(runScopeKey{}).(runScope)
	return scope.PromptContext
}

// WithRunWorkspace returns a child context carrying the run's session
// workspace root and its trust-gated project context. An empty dir is
// the unset case: the child wraps nothing, the host-level values apply.
// Values are run-scoped: each Run caller wraps its own context, so
// parallel runs never share one.
func WithRunWorkspace(ctx context.Context, dir, promptContext string) context.Context {
	if dir == "" {
		return ctx
	}
	return context.WithValue(ctx, runScopeKey{}, runScope{CWD: dir, PromptContext: promptContext})
}

// --- the run output tap (push channel, card t_7624bab1) ------------------
//
// A tool that produces output OVER TIME (the shell) publishes the bytes
// it has written so far through this ctx-carried sink while the call is
// still running - the pending chip's streaming source. Nothing buffers
// here: the sink belongs to the serving channel, which fans the chunks
// out or drops them. A chunk is a HINT, the tool result is truth (the
// same law the socket topics follow), so a lost chunk costs nothing.
// Unset = no streaming, exactly the pre-tap behavior.
type ToolOutputChunk struct {
	// CallID and Tool identify the running call. The loop stamps them
	// on the per-call ctx (WithToolCall); a tool never sets them.
	CallID string
	Tool   string
	// Text is the newly written bytes this chunk carries.
	Text string
	// Truncated reports that the producer's own cap dropped bytes from
	// this write (the truncation-marker law: the streaming path says so
	// instead of pretending the output ended).
	Truncated bool
}

// RunOutputTap receives one live output chunk. Implementations must not
// block (the producer writes under its own buffer lock).
type RunOutputTap func(ToolOutputChunk)

type runOutputTapKey struct{}

type toolCallScope struct {
	CallID string
	Tool   string
}

type toolCallKey struct{}

// WithRunOutputTap returns a child context carrying the run's output
// tap. Values are run-scoped: each Run caller wraps its own context, so
// parallel runs never share a sink.
func WithRunOutputTap(ctx context.Context, tap RunOutputTap) context.Context {
	if tap == nil {
		return ctx
	}
	return context.WithValue(ctx, runOutputTapKey{}, tap)
}

// WithToolCall tags a tool's per-call context with the call identity the
// loop knows and the tool cannot (the tap payload's routing keys).
func WithToolCall(ctx context.Context, callID, tool string) context.Context {
	if callID == "" && tool == "" {
		return ctx
	}
	return context.WithValue(ctx, toolCallKey{}, toolCallScope{CallID: callID, Tool: tool})
}

// EmitToolOutput hands one live output chunk to the run's tap. A context
// without a tap (the CLI loop, tests) is a silent no-op - the tool never
// needs to know whether anyone is streaming.
func EmitToolOutput(ctx context.Context, text string, truncated bool) {
	tap, _ := ctx.Value(runOutputTapKey{}).(RunOutputTap)
	if tap == nil {
		return
	}
	if text == "" && !truncated {
		return
	}
	call, _ := ctx.Value(toolCallKey{}).(toolCallScope)
	tap(ToolOutputChunk{CallID: call.CallID, Tool: call.Tool, Text: text, Truncated: truncated})
}

// The trust capsule's own consumer surfaces (the serving surface's
// workspace prompt-context resolution, the approval gate, the CLI
// loop) declare their private read faces; core carries none.

// Notice is one finished-thing record (card t_5d9032d3): a kanban
// task completed, a scheduled job run finished, or a dispatched chat
// run ended. The title line is METADATA ONLY (source kind + id +
// verdict) - prompt and session content never rides a notice
// (privacy law). Wire vocabulary, not seam vocabulary: the record
// shape survives in core so every emitter and the serving surface
// marshal the same bytes; the seam interface itself lives with each
// consumer.
type Notice struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"` // task | cron | chat
	SourceID  string `json:"source_id"`
	Title     string `json:"title"` // metadata line, no prompt/session content
	Verdict   string `json:"verdict"` // ok | failed
	CreatedAt string `json:"created_at"`
	Read      bool   `json:"read"`
}

// Emitter delivery is generic (mount machinery), so core carries no
// emitter contract at all: a hook is func(svc any) over a registry
// key, and the hook's owner type-asserts the value to its own local
// interface.