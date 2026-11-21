#!/usr/bin/env bash
set -uo pipefail

# docs-health_test.sh - scratch-repo tests for scripts/docs-health.sh.
# Run: bash scripts/docs-health_test.sh
#
# Every case builds a real git fixture (dated commits, real tags) and drives
# the SUT with DOCS_HEALTH_MAP + DOCS_HEALTH_NOW, so the rule math is pinned
# without touching the repo's own mapping table. What is pinned:
#
#   - coupled due rule: max mapped-code commit time STRICTLY after the doc's
#     last commit is due; equal or older is not (a doc that lands with its
#     code is not stale the moment it lands);
#   - age fallback: due only when a STABLE release tag is newer than the doc
#     AND the doc is AGE_MIN_DAYS old - a nightly prerelease tag never counts
#     as the release that ages a doc;
#   - link integrity: an untracked target is broken, a missing heading anchor
#     is broken, a GitHub-slugged heading resolves, fenced examples are not
#     links;
#   - the verdict line is one greppable DOCS WARNING / DOCS OK, and findings
#     still exit 0 (advisory) while a broken mapping table (bad path, untracked
#     doc) and a shallow clone exit 2 (unmeasurable, not a finding);
#   - the step-summary markdown lands in $GITHUB_STEP_SUMMARY;
#   - the repo's OWN table is live: every tracked .md has a row (a new doc
#     without a row fails here instead of going silently unmeasured), and the
#     notes script's default output dir is ignored (a generated release
#     artifact must not be committed, where it would read as a doc).

SUT="$(cd "$(dirname "$0")" && pwd)/docs-health.sh"
REPO="$(cd "$(dirname "$0")/.." && pwd)"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1"; }

GUSER=(git -c user.email=t@t -c user.name=t -c commit.gpgsign=false -c core.autocrlf=false)
T=1793800000
# the fixture's seed commit is dated BEFORE every fixture commit, so a tag on
# it can never read as "a release since the doc"
SEED_T=1793000000

# w <fixture> <path> <content>: write a file with its directory
w() {
	local fx=$1 p=$2 content=$3
	mkdir -p "$fx/$(dirname "$p")"
	printf '%s\n' "$content" >"$fx/$p"
}

# c <fixture> <subject>: a dated empty commit (dates force log order)
c() {
	local fx=$1 msg=$2
	T=$((T + 60))
	GIT_COMMITTER_DATE="@$T" GIT_AUTHOR_DATE="@$T" \
		"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "$msg"
}

# ci <fixture> <path> <content> <subject>: write, stage, dated commit
ci() {
	local fx=$1 p=$2 content=$3 msg=$4
	w "$fx" "$p" "$content"
	T=$((T + 60))
	GIT_COMMITTER_DATE="@$T" GIT_AUTHOR_DATE="@$T" \
		"${GUSER[@]}" -C "$fx" add -A
	GIT_COMMITTER_DATE="@$T" GIT_AUTHOR_DATE="@$T" \
		"${GUSER[@]}" -C "$fx" commit -q -m "$msg"
}

# new_fixture <map-body...>: an empty repo whose mapping table is a sibling
# file (never inside the fixture - it would show up in the tree)
#
# The fixture root must be a NATIVE path: a bare /tmp hands native git.exe a
# cwd it cannot change to, and every add/commit then runs against nothing
# (the fixtures look empty and the suite blames the SUT).
TMPROOT="$(cd "${TMPDIR:-/tmp}" 2>/dev/null && pwd -W 2>/dev/null || printf '%s' "${TMPDIR:-/tmp}")"

# fixture_error <msg>: the named cause plus the stake. Fixtures are real git
# repos driven with `git -C "$root"`, and `git -C ""` resolves to the CWD, so an
# empty fixture root commits (and tags) in the checkout the suite runs in.
fixture_error() {
	echo "docs-health_test: $* - refusing to continue (a fixture commit would land in the repo the suite was launched from)" >&2
}

