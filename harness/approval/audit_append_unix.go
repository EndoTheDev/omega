//go:build unix

package approval

import (
	"os"
	"syscall"
)

// appendFlags adds O_NOFOLLOW: a symlink planted over the 0600 audit file must not be followed.
func appendFlags() int {
	return os.O_WRONLY | os.O_APPEND | os.O_CREATE | syscall.O_NOFOLLOW
}
