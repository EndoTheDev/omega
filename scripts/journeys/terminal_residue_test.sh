#!/usr/bin/env bash
# terminal_residue_test.sh - the terminal reap contract of scripts/journeys/lib.sh
# (card t_79a6b353).
#
# THE FACT: a page load mounts the bottom pane, the pane claims a pty, and the
# pty outlives the page by design (TerminalTab.vue: the unmount unbinds the SSE,
# never the SHELL). The residue path reaped sessions, board cards and roster
# homes and nothing else, so 3 live shells accumulated across 5 recipes on a
# scratch lane - and the store's cap answers the NEXT claim with 409 "session cap
# reached", which the pane renders as "no terminal": a red one recipe later,
# naming the product.
#
# THE SECOND FACT, and why a naive diff is not enough: the pane RELAIMS what a
# bare DELETE takes. The dropped SSE reads as a missed attach, so the pane
# reconnects and claims a fresh pty half a second later. The recipes that
# reaped by hand leaked a shell per run that way. lib.sh releases the page
# (about:blank - the claim ladder dies with the document) before the first
# DELETE.
#
# Phases (a stub lane and a stub agent-browser - no real lane, no browser):
#   P0  nothing claimed                   -> the store is untouched (the reap
#                                            never kills a foreign shell)
#   P1  green exit through run.sh's trap  -> the diff is DELETEd, the baseline
#                                            survives, the page released FIRST
#   P2  the same recipe with the release  -> the reclaim LEAKS (proves P1 does
#       removed                              not pass by accident)
#   P3  die(), no EXIT trap at all        -> still DELETEd: a killed recipe
#                                            leaks no shell either
#   P4  the recipe installs its OWN EXIT  -> still DELETEd: the residue path
#       trap (twelve recipes do)             also runs at the recipe's return
#   P5  the same body on the pre-fix fork -> leaks (proves P4 is the return
#       (trap only)                          call, not luck)
#
# Run: bash scripts/journeys/terminal_residue_test.sh

set -uo pipefail

JROOT="$(cd "$(dirname "$0")" && pwd)"
LIB="$JROOT/lib.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); printf 'ok: %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf 'FAIL: %s\n' "$1" >&2; }

TMP="$(mktemp -d "${TMPDIR:-/tmp}/terminal-residue-test.XXXXXX")"
# native tools cannot open a POSIX /tmp path on MSYS hosts; the stubs are bash
# scripts invoked by absolute path, so the POSIX form is the one to keep.
if command -v cygpath >/dev/null 2>&1; then
  TMP="$(cygpath -u "$TMP")"
fi
STATE="$TMP/lane"
BIN="$TMP/bin"
WORK="$TMP/worktmp"
mkdir -p "$STATE" "$BIN" "$WORK"

cleanup() { rm -rf "$TMP" 2>/dev/null; }
trap cleanup EXIT

# --- the stub lane -----------------------------------------------------------
# Answers GET /terminal/sessions from $STATE/ids, serves a DELETE by dropping
# the id, and models the pane's RECLAIM: while $STATE/page-live exists a DELETE
# is followed by a fresh claim, exactly as TerminalTab's reconnect does.
cat > "$BIN/lane-curl" <<'STUB'
#!/usr/bin/env bash
set -u
method=GET
url=""
while [ $# -gt 0 ]; do
  case "$1" in
    -X|--request) method="${2:-GET}"; shift 2 ;;
    -m|--max-time|-H|--header|-d|--data|--data-raw|-o|--output) shift 2 ;;
    http://*|https://*) url="$1"; shift ;;
    *) shift ;;
  esac
done
state="${LANE_STATE:?}"
case "$url" in
  */terminal/sessions)
    printf '{"sessions":['
    first=1
    while IFS= read -r id; do
      [ -n "$id" ] || continue
      [ "$first" = "1" ] || printf ','
      printf '{"id":"%s"}' "$id"
      first=0
    done < "$state/ids"
    printf ']}\n'
    ;;
  */terminal/*)
    id="${url##*/}"
    printf 'delete %s\n' "$id" >> "$state/events"
    if [ -f "$state/page-live" ]; then
      n=$(( $(cat "$state/reclaims" 2>/dev/null || printf 0) + 1 ))
      printf '%s\n' "$n" > "$state/reclaims"
      printf 'reclaim-%s\n' "$n" >> "$state/ids"
      printf 'reclaim reclaim-%s\n' "$n" >> "$state/events"
    fi
    grep -vx "$id" "$state/ids" > "$state/ids.new" 2>/dev/null || true
    mv "$state/ids.new" "$state/ids"
    ;;
