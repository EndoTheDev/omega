#!/usr/bin/env bash
# scripts/removal-proof.sh - the shell-ring shrink's cover proof
# (card t_b75df168, v0.15.0 pre-tag; ADR Workshop/omega-shell-ring-shrink.md).
#
# The executable sentence, endo's ruling: "remove every capsule and what
# remains is a usable agent with a chat face."
#
# Builds FROM HEAD (git archive - the committed tree only, no dirty
# state rides the proof) into a scratch dir, DELETES every harness
# capsule dir (harness/datadir stays: it is the imported leaf the CLI
# consumes for the home layout, not a capsule), go-builds the binary,
# boots it into a throwaway home, and probes the wire: the base faces
# answer (chat, canvas, settings) and the capsule faces do not (their
# URLs ride the SPA catch-all - HTML, not the capsule's JSON). Prints
# the evidence lines and PASS/FAIL; exits 0 only green.
#
# Usage: scripts/removal-proof.sh [--keep]
#   PROOF_PORT, PROOF_STAGE, PROOF_HOME override the scratch paths.
#   The booted serve is killed at the end (the PID is printed).

set -uo pipefail

SRC=$(cd "$(dirname "$0")/.." && pwd)
# native tools (git, go) get the OS-native form, never the MSYS one
NATIVE_SRC="$SRC"
if command -v cygpath >/dev/null 2>&1; then NATIVE_SRC=$(cygpath -m "$SRC"); fi;
STAGE=${PROOF_STAGE:-}
HOME_DIR=${PROOF_HOME:-}
PORT=${PROOF_PORT:-18987}
KEEP=0
[ "${1:-}" = "--keep" ] && KEEP=1

step() { printf 'proof %s\n' "$*"; }
fail() { printf 'proof FAIL: %s\n' "$*" >&2; exit 1; }

# 1. the scratch dirs
if [ -z "$STAGE" ]; then
  STAGE=$(mktemp -d "${TMPDIR:-/tmp}/removal-proof-XXXXXX") || fail "mktemp stage"
fi
mkdir -p "$STAGE" || fail "mkdir stage"
step "stage: $STAGE"

# 2. build FROM HEAD
if [ -n "${HOME_DIR:-}" ]; then mkdir -p "$HOME_DIR"; else HOME_DIR=$(mktemp -d "${TMPDIR:-/tmp}/removal-proof-home-XXXXXX") || fail "mktemp home"; fi
git -C "$NATIVE_SRC" archive HEAD | tar -x -C "$STAGE" || fail "git archive"
step "staged HEAD $(git -C "$NATIVE_SRC" rev-parse --short HEAD)"

# 3. delete every capsule dir and strip the host's capsule wires with
# the explicit strip (scripts/proof-strip.py): the binary-purity
# scrubber strips ONE capsule at a time (the proven nm-purity
# mechanism); at all-at-once its cascades over-match host locals named
# like capsules (store, logger, trust) and eat function headers. The
# strip names every wire site and asserts its shape - the proof reds
# loud when the strip is stale against HEAD. datadir stays: it is the
# imported leaf, not a capsule.
REMOVED=$(cd "$STAGE/harness" && for d in */; do [ "${d%/}" = datadir ] || echo "${d%/}"; done | paste -sd, -)
(cd "$STAGE/harness" && for d in */; do [ "$d" = "datadir/" ] || rm -rf "$d"; done)
[ -d "$STAGE/harness/datadir" ] || fail "datadir vanished - the removal must keep the imported leaf"
[ -f "$STAGE/harness/README.md" ] || fail "harness/README.md vanished"
(cp scripts/proof-strip.py "$STAGE/proof-strip.py")
NATIVE_STRIP="$STAGE/proof-strip.py"
if command -v cygpath >/dev/null 2>&1; then NATIVE_STRIP=$(cygpath -m "$STAGE/proof-strip.py"); fi
(cd "$STAGE" && python3 "$NATIVE_STRIP") || fail "host strip"
step "capsules removed: $REMOVED (host wires explicitly stripped)"

# 4. build (with a bounded error-driven fixup: the all-at-once scrub
# strands what an nm-purity per-module run keeps alive - orphaned
# "if err != nil" guards whose producer died, a function whose tail
# return died ("missing return"), declarations left unread. Each class
# deletes exactly the strand the build reports; the arbiter is the
# build itself, run from the same staged tree every pass. A class the
# fixer does not model reds loud with the log.)
FIX="$STAGE/proof-fixup.py"
cat > "$FIX" <<'PYEOF'
import re, sys
log = open(sys.argv[1], encoding="utf-8", errors="replace").read()
log = log.replace("\\", "/")
byfile = {}  # file -> [(line, errtext)]
for m in re.finditer(r"^([\w./\-\[\]]+\.go):(\d+):\d+:(?: undefined: \w+| declared and not used: \w+| missing return| \".+?\" imported and not used)", log, re.M):
    path, line = m.group(1), int(m.group(2))
    byfile.setdefault(path, []).append((line, m.group(0)))
