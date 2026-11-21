#!/usr/bin/env bash
set -uo pipefail
# --- the pin: no line here may pipe a writer into an early-exit grep --------
# `printf '%s' "$OUT" | grep -q` under pipefail hands back the WRITER's
# SIGPIPE when grep leaves at the first match, so a needle that IS present
# reads as missing - and the negated form reads it as absent, which is a false
# green that hides a defect. A herestring has no writer, so grep's early exit
# costs nothing. This file scans its own live lines; comments may name the
# shape, so they are skipped.
_PIN_SHAPE='\|[[:space:]]*grep[[:space:]]+-[a-zA-Z]*q'
_PIN_SELF="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
_pin_hits="$(grep -vE '^[[:space:]]*#' "$_PIN_SELF" | grep -nE "$_PIN_SHAPE" || true)"
[ -z "$_pin_hits" ] \
	|| { printf '%s: pipes a writer into grep -q (SIGPIPE under pipefail): %s\n' "${_PIN_SELF##*/}" "$_pin_hits" >&2; exit 1; }


# gate-preflight.sh - local discovery run BEFORE a nightly gate dispatch.
#
# WHY (card t_062095b6): one gate attempt costs 30-45 minutes of CI to find
# reds that a local journey run finds in about 5. Three nights of gate cycles
# were discovery runs, not certification runs, because the release driver
# dispatched nightlies on unverified tips. This script is the discovery half:
# rebuild the lane from the current dev tip, run the journey suite against it,
# print the red LIST. Cheap enough to run before every dispatch.
#
# WHAT IT DOES (the five steps the card names):
#   1. rebuild the embed lane on the current dev tip - regenerate the SPA
#      bundle from source (webui/), overlay it into a STAGING copy of the
#      committed tree, and `go build ./cmd/omega` from that copy. The
#      checkout's api/static is never written: scripts/embed-lane.sh owns
#      every static/ commit, and a pre-flight is not a merge pass.
#   2. run scripts/journeys/run.sh against that lane, agent-browser PINNED at
#      0.42.0 (the suite's own JOURNEY_AB_REQUIRE_VERSION gate is set to 1, so
#      the two halves cannot drift apart the way the runner's float did on
#      2026-11-16 - see the pin block in scripts/journeys/run.sh).
#   3. print the red LIST, one line per red, in the nightly's shape:
#        RED <journey-id>  <first assertion line>
#      (the id is the recipe basename the nightly annotates; the assertion
#      line is the recipe's own `[journey RED] <msg>` - the first one, since
#      the first failure is the diagnosis and the rest is cascade).
#   4. exit 0 all green, exit 1 with the list on any red, exit 2 on
#      infrastructure failure (agent-browser missing or off the pin, the
#      bundle/binary will not build, the lane will not answer /health,
#      another suite holds the run lock, or there is nothing to judge).
#   5. kill NOTHING outside the never-kill class (the law of card t_a09099ed,
#      scripts/dev-server-kill.sh): the only process this script signals is
#      the lane binary it launched itself, by recorded pid, TERM then KILL.
#      No pkill/killall/name-pattern sweep, no taskkill, no port owner is ever
#      killed, and :8098 (dev lane) / :8099 (prod) are refused as a bind
#      target - a pre-flight never lands on a live lane, so it can never have
#      to clean one up.
#
# KNOWN LIMIT - STATED, NOT IMPLIED: local green does NOT certify. CI layout
# can differ from local by a small offset (the h+4 settings-clamp case; the
# gesture class that greens on winbtw Chrome and dies on the runner,
# t_ef12cb88). Pre-flight is DISCOVERY - it answers "is this tip already red
# before we pay for CI?". The nightly stays CERTIFICATION. Only a green
# nightly certifies a cut.
#
# stdout carries the RED LIST plus the verdict lines and nothing else, so a
# driver greps '^RED ' without noise. The suite's own output goes to a log file
# whose path is printed on stderr (never through a pipe into tee - see the
# hang note at the suite step).
#
# Usage:
#
#   scripts/gate-preflight.sh [--port <n>] [--skip-bundle] [--regression]
#                             [--keep]
#   scripts/gate-preflight.sh --evidence <dir> [--suite-log <file>]
#
#   --port <n>       lane port. Default: the first free port in 18200..18219.
#                    :8098 and :8099 are REFUSED (never-kill class).
#   --skip-bundle    build the COMMITTED api/static instead of regenerating
#                    the SPA. This is the byte-exact artifact the nightly
#                    builds; the default regenerates (step 1 above).
#   --regression     run only scripts/journeys/regression.list, not the full
#                    suite (the gate-time minimum, minutes instead of more).
#   --keep           keep the work dir (stage, lane home, evidence) and print
#                    its path for a human to poke at.
#   --evidence <d>   FORMAT ONLY: build/boot/run nothing; judge the journey
#                    logs already in <d> and print the red list. This is the
#                    offline surface scripts/gate-preflight_test.sh drives.
#   --suite-log <f>  with --evidence: also read that suite stdout log for
#                    `journeys: <name> RED` lines.
#
# Exit codes: 0 all green, 1 at least one red (the list is on stdout), 2
# infrastructure failure (the browser is missing or off the pin, the bundle or
# the binary will not build, the lane will not answer /health, another suite
# holds the run lock, or there is nothing to judge). Only exit 1 is a product
# verdict; exit 2 says nothing was asserted, in either direction.
#
# Test surface (the seams the house test law drives - every one of them is a
# stub target, never a behavior change; the same precedent as dev-server-kill's
# --table fixture):
#
#   PREFLIGHT_AB           agent-browser binary to judge (default: PATH).
#                          Also prepended to PATH for the suite, so both
#                          halves judge the same binary.
#   PREFLIGHT_AB_VERSION   the pin (default 0.42.0).
#   PREFLIGHT_BUILD_CMD    replaces step 1; must leave an executable at
#                          $PREFLIGHT_BIN. Default: bundle + `go build`.
#   PREFLIGHT_JOURNEYS_CMD replaces step 2; writes logs into
#                          $PREFLIGHT_EVIDENCE (and optionally the suite log at
#                          $PREFLIGHT_SUITE_LOG), exits like run.sh.
#   PREFLIGHT_HEALTH_CMD   replaces the /health probe; must print the lane's
#                          health JSON. Default: curl -s -m 5
#                          $PREFLIGHT_HEALTH_URL.
#   PREFLIGHT_LANE_WAIT    seconds to wait for /health (default 60).
#   PREFLIGHT_CHROME_PORT  CDP port the suite attaches to (default: the first
#                          free port in 9300..9339; the script refuses when none
#                          is free). Set it when a browser is
#                          already listening - run.sh attaches to a live CDP
#                          port instead of launching one (the winbtw lane: a
#                          host with no agent-browser-managed chromium).
#   PREFLIGHT_LOCK_WAIT    passed to run.sh as JOURNEY_LOCK_WAIT (default 0,
#                          i.e. refuse rather than interleave).

