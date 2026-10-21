#!/usr/bin/env bash
set -euo pipefail

# review-at-close.sh - the review-at-close contract, as a script.
#
# A reviewer pass NEVER re-reads a stale pre-review: the review happens at
# close time on a FRESH worktree with a COLD diff vs the merge-base with
# dev - the diff that would land, not the diff that existed when the card
# was created.
#
# Usage:
#
#     scripts/review-at-close.sh <task_id>[.suffix] [--diff] [--keep]
#
#   <task_id>  card id; reviews wt/<task_id>. Pass --branch to override.
#   --diff     print the full cold diff to stdout (default: stat + commit
#              list on stdout, full diff to a file next to the worktree)
#   --keep     keep the review worktree when done (default: removed)
#
# Output (stdout, machine-greppable last line):
#
#     review: <task_id> branch=<wt/...> tip=<sha> base=<sha> worktree=<path> cold_diff=<path>
#
# The reviewer reads; the script touches nothing but .worktrees/ and the
# diff file. No gates run here - the train runs the gates on the merged
# tree. Review is read-only (repo policy).

die() { echo "FATAL: review-at-close: $*" >&2; exit 2; }

task_id="" branch="" show_diff=0 keep=0
while [ $# -gt 0 ]; do
  case "$1" in
    --branch) branch="${2:?--branch needs a value}"; shift 2 ;;
    --diff) show_diff=1 ;;
    --keep) keep=1 ;;
    -h|--help)
      sed -n '2,20p' "$0"; exit 0 ;;
    -*) die "unknown option $1" ;;
    *)
      if [ -n "$task_id" ]; then die "one task id only"; fi
      task_id="$1" ;;
  esac
  shift
done
[ -n "$task_id$branch" ] || die "usage: review-at-close <task_id> [--diff] [--keep] (or --branch <br>)"

repo="$(git rev-parse --show-toplevel 2>/dev/null)" || die "run this inside the omega checkout"
cd "$repo"
BASE="dev"

if [ -z "$branch" ]; then
  branch="wt/$task_id"
fi
git show-ref --verify --quiet "refs/heads/$branch" || die "no branch $branch - the card has no landable work"
tip="$(git rev-parse "$branch")"
mb="$(git merge-base "$branch" "$BASE" 2>/dev/null)" || die "merge-base with $BASE failed"

wt=".worktrees/review-$task_id"
git worktree remove -f --force "$wt" 2>/dev/null || true
git worktree add -q --detach "$wt" "$branch" >/dev/null

commits="$(git log --oneline "$mb..$branch")"
stat="$(git diff --stat "$mb" "$branch")"
diff_file="/tmp/review-$task_id.cold.diff"
git diff "$mb" "$branch" >"$diff_file"

echo "commits ($mb..$branch):"
echo "$commits" | head -20
[ -n "$commits" ] || echo "  (none - empty review? the branch carries nothing over $BASE)"
echo
echo "cold diff vs $BASE merge-base $(git rev-parse --short "$mb"):"
echo "$stat" | tail -5
echo
echo "full cold diff: $diff_file"
if [ "$show_diff" = "1" ]; then
  echo
  git diff "$mb" "$branch"
fi

if [ "$keep" != "1" ]; then
  git worktree remove -f --force "$wt"
  kept="(removed)"
else
  kept="$repo/$wt"
fi

echo "review: $task_id branch=$branch tip=$(git rev-parse --short "$branch") base=$(git rev-parse --short "$mb") worktree=$kept cold_diff=$diff_file"