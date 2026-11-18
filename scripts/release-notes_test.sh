#!/usr/bin/env bash
set -uo pipefail

# release-notes_test.sh - scratch-repo tests for scripts/release-notes.sh.
# Run: bash scripts/release-notes_test.sh
# Synthetic commit ranges against real git fixture repos: the counts + scopes
# line, the GitHub body's per-class changelog (feat -> refactor -> fix ->
# tune -> docs, newest first inside each, the tail classes capped at 5),
# noise exclusion, empty-range and no-annotation fallbacks, and the POST law
# (t_878b1b7c + t_42854d3e): the two-audience shape (hook line first, then
# user-facing bullets, then the unchanged stats line), FIVE highlights at most
# (newest-5 feat default, the tag annotation's list or the override file's list
# wins), the counts line, the update + changelog link, the no-em-dash style
# law, and the loud 2000-char Discord cap with no trimmer behind it.
# The override file (scripts/release-notes.overrides.md) is per-release hand
# written prose; the script prefers it, and a missing file or a missing section
# still renders (N19, N20, N22). NOTE: run_notes pins NOTES_OVERRIDES into the
# fixture, so the shipped file never leaks into a synthetic range unless a test
# asks for it by name.
# The annotation has to SURVIVE the CI checkout (t_530c4635): a tag ref
# rewritten to its commit by actions/checkout@v4 reads as no annotation, so
# the absence is named on stderr with its remedy and the release workflow's
# restore step is pinned by shape and order (N17, N18).
# Merge commits never carry a bullet - they only count; their side commits
# ride the range and classify on their own subjects.
#
# The suite needs a NATIVE TMPDIR on Windows (mktemp under bare /tmp hands
# the SUT's python a path it cannot open - the fixtures then look empty).
# A TMPDIR that does not exist no longer commits into the launch repo: the
# preflight below fails loud, naming mktemp, before the first fixture.

SUT="$(cd "$(dirname "$0")" && pwd)/release-notes.sh"
SHIPPED_OVERRIDES="$(cd "$(dirname "$0")" && pwd)/release-notes.overrides.md"
# the style law's two characters, built from bytes so no editor mangles them
EM="$(printf '\xe2\x80\x94')"   # em-dash
EN="$(printf '\xe2\x80\x93')"   # en-dash

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1"; }

# fixture_error <msg>: the named cause plus the stake. Every fixture is a real
# git repo driven with `git -C "$root"`, and `git -C ""` resolves to the CWD -
# so an empty fixture root turns the suite into a commit generator for the
# checkout it runs in (proven: 171 fixture commits plus tags v1.0.0, v2.0.0 and
# v0.17.3 landed on the host repo from one hostile TMPDIR). Never continue
# with a root that is not a real directory.
fixture_error() {
	echo "release-notes_test: $* - refusing to continue (a fixture commit would land in the repo the suite was launched from)" >&2
}

# Temp-parent preflight (card t_60874fa5): prove the fixture parent exists and
# is writable BEFORE the first fixture, and name the cause when it is not. This
# is the loud half; new_fixture() re-checks every step so a later failure
# cannot fall through to an empty root either.
TMPROOT_PROBE="$(mktemp -d "${TMPDIR:-/tmp}/rnotes-probe.XXXXXX")" \
	|| { fixture_error "mktemp -d failed under '${TMPDIR:-/tmp}'"; exit 1; }
[ -d "$TMPROOT_PROBE" ] || { fixture_error "mktemp -d returned '$TMPROOT_PROBE', which is not a directory"; exit 1; }
rmdir "$TMPROOT_PROBE" 2>/dev/null || true

GUSER=(git -c user.email=t@t -c user.name=t)
T=1793700000

# c <fixture> <subject...>: a dated empty commit (dates force log order)
c() {
	local fx=$1 msg=$2
	T=$((T + 60))
	GIT_COMMITTER_DATE="@$T" GIT_AUTHOR_DATE="@$T" \
		"${GUSER[@]}" -C "$fx" commit --allow-empty -q -m "$msg"
}

# merge_commit <fixture> <merge-subject> <side-commit-subject...>:
# a true two-parent merge; each side commit rides the second parent
merge_commit() {
	local fx=$1 msg=$2 s
	shift 2
	"${GUSER[@]}" -C "$fx" checkout -q -b _mgside HEAD
	for s in "$@"; do c "$fx" "$s"; done
	"${GUSER[@]}" -C "$fx" checkout -q master
	"${GUSER[@]}" -C "$fx" merge -q --no-ff -m "$msg" _mgside >/dev/null
	"${GUSER[@]}" -C "$fx" branch -q -D _mgside
}

# new_fixture: a fresh scratch repo. Every step is checked and the failure is
# NAMED before returning - an unchecked mktemp here is the whole defect: root
# becomes "" and every later `git -c ... -C "$root"` resolves to the CWD.
new_fixture() {
	local root
	if ! root="$(mktemp -d "${TMPDIR:-/tmp}/rnotes-fx.XXXXXX")"; then
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
	if ! c "$root" seed; then
		fixture_error "the seed commit failed in '$root'"
		return 1
	fi
	if ! git -C "$root" tag v1.0.0; then
		fixture_error "tagging v1.0.0 failed in '$root'"
		return 1
	fi
	echo "$root"
}

