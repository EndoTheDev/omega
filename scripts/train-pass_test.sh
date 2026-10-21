#!/usr/bin/env bash
set -uo pipefail

# train-pass_test.sh - scratch-repo tests for scripts/train-pass.sh and
# scripts/train-bisect.sh.
# Run: bash scripts/train-pass_test.sh
#
# The hermes CLI is a stub; the board is a fixture sqlite db carrying the
# few tables the train touches (same columns as the live schema). Gates
# are overridden with TRAIN_GATES_CMD - the real gate list is repo-shaped
# (go.mod / modules/ui / journeys presence) and is exercised for real on
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
}

new_board() {
  KANBAN_DB="$HARNESS/board/kanban.db"
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

echo ""
echo "train-pass_test: $pass ok, $fail failed"
[ "$fail" -eq 0 ] || exit 1
exit 0