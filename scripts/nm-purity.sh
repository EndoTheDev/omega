#!/usr/bin/env bash
set -euo pipefail

# nm-purity.sh - the binary-purity machine law (v0.12 phase F, proof 1).
#
# For the module(s) named, scrub every wire to it out of a copy of the
# tree, build the binary, and assert zero module-noun symbols survive
# in `go tool nm` output. The proof is mechanical: the ONLY way a module
# stays pure-removable is that its removal compiles and the binary
# carries none of its symbols - module dir deleted, import lines gone,
# wire lines gone, leftover empty blocks pruned, build green.
#
# A red run means one of two things: the module grew a wire the scrubber
# does not model (the removal recipe doc in docs/removing-a-module.md is
# now lying), or the module's nouns leaked into an engine layer. Both
# are loud failures (Q4: the law is a machine check, not a promise).
#
# Usage:
#     scripts/nm-purity.sh                 # the kanban canary
#     scripts/nm-purity.sh --module delegate # one named module
#     scripts/nm-purity.sh --all           # every capsule (the nightly rotation)
#
# Exit codes: 0 all green, 1 one or more modules impure, 2 usage/IO error.

MODULES=()
while [ $# -gt 0 ]; do
  case "$1" in
    --all)
      # rotation: every capsule not in the removal-needs manifest
      # (scripts/nm-purity.modules). Manifest entries are reported as
      # SKIP with their reason - a known finding, not a nightly red.
      for d in modules/*/; do MODULES+=("$(basename "$d")"); done ;;
    --module) shift; MODULES+=("$1") ;;
    -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
    *) echo "nm-purity: unknown arg '$1'" >&2; exit 2 ;;
  esac
  shift
done
[ ${#MODULES[@]} -gt 0 ] || MODULES=(kanban)

MANIFEST="$(cd "$(dirname "$0")" && pwd)/nm-purity.modules"
MANIFESTED=()
if [ -f "$MANIFEST" ]; then
  # shellcheck disable=SC2207
  MANIFESTED=($(sed -e 's/#.*$//' -e 's/[[:space:]].*$//' "$MANIFEST" | grep -v '^$'))
fi

RUN=()
for MOD in "${MODULES[@]}"; do
  skip=""
  for M in "${MANIFESTED[@]:-}"; do
    [ "$MOD" = "$M" ] && skip="$M"
  done
  if [ -n "$skip" ]; then
    reason="$(grep -E "^$MOD[[:space:]]" "$MANIFEST" | sed -e 's/^[^[:space:]]*//' -e 's/^[[:space:]]*//' )"
    echo "nm-purity [$MOD]: SKIP - removal-needs manifest: $reason"
  else
    RUN+=("$MOD")
  fi
