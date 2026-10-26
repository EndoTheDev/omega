package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

// TestCompareVersions pins the semver precedence the update gates rely
// on: nightly < stable with the same base, numeric (not string) core
// field ordering, and prerelease identifier rules.
func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int // compareVersions(a, b)
		ok   bool
	}{
		// Nightly sorts BELOW stable with the same base version.
		{"v0.9.1-nightly.20261021", "v0.9.1", -1, true},
		{"v0.9.1", "v0.9.1-nightly.20261021", 1, true},
		// A nightly user must be offer-able a newer stable...
		{"v0.9.1", "v0.9.0-n.b", 1, true},
		{"v0.9.2-nightly.20261021", "v0.9.2", -1, true},
		// Numeric core compare, not string: 0.9.2 < 0.9.10.
		{"v0.9.2", "v0.9.10", -1, true},
		{"v0.9.10", "v0.9.2", 1, true},
		{"v0.9.1", "v0.9.1", 0, true},
		{"v0.10.0", "v0.9.1", 1, true},
		{"v1.0.0", "v0.99.9", 1, true},
		// Nightly dates compare numerically.
		{"v0.9.1-nightly.20261020", "v0.9.1-nightly.20261021", -1, true},
		{"v0.9.1-nightly.20261104", "v0.9.1-nightly.20261021", 1, true},
		// Cross-base: a newer base outranks any older-base prerelease.
		{"v0.9.2-nightly.20261020", "v0.9.1", 1, true},
		// Prerelease identifier rules.
		{"v0.9.1-alpha", "v0.9.1-beta", -1, true},
		{"v0.9.1-nightly.9", "v0.9.1-nightly.10", -1, true}, // numeric, not lexicographic
		{"v0.9.1-alpha", "v0.9.1-alpha.1", -1, true},        // fewer ids sort below more
		{"v0.9.1-1", "v0.9.1-alpha", -1, true},              // numeric ids below alphanumeric
		// Unparseable inputs must not guess a direction.
		{"dev", "v0.9.1", 0, false},
		{"v0.9.1", "dev", 0, false},
		{"v0.9", "v0.9.1", 0, false},     // two-field core
		{"v0.9.1.0", "v0.9.1", 0, false}, // four-field core
		{"v0.9.1-", "v0.9.1", 0, false},  // empty prerelease
		{"va.b.c", "v0.9.1", 0, false},   // non-numeric core
	}
	for _, tt := range tests {
		got, ok := compareVersions(tt.a, tt.b)
		if got != tt.want || ok != tt.ok {
			t.Errorf("compareVersions(%q, %q) = (%d, %v), want (%d, %v)", tt.a, tt.b, got, ok, tt.want, tt.ok)
		}
	}
}

func TestIsNightlyTag(t *testing.T) {
	yes := []string{"v0.9.1-nightly.20261021", "v1.0.0-nightly.1", "v2.0.0-nightly.20261231"}
	for _, tag := range yes {
		if !isNightlyTag(tag) {
			t.Errorf("isNightlyTag(%q) = false, want true", tag)
		}
	}
	no := []string{"v0.9.1", "v0.9.2-rc.1", "v0.9.2-beta.1", "nightly", "v0.9.1-prenightly.2", "v0.9.1-NIGHTLY.x"}
	for _, tag := range no {
		if isNightlyTag(tag) {
			t.Errorf("isNightlyTag(%q) = true, want false", tag)
		}
	}
}

