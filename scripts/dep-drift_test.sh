#!/usr/bin/env bash
set -uo pipefail

# dep-drift_test.sh - fixture tests for scripts/dep-drift.sh.
# Run: bash scripts/dep-drift_test.sh
#
# No network: both queries are overridden with fixture input files
# (DEP_DRIFT_GO_INPUT / DEP_DRIFT_NPM_INPUT), so every case is the script's own
# math and line format. The live halves (`go list -m -u`, `npm outdated`) are
# the nightly's job, not the gate's.

SCRIPT="$(cd "$(dirname "$0")" && pwd)/dep-drift.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }

# A fixture root with go.mod (one direct, one indirect requirement) and a webui
# tree (package.json + package-lock.json) whose lockfile is the resolved side.
ROOTDIR="$(mktemp -d "${TMPDIR:-/tmp}/dep-drift-fx.XXXXXX")" || {
  echo "dep-drift_test: mktemp failed - refusing to run (fixtures would land in the repo)" >&2
  exit 1
}
[ -d "$ROOTDIR" ] || {
  echo "dep-drift_test: fixture root not created ($ROOTDIR)" >&2
  exit 1
}

mkdir -p "$ROOTDIR/webui"
cat >"$ROOTDIR/go.mod" <<'GOMOD'
module fixture.example/dep-drift

go 1.26.5

require (
	fixture.example/direct v1.0.0
	fixture.example/indirect v2.0.0 // indirect
)
GOMOD
cat >"$ROOTDIR/webui/package.json" <<'PKG'
{
  "name": "fixture-webui",
  "dependencies": { "fixture-left": "^1.0.0" },
  "devDependencies": { "fixture-dev": "^3.0.0" }
}
PKG
cat >"$ROOTDIR/webui/package-lock.json" <<'LOCK'
{
  "name": "fixture-webui",
  "lockfileVersion": 3,
  "packages": {
    "": { "name": "fixture-webui" },
    "node_modules/fixture-left": { "version": "1.0.0" },
    "node_modules/fixture-dev": { "version": "3.0.1" },
    "node_modules/nested/inner": { "version": "9.9.9" }
  }
}
LOCK

GO_IN="$ROOTDIR/go-input.txt"
NPM_IN="$ROOTDIR/npm-input.json"
: >"$GO_IN"
printf '{}\n' >"$NPM_IN"

# run_case <stdin-free> [env assignments...] -> sets RC, OUT.
# Defaults: both halves quiet (no findings). Callers write $GO_IN / $NPM_IN.
run_case() {
	OUT="$(env "$@" bash "$SCRIPT" "$ROOTDIR" 2>&1)"
	RC=$?
}

# --- version math -----------------------------------------------------------

# S1: minor inside the same major -> MINOR
printf 'fixture.example/minor|v2.42.0|v2.43.0|direct\n' >"$GO_IN"
run_case DEP_DRIFT_GO_INPUT="$GO_IN" DEP_DRIFT_NPM_INPUT="$NPM_IN"
grep -q 'fixture.example/minor .*MINOR' <<<"$OUT" && ok ||
	bad "S1: 2.43.0 vs 2.42.0 should be MINOR: $OUT"

# S2: a new major -> MAJOR, and the count carries it
printf 'fixture.example/major|v2.42.0|v3.0.0|direct\n' >"$GO_IN"
run_case DEP_DRIFT_GO_INPUT="$GO_IN" DEP_DRIFT_NPM_INPUT="$NPM_IN"
grep -q 'fixture.example/major .*MAJOR' <<<"$OUT" && ok ||
	bad "S2: 3.0.0 vs 2.42.0 should be MAJOR: $OUT"
grep -q '^DEP DRIFT: 1 outdated (1 major)$' <<<"$OUT" && ok ||
	bad "S2: verdict line should be 'DEP DRIFT: 1 outdated (1 major)': $OUT"

# S3: patch inside the same minor -> PATCH
printf 'fixture.example/patchy|v2.42.0|v2.42.1|direct\n' >"$GO_IN"
run_case DEP_DRIFT_GO_INPUT="$GO_IN" DEP_DRIFT_NPM_INPUT="$NPM_IN"
grep -q 'fixture.example/patchy .*PATCH' <<<"$OUT" && ok ||
	bad "S3: 2.42.1 vs 2.42.0 should be PATCH: $OUT"

# S4: identical -> no row, no finding
printf 'fixture.example/same|v2.42.0|v2.42.0|direct\n' >"$GO_IN"
run_case DEP_DRIFT_GO_INPUT="$GO_IN" DEP_DRIFT_NPM_INPUT="$NPM_IN"
grep -q 'fixture.example/same' <<<"$OUT" && bad "S4: identical version produced a row" || ok

# S5: a pin AHEAD of upstream is not drift (never a permanent lie)
printf 'fixture.example/ahead|v2.43.0|v2.42.0|direct\n' >"$GO_IN"
run_case DEP_DRIFT_GO_INPUT="$GO_IN" DEP_DRIFT_NPM_INPUT="$NPM_IN"
grep -q 'fixture.example/ahead' <<<"$OUT" && bad "S5: a pin ahead of upstream produced a row" || ok

