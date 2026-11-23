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
# tasks, roster agent homes, the terminal sessions its page loads claim -
# are reaped by cleanup_journey_residue at every exit path (die, recipe
# EXIT traps, run.sh's pre-clean); wake entries in cronstate.json are the
# serve's own state, swept by the cron runner's GC once the declaring
# agent home is gone.
#
# Env:
#   JOURNEY_BASE_URL    default http://127.0.0.1:8098 (the dev lane)
#   JOURNEY_CHROME_BIN  explicit chromium binary; when set alongside
#                       JOURNEY_CHROME_LIBS, run.sh launches it headless
#                       (the Pi lane). Unset: agent-browser auto-launches
#                       its own Chrome (the CI lane).
#   JOURNEY_CHROME_PORT CDP port for a Pi-lane chrome (default 9333)
#   JOURNEY_SESSION     agent-browser session name (default
#                       omega-journey-<run id>, private to the run)
#   JOURNEY_HTTP_TIMEOUT deadline for one lane REST call (default 20s,
#                       scaled by the clock)
#   JOURNEY_CLOCK_PCT   the run's timing scale, exported by run.sh

JOURNEY_BASE_URL=${JOURNEY_BASE_URL:-http://127.0.0.1:8098}
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
[ -z "$_pin_hits" ] \
	|| { printf '%s: pipes a writer into grep -q (SIGPIPE under pipefail): %s\n' "${_PIN_SELF##*/}" "$_pin_hits" >&2; exit 1; }

JOURNEY_CHROME_PORT=${JOURNEY_CHROME_PORT:-9333}
# the run's own identity: private tag record, private browser session.
# run.sh exports it; a standalone recipe run gets its own shell pid.
JOURNEY_RUN_ID="${JOURNEY_RUN_ID:-$$}"
JOURNEY_SESSION=${JOURNEY_SESSION:-omega-journey-$JOURNEY_RUN_ID}

# JOURNEY_WORKTMP: a scratch path EVERY tool in the recipe can read.
# On MSYS hosts (windows git-bash) the native binaries recipes invoke
# (python3 is a native win32 build) cannot open MSYS /tmp paths - they
# resolve them to <drive>:\tmp and the file is not there. cygpath -m
# rewrites the scratch dir to a mixed C:/... form that bash reads,
# writes and spawns natively; POSIX hosts have no cygpath and pass the
# directory through unchanged (TMPDIR unset on the CI lane stays /tmp).
if [ -n "${JOURNEY_WORKTMP:-}" ]; then
  : # run.sh computed it for the whole run; one value or the tag records split
elif command -v cygpath >/dev/null 2>&1; then
  JOURNEY_WORKTMP="$(cygpath -m "${TMPDIR:-/tmp}")"
else
  JOURNEY_WORKTMP="${TMPDIR:-/tmp}"
fi
export JOURNEY_WORKTMP

# --- the suite clock (mechanism: budgets written on an idle host) ------------
# Every budget below was measured solo, on an idle machine. Under a loaded host
# (a merge train running SPA builds and go builds beside the suite) the same
# recipe takes multiples of that: measured 2026-11-20 on 12 burners, journey 114
# went 23s -> 296s against its 300s wall cap and journey 113 was killed at 25 of
# ~40 steps by that cap after 76s idle. So the suite carries ONE scale, computed
# once by run.sh from the host's own load average against its core count
# (JOURNEY_CLOCK_PCT: 100 = idle, 400 = saturated) and applied here to the two
# budgets that cannot be argued from an event: the browser call deadline and the
# floor every event wait gets. A standalone recipe run has no run.sh, so the
# default is an idle host.
JOURNEY_CLOCK_PCT="${JOURNEY_CLOCK_PCT:-100}"
case "$JOURNEY_CLOCK_PCT" in ''|*[!0-9]*) JOURNEY_CLOCK_PCT=100 ;; esac
[ "$JOURNEY_CLOCK_PCT" -ge 100 ] || JOURNEY_CLOCK_PCT=100

# --- the call deadline (mechanism: the CLI has no deadline of its own) --------
# agent-browser blocks on its daemon socket forever: one lost response parks the
# call, and the recipe with it, until run.sh's wall cap kills the whole recipe -
# the log then stops mid-step and nothing names the call that never came back
# (the silent-hang shape). Every call is bounded. On expiry the session is
# dropped, re-attached when this run attached it, and the call retried once; a
# second expiry is loud and named. timeout(1) is coreutils on every lane; a host
# without it keeps the old unbounded call and run.sh says so at suite start.
JOURNEY_AB_CALL_TIMEOUT="${JOURNEY_AB_CALL_TIMEOUT:-$((120 * JOURNEY_CLOCK_PCT / 100))}"
case "$JOURNEY_AB_CALL_TIMEOUT" in ''|*[!0-9]*) JOURNEY_AB_CALL_TIMEOUT=120 ;; esac
AB_TIMEOUT=0
command -v timeout >/dev/null 2>&1 && AB_TIMEOUT=1
JOURNEY_SESSION_NAME="$JOURNEY_SESSION"

