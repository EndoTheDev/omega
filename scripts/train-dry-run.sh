#!/usr/bin/env bash
# train-dry-run.sh — merge-train stage 1: stale wt/* sweep.
#
# Inventories every wt/* branch against the dev base, previews each
# merge with `git merge-tree --write-tree` (touches nothing), and in
# --apply mode deletes branches that are plain ancestors of dev
# (already merged, or dead stubs carrying no unique commits).
#
# Endo's WHERE-clause law: every destructive batch prints its candidate
# count before it runs and echoes every deletion it performed.
#
# Branches with a registered worktree are never touched — a live card or
# pair partner owns that checkout, even if the branch itself is merged.
#
# Usage:
#   scripts/train-dry-run.sh            # report only, zero mutations
#   scripts/train-dry-run.sh --dry-run  # same as default
#   scripts/train-dry-run.sh --apply    # delete ancestor-of-dev branches
#
# Categories:
#   (a) merged already / (b) dead stub, no unique commits -> deletable
#   (c) conflicted or carrying unique work                -> listed, untouched

set -euo pipefail

mode=report
case "${1:-}" in
  --apply)   mode=apply ;;
  --dry-run) mode=report ;;
  "")        mode=report ;;
  *) echo "usage: train-dry-run.sh [--dry-run|--apply]" >&2; exit 2 ;;
esac

base=dev
if ! git show-ref --verify --quiet "refs/heads/$base"; then
  echo "error: no '$base' branch in this repo" >&2
  exit 1
fi

# Branches currently checked out in a worktree: never touched.
declare -A WORKTREED=()
while read -r b; do
  [[ -n "$b" ]] && WORKTREED[$b]=1
done < <(git worktree list --porcelain |
         awk '/^branch refs\/heads\//{print $2}' |
         sed 's|^refs/heads/||')

deletable=()
kept_clean=0
kept_conflicted=0
kept_active=0
total=0

printf '%-22s %-12s %s\n' BRANCH LAST-COMMIT STATUS
printf '%s\n' '------------------------------------------------------------------------'
while read -r b; do
  [[ "$b" == wt/* ]] || continue
  total=$((total + 1))
  date=$(git log -1 --format=%cs "$b")
  if [[ -n "${WORKTREED[$b]:-}" ]]; then
    kept_active=$((kept_active + 1))
    printf '%-22s %-12s %s\n' "$b" "$date" "active worktree (skipped)"
    continue
  fi
  if git merge-base --is-ancestor "$b" "$base" 2>/dev/null; then
    deletable+=("$b")
    printf '%-22s %-12s %s\n' "$b" "$date" "ancestor of $base (deletable)"
  elif out=$(git merge-tree --write-tree "$base" "$b" 2>&1) &&
       ! grep -q '^CONFLICT' <<<"$out"; then
    kept_clean=$((kept_clean + 1))
    printf '%-22s %-12s %s\n' "$b" "$date" "merge-tree clean (keep: unique work)"
  else
    kept_conflicted=$((kept_conflicted + 1))
    printf '%-22s %-12s %s\n' "$b" "$date" "merge-tree conflicted (keep: unique work)"
  fi
done < <(git branch --format='%(refname:short)')

n=${#deletable[@]}
echo ""
echo "total wt/* branches:            $total"
echo "deletable (ancestor of $base):$n  [merged + dead stubs]"
echo "unique work, merge-tree clean:  $kept_clean"
echo "unique work, conflicted:        $kept_conflicted"
echo "active worktrees (skipped):     $kept_active"

if [[ "$mode" == "report" ]]; then
  echo ""
  echo "dry run: zero mutations performed. Re-run with --apply to delete."
  exit 0
fi

# WHERE-clause law: announce the batch size, then run it.
echo ""
if (( n == 0 )); then
  echo "apply: nothing to delete (batch of 0)."
  exit 0
fi
echo "apply: deletion batch of $n branch(es):"
printf '  %s\n' "${deletable[@]}"
ok=0
for b in "${deletable[@]}"; do
  # Safety belt in addition to our own ancestry check: -d refuses any
  # branch not merged into HEAD; never escalate to -D here.
  if git branch -d "$b" >/dev/null 2>&1; then
    ok=$((ok + 1))
  else
    echo "FAILED: git branch -d $b (not merged into HEAD?) — skipped" >&2
  fi
done
echo "changes(): deleted $ok of $n."