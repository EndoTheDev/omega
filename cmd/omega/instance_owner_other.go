//go:build !windows

package main

import (
	"fmt"
	"os"
)

// The job object is the Windows lifetime mechanism. The detached spawn it would hold
// is Windows-only in 0.19.0, so no other GOOS can own an instance.
type instanceJob struct{}

func (j *instanceJob) release() {}

func (j *instanceJob) assign(*os.Process) error {
	return fmt.Errorf("the instance job object is Windows-only in 0.19.0")
}
