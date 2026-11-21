#!/usr/bin/env bash
set -uo pipefail

# eyeball-pack_test.sh - the assembly half of scripts/eyeball-pack.sh, tested
# WITHOUT a lane and WITHOUT a browser (card t_214ca1bd).
# Run: bash scripts/eyeball-pack_test.sh
#
# A --diff-list run is the pack's plan mode: the changed paths come from a
# file instead of git, the browser is never launched, and the mapping, the
# file names and the index print exactly as a capture run prints them. That
# is the whole surface this suite judges:
#   - a diff maps to the expected surface set through the map file
#   - one changed component listed by two surfaces lists BOTH
#   - a surface with no changed component is skipped, and named as skipped
#   - the index line carries its contract shape (surface, numbered png,
#     the changed components)
#   - zero matched surfaces exits non-zero (a pack with nothing in it is a
#     failure, not an empty success)
#   - plan mode never shells out to agent-browser (a PATH stub records every
#     call; the record must stay empty)
#   - the REAL map is well-formed: exactly two separators per line, a reach
#     step, and every glob's static prefix is a real path in this repo (a map
#     that rots into typos would silently shrink every future pack)

SUT="$(cd "$(dirname "$0")" && pwd)/eyeball-pack.sh"
REPO="$(cd "$(dirname "$0")/.." && pwd)"
REAL_MAP="$REPO/scripts/eyeball-surfaces.txt"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1"; }
check() { # check <desc> <condition-exit>
  if [ "$2" -eq 0 ]; then ok; else bad "$1"; fi
}

WORK_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/eyeball-pack-test.XXXXXX")" || {
  echo "FAIL: cannot create a scratch dir" >&2
  exit 1
}
export TMPDIR="$WORK_ROOT"
mkdir -p "$TMPDIR"
trap 'rm -rf "$WORK_ROOT"' EXIT

# --- fixtures ---------------------------------------------------------------

FIX_MAP="$WORK_ROOT/fix-map.txt"
cat > "$FIX_MAP" <<'MAP'
# fixture map - three surfaces, one shared component
alpha|webui/app/components/alpha/*|url:/;wait:!!document.querySelector('.alpha')
beta|webui/app/components/beta.vue,webui/app/components/alpha/Beta.vue|url:/;wait:!!document.querySelector('.beta')
gamma|webui/app/components/gamma.vue,webui/app/components/gamma/*|url:/;wait:!!document.querySelector('.gamma')
MAP

fixture_diff() { # fixture_diff <name> <path...>
  local file="$WORK_ROOT/diff-$1.txt"
  shift
  : > "$file"
  for p in "$@"; do printf '%s\n' "$p" >> "$file"; done
  printf '%s' "$file"
}

run_pack() { # run_pack <diff-list> <out-dir> [extra args...]: stdout+stderr, sets RC
  local diff=$1 out=$2
  shift 2
  OUT="$("$SUT" --map "$FIX_MAP" --diff-list "$diff" --out-dir "$out" "$@" 2>&1)"
  RC=$?
}

index_lines() { printf '%s\n' "$OUT" | grep '^EYEBALL: ' | grep -F ' -> ' || true; }
skip_lines() { printf '%s\n' "$OUT" | grep '^EYEBALL-SKIP: ' || true; }
summary_line() { printf '%s\n' "$OUT" | grep '^EYEBALL: .* surfaces ' | tail -1; }

# --- T1: a diff maps to the expected surface set -----------------------------
d=$(fixture_diff t1 webui/app/components/alpha/One.vue)
run_pack "$d" "$WORK_ROOT/out1"
check "T1 exit 0 with one matched surface" "$([ "$RC" -eq 0 ] && echo 0 || echo 1)"
check "T1 alpha captured" "$(grep -q '^EYEBALL: alpha -> ' <<<"$OUT" && echo 0 || echo 1)"
check "T1 beta not captured" "$(grep -q '^EYEBALL: beta -> ' <<<"$OUT" && echo 1 || echo 0)"
check "T1 gamma not captured" "$(grep -q '^EYEBALL: gamma -> ' <<<"$OUT" && echo 1 || echo 0)"
check "T1 index carries the changed component" \
  "$(grep -q '^EYEBALL: alpha -> .*/01-alpha\.png (webui/app/components/alpha/One\.vue)$' <<<"$OUT" && echo 0 || echo 1)"
check "T1 summary counts planned and skipped" \
  "$(grep -qx 'EYEBALL: 1 surfaces planned, 2 skipped' <<<"$OUT" && echo 0 || echo 1)"

# --- T2: one component in two surfaces -> both listed ------------------------
d=$(fixture_diff t2 webui/app/components/alpha/Beta.vue)
run_pack "$d" "$WORK_ROOT/out2"
check "T2 exit 0 with two matched surfaces" "$([ "$RC" -eq 0 ] && echo 0 || echo 1)"
check "T2 alpha listed (shared component)" "$(grep -q '^EYEBALL: alpha -> ' <<<"$OUT" && echo 0 || echo 1)"
check "T2 beta listed (shared component)" "$(grep -q '^EYEBALL: beta -> ' <<<"$OUT" && echo 0 || echo 1)"
check "T2 numbering follows the map order (alpha 01, beta 02)" \
  "$(grep -q '^EYEBALL: beta -> .*/02-beta\.png ' <<<"$OUT" && echo 0 || echo 1)"
