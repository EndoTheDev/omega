#!/bin/bash
# run.sh - the validation journeys suite (customer-level browser checks).
#
# Rung 2 of the detection ladder: gates catch code-level breaks, these
# catch the invisible-to-gates family ("correct data, lying presentation"
# - the auto-focus that shipped 100% dead, the poisoned bundle, the NUXT
# ReferenceError). train-pass.sh runs this post-landing; nightly.yml runs
# it pre-tag. Red journey = red landing, same as a red gate.
#
# Usage:
#   scripts/journeys/run.sh                    # full suite vs $JOURNEY_BASE_URL
#   scripts/journeys/run.sh --base <url>       # other lane (overrides
#                                              # $JOURNEY_BASE_URL)
#   scripts/journeys/run.sh <journey-name>...  # subset (e.g. 20-chat-send-response)
#   scripts/journeys/run.sh --regression       # ONLY the named regression set
#                                              # (regression.list - the
#                                              # incidents-as-eval set)
#   JOURNEY_JSON=1 run.sh                      # machine summary on the final line
#   JOURNEY_LOCK_WAIT=600 run.sh               # queue behind a live suite
#                                              # instead of refusing
#   JOURNEY_LOCK_IMPL=auto|flock|mkdir run.sh  # pin the lock implementation
#                                              # (auto = flock where the host has
#                                              # it, the mkdir dir otherwise; the
#                                              # witness pins mkdir to exercise
#                                              # the MSYS shape anywhere)
#
# Every argument is accounted for: an unknown option, a --base with no URL,
# or a journey name no recipe matches is a loud exit 2. Nothing is dropped
# silently - a suite that quietly falls back to the default lane is a green
# about the wrong binary (card t_2a9be842).
#
# Exit codes: 0 all green, 1 at least one journey red, 2 lane/browser
# infra dead (nothing about the product is asserted in that state),
# 3 another suite holds the run lock (refused, nothing ran).
#
# Two lanes (amendment, Endo 2026-10-21):
#   - Pi per-landing: run.sh launches the Hermes-bundled headless
#     Chromium (no system chromium on the Pi, deliberate) and drives it
#     through the agent-browser CLI. The nss/nspr libs ride LD_LIBRARY_PATH
#     (assembled chromium-libs dir), never a system package.
#   - CI nightly: agent-browser auto-launches its own Chrome - install
#     once with `npm i -g agent-browser@0.42.0 && agent-browser install`
#     (the version is PINNED - see the agent-browser version pin block
#     below), run the same suite against the built artifact.
#
# Every write-journey creates a throwaway session (id prefix jrn-, label
# prefix "(journey)") and deletes it on exit; the suite leaves no
# residue in the live store. The residue contract (card t_a99ffcd7)
# extends the same law to every runtime actor a journey touches:
# board tasks (DELETE, not archive) and roster agent homes
# ($OMEGA_HOME/agents/jrn-*) are reaped at suite start, at every
# recipe exit and on every die; wake entries in cronstate.json are
# the serve's own state - the cron runner's GC sweeps them once the
# declaring agent home is gone. A journey keeps only its recipe and
# its result files (this suite's evidence dir).
#
# Chrome-death contract (proven live 2026-10-27: 37 orphaned chromes
# held a Pi 5 at load 10 and red-flaked every suite run):
#   normal exit, die(), INT (Ctrl-C)   -> the EXIT trap runs stop_chrome,
#                                        which group-kills the chrome we
#                                        launched (setsid makes it its
#                                        own process group) plus a belt
#                                        scan for stragglers wearing
#                                        this run's profile dir.
#   TERM/INT raised by a kill          -> trap re-raises via exit, which
#                                        runs the same EXIT trap.
#   SIGKILL on run.sh, crash, OOM      -> nothing can trap. sweep-chromes.sh
#                                        is the backstop: run.sh reaps
#                                        orphans at every suite start,
#                                        and the script is safe to run
#                                        standalone (cron, pre-merge).
# A chrome this run ATTACHED to (already listening when we started) is
# never ours: CHROME_PID stays empty and every kill path no-ops.
#
# Daemon contract (card t_6485e22e): killing the chrome is not enough. The
# agent-browser DAEMON outlives its client by design (that is how a session
# stays warm), and every session name ever used leaves one daemon holding
# one auto-launched chrome that sweep-chromes.sh correctly refuses to touch
# (the chrome's parent - the daemon - is alive). So the RELEASE GATE closes
# idle daemons: sweep-daemons.sh runs after the last recipe, while this
# suite still holds the run lock, so no other suite (the only thing that
# drives a session to a schedule) can be mid-flight. It never runs at suite
# start - a concurrent worker's session is live then.

set -u

# --- suite run lock ----------------------------------------------------------
# A journey suite is not concurrency-safe: two suites running at once share
# the agent-browser daemon's global connection, the CDP port and the lane,
# and interleave into mutual false reds (proven 2026-10-28: a worker verify
# during the v0.13.0 gate red-flaked BOTH sides - empty session lists,
# board-switcher timeouts, hangs). flock the global run lock on fd 9 for the
# whole suite body: the kernel holds/releases it, so every exit path (normal,
# die(), INT/TERM, SIGKILL, OOM) frees it without an explicit unlock. A second
# concurrent suite refuses with a distinct message instead of interleaving.
# Waiting is opt-in via env: worker verifies and the release gate run
# JOURNEY_LOCK_WAIT=600 so they queue behind a live suite instead of
# colliding with it (default 0 = refuse immediately). fd 9 is inherited by
# sweep-chromes.sh, which reads it as "called from the live suite"; a
# standalone sweep that cannot take the lock stands down. Every OTHER
# background child launches with 9>&-: the resident agent-browser daemon,
# the chrome, the recipe subshells and the watchdogs all outlive the suite
# sometimes, and a single leaked lock fd keeps the flock alive after
# run.sh exits - every later suite would refuse forever (proven live
# 2026-10-28, fixed the same day).
#
# OWNERSHIP IS NOT A PID (card t_804ab59c). The MSYS path could only ask
# `kill -0`, and a pid is a number, not an identity: a dead suite's record
# whose number Windows had since handed to an unrelated process refused every
# later suite with "another journey suite holds the run lock (pid N)" while no
# suite was running (hit live 2026-11-17, worked around with
# JOURNEY_LOCK=/tmp/omega-journeys-tfdd12843.lock). The record is now checked
# against the holder's own command line - lock_holder_is_suite below - so a
# recycled number is reaped while a genuine holder is still refused.
JOURNEY_LOCK="${JOURNEY_LOCK:-/tmp/omega-journeys.lock}"
export JOURNEY_LOCK
JOURNEY_LOCK_WAIT="${JOURNEY_LOCK_WAIT:-0}"
journey_lock_refuse() { # $1: holder pid text; callers pass 'cat-or-?'
  printf 'journeys: another journey suite holds the run lock (pid %s) - refusing to interleave\n' "$1" >&2
  printf '  queue behind it instead: JOURNEY_LOCK_WAIT=<secs> run.sh ...\n' >&2
  exit 3
}

