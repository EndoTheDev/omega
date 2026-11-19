#!/usr/bin/env bash
# factory-prune_test.sh - runnable checks for scripts/factory-prune.sh.
# Run: bash scripts/factory-prune_test.sh
#
# The script exists because the old one could not prune a card whose worktree
# still existed: it ran `git branch -d` FIRST, git refused the checked-out
# branch, and the refusal was reported as "not-merged" while `merged` stayed 1
# so the worktree block never ran. Seven live passes hit it (t_fdd12843,
# t_42854d3e, t_10ac04f0, t_0a9edc72, t_556bd2ad, t_f13642bc, t_cbe96297,
# card t_d4ae3e2b) - every one left the furniture behind.
#
# These checks pin the ordering and the honest verdicts:
#   - a merged card with a LIVE worktree prunes both, worktree line first;
#   - an unmerged card keeps both, and says not-merged;
#   - a dirty worktree keeps both, and says dirty (never not-merged);
#   - a branch-gone worktree still goes when its HEAD is in the merge ref;
#   - a prune run from a stale sibling worktree (where `branch -d` also lies
#     "not fully merged") still prunes, and says which mechanism it used;
#   - a husk under .worktrees/ (empty, unregistered - what a held-cwd removal
#     leaves) is swept, and the card's own verdict still prints;
#   - a registered worktree is never a husk, and a non-empty unregistered dir
#     is named for a human instead of deleted;
#   - a repo with no merge ref refuses loudly instead of guessing.
#
# Fixtures are a throwaway repo + bare remote under mktemp -d, never the real
# checkout. Worktrees live at .worktrees/<id> on branches wt/<id>, the exact
# shape the script matches.
set -uo pipefail

SCRIPT="$(cd "$(dirname "$0")" && pwd)/factory-prune.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }
has() { printf '%s\n' "$1" | grep -qF "$2"; }
hasnt() { ! printf '%s\n' "$1" | grep -qF "$2"; }

[ -f "$SCRIPT" ] || { echo "factory-prune_test: $SCRIPT missing" >&2; exit 1; }

# Native git is a Windows binary: MSYS /tmp paths are NOT translated for it,
# so a bare "$FIX/repo" lands on <drive>:/tmp/... while bash looks in the
# MSYS tmp. Every path handed to git as an argument goes through mixpath.
mixpath() { cygpath -m "$1" 2>/dev/null || printf '%s' "$1"; }

FIX=$(mktemp -d)
REPO="$FIX/repo"
git init -q --initial-branch=dev "$(mixpath "$REPO")" || { echo "factory-prune_test: git init failed" >&2; exit 1; }
cd "$REPO" || exit 1
git config user.email test@example.com
git config user.name factory-prune-test
git config core.autocrlf false
git config advice.forceDeleteBranch false
echo base >base.txt
git add base.txt
git commit -qm base
git init -q --bare "$(mixpath "$FIX/remote.git")"
git remote add origin "$(mixpath "$FIX/remote.git")"
git push -q origin dev

# card <id> <merged|unmerged|dirty|detached> - a card-shaped worktree.
#   merged   : own branch, committed, merged into dev, pushed
#   unmerged : own branch, committed, NOT merged
#   dirty    : merged, then a tracked file carries uncommitted work
#   detached : no branch at all, HEAD parked on dev (branch deleted elsewhere)
card() {
    local id=$1 kind=${2:-merged}
    if [ "$kind" = "detached" ]; then
        git worktree add -q --detach ".worktrees/$id" dev || return 1
    else
        git worktree add -q ".worktrees/$id" -b "wt/$id" dev || return 1
        ( cd ".worktrees/$id" && echo "$id" >"card-$id.txt" && git add . && git commit -qm "card $id" ) || return 1
    fi
    case "$kind" in
        merged | dirty)
            git merge -q --no-ff "wt/$id" -m "merge $id" >/dev/null 2>&1 || return 1
            git push -q origin dev || return 1
            ;;
    esac
    if [ "$kind" = "dirty" ]; then
        echo "uncommitted work" >>".worktrees/$id/card-$id.txt"
    fi
    return 0
}

