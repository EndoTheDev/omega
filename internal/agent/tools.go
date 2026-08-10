package agent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// NewRegistry returns the built-in tools the model may call during a
// conversation, keyed by tool name.
func NewRegistry() map[string]Tool {
	return map[string]Tool{
		"shell": {
			Description: "Run a shell command and return its stdout and stderr.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string", "description": "The command to run."},
				},
				"required": []string{"command"},
			},
			Run: runShell,
		},
		"read_file": {
			Description: "Read a file, returning its contents with line numbers.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":   map[string]any{"type": "string", "description": "Path to the file to read."},
					"offset": map[string]any{"type": "integer", "description": "1-based first line to read (default 1)."},
					"limit":  map[string]any{"type": "integer", "description": "Max lines to read (default all)."},
				},
				"required": []string{"path"},
			},
			Run: runReadFile,
		},
		"write_file": {
			Description: "Write content to a file, creating parent directories as needed.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "Path of the file to write."},
					"content": map[string]any{"type": "string", "description": "Full content to write."},
				},
				"required": []string{"path", "content"},
			},
			Run: runWriteFile,
		},
		"search_files": {
			Description: "Search file contents or file names under a directory.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern":   map[string]any{"type": "string", "description": "Regex to match."},
					"target":    map[string]any{"type": "string", "description": "content or files (default content)."},
					"path":      map[string]any{"type": "string", "description": "Directory to search (default .)."},
					"file_glob": map[string]any{"type": "string", "description": "Only search files whose name matches this regex."},
				},
				"required": []string{"pattern"},
			},
			Run: runSearchFiles,
		},
	}
}

// argString extracts a string argument, returning a clear error when it is
// missing or not a string.
func argString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required argument %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string, got %T", key, v)
	}
	return s, nil
}

// runShell executes a command through the platform shell and returns its
// combined stdout and stderr.
func runShell(ctx context.Context, args map[string]any) (string, error) {
	command, err := argString(args, "command")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("command must not be empty")
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("command failed: %w", err)
	}
	return string(out), nil
}

// runReadFile reads a file and returns its contents with line numbers.
// offset is 1-based; limit caps the number of lines returned.
func runReadFile(_ context.Context, args map[string]any) (string, error) {
	path, err := argString(args, "path")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path must not be empty")
	}

	offset := 1
	if v, ok := args["offset"]; ok {
		f, ok := v.(float64)
		if !ok {
			return "", fmt.Errorf("argument %q must be an integer", "offset")
		}
		offset = int(f)
		if offset < 1 {
			return "", fmt.Errorf("offset must be >= 1")
		}
	}
	limit := 0
	if v, ok := args["limit"]; ok {
		f, ok := v.(float64)
		if !ok {
			return "", fmt.Errorf("argument %q must be an integer", "limit")
		}
		limit = int(f)
		if limit < 0 {
			return "", fmt.Errorf("limit must be >= 0")
		}
	}

	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	var out strings.Builder
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		if line < offset {
			continue
		}
		if limit > 0 && line >= offset+limit {
			break
		}
		fmt.Fprintf(&out, "%d|%s\n", line, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return out.String(), nil
}

// runWriteFile writes content to a file, creating parent directories.
func runWriteFile(_ context.Context, args map[string]any) (string, error) {
	path, err := argString(args, "path")
	if err != nil {
		return "", err
	}
	content, err := argString(args, "content")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path must not be empty")
	}

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create dir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(content), path), nil
}

// runSearchFiles searches file contents (target "content") or file names
// (target "files") under a directory using a regex pattern.
func runSearchFiles(_ context.Context, args map[string]any) (string, error) {
	pattern, err := argString(args, "pattern")
	if err != nil {
		return "", err
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid pattern: %w", err)
	}

	target := "content"
	if v, ok := args["target"]; ok {
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("argument %q must be a string", "target")
		}
		target = s
	}
	if target != "content" && target != "files" {
		return "", fmt.Errorf("target must be %q or %q, got %q", "content", "files", target)
	}

	root := "."
	if v, ok := args["path"]; ok {
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("argument %q must be a string", "path")
		}
		root = s
	}

	var glob *regexp.Regexp
	if v, ok := args["file_glob"]; ok {
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("argument %q must be a string", "file_glob")
		}
		glob, err = regexp.Compile(s)
		if err != nil {
			return "", fmt.Errorf("invalid file_glob: %w", err)
		}
	}

	var out strings.Builder
	matches := 0
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			return nil
		}
		if glob != nil && !glob.MatchString(d.Name()) {
			return nil
		}
		if target == "files" {
			if re.MatchString(d.Name()) {
				fmt.Fprintln(&out, path)
				matches++
			}
			return nil
		}
		// content search
		file, openErr := os.Open(path)
		if openErr != nil {
			return nil
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		line := 0
		for scanner.Scan() {
			line++
			if re.MatchString(scanner.Text()) {
				fmt.Fprintf(&out, "%s:%d:%s\n", path, line, scanner.Text())
				matches++
			}
		}
		file.Close()
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walk %s: %w", root, err)
	}
	if matches == 0 {
		return "no matches", nil
	}
	return out.String(), nil
}