check "T2 summary counts two planned" \
  "$(grep -qx 'EYEBALL: 2 surfaces planned, 1 skipped' <<<"$OUT" && echo 0 || echo 1)"

# --- T3: a surface with no changed component is skipped BY NAME --------------
check "T3 gamma named as skipped" \
  "$(grep -qx 'EYEBALL-SKIP: gamma (no changed components)' <<<"$OUT" && echo 0 || echo 1)"

# --- T4: the index line's contract shape ------------------------------------
bad_shape=0
while IFS= read -r line; do
  [ -n "$line" ] || continue
  grep -Eq '^EYEBALL: [a-z0-9-]+ -> [^ ]*/[0-9][0-9]-[a-z0-9-]+\.png \([^)]+\)$' <<<"$line" || bad_shape=1
done <<< "$(index_lines)"
check "T4 every index line matches the contract shape" "$bad_shape"

# --- T5: zero matched surfaces exits non-zero -------------------------------
d=$(fixture_diff t5 README.md docs/architecture.md)
run_pack "$d" "$WORK_ROOT/out5"
check "T5 exit non-zero with zero surfaces" "$([ "$RC" -ne 0 ] && echo 0 || echo 1)"
check "T5 no index line" "$([ -z "$(index_lines)" ] && echo 0 || echo 1)"
check "T5 summary says zero planned, all skipped" \
  "$(grep -qx 'EYEBALL: 0 surfaces planned, 3 skipped' <<<"$OUT" && echo 0 || echo 1)"
check "T5 every surface named as skipped" "$([ "$(skip_lines | wc -l | tr -d ' ')" = "3" ] && echo 0 || echo 1)"

# --- T6: plan mode never launches a browser --------------------------------
STUB_DIR="$WORK_ROOT/stub-bin"
MARKER="$WORK_ROOT/browser-called.log"
mkdir -p "$STUB_DIR"
printf '#!/bin/bash\nprintf "called\\n" >> "%s"\nexit 99\n' "$MARKER" > "$STUB_DIR/agent-browser"
chmod +x "$STUB_DIR/agent-browser"
d=$(fixture_diff t6 webui/app/components/alpha/One.vue)
OUT="$(PATH="$STUB_DIR:$PATH" "$SUT" --map "$FIX_MAP" --diff-list "$d" --out-dir "$WORK_ROOT/out6" 2>&1)"
RC=$?
check "T6 plan run still exits 0" "$([ "$RC" -eq 0 ] && echo 0 || echo 1)"
check "T6 agent-browser was never called" "$([ ! -s "$MARKER" ] && echo 0 || echo 1)"

# --- T7: the real map maps a real component ---------------------------------
d=$(fixture_diff t7 webui/app/components/chat/AppComposer.vue)
OUT="$("$SUT" --map "$REAL_MAP" --diff-list "$d" --out-dir "$WORK_ROOT/out7" 2>&1)"
RC=$?
check "T7 real map: exit 0" "$([ "$RC" -eq 0 ] && echo 0 || echo 1)"
check "T7 real map: the composer's surface is chat-composer" \
  "$(grep -q '^EYEBALL: chat-composer -> ' <<<"$OUT" && echo 0 || echo 1)"
d=$(fixture_diff t7b webui/app/components/shell/MenuBar.vue)
OUT="$("$SUT" --map "$REAL_MAP" --diff-list "$d" --out-dir "$WORK_ROOT/out7b" 2>&1)"
check "T7 real map: a harness-free shell component maps to exactly one surface" \
  "$([ "$(printf '%s\n' "$OUT" | grep -c '^EYEBALL: menubar -> ')" = "1" ] && echo 0 || echo 1)"

# --- T8: the real map is well-formed ---------------------------------------
map_shape=0
map_glob=0
while IFS= read -r line; do
  case "$line" in ''|'#'*) continue ;; esac
  # exactly three fields
  [ "$(printf '%s' "$line" | tr -cd '|' | wc -c | tr -d ' ')" = "2" ] || { map_shape=1; echo "  bad shape: $line"; continue; }
  surface=${line%%|*}
  rest=${line#*|}
  globs=${rest%|*}
  reach=${rest##*|}
  [ -n "$surface" ] || { map_shape=1; echo "  empty surface: $line"; }
  [ -n "$reach" ] || { map_shape=1; echo "  empty reach: $line"; }
  grep -Eq '^[a-z]+:[^;]*(;[a-z]+:.*)*$' <<<"$reach" || { map_shape=1; echo "  bad reach: $line"; }
  grep -Eq '^[a-z0-9-]+$' <<<"$surface" || { map_shape=1; echo "  bad surface slug: $surface"; }
  IFS=',' read -r -a pats <<< "$globs"
  for p in "${pats[@]}"; do
    case "$p" in webui/*|harness/*) ;; *) map_glob=1; echo "  glob outside webui|harness: $p" ;; esac
    # the static prefix of the glob must exist in the repo
    static=${p%%[*?]*}
    dir=$(dirname "$static")
    [ -d "$REPO/$dir" ] || { map_glob=1; echo "  glob prefix is not a real dir: $p ($dir)"; }
  done
done < "$REAL_MAP"
check "T8 every real-map line has the three-field shape with a reach step" "$map_shape"
check "T8 every real-map glob prefixes a real repo dir" "$map_glob"

# --- summary ----------------------------------------------------------------
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

printf 'eyeball-pack_test: %d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ] || exit 1
exit 0
