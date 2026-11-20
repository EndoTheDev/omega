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
case "${args[0]:-}" in
  console) : ;;
  eval) printf '%s\n' "${STUB_EVAL_ANSWER:-false}" ;;
  *) : ;;
esac
exit 0
STUB
chmod +x "$TMP/bin/agent-browser"

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
import sys


class Handler(http.server.BaseHTTPRequestHandler):
    def _send(self, body=b"{}"):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path.startswith("/health"):
            self._send(b'{"status":"ok"}')
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
        self._send(b"{}")

    def log_message(self, *args):
        pass


http.server.ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()
PY
python3 "$NATIVE_TMP/lane.py" "$PORT" &
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
step "tag record=$JRN_TAGFILE tag=$tag session=$sid"
R
  # the host gets heavier between two recipes: the clock must follow it
  cat > "$1/recipes/904-raise-load.sh" <<'R'
#!/bin/bash
printf '100000.0 100000.0 100000.0 1/1\n' > "$JOURNEY_LOADAVG"
step "host load raised on the next sample"
R
}

# drive <tree> [VAR=value...] -- <recipe> [<recipe>...]: the REAL run.sh against
# the stubs, on stdout (callers redirect or pipe). The VAR=value assignments sit
# between the tree and a `--`; everything after it is a recipe name, so a
# multi-recipe run is just more names.
drive() {
  local tree="$1" envs=() recipes=()
  shift
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --) shift; recipes=("$@"); break ;;
      *) envs+=("$1"); shift ;;
    esac
  done
  (
    cd "$tree" || exit 2
    env PATH="$TMP/bin:$PATH" \
      STUB_HANG="$TMP/hang" \
      JOURNEY_BASE_URL="http://127.0.0.1:$PORT" \
      JOURNEY_LOCK="$TMP/lock" \
      JOURNEY_SESSION=clock-test \
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
mkdir -p "$FIXED" "$BROKEN" "$LEAKY"
make_tree "$FIXED"
make_tree "$BROKEN"
make_tree "$LEAKY"
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
printf '1.20 1.20 1.20 1/1\n' > "$TMP/load"
if drive "$FIXED" JOURNEY_LOADAVG="$TMP/load" -- 904-raise-load 901-budgets > "$TMP/p8.log" 2>&1; then
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

printf '\nclock_test: %s passed, %s failed\n' "$pass" "$fail"
[ "$fail" = 0 ] || exit 1
exit 0
