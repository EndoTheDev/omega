#!/usr/bin/env bash
set -uo pipefail

# train-pass_test.sh - scratch-repo tests for scripts/train-pass.sh and
# scripts/train-bisect.sh.
# Run: bash scripts/train-pass_test.sh
#
# The hermes CLI is a stub; the board is a fixture sqlite db carrying the
# few tables the train touches (same columns as the live schema). Gates
# are overridden with TRAIN_GATES_CMD - the real gate list is repo-shaped
# (go.mod / webui / journeys presence) and is exercised for real on
# every actual landing run against dev.

TRAIN="$(cd "$(dirname "$0")" && pwd)/train-pass.sh"
BISECT="$(cd "$(dirname "$0")" && pwd)/train-bisect.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
badf() {
  fail=$((fail + 1))
  echo "FAIL: $1"
}

# ---- the board reader ---------------------------------------------------
# The sqlite3 CLI is absent on fleet Windows hosts (the same gap
# train-pass.sh and factory-dispatch.sh document for themselves), and this
# suite seeds every fixture board with it - so on such a host the whole
# suite reds "sqlite3: command not found" no matter what the train does.
# Ship the shim the scripts already carry inline: the python sqlite3 module
# behind the CLI's own argument shapes ([-readonly] [-separator S] <arg>...,
# statements also via stdin). The stub hermes below calls it too, so it must
# sit on PATH, not be a shell function.
# A PATH entry must be POSIX under MSYS: a C:/ entry is split at the colon.
posix_path() {
  if command -v cygpath >/dev/null 2>&1; then cygpath -u "$1"; else printf '%s' "$1"; fi
}
if ! command -v sqlite3 >/dev/null 2>&1; then
  SUITE_PY="$(command -v python || command -v python3 || true)"
  [ -n "$SUITE_PY" ] || {
    echo "train-pass_test: no board reader - neither the sqlite3 CLI nor python is present" >&2
    exit 1
  }
  SHIM_ROOT="$(mktemp -d)"
  SHIM_DIR="$(posix_path "$SHIM_ROOT")/bin"
  mkdir -p "$SHIM_DIR"
  cat >"$SHIM_DIR/sqlite3" <<SHIM
#!/usr/bin/env bash
exec "$SUITE_PY" -c '
import sqlite3, sys
args, sep, readonly = sys.argv[1:], "|", False
while args and args[0].startswith("-"):
    a = args.pop(0)
    if a == "-separator":
        sep = args.pop(0)
    elif a == "-readonly":
        readonly = True
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
sys.stdout.reconfigure(newline="\n")
try:
    for stmt in stmts:
        cur = con.execute(stmt)
        if cur.description:
            for row in cur.fetchall():
                print(sep.join("" if v is None else str(v) for v in row))
    if not readonly:
        con.commit()
finally:
    con.close()
' "\$@"
SHIM
  chmod +x "$SHIM_DIR/sqlite3"
  PATH="$SHIM_DIR:$PATH"
  export PATH
fi

# ---- fixture scaffolding ------------------------------------------------

