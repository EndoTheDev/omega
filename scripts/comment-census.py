#!/usr/bin/env python3
"""comment-census.py - measure source comment density, and find provenance.

The comment law lives in docs/release-gate.md. Two numbers matter:

  density     comment lines / nonblank lines, per language and per file.
              Repo target ~10%; a file above 30% is worth a look.
  provenance  comment lines carrying history markers (card ids, dates,
              "proven"/"ruling"/"receipt"). The target is zero.

Heuristic by design: a "comment line" is a line whose stripped text opens
a `//` or `/*` comment, continues a `/* */` block, or opens an HTML
comment in a template. It does not parse the language, so a `//` inside a
string or a doc block nested in a template is counted as written. That is
fine for a density trend; it is not a compiler.

Usage:
  scripts/comment-census.py [root]            densities + worst files
  scripts/comment-census.py --marks [root]    provenance comment lines only
  scripts/comment-census.py --top 50 [root]   longer worst-file list

Exit status: 0 always (advisory, like docs-health.sh); gate it by reading
the numbers, or pass --check to fail when the target is missed.
"""
import os
import re
import sys

SKIP_DIRS = {"node_modules", "dist", ".nuxt", ".output", ".git", "bin", ".worktrees"}
SKIP_PATHS = ("api/static", "webui/dist")
CEILING_PCT = 30.0
TARGET_PCT = 10.0

MARK = re.compile(
    r"t_[0-9a-f]{8}\b|20[0-9]{2}-[0-9]{2}-[0-9]{2}|\bproven\b|\bruling\b|\breceipt\b",
    re.IGNORECASE,
)


def classify(line, in_block):
    """Return (is_comment, in_block_after)."""
    s = line.strip()
    if in_block:
        if "*/" in s:
            return True, False
        return True, True
    if s.startswith("//") or s.startswith("/*") or s.startswith("* ") or s == "*":
        return True, (s.startswith("/*") and "*/" not in s)
    return False, False


def scan(path):
    nonblank = comments = 0
    marks = []
    in_block = in_html = False
    try:
        with open(path, encoding="utf-8", errors="replace") as fh:
            for num, line in enumerate(fh, 1):
                if not line.strip():
                    continue
                nonblank += 1
                if in_html:
                    comments += 1
                    is_c = True
                    if "-->" in line:
                        in_html = False
                elif line.strip().startswith("<!--"):
                    comments += 1
                    is_c = True
                    if "-->" not in line:
                        in_html = True
                else:
                    is_c, in_block = classify(line, in_block)
                    if is_c:
                        comments += 1
                if is_c and MARK.search(line):
                    marks.append((num, line.rstrip()))
    except OSError:
        return None
    return nonblank, comments, marks


def main(argv):
    want_marks = "--marks" in argv
    check = "--check" in argv
    top = 25
    if "--top" in argv:
        top = int(argv[argv.index("--top") + 1])
    words = [a for a in argv if not a.startswith("--") and not a.isdigit()]
    root = words[0] if words else "."

    totals = {}
    rows = []
    marks_by_file = {}
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames if d not in SKIP_DIRS]
        rel_dir = os.path.relpath(dirpath, root).replace("\\", "/")
        if rel_dir.startswith(SKIP_PATHS):
            continue
        for name in sorted(filenames):
            if name.endswith(".go"):
                lang = "go"
            elif name.endswith((".ts", ".js", ".mjs", ".vue")):
                lang = "webui"
            else:
                continue
            path = os.path.join(dirpath, name)
            got = scan(path)
            if not got or not got[0]:
                continue
            nonblank, comments, marks = got
            agg = totals.setdefault(lang, [0, 0])
            agg[0] += nonblank
            agg[1] += comments
            rel = os.path.relpath(path, root).replace("\\", "/")
            rows.append((100.0 * comments / nonblank, comments, nonblank, rel))
            if marks:
                marks_by_file[rel] = marks

    if want_marks:
        total = 0
        for rel, marks in sorted(marks_by_file.items()):
            for num, text in marks:
                print(f"{rel}:{num}: {text.strip()}")
                total += 1
        print(f"# {total} provenance comment line(s) in {len(marks_by_file)} file(s)", file=sys.stderr)
        return 0 if not check or total == 0 else 1

    failed = False
    for lang in sorted(totals):
        nonblank, comments = totals[lang]
        pct = 100.0 * comments / nonblank
        print(f"{lang}: {comments} comment / {nonblank} nonblank = {pct:.1f}%")
        if pct > TARGET_PCT:
            failed = True
    over = [r for r in sorted(rows, reverse=True) if r[0] > CEILING_PCT]
    print(f"\ntop {top} comment-dense files:")
    for pct, comments, nonblank, rel in sorted(rows, reverse=True)[:top]:
        print(f"  {pct:5.1f}%  {comments:4d}/{nonblank:4d}  {rel}")
    print(f"\nfiles above {CEILING_PCT:.0f}%: {len(over)}")
    if check and (failed or over):
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
