#!/usr/bin/env bash
# runlock_test.sh - the witness for run.sh's suite run lock.
#
# Two claims pull in opposite directions, so both need a test that can fail:
#
#   1. a GENUINE holder is refused - the second suite exits 3, names the
#      holder pid and runs nothing
#   2. a STALE record is reaped - the owner pid is dead, or the number now
#      belongs to something that is not a journey suite. That second shape is
#      the live wedge of 2026-11-17 (card t_804ab59c): a dead suite's pid file
#      whose number had been handed to an unrelated process refused every
#      later suite with "another journey suite holds the run lock (pid N)"
#      while no suite was running
#   3. and the reap never overshoots - an owner that cannot be identified is
#      HELD. Refusing a suite is a nuisance; reaping a live suite's lock lets
#      two suites interleave, the exact failure the lock exists to prevent
#
# Every case drives the REAL run.sh. The suite runs with JOURNEY_LOCK_IMPL=mkdir
# so the MSYS shape (mkdir dir + pid file, no kernel flock) is what is tested
# even on a host that does have flock(1); the flock shape gets its own case
# where flock exists. The lane is a python stub and `agent-browser` is a no-op
# stub, so nothing touches a browser, a real lane or a real home.
#
# Case I pins the wedge itself: a copy of run.sh carrying the OLD
# pid-liveness-only rule must refuse the same recycled-pid record that the
# current run.sh reaps, or the witness proves nothing.
#
# Run: bash scripts/journeys/runlock_test.sh

set -uo pipefail

JROOT="$(cd "$(dirname "$0")" && pwd)"
RUN="$JROOT/run.sh"
LIB="$JROOT/lib.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }

if ! command -v python3 >/dev/null 2>&1; then
  echo 'runlock_test: python3 is required (the witness needs a lane stub to reach the suite body)' >&2
  exit 2
fi

TMP="$(mktemp -d "${TMPDIR:-/tmp}/runlock-test.XXXXXX")"
case "$TMP" in
  # a C:/ path in PATH never resolves (MSYS splits at the drive colon), so the
  # bash-side fixture dirs stay POSIX; native tools get the drive form
  [A-Za-z]:*) TMPP="$(cygpath -u "$TMP" 2>/dev/null || printf '%s' "$TMP")" ;;
  *) TMPP="$TMP" ;;
esac
if command -v cygpath >/dev/null 2>&1; then TMPN="$(cygpath -m "$TMPP")"; else TMPN="$TMPP"; fi

LOCK="$TMPP/journeys.lock"
DAEMONS="$TMPP/daemons"
HOME_FIXTURE="$TMPP/home"
STUBBIN="$TMPP/bin"
STUB="$TMPP/agent-browser-stub"
mkdir -p "$DAEMONS" "$HOME_FIXTURE" "$STUBBIN"

KIDS=""
cleanup() {
  local k
  for k in $KIDS; do kill "$k" 2>/dev/null; done
  kill "${LANE_PID:-}" "${HOLDER:-}" 2>/dev/null
  rm -rf "$TMP" 2>/dev/null
  true
}
trap cleanup EXIT

# --- fixtures ----------------------------------------------------------------

# mk_tree <dir> [legacy]: a run.sh-shaped tree. legacy carries the OLD
# pid-liveness-only staleness rule, so case I can prove the wedge is real.
mk_tree() {
  mkdir -p "$1/scripts/journeys/recipes"
  if [ "${2:-}" = legacy ]; then
    awk '{ if ($0 ~ /^ *if journey_lock_reapable/) print "    if ! kill -0 \"$holder\" 2>/dev/null; then"; else print }' \
      "$RUN" > "$1/scripts/journeys/run.sh"
  else
    cp "$RUN" "$1/scripts/journeys/run.sh"
  fi
  cp "$LIB" "$1/scripts/journeys/"
  printf '#!/bin/bash\n: # no-op recipe - the lock is the subject here\n' \
    > "$1/scripts/journeys/recipes/901-noop.sh"
  printf '#!/bin/bash\n: # keeps the suite body open so a second suite can arrive\nsleep 4\n' \
    > "$1/scripts/journeys/recipes/902-hold.sh"
}

