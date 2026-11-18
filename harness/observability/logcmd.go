package observability

// Module-owned command surface.

import (
	"context"
	"fmt"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// defaultLogLines is the /logs line count.
const defaultLogLines = 200

// logsHandler tails the log file the logger carries.
func logsHandler(ctx *core.Context) func(context.Context, string, string) (core.CommandResult, error) {
	return func(_ context.Context, name, args string) (core.CommandResult, error) {
		if name != "/logs" {
			return core.CommandResult{}, core.CommandMiss(name)
		}
		path := ""
		if ctx != nil {
			if p, ok := ctx.Services().Resolve("logger").(interface{ Path() string }); ok {
				path = p.Path()
			}
		}
		if path == "" {
			return core.CommandResult{}, fmt.Errorf("log file not configured")
		}
		fields := strings.Fields(args)
		if len(fields) > 0 && fields[0] == "-f" {
			return core.CommandResult{}, fmt.Errorf("/logs -f is gone with the terminal shell - the webui tail follows live")
		}
		lines, err := tailLines(path, defaultLogLines)
		if err != nil {
			return core.CommandResult{}, fmt.Errorf("tail log: %w", err)
		}
		return core.CommandResult{Text: "[logs: " + path + "]\n" + strings.Join(lines, "\n")}, nil
	}
}
