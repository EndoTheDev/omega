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
not_contains() {
	! grep -q "$1" "$2"
}

STAMP="s0" # bundle stamp the mock writes (bump to force a new bundle)

new_repo() {
	# $TMP: fixture repo; $BASE: its initial tip; $MOCK: fake bundle build
	TMP="$(mktemp -d)"
	OUT="$TMP/run.err"
	git -C "$TMP" init -q
	git -C "$TMP" config user.email t@t
	git -C "$TMP" config user.name t
	mkdir -p "$TMP/webui/app" "$TMP/harness/tools/ui" "$TMP/cmd" "$TMP/api/http_channel/ui"
	echo "source v1" >"$TMP/webui/app/Foo.vue"
	echo "tool v1" >"$TMP/harness/tools/ui/Bar.vue"
	# the serving shell's own module UI (the pane that reaches users only
	# through the bundle: one .vue panel + its helper siblings)
	echo "pane v1" >"$TMP/api/http_channel/ui/LogsList.vue"
	echo "// stream v1" >"$TMP/api/http_channel/ui/logstream.ts"
	echo "// severity v1" >"$TMP/api/http_channel/ui/severity.ts"
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
d="$(pwd)/api/static"
rm -rf "$d"
bid="${MOCK_BUILDID:-11111111-1111-1111-1111-111111111111}"
mkdir -p "$d/_nuxt/builds/meta"
echo "bundle-stamp=${MOCK_STAMP:-s0} buildId=$bid" >"$d/index.html"
poison=""
if [ "${MOCK_POISON:-0}" = "1" ]; then
	poison="<<<<<<< poison"
fi
echo "$poison" >"$d/_nuxt/chunk.js"
ts=$((1792531190000 + RANDOM))
echo "{\"id\":\"$bid\",\"timestamp\":$ts,\"prerendered\":[]}" >"$d/_nuxt/builds/meta/$bid.json"
echo "{\"id\":\"$bid\",\"timestamp\":$(date +%s%3N)}" >"$d/_nuxt/builds/latest.json"
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

# webui source change -> regen + lane commit
STAMP="s1"
echo "source v2" >"$TMP/webui/app/Foo.vue"
git -C "$TMP" commit -qam "ui change: Foo v2"
run --base "$BASE" "card t_test1"
check "webui change: exit 0" test "$rc" -eq 0
check "webui change: build ran" test -f "$TMP.mock/canary-mock-ran"
git -C "$TMP" log -1 --pretty=%s >"$TMP/head.msg"
check "lane commit message has the context" contains "embed-lane: rebuild webui bundle for card t_test1" "$TMP/head.msg"
git -C "$TMP" diff-tree -r --name-only HEAD~1 HEAD >"$TMP/lane-diff.txt"
check "lane commit touches only static/" bash -c "grep -qv '^api/static/' '$TMP/lane-diff.txt'; test \$? -eq 1"
check "lane commit carries the new bundle" contains "bundle-stamp=s1" "$TMP/api/static/index.html"
lane_sha="$(git -C "$TMP" rev-parse HEAD)"

# hash gate: unchanged bundle -> no second lane commit
rm -f "$TMP.mock/canary-mock-ran"
run --base "$BASE" "card t_test1 again"
check "hash gate: exit 0 on skip" test "$rc" -eq 0
check "hash gate: build ran" test -f "$TMP.mock/canary-mock-ran"
check "hash gate: no extra commit" test "$(git -C "$TMP" rev-parse HEAD)" = "$lane_sha"
check "hash gate: skip logged" contains "hash gate" "$TMP/run.out"

# nuxt-volatile-only regen (new buildId uuid + fresh timestamps, same
# content) must also skip - metadata churn is not a bundle change
MOCK_BUILDID=22222222-2222-2222-2222-222222222222 run --base "$BASE" "card t_test1 volatile"
check "volatile-only regen: exit 0 on skip" test "$rc" -eq 0
check "volatile-only regen: no extra commit" test "$(git -C "$TMP" rev-parse HEAD)" = "$lane_sha"
check "volatile-only regen: skip logged" contains "hash gate" "$TMP/run.out"

# harness/*/ui (module-shipped UI) also counts as webui source
STAMP="s2"
echo "tool v2" >"$TMP/harness/tools/ui/Bar.vue"
git -C "$TMP" commit -qam "ui change: tools panel v2"
run --base "$lane_sha" "card t_test2"
check "harness/*/ui change: lane commit created" contains "embed-lane: rebuild webui bundle for card t_test2" <(git -C "$TMP" log -1 --pretty=%s)

# --- the serving shell's module UI (api/<name>/ui/) is bundled too -------
# Regression pin (card t_4bcea3dd): a diff touching ONLY
# api/http_channel/ui/** used to read as a Go-only merge, so the lane
# printed "no webui source ... skip" and the pane fix shipped to nobody.
# Every shape below commits nothing but module-UI paths.
module_ui_regen() {
	# module_ui_regen <stamp> <context> <label> - asserts the gate is
	# NON-empty for the last commit and the lane committed the regen.
	local stamp="$1" ctx="$2" label="$3" base_now gate_out
	base_now="$(git -C "$TMP" rev-parse HEAD~1)"
	gate_out="$(cd "$TMP" && source "$LANE" && ui_changed "$base_now" HEAD)"
	check "$label: gate output non-empty" test -n "$gate_out"
	STAMP="$stamp"
	rm -f "$TMP.mock/canary-mock-ran"
	run --base "$base_now" "$ctx"
	check "$label: exit 0" test "$rc" -eq 0
	check "$label: regen ran (build reached)" test -f "$TMP.mock/canary-mock-ran"
	check "$label: lane commit created" contains "embed-lane: rebuild webui bundle for $ctx" <(git -C "$TMP" log -1 --pretty=%s)
	check "$label: no Go-only skip logged" not_contains "change gate: no webui source" "$TMP/run.out"
}

# (a) a pane edit
echo "pane v2" >"$TMP/api/http_channel/ui/LogsList.vue"
git -C "$TMP" commit -qam "ui change: logs pane v2 (module UI only)"
module_ui_regen s2a "card t_test2a" "api/*/ui .vue pane edit"

# (b) a helper .ts sibling only - no .vue, no webui/ path at all (the
#     exact t_217c24e8 shape: logstream.ts + LogsList.vue reached users
#     only because the branch also happened to touch webui/test/)
echo "// stream v2" >>"$TMP/api/http_channel/ui/logstream.ts"
git -C "$TMP" commit -qam "ui change: logs helper only (module UI .ts)"
module_ui_regen s2b "card t_test2b" "api/*/ui .ts helper edit"

# (c) a deletion inside the module UI dir
git -C "$TMP" rm -q api/http_channel/ui/severity.ts
git -C "$TMP" commit -qm "ui change: drop the severity helper (deletion)"
module_ui_regen s2c "card t_test2c" "api/*/ui deletion"

# (d) a rename OUT of the module UI dir: the source side must still count
mkdir -p "$TMP/docs"
git -C "$TMP" mv api/http_channel/ui/logstream.ts docs/logstream.ts
git -C "$TMP" commit -qm "ui change: move the stream helper out of the pane dir"
module_ui_regen s2d "card t_test2d" "api/*/ui rename-out"

# (e) the widening stays NARROW: a Go change inside the same shell module
#     is not SPA source, so the merge still pays nothing
echo "package main // server v1" >"$TMP/api/http_channel/server.go"
git -C "$TMP" add -A
git -C "$TMP" commit -qm "go change: http_channel server (not ui)"
rm -f "$TMP.mock/canary-mock-ran"
run --base "$(git -C "$TMP" rev-parse HEAD~1)" "go-merge api"
check "api shell Go change: exit 0" test "$rc" -eq 0
check "api shell Go change: still skips (no build)" test ! -f "$TMP.mock/canary-mock-ran"
check "api shell Go change: skip logged" contains "change gate: no webui source" "$TMP/run.out"

# fail-loud: broken build aborts, static/ untouched, no commit
STAMP="s3"
echo "source v3" >"$TMP/webui/app/Foo.vue"
git -C "$TMP" commit -qam "ui change: Foo v3"
before_static="$(git -C "$TMP" rev-parse HEAD:api/static)"
MOCK_FAIL=1 run --base "$(git -C "$TMP" rev-parse HEAD~1)" "card t_test3"
check "broken build: nonzero exit" test "$rc" -ne 0
check "broken build: static/ untouched" test "$(git -C "$TMP" rev-parse HEAD:api/static)" = "$before_static"
check "broken build: aborted loudly" contains "FAILED" "$OUT"
check "broken build: never hand-resolve law cited" contains "NEVER hand-resolved" "$OUT"

# belt-and-suspenders: conflict markers in regenerated output rejected
MOCK_POISON=1 run --base "$(git -C "$TMP" rev-parse HEAD~1)" "card t_test3"
check "poisoned bundle: rejected by lint gate" test "$rc" -ne 0
check "poisoned bundle: no lane commit" test "$(git -C "$TMP" rev-parse HEAD:api/static)" = "$before_static"

########## repo 2: base detection ##########
new_repo
# root commit without --base: nothing to infer the change gate from
run "ctx-only"
check "no --base on a root commit: nonzero exit" test "$rc" -ne 0
check "no --base on a root commit: error logged" contains "no parent commit" "$OUT"

# linear (fast-forward-landed) HEAD: default base = HEAD^1 works
STAMP="s1"
echo "source v2" >"$TMP/webui/app/Foo.vue"
git -C "$TMP" commit -qam "ui commit: Foo v2 (linear landing)"
run
check "linear HEAD: default base = HEAD^1 -> rc 0" test "$rc" -eq 0
check "linear HEAD: change gate saw the ui commit (lane commit created)" contains "embed-lane: rebuild webui bundle for merge" <(git -C "$TMP" log -1 --pretty=%s)

# true merge commit: default base = first parent (the pre-merge tip)
git -C "$TMP" checkout -q -b webui-a "$BASE"
STAMP="s1"
echo "panel v3" >"$TMP/harness/tools/ui/Bar.vue"
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
echo "aa: source" >"$TMP/webui/app/Foo.vue"
git -C "$TMP" commit -qam "branch A: component 1"
git -C "$TMP" checkout -q "$MAIN_BRANCH"
git -C "$TMP" checkout -q -b wui-b "$BASE"
echo "bb: source" >"$TMP/harness/tools/ui/Bar.vue"
git -C "$TMP" commit -qam "branch B: component 2"
git -C "$TMP" checkout -q "$MAIN_BRANCH"
git -C "$TMP" merge -q --no-edit wui-a >/dev/null
run "branch A"
check "parallel A: lane commit ok" test "$rc" -eq 0
check "parallel A: bundle carries A's regen" contains "bundle-stamp=pa" "$TMP/api/static/index.html"
STAMP="pb"
rc_merge=0
git -C "$TMP" merge --no-edit wui-b >/dev/null 2>"$TMP/merge-b.err" || rc_merge=$?
check "parallel B merges with zero static/ conflicts" test "$rc_merge" -eq 0
run "branch B"
check "parallel B: lane commit ok" test "$rc" -eq 0
check "parallel B: bundle carries B's regen" contains "bundle-stamp=pb" "$TMP/api/static/index.html"

########## summary ##########
echo "embed-lane_test: pass=$pass fail=$fail"
[ "$fail" -eq 0 ]