# --- the REST deadline (mechanism: the harness had none) ---------------------
# 164 calls in the corpus (plus lib.sh's own session create/label/delete and the
# residue sweep) ride curl with no answer deadline: curl bounds the CONNECT and
# nothing else, so a lane that accepts the connection and never answers parks
# the recipe until run.sh's wall cap - the silent-hang shape, which run.sh's
# health probe was fixed for alone (t_91615fe6). One deadline for the whole
# corpus, scaled by the clock like every other budget. A caller that passed its
# own -m/--max-time keeps its number. A background STREAM read (-N/--no-buffer,
# recipes 85/95/103) is exempt on purpose: its budget is the poll that reads the
# file it writes, never the transfer. A read (no -X/-d/-F/-T) is retried once
# after the deadline - every write in this corpus is idempotent by id, and a
# probe that answers empty makes a green read as red for no reason.
JOURNEY_HTTP_TIMEOUT="${JOURNEY_HTTP_TIMEOUT:-$((20 * JOURNEY_CLOCK_PCT / 100))}"
case "$JOURNEY_HTTP_TIMEOUT" in ''|*[!0-9]*) JOURNEY_HTTP_TIMEOUT=20 ;; esac
# attempts for a read whose transport never reached an answer (see the shim).
# A crashed curl is cheap to repeat and lands repeatedly under load; a deadline
# is expensive, so it repeats once.
JOURNEY_HTTP_READ_TRIES="${JOURNEY_HTTP_READ_TRIES:-4}"
case "$JOURNEY_HTTP_READ_TRIES" in ''|*[!0-9]*) JOURNEY_HTTP_READ_TRIES=4 ;; esac
[ "$JOURNEY_HTTP_READ_TRIES" -ge 1 ] || JOURNEY_HTTP_READ_TRIES=1
JOURNEY_HTTP_TIMEOUT_TRIES="${JOURNEY_HTTP_TIMEOUT_TRIES:-2}"
case "$JOURNEY_HTTP_TIMEOUT_TRIES" in ''|*[!0-9]*) JOURNEY_HTTP_TIMEOUT_TRIES=2 ;; esac
[ "$JOURNEY_HTTP_TIMEOUT_TRIES" -ge 1 ] || JOURNEY_HTTP_TIMEOUT_TRIES=1
# THE BINARY, NOT THE FUNCTION (mechanism: the shim execs itself). Every recipe
# sources lib.sh a second time (run.sh sources it, then the recipe does), and on
# that second pass `command -v curl` answers "curl" - the shim FUNCTION defined
# below - so the shim exec'd itself. bash burns the stack on the recursion and
# every wire read dies as rc 139/127 with an empty body, which reads exactly like
# a row that never landed: four recipes red on the first read in one suite run.
# type -P looks for a FILE and ignores functions; the resolved path is exported
# so a re-source in any subshell keeps the same answer.
case "${JOURNEY_CURL:-}" in
  /*) ;;
  *) JOURNEY_CURL="$(type -P curl 2>/dev/null)" ;;
esac
export JOURNEY_CURL
journey_curl_url() {
  local a url=""
  for a in "$@"; do
    case "$a" in http://*|https://*) url="$a" ;; esac
  done
  printf '%s' "$url"
}
if [ -n "$JOURNEY_CURL" ]; then
  curl() {
    local a own=0 stream=0 mut=0 url rc why tries
    for a in "$@"; do
      case "$a" in
        -m|--max-time|-m[0-9]*|--max-time=*) own=1 ;;
        -N|--no-buffer) stream=1 ;;
        -X|--request|-d|--data*|-F|--form*|-T|--upload-file) mut=1 ;;
      esac
    done
    if [ "$own" = "1" ] || [ "$stream" = "1" ]; then
      "$JOURNEY_CURL" "$@"
      return
    fi
    url="$(journey_curl_url "$@")"
    "$JOURNEY_CURL" --max-time "$JOURNEY_HTTP_TIMEOUT" "$@"
    rc=$?
    # A READ is safe to repeat: the deadline, the transport class (could not
    # connect, empty reply, recv failure) and a curl that DIED on a signal
    # (rc >= 128; the winbtw ucrt64 build segfaults under suite load) all leave
    # the wire as it was, and an empty answer otherwise makes a green read as
    # red for no reason. A WRITE is retried by the helper that owns its key,
    # never here - these creates carry no id the shim could recognise, and a
    # blind second POST makes a duplicate.
    #
    # The two classes get different budgets. A DEADLINE means the lane is slow
    # and spending another whole deadline on it costs the recipe its clock, so
    # it repeats once. A TRANSPORT fault (or a curl that crashed) never reached
    # an answer at all, costs a second rather than a deadline, and was measured
    # repeating several times in one run under suite load - so it gets
    # JOURNEY_HTTP_READ_TRIES attempts before the read gives up.
    if [ "$mut" = "0" ]; then
      tries=1
      case "$rc" in
        28) tries="$JOURNEY_HTTP_TIMEOUT_TRIES" ;;
        7|52|56) tries="$JOURNEY_HTTP_READ_TRIES" ;;
        *) [ "$rc" -ge 128 ] 2>/dev/null && tries="$JOURNEY_HTTP_READ_TRIES" ;;
      esac
      while [ "$rc" != "0" ] && [ "$tries" -gt 1 ]; do
        tries=$((tries - 1))
        case "$rc" in 28) why="in ${JOURNEY_HTTP_TIMEOUT}s" ;; *) why="rc=$rc" ;; esac
        printf '%s [journey] lane answered nothing %s, retrying (%s left): %s\n' \
          "$(date +%H:%M:%S)" "$why" "$tries" "${url:-?}" >&2
        sleep 1
        "$JOURNEY_CURL" --max-time "$JOURNEY_HTTP_TIMEOUT" "$@"
        rc=$?
      done
    fi
    # -s swallows curl's own words, so a failure that is NOT a deadline used to
    # read as an empty answer the caller then blamed on the product. Every
    # non-zero exit names itself.
    if [ "$rc" != "0" ]; then
      printf '%s [journey] lane call failed rc=%s after %ss: %s\n' \
        "$(date +%H:%M:%S)" "$rc" "$JOURNEY_HTTP_TIMEOUT" "${url:-?}" >&2
    fi
    return "$rc"
  }
fi

# ab <args...>: one agent-browser call, ALWAYS in this run's browser
# session: the daemon multiplexes concurrent clients (another card's
# journey run rides the same daemon) and the default connection is
# global - a session name keeps pages, navigation and the console log
# bucket private to THIS suite (lib.sh lives in run.sh's export).
ab() {
  journey_dead_check
  local rc
  if [ "$AB_TIMEOUT" = "0" ]; then
    "$AB" --session "$JOURNEY_SESSION_NAME" "$@"
    return
  fi
  timeout "$JOURNEY_AB_CALL_TIMEOUT" "$AB" --session "$JOURNEY_SESSION_NAME" "$@"
  rc=$?
  [ "$rc" = "124" ] || return "$rc"
  printf '%s [journey] agent-browser: %ss with no answer (%s) - dropping the session, retrying\n' \
    "$(date +%H:%M:%S)" "$JOURNEY_AB_CALL_TIMEOUT" "${1:-call}" >&2
  timeout 20 "$AB" --session "$JOURNEY_SESSION_NAME" close >/dev/null 2>&1 || true
  if [ "${JOURNEY_CDP_ATTACHED:-0}" = "1" ]; then
    timeout "$JOURNEY_AB_CALL_TIMEOUT" "$AB" --session "$JOURNEY_SESSION_NAME" \
      connect "$JOURNEY_CHROME_PORT" >/dev/null 2>&1 || true
  fi
  timeout "$JOURNEY_AB_CALL_TIMEOUT" "$AB" --session "$JOURNEY_SESSION_NAME" "$@"
  rc=$?
  if [ "$rc" = "124" ]; then
    printf '%s [journey] agent-browser: %s never answered twice at %ss - the CLI or its browser is wedged\n' \
      "$(date +%H:%M:%S)" "${1:-call}" "$JOURNEY_AB_CALL_TIMEOUT" >&2
  fi
  return "$rc"
}

# journey_budget <s>: an event wait's budget, floored at the round-trip floor.
# The corpus's 10s numbers grade a click that WRITES (a registry toggle, a
# save): the row's new state only arrives after the page's fetch, the server's
# own store write and the re-render off the response agree. That round trip is
# the wait class the host's load moves by seconds - measured 11s on a loaded
# host against the 10s journey 114 allowed, which is how a landed write read as
# a no-op toggle. A DOM settle is one render and does not move like that, which
# is why the floor belongs to the event waits and not to wait_settled. A longer
# budget costs nothing on a green run: the poll returns the moment the
# predicate holds.
JOURNEY_RTT_WAIT="${JOURNEY_RTT_WAIT:-$((30 * JOURNEY_CLOCK_PCT / 100))}"
case "$JOURNEY_RTT_WAIT" in ''|*[!0-9]*) JOURNEY_RTT_WAIT=30 ;; esac
journey_budget() {
  local t="$1"
  case "$t" in ''|*[!0-9]*) t=0 ;; esac
  [ "$t" -lt "$JOURNEY_RTT_WAIT" ] && t="$JOURNEY_RTT_WAIT"
  printf '%s' "$t"
}

# step <desc>: progress line
step() { journey_dead_check; printf '%s [journey] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }

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

# --- the death record (mechanism: the command-substitution boundary) --------
# `SID=$(new_journey_session x | tail -1)` runs the helper in a SUBSHELL: a die()
# there stops only the substitution, and the recipe runs on holding an empty
# id - every later step then red on the empty value, and the log reads as a
# product fault one screen away from the real one. die() records its message and
# the primitives read it, so the red that killed the subshell is the red the
# recipe ends on.
journey_death_mark() { printf '%s' "$JOURNEY_WORKTMP/omega-jrn-dead-$JOURNEY_RUN_ID"; }
JOURNEY_DYING=0
journey_dead_check() {
  local mark why
  [ "$JOURNEY_DYING" = "0" ] || return 0
  mark="$(journey_death_mark)"
  [ -f "$mark" ] || return 0
  why="$(head -1 "$mark" 2>/dev/null)"
  rm -f "$mark" 2>/dev/null
  die "${why:-a step died inside a command substitution}"
}

# die <msg>: red journey. Prints, captures evidence, exits non-zero.
die() {
  JOURNEY_DYING=1
  printf '%s [journey RED] %s\n' "$(date +%H:%M:%S)" "$*" >&2
  printf '%s\n' "$*" >"$(journey_death_mark)" 2>/dev/null || true
  ab screenshot 2>/dev/null | tail -1 >&2
  journey_exit_cleanup || true
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
# The budget is FLOORED at $JOURNEY_RTT_WAIT (journey_budget above): the
# numbers in the corpus were measured solo, and the loaded host is the case
# this suite keeps flaking on.
wait_js() {
  local expr="$1" t i=0 out
  t="$(journey_budget "${2:-15}")"
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
# Board.vue's `boards.length > 0`) can never satisfy a hard
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
  local expr="$1" t i=0 out
  t="$(journey_budget "${2:-15}")"
  while [ "$i" -lt $((t * 2)) ]; do
    out=$(jsq "$expr" 2>&1)
    if [ "$out" = "true" ]; then return 0; fi
    sleep 0.5
    i=$((i + 1))
  done
  return 1
}

# --- the determinism contract (card t_fdd12843) -----------------------------
# A recipe asserts on STATE, never on elapsed time. Four gate reds in three
# nights were one root class: a press read mid-unfold (t_44f55a54), an h+4
# clamp offset (113-settings-nav), a poller counted inside a fixed 17s window
# (111-socket-push), a browser-state leak (95-terminal). scripts/recipe-lint.sh
# holds the contract over the corpus; these are the primitives it expects a
# wait to be built from.
#
# Three shapes, and nothing else:
#   poll_js / wait_js     - a predicate that becomes true (an EVENT)
#   wait_settled          - a VALUE that stops changing (a SETTLE: geometry,
#                           computed style, a pane chasing a drag)
#   watch_absent          - a predicate that must stay false for a cadence's
#                           worth of time (an ABSENCE; the only assertion that
#                           needs a duration, so the window lives here, watched
#                           sample by sample, never slept through)
# A plain `sleep` between an action and a read is none of the three: it is a
# guess about how long the machine takes, and it is what the lint flags.

# wait_booted [timeout_s]: the shell publishes document.documentElement
# .dataset.omegaBooted="1" once its boot reads have RESOLVED (app.vue), which
# `ab wait --load networkidle` does NOT mean - networkidle returns during
# hydration, with 1.5-2.3s of quiet before the first app fetch on a loaded
# host, and three nightlies red'd a recipe that installed its recorder inside
# that gap (t_a071ee45).
# Never dies: boot-settled is a PRECONDITION, not the assertion. A stalled
# /models holds the marker (it is inside the boot group), and a recipe must
# not red for provider infra. Sets JOURNEY_BOOT_SETTLED=1/0 and says so; a
# recipe that genuinely needs the boundary (a poller window, a transcript
# read) waits for the marker itself.
wait_booted() {
  JOURNEY_BOOT_SETTLED=0
  if poll_js 'document.documentElement.dataset.omegaBooted==="1"' "${1:-25}"; then
    JOURNEY_BOOT_SETTLED=1
    return 0
  fi
  step "WARN: the boot marker never appeared in ${1:-25}s - the page's boot reads have not resolved (a stalled /models?)"
  return 1
}

# wait_settled <js-expr> <timeout_s> [stable_samples]: poll a js VALUE until it
# stops changing - two consecutive equal samples means the thing SETTLED. This
# is the measurement law for anything that animates: a smooth scroll, a CSS
# transition, a pane width chasing a divider drag, a lane unfolding a card
# under a press. Read once, after the value repeats; never mid-flight (the
# 22px ghost against the 218px settled pane).
# The expression must answer a VALUE (string or number), not a boolean: two
# equal `true`s say nothing about the geometry behind them.
wait_settled() {
  local expr="$1" t="${2:-15}" need="${3:-2}" i=0 prev="" out same=0
  while [ "$i" -lt $((t * 2)) ]; do
    out=$(jsq "$expr" 2>&1 | tail -1)
    if [ -n "$out" ] && [ "$out" = "$prev" ]; then
      same=$((same + 1))
      if [ "$same" -ge "$need" ]; then return 0; fi
    else
      same=0
      prev="$out"
    fi
    sleep 0.5
    i=$((i + 1))
  done
  die "wait_settled timed out after ${t}s: $expr never held still (last: [$out])"
}

# wait_layout [timeout_s]: the dock tree has stopped re-laying out. A viewport
# cross, a divider drag, a pane prune or a re-keyed tree re-renders over a
# frame or three, and nothing publishes when it is done - so the observable is
# the geometry itself: every pane's id in DOM order with its rounded box. This
# is the settle shape for gestures that change LAYOUT rather than state; a
# state change (a drawer opened, a pane closed) is an EVENT and belongs on
# wait_js.
wait_layout() {
  wait_settled '(()=>[...document.querySelectorAll("[data-pane-id]")].map(p=>p.dataset.paneId+"@"+Math.round(p.getBoundingClientRect().width)+"x"+Math.round(p.getBoundingClientRect().height)+"+"+Math.round(p.getBoundingClientRect().x)+"+"+Math.round(p.getBoundingClientRect().y)).join(","))()' "${1:-10}"
}

# watch_absent <js-expr> <window_s> <what> [evidence-expr]: the absence witness.
# Sample a BOOLEAN js expression every 0.5s for the whole window and die at the
# FIRST sample that sees it true. An "it never happened" claim needs a duration
# - a poller's cadence is seconds and a fixed sleep is the only honest way to
# span it - so the window is named here, watched sample by sample, and a
# violation fails with the window named instead of being discovered by a count
# afterwards. The optional evidence expression is read on the hit and printed
# with the red, so the judgement carries its own diagnosis.
watch_absent() {
  local expr="$1" t="${2:-10}" what="${3:-the condition}" i=0 out ev=""
  while [ "$i" -lt $((t * 2)) ]; do
    out=$(jsq "$expr" 2>&1 | tail -1)
    case "$out" in
      true)
        if [ -n "${4:-}" ]; then
          ev=$(jsq "$4" 2>&1 | tail -1 | tr -d '"\r')
        fi
        die "$what became true inside the ${t}s watched window${ev:+ | evidence: $ev}"
        ;;
    esac
    sleep 0.5
    i=$((i + 1))
  done
  step "ok: $what never happened across ${t}s of sampling"
}

# poll_grep <file> <pattern> <timeout_s>: poll a file for a pattern - a stream
# that arrives over its own socket, a log a background process appends to.
# Bounded; returns 1 on timeout (the caller names the red).
poll_grep() {
  local f="$1" pat="$2" t="${3:-20}" i=0
  while [ "$i" -lt $((t * 2)) ]; do
    grep -q "$pat" "$f" 2>/dev/null && return 0
    sleep 0.5
    i=$((i + 1))
  done
  return 1
}

# journey_port_free <port>: 0 when nothing listens on 127.0.0.1:<port>. curl's
# "could not connect" is the answer; any other status means something answered.
# `command` skips the shim above: a probe must not inherit the read retry.
journey_port_free() {
  local rc
  command curl -s -m 1 -o /dev/null "http://127.0.0.1:$1" 2>/dev/null
  rc=$?
  [ "$rc" = "7" ] && return 0
  return 1
}

# journey_pick_free_port <base> [span]: the first port at or above <base> that
# nothing listens on (mechanism: one CDP port for the host). run.sh used one
# default port for every run and ADOPTED whatever answered there, so a second
# run attached to the first run's chrome: one renderer for two suites, one
# localStorage and console buffer across runs, and the owner's exit killing the
# browser mid-recipe for the sibling. Only a caller who NAMED the port (a
# pre-launched browser, a pinned lane) gets the adopt.
journey_pick_free_port() {
  local base="${1:-9333}" span="${2:-200}" p=0 cand
  case "$base" in ''|*[!0-9]*) base=9333 ;; esac
  while [ "$p" -lt "$span" ]; do
    cand=$((base + p))
    if journey_port_free "$cand"; then
      printf '%s' "$cand"
      return 0
    fi
    p=$((p + 1))
  done
  return 1
}

# poll_http <url> <pattern> [timeout_s]: poll a lane route's body for a pattern,
# at the clock floor. A read right after a write is not a wait: the wire lands
# when the write lands, and under load it lands late - a one-shot read turns
# that into a red that names a stale page. Returns 1 on timeout (the caller
# names the red), like poll_grep.
#
# The give-up NAMES THE WIRE it read: how many reads answered and never matched
# against how many never answered at all (mechanism: a crashed transport is not
# an answer). Those are opposite verdicts - an answer without the row is the
# product's, silence is the host's - and before this line both red with the same
# words, which is how a segfaulting curl read as a lost board card.
poll_http() {
  local url="$1" pat="$2" t i=0 out code answered=0 silent=0
  t="$(journey_budget "${3:-10}")"
  while [ "$i" -lt $((t * 2)) ]; do
    out=$(curl -s -w '\n%{http_code}' "$url" 2>/dev/null)
    code="$(printf '%s' "$out" | tail -1)"
    case "$code" in
      2*|3*)
        answered=$((answered + 1))
        grep -q "$pat" <<<"$(printf '%s' "$out" | sed '$d')" && return 0
        ;;
      *) silent=$((silent + 1)) ;;
    esac
    sleep 0.5
    i=$((i + 1))
  done
  printf '%s [journey] %s never showed %s in %s reads (%s answered and never matched, %s never answered)\n' \
    "$(date +%H:%M:%S)" "$url" "$pat" "$i" "$answered" "$silent" >&2
  return 1
}

# journey_online: put the tab back on the network. OFFLINE is CLIENT state a
# recipe toggles on purpose (111 cuts the socket that way to prove the push
# channel), and a red before its own restore left every later recipe in the
# suite on a dead page: "cannot open", or - worse - a click that still answers
# from the stale DOM while its write can never leave the page (the no-op toggle
# shape). Bounded hard: this runs on exit paths, where the CLI itself may be
# what wedged.
journey_online() {
  if command -v timeout >/dev/null 2>&1; then
    timeout 15 "$AB" --session "$JOURNEY_SESSION_NAME" set offline off >/dev/null 2>&1 || true
  else
    "$AB" --session "$JOURNEY_SESSION_NAME" set offline off >/dev/null 2>&1 || true
  fi
}

# journey_clean_room: reset the CLIENT state a journey can mutate, so nothing
# it did becomes the next journey's baseline. This is the cross-journey law
# (t_81d0901b: 77-mobile-drawer closed both sidebars and opened a settings tab,
# then 70-dock-panes-dnd read "7 tabs missing" on a bundle that was fine).
#   - the network, back online (a recipe that died while the tab was offline)
#   - the viewport, back to desktop (a recipe that died at 420px)
#   - localStorage: the dock tree (every pane, its slot and its closed edges),
#     the settings anchor, the theme and every other omega.* knob a recipe
#     flips
#   - sessionStorage: the SPA's route/focus memory
# A no-op until this recipe has opened a page: a REST-only journey must not
# spawn a chrome on its way out.
JOURNEY_PAGE_OPENED=${JOURNEY_PAGE_OPENED:-0}
journey_clean_room() {
  journey_online
  [ "$JOURNEY_PAGE_OPENED" = "1" ] || return 0
  ab set viewport 1280 800 >/dev/null 2>&1 || true
  jsq '(()=>{try{localStorage.clear();sessionStorage.clear();return 1}catch(e){return 0}})()' \
    >/dev/null 2>&1 || true
}

# journey_exit_cleanup: the ONE exit path. run.sh's recipe subshell installs it
# as its EXIT trap AND calls it at the recipe's return, die() calls it, and a
# recipe that sets its own trap chains to it. The calls overlap by design (a die
# re-enters through the EXIT trap, a recipe whose own trap replaced the
# subshell's still gets the explicit call), so the work runs ONCE: a second call
# would only repeat reads and DELETEs on a store this run already emptied.
# Residue (sessions, board cards, agent homes, the terminal sessions the recipe's
# page loads claimed) plus the client state.
JOURNEY_EXIT_CLEANED=0
journey_exit_cleanup() {
  [ "$JOURNEY_EXIT_CLEANED" = "0" ] || return 0
  JOURNEY_EXIT_CLEANED=1
  journey_clean_room
  cleanup_journey_residue
  [ -n "${JRN_TAGFILE:-}" ] && rm -f "$JRN_TAGFILE" 2>/dev/null
  return 0
}

# open_app: fresh SPA load in the journey session + console hook. The
# viewport resets to desktop FIRST: a recipe that died mid-way leaves
# its mobile viewport on the tab (77-mobile-drawer crosses to 420px and
# dies at mobile), and the next recipe must not inherit it.
# The ENTRY CLEAN ROOM: once the page has an origin, a recipe clears the state
# the previous one left and reloads ONLY when there was something to clear (a
# virgin profile costs no second load). FIRST open_app of the recipe only: a
# recipe that re-opens the app mid-journey (a reload pass, a mid-run refresh)
# is asserting on state IT planted, so the clean room must not run again.
# Then the boot marker is the load boundary - networkidle is not one
# (t_a071ee45).
open_app() {
  journey_online
  ab set viewport 1280 800 >/dev/null 2>&1 || true
  ab open "$JOURNEY_BASE_URL/" >/dev/null 2>&1 || \
    { JOURNEY_CDP_ATTACHED=0; ab open "$JOURNEY_BASE_URL/" >/dev/null 2>&1 || die "cannot open $JOURNEY_BASE_URL"; }
  JOURNEY_PAGE_OPENED=1
  ab wait --load networkidle >/dev/null 2>&1 || true
  if [ "${JOURNEY_CLEAN_DONE:-0}" = "0" ]; then
    JOURNEY_CLEAN_DONE=1
    had=$(jsq '(()=>{try{const n=localStorage.length+sessionStorage.length;localStorage.clear();sessionStorage.clear();return n}catch(e){return 0}})()' 2>&1 | tail -1 | tr -d '"')
    case "$had" in
      ''|*[!0-9]*) had=0 ;;
    esac
    if [ "$had" != "0" ]; then
      step "clean room: cleared $had client key(s) a previous journey left, reloading"
      ab reload >/dev/null 2>&1 || true
      ab wait --load networkidle >/dev/null 2>&1 || true
    fi
  fi
  wait_booted
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
  if grep -qiE 'error|referenceerror|typeerror|syntaxerror' <<<"$errs"; then
    die "console errors on $JOURNEY_BASE_URL: $errs"
  fi
  step "ok: console silent"
}

# journey session lifecycle -------------------------------------------------

# JRN_TAGFILE: the per-RUN tag record (mechanism: one host-global file).
# The tag names this run's sessions inside their ids, and a recipe reads it
# back after the $(...) subshell that created the session. It used to be one
# path under $TMPDIR for the whole host, so a worker's targeted recipe run and a
# suite (or two targeted runs) overwrote each other's tag mid-journey: the
# second writer's tag came back to the first, whose label and search then looked
# for a session it never created. JOURNEY_RUN_ID (top of this file) makes the
# path private to the run.
JRN_TAGFILE="${JRN_TAGFILE:-$JOURNEY_WORKTMP/omega-jrn-runtag-$JOURNEY_RUN_ID}"

# --- who owns a jrn- artifact (mechanism: one lane, sibling runs) -------------
# Every runtime actor a journey plants carries the planting run in its name:
# jrn-<tag>-<runid>-<what>, where the tag is ddMmmHHMMSS and runid is run.sh's
# pid. The residue reaper used to delete EVERY jrn- artifact on the lane from
# every recipe's exit path, so a suite erased a concurrent sibling's session,
# board card and roster home mid-journey - the sibling then red on state it had
# only just created, and the red named the product. A second live run is a
# SIBLING, not residue: the run's own exit path owns its artifacts, and the
# reaper reaps a foreign one only once the run that planted it is gone.

# journey_run_live <runid>: 0 while that run's live marker names a live process.
journey_run_live() {
  local rid="$1"
  case "$rid" in ''|*[!0-9]*) return 1 ;; esac
  [ -f "$JOURNEY_WORKTMP/omega-jrn-live-$rid" ] || return 1
  kill -0 "$rid" 2>/dev/null || return 1
  return 0
}

# journey_artifact_owner <jrn-name>: the run id inside the name, empty when the
# name carries none (a legacy or hand-written jrn- artifact).
journey_artifact_owner() {
  local rest
  case "$1" in jrn-*) ;; *) return 0 ;; esac
  rest="${1#jrn-}"
  rest="${rest#*-}"
  case "${rest%%-*}" in ''|*[!0-9]*) return 0 ;; esac
  printf '%s' "${rest%%-*}"
}

# journey_reap_verdict <jrn-name>: 0 when this run may delete that artifact.
journey_reap_verdict() {
  local ours own
  ours="$(journey_tag 2>/dev/null || true)"
  if [ -n "$ours" ]; then
    case "$1" in "jrn-$ours-"*) return 0 ;; esac
  fi
  own="$(journey_artifact_owner "$1")"
  [ -z "$own" ] && return 0
  journey_run_live "$own" && return 1
  return 0
}


# new_journey_session <name>: POST /sessions with a unique id; prints
# the id; writes the unique run tag to $JRN_TAGFILE (function runs in a
# $(...) subshell, so state crosses back through the file).
new_journey_session() {
  local name="$1" resp id runtag rc
  # the tag carries the RUN (mechanism: a one-second session namespace). The
  # bare timestamp made the id of a recipe in run A and the same recipe in run B
  # identical whenever they started in the same second, so the second run drove
  # the first run's session (and either run's cleanup deleted it mid-journey).
  runtag="$(date +%d%b%H%M%S)-$JOURNEY_RUN_ID"
  printf '%s' "$runtag" > "$JRN_TAGFILE"
  id="jrn-${runtag}-${name}"
  resp=$(curl -s -X POST "${JOURNEY_BASE_URL}/sessions" -H 'Content-Type: application/json' \
         -d "{\"id\": \"${id}\"}")
  rc=$?
  # the id is the RUN's, so a second POST lands the same session - never a twin.
  # A lane that drops the connection under load answers nothing at all, and the
  # one-shot shape died on it with an empty message (the red said "failed: ").
  # The transport is what fails here, not the lane (curl rc 128+ dies on a signal
  # under suite load), so it gets the same bounded budget a read does.
  tries="$JOURNEY_HTTP_READ_TRIES"
  while ! grep -q "$id" <<<"$resp" && [ "$tries" -gt 1 ]; do
    tries=$((tries - 1))
    printf '%s [journey] POST /sessions answered nothing (rc=%s) - retrying (%s left, the id is this run-owned session)\n' \
      "$(date +%H:%M:%S)" "$rc" "$tries" >&2
    sleep 1
    resp=$(curl -s -X POST "${JOURNEY_BASE_URL}/sessions" -H 'Content-Type: application/json' \
           -d "{\"id\": \"${id}\"}")
    rc=$?
  done
  grep -q "$id" <<<"$resp" \
    || die "POST /sessions failed (rc=$rc): ${resp:-<no answer>}"
  printf '%s\n' "$id"
}

# journey_tag: read the run tag a previous subshell left
journey_tag() { [ -f "$JRN_TAGFILE" ] && cat "$JRN_TAGFILE" 2>/dev/null; return 0; }

# label_journey_session: friendlier sidebar row + stable search token
label_journey_session() {
  local id="$1" label="$2"
  curl -s -X PUT "${JOURNEY_BASE_URL}/sessions/${id}" \
    -H 'Content-Type: application/json' -d "{\"label\": \"${label}\"}" >/dev/null
  step "labeled: ${label}"
}

# cleanup_journey_sessions: delete every journey session THIS run owns, plus any
# leftover whose owning run is gone (a crashed run leaves its marker behind with
# a dead pid, so its sessions are reaped by the next run's pre-clean). A live
# sibling's session is left alone - see journey_reap_verdict.
cleanup_journey_sessions() {
  local sids ids
  # this run's id (fast path)
  if [ -n "${JRN_ID:-}" ]; then
    curl -s -X DELETE "${JOURNEY_BASE_URL}/sessions/${JRN_ID}" >/dev/null 2>&1 || true
  fi
  # leftovers (by id prefix), minus a sibling run's live ones
  ids=$(curl -s "${JOURNEY_BASE_URL}/sessions" | grep -oE '"id": ?"jrn-[A-Za-z0-9-]+"' | grep -oE 'jrn-[A-Za-z0-9-]+' || true)
  for sids in $ids; do
    journey_reap_verdict "$sids" || continue
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

# --- the terminal reap (the pty a page load claims) --------------------------
# A page load mounts the bottom pane, the pane claims a pty, and the pty
# outlives the page by design (TerminalTab.vue: the unmount unbinds the SSE,
# never the SHELL). Nothing reaped them: 3 live sessions accumulated across 5
# recipes on a scratch lane, and the store's maxSessions answers the next claim
# with 409 "session cap reached; close a terminal pane first", which the pane
# renders as "no terminal" - a red waiting for whichever recipe next reads it.
# The baseline is read when this file is sourced (recipe start, before any
# page can claim) and the diff is killed in the residue path below, so every
# recipe gets it for free - green, red and die alike.
JOURNEY_TERM_SNAPSHOT=0
JOURNEY_TERM_BEFORE=""

# journey_term_ids: the store's live session ids, one per line, sorted. A lane
# without the terminal capsule answers a non-listing body and this reads empty.
journey_term_ids() {
  local body
  body=$(curl -s "${JOURNEY_BASE_URL}/terminal/sessions" 2>/dev/null) || return 0
  printf '%s' "$body" | grep -oE '"id": ?"[a-f0-9-]+"' \
    | sed 's/.*"\([a-f0-9-]*\)".*/\1/' | sort
}

