#!/usr/bin/env bash
# factory-dispatch - collision-aware dispatch for the omega factory
#
# The bare dispatcher takes the OLDEST ready cards and is collision-blind:
# it will spawn a card whose files overlap a running card. This script
# applies the same-file law at dispatch time:
#   1. compute each running card's file surface
#   2. compute each candidate's file surface
#   3. park colliding candidates as 'todo' for the dispatch call, restore after
#   4. dispatch --max CAP (fills free slots with the oldest LEGAL cards)
#
# SURFACE PRECEDENCE - IDENTICAL ON BOTH PATHS (card t_42ff3dad, 2026-11-12):
# a card's surface is its curated `FILES:` line when it carries one (plus the
# move-shaped sources that line may not name), and its body prose ONLY when it
# carries none. WORKTREE EVIDENCE (git diff dev...wt/<id> + status --short) is
# UNIONED in on both paths - a candidate resumed from a dead run and a running
# card both really write those files, and the diff is the only honest evidence
# of what a RUNNING card touches. So a running card never surfaces more than
# its ready self. The guarded asymmetry read a running card's WHOLE body prose
# instead: a card that merely CITED a sibling's directory in its CONTEXT
# claimed that directory the moment it started, so the same pair read
# `sanctioned` at running=0 and `hold: ... (same-file)` once one leg was
# running (live 2026-11-12: t_4bcea3dd's prose quoted
# webui/app/composables/module-panels.generated.ts, which held t_2daae1aa's
# webui/app/components/ui/BandSelect.vue - neither card wrote a file the other
# wrote, and a cap slot idled for that card's whole run). A verdict that
# depends on RUN STATE is the one thing the machine verdict law assumes cannot
# happen. The prose fallback stays for FILES-less cards: a card that declares
# no surface at all is exactly the case where prose is all the guard has.
#
# ponytail: token granularity is body-text paths - a card whose body names
# no paths falls back to class rules (webui+webui allowed, Go beside webui
# allowed) and relies on the worker-side onboarding same-file check. If a
# running card exposes NO surface at all, hold everything (conservative).
# Upgrade path: card bodies grow a FILES: line the script reads first.
# A FILES-less card keeps that prose fallback for a SINGLE dispatch, but the
# prose can never authorize a PAIR: an absent FILES: line is UNPROVABLE, not
# precise (card t_2d0d48ac, 2026-11-11 - the absence used to read as proof).
#
# Usage: scripts/factory-dispatch.sh [--dry-run] [--max N]
set -euo pipefail
cd "$(dirname "$0")/.."

# Usage + strict arg validation BEFORE any state is read (no config, no
# kanban DB, no worktrees, nothing spawns). The original case loop had no
# default-reject branch: every unknown flag fell through silently and the
# script continued into the dispatch path (proven live 2026-11-02:
# `factory-dispatch.sh --help` printed the verdicts and then EXECUTED a
# real spawn - a typo'd flag must never reach the real dispatcher).
usage() {
  cat <<'USAGE_EOF'
factory-dispatch.sh - collision-aware dispatch for the omega factory.

Applies the same-file law at dispatch time: computes each running card's
write surface, holds every ready candidate whose surface collides, then
dispatches the legal remainder through the fence-stamped inner dispatcher
(OMEGA_WORKER=1, repo git shims on PATH). Legal/hold verdicts print per
candidate, followed by the PAIR verdict for the would-spawn head set
(cap minus running, in dispatch order) - `pair: sanctioned|forbidden
(<ids>, <why>)` - so the --max decision reads off the log instead of
judgment. A forbidden pair means at most one of those cards may run, and
the guard ENFORCES it: a live `--max N` (N>1) against a forbidden head
pair is clamped to one card, loudly, with the ids it declined to pair.
A card with NO `FILES:` line is UNPROVABLE for a pair (its prose
tokens still make it legal for a single dispatch) - the verdict names it.
The pass ends with a handoff report (wanted/after/over-run).

The inner dispatcher claims TWO lanes: ready cards, and review-state
cards whose claim_lock is free (`claim_review_task`, review -> running).
The pass gates its dispatch call on exactly that claimable work, so a
board whose ready queue is empty but whose review lane is not still
dispatches the review. The dry-run names both lanes: `candidates: N
ready, unclaimed` and `review candidates: N unclaimed (ids)`. Review
rows are not candidates - they take no hold, no pair slot and no
surface verdict (the inner claims them in its own lane).

Flags:
  --dry-run   evaluate and print verdicts only; no dispatch, no writes
  --max N     pass N as the inner dispatcher's bound for this invocation
              (explicit true concurrency; overrides the config cap)
  -h, --help  this usage text, exit 0

An unknown or malformed flag is refused (exit 2, this usage text to
stderr) before anything happens - it must never fall through to a real
dispatch. Without --max the cap is the LIVE config knob
kanban.max_in_progress_per_profile; it is never hardcoded.

Hermes home + board follow the hermes CLI's own resolution:
HERMES_KANBAN_DB (pins the DB file) > HERMES_KANBAN_BOARD /
HERMES_KANBAN_HOME > HERMES_HOME (a profiles/<name> home names its
root), else windows $LOCALAPPDATA/hermes, else POSIX $HOME/.hermes;
<root>/kanban/current names the active board, else the default board's
DB at <root>/kanban.db. The DB reader is the python sqlite3 module (the
sqlite3 CLI is absent on some fleet hosts). A missing reader, module or
board DB is a LOUD refusal (exit 3, message to stderr) - verdicts are
never printed from empty lists.
USAGE_EOF
}
usage_err() { echo "factory-dispatch.sh: $1" >&2; usage >&2; exit 2; }