mk_harness() {
  HARNESS="$(mktemp -d)"
  REPO="$HARNESS/repo"
  mkdir -p "$HARNESS/mock" "$HARNESS/board"
  : >"$HARNESS/mock/hermes.log"
  cat >"$HARNESS/mock/hermes" <<'STUB'
#!/usr/bin/env bash
# stub hermes: `kanban comment <id> <body>` inserts verbatim into the
# fixture board (strike counting reads it, exactly like the real CLI's
# comment); `kanban create ...` / `kanban edit ...` log to hermes.log.
log="$(dirname "$0")/hermes.log"
echo "hermes $*" >>"$log"
all=("$@")
n=${#all[@]}
if [ "${all[1]}" = "comment" ] && [ "$n" -ge 4 ]; then
  id="${all[2]}"
  body="${all[$((n-1))]}"
  sqlite3 "$KANBAN_DB" "INSERT INTO task_comments(task_id,author,body,created_at)
    VALUES('$id','train','$(printf '%s' "$body" | sed "s/'/''/g")',123);"
fi
exit 0
STUB
  chmod +x "$HARNESS/mock/hermes"
  HERMES="$HARNESS/mock/hermes"
  export HERMES
  unset TRAIN_GATES_CMD TRAIN_JOURNEYS_CMD TRAIN_NO_PUSH HERMES_KANBAN_BOARD TRAIN_RECONCILER_ASSIGNEE
  # the env contract under test (card t_7acaa267): every leg states its own
  unset KANBAN_DB HERMES_KANBAN_DB HERMES_KANBAN_HOME HERMES_HOME TRAIN_CARD
  unset OMEGA_HOME OMEGA_BIN JOURNEY_BASE_URL JOURNEY_WITNESS
}

new_board() {  # new_board [<db path>] - default: the harness dir (explicit KANBAN_DB)
  KANBAN_DB="${1:-$HARNESS/board/kanban.db}"
  mkdir -p "$(dirname "$KANBAN_DB")"
  export KANBAN_DB
  sqlite3 "$KANBAN_DB" <<'SQL'
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
  git init -q "$REPO"
  git -C "$REPO" config user.email t@t
  git -C "$REPO" config user.name t
  echo "shared unchanged" >"$REPO/shared.txt"
  echo "base" >"$REPO/base.txt"
  git -C "$REPO" add -A
  git -C "$REPO" commit -qm "base"
  git -C "$REPO" branch -M dev
}

card_row() { # card_row <id> <branch> <completed_at> [<workspace_path>]
  local ws="${4:-}"
  sqlite3 "$KANBAN_DB" "INSERT INTO tasks(id,title,assignee,status,completed_at,branch_name,workspace_kind,workspace_path)
    VALUES('$1','Test card $1','cody','done',$3,'$2','worktree','$ws');"
}

run() {
  rc=0
  ( cd "$REPO" && "$TRAIN" "$@" >"$HARNESS/run.out" 2>&1 ) || rc=$?
}

bisect_run() {
  rc=0
  ( cd "$REPO" && "$BISECT" "$@" >"$HARNESS/run.out" 2>&1 ) || rc=$?
}

# The no-caller-env pass (card t_7acaa267): HOME carries no hermes home, the
# remapped windows live home sits under LOCALAPPDATA - the exact shape that
# used to die `board db not found` exit 2 before the queue.
run_nocaller_env() {
  rc=0
  mkdir -p "$HARNESS/home" "$HARNESS/app"
  ( cd "$REPO" && env -u KANBAN_DB -u HERMES_KANBAN_DB -u HERMES_KANBAN_HOME -u HERMES_HOME \
      -u OMEGA_HOME -u OMEGA_BIN -u JOURNEY_BASE_URL \
      HOME="$HARNESS/home" LOCALAPPDATA="$HARNESS/app" \
      "$TRAIN" "$@" >"$HARNESS/run.out" 2>&1 ) || rc=$?
}

# <ok|dead>: a train run whose lane health answer comes from a curl stub on
# PATH (no server, no port) - the journeys precondition's two branches.
run_with_lane() {
  rc=0
  ( cd "$REPO" && PATH="$STUBBIN:$PATH" STUB_LANE_HEALTH="$1" \
      "$TRAIN" --no-push >"$HARNESS/run.out" 2>&1 ) || rc=$?
}

# a fixture checkout that carries the journeys suite (the gate sees it)
fixture_journeys() {
  mkdir -p "$REPO/scripts/journeys"
  cat >"$REPO/scripts/journeys/run.sh" <<'JR'
#!/usr/bin/env bash
# fixture suite: witness the env the train handed the gate
if [ -n "${JOURNEY_WITNESS:-}" ]; then
  { printf 'OMEGA_HOME=%s\n' "${OMEGA_HOME:-unset}"
    printf 'OMEGA_BIN=%s\n' "${OMEGA_BIN:-unset}"
  } >"$JOURNEY_WITNESS"
fi
exit 0
JR
  chmod +x "$REPO/scripts/journeys/run.sh"
}

# ... plus a serving lane home at <checkout>/bin
fixture_lane_home() {
  mkdir -p "$REPO/bin/config"
  : >"$REPO/bin/config/config.yaml"
  printf '#!/bin/sh\nexit 0\n' >"$REPO/bin/omega"
  chmod +x "$REPO/bin/omega"
}

# a curl stub: `{"ok":true}` for a healthy lane, connection failure for a dead
lane_stub() {
  STUBBIN="$(posix_path "$HARNESS")/stubbin"
  mkdir -p "$STUBBIN"
  cat >"$STUBBIN/curl" <<'CURL'
#!/usr/bin/env bash
case "${STUB_LANE_HEALTH:-}" in
  ok) printf '{"ok":true}\n'; exit 0 ;;
  *) exit 7 ;;
esac
CURL
  chmod +x "$STUBBIN/curl"
}

