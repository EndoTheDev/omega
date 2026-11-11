#!/usr/bin/env bash
# sweep_chromes_win_test.sh - the Windows half of the chrome-death contract.
#
# sweep-chromes.sh delegates to sweep-chromes.ps1 where /proc does not
# exist, and that half KILLS processes, so its decision needs a witness.
# Two phases, both driven through the real scripts:
#
#   A  replay a captured process table (SWEEP_PROC_JSON) and check the
#      kill list row by row: dead parent -> reap, live older parent ->
#      keep, live parent with a RECYCLED pid (created after the child) ->
#      reap, renderer child -> keep, foreign profile shape -> keep.
#   B  real chromes: one launched so its parent shell dies (orphan), one
#      launched from this test shell (live parent, protected). The
#      dispatcher must kill the first, leave the second, and exit 1; a
#      second replay of the same table must produce the same list.
#
# Run: bash scripts/journeys/sweep_chromes_win_test.sh
# Requires: powershell.exe. Phase B also needs a chromium binary; without
# one it prints SKIP and phase A still decides.

set -uo pipefail

JROOT="$(cd "$(dirname "$0")" && pwd)"
SWEEP="$JROOT/sweep-chromes.sh"
PS1="$JROOT/sweep-chromes.ps1"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }

if ! command -v powershell.exe >/dev/null 2>&1; then
  echo "SKIP: no powershell.exe (this test covers the Windows reaper)"
  exit 0
fi

TMP="$(mktemp -d "${TMPDIR:-/tmp}/sweepwin-test.XXXXXX")"
DIR_ORPH=""
DIR_PROT=""
PROF_ORPH=""
PROF_PROT=""
PROT_PID=""
cleanup() {
  [ -n "$PROT_PID" ] && powershell.exe -NoProfile -Command \
    "Stop-Process -Id $PROT_PID -Force -ErrorAction SilentlyContinue" >/dev/null 2>&1
  # a just-killed chrome still holds its profile files for a moment (the
  # crashpad handler outlives the main by a beat), so one rm is not enough
  sleep 1
  for d in "$DIR_PROT" "$DIR_ORPH" "$TMP"; do
    [ -n "$d" ] || continue
    for _ in 1 2 3 4 5; do
      [ -e "$d" ] || break
      rm -rf "$d" 2>/dev/null
      sleep 1
    done
  done
  true
}
trap cleanup EXIT

# --- helpers ------------------------------------------------------------------

ps1() { # run the reaper itself; args after the script path
  powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$(cygpath -w "$PS1")" "$@" 2>&1
}

# pids of MAIN chrome processes whose cmdline names $1 (empty if none)
main_pids() {
  powershell.exe -NoProfile -Command \
    "(Get-CimInstance Win32_Process -Filter \"Name='chrome.exe'\" | Where-Object { \$_.CommandLine -like '*$1*' -and \$_.CommandLine -notmatch '--type=' }).ProcessId" \
    2>/dev/null | tr -d '\r' | tr '\n' ' '
}

# --- phase A: the decision ----------------------------------------------------

cat >"$TMP/table.json" <<'JSON'
[
 {"ProcessId":901,"ParentProcessId":1,"Name":"bash.exe","CommandLine":"x","CreationDate":"2026-11-11T09:00:00"},
 {"ProcessId":902,"ParentProcessId":1,"Name":"chrome.exe","CommandLine":"C:/chrome.exe --user-data-dir=C:/Temp/other","CreationDate":"2026-11-11T11:00:00"},
 {"ProcessId":904,"ParentProcessId":1,"Name":"bash.exe","CommandLine":"x","CreationDate":"2026-11-11T10:00:00"},

 {"ProcessId":100,"ParentProcessId":900,"Name":"chrome.exe","CommandLine":"C:/chrome.exe --remote-debugging-port=0 --user-data-dir=C:/Temp/agent-browser-chrome-aaa --headless=new","CreationDate":"2026-11-11T10:00:00"},
 {"ProcessId":101,"ParentProcessId":901,"Name":"chrome.exe","CommandLine":"C:/chrome.exe --remote-debugging-port=9333 --user-data-dir=C:/Temp/omega-journeys-chrome-profile-9333","CreationDate":"2026-11-11T10:00:00"},
 {"ProcessId":102,"ParentProcessId":902,"Name":"chrome.exe","CommandLine":"C:/chrome.exe --remote-debugging-port=0 --user-data-dir=C:/Temp/agent-browser-chrome-bbb","CreationDate":"2026-11-11T10:00:00"},
 {"ProcessId":103,"ParentProcessId":100,"Name":"chrome.exe","CommandLine":"C:/chrome.exe --type=renderer --user-data-dir=C:/Temp/agent-browser-chrome-aaa","CreationDate":"2026-11-11T10:01:00"},
 {"ProcessId":104,"ParentProcessId":900,"Name":"chrome.exe","CommandLine":"C:/chrome.exe --remote-debugging-port=0 --user-data-dir=C:/Temp/someone-elses-profile","CreationDate":"2026-11-11T10:00:00"},
 {"ProcessId":105,"ParentProcessId":903,"Name":"chrome.exe","CommandLine":"C:/chrome.exe --remote-debugging-port=9333 --user-data-dir=C:/Temp/omega-journeys-chrome-profile-9334","CreationDate":"2026-11-11T10:00:00"},
 {"ProcessId":106,"ParentProcessId":900,"Name":"chrome.exe","CommandLine":"C:/chrome.exe --user-data-dir=C:/Temp/dbg7-chrome-profile","CreationDate":"2026-11-11T10:00:00"},
 {"ProcessId":107,"ParentProcessId":904,"Name":"chrome.exe","CommandLine":"C:/chrome.exe --remote-debugging-port=0 --user-data-dir=C:/Temp/dbg7-chrome-profile","CreationDate":"2026-11-11T10:00:00"}
]
JSON