# lock_holder_args <pid>: the holder's argv on one line, empty when the pid
# exposes none. /proc is the honest read (Linux always, MSYS for the processes
# its own runtime started); `ps -o args=` is the fallback. MSYS's ps has no -o
# and reports only the executable path, so a ps answer there confirms "some
# process", never "a suite" - which is the safe direction (see below).
lock_holder_args() {
  local args=""
  args="$(tr '\0' ' ' < "/proc/$1/cmdline" 2>/dev/null | tr -d '\r\n')"
  if [ -z "$args" ]; then
    args="$(ps -p "$1" -o args= 2>/dev/null | tr -d '\r\n' | sed 's/^ *//; s/ *$//')"
  fi
  printf '%s' "$args"
}

# lock_holder_is_suite <pid> -> 0 a live journey suite, 1 nothing that can be
# one (dead, or a recycled number), 2 alive but unverifiable.
# A suite is bash running this script, so its argv names .../journeys/run.sh.
# Invoked as ./run.sh from the suite home the argv carries only the bare
# script name, so the holder's cwd is read as the second signal - reaping a
# LIVE suite's lock would let two suites interleave, the exact failure this
# lock exists to prevent, so one signal alone is not enough to convict.
lock_holder_is_suite() {
  local pid="$1" args cwd
  case "$pid" in ''|*[!0-9]*) return 2 ;; esac   # empty/junk record: unproven
  kill -0 "$pid" 2>/dev/null || return 1
  args="$(lock_holder_args "$pid")"
  if [ -z "$args" ]; then
    # Alive but undescribed. A host WITH /proc that does not know this pid is
    # naming a process outside the shell runtime (a native Windows process on
    # MSYS) - never a bash suite, so the number was recycled. Everything else
    # (a pid /proc will not describe, or a host with no /proc to ask) cannot
    # be answered in either direction and stays unproven: refuse, never reap.
    if [ -d /proc ] && [ ! -d "/proc/$pid" ]; then return 1; fi
    return 2
  fi
  case "$args" in
    *journeys/run.sh*|*journeys\\run.sh*) return 0 ;;
  esac
  case "$args" in
    *run.sh*)
      cwd="$(readlink "/proc/$pid/cwd" 2>/dev/null || true)"
      cwd="${cwd//\\//}"
      case "$cwd" in */scripts/journeys|*/scripts/journeys/) return 0 ;; esac ;;
  esac
  return 1
}

# journey_lock_reapable <recorded pid> -> 0 that record names no live journey
# suite, so the dir it left is stale. Unproven is NOT reapable.
journey_lock_reapable() {
  lock_holder_is_suite "$1"
  case "$?" in
    0|2) return 1 ;;
  esac
  return 0
}

JOURNEY_LOCK_IMPL="${JOURNEY_LOCK_IMPL:-auto}"
case "$JOURNEY_LOCK_IMPL" in
  auto)
    if command -v flock >/dev/null 2>&1; then JOURNEY_LOCK_IMPL=flock; else JOURNEY_LOCK_IMPL=mkdir; fi ;;
  flock|mkdir) ;;
  *)
    printf 'journeys: JOURNEY_LOCK_IMPL must be auto, flock or mkdir (got %s)\n' "$JOURNEY_LOCK_IMPL" >&2
    exit 2 ;;
esac
if [ "$JOURNEY_LOCK_IMPL" = flock ] && ! command -v flock >/dev/null 2>&1; then
  printf 'journeys: JOURNEY_LOCK_IMPL=flock, but this host has no flock(1)\n' >&2
  exit 2
fi
if [ "$JOURNEY_LOCK_IMPL" = flock ]; then
  exec 9>"$JOURNEY_LOCK" || { echo "journeys: cannot open $JOURNEY_LOCK" >&2; exit 2; }
  if ! flock -w "$JOURNEY_LOCK_WAIT" 9; then
    journey_lock_refuse "$(cat "${JOURNEY_LOCK}.pid" 2>/dev/null || echo '?')"
  fi
  printf '%s\n' "$$" > "${JOURNEY_LOCK}.pid"
  journey_lock_release() { :; }
else
  # No flock(1) (git-bash/MSYS on winbtw): same lock as an atomic mkdir dir
  # next to the flock file, pid file inside. mkdir(2) is atomic system-wide;
  # a dir whose owner pid is dead is stale and gets reaped - the SIGKILL
  # story a kernel flock gets for free. A dir whose owner pid is ALIVE but is
  # not a journey suite is stale too (a recycled number, card t_804ab59c); a
  # dir whose owner cannot be identified is held, never reaped. Release
  # removes the dir on every trappable exit (traps set further down); no
  # trappable exit leaves a dir the NEXT suite's staleness check reaps. /tmp
  # is shared across every MSYS shell on the host, same as the flock file.
  # fd 9 does not exist in this mode; sweep-chromes.sh detects MSYS and hands
  # the reap to sweep-chromes.ps1, which runs regardless of the lock - a
  # chrome whose parent is dead cannot belong to a live suite.
  JOURNEY_LOCK_DIR="$JOURNEY_LOCK.d"
  journey_lock_try() {
    if mkdir "$JOURNEY_LOCK_DIR" 2>/dev/null; then
      printf '%s\n' "$$" > "$JOURNEY_LOCK_DIR/pid"
      JOURNEY_LOCK_MKDIR=1
      return 0
    fi
    local holder
    holder="$(tr -d '\r\n' < "$JOURNEY_LOCK_DIR/pid" 2>/dev/null || echo '?')"
    if journey_lock_reapable "$holder"; then
      printf 'journeys: reaping a stale run lock - pid %s is not a live journey suite\n' "$holder" >&2
      rm -rf "$JOURNEY_LOCK_DIR" 2>/dev/null
      if mkdir "$JOURNEY_LOCK_DIR" 2>/dev/null; then
        printf '%s\n' "$$" > "$JOURNEY_LOCK_DIR/pid"
        JOURNEY_LOCK_MKDIR=1
        return 0
      fi
    fi
    JOURNEY_LOCK_LAST_HOLDER="$holder"
    return 1
  }
  waited=0
  if ! journey_lock_try; then
    if [ "$JOURNEY_LOCK_WAIT" -gt 0 ]; then
      while [ "$waited" -lt "$JOURNEY_LOCK_WAIT" ]; do
        sleep 1
        waited=$((waited + 1))
        journey_lock_try && break
      done
    fi
    if [ -z "${JOURNEY_LOCK_MKDIR:-}" ]; then
      journey_lock_refuse "${JOURNEY_LOCK_LAST_HOLDER:-?}"
    fi
  fi
  journey_lock_release() {
    [ -n "${JOURNEY_LOCK_MKDIR:-}" ] || return 0
    rm -rf "$JOURNEY_LOCK_DIR" 2>/dev/null
    return 0
  }
