#!/usr/bin/env bash
set -uo pipefail

# drift-gate_test.sh - scratch-repo tests for scripts/drift-gate.sh.
# Run: bash scripts/drift-gate_test.sh
# Real git fixtures: an upstream whose master tip is dated N days back and
# a dev clone ahead K commits. The trip points ARE the ratified law:
# trips at commit 11 and at day 8, stays green at 10 and 7.

GATE="$(cd "$(dirname "$0")" && pwd)/drift-gate.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }

GUSER=(git -c user.email=t@t -c user.name=t)

# fixture <ahead> <base_age_days>: upstream master tip dated age days back,
# dev clone with `ahead` empty commits on top of it. Echoes the dev path.
fixture() {
	local ahead=$1 age=$2
	local root
	root="$(mktemp -d "${TMPDIR:-/tmp}/drift-fx.XXXXXX")"
	(
		cd "$root" &&
		git init -q -b master up &&
		"${GUSER[@]}" -C up commit --allow-empty -q -m mb &&
		if [ "$age" -gt 0 ]; then
			# rewrite the tip's committer date to exactly `age` days ago -
			# the gate measures head_ct - mb_ct in whole days. The -c
			# identity flags matter: CI runners have no default git
			# identity and a naked commit here would fail silently.
			env GIT_COMMITTER_DATE="@$(( $(date +%s) - age * 86400 )) +0000" \
				GIT_AUTHOR_DATE="@$(( $(date +%s) - age * 86400 )) +0000" \
				"${GUSER[@]}" -C up commit --amend -q --no-edit --allow-empty || exit 9
		fi &&
		git clone -q up dev &&
		"${GUSER[@]}" -C dev checkout -q -b dev &&
		for i in $(seq 1 "$ahead"); do
			"${GUSER[@]}" -C dev commit --allow-empty -q -m "c$i" || exit 9
		done &&
		:
	) >/dev/null 2>&1 || { bad "fixture $ahead/$age: setup failed"; echo; return 1; }
	echo "$root/dev"
}

# run_case <dev-path> [env assignments...] -> sets RC, OUT
run_case() {
	local dev=$1
	shift
	if [ -z "$dev" ] || [ ! -d "$dev" ]; then
		OUT=""
		RC=99
		return
	fi
	OUT="$(cd "$dev" && env "$@" bash "$GATE" 2>&1)"
	RC=$?
}

# S1: 10 commits ahead, base fresh -> green (limit is 10, trip is at 11)
fx=$(fixture 10 0)
run_case "$fx"
[ "$RC" -eq 0 ] && ok || bad "S1: 10 commits -> expected exit 0, got $RC"

# S2: 11 commits ahead -> RED at the exact commit 11
fx=$(fixture 11 0)
run_case "$fx"
[ "$RC" -eq 1 ] && ok || bad "S2: 11 commits -> expected exit 1 (trip at commit 11), got $RC"
printf '%s' "$OUT" | grep -q "11 commits ahead of master" && ok || bad "S2: reason text missing the commit count"

# S3: merge-base 8 days old, one dev commit dated now -> RED at day 8
fx=$(fixture 1 8)
run_case "$fx"
[ "$RC" -eq 1 ] && ok || bad "S3: 8 days -> expected exit 1 (trip at day 8), got $RC"
printf '%s' "$OUT" | grep -q "8 days past master's last sync" && ok || bad "S3: reason text missing the day count"

# S4: merge-base 7 days old -> green (trip is at day 8)
fx=$(fixture 1 7)
run_case "$fx"
[ "$RC" -eq 0 ] && ok || bad "S4: 7 days -> expected exit 0, got $RC"

# S5: both budgets blown -> ONE red with both reasons
fx=$(fixture 12 9)
run_case "$fx"
[ "$RC" -eq 1 ] && ok || bad "S5: 12 commits + 9 days -> expected exit 1, got $RC"
printf '%s' "$OUT" | grep -q "commits ahead of master" && \
printf '%s' "$OUT" | grep -q "days past master's last sync" && \
	ok || bad "S5: expected both reasons in one gate"

# S6: unmeasurable base -> exit 2 (also red: unknowable drift is not green)
fx=$(fixture 1 0)
run_case "$fx" DRIFT_BASE=origin/nope
[ "$RC" -eq 2 ] && ok || bad "S6: missing base -> expected exit 2, got $RC"

# S7: env budgets work (tighter limit trips earlier)
fx=$(fixture 2 0)
run_case "$fx" DRIFT_MAX_COMMITS=1
[ "$RC" -eq 1 ] && ok || bad "S7: DRIFT_MAX_COMMITS=1 with 2 commits -> expected exit 1, got $RC"

# S8: the CI summary block renders (GITHUB_STEP_SUMMARY)
fx=$(fixture 11 0)
sum="$(mktemp "${TMPDIR:-/tmp}/drift-sum.XXXXXX")"
run_case "$fx" GITHUB_STEP_SUMMARY="$sum"
printf '%s' "$OUT" | grep -Eq 'DRIFT GATE' && \
	grep -q 'Verdict: RED' "$sum" && \
	grep -q 'commits ahead of master' "$sum" && \
	ok || bad "S8: summary block missing or not written to GITHUB_STEP_SUMMARY"

echo
echo "drift-gate_test: $pass ok, $fail fail"
[ "$fail" -eq 0 ]