#!/usr/bin/env python3
"""strip_comments.py - apply the comment law mechanically.

Law (docs/release-gate.md): a comment earns its line only when deleting it
would let a competent reader break the code. History lives in git and the
board, never in source comments.

What the pass does, per whole-line comment block:

  class A  provenance removal. Card ids, ISO dates and the
           proven/ruling/receipt citations are cut out of the prose.
  class B  sentence reduction. The block keeps its summary sentence plus
           every later sentence that states a hazard (must/never/invariant/
           race/...). The remaining prose goes, and what survives is
           rewrapped to a normal comment width.

Never touched: `//go:build`/`//go:embed`/`//go:generate`/`//export`/linter
pragmas and shebangs (machine input); blocks that are preformatted (usage
samples, aligned tables, diagrams); comments that start after code on the
same line; the interior of Go raw strings and TS template literals.

Usage:
  scripts/strip_comments.py --dry [root] [subtree]   report only
  scripts/strip_comments.py --apply [root] [subtree] rewrite in place
  scripts/strip_comments.py --provenance --apply [root]
                                                     class A only (.sh too)
"""
import os
import re
import sys

SKIP_DIRS = {"node_modules", "dist", ".nuxt", ".output", ".git", "bin", ".worktrees"}
SKIP_PATHS = ("api/static", "webui/dist")
LANGS = {".go": "go", ".ts": "webui", ".js": "webui", ".mjs": "webui", ".vue": "webui"}
WRAP = int(os.environ.get("COMMENT_WRAP", "78"))

DIRECTIVE = re.compile(
    r"^\s*(?://(?:go:[a-z]+|export\s+\w+|line\s+\S+|\+build|nolint(?::\w+)?|"
    r"eslint|prettier|@ts-|@vue-|@flow)|//\s+\+build\b|#!)"
)

# A sentence earns its line only for one of these: a rule, a hazard, or a
# contract with another file. Every hit costs a line, so the list stays
# precise - ordinary prose verbs (uses, returns, handles) are not here.
HAZARD = re.compile(
    r"\b(must|never|cannot|can't|do not|don't|invariant|load-bearing|race|deadlock|"
    r"panic|goroutine|concurren\w*|atomic|unsafe|leak|silently|hazard|trap|"
    r"callers?|contract|exactly|byte|sorted|sort|ordering|precedence|"
    r"idempotent|re-?entrant|nil|clobber\w*|corrupt\w*|duplicat\w*|overwrit\w*|"
    r"blocking|deprecated|todo|fixme|warning)\b",
    re.IGNORECASE,
)

PREFORMATTED = re.compile(r"(\t| {2,}\s|:=|\bUsage:|\bfmt\.|\bgo (test|build|run)\b|│|├|└)")

CARD = re.compile(r"\bt_[0-9a-f]{8}\b")
MARK = re.compile(
    r"t_[0-9a-f]{8}\b|20[0-9]{2}-[0-9]{2}-[0-9]{2}|\bproven\b|\bruling\b|\breceipt\b",
    re.IGNORECASE,
)
CURRENT = [""]
DATE = re.compile(r"\b20[0-9]{2}-[0-9]{2}-[0-9]{2}\b")
PROV = re.compile(r"\b(proven|ruling|receipt)\b", re.IGNORECASE)


def drop_parens(text):
    """Remove parentheticals that carry provenance; keep the rest."""
    def one(m):
        inner = m.group(0)
        if CARD.search(inner) or DATE.search(inner) or PROV.search(inner) or "card" in inner.lower():
            return ""
        return inner

    prev, out = None, text
    while prev != out:
        prev = out
        out = re.sub(r"\((?:[^()]|\([^()]*\))*\)", one, out)
    return out