TREE="$TMPP/tree"
LEGACY="$TMPP/tree-legacy"
mk_tree "$TREE"
mk_tree "$LEGACY" legacy

# agent-browser stub: every call succeeds instantly, no browser is ever
# touched. run.sh resolves the CLI itself (command -v agent-browser) and
# passes the same path to the daemon sweep.
cat > "$STUB" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STUB_LOG"
exit 0
STUB
chmod +x "$STUB"
cp "$STUB" "$STUBBIN/agent-browser"
chmod +x "$STUBBIN/agent-browser"

# lane stub: /health must answer ok or run.sh exits 2 before a recipe runs
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
        body = b'{"ok":true,"models":[],"sessions":[],"tasks":[]}'
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
LANE_PID=$!
for _ in $(seq 1 60); do
  curl -s -m 1 "http://127.0.0.1:$LANE_PORT/health" >/dev/null 2>&1 && break
  sleep 0.2
done
curl -s -m 1 "http://127.0.0.1:$LANE_PORT/health" | grep -q '"ok"' \
  || { echo "runlock_test: the lane stub never came up - nothing below is witnessed" >&2; exit 2; }

# --- driving run.sh ----------------------------------------------------------

# suite <tree> <impl> [args...] -> OUT (stdout+stderr, CR stripped), returns
# run.sh's own exit status. JOURNEY_LOCK/IMPL ride the environment so every
# case reads the same lock.
suite() {
  local tree="$1" impl="$2" rc=0
  shift 2
  ( cd "$tree" && env \
      PATH="$STUBBIN:$PATH" \
      TMPDIR="$TMPP" \
      JOURNEY_LOCK_IMPL="$impl" \
      JOURNEY_LOCK="$LOCK" \
      JOURNEY_LOCK_WAIT=0 \
      JOURNEY_BASE_URL="http://127.0.0.1:$LANE_PORT" \
      JOURNEY_CHROME_PORT=59731 \
      JOURNEY_SESSION=runlock-test \
      JOURNEY_TIMEOUT=60 \
      JOURNEY_AB="$STUB" \
      STUB_LOG="$TMPP/calls.log" \
      JOURNEY_DAEMON_STATE="$DAEMONS" \
      OMEGA_HOME="$HOME_FIXTURE" \
      bash "$tree/scripts/journeys/run.sh" "$@" > "$TMPP/out.raw" 2>&1 ) || rc=$?
  OUT="$(tr -d '\r' < "$TMPP/out.raw")"
  return "$rc"
}

clear_lock() { rm -rf "$LOCK.d" "$LOCK.pid" 2>/dev/null; }
seed_lock() { # $1: pid text to record as the holder
  rm -rf "$LOCK.d" 2>/dev/null
  mkdir -p "$LOCK.d"
  printf '%s\n' "$1" > "$LOCK.d/pid"
}
dead_pid() { # a pid that cannot be alive: spawned, reaped, never reissued soon
  bash -c 'exit 0' &
  DEADP=$!
  wait "$DEADP" 2>/dev/null
}
spawn_with_cwd() { # $1: cwd, rest: argv -> SPAWNED = pid of a LIVE process
  # NOT a command substitution: `pid=$(spawn ...)` runs the spawn in a
  # throwaway subshell, and the job's pid dies with it (measured on MSYS -
  # kill -0 said DEAD one line later, which silently turned every fixture
  # into a dead-pid case).
  local dir="$1"
  shift
  ( cd "$dir" && exec "$@" ) &
  SPAWNED=$!
  KIDS="$KIDS $SPAWNED"
}
ran_green() { # $1 case tag, $2 rc, $3 output
  [ "$2" = 0 ] && ok || bad "$1: exit $2, want 0 (the suite must run). output: $(printf '%s' "$3" | tail -4)"
  case "$3" in
    *"suite done"*) ok ;;
    *) bad "$1: the suite never reached its summary. output: $(printf '%s' "$3" | tail -4)" ;;
  esac
  [ -e "$LOCK.d" ] && bad "$1: the run lock dir was left behind" || ok
}
refused() { # $1 case tag, $2 rc, $3 output, $4 pid text that must be named
  [ "$2" = 3 ] && ok || bad "$1: exit $2, want 3 (refused). output: $(printf '%s' "$3" | tail -4)"
  case "$3" in
    *"another journey suite holds the run lock (pid $4)"*) ok ;;
    *) bad "$1: the refusal did not name the holder (pid $4). output: $(printf '%s' "$3" | tail -4)" ;;
  esac
  case "$3" in
    *"suite done"*) bad "$1: a refused suite still ran the body" ;;
    *) ok ;;
  esac
}