# --- ISO: the fixture builder refuses a missing temp parent -----------------
# The defect that produced junk commits on a real branch (card t_60874fa5,
# residue on wt/t_42854d3e): an unchecked mktemp hands back an empty root, and
# every `git -C "$root"` then commits into the launch repo instead. The guard
# is load-bearing - keep it red by proving the refusal here.
iso="$(TMPDIR="${TMPDIR:-/tmp}/rnotes-no-such-dir" new_fixture 2>/dev/null)"
[ -z "$iso" ] && ok || bad "ISO: a failed fixture must echo no root (got '$iso')"
TMPDIR="${TMPDIR:-/tmp}/rnotes-no-such-dir" new_fixture >/dev/null 2>&1 \
	&& bad "ISO: new_fixture must fail when its temp parent does not exist" || ok

# run_notes <fixture> [args...]: stdout+stderr -> OUT; exit -> RC;
# generated files land in <fixture>/out. NOTES_OVERRIDES always points inside
# the fixture (absent unless the test writes one), or at OVERRIDES_FILE when a
# test sets it - the shipped overrides file must never colour a synthetic
# range by accident.
run_notes() {
	local root=$1 ov
	shift
	ov="${OVERRIDES_FILE:-$root/release-notes.overrides.md}"
	OUT="$(cd "$root" && NOTES_OUT_DIR="$root/out" NOTES_OVERRIDES="$ov" bash "$SUT" "$@" 2>&1)"
	RC=$?
}

md() { cat "$1/out/release-notes.md"; }
body() { cat "$1/out/release-notes-body.md"; }
bullets() { grep -c '^- ' "$1/out/release-notes.md" || true; }
body_bullets() { grep -c '^- ' "$1/out/release-notes-body.md" || true; }

# pad <n>: n x's - long synthetic subjects
pad() { printf '%*s' "$1" '' | tr ' ' x; }

# payload_len <fixture>: the Discord payload's content length in chars
payload_len() {
	python3 - "$1/out/discord-release.json" <<'PY'
import json, sys
sys.stdout.reconfigure(newline="\n")
print(len(json.load(open(sys.argv[1], encoding="utf-8"))["content"]))
PY
}

# expect_block <file> <label> <subject...>: the exact bullet block, in order
# - the pick is a contract, not a count
expect_block() {
	local f=$1 label=$2 got want s
	shift 2
	got="$(grep '^- ' "$f" || true)"
	want=""
	for s in "$@"; do want="$want- $s"$'\n'; done
	want="${want%$'\n'}"
	[ "$got" = "$want" ] && ok \
		|| bad "$label: bullets wrong - got [$(printf '%s' "$got" | tr '\n' '|')] want [$(printf '%s' "$want" | tr '\n' '|')]"
}

# the post's five highlights
expect_bullets() {
	local fx=$1 label=$2
	shift 2
	expect_block "$fx/out/release-notes.md" "$label(post)" "$@"
}

# the GitHub body's full per-class changelog
expect_body_bullets() {
	local fx=$1 label=$2
	shift 2
	expect_block "$fx/out/release-notes-body.md" "$label(body)" "$@"
}

# --- N1: counts line + scopes, merges counted, classes in ruled order -------
fx=$(new_fixture) || exit 1
c "$fx" "docs: note the soak law"
c "$fx" "tune(core): default context window 32768"
c "$fx" "fix(side): a race in the send path"
merge_commit "$fx" "Merge branch 'wt/x' into dev" "chore(x): side scaffolding"
c "$fx" "feat(main): the landing lane"
run_notes "$fx"
printf '%s' "$OUT" | grep -qF "1 feature, 0 refactors, 1 fix, 1 tune, 1 doc update, 1 merge across main, x, side, core" \
	&& ok || bad "N1: counts line wrong: $(printf '%s' "$OUT" | grep -m1 'features')"
expect_body_bullets "$fx" N1 \
	"feat(main): the landing lane" \
	"fix(side): a race in the send path" \
	"tune(core): default context window 32768" \
	"docs: note the soak law"
# the post carries the five-highlight law: only the newest feats, and the
# conventional-commit prefix never posts (factory vocabulary, t_42854d3e)
expect_bullets "$fx" N1 "the landing lane"
md "$fx" | grep -qF "Merge branch" && bad "N1: bare merge must not highlight" || ok
body "$fx" | grep -qF "Merge branch" && bad "N1: bare merge must not reach the record" || ok
md "$fx" | grep -qF "chore" && bad "N1: chore leaked into the post" || ok
body "$fx" | grep -qF "chore" && bad "N1: chore leaked into the record" || ok

# --- N2: every feat in the record, no scope dedupe, merges carry no bullet --
fx=$(new_fixture) || exit 1
c "$fx" "fix(core): the oldest fix (t_0000001)"
c "$fx" "feat(core): the context window grows"
merge_commit "$fx" "merge: t_768c6cc1 - the tree tells the product story (three-tier restructure)" \
	"feat(sessions): the hygiene pass"
c "$fx" "chore: scaffolding"
c "$fx" "embed-lane: rebuild webui bundle"
merge_commit "$fx" "merge: async cron overhaul - per-job agent and model (t_ecec864e)" \
	"feat(webui): command palette"
c "$fx" "feat(webui): reference pills for tokens"
run_notes "$fx"
# counts: 4 feats, 1 fix, 0 tunes, 0 docs, 2 merges
printf '%s' "$OUT" | grep -qF "4 features, 0 refactors, 1 fix, 0 tunes, 0 doc updates, 2 merges across webui, sessions, core" \
	&& ok || bad "N2: counts line wrong: $(printf '%s' "$OUT" | grep -m1 'features')"