def strip_provenance(text):
    out = drop_parens(text)
    # a citation that never closes its paren still cites: cut it
    out = re.sub(r"\(\s*(?:card|ruling|proven|receipt)\b[^)]*\)?", "", out, flags=re.IGNORECASE)
    out = re.sub(r"\bcard\s+t_[0-9a-f]{8}(?:'s)?\b", "", out, flags=re.IGNORECASE)
    out = CARD.sub("", out)
    out = DATE.sub("", out)
    # "ruling 17: ..." cites a decision; the sentence under it stands alone
    out = re.sub(r"^\s*(?:the\s+)?(?:card\s+|his\s+|endo's\s+)?ruling\s+\d+\s*:\s*", "", out, flags=re.IGNORECASE)
    out = re.sub(r"\bruling\s+\d+(?:'s)?\s*[,:;]\s*", "", out, flags=re.IGNORECASE)
    out = re.sub(r"\bruling\s+\d+(?:'s)?\b", "", out, flags=re.IGNORECASE)
    out = re.sub(r"\(\s*[,;]?\s*\)", "", out)
    out = re.sub(r"[ \t]{2,}", " ", out)
    out = re.sub(r"\s+([,.;:])", r"\1", out)
    out = re.sub(r"([,;:])(\s*[,;:])+", r"\1", out)
    out = re.sub(r"\.\s*\.", ".", out)
    out = re.sub(r"\s*-\s*$", "", out)
    out = re.sub(r"^\s*-\s*", "", out)
    out = re.sub(r"^\s*[,;:]\s*", "", out)
    return out.strip()


def split_sentences(text):
    guards = []

    def save(m):
        guards.append(m.group(0))
        return f"\x00{len(guards) - 1}\x00"

    t = text
    for pat in (r"\d+\.\d+", r"\b(?:e\.g|i\.e|vs|etc|cf|approx)\.", r"\b[A-Z]\.[A-Z]\."):
        t = re.sub(pat, save, t)
    parts = [p for p in re.split(r"(?<=[.!?])\s+", t) if p.strip()]
    return [re.sub(r"\x00(\d+)\x00", lambda m: guards[int(m.group(1))], p) for p in parts]


def wrap(text, indent):
    width = max(WRAP - len(indent) - 3, 40)
    out, cur = [], ""
    for w in text.split():
        if cur and len(cur) + 1 + len(w) > width:
            out.append(cur)
            cur = w
        else:
            cur = f"{cur} {w}".strip()
    if cur:
        out.append(cur)
    return out


def comment_text(line):
    s = line.lstrip()
    if s.startswith("//"):
        return s[2:].strip()
    if s.startswith("<!--"):
        t = s[4:].strip()
        return t[:-3].strip() if t.endswith("-->") else t
    if s.startswith("/*"):
        t = s[2:].strip()
        if t.startswith("*"):
            t = t[1:].strip()
        if t.endswith("*/"):
            t = t[:-2].strip()
        return t
    if s.startswith("#") and not s.startswith("#!"):
        return s[1:].strip()
    if s.startswith("*"):
        t = s[1:].strip()
        if t.endswith("*/"):
            t = t[:-2].strip()
        return t
    t = s.strip()
    if t.endswith("*/"):
        t = t[:-2].strip()
    if t.endswith("-->"):
        t = t[:-3].strip()
    return t


def block_at(lines, i):
    """Marker plus the line indices of the whole-line comment block at i."""
    s = lines[i].lstrip()
    if s.startswith("//"):
        marker = "//"
    elif s.startswith("<!--"):
        marker = "<!--"
    elif s.startswith("#") and not s.startswith("#!"):
        marker = "#"
    elif s.startswith("/*"):
        marker = "/*"
    else:
        return None, []
    idx, j = [], i
    if marker in ("<!--", "/*"):
        closer = "-->" if marker == "<!--" else "*/"
        while j < len(lines):
            idx.append(j)
            body = lines[j].lstrip()
            start = len(marker) if body.startswith(marker) else 0
            if closer in body[start:]:
                return marker, idx
            j += 1
        return None, []  # unterminated: leave it alone
    while j < len(lines):
        t = lines[j].lstrip()
        if marker == "//":
            ok = t.startswith("//")
        elif marker == "#":
            ok = t.startswith("#") and not t.startswith("#!")
        else:
            ok = t.startswith("*") and not t.startswith("*/")
        if not ok:
            break
        idx.append(j)
        if marker == "*" and "*/" in t:
            break
        j += 1
    return marker, idx