# journey_term_snapshot: record the store as the recipe found it. Idempotent -
# a recipe sources this file a second time, and the first read is the honest
# baseline.
journey_term_snapshot() {
  [ "$JOURNEY_TERM_SNAPSHOT" = "1" ] && return 0
  JOURNEY_TERM_SNAPSHOT=1
  JOURNEY_TERM_BEFORE="$(journey_term_ids)"
}

# journey_term_reap: kill every session this recipe's page loads minted.
# The pane RELAIMS what a bare DELETE takes: the dropped SSE reads as a missed
# attach, so TerminalTab reconnects and claims a fresh pty half a second later
# - a hand-rolled reap leaks one shell per run that way. Navigation is the hard
# stop (the claim ladder dies with the document), so a live page is released
# before the first DELETE. Bounded and never dies: this runs on exit paths.
journey_term_reap() {
  local now id round=0 reaped=0
  [ "$JOURNEY_TERM_SNAPSHOT" = "1" ] || return 0
  now="$(journey_term_ids)"
  [ "$now" = "$JOURNEY_TERM_BEFORE" ] && return 0
  if [ "${JOURNEY_PAGE_OPENED:-0}" = "1" ] && [ -n "${AB:-}" ]; then
    ab open about:blank >/dev/null 2>&1 || true
    JOURNEY_PAGE_OPENED=0
  fi
  # two rounds: the first kills what the recipe minted, the second catches a
  # claim already in flight when the page went away
  while [ "$round" -lt 2 ]; do
    round=$((round + 1))
    for id in $(comm -13 <(printf '%s\n' "$JOURNEY_TERM_BEFORE") <(journey_term_ids)); do
      curl -s -X DELETE "${JOURNEY_BASE_URL}/terminal/${id}" >/dev/null 2>&1 || true
      reaped=$((reaped + 1))
    done
    now="$(journey_term_ids)"
    [ "$now" = "$JOURNEY_TERM_BEFORE" ] && break
  done
  [ "$reaped" = "0" ] \
    || step "residue: killed $reaped terminal session(s) this recipe's page loads claimed"
  [ "$now" = "$JOURNEY_TERM_BEFORE" ] \
    || step "WARN: the store still lists $(printf '%s\n' "$now" | grep -c .) terminal session(s) beyond the recipe's baseline"
}

