#!/usr/bin/env bash
set -euo pipefail
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


# train-pass.sh - the merge train: ONE mechanical, no-LLM landing pass.
#
# Picks the queued landing cards (status 'done', branch still alive and
# carrying commits that are not on dev yet) - the WHOLE batch, oldest
# first - rebases each onto the accumulating dev tip, merges all of them
# with no gate between merges, runs the gate battery ONCE on the final
# tip, pushes dev, and writes one MARKER REF per card
# (refs/train/<seq>-<task_id>) at its merge commit. The marker chain is
# the bisect ladder - regressions get walked mechanically by
# scripts/train-bisect.sh, never by archaeology. The battery amortizes:
# N cards cost one gate battery, not N.
#
# Failure laws:
# - rebase conflict: abort, the tree rolls back to the batch position
#   BEFORE that card (the cards already merged stay), card untouched
#   (still done + queued), one strike comment, and the batch carries on
#   with the rest. Strike 2 = auto-demote (card back to ready with the
#   conflict note; the worker rerun rebases onto the new dev tip). A third
#   strike = reconciler agent card instead of another retry.
# - replay anomaly: the rebase zero-replayed the branch's unique commits
#   (fork-point fast-forward, dev already carrying the same patches) or the
#   merge degenerated to a two-parent-less no-op. Both are LOUD: evidence
#   comment, card demoted to ready, branch kept, batch continues. The train
#   never reports "landed" without a real merge commit carrying the branch's
#   patches in it.
# - red battery: the batch is walked by scripts/train-bisect.sh (--chain,
#   CODE legs only - a probe checkout cannot measure the served lane) to
#   name the offending card; a batch of one names its own card without a
#   walk. That card un-lands with the red-gate semantics
#   (dev reset to its pre-merge tip, evidence comment, card back to ready,
#   rebased branch KEPT); the cards after it re-merge onto the new tip and
#   the battery runs once more on THAT tree before anything is pushed. A red
#   the bisect cannot attribute (a pre-existing red on dev, a served-lane
#   red, a battery that breaks its own shape) un-lands the WHOLE batch:
#   nothing is ever pushed ungated.
# - race leg: -race needs a cgo toolchain. A host without one SKIPS the
#   leg with one loud receipt and keeps gating (Endo 2026-11-10 ruling,
#   card t_7cc44bf1: the signal stays on nightly/CI, where cgo exists) -
#   never a red gate for an environment the host can never satisfy. The
#   hard-fail stays exactly as-is where the toolchain exists.
#
# Board interaction: comments and result updates go through the hermes CLI
# (override with HERMES); when that CLI is not available in the calling
# context the train still lands - comments degrade to a direct SQLite
# append against the board DB, and demotes are always direct SQLite
# (the CLI has no done -> ready verb). A failed demote aborts the pass
# loudly; it never stays silent.
#
# Usage (from the main checkout, on dev):
#
#     scripts/train-pass.sh [--no-push]
#
#     scripts/train-pass.sh --gate-only [--no-push]
#         Run the gate battery on THIS tree and exit with its status. The
#         red-attribution seam: train-bisect.sh probes a candidate tree by
#         running exactly the battery the pass runs, so a red can never be
#         attributed by a proxy that gates something else. No board, no
#         queue, no merge, no push. The probe gate keeps the CODE legs and
#         SKIPS the legs a probe checkout cannot measure: the webui node
#         suite (node_modules lives in the main checkout), the embed lane (the
#         SPA build needs that same install) and the journeys (they drive the
#         SERVED lane, which lags the tree).
#
# Environment:
#
#   HERMES                     hermes CLI binary (default: hermes)
#   HERMES_KANBAN_BOARD        board slug (default: project-omega) - names the db
#                              the live-home probe looks for
#   KANBAN_DB                  board db path. An EXPLICIT value must exist: it
#                              never falls back to another board. Unset, the
#                              db is resolved as $HERMES_KANBAN_DB > the live
#                              home's boards/<slug>/kanban.db (windows
#                              %LOCALAPPDATA%/hermes, POSIX $HOME/.hermes) and
#                              a pass with no candidate refuses naming every
#                              probed path (card t_7acaa267: the old
#                              $HOME/.hermes default is the LEGACY home on a
#                              remapped host and red every review-seat merge
#                              rc 2 before the queue)
#   TRAIN_GATES_CMD            test override - replaces the whole gate list
#   TRAIN_CGO_PROBE_CMD        test override - its exit status IS the cgo
#                              toolchain verdict the -race leg skips on
#                              (0 = toolchain present). Unset in real passes:
#                              they probe by compiling a trivial cgo file.
#   TRAIN_JOURNEYS_CMD         the validation-journeys suite (default:
#                              scripts/journeys/run.sh --regression - the
#                              NAMED REGRESSION SET, regression.list's own
#                              gate-time minimum. The full suite belongs to
#                              the nightly, which builds the lane from the
#                              tree it tests; docs/pipeline.md carries the
#                              ruling. Missing = skipped loudly until the
#                              companion card lands). Flags allowed - the
#                              gate runs it through bash -c
#   OMEGA_HOME / OMEGA_BIN     the SERVING lane home the journey recipes seed
#                              into. Unset, the train derives <checkout>/bin
#                              (a serving home: config/config.yaml + the omega
#                              binary) and refuses BEFORE the merge when it
#                              cannot - a gate that cannot run green is a
#                              precondition, never a post-merge demote
#   JOURNEY_BASE_URL           the served lane the journeys gate drives
#                              (default: run.sh's own http://127.0.0.1:8098);
#                              checked healthy as a precondition
#   TRAIN_RECONCILER_ASSIGNEE  profile that takes third-strike cards
#                              (default: default)
#   TRAIN_NO_PUSH=1            same as --no-push
#   TRAIN_CARD                 land THIS card only, not the oldest queued
#                              one. The pin is the review seat's entry
#                              (scripts/factory-merge.sh): a pinned card
#                              that is not a queued landing refuses loudly
#                              instead of landing a neighbour. It also pins
#                              the BATCH to one card.
#   TRAIN_BISECT_CMD           the command train-bisect scores candidate
#                              trees with when a battery goes red. Unset
#                              (the real pass), it is this script's own
#                              --gate-only battery, so the walk scores the
#                              same code legs the pass just failed.
#   TRAIN_GATE_ONLY_PRE        --gate-only only: the diff base for the
#                              embed-lane and -race legs (default HEAD^1).
#   TRAIN_GATE_ONLY_TID        --gate-only only: the card id the embed-lane
#                              context line names (default gate-only).
#
# Exit codes: 0 the batch landed (or empty queue), 2 usage/precondition
# (nothing rebased, nothing merged, every card keeps its status), 3 rebase
# strike (the card stays queued), 4 demoted to ready (rebase strikes, replay
# anomalies, no-op merges, red gates), 5 third strike -> reconciler card,
# 6 red battery (a card or the whole batch un-landed), 7 the pinned card has
# nothing to land (already merged, or no branch). A BATCH pass that landed
# some cards and dropped others exits with the FIRST failure code - the
# landed cards are durable (markers + pushed), the log names what landed.

BASE="dev"
PUSH=1
[ "${TRAIN_NO_PUSH:-}" = "1" ] && PUSH=0
HERMES_BIN="${HERMES:-hermes}"
# The train-time journeys gate is the NAMED REGRESSION SET, not the full
# suite. regression.list is the gate-time minimum (every entry there burned
# a lane once and must never fail again); the full suite belongs to the
# nightly, which builds the lane from the tree it tests. The narrowing is
# not a coverage trade: the tick's fallback merge carries NO journeys gate,
# so a full-suite train gate vetoes nothing it would not already land - it
# only converts any standing product red into an un-land + demote + resweep
# cycle. Ruling 2026-11-19 on this card: the set ran 12/12 green while
# 92-registry-toggles - NOT in the set - red on the same lane, and the
# 75-divider-resize red the earlier rejection rested on was green. Widen
# the gate by adding a recipe to regression.list, never by asking the train
# for the full suite.
JOURNEYS_CMD="${TRAIN_JOURNEYS_CMD:-scripts/journeys/run.sh --regression}"
RECONCILER_ASSIGNEE="${TRAIN_RECONCILER_ASSIGNEE:-default}"
PIN="${TRAIN_CARD:-}"
GATE_ONLY=0
SELF="$_PIN_SELF"
SELF_DIR="${SELF%/*}"


