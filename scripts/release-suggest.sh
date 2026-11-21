#!/usr/bin/env bash
set -euo pipefail
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


# release-suggest.sh - the release prompt's suggested bump + evidence list.
#
# Computes the suggested version bump from the commits made since the last
# stable tag. This SUGGESTS - it never tags anything. Endo reads the
# evidence and confirms or overrides in one word: version is a promise,
# mechanical detection under-sees behavioral breaks, over-sees renames.
#
# Rules (ratified 2026-10-21):
#   major  any BREAKING CHANGE marker (a `feat!:`-shape bang before the
#          conventional colon, or a BREAKING CHANGE footer line) or
#          removed/renamed API surface (subject opens with
#          remove/drop/delete/rename)
#   minor  at least one feat: commit
#   patch  everything else - fixes, chores, docs, refactors, merge
#          commits, or an empty range
#
# Usage:
#     scripts/release-suggest.sh [base-ref]
#         default base: newest stable v* tag (a v tag without '-')
#
# Output: human summary, then one machine line `SUGGEST=<major|minor|patch>`
# (the release prompt renders the summary and reads the last line).
# Exit codes: 0 always (a suggestion is never a gate), 2 unusable input
# (no stable tag / not a git repo).

if ! git rev-parse --git-dir >/dev/null 2>&1; then
  echo "release-suggest: not a git repo" >&2
  exit 2
fi

BASE="${1:-}"
if [ -z "$BASE" ]; then
  # Newest STABLE tag: the '-' filter drops prereleases (vX.Y.Z-nightly.*
  # keeps its own number below the next stable); sort -V picks the newest
  # word.
  BASE="$(git tag -l 'v*' | grep -v -- '-' | sort -V | tail -1)"
  if [ -z "$BASE" ]; then
    echo "release-suggest: no stable v* tag found - pass a base ref" >&2
    exit 2
  fi
  BASE_IS_TAG=1
fi

total=$(git rev-list --count "${BASE}..HEAD")

# One pass, bash-native (no awk \x-escape portability traps): git emits
# record-separated NUL-free blocks; read splits on the record separator
# so multi-line bodies survive intact.
re_bang='^[a-z][a-z]*(\([^)]*\))?!:'
re_prefix='^(remove|drop|delete|rename)(d|s)?[[:space:](:]'
re_feat='^feat(\(|:| )'

evidence=()
major_seen=0
minor_seen=0
while IFS= read -r -d $'\x1e' rec; do
  rec="${rec##$'\n'}"   # records after the first start with git's separator newline
  rec="${rec%$'\n'}"
  [ -n "$rec" ] || continue
  sha="${rec%%$'\x1f'*}"
  rest="${rec#*$'\x1f'}"
  subj="${rest%%$'\x1f'*}"
  body="${rest#*$'\x1f'}"
  low="${subj,,}"
  # Breaking: the ! bang-marker (feat!:/refactor!:/feat(x)!:), a
  # BREAKING CHANGE footer line, or removed/renamed API surface.
  if grep -Eq '^[[:space:]]*BREAKING[[:space:]]+CHANGE' <<<"$body" \
     || [[ "$subj" =~ $re_bang ]] \
     || [[ "$low" =~ $re_prefix ]]; then
    major_seen=1
    evidence+=("$sha $subj")
    continue
  fi
  if [[ "$low" =~ $re_feat ]]; then
    minor_seen=1
    evidence+=("$sha $subj")
  fi
done < <(git log --format='%h%x1f%s%x1f%b%x1e' "${BASE}..HEAD")

level="patch"
[ "$major_seen" -eq 0 ] && [ "$minor_seen" -eq 1 ] && level="minor"
[ "$major_seen" -eq 0 ] || level="major"

# --- report ----------------------------------------------------------------
n=${#evidence[@]}
plural=""
[ "$n" -ne 1 ] && plural="s"
echo "Suggested bump: $level (since $BASE, $total commits)"
if [ "$n" -eq 0 ]; then
  echo "Evidence: none - nothing drove it above patch"
else
  echo "Evidence ($n commit$plural that drove it):"
  for e in "${evidence[@]}"; do
    echo "  $e"
  done
fi
echo "This is a suggestion - the tag is Endo's word (confirm or override in one command)."
echo "SUGGEST=$level"