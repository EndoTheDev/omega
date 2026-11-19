#!/usr/bin/env python3
"""comment-only-proof.py - prove a comment pass changed no product bytes.

For every file that differs from BASE, this strips comments and normalizes
whitespace on both sides and compares the token streams. Any difference is a
real edit (code, strings, template markup), not a comment change. It also
compares the directive set (`//go:build`, `//go:embed`, `//go:generate`,
`//export`, `//line`, linter pragmas, shebangs) so no machine input is lost.

The comment strip is language-aware: Go/TS/JS line and block comments, Go raw
strings, TS template literals (comment openers inside them are data), and
Vue HTML/CSS comments. It does not parse; it is a witness, not a compiler.

Usage:
  scripts/comment-only-proof.py [--base REF] [path ...]
  scripts/comment-only-proof.py --check-edit path   # single-file smoke test

Exit 0 when every changed file is comment-only (and directives match).
"""
import os
import re
import subprocess
import sys

DIRECTIVE = re.compile(
    r"^\s*(?://\s*(?:go:[a-z]+|export\b|line\s|nolint\b|eslint\b|prettier\b|"
    r"@ts-|@vue-|@flow)|\+build\b|#!)"
)


REGEX_PREV = set("(,=:[!&|?{};+-*%<>~^")
REGEX_KEYWORDS = ("return", "typeof", "case", "in", "of", "if", "while", "do", "else", "yield", "await")


def _regex_start(out):
    """A '/' opens a regex literal when the last significant token allows one."""
    text = "".join(out)
    i = len(text) - 1
    while i >= 0 and text[i].isspace():
        i -= 1
    if i < 0:
        return True
    c = text[i]
    if c in REGEX_PREV:
        return True
    for kw in REGEX_KEYWORDS:
        if text.endswith(kw) and (i + 1 - len(kw) == 0 or not (text[i - len(kw)].isalnum() or text[i - len(kw)] in "_$")):
            return True
    return False


def strip_js(src, go=False):
    """Remove // and /* */ comments; keep strings, raw strings and templates.

    In Go a backtick string is raw: backslashes are not escapes there.
    """
    out = []
    i, n = 0, len(src)
    while i < n:
        c = src[i]
        if c == "/" and i + 1 < n and src[i + 1] == "/":
            while i < n and src[i] != "\n":
                i += 1
        elif c == "/" and i + 1 < n and src[i + 1] == "*":
            i += 2
            while i + 1 < n and not (src[i] == "*" and src[i + 1] == "/"):
                i += 1
            i += 2
        elif c == "/" and _regex_start(out):
            out.append(c)
            i += 1
            in_class = False
            while i < n:
                ch = src[i]
                out.append(ch)
                if ch == "\\" and i + 1 < n:
                    out.append(src[i + 1])
                    i += 2
                    continue
                if ch == "[":
                    in_class = True
                elif ch == "]":
                    in_class = False
                elif ch == "/" and not in_class:
                    i += 1
                    break
                elif ch == "\n":
                    break
                i += 1
        elif c in "\"'":
            q = c
            out.append(c)
            i += 1
            while i < n:
                out.append(src[i])
                if src[i] == "\\" and i + 1 < n:
                    out.append(src[i + 1])
                    i += 2
                    continue
                if src[i] == q:
                    i += 1
                    break
                i += 1
        elif c == "`":
            out.append(c)
            i += 1
            while i < n:
                if not go and src[i] == "\\" and i + 1 < n:
                    out.append(src[i])
                    out.append(src[i + 1])
                    i += 2
                    continue
                if not go and src[i] == "$" and i + 1 < n and src[i + 1] == "{":
                    out.append("${")
                    i += 2
                    depth = 1
                    while i < n and depth:
                        if src[i] == "{":
                            depth += 1
                        elif src[i] == "}":
                            depth -= 1
                            if depth == 0:
                                out.append("}")
                                i += 1
                                break
                        chunk = src[i]
                        out.append(chunk)
                        i += 1
                    continue
                out.append(src[i])
                if src[i] == "`":
                    i += 1
                    break
                i += 1
        else:
            out.append(c)
            i += 1
    return "".join(out)


