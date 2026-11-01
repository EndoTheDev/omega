//go:build unix

package approval

import (
	"os"
	"syscall"
)

// appendFlags includes O_NOFOLLOW where the OS defines it: the audit
// file's mode is 0600 and its path is under omega home - a symlink
// planted over it must not be followed. Windows carries no O_NOFOLLOW;
// the sibling file answers for it.
func appendFlags() int {
	return os.O_WRONLY | os.O_APPEND | os.O_CREATE | syscall.O_NOFOLLOW
}