// TestUpdateGate pins the downgrade-refusal contract: equal skips,
// newer installs, older refuses unless explicitly allowed, unparseable
// current versions never gate.
func TestUpdateGate(t *testing.T) {
	decide := func(offered, current string, allow bool) string {
		gate, err := updateGate(offered, current, allow)
		if err != nil {
			return "error:" + err.Error()
		}
		return gate
	}
	if got := decide("v0.9.2", "v0.9.1", false); got != "install" {
		t.Errorf("newer offered: %q, want install", got)
	}
	if got := decide("v0.9.1", "v0.9.1", false); got != "skip" {
		t.Errorf("same offered: %q, want skip", got)
	}
	if got := decide("v0.9.1", "v0.9.2", false); got == "install" {
		t.Error("downgrade offered: installed, want refuse")
	}
	// Nightly → stable with the same base is an upgrade by semver.
	if got := decide("v0.9.2", "v0.9.2-nightly.20261021", false); got != "install" {
		t.Errorf("nightly->stable same base: %q, want install", got)
	}
	if got := decide("v0.9.1", "v0.9.2-nightly.20261021", false); got == "install" {
		t.Error("stable user offered older nightly: installed, want refuse")
	}
	got, err := updateGate("v0.9.1", "v0.9.2", false)
	if got != "refuse" || err == nil {
		t.Errorf("refusal must carry an explanatory error, got (%q, %v)", got, err)
	}
	if got := decide("v0.9.1", "v0.9.2", true); got != "install" {
		t.Errorf("downgrade with --allow-downgrade: %q, want install", got)
	}
	// Dev builds (unparseable current) install without gating.
	if got := decide("v0.9.1", "dev", false); got != "install" {
		t.Errorf("dev current offered newer: %q, want install", got)
	}
	if got := decide("v0.1.0", "dev", false); got != "install" {
		t.Errorf("dev current offered older: %q, want install (no gate)", got)
	}
	// Short-stamped running versions (tag-hash, card t_8acdfe17): the
	// hash is build metadata, invisible to the gate. A same-tag nightly
	// offered is "skip", the next nightly installs, an older one still
	// refuses.
	if got := decide("v0.9.1-nightly.20261022", "v0.9.1-nightly.20261022-c268097e", false); got != "skip" {
		t.Errorf("offered the running build's own tag: %q, want skip", got)
	}
	if got := decide("v0.9.1-nightly.20261023", "v0.9.1-nightly.20261022-c268097e", false); got != "install" {
		t.Errorf("next nightly offered: %q, want install (hash must not read as downgrade)", got)
	}
	if got := decide("v0.9.1-nightly.20261021", "v0.9.1-nightly.20261022-c268097e", false); got == "install" {
		t.Error("older nightly offered: installed, want refuse")
	}
	if got := decide("v0.9.2", "v0.9.1-c268097e", false); got != "install" {
		t.Errorf("stable offered to short-stamped nightly running version: %q, want install", got)
	}
}

func TestParseUpdateFlags(t *testing.T) {
	opts, err := parseUpdateFlags([]string{"--nightly", "--allow-downgrade"})
	if err != nil {
		t.Fatalf("flags: %v", err)
	}
	if !opts.nightly || !opts.allowDowngrade || opts.rollback {
		t.Errorf("opts = %+v", opts)
	}
	if _, err := parseUpdateFlags([]string{"--nighty"}); err == nil {
		t.Error("typo'd --nighty must error, not fall through to stable")
	}
	if _, err := parseUpdateFlags(nil); err != nil {
		t.Errorf("no flags: %v", err)
	}
	opts, err = parseUpdateFlags([]string{"arg.txt"})
	if err != nil || opts.nightly {
		t.Errorf("positional arg ignored: %+v, %v", opts, err)
	}
	// --yes (card t_dc91a94b): parses alone and alongside other flags;
	// near-misses must still error so a typo'd flag does not silently
	// skip the prompt.
	opts, err = parseUpdateFlags([]string{"--yes"})
	if err != nil || !opts.yes || opts.nightly || opts.rollback || opts.allowDowngrade {
		t.Errorf("--yes: opts = %+v, %v", opts, err)
	}
	opts, err = parseUpdateFlags([]string{"-y"})
	if err != nil || !opts.yes {
		t.Errorf("-y alias: opts = %+v, %v", opts, err)
	}
	opts, err = parseUpdateFlags([]string{"--nightly", "--yes"})
	if err != nil || !opts.nightly || !opts.yes {
		t.Errorf("--nightly --yes: opts = %+v, %v", opts, err)
	}
	if _, err := parseUpdateFlags([]string{"--ye"}); err == nil {
		t.Error("typo'd --ye must error, not fall through to an unprompted update")
	}
}

