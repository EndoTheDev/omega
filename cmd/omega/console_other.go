//go:build !windows

package main

// attachCallerConsole: only the Windows exe links GUI-subsystem; a bare launch stays the CLI loop.
func attachCallerConsole() bool { return false }
