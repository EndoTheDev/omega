#!/usr/bin/env bash
set -uo pipefail

# nm-purity_test.sh - cheap negative-branch tests for scripts/nm-purity.sh.
# The positive law (zero module symbols) is the canary itself: CI runs
# scripts/nm-purity.sh directly - one build inside a throwaway worktree,
# the test suite does not double it.

PURITY="$(cd "$(dirname "$0")" && pwd)/nm-purity.sh"

# winbtw seam (t_61673a95): the native tools this suite drives (python3,
# gofmt) cannot read MSYS /tmp paths, and mktemp seeds there by default -
# every scrub pass died invisibly and its assertions failed as lies.
# Repoint the temp base at a Windows-visible dir when cygpath exists;
# the Linux/CI lane has no cygpath and keeps today's /tmp behavior.
if command -v cygpath >/dev/null 2>&1 && [ -n "${LOCALAPPDATA:-}" ]; then
  WINTMP="$(cygpath -m "$LOCALAPPDATA/Temp" 2>/dev/null || true)"
  if [ -n "$WINTMP" ] && [ -d "$WINTMP" ]; then
    export TMPDIR="$WINTMP"
  fi
fi

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "FAIL: $1" >&2; }

# scrub - run the extracted scrub.py against the current fixture dir.
# stderr stays LIVE on purpose: a silently-skipped scrub reds every
# downstream assertion with lies - the failure mode that hid the
# winbtw breakage for weeks. A nonzero exit is counted here loud.
scrub() (
  cd "$TESTDIR" && exec python3 "$SCRUBPY" "$@"
)

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
STRIP_SED="$(grep "sed -E 's" "$PURITY" | sed -e "s/^.*sed -E '//" -e "s/'.*//" | head -1)"
if [ -z "$STRIP_SED" ]; then
  bad "noun check has no host-strip sed to extract"
else
  NOUNFILE="$(mktemp)"
  NOUNLINE='d17070 R go:itab.*github.com/EndoTheDev/omega/harness/terminal.ptyProcess,github.com/EndoTheDev/omega/harness/terminal.process'
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
      '0 R go:itab.*github.com/EndoTheDev/omega/harness/terminal.ptyProcess,main.process' \
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
scrub "config.go,knobs.go" memory >/dev/null || bad "scrub pass exited nonzero (memory fixture) - see stderr above"
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
scrub "config.go,sections.go" continuous >/dev/null || bad "scrub pass exited nonzero (continuous fixture) - see stderr above"
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
scrub "config.go,known.go" continuous >/dev/null || bad "scrub pass exited nonzero (known fixture) - see stderr above"
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

# 10. the named-instances strands (card t_4a1ed765):
#   - the composite-typed aggregate field (`Providers
#     map[string]ProviderConfig`) joins the SharedFields class and dies
#     with the capsule - the nightly red the card fixes;
#   - the type constructor and the make() type arg are value-position
#     wires;
#   - a func whose body emptied dies whole and its callers - even in a
#     file the scrub already passed - follow (the file-level carried
#     passes);
#   - a stdlib import whose last reader was the deleted func prunes;
#   - a param named exactly like the module (`provider string`)
#     survives: the bare module name is not a value-killer, only the
#     qualified use is the package wire.
TESTDIR="$(mktemp -d)"
cat > "$TESTDIR/config.go" <<'EOF'
package main

type ProviderConfig struct {
	Type      string
	ModelName string
}

type Config struct {
	Provider  ProviderConfig `yaml:"provider"`
	Providers map[string]ProviderConfig `yaml:"providers"`
	Server    string         `yaml:"server"`
}
EOF
cat > "$TESTDIR/calls.go" <<'EOF'
package main

func wrap(cfg *Config) {
	knobList(cfg)
	envOverride(cfg)
	keep := cfg.Server
	_ = keep
}
EOF
cat > "$TESTDIR/knobs.go" <<'EOF'
package main

