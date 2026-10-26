#!/usr/bin/env bash
set -uo pipefail

# orphan_test.sh - the chrome-death contract of scripts/journeys/run.sh.
#
# What kills the chrome, by death of run.sh:
#   normal exit / INT / TERM -> the EXIT trap -> stop_chrome (group kill)
#   SIGKILL on run.sh        -> nothing can trap -> sweep-chromes.sh
# This test drives the REAL run.sh and the REAL sweep, with a stub
# agent-browser on PATH (run.sh hangs at its `ab connect`, which is after
# the chrome launch and before any recipe runs - a deterministic hang
# window with a live chrome).
#
# Phases:
#   A  TERM run.sh                     -> trap kills the chrome
#   B  kill -9 run.sh                  -> orphan; sweep reaps it
#   C  chrome with a LIVE parent shell -> sweep leaves it alone
#   D  dbg*-shaped orphan              -> sweep reaps it; C survives the
#                                         same sweep run
#
# Run: bash scripts/journeys/orphan_test.sh
# Leftovers: the stub connect hangs as `sleep 240` (zeroCPU, self-expiring);
# the test pkills those that still exist at the end.

JROOT="$(cd "$(dirname "$0")" && pwd)"
RUN="$JROOT/run.sh"
SWEEP="$JROOT/sweep-chromes.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }

# --- fixtures -----------------------------------------------------------------

TMP="$(mktemp -d "${TMPDIR:-/tmp}/orphan-test.XXXXXX")"
cleanup() {
  # our own chromes first (any phase stragglers), then the fake lane and
  # the stub hangs; a freshly killed chrome may still be writing - rm twice
  kill "${LANE_PID:-}" 2>/dev/null
  rm -rf "$TMP" 2>/dev/null
  sleep 1
  rm -rf "$TMP" 2>/dev/null
  pkill -9 -f '^sleep 240$' 2>/dev/null
  true
}
trap cleanup EXIT

health_dir="$TMP/lane"
mkdir -p "$health_dir"
printf '{"ok":true}' >"$health_dir/health"

# stub agent-browser: `connect` hangs (the deterministic window); every
# other call succeeds instantly. Never touches a real browser daemon.
stub_dir="$TMP/stub"
mkdir -p "$stub_dir"
cat >"$stub_dir/agent-browser" <<'EOF'
#!/bin/bash
# orphan_test stub - no browser, ever. connect hangs on purpose.
case " $* " in
  *" connect "*) sleep 240 ;;
  *) exit 0 ;;
esac
EOF
chmod +x "$stub_dir/agent-browser"

BIN="$(ls "$HOME"/.hermes/tools/chromium-*/chrome-linux/chrome.real 2>/dev/null | head -1)"
if [ -z "$BIN" ]; then
  echo "SKIP: no bundled chromium on this host" >&2
  exit 0
fi
LIBS="$(find "$HOME/.hermes/profiles" -maxdepth 2 -type d -name chromium-libs 2>/dev/null | head -1)"
[ -n "$LIBS" ] || LIBS="$(dirname "$BIN")"

free_port() {
  local p
  for p in $(seq "$1" $(( $1 + 60 ))); do
    ss -tln 2>/dev/null | grep -q ":$p " || { echo "$p"; return 0; }
  done
  return 1
}

# chrome_main_up <profile-dir>: pid of a chrome.real MAIN whose cmdline
# names that profile dir (the /proc comm scan - the safe pattern).
chrome_main_up() {
  local p comm
  for p in /proc/[0-9]*; do
    comm="$(cat "$p/comm" 2>/dev/null)"
    [ "$comm" = "chrome.real" ] || continue
    case "$(tr '\0' ' ' <"$p/cmdline" 2>/dev/null)" in
      *"--user-data-dir=$1"*) echo "${p#/proc/}"; return 0 ;;
    esac
  done
  return 1
}

# start_run <cdp-port>: start run.sh with stub AB + fake lane. Echoes pid.
start_run() {
  local cdp="$1"
  (
    export PATH="$stub_dir:$PATH"
    export JOURNEY_BASE_URL="$LANE"
    export JOURNEY_CHROME_PORT="$cdp"
    export JOURNEY_CHROME_BIN="$BIN"
    export JOURNEY_CHROME_LIBS="$LIBS"
    export JOURNEY_SESSION="orphan-test-$$"
    export TMPDIR="$TMP"
    export JOURNEY_TIMEOUT=120
    exec bash "$RUN" no-such-recipe-xyz
  ) >"$TMP/run.$cdp.log" 2>&1 &
  echo $!
}

# wait_chrome <profile-dir> [timeout_s]: poll until a main chrome wears it.
wait_chrome() {
  local ud="$1" t="${2:-20}" i=0 pid
  while [ "$i" -lt $(( t * 4 )) ]; do
    pid="$(chrome_main_up "$ud")" && { echo "$pid"; return 0; }
    sleep 0.25
    i=$((i + 1))
  done
  return 1
}

# chrome_gone <profile-dir> [timeout_s]: poll until no main wears it.
chrome_gone() {
  local ud="$1" t="${2:-6}" i=0
  while [ "$i" -lt $(( t * 4 )) ]; do
    chrome_main_up "$ud" >/dev/null 2>&1 || return 0
    sleep 0.25
    i=$((i + 1))
  done
  return 1
}

# --- fake lane ------------------------------------------------------------------

LANE_PORT="$(free_port 18400)" || { echo "FAIL: no free lane port" >&2; exit 1; }
LANE="http://127.0.0.1:$LANE_PORT"
python3 -m http.server "$LANE_PORT" --bind 127.0.0.1 --directory "$health_dir" \
  >/dev/null 2>&1 &
