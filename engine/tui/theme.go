package tui

import (
	"bytes"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// detectSystemTheme returns "light" or "dark" based on the OS
// appearance setting. Detection is read-once; OS theme changes
// mid-session require /theme auto to re-detect.
// ponytail: read-once, not live. Upgrade path: file watch / dbus
// signal for live updates.
func detectSystemTheme() string {
	switch runtime.GOOS {
	case "windows":
		return detectWindowsTheme()
	case "darwin":
		return detectMacOSTheme()
	default:
		return detectLinuxTheme()
	}
}

// detectWindowsTheme reads AppsUseLightTheme from the registry.
func detectWindowsTheme() string {
	out, err := exec.Command("reg", "query",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`,
		"/v", "AppsUseLightTheme",
	).Output()
	if err != nil {
		return "dark"
	}
	if strings.Contains(string(out), "0x1") {
		return "light"
	}
	return "dark"
}

// detectMacOSTheme reads AppleInterfaceStyle via defaults.
// Key absent means light (macOS default).
func detectMacOSTheme() string {
	out, err := exec.Command("defaults", "read", "-g", "AppleInterfaceStyle").Output()
	if err != nil {
		return "light" // key not set = light mode
	}
	if strings.Contains(strings.ToLower(string(out)), "dark") {
		return "dark"
	}
	return "light"
}

// detectLinuxTheme checks XDG, gsettings, GTK_THEME, then COLORFGBG.
func detectLinuxTheme() string {
	// XDG Desktop Portal
	xdgCmd := exec.Command("dbus-send",
		"--session",
		"--print-reply=literal",
		"--dest=org.freedesktop.portal.Desktop",
		"/org/freedesktop/portal/desktop",
		"org.freedesktop.portal.Settings.ReadOne",
		"string:org.freedesktop.appearance",
		"string:color-scheme",
	)

	var outBuf bytes.Buffer
	xdgCmd.Stdout = &outBuf
	if err := xdgCmd.Run(); err == nil {
		fields := strings.Fields(outBuf.String())
		if len(fields) > 0 {
			switch fields[len(fields)-1] {
			case "1":
				return "dark"
			case "2":
				return "light"
			}
		}
	}

	// GNOME / Cinnamon: gsettings color-scheme
	out, err := exec.Command("gsettings", "get",
		"org.gnome.desktop.interface", "color-scheme",
	).Output()
	if err == nil {
		v := strings.TrimSpace(string(out))
		if strings.Contains(v, "dark") {
			return "dark"
		}
		if strings.Contains(v, "light") {
			return "light"
		}
	}

	// GTK-based DEs: GTK_THEME env var with :dark suffix
	if theme := os.Getenv("GTK_THEME"); strings.Contains(strings.ToLower(theme), ":dark") {
		return "dark"
	}

	// Terminal convention: COLORFGBG = "fg;bg", bg 0 = dark, 15 = light
	if bg := os.Getenv("COLORFGBG"); bg != "" {
		parts := strings.Split(bg, ";")
		if len(parts) >= 2 && parts[1] == "0" {
			return "dark"
		}
		if len(parts) >= 2 && parts[1] == "15" {
			return "light"
		}
	}

	return "dark"
}
