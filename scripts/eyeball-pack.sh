#!/bin/bash
# eyeball-pack.sh - the screenshot pack of the webui surfaces a change set touched.
#
# Every UI arc ends the same way ("what is there for me to eyeball?") and the
# pack used to be assembled by hand. The change set is mechanical (git knows
# it) and the capture rides the pinned agent-browser the journeys already
# drive, so this script owns only the assembly:
#
#   changed components -> surfaces (scripts/eyeball-surfaces.txt)
#                      -> one screenshot per mapped surface
#                      -> a printed index a human can read
#
# Evidence, not a verdict: the pack shows what moved. Endo's eyes stay his.
#
# Usage:
#   scripts/eyeball-pack.sh <lane-url> <base-ref> <out-dir>
#   scripts/eyeball-pack.sh --lane <url> --base <ref> --out-dir <dir> [options]
#   scripts/eyeball-pack.sh --map <file> --diff-list <file> --out-dir <dir>
#
#   --map <file>         surface map (default: scripts/eyeball-surfaces.txt)
#   --diff-list <file>   read the changed paths from a file (one per line)
#                        instead of asking git, and NEVER launch a browser.
#                        The mapping, the file names and the index print
#                        exactly as a capture run prints them - that is what
#                        makes the assembly testable without a lane.
#   --session <name>     agent-browser session (default omega-eyeball; a
#                        private name keeps concurrent workers apart)
#   --help
#
# Env:
#   EYEBALL_AB_VERSION     agent-browser pin (default 0.42.0, the journeys'
#                          pin; a differing host version warns, never refuses -
#                          the Pi lane carries 0.26.0 by design)
#   EYEBALL_VIEWPORT       "1280 800" (the journeys' desktop viewport)
#   EYEBALL_REACH_TIMEOUT  seconds a reach `wait:` may take (default 10)
#
# Output (stdout):
#   EYEBALL-PACK: <out-dir>
#   EYEBALL: <surface> -> <file> (<changed components>)
#   EYEBALL-SKIP: <surface> (<reason>)
#   EYEBALL: <n> surfaces captured, <m> skipped
# A --diff-list plan run says "planned" where a capture run says "captured".
#
# Exit: 0 when at least one surface was captured (a skipped surface is
# normal - its reach state may need data the lane does not have), 1 when zero
# surfaces were captured, 2 on a usage error or a browser that will not start.

set -u

ROOT="$(cd "$(dirname "$0")/.." && pwd)" || exit 2
cd "$ROOT" || exit 2

MAP_FILE="$ROOT/scripts/eyeball-surfaces.txt"
LANE_URL=""
BASE_REF=""
OUT_DIR=""
DIFF_LIST=""
SESSION="${EYEBALL_SESSION:-omega-eyeball}"
AB_VERSION_PIN="${EYEBALL_AB_VERSION:-0.42.0}"
VP_W="${EYEBALL_VIEWPORT_W:-1280}"
VP_H="${EYEBALL_VIEWPORT_H:-800}"
REACH_TIMEOUT="${EYEBALL_REACH_TIMEOUT:-10}"

usage() { sed -n '2,50p' "$0" | sed -e 's/^# \{0,1\}//'; }

die() { printf 'eyeball-pack: %s\n' "$*" >&2; exit 2; }

# --- arguments --------------------------------------------------------------

positional=()
while [ "$#" -gt 0 ]; do
  case "$1" in
    --map) [ "$#" -ge 2 ] || die "--map needs a file"; MAP_FILE=$2; shift 2 ;;
    --map=*) MAP_FILE=${1#*=}; shift ;;
    --diff-list) [ "$#" -ge 2 ] || die "--diff-list needs a file"; DIFF_LIST=$2; shift 2 ;;
    --diff-list=*) DIFF_LIST=${1#*=}; shift ;;
    --lane) [ "$#" -ge 2 ] || die "--lane needs a url"; LANE_URL=$2; shift 2 ;;
    --lane=*) LANE_URL=${1#*=}; shift ;;
    --base) [ "$#" -ge 2 ] || die "--base needs a ref"; BASE_REF=$2; shift 2 ;;
    --base=*) BASE_REF=${1#*=}; shift ;;
    --out-dir) [ "$#" -ge 2 ] || die "--out-dir needs a path"; OUT_DIR=$2; shift 2 ;;
    --out-dir=*) OUT_DIR=${1#*=}; shift ;;
    --session) [ "$#" -ge 2 ] || die "--session needs a name"; SESSION=$2; shift 2 ;;
    --session=*) SESSION=${1#*=}; shift ;;
    -h|--help) usage; exit 0 ;;
    -*) die "unknown option: $1" ;;
    *) positional+=("$1"); shift ;;
  esac
