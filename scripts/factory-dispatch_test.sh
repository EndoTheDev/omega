#!/usr/bin/env bash
set -uo pipefail

# factory-dispatch_test.sh - runnable checks for scripts/factory-dispatch.sh.
# Run: bash scripts/factory-dispatch_test.sh
# T1 pins the empty-RUNNING kill: the class-law RC loop must survive a zero
# running board under set -euo pipefail (proven live 2026-10-24: the guard
# died silently, exit 1, no verdicts, whenever no card was running).
# Every seed board carries the FULL tasks shape the guard reads today
# (worker_pid + last_heartbeat_at, guard bug #14's running-snapshot mirror
# rule): a narrower fixture table made every DB read die 'no such column'
# and red 26 checks that had nothing to do with the change under test.
# T16 pins the dry-run's PAIR VERDICT - the head set's pairing law, stated
# outright so the tick's --max decision is mechanical (go+go at running=0 is
# the live miss it was written for).
#
# T2 is a live dry-run smoke against the REAL resolved board (local only:
# skipped when the resolution refuses - no board DB at all, e.g. CI runners;
# that refusal itself must be loud and nonzero).
# T5-T8 pass GUARD through the ENVIRONMENT into `bash -c` (proven live
# 2026-11-05 on winbtw/bash 5.3: the `'"'"` quote dance inside the -cstring
# generated unexpected-EOF and the sourced functions never existed).
# DB access everywhere is the python sqlite3 module: the sqlite3 CLI is
# absent on win fleet hosts (proven live 2026-11-05 on winbtw). Fake-home
# fixtures pass MIXED (C:/...) paths to any python consumer - a windows
# python cannot read MSYS /tmp paths, so cygpath -m bridges (POSIX hosts pass
# through). Every guard call clears the ambient hermes env and pins
# HERMES_KANBAN_HOME to the fixture: a profile-routed host (HERMES_HOME set)
# must never leak the REAL board into a test run.

GUARD="$(cd "$(dirname "$0")" && pwd)/factory-dispatch.sh"

# ---- FENCE ROOT (card t_8ff31265) ------------------------------------------
# Every LIVE pass ends in `hermes kanban dispatch` behind the FENCE STAMP: the
# guard cds to `dirname "$0"/..` and refuses to dispatch at all (exit 1,
# "FENCE STAMP MISSING: <root>/bin/shims/git absent") unless THAT tree carries
# an executable bin/shims/git.
#
# That is why this suite used to be location-sensitive, and the mechanism was
# never stub-hermes PATH resolution: a copy of `scripts/factory-dispatch.sh` in
# a scratch dir makes the guard root the scratch dir's PARENT - a tree with no
# bin/shims - so the fence refused before the stub `hermes` on PATH was ever
# consulted, and 23 live-leg assertions (T9/T10/T11/T13/T19/T20) red as if the
# clamp or the claimable gate had regressed. The reviewer law forbids mutating
# the worktree under review, so a reviewer ALWAYS runs from a scratch copy and
# ALWAYS read that false red (vera, review of t_42ff3dad, 2026-11-13).
#
# The shim is a checkout artifact: only a checkout can satisfy the stamp. So a
# scratch copy gets a STAGED root - the same guard bytes at <stage>/scripts/
# factory-dispatch.sh with a real bin/shims/git beside them - and the live legs
# run the file under test exactly as a checkout would. A checkout that has LOST
# its shim is the opposite case (a guard regression), so that stays a hard red.
GUARD_ROOT="$(cd "$(dirname "$GUARD")/.." && pwd)"
stage_fence_root() {  # $1 = guard path -> copy of it inside a fence-shaped root
  local src="$1" stg real
  stg=$(mktemp -d) || return 1
  mkdir -p "$stg/scripts" "$stg/bin/shims" || return 1
  cp "$src" "$stg/scripts/factory-dispatch.sh" || return 1
  real=$(command -v git || true)
  # An absolute real-git path, never a bare `git`: the guard prepends this dir
  # to PATH for the dispatch call, so a bare name would re-enter this shim.
  {
    echo '#!/usr/bin/env bash'
    echo '# staged fence shim (test harness, card t_8ff31265): the guard only'
    echo '# requires an executable bin/shims/git to stamp a live dispatch pass.'
    if [ -n "$real" ]; then printf 'exec "%s" "$@"\n' "$real"; else echo 'exit 0'; fi
  } >"$stg/bin/shims/git" || return 1
  chmod +x "$stg/bin/shims/git" || return 1
  printf '%s' "$stg/scripts/factory-dispatch.sh"
}
if [ ! -x "$GUARD_ROOT/bin/shims/git" ]; then
  # A REAL omega checkout that carries this suite's own guard but lost the shim
  # is a guard regression, not a location artifact: name it and stop, never
  # stage a fake fence over a broken checkout.
  if [ -e "$GUARD_ROOT/.git" ] && [ -f "$GUARD_ROOT/scripts/factory-dispatch.sh" ]; then
    echo "factory-dispatch_test: bin/shims/git is MISSING from the checkout at $GUARD_ROOT - every live pass refuses to dispatch without the fence stamp, so this is a guard regression, not an environment gap" >&2
    exit 2
  fi
  STAGED_GUARD=$(stage_fence_root "$GUARD") \
    || { echo "factory-dispatch_test: cannot stage a fence root for $GUARD - the guard root $GUARD_ROOT has no bin/shims/git and no staging dir could be made" >&2; exit 2; }
  echo "fence: $GUARD_ROOT has no bin/shims/git (not an omega checkout) - live legs run a staged copy at ${STAGED_GUARD%/*} (same guard bytes, the stamp's own shim beside them)"
  trap 'rm -rf "${STAGED_GUARD%/scripts/factory-dispatch.sh}"' EXIT
  GUARD="$STAGED_GUARD"
fi

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }

PY=$(command -v python3 || command -v python)
[ -n "$PY" ] || { echo "factory-dispatch_test: no python on PATH (boards are queried through the python sqlite3 module)" >&2; exit 1; }
mixpath() { cygpath -m "$1" 2>/dev/null || printf '%s' "$1"; }
# same reader the guard itself uses (scripts/factory-dispatch.sh DBPY)
DBPY='
import sqlite3, sys
sys.stdout.reconfigure(newline="\n")   # windows: no \r\n row translation
path, mode, sql = sys.argv[1], sys.argv[2], sys.argv[3]
what = "write" if mode == "x" else "read"
try:
    con = sqlite3.connect(path, timeout=10)
except sqlite3.Error as exc:
    sys.stderr.write("DB open failed: %s\n" % exc)
    sys.exit(3)
try:
    if mode == "x":
        con.execute(sql)
        con.commit()
    else:
        for row in con.execute(sql):
            sys.stdout.write("|".join("" if v is None else str(v) for v in row) + "\n")
except sqlite3.Error as exc:
    sys.stderr.write("DB %s failed: %s\n" % (what, exc))
    sys.exit(3)
finally:
    con.close()
