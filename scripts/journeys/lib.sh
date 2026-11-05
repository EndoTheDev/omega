#!/bin/bash
# lib.sh - shared mechanics for the omega validation journeys.
#
# A recipe does: open the SPA, act like the customer, assert the
# user-visible outcome, exit 0 on green or die() on red. lib.sh owns the
# browser channel (agent-browser over CDP to a Hermes-bundled headless
# Chromium), the assertion helpers, and the journey-session cleanup trap.
#
# Recipes never send real traffic against live customer sessions: each
# write-journey creates its own throwaway session (id prefix `jrn-`,
# label prefix `(journey)`) and lib.sh deletes it in a cleanup trap.
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

# die <msg>: red journey. Prints, captures evidence, exits non-zero.
die() {
  printf '%s [journey RED] %s\n' "$(date +%H:%M:%S)" "$*" >&2
  ab screenshot 2>/dev/null | tail -1 >&2
  cleanup_journey_sessions || true
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

# send_journey_message <id> <text>: POST /chat on the lane (the same
# endpoint the composer fires) - used only for seeds a journey needs
# outside the composer path (e.g. refresh-mid-run pre-state).
send_journey_message() {
  local id="$1" text="$2" body
  body=$(printf '{"session_id": "%s", "messages": [{"role": "user", "content": "%s"}]}' "$id" \
         "$(printf '%s' "$text" | sed 's/"/\\"/g')")
  curl -s -X POST "${JOURNEY_BASE_URL}/chat" -H 'Content-Type: application/json' -d "$body" >/dev/null
}