# --- A: a genuine holder is refused (mkdir shape) ----------------------------
clear_lock
( cd "$TREE" && env PATH="$STUBBIN:$PATH" TMPDIR="$TMPP" \
    JOURNEY_LOCK_IMPL=mkdir JOURNEY_LOCK="$LOCK" JOURNEY_LOCK_WAIT=0 \
    JOURNEY_BASE_URL="http://127.0.0.1:$LANE_PORT" JOURNEY_CHROME_PORT=59731 \
    JOURNEY_SESSION=runlock-test JOURNEY_TIMEOUT=60 JOURNEY_AB="$STUB" \
    STUB_LOG="$TMPP/calls.log" JOURNEY_DAEMON_STATE="$DAEMONS" \
    OMEGA_HOME="$HOME_FIXTURE" \
    bash "$TREE/scripts/journeys/run.sh" 902-hold > "$TMPP/holder.raw" 2>&1 ) &
HOLDER=$!
held=""
for _ in $(seq 1 100); do
  [ -s "$LOCK.d/pid" ] && { held="$(tr -d '\r\n' < "$LOCK.d/pid")"; break; }
  sleep 0.1
done
if [ -z "$held" ]; then
  bad "A: the holder never took the run lock - the refusal case is unwitnessed"
  kill "$HOLDER" 2>/dev/null
else
  ok
  suite "$TREE" mkdir
  rc=$?
  refused A "$rc" "$OUT" "$held"
  kill -0 "$HOLDER" 2>/dev/null && ok || bad "A: the holder was gone before the refusal - the case proved nothing"
  wait "$HOLDER" 2>/dev/null
  hrc=$?
  [ "$hrc" = 0 ] && ok || bad "A: the holder exited $hrc, want 0. output: $(tr -d '\r' < "$TMPP/holder.raw" | tail -4)"
  [ -e "$LOCK.d" ] && bad "A: the holder left its run lock dir behind" || ok
fi
HOLDER=""

# --- B: a dead holder pid is stale ------------------------------------------
clear_lock
dead_pid
gone="$DEADP"
seed_lock "$gone"
suite "$TREE" mkdir 901-noop
rc=$?
ran_green B "$rc" "$OUT"
case "$OUT" in
  *"reaping a stale run lock - pid $gone is not a live journey suite"*) ok ;;
  *) bad "B: the stale record was not reported as reaped. output: $(printf '%s' "$OUT" | head -3)" ;;
esac

# --- C: THE WEDGE - a recycled pid is stale, not a holder -------------------
# a live process whose argv names no journey suite: exactly what an unrelated
# process that inherited a dead suite's pid looks like
clear_lock
spawn_with_cwd "$TMPP" sleep 60
recycled="$SPAWNED"
seed_lock "$recycled"
kill -0 "$recycled" 2>/dev/null && ok || bad "C: the recycled-pid fixture is not alive - the case proves nothing"
suite "$TREE" mkdir 901-noop
rc=$?
ran_green C "$rc" "$OUT"
case "$OUT" in
  *"reaping a stale run lock - pid $recycled"*) ok ;;
  *) bad "C: a recycled pid still wedged the suite. output: $(printf '%s' "$OUT" | head -3)" ;;
esac

