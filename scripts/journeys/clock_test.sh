#!/usr/bin/env bash
set -uo pipefail

# clock_test.sh - the witness for the journeys suite's timing budgets
# (card t_dd3abe4e, the suite-load flake family).
#
# THE FACTS (measured 2026-11-20 on the winbtw train host, 12 cpus):
#   - journey 114 took 23s solo and 296s with 12 cpu burners beside it, against
#     a 300s wall cap; its click -> server write -> re-render wait came in at
#     ~11s against the 10s the recipe allowed, which is how a landed toggle
#     write read as a no-op
#   - journey 113 took 76s solo and was killed by the same 300s cap at 25 of
#     ~40 steps under that load - the log stops mid-step and nothing names the
#     call that never came back
# Both reds are budgets written on an idle host. Four things answer them:
#
#   a  a browser call DEADLINE. agent-browser waits on its daemon socket with
#      no deadline of its own, so one lost response parks the recipe until the
#      wall cap. lib.sh bounds every call, drops the session on expiry,
#      re-attaches and retries once - P1 proves the recovery, P2 proves the pin
#      can fail (the deadline stripped = the old hang).
#   b  ONE scale for the whole run, measured from the host's own load average
#      against its core count (run.sh): the wall cap, the browser call deadline
#      and the event-wait floor all move together, and the suite prints the
#      budget it ran on. P3 pins the arithmetic, P4 the idle numbers, P8 the
#      ratchet that follows a load arriving MID-suite.
#   c  the wait floor itself, applied in wait_js/poll_js so the corpus's 10s
#      numbers inherit it. P5 pins that wait_js really waits the floor's
#      budget, not the number its caller asked for.
#   d  the wall cap's own clock. The watchdog's `sleep` used to outlive the
#      subshell that owned it and hold the CALLER's stdout, so a piped caller
#      hung for the whole cap after "suite done" - and the scale above makes
#      that cap up to four times longer. P7 pins the pipe closing with the suite
#      and proves the pre-fix shape holds it.
#
# P6 covers the sibling-state half: the run tag record is per RUN now, because
# one host-global path let a second run overwrite the tag the first was still
# reading (its label and search then looked for a session it never created).
#
# The batch pass that followed the two reds (the same card, second occurrence)
# closed the three mechanisms the wall-cap and the 10s wait left standing:
#
#   e  A REST DEADLINE. curl bounds the CONNECT and nothing else, so the 164
#      recipe calls to the lane (plus lib.sh's own session/residue calls) parked
#      the recipe to the wall cap whenever the lane accepted and did not answer -
#      the same class run.sh's health probe was fixed for alone. lib.sh's curl
#      shim carries one clock-scaled deadline, names the URL it gave up on,
#      retries a READ once, and leaves a background stream read (-N) alone.
#      P9 proves the bound and proves the pre-fix shape parks to the cap.
#   f  THE WALL STRETCH. load1 counts runnable threads; it does not say what a
#      second costs here. On the train host load1 13.67/12 cpus read as 113%
#      while `sleep 0.2` took 0.34s (171%), and the budgets are spent in wall
#      seconds. The clock now takes the larger of the load average and a timed
#      sleep, ratcheted, and prints which signal set it. P10 drives the probe to
#      a known stretch from a host that reports an idle load.
#   g  THE BROWSER SESSION BELONGS TO THE RUN. One default name for the host put
#      every run (a worker's targeted run, a crashed suite's daemon, the gate) in
#      one page, one console buffer and one localStorage. P11 proves two runs
#      differ and carries the pre-fix shape.
#
# The sibling pass closed the two state-sharing mechanisms that survive a run's
# own budgets, both of them "one lane, two live runs":
#
#   h  A LIVE SIBLING IS NOT RESIDUE. Every recipe's exit path reaps jrn-*
#      artifacts, and it reaped EVERY one on the lane - so a suite deleted a
#      concurrent sibling's session, board card and roster home mid-journey, and
#      the sibling red on state it had just planted. The planting run now rides
#      the name, run.sh publishes a live marker, and a foreign artifact is reaped
#      only once its run is gone. P12 drives own / crashed / live-sibling through
#      a delete-logging lane and proves the pre-fix shape deletes the live one.
#   i  THE CDP PORT BELONGS TO THE RUN TOO. One default port per host, and every
#      run adopted whatever answered there: two suites in one renderer and one
#      localStorage, and the owner's exit killed the browser under the sibling.
#      An unpinned run takes a free port of its own (pinned = the caller NAMED
#      one). P13 holds a chrome-shaped listener on a port and proves the run
#      steps off it, and that the pre-fix shape drives it.
#
# And the two the flake family actually runs THROUGH, both shared client state:
#
#   j  A DIE INSIDE A COMMAND SUBSTITUTION. `SID=$(new_journey_session x | tail
#      -1)` runs the helper in a subshell: a die() there stops only the
#      substitution, and the recipe runs on with an empty id, reding one screen
#      away from the fault. die() records its message; every primitive and the
#      recipe's own boundary read it. P14 drives the substitution death and the
#      pre-fix cascade.
#   k  THE OFFLINE TAB. 111 cuts the socket by putting the TAB offline. A red
#      before its own restore left every later recipe on a dead page ("cannot
#      open"), and one that still answers from the stale DOM has its writes
#      never leave the page - the no-op toggle. The clean room owns client state,
#      so it restores the network on every exit path. P15 holds the stub's argv
#      log to the restore, and to the pre-fix tree that never issues it.
#
# And the two the LOAD injects, both on the transport:
#
#   l  A CRASHED TRANSPORT. The host's curl build segfaults under suite load
#      (measured rc 139 in the corpus), and a read that dies mid-flight answers
#      NOTHING - the same empty body a poll reads as a missing row. The shim
#      retries the transport classes (7/52/56 and any signal death) up to
#      JOURNEY_HTTP_READ_TRIES, so one crash costs a second, not a recipe. P16
#      proves the recovery, and holds the one-attempt shape to the red.
#   m  A POLL THAT NEVER GOT AN ANSWER. poll_http counts answers and silence
#      apart and names BOTH when it gives up: a wire that never spoke is the
#      host's red, an answer that never carried the row is the product's, and
#      before this line the two red with the same words. P17 drives both
#      branches and holds the silent give-up to the pre-fix tree.
#
# Fixtures are a copy of the journeys tree, a stub agent-browser and a stub
# lane - no real browser, no real lane, nothing shared with a live run. The stub
# dir rides PATH in POSIX form: an MSYS shell splits a C:/ PATH entry at the
# drive colon, the stub then never resolves, the REAL CLI answers, and every leg
# below reads green against the thing it means to replace. CLOCK_TEST_KEEP=1
# keeps the fixtures for a post-mortem.
#
# Run: bash scripts/journeys/clock_test.sh

JROOT="$(cd "$(dirname "$0")" && pwd)"

pass=0
fail=0
ok() { pass=$((pass + 1)); printf 'ok: %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf 'FAIL: %s\n' "$1" >&2; }

TMP="$(mktemp -d "${TMPDIR:-/tmp}/clock-test.XXXXXX")"
# NATIVE_TMP for the win32 python (it cannot open a POSIX path); TMP stays in
# POSIX form because it rides PATH and is read by MSYS tools.
NATIVE_TMP="$TMP"
if command -v cygpath >/dev/null 2>&1; then
  NATIVE_TMP="$(cygpath -m "$TMP")"
  TMP="$(cygpath -u "$TMP")"
fi
LANE_PID=""
cleanup() {
  [ -n "$LANE_PID" ] && kill "$LANE_PID" 2>/dev/null
  [ -n "${CHROME_STATE_PID:-}" ] && kill "$CHROME_STATE_PID" 2>/dev/null
  case "${TMP:-}" in
    *clock-test.*) [ "${CLOCK_TEST_KEEP:-0}" = "0" ] && rm -rf "$TMP" 2>/dev/null ;;
  esac
  return 0
}
trap cleanup EXIT