# cleanup_journey_residue: reap every jrn- artifact on all three
# actor surfaces - sessions plus anything a crashed earlier run
# leaked. Board tasks go via DELETE (archiving is a tombstone, not a
# reap - an archived row is still a permanent board row); roster
# agent homes under $OMEGA_HOME/agents go via rm -rf, guarded by the
# jrn- prefix glob (a malformed dir is never rm'd blind). Absent
# modules degrade silently: a lane without the kanban seam answers a
# non-tasks payload and the task sweep reads zero rows.
cleanup_journey_residue() {
  local ids id tit d reaped=0 tab
  journey_term_reap
  cleanup_journey_sessions
  # board tasks: the FULL scan (the default list hides archived rows -
  # the exact tombstone accumulation this reaper exists to kill), one
  # status=all pass, DELETE per row, a live sibling's rows left alone
  ids=$(curl -s "${JOURNEY_BASE_URL}/tasks?status=all" | python3 -c '
import json,sys
try:
    rows = json.load(sys.stdin).get("tasks") or []
except Exception:
    sys.exit(0)
for t in rows:
    if str(t.get("title") or "").startswith("jrn-"):
        print(str(t.get("id") or "") + "\t" + str(t.get("title") or ""))' 2>/dev/null || true)
  tab="$(printf '\t')"
  while IFS="$tab" read -r id tit; do
    [ -n "$id" ] || continue
    journey_reap_verdict "${tit:-jrn-$id}" || continue
    curl -s -X DELETE "${JOURNEY_BASE_URL}/tasks/${id}" >/dev/null 2>&1 || true
    reaped=$((reaped + 1))
  done <<< "$ids"
  # roster agent homes (only when the lane home is known). The DELETE
  # rides the product's own verb first (card t_5df01d0f): once an agent's
  # chat has been served the daemon holds that home's store open, and on
  # windows a held handle makes the data files unremovable - RemoveAll
  # would leave a half-deleted home that keeps registering the agent. The
  # verb releases the handle; the rm below stays for a lane whose route
  # never answered.
  if [ -n "${OMEGA_HOME:-}" ] && [ -d "${OMEGA_HOME}/agents" ]; then
    for d in "${OMEGA_HOME}"/agents/jrn-*; do
      case "$d" in
        */agents/jrn-*)
          [ -d "$d" ] || continue
          journey_reap_verdict "${d##*/}" || continue
          curl -s -X DELETE "${JOURNEY_BASE_URL}/agents/${d##*/}" >/dev/null 2>&1 || true
          [ -d "$d" ] && { rm -rf "$d"; reaped=$((reaped + 1)); }
          ;;
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

# THE BASELINE, read once at source time. A source is the recipe's first line,
# so no page this recipe drives can have claimed yet, and a recipe that sources
# this file a second time keeps the first read (journey_term_snapshot is
# idempotent).
journey_term_snapshot