# a committed card branch: card_branch <task_id> <completed_at>
card_branch() {
  git -C "$REPO" checkout -q -b "wt/$1" dev
  echo "$1 work" >"$REPO/$1.txt"
  git -C "$REPO" add -A
  git -C "$REPO" commit -qm "feat(card): $1 work"
  git -C "$REPO" checkout -q dev
  card_row "$1" "wt/$1" "$2"
}

status_of() { sqlite3 "$KANBAN_DB" "SELECT status FROM tasks WHERE id='$1';"; }

echo "== S1: clean rebase -> land + marker"
ok

mk_harness
new_board
new_repo
git -C "$REPO" checkout -q -b wt/t_cardA
echo "cardA work" >"$REPO/cardA.txt"
git -C "$REPO" add -A
git -C "$REPO" commit -qm "feat(card): cardA work"
git -C "$REPO" checkout -q dev
echo "dev moves" >>"$REPO/shared.txt"
git -C "$REPO" add -A
git -C "$REPO" commit -qm "docs(dev): dev moves on"
card_row t_cardA wt/t_cardA 100
run --no-push
if [ "$rc" != 0 ]; then badf "S1: expected rc 0, got $rc: $(cat "$HARNESS/run.out")"; fi
grep -q "DONE t_cardA" "$HARNESS/run.out" || badf "S1: no DONE line: $(cat "$HARNESS/run.out")"
git -C "$REPO" rev-parse --verify -q refs/train/0001-t_cardA >/dev/null || badf "S1: marker ref missing"
[ "$(git -C "$REPO" rev-parse dev)" = "$(git -C "$REPO" rev-parse refs/train/0001-t_cardA)" ] ||
  badf "S1: marker must point at the landing and dev sits at it after one card"
git -C "$REPO" show-ref --verify --quiet refs/heads/wt/t_cardA && badf "S1: card branch not swept"
st="$(status_of t_cardA)"
[ "$st" = "done" ] || badf "S1: card stays done, got $st"
sqlite3 "$KANBAN_DB" "SELECT count(*) FROM task_comments WHERE task_id='t_cardA' AND author='train' AND body LIKE 'train: landed%';" |
  grep -q '^1$' || badf "S1: landing comment missing"
echo "== S2: conflict -> strike -> demote -> reconciler"
ok

mk_harness
new_board
new_repo
card_row t_conf wt/t_conf 50
git -C "$REPO" checkout -q -b wt/t_conf
echo "card line" >"$REPO/shared.txt"
git -C "$REPO" add -A
git -C "$REPO" commit -qm "feat(card): shared.txt card version"
git -C "$REPO" checkout -q dev
echo "dev line" >"$REPO/shared.txt"
git -C "$REPO" add -A
git -C "$REPO" commit -qm "docs(dev): shared.txt dev version"
run --no-push
if [ "$rc" != 3 ]; then badf "S2a: strike 1 should exit 3, got $rc: $(cat "$HARNESS/run.out")"; fi
[ "$(status_of t_conf)" = "done" ] || badf "S2a: strike 1 must keep the card done"
run --no-push
if [ "$rc" != 4 ]; then badf "S2b: strike 2 should exit 4, got $rc: $(cat "$HARNESS/run.out")"; fi
sqlite3 "$KANBAN_DB" "SELECT status||'|'||IFNULL(completed_at,'') FROM tasks WHERE id='t_conf';" |
  grep -q '^ready|$' || badf "S2b: strike 2 must demote to ready with completed_at cleared"
