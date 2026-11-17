#!/usr/bin/env bash
# factory-prune.sh - remove the worktree and branch of cards that MERGED.
# Usage: factory-prune.sh <card-id> [<card-id> ...]
#
# The merge check is the gate: `git branch -d` refuses an unmerged branch,
# and only a successful branch deletion unlocks the worktree removal -
# a clean worktree on an unmerged branch is never touched.
# git's own refusals are the safety - this script never forces anything.
#
# NEVER run this under WSL: the repo's worktree registrations use D:/
# paths, and WSL git reads them as nonexistent - a prune or repair under
# WSL deletes LIVE registrations (proven 2026-11-17).

set -uo pipefail

# WSL guard - WSL git misreads Windows-path registrations.
if [ -f /proc/version ] && grep -qi microsoft /proc/version 2>/dev/null; then
    echo "REFUSED wsl-git" >&2
    exit 2
fi

cd "$(git rev-parse --show-toplevel)" || exit 2

wtpath_of() {
    git worktree list --porcelain | grep -E "worktree .*[\\/]\\.worktrees\\/$1\$" | cut -d' ' -f2-
}

held=0
for id in "$@"; do
    merged=1
    if git show-ref --verify --quiet "refs/heads/wt/$id"; then
        if git branch -d "wt/$id" >/dev/null 2>&1; then
            echo "PRUNED-BRANCH wt/$id"
            merged=0
        else
            echo "KEPT-BRANCH wt/$id not-merged"
        fi
    else
        # Branch already gone - remove the worktree only if its HEAD is
        # reachable from origin/dev (merged by another path).
        wt_path=$(wtpath_of "$id")
        if [ -n "$wt_path" ]; then
            wt_head=$(git -C "$wt_path" rev-parse --quiet HEAD 2>/dev/null || echo none)
            if [ "$wt_head" != "none" ] && git merge-base --is-ancestor "$wt_head" origin/dev 2>/dev/null; then
                merged=0
            else
                echo "KEPT-WORKTREE $id unverified"
            fi
        fi
    fi

    if [ "$merged" -eq 0 ]; then
        wt_path=$(wtpath_of "$id")
        if [ -n "$wt_path" ]; then
            err=$(mktemp)
            if git worktree remove "$wt_path" 2>"$err"; then
                echo "PRUNED-WORKTREE $id"
            else
                case "$(cat "$err")" in
                    *"contains modified or untracked files"*) echo "KEPT-WORKTREE $id dirty" ;;
                    *) echo "KEPT-WORKTREE $id busy"; held=1 ;;
                esac
            fi
            rm -f "$err"
        fi
    fi
done

exit "$held"