# --- D: a foreign run.sh is not a journey suite -----------------------------
# the match is the suite's own path, not any file called run.sh. The fixture
# body is a loop on purpose: `bash -c 'sleep 60' x` exec-optimises into a bare
# `sleep 60` and the argv0 (the run.sh path) never reaches the process table.
clear_lock
spawn_with_cwd "$TMPP" bash -c 'while :; do sleep 1; done' /opt/elsewhere/tools/run.sh
foreign="$SPAWNED"
seed_lock "$foreign"
suite "$TREE" mkdir 901-noop
rc=$?
ran_green D "$rc" "$OUT"
case "$OUT" in
  *"reaping a stale run lock - pid $foreign"*) ok ;;
  *) bad "D: another tool's run.sh was read as a journey suite. output: $(printf '%s' "$OUT" | head -3)" ;;
esac

# --- E: ./run.sh from the suite home is a SUITE, not a recycled pid ---------
# argv carries only the bare script name there, so the cwd is the signal that
# has to convict it - the guard against reaping a live suite's lock
clear_lock
spawn_with_cwd "$TREE/scripts/journeys" bash -c 'while :; do sleep 1; done' ./run.sh
bare="$SPAWNED"
seed_lock "$bare"
suite "$TREE" mkdir 901-noop
rc=$?
refused E "$rc" "$OUT" "$bare"

# --- F: an unidentifiable record is HELD, never reaped ----------------------
# F1 a corrupt record (the '?' a failed read leaves)
clear_lock
seed_lock '?'
suite "$TREE" mkdir 901-noop
rc=$?
refused F1 "$rc" "$OUT" '?'
# F2 the empty record inside the winner's mkdir-then-write window: reaping it
# would hand the lock to a second suite while the first still runs
clear_lock
mkdir -p "$LOCK.d"
: > "$LOCK.d/pid"
suite "$TREE" mkdir 901-noop
rc=$?
[ "$rc" = 3 ] && ok || bad "F2: exit $rc, want 3 (an unwritten record is unproven, not stale). output: $(printf '%s' "$OUT" | tail -4)"


# --- G: the implementation pin is validated --------------------------------
clear_lock
suite "$TREE" bogus
rc=$?
[ "$rc" = 2 ] && ok || bad "G1: exit $rc, want 2 for an unknown JOURNEY_LOCK_IMPL"
case "$OUT" in
  *"JOURNEY_LOCK_IMPL must be auto, flock or mkdir"*) ok ;;
  *) bad "G1: the bad pin was not named. output: $(printf '%s' "$OUT" | head -3)" ;;
esac
if command -v flock >/dev/null 2>&1; then
  echo "SKIP case G2: flock exists here (the no-flock refusal cannot be exercised)"
else
  clear_lock
  suite "$TREE" flock
  rc=$?
  [ "$rc" = 2 ] && ok || bad "G2: exit $rc, want 2 when flock is pinned on a host without it"
fi

# --- H: the kernel shape still refuses a genuine holder ---------------------
if command -v flock >/dev/null 2>&1; then
  clear_lock
  ( exec 9>>"$LOCK"; flock -n 9; sleep 60 ) 9>>"$LOCK" &
  HOLDER=$!
  for _ in $(seq 1 50); do kill -0 "$HOLDER" 2>/dev/null && break; sleep 0.1; done
  printf '%s\n' "$HOLDER" > "$LOCK.pid"
  sleep 0.3
  suite "$TREE" flock
  rc=$?
  refused H "$rc" "$OUT" "$HOLDER"
  kill "$HOLDER" 2>/dev/null
  wait "$HOLDER" 2>/dev/null
  HOLDER=""
else
  echo "SKIP case H: no flock(1) on this host (the kernel-lock refusal rides the same message)"
fi

# --- I: the OLD rule still wedges - the witness can fail --------------------
clear_lock
legacy_pid=""
spawn_with_cwd "$TMPP" sleep 60
legacy_pid="$SPAWNED"
seed_lock "$legacy_pid"
suite "$LEGACY" mkdir 901-noop
rc=$?
[ "$rc" = 3 ] && ok || bad "I: the pid-liveness-only tree exited $rc, want 3 - the wedge is not reproduced, so cases B-D prove nothing"
[ -e "$LOCK.d" ] && ok || bad "I: the legacy tree reaped the record after all"

printf '\nrunlock_test: %s passed, %s failed\n' "$pass" "$fail"
[ "$fail" = 0 ] || exit 1
