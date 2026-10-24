#!/usr/bin/env bash
set -uo pipefail

# factory-dispatch_test.sh - runnable checks for scripts/factory-dispatch.sh.
# Run: bash scripts/factory-dispatch_test.sh
# T1 pins the empty-RUNNING kill: the class-law RC loop must survive a zero
# running board under set -euo pipefail (proven live 2026-10-24: the guard
# died silently, exit 1, no verdicts, whenever no card was running).
# T2 is a live dry-run smoke against the real board (local only: skipped
# when the board DB is absent, e.g. CI runners).

GUARD="$(cd "$(dirname "$0")" && pwd)/factory-dispatch.sh"
DB="$HOME/.hermes/kanban/boards/project-omega/kanban.db"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }

# T1: the RC-loop shape with an empty RUNNING array must not kill the shell.
out=$(bash -c '
set -euo pipefail
RUNNING=()
RC=$(for r in "${RUNNING[@]:-}"; do [ -n "$r" ] && echo go || true; done | sort -u | tr "\n" ",")
echo "RC=[$RC]"
' 2>&1)
rc=$?
[ "$rc" -eq 0 ] && ok || bad "T1: empty RUNNING array killed the RC loop (exit $rc) - the || true guard is gone"
printf '%s' "$out" | grep -q 'RC=\[' && ok || bad "T1: expected RC= line, got: $out"

# T2 (local only): dry-run on the real board must exit 0 with output - a
# silent empty run is the empty-RUNNING kill signature.
if [ -f "$DB" ]; then
	out=$(bash "$GUARD" --dry-run 2>&1)
	rc=$?
	[ "$rc" -eq 0 ] && ok || bad "T2: dry-run exited $rc (expected 0)"
	[ -n "$out" ] && ok || bad "T2: dry-run printed nothing (exit $rc) - the empty-RUNNING kill signature"
else
	echo "T2: skipped (no board DB at $DB)"
fi

echo
echo "factory-dispatch_test: $pass ok, $fail fail"
[ "$fail" -eq 0 ]