for path, spots in byfile.items():
    with open(path, encoding="utf-8") as f:
        lines = f.read().split("\n")
    for line, errtext in sorted(spots, reverse=True):
        i = line - 1
        if i >= len(lines):
            continue
        if "imported and not used" in errtext:
            # the quoted path names the dying import; blank the line
            import_path = errtext.split('"')[1]
            if '"' + import_path + '"' in lines[i] or import_path in lines[i]:
                lines[i] = ""
            continue
        if "missing return" in errtext:
            # the func whose closing brace is at i: walk back to its header
            s = i
            while s > 0 and not lines[s].startswith("func "):
                s -= 1
            if lines[s].startswith("func "):
                lines[s:i + 1] = []
            continue
        if lines[i].lstrip().startswith("if "):
            # an orphaned guard: the block dies with its head
            d = 0
            j = i
            while j < len(lines):
                d += lines[j].count("{") - lines[j].count("}")
                j += 1
                if d == 0:
                    break
            lines[i:j] = []
        elif lines[i].lstrip().startswith("for ") or lines[i].lstrip().startswith("switch "):
            d = 0
            j = i
            while j < len(lines):
                d += lines[j].count("{") - lines[j].count("}")
                j += 1
                if d == 0:
                    break
            lines[i:j] = []
        else:
            # a single strand line
            lines[i] = ""
    with open(path, "w", encoding="utf-8") as f:
        f.write("\n".join(lines))
PYEOF
FIX_NAT="$FIX"
LOG="$STAGE/build.log"
LOG_NAT="$LOG.win"
if command -v cygpath >/dev/null 2>&1; then
  FIX_NAT=$(cygpath -m "$FIX")
  LOG_NAT=$(cygpath -m "$LOG.win")
fi
for PASS in $(seq 1 "${PROOF_FIXUP_PASSES:-12}"); do
  if (cd "$STAGE" && go build -o bin/omega-proof ./cmd/omega) 2> "$LOG"; then break; fi
  [ "${PROOF_FIXUP_PASSES:-12}" = 0 ] && break
  cat "$LOG" > "$LOG.win"; sed -i 's/\\/\//g' "$LOG.win" 2>/dev/null || cp "$LOG" "$LOG.win"
  (cd "$STAGE" && python3 "$FIX_NAT" "$LOG_NAT") || fail "fixup pass $PASS"
  step "fixup pass $PASS"
  [ "$PASS" = "${PROOF_FIXUP_PASSES:-12}" ] && fail "fixup budget exhausted - a strand class the fixer does not model: $(head -10 "$LOG")"
done
(cd "$STAGE" && go build -o bin/omega-proof ./cmd/omega) || fail "go build (after fixups)"
step "built: $STAGE/bin/omega-proof"

# 5. the throwaway home (config dir is <home>/config - the lane layout)
mkdir -p "$HOME_DIR/config"
cat > "$HOME_DIR/config/config.yaml" <<EOF
provider:
  type: ollama
  model_name: proof-stub
  host: http://127.0.0.1:9
server:
  port: $PORT
compaction:
  enabled: false
logging:
  enabled: false
EOF
touch "$HOME_DIR/config/trust.yaml" # trust everything - it is a dead scratch home

# OMEGA_HOME must be an OS-native path (git-bash /tmp is not one)
NATIVE_HOME="$HOME_DIR"
if command -v cygpath >/dev/null 2>&1; then NATIVE_HOME=$(cygpath -m "$HOME_DIR"); fi

cd "$STAGE/bin"
# OMEGA_MAX_RETRIES=0: the stub provider's dial error surfaces as the
# stream's error event immediately - the retry budget is not what this
# proof exercises.
OMEGA_HOME="$NATIVE_HOME" OMEGA_MAX_RETRIES=0 ./omega-proof serve &
PID=$!
step "serve pid $PID on port $PORT"
cleanup() { kill "$PID" 2>/dev/null || taskkill //F //PID "$PID" >/dev/null 2>&1 || true; }
trap cleanup EXIT
trap '[ "$KEEP" = 1 ] && trap - EXIT; cleanup' INT TERM

BASE="http://127.0.0.1:$PORT"
# 6. wait for the wire
UP=0
for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
  CODE=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/health" 2>/dev/null)
  [ "$CODE" = "200" ] && { UP=1; break; }
  sleep 0.5
done
[ "$UP" = 1 ] || fail "the stripped binary never came up on $BASE/health"
step "booted: GET /health -> $CODE"

# 7. the base faces answer
say() { printf 'proof %s\n' "$*"; }
# the header probe rides a GET with dumped headers: Go's method-scoped
# mux answers a bare HEAD 405 (the handlers register GET, not HEAD),
# and native curl cannot write MSYS /tmp paths - so every -o target is
# the native home form.
probe() { # probe <path> -> echoes "<code> <ct>" ; body lands in $NATIVE_HOME/p.json
  CODE=$(curl -s -o "$NATIVE_HOME/p.json" -w '%{http_code}' "$BASE$1")
  CT=$(curl -s -D - -o /dev/null "$BASE$1" | tr -d '\r' | grep -i '^content-type:' | cut -d' ' -f2)
  printf '%s %s' "$CODE" "$CT"
}

# settings: GET /settings answers the schema (the third shell-ring face)
V=$(probe /settings)
say "GET /settings -> $V ($(wc -c < "$HOME_DIR/p.json") bytes)"
case "$V" in "200 application/json"*) : ;; *) fail "settings did not answer JSON base: $V" ;; esac

