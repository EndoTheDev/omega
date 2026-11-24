#!/usr/bin/env bash
set -uo pipefail

# store-fixture-extract_test.sh - the sanitizer's own gate.
# Run: bash scripts/store-fixture-extract_test.sh
#
# A fixture is only committable if nothing real left it. This suite builds a
# synthetic pre-JSONL store whose every content column carries a unique marker,
# runs the sanitizer over it, and then proves the shapes:
#
#   B1  the source is opened read-only: its bytes are identical after the run
#   B2  no marker survives anywhere in the emitted tree (store + manifest)
#   B3  --self-check is green on the emitted fixture
#   B4  timestamps are byte-identical to the source rows
#   B5  the schema is preserved: same table set (minus the derived tables),
#       same columns per kept table
#   B6  every id in the fixture is 32 hex; distinct source ids stay distinct
#   B7  the derived tables (fts, sqlite_sequence, any unknown table) are gone
#   B8  the manifest names the mandatory session's hashed id, its equal
#       created_at/updated_at, the dropped tables and the row counts
#   B9  structural classes are selected, not recency: the equal-stamp class, a
#       multi-message session and a zero-message session are all present
#   B10 TEETH: with the scrubber disabled the markers DO leak and --self-check
#       goes red naming the columns. A negative assertion nobody has seen pass
#       the other way is not a test.
#
# The synthetic store builder lives in this file (python3 stdlib only) so the
# suite needs no committed binary fixture.

SUT="$(cd "$(dirname "$0")" && pwd)/store-fixture-extract.py"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1"; }

chk() {
	local label=$1
	shift
	if "$@"; then
		ok
	else
		bad "$label"
	fi
}

has() { grep -qF -- "$2" <<<"$1"; }
lacks() { ! grep -qF -- "$2" <<<"$1"; }

PY=python3
command -v "$PY" >/dev/null 2>&1 || { echo "store-fixture-extract_test.sh: python3 not found" >&2; exit 2; }

FX=$(mktemp -d "${TMPDIR:-/tmp}/storefixture-fx.XXXXXX") || exit 1
[ -d "$FX" ] || { echo "store-fixture-extract_test.sh: no fixture dir" >&2; exit 1; }

# A path handed to a NATIVE tool (python3) cannot be an MSYS /tmp path: on the
# Windows host that resolves to D:\tmp\... and the file "does not exist".
nat() {
	if command -v cygpath >/dev/null 2>&1; then cygpath -m "$1"; else printf '%s' "$1"; fi
}
FX_N=$(nat "$FX")
# the sanitizer rides the same rule: python3 is a native exe
SUT_N=$(nat "$SUT")

MARK="MARKERLEAK"

# --- the synthetic pre-JSONL store -------------------------------------------
cat >"$FX/mkstore.py" <<'PYSTORE'
import sqlite3
import sys

path, mark = sys.argv[1], sys.argv[2]
MANDATORY = "72863811adb93cd5ca47642c9b8393f6"

db = sqlite3.connect(path)
db.executescript("""
CREATE TABLE sessions (
	id         TEXT PRIMARY KEY,
	parent_id  TEXT REFERENCES sessions(id) ON DELETE CASCADE,
	label      TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	prompt_tokens INTEGER NOT NULL DEFAULT 0,
	completion_tokens INTEGER NOT NULL DEFAULT 0,
	pinned INTEGER NOT NULL DEFAULT 0,
	archived INTEGER NOT NULL DEFAULT 0,
	title_state INTEGER NOT NULL DEFAULT 0,
	last_read_at TEXT NOT NULL DEFAULT '',
	agent TEXT NOT NULL DEFAULT 'default',
	last_run TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	provider TEXT NOT NULL DEFAULT '',
	workspace TEXT NOT NULL DEFAULT '',
	summary TEXT NOT NULL DEFAULT '',
	listed INTEGER NOT NULL DEFAULT 1,
	context_start   TEXT NOT NULL DEFAULT '',
	context_summary TEXT NOT NULL DEFAULT ''
);
CREATE TABLE messages (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	role       TEXT NOT NULL,
	payload    TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE TABLE image_captions (image_id TEXT PRIMARY KEY, caption TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE pending_calls (
	session_id TEXT NOT NULL, call_id TEXT NOT NULL, tool_name TEXT NOT NULL,
	arguments TEXT NOT NULL DEFAULT '', dispatched_at TEXT NOT NULL,
	PRIMARY KEY (session_id, call_id)
);
CREATE VIRTUAL TABLE messages_fts USING fts5(session_id UNINDEXED, payload, content='messages', content_rowid='id');
CREATE VIRTUAL TABLE sessions_fts USING fts5(session_id UNINDEXED, label, summary);
CREATE TABLE session_notes (note TEXT);
""")


