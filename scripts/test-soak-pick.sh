#!/usr/bin/env bash
# Tests the soak pick step in .github/workflows/nightly.yml against a
# scratch clone of this repo. The step's run block is EXTRACTED from the
# yml (single source of truth), then driven through scenarios with
# GITHUB_OUTPUT / GITHUB_STEP_SUMMARY pointing at temp files.
#
#   scripts/test-soak-pick.sh          # run all scenarios
#   scripts/test-soak-pick.sh --keep   # keep the scratch dir for inspection
#
# Premises (the 2026-10-30 card): origin/master at 79154fc, and the
# stale tag v0.10.1-nightly.20261024 (commit 2abf4ab, the history-rewrite
# era) sitting BEHIND master. Assertions degrade to warnings when the
# real repo has moved past a premise; the synthetic scenarios have no
# such anchor and always run.
set -u

keep=0
[ "${1:-}" = "--keep" ] || [ "${1:-}" = "-k" ] && keep=1

here="$(cd "$(dirname "$0")" && pwd -P)"
top="$(cd "$here/.." && git rev-parse --show-toplevel 2>/dev/null || echo "$here/..")"
common="$(cd "$top" && git rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$top/.git")"
yml="$top/.github/workflows/nightly.yml"
[ -f "$yml" ] || { echo "cannot find $yml - run inside the omega repo" >&2; exit 1; }

scratch="$(mktemp -d "${TMPDIR:-/tmp}/soak-pick-test.XXXXXXXX")"
trap '[ $keep -eq 1 ] || rm -rf "$scratch"' EXIT
echo "scratch: $scratch"

# --- extract the pick step's run block from the yml -----------------------
awk '
  /^      - name: Pick the newest nightly tag/ {armed=1; next}
  armed && /^        id: pick/ {on=1; next}
  on && /^      - / {exit}
  on && /^  [a-zA-Z]/ {exit}
  on && /^        run: \|/ {body=1; next}
  body && /^          / {sub(/^          /, ""); print}
' "$yml" > "$scratch/pick-snippet.sh" || exit 1

if [ ! -s "$scratch/pick-snippet.sh" ]; then
  echo "FAIL: extraction produced an empty snippet - is the pick step still present?" >&2
  exit 1
fi
if ! bash -n "$scratch/pick-snippet.sh" 2>"$scratch/syntax.err"; then
  echo "FAIL: extracted snippet fails bash -n:" >&2
  cat "$scratch/syntax.err" >&2
  exit 1
fi
echo "ok: extracted $(grep -c . "$scratch/pick-snippet.sh") non-empty lines, bash -n clean"
echo "--- snippet under test ---"
cat "$scratch/pick-snippet.sh"
echo "--------------------------"

# --- one shared scratch clone (objects stay in alternates, tags are local) --
git clone -q --shared "$common" "$scratch/clone" || exit 1
repo="$scratch/clone"
git -C "$repo" fetch -q origin master --tags
master_sha="$(git -C "$repo" rev-parse origin/master)"
echo "origin/master: $master_sha"
case "$master_sha" in
  79154fc*) echo "premise matched: master at 79154fc" ;;
  *) echo "WARNING: master has moved past 79154fc ($master_sha) - SHA-anchored premises below degrade" ;;
esac

fails=0
pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; fails=$((fails + 1)); }

# --- structural assertion: the soak job must carry no CI-green step -------
# The "Nightly CI green on the soak point" step queried
# actions/runs?head_sha=<soak point>, but nightly runs only ever carry a
# MASTER head_sha (scheduled fires run on the default branch, dispatched
# fires use ref=master; in-job checkouts do not change the run's
# head_sha). Soak points are dev commits, so the query was structurally
# empty and the step could only exit 1. The nightly tag is the receipt.
awk '
  /^  soak:/ {on=1; next}
  on && /^  [a-zA-Z]/ {exit}
  on {print}
' "$yml" > "$scratch/soak-steps.txt"
if grep -q "Nightly CI green on the soak point" "$scratch/soak-steps.txt"; then
  fail "soak job still carries the structurally-dead 'Nightly CI green on the soak point' step"
else
  pass "soak job has no CI-green-on-soak-point step"