die() { echo "FATAL: train-pass: $*" >&2; exit 2; }
note() { echo "==> train: $*"; }

usage() {
  cat >&2 <<'EOF'
usage: train-pass.sh [--no-push] [--gate-only]

Without --gate-only: run from the main checkout (~/dev/omega), on the dev
branch, clean tree. --gate-only: run the gate battery on the current tree
and exit with its status (the red-attribution probe seam).
EOF
  exit 2
}

while [ $# -gt 0 ]; do
  case "$1" in
    --no-push) PUSH=0 ;;
    --gate-only) GATE_ONLY=1 ;;
    -h|--help) usage ;;
    *) usage ;;
  esac
  shift
done

# ---- the gate battery ---------------------------------------------------
# The battery is ONE function, and it is the only thing the red-attribution
# seam runs (--gate-only): a candidate tree is scored by exactly the code
# legs the pass failed on, never by a proxy. `pre` is the battery's diff base
# (the batch base) and is resolved at call time, so the same function serves
# the pass and a probe.

journeys_path="$JOURNEYS_CMD"
# A command may carry flags, and a path may carry spaces: the WHOLE value is
# the path when it names a real file, else the first token is.
[ -f "$journeys_path" ] || journeys_path="${JOURNEYS_CMD%% *}"

# Does this host have a cgo toolchain, i.e. can `go test -race` run at all?
# `go env CGO_ENABLED` alone is not the answer (0 is also what a cgo file
# reports as "no Go source files", and a CGO_ENABLED=1 host can still have an
# unresolved CC), so the probe COMPILES a trivial cgo file - the same thing
# -race needs. Verified both ways 2026-11-19: this host answers 1 ("no Go
# source files" / "cgo: C compiler gcc not found"), a gcc host answers 0.
# TRAIN_CGO_PROBE_CMD is the fixture seam: its exit status IS the verdict.
cgo_toolchain_ok() {
  if [ -n "${TRAIN_CGO_PROBE_CMD:-}" ]; then
    bash -c "$TRAIN_CGO_PROBE_CMD" >/dev/null 2>&1
    return $?
  fi
  local dir rc
  # GOCACHE is a NATIVE absolute path (train-pass also runs from an MSYS
  # shell on winbtw, where the native go exe cannot open a /tmp dir) and go
  # guarantees it exists.
  dir="$(go env GOCACHE 2>/dev/null || true)"
  [ -n "$dir" ] || return 1
  dir="$dir/omega-cgo-probe.$$"
  mkdir -p "$dir" 2>/dev/null || return 1
  printf 'package main\n\nimport "C"\n\nfunc main() {}\n' >"$dir/probe.go" 2>/dev/null || {
    rm -rf "$dir"
    return 1
  }
  rc=0
  ( cd "$dir" && go build -o "$dir/probe.bin" probe.go ) >/dev/null 2>&1 || rc=$?
  rm -rf "$dir"
  return "$rc"
}

gates_pass() {
  set -e
  if [ -n "${TRAIN_GATES_CMD:-}" ]; then
    echo "TEST GATE OVERRIDE ACTIVE (TRAIN_GATES_CMD): it is the ONLY gate"
    bash -c "$TRAIN_GATES_CMD"
    echo "FIN"
    return 0
  fi
  if [ -f go.mod ] && command -v go >/dev/null 2>&1; then
    pkglist="$(go list ./... | grep -v '/bin/' || true)"
    if [ -n "$pkglist" ]; then
      echo "==> go vet"
      go vet $pkglist
      echo "==> dep law"
      ./scripts/gate-deps.sh
      # worker fence eval (card t_8abdbb15): the regression set's
      # landing eval. A worker session's push/landing must refuse or be
      # mechanically blocked - the eval FAILS if the operation
      # succeeds. Cheap (throwaway fixture repos, no network): it rides
      # every train pass, like the journeys do.
      if [ -f scripts/fence-test.sh ] && [ -f bin/shims/git ]; then
        echo "==> worker fence eval"
        bash scripts/fence-test.sh || exit 1
      fi
      echo "==> go test"
      go test $pkglist
      # -race on the package(s) the landing actually touches.
      if grep -q '\.go$' <<<"$(git diff --name-only "$pre" HEAD)"; then
        pkgmod="$(go list -m -f '{{.Path}}')"
        raceargs=()
        for d in $(git diff --name-only "$pre" HEAD | grep '\.go$' | xargs -n1 dirname | sort -u); do
          want="$pkgmod/$d"
          if grep -qFe "$want" <<<"$pkglist"; then
            raceargs+=("./$d")
          fi
        done
        if [ "${#raceargs[@]}" -gt 0 ]; then
          if cgo_toolchain_ok; then
            echo "==> go test -race on touched packages: ${raceargs[*]}"
            go test -race "${raceargs[@]}"
          else
            echo "!!! race leg skipped: no cgo toolchain on this host (t_7cc44bf1, Endo 2026-11-10) - the -race signal stays on nightly/CI"
          fi
        fi
      fi
    fi
  fi
  # A probe checkout carries no node_modules (the main checkout does), so the
  # node suite there measures a missing install, not the tree.
  if [ "${GATE_PROBE:-0}" = "1" ]; then
    echo "==> webui node suite: SKIPPED (probe gate: node_modules lives in the main checkout, not a probe tree)"
  elif [ -d webui/test ] && command -v node >/dev/null 2>&1; then
    echo "==> webui node suite"
    (cd webui && node --test 'test/*.test.ts' >/dev/null 2>&1) || {
      echo "webui suite failed - rerun (cd webui && node --test 'test/*.test.ts') for detail" >&2
      exit 1
    }
  fi
  echo "==> embed lane"
  if [ "${GATE_PROBE:-0}" = "1" ]; then
    echo "==> embed lane: SKIPPED (probe gate: a probe checkout has no node_modules for the SPA build)"
  elif [ -f scripts/embed-lane.sh ]; then
    scripts/embed-lane.sh --base "$pre" "card $tid"
  else
    echo "embed lane absent in this checkout - skipping (static/ change gate lives in the lane)"
  fi
  # The journeys measure the SERVED lane, which lags the tree it is gated
  # against: a probe checkout cannot move that lane, so scoring a probe on it
  # would read the same answer for every candidate.
  if [ "${GATE_PROBE:-0}" = "1" ]; then
    echo "==> validation journeys: SKIPPED (probe gate: the served lane measures the LANE, not this tree)"
  elif [ -f "$journeys_path" ]; then
    echo "==> validation journeys"
    echo "    lane env: OMEGA_HOME=${OMEGA_HOME:-<unset>} OMEGA_BIN=${OMEGA_BIN:-<unset>} base ${JOURNEY_BASE_URL:-<run.sh default>}"
    # An override whose WHOLE value is a path (spaces allowed) runs as one
    # argument; anything else is a command line, flags included.
    if [ "$JOURNEYS_CMD" = "$journeys_path" ]; then
      bash "$journeys_path" || exit 1
    else
      bash -c "$JOURNEYS_CMD" || exit 1
    fi
  elif [ -n "${TRAIN_JOURNEYS_CMD:-}" ]; then
    echo "journeys command is set but missing: $journeys_path (from '$JOURNEYS_CMD')" >&2
    exit 1
  else
    echo "==> validation journeys: SKIPPED - $journeys_path not present yet (the companion card wires it in)"
  fi
  echo "FIN"
}

