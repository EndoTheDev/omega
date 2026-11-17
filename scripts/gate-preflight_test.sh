#!/usr/bin/env bash
set -uo pipefail

# gate-preflight_test.sh - fixture tests for scripts/gate-preflight.sh.
# Run: bash scripts/gate-preflight_test.sh
#
# NO browser, NO go build, NO nuxt and NO real lane is ever touched: every
# stage of the script is driven through its documented stub seams
# (PREFLIGHT_AB / PREFLIGHT_BUILD_CMD / PREFLIGHT_HEALTH_CMD /
# PREFLIGHT_JOURNEYS_CMD) and the red-list formatter is judged offline through
# --evidence. The one thing each case really exercises is the CONTRACT: the
# exit-code ladder (0 green / 1 red list / 2 infra), the one-line-per-red shape,
# and the never-kill law (a decoy process must survive a full pre-flight, and
# the lane the script started must be gone).

PREFLIGHT="$(cd "$(dirname "$0")" && pwd)/gate-preflight.sh"

# winbtw seam: the fixture files are read by NATIVE binaries (git, the stub
# bins) and mktemp seeds /tmp on MSYS, which they cannot open - repoint the temp
# base at a Windows-visible dir when cygpath exists. The CI/Pi lane keeps /tmp.
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
contains() { grep -qF -- "$1" "$2"; }
not_contains() { ! grep -qF -- "$1" "$2"; }

WORK="$(mktemp -d)" || {
  echo "gate-preflight_test: cannot create a temp dir" >&2
  exit 2
}
DECOY=""
cleanup() {
  # a fixture lane stub that outlived a red case is a leak of THIS suite only.
  if [ -f "$WORK/lane.pid" ]; then
    lp="$(cat "$WORK/lane.pid" 2>/dev/null || true)"
    [ -n "$lp" ] && kill "$lp" 2>/dev/null
  fi
  [ -n "$DECOY" ] && kill "$DECOY" 2>/dev/null
  rm -rf -- "$WORK" 2>/dev/null || true
}
trap cleanup EXIT

OUT="$WORK/out.txt"
ERR="$WORK/err.txt"
export PREFLIGHT_TEST_PIDFILE="$WORK/lane.pid"

# run <args...>: invoke the script; stdout -> $OUT, stderr -> $ERR, $RC = exit.
RC=0
run() {
  bash "$PREFLIGHT" "$@" >"$OUT" 2>"$ERR"
  RC=$?
}

# =============================================================================
# 1. the red LIST (offline: --evidence is the formatter's whole input surface)
# =============================================================================
EVID="$WORK/evid"
mkdir -p "$EVID"

# A green log carries step lines only; a die() red carries `[journey RED] <msg>`;
# a wall-cap red carries run.sh's watchdog line; a crash red carries neither
# (it died before lib.sh loaded) and falls back to the last line. \r line endings
# ride along because a windows-authored log really does carry them.
printf '%s\n' \
  '12:00:01 [journey] ok: open: http://127.0.0.1:18200' \
  '12:00:02 [journey] ok: console silent' >"$EVID/10-alpha.log"
printf '%s\n' \
  '12:00:03 [journey] step: drag the divider' \
  '12:00:04 [journey RED] right divider inward: got [220] want [160] | gesture-active=0' \
  '12:00:04 [journey RED] cascade: a second failure must not be listed' >"$EVID/84-divider.log"
printf '%s\n' \
  '12:00:05 [journey] step: scroll to the anchor' >"$EVID/113-scroll.log"
printf '%s\n' 'journeys: 113-scroll exceeded 300s wall cap' >>"$EVID/113-scroll.log"
printf '12:00:06 [journey] step: boot\r\nbash: line 4: open_app: command not found\r\n\r\n' >"$EVID/77-crash.log"

# An id the suite reported as RED whose log carries no marker at all: the union
# must still list it (silence would read as green).
printf '%s\n' '12:00:07 [journey] ok: nothing failed here' >"$EVID/55-quiet.log"
printf '%s\n' \
  'journeys: 10-alpha                       green' \
  'journeys: 55-quiet                       RED' \
  'journeys: 84-divider                     RED' \
  'journeys: 113-scroll                     RED' \
  'journeys: 77-crash                       RED' >"$WORK/suite-stdout.log"