def sess(sid, label, created, updated, parent=None, messages=()):
	db.execute(
		"INSERT INTO sessions (id, parent_id, label, created_at, updated_at, prompt_tokens,"
		" completion_tokens, pinned, title_state, last_read_at, agent, last_run, model,"
		" provider, workspace, summary, listed, context_start, context_summary)"
		" VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
		(sid, parent, label, created, updated, 11, 22, 1, 2, updated, mark + "-agent",
		 '{"tokens_per_second":12.5,"chunks_per_second":3.5}', mark + "-model",
		 mark + "-provider", mark + "-workspace", mark + "-summary", 1,
		 mark + "-ctxstart", mark + "-ctxsummary"),
	)
	for role, payload, at in messages:
		db.execute("INSERT INTO messages (session_id, role, payload, created_at) VALUES (?,?,?,?)",
		           (sid, role, payload, at))


# the mandatory row: created_at == updated_at, five messages stamped AFTER the row
sess(MANDATORY, mark + "-label-mandatory",
     "2026-10-12T19:45:35.496622592Z", "2026-10-12T19:45:35.496622592Z",
     messages=[
         ("user", '{"role":"user","content":"%s-content-1","timestamp":"2026-10-12T19:45:36Z",'
                  '"unknown_key_%s":"%s-nested","count":3,"flag":true,'
                  '"nested":{"a":["%s-in-list"]}}' % (mark, mark, mark, mark),
          "2026-10-12T19:45:36Z"),
         ("assistant", '{"role":"assistant","content":"%s-content-2","tool_calls":[{"id":"call_1",'
                       '"function":{"name":"shell","arguments":"{\\"cmd\\":\\"%s-tool-arg\\"}"}}]}'
          % (mark, mark), "2026-10-12T19:50:00Z"),
         ("tool", '{"role":"tool","content":"%s-content-3","tool_call_id":"call_1"}' % mark,
          "2026-10-12T19:50:01Z"),
         ("user", '{"role":"user","content":""}', "2026-10-12T23:07:43Z"),
         ("assistant", '{"role":"assistant","content":"%s-content-5"}' % mark,
          "2026-10-12T23:07:44Z"),
     ])
sess("aaaa1111bbbb2222cccc3333dddd4444", mark + "-label-multi",
     "2026-10-14T08:00:00Z", "2026-10-14T09:30:00Z",
     messages=[
         ("user", '{"role":"user","content":"%s-multi-1"}' % mark, "2026-10-14T08:00:00Z"),
         ("assistant", '{"role":"assistant","content":"%s-multi-2"}' % mark, "2026-10-14T08:10:00Z"),
         ("user", '{"role":"user","content":"%s-multi-3"}' % mark, "2026-10-14T08:20:00Z"),
     ])
sess("eeee5555ffff6666aaaa7777bbbb8888", mark + "-label-zero",
     "2026-10-15T07:00:00Z", "2026-10-15T07:00:00Z")
sess("cccc9999dddd0000eeee1111ffff2222", mark + "-label-child",
     "2026-10-14T09:31:00Z", "2026-10-14T09:31:00Z", parent="aaaa1111bbbb2222cccc3333dddd4444")