// TestCountNewer pins the behind-count math: newest-first list, current
// at index k yields N = k tags strictly newer. The current version's
// build hash is build metadata and never counts for or against;
// unparseable tags never count as ahead.
func TestCountNewer(t *testing.T) {
	list := []string{"v0.9.4", "v0.9.3", "v0.9.2", "v0.9.1"}
	tests := []struct {
		name    string
		tags    []string
		current string
		want    int
	}{
		{"current at index 2", list, "v0.9.2", 2},
		{"current at index 1", list, "v0.9.3", 1},
		{"current newest (index 0)", list, "v0.9.4", 0},
		{"current older than everything", list, "v0.9.0", 4},
		{"current with build hash (trimmed)", []string{"v0.9.2", "v0.9.1"}, "v0.9.1-c268097e", 1},
		{"offered equals current: no count", []string{"v0.9.1", "v0.9.0"}, "v0.9.1", 0},
		{"unparseable tag never counts", []string{"garbage", "v0.9.2"}, "v0.9.1", 1},
		{"unparseable current: nothing counts", []string{"v0.9.4", "v0.9.1"}, "dev", 0},
		{"empty list", nil, "v0.9.1", 0},
	}
	for _, tt := range tests {
		if got := countNewer(tt.tags, tt.current); got != tt.want {
			t.Errorf("%s: countNewer(%q, %q) = %d, want %d", tt.name, tt.tags, tt.current, got, tt.want)
		}
	}
}

// TestChannelTags pins the channel split the behind-count relies on:
// stable = not draft, not prerelease (what /releases/latest serves);
// nightly = the -nightly.* tags, everything else invisible.
func TestChannelTags(t *testing.T) {
	list := []githubRelease{
		{TagName: "v0.9.2"},
		{TagName: "v0.9.2-rc.1", Prerelease: true},
		{TagName: "v0.9.2-nightly.20261022", Prerelease: true},
		{TagName: "v0.9.1", Draft: true},
	}
	stable := channelTags(list, false)
	if len(stable) != 1 || stable[0] != "v0.9.2" {
		t.Errorf("stable channel = %v, want [v0.9.2] (rc + nightlies invisible, drafts invisible)", stable)
	}
	nightly := channelTags(list, true)
	if len(nightly) != 1 || nightly[0] != "v0.9.2-nightly.20261022" {
		t.Errorf("nightly channel = %v, want [v0.9.2-nightly.20261022]", nightly)
	}
}

// runUpdatePrompt drives cmdUpdate against a release fixture with the
// running version pinned to current, a scripted stdin answer, captured
// stderr and a stubbed install. terminal simulates tty-ness of the
// process the update runs in.
func runUpdatePrompt(t *testing.T, current string, flags []string, terminal bool, answer string) (stdout, stderr string, installedTag string, installErr error) {
	t.Helper()
	t.Setenv("OMEGA_UPDATE_API_BASE", "")

	oldTerm := stdinIsTerminal
	stdinIsTerminal = func() bool { return terminal }
	t.Cleanup(func() { stdinIsTerminal = oldTerm })

	oldBase := githubAPIBase
	t.Cleanup(func() { githubAPIBase = oldBase })
	githubAPIBase = fixtureReleases(t,
		rel("v0.9.2"),
		[]githubRelease{rel("v0.9.2"), rel("v0.9.1"), rel("v0.9.0")})

	oldVer := omegaVersion
	t.Cleanup(func() { omegaVersion = oldVer })
	omegaVersion = current

	// Scripted stdin: the answer, then EOF (writer closed).
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("stdin pipe: %v", pipeErr)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = oldStdin; r.Close() })
	if answer != "" {
		w.WriteString(answer)
	}
	w.Close()

	// Captured stderr and stdout.
	or, ow, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("stdout pipe: %v", pipeErr)
	}
	sr, sw, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("stderr pipe: %v", pipeErr)
	}
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	os.Stdout = ow
	os.Stderr = sw
	t.Cleanup(func() { os.Stdout = oldStdout; or.Close() })
	t.Cleanup(func() { os.Stderr = oldStderr; sr.Close() })

	// Install seam: record, never touch a binary.
	oldInstall := installArchiveFn
	t.Cleanup(func() { installArchiveFn = oldInstall })
	installArchiveFn = func(rel githubRelease, nightly bool) error {
		installedTag = rel.TagName
		return nil
	}

	installErr = cmdUpdate(flags)
	ow.Close()
	sw.Close()
	ob, _ := io.ReadAll(or)
	b, _ := io.ReadAll(sr)
	return string(ob), string(b), installedTag, installErr
}

