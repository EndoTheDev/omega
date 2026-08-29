package tui

import (
	"sort"
	"testing"
)

func TestThemeNames(t *testing.T) {
	names := themeNames()

	if len(names) == 0 {
		t.Fatal("themeNames() returned empty slice, expected at least one theme")
	}

	has := func(target string) bool {
		for _, n := range names {
			if n == target {
				return true
			}
		}
		return false
	}
	if !has("dark") {
		t.Errorf("themeNames() = %v, missing \"dark\"", names)
	}
	if !has("light") {
		t.Errorf("themeNames() = %v, missing \"light\"", names)
	}

	if !sort.StringsAreSorted(names) {
		t.Errorf("themeNames() = %v, not sorted", names)
	}
}