done

# positionals fill whichever of lane/base/out-dir the flags left empty, in
# that order (so the documented call shape stays the plain one).
for arg in "${positional[@]+"${positional[@]}"}"; do
  if [ -z "$LANE_URL" ]; then LANE_URL=$arg
  elif [ -z "$BASE_REF" ]; then BASE_REF=$arg
  elif [ -z "$OUT_DIR" ]; then OUT_DIR=$arg
  else die "unexpected argument: $arg"
  fi
done

[ -f "$MAP_FILE" ] || die "no surface map at $MAP_FILE"
[ -n "$OUT_DIR" ] || die "no out-dir given (usage: eyeball-pack.sh <lane-url> <base-ref> <out-dir>)"

PLAN=0
if [ -n "$DIFF_LIST" ]; then
  PLAN=1
  [ -f "$DIFF_LIST" ] || die "no diff list at $DIFF_LIST"
else
  [ -n "$LANE_URL" ] || die "no lane url given"
  [ -n "$BASE_REF" ] || die "no base ref given"
fi

# --- the changed set --------------------------------------------------------
# No pathspec on the git call: the map's globs ARE the pathspec (they are what
# the pack claims to cover, and they reach the capsule UI under harness/*/ui
# that a bare `-- webui/` pathspec would miss). Unmapped paths fall through.
if [ "$PLAN" = 1 ]; then
  CHANGED_TEXT=$(sed -e 's/[[:space:]]*$//' "$DIFF_LIST" | grep -v '^[[:space:]]*#' | grep -v '^[[:space:]]*$')
else
  CHANGED_TEXT=$(git diff --name-only "$BASE_REF"..HEAD) || die "git diff failed for base ref $BASE_REF"
fi
CHANGED_TEXT=$(printf '%s\n' "$CHANGED_TEXT" | sed -e 's/\r$//' | sed -e '/^[[:space:]]*$/d' | sort -u)

# --- the map ----------------------------------------------------------------
# line shape: <surface>|<component globs>|<how-to-reach>
# globs are comma separated; `*` crosses `/` (bash string globbing).

matches_any() { # $1 path, rest: globs
  local path=$1 glob
  shift
  for glob in "$@"; do
    case "$path" in
      $glob) return 0 ;;
    esac
  done
  return 1
}

surface_lines=()    # <surface>|<globs>|<reach> for surfaces with a match
skip_lines=()       # <surface> (no changed components)

while IFS='|' read -r surface globs reach; do
  # a CRLF checkout must not turn every glob into a non-match
  surface=${surface%$'\r'}
  globs=${globs%$'\r'}
  reach=${reach%$'\r'}
  globs=${globs// /}
  case "${surface:-}" in ''|'#'*) continue ;; esac
  [ -n "${globs:-}" ] || continue
  [ -n "${reach:-}" ] || reach="url:/"
  IFS=',' read -r -a pats <<< "$globs"
  comps=()
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    matches_any "$f" "${pats[@]}" && comps+=("$f")
  done <<< "$CHANGED_TEXT"
  if [ "${#comps[@]}" -eq 0 ]; then
    skip_lines+=("$surface")
    continue
  fi
  joined=$(printf '%s, ' "${comps[@]}")
  surface_lines+=("$surface|${joined%, }|$reach")
done < "$MAP_FILE"

pad2() { printf '%02d' "$1"; }

slug() { printf '%s' "$1" | tr -c 'A-Za-z0-9._-' '-' | sed -e 's/-\+/-/g' -e 's/^-//' -e 's/-$//'; }

planned_names() { # $1 index, $2 surface
  printf '%s/%s-%s.png' "$OUT_DIR" "$(pad2 "$1")" "$(slug "$2")"
}

if [ "${#surface_lines[@]}" -eq 0 ]; then
  for s in "${skip_lines[@]+"${skip_lines[@]}"}"; do
    printf 'EYEBALL-SKIP: %s (no changed components)\n' "$s"
  done
  if [ "$PLAN" = 1 ]; then
    printf 'EYEBALL: 0 surfaces planned, %d skipped\n' "${#skip_lines[@]}"
  else
    printf 'EYEBALL: 0 surfaces captured, %d skipped\n' "${#skip_lines[@]}"
  fi
  exit 1
