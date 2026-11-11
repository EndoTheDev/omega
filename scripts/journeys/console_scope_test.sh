#!/usr/bin/env bash
set -uo pipefail

# console_scope_test.sh - the console-record contract of scripts/journeys/run.sh
# (card t_6654bc60).
#
# THE FACT (proven live on agent-browser 0.26.0, 2026-11-11): the console
# record belongs to the DAEMON SESSION, not the page and not the chrome. It
# survives a navigation, a reload and a reconnect to a DIFFERENT chrome - a
# fresh browser on another CDP port still answered `console` with the
# previous browser's error line. One transient [NUXT_E5002] therefore red
# every later assert_no_console_errors on that host, forever, while the
# manifest answered 200 and every product step stayed green.
#
# THE CONTRACT PINNED HERE: run.sh clears the record right before each recipe
# forks (console_scope_begin, lib.sh), so a recipe is judged on its OWN page
# life - an older run's error can no longer red it, and an error THIS run
# produces still does.
#
# Phases (a copy of the journeys tree, a stub agent-browser, a stub lane -
# no real browser, no real lane, nothing shared):
#   P1 fixed tree, old error + assert recipe   -> both green
#   P2 SAME pair with the scope call removed   -> the assert recipe REDs
#                                                 (proves P1 can fail)
#   P3 fixed tree, recipe logs its own error   -> RED (the assert still bites)
#   P4 fixed tree, session pre-poisoned before
#      the suite starts                         -> green
#
# Run: bash scripts/journeys/console_scope_test.sh

JROOT="$(cd "$(dirname "$0")" && pwd)"

pass=0
fail=0
ok() { pass=$((pass + 1)); printf 'ok: %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf 'FAIL: %s\n' "$1" >&2; }

TMP="$(mktemp -d "${TMPDIR:-/tmp}/console-scope-test.XXXXXX")"
# native tools (python3 is a win32 build on MSYS hosts) cannot open a POSIX
# /tmp path - it resolves to <drive>:\tmp. Keep bash-side paths POSIX (the
# stub dir rides PATH, and a C:/ entry never resolves there) and hand native
# consumers the cygpath -m mixed form.
NATIVE_TMP="$TMP"
if command -v cygpath >/dev/null 2>&1; then
  NATIVE_TMP="$(cygpath -m "$TMP")"
fi
LANE_PID=""
cleanup() {
  [ -n "$LANE_PID" ] && kill "$LANE_PID" 2>/dev/null
  rm -rf "$TMP" 2>/dev/null
}
trap cleanup EXIT

# --- fixtures ----------------------------------------------------------------

# stub agent-browser: the console record is one file this test owns. Every
# other subcommand is a no-op (the toys never touch a page).
mkdir -p "$TMP/bin"
cat > "$TMP/bin/agent-browser" <<'STUB'
#!/bin/bash
args=()
while [ $# -gt 0 ]; do
  case "$1" in
    --session) shift 2 ;;
    *) args+=("$1"); shift ;;
  esac
done
case "${args[0]:-}" in
  console)
    if [ "${args[1]:-}" = "--clear" ]; then
      : > "${STUB_CONSOLE:?}"
    else
      cat "${STUB_CONSOLE:?}" 2>/dev/null
    fi
    ;;
  *) : ;;
esac
exit 0
STUB
chmod +x "$TMP/bin/agent-browser"

# stub lane: run.sh's health gate, the residue sweep and the model probe all
# take a 200 and move on.
PORT="$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
cat > "$TMP/lane.py" <<'PY'
import http.server
import sys


class Handler(http.server.BaseHTTPRequestHandler):
    def _send(self, body=b"{}"):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path.startswith("/health"):
            self._send(b'{"status":"ok"}')
        else:
            self._send(b"{}")

    def do_POST(self):
        self._send(b'{"id":"jrn-stub"}')

    def do_PUT(self):
        self._send(b"{}")

    def do_DELETE(self):
        self._send(b"{}")

    def log_message(self, *args):
        pass


http.server.ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()
PY
python3 "$NATIVE_TMP/lane.py" "$PORT" &
LANE_PID=$!
for _ in $(seq 1 40); do
  curl -s -m 2 "http://127.0.0.1:$PORT/health" >/dev/null 2>&1 && break
  sleep 0.2
done