grep -q "strike 2" "$HARNESS/run.out" || badf "S2b: demote note missing"
git -C "$REPO" show-ref --verify --quiet refs/heads/wt/t_conf || badf "S2b: branch must survive the demote"
# third strike: card back in the queue after (a simulated) rerun + approval
sqlite3 "$KANBAN_DB" "UPDATE tasks SET status='done',completed_at=200 WHERE id='t_conf';"
run --no-push
if [ "$rc" != 5 ]; then badf "S2c: strike 3 should exit 5, got $rc: $(cat "$HARNESS/run.out")"; fi
grep -qE "create --json --assignee default .*reconciler" "$HARNESS/mock/hermes.log" ||
  badf "S2c: reconciler card not created: $(tail -3 "$HARNESS/mock/hermes.log")"
bn="$(sqlite3 "$KANBAN_DB" "SELECT IFNULL(branch_name,'') FROM tasks WHERE id='t_conf';")"
[ -z "$bn" ] || badf "S2c: original card must be queue-excluded (branch_name empty, got $bn)"
echo "== S3: red gate -> un-land + evidence"
ok

mk_harness
new_board
new_repo
card_row t_red wt/t_red 60
git -C "$REPO" checkout -q -b wt/t_red
echo "red work" >"$REPO/red.txt"
git -C "$REPO" add -A
git -C "$REPO" commit -qm "feat(card): red work"
git -C "$REPO" checkout -q dev
pre="$(git -C "$REPO" rev-parse dev)"
export TRAIN_GATES_CMD="echo intentional red; exit 1"
run --no-push
unset TRAIN_GATES_CMD
if [ "$rc" != 6 ]; then badf "S3: red gate should exit 6, got $rc: $(cat "$HARNESS/run.out")"; fi
[ "$(git -C "$REPO" rev-parse dev)" = "$pre" ] || badf "S3: dev must be un-landed"
[ "$(status_of t_red)" = "ready" ] || badf "S3: red gate must send the card to ready, got $(status_of t_red)"
git -C "$REPO" show-ref --verify --quiet refs/heads/wt/t_red || badf "S3: rebased branch must be kept on a red gate"
git -C "$REPO" rev-parse --verify -q refs/train/0001-t_red >/dev/null && badf "S3: no marker on a red landing"
grep -q "red gate" "$HARNESS/run.out" || badf "S3: red evidence missing: $(cat "$HARNESS/run.out")"
echo "== S4: empty queue + dirty card worktree guard"
ok

mk_harness
new_board
new_repo
run --no-push
if [ "$rc" != 0 ]; then badf "S4a: empty queue must exit 0, got $rc: $(cat "$HARNESS/run.out")"; fi
grep -q "queue empty" "$HARNESS/run.out" || badf "S4a: empty queue message missing"

mk_harness
new_board
new_repo
mkdir -p "$REPO/.worktrees/t_dirty"
git -C "$REPO" worktree add -q --detach "$REPO/.worktrees/t_dirty" dev
echo "uncommitted" >"$REPO/.worktrees/t_dirty/leftover.txt"
card_row t_dirty wt/t_dirty 70 "$REPO/.worktrees/t_dirty"
run --no-push
if [ "$rc" != 2 ]; then badf "S4b: dirty card worktree must abort (rc 2), got $rc: $(cat "$HARNESS/run.out")"; fi
echo "== S5: marker walk (train-bisect)"
ok

