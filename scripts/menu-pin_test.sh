#!/usr/bin/env bash
set -uo pipefail

# menu-pin_test.sh - the negative branches of scripts/menu-pin.sh, plus the
# corpus contract itself.
#
# The pin's job is to red when a menu moves and the recipe that witnesses it does
# not, so its own failure modes are what this suite pins:
#   - the real tree is green (the acceptance: every surface the three row-menu
#     recipes drive still agrees with its builder)
#   - a moved label REDS, and the message names the builder, the surface and both
#     sequences (the whole point: it has to tell the next worker which half moved)
#   - a reordered pair REDS (order is the contract, not the label set)
#   - a stat the recipe CLICKS by name reds when the builder renames it, and a
#     stat ADDED to the catalog reds the row count (117 asserts nine rows)
#   - a shape the recipes do not drive stays FREE (a running session row is not
#     asserted by 117, so its Stop row may move without a red)
#   - a witness phrase that moved is loud (exit 2, never a silent pass), and a
#     label computed by a call the pin does not model is loud too
#   - usage errors exit 2
#
# Nothing here runs a browser or a lane: both halves are read as source.

PIN="$(cd "$(dirname "$0")" && pwd)/menu-pin.sh"
REPO="$(cd "$(dirname "$0")/.." && pwd)"

# winbtw seam: the fixtures live under a Windows-visible temp dir (native python3
# cannot stat an MSYS /tmp path once the tree is handed to it by --root).
if command -v cygpath >/dev/null 2>&1 && [ -n "${LOCALAPPDATA:-}" ]; then
  WINTMP="$(cygpath -m "$LOCALAPPDATA/Temp" 2>/dev/null || true)"
  [ -n "$WINTMP" ] && [ -d "$WINTMP" ] && export TMPDIR="$WINTMP"
fi
T="$(mktemp -d "${TMPDIR:-/tmp}/menu-pin-test.XXXXXX")" || exit 1
trap 'rm -rf "$T"' EXIT

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }

# --- the pin: no line here may pipe a writer into an early-exit grep --------
# `printf '%s' "$OUT" | grep -q` under pipefail hands back the WRITER's SIGPIPE
# when grep leaves at the first match, so a needle that IS present reads as
# missing. A herestring has no writer.
_PIN_SHAPE='\|[[:space:]]*grep[[:space:]]+-[a-zA-Z]*q'
_PIN_SELF="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
_pin_hits="$(grep -vE '^[[:space:]]*#' "$_PIN_SELF" | grep -nE "$_PIN_SHAPE" || true)"
[ -z "$_pin_hits" ] && ok \
	|| bad "PIN: an assertion pipes a writer into grep -q (SIGPIPE under pipefail): $_pin_hits"