fi

JROOT="$(cd "$(dirname "$0")" && pwd)"
# lib_call <words>: run one lib.sh function in a throwaway shell. run.sh and the
# recipes share ONE implementation of harness policy (the port picker lives in
# lib.sh with the rest of it); run.sh itself never sources lib.sh, because the
# curl shim there would retry the suite's own lane probes.
lib_call() { bash -c ". '$JROOT/lib.sh' >/dev/null 2>&1; $*" 2>/dev/null; }
EVID="$(mktemp -d "${TMPDIR:-/tmp}/omega-journeys-evidence.XXXXXX")"

# backstop: reap chromes orphaned by dead earlier runs (SIGKILLed run.sh,
# dead worker debug sessions) before anything here can attach to them
bash "$JROOT/sweep-chromes.sh" -q 2>/dev/null || true

# a dead run's SEEDED THEME is residue too (mechanism: the active theme outlives
# the run). The residue contract reaps jrn- sessions, board cards and roster
# homes; a theme file the crash left behind is not reaped by anything, and the
# lane keeps it ACTIVE - so the next run's theme journey reads the dead run's id
# back out of the pane after a pick that landed, and reds on it. This runs inside
# the run lock, so every jrn-theme-* file present belongs to a run that is gone.
if [ -n "${OMEGA_HOME:-}" ] && [ -d "$OMEGA_HOME/themes" ]; then
  for f in "$OMEGA_HOME"/themes/jrn-theme-*; do
    case "$f" in
      */themes/jrn-theme-*)
        [ -f "$f" ] || continue
        rm -f "$f" && printf 'journeys: reaped a dead run theme %s\n' "${f##*/}"
        ;;
    esac
  done
fi

# --- args --------------------------------------------------------------------
# --base <url> IS the documented way to point the suite at a lane other than
# the dev default, and it must be parsed HERE: the subset filter below keeps
# only recognized journey names, so an unparsed flag used to be dropped in
# silence and the suite then ran against :8098 - green about whatever binary
# happened to be serving the dev lane (card t_2a9be842, reproduced live).
# The same trap catches a stray URL or a mistyped flag, so an argument this
# script does not understand exits 2 by name instead of being ignored.
journey_usage_die() { # $1: what was wrong
  printf 'journeys: %s\n' "$1" >&2
  printf '  usage: run.sh [--base <url>] [--regression] [<journey-name>...]\n' >&2
  journey_lock_release
  exit 2
}
JOURNEY_BASE_FLAG=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --base)
      case "${2:-}" in
        ""|-*) journey_usage_die "--base needs a URL (got '${2:-}')" ;;
      esac
      [ -n "$JOURNEY_BASE_FLAG" ] && journey_usage_die "--base given twice (${JOURNEY_BASE_FLAG} then $2)"
      JOURNEY_BASE_FLAG="$2"
      shift 2 ;;
    --base=*)
      case "${1#--base=}" in
        ""|-*) journey_usage_die "--base needs a URL (got '${1#--base=}')" ;;
      esac
      [ -n "$JOURNEY_BASE_FLAG" ] && journey_usage_die "--base given twice (${JOURNEY_BASE_FLAG} then ${1#--base=})"
      JOURNEY_BASE_FLAG="${1#--base=}"
      shift ;;
    --regression) break ;;   # handled by the pick-journeys section below
    -*) journey_usage_die "unknown option: $1" ;;
    *) break ;;              # first journey name: everything after is names
  esac
done

# --- env -------------------------------------------------------------------
JOURNEY_BASE_URL="${JOURNEY_BASE_FLAG:-${JOURNEY_BASE_URL:-http://127.0.0.1:8098}}"
# whether the CALLER named the CDP port. A named port is adopted (a pre-launched
# browser, a pinned lane); the default belongs to this run alone (see the attach
# block: mechanism "one CDP port for the host").
JOURNEY_CHROME_PORT_PINNED=0
if [ -n "${JOURNEY_CHROME_PORT:-}" ]; then JOURNEY_CHROME_PORT_PINNED=1; fi
JOURNEY_CHROME_PORT="${JOURNEY_CHROME_PORT:-9333}"
JOURNEY_JSON="${JOURNEY_JSON:-0}"

