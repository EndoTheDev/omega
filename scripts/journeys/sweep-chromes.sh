#!/bin/bash
# sweep-chromes.sh - reap orphaned headless chromiums.
#
# Backstop for the chrome-death contract in run.sh. run.sh kills its own
# chrome on every trappable exit path; nothing can trap a SIGKILLed (or
# crashed) run.sh, and worker-side debug chromes have no owner once the
# worker session ends. Both corpses look the same: a chrome.real main
# process reparented to init (ppid 1) wearing a journey or debug profile
# dir. This script kills exactly those, by pid - never by pattern text.
#
# Proven live 2026-10-27: 37 orphaned chromes held a Pi 5 at load 10 and
# red-flaked every suite run (E5002 console errors, 90s stream-wait
# timeouts, page-open failures). After the kills the identical suite went
# 24/24 green.
#
# Safety:
#   - /proc/*/comm scan, NOT pgrep -f: pgrep -f self-matches the
#     invoking shell (proven twice on 2026-10-27, one self-inflicted
#     SIGKILL). Filter by comm == chrome.real, never by cmdline text
#     across the whole system.
#   - Only MAIN processes are matched (they carry --remote-debugging-port;
#     render children carry --type=). Children die with the main, so no
#     process-group kills here: a debug chrome launched without setsid
#     shares its launcher's group, and a group kill would hit innocent
#     neighbors.
#   - Only ppid==1 processes are matched: a live run.sh (or a live worker
#     shell) is a parent and protects its chrome. Concurrent suites and
#     in-use debug chromes are never touched.
#   - Profile-shape match: cmdline must name a journey/debug/agent-browser
#     profile dir - "*chrome-profile*" covers omega-journeys-chrome-profile-
#     <port> and dbgN-chrome-profile; "*agent-browser-chrome-*" covers the
#     auto-launch browser that agent-browser itself starts and never reaps
#     when its daemon dies (24 such mains with dead parents were holding 314
#     chrome.exe on winbtw, found 2026-11-11).
#     Evidence behind that shape: WINDOWS-PROVEN (Win32_Process, the ps1
#     half, live reaped). The /proc scan below matches the same shape string,
#     but nobody has observed agent-browser's Pi profile naming - so the
#     Pi/Linux side is pattern-extended, not host-proven. A chrome outside
#     these naming shapes never matches, whatever else it is.
#
# NOT this script's business: an IDLE agent-browser DAEMON with the chrome it
# holds. That chrome's parent - the daemon - is alive, so the dead-parent rule
# here correctly leaves the pair alone; the daemon outlives its client by
# design, which is exactly why a chrome reaper must not judge it. Those are
# closed at the release gate by scripts/journeys/sweep-daemons.sh (card
# t_6485e22e), which verifies the run lock, the attached-client probe and an
# idle window before it closes anything.
#
# Windows (MSYS): /proc exists but holds only MSYS processes, so the scan
# below finds nothing there. sweep-chromes.ps1 does the same job off
# Win32_Process - same shapes, same "main process" rule, same live-parent
# protection, plus a pid-recycling guard the /proc version gets for free
# (a dead parent's pid can be worn by a live stranger; a real parent always
# predates its child). It runs before the lock block on purpose: in
# mkdir-lock mode run.sh exports no fd 9 for this script to inherit, and a
# sweep that can only kill a chrome whose parent is dead cannot collide
# with a live suite.
#
# Usage:
#   scripts/journeys/sweep-chromes.sh        # print what it found/killed
#   scripts/journeys/sweep-chromes.sh -q     # silent unless it killed
#
# Exit: 0 none killed, 1 orphans killed (visible in cron/CI logs).

set -u

quiet=0
[ "${1:-}" = "-q" ] && quiet=1

# --- Windows / MSYS: hand the whole job to the PowerShell half --------------
# MSYS does mount a /proc, but it only sees MSYS's own processes - a native
# chrome.exe is not in it, so the scan below finds nothing there (proven on
# winbtw: 314 chrome.exe, 0 matches). Detect the host, not the tree.
case "$(uname -s 2>/dev/null)" in
  MINGW*|MSYS*|CYGWIN*) on_windows=1 ;;
  *) on_windows=0 ;;