expect_body_bullets "$fx" N2 \
	"feat(webui): reference pills for tokens" \
	"feat(webui): command palette" \
	"feat(sessions): the hygiene pass" \
	"feat(core): the context window grows" \
	"fix(core): the oldest fix"
# the default highlight pick: newest 5 feats, only feats exist here (4), and
# the post renders them without the factory prefix
expect_bullets "$fx" N2 \
	"reference pills for tokens" \
	"command palette" \
	"the hygiene pass" \
	"the context window grows"
body "$fx" | grep -qF "the tree tells the product story" && bad "N2: card merge reached the record" || ok
[ "$(body "$fx" | grep -cF 'feat(webui)')" -eq 2 ] && ok || bad "N2: same-scope feats were deduped"
body "$fx" | grep -qF "embed-lane" && bad "N2: embed-lane leaked into the record" || ok

# --- N3: merge stories never become bullets ---------------------------------
fx=$(new_fixture) || exit 1
c "$fx" "feat(side): a real thing"
merge_commit "$fx" "merge: restructure part 3 - the seam constitution" "chore(mg): side scratch"
merge_commit "$fx" "merge: restructure" "chore(mg): side scratch"
run_notes "$fx"
expect_body_bullets "$fx" N3 "feat(side): a real thing"
expect_bullets "$fx" N3 "a real thing"

# --- N4: fixes newest-first in the record, order after refactors ------------
fx=$(new_fixture) || exit 1
c "$fx" "fix(a): first"
c "$fx" "fix(a): second in the same scope"
c "$fx" "fix(b): third"
c "$fx" "fix(c): fourth"
c "$fx" "refactor(b): the reshape"
c "$fx" "feat(a): the one feat"
run_notes "$fx"
expect_body_bullets "$fx" N4 \
	"feat(a): the one feat" \
	"refactor(b): the reshape" \
	"fix(c): fourth" \
	"fix(b): third" \
	"fix(a): second in the same scope" \
	"fix(a): first"
expect_bullets "$fx" N4 "the one feat"

# --- N5: noise-only range -> zeros, no bullets, exit 0 ----------------------
fx=$(new_fixture) || exit 1
c "$fx" "chore: one"
c "$fx" "style: two"
c "$fx" "wip: three"
run_notes "$fx"
printf '%s' "$OUT" | grep -qF "0 features, 0 refactors, 0 fixes, 0 tunes, 0 doc updates" \
	&& ok || bad "N5: noise-only counts wrong: $(printf '%s' "$OUT" | grep -m1 'features')"
[ "$(bullets "$fx")" -eq 0 ] && ok || bad "N5: noise must not highlight"
[ "$(body_bullets "$fx")" -eq 0 ] && ok || bad "N5: noise must not reach the record"
[ "$RC" -eq 0 ] && ok || bad "N5: exit $RC"
[ -f "$fx/out/discord-release.json" ] && ok || bad "N5: a noise-only post is still a post"

# --- N6: empty range -> graceful (base passed explicitly) --------------------
fx=$(new_fixture) || exit 1
run_notes "$fx" v1.0.0
[ "$RC" -eq 0 ] && ok || bad "N6: empty range exit $RC"
printf '%s' "$OUT" | grep -qF "0 features, 0 refactors, 0 fixes, 0 tunes" && ok || bad "N6: empty counts missing"

# --- N7: annotated tag, subject only -> it IS the prose paragraph ----------
fx=$(new_fixture) || exit 1
c "$fx" "feat(side): delegates got safer"
"${GUSER[@]}" -C "$fx" tag -a v2.0.0 -m 'Delegates got safer and the tree got flatter'
run_notes "$fx"
printf '%s' "$OUT" | LC_ALL=C grep -qF "🚀 **omega v2.0.0** is out!" && ok || bad "N7: rocket line wrong"
printf '%s' "$OUT" | grep -qF "Delegates got safer and the tree got flatter" \
	&& ok || bad "N7: tag-annotation prose missing"
# base excludes the version tag itself even though it points at HEAD
printf '%s' "$OUT" | grep -qF "1 feature, 0 refactors, 0 fixes, 0 tunes, 0 doc updates across side" \
	&& ok || bad "N7: base pick ignored the version tag"
printf '%s' "$OUT" | grep -qF "full changelog: <https://github.com/EndoTheDev/omega/releases/tag/v2.0.0>" \
	&& ok || bad "N7: update + changelog link missing"
# post: header(1), blank(2), prose(3)
sed -n '2p' "$fx/out/release-notes.md" | grep -q . \
	&& bad "N7: line 2 must stay blank (prose sits at line 3)" || ok
[ "$(sed -n '3p' "$fx/out/release-notes.md")" = "Delegates got safer and the tree got flatter" ] \
	&& ok || bad "N7: prose should be the first content line after the rocket"
BODY="$fx/out/release-notes-body.md"
LC_ALL=C grep -qF "🚀" "$BODY" && bad "N7: body file must not carry the rocket line" || ok
[ "$(sed -n '1p' "$BODY")" = "Delegates got safer and the tree got flatter" ] \
	&& ok || bad "N7: body file prose line"

