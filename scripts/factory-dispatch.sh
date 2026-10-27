#!/usr/bin/env bash
# factory-dispatch - collision-aware dispatch for the omega factory
#
# The bare dispatcher takes the OLDEST ready cards and is collision-blind:
# it will spawn a card whose files overlap a running card. This script
# applies the same-file law at dispatch time:
#   1. compute each running card's file surface (worktree diff + body tokens)
#   2. compute each candidate's file surface (body tokens)
#   3. park colliding candidates as 'todo' for the dispatch call, restore after
#   4. dispatch --max CAP (fills free slots with the oldest LEGAL cards)
#
# ponytail: token granularity is body-text paths - a card whose body names
# no paths falls back to class rules (webui+webui allowed, Go beside webui
# allowed) and relies on the worker-side onboarding same-file check. If a
# running card exposes NO surface at all, hold everything (conservative).
# Upgrade path: card bodies grow a FILES: line the script reads first.
#
# Usage: scripts/factory-dispatch.sh [--dry-run]
set -euo pipefail
cd "$(dirname "$0")/.."

DB="$HOME/.hermes/kanban/boards/project-omega/kanban.db"
CAP=4
DRY=0; MAXARG=0
while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY=1 ;;
    --max) MAXARG="${2:-0}"; shift ;;
  esac
  shift
done

tokens_of() {  # extract repo paths from free text (body or diff)
  # Brace lists (modules/{a,b}/) expand first: a FILES surface naming a
  # brace list must cover every member (proven 2026-10-26: the c2 card's
  # FILES line collapsed to a bare modules/ token and hid modules/cron
  # from the overlap check, guard read a colliding card false-legal).
  # The regex gate bounds eval to path-shaped words: no $, backticks,
  # parens or quotes pass it, so eval can only emit path strings.
  # (bash 5.3: quoted =~ RHS is literal - keep the pattern unquoted-var)
  local line gate='^[A-Za-z0-9_./{},: -]+$'
  while IFS= read -r line; do
    if [[ "$line" == *'{'* && "$line" =~ $gate ]]; then
      eval "printf '%s\n' $line" 2>/dev/null || printf '%s\n' "$line"
    else
      printf '%s\n' "$line"
    fi
  done | grep -oE '\b(modules|cmd|docs|scripts|agent|ai|\.github|internal)/[a-zA-Z0-9_./-]+' || true
}

candidate_tokens() {  # id -> WRITE-surface tokens: curated FILES: lines win over body prose
  local body files
  body=$(sqlite3 "$DB" "SELECT body FROM tasks WHERE id='$1';")
  files=$(grep -i '^FILES:' <<<"$body" | head -1 | cut -d: -f2- || true)
  if [ -n "$files" ]; then
    tokens_of <<<"$files"
  else
    tokens_of <<<"$body"
  fi
}

surface_of_card() {  # id -> file tokens: worktree diff against dev + body
  local id="$1"
  local wt=".worktrees/$id"
  if [ -d "$wt" ]; then
    { git diff --name-only dev..."wt/$id" 2>/dev/null   # three-dot: only THIS branch's changes
      git -C "$wt" status --short 2>/dev/null | awk '{print $NF}'
    } | sort -u
  fi
  sqlite3 "$DB" "SELECT body FROM tasks WHERE id='$id';" | tokens_of
}

class_of() {  # id -> webui | go | docs  (FILES: line is the truth; body text
              # only classifies when FILES is absent - out-of-scope prose
              # mentions misclassify cards, proven: a pure-Go TUI card
              # classed webui because its OUT OF SCOPE line said "webui")
  local t files
  t=$(sqlite3 "$DB" "SELECT lower(title||' '||body) FROM tasks WHERE id='$1';")
  files=$(grep -o 'files:.*' <<<"$t" | head -1)
  if [ -n "$files" ]; then
    if grep -q "webui\|\.ts\|\.vue\|nuxt" <<<"$files"; then echo webui
    elif grep -oE '[^ ]+' <<<"$files" | sed 's/^files://' | grep -qv '^docs/'; then echo go
    else echo docs; fi
  elif grep -q "docs/\|documentation" <<<"$t" && ! grep -q "webui" <<<"$t"; then echo docs
  elif grep -q "webui\|webui\|nuxt\|spa" <<<"$t"; then echo webui
  else echo go; fi
}

