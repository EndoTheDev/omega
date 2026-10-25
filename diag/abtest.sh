#!/bin/bash
set -u
HROOT="$(cd "$(dirname "$0")/.." && pwd)"
export JROOT="$HROOT/scripts/journeys"
export JOURNEY_BASE_URL=http://127.0.0.1:18431
export JOURNEY_CHROME_PORT=9433
export JOURNEY_SESSION=cody-diag30-probe
case "${1:-}" in
  tab) timeout 30 agent-browser-linux-arm64 --session cody-diag30-probe tabs ;;
  open) timeout 30 agent-browser-linux-arm64 --session cody-diag30-probe open http://127.0.0.1:18431/ ;;
  view) timeout 30 agent-browser --session cody-diag30-probe --print-last-action view 2>&1 | head -5 ;;
  *) echo "usage: $0 tab|open" ;;
esac
echo "rc=$?"