import "sort"

func knobList(cfg *Config) []map[string]any {
	names := make([]string, 0, len(cfg.Providers))
	for n := range cfg.Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	return []map[string]any{{"name": names}}
}

func instances(cfg *Config, raw any) error {
	list := make(map[string]ProviderConfig, 1)
	_ = list
	return nil
}

func envOverride(cfg *Config) {
	if cfg.Provider.Type != "" {
		cfg.Provider.ModelName = "x"
	}
}

func passthrough(provider string) string {
	return provider
}
EOF
if [ -z "$SCRUBPY" ]; then
  SCRUBPY="$(mktemp)"
  awk '/^cat > "\$TMP\/scrub.py"/{f=1;next} /^PYEOF$/{f=0} f' "$PURITY" > "$SCRUBPY"
fi
# calls.go sits BEFORE knobs.go on purpose: pass 1 scrubs it before the
# funcs die in knobs.go, so only the carried-pass loop cleans its call
# sites.
scrub "config.go,calls.go,knobs.go" provider >/dev/null || bad "scrub pass exited nonzero (instances fixture) - see stderr above"
if ! grep -q 'knobList' "$TESTDIR/calls.go" \
  && ! grep -q 'envOverride' "$TESTDIR/calls.go" \
  && grep -q 'cfg.Server' "$TESTDIR/calls.go" \
  && ! grep -q 'knobList' "$TESTDIR/knobs.go" \
  && ! grep -q 'envOverride' "$TESTDIR/knobs.go" \
  && ! grep -q '"sort"' "$TESTDIR/knobs.go" \
  && grep -q 'return nil' "$TESTDIR/knobs.go" \
  && grep -q 'passthrough' "$TESTDIR/knobs.go" \
  && grep -q 'return provider' "$TESTDIR/knobs.go" \
  && ! grep -q 'ProviderConfig' "$TESTDIR/config.go" \
  && ! grep -q 'Providers' "$TESTDIR/config.go" \
  && grep -q 'Server' "$TESTDIR/config.go"; then
  ok
else
  bad "scrubber missed a named-instances strand (composite field, value use, emptied func, carried call site, stdlib import, bare module param)"
fi
for f in "$TESTDIR/config.go" "$TESTDIR/calls.go" "$TESTDIR/knobs.go"; do
  if ! gofmt -e "$f" > /dev/null 2>"$TESTDIR/gofmt.err"; then
    bad "scrubbed fixture does not parse: $(cat "$TESTDIR/gofmt.err")"
  else
    ok
  fi
done
rm -rf "$TESTDIR" "$SCRUBPY"

# 11. the comment-reader strand (card t_be5a81a2): the module's case
# body wires through multi-name locals (`host, key, model := slotWire(c)`)
# whose only consumer dies as a wire (the literal headed `return
# slot.Config{`). The comments above the case carry the bare neighbor
# words as prose - reading those prose sightings as readers false-keeps
# `host` and `model`, and the strand compiles red (declared and not
# used, the nightly red the card fixes). A comment is not a reader.
TESTDIR="$(mktemp -d)"
cat > "$TESTDIR/config.go" <<'EOF'
package main

type SlotConfig struct {
	Mode string
}

type Config struct {
	Slot SlotConfig `yaml:"slot"`
}
EOF
cat > "$TESTDIR/sections.go" <<'EOF'
package main

var _ = Config{}.Slot

func slotSections(c Config) (any, bool) {
	switch "slot" {
	case "slot":
		// mode/policy are the user's knobs; host and
		// model are wiring - the decision model rides
		// the provider settings slot.
		host, key, model := slotWire(c)
		return slot.Config{
			Mode:     c.Slot.Mode,
			HTTPHost: host,
			APIKey:   key,
			Model:    model,
		}, true
	}
	return nil, false
}

