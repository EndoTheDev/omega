#!/usr/bin/env bash
set -euo pipefail

# bench-run.sh - one command runs the Linux container bench against this checkout.
#
# Builds a real linux/amd64 omega in a container, boots `omega serve` on a fresh
# one-file home with the deterministic mock provider, fetches the webui, runs one chat
# round trip, proves a settings write survives a server restart, then destroys the
# container. Every step leaves a receipt under $BENCH_OUT/<run-id>/ and RESULT.txt says
# whether the whole run passed.
#
# Requires: Docker with the Linux engine, curl, host port 18099 free, run from the
# repository root.

PORT=18099
BASE="http://127.0.0.1:${PORT}"

RUN_ID="$(date -u +%Y%m%dt%H%M%Sz)-$$"
OUT="${BENCH_OUT:-D:/dev/bench}/${RUN_ID}"
PROJECT="omega-bench-${RUN_ID}"
COMPOSE=(docker compose -p "$PROJECT" -f docker/bench-compose.yml)

START_TS="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
VERSION="$(git describe --tags --exact-match 2>/dev/null || echo dev)-$(git rev-parse --short HEAD)"

mkdir -p "$OUT"
# Never name a scratch variable TMP/TEMP/TMPDIR: those are exported in git bash, and an
# exported assignment of a FILE path leaves the docker CLI resolving its own temp dirs
# inside that file ("invalid output path" at the end of an otherwise green build).
POLL_BODY="$(mktemp)"

RESULTS=()
FAILED=0
running=0

pass() { RESULTS+=("PASS|$1|$2"); printf 'PASS %-30s %s\n' "$1" "$2"; }
fail() { FAILED=1; RESULTS+=("FAIL|$1|$2"); printf 'FAIL %-30s %s\n' "$1" "$2" >&2; }

assert_in() { # <file> <needle> <id> <description>
  if grep -qF -- "$2" "$1" 2>/dev/null; then pass "$3" "$4"; else fail "$3" "$4"; fi
}

write_meta() {
  # The base image is pulled by BuildKit into the build cache without a local tag, so the
  # only authoritative record of the digest this build resolved is the build log's FROM
  # line. The built image is local-only: its config digest (.Id) is its identity.
  local base_digest="-"
  if [ -f "$OUT/01-build.log" ]; then
    base_digest=$(grep -oE 'golang:1\.26\.11-bookworm@sha256:[0-9a-f]+' "$OUT/01-build.log" 2>/dev/null | head -1 || true)
    [ -n "$base_digest" ] || base_digest="-"
  fi
  {
    echo "run-id: ${RUN_ID}"
    echo "compose-project: ${PROJECT}"
    echo "git-head: $(git rev-parse HEAD)"
    echo "bench-version: ${VERSION}"
    echo "started-at: ${START_TS}"
    echo "ended-at: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "docker: $(docker version --format '{{.Server.Version}}' 2>/dev/null || echo unknown)"
    echo "compose: $(docker compose version --short 2>/dev/null || echo unknown)"
    echo "base-image: golang:1.26.11-bookworm"
    echo "base-image-digest: ${base_digest}"
    echo "image-digest: $(docker image inspect omega-bench:local --format '{{.Id}}' 2>/dev/null || echo none)"
  } >"$OUT/00-meta.txt"
}

write_result() {
  {
    echo "run-id: ${RUN_ID}"
    echo "receipts: ${OUT}"
    if [ "$FAILED" -eq 0 ]; then echo "result: PASS"; else echo "result: FAIL"; fi
    echo
    local row st id desc
    for row in ${RESULTS[@]+"${RESULTS[@]}"}; do
      IFS='|' read -r st id desc <<<"$row"
      printf '%-4s %-30s %s\n' "$st" "$id" "$desc"
    done
  } >"$OUT/RESULT.txt"
}

# residue lists the surviving docker objects by LABEL, not by name: a network never
# carries a service's custom label, so the compose project label is the only filter that
# sees all three kinds, and the bench label catches a container that escaped the project.
residue() {
  local containers volumes networks escaped
  containers=$(docker ps -aq --filter "label=com.docker.compose.project=${PROJECT}" 2>&1 || true)
  volumes=$(docker volume ls -q --filter "label=com.docker.compose.project=${PROJECT}" 2>&1 || true)
  networks=$(docker network ls -q --filter "label=com.docker.compose.project=${PROJECT}" 2>&1 || true)
  escaped=$(docker ps -aq --filter "label=omega.bench=${RUN_ID}" 2>&1 || true)
  {
    echo "# docker ps -aq --filter label=com.docker.compose.project=${PROJECT}"
    printf '%s\n' "$containers"
    echo "# docker volume ls -q --filter label=com.docker.compose.project=${PROJECT}"
    printf '%s\n' "$volumes"
    echo "# docker network ls -q --filter label=com.docker.compose.project=${PROJECT}"
    printf '%s\n' "$networks"
    echo "# docker ps -aq --filter label=omega.bench=${RUN_ID}"
    printf '%s\n' "$escaped"
  } >"$OUT/10-residue.txt"
  if [ -z "$containers" ]; then pass residue-containers "no container survived - 10-residue.txt"; else fail residue-containers "containers survived: $containers - 10-residue.txt"; fi
  if [ -z "$volumes" ]; then pass residue-volumes "no volume survived - 10-residue.txt"; else fail residue-volumes "volumes survived: $volumes - 10-residue.txt"; fi
  if [ -z "$networks" ]; then pass residue-networks "no network survived - 10-residue.txt"; else fail residue-networks "networks survived: $networks - 10-residue.txt"; fi
  if [ -z "$escaped" ]; then pass residue-bench-label "no container carries omega.bench - 10-residue.txt"; else fail residue-bench-label "containers carry omega.bench: $escaped"; fi
}

