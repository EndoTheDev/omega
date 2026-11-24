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
candidate, followed by the head verdict for the would-spawn set (seats
minus running, in dispatch order) - `pair: sanctioned|forbidden (<ids>,
<why>)` for two head cards - so the --max decision reads off the log
instead of judgment. A forbidden pair means at most one of those cards
may run, and the guard ENFORCES the verdict: a live pass against a
forbidden pair is clamped to ONE card, loudly, with the ids it declined.
A card with NO `FILES:` line is UNPROVABLE for a multi-card spawn (its
prose tokens still make it legal for a single dispatch) - the verdict
names it. The would-spawn set is bounded by the implementation seats (2),
so a third legal candidate is an OVER-CAP shape: it stays `legal:` on its
own verdict, takes no slot, and the pass names it in the
`impl lane over-cap:` line.

FACTORY SEATS (2+1, HIS ruling 2026-11-17; the 3+1 ruling of 2026-11-15
was narrowed after three persist-seam waves): 2 implementation seats + 1
review lane = 3 concurrent runs maximum. The config knob
kanban.max_in_progress_per_profile is ONE scalar shared by every
profile, so the 2+1 asymmetry cannot live there - this guard enforces
it. The implementation seats are that knob (clamped to 2: the factory
never runs more), and the review lane is hard-capped at ONE review run.
A second review candidate is HELD - parked, not merely reported - while
a review run is live or while an earlier review candidate claims this
pass, and released the moment the live review finishes. The `impl lane:`
and `review lane:` state lines name the live runs, the cap and the
over-cap verdict in the SAME shape, so a refused run is never silent.

The inner dispatcher claims TWO lanes: ready cards, and review-state
cards whose claim_lock is free (`claim_review_task`, review -> running).
The pass gates its dispatch call on exactly that claimable work, so a
board whose ready queue is empty but whose review lane is not still
dispatches the review. The dry-run names both lanes: `candidates: N
ready, unclaimed`, `review candidates: N unclaimed (ids)` and the
`review lane:` state line (live runs, the one review this pass may
spawn, the ids held back). Review rows are not candidates - they take
no surface verdict and no pair slot (the inner claims them in its
own lane, inside the same budget).


Flags:
  --dry-run   evaluate and print verdicts only; no dispatch, no writes
  --max N     pass N as the inner dispatcher's bound for this invocation
              (explicit true concurrency; overrides the config cap)
  -h, --help  this usage text, exit 0

An unknown or malformed flag is refused (exit 2, this usage text to
stderr) before anything happens - it must never fall through to a real
dispatch. Without --max the cap is the LIVE config knob
kanban.max_in_progress_per_profile; it is never hardcoded.

The cap read REFUSES rather than defaulting (exit 3, one greppable
`CONFIG CAP READ FAILED:` line naming the resolved root and the file it
tried): a dispatch with an unknown cap is a dispatch with an unknown
guard. Every pass that reads it prints `config: cap N from <file>`, and
the seats line names the cap's SOURCE (`config cap N` or `--max N`) so a
caller override can never be read as a config value.
FD_ALLOW_MISSING_CONFIG=1 is the sandbox/CI hook: a throwaway home that
carries no hermes config at all gets the documented floor of 2, loudly
(`CONFIG CAP DEFAULTED:`). It covers an ABSENT file only - a present
config that is unreadable, carries no numeric knob, or names a
non-positive value refuses in every mode.