# run_battery <base>: the battery over the CURRENT tree. Sets gates_log,
# gates_rc and battery_secs. The -e must be OFF at the call: ON it kills the
# whole pass on gate failure (no handler), and a `subshell ||` context
# suspends -e inside the subshell (every red sailed through green).
run_battery() {
  pre="$1"
  gates_log="$(mktemp "${TMPDIR:-/tmp}"/train-gates.XXXXXX.log)"
  battery_start="$(date +%s)"
  set +e
  ( gates_pass ) >"$gates_log" 2>&1
  gates_rc=$?
  set -e
  battery_secs=$(( $(date +%s) - battery_start ))
  battery_total=$((battery_total + battery_secs))
  battery_runs=$((battery_runs + 1))
  return 0
}
battery_total=0
battery_runs=0

# ---- --gate-only: the probe seam (no board, no queue, no merge) --------
# train-bisect.sh hands a candidate tree to this mode: the scored legs are
# the pass's own, so a walk can only ever name a tree the pass would fail.
if [ "$GATE_ONLY" = "1" ]; then
  REPO="$(git rev-parse --show-toplevel 2>/dev/null)" || die "--gate-only needs a checkout"
  cd "$REPO"
  pre="${TRAIN_GATE_ONLY_PRE:-$(git rev-parse -q --verify HEAD^1 2>/dev/null || git rev-parse HEAD)}"
  tid="${TRAIN_GATE_ONLY_TID:-gate-only}"
  GATE_PROBE=1
  export GATE_PROBE
  run_battery "$pre"
  if [ "$gates_rc" != 0 ]; then
    tail -25 "$gates_log" >&2 || true
    exit "$gates_rc"
  fi
  grep -q '^FIN$' "$gates_log" \
    || { echo "FATAL: train-pass: --gate-only broke the battery's own structure (no FIN marker)" >&2; exit 1; }
  exit 0
fi
# A pass never runs probe-shaped: the skip flag is the probe's alone.
unset GATE_PROBE

REPO="$(git rev-parse --show-toplevel 2>/dev/null)" || die "run this inside the omega checkout"
cd "$REPO"

# The worktree path fold is one file: factory-prune.sh reads the same
# registrations to tell a husk from a live worktree, and two copies of the
# fold drift (scripts/lib-wt.sh).
WT_LIB="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib-wt.sh"
[ -f "$WT_LIB" ] || die "missing scripts/lib-wt.sh next to train-pass.sh ($WT_LIB)"
. "$WT_LIB"

cur="$(git symbolic-ref --short HEAD 2>/dev/null || true)"
[ "$cur" = "$BASE" ] || die "HEAD is on '$cur', not $BASE - the train lands from the main checkout on dev"
[ -z "$(git status --porcelain)" ] || die "main checkout is dirty - commit or clean before a train pass"

# The landing must sit on top of what everyone else landed.
if grep -q '^origin$' <<<"$(git remote 2>/dev/null)"; then
  git fetch -q origin "$BASE"
  if ! git merge-base --is-ancestor "origin/$BASE" "$BASE"; then
    die "origin/$BASE diverged from local $BASE - resolve by hand, the train never rewrites"
  fi
fi

# ---- board access ------------------------------------------------------

now() { date +%s; }

# Board db resolution (host-remap law, card t_7acaa267). The old default was
# $HOME/.hermes/boards/... - the LEGACY home on a host whose live home was
# remapped (winbtw: %LOCALAPPDATA%/hermes), so a pass with no caller env died
# `board db not found` exit 2 BEFORE the queue and before the merge: every
# review-seat landing fell back to the tick sweep, forever. Order:
#
#   $KANBAN_DB > $HERMES_KANBAN_DB > the live home's board db
#
# An explicit override that names no file REFUSES without falling back -
# silently landing somebody else's queue is worse than a dead pass. The live
# home follows the hermes CLI's own chain (mirrors the resolve_hermes idiom in
# scripts/factory-dispatch.sh): $HERMES_KANBAN_HOME, else $HERMES_HOME (a
# profiles/<name> home names its root), else windows %LOCALAPPDATA%/hermes,
# else POSIX $HOME/.hermes - plus the same windows tree reached as
# $HOME/AppData/Local for a stripped-env caller. Never silent: a refusal names
# every probed path.
board_slug="${HERMES_KANBAN_BOARD:-project-omega}"
hermes_root_of() {  # a trailing profiles/<name> segment names its ROOT home
  local h="${1//\\//}"
  if [ "$h" != "$(dirname "$h")" ] && [ "$(basename "$(dirname "$h")")" = profiles ]; then
    h="$(dirname "$(dirname "$h")")"
  fi
  printf '%s' "$h"
}

kanban_db_candidates=()
if [ -n "${KANBAN_DB:-}" ]; then
  [ -f "$KANBAN_DB" ] || die "KANBAN_DB is set but there is no board db at $KANBAN_DB (an explicit override never falls back to another board)"
  kanban_db_candidates=("$KANBAN_DB")
elif [ -n "${HERMES_KANBAN_DB:-}" ]; then
  [ -f "$HERMES_KANBAN_DB" ] || die "HERMES_KANBAN_DB is set but there is no board db at $HERMES_KANBAN_DB (a pinned db is the caller's file, never guessed around)"
  kanban_db_candidates=("$HERMES_KANBAN_DB")
else
  hermes_roots=()
  if [ -n "${HERMES_KANBAN_HOME:-}" ]; then hermes_roots+=("${HERMES_KANBAN_HOME//\\//}"); fi
  if [ -n "${HERMES_HOME:-}" ]; then hermes_roots+=("$(hermes_root_of "$HERMES_HOME")"); fi
  if [ -n "${LOCALAPPDATA:-}" ]; then hermes_roots+=("${LOCALAPPDATA//\\//}/hermes"); fi
  hermes_roots+=("$HOME/AppData/Local/hermes" "$HOME/.hermes")
  for root in "${hermes_roots[@]}"; do
    cand="$root/kanban/boards/$board_slug/kanban.db"
    case " ${kanban_db_candidates[*]-} " in
      *" $cand "*) continue ;;   # same tree reached twice - probe it once
    esac
    kanban_db_candidates+=("$cand")
  done
fi

KANBAN_DB=""
for cand in "${kanban_db_candidates[@]}"; do
  if [ -f "$cand" ]; then
    KANBAN_DB="$cand"
    break
  fi
done
if [ -z "$KANBAN_DB" ]; then
  echo "FATAL: train-pass: no board db for board '$board_slug'. Probed, in order:" >&2
  for cand in "${kanban_db_candidates[@]}"; do echo "         $cand" >&2; done
  echo "       set KANBAN_DB (or HERMES_KANBAN_DB to pin one), or HERMES_KANBAN_BOARD" >&2
  echo "       to name the board. Nothing was queued, rebased or merged." >&2
  exit 2
fi
note "board db: $KANBAN_DB"
# The resolved path rides the environment: the hermes CLI fallback stub and
# every board_comment/demote below read it from here.
export KANBAN_DB