func slotWire(c Config) (string, string, string) {
	return "default-host", "default-key", "default-model"
}
EOF
SCRUBPY="$(mktemp)"
awk '/^cat > "\$TMP\/scrub.py"/{f=1;next} /^PYEOF$/{f=0} f' "$PURITY" > "$SCRUBPY"
scrub "config.go,sections.go" slot >/dev/null || bad "scrub pass exited nonzero (slot fixture) - see stderr above"
if [ -s "$TESTDIR/sections.go" ] \
  && ! grep -q 'slotWire(c)' "$TESTDIR/sections.go" \
  && grep -q 'func slotWire' "$TESTDIR/sections.go" \
  && grep -q 'return nil, false' "$TESTDIR/sections.go" \
  && ! grep -q 'SlotConfig' "$TESTDIR/config.go"; then
  ok
else
  bad "scrubber kept a declaration whose reader died (or ate slotWire / the literal-only return): $(cat "$TESTDIR/sections.go" 2>/dev/null)"
fi
for f in "$TESTDIR/config.go" "$TESTDIR/sections.go"; do
  if ! gofmt -e "$f" > /dev/null 2>"$TESTDIR/gofmt.err"; then
    bad "scrubbed fixture does not parse: $(cat "$TESTDIR/gofmt.err")"
  else
    ok
  fi
done
rm -rf "$TESTDIR" "$SCRUBPY"

# 12. the cascading-anchor strand (card t_91ad517a): every := LHS of a
# dying line is filed as dead vocabulary, and the value rule then kills
# any line whose bare words all name the dead - a live helper's
# parameter declaration is such a word. The dying capsule family must
# never anchor a maximally-common letter: its cascading assignment
# carries a SEMANTIC local name (distinct), not `n`, or the live helper
# that shares the letter dies in the same strip while its call sites
# stay - the undefined-symbol red that burned the provider proof.
TESTDIR="$(mktemp -d)"
cat > "$TESTDIR/config.go" <<'EOF'
package main

type ProviderConfig struct {
	Type      string
	Host      string
	APIKey    string
}

type Config struct {
	Provider  ProviderConfig            `yaml:"provider"`
	Providers map[string]ProviderConfig `yaml:"providers"`
	Server    string                    `yaml:"server"`
}
EOF
cat > "$TESTDIR/knobs.go" <<'EOF'
package main

func knobs(cfg *Config) []any {
	return []any{
		{Key: "server.port", Label: "port", Type: "number",
			Min: ptrF(1), Step: ptrF(1)},
		{Key: "server.bind", Label: "bind", Type: "text",
			Value: cfg.Server},
	}
}

func ptrF(n float64) *float64 { return &n }

func applyInstances(cfg *Config) error {
	inst := ProviderConfig{}
	inst.Host = "h"
	inherited, distinct := renameOff(cfg, inst.Host)
	if distinct == 0 {
		return nil
	}
	inst.APIKey = inherited
	cfg.Providers = map[string]ProviderConfig{"x": inst}
	return nil
}

func renameOff(cfg *Config, want string) (string, int) {
	for name, inst := range cfg.Providers {
		if inst.Host != want {
			continue
		}
		return inst.APIKey, 1
	}
	return "", 0
}
EOF
SCRUBPY="$(mktemp)"
awk '/^cat > "\$TMP\/scrub.py"/{f=1;next} /^PYEOF$/{f=0} f' "$PURITY" > "$SCRUBPY"
scrub "config.go,knobs.go" provider >/dev/null || bad "scrub pass exited nonzero - see stderr above"
if grep -q 'func ptrF' "$TESTDIR/knobs.go" \
  && grep -q 'Min: ptrF(1)' "$TESTDIR/knobs.go" \
  && ! grep -q 'ProviderConfig' "$TESTDIR/knobs.go" \
  && ! grep -q 'cfg.Providers' "$TESTDIR/knobs.go"; then
  ok
else
  bad "scrubber killed the live helper through the dying family's anchored parameter name (or kept capsule vocabulary)"