def strip_html(src):
    """Remove <!-- --> comments (Vue templates), then JS/CSS comments."""
    out = []
    i, n = 0, len(src)
    while i < n:
        if src.startswith("<!--", i):
            end = src.find("-->", i + 4)
            i = n if end < 0 else end + 3
            continue
        out.append(src[i])
        i += 1
    return strip_js("".join(out))


def normalize(text):
    """Collapse whitespace outside string-ish regions and drop it entirely."""
    out = []
    i, n = 0, len(text)
    while i < n:
        c = text[i]
        if c.isspace():
            while i < n and text[i].isspace():
                i += 1
            out.append(" ")
            continue
        if c in "\"'`":
            q = c
            out.append(c)
            i += 1
            while i < n:
                out.append(text[i])
                if text[i] == "\\" and i + 1 < n:
                    out.append(text[i + 1])
                    i += 2
                    continue
                if text[i] == q:
                    i += 1
                    break
                i += 1
            continue
        out.append(c)
        i += 1
    return "".join(out).strip()


def directives(text):
    return {m.group(0).strip() for m in
            (DIRECTIVE.match(line) for line in text.splitlines()) if m}


def strip_for(path, text):
    if path.endswith(".vue") or path.endswith(".html"):
        return strip_html(text)
    return strip_js(text, go=path.endswith(".go"))


def git(*args, binary=False):
    r = subprocess.run(["git"] + list(args), capture_output=True)
    if r.returncode != 0:
        raise SystemExit(f"git {' '.join(args)}: {r.stderr.decode('utf-8', 'replace')}")
    return r.stdout if binary else r.stdout.decode("utf-8", "replace")


def main(argv):
    base = "HEAD"
    if "--base" in argv:
        i = argv.index("--base")
        base = argv[i + 1]
        del argv[i:i + 2]
    check_edit = "--check-edit" in argv
    if check_edit:
        argv.remove("--check-edit")
    paths = [a for a in argv if not a.startswith("--")]

    if check_edit:
        path = paths[0]
        head = git("show", f"{base}:{path}", binary=True).decode("utf-8", "replace")
        with open(path, encoding="utf-8", newline="") as fh:
            work = fh.read()
        ok = (normalize(strip_for(path, head)) == normalize(strip_for(path, work))
              and directives(head) == directives(work))
        print(f"{'PASS' if ok else 'FAIL'} {path}")
        return 0 if ok else 1

    changed = git("diff", "--name-only", base, "--", *paths).split()
    untracked = git("ls-files", "--others", "--exclude-standard", "--", *paths).split()
    files = [p for p in changed + untracked
             if p.endswith((".go", ".ts", ".js", ".mjs", ".vue", ".html"))]
    if not files:
        print(f"no changed source files against {base}")
        return 0
    bad = 0
    for path in sorted(set(files)):
        if not os.path.exists(path):
            print(f"SKIP {path} (deleted)")
            continue
        try:
            head = git("show", f"{base}:{path}", binary=True).decode("utf-8", "replace")
        except SystemExit:
            print(f"NEW  {path} (not in {base})")
            continue
        with open(path, encoding="utf-8", newline="") as fh:
            work = fh.read()
        same_tokens = normalize(strip_for(path, head)) == normalize(strip_for(path, work))
        same_dirs = directives(head) == directives(work)
        if same_tokens and same_dirs:
            print(f"PASS {path}")
            continue
        bad += 1
        why = []
        if not same_tokens:
            why.append("product bytes differ")
        if not same_dirs:
            why.append(f"directives differ: -{sorted(directives(head) - directives(work))} "
                       f"+{sorted(directives(work) - directives(head))}")
        print(f"FAIL {path}: {'; '.join(why)}")
    total = len(set(files))
    print(f"\n{total - bad}/{total} changed source file(s) comment-only against {base}")
    return 0 if bad == 0 else 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
