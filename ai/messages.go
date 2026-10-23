package ai

import (
	"encoding/json"
	"fmt"

	"github.com/EndoTheDev/omega/core"
)

// Compatibility aliases: the message vocabulary (the Message seal and
// its parts) now lives in core (arc 1b2, t_d16468fd). Every provider
// lane and module import of ai.* keeps compiling; the aliases
// disappear when modules rewire in phase 2.
//
// DO NOT add new types here - new message shapes belong in core.
type (
	// Message is the sealed interface for all message types.
	Message = core.Message
	// System is the system prompt message.
	System = core.System
	// ImageContent carries a base64-encoded image with its MIME type.
	ImageContent = core.ImageContent
	// User is a user chat message.
	User = core.User
	// Timing carries server-measured generation durations.
	Timing = core.Timing
	// Assistant is the model's response in a turn.
	Assistant = core.Assistant
	// ToolResult is the result of a tool execution.
	ToolResult = core.ToolResult
	// ModelChange is a session entry recording a model switch.
	ModelChange = core.ModelChange
	// ThinkingLevelChange is a session entry recording a thinking level change.
	ThinkingLevelChange = core.ThinkingLevelChange
	// VariantSelect records the ACTIVE variant of one message branch point.
	VariantSelect = core.VariantSelect
	// VerdictOption is one pick row on a verdict card.
	VerdictOption = core.VerdictOption
	// VerdictCard is the transcript record of one agent-issued question.
	VerdictCard = core.VerdictCard
	// Verdict is the transcript record of one answer.
	Verdict = core.VerdictRecord
	// ToolCall is a tool invocation request from the model.
	ToolCall = core.ToolCall
)

// Constructor shims for the moved message vocabulary: provider lanes
// and modules keep calling ai.New* until phase 2 rewiring.
func NowISO() string { return core.NowISO() }

func NewSystem(content string) System { return core.NewSystem(content) }

func NewUser(content string) User { return core.NewUser(content) }

func NewUserWithImages(content string, images []ImageContent) User {
	return core.NewUserWithImages(content, images)
}

func NewAssistant(content string) Assistant { return core.NewAssistant(content) }

func NewToolResult(content, toolCallID string, isError bool) ToolResult {
	return core.NewToolResult(content, toolCallID, isError)
}

func NewModelChange(model string) ModelChange { return core.NewModelChange(model) }

func NewThinkingLevelChange(level string) ThinkingLevelChange {
	return core.NewThinkingLevelChange(level)
}

func NewVariantSelect(branch, child string) VariantSelect {
	return core.NewVariantSelect(branch, child)
}

func NewVerdictCard(cardID, title, body string, options []VerdictOption, multi, allowText bool, timeoutS int) VerdictCard {
	return core.NewVerdictCard(cardID, title, body, options, multi, allowText, timeoutS)
}

func NewVerdict(cardID string, optionIndexes []int, options []string, text string, skipped, expired bool) Verdict {
	return core.NewVerdictRecord(cardID, optionIndexes, options, text, skipped, expired)
}

// EncodeMessage serializes an ai.Message to its role discriminator
// and JSON payload. Used by the store extension for persistence
// and wire transfer.
func EncodeMessage(msg Message) (string, []byte, error) {
	var role string
	switch msg.(type) {
	case System:
		role = "system"
	case User:
		role = "user"
	case Assistant:
		role = "assistant"
	case ToolResult:
		role = "tool"
	case ModelChange:
		role = "model_change"
	case ThinkingLevelChange:
		role = "thinking_level_change"
	case VariantSelect:
		role = "variant_select"
	case VerdictCard:
		role = "verdict_card"
	case Verdict:
		role = "verdict"
	default:
		return "", nil, fmt.Errorf("unknown message type %T", msg)
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return "", nil, err
	}
	return role, payload, nil
}

// DecodeMessage reconstructs a Message from a role discriminator
// and JSON payload. Inverse of EncodeMessage.
func DecodeMessage(role string, payload []byte) (Message, error) {
	switch role {
	case "system":
		var m System
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "user":
		var m User
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "assistant":
		var m Assistant
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "tool":
		var m ToolResult
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "model_change":
		var m ModelChange
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "thinking_level_change":
		var m ThinkingLevelChange
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "variant_select":
		var m VariantSelect
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "verdict_card":
		var m VerdictCard
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	case "verdict":
		var m Verdict
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil, err
		}
		return m, nil
	default:
		return nil, fmt.Errorf("unknown role %q", role)
	}
}

// MessageRole returns the role string for a message ("user", "assistant",
// "system", "tool", or "unknown"). Shared by export, store commands, and
// the TUI to avoid duplicating the type switch.
func MessageRole(m Message) string {
	switch m.(type) {
	case System:
		return "system"
	case User:
		return "user"
	case Assistant:
		return "assistant"
	case ToolResult:
		return "tool"
	case VerdictCard:
		return "verdict_card"
	case Verdict:
		return "verdict"
	default:
		return "unknown"
	}
}