# --- N8: lightweight tag -> no annotation, so the hook is generated --------
# (t_42854d3e: the post never has a blank line where the hook belongs - with no
# annotation and no override the counts sentence is the honest floor, and the
# gap is named on stderr)
fx=$(new_fixture) || exit 1
c "$fx" "feat(side): a feature"
"${GUSER[@]}" -C "$fx" tag v2.0.0
run_notes "$fx"
[ "$RC" -eq 0 ] && ok || bad "N8: lightweight-tag run exit $RC"
[ "$(sed -n '2p' "$fx/out/release-notes.md")" = "" ] && ok || bad "N8: line 2 must stay blank"
[ "$(sed -n '3p' "$fx/out/release-notes.md")" = "This update brings 1 new feature." ] \
	&& ok || bad "N8: hook should be the generated counts sentence, got [$(sed -n '3p' "$fx/out/release-notes.md")]"
[ "$(sed -n '5p' "$fx/out/release-notes.md")" = "- a feature" ] \
	&& ok || bad "N8: post must carry the user-facing bullet after the hook"
printf '%s' "$OUT" | grep -qF "no override section and no tag annotation" \
	&& ok || bad "N8: the missing hook must be named on stderr"
printf '%s' "$OUT" | grep -qF "1 feature" && ok || bad "N8: counts expected"
printf '%s' "$OUT" | grep -qF "full changelog:" && ok || bad "N8: link expected"

# --- N9: annotated tag quoting survives the JSON escape ---------------------
fx=$(new_fixture) || exit 1
c "$fx" "feat(side): a feature"
"${GUSER[@]}" -C "$fx" tag -a v2.0.0 -m 'Endo said "ship it" - and the webhook must not choke'
run_notes "$fx"
python3 - "$fx/out" <<'PY' && ok || bad "N9: JSON payload shape/escape check failed"
import json, os, sys
d = sys.argv[1]
with open(os.path.join(d, "discord-release.json"), encoding="utf-8") as f:
    p = json.load(f)
with open(os.path.join(d, "release-notes.md"), encoding="utf-8") as f:
    md = f.read()
assert set(p) == {"content", "username"}, sorted(p)
assert p["username"] == "omega releases", p["username"]
assert p["content"] == md, "payload content must equal the post"
assert len(p["content"]) <= 2000
assert "Endo said \"ship it\"" in p["content"]
PY
printf '%s' "$OUT" | grep -q 'over the 2000-char cap' && bad "N9: a normal cycle must not hit the cap" || ok

# --- N10: the record caps the tail classes at 5, keeps every feat ----------
fx=$(new_fixture) || exit 1
for i in 1 2 3 4 5 6 7; do c "$fx" "fix(core): fix $i"; done
for i in 1 2 3 4 5 6 7; do c "$fx" "docs: doc $i"; done
c "$fx" "refactor(core): reshape one"
c "$fx" "feat(core): feature one"
c "$fx" "feat(core): feature two"
run_notes "$fx"
expect_body_bullets "$fx" N10 \
	"feat(core): feature two" \
	"feat(core): feature one" \
	"refactor(core): reshape one" \
	"fix(core): fix 7" \
	"fix(core): fix 6" \
	"fix(core): fix 5" \
	"fix(core): fix 4" \
	"fix(core): fix 3" \
	"docs: doc 7" \
	"docs: doc 6" \
	"docs: doc 5" \
	"docs: doc 4" \
	"docs: doc 3"
printf '%s' "$OUT" | grep -qF "7 fixes" && ok || bad "N10: counts must still carry every commit"
# the post still carries five at most, prefix-free
expect_bullets "$fx" N10 \
	"feature two" \
	"feature one"

# --- N11: the post law - five highlights, counts, link, under 900 chars -----
fx=$(new_fixture) || exit 1
for i in 1 2 3 4 5 6 7; do c "$fx" "feat(core): feature $i"; done
for i in 1 2; do c "$fx" "refactor(core): reshape $i"; done
for i in 1 2 3 4 5 6; do c "$fx" "fix(core): fix $i"; done
c "$fx" "tune(core): one tune"
for i in 1 2 3 4 5 6; do c "$fx" "docs: doc $i"; done
"${GUSER[@]}" -C "$fx" tag v2.0.0
run_notes "$fx"
[ "$RC" -eq 0 ] && ok || bad "N11: exit $RC"
[ "$(bullets "$fx")" -eq 5 ] && ok || bad "N11: post must carry 5 highlights, got $(bullets "$fx")"
expect_bullets "$fx" N11 \
	"feature 7" \
	"feature 6" \
	"feature 5" \
	"feature 4" \
	"feature 3"
printf '%s' "$OUT" | grep -qF "7 features, 2 refactors, 6 fixes, 1 tune, 6 doc updates" \
	&& ok || bad "N11: counts line missing from the post"
printf '%s' "$OUT" | grep -qF "Update: \`omega update\` · full changelog: <https://github.com/EndoTheDev/omega/releases/tag/v2.0.0>" \
	&& ok || bad "N11: update + changelog line missing"
[ "$(sed -n '2p' "$fx/out/release-notes.md")" = "" ] && ok || bad "N11: line 2 must stay blank"
[ "$(sed -n '3p' "$fx/out/release-notes.md")" = "This update brings 7 new features and 6 bug fixes." ] \
	&& ok || bad "N11: generated hook expected at line 3, got [$(sed -n '3p' "$fx/out/release-notes.md")]"
