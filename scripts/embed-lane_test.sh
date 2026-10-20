#!/usr/bin/env bash
set -uo pipefail

# embed-lane_test.sh - scratch-repo tests for scripts/embed-lane.sh.
# Run: bash scripts/embed-lane_test.sh
# The real nuxt build is mocked (EMBED_LANE_BUILD_CMD); the gates, the
# lane commit mechanics and the two-branch landing shape run against
# real git fixtures.

LANE="$(cd "$(dirname "$0")" && pwd)/embed-lane.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() {
	fail=$((fail + 1))
	echo "FAIL: $1"
}
check() {
	local label="$1"
	shift
	if "$@"; then ok; else bad "$label"; fi
}
contains() {
	grep -q "$1" "$2"
}

STAMP="s0" # bundle stamp the mock writes (bump to force a new bundle)

new_repo() {
	# $TMP: fixture repo; $BASE: its initial tip; $MOCK: fake bundle build
	TMP="$(mktemp -d)"
	OUT="$TMP/run.err"
	git -C "$TMP" init -q
	git -C "$TMP" config user.email t@t
	git -C "$TMP" config user.name t
	mkdir -p "$TMP/modules/ui/app" "$TMP/modules/tools/ui" "$TMP/cmd"
	echo "source v1" >"$TMP/modules/ui/app/Foo.vue"
	echo "tool v1" >"$TMP/modules/tools/ui/Bar.vue"
	echo 'package main' >"$TMP/cmd/omega.go"
	mk_mock
	(cd "$TMP" && MOCK_STAMP=s0 "$MOCK")
	git -C "$TMP" add -A
	git -C "$TMP" commit -qm "initial: source + static stamp s0"
	BASE="$(git -C "$TMP" rev-parse HEAD)"
	MAIN_BRANCH="$(git -C "$TMP" symbolic-ref --short HEAD)"
	rm -f "$TMP.mock/canary-mock-ran" # the setup mock run leaves it behind
	STAMP="s0"
}

mk_mock() {
	# $TMP.mock/mock-build.sh: OUTSIDE the fixture repo root on purpose -
	# an in-repo canary gets swept into the root commit by git add -A and
	# then breaks branch checkouts. Writes the static bundle (stamp-based);
	# the canary file proves a build ran; MOCK_FAIL / MOCK_POISON inject
	# build failures and conflict-marker output.
	mkdir -p "$TMP.mock"
	MOCK="$TMP.mock/mock-build.sh"
	cat >"$MOCK" <<'EOF'
#!/usr/bin/env bash
set -e
touch "$(dirname "$0")/canary-mock-ran"
if [ "${MOCK_FAIL:-0}" = "1" ]; then
	echo "mock build failure" >&2
	exit 1
fi
d="$(pwd)/modules/http_channel/static"
rm -rf "$d"
mkdir -p "$d/_nuxt"
echo "bundle-stamp=${MOCK_STAMP:-s0}" >"$d/index.html"
poison=""
if [ "${MOCK_POISON:-0}" = "1" ]; then
	poison="<<<<<<< poison"
fi
echo "$poison" >"$d/_nuxt/chunk.js"
EOF
	chmod +x "$MOCK"
}

run() {
	# run <args, ...>; rc, $TMP/run.out and $OUT capture the result.
	# cd INSIDE the fixture: the lane resolves its repo from cwd.
	STAMP="${STAMP:-s0}"
	rc=0
	(
		cd "$TMP" &&
			EMBED_LANE_BUILD_CMD="$MOCK" MOCK_STAMP="$STAMP" bash "$LANE" "$@"
	) >"$TMP/run.out" 2>"$OUT" || rc=$?
}

########## repo 1: gates + lane commit mechanics ##########
new_repo

# change gate: a Go-only diff skips regeneration entirely
echo 'package main // v2' >"$TMP/cmd/omega.go"
git -C "$TMP" commit -qam "go-only change"
run --base "$BASE" "go-merge"
check "go-only merge: exit 0" test "$rc" -eq 0
check "go-only merge: no build ran" test ! -f "$TMP.mock/canary-mock-ran"
check "go-only merge: no lane commit" test "$(git -C "$TMP" rev-list --count HEAD)" -eq 2
check "go-only merge: change-gate skip logged" contains "change gate" "$TMP/run.out"

# modules/ui source change -> regen + lane commit
STAMP="s1"
echo "source v2" >"$TMP/modules/ui/app/Foo.vue"
git -C "$TMP" commit -qam "ui change: Foo v2"
run --base "$BASE" "card t_test1"
check "modules/ui change: exit 0" test "$rc" -eq 0
check "modules/ui change: build ran" test -f "$TMP.mock/canary-mock-ran"
git -C "$TMP" log -1 --pretty=%s >"$TMP/head.msg"
check "lane commit message has the context" contains "embed-lane: rebuild webui bundle for card t_test1" "$TMP/head.msg"
git -C "$TMP" diff-tree -r --name-only HEAD~1 HEAD >"$TMP/lane-diff.txt"
check "lane commit touches only static/" bash -c "grep -qv '^modules/http_channel/static/' '$TMP/lane-diff.txt'; test \$? -eq 1"
check "lane commit carries the new bundle" contains "bundle-stamp=s1" "$TMP/modules/http_channel/static/index.html"
lane_sha="$(git -C "$TMP" rev-parse HEAD)"