mk_harness
new_board
new_repo
for i in 1 2 3; do
  git -C "$REPO" checkout -q -b "wt/t_l$i" dev
  echo "from landing $i" >"$REPO/l$i.txt"
  if [ "$i" = 2 ]; then echo "BUG_TOKEN" >"$REPO/bug.txt"; fi
  git -C "$REPO" add -A
  git -C "$REPO" commit -qm "feat(card): landing $i"
  git -C "$REPO" checkout -q dev
  git -C "$REPO" merge --no-ff -q -m "Merge card t_l$i: landing $i (work by cody)" "wt/t_l$i"
  git -C "$REPO" update-ref "refs/train/$(printf '%04d' "$i")-t_l$i" HEAD
done
TIP="$(git -C "$REPO" rev-parse dev)"
bisect_run --list
if [ "$rc" != 0 ]; then badf "S5a: --list rc $rc: $(cat "$HARNESS/run.out")"; fi
seqs="$(grep -oE '^000[0-9]' "$HARNESS/run.out" | tr -d '\n')"
[ "$seqs" = "000100020003" ] || badf "S5a: --list order wrong (got $seqs)"

probe="$HARNESS/mock/probe.sh"
printf '[ ! -f bug.txt ]\n' >"$probe"
chmod +x "$probe"
export TRAIN_BISECT_CMD="bash $probe"
bisect_run "$TIP"
unset TRAIN_BISECT_CMD
[ "$rc" = 0 ] || badf "S5b: bisect rc $rc: $(cat "$HARNESS/run.out")"
grep -q "culprit: landing 0002-t_l2" "$HARNESS/run.out" || badf "S5b: wrong culprit: $(cat "$HARNESS/run.out")"

bisect_run "$TIP"
grep -q "git bisect start" "$HARNESS/run.out" || badf "S5c: manual recipe missing when no cmd given"

echo "== S6: second landing gets seq 0002 (the real 1st-to-2nd-landing regression)"
ok

mk_harness
new_board
new_repo
# S1-style landing first (marker 0001), then a second card + landing
git -C "$REPO" checkout -q -b wt/t_cardA
echo "cardA work" >"$REPO/cardA.txt"
git -C "$REPO" add -A && git -C "$REPO" commit -qm "feat(card): cardA work"
git -C "$REPO" checkout -q dev
card_row t_cardA wt/t_cardA 100
run --no-push
git -C "$REPO" rev-parse --verify -q refs/train/0001-t_cardA >/dev/null || badf "S6: first marker missing"
git -C "$REPO" checkout -q -b wt/t_cardB
echo "cardB work" >"$REPO/cardB.txt"
git -C "$REPO" add -A && git -C "$REPO" commit -qm "feat(card): cardB work"
branch_tip="$(git -C "$REPO" rev-parse wt/t_cardB)"
git -C "$REPO" checkout -q dev
card_row t_cardB wt/t_cardB 200
run --no-push
if [ "$rc" != 0 ]; then badf "S6: second landing rc $rc: $(cat "$HARNESS/run.out")"; fi
git -C "$REPO" rev-parse --verify -q refs/train/0002-t_cardB >/dev/null || badf "S6: SEQUENCE BROKEN - second marker must be 0002, not a crashed re-0001"
[ "$(git -C "$REPO" rev-parse dev)" = "$(git -C "$REPO" rev-parse refs/train/0002-t_cardB)" ] ||
  badf "S6: 0002 marker must sit at dev tip"
[ "$(git -C "$REPO" rev-parse dev^2)" = "$branch_tip" ] ||
  badf "S6: the landing must be a real two-parent merge (branch side == replayed tip)"
[ "$(git -C "$REPO" rev-parse dev^1)" = "$(git -C "$REPO" rev-parse refs/train/0001-t_cardA)" ] ||
  badf "S6: the merge's first parent must be the previous dev tip (0001 landing)"
# train-bisect --list must walk both markers in order
bisect_run --list
seqs="$(grep -oE '^000[0-9]' "$HARNESS/run.out" | tr -d '\n')"
[ "$seqs" = "00010002" ] || badf "S6: --list chain wrong (got $seqs)"

echo "== S6b: zero-replay (patch already on dev) is LOUD, never a silent empty landing"
ok