[ "$(wc -c < "$fx/out/release-notes.md")" -lt 900 ] && ok || bad "N11: post must stay under 900 chars ($(wc -c < "$fx/out/release-notes.md"))"
# the record is untouched: 7 feat + 2 refactor + 5 fix + 1 tune + 5 docs
[ "$(body_bullets "$fx")" -eq 20 ] && ok || bad "N11: the record must keep 20 pick bullets, got $(body_bullets "$fx")"

# --- N12: an annotation highlight list OVERRIDES the default five ----------
fx=$(new_fixture) || exit 1
for i in 1 2 3 4 5; do c "$fx" "feat(core): feature $i"; done
MSG="$fx/../tagmsg-n12.txt"
{
	printf '%s\n\n' 'v2.0.0 - the curated release'
	printf '%s\n\n' 'The cycle flattened the tree and made the delegates safer, in one pass.'
	printf '%s\n' '- the landing lane speaks one dialect'
	printf '%s\n' '- the send path lost its retry ladder'
	printf '%s\n' '- delegates get a budget'
} > "$MSG"
"${GUSER[@]}" -C "$fx" tag -a v2.0.0 -F "$MSG"
run_notes "$fx"
[ "$RC" -eq 0 ] && ok || bad "N12: exit $RC"
expect_bullets "$fx" N12 \
	"the landing lane speaks one dialect" \
	"the send path lost its retry ladder" \
	"delegates get a budget"
[ "$(sed -n '3p' "$fx/out/release-notes.md")" = "The cycle flattened the tree and made the delegates safer, in one pass." ] \
	&& ok || bad "N12: annotation prose must ride the post verbatim"
md "$fx" | grep -qF "feat(core)" && bad "N12: the override must replace the default feats" || ok
printf '%s' "$OUT" | grep -qF "5 features" && ok || bad "N12: counts line missing from the post"
# the annotation curates the post, never the record
[ "$(body_bullets "$fx")" -eq 5 ] && ok || bad "N12: the record must keep all 5 feats, got $(body_bullets "$fx")"

# --- N13: wrapped annotation prose -> one paragraph, title dropped ---------
fx=$(new_fixture) || exit 1
for i in 1 2 3 4 5; do c "$fx" "feat(core): feature $i"; done
MSG="$fx/../tagmsg-n13.txt"
{
	printf '%s\n\n' 'v2.0.0 - the wrapped release'
	printf '%s\n' 'Paragraph one ran long so it wrapped'
	printf '%s\n' 'across three lines, and nobody'
	printf '%s\n' 'minded.'
	printf '\n'
	printf '%s\n' 'A second paragraph rides the same'
	printf '%s\n' 'post.'
} > "$MSG"
"${GUSER[@]}" -C "$fx" tag -a v2.0.0 -F "$MSG"
run_notes "$fx"
[ "$RC" -eq 0 ] && ok || bad "N13: exit $RC"
[ "$(sed -n '3p' "$fx/out/release-notes.md")" = "Paragraph one ran long so it wrapped across three lines, and nobody minded. A second paragraph rides the same post." ] \
	&& ok || bad "N13: wrapped prose must collapse into one verbatim paragraph"
md "$fx" | grep -qF "the wrapped release" && bad "N13: the annotation subject is a title, not prose" || ok
[ "$(bullets "$fx")" -eq 5 ] && ok || bad "N13: default five feats expected, got $(bullets "$fx")"

# --- N14: six annotation bullets -> first five post, the sixth named -------
fx=$(new_fixture) || exit 1
for i in 1 2 3; do c "$fx" "feat(core): feature $i"; done
MSG="$fx/../tagmsg-n14.txt"
{
	printf '%s\n' 'the six-bullet release'
	printf '\n'
	for i in 1 2 3 4 5 6; do printf -- '- highlight %s\n' "$i"; done
} > "$MSG"
"${GUSER[@]}" -C "$fx" tag -a v2.0.0 -F "$MSG"
run_notes "$fx"
[ "$RC" -eq 0 ] && ok || bad "N14: exit $RC"
expect_bullets "$fx" N14 \
	"highlight 1" \
	"highlight 2" \
	"highlight 3" \
	"highlight 4" \
	"highlight 5"
printf '%s' "$OUT" | grep -qF "annotation lists 6 highlights - the post keeps the first 5 (five-highlight law); not posted: highlight 6" \
	&& ok || bad "N14: the sixth bullet must be named on stderr: $(printf '%s' "$OUT" | grep -i annotation)"
md "$fx" | grep -qF "highlight 6" && bad "N14: the sixth bullet must not post" || ok
# bullets-only annotation falls back to the subject as the prose paragraph
[ "$(sed -n '3p' "$fx/out/release-notes.md")" = "the six-bullet release" ] \
	&& ok || bad "N14: subject fallback prose missing"

