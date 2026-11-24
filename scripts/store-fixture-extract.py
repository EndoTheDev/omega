#!/usr/bin/env python3
"""store-fixture-extract.py - turn a real pre-JSONL store into a committable fixture.

The legacy migration is only exercised by a store that HAS a sessions table and
LACKS the session_files watermark (harness/store/legacy.go). Every store CI ever
built was fresh, so no green run had executed that path - and a gate that never
ran proves nothing about the shape it never read. This tool produces that shape
from a real store, with the content replaced and the structure kept.

Sanitize by shape, never by deletion:

  ids            -> sha256 truncated to 32 hex (a session id's own shape)
  created_at, updated_at, at, dispatched_at, last_read_at -> byte-preserved
  message payloads, last_run, arguments -> parsed as JSON; every string leaf
                  becomes a same-length filler, unknown keys k_<hash8>
  every other text column -> same-length filler (empty stays empty)
  integers -> untouched

Tables kept: sessions, messages (required - isLegacyStore keys on a sessions
table), plus image_captions and pending_calls when present (legacyReadCarry).
Everything derived from the store (messages_fts*, sessions_fts*, sqlite_sequence,
triggers) is dropped: the migration never reads it and it is most of the file.

  store-fixture-extract.py --source <db> --out <dir> [--name legacy-v0.18.0]
  store-fixture-extract.py --self-check <fixture.sqlite>

--self-check re-reads an emitted fixture and exits nonzero on ANY value whose
shape is not a timestamp, a hashed id/key, a known JSON key, an enum, or a
filler. That is the machine proof that nothing real left.
"""

import argparse
import hashlib
import json
import re
import sqlite3
import sys
from pathlib import Path

SCHEMA_VERSION = 1

# The one session this whole exercise exists for: created_at == updated_at,
# five messages, never refreshed after creation. It must be in every fixture.
MANDATORY_SESSION = "72863811adb93cd5ca47642c9b8393f6"

MAX_SESSIONS = 12
MAX_BYTES = 2 * 1024 * 1024
MAX_CAPTIONS = 50

KEEP_TABLES = ("sessions", "messages", "image_captions", "pending_calls")
# sqlite_sequence is written by SQLite itself for a kept AUTOINCREMENT table.
ALLOWED_TABLES = KEEP_TABLES + ("sqlite_sequence",)

TIMESTAMP_RE = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$")
HASHED_ID_RE = re.compile(r"^[0-9a-f]{32}$")
HASHED_KEY_RE = re.compile(r"^k_[0-9a-f]{8}$")
FILLER_RE = re.compile(r"^x+$")

# Structural enums, kept as-is: they are vocabulary, not content.
ROLES = frozenset({"user", "assistant", "system", "tool", "developer", "function", "tool_result"})

# Timestamp columns keep exactly what the row held.
TIMESTAMP_COLUMNS = frozenset({"created_at", "updated_at", "at", "dispatched_at", "last_read_at"})
# Identity columns become a 32-hex hash. A row's integer primary key is an int and
# returns before this table is consulted.
ID_COLUMNS = frozenset({"id", "parent_id", "delegate_parent", "session_id", "image_id", "call_id"})
JSON_COLUMNS = frozenset({"payload", "last_run", "arguments"})
ENUM_COLUMNS = {"role": ROLES}

# Every key the agent writes into a message payload (core/ json tags) plus the
# provider wire shapes. A key outside this set is hashed, so distinct keys stay
# distinct without carrying their name.
KNOWN_KEYS = frozenset({
    "action", "actions", "active", "agent", "agents", "allow_text", "answer_ms", "args",
    "arguments", "at", "attachments", "base64", "blocks", "body", "branch", "cached_count",
    "call", "capabilities", "card_id", "carrier", "cache_control", "chunks_per_second",
    "completion_tokens", "content", "context_start", "context_summary", "count",
    "created_at", "db_size_bytes", "default", "default_option", "delegate_parent",
    "dependents", "description", "elapsed_ms", "end", "error", "eval_count", "expired",
    "filter", "finish_reason", "from", "function", "gate", "groups", "help", "id", "images",
    "index", "input_tokens", "is_error", "keep", "key", "kind", "knobs", "label",
    "last_read_at", "last_run", "level", "live", "machine_owned", "matches", "max",
    "media_type", "message", "message_count", "message_id", "mime_type", "min", "model",
    "model_name", "module", "multi", "name", "option", "option_index", "option_indexes",
    "options", "output_tokens", "overridden", "panel", "parameters", "parent_id", "path",
    "payload", "pending_calls", "pinned", "prompt", "prompt_eval_count", "prompt_tokens",
    "provider", "provider_name", "provides", "read", "requires", "response_ms", "role",
    "rows", "running", "scopes", "session", "session_id", "siblings", "size", "skills",
    "skipped", "slot", "snippet", "source_id", "sources", "start", "started_at", "state",
    "status", "step", "stop_reason", "summary", "system", "text", "thinking", "thinking_ms",
    "tier", "time", "timeout", "timeout_s", "timestamp", "timing", "title", "title_state",
    "to", "toggleable", "tokens_per_second", "tool_call", "tool_call_id", "tool_calls",
    "tool_count", "tool_name", "tools", "turn", "turns", "type", "updated_at", "url",
    "usage", "value", "verdict", "wal_size_bytes", "workspace",
})