def looks_pre(texts, lines, idx, marker):
    """Usage samples, aligned tables and lists carry their own line breaks."""
    for k in idx:
        after = lines[k].lstrip()[len(marker):]
        if after.startswith("   ") or after.startswith("\t"):
            return True
    return bool(PREFORMATTED.search(" ".join(t for t in texts if t)))


IDENT = re.compile(r"[a-z][A-Z]|[A-Z]{2,}|_|\w\(\)|\w\.\w|`")
CONFIG_PLUMBING = {"config.go", "configs.go", "defaults.go", "defaultwindow.ts"}
GO_KEYWORDS = {
    "return", "if", "for", "go", "defer", "case", "else", "var", "const", "type",
    "func", "switch", "select", "break", "continue", "goto", "range", "chan",
    "map", "struct", "interface", "package", "import", "default", "fallthrough",
    "export", "let", "new", "await", "throw", "yield", "typeof", "delete",
}


def field_decl(line):
    """True when the next line is a field or property declaration.

    A struct field or a TS property already states its name and type; the
    prose above it describes what the declaration describes.
    """
    s = line.strip()
    if not s or s.startswith(("//", "/*", "*", "#", "<!--")):
        return False
    if s.endswith(("{", "(", "=", ":=", "->")):
        return False
    m = re.match(r"^(?:readonly\s+|private\s+|public\s+)?([A-Za-z_$][\w$]*)\??\s*:\s*\S", s)
    if m and m.group(1) not in GO_KEYWORDS:
        return True
    m = re.match(r"^([A-Za-z_$][\w$]*)\s+[\w\*\[\]\.\(]+\s*(`[^`]*`)?[,;]?$", s)
    return bool(m) and m.group(1) not in GO_KEYWORDS

# A block whose opening sentence is a story rather than a statement is
# history: it belongs in the card and the commit, not in the file.
NARRATIVE = re.compile(
    r"\b(Endo|screenshot|milestone v|ruling|proven|receipt|QA finding|incident|"
    r"vault|Log entry|review finding|round \d)",
    re.IGNORECASE,
)


DANGLE = (
    "the", "a", "an", "of", "to", "into", "in", "on", "at", "by", "with", "for",
    "from", "as", "and", "or", "but", "that", "this", "its", "their", "his", "her",
    "every", "each", "which", "whose", "while", "so", "than", "per", "via", "over",
    "under", "after", "before", "both", "all", "also", "then",
)


def undangle(text):
    """Drop a cut's stranded tail words and possessives, so the line still reads."""
    out = text.rstrip()
    changed = True
    while changed:
        changed = False
        m = re.search(r"(\w+)'s[.]$", out)
        if m:
            out = out[: m.start()].rstrip()
            changed = True
            continue
        m = re.search(r"(?:\s|^)(\w+)[.]$", out)
        if m and m.group(1).lower() in DANGLE:
            out = out[: m.start()].rstrip()
            changed = True
    return out


def balanced(head):
    """Cut back to the last unmatched '(' so a truncated line never strands one."""
    out = head
    while out.count("(") > out.count(")"):
        i = out.rfind("(")
        if i < 0:
            break
        out = out[:i].rstrip(" ,;:-")
    return out


def shorten(sent, indent):
    """Cut a run-on summary at its first clause boundary, one line of budget.

    Omega's prose style is "X is Y: <long explanation>", so the clause cut
    keeps the definition and drops the explanation. A cut is refused when
    the dropped remainder carries a hazard: the law outranks the width.
    """
    budget = max(WRAP - len(indent) - 3, 40)
    if len(sent) <= budget:
        return sent
    floor = 24
    for sep in (": ", " - ", "; "):
        i = sent.find(sep, floor)
        if i < 0 or i > budget + 25:
            continue
        rest = sent[i:]
        if HAZARD.search(rest):
            continue
        head = sent[:i].rstrip(" ,;:-")
        if len(head.split()) < 3:
            continue
        head = undangle(balanced(sent[:i].rstrip(" ,;:-")))
        if len(head.split()) < 3:
            continue
        return head + "."
    # no clause boundary: cut at a word boundary so the summary holds one line
    cut = sent.rfind(" ", 0, budget - 1)
    if cut > 40:
        head = undangle(balanced(sent[:cut].rstrip(" ,;:-")))
        if len(head.split()) >= 4 and not HAZARD.search(sent[cut:]):
            return head + "."
    return sent


