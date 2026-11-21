#!/usr/bin/env bash
# factory-merge.sh - the REVIEW-SEAT merge entry (merger shape A).
# Usage: factory-merge.sh <card-id>
#
# WHY (maintainer ruling, 2026-11-17; card t_5b61405b): a vera-APPROVED card
# used to wait for the next tick's merge pass - up to 20 minutes, and the gap
# grows with throughput. The merge follows the REVIEW SEAT: an APPROVED
# verdict lands the card in the same session. The tick's merge pass stays as
# the FALLBACK sweep, and it merges only what the review seat left behind.
#
# WHAT THIS IS, EXACTLY: the same merge cycle the tick runs, for ONE named
# card - scripts/train-pass.sh with TRAIN_CARD pinned (branch rebase onto the
# dev tip, the full gate list green ON THAT MERGED TREE, `merge --no-ff`,
# push, marker ref, strike/demote laws included), then
# `bash scripts/factory-prune.sh <id>`. Nothing here re-implements the cycle;
# a second implementation is how the two lanes would drift.
#
# The APPROVAL PROOF is the pin itself: train-pass only lands a card the
# board shows as `done` with a live branch, so this script cannot land
# unreviewed work - a running card refuses.
#
# NOT a rebuild booking: the lane rebuild stays with the tick and with the
# product-bytes law (rebuild on runtime bytes only, batching untouched).
# This script only BOOKS it - `REBUILD-DUE` / `REBUILD-NOT-DUE` - so the
# tick's next pass knows what the landing owed.
#
# The worker fence (bin/shims/git, docs/pipeline.md) refuses landing from a
# stamped worker session. This script is the ONE sanctioned door through it:
# it re-execs once without the stamp and without the shim on PATH, AFTER the
# board has proven the card is a queued (approved) landing. Every other
# worker shell stays fenced.
#
# IDEMPOTENT: a card already in dev refuses cleanly (`ALREADY-MERGED`) and
# still runs the prune - a second attempt never double-merges. The verdict
# comes from git (is the branch an ancestor of the merge ref, or does dev
# carry the train's `Merge card <id>:` commit), never from a branch name
# being absent.
#
# The pipeline lock (`$HOME/.hermes/cache/scratch/omega-pipeline.lock`, the
# tick's own) is taken for the merge: a live tick pass and a review-seat
# merge never author the same checkout at once. A held lock defers to the
# tick (`DEFERRED`) instead of waiting forever.
#
# NEVER run this under WSL: the repo's worktree registrations use D:/ paths
# and WSL git reads them as nonexistent (same law as factory-prune.sh).
#
# Usage:  bash scripts/factory-merge.sh <card-id>
#
# Environment:
#   FM_REPO          the main checkout (default: this checkout's common dir)
#   FM_BASE          landing branch (default: dev)
#   FM_LOCK_DIR      pipeline-lock directory (default: $HOME/.hermes/cache/scratch)
#   FM_LOCK_WAIT     seconds to wait for a held pipeline lock (default: 0)
#   HERMES           hermes CLI (the receipt comment; default: hermes)
#   KANBAN_DB        board db (inherited by train-pass; unset, the train
#                    resolves the live home's board db itself - card
#                    t_7acaa267, never the dead $HOME/.hermes legacy default)
#   OMEGA_HOME       the SERVING lane home for the journeys gate (inherited;
#   OMEGA_BIN        unset, the train derives <checkout>/bin and refuses
#                    BEFORE the merge when it cannot)
#   JOURNEY_BASE_URL the lane those journeys drive (inherited; health-checked
#                    as a precondition)
#   TRAIN_GATES_CMD  forwarded - the test override for the gate list
#
# Greppable lines (factory-prune.sh conventions):
#   MERGED <id> <sha>            landed by this run; <sha> is the MERGE
#                                commit, and an SPA landing adds
#                                "(+ embed-lane bundle <sha>)"
#   REBUILD-DUE <id> ...         the landing touched runtime bytes
#   REBUILD-NOT-DUE <id> ...     docs/test-only landing
#   EYEBALL <id> ...             UI in range - the imagery receipt is owed
#   ALREADY-MERGED <id> ...      nothing to do; prune still ran
#   DEFERRED <id> ...            the pipeline lock is held - the tick sweeps
#   FAILED <id> ...              the train did not land; card left as train-pass left it
#   REFUSED <reason>             precondition (stderr, like factory-prune.sh)
#   NOTE <text>                  informational
#
# Exit codes: 0 merged or already merged, 2 usage/precondition refusal,
# 3 pipeline lock held (DEFERRED - the tick's fallback merges), 4 the
# checkout is not a clean dev checkout, 6 the train did not land.

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