FD_GH names the gh binary the tip-CI verdict reads through (default `gh`;
the test suite's seam). The pass prints one `tip CI:` line every run and
holds the spawn while the dev tip's `Journeys regression` job is red or in
flight (the merge-before-dispatch law) - docs/pipeline.md. Three verdict
states, never two: `absent` is GitHub ANSWERING with no run, so the guard
stands down; a read that never reached GitHub is `unknown` and proceeds
with a `tip CI: WARN` line, because a failed read is not a green tip. The
hold itself carries one door: a ready card whose declared `FILES:` surface
is entirely under scripts/journeys/recipes/ may still spawn (recipe bytes
touch no runtime, so that card class is the one a red recipe can only be
fixed from inside the hold). Every other candidate stays held.

Hermes home + board follow the hermes CLI's own resolution:
HERMES_KANBAN_DB (pins the DB file) > HERMES_KANBAN_BOARD /
HERMES_KANBAN_HOME > HERMES_HOME (a profiles/<name> home names its
root), else windows $LOCALAPPDATA/hermes, else POSIX $HOME/.hermes;
<root>/kanban/current names the active board, else the default board's
DB at <root>/kanban.db. The DB reader is the python sqlite3 module (the
sqlite3 CLI is absent on some fleet hosts). A missing reader, module or
board DB is a LOUD refusal (exit 3, message to stderr) - verdicts are
never printed from empty lists.

SELF-HEAL (0.17.3): the tick recreates its OWN configured board when that
board is missing, lazily and LOUDLY - `self-heal: board '<name>' was
missing - recreated (<db>)` lands in the pass report, never a silent
fallback to a different board. The default board heals through `hermes
kanban init`, a named board through `hermes kanban boards create`. A
pinned HERMES_KANBAN_DB is the caller's file and is never created here; a
dry-run writes nothing and says what a live pass would recreate.

ONBOARDING AT SPAWN (card t_f6d15b7f): every card a live pass sends
running gets the standing worker-onboarding comment in that SAME pass,
from this guard (`hermes kanban comment <id> "<text>"`), and the receipt
answers the inner dispatcher's `Spawned: N` with `Onboarded: N (<ids>)`
beside it - a failed post is named, never swallowed. The law does not
depend on the hermes spawner (which never posts it - AS-IS ruling
2026-11-13, and NO-UPSTREAM forbids raising it) nor on the tick's hand-
post a minute later, which a resumed or fast-completing worker can
outrun. The body is the tick's standing text byte-for-byte, and the post
is IDEMPOTENT: a card whose LATEST comment is already that text is
skipped by name (`onboard: <id> skipped ...`) instead of doubled, so the
guard and the tick's fallback sweep can both run without producing two
copies of one law.
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
                   # <root>/kanban/current > default board
  # 0.17.3: a NAMED candidate WINS even when its DB is missing. The old chain
  # kept walking and settled on the default board's DB - the tick then read
  # (and dispatched) somebody else's queue while the operator's board sat
  # missing, silently. self_heal_board below is the one place that decides
  # what a missing board means (recreate it, or refuse loudly).
  local cand
  if [ -n "${HERMES_KANBAN_DB:-}" ]; then DB="${HERMES_KANBAN_DB//\\//}"; BOARD="<pinned>"; return; fi
  # <root>/kanban/current is the CLI's own pointer file: ABSENT means the
  # default board (documented, mirrored from kanban_home/get_current_board).
  # An EXISTING pointer that cannot be read is a broken home, never a default -
  # the old `cat ... 2>/dev/null || true` swallowed exactly that and settled on
  # the default board's DB with no word (reader-block audit, card t_0303d69d).
  local ptr=""
  if [ -e "$ROOT/kanban/current" ]; then
    if [ ! -f "$ROOT/kanban/current" ] || [ ! -r "$ROOT/kanban/current" ]; then
      die "board pointer '$ROOT/kanban/current' exists but is not a readable file - refusing to fall back to the default board ($ROOT/kanban.db)" 3
    fi
    ptr=$(cat "$ROOT/kanban/current") \
      || die "board pointer '$ROOT/kanban/current' could not be read - refusing to fall back to the default board ($ROOT/kanban.db)" 3
  fi
  for cand in "${HERMES_KANBAN_BOARD:-}" "$ptr"; do
    [ -n "$cand" ] || continue
    case "$cand" in *[/*\\]*) continue ;; esac   # malformed slug: fall through, never crash
    if [ "$cand" = default ]; then DB="$ROOT/kanban.db"; BOARD="$cand"; return; fi
    DB="$ROOT/kanban/boards/$cand/kanban.db"; BOARD="$cand"; return
  done
  DB="$ROOT/kanban.db"; BOARD="default"
}
die() { echo "factory-dispatch.sh: $1" >&2; exit "${2:-3}"; }
# self_heal_board (0.17.3 make-over) - the tick recreates its OWN configured
# board when that board is missing. Two failures were possible before this:
# the old resolution chain fell through to the DEFAULT board's DB when the
# named board's file was gone (a silent dispatch on somebody else's queue),
# and a home with no kanban.db at all died without saying it could be built.
# The heal is LAZY (only while the file is absent) and LOUD (a pass-report
# line naming the board and the fresh DB) on purpose: "never silent" is the
# law, and the report is the only place the operator reads the tick.
#
#   default board -> `hermes kanban init`            (<root>/kanban.db)
#   named board   -> `hermes kanban boards create`  (<root>/kanban/boards/<slug>/kanban.db)
#
# A pinned HERMES_KANBAN_DB is the CALLER's file: this script never creates
# one (the pin means "read exactly this"). A dry-run writes nothing - it
# names what a live pass would recreate and lets readers_up refuse.
self_heal_board() {
  [ -f "$DB" ] && return 0
  local wanted="$BOARD"
  [ "$wanted" = "<pinned>" ] && die "board DB not found at $DB (HERMES_KANBAN_DB is pinned - the tick never creates a pinned file)" 3
  if [ "$DRY" = 1 ]; then
    echo "self-heal: board '$wanted' is missing - a live pass recreates it ($DB)"
    return 0
  fi
  if [ "$wanted" = default ]; then
    hermes kanban init >/dev/null 2>&1 || true
  else
    hermes kanban boards create "$wanted" >/dev/null 2>&1 || true
  fi
  [ -f "$DB" ] || die "self-heal failed: board '$wanted' was missing and could not be recreated ($DB)" 3
  echo "self-heal: board '$wanted' was missing - recreated ($DB)"
}
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
# candidates. grep/sed only - no yq dependence.
#
# NEVER A SILENT DEFAULT (card t_0303d69d). The old reader answered "" for
# every failure (`[ -f "$f" ] || return 0`) and the caller's `case "" -> CAP=2`
# turned "the cap could not be read" into "the cap is 2" without a word. The
# ticket's tightest live receipt (2026-11-17 03:03 vs 03:07, SAME shell, same
# cwd, no config edit): the dry-run printed `seats: 3 implementation + 1 review
# = 4 max (config cap 3)` and the `--max 1` pass four minutes later printed
# `seats: 1 implementation + 1 review = 2 max (config cap 1)`. Both reads
# returned 3 - the seats line LABELLED the caller's --max as a config cap, so an
# override and a lost read were the same text, and an operator lost an
# implementation seat chasing a read that never failed. Two laws answer both
# halves:
#   1. a failed read REFUSES (one greppable `CONFIG CAP READ FAILED:` line
#      naming the resolved ROOT and the file it tried, exit 3, no dispatch) -
#      a dispatch with an unknown cap is a dispatch with an unknown guard;
#   2. a successful read is NAMED in the log block (`config: cap N from <file>`)
#      and the seats line states the cap's SOURCE (`config cap N` vs `--max N`).
# FD_ALLOW_MISSING_CONFIG=1 is the SANDBOX hook (test/ops, same shape as FD_PY):
# fixtures and CI runners keep a board under a throwaway home with no hermes
# config at all, and there the floor of 2 is the documented behavior - never on
# a live factory host. It covers an ABSENT file ONLY: a present config that is
# unreadable, carries no numeric knob, or names a non-positive value is DRIFT
# and refuses in every mode.
#
# READER-BLOCK AUDIT (same card): every other read in this block feeds a PATH,
# not a guard number, and each already has a loud door - self_heal_board()
# recreates a missing board by name or refuses, readers_up() refuses a missing
# DB or reader, and resolve_board() settles on the default board only when the
# pointer file is ABSENT (the CLI's own default-board signal). The one silent
# read left in that chain - an EXISTING <root>/kanban/current that cannot be
# read - now refuses in place (see resolve_board).
CONFIG_CAP_FAILED='CONFIG CAP READ FAILED:'
config_cap() {  # -> the cap on stdout; nonzero when it cannot be named (caller exits 3)
  local f="${ROOT%/}/config.yaml" val
  if [ -z "${ROOT:-}" ] || [ ! -d "$ROOT" ]; then
    echo "$CONFIG_CAP_FAILED root '${ROOT:-<unset>}' is not a directory - no config to read (tried '$f')" >&2
    return 3
  fi
  if [ ! -e "$f" ]; then
    if [ "${FD_ALLOW_MISSING_CONFIG:-}" = 1 ]; then
      echo "CONFIG CAP DEFAULTED: '$f' is absent (FD_ALLOW_MISSING_CONFIG=1) - sandbox floor cap 2" >&2
      printf '2'
      return 0
    fi
    echo "$CONFIG_CAP_FAILED '$f' is absent (root '$ROOT') - a dispatch with an unknown cap is a dispatch with an unknown guard" >&2
    return 3
  fi
  if [ ! -f "$f" ] || [ ! -r "$f" ]; then
    echo "$CONFIG_CAP_FAILED '$f' is not a readable regular file (root '$ROOT') - a dispatch with an unknown cap is a dispatch with an unknown guard" >&2
    return 3
  fi
  val=$(sed -n '/^kanban:/,/^[^[:space:]#]/p' "$f" \
    | grep -E '^[[:space:]]*max_in_progress_per_profile:' \
    | head -1 | sed -E 's/^[^:]*:[[:space:]]*//' | tr -d "\"'[:space:]")
  case "$val" in
    ''|*[!0-9]*|0)
      echo "$CONFIG_CAP_FAILED '$f' carries no usable kanban.max_in_progress_per_profile (read '${val:-<empty>}') - a dispatch with an unknown cap is a dispatch with an unknown guard" >&2
      return 3 ;;
  esac
  printf '%s' "$val"
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
self_heal_board
readers_up
# The cap read REFUSES rather than defaulting (config_cap: one
# `CONFIG CAP READ FAILED:` line naming ROOT and the file it tried, exit 3
# here) - a pass that cannot name its cap does not dispatch, and it names the
# file it DID read on the success path (the `config:` log line below).
CAPFILE="${ROOT%/}/config.yaml"
CFG_CAP=$(config_cap) || exit 3
# The sandbox floor names its own absence: the log must never claim a file it did
# not read (config_cap already said so on stderr; this keeps the report honest).
CAPFROM="$CAPFILE"
if [ ! -e "$CAPFILE" ]; then
  CAPFROM="<absent: $CAPFILE> (sandbox floor, FD_ALLOW_MISSING_CONFIG=1)"
fi
CAP="$CFG_CAP"; CAPSRC="config cap $CFG_CAP"
if [ "$MAXARG" -gt 0 ]; then
  CAP="$MAXARG"; CAPSRC="--max $MAXARG"   # --max overrides: N stays a live concurrency cap
fi

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
  # EVERY `FILES:` line, joined into one value (card t_5389fd95). The old
  # `head -1` read made lines 2..N invisible to the hold verdict: a card
  # minted one-path-per-line under-declared its surface, so it read `legal`
  # beside a running card that owned a file on its 4th line (live
  # 2026-11-13 23:41: t_20fd8745 / t_d04a1c80 both legal while running
  # t_81d0901b owned webui/app/app.vue). The SINGLE-LINE shape stays the
  # documented contract (whitespace-separated paths, no commas); the union
  # read is defensive, not a new format.
  files=$(grep -i '^FILES:' <<<"$body" | cut -d: -f2- | tr '\n' ' ' || true)
  if [ -n "${files//[[:space:]]/}" ]; then   # whitespace-only = no declaration
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
  # UNION of every `FILES:` line (card t_5389fd95): a body carrying one bare
  # `FILES:` line plus one with paths is NOT absent, and the old `head -1`
  # read answered the wrong way for the reverse order too.
  files=$(dbq "SELECT body FROM tasks WHERE id='$1';" | grep -i '^FILES:' | cut -d: -f2- | tr '\n' ' ' || true)
  [ -z "${files//[[:space:]]/}" ]
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
  # EVERY declared `FILES:` line (card t_5389fd95): a bare-dir token on line 2
  # makes the surface exactly as unprovable as one on line 1, and the old
  # `head -1` read answered "precise" for the split shape.
  files=$(grep -i '^FILES:' <<<"$body" | cut -d: -f2- | tr '\n' ' ' || true)
  [ -n "${files//[[:space:]]/}" ] || return 0   # no declared surface: nothing to prove with
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
  # EVERY `files:` match, labels stripped per line and joined (card
  # t_5389fd95): the class must come from the FULL declared surface, so a
  # card whose first FILES line names docs/ and whose second names a .vue
  # classes webui, not docs. The label is stripped BEFORE the join - a
  # surviving mid-string `files:` token would count as a non-docs hit in the
  # go leg below.
  files=$(grep -o 'files:.*' <<<"$t" | sed 's/^files:[[:space:]]*//' | tr '\n' ' ')
  if [ -n "${files//[[:space:]]/}" ]; then
    if grep -q "webui\|\.ts\|\.vue\|nuxt" <<<"$files"; then echo webui
    # Labels are already stripped above; the empty-token trap the old sed
    # left behind (an EMPTY token counted as a non-docs hit, classing a
    # docs-only FILES line as go) cannot come back through this read.
    elif grep -oE '[^ ]+' <<<"$files" | grep -qv '^docs/'; then echo go
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

# -- review lane: live runs + claimable rows (read BEFORE the slot math) ------
# `hermes kanban dispatch` never only drained the ready queue: dispatch_once
# enumerates _lane_rows(conn,"review") - `status='review' AND claim_lock IS
# NULL` (hermes_cli/kanban_db_dispatch.py:2296, review loop at 2448) - and
# claims each row through claim_review_task (same file:2125 -> kanban_db.py:
# 2213, an atomic review->running CAS whose `claimed` event carries
# source_status="review"). PROBED for card t_3b13064e (2026-11-11) on a
# scratch board holding ONE review card, zero ready, zero running:
# dispatch_once(dry_run=True) answered spawned=[('t_082b9c1e','vera','')], and
# the live call flipped the row to running with worker_pid set. A free review
# card is therefore claimable work for THIS pass - and the gate below could
# not see it, so on a quiet board (workers hand off together, nothing ready,
# the review lane holds the only work) the pass printed `no dispatch` and the
# reviews could never spawn from it. The gate widens; the law does not: review
# rows are NOT candidates - no per-candidate surface verdict, no hold, no
# pair slot (the inner claims them in its own lane after ready).
#
# The AGGREGATE review count is not the cap. The 2+1 factory ruling
# (2026-11-17 evening, narrowing the 2026-11-15 3+1 ruling) keeps the review
# lane a SINGLE seat, and it is read from the
# board, not from a caller: a REVIEW RUN is a running card whose CURRENT run
# was opened by claim_review_task. kanban_db._claim_and_open_run(...,"review",
# ...) stamps the run's `claimed` event with source_status="review"
# (kanban_db.py:2236) and _retry_status_for_run (kanban_db.py:2246) reads
# exactly that field to tell a reviewer run from an implementation run, so
# task_events is the one honest witness - the status column alone cannot tell
# (both lanes end in status='running'). A board with no task_events table
# (test fixtures, pre-event boards) cannot prove a review run at all: the lane
# then reads as free and the pass SAYS SO in its state line, rather than
# holding every review on a guess.
REV_RAW=$(dbq "SELECT id FROM tasks WHERE status='review' AND claim_lock IS NULL ORDER BY priority DESC, created_at;")
REV=()
[ -n "$REV_RAW" ] && mapfile -t REV <<<"$REV_RAW"
RUNREV_RAW=$(dbq "SELECT DISTINCT t.id FROM tasks t JOIN task_events e ON e.task_id=t.id AND e.kind='claimed' AND e.run_id=t.current_run_id WHERE t.status='running' AND REPLACE(e.payload,' ','') LIKE '%\"source_status\":\"review\"%';" 2>/dev/null) \
  || RUNREV_RAW="__unprovable__"
REV_LANE_NOTE=""
case "$RUNREV_RAW" in
  __unprovable__) RUNREV_RAW=""; REV_LANE_NOTE=" (no task_events read - live review runs unprovable)";;
