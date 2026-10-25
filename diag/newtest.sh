#!/bin/bash
set -u
HROOT="$(cd "$(dirname "$0")/.." && pwd)"
export JROOT="$HROOT/scripts/journeys"
export JOURNEY_BASE_URL=http://127.0.0.1:18431
export JOURNEY_CHROME_PORT=9433
export JOURNEY_SESSION=cody-diag30-probe
. "$JROOT/lib.sh"
curl -s -X DELETE http://127.0.0.1:18431/sessions/jrn-25Oct173320-retry -o /dev/null
new_journey_session retry
echo "rc=$?"