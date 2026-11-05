#!/usr/bin/env bash
set -uo pipefail

# factory-dispatch_test.sh - runnable checks for scripts/factory-dispatch.sh.
# Run: bash scripts/factory-dispatch_test.sh
# T1 pins the empty-RUNNING kill: the class-law RC loop must survive a zero
# running board under set -euo pipefail (proven live 2026-10-24: the guard
# died silently, exit 1, no verdicts, whenever no card was running).
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
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT);
INSERT INTO tasks VALUES('t_move','running','engine ring move','engine ring move: webui/ -> engine/webui/','pibtw:1',NULL,0,'2026-10-28T01:00');
INSERT INTO tasks VALUES('t_cand','ready','engine ring move implement','FILES: engine/webui/
engine ring move: webui/ -> engine/webui/',NULL,0,5,'2026-10-28T02:00');"
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
    "INSERT INTO tasks VALUES('t_new1','running','x','',NULL,NULL,0,'2026-10-28T03:00');",
    "INSERT INTO tasks VALUES('t_new2','running','x','',NULL,NULL,0,'2026-10-28T03:01');",
    "INSERT INTO tasks VALUES('t_new3','running','x','',NULL,NULL,0,'2026-10-28T03:02');",
    "INSERT INTO tasks VALUES('t_new4','running','x','',NULL,NULL,0,'2026-10-28T03:03');",
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
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT);
INSERT INTO tasks VALUES('t_go','running','nm purity','FILES: scripts/nm-purity.sh','pibtw:1',NULL,0,'2026-10-28T01:00');
INSERT INTO tasks VALUES('t_web','ready','webui fix','FILES: engine/webui/',NULL,0,5,'2026-10-28T02:00');"
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
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT);
INSERT INTO tasks VALUES('t_run','running','go bits','pure go bits','pibtw:1',NULL,0,'2026-10-30T01:00');
INSERT INTO tasks VALUES('t_cand','ready','webui candidate','FILES: engine/webui/app/',NULL,NULL,0,'2026-10-30T02:00');
INSERT INTO tasks VALUES('t_hold','blocked','terminal module hold','design gate','pibtw:v0.14.0-hold',NULL,0,'2026-10-29T01:00');
INSERT INTO tasks VALUES('t_hold2','blocked','browser module hold','design gate','pibtw:design-hold',NULL,0,'2026-10-29T01:01');
INSERT INTO tasks VALUES('t_pid','blocked','stale-shape card','x','pibtw:4242',NULL,0,'2026-10-29T01:02');"
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
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT);"
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
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT);
INSERT INTO tasks VALUES('t_drun','running','go bits','pure go bits','pibtw:1',NULL,0,'2026-10-31T01:00');
INSERT INTO tasks VALUES('t_dready','ready','legal filler','filler','' ,NULL,0,'2026-10-30T00:30');
INSERT INTO tasks VALUES('t_drift14','ready','terminal module hold','design gate','pibtw:v0.14.0-hold',NULL,0,'2026-10-30T01:00');
INSERT INTO tasks VALUES('t_driftde','ready','browser module hold','design gate','pibtw:design-hold',NULL,0,'2026-10-30T01:01');"
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
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT);
INSERT INTO tasks VALUES('t_cand','ready','webui fix','FILES: engine/webui/',NULL,0,0,'2026-11-01T01:00');"
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
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT);
INSERT INTO tasks VALUES('t_pop','ready','populated candidate','FILES: engine/webui/',NULL,0,0,'2026-11-05T01:00');"
printf 'project-omega\n' >"$FDHOME/app/hermes/kanban/current"
PDB_N="$FDHOME_N/rooth/hermes/kanban/boards/project-omega/kanban.db"
sqlex "$PDB_N" "CREATE TABLE tasks(id TEXT, status TEXT, title TEXT DEFAULT '', body TEXT DEFAULT '',
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT);
INSERT INTO tasks VALUES('t_prof','ready','profile-root candidate','FILES: engine/webui/',NULL,0,0,'2026-11-05T01:00');"
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

echo
echo "factory-dispatch_test: $pass ok, $fail fail"
[ "$fail" -eq 0 ]
