package core

import (
	"context"
	"time"
)

// ToolProvider supplies tools to the agent.
type ToolProvider interface {
	Tools() map[string]Tool
}

// DefaultToolProvider wraps a static tool map.
type DefaultToolProvider struct {
	ToolsMap map[string]Tool
}

// Tools returns the tool map.
func (d DefaultToolProvider) Tools() map[string]Tool { return d.ToolsMap }

// PromptSectioner is the additive prompt-section seam.
type PromptSectioner interface {
	// PromptSections returns the sections this module contributes for
	// this build. Returns nil when it has nothing to render.
	PromptSections(opts PromptBuildOptions) []PromptSection
}

// LiveSeams is the optional Channel capability (v0.12 E2). A channel that serves
// nothing module-dependent (or cannot) does not implement it and the host skips it.
type LiveSeams interface {
	// RewireSeams re-reads the module seams from the live Context.
	RewireSeams(ctx *Context) error
}

// Channel is a delivery transport that connects external clients to the agent.
type Channel interface {
	// Start runs the channel until the context is cancelled or Stop is called.
	Start(ctx context.Context, deps ChannelDeps) error
	// Stop shuts the channel down.
	Stop() error
}

// SettingsKnob is one exposed config field.
type SettingsKnob struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Type  string `json:"type"`
	Value any    `json:"value"`
	// Default is the knob's effective default. It rides EVERY knob with NO omitempty,
	// because the settings pane must tell "no default known" apart from default=false / ""
	// / 0; a knob with no locatable default therefore answers JSON null. (settings tier
	// 2a); the pane's modified-dot and reset-to-default (tier 2b) compare against exactly
	// this field.
	Default any      `json:"default"`
	Options []string `json:"options,omitempty"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Step    *float64 `json:"step,omitempty"`
	Help    string   `json:"help,omitempty"`
	Live    bool     `json:"live,omitempty"`
	Filter string `json:"filter,omitempty"`
	Slot bool `json:"slot,omitempty"`
	// DefaultOption marks an AUXILIARY model knob whose picker offers the "default" entry:
	// the aux lane then follows whatever model the current run serves (the composer's
	// chip), resolved at call time - the choice persists as the literal word, never frozen
	// to a model name.
	DefaultOption bool `json:"default_option,omitempty"`
}

// SettingsGroup is one config section: a labeled set of knobs.
type SettingsGroup struct {
	Key   string         `json:"key"`
	Label string         `json:"label"`
	Knobs []SettingsKnob `json:"knobs"`
}

// SettingsSchema is the JSON-marshalable settings payload.
type SettingsSchema struct {
	Groups []SettingsGroup `json:"groups"`
	// Scopes lists the per-agent settings scopes. The field always rides the wire and is
	// never null - an empty list is the R8 empty world (no agent dirs), where the pane
	// renders no scope strip at all. Read-only: every write still lands on the agents REST
	// edit endpoint (R4), never on this payload.
	Scopes []SettingsScope `json:"scopes"`
}

// SettingsScope is one agent scope in the settings payload.
type SettingsScope struct {
	Name   string               `json:"name"`
	Groups []SettingsScopeGroup `json:"groups"`
	// Error names a layer file that could not be read or parsed: the scope still rides (R8
	// - every agent dir is a scope, a broken one must not vanish from the strip) with
	// inherited values and no override flags, and this field is what tells the pane the
	// flags are not the file's truth.
	Error string `json:"error,omitempty"`
}

// SettingsScopeGroup is one overridable knob group inside a scope - the four R2 groups,
// in render order.
type SettingsScopeGroup struct {
	Key        string              `json:"key"`
	Overridden bool                `json:"overridden"`
	Knobs      []SettingsScopeKnob `json:"knobs"`
}

// SettingsScopeKnob is one knob inside a scope. Secrets ride masked exactly like the
// main payload.
type SettingsScopeKnob struct {
	Key        string `json:"key"`
	Value      any    `json:"value"`
	Overridden bool   `json:"overridden"`
}

// Knob looks one knob up by its dotted key. The schema is the contract, so every
// one-knob reader locates its knob here instead of walking the groups itself: the
// channel's `?key=` read and the CLI's `omega settings get` address the same knob
// through this method, and an unknown key is one answer, not two. The returned pointer
// aliases the schema's own knob - readers must not mutate it.
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

// Settings is the runtime config knobs seam. Channels that host a UI (the webui
// settings pane lives behind the HTTP channel's /settings) use it to read the host's
// config knobs and save changes - the host owns the schema, validation, persistence,
// and hot-apply; the channel never touches config.yaml itself. Nil when the host does
// not expose settings (endpoints then answer 501).
type Settings interface {
	// Schema returns the settings payload. Secret values MUST be masked - the plain text
	// never rides the wire.
	Schema() (*SettingsSchema, error)
	// Save applies values (map of dotted yaml path -> JSON value), validates, persists.
	Save(values map[string]any) (*SettingsSchema, error)
	// Raw returns the config FILE as YAML text. Secret values are masked exactly like
	// Schema masks them - the file is the secret's home, so the wire never carries one.
	Raw() (string, error)
	// SaveRaw validates a whole YAML document through the same LoadConfig path Save.
	SaveRaw(text string) (*SettingsSchema, error)
	// Reset restores every knob that HAS a default to it through Save's own write path.
	// The config.yaml-only doors (provider.api_key, server.bind, the named instances and
	// their secrets) and knobs with no known default (provider.model_name - config
	// requires it) are kept: resetting is never destructive.
	Reset() (*SettingsSchema, error)
}

// ModuleStatus is one module in the registry introspection (v0.12 E2, the
// settings-toggle small law.
type ModuleStatus struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Provides    []string `json:"provides,omitempty"`
	Requires    []string `json:"requires,omitempty"`
	State       string   `json:"state"` // "mounted" | "disabled"
	Toggleable  bool     `json:"toggleable"`
	Dependents  []string `json:"dependents,omitempty"`
}

// ModuleToggles is the module-toggle seam (v0.12 E2). Toggle goes through the same
// mount machinery as boot (Q7: one code path) and rejects loudly (Q11); a host that
// does not expose toggles is nil and endpoints answer 501.
type ModuleToggles interface {
	// Modules returns every module in the build's registry catalog with its live state
	// (shell modules never appear.
	Modules() []ModuleStatus
	// Toggle enables or disables one module at runtime. A rejection (tier law, Q3 edge,
	// roll-back panic) returns an error naming the module and the failed check.
	Toggle(ctx context.Context, name string, enabled bool) error
	// Refusals returns the recorded boundary rejections (Q12).
	Refusals() []Refusal
}

// DisabledNames is the registry-browser toggle state (v0.14.1).
type DisabledNames struct {
	Skills []string `json:"skills"`
	Tools  []string `json:"tools"`
	Agents []string `json:"agents"`
}

// RegistryToggles is the registry-browser toggle seam (v0.14.1). A name the owning
// registry does not know returns an error - silence would persist a name toggle UIs can
// never show or clear.
type RegistryToggles interface {
	Disabled() DisabledNames
	// ToggleSkill enables/disables one skill (prompt-build exclusion).
	ToggleSkill(name string, enabled bool) error
	// ToggleTool enables/disables one tool (registry exclusion).
	ToggleTool(name string, enabled bool) error
	// ToggleAgent soft-disables one agent (roster + wake exclusion, the home untouched).
	ToggleAgent(name string, enabled bool) error
}

// UnknownName marks a toggle error whose name no registry knows - the
// 404 lane. The error contract for the serving surface: a refused name
// must be distinguishable from a failed persistence write.
type UnknownName interface {
	Unknown() bool
}

// ChannelDeps carries everything a channel needs to run.
type ChannelDeps struct {
	Ctx      *Context
	Store    any
	Settings Settings
	Modules  ModuleToggles
	// Registries (optional) exposes the registry-browser toggle seam. Nil and the
	// /disabled endpoints answer 501, like the module seam.
	Registries RegistryToggles
	Version    string
}

// QuestionPrompt is one agent-issued question card as it rides the wire and the
// transcript surfaces.
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

// QuestionAnswer is the recorded answer to one QuestionPrompt. Exactly one of the five
// applies.
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

// Kind labels the answer for tool results and wire payloads.
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

// WithRunOrigin tags the run context with its launching surface.
func WithRunOrigin(ctx context.Context, origin string) context.Context {
	if origin == "" {
		return ctx
	}
	return context.WithValue(ctx, runOriginKey{}, origin)
}

// RunOriginFromContext returns the run's origin tag ("" when unset - callers treat.
func RunOriginFromContext(ctx context.Context) string {
	origin, _ := ctx.Value(runOriginKey{}).(string)
	return origin
}

type runOriginKey struct{}

// runScope is one run's per-session working-folder override.
type runScope struct {
	CWD           string
	PromptContext string
}

type runScopeKey struct{}

// RunWorkspace returns the run's session workspace root ("" = none.
func RunWorkspace(ctx context.Context) string {
	scope, _ := ctx.Value(runScopeKey{}).(runScope)
	return scope.CWD
}

// RunPromptContext returns the run's trust-gated project context override ("" = unset.
func RunPromptContext(ctx context.Context) string {
	scope, _ := ctx.Value(runScopeKey{}).(runScope)
	return scope.PromptContext
}

// WithRunWorkspace returns a child context carrying the run's session workspace root.
// Values are run-scoped: each Run caller wraps its own context, so parallel runs never
// share one.
func WithRunWorkspace(ctx context.Context, dir, promptContext string) context.Context {
	if dir == "" {
		return ctx
	}
	return context.WithValue(ctx, runScopeKey{}, runScope{CWD: dir, PromptContext: promptContext})
}

// -- the run output tap ------------------ A tool that produces output OVER TIME (the.
// Unset = no streaming, exactly the pre-tap behavior.
type ToolOutputChunk struct {
	// CallID and Tool identify the running call. The loop stamps them
	// on the per-call ctx (WithToolCall); a tool never sets them.
	CallID string
	Tool   string
	Text string
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

// The trust capsule's own consumer surfaces (the serving surface's workspace.

// Notice is one finished-thing record. The title line is METADATA ONLY (source kind +
// id + verdict) - prompt and session content never rides a notice (privacy law).
type Notice struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"` // task | cron | chat
	SourceID  string `json:"source_id"`
	Title     string `json:"title"`   // metadata line, no prompt/session content
	Verdict   string `json:"verdict"` // ok | failed
	CreatedAt string `json:"created_at"`
	Read      bool   `json:"read"`
}

// Emitter delivery is generic (mount machinery), so core carries no emitter contract at
// all.