# fixture <dir>: a tree carrying every builder the pin reads and the three recipes
fixture() {
  local d=$1 src dst
  for src in "$REPO"/harness/*/ui/*Menu.ts \
             "$REPO"/webui/app/components/shell/statusStats.ts \
             "$REPO"/scripts/journeys/recipes/117-right-click-surfaces.sh \
             "$REPO"/scripts/journeys/recipes/119-wire-only-menus.sh \
             "$REPO"/scripts/journeys/recipes/120-capsule-row-menus.sh; do
    [ -f "$src" ] || { echo "fixture: missing $src" >&2; exit 1; }
    dst="$d/${src#"$REPO"/}"
    mkdir -p "$(dirname "$dst")"
    cp "$src" "$dst"
  done
}

# swap_lines <file> <needle-a> <needle-b>: exchange the two lines that carry them
swap_lines() {
  python3 - "$1" "$2" "$3" <<'PY'
import sys

path, first, second = sys.argv[1:4]
lines = open(path, encoding="utf-8").read().split("\n")
a = next(i for i, line in enumerate(lines) if first in line)
b = next(i for i, line in enumerate(lines) if second in line)
lines[a], lines[b] = lines[b], lines[a]
open(path, "w", encoding="utf-8").write("\n".join(lines))
PY
}

run_pin() { bash "$PIN" "$@" 2>&1; }

# 1. THE ACCEPTANCE: the real tree agrees
out=$(run_pin); rc=$?
if [ "$rc" -ne 0 ]; then bad "the real tree is not pinned: exit $rc | $out"
elif ! grep -q 'surfaces and .* recipe assertions agree' <<<"$out"; then
  bad "the real tree pin printed no verdict: $out"
else ok; fi

# 2. a moved label reds, and the message names the builder, the surface and both
#    sequences (both witnesses of the surface red, not just the first)
D="$T/moved"
fixture "$D"
sed -i 's/label: "Branch"/label: "Fork"/' "$D/harness/store/ui/sessionMenu.ts"
out=$(run_pin --root "$D"); rc=$?
if [ "$rc" -ne 1 ]; then bad "a moved label exited $rc (want 1) | $out"
elif ! grep -q 'harness/store/ui/sessionMenu.ts' <<<"$out"; then
  bad "the red does not name the builder: $out"
elif ! grep -q 'session row' <<<"$out"; then
  bad "the red does not name the surface: $out"
elif ! grep -q 'New session|Rename|Fork|' <<<"$out"; then
  bad "the red does not print the builder's new sequence: $out"
elif ! grep -q 'New session|Rename|Branch|' <<<"$out"; then
  bad "the red does not print the recipe's stale sequence: $out"
elif [ "$(printf '%s\n' "$out" | grep -c 'the menu moved and the recipe did not')" != "2" ]; then
  bad "a moved label must red BOTH witnesses of the surface: $out"
else ok; fi

# 3. a reordered pair reds (the sequence is order, not a set)
D="$T/reordered"
fixture "$D"
swap_lines "$D/harness/store/ui/sessionMenu.ts" \
  'id: "session:branch"' 'id: "session:export"'
out=$(run_pin --root "$D"); rc=$?
if [ "$rc" -ne 1 ]; then bad "a reordered pair exited $rc (want 1) | $out"
elif ! grep -q 'New session|Rename|Export|Branch|' <<<"$out"; then
  bad "the reorder red does not print the builder's sequence: $out"
else ok; fi

# 4. a stat the recipe clicks by name: renaming it reds, because click_row would
#    die at runtime on a row the builder no longer renders
D="$T/clicked-stat"
fixture "$D"
sed -i 's/label: "Message count"/label: "Message tally"/' \
  "$D/webui/app/components/shell/statusStats.ts"
out=$(run_pin --root "$D"); rc=$?
if [ "$rc" -ne 1 ]; then bad "a renamed clicked stat exited $rc (want 1) | $out"
elif ! grep -q 'webui/app/components/shell/statusStats.ts' <<<"$out"; then
  bad "the click red does not name the builder: $out"
elif ! grep -q 'Message count' <<<"$out"; then
  bad "the click red does not name the label the recipe clicks: $out"
else ok; fi

# 5. a stat ADDED to the catalog reds the row count 117 asserts
D="$T/extra-stat"
fixture "$D"
sed -i 's|^  { id: "version", label: "Build version" },|  { id: "version", label: "Build version" },\n  { id: "lane", label: "Lane uptime" },|' \
  "$D/webui/app/components/shell/statusStats.ts"
grep -q 'Lane uptime' "$D/webui/app/components/shell/statusStats.ts" \
  || bad "the extra-stat mutation never landed"
out=$(run_pin --root "$D"); rc=$?
if [ "$rc" -ne 1 ]; then bad "an added stat exited $rc (want 1) | $out"
elif ! grep -q 'renders 10 rows' <<<"$out"; then
  bad "the added-stat red does not state the new row count: $out"
else ok; fi

# 6. a shape no recipe drives stays free: 117 never asserts a RUNNING session row,
#    so its Stop label may move without the pin demanding a recipe edit
D="$T/free-shape"
fixture "$D"
sed -i 's/label: "Stop"/label: "Halt"/' "$D/harness/store/ui/sessionMenu.ts"
grep -q 'label: "Halt"' "$D/harness/store/ui/sessionMenu.ts" || bad "the free-shape mutation never landed"
out=$(run_pin --root "$D"); rc=$?
if [ "$rc" -ne 0 ]; then bad "an unasserted shape red the pin: exit $rc | $out"
else ok; fi

# 7. a witness phrase that moved is loud (the pin lost its line, never a pass)
D="$T/anchor"
fixture "$D"
sed -i 's/an idle session row menus/an idle session row shows/' \
  "$D/scripts/journeys/recipes/117-right-click-surfaces.sh"
out=$(run_pin --root "$D"); rc=$?
if [ "$rc" -ne 2 ]; then bad "a lost witness phrase exited $rc (want 2) | $out"
elif ! grep -q '117-right-click-surfaces.sh' <<<"$out"; then
  bad "the lost-witness message does not name the recipe: $out"
elif ! grep -q 'an idle session row menus' <<<"$out"; then
  bad "the lost-witness message does not quote the phrase: $out"
else ok; fi

# 8. a label the pin cannot evaluate is loud, not a silent skip
D="$T/computed"
fixture "$D"
sed -i 's/label: "Open",/label: pick(),/' "$D/harness/mcp/ui/mcpMenu.ts"
grep -q 'label: pick()' "$D/harness/mcp/ui/mcpMenu.ts" || bad "the computed-label mutation never landed"
out=$(run_pin --root "$D"); rc=$?
if [ "$rc" -ne 2 ]; then bad "a computed label exited $rc (want 2) | $out"
elif ! grep -q 'extend the pin' <<<"$out"; then
  bad "the computed-label message does not ask for the pin to be extended: $out"
else ok; fi

# 9. usage errors are loud
bash "$PIN" --nonsense >/dev/null 2>&1; rc=$?
[ "$rc" -eq 2 ] && ok || bad "unknown option exited $rc, want 2"
bash "$PIN" --root "$T/does-not-exist" >/dev/null 2>&1; rc=$?
[ "$rc" -eq 2 ] && ok || bad "a missing root exited $rc, want 2"
bash "$PIN" --root >/dev/null 2>&1; rc=$?
[ "$rc" -eq 2 ] && ok || bad "--root with no value exited $rc, want 2"

printf 'menu-pin_test: %s passed, %s failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ] || exit 1
exit 0
