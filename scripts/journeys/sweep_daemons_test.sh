#!/usr/bin/env bash
# sweep_daemons_test.sh - the witness for sweep-daemons.sh.
#
# sweep-daemons.sh KILLS daemons (and the chrome each one holds), so its
# three-part decision needs a witness that can fail:
#
#   G1  run-lock exclusivity   - a foreign suite's lock blocks the sweep,
#                                the gate's own lock (matching pid) lets it
#                                through, a mismatched claimant stands down,
#                                a recycled pid (live, not a suite) is stale
#                                and swept through, an unidentifiable record
#                                stands down (card t_b000c0e3)
#   G2  attached-client probe  - a session with a command in flight is kept
#   G3  idle window            - a freshly started session is kept
#
# The agent-browser CLI and `netstat` are stubbed through PATH / JOURNEY_AB,
# so the decision is driven through the real script without touching a real
# daemon. The last case drops the stub and holds a REAL TCP connection, so
# the netstat/ss branch is exercised on the host's own socket table.
#
# Run: bash scripts/journeys/sweep_daemons_test.sh

set -uo pipefail

JROOT="$(cd "$(dirname "$0")" && pwd)"
SWEEP="$JROOT/sweep-daemons.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }

TMP="$(mktemp -d "${TMPDIR:-/tmp}/sweepdaemon-test.XXXXXX")"
case "$TMP" in
  # a C:/ path in PATH never resolves (MSYS splits at the drive colon), so the
  # bash-side fixture dirs stay POSIX
  [A-Za-z]:*) TMPP="$(cygpath -u "$TMP" 2>/dev/null || printf '%s' "$TMP")" ;;
  *) TMPP="$TMP" ;;
esac
# ... and the other way round for a NATIVE tool: windows python cannot open an
# MSYS /c/... path, so hand it the drive form
if command -v cygpath >/dev/null 2>&1; then TMPN="$(cygpath -m "$TMPP")"; else TMPN="$TMPP"; fi
STATE="$TMPP/state"
STUBBIN="$TMPP/bin"
STUB="$TMPP/agent-browser-stub"
LOG="$TMPP/calls.log"
LOCK="$TMPP/journeys.lock"
mkdir -p "$STATE" "$STUBBIN"

cleanup() {
  [ -n "${PYB:-}" ] && kill "$PYB" 2>/dev/null
  [ -n "${LANE:-}" ] && kill "$LANE" 2>/dev/null
  [ -n "${SUITE_PID:-}" ] && kill "$SUITE_PID" 2>/dev/null
  [ -n "${RECYCLED_PID:-}" ] && kill "$RECYCLED_PID" 2>/dev/null
  rm -rf "$TMP" 2>/dev/null
  true
}
trap cleanup EXIT

# --- stubs -------------------------------------------------------------------
cat > "$STUB" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STUB_LOG"
if [ "${1:-}" = session ] && [ "${2:-}" = list ]; then
  printf '{"success":true,"data":{"sessions":['
  first=1
  for s in ${STUB_SESSIONS:-}; do
    [ "$first" = 1 ] || printf ','
    printf '"%s"' "$s"
    first=0
  done
  printf ']}}\n'
  exit 0
fi
if [ "${3:-}" = close ]; then
  for s in ${STUB_FAIL:-}; do
    [ "$2" = "$s" ] && exit 1
  done
  exit 0
fi
exit 0
STUB
chmod +x "$STUB"

cat > "$STUBBIN/netstat" <<'STUBNET'
#!/usr/bin/env bash
printf 'Active Connections\n\n  Proto  Local Address          Foreign Address        State           PID\n'
for p in ${STUB_ATTACH_PORTS:-}; do
  printf '  TCP    127.0.0.1:%s         127.0.0.1:49999    ESTABLISHED     4242\n' "$p"
done
exit 0
STUBNET
chmod +x "$STUBBIN/netstat"

