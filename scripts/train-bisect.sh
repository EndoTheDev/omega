#!/usr/bin/env bash
set -euo pipefail

# train-bisect.sh - the MARKER-BISECT LAW, as a script.
#
# Every train landing leaves a marker ref at its merge commit:
#
#     refs/train/<seq>-<task_id>        (zero-padded seq, chronological)
#
# "Which landing introduced this?" is a scripted walk over that chain,
# never an archaeology session. Two modes:
#
#     scripts/train-bisect.sh --list
#         The whole chain in landing order: seq, merge sha, task, subject.
#
#     scripts/train-bisect.sh <bad-sha-or-ref> [--cmd <test-cmd>]
#         Binary-search the marker chain for the first landing whose tree
#         fails <test-cmd> (run inside a fresh probe checkout of the
#         landing tree, cwd = repo). TRAIN_BISECT_CMD is the env form.
#         Default good anchor: the dev tip BEFORE the first marker
#         (refs/train/0001-* ^1); pass an explicit anchor as arg 2 if the
#         chain's start predates a good base.
#
#     scripts/train-bisect.sh <bad-sha> --cmd <test-cmd> --chain <id=sha,...>
#         Walk an EXPLICIT candidate list instead of the marker refs: the
#         train's batch, whose merges carry no markers yet (a marker is a
#         LANDING, and the battery decides that). Candidates are
#         "<label>=<sha>" in landing order; the default anchor is the first
#         candidate's first parent - the tip the batch was merged onto. The
#         culprit line names the label, which is the card id.
#
# Without --cmd the script prints the exact manual git commands instead
# of guessing - it never runs something interactive it cannot score.
#
# A regression fix enters the queue as a normal card with the bisect
# evidence in its body and outranks by severity; the fix's own landing
# adds the next marker.

die() { echo "FATAL: train-bisect: $*" >&2; exit 2; }

repo="$(git rev-parse --show-toplevel 2>/dev/null)" || die "run this inside the omega checkout"
cd "$repo"
BASE="dev"

markers() {
  git for-each-ref --sort=refname --format='%(refname) %(objectname:short) %(subject)' refs/train
}

list_chain() {
  markers | awk '{ref=$1; sub(/^refs\/train\//, "", ref); seq=ref; sub(/-.*/, "", seq); task=ref; sub(/^[0-9]+-/, "", task); sha=$2; $1=""; $2=""; sub(/^ +/, ""); printf "%s  %s  %-14s %s\n", seq, sha, task, $0}'
}

if [ "${1:-}" = "--list" ]; then
  n="$(git for-each-ref refs/train | wc -l)"
  if [ "$n" -eq 0 ]; then
    echo "train-bisect: no marker refs yet (refs/train/* is empty)."
    exit 0
  fi
  list_chain
  exit 0
fi

[ $# -ge 1 ] || { echo "usage: train-bisect.sh --list | <bad-sha> [--cmd <test>] [--chain <id=sha,...>] [good-anchor]" >&2; exit 2; }
bad="$1"; shift

cmd="${TRAIN_BISECT_CMD:-}"
chain=""
while [ $# -gt 0 ]; do
  case "$1" in
    --cmd) cmd="${2:?--cmd needs a value}"; shift ;;
    --chain) chain="${2:?--chain needs a value}"; shift ;;
    -h|--help) echo "usage: train-bisect.sh --list | <bad-sha> [--cmd <test>] [--chain <id=sha,...>] [good-anchor]" >&2; exit 0 ;;
    *) Good="${1:?anchor}" ;;
  esac
  shift
done
Good="${Good:-}"

git cat-file -e "$bad^{commit}" 2>/dev/null || die "bad candidate $bad is not a commit here"