def narration(indent, sents):
    """A single short sentence inside a body that names nothing: narration."""
    if not indent or len(sents) != 1:
        return False
    s = sents[0]
    if HAZARD.search(s) or IDENT.search(s):
        return False
    return len(s.split()) <= 14


def strip_line(line, marker):
    """Provenance removal that keeps one line's own shape."""
    s = line.lstrip()
    indent = line[: len(line) - len(s)]
    body = s[len(marker):]
    new = strip_provenance(body)
    if new == body.strip():
        return line
    if not new:
        return f"{indent}{marker}"
    return f"{indent}{marker} {new}"


def reduce_block(lines, idx, marker):
    """Replacement lines for one comment block, or None to leave it alone."""
    first = lines[idx[0]]
    if DIRECTIVE.match(first.lstrip()) or first.lstrip().startswith("#!"):
        return None
    indent = first[: len(first) - len(first.lstrip())]
    texts = [comment_text(lines[k]) for k in idx]
    # a directive anywhere in the block is machine input: the block stays whole
    if any(DIRECTIVE.match(lines[k].lstrip()) for k in idx):
        return None
    if marker in ("<!--", "/*") and idx:
        texts = [texts[0]] + [t for t in texts[1:] if t not in ("-->", "*/")]

    if looks_pre(texts, lines, idx, marker):
        # the shape stays; only history comes out of the lines
        out = [strip_line(lines[k], marker) for k in idx]
        return out if out != [lines[k] for k in idx] else None

    body = " ".join(t for t in texts if t)
    if not body:
        return None
    sents = split_sentences(strip_provenance(body))
    if not sents:
        return None
    # narration inside a body earns nothing
    if narration(indent, sents):
        return []
    # a block that opens by telling a story is history, not law
    if NARRATIVE.search(sents[0]) and not any(HAZARD.search(s) for s in sents):
        return []
    # config plumbing describes itself: only a hazard keeps a line there
    if os.path.basename(CURRENT[0]).lower() in CONFIG_PLUMBING and not any(
        HAZARD.search(s) for s in sents
    ):
        return []
    # a field or property doc that states no rule is the declaration, restated
    nxt = lines[idx[-1] + 1] if idx[-1] + 1 < len(lines) else ""
    if field_decl(nxt) and not any(HAZARD.search(s) for s in sents):
        return []

    keep = [s for n, s in enumerate(sents) if n == 0 or HAZARD.search(s)]
    keep[0] = shorten(keep[0], indent)

    if marker == "/*":
        opener = f"{indent}/**" if first.lstrip().startswith("/**") else f"{indent}/*"
        joined = " ".join(keep)
        if len(keep) == 1 and len(joined) + len(indent) + 7 <= WRAP:
            return [f"{opener} {joined} */"]
        return ([opener] + [f"{indent} * {w}" for s in keep for w in wrap(s, indent)]
                + [f"{indent} */"])
    if marker == "<!--":
        joined = " ".join(keep)
        if len(joined) + len(indent) + 11 <= WRAP:
            return [f"{indent}<!-- {joined} -->"]
        if len(idx) == 1:
            return [f"{indent}<!-- {' '.join(keep)} -->"]
        return ([f"{indent}<!--"] + [f"{indent}  {w}" for s in keep for w in wrap(s, indent)]
                + [f"{indent}-->"])
    if any("*/" in lines[k] or "-->" in lines[k] for k in idx):
        return None

    new = [f"{indent}{marker} {w}" for w in wrap(" ".join(keep), indent)]
    if new == [lines[k] for k in idx]:
        return None
    if len(new) >= len(idx) and " ".join(keep) == " ".join(t for t in texts if t):
        return None  # same words, no line saved: leave the bytes alone
    return new


