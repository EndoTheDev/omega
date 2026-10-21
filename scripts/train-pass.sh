#!/usr/bin/env bash
set -euo pipefail

# train-pass.sh - the merge train: ONE mechanical, no-LLM landing per pass.
#
# Picks the oldest review-approved card (status 'done', its wt/<task_id>
# branch still alive and carrying commits that are not on dev yet),
# rebases that branch onto the current dev tip, runs the full gate set on
# the merged tree, lands with a merge commit, pushes dev, and writes a
# MARKER REF: refs/train/<seq>-<task_id> at the merge commit. The marker
# chain is the bisect ladder - regressions get walked mechanically by
# scripts/train-bisect.sh, never by archaeology.
#
# Failure laws:
# - rebase conflict: abort, card untouched (still done + queued), one
#   strike comment. Strike 2 = auto-demote (card back to ready with the
#   conflict note; the worker rerun rebases onto the new dev tip). A third
#   strike = reconciler agent card instead of another retry.
# - replay anomaly: the rebase zero-replayed the branch's unique commits
#   (fork-point fast-forward, dev already carrying the same patches) or the
#   merge degenerated to a two-parent-less no-op. Both are LOUD: evidence
#   comment, card demoted to ready, branch kept. The train never reports
#   "landed" without a real merge commit with the branch's patches in it.
# - red gate: un-land (reset to the pre-pass tip), failure evidence
#   comment, card back to ready. The rebased branch is KEPT - the red is
#   the work's problem against dev, not the rebase's.
# - one landing per pass. Run it again for the next card.
#
# Board interaction: comments and result updates go through the hermes CLI
# (override with HERMES); when that CLI is not available in the calling
# context the train still lands - comments degrade to a direct SQLite
# append against the board DB, and demotes are always direct SQLite
# (the CLI has no done -> ready verb). A failed demote aborts the pass
# loudly; it never stays silent.
#
# Usage (from the main checkout, on dev):
#
#     scripts/train-pass.sh [--no-push]
#
# Environment:
#
#   HERMES                     hermes CLI binary (default: hermes)
#   HERMES_KANBAN_BOARD        board slug for the CLI (when not the active one)
#   KANBAN_DB                  board db path (default: active board's db;
#                              falls back to the project-omega path)
#   TRAIN_GATES_CMD            test override - replaces the whole gate list
#   TRAIN_JOURNEYS_CMD         the validation-journeys suite
#                              (default: scripts/journeys/run.sh; missing
#                              = skipped loudly until the companion card
#                              lands)
#   TRAIN_RECONCILER_ASSIGNEE  profile that takes third-strike cards
#                              (default: default)
#   TRAIN_NO_PUSH=1            same as --no-push
#
# Exit codes: 0 landed (or empty queue), 2 usage/precondition, 3 rebase
# strike (card stays queued), 4 demoted to ready (rebase strikes, replay
# anomalies, no-op merges), 5 third strike -> reconciler card, 6 red gate
# -> card back to ready.

BASE="dev"
PUSH=1
[ "${TRAIN_NO_PUSH:-}" = "1" ] && PUSH=0
HERMES_BIN="${HERMES:-hermes}"
JOURNEYS_CMD="${TRAIN_JOURNEYS_CMD:-scripts/journeys/run.sh}"
RECONCILER_ASSIGNEE="${TRAIN_RECONCILER_ASSIGNEE:-default}"

die() { echo "FATAL: train-pass: $*" >&2; exit 2; }
note() { echo "==> train: $*"; }

usage() {
  cat >&2 <<'EOF'
usage: train-pass.sh [--no-push]

Run from the main checkout (~/dev/omega), on the dev branch, clean tree.
EOF
  exit 2
}

while [ $# -gt 0 ]; do
  case "$1" in
    --no-push) PUSH=0 ;;
    -h|--help) usage ;;
    *) usage ;;
  esac
  shift
done

REPO="$(git rev-parse --show-toplevel 2>/dev/null)" || die "run this inside the omega checkout"
cd "$REPO"

cur="$(git symbolic-ref --short HEAD 2>/dev/null || true)"
[ "$cur" = "$BASE" ] || die "HEAD is on '$cur', not $BASE - the train lands from the main checkout on dev"
[ -z "$(git status --porcelain)" ] || die "main checkout is dirty - commit or clean before a train pass"

