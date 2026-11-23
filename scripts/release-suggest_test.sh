#!/usr/bin/env bash
set -uo pipefail

# release-suggest_test.sh - scratch-repo tests for scripts/release-suggest.sh.
# Run: bash scripts/release-suggest_test.sh
# Synthetic commit ranges against real git fixture repos:
# breaking -> major, feats -> minor, fixes-only -> patch, empty -> patch,
# and the newest-stable base pick (nightlies and rc tags never count).
# Every fixture tags v1.0.0 ON the seed commit - the synthetic commits
# are what the script must see since the stable word.
# The suite spells its temp base NATIVELY on Windows: the SUT reads the range
# through a native python, and a bare MSYS /tmp path resolves against the
# current drive root there, so every fixture reads back empty. cygpath -m
# names the SAME directory; POSIX hosts and already-native paths pass through.

SUG="$(cd "$(dirname "$0")" && pwd)/release-suggest.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1"; }

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
[ -z "$_pin_hits" ] && ok \
	|| bad "PIN: an assertion pipes a writer into grep -q (SIGPIPE under pipefail): $_pin_hits"
_PIN_TOKEN='|'
_pin_sample="  printf '%s\\n' \"\$OUT\" $_PIN_TOKEN grep -qxF -- \"\$1\""
grep -qE "$_PIN_SHAPE" <<<"$_pin_sample" && ok \
	|| bad "PIN: the detector no longer sees the pre-fix pipe shape"


GUSER=(git -c user.email=t@t -c user.name=t)

# fixture_error <msg>: the named cause plus the stake. Fixtures are real git
# repos driven with `git -C "$root"`, and `git -C ""` resolves to the CWD, so an
# empty fixture root commits into the checkout the suite runs in.
fixture_error() {
	echo "release-suggest_test: $* - refusing to continue (a fixture commit would land in the repo the suite was launched from)" >&2
}

# winbtw seam: the fixture base must be spelled the way a NATIVE consumer reads
# it (see the header). cygpath is absent on POSIX hosts, where this is a no-op.
TMPDIR_NATIVE="$(cygpath -m "${TMPDIR:-/tmp}" 2>/dev/null || true)"
[ -n "$TMPDIR_NATIVE" ] && [ -d "$TMPDIR_NATIVE" ] && export TMPDIR="$TMPDIR_NATIVE"

# Temp-parent preflight (card t_60874fa5): loud before the first fixture.
TMPROOT_PROBE="$(mktemp -d "${TMPDIR:-/tmp}/rls-probe.XXXXXX")" \
	|| { fixture_error "mktemp -d failed under '${TMPDIR:-/tmp}'"; exit 1; }
[ -d "$TMPROOT_PROBE" ] || { fixture_error "mktemp -d returned '$TMPROOT_PROBE', which is not a directory"; exit 1; }
rmdir "$TMPROOT_PROBE" 2>/dev/null || true

# new_fixture: a fresh scratch repo; every step checked, every failure named.
new_fixture() {
	local root
	if ! root="$(mktemp -d "${TMPDIR:-/tmp}/rls-fx.XXXXXX")"; then
		fixture_error "mktemp -d failed under '${TMPDIR:-/tmp}'"
		return 1
	fi
	if [ -z "$root" ] || [ ! -d "$root" ]; then
		fixture_error "mktemp -d returned '$root', which is not a directory"
		return 1
	fi
	if ! git init -q -b master "$root"; then
		fixture_error "git init failed in '$root'"
		return 1
	fi
	if ! "${GUSER[@]}" -C "$root" commit --allow-empty -q -m seed; then
		fixture_error "the seed commit failed in '$root'"
		return 1
	fi
	if ! "${GUSER[@]}" -C "$root" tag v1.0.0; then
		fixture_error "tagging v1.0.0 failed in '$root'"
		return 1
	fi
	echo "$root"
}