# Temp-parent preflight (card t_60874fa5): loud before the first fixture.
TMPROOT_PROBE="$(mktemp -d "$TMPROOT/docs-health-probe.XXXXXX")" \
	|| { fixture_error "mktemp -d failed under '$TMPROOT'"; exit 1; }
[ -d "$TMPROOT_PROBE" ] || { fixture_error "mktemp -d returned '$TMPROOT_PROBE', which is not a directory"; exit 1; }
rmdir "$TMPROOT_PROBE" 2>/dev/null || true

# new_fixture: a fresh scratch repo; every step checked, every failure named.
new_fixture() {
	local root
	if ! root="$(mktemp -d "$TMPROOT/docs-health-fx.XXXXXX")"; then
		fixture_error "mktemp -d failed under '$TMPROOT'"
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
	if ! GIT_COMMITTER_DATE="@$SEED_T" GIT_AUTHOR_DATE="@$SEED_T" \
		"${GUSER[@]}" -C "$root" commit -q --allow-empty -m seed; then
		fixture_error "the seed commit failed in '$root'"
		return 1
	fi
	printf '%s\n' "$@" >"${root}-map"
	echo "$root"
}

# --- ISO: the fixture builder refuses a missing temp parent -----------------
# An unchecked mktemp hands back an empty root and every `git -C "$root"` then
# commits (and tags) in the launch repo (card t_60874fa5). Prove the refusal.
iso="$(TMPROOT="$TMPROOT/no-such-dir" new_fixture 'docs/a.md' 2>/dev/null)"
[ -z "$iso" ] && ok || bad "ISO: a failed fixture must echo no root (got '$iso')"
TMPROOT="$TMPROOT/no-such-dir" new_fixture 'docs/a.md' >/dev/null 2>&1 \
	&& bad "ISO: new_fixture must fail when its temp parent does not exist" || ok

# run_health <fixture> [VAR=VALUE...]: stdout+stderr -> OUT, exit -> RC
run_health() {
	local root=$1
	shift
	OUT="$(cd "$root" && env DOCS_HEALTH_MAP="${root}-map" "$@" bash "$SUT" . 2>&1)"
	RC=$?
}

# has <text>: OUT contains the literal
has() { grep -qF -- "$1" <<<"$OUT" && ok || bad "$2: missing [$1]"; }
hasnt() { grep -qF -- "$1" <<<"$OUT" && bad "$2: unexpected [$1]" || ok; }

# --- H1: mapped code newer than the doc -> DUE ------------------------------
fx=$(new_fixture 'docs/a.md|code/') || exit 1
ci "$fx" docs/a.md '# A' "docs: a"
ci "$fx" code/x.go 'package code' "feat(code): newer than the doc"
run_health "$fx" "DOCS_HEALTH_NOW=$T"
[ "$RC" -eq 0 ] && ok || bad "H1: exit $RC (advisory must be 0)"
grep -qE '^docs/a\.md +coupled +[0-9]{4}-[0-9]{2}-[0-9]{2} +code [0-9]{4}-[0-9]{2}-[0-9]{2} +DUE$' <<<"$OUT" \
	&& ok || bad "H1: the due row is wrong: $(grep -m1 'docs/a.md' <<<"$OUT")"
has 'DOCS WARNING: 1 due, 0 broken links' H1

# --- H2: doc newer than its code -> not due, DOCS OK ----------------------
fx=$(new_fixture 'docs/a.md|code/') || exit 1
ci "$fx" code/x.go 'package code' "feat(code): first"
ci "$fx" docs/a.md '# A' "docs: written after the code"
run_health "$fx" "DOCS_HEALTH_NOW=$T"
[ "$RC" -eq 0 ] && ok || bad "H2: exit $RC"
grep -qE '^docs/a\.md +coupled .* ok$' <<<"$OUT" && ok \
	|| bad "H2: expected ok: $(grep -m1 'docs/a.md' <<<"$OUT")"
grep -qxF 'DOCS OK' <<<"$OUT" && ok || bad "H2: DOCS OK missing"
hasnt 'DOCS WARNING' H2

