#!/usr/bin/env bash
set -uo pipefail

# release-notes_test.sh - scratch-repo tests for scripts/release-notes.sh.
# Run: bash scripts/release-notes_test.sh
# Synthetic commit ranges against real git fixture repos: counts + scopes
# line, the per-class bullet pick (feat -> refactor -> fix -> tune -> docs,
# newest first, the tail classes capped at 5), noise exclusion, empty-range
# and no-annotation fallbacks, the annotated-tag lead sentence, and the JSON
# payload shape.
# Merge commits never carry a bullet - they only count; their side commits
# ride the range and classify on their own subjects.

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
bullets() { grep -c '^- ' "$1/out/release-notes.md" || true; }

# expect_bullets <fixture> <label> <subject...>: the exact bullet block, in
# order - the pick is a contract, not a count
expect_bullets() {
	local fx=$1 label=$2 got want s
	shift 2
	got="$(grep '^- ' "$fx/out/release-notes.md" || true)"
	want=""
	for s in "$@"; do want="$want- $s"$'\n'; done
	want="${want%$'\n'}"
	[ "$got" = "$want" ] && ok \
		|| bad "$label: bullets wrong - got [$(printf '%s' "$got" | tr '\n' '|')] want [$(printf '%s' "$want" | tr '\n' '|')]"
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
	&& ok || bad "N1: counts line wrong: $(sed -n '3p' <<<"$OUT")"
expect_bullets "$fx" N1 \
	"feat(main): the landing lane" \
	"fix(side): a race in the send path" \
	"tune(core): default context window 32768" \
	"docs: note the soak law"
md "$fx" | grep -qF "Merge branch" && bad "N1: bare merge must not highlight" || ok
md "$fx" | grep -qF "chore" && bad "N1: chore leaked into bullets" || ok

# --- N2: every feat bullet, no scope dedupe, merges never carry a bullet ----
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
	&& ok || bad "N2: counts line wrong: $(sed -n '3p' <<<"$OUT")"
expect_bullets "$fx" N2 \
	"feat(webui): reference pills for tokens" \
	"feat(webui): command palette" \
	"feat(sessions): the hygiene pass" \
	"feat(core): the context window grows" \
	"fix(core): the oldest fix"
md "$fx" | grep -qF "the tree tells the product story" && bad "N2: card merge highlighted" || ok
md "$fx" | grep -qF "async cron overhaul" && bad "N2: card merge highlighted" || ok
[ "$(md "$fx" | grep -cF 'feat(webui)')" -eq 2 ] && ok || bad "N2: same-scope feats were deduped"
md "$fx" | grep -qF "chore" && bad "N2: chore leaked into bullets" || ok
md "$fx" | grep -qF "embed-lane" && bad "N2: embed-lane leaked into bullets" || ok

# --- N3: merge stories never become bullets ---------------------------------
fx=$(new_fixture)
c "$fx" "feat(side): a real thing"
merge_commit "$fx" "merge: restructure part 3 - the seam constitution" "chore(mg): side scratch"
merge_commit "$fx" "merge: restructure" "chore(mg): side scratch"
run_notes "$fx"
expect_bullets "$fx" N3 "feat(side): a real thing"

# --- N4: fixes newest-first, same scope kept, order after refactors ----------
fx=$(new_fixture)
c "$fx" "fix(a): first"
c "$fx" "fix(a): second in the same scope"
c "$fx" "fix(b): third"
c "$fx" "fix(c): fourth"
c "$fx" "refactor(b): the reshape"
c "$fx" "feat(a): the one feat"
run_notes "$fx"
expect_bullets "$fx" N4 \
	"feat(a): the one feat" \
	"refactor(b): the reshape" \
	"fix(c): fourth" \
	"fix(b): third" \
	"fix(a): second in the same scope" \
	"fix(a): first"

# --- N5: noise-only range -> zeros, no bullets, exit 0 -----------------------
fx=$(new_fixture)
c "$fx" "chore: one"
c "$fx" "style: two"
c "$fx" "wip: three"
run_notes "$fx"
printf '%s' "$OUT" | grep -qF "0 features, 0 refactors, 0 fixes, 0 tunes, 0 doc updates" \
	&& ok || bad "N5: noise-only counts wrong: $(sed -n '3p' <<<"$OUT")"
[ "$(bullets "$fx")" -eq 0 ] && ok || bad "N5: noise must not highlight"
[ "$RC" -eq 0 ] && ok || bad "N5: exit $RC"

# --- N6: empty range -> graceful (base passed explicitly) --------------------
fx=$(new_fixture)
run_notes "$fx" v1.0.0
[ "$RC" -eq 0 ] && ok || bad "N6: empty range exit $RC"
printf '%s' "$OUT" | grep -qF "0 features, 0 refactors, 0 fixes, 0 tunes" && ok || bad "N6: empty counts missing"

# --- N7: annotated tag -> the lead sentence, version + URL from the tag ------
fx=$(new_fixture)
c "$fx" "feat(side): delegates got safer"
"${GUSER[@]}" -C "$fx" tag -a v2.0.0 -m 'Delegates got safer and the tree got flatter'
run_notes "$fx"
printf '%s' "$OUT" | LC_ALL=C grep -qF "🚀 **omega v2.0.0** is out!" && ok || bad "N7: rocket line wrong"
printf '%s' "$OUT" | grep -qF "Delegates got safer and the tree got flatter" \
	&& ok || bad "N7: tag-annotation lead missing"
# base excludes the version tag itself even though it points at HEAD
printf '%s' "$OUT" | grep -qF "1 feature, 0 refactors, 0 fixes, 0 tunes, 0 doc updates across side" \
	&& ok || bad "N7: base pick ignored the version tag: $(sed -n '5p' <<<"$OUT")"
printf '%s' "$OUT" | grep -qF "releases/tag/v2.0.0" && ok || bad "N7: release URL uses v2.0.0"
# md: rocket(1), blank(2), lead(3); body: lead(1), blank(2), counts(3)
sed -n '2p' "$fx/out/release-notes.md" | grep -q . \
	&& bad "N7: line 2 must stay blank (lead sits at line 3)" || ok
[ "$(sed -n '3p' "$fx/out/release-notes.md")" = "Delegates got safer and the tree got flatter" ] \
	&& ok || bad "N7: lead sentence should be the first content line after the rocket"
BODY="$fx/out/release-notes-body.md"
LC_ALL=C grep -qF "🚀" "$BODY" && bad "N7: body file must not carry the rocket line" || ok
[ "$(sed -n '1p' "$BODY")" = "Delegates got safer and the tree got flatter" ] \
	&& ok || bad "N7: body file lead"
grep -qF -- "- feat(side): delegates got safer" "$BODY" && ok || bad "N7: body file highlight"

# --- N8: lightweight tag -> no lead sentence anywhere (fallback) -------------
fx=$(new_fixture)
c "$fx" "feat(side): a feature"
"${GUSER[@]}" -C "$fx" tag v2.0.0
run_notes "$fx"
[ "$RC" -eq 0 ] && ok || bad "N8: lightweight-tag run exit $RC"
[ "$(sed -n '2p' "$fx/out/release-notes.md")" = "" ] && ok || bad "N8: no lead expected on line 2"
sed -n '3p' "$fx/out/release-notes.md" | grep -qF "1 feature" && ok || bad "N8: counts expected on line 3"

# --- N9: annotated tag quoting survives the JSON escape ----------------------
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
assert p["content"] == md, "payload content must equal the markdown"
assert len(p["content"]) <= 2000
PY

# --- N10: the tail classes cap at 5, feats and refactors stay complete ------
fx=$(new_fixture)
for i in 1 2 3 4 5 6 7; do c "$fx" "fix(core): fix $i"; done
for i in 1 2 3 4 5 6 7; do c "$fx" "docs: doc $i"; done
c "$fx" "refactor(core): reshape one"
c "$fx" "feat(core): feature one"
c "$fx" "feat(core): feature two"
run_notes "$fx"
expect_bullets "$fx" N10 \
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

echo
echo "release-notes_test: $pass ok, $fail fail"
[ "$fail" -eq 0 ]