esac
exit 0
STUB

# The stub agent-browser: the only call the reap makes is the page release.
cat > "$BIN/agent-browser" <<'STUB'
#!/usr/bin/env bash
set -u
args=()
while [ $# -gt 0 ]; do
  case "$1" in
    --session) shift 2 ;;
    *) args+=("$1"); shift ;;
  esac
done
state="${LANE_STATE:?}"
if [ "${args[0]:-}" = "open" ]; then
  printf 'open %s\n' "${args[1]:-}" >> "$state/events"
  case "${args[1]:-}" in about:blank) rm -f "$state/page-live" ;; esac
fi
exit 0
STUB
chmod +x "$BIN/lane-curl" "$BIN/agent-browser"

# the negative control: lib.sh with the page release removed, i.e. the shape a
# hand-rolled reap has.
LIB_NOREL="$TMP/lib-nonav.sh"
sed 's|^[[:space:]]*ab open about:blank.*$|    :|' "$LIB" > "$LIB_NOREL"
if grep -q 'ab open about:blank' "$LIB_NOREL"; then
  bad "the negative control still carries the page release - P2 would prove nothing"
fi

# --- fixtures ----------------------------------------------------------------
reset_lane() { # $*: the ids the store already holds
  : > "$STATE/ids"
  local id
  for id in "$@"; do printf '%s\n' "$id" >> "$STATE/ids"; done
  : > "$STATE/events"
  rm -f "$STATE/page-live" "$STATE/reclaims"
}
ids_now() { sort "$STATE/ids" 2>/dev/null | tr '\n' ' ' | sed 's/ *$//'; }
expect_ids() { # $1: space-joined expected ids, $2: what
  local want got
  want="$(printf '%s' "$1" | tr ' ' '\n' | sort | tr '\n' ' ' | sed 's/ *$//')"
  got="$(ids_now)"
  if [ "$got" = "$want" ]; then ok "$2: store [$got]"; else bad "$2: store [$got], want [$want]"; fi
}
expect_event() { # $1: a needle in the event log, $2: what
  if grep -qx "$1" "$STATE/events"; then ok "$2"; else bad "$2 (events: $(tr '\n' '|' < "$STATE/events"))"; fi
}

# run_phase <body> [lib]: one recipe-shaped shell - the trap first (run.sh's
# recipe fork), then the source, then the body.
run_phase() {
  PHASE_BODY="$1" PHASE_LIB="${2:-$LIB}" bash -c '
    set -uo pipefail
    trap "journey_exit_cleanup" EXIT
    . "$PHASE_LIB"
    . "$PHASE_BODY"
  ' 2>/dev/null
}

# run_fork_phase <body> [lib]: the CURRENT fork shape - the trap, the recipe,
# then the explicit call at the recipe's return (a recipe's own trap replaces
# the subshell's, so the trap alone is not the whole exit path).
run_fork_phase() {
  PHASE_BODY="$1" PHASE_LIB="${2:-$LIB}" bash -c '
    set -uo pipefail
    trap "journey_exit_cleanup" EXIT
    . "$PHASE_LIB"
    . "$PHASE_BODY"
    journey_dead_check
    journey_exit_cleanup
  ' 2>/dev/null
}

phase_dir="$TMP/phases"
mkdir -p "$phase_dir"
# the recipe bodies. green: open_app on a live page (the pane claims b2).
printf '%s\n' \
  'printf "b2\n" >> "$LANE_STATE/ids"' \
  ': > "$LANE_STATE/page-live"' \
  'JOURNEY_PAGE_OPENED=1' > "$phase_dir/green.sh"
printf '%s\n' ':' > "$phase_dir/idle.sh"
# die: the same claim, then a red. NO trap - die() owns the residue path.
printf '%s\n' \
  'printf "b2\n" >> "$LANE_STATE/ids"' \
  ': > "$LANE_STATE/page-live"' \
  'JOURNEY_PAGE_OPENED=1' \
  'die "boom"' > "$phase_dir/die.sh"