# --- N15: the fat cycle that used to blow the cap now posts under it -------
# 60 feats at pad 40 = a ~3500-char record; the post is five bullets.
fx=$(new_fixture) || exit 1
for i in $(seq 1 60); do c "$fx" "feat(core): feature $i $(pad 40)"; done
"${GUSER[@]}" -C "$fx" tag v2.0.0
run_notes "$fx"
[ "$RC" -eq 0 ] && ok || bad "N15: fat range must exit 0, got $RC"
[ -f "$fx/out/discord-release.json" ] && ok || bad "N15: fat range must still write the payload"
[ "$(payload_len "$fx")" -le 2000 ] && ok || bad "N15: payload over the cap ($(payload_len "$fx") chars)"
[ "$(bullets "$fx")" -eq 5 ] && ok || bad "N15: fat range post must stay at 5 bullets, got $(bullets "$fx")"
[ "$(wc -c < "$fx/out/release-notes-body.md")" -gt 2000 ] && ok || bad "N15: the record should still be the full fat changelog"
[ "$(body_bullets "$fx")" -eq 60 ] && ok || bad "N15: the record must keep all 60 feats, got $(body_bullets "$fx")"
printf '%s' "$OUT" | grep -qF "60 features" && ok || bad "N15: counts must report the true total"

# --- N16: a post over 2000 chars is a loud backstop (no trimmer, no payload)
# A 2100-char annotation line: the tag is broken, so nothing posts.
fx=$(new_fixture) || exit 1
c "$fx" "feat(core): a feature"
"${GUSER[@]}" -C "$fx" tag -a v2.0.0 -m "$(pad 2100)"
mkdir -p "$fx/out"
printf '%s\n' '{"content":"stale payload from a previous run"}' > "$fx/out/discord-release.json"
run_notes "$fx"
[ "$RC" -eq 1 ] && ok || bad "N16: an over-cap post must exit 1, got $RC"
printf '%s' "$OUT" | grep -qF "over the 2000-char cap" && ok || bad "N16: the cap message missing: $(printf '%s' "$OUT" | tail -2)"
[ -f "$fx/out/discord-release.json" ] && bad "N16: a skipped post must not leave a payload (stale JSON must be removed)" || ok
[ -f "$fx/out/discord-exception.txt" ] && bad "N16: the trimmer's exception file is gone from this design" || ok
[ "$(wc -c < "$fx/out/release-notes.md")" -gt 2000 ] && ok || bad "N16: the post itself must still be written for the log"
[ "$(body_bullets "$fx")" -eq 1 ] && ok || bad "N16: the record must survive the loud skip"

# --- N17: the CI checkout shape - a tag ref rewritten to its commit reads ---
# as no annotation (the v0.17.1 defect): the post ships bare and the absence
# is NAMED on stderr, remedy included. Restoring the tag object - what the
# release workflow now does - brings the prose back.
fx=$(new_fixture) || exit 1
c "$fx" "feat(side): a feature"
"${GUSER[@]}" -C "$fx" tag -a v2.0.0 -m 'The paragraph the runner must not drop.'
tag_obj="$("${GUSER[@]}" -C "$fx" rev-parse 'refs/tags/v2.0.0')"
commit="$("${GUSER[@]}" -C "$fx" rev-parse 'v2.0.0^{commit}')"
# actions/checkout@v4's targeted fallback fetch, verbatim effect:
# `git fetch origin +<sha>:refs/tags/<v>` writes the COMMIT into the tag ref
"${GUSER[@]}" -C "$fx" update-ref refs/tags/v2.0.0 "$commit"
[ "$(git -C "$fx" cat-file -t refs/tags/v2.0.0)" = "commit" ] && ok \
	|| bad "N17: fixture did not reproduce the checkout shape"
run_notes "$fx"
printf '%s' "$OUT" | grep -qF 'The paragraph the runner must not drop.' \
	&& bad "N17: a rewritten tag ref must not yield prose (the tag object is gone)" || ok
[ "$(sed -n '1p' "$fx/out/release-notes-body.md")" = "1 feature, 0 refactors, 0 fixes, 0 tunes, 0 doc updates across side" ] \
	&& ok || bad "N17: the body must lead with the counts line when no annotation reads"
printf '%s' "$OUT" | grep -qF 'refs/tags/v2.0.0 carries no readable annotation' \
	&& ok || bad "N17: the absence must be named on stderr: $(printf '%s' "$OUT" | head -2)"
printf '%s' "$OUT" | grep -qF 'restore the tag object: git fetch --force origin refs/tags/v2.0.0:refs/tags/v2.0.0' \
	&& ok || bad "N17: the remedy must be named on stderr"
"${GUSER[@]}" -C "$fx" update-ref refs/tags/v2.0.0 "$tag_obj"
run_notes "$fx"
[ "$(sed -n '1p' "$fx/out/release-notes-body.md")" = "The paragraph the runner must not drop." ] \
	&& ok || bad "N17: a restored tag object must put the prose back at the top of the body"
printf '%s' "$OUT" | grep -qF 'carries no readable annotation' \
	&& bad "N17: a readable annotation must not warn" || ok

# --- N18: the release workflow restores the tag object before the notes ----
# step and fails loud if the remote says annotated while local is a commit.
# The script cannot repair this itself (a bare checkout has no tag object),
# so the workflow step is the fix - pin its shape and its order.
WF="$(cd "$(dirname "$0")/.." && pwd)/.github/workflows/release.yml"
grep -qF 'refs/tags/$VERSION:refs/tags/$VERSION' "$WF" && ok \
	|| bad "N18: the workflow must re-fetch the tag ref"
grep -qF 'is annotated on the remote but the local ref is a bare commit' "$WF" && ok \
	|| bad "N18: the workflow must fail loud on the rewritten shape"
