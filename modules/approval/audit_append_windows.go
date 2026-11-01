//go:build !unix

package approval

import "os"

// appendFlags: the non-unix answer (Windows has no O_NOFOLLOW).
func appendFlags() int {
	return os.O_WRONLY | os.O_APPEND | os.O_CREATE
}