run --evidence "$EVID" --suite-log "$WORK/suite-stdout.log"
check "red evidence exits 1" [ "$RC" -eq 1 ]
check "one RED line per red (4)" [ "$(grep -c '^RED ' "$OUT")" -eq 4 ]
check "the green journey is not listed" not_contains "RED 10-alpha" "$OUT"
check "id + first assertion line" contains "RED 84-divider  right divider inward: got [220] want [160] | gesture-active=0" "$OUT"
check "the first failure wins, the cascade is not listed" not_contains "cascade" "$OUT"
check "the timestamp and the RED prefix are stripped" not_contains "[journey RED]" "$OUT"
check "a wall-cap red carries the watchdog line" contains "RED 113-scroll  journeys: 113-scroll exceeded 300s wall cap" "$OUT"
check "the crash red falls back to its last non-blank line" contains "RED 77-crash  bash: line 4: open_app: command not found" "$OUT"
check "stdout carries no carriage returns" bash -c '! grep -q "$(printf "\r")" "$1"' _ "$OUT"
check "an id named only by the suite log is still a red" contains "RED 55-quiet" "$OUT"
check "the verdict line counts the reds" contains "gate-preflight: 4 red - dispatch blocked" "$OUT"
check "ids are sorted" bash -c "[ \"\$(grep -o '^RED [^ ]*' '$OUT' | tr '\n' ' ')\" = 'RED 113-scroll RED 55-quiet RED 77-crash RED 84-divider ' ]"

# stdout is machine-readable: RED lines and gate-preflight verdict lines only.
check "stdout carries RED and verdict lines only" \
  bash -c "! grep -vE '^(RED [^ ]+  |gate-preflight: )' '$OUT' | grep -q ."

# all green
GREEN="$WORK/evid-green"
mkdir -p "$GREEN"
printf '%s\n' '12:00:01 [journey] ok: open' '12:00:02 [journey] ok: console silent' >"$GREEN/10-alpha.log"
printf '%s\n' 'journeys: 10-alpha                       green' >"$WORK/suite-green.log"
run --evidence "$GREEN" --suite-log "$WORK/suite-green.log"
check "green evidence exits 0" [ "$RC" -eq 0 ]
check "green prints no RED line" not_contains "RED " "$OUT"
check "green says how many logs it judged" contains "0 red of 1 journey log(s)" "$OUT"

# an empty evidence dir is NOT a green (the suite never reached a recipe)
EMPTY="$WORK/evid-empty"
mkdir -p "$EMPTY"
run --evidence "$EMPTY"
check "an empty evidence dir exits 2" [ "$RC" -eq 2 ]
check "an empty evidence dir says nothing was judged" contains "nothing to judge" "$ERR"

# format-only mode touches nothing: no lane, no browser, no build
run --evidence "$EVID"
check "evidence mode without a suite log still lists the log-derived reds" [ "$RC" -eq 1 ]
check "evidence mode refuses a missing dir" bash -c "bash '$PREFLIGHT' --evidence '$WORK/nope' >/dev/null 2>&1; [ \$? -eq 2 ]"

# =============================================================================
# 2. the agent-browser pin (infra: exit 2, never a red)
# =============================================================================
# Every case from here on pins the browser through PREFLIGHT_AB, so the result
# does not depend on what this host happens to have on PATH.
PREFLIGHT_AB="$WORK/no-such-agent-browser"
export PREFLIGHT_AB
run
check "a missing PREFLIGHT_AB exits 2" [ "$RC" -eq 2 ]
check "the missing browser message names the install command" contains "npm i -g agent-browser@0.42.0" "$ERR"

# off-pin stub: 0.43.0 is the version that broke the gesture path.
mkdir -p "$WORK/ab-float"
printf '%s\n' '#!/usr/bin/env bash' 'echo "agent-browser 0.43.0"' >"$WORK/ab-float/agent-browser"
chmod +x "$WORK/ab-float/agent-browser"
PREFLIGHT_AB="$WORK/ab-float/agent-browser"
run
check "an off-pin agent-browser exits 2" [ "$RC" -eq 2 ]
check "the off-pin message names the install pin" contains "npm i -g agent-browser@0.42.0" "$ERR"
check "the off-pin message names what it saw" contains "0.43.0" "$ERR"

# on-pin stub: the gate passes and the run goes on to fail at the build - the
# pin's own refusal line must be ABSENT, or the assertions above proved nothing.
mkdir -p "$WORK/ab-pinned"
printf '%s\n' '#!/usr/bin/env bash' 'echo "agent-browser 0.42.0"' >"$WORK/ab-pinned/agent-browser"
chmod +x "$WORK/ab-pinned/agent-browser"
PREFLIGHT_AB="$WORK/ab-pinned/agent-browser"
PREFLIGHT_BUILD_CMD='exit 1'
export PREFLIGHT_BUILD_CMD
run
check "on-pin passes the gate and dies at the build" [ "$RC" -eq 2 ]
check "the build failure is named" contains "the lane build failed" "$ERR"
check "an on-pin browser produces no pin complaint" bash -c '! grep -qF "s PIN is" "$1"' _ "$ERR"

# =============================================================================
# 3. the build stage
# =============================================================================
PREFLIGHT_BUILD_CMD=':'
run
check "a build that leaves no binary exits 2" [ "$RC" -eq 2 ]
check "the empty build is named" contains "left no executable" "$ERR"
unset PREFLIGHT_BUILD_CMD