# --- fixtures ----------------------------------------------------------------
# nothing stamps an agent-browser session file after launch, so the pid file's
# mtime is the daemon's start; age it with touch -d @epoch
mk_session() { # $1 name, $2 age_secs, $3 control port
  printf '%s\n' 4242 > "$STATE/$1.pid"
  printf '%s\n' "$3" > "$STATE/$1.port"
  touch -d "@$(( $(date +%s) - $2 ))" "$STATE/$1.pid"
}

# the lock holder, in the shape THIS host's run.sh uses: flock where flock
# exists, the mkdir dir where it does not (MSYS). Both modes also leave the
# ${JOURNEY_LOCK}.pid record the gate reads.
HOLD_JOB=""
hold_lock() { # $1 = pid to record as the holder
  printf '%s\n' "$1" > "$LOCK.pid"
  if command -v flock >/dev/null 2>&1; then
    ( exec 9>>"$LOCK"; flock -n 9; sleep 20 ) 9>>"$LOCK" &
    HOLD_JOB=$!
    sleep 1
  else
    mkdir -p "$LOCK.d"
    printf '%s\n' "$1" > "$LOCK.d/pid"
  fi
}
drop_lock() {
  [ -n "$HOLD_JOB" ] && { kill "$HOLD_JOB" 2>/dev/null; wait "$HOLD_JOB" 2>/dev/null; HOLD_JOB=""; }
  rm -rf "$LOCK.d" "$LOCK.pid" 2>/dev/null
}

# a LIVE process whose argv NAMES .../journeys/run.sh = a genuine suite holder
# to the ownership check. `bash -c 'sleep 60' <path>` exec-optimises into a bare
# `sleep 60` (argv0 never reaches the process table), so the loop body keeps
# bash in argv. Never read the pid through $(...) - the command substitution's
# subshell takes the background job with it and the fixture reads as dead.
SUITE_PID=""
spawn_suite_holder() {
  bash -c 'while :; do sleep 1; done' "$TMPP/scripts/journeys/run.sh" &
  SUITE_PID=$!
  for _ in $(seq 1 100); do kill -0 "$SUITE_PID" 2>/dev/null && break; sleep 0.05; done
}
kill_fixture() { # $1 = pid
  [ -n "${1:-}" ] || return 0
  kill "$1" 2>/dev/null
  wait "$1" 2>/dev/null
  return 0
}

reset_log() { : > "$LOG"; }
closes() { grep -c -- ' close$' "$LOG" 2>/dev/null || true; }
closed_names() { sed -n 's/^--session \(.*\) close$/\1/p' "$LOG" 2>/dev/null | sort | tr '\n' ' '; }

# drive the real script. The stub dir is prepended per invocation, never
# exported: the last case must see the host's real socket table.
sd_script() { # $1 = the script to drive; then VAR=val assignments, then args
  local script="$1" assigns=() rc=0
  shift
  while [ "$#" -gt 0 ] && [ "${1#*=}" != "$1" ]; do assigns+=("$1"); shift; done
  env "${assigns[@]+"${assigns[@]}"}" \
    PATH="$STUBBIN:$PATH" \
    JOURNEY_AB="$STUB" \
    JOURNEY_DAEMON_STATE="$STATE" \
    JOURNEY_LOCK="$LOCK" \
    STUB_LOG="$LOG" \
    bash "$script" "$@" > "$TMPP/out.raw" 2>&1 || rc=$?
  tr -d '\r' < "$TMPP/out.raw"
  return "$rc"
}

sd() { sd_script "$SWEEP" "$@"; }

base_sessions="old-idle fresh attached omega-journey"
mk_session old-idle 1800 59101
mk_session fresh 60 59102
mk_session attached 1800 59103
mk_session omega-journey 30 59104

