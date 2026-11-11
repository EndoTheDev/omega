#!/bin/bash
# sweep-daemons.sh - close IDLE agent-browser daemons (each one holds a chrome).
#
# The other half of the chrome-accumulation problem, and the half
# sweep-chromes.sh must never touch.
#
#   A daemon outliving its client is agent-browser's design: the first
#   `agent-browser ... --session <name>` call forks a daemon, the CLI exits,
#   the daemon keeps the Chrome it launched alive so the NEXT command in the
#   same session reuses the warm browser. Every session name ever used
#   therefore leaves behind one daemon + one chrome that nothing reaps.
#   Sweep evidence (winbtw, 2026-11-11): 6 live daemons, each parented by a
#   dead CLI, each holding exactly one `agent-browser-chrome-<uuid>` main -
#   and sweep-chromes.sh correctly leaves them all alone, because the chrome
#   they hold has a LIVE parent (the daemon).
#
# Closing one is only safe when nobody is driving it. `agent-browser close`
# tears the daemon and its Chrome down (proven 2026-11-11: the pid state
# files vanish and the chrome's process tree with them); on a session that
# merely ATTACHED to a chrome (`connect <port>`, the run.sh CDP-attach path)
# it disconnects and leaves the foreign chrome running (also proven) - the
# attached chrome was never the daemon's to kill.
#
# Liveness law - three verified facts, no pattern guessing:
#
#   G1  THE RUN LOCK. A suite body holds it for its whole run, so while we
#       hold it (or read a pid file that says the caller holds it) no other
#       run.sh can be between recipes. Closing a session a live suite is
#       mid-use of is impossible by construction, not by luck.
#   G2  NO ATTACHED CLIENT. A client command holds an ESTABLISHED connection
#       on the session's control port for as long as it runs and drops it
#       when it exits (proven: `wait 15000` held one, the moment it ended
#       only TIME_WAIT rows remained). A session with a client attached is
#       live and is skipped. Hosts where neither netstat nor ss exists
#       cannot answer - the answer is then "unknown" and the session is
#       skipped, so an unverifiable host reaps nothing.
#   G3  AN IDLE WINDOW. No state file is touched after a daemon starts, so
#       there is no activity clock to read; the session's daemon must have
#       been up for JOURNEY_DAEMON_IDLE_MIN minutes (default 10). A session
#       a worker started a moment ago is never swept. The GATE's own session
#       ($JOURNEY_SESSION, still fresh, provably done because the suite body
#       has ended) is exempt.
#
# Where it runs: at the RELEASE GATE, the moment run.sh has finished its
# recipes and still holds the run lock (see run.sh). Never at suite start -
# a concurrent worker's suite would be refused or queued, but the worker's
# own session could be mid-flight on a second host share.
#
# Usage:
#   scripts/journeys/sweep-daemons.sh                 # standalone: take the lock, then sweep
#   scripts/journeys/sweep-daemons.sh -q              # silent unless it closed something
#   scripts/journeys/sweep-daemons.sh -n              # print the decision, close nothing
#   JOURNEY_GATE_PID=$$ ... --gate                    # from run.sh: caller owns the lock
#
# Env:
#   JOURNEY_LOCK               run lock path (default /tmp/omega-journeys.lock)
#   JOURNEY_AB                 agent-browser binary (run.sh passes its own)
#   JOURNEY_DAEMON_STATE       agent-browser state dir (default ~/.agent-browser)
#   JOURNEY_DAEMON_IDLE_MIN    idle window in minutes (default 10)
#   JOURNEY_SESSION            the gate suite's session name (exempt from G3)
#   JOURNEY_GATE_PID           the pid whose run lock the caller holds (gate mode)
#
# Exit: 0 nothing closed, 1 daemons closed (visible in cron/CI logs).

set -u

quiet=0
dry=0
gate=0
for arg in "$@"; do
  case "$arg" in
    -q) quiet=1 ;;
    -n|--dry-run) dry=1 ;;
    -g|--gate) gate=1 ;;
    *) printf 'sweep-daemons: unknown argument: %s\n' "$arg" >&2; exit 2 ;;
  esac
done

say() { [ "$quiet" = 1 ] || printf '%s\n' "$*"; }

JOURNEY_LOCK="${JOURNEY_LOCK:-/tmp/omega-journeys.lock}"
STATE="${JOURNEY_DAEMON_STATE:-$HOME/.agent-browser}"
IDLE_MIN="${JOURNEY_DAEMON_IDLE_MIN:-10}"
case "$IDLE_MIN" in ''|*[!0-9]*) IDLE_MIN=10 ;; esac
GATE_PID="${JOURNEY_GATE_PID:-}"