# The sqlite3 CLI is absent on fleet Windows hosts (the same gap
# scripts/tick-pending.sh documents: boards are read through the python
# sqlite3 module there). Every call site in this script passes its SQL as
# ONE argument in the CLI's own shapes - [-readonly] [-separator S] <db>
# <sql>, or a heredoc as stdin - so a shell function stands in for the CLI
# without touching a call. Without either reader the pass dies loudly: an
# unreadable board must never look like "queue empty, nothing to land".
if ! command -v sqlite3 >/dev/null 2>&1; then
  TRAIN_PY="$(command -v python || command -v python3 || true)"
  [ -n "$TRAIN_PY" ] || die "no board reader: the sqlite3 CLI and python are both missing"
  sqlite3() { "$TRAIN_PY" -c '
import sqlite3, sys
args, sep = sys.argv[1:], "|"
while args and args[0].startswith("-"):
    a = args.pop(0)
    if a == "-separator":
        sep = args.pop(0)
db = args[0]
sql = " ".join(args[1:]) if len(args) > 1 else sys.stdin.read()
stmts, buf = [], ""
for line in sql.splitlines(True):
    buf += line
    if sqlite3.complete_statement(buf):
        stmts.append(buf)
        buf = ""
if buf.strip():
    stmts.append(buf)
try:
    con = sqlite3.connect(db, timeout=30)
except sqlite3.OperationalError:
    # A native python cannot open an MSYS path (/tmp/...): the caller may
    # have handed us the shell spelling. Convert once and retry; when
    # cygpath is absent or refuses, the original error stands.
    import subprocess
    fixed = ""
    try:
        conv = subprocess.run(["cygpath", "-m", db], capture_output=True, text=True)
        if conv.returncode == 0:
            fixed = conv.stdout.strip()
    except OSError:
        fixed = ""
    if not fixed:
        raise
    con = sqlite3.connect(fixed, timeout=30)
# Windows text streams translate \n to \r\n; the callers parse LF records.
sys.stdout.reconfigure(newline="\n")
try:
    for stmt in stmts:
        cur = con.execute(stmt)
        if cur.description:
            for row in cur.fetchall():
                print(sep.join("" if v is None else str(v) for v in row))
    con.commit()
finally:
    con.close()
' "$@"; }
fi


# sqlq <text>: escape single quotes for a SQL literal.
sqlq() { sed "s/'/''/g" <<<"$1"; }

# board_comment <task_id> <body>: hermes CLI first; direct SQLite append
# as the fallback so a train run never loses its evidence trail.
board_comment() {
  local tid="$1" body="$2" cliargs=()
  cliargs=("$tid" "$body")
  if [ -n "${HERMES_KANBAN_BOARD:-}" ]; then
    cliargs=("--board" "$HERMES_KANBAN_BOARD" "${cliargs[@]}")
  fi
  if "$HERMES_BIN" kanban comment "${cliargs[@]}" >/dev/null 2>&1; then
    return 0
  fi
  sqlite3 "$KANBAN_DB" "INSERT INTO task_comments(task_id,author,body,created_at)
    VALUES('$(sqlq "$tid")','train','$(sqlq "$body")',$(now));"
}

# task_field <task_id> <column> on stdout.
task_field() {
  sqlite3 -readonly "$KANBAN_DB" "SELECT $2 FROM tasks WHERE id='$(sqlq "$1")';"
}

# strike_count <task_id>: how many rebase-conflict strikes this card has
# already eaten.
strike_count() {
  sqlite3 -readonly "$KANBAN_DB" \
    "SELECT count(*) FROM task_comments
      WHERE task_id='$(sqlq "$1")'
        AND body LIKE 'train: rebase conflict (strike%';"
}

# demote_card <task_id> <reason>: done -> ready (or todo when an
# incomplete parent gate applies), completed_at cleared, promoted_manual
# event appended - the same flip the dashboard drag performs.
demote_card() {
  local tid="$1" reason="$2"
  local upd
  upd="$(sqlite3 "$KANBAN_DB" "
  UPDATE tasks
     SET status = (CASE WHEN (
             SELECT count(*) FROM task_links l JOIN tasks p ON p.id = l.parent_id
              WHERE l.child_id = '$(sqlq "$tid")'
                AND p.status NOT IN ('done','archived')
         ) > 0 THEN 'todo' ELSE 'ready' END),
         completed_at = NULL,
         current_run_id = NULL,
         claim_lock = NULL,
         claim_expires = NULL,
         worker_pid = NULL
   WHERE id = '$(sqlq "$tid")' AND status = 'done';
  SELECT changes();")"
  if [ "$upd" != "1" ]; then
    die "demote of $tid did not move exactly one row (got $upd) - board state not what train-pass expects"
  fi
  sqlite3 "$KANBAN_DB" "INSERT INTO task_events(task_id, kind, payload, created_at)
       VALUES('$(sqlq "$tid")','promoted_manual',
              '{\"actor\": \"train-pass\", \"reason\": \"$(sqlq "$reason")\"}',
              $(now));"
}

# reconcile_card <task_id> <strikes> <branch>: third strike - a reconciler
# agent card takes the rebase, the original card is excluded from the
# queue (branch_name cleared) instead of being retried forever.
reconcile_card() {
  local tid="$1" strikes="$2" branch="$3"
  local title="reconciler: rebase landing for $tid ($strikes rebase strikes)"
  local body
  body=$(cat <<EOF
The merge train hit a third rebase strike on $tid (branch $branch): the
card's rebases keep conflicting against a moving dev. This card takes the
rebase reconciliation - resolve the branch onto the dev tip by hand or
rework the landing, then land through the train. Original card: $tid
(excluded from the train queue by clearing its branch_name; its branch
stays for archaeology).
EOF
)
  printf '%s\n' "$body" | "$HERMES_BIN" kanban create --json \
    --assignee "$RECONCILER_ASSIGNEE" --body-file - "$title" >/dev/null 2>&1 || {
    echo "FATAL: train-pass: third strike on $tid but the reconciler card could not" >&2
    echo "       be created (hermes CLI refused). Create it by hand:" >&2
    echo "         hermes kanban create --assignee $RECONCILER_ASSIGNEE '$title'" >&2
    die "reconciler card creation failed"
  }
  sqlite3 "$KANBAN_DB" "
  UPDATE tasks SET branch_name = NULL WHERE id = '$(sqlq "$tid")' AND status = 'done';
  SELECT changes();"
  sqlite3 "$KANBAN_DB" "INSERT INTO task_events(task_id, kind, payload, created_at)
       VALUES('$(sqlq "$tid")','commented',
              '{\"author\": \"train\", \"note\": \"third strike ($strikes) - handed to the reconciler, queue-excluded\"}',
              $(now));
"
}

# ---- queue -------------------------------------------------------------

# Oldest first: done cards whose wt branch still exists and still carries
# commits that are not on dev (a landed branch is an ancestor and gets
# swept at the end of its own landing). TRAIN_CARD pins the queue to one
# card - the review seat's single-card landing, where "the oldest queued
# card" may well be somebody else's.
#
# The branch is branch_name, or wt/<task_id> when the dispatcher recorded
# none: a scratch-spawned card lands with branch_name NULL while its worker
# still commits to wt/<task_id>, and refusing those rows cost a hand repair
# on the board. NULL is also how reconcile_card takes a third-strike card out
# of the queue, so the fallback stays off a row carrying the reconciler's
# exclusion event - it only resolves a branch that was never recorded at all.
QUEUE_BRANCH="COALESCE(NULLIF(branch_name, ''), 'wt/' || id)"
QUEUE_WHERE="status = 'done'
       AND ( (branch_name IS NOT NULL AND branch_name != '')
             OR NOT EXISTS (SELECT 1 FROM task_events e
                             WHERE e.task_id = tasks.id
                               AND e.payload LIKE '%queue-excluded%') )"
if [ -n "$PIN" ]; then
  mapfile -t CANDIDATES < <(sqlite3 -readonly -separator ' ' "$KANBAN_DB" "
    SELECT id, $QUEUE_BRANCH FROM tasks
     WHERE $QUEUE_WHERE
       AND id = '$(sqlq "$PIN")'
     ORDER BY completed_at ASC, id ASC;")
  [ "${#CANDIDATES[@]}" -gt 0 ] \
    || die "pinned card $PIN is not a queued landing (needs status 'done' and a branch - branch_name, or wt/<card-id> when the row carries none - and no reconciler queue-exclusion)"
else
  mapfile -t CANDIDATES < <(sqlite3 -readonly -separator ' ' "$KANBAN_DB" "
    SELECT id, $QUEUE_BRANCH FROM tasks
     WHERE $QUEUE_WHERE
     ORDER BY completed_at ASC, id ASC;")
fi


# The batch is every queued landing (oldest first) whose branch still exists
# and still carries commits not on dev. TRAIN_CARD pins it to one card - the
# review seat's landing, where "the oldest queued card" may well be somebody
# else's.
BATCH=()
for cand in "${CANDIDATES[@]}"; do
  c="${cand%% *}"; b="${cand#* }"
  if git show-ref --verify --quiet "refs/heads/$b" \
     && ! git merge-base --is-ancestor "$b" "$BASE" 2>/dev/null; then
    BATCH+=("$c|$b")
  fi
done

if [ "${#BATCH[@]}" -eq 0 ]; then
  if [ -n "$PIN" ]; then
    # The pin's card is done but its branch is already in dev (or gone).
    # "queue empty" would read as a landing to the caller - refuse instead.
    echo "train: pinned card $PIN has nothing to land (branch already merged into $BASE, or no live branch)." >&2
    exit 7
  fi
  echo "train: queue empty."
  exit 0
fi

batch_ids=""
for entry in "${BATCH[@]}"; do
  batch_ids="${batch_ids:+$batch_ids,}${entry%%|*}"
done
if [ -n "$PIN" ]; then
  note "pinned card: ${BATCH[0]%%|*} ($(task_field "${BATCH[0]%%|*}" title || true)), branch ${BATCH[0]#*|}"
else
  note "batch of ${#BATCH[@]} queued card(s): $batch_ids"
fi

# ---- gate preconditions (refuse BEFORE the merge, never mid-cycle) ------
# The journeys gate measures the SERVED lane, and six recipes seed agent
# homes into the lane home ($OMEGA_HOME), dying loudly when the env does not
# name it (110/114/60/92/94/96: "set OMEGA_HOME or OMEGA_BIN ..."). A pass
# with no caller env therefore burned a full merge cycle and paid a red gate
# for a harness gap: on winbtw 2026-11-18, 5 of 9 journey reds were that
# precondition die with zero product assertions reached, and an approved
# card was demoted on it. A gate that cannot run green is a PRECONDITION:
# it refuses HERE, before anything is rebased or merged, and the card keeps
# its status, its branch and its verdict.
journeys_will_run() { [ -z "${TRAIN_GATES_CMD:-}" ] && [ -f "$journeys_path" ]; }

lane_home_usable() {  # <dir>: a serving lane home - its config + serve binary
  local d="$1" b
  [ -f "$d/config/config.yaml" ] || return 1
  for b in "$d/omega" "$d/omega.exe"; do
    if [ -x "$b" ]; then return 0; fi
  done
  return 1
}

resolve_lane_env() {
  local b
  if [ -z "${OMEGA_HOME:-}" ] && [ -n "${OMEGA_BIN:-}" ]; then
    OMEGA_HOME="$(dirname "$OMEGA_BIN")"
  fi
  if [ -n "${OMEGA_HOME:-}" ]; then
    [ -d "$OMEGA_HOME" ] || die "OMEGA_HOME is set but is not a directory: $OMEGA_HOME (the journey recipes seed agent homes into it)"
    if [ -z "${OMEGA_BIN:-}" ]; then
      for b in "$OMEGA_HOME/omega" "$OMEGA_HOME/omega.exe"; do
        if [ -x "$b" ]; then OMEGA_BIN="$b"; break; fi
      done
    fi
    export OMEGA_HOME OMEGA_BIN
    note "journeys lane home (caller): $OMEGA_HOME"
    return 0
  fi
  # Derivation: the checkout's own bin/ IS the dev lane home (config/config.yaml
  # plus the omega binary the lane's serve was launched from). Nothing is
  # guessed past that - an unproven home would seed into the wrong directory.
  if lane_home_usable "$REPO/bin"; then
    OMEGA_HOME="$REPO/bin"
    for b in "$REPO/bin/omega" "$REPO/bin/omega.exe"; do
      if [ -x "$b" ]; then OMEGA_BIN="$b"; break; fi
    done
    export OMEGA_HOME OMEGA_BIN
    note "journeys lane home (derived): $OMEGA_HOME"
    return 0
  fi
  die "the validation journeys gate cannot run green without a lane home: OMEGA_HOME and OMEGA_BIN are unset and $REPO/bin is not a serving lane home (needs config/config.yaml plus omega or omega.exe). Probed: OMEGA_HOME (unset), OMEGA_BIN (unset), $REPO/bin. Refusing BEFORE the merge - nothing rebased, nothing merged, the card keeps its status."
}

lane_healthy() {  # <url>: the served lane the journeys drive must answer ok
  local url="$1" answer=""
  if command -v curl >/dev/null 2>&1; then
    answer="$(curl -s -m 10 "$url/health" 2>/dev/null || true)"
  fi
  case "$answer" in
    *'"ok"'*) note "journeys lane healthy: $url"; return 0 ;;
  esac
  die "the validation journeys gate drives the SERVED lane, and $url/health is not healthy (answer: ${answer:-<none>}). A lane red of that shape says nothing about this landing. Refusing BEFORE the merge - nothing rebased, nothing merged, the card keeps its status. Fix the lane (or set JOURNEY_BASE_URL to the lane that serves this checkout) and rerun."
}

if journeys_will_run; then
  resolve_lane_env
  lane_healthy "${JOURNEY_BASE_URL:-http://127.0.0.1:8098}"
fi

# ---- free the branch ---------------------------------------------------
#
# A card's workspace_path is a WORKTREE only while this checkout registers it.
# On winbtw `git worktree remove -f` against a tree some live session holds as
# its cwd deletes every file but cannot unlink the top directory, and what is
# left is a HUSK: an empty, UNREGISTERED dir (no .git file, no
# .git/worktrees/<id> entry). Every command that then assumed "a dir at
# workspace_path is a worktree" lies - `git -C <husk> status` falls through to
# THIS checkout (the husk's parent), so the dirty guard read the main repo and
# the free step died `fatal: not a working tree` on every pass, forever
# (proven live on t_7acaa267, 2026-11-19, including the tick's fallback sweep).
# The free step therefore asks the repo's own worktree list first
# (wt_registered, scripts/lib-wt.sh), and treats a husk as the plain
# directory it is. The fold it compares with lives in the shared helper, so
# factory-prune.sh's sweep answers the same question the same way.

# The free step's precondition is the DIRTY guard above, not the removal: the
# work is committed on the branch, so freeing the tree is cleanup. A live
# process holding it as its cwd makes the final rmdir impossible on winbtw -
# git has already unlinked every file and dropped the registration, then fails
# on the top directory (rc 255, "failed to delete ... Permission denied"). That
# is the review seat's own shape (the reviewer session is spawned with cwd =
# the card worktree), so vetoing it defers every review-seat landing to the
# tick's sweep, and factory-prune.sh's sweep_husks already reaps a
# registration-less empty dir. Teeth: a registration still standing, or a
# leftover with entries in it, is state this pass cannot vouch for.
dir_is_empty() {  # <dir>: empty AND listable (a failed listing proves nothing)
  local out
  out="$(find "$1" -mindepth 1 -maxdepth 1 -print -quit 2>/dev/null)" || return 1
  [ -z "$out" ]
}

free_card_tree() {  # <task_id> <branch>
  local tid="$1" bra="$2" ws wskind
  ws="$(task_field "$tid" "workspace_path" || true)"
  wskind="$(task_field "$tid" "workspace_kind" || true)"
  if [ -n "$ws" ] && [ -d "$ws" ]; then
    if wt_registered "$ws"; then
      if [ -n "$(git -C "$ws" status --porcelain 2>/dev/null)" ]; then
        echo "FATAL: train-pass: done card $tid has $ws dirty - uncommitted work in a closed card;" >&2
        echo "       the train never destroys unclaimed state. A human looks at it." >&2
        exit 2
      fi
      if ! git worktree remove -f "$ws" >/dev/null 2>&1; then
        if ! wt_registered "$ws" && dir_is_empty "$ws"; then
          note "card worktree $ws is held by a live session: git dropped the registration and every file, the empty husk stays for the pruner sweep - landing"
        else
          die "could not free the card worktree $ws (rm the worktree by hand)"
        fi
      else
        note "freed card worktree $ws"
      fi
    elif [ "$wskind" = "worktree" ]; then
      # Not a worktree of this checkout: the husk a held-cwd removal leaves
      # behind, or an unregistered leftover. Nothing here is work this checkout
      # claims - the card's commits are on branch $bra - so remove the plain
      # directory and land. Only a HOLDER can refuse it (winbtw: "Device or
      # resource busy" while a session's cwd is inside), and that die names it
      # instead of sending a human to a worktree removal git cannot perform.
      if ! rm -rf "$ws" 2>/dev/null; then
        die "could not remove the leftover directory $ws - it is no worktree of this checkout (the card's commits are on $bra, nothing was rebased or merged), but a live process still holds it: a session spawned with its cwd at the workspace path is the usual one. End that session, then rerun the pass."
      fi
      note "removed unregistered leftover dir $ws (not a worktree of this checkout)"
    else
      note "card workspace $ws is not a worktree of this checkout and the card does not declare one (workspace_kind=${wskind:-unset}) - left untouched"
    fi
  fi
}

# The whole batch's trees are freed BEFORE the first merge: a refusal here is
# then a clean precondition (dev untouched, nothing half-merged).
for entry in "${BATCH[@]}"; do
  free_card_tree "${entry%%|*}" "${entry#*|}"
done

# ---- rebase + merge ----------------------------------------------------

# branch_patchids <start> <exclude> -> sorted patch-id list of the commits
# unique to <start> vs <exclude>. The replay-parity guard compares this
# before/after the rebase: a fork-point fast-forward or an already-in-dev
# patch that zero-replays MUST show up as a parity break, never as a
# silent empty landing.
branch_patchids() {
  local sha
  for sha in $(git rev-list --no-merges --reverse "$1" "^$2"); do
    git show "$sha" | git patch-id | awk '{print $1}'
  done | sort
}

# branch_carried <tip> <branch> -> patch ids of the branch's commits whose
# patch the tip ALREADY carries. A batch leaves each card rebased onto its
# predecessor, so a retry's tip carries those copies under other shas; the
# rebase drops them as empty and they are duplicates, never lost work.
branch_carried() {
  local mark sha
  while read -r mark sha; do
    [ "$mark" = "-" ] || continue
    git show "$sha" | git patch-id | awk '{print $1}'
  done < <(git cherry "$1" "$2")
}

# branch_commit_count <branch> <tip>: commits unique to the branch. Counted,
# not patch-id'd: an initially-empty commit has no patch at all and lands.
branch_commit_count() { git rev-list --count --no-merges "$1" "^$2"; }

conflict_step() {  # <task_id> <branch> <pre-tip>: strike bookkeeping, never exits
  local tid="$1" bra="$2" pre="$3" files n body rc
  # Conflicted file list comes FIRST - the abort wipes the index state.
  files="$(git diff --name-only --diff-filter=U 2>/dev/null | head -8 || true)"
  git rebase --abort 2>/dev/null || true
  git merge --abort 2>/dev/null || true
  git checkout -q -f "$BASE"
  git reset -q --hard "$pre" 2>/dev/null || true
  n=$(($(strike_count "$tid") + 1))
  body="train: rebase conflict (strike $n) on $bra vs $BASE - conflict in: ${files:-<n/a>}. Strike 2 demotes; strike 3 goes to the reconciler."
  board_comment "$tid" "$body"
  note "rebase conflict on $tid, strike $n"
  if [ "$n" -ge 3 ]; then
    note "third strike - reconciler card instead of another retry"
    reconcile_card "$tid" "$n" "$bra"
    CARD_RC=5
  elif [ "$n" -eq 2 ]; then
    note "strike 2 - auto-demote: card back to ready with the conflict note"
    demote_card "$tid" \
      "merge train: rebase conflict vs $BASE ($n strikes). On $bra - rebase onto the new dev tip in the rerun."
    CARD_RC=4
  else
    note "strike 1 - card stays queued, a later pass retries"
    CARD_RC=3
  fi
  return 0
}

two_parent() { git rev-parse -q --verify "$1^2" >/dev/null 2>&1; }

# drop_card <rc>: a batch pass exits with its FIRST dropped card's code. The
# cards that landed are durable (markers + push) and the log names them.
drop_card() { if [ "$batch_rc" = "0" ]; then batch_rc="$1"; fi; return 0; }

# ---- merge the batch (no gate between merges) --------------------------
# Each branch is rebased onto the tip the previous merges left, then merged.
# A card that cannot rebase or merge is dropped from the batch and the loop
# carries on: one conflicted card never holds the rest hostage.

batch_base="$(git rev-parse HEAD)"
B_LAND=()
batch_rc=0
CARD_RC=0

for entry in "${BATCH[@]}"; do
  tid="${entry%%|*}"; bra="${entry#*|}"
  title="$(task_field "$tid" "title" || true)"
  who="$(task_field "$tid" "assignee" || true)"
  [ -n "$who" ] || who="?"
  # Heal the row, not only this pass: a landing that leaves branch_name NULL is
  # the same stale row a human repaired by hand before it could queue.
  if [ -z "$(task_field "$tid" branch_name || true)" ]; then
    sqlite3 "$KANBAN_DB" "UPDATE tasks SET branch_name = '$(sqlq "$bra")'
       WHERE id = '$(sqlq "$tid")' AND (branch_name IS NULL OR branch_name = '');" >/dev/null
    note "card $tid carries no branch_name - resolved its wt branch $bra (row repaired)"
  fi
  note "merging $tid ($title), branch $bra"

  pre="$(git rev-parse HEAD)"
  pre_unique="$(branch_patchids "$bra" "$pre")"
  pre_carried="$(branch_carried "$pre" "$bra")"
  pre_n="$(branch_commit_count "$bra" "$pre")"
  CARD_RC=0
  git checkout -q "$bra" 2>/dev/null || die "cannot check out $bra locally"
  # --no-fork-point: the replay set is merge-base-defined, no reflog guessing.
  # --reapply-cherry-picks: dev may already carry a SAME-PATCH-IDENTICAL commit
  # EARLIER in its history (applied, reverted, re-added by a later commit) -
  # cherry-pick suppression would silently skip the branch's real work and
  # fast-forward onto dev, landing nothing (found live on the 2nd landing).
  if ! git rebase --no-fork-point --reapply-cherry-picks "$BASE" >/dev/null 2>&1; then
    git checkout -q "$BASE" 2>/dev/null || true
    conflict_step "$tid" "$bra" "$pre"
    drop_card "$CARD_RC"
    continue
  fi
  git checkout -q "$BASE"

  # Replay-parity: the rebase must replay the branch's work. A commit it
  # DROPS is only allowed to be a predecessor's copy the tip already carries
  # (the batch rebased this branch onto its predecessor in an earlier pass);
  # anything else dropped is lost work, and a branch left with nothing to
  # replay is the silent empty landing the guard exists to stop.
  post_unique="$(branch_patchids "$bra" "$pre")"
  post_n="$(branch_commit_count "$bra" "$pre")"
  lost_bad=0
  if [ "$post_n" -eq 0 ]; then
    lost_bad=1
  else
    while read -r pid; do
      [ -n "$pid" ] || continue
      case $'\n'"$pre_carried"$'\n' in
        *$'\n'"$pid"$'\n'*) ;;
        *)
          lost_bad=1
          break
          ;;
      esac
    done < <(comm -23 <(printf '%s\n' "$pre_unique") <(printf '%s\n' "$post_unique"))
  fi
  if [ "$lost_bad" = "1" ]; then
    note "rebase replay anomaly on $tid: branch carried $pre_n unique commits, $post_n replayed - dropped from the batch"
    board_comment "$tid" "train: rebase replay anomaly on $bra rebased onto $BASE - branch carried $pre_n unique commits, $post_n were replayed. Landing ABORTED, the branch is kept, card back to ready with this evidence."
    demote_card "$tid" "merge train: rebase replay anomaly ($pre_n unique commits zero-replayed). On $bra - verify the branch content against dev, then rerun."
    drop_card 4
    continue
  fi
  if ! git merge --no-ff -q -m "Merge card $tid: $(cut -c1-60 <<<"$title") (work by $who)" "$bra" 2>/dev/null; then
    note "conflict at the merge stage (post-rebase) for $tid"
    conflict_step "$tid" "$bra" "$pre"
    drop_card "$CARD_RC"
    continue
  fi
  mergesha="$(git rev-parse HEAD)"
  if [ "$mergesha" = "$pre" ] || ! two_parent "$mergesha"; then
    git reset -q --hard "$pre" >/dev/null 2>&1 || true
    note "landing aborted: merge of $bra produced no two-parent merge commit (mergesha was $mergesha)"
    board_comment "$tid" "train: the merge of $bra degenerated to a no-op (mergesha = $mergesha = pre-pass tip or single-parent). Landing ABORTED, dev untouched, card back to ready with this evidence."
    demote_card "$tid" "merge train: no-op merge detected. On $bra - a real merge commit is required to land."
    drop_card 4
    continue
  fi
  B_LAND+=("$tid|$bra|$mergesha|$pre")
  note "merged $tid at $mergesha"