# --- the suite clock (mechanism: budgets written on an idle host) ------------
# Every budget in this suite was measured solo, on an idle machine. The suite
# flakes under load because those numbers describe the idle host: measured
# 2026-11-20 on 12 burners beside this host, journey 114 went 23s -> 296s (its
# wall cap is 300s) and journey 113 was killed at 25 of ~40 steps by that cap
# after 76s idle - the "silent hang" and "toggle no-op" reds in the train are
# this one class. So the host's own load average against its core count becomes
# ONE scale for the whole run (100% = idle, capped at 400%), applied to the
# per-recipe wall cap here and, through the exported JOURNEY_CLOCK_PCT, to the
# browser call deadline and the event-wait floor in lib.sh. It is printed on
# every run, green or red, so a slow suite states its own budget instead of
# leaving the next reader to guess.
#
# The load can ARRIVE mid-suite (the merge train starts an SPA build beside a
# running suite), so the scale is re-sampled before every recipe and only ever
# ratchets UP: nothing already promised to a running recipe is taken back, and
# a recipe that starts on a heavier host gets the heavier budget.
JOURNEY_LOADAVG="${JOURNEY_LOADAVG:-/proc/loadavg}"
JOURNEY_CORES="$(nproc 2>/dev/null || getconf _NPROCESSORS_ONLN 2>/dev/null || echo 1)"
case "$JOURNEY_CORES" in ''|*[!0-9]*) JOURNEY_CORES=1 ;; esac
[ "$JOURNEY_CORES" -ge 1 ] || JOURNEY_CORES=1
JOURNEY_LOAD1=""
CLOCK_PCT=""
# clock_sample -> 0 when the host reports a load average (CLOCK_PCT set), 1 on
# a host without one. A global, not stdout: the caller needs JOURNEY_LOAD1 too.
clock_sample() {
  local l
  l="$(cut -d' ' -f1 "$JOURNEY_LOADAVG" 2>/dev/null | tr -d '\r')"
  case "$l" in ''|*[!0-9.]*) return 1 ;; esac
  JOURNEY_LOAD1="$l"
  CLOCK_PCT="$(awk -v l="$l" -v c="$JOURNEY_CORES" \
    'BEGIN { p = (l / c) * 100; if (p < 100) p = 100; if (p > 400) p = 400; printf "%d", p }')"
  case "$CLOCK_PCT" in ''|*[!0-9]*) return 1 ;; esac
  return 0
}
# clock_set <pct>: the ONE scale. The wall cap follows it unless the caller
# pinned JOURNEY_TIMEOUT; lib.sh's budgets follow the exported percentage.
JOURNEY_TIMEOUT_PINNED=0
[ -n "${JOURNEY_TIMEOUT:-}" ] && JOURNEY_TIMEOUT_PINNED=1
clock_set() {
  JOURNEY_CLOCK_PCT="$1"
  case "$JOURNEY_CLOCK_PCT" in ''|*[!0-9]*) JOURNEY_CLOCK_PCT=100 ;; esac
  [ "$JOURNEY_CLOCK_PCT" -ge 100 ] || JOURNEY_CLOCK_PCT=100
  export JOURNEY_CLOCK_PCT
  if [ "$JOURNEY_TIMEOUT_PINNED" = "0" ]; then
    JOURNEY_TIMEOUT=$((300 * JOURNEY_CLOCK_PCT / 100))
    export JOURNEY_TIMEOUT
  fi
}
# --- the wall stretch (mechanism: a load average is not a slowdown) ----------
# load1 counts RUNNABLE threads; it does not say what a second costs here.
# Measured 2026-11-20 on the train host, the one this flake family came from:
# load1 13.67 on 12 cpus read as 113%, while `sleep 0.2` took 0.34s - 171%. The
# budgets are spent in WALL seconds, and the scheduler stretches those far more
# than the queue length suggests, so the clock takes the LARGER of the two
# signals. The probe is a few timed sleeps; it runs once at suite start (median
# of three) and once before each recipe (one sample), and only a real STEP moves
# the scale - one delayed sleep is noise, not a budget. A host whose date has no
# nanoseconds, or JOURNEY_WALL_PROBE_MS=0, keeps the load-average signal alone.
JOURNEY_WALL_PROBE_MS="${JOURNEY_WALL_PROBE_MS:-200}"
case "$JOURNEY_WALL_PROBE_MS" in ''|*[!0-9]*) JOURNEY_WALL_PROBE_MS=200 ;; esac
JOURNEY_CLOCK_STEP="${JOURNEY_CLOCK_STEP:-25}"
case "$JOURNEY_CLOCK_STEP" in ''|*[!0-9]*) JOURNEY_CLOCK_STEP=25 ;; esac
WALL_PCT=""
WALL_SAMPLE_MS=""
# wall_probe_once -> 0 with WALL_SAMPLE_MS = one timed sleep, in milliseconds.
wall_probe_once() {
  local t0 t1
  t0="$(date +%s%N 2>/dev/null)"
  case "${t0:-}" in
    ''|*[!0-9]*) return 1 ;;
  esac
  [ "${#t0}" -ge 16 ] || return 1
  sleep "0.$(printf '%03d' "$JOURNEY_WALL_PROBE_MS")" 2>/dev/null || return 1
  t1="$(date +%s%N 2>/dev/null)"
  case "${t1:-}" in
    ''|*[!0-9]*) return 1 ;;
  esac
  WALL_SAMPLE_MS=$(( (t1 - t0) / 1000000 ))
  [ "$WALL_SAMPLE_MS" -gt 0 ] || return 1
  return 0
}
# wall_sample <n> -> 0 with WALL_PCT = the median stretch across n probes.
wall_sample() {
  local n="${1:-1}" i=0 ms pct samples=""
  WALL_PCT=""
  [ "$JOURNEY_WALL_PROBE_MS" -ge 50 ] || return 1
  while [ "$i" -lt "$n" ]; do
    wall_probe_once || return 1
    samples="$samples$WALL_SAMPLE_MS
"
    i=$((i + 1))
  done
  ms="$(printf '%s' "$samples" | grep -E '^[0-9]+$' | sort -n | sed -n "$(( (n + 1) / 2 ))p")"
  case "$ms" in ''|*[!0-9]*) return 1 ;; esac
  pct=$(( ms * 100 / JOURNEY_WALL_PROBE_MS ))
  [ "$pct" -lt 100 ] && pct=100
  [ "$pct" -gt 400 ] && pct=400
  WALL_PCT="$pct"
  return 0
}
# clock_refresh <samples>: read both signals, move the scale when the reading is
# a real step above it. Ratchets UP only - nothing already promised to a running
# recipe is taken back. -> 0 when the scale moved.
clock_refresh() {
  local cand
  # a global, not stdout: clock_sample sets CLOCK_PCT only on success, so a
  # host with no load average must read the running scale instead of ""
  if clock_sample; then cand="$CLOCK_PCT"; else cand="$JOURNEY_CLOCK_PCT"; fi
  if wall_sample "${1:-1}" && [ "$WALL_PCT" -gt "$cand" ]; then cand="$WALL_PCT"; fi
  case "$cand" in ''|*[!0-9]*) return 1 ;; esac
  [ $((cand - JOURNEY_CLOCK_PCT)) -ge "$JOURNEY_CLOCK_STEP" ] || return 1
  clock_set "$cand"
  return 0
}
clock_report() {
  local detail="no load average on this host"
  [ -n "$JOURNEY_LOAD1" ] && detail="host load $JOURNEY_LOAD1 on $JOURNEY_CORES cpu"
  [ -n "$WALL_PCT" ] && detail="$detail, wall stretch $WALL_PCT%"
  printf 'journeys: timing budget %s%% (%s) - wall cap %ss per recipe\n' \
    "$JOURNEY_CLOCK_PCT" "$detail" "$JOURNEY_TIMEOUT"
}
JOURNEY_CLOCK_PINNED=0
if [ -n "${JOURNEY_CLOCK_PCT:-}" ]; then
  JOURNEY_CLOCK_PINNED=1
  clock_set "$JOURNEY_CLOCK_PCT"
else
  clock_set 100
  clock_refresh 3 || true
fi
# the run's own identity: lib.sh keeps this run's tag record apart from every
# other run on the host (JRN_TAGFILE).
JOURNEY_RUN_ID="${JOURNEY_RUN_ID:-$$}"
export JOURNEY_RUN_ID
# the scratch path lib.sh uses, computed HERE so the run and every recipe agree
# on it (the live marker below must sit where the reaper looks, and MSYS needs
# the mixed C:/ form for native tools).
if command -v cygpath >/dev/null 2>&1; then
  JOURNEY_WORKTMP="$(cygpath -m "${TMPDIR:-/tmp}")"
else
  JOURNEY_WORKTMP="${TMPDIR:-/tmp}"