# --- ISO: the fixture builder refuses a missing temp parent -----------------
# An unchecked mktemp hands back an empty root and every `git -C "$root"` then
# commits into the launch repo (card t_60874fa5). Prove the refusal stays.
iso="$(TMPDIR="${TMPDIR:-/tmp}/rls-no-such-dir" new_fixture 2>/dev/null)"
[ -z "$iso" ] && ok || bad "ISO: a failed fixture must echo no root (got '$iso')"
TMPDIR="${TMPDIR:-/tmp}/rls-no-such-dir" new_fixture >/dev/null 2>&1 \
	&& bad "ISO: new_fixture must fail when its temp parent does not exist" || ok

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
	grep -qE "^SUGGEST=$1$" <<<"$OUT" || bad "$2: expected SUGGEST=$1, got: $(printf '%s' "$OUT" | tail -1)"
	grep -q "Suggested bump: $1" <<<"$OUT" && ok || bad "$2: missing human 'Suggested bump: $1' line"
}

# --- S1: BREAKING CHANGE marker (`feat!:` shape) -> major -------------------
fx=$(new_fixture) || exit 1
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "fix: a small fix"
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "feat!: replace the store with an fs-free one"
run_suggest "$fx"
assert_level major "S1 breaking marker"
grep -qE '^  [0-9a-f]+ feat!:' <<<"$OUT" && ok || bad "S1: evidence should name the feat! commit"
grep -q "fix: a small fix" <<<"$OUT" && \
	bad "S1: the fix commit must NOT appear in a major's evidence list" || ok

# --- S2: BREAKING CHANGE footer in the body -> major ------------------------
fx=$(new_fixture) || exit 1
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "feat: new config surface" -m "BREAKING CHANGE: the gateway key is gone"
run_suggest "$fx"
assert_level major "S2 BREAKING CHANGE footer"

# --- S3: removed API surface -> major ---------------------------------------
fx=$(new_fixture) || exit 1
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "remove(gateway): drop the dead channel"
run_suggest "$fx"
assert_level major "S3 remove prefix"

# --- S4: renamed API surface -> major ---------------------------------------
fx=$(new_fixture) || exit 1
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "rename: messageRole helper"
run_suggest "$fx"
assert_level major "S4 rename prefix"

# --- S5: feat commits (no breaking) -> minor with both feats as evidence ----
fx=$(new_fixture) || exit 1
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "feat(train): the landing lane"
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "fix(tui): sticky layout"
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "feat(ci): nightly job wiring"
run_suggest "$fx"
assert_level minor "S5 feats"
n_ev=$(printf '%s' "$OUT" | grep -cE '^  [0-9a-f]+ feat\(')
[ "$n_ev" -eq 2 ] && ok || bad "S5: expected 2 evidence commits, got $n_ev"

# --- S6: fixes/chore/docs + a merge commit -> patch, empty evidence ---------
fx=$(new_fixture) || exit 1
"${GUSER[@]}" -C "$fx" checkout -q -b side
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "chore(side): scratch"
"${GUSER[@]}" -C "$fx" checkout -q master
"${GUSER[@]}" -C "$fx" merge -q --no-ff -m "Merge branch side" side >/dev/null 2>&1
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "fix: a race in the send path"
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "docs: note the soak law"
run_suggest "$fx"
assert_level patch "S6 fixes+merge"
grep -q "Evidence: none" <<<"$OUT" && ok || bad "S6: patch with no driver should have empty evidence"

# --- S7: empty range -> patch ------------------------------------------------
fx=$(new_fixture) || exit 1
run_suggest "$fx" v1.0.0
assert_level patch "S7 empty range"
[ "$RC" -eq 0 ] && ok || bad "S7: a suggestion must exit 0 even on an empty range (got $RC)"

# --- S8: base pick: newest STABLE tag (nightly + rc tags never count) --------
fx=$(new_fixture) || exit 1
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "feat: a"
"${GUSER[@]}" -C "$fx" tag v1.2.0
"${GUSER[@]}" -C "$fx" tag v1.2.0-rc1
"${GUSER[@]}" -C "$fx" tag v1.3.0-nightly.20261021
"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "feat: b"
run_suggest "$fx"
assert_level minor "S8 default base"
grep -q "since v1.2.0, 1 commits" <<<"$OUT" && ok || bad "S8: expected base v1.2.0 (not rc1/nightly) with 1 commit since"

echo
echo "release-suggest_test: $pass ok, $fail fail"
[ "$fail" -eq 0 ]
