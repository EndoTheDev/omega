#!/usr/bin/env bash
# factory-prune.sh - remove the worktree, branch and origin marker of cards
# that MERGED.
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
# Husks: an unregistered EMPTY dir under .worktrees/ is what a held-cwd
# removal leaves behind (git deletes the contents, the top directory
# survives the handle), and `git worktree list` cannot name it - so
# wtpath_of cannot see it and the card would read "nothing-to-prune"
# forever. sweep_husks clears that shape before the per-card loop and says
# PRUNED-HUSK <id>. A registered worktree is never a husk (both sides go
# through the same scripts/lib-wt.sh fold), and a NON-empty unregistered
# dir is never deleted here - NOTE <id> unregistered-dir-kept names it for
# a human, since it may be unclaimed work or a `dir` workspace.
#
# NEVER run this under WSL: the repo's worktree registrations use D:/
# paths, and WSL git reads them as nonexistent - a prune or repair under
# WSL deletes LIVE registrations (proven 2026-11-17).
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
# MARKER REF: a landed card also carries refs/train/<seq>-<id> at its merge
# commit. The ORIGIN copy dies with the card - it keeps the landing's ancestry
# reachable from the remote long after the card, and a card landed before a
# history rewrite shipped the whole pre-rewrite tree through its marker, so
# consumed markers defeated the rewrite for as long as they survived. The
# LOCAL ref stays: train-pass allocates the next seq from the local refs and
# train-bisect walks them, so deleting it here would reset the seq chain to
# 0001. Deletion is by PROOF, like every other removal here: a marker dies
# when this run established the card is consumed, or - on the already-pruned
# path, which is the retry door for a refused push - when the marker's own
# commit is in the merge ref. A marker this clone cannot vouch for is named,
# never guessed away.

set -uo pipefail
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
[ -z "$_pin_hits" ] \
	|| { printf '%s: pipes a writer into grep -q (SIGPIPE under pipefail): %s\n' "${_PIN_SELF##*/}" "$_pin_hits" >&2; exit 1; }


# WSL guard - WSL git misreads Windows-path registrations.
if [ -f /proc/version ] && grep -qi microsoft /proc/version 2>/dev/null; then
    echo "REFUSED wsl-git" >&2
    exit 2
fi

REPO="$(git rev-parse --show-toplevel)" || exit 2
cd "$REPO" || exit 2

# The worktree path fold lives in scripts/lib-wt.sh: the train's free step
# reads the same registrations, and two copies of the fold drift.
WT_LIB="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib-wt.sh"
if [ ! -f "$WT_LIB" ]; then
    echo "REFUSED missing-helper (scripts/lib-wt.sh does not exist at $WT_LIB)" >&2
    exit 2
fi
. "$WT_LIB"

# .worktrees lives beside the SHARED .git, so a prune run from a sibling
# worktree sweeps the same directory the main checkout holds.
common="$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null \
    || git rev-parse --git-common-dir 2>/dev/null || true)"
if [ -n "$common" ] && [ -d "$common" ]; then
    WT_ROOT="$(cd "$common/.." && pwd -P)/.worktrees"
else
    WT_ROOT="$REPO/.worktrees"
fi

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

# Is this commit reachable from the merge ref? The only merge verdict here.
merged_into_ref() {
    git merge-base --is-ancestor "$1" "$MERGE_REF" 2>/dev/null
}

# sweep_husks: remove the empty, unregistered leftover dirs under
# .worktrees/ - the husk a held-cwd removal leaves - and NAME every
# unregistered dir it does not remove, so a prune run never reports a clean
# sweep over furniture it simply could not see. Global rather than per-card:
# a husk belongs to a card that may never get another prune run, and an
# EMPTY dir carries no work. wt_registered answers through the shared fold,
# so a registered worktree in any spelling is never a husk.
sweep_husks() {
    [ -d "$WT_ROOT" ] || return 0
    local d id
    for d in "$WT_ROOT"/*/; do
        [ -d "$d" ] || continue
        d="${d%/}"
        id="${d##*/}"
        wt_registered "$d" && continue
        if [ -z "$(ls -A "$d" 2>/dev/null)" ]; then
            if rmdir "$d" 2>/dev/null; then
                echo "PRUNED-HUSK $id (empty dir, no worktree registration)"
            else
                echo "NOTE $id husk-empty-held $d (no worktree registration; a live process still holds the dir)"
            fi
        else
            echo "NOTE $id unregistered-dir-kept $d (no worktree registration and not empty - left for a human)"
        fi
    done
    return 0
}