# --- A: a genuine suite holder holds the lock --------------------------------
# The recorded pid is a LIVE process whose argv names .../journeys/run.sh, so
# the ownership check convicts it as a suite: the stand-down below is the
# "genuine holder" leg, not merely a number that happens to be alive.
reset_log
drop_lock
spawn_suite_holder
hold_lock "$SUITE_PID"
out="$(sd "STUB_SESSIONS=$base_sessions" "STUB_ATTACH_PORTS=59103")"
rc=$?
[ "$rc" = 0 ] && ok || bad "A: exit $rc, want 0 (stand down). output: $out"
case "$out" in
  *"holds the run lock"*) ok ;;
  *) bad "A: stand-down reason missing. output: $out" ;;
esac
[ "$(closes)" = 0 ] && ok || bad "A: closed $(closes) session(s) while a suite held the lock: $(closed_names)"
kill_fixture "$SUITE_PID"; SUITE_PID=""
drop_lock

# --- B: the gate's own lock -> close idle, keep live -------------------------
# Gate mode reads the caller's own lock record, so this case seats it itself.
reset_log
drop_lock
hold_lock "$$"
out="$(sd "STUB_SESSIONS=$base_sessions" "STUB_ATTACH_PORTS=59103" \
         JOURNEY_GATE_PID="$$" JOURNEY_SESSION="omega-journey" --gate)"
rc=$?
[ "$rc" = 1 ] && ok || bad "B: exit $rc, want 1 (it closed something). output: $out"
[ "$(closed_names)" = "old-idle omega-journey " ] \
  && ok || bad "B: closed '$(closed_names)', want 'old-idle omega-journey ' (attached + fresh are live)"
case "$out" in
  *"attached is live (client attached)"*) ok ;;
  *) bad "B: the attached session was not named as live. output: $out" ;;
esac
case "$out" in
  *"fresh is 1m old"*) ok ;;
  *) bad "B: the fresh session was not held by the idle window. output: $out" ;;
esac

# --- C: a mismatched claimant cannot use gate mode ---------------------------
reset_log
drop_lock
hold_lock "$$"
out="$(sd "STUB_SESSIONS=$base_sessions" "STUB_ATTACH_PORTS=59103" \
         JOURNEY_GATE_PID=1 JOURNEY_SESSION="omega-journey" --gate)"
rc=$?
[ "$rc" = 0 ] && ok || bad "C: exit $rc, want 0 (stand down). output: $out"
[ "$(closes)" = 0 ] && ok || bad "C: gate mode with a foreign holder closed $(closes): $(closed_names)"
case "$out" in
  *"standing down"*) ok ;;
  *) bad "C: stand-down reason missing. output: $out" ;;
esac

# --- D: standalone with a free lock -> same decision, lock released ---------
reset_log
drop_lock
out="$(sd "STUB_SESSIONS=$base_sessions" "STUB_ATTACH_PORTS=59103" \
         JOURNEY_SESSION="omega-journey")"
rc=$?
[ "$rc" = 1 ] && ok || bad "D: exit $rc, want 1. output: $out"
[ "$(closed_names)" = "old-idle " ] && ok \
  || bad "D: standalone closed '$(closed_names)', want 'old-idle ' only (no gate exemption outside the gate)"
[ -e "$LOCK.d" ] && bad "D: the stand-alone sweep left its lock dir behind" || ok

# --- I: a recycled pid (LIVE, but not a suite) is stale -----------------------
# The failure this card exists for: a dead suite's dir whose number the host
# since handed to an unrelated live process. Under the old pid-liveness rule
# this stood down forever ("a journey suite holds the run lock") while no suite
# ran, so the idle-daemon sweep never ran outside a suite. It must reap the
# stale dir, take the lock and sweep.
reset_log
drop_lock
sleep 60 &
RECYCLED_PID=$!
mkdir -p "$LOCK.d"
printf '%s\n' "$RECYCLED_PID" > "$LOCK.d/pid"
out="$(sd "STUB_SESSIONS=$base_sessions" "STUB_ATTACH_PORTS=59103" \
         JOURNEY_LOCK_IMPL=mkdir JOURNEY_SESSION="omega-journey")"
