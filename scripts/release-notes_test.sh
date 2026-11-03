#!/usr/bin/env bash
set -uo pipefail

# release-notes_test.sh - scratch-repo tests for scripts/release-notes.sh.
# Run: bash scripts/release-notes_test.sh
# Synthetic commit ranges against real git fixture repos: counts + scopes
# line, the mechanical highlight pick (feats -> card merges -> fixes), topic
# dedupe, noise exclusion, empty-range and no-annotation fallbacks, the
# annotated-tag lead sentence, and the JSON payload shape.
# Merge candidates need REAL two-parent commits - the pick classifies on
# parents, not on the subject alone.

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

# --- N1: counts line + scopes, merges counted, bare merges uninformative ----
fx=$(new_fixture)
c "$fx" "docs: note the soak law"
c "$fx" "tune(core): default context window 32768"
c "$fx" "fix(side): a race in the send path"
merge_commit "$fx" "Merge branch 'wt/x' into dev" "chore(x): side scaffolding"
c "$fx" "feat(main): the landing lane"
run_notes "$fx"
printf '%s' "$OUT" | grep -qF "1 feature, 1 fix, 1 tune, 2 other, 1 merge across main, x, side, core" \
	&& ok || bad "N1: counts line wrong: $(sed -n '3p' <<<"$OUT")"
[ "$(bullets "$fx")" -eq 2 ] && ok || bad "N1: expected 2 highlights (1 feat + 1 fix fill), got $(bullets "$fx")"
md "$fx" | grep -qF -- "- feat(main): the landing lane" && ok || bad "N1: feat highlight missing"
md "$fx" | grep -qF -- "- fix(side): a race in the send path" && ok || bad "N1: fix fill missing"
md "$fx" | grep -qF "Merge branch" && bad "N1: bare merge must not highlight" || ok
md "$fx" | grep -qF "docs:" && bad "N1: docs commit highlighted" || ok

# --- N2: feats cap 3 + scope dedupe, merges cap 2, fixes squeezed out -------
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
# counts: 4 feats, 1 fix, 0 tunes, 2 other (chore + embed-lane), 2 merges
printf '%s' "$OUT" | grep -qF "4 features, 1 fix, 0 tunes, 2 other, 2 merges across webui, sessions, core" \
	&& ok || bad "N2: counts line wrong: $(sed -n '3p' <<<"$OUT")"
[ "$(bullets "$fx")" -eq 5 ] && ok || bad "N2: expected 5 highlights, got $(bullets "$fx")"
md "$fx" | grep -qF -- "- the tree tells the product story (three-tier restructure)" \
	&& ok || bad "N2: card-merge highlight missing or card id not stripped"
md "$fx" | grep -qF -- "- async cron overhaul - per-job agent and model" \
	&& ok || bad "N2: second card-merge missing"
[ "$(md "$fx" | grep -cF 'feat(webui)')" -eq 1 ] && ok || bad "N2: webui scope not deduped to one pick"
md "$fx" | grep -qF -- "- fix(" && bad "N2: fix must be squeezed out at 5/5" || ok
md "$fx" | grep -qF "chore" && bad "N2: chore leaked into highlights" || ok
md "$fx" | grep -qF "embed-lane" && bad "N2: embed-lane leaked into highlights" || ok

# --- N3: substring-dedupe on merge stories ----------------------------------
fx=$(new_fixture)
c "$fx" "feat(side): a real thing"
merge_commit "$fx" "merge: restructure part 3 - the seam constitution" "chore(mg): side scratch"
merge_commit "$fx" "merge: restructure" "chore(mg): side scratch"
run_notes "$fx"
[ "$(md "$fx" | grep -c 'restructure')" -eq 1 ] \
	&& ok || bad "N3: merge stories not deduped by substring"
md "$fx" | grep -qF -- "- restructure" && ok || bad "N3: newest merge should win the dedupe"

# --- N4: fixes fill the remaining slots, one per scope ------------------------
fx=$(new_fixture)
c "$fx" "fix(a): first"
c "$fx" "fix(a): second in the same scope"
c "$fx" "fix(b): third"
c "$fx" "fix(c): fourth"
c "$fx" "feat(a): the one feat"
run_notes "$fx"
[ "$(bullets "$fx")" -eq 4 ] && ok || bad "N4: expected 4 highlights (1 feat + 3 unique fixes), got $(bullets "$fx")"
md "$fx" | grep -qF "second in the same scope" && ok || bad "N4: newest same-scope fix should win the dedupe"
md "$fx" | grep -qF "fix(a): first" && bad "N4: same-scope fix not deduped" || ok
[ "$(md "$fx" | grep -cF -- '- fix(')" -eq 3 ] && ok || bad "N4: expected 3 fix highlights"

# --- N5: noise-only range -> zeros, no bullets, exit 0 -----------------------
fx=$(new_fixture)
c "$fx" "chore: one"
c "$fx" "style: two"
c "$fx" "wip: three"
run_notes "$fx"
printf '%s' "$OUT" | grep -qF "0 features, 0 fixes, 0 tunes, 3 other" \
	&& ok || bad "N5: noise-only counts wrong: $(sed -n '3p' <<<"$OUT")"
[ "$(bullets "$fx")" -eq 0 ] && ok || bad "N5: noise must not highlight"
[ "$RC" -eq 0 ] && ok || bad "N5: exit $RC"

# --- N6: empty range -> graceful (base passed explicitly) --------------------
fx=$(new_fixture)
run_notes "$fx" v1.0.0
[ "$RC" -eq 0 ] && ok || bad "N6: empty range exit $RC"
printf '%s' "$OUT" | grep -qF "0 features, 0 fixes, 0 tunes" && ok || bad "N6: empty counts missing"

# --- N7: annotated tag -> the lead sentence, version + URL from the tag ------
fx=$(new_fixture)
c "$fx" "feat(side): delegates got safer"
"${GUSER[@]}" -C "$fx" tag -a v2.0.0 -m 'Delegates got safer and the tree got flatter'
run_notes "$fx"
printf '%s' "$OUT" | grep -qF "🚀 **omega v2.0.0** is out!" && ok || bad "N7: rocket line wrong"
printf '%s' "$OUT" | grep -qF "Delegates got safer and the tree got flatter" \
	&& ok || bad "N7: tag-annotation lead missing"
# base excludes the version tag itself even though it points at HEAD
printf '%s' "$OUT" | grep -qF "1 feature, 0 fixes, 0 tunes across side" \
	&& ok || bad "N7: base pick ignored the version tag: $(sed -n '3p' <<<"$OUT")"
printf '%s' "$OUT" | grep -qF "releases/tag/v2.0.0" && ok || bad "N7: release URL uses v2.0.0"
# md: rocket(1), blank(2), lead(3); body: lead(1), blank(2), counts(3)
sed -n '2p' "$fx/out/release-notes.md" | grep -q . \
	&& bad "N7: line 2 must stay blank (lead sits at line 3)" || ok
[ "$(sed -n '3p' "$fx/out/release-notes.md")" = "Delegates got safer and the tree got flatter" ] \
	&& ok || bad "N7: lead sentence should be the first content line after the rocket"
BODY="$fx/out/release-notes-body.md"
grep -qF "🚀" "$BODY" && bad "N7: body file must not carry the rocket line" || ok
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

echo
echo "release-notes_test: $pass ok, $fail fail"
[ "$fail" -eq 0 ]
