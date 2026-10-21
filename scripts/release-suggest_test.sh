#!/usr/bin/env bash
set -uo pipefail

# release-suggest_test.sh - scratch-repo tests for scripts/release-suggest.sh.
# Run: bash scripts/release-suggest_test.sh
# Synthetic commit ranges against real git fixture repos:
# breaking -> major, feats -> minor, fixes-only -> patch, empty -> patch,
# and the newest-stable base pick (nightlies and rc tags never count).
# Every fixture tags v1.0.0 ON the seed commit - the synthetic commits
# are what the script must see since the stable word.

SUG="$(cd "$(dirname "$0")" && pwd)/release-suggest.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1"; }

GUSER=(git -c user.email=t@t -c user.name=t)

new_fixture() {
	local root
	root="$(mktemp -d "${TMPDIR:-/tmp}/rls-fx.XXXXXX")"
	git init -q -b master "$root"
	"${GUSER[@]}" -C "$root" commit --allow-empty -q -m seed
	"${GUSER[@]}" -C "$root" tag v1.0.0
	echo "$root"
}

# run_suggest <fixture-path> [base]: full output -> OUT; exit code -> RC
run_suggest() {
	local root=$1 base=${2:-}
	if [ -n "$base" ]; then
		OUT="$(cd "$root" && bash "$SUG" "$base" 2>&1)"
	else
		OUT="$(cd "$root" && bash "$SUG" 2>&1)"
	fi
	RC=$?
}

assert_level() { # <want> <label>
	printf '%s' "$OUT" | grep -qE "^SUGGEST=$1$" || bad "$2: expected SUGGEST=$1, got: $(printf '%s' "$OUT" | tail -1)"
	printf '%s' "$OUT" | grep -q "Suggested bump: $1" && ok || bad "$2: missing human 'Suggested bump: $1' line"
}

# --- S1: BREAKING CHANGE marker (`feat!:` shape) -> major -------------------
fx=$(new_fixture)
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "fix: a small fix"
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "feat!: replace the store with an fs-free one"
run_suggest "$fx"
assert_level major "S1 breaking marker"
printf '%s' "$OUT" | grep -qE '^  [0-9a-f]+ feat!:' && ok || bad "S1: evidence should name the feat! commit"
printf '%s' "$OUT" | grep -q "fix: a small fix" && \
	bad "S1: the fix commit must NOT appear in a major's evidence list" || ok

# --- S2: BREAKING CHANGE footer in the body -> major ------------------------
fx=$(new_fixture)
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "feat: new config surface" -m "BREAKING CHANGE: the gateway key is gone"
run_suggest "$fx"
assert_level major "S2 BREAKING CHANGE footer"

# --- S3: removed API surface -> major ---------------------------------------
fx=$(new_fixture)
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "remove(gateway): drop the dead channel"
run_suggest "$fx"
assert_level major "S3 remove prefix"

# --- S4: renamed API surface -> major ---------------------------------------
fx=$(new_fixture)
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "rename: messageRole helper"
run_suggest "$fx"
assert_level major "S4 rename prefix"

# --- S5: feat commits (no breaking) -> minor with both feats as evidence ----
fx=$(new_fixture)
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "feat(train): the landing lane"
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "fix(tui): sticky layout"
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "feat(ci): nightly job wiring"
run_suggest "$fx"
assert_level minor "S5 feats"
n_ev=$(printf '%s' "$OUT" | grep -cE '^  [0-9a-f]+ feat\(')
[ "$n_ev" -eq 2 ] && ok || bad "S5: expected 2 evidence commits, got $n_ev"

# --- S6: fixes/chore/docs + a merge commit -> patch, empty evidence ---------
fx=$(new_fixture)
"${GUSER[@]}" -C "$fx" checkout -q -b side
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "chore(side): scratch"
"${GUSER[@]}" -C "$fx" checkout -q master
"${GUSER[@]}" -C "$fx" merge -q --no-ff -m "Merge branch side" side >/dev/null 2>&1
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "fix: a race in the send path"
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "docs: note the soak law"
run_suggest "$fx"
assert_level patch "S6 fixes+merge"
printf '%s' "$OUT" | grep -q "Evidence: none" && ok || bad "S6: patch with no driver should have empty evidence"

# --- S7: empty range -> patch ------------------------------------------------
fx=$(new_fixture)
run_suggest "$fx" v1.0.0
assert_level patch "S7 empty range"
[ "$RC" -eq 0 ] && ok || bad "S7: a suggestion must exit 0 even on an empty range (got $RC)"

# --- S8: base pick: newest STABLE tag (nightly + rc tags never count) --------
fx=$(new_fixture)
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "feat: a"
"${GUSER[@]}" -C "$fx" tag v1.2.0
"${GUSER[@]}" -C "$fx" tag v1.2.0-rc1
"${GUSER[@]}" -C "$fx" tag v1.3.0-nightly.20261021
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "feat: b"
run_suggest "$fx"
assert_level minor "S8 default base"
printf '%s' "$OUT" | grep -q "since v1.2.0, 1 commits" && ok || bad "S8: expected base v1.2.0 (not rc1/nightly) with 1 commit since"

echo
echo "release-suggest_test: $pass ok, $fail fail"
[ "$fail" -eq 0 ]