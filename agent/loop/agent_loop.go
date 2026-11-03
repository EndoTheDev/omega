package loop

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/EndoTheDev/omega/core"
	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/llm"
)

// defaultMaxTurns caps the conversation loop when no explicit cap is set.
const defaultMaxTurns = 100

// maxOverflowRetries caps how many times a turn is retried after a
// context overflow error; a second overflow surfaces the error.
// ponytail: fixed cap like the compaction threshold; upgrade path:
// expose as a config knob next to compaction settings.
const maxOverflowRetries = 1

// Loop is the standard turn-based conversation loop. It streams
// provider responses, executes tool calls, and feeds results back
// into the message history until the provider stops calling tools
// or the turn cap is reached.
type Loop struct{}

// Run executes the conversation loop. It reads all inputs from opts
// and writes events to opts.Events. The caller is responsible for
// closing the events channel.
func (Loop) Run(ctx context.Context, opts core.LoopOptions) error {
	tools := opts.Tools
	if tools == nil {
		tools = map[string]core.Tool{}
	}

	// Merge tool providers. Existing tools take precedence over
	// provider tools; provider tools take precedence over extension tools.
	for _, tp := range opts.ToolProviders {
		if tp == nil {
			continue
		}
		extTools := tp.Tools()
		merged := make(map[string]core.Tool, len(tools)+len(extTools))
		for name, t := range tools {
			merged[name] = t
		}
		for name, t := range extTools {
			if _, exists := merged[name]; !exists {
				merged[name] = t
			}
		}
		tools = merged
	}

	// Run-scoped deny (agent.WithDeniedTools): the merged registry - the
	// caller's map PLUS every provider's contribution - drops the denied
	// names before schemas and dispatch see them. A fresh map: the deny
	// must never shrink a map the caller still owns. v0.14.1 folds the
	// persistent disabled.tools list into the same filter; the run's
	// own deny (subagent scopes) rides with it.
	denied := agent.DeniedTools(ctx)
	if len(opts.DisabledTools) > 0 {
		if len(denied) == 0 {
			denied = make(map[string]bool)
		}
		for _, name := range opts.DisabledTools {
			denied[name] = true
		}
	}
	if len(denied) > 0 {
		filtered := make(map[string]core.Tool, len(tools))
		for name, t := range tools {
			if !denied[name] {
				filtered[name] = t
			}
		}
		tools = filtered
	}

	// Toolless run (agent.WithToolless): a metadata turn - the title
	// call is generated from user content that invites tool use, and
	// nothing it does outlives the title. The fully merged registry
	// above is replaced wholesale: no schema, no dispatch entry, and
	// no future module registration can resurrect one for this run.
	if agent.Toolless(ctx) {
		tools = map[string]core.Tool{}
	}

	maxTurns := opts.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxTurns
	}

	messages := opts.Messages

	// Build the system prompt. The prompt builder can fully replace it.
	// Guidelines are appended to any non-empty prompt.
	prompt := ""
	if opts.PromptBuilder != nil {
		if extPrompt, ok := opts.PromptBuilder.BuildPrompt(ctx, core.PromptBuildOptions{
			CWD:            opts.CWD,
			Messages:       messages,
			Extensions:     opts.ExtensionInfos,
			ProjectContext: opts.PromptContext,
			Custom:         opts.PromptCustom,
			Append:         opts.PromptAppend,
		}); ok {
			prompt = extPrompt
		}
		if prompt != "" {
			if guidelines := opts.PromptBuilder.Guidelines(); len(guidelines) > 0 {
				prompt += "\n## Extension Guidelines\n"
				for _, g := range guidelines {
					prompt += "- " + g + "\n"
				}
			}
		}
	}
	if prompt != "" {
		messages = append([]core.Message{core.NewSystem(prompt)}, messages...)
	}

	start := core.AgentStart{Type: "agent_start", ModelName: ""}
	if opts.Provider != nil {
		start.ModelName = opts.Provider.ModelName()
	}
	opts.Events <- start
	if opts.Logger != nil {
		opts.Logger.Printf("agent loop starting, model=%s, turns=%d", start.ModelName, maxTurns)
	}

	turns := 0
	overflowRetries := 0

	// Run affinity (t_f3fb7c52): every run drains injected results for
	// ITS OWN key only. The host may pin a deterministic key (tests);
	// otherwise a fresh nonce is minted per Run - the title turn and
	// sibling /chat runs sharing one session id each get their own key,
	// so a delegated child's result can only reach the run that spawned
	// it. The tool ctx below carries the same key, the delegate tool
	// stamps it at spawn.
	affinity := agent.RunAffinityFromContext(ctx)
	if affinity == "" {
		affinity = agent.NewRunAffinity()
		ctx = agent.WithRunAffinity(ctx, affinity)
	}

	// Turn-copy persistence (card t_471f6d4a): the run's new input
	// persists here first, then one assistant + its tool results per
	// turn, provider-correct order - user -> assistant(A + thinking +
	// calls) -> tool results -> assistant(B). The caller wraps the
	// TurnSink (with its session id and any head-parent pin) and the
	// messages it is adding with this send around the run context; a
	// sink without input persists turns only. Persistence failures log
	// and move on - a dead store never kills a live run.
	sink := agent.RunTurnSink(ctx)
	for _, msg := range agent.PersistInput(ctx) {
		persistTurnMessage(ctx, sink, opts.Logger, msg, "user input")
	}

	for {
		if ctx.Err() != nil {
			end := core.AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "cancelled", Error: ctx.Err().Error()}
			opts.Events <- end
			return nil
		}
		if turns >= maxTurns {
			if opts.Logger != nil {
				opts.Logger.Printf("max turns reached (%d)", maxTurns)
			}
			end := core.AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "max_turns"}
			opts.Events <- end
			return nil
		}

		if opts.CompactionProvider != nil {
			compacted, err := opts.CompactionProvider.Compact(ctx, messages)
			if err != nil {
				end := core.AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "error", Error: err.Error()}
				opts.Events <- end
				return nil
			}
			if len(compacted) < len(messages) {
				if opts.Logger != nil {
					opts.Logger.Printf("compaction triggered")
				}
			}
			messages = compacted
		}

		turns++
		turnStart := core.TurnStart{Type: "turn_start", Turn: turns}
		opts.Events <- turnStart

		// Phase meter: the persistence record's timing fields are
		// measured here, where the chunks actually arrive - chunk-to-
		// chunk while a phase runs, boundary-to-chunk when it opens
		// (turn start, or the last tool result), tool-call arrivals
		// close both spans. Same law the channel's live-view meter
		// follows; the loop's numbers are the persisted ones.
		meter := newPhaseMeter()
		var content, thinking strings.Builder
		var toolCalls []core.ToolCall
		finishReason := "stop"
		streamErr := ""

		if opts.Provider == nil {
			end := core.AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "error", Error: "no provider configured"}
			opts.Events <- end
			return nil
		}

		for event := range opts.Provider.Stream(ctx, messages, toolSchemas(tools)) {
			switch e := event.(type) {
			case core.ResponseChunk:
				meter.response(time.Now())
				content.WriteString(e.Content)
			case core.ThinkingChunk:
				meter.thinking(time.Now())
				thinking.WriteString(e.Content)
			case core.ToolCallEvent:
				meter.toolCall(e.ToolCall.ID, time.Now())
				toolCalls = append(toolCalls, e.ToolCall)
			case core.StreamEnd:
				finishReason = e.FinishReason
				streamErr = e.Error
			}
			opts.Events <- core.StreamEvent{Event: event}
		}

		if streamErr != "" {
			if opts.Logger != nil {
				opts.Logger.Errorf("stream error: %s", streamErr)
			}
			// A context overflow error triggers one auto-compaction and
			// retry of the turn. The failed attempt counts as a turn and
			// emits TurnStart without TurnEnd - acceptable asymmetry, the
			// retried turn reports its own TurnEnd. Skip the retry when
			// response content was already streamed: the user saw it, and
			// retrying would duplicate it.
			if isOverflowError(streamErr) && opts.CompactionProvider != nil && overflowRetries < maxOverflowRetries && content.Len() == 0 {
				overflowRetries++
				if opts.Logger != nil {
					opts.Logger.Printf("context overflow, retrying (attempt %d/%d)", overflowRetries, maxOverflowRetries)
				}
				compacted, err := opts.CompactionProvider.Compact(ctx, messages)
				if err != nil {
					end := core.AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "error", Error: err.Error()}
					opts.Events <- end
					return nil
				}
				messages = compacted
				continue
			}
			// No compactor loaded: surface a friendly message instead
			// of the raw provider error.
			errMsg := streamErr
			if isOverflowError(streamErr) && opts.CompactionProvider == nil {
				errMsg = "context full — start a new session (/new)"
				if opts.Logger != nil {
					opts.Logger.Errorf("context full, no compactor loaded")
				}
			}
			end := core.AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "error", Error: errMsg}
			opts.Events <- end
			return nil
		}

		assistant := core.NewAssistant(content.String())
		if thinking.Len() > 0 {
			text := thinking.String()
			assistant.Thinking = &text
		}
		assistant.ToolCalls = toolCalls
		// Timing rides the assistant record itself: the loop's meters
		// are the persisted ones, so every surface's reload (webui,
		// TUI, export) sees thinking/response spans - not only the
		// sessions the http channel used to meter.
		if thinkMs, respMs := meter.spans(); thinkMs > 0 || respMs > 0 {
			assistant.Timing = &core.Timing{ThinkingMs: thinkMs, ResponseMs: respMs}
		}
		persistTurnMessage(ctx, sink, opts.Logger, assistant, "assistant")
		messages = append(messages, assistant)
		assistantEvent := core.AssistantMessageEvent{Type: "assistant_message", Message: assistant}
		opts.Events <- assistantEvent

		// Execute tool calls concurrently. Results are collected in
		// order so the message history stays deterministic.
		results := make([]core.ToolResult, len(toolCalls))
		completed := make([]time.Time, len(toolCalls))
		var wg sync.WaitGroup
		for i, call := range toolCalls {
			tool, ok := tools[call.Name]
			if !ok {
				results[i] = core.NewToolResult("unknown tool: "+call.Name, call.ID, true)
				continue
			}
			wg.Add(1)
			go func(idx int, c core.ToolCall, t core.Tool) {
				defer wg.Done()
				// The smart-approval gate (card t_c2b52cb5): a mounted seam
				// decides before the command runs - approve passes through,
				// anything else becomes the tool result and the command never
				// executed. Gate runs INSIDE the call's goroutine, same ctx: a
				// run stop cancels a mid-gate block like it cancels a command.
				// The returned line is the gate's transcript record (card
				// t_1b3e2a8b): stamped onto this call's tool result so the
				// decision renders on the call's own row - live and restored.
				// No seam, no stamp: the tool result carries the execution's
				// own truth alone.
				var gateLine string
				if opts.Approvals != nil {
					line, gerr := opts.Approvals.Gate(ctx, c.Name, c.Arguments)
					if gerr != nil {
						res := core.NewToolResult(gerr.Error(), c.ID, true)
						res.Gate = line
						results[idx] = res
						completed[idx] = time.Now()
						if opts.Logger != nil {
							opts.Logger.Errorf("tool %s gated: %v", c.Name, gerr)
						}
						return
					}
					gateLine = line
				}
				result, err := t.Run(ctx, c.Arguments)
				completed[idx] = time.Now()
				// The learning-loop seam (t_9972dfd0): every executed
				// tool call is one iteration; a skills write/update is
				// the persisted lesson. Concurrent-safe by contract; a
				// nil seam is the off case.
				if opts.Nudge != nil {
					opts.Nudge.ObserveToolRun(c.Name, c.Arguments, err)
				}
				if err != nil {
					res := core.NewToolResult(err.Error(), c.ID, true)
					res.Gate = gateLine
					results[idx] = res
					if opts.Logger != nil {
						opts.Logger.Errorf("tool %s error: %v", c.Name, err)
					}
					return
				}
				if opts.MaxToolOutput > 0 && len(result) > opts.MaxToolOutput {
					result = result[:opts.MaxToolOutput] + fmt.Sprintf("\n... [truncated, %d bytes total]", len(result))
				}
				res := core.NewToolResult(result, c.ID, false)
				res.Gate = gateLine
				results[idx] = res
			}(i, call, tool)
		}
		wg.Wait()

		executed := 0
		for i, msg := range results {
			// Wall-clock the tool actually ran: dispatch (the call
			// event) to completion. Unknown tools report none - they
			// never ran.
			if start, ok := meter.callStart(toolCalls[i].ID); ok && !completed[i].IsZero() {
				msg.Timing = &core.Timing{ElapsedMs: completed[i].Sub(start).Milliseconds()}
			}
			persistTurnMessage(ctx, sink, opts.Logger, msg, "tool result")
			messages = append(messages, msg)
			toolResultEvent := core.ToolResultEvent{Type: "tool_result", Message: msg}
			opts.Events <- toolResultEvent
			executed++
		}

		turnEnd := core.TurnEnd{Type: "turn_end", Turn: turns, ToolCalls: executed}
		opts.Events <- turnEnd

		// Nudge delivery (t_9972dfd0): after the tool batch lands in
		// the history exactly one reminder may be due - consume it and
		// ride it as the next turn prep's prompt line. Ephemeral by
		// law: the line is turn-prep scaffolding, not a persisted turn
		// (restored sessions replay the work, not the reminder). A
		// no-tool turn ends the run here unhinted.
		if opts.Nudge != nil && executed > 0 {
			if line := opts.Nudge.NudgeLine(); line != "" {
				messages = append(messages, core.NewUser(line))
			}
		}

		// Non-blocking: pull THIS run's buffered subagent results and
		// batch them into one user message. Runs after every turn
		// (not just when there are no tool calls) so that results
		// are picked up even while the agent polls with delegate.status.
		// Run affinity (t_f3fb7c52): Poll fetches only this run's
		// key-addressed queue.
		if opts.InjectedMessages != nil {
			var combined string
			for _, msg := range opts.InjectedMessages.Poll(affinity) {
				if combined != "" {
					combined += "\n\n---\n\n"
				}
				combined += msg.Text
			}
			if combined != "" {
				injected := core.NewUser(combined)
				persistTurnMessage(ctx, sink, opts.Logger, injected, "user input")
				messages = append(messages, injected)
				continue
			}
		}

		if len(toolCalls) == 0 {
			// One-shot mode (UserInput == nil): block if subagents are
			// still running. TUI mode (UserInput != nil) never blocks.
			// Run affinity (t_f3fb7c52): the wait is addressed to THIS
			// run's key - another run's result never releases or
			// enters this block.
			if opts.InjectedMessages != nil && opts.UserInput == nil && opts.PendingDelegations != nil && opts.PendingDelegations(affinity) > 0 {
				msg, ok := opts.InjectedMessages.Wait(ctx, affinity)
				if ok {
					injected := core.NewUser(msg.Text)
					persistTurnMessage(ctx, sink, opts.Logger, injected, "user input")
					messages = append(messages, injected)
					continue
				}
				if err := ctx.Err(); err != nil {
					end := core.AgentEnd{Type: "agent_end", Turns: turns, FinishReason: "cancelled", Error: err.Error()}
					opts.Events <- end
					return nil
				}
				// The seam settled without a result and without a
				// cancellation (a stream that stopped): no result will
				// ever come - fall through to the run's own end.
			}
			end := core.AgentEnd{Type: "agent_end", Turns: turns, FinishReason: finishReason, Message: assistant}
			opts.Events <- end
			if opts.Logger != nil {
				if finishReason == "error" || finishReason == "cancelled" {
					opts.Logger.Errorf("agent ended: %s", finishReason)
				} else {
					opts.Logger.Printf("agent ended after %d turns", turns)
				}
			}
			return nil
		}
	}
}

