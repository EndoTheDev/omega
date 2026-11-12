#!/usr/bin/env bash
# factory status - one glance at the whole pipeline (Endo's morning check)
# Usage: scripts/factory-status.sh  (run from anywhere)
#
# WHAT THIS IS FOR (card t_534d58c5, 2026-11-13): three cards died mid-pass
# leaving NOTHING a reader could look at. The board still said `running`, the
# worker's own output was empty, and the death only existed as a traceback in
# a profile log nobody had named. This script is the readout that makes a
# death legible: who is running, is the pid actually alive, what killed the
# last few runs, and which host conditions are on the books.
#
# The deaths were NOT a kill path - the audit in that card greps every factory
# script for taskkill/kill and finds none (the mechanism was the gateway's
# worker-receipt seam: `worker_receipt_timeout` in agent/runtime_session_store).
# Nothing here kills, reclaims or writes: it is a READ.
#
# Board/home resolution and the DB reader mirror scripts/factory-dispatch.sh
# (that script is the authority for the chain - keep the two in step). The
# sqlite3 CLI is absent on fleet Windows hosts, so boards are read through the
# python sqlite3 module, and a missing reader or board is a LOUD refusal: a
# status line printed from empty lists is the failure mode this script exists
# to prevent.
set -euo pipefail
cd "$(dirname "$0")/.."

root_of_env() {  # a trailing profiles/<name> segment names its ROOT home
  local h="${1//\\//}"
  if [ "$h" != "$(dirname "$h")" ] && [ "$(basename "$(dirname "$h")")" = profiles ]; then
    h="$(dirname "$(dirname "$h")")"
  fi
  printf '%s' "$h"
}
if [ -n "${HERMES_KANBAN_HOME:-}" ]; then ROOT="${HERMES_KANBAN_HOME//\\//}"
elif [ -n "${HERMES_HOME:-}" ]; then ROOT=$(root_of_env "$HERMES_HOME")
elif [ -n "${LOCALAPPDATA:-}" ]; then ROOT="${LOCALAPPDATA//\\//}/hermes"
else ROOT="$HOME/.hermes"; fi

