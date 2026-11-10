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
# now lying), or the module's nouns leaked into a lower layer. Both
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
      for d in harness/*/; do MODULES+=("$(basename "$d")"); done ;;
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
# native tools (python, go) read a bare /tmp as C:\tmp on windows (msys),
# while msys tar rejects the C:\ spelling - so keep TMP posix and convert
# per call site. cygpath exists only on msys; the fallback keeps linux
# (CI runners) on the original spelling.
nat() { cygpath -w "$1" 2>/dev/null || echo "$1"; }

# The scrubber: deletes the module's wires from a .go file and prunes
# the orphaned constructs the deletions leave behind. The cascade is
# confined to the declarations that reference the module's identifiers
# - an unrelated function keeps every line (the false-positive
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
                m = re.match(r"^\s*([A-Za-z_]\w*)\s+\*?([A-Za-z_][\w\.]*)\s*(\x60[^\x60]*\x60)?\s*$", line)
                if m and name in m.group(2).lower() and m.group(2).lower().endswith("config"):
                    SharedFields[name].add(m.group(1))
                # composite-typed aggregate fields (t_4a1ed765): the
                # map/slice/array/star shapes (`Providers
                # map[string]ProviderConfig`) are the same vocabulary
                # class as the direct field - the type's NAME carries
                # the module noun, so the aggregate field dies with the
                # capsule. Peel the wrapper prefix and apply the same
                # predicate to the final type identifier.
                m = re.match(r"^\s*([A-Za-z_]\w*)\s+((?:map\[[^\]]*\]|\[[^\]\[]*\]|\*)+)\s*([A-Za-z_][\w\.]*)\s*(\x60[^\x60]*\x60)?\s*$", line)
                if m and name in m.group(3).lower() and m.group(3).lower().endswith("config"):
                    SharedFields[name].add(m.group(1))

def scrub(text, modules, carried=None):
    """modules: list of module names. carried: func names a prior
    scrub pass deleted in ANOTHER file. Returns (text, removed):
    the scrubbed text and the func names deleted whole here."""
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
    if carried:
        dead_ids.update(carried)

    # 1. import lines + alias discovery (file-level)
    keep = []
    for ln in lines:
        deleted = False
        for name in mnames:
            m = re.match(r'^(\s*)([A-Za-z_][A-Za-z0-9_]*\s+)?"[^"]*/harness/' + re.escape(name) + r'"(\s*//.*)?$', ln)
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

    def split_chunks(lines):
        chunks = []
        cur = []
        depth = 0
        for ln in lines:
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
        return chunks

    chunks = split_chunks(keep)

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
        # a bare identifier or identifier list (list items, re-assigns).
        # The bare MODULE name is excluded (see the value rule below):
        # it may name a live local or param.
        if re.match(r"^((" + "|".join(re.escape(i) for i in sorted(dead_ids - set(mnames))) + r"))\s*(,\s*)*$", stripped):
            return True
        # a struct field typed by a dead host-config type - the tag is
        # string-stripped above, so the line body reads `Skills
        # SkillsConfig`; the field's own name dies with it (access
        # sites read a struct that no longer carries it). The head word
        # must not be a keyword: `package cron` is a package clause,
        # not a field.
        m = re.match(r"^\s*([A-Za-z_]\w*)\s+\*?([A-Za-z_]\w*)\s*(//.*)?$", body)
        if m and m.group(2) in dead_ids and m.group(1) not in keywords:
            dead_fields.add(m.group(1))
            return True
        # the composite shape of the same class (t_4a1ed765): `Providers
        # map[string]ProviderConfig` - a wrapper (map key type, slice or
        # array mark, star) sits between the field name and the dead
        # type's identifier
        m = re.match(r"^\s*([A-Za-z_]\w*)\s+((?:map\[[^\]]*\]|\[[^\]\[]*\]|\*)+)\s*([A-Za-z_]\w*)\s*(//.*)?$", body)
        if m and m.group(3) in dead_ids and m.group(1) not in keywords:
            dead_fields.add(m.group(1))
            return True
        # a composite-literal key typed by a dead host-config type
        # (`Kanban: KanbanConfig{` in DefaultConfig): dies with its
        # braces - the wire-headed block rule below eats the literal
        m = re.match(r"^\s*([A-Za-z_]\w*)\s*:\s*\*?([A-Za-z_]\w*)\{", body)
        if m and m.group(2) in dead_ids:
            dead_fields.add(m.group(1))
            return True
        # a VALUE use of a dead identifier (t_4a1ed765): the type
        # constructor `inst := ProviderConfig{}`, the make() type arg
        # `make(map[string]ProviderConfig, n)`, the call of a dead
        # function. Member access is the rules above; a dot-adjacent
        # token is another identifier's member (cfg.Providers, pkg.X)
        # and reads as the qualifier's wire, never this token's value.
        # Comments are prose (mark_word already used them to MARK this
        # chunk) and must not re-kill live lines here. The bare MODULE
        # name is NOT a value-killer: a local or param may carry it
        # (`trust trustFlags` - trustFlags is host vocabulary that
        # survives the capsule) and only the qualified use is the
        # package wire, which the access rule above models.
        code = re.sub(r"//[^\n]*", "", body)
        bare_ids = sorted(dead_ids - set(mnames))
        for ident in bare_ids:
            for mt in re.finditer(r"\b" + re.escape(ident) + r"\b", code):
                if mt.start() > 0 and code[mt.start() - 1] == ".":
                    continue
                if mt.end() < len(code) and code[mt.end()] == ".":
                    continue
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
        # Anchored short-decl names are FUNCTION-scoped, not file
        # vocabulary (card t_f77dd67d). collect_anchors files every :=
        # name of a dying wire line into dead_ids, and dead_ids is the
        # file-wide set - so a local anchored in a dying capsule func
        # leaked into every later chunk of the file and killed a LIVE
        # sibling that happened to declare the same spelling. The
        # provider proof died this way: providerInstancesKnob anchored
        # `out` (its `out := make(..., len(names))` read the anchored
        # `names`), and settingsArgs - a pure argv helper with no
        # capsule wire at all - lost `out :=`, its append and
        # `return out` to the value rule, leaving "missing return".
        # Go gives a := name the scope of its block, so an anchor never
        # outlives its own function: chunks ARE functions, and the
        # package-level vocabulary (module name, host config types,
        # deleted func names) stays in dead_ids across chunks. Snapshot
        # and restore, so the cascade inside the chunk is unchanged.
        base_dead = set(dead_ids)
        try:
            return scrub_chunk_inner(chunk)
        finally:
            dead_ids.clear()
            dead_ids.update(base_dead)

    def scrub_chunk_inner(chunk):
        removed = set()
        # a chunk untouched by the module keeps every line
        if not any(dead_word(ln) or mark_word(ln) for ln in chunk):
            return chunk, removed
        # a host-config type named for the module dies whole: struct,
        # doc comment and all. The decl may share its chunk with
        # whatever comes before it - the first type in a file sits in
        # the same top-level group as the package clause - so scan for
        # the decl line and cut its brace-balanced span, keeping the
        # rest of the chunk.
        for idx, ln in enumerate(chunk):
            m = re.match(r"type\s+([A-Za-z_]\w*)\b", ln.strip())
            if m and m.group(1) in dead_ids:
                s = idx
                while s > 0 and chunk[s - 1].lstrip().startswith("//"):
                    s -= 1
                d4 = 0
                e = idx
                while e < len(chunk):
                    d4 += chunk[e].count("{") - chunk[e].count("}")
                    if d4 <= 0:
                        break
                    e += 1
                chunk = chunk[:s] + chunk[e + 1:]
                break
        start_len = len(chunk)
        for _ in range(15):
            n = len(chunk)
            kill = set()
            i = 0
            while i < n:
                ln = chunk[i]
                if is_wire_line(ln):
                    # control-flow heads are NOT anchor sources: a head
                    # that dies with its block takes its := names' whole
                    # scope with it, and an outer binding may shadow the
                    # name (the range var "n" vs a sibling func's param,
                    # t_4a1ed765) - anchoring it kills live lines.
                    if not re.match(r"^\s*(if|for|switch)\b", ln):
                        collect_anchors(ln)
                    delta = ln.count("{") - ln.count("}")
                    if ln.rstrip().endswith("{"):
                        # a wire-headed conditional dies WITH its block:
                        # the condition is the module's, so every branch
                        # under it is unreachable wiring. Inner
                        # declarations die with the block (their
                        # orphaned references cascade after).
                        j2 = i + 1
                        d3 = delta
                        while d3 > 0 and j2 < n:
                            d3 += chunk[j2].count("{") - chunk[j2].count("}")
                            j2 += 1
                        for b in chunk[i + 1:j2]:
                            collect_anchors(b)
                        kill.update(range(i, j2))
                        i = j2
                        continue
                    if delta < 0:
                        # a wire line that CLOSES a construct opened
                        # above: the tail dies with its whole construct.
                        # Deleting the `}` alone strands the opener, and
                        # Go reads the stranded head line as the start of
                        # the next element (the orphan `{Key: "x", ...`
                        # head that broke the memory capsule). Walk back
                        # with a naive LIFO: closers met on the way are
                        # matched by openers further up and add to the
                        # debt; the first opener that clears the debt is
                        # the tail's match, and the region it spans is
                        # brace-balanced, so it dies whole.
                        d = -delta
                        k = i
                        while d > 0 and k > 0:
                            k -= 1
                            c = chunk[k].count("{") - chunk[k].count("}")
                            if c > 0:
                                d -= c
                            else:
                                d += -c
                        if d > 0:
                            k = i  # no opener in this chunk: die alone
                        for b in chunk[k:i]:
                            collect_anchors(b)
                        kill.update(range(k, i + 1))
                        i += 1
                        continue
                    kill.add(i)
                    i += 1
                    continue
                # RHS = single dead value -> the line dies. The bare
                # MODULE name is excluded - it may name a live local
                # or param (see the value rule).
                m = re.match(r'^([^=]+)=\s*([A-Za-z_][A-Za-z0-9_]*)\s*(//.*)?$', ln.strip())
                if m and m.group(2) in dead_ids and m.group(2) not in mnames:
                    kill.add(i)
                    i += 1
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
                    while j < n:
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
                        code = [b for b in chunk[i + 1:j - 1]
                                if b.strip() and not b.strip().startswith("//")]
                        # (a) the condition references a dead identifier
                        if dead_word(ln):
                            for b in code:
                                collect_anchors(b)
                            kill.update(range(i, j))
                            i = j
                            continue
                        # (b) every code word of the body is dead
                        if code and all(only_dead_words(b) for b in code):
                            for b in code:
                                collect_anchors(b)
                            kill.update(range(i, j))
                            i = j
                            continue
                        # (c) the body emptied out (comments may remain)
                        if not code or all(b.strip() == "" or b.strip().startswith("//") for b in code):
                            kill.update(range(i, j))
                            i = j
                            continue
                        # none of (a)(b)(c) fired, or the block never closed
                        # inside this chunk: the head lives, and the scan
                        # reaches its body lines one by one
                        i += 1
                        continue
                # a return line whose values are all dead identifiers
                # or go literals (nil/true/false/numbers/quoted strings)
                # dies: it hands back definitions the deletion removed
                # (the sectionConfig case that copies the module's config
                # section and returns it). At least one value must be a
                # dead identifier - a literal-only return (`return nil,
                # false` trailing a switch) is not a wire.
                rbody = re.sub(r"//[^\n]*$", "", ln.strip())
                m = re.match(r"^\s*return\s+(.+)$", rbody)
                if m:
                    dead_vals = 0
                    live_vals = 0
                    for val in m.group(1).split(","):
                        val = val.strip().rstrip(",").strip()
                        if not val:
                            live_vals += 1
                        elif val in dead_ids and val not in mnames:
                            # a bare MODULE name as a return value is a
                            # live local or param (the qualified use is
                            # the access rule's wire)
                            dead_vals += 1
                        elif val in ("nil", "true", "false", "_") or val.isdigit() or val.startswith('"'):
                            pass
                        else:
                            live_vals += 1
                    if dead_vals > 0 and live_vals == 0:
                        kill.add(i)
                        i += 1
                        continue
                # orphaned switch case: its body is gone
                m = re.match(r"^(\s*)(case\b|default:)", ln)
                nxt = chunk[i + 1].strip() if i + 1 < len(chunk) else "}"
                if m and (nxt.startswith("case ") or nxt.startswith("default:") or nxt.startswith("}")):
                    kill.add(i)
                    i += 1
                    continue
                i += 1
            if not kill:
                break
            chunk = [ln for idx, ln in enumerate(chunk) if idx not in kill]
        # declared-and-not-used cleanup: a short declaration whose name
        # outlived its last reader (the reader died as a wire block)
        # strands an unused name Go refuses to compile. Blank the name
        # in place; if every name of a plain statement is unread, the
        # whole line dies. When no non-blank reader is new, the short
        # form degrades to a plain assignment (same block semantics).
        # Readers are read in CODE text: strings blanked, comments
        # stripped (same prelude as is_wire_line). A comment sighting
        # is prose, not a reader - the approvals case whose `host,
        # key, model :=` reader line died as a wire kept `host` and
        # `model` alive off the comment prose above it and stranded
        # both as declared-and-not-used (the nightly red, cascade class).
        def code_text(ln):
            body = re.sub(r"`[^`]*`", " ` ", ln)
            body = re.sub(r'"(\\.|[^"\\])*"', '""', body)
            body = re.sub(r"'[^']*'", "''", body)
            body = re.sub(r"//[^\n]*", "", body)
            return body

        for _pass in range(16):
            changed = False

            def predeclared(i2, n):
                for pl in (code_text(x) for x in chunk[:i2]):
                    pm = re.match(r"^\s*([A-Za-z_][A-Za-z0-9_,\s]*?)\s*:=\s*[^;{}]*$", pl)
                    if pm and re.search(r"\b" + re.escape(n) + r"\b", pm.group(1)):
                        return True
                    pm = re.match(r"^\s*var\s+([\w\s,]+?)\s*(?:[\[\*A-Za-z_].*)?$", pl)
                    if pm and re.search(r"\b" + re.escape(n) + r"\b", pm.group(1)):
                        return True
                return False

            for idx, ln in enumerate(chunk):
                if ":=" not in ln:
                    continue
                m = re.match(r"^\s*([A-Za-z_][A-Za-z0-9_,\s]*?)\s*:=\s*([^;{}]*)(\{)?$", ln)
                if not m:
                    continue
                names = [n.strip() for n in m.group(1).split(",") if n.strip()]
                if not all(re.match(r"^[A-Za-z_]\w*$", n) for n in names):
                    continue
                others = "\n".join(code_text(x) for x in chunk[:idx] + chunk[idx + 1:])
                unused = [n for n in names
                          if n != "_" and not re.search(r"\b" + re.escape(n) + r"\b", others)]
                if not unused:
                    continue
                head = bool(re.match(r"^\s*(if|for|switch)\b", ln))
                if not head and len(unused) == len(names):
                    # every name unread: the whole declaration dies. A
                    # multi-line RHS (a map/slice literal head ending in
                    # "{") dies WITH its balanced block - killing the
                    # head line alone strands the literal body below
                    # (providerTypes in Validate, t_4a1ed765).
                    if ln.rstrip().endswith("{"):
                        d5 = ln.count("{") - ln.count("}")
                        e5 = idx
                        while d5 > 0 and e5 + 1 < len(chunk):
                            e5 += 1
                            d5 += chunk[e5].count("{") - chunk[e5].count("}")
                        chunk = chunk[:idx] + chunk[e5 + 1:]
                    else:
                        chunk = chunk[:idx] + chunk[idx + 1:]
                    changed = True
                    break
                lhs = ", ".join("_" if n in unused else n for n in names)
                newline = ln[:m.start(1)] + lhs + ln[m.end(1):]
                keep = [n for n in names if n not in unused]
                if (not head and keep
                        and all(predeclared(idx, n) for n in keep)):
                    newline = newline.replace(":=", "=", 1)
                if head and not keep and ln.strip().startswith("for"):
                    # `for _, _ := range` refuses the short form; the
                    # assignment form accepts every blank
                    newline = newline.replace(":=", "=", 1)
                chunk[idx] = newline
                changed = True
                break
            if not changed:
                break
        # a func whose body emptied in the cascade dies whole: a
        # nil-bodied func with a non-error return refuses to compile
        # ("missing return"), so its callers must follow. The name
        # joins the dead vocabulary and scrub() re-runs the file so
        # EARLIER chunks (the call sites) see it. Only a chunk that
        # actually deleted something fires this rule - an always-empty
        # live stub keeps its lines.
        if len(chunk) != start_len:
            scan = 0
            while scan < len(chunk):
                ln2 = chunk[scan]
                if not re.match(r"^\s*func\b", ln2) or not ln2.rstrip().endswith("{"):
                    scan += 1
                    continue
                d6 = ln2.count("{") - ln2.count("}")
                e6 = scan
                while d6 > 0 and e6 + 1 < len(chunk):
                    e6 += 1
                    d6 += chunk[e6].count("{") - chunk[e6].count("}")
                if d6 > 0:
                    scan += 1
                    continue
                interior = [x for x in chunk[scan + 1:e6]
                            if x.strip() and not x.strip().startswith("//")]
                if not interior:
                    nm = re.match(r"^\s*func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)", ln2)
                    if nm:
                        removed.add(nm.group(1))
                    s3 = scan
                    while s3 > 0 and chunk[s3 - 1].lstrip().startswith("//"):
                        s3 -= 1
                    chunk = chunk[:s3] + chunk[e6 + 1:]
                    scan = s3
                    continue
                scan = e6 + 1
        return chunk, removed

    out = []
    removed = set()
    all_removed = set()
    for ch in chunks:
        r, rm = scrub_chunk(ch)
        out.extend(r)
        removed |= rm
        all_removed |= rm
    # a func whose body emptied left whole (scrub_chunk above) is dead
    # vocabulary now, and its call sites sit in EARLIER chunks of this
    # file. Feed the name back and re-run until no func leaves whole.
    rounds = 0
    while removed and rounds < 8:
        rounds += 1
        dead_ids.update(removed)
        removed = set()
        out2 = []
        for ch in split_chunks(out):
            r, rm = scrub_chunk(ch)
            out2.extend(r)
            removed |= rm
            all_removed |= rm
        out = out2
    # import pruning: a cascade that ate a package's last client
    # leaves the import behind, and Go compiles the leftovers loud
    # ("imported and not used"). Stdlib imports prune too (sort lost
    # its last reader with providerInstancesKnob, t_4a1ed765). Only
    # lines inside the import block are candidates - a `word "literal"`
    # line elsewhere is Go code, not an import.
    import_shapes = re.compile(r'^\s*([A-Za-z_]\w*\s+)?"((?:\\.|[^"\\])*)"\s*(//.*)?$')
    # a single-LINE import (`import "sort"`, no block) prunes too - the
    # import block below never opens for it
    single_import = re.compile(r'^import\s+([^\s]+\s+)?"((?:\\.|[^"\\])*)"\s*(//.*)?$')
    b0 = next((k for k, ln in enumerate(out) if ln.strip() == "import ("), None)
    b1 = None
    if b0 is not None:
        b1 = next((k for k in range(b0 + 1, len(out)) if out[k].strip() == ")"), None)

    def is_import_line(k, ln):
        if single_import.match(ln.strip()):
            return True
        if b0 is None or k < b0:
            return False
        if b1 is not None and k > b1:
            return False
        return bool(import_shapes.match(ln))

    def import_parts(ln):
        s = ln.strip()
        sm = single_import.match(s)
        if sm:
            return (sm.group(1) or "").strip(), sm.group(2)
        m = import_shapes.match(ln)
        return (m.group(1) or "").strip() if m else "", m.group(2) if m else ""

    body = "\n".join(ln for k, ln in enumerate(out) if not is_import_line(k, ln))
    pruned = []
    for k, ln in enumerate(out):
        if not is_import_line(k, ln):
            pruned.append(ln)
            continue
        if ln.strip().startswith("."):
            # dotted import: side effect only, its path is never used
            pruned.append(ln)
            continue
        alias, path = import_parts(ln)
        if alias == "_":
            pruned.append(ln)
            continue
        last = path.rstrip("/").split("/")[-1]
        cands = {last, last.split(".")[0]}
        if re.match(r"^v\d+$", last) and "/" in path:
            cands.add(path.rstrip("/").split("/")[-2])
        hits = (alias and re.search(r"\b" + re.escape(alias) + r"\.", body)) or \
            any(re.search(r"\b" + re.escape(c) + r"\.", body) for c in cands)
        if not hits:
            continue
        pruned.append(ln)
    return "\n".join(pruned), all_removed