# --- the agent-browser CLI ---------------------------------------------------
AB="${JOURNEY_AB:-$(command -v agent-browser 2>/dev/null | head -1)}"
if [ -z "$AB" ]; then
  AB="$(find "$HOME/.hermes/tools" "$HOME/AppData/Local/hermes/tools" -maxdepth 5 -type f \
     \( -name 'agent-browser-linux-arm64' -o -name 'agent-browser-linux-x64' \
        -o -name 'agent-browser-win32-x64.exe' -o -name 'agent-browser-win32-arm64.exe' \
        -o -name 'agent-browser' \) 2>/dev/null | head -1)"
fi
if [ -z "$AB" ] || [ ! -x "$AB" ]; then
  say 'sweep-daemons: no agent-browser CLI - nothing to close'
  exit 0
fi

# --- G1: the run lock --------------------------------------------------------
LOCK_TAKEN=0

# pid recorded by whoever holds the lock. run.sh writes ${JOURNEY_LOCK}.pid in
# flock mode and ${JOURNEY_LOCK}.d/pid in mkdir mode (no flock on MSYS).
lock_holder_pid() {
  if [ -f "$JOURNEY_LOCK.d/pid" ]; then
    tr -d '\r\n' < "$JOURNEY_LOCK.d/pid" 2>/dev/null
    return 0
  fi
  if [ -f "$JOURNEY_LOCK.pid" ]; then
    tr -d '\r\n' < "$JOURNEY_LOCK.pid" 2>/dev/null
    return 0
  fi
  return 1
}

release_lock() {
  [ "$LOCK_TAKEN" = 1 ] || return 0
  rm -rf "$JOURNEY_LOCK.d" 2>/dev/null
  return 0
}
trap release_lock EXIT

take_lock() { # 0 = we hold the run lock now
  if command -v flock >/dev/null 2>&1; then
    exec 9>>"$JOURNEY_LOCK" || return 1
    if flock -n 9; then
      printf '%s\n' "$$" > "${JOURNEY_LOCK}.pid" 2>/dev/null
      LOCK_TAKEN=1
      return 0
    fi
    exec 9>&-
    return 1
  fi
  if mkdir "$JOURNEY_LOCK.d" 2>/dev/null; then
    printf '%s\n' "$$" > "$JOURNEY_LOCK.d/pid"
    LOCK_TAKEN=1
    return 0
  fi
  # a dir whose owner is dead is stale (SIGKILLed suite) - reap it, same rule
  # run.sh applies
  local holder
  holder="$(tr -d '\r\n' < "$JOURNEY_LOCK.d/pid" 2>/dev/null || echo '')"
  case "$holder" in
    [0-9]*)
      if ! kill -0 "$holder" 2>/dev/null; then
        rm -rf "$JOURNEY_LOCK.d" 2>/dev/null
        if mkdir "$JOURNEY_LOCK.d" 2>/dev/null; then
          printf '%s\n' "$$" > "$JOURNEY_LOCK.d/pid"
          LOCK_TAKEN=1
          return 0
        fi
      fi ;;
  esac
  return 1
}

if [ "$gate" = 1 ]; then
  # Called from the suite body. We cannot take the lock our own caller holds,
  # so verify the caller really holds it: the recorded holder pid must be the
  # pid the caller claims. A foreign suite holds a different pid - stand down.
  if [ -z "$GATE_PID" ]; then
    say 'sweep-daemons: --gate without JOURNEY_GATE_PID - cannot verify the caller holds the run lock, standing down'
    exit 0
  fi
  holder="$(lock_holder_pid || true)"
  if [ -z "$holder" ]; then
    say "sweep-daemons: no run lock on record - cannot verify the caller (pid $GATE_PID) holds it, standing down"
    exit 0
  fi
  if [ "$holder" != "$GATE_PID" ]; then
    say "sweep-daemons: run lock is held by pid $holder, not by the caller (pid $GATE_PID) - standing down"
    exit 0
  fi
else
  if ! take_lock; then
    holder="$(lock_holder_pid || echo '?')"
    say "sweep-daemons: a journey suite holds the run lock (pid ${holder:-?}) - skipping"
    exit 0
  fi
fi

# --- the live sessions -------------------------------------------------------
# `session list` reports live sessions only: a state dir seeded with a bogus
# pid is not listed (proven 2026-11-11), so everything below is a real daemon.
list_sessions() {
  local out names
  if out="$("$AB" session list --json 2>/dev/null)"; then
    names="$(printf '%s' "$out" \
      | sed -n 's/.*"sessions":\[\([^]]*\)\].*/\1/p' \
      | tr ',' '\n' | tr -d '\r' \
      | sed 's/^[[:space:]]*"//; s/"[[:space:]]*$//' \
      | grep -v '^[[:space:]]*$')"
    [ -n "$names" ] && { printf '%s\n' "$names"; return 0; }
  fi
  # older CLI without --json: "Active sessions:" then two-space indented names
  "$AB" session list 2>/dev/null | tr -d '\r' | sed -n 's/^  \([^ ][^ ]*\)$/\1/p'
}

