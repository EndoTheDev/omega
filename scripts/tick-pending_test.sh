#!/usr/bin/env bash
# tick-pending_test.sh - runnable checks for scripts/tick-pending.sh.
# Run: bash scripts/tick-pending_test.sh
#
# The script exists because the tick re-reported a settled `PENDING (Endo)` flag
# twelve times and then blocked a correctly-ready card on it (card t_f7666159):
# the list was derived from the tick's own reports, not from the board. These
# checks pin the two directions that matter - a decided marker is NEVER pending,
# and a genuinely owed word ALWAYS is - plus the shapes that kept the loop
# alive: a ruling written as "verbatim from the vault" (decided, no SETTLED
# word), a past ruling inside a block reason ("Endo ruling 2026-11-15", landed),
# and a BLOB body (8 live rows carry one; a reader that assumes TEXT dies on
# them).
#
# Fixtures are a throwaway home + board, never the real one. DB access is the
# python sqlite3 module (the sqlite3 CLI is absent on fleet Windows hosts) and
# fake-home paths are passed to python as native (C:/...) paths.
set -uo pipefail

SCRIPT="$(cd "$(dirname "$0")" && pwd)/tick-pending.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }

PY=$(command -v python3 || command -v python)
[ -n "$PY" ] || { echo "tick-pending_test: no python on PATH" >&2; exit 1; }
mixpath() { cygpath -m "$1" 2>/dev/null || printf '%s' "$1"; }
sqlex() {
  "$PY" -c '
import sqlite3, sys
con = sqlite3.connect(sys.argv[1], timeout=10)
con.executescript(sys.argv[2])
con.commit()
con.close()
' "$1" "$2"
}

TPHOME=$(mktemp -d)
mkdir -p "$TPHOME/kanban/boards/project-omega"
printf 'project-omega\n' >"$TPHOME/kanban/current"
TDB_N="$(mixpath "$TPHOME")/kanban/boards/project-omega/kanban.db"

# The fixture board: one card per shape, so a verdict below names its own case.
# Every marker line is a REAL line start (that is the discriminator against
# prose that merely mentions the marker - see t_prose).
sqlex "$TDB_N" "
CREATE TABLE tasks(id TEXT, status TEXT, title TEXT DEFAULT '', body TEXT DEFAULT '', created_at INTEGER DEFAULT 0);
CREATE TABLE task_comments(id INTEGER PRIMARY KEY AUTOINCREMENT, task_id TEXT, author TEXT DEFAULT 'default',
  body TEXT NOT NULL, created_at INTEGER NOT NULL);
CREATE TABLE task_events(id INTEGER PRIMARY KEY AUTOINCREMENT, task_id TEXT, run_id INTEGER, kind TEXT NOT NULL,
  payload TEXT, created_at INTEGER NOT NULL);