# --- fixtures ----------------------------------------------------------------

# stub agent-browser. One call hangs (the wedged CLI: no answer, ever) when the
# marker file exists, then answers normally - so the deadline's drop-and-retry
# has something to recover TO. `eval` answers $STUB_EVAL_ANSWER so a poll can be
# made to miss on purpose.
mkdir -p "$TMP/bin"
cat > "$TMP/bin/agent-browser" <<'STUB'
#!/bin/bash
args=()
while [ $# -gt 0 ]; do
  case "$1" in
    --session) shift 2 ;;
    *) args+=("$1"); shift ;;
  esac
done
if [ "${args[0]:-}" = "eval" ] && [ -f "${STUB_HANG:?}" ]; then
  rm -f "$STUB_HANG"
  sleep 60
fi
# the offline toggles are observable: every call's argv lands in the log the
# witness reads (the clean room's restore is one of them)
if [ -n "${STUB_AB_LOG:-}" ]; then
  printf '%s\n' "${args[*]}" >> "$STUB_AB_LOG"
fi
case "${args[0]:-}" in
  console) : ;;
  eval) printf '%s\n' "${STUB_EVAL_ANSWER:-false}" ;;
  *) : ;;
esac
exit 0
STUB
chmod +x "$TMP/bin/agent-browser"

# stub `sleep`: it stretches ONLY the wall probe's exact duration, so the clock's
# second signal can be driven to a known percentage on a host whose real
# scheduler is idle (CI). Every other sleep is the real one, exec'd.
REAL_SLEEP="$(command -v sleep)"
cat > "$TMP/bin/sleep" <<'STUB'
#!/bin/bash
if [ "${1:-}" = "0.${PROBE_ARG:-200}" ]; then
  exec "$REAL_SLEEP" "0.${PROBE_STRETCHED:-600}"
fi
exec "$REAL_SLEEP" "$@"
STUB
chmod +x "$TMP/bin/sleep"

# stub curl: the host's curl build SEGFAULTS under suite load (measured rc 139
# in the corpus - 102's POST /sessions retried onto the same crash). While
# $STUB_CURL_DIE_FILE names a count above zero a call dies the way that build
# does and eats one; every other call is the REAL curl, baked here because this
# stub owns `command -v curl` for the whole fixture PATH.
REAL_CURL="$(command -v curl)"
cat > "$TMP/bin/curl" <<'STUB'
#!/bin/bash
if [ -n "${STUB_CURL_DIE_FILE:-}" ] && [ -f "$STUB_CURL_DIE_FILE" ]; then
  n="$(cat "$STUB_CURL_DIE_FILE" 2>/dev/null)"
  case "$n" in
    ''|*[!0-9]*) ;;
    0) ;;
    *) printf '%s' "$((n - 1))" >"$STUB_CURL_DIE_FILE"; kill -SEGV $$ ;;
  esac
fi
exec __REAL_CURL__ "$@"
STUB
python3 - "$TMP/bin/curl" "$REAL_CURL" <<'PY'
import sys
path, real = sys.argv[1], sys.argv[2]
src = open(path, encoding="utf-8").read()
assert "__REAL_CURL__" in src, "the stub's exec target is not where the fixture put it"
open(path, "w", encoding="utf-8", newline="").write(src.replace("__REAL_CURL__", real, 1))
PY
chmod +x "$TMP/bin/curl"

# stub lane: run.sh's health gate, the model probe, the residue sweep and
# POST /sessions (which echoes the id it is handed, so a journey session can be
# created against it) all answer.
PORT="$(python3 -c '
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()')"
cat > "$TMP/lane.py" <<'PY'
import http.server
import json
import os
import sys
import time

STATE = sys.argv[2] if len(sys.argv) > 2 else ""


def _state(name, default="{}"):
    if not STATE:
        return default
    try:
        with open(os.path.join(STATE, name), encoding="utf-8") as fh:
            return fh.read()
    except OSError:
        return default


def _log(name, line):
    if not STATE:
        return
    try:
        with open(os.path.join(STATE, name), "a", encoding="utf-8") as fh:
            fh.write(line + "\n")
    except OSError:
        pass


class Handler(http.server.BaseHTTPRequestHandler):
    def _send(self, body=b"{}"):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path.startswith("/health"):
            self._send(b'{"status":"ok"}')
        elif self.path.startswith("/json/version"):
            self._send(b'{"Browser":"Chrome/999.0.0.0"}')
        elif self.path.startswith("/hang"):
            # a lane that ACCEPTS and never answers (the REST deadline's target)
            time.sleep(120)
            try:
                self._send(b"{}")
            except OSError:
                # the caller gave up on its own deadline - the answer it never
                # waited for is not a server error
                pass
        elif self.path.startswith("/sessions"):
            self._send(_state("sessions.json").encode())
        elif self.path.startswith("/tasks"):
            self._send(_state("tasks.json").encode())
        else:
            self._send(b"{}")

    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(n).decode("utf-8", "replace") if n else "{}"
        try:
            sid = json.loads(body).get("id") or "jrn-stub"
        except Exception:
            sid = "jrn-stub"
        self._send(json.dumps({"id": sid}).encode())

    def do_PUT(self):
        self._send(b"{}")

    def do_DELETE(self):
        tail = self.path.rstrip("/").rsplit("/", 1)[-1]
        if self.path.startswith("/sessions/"):
            _log("deleted-sessions.log", tail)
        elif self.path.startswith("/tasks/"):
            _log("deleted-tasks.log", tail)
        self._send(b"{}")

    def log_message(self, *args):
        pass


http.server.ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()
PY
LANESTATE="$NATIVE_TMP/lanestate"
mkdir -p "$LANESTATE"
python3 "$NATIVE_TMP/lane.py" "$PORT" "$LANESTATE" &
LANE_PID=$!
for _ in $(seq 1 40); do
  curl -s -m 2 "http://127.0.0.1:$PORT/health" >/dev/null 2>&1 && break
  sleep 0.2
done

