#!/usr/bin/env bash
# factory-merge_test.sh - runnable checks for scripts/factory-merge.sh.
# Run: bash scripts/factory-merge_test.sh
#
# The script under test is the REVIEW-SEAT merge entry (card t_5b61405b):
# an APPROVED card lands in the same session, through the SAME cycle the
# tick runs (scripts/train-pass.sh, pinned to the card), then the prune
# closer. These checks pin the shapes that make that safe:
#
#   - an approved card merges inside the review session: real repo, real
#     rebase, real merge commit, real push, prune, receipt comment;
#   - the rebuild booking follows the product-bytes law (runtime bytes DUE,
#     a test-only landing NOT-DUE) and the UI eyeball line when UI is in
#     range;
#   - a second attempt refuses cleanly (ALREADY-MERGED, no second merge
#     commit) - the board can never show a double merge;
#   - a forced red gate leaves the card exactly where today's flow leaves it
#     (train-pass demote to ready) with nothing landed, and the tick's
#     fallback sweep still lands a card the review session skipped;
#   - a held pipeline lock DEFERS to the tick instead of authoring beside it;
#   - a fenced worker session (OMEGA_WORKER + the shim at the front of PATH)
#     still merges through this door, while the shim itself still refuses a
#     bare push - the door is this script, not a disabled fence;
#   - a card that is not a queued (approved) landing never reaches a rebase.
#
# The board is a fixture sqlite db with the live schema's columns. The
# sqlite3 CLI is absent on fleet Windows hosts, so the fixture ships a
# python-backed `sqlite3` on PATH (the same substitution train-pass makes
# internally); one check runs WITHOUT it to prove train-pass's own reader.
set -uo pipefail

SCRIPT="$(cd "$(dirname "$0")" && pwd)/factory-merge.sh"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }
has() { printf '%s\n' "$1" | grep -qF "$2"; }
hasnt() { ! printf '%s\n' "$1" | grep -qF "$2"; }

[ -f "$SCRIPT" ] || { echo "factory-merge_test: $SCRIPT missing" >&2; exit 1; }
[ -x "$SCRIPT" ] || { echo "factory-merge_test: $SCRIPT is not executable" >&2; exit 1; }

# Native git is a Windows binary: MSYS /tmp paths are NOT translated for it,
# so every path handed to git as an argument goes through mixpath.
mixpath() { cygpath -m "$1" 2>/dev/null || printf '%s' "$1"; }

# ---- fixture scaffolding ------------------------------------------------

HARNESS="$(mktemp -d "${TMPDIR:-/tmp}/fmtest.XXXXXX")"
# Native tools (python, git) cannot read an MSYS /tmp path: the whole fixture
# lives on a native path, which bash also understands.
HARNESS="$(cygpath -m "$HARNESS" 2>/dev/null || printf '%s' "$HARNESS")"
REPO="$HARNESS/repo"
ORIGIN="$HARNESS/origin.git"
BOARD="$HARNESS/board/kanban.db"
MOCK="$HARNESS/mock"
LOCKDIR="$HARNESS/locks"
mkdir -p "$HARNESS/bin" "$HARNESS/board" "$MOCK" "$LOCKDIR"

cleanup() { [ -n "${HARNESS:-}" ] && rm -rf "$HARNESS" 2>/dev/null; }
trap cleanup EXIT
[ -n "${FM_TEST_KEEP:-}" ] && trap - EXIT && echo "factory-merge_test: fixture kept at $HARNESS" >&2

# The fixture's board reader: a python-backed sqlite3 CLI (the CLI itself is
# missing on this host). Same shapes train-pass uses.
cat >"$HARNESS/bin/board_reader.py" <<'PY'
import sqlite3, sys
args, sep = sys.argv[1:], "|"
while args and args[0].startswith("-"):
    a = args.pop(0)
    if a == "-separator":
        sep = args.pop(0)
db = args[0]
sql = " ".join(args[1:]) if len(args) > 1 else sys.stdin.read()
stmts, buf = [], ""
for line in sql.splitlines(True):
    buf += line
    if sqlite3.complete_statement(buf):
        stmts.append(buf)
        buf = ""
