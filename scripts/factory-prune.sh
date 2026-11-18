#!/usr/bin/env bash
# factory-prune.sh - remove the worktree and branch of cards that MERGED.
# Usage: factory-prune.sh <card-id> [<card-id> ...]
#
# ORDER LAW (card t_d4ae3e2b): the merge verdict comes from
# `git merge-base --is-ancestor`, NEVER from `git branch -d`'s exit code.
# The worktree is removed FIRST, the branch SECOND. `branch -d` refuses for
# two reasons that share one message ("not fully merged"): the branch is
# checked out in a live worktree, or the shell's own HEAD does not contain
# it. The old script read that refusal as "not-merged", so `merged` stayed 1
# and the worktree block below never ran - every live prune reported
# KEPT-BRANCH and left the furniture behind (live receipts, 2026-11-17 and
# 2026-11-18: t_fdd12843, t_42854d3e, t_10ac04f0, t_0a9edc72, t_556bd2ad,
# t_f13642bc, t_cbe96297).
#
# A KEPT line names its true cause: not-merged, dirty, busy, unverified.
# The dirty/busy guards are the real protections: a worktree that will not
# leave cleanly keeps its branch with it. Nothing on an unmerged branch is
# ever touched, and no removal is forced.
#
# Delete mechanism: `git branch -d` first (it re-checks against HEAD and
# upstream); if it refuses a branch this script has ALREADY proved merged
# into the merge ref, fall back to `git update-ref -d` and say so on the
# line. By that proof the commits are reachable from the merge ref, so the
# fallback loses nothing - it only stops HEAD's unrelated position from
# masquerading as a merge test.
#
# Merge ref: origin/dev when it exists, else the local dev. Neither present
# is a loud refusal (a prune without a merge reference verifies nothing).
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

if git rev-parse --verify --quiet refs/remotes/origin/dev >/dev/null; then
    MERGE_REF=origin/dev
elif git rev-parse --verify --quiet refs/heads/dev >/dev/null; then
    MERGE_REF=dev
else
    echo "REFUSED no-merge-ref (neither origin/dev nor dev resolves)" >&2
    exit 2
fi

# Path of the card's registered worktree, empty when there is none.
# A case-glob match, not a regex: the old grep pattern escaped the separator
# into a literal backslash ("worktrees\\/<id>"), so it matched NO path on any
# platform - worktree removal was unreachable even when the order was right.
wtpath_of() {
    git worktree list --porcelain \
        | sed -n 's|^worktree \(.*\)$|\1|p' \
        | while IFS= read -r p; do
              case "$p" in
                  *.worktrees/"$1" | *.worktrees\\"$1") printf '%s\n' "$p" ;;
              esac
          done
}

# Is this path still a registered worktree? (Exact string, as wtpath_of gave it.)
wt_registered() {
    git worktree list --porcelain | grep -qxF "worktree $1"
}

# Is this commit reachable from the merge ref? The only merge verdict here.
merged_into_ref() {
    git merge-base --is-ancestor "$1" "$MERGE_REF" 2>/dev/null
}

held=0
for id in "$@"; do
    branch="wt/$id"
    has_branch=0
    git show-ref --verify --quiet "refs/heads/$branch" && has_branch=1
    wt_path=$(wtpath_of "$id")

    # ---- the merge verdict, taken before anything is removed -------------
    if [ "$has_branch" -eq 1 ]; then
        if ! merged_into_ref "refs/heads/$branch"; then
            echo "KEPT-BRANCH $branch not-merged (tip is not an ancestor of $MERGE_REF)"
            continue
        fi
    elif [ -n "$wt_path" ]; then
        # Branch already gone: the worktree HEAD must still be provably merged.
        wt_head=$(git -C "$wt_path" rev-parse --quiet HEAD 2>/dev/null || echo none)
        if [ "$wt_head" = "none" ]; then
            echo "KEPT-WORKTREE $id unverified (branch gone, worktree HEAD unreadable)"
            continue
        fi
        if ! merged_into_ref "$wt_head"; then
            echo "KEPT-WORKTREE $id unverified (branch gone, worktree HEAD not in $MERGE_REF)"
            continue
        fi
    else
        echo "CLEAN $id nothing-to-prune"
        continue
    fi

    # ---- worktree first ---------------------------------------------------
    if [ -n "$wt_path" ]; then
        err=$(mktemp)
        if git worktree remove "$wt_path" 2>"$err"; then
            echo "PRUNED-WORKTREE $id"
        else
            cause=$(cat "$err")
            rm -f "$err"
            if wt_registered "$wt_path"; then
                # The registration survived: the tree is still a live checkout.
                case "$cause" in
                    *"contains modified or untracked files"*|*"contains modified"*)
                        echo "KEPT-WORKTREE $id dirty - branch $branch kept with it" ;;
                    *)
                        echo "KEPT-WORKTREE $id busy - branch $branch kept with it"
                        held=1 ;;
                esac
                continue
            fi
            # Registration gone, so the branch is no longer checked out. A
            # stale handle can still hold the emptied shell after the
            # registration dies (live receipts t_0a9edc72, t_556bd2ad); the
            # branch still goes, and the shell is named rather than dropped.
            echo "PRUNED-WORKTREE $id"
            if [ -d "$wt_path" ]; then
                rmdir "$wt_path" 2>/dev/null \
                    || echo "NOTE $id empty-dir-held $wt_path (registration gone; shell frees on handle release)"
            fi
        fi
        rm -f "$err" 2>/dev/null
    fi

    # ---- branch second ----------------------------------------------------
    if [ "$has_branch" -eq 1 ]; then
        if git branch -d "$branch" >/dev/null 2>&1; then
            echo "PRUNED-BRANCH $branch"
        elif git update-ref -d "refs/heads/$branch" 2>/dev/null; then
            echo "PRUNED-BRANCH $branch (branch -d compared against HEAD; proved merged into $MERGE_REF)"
        else
            echo "KEPT-BRANCH $branch delete-refused (proved merged into $MERGE_REF; git refused anyway)"
            held=1
        fi
    fi
done

exit "$held"