# --- H3: same commit touches doc + code -> not due (strictly after only) ---
fx=$(new_fixture 'docs/a.md|code/') || exit 1
w "$fx" docs/a.md '# A'
w "$fx" code/x.go 'package code'
"${GUSER[@]}" -C "$fx" add -A
T=$((T + 60))
GIT_COMMITTER_DATE="@$T" GIT_AUTHOR_DATE="@$T" \
	"${GUSER[@]}" -C "$fx" commit -q -m "feat(code): doc lands with its code"
run_health "$fx" "DOCS_HEALTH_NOW=$T"
grep -qxF 'DOCS OK' <<<"$OUT" && ok \
	|| bad "H3: a doc committed with its code must not be due: $(printf '%s' "$OUT" | tail -2)"

# --- H4: age fallback - a stable release after the doc + 30d -> DUE -------
fx=$(new_fixture 'docs/a.md') || exit 1
ci "$fx" docs/a.md '# A' "docs: a"
c "$fx" "chore: later work"
"${GUSER[@]}" -C "$fx" tag v1.1.0
doc_ct=$(git -C "$fx" log -1 --format=%ct -- docs/a.md)
now=$((doc_ct + 45 * 86400))
run_health "$fx" "DOCS_HEALTH_NOW=$now"
grep -qE '^docs/a\.md +age .* 45d, 1 rel +DUE$' <<<"$OUT" && ok \
	|| bad "H4: age rule row wrong: $(grep -m1 'docs/a.md' <<<"$OUT")"
has 'DOCS WARNING: 1 due, 0 broken links' H4

# --- H5: the age guard - a release exists but the doc is young ------------
fx=$(new_fixture 'docs/a.md') || exit 1
ci "$fx" docs/a.md '# A' "docs: a"
c "$fx" "chore: later work"
"${GUSER[@]}" -C "$fx" tag v1.1.0
doc_ct=$(git -C "$fx" log -1 --format=%ct -- docs/a.md)
run_health "$fx" "DOCS_HEALTH_NOW=$((doc_ct + 20 * 86400))"
grep -qE '^docs/a\.md +age .* 20d, 1 rel +ok$' <<<"$OUT" && ok \
	|| bad "H5: the 30-day guard did not hold: $(grep -m1 'docs/a.md' <<<"$OUT")"
grep -qxF 'DOCS OK' <<<"$OUT" && ok || bad "H5: DOCS OK missing"

# --- H6: age fallback - no release since the doc -> not due ---------------
fx=$(new_fixture 'docs/a.md') || exit 1
"${GUSER[@]}" -C "$fx" tag v0.9.0
ci "$fx" docs/a.md '# A' "docs: a"
"${GUSER[@]}" -C "$fx" tag v1.0.0
doc_ct=$(git -C "$fx" log -1 --format=%ct -- docs/a.md)
run_health "$fx" "DOCS_HEALTH_NOW=$((doc_ct + 200 * 86400))"
grep -qE '^docs/a\.md +age .* 200d, 0 rel +ok$' <<<"$OUT" && ok \
	|| bad "H6: zero releases since must stay ok: $(grep -m1 'docs/a.md' <<<"$OUT")"

# --- H7: a nightly prerelease tag never ages a doc ------------------------
fx=$(new_fixture 'docs/a.md') || exit 1
ci "$fx" docs/a.md '# A' "docs: a"
c "$fx" "chore: later work"
"${GUSER[@]}" -C "$fx" tag v1.1.0-nightly.20260101
doc_ct=$(git -C "$fx" log -1 --format=%ct -- docs/a.md)
run_health "$fx" "DOCS_HEALTH_NOW=$((doc_ct + 200 * 86400))"
grep -qE '^docs/a\.md +age .* 200d, 0 rel +ok$' <<<"$OUT" && ok \
	|| bad "H7: a nightly tag must not count as a release: $(grep -m1 'docs/a.md' <<<"$OUT")"