mk_harness
new_board
new_repo
echo "v0" >"$REPO/x.txt"
git -C "$REPO" add -A && git -C "$REPO" commit -qm "base v0"
# branch: change x.txt to v1
git -C "$REPO" checkout -q -b wt/t_dup
echo "v1" >"$REPO/x.txt"
git -C "$REPO" commit -qam "fix(card): x.txt v1"
# dev: the SAME change lands via dev's own commit first
git -C "$REPO" checkout -q dev
echo "v1" >"$REPO/x.txt"
git -C "$REPO" commit -qam "fix(dev): x.txt v1 (independent)"
card_row t_dup wt/t_dup 300
pre="$(git -C "$REPO" rev-parse dev)"
run --no-push
if [ "$rc" != 4 ]; then badf "S6b: zero-replay must demote (rc 4), got $rc: $(cat "$HARNESS/run.out")"; fi
[ "$(git -C "$REPO" rev-parse dev)" = "$pre" ] || badf "S6b: dev untouched on anomaly"
[ "$(status_of t_dup)" = "ready" ] || badf "S6b: card demoted to ready, got $(status_of t_dup)"
git -C "$REPO" rev-parse --verify -q refs/train/0001-t_dup >/dev/null && badf "S6b: no marker on an aborted landing"
git -C "$REPO" show-ref --verify --quiet refs/heads/wt/t_dup || badf "S6b: branch kept on anomaly"
grep -q "replay anomaly" "$HARNESS/run.out" || badf "S6b: anomaly evidence missing"

echo "== S6c: an initially-empty commit branch still lands a REAL two-parent merge"
ok

mk_harness
new_board
new_repo
card_row t_emp wt/t_emp 400
git -C "$REPO" checkout -q -b wt/t_emp
git -C "$REPO" commit -q --allow-empty -m "empty commit on card branch"
git -C "$REPO" checkout -q dev
pre="$(git -C "$REPO" rev-parse dev)"
run --no-push
# git keeps initially-empty commits during replay, so the landing is a
# genuine two-parent merge with no content drift - never "already up to
# date": the train's no-op-merge guard keeps that path impossible.
if [ "$rc" != 0 ]; then badf "S6c: expected rc 0, got $rc: $(cat "$HARNESS/run.out")"; fi
[ "$(git -C "$REPO" rev-parse dev^2)" != "" ] || badf "S6c: landing has no branch-side parent"
[ "$(git -C "$REPO" rev-parse dev)" = "$(git -C "$REPO" rev-parse refs/train/0001-t_emp)" ] ||
  badf "S6c: marker at dev tip"
[ "$(status_of t_emp)" = "done" ] || badf "S6c: card stays done, got $(status_of t_emp)"
git -C "$REPO" diff-tree -r "$pre" HEAD | grep . && badf "S6c: no content drift from an empty-branch landing"

echo "== S6d: patch dev applied+reverted earlier gets REAPPLIED (cherry-pick suppression)"
ok

mk_harness
new_board
new_repo
echo "keep" >"$REPO/keep.txt"
git -C "$REPO" add -A && git -C "$REPO" commit -qm "base A (smoke proof present)"
git -C "$REPO" tag baseA
# dev: remove the proof (the same patch the branch will carry), then a fixture commit re-adds it
git -C "$REPO" rm -q keep.txt
git -C "$REPO" commit -qm "docs(dev): dev removes the proof"
echo "keep" >"$REPO/keep.txt"
git -C "$REPO" add -A && git -C "$REPO" commit -qm "fix(dev): fixture re-adds the proof"
# branch from baseA: the removal (patch-id identical to dev's earlier removal)
git -C "$REPO" checkout -q -b wt/t_skip baseA
git -C "$REPO" rm -q keep.txt
git -C "$REPO" commit -qm "fix(card): remove the proof (same patch)"
git -C "$REPO" checkout -q dev
card_row t_skip wt/t_skip 500
run --no-push
if [ "$rc" != 0 ]; then badf "S6d: reapply must land (rc 0), got $rc: $(cat "$HARNESS/run.out")"; fi
[ -f "$REPO/keep.txt" ] && badf "S6d: the landing must re-remove the proof from dev"
[ "$(git -C "$REPO" rev-parse dev)" = "$(git -C "$REPO" rev-parse refs/train/0001-t_skip)" ] ||
  badf "S6d: marker at dev tip"