PROG=gate-preflight

usage() {
  sed -n '/^# Usage:/,/^# Test surface/p' "$0" | sed '$d' | sed 's/^# \{0,1\}//'
}

# --- args --------------------------------------------------------------------
port=""
evidence=""
suite_log_arg=""
keep=0
skip_bundle=0
regression=0

die2() { # $1: what is wrong. exit 2 = infrastructure, never a red.
  printf '%s: %s\n' "$PROG" "$1" >&2
  printf '%s: exit 2 (infrastructure) - nothing about the product was asserted\n' "$PROG" >&2
  exit 2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --port)
      case "${2:-}" in "" | -* | *[!0-9]*) die2 "--port needs a number (got '${2:-}')" ;; esac
      port="$2"
      shift 2
      ;;
    --evidence)
      case "${2:-}" in "" | -*) die2 "--evidence needs a directory (got '${2:-}')" ;; esac
      evidence="$2"
      shift 2
      ;;
    --suite-log)
      case "${2:-}" in "" | -*) die2 "--suite-log needs a file (got '${2:-}')" ;; esac
      suite_log_arg="$2"
      shift 2
      ;;
    --skip-bundle) skip_bundle=1; shift ;;
    --regression) regression=1; shift ;;
    --keep) keep=1; shift ;;
    -h | --help) usage; exit 0 ;;
    *) die2 "unknown argument: $1" ;;
  esac