# hash gate: unchanged bundle -> no second lane commit
rm -f "$TMP.mock/canary-mock-ran"
run --base "$BASE" "card t_test1 again"
check "hash gate: exit 0 on skip" test "$rc" -eq 0
check "hash gate: build ran" test -f "$TMP.mock/canary-mock-ran"
check "hash gate: no extra commit" test "$(git -C "$TMP" rev-parse HEAD)" = "$lane_sha"
check "hash gate: skip logged" contains "hash gate" "$TMP/run.out"

# modules/*/ui (module-shipped UI) also counts as webui source
STAMP="s2"
echo "tool v2" >"$TMP/modules/tools/ui/Bar.vue"
git -C "$TMP" commit -qam "ui change: tools panel v2"
run --base "$lane_sha" "card t_test2"
check "modules/*/ui change: lane commit created" contains "embed-lane: rebuild webui bundle for card t_test2" <(git -C "$TMP" log -1 --pretty=%s)

# fail-loud: broken build aborts, static/ untouched, no commit
STAMP="s3"
echo "source v3" >"$TMP/modules/ui/app/Foo.vue"
git -C "$TMP" commit -qam "ui change: Foo v3"
before_static="$(git -C "$TMP" rev-parse HEAD:modules/http_channel/static)"
MOCK_FAIL=1 run --base "$(git -C "$TMP" rev-parse HEAD~1)" "card t_test3"
check "broken build: nonzero exit" test "$rc" -ne 0
check "broken build: static/ untouched" test "$(git -C "$TMP" rev-parse HEAD:modules/http_channel/static)" = "$before_static"
check "broken build: aborted loudly" contains "FAILED" "$OUT"
check "broken build: never hand-resolve law cited" contains "NEVER hand-resolved" "$OUT"

# belt-and-suspenders: conflict markers in regenerated output rejected
MOCK_POISON=1 run --base "$(git -C "$TMP" rev-parse HEAD~1)" "card t_test3"
check "poisoned bundle: rejected by lint gate" test "$rc" -ne 0
check "poisoned bundle: no lane commit" test "$(git -C "$TMP" rev-parse HEAD:modules/http_channel/static)" = "$before_static"

########## repo 2: base detection ##########
new_repo
# root commit without --base: nothing to infer the change gate from
run "ctx-only"
check "no --base on a root commit: nonzero exit" test "$rc" -ne 0
check "no --base on a root commit: error logged" contains "no parent commit" "$OUT"

# linear (fast-forward-landed) HEAD: default base = HEAD^1 works
STAMP="s1"
echo "source v2" >"$TMP/modules/ui/app/Foo.vue"
git -C "$TMP" commit -qam "ui commit: Foo v2 (linear landing)"
run
check "linear HEAD: default base = HEAD^1 -> rc 0" test "$rc" -eq 0
check "linear HEAD: change gate saw the ui commit (lane commit created)" contains "embed-lane: rebuild webui bundle for merge" <(git -C "$TMP" log -1 --pretty=%s)

# true merge commit: default base = first parent (the pre-merge tip)
git -C "$TMP" checkout -q -b webui-a "$BASE"
STAMP="s1"
echo "panel v3" >"$TMP/modules/tools/ui/Bar.vue"
git -C "$TMP" commit -qam "branch A: webui source v3"
git -C "$TMP" checkout -q "$MAIN_BRANCH"
git -C "$TMP" merge -q --no-ff --no-edit webui-a >/dev/null
STAMP="s2"
run "branch A"
check "merge HEAD: exit 0" test "$rc" -eq 0
check "merge HEAD: default base = first parent (lane commit created)" contains "embed-lane: rebuild webui bundle for branch A" <(git -C "$TMP" log -1 --pretty=%s)

########## repo 3: the two-branch parallel proof shape ##########
new_repo
STAMP="pa"
git -C "$TMP" checkout -q -b wui-a "$BASE"
echo "aa: source" >"$TMP/modules/ui/app/Foo.vue"
git -C "$TMP" commit -qam "branch A: component 1"
git -C "$TMP" checkout -q "$MAIN_BRANCH"
git -C "$TMP" checkout -q -b wui-b "$BASE"
echo "bb: source" >"$TMP/modules/tools/ui/Bar.vue"
git -C "$TMP" commit -qam "branch B: component 2"
git -C "$TMP" checkout -q "$MAIN_BRANCH"
git -C "$TMP" merge -q --no-edit wui-a >/dev/null
run "branch A"
check "parallel A: lane commit ok" test "$rc" -eq 0
check "parallel A: bundle carries A's regen" contains "bundle-stamp=pa" "$TMP/modules/http_channel/static/index.html"
STAMP="pb"
rc_merge=0
git -C "$TMP" merge --no-edit wui-b >/dev/null 2>"$TMP/merge-b.err" || rc_merge=$?
check "parallel B merges with zero static/ conflicts" test "$rc_merge" -eq 0
run "branch B"
check "parallel B: lane commit ok" test "$rc" -eq 0
check "parallel B: bundle carries B's regen" contains "bundle-stamp=pb" "$TMP/modules/http_channel/static/index.html"

########## summary ##########
echo "embed-lane_test: pass=$pass fail=$fail"
[ "$fail" -eq 0 ]