# a build that produces a lane which dies immediately: the health wait must not
# burn its whole budget on a corpse, and the lane log must be shown.
cat >"$WORK/build-dead-lane.sh" <<'STUB'
#!/usr/bin/env bash
{
  printf '%s\n' '#!/usr/bin/env bash'
  printf '%s\n' 'echo "fixture lane: no provider reachable" >&2'
  printf '%s\n' 'exit 1'
} >"$PREFLIGHT_BIN"
chmod +x "$PREFLIGHT_BIN"
STUB
started=$SECONDS
# A deliberately HUGE wait budget: the point is not the absolute time (the stage
# costs several seconds on winbtw) but that a corpse is detected instead of
# burning the budget.
PREFLIGHT_BUILD_CMD="bash '$WORK/build-dead-lane.sh'"
PREFLIGHT_LANE_WAIT=100
export PREFLIGHT_BUILD_CMD PREFLIGHT_LANE_WAIT
run --port 18211
elapsed=$((SECONDS - started))
unset PREFLIGHT_LANE_WAIT
check "a lane that dies exits 2" [ "$RC" -eq 2 ]
check "a dead lane is named as infra" contains "lane will not start" "$ERR"
check "a dead lane prints its log tail" contains "fixture lane: no provider reachable" "$ERR"
check "a dead lane short-circuits the health wait (did not burn 100s)" [ "$elapsed" -lt 40 ]

# =============================================================================
# 4. never-kill: ports, decoys, and the source law
# =============================================================================
run --port 8098
check "binding the dev lane is refused" [ "$RC" -eq 2 ]
check "the refusal names the never-kill law" contains "never-kill class" "$ERR"
run --port 8099
check "binding prod is refused" [ "$RC" -eq 2 ]

# a real decoy process, started by this suite, must survive a full pre-flight.
sleep 90 &
DECOY=$!

cat >"$WORK/build-ok.sh" <<'STUB'
#!/usr/bin/env bash
{
  printf '%s\n' '#!/usr/bin/env bash'
  printf '%s\n' 'printf "%s\n" "$$" > "$PREFLIGHT_TEST_PIDFILE"'
  printf '%s\n' 'exec sleep 90'
} >"$PREFLIGHT_BIN"
chmod +x "$PREFLIGHT_BIN"
STUB

cat >"$WORK/journeys-green.sh" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' '12:00:01 [journey] ok: open' '12:00:02 [journey] ok: console silent' \
  >"$PREFLIGHT_EVIDENCE/10-alpha.log"
printf '%s\n' 'journeys: 10-alpha                       running'
printf '%s\n' 'journeys: 10-alpha                       green'
printf '%s\n' 'journeys: suite done - 2s, green=1 skipped=0 red=0'
exit 0
STUB

rm -f "$PREFLIGHT_TEST_PIDFILE"
PREFLIGHT_BUILD_CMD="bash '$WORK/build-ok.sh'"
PREFLIGHT_HEALTH_CMD='printf %s "{\"status\":\"ok\"}"'
PREFLIGHT_JOURNEYS_CMD="bash '$WORK/journeys-green.sh'"
export PREFLIGHT_BUILD_CMD PREFLIGHT_HEALTH_CMD PREFLIGHT_JOURNEYS_CMD
run --port 18212
lane_pid="$(cat "$PREFLIGHT_TEST_PIDFILE" 2>/dev/null || true)"
check "all-green pre-flight exits 0" [ "$RC" -eq 0 ]
check "green prints the discovery caveat" contains "only a green nightly certifies a cut" "$OUT"
check "green prints the tip it tested" contains "gate-preflight: tip " "$OUT"
check "the lane was started" [ -n "$lane_pid" ]
check "the lane it started is stopped" bash -c "! kill -0 '$lane_pid' 2>/dev/null"
check "the decoy process was never touched" bash -c "kill -0 '$DECOY' 2>/dev/null"
check "the lane stop names the pid discipline" contains "nothing else was signalled" "$ERR"

# =============================================================================
# 5. the suite's own exit codes (3 = lock held, 2 = infra, 1 = red)
# =============================================================================
for code in 2 3
do
  rc=$code
  PREFLIGHT_JOURNEYS_CMD="exit $rc"
  export PREFLIGHT_JOURNEYS_CMD
  run --port 18213
  check "a suite exit $rc maps to infra exit 2" [ "$RC" -eq 2 ]
done
check "a lock-held suite says nothing ran" contains "holds the run lock" "$ERR"

cat >"$WORK/journeys-red.sh" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' '12:00:01 [journey] step: drag the divider' \
  '12:00:02 [journey RED] right divider inward: got [220] want [160] | gesture-active=0' \
  >"$PREFLIGHT_EVIDENCE/84-divider.log"