done

if [ "${#B_LAND[@]}" -eq 0 ]; then
  note "batch: nothing merged this pass (${#BATCH[@]} card(s) dropped before the battery)"
  exit "$batch_rc"
fi

# ---- the battery: ONE gate run for the whole batch ---------------------
# The battery's diff base is the BATCH base: the pass gates the whole batch,
# not one card, so the embed-lane change gate and the -race package set cover
# every merged card. Nothing is pushed before this is green on the final tree.

# attribute_red: walk the batch with scripts/train-bisect.sh and set CULPRIT.
# The walk scores trees with the pass's own battery (--gate-only), so a
# candidate can only ever be judged by the legs the pass just failed on.
# Empty CULPRIT = the walk could not name a card of this batch.
attribute_red() {
  local entry chain="" bad rc
  for entry in "${B_LAND[@]}"; do
    chain="${chain:+$chain,}${entry%%|*}=$(cut -d'|' -f3 <<<"$entry")"
  done
  bad="$(git rev-parse HEAD)"
  if [ -n "${TRAIN_BISECT_CMD:-}" ]; then
    bisect_cmd="$TRAIN_BISECT_CMD"
  else
    bisect_cmd="bash '$SELF' --gate-only"
    export TRAIN_GATE_ONLY_PRE="$batch_base"
  fi
  bisect_log="$(mktemp "${TMPDIR:-/tmp}"/train-bisect.XXXXXX.log)"
  note "train-bisect over ${#B_LAND[@]} candidate(s): $chain"
  set +e
  bash "$SELF_DIR/train-bisect.sh" "$bad" --cmd "$bisect_cmd" --chain "$chain" >"$bisect_log" 2>&1
  rc=$?
  set -e
  tail -6 "$bisect_log" 2>/dev/null || true
  if [ "$rc" != "0" ]; then
    echo "train-bisect refused the walk (rc $rc) - see $bisect_log" >&2
  fi
  CULPRIT="$(sed -n 's/^culprit: landing //p' "$bisect_log" | head -1)"
  if [ -z "$CULPRIT" ]; then
    return 0
  fi
  case ",$batch_ids," in
    *",$CULPRIT,"*) ;;
    *)
      note "train-bisect named $CULPRIT, which is not a card of this batch - unattributed"
      CULPRIT=""
      ;;
  esac
  return 0
}

