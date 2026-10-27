#!/usr/bin/env bash
# Base-selection assertions for scripts/embed-lane.sh - no framework, no
# dependencies. Builds scratch repos, sources the lane script, and checks
# that select_base picks the pre-merge tip for every merge shape the
# merge train produces, plus one end-to-end gate run against a fake
# bundle build (EMBED_LANE_BUILD_CMD override - the real nuxt build is
# never touched here).
#
# Run: bash scripts/embed-lane-base-test.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EMBED_LANE="$here/embed-lane.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "ok  : $*"; }

# make_repo <dir>: origin (bare) + dev checkout, one Go-only commit
# pushed, so origin/dev exists and sits behind everything that follows.
make_repo() {
	local dir="$1"
	mkdir -p "$dir"
	git init -q --bare -b dev "$dir/origin.git"
	git init -q -b dev "$dir/dev"
	git -C "$dir/dev" config user.email lane@test
	git -C "$dir/dev" config user.name lane
	echo "package main" >"$dir/dev/main.go"
	git -C "$dir/dev" add .
	git -C "$dir/dev" commit -qm initial
	git -C "$dir/dev" remote add origin "$dir/origin.git"
	git -C "$dir/dev" push -q origin dev
}

# card_commit <repo> <path> <content> <msg>: one card-shaped commit.
card_commit() {
	local repo="$1" path="$2" content="$3" msg="$4"
	mkdir -p "$repo/$(dirname "$path")"
	printf '%s\n' "$content" >"$repo/$path"
	git -C "$repo" add .
	git -C "$repo" commit -qm "$msg"
}

# select_in <repo> [explicit rev]: run select_base inside $repo and echo
# "<BASE>|<BASE_TRUST>|<GATE_TRUSTED>". A failure (fail_no_base) echoes
# "FATAL|<stderr first line>|".
select_in() {
	local repo="$1" explicit="${2:-}" out
	out="$(
		cd "$repo" &&
			source "$EMBED_LANE" &&
			select_base "$explicit" >/dev/null 2>&1 &&
			printf '%s|%s|%s' "$BASE" "$BASE_TRUST" "$GATE_TRUSTED"
	)" || out="FATAL|select_base exited non-zero|"
	printf '%s\n' "$out"
}

assert_base() {
	local desc="$1" repo="$2" got="$3" want_base="$4" want_trust="$5" want_gate="$6"
	local base trust gate
	IFS='|' read -r base trust gate <<<"$got"
	[ "$base" = "$(git -C "$repo" rev-parse "$want_base^{commit}")" ] ||
		fail "$desc: base $base != $want_base ($(git -C "$repo" rev-parse "$want_base"))"
	[ "$trust" = "$want_trust" ] || fail "$desc: trust '$trust' != '$want_trust'"
	[ "$gate" = "$want_gate" ] || fail "$desc: gate $gate != $want_gate"
	pass "$desc"
}

# --- shape 1: multi-commit ff, origin/dev still at the pre-merge tip -----
d="$tmp/s1-multiff"
make_repo "$d"
git -C "$d/dev" checkout -qb card
card_commit "$d/dev" "engine/webui/App.vue" "v1" "card: ui one"
card_commit "$d/dev" "engine/webui/chat.ts" "v1" "card: ui two"
git -C "$d/dev" checkout -q dev
git -C "$d/dev" merge -q --ff-only card
assert_base "multi-commit ff -> pre-merge tip" "$d/dev" \
	"$(select_in "$d/dev")" \
	"origin/dev" \
	"origin/dev (at or behind the pre-merge tip; superset diff)" \
	"1"

# --- shape 2: multi-commit ff, pushed before the lane ran -----------------
d="$tmp/s2-multiff-push"
make_repo "$d"
git -C "$d/dev" checkout -qb card
card_commit "$d/dev" "engine/webui/App.vue" "v1" "card: ui one"
card_commit "$d/dev" "engine/webui/chat.ts" "v1" "card: ui two"
git -C "$d/dev" checkout -q dev
git -C "$d/dev" merge -q --ff-only card
git -C "$d/dev" push -q origin dev
got="$(select_in "$d/dev")"
IFS='|' read -r base trust gate <<<"$got"
[ "$base" = "$(git -C "$d/dev" rev-parse HEAD^1)" ] || fail "unproven shape: base $base != HEAD^1"
[ "$gate" = "0" ] || fail "unproven shape: GATE_TRUSTED should be 0, got '$got'"
pass "pushed multi-commit ff -> gate untrusted (bypass, no silent skip)"

# --- shape 3: real merge commit (HEAD^1 is the pre-landing tip) ----------
d="$tmp/s3-merge-commit"
make_repo "$d"
git -C "$d/dev" checkout -qb card
card_commit "$d/dev" "engine/webui/App.vue" "v1" "card: ui one"
git -C "$d/dev" checkout -q dev
git -C "$d/dev" merge -q --no-ff -m merge card
assert_base "real merge commit -> HEAD^1" "$d/dev" \
	"$(select_in "$d/dev")" \
	"HEAD^1" \
	"merge parent HEAD^1" \
	"1"

# --- shape 4: plain commit on dev (no merge at all), origin behind -------
d="$tmp/s4-plain-commit"
make_repo "$d"
card_commit "$d/dev" "main.go" "package main // go only" "go change"
assert_base "plain commit, origin behind -> origin/dev" "$d/dev" \
	"$(select_in "$d/dev")" \
	"origin/dev" \
	"origin/dev (at or behind the pre-merge tip; superset diff)" \
	"1"

