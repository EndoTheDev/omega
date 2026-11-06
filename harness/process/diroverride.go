// The working-directory override helper (card t_ccf74d88). Capsule
// law: harness/process imports no other module, so this carries its
// own copy of the resolve shape (rooted at the run's session
// workspace, absolute paths passing through).
package process

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/EndoTheDev/omega/core"
)

// dirOverride resolves the optional working-directory argument (card
// t_ccf74d88): an absolute path passes through cleaned; a relative one
// roots at the run's session workspace, or at the process working
// directory when the run has none. A missing or non-directory path is
// a named tool error, never a cryptic spawn chdir failure at
// start-time.
func dirOverride(ctx context.Context, args map[string]any) (string, error) {
	v, _ := args["dir"].(string)
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	abs := v
	if !filepath.IsAbs(v) {
		if root := core.RunWorkspace(ctx); root != "" {
			abs = filepath.Join(root, v)
		} else {
			a, err := filepath.Abs(v)
			if err != nil {
				return "", fmt.Errorf("dir %q: %v", v, err)
			}
			abs = a
		}
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("dir %s: %v", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("dir %s: not a directory", abs)
	}
	return abs, nil
}