cleanup() {
  local code=$?
  set +e
  if [ "$running" -eq 1 ]; then
    printf 'bench: tearing down the run after an early exit\n' >&2
    "${COMPOSE[@]}" down -v --remove-orphans >>"$OUT/09-serve.log" 2>&1
    running=0
    residue
  fi
  rm -f "$POLL_BODY"
  write_meta
  write_result
  exit "$code"
}
trap cleanup EXIT

poll_health() { # <dest>
  local dest=$1 deadline=$((SECONDS + 60)) code
  while [ "$SECONDS" -lt "$deadline" ]; do
    code=$(curl -sS -o "$dest" -w '%{http_code}' --max-time 5 "$BASE/health" 2>/dev/null || true)
    if [ "$code" = "200" ]; then return 0; fi
    sleep 1
  done
  return 1
}

port_busy() {
  local port=$1
  # LISTEN only: a TIME_WAIT row names the port as the REMOTE end (the previous run's own
  # clients), which is not a listener and must not block the next run.
  if command -v ss >/dev/null 2>&1; then
    ss -ltn 2>/dev/null | grep -qE "[:.]${port}[[:space:]]"
  elif command -v netstat >/dev/null 2>&1; then
    netstat -an 2>/dev/null | grep -E "(127\.0\.0\.1|0\.0\.0\.0|\[::\]):${port}[[:space:]]" | grep -qi "listen"
  else
    return 1
  fi
}

# sse_data_all <file> <event>: the data line of every frame of that event.
sse_data_all() {
  awk -v ev="$2" '
    $0 == "event: " ev { want = 1; next }
    want && /^data: / { sub(/^data: /, ""); print; want = 0 }
  ' "$1"
}

printf 'bench run %s\n' "$RUN_ID"
printf 'receipts -> %s\n\n' "$OUT"

# --- preflight --------------------------------------------------------------
if port_busy "$PORT"; then
  fail preflight-port "host port ${PORT} is busy; it is fixed because server.public_hosts is baked into the image"
  printf '\nbench: host port %d is busy - free it and rerun\n' "$PORT" >&2
  exit 1
fi
pass preflight-port "host port ${PORT} is free"

export BENCH_VERSION="$VERSION"
export BENCH_RUN_ID="$RUN_ID"

# --- build + boot -----------------------------------------------------------
if ! "${COMPOSE[@]}" build >"$OUT/01-build.log" 2>&1; then
  fail build "docker compose build failed - 01-build.log"
  exit 1
fi
pass build "linux/$(uname -m) omega built as ${VERSION} - 01-build.log"

if ! "${COMPOSE[@]}" up -d >>"$OUT/01-build.log" 2>&1; then
  fail up "docker compose up failed - 01-build.log"
  exit 1
fi
running=1
pass up "one container up on 127.0.0.1:${PORT} - 01-build.log"

# --- health -----------------------------------------------------------------
if poll_health "$OUT/02-health.json"; then
  pass health "GET /health answered 200 on a fresh home - 02-health.json"
else
  fail health "GET /health never answered 200 in 60s - 02-health.json"
fi
assert_in "$OUT/02-health.json" '"ok"' health-ok 'health body carries "ok" - 02-health.json'

# --- webui ------------------------------------------------------------------
code=$(curl -sS -o "$OUT/03-webui.html" -w '%{http_code}' --max-time 15 "$BASE/" || true)
if [ "$code" = "200" ]; then pass webui-status "GET / answered 200 - 03-webui.html"; else fail webui-status "GET / answered ${code:-none} - 03-webui.html"; fi
assert_in "$OUT/03-webui.html" '<div id="__nuxt">' webui-shell 'the served page is the SPA shell - 03-webui.html'

entry=$(grep -oE '<script[^>]*type="module"[^>]*src="[^"]+"' "$OUT/03-webui.html" 2>/dev/null | head -1 | sed -e 's/.*src="//' -e 's/"$//')
if [ -z "$entry" ]; then
  fail webui-entry "no module entry script in the SPA shell - 03-webui.html"