branch_exists() { git show-ref --verify --quiet "refs/heads/$1"; }
run_prune() { ( cd "$REPO" && bash "$SCRIPT" "$@" 2>&1 ); }
run_prune_from() {
    local where=$1
    shift
    ( cd "$where" && bash "$SCRIPT" "$@" 2>&1 )
}

# -- T1: the live shape - merged card, worktree still present --------------
# This is the case that reported KEPT-BRANCH not-merged seven times live.
card t_live merged && ok || bad "T1a: could not build the merged-card fixture"
out=$(run_prune t_live); rc=$?
[ "$rc" -eq 0 ] && ok || bad "T1b: a clean prune exited $rc (out: $out)"
has "$out" "PRUNED-WORKTREE t_live" && ok || bad "T1c: the live worktree was not pruned (out: $out)"
has "$out" "PRUNED-BRANCH wt/t_live" && ok || bad "T1d: the merged branch was not pruned (out: $out)"
hasnt "$out" "not-merged" && ok || bad "T1e: a merged card was reported not-merged - the original defect"
[ ! -d ".worktrees/t_live" ] && ok || bad "T1f: the worktree dir survived a PRUNED-WORKTREE line"
branch_exists wt/t_live && bad "T1g: the branch survived a PRUNED-BRANCH line" || ok
wl=$(printf '%s\n' "$out" | grep -n 'PRUNED-WORKTREE t_live' | cut -d: -f1)
bl=$(printf '%s\n' "$out" | grep -n 'PRUNED-BRANCH wt/t_live' | cut -d: -f1)
[ -n "$wl" ] && [ -n "$bl" ] && [ "$wl" -lt "$bl" ] && ok \
    || bad "T1h: the worktree line did not precede the branch line (order law; out: $out)"

# -- T2: unmerged card - both halves stay, verdict says not-merged ---------
card t_open unmerged && ok || bad "T2a: could not build the unmerged-card fixture"
out=$(run_prune t_open); rc=$?
[ "$rc" -eq 0 ] && ok || bad "T2b: a kept card exited $rc (out: $out)"
has "$out" "KEPT-BRANCH wt/t_open not-merged" && ok || bad "T2c: an unmerged branch was not kept as not-merged (out: $out)"
hasnt "$out" "PRUNED-WORKTREE t_open" && ok || bad "T2d: an unmerged card's worktree was pruned"
hasnt "$out" "PRUNED-BRANCH wt/t_open" && ok || bad "T2e: an unmerged branch was deleted"
[ -d ".worktrees/t_open" ] && ok || bad "T2f: an unmerged card's worktree dir is gone"
branch_exists wt/t_open && ok || bad "T2g: an unmerged branch is gone"

# -- T3: dirty merged worktree - KEPT for the true cause -------------------
card t_dirty dirty && ok || bad "T3a: could not build the dirty-worktree fixture"
out=$(run_prune t_dirty); rc=$?
[ "$rc" -eq 0 ] && ok || bad "T3b: a dirty-kept card exited $rc (out: $out)"
has "$out" "KEPT-WORKTREE t_dirty dirty" && ok || bad "T3c: a dirty worktree was not kept as dirty (out: $out)"
hasnt "$out" "KEPT-BRANCH wt/t_dirty not-merged" && ok \
    || bad "T3d: a MERGED branch was blamed as not-merged because its worktree was dirty (out: $out)"
hasnt "$out" "PRUNED-WORKTREE t_dirty" && ok || bad "T3e: a dirty worktree was pruned"
[ -d ".worktrees/t_dirty" ] && ok || bad "T3f: the dirty worktree dir is gone"
branch_exists wt/t_dirty && ok || bad "T3g: the branch went with a dirty worktree still live"