if __name__ == "__main__":
    modspecs = [s.split("=")[-1] for s in sys.argv[2:]]
    files = sys.argv[1].split(",")
    seed_shared(modspecs, files)
    # a func whose body emptied dies whole (scrub_chunk); its name is
    # dead vocabulary for OTHER files too (a call site across the file
    # boundary never sees one file's dead_ids). Pass over the file set
    # until the deletion frontier is quiet (bounded, like the chunk
    # re-run inside scrub()).
    carried = set()
    for _pass in range(6):
        grew = False
        for fname in files:
            with open(fname) as f:
                text = f.read()
            text2, rem = scrub(text, modspecs, carried)
            fresh = rem - carried
            if fresh:
                grew = True
                carried |= fresh
            if text2 != text:
                with open(fname, "w") as f:
                    f.write(text2)
        if not grew:
            break
PYEOF
# go files that may carry wires: cmd/ (the binary). Engine layers and
# core importing a module is illegal already - the purity law says SO,
# so the scrubber only walks the host. The list derives from the WORK
# COPY inside the loop: a fixture sitting uncommitted in the live
# tree must never enter a proof built from HEAD.

fails=0
for MOD in "${MODULES[@]}"; do
  WORK="$TMP/$MOD"
  mkdir -p "$WORK"
  ( cd "$ROOT" && git archive HEAD | tar -x -C "$WORK" )
  if [ ! -d "$WORK/harness/$MOD" ]; then
    echo "nm-purity [$MOD]: FAIL - no harness/$MOD in the tree" >&2
    fails=$((fails+1)); continue
  fi
  rm -rf "$WORK/harness/$MOD"

  ( cd "$WORK" && find cmd -name '*.go' | sort > "$TMP/$MOD.gofiles.txt" )
  FALLIST="$TMP/$MOD.gofiles.txt"

  spec="$MOD=$MOD"
  # the scrub walks the work COPY only - run inside $WORK with relative paths
  ( cd "$WORK" && python3 "$(nat "$TMP/scrub.py")" "$(paste -sd, "$FALLIST")" $spec )

  if ( cd "$WORK" && go build -o "$(nat "$WORK/omega")" ./cmd/omega ) > "$TMP/$MOD.build.log" 2>&1; then
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
  if ! go tool nm "$(nat "$WORK/omega")" > "$TMP/$MOD.nm" 2>"$TMP/$MOD.nm.err"; then
    echo "nm-purity [$MOD]: FAIL - go tool nm could not read the binary:" >&2
    sed 's/^/    /' "$TMP/$MOD.nm.err" | head -5 >&2
    fails=$((fails+1)); continue
  fi
  if grep -F "harness/$MOD" "$TMP/$MOD.nm" > "$TMP/$MOD.nm.log" 2>&1; then
    echo "nm-purity [$MOD]: FAIL - module symbols in the binary:" >&2
    sed 's/^/    /' "$TMP/$MOD.nm.log" | head -10 >&2
    fails=$((fails+1)); continue
  fi
  # the noun itself: zero occurrences of the word in OMEGA-OWNED symbol
  # names. The law owns omega's vocabulary, not third parties': symbol
  # paths under the omega module, main-package symbols, and type
  # descriptors naming a main-scope type ([^/]main\. - never a /main/
  # third-party path component) carry the noun; github.com/robfig/cron
  # (harness/continuous's scheduler), modernc.org/memory (the sqlite
  # driver) and the crypto/internal stdlib are somebody else's names
  # and stay in the binary no matter how cleanly a module leaves.
  # (string constants live in rodata, not in symbol names - this is a
  # TYPE/FUNC symbol check, not a string scan)
  grep -E 'github\.com/EndoTheDev/omega/|(^|[^/])main\.' "$TMP/$MOD.nm" > "$TMP/$MOD.own" 2>/dev/null || true
  # Another capsule's package vocabulary is not the host's: terminal
  # owns terminal.process and its OWN capsule scrub proves that
  # vocabulary dies with the terminal capsule. The go1.26.10 runner
  # emits terminal's ptyProcess->process itab as a NAMED symbol the
  # 1.27 lane never sees (the nightly red on the runner toolchain),
  # and a bare noun grep over Omega-owned symbols counts the
  # DIFFERENT module's type name against the process check. Strip
  # every omega harness/<name> segment out of the symbol names before
  # the noun grep: an itab whose type operands all live under
  # harness/<other> falls out of scope, while a host operand
  # (github.com/EndoTheDev/omega/core or main-package vocabulary)
  # survives the strip and still reds. The segment swallow runs to the
  # next comma or whitespace - comma-bearing mangled generic names can
  # over-strip, which only widens the exclusion across another
  # capsule's own text, never across the host's.
  sed -E 's#github\.com/EndoTheDev/omega/(modules|harness)/[A-Za-z0-9_][^, ]*##g' \
    "$TMP/$MOD.own" > "$TMP/$MOD.own.host" 2>/dev/null || true
  # The stdlib embed.FS member is not the host's vocabulary either:
  # every `var <X> embed.FS` in the tree emits an nm symbol
  # `...<X>.files` (the struct's unexported member), which survives
  # every scrub no matter how cleanly a module leaves. Collect the
  # tree's embed var names and drop their member symbols - the field
  # name is stdlib's, exactly the carve-out class above.
  for EV in $(grep -rhoE '^var +([A-Za-z_][A-Za-z0-9_]*) +embed\.FS\b' \
      --include='*.go' "$ROOT" | awk '{print $2}' | sort -u); do
    sed -i -E "/\.$EV\.files$/d" "$TMP/$MOD.own.host" 2>/dev/null || true
  done
  if grep -w "$MOD" "$TMP/$MOD.own.host" > "$TMP/$MOD.noun.log" 2>&1; then
    echo "nm-purity [$MOD]: FAIL - the module noun survives as an omega-owned symbol:" >&2
    sed 's/^/    /' "$TMP/$MOD.noun.log" | head -10 >&2
    fails=$((fails+1)); continue
  fi
  echo "nm-purity [$MOD]: GREEN - zero module symbols in the binary"
done

[ "$fails" -eq 0 ] || exit 1
exit 0