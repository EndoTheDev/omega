// core-skills is an omega extension that implements the skills seam.
// It scans a directory for <name>/<name>.md skill files, parses their
// YAML frontmatter, and provides the skills.read tool and /skills
// slash command. The skills directory is passed via OMEGA_SKILLS_DIR
// environment variable.
//
// Seam: skills
// Methods: skills/load
// Tools: skills.read
// Commands: /skills
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// --- omega extension protocol types ---

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError        `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type extTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type extCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Dir         string `json:"dir"`
}

var toolDefs = []extTool{
	{
		Name:        "skills.read",
		Description: "Load a skill's full content by name. Returns the skill's markdown body and the directory path where its files (scripts, references, templates) live.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "The skill name (from the Available Skills list)"},
			},
			"required": []string{"name"},
		},
	},
}

var commandDefs = []extCommand{
	{
		Name:        "/skills",
		Description: "List loaded skills with name and description",
	},
}

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
				"name":          "core-skills",
				"seams":         []string{"skills"},
				"tools":         toolDefs,
				"commands":      commandDefs,
				"subscriptions": []string{},
			})
			if req.ID != nil {
				encoder.Encode(rpcResponse{JSONRPC: "2.0", ID: *req.ID, Result: result})
			}

		case "tool_call":
			var params struct {
				Name string         `json:"name"`
				Tool string         `json:"tool"`
				Args map[string]any `json:"args"`
			}
			json.Unmarshal(req.Params, &params)
			toolName := params.Name
			if toolName == "" {
				toolName = params.Tool
			}

			if toolName == "skills.read" {
				name, _ := params.Args["name"].(string)
				if name == "" {
					encoder.Encode(rpcResponse{
						JSONRPC: "2.0", ID: *req.ID,
						Error: &rpcError{Code: -32602, Message: "missing required argument \"name\""},
					})
					continue
				}
				dir := os.Getenv("OMEGA_SKILLS_DIR")
				if dir == "" {
					encoder.Encode(rpcResponse{
						JSONRPC: "2.0", ID: *req.ID,
						Error: &rpcError{Code: -32603, Message: "OMEGA_SKILLS_DIR not set"},
					})
					continue
				}
				skillFile := filepath.Join(dir, name, name+".md")
				s, err := loadSkill(skillFile)
				if err != nil {
					if os.IsNotExist(err) {
						entries, _ := os.ReadDir(dir)
						var names []string
						for _, e := range entries {
							if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
								names = append(names, e.Name())
							}
						}
						encoder.Encode(rpcResponse{
							JSONRPC: "2.0", ID: *req.ID,
							Error: &rpcError{Code: -32603, Message: fmt.Sprintf("skill %q not found. Available: %s", name, strings.Join(names, ", "))},
						})
						continue
					}
					encoder.Encode(rpcResponse{
						JSONRPC: "2.0", ID: *req.ID,
						Error: &rpcError{Code: -32603, Message: err.Error()},
					})
					continue
				}
				s.Name = name
				s.Dir = filepath.Join(dir, name)
				output := formatSkill(s)
				result, _ := json.Marshal(map[string]any{"content": output, "is_error": false})
				encoder.Encode(rpcResponse{JSONRPC: "2.0", ID: *req.ID, Result: result})
			} else {
				encoder.Encode(rpcResponse{
					JSONRPC: "2.0", ID: *req.ID,
					Error: &rpcError{Code: -32601, Message: "unknown tool: " + params.Name},
				})
			}

		case "command":
			var params struct {
				Name string `json:"name"`
				Args string `json:"args"`
			}
			json.Unmarshal(req.Params, &params)

			if params.Name == "/skills" {
				dir := os.Getenv("OMEGA_SKILLS_DIR")
				skills, err := scanSkills(dir)
				if err != nil {
					encoder.Encode(rpcResponse{
						JSONRPC: "2.0", ID: *req.ID,
						Error: &rpcError{Code: -32603, Message: err.Error()},
					})
					continue
				}
				var sb strings.Builder
				if len(skills) == 0 {
					sb.WriteString("[no skills loaded]")
				} else {
					nameWidth := 12
					for _, s := range skills {
						if len(s.Name) > nameWidth {
							nameWidth = len(s.Name)
						}
					}
					fmt.Fprintf(&sb, "%-*s  %s\n", nameWidth, "NAME", "DESCRIPTION")
					for _, s := range skills {
						fmt.Fprintf(&sb, "%-*s  %s\n", nameWidth, s.Name, s.Description)
					}
				}
				result, _ := json.Marshal(map[string]any{"output": sb.String()})
				encoder.Encode(rpcResponse{JSONRPC: "2.0", ID: *req.ID, Result: result})
			} else {
				encoder.Encode(rpcResponse{
					JSONRPC: "2.0", ID: *req.ID,
					Error: &rpcError{Code: -32601, Message: "unknown command: " + params.Name},
				})
			}

		case "skills/load":
			var params struct {
				Dir string `json:"dir"`
			}
			json.Unmarshal(req.Params, &params)
			skills, err := scanSkills(params.Dir)
			if err != nil {
				encoder.Encode(rpcResponse{
					JSONRPC: "2.0", ID: *req.ID,
					Error: &rpcError{Code: -32603, Message: err.Error()},
				})
				continue
			}
			result, _ := json.Marshal(skills)
			encoder.Encode(rpcResponse{JSONRPC: "2.0", ID: *req.ID, Result: result})

		case "shutdown":
			return

		default:
			if req.ID != nil {
				encoder.Encode(rpcResponse{
					JSONRPC: "2.0", ID: *req.ID,
					Error: &rpcError{Code: -32601, Message: "unknown method: " + req.Method},
				})
			}
		}
	}
}

// scanSkills scans a directory for <name>/<name>.md skill files.
func scanSkills(dir string) ([]skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var skills []skill
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		skillFile := filepath.Join(dir, entry.Name(), entry.Name()+".md")
		s, err := loadSkill(skillFile)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		s.Dir = filepath.Join(dir, entry.Name())
		skills = append(skills, s)
	}
	return skills, nil
}

// loadSkill reads a single skill .md file and parses its YAML
// frontmatter. The frontmatter is delimited by --- lines.
func loadSkill(path string) (skill, error) {
	f, err := os.Open(path)
	if err != nil {
		return skill{}, err
	}
	defer f.Close()

	var s skill
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		s.Content = scanner.Text() + "\n" + readRemaining(scanner)
		return s, nil
	}
	for scanner.Scan() {
		line := scanner.Text()
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
	s.Content = readRemaining(scanner)
	return s, scanner.Err()
}

func formatSkill(s skill) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Skill: %s\n", s.Name)
	fmt.Fprintf(&sb, "Directory: %s\n\n", s.Dir)
	sb.WriteString(s.Content)
	return sb.String()
}

func readRemaining(scanner *bufio.Scanner) string {
	var sb strings.Builder
	for scanner.Scan() {
		sb.WriteString(scanner.Text())
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}