# --- H8: a broken internal link is found, and the run still exits 0 -------
fx=$(new_fixture 'docs/a.md') || exit 1
ci "$fx" docs/a.md '# A
[gone](b.md)' "docs: a link to a file nobody wrote"
run_health "$fx" "DOCS_HEALTH_NOW=$T"
[ "$RC" -eq 0 ] && ok || bad "H8: findings must exit 0, got $RC"
has 'DOCS WARNING: 0 due, 1 broken links' H8
has 'docs/a.md -> b.md (target not tracked: docs/b.md)' H8

# --- H9: a link to a tracked non-md file is fine --------------------------
fx=$(new_fixture 'docs/a.md') || exit 1
w "$fx" docs/a.md '# A
[code](../code/x.go)'
w "$fx" code/x.go 'package code'
"${GUSER[@]}" -C "$fx" add -A
T=$((T + 60))
GIT_COMMITTER_DATE="@$T" GIT_AUTHOR_DATE="@$T" \
	"${GUSER[@]}" -C "$fx" commit -q -m "docs: link the code"
run_health "$fx" "DOCS_HEALTH_NOW=$T"
grep -qxF 'DOCS OK' <<<"$OUT" && ok \
	|| bad "H9: a tracked ../ target must resolve: $(printf '%s' "$OUT" | tail -2)"