fi

# --- plan mode: no browser --------------------------------------------------
if [ "$PLAN" = 1 ]; then
  printf 'EYEBALL-PACK: %s (plan - no browser launched)\n' "$OUT_DIR"
  n=0
  for line in "${surface_lines[@]}"; do
    n=$((n + 1))
    surface=${line%%|*}
    rest=${line#*|}
    comps=${rest%|*}
    printf 'EYEBALL: %s -> %s (%s)\n' "$surface" "$(planned_names "$n" "$surface")" "$comps"
  done
  for s in "${skip_lines[@]+"${skip_lines[@]}"}"; do
    printf 'EYEBALL-SKIP: %s (no changed components)\n' "$s"
  done
  printf 'EYEBALL: %d surfaces planned, %d skipped\n' "$n" "${#skip_lines[@]}"
  exit 0
fi

# --- capture ----------------------------------------------------------------

# agent-browser binary: same resolution order as scripts/journeys/run.sh
# (PATH, then the Hermes tools dir, then the extensionless shim), so the
# pack drives the SAME pinned CLI the journeys do. No new browser.
AB="$(command -v agent-browser 2>/dev/null | head -1)"
if [ -z "$AB" ]; then
  AB="$(find "$HOME/.hermes/tools" -maxdepth 5 -type f \
     \( -name 'agent-browser-linux-arm64' -o -name 'agent-browser-linux-x64' \
        -o -name 'agent-browser-win32-x64.exe' -o -name 'agent-browser-win32-arm64.exe' \) 2>/dev/null | head -1)"
fi
if [ -z "$AB" ]; then
  AB="$(find "$HOME/.hermes/tools" -maxdepth 3 -type f -name 'agent-browser' 2>/dev/null | head -1)"
fi
[ -n "$AB" ] || die "agent-browser CLI not found (PATH or ~/.hermes/tools) - npm i -g agent-browser@$AB_VERSION_PIN && agent-browser install"

ab_version="$("$AB" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)"
if [ "$ab_version" != "$AB_VERSION_PIN" ]; then
  printf 'eyeball-pack: agent-browser %s, PINNED %s (the journeys pin; a host may legitimately differ)\n' \
    "${ab_version:-unknown}" "$AB_VERSION_PIN" >&2
fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/eyeball-pack.XXXXXX")" || die "cannot make a scratch dir"
mkdir -p "$OUT_DIR" || die "cannot create out-dir $OUT_DIR"

ab() { "$AB" --session "$SESSION" "$@"; }
js() { printf '%s' "$1" | ab eval --stdin; }

# a NATIVE tool (the agent-browser exe) cannot open an MSYS path: hand it the
# mixed C:/... form when cygpath exists (the journeys' JOURNEY_WORKTMP law).
native_path() {
  if command -v cygpath >/dev/null 2>&1; then cygpath -m "$1"; else printf '%s' "$1"; fi
}

cleanup_browser() { ab close >/dev/null 2>&1 || true; }
trap cleanup_browser EXIT

# The FIRST agent-browser call is unpiped and redirected: on an auto-launch
# lane it is the call that spawns the daemon and its chrome, and a pipe or a
# command substitution there hangs until the spawn's write-end closes
# (scripts/journeys/lib.sh's t_b86f709d law).
browser_up() {
  local log="$WORK/ab-boot.log"
  ab set viewport "$VP_W" "$VP_H" >"$log" 2>&1 || return 1
  ab open "$LANE_URL/" >>"$log" 2>&1 || return 1
  ab wait --load networkidle >>"$log" 2>&1 || true
  return 0
}

# wait_js: poll a BOOLEAN js expr (the journeys' wait_js answers "true" - a
# `?1:0` probe never matches and reds forever, t_163d1e87).
wait_js() {
  local expr=$1 t=${2:-$REACH_TIMEOUT} i=0 out
  while [ "$i" -lt $((t * 2)) ]; do
    out=$(js "$expr" 2>&1 | tail -1)
    [ "$out" = "true" ] && return 0
    sleep 0.5
    i=$((i + 1))
  done
  return 1
}

# click_dock_tab <Name>: the canonical chip matcher - edge strips render
# icon-only chips whose name rides aria-label, main/bottom strips carry the
# name as text (t_502b8e81 / the icon-chip law).
click_dock_tab() {
  local name=$1 out
  out=$(js "(()=>{const n=(e)=>((e.getAttribute('aria-label')||'')||e.title||((e.querySelector('.dock-tab-title')||{textContent:''}).textContent)).trim();
 const t=[...document.querySelectorAll('.dock-tab')].find((e)=>n(e)==='$name');
 if(!t) return 0; t.click(); return 1})()" 2>&1 | tail -1 | tr -d '"')
  [ "$out" = "1" ]
}