# A card that cannot follow the culprit onto the new tip is dropped from the
# batch: branch kept, card back to ready, evidence on the card.
drop_reland() {  # <task_id> <branch> <reason>
  local id="$1" bra="$2" why="$3"
  note "re-land of $id failed: $why"
  board_comment "$id" "train: red gate on the batch battery ($batch_ids), culprit $CULPRIT un-landed - this card's own commits on $bra did not re-land onto the culprit-free tip ($why). Dropped from the batch, branch kept, card back to ready."
  demote_card "$id" "merge train: re-land after the red-battery culprit failed ($why). On $bra - rebase onto the new dev tip, then rerun."
}

# unland_culprit <card-id> <evidence-log>: dev back to the culprit's pre-merge
# tip, the culprit back to ready with the evidence, and every card that came
# AFTER it re-merged onto the new tip. Cards before it are untouched (their
# merges are ancestors of that tip). Rewrites B_LAND to the surviving landings.
unland_culprit() {
  local culprit="$1" evid="$2" entry id bra oldsha newsha newpre
  local culsha="" culbra="" pretip="" seen=0 prev_old
  local pre_cards=() post_cards=() survivors=()
  for entry in "${B_LAND[@]}"; do
    id="${entry%%|*}"
    if [ "$seen" = "0" ]; then
      if [ "$id" = "$culprit" ]; then
        seen=1
        culbra="$(cut -d'|' -f2 <<<"$entry")"
        culsha="$(cut -d'|' -f3 <<<"$entry")"
        pretip="$(cut -d'|' -f4 <<<"$entry")"
        continue
      fi
      pre_cards+=("$entry")
      continue
    fi
    post_cards+=("$entry")
  done
  [ -n "$culsha" ] || die "train-bisect named $culprit but no merged card matches it"

  git checkout -q "$BASE" 2>/dev/null || true
  git reset -q --hard "$pretip"
  git clean -qfd api/static >/dev/null 2>&1 || true
  note "un-landed $culprit: $BASE back to ${pretip:0:12}"

  board_comment "$culprit" "train: red gate on the batch battery ($batch_ids) - train-bisect names this card's landing ($culsha) as the culprit. Un-landed, back to ready. Failure evidence (tail):

$(tail -c 1200 "$evid")

Full gate log: $evid
Bisect log: ${bisect_log:-<none>}"
  demote_card "$culprit" \
    "merge train: red gate - this landing ($culsha) is the batch battery's culprit per train-bisect. Un-landed; rebased branch $culbra is KEPT."

  survivors=("${pre_cards[@]}")
  prev_old="$culsha"
  for entry in "${post_cards[@]}"; do
    id="${entry%%|*}"; bra="$(cut -d'|' -f2 <<<"$entry")"; oldsha="$(cut -d'|' -f3 <<<"$entry")"
    newpre="$(git rev-parse HEAD)"
    # --onto: the card was rebased onto the culprit's landing, so the culprit
    # must be the exclusion point - a plain merge would carry its changes back.
    if ! git rebase --onto "$newpre" "$prev_old" "$bra" >/dev/null 2>&1; then
      git rebase --abort 2>/dev/null || true
      git checkout -q -f "$BASE" 2>/dev/null || true
      git reset -q --hard "$newpre"
      drop_reland "$id" "$bra" "rebase conflict"
      prev_old="$oldsha"
      continue
    fi
    git checkout -q "$BASE"
    if ! git merge --no-ff -q -m "Merge card $id: $(cut -c1-60 <<<"$(task_field "$id" title || true)") (work by $(task_field "$id" assignee || true))" "$bra" 2>/dev/null; then
      git merge --abort 2>/dev/null || true
      git checkout -q -f "$BASE" 2>/dev/null || true
      git reset -q --hard "$newpre"
      drop_reland "$id" "$bra" "merge conflict"
      prev_old="$oldsha"
      continue
    fi
    newsha="$(git rev-parse HEAD)"
    if [ "$newsha" = "$newpre" ] || ! two_parent "$newsha"; then
      git reset -q --hard "$newpre"
      drop_reland "$id" "$bra" "no-op merge"
      prev_old="$oldsha"
      continue
    fi
    survivors+=("$id|$bra|$newsha|$newpre")
    note "re-landed $id at $newsha"
    prev_old="$oldsha"
  done
  B_LAND=("${survivors[@]}")
}