# make_tree <dir>: a run.sh-shaped tree whose recipes are the probes.
make_tree() {
  mkdir -p "$1/recipes"
  cp "$JROOT/lib.sh" "$JROOT/run.sh" "$JROOT/sweep-chromes.sh" "$JROOT/sweep-chromes.ps1" \
     "$JROOT/sweep-daemons.sh" "$1"/
  # a plain browser call: the one the stub can wedge. stderr is KEPT - the
  # deadline's own words ("with no answer ... retrying") are the evidence.
  cat > "$1/recipes/900-call.sh" <<'R'
#!/bin/bash
ab eval '1' >/dev/null
step "call answered"
R
  # the budgets a recipe actually runs with. The floor assertions are written
  # against the floor ITSELF so they hold at any scale: a budget under it is
  # raised to it, a budget above it is untouched.
  cat > "$1/recipes/901-budgets.sh" <<'R'
#!/bin/bash
step "budgets cap=$JOURNEY_TIMEOUT rtt=$JOURNEY_RTT_WAIT deadline=$JOURNEY_AB_CALL_TIMEOUT pct=$JOURNEY_CLOCK_PCT"
assert_eq "$(journey_budget $((JOURNEY_RTT_WAIT - 1)))" "$JOURNEY_RTT_WAIT" "a budget under the floor is raised to it"
assert_eq "$(journey_budget $((JOURNEY_RTT_WAIT + 100)))" "$((JOURNEY_RTT_WAIT + 100))" "a budget above the floor is left alone"
R
  # the floor in the primitive that matters: wait_js must wait the floor's
  # budget, not the 0 its caller asked for
  cat > "$1/recipes/902-floor-applies.sh" <<'R'
#!/bin/bash
wait_js 'false' 0
die "wait_js returned on a false predicate"
R
  # the run tag record follows the RUN, not the host
  cat > "$1/recipes/903-run-tag.sh" <<'R'
#!/bin/bash
sid=$(new_journey_session tag-probe)
tag=$(journey_tag)
step "tag record=$JRN_TAGFILE tag=$tag runid=$JOURNEY_RUN_ID session=$sid"
R
  # the host gets heavier between two recipes: the clock must follow it
  cat > "$1/recipes/904-raise-load.sh" <<'R'
#!/bin/bash
printf '100000.0 100000.0 100000.0 1/1\n' > "$JOURNEY_LOADAVG"
step "host load raised on the next sample"
R
  # a REST call to a lane that accepts and never answers. stderr is KEPT - the
  # shim's own words ("answered nothing ... retrying once") are the evidence.
  cat > "$1/recipes/905-rest-deadline.sh" <<'R'
#!/bin/bash
rc=0
curl -s "$JOURNEY_BASE_URL/hang" >/dev/null || rc=$?
step "rest rc=$rc"
assert_eq "$rc" "28" "the lane REST call gave up on its own deadline"
R
  # which browser session this run drives
  cat > "$1/recipes/906-session.sh" <<'R'
#!/bin/bash
step "session=$JOURNEY_SESSION runid=$JOURNEY_RUN_ID"
R
  # the residue reaper owns its own run's work and a DEAD run's leftovers - a
  # live sibling's artifacts are not residue
  cat > "$1/recipes/907-residue-ownership.sh" <<'R'
#!/bin/bash
ST="${RESIDUE_STATE:?drive sets RESIDUE_STATE}"
mkdir -p "$ST"
rm -f "$ST/deleted-sessions.log" "$ST/deleted-tasks.log"
sid=$(new_journey_session residue-probe)
tag=$(journey_tag)
(exit 0) & wait $!; dead=$!
live=$$
[ -f "$JOURNEY_WORKTMP/omega-jrn-live-$live" ] || : >"$JOURNEY_WORKTMP/omega-jrn-live-$live"
live_sid="jrn-a1-${live}-live"
dead_sid="jrn-a1-${dead}-dead"
ours_task="jrn-${tag}-notify"
live_task="jrn-a1-${live}-notify"
dead_task="jrn-a1-${dead}-notify"
python3 - "$ST" "$sid" "$live_sid" "$dead_sid" "$ours_task" "$live_task" "$dead_task" <<'PY'
import json, os, sys
st, sid, live_sid, dead_sid, ours_task, live_task, dead_task = sys.argv[1:8]
json.dump({"sessions": [{"id": sid}, {"id": live_sid}, {"id": dead_sid}]},
          open(os.path.join(st, "sessions.json"), "w"))
json.dump({"tasks": [{"id": "t-our", "title": ours_task},
                     {"id": "t-live", "title": live_task},
                     {"id": "t-dead", "title": dead_task}]},
          open(os.path.join(st, "tasks.json"), "w"))
PY
cleanup_journey_residue
del_s() { grep -qx "$1" "$ST/deleted-sessions.log" 2>/dev/null; }
del_t() { grep -qx "$1" "$ST/deleted-tasks.log" 2>/dev/null; }
del_s "$sid" || die "the reaper skipped this run's OWN session"
del_t "t-our" || die "the reaper skipped this run's OWN board card"
del_s "$dead_sid" || die "the reaper skipped a CRASHED run's session - nothing would ever clear it"
del_t "t-dead" || die "the reaper skipped a CRASHED run's board card"
del_s "$live_sid" && die "the reaper deleted a LIVE sibling's session ($live_sid)"
del_t "t-live" && die "the reaper deleted a LIVE sibling's board card ($live_task)"
step "residue ownership: own+dead reaped, the live sibling left standing"
R
  # the CDP port this run ended up on
  cat > "$1/recipes/908-chrome-port.sh" <<'R'
#!/bin/bash
step "chrome port=$JOURNEY_CHROME_PORT"
R
  # a die inside a command substitution stops only the substitution: the recipe
  # must not run on past it
  cat > "$1/recipes/909-death-in-subshell.sh" <<'R'
#!/bin/bash
out=$(step "inside the substitution"; die "the helper died inside a command substitution")
step "after the substitution - the recipe ran past a dead step"
R
  # a recipe that leaves the tab offline and then dies
  cat > "$1/recipes/910-offline-death.sh" <<'R'
#!/bin/bash
ab set offline on >/dev/null 2>&1
step "the tab is offline"
die "died while the tab was offline"
R
  # ... and the next recipe in the suite, which must start online again
  cat > "$1/recipes/911-after-offline.sh" <<'R'
#!/bin/bash
step "the next recipe runs"
R
  # a READ that rides a crashing transport. The count file makes the next N curl
  # calls die 139 (the host build under load) and then answer, so the shim's
  # transport retry has something to recover TO.
  cat > "$1/recipes/912-transport-crash.sh" <<'R'
#!/bin/bash
printf '%s' "${STUB_CURL_CRASH:-2}" > "$STUB_CURL_DIE_FILE"
body=$(curl -s "$JOURNEY_BASE_URL/skills")
[ -n "$body" ] || die "a read that rode a crashed transport came back empty"
step "the read answered after the crashes: ${#body} bytes"
R
  # a poll over a transport that never answers: the give-up must name the wire
  # it read, or the red is indistinguishable from a missing row
  cat > "$1/recipes/913-poll-silence.sh" <<'R'
#!/bin/bash
printf '1000' > "$STUB_CURL_DIE_FILE"
poll_http "$JOURNEY_BASE_URL/skills" "never-a-row" 2 || true
step "poll silence probe done"
R
  # ... and the other branch: the lane ANSWERS, the row just is not there
  cat > "$1/recipes/914-poll-mismatch.sh" <<'R'
#!/bin/bash
poll_http "$JOURNEY_BASE_URL/skills" "never-a-row" 2 || true
step "poll mismatch probe done"
R
}

# drive <tree> [VAR=value...] -- <recipe> [<recipe>...]: the REAL run.sh against
# the stubs, on stdout (callers redirect or pipe). The VAR=value assignments sit
# between the tree and a `--`; everything after it is a recipe name, so a
# multi-recipe run is just more names. DRIVE_PIN_SESSION=0 leaves the browser
# session name to the suite (the run-private default is what P11 measures).
DRIVE_PIN_SESSION="${DRIVE_PIN_SESSION:-1}"
drive() {
  local tree="$1" envs=() recipes=() pinargs=()
  shift
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --) shift; recipes=("$@"); break ;;
      *) envs+=("$1"); shift ;;
    esac
  done
  # a plain `if`, not `${VAR:+...}`: "0" is non-empty, so the :+ form would pin
  # the session on exactly the leg that must not have it
  if [ "$DRIVE_PIN_SESSION" = "1" ]; then
    pinargs=(JOURNEY_SESSION=clock-test)
  fi
  (
    cd "$tree" || exit 2
    env PATH="$TMP/bin:$PATH" \
      STUB_HANG="$TMP/hang" \
      REAL_SLEEP="$REAL_SLEEP" \
      JOURNEY_BASE_URL="http://127.0.0.1:$PORT" \
      JOURNEY_LOCK="$TMP/lock" \
      ${pinargs[@]+"${pinargs[@]}"} \
      JOURNEY_WORKTMP="$NATIVE_TMP" \
      ${envs[@]+"${envs[@]}"} \
      bash "$tree/run.sh" ${recipes[@]+"${recipes[@]}"}
  )
}

