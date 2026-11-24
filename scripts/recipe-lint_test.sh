#!/usr/bin/env bash
set -uo pipefail

# recipe-lint_test.sh - the negative branches of scripts/recipe-lint.sh, plus
# the corpus contract itself (card t_fdd12843).
#
# The lint's job is to fail a recipe that asserts on elapsed time, so its own
# failure modes are what this suite pins:
#   - a fixed sleep IS flagged (the rule that catches the next regression)
#   - a poll interval inside a bounded loop is NOT flagged (or the rule would
#     forbid the very shape it asks for and every recipe would need a marker)
#   - a bounded for-loop that polls a JS read on an iteration count IS
#     unbudgeted-poll, and the same wait routed through a poll primitive is
#     clean; the debt ledger's count is a ratchet (a listed recipe may not gain
#     a site, an entry that outlives its site is ledger-rot). The ledger is
#     empty once every recipe converts: growth is then proven by the unlisted
#     fixture (case 19), and rot has no entry to outlive, so it reports itself
#     unprovable instead of passing silently or being deleted
#   - a here-doc body full of `for`/`done`/`sleep` is opaque (a python block is
#     not bash - a parser that reads it drifts and silently stops judging)
#   - a `for ... in a b done; do` header does not close the loop it opens (the
#     one line in the corpus that made 426 findings out of a clean file)
#   - a justification must carry a reason, and a marker whose sleep has gone is
#     reported, because both are how an allow rots into a suppression
#   - --advisory reports without failing; a bad arg / missing file exits 2
#   - the REAL corpus lints clean: the acceptance for this card, pinned so the
#     next sleep added to a recipe reds here instead of in the nightly.
#
# The lint is grep-class and advisory-capable: it never runs a browser, and
# nothing here does either.

LINT="$(cd "$(dirname "$0")" && pwd)/recipe-lint.sh"
CORPUS="$(cd "$(dirname "$0")" && pwd)/journeys/recipes"

# winbtw seam: the fixtures live under a Windows-visible temp dir (mktemp on a
# bare /tmp is fine for bash but the lint is driven by MSYS awk, which follows
# the same paths - keep them native so a later tool can read them too).
if command -v cygpath >/dev/null 2>&1 && [ -n "${LOCALAPPDATA:-}" ]; then
  WINTMP="$(cygpath -m "$LOCALAPPDATA/Temp" 2>/dev/null || true)"
  [ -n "$WINTMP" ] && [ -d "$WINTMP" ] && export TMPDIR="$WINTMP"
fi
T="$(mktemp -d "${TMPDIR:-/tmp}/recipe-lint-test.XXXXXX")" || exit 1
trap 'rm -rf "$T"' EXIT

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }
# skip: a case the current corpus state cannot exercise. Loud, and never a
# silent pass - the run's counts stay honest about what was actually proven.
skip() { echo "SKIP: $1" >&2; }

# --- the pin: no line here may pipe a writer into an early-exit grep --------
# `printf '%s' "$OUT" | grep -q` under pipefail hands back the WRITER's
# SIGPIPE when grep leaves at the first match, so a needle that IS present
# reads as missing - and the negated form reads it as absent, which is a false
# green that hides a defect. A herestring has no writer, so grep's early exit
# costs nothing. This file scans its own live lines; comments may name the
# shape, so they are skipped.
_PIN_SHAPE='\|[[:space:]]*grep[[:space:]]+-[a-zA-Z]*q'
_PIN_SELF="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
_pin_hits="$(grep -vE '^[[:space:]]*#' "$_PIN_SELF" | grep -nE "$_PIN_SHAPE" || true)"
[ -z "$_pin_hits" ] && ok \
	|| bad "PIN: an assertion pipes a writer into grep -q (SIGPIPE under pipefail): $_pin_hits"
_PIN_TOKEN='|'
_pin_sample="  printf '%s\\n' \"\$OUT\" $_PIN_TOKEN grep -qxF -- \"\$1\""
grep -qE "$_PIN_SHAPE" <<<"$_pin_sample" && ok \
	|| bad "PIN: the detector no longer sees the pre-fix pipe shape"