def digest32(value: str) -> str:
    return hashlib.sha256(value.encode("utf-8")).hexdigest()[:32]


def hash_id(value):
    if value is None or value == "":
        return value
    return digest32(value)


def sanitize_text(value):
    """The one door every content string goes through (the --self-check seam)."""
    if value is None or value == "":
        return value
    return "x" * len(value)


def sanitize_key(key: str) -> str:
    if key in KNOWN_KEYS:
        return key
    return "k_" + hashlib.sha256(key.encode("utf-8")).hexdigest()[:8]


def scrub_json_value(value):
    if isinstance(value, dict):
        return {sanitize_key(k): scrub_json_value(v) for k, v in value.items()}
    if isinstance(value, list):
        return [scrub_json_value(v) for v in value]
    if isinstance(value, str):
        return sanitize_text(value)
    return value


def scrub_json_text(text: str):
    """Sanitize a JSON document, keeping it valid JSON. Non-JSON text is scrubbed whole."""
    try:
        parsed = json.loads(text)
    except (ValueError, TypeError):
        return sanitize_text(text)
    # compact, ASCII-only: the migration writes payloads back as RawMessage and its
    # read-back compares bytes, so the emitted text must survive a JSON round trip
    return json.dumps(scrub_json_value(parsed), separators=(",", ":"), ensure_ascii=True)


def source_uri(path: Path) -> str:
    # immutable=1: sqlite may never take a lock, touch a WAL, or write a journal on
    # the source. This runs against the only surviving copy of a real store.
    return path.resolve().as_uri() + "?mode=ro&immutable=1"


def table_names(db) -> list:
    rows = db.execute("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name").fetchall()
    return [r[0] for r in rows]


def column_names(db, table: str) -> list:
    return [r[1] for r in db.execute(
        "SELECT * FROM pragma_table_info('%s')" % table.replace("'", "''"))]


def table_ddl(db, table: str):
    row = db.execute("SELECT sql FROM sqlite_master WHERE type='table' AND name=?",
                     (table,)).fetchone()
    return row[0] if row else None


def count_rows(db, table: str) -> int:
    try:
        return db.execute("SELECT COUNT(*) FROM '%s'" % table.replace("'", "''")).fetchone()[0]
    except sqlite3.Error:
        return 0


def sanitize_column_value(column: str, value):
    """One cell, sanitized by the shape its column carries."""
    if value is None:
        return None
    if isinstance(value, (bytes, bytearray)):
        # A blob is content: keep the type and the size, drop the bytes.
        return b"\x00" * len(value)
    if isinstance(value, int):
        return value
    if not isinstance(value, str):
        return value
    if value == "":
        return value
    if column in TIMESTAMP_COLUMNS and TIMESTAMP_RE.match(value):
        return value
    if column in JSON_COLUMNS:
        return scrub_json_text(value)
    if column in ENUM_COLUMNS and value in ENUM_COLUMNS[column]:
        return value
    if column in ID_COLUMNS:
        return hash_id(value)
    return sanitize_text(value)


def session_classes(rows, message_counts) -> dict:
    """Which structural class each session belongs to (a session can be several)."""
    classes = {}
    for row in rows:
        tags = []
        if row["updated_at"] == row["created_at"]:
            tags.append("updated_at_equals_created_at")
        n = message_counts.get(row["id"], 0)
        if n == 0:
            tags.append("zero_message")
        elif n >= 3:
            tags.append("multi_message")
        classes[row["id"]] = tags
    return classes


