#!/usr/bin/env bash
# factory status - one glance at the whole pipeline (Endo's morning check)
# Usage: scripts/factory-status.sh  (run from anywhere)
set -euo pipefail
cd "$(dirname "$0")/.."

DB="$HOME/.hermes/kanban/boards/project-omega/kanban.db"

echo "== factory $(date +%F\ %H:%M\ %Z) =="
echo "-- running --"
sqlite3 "$DB" "SELECT '  '||id||'  '||substr(title,1,52) FROM tasks WHERE status='running';"
echo "-- board --"
sqlite3 "$DB" "SELECT '  '||status||': '||COUNT(*) FROM tasks WHERE status IN ('ready','todo','blocked') GROUP BY status ORDER BY status;"

echo "-- git --"
git fetch -q origin dev master 2>/dev/null || true
AHEAD=$(git rev-list --count origin/master..origin/dev)
echo "  dev:    $(git rev-parse --short origin/dev)"
echo "  master: $(git rev-parse --short origin/master)  (dev ahead by $AHEAD)"

echo "-- channels --"
TAG=$(git tag -l 'v*-nightly.*' --sort=-v:refname | head -1)
if [ -n "$TAG" ]; then
  AGE_H=$(( ( $(date +%s) - $(git log -1 --format=%ct "$TAG") ) / 3600 ))
  SOAK="$(( AGE_H / 24 ))d$(( AGE_H % 24 ))h"
  [ "$AGE_H" -ge 48 ] && SOAK="$SOAK  <- soak-ready"
  echo "  latest nightly: $TAG  (age $SOAK)"
else
  echo "  latest nightly: none yet"
fi
STABLE=$(git tag -l 'v*.*.*' | grep -v -- '-' | sort -V | tail -1)
echo "  stable: $STABLE"

echo "-- lane --"
HEALTH=$(curl -s --max-time 4 http://127.0.0.1:8098/health || echo '{"status":"unreachable"}')
echo "  :8098 $(echo "$HEALTH" | grep -o '"version":"[^"]*"' | head -1)"
echo "  drift gate: $AHEAD commits ahead of master (red while > 10, expected until first soak ff)"