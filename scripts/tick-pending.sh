#!/usr/bin/env bash
# tick-pending.sh - the MECHANICAL derivation of "needs Endo's word".
# Usage:
#     scripts/tick-pending.sh                 # the PENDING list, derived from the board
#     scripts/tick-pending.sh --check <id>    # one card: 0 settled, 1 pending, 2 no marker
#
# WHY THIS EXISTS (card t_f7666159, 2026-11-17): the tick's standing
# `PENDING (Endo)` flag was a SELF-REPORT LOOP, not a live query. t_d1fe01da was
# ruled SETTLED at midday 11-17 - the card body carried the RULING block, a card
# comment carried the RULING marker, the row read `ready` - and the tick went on
# re-reporting the flag twelve times, then BLOCKED the correctly-ready card on
# the belief the ruling was still owed ("it unblocks the moment your word
# lands"). The list was derived from the tick's own previous reports, carried
# pass to pass in conversation memory. A pass that blocks a correct card on an
# unverified belief is acting on the report chain, not on the board.
#
# So the derivation is a READ of the board, never a recital: markers and state
# are grepped, the settled marker vetoes, and the report's PENDING section is
# this script's output verbatim. The report chain can no longer feed the flag.
#
# WHAT IS "PENDING" (the four shapes, one reason line each):
#   1. TRIAGE whose body (or latest comment) asks for a ruling    -> triage:
#   2. SCHEDULED whose park reason gates on a tag/word/cut trigger -> scheduled:
#   3. BLOCKED whose block reason names a word still OWED         -> blocked:
#   4. any card whose body/latest comment carries an UNSETTLED ruling request
#      (`PENDING (Endo): <question>`, or a `RULING (` block with no verdict)
#
# WHAT IS NEVER PENDING: a card whose body or comments carry a `RULING (...)`
# marker reading SETTLED (or HOLD / ARCHIVED - the two other decided markers;
# the skill's two markers are opposite meanings and HOLD is DECIDED). Those
# print as `SETTLED <id>` under the settled header, and --check answers 0.
# Re-opening a settled card needs an edit that drops the decided word, or a
# fresh card - a settled marker is final for its card by construction.
#
# Output (greppable, one line per card):
#     PENDING <id> <one-line reason>
#     SETTLED <id>
#     TICK-PENDING: <n> pending, <m> settled
#
# --check exit codes: 0 = a DECIDED marker exists, the card queues normally;
# 1 = a pending signal exists, the word IS owed; 2 = no marker and no pending
# request - NO EVIDENCE, which is never "settled": read the card, and never
# block it on a belief. 3 = the tool failed (no board, no reader, unknown id).
#
# Board/home resolution and the DB reader mirror scripts/factory-status.sh,
# which in turn mirrors factory-dispatch.sh (the authority for the chain). The
# sqlite3 CLI is absent on fleet Windows hosts, so boards are read through the
# python sqlite3 module, and a missing reader or board is a LOUD refusal: a
# PENDING list printed from empty lists is exactly the failure this script
# exists to prevent - an empty list reads as "nothing owed".
#
# Env: TP_PY pins the interpreter (tests/probes). HERMES_KANBAN_DB,
# HERMES_KANBAN_BOARD, HERMES_KANBAN_HOME, HERMES_HOME, LOCALAPPDATA, HOME
# resolve the board the way the hermes CLI does.
set -uo pipefail

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
  PTR=""
  if [ -e "$ROOT/kanban/current" ]; then
    if [ ! -f "$ROOT/kanban/current" ] || [ ! -r "$ROOT/kanban/current" ]; then
      echo "tick-pending.sh: board pointer '$ROOT/kanban/current' exists but is not a readable file - refusing to guess a board" >&2
      exit 3
    fi
    PTR=$(cat "$ROOT/kanban/current") || {
      echo "tick-pending.sh: board pointer '$ROOT/kanban/current' could not be read - refusing to guess a board" >&2
      exit 3
    }
  fi
  for cand in "${HERMES_KANBAN_BOARD:-}" "$PTR"; do
    [ -n "$cand" ] || continue
    case "$cand" in *[/*\\]*) continue ;; esac
    if [ "$cand" = default ]; then DB="$ROOT/kanban.db"; break; fi
    DB="$ROOT/kanban/boards/$cand/kanban.db"; break
  done
  [ -n "$DB" ] || DB="$ROOT/kanban.db"
fi

die() { echo "tick-pending.sh: $1" >&2; exit 3; }

MODE=list
WANT=""
while [ $# -gt 0 ]; do
  case "$1" in
    --check)
      [ $# -ge 2 ] || die "--check needs a card id"
      MODE=check; WANT="$2"; shift 2 ;;
    --check=*)
      MODE=check; WANT="${1#--check=}"; shift ;;
    -h|--help)
      sed -n '2,50p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *)
      die "unknown argument '$1' (usage: tick-pending.sh [--check <id>])" ;;
  esac
done

PY="${TP_PY:-}"
if [ -z "$PY" ]; then
  if command -v python3 >/dev/null 2>&1; then PY=python3
  elif command -v python >/dev/null 2>&1; then PY=python; fi
fi
[ -n "$PY" ] || die "no python on PATH - the sqlite3-module DB reader is unavailable"
"$PY" -c 'import sqlite3' >/dev/null 2>&1 || die "python '$PY' lacks the sqlite3 module"
[ -f "$DB" ] || die "board DB not found at $DB (root '$ROOT')"

# The derivation itself. One process, one read, no writes (query_only).
#
# Two marker families, opposite meanings, and the whole point is that neither is
# remembered:
#   `RULING (<date>): SETTLED|HOLD|ARCHIVED ...`  decided - never ask again
#   `PENDING (Endo): <question>`                  owed - must keep appearing
# A marker is a LINE START (after an optional short uppercase label such as
# `DESIGN `) so prose that merely mentions the marker does not count as one.
# The verdict is read from the marker's own paragraph (up to a blank line), and
# a negated verdict ("not settled", "unsettled") stays unsettled - a false
# SETTLED is the dangerous direction: it hides a real question.
TPPY=$(cat <<'PY'
import json, re, sqlite3, sys

sys.stdout.reconfigure(newline="\n")
db, mode = sys.argv[1], sys.argv[2]
wanted = sys.argv[3] if len(sys.argv) > 3 else ""


def text(value):
    if value is None:
        return ""
    if isinstance(value, bytes):
        return value.decode("utf-8", "replace")
    return value


def one_line(value, limit=110):
    s = re.sub(r"\s+", " ", text(value)).strip()
    return s[:limit]


con = sqlite3.connect(db, timeout=10)
con.execute("PRAGMA query_only=ON")
cur = con.cursor()

try:
    rows = list(cur.execute("SELECT id, status, title, body FROM tasks ORDER BY id"))
except sqlite3.Error as exc:
    sys.stderr.write("tick-pending.sh: board read failed (tasks): %s\n" % exc)
    sys.exit(3)

MARKER = re.compile(r"^[ \t]*(?:[A-Z][A-Z0-9 /_-]{0,24}[ \t]+)?RULING[ \t]*\(", re.M)
PENDMARK = re.compile(r"^[ \t]*PENDING[ \t]*\([ \t]*Endo[ \t]*\)", re.M)
DECIDED = re.compile(r"\b(settled|hold|archived|decided)\b", re.I)
NEGATED = re.compile(r"\b(not[ \t-]+(settled|decided)|unsettled|undecided)\b", re.I)
# An open-question signal INSIDE the ruling clause. A `RULING (` block is a
# decision record by nature - its date and verdict are the whole point - so the
# DEFAULT is decided, and only an explicit ask makes it owed: a question mark, a
# word still owed, an awaited answer, or an unsettled/negated verdict. Fourteen
# settled rulings on the live board read "verbatim from the vault" / "option A"
# and never say SETTLED; treating "no verdict word" as pending re-asked every
# one of them - the exact loop this script exists to end.
OPEN = re.compile(
    r"\?|\bowed\b|\bawait\w*\b|\bpending\b|\bunsettled\b"
    r"|\bneeds?[ \t]+(his|endo)\b|\bto[ \t]+be[ \t]+ruled\b|\bnot[ \t]+yet\b",
    re.I,
)

# A body/comment that ASKS for his word (the triage shape).
ASKS = re.compile(
    r"pending[ \t]*\([ \t]*endo[ \t]*\)"
    r"|(needs?|await\w*|owes?|owed)\b[^\n]{0,48}\b(his|endo[^ \n]{0,2}s?)[ \t]+(word|answer|call|decision|say|ruling)"
    r"|\b(his|endo[^ \n]{0,2}s?)[ \t]+(word|answer|call|decision)\b"
    r"|until[ \t]+(he|endo)\b|ruling[ \t]+owed|unruled"
    r"|asks?[ \t]+for[ \t]+(a[ \t]+)?(design[ \t]+)?ruling",
    re.I,
)
# A park reason gates on a trigger Endo owns (the scheduled shape).
TAGGATE = re.compile(
    r"\btag\b|\bcut\b|\brelease\b|\bhis[ \t]+word\b|endo[^ \n]{0,2}s?[ \t]+word|gives[ \t]+the[ \t]+word",
    re.I,
)
# A block reason names a word still OWED. A past ruling ("Endo ruling
# 2026-11-15") is deliberately NOT a match - that word already landed, which is
# how window-gated cards read, and re-listing them is the loop this kills.
OWED = re.compile(
    r"\b(his|endo[^ \n]{0,2}s?)[ \t]+(word|answer|call|decision|say)\b"
    r"|until[ \t]+(he|endo)\b|pending[ \t]*\([ \t]*endo[ \t]*\)"
    r"|await\w*[ \t]+(his|endo)|owes?[ \t]+(his|endo)|word[ \t]+owed|ruling[ \t]+owed",
    re.I,
)


def blocks(body):
    out, cur_block = [], []
    for line in text(body).splitlines():
        if line.strip() == "":
            if cur_block:
                out.append("\n".join(cur_block))
                cur_block = []
        else:
            cur_block.append(line)
    if cur_block:
        out.append("\n".join(cur_block))
    return out


def first_line(s):
    for line in s.splitlines():
        if line.strip():
            return line.strip()
    return ""


def markers(body):
    """(verdict, excerpt) per marker block, in document order.
    verdict: settled | unsettled | pending"""
    found = []
    for block in blocks(body):
        m = MARKER.search(block)
        if m:
            head = block[m.start():]
            lines = head.splitlines()
            probe = lines[0]
            if len(lines) > 1:  # a verdict on the marker's next line still counts
                probe = probe + " " + lines[1].strip()[:120]
            if NEGATED.search(probe):
                found.append(("unsettled", one_line(head, 90)))
            elif DECIDED.search(probe):
                found.append(("settled", one_line(head, 90)))
            elif OPEN.search(probe):
                found.append(("unsettled", one_line(head, 90)))
            else:
                found.append(("settled", one_line(head, 90)))
            continue
        p = PENDMARK.search(block)
        if p:
            found.append(("pending", one_line(block[p.start():], 90)))
    return found


def comments_for(task_id):
    try:
        return list(cur.execute(
            "SELECT body FROM task_comments WHERE task_id=? ORDER BY created_at, id",
            (task_id,)))
    except sqlite3.Error:
        return []


def event_reason(task_id, kinds):
    try:
        row = cur.execute(
            "SELECT payload FROM task_events WHERE task_id=? AND kind IN (%s) ORDER BY id DESC LIMIT 1"
            % ",".join("?" * len(kinds)),
            (task_id,) + tuple(kinds)).fetchone()
    except sqlite3.Error:
        return ""
    if not row:
        return ""
    raw = text(row[0]) or "{}"
    try:
        payload = json.loads(raw)
    except ValueError:
        return one_line(raw)
    if not isinstance(payload, dict):
        return one_line(raw)
    for key in ("reason", "summary", "note"):
        value = payload.get(key)
        if value:
            return one_line(value, 200)
    return ""


def derive(status, body, comments, task_reason):
    """(state, why) where state is pending | settled | nomarker."""
    all_marks = []
    for verdict, excerpt in markers(body):
        all_marks.append((verdict, excerpt))
    for (cbody,) in comments:
        for verdict, excerpt in markers(cbody):
            all_marks.append((verdict, excerpt))

    # The settled veto: a card carrying a decided marker is NEVER pending.
    for verdict, excerpt in all_marks:
        if verdict == "settled":
            return "settled", excerpt

    pending = []
    for verdict, excerpt in all_marks:
        if verdict == "pending":
            pending.append("PENDING (Endo) marker: " + excerpt)
        else:
            pending.append("ruling marker with no verdict (unsettled): " + excerpt)

    if status == "triage":
        hay = text(body) + "\n" + "\n".join(text(c) for (c,) in comments)
        hit = ASKS.search(hay)
        if hit:
            pending.append('triage card asks for a ruling (matched "%s")'
                           % one_line(hit.group(0), 60))

    if status == "scheduled" and task_reason:
        if TAGGATE.search(task_reason):
            pending.append('scheduled, park reason gates on a tag/word trigger ("%s")'
                           % one_line(task_reason, 80))

    if status == "blocked" and task_reason:
        if OWED.search(task_reason):
            pending.append('blocked, reason names a word still owed ("%s")'
                           % one_line(task_reason, 80))

    if not pending:
        return "nomarker", ""
    # One line per card: the first signal is the headline, the rest are counted -
    # a card is one row in the report, never a paragraph.
    head = pending[0]
    if len(pending) > 1:
        head += " (+%d more signal%s)" % (len(pending) - 1, "" if len(pending) == 2 else "s")
    return "pending", head


if mode == "check":
    match = [r for r in rows if text(r[0]) == wanted]
    if not match:
        sys.stderr.write("tick-pending.sh: no card %s on this board\n" % wanted)
        sys.exit(3)
    tid, status, _title, body = match[0]
    state, why = derive(text(status), body, comments_for(tid),
                        event_reason(tid, ("blocked", "scheduled")))
    if state == "settled":
        print("SETTLED %s" % tid)
        sys.exit(0)
    if state == "pending":
        print("PENDING %s %s" % (tid, why))
        sys.exit(1)
    print("NOMARKER %s no RULING marker and no pending request - "
          "read the card, never block it on a belief" % tid)
    sys.exit(2)

pending_rows, settled_rows = [], []
for tid, status, _title, body in rows:
    status = text(status)
    if status == "archived":
        continue
    state, why = derive(status, body, comments_for(tid),
                        event_reason(tid, ("blocked", "scheduled")))
    if state == "pending":
        pending_rows.append((tid, why))
    elif state == "settled" and status not in ("done", "archived"):
        # A decided marker is worth a line only on a card that is still in
        # play: on a done card it is history, and printing forty of them
        # every pass is its own noise floor. An OPEN question on a done card
        # still prints as PENDING above - the finished-build-open-decision case.
        settled_rows.append(tid)

print("== pending: needs Endo's word ==")
if pending_rows:
    for tid, why in pending_rows:
        print("PENDING %s %s" % (tid, why))
else:
    print("PENDING none")
if settled_rows:
    print("== settled: decided, ignore ==")
    for tid in settled_rows:
        print("SETTLED %s" % tid)

print("TICK-PENDING: %d pending, %d settled, %d cards read"
      % (len(pending_rows), len(settled_rows), len(rows)))
PY
)

"$PY" -c "$TPPY" "$DB" "$MODE" "$WANT"
rc=$?
exit "$rc"