def residue_line(line):
    """Rewrite one comment line so no provenance marker survives it."""
    body = line
    body = re.sub(r"\(?\s*card\s+t_[0-9a-f]{8}\b[^)]*\)?", "", body, flags=re.IGNORECASE)
    body = re.sub(r"\(\s*t_[0-9a-f]{8}\b[^)]*\)?", "", body)
    body = re.sub(r"\bcard\s+t_[0-9a-f]{8}(?:'s)?\b", "", body, flags=re.IGNORECASE)
    body = CARD.sub("", body)
    body = DATE.sub("", body)
    body = re.sub(r"\(\s*Endo'?s?[^)]*\)", "", body)
    body = re.sub(r"\bEndo'?s?\s+(?:band-search\s+)?ruling\s*[,:]?\s*", "", body, flags=re.IGNORECASE)
    body = re.sub(r"\bruling\b", "rule", body, flags=re.IGNORECASE)
    body = re.sub(r"\bproven\b", "verified", body, flags=re.IGNORECASE)
    body = re.sub(r"\breceipt\b", "record", body, flags=re.IGNORECASE)
    body = re.sub(r"\(\s*[,;]?\s*\)", "", body)
    body = re.sub(r"\s+([,.;:)])", r"\1", body)
    body = re.sub(r"([,;:])(\s*[,;:])+", r"\1", body)
    body = re.sub(r"\(\s+", "(", body)
    body = re.sub(r"\s+\)", ")", body)
    body = re.sub(r"[ \t]{2,}", " ", body)
    body = re.sub(r"\.\s*\.", ".", body)
    body = re.sub(r"\bthe rule\b", "the rule", body)
    return body


def residue_sweep(path):
    """Line-level marker cleanup: covers blocks the structural pass skips."""
    try:
        with open(path, encoding="utf-8", newline="") as fh:
            src = fh.read()
    except OSError:
        return None, 0
    crlf = "\r\n" in src
    out, changed = [], 0
    for line in src.splitlines():
        s = line.lstrip()
        if s.startswith(("//", "*", "/*", "<!--")) and MARK.search(line):
            indent = line[: len(line) - len(s)]
            new = residue_line(s)
            if new != s:
                out.append(indent + new)
                changed += 1
                continue
        out.append(line)
    if not changed:
        return None, 0
    text = ("\r\n" if crlf else "\n").join(out)
    if src.endswith("\n") and not text.endswith("\n"):
        text += "\r\n" if crlf else "\n"
    return text, changed


def dangle_sweep(path):
    """Repair lines a cut left stranded: '... survives the.' has no tail."""
    try:
        with open(path, encoding="utf-8", newline="") as fh:
            src = fh.read()
    except OSError:
        return None, 0
    crlf = "\r\n" in src
    out, changed = [], 0
    for line in src.splitlines():
        s = line.lstrip()
        if s.startswith(("//", "*", "/*", "<!--")) and s.rstrip().endswith("."):
            indent = line[: len(line) - len(s)]
            body = s.rstrip()[:-1].rstrip()
            trimmed = True
            while trimmed:
                trimmed = False
                m = re.search(r"(\w+)'s$", body)
                if m:
                    body = body[: m.start()].rstrip()
                    trimmed = True
                    continue
                m = re.search(r"(?:\s|^)(\w+)$", body)
                if m and m.group(1).lower() in DANGLE:
                    body = body[: m.start()].rstrip()
                    trimmed = True
            while body.count("(") > body.count(")"):
                i = body.rfind("(")
                if i < 0:
                    break
                body = undangle(body[:i].rstrip(" ,;:-"))
            if body and len(body.rstrip("/*- ")) > 6 and body != s.rstrip()[:-1].rstrip():
                out.append(f"{indent}{body}.")
                changed += 1
                continue
        out.append(line)
    if not changed:
        return None, 0
    text = ("\r\n" if crlf else "\n").join(out)
    if src.endswith("\n") and not text.endswith("\n"):
        text += "\r\n" if crlf else "\n"
    return text, changed


def trailing_sweep(path):
    """Provenance removal from comments that ride after code on the same line."""
    try:
        with open(path, encoding="utf-8", newline="") as fh:
            src = fh.read()
    except OSError:
        return None, 0
    crlf = "\r\n" in src
    out, changed = [], 0
    for line in src.splitlines():
        m = re.search(r"(\s+)//(?![/:])", line)
        if m and MARK.search(line[m.end():]):
            head = line[: m.end()]
            new = residue_line(line[m.end():]).rstrip()
            if new and new != line[m.end():].rstrip():
                out.append(f"{head}{new}")
                changed += 1
                continue
        out.append(line)
    if not changed:
        return None, 0
    text = ("\r\n" if crlf else "\n").join(out)
    if src.endswith("\n") and not text.endswith("\n"):
        text += "\r\n" if crlf else "\n"
    return text, changed


