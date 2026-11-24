//go:build windows

package main

import (
	"strings"
	"testing"
)

// TestDesktopParentProcessImage pins the ruling-14 mechanism: the bare-launch routing
// reads the real parent image. The go test binary is never an Explorer child, so the
// desktop branch must not fire for it.
func TestDesktopParentProcessImage(t *testing.T) {
	image := parentProcessImage()
	if image == "" {
		t.Fatal("parentProcessImage returned nothing")
	}
	t.Logf("parent image = %s", image)
	if strings.EqualFold(image, shellParentImage) {
		t.Fatalf("the test process parent reads as %q, which is the desktop branch", image)
	}
}