'
sqlex() {  # multi-statement fixture seed (executescript)
  "$PY" -c '
import sqlite3, sys
con = sqlite3.connect(sys.argv[1], timeout=10)
con.executescript(sys.argv[2])
con.commit()
' "$1" "$2"; }
sql() { "$PY" -c "$DBPY" "$1" q "$2"; }

# T1: the RC-loop shape with an empty RUNNING array must not kill the shell.
out=$(bash -c '
set -euo pipefail
RUNNING=()
RC=$(for r in "${RUNNING[@]:-}"; do [ -n "$r" ] && echo go || true; done | sort -u | tr "\n" ",")
echo "RC=[$RC]"
' 2>&1)
rc=$?
[ "$rc" -eq 0 ] && ok || bad "T1: empty RUNNING array killed the RC loop (exit $rc) - the || true guard is gone"
printf '%s' "$out" | grep -q 'RC=\[' && ok || bad "T1: expected RC= line, got: $out"

# T2 (local only): dry-run on the REAL resolved board must exit 0 with
# output - a silent empty run is the empty-RUNNING kill signature. No board
# DB anywhere must refuse LOUDLY (nonzero exit) - the old bug printed
# verdicts from empty lists with exit 0.
out=$(HERMES_HOME= HERMES_KANBAN_HOME= HERMES_KANBAN_DB= HERMES_KANBAN_BOARD= bash "$GUARD" --dry-run 2>&1)
rc=$?
if printf '%s' "$out" | grep -q "board DB not found"; then
  [ "$rc" -ne 0 ] && ok || bad "T2: missing live board refused with exit 0 - the silent-empty signature"
  echo "T2: skipped (no live board DB resolved; refusal was loud, exit $rc)"
else
  [ "$rc" -eq 0 ] && ok || bad "T2: dry-run exited $rc (expected 0)"
  [ -n "$out" ] && ok || bad "T2: dry-run printed nothing (exit $rc) - the empty-RUNNING kill signature"
fi

# T3 (local + CI): guard bug #4 - review-park SQL. A review-state card
# gets the dispatch-hold sentinel ONLY when its lock is free; a foreign
# sentinel (design-hold) survives; unpark restores NULL, never ''.
T3DB=$(mktemp); T3DB_N=$(mixpath "$T3DB")
sqlex "$T3DB_N" "CREATE TABLE tasks(id TEXT, status TEXT, claim_lock TEXT);
INSERT INTO tasks VALUES('t_rev_free','review',NULL);
INSERT INTO tasks VALUES('t_rev_held','review','pibtw:design-hold');
INSERT INTO tasks VALUES('t_ready1','ready',NULL);"
sqlex "$T3DB_N" "UPDATE tasks SET claim_lock='pibtw:dispatch-hold' WHERE id='t_rev_free' AND (claim_lock IS NULL OR claim_lock='');"
l=$(sql "$T3DB_N" "SELECT claim_lock FROM tasks WHERE id='t_rev_free';")
[ "$l" = "pibtw:dispatch-hold" ] && ok || bad "T3: free review card not parked (claim_lock='$l')"
l=$(sql "$T3DB_N" "SELECT claim_lock FROM tasks WHERE id='t_rev_held';")
[ "$l" = "pibtw:design-hold" ] && ok || bad "T3: park overwrote a foreign sentinel ('$l')"
sqlex "$T3DB_N" "UPDATE tasks SET claim_lock=NULL WHERE id='t_rev_free' AND claim_lock='pibtw:dispatch-hold';"
l=$(sql "$T3DB_N" "SELECT claim_lock IS NULL FROM tasks WHERE id='t_rev_free';")
[ "$l" = "1" ] && ok || bad "T3: unpark left claim_lock='$l' (must restore NULL, never '')"
rm -f "$T3DB"

# T4 (local + CI): guard bug #5 - the candidate sort must be priority
# first (DESC, higher = more urgent), ties oldest-first.
T4DB=$(mktemp); T4DB_N=$(mixpath "$T4DB")
sqlex "$T4DB_N" "CREATE TABLE tasks(id TEXT, status TEXT, claim_lock TEXT, priority INTEGER DEFAULT 0, created_at TEXT);
INSERT INTO tasks VALUES('t_old_low','ready',NULL,0,'2026-10-01');
INSERT INTO tasks VALUES('t_mid_arc','ready',NULL,5,'2026-10-20');
INSERT INTO tasks VALUES('t_new_urgent','ready',NULL,10,'2026-10-26');
INSERT INTO tasks VALUES('t_newer_low','ready',NULL,0,'2026-10-27');"
order=$(sql "$T4DB_N" "SELECT id FROM tasks WHERE status='ready' ORDER BY priority DESC, created_at;")
[ "$order" = "t_new_urgent
t_mid_arc
t_old_low
t_newer_low" ] && ok || bad "T4: priority sort wrong - got: $order"
rm -f "$T4DB"

# T5 (local + CI): guard bug #6 - brace-list FILES surfaces. A FILES line
# like `harness/{cron,memory}/` must expand to every member before token
# extraction (proven live 2026-10-26: it collapsed to a bare modules/
# token and the guard read a harness/cron collision false-legal). The
# eval gate admits path chars only; metachar lines pass through raw.
# Also pins the bash 5.3 =~ shape: the regex must ride an UNQUOTED var -
# a quoted RHS is literal in 5.3 and matches nothing.
tok=$(GUARD="$GUARD" bash -c '
source <(sed -n "/^move_paths()/,/^}/p; /^tokens_of()/,/^}/p" "$GUARD")
printf "FILES: harness/{cron,memory,delegate}/\nplain harness/x/ text\ncmd;bad {a,b}\n" | tokens_of
' 2>/dev/null)
[ "$(printf "%s\n" "$tok" | grep -c "^harness/")" = "4" ] \
	&& ok || bad "T5: brace list emitted $(printf "%s\n" "$tok" | grep -c "^harness/") tokens, expected 4 (got: $tok)"
printf "%s" "$tok" | grep -q "^harness/cron/" && ok || bad "T5: harness/cron/ missing from expansion"
printf "%s" "$tok" | grep -q "^harness/delegate/" && ok || bad "T5: harness/delegate/ missing from expansion"
printf "%s" "$tok" | grep -q "cmd;bad" && bad "T5: metachar line was eval-expanded (gate failed)" || ok

# T6 (local + CI): guard bug #7 - move-shaped lines. A move line names the
# card's WHOLE write surface, including source paths the rooted prefix scan
# can never see (proven live 2026-10-28: t_7e47d18f "webui/ -> engine/webui/"
# spawned beside the running webui settings card; verdicts read them disjoint).
# Prose arrows and the literal "moves X to Y" phrase must stay silent.
tok=$(GUARD="$GUARD" bash -c '
source <(sed -n "/^move_paths()/,/^}/p; /^tokens_of()/,/^}/p" "$GUARD")
printf "%s\n" \
  "shell ring move: webui/ -> webui/, http_channel/ -> api/" \
  "moves agent/ ai/ tui/ webui/ + docs + CI paths" \
  "harness/{cron,memory}/ dissolves into core/" \
  "an arrow (\`->\`, \`=>\`) in prose with no paths" \
  | tokens_of
' 2>/dev/null)
for want in "webui/" "http_channel/" "api/" \
            "agent/" "ai/" "tui/" "harness/cron/" "harness/memory/" "core/"; do
  printf "%s" "$tok" | grep -qx "$want" && ok || bad "T6: move token '$want' missing (got: $tok)"
done
printf "%s" "$tok" | grep -q "arrow" && bad "T6: prose arrow line leaked tokens" || ok

# T7 (local + CI): the collision itself - a move source token collides with a
# running card's edited webui file under the existing prefix-overlap law.
rc=$(GUARD="$GUARD" bash -c '
source <(sed -n "/^move_paths()/,/^}/p; /^tokens_of()/,/^}/p; /^overlaps()/,/^}/p" "$GUARD")
ctok=$(mktemp); rtok=$(mktemp)
printf "engine ring move: webui/ -> engine/webui/\n" | tokens_of >"$ctok"
printf "webui/app/components/settings/groups.svelte\n" >"$rtok"
overlaps "$ctok" "$rtok"; r=$?
rm -f "$ctok" "$rtok"; exit $r
' 2>/dev/null; echo $?)
[ "$rc" = "0" ] && ok || bad "T7: move source webui/ did NOT collide with webui/app/ edit (rc=$rc)"

# T8 (local + CI): FILES-override + move law. A FILES line naming only move
# TARGETS must still surface the SOURCES from the body - the exact t_7e47d18f
# shape (FILES: engine/..., body says the sources).
rc=$(GUARD="$GUARD" bash -c '
source <(sed -n "/^move_paths()/,/^}/p; /^tokens_of()/,/^}/p" "$GUARD")
files="FILES: engine/webui/"
body="$files
engine ring move: webui/ -> engine/webui/"
{ tokens_of <<<"$files"; move_paths <<<"$body"; } | grep -qx "webui/" >/dev/null 2>&1
echo $?
' 2>/dev/null)
[ "$rc" = "0" ] && ok || bad "T8: FILES-only-target move card lost its source surface (rc=$rc)"

# T9 (local + CI): end-to-end on a FAKE board, no real dispatch. Pins both
# fixes of the night: (a) the t_7e47d18f shape - FILES line naming only move
# TARGETS - collides with a running move card whose body names the sources
# ("webui/ -> engine/webui/"); (b) the handoff report - re-read running AFTER
# the stub dispatch, report delta, flag over-run (guard bug #8). Board sits
# under the hermes home given via HERMES_KANBAN_HOME, active through
# <root>/kanban/current - the resolution the live tick rides.
FDHOME=$(mktemp -d); FDHOME_N=$(mixpath "$FDHOME")
mkdir -p "$FDHOME/kanban/boards/project-omega" "$FDHOME/bin"
printf 'project-omega\n' >"$FDHOME/kanban/current"
FDB_N="$FDHOME_N/kanban/boards/project-omega/kanban.db"
sqlex "$FDB_N" "CREATE TABLE tasks(id TEXT, status TEXT, title TEXT DEFAULT '', body TEXT DEFAULT '',
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT,
  worker_pid INTEGER, last_heartbeat_at INTEGER);
INSERT INTO tasks VALUES('t_move','running','engine ring move','engine ring move: webui/ -> engine/webui/','pibtw:1',NULL,0,'2026-10-28T01:00',NULL,NULL);
INSERT INTO tasks VALUES('t_cand','ready','engine ring move implement','FILES: engine/webui/
engine ring move: webui/ -> engine/webui/',NULL,0,5,'2026-10-28T02:00',NULL,NULL);"
cat >"$FDHOME/bin/hermes" <<'EOF'
#!/usr/bin/env bash
py=$(command -v python3 || command -v python)
exec "$py" - "$HERMES_STUB_DB" <<'PYEOF'
import sqlite3, sys
# stub dispatch: mimic inner behavior - release_stale_claims drops an expired
# running card to ready, then spawn MORE cards than the guard requested.
con = sqlite3.connect(sys.argv[1], timeout=10)
for s in (
    "UPDATE tasks SET status='ready', claim_lock=NULL WHERE id='t_move';",
    "INSERT INTO tasks VALUES('t_new1','running','x','',NULL,NULL,0,'2026-10-28T03:00',NULL,NULL);",
    "INSERT INTO tasks VALUES('t_new2','running','x','',NULL,NULL,0,'2026-10-28T03:01',NULL,NULL);",
    "INSERT INTO tasks VALUES('t_new3','running','x','',NULL,NULL,0,'2026-10-28T03:02',NULL,NULL);",
    "INSERT INTO tasks VALUES('t_new4','running','x','',NULL,NULL,0,'2026-10-28T03:03',NULL,NULL);",
):
    con.execute(s)
con.commit()
print("Spawned: 4 (t_new1 t_new2 t_new3 t_new4)")
PYEOF
EOF
chmod +x "$FDHOME/bin/hermes"
out=$(HOME="$FDHOME" HERMES_HOME= HERMES_KANBAN_HOME="$FDHOME_N" HERMES_KANBAN_DB= PATH="$FDHOME/bin:$PATH" HERMES_STUB_DB="$FDB_N" bash "$GUARD" --dry-run 2>&1)
rc=$?
[ "$rc" -eq 0 ] && ok || bad "T9: dry-run exited $rc"
printf "%s" "$out" | grep -q "^hold: t_cand" && ok || bad "T9: move card did not hold the webui candidate (out: $out)"
printf "%s" "$out" | grep -q "dry-run: no dispatch" && ok || bad "T9: dry-run dispatched (out: $out)"
out=$(HOME="$FDHOME" HERMES_HOME= HERMES_KANBAN_HOME="$FDHOME_N" HERMES_KANBAN_DB= PATH="$FDHOME/bin:$PATH" HERMES_STUB_DB="$FDB_N" bash "$GUARD" 2>&1)
rc=$?
[ "$rc" -eq 0 ] && ok || bad "T9: dispatch pass exited $rc (out: $out)"
printf "%s" "$out" | grep -q "wanted <=2 running (snapshot 1 + slots 1)" && ok || bad "T9: handoff line wrong (out: $out)"
printf "%s" "$out" | grep -q "after: 4 (delta 3 since snapshot)" && ok || bad "T9: post-call re-read wrong (out: $out)"
printf "%s" "$out" | grep -q "over-run" && ok || bad "T9: over-run not reported (out: $out)"
l=$(sql "$FDB_N" "SELECT claim_lock IS NULL FROM tasks WHERE id='t_cand';")
[ "$l" = "1" ] && ok || bad "T9: unpark left t_cand locked ('$l')"
rm -rf "$FDHOME"

# T10 (local + CI): guard bug #10 (proven live 2026-10-28) - the explicit
# --max N path never set SLOTS, so the handoff echo died under set -u AFTER
# the spawn ("SLOTS: unbound variable", exit 1, every later line skipped).
# Same stub shape as T9, one running card, --max 2: the pass must exit 0 and
# report the true slot count (2 - 1 = 1).
FDHOME=$(mktemp -d); FDHOME_N=$(mixpath "$FDHOME")
mkdir -p "$FDHOME/kanban/boards/project-omega" "$FDHOME/bin"
printf 'project-omega\n' >"$FDHOME/kanban/current"
FDB_N="$FDHOME_N/kanban/boards/project-omega/kanban.db"
sqlex "$FDB_N" "CREATE TABLE tasks(id TEXT, status TEXT, title TEXT DEFAULT '', body TEXT DEFAULT '',
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT,
  worker_pid INTEGER, last_heartbeat_at INTEGER);
INSERT INTO tasks VALUES('t_go','running','nm purity','FILES: scripts/nm-purity.sh','pibtw:1',NULL,0,'2026-10-28T01:00',NULL,NULL);
INSERT INTO tasks VALUES('t_web','ready','webui fix','FILES: engine/webui/',NULL,0,5,'2026-10-28T02:00',NULL,NULL);"
cat >"$FDHOME/bin/hermes" <<'EOF'
#!/usr/bin/env bash
echo "Spawned: 1 (t_web)"
EOF
chmod +x "$FDHOME/bin/hermes"
out=$(HOME="$FDHOME" HERMES_HOME= HERMES_KANBAN_HOME="$FDHOME_N" HERMES_KANBAN_DB= PATH="$FDHOME/bin:$PATH" bash "$GUARD" --max 2 2>&1)
rc=$?
[ "$rc" -eq 0 ] && ok || bad "T10: --max 2 pass exited $rc - SLOTS unset on the explicit path (out: $out)"
printf "%s" "$out" | grep -q "wanted <=2 running (snapshot 1 + slots 1)" && ok || bad "T10: handoff line wrong (out: $out)"
rm -rf "$FDHOME"

# T11 (local + CI): guard bug #11 - the dispatch sweep must not lift NAMED
# hold locks. recompute_ready (kanban_db.py:2035) runs on every inner
# dispatch and promotes todo/blocked cards whose parents are all done -
# a parentless card satisfies that vacuously, deliberate SQL parks carry
# no sticky kanban_block event, so every pass flipped parked holds
# (blocked + claim_lock='pibtw:v0.14.0-hold') to ready and the tick
# re-parked them (proven live 2026-10-29/30, t_55a3fc4b/t_95c91f9c).
# The stub mimics recompute_ready exactly. Pinned here: a dry-run never
# flips the parked card; a real pass flips AND the guard re-parks both
# named holds (lock intact); a pid-shaped lock is NOT re-parked (the
# restore fires on named holds only - pid claims stay sweep-reapable).
FDHOME=$(mktemp -d); FDHOME_N=$(mixpath "$FDHOME")
mkdir -p "$FDHOME/kanban/boards/project-omega" "$FDHOME/bin"
printf 'project-omega\n' >"$FDHOME/kanban/current"
FDB_N="$FDHOME_N/kanban/boards/project-omega/kanban.db"
sqlex "$FDB_N" "CREATE TABLE tasks(id TEXT, status TEXT, title TEXT DEFAULT '', body TEXT DEFAULT '',
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT,
  worker_pid INTEGER, last_heartbeat_at INTEGER);
INSERT INTO tasks VALUES('t_run','running','go bits','pure go bits','pibtw:1',NULL,0,'2026-10-30T01:00',NULL,NULL);
INSERT INTO tasks VALUES('t_cand','ready','webui candidate','FILES: engine/webui/app/',NULL,NULL,0,'2026-10-30T02:00',NULL,NULL);
INSERT INTO tasks VALUES('t_hold','blocked','terminal module hold','design gate','pibtw:v0.14.0-hold',NULL,0,'2026-10-29T01:00',NULL,NULL);
INSERT INTO tasks VALUES('t_hold2','blocked','browser module hold','design gate','pibtw:design-hold',NULL,0,'2026-10-29T01:01',NULL,NULL);
INSERT INTO tasks VALUES('t_pid','blocked','stale-shape card','x','pibtw:4242',NULL,0,'2026-10-29T01:02',NULL,NULL);"
cat >"$FDHOME/bin/hermes" <<'EOF'
#!/usr/bin/env bash
py=$(command -v python3 || command -v python)
exec "$py" - "$HERMES_STUB_DB" <<'PYEOF'
import sqlite3, sys
# stub: mimic recompute_ready - parentless non-sticky todo/blocked cards
# go ready regardless of claim_lock (proven live behavior).
con = sqlite3.connect(sys.argv[1], timeout=10)
con.execute("UPDATE tasks SET status='ready' WHERE status IN ('todo','blocked');")
con.commit()
print("Spawned: 0")
PYEOF
EOF
chmod +x "$FDHOME/bin/hermes"
out=$(HOME="$FDHOME" HERMES_HOME= HERMES_KANBAN_HOME="$FDHOME_N" HERMES_KANBAN_DB= PATH="$FDHOME/bin:$PATH" HERMES_STUB_DB="$FDB_N" bash "$GUARD" --dry-run 2>&1)
rc=$?
[ "$rc" -eq 0 ] && ok || bad "T11: dry-run exited $rc (out: $out)"
s=$(sql "$FDB_N" "SELECT status FROM tasks WHERE id='t_hold';")
[ "$s" = "blocked" ] && ok || bad "T11: dry-run flipped the parked hold (t_hold status=$s)"
out=$(HOME="$FDHOME" HERMES_HOME= HERMES_KANBAN_HOME="$FDHOME_N" HERMES_KANBAN_DB= PATH="$FDHOME/bin:$PATH" HERMES_STUB_DB="$FDB_N" bash "$GUARD" 2>&1)
rc=$?
[ "$rc" -eq 0 ] && ok || bad "T11: dispatch pass exited $rc (out: $out)"
printf "%s" "$out" | grep -q "re-parked: t_hold (ready -> blocked, named hold lifted by the dispatch sweep)" \
	&& ok || bad "T11: t_hold not re-parked (out: $out)"
sh2=$(sql "$FDB_N" "SELECT id||' '||status||' '||claim_lock FROM tasks WHERE id IN ('t_hold','t_hold2') ORDER BY id;")
[ "$sh2" = "t_hold blocked pibtw:v0.14.0-hold
t_hold2 blocked pibtw:design-hold" ] \
	&& ok || bad "T11: parked shape wrong after pass (got: $sh2)"
s=$(sql "$FDB_N" "SELECT status FROM tasks WHERE id='t_pid';")
[ "$s" = "ready" ] && ok || bad "T11: pid-shaped lock was re-parked ($s) - restore overreached"
rm -rf "$FDHOME"

# T12 (local + CI): guard bug #12 - the cap is the LIVE config knob
# kanban.max_in_progress_per_profile, not a hardcode (CAP=4 drifted from
# the law: the config said 2, the guard could spawn past the sanctioned
# concurrency). Pinned here: sandbox home with the key -> CAP reads it;
# key absent or file absent -> fallback 2; --max overrides the config.
# The config is read from the resolved ROOT (<hermes-home>/config.yaml -
# the LIVE config the tick reads), never a $HOME/.hermes hardcode.
CFHOME=$(mktemp -d); CFHOME_N=$(mixpath "$CFHOME")
mkdir -p "$CFHOME/kanban/boards/project-omega" "$CFHOME/bin"
printf 'project-omega\n' >"$CFHOME/kanban/current"
CFB_N="$CFHOME_N/kanban/boards/project-omega/kanban.db"
sqlex "$CFB_N" "CREATE TABLE tasks(id TEXT, status TEXT, title TEXT DEFAULT '', body TEXT DEFAULT '',
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT,
  worker_pid INTEGER, last_heartbeat_at INTEGER);"
out=$(HOME="$CFHOME" HERMES_HOME= HERMES_KANBAN_HOME="$CFHOME_N" HERMES_KANBAN_DB= PATH="$CFHOME/bin:$PATH" bash "$GUARD" --dry-run 2>&1)
printf "%s" "$out" | grep -q "cap=2" && ok || bad "T12: no config file -> cap must fall back to 2 (out: $out)"
printf 'kanban:\n  max_in_progress_per_profile: 4\n' >"$CFHOME/config.yaml"
out=$(HOME="$CFHOME" HERMES_HOME= HERMES_KANBAN_HOME="$CFHOME_N" HERMES_KANBAN_DB= PATH="$CFHOME/bin:$PATH" bash "$GUARD" --dry-run 2>&1)
printf "%s" "$out" | grep -q "cap=4" && ok || bad "T12: config knob not read (out: $out)"
out=$(HOME="$CFHOME" HERMES_HOME= HERMES_KANBAN_HOME="$CFHOME_N" HERMES_KANBAN_DB= PATH="$CFHOME/bin:$PATH" bash "$GUARD" --dry-run --max 1 2>&1)
printf "%s" "$out" | grep -q "cap=1" && ok || bad "T12: --max must override the config cap (out: $out)"
rm -rf "$CFHOME"

# T13 (local + CI): guard bug #13 (proven live 2026-10-31) - the pre-pass
# snapshot recorded a drifted hold VERBATIM. A card that reached pass
# start ALREADY drifted (status='ready' + named hold, promoted by a
# recompute_ready OUTSIDE the guard: crashed tick, bare kanban CLI call)
# was snapshotted as ready; the restore saw ready->ready = no drift, and
# the park stayed laundered across every later pass (proven live:
# promote events kept firing on t_55a3fc4b/t_95c91f9c after bug #11's
# fix). Pinned here: BOTH drifting shapes (v0.14.0-style hold and
# design-hold) at pass start in READY -> the snapshot parks them and
# names the normalization; the stub recompute_ready flips them again;
# the restore re-parks; final shape blocked + locks intact.
FDHOME=$(mktemp -d); FDHOME_N=$(mixpath "$FDHOME")
mkdir -p "$FDHOME/kanban/boards/project-omega" "$FDHOME/bin"
printf 'project-omega\n' >"$FDHOME/kanban/current"
DRB_N="$FDHOME_N/kanban/boards/project-omega/kanban.db"
sqlex "$DRB_N" "CREATE TABLE tasks(id TEXT, status TEXT, title TEXT DEFAULT '', body TEXT DEFAULT '',
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT,
  worker_pid INTEGER, last_heartbeat_at INTEGER);
INSERT INTO tasks VALUES('t_drun','running','go bits','pure go bits','pibtw:1',NULL,0,'2026-10-31T01:00',NULL,NULL);
INSERT INTO tasks VALUES('t_dready','ready','legal filler','filler','' ,NULL,0,'2026-10-30T00:30',NULL,NULL);
INSERT INTO tasks VALUES('t_drift14','ready','terminal module hold','design gate','pibtw:v0.14.0-hold',NULL,0,'2026-10-30T01:00',NULL,NULL);
INSERT INTO tasks VALUES('t_driftde','ready','browser module hold','design gate','pibtw:design-hold',NULL,0,'2026-10-30T01:01',NULL,NULL);"
cat >"$FDHOME/bin/hermes" <<'EOF'
#!/usr/bin/env bash
py=$(command -v python3 || command -v python)
exec "$py" - "$HERMES_STUB_DB" <<'PYEOF'
import sqlite3, sys
# stub: mimic recompute_ready - parentless non-sticky todo/blocked cards
# go ready regardless of claim_lock (proven live behavior).
con = sqlite3.connect(sys.argv[1], timeout=10)
con.execute("UPDATE tasks SET status='ready' WHERE status IN ('todo','blocked');")
con.commit()
print("Spawned: 0")
PYEOF
EOF
chmod +x "$FDHOME/bin/hermes"
out=$(HOME="$FDHOME" HERMES_HOME= HERMES_KANBAN_HOME="$FDHOME_N" HERMES_KANBAN_DB= PATH="$FDHOME/bin:$PATH" HERMES_STUB_DB="$DRB_N" bash "$GUARD" --dry-run 2>&1)
rc=$?
[ "$rc" -eq 0 ] && ok || bad "T13: dry-run exited $rc (out: $out)"
s=$(sql "$DRB_N" "SELECT count(*) FROM tasks WHERE status='ready' AND coalesce(claim_lock,'') != '';")
[ "$s" = "2" ] && ok || bad "T13: dry-run flipped the drifted holds (ready-with-hold count: $s)"
out=$(HOME="$FDHOME" HERMES_HOME= HERMES_KANBAN_HOME="$FDHOME_N" HERMES_KANBAN_DB= PATH="$FDHOME/bin:$PATH" HERMES_STUB_DB="$DRB_N" bash "$GUARD" 2>&1)
rc=$?
[ "$rc" -eq 0 ] && ok || bad "T13: dispatch pass exited $rc (out: $out)"
printf "%s" "$out" | grep -q "hold-drift normalized: t_drift14 (ready -> blocked" \
	&& ok || bad "T13: v0.14.0 drift not normalized at snapshot (out: $out)"
printf "%s" "$out" | grep -q "hold-drift normalized: t_driftde (ready -> blocked" \
	&& ok || bad "T13: design-hold drift not normalized at snapshot (out: $out)"
printf "%s" "$out" | grep -q "re-parked: t_drift14 (ready -> blocked" \
	&& ok || bad "T13: re-promoted hold not re-parked by the restore (out: $out)"
sh13=$(sql "$DRB_N" "SELECT id||'|'||status||'|'||claim_lock FROM tasks WHERE id LIKE 't_drift%' ORDER BY id;")
[ "$sh13" = "t_drift14|blocked|pibtw:v0.14.0-hold
t_driftde|blocked|pibtw:design-hold" ] \
	&& ok || bad "T13: parked shape wrong after pass (got: $sh13)"
rm -rf "$FDHOME"

# T14 (local + CI): strict arg validation - unknown flags, malformed
# --max values and bare --help must die BEFORE any state is touched.
# Proven live 2026-11-02: `factory-dispatch.sh --help` printed the
# verdicts and then EXECUTED a real dispatch (Spawned: 1, run 871) - the
# parse loop had no default-reject branch. Pinned here: every refusal
# exits 2 (help: 0), usage text prints, and ZERO dispatch side effects
# occur: the stub hermes logs every invocation and the fake board keeps
# its exact status/lock shape. Legal invocations (bare pass, --max N,
# dry-run) stay covered by T9/T10/T12.
FDHOME=$(mktemp -d); FDHOME_N=$(mixpath "$FDHOME")
mkdir -p "$FDHOME/kanban/boards/project-omega" "$FDHOME/bin"
printf 'project-omega\n' >"$FDHOME/kanban/current"
AVB_N="$FDHOME_N/kanban/boards/project-omega/kanban.db"
sqlex "$AVB_N" "CREATE TABLE tasks(id TEXT, status TEXT, title TEXT DEFAULT '', body TEXT DEFAULT '',
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT,
  worker_pid INTEGER, last_heartbeat_at INTEGER);
INSERT INTO tasks VALUES('t_cand','ready','webui fix','FILES: engine/webui/',NULL,0,0,'2026-11-01T01:00',NULL,NULL);"
cat >"$FDHOME/bin/hermes" <<'EOF'
#!/usr/bin/env bash
echo "[STUB CALLED] $*" >>"${HERMES_STUB_LOG:?}"   # ANY invocation = side effect
echo "Spawned: 1"
EOF
chmod +x "$FDHOME/bin/hermes"
SNAP0=$(sql "$AVB_N" "SELECT id||' '||status||' '||coalesce(claim_lock,'') FROM tasks;")
stub_log="$FDHOME/stub-calls.log"; : >"$stub_log"
no_side_effect() {  # rc, stub log, board shape all match refusal law
  local rc="$1" want="$2" msg="$3" log rcnt
  log=$(cat "$stub_log" 2>/dev/null || echo MISSING)
  rcnt=$(sql "$AVB_N" "SELECT count(*) FROM tasks WHERE status='running';")
  s=$(sql "$AVB_N" "SELECT id||' '||status||' '||coalesce(claim_lock,'') FROM tasks;")
  [ "$s" = "$SNAP0" ] || bad "$msg: board shape drifted ('$s' != '$SNAP0')"
  [ "$rcnt" = "0" ] || bad "$msg: a card entered running (spawn side effect)"
  [ "$log" = "" ] || bad "$msg: stub hermes invoked ('${log:-MISSING}')"
  [ "$rc" = "$want" ] || bad "$msg: exit $rc (expected $want)"
}
out=$(HOME="$FDHOME" HERMES_HOME= HERMES_KANBAN_HOME="$FDHOME_N" HERMES_KANBAN_DB= PATH="$FDHOME/bin:$PATH" HERMES_STUB_LOG="$stub_log" bash "$GUARD" --help 2>&1); rc=$?
[ "$rc" = "0" ] || bad "T14: --help exited $rc (expected 0)"
printf "%s" "$out" | grep -q "collision-aware dispatch" || bad "T14: --help printed no usage (out: $out)"
no_side_effect "$rc" 0 "T14 --help"
out=$(HOME="$FDHOME" HERMES_HOME= HERMES_KANBAN_HOME="$FDHOME_N" HERMES_KANBAN_DB= PATH="$FDHOME/bin:$PATH" HERMES_STUB_LOG="$stub_log" bash "$GUARD" -h 2>&1); rc=$?
[ "$rc" = "0" ] || bad "T14: -h exited $rc (expected 0)"
no_side_effect "$rc" 0 "T14 -h"
out=$(HOME="$FDHOME" HERMES_HOME= HERMES_KANBAN_HOME="$FDHOME_N" HERMES_KANBAN_DB= PATH="$FDHOME/bin:$PATH" HERMES_STUB_LOG="$stub_log" bash "$GUARD" --typo 2>&1); rc=$?
printf "%s" "$out" | grep -q "unknown argument: --typo" || bad "T14: --typo message wrong (out: $out)"
printf "%s" "$out" | grep -q "collision-aware dispatch" || bad "T14: --typo printed no usage"
no_side_effect "$rc" 2 "T14 --typo"
out=$(HOME="$FDHOME" HERMES_HOME= HERMES_KANBAN_HOME="$FDHOME_N" HERMES_KANBAN_DB= PATH="$FDHOME/bin:$PATH" HERMES_STUB_LOG="$stub_log" bash "$GUARD" --max 2>&1); rc=$?
printf "%s" "$out" | grep -q -- "--max needs a value" || bad "T14: valueless --max message wrong (out: $out)"
no_side_effect "$rc" 2 "T14 --max missing value"
out=$(HOME="$FDHOME" HERMES_HOME= HERMES_KANBAN_HOME="$FDHOME_N" HERMES_KANBAN_DB= PATH="$FDHOME/bin:$PATH" HERMES_STUB_LOG="$stub_log" bash "$GUARD" --max abc 2>&1); rc=$?
printf "%s" "$out" | grep -q -- "--max must be a positive integer" || bad "T14: non-integer --max message wrong (out: $out)"
no_side_effect "$rc" 2 "T14 --max abc"
out=$(HOME="$FDHOME" HERMES_HOME= HERMES_KANBAN_HOME="$FDHOME_N" HERMES_KANBAN_DB= PATH="$FDHOME/bin:$PATH" HERMES_STUB_LOG="$stub_log" bash "$GUARD" --max 0 2>&1); rc=$?
no_side_effect "$rc" 2 "T14 --max 0"
rm -rf "$FDHOME"

# T15 (local + CI): home resolution + loud refusal (2026-11-05 card). A
# POSIX-style home WITHOUT a kanban dir plus a LOCALAPPDATA-style home WITH
# a populated project-omega board: the guard must pick the populated one
# (the CLI's platform-default chain - env vars first, then LOCALAPPDATA,
# then POSIX $HOME), resolve the named board through <root>/kanban/current,
# and resolve a profile-shaped HERMES_HOME (<root>/profiles/<name>) to its
# ROOT. When NEITHER home carries a board, the refusal is LOUD (exit 3) -
# the old bug printed verdicts computed from EMPTY lists with exit 0.
FDHOME=$(mktemp -d); FDHOME_N=$(mixpath "$FDHOME")
mkdir -p "$FDHOME/posixhome/.hermes"          # POSIX-style home: no kanban inside
mkdir -p "$FDHOME/app/hermes/kanban/boards/project-omega"
mkdir -p "$FDHOME/empty"                      # LOCALAPPDATA-style home: no hermes dir
mkdir -p "$FDHOME/rooth/hermes/kanban/boards/project-omega" \
         "$FDHOME/rooth/hermes/profiles/w1"   # a profile-shaped HERMES_HOME
ADB_N="$FDHOME_N/app/hermes/kanban/boards/project-omega/kanban.db"
sqlex "$ADB_N" "CREATE TABLE tasks(id TEXT, status TEXT, title TEXT DEFAULT '', body TEXT DEFAULT '',
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT,
  worker_pid INTEGER, last_heartbeat_at INTEGER);
INSERT INTO tasks VALUES('t_pop','ready','populated candidate','FILES: engine/webui/',NULL,0,0,'2026-11-05T01:00',NULL,NULL);"
printf 'project-omega\n' >"$FDHOME/app/hermes/kanban/current"
PDB_N="$FDHOME_N/rooth/hermes/kanban/boards/project-omega/kanban.db"
sqlex "$PDB_N" "CREATE TABLE tasks(id TEXT, status TEXT, title TEXT DEFAULT '', body TEXT DEFAULT '',
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT,
  worker_pid INTEGER, last_heartbeat_at INTEGER);
INSERT INTO tasks VALUES('t_prof','ready','profile-root candidate','FILES: engine/webui/',NULL,0,0,'2026-11-05T01:00',NULL,NULL);"
printf 'project-omega\n' >"$FDHOME/rooth/hermes/kanban/current"

# T15a: POSIX home empty + LOCALAPPDATA home populated -> picks the populated one
out=$(HOME="$FDHOME/posixhome" LOCALAPPDATA="$FDHOME_N/app" HERMES_HOME= HERMES_KANBAN_HOME= HERMES_KANBAN_DB= HERMES_KANBAN_BOARD= bash "$GUARD" --dry-run 2>&1)
rc=$?
[ "$rc" -eq 0 ] && ok || bad "T15a: dry-run on the populated LOCALAPPDATA-style home exited $rc (out: $out)"
printf "%s" "$out" | grep -q "legal: t_pop" && ok || bad "T15a: populated board not picked (out: $out)"
printf "%s" "$out" | grep -q "command not found" && bad "T15a: shell-level command failure leaked (out: $out)" || ok

# T15b: NEITHER home carries a board -> loud refusal, never empty-list verdicts
out=$(HOME="$FDHOME/posixhome" LOCALAPPDATA="$FDHOME_N/empty" HERMES_HOME= HERMES_KANBAN_HOME= HERMES_KANBAN_DB= HERMES_KANBAN_BOARD= bash "$GUARD" --dry-run 2>&1)
rc=$?
[ "$rc" -eq 3 ] && ok || bad "T15b: missing board DB refused with exit $rc (expected 3)"
printf "%s" "$out" | grep -q "board DB not found" && ok || bad "T15b: refusal was not loud (out: $out)"
printf "%s" "$out" | grep -qE "^(hold|legal):" && bad "T15b: verdicts printed from EMPTY lists (out: $out)" || ok

# T15c: a profile-shaped HERMES_HOME (<root>/profiles/<name>) names its ROOT
out=$(HOME="$FDHOME/posixhome" LOCALAPPDATA="$FDHOME_N/app" HERMES_HOME="$FDHOME_N/rooth/hermes/profiles/w1" HERMES_KANBAN_HOME= HERMES_KANBAN_DB= HERMES_KANBAN_BOARD= bash "$GUARD" --dry-run 2>&1)
rc=$?
[ "$rc" -eq 0 ] && ok || bad "T15c: profile-shaped HERMES_HOME dry-run exited $rc (out: $out)"
printf "%s" "$out" | grep -q "legal: t_prof" && ok || bad "T15c: profile-shaped HERMES_HOME did not resolve to its ROOT board (out: $out)"
rm -rf "$FDHOME"

# T16 (local + CI): the PAIR VERDICT - the dry-run states the pairing law for
# the would-spawn head set (first SLOTS ready/unclaimed cards in dispatch
# order, SLOTS = cap - running). The live miss this pins (tick 2026-11-07
# 15:36): running=0, TWO go head cards, `--max 2` - every candidate read
# legal against the empty running set, the pass spawned a Go+Go pair the law
# forbids. Per-candidate verdicts can never answer the --max question; the
# pair line must. Shapes pinned: forbidden go+go at running=0 (the exact
# miss, cap-derived and --max paths), sanctioned go+webui and webui+webui
# with disjoint FILES, forbidden same-file, single candidate, empty board,
# and the class-list containment law over a multi-element running class list
# (docs,go,webui) - the go candidate must hold even though the running go
# card exposes NO surface tokens (the unconditional class leg).
PAIRRUN() {  # $1 = seed SQL, rest = extra guard args; prints the dry-run
  local seed="$1" home db out
  shift
  home=$(mktemp -d)
  mkdir -p "$home/kanban/boards/project-omega"
  printf 'project-omega\n' >"$home/kanban/current"
  db="$(mixpath "$home")/kanban/boards/project-omega/kanban.db"
  sqlex "$db" "CREATE TABLE tasks(id TEXT, status TEXT, title TEXT DEFAULT '', body TEXT DEFAULT '',
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT,
  worker_pid INTEGER, last_heartbeat_at INTEGER);
$seed"
  out=$(HOME="$home" HERMES_HOME= HERMES_KANBAN_HOME="$(mixpath "$home")" HERMES_KANBAN_DB= HERMES_KANBAN_BOARD= \
    bash "$GUARD" --dry-run "$@" 2>&1)
  rm -rf "$home"
  printf '%s' "$out"
}
GO1="INSERT INTO tasks VALUES('t_go1','ready','go one','FILES: scripts/factory-dispatch.sh',NULL,NULL,5,'2026-11-07T15:00',NULL,NULL);"
GO2="INSERT INTO tasks VALUES('t_go2','ready','go two','FILES: harness/cron/runner.go',NULL,NULL,5,'2026-11-07T15:01',NULL,NULL);"
WEB="INSERT INTO tasks VALUES('t_web','ready','webui one','FILES: engine/webui/app/pages/x.vue',NULL,NULL,5,'2026-11-07T15:02',NULL,NULL);"

# T16a: running=0, go+go at the head, cap 2 - SANCTIONED since the 2026-11-10
# ruling when the surfaces are provably disjoint (go1 and go2 name unrelated
# files, so both go legal and the pair is sanctioned).
out=$(PAIRRUN "$GO1
$GO2")
printf "%s" "$out" | grep -qF 'pair: sanctioned (t_go1 t_go2, classes go+go, FILES disjoint)' \
	&& ok || bad "T16a: disjoint go+go head not sanctioned (out: $out)"
printf "%s" "$out" | grep -q "^legal: t_go2" \
	&& ok || bad "T16a: disjoint go+go - t_go2 should be legal (out: $out)"
printf "%s" "$out" | grep -q "^legal: t_go1" \
	&& ok || bad "T16a: t_go1 should stay legal (out: $out)"

# T16a2: go+go sharing a file - the same-pass leg HOLDS the second card, so the
# pair line (legal head only) reports the single card that will actually spawn.
# The class law itself is asserted above by T16a; here the hold is the proof.
out=$(PAIRRUN "$GO1
INSERT INTO tasks VALUES('t_go3','ready','go three','FILES: scripts/factory-dispatch.sh',NULL,NULL,5,'2026-11-07T15:01',NULL,NULL);")
printf "%s" "$out" | grep -q "^hold: t_go3" \
	&& ok || bad "T16a2: the same-pass leg did not hold t_go3 (out: $out)"
printf "%s" "$out" | grep -qF 'pair: sanctioned (t_go1, single would-spawn candidate)' \
	&& ok || bad "T16a2: held card leaked into the pair verdict (out: $out)"

# T16a3: a HELD candidate must not poison the pair verdict. Dispatch order puts
# t_go1 (legal) then t_go3 (held, collides with t_go1) then t_web (legal): two
# slots are free and both legal cards can spawn, so the pair line must name
# them, not the card the pass already held. Live shape 2026-11-10: the old raw
# head read `forbidden` here and the tick dispatched ONE worker instead of two.
out=$(PAIRRUN "$GO1
INSERT INTO tasks VALUES('t_go3','ready','go three','FILES: scripts/factory-dispatch.sh',NULL,NULL,5,'2026-11-07T15:01',NULL,NULL);
$WEB")
printf "%s" "$out" | grep -qF 'pair: sanctioned (t_go1 t_web, classes go+webui, FILES disjoint)' \
	&& ok || bad "T16a3: held candidate poisoned the legal pair (out: $out)"
printf "%s" "$out" | grep -q "held this pass" \
	&& bad "T16a3: held-this-pass caveat must be dead (out: $out)" || ok

# T16b: sanctioned go+webui (the only sanctioned mixed pair), disjoint FILES.
out=$(PAIRRUN "$GO1
$WEB")
printf "%s" "$out" | grep -qF 'pair: sanctioned (t_go1 t_web, classes go+webui, FILES disjoint)' \
	&& ok || bad "T16b: go+webui head not sanctioned (out: $out)"

# T16c: sanctioned webui+webui, disjoint FILES (class must not forbid it).
out=$(PAIRRUN "INSERT INTO tasks VALUES('t_w1','ready','webui one','FILES: engine/webui/app/pages/a.vue',NULL,NULL,5,'2026-11-07T15:00',NULL,NULL);
INSERT INTO tasks VALUES('t_w2','ready','webui two','FILES: engine/webui/test/b.test.ts',NULL,NULL,5,'2026-11-07T15:01',NULL,NULL);")
printf "%s" "$out" | grep -qF 'pair: sanctioned (t_w1 t_w2, classes webui+webui, FILES disjoint)' \
	&& ok || bad "T16c: webui+webui with disjoint FILES not sanctioned (out: $out)"

# T16d: same-file collision - two webui candidates naming the same file. The
# per-candidate leg holds t_d2, so only t_d1 can spawn and the pair line (legal
# head) names one card. The collision law is proven by the hold itself.
out=$(PAIRRUN "INSERT INTO tasks VALUES('t_d1','ready','webui one','FILES: engine/webui/app/components/dock.vue',NULL,NULL,5,'2026-11-07T15:00',NULL,NULL);
INSERT INTO tasks VALUES('t_d2','ready','webui two','FILES: engine/webui/app/components/dock.vue',NULL,NULL,5,'2026-11-07T15:01',NULL,NULL);")
printf "%s" "$out" | grep -q "^hold: t_d2  (same-file: webui/app/components/dock.vue" \
	&& ok || bad "T16d: same-file collision did not hold t_d2 (out: $out)"
printf "%s" "$out" | grep -qF 'pair: sanctioned (t_d1, single would-spawn candidate)' \
	&& ok || bad "T16d: held card leaked into the pair verdict (out: $out)"

# T16e: a single head slot forms no pair - sanctioned, and it names the card.
out=$(PAIRRUN "$GO1")
printf "%s" "$out" | grep -qF 'pair: sanctioned (t_go1, single would-spawn candidate)' \
	&& ok || bad "T16e: single-candidate head not sanctioned (out: $out)"

# T16f: --max 1 -> SLOTS 1 -> one head slot, so the same two go cards are not
# a pair this pass (the --max decision is what the line exists to inform).
out=$(PAIRRUN "$GO1
$GO2" --max 1)
printf "%s" "$out" | grep -qF 'pair: sanctioned (t_go1, single would-spawn candidate)' \
	&& ok || bad "T16f: --max 1 head must be a single slot (out: $out)"
printf "%s" "$out" | grep -q "cap=1" && ok || bad "T16f: --max did not set the cap (out: $out)"

# T16g: --max 2 on the SAME board reaches two slots - the explicit path must
# run the same slot math as the cap path. Disjoint go+go is sanctioned since
# the 2026-11-10 ruling, so the verdict is sanctioned on BOTH paths.
out=$(PAIRRUN "$GO1
$GO2" --max 2)
printf "%s" "$out" | grep -qF 'pair: sanctioned (t_go1 t_go2, classes go+go, FILES disjoint)' \
	&& ok || bad "T16g: --max 2 disjoint go+go head not sanctioned (out: $out)"

# T16h: empty board - the line still prints, never a silent absence (an
# empty verdict section must be distinguishable from a blind computation).
out=$(PAIRRUN "SELECT 1;")
printf "%s" "$out" | grep -qF 'pair: sanctioned (none, empty would-spawn set)' \
	&& ok || bad "T16h: empty board printed no pair verdict (out: $out)"

# T16i: class-list containment + the unconditional class leg. Three running
# cards (docs, go, webui) make RC='docs,go,webui,': the go candidate must
# hold even though the running go card exposes NO surface tokens (the gate
# that read it legal live, tick 2026-11-07 18:36). SLOTS = 2 - 3 < 0 -> 0.
out=$(PAIRRUN "INSERT INTO tasks VALUES('t_rdoc','running','doc bits','FILES: docs/cli.md','pibtw:1',NULL,5,'2026-11-07T14:00',NULL,NULL);
INSERT INTO tasks VALUES('t_rgo','running','go bits','pure go bits','pibtw:2',NULL,5,'2026-11-07T14:01',NULL,NULL);
INSERT INTO tasks VALUES('t_rweb','running','webui bits','FILES: engine/webui/app/','pibtw:3',NULL,5,'2026-11-07T14:02',NULL,NULL);
INSERT INTO tasks VALUES('t_cgo','ready','go candidate','FILES: cmd/omega/loop.go',NULL,NULL,5,'2026-11-07T15:00',NULL,NULL);
INSERT INTO tasks VALUES('t_cdoc','ready','doc candidate','FILES: docs/other.md',NULL,NULL,5,'2026-11-07T15:01',NULL,NULL);")
printf "%s" "$out" | grep -qF 'hold: t_cgo  (go-class beside go with unproven surface: running=[docs,go,webui,]' \
	&& ok || bad "T16i: go candidate beside a token-less running go card not held (out: $out)"
printf "%s" "$out" | grep -q "^legal: t_cdoc" \
	&& ok || bad "T16i: doc candidate should stay legal - a FILES: docs/ card must class docs, not go (out: $out)"
printf "%s" "$out" | grep -qF 'pair: sanctioned (none, empty would-spawn set)' \
	&& ok || bad "T16i: running > cap must give zero head slots (out: $out)"

# T17 (local + CI): a card with NO `FILES:` line is UNPROVABLE, not precise
# (card t_2d0d48ac, live 2026-11-11). The old guard read the ABSENCE as proof:
# candidate_tokens() fell back to the whole body prose, imprecise_surface()
# inspected the FILES line only, found nothing bare and answered "precise", so
# a card that declares no write surface at all got the EASIEST pairing -
# t_cfc2aa58 (no FILES line, BLOCKED) was spawned beside the webui card
# t_b5784de3 as `pair: sanctioned ... FILES disjoint` on a surface nobody
# proved. Pinned shapes: (T17a) the incident - a no-FILES card beside a legal
# card reads forbidden and the reason NAMES the missing line, while the
# no-FILES card stays legal on its own (the prose fallback still serves a
# SINGLE dispatch); (T17b) the go+go leg holds the pair and names the card
# that must grow a FILES line; (T17c) the boundary - `FILES:` with no value
# declares nothing either; (T17d) the CONTROL - the same fixture with a real
# FILES line still pairs, so the rule is the absent line, never a blanket
# pair ban (the sanctioned shapes stay green: T16a/T16b/T16c/T16d/T16g).
NF_WEB="INSERT INTO tasks VALUES('t_nofiles','ready','webui tweak','the fix lands in webui/app/pages/y.vue and its test',NULL,NULL,5,'2026-11-07T14:00',NULL,NULL);"
NF_GO="INSERT INTO tasks VALUES('t_nfgo','ready','cron wake entry','the cron runner in harness/cron/runner.go drops its wake entry',NULL,NULL,5,'2026-11-07T14:00',NULL,NULL);"

# T17a: no FILES line beside a legal webui card -> forbidden pair, named reason.
out=$(PAIRRUN "$NF_WEB
$WEB")
printf "%s" "$out" | grep -q "^legal: t_nofiles" \
	&& ok || bad "T17a: a no-FILES card must stay legal alone - the prose fallback serves single dispatch, only the pair is forbidden (out: $out)"
printf "%s" "$out" | grep -qF 'pair: forbidden (t_nofiles t_web, no FILES: line on t_nofiles - unprovable surface, never a pair)' \
	&& ok || bad "T17a: no FILES line is unprovable, not precise - the pair was not forbidden / reason did not name the missing line (out: $out)"

# T17b: the go+go leg - the no-FILES go card makes the pair unprovable, and the
# hold names it (same-pass leg: t_nfgo went legal first, t_gofl follows it).
out=$(PAIRRUN "$NF_GO
INSERT INTO tasks VALUES('t_gofl','ready','memory store','FILES: harness/memory/store.go',NULL,NULL,5,'2026-11-11T05:01',NULL,NULL);")
printf "%s" "$out" | grep -q "^legal: t_nfgo" \
	&& ok || bad "T17b: no-FILES go card should stay legal alone (out: $out)"
printf "%s" "$out" | grep -q "^hold: t_gofl  (go-class beside go with unproven surface" \
	&& ok || bad "T17b: no FILES line is unprovable, not precise - the go pair was not held (out: $out)"
printf "%s" "$out" | grep -qF 'absent FILES: line is unprovable, never precise: same-pass t_nfgo' \
	&& ok || bad "T17b: the hold reason did not name the FILES-less card (out: $out)"

# T17c: the boundary - a `FILES:` line with NO value declares no surface either.
out=$(PAIRRUN "INSERT INTO tasks VALUES('t_empty','ready','empty surface','FILES:',NULL,NULL,5,'2026-11-07T14:00',NULL,NULL);
$WEB")
printf "%s" "$out" | grep -qF 'pair: forbidden (t_empty t_web, no FILES: line on t_empty - unprovable surface, never a pair)' \
	&& ok || bad "T17c: an EMPTY FILES line must read absent, not precise (out: $out)"

# T17d: CONTROL - the same two-card fixture with a real FILES line still pairs.
out=$(PAIRRUN "INSERT INTO tasks VALUES('t_precise','ready','webui tweak','FILES: webui/app/pages/y.vue',NULL,NULL,5,'2026-11-07T14:00',NULL,NULL);
$WEB")
printf "%s" "$out" | grep -qF 'pair: sanctioned (t_precise t_web, classes webui+webui, FILES disjoint)' \
	&& ok || bad "T17d: a declared FILES surface must still pair - the rule is the ABSENT line, not a pair ban (out: $out)"

# T18 (local + CI): the PROSE legs of class_of() match on WORD BOUNDARIES (card
# t_64255dca, tagged during the t_2d0d48ac review). The webui leg grepped
# "webui\|webui\|nuxt\|spa" - a bare `spa` alternative is a SUBSTRING, and it
# matches "di+spa+tch" and "space". This factory's card prose says "dispatch"
# constantly, so ANY FILES-less card mentioning a dispatch classed webui
# whatever its real surface (that is why the t_2d0d48ac incident pair reported
# `classes webui+webui`). Class feeds the go+go hold legs, so the misclass let
# a go-shaped FILES-less candidate skip the class-leg hold beside a running go
# card. Pinned shapes: (T18a) the dispatch fixture classes go, "space" classes
# go, and the TRUE hits still class webui (engine/webui prose, nuxt.config.ts,
# the word SPA) while docs prose stays docs - no over-tightening; (T18b) the
# harm itself - a FILES-less dispatch card beside a RUNNING go card must HOLD
# (pre-change the same fixture read `legal: t_dispatch`, the class leg skipped);
# (T18c) the PRE-change prose body pinned verbatim still answers webui for that
# very row, so the fixture is the exact red this fix removes rather than a
# shape that happens to pass.
T18DB=$(mktemp); T18DB_N=$(mixpath "$T18DB")
sqlex "$T18DB_N" "CREATE TABLE tasks(id TEXT, status TEXT, title TEXT DEFAULT '', body TEXT DEFAULT '',
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT,
  worker_pid INTEGER, last_heartbeat_at INTEGER);
INSERT INTO tasks VALUES('t_dispatch','ready','dispatch guard sweep','the dispatch pass prints verdicts for the oldest ready card','',NULL,5,'2026-11-11T05:00',NULL,NULL);
INSERT INTO tasks VALUES('t_space','ready','free space check','disk space on the pi host','',NULL,5,'2026-11-11T05:01',NULL,NULL);
INSERT INTO tasks VALUES('t_webprose','ready','dock tweak','the engine/webui dock pane and its nuxt route','',NULL,5,'2026-11-11T05:02',NULL,NULL);
INSERT INTO tasks VALUES('t_spaprose','ready','spa tweak','the spa dock pane','',NULL,5,'2026-11-11T05:03',NULL,NULL);
INSERT INTO tasks VALUES('t_docsprose','ready','doc pass','documentation tidy-up in docs/cli.md','',NULL,5,'2026-11-11T05:04',NULL,NULL);"
# claim_lock '' rather than NULL: class_of() reads the row, claim state is
# irrelevant here, and the dry-run shapes below use their own NULL fixtures.
T18SED="/^DBPY='/,/^'/p; /^dbq()/,/^}/p; /^class_of()/,/^}/p"
out=$(DB="$T18DB_N" PY="$PY" GUARD="$GUARD" SEDEXPR="$T18SED" bash -c '
source <(sed -n "$SEDEXPR" "$GUARD")
# the PRE-change prose body, pinned verbatim: the old legs grepped bare
# substrings and had the doubled "webui\|webui" alternative.
class_of_old() {
  local t
  t=$(dbq "SELECT lower(title||char(32)||body) FROM tasks WHERE id='"'"'$1'"'"';")
  if grep -q "docs/\|documentation" <<<"$t" && ! grep -q "webui" <<<"$t"; then echo docs
  elif grep -q "webui\|webui\|nuxt\|spa" <<<"$t"; then echo webui
  else echo go; fi
}
for id in t_dispatch t_space t_webprose t_spaprose t_docsprose; do
  printf "new %s=%s\n" "$id" "$(class_of "$id")"
done
printf "old t_dispatch=%s\n" "$(class_of_old t_dispatch)"
printf "old t_space=%s\n" "$(class_of_old t_space)"
' 2>&1)
printf "%s" "$out" | grep -qx "new t_dispatch=go" \
	&& ok || bad "T18a: a FILES-less card whose prose says 'dispatch' must class go, not webui (out: $out)"
printf "%s" "$out" | grep -qx "new t_space=go" \
	&& ok || bad "T18a: 'space' must not class webui either - bare spa is a substring (out: $out)"
printf "%s" "$out" | grep -qx "new t_webprose=webui" \
	&& ok || bad "T18a: TRUE webui prose (engine/webui + nuxt) must still class webui (out: $out)"
printf "%s" "$out" | grep -qx "new t_spaprose=webui" \
	&& ok || bad "T18a: the word SPA must still class webui - word boundaries, not a removal (out: $out)"
printf "%s" "$out" | grep -qx "new t_docsprose=docs" \
	&& ok || bad "T18a: docs prose must still class docs (out: $out)"
printf "%s" "$out" | grep -qx "old t_dispatch=webui" \
	&& ok || bad "T18c: the pinned PRE-change prose body no longer reds on the fixture - the shape is not the bug (out: $out)"
printf "%s" "$out" | grep -qx "old t_space=webui" \
	&& ok || bad "T18c: the pinned PRE-change body must still misclass 'space' (out: $out)"
rm -f "$T18DB"

# T18b: the harm leg - a FILES-less 'dispatch' candidate beside a RUNNING go
# card. Pre-change the misclass made it webui, so the go-class hold never fired
# and the card read `legal: t_dispatch` (probe receipt on the card). Post-change
# it classes go and holds on the unproven surface, naming itself.
out=$(PAIRRUN "INSERT INTO tasks VALUES('t_rgo','running','cron bits','FILES: harness/cron/runner.go','pibtw:1',NULL,5,'2026-11-11T04:00',NULL,NULL);
INSERT INTO tasks VALUES('t_dispatch','ready','dispatch guard sweep','the dispatcher picks the oldest ready card; this card declares no files line',NULL,NULL,5,'2026-11-11T05:00',NULL,NULL);")
printf "%s" "$out" | grep -q "^hold: t_dispatch  (go-class beside go with unproven surface" \
	&& ok || bad "T18b: a FILES-less dispatch card beside a running go card must class go and hold - not skip the class leg as webui (out: $out)"
printf "%s" "$out" | grep -qF "[absent FILES: line is unprovable, never precise: candidate t_dispatch]" \
	&& ok || bad "T18b: the hold reason must name the FILES-less candidate (out: $out)"
printf "%s" "$out" | grep -q "^legal: t_dispatch" \
	&& bad "T18b: the candidate leaked as legal - the webui misclass is still in the prose leg (out: $out)" || ok

# T19 pins the CLAMP (card t_8a1a46e1): a LIVE `--max N` (N>1) must OBEY the
# pair verdict the dry-run prints, not assume the caller read it. On 2026-11-11
# a tick batched the dry-run and the real dispatch into one terminal call, so
# `pair: forbidden` printed and the real `--max 2` spawned both cards anyway
# (surfaces happened to be disjoint - luck, not law). Shapes pinned: (T19a) a
# forbidden head set with --max 2 hands the inner dispatcher --max 1, spawns
# exactly ONE card, and says so in a loud `clamp:` line naming the ids it
# declined to pair; (T19b) the CONTROL - a sanctioned pair still gets --max 2
# and two spawns; (T19c) a dry-run prints the verdict and NO clamp (the clamp
# never touches dry-run output, and a dry-run never spawns); (T19d) --max 1 is
# already single, so no clamp line (the clamp is for N>1 only).
#
# T19 helper: a LIVE pass against a fake board with a STUB inner dispatcher.
# The stub records the --max it was handed and spawns exactly that many ready
# cards, so the assertions read the real spawn count the pass would produce.
# The stub is found through PATH, and the guard calls bare `hermes` - the
# PATH entry must be POSIX (cygpath -u): a C:/ entry is split at the drive
# colon by MSYS and never resolves, which would silently hand the call to the
# REAL hermes (it refuses in a worker context), reding the log assertion.
# That is a SEPARATE trap from the suite's location sensitivity: a live pass
# also needs the fence stamp at the guard's OWN root (bin/shims/git), which the
# preflight at the top of this file stages for a scratch copy. Both conditions
# hold before any live leg runs, so a `calls: 0` here is a real guard finding.
SPAWN_BASE=$(mktemp -d)
SPAWN_BASE_N=$(mixpath "$SPAWN_BASE")
SPAWN_BASE_U=$(cygpath -u "$SPAWN_BASE" 2>/dev/null || printf '%s' "$SPAWN_BASE")
sphome() { printf '%s' "$SPAWN_BASE/$1"; }
spdb()   { printf '%s' "$SPAWN_BASE_N/$1/kanban/boards/project-omega/kanban.db"; }
splog()  { printf '%s' "$SPAWN_BASE_N/$1/args.log"; }
SPAWNRUN() {  # $1 = case tag (names its board dir), $2 = seed SQL, rest = guard args
  local tag="$1" seed="$2" home hn hu
  shift 2
  home=$(sphome "$tag")
  rm -rf "$home"; mkdir -p "$home/kanban/boards/project-omega" "$home/bin"
  hn=$(mixpath "$home")
  hu="$SPAWN_BASE_U/$tag"
  printf 'project-omega\n' >"$home/kanban/current"
  : >"$(splog "$tag")"
  sqlex "$(spdb "$tag")" "CREATE TABLE tasks(id TEXT, status TEXT, title TEXT DEFAULT '', body TEXT DEFAULT '',
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT,
  worker_pid INTEGER, last_heartbeat_at INTEGER);
$seed"
  cat >"$home/bin/hermes" <<'STUB_EOF'
#!/usr/bin/env bash
py=$(command -v python3 || command -v python)
exec "$py" - "$HERMES_STUB_DB" "$HERMES_STUB_LOG" "$@" <<'PYEOF'
import sqlite3, sys
db, log = sys.argv[1], sys.argv[2]
args = sys.argv[3:]
mx = int(args[args.index("--max") + 1]) if "--max" in args else 0
open(log, "a", newline="").write(" ".join(args) + "\n")  # newline="": no CRLF translation
con = sqlite3.connect(db, timeout=10)
order = "ORDER BY priority DESC, created_at"
rows = [r[0] for r in con.execute(
    "SELECT id FROM tasks WHERE status='ready' AND claim_lock IS NULL " + order)]
# the review lane: dispatch_once claims `status='review' AND claim_lock IS
# NULL` through claim_review_task (review -> running), sharing the same budget
# after the ready lane. Mirrored here so a review-only board's spawn is real.
revs = [r[0] for r in con.execute(
    "SELECT id FROM tasks WHERE status='review' AND claim_lock IS NULL " + order)]
take = rows[:mx]
rev_take = revs[:max(mx - len(take), 0)]
for cid in take:
    con.execute("UPDATE tasks SET status='running', claim_lock='pibtw:9', worker_pid=1 WHERE id=?", (cid,))
for cid in rev_take:
    con.execute("UPDATE tasks SET status='running', claim_lock='pibtw:9', worker_pid=1 WHERE id=?", (cid,))
con.commit()
print("Spawned: %d (%s)" % (len(take) + len(rev_take), " ".join(take + rev_take)))
PYEOF
STUB_EOF
  chmod +x "$home/bin/hermes"
  out=$(HOME="$home" HERMES_HOME= HERMES_KANBAN_HOME="$hn" HERMES_KANBAN_DB= HERMES_KANBAN_BOARD= \
    PATH="$hu/bin:$PATH" HERMES_STUB_DB="$(spdb "$tag")" HERMES_STUB_LOG="$(splog "$tag")" \
    bash "$GUARD" "$@" 2>&1)
  printf '%s' "$out"
}

# T19a: forbidden head set (no-FILES card beside a legal webui card) under a
# LIVE --max 2 -> the clamp fires, ONE spawn, and the ids it declined to pair.
out=$(SPAWNRUN clamp-forbidden "$NF_WEB
$WEB" --max 2)
printf "%s" "$out" | grep -qF 'pair: forbidden (t_nofiles t_web, no FILES: line on t_nofiles - unprovable surface, never a pair)' \
	&& ok || bad "T19a: fixture no longer reads forbidden - the clamp case is not testing the clamp (out: $out)"
printf "%s" "$out" | grep -qF 'clamp: --max 2 -> 1 (pair: forbidden - no FILES: line on t_nofiles - unprovable surface, never a pair) - declined to pair: t_nofiles t_web' \
	&& ok || bad "T19a: a forbidden head pair did not clamp --max 2, or the clamp line lost its reason/ids (out: $out)"
grep -qxF 'kanban dispatch --max 1' "$(splog clamp-forbidden)" \
	&& ok || bad "T19a: the inner dispatcher was not handed --max 1 (got: $(tr '\n' ';' <"$(splog clamp-forbidden)"))"
printf "%s" "$out" | grep -qF 'Spawned: 1 (t_nofiles)' \
	&& ok || bad "T19a: exactly ONE card must spawn under the clamp (out: $out)"
nrun=$(sql "$(spdb clamp-forbidden)" "SELECT COUNT(*) FROM tasks WHERE status='running';")
[ "$nrun" = "1" ] && ok || bad "T19a: $nrun cards running after a clamped pass (expected 1)"

# T19b: CONTROL - the same LIVE path with a SANCTIONED pair (disjoint go+go)
# still passes --max 2 through and spawns two. The clamp must not be a blanket.
out=$(SPAWNRUN clamp-sanctioned "$GO1
$GO2" --max 2)
printf "%s" "$out" | grep -qF 'pair: sanctioned (t_go1 t_go2, classes go+go, FILES disjoint)' \
	&& ok || bad "T19b: control fixture is not a sanctioned pair any more (out: $out)"
printf "%s" "$out" | grep -q "clamp:" \
	&& bad "T19b: a sanctioned pair was clamped - the clamp must obey the verdict, not ban pairs (out: $out)" || ok
grep -qxF 'kanban dispatch --max 2' "$(splog clamp-sanctioned)" \
	&& ok || bad "T19b: a sanctioned pair did not pass --max 2 through (got: $(tr '\n' ';' <"$(splog clamp-sanctioned)"))"
nrun=$(sql "$(spdb clamp-sanctioned)" "SELECT COUNT(*) FROM tasks WHERE status='running';")
[ "$nrun" = "2" ] && ok || bad "T19b: $nrun cards running after a sanctioned pair (expected 2)"

# T19c: DRY-RUN is untouched - the verdict prints, the clamp never does, and
# nothing dispatches (ticks and tests read this output; it stays byte-stable).
out=$(PAIRRUN "$NF_WEB
$WEB" --max 2)
printf "%s" "$out" | grep -qF 'pair: forbidden (t_nofiles t_web' \
	&& ok || bad "T19c: dry-run lost its pair verdict (out: $out)"
printf "%s" "$out" | grep -q "clamp:" \
	&& bad "T19c: dry-run printed a clamp line - the clamp must sit BELOW the dry exit (out: $out)" || ok
printf "%s" "$out" | grep -q "dry-run: no dispatch" \
	&& ok || bad "T19c: dry-run dispatched (out: $out)"

# T19d: --max 1 needs no clamp - the request is already single, so the line
# stays silent (N>1 is the condition, never a blanket on every forbidden pass).
out=$(SPAWNRUN max1 "$NF_WEB
$WEB" --max 1)
printf "%s" "$out" | grep -q "clamp:" \
	&& bad "T19d: --max 1 is already single - no clamp line expected (out: $out)" || ok
grep -qxF 'kanban dispatch --max 1' "$(splog max1)" \
	&& ok || bad "T19d: --max 1 pass did not hand --max 1 through (got: $(tr '\n' ';' <"$(splog max1)"))"
# T19e: the clamp also covers a BARE pass (no --max): the guard computes its
# own slot count there and it can equal 2, so an unclamped bare pass would keep
# the hole open behind the "always pass an explicit --max" ritual this card
# exists to delete.
out=$(SPAWNRUN clamp-bare "$NF_WEB
$WEB")
printf "%s" "$out" | grep -qF 'clamp: wanted 2 -> 1 (pair: forbidden' \
	&& ok || bad "T19e: a bare pass against a forbidden head set did not clamp (out: $out)"
grep -qxF 'kanban dispatch --max 1' "$(splog clamp-bare)" \
	&& ok || bad "T19e: the bare pass was not clamped to --max 1 (got: $(tr '\n' ';' <"$(splog clamp-bare)"))"
nrun=$(sql "$(spdb clamp-bare)" "SELECT COUNT(*) FROM tasks WHERE status='running';")
[ "$nrun" = "1" ] && ok || bad "T19e: $nrun cards running after a bare clamped pass (expected 1)"

# T20 (local + CI): the CLAIMABLE-WORK GATE (card t_3b13064e, 2026-11-11). The
# pass used to gate the inner `hermes kanban dispatch` on a READY card alone,
# but the inner claims two lanes: dispatch_once enumerates
# `status='review' AND claim_lock IS NULL` and claims it through
# claim_review_task (review -> running). So on a quiet board - every worker
# handed off together, ready queue empty, the review lane holding the only
# work - the pass printed `no dispatch` and the reviews could never spawn from
# it (probed live 2026-11-11 08:19: two cards sat in review with running=0 and
# the inner dispatcher never got the call). Shapes pinned: (T20a) a
# review-only board opens the gate, the stub inner dispatcher is called ONCE
# and the review row really spawns (review -> running); (T20b) CONTROL - a
# board with no claimable work at all stays a no-op, never a pointless call;
# (T20c) CONTROL - a review card carrying a NAMED lock is invisible to the
# inner claim filter, so it does not open the gate; (T20d) the dry-run must
# name the review candidate it would spawn (a dry-run reporting 0 while the
# live pass spawns one is the verdict-beside-the-action failure this factory
# already closed on the pair clamp).
REV_SEED="INSERT INTO tasks VALUES('t_rev','review','review handoff','the review of the batch',NULL,NULL,5,'2026-11-11T08:00',NULL,NULL);"

# T20a: ONE review card, zero ready, zero running -> the dispatcher IS called,
# once, and the review spawns.
out=$(SPAWNRUN review-only "$REV_SEED")
printf "%s" "$out" | grep -qF 'review candidates: 1 unclaimed (t_rev)' \
	&& ok || bad "T20a: a review-only board must name its review candidate (out: $out)"
ncalls=$(grep -c . "$(splog review-only)" || true)
[ "$ncalls" = "1" ] && ok || bad "T20a: the inner dispatcher must be called EXACTLY once (calls: $ncalls, log: $(tr '\n' ';' <"$(splog review-only)"))"
grep -qE '^kanban dispatch --max [0-9]+$' "$(splog review-only)" \
	&& ok || bad "T20a: the inner call lost its shape (log: $(tr '\n' ';' <"$(splog review-only)"))"
printf "%s" "$out" | grep -qF 'Spawned: 1 (t_rev)' \
	&& ok || bad "T20a: the review row did not spawn (out: $out)"
s=$(sql "$(spdb review-only)" "SELECT status FROM tasks WHERE id='t_rev';")
[ "$s" = "running" ] && ok || bad "T20a: review card status is '$s' after the pass (expected running - claim_review_task flips review -> running)"
printf "%s" "$out" | grep -q "no dispatch" \
	&& bad "T20a: the pass took the no-dispatch branch on a review-only board (out: $out)" || ok

# T20b: CONTROL - zero ready AND zero review: no claimable work, no call. A
# pass with nothing to claim must stay a no-op, not a pointless invocation.
out=$(SPAWNRUN empty-board "SELECT 1;")
ncalls=$(grep -c . "$(splog empty-board)" || true)
[ "$ncalls" = "0" ] && ok || bad "T20b: an empty board made $ncalls inner dispatch call(s) - expected none (log: $(tr '\n' ';' <"$(splog empty-board)"))"
printf "%s" "$out" | grep -qF 'no dispatch (running=0/cap=2, claimable=0)' \
	&& ok || bad "T20b: the empty-board line must state the reason (out: $out)"
printf "%s" "$out" | grep -qF 'review candidates: 0 unclaimed' \
	&& ok || bad "T20b: the empty board must still print the review count (out: $out)"

# T20c: CONTROL - a review card with a NAMED lock is not claimable: the inner
# filter is `claim_lock IS NULL`, so the gate must stay shut. The lock is
# deliberately NOT `...-hold`: the guard's bug #13 leg normalizes ANY
# `pibtw:%-hold` card to blocked before the gate, which would make this case
# pass for the wrong reason (and prove nothing about the claim filter).
out=$(SPAWNRUN review-locked "INSERT INTO tasks VALUES('t_revl','review','parked review','the review waits on the design gate','pibtw:review-park',NULL,5,'2026-11-11T08:00',NULL,NULL);")
ncalls=$(grep -c . "$(splog review-locked)" || true)
[ "$ncalls" = "0" ] && ok || bad "T20c: a locked review card opened the gate ($ncalls call(s)) - the inner filter is claim_lock IS NULL (log: $(tr '\n' ';' <"$(splog review-locked)"))"
printf "%s" "$out" | grep -qF 'review candidates: 0 unclaimed' \
	&& ok || bad "T20c: a locked review card counted as a review candidate (out: $out)"
printf "%s" "$out" | grep -qF 'no dispatch (running=0/cap=2, claimable=0)' \
	&& ok || bad "T20c: the locked-review board must report no claimable work (out: $out)"
s=$(sql "$(spdb review-locked)" "SELECT status||'|'||claim_lock FROM tasks WHERE id='t_revl';")
[ "$s" = "review|pibtw:review-park" ] && ok || bad "T20c: the locked review card changed shape ('$s')"

# T20d: the DRY-RUN names the review candidate it would spawn - the live pass
# in T20a spawned one, so a dry-run reporting zero would lie about it.
out=$(PAIRRUN "$REV_SEED")
printf "%s" "$out" | grep -qF 'review candidates: 1 unclaimed (t_rev)' \
	&& ok || bad "T20d: dry-run did not name the review candidate (out: $out)"
printf "%s" "$out" | grep -qF 'pair: sanctioned (none, empty would-spawn set)' \
	&& ok || bad "T20d: a review row must not enter the pair accounting (out: $out)"
printf "%s" "$out" | grep -q "dry-run: no dispatch" \
	&& ok || bad "T20d: dry-run dispatched (out: $out)"
rm -rf "$SPAWN_BASE"

# T21 (local + CI): the RUNNING path shares the candidate precedence (card
# t_42ff3dad, 2026-11-12). surface_of_card() read the running card's WHOLE body
# prose, so a card whose curated FILES: line is disjoint from a sibling's but
# whose CONTEXT prose cites the sibling's DIRECTORY claimed that directory the
# moment it started running, and the sibling went `hold: ... (same-file)`. The
# same pair read `sanctioned` while both cards were ready (live receipt: the
# 19:39 tick sanctioned t_4bcea3dd+t_2daae1aa, the 19:52 tick - with
# t_4bcea3dd running - held t_2daae1aa on the bare token `webui/app/`, which
# its prose merely cited; neither card wrote a file the other wrote). A cap
# slot idled for the whole run, and the pair verdict depended on RUN STATE -
# the one thing the machine verdict law assumes cannot happen.
#
# The fixture needs REAL worktree diffs, so the guard under test runs from a
# THROWAWAY git repo (the guard cd's to <script>/..): wt/t_cite carries a diff
# on scripts/embed-lane.sh, wt/t_intrude a diff INSIDE the sibling's directory.
# Shapes pinned:
#   T21a  the incident - t_cite (FILES scripts/*, prose cites webui/app/) is
#         spawnable and t_web is LEGAL both before and after t_cite starts
#         running: the pair decision must not read the run state.
#   T21b  the conservative direction, DIFF leg - a running card whose FILES
#         line is disjoint but whose worktree diff really writes into the
#         sibling's directory still holds it (the union cannot be used to
#         defeat the same-file law).
#   T21c  the conservative direction, FILES leg - a running card that DECLARES
#         the sibling's directory holds it with no diff at all.
#   T21d  the prose fallback survives - a running card with NO FILES: line is
#         still guarded by its prose (that is the only surface the guard has),
#         and a FILES-less card with no branch yet must not kill the pass.
T21REPO=$(mktemp -d); T21HOME=$(mktemp -d)
mkdir -p "$T21REPO/scripts"
cp "$GUARD" "$T21REPO/scripts/factory-dispatch.sh"
GITC() { git -c user.name=t -c user.email=t@t "$@" >/dev/null 2>&1; }
(
  cd "$T21REPO" || exit 3
  git init -q -b dev . || exit 3
  printf '.worktrees/\n' >.gitignore
  printf '#!/usr/bin/env bash\n: baseline\n' >scripts/embed-lane.sh
  GITC add -A; GITC commit -qm init
  GITC checkout -q -b wt/t_cite
  printf '#!/usr/bin/env bash\necho cite\n' >scripts/embed-lane.sh
  GITC add -A; GITC commit -qm cite
  GITC checkout -q dev
  GITC checkout -q -b wt/t_intrude
  mkdir -p webui/app/components/ui
  printf '<template><div/></template>\n' >webui/app/components/ui/BandSelect.vue
  GITC add -A; GITC commit -qm intrude
  GITC checkout -q dev
) || bad "T21: the throwaway fixture repo could not be built"
mkdir -p "$T21HOME/kanban/boards/project-omega" \
         "$T21REPO/.worktrees/t_cite" "$T21REPO/.worktrees/t_intrude" \
         "$T21REPO/.worktrees/t_filerun" "$T21REPO/.worktrees/t_nofiles"
printf 'project-omega\n' >"$T21HOME/kanban/current"
T21DB_N="$(mixpath "$T21HOME")/kanban/boards/project-omega/kanban.db"
# The prose cites the DIRECTORY (bare `webui/app/` token) exactly like the live
# card did; tokens_of turns it into the prefix token that held the sibling.
T21_CITE_BODY="FILES: scripts/embed-lane.sh scripts/embed-lane_test.sh
CONTEXT: which roots get bundled - under webui/app/ the generated panel
manifest (webui/app/composables/module-panels.generated.ts) decides what ships.
DO: touch scripts/embed-lane.sh"
sqlex "$T21DB_N" "CREATE TABLE tasks(id TEXT, status TEXT, title TEXT DEFAULT '', body TEXT DEFAULT '',
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT,
  worker_pid INTEGER, last_heartbeat_at INTEGER);
INSERT INTO tasks VALUES('t_cite','ready','bundle roots','$T21_CITE_BODY',NULL,NULL,5,'2026-11-12T19:00',NULL,NULL);
INSERT INTO tasks VALUES('t_web','ready','band select','FILES: webui/app/components/ui/BandSelect.vue',NULL,NULL,5,'2026-11-12T19:01',NULL,NULL);
INSERT INTO tasks VALUES('t_intrude','blocked','intruder','FILES: scripts/other.sh',NULL,NULL,5,'2026-11-12T19:02',NULL,NULL);
INSERT INTO tasks VALUES('t_filerun','blocked','declared sibling dir','FILES: webui/app/components/ui/',NULL,NULL,5,'2026-11-12T19:03',NULL,NULL);
INSERT INTO tasks VALUES('t_nofiles','blocked','prose only','CONTEXT: the guard reads webui/app/ roots while deciding',NULL,NULL,5,'2026-11-12T19:04',NULL,NULL);"
T21STATE() {  # $1 = id to leave RUNNING ('' -> t_cite ready); t_web is the sibling
  sqlex "$T21DB_N" "UPDATE tasks SET status='blocked', claim_lock=NULL
WHERE id IN ('t_cite','t_intrude','t_filerun','t_nofiles');
UPDATE tasks SET status='ready', claim_lock=NULL WHERE id='t_web';"
  case "$1" in
    '') sqlex "$T21DB_N" "UPDATE tasks SET status='ready' WHERE id='t_cite';" ;;
    *)  sqlex "$T21DB_N" "UPDATE tasks SET status='running' WHERE id='$1';" ;;
  esac
}
T21RUN() {  # $1 = running id ('' = none) -> guard output on the fixture board
  T21STATE "$1"
  ( cd "$T21REPO" && HOME="$T21HOME" HERMES_HOME= HERMES_KANBAN_HOME="$(mixpath "$T21HOME")" \
      HERMES_KANBAN_DB= HERMES_KANBAN_BOARD= bash scripts/factory-dispatch.sh --dry-run 2>&1 )
}

# T21a: running=none and running=t_cite must agree that t_web may spawn.
out0=$(T21RUN "")
printf "%s" "$out0" | grep -q "^legal: t_cite" \
	&& ok || bad "T21a: t_cite should be legal while ready (out: $out0)"
printf "%s" "$out0" | grep -q "^legal: t_web" \
	&& ok || bad "T21a: t_web should be legal beside the ready t_cite (out: $out0)"
printf "%s" "$out0" | grep -qF 'pair: sanctioned (t_cite t_web, classes go+webui, FILES disjoint)' \
	&& ok || bad "T21a: the disjoint pair was not sanctioned while both are ready (out: $out0)"
out1=$(T21RUN t_cite)
printf "%s" "$out1" | grep -q "^hold: t_web" \
	&& bad "T21a: t_web HELD once t_cite started running - the pair verdict read the run state; the running card's prose must not claim a directory its FILES: line never named (out: $out1)" \
	|| ok
printf "%s" "$out1" | grep -q "^legal: t_web" \
	&& ok || bad "T21a: t_web should stay legal once t_cite is running (out: $out1)"
printf "%s" "$out1" | grep -qF 'pair: sanctioned (t_web, single would-spawn candidate)' \
	&& ok || bad "T21a: the pair verdict flipped with the run state (out: $out1)"

# T21b: DIFF leg - t_intrude declares scripts/other.sh but its branch really
# writes webui/app/components/ui/BandSelect.vue. The union must hold t_web.
out=$(T21RUN t_intrude)
printf "%s" "$out" | grep -q "^hold: t_web  (same-file: webui/app/components/ui/BandSelect.vue" \
	&& ok || bad "T21b: a running card whose DIFF writes the sibling's file must still hold it (out: $out)"

# T21c: FILES leg - t_filerun declares the sibling's directory and has no diff.
out=$(T21RUN t_filerun)
printf "%s" "$out" | grep -q "^hold: t_web" \
	&& ok || bad "T21c: a running card that DECLARES the sibling's directory must hold it (out: $out)"

# T21d: FILES-less running card - prose is the only surface it has (and it has
# no branch yet, so the diff probe fails; the pass must not die on that).
out=$(T21RUN t_nofiles); rc=$?
[ "$rc" -eq 0 ] && ok || bad "T21d: a FILES-less running card with no branch killed the pass (exit $rc, out: $out)"
printf "%s" "$out" | grep -q "^hold: t_web" \
	&& ok || bad "T21d: the prose fallback was lost - a FILES-less running card must still hold the sibling (out: $out)"
rm -rf "$T21REPO" "$T21HOME"

# T22 (local + CI): the CORPSE readout (card t_534d58c5, 2026-11-13). Cards
# died mid-pass leaving the pass log reading `running: t_x` for a process that
# was already gone - the death only existed as a traceback nobody had named.
# The readout must fire for a running card whose registered worker pid is
# PROVABLY gone, must stay SILENT for a live pid, and must never fire on a
# refused probe (the first live run of this readout printed a corpse for two
# LIVE cards because tasklist refused `//FI`; a false corpse is worse than no
# readout). It is also read-only: the card keeps its cap slot and its status.
T22REPO=$(mktemp -d); T22HOME=$(mktemp -d)
mkdir -p "$T22REPO/scripts" "$T22HOME/kanban/boards/project-omega"
cp "$GUARD" "$T22REPO/scripts/factory-dispatch.sh"
printf 'project-omega\n' >"$T22HOME/kanban/current"
T22DB_N="$(mixpath "$T22HOME")/kanban/boards/project-omega/kanban.db"
# A live pid the host can prove (this test shell) and a pid that is really
# gone: spawn a sleeper, read its host pid, kill it, reap it.
LIVE_PID=$(cat /proc/$$/winpid 2>/dev/null || echo $$)
sleep 60 & T22VICTIM=$!
VICTIM_PID=$(cat "/proc/$T22VICTIM/winpid" 2>/dev/null || echo "$T22VICTIM")
kill "$T22VICTIM" 2>/dev/null || true
wait "$T22VICTIM" 2>/dev/null || true
T22NOW=$(date +%s)
sqlex "$T22DB_N" "CREATE TABLE tasks(id TEXT, status TEXT, title TEXT DEFAULT '', body TEXT DEFAULT '',
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT,
  worker_pid INTEGER, last_heartbeat_at INTEGER, current_run_id INTEGER);
CREATE TABLE task_runs(id INTEGER PRIMARY KEY, task_id TEXT, profile TEXT, status TEXT);
INSERT INTO task_runs VALUES(4242,'t_dead','cody','running');
INSERT INTO task_runs VALUES(4243,'t_alive','vera','running');
INSERT INTO tasks VALUES('t_dead','running','dead worker','FILES: scripts/x.sh',NULL,$((T22NOW + 900)),5,'2026-11-12T19:00',$VICTIM_PID,$((T22NOW - 45)),4242);
INSERT INTO tasks VALUES('t_alive','running','live worker','FILES: scripts/y.sh',NULL,$((T22NOW + 900)),5,'2026-11-12T19:01',$LIVE_PID,$((T22NOW - 5)),4243);"
T22RUN() { ( cd "$T22REPO" && HOME="$T22HOME" HERMES_HOME= HERMES_KANBAN_HOME="$(mixpath "$T22HOME")" \
    HERMES_KANBAN_DB= HERMES_KANBAN_BOARD= bash scripts/factory-dispatch.sh --dry-run 2>&1 ); }
out=$(T22RUN); rc=$?
[ "$rc" -eq 0 ] && ok || bad "T22: the corpse readout killed the pass (exit $rc, out: $out)"
printf "%s" "$out" | grep -q "corpse: t_dead run 4242 (cody) worker pid $VICTIM_PID is GONE" \
	&& ok || bad "T22a: a running card with a dead worker pid printed no corpse line (out: $out)"
printf "%s" "$out" | grep -qF "last tool calls in" \
	&& ok || bad "T22a: the corpse line named no corpse source (out: $out)"
printf "%s" "$out" | grep -q "corpse: t_alive" \
	&& bad "T22b: a LIVE worker pid read as a corpse - a false corpse makes every later corpse line untrustworthy (out: $out)" \
	|| ok
s=$(sql "$T22DB_N" "SELECT status||'|'||coalesce(worker_pid,0)||'|'||current_run_id FROM tasks WHERE id='t_dead';")
[ "$s" = "running|$VICTIM_PID|4242" ] && ok || bad "T22c: the readout MUTATED the dead card ('$s') - it must be read-only"
rm -rf "$T22REPO" "$T22HOME"

echo
echo "factory-dispatch_test: $pass ok, $fail fail"
[ "$fail" -eq 0 ]