-- 1. the t_d1fe01da shape: the ruling landed and a comment carries the marker
INSERT INTO tasks VALUES('t_settled_ready','ready','design ruling: the fork','PROBLEM: a contract change.

WORK:
1. both halves.',100);
INSERT INTO task_comments(task_id,author,body,created_at) VALUES('t_settled_ready','default',
  'RULING (2026-11-17): SETTLED - option C, both halves, one card. Do not re-report this as a pending ruling - the ruling exists.',500);

-- 2. the same word inside the marker parens, body-only
INSERT INTO tasks VALUES('t_settled_body','ready','body marker','DESIGN RULING (settled by the maintainer, 2026-11-17): option C - both fixes, one card.',101);

-- 3. a ruling written as a DECISION with no SETTLED word at all
INSERT INTO tasks VALUES('t_decided_vault','ready','verbatim ruling','RULING (Endo, 2026-11-12, verbatim from the vault): \"(1) MODULES SECTION: the modules tab keeps its folds.\"',102);

-- 4. triage asking for the word
INSERT INTO tasks VALUES('t_triage_ask','triage','unruled design fork','PROBLEM: two shippable shapes.

DESIGN: needs Endo''s word before a worker touches the surface.',103);

-- 5. scheduled, park reason gated on a tag word (the starter-card shape)
INSERT INTO tasks VALUES('t_sched_tag','scheduled','starter card','BODY: cut after 0.17.3.',104);
INSERT INTO task_events(task_id,kind,payload,created_at) VALUES('t_sched_tag','scheduled',
  '{\"reason\": \"0.17.4 starter - un-schedules at the 0.17.3 tag word\"}',600);

-- 6. scheduled, no reason at all: parked, nothing owed to him
INSERT INTO tasks VALUES('t_sched_plain','scheduled','parked with no reason','BODY: waits on a sibling card.',105);
INSERT INTO task_events(task_id,kind,payload,created_at) VALUES('t_sched_plain','scheduled','{\"reason\": null}',601);

-- 7. blocked, reason names a word still OWED
INSERT INTO tasks VALUES('t_blocked_owed','blocked','blocked on his word','BODY: the fold question.',106);
INSERT INTO task_events(task_id,kind,payload,created_at) VALUES('t_blocked_owed','blocked',
  '{\"reason\": \"blocked: needs his word on whether the dock folds before the sort lands\"}',602);

-- 8. blocked, WINDOW-gated on a ruling that already landed (the live shape)
INSERT INTO tasks VALUES('t_blocked_window','blocked','window gated','BODY: deferred.',107);
INSERT INTO task_events(task_id,kind,payload,created_at) VALUES('t_blocked_window','blocked',
  '{\"reason\": \"window-gated (Endo ruling 2026-11-15): post-0.17.3 patch window only, after the provider merge lands. Do not dispatch into the draining window.\"}',603);

-- 9. the skill marker for an owed question, on a READY card
INSERT INTO tasks VALUES('t_pendmark','ready','asks a question','PENDING (Endo): which lane does the export command ride?',108);

-- 10. a ruling block that explicitly owes the verdict
INSERT INTO tasks VALUES('t_unsettled_ruling','ready','no verdict yet','RULING (owed, 2026-11-18): do we fold the dock before the sort?',109);

-- 11. prose that MENTIONS the marker mid-sentence: not a marker
INSERT INTO tasks VALUES('t_prose','ready','prose about markers','A card whose body or comments carry a RULING (...) marker reading SETTLED is never pending.',110);

-- 12. a DONE card with a decided marker: history, no line
INSERT INTO tasks VALUES('t_done_decided','done','shipped work','RULING (2026-11-10): HOLD - do not merge; work lives outside this repo.',111);

-- 13. a DONE card whose DECISION is still open: pending, never silent
INSERT INTO tasks VALUES('t_done_open','done','shipped with an open fork','WORK: built and merged.

PENDING (Endo): does the second tab ride the same fold?',112);

-- 14. archived: invisible even with an owed marker
INSERT INTO tasks VALUES('t_archived_owed','archived','archived','PENDING (Endo): never mind.',113);

-- 15. REOPENED: a settled comment at 500, a newer owed comment at 900. The
-- newest marker wins (round-1 review finding) - this is the card a re-ask
-- lands on the first time a worker asks again on a ruled card.
INSERT INTO tasks VALUES('t_reopened','ready','ruled, then re-asked','BODY: option C shipped.',115);
INSERT INTO task_comments(task_id,author,body,created_at) VALUES('t_reopened','default',
  'RULING (2026-11-17): SETTLED - option C, both halves.',500);
INSERT INTO task_comments(task_id,author,body,created_at) VALUES('t_reopened','default',
  'PENDING (Endo): new evidence - does option C still hold for the second tab?',900);

-- 16. the same shape where the settled marker is in the BODY (body sorts
-- oldest: it has no mtime to order)
INSERT INTO tasks VALUES('t_body_then_ask','ready','body ruled, comment re-asks',
  'RULING (2026-11-17): SETTLED - option A.',116);
INSERT INTO task_comments(task_id,author,body,created_at) VALUES('t_body_then_ask','default',
  'PENDING (Endo): the fold question reopened by the merge.',901);

-- 17. the CONTROL direction: asked first, answered later. Settled still wins.
INSERT INTO tasks VALUES('t_ask_then_settled','ready','asked, then answered',
  'PENDING (Endo): does the dock fold before the sort?',117);
INSERT INTO task_comments(task_id,author,body,created_at) VALUES('t_ask_then_settled','default',
  'RULING (2026-11-17): SETTLED - option B, fold first.',902);
"

# A BLOB body carrying a settled marker (8 live rows carry BLOB bodies; a TEXT
# assumption raises TypeError and the whole derivation dies).
"$PY" -c '
import sqlite3, sys
con = sqlite3.connect(sys.argv[1], timeout=10)
con.execute("INSERT INTO tasks(id,status,title,body,created_at) VALUES(?,?,?,?,?)",
            ("t_blob_settled", "ready", "blob body",
             "RULING (2026-11-17): SETTLED - blob bodies read too.".encode("utf-8"), 114))
con.commit(); con.close()
' "$TDB_N"

run_list() {  # $1 = extra env assignments (may be '')
  ( cd "$(dirname "$SCRIPT")" && env HOME="$TPHOME" HERMES_HOME= \
      HERMES_KANBAN_HOME="$(mixpath "$TPHOME")" HERMES_KANBAN_DB= HERMES_KANBAN_BOARD= \
      bash "$SCRIPT" 2>&1 )
}
run_check() {  # $1 = card id
  ( cd "$(dirname "$SCRIPT")" && env HOME="$TPHOME" HERMES_HOME= \
      HERMES_KANBAN_HOME="$(mixpath "$TPHOME")" HERMES_KANBAN_DB= HERMES_KANBAN_BOARD= \
      bash "$SCRIPT" --check "$1" 2>&1 )
}
has() { grep -qF "$2" <<<"$1"; }
hasnt() { ! grep -qF "$2" <<<"$1"; }

out=$(run_list); rc=$?
[ "$rc" -eq 0 ] && ok || bad "T1: list mode exited $rc on a healthy fixture (out: $out)"

# -- the settled veto (the 12-report loop) ----------------------------------
has "$out" "SETTLED t_settled_ready" && ok || bad "T2a: a comment-carried SETTLED marker did not print SETTLED (out: $out)"
has "$out" "SETTLED t_settled_body" && ok || bad "T2b: a body SETTLED marker did not print SETTLED (out: $out)"
hasnt "$out" "PENDING t_settled_ready" && ok || bad "T2c: a settled card was ALSO printed as pending - that is the loop"
hasnt "$out" "PENDING t_settled_body" && ok || bad "T2d: a settled card was listed as pending"
has "$out" "SETTLED t_blob_settled" && ok || bad "T2e: a BLOB body killed the marker read (out: $out)"
# a decision recorded without the word SETTLED is still a decision
has "$out" "SETTLED t_decided_vault" && ok || bad "T2f: a 'verbatim from the vault' ruling read as owed - the 14-card false-pending class"
hasnt "$out" "t_done_decided" && ok \
  || bad "T2g: a decided DONE card printed a line - history is not news, and a HOLD marker is still decided"
hasnt "$out" "PENDING t_done_decided" && ok || bad "T2h: a decided done card was re-asked"

# -- the four pending shapes ----------------------------------------------
has "$out" "PENDING t_triage_ask triage card asks for a ruling" && ok \
  || bad "T3a: a triage card asking for the word was not pending (out: $out)"
has "$out" "PENDING t_sched_tag scheduled, park reason gates on a tag/word trigger" && ok \
  || bad "T3b: a tag-gated scheduled card was not pending (out: $out)"
hasnt "$out" "PENDING t_sched_plain" && ok \
  || bad "T3c: a parked card with no gate read as owed - a bare 'scheduled' is not his word"
has "$out" "PENDING t_blocked_owed blocked, reason names a word still owed" && ok \
  || bad "T3d: a block reason naming an owed word was not pending (out: $out)"
hasnt "$out" "PENDING t_blocked_window" && ok \
  || bad "T3e: a window-gated card behind a LANDED ruling read as owed (out: $out)"
has "$out" "PENDING t_pendmark PENDING (Endo) marker" && ok \
  || bad "T3f: the skill's owed-question marker did not read as pending (out: $out)"
has "$out" "PENDING t_unsettled_ruling ruling marker with no verdict" && ok \
  || bad "T3g: a RULING block that owes its verdict read as decided (out: $out)"
has "$out" "PENDING t_done_open" && ok \
  || bad "T3h: an open decision on a DONE card was swallowed - the finished-build case"

# -- marker recency: the newest marker owns the state ---------------------
# A settled marker is NOT final by construction. A later owed marker wins
# (round-1 review finding: an old SETTLED vetoed a newer `PENDING (Endo):`
# re-ask, hiding it from BOTH the report and --check).
has "$out" "PENDING t_reopened PENDING (Endo) marker" && ok \
  || bad "T9a: a newer owed comment did not re-open a settled card (out: $out)"
hasnt "$out" "SETTLED t_reopened" && ok \
  || bad "T9b: the re-opened card still printed SETTLED - the doubly-suppressed shape"
has "$out" "PENDING t_body_then_ask PENDING (Endo) marker" && ok \
  || bad "T9c: a comment re-ask lost to a body settled marker (out: $out)"
hasnt "$out" "SETTLED t_body_then_ask" && ok || bad "T9d: the body-ruled re-ask printed SETTLED"
has "$out" "SETTLED t_ask_then_settled" && ok \
  || bad "T9e: the control direction (asked, then answered) did not settle (out: $out)"
hasnt "$out" "PENDING t_ask_then_settled" && ok \
  || bad "T9f: an answered question stayed pending - recency runs both ways"
out=$(run_check t_reopened); rc=$?
[ "$rc" -eq 1 ] && ok || bad "T9g: --check on the re-opened card exited $rc, not 1 (a block decision would read it as settled)"
cout=$(run_check t_body_then_ask); rc=$?
[ "$rc" -eq 1 ] && ok || bad "T9h: --check on the body-ruled re-ask exited $rc, not 1"
cout=$(run_check t_ask_then_settled); rc=$?
[ "$rc" -eq 0 ] && ok || bad "T9i: --check on the answered card exited $rc, not 0"
out=$(run_list)  # T4/T5 below read the LIST output; --check clobbered it above

# -- what must NOT be a marker --------------------------------------------
hasnt "$out" "SETTLED t_prose" && ok || bad "T4a: prose mentioning the marker counted as one"
hasnt "$out" "NOMARKER" && ok || bad "T4b: NOMARKER belongs to --check only"
hasnt "$out" "t_archived_owed" && ok || bad "T4c: an archived card appeared in the derivation"

# -- summary line and counts ----------------------------------------------
has "$out" "TICK-PENDING: 8 pending, 5 settled, 18 cards read" && ok \
  || bad "T5: the summary line is wrong or missing (out: $out)"

# -- --check: 0 settled, 1 pending, 2 no evidence -------------------------
out=$(run_check t_settled_ready); rc=$?
[ "$rc" -eq 0 ] && ok || bad "T6a: --check on a settled marker exited $rc, not 0"
has "$out" "SETTLED t_settled_ready" && ok || bad "T6b: --check printed no verdict line (out: $out)"
out=$(run_check t_pendmark); rc=$?
[ "$rc" -eq 1 ] && ok || bad "T6c: --check on an owed marker exited $rc, not 1"
out=$(run_check t_prose); rc=$?
[ "$rc" -eq 2 ] && ok || bad "T6d: --check with no marker exited $rc, not 2 (no evidence is never settled)"
grep -q '^NOMARKER t_prose ' <<<"$out" && ok || bad "T6e: --check said neither settled nor pending (out: $out)"
out=$(run_check t_nosuchcard); rc=$?
[ "$rc" -eq 3 ] && ok || bad "T6f: --check on an unknown id exited $rc, not 3"
grep -q 'no card t_nosuchcard' <<<"$out" && ok || bad "T6g: the unknown-id refusal named no id (out: $out)"

# -- refusals: no board, no reader ---------------------------------------
out=$( cd "$(dirname "$SCRIPT")" && env HOME="$(mktemp -d)" HERMES_HOME= \
        HERMES_KANBAN_HOME="$(mixpath "$(mktemp -d)")" HERMES_KANBAN_DB= HERMES_KANBAN_BOARD= \
        bash "$SCRIPT" 2>&1 ); rc=$?
[ "$rc" -eq 3 ] && ok || bad "T7a: a missing board exited $rc, not 3"
grep -q 'board DB not found' <<<"$out" && ok || bad "T7b: the missing board was not named (out: $out)"
out=$( cd "$(dirname "$SCRIPT")" && env HOME="$TPHOME" HERMES_HOME= TP_PY=/nonexistent/python \
        HERMES_KANBAN_HOME="$(mixpath "$TPHOME")" HERMES_KANBAN_DB= HERMES_KANBAN_BOARD= \
        bash "$SCRIPT" 2>&1 ); rc=$?
[ "$rc" -eq 3 ] && ok || bad "T7c: a dead reader exited $rc, not 3 - an empty list reads as 'nothing owed'"
grep -q 'lacks the sqlite3 module\|no python on PATH' <<<"$out" && ok \
  || bad "T7d: the dead reader was not named (out: $out)"
out=$( bash "$SCRIPT" --bogus 2>&1 ); rc=$?
[ "$rc" -eq 3 ] && ok || bad "T7e: an unknown argument exited $rc, not 3"

# -- a CRLF board pointer still resolves (nit: PTR=$(cat) kept the CR) ----
# Two shapes: a CRLF pointer (the real writer's shape - this bash strips the
# trailing CRLF in command substitution, so it is the no-regression guard) and
# a BARE CR with no closing LF (nothing strips that one - it is the pin: the
# old read handed the CR straight into the board path).
CR_HOME=$(mktemp -d)
mkdir -p "$CR_HOME/kanban/boards/project-omega"
printf 'project-omega\r\n' >"$CR_HOME/kanban/current"
CR2_HOME=$(mktemp -d)
mkdir -p "$CR2_HOME/kanban/boards/project-omega"
printf 'project-omega\r' >"$CR2_HOME/kanban/current"
for h in "$CR_HOME" "$CR2_HOME"; do
  sqlex "$(mixpath "$h")/kanban/boards/project-omega/kanban.db" \
    "CREATE TABLE tasks(id TEXT, status TEXT, title TEXT, body TEXT);
CREATE TABLE task_comments(id INTEGER PRIMARY KEY AUTOINCREMENT, task_id TEXT, body TEXT, created_at INTEGER);
CREATE TABLE task_events(id INTEGER PRIMARY KEY AUTOINCREMENT, task_id TEXT, kind TEXT, payload TEXT, created_at INTEGER);" \
    && ok || bad "T10a: could not build the CR-pointer fixture board ($h)"
  cout=$( cd "$(dirname "$SCRIPT")" && env HOME="$h" HERMES_HOME= \
          HERMES_KANBAN_HOME="$(mixpath "$h")" HERMES_KANBAN_DB= HERMES_KANBAN_BOARD= \
          bash "$SCRIPT" 2>&1 ); rc=$?
  [ "$rc" -eq 0 ] && ok || bad "T10b: a CR-carrying board pointer did not resolve (rc $rc, out: $cout)"
  grep -qx 'PENDING none' <<<"$cout" && ok \
    || bad "T10c: the CR pointer did not reach the board (out: $cout)"
done

# -- a half-read board refuses loud (nit: comments_for/event_reason
#    swallowed sqlite3.Error, so a failed comment read degraded the
#    derivation to body-only - silently hiding a comment-carried re-ask) ----
HALF_HOME=$(mktemp -d)
mkdir -p "$HALF_HOME/kanban/boards/project-omega"
printf 'project-omega\n' >"$HALF_HOME/kanban/current"
sqlex "$(mixpath "$HALF_HOME")/kanban/boards/project-omega/kanban.db" \
  "CREATE TABLE tasks(id TEXT, status TEXT, title TEXT, body TEXT);
INSERT INTO tasks VALUES('t_half','ready','half a board','BODY.');" \
  && ok || bad "T11a: could not build the half-read fixture board"
cout=$( cd "$(dirname "$SCRIPT")" && env HOME="$HALF_HOME" HERMES_HOME= \
        HERMES_KANBAN_HOME="$(mixpath "$HALF_HOME")" HERMES_KANBAN_DB= HERMES_KANBAN_BOARD= \
        bash "$SCRIPT" 2>&1 ); rc=$?
[ "$rc" -eq 3 ] && ok \
  || bad "T11b: a board with no comment table exited $rc, not 3 - a silent body-only read reads as 'nothing owed'"
grep -q 'comment read failed' <<<"$cout" && ok \
  || bad "T11c: the half-read failure was not named (out: $cout)"

# -- an all-clear is stated, never an empty page -------------------------
EMPTY_HOME=$(mktemp -d)
EMPTY_DB="$(mixpath "$EMPTY_HOME")/empty.db"
sqlex "$EMPTY_DB" "CREATE TABLE tasks(id TEXT, status TEXT, title TEXT, body TEXT);
CREATE TABLE task_comments(id INTEGER PRIMARY KEY AUTOINCREMENT, task_id TEXT, body TEXT, created_at INTEGER);
CREATE TABLE task_events(id INTEGER PRIMARY KEY AUTOINCREMENT, task_id TEXT, kind TEXT, payload TEXT, created_at INTEGER);" \
  && ok || bad "T8a: could not build the empty fixture board"
out=$( cd "$(dirname "$SCRIPT")" && env HOME="$TPHOME" HERMES_HOME= \
        HERMES_KANBAN_DB="$EMPTY_DB" bash "$SCRIPT" 2>&1 ); rc=$?
[ "$rc" -eq 0 ] && ok || bad "T8b: an empty board exited $rc"
grep -qx 'PENDING none' <<<"$out" && ok || bad "T8c: an empty board printed no explicit all-clear (out: $out)"
rm -rf "$TPHOME" "$EMPTY_HOME" "$CR_HOME" "$CR2_HOME" "$HALF_HOME"

echo
# --- the pin: no line here may pipe a writer into an early-exit grep --------
# `printf '%s' "$OUT" | grep -q` under pipefail hands back the WRITER's
# SIGPIPE when grep leaves at the first match, so a needle that IS present
# reads as missing - and the negated form reads it as absent, which is a false
# green that hides a defect. A herestring has no writer, so grep's early exit
# costs nothing. This file scans its own live lines; comments may name the
# shape, so they are skipped.
_PIN_SHAPE='\|[[:space:]]*grep[[:space:]]+-[a-zA-Z]*q'
_PIN_SELF="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
_pin_hits="$(grep -vE '^[[:space:]]*#' "$_PIN_SELF" | grep -nE "$_PIN_SHAPE" || true)"
[ -z "$_pin_hits" ] && ok \
	|| bad "PIN: an assertion pipes a writer into grep -q (SIGPIPE under pipefail): $_pin_hits"
_PIN_TOKEN='|'
_pin_sample="  printf '%s\\n' \"\$OUT\" $_PIN_TOKEN grep -qxF -- \"\$1\""
grep -qE "$_PIN_SHAPE" <<<"$_pin_sample" && ok \
	|| bad "PIN: the detector no longer sees the pre-fix pipe shape"

# the behaviour, not the spelling: drive the extracted helpers on a payload
# that outruns the pipe buffer, with the needle on the FIRST line (the worst
# case for an early exit). A reformed helper extracts as a fragment and
# defines nothing, so the drive refuses instead of passing vacuously.
_pin_big="$(awk 'BEGIN{for(i=1;i<=8000;i++) printf "row %05d done\n", i}')"
_pin_has="$(awk '/^has\(\)/{print; exit}' "$_PIN_SELF")"
_pin_hasnt="$(awk '/^hasnt\(\)/{print; exit}' "$_PIN_SELF")"
[ -n "$_pin_has" ] && ok || bad "PIN: has() not found in this suite"
[ -n "$_pin_hasnt" ] && ok || bad "PIN: hasnt() not found in this suite"
_pin_races="$(
	set -uo pipefail
	fail=0
	ok() { :; }
	bad() { fail=$((fail + 1)); }
	eval "$_pin_has"
	eval "$_pin_hasnt"
	declare -F has >/dev/null && declare -F hasnt >/dev/null || { printf 'nodfn'; exit 0; }
	i=0
	while [ "$i" -lt 20 ]; do
		has "$_pin_big" "row 00001 done"
		hasnt "$_pin_big" "row 99999 done"
		i=$((i + 1))
	done
	printf '%s' "$fail"
)"
case "$_pin_races" in
0) ok ;;
nodfn) bad "PIN: the extracted has()/hasnt() defined no function (reformatted?)" ;;
*) bad "PIN: the assertion helpers lost a match $_pin_races/40 times on a $(( ${#_pin_big} / 1024 )) KB payload" ;;
esac

echo "tick-pending_test: $pass ok, $fail fail"
[ "$fail" -eq 0 ]