fi
export JOURNEY_WORKTMP
# the run's LIVE MARKER (mechanism: one lane, sibling runs). A jrn- artifact is
# reaped only when the run that planted it is gone; the marker is how a second
# run tells a sibling's work from a crash's leftover. A SIGKILLed run leaves it
# behind with a dead pid, which reads as gone - the next pre-clean reaps it.
JRN_LIVE_FILE="$JOURNEY_WORKTMP/omega-jrn-live-$JOURNEY_RUN_ID"
: >"$JRN_LIVE_FILE" 2>/dev/null || true
# the browser session name rides EVERY command (lib.sh ab) and the CDP
# connect: the daemon multiplexes concurrent clients, and the daemon's DEFAULT
# connection is global - without a session-scoped connect the LAST connect wins
# and both runs drive one chrome (mis-read DOM, foreign console noise, phantom
# reds). The default used to be one name for the whole host, which made every
# run on this machine a sibling of every other in the same daemon: a worker's
# targeted recipe run, a crashed suite's leftover daemon, and the gate itself
# shared one page, one console buffer and one localStorage. The name carries the
# RUN now, so "which browser did this run drive" has exactly one answer; a
# caller that wants to share a session (a repro driving an existing daemon)
# still exports JOURNEY_SESSION and wins.
JOURNEY_SESSION="${JOURNEY_SESSION:-omega-journey-$JOURNEY_RUN_ID}"
export JOURNEY_SESSION

# --- agent-browser version pin (card t_a1fe9f84) -----------------------------
# agent-browser is a GATE DEPENDENCY, not an incidental tool: the
# gesture-class recipes (84 divider drag, 98 board drag) drive real pointer
# input through it, and nothing a recipe does through eval can substitute
# for that. A floating version therefore silently changes what the gate
# measures. The receipt: nightlies 43795957657 and 43797271512 (2026-11-16)
# came back with IDENTICAL gesture reds twice - 84 "right divider inward:
# got [220] want [160] | gesture-active=0" and 98 "mid-drag order for
# triage never matched in 6s | dragging=[]" - while the same tree was green
# on 2026-11-12 (run 43210278632) under 0.42.0. The runner installed
# unpinned, so the suite silently moved to 0.43.0 and its drag input
# synthesis stopped engaging at all.
#
# 0.42.0 is the version the recipes are written and green against. 0.43.0's
# gesture path is the known break; adapting the recipes to it is a separate
# decision, not this pin. nightly.yml installs agent-browser@0.42.0 and sets
# JOURNEY_AB_REQUIRE_VERSION=1; both numbers move together or not at all.
# A non-CI host may legitimately differ (the Pi lane carries 0.26.0), so a
# mismatch is a loud warning by default and a refusal only when the caller
# says the version IS the gate.
JOURNEY_AB_VERSION="${JOURNEY_AB_VERSION:-0.42.0}"
JOURNEY_AB_REQUIRE_VERSION="${JOURNEY_AB_REQUIRE_VERSION:-0}"

# --- agent-browser binary ----------------------------------------------------
AB="$(command -v agent-browser 2>/dev/null | head -1)"
if [ -z "$AB" ]; then
  AB="$(find "$HOME/.hermes/tools" -maxdepth 5 -type f \
     \( -name 'agent-browser-linux-arm64' -o -name 'agent-browser-linux-x64' \
        -o -name 'agent-browser-win32-x64.exe' -o -name 'agent-browser-win32-arm64.exe' \) 2>/dev/null | head -1)"
fi
if [ -z "$AB" ]; then
  # extensionless shim class: a tiny wrapper script (e.g. win hosts keep
  # ~/.hermes/tools/journeywinbin/agent-browser, a bash exec into the
  # .exe) that PATH did not carry when this suite started.
  AB="$(find "$HOME/.hermes/tools" -maxdepth 3 -type f -name 'agent-browser' 2>/dev/null | head -1)"
fi
if [ -z "$AB" ]; then
  echo "journeys: agent-browser CLI not found (PATH or ~/.hermes/tools)" >&2
  echo "  install: npm i -g agent-browser@$JOURNEY_AB_VERSION && agent-browser install" >&2
  exit 2
fi

# version gate: parse "agent-browser X.Y.Z" out of --version. An unreadable
# version is not silently a pass - it prints as "unknown" and takes the same
# mismatch path, so a wrapped shim that cannot report itself shows up in the
# log instead of looking pinned.
ab_version="$("$AB" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)"
if [ "$ab_version" != "$JOURNEY_AB_VERSION" ]; then
  printf 'journeys: agent-browser %s, PINNED %s (the 0.43.0 gesture break is why the pin exists)\n' \
    "${ab_version:-unknown}" "$JOURNEY_AB_VERSION" >&2
  if [ "$JOURNEY_AB_REQUIRE_VERSION" = "1" ]; then
    printf 'journeys: JOURNEY_AB_REQUIRE_VERSION=1 - refusing to run the gate on an unpinned browser\n' >&2
    journey_lock_release
    exit 2
  fi
fi

# --- the timing budget, stated on every run --------------------------------
clock_report
printf 'journeys: browser session %s (cdp port %s)\n' "$JOURNEY_SESSION" "$JOURNEY_CHROME_PORT"
if ! command -v timeout >/dev/null 2>&1; then
  printf 'journeys: no timeout(1) on this host - a wedged agent-browser call is bounded only by the wall cap\n' >&2
fi

# --- chrome launch (Pi lane) -------------------------------------------------
# The Hermes-bundled Chromium loads its nss/nspr libs from the lib path
# (deliberate on the Pi: no system chromium). The suite auto-detects an
# assembled chromium-libs dir; a fresh host sets JOURNEY_CHROME_LIBS.
launch_chrome() {
  local pattern="${JOURNEY_CHROME_BIN:-$HOME/.hermes/tools/chromium-*/chrome-linux/chrome.real}"
  local BINPATH=""
  for cand in $pattern; do
    if [ -x "$cand" ]; then BINPATH="$cand"; break; fi
  done
  [ -n "$BINPATH" ] || return 1
  local LIBS
  if [ -n "${JOURNEY_CHROME_LIBS:-}" ]; then
    LIBS="$JOURNEY_CHROME_LIBS"
  else
    LIBS="$(find "$HOME/.hermes/profiles" -maxdepth 2 -type d -name 'chromium-libs' 2>/dev/null | head -1)"
  fi
  [ -n "$LIBS" ] || LIBS="$(dirname "$BINPATH")"
  # per-port profile: two concurrent suite runs launch two chromiums -
  # one shared user-data-dir would collide (chromium refuses a second
  # instance on a locked profile, or the reset wipes a running one)
  local UD="${TMPDIR:-/tmp}/omega-journeys-chrome-profile-$JOURNEY_CHROME_PORT"
  if [ "${1:-}" = "reset" ]; then rm -rf "$UD"; fi
  mkdir -p "$UD"
  # setsid puts chrome (and its render children) in a process group of
  # its own: the group kill in stop_chrome then takes the whole tree
  # without touching this script's own group. 9>&-: chrome may outlive
  # a SIGKILLed run.sh - it must never keep the run-lock fd alive.
  setsid env \
    LD_LIBRARY_PATH="$LIBS:$(dirname "$BINPATH")" \
    "$BINPATH" \
    --headless=new --remote-debugging-port="$JOURNEY_CHROME_PORT" \
    --user-data-dir="$UD" --no-first-run --no-default-browser-check \
    --disable-gpu --window-size=1280,720 --noerrdialogs \
    --use-mock-keychain --password-store=basic \
    9>&- >/dev/null 2>&1 &
  CHROME_PID=$!
  CHROME_UD="$UD"
  local i=0
  while [ "$i" -lt 20 ]; do
    if curl -s "http://127.0.0.1:$JOURNEY_CHROME_PORT/json/version" 2>/dev/null | grep -q Chrome; then
      return 0
    fi
    sleep 0.5
    i=$((i + 1))
  done
  return 1
}