# expect_clean <name> <file>: exit 0 and no findings
expect_clean() {
  local out rc
  out=$(bash "$LINT" "$2" 2>&1); rc=$?
  if [ "$rc" -ne 0 ]; then bad "$1: exit $rc (want 0) | $out"
  elif [ -n "$out" ]; then bad "$1: findings on a clean file: $out"
  else ok; fi
}

# expect_finding <name> <file> <rule> <line>: exit 1 and exactly that finding
expect_finding() {
  local out rc got
  out=$(bash "$LINT" "$2" 2>&1); rc=$?
  if [ "$rc" -ne 1 ]; then bad "$1: exit $rc (want 1) | $out"; return; fi
  got=$(printf '%s\n' "$out" | grep -c "^$2:$4: $3:")
  if [ "$got" != "1" ]; then bad "$1: want one '$3' finding at $2:$4, got: $out"
  else ok; fi
}

# 1. a clean recipe: polls and settles only
cat > "$T/clean.sh" <<'EOF'
#!/bin/bash
. "${JROOT:?}/lib.sh"
open_app
poll_js '!!document.querySelector(".msg")' 10
wait_settled 'document.querySelector(".msg").offsetHeight' 10
i=0
while [ "$i" -lt 20 ]; do
  sleep 0.5
  i=$((i + 1))
done
assert_eq "$(jsq '1')" "1" "clean"
EOF
expect_clean "a poll/settle-only recipe is clean" "$T/clean.sh"

# 2. a fixed sleep at statement level
cat > "$T/sleep.sh" <<'EOF'
#!/bin/bash
. "${JROOT:?}/lib.sh"
open_app
ab mouse down
sleep 2
EOF
expect_finding "a fixed sleep is flagged" "$T/sleep.sh" fixed-sleep 5

# 3. a sleep inside a bounded loop is the sanctioned poll interval
cat > "$T/loop.sh" <<'EOF'
#!/bin/bash
. "${JROOT:?}/lib.sh"
open_app
for i in 1 2 3; do
  sleep 0.25
done
EOF
expect_clean "a loop's poll interval is not a fixed sleep" "$T/loop.sh"

# 4. the same, on ONE line (the divider-drag shape in recipes 75/77/78)
cat > "$T/oneline.sh" <<'EOF'
#!/bin/bash
. "${JROOT:?}/lib.sh"
open_app
x=10; while [ "$x" -le 200 ]; do ab mouse move "$x" 400; sleep 0.03; x=$((x + 10)); done
EOF
expect_clean "a one-line loop's interval is not a fixed sleep" "$T/oneline.sh"

# 5. a sleep in front of a count is the 17s-window class
cat > "$T/window.sh" <<'EOF'
#!/bin/bash
. "${JROOT:?}/lib.sh"
open_app
sleep 17
polled=$(jsq '1')
assert_eq "$polled" "0" "no polling fetch"
EOF
expect_finding "a fixed window grading a read is count-window" "$T/window.sh" count-window 4

# 6. an inline justification clears the finding
cat > "$T/justified.sh" <<'EOF'
#!/bin/bash
. "${JROOT:?}/lib.sh"
open_app
ab mouse move 10 10
sleep 0.15   # recipe-lint: allow fixed-sleep - input pacing: move and press coalesce in one task
ab mouse down
EOF
expect_clean "an inline justification clears the finding" "$T/justified.sh"

# 7. the same marker on the line above
cat > "$T/justified-above.sh" <<'EOF'
#!/bin/bash
. "${JROOT:?}/lib.sh"
open_app
ab mouse move 10 10
# recipe-lint: allow fixed-sleep - input pacing: move and press coalesce in one task
sleep 0.15
ab mouse down
EOF
expect_clean "a marker on the line above clears the finding" "$T/justified-above.sh"

# 8. a marker with no reason is itself a finding
cat > "$T/bare.sh" <<'EOF'
#!/bin/bash
. "${JROOT:?}/lib.sh"
open_app
sleep 2   # recipe-lint: allow fixed-sleep -
EOF
expect_finding "a justification with no reason is reported" "$T/bare.sh" bare-justification 4

# 9. a marker whose sleep has gone (the line was converted)
cat > "$T/stale.sh" <<'EOF'
#!/bin/bash
. "${JROOT:?}/lib.sh"
open_app
# recipe-lint: allow fixed-sleep - this pause was removed when the wait became a poll
wait_js 'true' 5
EOF
expect_finding "a marker on a line with no sleep is stale" "$T/stale.sh" stale-allow 5

