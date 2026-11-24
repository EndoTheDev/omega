//go:build !windows

package main

import "fmt"

// The window is Windows-first; desktopWindowSupported is the one platform gate.
func desktopWindowSupported() error {
	return fmt.Errorf("desktop is Windows-only in 0.19.0 (mac/linux follow in 0.19.x)")
}

func desktopLaunch(Config, string) error { return desktopWindowSupported() }

func spawnSetupConsole() error {
	return fmt.Errorf("the setup console spawn is Windows-only in 0.19.0")
}
