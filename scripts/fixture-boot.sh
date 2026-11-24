#!/usr/bin/env bash
set -uo pipefail

# fixture-boot.sh - boot a release binary against a real-shape legacy store fixture.
#
# The migration this gates is only exercised by a store that HAS a sessions table and
# LACKS the session_files watermark, so a fresh-lane CI run never reaches it. This
# boots a binary over a sanitized copy of a real store and requires: health at the
# expected version, a green migration (the source archived, the session files
# written), and the known-broken record intact - the session whose created_at and
# updated_at are the same value must still read back equal.
#
#   scripts/fixture-boot.sh --fixture <f.sqlite> --manifest <f.MANIFEST.json> \
#                           --bin <omega> --version <tag>
#
# The fixture is COPIED into a throwaway home first: the migration archives the
# source aside and writes files, so the committed fixture must never be booted in
# place.
#
# Env seams (the suite drives the timeout with them):
#   FIXTURE_BOOT_PORT     default 18099  (the lane port; /health never calls the provider)
#   FIXTURE_BOOT_TIMEOUT  default 60     (seconds the health poll may take)
#   FIXTURE_BOOT_KEEP     default 0      (1 keeps the temp home for a post-mortem)
#
# Exit: 0 green, 1 red (the log tail is printed), 2 usage/setup error.

PORT=${FIXTURE_BOOT_PORT:-18099}
TIMEOUT=${FIXTURE_BOOT_TIMEOUT:-60}
KEEP=${FIXTURE_BOOT_KEEP:-0}
PY=python3

FIXTURE=""
MANIFEST=""
BIN=""
VERSION=""

usage() {
	cat >&2 <<'USAGE'
usage: scripts/fixture-boot.sh --fixture <f.sqlite> --manifest <f.MANIFEST.json> \
                              --bin <omega> --version <tag>
USAGE
}

die() {
	printf '\nfixture-boot.sh: FAIL: %s\n' "$1" >&2
	if [ $# -gt 1 ]; then
		printf 'fixture-boot.sh: %s\n' "$2" >&2
	fi
	exit 1
}

while [ $# -gt 0 ]; do
	case $1 in
	--fixture) FIXTURE=${2:-}; shift 2 ;;
	--manifest) MANIFEST=${2:-}; shift 2 ;;
	--bin) BIN=${2:-}; shift 2 ;;
	--version) VERSION=${2:-}; shift 2 ;;
	-h | --help) usage; exit 0 ;;
	*) usage; die "unknown argument: $1" ;;
	esac
done

[ -n "$FIXTURE" ] && [ -n "$MANIFEST" ] && [ -n "$BIN" ] && [ -n "$VERSION" ] || {
	usage
	die "all of --fixture --manifest --bin --version are required"
}
[ -f "$FIXTURE" ] || die "no fixture at $FIXTURE"
[ -f "$MANIFEST" ] || die "no manifest at $MANIFEST"
[ -x "$BIN" ] || die "binary is not executable: $BIN"
command -v "$PY" >/dev/null 2>&1 || die "$PY not found - the manifest and the session header are JSON"

# The manifest is the receipt: it names the hashed id and the stamps the boot must
# reproduce. Never recompute the expectation from the fixture - the receipt is what
# a reviewer read.
EXPECT=$("$PY" - "$MANIFEST" <<'PYMANIFEST'
import json
import sys

m = json.load(open(sys.argv[1], encoding="utf-8"))
reg = m.get("regression_session") or {}
print("%s\t%s\t%s" % (reg.get("hashed_id", ""), reg.get("created_at", ""),
                      reg.get("updated_at", "")))
PYMANIFEST
) || die "could not read $MANIFEST"
HASHED_ID=$(printf '%s' "$EXPECT" | cut -f1)
EXPECT_CREATED=$(printf '%s' "$EXPECT" | cut -f2)
EXPECT_UPDATED=$(printf '%s' "$EXPECT" | cut -f3)
[ -n "$HASHED_ID" ] || die "$MANIFEST names no regression session (regression_session.hashed_id is empty)" \
	"the fixture was extracted from a store without the v0.18.0 row - refuse to boot it as a gate"
[ "$EXPECT_CREATED" = "$EXPECT_UPDATED" ] || die "$MANIFEST's regression session no longer carries equal stamps: $EXPECT_CREATED / $EXPECT_UPDATED"

HOME_DIR=$(mktemp -d "${TMPDIR:-/tmp}/fixture-boot.XXXXXX") || die "could not make a temp home"
# A path handed to a NATIVE tool (the binary, python3) cannot be an MSYS /tmp path:
# on the Windows host that resolves to 	mp\... and the home looks absent.
nat() {
	if command -v cygpath >/dev/null 2>&1; then cygpath -m "$1"; else printf '%s' "$1"; fi
}
HOME_N=$(nat "$HOME_DIR")
LOG=$HOME_DIR/boot.log
PID=""
cleanup() {
	if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
		kill "$PID" 2>/dev/null
		wait "$PID" 2>/dev/null
	fi
	if [ "$KEEP" = 1 ]; then
		printf 'fixture-boot.sh: kept %s\n' "$HOME_DIR" >&2
	else
		rm -rf "$HOME_DIR"
	fi
}
trap cleanup EXIT INT TERM