done
[ ${#RUN[@]} -gt 0 ] || { echo "nm-purity: every requested module is manifested - nothing to prove" >&2; exit 2; }
MODULES=("${RUN[@]}")

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d "${TMPDIR:-/tmp}/nm-purity.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT

# The scrubber: deletes the module's wires from a .go file and prunes
# the empty constructs the deletions leave behind. Fixpoint loop so
# nested orphans resolve. Dead identifiers grow as it goes: a deleted
# "a, b := module.X()" kills every later read of a and b.
# The scrubber: deletes the module's wires from a .go file and prunes
# the orphaned constructs the deletions leave behind. The cascade is
# confined to the declarations that reference the module's identifiers
# - an unrelated function keeps every line, ever (the false-positive
# rule that ate `return nil` lives here no more).
cat > "$TMP/scrub.py" <<'PYEOF'
import re, sys

# Host config vocabulary, shared across every file in one run: the
# module's config struct is module-shaped vocabulary wherever it sits,
# and the aggregate Config field naming it is read in many files. The
# class is narrow on purpose: a `type <Noun>Config` host struct and the
# aggregate field typed by it (or by the module alias: `Continuous
# continuous.Config`). The noun in the type NAME is the marker -
# trustFlags-style hosts that merely mention the word are not config
# structs and are left alone.
SharedTypes = {}
SharedFields = {}

def seed_shared(names, files):
    """Collect each module noun's host config vocabulary by scanning
    the ORIGINAL (pre-scrub) tree, so the vocabulary is known before
    the first file is rewritten."""
    for name in names:
        SharedTypes.setdefault(name, set())
        SharedFields.setdefault(name, set())
        for fname in files:
            with open(fname) as f:
                text = f.read()
            # strip line comments: a doc comment naming the struct is
            # prose, not a declaration
            body = re.sub(r"//[^\n]*", "", text)
            for m in re.finditer(r"^type\s+([A-Za-z_]\w*[Cc]onfig)\b", body, re.M):
                if name in m.group(1).lower():
                    SharedTypes[name].add(m.group(1))
            for line in body.split("\n"):
                m = re.match(r"^\s*([A-Za-z_]\w*)\s+\*?([A-Za-z_][\w\.]*)\s*$", line)
                if m and name in m.group(2).lower() and m.group(2).lower().endswith("config"):
                    SharedFields[name].add(m.group(1))

def scrub(text, modules):
    """modules: list of module names. Returns scrubbed text."""
    mnames = [m for m in modules]
    lines = text.split("\n")
    dead_ids = set()
    dead_fields = set()

    # the module itself, plus its host config vocabulary (the
    # <Noun>Config struct type and the aggregate field naming it)
    for name in mnames:
        dead_ids.add(name)
        dead_ids.update(SharedTypes.get(name, ()))
        dead_fields.update(SharedFields.get(name, ()))

    # 1. import lines + alias discovery (file-level)
    keep = []
    for ln in lines:
        deleted = False
        for name in mnames:
            m = re.match(r'^(\s*)([A-Za-z_][A-Za-z0-9_]*\s+)?"[^"]*/modules/' + re.escape(name) + r'"(\s*//.*)?$', ln)
            if m:
                alias = (m.group(2) or "").strip() or name
                for tok in re.split(r"[,\s]+", alias):
                    if tok:
                        dead_ids.add(tok)
                deleted = True
        if not deleted:
            keep.append(ln)
    for name in mnames:
        dead_ids.add(name)

    def dead_word(ln):
        body = re.sub(r"`[^`]*`", "", ln)
        body = re.sub(r'"(\\.|[^"\\])*"', '""', body)
        body = re.sub(r"'[^']*'", "''", body)
        m = re.search(r"\b((" + "|".join(re.escape(i) for i in sorted(dead_ids)) + r"))\b", body)
        return m.group(1) if m else None

    # 2. split into top-level declaration chunks
    decl = re.compile(r"^(func|type|var|const)\b|^\}", re.M)
    chunks = []
    cur = []
    depth = 0
    for ln in keep:
        cur.append(ln)
        depth += ln.count("{") - ln.count("}")
        if depth <= 0 and cur and (ln.strip() == "}" or ln.strip() == "()"):
            chunks.append(cur)
            cur = []
        if depth <= 0 and ln.strip() == ")":
            chunks.append(cur)
            cur = []
    if cur:
        chunks.append(cur)

    keywords = {"true", "false", "nil", "if", "else", "for", "range",
                "return", "switch", "case", "default", "break", "continue",
                "func", "var", "const", "type", "struct", "map", "string", "_", "package", "import"}

    def only_dead_words(ln):
        body = re.sub(r"`[^`]*`", "", ln)
        body = re.sub(r'"(\\.|[^"\\])*"', '""', body)
        body = re.sub(r"'[^']*'", "''", body)
        words = re.findall(r"[A-Za-z_][A-Za-z0-9_]*", body)
        code = [w for w in words if w not in keywords]
        return bool(code) and all(w in dead_ids for w in code)

    # Sentinels collect_anchors must never deaden: err/ok are
    # re-declared per statement all over Go code - one wire sighting of
    # "ptr, err := module.F()" makes ptr dead, never err. And a
    # statement keyword must never deaden: the := anchor regex on
    # "if x, err := module.F()" grabs "if" too, and a dead "if" deletes
    # every conditional in the chunk.
    reserved = {"err", "ok", "_"}

    def add_anchor(name):
        if name and name not in reserved and name not in keywords:
            dead_ids.add(name)

    def collect_anchors(ln):
        body = re.sub(r"`[^`]*`", "", ln)
        body = re.sub(r'"(\\.|[^"\\])*"', '""', body)
        m = re.match(r"^\s*([A-Za-z_][A-Za-z0-9_,\s]*)\s*:=", body)
        if m:
            for tok in re.split(r"[,\s]+", m.group(1).strip()):
                if tok:
                    add_anchor(tok)
        # struct-field shape (a deleted field line: NAME TYPE): the
        # field's name dies with it - every .NAME access downstream
        # reads a struct that no longer has it.
        m = re.match(r"^\s*([A-Za-z_][A-Za-z0-9_]*)\s+[A-Za-z_\*\[\]][\w\.\*\[\],\s]*$", body)
        if m and m.group(1) not in keywords:
            dead_fields.add(m.group(1))

    def is_wire_line(ln):
        body = re.sub(r"`[^`]*`", "", ln)
        body = re.sub(r'"(\\.|[^"\\])*"', '""', body)
        body = re.sub(r"'[^']*'", "''", body)
        for ident in dead_ids:
            if re.search(r"\b" + re.escape(ident) + r"\.", body):
                return True
        # a deleted struct field's access sites are wires too
        for ident in dead_fields:
            if re.search(r"\." + re.escape(ident) + r"\b", body):
                return True
        stripped = ln.strip()
        # a bare identifier or identifier list (list items, re-assigns)
        if re.match(r"^((" + "|".join(re.escape(i) for i in sorted(dead_ids)) + r"))\s*(,\s*)*$", stripped):
            return True
        # a struct field typed by a dead host-config type - the tag is
        # string-stripped above, so the line body reads `Skills
        # SkillsConfig`; the field's own name dies with it (access
        # sites read a struct that no longer carries it)
        m = re.match(r"^([A-Za-z_]\w*)\s+\*?([A-Za-z_]\w*)\s*(//.*)?$", body)
        if m and m.group(2) in dead_ids:
            dead_fields.add(m.group(1))
            return True
        # a composite-literal key typed by a dead host-config type
        # (`Kanban: KanbanConfig{` in DefaultConfig): dies with its
        # braces - the wire-headed block rule below eats the literal
        m = re.match(r"^([A-Za-z_]\w*)\s*:\s*\*?([A-Za-z_]\w*)\{", body)
        if m and m.group(2) in dead_ids:
            dead_fields.add(m.group(1))
            return True
        return False

    # mark_word: chunk GUARD matcher. Unlike dead_word it reads quoted
    # strings - a switch case naming the module in a string (case
    # "x":), a config-section key, is a wire into the chunk even
    # though the word appears nowhere else in live code. Marking only
    # ENTERS the fixpoint; every deletion still needs a wire rule.
    def mark_word(ln):
        body = re.sub(r"`[^`]*`", " ` ", ln)
        m = re.search(r"\b((" + "|".join(re.escape(i) for i in sorted(dead_ids)) + r"))\b", body)
        if m:
            return m.group(1)
        # a dead host-config field's access sites are wires into the
        # chunk even when the chunk nowhere names the module (the
        # applyEnv entry table, the settings key handlers)
        for ident in sorted(dead_fields):
            if re.search(r"\." + re.escape(ident) + r"\b", body):
                return ident
        return None

    def scrub_chunk(chunk):
        # a chunk untouched by the module keeps every line
        if not any(dead_word(ln) or mark_word(ln) for ln in chunk):
            return chunk
        # a host-config type named for the module dies whole: struct,
        # doc comment and all (the leading comment belongs to it)
        i0 = 0
        while i0 < len(chunk) and (not chunk[i0].strip() or chunk[i0].lstrip().startswith("//")):
            i0 += 1
        m = re.match(r"type\s+([A-Za-z_]\w*)\b", chunk[i0].strip()) if i0 < len(chunk) else None
        if m and m.group(1) in dead_ids:
            return []
        for _ in range(15):
            changed = False
            out = []
            i = 0
            while i < len(chunk):
                ln = chunk[i]
                if is_wire_line(ln):
                    collect_anchors(ln)
                    if ln.rstrip().endswith("{"):
                        # a wire-headed conditional dies WITH its block:
                        # the condition is the module's, so every branch
                        # under it is unreachable wiring. Inner
                        # declarations die with the block (their
                        # orphaned references cascade after).
                        depth3 = ln.count("{") - ln.count("}")
                        j2 = i + 1
                        while depth3 > 0 and j2 < len(chunk):
                            depth3 += chunk[j2].count("{") - chunk[j2].count("}")
                            j2 += 1
                        for b in chunk[i + 1:j2]:
                            collect_anchors(b)
                        i = j2
                    else:
                        i += 1
                    changed = True
                    continue
                # RHS = single dead value -> the line dies
                m = re.match(r'^([^=]+)=\s*([A-Za-z_][A-Za-z0-9_]*)\s*(//.*)?$', ln.strip())
                if m and m.group(2) in dead_ids:
                    i += 1
                    changed = True
                    continue
                m = re.match(r"^(\s*)(if|for|switch)\b", ln)
                if m:
                    # block extent: a statement head whose first line
                    # carries no open brace either opens the body on a
                    # LATER line (multi-line condition) or is a
                    # one-liner. Scan forward from the head line: the
                    # block ends when braces balance after a body
                    # opened, or the head line closes itself.
                    depth2 = 0
                    opened = False
                    j = i
                    while j < len(chunk):
                        d2 = chunk[j].count("{") - chunk[j].count("}")
                        if d2 > 0:
                            opened = True
                        depth2 += d2
                        j += 1
                        if depth2 == 0 and (opened or not chunk[j - 1].rstrip().endswith(
                                ("||", "&&", ",", "(", "["))):
                            break
                    if not opened:
                        j = i + 1  # head line only (the old one-liner read)
                    if depth2 == 0:
                        block = chunk[i:j]
                        code = [b for b in block[1:-1]
                                if b.strip() and not b.strip().startswith("//")]
                        # (a) the condition references a dead identifier
                        if dead_word(ln):
                            for b in code:
                                collect_anchors(b)
                            i = j
                            changed = True
                            continue
                        # (b) every code word of the body is dead
                        if code and all(only_dead_words(b) for b in code):
                            for b in code:
                                collect_anchors(b)
                            i = j
                            changed = True
                            continue
                        # (c) the body emptied out (comments may remain)
                        if not code or all(b.strip() == "" or b.strip().startswith("//") for b in code):
                            i = j
                            changed = True
                            continue
                # orphaned switch case: its body is gone
                m = re.match(r"^(\s*)(case\b|default:)", ln)
                nxt = chunk[i + 1].strip() if i + 1 < len(chunk) else "}"
                if m and (nxt.startswith("case ") or nxt.startswith("default:") or nxt.startswith("}")):
                    i += 1
                    changed = True
                    continue
                out.append(ln)
                i += 1
            chunk = out
            if not changed:
                break
        return chunk

    out = []
    for ch in chunks:
        out.extend(scrub_chunk(ch))
    return "\n".join(out)

if __name__ == "__main__":
    modspecs = [s.split("=")[-1] for s in sys.argv[2:]]
    files = sys.argv[1].split(",")
    seed_shared(modspecs, files)
    for fname in files:
        with open(fname) as f:
            text = f.read()
        with open(fname, "w") as f:
            f.write(scrub(text, modspecs))
PYEOF
# go files that may carry wires: cmd/ (the binary). Engine layers and
# core importing a module is illegal already - the purity law says SO,
# so the scrubber only walks the host.
mapfile -t GOPATHS < <(find cmd -name '*.go' | sort)
FALLIST="$TMP/gofiles.txt"
printf '%s\n' "${GOPATHS[@]}" > "$FALLIST"

fails=0
for MOD in "${MODULES[@]}"; do
  WORK="$TMP/$MOD"
  mkdir -p "$WORK"
  ( cd "$ROOT" && git archive HEAD | tar -x -C "$WORK" )
  if [ ! -d "$WORK/modules/$MOD" ]; then
    echo "nm-purity [$MOD]: FAIL - no modules/$MOD in the tree" >&2
    fails=$((fails+1)); continue
  fi
  rm -rf "$WORK/modules/$MOD"

  spec="$MOD=$MOD"
  # the scrub walks the work COPY only - run inside $WORK with relative paths
  ( cd "$WORK" && python3 "$TMP/scrub.py" "$(paste -sd, "$FALLIST")" $spec )

  if ( cd "$WORK" && go build -o "$WORK/omega" ./cmd/omega ) > "$TMP/$MOD.build.log" 2>&1; then
    build=ok
  else
    build=red
  fi
  if [ "$build" != ok ]; then
    echo "nm-purity [$MOD]: FAIL - the scrubbed tree does not compile (a wire the scrubber does not model, or a leaked noun):" >&2
    sed 's/^/    /' "$TMP/$MOD.build.log" | head -10 >&2
    fails=$((fails+1)); continue
  fi
  # the symbol table reads once into a file; every check below greps
  # the file. Piping nm straight into grep -q was a silent no-op under
  # pipefail: grep exits at the first match, the upstream nm dies of
  # SIGPIPE, and the pipeline exit 141 reads as no-match - the noun
  # check fired or not on a race.
  if ! go tool nm "$WORK/omega" > "$TMP/$MOD.nm" 2>"$TMP/$MOD.nm.err"; then
    echo "nm-purity [$MOD]: FAIL - go tool nm could not read the binary:" >&2
    sed 's/^/    /' "$TMP/$MOD.nm.err" | head -5 >&2
    fails=$((fails+1)); continue
  fi
  if grep -F "modules/$MOD" "$TMP/$MOD.nm" > "$TMP/$MOD.nm.log" 2>&1; then
    echo "nm-purity [$MOD]: FAIL - module symbols in the binary:" >&2
    sed 's/^/    /' "$TMP/$MOD.nm.log" | head -10 >&2
    fails=$((fails+1)); continue
  fi
  # the noun itself: zero occurrences of the word in OMEGA-OWNED symbol
  # names. The law owns omega's vocabulary, not third parties': symbol
  # paths under the omega module, main-package symbols, and type
  # descriptors naming a main-scope type ([^/]main\. - never a /main/
  # third-party path component) carry the noun; github.com/robfig/cron
  # (modules/continuous's scheduler), modernc.org/memory (the sqlite
  # driver) and the crypto/internal stdlib are somebody else's names
  # and stay in the binary no matter how cleanly a module leaves.
  # (string constants live in rodata, not in symbol names - this is a
  # TYPE/FUNC symbol check, not a string scan)
  grep -E 'github\.com/EndoTheDev/omega/|(^|[^/])main\.' "$TMP/$MOD.nm" > "$TMP/$MOD.own" 2>/dev/null || true
  if grep -w "$MOD" "$TMP/$MOD.own" > "$TMP/$MOD.noun.log" 2>&1; then
    echo "nm-purity [$MOD]: FAIL - the module noun survives as an omega-owned symbol:" >&2
    sed 's/^/    /' "$TMP/$MOD.noun.log" | head -10 >&2
    fails=$((fails+1)); continue
  fi
  echo "nm-purity [$MOD]: GREEN - zero module symbols in the binary"
done

[ "$fails" -eq 0 ] || exit 1
exit 0