done

# :8098 is the dev lane and :8099 is prod: a pre-flight must never bind either
# (the never-kill class is defined by those two ports - card t_a09099ed).
case "$port" in
  8098 | 8099)
    die2 "--port $port refused: :$port is never-kill class (the dev lane / prod). Use a private port, or none and the script picks one"
    ;;
esac

# --- red-list formatting (the ONE surface both modes share) -------------------
# A recipe log is authoritative for the ASSERTION; the suite stdout log is
# authoritative for the ID SET (run.sh prints `journeys: <name> RED`, which is
# also what the nightly annotates). Reading both keeps a silently-red recipe
# from vanishing just because run.sh's line was missed, and keeps a log with a
# stale marker from inventing a red nobody reported.

# red_ids <suite-log-or-empty> <evidence-dir>: one id per line, sorted.
red_ids() {
  local suite_log="$1" evid="$2" f
  {
    if [ -n "$suite_log" ] && [ -f "$suite_log" ]; then
      # run.sh prints `journeys: %-30s RED` - the padding is part of the shape.
      sed -n -E 's/^journeys: ([^ ]+) +RED$/\1/p' "$suite_log"
      # ... and its FINAL verdict line names the whole failing set. That line is
      # the belt: a red the per-recipe line lost (padding, a truncated log) must
      # never vanish into a green. The separator is matched without naming the
      # dash run.sh uses there.
      sed -n 's/^journeys: RED.*failing: *//p' "$suite_log" | tr ' ' '\n'
    fi
    if [ -n "$evid" ] && [ -d "$evid" ]; then
      for f in "$evid"/*.log; do
        [ -f "$f" ] || continue
        if grep -qE '\[journey RED\]|exceeded [0-9]+s wall cap|survived TERM' "$f" 2>/dev/null; then
          basename "$f" .log
        fi
      done
    fi
  } | LC_ALL=C sort -u
}

# first_assertion <log>: the recipe's own first failure line, clipped to one
# line. Fallbacks in order - the wall cap (the watchdog appends it, no die
# fired), the blocked-child SIGKILL, then the last non-blank line (a recipe
# that died before lib.sh loaded: a syntax error, a missing command).
first_assertion() {
  local log="$1" line=""
  [ -f "$log" ] || { printf 'no log\n'; return 0; }
  line=$(grep -m1 -F '[journey RED]' "$log" 2>/dev/null || true)
  [ -n "$line" ] || line=$(grep -m1 -E 'exceeded [0-9]+s wall cap' "$log" 2>/dev/null || true)
  [ -n "$line" ] || line=$(grep -m1 -F 'survived TERM' "$log" 2>/dev/null || true)
  [ -n "$line" ] || line=$(grep -v '^[[:space:]]*$' "$log" 2>/dev/null | tail -1 || true)
  printf '%s' "$line" |
    tr -d '\r' |
    sed -E 's/^[0-9]{2}:[0-9]{2}:[0-9]{2} \[journey RED\] //' |
    tr -s '[:space:]' ' ' |
    cut -c1-200
}

# print_reds <suite-log-or-empty> <evidence-dir>: the list, one line per red.
# Sets RED_N. Ids with no log still print (a red whose evidence vanished is
# still a red - silence there would read as green).
RED_N=0
print_reds() {
  local suite_log="$1" evid="$2" id
  RED_N=0
  for id in $(red_ids "$suite_log" "$evid"); do
    printf 'RED %s  %s\n' "$id" "$(first_assertion "$evid/$id.log")"
    RED_N=$((RED_N + 1))
  done
}

# --- format-only mode --------------------------------------------------------
if [ -n "$evidence" ]; then
  [ -d "$evidence" ] || die2 "--evidence is not a directory: $evidence"
  [ -z "$suite_log_arg" ] || [ -f "$suite_log_arg" ] || die2 "--suite-log not found: $suite_log_arg"
  logs=0
  for f in "$evidence"/*.log; do [ -f "$f" ] && logs=$((logs + 1)); done
  # An empty evidence dir is NOT a green: it means the suite never reached a
  # recipe, and a formatter that reads "nothing to judge" as "nothing wrong" is
  # the exact discovery lie this script exists to kill.
  [ "$logs" -gt 0 ] || die2 "no journey logs in $evidence (nothing to judge)"
  print_reds "$suite_log_arg" "$evidence"
  if [ "$RED_N" -gt 0 ]; then
    printf '%s: %s red - dispatch blocked\n' "$PROG" "$RED_N"
    exit 1
  fi
  printf '%s: 0 red of %s journey log(s) - green in this evidence dir\n' "$PROG" "$logs"
  exit 0
fi

# --- preconditions -----------------------------------------------------------
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root" || die2 "cannot enter the repo root ($root)"

command -v git >/dev/null 2>&1 || die2 "git not found"
git rev-parse --git-dir >/dev/null 2>&1 || die2 "not a git checkout: $root"
tip="$(git rev-parse --short HEAD)" || die2 "cannot read HEAD"
tip_subject="$(git log -1 --format=%s HEAD 2>/dev/null | cut -c1-60)"

if [ -n "$(git status --porcelain 2>/dev/null)" ]; then
  # The lane is built from the COMMITTED tree (git archive HEAD), so working-
  # file edits are invisible to it - say so instead of silently testing a
  # tree that is not what you are looking at.
  printf '%s: WARNING the checkout is dirty - the lane is built from the committed tree (%s), not your working files\n' \
    "$PROG" "$tip" >&2
fi

# --- agent-browser gate ------------------------------------------------------
ab_version="${PREFLIGHT_AB_VERSION:-0.42.0}"
if [ -n "${PREFLIGHT_AB:-}" ]; then
  ab="$PREFLIGHT_AB"
  [ -x "$ab" ] || die2 "agent-browser not found at PREFLIGHT_AB=$ab - install the pin: npm i -g agent-browser@$ab_version && agent-browser install"
else
  ab="$(command -v agent-browser 2>/dev/null | head -1)"
  [ -n "$ab" ] || die2 "agent-browser CLI not found (PATH). install the pin: npm i -g agent-browser@$ab_version && agent-browser install"
fi
ab_seen="$("$ab" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)"
if [ "$ab_seen" != "$ab_version" ]; then
  # Off the pin is an INFRASTRUCTURE failure, not a red: the nightly refuses to
  # run on a float (JOURNEY_AB_REQUIRE_VERSION=1) and 0.43.0's gesture path
  # reds 84/98 on a perfectly good tree. A local run on a different browser
  # would discover reds that nightly does not have, or miss the ones it does.
  die2 "agent-browser ${ab_seen:-unknown}, the suite's PIN is $ab_version - install it: npm i -g agent-browser@$ab_version"
fi

# --- work dir ----------------------------------------------------------------
work="$(mktemp -d "${TMPDIR:-/tmp}/$PROG.XXXXXX")" || die2 "cannot create a work dir"
stage="$work/tree"
lane_home="$work/lane"
evid="$work/evidence"
suite_log="$work/suite.log"
lane_log="$work/lane.log"
mkdir -p "$stage" "$lane_home/config" "$evid" || die2 "cannot populate the work dir"

case "$(uname -s 2>/dev/null)" in
  MINGW* | MSYS* | CYGWIN*) exe=".exe" ;;
  *) exe="" ;;
esac
bin="$lane_home/omega$exe"

winpath() { # a path a NATIVE tool (go, the omega exe, python) can open
  if command -v cygpath >/dev/null 2>&1; then cygpath -m "$1"; else printf '%s\n' "$1"; fi
}

lane_pid=""

# lane_alive: is the lane still RUNNING? /proc/<pid>/stat state 'Z' is a dead
# process its parent has not reaped yet, and kill -0 says yes to a corpse - a
# poll built on kill -0 alone waits its whole budget out on a lane that already
# died (the same trap run.sh's proc_alive names). No /proc entry for a pid the
# kernel still knows (as MSYS sometimes does) falls back to kill -0.
lane_alive() {
  local st
  [ -n "$lane_pid" ] || return 1
  st="$(cat "/proc/$lane_pid/stat" 2>/dev/null)" || { kill -0 "$lane_pid" 2>/dev/null; return; }
  case "${st##*) }" in Z*) return 1 ;; esac
  return 0
}

stop_lane() {
  # Never-kill law: ONE pid, the lane this script started. Not a pattern, not a
  # port owner, not a child sweep of somebody else's tree.
  [ -n "$lane_pid" ] || return 0
  lane_alive || { lane_pid=""; return 0; }
  kill -TERM "$lane_pid" 2>/dev/null
  i=0
  while [ "$i" -lt 20 ] && lane_alive; do
    sleep 0.25
    i=$((i + 1))
  done
  lane_alive && kill -KILL "$lane_pid" 2>/dev/null
  lane_pid=""
  return 0
}

cleanup() {
  stop_lane
  if [ "$keep" -eq 1 ]; then
    printf '%s: kept work dir %s\n' "$PROG" "$work" >&2
  else
    rm -rf -- "$work" 2>/dev/null || true
  fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

printf '%s: tip %s (%s)\n' "$PROG" "$tip" "$tip_subject"

# --- step 1: the lane binary ------------------------------------------------
# Stage the COMMITTED tree (git archive: tracked files only, so no .git, no
# node_modules, no stale dist - and the checkout's api/static is never
# touched). Then: bundle (default) or keep the committed api/static, then
# `go build ./cmd/omega` FROM THE STAGE - go:embed captures static/ at compile
# time of the tree you build in, so a build in the checkout would silently bake
# the OLD bundle while api/static looked fresh.
git archive HEAD | tar -x -C "$stage" 2>/dev/null ||
  die2 "cannot stage the committed tree (git archive HEAD | tar -x)"

# bundle_hash <dir>: content hash of a served bundle with nuxt's volatile build
# metadata (buildId uuid, epoch-ms timestamps) stripped - the same rule as
# scripts/embed-lane.sh's strip_volatile, copied rather than sourced because
# embed-lane.sh sets `set -e` at file scope and this script handles exit codes
# itself.
bundle_hash() {
  local f rel
  while IFS= read -r -d '' f; do
    rel="${f#"$1"/}"
    printf '%s\n' "$(printf '%s' "$rel" | sed -E 's/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/BUILDID/g')"
    sed -E 's/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/BUILDID/g; s/17[0-9]{11}/EPOCHMS/g' "$f" |
      sha256sum
  done < <(find "$1" -type f -print0 2>/dev/null | LC_ALL=C sort -z) | sha256sum
}

committed_bundle=""
[ -d "$stage/api/static" ] && committed_bundle="$(bundle_hash "$stage/api/static")"

default_build() {
  local webui="$root/webui"
  if [ "$skip_bundle" != 1 ]; then
    # Side effect, stated: `nuxt generate` writes webui/.nuxt and webui/dist IN
    # THE CHECKOUT (both gitignored build output). Only api/static is off limits
    # (scripts/embed-lane.sh owns it), and this never touches it.
    if [ ! -f "$webui/node_modules/.package-lock.json" ]; then
      printf '%s: npm ci (webui/node_modules absent or empty)\n' "$PROG" >&2
      (cd "$webui" && npm ci --no-audit --no-fund) || return 1
    fi
    # generate, not build: ssr:false build emits a nitro server, generate emits
    # the static SPA the go binary embeds. Invoke nuxt's own bin (a non-tty
    # shell reds `npx nuxt generate` with "stdin is not a tty"); fall back to
    # npx only when the direct path is absent.
    if [ -f "$webui/node_modules/nuxt/bin/nuxt.mjs" ]; then
      (cd "$webui" && node node_modules/nuxt/bin/nuxt.mjs generate) || return 1
    else
      (cd "$webui" && npx nuxt generate) || return 1
    fi
    [ -d "$webui/dist" ] || return 1
    rm -rf -- "$stage/api/static" && cp -r -- "$webui/dist" "$stage/api/static" || return 1
  fi
  (cd "$stage" && go build -o "$(winpath "$bin")" ./cmd/omega) || return 1
}

printf '%s: building the lane (%s)\n' "$PROG" \
  "$([ "$skip_bundle" = 1 ] && echo 'committed api/static' || echo 'regenerating the SPA bundle')" >&2
if [ -n "${PREFLIGHT_BUILD_CMD:-}" ]; then
  PREFLIGHT_TREE="$stage" PREFLIGHT_BIN="$bin" PREFLIGHT_REPO="$root" \
    bash -c "$PREFLIGHT_BUILD_CMD" >&2
  build_rc=$?
else
  default_build >&2
  build_rc=$?
fi
[ "$build_rc" -eq 0 ] || die2 "the lane build failed (exit $build_rc) - fix the build, nothing was asserted"
[ -x "$bin" ] || die2 "the build left no executable at $bin"

# A tip whose webui source and committed api/static disagree is a lane-commit
# gap: this run just tested a bundle that CI will NOT serve (the nightly builds
# the committed one). Not a red - the nightly will say - but never silent.
if [ -n "$committed_bundle" ] && [ "$skip_bundle" != 1 ]; then
  fresh_bundle="$(bundle_hash "$stage/api/static")"
  if [ "$fresh_bundle" != "$committed_bundle" ]; then
    printf '%s: WARNING the regenerated bundle differs from the committed api/static at %s - CI will serve the committed one (rerun with --skip-bundle to test exactly that)\n' \
      "$PROG" "$tip" >&2
  fi
fi

# --- step 2: lane ------------------------------------------------------------
port_listening() {
  if command -v netstat >/dev/null 2>&1; then
    grep -qE "[:.]$1[[:space:]].*LISTEN" <<<"$(MSYS_NO_PATHCONV=1 netstat -ano 2>/dev/null)"
  else
    curl -s -m 1 -o /dev/null "http://127.0.0.1:$1/" 2>/dev/null
  fi
}

pick_port() { # $1 first, $2 last. Empty when none is free.
  local p="$1"
  while [ "$p" -le "$2" ]; do
    if ! port_listening "$p"; then
      printf '%s\n' "$p"
      return 0
    fi
    p=$((p + 1))
  done
  return 1
}

if [ -z "$port" ]; then
  port="$(pick_port 18200 18219)" || die2 "no free lane port in 18200..18219 (pass --port <n>; nothing was killed to make room)"
  case "$port" in 8098 | 8099) die2 "port scan landed on never-kill $port" ;; esac
else
  port_listening "$port" &&
    die2 "port $port is already listening (that process is not mine to kill - pass another --port)"
fi
chrome_port="${PREFLIGHT_CHROME_PORT:-}"
# The suite LAUNCHES a chromium on this port (an explicit PREFLIGHT_CHROME_PORT
# means "attach to the live one already there"), so a busy port is not an
# alternative - a fallback picked past the scan would have the suite attach to a
# browser this script never started. Widen the scan, then refuse.
if [ -z "$chrome_port" ]; then
  chrome_port="$(pick_port 9300 9339)" ||
    die2 "no free CDP port in 9300..9339 for the suite's chromium (pass PREFLIGHT_CHROME_PORT=<port> to attach a live one)"
fi

base_url="http://127.0.0.1:$port"

# The nightly's lane home, verbatim: a minimal config.yaml and a dummy ollama
# provider that is never reachable ON PURPOSE (boot validates the wire, /health
# never calls the provider). No trust.yaml: the automation lane, like CI.
cat >"$lane_home/config/config.yaml" <<'LANE_CONFIG'
provider:
  type: ollama
  model_name: dummy
  host: http://127.0.0.1:9
LANE_CONFIG

printf '%s: booting the lane on %s\n' "$PROG" "$base_url" >&2
# The session db sits OUTSIDE the lane home, exactly like the nightly's
# $RUNNER_TEMP/nightly-lane.db next to omega-lane/. Inside the home at the root
# it reads as a LEGACY datadir: the first `omega kanban ...` CLI call then runs
# the omega.db -> data/omega.db migration while the serve holds that file, which
# on windows fails and falls through to a bogus project-path chdir
# ("chdir kanban: ...") - redding recipe 98's board seed for a pure environment
# reason (proven live 2026-11-17: rc=1 at t+0, rc=0 after the migration).
OMEGA_MODEL=dummy OMEGA_HOST=http://127.0.0.1:9 OMEGA_PORT="$port" \
  OMEGA_DB_PATH="$(winpath "$work/lane.db")" OMEGA_HOME="$(winpath "$lane_home")" \
  "$bin" serve >"$lane_log" 2>&1 &
lane_pid=$!

lane_wait="${PREFLIGHT_LANE_WAIT:-60}"
PREFLIGHT_HEALTH_URL="$base_url/health"
export PREFLIGHT_HEALTH_URL
health_ok() {
  local out
  if [ -n "${PREFLIGHT_HEALTH_CMD:-}" ]; then
    out="$(bash -c "$PREFLIGHT_HEALTH_CMD" 2>/dev/null || true)"
  else
    out="$(curl -s -m 5 "$PREFLIGHT_HEALTH_URL" 2>/dev/null || true)"
  fi
  grep -q '"ok"' <<<"$out"
}

healthy=0
i=0
while [ "$i" -lt $((lane_wait * 2)) ]; do
  # Liveness FIRST: a lane that already exited must not cost a probe at all (a
  # refused connect burns the curl budget on this host - ~2s - and a corpse
  # would spend the whole wait on nothing).
  lane_alive || break
  if health_ok; then
    healthy=1
    break
  fi
  sleep 0.5
  i=$((i + 1))
done
if [ "$healthy" != 1 ]; then
  printf '%s: the lane never answered /health on %s\n' "$PROG" "$base_url" >&2
  printf '%s: --- lane log (%s) ---\n' "$PROG" "$lane_log" >&2
  tail -15 "$lane_log" 2>/dev/null | sed 's/^/  | /' >&2
  stop_lane
  die2 "lane will not start (infra: nothing about the product was asserted)"
fi

# --- step 3: the suite -------------------------------------------------------
session="omega-preflight-$(date +%s)-$$"
journey_args=""
[ "$regression" -eq 1 ] && journey_args="--regression"

# The suite's output goes to a FILE, never through a pipe into tee. run.sh
# leaves a per-recipe wall-cap watchdog (`sleep $JOURNEY_TIMEOUT`) alive when it
# kills the subshell that owns it, and that orphan inherits the caller's stdout -
# a piped caller then waits on tee for a write end that the sleep holds, i.e.
# the pre-flight hangs for up to JOURNEY_TIMEOUT seconds AFTER a finished suite
# (measured live: 4 orphaned `sleep 300` held the pipe). A file fd cannot be
# held that way. The path is printed up front, so a human tails it.
printf '%s: running the journey suite (agent-browser %s, session %s)\n' "$PROG" "$ab_version" "$session" >&2
printf '%s: suite log %s\n' "$PROG" "$suite_log" >&2
# The pinned CLI is the one PATH resolves for the suite too: run.sh picks its
# AB by `command -v agent-browser`, so a PREFLIGHT_AB override must reach it.
if [ -n "${PREFLIGHT_AB:-}" ]; then
  PATH="$(cd "$(dirname "$ab")" && pwd):$PATH"
fi

suite_rc=0
if [ -n "${PREFLIGHT_JOURNEYS_CMD:-}" ]; then
  PREFLIGHT_EVIDENCE="$evid" PREFLIGHT_SUITE_LOG="$suite_log" PREFLIGHT_LANE_URL="$base_url" \
    PREFLIGHT_LANE_HOME="$lane_home" PREFLIGHT_BIN="$bin" \
    bash -c "$PREFLIGHT_JOURNEYS_CMD" >"$suite_log" 2>&1
  suite_rc=$?
else
  JOURNEY_BASE_URL="$base_url" \
    JOURNEY_AB_VERSION="$ab_version" \
    JOURNEY_AB_REQUIRE_VERSION=1 \
    JOURNEY_SESSION="$session" \
    JOURNEY_CHROME_PORT="$chrome_port" \
    JOURNEY_LOCK_WAIT="${PREFLIGHT_LOCK_WAIT:-0}" \
    OMEGA_HOME="$(winpath "$lane_home")" \
    OMEGA_BIN="$(winpath "$bin")" \
    bash "$root/scripts/journeys/run.sh" $journey_args >"$suite_log" 2>&1
  suite_rc=$?
fi

stopped_pid="$lane_pid"
stop_lane
printf '%s: lane stopped (pid %s, TERM by recorded pid only - nothing else was signalled)\n' "$PROG" "${stopped_pid:-none}" >&2

# --- step 4: the verdict -----------------------------------------------------
# run.sh mktemps its OWN evidence dir per run and names it in the suite output;
# that is where the recipe logs are, so the assertion lines come from there. The
# dir this script created is the fallback (the stub seam writes into it). A red
# whose log cannot be found still prints - as "no log" - because silence there
# would read as green.
suite_evid="$(sed -n 's/^journeys: evidence in \(.*\)$/\1/p' "$suite_log" 2>/dev/null | tail -1)"
if [ -z "$suite_evid" ] || [ ! -d "$suite_evid" ]; then
  suite_evid=""
fi
evid_used="${suite_evid:-$evid}"
print_reds "$suite_log" "$evid_used"

case "$suite_rc" in
  0 | 1) : ;;
  3)
    die2 "another journey suite holds the run lock - nothing ran (queue by setting PREFLIGHT_LOCK_WAIT to the seconds to wait, e.g. 1)"
    ;;
  *)
    die2 "the journey suite exited $suite_rc (lane/browser infra, not a product verdict)"
    ;;
esac

if [ "$RED_N" -gt 0 ]; then
  printf '%s: suite log %s\n' "$PROG" "$suite_log" >&2
  printf '%s: evidence %s\n' "$PROG" "$evid_used" >&2
  printf '%s: %s red - dispatch blocked (local discovery; the nightly stays certification)\n' "$PROG" "$RED_N"
  exit 1
fi

# A suite that says RED but names nothing would exit 0 here - a false green, the
# one outcome this script must never produce. Stop loudly instead.
[ "$suite_rc" -ne 1 ] ||
  die2 "the journey suite reported red but named no journey (run.sh exit 1, no id in its output)"

# ... and so would a suite that exits 0 while JUDGING nothing: run.sh emptied by
# a bad merge, a suite that bailed before its first recipe, a stub seam with no
# work. That is the same discovery lie the format-only guard above refuses ("a
# formatter that reads nothing to judge as nothing wrong"), in its live shape -
# and here it would let a driver dispatch a nightly believing the tip was
# pre-flighted. A result line (`journeys: <id>  green|RED`) or a recipe log is
# the evidence that something was asserted; absent both, nothing was.
judged=0
if [ -f "$suite_log" ] &&
  grep -q . <<<"$(sed -n -E 's/^journeys: ([^ ]+) +(green|RED)$/\1/p' "$suite_log" 2>/dev/null)"; then
  judged=1
else
  for f in "$evid_used"/*.log; do
    [ -f "$f" ] && judged=1 && break
  done
fi
[ "$judged" -eq 1 ] ||
  die2 "the journey suite exited 0 but judged no recipe (no result line in the suite log, no log in $evid_used - nothing to judge)"

printf '%s: 0 red - green locally (DISCOVERY ONLY: CI layout can differ by a small offset, and only a green nightly certifies a cut)\n' "$PROG"
exit 0
