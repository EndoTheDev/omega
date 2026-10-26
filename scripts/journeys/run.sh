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
#   scripts/journeys/run.sh --base <url>       # other lane (seed-broken.sh uses this)
#   scripts/journeys/run.sh <journey-name>...  # subset (e.g. 20-chat-send-response)
#   JOURNEY_JSON=1 run.sh                      # machine summary on the final line
#
# Exit codes: 0 all green, 1 at least one journey red, 2 lane/browser
# infra dead (nothing about the product is asserted in that state).
#
# Two lanes (amendment, Endo 2026-10-21):
#   - Pi per-landing: run.sh launches the Hermes-bundled headless
#     Chromium (no system chromium on the Pi, deliberate) and drives it
#     through the agent-browser CLI. The nss/nspr libs ride LD_LIBRARY_PATH
#     (assembled chromium-libs dir), never a system package.
#   - CI nightly: agent-browser auto-launches its own Chrome - install
#     once with `npm i -g agent-browser && agent-browser install`, run
#     the same suite against the built artifact.
#
# Every write-journey creates a throwaway session (id prefix jrn-, label
# prefix "(journey)") and deletes it on exit; the suite leaves no
# residue in the live store.
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

set -u

JROOT="$(cd "$(dirname "$0")" && pwd)"
EVID="$(mktemp -d "${TMPDIR:-/tmp}/omega-journeys-evidence.XXXXXX")"

# backstop: reap chromes orphaned by dead earlier runs (SIGKILLed run.sh,
# dead worker debug sessions) before anything here can attach to them
bash "$JROOT/sweep-chromes.sh" -q 2>/dev/null || true

# --- env -------------------------------------------------------------------
JOURNEY_BASE_URL="${JOURNEY_BASE_URL:-http://127.0.0.1:8098}"
JOURNEY_CHROME_PORT="${JOURNEY_CHROME_PORT:-9333}"
JOURNEY_TIMEOUT="${JOURNEY_TIMEOUT:-300}"
JOURNEY_JSON="${JOURNEY_JSON:-0}"
# the browser session name rides EVERY command (lib.sh ab) and the CDP
# connect: two concurrent suite runs share one agent-browser daemon, and
# the daemon's default connection is global - without a session-scoped
# connect the LAST connect wins and both runs drive one chrome (mis-read
# DOM, foreign console noise, phantom reds). A fixed name keeps the
# single-run behavior; concurrent workers export distinct names.
JOURNEY_SESSION="${JOURNEY_SESSION:-omega-journey}"

# --- agent-browser binary ----------------------------------------------------
AB="$(command -v agent-browser 2>/dev/null | head -1)"
if [ -z "$AB" ]; then
  AB="$(find "$HOME/.hermes/tools" -maxdepth 5 -type f \
     \( -name 'agent-browser-linux-arm64' -o -name 'agent-browser-linux-x64' \) 2>/dev/null | head -1)"
fi
if [ -z "$AB" ]; then
  echo "journeys: agent-browser CLI not found (PATH or ~/.hermes/tools)" >&2
  echo "  install: npm i -g agent-browser && agent-browser install" >&2
  exit 2
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
  # without touching this script's own group.
  setsid env \
    LD_LIBRARY_PATH="$LIBS:$(dirname "$BINPATH")" \
    "$BINPATH" \
    --headless=new --remote-debugging-port="$JOURNEY_CHROME_PORT" \
    --user-data-dir="$UD" --no-first-run --no-default-browser-check \
    --disable-gpu --window-size=1280,720 --noerrdialogs \
    --use-mock-keychain --password-store=basic \
    >/dev/null 2>&1 &
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

trap 'stop_chrome' EXIT