rc=$?
[ "$rc" = 1 ] && ok || bad "I: exit $rc, want 1 (a recycled-pid lock must not block the sweep). output: $out"
[ "$(closed_names)" = "old-idle " ] \
  && ok || bad "I: closed '$(closed_names)', want 'old-idle ' (the sweep never ran under the stale lock)"
case "$out" in
  *"holds the run lock"*) bad "I: the sweep stood down on a live non-suite pid. output: $out" ;;
  *"reaping a stale run lock"*) ok ;;
  *) bad "I: the reap was not reported. output: $out" ;;
esac
kill_fixture "$RECYCLED_PID"; RECYCLED_PID=""
drop_lock

# --- J: the witness can fail - the OLD rule still wedges on I ----------------
# A copy reduced back to `kill -0` must stand down on the same fixture, or case
# I proves nothing about the ownership check.
reset_log
drop_lock
LEGACY="$TMPP/legacy-sweep.sh"
sed 's|^  if lock_holder_reapable "\$holder"; then$|  if ! kill -0 "$holder" 2>/dev/null; then|' \
  "$SWEEP" > "$LEGACY"
if cmp -s "$SWEEP" "$LEGACY"; then
  bad "J: the reduction changed nothing, so the canary would run the fixed rule twice - vacuous"
elif grep -q 'if ! kill -0 "$holder" 2>/dev/null; then' "$LEGACY"; then
  sleep 60 &
  RECYCLED_PID=$!
  mkdir -p "$LOCK.d"
  printf '%s\n' "$RECYCLED_PID" > "$LOCK.d/pid"
  out="$(sd_script "$LEGACY" "STUB_SESSIONS=$base_sessions" "STUB_ATTACH_PORTS=59103" \
           JOURNEY_LOCK_IMPL=mkdir JOURNEY_SESSION="omega-journey")"
  rc=$?
  [ "$rc" = 0 ] && ok || bad "J: the legacy rule exit $rc, want 0 (a live recycled pid must wedge it). output: $out"
  [ "$(closes)" = 0 ] && ok || bad "J: the legacy rule swept anyway: $(closed_names)"
  case "$out" in
    *"holds the run lock"*) ok ;;
    *) bad "J: the legacy rule did not report the stand-down. output: $out" ;;
  esac
  kill_fixture "$RECYCLED_PID"; RECYCLED_PID=""
else
  bad "J: could not reduce the script to the old rule - case I would not be falsifiable"
fi
drop_lock

# --- K: an unidentifiable record is HELD, never reaped -----------------------
# Three shapes of "cannot prove this is not a live suite": an empty record, a
# junk record, and the mkdir-then-write window (dir with no pid file yet).
# Reaping any of them could tear a live suite's lock and interleave two suites.
for shape in empty junk nopid; do
  reset_log
  drop_lock
  mkdir -p "$LOCK.d"
  case "$shape" in
    empty) : > "$LOCK.d/pid" ;;
    junk) printf 'not-a-pid\n' > "$LOCK.d/pid" ;;
  esac
  out="$(sd "STUB_SESSIONS=$base_sessions" "STUB_ATTACH_PORTS=59103" JOURNEY_LOCK_IMPL=mkdir)"
  rc=$?
  [ "$rc" = 0 ] && ok || bad "K/$shape: exit $rc, want 0 (stand down). output: $out"
  [ "$(closes)" = 0 ] && ok || bad "K/$shape: an unidentifiable record closed $(closes): $(closed_names)"
done
drop_lock

# --- E: dry run decides, closes nothing --------------------------------------
reset_log
hold_lock "$$"
out="$(sd "STUB_SESSIONS=$base_sessions" "STUB_ATTACH_PORTS=59103" \
         JOURNEY_GATE_PID="$$" JOURNEY_SESSION="omega-journey" --gate -n)"