if buf.strip():
    stmts.append(buf)
con = sqlite3.connect(db, timeout=30)
# Windows text streams translate \n to \r\n; the callers parse LF records.
sys.stdout.reconfigure(newline="\n")
try:
    for stmt in stmts:
        cur = con.execute(stmt)
        if cur.description:
            for row in cur.fetchall():
                print(sep.join("" if v is None else str(v) for v in row))
    con.commit()
finally:
    con.close()
PY
cat >"$HARNESS/bin/sqlite3" <<'SH'
#!/usr/bin/env bash
here="$(cd "$(dirname "$0")" && pwd)"
script="$(cygpath -m "$here/board_reader.py" 2>/dev/null || printf '%s' "$here/board_reader.py")"
for p in python python3; do
  if command -v "$p" >/dev/null 2>&1; then
    exec "$p" "$script" "$@"
  fi
done
echo "sqlite3 shim: no python" >&2
exit 1
SH
chmod +x "$HARNESS/bin/sqlite3"
# PATH entries must be MSYS form (/c/...): bash's own lookup ignores a
# "C:/..." entry, while native tools need the C:/ form for their arguments.
HBIN="$(cygpath -u "$HARNESS/bin" 2>/dev/null || printf '%s' "$HARNESS/bin")"
MMOCK="$(cygpath -u "$MOCK" 2>/dev/null || printf '%s' "$MOCK")"
export PATH="$HBIN:$MMOCK:$PATH"

