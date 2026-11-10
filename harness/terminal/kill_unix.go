//go:build unix

package terminal

import (
	"os/exec"

	"github.com/EndoTheDev/omega/agent"
)

// shellKillSetup is the shell tool's instance of the shared
// process-group kill (agent.SetupProcessGroupKill).
func shellKillSetup(cmd *exec.Cmd) { agent.SetupProcessGroupKill(cmd) }