rc=$?
[ "$rc" = 0 ] && ok || bad "E: exit $rc, want 0 for a dry run. output: $out"
[ "$(closes)" = 0 ] && ok || bad "E: -n closed $(closes) session(s)"
case "$out" in
  *"would close idle daemon old-idle"*) ok ;;
  *) bad "E: dry run did not name old-idle. output: $out" ;;
esac
case "$out" in
  *"2 idle daemon(s) would be closed"*) ok ;;
  *) bad "E: dry-run summary wrong (want 2: old-idle + the gate session). output: $out" ;;
esac

# --- F: a close that fails is not counted as a close -------------------------
reset_log
drop_lock
out="$(sd "STUB_SESSIONS=old-idle" "STUB_FAIL=old-idle" )"
rc=$?
[ "$rc" = 0 ] && ok || bad "F: exit $rc, want 0 when the only close failed. output: $out"
case "$out" in
  *"close failed for old-idle"*) ok ;;
  *) bad "F: failed close not reported. output: $out" ;;
esac

# --- G: the REAL socket table, not the stub ---------------------------------
real=0
command -v python3 >/dev/null 2>&1 && real=1
if [ "$real" = 1 ]; then
  STATE2="$TMPP/state2"
  mkdir -p "$STATE2"
  python3 - "$TMPN/liveport" >/dev/null 2>&1 <<'PY' &
import socket, sys, time
s = socket.socket()
s.bind(("127.0.0.1", 0))
s.listen(1)
port = s.getsockname()[1]
open(sys.argv[1], "w").write(str(port))
c = socket.create_connection(("127.0.0.1", port))
a, _ = s.accept()
time.sleep(30)
PY
  PYB=$!
  for _ in $(seq 1 60); do [ -s "$TMPP/liveport" ] && break; sleep 0.2; done
  liveport="$(cat "$TMPP/liveport" 2>/dev/null || echo '')"
  if [ -z "$liveport" ]; then
    bad "G: the real socket fixture never came up - the probe branch is unwitnessed"
  else
    printf '4242\n' > "$STATE2/real-attached.pid"
    printf '%s\n' "$liveport" > "$STATE2/real-attached.port"
    touch -d "@$(( $(date +%s) - 1800 ))" "$STATE2/real-attached.pid"
    printf '4242\n' > "$STATE2/real-idle.pid"
    printf '59199\n' > "$STATE2/real-idle.port"
    touch -d "@$(( $(date +%s) - 1800 ))" "$STATE2/real-idle.pid"
    reset_log
    drop_lock
    env JOURNEY_AB="$STUB" JOURNEY_DAEMON_STATE="$STATE2" JOURNEY_LOCK="$LOCK" \
        STUB_LOG="$LOG" STUB_SESSIONS="real-attached real-idle" \
        bash "$SWEEP" > "$TMPP/out.raw" 2>&1
    rc=$?
    out="$(tr -d '\r' < "$TMPP/out.raw")"
    [ "$rc" = 1 ] && ok || bad "G: exit $rc, want 1. output: $out"
    [ "$(closed_names)" = "real-idle " ] \
      && ok || bad "G: closed '$(closed_names)', want 'real-idle ' (a real ESTABLISHED socket must protect real-attached)"
  fi
else
  echo "SKIP case G: no python3 (the real socket-table branch stays unwitnessed)"
fi

# --- H: run.sh really calls the sweep at the gate ---------------------------
# The decision tests above prove the sweep's own logic. This proves the WIRING:
# run.sh must invoke it after the last recipe, with the lock pid the sweep
# verifies against, or the whole card is a comment. A second tree has the call
# neutralised - that shape must close nothing, or the assertion is vacuous.
if [ "$real" = 1 ]; then
  LANE_PORT="$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
  cat > "$TMPP/lane.py" <<'PY'
import http.server
import sys


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        body = b'{"ok":true,"models":[],"sessions":[]}'
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    do_POST = do_GET
    do_PUT = do_GET
    do_DELETE = do_GET

    def log_message(self, *a):
        pass