# reach <spec>: run the map's semicolon-separated steps in order.
reach() {
  local spec=$1 step kind arg oldifs=$IFS
  IFS=';'
  for step in $spec; do
    IFS=$oldifs
    step="${step#"${step%%[![:space:]]*}"}"
    step="${step%"${step##*[![:space:]]}"}"
    [ -n "$step" ] || { IFS=';'; continue; }
    kind=${step%%:*}
    arg=${step#*:}
    case "$kind" in
      url)     ab open "${LANE_URL}${arg}" >/dev/null 2>&1 || return 1
               ab wait --load networkidle >/dev/null 2>&1 || true ;;
      reload)  ab reload >/dev/null 2>&1 || return 1
               ab wait --load networkidle >/dev/null 2>&1 || true ;;
      vp)      ab set viewport "${arg%x*}" "${arg#*x}" >/dev/null 2>&1 || return 1 ;;
      press)   ab press "$arg" >/dev/null 2>&1 || return 1 ;;
      tab)     click_dock_tab "$arg" || return 1 ;;
      click)   js "(()=>{const e=document.querySelector('$arg');if(!e)return 0;e.click();return 1})()" 2>&1 | grep -q '1' || return 1 ;;
      eval)    js "$arg" >/dev/null 2>&1 || return 1 ;;
      wait)    wait_js "$arg" || return 1 ;;
      waittext) ab wait --text "$arg" >/dev/null 2>&1 || return 1 ;;
      *)       printf 'eyeball-pack: unknown reach step: %s\n' "$step" >&2; return 1 ;;
    esac
    IFS=';'
  done
  IFS=$oldifs
  return 0
}

browser_up || die "agent-browser cannot start on $LANE_URL (log: $WORK/ab-boot.log)"

# Every surface is shot from the SAME baseline (the journeys' clean-state
# law): the dock layout, the open tabs and the viewport all persist in the
# browser, so without a reset surface N's screenshot carries surface N-1's
# main-area state and reads as "the thing I changed is broken/duplicated".
# EYEBALL_NO_RESET=1 keeps the running state for a caller that wants a
# continuous walk instead.
reset_state() {
  [ "${EYEBALL_NO_RESET:-0}" = "1" ] && return 0
  js 'localStorage.clear()' >/dev/null 2>&1 || return 1
  ab set viewport "$VP_W" "$VP_H" >/dev/null 2>&1 || true
  ab open "$LANE_URL/" >/dev/null 2>&1 || return 1
  ab wait --load networkidle >/dev/null 2>&1 || true
  return 0
}

printf 'EYEBALL-PACK: %s\n' "$OUT_DIR"

captured=0
skipped=0
n=0
for line in "${surface_lines[@]}"; do
  n=$((n + 1))
  surface=${line%%|*}
  rest=${line#*|}
  comps=${rest%|*}
  reach_spec=${rest##*|}
  target=$(planned_names "$n" "$surface")
  reason=""
  if ! reset_state; then
    reason="could not reset the browser to the clean baseline"
  elif ! reach "$reach_spec"; then
    reason="reach failed: $reach_spec"
  elif ! ab screenshot "$(native_path "$target")" >/dev/null 2>&1 || [ ! -s "$target" ]; then
    reason="screenshot did not land"
  fi
  if [ -n "$reason" ]; then
    skipped=$((skipped + 1))
    printf 'EYEBALL-SKIP: %s (%s)\n' "$surface" "$reason"
  else
    captured=$((captured + 1))
    printf 'EYEBALL: %s -> %s (%s)\n' "$surface" "$target" "$comps"
  fi
done

for s in "${skip_lines[@]+"${skip_lines[@]}"}"; do
  skipped=$((skipped + 1))
  printf 'EYEBALL-SKIP: %s (no changed components)\n' "$s"
done

printf 'EYEBALL: %d surfaces captured, %d skipped\n' "$captured" "$skipped"
[ "$captured" -gt 0 ] || exit 1
exit 0
