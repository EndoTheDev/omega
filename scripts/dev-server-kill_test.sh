#!/usr/bin/env bash
set -uo pipefail

# dev-server-kill_test.sh - fixture-table tests for scripts/dev-server-kill.sh.
# Run: bash scripts/dev-server-kill_test.sh
#
# Every case rides the script's --table fixture flag: NO real process is
# enumerated, and --apply refuses to shell out to taskkill under it, so a
# green run here cannot kill anything. The fixture table is the verdict
# engine's whole input surface (pid|name|cmdline|cwd|card-status[|ports]),
# which is why the dry-run output is the test surface.

KILLER="$(cd "$(dirname "$0")" && pwd)/dev-server-kill.sh"

# winbtw seam: the probe is a NATIVE python (it reads the fixture file), and
# mktemp seeds /tmp on MSYS, which python cannot open - every case would red
# with an empty table. Repoint the temp base at a Windows-visible dir when
# cygpath exists; the Linux/CI lane keeps today's /tmp behavior.
if command -v cygpath >/dev/null 2>&1 && [ -n "${LOCALAPPDATA:-}" ]; then
  WINTMP="$(cygpath -m "$LOCALAPPDATA/Temp" 2>/dev/null || true)"
  if [ -n "$WINTMP" ] && [ -d "$WINTMP" ]; then
    export TMPDIR="$WINTMP"
  fi
fi

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() {
  fail=$((fail + 1))
  echo "FAIL: $1" >&2
}
check() {
  local label="$1"
  shift
  if "$@"; then ok; else bad "$label"; fi
}
contains() { grep -qF "$1" "$2"; }
not_contains() { ! grep -qF "$1" "$2"; }

WORK="$(mktemp -d)" || {
  echo "dev-server-kill_test: cannot create a temp dir" >&2
  exit 2
}
trap 'rm -rf -- "$WORK" 2>/dev/null || true' EXIT

REPO="C:/fake/repo"
OUT="$WORK/out.txt"
ERR="$WORK/err.txt"

# run - judge a fixture table; stdout lands in $OUT, stderr in $ERR.
run() {
  bash "$KILLER" --table "$1" --repo "$REPO" >"$OUT" 2>"$ERR"
}

# The main fixture. Comment lines name what each row is FOR; the assertions
# below are one per verdict class the card rules define.
cat >"$WORK/table-main.txt" <<'TABLE'
# --- dev server identity, per owning card ---------------------------------
# done card, nuxt dev in its worktree -> KILL
101|node.exe|node C:/fake/repo/.worktrees/t_done1/webui/node_modules/.bin/nuxt dev|C:/fake/repo/.worktrees/t_done1/webui/|done
# archived card (a merged card leaves the board archived) -> KILL
102|node.exe|node C:/fake/repo/.worktrees/t_merged2/webui/node_modules/.bin/vite|C:/fake/repo/.worktrees/t_merged2/webui/|archived
# running card -> KEEP (never-kill class)
103|node.exe|node C:/fake/repo/.worktrees/t_run3/webui/node_modules/.bin/nuxt dev|C:/fake/repo/.worktrees/t_run3/webui/|running
# ready card -> KEEP
104|node.exe|node C:/fake/repo/.worktrees/t_rdy4/webui/node_modules/.bin/nuxt dev|C:/fake/repo/.worktrees/t_rdy4/webui/|ready
# review card -> KEEP
105|node.exe|node C:/fake/repo/.worktrees/t_rev5/webui/node_modules/.bin/nuxt dev|C:/fake/repo/.worktrees/t_rev5/webui/|review
# owning card unprovable -> UNKNOWN (keep, report)
106|node.exe|node C:/fake/repo/.worktrees/t_ghost6/webui/node_modules/.bin/nuxt dev|C:/fake/repo/.worktrees/t_ghost6/webui/|-
# a node process that is NOT a dev server, inside a dead card's worktree:
# a language server or test runner keeps its directory -> UNKNOWN, never KILL
107|node.exe|node C:/Users/user/lsp/cli.js --stdio start|C:/fake/repo/.worktrees/t_done1/|done
# --- main checkout dev servers --------------------------------------------
# nuxt/vite from the main checkout, no live webui card -> KILL
108|node.exe|node C:/fake/repo/webui/node_modules/vite/bin/vite.js --port 3000|C:/fake/repo/webui/|-
# a dev server outside the checkout is somebody else's project -> UNKNOWN
109|node.exe|node D:/Other/app/node_modules/vite/bin/vite.js|D:/Other/app/|-
# --- serves and ports ------------------------------------------------------
# the lane serve on :8098 is law -> KEEP whatever else is true
110|omega.exe|"C:/fake/repo/bin/omega.exe" serve|C:/WINDOWS/system32/|-|8098
# prod on :8099 -> KEEP
111|omega.exe|"/opt/omega/omega" serve|/|-|8099
# an omega serve with no provable owner -> KEEP (never-kill class)
112|omega.exe|"C:/Users/user/omega.exe" serve|C:/WINDOWS/system32/|-|
# omega serve left behind in a done card's worktree -> KILL
113|omega.exe|"D:/Code/omega/bin/omega.exe" serve|C:/fake/repo/.worktrees/t_done7/|done|18311
# --- the tree rule: a child of a KILL verdict rides the kill ---------------
# an esbuild/vite service child (no nuxt/vite in its OWN argv) orphaned under
# a KILL dev server -> KILL, named through its parent
114|node.exe|node C:/fake/repo/.worktrees/t_done1/node_modules/esbuild/bin/esbuild --service=0.28.2|C:/fake/repo/.worktrees/t_done1/|done||101
# a helper child of a KEEP process (the :8098 lane) never inherits a kill
115|node.exe|node C:/Users/user/helper.js|C:/WINDOWS/system32/|-||110
# --- malformed / blank -----------------------------------------------------
# blank lines and directives are not rows
TABLE

