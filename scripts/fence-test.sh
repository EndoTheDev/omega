#!/usr/bin/env bash
set -u   # no -e: refused operations are expected exits, the counters decide

# fence-test.sh - shell checks for bin/shims/git, the machine fence
# (card t_8b32a09a). Run: bash scripts/fence-test.sh
#
# Fenced mode stamps OMEGA_WORKER=1: push refuses, every dev/master
# landing refuses, ordinary work forwards. Unstamped mode forwards to
# the real git byte-for-byte - proved by a push that must actually
# land on a local bare remote.

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SHIM="$ROOT/bin/shims/git"
[ -f "$SHIM" ] || { echo "FAIL: shim missing at $SHIM"; exit 1; }

pass=0; fail=0
ok()    { pass=$((pass + 1)); }
bad()   { fail=$((fail + 1)); echo "FAIL: $1" >&2; }

# Fixture: a repo with disjoint master/dev/feat branches (separate files,
# merges never conflict) and a local bare origin.
FIX="$(mktemp -d)"
trap 'rm -rf "$FIX"' EXIT
git -C "$FIX" -c init.defaultBranch=master init -q repo
R="$FIX/repo"
G() { git -C "$R" -c user.name=fence -c user.email=fence@local "$@"; }  # real git, setup + asserts
S() { env -C "$R" "$SHIM" "$@"; }                 # shim, unstamped
F() { env -C "$R" OMEGA_WORKER=1 "$SHIM" "$@"; }  # shim, fenced

G commit -q --allow-empty -m base
G branch -q dev HEAD
G branch -q feat HEAD
G checkout -q dev
echo dev > "$R/dev.txt"; G add dev.txt;   G commit -q -m devline
G checkout -q feat
echo feat > "$R/feat.txt"; G add feat.txt; G commit -q -m featline
DEVSHA="$(G rev-parse dev)"
G checkout -q -b pushless
git init -q --bare "$FIX/origin.git"
G remote add origin "$FIX/origin.git"

must_block() {  # label, then the fenced invocation
  local label="$1"; shift
  out=$("$@" 2>&1); rc=$?
  [ "$rc" -ne 0 ]         && ok || bad "$label: exited 0 (must refuse)"
  printf '%s' "$out" | grep -q "FENCE:" && ok || bad "$label: refusal did not name the law: $out"
}
must_pass() {   # label, then the invocation
  local label="$1"; shift
  "$@" >/dev/null 2>&1 && ok || bad "$label: refused an ordinary operation (exit $?)"
}

O() { git -C "$FIX/origin.git" "$@"; }    # asserts on the bare remote

# --- fenced: push is always refused, and nothing lands ---
must_block "push"                F push origin pushless:refs/heads/attpush
O rev-parse --verify --quiet refs/heads/attpush >/dev/null 2>&1 && \
  bad "push: the attempt LANDED on origin - the fence lied" || ok
must_block "push --dry-run"      F push --dry-run origin HEAD
must_block "push -C (global opt)" env -C "$FIX" OMEGA_WORKER=1 "$SHIM" -C "$R" push origin pushless
must_block "merge dev"           F merge dev
must_block "merge --ff-only master" F merge --ff-only master
must_block "merge origin/master" F merge origin/master
must_block "rebase master"       F rebase master
must_block "rebase onto dev"     F rebase --onto dev HEAD~1
must_block "reset --hard dev"    F reset --hard dev
must_block "reset --hard origin/master" F reset --hard origin/master
must_block "pull origin dev"     F pull origin dev
must_block "fetch dev refspec"   F fetch origin dev
must_block "fetch dev:dev"       F fetch origin +refs/heads/dev:refs/heads/dev
must_block "cherry-pick dev"     F cherry-pick dev
must_block "branch -f dev"       F branch -f dev HEAD
must_block "branch -m dev"       F branch -m dev dev-renamed
must_block "update-ref dev"      F update-ref refs/heads/dev HEAD

# a commit while dev is checked out IS a landing
F checkout -q dev
must_block "commit on dev checkout" F -c user.name=fence -c user.email=fence@local commit --allow-empty -m land
[ "$(G rev-parse dev)" = "$DEVSHA" ] && ok || bad "commit-on-dev: dev tip moved despite the refusal"

# --- fenced: ordinary work forwards ---
F checkout -q feat
must_pass "status"      F status --porcelain
must_pass "log"         F log -1 --oneline
must_pass "tag"         F tag fence-t1
must_pass "add+commit"  sh -c 'echo more >> "'"$R"'/feat.txt" && env -C "'"$R"'" OMEGA_WORKER=1 "'"$SHIM"'" add feat.txt && env -C "'"$R"'" OMEGA_WORKER=1 "'"$SHIM"'" -c user.name=fence -c user.email=fence@local commit -q -m more'
F checkout -q dev; F checkout -q feat   # unstamped-dev checkout/switch forwards

# --- unstamped: byte-for-byte passthrough, push really lands ---
must_pass "unstamped push lands" S push origin pushless:refs/heads/unfencedpush
O rev-parse --verify --quiet refs/heads/unfencedpush >/dev/null 2>&1 && \
  ok || bad "unstamped push: did not land on origin (passthrough broken)"
must_pass "unstamped merge dev"  S merge dev
must_pass "unstamped reset master" S reset --hard master
must_pass "unstamped work on dev" sh -c 'cd "'"$R"'" && git checkout -q master && git -c user.name=fence -c user.email=fence@local commit -q --allow-empty -m land && git checkout -q feat'

# a stamp of ANY value fences (fail-closed); only absence passes through
must_block "stamp '0' fences too" env -C "$R" OMEGA_WORKER=0 "$SHIM" push origin pushless

echo "fence-test: $pass passed, $fail failed"
[ "$fail" -eq 0 ]