# rollback_batch <evidence-log>: the whole batch un-lands. Used when the red
# cannot be pinned to one card (the walk refused, or the tree is still red
# without the culprit) - a red dev never gets a push.
rollback_batch() {
  local evid="$1" entry id bra st
  git checkout -q "$BASE" 2>/dev/null || true
  git reset -q --hard "$batch_base"
  git clean -qfd api/static >/dev/null 2>&1 || true
  note "un-landed the WHOLE batch: $BASE back to ${batch_base:0:12}"
  for entry in "${B_LAND[@]}"; do
    id="${entry%%|*}"; bra="$(cut -d'|' -f2 <<<"$entry")"
    st="$(task_field "$id" status || true)"
    [ "$st" = "done" ] || continue
    board_comment "$id" "train: red gate on the batch battery ($batch_ids) and the red could not be pinned to one card - the WHOLE batch was un-landed, back to ready. Failure evidence (tail):

$(tail -c 1200 "$evid")

Full gate log: $evid
Bisect log: ${bisect_log:-<none>}"
    demote_card "$id" \
      "merge train: red gate on the batch battery - the whole batch un-landed (batch: $batch_ids). Failure tail on the card; rebased branch $bra is KEPT."
  done
}

run_battery "$batch_base"
bisect_used="no"