# evid_dir <run log>: the evidence dir the suite names in its own output.
evid_dir() {
  sed -n 's/^journeys: evidence in //p' "$1" 2>/dev/null | tail -1
}

FIXED="$TMP/fixed"
BROKEN="$TMP/broken"
LEAKY="$TMP/leaky"
NODL="$TMP/nodl"
NOWS="$TMP/nows"
SHARED="$TMP/shared"
MUTREAP="$TMP/mutreap"
PFREE="$TMP/pfree"
POLD="$TMP/pold"
NOGUARD="$TMP/noguard"
NOONLINE="$TMP/noonline"
NOVERDICT="$TMP/noverdict"
mkdir -p "$FIXED" "$BROKEN" "$LEAKY" "$NODL" "$NOWS" "$SHARED" "$MUTREAP" "$PFREE" "$POLD" \
  "$NOGUARD" "$NOONLINE" "$NOVERDICT"
make_tree "$FIXED"
make_tree "$BROKEN"
make_tree "$LEAKY"
make_tree "$NODL"
make_tree "$NOWS"
make_tree "$SHARED"
make_tree "$MUTREAP"
make_tree "$PFREE"
make_tree "$POLD"
make_tree "$NOGUARD"
make_tree "$NOONLINE"
make_tree "$NOVERDICT"

# a second listener that answers the CDP version probe on its own port - the
# "browser another run already launched" the port picker must not adopt
CPORT="$(python3 -c '
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()')"
python3 "$NATIVE_TMP/lane.py" "$CPORT" "$LANESTATE" &
CHROME_STATE_PID=$!
# the mutation: the pre-fix shape - a call with no deadline of its own
python3 - "$NATIVE_TMP/broken/lib.sh" <<'PY'
import sys

path = sys.argv[1]
src = open(path, encoding="utf-8").read()
old = '  timeout "$JOURNEY_AB_CALL_TIMEOUT" "$AB" --session "$JOURNEY_SESSION_NAME" "$@"\n  rc=$?\n  [ "$rc" = "124" ] || return "$rc"\n'
new = '  "$AB" --session "$JOURNEY_SESSION_NAME" "$@"\n  return $?\n'
assert old in src, "the deadline call site is not shaped as the mutation expects"
open(path, "w", encoding="utf-8", newline="").write(src.replace(old, new, 1))
PY
# the mutation: the pre-fix watchdog - a bare `sleep` in the subshell, and the
# subshell inheriting the caller's fds
python3 - "$NATIVE_TMP/leaky/run.sh" <<'PY'
import sys

path = sys.argv[1]
src = open(path, encoding="utf-8").read()
head_old = '''  (
    wd_sleep=""
    trap '[ -n "$wd_sleep" ] && kill "$wd_sleep" 2>/dev/null; exit 0' TERM
    sleep "$JOURNEY_TIMEOUT" &
    wd_sleep=$!
    wait "$wd_sleep" 2>/dev/null || exit 0
    {
'''
head_new = '  (sleep "$JOURNEY_TIMEOUT" && {\n'
tail_old = '    } >/dev/null 2>&1\n  ) 9>&- &\n  watchdog=$!\n'
tail_new = '  }) 9>&- &\n  watchdog=$!\n'
assert head_old in src, "the watchdog head is not shaped as the mutation expects"
assert tail_old in src, "the watchdog tail is not shaped as the mutation expects"
src = src.replace(head_old, head_new, 1).replace(tail_old, tail_new, 1)
open(path, "w", encoding="utf-8", newline="").write(src)
PY
# the mutation: the pre-fix REST call - curl with no answer deadline
python3 - "$NATIVE_TMP/nodl/lib.sh" <<'PY'
import sys

path = sys.argv[1]
src = open(path, encoding="utf-8").read()
old = ' --max-time "$JOURNEY_HTTP_TIMEOUT" "$@"'
assert src.count(old) == 2, "the REST shim is not shaped as the mutation expects"
open(path, "w", encoding="utf-8", newline="").write(src.replace(old, ' "$@"'))
PY
# the mutation: the clock reading the load average ALONE (no wall-stretch signal)
python3 - "$NATIVE_TMP/nows/run.sh" <<'PY'
import sys

path = sys.argv[1]
src = open(path, encoding="utf-8").read()
old = '  if wall_sample "${1:-1}" && [ "$WALL_PCT" -gt "$cand" ]; then cand="$WALL_PCT"; fi\n'
assert old in src, "the wall signal is not shaped as the mutation expects"
open(path, "w", encoding="utf-8", newline="").write(src.replace(old, "", 1))
PY
# the mutation: one browser session name for the whole host
python3 - "$NATIVE_TMP/shared/run.sh" <<'PY'
import sys

path = sys.argv[1]
src = open(path, encoding="utf-8").read()
old = 'JOURNEY_SESSION="${JOURNEY_SESSION:-omega-journey-$JOURNEY_RUN_ID}"'
assert old in src, "the session default is not shaped as the mutation expects"
open(path, "w", encoding="utf-8", newline="").write(
    src.replace(old, 'JOURNEY_SESSION="${JOURNEY_SESSION:-omega-journey}"', 1))
PY
# the mutation: the pre-fix reaper - every jrn- artifact on the lane, live
# sibling or not
python3 - "$NATIVE_TMP/mutreap/lib.sh" <<'PY'
import sys

path = sys.argv[1]
src = open(path, encoding="utf-8").read()
old = '  journey_run_live "$own" && return 1\n'
assert old in src, "the ownership test is not shaped as the mutation expects"
open(path, "w", encoding="utf-8", newline="").write(src.replace(old, "  :\n", 1))
PY
# the mutation: the default CDP port is the test's own held port, so the
# adoption rule is the only thing between the run and a browser it did not
# launch (the picker's base is the same variable, by construction)
python3 - "$NATIVE_TMP/pfree/run.sh" "$NATIVE_TMP/pold/run.sh" "$CPORT" <<'PY'
import sys

port = sys.argv[3]
old = 'JOURNEY_CHROME_PORT="${JOURNEY_CHROME_PORT:-9333}"'
new = 'JOURNEY_CHROME_PORT="${JOURNEY_CHROME_PORT:-%s}"' % port
for path in sys.argv[1:3]:
    src = open(path, encoding="utf-8").read()
    assert old in src, "the port default is not shaped as the mutation expects"
    open(path, "w", encoding="utf-8", newline="").write(src.replace(old, new, 1))
PY
# the mutation: the pre-fix adopt - whatever answers on the port is ours to drive
python3 - "$NATIVE_TMP/pold/run.sh" <<'PY'
import sys

path = sys.argv[1]
src = open(path, encoding="utf-8").read()
old = 'if [ "$JOURNEY_CHROME_PORT_PINNED" = "0" ] && chrome_listening; then'
assert old in src, "the adopt guard is not shaped as the mutation expects"
open(path, "w", encoding="utf-8", newline="").write(src.replace(old, "if false; then", 1))
PY
# the mutation: no death record - a die inside $( ) kills only the substitution
# and the recipe runs on
python3 - "$NATIVE_TMP/noguard/lib.sh" <<'PY'
import sys

path = sys.argv[1]
src = open(path, encoding="utf-8").read()
old = 'journey_dead_check() {\n'
assert old in src, "the death guard is not shaped as the mutation expects"
open(path, "w", encoding="utf-8", newline="").write(
    src.replace(old, 'journey_dead_check() {\n  return 0\n', 1))
PY
# the mutation: the pre-fix clean room - nothing puts the tab back online
python3 - "$NATIVE_TMP/noonline/lib.sh" <<'PY'
import sys