# chrome_procs_matching <profile-dir>: print pids of chrome.real MAIN
# processes (the launcher, not --type= render children) whose cmdline
# names that profile dir. The /proc comm-name scan is the safe pattern:
# pgrep -f self-matches the invoking shell (proven twice 2026-10-27,
# one self-inflicted SIGKILL) - filter by /proc/*/comm, never by
# pattern text. comm "chrome.real" contains no spaces, so stat field 4
# (ppid) is unshifted.
chrome_procs_matching() {
  local want="$1" p pid comm
  for p in /proc/[0-9]*; do
    pid="${p#/proc/}"
    comm="$(cat "$p/comm" 2>/dev/null)"
    [ "$comm" = "chrome.real" ] || continue
    case "$(tr '\0' ' ' <"$p/cmdline" 2>/dev/null)" in
      *"--user-data-dir=$want"*"--remote-debugging-port"*) echo "$pid" ;;
    esac
  done
}

stop_chrome() {
  # primary: the chrome we launched. setsid made it a process-group
  # leader, so the group kill takes every render child with it.
  if [ -n "${CHROME_PID:-}" ] && kill -0 "$CHROME_PID" 2>/dev/null; then
    kill -TERM -- "-$CHROME_PID" 2>/dev/null
    kill -TERM "$CHROME_PID" 2>/dev/null
    local i=0
    while kill -0 "$CHROME_PID" 2>/dev/null && [ "$i" -lt 10 ]; do
      sleep 0.2
      i=$((i + 1))
    done
    kill -KILL -- "-$CHROME_PID" 2>/dev/null
    kill -KILL "$CHROME_PID" 2>/dev/null
  fi
  # belt: a chrome wearing this run's profile dir but not the pid we
  # recorded (setsid forked instead of execing, pid churn). Children
  # die with their main process, so only mains are targeted.
  local pid
  for pid in $(chrome_procs_matching "${CHROME_UD:-__none__}"); do
    kill "$pid" 2>/dev/null
  done
  CHROME_PID=""
}

trap 'journey_lock_release; stop_chrome; rm -f "$JRN_LIVE_FILE" 2>/dev/null' EXIT

# --- lane health -------------------------------------------------------------
# -m 10 bounds the lane HTTP calls. A lane that ACCEPTS the connection but
# never answers (a frozen single-threaded server, a socket nobody services)
# used to park this curl - and with it the run lock - forever, before a single
# recipe ran; every later suite then refused or queued behind a suite that
# would never finish (witnessed live 2026-11-16, card t_91615fe6). A dead lane
# now reports "not healthy" in seconds instead of hanging. One fetch, reused by
# the diagnosis below: the old follow-up curl doubled the wait on a dead lane
# and could re-hang on a lane that froze after the first attempt.
lane_health="$(curl -s -m 10 "$JOURNEY_BASE_URL/health" 2>/dev/null)"
if ! printf '%s' "$lane_health" | grep -q '"ok"'; then
  echo "journeys: lane $JOURNEY_BASE_URL is not healthy (infra, not product)" >&2
  printf '%s\n' "$lane_health" >&2 || true
  exit 2
fi

# --- browser attach ----------------------------------------------------------
JOURNEY_CDP_ATTACHED=0
CHROME_PID=""
CHROME_UD=""
# INT/TERM on this script: exit with the conventional status, which runs
# the EXIT trap -> stop_chrome. SIGKILL cannot be trapped; the suite-start
# sweep and standalone sweep-chromes.sh are the backstop for that.
trap 'exit 130' INT
trap 'exit 143' TERM
chrome_listening() {
  curl -s "http://127.0.0.1:$JOURNEY_CHROME_PORT/json/version" 2>/dev/null | grep -q Chrome
}
# --- the port this run owns (mechanism: one CDP port for the host) -----------
# A browser already answering on the DEFAULT port is not ours to drive: it is
# another run's, or a crashed run's leftover. Adopting it puts two suites in one
# renderer, one localStorage and one console buffer, and the owner's exit kills
# the browser under the sibling mid-recipe. A caller that NAMED the port (a
# pre-launched chrome, a pinned lane) keeps the adopt.
if [ "$JOURNEY_CHROME_PORT_PINNED" = "0" ] && chrome_listening; then
  free_port="$(lib_call "journey_pick_free_port $JOURNEY_CHROME_PORT")"
  if [ -n "$free_port" ] && [ "$free_port" != "$JOURNEY_CHROME_PORT" ]; then
    printf 'journeys: cdp port %s answers and this run did not launch it - taking %s\n' \
      "$JOURNEY_CHROME_PORT" "$free_port"
    JOURNEY_CHROME_PORT="$free_port"
  fi
fi
if ! chrome_listening; then
  # a stale NON-chrome listener may hold the CDP port: clear it
  holder=$(ss -tlnp 2>/dev/null | grep ":${JOURNEY_CHROME_PORT} " | grep -oE 'pid=[0-9]+' | head -1 | cut -d= -f2)
  if [ -n "$holder" ] && ! grep -qa chrome "/proc/$holder/comm" 2>/dev/null; then
    kill -9 "$holder" 2>/dev/null || true
    sleep 1
  fi
  # Pi lane: launch the bundled headless Chromium when its binary exists;
  # otherwise fall through to agent-browser auto-launch (the CI lane).
  pattern="$HOME/.hermes/tools/chromium-*/chrome-linux/chrome.real"
  pi_chrome=""
  for cand in $pattern; do [ -x "$cand" ] && pi_chrome="$cand" && break; done
  if [ -n "$pi_chrome" ] || [ -n "${JOURNEY_CHROME_BIN:-}" ]; then
    if ! launch_chrome reset; then
      # a sibling can take the port between the probe above and the bind: one
      # retry on the next free port, never for a port the caller named.
      retry_port=""
      if [ "$JOURNEY_CHROME_PORT_PINNED" = "0" ]; then
        retry_port="$(lib_call "journey_pick_free_port $((JOURNEY_CHROME_PORT + 1))")"
      fi
      if [ -z "$retry_port" ]; then
        echo "journeys: headless chromium launch failed on port $JOURNEY_CHROME_PORT" >&2
        exit 2
      fi
      printf 'journeys: cdp port %s was taken during launch - taking %s\n' \
        "$JOURNEY_CHROME_PORT" "$retry_port"
      JOURNEY_CHROME_PORT="$retry_port"
      launch_chrome reset || { echo "journeys: headless chromium launch failed on port $JOURNEY_CHROME_PORT" >&2; exit 2; }
    fi
  fi