# --- H10: anchors - same-file, cross-file, GitHub slugging ----------------
fx=$(new_fixture 'docs/a.md,docs/b.md') || exit 1
w "$fx" docs/a.md '# A
## The `x` **loud** one
[self](#the-x-loud-one)
[cross](b.md#geometry-contracts)
[bad-cross](b.md#no-such-heading)
[bad-self](#nope)'
w "$fx" docs/b.md '# B
## Geometry contracts'
"${GUSER[@]}" -C "$fx" add -A
T=$((T + 60))
GIT_COMMITTER_DATE="@$T" GIT_AUTHOR_DATE="@$T" \
	"${GUSER[@]}" -C "$fx" commit -q -m "docs: anchors"
run_health "$fx" "DOCS_HEALTH_NOW=$T"
has 'DOCS WARNING: 0 due, 2 broken links' H10
has 'docs/a.md -> #nope (no heading anchor #nope in docs/a.md)' H10
has 'docs/a.md -> b.md#no-such-heading (no heading anchor #no-such-heading in docs/b.md)' H10
hasnt 'the-x-loud-one (' H10
hasnt 'geometry-contracts (' H10

# --- H11: fenced examples are not links -----------------------------------
fx=$(new_fixture 'docs/a.md') || exit 1
w "$fx" docs/a.md '# A
```
[example](nowhere.md)
```
[real](gone.md)'
"${GUSER[@]}" -C "$fx" add -A
T=$((T + 60))
GIT_COMMITTER_DATE="@$T" GIT_AUTHOR_DATE="@$T" \
	"${GUSER[@]}" -C "$fx" commit -q -m "docs: fenced example"
run_health "$fx" "DOCS_HEALTH_NOW=$T"
has 'DOCS WARNING: 0 due, 1 broken links' H11
hasnt 'nowhere.md' H11

# --- H12: an unmapped tracked doc is named, never silently skipped --------
fx=$(new_fixture 'docs/a.md') || exit 1
ci "$fx" docs/a.md '# A' "docs: a"
ci "$fx" docs/new.md '# New' "docs: a doc nobody mapped"
run_health "$fx" "DOCS_HEALTH_NOW=$T"
grep -qE '^docs/new\.md +- .* +UNMAPPED$' <<<"$OUT" && ok \
	|| bad "H12: unmapped row missing: $(grep -m1 'new.md' <<<"$OUT")"
has 'DOCS WARNING: 1 unmapped tracked doc(s): docs/new.md' H12
# the unmapped verdict REPLACES the DOCS OK - one verdict line, never both
hasnt 'DOCS OK' H12
[ "$(grep -cE '^(DOCS OK|DOCS WARNING)' <<<"$OUT")" -eq 1 ] && ok \
	|| bad "H12: expected exactly one verdict line, got $(grep -cE '^(DOCS OK|DOCS WARNING)' <<<"$OUT")"

# --- H13: mapping rot exits 2 - a path with no tracked files --------------
fx=$(new_fixture 'docs/a.md|code/') || exit 1
ci "$fx" docs/a.md '# A' "docs: a"
run_health "$fx" "DOCS_HEALTH_NOW=$T"
[ "$RC" -eq 2 ] && ok || bad "H13: a bad mapping path must exit 2, got $RC"
has "mapping names a path with no tracked files: 'code/' (doc: docs/a.md)" H13

# --- H14: mapping rot exits 2 - an untracked doc --------------------------
fx=$(new_fixture 'docs/ghost.md|docs/') || exit 1
ci "$fx" docs/a.md '# A' "docs: a"
run_health "$fx" "DOCS_HEALTH_NOW=$T"
[ "$RC" -eq 2 ] && ok || bad "H14: an untracked doc in the table must exit 2, got $RC"
has "mapping names an untracked doc: 'docs/ghost.md'" H14

# --- H15: a shallow clone is unmeasurable, not green ----------------------
fx=$(new_fixture 'docs/a.md|code/') || exit 1
ci "$fx" docs/a.md '# A' "docs: a"
ci "$fx" code/x.go 'package code' "feat(code): x"
shallow="${fx}-shallow"
git clone -q --depth 1 "file://$fx" "$shallow"
[ "$(git -C "$shallow" rev-parse --is-shallow-repository)" = "true" ] && ok \
	|| bad "H15: fixture is not shallow"
OUT="$(cd "$shallow" && env DOCS_HEALTH_MAP="${fx}-map" bash "$SUT" . 2>&1)"
RC=$?
[ "$RC" -eq 2 ] && ok || bad "H15: a shallow clone must exit 2, got $RC"
has 'shallow checkout' H15

# --- H16: the step summary carries the table ------------------------------
fx=$(new_fixture 'docs/a.md|code/') || exit 1
ci "$fx" docs/a.md '# A' "docs: a"
ci "$fx" code/x.go 'package code' "feat(code): newer"
SUM="${fx}-summary.md"
: >"$SUM"
OUT="$(cd "$fx" && env DOCS_HEALTH_MAP="${fx}-map" DOCS_HEALTH_NOW="$T" \
	GITHUB_STEP_SUMMARY="$SUM" bash "$SUT" . 2>&1)"
RC=$?
[ "$RC" -eq 0 ] && ok || bad "H16: exit $RC"
grep -qF '## Repo docs health (advisory)' "$SUM" && ok || bad "H16: summary heading missing"
grep -qF '| `docs/a.md` | coupled |' "$SUM" && ok || bad "H16: summary row missing"
grep -qF '| doc | mode | doc touched | code / age | status |' "$SUM" && ok \
	|| bad "H16: summary table header missing"
# the greppable line stays on stdout, not in the summary
grep -qF 'DOCS WARNING' "$SUM" && bad "H16: the verdict belongs to the log, not the summary" || ok

# --- H17: the repo's own table is live (every tracked .md has a row) ------
[ -x "$SUT" ] || chmod +x "$SUT"
OUT="$(cd "$REPO" && bash "$SUT" . 2>&1)"
RC=$?
[ "$RC" -eq 0 ] && ok || bad "H17: the real checkout must exit 0, got $RC"
grep -qE '^(DOCS OK|DOCS WARNING: [0-9]+ due, [0-9]+ broken links)$' <<<"$OUT" && ok \
	|| bad "H17: no greppable verdict line: $(printf '%s' "$OUT" | tail -2)"
hasnt 'UNMAPPED' H17
tracked_md="$(cd "$REPO" && git ls-files '*.md' | wc -l | tr -d ' ')"
grep -qF "docs-health: ${tracked_md} doc(s)" <<<"$OUT" && ok \
	|| bad "H17: table covers ${tracked_md} tracked docs but reports $(grep -m1 'docs-health:' <<<"$OUT")"

# --- H18: findings + unmapped -> the contract line is the last DOCS line ---
fx=$(new_fixture 'docs/a.md|code/') || exit 1
ci "$fx" docs/a.md '# A' "docs: a"
ci "$fx" code/x.go 'package code' "feat(code): newer"
ci "$fx" docs/new.md '# New' "docs: unmapped"
run_health "$fx" "DOCS_HEALTH_NOW=$T"
has 'DOCS WARNING: 1 unmapped tracked doc(s): docs/new.md' H18
has 'DOCS WARNING: 1 due, 0 broken links' H18
last="$(grep -E '^(DOCS OK|DOCS WARNING)' <<<"$OUT" | tail -1)"
[ "$last" = 'DOCS WARNING: 1 due, 0 broken links' ] && ok \
	|| bad "H18: the contract line must come last, got [$last]"

# --- H19: generated release output stays ignored --------------------------
# dist/ is the notes script's default output dir (override in its header). A
# committed artifact reads as a doc to H17, and a local run of the script
# leaves the files in the tree, so the ignore rule is part of the table being
# live - not a convenience.
for artifact in dist/release-notes.md dist/release-notes-body.md dist/discord-release.json; do
	( cd "$REPO" && git check-ignore -q "$artifact" ) && ok \
		|| bad "H19: generated release artifact not gitignored: $artifact (running the notes script would dirty the checkout)"
done

# --- H20: is_tracked cannot lose a match to the writer's SIGPIPE ----------
# `printf '%s\n' "$TRACKED" | grep -qxF` under pipefail hands back the WRITER's
# SIGPIPE when grep exits at the FIRST match, so a tracked path sorts early in
# `git ls-files` (.agents/*) and reads as untracked: the mapping-rot die then
# fires on a healthy table, nightly-only and timing-dependent. No membership
# test in the SUT may pipe into `grep -q` - a herestring has no writer, so
# grep's early exit costs nothing.
PIPE_SHAPE='\|[[:space:]]*grep[[:space:]]+-[a-zA-Z]*q'
# a comment can name the hazard (this file's own header does); only code counts
piped="$(grep -vE '^[[:space:]]*#' "$SUT" | grep -nE "$PIPE_SHAPE")"
[ -z "$piped" ] && ok || bad "H20: a membership test pipes into grep -q (writer SIGPIPE under pipefail): $piped"
# the pin must stay red on the exact shape it exists to stop. The sample is
# assembled from PIPE_TOKEN, never written out: H21 scans THIS file, so a
# literal pre-fix line here would be the pin's own first finding.
PIPE_TOKEN='|'
PIPE_SAMPLE="  printf '%s\\n' \"\$TRACKED\" $PIPE_TOKEN grep -qxF -- \"\$1\""
grep -qE "$PIPE_SHAPE" <<<"$PIPE_SAMPLE" && ok \
	|| bad "H20: the detector no longer sees the pre-fix pipe shape"

is_tracked_src="$(awk '/^is_tracked\(\)/{f=1} f{print} f&&/^\}$/{exit}' "$SUT")"
[ -n "$is_tracked_src" ] && ok || bad "H20: is_tracked() not found in the SUT"
# drive the extracted function on the REAL ls-files list, .agents/COMMIT.md
# first in sort order (the worst case for an early exit)
TRACKED_REAL="$(cd "$REPO" && git ls-files)"
[ -n "$TRACKED_REAL" ] && ok || bad "H20: the real checkout lists no tracked files"
races="$(
	set -uo pipefail
	export TRACKED="$TRACKED_REAL"
	eval "$is_tracked_src"
	n=0 i=0
	while [ "$i" -lt 100 ]; do
		is_tracked .agents/COMMIT.md || n=$((n + 1))
		i=$((i + 1))
	done
	printf '%s' "$n"
)"
[ "$races" = 0 ] && ok \
	|| bad "H20: is_tracked lost a tracked doc $races/100 times (.agents/COMMIT.md over the full ls-files list)"
# the false verdict was one-sided: a genuinely untracked path must still fail
(
	set -uo pipefail
	export TRACKED="$TRACKED_REAL"
	eval "$is_tracked_src"
	is_tracked .agents/NOT-A-REAL-DOC.md
) && bad "H20: is_tracked accepted a path nobody tracks" || ok

# --- H21: the assertion helpers cannot lose a match to a writer's SIGPIPE --
# has()/hasnt() and every inline assertion here route $OUT through a pipeline.
# `printf '%s' "$OUT" | grep -q` under pipefail hands back the WRITER's SIGPIPE
# when grep exits at the FIRST match, so a needle that IS in $OUT reads as
# missing (has: a false red) and a needle that IS there reads as absent
# (hasnt: a false green, which hides a defect instead of burning a retry).
# $OUT is the whole table now, so the odds are not theoretical. A herestring
# has no writer: grep's early exit costs nothing.
SELF="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"
piped="$(grep -vE '^[[:space:]]*#' "$SELF" | grep -nE "$PIPE_SHAPE")"
[ -z "$piped" ] && ok || bad "H21: an assertion in this suite pipes into grep -q: $piped"
# the detector must flag the pre-fix line and leave the herestring alone
grep -qE "$PIPE_SHAPE" <<<"$PIPE_SAMPLE" && ok \
	|| bad "H21: the detector no longer sees the pre-fix pipe shape"
grep -qE "$PIPE_SHAPE" <<<"  grep -qxF -- \"\$1\" <<<\"\$OUT\"" \
	&& bad "H21: the detector flags the fixed herestring shape" || ok

# the behaviour, not the spelling: drive the helpers on a payload big enough to
# outrun the pipe buffer with the needle on the FIRST line (the worst case).
# 20 rounds is enough: once $OUT exceeds the buffer the loss is deterministic,
# not probabilistic - 20/20 on this host, 0/20 for the herestring.
BIG_TABLE="$(awk 'BEGIN{for(i=1;i<=4000;i++) printf "row %05d | coupled | DUE\n", i}')"
has_src="$(awk '/^has\(\)/{print; exit}' "$SELF")"
hasnt_src="$(awk '/^hasnt\(\)/{print; exit}' "$SELF")"
[ -n "$has_src" ] && ok || bad "H21: has() not found in this suite"
[ -n "$hasnt_src" ] && ok || bad "H21: hasnt() not found in this suite"
races="$(
	set -uo pipefail
	export OUT="$BIG_TABLE"
	fail=0
	ok() { :; }
	bad() { fail=$((fail + 1)); }
	eval "$has_src"
	eval "$hasnt_src"
	# a reformatted helper extracts as a fragment and defines nothing, so the
	# drive would pass vacuously - refuse instead of reporting a green
	declare -F has >/dev/null && declare -F hasnt >/dev/null || { printf 'nodfn'; exit 0; }
	i=0
	while [ "$i" -lt 20 ]; do
		has 'row 00001 |' drive
		hasnt 'row 99999 |' drive
		i=$((i + 1))
	done
	printf '%s' "$fail"
)"
case "$races" in
0) ok ;;
nodfn) bad "H21: the extracted has()/hasnt() defined no function (reformatted?)" ;;
*) bad "H21: the assertion helpers lost a match $races/40 times on a $(( ${#BIG_TABLE} / 1024 )) KB payload" ;;
esac
# the hasnt direction fails the other way: a present needle must report bad()
greens="$(
	set -uo pipefail
	export OUT="$BIG_TABLE"
	fail=0
	ok() { :; }
	bad() { fail=$((fail + 1)); }
	eval "$hasnt_src"
	declare -F hasnt >/dev/null || { printf 'nodfn'; exit 0; }
	i=0 lost=0
	while [ "$i" -lt 20 ]; do
		before=$fail
		hasnt 'row 00001 |' probe
		[ "$fail" -eq "$before" ] && lost=$((lost + 1))
		i=$((i + 1))
	done
	printf '%s' "$lost"
)"
case "$greens" in
0) ok ;;
nodfn) bad "H21: the extracted hasnt() defined no function (reformatted?)" ;;
*) bad "H21: hasnt() read a present needle as absent $greens/20 times" ;;
esac

echo
echo "docs-health_test: $pass ok, $fail fail"
[ "$fail" -eq 0 ]