# 1. a throwaway home carrying a COPY of the fixture
mkdir -p "$HOME_DIR/config" "$HOME_DIR/data" || die "could not lay out $HOME_DIR"
cp "$FIXTURE" "$HOME_DIR/data/omega.db" || die "could not copy the fixture into $HOME_DIR"
# The lane shape the nightly proves: a fresh home, the minimal config (serve refuses
# a home without config.yaml), dummy provider values so nothing reaches a model.
cat >"$HOME_DIR/config/config.yaml" <<'LANE_CONFIG'
provider:
  type: ollama
  model_name: dummy
  host: http://127.0.0.1:9
LANE_CONFIG
printf '[1/4] home: %s (fixture copied to data/omega.db)\n' "$HOME_DIR"

# 2. boot the binary against the copy
OMEGA_HOME="$HOME_N" OMEGA_PORT="$PORT" "$BIN" serve >"$LOG" 2>&1 &
PID=$!
printf '[2/4] boot: %s serve (pid %s, port %s)\n' "$BIN" "$PID" "$PORT"

# 3. health: status ok AND the version this binary was built to be
deadline=$((SECONDS + TIMEOUT))
READY=0
LAST=""
while :; do
	BODY=$(curl -fsS -m 3 "http://127.0.0.1:$PORT/health" 2>/dev/null) || BODY=""
	if [ -n "$BODY" ]; then
		LAST=$BODY
		status=$(printf '%s' "$BODY" | sed -n 's/.*"status"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
		got=$(printf '%s' "$BODY" | sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
		if [ "$status" = ok ] && [ "$got" = "$VERSION" ]; then
			READY=1
			break
		fi
	fi
	if [ "$SECONDS" -ge "$deadline" ]; then
		break
	fi
	sleep 1
done
if [ "$READY" -ne 1 ]; then
	printf '\n--- boot log (%s) ---\n' "$LOG" >&2
	tail -40 "$LOG" >&2 || true
	die "health never answered ok at $VERSION within ${TIMEOUT}s - last body: ${LAST:-<no answer>}"
fi
printf '[3/4] health: ok at %s\n' "$VERSION"

# 4. the migration's receipts: the source archived, the known-broken record intact
if ! ls "$HOME_DIR"/data/omega.db.*.bak >/dev/null 2>&1; then
	printf '\n--- boot log (%s) ---\n' "$LOG" >&2
	tail -40 "$LOG" >&2 || true
	die "the legacy store was never archived - the migration did not run (isLegacyStore false?)"
fi
SESSION_FILE=$HOME_DIR/data/sessions/$HASHED_ID.jsonl
if [ ! -f "$SESSION_FILE" ]; then
	die "no session file for the regression session: $SESSION_FILE" \
		"the migration is green but the record this gate exists for is missing"
fi

"$PY" - "$(nat "$SESSION_FILE")" "$HASHED_ID" "$EXPECT_CREATED" "$EXPECT_UPDATED" <<'PYHEADER'
import json
import sys

path, want_id, want_created, want_updated = sys.argv[1:5]
with open(path, encoding="utf-8") as f:
    header = json.loads(f.readline())
problems = []
if header.get("kind") != "session":
    problems.append("first line is not the session header: kind=%r" % header.get("kind"))
if header.get("session") != want_id:
    problems.append("header session %r, want %r" % (header.get("session"), want_id))
if header.get("created_at") != want_created:
    problems.append("header created_at %r, want %r" % (header.get("created_at"), want_created))
if header.get("updated_at") != want_updated:
    problems.append("header updated_at %r, want %r" % (header.get("updated_at"), want_updated))
if header.get("created_at") != header.get("updated_at"):
    problems.append("header created_at %r != updated_at %r - the v0.18.0 divergence is back"
                    % (header.get("created_at"), header.get("updated_at")))
if problems:
    for p in problems:
        print("fixture-boot.sh: %s" % p, file=sys.stderr)
    sys.exit(1)
PYHEADER
if [ $? -ne 0 ]; then
	printf '\n--- boot log (%s) ---\n' "$LOG" >&2
	tail -40 "$LOG" >&2 || true
	die "the migrated session header does not carry the source row's stamps"
fi
printf '[4/4] migration: %s jsonl carries updated_at == created_at == %s\n' "$HASHED_ID" "$EXPECT_CREATED"

printf 'fixture-boot: GREEN - %s booted %s over the fixture and migrated it intact\n' "$VERSION" "$BIN"
exit 0