for i in range(12):
    sid = "%032x" % (0xdead0000 + i)
    sess(sid, "%s-label-fill-%d" % (mark, i),
         "2026-10-%02dT10:00:00Z" % (16 + (i % 8)),
         "2026-10-%02dT11:00:00Z" % (16 + (i % 8)),
         messages=[("user", '{"role":"user","content":"%s-fill-%d"}' % (mark, i),
                    "2026-10-%02dT10:00:00Z" % (16 + (i % 8)))])
db.execute("INSERT INTO image_captions VALUES (?,?,?)",
           ("f" * 64, mark + "-caption", "2026-10-13T00:00:00Z"))
db.execute("INSERT INTO pending_calls VALUES (?,?,?,?,?)",
           (MANDATORY, "call_pending_1", "shell", '{"cmd":"%s-pending-arg"}' % mark,
            "2026-10-12T23:00:00Z"))
db.execute("INSERT INTO session_notes VALUES (?)", (mark + "-derived-note",))
db.execute("INSERT INTO messages_fts(rowid, session_id, payload) SELECT id, session_id, payload FROM messages")
db.execute("INSERT INTO sessions_fts(session_id, label, summary) SELECT id, label, summary FROM sessions")
db.commit()
db.close()
PYSTORE

SRC=$FX/prodlike.db
SRC_N=$FX_N/prodlike.db
OUT=$FX/out
OUT_N=$FX_N/out
$PY "$FX_N/mkstore.py" "$SRC_N" "$MARK" || { echo "store-fixture-extract_test.sh: store builder failed" >&2; exit 1; }
SRC_SHA_BEFORE=$(sha256sum "$SRC" | cut -d' ' -f1)
SRC_BYTES_BEFORE=$(wc -c <"$SRC")

EXTRACT_OUT=$("$PY" "$SUT_N" --source "$SRC_N" --out "$OUT_N" --name legacy-v0.18.0 2>&1)
EXTRACT_RC=$?
FIXTURE=$OUT/legacy-v0.18.0.sqlite
MANIFEST=$OUT/legacy-v0.18.0.MANIFEST.json

# --- B0: the run itself ------------------------------------------------------
chk "B0: extractor exited $EXTRACT_RC - got [$EXTRACT_OUT]" test "$EXTRACT_RC" -eq 0
chk "B0: no fixture emitted" test -f "$FIXTURE"
chk "B0: no manifest emitted" test -f "$MANIFEST"
chk "B0: extractor did not report the regression session - got [$EXTRACT_OUT]" \
	has "$EXTRACT_OUT" "regression:  "

# --- B1: the source was read-only -------------------------------------------
chk "B1: the source bytes changed - the extractor wrote to it" \
	test "$(sha256sum "$SRC" | cut -d' ' -f1)" = "$SRC_SHA_BEFORE"
chk "B1: the source size changed" test "$(wc -c <"$SRC")" = "$SRC_BYTES_BEFORE"
chk "B1: a journal/WAL was left beside the source" \
	test -z "$(ls "$SRC"-wal "$SRC"-shm "$SRC"-journal 2>/dev/null)"

# --- B2: nothing real survived ----------------------------------------------
chk "B2: a source marker survived into the emitted tree" \
	lacks "$(grep -rl -- "$MARK" "$OUT" 2>/dev/null)" "$MARK"
chk "B2: the emitted store carries a source marker" \
	test -z "$(grep -rl -- "$MARK" "$OUT" 2>/dev/null)"

# --- B3: the self-check is green -------------------------------------------
SELF=$("$PY" "$SUT_N" --self-check "$FX_N/out/legacy-v0.18.0.sqlite" 2>&1)
SELF_RC=$?
chk "B3: --self-check exited $SELF_RC - got [$SELF]" test "$SELF_RC" -eq 0
chk "B3: --self-check did not say ok - got [$SELF]" has "$SELF" "self-check: ok"