# ---- hermes home + board resolution - the CLI's own chain (2026-11-05) ----
# The old hardcoded $HOME/.hermes layout is wrong on windows hosts: the real
# board DB sits under $LOCALAPPDATA/hermes, and the sqlite3 CLI is absent on
# some fleet hosts entirely (proven live 2026-11-05 on winbtw: the guard died
# twice on `sqlite3: command not found`, then printed verdicts computed from
# EMPTY lists - false-legal in the dangerous direction). Resolution mirrors
# hermes' own code (hermes_constants.get_default_hermes_root,
# kanban_db.kanban_home/get_current_board): nothing is hardcoded to one host.
root_of_env() {  # a trailing profiles/<name> segment names its ROOT home
  local h="${1//\\//}"   # windows backslashes -> forward slashes
  if [ "$h" != "$(dirname "$h")" ] && [ "$(basename "$(dirname "$h")")" = profiles ]; then
    h="$(dirname "$(dirname "$h")")"
  fi
  printf '%s' "$h"
}
resolve_hermes() {
  # 1) HERMES_KANBAN_HOME - kanban_home()'s own override
  # 2) HERMES_HOME - a profile-routed host names its root through profiles/
  # 3) platform default: windows $LOCALAPPDATA/hermes, POSIX $HOME/.hermes
  if [ -n "${HERMES_KANBAN_HOME:-}" ]; then ROOT="${HERMES_KANBAN_HOME//\\//}"; return; fi
  if [ -n "${HERMES_HOME:-}" ]; then ROOT=$(root_of_env "$HERMES_HOME"); return; fi
  if [ -n "${LOCALAPPDATA:-}" ]; then ROOT="${LOCALAPPDATA//\\//}/hermes"; else ROOT="$HOME/.hermes"; fi
}
resolve_board() {  # HERMES_KANBAN_DB pins the file > HERMES_KANBAN_BOARD >
                   # <root>/kanban/current (slug with a DB) > default board
  local cand
  if [ -n "${HERMES_KANBAN_DB:-}" ]; then DB="${HERMES_KANBAN_DB//\\//}"; BOARD="<pinned>"; return; fi
  for cand in "${HERMES_KANBAN_BOARD:-}" "$(cat "$ROOT/kanban/current" 2>/dev/null || true)"; do
    [ -n "$cand" ] || continue
    case "$cand" in *[/*\\]*) continue ;; esac   # malformed slug: fall through, never crash
    if [ "$cand" = default ]; then DB="$ROOT/kanban.db"; BOARD="$cand"; return; fi
    if [ -f "$ROOT/kanban/boards/$cand/kanban.db" ]; then
      DB="$ROOT/kanban/boards/$cand/kanban.db"; BOARD="$cand"; return
    fi
  done
  DB="$ROOT/kanban.db"; BOARD="default"
}
die() { echo "factory-dispatch.sh: $1" >&2; exit "${2:-3}"; }
readers_up() {  # loud-refusal gate: no reader, no verdicts from empty lists
  # (FD_PY is a test/ops hook: pin the interpreter, or point it nowhere to
  # exercise the refusal path.)
  PY="${FD_PY:-}"
  if [ -z "$PY" ]; then
    if command -v python3 >/dev/null 2>&1; then PY=python3
    elif command -v python >/dev/null 2>&1; then PY=python; fi
  fi
  [ -n "$PY" ] || die "no python on PATH - the sqlite3-module DB reader is unavailable"
  "$PY" -c 'import sqlite3' >/dev/null 2>&1 \
    || die "python '$PY' lacks the sqlite3 module - DB reader unavailable"
  [ -f "$DB" ] \
    || die "board DB not found at $DB (root '$ROOT', board '$BOARD')"
}
# The DB reader: the python sqlite3 module. One process per statement (the
# pass runs ~15 statements - startup cost is noise against a 60s tick).
# A failed open or statement exits 3 with a named error - set -e then stops
# the pass LOUD. An empty result stays the legitimately-empty-board case.
DBPY='
import sqlite3, sys
sys.stdout.reconfigure(newline="\n")   # windows: no \r\n row translation
path, mode, sql = sys.argv[1], sys.argv[2], sys.argv[3]
what = "write" if mode == "x" else "read"
try:
    con = sqlite3.connect(path, timeout=10)
except sqlite3.Error as exc:
    sys.stderr.write("factory-dispatch.sh: DB open failed: %s\n" % exc)
    sys.exit(3)
try:
    if mode == "x":
        con.execute(sql)
        con.commit()
    else:
        for row in con.execute(sql):
            sys.stdout.write("|".join("" if v is None else str(v) for v in row) + "\n")
except sqlite3.Error as exc:
    sys.stderr.write("factory-dispatch.sh: DB %s failed: %s\n" % (what, exc))
    sys.exit(3)
finally:
    con.close()
'
dbq() { "$PY" -c "$DBPY" "$DB" q "$1"; }
dbx() { "$PY" -c "$DBPY" "$DB" x "$1"; }

# The cap is the LIVE config knob kanban.max_in_progress_per_profile
# (2026-11-05 live: 1; the main session flips it to 2 after this card's
# proof - this reader must always MATCH the live value, never hardcode),
# never a hardcode: a hardcode drifted
# from the law (CAP=4 while the config said 2) and the guard could spawn
# past the sanctioned concurrency when the queue held several legal
# candidates. grep/sed only - no yq dependence. Falls back to 2 when the
# key or the file is absent (sandbox homes, CI runners).
config_cap() {
  local f="$ROOT/config.yaml"
  [ -f "$f" ] || return 0
  sed -n '/^kanban:/,/^[^[:space:]#]/p' "$f" \
    | grep -E '^[[:space:]]*max_in_progress_per_profile:' \
    | head -1 | sed -E 's/^[^:]*:[[:space:]]*//' | tr -d "\"'[:space:]"
}

DRY=0; MAXARG=0
while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY=1 ;;
    -h|--help) usage; exit 0 ;;
    --max)
      [ $# -ge 2 ] || usage_err "--max needs a value"
      case "$2" in ''|*[!0-9]*|0) usage_err "--max must be a positive integer, got '$2'" ;; esac
      MAXARG="$2"; shift ;;
    *) usage_err "unknown argument: $1" ;;
  esac
  shift
done
resolve_hermes
resolve_board
readers_up
CAP=$(config_cap)
case "$CAP" in ''|*[!0-9]*|0) CAP=2 ;; esac
[ "$MAXARG" -gt 0 ] && CAP="$MAXARG"   # --max overrides: N stays a live concurrency cap

move_paths() {  # stdin -> both sides of every move-shaped line, path tokens only
  # A MOVE card writes BOTH sides of every move it declares: the target and
  # the source. The rooted prefix scan in tokens_of can never see sources
  # like webui/ or engine/ (proven live 2026-10-28: the engine-ring move card
  # t_7e47d18f "webui/ -> engine/webui/" spawned beside the running webui
  # settings card t_8ac225f1; the token check read them disjoint and the
  # worker was killed at ~2 min). Shapes: "A -> B", "A => B", bare source
  # lists after "moves" ("moves agent/ ai/ webui/"), "A dissolves into B".
  # Move-shaped detection is LINE-wide: a line that declares a move names its
  # whole write surface. Each extracted token must carry a slash, so prose
  # arrows ("an arrow (`->`, `=>`)") and the literal phrase "moves X to Y"
  # produce nothing. Inflation is the safe direction: extra tokens can only
  # HOLD more cards, never free one.
  grep -Ei '\->|=>|\bmoves\b|\bdissolves? into\b' \
    | grep -oE '[a-zA-Z0-9_.{}-]+/[a-zA-Z0-9_./{}-]*' || true
}

tokens_of() {  # extract repo paths from free text (body or diff)
  # Brace lists (harness/{a,b}/) expand first: a FILES surface naming a
  # brace list must cover every member (proven 2026-10-26: the c2 card's
  # FILES line collapsed to a bare harness/ token and hid harness/cron
  # from the overlap check, guard read a colliding card false-legal).
  # The regex gate bounds eval to path-shaped words: no $, backticks,
  # parens or quotes pass it, so eval can only emit path strings.
  # (bash 5.3: quoted =~ RHS is literal - keep the pattern unquoted-var)
  # Move law runs on the brace-expanded stream: a move-shaped line adds its
  # source paths to the card's write surface - the target side the rooted
  # scan below already catches when it sits under a watched prefix.
  local stream expanded line gate='^[A-Za-z0-9_./{},: -]+$'
  stream=$(cat)
  # move law pass 1 on the RAW text - brace expansion below shatters sentences
  # ("A dissolves into B" becomes one-word lines), killing the phrase match
  move_paths <<<"$stream"
  expanded=$(while IFS= read -r line; do
    if [[ "$line" == *'{'* && "$line" =~ $gate ]]; then
      eval "printf '%s\n' $line" 2>/dev/null || printf '%s\n' "$line"
    else
      printf '%s\n' "$line"
    fi
  done <<<"$stream")
  # move law pass 2 on the expanded stream - brace-listed moves
  # ("harness/{cron,memory}/ -> core/") surface their members' sides
  move_paths <<<"$expanded"
  # The rooted scan must watch every top-level code dir. The list predates
  # the engine-ring reorg: engine/ and core/ were missing, so engine/webui
  # paths produced ZERO tokens on both sides (running surface AND candidate
  # FILES lines). Empty candidate tokens can never overlap, so every webui
  # card read false-legal beside a running webui card (proven 2026-10-28:
  # dry-run called t_210a0d19 legal beside running t_372b9e9a while both
  # name engine/webui/app/components/ and engine/webui/test/).
  printf '%s\n' "$expanded" | grep -oE '\b(harness|modules|cmd|docs|scripts|agent|api|webui|llm|core|deploy|\.github|internal)/[a-zA-Z0-9_./-]+' || true
}