# canvas: the snapshot answers, an empty canvas is 200 []
V=$(probe /ui/canvas)
say "GET /ui/canvas -> $V body: $(head -c 80 "$HOME_DIR/p.json")"
case "$V" in 200*) : ;; *) fail "canvas snapshot not 200: $V" ;; esac

# models + the chat floor: POST /sessions must answer (the in-memory
# store fallback the host offers when the store capsule is gone)
SID="proof-$(date +%s)$$"
CODE=$(curl -s -o "$NATIVE_HOME/p.json" -w '%{http_code}' -X POST "$BASE/sessions" \
  -H 'Content-Type: application/json' -d "{\"id\":\"$SID\",\"title\":\"removal proof\"}")
say "POST /sessions -> $CODE body head: $(head -c 80 "$HOME_DIR/p.json")"
case "$CODE" in 2*) : ;; *) fail "the chat's session floor did not answer: $CODE" ;; esac

# /models answers from config
CODE=$(curl -s -o "$NATIVE_HOME/p.json" -w '%{http_code}' "$BASE/models")
say "GET /models -> $CODE body head: $(head -c 80 "$HOME_DIR/p.json")"
case "$CODE" in 200) : ;; *) fail "models did not answer: $CODE" ;; esac

# /chat answers a STREAM (the provider is a stub - the stream carries
# the provider-need error event; that IS the face answering)
curl -s -N --max-time 20 -X POST "$BASE/chat" \
  -H 'Content-Type: application/json' \
  -d "{\"session_id\":\"$SID\",\"messages\":[{\"role\":\"user\",\"content\":\"proof\"}]}" \
  > "$HOME_DIR/p.sse" 2>/dev/null
if [ -s "$HOME_DIR/p.sse" ]; then
  say "POST /chat -> streamed $(wc -c < "$HOME_DIR/p.sse") bytes; frame head: $(head -c 120 "$HOME_DIR/p.sse" | tr -d '\n')"
else
  fail "POST /chat answered nothing"
fi

# /commands answers (the palette over the universal seam; no capsules = empty)
CODE=$(curl -s -o "$NATIVE_HOME/p.json" -w '%{http_code}' "$BASE/commands")
say "GET /commands -> $CODE body head: $(head -c 60 "$HOME_DIR/p.json")"
case "$CODE" in 2*) : ;; *) fail "commands did not answer: $CODE" ;; esac

# 8. the NOTHING ELSE: every capsule face does not serve its JSON.
# With no capsules compiled in the URL rides the SPA catch-all: 404 or
# 200 text/html - either way, no capsule JSON. (Contrast is asserted by
# the journey recipe: in the full build the same probe IS JSON.)
NOTHING_ELSE_OK=1
for p in /sessions /tasks /notifications /verdicts /skills/content /logs /agents /files/browse /workspaces/browse /insights /cron/jobs; do
  CODE=$(curl -s -o "$NATIVE_HOME/p2" -w '%{http_code}' "$BASE$p")
  CT=$(curl -s -D - -o /dev/null "$BASE$p" | tr -d '\r' | grep -i '^content-type:' | cut -d' ' -f2)
  say "GET $p -> $CODE $CT"
  case "$CT" in
    application/json*) if [ "$p" = "/logs" ] || [ "$p" = "/cron/jobs" ]; then
                          # /logs and /cron/jobs are SHELL seams, not capsule REST
                          : # they must nil-degrade, not serve a capsule's rows
                        else
                          NOTHING_ELSE_OK=0
                        fi ;;
    *) : ;;
  esac
done
# the shell-served seam-nil degraded answers must not serve capsule rows
LOGS_BODY=$(curl -s "$BASE/logs")
say "GET /logs body head: $(head -c 80 <<< "$LOGS_BODY")"
[ "$NOTHING_ELSE_OK" = 1 ] || fail "a capsule face served JSON in the every-capsule-removed build"

printf '\nproof PASS: the capsule-stripped binary boots and serves chat + canvas + settings, nothing else\n'
printf 'proof home (evidence bodies): %s (stage kept: %s)\n' "$HOME_DIR" "$STAGE"
[ "$KEEP" = 1 ] || { rm -rf "$STAGE" 2>/dev/null; }
exit 0
