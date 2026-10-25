#!/bin/bash
# journey 30 loop for t_1c9793a8 repro (worktree, private lane + mock)
cd /home/endo/dev/omega/.worktrees/t_1c9793a8
for i in $(seq 1 20); do
  echo "=== run $i $(date +%H:%M:%S)"
  JOURNEY_TIMEOUT=60 JOURNEY_BASE_URL=http://127.0.0.1:18200 \
    JOURNEY_CHROME_PORT=9433 JOURNEY_SESSION=cody-t1c9 \
    JROOT="$JROOT" \
    bash scripts/journeys/run.sh 30-retry-single-user 2>&1 |
    grep -E '30-retry.*(green|RED)|suite done'
done
echo LOOP_DONE