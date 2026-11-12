#!/usr/bin/env bash
# factory-status_test.sh - runnable checks for scripts/factory-status.sh.
# Run: bash scripts/factory-status_test.sh
#
# The script exists because a mid-pass death left NOTHING to read (card
# t_534d58c5): the board said `running`, the worker's output was empty, and the
# cause lived in a profile log nobody had named. These checks pin the four
# things that make a death legible - a worker line with a proven pid state, a
# CORPSE marker when the pid is gone, the last bad runs with their error and
# traceback pointer, and the host conditions (parked profile, unclean gateway
# death) that kill workers without killing them.
#
# The refusal leg matters as much as the rest: the old script read a hardcoded
# `$HOME/.hermes` board with the sqlite3 CLI, neither of which exists on a fleet
# Windows host, so it printed a header and nothing else. A status readout
# computed from empty lists is the failure mode this file guards.
#
# Fixtures are a throwaway home + board (never the real one); DB access is the
# python sqlite3 module because the sqlite3 CLI is absent on fleet hosts, and
# fake-home paths are passed to python as native (C:/...) paths.
set -uo pipefail

SCRIPT="$(cd "$(dirname "$0")" && pwd)/factory-status.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }

PY=$(command -v python3 || command -v python)
[ -n "$PY" ] || { echo "factory-status_test: no python on PATH" >&2; exit 1; }
mixpath() { cygpath -m "$1" 2>/dev/null || printf '%s' "$1"; }
sqlex() {
  "$PY" -c '
import sqlite3, sys
con = sqlite3.connect(sys.argv[1], timeout=10)
con.executescript(sys.argv[2])
con.commit()
' "$1" "$2"
}

FHOME=$(mktemp -d)
mkdir -p "$FHOME/kanban/boards/project-omega" "$FHOME/profiles/cody/logs" \
         "$FHOME/profiles/vera/logs" "$FHOME/state"
printf 'project-omega\n' >"$FHOME/kanban/current"
: >"$FHOME/profiles/cody/gateway.parked"
printf '{"phase": "running", "pid": 4242, "start_time": %s, "prior_unclean_exit": true}\n' \
  "$(date +%s)" >"$FHOME/state/gateway.lifecycle.json"
FDB_N="$(mixpath "$FHOME")/kanban/boards/project-omega/kanban.db"

# A live pid the host can prove (this shell) and a pid that is really gone.
LIVE_PID=$(cat /proc/$$/winpid 2>/dev/null || echo $$)
sleep 60 &VICTIM=$!
VICTIM_PID=$(cat "/proc/$VICTIM/winpid" 2>/dev/null || echo "$VICTIM")
kill "$VICTIM" 2>/dev/null || true
wait "$VICTIM" 2>/dev/null || true
NOW=$(date +%s)

sqlex "$FDB_N" "CREATE TABLE tasks(id TEXT, status TEXT, title TEXT DEFAULT '',
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT,
  worker_pid INTEGER, last_heartbeat_at INTEGER, current_run_id INTEGER);
CREATE TABLE task_runs(id INTEGER PRIMARY KEY, task_id TEXT, profile TEXT, status TEXT,
  outcome TEXT, ended_at INTEGER, error TEXT);
INSERT INTO tasks VALUES('t_dead','running','the dead one',NULL,$((NOW + 600)),5,'2026-11-12T19:00',$VICTIM_PID,$((NOW - 95)),4242);
INSERT INTO tasks VALUES('t_alive','running','the live one',NULL,$((NOW + 600)),5,'2026-11-12T19:01',$LIVE_PID,$((NOW - 5)),4243);
INSERT INTO tasks VALUES('t_ready','ready','queued work',NULL,NULL,5,'2026-11-12T19:02',NULL,NULL,NULL);
INSERT INTO task_runs VALUES(4242,'t_dead','cody','running','running',NULL,NULL);
INSERT INTO task_runs VALUES(4243,'t_alive','vera','running','running',NULL,NULL);
INSERT INTO task_runs VALUES(4241,'t_dead','cody','crashed','failed',$((NOW - 600)),'managed_worker_failed: worker died mid-stream (exit 1, empty output)');"