fi
if chrome_listening; then
  # background+wait, not a foreground call: a foreground child defers
  # TERM/INT traps until it exits (proven by orphan_test phase A - the
  # chrome outlived a mid-connect TERM), while the wait builtin runs a
  # pending trap at once. Recipes stay foreground: a TERM mid-recipe
  # defers the trap to the recipe end, the trap still fires, the chrome
  # dies with the run - never an orphan.
  # 9>&- : never leak the run-lock fd into the resident daemon - the
  # connect process forks a daemon that outlives the suite, and an
  # inherited lock fd would keep the flock alive after run.sh exits
  # (every later suite then refused forever).
  "$AB" --session "$JOURNEY_SESSION" connect "$JOURNEY_CHROME_PORT" 9>&- >/dev/null 2>&1 &
  if ! wait "$!"; then
    echo "journeys: agent-browser cannot attach to the CDP port" >&2
    exit 2
  fi
  JOURNEY_CDP_ATTACHED=1
fi
# else: agent-browser daemon launches its own Chrome on first command.

# --- pick journeys -----------------------------------------------------------
all=$(find "$JROOT/recipes" -name '*.sh' | sort)
# --regression: run ONLY the named regression set (regression.list, the
# incidents-as-eval set - card t_8abdbb15). This is the gate-time
# minimum and the fast lane-sha proof; the full suite still runs the
# set as part of everything. A listed name that matches no recipe is
# an ERROR (exit 2) - a rotting entry reads as deleted proof.
if [ "${1:-}" = "--regression" ]; then
  shift
  [ "$#" -gt 0 ] && journey_usage_die "--regression takes no journey names (got: $*)"
  want=""
  while read -r regname _ || [ -n "$regname" ]; do
    case "$regname" in ""|\#*) continue ;; esac
    found=""
    for cand in $all; do
      case "$(basename "$cand")" in
        "$regname"|"$regname".sh) found="$cand" ;;
      esac
    done
    if [ -z "$found" ]; then
      echo "journeys: regression.list names unknown recipe: $regname (deleted entry = deleted proof - fix the list)" >&2
      exit 2
    fi
    want="$want $found"
  done < "$JROOT/regression.list"
  [ -n "$want" ] || { echo "journeys: regression.list carries no recipes" >&2; exit 2; }
elif [ "$#" -eq 0 ]; then
  want="$all"
else
  want=""
  for name in "$@"; do
    matched=0
    for cand in $all; do
      case "$(basename "$cand")" in
        *"$name"*) want="$want $cand"; matched=1 ;;
      esac
    done
    if [ "$matched" = "0" ]; then
      # the silent-drop trap: a name that matches nothing used to be ignored,
      # so `run.sh --base <url> <journey>` ran the journey against the DEFAULT
      # lane and read green (card t_2a9be842). Name it and exit.
      journey_usage_die "no recipe matches: $name"
    fi
  done
fi

# --- run ---------------------------------------------------------------------
# proc_alive <pid>: alive means still RUNNING. /proc/<pid>/stat state 'Z' is a
# dead process whose parent has not reaped it yet - kill -0 says yes to it, and
# a poll built on kill -0 alone would wait its whole budget out on every kill
# and then report a "survivor" that is a corpse. No /proc (or no stat entry for
# a pid the kernel still knows, as MSYS does) falls back to kill -0.
proc_alive() {
  local st
  st="$(cat "/proc/$1/stat" 2>/dev/null)" || { kill -0 "$1" 2>/dev/null; return; }
  case "${st##*) }" in Z*) return 1 ;; esac
  return 0
}

# proc_child_pids <pid>: every live descendant, depth-first over /proc ppid
# links. pgrep is not on every lane host (winbtw ships no procps), so the walk
# reads stat field 4 directly - the same comm-name caution as
# chrome_procs_matching: a comm containing a space would shift the fields.
proc_child_pids() {
  local want="$1" p pid
  for p in /proc/[0-9]*; do
    pid="${p#/proc/}"
    [ "$(awk '{print $4}' "$p/stat" 2>/dev/null)" = "$want" ] || continue
    printf '%s\n' "$pid"
    proc_child_pids "$pid"
  done
}

export JOURNEY_BASE_URL JOURNEY_CHROME_PORT JOURNEY_TIMEOUT JOURNEY_JSON JOURNEY_CDP_ATTACHED
export JOURNEY_LOADAVG
export JROOT
export AB

# Model probe: recipes that drive a real chat turn (JOURNEY_NEEDS_MODEL=1)
# stream tokens - no provider, no journey. The lane's /models is the one
# source of truth (the webui uses it too). A lane with no reachable model
# SKIPS those recipes loudly instead of burning wait caps to a RED that
# says nothing about the presentation. The nightly CI lane without model
# creds runs green with skips; the dev lane runs everything.
lane_has_models() {
  curl -s -m 10 "$JOURNEY_BASE_URL/models" | grep -q '"models":\[[^]]'
}
NEEDS_MODEL_PROBE_DONE=0
NEEDS_MODEL_PROBE_RESULT=0
needs_model_skip() {
  grep -q '^JOURNEY_NEEDS_MODEL=1' "$1" || return 1
  if [ "$NEEDS_MODEL_PROBE_DONE" = "0" ]; then
    NEEDS_MODEL_PROBE_DONE=1
    if lane_has_models; then NEEDS_MODEL_PROBE_RESULT=1; fi
    if [ "$NEEDS_MODEL_PROBE_RESULT" = "0" ]; then
      echo "journeys: lane has no reachable model - model-driven recipes will SKIP"
    fi
  fi
  [ "$NEEDS_MODEL_PROBE_RESULT" = "0" ]
}

started=$SECONDS
green=0
skipped=0
failed=""

# pre-clean: a crashed earlier run may have left journey artifacts
# behind - sessions, board tasks, roster agent homes (card t_a99ffcd7)
bash -c ". '$JROOT/lib.sh' && cleanup_journey_residue" >/dev/null 2>&1 || true