# -- T4: branch already gone, worktree HEAD merged - worktree still goes ---
card t_nobranch detached && ok || bad "T4a: could not build the branch-gone fixture"
out=$(run_prune t_nobranch); rc=$?
[ "$rc" -eq 0 ] && ok || bad "T4b: a branch-gone prune exited $rc (out: $out)"
has "$out" "PRUNED-WORKTREE t_nobranch" && ok || bad "T4c: a merged HEAD worktree without a branch was not pruned (out: $out)"
hasnt "$out" "PRUNED-BRANCH" && ok || bad "T4d: a nonexistent branch got a PRUNED-BRANCH line (out: $out)"
[ ! -d ".worktrees/t_nobranch" ] && ok || bad "T4e: the worktree dir survived"

# -- T5: nothing to prune is stated, never silent --------------------------
out=$(run_prune t_absent); rc=$?
[ "$rc" -eq 0 ] && ok || bad "T5a: an unknown card exited $rc (out: $out)"
has "$out" "CLEAN t_absent nothing-to-prune" && ok || bad "T5b: an already-pruned card printed no CLEAN line (out: $out)"

# -- T6: the second lie - a stale sibling checkout ------------------------
# `git branch -d` compares against HEAD, so a worker running the script from
# its own worktree (based on older dev) gets "not fully merged" for a branch
# that IS in origin/dev. The script must still prune and name the mechanism.
git worktree add -q .worktrees/t_sib -b wt/t_sib dev && ok || bad "T6a: could not build the sibling fixture"
card t_stale merged && ok || bad "T6b: could not build the stale-checkout fixture"
out=$(run_prune_from "$REPO/.worktrees/t_sib" t_stale); rc=$?
[ "$rc" -eq 0 ] && ok || bad "T6c: a prune from a sibling worktree exited $rc (out: $out)"
has "$out" "PRUNED-WORKTREE t_stale" && ok || bad "T6d: the worktree was not pruned from a sibling checkout (out: $out)"
has "$out" "PRUNED-BRANCH wt/t_stale" && ok || bad "T6e: the merged branch was not pruned from a sibling checkout (out: $out)"
has "$out" "proved merged into origin/dev" && ok \
    || bad "T6f: the branch-delete fallback ran without naming why (out: $out)"
hasnt "$out" "not-merged" && ok || bad "T6g: a stale HEAD made a merged branch read not-merged (out: $out)"
branch_exists wt/t_stale && bad "T6h: the branch survived a PRUNED-BRANCH line" || ok
[ ! -d ".worktrees/t_stale" ] && ok || bad "T6i: the worktree dir survived"

# -- T7: batch order does not stop at a kept card ------------------------
out=$(run_prune t_open t_dirty t_absent); rc=$?
[ "$rc" -eq 0 ] && ok || bad "T7a: a mixed batch exited $rc (out: $out)"
has "$out" "KEPT-BRANCH wt/t_open not-merged" && ok || bad "T7b: the batch dropped the unmerged card (out: $out)"
has "$out" "KEPT-WORKTREE t_dirty dirty" && ok || bad "T7c: the batch dropped the dirty card (out: $out)"
has "$out" "CLEAN t_absent nothing-to-prune" && ok || bad "T7d: the batch stopped before an already-pruned card (out: $out)"

# -- T8: idempotent - a second pass is CLEAN, not another lie ------------
out=$(run_prune t_live); rc=$?
[ "$rc" -eq 0 ] && ok || bad "T8a: a second pass exited $rc (out: $out)"
has "$out" "CLEAN t_live nothing-to-prune" && ok || bad "T8b: a pruned card's second pass printed no CLEAN (out: $out)"

# -- T10: a HUSK under .worktrees/ is swept -------------------------------
# A live session holding a card worktree as cwd lets `git worktree remove -f`
# delete every file but not the top dir. What is left names no worktree, so
# wtpath_of cannot reach it and every prune run reported "nothing-to-prune"
# over the furniture.
mkdir -p .worktrees/t_husk
out=$(run_prune t_husk); rc=$?
[ "$rc" -eq 0 ] && ok || bad "T10a: a prune with a husk present exited $rc (out: $out)"
has "$out" "PRUNED-HUSK t_husk" && ok || bad "T10b: the husk was not swept (out: $out)"
[ ! -d .worktrees/t_husk ] && ok || bad "T10c: the husk dir survived a PRUNED-HUSK line"
has "$out" "CLEAN t_husk nothing-to-prune" && ok || bad "T10d: the card's own verdict went missing (out: $out)"