# 10. a recipe that opens nothing and declares nothing
cat > "$T/noclean.sh" <<'EOF'
#!/bin/bash
. "${JROOT:?}/lib.sh"
curl -s "$JOURNEY_BASE_URL/health" >/dev/null
EOF
expect_finding "a browserless recipe without a declaration is flagged" "$T/noclean.sh" no-cleanup 1

# 11. the declaration clears it (REST-only recipes 76/85/88/90/94/95)
cat > "$T/nobrowser.sh" <<'EOF'
#!/bin/bash
# recipe-lint: allow no-browser - this recipe drives REST only and never opens a page
. "${JROOT:?}/lib.sh"
curl -s "$JOURNEY_BASE_URL/health" >/dev/null
EOF
expect_clean "a declared browserless recipe is clean" "$T/nobrowser.sh"

# 12. a here-doc body is opaque: python's `for`/`done`/sleep are not bash blocks
cat > "$T/heredoc.sh" <<'EOF'
#!/bin/bash
. "${JROOT:?}/lib.sh"
open_app
python3 - "$f" <<'PY'
for line in open("x"):
    if line:
        pass
done = 1
sleep = 2
PY
poll_js 'true' 5
EOF
expect_clean "a here-doc body is not parsed as bash" "$T/heredoc.sh"

# 13. a quoted JS probe does not open a bash loop (a `for (` in a probe string)
cat > "$T/quoted.sh" <<'EOF'
#!/bin/bash
. "${JROOT:?}/lib.sh"
open_app
jsq '(()=>{for (const x of []) {}; return 1})()'
sleep 2
EOF
expect_finding "a for( inside a probe string opens no loop" "$T/quoted.sh" fixed-sleep 5

# 14. `done` as a for-list item must not close the loop it sits in
cat > "$T/donelist.sh" <<'EOF'
#!/bin/bash
. "${JROOT:?}/lib.sh"
open_app
for st in triage todo done; do
  archive_cards "$st"
done
sleep 2
EOF
out=$(bash "$LINT" "$T/donelist.sh" 2>&1); rc=$?
if [ "$rc" -ne 1 ]; then bad "done-as-list-item: exit $rc (want 1) | $out"
elif grep -q unbalanced <<<"$out"; then bad "done-as-list-item: the parser drifted | $out"
elif [ "$(printf '%s\n' "$out" | grep -c 'fixed-sleep')" != "1" ]; then bad "done-as-list-item: want one finding | $out"
else ok; fi

# 15. --advisory reports and never fails
out=$(bash "$LINT" --advisory "$T/sleep.sh" 2>&1); rc=$?
if [ "$rc" -ne 0 ]; then bad "--advisory exited $rc (want 0)"
elif ! grep -q 'fixed-sleep' <<<"$out"; then bad "--advisory dropped the findings: $out"
else ok; fi

# 16. usage errors are loud: unknown option and a missing file both exit 2
if bash "$LINT" --nonsense >/dev/null 2>&1; then bad "unknown option was accepted"
elif [ $? -eq 2 ]; then ok; else bad "unknown option exited $?, want 2"; fi
if bash "$LINT" "$T/does-not-exist.sh" >/dev/null 2>&1; then bad "a missing file was accepted"
elif [ $? -eq 2 ]; then ok; else bad "missing file exited $?, want 2"; fi

# 17. no arguments means the corpus (the default target)
if bash "$LINT" >/dev/null 2>&1; then ok; else bad "the default corpus lint exited $? (findings in scripts/journeys/recipes)"; fi

