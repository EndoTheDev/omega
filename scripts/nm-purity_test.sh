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

# 4. the noun check is scoped to omega-owned symbols: the owned-symbol
# filter exists and its result file is what the noun greps. The
# nightly proved the unscoped form: robfig/cron (continuous's
# scheduler), modernc.org/memory (the sqlite driver) and the
# crypto stdlib all carry capsule nouns through no wire of ours.
if ! grep -q 'EndoTheDev/omega/' "$PURITY"; then
  bad "noun check has no omega-owned scope filter"
else
  ok
fi
if grep -q 'grep -E .*EndoTheDev.*MOD\.own' "$PURITY" || grep -q 'grep -w "$MOD" "$TMP/$MOD.own"' "$PURITY"; then
  ok
else
  bad "noun check does not grep the owned-symbols file"
fi

# 5. nm output is read once into a file, never piped straight into
# grep -wq: under pipefail that exits at the first match, kills the
# upstream nm with SIGPIPE, and the pipeline's 141 reads as no-match -
# the noun check became a coin flip (the card proves it both ways).
if grep -q 'grep -wq "$MOD"' "$PURITY"; then
  bad "noun check pipes nm into grep -wq - the SIGPIPE no-op is back"
else
  ok
fi

# 6. the scope filter bites for real: the script's own own-symbol grep
# runs against a synthetic nm table carrying the exact nightly evidence
# (run 40563853766). Third-party nouns drop; omega-owned symbol paths
# and main-scope type descriptors stay in scope.
SCOPE_LINE="$(grep -n "grep -E 'github" "$PURITY" | head -1 | cut -d: -f1)"
SCOPE_PAT="$(sed -n "${SCOPE_LINE}p" "$PURITY" | sed -e "s/.*grep -E '//" -e "s/'.*//")"
OWNFILE="$(mktemp)"
printf '%s\n' \
  '0 T github.com/robfig/cron/v3.Cron.Run' \
  '0 T modernc.org/memory.Memory' \
  '0 T crypto/internal/fips140/drbg.memory' \
  '0 D type:.eq.struct { Title string "title"; Verdict string "verdict" }' \
  > "$OWNFILE"
printf '%s\n' \
  '0 T github.com/EndoTheDev/omega/cmd/omega/config.(*Server).run' \
  '0 T main.runtimeArgs' \
  '0 D type:.eq.struct { Skills main.SkillsConfig; Memory main.MemoryConfig }' \
  >> "$OWNFILE"
grep -E "$SCOPE_PAT" "$OWNFILE" > "$OWNFILE.out" 2>/dev/null || true
grep -q 'EndoTheDev\|main\.' "$OWNFILE.out" && ! grep -q 'robfig\|modernc\|crypto/internal\|Verdict string' "$OWNFILE.out" \
  && ok || bad "scope filter keeps third-party nouns or drops omega-owned ones"
rm -f "$OWNFILE" "$OWNFILE.out"

# 7. the scrubber kills a composite-literal element WHOLE when its
# wire is the tail line: `Value: cfg.Memory.File, Help: "..."},`
# carries the element's closing brace, and deleting it alone strands
# the opener - Go reads the stranded head as the next element (the
# syntax error that kept the memory capsule red).
TESTDIR="$(mktemp -d)"
cat > "$TESTDIR/config.go" <<'EOF'
package main

type MemoryConfig struct {
	Enabled bool
	File    string
}

type Config struct {
	Memory MemoryConfig
}
EOF
cat > "$TESTDIR/knobs.go" <<'EOF'
package main

func knobs(cfg *Config) []int {
	return []int{
		{Key: "memory.enabled", Label: "enabled", Type: "toggle",
			Value: cfg.Memory.Enabled,
			Help:  "keep me"},
		{Key: "memory.file", Label: "memory file", Type: "text",
			Value: cfg.Memory.File, Help: "applies on restart"},
		{Key: "other.alive", Label: "alive", Type: "text",
			Value: cfg.Other.Fine},
	}
}
EOF
SCRUBPY="$(mktemp)"
awk '/^cat > "\$TMP\/scrub.py"/{f=1;next} /^PYEOF$/{f=0} f' "$PURITY" > "$SCRUBPY"
(cd "$TESTDIR" && python3 "$SCRUBPY" "config.go,knobs.go" memory) >/dev/null 2>&1
if [ -s "$TESTDIR/knobs.go" ] \
  && ! grep -q 'Key: "memory.file"' "$TESTDIR/knobs.go" \
  && ! grep -q 'Memory' "$TESTDIR/knobs.go" \
  && grep -q 'Key: "memory.enabled"' "$TESTDIR/knobs.go" \
  && grep -q 'keep me' "$TESTDIR/knobs.go" \
  && grep -q 'other.alive' "$TESTDIR/knobs.go" \
  && ! grep -q 'MemoryConfig' "$TESTDIR/config.go" \
  && ! grep -q 'Memory MemoryConfig' "$TESTDIR/config.go"; then
  ok
else
  bad "scrubber did not kill the wire-tail element whole (or over-killed the live knobs)"
fi
# and what survived must parse as Go - stranded openers fail here loud
for f in "$TESTDIR/config.go" "$TESTDIR/knobs.go"; do
  if ! gofmt -e "$f" > /dev/null 2>"$TESTDIR/gofmt.err"; then
    bad "scrubbed fixture does not parse: $(cat "$TESTDIR/gofmt.err")"
  else
    ok
  fi
done
rm -rf "$TESTDIR" "$SCRUBPY"

if [ "$fail" -gt 0 ]; then
  echo "nm-purity_test: $pass ok, $fail FAIL" >&2
  exit 1
fi
echo "nm-purity_test: $pass ok, 0 fail"
exit 0