ID="${1:-}"
case "$ID" in
  '' | -h | --help | --usage)
    sed -n '2,3p' "$0" | sed 's/^# //' >&2
    echo "usage: factory-merge.sh <card-id>" >&2
    exit 2
    ;;
esac

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

refuse() {
  echo "REFUSED $*" >&2
  exit 2
}

# ---- WSL guard (the prune law: WSL git misreads D:/ registrations) ------
if [ -f /proc/version ] && grep -qi microsoft /proc/version 2>/dev/null; then
  refuse "wsl-git (the merge harness is MSYS/native git only)"
fi

# ---- the fence door ----------------------------------------------------
# A stamped worker session carries OMEGA_WORKER[_CARD] and the shim dir at
# the front of PATH. Re-exec ONCE without either, so the delegated train
# (which checks out dev, commits the merge and pushes) is not refused by a
# fence that exists to stop hand-invented landing, not this entry.
strip_shim_path() {
  local out='' d
  local IFS=:
  for d in ${PATH:-}; do
    case "$d" in
      */bin/shims | */bin/shims/) continue ;;
    esac
    out="${out:+$out:}$d"
  done
  printf '%s' "$out"
}
if [ -n "${OMEGA_WORKER:-}${OMEGA_WORKER_CARD:-}" ] && [ "${FM_UNFENCED:-}" != "1" ]; then
  exec env -u OMEGA_WORKER -u OMEGA_WORKER_CARD FM_UNFENCED=1 \
    PATH="$(strip_shim_path)" bash "$0" "$@"
fi

# ---- the checkout ------------------------------------------------------
BASE="${FM_BASE:-dev}"
REPO="${FM_REPO:-}"
if [ -z "$REPO" ]; then
  common="$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null || true)"
  if [ -z "$common" ]; then
    common="$(git rev-parse --git-common-dir 2>/dev/null || true)"
    [ -n "$common" ] && common="$(cd "$common" && pwd -P)"
  fi
  [ -n "$common" ] || refuse "not-inside-a-git-checkout (run this from the omega checkout or set FM_REPO)"
  REPO="$(cd "$common/.." && pwd -P)"
fi
cd "$REPO" 2>/dev/null || refuse "no-checkout ($REPO does not resolve)"
[ -f "$REPO/scripts/train-pass.sh" ] \
  || refuse "no-train-pass ($REPO has no scripts/train-pass.sh)"
[ -f "$REPO/scripts/factory-prune.sh" ] \
  || refuse "no-prune ($REPO has no scripts/factory-prune.sh)"

if git rev-parse --verify --quiet refs/remotes/origin/$BASE >/dev/null; then
  MERGE_REF="origin/$BASE"
elif git rev-parse --verify --quiet "refs/heads/$BASE" >/dev/null; then
  MERGE_REF="$BASE"
else
  refuse "no-merge-ref (neither origin/$BASE nor $BASE resolves)"
fi

branch="wt/$ID"

# Same freshness law the train applies: never land on a base that moved.
# The first merged-verdict below must read a FRESH merge ref, so the fetch
# comes first and a refused fetch is named, never silent.
if grep -q '^origin$' <<<"$(git remote 2>/dev/null)"; then
  git fetch -q origin "$BASE" 2>/dev/null || echo "NOTE fetch origin $BASE refused - merging against the local tip"
fi

# merged_now: the branch is in the merge ref, or (branch already swept) the
# train's own merge commit names the card. Nothing else counts as merged.
merged_now() {
  if git show-ref --verify --quiet "refs/heads/$branch"; then
    git merge-base --is-ancestor "refs/heads/$branch" "$MERGE_REF" 2>/dev/null
    return
  fi
  [ -n "$(git log -1 --format=%H --fixed-strings --grep="Merge card $ID:" "$MERGE_REF" 2>/dev/null)" ]
}