restore_ln="$(grep -nF 'refs/tags/$VERSION:refs/tags/$VERSION' "$WF" | head -1 | cut -d: -f1)"
notes_ln="$(grep -nF 'scripts/release-notes.sh "' "$WF" | head -1 | cut -d: -f1)"
[ -n "$restore_ln" ] && [ -n "$notes_ln" ] && [ "$restore_ln" -lt "$notes_ln" ] && ok \
	|| bad "N18: the restore must run before the notes step ($restore_ln vs $notes_ln)"

# --- N19: the two-audience shape, no override and no annotation -------------
# Hook line first, bullets next, the stats line after them, the update line
# last. A missing override file renders; it never crashes the run.
fx=$(new_fixture) || exit 1
c "$fx" "feat(core): the palette gains a clock editor"
c "$fx" "feat(core): agents get cards"
c "$fx" "fix(core): a stale row after reload"
"${GUSER[@]}" -C "$fx" tag v2.0.0
run_notes "$fx"
[ "$RC" -eq 0 ] && ok || bad "N19: a missing override file must not crash, exit $RC"
[ ! -f "$fx/release-notes.overrides.md" ] && ok || bad "N19: fixture must carry no override file"
MD19="$fx/out/release-notes.md"
[ "$(sed -n '1p' "$MD19")" = "🚀 **omega v2.0.0** is out!" ] && ok || bad "N19: line 1 is the header"
[ "$(sed -n '2p' "$MD19")" = "" ] && ok || bad "N19: line 2 is blank"
[ "$(sed -n '3p' "$MD19")" = "This update brings 2 new features and 1 bug fix." ] \
	&& ok || bad "N19: line 3 is the hook, got [$(sed -n '3p' "$MD19")]"
[ "$(sed -n '4p' "$MD19")" = "" ] && ok || bad "N19: line 4 is blank"
[ "$(sed -n '5p' "$MD19")" = "- agents get cards" ] && ok || bad "N19: line 5 is the newest bullet, got [$(sed -n '5p' "$MD19")]"
[ "$(sed -n '6p' "$MD19")" = "- the palette gains a clock editor" ] \
	&& ok || bad "N19: line 6 is the older bullet, got [$(sed -n '6p' "$MD19")]"
[ "$(sed -n '7p' "$MD19")" = "" ] && ok || bad "N19: line 7 is blank"
[ "$(sed -n '8p' "$MD19")" = "2 features, 0 refactors, 1 fix, 0 tunes, 0 doc updates across core" ] \
	&& ok || bad "N19: line 8 is the unchanged stats line, got [$(sed -n '8p' "$MD19")]"
[ "$(sed -n '9p' "$MD19")" = "" ] && ok || bad "N19: line 9 is blank"
printf '%s' "$(sed -n '10p' "$MD19")" | grep -qF "Update: \`omega update\` · full changelog:" \
	&& ok || bad "N19: line 10 is the update + changelog link"
[ "$(sed -n '11p' "$MD19")" = "" ] && ok || bad "N19: the post ends after the link"
# the shape held: no bullet rode above the hook
grep -n '^- ' "$MD19" | cut -d: -f1 | awk '$1 <= 3 {exit 1}' && ok || bad "N19: a bullet rode above the hook"
grep -qF "$EM" "$MD19" && bad "N19: generated text must not carry an em-dash" || ok

# --- N20: the override file wins over the annotation, per version -----------
# A section for another version is inert; the stats line stays the commit
# log's; the record keeps the raw subjects and leads with the override hook.
fx=$(new_fixture) || exit 1
c "$fx" "feat(core): feature one"
c "$fx" "fix(core): a stale row"
"${GUSER[@]}" -C "$fx" tag -a v2.0.0 -m 'The annotation paragraph the override must replace - tuner vocabulary.'
{
	printf '%s\n' '## v1.0.0'
	printf '%s\n' 'hook: A stale section for an older release.'
	printf -- '- a stale bullet\n'
	printf '\n'
	printf '%s\n' '## v2.0.0'
	printf '%s\n' 'hook: The palette learned to edit the clock, in place.'
	printf -- '- the command palette now edits the top bar clock field\n'
	printf -- '- the agents tab works like sessions do\n'
	printf -- '- the files tree was rebuilt\n'
	printf -- '- folder references pin like files\n'
	printf -- '- prod deploy is one command\n'
	printf -- '- a sixth bullet that must not post\n'
} > "$fx/release-notes.overrides.md"
run_notes "$fx"
[ "$RC" -eq 0 ] && ok || bad "N20: exit $RC"
[ "$(sed -n '3p' "$fx/out/release-notes.md")" = "The palette learned to edit the clock, in place." ] \
	&& ok || bad "N20: the override hook must be the post hook, got [$(sed -n '3p' "$fx/out/release-notes.md")]"
expect_bullets "$fx" N20 \
	"the command palette now edits the top bar clock field" \
	"the agents tab works like sessions do" \
	"the files tree was rebuilt" \
	"folder references pin like files" \
	"prod deploy is one command"
printf '%s' "$OUT" | grep -qF "override lists 6 highlights - the post keeps the first 5 (five-highlight law); not posted: a sixth bullet that must not post" \
	&& ok || bad "N20: the sixth override bullet must be named on stderr"