run_status() {  # $1 = extra env assignments (may be '')
  ( cd "$(dirname "$SCRIPT")/.." && env HOME="$FHOME" HERMES_HOME= \
      HERMES_KANBAN_HOME="$(mixpath "$FHOME")" HERMES_KANBAN_DB= HERMES_KANBAN_BOARD= \
      bash "$SCRIPT" 2>&1 )
}

out=$(run_status ""); rc=$?
[ "$rc" -eq 0 ] && ok || bad "T1: status exited $rc on a healthy fixture (out: $out)"

# T2: both running cards get a worker line, with the pid verdict the host gave.
printf '%s' "$out" | grep -qE "t_dead +run 4242 +cody +hb [0-9]+m[0-9]+s +pid $VICTIM_PID +gone" \
	&& ok || bad "T2a: the dead worker's line is missing/wrong (out: $out)"
printf '%s' "$out" | grep -qE "t_alive +run 4243 +vera +hb [0-9]+s +pid $LIVE_PID +alive" \
	&& ok || bad "T2b: the LIVE worker's line is missing/wrong (out: $out)"

# T3: the corpse marker fires for the dead card only, and names the sources.
printf '%s' "$out" | grep -q 'CORPSE: the worker process is gone but the card is still running' \
	&& ok || bad "T3a: no CORPSE marker for a running card whose pid is gone (out: $out)"
n=$(printf '%s' "$out" | grep -c 'CORPSE:')
[ "$n" -eq 1 ] && ok || bad "T3b: expected exactly 1 CORPSE marker (dead card only), got $n - a false corpse makes the readout worthless"
printf '%s' "$out" | grep -qF "task_runs.error for run 4242" \
	&& ok || bad "T3c: the corpse marker named no run-level cause (out: $out)"
printf '%s' "$out" | grep -qF "profiles/cody/logs/agent.log" \
	&& ok || bad "T3d: the corpse marker named no traceback path (out: $out)"

# T4: bad endings carry the error text and a per-run traceback pointer.
printf '%s' "$out" | grep -qF 'run 4241' \
	&& ok || bad "T4a: the crashed run is missing from -- recent bad endings -- (out: $out)"
printf '%s' "$out" | grep -qF 'worker died mid-stream' \
	&& ok || bad "T4b: the crashed run printed no error text (out: $out)"
printf '%s' "$out" | grep -qF 'traceback:' \
	&& ok || bad "T4c: no traceback pointer (out: $out)"

# T5/T6: the two host conditions that kill workers without a kill.
printf '%s' "$out" | grep -q 'PARKED PROFILES: cody' \
	&& ok || bad "T5: the parked-profile marker was not surfaced (out: $out)"
printf '%s' "$out" | grep -q 'previous life exited UNCLEANLY' \
	&& ok || bad "T6: the unclean gateway death in the lifecycle ledger was not surfaced (out: $out)"

# T7: a missing board must refuse LOUDLY, never print a healthy-looking header.
out=$( cd "$(dirname "$SCRIPT")/.." && env HOME="$(mktemp -d)" HERMES_HOME= \
        HERMES_KANBAN_HOME="$(mixpath "$(mktemp -d)")" HERMES_KANBAN_DB= HERMES_KANBAN_BOARD= \
        bash "$SCRIPT" 2>&1 ); rc=$?
[ "$rc" -ne 0 ] && ok || bad "T7a: a missing board exited 0 - verdicts from empty lists"
printf '%s' "$out" | grep -q 'board DB not found' \
	&& ok || bad "T7b: the refusal did not name the missing board (out: $out)"

# T8: no usable reader must refuse too (the sqlite3 CLI is not a fallback).
out=$( cd "$(dirname "$SCRIPT")/.." && env HOME="$FHOME" HERMES_HOME= FS_PY=/nonexistent/python \
        HERMES_KANBAN_HOME="$(mixpath "$FHOME")" HERMES_KANBAN_DB= HERMES_KANBAN_BOARD= \
        bash "$SCRIPT" 2>&1 ); rc=$?
[ "$rc" -ne 0 ] && ok || bad "T8: a dead python refused with exit 0"

rm -rf "$FHOME"

echo
echo "factory-status_test: $pass ok, $fail fail"
[ "$fail" -eq 0 ]