# a copy of the journeys tree per tree under test: JROOT is derived from
# run.sh's own location, so the toys live in the copy's recipes/ and never
# in the repo.
make_tree() { # $1 = dir
  mkdir -p "$1"
  cp -r "$JROOT/lib.sh" "$JROOT/run.sh" "$JROOT/sweep-chromes.sh" "$1"/
  mkdir -p "$1/recipes"
  cat > "$1/recipes/990-scope-old-error.sh" <<'R'
#!/bin/bash
# a PREVIOUS page load's console line: exactly what the daemon session
# record hands to the next recipe when nothing scopes it
printf '%s\n' '[error] [NUXT] [NUXT_E5002] could not fetch the app manifest (builds/meta/old-run.json)' >> "$STUB_CONSOLE"
step "old-run console line planted"
R
  cat > "$1/recipes/991-scope-assert.sh" <<'R'
#!/bin/bash
assert_no_console_errors
R
  cat > "$1/recipes/992-scope-live-error.sh" <<'R'
#!/bin/bash
# an error THIS run produces must still red the recipe
printf '%s\n' '[error] THIS-RUN-REAL: hydration ReferenceError' >> "$STUB_CONSOLE"
assert_no_console_errors
R
}

# drive the real run.sh against the stub lane + stub browser
drive() { # $1 = tree, $2 = log path, rest = recipe names
  local tree="$1" out="$2"
  shift 2
  (
    cd "$tree" || exit 2
    PATH="$TMP/bin:$PATH" \
      STUB_CONSOLE="$STUB_CONSOLE" \
      JOURNEY_BASE_URL="http://127.0.0.1:$PORT" \
      JOURNEY_LOCK="$TMP/lock" \
      JOURNEY_SESSION=scope-test \
      JOURNEY_TIMEOUT=60 \
      bash "$tree/run.sh" "$@"
  ) > "$out" 2>&1
}

FIXED="$TMP/fixed"
BROKEN="$TMP/broken"
make_tree "$FIXED"
make_tree "$BROKEN"
# the mutation: drop the scope call from the copy - the exact shape the
# harness had before t_6654bc60
python3 - "$NATIVE_TMP/broken/run.sh" <<'PY'
import sys

path = sys.argv[1]
src = open(path, encoding="utf-8").read()
assert "    console_scope_begin\n" in src, "scope call not found in the copy"
open(path, "w", encoding="utf-8", newline="").write(src.replace("    console_scope_begin\n", ""))
PY

# --- P1: fixed tree - an old error cannot red the next recipe ----------------
STUB_CONSOLE="$TMP/p1.console"
: > "$STUB_CONSOLE"
if drive "$FIXED" "$TMP/p1.log" 990-scope-old-error 991-scope-assert; then
  ok "P1 suite green (old error never reaches the next recipe)"
else
  bad "P1 suite RED on the fixed tree: $(grep -E 'journey RED|RED' "$TMP/p1.log" | head -2)"
fi
grep -q '990-scope-old-error.*green' "$TMP/p1.log" && grep -q '991-scope-assert.*green' "$TMP/p1.log" \
  && ok "P1 both recipes ran green" \
  || bad "P1 recipes did not both run green"
grep -q 'old-run console line planted' "$(sed -n 's/^journeys: evidence in //p' "$TMP/p1.log" | tail -1)/990-scope-old-error.log" 2>/dev/null \
  && ok "P1 the old line really was planted" \
  || bad "P1 the poison recipe never ran"

# --- P2: the mutation - the same pair must fail -------------------------------
STUB_CONSOLE="$TMP/p2.console"
: > "$STUB_CONSOLE"
if drive "$BROKEN" "$TMP/p2.log" 990-scope-old-error 991-scope-assert; then
  bad "P2 unscoped tree stayed green - the pin cannot fail, it proves nothing"
else
  ok "P2 unscoped tree REDs (the pin detects the bug it exists for)"
fi
grep -q 'NUXT_E5002' "$TMP/p2.log" \
  && ok "P2 the RED names the stale line" \
  || bad "P2 the RED did not name the stale console line"

# --- P3: a real error from THIS recipe still bites ----------------------------
STUB_CONSOLE="$TMP/p3.console"
: > "$STUB_CONSOLE"
if drive "$FIXED" "$TMP/p3.log" 992-scope-live-error; then
  bad "P3 a real error from this run passed silently - the assert was weakened"
else
  ok "P3 a real error from this run still REDs"
fi
grep -q 'THIS-RUN-REAL' "$TMP/p3.log" \
  && ok "P3 the RED names this run's error" \
  || bad "P3 the RED did not name this run's error"

# --- P4: a session poisoned BEFORE the suite starts ---------------------------
STUB_CONSOLE="$TMP/p4.console"
printf '%s\n' '[error] [NUXT] [NUXT_E5002] could not fetch the app manifest (builds/meta/earlier-suite.json)' > "$STUB_CONSOLE"
if drive "$FIXED" "$TMP/p4.log" 991-scope-assert; then
  ok "P4 a record left by an earlier suite no longer reds the run"
else
  bad "P4 a pre-poisoned session still reds: $(grep -E 'journey RED' "$TMP/p4.log" | head -1)"
fi

printf '\nconsole_scope_test: %s passed, %s failed\n' "$pass" "$fail"
[ "$fail" = 0 ] || exit 1
exit 0