for recipe in $want; do
  name="$(basename "$recipe" .sh)"
  if needs_model_skip "$recipe"; then
    printf 'journeys: %-30s SKIPPED (lane has no reachable model)\n' "$name"
    skipped=$((skipped + 1))
    continue
  fi
  log="$EVID/$name.log"
  # the scale is re-sampled here, before the recipe gets its budgets: the load
  # this suite was sized for can change while it runs (see the clock block), and
  # so can the stretch a second costs on this host.
  if [ "$JOURNEY_CLOCK_PINNED" = "0" ] && clock_refresh 1; then
    clock_report
  fi
  printf 'journeys: %-30s running\n' "$name"
  recipe_started=$SECONDS
  # 9>&- on both background jobs: the recipe subshell and the watchdog
  # compound fork children (ab CLI, sleep) that may be orphaned after
  # the suite exits - none of them may keep the run-lock description
  # alive, or the next suite refuses forever (proven 2026-10-28: leaked
  # fd 9 in watchdog sleeps + the resident agent-browser daemon held
  # the lock after a finished run; later suites refused).
  (
    # the ONE exit path: residue (sessions, board cards, agent homes) plus the
    # client state a journey mutated - panes, viewport, localStorage - so no
    # journey inherits the last one's baseline (the determinism contract,
    # card t_fdd12843; lib.sh journey_exit_cleanup).
    trap 'journey_exit_cleanup' EXIT
    . "$JROOT/lib.sh"
    # the death record is per RECIPE: what killed the previous recipe's subshell
    # says nothing about this one, and the mark must be gone before the first
    # primitive (whose own redirection would swallow the guard's message).
    rm -f "$(journey_death_mark)" 2>/dev/null
    # scope the console record to THIS recipe (t_6654bc60): the daemon's
    # buffer outlives the page, the reload and the chrome, so a transient
    # error from an earlier run used to red every later
    # assert_no_console_errors while the pages themselves were silent.
    console_scope_begin
    . "$recipe"
    # a die inside a command substitution stopped only that subshell; the mark
    # it left is this recipe's real verdict (mechanism: the substitution boundary)
    journey_dead_check
  ) >> "$log" 2>&1 9>&- &
  # >> not > on the log above: the WATCHDOG writes this same file (the cap's
  # verdict lines), and a truncating fd there keeps offset 0, so the recipe's own
  # later writes land on top of what the watchdog already appended - the cap line
  # then reads as never written. The evidence dir is fresh per run, so append is
  # the same thing as truncate for the first writer.
  rpid=$!
  # The cap's own clock (mechanism: the watchdog's sleep outlives its subshell).
  # Killing this subshell on the green path leaves its `sleep` alive, orphaned -
  # and that sleep inherited run.sh's stdout, so a caller that PIPES the suite
  # (`run.sh ... | tee log`, `$(run.sh ...)`) gets no EOF until the cap expires:
  # the suite reports "suite done" and then the caller hangs for up to
  # JOURNEY_TIMEOUT seconds. That is the pre-flight's workaround in
  # gate-preflight.sh, and the clock multiplies it (a 400% host holds the pipe
  # for 1200s). So the sleep is OUR child, named, and TERM to the subshell takes
  # it along - plus the whole subshell's fds go to /dev/null, the belt that no
  # fork of it can hold a caller's pipe even if the kill is missed.
  (
    wd_sleep=""
    trap '[ -n "$wd_sleep" ] && kill "$wd_sleep" 2>/dev/null; exit 0' TERM
    sleep "$JOURNEY_TIMEOUT" &
    wd_sleep=$!
    wait "$wd_sleep" 2>/dev/null || exit 0
    {
    # The verdict goes FIRST (mechanism: the cap line raced the kill). Main
    # kills this subshell as soon as the recipe is gone, and the recipe's own
    # EXIT trap (residue cleanup) can take a second - so a cap line written
    # after the kill poll is written after main's TERM, and the log then says
    # only "RED" with no cap anywhere in it.
    printf 'journeys: %s exceeded %ss wall cap\n' "$name" "$JOURNEY_TIMEOUT" >> "$log"
    # TERM first: a recipe between steps (or one carrying a trap) leaves on it.
    kill "$rpid" 2>/dev/null
    # ... but a subshell blocked inside a foreground child - an agent-browser
    # call that never returns - defers TERM until that child exits, which may
    # be never: `wait "$rpid"` below then holds the run lock forever and every
    # later suite refuses or queues behind a suite that cannot finish
    # (witnessed live 2026-11-16, card t_91615fe6). Poll briefly, then
    # hard-kill the subshell and every descendant we can see, so the wall cap
    # is a cap and not a hint. Residue: a SIGKILLed recipe skips its EXIT-trap
    # cleanup; the next suite's pre-clean reaps it (as after a crashed run).
    i=0
    while [ "$i" -lt 25 ] && proc_alive "$rpid"; do
      sleep 0.2
      i=$((i + 1))
    done
    if proc_alive "$rpid"; then
      for cpid in $(proc_child_pids "$rpid"); do
        kill -9 "$cpid" 2>/dev/null
      done
      kill -9 "$rpid" 2>/dev/null
      printf 'journeys: %s survived TERM (blocked child) - SIGKILLed\n' "$name" >> "$log"
    fi
    } >/dev/null 2>&1
  ) 9>&- &
  watchdog=$!
  if wait "$rpid"; then
    printf 'journeys: %-30s green (%ss of %ss)\n' "$name" "$((SECONDS - recipe_started))" "$JOURNEY_TIMEOUT"
    green=$((green + 1))
  else
    printf 'journeys: %-30s RED (%ss of %ss)\n' "$name" "$((SECONDS - recipe_started))" "$JOURNEY_TIMEOUT"
    printf '  evidence: %s\n' "$log"
    grep -E 'journey RED|timed out' "$log" | head -3 | sed 's/^/  | /' >&2
    tail -3 "$log" | sed 's/^/  | /' >&2
    failed="$failed $name"
  fi
  kill "$watchdog" 2>/dev/null
  wait "$watchdog" 2>/dev/null
done

# --- release gate: close idle agent-browser daemons ---------------------------
# The suite body is over and the run lock is still ours, so no other run.sh
# suite can be between recipes: the only sessions that could be mid-flight
# are gone by construction. Each closed daemon takes the chrome it was
# holding with it (a chrome this suite ATTACHED to is left alone - it was
# never the daemon's). JOURNEY_GATE_PID is the lock the sweep verifies
# against; it stands down if the recorded holder is anyone else.
JOURNEY_GATE_PID=$$ JOURNEY_SESSION="$JOURNEY_SESSION" JOURNEY_AB="$AB" \
  bash "$JROOT/sweep-daemons.sh" -q --gate 2>/dev/null || true

elapsed=$((SECONDS - started))
redcount=0
for _ in $failed; do redcount=$((redcount + 1)); done
printf 'journeys: suite done — %ss, green=%s skipped=%s red=%s\n' "$elapsed" "$green" "$skipped" "$redcount"
printf 'journeys: evidence in %s\n' "$EVID"

if [ "$JOURNEY_JSON" = "1" ]; then
  json_failed=""
  for f in $failed; do
    if [ -n "$json_failed" ]; then json_failed="$json_failed,"; fi
    json_failed="$json_failed\"$f\""
  done
  printf 'journeys: {"green": %s, "skipped": %s, "failed": [%s]}\n' "$green" "$skipped" "$json_failed"
fi

if [ "$redcount" -gt 0 ]; then
  printf 'journeys: RED — failing:%s\n' "$failed"
  exit 1
fi
exit 0