sw() { SWEEP_PROC_JSON="$(cygpath -w "$TMP/table.json")" ps1 "$@" | tr -d '\r'; }

out="$(sw)"
for pid in 100 102 105; do
  case "$out" in
    *"would kill orphan chrome pid=$pid "*) ok ;;
    *) bad "replay: pid $pid should be on the kill list. output: $out" ;;
  esac
done
for pid in 101 103 104 106 107; do
  case "$out" in
    *"pid=$pid "*) bad "replay: pid $pid must NOT be on the kill list. output: $out" ;;
    *) ok ;;
  esac
done
case "$out" in
  *"3 orphaned chrome(s) would be reaped"*) ok ;;
  *) bad "replay: summary should count 3. output: $out" ;;
esac
n=$(printf '%s\n' "$out" | grep -c 'would kill orphan chrome')
[ "$n" = 3 ] && ok || bad "replay: $n kill lines, want exactly 3. output: $out"
printf '%s\n' "$out" | grep -q 'reaped 3 orphaned' \
  && bad "replay: -DryRun must report only, never report a reap. output: $out" || ok

# --- phase B: the pipe, on real chromes ---------------------------------------

CHROME=""
for cand in "$HOME"/.hermes/tools/chromium-*/chrome-win64/chrome.exe \
            "$HOME"/.hermes/tools/chromium-*/chrome-linux/chrome.real \
            "$HOME"/AppData/Local/hermes/tools/chromium-*/chrome-win64/chrome.exe \
            "$HOME"/AppData/Local/hermes/tools/chromium-*/chrome-linux/chrome.real; do
  [ -x "$cand" ] && CHROME="$cand" && break
done
if [ -z "$CHROME" ]; then
  echo "SKIP phase B: no chromium under \$HOME/.hermes/tools (phase A passed)"
  printf '\nsweep_chromes_win_test: %s passed, %s failed\n' "$pass" "$fail"
  [ "$fail" = 0 ] || exit 1
  exit 0
fi
CW="$(cygpath -w "$CHROME")"
stamp="$$-$(date +%s)"
DIR_ORPH="${TMPDIR:-/tmp}/agent-browser-chrome-orphan-$stamp"
DIR_PROT="${TMPDIR:-/tmp}/agent-browser-chrome-live-$stamp"
PROF_ORPH="$(cygpath -w "$DIR_ORPH")"
PROF_PROT="$(cygpath -w "$DIR_PROT")"

# orphan: started by a PowerShell that exits at once, so the parent is gone
# by the time the reaper looks - the shape agent-browser leaves behind when
# its daemon dies. (Launching it straight from this bash does NOT orphan it:
# bash's fork emulation records the long-lived shell as the parent.)
powershell.exe -NoProfile -Command \
  "Start-Process -FilePath '$CW' -ArgumentList '--user-data-dir=$PROF_ORPH','--remote-debugging-port=0','--headless=new','--no-first-run','--disable-gpu','about:blank'" \
  >/dev/null 2>&1

orphan_pid=""
for _ in $(seq 1 40); do
  candidate="$(main_pids "agent-browser-chrome-orphan-$stamp")"
  if [ -n "$candidate" ]; then
    seen="$(ps1 -DryRun | tr -d '\r')"
    case "$seen" in
      *"agent-browser-chrome-orphan-$stamp"*) orphan_pid="$candidate"; break ;;
    esac
  fi
  sleep 0.5
done
if [ -z "$orphan_pid" ]; then
  bad "phase B: the orphan chrome never showed up as an orphan (last scan: ${seen:-<none>})"
else
  ok
fi

# protected: same profile shape, parent is THIS script - still alive
"$CW" --user-data-dir="$PROF_PROT" --remote-debugging-port=0 --headless=new \
  --no-first-run --disable-gpu about:blank >/dev/null 2>&1 &
PROT_PID=""
for _ in $(seq 1 40); do
  PROT_PID="$(main_pids "agent-browser-chrome-live-$stamp")"
  [ -n "$PROT_PID" ] && break
  sleep 0.5
done
if [ -z "$PROT_PID" ]; then
  bad "phase B: the protected chrome never started"
else
  ok
fi

if [ -n "$orphan_pid" ] && [ -n "$PROT_PID" ]; then
  out="$(bash "$SWEEP" 2>&1 | tr -d '\r')"
  rc=$?
  [ "$rc" = 1 ] && ok || bad "phase B: sweep exit $rc, want 1 (it reaped). output: $out"
  left="$(main_pids "agent-browser-chrome-orphan-$stamp")"
  [ -z "${left// /}" ] && ok || bad "phase B: the orphan chrome ($orphan_pid) survived the sweep"
  left="$(main_pids "agent-browser-chrome-live-$stamp")"
  [ -n "${left// /}" ] && ok || bad "phase B: the sweep killed a chrome with a LIVE parent"
else
  bad "phase B: skipped the kill round (no orphan or no protected chrome)"
fi

# the same replay twice: a reaper that cannot be re-run is a reaper that
# reaps the wrong thing on run two
out1="$(sw)"; out2="$(sw)"
[ "$out1" = "$out2" ] && ok || bad "phase A is not idempotent: '$out1' vs '$out2'"

printf '\nsweep_chromes_win_test: %s passed, %s failed\n' "$pass" "$fail"
[ "$fail" = 0 ] || exit 1