# --- lane health -------------------------------------------------------------
if ! curl -s "$JOURNEY_BASE_URL/health" 2>/dev/null | grep -q '"ok"'; then
  echo "journeys: lane $JOURNEY_BASE_URL is not healthy (infra, not product)" >&2
  curl -s "$JOURNEY_BASE_URL/health" >&2 || true
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
    launch_chrome reset || { echo "journeys: headless chromium launch failed" >&2; exit 2; }
  fi
fi
if chrome_listening; then
  # background+wait, not a foreground call: a foreground child defers
  # TERM/INT traps until it exits (proven by orphan_test phase A - the
  # chrome outlived a mid-connect TERM), while the wait builtin runs a
  # pending trap at once. Recipes stay foreground: a TERM mid-recipe
  # defers the trap to the recipe end, the trap still fires, the chrome
  # dies with the run - never an orphan.
  "$AB" --session "$JOURNEY_SESSION" connect "$JOURNEY_CHROME_PORT" >/dev/null 2>&1 &
  if ! wait "$!"; then
    echo "journeys: agent-browser cannot attach to the CDP port" >&2
    exit 2
  fi
  JOURNEY_CDP_ATTACHED=1
fi
# else: agent-browser daemon launches its own Chrome on first command.

# --- pick journeys -----------------------------------------------------------
all=$(find "$JROOT/recipes" -name '*.sh' | sort)
if [ "$#" -eq 0 ]; then
  want="$all"
else
  want=""
  for name in "$@"; do
    for cand in $all; do
      case "$(basename "$cand")" in
        *"$name"*) want="$want $cand" ;;
      esac
    done
  done
  if [ -z "$want" ]; then
    echo "journeys: no recipe matches: $*" >&2
    exit 2
  fi
fi

# --- run ---------------------------------------------------------------------
export JOURNEY_BASE_URL JOURNEY_CHROME_PORT JOURNEY_TIMEOUT JOURNEY_JSON JOURNEY_CDP_ATTACHED
export JROOT
export AB

# Model probe: recipes that drive a real chat turn (JOURNEY_NEEDS_MODEL=1)
# stream tokens - no provider, no journey. The lane's /models is the one
# source of truth (the webui uses it too). A lane with no reachable model
# SKIPS those recipes loudly instead of burning wait caps to a RED that
# says nothing about the presentation. The nightly CI lane without model
# creds runs green with skips; the dev lane runs everything.
lane_has_models() {
  curl -s "$JOURNEY_BASE_URL/models" | grep -q '"models":\[[^]]'
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

# pre-clean: a crashed earlier run may have left journey sessions behind
bash -c ". '$JROOT/lib.sh' && cleanup_journey_sessions" >/dev/null 2>&1 || true

for recipe in $want; do
  name="$(basename "$recipe" .sh)"
  if needs_model_skip "$recipe"; then
    printf 'journeys: %-30s SKIPPED (lane has no reachable model)\n' "$name"
    skipped=$((skipped + 1))
    continue
  fi
  log="$EVID/$name.log"
  printf 'journeys: %-30s running\n' "$name"
  (
    trap 'cleanup_journey_sessions' EXIT
    . "$JROOT/lib.sh"
    . "$recipe"
  ) > "$log" 2>&1 &
  rpid=$!
  sleep "$JOURNEY_TIMEOUT" && {
    kill "$rpid" 2>/dev/null
    printf 'journeys: %s exceeded %ss wall cap\n' "$name" "$JOURNEY_TIMEOUT" >> "$log"
  } &
  watchdog=$!
  if wait "$rpid"; then
    printf 'journeys: %-30s green\n' "$name"
    green=$((green + 1))
  else
    printf 'journeys: %-30s RED\n' "$name"
    printf '  evidence: %s\n' "$log"
    grep -E 'journey RED|timed out' "$log" | head -3 | sed 's/^/  | /' >&2
    tail -3 "$log" | sed 's/^/  | /' >&2
    failed="$failed $name"
  fi
  kill "$watchdog" 2>/dev/null
  wait "$watchdog" 2>/dev/null
done

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