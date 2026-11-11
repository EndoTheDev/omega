#!/bin/bash
# lib.sh - shared mechanics for the omega validation journeys.
#
# A recipe does: open the SPA, act like the customer, assert the
# user-visible outcome, exit 0 on green or die() on red. lib.sh owns the
# browser channel (agent-browser over CDP to a Hermes-bundled headless
# Chromium), the assertion helpers, and the journey-session cleanup trap.
#
# Recipes never send real traffic against live customer sessions: each
# write-journey creates its own throwaway session (id prefix `jrn-`, label
# prefix `(journey)`) and lib.sh deletes it in a cleanup trap.
#
# Residue contract (card t_a99ffcd7): a journey keeps its recipe and its
# result files, and NOTHING else. Its runtime actors - sessions, board
# tasks, roster agent homes - are reaped by cleanup_journey_residue at
# every exit path (die, recipe EXIT traps, run.sh's pre-clean); wake
# entries in cronstate.json are the serve's own state, swept by the cron
# runner's GC once the declaring agent home is gone.
#
# Env:
#   JOURNEY_BASE_URL    default http://127.0.0.1:8098 (the dev lane)
#   JOURNEY_CHROME_BIN  explicit chromium binary; when set alongside
#                       JOURNEY_CHROME_LIBS, run.sh launches it headless
#                       (the Pi lane). Unset: agent-browser auto-launches
#                       its own Chrome (the CI lane).
#   JOURNEY_CHROME_PORT CDP port for a Pi-lane chrome (default 9333)
#   JOURNEY_SESSION     agent-browser session name (default omega-journey)