# --- G2: is a client attached right now? -------------------------------------
# 0 attached, 1 idle, 2 cannot tell (no port record, or no socket tool).
client_attached() { # $1: session name
  local port
  port=''
  if [ -f "$STATE/$1.port" ]; then
    port="$(tr -d '\r\n' < "$STATE/$1.port" 2>/dev/null || true)"
  fi
  case "$port" in ''|*[!0-9]*) return 2 ;; esac
  if command -v netstat >/dev/null 2>&1; then
    if netstat -ano 2>/dev/null | tr -d '\r' | awk -v p=":$port" '
        NR > 3 {
          l = length(p)
          if ($1 == "TCP" && substr($2, length($2) - l + 1) == p && $4 == "ESTABLISHED") { found = 1; exit }
        }
        END { exit(found ? 0 : 1) }'; then
      return 0
    fi
    return 1
  fi
  if command -v ss >/dev/null 2>&1; then
    if ss -Htn state established 2>/dev/null | tr -d '\r' | awk -v p=":$port" '
        {
          l = length(p)
          if (substr($3, length($3) - l + 1) == p) { found = 1; exit }
        }
        END { exit(found ? 0 : 1) }'; then
      return 0
    fi
    return 1
  fi
  return 2
}

# --- G3: how long has this daemon been up? -----------------------------------
# Nothing stamps an agent-browser session file after launch (proven: two
# commands left every mtime at the launch second), so the pid file's own
# mtime IS the daemon's start. Unknown age is not closed.
daemon_pid() { # $1: session name -> the pid the state dir records
  local f
  f="$STATE/$1.pid"
  [ -f "$f" ] || return 1
  tr -d '\r\n' < "$f" 2>/dev/null
}

daemon_age() { # $1: session name -> seconds, non-zero exit when unknown
  local f m
  f="$STATE/$1.pid"
  [ -f "$f" ] || return 1
  m="$(stat -c %Y "$f" 2>/dev/null)" || return 1
  case "$m" in ''|*[!0-9]*) return 1 ;; esac
  printf '%s\n' "$(( $(date +%s) - m ))"
}

# --- sweep -------------------------------------------------------------------
idle_secs=$((IDLE_MIN * 60))
closed=0
kept=0

sessions="$(list_sessions || true)"
if [ -z "${sessions//[[:space:]]/}" ]; then
  say 'sweep-daemons: no live agent-browser session'
  exit 0
fi

for name in $sessions; do
  client_attached "$name"
  case "$?" in
    0) say "sweep-daemons: $name is live (client attached) - keeping"; kept=$((kept + 1)); continue ;;
    2) say "sweep-daemons: $name liveness unverifiable on this host - keeping"; kept=$((kept + 1)); continue ;;
  esac

  age="$(daemon_age "$name" || true)"
  pid_at_list="$(daemon_pid "$name" || true)"
  if [ -z "$age" ]; then
    say "sweep-daemons: $name has no state pid file to date - keeping"
    kept=$((kept + 1))
    continue
  fi

  ours=0
  if [ "$gate" = 1 ] && [ -n "${JOURNEY_SESSION:-}" ] && [ "$name" = "$JOURNEY_SESSION" ]; then
    ours=1
  fi
  if [ "$ours" = 0 ] && [ "$age" -lt "$idle_secs" ]; then
    say "sweep-daemons: $name is $((age / 60))m old (idle window ${IDLE_MIN}m) - keeping"
    kept=$((kept + 1))
    continue
  fi
  # TOCTOU: a session of the same name restarted between the listing and the
  # close would be a FRESH daemon - never close one whose recorded pid moved.
  pid_now="$(daemon_pid "$name" || true)"
  if [ -z "$pid_at_list" ] || [ "$pid_at_list" != "$pid_now" ]; then
    say "sweep-daemons: $name changed under us - keeping"
    kept=$((kept + 1))
    continue
  fi

  if [ "$dry" = 1 ]; then
    printf 'sweep-daemons: would close idle daemon %s (%sm)\n' "$name" "$((age / 60))"
    closed=$((closed + 1))
    continue
  fi
  if "$AB" --session "$name" close >/dev/null 2>&1; then
    printf 'sweep-daemons: closed idle daemon %s (%sm)\n' "$name" "$((age / 60))"
    closed=$((closed + 1))
  else
    printf 'sweep-daemons: close failed for %s - leaving it\n' "$name" >&2
    kept=$((kept + 1))
  fi
done

if [ "$dry" = 1 ]; then
  printf 'sweep-daemons: %s idle daemon(s) would be closed\n' "$closed"
  exit 0
fi
if [ "$closed" -gt 0 ]; then
  printf 'sweep-daemons: closed %s idle daemon(s), kept %s live\n' "$closed" "$kept"
  exit 1
fi
say "sweep-daemons: no idle daemon to close (kept $kept live)"
exit 0