esac
if [ "$on_windows" = 1 ]; then
  jdir="$(cd "$(dirname "$0")" && pwd)"
  ps1="$jdir/sweep-chromes.ps1"
  if [ ! -f "$ps1" ] || ! command -v powershell.exe >/dev/null 2>&1; then
    [ "$quiet" = "1" ] || printf 'sweep-chromes: no Windows reaper next to this script - nothing to scan\n'
    exit 0
  fi
  wps1="$ps1"
  command -v cygpath >/dev/null 2>&1 && wps1="$(cygpath -w "$ps1")"
  if [ "$quiet" = "1" ]; then
    exec powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$wps1" -Quiet
  fi
  exec powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$wps1"
fi

# --- run lock ----------------------------------------------------------------
# run.sh flocks /tmp/omega-journeys.lock on fd 9 for its whole suite body.
# Called from a live suite (fd 9 inherited, pointing at the lock file),
# proceeding is the point: the suite-start sweep reaps orphans before the
# recipes run. A standalone sweep (cron, pre-merge, worker) must never fight
# a live suite: if the lock is held by someone else, stand down entirely.
# A sweep that takes the lock holds it for the reaping, so a suite starting
# mid-sweep queues instead of racing it.
JOURNEY_LOCK="${JOURNEY_LOCK:-/tmp/omega-journeys.lock}"
if [ "$(readlink "/proc/$$/fd/9" 2>/dev/null)" = "$JOURNEY_LOCK" ]; then
  : # inherited the live suite's lock fd - sweep on
else
  exec 9>>"$JOURNEY_LOCK"
  if ! flock -n 9; then
    holder="$(cat "${JOURNEY_LOCK}.pid" 2>/dev/null || echo '?')"
    [ "$quiet" = "1" ] || printf 'sweep-chromes: journey suite holds the run lock (pid %s) - skipping\n' "$holder"
    exec 9>&-
    exit 0
  fi
fi

count=0
mains=""

for p in /proc/[0-9]*; do
  pid="${p#/proc/}"
  [ "$(cat "$p/comm" 2>/dev/null)" = "chrome.real" ] || continue
  # "chrome.real" has no spaces, so stat fields 4 (ppid) and 5 (pgrp) are
  # unshifted by the comm.
  ppid="$(awk '{print $4}' "$p/stat" 2>/dev/null)"
  [ "$ppid" = "1" ] || continue
  cmd="$(tr '\0' ' ' <"$p/cmdline" 2>/dev/null)"
  case "$cmd" in
    *--remote-debugging-port*) ;;
    *) continue ;;
  esac
  case "$cmd" in
    *chrome-profile*|*agent-browser-chrome-*) ;;
    *) continue ;;
  esac
  mains="$mains $pid"
done

for pid in $mains; do
  ud="$(tr '\0' '\n' </proc/$pid/cmdline 2>/dev/null | grep '^--user-data-dir=' | head -1)"
  count=$((count + 1))
  if [ "$quiet" = "0" ]; then
    printf 'sweep-chromes: killing orphan chrome pid=%s %s\n' "$pid" "${ud#--user-data-dir=}"
  fi
  kill -TERM "$pid" 2>/dev/null
done

# give chromium a moment to take its children down, then finish the job
if [ -n "$mains" ]; then
  sleep 3
  for pid in $mains; do
    kill -KILL "$pid" 2>/dev/null
    # any chrome.real child that outlived its main
    for p in /proc/[0-9]*; do
      cpid="${p#/proc/}"
      [ "$(awk '{print $4}' "$p/stat" 2>/dev/null)" = "$pid" ] || continue
      [ "$(cat "$p/comm" 2>/dev/null)" = "chrome.real" ] || continue
      kill -KILL "$cpid" 2>/dev/null
    done
  done
fi

if [ "$count" -gt 0 ]; then
  printf 'sweep-chromes: reaped %s orphaned chrome(s)\n' "$count"
  exit 1
fi
[ "$quiet" = "1" ] || printf 'sweep-chromes: no orphaned chrome.real processes\n'
exit 0