if [ -n "${HERMES_KANBAN_DB:-}" ]; then DB="${HERMES_KANBAN_DB//\\//}"
else
  DB=""
  for cand in "${HERMES_KANBAN_BOARD:-}" "$(cat "$ROOT/kanban/current" 2>/dev/null || true)"; do
    [ -n "$cand" ] || continue
    case "$cand" in *[/*\\]*) continue ;; esac
    if [ "$cand" = default ]; then DB="$ROOT/kanban.db"; break; fi
    if [ -f "$ROOT/kanban/boards/$cand/kanban.db" ]; then DB="$ROOT/kanban/boards/$cand/kanban.db"; break; fi
  done
  [ -n "$DB" ] || DB="$ROOT/kanban.db"
fi

die() { echo "factory-status.sh: $1" >&2; exit 3; }
PY="${FS_PY:-}"
if [ -z "$PY" ]; then
  if command -v python3 >/dev/null 2>&1; then PY=python3
  elif command -v python >/dev/null 2>&1; then PY=python; fi
fi
[ -n "$PY" ] || die "no python on PATH - the sqlite3-module DB reader is unavailable"
"$PY" -c 'import sqlite3' >/dev/null 2>&1 || die "python '$PY' lacks the sqlite3 module"
[ -f "$DB" ] || die "board DB not found at $DB (root '$ROOT')"

# Same reader shape as factory-dispatch.sh DBPY (one process per statement).
DBPY='
import sqlite3, sys
sys.stdout.reconfigure(newline="\n")
path, mode, sql = sys.argv[1], sys.argv[2], sys.argv[3]
try:
    con = sqlite3.connect(path, timeout=10)
except sqlite3.Error as exc:
    sys.stderr.write("factory-status.sh: DB open failed: %s\n" % exc)
    sys.exit(3)
try:
    con.execute("PRAGMA query_only=ON")
    for row in con.execute(sql):
        sys.stdout.write("|".join("" if v is None else str(v) for v in row) + "\n")
except sqlite3.Error as exc:
    sys.stderr.write("factory-status.sh: DB read failed: %s\n" % exc)
    sys.exit(3)
finally:
    con.close()
'
dbq() { "$PY" -c "$DBPY" "$DB" q "$1"; }

age_hm() {  # seconds -> 12s | 4m05s | 3h12m | 2d04h   (-1/NULL -> ?)
  local s="$1"
  case "$s" in ''|-1|*[!0-9-]*) printf '?'; return ;; esac
  if [ "$s" -lt 60 ]; then printf '%ss' "$s"
  elif [ "$s" -lt 3600 ]; then printf '%dm%02ds' $((s / 60)) $((s % 60))
  elif [ "$s" -lt 86400 ]; then printf '%dh%02dm' $((s / 3600)) $((s % 3600 / 60))
  else printf '%dd%02dh' $((s / 86400)) $((s % 86400 / 3600)); fi
}

# Is the pid actually alive? POSITIVE EVIDENCE ONLY: a refused query answers
# `unknown` and never reads as a dead worker (the first live run of the guard's
# corpse readout fired on two live cards because `//FI` was refused by tasklist
# itself - a false corpse is worse than no readout). Windows: /FI /FO /NH stay
# intact under MSYS_NO_PATHCONV, and field 2 of the CSV row must EQUAL the pid.
pid_state() {  # pid -> alive | gone | unknown
  local p="$1" out f2
  case "$p" in ''|*[!0-9]*) printf unknown; return ;; esac
  if command -v tasklist >/dev/null 2>&1; then
    out=$(MSYS_NO_PATHCONV=1 tasklist /FI "PID eq $p" /FO CSV /NH 2>/dev/null) \
      || { printf unknown; return; }
    f2=$(printf '%s\n' "$out" | head -1 | cut -d, -f2 | tr -d '"')
    if [ "$f2" = "$p" ]; then printf alive; else printf gone; fi
    return
  fi
  if kill -0 "$p" 2>/dev/null; then printf alive; else printf gone; fi
}

echo "== factory $(date +%F\ %H:%M\ %Z)   board: $DB =="

# -- workers: who is running, and is the pid really alive ---------------------
# The board's heartbeat is the only liveness the dispatcher trusts; the pid is
# the only liveness the HOST can prove. A card answering `running` with a
# silent pid is the exact shape of a mid-pass death.
echo "-- workers --"
WORKERS=$(dbq "SELECT t.id, substr(replace(replace(t.title,'|','/'),char(10),' '),1,44), coalesce(t.current_run_id,''),
                      coalesce((SELECT r.profile FROM task_runs r WHERE r.id=t.current_run_id),''),
                      coalesce(t.worker_pid,''),
                      coalesce(CAST(strftime('%s','now') AS INTEGER) - t.last_heartbeat_at, -1),
                      coalesce(t.claim_expires,0)
               FROM tasks t WHERE t.status='running' ORDER BY t.created_at;")
if [ -z "$WORKERS" ]; then
  echo "  none"
else
  while IFS='|' read -r wid wtitle wrun wprof wpid whb wexp; do
    [ -n "$wid" ] || continue
    wstate=$(pid_state "$wpid")
    [ -z "$wpid" ] && wstate='no pid'
    if [ "$wexp" -gt 0 ] 2>/dev/null; then
      wexp_s=$(( wexp - $(date +%s) )); wexp_txt="claim $([ "$wexp_s" -lt 0 ] && echo "EXPIRED $(( -wexp_s ))s ago" || echo "in $(age_hm "$wexp_s")")"
    else
      wexp_txt="claim none"
    fi
    printf '  %-14s run %-6s %-8s hb %-7s pid %-7s %-8s %s\n' \
      "$wid" "${wrun:-?}" "${wprof:-?}" "$(age_hm "$whb")" "${wpid:-none}" "$wstate" "$wexp_txt"
    printf '                 %s\n' "$wtitle"
    # A GONE pid on a running card is a corpse: name it here, at the top, so a
    # death never needs a board query to notice.
    if [ "$wstate" = gone ]; then
      echo "                 ** CORPSE: the worker process is gone but the card is still running **"
      echo "                 ** cause: task_runs.error for run ${wrun:-?} | last tool calls: $ROOT/profiles/${wprof:-<profile>}/logs/agent.log **"
    fi
  done <<<"$WORKERS"
fi

# -- corpse: the last runs that ended badly -----------------------------------
# The platform already records this (task_runs.error is the run-level cause;
# the profile's agent.log carries the traceback and the last tool call). This
# section only puts those two pointers in one place so the next reader does not
# reconstruct a death from the DB by hand.
echo "-- recent bad endings (last 5) --"
# A garbage ended_at (ms-epoch leftovers, epoch 0) must not print as a
# 20000-day age: the row stays visible, the timestamp reads `?`.
BAD=$(dbq "SELECT coalesce(task_id,'?'), id, coalesce(profile,'?'), coalesce(status,'?'),
                  coalesce(outcome,'?'),
                  CASE WHEN ended_at BETWEEN 1577836800 AND CAST(strftime('%s','now') AS INTEGER)+86400
                       THEN strftime('%m-%d %H:%M', ended_at,'unixepoch','localtime') ELSE '?' END,
                  CASE WHEN ended_at BETWEEN 1577836800 AND CAST(strftime('%s','now') AS INTEGER)+86400
                       THEN CAST(strftime('%s','now') AS INTEGER) - ended_at ELSE -1 END,
                  substr(replace(replace(coalesce(error,''),char(10),' '),'|','/'),1,110)
           FROM task_runs
           WHERE ended_at IS NOT NULL
             AND (status IN ('crashed','failed') OR outcome IN ('crashed','failed'))
           ORDER BY ended_at DESC LIMIT 5;")
if [ -z "$BAD" ]; then
  echo "  none recorded"
else
  while IFS='|' read -r bid brun bprof bstat bout bats bage berr; do
    [ -n "$bid" ] || continue
    printf '  %-14s run %-6s %-7s %-8s ended %-12s (%-7s ago) %s\n' \
      "$bid" "$brun" "$bprof" "$bstat" "$bats" "$(age_hm "$bage")" "${berr:-<no error recorded>}"
    printf '                 traceback: %s/profiles/%s/logs/agent.log\n' "$ROOT" "$bprof"
  done <<<"$BAD"
fi

# -- host conditions that kill workers WITHOUT killing them -------------------
# Parked profile: the host multiplexer does not serve a parked profile, so a
# managed worker can never bind - 13 minutes alive, zero heartbeats, zero log
# lines, no kill (run 1400, 2026-11-12). Gateway churn: a restart mid-turn
# takes every worker with it (runs 1402/1403, 23:27-23:40). Both are readable
# here before they are diagnosed the hard way.
echo "-- host --"
PARKED=""
for marker in "$ROOT"/profiles/*/gateway.parked; do
  [ -e "$marker" ] || continue
  PARKED="$PARKED $(basename "$(dirname "$marker")")"
done
if [ -n "$PARKED" ]; then
  echo "  PARKED PROFILES:$PARKED  <- a parked profile never serves a worker (silent death; unpark before dispatching)"
else
  echo "  parked profiles: none"
fi
LEDGER="$ROOT/state/gateway.lifecycle.json"
if [ -f "$LEDGER" ]; then
  # phase/pid/started_at/prior_unclean_exit - one json read, no log scraping.
  "$PY" -c '
import json, sys, time
sys.stdout.reconfigure(newline="\n")   # windows: no \r\n
try:
    d = json.load(open(sys.argv[1], encoding="utf-8"))
except Exception as exc:
    print("  gateway ledger unreadable: %s" % exc); raise SystemExit(0)
ts = d.get("start_time") or 0
age = int(time.time() - ts) if ts else None
print("  gateway: phase=%s pid=%s up %s%s" % (
    d.get("phase"), d.get("pid"),
    ("%dm%02ds" % (age // 60, age % 60)) if age is not None else "?",
    "  <- previous life exited UNCLEANLY" if d.get("prior_unclean_exit") else ""))
' "$LEDGER"
else
  echo "  gateway ledger: absent ($LEDGER)"
fi

# -- board --------------------------------------------------------------------
echo "-- board --"
dbq "SELECT '  '||status||': '||COUNT(*) FROM tasks
     WHERE status IN ('running','ready','review','todo','blocked','scheduled','triage')
     GROUP BY status ORDER BY status;"
echo "  done: $(dbq "SELECT COUNT(*) FROM tasks WHERE status='done';")  archived: $(dbq "SELECT COUNT(*) FROM tasks WHERE status='archived';")"

# -- git ----------------------------------------------------------------------
# origin read is best-effort: an offline host must still print the local tip.
echo "-- git --"
git fetch -q origin dev master 2>/dev/null || true
AHEAD=$(git rev-list --count origin/master..origin/dev 2>/dev/null || echo '?')
echo "  dev:    $(git rev-parse --short origin/dev 2>/dev/null || git rev-parse --short dev 2>/dev/null || echo '?')"
echo "  master: $(git rev-parse --short origin/master 2>/dev/null || echo '?')  (dev ahead by $AHEAD)"
TAG=$(git tag -l 'v*-nightly.*' --sort=-v:refname | head -1)
if [ -n "$TAG" ]; then
  AGE_H=$(( ( $(date +%s) - $(git log -1 --format=%ct "$TAG") ) / 3600 ))
  SOAK="$(age_hm $(( AGE_H * 3600 )))"
  [ "$AGE_H" -ge 48 ] && SOAK="$SOAK  <- soak-ready"
  echo "  latest nightly: $TAG  (age $SOAK)"
else
  echo "  latest nightly: none yet"
fi
STABLE=$(git tag -l 'v*.*.*' | grep -v -- '-' | sort -V | tail -1 || true)
echo "  stable: ${STABLE:-none}"

# -- lane ---------------------------------------------------------------------
echo "-- lane --"
HEALTH=$(curl -s --max-time 4 http://127.0.0.1:8098/health || echo '{"status":"unreachable"}')
echo "  :8098 $(echo "$HEALTH" | grep -o '"version":"[^"]*"' | head -1 || true)"
echo "  drift gate: $AHEAD commits ahead of master (red while > 10, expected until first soak ff)"