# The landing must sit on top of what everyone else landed.
if git remote 2>/dev/null | grep -q '^origin$'; then
  git fetch -q origin "$BASE"
  if ! git merge-base --is-ancestor "origin/$BASE" "$BASE"; then
    die "origin/$BASE diverged from local $BASE - resolve by hand, the train never rewrites"
  fi
fi

# ---- board access ------------------------------------------------------

now() { date +%s; }

KANBAN_DB="${KANBAN_DB:-${HERMES_KANBAN_DB:-$HOME/.hermes/kanban/boards/project-omega/kanban.db}}"
[ -f "$KANBAN_DB" ] || die "board db not found at $KANBAN_DB (set KANBAN_DB)"

# sqlq <text>: escape single quotes for a SQL literal.
sqlq() { sed "s/'/''/g" <<<"$1"; }

# board_comment <task_id> <body>: hermes CLI first; direct SQLite append
# as the fallback so a train run never loses its evidence trail.
board_comment() {
  local tid="$1" body="$2" cliargs=()
  cliargs=("$tid" "$body")
  if [ -n "${HERMES_KANBAN_BOARD:-}" ]; then
    cliargs=("--board" "$HERMES_KANBAN_BOARD" "${cliargs[@]}")
  fi
  if "$HERMES_BIN" kanban comment "${cliargs[@]}" >/dev/null 2>&1; then
    return 0
  fi
  sqlite3 "$KANBAN_DB" "INSERT INTO task_comments(task_id,author,body,created_at)
    VALUES('$(sqlq "$tid")','train','$(sqlq "$body")',$(now));"
}

# task_field <task_id> <column> on stdout.
task_field() {
  sqlite3 -readonly "$KANBAN_DB" "SELECT $2 FROM tasks WHERE id='$(sqlq "$1")';"
}

# strike_count <task_id>: how many rebase-conflict strikes this card has
# already eaten.
strike_count() {
  sqlite3 -readonly "$KANBAN_DB" \
    "SELECT count(*) FROM task_comments
      WHERE task_id='$(sqlq "$1")'
        AND body LIKE 'train: rebase conflict (strike%';"
}

# demote_card <task_id> <reason>: done -> ready (or todo when an
# incomplete parent gate applies), completed_at cleared, promoted_manual
# event appended - the same flip the dashboard drag performs.
demote_card() {
  local tid="$1" reason="$2"
  local upd
  upd="$(sqlite3 "$KANBAN_DB" "
  UPDATE tasks
     SET status = (CASE WHEN (
             SELECT count(*) FROM task_links l JOIN tasks p ON p.id = l.parent_id
              WHERE l.child_id = '$(sqlq "$tid")'
                AND p.status NOT IN ('done','archived')
         ) > 0 THEN 'todo' ELSE 'ready' END),
         completed_at = NULL,
         current_run_id = NULL,
         claim_lock = NULL,
         claim_expires = NULL,
         worker_pid = NULL
   WHERE id = '$(sqlq "$tid")' AND status = 'done';
  SELECT changes();")"
  if [ "$upd" != "1" ]; then
    die "demote of $tid did not move exactly one row (got $upd) - board state not what train-pass expects"
  fi
  sqlite3 "$KANBAN_DB" "INSERT INTO task_events(task_id, kind, payload, created_at)
       VALUES('$(sqlq "$tid")','promoted_manual',
              '{\"actor\": \"train-pass\", \"reason\": \"$(sqlq "$reason")\"}',
              $(now));"
}

# reconcile_card <task_id> <strikes> <branch>: third strike - a reconciler
# agent card takes the rebase, the original card is excluded from the
# queue (branch_name cleared) instead of being retried forever.
reconcile_card() {
  local tid="$1" strikes="$2" branch="$3"
  local title="reconciler: rebase landing for $tid ($strikes rebase strikes)"
  local body
  body=$(cat <<EOF
The merge train hit a third rebase strike on $tid (branch $branch): the
card's rebases keep conflicting against a moving dev. This card takes the
rebase reconciliation - resolve the branch onto the dev tip by hand or
rework the landing, then land through the train. Original card: $tid
(excluded from the train queue by clearing its branch_name; its branch
stays for archaeology).
EOF
)
  printf '%s\n' "$body" | "$HERMES_BIN" kanban create --json \
    --assignee "$RECONCILER_ASSIGNEE" --body-file - "$title" >/dev/null 2>&1 || {
    echo "FATAL: train-pass: third strike on $tid but the reconciler card could not" >&2
    echo "       be created (hermes CLI refused). Create it by hand:" >&2
    echo "         hermes kanban create --assignee $RECONCILER_ASSIGNEE '$title'" >&2
    die "reconciler card creation failed"
  }
  sqlite3 "$KANBAN_DB" "
  UPDATE tasks SET branch_name = NULL WHERE id = '$(sqlq "$tid")' AND status = 'done';
  SELECT changes();"
  sqlite3 "$KANBAN_DB" "INSERT INTO task_events(task_id, kind, payload, created_at)
       VALUES('$(sqlq "$tid")','commented',
              '{\"author\": \"train\", \"note\": \"third strike ($strikes) - handed to the reconciler, queue-excluded\"}',
              $(now));
"
}