# --- B4-B9: shapes, compared against the source ----------------------------
cat >"$FX/shapes.py" <<'PYSHAPE'
import json
import re
import sqlite3
import sys

src_path, fix_path, manifest_path, keep = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4].split(",")
KEEP = [t for t in keep]
TS = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z$")
H32 = re.compile(r"^[0-9a-f]{32}$")
MANDATORY = "72863811adb93cd5ca47642c9b8393f6"
problems = []

src = sqlite3.connect(src_path)
src.row_factory = sqlite3.Row
fix = sqlite3.connect(fix_path)
fix.row_factory = sqlite3.Row
manifest = json.load(open(manifest_path, encoding="utf-8"))

# B4 timestamps byte-identical, per row, in the migration's read order
def hash32(v):
    import hashlib
    return hashlib.sha256(v.encode()).hexdigest()[:32]

src_sessions = src.execute("SELECT id, created_at, updated_at FROM sessions ORDER BY created_at, id").fetchall()
fix_by_id = {r["id"]: r for r in fix.execute("SELECT id, created_at, updated_at FROM sessions")}
checked = 0
for r in src_sessions:
    got = fix_by_id.get(hash32(r["id"]))
    if got is None:
        continue
    checked += 1
    if got["created_at"] != r["created_at"] or got["updated_at"] != r["updated_at"]:
        problems.append("B4 timestamp drift on %s: %r/%r vs %r/%r" % (
            r["id"], got["created_at"], got["updated_at"], r["created_at"], r["updated_at"]))
if checked == 0:
    problems.append("B4 no emitted session matched a source session")

# message at/created_at, matched through the hashed session id and the row id
src_msgs = {r["id"]: r for r in src.execute("SELECT id, session_id, created_at FROM messages")}
for m in fix.execute("SELECT id, session_id, created_at FROM messages"):
    s = src_msgs.get(m["id"])
    if s is None:
        problems.append("B5 message id %s has no source row" % m["id"])
        continue
    if s["created_at"] != m["created_at"]:
        problems.append("B4 message %s created_at drift" % m["id"])

# B5 schema preserved
src_tables = {r[0] for r in src.execute("SELECT name FROM sqlite_master WHERE type='table'")}
fix_tables = {r[0] for r in fix.execute("SELECT name FROM sqlite_master WHERE type='table'")}
# sqlite_sequence is rebuilt by SQLite itself for a kept AUTOINCREMENT table
derived = src_tables - set(KEEP) - {"sqlite_sequence"}
if fix_tables != set(KEEP) | ({"sqlite_sequence"} & fix_tables):
    problems.append("B5 fixture tables %s, want %s" % (sorted(fix_tables), sorted(KEEP)))
for leaked in derived:
    if leaked in fix_tables:
        problems.append("B7 derived table %s survived" % leaked)
for t in KEEP:
    if t not in fix_tables:
        continue
    scols = [r[1] for r in src.execute("SELECT * FROM pragma_table_info(?)", (t,))]
    fcols = [r[1] for r in fix.execute("SELECT * FROM pragma_table_info(?)", (t,))]
    if scols != fcols:
        problems.append("B5 columns of %s differ: %s vs %s" % (t, fcols, scols))

# B6 ids: 32 hex, distinct source ids stay distinct
src_ids = [r[0] for r in src.execute("SELECT id FROM sessions")]
fix_ids = [r[0] for r in fix.execute("SELECT id FROM sessions")]
for sid in fix_ids:
    if not H32.match(sid):
        problems.append("B6 session id not 32 hex: %r" % sid)
if len(set(fix_ids)) != len(fix_ids):
    problems.append("B6 hashed session ids collided")
expect = {hash32(s) for s in src_ids}
if not set(fix_ids) <= expect:
    problems.append("B6 an emitted id is not the source id's hash")

# B8 the manifest is a receipt, not content
reg = manifest.get("regression_session")
if not reg:
    problems.append("B8 manifest has no regression_session")