# S6: pseudo-version revision bump (identical core) -> PSEUDO
printf 'fixture.example/pseudo|v0.0.0-20260101000000-aaaaaaaaaaaa|v0.0.0-20261111000000-bbbbbbbbbbbb|graph\n' >"$GO_IN"
run_case DEP_DRIFT_GO_INPUT="$GO_IN" DEP_DRIFT_NPM_INPUT="$NPM_IN"
grep -q 'fixture.example/pseudo .*PSEUDO' <<<"$OUT" && ok ||
	bad "S6: pseudo-version bump should be PSEUDO: $OUT"

# S7: prerelease -> its own release is a move forward, the least true class
printf 'fixture.example/pre|v1.2.3-rc.1|v1.2.3|direct\n' >"$GO_IN"
run_case DEP_DRIFT_GO_INPUT="$GO_IN" DEP_DRIFT_NPM_INPUT="$NPM_IN"
grep -q 'fixture.example/pre .*PATCH' <<<"$OUT" && ok ||
	bad "S7: rc.1 -> release of the same core should be PATCH: $OUT"

# --- line format ------------------------------------------------------------

# S8: n > 0 -> table + `DEP DRIFT: <n> outdated (<m> major)`, exit 0
printf '%s\n' \
	'fixture.example/major|v1.0.0|v2.0.0|direct' \
	'fixture.example/minor|v1.0.0|v1.1.0|indirect' \
	'fixture.example/patchy|v1.0.0|v1.0.1|graph' >"$GO_IN"
run_case DEP_DRIFT_GO_INPUT="$GO_IN" DEP_DRIFT_NPM_INPUT="$NPM_IN"
[ "$RC" -eq 0 ] && ok || bad "S8: findings must exit 0 (advisory), got $RC"
grep -q '^DEP DRIFT: 3 outdated (1 major)$' <<<"$OUT" && ok ||
	bad "S8: verdict line should be 'DEP DRIFT: 3 outdated (1 major)': $OUT"
grep -q '^source ' <<<"$OUT" && ok || bad "S8: the drift table header is missing"
grep -q 'fixture.example/major .*MAJOR .*direct' <<<"$OUT" && ok ||
	bad "S8: the direct kind is missing from the row: $OUT"
grep -q 'fixture.example/minor .*MINOR .*indirect' <<<"$OUT" && ok ||
	bad "S8: the indirect kind is missing from the row: $OUT"
grep -q 'fixture.example/patchy .*PATCH .*graph' <<<"$OUT" && ok ||
	bad "S8: a module the graph pulls in should read 'graph': $OUT"

# S9: n = 0 -> exactly one line, `DEP DRIFT OK`, exit 0
: >"$GO_IN"
run_case DEP_DRIFT_GO_INPUT="$GO_IN" DEP_DRIFT_NPM_INPUT="$NPM_IN"
[ "$RC" -eq 0 ] && ok || bad "S9: clean tree must exit 0, got $RC"
[ "$OUT" = "DEP DRIFT OK" ] && ok || bad "S9: expected stdout to be exactly 'DEP DRIFT OK', got: $OUT"

# --- webui half -------------------------------------------------------------

# S10: declared from package.json, resolved from the lockfile, delta from core
printf '%s\n' '{
  "fixture-left": { "wanted": "1.0.0", "latest": "2.0.0" },
  "fixture-dev":  { "wanted": "3.0.0", "latest": "3.0.1" }
}' >"$NPM_IN"
printf 'fixture.example/npm-quiet|v1.0.0|v1.0.1|direct\n' >"$GO_IN"
run_case DEP_DRIFT_GO_INPUT="$GO_IN" DEP_DRIFT_NPM_INPUT="$NPM_IN"
grep -q 'webui .*fixture-left .*\^1\.0\.0 .*2\.0\.0 .*MAJOR' <<<"$OUT" && ok ||
	bad "S10: webui row should be name|declared|latest|MAJOR: $OUT"
grep -q 'webui .*fixture-dev .*\^3\.0\.0 .*3\.0\.1 .*3\.0\.1 .*SAME' <<<"$OUT" && bad "S10: a package already at its latest produced a row" || ok

# S11: an npm query with nothing in it is a clean webui half, not a failure
printf '{}\n' >"$NPM_IN"
run_case DEP_DRIFT_GO_INPUT="$GO_IN" DEP_DRIFT_NPM_INPUT="$NPM_IN"
[ "$RC" -eq 0 ] && ok || bad "S11: empty npm findings must exit 0, got $RC"

# --- step summary -----------------------------------------------------------