printf '%s\n' 'journeys: 84-divider                     RED'
printf '%s\n' 'journeys: suite done - 2s, green=0 skipped=0 red=1'
printf '%s\n' 'journeys: RED - failing: 84-divider'
exit 1
STUB
PREFLIGHT_JOURNEYS_CMD="bash '$WORK/journeys-red.sh'"
export PREFLIGHT_JOURNEYS_CMD
run --port 18214
check "a red suite exits 1" [ "$RC" -eq 1 ]
check "the end-to-end red list carries id + assertion" \
  contains "RED 84-divider  right divider inward: got [220] want [160] | gesture-active=0" "$OUT"
check "the end-to-end verdict blocks dispatch" contains "dispatch blocked" "$OUT"
check "the suite verdict line is not read as a journey id" not_contains "RED suite" "$OUT"

# run.sh keeps its OWN evidence dir (it mktemps one per run) and names it in its
# output; the assertion lines must come from THERE, not from the dir this script
# made. The stub writes its logs into a separate dir and prints the line.
cat >"$WORK/journeys-own-evid.sh" <<'STUB'
#!/usr/bin/env bash
own="$PREFLIGHT_EVIDENCE/../run-own-evid"
mkdir -p "$own"
printf '%s\n' '12:00:09 [journey RED] divider absorb: got [354] want [320]' \
  >"$own/75-divider-resize.log"
printf '%s\n' 'journeys: 75-divider-resize             RED'
printf '%s\n' "journeys: evidence in $own"
printf '%s\n' 'journeys: RED - failing: 75-divider-resize'
exit 1
STUB
PREFLIGHT_JOURNEYS_CMD="bash '$WORK/journeys-own-evid.sh'"
export PREFLIGHT_JOURNEYS_CMD
run --port 18215
check "a suite that names its own evidence dir still exits 1" [ "$RC" -eq 1 ]
check "the assertion line comes from the suite's own evidence dir" \
  contains "RED 75-divider-resize  divider absorb: got [354] want [320]" "$OUT"
check "the missing-log fallback is not what printed" not_contains "no log" "$OUT"

# The belt: a suite that reports RED only in its FINAL verdict line (the
# per-recipe line lost to padding or a truncated log) must still list the red -
# and a suite that says RED without naming anything must never exit 0.
cat >"$WORK/journeys-summary-only.sh" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' 'journeys: suite done - 9s, green=1 skipped=0 red=1'
printf '%s\n' 'journeys: RED — failing: 84-divider 98-kanban'
exit 1
STUB
PREFLIGHT_JOURNEYS_CMD="bash '$WORK/journeys-summary-only.sh'"
export PREFLIGHT_JOURNEYS_CMD
run --port 18216
check "a summary-only red list still exits 1" [ "$RC" -eq 1 ]
check "the summary line names the first red" contains "RED 84-divider" "$OUT"
check "the summary line names the second red" contains "RED 98-kanban" "$OUT"

cat >"$WORK/journeys-nameless-red.sh" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' 'journeys: suite done - 9s, green=4 skipped=0 red=1'
exit 1
STUB
PREFLIGHT_JOURNEYS_CMD="bash '$WORK/journeys-nameless-red.sh'"
export PREFLIGHT_JOURNEYS_CMD
run --port 18217
check "a red with no id never exits 0" [ "$RC" -eq 2 ]
check "the nameless red is named as infra" contains "reported red but named no journey" "$ERR"

# =============================================================================
# 6. argument handling + the never-kill source law
# =============================================================================
run --nonsense
check "an unknown argument exits 2" [ "$RC" -eq 2 ]
run --port
check "--port with no value exits 2" [ "$RC" -eq 2 ]
run --port abc
check "--port with garbage exits 2" [ "$RC" -eq 2 ]
run --help
check "--help exits 0" [ "$RC" -eq 0 ]
check "--help documents the exit ladder" contains "Exit codes: 0 all green" "$OUT"

CODE_ONLY="$WORK/code-lines.txt"
grep -v '^[[:space:]]*#' "$PREFLIGHT" >"$CODE_ONLY"
check "no pkill/killall/taskkill in code (never-kill law)" \
  bash -c "! grep -Eq 'pkill|killall|taskkill' '$CODE_ONLY'"
check "every kill verb in code targets the recorded lane pid" \
  bash -c "! grep -E '(^|[[:space:];&|(])kill[[:space:]]+-[A-Za-z0-9]' '$CODE_ONLY' | grep -vq 'lane_pid'"
check "the script states the never-kill law in its header" contains "never-kill class" "$PREFLIGHT"
check "the script states the known limit in its header" contains "local green does NOT certify" "$PREFLIGHT"

echo "gate-preflight_test: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