fi
for f in "$TESTDIR/config.go" "$TESTDIR/knobs.go"; do
  if ! gofmt -e "$f" > /dev/null 2>"$TESTDIR/gofmt.err"; then
    bad "scrubbed fixture does not parse: $(cat "$TESTDIR/gofmt.err")"
  else
    ok
  fi
done
rm -rf "$TESTDIR" "$SCRUBPY"

# 13. the teeth of 12 (gate law: a deliberately-broken shape stays red).
# The same fixture with the BROKEN shape restored - the one-letter `n`
# back on the dying cascade line - must RED: the anchored letter kills
# the live helper's signature while its call sites survive, exactly the
# undefined-symbol red that burned the provider proof. Without this
# witness, softening the value rule goes silently green.
TESTDIR="$(mktemp -d)"
cat > "$TESTDIR/config.go" <<'EOF'
package main

type ProviderConfig struct {
	Type      string
	Host      string
	APIKey    string
}

type Config struct {
	Provider  ProviderConfig            `yaml:"provider"`
	Providers map[string]ProviderConfig `yaml:"providers"`
	Server    string                    `yaml:"server"`
}
EOF
cat > "$TESTDIR/knobs.go" <<'EOF'
package main

func knobs(cfg *Config) []any {
	return []any{
		{Key: "server.port", Label: "port", Type: "number",
			Min: ptrF(1), Step: ptrF(1)},
		{Key: "server.bind", Label: "bind", Type: "text",
			Value: cfg.Server},
	}
}

func ptrF(n float64) *float64 { return &n }

func applyInstances(cfg *Config) error {
	inst := ProviderConfig{}
	inst.Host = "h"
	inherited, n := renameOff(cfg, inst.Host)
	if n == 0 {
		return nil
	}
	inst.APIKey = inherited
	cfg.Providers = map[string]ProviderConfig{"x": inst}
	return nil
}

func renameOff(cfg *Config, want string) (string, int) {
	for name, inst := range cfg.Providers {
		if inst.Host != want {
			continue
		}
		return inst.APIKey, 1
	}
	return "", 0
}
EOF
SCRUBPY="$(mktemp)"
awk '/^cat > "\$TMP\/scrub.py"/{f=1;next} /^PYEOF$/{f=0} f' "$PURITY" > "$SCRUBPY"
scrub "config.go,knobs.go" provider >/dev/null || bad "scrub pass exited nonzero - see stderr above"
if ! grep -q 'func ptrF' "$TESTDIR/knobs.go" \
  && grep -q 'Min: ptrF(1)' "$TESTDIR/knobs.go"; then
  ok
else
  bad "the deliberately-broken n-anchored shape no longer reds - the value rule was softened or the anchor law lost its teeth"
fi
rm -rf "$TESTDIR" "$SCRUBPY"

# 14. the anchor-scope strand (card t_f77dd67d): the := names a dying
# wire line files are FUNCTION-scoped - Go gives a := name the scope of
# its block - while dead_ids is file-wide. An anchor born inside a
# dying capsule func therefore killed a LIVE sibling helper of the
# same file that merely used the same spelling. cmd/omega/settings.go
# died this way: providerInstancesKnob anchored `out`, and settingsArgs
# (a pure argv helper, no capsule wire at all) lost its declaration,
# its append and its `return out` to the value rule - "missing return"
# in the scrubbed tree, provider the only red in the --all rotation.
TESTDIR="$(mktemp -d)"
cat > "$TESTDIR/config.go" <<'EOF'
package main

type ProviderConfig struct {
	Type string
}

type Config struct {
	Provider  ProviderConfig            `yaml:"provider"`
	Providers map[string]ProviderConfig `yaml:"providers"`
	Server    string                    `yaml:"server"`
}
EOF
cat > "$TESTDIR/knobs.go" <<'EOF'
package main