esac
RUNREV=()
[ -n "$RUNREV_RAW" ] && mapfile -t RUNREV <<<"$RUNREV_RAW"
RUNREV_N=${#RUNREV[@]}
RUNREV_IDS="${RUNREV[*]:-none}"
RUNIMPL_N=$(( ${#RUNNING[@]} - RUNREV_N ))
[ "$RUNIMPL_N" -lt 0 ] && RUNIMPL_N=0
# The IMPLEMENTATION runs, by id: the running set minus the review runs. The
# `impl lane:` state line names them the way the review line names `t_rvr`,
# because "live=2 cap=2, over-cap" is unreadable without knowing WHICH two.
RUNIMPL_IDS=""
for r in "${RUNNING[@]:-}"; do
  [ -n "$r" ] || continue
  is_rev=0
  for rv in "${RUNREV[@]:-}"; do
    [ "$rv" = "$r" ] && { is_rev=1; break; }
  done
  [ "$is_rev" = 1 ] || RUNIMPL_IDS="$RUNIMPL_IDS $r"
done
RUNIMPL_IDS="${RUNIMPL_IDS# }"

# -- factory seats (2+1, HIS ruling 2026-11-17): 2 implementation + 1 review --
# The knob is a CONCURRENCY scalar for every profile, so the asymmetry lives
# here. Implementation seats = the knob, clamped to 2: a config value of 3 or
# more must not buy a third implementation card (the total would be 4 with the
# review lane), and the clamp is LOUD rather than silent.
#
# WHY 2 AND NOT 3 (the narrowing that retired the 3+1 ruling of 2026-11-15):
# three persist-seam waves in one evening (20:46, 21:11, 21:46 - the third with
# ZERO tick-side load) proved that three concurrent workers running gate suites
# stall the gateway's worker.persist receipts. Serializing one seat buys a seam
# that never sees its failure shape; the knob kanban.max_in_progress_per_profile
# already carries 2, and this clamp is the second, independent wall so a config
# value of 4 cannot walk it back open.
IMPL_SEATS="$CAP"; SEAT_NOTE=""
if [ "$IMPL_SEATS" -gt 2 ]; then
  IMPL_SEATS=2
  # CAPSRC, not the bare number: under --max the clamp is the CALLER's number,
  # and calling it `config cap` is how the 2026-11-16 ticket read an override as
  # a lost config read.
  SEAT_NOTE="$CAPSRC > 2 - implementation seats clamped to 2"
fi
REVIEW_SEATS=1
TOTAL_SEATS=$(( IMPL_SEATS + REVIEW_SEATS ))

# -- review-lane cap 1: ONE review run at a time ----------------------------
# Enforced by PARKING, not by reporting: the review lane's claim filter is
# `claim_lock IS NULL`, so a sentinel lock hides a row from it exactly the way
# it hides a ready collider from the ready lane. Held rows are released by the
# EXIT trap below, so the next pass (after the live review finishes) sees them
# again - the release is not a timer, it is the absence of a live review run.
REV_SPAWN=""; REVHOLD=""; REV_HOLD_WHY=""
if [ "${#REV[@]}" -gt 0 ]; then
  if [ "$RUNREV_N" -gt 0 ]; then
    REVHOLD="${REV[*]}"
    REV_HOLD_WHY="live review run ($RUNREV_IDS) - one review at a time"
  else
    REV_SPAWN="${REV[0]}"
    if [ "${#REV[@]}" -gt 1 ]; then
      REVHOLD="${REV[*]:1}"
      REV_HOLD_WHY="lane cap 1 - only $REV_SPAWN may claim a review this pass"
    fi
  fi
fi

# -- slot math: ONE number, shared by the head verdict and the dispatch call --
# BUDGET is this pass's add-capacity under the 2+1 invariant: --max N stays a
# TRUE concurrency cap (running + spawns), a bare pass fills the factory seats
# ($TOTAL_SEATS). The review seat is reserved FIRST because the inner does the
# same (dispatch_once narrows ready_budget to spawn_budget - 1 while spawnable
# review work exists), so an implementation backlog can never starve the
# reviewer; the implementation slots are then bounded both by what is left of
# the budget and by the implementation seats the running set leaves free.
if [ "$MAXARG" -gt 0 ]; then
  BUDGET=$(( MAXARG - ${#RUNNING[@]} ))
else
  BUDGET=$(( TOTAL_SEATS - ${#RUNNING[@]} ))
fi
[ "$BUDGET" -lt 0 ] && BUDGET=0
REV_SLOTS=0
[ -n "$REV_SPAWN" ] && REV_SLOTS=1
[ "$REV_SLOTS" -gt "$BUDGET" ] && REV_SLOTS=0   # no seat left: the review waits too
IMPL_ROOM=$(( IMPL_SEATS - RUNIMPL_N ))
[ "$IMPL_ROOM" -lt 0 ] && IMPL_ROOM=0
SLOTS=$(( BUDGET - REV_SLOTS ))
[ "$SLOTS" -gt "$IMPL_ROOM" ] && SLOTS=$IMPL_ROOM
[ "$SLOTS" -lt 0 ] && SLOTS=0
# NO THIRD SLOT EXISTS (2+1 law, 2026-11-17). SLOTS is bounded by IMPL_ROOM,
# which is derived from IMPL_SEATS (hard-clamped to 2), so the head set is a
# PAIR at most and the old `SLOTS > 3` ceiling - and the `trio:` verdict that
# needed it - is gone with the third implementation seat. A THIRD legal
# candidate is not dropped in silence: the candidate loop records every legal
# card it could not seat and the `impl lane over-cap:` line names them.
# A review candidate with no seat left is HELD, not promised: the state line
# must never name a spawn the budget cannot fund (verdict-beside-the-action is
# exactly the failure this factory closed on the pair clamp).
if [ -n "$REV_SPAWN" ] && [ "$REV_SLOTS" -eq 0 ]; then
  REVHOLD="${REV[*]}"; REV_SPAWN=""
  REV_HOLD_WHY="no seat left this pass (running ${#RUNNING[@]} of $TOTAL_SEATS factory seats)"
fi

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
# implementation-seat state: the live implementation runs, the cap, and what is
# left. Mirrors the review-lane line below in the SAME shape (2+1 law,
# 2026-11-17): a refused third implementation run reads as a clamp in the pass
# report, never as a silence. Both counts come from the board (RUNIMPL_* above),
# never from a caller.
IMPLIDS=""
[ -n "$RUNIMPL_IDS" ] && IMPLIDS=" ($RUNIMPL_IDS)"
if [ "$RUNIMPL_N" -ge "$IMPL_SEATS" ]; then
  echo "impl lane: live=$RUNIMPL_N$IMPLIDS cap=$IMPL_SEATS - no seat free, a further implementation run is over-cap ($IMPL_SEATS implementation + $REVIEW_SEATS review = $TOTAL_SEATS max)"
else
  echo "impl lane: live=$RUNIMPL_N$IMPLIDS cap=$IMPL_SEATS - room: $IMPL_ROOM"
fi
# review-lane state: the live review runs, the ONE review this pass may spawn,
# and the rows held back by the lane cap (read above, before the slot math).
REVIDS=""
[ "${#REV[@]}" -gt 0 ] && REVIDS=" (${REV[*]})"
echo "review candidates: ${#REV[@]} unclaimed$REVIDS"
if [ "$RUNREV_N" -gt 0 ]; then
  echo "review lane: live=$RUNREV_N ($RUNREV_IDS) cap=1 - a second review is HELD ($IMPL_SEATS implementation + $REVIEW_SEATS review = $TOTAL_SEATS max)"
elif [ -n "$REV_SPAWN" ]; then
  echo "review lane: live=0 cap=1 - spawn: $REV_SPAWN$REV_LANE_NOTE"
else
  echo "review lane: live=0 cap=1 - spawn: none$REV_LANE_NOTE"
fi
[ -n "$REVHOLD" ] && echo "review lane hold: $REVHOLD ($REV_HOLD_WHY)"
# The cap PROVENANCE line (card t_0303d69d): every pass names the file the cap
# came from, so the next "cap 3 here, cap 1 there" ticket is diagnosable from
# the log alone. Under --max the seats line names the CALLER as the source - the
# old text said `config cap $CAP` either way, which is exactly the confusion the
# ticket was filed on.
if [ "$MAXARG" -gt 0 ]; then
  echo "config: cap $CFG_CAP from $CAPFROM (--max $MAXARG overrides this pass)"
else
  echo "config: cap $CFG_CAP from $CAPFROM"
fi
if [ -n "$SEAT_NOTE" ]; then
  echo "seats: $SEAT_NOTE ($IMPL_SEATS implementation + $REVIEW_SEATS review = $TOTAL_SEATS max)"
else
  echo "seats: $IMPL_SEATS implementation + $REVIEW_SEATS review = $TOTAL_SEATS max ($CAPSRC)"
fi
PARKED=(); LEGAL=0; UNSLOTTED=()

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
#
# The ONE park that survives (2026-11-15 lane-cap ruling, unchanged by the
# 2+1 narrowing of 2026-11-17) is the lane CAP, not
# the old race: a second review candidate is held behind the sentinel while a
# review run is live (or while an earlier review candidate claims this pass),
# and released by the EXIT trap. That is a capacity hold - the ready lane still
# cannot touch a review row - and it is the only machine enforcement of
# "vera runs at most 1 review at a time".
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
  elif [ "$HOLD" = 0 ]; then
    # Legal on its own verdict, but the pass has no implementation slot for it
    # (all seats live, or the caller's --max asked for fewer). That is the
    # over-cap shape under the 2+1 law: named below, never dropped in silence.
    UNSLOTTED+=("$c")
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

# ---- head verdict: the pairing law over the would-spawn head set -----------
# THE incident this card cites (tick 2026-11-07 15:36): running=0, two go
# head cards, `--max 2` - every candidate read legal because each was
# compared against the EMPTY running set only, and the pass spawned a Go+Go
# pair the pairing law forbids. Per-candidate verdicts cannot answer the
# question the --max decision actually asks ("is the PAIR at the head
# legal?"), so the run states it outright:
#
#   pair: sanctioned|forbidden (<ids>, <why>)        the head set (one or two)
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
#
# THE HEAD SET IS A PAIR AT MOST (2+1 law, HIS ruling 2026-11-17 evening). The
# 3+1 ruling of 2026-11-15 carried a `trio:` verdict that sanctioned three
# concurrent implementation cards on a mutually-disjoint proof; the third
# implementation seat is gone (three concurrent workers running gate suites
# stall the gateway's worker.persist receipts - three persist-seam waves in one
# evening, the last with ZERO tick-side load). So a would-be trio is now an
# OVER-CAP shape, not a verdict: the seat math hands the head the largest pair
# the seats can fund, this verdict decides that pair, and every legal candidate
# left without a slot is named by the `impl lane over-cap:` line. Pairs are
# untouched - the go+go disjointness proof, the bare-dir/token proof and the
# absent-FILES rule all behave exactly as they did.
#
# The verdict CLAMPS the spawn (machine-enforced, never merely printed):
#   sanctioned pair -> that many cards may spawn
#   forbidden pair  -> ONE card
# The reduced set is what the inner dispatcher is budgeted, and the head cards
# the verdict declined are parked so a skipped head card cannot let the inner
# fall through to a card no verdict cleared.
WHYLEG=""
head_leg_ok() {  # $1 $2 head indexes -> 0 provable+disjoint, 1 with WHYLEG said
  local pi="$1" pj="$2" ptok a b
  a="${HEAD[pi]}"; b="${HEAD[pj]}"
  # ABSENT-FILES rule (card t_2d0d48ac, 2026-11-11): a card with no usable
  # `FILES:` line declares NO write surface. The prose fallback may still make
  # that card legal on its own (single dispatch), but prose tokens never
  # authorize a multi-card spawn - the surface is unproven, so the leg is
  # forbidden and the verdict names the card that must grow a FILES line. This
  # leg runs BEFORE the class laws and covers EVERY class pair: t_cfc2aa58 (no
  # FILES line at all) beside the webui card t_b5784de3 read `sanctioned ...
  # FILES disjoint` because the absence was mistaken for proof.
  files_line_absent "$a" && { WHYLEG="no FILES: line on $a - unprovable surface, never a pair"; return 1; }
  files_line_absent "$b" && { WHYLEG="no FILES: line on $b - unprovable surface, never a pair"; return 1; }
  # class law BEFORE the token check for EVERY pair: a go+go pair is forbidden
  # unless the surfaces are provably disjoint (2026-11-10 ruling - before it,
  # class alone forbade the pair). The proof is demanded only when BOTH sides
  # are go; a bare-dir/token-less surface keeps the pair forbidden.
  if [ "${HEADCLS[pi]}" = go ] && [ "${HEADCLS[pj]}" = go ]; then
    if ! { [ -s "$HEADDIR/$((pi + 1))" ] && [ -s "$HEADDIR/$((pj + 1))" ] \
           && ! imprecise_surface "$a" && ! imprecise_surface "$b" \
           && ! overlaps "$HEADDIR/$((pi + 1))" "$HEADDIR/$((pj + 1))"; }; then
      WHYLEG="go+go with unproven surface (disjoint go pairs are sanctioned since 2026-11-10)"
      return 1
    fi
  fi
  if overlaps "$HEADDIR/$((pi + 1))" "$HEADDIR/$((pj + 1))"; then
    WHYLEG="same-file"
    while read -r ptok; do
      [ -n "$ptok" ] || continue
      ptok="${ptok%/}"
      if grep -qE "^${ptok}(/|$)" "$HEADDIR/$((pj + 1))"; then
        WHYLEG="same-file: $ptok"; break
      fi
    done <"$HEADDIR/$((pi + 1))"
    return 1
  fi
  return 0
}

PAIRV=sanctioned; PAIRWHY=""; PAIRIDS=""
# VERDICT_CAP = the most implementation cards the verdict sanctions this pass;
# CLAMP_* describe the reduction for the LIVE clamp line only (the dry-run
# prints the verdict itself and never a clamp - see the dry block below).
VERDICT_CAP=$SLOTS; CLAMP_REASON=""; CLAMP_NOUN=""; CLAMP_DECLINED=""
if [ "${#HEAD[@]}" -eq 0 ]; then
  PAIRIDS="none"; PAIRWHY="empty would-spawn set"
  echo "pair: $PAIRV ($PAIRIDS, $PAIRWHY)"
elif [ "${#HEAD[@]}" -eq 1 ]; then
  PAIRIDS="${HEAD[0]}"; PAIRWHY="single would-spawn candidate"
  echo "pair: $PAIRV ($PAIRIDS, $PAIRWHY)"
else
  # SLOTS never exceeds the implementation seats (hard-clamped to 2), so HEAD
  # holds exactly two here: the head set is a PAIR, and this line answers the
  # whole question. A third legal candidate never reaches HEAD - it is named by
  # the over-cap line below (see the slot-math law above).
  PAIRIDS="${HEAD[*]}"
  PAIRCLS=$(printf '%s+' "${HEADCLS[@]}"); PAIRCLS="${PAIRCLS%+}"
  if head_leg_ok 0 1; then
    PAIRWHY="classes $PAIRCLS, FILES disjoint"
  else
    PAIRV=forbidden; PAIRWHY="$WHYLEG"; VERDICT_CAP=1
    CLAMP_REASON="pair: $PAIRV - $PAIRWHY"
    CLAMP_NOUN="declined to pair: "; CLAMP_DECLINED="$PAIRIDS"
  fi
  echo "pair: $PAIRV ($PAIRIDS, $PAIRWHY)"
fi
# HEAD carries no held cards (they are filtered at collection), so no "held
# this pass" caveat can arise - the members named above are all spawnable.
rm -rf "$HEADDIR"

# -- over-cap readout: the legal candidates this pass could not seat ----------
# The 2+1 clamp is machine-enforced by the slot math, but "refused" has to be
# READABLE or it reads as a lost card: a candidate that is `legal:` on its own
# verdict yet takes no head slot (every implementation seat live, or the caller
# asked for fewer with --max) is named here with the arithmetic that refused
# it. Same law as the review-lane hold line: the verdict never sits silent
# beside the action. Printed on BOTH paths, above the dry exit, so a dry-run
# and a live pass agree on who was refused.
if [ "${#UNSLOTTED[@]}" -gt 0 ]; then
  echo "impl lane over-cap: ${UNSLOTTED[*]} - legal, but no implementation slot this pass ($SLOTS head slot(s), $RUNIMPL_N of $IMPL_SEATS seats live)"
fi

# ---- tip CI verdict: the CI-owned journeys gate (card t_a78c6f3a) ---------
# Every push to dev triggers the ci.yml `Journeys regression` job, so the LOCAL
# battery can no longer say anything about the browser legs - and a pass that
# never reads them would pile local-green landings onto a tip whose suite is
# unproven. The merge-before-dispatch law gains this leg: read the dev tip's
# verdict BEFORE anything spawns and hold the pass while it is red or in
# flight. Green or absent (no run for that sha: a docs-only push, or a
# workflow revision older than the job) proceeds. An unreadable verdict (no
# gh, no auth, no dev ref) proceeds LOUDLY - a missing tool must never freeze
# the factory. FD_GH is the suite's seam: the gh binary the check reads
# through.
#
# THREE STATES, NEVER TWO. `absent` means GitHub ANSWERED - no run for that sha
# - so the guard may stand down. A read that never reached GitHub (curl exit
# nonzero, the bounded timeout firing, a body gh cannot parse) is `unknown`: it
# proceeds, but with a WARN line of its own. The two used to collapse into
# `absent`, so a TLS timeout printed `tip CI: absent` over a red tip and
# silently turned the guard off - a failed read is not a green tip.
FD_GH="${FD_GH:-gh}"
TIP_SHA=""
TIP_VERDICT="unknown"
TIP_WHY=""
TIP_HOLD=0
TIP_WARN=0

# Reads through FD_GH; bounded by coreutils timeout where the host has it, so a
# hung GitHub call can never stall the tick (the read is advisory, the hold is
# not worth a frozen factory).
gh_call() {
  if command -v timeout >/dev/null 2>&1; then
    timeout 30 "$FD_GH" "$@"
  else
    "$FD_GH" "$@"
  fi
}

tip_ci_verdict() {
  local sha runid jobs jname jstatus jconcl rc
  # Sets TIP_VERDICT / TIP_SHA / TIP_WHY directly: a command substitution would
  # run this in a subshell and throw all three away.
  sha="$(git rev-parse -q --verify dev 2>/dev/null || true)"
  [ -n "$sha" ] || sha="$(git rev-parse -q --verify origin/dev 2>/dev/null || true)"
  if [ -z "$sha" ]; then
    TIP_WHY="no dev ref in $PWD"
    TIP_VERDICT="unknown"
    return 0
  fi
  TIP_SHA="$sha"
  if ! command -v "$FD_GH" >/dev/null 2>&1; then
    TIP_WHY="no $FD_GH on PATH"
    TIP_VERDICT="unknown"
    return 0
  fi
  # rc AND output are read separately: `|| true` alone made a failed read
  # indistinguishable from an empty answer, and an empty answer is `absent` -
  # the guard switching itself off on a timeout.
  rc=0
  runid="$(gh_call run list --workflow ci.yml --commit "$sha" --event push --limit 10 \
    --json databaseId --jq '.[0].databaseId // empty' 2>/dev/null)" || rc=$?
  if [ "$rc" -ne 0 ]; then
    TIP_WHY="$FD_GH run list failed (rc=$rc): the read never reached GitHub"
    TIP_VERDICT="unknown"
    return 0
  fi
  if [ -z "$runid" ]; then
    TIP_WHY="no CI run recorded for this commit"
    TIP_VERDICT="absent"
    return 0
  fi
  rc=0
  jobs="$(gh_call run view "$runid" --json jobs \
    --jq '.jobs[] | [.name, .status, (.conclusion // "")] | @tsv' 2>/dev/null)" || rc=$?
  if [ "$rc" -ne 0 ]; then
    TIP_WHY="$FD_GH run view $runid failed (rc=$rc): the read never reached GitHub"
    TIP_VERDICT="unknown"
    return 0
  fi
  if [ -z "$jobs" ]; then
    TIP_WHY="run $runid answered no job rows - cannot tell green from red"
    TIP_VERDICT="unknown"
    return 0
  fi
  while IFS=$'\t' read -r jname jstatus jconcl; do
    case "$jname" in
      'Journeys regression'*)
        if [ "$jstatus" != "completed" ]; then
          TIP_WHY="job \"$jname\" is $jstatus (run $runid)"
          TIP_VERDICT="inflight"
          return 0
        fi
        case "$jconcl" in
          success) TIP_WHY="job \"$jname\" green (run $runid)"; TIP_VERDICT="green" ;;
          skipped) TIP_WHY="job \"$jname\" skipped (run $runid)"; TIP_VERDICT="absent" ;;
          *) TIP_WHY="job \"$jname\" $jconcl (run $runid)"; TIP_VERDICT="red" ;;
        esac
        return 0
        ;;
    esac
  done <<<"$jobs"
  TIP_WHY="run $runid carries no \"Journeys regression\" job (workflow revision older than the job)"
  TIP_VERDICT="absent"
}

tip_ci_verdict
case "$TIP_VERDICT" in
  red|inflight) TIP_HOLD=1 ;;
  unknown) TIP_WARN=1 ;;
esac
echo "tip CI: $TIP_VERDICT - dev ${TIP_SHA:0:12} ${TIP_WHY}"
if [ "$TIP_HOLD" = 1 ]; then
  echo "tip CI: HOLD - no spawn while the dev tip's regression verdict is unproven (the merge-before-dispatch law: a red tip is fix-forward + the un-land discipline, carded per the red-gate flake law)"
fi
if [ "$TIP_WARN" = 1 ]; then
  echo "tip CI: WARN - the tip's CI verdict could NOT be read: this pass proceeds WITHOUT the CI gate, which is not the same as a green tip - verify dev by hand"
fi

# ---- fix-forward door: the hold exempts recipe-only bytes --------------------
# The hold used to close on the one card class that fixes a red recipe: nothing
# could spawn while the tip was red, so a red recipe held its own fix card at
# the door and only an operator landing could clear it. The door is NARROW by
# design - a card qualifies ONLY when it declares a `FILES:` line and EVERY
# token on it sits under scripts/journeys/recipes/. Recipe bytes touch no
# runtime, which is why they can be fixed from inside the hold; a red gate
# caused by product bytes keeps the hold, and a card with no declared surface
# is unprovable, never exempt.
# The door covers the TIP hold and nothing else: a card the candidate loop has
# ALREADY held carries an independent capacity hold (same-file, go-beside-go,
# unprovable surface) that owns it for this pass. Releasing such a card here
# announced a spawn the inner could never claim - door OPEN beside
# `Spawned: 0`, byte-identical every pass: a livelock wearing the door's badge.
parked_has() {  # id -> 0 when the candidate loop already held it this pass
  local p
  for p in "${PARKED[@]:-}"; do
    [ -n "$p" ] && [ "$p" = "$1" ] && return 0
  done
  return 1
}
FIXFWD=(); FIXFWD_HOLD=0; TIP_DOOR=0; FIXN=0; FIXHELD=""; FIXLOCK=""; FIXCAP=""
if [ "$TIP_HOLD" = 1 ]; then
  for c in "${CAND[@]:-}"; do
    [ -n "$c" ] || continue
    fftok=""
    # Declared surface AND worktree evidence (surface_of_card): a resumed card
    # whose worktree already carries runtime writes is not recipe-only, whatever
    # its FILES: line promises.
    if ! files_line_absent "$c"; then fftok=$(surface_of_card "$c"); fi
    if [ -n "$fftok" ] && ! grep -qvE '^scripts/journeys/recipes/' <<<"$fftok"; then
      if parked_has "$c"; then FIXLOCK="$FIXLOCK $c"; else FIXFWD+=("$c"); fi
    else
      FIXHELD="$FIXHELD $c"
    fi
  done
  [ "${#FIXFWD[@]}" -gt 0 ] && FIXFWD_HOLD=1
  if [ "$FIXFWD_HOLD" = 1 ]; then
    # The pair verdict still bounds the door: a forbidden head pair caps a pass
    # at ONE card, and the seat math that produced VERDICT_CAP (no free
    # implementation seat => 0) bounds it too. Neither law is relaxed here.
    FIXN=${#FIXFWD[@]}
    [ "$FIXN" -gt "$VERDICT_CAP" ] && FIXN=$VERDICT_CAP
    if [ "$FIXN" -gt 0 ]; then
      # Slice to the sanctioned prefix BEFORE anything reads FIXFWD: the door
      # echo and the spawn path's unlock test must name the same set, or a card
      # the echo never named can be the one the inner claims (the cap is often
      # narrower than the recipe-only count).
      if [ "$FIXN" -lt "${#FIXFWD[@]}" ]; then
        FIXCAP=" ${FIXFWD[*]:$FIXN}"
        FIXFWD=("${FIXFWD[@]:0:$FIXN}")
      fi
      TIP_DOOR=1
      echo "tip CI: fix-forward door OPEN - spawn set under the hold: ${FIXFWD[*]}${FIXHELD:+ - held:${FIXHELD}}${FIXCAP:+ - over cap:${FIXCAP}}${FIXLOCK:+ - locked:${FIXLOCK}} (every FILES: token under scripts/journeys/recipes/; nothing else spawns)"
    else
      echo "tip CI: fix-forward door BLOCKED - ${FIXFWD[*]}${FIXLOCK:+ $FIXLOCK} declares a recipe-only FILES: surface but no slot sanctions it this pass (verdict cap $VERDICT_CAP)"
    fi
  else
    if [ -n "$FIXLOCK" ]; then
      echo "tip CI: fix-forward door CLOSED - recipe-only card(s) held by an independent lock this pass:$FIXLOCK (the door exempts the tip hold alone, and a claim-locked card cannot spawn)"
    else
      echo "tip CI: fix-forward door CLOSED - no ready card declares a recipe-only FILES: surface, so the hold stands for every candidate"
    fi
  fi
fi

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
# The review-lane hold and the verdict's declined head cards join the same
# sentinel park. Both are CAPACITY holds, enforced in the spawn path: the
# review lane's filter is `claim_lock IS NULL`, so a held review row cannot be
# claimed this pass even if the caller's --max would have room for it, and a
# declined head card cannot be taken when the inner skips an earlier one. The
# EXIT trap releases both, so nothing outlives the pass.
if [ -n "$REVHOLD" ]; then PARKED+=($REVHOLD); fi
if [ "${#HEAD[@]}" -gt "${VERDICT_CAP:-0}" ]; then
  for ((vd = VERDICT_CAP; vd < ${#HEAD[@]}; vd++)); do PARKED+=("${HEAD[vd]}"); done
fi
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
# The gate's ceiling is the FACTORY SEATS, not the implementation knob: with
# 2 implementation cards running, a third run may still be the review lane's
# one seat (2 + 1 = 3). Past TOTAL_SEATS nothing can spawn, whatever the
# claimable count says.
if { [ "$TIP_HOLD" = 0 ] || [ "$TIP_DOOR" = 1 ]; } && [ "${#RUNNING[@]}" -lt "$TOTAL_SEATS" ] && [ "$CLAIMABLE" -gt 0 ]; then
  DOUT=$(mktemp)
  # THE DOOR'S NARROWNESS, enforced in the spawn path: the hold exempts
  # recipe-only cards, nothing else. Every other candidate and every unclaimed
  # review row joins the sentinel park for this pass (the EXIT trap releases
  # them), so the inner dispatcher cannot claim work the hold still covers. The
  # review lane behaves exactly as it does on any held pass - nothing spawns
  # from it - which is why its rows are parked here too.
  if [ "$TIP_DOOR" = 1 ]; then
    for c in "${CAND[@]:-}"; do
      [ -n "$c" ] || continue
      ff_in=0
      for f in "${FIXFWD[@]}"; do [ "$f" = "$c" ] && { ff_in=1; break; }; done
      [ "$ff_in" = 1 ] || PARKED+=("$c")
    done
    for rv in "${REV[@]:-}"; do [ -n "$rv" ] && PARKED+=("$rv"); done
    park "${PARKED[@]:-}"
    VERDICT_CAP=$FIXN
    REV_SLOTS=0
    SLOTS=$FIXN
    echo "tip CI: fix-forward dispatch - $FIXN recipe-only card(s) released past the hold, every other lane held"
  fi
  # BUDGET-derived, and CLAMPED below by the head verdict: an explicit --max N
  # is a TRUE concurrency cap (running + spawns), a bare pass fills the factory
  # seats - either way the ask here is running + sanctioned spawns, and the
  # inner dispatcher counts running AFTER its reclaim phase (guard bug #8), so
  # budgeting from this pre-pass snapshot over-counts zombies: still 'running'
  # here, already claimable there. Proven 2026-10-28: --max 1 with 2
  # zombie-running made B below zero, SLOTS kept its cap-derived 2, the inner
  # got --max 4 on a reclaim-empty pool and spawned 4.
  WANTED=$(( ${#RUNNING[@]} + SLOTS + REV_SLOTS ))
  SPAWN_CAP=$VERDICT_CAP
  if [ "$TIP_DOOR" = 0 ] && [ "${#HEAD[@]}" -gt 0 ] && [ "${#HEAD[@]}" -lt "$SPAWN_CAP" ]; then
    SPAWN_CAP=${#HEAD[@]}
  fi
  # CLAMP (card t_8a1a46e1): the head verdict is computed on THIS path too - it
  # was once merely printed, and the caller was trusted to read it. On
  # 2026-11-11 a tick batched the dry-run and the real dispatch into one
  # terminal call, so `pair: forbidden` printed and this very `--max 2`
  # spawned both cards anyway (surfaces happened to be disjoint: luck, not
  # law). The verdict is a gate now: a forbidden pair caps a live pass at ONE
  # card, said out loud with the ids it declined. The dry-run output stays
  # byte-identical (the clamp sits below the dry exit, and a dry-run never
  # spawns).
  # It sits BELOW the budget computation on purpose: a BARE pass (no --max)
  # computes its own slot count and can exceed 1 - leaving it unclamped would
  # keep the hole open behind the "always pass an explicit --max" ritual this
  # card exists to delete.
  DISPATCH_MAX=$(( ${#RUNNING[@]} + SPAWN_CAP + REV_SLOTS ))
  # The line is for a VERDICT clamp only: a pass that simply has fewer legal
  # cards than slots (SPAWN_CAP trimmed to the head size) clamped nothing and
  # must stay silent - a `clamp:` with no reason reads as a defect.
  if [ "$SPAWN_CAP" -lt "$SLOTS" ] && [ -n "$CLAMP_REASON" ]; then
    if [ "$MAXARG" -gt 0 ]; then
      echo "clamp: --max $MAXARG -> $DISPATCH_MAX ($CLAMP_REASON) - $CLAMP_NOUN$CLAMP_DECLINED"
    else
      echo "clamp: wanted $WANTED -> $DISPATCH_MAX ($CLAMP_REASON) - $CLAMP_NOUN$CLAMP_DECLINED"
    fi
  fi
  # SEATS CLAMP: an explicit --max larger than the factory can ever run is
  # narrowed to the seats, loudly - the 2+1 invariant is not a caller option,
  # and this is the line that names the clamp when a caller asks for a third
  # implementation run (`--max 3` on a 2-seat factory).
  if [ "$MAXARG" -gt 0 ] && [ "$MAXARG" -gt "$DISPATCH_MAX" ] && [ "$DISPATCH_MAX" -eq "$WANTED" ] \
     && [ "$TIP_DOOR" = 0 ]; then
    echo "clamp: --max $MAXARG -> $DISPATCH_MAX (factory seats: $IMPL_SEATS implementation + $REVIEW_SEATS review = $TOTAL_SEATS max)"
  fi
  SLOTS=$(( SPAWN_CAP + REV_SLOTS ))   # the handoff echoes what this pass really asked for
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
  # ONBOARD (card t_f6d15b7f): the standing worker-onboarding law is posted BY
  # THE SPAWNER. It used to depend on two things this pass does not control:
  # the hermes spawner (never posts it - AS-IS per the 2026-11-13 ruling, and
  # NO-UPSTREAM forbids raising it) and the tick's fallback hand-post about a
  # minute later (a resumed or fast-completing worker can read its card before
  # that notice lands - missed three times, 2026-11-13 + twice on 2026-11-18).
  # The guard knows exactly which cards went running in THIS call, so it posts
  # the law itself, on the spawn verdict path: `Spawned: N` is answered by
  # `Onboarded: N` beside it, and a miss is a pass that says so out loud.
  #
  # The body is the STANDING text the tick hand-posts, byte-identical on
  # purpose: the fallback sweep's own idempotency check then reads a comment
  # this pass wrote and skips the card. One law, one body, whoever got there
  # first.
  #
  # Review-seat cards get it too: the text is the standing law for a card in
  # this factory, and the tick has never drawn a lane distinction.
  ONBOARD_TEXT='Load skill worker-onboarding at run start - you are one of up to 2 concurrent workers. When your card is done, request review with --reviewer vera (rule 7).'
  ONBOARDED=0; ONBOARD_IDS=""
  for oid in $NEWRUN; do
    [ -n "$oid" ] || continue
    # Idempotent by construction: a duplicate onboarding comment is noise, so a
    # card whose LATEST comment is already the standing text is left alone
    # (a resumed card the tick hand-posted, or a re-post after a crashed pass).
    # `id` is the tiebreak, not created_at: that column is second-granular and
    # two comments in one pass tie.
    OLAST=$(dbq "SELECT body FROM task_comments WHERE task_id='$oid' ORDER BY id DESC LIMIT 1;")
    if [ "$OLAST" = "$ONBOARD_TEXT" ]; then
      echo "onboard: $oid skipped - its latest comment is already the standing onboarding text"
      continue
    fi
    ORC=0
    OOUT=$(hermes kanban comment "$oid" "$ONBOARD_TEXT" 2>&1) || ORC=$?
    if [ "$ORC" -eq 0 ]; then
      ONBOARDED=$(( ONBOARDED + 1 ))
      ONBOARD_IDS="${ONBOARD_IDS:+$ONBOARD_IDS }$oid"
    else
      # Loud, never silent: a worker that starts without the law is exactly the
      # failure this block exists to end, and a swallowed post reads as handled.
      # The CLI's own words ride along - "rc=1" alone names no cause.
      echo "onboard: $oid comment FAILED (rc=$ORC) - that worker starts without the onboarding law: $OOUT" >&2
    fi
  done
  echo "Onboarded: $ONBOARDED${ONBOARD_IDS:+ ($ONBOARD_IDS)}"
  echo "handoff: wanted <=$WANT running (snapshot ${#RUNNING[@]} + slots $SLOTS), after: $NOWRUN (delta $(( NOWRUN - ${#RUNNING[@]} )) since snapshot)"
  [ -n "$NEWRUN" ] && echo "  newly running: $NEWRUN"
  if [ "$NOWRUN" -gt "$WANT" ]; then
    echo "over-run: +$(( NOWRUN - WANT )) past the request - inner count was taken after its reclaim phase (release_stale_claims requeues claim-expired running cards first), so its budget read sat below the guard snapshot"
  fi
  rm -f "$DOUT"
else
  # Name the reason, not just the cap: `running=0/cap=2` on a board whose only
  # work sits in the review lane with a lock, or on a board with no work at
  # all, is the same line - the tick reads claimable to tell them apart. A tip
  # hold is a third reason and must not read as a full factory.
  if [ "$TIP_HOLD" = 1 ]; then
    echo "no dispatch (tip CI $TIP_VERDICT, running=${#RUNNING[@]}/cap=$CAP, claimable=$CLAIMABLE)"
  else
    echo "no dispatch (running=${#RUNNING[@]}/cap=$CAP, claimable=$CLAIMABLE)"
  fi
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