[ "$(status_of t_skip)" = "done" ] || badf "S6d: card stays done, got $(status_of t_skip)"

echo ""
echo "== S7: NO caller env - the board db resolves from the live home (host-remap)"
ok

mk_harness
new_repo
new_board "$HARNESS/app/hermes/kanban/boards/project-omega/kanban.db"
card_branch t_live 600
run_nocaller_env --no-push
if [ "$rc" != 0 ]; then badf "S7: expected rc 0 with no caller env, got $rc: $(cat "$HARNESS/run.out")"; fi
grep -q "board db: .*app/hermes/kanban/boards/project-omega/kanban.db" "$HARNESS/run.out" ||
  badf "S7: the pass must name the live-home db it resolved: $(cat "$HARNESS/run.out")"
grep -q "DONE t_live" "$HARNESS/run.out" || badf "S7: no landing: $(cat "$HARNESS/run.out")"
[ "$(git -C "$REPO" rev-parse dev)" = "$(git -C "$REPO" rev-parse refs/train/0001-t_live)" ] ||
  badf "S7: marker must sit at dev tip"

echo "== S8: no candidate anywhere -> refuse exit 2 naming every probed path"
ok

mk_harness
new_repo
new_board "$HARNESS/elsewhere/kanban.db"
card_branch t_nowhere 610
pre="$(git -C "$REPO" rev-parse dev)"
run_nocaller_env --no-push
if [ "$rc" != 2 ]; then badf "S8: a pass with no board db must refuse exit 2, got $rc: $(cat "$HARNESS/run.out")"; fi
grep -q "no board db for board 'project-omega'" "$HARNESS/run.out" || badf "S8: refusal must name the board: $(cat "$HARNESS/run.out")"
grep -q "Probed, in order" "$HARNESS/run.out" || badf "S8: refusal must name the probes: $(cat "$HARNESS/run.out")"
grep -q "app/hermes/kanban/boards/project-omega/kanban.db" "$HARNESS/run.out" || badf "S8: windows live home not probed"
grep -q "\.hermes/kanban/boards/project-omega/kanban.db" "$HARNESS/run.out" || badf "S8: POSIX live home not probed"
grep -q "DONE t_nowhere" "$HARNESS/run.out" && badf "S8: nothing may land without a board"
[ "$(git -C "$REPO" rev-parse dev)" = "$pre" ] || badf "S8: dev must be untouched"
git -C "$REPO" show-ref --verify --quiet refs/heads/wt/t_nowhere || badf "S8: the card branch must survive a precondition refusal"

echo "== S9: an explicit KANBAN_DB that names no file refuses, never falls back"
ok

mk_harness
new_repo
# the live-home candidate EXISTS - the explicit dead path must still refuse
new_board "$HARNESS/app/hermes/kanban/boards/project-omega/kanban.db"
card_branch t_pin 620
pre="$(git -C "$REPO" rev-parse dev)"
rc=0
( cd "$REPO" && KANBAN_DB="$HARNESS/dead/kanban.db" "$TRAIN" --no-push >"$HARNESS/run.out" 2>&1 ) || rc=$?
if [ "$rc" != 2 ]; then badf "S9: a dead explicit KANBAN_DB must refuse exit 2, got $rc: $(cat "$HARNESS/run.out")"; fi
grep -q "KANBAN_DB is set but there is no board db at .*dead/kanban.db" "$HARNESS/run.out" ||
  badf "S9: refusal must name the dead override: $(cat "$HARNESS/run.out")"
grep -q "DONE t_pin" "$HARNESS/run.out" && badf "S9: an explicit dead override must never land a neighbour board"
[ "$(git -C "$REPO" rev-parse dev)" = "$pre" ] || badf "S9: dev must be untouched"

echo "== S10a: journeys gate with no derivable lane home refuses BEFORE the merge"
ok

