//go:build !unix

package approval

import "os"

func appendFlags() int {
	return os.O_WRONLY | os.O_APPEND | os.O_CREATE
}