def select_sessions(rows, messages_by_session, classes):
    """Structural classes, not recency: the equal-stamp class (mandatory row first),
    a multi-message session, a zero-message session when one exists, then fill."""
    by_id = {r["id"]: r for r in rows}
    chosen, reasons = [], {}

    def take(sid, why):
        if sid in by_id and sid not in chosen:
            chosen.append(sid)
            reasons[sid] = why

    if MANDATORY_SESSION in by_id:
        take(MANDATORY_SESSION, "mandatory: v0.18.0 boot-kill")
    # the equal-stamp class: every row of it the cap allows, oldest first
    for r in rows:
        if "updated_at_equals_created_at" in classes[r["id"]]:
            take(r["id"], "equal_stamp")
    # one multi-message session, the richest one
    multi = sorted(
        (r for r in rows if "multi_message" in classes[r["id"]]),
        key=lambda r: (-len(messages_by_session.get(r["id"], [])), r["created_at"], r["id"]),
    )
    if multi:
        take(multi[0]["id"], "multi_message")
    # one zero-message session, when the store ever had one
    zero = sorted((r for r in rows if "zero_message" in classes[r["id"]]),
                  key=lambda r: (r["created_at"], r["id"]))
    if zero:
        take(zero[0]["id"], "zero_message")
    # fill: recent sessions, so the fixture keeps a live-looking tail
    for r in sorted(rows, key=lambda r: (r["created_at"], r["id"]), reverse=True):
        if len(chosen) >= MAX_SESSIONS:
            break
        take(r["id"], "recent")

    # A selected child whose parent is missing keeps a dangling reference the
    # migration only ever compares as a string; pull the parent in when it fits.
    for sid in list(chosen):
        parent = by_id[sid].get("parent_id") or ""
        if parent and parent in by_id and parent not in chosen and len(chosen) < MAX_SESSIONS:
            take(parent, "parent_closure")
    return chosen, reasons