fi
if grep -q 'head_sha=' "$scratch/soak-steps.txt"; then
  fail "soak job still queries runs by head_sha - that query is structurally empty on soak points"
else
  pass "soak job has no head_sha= run query"
fi

# run the picker in the scratch clone, print its observation
# usage: run_pick <label>
run_pick() {
  : > "$scratch/out"; : > "$scratch/sum"
  rc=2
  rc="$( cd "$repo" && \
    GITHUB_OUTPUT="$scratch/out" GITHUB_STEP_SUMMARY="$scratch/sum" \
    bash "$scratch/pick-snippet.sh" >/dev/null 2>&1; echo $? )"
  tag="$(sed -n 's/^TAG=//p' "$scratch/out")"
  none="$(sed -n 's/^NONE=//p' "$scratch/out")"
  echo "$1: rc=$rc TAG=${tag:-<unset>} NONE=${none:-<unset>}"
}

# descendant of origin/master committed `age` seconds ago, tagged `name`
aged_descendant() { # <age_seconds> <tag_name>
  local age="$1" name="$2" when
  when=$(( $(date +%s) - age ))
  git -C "$repo" checkout -q --detach origin/master || fail "setup: checkout origin/master"
  echo "$name" > "$repo/soak-probe.txt"
  git -C "$repo" add soak-probe.txt
  GIT_AUTHOR_DATE="@$when +0000" GIT_COMMITTER_DATE="@$when +0000" \
    git -C "$repo" -c user.email=soak@test -c user.name=soak commit -qm "soak-pick test: $name" \
    || fail "setup: commit $name"
  git -C "$repo" tag "$name" || fail "setup: tag $name"
  git -C "$repo" rev-parse "${name}^{commit}" >/dev/null || fail "setup: $name unresolvable"
}

# child of an OLD commit that does not contain master, i.e. a point
# master can never ff onto - dated `age` seconds ago, tagged `name`
aged_behind() { # <base_ref_or_sha> <age_seconds> <tag_name>
  local base="$1" age="$2" name="$3" when
  when=$(( $(date +%s) - age ))
  git -C "$repo" checkout -q --detach "$base" || fail "setup: checkout $base"
  echo "$name" > "$repo/soak-probe.txt"
  git -C "$repo" add soak-probe.txt
  GIT_AUTHOR_DATE="@$when +0000" GIT_COMMITTER_DATE="@$when +0000" \
    git -C "$repo" -c user.email=soak@test -c user.name=soak commit -qm "soak-pick test behind master: $name" \
    || fail "setup: commit $name"
  git -C "$repo" tag "$name" || fail "setup: tag $name"
  git -C "$repo" rev-parse "${name}^{commit}" >/dev/null || fail "setup: $name unresolvable"
}

STALE_TAG=v0.10.1-nightly.20261024
stale_sha="$(git -C "$repo" rev-parse "${STALE_TAG}^{commit}" 2>/dev/null || true)"
if [ -z "$stale_sha" ]; then
  echo "WARNING: premise tag $STALE_TAG not found in the clone"
fi
if [ -n "$stale_sha" ] && git -C "$repo" merge-base --is-ancestor "$stale_sha" origin/master 2>/dev/null; then
  echo "info: $STALE_TAG is an ancestor of master (repo moved past it); the SHA premise is stale, exclusion still asserted"
fi

# --- S1: the real bug case. Real tags, real master. The stale tag must
# stay out of the eligible set, and the stale tag must never be picked.
run_pick "S1 real-tags"
[ "$rc" = "0" ] || fail "S1: pick step exited $rc, want 0"
if [ -n "$stale_sha" ]; then
  if grep -q "$STALE_TAG" "$scratch/sum"; then
    fail "S1: summary names the behind-master tag $STALE_TAG as eligible - bug reproduced"
  else
    pass "S1: eligible set excludes $STALE_TAG (behind master)"
  fi
  [ "$tag" != "$STALE_TAG" ] || fail "S1: picked the behind-master tag"
fi

