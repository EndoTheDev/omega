package main

import (
	"strconv"
	"strings"
)

// compareVersions compares two version strings ("vMAJOR.MINOR.PATCH"
// with optional "-PRERELEASE") per semver precedence:
//
//   - core fields compare numerically field by field
//   - a prerelease sorts BELOW the same core without one
//     (v0.9.1-nightly.20261021 < v0.9.1)
//   - prerelease identifiers compare numerically when both are
//     numeric ("9" < "10"), alphanumerics lexically, and numeric
//     identifiers sort below alphanumeric ones
//
// ok is false when either side is not valid semver — callers must
// treat an unparseable current version (e.g. a "dev" build) as
// unknown and skip gating rather than guess a direction.
func compareVersions(a, b string) (int, bool) {
	coreA, preA, okA := splitSemver(a)
	coreB, preB, okB := splitSemver(b)
	if !okA || !okB {
		return 0, false
	}
	if c := compareCoreFields(coreA, coreB); c != 0 {
		return c, true
	}
	switch {
	case preA == "" && preB == "":
		return 0, true
	case preA == "":
		return 1, true // stable sorts above a prerelease of the same core
	case preB == "":
		return -1, true
	}
	return comparePrereleaseIDs(preA, preB), true
}

// splitSemver breaks a version into its numeric core fields and its
// (possibly empty) prerelease string. Accepts an optional leading "v".
func splitSemver(v string) (core [3]int, prerelease string, ok bool) {
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	if i := strings.IndexByte(v, '-'); i >= 0 {
		prerelease = v[i+1:]
		if prerelease == "" {
			return core, "", false
		}
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return core, "", false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return core, "", false
		}
		core[i] = n
	}
	return core, prerelease, true
}

func compareCoreFields(a, b [3]int) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// comparePrereleaseIDs applies semver dot-separated identifier
// precedence to two non-empty prerelease strings.
func comparePrereleaseIDs(a, b string) int {
	idsA := strings.Split(a, ".")
	idsB := strings.Split(b, ".")
	for i := 0; i < len(idsA) && i < len(idsB); i++ {
		if c := comparePrereleaseID(idsA[i], idsB[i]); c != 0 {
			return c
		}
	}
	// Fewer identifiers sort below more (1.0.0-alpha < 1.0.0-alpha.1).
	switch {
	case len(idsA) < len(idsB):
		return -1
	case len(idsA) > len(idsB):
		return 1
	}
	return 0
}

func comparePrereleaseID(a, b string) int {
	numA, errA := strconv.Atoi(a)
	numB, errB := strconv.Atoi(b)
	switch {
	case errA == nil && errB == nil:
		switch {
		case numA < numB:
			return -1
		case numA > numB:
			return 1
		}
		return 0
	case errA == nil:
		return -1 // numeric identifiers sort below alphanumeric
	case errB == nil:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

// isNightlyTag reports whether a release tag is a nightly prerelease
// tag. The nightly channel is the hyphen form vMAJOR.MINOR.PATCH-nightly.YYYYMMDD
// (see .github/workflows/nightly.yml), so anything carrying the
// "-nightly." marker counts; stable and other prereleases (rc, beta) do not.
func isNightlyTag(tag string) bool {
	return strings.Contains(tag, "-nightly.")
}