LANE_PID=$!
for _ in $(seq 1 20); do
  curl -s "$LANE/health" 2>/dev/null | grep -q '"ok"' && break
  sleep 0.25
done
curl -s "$LANE/health" 2>/dev/null | grep -q '"ok"' \
  || { echo "FAIL: fake lane did not come up" >&2; exit 1; }

# --- phase A: TERM on run.sh -> EXIT trap kills chrome ---------------------------

A_PORT="$(free_port 18500)"
A_UD="$TMP/omega-journeys-chrome-profile-$A_PORT"
A_PID="$(start_run "$A_PORT")"
A_PID_REAL="$(chrome_main_up "$A_UD")" \
  && [ -n "$A_PID_REAL" ] \
  || A_PID_REAL="$(wait_chrome "$A_UD" 25)"
if [ -n "$A_PID_REAL" ]; then
  ok
else
  bad "A: chrome never appeared"; cat "$TMP/run.$A_PORT.log" >&2
fi
if [ -n "${A_PID_REAL:-}" ]; then
  kill -TERM "$A_PID" 2>/dev/null
  wait "$A_PID" 2>/dev/null
  if chrome_gone "$A_UD" 6; then
    ok
  else
    bad "A: chrome $A_PID_REAL survived TERM to run.sh (trap did not fire)"
  fi
fi

# --- phase B: kill -9 run.sh -> orphan -> sweep reaps -----------------------------

B_PORT="$(free_port 18600)"
B_UD="$TMP/omega-journeys-chrome-profile-$B_PORT"
B_PID="$(start_run "$B_PORT")"
if wait_chrome "$B_UD" 25 >/dev/null; then
  ok
else
  bad "B: chrome never appeared"; cat "$TMP/run.$B_PORT.log" >&2
fi
kill -9 "$B_PID" 2>/dev/null
sleep 1
if chrome_main_up "$B_UD" >/dev/null 2>&1; then
  ok
else
  bad "B: chrome exited before the sweep saw the orphan (launch crash?) - sweep-exit assertion below is then meaningless"
fi
if bash "$SWEEP" -q >/dev/null 2>&1; then
  bad "B: sweep exited 0 but an orphan existed - it did not reap"
else
  ok
fi
if chrome_gone "$B_UD" 6; then
  ok
else
  bad "B: orphan chrome survived the sweep: $B_UD"
fi

# --- phase C: live-parent chrome -> sweep must leave it ---------------------------

C_PORT="$(free_port 18700)"
C_UD="$TMP/dbg-live-parent-chrome-profile"
env LD_LIBRARY_PATH="$LIBS:$(dirname "$BIN")" "$BIN" \
  --headless=new --remote-debugging-port="$C_PORT" \
  --user-data-dir="$C_UD" --no-first-run --disable-gpu \
  >/dev/null 2>&1 &
C_PARENT=$!
if wait_chrome "$C_UD" 25 >/dev/null; then
  ok
else
  bad "C: fixture chrome never appeared"
fi
C_SAW_CHROME=0
chrome_main_up "$C_UD" >/dev/null 2>&1 && C_SAW_CHROME=1
bash "$SWEEP" -q >/dev/null 2>&1
C_RC=$?
if [ "$C_RC" = "0" ]; then
  ok
else
  bad "C: sweep exited $C_RC against a live-parent chrome (fixture may be gone)"
fi
if chrome_main_up "$C_UD" >/dev/null 2>&1; then
  ok
else
  if [ "$C_SAW_CHROME" = "1" ]; then
    bad "C: sweep killed a chrome whose parent shell is alive"
  fi
fi

# --- phase D: dbg-shaped orphan -> sweep reaps; C's chrome survives ----------------

D_PORT="$(free_port 18800)"

# the inner bash is the chrome's parent; kill it to orphan the chrome.
# The launcher runs detached from any setsid: chrome inherits the wrapper
# bash's process group, like a worker's debug chrome would.
inner_bash=$(bash -c '
  env LD_LIBRARY_PATH="'"$LIBS:$(dirname "$BIN")"'" "'"$BIN"'" \
    --headless=new --remote-debugging-port='"$D_PORT"' --no-first-run --disable-gpu \
    --user-data-dir="'"$TMP"'/dbg9-orphan-chrome-profile" \
    >/dev/null 2>&1 &
  sleep 240
' >/dev/null 2>&1 & echo $!)
# the inner bash is the chrome's parent; kill it to orphan the chrome
sleep 2
kill -9 "$inner_bash" 2>/dev/null
D_ORPHAN=""
for i in $(seq 1 12); do
  D_ORPHAN="$(chrome_main_up "$TMP/dbg9-orphan-chrome-profile")" && break
  sleep 0.5
done
if [ -n "${D_ORPHAN:-}" ]; then
  ppid="$(awk '{print $4}' "/proc/$D_ORPHAN/stat" 2>/dev/null)"
  if [ "$ppid" = "1" ]; then
    ok
  else
    bad "D: orphan has ppid $ppid, not 1 - sweep would skip it"
  fi
  bash "$SWEEP" >/dev/null 2>&1 && bad "D: sweep exited 0 with a known orphan present"
  chrome_gone "$TMP/dbg9-orphan-chrome-profile" 6 \
    && ok || bad "D: dbg-shaped orphan survived the sweep"
  if chrome_main_up "$C_UD" >/dev/null 2>&1; then
    ok
  else
    bad "D: the same sweep killed phase C's live-parent chrome"
  fi
else
  bad "D: orphan fixture never appeared"
fi
kill "$C_PARENT" 2>/dev/null

# --- report ------------------------------------------------------------------------

printf 'orphan_test: %s passed, %s failed\n' "$pass" "$fail"
[ "$fail" = "0" ]