else:
    if reg.get("hashed_id") != hash32(MANDATORY):
        problems.append("B8 mandatory hashed id wrong: %r" % reg.get("hashed_id"))
    if reg.get("created_at") != reg.get("updated_at"):
        problems.append("B8 mandatory stamps are not equal: %r/%r" % (reg.get("created_at"), reg.get("updated_at")))
    if reg.get("created_at") != "2026-10-12T19:45:35.496622592Z":
        problems.append("B8 mandatory created_at not byte-preserved: %r" % reg.get("created_at"))
if not manifest.get("fixture", {}).get("dropped_tables"):
    problems.append("B8 manifest records no dropped tables")
if manifest.get("source", {}).get("row_counts", {}).get("sessions") != len(src_ids):
    problems.append("B8 manifest source row counts are wrong")
if manifest.get("selection", {}).get("emitted_sessions") != len(fix_ids):
    problems.append("B8 manifest emitted_sessions does not match the fixture")
if manifest.get("fixture", {}).get("bytes") is None or manifest["fixture"]["bytes"] > 2 * 1024 * 1024:
    problems.append("B8 fixture byte cap not recorded or exceeded")
if len(fix_ids) > 12:
    problems.append("B8 session cap exceeded: %d" % len(fix_ids))

# B9 structural classes were selected
classes = manifest.get("selection", {}).get("classes", {})
for name in ("equal_stamp", "multi_message", "zero_message"):
    if not classes.get(name):
        problems.append("B9 no %s session in the selection" % name)

for p in problems:
    print(p)
sys.exit(1 if problems else 0)
PYSHAPE

SHAPES=$($PY "$FX_N/shapes.py" "$SRC_N" "$OUT_N/legacy-v0.18.0.sqlite" "$OUT_N/legacy-v0.18.0.MANIFEST.json" "sessions,messages,image_captions,pending_calls" 2>&1)
SHAPES_RC=$?
chk "B4-B9: shape assertions failed - got [$SHAPES]" test "$SHAPES_RC" -eq 0

# --- B10: teeth - disable the scrubber and watch the gate go red -----------
# The copy is patched before main() runs, so the override is what every call site
# resolves to (a trailing redefinition would land after the __main__ guard).
cat >"$FX/patch.py" <<'PYPATCH'
import pathlib
import sys

sut, out = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2])
src = sut.read_text()
needle = "def main(argv) -> int:"
if needle not in src:
    sys.exit("patch.py: main() not found in the sanitizer")
out.write_text(src.replace(needle, "def sanitize_text(value):\n    return value\n\n\n" + needle, 1))
PYPATCH
UNSCRUBBED=$FX/unscrubbed.py
UNSCRUBBED_N=$FX_N/unscrubbed.py
$PY "$FX_N/patch.py" "$SUT_N" "$UNSCRUBBED_N" || bad "B10: could not build the disabled-scrubber copy"
$PY "$UNSCRUBBED_N" --source "$SRC_N" --out "$FX_N/out-bad" --name legacy-v0.18.0 >/dev/null 2>&1
BAD_FIXTURE=$FX/out-bad/legacy-v0.18.0.sqlite
BAD_FIXTURE_N=$FX_N/out-bad/legacy-v0.18.0.sqlite
LEAK=$(grep -rl -- "$MARK" "$FX/out-bad" 2>/dev/null)
chk "B10: disabling the scrubber leaked NO marker - the markers do not witness the scrub" \
	test -n "$LEAK"
BAD_CHECK=$("$PY" "$SUT_N" --self-check "$BAD_FIXTURE_N" 2>&1)
BAD_CHECK_RC=$?
chk "B10: --self-check stayed green on an unscrubbed fixture" test "$BAD_CHECK_RC" -ne 0
chk "B10: --self-check did not name the leaking column - got [$BAD_CHECK]" \
	has "$BAD_CHECK" "not a filler"
chk "B10: --self-check did not name the leaking session label" \
	has "$BAD_CHECK" "-label-mandatory"

echo "store-fixture-extract_test.sh: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