# hermes stub: `kanban comment <id> <body>` inserts verbatim into the fixture
# board (strike counting and the receipt read it); everything else logs.
cat >"$MOCK/hermes" <<'STUB'
#!/usr/bin/env bash
echo "hermes $*" >>"$(dirname "$0")/hermes.log"
all=("$@")
n=${#all[@]}
if [ "${all[1]:-}" = "comment" ] && [ "$n" -ge 4 ]; then
  id="${all[2]}"
  body="${all[$((n - 1))]}"
  sqlite3 "$KANBAN_DB" "INSERT INTO task_comments(task_id,author,body,created_at)
    VALUES('$id','merge','$(printf '%s' "$body" | sed "s/'/''/g")',123);"
fi
exit 0
STUB
chmod +x "$MOCK/hermes"

new_board() {
  sqlite3 "$BOARD" <<'SQL'
CREATE TABLE tasks (
  id TEXT PRIMARY KEY, title TEXT, body TEXT, assignee TEXT, status TEXT,
  completed_at INTEGER, claim_lock TEXT, claim_expires INTEGER, worker_pid INTEGER,
  current_run_id INTEGER, workspace_kind TEXT, workspace_path TEXT,
  branch_name TEXT, result TEXT);
CREATE TABLE task_links (parent_id TEXT, child_id TEXT);
CREATE TABLE task_comments (
  id INTEGER PRIMARY KEY AUTOINCREMENT, task_id TEXT NOT NULL,
  author TEXT NOT NULL, body TEXT NOT NULL, created_at INTEGER NOT NULL);
CREATE TABLE task_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT, task_id TEXT NOT NULL, run_id INTEGER,
  kind TEXT NOT NULL, payload TEXT, created_at INTEGER NOT NULL);
SQL
}

new_repo() {
  git init -q --initial-branch=dev "$(mixpath "$REPO")"
  git -C "$(mixpath "$REPO")" config user.email t@t
  git -C "$(mixpath "$REPO")" config user.name t
  git -C "$(mixpath "$REPO")" config commit.gpgsign false
  mkdir -p "$REPO/scripts" "$REPO/bin/shims" "$REPO/core"
  cp "$ROOT/scripts/train-pass.sh" "$ROOT/scripts/factory-merge.sh" \
     "$ROOT/scripts/factory-prune.sh" "$REPO/scripts/"
  cp "$ROOT/bin/shims/git" "$REPO/bin/shims/git"
  chmod +x "$REPO/scripts/"*.sh "$REPO/bin/shims/git"
  echo "base" >"$REPO/base.txt"
  echo "package core" >"$REPO/core/doc.go"
  printf '.worktrees/\n' >"$REPO/.gitignore"
  git -C "$(mixpath "$REPO")" add -A
  git -C "$(mixpath "$REPO")" commit -q -m "chore: base"
  git init -q --bare "$(mixpath "$ORIGIN")"
  git -C "$(mixpath "$REPO")" remote add origin "$(mixpath "$ORIGIN")"
  git -C "$(mixpath "$REPO")" push -q -u origin dev
}

# new_card <id> <file> <status>: a card whose branch carries ONE commit
# touching <file>, plus its board row.
new_card() {
  local id="$1" file="$2" status="${3:-done}" wt="$REPO/.worktrees/$1"
  git -C "$(mixpath "$REPO")" worktree add -q "$(mixpath "$wt")" -b "wt/$id" dev
  mkdir -p "$(dirname "$wt/$file")"
  echo "$id work" >>"$wt/$file"
  git -C "$(mixpath "$wt")" add -A
  git -C "$(mixpath "$wt")" commit -q -m "feat($id): work"
  sqlite3 "$BOARD" "INSERT INTO tasks(id,title,assignee,status,completed_at,workspace_kind,workspace_path,branch_name)
    VALUES('$id','card $id','cody','$status',1800000000,'worktree','$(mixpath "$wt")','wt/$id');"
}

# merge <id> [extra env...]: run the review-seat entry, stdout+stderr in OUT,
# exit code in RC.
merge() {
  OUT="$(env FM_REPO="$(mixpath "$REPO")" FM_LOCK_DIR="$LOCKDIR" \
    KANBAN_DB="$BOARD" HERMES="$MOCK/hermes" TRAIN_GATES_CMD="${GATES:-true}" \
    PATH="$MMOCK:$HBIN:$PATH" \
    bash "$SCRIPT" "$@" 2>&1)"
  RC=$?
}

# tick_sweep: the tick's merge pass - an UNPINNED train pass (the fallback),
# run from the main checkout exactly as the tick runs it.
tick_sweep() {
  OUT="$(cd "$REPO" && env KANBAN_DB="$BOARD" HERMES="$MOCK/hermes" TRAIN_GATES_CMD="${GATES:-true}" \
    PATH="$MMOCK:$HBIN:$PATH" \
    bash scripts/train-pass.sh 2>&1)"
  RC=$?
}

dev_sha() { git -C "$(mixpath "$REPO")" rev-parse dev; }
origin_sha() { git -C "$(mixpath "$ORIGIN")" rev-parse dev; }
card_status() { sqlite3 "$BOARD" "SELECT status FROM tasks WHERE id='$1';"; }
comment_count() { sqlite3 "$BOARD" "SELECT count(*) FROM task_comments WHERE task_id='$1';"; }
merge_commit_count() {
  git -C "$(mixpath "$REPO")" log --merges --format=%s dev | grep -cF "Merge card $1:" || true
}

# ---- the fixture --------------------------------------------------------

new_board || { echo "factory-merge_test: board fixture failed" >&2; exit 1; }
new_repo || { echo "factory-merge_test: repo fixture failed" >&2; exit 1; }

# ---- 1. an approved card merges in the review session -------------------

new_card t_c1 core/thing.go done
base="$(dev_sha)"
merge t_c1
has "$OUT" "MERGED t_c1" && ok || bad "S1: MERGED line missing: $OUT"
[ "$RC" = "0" ] && ok || bad "S1: rc was $RC, wanted 0"
[ "$(dev_sha)" != "$base" ] && ok || bad "S1: dev did not move"
[ "$(git -C "$(mixpath "$REPO")" rev-list --parents -n1 dev | wc -w)" = "3" ] \
  && ok || bad "S1: landed commit is not a two-parent merge commit"
[ "$(dev_sha)" = "$(origin_sha)" ] && ok || bad "S1: dev was not pushed to origin"
git -C "$(mixpath "$REPO")" show-ref --verify --quiet refs/heads/wt/t_c1 \
  && bad "S1: branch wt/t_c1 survived the prune" || ok
[ -d "$REPO/.worktrees/t_c1" ] && bad "S1: card worktree survived the prune" || ok
git -C "$(mixpath "$REPO")" for-each-ref --format='%(refname)' 'refs/train/*-t_c1' \
  | grep -q . && ok || bad "S1: no marker ref written by the landing"
comment_count t_c1 | grep -q '^[1-9]' && ok || bad "S1: no merge receipt comment on the card"
sqlite3 "$BOARD" "SELECT body FROM task_comments WHERE task_id='t_c1';" >"$HARNESS/c1.txt"
grep -q "landed" "$HARNESS/c1.txt" && ok || bad "S1: receipt does not name the landing"
grep -q "review seat" "$HARNESS/c1.txt" && ok || bad "S1: receipt does not name the reviewing seat"
has "$OUT" "REBUILD-DUE t_c1" && ok || bad "S1: product bytes did not book a rebuild: $OUT"

# ---- 2. a second attempt refuses cleanly (no double merge) --------------

c1merges="$(merge_commit_count t_c1)"
merge t_c1
has "$OUT" "ALREADY-MERGED t_c1" && ok || bad "S2: ALREADY-MERGED line missing: $OUT"
[ "$RC" = "0" ] && ok || bad "S2: rc was $RC, wanted 0 (a clean refusal)"
[ "$(merge_commit_count t_c1)" = "$c1merges" ] && ok || bad "S2: a second merge commit appeared"
[ "$(dev_sha)" = "$(origin_sha)" ] && ok || bad "S2: origin drifted on the second attempt"

# ---- 3. the rebuild booking follows the product-bytes law ---------------

new_card t_c2 core/thing_test.go done
merge t_c2
has "$OUT" "MERGED t_c2" && ok || bad "S3: test-only card did not merge: $OUT"
has "$OUT" "REBUILD-NOT-DUE t_c2" && ok || bad "S3: a test-only landing booked a rebuild: $OUT"

# ---- 4. UI in range carries the imagery line ----------------------------

new_card t_c3 webui/app/pages/index.vue done
merge t_c3
has "$OUT" "MERGED t_c3" && ok || bad "S4: UI card did not merge: $OUT"
has "$OUT" "EYEBALL t_c3" && ok || bad "S4: no imagery/eyeball line for a UI landing: $OUT"

# ---- 5. a forced red gate leaves the card where today's flow leaves it --

new_card t_c4 core/red.go done
base="$(dev_sha)"
GATES=false merge t_c4
has "$OUT" "FAILED t_c4" && ok || bad "S5: red-gate run did not report FAILED: $OUT"
[ "$RC" = "6" ] && ok || bad "S5: rc was $RC, wanted 6"
[ "$(dev_sha)" = "$base" ] && ok || bad "S5: a red gate moved dev"
[ "$(card_status t_c4)" = "ready" ] && ok || bad "S5: card status is $(card_status t_c4), wanted ready (the train's own demote)"
git -C "$(mixpath "$REPO")" show-ref --verify --quiet refs/heads/wt/t_c4 \
  && ok || bad "S5: the rebased branch was not kept for the rerun"
has "$OUT" "FAILED t_c4" && hasnt "$OUT" "MERGED t_c4" && ok || bad "S5: a failed landing reported itself merged"

# ---- 6. a held pipeline lock defers to the tick -------------------------

new_card t_c5 core/locked.go done
base="$(dev_sha)"
printf 'tick pass\n' >"$LOCKDIR/omega-pipeline.lock"
merge t_c5
has "$OUT" "DEFERRED t_c5" && ok || bad "S6: DEFERRED line missing: $OUT"
[ "$RC" = "3" ] && ok || bad "S6: rc was $RC, wanted 3"
[ "$(dev_sha)" = "$base" ] && ok || bad "S6: a deferred merge landed anyway"
[ -e "$LOCKDIR/omega-pipeline.lock" ] && ok || bad "S6: the merge removed a lock it did not take"
rm -f "$LOCKDIR/omega-pipeline.lock"

# ---- 7. the tick's fallback sweep lands what the review seat skipped ---

tick_sweep
has "$OUT" "landed t_c5" && ok || bad "S7: the fallback sweep did not land the skipped card: $OUT"
[ "$(merge_commit_count t_c5)" -ge 1 ] && ok || bad "S7: the skipped card is still unlanded"
merge t_c5
has "$OUT" "ALREADY-MERGED t_c5" && ok || bad "S8: the review seat re-merged a card the tick swept: $OUT"

# ---- 9. a fenced worker session merges through the door -----------------
# The real shim, the real stamp: a bare push refuses, factory-merge lands.

new_card t_c6 core/fenced.go done
FENCED_PATH="$(cygpath -u "$REPO/bin/shims"):$MMOCK:$HBIN:$PATH"
out=$(env -C "$(mixpath "$REPO")" OMEGA_WORKER=1 OMEGA_WORKER_CARD=t_c6 \
  PATH="$FENCED_PATH" git push --dry-run origin dev 2>&1)
has "$out" "FENCE:" && ok || bad "S9: the fence shim did not refuse a bare push (fixture broken)"
OUT="$(cd "$REPO" && env OMEGA_WORKER=1 OMEGA_WORKER_CARD=t_c6 \
  FM_REPO="$(mixpath "$REPO")" FM_LOCK_DIR="$LOCKDIR" KANBAN_DB="$BOARD" \
  HERMES="$MOCK/hermes" TRAIN_GATES_CMD=true PATH="$FENCED_PATH" \
  bash "$SCRIPT" t_c6 2>&1)"
RC=$?
has "$OUT" "MERGED t_c6" && ok || bad "S9: the review seat could not merge from a fenced session: $OUT"
[ "$RC" = "0" ] && ok || bad "S9: rc was $RC, wanted 0"

# ---- 10. a card that is not an approved landing never rebases -----------

new_card t_c7 core/running.go running
base="$(dev_sha)"
merge t_c7
has "$OUT" "FAILED t_c7" && ok || bad "S10: a running card did not fail: $OUT"
[ "$RC" = "6" ] && ok || bad "S10: rc was $RC, wanted 6"
[ "$(dev_sha)" = "$base" ] && ok || bad "S10: a running (unapproved) card was landed"

# ---- 11. a checkout that is not on dev refuses, naming the reason -------

new_card t_c8 core/offdev.go done
git -C "$(mixpath "$REPO")" checkout -q -b scratch-branch
merge t_c8
has "$OUT" "REFUSED not-on-dev" && ok || bad "S11: refused without naming not-on-dev: $OUT"
[ "$RC" = "2" ] && ok || bad "S11: rc was $RC, wanted 2"
git -C "$(mixpath "$REPO")" checkout -q dev
git -C "$(mixpath "$REPO")" branch -q -D scratch-branch

# ---- 12. train-pass's own board reader (no sqlite3 CLI on PATH) ---------
# Proven by construction above; this pins the reader itself, since a silent
# "queue empty" from an unreadable board is the failure this law exists for.

new_card t_c9 core/reader.go done
NP="$(printf "%s" "$PATH" | sed "s|$HBIN:||")"
out=$(cd "$REPO" && env TRAIN_CARD=t_c9 KANBAN_DB="$BOARD" HERMES="$MOCK/hermes" \
  TRAIN_GATES_CMD=true PATH="$NP" \
  bash scripts/train-pass.sh 2>&1)
rc=$?
has "$out" "landed t_c9" && [ "$rc" = "0" ] && ok \
  || bad "S12: the python board reader did not drive a landing (rc $rc): $out"

# ---- 13. no FM_REPO: the entry resolves the main checkout itself -------
# The review session may run the entry from its review worktree; the common
# git dir names the checkout that lands.

new_card t_c10 core/anywhere.go done
OUT="$(cd "$REPO/.worktrees/t_c10" && env -u FM_REPO \
  FM_LOCK_DIR="$LOCKDIR" KANBAN_DB="$BOARD" HERMES="$MOCK/hermes" \
  TRAIN_GATES_CMD=true PATH="$MMOCK:$HBIN:$PATH" \
  bash "$SCRIPT" t_c10 2>&1)"
RC=$?
has "$OUT" "MERGED t_c10" && ok || bad "S13: the entry did not resolve the main checkout from a worktree: $OUT"
[ "$RC" = "0" ] && ok || bad "S13: rc was $RC, wanted 0"

echo "factory-merge_test: $pass ok, $fail fail"
[ "$fail" = "0" ]