LABELS=()
chain_mode=0
if [ -n "$chain" ]; then
  # Explicit candidates (the train's batch): "<label>=<sha>" in landing
  # order. No marker refs exist for a landing that has not been gated yet.
  chain_mode=1
  IFS=',' read -r -a _entries <<<"$chain"
  for _e in "${_entries[@]}"; do
    _lab="${_e%%=*}"; _sha="${_e#*=}"
    { [ -n "$_lab" ] && [ "$_sha" != "$_e" ]; } \
      || die "malformed --chain entry '$_e' (expected label=sha)"
    git cat-file -e "$_sha^{commit}" 2>/dev/null \
      || die "chain candidate $_lab ($_sha) is not a commit here"
    M+=("$_sha")
    LABELS+=("$_lab")
  done
  n=${#M[@]}
  [ "$n" -gt 0 ] || die "--chain named no candidates"
  git merge-base --is-ancestor "${M[$((n-1))]}" "$bad" || \
    die "$bad does not descend the chain's last candidate (${LABELS[$((n-1))]})"
else
  mapfile -t M < <(markers | cut -d' ' -f1)
  n=${#M[@]}
  [ "$n" -gt 0 ] || die "no marker refs - nothing to walk (refs/train/* empty)"
  [ "$n" -ge 1 ] || die "no markers"

  git merge-base --is-ancestor "${M[$((n-1))]}" "$bad" || \
    die "$bad does not descend the marker chain (last landing $(basename "${M[$((n-1))]}"))"
fi

First="${M[0]}"
last="${M[$((n-1))]}"
anchor=""
if [ -n "$Good" ]; then
  git cat-file -e "$Good^{commit}" 2>/dev/null || die "good anchor $Good is not a commit"
  git merge-base --is-ancestor "$Good" "$bad" || die "good anchor is not an ancestor of $bad"
  git merge-base --is-ancestor "${M[0]}" "$bad" || true
  anchor="$Good"
else
  anchor="$(git rev-parse "$First^1")"   # the tip the first candidate landed on
fi

# The test at the bad tip must fail, or the whole walk is meaningless.
score() { # score <sha-tree>: 0 = good, 1 = bad
  local wt=".train-bisect"
  git worktree remove -f --force "$wt" >/dev/null 2>&1 || true
  git worktree add -q --detach "$wt" "$1" >/dev/null 2>&1 || die "cannot probe $1"
  local rc=0
  ( cd "$wt" && bash -c "$cmd" >/dev/null 2>&1 ) || rc=1
  git worktree remove -f --force "$wt" >/dev/null 2>&1 || true
  return $rc
}

if [ -z "$cmd" ]; then
  echo "train-bisect: no test command given - walk it by hand:"
  echo "  git bisect start $bad $anchor"
  echo "  # then at each step it offers, answer with: git bisect good|bad"
  echo "  # when named: map the result commit onto its landing:"
  echo "  $($0 --list | head -3)"
  exit 0
fi

# The walk only makes sense if bad really is bad.
if score "$bad"; then
  die "$bad does not fail the test command - double check what you are bisecting"
fi
if ! score "$anchor"; then
  die "the good anchor $anchor FAILS the test command - pass a good anchor by hand"
fi

lo=1; hi="$n"
while [ "$lo" -lt "$hi" ]; do
  mid=$(( (lo + hi) / 2 ))
  if score "${M[$((mid-1))]}"; then
    lo=$((mid + 1))
  else
    hi=$mid
  fi
done

Cul_ref="${M[$((lo-1))]}"
Cul="$(git rev-parse "$Cul_ref")"
if [ "$chain_mode" = "1" ]; then
  name="${LABELS[$((lo-1))]}"
else
  name="$(basename "$Cul_ref")"
fi
echo "culprit: landing $name"
echo "  merge commit: $(git rev-parse --short "$Cul_ref")"
echo "  subject: $(git log -1 --format=%s "$Cul_ref")"
echo "  tree state: $(git log -1 --format=%cs "$Cul_ref")"
echo "walk: $(git rev-list --count "$Cul_ref^1..$bad") commits between the last-good landing and $bad"
exit 0