JOURNEY_BASE_URL=${JOURNEY_BASE_URL:-http://127.0.0.1:8098}
JOURNEY_CHROME_PORT=${JOURNEY_CHROME_PORT:-9333}
JOURNEY_SESSION=${JOURNEY_SESSION:-omega-journey}

# JOURNEY_WORKTMP: a scratch path EVERY tool in the recipe can read.
# On MSYS hosts (windows git-bash) the native binaries recipes invoke
# (python3 is a native win32 build) cannot open MSYS /tmp paths - they
# resolve them to <drive>:\tmp and the file is not there. cygpath -m
# rewrites the scratch dir to a mixed C:/... form that bash reads,
# writes and spawns natively; POSIX hosts have no cygpath and pass the
# directory through unchanged (TMPDIR unset on the CI lane stays /tmp).
if command -v cygpath >/dev/null 2>&1; then
  JOURNEY_WORKTMP="$(cygpath -m "${TMPDIR:-/tmp}")"
else
  JOURNEY_WORKTMP="${TMPDIR:-/tmp}"
fi
export JOURNEY_WORKTMP

# ab <args...>: one agent-browser call, ALWAYS in this run's browser
# session: the daemon multiplexes concurrent clients (another card's
# journey run rides the same daemon) and the default connection is
# global - a session name keeps pages, navigation and the console log
# bucket private to THIS suite (lib.sh lives in run.sh's export).
ab() {
  "$AB" --session "${JOURNEY_SESSION:-omega-journey}" "$@"
}

# step <desc>: progress line
step() { printf '%s [journey] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }

# console_scope_begin: start a CLEAN console record for the recipe about to
# run. agent-browser keeps the console buffer with the DAEMON SESSION, not
# with the page or the chrome: it survives a navigation, a reload AND a
# reconnect to a different chrome (proven live t_6654bc60 - a fresh chrome
# on another port still answered with the previous chrome's error). One
# transient [NUXT_E5002] from an earlier run therefore red ever later
# assert_no_console_errors on that host, while every product step stays
# green and the manifest itself answers 200. run.sh calls this before every
# recipe forks, so the assert judges only what THIS run's own load and
# interactions produced. That strictly narrows the window - an old error
# can no longer mask a new one, and no recipe skips the assertion.
console_scope_begin() {
  # unpiped + redirected on purpose: on an auto-launch lane this is the
  # first ab call of the run, and a pipe or a command substitution here
  # would hang while the daemon spawns its chrome (the t_b86f709d law).
  ab console --clear >/dev/null 2>&1 || true
}

# die <msg>: red journey. Prints, captures evidence, exits non-zero.
die() {
  printf '%s [journey RED] %s\n' "$(date +%H:%M:%S)" "$*" >&2
  ab screenshot 2>/dev/null | tail -1 >&2
  cleanup_journey_residue || true
  exit 1
}

# assert_eq <got> <want> <name>
assert_eq() {
  if [ "$1" != "$2" ]; then
    die "${3:-assert_eq} failed: got [$1] want [$2]"
  fi
  step "ok: ${3:-assert_eq}"
}

# assert_ne <got> <notwant> <name>
assert_ne() {
  if [ "$1" = "$2" ]; then
    die "${3:-assert_ne} failed: got [$1] (must differ)"
  fi
  step "ok: ${3:-assert_ne}"
}

# js <script>: eval via stdin (handles quotes/newlines safely)
js() {
  printf '%s' "$1" | ab eval --stdin
}

# jsq <expr>: inline eval for simple single-line expressions
jsq() { ab eval "$1"; }

# wait_js <expr> <timeout_s>: poll a JS truthy condition (25s CLI cap is
# not enough for stream completion). Prints nothing, dies on timeout.
wait_js() {
  local expr="$1" t="${2:-15}" i=0 out
  while [ "$i" -lt $((t * 2)) ]; do
    out=$(jsq "$expr" 2>&1)
    if [ "$out" = "true" ]; then return 0; fi
    sleep 0.5
    i=$((i + 1))
  done
  die "wait_js timed out after ${t}s: $expr (last: $out)"
}

# wait_text <text> <timeout_s>: wait until text renders, then die on miss
wait_text() { ab wait --text "$1" >/dev/null 2>&1 || die "text never rendered: $1"; step "ok: text rendered: $1"; }

# --- the selector-wait law (card t_2bfd52ac) ---------------------------------
# A component fed by a single fetch-on-mount (a v-if gating a control
# until a GET answers, e.g. the board switcher's BandSelect chip behind
# Board.vue's `boards.length > 1 || boardName`) can never satisfy a hard
# wait_js when that fetch silently fails - and the wait burns its cap as
# a RED that says nothing. For such selectors:
#   poll first (poll_js never dies) -> on a miss re-mount the surface
#   (ab reload re-runs the fetch) -> poll again -> die NAMING the API's
#   answer, so the next red carries its own diagnosis. Whole-recipe
#   provider/model dependence remains run.sh's JOURNEY_NEEDS_MODEL gate;
#   this law is for inline waits only. Proven shape: recipe 98's
#   switch_to_board.

# poll_js <expr> <timeout_s>: like wait_js but NEVER dies - returns 1 on
# timeout. wait_js always exits the recipe on timeout (die calls exit, and
# `wait_js ... || true` cannot catch an exit), so any fallback path must
# use poll_js.
poll_js() {
  local expr="$1" t="${2:-15}" i=0 out
  while [ "$i" -lt $((t * 2)) ]; do
    out=$(jsq "$expr" 2>&1)
    if [ "$out" = "true" ]; then return 0; fi
    sleep 0.5
    i=$((i + 1))
  done
  return 1
}

# open_app: fresh SPA load in the journey session + console hook. The
# viewport resets to desktop FIRST: a recipe that died mid-way leaves
# its mobile viewport on the tab (77-mobile-drawer crosses to 420px and
# dies at mobile), and the next recipe must not inherit it.
open_app() {
  ab set viewport 1280 800 >/dev/null 2>&1 || true
  ab open "$JOURNEY_BASE_URL/" >/dev/null 2>&1 || \
    { JOURNEY_CDP_ATTACHED=0; ab open "$JOURNEY_BASE_URL/" >/dev/null 2>&1 || die "cannot open $JOURNEY_BASE_URL"; }
  ab wait --load networkidle >/dev/null 2>&1 || true
  step "open: $JOURNEY_BASE_URL"
}

# open_journey_row <label>: open the journey session's sidebar row,
# retrying while the sidebar's session list is still loading (the SPA
# fetches /sessions after boot - a one-shot click races that boot).
# The open rides a DOM click on the matching .session-list li, never a
# pointer-pipeline click: a real-mouse click on a row whose title is
# long enough to reach the hover action cluster (x93..201 at the 240px
# sidebar, 5 buttons since t_6bf696e4) lights the cluster through
# li:hover FIRST (main.css: .session-list li:hover .row-actions ->
# pointer-events:auto) and the mousedown lands on a @click.stop action
# button - rename opens, the row never does. The plain DOM click skips
# the hover lit-up entirely (the t_7adf2e7f law: the row stays one
# click target; the buttons stay hover-reached).
# The verdict is the OPEN, and it has two honest pass shapes:
# the route moved to /chat/ (fresh open), or a focused .dock-tab
# already carries this row's label - a fresh_open remount restores the
# dock's last route, so the reopened session's tab is back BEFORE the
# click and the route legitimately does not move (83's pin-restore
# reopen). A same-route pass WITHOUT the matching tab is the earlier
# recipe's deleted-session route and must fail: that lie red'd
# 61 on every run since birth (195ddbf8).
# Re-clicked on every poll until the verdict passes.
dock_tab_match_js() {
  jsq "(()=>{const els=[...document.querySelectorAll('.dock-tab')];return els.some(e=>(((e.querySelector('.dock-tab-title')?e.querySelector('.dock-tab-title').textContent:null)??e.getAttribute('aria-label')??e.title??'').trim()).includes(\"$1\")&&e.getAttribute('aria-selected')==='true')?1:0})()"
}
open_journey_row() {
  local label="$1" i=0 before after clicked opened
  before=$(jsq 'location.pathname' 2>&1 | tail -1 | tr -d '"')
  while [ "$i" -lt 30 ]; do
    clicked=$(jsq "(()=>{const rows=[...document.querySelectorAll('.session-list li')];const r=rows.find(x=>(x.innerText||'').includes(\"$label\"));return r?(r.click(),1):0})()" 2>&1 | tail -1)
    if [ "$clicked" = "1" ]; then
      opened=$(jsq 'location.pathname.startsWith("/chat/") ? 1 : 0' 2>&1 | tail -1)
      if [ "$opened" = "1" ]; then
        after=$(jsq 'location.pathname' 2>&1 | tail -1 | tr -d '"')
        if [ "$after" != "$before" ] || [ "$(dock_tab_match_js "$label")" = "1" ]; then
          step "ok: opened journey session row: $label"
          return 0
        fi
      fi
    fi
    sleep 1
    i=$((i + 1))
  done
  die "journey session row never opened: $label"
}

# assert_no_console_errors: the presentation-detector. A poisoned bundle,
# a hydration ReferenceError, a broken signal shape all show up here.
# Noise whitelist: none - the built app must be silent. EXCEPTION: a
# recipe run against a nuxt DEV lane (t_67fc1905's verify setup) rides
# the vite HMR client, whose websocket/connect noise is a dev artifact -
# set JOURNEY_DEV_LANE=1 there; the embedded lane (CI, :8098) never sets
# it and asserts genuine silence.
# The window judged here is the RECIPE (run.sh clears the session's console
# record right before each recipe forks, see console_scope_begin): the
# buffer outlives pages, reloads and chromes, so an unscoped read red on
# errors that no longer exist anywhere (t_6654bc60).
assert_no_console_errors() {
  local errs
  errs=$(ab console 2>&1)
  if [ -n "${JOURNEY_DEV_LANE:-}" ]; then
    errs=$(printf '%s' "$errs" | grep -viE 'vite|hmr|nuxt devtools|suspense')
  fi
  if printf '%s' "$errs" | grep -qiE 'error|referenceerror|typeerror|syntaxerror'; then
    die "console errors on $JOURNEY_BASE_URL: $errs"
  fi
  step "ok: console silent"
}

# journey session lifecycle -------------------------------------------------

JRN_TAGFILE="${JRN_TAGFILE:-$JOURNEY_WORKTMP/omega-jrn-runtag}"

# new_journey_session <name>: POST /sessions with a unique id; prints
# the id; writes the unique run tag to $JRN_TAGFILE (function runs in a
# $(...) subshell, so state crosses back through the file).
new_journey_session() {
  local name="$1" resp id runtag
  runtag=$(date +%d%b%H%M%S)
  printf '%s' "$runtag" > "$JRN_TAGFILE"
  id="jrn-${runtag}-${name}"
  resp=$(curl -s -X POST "${JOURNEY_BASE_URL}/sessions" -H 'Content-Type: application/json' \
         -d "{\"id\": \"${id}\"}")
  printf '%s' "$resp" | grep -q "$id" || die "POST /sessions failed: $resp"
  printf '%s\n' "$id"
}

# journey_tag: read the run tag a previous subshell left
journey_tag() { cat "$JRN_TAGFILE"; }

# label_journey_session: friendlier sidebar row + stable search token
label_journey_session() {
  local id="$1" label="$2"
  curl -s -X PUT "${JOURNEY_BASE_URL}/sessions/${id}" \
    -H 'Content-Type: application/json' -d "{\"label\": \"${label}\"}" >/dev/null
  step "labeled: ${label}"
}

# cleanup_journey_sessions: delete every journey session this run (and
# any leftover from a crashed earlier run) via the API.
cleanup_journey_sessions() {
  local sids ids
  # this run's id (fast path)
  if [ -n "${JRN_ID:-}" ]; then
    curl -s -X DELETE "${JOURNEY_BASE_URL}/sessions/${JRN_ID}" >/dev/null 2>&1 || true
  fi
  # leftovers from crashed runs (by id prefix)
  ids=$(curl -s "${JOURNEY_BASE_URL}/sessions" | grep -oE '"id": ?"jrn-[A-Za-z0-9-]+"' | grep -oE 'jrn-[A-Za-z0-9-]+' || true)
  for sids in $ids; do
    curl -s -X DELETE "${JOURNEY_BASE_URL}/sessions/${sids}" >/dev/null 2>&1 || true
  done
  step "cleanup: journey sessions deleted"
}

# --- the residue contract (card t_a99ffcd7) -------------------------
# A journey may leave behind its recipe (checked into the repo) and
# its result files (run.sh's evidence dir, scratch under $TMPDIR). Its
# RUNTIME ACTORS die with the run: sessions, board tasks, roster
# agent homes. cronstate.json wake entries are the serve's own state
# - no recipe ever writes that file; the cron runner's GC (harness/
# cron sweepStaleState) drops them the moment the declaring agent home
# is gone (the reload watcher polls every 5s).

# cleanup_journey_residue: reap every jrn- artifact on all three
# actor surfaces - sessions plus anything a crashed earlier run
# leaked. Board tasks go via DELETE (archiving is a tombstone, not a
# reap - an archived row is still a permanent board row); roster
# agent homes under $OMEGA_HOME/agents go via rm -rf, guarded by the
# jrn- prefix glob (a malformed dir is never rm'd blind). Absent
# modules degrade silently: a lane without the kanban seam answers a
# non-tasks payload and the task sweep reads zero rows.
cleanup_journey_residue() {
  local ids id d reaped=0
  cleanup_journey_sessions
  # board tasks: the FULL scan (the default list hides archived rows -
  # the exact tombstone accumulation this reaper exists to kill), one
  # status=all pass, DELETE per row
  ids=$(curl -s "${JOURNEY_BASE_URL}/tasks?status=all" | python3 -c '
import json,sys
try:
    rows = json.load(sys.stdin).get("tasks") or []
except Exception:
    sys.exit(0)
print(" ".join(t["id"] for t in rows
              if str(t.get("title") or "").startswith("jrn-")))' 2>/dev/null || true)
  for id in $ids; do
    curl -s -X DELETE "${JOURNEY_BASE_URL}/tasks/${id}" >/dev/null 2>&1 || true
    reaped=$((reaped + 1))
  done
  # roster agent homes (only when the lane home is known)
  if [ -n "${OMEGA_HOME:-}" ] && [ -d "${OMEGA_HOME}/agents" ]; then
    for d in "${OMEGA_HOME}"/agents/jrn-*; do
      case "$d" in
        */agents/jrn-*) [ -d "$d" ] && { rm -rf "$d"; reaped=$((reaped + 1)); } ;;
      esac
    done
  fi
  [ "$reaped" = "0" ] || step "residue: reaped $reaped jrn- artifact(s)"
}

# send_journey_message <id> <text>: POST /chat on the lane (the same
# endpoint the composer fires) - used only for seeds a journey needs
# outside the composer path (e.g. refresh-mid-run pre-state).
send_journey_message() {
  local id="$1" text="$2" body
  body=$(printf '{"session_id": "%s", "messages": [{"role": "user", "content": "%s"}]}' "$id" \
         "$(printf '%s' "$text" | sed 's/"/\\"/g')")
  curl -s -X POST "${JOURNEY_BASE_URL}/chat" -H 'Content-Type: application/json' -d "$body" >/dev/null
}