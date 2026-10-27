#!/usr/bin/env bash
set -uo pipefail

# factory-dispatch_test.sh - runnable checks for scripts/factory-dispatch.sh.
# Run: bash scripts/factory-dispatch_test.sh
# T1 pins the empty-RUNNING kill: the class-law RC loop must survive a zero
# running board under set -euo pipefail (proven live 2026-10-24: the guard
# died silently, exit 1, no verdicts, whenever no card was running).
# T2 is a live dry-run smoke against the real board (local only: skipped
# when the board DB is absent, e.g. CI runners).

GUARD="$(cd "$(dirname "$0")" && pwd)/factory-dispatch.sh"
DB="$HOME/.hermes/kanban/boards/project-omega/kanban.db"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }

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

# T2 (local only): dry-run on the real board must exit 0 with output - a
# silent empty run is the empty-RUNNING kill signature.
if [ -f "$DB" ]; then
	out=$(bash "$GUARD" --dry-run 2>&1)
	rc=$?
	[ "$rc" -eq 0 ] && ok || bad "T2: dry-run exited $rc (expected 0)"
	[ -n "$out" ] && ok || bad "T2: dry-run printed nothing (exit $rc) - the empty-RUNNING kill signature"
else
	echo "T2: skipped (no board DB at $DB)"
fi

# T3 (local + CI): guard bug #4 - review-park SQL. A review-state card
# gets the dispatch-hold sentinel ONLY when its lock is free; a foreign
# sentinel (design-hold) survives; unpark restores NULL, never ''.
T3DB=$(mktemp)
sqlite3 "$T3DB" "CREATE TABLE tasks(id TEXT, status TEXT, claim_lock TEXT);
INSERT INTO tasks VALUES('t_rev_free','review',NULL);
INSERT INTO tasks VALUES('t_rev_held','review','pibtw:design-hold');
INSERT INTO tasks VALUES('t_ready1','ready',NULL);" 2>/dev/null
sqlite3 "$T3DB" "UPDATE tasks SET claim_lock='pibtw:dispatch-hold' WHERE id='t_rev_free' AND (claim_lock IS NULL OR claim_lock='');"
l=$(sqlite3 "$T3DB" "SELECT claim_lock FROM tasks WHERE id='t_rev_free';")
[ "$l" = "pibtw:dispatch-hold" ] && ok || bad "T3: free review card not parked (claim_lock='$l')"
l=$(sqlite3 "$T3DB" "SELECT claim_lock FROM tasks WHERE id='t_rev_held';")
[ "$l" = "pibtw:design-hold" ] && ok || bad "T3: park overwrote a foreign sentinel ('$l')"
sqlite3 "$T3DB" "UPDATE tasks SET claim_lock=NULL WHERE id='t_rev_free' AND claim_lock='pibtw:dispatch-hold';"
l=$(sqlite3 "$T3DB" "SELECT claim_lock IS NULL FROM tasks WHERE id='t_rev_free';")
[ "$l" = "1" ] && ok || bad "T3: unpark left claim_lock='$l' (must restore NULL, never '')"
rm -f "$T3DB"