http.server.ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()
PY
  python3 "$TMPN/lane.py" "$LANE_PORT" >/dev/null 2>&1 &
  LANE=$!
  for _ in $(seq 1 60); do
    curl -s -m 1 "http://127.0.0.1:$LANE_PORT/health" >/dev/null 2>&1 && break
    sleep 0.2
  done

  # run.sh resolves the CLI itself (`command -v agent-browser`), so the stub
  # needs that name on PATH as well as the JOURNEY_AB it passes to the sweep
  cp "$STUB" "$STUBBIN/agent-browser"
  chmod +x "$STUBBIN/agent-browser"

  STATE3="$TMPP/state3"
  mkdir -p "$STATE3"
  printf '4242\n' > "$STATE3/gate-idle.pid"
  printf '59201\n' > "$STATE3/gate-idle.port"
  touch -d "@$(( $(date +%s) - 1800 ))" "$STATE3/gate-idle.pid"
  printf '4242\n' > "$STATE3/gate-fresh.pid"
  printf '59202\n' > "$STATE3/gate-fresh.port"

  mk_tree() { # $1 dir, $2 = keep | strip
    mkdir -p "$1/recipes"
    cp "$JROOT/lib.sh" "$JROOT/sweep-daemons.sh" "$1/"
    if [ "$2" = strip ]; then
      awk '{ if ($0 ~ /sweep-daemons\.sh" -q --gate/) print "  : # gate call stripped by the witness"; else print }' \
        "$JROOT/run.sh" > "$1/run.sh"
    else
      cp "$JROOT/run.sh" "$1/run.sh"
    fi
    printf '#!/bin/bash\n: # a no-op recipe: the gate is the subject here\n' \
      > "$1/recipes/900-nowhere.sh"
  }

  gate_run() { # $1 tree -> output; exits with run.sh's status
    local rc=0
    ( cd "$1" && PATH="$STUBBIN:$PATH" STUB_LOG="$LOG" \
        STUB_SESSIONS="gate-idle gate-fresh" \
        JOURNEY_DAEMON_STATE="$STATE3" JOURNEY_LOCK="$LOCK" \
        JOURNEY_BASE_URL="http://127.0.0.1:$LANE_PORT" JOURNEY_TIMEOUT=60 \
        bash "$1/run.sh" > "$TMPP/run.raw" 2>&1 ) || rc=$?
    tr -d '\r' < "$TMPP/run.raw"
    return "$rc"
  }

  drop_lock
  reset_log
  mk_tree "$TMPP/tree-keep" keep
  out="$(gate_run "$TMPP/tree-keep")"
  rc=$?
  [ "$rc" = 0 ] && ok || bad "H: run.sh exit $rc, want 0. output: $out"
  case "$out" in
    *"suite done"*) ok ;;
    *) bad "H: run.sh never reached its summary. output: $out" ;;
  esac
  [ "$(closed_names)" = "gate-idle " ] \
    && ok || bad "H: the gate closed '$(closed_names)', want 'gate-idle ' (run.sh must call the sweep, and only the idle daemon goes)"
  case "$out" in
    *"closed idle daemon gate-idle"*) ok ;;
    *) bad "H: the sweep reported nothing at the gate. output: $out" ;;
  esac

  drop_lock
  reset_log
  mk_tree "$TMPP/tree-strip" strip
  out="$(gate_run "$TMPP/tree-strip")"
  rc=$?
  [ "$rc" = 0 ] && ok || bad "H-strip: run.sh exit $rc, want 0. output: $out"
  [ "$(closes)" = 0 ] \
    && ok || bad "H-strip: a run.sh without the gate call still closed '$(closed_names)' - the wiring assertion proves nothing"
else
  echo "SKIP case H: no python3 (the run.sh wiring stays unwitnessed)"
fi

printf '\nsweep_daemons_test: %s passed, %s failed\n' "$pass" "$fail"
[ "$fail" = 0 ] || exit 1