# S12: GITHUB_STEP_SUMMARY gets a markdown table and the verdict count
sum="$ROOTDIR/summary.md"
: >"$sum"
printf 'fixture.example/major|v1.0.0|v2.0.0|direct\n' >"$GO_IN"
printf '{}\n' >"$NPM_IN"
run_case DEP_DRIFT_GO_INPUT="$GO_IN" DEP_DRIFT_NPM_INPUT="$NPM_IN" GITHUB_STEP_SUMMARY="$sum"
grep -q '## Dependency drift (advisory)' "$sum" && ok || bad "S12: summary heading missing"
grep -q '\*\*1 outdated, 1 major\*\*' "$sum" && ok || bad "S12: summary count line missing"
grep -q '| source | module | declared | latest | delta | kind |' "$sum" && ok ||
	bad "S12: summary table header missing: $(cat "$sum")"
grep -q '| go | `fixture.example/major` | v1.0.0 | v2.0.0 | MAJOR | direct |' "$sum" && ok ||
	bad "S12: summary table row missing: $(cat "$sum")"
grep -q '### Major bumps' "$sum" && ok || bad "S12: the major-bump section is missing from the summary"

# S13: a clean tree still reports into the summary, and says so
: >"$sum"
: >"$GO_IN"
run_case DEP_DRIFT_GO_INPUT="$GO_IN" DEP_DRIFT_NPM_INPUT="$NPM_IN" GITHUB_STEP_SUMMARY="$sum"
grep -q 'Nothing behind upstream' "$sum" && ok || bad "S13: clean-tree summary line missing"

# --- tool failure (exit 2) --------------------------------------------------

# S14: no go -> the module graph is unknowable
run_case DEP_DRIFT_GO=dep-drift-no-such-go DEP_DRIFT_NPM_INPUT="$NPM_IN"
[ "$RC" -eq 2 ] && ok || bad "S14: missing go should exit 2, got $RC"
grep -q 'go not found' <<<"$OUT" && ok || bad "S14: missing-go message did not name the cause: $OUT"

# S15: no npm -> the webui half would be unmeasured
printf 'fixture.example/one|v1.0.0|v1.0.1|direct\n' >"$GO_IN"
run_case DEP_DRIFT_GO_INPUT="$GO_IN" DEP_DRIFT_NPM=dep-drift-no-such-npm
[ "$RC" -eq 2 ] && ok || bad "S15: missing npm should exit 2, got $RC"
grep -q 'npm not found' <<<"$OUT" && ok || bad "S15: missing-npm message did not name the cause: $OUT"

# S16: a query file that does not exist is a tool failure, never a silent pass
run_case DEP_DRIFT_GO_INPUT="$ROOTDIR/no-such-input.txt" DEP_DRIFT_NPM_INPUT="$NPM_IN"
[ "$RC" -eq 2 ] && ok || bad "S16: absent go query file should exit 2, got $RC"

# S17: unparsable npm JSON -> exit 2 (a table built on guesses is worse)
printf 'not json at all\n' >"$NPM_IN"
run_case DEP_DRIFT_GO_INPUT="$GO_IN" DEP_DRIFT_NPM_INPUT="$NPM_IN"
[ "$RC" -eq 2 ] && ok || bad "S17: unparsable npm JSON should exit 2, got $RC"

# S18: webui tree without a lockfile -> the resolved side is unknowable
printf '{}\n' >"$NPM_IN"
mv "$ROOTDIR/webui/package-lock.json" "$ROOTDIR/webui/package-lock.json.away"
run_case DEP_DRIFT_GO_INPUT="$GO_IN" DEP_DRIFT_NPM_INPUT="$NPM_IN"
[ "$RC" -eq 2 ] && ok || bad "S18: missing package-lock.json should exit 2, got $RC"
grep -q 'no package-lock.json' <<<"$OUT" && ok || bad "S18: missing-lockfile message did not name the cause: $OUT"
mv "$ROOTDIR/webui/package-lock.json.away" "$ROOTDIR/webui/package-lock.json"

# --- P1: no assertion may pipe into grep -q -------------------------------
# `printf '%s' "$OUT" | grep -q` under pipefail hands back the WRITER's
# SIGPIPE when grep exits at the FIRST match, so a row that IS in the table
# reads as missing and the case goes red on a healthy SUT: nightly-only,
# timing-dependent, and the debugging run always passes. A herestring has no
# writer, so grep's early exit costs nothing. Same shape as docs-health_test
# H21; each suite pins itself.
PIPE_SHAPE='\|[[:space:]]*grep[[:space:]]+-[a-zA-Z]*q'
SELF="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"
piped="$(grep -vE '^[[:space:]]*#' "$SELF" | grep -nE "$PIPE_SHAPE")"
[ -z "$piped" ] && ok \
	|| bad "P1: an assertion pipes into grep -q (writer SIGPIPE under pipefail): $piped"
# assembled from PIPE_TOKEN, never written out: the detector scans THIS file
PIPE_TOKEN='|'
PIPE_SAMPLE="printf '%s' \"\$OUT\" $PIPE_TOKEN grep -q 'x'"
grep -qE "$PIPE_SHAPE" <<<"$PIPE_SAMPLE" && ok \
	|| bad "P1: the detector no longer sees the pre-fix pipe shape"

echo
echo "dep-drift_test: $pass ok, $fail fail"
[ "$fail" -eq 0 ]
