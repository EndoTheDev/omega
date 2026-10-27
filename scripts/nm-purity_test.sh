#!/usr/bin/env bash
set -uo pipefail

# nm-purity_test.sh - cheap negative-branch tests for scripts/nm-purity.sh.
# The positive law (zero module symbols) is the canary itself: CI runs
# scripts/nm-purity.sh directly - one build inside a throwaway worktree,
# the test suite does not double it.

PURITY="$(cd "$(dirname "$0")" && pwd)/nm-purity.sh"

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }

# 1. unknown arg refuses loud with the usage exit code
if bash "$PURITY" --nonsense >/dev/null 2>&1; then
  bad "unknown arg was accepted"
elif [ $? -eq 2 ]; then
  ok
else
  bad "unknown arg exited $?, want 2"
fi

# 2. a module that does not exist fails RED, not green
if bash "$PURITY" --module definitely-not-a-module >/dev/null 2>&1; then
  bad "a nonexistent module purity-checked GREEN"
elif [ $? -eq 1 ]; then
  ok
else
  bad "nonexistent module exited $?, want 1"
fi

# 3. the canary module list default: no args means kanban - the script's
# own help states it, and a wrong default silently rots the canary
if ! bash "$PURITY" --help 2>/dev/null | grep -q "kanban"; then
  bad "help does not name the kanban default"
else
  ok
fi

if [ "$fail" -gt 0 ]; then
  echo "nm-purity_test: $pass ok, $fail FAIL" >&2
  exit 1
fi
echo "nm-purity_test: $pass ok, 0 fail"
exit 0