# ---- queue -------------------------------------------------------------

# Oldest first: done cards whose wt branch still exists and still carries
# commits that are not on dev (a landed branch is an ancestor and gets
# swept at the end of its own landing).
mapfile -t CANDIDATES < <(sqlite3 -readonly -separator ' ' "$KANBAN_DB" "
  SELECT id, branch_name FROM tasks
   WHERE status = 'done'
     AND branch_name IS NOT NULL AND branch_name != ''
   ORDER BY completed_at ASC, id ASC;")

pick=""
for cand in "${CANDIDATES[@]}"; do
  c="${cand%% *}"; b="${cand#* }"
  [ -n "$b" ] || continue
  if git show-ref --verify --quiet "refs/heads/$b" \
     && ! git merge-base --is-ancestor "$b" "$BASE" 2>/dev/null; then
    pick="$c|$b"
    break
  fi
done

if [ -z "$pick" ]; then
  echo "train: queue empty."
  exit 0
fi
tid="${pick%%|*}"
bra="${pick#*|}"

title="$(task_field "$tid" "title" || true)"
who="$(task_field "$tid" "assignee" || true)"
[ -n "$who" ] || who="?"
note "oldest queued card: $tid ($title), branch $bra"

# ---- free the branch ---------------------------------------------------

ws="$(task_field "$tid" "workspace_path" || true)"
if [ -n "$ws" ] && [ -d "$ws" ]; then
  if [ -n "$(git -C "$ws" status --porcelain 2>/dev/null)" ]; then
    echo "FATAL: train-pass: done card $tid has $ws dirty - uncommitted work in a closed card;" >&2
    echo "       the train never destroys unclaimed state. A human looks at it." >&2
    exit 2
  fi
  git worktree remove -f "$ws" >/dev/null 2>&1 || die "could not free the card worktree $ws (rm the worktree by hand)"
  note "freed card worktree $ws"
fi

# ---- rebase + merge ----------------------------------------------------

# branch_patchids <start> <exclude> -> sorted patch-id list of the commits
# unique to <start> vs <exclude>. The replay-parity guard compares this
# before/after the rebase: a fork-point fast-forward or an already-in-dev
# patch that zero-replays MUST show up as a parity break, never as a
# silent empty landing.
branch_patchids() {
  local sha
  for sha in $(git rev-list --no-merges --reverse "$1" "^$2"); do
    git show "$sha" | git patch-id | awk '{print $1}'
  done | sort
}

pre="$(git rev-parse HEAD)"
pre_unique="$(branch_patchids "$bra" "$pre")"

conflict_step() {
  # Conflicted file list comes FIRST - the abort wipes the index state.
  local files
  files="$(git diff --name-only --diff-filter=U 2>/dev/null | head -8 || true)"
  git rebase --abort 2>/dev/null || true
  git merge --abort 2>/dev/null || true
  git checkout -q -f "$BASE"
  git reset -q --hard "$pre" 2>/dev/null || true
  local n body
  n=$(($(strike_count "$tid") + 1))
  body="train: rebase conflict (strike $n) on $bra vs $BASE - conflict in: ${files:-<n/a>}. Strike 2 demotes; strike 3 goes to the reconciler."
  board_comment "$tid" "$body"
  note "rebase conflict, strike $n"
  if [ "$n" -ge 3 ]; then
    note "third strike - reconciler card instead of another retry"
    reconcile_card "$tid" "$n" "$bra"
    exit 5
  elif [ "$n" -eq 2 ]; then
    note "strike 2 - auto-demote: card back to ready with the conflict note"
    demote_card "$tid" \
      "merge train: rebase conflict vs $BASE ($n strikes). On $bra - rebase onto the new dev tip in the rerun."
    exit 4
  fi
  note "strike 1 - card stays queued, the next pass retries"
  exit 3
}

git checkout -q "$bra" 2>/dev/null || die "cannot check out $bra locally"
if ! git rebase --no-fork-point "$BASE" >/dev/null 2>&1; then
  git checkout -q "$BASE" 2>/dev/null || true
  conflict_step
fi
git checkout -q "$BASE"

# Replay-parity: the rebase must have replayed exactly what the branch
# had. A zero-replay (fork-point fast-forward, patch already on dev) would
# otherwise flow into an "already up to date" merge and land nothing.
post_unique="$(branch_patchids "$bra" "$pre")"
if [ "$pre_unique" != "$post_unique" ]; then
  pre_n="$(awk 'END{print NR}' <<<"$pre_unique")"
  post_n="$(awk 'END{print NR}' <<<"$post_unique")"
  note "rebase replay anomaly: branch carried $pre_n unique commits, $post_n replayed - aborting"
  board_comment "$tid" "train: rebase replay anomaly on $bra rebased onto $BASE - branch carried $pre_n unique commits, $post_n were replayed. Landing ABORTED, the branch is kept, card back to ready with this evidence."
  demote_card "$tid" "merge train: rebase replay anomaly ($pre_n unique commits zero-replayed). On $bra - verify the branch content against dev, then rerun."
  exit 4
fi
if ! git merge --no-ff -q -m "Merge card $tid: $(cut -c1-60 <<<"$title") (work by $who)" "$bra" 2>/dev/null; then
  note "conflict at the merge stage (post-rebase)"
  conflict_step
fi
mergesha="$(git rev-parse HEAD)"
if [ "$mergesha" = "$pre" ] || ! git rev-parse -q --verify "$mergesha^2" >/dev/null 2>&1; then
  git reset -q --hard "$pre" >/dev/null 2>&1 || true
  note "landing aborted: merge of $bra produced no two-parent merge commit (mergesha was $mergesha)"
  board_comment "$tid" "train: the merge of $bra degenerated to a no-op (mergesha = $mergesha = pre-pass tip or single-parent). Landing ABORTED, dev untouched, card back to ready with this evidence."
  demote_card "$tid" "merge train: no-op merge detected. On $bra - a real merge commit is required to land."
  exit 4
fi
note "merge commit $mergesha"

# ---- gates on the merged tree -----------------------------------------

gates_log="$(mktemp "${TMPDIR:-/tmp}"/train-gates.XXXXXX.log)"

gates_pass() {
  set -e
  if [ -n "${TRAIN_GATES_CMD:-}" ]; then
    echo "TEST GATE OVERRIDE ACTIVE (TRAIN_GATES_CMD): it is the ONLY gate"
    bash -c "$TRAIN_GATES_CMD"
    echo "FIN"
    return 0
  fi
  if [ -f go.mod ] && command -v go >/dev/null 2>&1; then
    pkglist="$(go list ./... | grep -v '/bin/' || true)"
    if [ -n "$pkglist" ]; then
      echo "==> go vet"
      go vet $pkglist
      echo "==> dep law"
      ./gate-deps.sh
      echo "==> go test"
      go test $pkglist
      # -race on the package(s) the landing actually touches.
      if git diff --name-only "$pre" HEAD | grep -q '\.go$'; then
        pkgmod="$(go list -m -f '{{.Path}}')"
        raceargs=()
        for d in $(git diff --name-only "$pre" HEAD | grep '\.go$' | xargs -n1 dirname | sort -u); do
          want="$pkgmod/$d"
          if grep -qFe "$want" <<<"$pkglist"; then
            raceargs+=("./$d")
          fi
        done
        if [ "${#raceargs[@]}" -gt 0 ]; then
          echo "==> go test -race on touched packages: ${raceargs[*]}"
          go test -race "${raceargs[@]}"
        fi
      fi
    fi
  fi
  if [ -d modules/ui/test ] && command -v node >/dev/null 2>&1; then
    echo "==> webui node suite"
    (cd modules/ui && node --test 'test/*.test.ts' >/dev/null 2>&1) || {
      echo "webui suite failed - rerun (cd modules/ui && node --test 'test/*.test.ts') for detail" >&2
      exit 1
    }
  fi
  echo "==> embed lane"
  if [ -f scripts/embed-lane.sh ]; then
    scripts/embed-lane.sh --base "$pre" "card $tid"
  else
    echo "embed lane absent in this checkout - skipping (static/ change gate lives in the lane)"
  fi
  if [ -f "$JOURNEYS_CMD" ]; then
    echo "==> validation journeys"
    "$JOURNEYS_CMD" || exit 1
  elif [ -n "${TRAIN_JOURNEYS_CMD:-}" ]; then
    echo "journeys command is set but missing: $JOURNEYS_CMD" >&2
    exit 1
  else
    echo "==> validation journeys: SKIPPED - $JOURNEYS_CMD not present yet (the companion card wires it in)"
  fi
  echo "FIN"
}
# Run the gate list in a fresh subshell. The -e must be OFF at this
# statement: ON it kills the whole pass on gate failure (no handler), and
# a `subshell ||` context suspends -e inside the subshell (every red
# sailed through green). Sandwich it instead.
set +e
( gates_pass ) >"$gates_log" 2>&1
gates_rc=$?
set -e
if [ "$gates_rc" != "0" ]; then
  tail -25 "$gates_log" >&2 || true
  git reset -q --hard "$pre"
  git clean -qfd modules/http_channel/static >/dev/null 2>&1 || true
  note "red gate (rc $gates_rc) - un-landed: dev back to $pre"
  board_comment "$tid" "train: red gate on $mergesha - un-landed, back to ready. Failure evidence (tail):

$(tail -c 1200 "$gates_log")

Full gate log: $gates_log"
  demote_card "$tid" \
    "merge train: red gate - $mergesha un-landed (rc $gates_rc). Failure tail on the card; rebased branch $bra is KEPT."
  exit 6
fi
grep -q '^FIN$' "$gates_log" || {
  git reset -q --hard "$pre"
  die "gate list broke its own structure (no FIN marker) - un-landed, dev back to $pre"
}
note "gates green"

# ---- land --------------------------------------------------------------

have_origin() { git remote 2>/dev/null | grep -q '^origin$'; }

# marker: refs/train/<seq>-<task_id> at the merge commit. Zero-padded seq
# keeps refname sort == chronological order, which train-bisect walks on.
# FULL refnames only: %(refname:short) elides refs/ -> 'train/0001-x',
# which once made the seq parse crash and reset the chain to 0001 forever
# (found on the 2nd real landing). The marker is written BEFORE the push
# so a landing never reaches origin without its marker.
last_ref="$(git for-each-ref --format='%(refname)' refs/train 2>/dev/null | sort | tail -1)"
last_num=0
if [ -n "$last_ref" ]; then
  tmp="${last_ref##*/}"   # refs/train/0001-t_x -> 0001-t_x
  tmp="${tmp%%-*}"        # -> 0001
  case "$tmp" in
    ''|*[!0-9]*) die "latest marker '$last_ref' carries no numeric seq - the marker chain is corrupted; resolve by hand, the train never guesses" ;;
  esac
  last_num=$((10#$tmp))
fi
seq=$((last_num + 1))
s="$(printf '%04d' "$seq")"
marker="refs/train/$s-$tid"
git update-ref "$marker" "$mergesha"
if [ "$PUSH" = "1" ] && have_origin; then
  git push -q origin "$BASE"
  git push -q origin "refs/train/$s-$tid:refs/train/$s-$tid" \
    || echo "    (marker push refused - the marker is local; push refs/train/* by hand later)"
fi

note "landed $tid at $mergesha; marker ref $marker"

# ---- board bookkeeping + branch sweep -----------------------------------

board_comment "$tid" "train: landed $mergesha on $BASE, marker ref $marker."
"$HERMES_BIN" kanban edit "$tid" \
  --summary "landed via merge train: $mergesha, marker $marker" \
  --metadata "{\"landed_by\":\"train-pass\",\"merge_sha\":\"$mergesha\",\"marker\":\"refs/train/$s-$tid\"}" \
  >/dev/null 2>&1 || note "(result edit skipped: the hermes CLI refused in this context; the landing comment is on the card)"

git branch -d -q "$bra" 2>/dev/null || true
echo "train: DONE $tid -> $mergesha, marker $marker"