path = sys.argv[1]
src = open(path, encoding="utf-8").read()
old = 'journey_online() {\n'
assert old in src, "the online restore is not shaped as the mutation expects"
open(path, "w", encoding="utf-8", newline="").write(
    src.replace(old, 'journey_online() {\n  return 0\n', 1))
PY
# the mutation: a poll gives up silently - the pre-fix shape, where a transport
# that never answered and a row that never arrived red with the same words
python3 - "$NATIVE_TMP/noverdict/lib.sh" <<'PY'
import sys

path = sys.argv[1]
src = open(path, encoding="utf-8").read()
old = '  printf \'%s [journey] %s never showed %s in %s reads'
assert old in src, "the poll verdict line is not shaped as the mutation expects"
open(path, "w", encoding="utf-8", newline="").write(
    src.replace(old, '  : \'%s [journey] %s never showed %s in %s reads', 1))
PY

# --- P0: the fixture is the CLI under test, not a real one -------------------
drive "$FIXED" JOURNEY_TIMEOUT=30 -- 900-call > "$TMP/p0.log" 2>&1
if grep -q 'call answered' "$(evid_dir "$TMP/p0.log")/900-call.log" 2>/dev/null; then
  ok "P0 the suite drove the stub CLI"
else
  bad "P0 the fixture never ran: $(tail -3 "$TMP/p0.log")"
fi

# --- P1: a wedged call is bounded and RECOVERED -------------------------------
: > "$TMP/hang"
if drive "$FIXED" JOURNEY_AB_CALL_TIMEOUT=3 JOURNEY_TIMEOUT=60 -- 900-call > "$TMP/p1.log" 2>&1; then
  ok "P1 the wedged call did not park the recipe (suite green)"
else
  bad "P1 the recipe failed on a recoverable wedge: $(grep -E 'journey RED|wall cap' "$TMP/p1.log" | head -2)"
fi
grep -q 'with no answer (eval) - dropping the session, retrying' \
  "$(evid_dir "$TMP/p1.log")/900-call.log" 2>/dev/null \
  && ok "P1 the recovery is named in the rule's log (deadline, drop, retry)" \
  || bad "P1 the deadline never fired - the call was not bounded"

# --- P2: the same wedge on the pre-fix shape must NOT recover -----------------
: > "$TMP/hang"
if drive "$BROKEN" JOURNEY_AB_CALL_TIMEOUT=3 JOURNEY_TIMEOUT=15 -- 900-call > "$TMP/p2.log" 2>&1; then
  bad "P2 the unbounded tree stayed green - the pin proves nothing"
else
  ok "P2 without the deadline the recipe is killed by the wall cap (the silent hang)"
fi
grep -q 'exceeded 15s wall cap' "$(evid_dir "$TMP/p2.log")/900-call.log" 2>/dev/null \
  && ok "P2 the RED names the wall cap" \
  || bad "P2 the RED did not name the wall cap: $(tail -3 "$TMP/p2.log")"
rm -f "$TMP/hang"

# --- P3: one scale moves the cap, the wait floor and the call deadline --------
if drive "$FIXED" JOURNEY_CLOCK_PCT=300 -- 901-budgets > "$TMP/p3.log" 2>&1; then
  ok "P3 the suite ran on a pinned 300% clock"
else
  bad "P3 the budget probe failed: $(tail -3 "$TMP/p3.log")"
fi
grep -q 'timing budget 300%' "$TMP/p3.log" \
  && ok "P3 the suite states the budget it ran on" \
  || bad "P3 the suite never printed its timing budget"
P3LOG="$(evid_dir "$TMP/p3.log")/901-budgets.log"
grep -q 'budgets cap=900 rtt=90 deadline=360 pct=300' "$P3LOG" 2>/dev/null \
  && ok "P3 cap 900 / floor 90 / deadline 360 all follow the one scale" \
  || bad "P3 the budgets did not scale together: $(grep budgets "$P3LOG" 2>/dev/null)"

# --- P4: an idle host keeps the old numbers ----------------------------------
if drive "$FIXED" JOURNEY_CLOCK_PCT=100 -- 901-budgets > "$TMP/p4.log" 2>&1; then
  P4LOG="$(evid_dir "$TMP/p4.log")/901-budgets.log"
  grep -q 'budgets cap=300 rtt=30 deadline=120 pct=100' "$P4LOG" 2>/dev/null \
    && ok "P4 at 100% the cap stays 300s, the floor 30s, the deadline 120s" \
    || bad "P4 the idle budgets moved: $(grep budgets "$P4LOG" 2>/dev/null)"
else
  bad "P4 the idle budget probe failed: $(tail -3 "$TMP/p4.log")"
fi

# --- P5: wait_js waits the FLOOR, not the number its caller asked for --------
if drive "$FIXED" JOURNEY_RTT_WAIT=2 JOURNEY_TIMEOUT=60 -- 902-floor-applies > "$TMP/p5.log" 2>&1; then
  bad "P5 wait_js returned on a false predicate - the probe is broken"
else
  grep -q 'wait_js timed out after 2s' "$TMP/p5.log" \
    && ok "P5 wait_js took its budget from the floor (2s), not from the caller's 0" \
    || bad "P5 the timeout did not name the floored budget: $(grep 'timed out' "$TMP/p5.log" | head -2)"
fi

# --- P6: the tag record belongs to the RUN -----------------------------------
drive "$FIXED" JOURNEY_TIMEOUT=60 -- 903-run-tag > "$TMP/p6a.log" 2>&1
drive "$FIXED" JOURNEY_TIMEOUT=60 -- 903-run-tag > "$TMP/p6b.log" 2>&1
TAGA="$(sed -n 's/.*tag record=\([^ ]*\) .*/\1/p' \
  "$(evid_dir "$TMP/p6a.log")/903-run-tag.log" 2>/dev/null)"
TAGB="$(sed -n 's/.*tag record=\([^ ]*\) .*/\1/p' \
  "$(evid_dir "$TMP/p6b.log")/903-run-tag.log" 2>/dev/null)"
if [ -n "$TAGA" ] && [ -n "$TAGB" ] && [ "$TAGA" != "$TAGB" ]; then
  ok "P6 two runs keep separate tag records ($(basename "$TAGA") vs $(basename "$TAGB"))"
else
  bad "P6 the tag record is shared across runs: [$TAGA] vs [$TAGB]"
fi
case "$TAGA" in
  *omega-jrn-runtag-*) ok "P6 the record path carries the run id" ;;
  *) bad "P6 the record path lost the run id: $TAGA" ;;
esac
# the tag INSIDE the session id carries the run too: a bare timestamp named two
# concurrent runs' sessions identically inside the same second.
TAGV="$(sed -n 's/.*tag=\([^ ]*\) .*/\1/p' "$(evid_dir "$TMP/p6a.log")/903-run-tag.log" 2>/dev/null)"
RUNV="$(sed -n 's/.*runid=\([^ ]*\) .*/\1/p' "$(evid_dir "$TMP/p6a.log")/903-run-tag.log" 2>/dev/null)"
case "$TAGV" in
  *-"$RUNV") ok "P6 the session tag ends with the run id ($TAGV)" ;;
  *) bad "P6 the session tag carries no run id: tag=[$TAGV] runid=[$RUNV]" ;;
esac
ls "$TMP"/omega-jrn-runtag-* >/dev/null 2>&1 \
  && bad "P6 a tag record survived the run (the exit path stopped cleaning it)" \
  || ok "P6 the exit path removes the run's tag record"