# prune <id>: the same closer the tick runs. Its lines are passed through
# verbatim; a busy/dirty worktree is a KEPT line, never a merge failure.
prune_card() {
  local out rc
  out="$(bash "$REPO/scripts/factory-prune.sh" "$ID" 2>&1)"
  rc=$?
  printf '%s\n' "$out"
  return $rc
}

if merged_now; then
  shas="$(git rev-parse --short=12 "$MERGE_REF")"
  echo "ALREADY-MERGED $ID ($branch is in $MERGE_REF at $shas - no second merge)"
  prune_card || true
  exit 0
fi

# A landing happens from the main checkout, on the base, clean - the train's
# own preconditions, checked here so a refusal names ONE reason instead of
# letting train-pass die three steps in.
cur="$(git symbolic-ref --short HEAD 2>/dev/null || true)"
[ "$cur" = "$BASE" ] || refuse "not-on-$BASE (HEAD is on '${cur:-detached}'; the merge lands from the main checkout on $BASE)"
[ -z "$(git status --porcelain)" ] || refuse "checkout-dirty (commit or clean $REPO before a merge)"

# ---- the pipeline lock (the tick's own) --------------------------------
LOCK_DIR="${FM_LOCK_DIR:-$HOME/.hermes/cache/scratch}"
LOCK="$LOCK_DIR/omega-pipeline.lock"
WAIT="${FM_LOCK_WAIT:-0}"
took_lock=0
[ -d "$LOCK_DIR" ] || mkdir -p "$LOCK_DIR" 2>/dev/null || true
if [ -e "$LOCK" ]; then
  waited=0
  while [ -e "$LOCK" ] && [ "$waited" -lt "$WAIT" ]; do
    sleep 2
    waited=$((waited + 2))
  done
fi
if [ -e "$LOCK" ]; then
  echo "DEFERRED $ID pipeline-lock held ($LOCK) - the tick's fallback merge pass sweeps this card"
  exit 3
fi
printf 'factory-merge %s pid %s\n' "$ID" "$$" >"$LOCK" 2>/dev/null || {
  echo "DEFERRED $ID pipeline-lock not writable ($LOCK) - the tick's fallback merge pass sweeps this card"
  exit 3
}
took_lock=1
trap '[ "$took_lock" = 1 ] && rm -f "$LOCK"' EXIT

# The tick may have landed it between the first check and the lock.
if merged_now; then
  shas="$(git rev-parse --short=12 "$MERGE_REF")"
  echo "ALREADY-MERGED $ID ($branch is in $MERGE_REF at $shas - the tick landed it first)"
  prune_card || true
  exit 0
fi

pre="$(git rev-parse "$BASE")"

# ---- the cycle: the train, pinned to this card -------------------------
echo "==> merge (review seat): $ID via train-pass pin"
env -u TRAIN_NO_PUSH -u TRAIN_CARD TRAIN_CARD="$ID" \
  bash "$REPO/scripts/train-pass.sh"
train_rc=$?

if [ "$train_rc" != "0" ]; then
  # A failure can still mean "somebody landed it first" - verify before
  # reporting a failure, and never report a landing that git cannot show.
  if merged_now; then
    shas="$(git rev-parse --short=12 "$MERGE_REF")"
    echo "ALREADY-MERGED $ID ($branch reached $MERGE_REF at $shas while the train failed rc $train_rc)"
    prune_card || true
    exit 0
  fi
  if [ "$train_rc" = 2 ]; then
    # A precondition refusal happens BEFORE the rebase: nothing was merged, the
    # card keeps its status and its verdict. Name it so the review seat reads
    # "the environment is wrong", not "the work is wrong" (card t_7acaa267).
    echo "NOTE train-pass refused a precondition (rc 2) before the merge - nothing was rebased or landed. The cause is named above (board db, lane home or lane health); the card keeps its status."
  fi
  echo "FAILED $ID train-pass rc $train_rc - nothing landed here; the card stays where the train left it and the tick's fallback merge pass sweeps it"
  exit 6
