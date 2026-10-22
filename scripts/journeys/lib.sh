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

# open_app: fresh SPA load in the journey session + console hook
open_app() {
  ab open "$JOURNEY_BASE_URL/" >/dev/null 2>&1 || \
    { JOURNEY_CDP_ATTACHED=0; ab open "$JOURNEY_BASE_URL/" >/dev/null 2>&1 || die "cannot open $JOURNEY_BASE_URL"; }
  ab wait --load networkidle >/dev/null 2>&1 || true
  step "open: $JOURNEY_BASE_URL"
}

# open_journey_row <label>: click the journey session's sidebar row,
# retrying while the sidebar's session list is still loading (the SPA
# fetches /sessions after boot - a one-shot find races that boot).
# The find can also dispatch a REAL click too early: a row whose Vue
# handlers have not hydrated yet swallows the click silently (route
# unchanged, no tab opens) while the find reports success. So the
# verdict is the OPEN - the focused chat tab drives the route to
# /chat/<id> - re-clicked on every poll until the route moves.
open_journey_row() {
  local label="$1" i=0 opened
  while [ "$i" -lt 30 ]; do
    if ab find text "$label" click >/dev/null 2>&1; then
      opened=$(jsq 'location.pathname.startsWith("/chat/") ? 1 : 0' 2>&1 | tail -1)
      if [ "$opened" = "1" ]; then
        step "ok: opened journey session row: $label"
        return 0
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

JRN_TAGFILE="${TMPDIR:-/tmp}/omega-jrn-runtag"

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