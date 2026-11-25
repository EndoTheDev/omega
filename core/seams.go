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

func (d DefaultToolProvider) Tools() map[string]Tool { return d.ToolsMap }

// PromptSectioner is the additive prompt-section seam.
type PromptSectioner interface {
	// PromptSections returns this module's sections for the build, nil when it has
	// nothing to render.
	PromptSections(opts PromptBuildOptions) []PromptSection
}

// LiveSeams is the optional Channel capability. A channel that serves nothing
// module-dependent (or cannot) does not implement it and the host skips it.
type LiveSeams interface {
	// RewireSeams re-reads the module seams from the live Context.
	RewireSeams(ctx *Context) error
}

type Channel interface {
	Start(ctx context.Context, deps ChannelDeps) error
	Stop() error
}

type SettingsKnob struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Type  string `json:"type"`
	Value any    `json:"value"`
	// Default rides EVERY knob with NO omitempty, so the settings pane can tell "no default
	// known" (JSON null) from default=false / "" / 0; the pane's modified-dot and
	// reset-to-default compare against exactly this field.
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
	// the aux lane then follows whatever model the current run serves, resolved at call
	// time - the choice persists as the literal word, never frozen to a model name.
	DefaultOption bool `json:"default_option,omitempty"`
}

type SettingsGroup struct {
	Key   string         `json:"key"`
	Label string         `json:"label"`
	Knobs []SettingsKnob `json:"knobs"`
}

type SettingsSchema struct {
	Groups []SettingsGroup `json:"groups"`
	// Scopes always rides the wire and is never null - an empty list is the empty world
	// (no agent dirs), where the pane renders no scope strip. Read-only: every write lands
	// on the agents REST edit endpoint, never on this payload.
	Scopes []SettingsScope `json:"scopes"`
}

// SettingsScope is one agent scope in the settings payload.
type SettingsScope struct {
	Name   string               `json:"name"`
	Groups []SettingsScopeGroup `json:"groups"`
	// Error names a layer file that could not be read or parsed. The scope still rides
	// (every agent dir is a scope, a broken one must not vanish from the strip) with
	// inherited values and no override flags; this field is what tells the pane the flags
	// are not the file's truth.
	Error string `json:"error,omitempty"`
}

// SettingsScopeGroup is one overridable knob group inside a scope.
type SettingsScopeGroup struct {
	Key        string              `json:"key"`
	Overridden bool                `json:"overridden"`
	Knobs      []SettingsScopeKnob `json:"knobs"`
}

// SettingsScopeKnob is one knob inside a scope; secrets ride masked like the main payload.
type SettingsScopeKnob struct {
	Key        string `json:"key"`
	Value      any    `json:"value"`
	Overridden bool   `json:"overridden"`
}

// Knob looks one knob up by its dotted key: the channel's `?key=` read and the CLI's
// `omega settings get` both address a knob through here, so an unknown key is one answer,
// not two. The returned pointer aliases the schema's own knob - readers must not mutate it.
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

// Settings is the runtime config knobs seam. Channels that host a UI (the webui settings
// pane lives behind the HTTP channel's /settings) read the host's knobs and save changes
// through it: the host owns the schema, validation, persistence and hot-apply, the channel
// never touches config.yaml. Nil when the host does not expose settings (endpoints then
// answer 501).
type Settings interface {
	// Schema returns the payload with secret values masked - plain text never rides the
	// wire.
	Schema() (*SettingsSchema, error)
	// Save applies values (map of dotted yaml path -> JSON value), validates, persists.
	Save(values map[string]any) (*SettingsSchema, error)
	// Raw returns the config FILE as YAML, secrets masked exactly as Schema masks them: the
	// file is the secret's home, so the wire never carries one.
	Raw() (string, error)
	// SaveRaw validates a whole YAML document through the same LoadConfig path Save uses.
	SaveRaw(text string) (*SettingsSchema, error)
	// Reset restores every knob that HAS a default through Save's own write path. The
	// config.yaml-only doors (provider.api_key, server.bind, the named instances and their
	// secrets) and knobs with no known default (provider.model_name - config requires it)
	// are kept: resetting is never destructive.
	Reset() (*SettingsSchema, error)
}

// ModuleStatus is one module in the registry introspection.
type ModuleStatus struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Provides    []string `json:"provides,omitempty"`
	Requires    []string `json:"requires,omitempty"`
	State       string   `json:"state"` // "mounted" | "disabled"
	Toggleable  bool     `json:"toggleable"`
	Dependents  []string `json:"dependents,omitempty"`
}