func providerInstancesKnob(cfg *Config) []map[string]any {
	names := make([]string, 0, len(cfg.Providers))
	for n := range cfg.Providers {
		names = append(names, n)
	}
	out := make([]map[string]any, 0, len(names))
	for _, n := range names {
		out = append(out, map[string]any{"name": n})
	}
	return out
}

func settingsArgs(rest []string) []string {
	out := make([]string, 0, len(rest))
	for i := 0; i < len(rest); i++ {
		out = append(out, rest[i])
	}
	return out
}
EOF
SCRUBPY="$(mktemp)"
awk '/^cat > "\$TMP\/scrub.py"/{f=1;next} /^PYEOF$/{f=0} f' "$PURITY" > "$SCRUBPY"
scrub "config.go,knobs.go" provider >/dev/null || bad "scrub pass exited nonzero (anchor-scope fixture) - see stderr above"
if ! grep -q 'providerInstancesKnob' "$TESTDIR/knobs.go" \
  && grep -q 'func settingsArgs' "$TESTDIR/knobs.go" \
  && grep -q 'out := make(\[\]string, 0, len(rest))' "$TESTDIR/knobs.go" \
  && grep -q 'return out' "$TESTDIR/knobs.go" \
  && ! grep -q 'Provider' "$TESTDIR/config.go"; then
  ok
else
  bad "an anchored local leaked out of its function and ate the live sibling's declaration/return: $(cat "$TESTDIR/knobs.go" 2>/dev/null)"
fi
for f in "$TESTDIR/config.go" "$TESTDIR/knobs.go"; do
  if ! gofmt -e "$f" > /dev/null 2>"$TESTDIR/gofmt.err"; then
    bad "scrubbed fixture does not parse: $(cat "$TESTDIR/gofmt.err")"
  else
    ok
  fi
done
rm -rf "$TESTDIR" "$SCRUBPY"

# 15. the teeth of 14 (gate law: a deliberately-broken shape stays red).
# Scoping the anchor must not neuter it: a local anchored by a dying
# wire line still cascades INSIDE its own function. Here `out` reads
# the anchored `names`, so the whole wired func must die; a scrubber
# that simply stopped anchoring would leave `out` behind reading a
# removed `names` - a build red, not a green proof.
TESTDIR="$(mktemp -d)"
cat > "$TESTDIR/config.go" <<'EOF'
package main

type ProviderConfig struct {
	Type string
}

type Config struct {
	Provider  ProviderConfig            `yaml:"provider"`
	Providers map[string]ProviderConfig `yaml:"providers"`
}
EOF
cat > "$TESTDIR/knobs.go" <<'EOF'
package main

func providerInstancesKnob(cfg *Config) []map[string]any {
	names := make([]string, 0, len(cfg.Providers))
	for n := range cfg.Providers {
		names = append(names, n)
	}
	out := make([]map[string]any, 0, len(names))
	return out
}
EOF
SCRUBPY="$(mktemp)"
awk '/^cat > "\$TMP\/scrub.py"/{f=1;next} /^PYEOF$/{f=0} f' "$PURITY" > "$SCRUBPY"
scrub "config.go,knobs.go" provider >/dev/null || bad "scrub pass exited nonzero (anchor-teeth fixture) - see stderr above"
if ! grep -q 'providerInstancesKnob' "$TESTDIR/knobs.go" \
  && ! grep -q '\bnames\b' "$TESTDIR/knobs.go" \
  && ! grep -q '\bout\b' "$TESTDIR/knobs.go"; then
  ok
else
  bad "the anchor stopped cascading inside its own function - a removed wire line stranded a live local: $(cat "$TESTDIR/knobs.go" 2>/dev/null)"
fi
rm -rf "$TESTDIR" "$SCRUBPY"

if [ "$fail" -gt 0 ]; then
  echo "nm-purity_test: $pass ok, $fail FAIL" >&2
  exit 1
fi
echo "nm-purity_test: $pass ok, 0 fail"
exit 0