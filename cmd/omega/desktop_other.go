//go:build !windows

package main

import "fmt"

// The window is Windows-first; desktopWindowSupported is the one platform gate.
func desktopWindowSupported() error {
	return fmt.Errorf("desktop is Windows-only in 0.19.0 (mac/linux follow in 0.19.x)")
}

func desktopLaunch(Config, string) error { return desktopWindowSupported() }

// bareLaunchWantsDesktop: bare-launch routing is a Windows shell affordance only.
func bareLaunchWantsDesktop() bool { return false }

func spawnDetachedServe(string) (instanceProcess, error) {
	return nil, fmt.Errorf("the detached agent spawn is Windows-only in 0.19.0")
}

func showBootError(string) {}

func spawnSetupConsole() error {
	return fmt.Errorf("the setup console spawn is Windows-only in 0.19.0")
}