# ---- marker refs (refs/train/<seq>-<id>) ---------------------------------
# The glob is a SUFFIX match, so a marker for t_abc can never be read as one
# for t_abcd.
have_origin() { grep -qx origin <<<"$(git remote 2>/dev/null)"; }

# origin_markers_of <id>: "<ref> <sha>" per marker on origin. A FAILED read is
# not an empty one: an ls-remote that could not answer must never look like
# "no markers", because a silent skip leaves exactly the leak this deletes.
origin_markers_of() {
    have_origin || return 0
    local out
    out="$(git ls-remote --refs origin "refs/train/*-$1" 2>/dev/null)" || return 1
    [ -n "$out" ] && printf '%s\n' "$out"
    return 0
}

local_markers_of() {
    git for-each-ref --format='%(refname)' "refs/train/*-$1" 2>/dev/null
}

# drop_markers <id> <proved>
#   proved 1 - this run proved the card consumed (its worktree and branch are
#     gone). The origin's whole marker set goes, including a marker this clone
#     no longer carries locally.
#   proved 0 - no furniture left to prove anything with, so only a marker
#     whose own commit is in the merge ref dies: the retry door for a push
#     refused while the card was pruned. A marker with no local twin is proved
#     by the sha the remote advertises; anything else is named and kept.
drop_markers() {
    local id=$1 proved=${2:-0} pairs locals ref target sha wanted="" origin_refs=""
    if ! pairs="$(origin_markers_of "$id")"; then
        echo "NOTE $id marker-origin-unreadable (ls-remote origin could not answer; a later prune retries it)"
        held=1
        return 0
    fi
    while read -r sha ref; do
        [ -n "$ref" ] && origin_refs="$origin_refs $ref"
    done <<<"$pairs"
    if [ "$proved" -eq 1 ]; then
        wanted="$origin_refs"
    else
        locals="$(local_markers_of "$id")"
        for ref in $locals; do
            target="$(git rev-parse --quiet --verify "$ref^{commit}" 2>/dev/null || true)"
            if [ -z "$target" ] || ! merged_into_ref "$target"; then
                echo "NOTE $id marker-kept-unverified $ref (target is not in $MERGE_REF)"
                continue
            fi
            case " $origin_refs " in
                *" $ref "*) wanted="$wanted $ref" ;;
            esac
        done
        while read -r sha ref; do
            [ -n "$ref" ] || continue
            case " $(printf '%s' "$locals" | tr '\n' ' ') " in
                *" $ref "*) continue ;;
            esac
            if git rev-parse --quiet --verify "$sha^{commit}" >/dev/null 2>&1 \
                && merged_into_ref "$sha"; then
                wanted="$wanted $ref"
            else
                echo "NOTE $id marker-kept-unverified $ref (no local ref; the remote sha is not in $MERGE_REF)"
            fi
        done <<<"$pairs"
    fi
    for ref in $wanted; do
        if git push -q origin ":refs/train/${ref#refs/train/}" >/dev/null 2>&1; then
            echo "PRUNED-MARKER $ref (origin copy gone; the local ref keeps the seq chain)"
        else
            echo "NOTE $id marker-origin-kept $ref (push refused; a later prune retries it)"
            held=1
        fi
    done
    return 0
}

# The husk sweep runs FIRST: the per-card loop can only report on what the
# worktree list names, and this is the pass that clears what it cannot.
sweep_husks

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
        drop_markers "$id" 0
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

    # ---- marker last ------------------------------------------------------
    # The card is consumed whichever way the furniture went (the merge verdict
    # above is the proof), and the marker's origin copy is what outlives it.
    drop_markers "$id" 1
done

exit "$held"