fi

post="$(git rev-parse "$BASE")"
if [ "$post" = "$pre" ]; then
  echo "FAILED $ID train-pass reported success but $BASE did not move ($pre)"
  exit 6
fi

# ---- verify the landing chain, as the train does -----------------------
# The documented end state is the train's two-parent merge commit, OPTIONALLY
# followed by the embed-lane bundle commit the gate list lands on top of it:
# an SPA landing ends at the bundle commit. Reading the tip alone as the
# landing reads every clean SPA merge as divergence. A tip with no merge
# commit in its chain, or a merge commit not on the pre-landing tip, is a
# real divergence and fails by name.
two_parent() { git rev-parse -q --verify "$1^2" >/dev/null 2>&1; }
landing='' bundle=''
if two_parent "$post"; then
  landing="$post"
elif two_parent "$post^1"; then
  landing="$(git rev-parse "$post^1")"
  bundle="$post"
fi
if [ -z "$landing" ]; then
  echo "FAILED $ID $BASE moved to $post without a merge commit (no two-parent landing in the chain) - inspect by hand"
  exit 6
fi
if [ "$(git rev-parse "$landing^1")" != "$pre" ]; then
  echo "FAILED $ID the merge commit $landing does not sit on the pre-merge tip $pre - inspect by hand"
  exit 6
fi
short="$(printf '%s' "$landing" | cut -c1-12)"
tip="$(printf '%s' "$post" | cut -c1-12)"
marker="$(git for-each-ref --format='%(refname)' "refs/train/*-$ID" 2>/dev/null | sort | tail -1)"

echo "MERGED $ID $short on $BASE${bundle:+ (+ embed-lane bundle ${bundle:0:12})}${marker:+ (marker $marker)}"

# ---- the prune closer --------------------------------------------------
prune_card || echo "NOTE prune of $ID reported held state (the card is landed; furniture can be swept later)"

# ---- the rebuild booking (product bytes only; batching untouched) ------
changed="$(git diff --name-only "$pre" "$post" 2>/dev/null || true)"
product="$(printf '%s\n' "$changed" | grep -v '_test\.go$' \
  | grep -E '^(cmd/|core/|agent/|harness/|llm/|api/[^/]+\.go|webui/app/)' || true)"
ui="$(printf '%s\n' "$changed" \
  | grep -E '^(webui/app/|harness/[^/]+/ui/|api/[^/]+/ui/)' || true)"
if [ -n "$product" ]; then
  n="$(printf '%s\n' "$product" | grep -c . )"
  echo "REBUILD-DUE $ID $n runtime file(s) in ${pre:0:12}..${tip} - the tick's rebuild booking, not this script's"
else
  echo "REBUILD-NOT-DUE $ID (no runtime bytes in ${pre:0:12}..${tip} - docs/test-only landing)"
fi
if [ -n "$ui" ]; then
  echo "EYEBALL $ID UI in range - the imagery receipt (screenshots of the served surface) is owed before the rebuild is called good"
fi

# ---- the receipt (the card's merge comment) ----------------------------
receipt="merge (review seat): landed $short on $BASE, marker ${marker:-none}, pruned $ID.

- cycle: scripts/factory-merge.sh -> train-pass.sh pinned to $ID (rebase onto $BASE, gates green on the merged tree, merge --no-ff, push).
- rebuild: $(if [ -n "$product" ]; then echo "DUE ($(printf '%s\n' "$product" | grep -c .) runtime file(s)) - the tick books it per the product-bytes law."; else echo "not due (no runtime bytes in the landing)."; fi)$(if [ -n "$ui" ]; then echo "
- eyeball: UI in range - screenshots of the served surface are the imagery receipt."; fi)
- the tick's merge pass will not re-merge: $branch is an ancestor of $BASE."

if printf '%s' "$receipt" | "${HERMES:-hermes}" kanban comment "$ID" "$receipt" >/dev/null 2>&1; then
  :
else
  echo "NOTE receipt comment refused by the hermes CLI - the train's own landing comment stands, the receipt is above"
fi

exit 0
