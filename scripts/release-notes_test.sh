#!/usr/bin/env bash
set -uo pipefail

# release-notes_test.sh - scratch-repo tests for scripts/release-notes.sh.
# Run: bash scripts/release-notes_test.sh
# Synthetic commit ranges against real git fixture repos: the counts + scopes
# line, the GitHub body's per-class changelog (feat -> refactor -> fix ->
# tune -> docs, newest first inside each, the tail classes capped at 5),
# noise exclusion, empty-range and no-annotation fallbacks, and the POST law
# (t_878b1b7c): FIVE highlights (newest-5 feat default, the tag annotation's
# list wins), ONE prose paragraph read off the annotation body, the counts
# line, the update + changelog link, and the loud 2000-char Discord cap with
# no trimmer behind it.
# The annotation has to SURVIVE the CI checkout (t_530c4635): a tag ref
# rewritten to its commit by actions/checkout@v4 reads as no annotation, so
# the absence is named on stderr with its remedy and the release workflow's
# restore step is pinned by shape and order (N17, N18).
# Merge commits never carry a bullet - they only count; their side commits
# ride the range and classify on their own subjects.
#
# The suite needs a NATIVE TMPDIR on Windows (mktemp under bare /tmp hands
# the SUT's python a path it cannot open - the fixtures then look empty).

SUT="$(cd "$(dirname "$0")" && pwd)/release-notes.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1"; }

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

new_fixture() {
	local root
	root="$(mktemp -d "${TMPDIR:-/tmp}/rnotes-fx.XXXXXX")"
	git init -q -b master "$root"
	c "$root" seed
	git -C "$root" tag v1.0.0
	echo "$root"
}

# run_notes <fixture> [args...]: stdout+stderr -> OUT; exit -> RC;
# generated files land in <fixture>/out
run_notes() {
	local root=$1
	shift
	OUT="$(cd "$root" && NOTES_OUT_DIR="$root/out" bash "$SUT" "$@" 2>&1)"
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
fx=$(new_fixture)
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
# the post carries the five-highlight law: only the newest feats
expect_bullets "$fx" N1 "feat(main): the landing lane"
md "$fx" | grep -qF "Merge branch" && bad "N1: bare merge must not highlight" || ok
body "$fx" | grep -qF "Merge branch" && bad "N1: bare merge must not reach the record" || ok
md "$fx" | grep -qF "chore" && bad "N1: chore leaked into the post" || ok
body "$fx" | grep -qF "chore" && bad "N1: chore leaked into the record" || ok

# --- N2: every feat in the record, no scope dedupe, merges carry no bullet --
fx=$(new_fixture)
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
# the default highlight pick: newest 5 feats, and only feats exist here (4)
expect_bullets "$fx" N2 \
	"feat(webui): reference pills for tokens" \
	"feat(webui): command palette" \
	"feat(sessions): the hygiene pass" \
	"feat(core): the context window grows"
body "$fx" | grep -qF "the tree tells the product story" && bad "N2: card merge reached the record" || ok
[ "$(body "$fx" | grep -cF 'feat(webui)')" -eq 2 ] && ok || bad "N2: same-scope feats were deduped"
body "$fx" | grep -qF "embed-lane" && bad "N2: embed-lane leaked into the record" || ok

# --- N3: merge stories never become bullets ---------------------------------
fx=$(new_fixture)
c "$fx" "feat(side): a real thing"
merge_commit "$fx" "merge: restructure part 3 - the seam constitution" "chore(mg): side scratch"
merge_commit "$fx" "merge: restructure" "chore(mg): side scratch"
run_notes "$fx"
expect_body_bullets "$fx" N3 "feat(side): a real thing"
expect_bullets "$fx" N3 "feat(side): a real thing"

# --- N4: fixes newest-first in the record, order after refactors ------------
fx=$(new_fixture)
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
expect_bullets "$fx" N4 "feat(a): the one feat"

# --- N5: noise-only range -> zeros, no bullets, exit 0 ----------------------
fx=$(new_fixture)
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
fx=$(new_fixture)
run_notes "$fx" v1.0.0
[ "$RC" -eq 0 ] && ok || bad "N6: empty range exit $RC"
printf '%s' "$OUT" | grep -qF "0 features, 0 refactors, 0 fixes, 0 tunes" && ok || bad "N6: empty counts missing"

# --- N7: annotated tag, subject only -> it IS the prose paragraph ----------
fx=$(new_fixture)
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

# --- N8: lightweight tag -> no prose anywhere (fallback) --------------------
fx=$(new_fixture)
c "$fx" "feat(side): a feature"
"${GUSER[@]}" -C "$fx" tag v2.0.0
run_notes "$fx"
[ "$RC" -eq 0 ] && ok || bad "N8: lightweight-tag run exit $RC"
[ "$(sed -n '2p' "$fx/out/release-notes.md")" = "" ] && ok || bad "N8: no prose expected on line 2"
[ "$(sed -n '3p' "$fx/out/release-notes.md")" = "- feat(side): a feature" ] \
	&& ok || bad "N8: post must fall through to the highlight bullets"
printf '%s' "$OUT" | grep -qF "1 feature" && ok || bad "N8: counts expected"
printf '%s' "$OUT" | grep -qF "full changelog:" && ok || bad "N8: link expected"

# --- N9: annotated tag quoting survives the JSON escape ---------------------
fx=$(new_fixture)
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
fx=$(new_fixture)
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
# the post still carries five at most
expect_bullets "$fx" N10 \
	"feat(core): feature two" \
	"feat(core): feature one"

# --- N11: the post law - five highlights, counts, link, under 900 chars -----
fx=$(new_fixture)
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
	"feat(core): feature 7" \
	"feat(core): feature 6" \
	"feat(core): feature 5" \
	"feat(core): feature 4" \
	"feat(core): feature 3"
printf '%s' "$OUT" | grep -qF "7 features, 2 refactors, 6 fixes, 1 tune, 6 doc updates" \
	&& ok || bad "N11: counts line missing from the post"
printf '%s' "$OUT" | grep -qF "Update: \`omega update\` · full changelog: <https://github.com/EndoTheDev/omega/releases/tag/v2.0.0>" \
	&& ok || bad "N11: update + changelog line missing"
[ "$(sed -n '2p' "$fx/out/release-notes.md")" = "" ] && ok || bad "N11: no prose paragraph expected (no annotation)"
[ "$(wc -c < "$fx/out/release-notes.md")" -lt 900 ] && ok || bad "N11: post must stay under 900 chars ($(wc -c < "$fx/out/release-notes.md"))"
# the record is untouched: 7 feat + 2 refactor + 5 fix + 1 tune + 5 docs
[ "$(body_bullets "$fx")" -eq 20 ] && ok || bad "N11: the record must keep 20 pick bullets, got $(body_bullets "$fx")"

# --- N12: an annotation highlight list OVERRIDES the default five ----------
fx=$(new_fixture)
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
fx=$(new_fixture)
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
fx=$(new_fixture)
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
fx=$(new_fixture)
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
fx=$(new_fixture)
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
fx=$(new_fixture)
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

echo
echo "release-notes_test: $pass ok, $fail fail"
[ "$fail" -eq 0 ]