mk_harness
new_repo
new_board
fixture_journeys
git -C "$REPO" add -A
git -C "$REPO" commit -qm "test(fixture): journeys suite"
card_branch t_nolane 630
pre="$(git -C "$REPO" rev-parse dev)"
run --no-push
if [ "$rc" != 2 ]; then badf "S10a: no lane home must refuse exit 2, got $rc: $(cat "$HARNESS/run.out")"; fi
grep -q "cannot run green without a lane home" "$HARNESS/run.out" || badf "S10a: refusal must name the lane-home gap: $(cat "$HARNESS/run.out")"
[ "$(git -C "$REPO" rev-parse dev)" = "$pre" ] || badf "S10a: the refusal must come BEFORE the merge (dev moved)"
[ "$(status_of t_nolane)" = "done" ] || badf "S10a: a precondition refusal never demotes, got $(status_of t_nolane)"
git -C "$REPO" show-ref --verify --quiet refs/heads/wt/t_nolane || badf "S10a: branch must survive"
sqlite3 "$KANBAN_DB" "SELECT count(*) FROM task_comments WHERE task_id='t_nolane';" | grep -q '^0$' ||
  badf "S10a: a precondition refusal posts nothing (no strike, no red-gate evidence)"

echo "== S10b: <checkout>/bin is derived and the gate inherits OMEGA_HOME/OMEGA_BIN"
ok

mk_harness
new_repo
new_board
fixture_journeys
fixture_lane_home
git -C "$REPO" add -A
git -C "$REPO" commit -qm "test(fixture): journeys suite + lane home"
card_branch t_lane 640
lane_stub
export JOURNEY_WITNESS="$HARNESS/journey.env"
run_with_lane ok
unset JOURNEY_WITNESS
if [ "$rc" != 0 ]; then badf "S10b: expected rc 0 with a derived lane home, got $rc: $(cat "$HARNESS/run.out")"; fi
grep -q "journeys lane home (derived): .*bin" "$HARNESS/run.out" || badf "S10b: derivation line missing: $(cat "$HARNESS/run.out")"
[ -f "$HARNESS/journey.env" ] || badf "S10b: the journeys command never ran"
grep -q "OMEGA_HOME=.*/repo/bin" "$HARNESS/journey.env" || badf "S10b: OMEGA_HOME must reach the gate: $(cat "$HARNESS/journey.env")"
grep -q "OMEGA_BIN=.*/repo/bin/omega" "$HARNESS/journey.env" || badf "S10b: OMEGA_BIN must reach the gate: $(cat "$HARNESS/journey.env")"
grep -q "DONE t_lane" "$HARNESS/run.out" || badf "S10b: no landing: $(cat "$HARNESS/run.out")"

echo "== S10c: a dead lane refuses BEFORE the merge (never a post-merge demote)"
ok

mk_harness
new_repo
new_board
fixture_journeys
fixture_lane_home
git -C "$REPO" add -A
git -C "$REPO" commit -qm "test(fixture): journeys suite + lane home"
card_branch t_deadlane 650
lane_stub
pre="$(git -C "$REPO" rev-parse dev)"
run_with_lane dead
if [ "$rc" != 2 ]; then badf "S10c: a dead lane must refuse exit 2, got $rc: $(cat "$HARNESS/run.out")"; fi
grep -q "is not healthy" "$HARNESS/run.out" || badf "S10c: refusal must name the lane health: $(cat "$HARNESS/run.out")"
[ "$(git -C "$REPO" rev-parse dev)" = "$pre" ] || badf "S10c: the refusal must come BEFORE the merge (dev moved)"
[ "$(status_of t_deadlane)" = "done" ] || badf "S10c: a lane red at precondition time never demotes, got $(status_of t_deadlane)"
grep -q "DONE t_deadlane" "$HARNESS/run.out" && badf "S10c: nothing may land on a dead lane"

echo ""
echo "train-pass_test: $pass ok, $fail failed"
[ "$fail" -eq 0 ] || exit 1
exit 0