# 18. THE ACCEPTANCE: every recipe in the corpus lints clean
if [ -d "$CORPUS" ]; then
  out=$(bash "$LINT" "$CORPUS"/*.sh 2>&1); rc=$?
  if [ "$rc" -ne 0 ]; then bad "the recipe corpus is not determinism-clean: $out"
  else ok; fi
else
  bad "no recipe corpus at $CORPUS"
fi

# --- the poll-bound ratchet (rule 4) ----------------------------------------
# A bounded for-loop is an iteration count, so a wait built from one is a
# timeout in disguise: 50-sessions-reload's reload-bucket read was
# `for i in 1..10; do out=$(jsq ...); sleep 1; done` - a hard 10s bound that
# greens solo and reds on a loaded host. The pre-fix shape must red, a poll
# routed through a primitive must stay clean, and the debt ledger must catch
# both a new site in a listed recipe and a listing that outlived its sites.

# 19. the pre-fix shape: a hand-rolled bounded loop reading JS
cat > "$T/unbudgeted.sh" <<'EOF'
#!/bin/bash
. "${JROOT:?}/lib.sh"
open_app
pinned=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  pinned=$(jsq '(()=>1)()' 2>&1)
  [ "$pinned" = "1" ] && break
  sleep 1
done
assert_eq "$pinned" "1" "pin survives reload"
EOF
expect_finding "a bounded for-loop polling a JS read is unbudgeted-poll" "$T/unbudgeted.sh" unbudgeted-poll 5

# 20. the budgeted shape: the same waits routed through the primitives
cat > "$T/budgeted.sh" <<'EOF'
#!/bin/bash
. "${JROOT:?}/lib.sh"
open_app
pinned=$(poll_js_value '(()=>1)()' -eq 1)
assert_eq "$pinned" "1" "pin survives reload"
for i in 1 2 3; do
  wait_js 'true' 5 && break
done
EOF
expect_clean "a poll routed through poll_js_value / wait_js is clean" "$T/budgeted.sh"

# 21-22. the ledger: a listed recipe may not GAIN a site, and an entry that
# outlives its sites is rot (an allowance must not survive its own conversion)
LEDGER="$(cd "$(dirname "$0")" && pwd)/journeys/recipe-lint.legacy"
read -r LEDGERED LEDGER_N <<<"$(awk '$1 !~ /^#/ && NF >= 2 {print $1, $2}' "$LEDGER" 2>/dev/null | sort -k2 -nr | head -1)"
if [ -n "${LEDGERED:-}" ] && [ -n "${LEDGER_N:-}" ]; then
  mkdir -p "$T/ledgered" "$T/rotted"
  python3 - "$T/ledgered/$LEDGERED" "$T/rotted/$LEDGERED" "$LEDGER_N" <<'PY'
import sys

grown, rotted, n = sys.argv[1], sys.argv[2], int(sys.argv[3])
one = "for i in 1 2 3; do v=$(jsq '1' 2>&1); [ \"$v\" = \"1\" ] && break; sleep 1; done\n"
head = '#!/bin/bash\n. "${JROOT:?}/lib.sh"\nopen_app\n'
with open(grown, "w", encoding="utf-8", newline="") as fh:
    fh.write(head + one * (n + 1))
with open(rotted, "w", encoding="utf-8", newline="") as fh:
    fh.write(head + one * (n - 1))
PY
  expect_finding "a listed recipe that gains a site reds" "$T/ledgered/$LEDGERED" unbudgeted-poll $((LEDGER_N + 4))
  if [ "$LEDGER_N" -gt 0 ]; then
    expect_finding "an entry that outlives its sites is ledger-rot" "$T/rotted/$LEDGERED" ledger-rot 1
  else
    bad "the ledger names no site count - the rot case cannot run"
  fi
else
  # The ledger is EMPTY: every recipe converted, so no allowance is left to
  # grow into and no entry is left to outlive its sites. Growth is still proven
  # against exactly this state - with no entry, an unlisted recipe's first site
  # is over an allowance of zero (case 19). Rot needs a positive count to
  # outlive, so it reports itself unprovable here; the case and the mechanism
  # stay, live again the moment an entry returns. What this state CAN assert is
  # the retirement's own shape: the file is still there, header and all,
  # because that format line is what the lint reads.
  skip "the ledger is empty - the rot case is unprovable until an entry returns (growth stays proven by case 19)"
  if [ -s "$LEDGER" ] && grep -q '^# Format: <recipe file> <count of unbudgeted-poll sites>$' "$LEDGER"; then
    ok
  else
    bad "the empty ledger lost its file or its header - the lint reads that format line"
  fi
fi

printf 'recipe-lint_test: %s passed, %s failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ] || exit 1
exit 0