else
  code=$(curl -sS -o "$OUT/04-entry.txt" -w '%{http_code}' --max-time 30 "${BASE}${entry}" || true)
  if [ "$code" = "200" ] && [ -s "$OUT/04-entry.txt" ]; then
    pass webui-entry "entry script ${entry} fetched - 04-entry.txt"
  else
    fail webui-entry "entry script ${entry} answered ${code:-none} - 04-entry.txt"
  fi
fi

# --- chat round trip --------------------------------------------------------
curl -N -sS --max-time 180 -X POST "$BASE/chat" \
  -H 'Content-Type: application/json' \
  -H "Origin: $BASE" \
  -d '{"messages":[{"role":"user","content":"bench round trip"}]}' \
  >"$OUT/05-chat.sse" || true

if grep -qx 'event: session' "$OUT/05-chat.sse"; then pass chat-session "the run opened on a session frame - 05-chat.sse"; else fail chat-session "no session frame - 05-chat.sse"; fi
if grep -qx 'event: tool_call' "$OUT/05-chat.sse"; then pass chat-tool-call "the mock called an announced tool - 05-chat.sse"; else fail chat-tool-call "no tool_call frame - 05-chat.sse"; fi

# The mock streams its answer in fixed-size chunks, so the phrase under test can straddle
# a frame boundary: reassemble the content field of every response_chunk before asserting.
reply="$(sse_data_all "$OUT/05-chat.sse" response_chunk | sed -e 's/^{"type":"response_chunk","content":"//' -e 's/"}$//' | tr -d '\r\n')"
case "$reply" in
  '[mock tool '*) pass chat-reply-prefix "the answer text starts [mock tool - 05-chat.sse" ;;
  *) fail chat-reply-prefix "the answer text does not start [mock tool: ${reply:0:40} - 05-chat.sse" ;;
esac
case "$reply" in
  *'tool result consumed'*) pass chat-reply-tool "the answer consumed the tool result - 05-chat.sse" ;;
  *) fail chat-reply-tool "the answer never consumed the tool result - 05-chat.sse" ;;
esac

if sse_data_all "$OUT/05-chat.sse" agent_end | grep -qF '"finish_reason":"stop"'; then
  pass chat-agent-end "the run ended with finish_reason stop - 05-chat.sse"
else
  fail chat-agent-end "no agent_end with finish_reason stop - 05-chat.sse"
fi

# fresh home: the run's own session is the only one the store knows
curl -sS --max-time 15 "$BASE/sessions" >"$OUT/11-sessions.json" || true
sessions=$(grep -o '"id":"' "$OUT/11-sessions.json" 2>/dev/null | wc -l | tr -d ' ')
if [ "$sessions" = "1" ]; then
  pass fresh-home "GET /sessions lists exactly the run's own session - 11-sessions.json"
else
  fail fresh-home "GET /sessions lists ${sessions:-0} sessions, want 1 - 11-sessions.json"
fi

# --- settings write + readback ---------------------------------------------
curl -sS --max-time 15 -X PUT "$BASE/settings" \
  -H 'Content-Type: application/json' \
  -H "Origin: $BASE" \
  -d '{"values":{"compaction.keep_last":12}}' >"$OUT/06-settings.json" || true
curl -sS --max-time 15 "$BASE/settings?key=compaction.keep_last" >"$OUT/06-settings-readback.json" || true
assert_in "$OUT/06-settings-readback.json" '"value":12' settings-readback 'the knob reads back 12 - 06-settings-readback.json'

"${COMPOSE[@]}" exec -T omega cat /home/omega/.omega/config/config.yaml >"$OUT/07-config.yaml" 2>/dev/null || true
assert_in "$OUT/07-config.yaml" 'keep_last: 12' settings-config 'the value landed in the home config.yaml - 07-config.yaml'

# --- restart: the persistence proof ----------------------------------------
if ! "${COMPOSE[@]}" restart >>"$OUT/01-build.log" 2>&1; then
  fail restart "docker compose restart failed - 01-build.log"
fi
if poll_health "$POLL_BODY"; then
  pass health-after-restart "the restarted server answered /health - 08-settings-after-restart.json"
else
  fail health-after-restart "the restarted server never answered /health in 60s"
fi
curl -sS --max-time 15 "$BASE/settings?key=compaction.keep_last" >"$OUT/08-settings-after-restart.json" || true
assert_in "$OUT/08-settings-after-restart.json" '"value":12' settings-after-restart 'the knob still reads 12 after the restart - 08-settings-after-restart.json'

# --- logs + teardown --------------------------------------------------------
"${COMPOSE[@]}" logs --no-color >"$OUT/09-serve.log" 2>&1 || true
"${COMPOSE[@]}" down -v --remove-orphans >>"$OUT/09-serve.log" 2>&1 || true
running=0
residue

if [ "$FAILED" -eq 0 ]; then
  printf '\nbench PASS - receipts in %s\n' "$OUT"
  exit 0
fi
printf '\nbench FAIL - receipts kept in %s\n' "$OUT" >&2
exit 1
