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
if grep -q 'grep -E .*EndoTheDev.*MOD\.own' "$PURITY" || grep -q 'grep -w "$MOD" "$TMP/$MOD.own.host"' "$PURITY"; then
  ok
else
  bad "noun check does not grep the host-scoped owned-symbols file"
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

# 6b. the noun check excludes another capsule's package vocabulary, so
# the go1.26.10 runner's NAMED terminal itab (t_1b96003f nightly red)
# cannot count a DIFFERENT module's type name against this capsule.
# The full pipeline on a synthetic table: scope filter, host strip,
# then the word-grep. The exact failing itab is green for --module
# process; a host-scope noun still reds, and it reds where it lives -
# an itab with a mixed shape (modules operand + main.process operand)
# keeps the host operand after the strip. The -w word law is unchanged:
# main.processRunner is a substring, not the word, and stays green.
STRIP_SED="$(grep "sed -E 's|" "$PURITY" | sed -e "s/^.*sed -E '//" -e "s/'.*//" | head -1)"
if [ -z "$STRIP_SED" ]; then
  bad "noun check has no host-strip sed to extract"
else
  NOUNFILE="$(mktemp)"
  NOUNLINE='d17070 R go:itab.*github.com/EndoTheDev/omega/modules/terminal.ptyProcess,github.com/EndoTheDev/omega/modules/terminal.process'
  case_green() {
    printf '%s\n' \
      "$NOUNLINE" \
      '0 T github.com/robfig/cron/v3.Cron.Run' \
      '0 T github.com/EndoTheDev/omega/cmd/omega/config.(*Server).run' \
      '0 T main.runtimeArgs' \
      '0 T main.processRunner' \
    > "$NOUNFILE.nm"
    grep -E 'github\.com/EndoTheDev/omega/|(^|[^/])main\.' "$NOUNFILE.nm" > "$NOUNFILE.own" 2>/dev/null || true
    sed -E "$STRIP_SED" "$NOUNFILE.own" > "$NOUNFILE.host" 2>/dev/null || true
    if grep -w process "$NOUNFILE.host" > "$NOUNFILE.log" 2>&1; then
      bad "the terminal itab still counts against the process noun check: $(cat "$NOUNFILE.log")"
    else
      ok
    fi
    grep -q 'main.processRunner' "$NOUNFILE.host" \
      && ! grep -wq process "$NOUNFILE.host" \
      && ok || bad "host scope lost from the noun check (processRunner fixture vanished)"
  }
  case_green
  case_red() {
    printf '%s\n' \
      'd17071 R go:itab.*main.shell,main.process' \
      '0 R go:itab.*github.com/EndoTheDev/omega/modules/terminal.ptyProcess,main.process' \
      '0 T main.(*agent).process' \
    > "$NOUNFILE.nm"
    grep -E 'github\.com/EndoTheDev/omega/|(^|[^/])main\.' "$NOUNFILE.nm" > "$NOUNFILE.own" 2>/dev/null || true
    sed -E "$STRIP_SED" "$NOUNFILE.own" > "$NOUNFILE.host" 2>/dev/null || true
    if grep -wq process "$NOUNFILE.host"; then
      ok
      grep -wq 'main\.process' "$NOUNFILE.host" \
        && ok || bad "host operand did not survive the strip: $(grep -w process "$NOUNFILE.host")"
    else
      bad "host-scope process noun read GREEN (the exclusion over-stripped)"
    fi
  }
  case_red
fi
rm -f "$NOUNFILE.nm" "$NOUNFILE.own" "$NOUNFILE.host" "$NOUNFILE.log"

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
# 8. a return line whose values died with their definitions dies: the
# sectionConfig case copies the module's section into a local and hands
# it back - `return cont, true` names an anchor the deletion removed.
# A literal-only return (`return nil, false` trailing the switch) is
# not a wire and survives.
TESTDIR="$(mktemp -d)"
cat > "$TESTDIR/config.go" <<'EOF'
package main

type ContinuousConfig struct {
	Home string
}

type Config struct {
	Continuous ContinuousConfig
}

type Disabled struct {
	Agents []string
}
EOF
cat > "$TESTDIR/sections.go" <<'EOF'
package main

func section(c Config) (any, bool) {
	switch "continuous" {
	case "continuous":
		cont := c.Continuous
		cont.Home = "replaced by the module"
		return cont, true
	}
	return nil, false
}

var _ = Config{}.Disabled
EOF
SCRUBPY="$(mktemp)"
awk '/^cat > "\$TMP\/scrub.py"/{f=1;next} /^PYEOF$/{f=0} f' "$PURITY" > "$SCRUBPY"
(cd "$TESTDIR" && python3 "$SCRUBPY" "config.go,sections.go" continuous) >/dev/null 2>&1
if [ -s "$TESTDIR/sections.go" ] \
  && ! grep -q 'Cont' "$TESTDIR/sections.go" \
  && grep -q 'return nil, false' "$TESTDIR/sections.go" \
  && ! grep -q 'ContinuousConfig' "$TESTDIR/config.go"; then
  ok
else
  bad "scrubber kept a return of dead definitions or ate the literal-only return"
fi
for f in "$TESTDIR/config.go" "$TESTDIR/sections.go"; do
  if ! gofmt -e "$f" > /dev/null 2>"$TESTDIR/gofmt.err"; then
    bad "scrubbed fixture does not parse: $(cat "$TESTDIR/gofmt.err")"
  else
    ok
  fi
done

# 9. a short declaration whose last reader died as a wire block strands
# an unused name - Go refuses it. The unread name blanks in place and
# the error path keeps its declaration.
cat > "$TESTDIR/config.go" <<'EOF'
package main

type ContinuousConfig struct {
	Home string
}

type Config struct {
	Continuous ContinuousConfig
}

type Disabled struct {
	Agents []string
}
EOF
cat > "$TESTDIR/known.go" <<'EOF'
package main

import "errors"

type Profile struct {
	Continuous bool
}

func LoadProfile(home, name string) (*Profile, error) {
	_ = home
	_ = name
	return &Profile{Continuous: true}, nil
}

var errRefused = errors.New("refused")

func agentKnown(name string) error {
	p, err := LoadProfile("home", name)
	if err != nil {
		return err
	}
	if !p.Continuous {
		return errRefused
	}
	return nil
}
EOF
(cd "$TESTDIR" && python3 "$SCRUBPY" "config.go,known.go" continuous) >/dev/null 2>&1
if grep -q '_, err := LoadProfile' "$TESTDIR/known.go" \
  && ! grep -q 'p.Continuous' "$TESTDIR/known.go"; then
  ok
else
  bad "scrubber did not blank the declaration nobody reads anymore"
fi
if ! gofmt -e "$TESTDIR/known.go" > /dev/null 2>"$TESTDIR/gofmt.err"; then
  bad "scrubbed fixture does not parse: $(cat "$TESTDIR/gofmt.err")"
else
  ok
fi
rm -rf "$TESTDIR" "$SCRUBPY"

if [ "$fail" -gt 0 ]; then
  echo "nm-purity_test: $pass ok, $fail FAIL" >&2
  exit 1
fi
echo "nm-purity_test: $pass ok, 0 fail"
exit 0