# clobber: the same claim, then the recipe installs its own EXIT trap - the
# shape twelve recipes carry (a board reap, a cron delete). A trap REPLACES the
# subshell's, so the residue path must not live in the trap alone.
printf '%s\n' \
  'printf "b2\n" >> "$LANE_STATE/ids"' \
  ': > "$LANE_STATE/page-live"' \
  'JOURNEY_PAGE_OPENED=1' \
  'cleanup() { :; }' \
  'trap cleanup EXIT' > "$phase_dir/clobber.sh"

export LANE_STATE="$STATE"
export JOURNEY_BASE_URL="http://127.0.0.1:8199"
export JOURNEY_CURL="$BIN/lane-curl"
export AB="$BIN/agent-browser"
export JOURNEY_WORKTMP="$WORK"
export JOURNEY_RUN_ID=4242

# --- P0: a recipe that claims nothing leaves the store alone ------------------
reset_lane a1
run_phase "$phase_dir/idle.sh"
expect_ids "a1" "P0 clean recipe"
if [ ! -s "$STATE/events" ]; then ok "P0 nothing was killed and no page was released"; else bad "P0 touched the terminal store: $(tr '\n' '|' < "$STATE/events")"; fi

# --- P1: green exit, run.sh's trap -------------------------------------------
reset_lane a1
run_phase "$phase_dir/green.sh"
expect_ids "a1" "P1 green exit left the store as it found it"
expect_event "delete b2" "P1 the minted session was DELETEd"
expect_event "open about:blank" "P1 the page was released"

# the release must PRECEDE the delete, or the pane reclaims what was killed
nav="$(grep -nx 'open about:blank' "$STATE/events" | cut -d: -f1)"
del="$(grep -nx 'delete b2' "$STATE/events" | cut -d: -f1)"
if [ -n "$nav" ] && [ -n "$del" ] && [ "$nav" -lt "$del" ]; then
  ok "P1 the page release came first (line $nav before $del)"
else
  bad "P1 the release did not precede the delete (events: $(tr '\n' '|' < "$STATE/events"))"
fi

# --- P2: the same recipe with the page release removed LEAKS -----------------
reset_lane a1
run_phase "$phase_dir/green.sh" "$LIB_NOREL"
if [ "$(ids_now)" != "a1" ]; then
  ok "P2 the reclaim leaks without the release (store [$(ids_now)])"
else
  bad "P2 the reclaim did not leak - the stub no longer models the pane"
fi

# --- P3: die(), no EXIT trap at all ------------------------------------------
reset_lane a1
# no trap on purpose: a red that only cleaned up through an EXIT trap would
# leak on a SIGKILLed recipe.
rc=0
PHASE_BODY="$phase_dir/die.sh" PHASE_LIB="$LIB" bash -c '
  set -uo pipefail
  . "$PHASE_LIB"
  . "$PHASE_BODY"
' 2>/dev/null || rc=$?
if [ "$rc" = "1" ]; then ok "P3 die exited 1"; else bad "P3 die exited $rc, want 1"; fi
expect_ids "a1" "P3 the die path left the store as it found it"
expect_event "delete b2" "P3 the die path DELETEd the minted session"

# --- P4: a recipe that installs its OWN EXIT trap ---------------------------
reset_lane a1
run_fork_phase "$phase_dir/clobber.sh"
expect_ids "a1" "P4 a clobbering recipe trap still reaped the session"
expect_event "delete b2" "P4 the return-path cleanup DELETEd the minted session"

# --- P5: the same body on the pre-fix fork (the trap alone) LEAKS -----------
reset_lane a1
run_phase "$phase_dir/clobber.sh"
if [ "$(ids_now)" != "a1" ]; then
  ok "P5 the clobbered trap alone leaks (store [$(ids_now)])"
else
  bad "P5 the trap-only fork did not leak - P4 would prove nothing"
fi

# --- verdict -----------------------------------------------------------------
printf '\nterminal_residue_test: %s pass, %s fail\n' "$pass" "$fail"
[ "$fail" = "0" ] || exit 1
exit 0