// ModuleToggles is the module-toggle seam. Toggle goes through the same mount machinery as
// boot and rejects loudly; a host that does not expose toggles is nil (endpoints answer 501).
type ModuleToggles interface {
	// Modules returns every module in the registry catalog with its live state (shell
	// modules never appear).
	Modules() []ModuleStatus
	// Toggle enables or disables one module at runtime. A rejection (tier law, edge case,
	// roll-back panic) returns an error naming the module and the failed check.
	Toggle(ctx context.Context, name string, enabled bool) error
	Refusals() []Refusal
}

type DisabledNames struct {
	Skills []string `json:"skills"`
	Tools  []string `json:"tools"`
	Agents []string `json:"agents"`
}

// RegistryToggles is the registry-browser toggle seam. A name the owning registry does not
// know returns an error - silence would persist a name toggle UIs can never show or clear.
type RegistryToggles interface {
	Disabled() DisabledNames
	// ToggleSkill enables/disables one skill (prompt-build exclusion).
	ToggleSkill(name string, enabled bool) error
	// ToggleTool enables/disables one tool (registry exclusion).
	ToggleTool(name string, enabled bool) error
	// ToggleAgent soft-disables one agent (roster + wake exclusion, the home untouched).
	ToggleAgent(name string, enabled bool) error
}

// UnknownName marks a toggle error whose name no registry knows - the 404 lane. A
// refused name must be distinguishable from a failed persistence write.
type UnknownName interface {
	Unknown() bool
}

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

// QuestionAnswer is the recorded answer to one QuestionPrompt; exactly one of its shapes
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

// WithRunOrigin stamps the surface that started the run. The vocabulary is closed at the
// writers: "interactive" (a terminal conversation), "gateway" (the HTTP channel), "cron"
// (the scheduler) and "delegate" (a subagent). Read by the approval gate, the verdict
// tool and the observability ring.
func WithRunOrigin(ctx context.Context, origin string) context.Context {
	if origin == "" {
		return ctx
	}
	return context.WithValue(ctx, runOriginKey{}, origin)
}

func RunOriginFromContext(ctx context.Context) string {
	origin, _ := ctx.Value(runOriginKey{}).(string)
	return origin
}

type runOriginKey struct{}

type runScope struct {
	CWD           string
	PromptContext string
}

type runScopeKey struct{}

func RunWorkspace(ctx context.Context) string {
	scope, _ := ctx.Value(runScopeKey{}).(runScope)
	return scope.CWD
}

// RunPromptContext returns the run's trust-gated project context override.
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

// ToolOutputChunk is one live chunk from a tool that produces output OVER TIME; a run with
// no tap set does not stream at all (the pre-tap behavior).
type ToolOutputChunk struct {
	// CallID and Tool identify the running call: the loop stamps them on the per-call ctx
	// (WithToolCall), a tool never sets them.
	CallID string
	Tool   string
	Text string
	Truncated bool
}

// RunOutputTap receives one live output chunk. Implementations must not block
// (the producer writes under its own buffer lock).
type RunOutputTap func(ToolOutputChunk)

type runOutputTapKey struct{}

type toolCallScope struct {
	CallID string
	Tool   string
}

type toolCallKey struct{}

// WithRunOutputTap returns a child context carrying the run's output tap. Values are
// run-scoped: each Run caller wraps its own context, so parallel runs never share a sink.
func WithRunOutputTap(ctx context.Context, tap RunOutputTap) context.Context {
	if tap == nil {
		return ctx
	}
	return context.WithValue(ctx, runOutputTapKey{}, tap)
}

// WithToolCall tags a tool's per-call context with the call identity the loop knows and the
// tool cannot (the tap payload's routing keys).
func WithToolCall(ctx context.Context, callID, tool string) context.Context {
	if callID == "" && tool == "" {
		return ctx
	}
	return context.WithValue(ctx, toolCallKey{}, toolCallScope{CallID: callID, Tool: tool})
}

// EmitToolOutput hands one live output chunk to the run's tap. A context without one (the
// CLI loop, tests) is a silent no-op, so a tool never needs to know whether anyone streams.
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

// Notice is one finished-thing record. The title line is METADATA ONLY (source kind +
// id + verdict) - prompt and session content never rides a notice (privacy law).
type Notice struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"` // task | cron | chat
	SourceID  string `json:"source_id"`
	Title     string `json:"title"`
	Verdict   string `json:"verdict"` // ok | failed
	CreatedAt string `json:"created_at"`
	Read      bool   `json:"read"`
}

// No Emitter contract lives here: emitter delivery is generic mount machinery.