// TestCmdUpdatePrompt pins the bare-update contract (card t_dc91a94b):
// behind report + y/N prompt; only y installs; n, junk, EOF and a
// non-tty stdin decline with the --yes hint and never touch the binary.
func TestCmdUpdatePrompt(t *testing.T) {
	// y installs, after the behind line and the prompt.
	_, stderr, tag, err := runUpdatePrompt(t, "v0.9.1", nil, true, "y\n")
	if err != nil {
		t.Fatalf("bare update, answer y: %v", err)
	}
	if tag != "v0.9.2" {
		t.Errorf("y installed %q, want v0.9.2", tag)
	}
	if !strings.Contains(stderr, "you are 1 versions behind: v0.9.1 -> v0.9.2") {
		t.Errorf("behind line missing: %q", stderr)
	}
	if !strings.Contains(stderr, "install v0.9.2 (stable channel)? [y/N]") {
		t.Errorf("prompt missing: %q", stderr)
	}

	// n declines: exit 0, no install, hint printed.
	_, stderr, tag, err = runUpdatePrompt(t, "v0.9.1", nil, true, "n\n")
	if err != nil || tag != "" {
		t.Errorf("answer n: err = %v, installed = %q, want nil/\"\"", err, tag)
	}
	if !strings.Contains(stderr, "--yes to install") {
		t.Errorf("decline hint missing: %q", stderr)
	}

	// Junk declines.
	_, _, tag, err = runUpdatePrompt(t, "v0.9.1", nil, true, "no\n")
	if err != nil || tag != "" {
		t.Errorf("answer no: err = %v, installed = %q", err, tag)
	}

	// EOF declines (empty stdin, writer closed immediately).
	_, _, tag, err = runUpdatePrompt(t, "v0.9.1", nil, true, "")
	if err != nil || tag != "" {
		t.Errorf("EOF: err = %v, installed = %q, want nil/\"\"", err, tag)
	}

	// Non-tty declines without even reading: a scripted y in the pipe
	// must NOT install (unattended bare updates never replace the
	// binary; the scripted path is --yes).
	_, stderr, tag, err = runUpdatePrompt(t, "v0.9.1", nil, false, "y\n")
	if err != nil || tag != "" {
		t.Errorf("non-tty: err = %v, installed = %q, want nil/\"\"", err, tag)
	}
	if !strings.Contains(stderr, "--yes to install") {
		t.Errorf("non-tty decline hint missing: %q", stderr)
	}
}

// TestCmdUpdateYesSkipsPrompt: --yes installs even with an empty stdin
// (EOF) — if it read stdin at all it would decline, so an install here
// proves the prompt is never consulted. The behind line still prints.
func TestCmdUpdateYesSkipsPrompt(t *testing.T) {
	_, stderr, tag, err := runUpdatePrompt(t, "v0.9.1", []string{"--yes"}, true, "")
	if err != nil {
		t.Fatalf("--yes: %v", err)
	}
	if tag != "v0.9.2" {
		t.Errorf("--yes installed %q, want v0.9.2", tag)
	}
	if !strings.Contains(stderr, "you are 1 versions behind: v0.9.1 -> v0.9.2") {
		t.Errorf("--yes must still print the behind line first: %q", stderr)
	}
	if strings.Contains(stderr, "--yes to install") {
		t.Errorf("--yes flow printed the decline hint: %q", stderr)
	}
}

// TestCmdUpdateSkipNoPrompt: already-current runs print the old
// "already up to date" line - no behind fetch, no prompt, no install.
func TestCmdUpdateSkipNoPrompt(t *testing.T) {
	stdout, _, tag, err := runUpdatePrompt(t, "v0.9.2", nil, true, "")
	if err != nil {
		t.Fatalf("skip path: %v", err)
	}
	if tag != "" {
		t.Errorf("skip path installed %q", tag)
	}
	if !strings.Contains(stdout, "already up to date") {
		t.Errorf("skip path stdout = %q, want \"already up to date\"", stdout)
	}
	if strings.Contains(stdout, "behind") || strings.Contains(stdout, "install") {
		t.Errorf("skip path must not prompt or count: %q", stdout)
	}
}