run "$WORK/table-main.txt"
check "exit 0 in a dry-run" [ "$?" -eq 0 ]
check "done-card worktree nuxt = KILL" contains "KILL 101 " "$OUT"
check "archived-card worktree vite = KILL" contains "KILL 102 " "$OUT"
check "running-card worktree nuxt = KEEP" contains "KEEP 103 " "$OUT"
check "ready-card worktree nuxt = KEEP" contains "KEEP 104 " "$OUT"
check "review-card worktree nuxt = KEEP" contains "KEEP 105 " "$OUT"
check "unprovable owner = UNKNOWN" contains "UNKNOWN 106 " "$OUT"
check "non-dev-server node in a dead worktree = UNKNOWN" contains "UNKNOWN 107 " "$OUT"
check "non-dev-server node is never a KILL" not_contains "KILL 107 " "$OUT"
check "main-checkout nuxt, no live webui card = KILL" contains "KILL 108 " "$OUT"
check "dev server outside the checkout = UNKNOWN" contains "UNKNOWN 109 " "$OUT"
check ":8098 lane serve = KEEP" contains "KEEP 110 " "$OUT"
check ":8099 prod = KEEP" contains "KEEP 111 " "$OUT"
check "ownerless omega serve = KEEP" contains "KEEP 112 " "$OUT"
check "omega serve in a done worktree = KILL" contains "KILL 113 " "$OUT"
check "tree child of a KILL dev server = KILL" contains "KILL 114 " "$OUT"
check "tree kill names its parent" contains "tree child of KILL 101" "$OUT"
check "a child of a KEEP process stays UNKNOWN" contains "UNKNOWN 115 " "$OUT"
check "a child of a KEEP process is never a KILL" not_contains "KILL 115 " "$OUT"

# Every stdout line is either a verdict line (`<VERDICT> <pid> <evidence>`) or
# the summary - nothing else leaks into the table.
check "every stdout line is a verdict line or the summary" \
  bash -c "! grep -vE '^(KILL|KEEP|UNKNOWN) [0-9]+ .+|^KILL-STEP: [0-9]+ kill / [0-9]+ keep / [0-9]+ unknown\$' '$OUT' | grep -q ."

# Summary line: `KILL-STEP: <n> kill / <k> keep / <u> unknown`, counted from
# the table above (KILL: 101 102 108 113 114; KEEP: 103 104 105 110 111 112;
# UNKNOWN: 106 107 109 115).
check "summary line format + counts" contains "KILL-STEP: 5 kill / 6 keep / 4 unknown" "$OUT"
check "summary counts match the verdict lines" \
  bash -c "[ \"\$(grep -c '^KILL ' '$OUT')\" = 5 ] && [ \"\$(grep -c '^KEEP ' '$OUT')\" = 6 ] && [ \"\$(grep -c '^UNKNOWN ' '$OUT')\" = 4 ]"

# --- a live card owning webui work flips the main-checkout verdict ---------
cat >"$WORK/table-webui-live.txt" <<'TABLE'
#webui-live: t_web9
108|node.exe|node C:/fake/repo/webui/node_modules/vite/bin/vite.js|C:/fake/repo/webui/|-
TABLE
run "$WORK/table-webui-live.txt"
check "main-checkout dev server with a live webui card = KEEP" contains "KEEP 108 " "$OUT"
check "the keep names the owning card" contains "t_web9" "$OUT"
check "one-line summary" contains "KILL-STEP: 0 kill / 1 keep / 0 unknown" "$OUT"

# --- --apply under --table is a NO-OP -------------------------------------
# The fixture asks for kills; --apply must still not reach taskkill. Pid
# 101 is a fixture number, but the contract is the branch: no APPLY line,
# no taskkill call, exit 0.
bash "$KILLER" --apply --table "$WORK/table-main.txt" --repo "$REPO" >"$OUT" 2>"$ERR"
check "--apply on a fixture exits 0" [ "$?" -eq 0 ]
check "--apply on a fixture prints no APPLY line" not_contains "APPLY " "$OUT"
check "--apply on a fixture still prints the verdicts" contains "KILL 101 " "$OUT"
check "--apply on a fixture says why it is a no-op" contains "no-op under --table" "$ERR"

# --- argument handling ----------------------------------------------------
bash "$KILLER" --nonsense >/dev/null 2>&1
check "unknown argument exits 2" [ "$?" -eq 2 ]
bash "$KILLER" --table "$WORK/does-not-exist.txt" >/dev/null 2>&1
check "missing fixture table exits 2" [ "$?" -eq 2 ]

# --- the live probe never runs under --table ------------------------------
# A fixture run must not read the board, netstat, or any process: the only
# host fact it is allowed is python's existence. Prove it by pointing the
# board env at a path that would explode if read.
HERMES_KANBAN_HOME="$WORK/no-such-board-home" \
  HERMES_KANBAN_DB="$WORK/no-such-board.db" \
  bash "$KILLER" --table "$WORK/table-webui-live.txt" --repo "$REPO" >"$OUT" 2>"$ERR"
check "fixture run ignores the board" contains "KILL-STEP: 0 kill / 1 keep / 0 unknown" "$OUT"
check "fixture run reports no board complaint" not_contains "board read" "$ERR"

echo "dev-server-kill_test: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