# --- P7: the cap does not outlive the suite in a caller's pipe ---------------
# A recipe that greens in a second, wall cap pinned at 30s. The pipe must close
# with the suite; the pre-fix watchdog leaves its `sleep 30` holding the write
# end, so cat below waits out the whole cap (measured live: 4 orphaned
# `sleep 300` held the pre-flight's pipe 10 minutes past a finished suite).
pipe_seconds() { # <tree> <log> -> seconds from start to EOF on the pipe
  local tree="$1" out="$2" t0 t1
  t0="$(date +%s)"
  drive "$tree" JOURNEY_TIMEOUT=30 -- 900-call 2>&1 | cat > "$out"
  t1="$(date +%s)"
  printf '%s' "$((t1 - t0))"
}
FIXED_SECS="$(pipe_seconds "$FIXED" "$TMP/p7a.log")"
LEAKY_SECS="$(pipe_seconds "$LEAKY" "$TMP/p7b.log")"
grep -q 'suite done' "$TMP/p7a.log" || bad "P7 the fixed suite never finished: $(tail -3 "$TMP/p7a.log")"
# The verdict is the DELTA, not an absolute second count: a loaded host slows
# both legs together (the suite's own scans took 9s of a 14s fixed leg at load
# 36), while the leak adds the whole cap to the pre-fix one.
if [ "$((LEAKY_SECS - FIXED_SECS))" -ge 15 ] && [ "$FIXED_SECS" -lt 30 ]; then
  ok "P7 a piped caller gets EOF with the suite, the pre-fix watchdog holds it to the cap (${FIXED_SECS}s vs ${LEAKY_SECS}s of a 30s cap)"
else
  bad "P7 the pipe did not close with the suite: fixed ${FIXED_SECS}s vs pre-fix ${LEAKY_SECS}s of a 30s cap"
fi

# --- P8: a load that ARRIVES mid-suite ratchets the scale up -----------------
# The wall-stretch probe is OFF here on purpose: this leg grades the load-average
# arithmetic and the ratchet alone, so it reads the same on an idle CI runner and
# on a contended host. P10 is the leg that grades the probe.
printf '1.20 1.20 1.20 1/1\n' > "$TMP/load"
if drive "$FIXED" JOURNEY_WALL_PROBE_MS=0 JOURNEY_LOADAVG="$TMP/load" -- 904-raise-load 901-budgets > "$TMP/p8.log" 2>&1; then
  ok "P8 the ratchet probe ran"
else
  bad "P8 the ratchet probe failed: $(tail -3 "$TMP/p8.log")"
fi
grep -q 'timing budget 100%' "$TMP/p8.log" \
  && ok "P8 the suite started on the idle scale" \
  || bad "P8 the suite did not start at 100%: $(grep 'timing budget' "$TMP/p8.log")"
grep -q 'timing budget 400% (host load 100000.0' "$TMP/p8.log" \
  && ok "P8 the load that arrived mid-suite moved the scale" \
  || bad "P8 the clock never re-sampled: $(grep 'timing budget' "$TMP/p8.log")"
P8LOG="$(evid_dir "$TMP/p8.log")/901-budgets.log"
grep -q 'budgets cap=1200 rtt=120 deadline=480 pct=400' "$P8LOG" 2>/dev/null \
  && ok "P8 the later recipe ran on the raised scale (cap 1200 / floor 120)" \
  || bad "P8 the later recipe did not inherit the ratchet: $(grep budgets "$P8LOG" 2>/dev/null)"

# --- P9: a lane that accepts and never answers is bounded, and NAMED ---------
# curl bounds the CONNECT and nothing else; 164 recipe calls rode it that way, so
# a lane that accepts and never answers parked the recipe to the wall cap - the
# silent-hang shape. The stub lane's /hang sleeps 120s on the request thread.
if drive "$FIXED" JOURNEY_HTTP_TIMEOUT=1 JOURNEY_TIMEOUT=60 JOURNEY_WALL_PROBE_MS=0 \
     -- 905-rest-deadline > "$TMP/p9.log" 2>&1; then
  ok "P9 a lane that never answers does not park the recipe"
else
  bad "P9 the REST call parked the recipe: $(tail -3 "$TMP/p9.log")"
fi
P9LOG="$(evid_dir "$TMP/p9.log")/905-rest-deadline.log"
grep -q 'lane answered nothing in 1s, retrying once: http://127.0.0.1' "$P9LOG" 2>/dev/null \
  && ok "P9 the deadline names the URL it gave up on and retries a read once" \
  || bad "P9 the REST deadline never fired: $(grep -iE 'rest rc|lane ' "$P9LOG" 2>/dev/null | head -2)"
grep -q 'rest rc=28' "$P9LOG" 2>/dev/null \
  && ok "P9 the call returned curl's timeout status, not an empty success" \
  || bad "P9 the call did not report its timeout: $(tail -3 "$P9LOG" 2>/dev/null)"
if drive "$NODL" JOURNEY_HTTP_TIMEOUT=1 JOURNEY_TIMEOUT=15 JOURNEY_WALL_PROBE_MS=0 \
     -- 905-rest-deadline > "$TMP/p9b.log" 2>&1; then
  bad "P9 the tree without the REST deadline stayed green - the pin proves nothing"
else
  ok "P9 without the deadline the recipe is killed by the wall cap (the silent hang)"
fi
grep -q 'exceeded 15s wall cap' "$(evid_dir "$TMP/p9b.log")/905-rest-deadline.log" 2>/dev/null \
  && ok "P9 the pre-fix RED names the wall cap" \
  || bad "P9 the pre-fix RED named nothing: $(tail -3 "$TMP/p9b.log")"

# --- P10: the wall stretch, not the queue length, sets the scale -------------
# The stub sleep stretches ONLY the probe's duration by 3x, so a host that
# reports an idle load (1.20 on 12 cpus = 100%) must still be sized for 300%.
printf '1.20 1.20 1.20 1/1\n' > "$TMP/load-idle"
if drive "$FIXED" JOURNEY_LOADAVG="$TMP/load-idle" PROBE_STRETCHED=600 JOURNEY_TIMEOUT=600 \
     -- 901-budgets > "$TMP/p10.log" 2>&1; then
  ok "P10 the wall-stretch probe ran"
else
  bad "P10 the probe run failed: $(tail -3 "$TMP/p10.log")"
fi
P10PCT="$(sed -n 's/^journeys: timing budget \([0-9]*\)%.*/\1/p' "$TMP/p10.log" | head -1)"
if [ -n "$P10PCT" ] && [ "$P10PCT" -ge 300 ]; then
  ok "P10 a stretched second moves the scale (load says 100%, the clock says ${P10PCT}%)"
else
  bad "P10 the wall signal never moved the scale: $(grep 'timing budget' "$TMP/p10.log" | head -2)"
fi
grep -q "timing budget $P10PCT% (.*wall stretch $P10PCT%" "$TMP/p10.log" \
  && ok "P10 the line names the signal that set the budget" \
  || bad "P10 the budget line does not name the wall signal: $(grep 'timing budget' "$TMP/p10.log" | head -1)"
if drive "$NOWS" JOURNEY_LOADAVG="$TMP/load-idle" PROBE_STRETCHED=600 JOURNEY_TIMEOUT=600 \
     -- 901-budgets > "$TMP/p10b.log" 2>&1 \
   && grep -q 'timing budget 100%' "$TMP/p10b.log"; then
  ok "P10 the load average alone reads 100% on the same host (the pre-fix shape)"
else
  bad "P10 the pre-fix clock did not stay at 100%: $(grep 'timing budget' "$TMP/p10b.log" | head -2)"
fi

# --- P11: the browser session belongs to the RUN -----------------------------
# One name for the whole host made every run on the machine a sibling in the same
# daemon: a worker's targeted run, a crashed suite's leftover daemon and the gate
# shared one page, one console buffer and one localStorage.
DRIVE_PIN_SESSION=0
drive "$FIXED" JOURNEY_TIMEOUT=60 JOURNEY_WALL_PROBE_MS=0 \
  -- 906-session > "$TMP/p11a.log" 2>&1
