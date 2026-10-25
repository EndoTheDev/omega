#!/bin/bash
# redloop.sh - hunt a switch-back red on the diag lane (push logging on).
# Runs diag30.sh repeatedly; evidence under diag/evidence/run-*, RED marker
# to redloop-state.
set -u
HROOT="$(cd "$(dirname "$0")/.." && pwd)"
export AB="$(command -v agent-browser 2>/dev/null | head -1)"
[ -n "$AB" ] || AB="$HOME/.hermes/tools/agent-browser-0.26.0-linux-arm64/bin/agent-browser-linux-arm64"
export AB
N="${1:-30}"
REDS=0
for i in $(seq 1 "$N"); do
  # unique run tag per iteration: journey_tag reads the runtag file which
  # has second resolution only - back-to-back runs collide on sessions.id
  printf '%s%02d' "$(date +%d%b%H%M%S)" "$((i % 40))" > "$HOME/.hermes/profiles/cody/cache/scratch/omega-jrn-runtag"
  echo "=== loop $i/$(date +%T) ==="
  timeout 200 bash "$HROOT/diag/diag30.sh" 2>&1 | tail -3
  sleep 1
  # find the newest evidence red.txt written in this iteration
  last=$(ls -1td "$HROOT/diag/evidence/"run-* | head -1)
  if [ -f "$last/red.txt" ] && grep -q "^msg: sibling" "$last/red.txt" 2>/dev/null; then
    REDS=$((REDS+1)); echo "!!! RED (switch-back) at: $last — PAUSING 15min for interactive probe (delete $HROOT/diag/redloop-state to resume)"
    touch "$HROOT/diag/redloop-state"
    for t in $(seq 1 90); do [ -f "$HROOT/diag/redloop-state" ] || break; sleep 10; done
    rm -f "$HROOT/diag/redloop-state"
  fi
done
echo "loop done: $N runs, $REDS switch-back reds"