# --- S2: a point BEHIND master that is version-newest AND 48h+ would be
# the old logic's pick; lineage must disqualify it -> NONE, exit 0.
# (v0.10.1-nightly.20261024's shape: 2abf4ab is behind master.)
aged_behind "2abf4ab" $((100 * 3600)) v9.9.9-nightly.21991231
run_pick "S2 version-newest-behind"
[ "$rc" = "0" ] || fail "S2: exited $rc, want 0 (green no-advance)"
[ "$none" = "true" ] || fail "S2: NONE=$none, want true - lineage filter did not disqualify the behind-master point"
[ "$tag" != "v9.9.9-nightly.21991231" ] || fail "S2: old behavior reproduced - picked the behind-master version-newest tag"
git -C "$repo" tag -d v9.9.9-nightly.21991231 >/dev/null

# --- S3: the 48h boundary. A 49h-old point ahead of master is eligible,
# a 47h-old one is not; the pick must be the 49h commit.
aged_descendant $((49 * 3600)) v1.1.1-nightly.20000101
aged_descendant $((47 * 3600)) v1.1.1-nightly.20000102
run_pick "S3 boundary 49h/47h"
[ "$rc" = "0" ] || fail "S3: exited $rc, want 0"
[ "$tag" = "v1.1.1-nightly.20000101" ] || \
  fail "S3: TAG=${tag:-<unset>}, want v1.1.1-nightly.20000101 (49h eligible, 47h not)"
[ "$none" = "false" ] || fail "S3: NONE=$none, want false"
grep -q "v1.1.1-nightly.20000101" "$scratch/sum" || fail "S3: summary does not name the picked tag"
git -C "$repo" tag -d v1.1.1-nightly.20000101 v1.1.1-nightly.20000102 >/dev/null

# --- S4: newest by commit DATE, not by version sort. Three eligible
# points; version-newest is the OLDEST commit. Correct pick: 60h.
aged_descendant $((100 * 3600)) v1.1.1-nightly.20000103
aged_descendant $((80 * 3600)) v1.1.1-nightly.20000101
aged_descendant $((60 * 3600)) v1.1.1-nightly.20000102
run_pick "S4 pick by commit date"
[ "$tag" = "v1.1.1-nightly.20000102" ] || \
  fail "S4: TAG=${tag:-<unset>}, want v1.1.1-nightly.20000102 (newest commit date, not version order)"
git -C "$repo" tag -d v1.1.1-nightly.20000103 v1.1.1-nightly.20000101 v1.1.1-nightly.20000102 >/dev/null

# --- S5: only fresh points ahead of master -> NONE, exit 0. This is the
# v0.13.1-nightly.20261030 case before its commit turns 48h old: no
# eligible point, green no-advance instead of a soak red.
aged_descendant 3600 v9.9.9-nightly.21991231
run_pick "S5 only-fresh-ahead"
[ "$rc" = "0" ] || fail "S5: exited $rc, want 0 (the NONE path must be green)"
[ "$none" = "true" ] || fail "S5: NONE=$none, want true - a <48h point ahead of master must not be eligible"
grep -q "no eligible soak point" "$scratch/sum" || fail "S5: summary missing the no-eligible-soak-point block"
git -C "$repo" tag -d v9.9.9-nightly.21991231 >/dev/null

# --- S6: no nightly tags at all -> NONE, true, exit 0, distinct summary.
# The picker refetches tags from origin every run, so the tagless case
# needs a tagless origin too: a fresh bare repo holding master and
# nothing else, plus the local nightly tags deleted (the fetch can
# no longer restore them).
git -C "$repo" init -q --bare "$scratch/empty-origin"
git -C "$repo" remote set-url origin "$scratch/empty-origin"
git -C "$repo" branch -qf master origin/master 2>/dev/null
git -C "$repo" push -q origin master 2>/dev/null
git -C "$repo" for-each-ref --format='%(refname:short)' 'refs/tags/v*-nightly.*' |
  while read -r t; do git -C "$repo" tag -d "$t" >/dev/null; done
run_pick "S6 no-nightly-tags"
[ "$rc" = "0" ] || fail "S6: exited $rc, want 0"
[ "$none" = "true" ] || fail "S6: NONE=$none, want true"
grep -q "no nightly tag exists yet" "$scratch/sum" || fail "S6: summary missing the no-nightly-tag block"

echo
if [ "$fails" -gt 0 ]; then
  echo "RESULT: $fails failing assertion(s)"
  exit 1
fi
echo "RESULT: all soak-pick scenarios green"
[ $keep -eq 1 ] && echo "kept: $scratch"
exit 0