# --- shape 5: root commit, no parent, no marker -> loud stop -------------
d="$tmp/s5-root"
git init -q -b dev "$d/dev"
git -C "$d/dev" config user.email lane@test
git -C "$d/dev" config user.name lane
echo "package main" >"$d/dev/main.go"
git -C "$d/dev" add .
git -C "$d/dev" commit -qm initial
got="$(select_in "$d/dev")"
[ "$got" = "FATAL|select_base exited non-zero|" ] || fail "root commit: expected loud failure, got '$got'"
pass "root commit -> loud stop (no silent skip)"

# --- shape 6: --base override stays explicit and trusted ------------------
d="$tmp/s6-override"
make_repo "$d"
git -C "$d/dev" checkout -qb card
card_commit "$d/dev" "engine/webui/App.vue" "v1" "card: ui one"
git -C "$d/dev" checkout -q dev
git -C "$d/dev" merge -q --ff-only card
assert_base "--base override -> explicit, trusted" "$d/dev" \
	"$(select_in "$d/dev" origin/dev)" \
	"origin/dev" \
	"explicit (--base)" \
	"1"

# --- e2e: multi-commit ff webui branch, no --base: the gate must SEE it --
d="$tmp/s7-e2e-webui"
make_repo "$d"
cat >"$d/fake-build.sh" <<'EOF'
mkdir -p engine/http_channel/static
printf 'bundle %s\n' "${FAKE_BUNDLE_VERSION:-v0}" > engine/http_channel/static/bundle.txt
EOF
chmod +x "$d/fake-build.sh"
git -C "$d/dev" checkout -qb card
card_commit "$d/dev" "engine/webui/VerdictCard.vue" "v1.1" "card: verdict v1.1"
card_commit "$d/dev" "engine/webui/chat.ts" "v1.1" "card: chat v1.1"
card_commit "$d/dev" "main.go" "package main // v1.1" "card: go v1.1"
git -C "$d/dev" checkout -q dev
git -C "$d/dev" merge -q --ff-only card
out="$(cd "$d/dev" && EMBED_LANE_BUILD_CMD="$d/fake-build.sh" bash "$EMBED_LANE" "e2e webui")"
echo "$out"
case "$out" in
*"regenerating the webui bundle"*) pass "e2e: multi-commit ff webui merge triggers regen without --base" ;;
*"change gate: no webui source"*) fail "e2e: THE BUG - gate skipped a webui diff" ;;
*) fail "e2e: neither regen nor gate-skip in output? '$out'" ;;
esac
case "$out" in
*"lane commit "*) : ;;
*) fail "e2e: expected a lane commit after regen" ;;
esac
grep -q "bundle v0" "$d/dev/engine/http_channel/static/bundle.txt" ||
	fail "e2e: static bundle not regenerated"
pass "e2e: bundle regenerated and lane-committed"

# --- e2e: same tree rerun hits the hash gate, clean exit ------------------
out2="$(cd "$d/dev" && EMBED_LANE_BUILD_CMD="$d/fake-build.sh" bash "$EMBED_LANE" "e2e rerun")"
echo "$out2"
case "$out2" in
*"bundle content unchanged"*) pass "e2e: rerun absorbed by the hash gate (no churn commit)" ;;
*) fail "e2e: rerun should skip via the hash gate, got '$out2'" ;;
esac

# --- e2e: Go-only merge skips, WITH the base rev printed (part 2) ---------
d="$tmp/s8-e2e-goonly"
make_repo "$d"
git -C "$d/dev" checkout -qb card
card_commit "$d/dev" "engine/agent/x.go" "package agent" "card: go one"
card_commit "$d/dev" "engine/agent/y.go" "package agent" "card: go two"
git -C "$d/dev" checkout -q dev
git -C "$d/dev" merge -q --ff-only card
premerge="$(git -C "$d/dev" rev-parse --short origin/dev)"
out="$(cd "$d/dev" && EMBED_LANE_BUILD_CMD="$d/fake-build.sh" bash "$EMBED_LANE" "e2e goonly" 2>&1)"
echo "$out"
case "$out" in
*"change gate: no webui source in $premerge..HEAD"*)
	pass "e2e: Go-only skip prints the exact base used ($premerge)" ;;
*) fail "e2e: gate skip must name the base $premerge, got '$out'" ;;
esac

# --- e2e: unproven multi-commit ff (pushed first, no --base) still regens -
d="$tmp/s9-e2e-unproven"
make_repo "$d"
cat >"$d/fake-build.sh" <<'EOF'
mkdir -p engine/http_channel/static
printf 'bundle %s\n' "${FAKE_BUNDLE_VERSION:-v0}" > engine/http_channel/static/bundle.txt
EOF
chmod +x "$d/fake-build.sh"
git -C "$d/dev" checkout -qb card
card_commit "$d/dev" "engine/webui/App.vue" "v2" "card: ui one"
card_commit "$d/dev" "engine/webui/chat.ts" "v2" "card: chat two"
git -C "$d/dev" checkout -q dev
git -C "$d/dev" merge -q --ff-only card
git -C "$d/dev" push -q origin dev
out="$(cd "$d/dev" && EMBED_LANE_BUILD_CMD="$d/fake-build.sh" bash "$EMBED_LANE" "e2e unproven" 2>&1)"
echo "$out"
case "$out" in
*"change gate: no webui source"*) fail "e2e: THE BUG under 'origin/dev == HEAD' - gate skipped" ;;
*"regenerating the webui bundle"*) pass "e2e: unproven base bypasses the gate and regens" ;;
*) fail "e2e: unexpected output '$out'" ;;
esac

echo "ALL PASS"