# The husk a prune run from a SIBLING worktree must still find: .worktrees/
# sits beside the shared .git, not beside the worktree the script ran in.
mkdir -p .worktrees/t_husk2
out=$(run_prune_from "$REPO/.worktrees/t_sib" t_husk2); rc=$?
[ "$rc" -eq 0 ] && ok || bad "T10e: a prune from a sibling worktree exited $rc (out: $out)"
has "$out" "PRUNED-HUSK t_husk2" && ok \
    || bad "T10f: a sibling-worktree prune could not see the husk (out: $out)"
[ ! -d .worktrees/t_husk2 ] && ok || bad "T10g: the husk dir survived the sibling-worktree sweep"

# -- T11: a REGISTERED worktree is never a husk ---------------------------
# t_open is registered and unmerged (T2). A prune run that does not name it
# must leave it alone: the sweep asks the same registration list the free
# step asks.
out=$(run_prune t_nobody); rc=$?
[ "$rc" -eq 0 ] && ok || bad "T11a: an unrelated prune exited $rc (out: $out)"
has "$out" "PRUNED-HUSK t_open" && bad "T11b: a registered worktree was swept as a husk (out: $out)" || ok
[ -f .worktrees/t_open/.git ] && ok || bad "T11c: a registered worktree lost its .git file"
[ -d .worktrees/t_open ] && ok || bad "T11d: a registered worktree dir is gone"

# -- T12: a NON-empty unregistered dir is named, never deleted ------------
# It may be unclaimed work, or a `dir` workspace: the pruner says so instead
# of calling the sweep clean.
mkdir -p .worktrees/t_orphan
echo unclaimed >.worktrees/t_orphan/notes.txt
out=$(run_prune t_nobody); rc=$?
has "$out" "NOTE t_orphan unregistered-dir-kept" && ok \
    || bad "T12a: an unregistered dir holding files was not named (out: $out)"
hasnt "$out" "PRUNED-HUSK t_orphan" && ok || bad "T12b: a dir holding files was deleted as a husk (out: $out)"
[ -f .worktrees/t_orphan/notes.txt ] && ok || bad "T12c: the unclaimed file was destroyed"

# -- T13: the shared fold agrees with git in every spelling ---------------
# The board writes D:\..., git prints D:/..., an MSYS caller hands /d/... .
# wt_norm is the only comparison, and it lives in scripts/lib-wt.sh with
# train-pass.sh sourcing the same file.
. "$(dirname "$SCRIPT")/lib-wt.sh"
reg="$(mixpath "$REPO/.worktrees/t_open")"
win="${reg//\//\\}"
wt_registered "$win" && ok || bad "T13a: the backslash (board) spelling did not fold: $win"
wt_registered "$reg/" && ok || bad "T13b: a trailing-slash spelling did not fold: $reg/"
wt_registered "$reg-missing" && bad "T13c: an unregistered path answered registered" || ok

# -- T9: no merge reference refuses loudly, never guesses ---------------
LONE="$(mktemp -d)/lone"
git init -q --initial-branch=trunk "$(mixpath "$LONE")" && ok || bad "T9a: could not build the no-merge-ref fixture"
( cd "$LONE" && git config user.email t@e.st && git config user.name t \
    && echo x >x.txt && git add x.txt && git commit -qm x ) >/dev/null 2>&1
out=$( cd "$LONE" && bash "$SCRIPT" t_whatever 2>&1 ); rc=$?
[ "$rc" -eq 2 ] && ok || bad "T9b: a repo with no merge ref exited $rc, not 2 (out: $out)"
has "$out" "REFUSED no-merge-ref" && ok || bad "T9c: the missing merge ref was not named (out: $out)"

cd /
rm -rf "$FIX" 2>/dev/null || true

echo
echo "factory-prune_test: $pass ok, $fail fail"
[ "$fail" -eq 0 ]
