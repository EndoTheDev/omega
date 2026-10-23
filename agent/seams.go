package agent

import (
	"context"

	"github.com/EndoTheDev/omega/core"
)

// Re-export shims (arc 1b, t_7bb4790d): the seam contracts and their
// seam-adjacent payloads now live in core. Every module import of
// agent.* keeps compiling; the aliases disappear when modules rewire
// in phase 2. No behavior changes - these are type identities, not
// wrappers.
//
// DO NOT add new seams here - the seam contracts belong in core.

type (
	// LoopProvider drives the multi-turn conversation; the default
	// implementation is the agent-loop plugin.
	LoopProvider = core.LoopProvider
	// LoopOptions carries everything the loop needs to run one agent session.
	LoopOptions = core.LoopOptions
	// CompactionProvider handles context compaction beyond the token budget.
	CompactionProvider = core.CompactionProvider
	// ToolProvider supplies tools to the agent. Additive.
	ToolProvider = core.ToolProvider
	// DefaultToolProvider wraps a static tool map (tests).
	DefaultToolProvider = core.DefaultToolProvider
	// StoreProvider is the session persistence seam.
	StoreProvider = core.StoreProvider
	// AgentSessionStore is the optional agent-scoped session surface.
	AgentSessionStore = core.AgentSessionStore
	// SkillsProvider is the skill loading seam.
	SkillsProvider = core.SkillsProvider
	// LoggerProvider is the logging seam.
	LoggerProvider = core.LoggerProvider
	// MemoryProvider manages persistent memory across sessions.
	MemoryProvider = core.MemoryProvider
	// PromptBuilder builds the system prompt and the guideline lines.
	PromptBuilder = core.PromptBuilder
	// PromptSectioner is the additive prompt-section seam.
	PromptSectioner = core.PromptSectioner
	// Channel is a delivery transport connecting external clients. Additive.
	Channel = core.Channel
	// SettingsKnob is one exposed config field.
	SettingsKnob = core.SettingsKnob
	// SettingsGroup is one config section: a labeled set of knobs.
	SettingsGroup = core.SettingsGroup
	// SettingsSchema is the JSON-marshalable settings payload.
	SettingsSchema = core.SettingsSchema
	// Settings is the runtime config knobs seam.
	Settings = core.Settings
	// ChannelDeps carries everything a channel needs to run.
	ChannelDeps = core.ChannelDeps
	// CronProvider schedules omega prompts on cron expressions.
	CronProvider = core.CronProvider
	// CronDeps carries everything the cron runner needs.
	CronDeps = core.CronDeps
	// CronJobInfo is one read-only row in the cron inspector listing.
	CronJobInfo = core.CronJobInfo
	// CronLister is the read-only inspector view over the cron scheduler.
	CronLister = core.CronLister
	// SkillsLister is the read-only inspector view over the skill inventory.
	SkillsLister = core.SkillsLister
	// VerdictPrompt is one agent-issued question (verdict cards v1).
	VerdictPrompt = core.VerdictPrompt
	// Verdict is the recorded answer to one VerdictPrompt.
	Verdict = core.Verdict
	// VerdictsProvider is the interactive-question seam.
	VerdictsProvider = core.VerdictsProvider
	// Frontend is the user-facing interface seam.
	Frontend = core.Frontend
	// FrontendOptions carries host-level settings the frontend needs.
	FrontendOptions = core.FrontendOptions
	// TrustProvider gates project context (AGENTS.md) behind a trust check.
	TrustProvider = core.TrustProvider
	// Notification is one finished-thing record (card t_5d9032d3).
	Notification = core.Notification
	// NotificationsProvider is the finished-things store seam.
	NotificationsProvider = core.NotificationsProvider
)

// Run origins (verdict cards v1): which surface launched the agent -
// verdict.request only issues from interactive turns - cron fires and
// delegate subagents must never hard-pause waiting for a click.
const (
	RunOriginInteractive = core.RunOriginInteractive
	RunOriginCron        = core.RunOriginCron
	RunOriginDelegate    = core.RunOriginDelegate
)

// WithRunOrigin tags the run context with its launching surface. The
// request_verdict tool reads it to refuse background origins.
func WithRunOrigin(ctx context.Context, origin string) context.Context {
	return core.WithRunOrigin(ctx, origin)
}

// RunOriginFromContext returns the run's origin tag ("" when unset -
// callers treat that as interactive).
func RunOriginFromContext(ctx context.Context) string {
	return core.RunOriginFromContext(ctx)
}