md "$fx" | grep -qF "a sixth bullet" && bad "N20: the sixth override bullet must not post" || ok
md "$fx" | grep -qF "tuner vocabulary" && bad "N20: annotation prose must not post under an override" || ok
md "$fx" | grep -qF "stale bullet" && bad "N20: another version's bullets must not post" || ok
md "$fx" | grep -qF "stale section" && bad "N20: another version's hook must not post" || ok
printf '%s' "$OUT" | grep -qF "1 feature, 0 refactors, 1 fix, 0 tunes, 0 doc updates across core" \
	&& ok || bad "N20: the stats line stays the commit log's"
[ "$(sed -n '1p' "$fx/out/release-notes-body.md")" = "The palette learned to edit the clock, in place." ] \
	&& ok || bad "N20: the record should lead with the override hook"
body "$fx" | grep -qF "feat(core): feature one" && ok || bad "N20: the record keeps the raw subjects"

# --- N21: the style law - no em-dash, no en-dash in generated text ----------
fx=$(new_fixture) || exit 1
c "$fx" "feat(core): a feature $EM with an em-dash"
{
	printf '%s\n' '## v2.0.0'
	printf 'hook: The make-over %s the biggest refresh yet.\n' "$EM"
	printf -- '- the palette edits the clock %s in place\n' "$EN"
} > "$fx/release-notes.overrides.md"
"${GUSER[@]}" -C "$fx" tag -a v2.0.0 -m "An annotation $EM that must be normalized too."
run_notes "$fx"
[ "$RC" -eq 0 ] && ok || bad "N21: exit $RC"
[ "$(sed -n '3p' "$fx/out/release-notes.md")" = "The make-over - the biggest refresh yet." ] \
	&& ok || bad "N21: the hook must ride plain hyphens, got [$(sed -n '3p' "$fx/out/release-notes.md")]"
expect_bullets "$fx" N21 "the palette edits the clock - in place"
for f in "$fx/out/release-notes.md" "$fx/out/release-notes-body.md"; do
	grep -qF "$EM" "$f" && bad "N21: em-dash leaked into $(basename "$f")" || ok
	grep -qF "$EN" "$f" && bad "N21: en-dash leaked into $(basename "$f")" || ok
done
body "$fx" | grep -qF "a feature - with an em-dash" \
	&& ok || bad "N21: the record subject must ride plain hyphens too"

# --- N22: the shipped override renders the v0.17.3 worked examples ----------
# The acceptance shape for the two-audience template: a v0.17.3 range carrying
# the OLD annotation prose (factory vocabulary) renders the hand-written hook
# and the rewritten bullets instead, with the internal voice nowhere in the
# post and the stats line untouched.
fx=$(new_fixture) || exit 1
c "$fx" "feat(webui): the palette takes over the bar's time field in place"
c "$fx" "feat(agents): the agents tab takes the sessions interaction vocabulary"
c "$fx" "feat(factory): three disjoint cards and one review at a time"
"${GUSER[@]}" -C "$fx" tag -a v0.17.3 \
	-m "0.17.3 is the make-over: the palette takes over the bar's time field in place, the agents tab takes the sessions interaction vocabulary, and the factory runs three disjoint cards and one review at a time."
OVERRIDES_FILE="$SHIPPED_OVERRIDES" run_notes "$fx"
[ "$RC" -eq 0 ] && ok || bad "N22: exit $RC"
[ "$(sed -n '3p' "$fx/out/release-notes.md")" = "The make-over: the web ui's biggest visual and interaction refresh yet." ] \
	&& ok || bad "N22: the v0.17.3 hook is missing, got [$(sed -n '3p' "$fx/out/release-notes.md")]"
expect_bullets "$fx" N22 \
	"the command palette now edits the top bar's clock field right where it lives - click, type, done, no trip through settings" \
	"the agents tab now works like sessions do: agent cards with one-line previews, timestamps, and rebind-to-canonical-chat" \
	"under the hood, the build pipeline runs more work in parallel without collisions" \
	"folder references pin like file references, so a whole folder can ride along with a message" \
	"the files tree was rebuilt: hover guides show where a row sits and the tree scans faster"
[ "$(bullets "$fx")" -eq 5 ] && ok || bad "N22: the v0.17.3 post must carry 5 bullets, got $(bullets "$fx")"
md "$fx" | grep -qF "takes over the bar's time field in place" \
	&& bad "N22: the internal-voice annotation must not reach the post" || ok
md "$fx" | grep -qF "disjoint cards" && bad "N22: factory vocabulary reached the post" || ok
md "$fx" | grep -qF "interaction vocabulary" && bad "N22: factory vocabulary reached the post" || ok
printf '%s' "$OUT" | grep -qF "3 features, 0 refactors, 0 fixes, 0 tunes, 0 doc updates across factory, agents, webui" \
	&& ok || bad "N22: the stats line must stay the commit log's, got [$(printf '%s' "$OUT" | grep -m1 'features')]"
# the shipped file itself: one section per release, plain hyphens only
[ -f "$SHIPPED_OVERRIDES" ] && ok || bad "N22: scripts/release-notes.overrides.md must ship"
grep -qE '^## v0\.17\.3$' "$SHIPPED_OVERRIDES" && ok || bad "N22: the shipped file must carry a v0.17.3 section"
grep -qF "$EM" "$SHIPPED_OVERRIDES" && bad "N22: the shipped override carries an em-dash" || ok
grep -qF "$EN" "$SHIPPED_OVERRIDES" && bad "N22: the shipped override carries an en-dash" || ok

echo
echo "release-notes_test: $pass ok, $fail fail"
[ "$fail" -eq 0 ]