// isOverflowError reports whether a provider error indicates the context
// window was exceeded. ponytail: substring match on common provider
// wording. Upgrade path: structured error codes per provider.
func isOverflowError(err string) bool {
	lower := strings.ToLower(err)
	for _, phrase := range []string{"context length", "context_length", "too long", "token limit", "maximum context"} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// persistTurnMessage writes one finished message through the run's
// TurnSink. Persistence failures log and move on - a dead store never
// kills a live run, the channel surfaces the reload gap instead of an
// error frame mid-stream.
func persistTurnMessage(ctx context.Context, sink agent.TurnSink, logger core.LoggerProvider, msg core.Message, stage string) {
	if sink == nil {
		return
	}
	// Persistence outlives the run context: a stop or cancel must not
	// eat the turn copy. The partial assistant of a cancelled stream
	// and the errored tool result that follows it belong to the
	// transcript as much as any finished turn.
	if err := sink.AppendTurnMessage(context.WithoutCancel(ctx), msg); err != nil && logger != nil {
		logger.Errorf("persist %s: %v", stage, err)
	}
}

// phaseMeter tracks the turn's timing phases on the persistence record:
// chunk-to-chunk while a phase runs, boundary-to-chunk when it opens
// (turn start), tool-call arrivals closing both spans. The same law the
// http channel's live-view accumulator follows - the loop's numbers are
// the persisted ones now.
type phaseMeter struct {
	lastThink, lastResp, openAt time.Time
	callStarts                  map[string]time.Time
	thinkDur, respDur           time.Duration
}

func newPhaseMeter() *phaseMeter {
	return &phaseMeter{openAt: time.Now()}
}

func (m *phaseMeter) thinking(now time.Time) {
	m.thinkDur += m.delta(m.lastThink, now)
	m.lastThink = now
	m.openAt = now
}
func (m *phaseMeter) response(now time.Time) {
	m.respDur += m.delta(m.lastResp, now)
	m.lastResp = now
	m.openAt = now
}

// toolCall records a call's dispatch instant and closes both stream
// spans - the gap while the tool runs belongs to nobody.
func (m *phaseMeter) toolCall(id string, now time.Time) {
	m.lastThink, m.lastResp, m.openAt = time.Time{}, time.Time{}, time.Time{}
	if m.callStarts == nil {
		m.callStarts = map[string]time.Time{}
	}
	if id != "" {
		m.callStarts[id] = now
	}
}

func (m *phaseMeter) callStart(id string) (time.Time, bool) {
	if m.callStarts == nil {
		return time.Time{}, false
	}
	t, ok := m.callStarts[id]
	return t, ok
}

// spans returns the turn's accumulated thinking and response durations.
func (m *phaseMeter) spans() (thinkMs, respMs int64) {
	return m.thinkDur.Milliseconds(), m.respDur.Milliseconds()
}

// delta returns the wall clock the chunk bills into its span:
// chunk-to-chunk while the same phase is running (last set), and
// boundary-to-chunk when the chunk opens a span, so a single-chunk
// phase still carries its latency. An unset boundary contributes
// nothing.
func (m *phaseMeter) delta(last, now time.Time) time.Duration {
	if !last.IsZero() {
		return now.Sub(last)
	}
	if !m.openAt.IsZero() {
		return now.Sub(m.openAt)
	}
	return 0
}

// toolSchemas converts a tools map to the provider schema list.
func toolSchemas(tools map[string]core.Tool) []llm.ToolSchema {
	if len(tools) == 0 {
		return nil
	}
	result := make([]llm.ToolSchema, 0, len(tools))
	for name, tool := range tools {
		result = append(result, llm.ToolSchema{
			Name:        name,
			Description: tool.Description,
			// The action enum derives from the tool's Actions metadata,
			// not from a hand-maintained schema value - one source of
			// truth, no drift possible.
			Parameters: tool.SchemaParameters(),
		})
	}
	return result
}