if [ "$gates_rc" != "0" ]; then
  tail -25 "$gates_log" >&2 || true
  CULPRIT=""
  if [ "${#B_LAND[@]}" = "1" ]; then
    # One merged card is its own attribution - a walk over one candidate
    # would cost two probe batteries to say what the batch already says.
    CULPRIT="${B_LAND[0]%%|*}"
    bisect_used="no (single card $CULPRIT)"
    note "red gate: battery rc $gates_rc on the one merged card $CULPRIT - no walk needed"
  else
    note "red gate: battery rc $gates_rc over ${#B_LAND[@]} merged card(s) - walking the batch"
    attribute_red
    if [ -z "$CULPRIT" ]; then
      bisect_used="no (unattributed)"
      note "train-bisect could not name a card of this batch - un-landing the WHOLE batch"
      rollback_batch "$gates_log"
      note "batch: cards=$batch_ids battery=${battery_total}s runs=$battery_runs bisect=$bisect_used landed=0"
      exit 6
    fi
    bisect_used="yes ($CULPRIT)"
  fi

  note "un-landing $CULPRIT and re-merging the cards after it"
  unland_culprit "$CULPRIT" "$gates_log"
  if [ "${#B_LAND[@]}" -eq 0 ]; then
    note "batch: cards=$batch_ids battery=${battery_total}s runs=$battery_runs bisect=$bisect_used landed=0"
    note "nothing survives the un-land - $BASE sits at ${batch_base:0:12}"
    exit 6
  fi

  # The batch tree changed: gate it ONCE more before anything is pushed. A red
  # here says the culprit's removal was not the whole story.
  note "battery re-run on the culprit-free tip (${#B_LAND[@]} card(s))"
  run_battery "$batch_base"
  if [ "$gates_rc" != "0" ]; then
    tail -25 "$gates_log" >&2 || true
    note "red gate again without $CULPRIT - un-landing the WHOLE batch (nothing is pushed ungated)"
    rollback_batch "$gates_log"
    note "batch: cards=$batch_ids battery=${battery_total}s runs=$battery_runs bisect=$bisect_used landed=0"
    exit 6
  fi
fi

grep -q '^FIN$' "$gates_log" || {
  rollback_batch "$gates_log"
  note "batch: cards=$batch_ids battery=${battery_total}s runs=$battery_runs bisect=$bisect_used landed=0"
  die "the battery broke its own structure (no FIN marker) - the whole batch is un-landed"
}
note "battery green: ${battery_total}s over $battery_runs run(s)"
note "batch: cards=$batch_ids battery=${battery_total}s runs=$battery_runs bisect=$bisect_used landed=${#B_LAND[@]}"
# A skipped leg must be visible in the PASS log too, not only in the gate log
# (a green pass prints no gate detail, and a permanent ruling nobody can see
# is how a "temporary" workaround becomes invisible debt - t_7cc44bf1).
grep -m1 'race leg skipped' "$gates_log" 2>/dev/null || true

# ---- land --------------------------------------------------------------

have_origin() { grep -q '^origin$' <<<"$(git remote 2>/dev/null)"; }

# marker: refs/train/<seq>-<task_id> at each card's merge commit. Zero-padded
# seq keeps refname sort == chronological order, which train-bisect walks on.
# FULL refnames only: %(refname:short) elides refs/ -> 'train/0001-x', which
# once made the seq parse crash and reset the chain to 0001 forever (found on
# the 2nd real landing). A batch allocates one seq per card, in merge order,
# and every marker is written BEFORE the push so a landing never reaches
# origin without its marker.
last_ref="$(git for-each-ref --format='%(refname)' refs/train 2>/dev/null | sort | tail -1)"
seq=0
if [ -n "$last_ref" ]; then
  tmp="${last_ref##*/}"   # refs/train/0001-t_x -> 0001-t_x
  tmp="${tmp%%-*}"        # -> 0001
  case "$tmp" in
    ''|*[!0-9]*) die "latest marker '$last_ref' carries no numeric seq - the marker chain is corrupted; resolve by hand, the train never guesses" ;;
  esac
  seq=$((10#$tmp))
fi

landed_markers=()
landed_seq=()
for entry in "${B_LAND[@]}"; do
  tid="${entry%%|*}"
  mergesha="$(cut -d'|' -f3 <<<"$entry")"
  seq=$((seq + 1))
  s="$(printf '%04d' "$seq")"
  marker="refs/train/$s-$tid"
  git update-ref "$marker" "$mergesha"
  note "landed $tid at $mergesha; marker ref $marker"
  landed_markers+=("$marker")
  landed_seq+=("$s")
done

if [ "$PUSH" = "1" ] && have_origin; then
  git push -q origin "$BASE"
  for marker in "${landed_markers[@]}"; do
    git push -q origin "$marker:$marker" \
      || echo "    (marker push refused - $marker is local; push refs/train/* by hand later)"
  done
fi

# ---- board bookkeeping + branch sweep ----------------------------------

i=0
for entry in "${B_LAND[@]}"; do
  tid="${entry%%|*}"
  bra="$(cut -d'|' -f2 <<<"$entry")"
  mergesha="$(cut -d'|' -f3 <<<"$entry")"
  s="${landed_seq[$i]}"
  marker="refs/train/$s-$tid"
  i=$((i + 1))
  board_comment "$tid" "train: landed $mergesha on $BASE, marker ref $marker."
  "$HERMES_BIN" kanban edit "$tid" \
    --summary "landed via merge train: $mergesha, marker $marker" \
    --metadata "{\"landed_by\":\"train-pass\",\"merge_sha\":\"$mergesha\",\"marker\":\"$marker\",\"batch\":\"$batch_ids\"}" \
    >/dev/null 2>&1 || note "(result edit skipped: the hermes CLI refused in this context; the landing comment is on the card)"
  git branch -d -q "$bra" 2>/dev/null || true
  echo "train: DONE $tid -> $mergesha, marker $marker"
done

exit "$batch_rc"
