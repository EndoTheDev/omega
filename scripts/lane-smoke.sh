#!/usr/bin/env bash
set -uo pipefail

# lane-smoke.sh - advisory served-SPA smoke for one lane.
#
# A lane whose binary dropped the SPA entry chunk is visually dead while
# /health stays green (black page, 2026-11-22). The served root HTML names its
# entry script - importmap "#entry", else the module script src - so fetch that
# script and require 200 with nonzero bytes.
#
# Advisory by shape (the drift-gate family): the lane check prints exactly one
# verdict line and ALWAYS exits 0, so a dead lane never reds a merge gate; the
# line is the signal a watcher greps.
#
# Usage:
#     scripts/lane-smoke.sh                  # check $OMEGA_LANE_URL
#     scripts/lane-smoke.sh --self-test      # fixture proof, no lane needed
#
# Environment:
#     OMEGA_LANE_URL   lane root (default http://100.108.38.32:8098/)
#
# Verdict line: "LANE-SMOKE OK: <entry> <bytes> bytes" or
#               "LANE-SMOKE WARNING: <reason>".
# Exit: 0 always for the lane check; --self-test exits 1 when a fixture breaks.

DEFAULT_LANE_URL="http://100.108.38.32:8098/"

usage() {
  printf '%s\n' \
    'usage: scripts/lane-smoke.sh [--self-test]' \
    "  lane check: fetch \$OMEGA_LANE_URL (default $DEFAULT_LANE_URL), extract the" \
    '  entry script the served HTML names, require nonempty bytes. One verdict' \
    '  line, always exit 0: LANE-SMOKE OK: ... | LANE-SMOKE WARNING: ...' \
    '  --self-test: fixture proof of every verdict, no lane needed.'
}

# extract_entry reads HTML on stdin and prints the entry script path it names.
extract_entry() {
  local html entry
  html="$(cat)"
  entry="$(grep -oE '"#entry"[[:space:]]*:[[:space:]]*"[^"]+"' -m1 <<<"$html" |
    sed -E 's/.*"([^"]+)"$/\1/')"
  if [ -z "$entry" ]; then
    entry="$(grep -oiE '<script[^>]+type="module"[^>]+src="[^"]+"' -m1 <<<"$html" |
      sed -E 's/.*src="([^"]+)".*/\1/')"
  fi
  printf '%s' "$entry"
}

# asset_url joins the lane root with the entry path the HTML named.
asset_url() {
  case "$2" in
    http://* | https://*) printf '%s' "$2" ;;
    /*) printf '%s%s' "${1%/}" "$2" ;;
    *) printf '%s/%s' "${1%/}" "$2" ;;
  esac
}

# check_lane prints the one verdict line; returns 0 when the entry is served.
check_lane() {
  local base="${1%/}" html entry url size code
  html="$(curl -fsS --max-time 10 "$base/" 2>/dev/null)" ||
    { printf 'LANE-SMOKE WARNING: lane root unreachable (%s/)\n' "$base"; return 1; }
  entry="$(extract_entry <<<"$html")"
  if [ -z "$entry" ]; then
    printf 'LANE-SMOKE WARNING: served HTML names no entry script (%s/)\n' "$base"
    return 1
  fi
  url="$(asset_url "$base" "$entry")"
  size="$(curl -fsSL --max-time 60 "$url" 2>/dev/null | wc -c | tr -d ' ')"
  if [ -n "$size" ] && [ "$size" -gt 0 ]; then
    printf 'LANE-SMOKE OK: %s %s bytes\n' "$entry" "$size"
    return 0
  fi
  code="$(curl -sS -I --max-time 15 "$url" 2>/dev/null | head -1 | awk '{print $2}')"
  printf 'LANE-SMOKE WARNING: entry %s answered HTTP %s with no bytes (the served HTML names a bundle the binary does not embed)\n' \
    "$entry" "${code:-000}"
  return 1
}

# self_test serves fixture HTMLs from a scratch dir and proves both verdicts.
self_test() {
  local py dir dir_http port pid fails=0 out i
  py="$(command -v python3 || command -v python)"
  if [ -z "$py" ]; then
    echo "LANE-SMOKE SELF-TEST FAIL: no python3/python for the fixture server"
    return 1
  fi
  dir="$(mktemp -d)"
  # a native win32 python cannot open an MSYS /tmp path
  dir_http="$dir"
  if command -v cygpath >/dev/null 2>&1; then
    dir_http="$(cygpath -w "$dir")"
  fi
  mkdir -p "$dir/healthy/_nuxt" "$dir/missing/_nuxt" "$dir/fallback/_nuxt" "$dir/noentry"
  printf '%s' '<!DOCTYPE html><html><head><script type="importmap">{"imports":{"#entry":"/_nuxt/entry.abc.js"}}</script><script type="module" src="/_nuxt/entry.abc.js" crossorigin></script></head><body><div id="__nuxt"></div></body></html>' \
    >"$dir/healthy/index.html"
  printf 'console.log("entry");\n' >"$dir/healthy/_nuxt/entry.abc.js"
  printf '%s' '<!DOCTYPE html><html><head><script type="importmap">{"imports":{"#entry":"/_nuxt/_gone.js"}}</script></head></html>' \
    >"$dir/missing/index.html"
  printf '%s' '<!DOCTYPE html><html><head><script type="module" src="/_nuxt/fallback.js"></script></head></html>' \
    >"$dir/fallback/index.html"
  printf 'console.log("fallback");\n' >"$dir/fallback/_nuxt/fallback.js"
  printf '%s' '<!DOCTYPE html><html><head><title>omega</title></head><body></body></html>' \
    >"$dir/noentry/index.html"

  port="$("$py" -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
  "$py" -m http.server "$port" --bind 127.0.0.1 --directory "$dir_http" >/dev/null 2>&1 &
  pid=$!

  local i
  for i in $(seq 1 100); do
    curl -sS --max-time 1 "http://127.0.0.1:$port/healthy/index.html" >/dev/null 2>&1 && break
    sleep 0.1
  done

  expect() { # label expected-regex fixture-dir
    local label="$1" want="$2" base="http://127.0.0.1:$port/$3" out
    out="$(check_lane "$base")"
    if grep -qE "$want" <<<"$out"; then
      echo "SELF-TEST PASS: $label -> $out"
    else
      echo "SELF-TEST FAIL: $label expected /$want/ got: $out"
      fails=$((fails + 1))
    fi
  }

  expect "healthy fixture" '^LANE-SMOKE OK: /_nuxt/entry\.abc\.js [0-9]+ bytes$' healthy
  expect "missing asset" '^LANE-SMOKE WARNING: entry /_nuxt/_gone\.js answered HTTP 404' missing
  expect "module-src fallback" '^LANE-SMOKE OK: /_nuxt/fallback\.js [0-9]+ bytes$' fallback
  expect "no entry named" '^LANE-SMOKE WARNING: served HTML names no entry script' noentry

  kill "$pid" 2>/dev/null
  wait "$pid" 2>/dev/null
  rm -rf "$dir"

  if [ "$fails" -eq 0 ]; then
    echo "LANE-SMOKE SELF-TEST PASS"
    return 0
  fi
  echo "LANE-SMOKE SELF-TEST FAIL: $fails fixture(s) wrong"
  return 1
}

rc=0
case "${1:-}" in
  --self-test) self_test
    rc=$? ;;
  -h | --help) usage ;;
  "") check_lane "${OMEGA_LANE_URL:-$DEFAULT_LANE_URL}" || true ;;
  *) echo "lane-smoke: unknown argument: $1" >&2
    usage >&2
    exit 2 ;;
esac
exit "$rc"