def emit(source: Path, out_dir: Path, name: str) -> int:
    if not source.is_file():
        print("store-fixture-extract: source not found: %s" % source, file=sys.stderr)
        return 2
    out_dir.mkdir(parents=True, exist_ok=True)
    fixture = out_dir / (name + ".sqlite")
    manifest_path = out_dir / (name + ".MANIFEST.json")
    if fixture.exists():
        fixture.unlink()

    src = sqlite3.connect(source_uri(source), uri=True)
    src.row_factory = sqlite3.Row
    present = table_names(src)
    if "sessions" not in present:
        print("store-fixture-extract: %s has no sessions table - not a legacy store" % source,
              file=sys.stderr)
        return 2

    kept = [t for t in KEEP_TABLES if t in present]
    dropped = [t for t in present if t not in kept]
    src_counts = {t: count_rows(src, t) for t in present}

    # messages grouped by session, in the migration's own read order (ORDER BY id)
    messages_by_session, message_sizes = {}, {}
    if "messages" in present:
        for row in src.execute("SELECT * FROM messages ORDER BY id"):
            messages_by_session.setdefault(row["session_id"], []).append(dict(row))
            message_sizes[row["session_id"]] = message_sizes.get(row["session_id"], 0) + len(
                row["payload"] or "")

    sessions = [dict(r) for r in src.execute("SELECT * FROM sessions ORDER BY created_at, id")]
    by_id = {r["id"]: r for r in sessions}
    counts = {sid: len(msgs) for sid, msgs in messages_by_session.items()}
    classes = session_classes(sessions, counts)
    chosen, reasons = select_sessions(sessions, messages_by_session, classes)

    # the byte cap: walk the selection in order, skip what would overflow
    emitted, bytes_est, dropped_for_cap = [], 0, 0
    for sid in chosen:
        cost = message_sizes.get(sid, 0) + 512
        if emitted and bytes_est + cost > MAX_BYTES:
            dropped_for_cap += 1
            continue
        emitted.append(sid)
        bytes_est += cost
    emitted_set = set(emitted)

    dest = sqlite3.connect(str(fixture))
    try:
        # Schema first: the kept tables' own DDL, then their indexes. Triggers go -
        # they belong to the derived index (fts writes) and this file is read-only
        # truth for the migration.
        for table in kept:
            ddl = table_ddl(src, table)
            if ddl:
                dest.execute(ddl)
        for table in kept:
            for (sql,) in src.execute(
                "SELECT sql FROM sqlite_master WHERE type='index' AND tbl_name=? AND sql IS NOT NULL",
                (table,),
            ):
                dest.execute(sql)

        def insert(table, cols, values):
            dest.execute(
                "INSERT INTO '%s' (%s) VALUES (%s)"
                % (table, ", ".join("'%s'" % c for c in cols), ", ".join("?" * len(cols))),
                values,
            )

        session_cols = column_names(src, "sessions")
        for sid in emitted:
            insert("sessions", session_cols,
                   [sanitize_column_value(c, by_id[sid][c]) for c in session_cols])
        if "messages" in present:
            msg_cols = column_names(src, "messages")
            for sid in emitted:
                for msg in messages_by_session.get(sid, []):
                    insert("messages", msg_cols,
                           [sanitize_column_value(c, msg[c]) for c in msg_cols])
        if "image_captions" in present:
            cols = column_names(src, "image_captions")
            for i, row in enumerate(src.execute("SELECT * FROM image_captions")):
                if i >= MAX_CAPTIONS:
                    break
                insert("image_captions", cols,
                       [sanitize_column_value(c, row[c]) for c in cols])
        if "pending_calls" in present:
            cols = column_names(src, "pending_calls")
            for row in src.execute("SELECT * FROM pending_calls"):
                if row["session_id"] not in emitted_set:
                    continue
                insert("pending_calls", cols,
                       [sanitize_column_value(c, row[c]) for c in cols])
        dest.commit()
        dest_counts = {t: count_rows(dest, t) for t in kept}
    finally:
        dest.close()

    source_sha = hashlib.sha256(source.read_bytes()).hexdigest()
    fixture_sha = hashlib.sha256(fixture.read_bytes()).hexdigest()

    regression = None
    if MANDATORY_SESSION in by_id:
        row = by_id[MANDATORY_SESSION]
        regression = {
            "source_id": MANDATORY_SESSION,
            "hashed_id": hash_id(MANDATORY_SESSION),
            "created_at": row["created_at"],
            "updated_at": row["updated_at"],
            "equal": row["created_at"] == row["updated_at"],
            "messages": len(messages_by_session.get(MANDATORY_SESSION, [])),
            "class": "updated_at_equals_created_at",
            "emitted": MANDATORY_SESSION in emitted_set,
        }

    manifest = {
        "tool": "store-fixture-extract.py",
        "schema_version": SCHEMA_VERSION,
        "source": {
            "file": source.name,
            "sha256": source_sha,
            "bytes": source.stat().st_size,
            "tables": sorted(present),
            "row_counts": src_counts,
        },
        "fixture": {
            "file": fixture.name,
            "sha256": fixture_sha,
            "bytes": fixture.stat().st_size,
            "tables": kept,
            "dropped_tables": sorted(dropped),
            "row_counts": dest_counts,
        },
        "selection": {
            "by": "structural class, not recency",
            "cap_sessions": MAX_SESSIONS,
            "cap_bytes": MAX_BYTES,
            "source_sessions": len(sessions),
            "emitted_sessions": len(emitted),
            "dropped_sessions": len(sessions) - len(emitted),
            "dropped_for_byte_cap": dropped_for_cap,
            "classes": {
                "equal_stamp": sum(1 for s in emitted
                                   if "updated_at_equals_created_at" in classes[s]),
                "multi_message": sum(1 for s in emitted if "multi_message" in classes[s]),
                "zero_message": sum(1 for s in emitted if "zero_message" in classes[s]),
            },
        },
        "regression_session": regression,
        "sessions": [
            {
                "id": hash_id(sid),
                "why": reasons.get(sid, "recent"),
                "classes": classes[sid],
                "created_at": by_id[sid]["created_at"],
                "updated_at": by_id[sid]["updated_at"],
                "messages": len(messages_by_session.get(sid, [])),
            }
            for sid in emitted
        ],
    }
    manifest_path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    src.close()

    print("source:      %s (%d bytes, %d sessions, %d messages)"
          % (source.name, manifest["source"]["bytes"], src_counts.get("sessions", 0),
             src_counts.get("messages", 0)))
    print("fixture:     %s (%d bytes, %d sessions, %d messages, tables: %s)"
          % (fixture, manifest["fixture"]["bytes"], dest_counts.get("sessions", 0),
             dest_counts.get("messages", 0), ", ".join(kept)))
    print("dropped:     %d sessions, %d tables (%s)"
          % (len(sessions) - len(emitted), len(dropped), ", ".join(sorted(dropped)) or "none"))
    if regression:
        print("regression:  %s created_at=%s updated_at=%s (%d messages)"
              % (regression["hashed_id"], regression["created_at"], regression["updated_at"],
                 regression["messages"]))
    else:
        print("regression:  ABSENT - the mandatory session %s was not in the source"
              % MANDATORY_SESSION, file=sys.stderr)
    print("manifest:    %s" % manifest_path)
    return 0


