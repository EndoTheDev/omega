package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/EndoTheDev/omega/agent"
	"github.com/EndoTheDev/omega/ai"
	"github.com/EndoTheDev/omega/extensions/store"
)

// messageRole returns the role string for a message.
func messageRole(m ai.Message) string {
	switch m.(type) {
	case ai.User:
		return "user"
	case ai.Assistant:
		return "assistant"
	case ai.System:
		return "system"
	case ai.ToolResult:
		return "tool"
	default:
		return "unknown"
	}
}

// exportMessages writes messages as JSONL (one JSON object per line with
// role and content fields) to w. Used by both the CLI export subcommand
// and the TUI /export slash command.
func exportMessages(messages []ai.Message, w io.Writer) error {
	enc := json.NewEncoder(w)
	for _, msg := range messages {
		entry := map[string]any{
			"role":    messageRole(msg),
			"content": agent.MessageText(msg),
		}
		if err := enc.Encode(entry); err != nil {
			return err
		}
	}
	return nil
}

// resolveSessionCLI resolves a session argument to a session ID for the
// CLI export command. Tries exact ID match, then case-insensitive label
// prefix match. Returns an error if multiple labels match.
func resolveSessionCLI(storeDB agent.StoreProvider, arg string) (string, error) {
	ctx := context.Background()

	// Try exact ID match first.
	if _, err := storeDB.GetSession(ctx, arg); err == nil {
		return arg, nil
	}

	// Try case-insensitive label prefix match.
	sessions, err := storeDB.ListSessions(ctx)
	if err != nil {
		return "", fmt.Errorf("list sessions: %w", err)
	}
	lower := strings.ToLower(arg)
	var matches []string
	for _, s := range sessions {
		if s.Label != "" && strings.HasPrefix(strings.ToLower(s.Label), lower) {
			matches = append(matches, s.ID)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("session not found: %s", arg)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("multiple sessions match %q: %s", arg, strings.Join(matches, ", "))
	}
}

// cmdExport loads config, opens the store, resolves the session, and
// writes its messages as JSONL to the output path (or stdout with "-").
func cmdExport(configPath string, rest []string) error {
	args := stripConfigFlag(rest)
	if len(args) == 0 {
		return fmt.Errorf("usage: omega export <session-id-or-label> [output-path]")
	}
	sessionArg := args[0]
	outputPath := sessionArg + ".jsonl"
	if len(args) > 1 {
		outputPath = args[1]
	}

	cfg, err := LoadConfig(resolveConfigPath(configPath))
	if err != nil {
		return err
	}
	resolveHomePaths(&cfg)

	storeDB, err := store.Open(cfg.Store.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer storeDB.Close()

	sessionID, err := resolveSessionCLI(storeDB, sessionArg)
	if err != nil {
		return err
	}

	messages, err := storeDB.GetMessages(context.Background(), sessionID)
	if err != nil {
		return fmt.Errorf("get messages: %w", err)
	}

	var w io.Writer
	if outputPath == "-" {
		w = os.Stdout
	} else {
		f, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("create %s: %w", outputPath, err)
		}
		defer f.Close()
		w = f
	}

	if err := exportMessages(messages, w); err != nil {
		return fmt.Errorf("export: %w", err)
	}

	if outputPath != "-" {
		fmt.Printf("exported %d messages to %s\n", len(messages), outputPath)
	}
	return nil
}
