// core-prompt is an omega extension that builds the system prompt.
// It owns the prompt template and assembly logic. Data-gathering
// (skills, project context) is delegated to the harness library.
//
// Seam: prompt_builder
// Method: prompt/build — receives cwd, messages, extensions, project_context,
// custom, append. Reads skills from OMEGA_SKILLS_DIR, assembles the full
// system prompt, returns {ok: true, prompt: result}.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/EndoTheDev/omega/agent"
)

// --- omega extension protocol types ---

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError        `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// --- prompt building ---

// promptBuildParams is the params object for the prompt/build method.
type promptBuildParams struct {
	CWD            string    `json:"cwd"`
	Extensions     []extInfo `json:"extensions"`
	ProjectContext string    `json:"project_context"`
	Custom         string    `json:"custom"`
	Append         []string  `json:"append"`
}

// extInfo is the minimal subset of agent.ExtensionInfo the prompt
// builder reads: extension name and tool list.
type extInfo struct {
	Name     string `json:"Name"`
	ToolList []struct {
		Name        string `json:"Name"`
		Description string `json:"Description"`
	} `json:"ToolList"`
}

// loadSkills reads skills from OMEGA_SKILLS_DIR. Return nil if the
// env var is not set or the directory is missing.
func loadSkills() []agent.Skill {
	dir := os.Getenv("OMEGA_SKILLS_DIR")
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var skills []agent.Skill
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		skillFile := filepath.Join(dir, entry.Name(), entry.Name()+".md")
		data, err := os.ReadFile(skillFile)
		if err != nil {
			continue
		}
		s := agent.Skill{Name: entry.Name(), Dir: filepath.Join(dir, entry.Name())}
		// Parse simple YAML frontmatter (name, description).
		lines := strings.Split(string(data), "\n")
		if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
			for _, line := range lines[1:] {
				if strings.TrimSpace(line) == "---" {
					break
				}
				parts := strings.SplitN(line, ":", 2)
				if len(parts) != 2 {
					continue
				}
				key := strings.TrimSpace(parts[0])
				val := strings.TrimSpace(parts[1])
				switch key {
				case "name":
					s.Name = val
				case "description":
					s.Description = val
				}
			}
		}
		skills = append(skills, s)
	}
	return skills
}

// buildPrompt assembles the system prompt from the prompt/build params.
func buildPrompt(params promptBuildParams) string {
	skills := loadSkills()
	infos := params.Extensions

	var b strings.Builder
	b.WriteString("You are an AI coding agent with access to tools.\n")

	b.WriteString("\n## Guidelines\n")
	b.WriteString("- Use tools to read files and run commands before making assumptions.\n")
	b.WriteString("- Prefer the simplest solution that works. Avoid unnecessary abstraction.\n")
	b.WriteString("- When editing files, match the existing style and conventions.\n")
	b.WriteString("- Report what you did concisely. Do not repeat file contents back.\n")
	b.WriteString("- If something fails, report the error honestly rather than guessing.\n")

	if params.ProjectContext != "" {
		b.WriteString("\n## Project Context\n")
		b.WriteString(params.ProjectContext)
		b.WriteString("\n")
	}

	if len(skills) > 0 {
		b.WriteString("\n## Available Skills\n")
		b.WriteString("Call the skills.read tool with a skill name to read its full content.\n")
		for _, skill := range skills {
			fmt.Fprintf(&b, "- %s: %s\n", skill.Name, skill.Description)
		}
	}

	b.WriteString("\n## Tools\n")
	if len(infos) > 0 {
		for _, ext := range infos {
			if len(ext.ToolList) > 0 {
				fmt.Fprintf(&b, "### %s\n", ext.Name)
				for _, t := range ext.ToolList {
					fmt.Fprintf(&b, "- %s: %s\n", t.Name, firstLine(t.Description))
				}
				b.WriteString("\n")
			}
		}
	}

	b.WriteString("\n## Environment\n")
	fmt.Fprintf(&b, "CWD: %s\n", params.CWD)
	fmt.Fprintf(&b, "OS: %s\n", runtime.GOOS)
	if runtime.GOOS == "windows" {
		b.WriteString("Shell: cmd.exe\n")
	} else {
		b.WriteString("Shell: bash\n")
	}
	fmt.Fprintf(&b, "Date: %s\n", time.Now().Format("2006-01-02"))

	if params.Custom != "" {
		b.WriteString("\n")
		b.WriteString(params.Custom)
		b.WriteString("\n")
	}
	for _, extra := range params.Append {
		b.WriteString("\n")
		b.WriteString(extra)
		b.WriteString("\n")
	}
	return b.String()
}

// firstLine returns the first non-empty line of s, or s itself if it
// has no newlines. Used to keep tool descriptions short in the system
// prompt — full descriptions go to the LLM via the provider's JSON
// tool schemas.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line)
		}
	}
	return s
}

// --- JSON-RPC dispatch ---

func main() {
	stdin := bufio.NewReader(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	for {
		line, err := stdin.ReadString('\n')
		if err != nil {
			return
		}
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}

		switch req.Method {
		case "initialize":
			result, _ := json.Marshal(map[string]any{
				"name":         "core-prompt",
				"tools":        []any{},
				"subscriptions": []string{},
				"seams":         []string{"prompt_builder"},
			})
			if req.ID != nil {
				encoder.Encode(rpcResponse{JSONRPC: "2.0", ID: *req.ID, Result: result})
			}

		case "prompt/build":
			var params promptBuildParams
			if req.Params != nil {
				json.Unmarshal(req.Params, &params)
			}
			prompt := buildPrompt(params)
			result, _ := json.Marshal(map[string]any{
				"ok":     true,
				"prompt": prompt,
			})
			if req.ID != nil {
				encoder.Encode(rpcResponse{JSONRPC: "2.0", ID: *req.ID, Result: result})
			}

		case "shutdown":
			return

		default:
			if req.ID != nil {
				encoder.Encode(rpcResponse{JSONRPC: "2.0", ID: *req.ID, Result: json.RawMessage("{}")})
			}
		}
	}
}