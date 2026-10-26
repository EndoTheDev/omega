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
#   - Profile-shape match: cmdline must name "*chrome-profile*", which
#     covers the journey shape (omega-journeys-chrome-profile-<port>) and
#     the worker debug shape (dbgN-chrome-profile). A chrome outside this
#     naming never matches, whatever else it is.
#
# Usage:
#   scripts/journeys/sweep-chromes.sh        # print what it found/killed
#   scripts/journeys/sweep-chromes.sh -q     # silent unless it killed
#
# Exit: 0 none killed, 1 orphans killed (visible in cron/CI logs).

set -u

quiet=0
[ "${1:-}" = "-q" ] && quiet=1

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
    *chrome-profile*) ;;
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