overlaps() {  # candidate tokens, running tokens -> hit if any token pair overlaps
  # a token matches if the strings are equal, or one is a directory prefix of
  # the other. Trailing slashes are normalized first - "modules/http_channel/"
  # as a dir token made the prefix pattern "modules/http_channel//*" (double
  # slash, never matches) and the guard went token-blind on exactly the
  # ephemeral x bottom-panel server.go overlap (proven 2026-10-22).
  local ctok rtok ct rt
  while read -r ctok; do
    [ -z "$ctok" ] && continue
    ct="${ctok%/}"
    while read -r rtok; do
      [ -z "$rtok" ] && continue
      rt="${rtok%/}"
      if [[ "$ct" == "$rt" || "$ct" == "$rt"/* || "$rt" == "$ct"/* ]]; then
        return 0
      fi
    done <"$2"
  done <"$1"
  return 1
}

# -- running cards + their combined surface --
mapfile -t RUNNING < <(sqlite3 "$DB" "SELECT id FROM tasks WHERE status='running' ORDER BY created_at;")
SURF=/tmp/fd_running_surface.$$; : >"$SURF"
for r in "${RUNNING[@]:-}"; do
  [ -z "$r" ] && continue
  surface_of_card "$r" >>"$SURF"
done
RUN_HAS_SURFACE=$(grep -q . "$SURF" && echo 1 || echo 0)

# -- candidates, PRIORITY FIRST then oldest: guard bug #5 (2026-10-26) --
# the sort was pure creation order, so an urgent bug card could never
# jump a queued refactor chain. Higher priority number = more urgent
# (10 = release-blocking class, 5 = standing queue, 0 = default).
# Ties stay oldest-first (FIFO preserved for the default mass).
mapfile -t CAND < <(sqlite3 "$DB" "SELECT id FROM tasks WHERE status='ready' ORDER BY priority DESC, created_at;")
PARKED=(); LEGAL=0

# guard bug #4 (2026-10-25): hermes kanban dispatch once claimed a card
# that had flipped ready -> review mid-pass (t_bc321318 got a zombie
# worker spawned onto already-merged work). The review pass and this
# dispatch call race on status; park review-state cards for the
# duration of the dispatch call - the EXIT trap unparks them.
# Ceiling: if the daemon ever mis-claims todo/blocked cards the same
# way, widen this status list.
mapfile -t REVIEWCARDS < <(sqlite3 "$DB" "SELECT id FROM tasks WHERE status='review' AND (claim_lock IS NULL OR claim_lock='');" 2>/dev/null || true)
PARKED+=("${REVIEWCARDS[@]:-}")
PASS_SURF=$(mktemp); : >"$PASS_SURF"   # surfaces of candidates already marked legal THIS pass (pairwise)
for c in "${CAND[@]:-}"; do
  [ -z "$c" ] && continue
  # ponytail: no early break at 2 legal - the dispatcher may skip a legal
  # candidate and fall through to an unevaluated one, spawning a same-file
  # pair (proven live 2026-10-21: nuxt.config.ts collision via t_67fc1905).
  # Every ready candidate gets a hold verdict; the cap math stays in the
  # dispatch call.
  CTOK=$(mktemp)
  { candidate_tokens "$c"
    git diff --name-only dev..."wt/$c" 2>/dev/null || true   # resumed candidates carry real writes in their worktrees
    git -C ".worktrees/$c" status --short 2>/dev/null | awk '{print $NF}' || true   # ...and UNCOMMITTED files (proven: t_88bb7dcf's uncommitted journey script was invisible without this)
  } >"$CTOK"
  HOLD=0; WHY=""; MATCHF=""
  # class law FIRST and unconditionally: a go-class candidate never spawns
  # beside a running go card, no matter what tokens it carries (proven:
  # t_7bd3dc06 spawned beside running t_c5b98609 because token-bearing
  # candidates skipped the class check entirely).
  RC=$(for r in "${RUNNING[@]:-}"; do [ -n "$r" ] && class_of "$r" || true; done | sort -u | tr '\n' ',')
  CC=$(class_of "$c")
  if [ "$RUN_HAS_SURFACE" = 1 ] && [ "$CC" = go ] && [[ ",$RC," == *,go,* ]]; then
    HOLD=1; WHY="go-class beside running go: $RC (sanctioned pairs are go+webui only)"
  elif grep -q . "$CTOK"; then
    if [ "$RUN_HAS_SURFACE" = 1 ] && overlaps "$CTOK" "$SURF"; then
      HOLD=1; MATCHF="$SURF"
    elif grep -q . "$PASS_SURF" && overlaps "$CTOK" "$PASS_SURF"; then
      HOLD=1; MATCHF="$PASS_SURF"   # pair collision: a legal co-spawn candidate owns the file
    fi
    if [ "$HOLD" = 1 ]; then
      WHY="same-file"
      while read -r ct; do
        [ -n "$MATCHF" ] && grep -qE "^${ct}(/|$)" "$MATCHF" && { WHY="same-file: $ct"; break; }
      done <"$CTOK"
    fi
  fi
  rm -f "$CTOK"
  if [ "$HOLD" = 1 ]; then
    echo "hold: $c  ($WHY)"
    PARKED+=("$c")
  else
    echo "legal: $c"
    LEGAL=$((LEGAL+1))
    { git diff --name-only dev..."wt/$c" 2>/dev/null || true   # resumed candidates carry real writes
      candidate_tokens "$c"
    } >>"$PASS_SURF"
  fi
done
rm -f "$SURF" "$PASS_SURF"

[ "$DRY" = 1 ] && { echo "(dry-run: no dispatch)"; exit 0; }

# -- park colliders (sentinel claim-lock: the dispatcher skips locked cards;
#    a status flip does NOT protect - unclaimed cards are daemon food), dispatch, restore --
# park only takes NULL/'' locks: a card already holding another sentinel (queue-hold
# from a tick's canonical park) keeps it - overwriting it unparked a parked card live.
# unpark restores NULL, never '': the dispatcher's claim filter is `claim_lock IS NULL`,
# and an '' lock left the card invisible to every later dispatch (Spawned: 0, all buckets empty).
park()   { local p; for p in "$@"; do [ -n "$p" ] || continue; sqlite3 "$DB" "UPDATE tasks SET claim_lock='pibtw:dispatch-hold' WHERE id='$p' AND (claim_lock IS NULL OR claim_lock='');"; done; }
unpark() { local p; for p in "$@"; do [ -n "$p" ] || continue; sqlite3 "$DB" "UPDATE tasks SET claim_lock=NULL WHERE id='$p' AND claim_lock='pibtw:dispatch-hold';"; done; }
park "${PARKED[@]:-}"
trap 'unpark "${PARKED[@]:-}"' EXIT
if [ "${#RUNNING[@]}" -lt "$CAP" ] && [ "$(sqlite3 "$DB" "SELECT COUNT(*) FROM tasks WHERE status='ready';")" -gt 0 ]; then
  SLOTS=$(( CAP - ${#RUNNING[@]} ))
  [ "$SLOTS" -gt 2 ] && SLOTS=2   # deliberate pairing: max 2 spawns per pass
  [ "$MAXARG" -gt 0 ] && { B=$(( MAXARG - ${#RUNNING[@]} )); [ "$B" -lt "$SLOTS" ] && [ "$B" -ge 0 ] && SLOTS=$B; }   # an explicit --max N bounds total concurrency (running + spawns <= N)
  [ "$SLOTS" -le 0 ] && { echo "no dispatch (--max reached: ${#RUNNING[@]} running)"; exit 0; }
  hermes kanban dispatch --max $(( ${#RUNNING[@]} + SLOTS )) 2>&1 | grep -E "Spawned|assigned" || true
else
  echo "no dispatch (running=${#RUNNING[@]}/cap=$CAP)"
fi
sleep 2
exit 0