drive "$FIXED" JOURNEY_TIMEOUT=60 JOURNEY_WALL_PROBE_MS=0 \
  -- 906-session > "$TMP/p11b.log" 2>&1
P11LOGA="$(evid_dir "$TMP/p11a.log")/906-session.log"
P11LOGB="$(evid_dir "$TMP/p11b.log")/906-session.log"
SESA="$(sed -n 's/.*session=\([^ ]*\) .*/\1/p' "$P11LOGA" 2>/dev/null)"
SESB="$(sed -n 's/.*session=\([^ ]*\) .*/\1/p' "$P11LOGB" 2>/dev/null)"
RUNA="$(sed -n 's/.*runid=\([^ ]*\)$/\1/p' "$P11LOGA" 2>/dev/null)"
if [ -n "$SESA" ] && [ -n "$SESB" ] && [ "$SESA" != "$SESB" ]; then
  ok "P11 two runs on one host drive different browser sessions ($SESA vs $SESB)"
else
  bad "P11 the browser session is shared across runs: [$SESA] vs [$SESB]"
fi
case "$SESA" in
  *-"$RUNA") ok "P11 the session name carries the run id ($SESA)" ;;
  *) bad "P11 the session name lost the run id: session=[$SESA] runid=[$RUNA]" ;;
esac
if drive "$SHARED" JOURNEY_TIMEOUT=60 JOURNEY_WALL_PROBE_MS=0 \
     -- 906-session > "$TMP/p11c.log" 2>&1; then
  SHAA="$(sed -n 's/.*session=\([^ ]*\) .*/\1/p' "$(evid_dir "$TMP/p11c.log")/906-session.log" 2>/dev/null)"
  [ "$SHAA" = "omega-journey" ] \
    && ok "P11 the pre-fix default names every run the same ($SHAA)" \
    || bad "P11 the pre-fix tree did not reproduce the shared name: [$SHAA]"
else
  bad "P11 the pre-fix run failed: $(tail -3 "$TMP/p11c.log")"
fi

# --- P12: a live sibling run is not residue ----------------------------------
# Every recipe's exit path runs the residue reaper. It used to delete EVERY jrn-
# artifact on the lane, so a suite erased a concurrent sibling's session, board
# card and roster home mid-journey and the sibling red on state it had just
# planted. The stub lane keeps a session list and a task list, and logs the
# deletes, so the three outcomes are observable: own reaped, crashed reaped,
# live sibling NOT.
rm -f "$LANESTATE"/*.json "$LANESTATE"/*.log 2>/dev/null
if drive "$FIXED" JOURNEY_TIMEOUT=120 JOURNEY_WALL_PROBE_MS=0 RESIDUE_STATE="$LANESTATE" \
     -- 907-residue-ownership > "$TMP/p12.log" 2>&1; then
  ok "P12 the reaper owns this run's and a crashed run's artifacts, and leaves a live sibling's"
else
  bad "P12 the ownership probe failed: $(grep -E 'journey RED' "$TMP/p12.log" | head -2)"
fi
grep -q 'residue ownership: own+dead reaped' "$(evid_dir "$TMP/p12.log")/907-residue-ownership.log" 2>/dev/null \
  && ok "P12 the probe ran its three-way assertion" \
  || bad "P12 the probe never reached its verdict: $(tail -3 "$TMP/p12.log")"
rm -f "$LANESTATE"/*.json "$LANESTATE"/*.log 2>/dev/null
if drive "$MUTREAP" JOURNEY_TIMEOUT=120 JOURNEY_WALL_PROBE_MS=0 RESIDUE_STATE="$LANESTATE" \
     -- 907-residue-ownership > "$TMP/p12b.log" 2>&1; then
  bad "P12 the pre-fix reaper stayed green - the pin proves nothing"
else
  grep -q "the reaper deleted a LIVE sibling's session" "$TMP/p12b.log" \
    && ok "P12 without the ownership test the reaper deletes a live sibling's session (the pre-fix shape)" \
    || bad "P12 the pre-fix shape red'd for another reason: $(tail -3 "$TMP/p12b.log")"
fi

# --- P13: the browser belongs to the run, not to the port --------------------
# A second listener answering the CDP probe stands in for a browser another run
# launched. A run that did not launch it must not drive it (two suites in one
# renderer, and the owner's exit kills it mid-recipe for the sibling).
if curl -s -m 2 "http://127.0.0.1:$CPORT/json/version" | grep -q Chrome; then
  ok "P13 the held chrome-shaped listener answers the CDP probe"
else
  bad "P13 the held listener never came up on $CPORT"
fi
PICK="$(bash -c ". '$FIXED/lib.sh' >/dev/null 2>&1; journey_pick_free_port '$CPORT'")"
if [ -n "$PICK" ] && [ "$PICK" != "$CPORT" ]; then
  ok "P13 the picker steps off a held port ($CPORT -> $PICK)"
else
  bad "P13 the picker kept a held port: [$PICK] (held $CPORT)"
fi
PICKFREE="$(bash -c ". '$FIXED/lib.sh' >/dev/null 2>&1; journey_pick_free_port 9340")"
if [ -n "$PICKFREE" ] && ! curl -s -m 1 -o /dev/null "http://127.0.0.1:$PICKFREE" 2>/dev/null; then
  ok "P13 a free port is taken as-is ($PICKFREE)"
else
  bad "P13 the picker did not land on a free port: [$PICKFREE]"
fi
if drive "$PFREE" JOURNEY_TIMEOUT=120 JOURNEY_WALL_PROBE_MS=0 -- 908-chrome-port > "$TMP/p13.log" 2>&1; then
  P13PORT="$(sed -n 's/.*chrome port=\([0-9]*\).*/\1/p' \
    "$(evid_dir "$TMP/p13.log")/908-chrome-port.log" 2>/dev/null | head -1)"
  if [ -n "$P13PORT" ] && [ "$P13PORT" != "$CPORT" ]; then
    ok "P13 the run took a port of its own instead of the browser already there ($CPORT -> $P13PORT)"
  else
    bad "P13 the run drove the browser it did not launch: port=[$P13PORT] held=$CPORT"
  fi
else
  bad "P13 the port probe run failed: $(tail -3 "$TMP/p13.log")"
fi
if drive "$POLD" JOURNEY_TIMEOUT=120 JOURNEY_WALL_PROBE_MS=0 -- 908-chrome-port > "$TMP/p13b.log" 2>&1; then
  P13OLD="$(sed -n 's/.*chrome port=\([0-9]*\).*/\1/p' \
    "$(evid_dir "$TMP/p13b.log")/908-chrome-port.log" 2>/dev/null | head -1)"
  if [ "$P13OLD" = "$CPORT" ]; then
    ok "P13 the pre-fix shape adopts the browser already on the port ($P13OLD)"
  else
    bad "P13 the pre-fix shape did not reproduce the adopt: [$P13OLD] vs held $CPORT"
  fi
else
  bad "P13 the pre-fix port run failed: $(tail -3 "$TMP/p13b.log")"
fi