candidate_tokens() {  # id -> WRITE-surface tokens: curated FILES: lines win over body prose
  local body files
  body=$(dbq "SELECT body FROM tasks WHERE id='$1';")
  files=$(grep -i '^FILES:' <<<"$body" | head -1 | cut -d: -f2- || true)
  if [ -n "$files" ]; then
    { tokens_of <<<"$files"
      move_paths <<<"$body"   # a FILES line naming move TARGETS only must not hide the SOURCES
    } | sort -u
  else
    tokens_of <<<"$body"
  fi
}

files_line_absent() {  # id -> 0 (true) when the card declares NO usable FILES: line
  # Distinguishes the two unprovable shapes for the VERDICT TEXT: "absent"
  # names the missing line, "bare dir" names the sloppy token. A FILES: line
  # that carries no value ("FILES:") declares no surface either, so it counts
  # as absent - an empty declaration is not a proof.
  local files
  files=$(dbq "SELECT body FROM tasks WHERE id='$1';" | grep -i '^FILES:' | head -1 | cut -d: -f2- || true)
  [ -z "$files" ]
}

imprecise_surface() {  # id -> 0 (true) when the surface is UNPROVABLE: no FILES:
                       # line at all, or a FILES line naming a BARE directory
  # A bare token ('agent', 'harness', 'core') is a directory name whose real
  # coverage the tokenizer cannot pin: it can swallow every path under it, or
  # nothing. Two go cards sanctioned on those tokens is exactly the false-green
  # shape, so the go+go disjointness proof treats such a surface as UNPROVABLE.
  # The fix is on the card, not the guard: name paths ('agent/api/', '.go' files).
  # ABSENT FILES line = UNPROVABLE (card t_2d0d48ac, 2026-11-11). The old leg
  # said "absent is handled by the empty-token leg" - it is not: candidate_tokens
  # falls back to the WHOLE body prose, so a card that declares no surface at all
  # still carries prose tokens, and this function inspected the FILES line only,
  # found nothing bare, and answered "precise". Net: no FILES line -> prose tokens
  # -> precise -> the pair sanctioned. The card with the LEAST surface discipline
  # got the EASIEST pairing (proven live 2026-11-11: t_cfc2aa58, no FILES line,
  # spawned beside the webui card t_b5784de3 as "FILES disjoint" on a surface
  # nobody proved). The prose fallback stays legal for a SINGLE dispatch; it must
  # never authorize a PAIR.
  local body files tok
  body=$(dbq "SELECT body FROM tasks WHERE id='$1';")
  files=$(grep -i '^FILES:' <<<"$body" | head -1 | cut -d: -f2- || true)
  [ -n "$files" ] || return 0        # no declared surface: nothing to prove with
  for tok in $files; do
    case "$tok" in
      */*|*.*) : ;;                  # path-shaped: precise enough
      *) return 0 ;;                 # bare word: unprovable
    esac
  done
  return 1
}

surface_of_card() {  # id -> file tokens: FILES: line (prose only when absent)
                     # UNION worktree diff + status (card t_42ff3dad)
  # The precedence is candidate_tokens' own - this function used to be the one
  # place that READ DIFFERENTLY (whole body prose, no FILES: preference), so a
  # card that merely CITED a sibling's directory in its CONTEXT claimed that
  # directory the moment it started running, and the sibling went
  # `hold: ... (same-file)` while the same pair had read `sanctioned` with both
  # cards ready. The verdict must not depend on run state. The worktree
  # evidence stays in the union because it is the only honest record of what a
  # RUNNING card actually touches - a diff that writes into a sibling's
  # directory must still hold (the conservative direction). Both git probes are
  # `|| true`: a card with a worktree dir but no branch yet (or a worktree the
  # board no longer has on disk) must not kill the pass on `set -e`.
  local id="$1"
  local wt=".worktrees/$id"
  {
    candidate_tokens "$id"
    if [ -d "$wt" ]; then
      git diff --name-only dev..."wt/$id" 2>/dev/null || true   # three-dot: only THIS branch's changes
      git -C "$wt" status --short 2>/dev/null | awk '{print $NF}' || true   # ...and uncommitted files
    fi
  } | sort -u
}

class_of() {  # id -> webui | go | docs  (FILES: line is the truth; body text
              # only classifies when FILES is absent - out-of-scope prose
              # mentions misclassify cards, proven: a pure-Go TUI card
              # classed webui because its OUT OF SCOPE line said "webui")
  local t files
  t=$(dbq "SELECT lower(title||' '||body) FROM tasks WHERE id='$1';")
  files=$(grep -o 'files:.*' <<<"$t" | head -1)
  if [ -n "$files" ]; then
    if grep -q "webui\|\.ts\|\.vue\|nuxt" <<<"$files"; then echo webui
    # Strip the 'files:' label BEFORE tokenizing: the old sed ran per token and
    # left an EMPTY token behind, which `grep -qv '^docs/'` counted as a
    # non-docs hit - a docs-only FILES line ('FILES: docs/cli.md') classed as
    # go, holding that card (and every later go candidate behind it).
    elif grep -oE '[^ ]+' <<<"${files#files:}" | grep -qv '^docs/'; then echo go
    else echo docs; fi
  # Prose legs match on WORD BOUNDARIES, never bare substrings (card t_64255dca).
  # The old webui leg grepped "webui\|webui\|nuxt\|spa": bare `spa` matches the
  # word "dispatch" ("di"+"spa"+"tch") and "space", and this factory's card prose
  # says "dispatch" constantly - so ANY FILES-less card mentioning a dispatch
  # classed webui whatever its real surface (the t_2d0d48ac incident pair read
  # `classes webui+webui` for exactly this). Class feeds the go+go hold legs, so
  # the misclass let a go-shaped FILES-less candidate skip the class-leg hold
  # beside a running go card. \b keeps every true hit ("engine/webui",
  # "nuxt.config.ts", the word SPA) and drops the substring shapes.
  elif grep -qE 'docs/|\bdocumentation\b' <<<"$t" && ! grep -qE '\bwebui\b' <<<"$t"; then echo docs
  elif grep -qE '\b(webui|nuxt|spa)\b' <<<"$t"; then echo webui
  else echo go; fi
}

overlaps() {  # candidate tokens, running tokens -> hit if any token pair overlaps
  # a token matches if the strings are equal, or one is a directory prefix of
  # the other. Trailing slashes are normalized first - "harness/http_channel/"
  # as a dir token made the prefix pattern "harness/http_channel//*" (double
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
# guard bug #9 (2026-11-01, proven live): the cap gate below read this
# snapshot, and a claim-expired zombie pair (workers dead the same minute,
# pids gone, heartbeats stale) held the gate shut - while the only reaper
# (release_stale_claims) runs INSIDE the dispatch call the gate holds back.
# Deadlock: no pass ever reaped them. The inner counts running AFTER its
# reclaim phase (guard bug #8), so this snapshot must too: a running card
# whose claim_expires elapsed is 'ready' at the inner, not running. Drop it
# here; the gate, the surface holds and the slot math all read the same
# post-reclaim set. A zombie the inner somehow keeps stays bounded -
# DISPATCH_MAX is a hard cap either way.
# guard bug #14 (2026-11-09, proven live 3x as 'silent Spawned-0'): that
# drop rule was WRONG for one shape - a claim-expired row whose worker is
# alive with a fresh heartbeat. The inner does not reclaim those: it
# EXTENDS the claim (kanban_db.py release_stale_claims, ttl_expired path -
# live _worker_alive + heartbeat newer than
# DEFAULT_CLAIM_HEARTBEAT_MAX_STALE_SECONDS = 3600) and the card STAYS
# running, still consuming the very budget this snapshot just freed.
# Snapshot 0 running, inner counts 1-2, cap 2: the whole budget evaporates
# and the pass prints 'Spawned: 0' with every counter empty - twice in one
# tick 2026-11-09 (t_00e4a784 worker + vera review continuation, both
# claim-expired-but-live). Self-healed only when the cards completed.
# The mirror rule: keep an expired row ONLY when the inner would extend
# it - worker_pid registered AND last_heartbeat_at within 3600s. Rows
# without a worker pid, or with a stale/no heartbeat, are reclaimed by
# the inner's heartbeat-stale backstop and stay dropped, as before.
RUNNING_RAW=$(dbq "SELECT id FROM tasks WHERE status='running' AND (claim_expires IS NULL OR claim_expires > strftime('%s','now') OR (worker_pid IS NOT NULL AND last_heartbeat_at IS NOT NULL AND last_heartbeat_at > strftime('%s','now') - 3600)) ORDER BY created_at;")
RUNNING=()
[ -n "$RUNNING_RAW" ] && mapfile -t RUNNING <<<"$RUNNING_RAW"
SURF=/tmp/fd_running_surface.$$; : >"$SURF"
# PER-CARD go surfaces (2026-11-10 ruling): the aggregate SURF cannot prove a
# go pair disjoint - a running go card with no tokens hides behind the doc or
# webui cards that do carry tokens (the T16i shape: three running cards, the
# go one token-less, the candidate read legal). The proof needs the go cards
# counted and tokenised individually.
RUNGO_SURF=/tmp/fd_rungo_surface.$$; : >"$RUNGO_SURF"
RUNGO_CARDS=0; RUNGO_TOKENED=0; RUNGO_IMPRECISE=0; RUNGO_NOFILES=""
for r in "${RUNNING[@]:-}"; do
  [ -z "$r" ] && continue
  surface_of_card "$r" >>"$SURF"
  if [ "$(class_of "$r")" = go ]; then
    RUNGO_CARDS=$((RUNGO_CARDS + 1))
    imprecise_surface "$r" && RUNGO_IMPRECISE=$((RUNGO_IMPRECISE + 1))
    # named in the hold verdict: an absent FILES line is the reason the pair is
    # unprovable, and the log must say which card to fix
    files_line_absent "$r" && RUNGO_NOFILES="$RUNGO_NOFILES $r"
    rgo=$(surface_of_card "$r")
    if [ -n "$rgo" ]; then
      RUNGO_TOKENED=$((RUNGO_TOKENED + 1))
      printf '%s\n' "$rgo" >>"$RUNGO_SURF"
    fi
  fi
done
RUN_HAS_SURFACE=$(grep -q . "$SURF" && echo 1 || echo 0)

# -- slot math: ONE number, shared by the pair verdict and the dispatch call --
# SLOTS is this pass's add-capacity in the guard's own accounting: the cap
# minus the post-reclaim running snapshot (bug #8's snapshot side). The
# dry-run's pair verdict reads it, and the dispatch call below budgets from
# it - a pair verdict computed against a different cap than the pass would
# use is worse than no verdict at all. Explicit --max N bounds TRUE
# concurrency (N total, not N new), so it subtracts the running set the same
# way; without --max the pass never adds more than 2 (the pairing law).
if [ "$MAXARG" -gt 0 ]; then
  SLOTS=$(( MAXARG - ${#RUNNING[@]} ))
else
  SLOTS=$(( CAP - ${#RUNNING[@]} ))
  [ "$SLOTS" -gt 2 ] && SLOTS=2   # deliberate pairing: max 2 spawns per pass
fi
[ "$SLOTS" -lt 0 ] && SLOTS=0

# -- candidates, PRIORITY FIRST then oldest: guard bug #5 (2026-10-26) --
# the sort was pure creation order, so an urgent bug card could never
# jump a queued refactor chain. Higher priority number = more urgent
# (10 = release-blocking class, 5 = standing queue, 0 = default).
# Ties stay oldest-first (FIFO preserved for the default mass).
CAND_RAW=$(dbq "SELECT id FROM tasks WHERE status='ready' AND claim_lock IS NULL ORDER BY priority DESC, created_at;")
CAND=()
[ -n "$CAND_RAW" ] && mapfile -t CAND <<<"$CAND_RAW"
# guard bug #7 (2026-10-30): a ready card carrying a NAMED claim_lock
# (deliberate holds like 'pibtw:v0.14.0-hold') is INVISIBLE to the
# dispatcher (its claim filter is claim_lock IS NULL) - as a candidate
# it was marked legal, held every younger card behind it pairwise, and
# the real dispatch claimed nothing: board deadlock with legal cards
# starving (proven: dry-run 'legal: t_55a3fc4b' beside 2 starved cards).
# board-state snapshot (2026-11-05 card): the tick log must show the REAL
# running set and candidate count - a dry-run whose verdict section is
# empty because the board is empty must be indistinguishable from one
# whose computation went blind.
echo "running: ${RUNNING[*]:-none} (surface tokens: $(grep -c . "$SURF" || true))"

# -- corpse readout: a running card whose worker pid is PROVABLY GONE ---------
# Card t_534d58c5 (2026-11-13). Three cards died mid-pass and left the tick
# nothing to read: worker_result exit_code 1, EMPTY output, worker_lost
# managed_worker_failed (16:30 2026-11-11 run 1334, 21:17 2026-11-12 runs 1397
# and a sibling). The mechanism was the gateway's worker-receipt seam, not a
# kill (see the card: every factory script carries ZERO taskkill/kill calls),
# but the SYMPTOM the operator hit was that a death produced no line anywhere
# the factory reads - the pass printed "running: t_x" for a card whose process
# was already dead, and the next tick's log had to reconstruct the death from
# the board DB by hand.
#
# This is a READ-ONLY readout and it changes NO accounting: the card keeps its
# cap slot (guard bug #14's keep-rule above is untouched - dropping a
# claim-expired-but-live card from the snapshot is what evaporated the budget),
# nothing is reclaimed, requeued, parked or killed. It only names the corpse so
# a death is visible in the pass log the same tick it happens.
#
# The pid check is POSITIVE-EVIDENCE ONLY: a card gets a corpse line only when
# a pid is registered AND the host answers that the pid is gone. No tasklist
# and no kill(0) answer => no line, never a guess. Windows: tasklist CSV, field
# 2 exact (the CSV row is `"image","<pid>",...`; a bare pid substring would
# also match a session number). POSIX: the worker runs as the guard's own user,
# so a failed `kill -0` is ESRCH, not EPERM.
pid_state() {  # pid -> alive | gone | unknown
  local p="$1" out f2
  case "$p" in ''|*[!0-9]*) printf unknown; return ;; esac
  if command -v tasklist >/dev/null 2>&1; then
    # /FI /FO /NH stay intact under MSYS_NO_PATHCONV; `//FI` is refused by the
    # binary itself ("Invalid argument/option") and a REFUSED query must never
    # read as a dead pid - a false corpse is worse than no readout (it fired on
    # two live cards in this readout's first live run, 2026-11-13). tasklist
    # answers rc 0 for "no tasks match" and rc 1 only for a bad invocation, so
    # rc!=0 means UNKNOWN, not gone.
    out=$(MSYS_NO_PATHCONV=1 tasklist /FI "PID eq $p" /FO CSV /NH 2>/dev/null) \
      || { printf unknown; return; }
    # CSV row: "image.exe","<pid>","Console","1","10,960 K" - field 2 must
    # EQUAL the pid; a bare substring also matches a session number.
    f2=$(printf '%s\n' "$out" | head -1 | cut -d, -f2 | tr -d '"')
    if [ "$f2" = "$p" ]; then printf alive; else printf gone; fi
    return
  fi
  if kill -0 "$p" 2>/dev/null; then printf alive; else printf gone; fi
}
corpse_readout() {
  local r row wpid run age prof
  for r in "${RUNNING[@]:-}"; do
    [ -n "$r" ] || continue
    # The task-level read is enough to prove a corpse: registered pid + the
    # heartbeat age. If even THIS fails the pass must keep going - a readout
    # must never be the thing that kills a dispatch pass.
    row=$(dbq "SELECT coalesce(worker_pid,''), coalesce(current_run_id,''),
                      coalesce(CAST(strftime('%s','now') AS INTEGER) - last_heartbeat_at, -1)
               FROM tasks WHERE id='$r';") || continue
    [ -n "$row" ] || continue
    IFS='|' read -r wpid run age <<<"$row"
    [ -n "$wpid" ] || continue                       # no pid registered: nothing to prove
    [ "$(pid_state "$wpid")" = gone ] || continue    # alive or unverifiable: no line
    [ "$age" -ge 0 ] 2>/dev/null || age='?'
    # The run's profile only names WHERE the traceback lives. Boards without a
    # task_runs table (older fixtures) still get the corpse line, minus the
    # pointer - swallowed on purpose, see above.
    prof=$(dbq "SELECT coalesce(profile,'') FROM task_runs WHERE id='${run:-0}';" 2>/dev/null) || prof=""
    echo "corpse: $r run ${run:-?}${prof:+ ($prof)} worker pid $wpid is GONE, last heartbeat ${age}s ago"
    echo "  the run died mid-pass with no result: cause in task_runs.error for run ${run:-?}, last tool calls in $ROOT/profiles/${prof:-<profile>}/logs/agent.log"
  done
}
corpse_readout

echo "candidates: ${#CAND[@]} ready, unclaimed"
# -- review lane: the SAME inner call claims review-state cards ---------------
# `hermes kanban dispatch` never only drained the ready queue: dispatch_once
# enumerates _lane_rows(conn,"review") - `status='review' AND claim_lock IS
# NULL` (hermes_cli/kanban_db_dispatch.py:2296, review loop at 2448) - and
# claims each row through claim_review_task (same file:2125 -> kanban_db.py:
# 2213, an atomic review->running CAS whose `claimed` event carries
# source_status="review"). PROBED for this card (t_3b13064e, 2026-11-11) on a
# scratch board holding ONE review card, zero ready, zero running:
# dispatch_once(dry_run=True) answered spawned=[('t_082b9c1e','vera','')], and
# the live call flipped the row to running with worker_pid set. A free review
# card is therefore claimable work for THIS pass - and the gate below could
# not see it, so on a quiet board (workers hand off together, nothing ready,
# the review lane holds the only work) the pass printed `no dispatch` and the
# reviews could never spawn from it. The gate widens; the law does not: review
# rows are NOT candidates - no per-candidate surface verdict, no hold, no pair
# slot (the inner claims them in its own lane after ready).
REV_RAW=$(dbq "SELECT id FROM tasks WHERE status='review' AND claim_lock IS NULL ORDER BY priority DESC, created_at;")
REV=()
[ -n "$REV_RAW" ] && mapfile -t REV <<<"$REV_RAW"
REVIDS=""
[ "${#REV[@]}" -gt 0 ] && REVIDS=" (${REV[*]})"
echo "review candidates: ${#REV[@]} unclaimed$REVIDS"
PARKED=(); LEGAL=0

# guard bug #4 (2026-10-25) is RETIRED (2026-11-09): hermes kanban
# dispatch once claimed a card that had flipped ready -> review mid-pass
# (t_bc321318, zombie worker on merged work). Today's dispatcher makes
# that impossible: the ready lane claims through an atomic
# ready->running CAS (`_claim_and_open_run(..., "ready", ...)`) so a
# card that flips to review mid-pass simply fails the claim. The park
# built against the old race parked EVERY review-state card for the
# whole dispatch call - blinding the dispatcher's own review lane for
# the same window and starving the reviewer on every guard-driven pass
# (vera's structural starvation, root-caused 2026-11-09). Review cards
# stay unparked: the ready lane cannot touch them (CAS), the review
# lane must see them.
PASS_SURF=$(mktemp); : >"$PASS_SURF"   # surfaces of candidates already marked legal THIS pass (pairwise)
PASSGO_SURF=$(mktemp); : >"$PASSGO_SURF"   # go-class legal candidates only - the same-pass disjointness proof
PASSGO_CARDS=0; PASSGO_TOKENED=0; PASSGO_IMPRECISE=0; PASSGO_NOFILES=""
PASS_CLASS=""   # classes of candidates already marked legal THIS pass
# The would-spawn head set (pair verdict, below): the first SLOTS candidates
# in DISPATCH ORDER that this pass did NOT hold - the cards the inner
# dispatcher can actually take - their classes and their token files.
HEAD=(); HEADCLS=(); HEADDIR=$(mktemp -d)
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
  # candidates skipped the class check entirely). SAME-PASS leg added
  # 2026-11-09 (guard bug #12, proven live: t_2919b7a3 + t_150ce9fc
  # co-spawned as a go+go pair in ONE pass with an empty running set -
  # the running-set check never fired; the same-pass guard below covered
  # files only, not class).
  RC=$(for r in "${RUNNING[@]:-}"; do [ -n "$r" ] && class_of "$r" || true; done | sort -u | tr '\n' ',')
  CC=$(class_of "$c")
  # The class leg is UNCONDITIONAL - it needs no surface tokens (a class is
  # read from FILES/body text), so gating it on RUN_HAS_SURFACE made a go
  # candidate legal beside a running go card that exposed no tokens (the
  # second blind spot, tick 2026-11-07 18:36: 'legal: t_ed045792' beside
  # running go t_3360a8b0). Class-list containment: RC arrives as
  # 'docs,go,webui,' and the comma-wrapped compare keeps 'go' from matching
  # inside another word.
  if [ "$CC" = go ] \
     && { [[ ",$RC," == *,go,* ]] || [[ ",$PASS_CLASS," == *,go,* ]]; }; then
    # go+go SANCTIONED 2026-11-10 (Endo ruling): two Go cards may run together
    # when their file surfaces are PROVABLY disjoint - both sides carry tokens
    # and no token overlaps. The proof is REQUIRED, never assumed: an unknown
    # surface on either side keeps the pair forbidden (a blind go pair is the
    # shape of guard bug #12, where two go cards co-spawned unexamined).
    # Same-pass and running legs are proven separately - a candidate can be
    # disjoint from the running go card yet collide with a same-pass one.
    GODISJ=1
    [ -s "$CTOK" ] || GODISJ=0
    imprecise_surface "$c" && GODISJ=0   # bare-dir FILES line: unprovable, hold
    if [[ ",$RC," == *,go,* ]]; then
      # EVERY running go card must have exposed tokens AND a precise surface -
      # one token-less or bare-dir go card makes the pair unprovable, and the
      # aggregate SURF hid exactly that behind the doc/webui cards that do
      # carry tokens.
      if [ "$RUNGO_CARDS" -gt 0 ] && [ "$RUNGO_TOKENED" -eq "$RUNGO_CARDS" ] \
         && [ "$RUNGO_IMPRECISE" -eq 0 ] && [ -s "$RUNGO_SURF" ]; then
        overlaps "$CTOK" "$RUNGO_SURF" && GODISJ=0
      else
        GODISJ=0
      fi
    fi
    if [[ ",$PASS_CLASS," == *,go,* ]]; then
      if [ "$PASSGO_CARDS" -gt 0 ] && [ "$PASSGO_TOKENED" -eq "$PASSGO_CARDS" ] \
         && [ "$PASSGO_IMPRECISE" -eq 0 ] && [ -s "$PASSGO_SURF" ]; then
        overlaps "$CTOK" "$PASSGO_SURF" && GODISJ=0
      else
        GODISJ=0
      fi
    fi
    if [ "$GODISJ" = 0 ]; then
      HOLD=1; WHY="go-class beside go with unproven surface: running=[$RC] same-pass=[$PASS_CLASS] (disjoint go pairs are sanctioned since 2026-11-10; overlapping or unprovable surfaces stay forbidden)"
      # Name the offending card: an ABSENT `FILES:` line is its own failure (the
      # card declares no surface at all), distinct from a bare-dir token, and the
      # log is the only place the worker learns which card to fix. An empty
      # declaration is named too. No absent line anywhere -> the message above
      # stands unchanged (the bare-dir / token-less shapes).
      NFHITS=""
      files_line_absent "$c" && NFHITS="$NFHITS candidate $c"
      [ -n "$RUNGO_NOFILES" ] && NFHITS="$NFHITS running$RUNGO_NOFILES"
      [ -n "$PASSGO_NOFILES" ] && NFHITS="$NFHITS same-pass$PASSGO_NOFILES"
      [ -n "$NFHITS" ] && WHY="$WHY [absent FILES: line is unprovable, never precise:$NFHITS]"
    fi
  fi
  if [ "$HOLD" = 0 ] && grep -q . "$CTOK"; then
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
  if [ "$HOLD" = 0 ] && [ "${#HEAD[@]}" -lt "$SLOTS" ]; then
    HEAD+=("$c"); HEADCLS+=("$CC")
    cp "$CTOK" "$HEADDIR/${#HEAD[@]}"   # 1-based: HEADDIR/N pairs with HEAD[N-1]
  fi
  rm -f "$CTOK"
  if [ "$HOLD" = 1 ]; then
    echo "hold: $c  ($WHY)"
    PARKED+=("$c")
  else
    echo "legal: $c"
    LEGAL=$((LEGAL+1))
    PASS_CLASS="$PASS_CLASS,$CC"
    if [ "$CC" = go ]; then   # same-pass disjointness proof for the NEXT go candidate
      PASSGO_CARDS=$((PASSGO_CARDS + 1))
      imprecise_surface "$c" && PASSGO_IMPRECISE=$((PASSGO_IMPRECISE + 1))
      files_line_absent "$c" && PASSGO_NOFILES="$PASSGO_NOFILES $c"
      PASSGO_TOK=$(candidate_tokens "$c")
      if [ -n "$PASSGO_TOK" ]; then
        PASSGO_TOKENED=$((PASSGO_TOKENED + 1))
        printf '%s\n' "$PASSGO_TOK" >>"$PASSGO_SURF"
      fi
    fi
    { git diff --name-only dev..."wt/$c" 2>/dev/null || true   # resumed candidates carry real writes
      candidate_tokens "$c"
    } >>"$PASS_SURF"
  fi
done
rm -f "$SURF" "$PASS_SURF"

# ---- pair verdict: the pairing law over the would-spawn head set -----------
# THE incident this card cites (tick 2026-11-07 15:36): running=0, two go
# head cards, `--max 2` - every candidate read legal because each was
# compared against the EMPTY running set only, and the pass spawned a Go+Go
# pair the pairing law forbids. Per-candidate verdicts cannot answer the
# question the --max decision actually asks ("is the PAIR at the head
# legal?"), so the run states it outright:
#
#   pair: sanctioned|forbidden (<ids>, <why>)
#
# HEAD is the head of the LEGAL set - the first SLOTS candidates this pass did
# NOT hold, which is exactly what the inner dispatcher takes once the held
# candidates are parked as todo. Collecting the raw dispatch order instead made
# this line name cards the same pass had already held, so a board with two
# legal cards behind one held card read `forbidden` and the tick passed
# --max 1 with both slots free (proven live 2026-11-10: t_7624bab1 legal,
# t_5a49790b held, t_8fd38f88 legal - pair named the held card, one worker
# spawned). The line answers the --max question, so it may only name cards
# that can actually spawn.
#
# It is still not an echo of the per-candidate verdicts: those compare against
# the running set and an AGGREGATED same-pass surface, while this compares the
# head cards' exact token files against each other - the check that caught the
# aggregate-surface bug (T16i). A hole in the same-pass accounting still reds
# here.
# The verdict errs on the safe side: a forbidden head pair means at most ONE
# of those cards may run, so the tick's mechanical reply is --max 1 (SLOTS
# 1); a sanctioned pair may pass --max 2.
PAIRV=sanctioned; PAIRWHY=""; PAIRIDS=""
if [ "${#HEAD[@]}" -eq 0 ]; then
  PAIRIDS="none"; PAIRWHY="empty would-spawn set"
elif [ "${#HEAD[@]}" -eq 1 ]; then
  PAIRIDS="${HEAD[0]}"; PAIRWHY="single would-spawn candidate"
else
  PAIRIDS="${HEAD[*]}"
  PAIRCLS=$(printf '%s+' "${HEADCLS[@]}"); PAIRCLS="${PAIRCLS%+}"
  PAIRWHY="classes $PAIRCLS, FILES disjoint"
  for ((pi = 0; pi < ${#HEAD[@]}; pi++)); do
    for ((pj = pi + 1; pj < ${#HEAD[@]}; pj++)); do
      # ABSENT-FILES rule (card t_2d0d48ac, 2026-11-11): a card with no usable
      # `FILES:` line declares NO write surface. The prose fallback may still
      # make that card legal on its own (single dispatch), but prose tokens
      # never authorize a PAIR - the surface is unproven, so the pair is
      # forbidden and the verdict names the card that must grow a FILES line.
      # This leg runs BEFORE the class laws and covers EVERY class pair, which
      # is the incident shape: t_cfc2aa58 (no FILES line at all) beside the
      # webui card t_b5784de3 read `sanctioned ... FILES disjoint` because the
      # absence was mistaken for proof. The bare-directory rule above is
      # untouched - a bare token stays a go+go-only blocker.
      if files_line_absent "${HEAD[pi]}"; then
        PAIRV=forbidden; PAIRWHY="no FILES: line on ${HEAD[pi]} - unprovable surface, never a pair"
        break 2
      fi
      if files_line_absent "${HEAD[pj]}"; then
        PAIRV=forbidden; PAIRWHY="no FILES: line on ${HEAD[pj]} - unprovable surface, never a pair"
        break 2
      fi
      # class law BEFORE the token check for EVERY pair: a go+go head is
      # forbidden unless the surfaces are provably disjoint (2026-11-10
      # ruling - before it, class alone forbade the pair).
      if [ "${HEADCLS[pi]}" = go ] && [ "${HEADCLS[pj]}" = go ]; then
        # go+go SANCTIONED 2026-11-10 (Endo ruling) when both head cards carry
        # tokens and none overlap; an unprovable surface keeps the pair
        # forbidden. The same-file check below still runs for a sanctioned
        # pair, so a shared file reds it anyway.
        if [ -s "$HEADDIR/$((pi + 1))" ] && [ -s "$HEADDIR/$((pj + 1))" ] \
           && ! imprecise_surface "${HEAD[pi]}" && ! imprecise_surface "${HEAD[pj]}" \
           && ! overlaps "$HEADDIR/$((pi + 1))" "$HEADDIR/$((pj + 1))"; then
          :   # provably disjoint go pair - fall through to the token check
        else
          PAIRV=forbidden; PAIRWHY="go+go with unproven surface (disjoint go pairs are sanctioned since 2026-11-10)"
          break 2
        fi
      fi
      if overlaps "$HEADDIR/$((pi + 1))" "$HEADDIR/$((pj + 1))"; then
        PAIRV=forbidden; PAIRWHY="same-file"
        while read -r ptok; do
          [ -n "$ptok" ] || continue
          ptok="${ptok%/}"
          if grep -qE "^${ptok}(/|$)" "$HEADDIR/$((pj + 1))"; then
            PAIRWHY="same-file: $ptok"; break
          fi
        done <"$HEADDIR/$((pi + 1))"
        break 2
      fi
    done
  done
fi
# HEAD carries no held cards (they are filtered at collection), so no "held
# this pass" caveat can arise - the members named above are all spawnable.
echo "pair: $PAIRV ($PAIRIDS, $PAIRWHY)"
rm -rf "$HEADDIR"

[ "$DRY" = 1 ] && { echo "cap=$CAP"; echo "(dry-run: no dispatch)"; exit 0; }

# guard bug #11 (2026-10-30): recompute_ready (kanban_db.py:2035) runs on
# every `hermes kanban dispatch` and promotes todo/blocked cards whose
# parents are all done/archived - and a card with NO parents satisfies
# "all parents done" VACUOUSLY. Deliberate operator parks
# (status='blocked' + claim_lock='pibtw:v0.14.0-hold') are direct-SQL
# parks, so they carry no sticky kanban_block event and recompute_ready
# flips them to ready on EVERY pass: the lock survives (the lane filter
# still excludes them - wrong shape, right effect), then the tick
# re-parks. Three re-parks in one tick, 2026-10-29/30, proven live on
# t_55a3fc4b/t_95c91f9c (8 'promoted' events each). hermes kanban
# dispatch is NOT touched (comment scope): the guard takes a pre-pass
# snapshot of every named hold and re-parks precisely what the inner
# sweep flipped, below. A named hold clears by its holder alone - only
# pid-shaped claim_locks are sweep-reapable (bug #8's path, untouched).
# guard bug #13 (2026-10-31): the pre-pass snapshot recorded status
# VERBATIM, so a hold that had ALREADY drifted before the pass (ready +
# named hold, promoted by a recompute_ready OUTSIDE this guard: crashed
# tick, bare kanban CLI call) was snapshotted as the legal baseline and
# the restore saw ready->ready = no drift = never re-parked. Every later
# pass then laundered the same drift (proven live: promote events keep
# firing on t_55a3fc4b/t_95c91f9c after bug #11's fix). Normalize AT
# SNAPSHOT: a named hold whose status is not blocked is already drifted -
# park it immediately and record blocked as the baseline, so the restore
# below guards against the inner sweep re-promoting it (the bug #11 path)
# exactly as before.
HOLDS=()
HOLDSRAW=$(dbq "SELECT id||'|'||status FROM tasks WHERE claim_lock LIKE 'pibtw:%-hold';")
while IFS='|' read -r hid hstat; do
  [ -n "$hid" ] || continue
  if [ "$hstat" != "blocked" ]; then
    dbx "UPDATE tasks SET status='blocked' WHERE id='$hid' AND status='$hstat' AND claim_lock LIKE 'pibtw:%-hold';"
    echo "hold-drift normalized: $hid ($hstat -> blocked, drifted before the pass)"
    hstat=blocked
  fi
  HOLDS+=("$hid|$hstat")
done <<<"$HOLDSRAW"

# -- park colliders (sentinel claim-lock: the dispatcher skips locked cards;
#    a status flip does NOT protect - unclaimed cards are daemon food), dispatch, restore --
# park only takes NULL/'' locks: a card already holding another sentinel (queue-hold
# from a tick's canonical park) keeps it - overwriting it unparked a parked card live.
# unpark restores NULL, never '': the dispatcher's claim filter is `claim_lock IS NULL`,
# and an '' lock left the card invisible to every later dispatch (Spawned: 0, all buckets empty).
park()   { local p; for p in "$@"; do [ -n "$p" ] || continue; dbx "UPDATE tasks SET claim_lock='pibtw:dispatch-hold' WHERE id='$p' AND (claim_lock IS NULL OR claim_lock='');"; done; }
unpark() { local p; for p in "$@"; do [ -n "$p" ] || continue; dbx "UPDATE tasks SET claim_lock=NULL WHERE id='$p' AND claim_lock='pibtw:dispatch-hold';"; done; }
park "${PARKED[@]:-}"
trap 'unpark "${PARKED[@]:-}"' EXIT
# -- claimable-work gate ------------------------------------------------------
# The inner call is made only when the inner can actually CLAIM something:
# a ready card (any lock state - unchanged, the ready lane's own filter
# decides), or a review card whose claim_lock IS NULL (the review lane's
# filter, `_lane_rows(conn,"review")`). The review leg is what a quiet board
# needs: every worker hands off together, the ready queue empties, and the
# review lane holds the only spawnable work - a ready-only gate printed
# `no dispatch` through every quiet cycle and the reviews could not spawn from
# it (probed, see the review-lane block above). Nothing else moves: the cap,
# the park/restore of named holds, the pair verdict and the fence stamp are
# untouched, and review rows never enter the candidate/pair accounting.
CLAIMABLE=$(dbq "SELECT (SELECT COUNT(*) FROM tasks WHERE status='ready') + (SELECT COUNT(*) FROM tasks WHERE status='review' AND claim_lock IS NULL);")
if [ "${#RUNNING[@]}" -lt "$CAP" ] && [ "$CLAIMABLE" -gt 0 ]; then
  DOUT=$(mktemp)
  if [ "$MAXARG" -gt 0 ]; then
    # an explicit --max N bounds TRUE concurrency: pass N through unchanged.
    # The inner dispatcher counts running AFTER its reclaim phase (guard bug #8),
    # so budgeting from this pre-pass snapshot over-counts zombies: still
    # 'running' here, already claimable there. Proven 2026-10-28: --max 1 with
    # 2 zombie-running made B below zero, SLOTS kept its cap-derived 2, the
    # inner got --max 4 on a reclaim-emptied pool and spawned 4.
    DISPATCH_MAX="$MAXARG"
  else
    DISPATCH_MAX=$(( ${#RUNNING[@]} + SLOTS ))
  fi
  # CLAMP (card t_8a1a46e1): the pair verdict above is computed on THIS path
  # too - it was merely printed, and the caller was trusted to read it. On
  # 2026-11-11 a tick batched the dry-run and the real dispatch into one
  # terminal call, so `pair: forbidden` printed and this very `--max 2`
  # spawned both cards anyway (surfaces happened to be disjoint: luck, not
  # law). The verdict is a gate now: a forbidden head pair caps a live pass
  # at ONE card, said out loud with the ids it declined to pair. A sanctioned
  # pair passes N through untouched. The dry-run output stays byte-identical
  # (the clamp sits below the dry exit, and a dry-run never spawns).
  # It sits BELOW the --max branch on purpose: a BARE pass (no --max) computes
  # its own slot count and can equal 2 - leaving it unclamped would keep the
  # hole open behind the "always pass an explicit --max" ritual this card
  # exists to delete.
  if [ "$DISPATCH_MAX" -gt 1 ] && [ "$PAIRV" != sanctioned ]; then
    if [ "$MAXARG" -gt 0 ]; then
      echo "clamp: --max $MAXARG -> 1 (pair: $PAIRV - $PAIRWHY) - declined to pair: $PAIRIDS"
    else
      echo "clamp: wanted $DISPATCH_MAX -> 1 (pair: $PAIRV - $PAIRWHY) - declined to pair: $PAIRIDS"
    fi
    DISPATCH_MAX=1
  fi
  # FENCE STAMP (card t_8b32a09a, Endo ruling 2026-11-02 "bake + fence"):
  # every worker spawned through this call is stamped OMEGA_WORKER=1 and
  # gets repo bin/shims at the FRONT of PATH, so bare `git` inside a
  # worker shell is the fence shim (push + dev/master landings refuse;
  # everything else forwards). Env-scoped BY RULING: the stamp lives only
  # inside this invocation - the tick shell and the main session read no
  # marker and keep landing + pushing by law. The worker env is built
  # from this command's environment, so the stamp and the shim dir
  # survive into the spawned session.
  SHIMDIR="$PWD/bin/shims"
  if [ -x "$SHIMDIR/git" ]; then
    OMEGA_WORKER=1 PATH="$SHIMDIR:$PATH" hermes kanban dispatch --max "$DISPATCH_MAX" >"$DOUT" 2>&1 || true
  else
    # Loud, not silent: an unstamped dispatch here runs the full
    # forbidden tick cycle inside a worker again - the exact failure the
    # fence exists to make impossible (t_529628d5, t_5782d11e).
    echo "FENCE STAMP MISSING: $SHIMDIR/git absent - dispatch ABORTED, pass runs unfenced (restore bin/shims/git)" >&2
    exit 1
  fi
  grep -E "Spawned|assigned" "$DOUT" || true
  # guard bug #8 (2026-10-28, proven twice in one night): the inner dispatcher
  # spawned past the requested total (guard snapshot 1 running, slots 1, --max 2,
  # inner spawned 2). hermes kanban dispatch is NOT touched (comment scope); the
  # guard side owns the accounting. Mechanism, read from hermes source
  # (hermes_cli/kanban_db_dispatch.py): dispatch_once runs _run_reclaim_phase
  # BEFORE _tick_spawn_budget counts - release_stale_claims requeues running
  # cards whose claim_expires elapsed (and stale-heartbeat orphans) back to
  # 'ready', so the inner's budget-time running count sits BELOW this guard's
  # pre-pass snapshot and it fills the freed slots. The guard's snapshot is the
  # stale one. What the guard CAN do: re-read running after the call and report
  # the delta, so over-runs surface in the tick log instead of hiding.
  WANT="$DISPATCH_MAX"
  NOWRUN=$(dbq "SELECT COUNT(*) FROM tasks WHERE status='running';")
  RUNNOW=$(dbq "SELECT id FROM tasks WHERE status='running';")
  NEWRUN=$(comm -13 \
    <(printf '%s\n' "${RUNNING[@]:-}" | sort -u) \
    <(printf '%s\n' "$RUNNOW" | sort -u) \
    | tr '\n' ' ')
  echo "handoff: wanted <=$WANT running (snapshot ${#RUNNING[@]} + slots $SLOTS), after: $NOWRUN (delta $(( NOWRUN - ${#RUNNING[@]} )) since snapshot)"
  [ -n "$NEWRUN" ] && echo "  newly running: $NEWRUN"
  if [ "$NOWRUN" -gt "$WANT" ]; then
    echo "over-run: +$(( NOWRUN - WANT )) past the request - inner count was taken after its reclaim phase (release_stale_claims requeues claim-expired running cards first), so its budget read sat below the guard snapshot"
  fi
  rm -f "$DOUT"
else
  # Name the reason, not just the cap: `running=0/cap=2` on a board whose only
  # work sits in the review lane with a lock, or on a board with no work at
  # all, is the same line - the tick reads claimable to tell them apart.
  echo "no dispatch (running=${#RUNNING[@]}/cap=$CAP, claimable=$CLAIMABLE)"
fi

# sweep repair (guard bug #11): named holds go back to their pre-pass
# status. The restore fires ONLY when the card still carries a named
# hold and its status drifted; a lock the holder cleared mid-pass makes
# the read empty and the card is left alone.
if [ "${#HOLDS[@]}" -gt 0 ]; then
  for hs in "${HOLDS[@]:-}"; do
    [ -n "$hs" ] || continue
    hid="${hs%%|*}"; hwas="${hs#*|}"
    hnow=$(dbq "SELECT status FROM tasks WHERE id='$hid' AND claim_lock LIKE 'pibtw:%-hold';" || true)
    if [ -n "$hnow" ] && [ "$hnow" != "$hwas" ]; then
      dbx "UPDATE tasks SET status='$hwas' WHERE id='$hid' AND status='$hnow';"
      echo "re-parked: $hid ($hnow -> $hwas, named hold lifted by the dispatch sweep)"
    fi
  done
fi

sleep 2
exit 0