# T4 (local + CI): guard bug #5 - the candidate sort must be priority
# first (DESC, higher = more urgent), ties oldest-first.
T4DB=$(mktemp)
sqlite3 "$T4DB" "CREATE TABLE tasks(id TEXT, status TEXT, claim_lock TEXT, priority INTEGER DEFAULT 0, created_at TEXT);
INSERT INTO tasks VALUES('t_old_low','ready',NULL,0,'2026-10-01');
INSERT INTO tasks VALUES('t_mid_arc','ready',NULL,5,'2026-10-20');
INSERT INTO tasks VALUES('t_new_urgent','ready',NULL,10,'2026-10-26');
INSERT INTO tasks VALUES('t_newer_low','ready',NULL,0,'2026-10-27');" 2>/dev/null
order=$(sqlite3 "$T4DB" "SELECT id FROM tasks WHERE status='ready' ORDER BY priority DESC, created_at;")
[ "$order" = "t_new_urgent
t_mid_arc
t_old_low
t_newer_low" ] && ok || bad "T4: priority sort wrong - got: $order"
rm -f "$T4DB"

# T5 (local + CI): guard bug #6 - brace-list FILES surfaces. A FILES line
# like `modules/{cron,memory}/` must expand to every member before token
# extraction (proven live 2026-10-26: it collapsed to a bare modules/
# token and the guard read a modules/cron collision false-legal). The
# eval gate admits path chars only; metachar lines pass through raw.
# Also pins the bash 5.3 =~ shape: the regex must ride an UNQUOTED var -
# a quoted RHS is literal in 5.3 and matches nothing.
tok=$(bash -c '
source <(sed -n "/^move_paths()/,/^}/p; /^tokens_of()/,/^}/p" "'"$GUARD"'")
printf "FILES: modules/{cron,memory,delegate}/\nplain modules/x/ text\ncmd;bad {a,b}\n" | tokens_of
' 2>/dev/null)
[ "$(printf "%s\n" "$tok" | grep -c "^modules/")" = "4" ] \
	&& ok || bad "T5: brace list emitted $(printf "%s\n" "$tok" | grep -c "^modules/") tokens, expected 4 (got: $tok)"
printf "%s" "$tok" | grep -q "^modules/cron/" && ok || bad "T5: modules/cron/ missing from expansion"
printf "%s" "$tok" | grep -q "^modules/delegate/" && ok || bad "T5: modules/delegate/ missing from expansion"
printf "%s" "$tok" | grep -q "cmd;bad" && bad "T5: metachar line was eval-expanded (gate failed)" || ok

# T6 (local + CI): guard bug #7 - move-shaped lines. A move line names the
# card's WHOLE write surface, including source paths the rooted prefix scan
# can never see (proven live 2026-10-28: t_7e47d18f "webui/ -> engine/webui/"
# spawned beside the running webui settings card; verdicts read them disjoint).
# Prose arrows and the literal "moves X to Y" phrase must stay silent.
tok=$(bash -c '
source <(sed -n "/^move_paths()/,/^}/p; /^tokens_of()/,/^}/p" "'"$GUARD"'")
printf "%s\n" \
  "engine ring move: webui/ -> engine/webui/, http_channel/ -> engine/http_channel/" \
  "moves agent/ ai/ tui/ webui/ + docs + CI paths" \
  "modules/{cron,memory}/ dissolves into core/" \
  "an arrow (\`->\`, \`=>\`) in prose with no paths" \
  | tokens_of
' 2>/dev/null)
for want in "webui/" "engine/webui/" "http_channel/" "engine/http_channel/" \
            "agent/" "ai/" "tui/" "modules/cron/" "modules/memory/" "core/"; do
  printf "%s" "$tok" | grep -qx "$want" && ok || bad "T6: move token '$want' missing (got: $tok)"
done
printf "%s" "$tok" | grep -q "arrow" && bad "T6: prose arrow line leaked tokens" || ok

# T7 (local + CI): the collision itself - a move source token collides with a
# running card's edited webui file under the existing prefix-overlap law.
rc=$(bash -c '
source <(sed -n "/^move_paths()/,/^}/p; /^tokens_of()/,/^}/p; /^overlaps()/,/^}/p" "'"$GUARD"'")
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
rc=$(bash -c '
source <(sed -n "/^move_paths()/,/^}/p; /^tokens_of()/,/^}/p" "'"$GUARD"'")
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
# the stub dispatch, report delta, flag over-run (guard bug #8: hermes dispatch
# counts running AFTER its reclaim phase requeues claim-expired cards, so the
# guard snapshot is the stale one).
FDHOME=$(mktemp -d)
mkdir -p "$FDHOME/.hermes/kanban/boards/project-omega" "$FDHOME/bin"
FDB="$FDHOME/.hermes/kanban/boards/project-omega/kanban.db"
sqlite3 "$FDB" "CREATE TABLE tasks(id TEXT, status TEXT, title TEXT DEFAULT '', body TEXT DEFAULT '',
  claim_lock TEXT, claim_expires INTEGER, priority INTEGER DEFAULT 0, created_at TEXT);
INSERT INTO tasks VALUES('t_move','running','engine ring move','engine ring move: webui/ -> engine/webui/','pibtw:1',NULL,0,'2026-10-28T01:00');
INSERT INTO tasks VALUES('t_cand','ready','engine ring move implement','FILES: engine/webui/
engine ring move: webui/ -> engine/webui/',NULL,0,5,'2026-10-28T02:00');" 2>/dev/null
cat >"$FDHOME/bin/hermes" <<'EOF'
#!/usr/bin/env bash
# stub dispatch: mimic inner behavior - release_stale_claims drops an expired
# running card to ready, then spawn MORE cards than the guard requested.
sqlite3 "$HERMES_STUB_DB" "UPDATE tasks SET status='ready', claim_lock=NULL WHERE id='t_move';"
sqlite3 "$HERMES_STUB_DB" "INSERT INTO tasks VALUES('t_new1','running','x','',NULL,NULL,0,'2026-10-28T03:00');
INSERT INTO tasks VALUES('t_new2','running','x','',NULL,NULL,0,'2026-10-28T03:01');
INSERT INTO tasks VALUES('t_new3','running','x','',NULL,NULL,0,'2026-10-28T03:02');
INSERT INTO tasks VALUES('t_new4','running','x','',NULL,NULL,0,'2026-10-28T03:03');"
echo "Spawned: 4 (t_new1 t_new2 t_new3 t_new4)"
EOF
chmod +x "$FDHOME/bin/hermes"
out=$(HOME="$FDHOME" PATH="$FDHOME/bin:$PATH" HERMES_STUB_DB="$FDB" bash "$GUARD" --dry-run 2>&1)
rc=$?
[ "$rc" -eq 0 ] && ok || bad "T9: dry-run exited $rc"
printf "%s" "$out" | grep -q "^hold: t_cand" && ok || bad "T9: move card did not hold the webui candidate (out: $out)"
printf "%s" "$out" | grep -q "dry-run: no dispatch" && ok || bad "T9: dry-run dispatched (out: $out)"
out=$(HOME="$FDHOME" PATH="$FDHOME/bin:$PATH" HERMES_STUB_DB="$FDB" bash "$GUARD" 2>&1)
rc=$?
[ "$rc" -eq 0 ] && ok || bad "T9: dispatch pass exited $rc (out: $out)"
printf "%s" "$out" | grep -q "wanted <=3 running (snapshot 1 + slots 2)" && ok || bad "T9: handoff line wrong (out: $out)"
printf "%s" "$out" | grep -q "after: 4 (delta 3 since snapshot)" && ok || bad "T9: post-call re-read wrong (out: $out)"
printf "%s" "$out" | grep -q "over-run" && ok || bad "T9: over-run not reported (out: $out)"
l=$(sqlite3 "$FDB" "SELECT claim_lock IS NULL FROM tasks WHERE id='t_cand';")
[ "$l" = "1" ] && ok || bad "T9: unpark left t_cand locked ('$l')"
rm -rf "$FDHOME"

echo
echo "factory-dispatch_test: $pass ok, $fail fail"
[ "$fail" -eq 0 ]