# --- P14: a die inside a command substitution is still the recipe's red ------
# `SID=$(new_journey_session x | tail -1)` runs the helper in a subshell, so a
# die() there stops only the substitution and the recipe runs on with an empty
# id - every later step then reds on the empty value, one screen away from the
# real fault. The death record makes the killing red the recipe's own.
rm -f "$LANESTATE"/*.log 2>/dev/null
if drive "$FIXED" JOURNEY_TIMEOUT=120 JOURNEY_WALL_PROBE_MS=0 -- 909-death-in-subshell \
     > "$TMP/p14.log" 2>&1; then
  bad "P14 the recipe ran on past a die inside a command substitution"
else
  ok "P14 a dead step inside a subshell reds the recipe"
fi
P14LOG="$(evid_dir "$TMP/p14.log")/909-death-in-subshell.log"
grep -q "the helper died inside a command substitution" "$P14LOG" 2>/dev/null \
  && ok "P14 the red names the step that died, not its downstream symptom" \
  || bad "P14 the red lost the killing message: $(tail -3 "$P14LOG" 2>/dev/null)"
grep -q "after the substitution" "$P14LOG" 2>/dev/null \
  && bad "P14 the recipe stepped into the cascade anyway" \
  || ok "P14 the recipe stopped before the cascade"
if drive "$NOGUARD" JOURNEY_TIMEOUT=120 JOURNEY_WALL_PROBE_MS=0 -- 909-death-in-subshell \
     > "$TMP/p14b.log" 2>&1; then
  grep -q "after the substitution" "$(evid_dir "$TMP/p14b.log")/909-death-in-subshell.log" 2>/dev/null \
    && ok "P14 without the record the recipe runs on with an empty value (the pre-fix shape)" \
    || bad "P14 the pre-fix shape did not reach the cascade"
else
  bad "P14 the pre-fix tree red'd for another reason: $(tail -3 "$TMP/p14b.log")"
fi

# --- P15: a recipe that dies while the tab is offline leaves the suite online -
# 111 puts the tab offline to cut the socket. A red before its own restore left
# every later recipe on a dead page: "cannot open", or a click that answers from
# the stale DOM while its write can never leave it (the no-op toggle).
ABLOG="$TMP/ab.log"
rm -f "$ABLOG"
drive "$FIXED" JOURNEY_TIMEOUT=120 JOURNEY_WALL_PROBE_MS=0 STUB_AB_LOG="$ABLOG" \
  -- 910-offline-death 911-after-offline > "$TMP/p15.log" 2>&1
if grep -q "^set offline on" "$ABLOG" 2>/dev/null && grep -q "^set offline off" "$ABLOG" 2>/dev/null; then
  ok "P15 the suite puts the tab back online after a recipe died offline"
else
  bad "P15 nothing restored the tab: $(cat "$ABLOG" 2>/dev/null | head -5)"
fi
grep -q "the next recipe runs" "$(evid_dir "$TMP/p15.log")/911-after-offline.log" 2>/dev/null \
  && ok "P15 the sibling recipe after it still runs" \
  || bad "P15 the sibling recipe never ran: $(tail -3 "$TMP/p15.log")"
ABLOG2="$TMP/ab2.log"
rm -f "$ABLOG2"
drive "$NOONLINE" JOURNEY_TIMEOUT=120 JOURNEY_WALL_PROBE_MS=0 STUB_AB_LOG="$ABLOG2" \
  -- 910-offline-death 911-after-offline > "$TMP/p15b.log" 2>&1
grep -q "^set offline off" "$ABLOG2" 2>/dev/null \
  && bad "P15 the pre-fix tree restored the network - the pin proves nothing" \
  || ok "P15 without the clean room the tab stays offline for every later recipe (the pre-fix shape)"

# --- P16: a read that rides a crashing transport still answers -----------------
# The host's curl build segfaults under suite load (rc 139, in the corpus). A read
# that dies mid-flight answers NOTHING, which is the same empty body a poll reads
# as a missing row - so the shim retries the transport class, and ONE attempt is
# the pre-fix shape.
if drive "$FIXED" JOURNEY_TIMEOUT=60 JOURNEY_WALL_PROBE_MS=0 STUB_CURL_CRASH=2 \
     STUB_CURL_DIE_FILE="$TMP/crash-fixed" -- 912-transport-crash > "$TMP/p16.log" 2>&1; then
  ok "P16 a read survives two crashed transports"
  grep -q "answered nothing rc=139" "$(evid_dir "$TMP/p16.log")/912-transport-crash.log" 2>/dev/null \
    && ok "P16 the retry names the crash it survived (rc=139)" \
    || bad "P16 the retry never named the transport fault: $(tail -3 "$(evid_dir "$TMP/p16.log")/912-transport-crash.log" 2>/dev/null)"
else
  bad "P16 the read gave up on the first crash: $(tail -3 "$TMP/p16.log")"
fi
if drive "$FIXED" JOURNEY_TIMEOUT=60 JOURNEY_WALL_PROBE_MS=0 JOURNEY_HTTP_READ_TRIES=1 \
     STUB_CURL_CRASH=2 STUB_CURL_DIE_FILE="$TMP/crash-prefix" -- 912-transport-crash > "$TMP/p16b.log" 2>&1; then
  bad "P16 one attempt survived the crash - the pin proves nothing"
else
  ok "P16 on one attempt the same crash leaves the read empty (the pre-fix shape)"
fi

# --- P17: a poll that gave up names the WIRE it read ---------------------------
# Two reds that used to read identically and mean opposite things: a transport
# that never answered (the host) and an answer that never carried the row (the
# product). The give-up line separates them by counting both.
if drive "$FIXED" JOURNEY_TIMEOUT=120 JOURNEY_WALL_PROBE_MS=0 \
     JOURNEY_CLOCK_PCT=100 JOURNEY_RTT_WAIT=1 STUB_CURL_DIE_FILE="$TMP/silence-fixed" -- 913-poll-silence > "$TMP/p17.log" 2>&1; then
  ok "P17 the poll over a silent transport returned instead of parking"
else
  bad "P17 the poll leg never ran: $(tail -3 "$TMP/p17.log")"
fi
P17LOG="$(evid_dir "$TMP/p17.log")/913-poll-silence.log"
grep -q "never showed never-a-row" "$P17LOG" 2>/dev/null \
  && grep -Eq "\([0-9]+ answered and never matched, [1-9][0-9]* never answered\)" "$P17LOG" \
  && ok "P17 a silent transport is counted as unanswered reads, not as a missing row" \
  || bad "P17 the give-up line did not count the silence: $(tail -3 "$P17LOG" 2>/dev/null)"
drive "$FIXED" JOURNEY_TIMEOUT=120 JOURNEY_WALL_PROBE_MS=0 \
  JOURNEY_CLOCK_PCT=100 JOURNEY_RTT_WAIT=1 STUB_CURL_DIE_FILE="$TMP/mismatch-fixed" -- 914-poll-mismatch > "$TMP/p17b.log" 2>&1
P17BLOG="$(evid_dir "$TMP/p17b.log")/914-poll-mismatch.log"
grep -Eq "\([1-9][0-9]* answered and never matched, 0 never answered\)" "$P17BLOG" 2>/dev/null \
  && ok "P17 a lane that answers without the row is counted as an answer, not a transport fault" \
  || bad "P17 the mismatch branch was not separated: $(tail -3 "$P17BLOG" 2>/dev/null)"
drive "$NOVERDICT" JOURNEY_TIMEOUT=120 JOURNEY_WALL_PROBE_MS=0 \
  JOURNEY_CLOCK_PCT=100 JOURNEY_RTT_WAIT=1 STUB_CURL_DIE_FILE="$TMP/silence-prefix" -- 913-poll-silence > "$TMP/p17c.log" 2>&1
P17CLOG="$(evid_dir "$TMP/p17c.log")/913-poll-silence.log"
grep -q "never showed never-a-row" "$P17CLOG" 2>/dev/null \
  && bad "P17 the pre-fix poll carried the verdict - the pin proves nothing" \
  || ok "P17 without the verdict line a give-up says nothing about the wire (the pre-fix shape)"

printf '\nclock_test: %s passed, %s failed\n' "$pass" "$fail"
[ "$fail" = 0 ] || exit 1
exit 0