def check_json_shape(where: str, text: str, problems: list) -> None:
    try:
        parsed = json.loads(text)
    except ValueError:
        problems.append("%s: not JSON: %r" % (where, text[:40]))
        return

    def walk(node, path):
        if isinstance(node, dict):
            for k, v in node.items():
                if not (k in KNOWN_KEYS or HASHED_KEY_RE.match(k)):
                    problems.append("%s.%s: key is neither known vocabulary nor k_<hash8>"
                                    % (path, k))
                walk(v, "%s.%s" % (path, k))
        elif isinstance(node, list):
            for i, v in enumerate(node):
                walk(v, "%s[%d]" % (path, i))
        elif isinstance(node, str):
            if node and not FILLER_RE.match(node):
                problems.append("%s: string is not a filler: %r" % (path, node[:40]))

    walk(parsed, where)


def self_check(path_str: str) -> int:
    path = Path(path_str)
    if path.is_dir():
        candidates = sorted(path.glob("*.sqlite"))
        if not candidates:
            print("self-check: no .sqlite under %s" % path, file=sys.stderr)
            return 2
        path = candidates[0]
    if not path.is_file():
        print("self-check: no such fixture: %s" % path, file=sys.stderr)
        return 2
    db = sqlite3.connect(path.resolve().as_uri() + "?mode=ro", uri=True)
    db.row_factory = sqlite3.Row
    problems = []
    tables = table_names(db)
    for table in tables:
        if table not in ALLOWED_TABLES:
            problems.append("table %s: derived or unknown table survived the extraction" % table)
    if "sessions" not in tables:
        problems.append("no sessions table - the migration would never run (isLegacyStore)")
    if "session_files" in tables:
        problems.append("session_files: the post-migration watermark must not be in the fixture")
    session_ids = set()
    for row in db.execute("SELECT id FROM sessions"):
        sid = row["id"]
        if not isinstance(sid, str) or not HASHED_ID_RE.match(sid):
            problems.append("sessions.id: not a 32-hex hashed id: %r" % (sid,))
        session_ids.add(sid)
    for table in [t for t in KEEP_TABLES if t in tables]:
        cols = column_names(db, table)
        for row in db.execute("SELECT * FROM '%s'" % table):
            for col in cols:
                value = row[col]
                where = "%s.%s" % (table, col)
                if value is None:
                    continue
                if isinstance(value, (bytes, bytearray)):
                    if any(value):
                        problems.append("%s: blob carries non-zero bytes" % where)
                    continue
                if isinstance(value, int):
                    continue
                if not isinstance(value, str):
                    problems.append("%s: unexpected type %s" % (where, type(value).__name__))
                    continue
                if value == "":
                    continue
                if col in TIMESTAMP_COLUMNS:
                    if not TIMESTAMP_RE.match(value):
                        problems.append("%s: not a timestamp: %r" % (where, value[:40]))
                    continue
                if col in JSON_COLUMNS:
                    check_json_shape(where, value, problems)
                    continue
                if col in ENUM_COLUMNS:
                    if value not in ENUM_COLUMNS[col]:
                        problems.append("%s: enum value outside %s: %r"
                                        % (where, sorted(ENUM_COLUMNS[col]), value[:40]))
                    continue
                if col in ID_COLUMNS:
                    if not HASHED_ID_RE.match(value):
                        problems.append("%s: not a 32-hex hashed id: %r" % (where, value[:40]))
                    continue
                if not FILLER_RE.match(value):
                    problems.append("%s: not a filler: %r" % (where, value[:60]))
    if "messages" in tables:
        for row in db.execute("SELECT session_id FROM messages"):
            if row["session_id"] not in session_ids:
                problems.append("messages.session_id: no such session %r" % (row["session_id"],))
    if problems:
        print("self-check: FAIL - %d problem(s) in %s" % (len(problems), path), file=sys.stderr)
        for p in problems[:40]:
            print("  " + p, file=sys.stderr)
        return 1
    print("self-check: ok - %s carries no value outside the sanitized shapes" % path)
    return 0


def main(argv) -> int:
    ap = argparse.ArgumentParser(description="sanitized legacy-store fixture extractor")
    ap.add_argument("--source", help="the pre-JSONL store to read (never written)")
    ap.add_argument("--out", help="scratch dir for the .sqlite + MANIFEST.json")
    ap.add_argument("--name", default="legacy-v0.18.0", help="fixture basename")
    ap.add_argument("--self-check", metavar="FIXTURE", help="verify an emitted fixture")
    args = ap.parse_args(argv)
    if args.self_check:
        return self_check(args.self_check)
    if not args.source or not args.out:
        ap.print_usage(sys.stderr)
        print("store-fixture-extract: --source and --out are required (or --self-check)",
              file=sys.stderr)
        return 2
    return emit(Path(args.source), Path(args.out), args.name)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