def transform(path, provenance_only=False):
    CURRENT[0] = path
    try:
        with open(path, encoding="utf-8", newline="") as fh:
            src = fh.read()
    except OSError:
        return None, 0
    crlf = "\r\n" in src
    lines = src.splitlines()
    out, i, changed = [], 0, 0
    in_raw, in_import = False, False
    while i < len(lines):
        line = lines[i]
        # comment mass inside an import group is layout: gofmt owns it
        if re.match(r"^\s*import\s*\(", line):
            in_import = True
        if in_import:
            out.append(line)
            if re.match(r"^\s*\)", line):
                in_import = False
            i += 1
            continue
        if path.endswith(".go") and line.count("`") % 2 == 1 and not line.lstrip().startswith(
            ("//", "/*", "*", "#")
        ):
            in_raw = not in_raw
        if in_raw:
            out.append(line)
            i += 1
            continue
        marker, idx = block_at(lines, i)
        if not idx:
            out.append(line)
            i += 1
            continue
        if provenance_only:
            texts = [comment_text(lines[k]) for k in idx]
            body = " ".join(t for t in texts if t)
            cleaned = strip_provenance(body)
            if cleaned and cleaned != body and not DIRECTIVE.match(lines[idx[0]].lstrip()):
                indent = lines[idx[0]][: len(lines[idx[0]]) - len(lines[idx[0]].lstrip())]
                out.extend(f"{indent}{marker} {w}" for w in wrap(cleaned, indent))
                changed += 1
            else:
                out.extend(lines[k] for k in idx)
            i = idx[-1] + 1
            continue
        repl = reduce_block(lines, idx, marker)
        if repl is None:
            out.extend(lines[k] for k in idx)
        else:
            out.extend(repl)
            changed += 1
        i = idx[-1] + 1
    if not changed:
        return None, 0
    cleaned, blanks = [], 0
    for l in out:
        if l.strip():
            blanks = 0
            cleaned.append(l)
        else:
            blanks += 1
            if blanks <= 2:
                cleaned.append(l)
    text = ("\r\n" if crlf else "\n").join(cleaned)
    if src.endswith("\n") and not text.endswith("\n"):
        text += "\r\n" if crlf else "\n"
    return text, changed


def walk(root, only, provenance_only):
    exts = dict(LANGS)
    if provenance_only:
        exts[".sh"] = "sh"
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = sorted(d for d in dirnames if d not in SKIP_DIRS)
        rel_dir = os.path.relpath(dirpath, root).replace("\\", "/")
        if rel_dir.startswith(SKIP_PATHS):
            continue
        for name in sorted(filenames):
            if os.path.splitext(name)[1] not in exts:
                continue
            path = os.path.join(dirpath, name)
            rel = os.path.relpath(path, root).replace("\\", "/")
            if only and not rel.startswith(only):
                continue
            yield path, rel


def main(argv):
    apply = "--apply" in argv
    prov = "--provenance" in argv
    residue = "--residue" in argv
    dangle = "--dangle" in argv
    trailing = "--trailing" in argv
    words = [a for a in argv if not a.startswith("--")]
    root = words[0] if words else "."
    only = words[1] if len(words) > 1 else None
    files = blocks = 0
    for path, rel in walk(root, only, prov or residue):
        if residue:
            text, changed = residue_sweep(path)
        elif dangle:
            text, changed = dangle_sweep(path)
        elif trailing:
            text, changed = trailing_sweep(path)
        else:
            text, changed = transform(path, prov)
        if not text:
            continue
        blocks += changed
        files += 1
        if apply:
            with open(path, "w", encoding="utf-8", newline="") as fh:
                fh.write(text)
    verb = "rewrote" if apply else "would rewrite"
    print(f"{verb} {blocks} comment block(s) in {files} file(s)")


if __name__ == "__main__":
    main(sys.argv[1:])
